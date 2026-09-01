package api

import (
	"context"
	"log/slog"
	"net/http"
	"time"

	"github.com/conradevans/MiniBase/internal/metadata"
)

const (
	APIVersion        = "v1"
	maxHeaderBytes    = 1 << 20
	readHeaderTimeout = 5 * time.Second
	readTimeout       = 10 * time.Second
	writeTimeout      = 15 * time.Minute
	idleTimeout       = 60 * time.Second
)

type metadataReader interface {
	Ping(context.Context) error
	SchemaVersion(context.Context) (int, error)
	ListDatabases(context.Context) ([]metadata.Database, error)
	GetDatabase(context.Context, string) (metadata.Database, error)
}

type databaseProvisioner interface {
	ProvisionDatabase(context.Context, string) (metadata.Database, error)
}

type backupManager interface {
	ListBackups(context.Context) ([]metadata.Backup, error)
	GetBackup(context.Context, string) (metadata.Backup, error)
	ListBackupsForDatabase(context.Context, string) ([]metadata.Backup, error)
	CreateBackup(context.Context, string) (metadata.Backup, error)
	RestoreAsNewDatabase(context.Context, string, string) (metadata.Database, error)
	RestoreInPlace(context.Context, string, string) (metadata.Database, error)
}

type Server struct {
	store       metadataReader
	provisioner databaseProvisioner
	backups     backupManager
	logger      *slog.Logger
	frontend    http.Handler
}

func New(store metadataReader, provisioner databaseProvisioner, backupService backupManager, frontendDirectory string, logger *slog.Logger) *Server {
	if logger == nil {
		logger = slog.New(slog.DiscardHandler)
	}
	return &Server{
		store:       store,
		provisioner: provisioner,
		backups:     backupService,
		frontend:    newFrontendHandler(frontendDirectory),
		logger:      logger,
	}
}

func HTTPServer(address string, handler http.Handler) *http.Server {
	return &http.Server{
		Addr:              address,
		Handler:           handler,
		ReadHeaderTimeout: readHeaderTimeout,
		ReadTimeout:       readTimeout,
		WriteTimeout:      writeTimeout,
		IdleTimeout:       idleTimeout,
		MaxHeaderBytes:    maxHeaderBytes,
	}
}
