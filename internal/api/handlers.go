package api

import (
	"encoding/json"
	"errors"
	"net/http"
	"strings"

	"github.com/conradevans/MiniBase/internal/metadata"
)

const databasesPathPrefix = "/api/v1/databases/"

type errorResponse struct {
	Error apiError `json:"error"`
}

type apiError struct {
	Code    string `json:"code"`
	Message string `json:"message"`
}

type healthResponse struct {
	Status           string `json:"status"`
	MetadataDatabase string `json:"metadataDatabase"`
}

type statusResponse struct {
	Service          string `json:"service"`
	APIVersion       string `json:"apiVersion"`
	MetadataDatabase string `json:"metadataDatabase"`
	SchemaVersion    int    `json:"schemaVersion"`
}

func (s *Server) ServeHTTP(response http.ResponseWriter, request *http.Request) {
	response.Header().Set("Cache-Control", "no-store")
	response.Header().Set("X-Content-Type-Options", "nosniff")

	if request.Method != http.MethodGet {
		if knownPath(request.URL.Path) {
			response.Header().Set("Allow", http.MethodGet)
			writeError(response, http.StatusMethodNotAllowed, "method_not_allowed", "method not allowed")
			return
		}
		writeError(response, http.StatusNotFound, "not_found", "resource not found")
		return
	}

	switch {
	case request.URL.Path == "/health":
		s.handleHealth(response, request)
	case request.URL.Path == "/api/v1/status":
		s.handleStatus(response, request)
	case request.URL.Path == "/api/v1/databases":
		s.handleListDatabases(response, request)
	case strings.HasPrefix(request.URL.Path, databasesPathPrefix):
		s.handleGetDatabase(response, request)
	default:
		writeError(response, http.StatusNotFound, "not_found", "resource not found")
	}
}

func (s *Server) handleHealth(response http.ResponseWriter, request *http.Request) {
	if err := s.store.Ping(request.Context()); err != nil {
		s.logger.Error("health check failed", "component", "metadata_database")
		writeError(response, http.StatusServiceUnavailable, "service_unavailable", "service unavailable")
		return
	}
	writeJSON(response, http.StatusOK, healthResponse{
		Status:           "ok",
		MetadataDatabase: "reachable",
	})
}

func (s *Server) handleStatus(response http.ResponseWriter, request *http.Request) {
	if err := s.store.Ping(request.Context()); err != nil {
		s.logger.Error("status check failed", "component", "metadata_database")
		writeError(response, http.StatusServiceUnavailable, "service_unavailable", "service unavailable")
		return
	}
	schemaVersion, err := s.store.SchemaVersion(request.Context())
	if err != nil {
		s.logger.Error("status check failed", "component", "schema_version")
		writeError(response, http.StatusServiceUnavailable, "service_unavailable", "service unavailable")
		return
	}
	writeJSON(response, http.StatusOK, statusResponse{
		Service:          "minibase",
		APIVersion:       APIVersion,
		MetadataDatabase: "reachable",
		SchemaVersion:    schemaVersion,
	})
}

func (s *Server) handleListDatabases(response http.ResponseWriter, request *http.Request) {
	databases, err := s.store.ListDatabases(request.Context())
	if err != nil {
		s.logger.Error("database metadata listing failed")
		writeError(response, http.StatusInternalServerError, "internal_error", "internal server error")
		return
	}
	writeJSON(response, http.StatusOK, databases)
}

func (s *Server) handleGetDatabase(response http.ResponseWriter, request *http.Request) {
	id := strings.TrimPrefix(request.URL.Path, databasesPathPrefix)
	if id == "" || strings.Contains(id, "/") {
		writeError(response, http.StatusNotFound, "not_found", "resource not found")
		return
	}

	database, err := s.store.GetDatabase(request.Context(), id)
	if errors.Is(err, metadata.ErrNotFound) {
		writeError(response, http.StatusNotFound, "not_found", "database not found")
		return
	}
	if err != nil {
		s.logger.Error("database metadata lookup failed")
		writeError(response, http.StatusInternalServerError, "internal_error", "internal server error")
		return
	}
	writeJSON(response, http.StatusOK, database)
}

func knownPath(path string) bool {
	if path == "/health" || path == "/api/v1/status" || path == "/api/v1/databases" {
		return true
	}
	if !strings.HasPrefix(path, databasesPathPrefix) {
		return false
	}
	id := strings.TrimPrefix(path, databasesPathPrefix)
	return id != "" && !strings.Contains(id, "/")
}

func writeError(response http.ResponseWriter, status int, code, message string) {
	writeJSON(response, status, errorResponse{
		Error: apiError{
			Code:    code,
			Message: message,
		},
	})
}

func writeJSON(response http.ResponseWriter, status int, value any) {
	response.Header().Set("Content-Type", "application/json; charset=utf-8")
	response.WriteHeader(status)
	_ = json.NewEncoder(response).Encode(value)
}
