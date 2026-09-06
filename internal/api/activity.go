package api

import (
	"context"
	"errors"
	"net/http"

	"github.com/conradevans/MiniBase/internal/ids"
	"github.com/conradevans/MiniBase/internal/metadata"
)

const activityListLimit = 100

type activityStore interface {
	CreateActivity(
		context.Context,
		metadata.ActivityEventInput,
	) (metadata.ActivityEvent, error)

	ListActivity(
		context.Context,
		int,
	) ([]metadata.ActivityEvent, error)

	ListActivityForDatabase(
		context.Context,
		string,
		int,
	) ([]metadata.ActivityEvent, error)
}

func (s *Server) handleListActivity(
	response http.ResponseWriter,
	request *http.Request,
) {
	if s.activity == nil {
		writeError(
			response,
			http.StatusServiceUnavailable,
			"service_unavailable",
			"service unavailable",
		)
		return
	}

	events, err := s.activity.ListActivity(
		request.Context(),
		activityListLimit,
	)
	if err != nil {
		s.logger.Error("activity metadata listing failed")
		writeError(
			response,
			http.StatusInternalServerError,
			"internal_error",
			"internal server error",
		)
		return
	}

	writeJSON(
		response,
		http.StatusOK,
		events,
	)
}

func (s *Server) handleListDatabaseActivity(
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

	if s.activity == nil {
		writeError(
			response,
			http.StatusServiceUnavailable,
			"service_unavailable",
			"service unavailable",
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
		s.logger.Error(
			"database lookup for activity failed",
			"database_id",
			databaseID,
		)
		writeError(
			response,
			http.StatusInternalServerError,
			"internal_error",
			"internal server error",
		)
		return
	}

	events, err :=
		s.activity.ListActivityForDatabase(
			request.Context(),
			databaseID,
			activityListLimit,
		)
	if err != nil {
		s.logger.Error(
			"database activity metadata listing failed",
			"database_id",
			databaseID,
		)
		writeError(
			response,
			http.StatusInternalServerError,
			"internal_error",
			"internal server error",
		)
		return
	}

	writeJSON(
		response,
		http.StatusOK,
		events,
	)
}

func (s *Server) recordActivity(
	ctx context.Context,
	input metadata.ActivityEventInput,
) {
	if s.activity == nil {
		return
	}

	if _, err := s.activity.CreateActivity(
		ctx,
		input,
	); err != nil {
		s.logger.Error(
			"activity recording failed",
			"type",
			string(input.Type),
			"outcome",
			string(input.Outcome),
			"source",
			string(input.Source),
		)
	}
}

func (s *Server) recordDatabaseActivity(
	ctx context.Context,
	database metadata.Database,
	eventType metadata.ActivityType,
	outcome metadata.ActivityOutcome,
	source metadata.ActivitySource,
	detail string,
) {
	s.recordActivity(
		ctx,
		metadata.ActivityEventInput{
			DatabaseID:          database.ID,
			DatabaseDisplayName: database.DisplayName,
			Type:                eventType,
			Outcome:             outcome,
			Source:              source,
			Detail:              detail,
		},
	)
}

func (s *Server) recordDatabaseActivityByID(
	ctx context.Context,
	databaseID string,
	eventType metadata.ActivityType,
	outcome metadata.ActivityOutcome,
	source metadata.ActivitySource,
	detail string,
) {
	database, err := s.store.GetDatabase(
		ctx,
		databaseID,
	)
	if err == nil {
		s.recordDatabaseActivity(
			ctx,
			database,
			eventType,
			outcome,
			source,
			detail,
		)
		return
	}

	s.recordActivity(
		ctx,
		metadata.ActivityEventInput{
			Type:    eventType,
			Outcome: outcome,
			Source:  source,
			Detail:  detail,
		},
	)
}
