package api

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"

	"github.com/conradevans/MiniBase/internal/metadata"
)

func testServer(t *testing.T) (*Server, *metadata.Store) {
	t.Helper()
	store, err := metadata.Open(context.Background(), filepath.Join(t.TempDir(), "minibase.db"))
	if err != nil {
		t.Fatalf("metadata.Open() error = %v", err)
	}
	t.Cleanup(func() {
		_ = store.Close()
	})
	return New(store, nil, "", slog.New(slog.NewTextHandler(io.Discard, nil))), store
}

func request(t *testing.T, server http.Handler, method, path string) *httptest.ResponseRecorder {
	t.Helper()
	recorder := httptest.NewRecorder()
	server.ServeHTTP(recorder, httptest.NewRequest(method, path, nil))
	return recorder
}

func TestHealth(t *testing.T) {
	server, _ := testServer(t)
	response := request(t, server, http.MethodGet, "/health")
	if response.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s", response.Code, response.Body.String())
	}

	var body map[string]any
	if err := json.Unmarshal(response.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if body["status"] != "ok" || body["metadataDatabase"] != "reachable" {
		t.Fatalf("body = %#v", body)
	}
	assertSafeResponse(t, response.Body.String())
}

func TestStatus(t *testing.T) {
	server, _ := testServer(t)
	response := request(t, server, http.MethodGet, "/api/v1/status")
	if response.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s", response.Code, response.Body.String())
	}

	var body map[string]any
	if err := json.Unmarshal(response.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if body["service"] != "minibase" || body["apiVersion"] != APIVersion {
		t.Fatalf("body = %#v", body)
	}
	if body["metadataDatabase"] != "reachable" || body["schemaVersion"] != float64(metadata.CurrentSchemaVersion) {
		t.Fatalf("body = %#v", body)
	}
	assertSafeResponse(t, response.Body.String())
}

func TestEmptyDatabaseListIsJSONArray(t *testing.T) {
	server, _ := testServer(t)
	response := request(t, server, http.MethodGet, "/api/v1/databases")
	if response.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s", response.Code, response.Body.String())
	}

	var body []metadata.Database
	if err := json.Unmarshal(response.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if body == nil || len(body) != 0 {
		t.Fatalf("body = %#v, want non-nil empty array", body)
	}
	if strings.TrimSpace(response.Body.String()) != "[]" {
		t.Fatalf("raw body = %q, want []", response.Body.String())
	}
}

func TestDatabaseDetail(t *testing.T) {
	server, store := testServer(t)
	database, err := store.CreateDatabaseMetadata(context.Background(), "Test Database")
	if err != nil {
		t.Fatalf("CreateDatabaseMetadata() error = %v", err)
	}

	response := request(t, server, http.MethodGet, "/api/v1/databases/"+database.ID)
	if response.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s", response.Code, response.Body.String())
	}

	var body metadata.Database
	if err := json.Unmarshal(response.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if body.ID != database.ID || body.DisplayName != database.DisplayName || body.InternalName != database.InternalName {
		t.Fatalf("body = %#v, want %#v", body, database)
	}
	var fields map[string]json.RawMessage
	if err := json.Unmarshal(response.Body.Bytes(), &fields); err != nil {
		t.Fatalf("decode response fields: %v", err)
	}
	assertExactKeys(t, fields, "id", "displayName", "internalName", "status", "createdAt", "updatedAt")
	assertSafeResponse(t, response.Body.String())
}

func TestMissingDatabaseAndUnknownRouteReturnJSON404(t *testing.T) {
	server, _ := testServer(t)
	for _, path := range []string{
		"/api/v1/databases/database_00000000000000000000000000000000",
		"/unknown",
		"/api/v1/databases/",
		"/api/v1/databases/id/extra",
	} {
		t.Run(path, func(t *testing.T) {
			response := request(t, server, http.MethodGet, path)
			if response.Code != http.StatusNotFound {
				t.Fatalf("status = %d, body = %s", response.Code, response.Body.String())
			}
			if response.Header().Get("Content-Type") != "application/json; charset=utf-8" {
				t.Fatalf("Content-Type = %q", response.Header().Get("Content-Type"))
			}
			assertSafeResponse(t, response.Body.String())
		})
	}
}

func TestMutationMethodsAreNotImplemented(t *testing.T) {
	server, _ := testServer(t)
	response := request(t, server, http.MethodPut, "/api/v1/databases")
	if response.Code != http.StatusMethodNotAllowed {
		t.Fatalf("status = %d, body = %s", response.Code, response.Body.String())
	}
	if response.Header().Get("Allow") != "GET, POST" {
		t.Fatalf("Allow = %q", response.Header().Get("Allow"))
	}
}

func TestHealthFailsSafelyWhenStoreIsUnavailable(t *testing.T) {
	server, store := testServer(t)
	if err := store.Close(); err != nil {
		t.Fatalf("Close() error = %v", err)
	}

	response := request(t, server, http.MethodGet, "/health")
	if response.Code != http.StatusServiceUnavailable {
		t.Fatalf("status = %d, body = %s", response.Code, response.Body.String())
	}
	if strings.Contains(strings.ToLower(response.Body.String()), "closed") {
		t.Fatalf("response leaked internal error: %s", response.Body.String())
	}
	assertSafeResponse(t, response.Body.String())
}

