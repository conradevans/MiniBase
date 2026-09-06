package metadata

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/conradevans/MiniBase/internal/ids"
)

const (
	MaxActivityDetailRunes  = 500
	maxActivityListSize     = 200
	defaultActivityListSize = 100
)

type ActivityType string

const (
	ActivityDatabaseCreate       ActivityType = "database_create"
	ActivityDatabaseDelete       ActivityType = "database_delete"
	ActivityBackupCreate         ActivityType = "backup_create"
	ActivityBackupRestoreNew     ActivityType = "backup_restore_new"
	ActivityBackupRestoreReplace ActivityType = "backup_restore_replace"
	ActivityAttachmentAttach     ActivityType = "attachment_attach"
	ActivityAttachmentDetach     ActivityType = "attachment_detach"
	ActivityAutomaticBackup      ActivityType = "automatic_backup"
	ActivityRetentionPrune       ActivityType = "retention_prune"
)

type ActivityOutcome string

const (
	ActivitySuccess ActivityOutcome = "success"
	ActivityFailure ActivityOutcome = "failure"
	ActivityBlocked ActivityOutcome = "blocked"
)

type ActivitySource string

const (
	ActivitySourceAdmin      ActivitySource = "admin"
	ActivitySourceMiniDeploy ActivitySource = "minideploy"
	ActivitySourceSystem     ActivitySource = "system"
)

var ErrInvalidActivity = errors.New("invalid activity event")

type ActivityEvent struct {
	DatabaseID          string          `json:"databaseId,omitempty"`
	DatabaseDisplayName string          `json:"databaseDisplayName,omitempty"`
	Type                ActivityType    `json:"type"`
	Outcome             ActivityOutcome `json:"outcome"`
	Source              ActivitySource  `json:"source"`
	Detail              string          `json:"detail"`
	CreatedAt           time.Time       `json:"createdAt"`
}

type ActivityEventInput struct {
	DatabaseID          string
	DatabaseDisplayName string
	Type                ActivityType
	Outcome             ActivityOutcome
	Source              ActivitySource
	Detail              string
}

func (value ActivityType) Valid() bool {
	switch value {
	case ActivityDatabaseCreate,
		ActivityDatabaseDelete,
		ActivityBackupCreate,
		ActivityBackupRestoreNew,
		ActivityBackupRestoreReplace,
		ActivityAttachmentAttach,
		ActivityAttachmentDetach,
		ActivityAutomaticBackup,
		ActivityRetentionPrune:
		return true
	default:
		return false
	}
}

func (value ActivityOutcome) Valid() bool {
	switch value {
	case ActivitySuccess, ActivityFailure, ActivityBlocked:
		return true
	default:
		return false
	}
}

func (value ActivitySource) Valid() bool {
	switch value {
	case ActivitySourceAdmin,
		ActivitySourceMiniDeploy,
		ActivitySourceSystem:
		return true
	default:
		return false
	}
}

func (s *Store) CreateActivity(
	ctx context.Context,
	input ActivityEventInput,
) (ActivityEvent, error) {
	if !input.Type.Valid() ||
		!input.Outcome.Valid() ||
		!input.Source.Valid() {
		return ActivityEvent{}, ErrInvalidActivity
	}

	if (input.DatabaseID == "") !=
		(input.DatabaseDisplayName == "") {
		return ActivityEvent{}, ErrInvalidActivity
	}

	databaseID := ""
	databaseDisplayName := ""

	if input.DatabaseID != "" {
		if !ids.ValidDatabaseID(input.DatabaseID) {
			return ActivityEvent{}, ErrInvalidIdentifier
		}

		normalizedName, err := NormalizeDisplayName(
			input.DatabaseDisplayName,
		)
		if err != nil {
			return ActivityEvent{}, ErrInvalidActivity
		}

		databaseID = input.DatabaseID
		databaseDisplayName = normalizedName
	}

	detail := strings.TrimSpace(input.Detail)
	if detail == "" ||
		utf8.RuneCountInString(detail) >
			MaxActivityDetailRunes {
		return ActivityEvent{}, ErrInvalidActivity
	}

	event := ActivityEvent{
		DatabaseID:          databaseID,
		DatabaseDisplayName: databaseDisplayName,
		Type:                input.Type,
		Outcome:             input.Outcome,
		Source:              input.Source,
		Detail:              detail,
		CreatedAt:           s.now().UTC(),
	}

	var storedDatabaseID any
	var storedDatabaseDisplayName any

	if event.DatabaseID != "" {
		storedDatabaseID = event.DatabaseID
		storedDatabaseDisplayName =
			event.DatabaseDisplayName
	}

	_, err := s.db.ExecContext(
		ctx,
		`INSERT INTO activity_events (
			database_id,
			database_display_name,
			event_type,
			outcome,
			source,
			detail,
			created_at
		) VALUES (?, ?, ?, ?, ?, ?, ?)`,
		storedDatabaseID,
		storedDatabaseDisplayName,
		event.Type,
		event.Outcome,
		event.Source,
		event.Detail,
		event.CreatedAt.Format(time.RFC3339Nano),
	)
	if err != nil {
		return ActivityEvent{},
			fmt.Errorf(
				"create activity metadata: %w",
				err,
			)
	}

	return event, nil
}

