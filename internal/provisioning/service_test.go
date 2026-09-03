package provisioning

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/conradevans/MiniBase/internal/metadata"
)

func TestProvisionDatabaseSuccess(t *testing.T) {
	fixture := newServiceFixture()
	database, err := fixture.service.ProvisionDatabase(context.Background(), "  Example Database  ")
	if err != nil {
		t.Fatalf("ProvisionDatabase() error = %v", err)
	}
	if database.DisplayName != "Example Database" || database.Status != metadata.StatusReady {
		t.Fatalf("database = %#v", database)
	}
	if len(fixture.metadata.statuses) != 1 || fixture.metadata.statuses[0] != metadata.StatusReady {
		t.Fatalf("status updates = %v", fixture.metadata.statuses)
	}
	if !fixture.postgres.roleCreated || !fixture.postgres.databaseCreated || !fixture.postgres.configured {
		t.Fatalf("PostgreSQL workflow incomplete: %#v", fixture.postgres)
	}
	if fixture.credentials.passwordReturned == "" {
		t.Fatal("credential was not generated")
	}
}

func TestProvisionDatabaseForRestoreLeavesResourceProvisioning(t *testing.T) {
	fixture := newServiceFixture()
	database, err := fixture.service.ProvisionDatabaseForRestore(context.Background(), "Recovered")
	if err != nil {
		t.Fatalf("ProvisionDatabaseForRestore() error = %v", err)
	}
	if database.Status != metadata.StatusProvisioning {
		t.Fatalf("status = %q, want provisioning", database.Status)
	}
	if len(fixture.metadata.statuses) != 0 {
		t.Fatalf("status updates = %v, want none", fixture.metadata.statuses)
	}
}

func TestFinalizeRestoredDatabaseReappliesSecurityAndMarksReady(t *testing.T) {
	fixture := newServiceFixture()
	record, err := fixture.service.ProvisionDatabaseForRestore(context.Background(), "Recovered")
	if err != nil {
		t.Fatalf("ProvisionDatabaseForRestore() error = %v", err)
	}
	fixture.postgres.configured = false
	ready, err := fixture.service.FinalizeRestoredDatabase(context.Background(), record)
	if err != nil {
		t.Fatalf("FinalizeRestoredDatabase() error = %v", err)
	}
	if !fixture.postgres.configured || ready.Status != metadata.StatusReady {
		t.Fatalf("configured=%v database=%#v", fixture.postgres.configured, ready)
	}
}

func TestMarkDatabaseRestoringFailsClosedAcrossRestart(t *testing.T) {
	fixture := newServiceFixture()
	record, err := fixture.service.ProvisionDatabase(context.Background(), "Existing")
	if err != nil {
		t.Fatalf("ProvisionDatabase() error = %v", err)
	}
	restoring, err := fixture.service.MarkDatabaseRestoring(context.Background(), record.ID)
	if err != nil {
		t.Fatalf("MarkDatabaseRestoring() error = %v", err)
	}
	if restoring.Status != metadata.StatusError {
		t.Fatalf("restoring status = %q, want fail-closed error", restoring.Status)
	}
	statusCount := len(fixture.metadata.statuses)
	if err := fixture.service.Reconcile(context.Background()); err != nil {
		t.Fatalf("Reconcile() error = %v", err)
	}
	if len(fixture.metadata.statuses) != statusCount {
		t.Fatal("startup reconciliation incorrectly marked an interrupted restore ready")
	}
}

