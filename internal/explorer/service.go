package explorer

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"unicode/utf8"

	"github.com/conradevans/MiniBase/internal/ids"
	"github.com/conradevans/MiniBase/internal/metadata"
)

const (
	DefaultPageSize = 50
	MaxPageSize     = 100
	maxIdentifier   = 63
)

var (
	ErrDatabaseNotFound = errors.New("database not found")
	ErrDatabaseNotReady = errors.New("database is not ready")
	ErrInvalidRequest   = errors.New("invalid explorer request")
)

type MetadataStore interface {
	GetDatabase(context.Context, string) (metadata.Database, error)
}

type Postgres interface {
	ListObjects(context.Context, string, string) (Catalog, error)
	DescribeObject(context.Context, string, string, string, string) (Description, error)
	BrowseRows(context.Context, string, string, string, string, int, int) (RowsPage, error)
}

type Service struct {
	metadata MetadataStore
	postgres Postgres
}

func NewService(metadataStore MetadataStore, postgres Postgres) *Service {
	return &Service{metadata: metadataStore, postgres: postgres}
}

func (s *Service) ListObjects(ctx context.Context, databaseID string) (Catalog, error) {
	database, err := s.readyDatabase(ctx, databaseID)
	if err != nil {
		return Catalog{}, err
	}
	return s.postgres.ListObjects(ctx, database.InternalName, database.RoleName)
}

func (s *Service) DescribeObject(
	ctx context.Context,
	databaseID string,
	schemaName string,
	objectName string,
) (Description, error) {
	if !validObjectIdentifier(schemaName) || !validObjectIdentifier(objectName) {
		return Description{}, ErrInvalidRequest
	}
	database, err := s.readyDatabase(ctx, databaseID)
	if err != nil {
		return Description{}, err
	}
	return s.postgres.DescribeObject(
		ctx,
		database.InternalName,
		database.RoleName,
		schemaName,
		objectName,
	)
}

func (s *Service) BrowseRows(
	ctx context.Context,
	databaseID string,
	schemaName string,
	objectName string,
	limit int,
	offset int,
) (RowsPage, error) {
	if !validObjectIdentifier(schemaName) ||
		!validObjectIdentifier(objectName) ||
		!validPageSize(limit) ||
		offset < 0 {
		return RowsPage{}, ErrInvalidRequest
	}
	database, err := s.readyDatabase(ctx, databaseID)
	if err != nil {
		return RowsPage{}, err
	}
	return s.postgres.BrowseRows(
		ctx,
		database.InternalName,
		database.RoleName,
		schemaName,
		objectName,
		limit,
		offset,
	)
}

func (s *Service) readyDatabase(
	ctx context.Context,
	databaseID string,
) (metadata.Database, error) {
	if !ids.ValidDatabaseID(databaseID) {
		return metadata.Database{}, ErrDatabaseNotFound
	}
	database, err := s.metadata.GetDatabase(ctx, databaseID)
	if errors.Is(err, metadata.ErrNotFound) || errors.Is(err, metadata.ErrInvalidIdentifier) {
		return metadata.Database{}, ErrDatabaseNotFound
	}
	if err != nil {
		return metadata.Database{}, fmt.Errorf("read explorer database metadata: %w", err)
	}
	if database.Status != metadata.StatusReady {
		return metadata.Database{}, ErrDatabaseNotReady
	}
	return database, nil
}

func validObjectIdentifier(value string) bool {
	return value != "" &&
		len(value) <= maxIdentifier &&
		utf8.ValidString(value) &&
		!strings.ContainsRune(value, '\x00')
}

func validPageSize(limit int) bool {
	switch limit {
	case 25, DefaultPageSize, MaxPageSize:
		return true
	default:
		return false
	}
}
