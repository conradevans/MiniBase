package api

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/conradevans/MiniBase/internal/accessauth"
)

type fakePublicAccessValidator struct{}

func (fakePublicAccessValidator) Validate(
	_ context.Context,
	token string,
) (accessauth.AccessIdentity, error) {
	if token != "accepted-token" {
		return accessauth.AccessIdentity{},
			accessauth.ErrAccessDenied
	}

	return accessauth.AccessIdentity{
		Email: "admin@example.com",
	}, nil
}

func publicRequest(
	t *testing.T,
	handler http.Handler,
	path string,
	token string,
) *httptest.ResponseRecorder {
	t.Helper()

	request := httptest.NewRequest(
		http.MethodGet,
		path,
		nil,
	)

	if token != "" {
		request.Header.Set(
			accessauth.AccessJWTHeader,
			token,
		)
	}

	response := httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	return response
}

func TestPublicHandlerAllowsGuestWithoutAccessToken(
	t *testing.T,
) {
	server, _ := testServer(t)
	handler := PublicHandler(server, nil)

	for _, path := range []string{
		"/health",
		"/api/v1/guest/status",
		"/api/v1/guest/databases",
	} {
		response := publicRequest(
			t,
			handler,
			path,
			"",
		)

		if response.Code != http.StatusOK {
			t.Fatalf(
				"%s status = %d, body = %s",
				path,
				response.Code,
				response.Body.String(),
			)
		}
	}
}

func TestPublicHandlerRejectsAdminWithoutAccessToken(
	t *testing.T,
) {
	server, _ := testServer(t)
	handler := PublicHandler(
		server,
		fakePublicAccessValidator{},
	)

	for _, path := range []string{
		"/admin",
		"/api/v1/status",
		"/api/v1/databases",
		"/api/v1/backups",
	} {
		response := publicRequest(
			t,
			handler,
			path,
			"",
		)

		if response.Code != http.StatusUnauthorized {
			t.Fatalf(
				"%s status = %d, want %d",
				path,
				response.Code,
				http.StatusUnauthorized,
			)
		}
	}
}

func TestPublicHandlerAcceptsValidAdminToken(
	t *testing.T,
) {
	server, _ := testServer(t)

	handler := PublicHandler(
		server,
		fakePublicAccessValidator{},
	)

	response := publicRequest(
		t,
		handler,
		"/api/v1/status",
		"accepted-token",
	)

	if response.Code != http.StatusOK {
		t.Fatalf(
			"status = %d, body = %s",
			response.Code,
			response.Body.String(),
		)
	}
}

func TestPublicHandlerRejectsInvalidAdminToken(
	t *testing.T,
) {
	server, _ := testServer(t)

	handler := PublicHandler(
		server,
		fakePublicAccessValidator{},
	)

	response := publicRequest(
		t,
		handler,
		"/api/v1/status",
		"wrong-token",
	)

	if response.Code != http.StatusForbidden {
		t.Fatalf(
			"status = %d, want %d",
			response.Code,
			http.StatusForbidden,
		)
	}
}

func TestPublicHandlerNeverExposesMiniDeployIntegration(
	t *testing.T,
) {
	server, _ := testServer(t)

	handler := PublicHandler(
		server,
		fakePublicAccessValidator{},
	)

	for _, token := range []string{
		"",
		"accepted-token",
	} {
		response := publicRequest(
			t,
			handler,
			"/internal/minideploy/databases",
			token,
		)

		if response.Code != http.StatusNotFound {
			t.Fatalf(
				"status = %d, want %d",
				response.Code,
				http.StatusNotFound,
			)
		}
	}
}

func TestPublicHandlerAllowsOnlyExplicitPublicFrontendSurface(
	t *testing.T,
) {
	server, _ := testServer(t)
	handler := PublicHandler(
		server,
		fakePublicAccessValidator{},
	)

	for _, path := range []string{
		"/",
		"/guest",
		"/guest/",
	} {
		response := publicRequest(
			t,
			handler,
			path,
			"",
		)

		// The test server may not have a built frontend available,
		// in which case the frontend handler returns 503. What matters
		// here is that the public-origin allowlist does not reject the
		// route before it reaches the frontend handler.
		switch response.Code {
		case http.StatusNotFound,
			http.StatusUnauthorized,
			http.StatusForbidden:
			t.Fatalf(
				"%s was blocked by public origin: status = %d, body = %s",
				path,
				response.Code,
				response.Body.String(),
			)
		}
	}

	for _, path := range []string{
		"/debug",
		"/internal",
		"/internal/future-private",
		"/future-management-route",
	} {
		response := publicRequest(
			t,
			handler,
			path,
			"",
		)
		if response.Code != http.StatusNotFound {
			t.Fatalf(
				"%s status = %d, want %d",
				path,
				response.Code,
				http.StatusNotFound,
			)
		}
	}
}
