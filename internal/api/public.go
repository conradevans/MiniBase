package api

import (
	"net/http"
	"strings"

	"github.com/conradevans/MiniBase/internal/accessauth"
)

// PublicHandler exposes only the public ReactorLab surface:
//
//   - health
//   - read-only guest endpoints and pages
//   - frontend assets
//   - Access-protected administrator pages and API
//
// MiniDeploy integration routes remain private to the management listener.
func PublicHandler(
	server *Server,
	validator accessauth.AccessTokenValidator,
) http.Handler {
	protected := accessauth.RequireAccess(
		validator,
		http.HandlerFunc(server.ServeHTTP),
	)

	return http.HandlerFunc(func(
		response http.ResponseWriter,
		request *http.Request,
	) {
		requestPath := request.URL.Path

		// Never expose the MiniDeploy service-to-service integration
		// surface through the public origin.
		if strings.HasPrefix(
			requestPath,
			miniDeployIntegrationPrefix,
		) {
			response.Header().Set(
				"Cache-Control",
				"no-store",
			)
			response.Header().Set(
				"X-Content-Type-Options",
				"nosniff",
			)

			writeError(
				response,
				http.StatusNotFound,
				"not_found",
				"resource not found",
			)
			return
		}

		// Health is intentionally safe and does not expose secrets.
		if requestPath == "/health" {
			server.ServeHTTP(response, request)
			return
		}

		// Guest API is deliberately read-only and uses a reduced DTO.
		if requestPath == "/api/v1/guest" ||
			strings.HasPrefix(
				requestPath,
				"/api/v1/guest/",
			) {

			server.ServeHTTP(response, request)
			return
		}

		// All administrator API routes require a valid Cloudflare
		// Access assertion at the origin.
		if requestPath == "/api/v1" ||
			strings.HasPrefix(
				requestPath,
				"/api/v1/",
			) {

			protected.ServeHTTP(response, request)
			return
		}

		// Administrator SPA pages are also protected.
		if requestPath == "/admin" ||
			strings.HasPrefix(
				requestPath,
				"/admin/",
			) {

			protected.ServeHTTP(response, request)
			return
		}

		// Only explicitly public frontend surfaces are allowed through.
		// Keeping this as an allowlist prevents future private routes from
		// becoming public accidentally.
		if requestPath == "/" ||
			requestPath == "/guest" ||
			requestPath == "/guest/" ||
			strings.HasPrefix(
				requestPath,
				"/assets/",
			) {
			server.ServeHTTP(response, request)
			return
		}

		response.Header().Set(
			"Cache-Control",
			"no-store",
		)
		response.Header().Set(
			"X-Content-Type-Options",
			"nosniff",
		)
		writeError(
			response,
			http.StatusNotFound,
			"not_found",
			"resource not found",
		)
	})
}
