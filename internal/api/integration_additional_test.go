package api

import (
	"context"
	"encoding/json"
	"net/http"
	"strings"
	"testing"

	"github.com/conradevans/MiniBase/internal/metadata"
)

func TestMiniDeployIntegrationCreateDatabaseDelegatesAndReturnsSafeDTO(t *testing.T) {
	server, _, _, _, _, _, _ := integrationFixture(t)
	provisioner := &fakeProvisioner{database: metadata.Database{
		ID:           "database_aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa",
		DisplayName:  "Created for MiniDeploy",
		InternalName: "mb_db_aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa",
		RoleName:     "mb_role_aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa",
		Status:       metadata.StatusReady,
	}}
	server.provisioner = provisioner

	response := integrationRequest(server, http.MethodPost, miniDeployIntegrationPrefix+"databases", `{"displayName":"Created for MiniDeploy"}`, "Bearer "+integrationTestToken)
	if response.Code != http.StatusCreated {
		t.Fatalf("status = %d", response.Code)
	}
	if provisioner.displayName != "Created for MiniDeploy" {
		t.Fatalf("provisioner display name = %q", provisioner.displayName)
	}
	var fields map[string]json.RawMessage
	if err := json.Unmarshal(response.Body.Bytes(), &fields); err != nil {
		t.Fatal(err)
	}
	assertExactKeys(t, fields, "id", "displayName", "status", "attached")
	assertSafeResponse(t, response.Body.String())
}

func TestMiniDeployIntegrationRejectsNonReadyDatabaseAndMissingCredential(t *testing.T) {
	server, store, credentials, ready, _, _, _ := integrationFixture(t)
	authorization := "Bearer " + integrationTestToken
	notReady, err := store.CreateDatabaseMetadata(context.Background(), "Not Ready")
	if err != nil {
		t.Fatal(err)
	}
	response := integrationRequest(server, http.MethodPost, miniDeployIntegrationPrefix+"attachments", `{"databaseId":"`+notReady.ID+`","consumerRef":"not-ready","bindingName":"primary"}`, authorization)
	if response.Code != http.StatusConflict {
		t.Fatalf("non-ready attachment status = %d", response.Code)
	}

	response = integrationRequest(server, http.MethodPost, miniDeployIntegrationPrefix+"attachments", `{"databaseId":"`+ready.ID+`","consumerRef":"missing-credential","bindingName":"primary"}`, authorization)
	if response.Code != http.StatusCreated {
		t.Fatalf("attachment status = %d", response.Code)
	}
	var attachment metadata.Attachment
	if err := json.Unmarshal(response.Body.Bytes(), &attachment); err != nil {
		t.Fatal(err)
	}
	if err := credentials.Delete(ready.ID); err != nil {
		t.Fatal(err)
	}
	response = integrationRequest(server, http.MethodGet, miniDeployIntegrationPrefix+"attachments/"+attachment.ID+"/binding", "", authorization)
	if response.Code != http.StatusServiceUnavailable {
		t.Fatalf("missing credential binding status = %d", response.Code)
	}
	assertSafeResponse(t, response.Body.String())
}

func TestOrdinaryAPIsExposeSafeAttachmentMetadataOnly(t *testing.T) {
	server, store, _, database, password, _, _ := integrationFixture(t)
	attachment, err := store.CreateAttachment(context.Background(), database.ID, metadata.ConsumerTypeMiniDeploy, "scheduler", metadata.BindingNamePrimary)
	if err != nil {
		t.Fatal(err)
	}

	admin := integrationRequest(server, http.MethodGet, "/api/v1/databases/"+database.ID, "", "")
	if admin.Code != http.StatusOK {
		t.Fatalf("admin detail status = %d", admin.Code)
	}
	adminBody := admin.Body.String()
	for _, forbidden := range []string{password, "password", "databaseUrl", "DATABASE_URL", "credentialPath", "mb_role_"} {
		if strings.Contains(adminBody, forbidden) {
			t.Fatalf("ordinary Admin response contains forbidden binding material marker %q", forbidden)
		}
	}
	var detail struct {
		Attachments []map[string]json.RawMessage `json:"attachments"`
	}
	if err := json.Unmarshal(admin.Body.Bytes(), &detail); err != nil {
		t.Fatal(err)
	}
	if len(detail.Attachments) != 1 {
		t.Fatalf("attachment count = %d", len(detail.Attachments))
	}
	var returnedID string
	if err := json.Unmarshal(detail.Attachments[0]["id"], &returnedID); err != nil || returnedID != attachment.ID {
		t.Fatal("ordinary Admin response omitted the safe attachment ID")
	}
	assertExactKeys(t, detail.Attachments[0], "id", "databaseId", "consumerType", "consumerRef", "bindingName", "createdAt", "updatedAt")

	guest := integrationRequest(server, http.MethodGet, "/api/v1/guest/databases", "", "")
	if guest.Code != http.StatusOK {
		t.Fatalf("guest status = %d", guest.Code)
	}
	if strings.Contains(guest.Body.String(), "attachment") || strings.Contains(guest.Body.String(), password) {
		t.Fatal("Guest response exposed attachment or credential data")
	}
	var databases []map[string]json.RawMessage
	if err := json.Unmarshal(guest.Body.Bytes(), &databases); err != nil {
		t.Fatal(err)
	}
	if len(databases) != 1 {
		t.Fatalf("guest database count = %d", len(databases))
	}
	assertExactKeys(t, databases[0], "id", "displayName", "status")
}
