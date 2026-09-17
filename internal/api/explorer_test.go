package api

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"strings"
	"testing"

	"github.com/conradevans/MiniBase/internal/explorer"
)

const explorerDatabaseID = "database_0123456789abcdef0123456789abcdef"

type fakeDatabaseExplorer struct {
	catalog     explorer.Catalog
	description explorer.Description
	page        explorer.RowsPage
	err         error
	limit       int
	offset      int
	schema      string
	object      string
}

func (fake *fakeDatabaseExplorer) ListObjects(context.Context, string) (explorer.Catalog, error) {
	return fake.catalog, fake.err
}

func (fake *fakeDatabaseExplorer) DescribeObject(
	_ context.Context,
	_ string,
	schemaName string,
	objectName string,
) (explorer.Description, error) {
	fake.schema = schemaName
	fake.object = objectName
	return fake.description, fake.err
}

func (fake *fakeDatabaseExplorer) BrowseRows(
	_ context.Context,
	_ string,
	schemaName string,
	objectName string,
	limit int,
	offset int,
) (explorer.RowsPage, error) {
	fake.schema = schemaName
	fake.object = objectName
	fake.limit = limit
	fake.offset = offset
	return fake.page, fake.err
}

func configuredExplorerServer(t *testing.T, fake *fakeDatabaseExplorer) *Server {
	server, _ := testServer(t)
	server.ConfigureExplorer(fake)
	return server
}

func TestExplorerStructuredAdminRoutes(t *testing.T) {
	fake := &fakeDatabaseExplorer{
		catalog: explorer.Catalog{Schemas: []explorer.Schema{{
			Name: "public",
			Objects: []explorer.Object{
				{Name: "users", Type: explorer.ObjectTypeTable},
				{Name: "active_users", Type: explorer.ObjectTypeView},
				{Name: "monthly", Type: explorer.ObjectTypeMaterializedView},
			},
		}}},
		description: explorer.Description{
			Schema: "public",
			Object: explorer.Object{Name: "users", Type: explorer.ObjectTypeTable},
			Columns: []explorer.Column{{
				Name: "id", DataType: "bigint", Nullable: false, PrimaryKey: true, OrdinalPosition: 1,
			}},
		},
		page: explorer.RowsPage{
			Schema:  "public",
			Object:  explorer.Object{Name: "users", Type: explorer.ObjectTypeTable},
			Columns: []string{"id", "nullable", "payload", "blob"},
			Rows: [][]explorer.Cell{{
				{Kind: explorer.CellKindValue, Value: "1"},
				{Kind: explorer.CellKindNull},
				{Kind: explorer.CellKindJSON, Value: `{"email":"admin@example.com"}`},
				{Kind: explorer.CellKindBinary, Value: "<binary: 18432 bytes>"},
			}},
			Limit: 50, Offset: 0, HasMore: false,
		},
	}
	server := configuredExplorerServer(t, fake)

	objects := request(t, server, http.MethodGet, "/api/v1/databases/"+explorerDatabaseID+"/explorer/objects")
	if objects.Code != http.StatusOK || !strings.Contains(objects.Body.String(), "materialized_view") {
		t.Fatalf("objects status=%d body=%s", objects.Code, objects.Body.String())
	}
	columns := request(t, server, http.MethodGet, "/api/v1/databases/"+explorerDatabaseID+"/explorer/columns?schema=public&object=users")
	if columns.Code != http.StatusOK || !strings.Contains(columns.Body.String(), `"primaryKey":true`) {
		t.Fatalf("columns status=%d body=%s", columns.Code, columns.Body.String())
	}
	rows := request(t, server, http.MethodGet, "/api/v1/databases/"+explorerDatabaseID+"/explorer/rows?schema=public&object=users")
	if rows.Code != http.StatusOK {
		t.Fatalf("rows status=%d body=%s", rows.Code, rows.Body.String())
	}
	if fake.limit != 50 || fake.offset != 0 || fake.schema != "public" || fake.object != "users" {
		t.Fatalf("rows query = %q.%q limit=%d offset=%d", fake.schema, fake.object, fake.limit, fake.offset)
	}
	for _, forbidden := range []string{"databaseUrl", "DATABASE_URL", "credentialPath", "roleName"} {
		if strings.Contains(rows.Body.String(), forbidden) {
			t.Fatalf("response exposed control-plane field %q: %s", forbidden, rows.Body.String())
		}
	}
}

