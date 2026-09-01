package api

import (
	"bytes"
	"context"
	"encoding/json"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/conradevans/MiniBase/internal/metadata"
	"github.com/conradevans/MiniBase/internal/secrets"
)

const integrationTestToken = "0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef"

func integrationRequest(server http.Handler, method, path, body, authorization string) *httptest.ResponseRecorder {
	request := httptest.NewRequest(method, path, strings.NewReader(body))
	if body != "" {
		request.Header.Set("Content-Type", "application/json")
	}
	if authorization != "" {
		request.Header.Set("Authorization", authorization)
	}
	response := httptest.NewRecorder()
	server.ServeHTTP(response, request)
	return response
}

func integrationFixture(t *testing.T) (*Server, *metadata.Store, *secrets.Store, metadata.Database, string, string, *bytes.Buffer) {
	t.Helper()
	store, _ := openAPIStore(t)
	credentialRoot := filepath.Join(t.TempDir(), "credentials")
	credentials, err := secrets.New(credentialRoot)
	if err != nil {
		t.Fatal(err)
	}
	database, err := store.CreateProvisioningDatabase(context.Background(), metadata.ProvisioningDatabase{
		ID:           "database_0123456789abcdef0123456789abcdef",
		DisplayName:  "Scheduler Production",
		InternalName: "mb_db_0123456789abcdef0123456789abcdef",
		RoleName:     "mb_role_0123456789abcdef0123456789abcdef",
	})
	if err != nil {
		t.Fatal(err)
	}
	database, err = store.UpdateDatabaseStatus(context.Background(), database.ID, metadata.StatusReady)
	if err != nil {
		t.Fatal(err)
	}
	password, err := credentials.Create(database.ID)
	if err != nil {
		t.Fatal(err)
	}
	logs := &bytes.Buffer{}
	server := New(store, nil, nil, "", slog.New(slog.NewTextHandler(logs, nil)))
	server.ConfigureMiniDeployIntegration([]byte(integrationTestToken), credentials)
	return server, store, credentials, database, password, credentialRoot, logs
}

func TestMiniDeployIntegrationAuthenticationAndSafeList(t *testing.T) {
	server, _, _, database, _, _, logs := integrationFixture(t)
	wrong := "Bearer " + strings.Repeat("x", len(integrationTestToken))
	for name, authorization := range map[string]string{
		"missing":   "",
		"malformed": "Basic invalid",
		"incorrect": wrong,
	} {
		t.Run(name, func(t *testing.T) {
			response := integrationRequest(server, http.MethodGet, miniDeployIntegrationPrefix+"databases", "", authorization)
			if response.Code != http.StatusUnauthorized {
				t.Fatalf("status = %d", response.Code)
			}
			assertSafeResponse(t, response.Body.String())
		})
	}
	if strings.Contains(logs.String(), integrationTestToken) || strings.Contains(logs.String(), strings.TrimPrefix(wrong, "Bearer ")) {
		t.Fatal("integration authentication value entered logs")
	}
	response := integrationRequest(server, http.MethodGet, miniDeployIntegrationPrefix+"databases", "", "Bearer "+integrationTestToken)
	if response.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s", response.Code, response.Body.String())
	}
	var databases []map[string]json.RawMessage
	if err := json.Unmarshal(response.Body.Bytes(), &databases); err != nil {
		t.Fatal(err)
	}
	if len(databases) != 1 {
		t.Fatalf("database count = %d", len(databases))
	}
	rawID := databases[0]["id"]
	assertExactKeys(t, databases[0], "id", "displayName", "status", "attached")
	var returnedID string
	if err := json.Unmarshal(rawID, &returnedID); err != nil {
		t.Fatalf("decode safe database ID: %v", err)
	}
	if returnedID != database.ID {
		t.Fatalf("safe database ID = %q, want %q", returnedID, database.ID)
	}
}

