package api

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	backupservice "github.com/conradevans/MiniBase/internal/backups"
	"github.com/conradevans/MiniBase/internal/metadata"
)

func TestBackupListDetailAndDatabaseListUseSafeResponses(t *testing.T) {
	server, store := testServer(t)
	database, err := store.CreateDatabaseMetadata(context.Background(), "Example Database")
	if err != nil {
		t.Fatal(err)
	}
	completed := time.Date(2026, 9, 1, 12, 1, 0, 0, time.UTC)
	backup := metadata.Backup{
		ID: "backup_0123456789abcdef0123456789abcdef", DatabaseID: database.ID,
		Kind: metadata.BackupKindManual, Status: metadata.BackupStatusReady,
		SizeBytes: 1234, CreatedAt: completed.Add(-time.Minute), CompletedAt: &completed,
	}
	manager := &fakeBackupManager{backups: []metadata.Backup{backup}, backup: backup}
	server.backups = manager

	for _, path := range []string{
		"/api/v1/backups",
		"/api/v1/backups/" + backup.ID,
		"/api/v1/databases/" + database.ID + "/backups",
	} {
		response := request(t, server, http.MethodGet, path)
		if response.Code != http.StatusOK {
			t.Fatalf("%s status=%d body=%s", path, response.Code, response.Body.String())
		}
		assertSafeResponse(t, response.Body.String())
		if strings.Contains(response.Body.String(), "internalName") || strings.Contains(response.Body.String(), "path") {
			t.Fatalf("%s exposed internal data: %s", path, response.Body.String())
		}
		var object map[string]json.RawMessage
		if path == "/api/v1/backups/"+backup.ID {
			if err := json.Unmarshal(response.Body.Bytes(), &object); err != nil {
				t.Fatal(err)
			}
		} else {
			var list []map[string]json.RawMessage
			if err := json.Unmarshal(response.Body.Bytes(), &list); err != nil || len(list) != 1 {
				t.Fatalf("decode list=%v value=%#v", err, list)
			}
			object = list[0]
		}
		assertExactKeys(t, object, "id", "databaseId", "databaseDisplayName", "kind", "status", "sizeBytes", "createdAt", "completedAt")
	}
}

func TestEmptyBackupListIsJSONArray(t *testing.T) {
	server, _ := testServer(t)
	server.backups = &fakeBackupManager{}
	response := request(t, server, http.MethodGet, "/api/v1/backups")
	if response.Code != http.StatusOK || strings.TrimSpace(response.Body.String()) != "[]" {
		t.Fatalf("status=%d body=%q", response.Code, response.Body.String())
	}
}

func TestManualBackupAcceptsNoBodyOrEmptyObject(t *testing.T) {
	for _, body := range []string{"", "{}"} {
		t.Run(body, func(t *testing.T) {
			server, store := testServer(t)
			database, _ := store.CreateDatabaseMetadata(context.Background(), "Example")
			manager := &fakeBackupManager{backup: metadata.Backup{
				ID: "backup_0123456789abcdef0123456789abcdef", DatabaseID: database.ID,
				Kind: metadata.BackupKindManual, Status: metadata.BackupStatusReady, SizeBytes: 10,
			}}
			server.backups = manager
			request := httptest.NewRequest(http.MethodPost, "/api/v1/databases/"+database.ID+"/backups", strings.NewReader(body))
			if body != "" {
				request.Header.Set("Content-Type", "application/json")
			}
			response := httptest.NewRecorder()
			server.ServeHTTP(response, request)
			if response.Code != http.StatusCreated {
				t.Fatalf("status=%d body=%s", response.Code, response.Body.String())
			}
			if manager.createdFor != database.ID {
				t.Fatalf("createdFor=%q", manager.createdFor)
			}
			assertSafeResponse(t, response.Body.String())
		})
	}
}