func TestCleanupRestoreTargetRemovesOnlyExactProvisioningResource(t *testing.T) {
	fixture := newServiceFixture()
	record, err := fixture.service.ProvisionDatabaseForRestore(context.Background(), "Recovered")
	if err != nil {
		t.Fatalf("ProvisionDatabaseForRestore() error = %v", err)
	}
	if err := fixture.service.CleanupRestoreTarget(record); err != nil {
		t.Fatalf("CleanupRestoreTarget() error = %v", err)
	}
	if !fixture.metadata.deleted || !fixture.credentials.deleted {
		t.Fatal("metadata or credential not removed")
	}
	if len(fixture.postgres.droppedDatabases) != 1 || fixture.postgres.droppedDatabases[0] != record.InternalName ||
		len(fixture.postgres.droppedRoles) != 1 || fixture.postgres.droppedRoles[0] != record.RoleName {
		t.Fatalf("unexpected cleanup: databases=%v roles=%v", fixture.postgres.droppedDatabases, fixture.postgres.droppedRoles)
	}
}

func TestCleanupRestoreTargetRejectsReadyOrMismatchedResource(t *testing.T) {
	tests := []struct {
		name   string
		mutate func(*serviceFixture, *metadata.Database)
	}{
		{name: "ready", mutate: func(f *serviceFixture, _ *metadata.Database) { f.metadata.record.Status = metadata.StatusReady }},
		{name: "mismatched role", mutate: func(_ *serviceFixture, record *metadata.Database) {
			record.RoleName = "mb_role_ffffffffffffffffffffffffffffffff"
		}},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			fixture := newServiceFixture()
			record, err := fixture.service.ProvisionDatabaseForRestore(context.Background(), "Recovered")
			if err != nil {
				t.Fatalf("ProvisionDatabaseForRestore() error = %v", err)
			}
			test.mutate(&fixture, &record)
			if err := fixture.service.CleanupRestoreTarget(record); !errors.Is(err, ErrProvisioning) {
				t.Fatalf("CleanupRestoreTarget() error = %v, want ErrProvisioning", err)
			}
			if fixture.metadata.deleted || fixture.credentials.deleted ||
				len(fixture.postgres.droppedDatabases) != 0 || len(fixture.postgres.droppedRoles) != 0 {
				t.Fatal("cleanup touched resources after strict identity check failed")
			}
		})
	}
}

func TestDeleteDatabaseResourcesIsStrictOrderedAndRetrySafe(t *testing.T) {
	fixture := newServiceFixture()
	record := metadata.Database{
		ID:           "database_0123456789abcdef0123456789abcdef",
		DisplayName:  "Delete",
		InternalName: testDatabaseName,
		RoleName:     testRoleName,
		Status:       metadata.StatusError,
	}
	fixture.metadata.record = record

	for attempt := 0; attempt < 2; attempt++ {
		if err := fixture.service.DeleteDatabaseResources(context.Background(), record); err != nil {
			t.Fatalf("DeleteDatabaseResources() attempt %d error = %v", attempt+1, err)
		}
	}
	if len(fixture.postgres.droppedDatabases) != 2 || len(fixture.postgres.droppedRoles) != 2 {
		t.Fatalf("idempotent PostgreSQL cleanup = databases %v roles %v",
			fixture.postgres.droppedDatabases, fixture.postgres.droppedRoles)
	}
	if !fixture.credentials.deleted || fixture.credentials.deletedID != record.ID {
		t.Fatal("database credential was not deleted")
	}

	ready := newServiceFixture()
	ready.metadata.record = record
	ready.metadata.record.Status = metadata.StatusReady
	if err := ready.service.DeleteDatabaseResources(context.Background(), ready.metadata.record); !errors.Is(err, ErrProvisioning) {
		t.Fatalf("ready database cleanup error = %v, want ErrProvisioning", err)
	}
	if len(ready.postgres.droppedDatabases) != 0 || len(ready.postgres.droppedRoles) != 0 || ready.credentials.deleted {
		t.Fatal("strict cleanup touched a database outside fail-closed error state")
	}
}

