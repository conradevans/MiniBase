package api

import (
	"bytes"
	"encoding/json"
	"errors"
	"io"
	"mime"
	"net/http"
	"strings"
	"time"

	backupservice "github.com/conradevans/MiniBase/internal/backups"
	"github.com/conradevans/MiniBase/internal/ids"
	"github.com/conradevans/MiniBase/internal/metadata"
)

type backupResponse struct {
	ID                  string                `json:"id"`
	DatabaseID          string                `json:"databaseId"`
	DatabaseDisplayName string                `json:"databaseDisplayName"`
	Kind                metadata.BackupKind   `json:"kind"`
	Status              metadata.BackupStatus `json:"status"`
	SizeBytes           int64                 `json:"sizeBytes"`
	CreatedAt           time.Time             `json:"createdAt"`
	CompletedAt         *time.Time            `json:"completedAt"`
}

type restoreBackupRequest struct {
	Mode             string `json:"mode"`
	DisplayName      string `json:"displayName"`
	TargetDatabaseID string `json:"targetDatabaseId"`
}

func (s *Server) routeBackupCollection(response http.ResponseWriter, request *http.Request) {
	if request.Method != http.MethodGet {
		response.Header().Set("Allow", http.MethodGet)
		writeError(response, http.StatusMethodNotAllowed, "method_not_allowed", "method not allowed")
		return
	}
	s.handleListBackups(response, request)
}

func (s *Server) routeBackupResource(response http.ResponseWriter, request *http.Request) {
	parts := strings.Split(strings.TrimPrefix(request.URL.Path, backupsPathPrefix), "/")
	switch {
	case len(parts) == 1 && parts[0] != "":
		if request.Method != http.MethodGet {
			response.Header().Set("Allow", http.MethodGet)
			writeError(response, http.StatusMethodNotAllowed, "method_not_allowed", "method not allowed")
			return
		}
		s.handleGetBackup(response, request, parts[0])
	case len(parts) == 2 && parts[0] != "" && parts[1] == "restore":
		if request.Method != http.MethodPost {
			response.Header().Set("Allow", http.MethodPost)
			writeError(response, http.StatusMethodNotAllowed, "method_not_allowed", "method not allowed")
			return
		}
		s.handleRestoreBackup(response, request, parts[0])
	default:
		writeError(response, http.StatusNotFound, "not_found", "resource not found")
	}
}

func (s *Server) routeDatabaseResource(response http.ResponseWriter, request *http.Request) {
	parts := strings.Split(strings.TrimPrefix(request.URL.Path, databasesPathPrefix), "/")
	switch {
	case len(parts) == 1 && parts[0] != "":
		switch request.Method {
		case http.MethodGet:
			s.handleGetDatabase(response, request, parts[0])
		case http.MethodDelete:
			s.handleDeleteDatabase(response, request, parts[0])
		default:
			response.Header().Set("Allow", "GET, DELETE")
			writeError(response, http.StatusMethodNotAllowed, "method_not_allowed", "method not allowed")
		}
	case len(parts) == 2 &&
		parts[0] != "" &&
		parts[1] == "attach":

		if request.Method != http.MethodPost {
			response.Header().Set(
				"Allow",
				http.MethodPost,
			)
			writeError(
				response,
				http.StatusMethodNotAllowed,
				"method_not_allowed",
				"method not allowed",
			)
			return
		}

		s.handleAttachDatabase(
			response,
			request,
			parts[0],
		)

	case len(parts) == 2 &&
		parts[0] != "" &&
		parts[1] == "detach":

		if request.Method != http.MethodPost {
			response.Header().Set(
				"Allow",
				http.MethodPost,
			)
			writeError(
				response,
				http.StatusMethodNotAllowed,
				"method_not_allowed",
				"method not allowed",
			)
			return
		}

		s.handleDetachDatabase(
			response,
			request,
			parts[0],
		)

	case len(parts) == 2 && parts[0] != "" && parts[1] == "backups":
		switch request.Method {
		case http.MethodGet:
			s.handleListDatabaseBackups(response, request, parts[0])
		case http.MethodPost:
			s.handleCreateBackup(response, request, parts[0])
		default:
			response.Header().Set("Allow", "GET, POST")
			writeError(response, http.StatusMethodNotAllowed, "method_not_allowed", "method not allowed")
		}
	default:
		writeError(response, http.StatusNotFound, "not_found", "resource not found")
	}
}

