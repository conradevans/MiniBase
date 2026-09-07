package api

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"math"
	"net/http"
	"os/exec"
	"strconv"
	"strings"
	"time"

	"github.com/conradevans/MiniBase/internal/metadata"
)

const (
	reactorLabDatabasesPath  = "/internal/reactorlab/databases"
	reactorLabCommandTimeout = 8 * time.Second
)

var errReactorLabObservability = errors.New("ReactorLab observability unavailable")

type reactorLabBackupReader interface {
	ListBackups(context.Context) ([]metadata.Backup, error)
}

type reactorLabAttachmentReader interface {
	ListAttachments(context.Context) ([]metadata.Attachment, error)
}

type reactorLabRuntime interface {
	databaseStats(context.Context) (map[string]reactorLabDatabaseRuntimeStats, error)
	postgresMetrics(context.Context) (reactorLabPostgresMetrics, error)
}

type reactorLabObservabilityResponse struct {
	Databases   []reactorLabDatabaseMetrics `json:"databases"`
	Postgres    reactorLabPostgresMetrics   `json:"postgres"`
	CollectedAt time.Time                   `json:"collectedAt"`
}

type reactorLabDatabaseMetrics struct {
	ID                string                  `json:"id"`
	DisplayName       string                  `json:"displayName"`
	Status            metadata.DatabaseStatus `json:"status"`
	Attachments       []reactorLabAttachment  `json:"attachments"`
	SizeBytes         int64                   `json:"sizeBytes"`
	Connections       int64                   `json:"connections"`
	ActiveConnections int64                   `json:"activeConnections"`
	IdleConnections   int64                   `json:"idleConnections"`
	Transactions      reactorLabTransactions  `json:"transactions"`
	Cache             reactorLabCacheMetrics  `json:"cache"`
	Rows              reactorLabRowMetrics    `json:"rows"`
	BackupCount       int                     `json:"backupCount"`
	BackupBytes       int64                   `json:"backupBytes"`
	LatestBackupAt    *time.Time              `json:"latestBackupAt,omitempty"`
	BackupAgeSeconds  *float64                `json:"backupAgeSeconds,omitempty"`
}

type reactorLabAttachment struct {
	ConsumerType string `json:"consumerType"`
	ConsumerRef  string `json:"consumerRef"`
	BindingName  string `json:"bindingName"`
}

type reactorLabTransactions struct {
	Commits   int64 `json:"commits"`
	Rollbacks int64 `json:"rollbacks"`
}

type reactorLabCacheMetrics struct {
	BlockReads int64 `json:"blockReads"`
	BlockHits  int64 `json:"blockHits"`
}

type reactorLabRowMetrics struct {
	Inserted int64 `json:"inserted"`
	Updated  int64 `json:"updated"`
	Deleted  int64 `json:"deleted"`
}

type reactorLabDatabaseRuntimeStats struct {
	SizeBytes         int64
	Connections       int64
	ActiveConnections int64
	IdleConnections   int64
	Commits           int64
	Rollbacks         int64
	BlockReads        int64
	BlockHits         int64
	RowsInserted      int64
	RowsUpdated       int64
	RowsDeleted       int64
}

type reactorLabPostgresMetrics struct {
	State            string  `json:"state"`
	CPUPercent       float64 `json:"cpuPercent"`
	MemoryUsedBytes  int64   `json:"memoryUsedBytes"`
	MemoryLimitBytes int64   `json:"memoryLimitBytes"`
	MemoryPercent    float64 `json:"memoryPercent"`
	NetworkRXBytes   int64   `json:"networkRxBytes"`
	NetworkTXBytes   int64   `json:"networkTxBytes"`
	BlockReadBytes   int64   `json:"blockReadBytes"`
	BlockWriteBytes  int64   `json:"blockWriteBytes"`
	PIDs             int64   `json:"pids"`
}

type commandReactorLabRuntime struct{}

type dockerStatsRow struct {
	CPUPerc  string `json:"CPUPerc"`
	MemUsage string `json:"MemUsage"`
	MemPerc  string `json:"MemPerc"`
	NetIO    string `json:"NetIO"`
	BlockIO  string `json:"BlockIO"`
	PIDs     string `json:"PIDs"`
}

