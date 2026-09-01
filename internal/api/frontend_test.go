package api

import (
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

const testFrontendMarker = "MiniBase dashboard test build"

func TestStaticFrontendServingAndSPAFallback(t *testing.T) {
	server, _ := testServer(t)
	frontendDirectory := testFrontendDirectory(t)
	server.frontend = newFrontendHandler(frontendDirectory)

	for _, route := range []string{
		"/",
		"/guest",
		"/guest/",
		"/admin",
		"/admin/",
		"/admin/databases",
		"/admin/databases/",
		"/admin/backups",
		"/admin/backups/",
		"/admin/databases/database_0123456789abcdef0123456789abcdef",
	} {
		t.Run(route, func(t *testing.T) {
			response := request(t, server, http.MethodGet, route)
			if response.Code != http.StatusOK {
				t.Fatalf("status = %d, body = %s", response.Code, response.Body.String())
			}
			if !strings.Contains(response.Body.String(), testFrontendMarker) {
				t.Fatalf("route did not receive SPA document: %s", response.Body.String())
			}
			if !strings.Contains(response.Header().Get("Content-Type"), "text/html") {
				t.Fatalf("Content-Type = %q", response.Header().Get("Content-Type"))
			}
			if response.Header().Get("Content-Security-Policy") == "" {
				t.Fatal("SPA document is missing Content-Security-Policy")
			}
		})
	}

	assetResponse := request(t, server, http.MethodGet, "/assets/app.js")
	if assetResponse.Code != http.StatusOK || strings.TrimSpace(assetResponse.Body.String()) != "export const app = 'minibase'" {
		t.Fatalf("asset status = %d, body = %s", assetResponse.Code, assetResponse.Body.String())
	}
	if !strings.Contains(assetResponse.Header().Get("Content-Type"), "javascript") {
		t.Fatalf("asset Content-Type = %q", assetResponse.Header().Get("Content-Type"))
	}

	headResponse := request(t, server, http.MethodHead, "/admin")
	if headResponse.Code != http.StatusOK || headResponse.Body.Len() != 0 {
		t.Fatalf("HEAD status = %d, body length = %d", headResponse.Code, headResponse.Body.Len())
	}
}

func TestAPIRoutesNeverFallThroughToFrontend(t *testing.T) {
	server, _ := testServer(t)
	server.frontend = newFrontendHandler(testFrontendDirectory(t))

	for _, route := range []string{
		"/api/v1/unknown",
		"/api/v1/databases/database_00000000000000000000000000000000",
		"/api/v1/guest/unknown",
	} {
		response := request(t, server, http.MethodGet, route)
		if response.Code != http.StatusNotFound {
			t.Fatalf("%s status = %d, body = %s", route, response.Code, response.Body.String())
		}
		if response.Header().Get("Content-Type") != "application/json; charset=utf-8" {
			t.Fatalf("%s Content-Type = %q", route, response.Header().Get("Content-Type"))
		}
		if strings.Contains(response.Body.String(), testFrontendMarker) {
			t.Fatalf("%s fell through to the SPA", route)
		}
	}
}

func TestStaticFrontendRejectsTraversalDotfilesAndSymlinkEscape(t *testing.T) {
	server, _ := testServer(t)
	frontendDirectory := testFrontendDirectory(t)
	outsideFile := filepath.Join(t.TempDir(), "outside-secret.txt")
	if err := os.WriteFile(outsideFile, []byte("must not be served"), 0o600); err != nil {
		t.Fatalf("write outside fixture: %v", err)
	}
	if err := os.Symlink(outsideFile, filepath.Join(frontendDirectory, "assets", "linked.js")); err != nil {
		t.Fatalf("create symlink fixture: %v", err)
	}
	server.frontend = newFrontendHandler(frontendDirectory)

	for _, route := range []string{
		"/assets/../index.html",
		"/assets/%2e%2e/index.html",
		"/assets/.secret",
		"/assets/linked.js",
		"/assets/",
		"/admin/databases/../../index.html",
	} {
		t.Run(route, func(t *testing.T) {
			response := request(t, server, http.MethodGet, route)
			if response.Code == http.StatusOK {
				t.Fatalf("unsafe path returned 200: %s", response.Body.String())
			}
			if strings.Contains(response.Body.String(), "must not be served") || strings.Contains(response.Body.String(), testFrontendMarker) {
				t.Fatalf("unsafe path exposed a file: %s", response.Body.String())
			}
		})
	}
}

func TestMissingFrontendFailsSafelyWithoutAffectingAPI(t *testing.T) {
	server, _ := testServer(t)
	server.frontend = newFrontendHandler(filepath.Join(t.TempDir(), "missing"))

	response := request(t, server, http.MethodGet, "/")
	if response.Code != http.StatusServiceUnavailable {
		t.Fatalf("status = %d, body = %s", response.Code, response.Body.String())
	}
	if strings.Contains(response.Body.String(), "/srv/") || strings.Contains(response.Body.String(), "missing") {
		t.Fatalf("missing frontend response leaked a filesystem path: %s", response.Body.String())
	}

	health := request(t, server, http.MethodGet, "/health")
	if health.Code != http.StatusOK {
		t.Fatalf("health status = %d, body = %s", health.Code, health.Body.String())
	}

	methodResponse := request(t, server, http.MethodPost, "/admin")
	if methodResponse.Code != http.StatusMethodNotAllowed || methodResponse.Header().Get("Allow") != "GET, HEAD" {
		t.Fatalf("method status = %d, Allow = %q", methodResponse.Code, methodResponse.Header().Get("Allow"))
	}
}

func testFrontendDirectory(t *testing.T) string {
	t.Helper()
	directory := t.TempDir()
	if err := os.MkdirAll(filepath.Join(directory, "assets"), 0o700); err != nil {
		t.Fatalf("create assets fixture: %v", err)
	}
	if err := os.WriteFile(filepath.Join(directory, "index.html"), []byte("<!doctype html><title>"+testFrontendMarker+"</title>"), 0o600); err != nil {
		t.Fatalf("write index fixture: %v", err)
	}
	if err := os.WriteFile(filepath.Join(directory, "assets", "app.js"), []byte("export const app = 'minibase'"), 0o600); err != nil {
		t.Fatalf("write asset fixture: %v", err)
	}
	if err := os.WriteFile(filepath.Join(directory, "assets", ".secret"), []byte("must not be served"), 0o600); err != nil {
		t.Fatalf("write dotfile fixture: %v", err)
	}
	return directory
}
