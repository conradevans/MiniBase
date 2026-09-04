package api

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"mime"
	"net/http"

	"github.com/conradevans/MiniBase/internal/ids"
	"github.com/conradevans/MiniBase/internal/metadata"
	"github.com/conradevans/MiniBase/internal/minideploy"
)

type miniDeployLifecycleClient interface {
	ListDeployments(
		ctx context.Context,
	) ([]minideploy.Deployment, error)

	DetachDatabase(
		ctx context.Context,
		app string,
		databaseID string,
		attachmentID string,
	) error

	AttachDatabase(
		ctx context.Context,
		app string,
		databaseID string,
	) error
}

type attachDatabaseToDeploymentRequest struct {
	App string `json:"app"`
}

func (s *Server) ConfigureMiniDeployLifecycle(
	client miniDeployLifecycleClient,
) {
	s.miniDeployLifecycle = client
}

func (s *Server) handleMiniDeployDeployments(
	response http.ResponseWriter,
	request *http.Request,
) {
	if s.miniDeployLifecycle == nil {
		writeError(
			response,
			http.StatusServiceUnavailable,
			"minideploy_unavailable",
			"MiniDeploy lifecycle service is unavailable",
		)
		return
	}

	deployments, err :=
		s.miniDeployLifecycle.ListDeployments(
			request.Context(),
		)
	if err != nil {
		s.writeMiniDeployLifecycleError(
			response,
			err,
		)
		return
	}

	writeJSON(
		response,
		http.StatusOK,
		deployments,
	)
}

func (s *Server) handleDetachDatabase(
	response http.ResponseWriter,
	request *http.Request,
	databaseID string,
) {
	if !ids.ValidDatabaseID(databaseID) {
		writeError(
			response,
			http.StatusNotFound,
			"not_found",
			"database not found",
		)
		return
	}

	if s.miniDeployLifecycle == nil ||
		s.attachments == nil {

		writeError(
			response,
			http.StatusServiceUnavailable,
			"minideploy_unavailable",
			"MiniDeploy lifecycle service is unavailable",
		)
		return
	}

	if _, err := s.store.GetDatabase(
		request.Context(),
		databaseID,
	); errors.Is(err, metadata.ErrNotFound) {

		writeError(
			response,
			http.StatusNotFound,
			"not_found",
			"database not found",
		)
		return
	} else if err != nil {
		writeError(
			response,
			http.StatusInternalServerError,
			"internal_error",
			"internal server error",
		)
		return
	}

	attachments, err :=
		s.attachments.ListAttachmentsForDatabase(
			request.Context(),
			databaseID,
		)
	if err != nil {
		writeError(
			response,
			http.StatusInternalServerError,
			"internal_error",
			"internal server error",
		)
		return
	}

	if len(attachments) != 1 {
		writeError(
			response,
			http.StatusConflict,
			"database_not_attached",
			"database does not have one detachable MiniDeploy attachment",
		)
		return
	}

	attachment := attachments[0]

	if attachment.ConsumerType !=
		metadata.ConsumerTypeMiniDeploy ||
		attachment.BindingName !=
			metadata.BindingNamePrimary {

		writeError(
			response,
			http.StatusConflict,
			"database_not_attached",
			"database does not have one detachable MiniDeploy attachment",
		)
		return
	}

	if err := s.miniDeployLifecycle.DetachDatabase(
		request.Context(),
		attachment.ConsumerRef,
		databaseID,
		attachment.ID,
	); err != nil {
		s.writeMiniDeployLifecycleError(
			response,
			err,
		)
		return
	}

	remaining, err :=
		s.attachments.ListAttachmentsForDatabase(
			request.Context(),
			databaseID,
		)
	if err != nil || len(remaining) != 0 {
		s.logger.Error(
			"database detach completed with inconsistent attachment metadata",
			"database_id",
			databaseID,
		)

		writeError(
			response,
			http.StatusBadGateway,
			"lifecycle_inconsistent",
			"database attachment state could not be verified",
		)
		return
	}

	s.handleGetDatabase(
		response,
		request,
		databaseID,
	)
}