func (s *Server) handleReactorLabDatabases(
	response http.ResponseWriter,
	request *http.Request,
) {
	if s.reactorLabBackups == nil ||
		s.attachments == nil ||
		s.reactorLabRuntime == nil {
		writeError(
			response,
			http.StatusServiceUnavailable,
			"service_unavailable",
			"service unavailable",
		)
		return
	}

	result, err := collectReactorLabObservability(
		request.Context(),
		s.store,
		s.reactorLabBackups,
		s.attachments,
		s.reactorLabRuntime,
		time.Now().UTC(),
	)
	if err != nil {
		s.logger.Error("ReactorLab database observability failed")
		writeError(
			response,
			http.StatusServiceUnavailable,
			"service_unavailable",
			"service unavailable",
		)
		return
	}

	writeJSON(response, http.StatusOK, result)
}

func collectReactorLabObservability(
	ctx context.Context,
	store metadataReader,
	backups reactorLabBackupReader,
	attachments reactorLabAttachmentReader,
	runtime reactorLabRuntime,
	now time.Time,
) (reactorLabObservabilityResponse, error) {
	databases, err := store.ListDatabases(ctx)
	if err != nil {
		return reactorLabObservabilityResponse{}, errReactorLabObservability
	}

	backupRecords, err := backups.ListBackups(ctx)
	if err != nil {
		return reactorLabObservabilityResponse{}, errReactorLabObservability
	}

	attachmentRecords, err := attachments.ListAttachments(ctx)
	if err != nil {
		return reactorLabObservabilityResponse{}, errReactorLabObservability
	}

	databaseStats, err := runtime.databaseStats(ctx)
	if err != nil {
		return reactorLabObservabilityResponse{}, errReactorLabObservability
	}

	postgres, err := runtime.postgresMetrics(ctx)
	if err != nil {
		return reactorLabObservabilityResponse{}, errReactorLabObservability
	}

	backupsByDatabase := make(map[string][]metadata.Backup)
	for _, backup := range backupRecords {
		backupsByDatabase[backup.DatabaseID] = append(
			backupsByDatabase[backup.DatabaseID],
			backup,
		)
	}

	attachmentsByDatabase := make(map[string][]reactorLabAttachment)
	for _, attachment := range attachmentRecords {
		if attachment.ConsumerType != metadata.ConsumerTypeMiniDeploy ||
			attachment.BindingName != metadata.BindingNamePrimary {
			continue
		}

		attachmentsByDatabase[attachment.DatabaseID] = append(
			attachmentsByDatabase[attachment.DatabaseID],
			reactorLabAttachment{
				ConsumerType: attachment.ConsumerType,
				ConsumerRef:  attachment.ConsumerRef,
				BindingName:  attachment.BindingName,
			},
		)
	}

	output := make([]reactorLabDatabaseMetrics, 0, len(databases))
	for _, database := range databases {
		stats, exists := databaseStats[database.InternalName]
		if database.Status == metadata.StatusReady && !exists {
			return reactorLabObservabilityResponse{}, errReactorLabObservability
		}
		safeAttachments := attachmentsByDatabase[database.ID]
		if safeAttachments == nil {
			safeAttachments = []reactorLabAttachment{}
		}

		item := reactorLabDatabaseMetrics{
			ID:                database.ID,
			DisplayName:       database.DisplayName,
			Status:            database.Status,
			Attachments:       safeAttachments,
			SizeBytes:         stats.SizeBytes,
			Connections:       stats.Connections,
			ActiveConnections: stats.ActiveConnections,
			IdleConnections:   stats.IdleConnections,
			Transactions: reactorLabTransactions{
				Commits:   stats.Commits,
				Rollbacks: stats.Rollbacks,
			},
			Cache: reactorLabCacheMetrics{
				BlockReads: stats.BlockReads,
				BlockHits:  stats.BlockHits,
			},
			Rows: reactorLabRowMetrics{
				Inserted: stats.RowsInserted,
				Updated:  stats.RowsUpdated,
				Deleted:  stats.RowsDeleted,
			},
		}

		for _, backup := range backupsByDatabase[database.ID] {
			item.BackupCount++
			if backup.Status != metadata.BackupStatusReady {
				continue
			}
			item.BackupBytes += backup.SizeBytes
			if backup.CompletedAt == nil {
				continue
			}
			completed := backup.CompletedAt.UTC()
			if item.LatestBackupAt == nil ||
				completed.After(*item.LatestBackupAt) {
				item.LatestBackupAt = &completed
			}
		}

		if item.LatestBackupAt != nil {
			age := now.Sub(*item.LatestBackupAt).Seconds()
			if age < 0 {
				age = 0
			}
			item.BackupAgeSeconds = &age
		}

		output = append(output, item)
	}

	return reactorLabObservabilityResponse{
		Databases:   output,
		Postgres:    postgres,
		CollectedAt: now,
	}, nil
}

