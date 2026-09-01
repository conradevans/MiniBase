package api

import (
	"context"
	"errors"
	"net/http"
	"strings"

	"github.com/conradevans/MiniBase/internal/ids"
	"github.com/conradevans/MiniBase/internal/integrationauth"
	"github.com/conradevans/MiniBase/internal/metadata"
)

const miniDeployIntegrationPrefix = "/api/v1/integrations/minideploy/"

type attachmentStore interface {
	CreateAttachment(context.Context, string, string, string, string) (metadata.Attachment, error)
	GetAttachment(context.Context, string) (metadata.Attachment, error)
	GetAttachmentForConsumer(context.Context, string, string, string) (metadata.Attachment, error)
	ListAttachments(context.Context) ([]metadata.Attachment, error)
	ListAttachmentsForDatabase(context.Context, string) ([]metadata.Attachment, error)
	DeleteAttachment(context.Context, string) error
}

type credentialReader interface {
	Read(string) (string, error)
}

type integrationDatabaseResponse struct {
	ID          string                  `json:"id"`
	DisplayName string                  `json:"displayName"`
	Status      metadata.DatabaseStatus `json:"status"`
	Attached    bool                    `json:"attached"`
}

type createAttachmentRequest struct {
	DatabaseID  string `json:"databaseId"`
	ConsumerRef string `json:"consumerRef"`
	BindingName string `json:"bindingName"`
}

type bindingResponse struct {
	DatabaseID    string `json:"databaseId"`
	Engine        string `json:"engine"`
	Host          string `json:"host"`
	Port          int    `json:"port"`
	Database      string `json:"database"`
	Username      string `json:"username"`
	Password      string `json:"password"`
	DockerNetwork string `json:"dockerNetwork"`
}

func (s *Server) ConfigureMiniDeployIntegration(token []byte, credentials credentialReader) {
	s.integrationToken = append([]byte(nil), token...)
	s.credentials = credentials
}

func (s *Server) authorizeMiniDeploy(response http.ResponseWriter, request *http.Request) bool {
	if len(s.integrationToken) == 0 || !integrationauth.Authorized(request.Header.Get("Authorization"), s.integrationToken) {
		response.Header().Set("WWW-Authenticate", `Bearer realm="minibase-integration"`)
		writeError(response, http.StatusUnauthorized, "unauthorized", "integration authentication required")
		return false
	}
	return true
}

func (s *Server) routeMiniDeployIntegration(response http.ResponseWriter, request *http.Request) {
	if !s.authorizeMiniDeploy(response, request) {
		return
	}
	path := strings.TrimPrefix(request.URL.Path, miniDeployIntegrationPrefix)
	switch {
	case path == "databases" && request.Method == http.MethodGet:
		s.handleIntegrationListDatabases(response, request)
	case path == "databases" && request.Method == http.MethodPost:
		s.handleIntegrationCreateDatabase(response, request)
	case path == "attachments" && request.Method == http.MethodPost:
		s.handleIntegrationCreateAttachment(response, request)
	case strings.HasPrefix(path, "attachments/"):
		s.routeIntegrationAttachment(response, request, strings.TrimPrefix(path, "attachments/"))
	default:
		writeError(response, http.StatusNotFound, "not_found", "resource not found")
	}
}

func (s *Server) handleIntegrationListDatabases(response http.ResponseWriter, request *http.Request) {
	if s.attachments == nil {
		writeError(response, http.StatusServiceUnavailable, "service_unavailable", "service unavailable")
		return
	}
	databases, err := s.store.ListDatabases(request.Context())
	if err != nil {
		writeError(response, http.StatusInternalServerError, "internal_error", "internal server error")
		return
	}
	attachments, err := s.attachments.ListAttachments(request.Context())
	if err != nil {
		writeError(response, http.StatusInternalServerError, "internal_error", "internal server error")
		return
	}
	attached := make(map[string]bool, len(attachments))
	for _, attachment := range attachments {
		attached[attachment.DatabaseID] = true
	}
	result := make([]integrationDatabaseResponse, 0)
	for _, database := range databases {
		if database.Status != metadata.StatusReady {
			continue
		}
		result = append(result, integrationDatabaseResponse{
			ID: database.ID, DisplayName: database.DisplayName,
			Status: database.Status, Attached: attached[database.ID],
		})
	}
	writeJSON(response, http.StatusOK, result)
}

