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

	"github.com/conradevans/MiniBase/internal/api"
	"github.com/conradevans/MiniBase/internal/backups"
	"github.com/conradevans/MiniBase/internal/config"
	"github.com/conradevans/MiniBase/internal/metadata"
	"github.com/conradevans/MiniBase/internal/provisioning"
	"github.com/conradevans/MiniBase/internal/secrets"
)

const shutdownTimeout = 10 * time.Second

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

	handler := api.New(store, provisioningService, backupService, cfg.FrontendDir, logger)
	server := api.HTTPServer(cfg.ListenAddress, handler)
	listener, err := net.Listen("tcp", cfg.ListenAddress)
	if err != nil {
		logger.Error("HTTP listener startup failed")
		return 1
	}

	logger.Info(
		"MiniBase control plane started",
		"listen", listener.Addr().String(),
		"schema_version", schemaVersion,
	)

	serverErrors := make(chan error, 1)
	go func() {
		serverErrors <- server.Serve(listener)
	}()

	signals := make(chan os.Signal, 1)
	signal.Notify(signals, syscall.SIGINT, syscall.SIGTERM)
	defer signal.Stop(signals)

	select {
	case signal := <-signals:
		logger.Info("shutdown requested", "signal", signal.String())
	case serveErr := <-serverErrors:
		if !errors.Is(serveErr, http.ErrServerClosed) {
			logger.Error("HTTP server stopped unexpectedly")
			return 1
		}
		return 0
	}

	shutdownContext, cancel := context.WithTimeout(context.Background(), shutdownTimeout)
	defer cancel()
	if err := server.Shutdown(shutdownContext); err != nil {
		logger.Error("graceful shutdown failed")
		return 1
	}

	serveErr := <-serverErrors
	if !errors.Is(serveErr, http.ErrServerClosed) {
		logger.Error("HTTP server stopped unexpectedly during shutdown")
		return 1
	}

	logger.Info("MiniBase control plane stopped")
	return 0
}
