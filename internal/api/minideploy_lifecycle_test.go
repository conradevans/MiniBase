package api

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/conradevans/MiniBase/internal/metadata"
	"github.com/conradevans/MiniBase/internal/minideploy"
)

type fakeMiniDeployLifecycleClient struct {
	deployments []minideploy.Deployment
	listErr     error
	attachErr   error
	detachErr   error

	attachedApp      string
	attachedDatabase string

	detachedApp        string
	detachedDatabase   string
	detachedAttachment string

	onAttach func()
	onDetach func()
}

func (client *fakeMiniDeployLifecycleClient) ListDeployments(
	context.Context,
) ([]minideploy.Deployment, error) {
	return client.deployments, client.listErr
}

func (client *fakeMiniDeployLifecycleClient) AttachDatabase(
	_ context.Context,
	app string,
	databaseID string,
) error {
	client.attachedApp = app
	client.attachedDatabase = databaseID

	if client.attachErr != nil {
		return client.attachErr
	}

	if client.onAttach != nil {
		client.onAttach()
	}

	return nil
}

func (client *fakeMiniDeployLifecycleClient) DetachDatabase(
	_ context.Context,
	app string,
	databaseID string,
	attachmentID string,
) error {
	client.detachedApp = app
	client.detachedDatabase = databaseID
	client.detachedAttachment = attachmentID

	if client.detachErr != nil {
		return client.detachErr
	}

	if client.onDetach != nil {
		client.onDetach()
	}

	return nil
}

func readyLifecycleDatabase(
	t *testing.T,
	store *metadata.Store,
	name string,
) metadata.Database {
	t.Helper()

	database, err :=
		store.CreateDatabaseMetadata(
			context.Background(),
			name,
		)
	if err != nil {
		t.Fatal(err)
	}

	database, err =
		store.UpdateDatabaseStatus(
			context.Background(),
			database.ID,
			metadata.StatusReady,
		)
	if err != nil {
		t.Fatal(err)
	}

	return database
}

func lifecycleJSONRequest(
	t *testing.T,
	server http.Handler,
	path string,
	body string,
) *httptest.ResponseRecorder {
	t.Helper()

	request := httptest.NewRequest(
		http.MethodPost,
		path,
		strings.NewReader(body),
	)
	request.Header.Set(
		"Content-Type",
		"application/json",
	)

	response := httptest.NewRecorder()
	server.ServeHTTP(response, request)

	return response
}

func TestMiniDeployDeploymentListIsProxiedSafely(
	t *testing.T,
) {
	server, _ := testServer(t)

	client := &fakeMiniDeployLifecycleClient{
		deployments: []minideploy.Deployment{{
			App:       "scheduler",
			Supported: true,
			Status:    "running",
		}},
	}

	server.ConfigureMiniDeployLifecycle(client)

	response := request(
		t,
		server,
		http.MethodGet,
		"/api/v1/deployments",
	)

	if response.Code != http.StatusOK {
		t.Fatalf(
			"status = %d, body = %s",
			response.Code,
			response.Body.String(),
		)
	}

	var deployments []minideploy.Deployment
	if err := json.Unmarshal(
		response.Body.Bytes(),
		&deployments,
	); err != nil {
		t.Fatal(err)
	}

	if len(deployments) != 1 ||
		deployments[0].App != "scheduler" {

		t.Fatalf(
			"deployments = %#v",
			deployments,
		)
	}

	assertSafeResponse(
		t,
		response.Body.String(),
	)
}

func TestAttachDatabaseCoordinatesThroughMiniDeploy(
	t *testing.T,
) {
	server, store := testServer(t)

	database := readyLifecycleDatabase(
		t,
		store,
		"Attach Test",
	)

	client := &fakeMiniDeployLifecycleClient{
		deployments: []minideploy.Deployment{{
			App:              "scheduler",
			Supported:        true,
			Status:           "running",
			DatabaseAttached: false,
		}},
	}

	client.onAttach = func() {
		if _, err := store.CreateAttachment(
			context.Background(),
			database.ID,
			metadata.ConsumerTypeMiniDeploy,
			"scheduler",
			metadata.BindingNamePrimary,
		); err != nil {
			t.Fatal(err)
		}
	}

	server.ConfigureMiniDeployLifecycle(client)

	response := lifecycleJSONRequest(
		t,
		server,
		"/api/v1/databases/"+
			database.ID+
			"/attach",
		`{"app":"scheduler"}`,
	)

	if response.Code != http.StatusOK {
		t.Fatalf(
			"status = %d, body = %s",
			response.Code,
			response.Body.String(),
		)
	}

	if client.attachedApp != "scheduler" ||
		client.attachedDatabase != database.ID {

		t.Fatalf(
			"attach = app:%q db:%q",
			client.attachedApp,
			client.attachedDatabase,
		)
	}

	attachments, err :=
		store.ListAttachmentsForDatabase(
			context.Background(),
			database.ID,
		)
	if err != nil {
		t.Fatal(err)
	}

	if len(attachments) != 1 ||
		attachments[0].ConsumerRef != "scheduler" {

		t.Fatalf(
			"attachments = %#v",
			attachments,
		)
	}

	assertSafeResponse(
		t,
		response.Body.String(),
	)
}

func TestDetachDatabaseCoordinatesThroughMiniDeploy(
	t *testing.T,
) {
	server, store := testServer(t)

	database := readyLifecycleDatabase(
		t,
		store,
		"Detach Test",
	)

	attachment, err :=
		store.CreateAttachment(
			context.Background(),
			database.ID,
			metadata.ConsumerTypeMiniDeploy,
			"scheduler",
			metadata.BindingNamePrimary,
		)
	if err != nil {
		t.Fatal(err)
	}

	client := &fakeMiniDeployLifecycleClient{}

	client.onDetach = func() {
		if err := store.DeleteAttachment(
			context.Background(),
			attachment.ID,
		); err != nil {
			t.Fatal(err)
		}
	}

	server.ConfigureMiniDeployLifecycle(client)

	response := request(
		t,
		server,
		http.MethodPost,
		"/api/v1/databases/"+
			database.ID+
			"/detach",
	)

	if response.Code != http.StatusOK {
		t.Fatalf(
			"status = %d, body = %s",
			response.Code,
			response.Body.String(),
		)
	}

	if client.detachedApp != "scheduler" ||
		client.detachedDatabase != database.ID ||
		client.detachedAttachment != attachment.ID {

		t.Fatalf(
			"detach = app:%q db:%q attachment:%q",
			client.detachedApp,
			client.detachedDatabase,
			client.detachedAttachment,
		)
	}

	remaining, err :=
		store.ListAttachmentsForDatabase(
			context.Background(),
			database.ID,
		)
	if err != nil {
		t.Fatal(err)
	}

	if len(remaining) != 0 {
		t.Fatalf(
			"remaining attachments = %#v",
			remaining,
		)
	}

	assertSafeResponse(
		t,
		response.Body.String(),
	)
}
