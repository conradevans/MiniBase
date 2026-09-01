package api

import (
	"context"
	"encoding/json"
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
	return New(store, slog.New(slog.NewTextHandler(io.Discard, nil))), store
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
	response := request(t, server, http.MethodPost, "/api/v1/databases")
	if response.Code != http.StatusMethodNotAllowed {
		t.Fatalf("status = %d, body = %s", response.Code, response.Body.String())
	}
	if response.Header().Get("Allow") != http.MethodGet {
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
