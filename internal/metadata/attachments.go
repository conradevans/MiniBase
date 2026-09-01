package metadata

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"regexp"
	"time"

	"github.com/conradevans/MiniBase/internal/ids"
)

const (
	ConsumerTypeMiniDeploy = "minideploy"
	BindingNamePrimary     = "primary"
)

var canonicalConsumerPattern = regexp.MustCompile(`^[a-z0-9](?:[a-z0-9-]{0,61}[a-z0-9])?$`)

type Attachment struct {
	ID           string    `json:"id"`
	DatabaseID   string    `json:"databaseId"`
	ConsumerType string    `json:"consumerType"`
	ConsumerRef  string    `json:"consumerRef"`
	BindingName  string    `json:"bindingName"`
	CreatedAt    time.Time `json:"createdAt"`
	UpdatedAt    time.Time `json:"updatedAt"`
}

func newAttachmentID() (string, error) { return ids.AttachmentID() }

func ValidateConsumerRef(value string) error {
	if !canonicalConsumerPattern.MatchString(value) {
		return fmt.Errorf("invalid canonical consumer reference")
	}
	return nil
}

func validateAttachmentInput(databaseID, consumerType, consumerRef, bindingName string) error {
	if !ids.ValidDatabaseID(databaseID) {
		return ErrInvalidIdentifier
	}
	if consumerType != ConsumerTypeMiniDeploy || bindingName != BindingNamePrimary {
		return ErrInvalidIdentifier
	}
	return ValidateConsumerRef(consumerRef)
}

func (s *Store) CreateAttachment(ctx context.Context, databaseID, consumerType, consumerRef, bindingName string) (Attachment, error) {
	if err := validateAttachmentInput(databaseID, consumerType, consumerRef, bindingName); err != nil {
		return Attachment{}, err
	}
	database, err := s.GetDatabase(ctx, databaseID)
	if err != nil {
		return Attachment{}, err
	}
	if database.Status != StatusReady {
		return Attachment{}, fmt.Errorf("%w: database is not ready", ErrConflict)
	}
	id, err := s.newAttachmentID()
	if err != nil {
		return Attachment{}, fmt.Errorf("generate attachment ID: %w", err)
	}
	if !ids.ValidAttachmentID(id) {
		return Attachment{}, ErrInvalidIdentifier
	}
	now := s.now().UTC()
	attachment := Attachment{
		ID: id, DatabaseID: databaseID, ConsumerType: consumerType,
		ConsumerRef: consumerRef, BindingName: bindingName,
		CreatedAt: now, UpdatedAt: now,
	}
	_, err = s.db.ExecContext(ctx,
		`INSERT INTO attachments (id,database_id,consumer_type,consumer_ref,binding_name,created_at,updated_at) VALUES (?,?,?,?,?,?,?)`,
		id, databaseID, consumerType, consumerRef, bindingName,
		now.Format(time.RFC3339Nano), now.Format(time.RFC3339Nano),
	)
	if err != nil {
		return Attachment{}, classifyWriteError("create attachment", err)
	}
	return attachment, nil
}

func (s *Store) GetAttachment(ctx context.Context, id string) (Attachment, error) {
	if !ids.ValidAttachmentID(id) {
		return Attachment{}, ErrInvalidIdentifier
	}
	return scanAttachmentResult(s.db.QueryRowContext(ctx,
		`SELECT id,database_id,consumer_type,consumer_ref,binding_name,created_at,updated_at FROM attachments WHERE id=?`, id), "get attachment")
}

func (s *Store) GetAttachmentForConsumer(ctx context.Context, consumerType, consumerRef, bindingName string) (Attachment, error) {
	if consumerType != ConsumerTypeMiniDeploy || bindingName != BindingNamePrimary {
		return Attachment{}, ErrInvalidIdentifier
	}
	if err := ValidateConsumerRef(consumerRef); err != nil {
		return Attachment{}, err
	}
	return scanAttachmentResult(s.db.QueryRowContext(ctx,
		`SELECT id,database_id,consumer_type,consumer_ref,binding_name,created_at,updated_at FROM attachments WHERE consumer_type=? AND consumer_ref=? AND binding_name=?`,
		consumerType, consumerRef, bindingName), "get consumer attachment")
}

func (s *Store) ListAttachments(ctx context.Context) ([]Attachment, error) {
	return s.listAttachments(ctx, "", nil)
}

func (s *Store) ListAttachmentsForDatabase(ctx context.Context, databaseID string) ([]Attachment, error) {
	if !ids.ValidDatabaseID(databaseID) {
		return nil, ErrInvalidIdentifier
	}
	return s.listAttachments(ctx, " WHERE database_id = ?", []any{databaseID})
}

func (s *Store) listAttachments(ctx context.Context, where string, args []any) ([]Attachment, error) {
	rows, err := s.db.QueryContext(ctx,
		`SELECT id,database_id,consumer_type,consumer_ref,binding_name,created_at,updated_at FROM attachments`+where+` ORDER BY created_at,id`, args...)
	if err != nil {
		return nil, fmt.Errorf("list attachments: %w", err)
	}
	defer rows.Close()
	result := make([]Attachment, 0)
	for rows.Next() {
		attachment, err := scanAttachment(rows)
		if err != nil {
			return nil, fmt.Errorf("scan attachment: %w", err)
		}
		result = append(result, attachment)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate attachments: %w", err)
	}
	return result, nil
}

func (s *Store) DeleteAttachment(ctx context.Context, id string) error {
	if !ids.ValidAttachmentID(id) {
		return ErrInvalidIdentifier
	}
	result, err := s.db.ExecContext(ctx, "DELETE FROM attachments WHERE id=?", id)
	if err != nil {
		return fmt.Errorf("delete attachment: %w", err)
	}
	count, err := result.RowsAffected()
	if err != nil {
		return fmt.Errorf("read attachment deletion result: %w", err)
	}
	if count == 0 {
		return fmt.Errorf("%w: attachment", ErrNotFound)
	}
	return nil
}

func scanAttachmentResult(row *sql.Row, operation string) (Attachment, error) {
	attachment, err := scanAttachment(row)
	if errors.Is(err, sql.ErrNoRows) {
		return Attachment{}, fmt.Errorf("%w: attachment", ErrNotFound)
	}
	if err != nil {
		return Attachment{}, fmt.Errorf("%s: %w", operation, err)
	}
	return attachment, nil
}

func scanAttachment(scanner rowScanner) (Attachment, error) {
	var attachment Attachment
	var createdAt, updatedAt string
	if err := scanner.Scan(&attachment.ID, &attachment.DatabaseID, &attachment.ConsumerType,
		&attachment.ConsumerRef, &attachment.BindingName, &createdAt, &updatedAt); err != nil {
		return Attachment{}, err
	}
	var err error
	attachment.CreatedAt, err = time.Parse(time.RFC3339Nano, createdAt)
	if err != nil {
		return Attachment{}, err
	}
	attachment.UpdatedAt, err = time.Parse(time.RFC3339Nano, updatedAt)
	return attachment, err
}