func (s *Store) ListActivity(
	ctx context.Context,
	limit int,
) ([]ActivityEvent, error) {
	return s.listActivity(
		ctx,
		"",
		nil,
		normalizeActivityLimit(limit),
	)
}

func (s *Store) ListActivityForDatabase(
	ctx context.Context,
	databaseID string,
	limit int,
) ([]ActivityEvent, error) {
	if !ids.ValidDatabaseID(databaseID) {
		return nil, ErrInvalidIdentifier
	}

	return s.listActivity(
		ctx,
		" WHERE database_id = ?",
		[]any{databaseID},
		normalizeActivityLimit(limit),
	)
}

func (s *Store) listActivity(
	ctx context.Context,
	whereClause string,
	args []any,
	limit int,
) ([]ActivityEvent, error) {
	queryArgs := append(
		append([]any(nil), args...),
		limit,
	)

	rows, err := s.db.QueryContext(
		ctx,
		`SELECT
			database_id,
			database_display_name,
			event_type,
			outcome,
			source,
			detail,
			created_at
		FROM activity_events`+
			whereClause+
			` ORDER BY created_at DESC, id DESC
			LIMIT ?`,
		queryArgs...,
	)
	if err != nil {
		return nil,
			fmt.Errorf(
				"list activity metadata: %w",
				err,
			)
	}
	defer rows.Close()

	events := make([]ActivityEvent, 0)
	for rows.Next() {
		event, err := scanActivity(rows)
		if err != nil {
			return nil,
				fmt.Errorf(
					"scan activity metadata: %w",
					err,
				)
		}
		events = append(events, event)
	}

	if err := rows.Err(); err != nil {
		return nil,
			fmt.Errorf(
				"iterate activity metadata: %w",
				err,
			)
	}

	return events, nil
}

func scanActivity(scanner rowScanner) (ActivityEvent, error) {
	var event ActivityEvent
	var databaseID sql.NullString
	var databaseDisplayName sql.NullString
	var createdAt string

	if err := scanner.Scan(
		&databaseID,
		&databaseDisplayName,
		&event.Type,
		&event.Outcome,
		&event.Source,
		&event.Detail,
		&createdAt,
	); err != nil {
		return ActivityEvent{}, err
	}

	if databaseID.Valid {
		event.DatabaseID = databaseID.String
	}

	if databaseDisplayName.Valid {
		event.DatabaseDisplayName =
			databaseDisplayName.String
	}

	parsedTime, err := time.Parse(
		time.RFC3339Nano,
		createdAt,
	)
	if err != nil {
		return ActivityEvent{}, err
	}

	event.CreatedAt = parsedTime

	if !event.Type.Valid() ||
		!event.Outcome.Valid() ||
		!event.Source.Valid() {
		return ActivityEvent{}, ErrInvalidActivity
	}

	return event, nil
}

func normalizeActivityLimit(limit int) int {
	if limit <= 0 {
		return defaultActivityListSize
	}
	if limit > maxActivityListSize {
		return maxActivityListSize
	}
	return limit
}
