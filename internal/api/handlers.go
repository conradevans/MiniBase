package api

import (
	"encoding/json"
	"errors"
	"io"
	"mime"
	"net/http"
	"strings"

	"github.com/conradevans/MiniBase/internal/metadata"
	"github.com/conradevans/MiniBase/internal/provisioning"
)

const (
	databasesPathPrefix = "/api/v1/databases/"
	maxRequestBodyBytes = 4 << 10
)

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

type createDatabaseRequest struct {
	DisplayName string `json:"displayName"`
}

func (s *Server) ServeHTTP(response http.ResponseWriter, request *http.Request) {
	response.Header().Set("Cache-Control", "no-store")
	response.Header().Set("X-Content-Type-Options", "nosniff")

	switch {
	case request.URL.Path == "/health":
		s.requireGet(response, request, s.handleHealth)
	case request.URL.Path == "/api/v1/status":
		s.requireGet(response, request, s.handleStatus)
	case request.URL.Path == "/api/v1/databases":
		switch request.Method {
		case http.MethodGet:
			s.handleListDatabases(response, request)
		case http.MethodPost:
			s.handleCreateDatabase(response, request)
		default:
			response.Header().Set("Allow", "GET, POST")
			writeError(response, http.StatusMethodNotAllowed, "method_not_allowed", "method not allowed")
		}
	case strings.HasPrefix(request.URL.Path, databasesPathPrefix):
		id := strings.TrimPrefix(request.URL.Path, databasesPathPrefix)
		if id == "" || strings.Contains(id, "/") {
			writeError(response, http.StatusNotFound, "not_found", "resource not found")
			return
		}
		if request.Method != http.MethodGet {
			response.Header().Set("Allow", http.MethodGet)
			writeError(response, http.StatusMethodNotAllowed, "method_not_allowed", "method not allowed")
			return
		}
		s.handleGetDatabase(response, request, id)
	default:
		writeError(response, http.StatusNotFound, "not_found", "resource not found")
	}
}

func (s *Server) requireGet(response http.ResponseWriter, request *http.Request, handler http.HandlerFunc) {
	if request.Method != http.MethodGet {
		response.Header().Set("Allow", http.MethodGet)
		writeError(response, http.StatusMethodNotAllowed, "method_not_allowed", "method not allowed")
		return
	}
	handler(response, request)
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

func (s *Server) handleGetDatabase(response http.ResponseWriter, request *http.Request, id string) {
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

func (s *Server) handleCreateDatabase(response http.ResponseWriter, request *http.Request) {
	mediaType, _, err := mime.ParseMediaType(request.Header.Get("Content-Type"))
	if err != nil || mediaType != "application/json" {
		writeError(response, http.StatusUnsupportedMediaType, "unsupported_media_type", "Content-Type must be application/json")
		return
	}
	if s.provisioner == nil {
		writeError(response, http.StatusServiceUnavailable, "service_unavailable", "service unavailable")
		return
	}

	request.Body = http.MaxBytesReader(response, request.Body, maxRequestBodyBytes)
	decoder := json.NewDecoder(request.Body)
	decoder.DisallowUnknownFields()

	var input createDatabaseRequest
	if err := decoder.Decode(&input); err != nil {
		var maxBytesError *http.MaxBytesError
		if errors.As(err, &maxBytesError) {
			writeError(response, http.StatusRequestEntityTooLarge, "request_too_large", "request body too large")
			return
		}
		writeError(response, http.StatusBadRequest, "invalid_request", "invalid JSON request")
		return
	}
	if err := decoder.Decode(&struct{}{}); !errors.Is(err, io.EOF) {
		writeError(response, http.StatusBadRequest, "invalid_request", "request must contain one JSON value")
		return
	}

	database, err := s.provisioner.ProvisionDatabase(request.Context(), input.DisplayName)
	if errors.Is(err, metadata.ErrInvalidDisplayName) {
		writeError(response, http.StatusBadRequest, "invalid_display_name", "displayName is required and must be at most 200 characters")
		return
	}
	if err != nil {
		s.logger.Error("database provisioning failed")
		writeError(response, http.StatusInternalServerError, "provisioning_failed", "database provisioning failed")
		return
	}
	writeJSON(response, http.StatusCreated, database)
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

var _ databaseProvisioner = (*provisioning.Service)(nil)