func TestRestoreNewAndReplaceHaveExplicitModes(t *testing.T) {
	server, _ := testServer(t)
	manager := &fakeBackupManager{
		database: metadata.Database{
			ID: "database_aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa", DisplayName: "Recovered",
			InternalName: "mb_db_bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb", Status: metadata.StatusReady,
		},
	}
	server.backups = manager
	backupID := "backup_0123456789abcdef0123456789abcdef"

	newResponse := apiJSONRequest(t, server, http.MethodPost, "/api/v1/backups/"+backupID+"/restore", `{"mode":"new","displayName":"Recovered"}`)
	if newResponse.Code != http.StatusCreated || manager.restoreNewName != "Recovered" {
		t.Fatalf("new status=%d body=%s name=%q", newResponse.Code, newResponse.Body.String(), manager.restoreNewName)
	}
	assertSafeResponse(t, newResponse.Body.String())

	targetID := "database_11111111111111111111111111111111"
	replaceResponse := apiJSONRequest(t, server, http.MethodPost, "/api/v1/backups/"+backupID+"/restore", `{"mode":"replace","targetDatabaseId":"`+targetID+`"}`)
	if replaceResponse.Code != http.StatusOK || manager.restoreTarget != targetID {
		t.Fatalf("replace status=%d body=%s target=%q", replaceResponse.Code, replaceResponse.Body.String(), manager.restoreTarget)
	}
	assertSafeResponse(t, replaceResponse.Body.String())
}

func TestBackupMutationsRejectMalformedOrAmbiguousBodiesWithoutSideEffects(t *testing.T) {
	tests := []struct {
		name, path, contentType, body string
	}{
		{name: "backup unknown field", path: "/api/v1/databases/database_11111111111111111111111111111111/backups", contentType: "application/json", body: `{"path":"/tmp/x"}`},
		{name: "restore missing mode", path: "/api/v1/backups/backup_11111111111111111111111111111111/restore", contentType: "application/json", body: `{"displayName":"X"}`},
		{name: "restore unknown field", path: "/api/v1/backups/backup_11111111111111111111111111111111/restore", contentType: "application/json", body: `{"mode":"new","displayName":"X","path":"/tmp/x"}`},
		{name: "new with target", path: "/api/v1/backups/backup_11111111111111111111111111111111/restore", contentType: "application/json", body: `{"mode":"new","displayName":"X","targetDatabaseId":"database_11111111111111111111111111111111"}`},
		{name: "replace with display", path: "/api/v1/backups/backup_11111111111111111111111111111111/restore", contentType: "application/json", body: `{"mode":"replace","displayName":"X","targetDatabaseId":"database_11111111111111111111111111111111"}`},
		{name: "wrong content type", path: "/api/v1/backups/backup_11111111111111111111111111111111/restore", contentType: "text/plain", body: `{"mode":"new","displayName":"X"}`},
		{name: "multiple values", path: "/api/v1/backups/backup_11111111111111111111111111111111/restore", contentType: "application/json", body: `{"mode":"new","displayName":"X"} {}`},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			server, _ := testServer(t)
			manager := &fakeBackupManager{}
			server.backups = manager
			request := httptest.NewRequest(http.MethodPost, test.path, strings.NewReader(test.body))
			request.Header.Set("Content-Type", test.contentType)
			response := httptest.NewRecorder()
			server.ServeHTTP(response, request)
			if response.Code < 400 || response.Code >= 500 {
				t.Fatalf("status=%d body=%s", response.Code, response.Body.String())
			}
			if manager.createdFor != "" || manager.restoreNewName != "" || manager.restoreTarget != "" {
				t.Fatal("malformed request triggered a mutation")
			}
			assertSafeResponse(t, response.Body.String())
		})
	}
}

func TestBackupMissingAndOperationalErrorsAreSafe(t *testing.T) {
	server, _ := testServer(t)
	manager := &fakeBackupManager{err: metadata.ErrNotFound}
	server.backups = manager
	response := request(t, server, http.MethodGet, "/api/v1/backups/backup_11111111111111111111111111111111")
	if response.Code != http.StatusNotFound {
		t.Fatalf("status=%d body=%s", response.Code, response.Body.String())
	}

	manager.err = errors.New("sensitive internal archive detail")
	response = request(t, server, http.MethodGet, "/api/v1/backups")
	if response.Code != http.StatusInternalServerError || strings.Contains(response.Body.String(), "sensitive") {
		t.Fatalf("status=%d body=%s", response.Code, response.Body.String())
	}
	assertSafeResponse(t, response.Body.String())
}