func (commandReactorLabRuntime) databaseStats(
	ctx context.Context,
) (map[string]reactorLabDatabaseRuntimeStats, error) {
	const query = `
WITH activity AS (
	SELECT
		datname,
		count(*) FILTER (WHERE state = 'active') AS active_connections,
		count(*) FILTER (WHERE state = 'idle') AS idle_connections
	FROM pg_stat_activity
	WHERE datname IS NOT NULL
	GROUP BY datname
)
SELECT
	d.datname,
	pg_database_size(d.datname),
	COALESCE(s.numbackends, 0),
	COALESCE(s.xact_commit, 0),
	COALESCE(s.xact_rollback, 0),
	COALESCE(s.blks_read, 0),
	COALESCE(s.blks_hit, 0),
	COALESCE(s.tup_inserted, 0),
	COALESCE(s.tup_updated, 0),
	COALESCE(s.tup_deleted, 0),
	COALESCE(a.active_connections, 0),
	COALESCE(a.idle_connections, 0)
FROM pg_database d
LEFT JOIN pg_stat_database s ON s.datid = d.oid
LEFT JOIN activity a ON a.datname = d.datname
WHERE NOT d.datistemplate
ORDER BY d.datname;`

	output, err := runReactorLabCommand(
		ctx,
		"docker",
		"exec",
		"minibase-postgres",
		"psql",
		"-X",
		"--no-psqlrc",
		"-U",
		"minibase_admin",
		"-d",
		"postgres",
		"-A",
		"-t",
		"-q",
		"-F",
		"|",
		"-c",
		query,
	)
	if err != nil {
		return nil, errReactorLabObservability
	}

	return parsePostgresDatabaseStats(output)
}

func (commandReactorLabRuntime) postgresMetrics(
	ctx context.Context,
) (reactorLabPostgresMetrics, error) {
	output, err := runReactorLabCommand(
		ctx,
		"docker",
		"stats",
		"--no-stream",
		"--format",
		"{{json .}}",
		"minibase-postgres",
	)
	if err != nil {
		return reactorLabPostgresMetrics{}, errReactorLabObservability
	}

	var row dockerStatsRow
	if err := json.Unmarshal([]byte(strings.TrimSpace(output)), &row); err != nil {
		return reactorLabPostgresMetrics{}, errReactorLabObservability
	}

	cpu, err := parseDockerPercent(row.CPUPerc)
	if err != nil {
		return reactorLabPostgresMetrics{}, errReactorLabObservability
	}
	memoryPercent, err := parseDockerPercent(row.MemPerc)
	if err != nil {
		return reactorLabPostgresMetrics{}, errReactorLabObservability
	}
	memoryUsed, memoryLimit, err := parseDockerBytePair(row.MemUsage)
	if err != nil {
		return reactorLabPostgresMetrics{}, errReactorLabObservability
	}
	networkRX, networkTX, err := parseDockerBytePair(row.NetIO)
	if err != nil {
		return reactorLabPostgresMetrics{}, errReactorLabObservability
	}
	blockRead, blockWrite, err := parseDockerBytePair(row.BlockIO)
	if err != nil {
		return reactorLabPostgresMetrics{}, errReactorLabObservability
	}
	pids, err := strconv.ParseInt(strings.TrimSpace(row.PIDs), 10, 64)
	if err != nil {
		return reactorLabPostgresMetrics{}, errReactorLabObservability
	}

	return reactorLabPostgresMetrics{
		State:            "running",
		CPUPercent:       cpu,
		MemoryUsedBytes:  memoryUsed,
		MemoryLimitBytes: memoryLimit,
		MemoryPercent:    memoryPercent,
		NetworkRXBytes:   networkRX,
		NetworkTXBytes:   networkTX,
		BlockReadBytes:   blockRead,
		BlockWriteBytes:  blockWrite,
		PIDs:             pids,
	}, nil
}