func TestExplorerPaginationValidation(t *testing.T) {
	fake := &fakeDatabaseExplorer{page: explorer.RowsPage{Rows: make([][]explorer.Cell, 0)}}
	server := configuredExplorerServer(t, fake)
	base := "/api/v1/databases/" + explorerDatabaseID + "/explorer/rows?schema=public&object=users"

	for _, limit := range []string{"25", "50", "100"} {
		response := request(t, server, http.MethodGet, base+"&limit="+limit+"&offset=25")
		if response.Code != http.StatusOK {
			t.Fatalf("limit %s status=%d body=%s", limit, response.Code, response.Body.String())
		}
	}
	for _, query := range []string{
		base + "&limit=1",
		base + "&limit=101",
		base + "&offset=-1",
		base + "&limit=not-a-number",
		base + "&schema=other",
		base + "&sort=id",
		"/api/v1/databases/" + explorerDatabaseID + "/explorer/rows?object=users",
	} {
		response := request(t, server, http.MethodGet, query)
		if response.Code != http.StatusBadRequest {
			t.Fatalf("%s status=%d body=%s", query, response.Code, response.Body.String())
		}
	}
}

func TestExplorerSafeErrorsAndMethods(t *testing.T) {
	for _, test := range []struct {
		err        error
		wantStatus int
		wantCode   string
	}{
		{explorer.ErrDatabaseNotFound, http.StatusNotFound, "not_found"},
		{explorer.ErrDatabaseNotReady, http.StatusConflict, "database_not_ready"},
		{explorer.ErrObjectNotFound, http.StatusNotFound, "not_found"},
		{explorer.ErrQueryTimeout, http.StatusGatewayTimeout, "explorer_timeout"},
		{explorer.ErrResultTooLarge, http.StatusRequestEntityTooLarge, "explorer_result_too_large"},
		{errors.New("postgres 10.0.0.2 password=/secret/path"), http.StatusServiceUnavailable, "explorer_unavailable"},
	} {
		server := configuredExplorerServer(t, &fakeDatabaseExplorer{err: test.err})
		response := request(t, server, http.MethodGet, "/api/v1/databases/"+explorerDatabaseID+"/explorer/objects")
		if response.Code != test.wantStatus || !strings.Contains(response.Body.String(), test.wantCode) {
			t.Fatalf("error %v status=%d body=%s", test.err, response.Code, response.Body.String())
		}
		if strings.Contains(response.Body.String(), "10.0.0.2") || strings.Contains(response.Body.String(), "/secret/") {
			t.Fatalf("raw error leaked: %s", response.Body.String())
		}
	}

	server := configuredExplorerServer(t, &fakeDatabaseExplorer{})
	postRows := request(t, server, http.MethodPost, "/api/v1/databases/"+explorerDatabaseID+"/explorer/rows")
	if postRows.Code != http.StatusMethodNotAllowed || postRows.Header().Get("Allow") != http.MethodGet {
		t.Fatalf("POST rows status=%d Allow=%q", postRows.Code, postRows.Header().Get("Allow"))
	}
	query := request(t, server, http.MethodGet, "/api/v1/databases/"+explorerDatabaseID+"/explorer/query")
	if query.Code != http.StatusNotFound {
		t.Fatalf("arbitrary query status=%d body=%s", query.Code, query.Body.String())
	}
}

func TestExplorerRequiresAdminAuthorizationAndIsAbsentFromGuest(t *testing.T) {
	server := configuredExplorerServer(t, &fakeDatabaseExplorer{})
	public := PublicHandler(server, fakePublicAccessValidator{})
	adminPath := "/api/v1/databases/" + explorerDatabaseID + "/explorer/objects"
	if response := publicRequest(t, public, adminPath, ""); response.Code != http.StatusUnauthorized {
		t.Fatalf("unauthorized admin status=%d", response.Code)
	}
	if response := publicRequest(t, public, adminPath, "accepted-token"); response.Code != http.StatusOK {
		t.Fatalf("authorized admin status=%d body=%s", response.Code, response.Body.String())
	}
	guestPath := "/api/v1/guest/databases/" + explorerDatabaseID + "/explorer/objects"
	if response := publicRequest(t, public, guestPath, ""); response.Code != http.StatusNotFound {
		t.Fatalf("guest explorer status=%d body=%s", response.Code, response.Body.String())
	}
}

func TestExplorerResponsesRemainJSON(t *testing.T) {
	server := configuredExplorerServer(t, &fakeDatabaseExplorer{catalog: explorer.Catalog{Schemas: make([]explorer.Schema, 0)}})
	response := request(t, server, http.MethodGet, "/api/v1/databases/"+explorerDatabaseID+"/explorer/objects")
	var body map[string]json.RawMessage
	if err := json.Unmarshal(response.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	assertExactKeys(t, body, "schemas")
}
