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
	var databases []map[string]json.RawMessage
	if err := json.Unmarshal(databaseResponse.Body.Bytes(), &databases); err != nil {
		t.Fatalf("decode guest databases: %v", err)
	}
	if len(databases) != 1 {
		t.Fatalf("guest databases = %d, want 1", len(databases))
	}
	assertExactKeys(t, databases[0], "id", "displayName", "status")
	body := databaseResponse.Body.String()
	if strings.Contains(body, database.InternalName) || strings.Contains(body, "internalName") || strings.Contains(body, "roleName") {
		t.Fatalf("guest response exposed internal metadata: %s", body)
	}
	assertSafeResponse(t, body)
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