func runReactorLabCommand(
	ctx context.Context,
	name string,
	args ...string,
) (string, error) {
	operationContext, cancel := context.WithTimeout(
		ctx,
		reactorLabCommandTimeout,
	)
	defer cancel()

	command := exec.CommandContext(operationContext, name, args...)
	command.Stderr = io.Discard
	output, err := command.Output()
	if err != nil {
		return "", errReactorLabObservability
	}
	return string(output), nil
}

func parsePostgresDatabaseStats(
	output string,
) (map[string]reactorLabDatabaseRuntimeStats, error) {
	result := make(map[string]reactorLabDatabaseRuntimeStats)

	for _, line := range strings.Split(strings.TrimSpace(output), "\n") {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		fields := strings.Split(line, "|")
		if len(fields) != 12 || strings.TrimSpace(fields[0]) == "" {
			return nil, errReactorLabObservability
		}

		values := make([]int64, 11)
		for index, field := range fields[1:] {
			value, err := strconv.ParseInt(strings.TrimSpace(field), 10, 64)
			if err != nil || value < 0 {
				return nil, errReactorLabObservability
			}
			values[index] = value
		}

		result[fields[0]] = reactorLabDatabaseRuntimeStats{
			SizeBytes:         values[0],
			Connections:       values[1],
			Commits:           values[2],
			Rollbacks:         values[3],
			BlockReads:        values[4],
			BlockHits:         values[5],
			RowsInserted:      values[6],
			RowsUpdated:       values[7],
			RowsDeleted:       values[8],
			ActiveConnections: values[9],
			IdleConnections:   values[10],
		}
	}

	return result, nil
}

func parseDockerPercent(value string) (float64, error) {
	trimmed := strings.TrimSpace(strings.TrimSuffix(value, "%"))
	number, err := strconv.ParseFloat(trimmed, 64)
	if err != nil || number < 0 {
		return 0, errReactorLabObservability
	}
	return number, nil
}

func parseDockerBytePair(value string) (int64, int64, error) {
	parts := strings.Split(value, "/")
	if len(parts) != 2 {
		return 0, 0, errReactorLabObservability
	}
	first, err := parseDockerBytes(parts[0])
	if err != nil {
		return 0, 0, err
	}
	second, err := parseDockerBytes(parts[1])
	if err != nil {
		return 0, 0, err
	}
	return first, second, nil
}

func parseDockerBytes(value string) (int64, error) {
	value = strings.TrimSpace(value)
	if value == "" {
		return 0, errReactorLabObservability
	}

	index := 0
	for index < len(value) {
		character := value[index]
		if (character >= '0' && character <= '9') || character == '.' {
			index++
			continue
		}
		break
	}
	if index == 0 {
		return 0, errReactorLabObservability
	}

	number, err := strconv.ParseFloat(value[:index], 64)
	if err != nil || number < 0 {
		return 0, errReactorLabObservability
	}

	unit := strings.TrimSpace(value[index:])
	factors := map[string]float64{
		"B":   1,
		"kB":  1000,
		"MB":  1000 * 1000,
		"GB":  1000 * 1000 * 1000,
		"TB":  1000 * 1000 * 1000 * 1000,
		"KiB": 1024,
		"MiB": 1024 * 1024,
		"GiB": 1024 * 1024 * 1024,
		"TiB": 1024 * 1024 * 1024 * 1024,
	}
	factor, ok := factors[unit]
	if !ok {
		return 0, errReactorLabObservability
	}

	return int64(math.Round(number * factor)), nil
}