func (s *Server) handleAttachDatabase(
	response http.ResponseWriter,
	request *http.Request,
	databaseID string,
) {
	if !ids.ValidDatabaseID(databaseID) {
		writeError(
			response,
			http.StatusNotFound,
			"not_found",
			"database not found",
		)
		return
	}

	if s.miniDeployLifecycle == nil ||
		s.attachments == nil {

		writeError(
			response,
			http.StatusServiceUnavailable,
			"minideploy_unavailable",
			"MiniDeploy lifecycle service is unavailable",
		)
		return
	}

	database, err := s.store.GetDatabase(
		request.Context(),
		databaseID,
	)
	if errors.Is(err, metadata.ErrNotFound) {
		writeError(
			response,
			http.StatusNotFound,
			"not_found",
			"database not found",
		)
		return
	}
	if err != nil {
		writeError(
			response,
			http.StatusInternalServerError,
			"internal_error",
			"internal server error",
		)
		return
	}

	if database.Status != metadata.StatusReady {
		writeError(
			response,
			http.StatusConflict,
			"database_unavailable",
			"database is not ready for attachment",
		)
		return
	}

	existing, err :=
		s.attachments.ListAttachmentsForDatabase(
			request.Context(),
			databaseID,
		)
	if err != nil {
		writeError(
			response,
			http.StatusInternalServerError,
			"internal_error",
			"internal server error",
		)
		return
	}

	if len(existing) != 0 {
		writeError(
			response,
			http.StatusConflict,
			"database_attached",
			"database is already attached",
		)
		return
	}

	request.Body = http.MaxBytesReader(
		response,
		request.Body,
		maxRequestBodyBytes,
	)

	mediaType, _, err := mime.ParseMediaType(
		request.Header.Get("Content-Type"),
	)
	if err != nil ||
		mediaType != "application/json" {

		writeError(
			response,
			http.StatusUnsupportedMediaType,
			"unsupported_media_type",
			"Content-Type must be application/json",
		)
		return
	}

	decoder := json.NewDecoder(request.Body)
	decoder.DisallowUnknownFields()

	var input attachDatabaseToDeploymentRequest
	if err := decoder.Decode(&input); err != nil {
		var maxBytesError *http.MaxBytesError

		if errors.As(err, &maxBytesError) {
			writeError(
				response,
				http.StatusRequestEntityTooLarge,
				"request_too_large",
				"request body too large",
			)
			return
		}

		writeError(
			response,
			http.StatusBadRequest,
			"invalid_request",
			"invalid JSON request",
		)
		return
	}

	if err := decoder.Decode(
		&struct{}{},
	); !errors.Is(err, io.EOF) {

		writeError(
			response,
			http.StatusBadRequest,
			"invalid_request",
			"request must contain one JSON value",
		)
		return
	}

	if err := metadata.ValidateConsumerRef(
		input.App,
	); err != nil {

		writeError(
			response,
			http.StatusBadRequest,
			"invalid_deployment",
			"deployment is invalid",
		)
		return
	}

	deployments, err :=
		s.miniDeployLifecycle.ListDeployments(
			request.Context(),
		)
	if err != nil {
		s.writeMiniDeployLifecycleError(
			response,
			err,
		)
		return
	}

	var selected *minideploy.Deployment

	for index := range deployments {
		if deployments[index].App == input.App {
			selected = &deployments[index]
			break
		}
	}

	if selected == nil {
		writeError(
			response,
			http.StatusNotFound,
			"deployment_not_found",
			"deployment not found",
		)
		return
	}

	if !selected.Supported {
		writeError(
			response,
			http.StatusConflict,
			"deployment_unsupported",
			"deployment does not support MiniBase",
		)
		return
	}

	if selected.DatabaseAttached {
		writeError(
			response,
			http.StatusConflict,
			"deployment_attached",
			"deployment already has a database attached",
		)
		return
	}

	if !selected.DatabaseDetached &&
		selected.Status != "running" {

		writeError(
			response,
			http.StatusConflict,
			"deployment_unavailable",
			"deployment is not available for database attachment",
		)
		return
	}

	if err := s.miniDeployLifecycle.AttachDatabase(
		request.Context(),
		input.App,
		databaseID,
	); err != nil {
		s.writeMiniDeployLifecycleError(
			response,
			err,
		)
		return
	}

	attached, err :=
		s.attachments.ListAttachmentsForDatabase(
			request.Context(),
			databaseID,
		)
	if err != nil ||
		len(attached) != 1 ||
		attached[0].ConsumerType !=
			metadata.ConsumerTypeMiniDeploy ||
		attached[0].BindingName !=
			metadata.BindingNamePrimary ||
		attached[0].ConsumerRef != input.App {

		s.logger.Error(
			"database attach completed with inconsistent attachment metadata",
			"database_id",
			databaseID,
		)

		writeError(
			response,
			http.StatusBadGateway,
			"lifecycle_inconsistent",
			"database attachment state could not be verified",
		)
		return
	}

	s.handleGetDatabase(
		response,
		request,
		databaseID,
	)
}

func (s *Server) writeMiniDeployLifecycleError(
	response http.ResponseWriter,
	err error,
) {
	var httpError minideploy.HTTPError

	if errors.As(err, &httpError) {
		switch httpError.Status {
		case http.StatusNotFound:
			writeError(
				response,
				http.StatusNotFound,
				"deployment_not_found",
				"deployment not found",
			)

		case http.StatusConflict:
			writeError(
				response,
				http.StatusConflict,
				"database_lifecycle_conflict",
				"database attachment state changed",
			)

		case http.StatusUnprocessableEntity:
			writeError(
				response,
				http.StatusConflict,
				"deployment_unsupported",
				"deployment does not support MiniBase",
			)

		default:
			writeError(
				response,
				http.StatusBadGateway,
				"minideploy_unavailable",
				"MiniDeploy lifecycle operation failed",
			)
		}

		return
	}

	writeError(
		response,
		http.StatusBadGateway,
		"minideploy_unavailable",
		"MiniDeploy lifecycle service is unavailable",
	)
}