func TestSecretWriteFailureRemovesMetadataWithoutTouchingPostgres(t *testing.T) {
	fixture := newServiceFixture()
	fixture.credentials.createErr = errors.New("secret write failed")

	if _, err := fixture.service.ProvisionDatabase(context.Background(), "Example"); !errors.Is(err, ErrProvisioning) {
		t.Fatalf("ProvisionDatabase() error = %v, want ErrProvisioning", err)
	}
	if !fixture.metadata.deleted {
		t.Fatal("metadata was not removed")
	}
	if fixture.postgres.roleCreated || fixture.postgres.databaseCreated || len(fixture.postgres.droppedRoles) != 0 {
		t.Fatal("PostgreSQL was touched after secret-write failure")
	}
}

func TestPostgresFailureCompensatesCreatedResources(t *testing.T) {
	fixture := newServiceFixture()
	fixture.postgres.createDatabaseErr = errors.New("database create failed")

	if _, err := fixture.service.ProvisionDatabase(context.Background(), "Example"); !errors.Is(err, ErrProvisioning) {
		t.Fatalf("ProvisionDatabase() error = %v, want ErrProvisioning", err)
	}
	if len(fixture.postgres.droppedRoles) != 1 {
		t.Fatalf("dropped roles = %v", fixture.postgres.droppedRoles)
	}
	if len(fixture.postgres.droppedDatabases) != 0 {
		t.Fatalf("database not successfully created but drop attempted: %v", fixture.postgres.droppedDatabases)
	}
	if !fixture.credentials.deleted || !fixture.metadata.deleted {
		t.Fatal("credential or metadata cleanup did not complete")
	}
}

func TestReadyMetadataFailureCompensatesEverything(t *testing.T) {
	fixture := newServiceFixture()
	fixture.metadata.readyUpdateErr = errors.New("ready update failed")

	if _, err := fixture.service.ProvisionDatabase(context.Background(), "Example"); !errors.Is(err, ErrProvisioning) {
		t.Fatalf("ProvisionDatabase() error = %v, want ErrProvisioning", err)
	}
	if len(fixture.postgres.droppedDatabases) != 1 || len(fixture.postgres.droppedRoles) != 1 {
		t.Fatalf("PostgreSQL cleanup incomplete: databases=%v roles=%v", fixture.postgres.droppedDatabases, fixture.postgres.droppedRoles)
	}
	if !fixture.credentials.deleted || !fixture.metadata.deleted {
		t.Fatal("credential or metadata cleanup incomplete")
	}
}

func TestCleanupFailurePreservesMetadataAndCredentialAndMarksError(t *testing.T) {
	fixture := newServiceFixture()
	fixture.postgres.configureErr = errors.New("configure failed")
	fixture.postgres.dropDatabaseErr = errors.New("drop failed")

	if _, err := fixture.service.ProvisionDatabase(context.Background(), "Example"); !errors.Is(err, ErrProvisioning) {
		t.Fatalf("ProvisionDatabase() error = %v, want ErrProvisioning", err)
	}
	if fixture.metadata.deleted {
		t.Fatal("metadata was deleted after incomplete cleanup")
	}
	if fixture.credentials.deleted {
		t.Fatal("credential was deleted while PostgreSQL resource remained")
	}
	if len(fixture.metadata.statuses) == 0 || fixture.metadata.statuses[len(fixture.metadata.statuses)-1] != metadata.StatusError {
		t.Fatalf("status updates = %v, want final error", fixture.metadata.statuses)
	}
	if len(fixture.postgres.droppedRoles) != 0 {
		t.Fatal("role cleanup was attempted while its database remained")
	}
}

