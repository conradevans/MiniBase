package api

import (
	"context"
	"encoding/json"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/conradevans/MiniBase/internal/metadata"
)

type fakeReactorLabBackups struct {
	backups []metadata.Backup
	err     error
}

func (fake fakeReactorLabBackups) ListBackups(
	_ context.Context,
) ([]metadata.Backup, error) {
	if fake.err != nil {
		return nil, fake.err
	}
	return fake.backups, nil
}

type fakeReactorLabRuntime struct {
	stats    map[string]reactorLabDatabaseRuntimeStats
	postgres reactorLabPostgresMetrics
	err      error
}

func (fake fakeReactorLabRuntime) databaseStats(
	_ context.Context,
) (map[string]reactorLabDatabaseRuntimeStats, error) {
	if fake.err != nil {
		return nil, fake.err
	}
	return fake.stats, nil
}

func (fake fakeReactorLabRuntime) postgresMetrics(
	_ context.Context,
) (reactorLabPostgresMetrics, error) {
	if fake.err != nil {
		return reactorLabPostgresMetrics{}, fake.err
	}
	return fake.postgres, nil
}

func TestReactorLabDatabaseObservabilityIsAllowlisted(t *testing.T) {
	server, store := testServer(t)
	ctx := context.Background()

	first, err := store.CreateDatabaseMetadata(ctx, "MyScheduler Production")
	if err != nil {
		t.Fatal(err)
	}
	second, err := store.CreateDatabaseMetadata(ctx, "Golfmullet Production")
	if err != nil {
		t.Fatal(err)
	}

	first, err = store.UpdateDatabaseStatus(
		ctx,
		first.ID,
		metadata.StatusReady,
	)
	if err != nil {
		t.Fatal(err)
	}

	attachment, err := store.CreateAttachment(
		ctx,
		first.ID,
		metadata.ConsumerTypeMiniDeploy,
		"myscheduler",
		metadata.BindingNamePrimary,
	)
	if err != nil {
		t.Fatal(err)
	}

	completed := time.Now().UTC().Add(-2 * time.Hour)
	server.reactorLabBackups = fakeReactorLabBackups{
		backups: []metadata.Backup{
			{
				ID:          "backup_11111111111111111111111111111111",
				DatabaseID:  first.ID,
				Kind:        metadata.BackupKindAutomatic,
				Status:      metadata.BackupStatusReady,
				SizeBytes:   62000,
				CreatedAt:   completed.Add(-time.Minute),
				CompletedAt: &completed,
			},
			{
				ID:         "backup_22222222222222222222222222222222",
				DatabaseID: first.ID,
				Kind:       metadata.BackupKindManual,
				Status:     metadata.BackupStatusError,
				CreatedAt:  completed.Add(time.Hour),
			},
		},
	}
	server.reactorLabRuntime = fakeReactorLabRuntime{
		stats: map[string]reactorLabDatabaseRuntimeStats{
			first.InternalName: {
				SizeBytes:         9000000,
				Connections:       1,
				ActiveConnections: 0,
				IdleConnections:   1,
				Commits:           100,
				Rollbacks:         2,
				BlockReads:        3,
				BlockHits:         400,
				RowsInserted:      50,
				RowsUpdated:       20,
				RowsDeleted:       5,
			},
			second.InternalName: {
				SizeBytes: 7000000,
				Commits:   80,
			},
		},
		postgres: reactorLabPostgresMetrics{
			State:            "running",
			CPUPercent:       0.2,
			MemoryUsedBytes:  70 * 1024 * 1024,
			MemoryLimitBytes: 15 * 1024 * 1024 * 1024,
			MemoryPercent:    0.5,
			NetworkRXBytes:   24000,
			NetworkTXBytes:   15000,
			BlockReadBytes:   57000000,
			BlockWriteBytes:  900000,
			PIDs:             7,
		},
	}

	response := request(
		t,
		server,
		http.MethodGet,
		reactorLabDatabasesPath,
	)
	if response.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s", response.Code, response.Body.String())
	}

	var body reactorLabObservabilityResponse
	if err := json.Unmarshal(response.Body.Bytes(), &body); err != nil {
		t.Fatal(err)
	}
	if len(body.Databases) != 2 {
		t.Fatalf("database count = %d, want 2", len(body.Databases))
	}
	if body.Databases[0].ID != first.ID ||
		body.Databases[0].DisplayName != first.DisplayName ||
		body.Databases[0].SizeBytes != 9000000 ||
		body.Databases[0].Connections != 1 ||
		body.Databases[0].BackupCount != 2 ||
		body.Databases[0].BackupBytes != 62000 ||
		body.Databases[0].LatestBackupAt == nil ||
		body.Databases[0].BackupAgeSeconds == nil {
		t.Fatalf("first database = %#v", body.Databases[0])
	}
	if len(body.Databases[0].Attachments) != 1 {
		t.Fatalf(
			"first database attachments = %#v",
			body.Databases[0].Attachments,
		)
	}
	gotAttachment := body.Databases[0].Attachments[0]
	if gotAttachment.ConsumerType != metadata.ConsumerTypeMiniDeploy ||
		gotAttachment.ConsumerRef != "myscheduler" ||
		gotAttachment.BindingName != metadata.BindingNamePrimary {
		t.Fatalf("attachment = %#v", gotAttachment)
	}
	if body.Databases[1].Attachments == nil ||
		len(body.Databases[1].Attachments) != 0 {
		t.Fatalf(
			"second database attachments = %#v",
			body.Databases[1].Attachments,
		)
	}

	if body.Postgres.State != "running" || body.Postgres.PIDs != 7 {
		t.Fatalf("postgres = %#v", body.Postgres)
	}

	raw := response.Body.String()
	for _, forbidden := range []string{
		"internalName",
		"roleName",
		first.InternalName,
		second.InternalName,
		"DATABASE_URL",
		"password",
		"credential",
		attachment.ID,
		"attachment_",
	} {
		if strings.Contains(raw, forbidden) {
			t.Fatalf("response leaked %q: %s", forbidden, raw)
		}
	}
}