func (s *Server) handleIntegrationCreateDatabase(response http.ResponseWriter, request *http.Request) {
	if s.provisioner == nil {
		writeError(response, http.StatusServiceUnavailable, "service_unavailable", "service unavailable")
		return
	}
	var input createDatabaseRequest
	if err := decodeRequiredJSON(response, request, &input); err != nil {
		writeDecodeError(response, err)
		return
	}
	database, err := s.provisioner.ProvisionDatabase(request.Context(), input.DisplayName)
	if errors.Is(err, metadata.ErrInvalidDisplayName) {
		writeError(response, http.StatusBadRequest, "invalid_display_name", "displayName is required and must be at most 200 characters")
		return
	}
	if err != nil {
		s.logger.Error("integration database provisioning failed")
		writeError(response, http.StatusInternalServerError, "provisioning_failed", "database provisioning failed")
		return
	}
	writeJSON(response, http.StatusCreated, integrationDatabaseResponse{
		ID: database.ID, DisplayName: database.DisplayName, Status: database.Status,
	})
}

func (s *Server) handleIntegrationCreateAttachment(response http.ResponseWriter, request *http.Request) {
	if s.attachments == nil {
		writeError(response, http.StatusServiceUnavailable, "service_unavailable", "service unavailable")
		return
	}
	var input createAttachmentRequest
	if err := decodeRequiredJSON(response, request, &input); err != nil {
		writeDecodeError(response, err)
		return
	}
	attachment, err := s.attachments.CreateAttachment(request.Context(), input.DatabaseID,
		metadata.ConsumerTypeMiniDeploy, input.ConsumerRef, input.BindingName)
	if err != nil {
		s.writeIntegrationMetadataError(response, err)
		return
	}
	writeJSON(response, http.StatusCreated, attachment)
}

func (s *Server) routeIntegrationAttachment(response http.ResponseWriter, request *http.Request, suffix string) {
	parts := strings.Split(suffix, "/")
	if len(parts) == 1 && parts[0] != "" && request.Method == http.MethodDelete {
		if s.attachments == nil {
			writeError(response, http.StatusServiceUnavailable, "service_unavailable", "service unavailable")
			return
		}
		err := s.attachments.DeleteAttachment(request.Context(), parts[0])
		if err != nil && !errors.Is(err, metadata.ErrNotFound) {
			s.writeIntegrationMetadataError(response, err)
			return
		}
		response.WriteHeader(http.StatusNoContent)
		return
	}
	if len(parts) == 2 && parts[0] != "" && parts[1] == "binding" && request.Method == http.MethodGet {
		s.handleIntegrationBinding(response, request, parts[0])
		return
	}
	writeError(response, http.StatusNotFound, "not_found", "resource not found")
}

func (s *Server) handleIntegrationBinding(response http.ResponseWriter, request *http.Request, attachmentID string) {
	if s.attachments == nil || s.credentials == nil {
		writeError(response, http.StatusServiceUnavailable, "service_unavailable", "service unavailable")
		return
	}
	attachment, err := s.attachments.GetAttachment(request.Context(), attachmentID)
	if err != nil {
		s.writeIntegrationMetadataError(response, err)
		return
	}
	database, err := s.store.GetDatabase(request.Context(), attachment.DatabaseID)
	if err != nil {
		s.writeIntegrationMetadataError(response, err)
		return
	}
	if database.Status != metadata.StatusReady || !ids.ValidDatabaseInternalName(database.InternalName) || !ids.ValidRoleInternalName(database.RoleName) {
		writeError(response, http.StatusConflict, "database_not_ready", "database binding is unavailable")
		return
	}
	password, err := s.credentials.Read(database.ID)
	if err != nil {
		s.logger.Error("integration credential read failed", "database_id", database.ID)
		writeError(response, http.StatusServiceUnavailable, "binding_unavailable", "database binding is unavailable")
		return
	}
	response.Header().Set("Cache-Control", "no-store, max-age=0")
	response.Header().Set("Pragma", "no-cache")
	writeJSON(response, http.StatusOK, bindingResponse{
		DatabaseID: database.ID, Engine: "postgresql", Host: "minibase-postgres",
		Port: 5432, Database: database.InternalName, Username: database.RoleName,
		Password: password, DockerNetwork: "reactorlab-data",
	})
}

func (s *Server) writeIntegrationMetadataError(response http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, metadata.ErrInvalidIdentifier):
		writeError(response, http.StatusBadRequest, "invalid_request", "invalid integration resource")
	case errors.Is(err, metadata.ErrNotFound):
		writeError(response, http.StatusNotFound, "not_found", "integration resource not found")
	case errors.Is(err, metadata.ErrConflict):
		writeError(response, http.StatusConflict, "conflict", "integration resource conflicts with existing state")
	default:
		s.logger.Error("integration metadata operation failed")
		writeError(response, http.StatusInternalServerError, "internal_error", "internal server error")
	}
}
