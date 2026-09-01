package api

import (
	"io/fs"
	"net/http"
	"os"
	"path"
	"strings"
)

const frontendUnavailableMessage = "dashboard build is unavailable"

type frontendHandler struct {
	directory string
}

func newFrontendHandler(directory string) http.Handler {
	return &frontendHandler{directory: directory}
}

func (handler *frontendHandler) ServeHTTP(response http.ResponseWriter, request *http.Request) {
	if request.Method != http.MethodGet && request.Method != http.MethodHead {
		response.Header().Set("Allow", "GET, HEAD")
		writeError(response, http.StatusMethodNotAllowed, "method_not_allowed", "method not allowed")
		return
	}

	switch {
	case isFrontendRoute(request.URL.Path):
		handler.serveFile(response, request, "index.html", true)
	case strings.HasPrefix(request.URL.Path, "/assets/"):
		assetName := strings.TrimPrefix(request.URL.Path, "/")
		if !validAssetName(assetName) {
			writeError(response, http.StatusNotFound, "not_found", "resource not found")
			return
		}
		handler.serveFile(response, request, assetName, false)
	default:
		writeError(response, http.StatusNotFound, "not_found", "resource not found")
	}
}

func (handler *frontendHandler) serveFile(response http.ResponseWriter, request *http.Request, name string, isDocument bool) {
	if strings.TrimSpace(handler.directory) == "" {
		writeError(response, http.StatusServiceUnavailable, "frontend_unavailable", frontendUnavailableMessage)
		return
	}

	root, err := os.OpenRoot(handler.directory)
	if err != nil {
		writeError(response, http.StatusServiceUnavailable, "frontend_unavailable", frontendUnavailableMessage)
		return
	}
	defer root.Close()

	file, err := root.Open(name)
	if err != nil {
		writeError(response, http.StatusNotFound, "not_found", "resource not found")
		return
	}
	defer file.Close()

	info, err := file.Stat()
	if err != nil || !info.Mode().IsRegular() {
		writeError(response, http.StatusNotFound, "not_found", "resource not found")
		return
	}

	if isDocument {
		response.Header().Set("Content-Security-Policy", "default-src 'self'; script-src 'self'; style-src 'self'; img-src 'self' data:; connect-src 'self'; base-uri 'none'; frame-ancestors 'none'; form-action 'self'")
		response.Header().Set("Referrer-Policy", "no-referrer")
		response.Header().Set("X-Frame-Options", "DENY")
	}
	http.ServeContent(response, request, name, info.ModTime(), file)
}

func isFrontendRoute(requestPath string) bool {
	normalized := requestPath
	if normalized != "/" {
		normalized = strings.TrimSuffix(normalized, "/")
	}
	switch normalized {
	case "/", "/guest", "/admin", "/admin/databases":
		return true
	}

	const detailPrefix = "/admin/databases/"
	if !strings.HasPrefix(normalized, detailPrefix) {
		return false
	}
	databaseID := strings.TrimPrefix(normalized, detailPrefix)
	return databaseID != "" && !strings.Contains(databaseID, "/") && databaseID != "." && databaseID != ".."
}

func validAssetName(name string) bool {
	if !fs.ValidPath(name) || !strings.HasPrefix(name, "assets/") || strings.Contains(name, "\\") {
		return false
	}
	for _, segment := range strings.Split(path.Clean(name), "/") {
		if strings.HasPrefix(segment, ".") {
			return false
		}
	}
	return true
}
