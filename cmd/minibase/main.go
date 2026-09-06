package main

import (
	"context"
	"errors"
	"log/slog"
	"net"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/conradevans/MiniBase/internal/accessauth"
	"github.com/conradevans/MiniBase/internal/api"
	"github.com/conradevans/MiniBase/internal/backups"
	"github.com/conradevans/MiniBase/internal/config"
	"github.com/conradevans/MiniBase/internal/integrationauth"
	"github.com/conradevans/MiniBase/internal/metadata"
	"github.com/conradevans/MiniBase/internal/minideploy"
	"github.com/conradevans/MiniBase/internal/provisioning"
	"github.com/conradevans/MiniBase/internal/secrets"
)

const shutdownTimeout = 10 * time.Second

type serverResult struct {
	name string
	err  error
}

func serveHTTP(
	name string,
	server *http.Server,
	listener net.Listener,
	results chan<- serverResult,
) {
	err := server.Serve(listener)
	if errors.Is(err, http.ErrServerClosed) {
		err = nil
	}

	results <- serverResult{
		name: name,
		err:  err,
	}
}

func main() {
	os.Exit(run())
}

func run() int {
	logger := slog.New(slog.NewJSONHandler(os.Stderr, nil))

	cfg, err := config.Parse(os.Args[1:])
	if err != nil {
		logger.Error("invalid configuration")
		return 2
	}

	store, err := metadata.Open(context.Background(), cfg.MetadataDBPath)
	if err != nil {
		logger.Error("metadata initialization failed")
		return 1
	}
	defer func() {
		if err := store.Close(); err != nil {
			logger.Error("metadata shutdown failed")
		}
	}()

	credentialStore, err := secrets.New(cfg.DatabaseSecretRoot)
	if err != nil {
		logger.Error("database credential store initialization failed")
		return 1
	}
	provisioningService := provisioning.NewService(store, credentialStore, provisioning.NewDockerPostgres())
	if err := provisioningService.Reconcile(context.Background()); err != nil {
		logger.Error("provisioning reconciliation failed")
		return 1
	}

	archiveStore, err := backups.NewFileStore(cfg.BackupRoot)
	if err != nil {
		logger.Error("backup archive store initialization failed")
		return 1
	}
	backupService := backups.NewService(store, archiveStore, backups.NewDockerPostgres(), provisioningService)
	if cfg.RunDueBackups {
		result, err := backupService.RunDueAutomaticBackups(context.Background())
		if err != nil {
			logger.Error("automatic backup run failed")
			return 1
		}
		logger.Info(
			"automatic backup run complete",
			"databases_checked", result.DatabasesChecked,
			"backups_created", result.BackupsCreated,
			"backups_pruned", result.BackupsPruned,
		)
		return 0
	}

	schemaVersion, err := store.SchemaVersion(context.Background())
	if err != nil {
		logger.Error("schema version check failed")
		return 1
	}

	integrationToken, err := integrationauth.Ensure(cfg.IntegrationTokenPath)
	if err != nil {
		logger.Error("MiniDeploy integration token initialization failed")
		return 1
	}

	handler := api.New(store, provisioningService, backupService, cfg.FrontendDir, logger)
	handler.ConfigureMiniDeployIntegration(integrationToken, credentialStore)

	miniDeployClient, err := minideploy.NewClient(
		minideploy.DefaultURL,
		integrationToken,
		&http.Client{
			Timeout: 10 * time.Minute,
		},
	)
	if err != nil {
		logger.Error("MiniDeploy lifecycle client initialization failed")
		return 1
	}
	handler.ConfigureMiniDeployLifecycle(miniDeployClient)

	accessValidator, accessErr :=
		accessauth.NewCloudflareAccessValidator(
			accessauth.ConfigFromEnvironment(),
		)
	if accessErr != nil {
		logger.Warn(
			"public administrator routes disabled",
		)
		accessValidator = nil
	}

	managementServer := api.HTTPServer(
		cfg.ListenAddress,
		handler,
	)

	publicServer := api.HTTPServer(
		cfg.PublicListenAddress,
		api.PublicHandler(
			handler,
			accessValidator,
		),
	)

	managementListener, err := net.Listen(
		"tcp",
		cfg.ListenAddress,
	)
	if err != nil {
		logger.Error(
			"private HTTP listener startup failed",
		)
		return 1
	}

	publicListener, err := net.Listen(
		"tcp",
		cfg.PublicListenAddress,
	)
	if err != nil {
		_ = managementListener.Close()
		logger.Error(
			"public HTTP listener startup failed",
		)
		return 1
	}

	logger.Info(
		"MiniBase private control plane started",
		"listen",
		managementListener.Addr().String(),
		"schema_version",
		schemaVersion,
	)

	logger.Info(
		"MiniBase public origin started",
		"listen",
		publicListener.Addr().String(),
	)

	results := make(chan serverResult, 2)

	go serveHTTP(
		"private",
		managementServer,
		managementListener,
		results,
	)

	go serveHTTP(
		"public",
		publicServer,
		publicListener,
		results,
	)

	signals := make(chan os.Signal, 1)
	signal.Notify(
		signals,
		syscall.SIGINT,
		syscall.SIGTERM,
	)
	defer signal.Stop(signals)

	completedServers := 0
	exitCode := 0

	select {
	case receivedSignal := <-signals:
		logger.Info(
			"shutdown requested",
			"signal",
			receivedSignal.String(),
		)

	case result := <-results:
		completedServers = 1
		exitCode = 1

		if result.err != nil {
			logger.Error(
				"HTTP server stopped unexpectedly",
				"server",
				result.name,
			)
		} else {
			logger.Error(
				"HTTP server stopped unexpectedly",
				"server",
				result.name,
			)
		}
	}

	shutdownContext, cancel :=
		context.WithTimeout(
			context.Background(),
			shutdownTimeout,
		)
	defer cancel()

	for _, target := range []struct {
		name   string
		server *http.Server
	}{
		{
			name:   "private",
			server: managementServer,
		},
		{
			name:   "public",
			server: publicServer,
		},
	} {
		if err := target.server.Shutdown(
			shutdownContext,
		); err != nil {
			logger.Error(
				"graceful shutdown failed",
				"server",
				target.name,
			)
			exitCode = 1
		}
	}

	for completedServers < 2 {
		result := <-results
		completedServers++

		if result.err != nil {
			logger.Error(
				"HTTP server shutdown failed",
				"server",
				result.name,
			)
			exitCode = 1
		}
	}

	if exitCode != 0 {
		return exitCode
	}

	logger.Info("MiniBase control plane stopped")
	return 0
}