func (s *Server) handleDeleteDatabase(response http.ResponseWriter, request *http.Request, databaseID string) {
	if !ids.ValidDatabaseID(databaseID) {
		writeError(response, http.StatusNotFound, "not_found", "database not found")
		return
	}
	if s.backups == nil {
		writeError(response, http.StatusConflict, "database_unavailable", "database is not available")
		return
	}
	err := s.backups.DeleteDatabase(request.Context(), databaseID)
	switch {
	case err == nil:
		response.WriteHeader(http.StatusNoContent)
	case errors.Is(err, metadata.ErrNotFound), errors.Is(err, metadata.ErrInvalidIdentifier):
		writeError(response, http.StatusNotFound, "not_found", "database not found")
	case errors.Is(err, backupservice.ErrDatabaseAttached):
		writeError(response, http.StatusConflict, "database_attached", "database is attached")
	case errors.Is(err, backupservice.ErrDatabaseUnavailable):
		writeError(response, http.StatusConflict, "database_unavailable", "database is not available")
	default:
		s.logger.Error("database deletion failed", "database_id", databaseID)
		writeError(response, http.StatusInternalServerError, "deletion_failed", "database deletion failed")
	}
}

func (s *Server) handleListBackups(response http.ResponseWriter, request *http.Request) {
	if s.backups == nil {
		writeError(response, http.StatusServiceUnavailable, "service_unavailable", "service unavailable")
		return
	}
	backups, err := s.backups.ListBackups(request.Context())
	if err != nil {
		s.logger.Error("backup metadata listing failed")
		writeError(response, http.StatusInternalServerError, "internal_error", "internal server error")
		return
	}
	result, err := s.backupResponses(request, backups)
	if err != nil {
		s.logger.Error("backup database metadata lookup failed")
		writeError(response, http.StatusInternalServerError, "internal_error", "internal server error")
		return
	}
	writeJSON(response, http.StatusOK, result)
}

func (s *Server) handleGetBackup(response http.ResponseWriter, request *http.Request, backupID string) {
	if s.backups == nil {
		writeError(response, http.StatusServiceUnavailable, "service_unavailable", "service unavailable")
		return
	}
	backup, err := s.backups.GetBackup(request.Context(), backupID)
	if err != nil {
		s.writeBackupError(response, err)
		return
	}
	result, err := s.backupResponse(request, backup)
	if err != nil {
		s.logger.Error("backup database metadata lookup failed")
		writeError(response, http.StatusInternalServerError, "internal_error", "internal server error")
		return
	}
	writeJSON(response, http.StatusOK, result)
}

func (s *Server) handleListDatabaseBackups(response http.ResponseWriter, request *http.Request, databaseID string) {
	if s.backups == nil {
		writeError(response, http.StatusServiceUnavailable, "service_unavailable", "service unavailable")
		return
	}
	if _, err := s.store.GetDatabase(request.Context(), databaseID); err != nil {
		s.writeBackupError(response, err)
		return
	}
	backups, err := s.backups.ListBackupsForDatabase(request.Context(), databaseID)
	if err != nil {
		s.writeBackupError(response, err)
		return
	}
	result, err := s.backupResponses(request, backups)
	if err != nil {
		s.logger.Error("backup database metadata lookup failed")
		writeError(response, http.StatusInternalServerError, "internal_error", "internal server error")
		return
	}
	writeJSON(response, http.StatusOK, result)
}

func (s *Server) handleCreateBackup(response http.ResponseWriter, request *http.Request, databaseID string) {
	if s.backups == nil {
		writeError(response, http.StatusServiceUnavailable, "service_unavailable", "service unavailable")
		return
	}
	if err := decodeOptionalEmptyObject(response, request); err != nil {
		writeDecodeError(response, err)
		return
	}
	backup, err := s.backups.CreateBackup(request.Context(), databaseID)
	if err != nil {
		s.writeBackupError(response, err)
		return
	}
	result, err := s.backupResponse(request, backup)
	if err != nil {
		s.logger.Error("created backup database metadata lookup failed")
		writeError(response, http.StatusInternalServerError, "internal_error", "internal server error")
		return
	}
	writeJSON(response, http.StatusCreated, result)
}

func (s *Server) handleRestoreBackup(response http.ResponseWriter, request *http.Request, backupID string) {
	if s.backups == nil {
		writeError(response, http.StatusServiceUnavailable, "service_unavailable", "service unavailable")
		return
	}
	var input restoreBackupRequest
	if err := decodeRequiredJSON(response, request, &input); err != nil {
		writeDecodeError(response, err)
		return
	}

	switch input.Mode {
	case "new":
		if strings.TrimSpace(input.DisplayName) == "" || input.TargetDatabaseID != "" {
			writeError(response, http.StatusBadRequest, "invalid_request", "new restore requires displayName only")
			return
		}
		database, err := s.backups.RestoreAsNewDatabase(request.Context(), backupID, input.DisplayName)
		if errors.Is(err, metadata.ErrInvalidDisplayName) {
			writeError(response, http.StatusBadRequest, "invalid_display_name", "displayName is required and must be at most 200 characters")
			return
		}
		if err != nil {
			s.writeBackupError(response, err)
			return
		}
		writeJSON(response, http.StatusCreated, database)
	case "replace":
		if input.TargetDatabaseID == "" || input.DisplayName != "" {
			writeError(response, http.StatusBadRequest, "invalid_request", "replace restore requires targetDatabaseId only")
			return
		}
		database, err := s.backups.RestoreInPlace(request.Context(), backupID, input.TargetDatabaseID)
		if err != nil {
			s.writeBackupError(response, err)
			return
		}
		writeJSON(response, http.StatusOK, database)
	default:
		writeError(response, http.StatusBadRequest, "invalid_request", "mode must be new or replace")
	}
}

