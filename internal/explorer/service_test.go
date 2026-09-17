package explorer

import (
	"context"
	"errors"
	"testing"

	"github.com/conradevans/MiniBase/internal/metadata"
)

const (
	testDatabaseID   = "database_0123456789abcdef0123456789abcdef"
	testDatabaseName = "mb_db_0123456789abcdef0123456789abcdef"
	testRoleName     = "mb_role_0123456789abcdef0123456789abcdef"
)

type fakeMetadataStore struct {
	database metadata.Database
	err      error
}

func (store fakeMetadataStore) GetDatabase(context.Context, string) (metadata.Database, error) {
	return store.database, store.err
}

type fakePostgres struct {
	catalog      Catalog
	description  Description
	page         RowsPage
	lastDatabase string
	lastRole     string
	lastSchema   string
	lastObject   string
	lastLimit    int
	lastOffset   int
}

func (postgres *fakePostgres) ListObjects(_ context.Context, databaseName, roleName string) (Catalog, error) {
	postgres.lastDatabase = databaseName
	postgres.lastRole = roleName
	return postgres.catalog, nil
}

func (postgres *fakePostgres) DescribeObject(
	_ context.Context,
	databaseName string,
	roleName string,
	schemaName string,
	objectName string,
) (Description, error) {
	postgres.lastDatabase = databaseName
	postgres.lastRole = roleName
	postgres.lastSchema = schemaName
	postgres.lastObject = objectName
	return postgres.description, nil
}

func (postgres *fakePostgres) BrowseRows(
	_ context.Context,
	databaseName string,
	roleName string,
	schemaName string,
	objectName string,
	limit int,
	offset int,
) (RowsPage, error) {
	postgres.lastDatabase = databaseName
	postgres.lastRole = roleName
	postgres.lastSchema = schemaName
	postgres.lastObject = objectName
	postgres.lastLimit = limit
	postgres.lastOffset = offset
	return postgres.page, nil
}

func readyService(postgres *fakePostgres) *Service {
	return NewService(fakeMetadataStore{database: metadata.Database{
		ID:           testDatabaseID,
		InternalName: testDatabaseName,
		RoleName:     testRoleName,
		Status:       metadata.StatusReady,
	}}, postgres)
}

func TestServiceRejectsUnknownAndNonReadyDatabases(t *testing.T) {
	missing := NewService(fakeMetadataStore{err: metadata.ErrNotFound}, &fakePostgres{})
	if _, err := missing.ListObjects(context.Background(), testDatabaseID); !errors.Is(err, ErrDatabaseNotFound) {
		t.Fatalf("missing database error = %v", err)
	}
	if _, err := missing.ListObjects(context.Background(), "invalid"); !errors.Is(err, ErrDatabaseNotFound) {
		t.Fatalf("invalid database error = %v", err)
	}

	for _, status := range []metadata.DatabaseStatus{
		metadata.StatusMetadataOnly,
		metadata.StatusProvisioning,
		metadata.StatusError,
	} {
		service := NewService(fakeMetadataStore{database: metadata.Database{
			ID: testDatabaseID, InternalName: testDatabaseName, Status: status,
		}}, &fakePostgres{})
		if _, err := service.ListObjects(context.Background(), testDatabaseID); !errors.Is(err, ErrDatabaseNotReady) {
			t.Fatalf("status %q error = %v", status, err)
		}
	}
}

func TestServiceAcceptsUnusualIdentifiersAndBoundedPages(t *testing.T) {
	postgres := &fakePostgres{page: RowsPage{Rows: make([][]Cell, 0)}}
	service := readyService(postgres)
	if _, err := service.DescribeObject(
		context.Background(),
		testDatabaseID,
		"Odd Schema",
		"select table!",
	); err != nil {
		t.Fatalf("DescribeObject() error = %v", err)
	}
	if postgres.lastSchema != "Odd Schema" || postgres.lastObject != "select table!" {
		t.Fatalf("object = %q.%q", postgres.lastSchema, postgres.lastObject)
	}
	if postgres.lastDatabase != testDatabaseName || postgres.lastRole != testRoleName {
		t.Fatalf("execution identity = database %q role %q", postgres.lastDatabase, postgres.lastRole)
	}

	for _, limit := range []int{25, 50, 100} {
		if _, err := service.BrowseRows(
			context.Background(),
			testDatabaseID,
			"public",
			"users",
			limit,
			25,
		); err != nil {
			t.Fatalf("limit %d error = %v", limit, err)
		}
		if postgres.lastLimit != limit || postgres.lastOffset != 25 {
			t.Fatalf("page = %d/%d", postgres.lastLimit, postgres.lastOffset)
		}
	}
}

func TestServiceRejectsUnsafeObjectAndPaginationInputs(t *testing.T) {
	service := readyService(&fakePostgres{})
	for _, test := range []struct {
		schema string
		object string
		limit  int
		offset int
	}{
		{"", "users", 50, 0},
		{"public", "", 50, 0},
		{"public", "users", 1, 0},
		{"public", "users", 101, 0},
		{"public", "users", 50, -1},
		{"public", string(make([]byte, 64)), 50, 0},
	} {
		if _, err := service.BrowseRows(
			context.Background(),
			testDatabaseID,
			test.schema,
			test.object,
			test.limit,
			test.offset,
		); !errors.Is(err, ErrInvalidRequest) {
			t.Fatalf("input %#v error = %v", test, err)
		}
	}
}
