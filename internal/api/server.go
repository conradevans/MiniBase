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
	writeTimeout      = 15 * time.Second
	idleTimeout       = 60 * time.Second
)

type metadataReader interface {
	Ping(context.Context) error
	SchemaVersion(context.Context) (int, error)
	ListDatabases(context.Context) ([]metadata.Database, error)
	GetDatabase(context.Context, string) (metadata.Database, error)
}

type Server struct {
	store  metadataReader
	logger *slog.Logger
}

func New(store metadataReader, logger *slog.Logger) *Server {
	if logger == nil {
		logger = slog.New(slog.DiscardHandler)
	}
	return &Server{store: store, logger: logger}
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