func TestPublicHandlerNeverExposesReactorLabObservability(t *testing.T) {
	server, _ := testServer(t)
	handler := PublicHandler(server, fakePublicAccessValidator{})

	for _, token := range []string{"", "accepted-token"} {
		response := publicRequest(
			t,
			handler,
			reactorLabDatabasesPath,
			token,
		)
		if response.Code != http.StatusNotFound {
			t.Fatalf(
				"token %q status = %d, want %d",
				token,
				response.Code,
				http.StatusNotFound,
			)
		}
	}
}

func TestReactorLabDatabaseObservabilityRequiresGET(t *testing.T) {
	server, _ := testServer(t)
	response := request(
		t,
		server,
		http.MethodPost,
		reactorLabDatabasesPath,
	)
	if response.Code != http.StatusMethodNotAllowed {
		t.Fatalf("status = %d, want %d", response.Code, http.StatusMethodNotAllowed)
	}
	if response.Header().Get("Allow") != http.MethodGet {
		t.Fatalf("Allow = %q", response.Header().Get("Allow"))
	}
}

func TestParsePostgresDatabaseStats(t *testing.T) {
	output := strings.Join([]string{
		"mb_db_one|9000627|1|15305|5|204|735365|2570|487|1267|0|1",
		"mb_db_two|7689907|0|12533|1|84|472873|7|2|0|0|0",
		"postgres|7689907|1|111624|0|492|3709040|0|0|0|1|0",
	}, "\n")

	stats, err := parsePostgresDatabaseStats(output)
	if err != nil {
		t.Fatal(err)
	}
	if len(stats) != 3 {
		t.Fatalf("stats count = %d, want 3", len(stats))
	}
	first := stats["mb_db_one"]
	if first.SizeBytes != 9000627 ||
		first.Connections != 1 ||
		first.Commits != 15305 ||
		first.ActiveConnections != 0 ||
		first.IdleConnections != 1 {
		t.Fatalf("first stats = %#v", first)
	}
}

func TestDockerMetricParsers(t *testing.T) {
	cases := map[string]int64{
		"71.89MiB": int64(mathRound(71.89 * 1024 * 1024)),
		"14.97GiB": int64(mathRound(14.97 * 1024 * 1024 * 1024)),
		"24.1kB":   24100,
		"57.2MB":   57200000,
		"897kB":    897000,
	}

	for input, want := range cases {
		got, err := parseDockerBytes(input)
		if err != nil {
			t.Fatalf("parseDockerBytes(%q): %v", input, err)
		}
		if got != want {
			t.Fatalf("parseDockerBytes(%q) = %d, want %d", input, got, want)
		}
	}

	first, second, err := parseDockerBytePair("24.1kB / 15.2kB")
	if err != nil {
		t.Fatal(err)
	}
	if first != 24100 || second != 15200 {
		t.Fatalf("pair = %d/%d", first, second)
	}
}

func mathRound(value float64) float64 {
	if value < 0 {
		return float64(int64(value - 0.5))
	}
	return float64(int64(value + 0.5))
}