func TestCompensationTouchesOnlyGeneratedResource(t *testing.T) {
	fixture := newServiceFixture()
	fixture.postgres.configureErr = errors.New("configure failed")
	recordID := "database_11111111111111111111111111111111"
	databaseName := "mb_db_22222222222222222222222222222222"
	roleName := "mb_role_33333333333333333333333333333333"
	fixture.service.newDatabaseID = func() (string, error) { return recordID, nil }
	fixture.service.newDatabaseName = func() (string, error) { return databaseName, nil }
	fixture.service.newRoleName = func() (string, error) { return roleName, nil }

	_, _ = fixture.service.ProvisionDatabase(context.Background(), "Example")
	if len(fixture.postgres.droppedDatabases) != 1 || fixture.postgres.droppedDatabases[0] != databaseName {
		t.Fatalf("dropped databases = %v", fixture.postgres.droppedDatabases)
	}
	if len(fixture.postgres.droppedRoles) != 1 || fixture.postgres.droppedRoles[0] != roleName {
		t.Fatalf("dropped roles = %v", fixture.postgres.droppedRoles)
	}
	if fixture.credentials.deletedID != recordID {
		t.Fatalf("deleted credential ID = %q", fixture.credentials.deletedID)
	}
}

func TestReconciliationIsConservativeAndNonDestructive(t *testing.T) {
	tests := []struct {
		name             string
		role             RoleState
		database         DatabaseState
		credentialExists bool
		wantStatus       metadata.DatabaseStatus
	}{
		{
			name:             "complete",
			role:             RoleState{Exists: true, Login: true},
			database:         DatabaseState{Exists: true, Owner: testRoleName, OwnerSchemaCreate: true},
			credentialExists: true,
			wantStatus:       metadata.StatusReady,
		},
		{name: "missing role", database: DatabaseState{Exists: true, Owner: testRoleName, OwnerSchemaCreate: true}, credentialExists: true, wantStatus: metadata.StatusError},
		{name: "missing database", role: RoleState{Exists: true, Login: true}, credentialExists: true, wantStatus: metadata.StatusError},
		{name: "wrong owner", role: RoleState{Exists: true, Login: true}, database: DatabaseState{Exists: true, Owner: "mb_role_ffffffffffffffffffffffffffffffff", OwnerSchemaCreate: true}, credentialExists: true, wantStatus: metadata.StatusError},
		{name: "missing credential", role: RoleState{Exists: true, Login: true}, database: DatabaseState{Exists: true, Owner: testRoleName, OwnerSchemaCreate: true}, wantStatus: metadata.StatusError},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			fixture := newServiceFixture()
			fixture.metadata.record = metadata.Database{
				ID:           "database_0123456789abcdef0123456789abcdef",
				DisplayName:  "Reconcile",
				InternalName: testDatabaseName,
				RoleName:     testRoleName,
				Status:       metadata.StatusProvisioning,
			}
			fixture.postgres.roleState = test.role
			fixture.postgres.databaseState = test.database
			fixture.credentials.exists = test.credentialExists

			if err := fixture.service.Reconcile(context.Background()); err != nil {
				t.Fatalf("Reconcile() error = %v", err)
			}
			if len(fixture.metadata.statuses) != 1 || fixture.metadata.statuses[0] != test.wantStatus {
				t.Fatalf("statuses = %v, want %q", fixture.metadata.statuses, test.wantStatus)
			}
			if len(fixture.postgres.droppedDatabases) != 0 || len(fixture.postgres.droppedRoles) != 0 || fixture.credentials.deleted {
				t.Fatal("reconciliation performed destructive cleanup")
			}
		})
	}
}

type serviceFixture struct {
	service     *Service
	metadata    *fakeMetadata
	credentials *fakeCredentials
	postgres    *fakePostgres
}

func newServiceFixture() serviceFixture {
	metadataStore := &fakeMetadata{}
	credentialStore := &fakeCredentials{exists: true}
	postgres := &fakePostgres{
		roleState:     RoleState{Exists: true, Login: true},
		databaseState: DatabaseState{Exists: true, Owner: testRoleName, OwnerSchemaCreate: true},
	}
	service := NewService(metadataStore, credentialStore, postgres)
	service.newDatabaseID = func() (string, error) { return "database_0123456789abcdef0123456789abcdef", nil }
	service.newDatabaseName = func() (string, error) { return testDatabaseName, nil }
	service.newRoleName = func() (string, error) { return testRoleName, nil }
	service.compensationTimeout = time.Second
	return serviceFixture{service: service, metadata: metadataStore, credentials: credentialStore, postgres: postgres}
}

