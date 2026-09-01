package provisioning

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/conradevans/MiniBase/internal/ids"
	"github.com/conradevans/MiniBase/internal/metadata"
)

var ErrProvisioning = errors.New("database provisioning failed")

type metadataStore interface {
	CreateProvisioningDatabase(context.Context, metadata.ProvisioningDatabase) (metadata.Database, error)
	GetDatabase(context.Context, string) (metadata.Database, error)
	UpdateDatabaseStatus(context.Context, string, metadata.DatabaseStatus) (metadata.Database, error)
	DeleteDatabaseMetadata(context.Context, string) error
	ListProvisioningDatabases(context.Context) ([]metadata.Database, error)
}

type credentialStore interface {
	Create(string) (string, error)
	Exists(string) (bool, error)
	Delete(string) error
}

type Service struct {
	metadata            metadataStore
	credentials         credentialStore
	postgres            Postgres
	newDatabaseID       func() (string, error)
	newDatabaseName     func() (string, error)
	newRoleName         func() (string, error)
	compensationTimeout time.Duration
}

func NewService(metadataStore metadataStore, credentials credentialStore, postgres Postgres) *Service {
	return &Service{
		metadata:            metadataStore,
		credentials:         credentials,
		postgres:            postgres,
		newDatabaseID:       ids.DatabaseID,
		newDatabaseName:     ids.DatabaseInternalName,
		newRoleName:         ids.RoleInternalName,
		compensationTimeout: 30 * time.Second,
	}
}

func (s *Service) ProvisionDatabase(ctx context.Context, displayName string) (metadata.Database, error) {
	record, err := s.ProvisionDatabaseForRestore(ctx, displayName)
	if err != nil {
		return metadata.Database{}, err
	}
	ready, err := s.metadata.UpdateDatabaseStatus(ctx, record.ID, metadata.StatusReady)
	if err != nil {
		s.compensate(compensationState{
			record:            record,
			metadataCreated:   true,
			credentialCreated: true,
			roleCreated:       true,
			databaseCreated:   true,
		})
		return metadata.Database{}, ErrProvisioning
	}
	return ready, nil
}

func (s *Service) ProvisionDatabaseForRestore(ctx context.Context, displayName string) (metadata.Database, error) {
	normalizedName, err := metadata.NormalizeDisplayName(displayName)
	if err != nil {
		return metadata.Database{}, err
	}

	databaseID, err := s.newDatabaseID()
	if err != nil {
		return metadata.Database{}, ErrProvisioning
	}
	databaseName, err := s.newDatabaseName()
	if err != nil {
		return metadata.Database{}, ErrProvisioning
	}
	roleName, err := s.newRoleName()
	if err != nil {
		return metadata.Database{}, ErrProvisioning
	}

	record, err := s.metadata.CreateProvisioningDatabase(ctx, metadata.ProvisioningDatabase{
		ID:           databaseID,
		DisplayName:  normalizedName,
		InternalName: databaseName,
		RoleName:     roleName,
	})
	if err != nil {
		if errors.Is(err, metadata.ErrInvalidDisplayName) {
			return metadata.Database{}, err
		}
		return metadata.Database{}, ErrProvisioning
	}

	state := compensationState{record: record, metadataCreated: true}
	password, err := s.credentials.Create(record.ID)
	if err != nil {
		s.compensate(state)
		return metadata.Database{}, ErrProvisioning
	}
	state.credentialCreated = true

	if err := s.postgres.CreateRole(ctx, record.RoleName, password); err != nil {
		s.compensate(state)
		return metadata.Database{}, ErrProvisioning
	}
	state.roleCreated = true

	if err := s.postgres.CreateDatabase(ctx, record.InternalName, record.RoleName); err != nil {
		s.compensate(state)
		return metadata.Database{}, ErrProvisioning
	}
	state.databaseCreated = true

	if err := s.postgres.ConfigureDatabasePrivileges(ctx, record.InternalName, record.RoleName); err != nil {
		s.compensate(state)
		return metadata.Database{}, ErrProvisioning
	}
	if err := s.verifyProvisionedState(ctx, record); err != nil {
		s.compensate(state)
		return metadata.Database{}, ErrProvisioning
	}

	return record, nil
}

func (s *Service) FinalizeRestoredDatabase(ctx context.Context, record metadata.Database) (metadata.Database, error) {
	if !ids.ValidDatabaseID(record.ID) ||
		!ids.ValidDatabaseInternalName(record.InternalName) ||
		!ids.ValidRoleInternalName(record.RoleName) {
		return metadata.Database{}, ErrProvisioning
	}
	current, err := s.metadata.GetDatabase(ctx, record.ID)
	if err != nil || current.ID != record.ID ||
		current.InternalName != record.InternalName || current.RoleName != record.RoleName ||
		(current.Status != metadata.StatusProvisioning && current.Status != metadata.StatusError) {
		return metadata.Database{}, ErrProvisioning
	}
	if err := s.postgres.ConfigureDatabasePrivileges(ctx, record.InternalName, record.RoleName); err != nil {
		return metadata.Database{}, ErrProvisioning
	}
	if err := s.verifyProvisionedState(ctx, record); err != nil {
		return metadata.Database{}, ErrProvisioning
	}
	ready, err := s.metadata.UpdateDatabaseStatus(ctx, record.ID, metadata.StatusReady)
	if err != nil {
		return metadata.Database{}, ErrProvisioning
	}
	return ready, nil
}