func (s *Server) backupResponses(request *http.Request, backups []metadata.Backup) ([]backupResponse, error) {
	result := make([]backupResponse, 0, len(backups))
	for _, backup := range backups {
		response, err := s.backupResponse(request, backup)
		if err != nil {
			return nil, err
		}
		result = append(result, response)
	}
	return result, nil
}

func (s *Server) backupResponse(request *http.Request, backup metadata.Backup) (backupResponse, error) {
	database, err := s.store.GetDatabase(request.Context(), backup.DatabaseID)
	if err != nil {
		return backupResponse{}, err
	}
	return backupResponse{
		ID:                  backup.ID,
		DatabaseID:          backup.DatabaseID,
		DatabaseDisplayName: database.DisplayName,
		Kind:                backup.Kind,
		Status:              backup.Status,
		SizeBytes:           backup.SizeBytes,
		CreatedAt:           backup.CreatedAt,
		CompletedAt:         backup.CompletedAt,
	}, nil
}

var (
	errInvalidJSON      = errors.New("invalid JSON request")
	errUnsupportedMedia = errors.New("unsupported media type")
	errRequestTooLarge  = errors.New("request body too large")
)

func decodeOptionalEmptyObject(response http.ResponseWriter, request *http.Request) error {
	request.Body = http.MaxBytesReader(response, request.Body, maxRequestBodyBytes)
	body, err := io.ReadAll(request.Body)
	if err != nil {
		var maxBytesError *http.MaxBytesError
		if errors.As(err, &maxBytesError) {
			return errRequestTooLarge
		}
		return errInvalidJSON
	}
	if len(bytes.TrimSpace(body)) == 0 {
		return nil
	}
	if !hasJSONContentType(request) {
		return errUnsupportedMedia
	}
	decoder := json.NewDecoder(bytes.NewReader(body))
	decoder.DisallowUnknownFields()
	var input struct{}
	if err := decoder.Decode(&input); err != nil {
		return errInvalidJSON
	}
	if err := decoder.Decode(&struct{}{}); !errors.Is(err, io.EOF) {
		return errInvalidJSON
	}
	return nil
}

func decodeRequiredJSON(response http.ResponseWriter, request *http.Request, destination any) error {
	if !hasJSONContentType(request) {
		return errUnsupportedMedia
	}
	request.Body = http.MaxBytesReader(response, request.Body, maxRequestBodyBytes)
	decoder := json.NewDecoder(request.Body)
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(destination); err != nil {
		var maxBytesError *http.MaxBytesError
		if errors.As(err, &maxBytesError) {
			return errRequestTooLarge
		}
		return errInvalidJSON
	}
	if err := decoder.Decode(&struct{}{}); !errors.Is(err, io.EOF) {
		var maxBytesError *http.MaxBytesError
		if errors.As(err, &maxBytesError) {
			return errRequestTooLarge
		}
		return errInvalidJSON
	}
	return nil
}

func hasJSONContentType(request *http.Request) bool {
	mediaType, _, err := mime.ParseMediaType(request.Header.Get("Content-Type"))
	return err == nil && mediaType == "application/json"
}

func writeDecodeError(response http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, errUnsupportedMedia):
		writeError(response, http.StatusUnsupportedMediaType, "unsupported_media_type", "Content-Type must be application/json")
	case errors.Is(err, errRequestTooLarge):
		writeError(response, http.StatusRequestEntityTooLarge, "request_too_large", "request body too large")
	default:
		writeError(response, http.StatusBadRequest, "invalid_request", "invalid JSON request")
	}
}

func (s *Server) writeBackupError(response http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, metadata.ErrNotFound), errors.Is(err, metadata.ErrInvalidIdentifier):
		writeError(response, http.StatusNotFound, "not_found", "resource not found")
	case errors.Is(err, backupservice.ErrBackupNotReady):
		writeError(response, http.StatusConflict, "backup_not_ready", "backup is not ready")
	case errors.Is(err, backupservice.ErrDatabaseUnavailable):
		writeError(response, http.StatusConflict, "database_unavailable", "database is not available")
	case errors.Is(err, backupservice.ErrBackupFailed):
		s.logger.Error("backup operation failed")
		writeError(response, http.StatusInternalServerError, "backup_failed", "backup operation failed")
	case errors.Is(err, backupservice.ErrRestoreFailed):
		s.logger.Error("restore operation failed")
		writeError(response, http.StatusInternalServerError, "restore_failed", "restore operation failed; review target status and retained safety backup")
	default:
		s.logger.Error("backup request failed")
		writeError(response, http.StatusInternalServerError, "internal_error", "internal server error")
	}
}