type fakeMetadata struct {
	record         metadata.Database
	statuses       []metadata.DatabaseStatus
	deleted        bool
	readyUpdateErr error
}

func (store *fakeMetadata) CreateProvisioningDatabase(_ context.Context, input metadata.ProvisioningDatabase) (metadata.Database, error) {
	store.record = metadata.Database{
		ID: input.ID, DisplayName: input.DisplayName, InternalName: input.InternalName,
		RoleName: input.RoleName, Status: metadata.StatusProvisioning,
	}
	return store.record, nil
}

func (store *fakeMetadata) GetDatabase(_ context.Context, id string) (metadata.Database, error) {
	if store.record.ID != id {
		return metadata.Database{}, metadata.ErrNotFound
	}
	return store.record, nil
}

func (store *fakeMetadata) UpdateDatabaseStatus(_ context.Context, _ string, status metadata.DatabaseStatus) (metadata.Database, error) {
	if status == metadata.StatusReady && store.readyUpdateErr != nil {
		return metadata.Database{}, store.readyUpdateErr
	}
	store.statuses = append(store.statuses, status)
	store.record.Status = status
	return store.record, nil
}

func (store *fakeMetadata) DeleteDatabaseMetadata(context.Context, string) error {
	store.deleted = true
	return nil
}

func (store *fakeMetadata) ListProvisioningDatabases(context.Context) ([]metadata.Database, error) {
	if store.record.Status == metadata.StatusProvisioning {
		return []metadata.Database{store.record}, nil
	}
	return []metadata.Database{}, nil
}

type fakeCredentials struct {
	createErr        error
	passwordReturned string
	exists           bool
	deleted          bool
	deletedID        string
}

func (store *fakeCredentials) Create(string) (string, error) {
	if store.createErr != nil {
		return "", store.createErr
	}
	store.passwordReturned = strings.Repeat("a", 64)
	return store.passwordReturned, nil
}
func (store *fakeCredentials) Exists(string) (bool, error) { return store.exists, nil }
func (store *fakeCredentials) Delete(id string) error {
	store.deleted = true
	store.deletedID = id
	return nil
}

type fakePostgres struct {
	roleCreated       bool
	databaseCreated   bool
	configured        bool
	createDatabaseErr error
	configureErr      error
	dropDatabaseErr   error
	roleState         RoleState
	databaseState     DatabaseState
	droppedDatabases  []string
	droppedRoles      []string
}

func (postgres *fakePostgres) CreateRole(context.Context, string, string) error {
	postgres.roleCreated = true
	return nil
}
func (postgres *fakePostgres) CreateDatabase(context.Context, string, string) error {
	if postgres.createDatabaseErr != nil {
		return postgres.createDatabaseErr
	}
	postgres.databaseCreated = true
	return nil
}
func (postgres *fakePostgres) ConfigureDatabasePrivileges(context.Context, string, string) error {
	if postgres.configureErr != nil {
		return postgres.configureErr
	}
	postgres.configured = true
	return nil
}
func (postgres *fakePostgres) InspectRole(context.Context, string) (RoleState, error) {
	return postgres.roleState, nil
}
func (postgres *fakePostgres) InspectDatabase(context.Context, string, string) (DatabaseState, error) {
	return postgres.databaseState, nil
}
func (postgres *fakePostgres) DropDatabase(_ context.Context, name string) error {
	postgres.droppedDatabases = append(postgres.droppedDatabases, name)
	return postgres.dropDatabaseErr
}
func (postgres *fakePostgres) DropRole(_ context.Context, name string) error {
	postgres.droppedRoles = append(postgres.droppedRoles, name)
	return nil
}