func TestMiniDeployIntegrationAttachmentBindingAndDetach(t *testing.T) {
	server, store, credentials, database, password, _, _ := integrationFixture(t)
	authorization := "Bearer " + integrationTestToken
	body := `{"databaseId":"` + database.ID + `","consumerRef":"scheduler","bindingName":"primary"}`
	response := integrationRequest(server, http.MethodPost, miniDeployIntegrationPrefix+"attachments", body, authorization)
	if response.Code != http.StatusCreated {
		t.Fatalf("attachment status = %d, body = %s", response.Code, response.Body.String())
	}
	var attachment metadata.Attachment
	if err := json.Unmarshal(response.Body.Bytes(), &attachment); err != nil {
		t.Fatal(err)
	}
	duplicate := integrationRequest(server, http.MethodPost, miniDeployIntegrationPrefix+"attachments", body, authorization)
	if duplicate.Code != http.StatusConflict {
		t.Fatalf("duplicate status = %d", duplicate.Code)
	}
	binding := integrationRequest(server, http.MethodGet, miniDeployIntegrationPrefix+"attachments/"+attachment.ID+"/binding", "", authorization)
	if binding.Code != http.StatusOK {
		t.Fatalf("binding status = %d, body = %s", binding.Code, binding.Body.String())
	}
	if binding.Header().Get("Cache-Control") != "no-store, max-age=0" {
		t.Fatalf("binding Cache-Control = %q", binding.Header().Get("Cache-Control"))
	}
	var fields map[string]json.RawMessage
	if err := json.Unmarshal(binding.Body.Bytes(), &fields); err != nil {
		t.Fatal(err)
	}
	assertExactKeys(t, fields, "databaseId", "engine", "host", "port", "database", "username", "password", "dockerNetwork")
	var decoded bindingResponse
	if err := json.Unmarshal(binding.Body.Bytes(), &decoded); err != nil {
		t.Fatal(err)
	}
	if decoded.Password != password || decoded.DatabaseID != database.ID || decoded.DockerNetwork != "reactorlab-data" {
		t.Fatal("binding response did not match secure metadata")
	}
	deleteResponse := integrationRequest(server, http.MethodDelete, miniDeployIntegrationPrefix+"attachments/"+attachment.ID, "", authorization)
	if deleteResponse.Code != http.StatusNoContent {
		t.Fatalf("delete status = %d", deleteResponse.Code)
	}
	if _, err := store.GetDatabase(context.Background(), database.ID); err != nil {
		t.Fatalf("detach deleted database: %v", err)
	}
	if exists, err := credentials.Exists(database.ID); err != nil || !exists {
		t.Fatalf("detach deleted credential: exists=%v err=%v", exists, err)
	}
	secondDelete := integrationRequest(server, http.MethodDelete, miniDeployIntegrationPrefix+"attachments/"+attachment.ID, "", authorization)
	if secondDelete.Code != http.StatusNoContent {
		t.Fatalf("idempotent delete status = %d", secondDelete.Code)
	}
}

func TestMiniDeployBindingRejectsUnsafeCredentialPermissions(t *testing.T) {
	server, store, _, database, password, credentialRoot, logs := integrationFixture(t)
	attachment, err := store.CreateAttachment(context.Background(), database.ID, metadata.ConsumerTypeMiniDeploy, "scheduler", metadata.BindingNamePrimary)
	if err != nil {
		t.Fatal(err)
	}
	credentialPath := filepath.Join(credentialRoot, database.ID, "password")
	if err := os.Chmod(credentialPath, 0o644); err != nil {
		t.Fatal(err)
	}
	response := integrationRequest(server, http.MethodGet, miniDeployIntegrationPrefix+"attachments/"+attachment.ID+"/binding", "", "Bearer "+integrationTestToken)
	if response.Code != http.StatusServiceUnavailable {
		t.Fatalf("status = %d", response.Code)
	}
	if strings.Contains(response.Body.String(), password) || strings.Contains(logs.String(), password) {
		t.Fatal("unsafe credential error leaked password")
	}
}

func openAPIStore(t *testing.T) (*metadata.Store, string) {
	t.Helper()
	path := filepath.Join(t.TempDir(), "minibase.db")
	store, err := metadata.Open(context.Background(), path)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = store.Close() })
	return store, path
}
