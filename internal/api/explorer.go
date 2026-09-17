package api

import (
	"errors"
	"net/http"
	"net/url"
	"strconv"

	"github.com/conradevans/MiniBase/internal/explorer"
)

func (s *Server) routeDatabaseExplorer(
	response http.ResponseWriter,
	request *http.Request,
	databaseID string,
	resource string,
) {
	if s.explorer == nil {
		writeError(response, http.StatusServiceUnavailable, "explorer_unavailable", "database explorer is unavailable")
		return
	}

	switch resource {
	case "objects":
		if len(request.URL.Query()) != 0 {
			writeExplorerInvalidRequest(response)
			return
		}
		catalog, err := s.explorer.ListObjects(request.Context(), databaseID)
		if err != nil {
			s.writeExplorerError(response, databaseID, "list_objects", err)
			return
		}
		writeJSON(response, http.StatusOK, catalog)
	case "columns":
		schemaName, objectName, ok := explorerObjectQuery(request.URL.Query())
		if !ok {
			writeExplorerInvalidRequest(response)
			return
		}
		description, err := s.explorer.DescribeObject(
			request.Context(),
			databaseID,
			schemaName,
			objectName,
		)
		if err != nil {
			s.writeExplorerError(response, databaseID, "describe_object", err)
			return
		}
		writeJSON(response, http.StatusOK, description)
	case "rows":
		schemaName, objectName, limit, offset, ok := explorerRowsQuery(request.URL.Query())
		if !ok {
			writeExplorerInvalidRequest(response)
			return
		}
		page, err := s.explorer.BrowseRows(
			request.Context(),
			databaseID,
			schemaName,
			objectName,
			limit,
			offset,
		)
		if err != nil {
			s.writeExplorerError(response, databaseID, "browse_rows", err)
			return
		}
		writeJSON(response, http.StatusOK, page)
	default:
		writeError(response, http.StatusNotFound, "not_found", "resource not found")
	}
}

func explorerObjectQuery(values url.Values) (string, string, bool) {
	if !onlyQueryKeys(values, "schema", "object") {
		return "", "", false
	}
	schemaName, schemaOK := oneQueryValue(values, "schema")
	objectName, objectOK := oneQueryValue(values, "object")
	return schemaName, objectName, schemaOK && objectOK
}

func explorerRowsQuery(values url.Values) (string, string, int, int, bool) {
	if !onlyQueryKeys(values, "schema", "object", "limit", "offset") {
		return "", "", 0, 0, false
	}
	schemaName, schemaOK := oneQueryValue(values, "schema")
	objectName, objectOK := oneQueryValue(values, "object")
	if !schemaOK || !objectOK {
		return "", "", 0, 0, false
	}

	limit := explorer.DefaultPageSize
	if rawLimit, exists := values["limit"]; exists {
		if len(rawLimit) != 1 {
			return "", "", 0, 0, false
		}
		parsed, err := strconv.Atoi(rawLimit[0])
		if err != nil {
			return "", "", 0, 0, false
		}
		limit = parsed
	}
	offset := 0
	if rawOffset, exists := values["offset"]; exists {
		if len(rawOffset) != 1 {
			return "", "", 0, 0, false
		}
		parsed, err := strconv.Atoi(rawOffset[0])
		if err != nil {
			return "", "", 0, 0, false
		}
		offset = parsed
	}
	if (limit != 25 && limit != explorer.DefaultPageSize && limit != explorer.MaxPageSize) || offset < 0 {
		return "", "", 0, 0, false
	}
	return schemaName, objectName, limit, offset, true
}

func onlyQueryKeys(values url.Values, allowed ...string) bool {
	allowedKeys := make(map[string]struct{}, len(allowed))
	for _, key := range allowed {
		allowedKeys[key] = struct{}{}
	}
	for key := range values {
		if _, ok := allowedKeys[key]; !ok {
			return false
		}
	}
	return true
}

func oneQueryValue(values url.Values, key string) (string, bool) {
	entries, ok := values[key]
	value := ""
	if ok && len(entries) == 1 {
		value = entries[0]
	}
	return value, ok && len(entries) == 1 && value != ""
}

func writeExplorerInvalidRequest(response http.ResponseWriter) {
	writeError(response, http.StatusBadRequest, "invalid_explorer_request", "invalid database explorer request")
}

func (s *Server) writeExplorerError(
	response http.ResponseWriter,
	databaseID string,
	operation string,
	err error,
) {
	switch {
	case errors.Is(err, explorer.ErrInvalidRequest):
		writeExplorerInvalidRequest(response)
	case errors.Is(err, explorer.ErrDatabaseNotFound):
		writeError(response, http.StatusNotFound, "not_found", "database not found")
	case errors.Is(err, explorer.ErrObjectNotFound):
		writeError(response, http.StatusNotFound, "not_found", "database object not found")
	case errors.Is(err, explorer.ErrDatabaseNotReady):
		writeError(response, http.StatusConflict, "database_not_ready", "database is not ready")
	case errors.Is(err, explorer.ErrQueryTimeout):
		writeError(response, http.StatusGatewayTimeout, "explorer_timeout", "database explorer query timed out")
	case errors.Is(err, explorer.ErrResultTooLarge):
		writeError(response, http.StatusRequestEntityTooLarge, "explorer_result_too_large", "database explorer result is too large")
	default:
		s.logger.Error(
			"database explorer operation failed",
			"database_id", databaseID,
			"operation", operation,
		)
		writeError(response, http.StatusServiceUnavailable, "explorer_unavailable", "database explorer is unavailable")
	}
}