func assertSafeResponse(t *testing.T, body string) {
	t.Helper()
	lowerBody := strings.ToLower(body)
	for _, forbidden := range []string{
		"password",
		"credential",
		"connectionstring",
		"database_url",
		"jwt",
		"/srv/",
		".db",
		"stack trace",
	} {
		if strings.Contains(lowerBody, forbidden) {
			t.Fatalf("response contains forbidden field or detail %q: %s", forbidden, body)
		}
	}
}

func assertExactKeys(t *testing.T, fields map[string]json.RawMessage, expected ...string) {
	t.Helper()
	if len(fields) != len(expected) {
		t.Fatalf("response fields = %v, want exactly %v", fields, expected)
	}
	for _, key := range expected {
		if _, exists := fields[key]; !exists {
			t.Fatalf("response is missing expected field %q: %v", key, fields)
		}
		delete(fields, key)
	}
	if len(fields) != 0 {
		t.Fatalf("response contains unexpected fields: %v", fields)
	}
}

func TestCreateDatabase(t *testing.T) {
	server, _ := testServer(t)
	provisioner := &fakeProvisioner{database: metadata.Database{
		ID:           "database_0123456789abcdef0123456789abcdef",
		DisplayName:  "MyScheduler Production",
		InternalName: "mb_db_0123456789abcdef0123456789abcdef",
		RoleName:     "mb_role_0123456789abcdef0123456789abcdef",
		Status:       metadata.StatusReady,
	}}
	server.provisioner = provisioner

	response := jsonRequest(t, server, `{"displayName":"  MyScheduler Production  "}`)
	if response.Code != http.StatusCreated {
		t.Fatalf("status = %d, body = %s", response.Code, response.Body.String())
	}
	if provisioner.displayName != "  MyScheduler Production  " {
		t.Fatalf("provisioner display name = %q", provisioner.displayName)
	}
	var fields map[string]json.RawMessage
	if err := json.Unmarshal(response.Body.Bytes(), &fields); err != nil {
		t.Fatalf("decode response fields: %v", err)
	}
	assertExactKeys(t, fields, "id", "displayName", "internalName", "status", "createdAt", "updatedAt")
	assertSafeResponse(t, response.Body.String())
	if strings.Contains(response.Body.String(), "roleName") {
		t.Fatal("response exposed internal role name")
	}
}

func TestCreateDatabaseRejectsInvalidRequests(t *testing.T) {
	server, _ := testServer(t)
	server.provisioner = &fakeProvisioner{}

	tests := []struct {
		name        string
		contentType string
		body        string
		wantStatus  int
	}{
		{name: "malformed JSON", contentType: "application/json", body: `{"displayName":`, wantStatus: http.StatusBadRequest},
		{name: "unknown field", contentType: "application/json", body: `{"displayName":"Example","status":"ready"}`, wantStatus: http.StatusBadRequest},
		{name: "multiple values", contentType: "application/json", body: `{"displayName":"Example"} {}`, wantStatus: http.StatusBadRequest},
		{name: "missing display name", contentType: "application/json", body: `{}`, wantStatus: http.StatusBadRequest},
		{name: "blank display name", contentType: "application/json", body: `{"displayName":"   "}`, wantStatus: http.StatusBadRequest},
		{name: "oversized", contentType: "application/json", body: `{"displayName":"` + strings.Repeat("a", maxRequestBodyBytes) + `"}`, wantStatus: http.StatusRequestEntityTooLarge},
		{name: "wrong content type", contentType: "text/plain", body: `{"displayName":"Example"}`, wantStatus: http.StatusUnsupportedMediaType},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			request := httptest.NewRequest(http.MethodPost, "/api/v1/databases", strings.NewReader(test.body))
			request.Header.Set("Content-Type", test.contentType)
			response := httptest.NewRecorder()
			server.ServeHTTP(response, request)
			if response.Code != test.wantStatus {
				t.Fatalf("status = %d, want %d; body = %s", response.Code, test.wantStatus, response.Body.String())
			}
			assertSafeResponse(t, response.Body.String())
		})
	}
}

func TestCreateDatabaseProvisioningFailureIsSafe(t *testing.T) {
	server, _ := testServer(t)
	server.provisioner = &fakeProvisioner{err: errors.New("internal secret-bearing failure")}

	response := jsonRequest(t, server, `{"displayName":"Example"}`)
	if response.Code != http.StatusInternalServerError {
		t.Fatalf("status = %d, body = %s", response.Code, response.Body.String())
	}
	if strings.Contains(response.Body.String(), "secret-bearing") {
		t.Fatalf("response leaked provisioning error: %s", response.Body.String())
	}
	assertSafeResponse(t, response.Body.String())
}

func jsonRequest(t *testing.T, server http.Handler, body string) *httptest.ResponseRecorder {
	t.Helper()
	request := httptest.NewRequest(http.MethodPost, "/api/v1/databases", strings.NewReader(body))
	request.Header.Set("Content-Type", "application/json; charset=utf-8")
	response := httptest.NewRecorder()
	server.ServeHTTP(response, request)
	return response
}

type fakeProvisioner struct {
	database    metadata.Database
	err         error
	displayName string
}

func (provisioner *fakeProvisioner) ProvisionDatabase(_ context.Context, displayName string) (metadata.Database, error) {
	provisioner.displayName = displayName
	if provisioner.err != nil {
		return metadata.Database{}, provisioner.err
	}
	if _, err := metadata.NormalizeDisplayName(displayName); err != nil {
		return metadata.Database{}, err
	}
	return provisioner.database, nil
}