func (s *Service) MarkDatabaseRestoring(ctx context.Context, databaseID string) (metadata.Database, error) {
	if !ids.ValidDatabaseID(databaseID) {
		return metadata.Database{}, metadata.ErrInvalidIdentifier
	}
	record, err := s.metadata.UpdateDatabaseStatus(ctx, databaseID, metadata.StatusError)
	if err != nil {
		return metadata.Database{}, err
	}
	return record, nil
}

func (s *Service) MarkDatabaseError(ctx context.Context, databaseID string) error {
	if !ids.ValidDatabaseID(databaseID) {
		return metadata.ErrInvalidIdentifier
	}
	_, err := s.metadata.UpdateDatabaseStatus(ctx, databaseID, metadata.StatusError)
	return err
}

func (s *Service) CleanupRestoreTarget(record metadata.Database) error {
	if !ids.ValidDatabaseID(record.ID) ||
		!ids.ValidDatabaseInternalName(record.InternalName) ||
		!ids.ValidRoleInternalName(record.RoleName) {
		return ErrProvisioning
	}
	ctx, cancel := context.WithTimeout(context.Background(), s.compensationTimeout)
	defer cancel()
	current, err := s.metadata.GetDatabase(ctx, record.ID)
	if err != nil || current.ID != record.ID ||
		current.InternalName != record.InternalName || current.RoleName != record.RoleName ||
		current.Status != metadata.StatusProvisioning {
		return ErrProvisioning
	}
	if !s.compensate(compensationState{
		record:            current,
		metadataCreated:   true,
		credentialCreated: true,
		roleCreated:       true,
		databaseCreated:   true,
	}) {
		return ErrProvisioning
	}
	return nil
}

func (s *Service) Reconcile(ctx context.Context) error {
	records, err := s.metadata.ListProvisioningDatabases(ctx)
	if err != nil {
		return fmt.Errorf("list provisioning metadata: %w", err)
	}

	var reconciliationErrors []error
	for _, record := range records {
		status := metadata.StatusReady
		if err := s.verifyProvisionedState(ctx, record); err != nil {
			status = metadata.StatusError
		} else {
			exists, err := s.credentials.Exists(record.ID)
			if err != nil || !exists {
				status = metadata.StatusError
			}
		}
		if _, err := s.metadata.UpdateDatabaseStatus(ctx, record.ID, status); err != nil {
			reconciliationErrors = append(reconciliationErrors, fmt.Errorf("update reconciled metadata: %w", err))
		}
	}
	return errors.Join(reconciliationErrors...)
}

func (s *Service) verifyProvisionedState(ctx context.Context, record metadata.Database) error {
	if !ids.ValidDatabaseInternalName(record.InternalName) || !ids.ValidRoleInternalName(record.RoleName) {
		return ErrProvisioning
	}
	role, err := s.postgres.InspectRole(ctx, record.RoleName)
	if err != nil {
		return ErrProvisioning
	}
	if !role.Exists || !role.Login || role.Superuser || role.CreateDB || role.CreateRole || role.Replication || role.BypassRLS {
		return ErrProvisioning
	}
	database, err := s.postgres.InspectDatabase(ctx, record.InternalName, record.RoleName)
	if err != nil {
		return ErrProvisioning
	}
	if !database.Exists ||
		database.Owner != record.RoleName ||
		database.PublicConnect ||
		database.PublicSchemaCreate ||
		!database.OwnerSchemaCreate {
		return ErrProvisioning
	}
	return nil
}

type compensationState struct {
	record            metadata.Database
	metadataCreated   bool
	credentialCreated bool
	roleCreated       bool
	databaseCreated   bool
}

func (s *Service) compensate(state compensationState) bool {
	ctx, cancel := context.WithTimeout(context.Background(), s.compensationTimeout)
	defer cancel()

	databaseClean := true
	if state.databaseCreated {
		databaseClean = s.postgres.DropDatabase(ctx, state.record.InternalName) == nil
	}

	roleClean := !state.roleCreated
	if state.roleCreated && databaseClean {
		roleClean = s.postgres.DropRole(ctx, state.record.RoleName) == nil
	}

	credentialClean := !state.credentialCreated
	if state.credentialCreated && databaseClean && roleClean {
		credentialClean = s.credentials.Delete(state.record.ID) == nil
	}

	allExternalClean := databaseClean && roleClean && credentialClean
	if state.metadataCreated && allExternalClean {
		if err := s.metadata.DeleteDatabaseMetadata(ctx, state.record.ID); err == nil {
			return true
		}
	}
	if state.metadataCreated {
		_, _ = s.metadata.UpdateDatabaseStatus(ctx, state.record.ID, metadata.StatusError)
	}
	return false
}
