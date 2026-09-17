package api

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/conradevans/MiniBase/internal/metadata"
)

func TestUpdateGuestVisibilityPersistsOnlyVisibility(t *testing.T) {
	server, store := testServer(t)
	database, err := store.CreateDatabaseMetadata(context.Background(), "Private Database")
	if err != nil {
		t.Fatalf("CreateDatabaseMetadata() error = %v", err)
	}
	database, err = store.UpdateDatabaseStatus(context.Background(), database.ID, metadata.StatusReady)
	if err != nil {
		t.Fatalf("UpdateDatabaseStatus() error = %v", err)
	}
	server.provisioner = &fakeProvisioner{}
	manager := &fakeBackupManager{}
	server.backups = manager

	response := apiJSONRequest(
		t,
		server,
		http.MethodPatch,
		"/api/v1/databases/"+database.ID+"/visibility",
		`{"guestVisible":true}`,
	)
	if response.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s", response.Code, response.Body.String())
	}
	var fields map[string]json.RawMessage
	if err := json.Unmarshal(response.Body.Bytes(), &fields); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	assertExactKeys(t, fields, "id", "guestVisible")

	reloaded, err := store.GetDatabase(context.Background(), database.ID)
	if err != nil {
		t.Fatalf("GetDatabase() error = %v", err)
	}
	if !reloaded.GuestVisible || reloaded.DisplayName != database.DisplayName ||
		reloaded.InternalName != database.InternalName || reloaded.Status != database.Status ||
		!reloaded.CreatedAt.Equal(database.CreatedAt) {
		t.Fatalf("visibility update changed unrelated metadata: before=%#v after=%#v", database, reloaded)
	}
	if manager.createdFor != "" || manager.restoreNewName != "" || manager.restoreTarget != "" || manager.deletedID != "" {
		t.Fatalf("visibility update triggered backup lifecycle behavior: %#v", manager)
	}

	hidden := apiJSONRequest(
		t,
		server,
		http.MethodPatch,
		"/api/v1/databases/"+database.ID+"/visibility",
		`{"guestVisible":false}`,
	)
	if hidden.Code != http.StatusOK {
		t.Fatalf("hide status = %d, body = %s", hidden.Code, hidden.Body.String())
	}
	reloaded, err = store.GetDatabase(context.Background(), database.ID)
	if err != nil || reloaded.GuestVisible {
		t.Fatalf("hidden database = %#v, error = %v", reloaded, err)
	}
}

func TestUpdateGuestVisibilityRejectsInvalidRequestsWithoutMutation(t *testing.T) {
	tests := []struct {
		name        string
		contentType string
		body        string
		wantStatus  int
	}{
		{name: "missing field", contentType: "application/json", body: `{}`, wantStatus: http.StatusBadRequest},
		{name: "null field", contentType: "application/json", body: `{"guestVisible":null}`, wantStatus: http.StatusBadRequest},
		{name: "unknown field", contentType: "application/json", body: `{"guestVisible":true,"other":false}`, wantStatus: http.StatusBadRequest},
		{name: "wrong type", contentType: "application/json", body: `{"guestVisible":"true"}`, wantStatus: http.StatusBadRequest},
		{name: "malformed", contentType: "application/json", body: `{"guestVisible":`, wantStatus: http.StatusBadRequest},
		{name: "multiple values", contentType: "application/json", body: `{"guestVisible":true} {}`, wantStatus: http.StatusBadRequest},
		{name: "empty", contentType: "application/json", body: ``, wantStatus: http.StatusBadRequest},
		{name: "wrong content type", contentType: "text/plain", body: `{"guestVisible":true}`, wantStatus: http.StatusUnsupportedMediaType},
		{name: "oversized", contentType: "application/json", body: `{"guestVisible":false,"padding":"` + strings.Repeat("x", maxRequestBodyBytes) + `"}`, wantStatus: http.StatusRequestEntityTooLarge},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			server, store := testServer(t)
			database, err := store.CreateDatabaseMetadata(context.Background(), "Unchanged")
			if err != nil {
				t.Fatal(err)
			}
			request := httptest.NewRequest(
				http.MethodPatch,
				"/api/v1/databases/"+database.ID+"/visibility",
				strings.NewReader(test.body),
			)
			request.Header.Set("Content-Type", test.contentType)
			response := httptest.NewRecorder()
			server.ServeHTTP(response, request)
			if response.Code != test.wantStatus {
				t.Fatalf("status = %d, want %d; body = %s", response.Code, test.wantStatus, response.Body.String())
			}
			reloaded, err := store.GetDatabase(context.Background(), database.ID)
			if err != nil || reloaded.GuestVisible {
				t.Fatalf("invalid request changed visibility: database=%#v error=%v", reloaded, err)
			}
			assertSafeResponse(t, response.Body.String())
		})
	}
}

func TestUpdateGuestVisibilityRouteErrors(t *testing.T) {
	server, _ := testServer(t)
	validMissingID := "database_00000000000000000000000000000000"

	missing := apiJSONRequest(t, server, http.MethodPatch, "/api/v1/databases/"+validMissingID+"/visibility", `{"guestVisible":true}`)
	if missing.Code != http.StatusNotFound {
		t.Fatalf("missing status = %d, body = %s", missing.Code, missing.Body.String())
	}
	invalid := apiJSONRequest(t, server, http.MethodPatch, "/api/v1/databases/not-an-id/visibility", `{"guestVisible":true}`)
	if invalid.Code != http.StatusNotFound {
		t.Fatalf("invalid ID status = %d, body = %s", invalid.Code, invalid.Body.String())
	}
	wrongMethod := request(t, server, http.MethodGet, "/api/v1/databases/"+validMissingID+"/visibility")
	if wrongMethod.Code != http.StatusMethodNotAllowed || wrongMethod.Header().Get("Allow") != http.MethodPatch {
		t.Fatalf("wrong method status = %d, Allow = %q", wrongMethod.Code, wrongMethod.Header().Get("Allow"))
	}
}
