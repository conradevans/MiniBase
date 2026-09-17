package api

import (
	"context"
	"encoding/json"
	"net/http"
	"strings"
	"testing"
)

func TestGuestEndpointsReturnExactSafeDTOs(t *testing.T) {
	server, store := testServer(t)
	database, err := store.CreateDatabaseMetadata(context.Background(), "Guest-visible Database")
	if err != nil {
		t.Fatalf("CreateDatabaseMetadata() error = %v", err)
	}
	if _, err := store.UpdateGuestVisibility(context.Background(), database.ID, true); err != nil {
		t.Fatalf("UpdateGuestVisibility() error = %v", err)
	}
	hidden, err := store.CreateDatabaseMetadata(context.Background(), "Hidden Secret Database")
	if err != nil {
		t.Fatalf("CreateDatabaseMetadata(hidden) error = %v", err)
	}

	statusResponse := request(t, server, http.MethodGet, "/api/v1/guest/status")
	if statusResponse.Code != http.StatusOK {
		t.Fatalf("guest status = %d, body = %s", statusResponse.Code, statusResponse.Body.String())
	}
	var statusFields map[string]json.RawMessage
	if err := json.Unmarshal(statusResponse.Body.Bytes(), &statusFields); err != nil {
		t.Fatalf("decode guest status: %v", err)
	}
	assertExactKeys(t, statusFields, "service", "status")
	assertSafeResponse(t, statusResponse.Body.String())

	databaseResponse := request(t, server, http.MethodGet, "/api/v1/guest/databases")
	if databaseResponse.Code != http.StatusOK {
		t.Fatalf("guest databases = %d, body = %s", databaseResponse.Code, databaseResponse.Body.String())
	}
	var payload struct {
		Summary   map[string]json.RawMessage   `json:"summary"`
		Databases []map[string]json.RawMessage `json:"databases"`
	}
	if err := json.Unmarshal(databaseResponse.Body.Bytes(), &payload); err != nil {
		t.Fatalf("decode guest databases: %v", err)
	}
	var envelope map[string]json.RawMessage
	if err := json.Unmarshal(databaseResponse.Body.Bytes(), &envelope); err != nil {
		t.Fatalf("decode guest envelope: %v", err)
	}
	assertExactKeys(t, envelope, "summary", "databases")
	assertExactKeys(t, payload.Summary, "total", "showing", "hidden")
	if len(payload.Databases) != 1 {
		t.Fatalf("guest databases = %d, want 1", len(payload.Databases))
	}
	assertExactKeys(t, payload.Databases[0], "id", "displayName", "status")
	var summary guestDatabaseSummary
	if err := json.Unmarshal(databaseResponse.Body.Bytes(), &struct {
		Summary *guestDatabaseSummary `json:"summary"`
	}{Summary: &summary}); err != nil {
		t.Fatalf("decode guest summary: %v", err)
	}
	if summary != (guestDatabaseSummary{Total: 2, Showing: 1, Hidden: 1}) {
		t.Fatalf("summary = %#v", summary)
	}
	body := databaseResponse.Body.String()
	if strings.Contains(body, database.InternalName) || strings.Contains(body, hidden.InternalName) ||
		strings.Contains(body, hidden.DisplayName) || strings.Contains(body, hidden.ID) ||
		strings.Contains(body, "internalName") || strings.Contains(body, "roleName") ||
		strings.Contains(body, "guestVisible") {
		t.Fatalf("guest response exposed internal metadata: %s", body)
	}
	assertSafeResponse(t, body)
}

func TestGuestDatabasesStableWhenNoneAreVisible(t *testing.T) {
	server, store := testServer(t)
	if _, err := store.CreateDatabaseMetadata(context.Background(), "Hidden by default"); err != nil {
		t.Fatalf("CreateDatabaseMetadata() error = %v", err)
	}

	response := request(t, server, http.MethodGet, "/api/v1/guest/databases")
	if response.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s", response.Code, response.Body.String())
	}
	var payload guestDatabasesResponse
	if err := json.Unmarshal(response.Body.Bytes(), &payload); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if payload.Databases == nil || len(payload.Databases) != 0 {
		t.Fatalf("databases = %#v, want non-nil empty array", payload.Databases)
	}
	if payload.Summary != (guestDatabaseSummary{Total: 1, Showing: 0, Hidden: 1}) {
		t.Fatalf("summary = %#v", payload.Summary)
	}
}

func TestGuestDatabasesSummaryWhenAllAreVisible(t *testing.T) {
	server, store := testServer(t)
	for _, name := range []string{"First", "Second"} {
		database, err := store.CreateDatabaseMetadata(context.Background(), name)
		if err != nil {
			t.Fatalf("CreateDatabaseMetadata() error = %v", err)
		}
		if _, err := store.UpdateGuestVisibility(context.Background(), database.ID, true); err != nil {
			t.Fatalf("UpdateGuestVisibility() error = %v", err)
		}
	}

	response := request(t, server, http.MethodGet, "/api/v1/guest/databases")
	var payload guestDatabasesResponse
	if err := json.Unmarshal(response.Body.Bytes(), &payload); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if len(payload.Databases) != 2 || payload.Summary != (guestDatabaseSummary{Total: 2, Showing: 2, Hidden: 0}) {
		t.Fatalf("payload = %#v", payload)
	}
}

func TestGuestEndpointsAreReadOnly(t *testing.T) {
	server, _ := testServer(t)
	for _, path := range []string{"/api/v1/guest/status", "/api/v1/guest/databases"} {
		for _, method := range []string{http.MethodPost, http.MethodPut, http.MethodPatch, http.MethodDelete} {
			t.Run(method+" "+path, func(t *testing.T) {
				response := request(t, server, method, path)
				if response.Code != http.StatusMethodNotAllowed {
					t.Fatalf("status = %d, body = %s", response.Code, response.Body.String())
				}
				if response.Header().Get("Allow") != http.MethodGet {
					t.Fatalf("Allow = %q, want GET", response.Header().Get("Allow"))
				}
			})
		}
	}
}

func TestUnknownGuestRouteRemainsJSON404(t *testing.T) {
	server, _ := testServer(t)
	response := request(t, server, http.MethodGet, "/api/v1/guest/credentials")
	if response.Code != http.StatusNotFound {
		t.Fatalf("status = %d, body = %s", response.Code, response.Body.String())
	}
	if response.Header().Get("Content-Type") != "application/json; charset=utf-8" {
		t.Fatalf("Content-Type = %q", response.Header().Get("Content-Type"))
	}
	assertSafeResponse(t, response.Body.String())
}