func TestDeleteDatabaseResponseContract(t *testing.T) {
	databaseID := "database_11111111111111111111111111111111"
	tests := []struct {
		name       string
		id         string
		err        error
		wantStatus int
		wantCode   string
		wantCalled bool
	}{
		{name: "success", id: databaseID, wantStatus: http.StatusNoContent, wantCalled: true},
		{name: "attached", id: databaseID, err: backupservice.ErrDatabaseAttached, wantStatus: http.StatusConflict, wantCode: "database_attached", wantCalled: true},
		{name: "unavailable", id: databaseID, err: backupservice.ErrDatabaseUnavailable, wantStatus: http.StatusConflict, wantCode: "database_unavailable", wantCalled: true},
		{name: "missing", id: databaseID, err: metadata.ErrNotFound, wantStatus: http.StatusNotFound, wantCode: "not_found", wantCalled: true},
		{name: "invalid", id: "invalid", wantStatus: http.StatusNotFound, wantCode: "not_found"},
		{name: "failure", id: databaseID, err: errors.New("raw PostgreSQL /srv/secret detail"), wantStatus: http.StatusInternalServerError, wantCode: "deletion_failed", wantCalled: true},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			server, _ := testServer(t)
			manager := &fakeBackupManager{err: test.err}
			server.backups = manager
			response := request(t, server, http.MethodDelete, "/api/v1/databases/"+test.id)
			if response.Code != test.wantStatus {
				t.Fatalf("status=%d want=%d body=%s", response.Code, test.wantStatus, response.Body.String())
			}
			if (manager.deletedID != "") != test.wantCalled {
				t.Fatalf("deletedID=%q wantCalled=%v", manager.deletedID, test.wantCalled)
			}
			if test.wantStatus == http.StatusNoContent {
				if response.Body.Len() != 0 {
					t.Fatalf("204 response body=%q", response.Body.String())
				}
				return
			}
			var body errorResponse
			if err := json.Unmarshal(response.Body.Bytes(), &body); err != nil {
				t.Fatal(err)
			}
			if body.Error.Code != test.wantCode {
				t.Fatalf("error code=%q want=%q", body.Error.Code, test.wantCode)
			}
			if strings.Contains(response.Body.String(), "PostgreSQL") || strings.Contains(response.Body.String(), "/srv/") {
				t.Fatalf("response exposed internal deletion error: %s", response.Body.String())
			}
			assertSafeResponse(t, response.Body.String())
		})
	}
}

func apiJSONRequest(t *testing.T, server http.Handler, method, path, body string) *httptest.ResponseRecorder {
	t.Helper()
	request := httptest.NewRequest(method, path, strings.NewReader(body))
	request.Header.Set("Content-Type", "application/json")
	response := httptest.NewRecorder()
	server.ServeHTTP(response, request)
	return response
}

type fakeBackupManager struct {
	backups        []metadata.Backup
	backup         metadata.Backup
	database       metadata.Database
	err            error
	createdFor     string
	restoreNewName string
	restoreTarget  string
	deletedID      string
}

func (manager *fakeBackupManager) ListBackups(context.Context) ([]metadata.Backup, error) {
	if manager.err != nil {
		return nil, manager.err
	}
	return append([]metadata.Backup(nil), manager.backups...), nil
}
func (manager *fakeBackupManager) GetBackup(context.Context, string) (metadata.Backup, error) {
	return manager.backup, manager.err
}
func (manager *fakeBackupManager) ListBackupsForDatabase(context.Context, string) ([]metadata.Backup, error) {
	return append([]metadata.Backup(nil), manager.backups...), manager.err
}
func (manager *fakeBackupManager) CreateBackup(_ context.Context, databaseID string) (metadata.Backup, error) {
	manager.createdFor = databaseID
	return manager.backup, manager.err
}
func (manager *fakeBackupManager) RestoreAsNewDatabase(_ context.Context, _ string, displayName string) (metadata.Database, error) {
	manager.restoreNewName = displayName
	return manager.database, manager.err
}
func (manager *fakeBackupManager) RestoreInPlace(_ context.Context, _ string, targetDatabaseID string) (metadata.Database, error) {
	manager.restoreTarget = targetDatabaseID
	return manager.database, manager.err
}
func (manager *fakeBackupManager) DeleteDatabase(_ context.Context, databaseID string) error {
	manager.deletedID = databaseID
	return manager.err
}
