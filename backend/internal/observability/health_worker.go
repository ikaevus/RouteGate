package observability

import (
	"context"
	"log/slog"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
)

const HealthEvaluationInterval = 30 * time.Second

type HealthWorker struct {
	logger     *slog.Logger
	pool       *pgxpool.Pool
	repository *HealthRepository
	interval   time.Duration
}

func NewHealthWorker(logger *slog.Logger, pool *pgxpool.Pool) *HealthWorker {
	return &HealthWorker{
		logger:     logger,
		pool:       pool,
		repository: NewHealthRepository(pool),
		interval:   HealthEvaluationInterval,
	}
}

func (w *HealthWorker) Run(ctx context.Context) error {
	w.evaluateSafe(ctx, time.Now().UTC())
	ticker := time.NewTicker(w.interval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return nil
		case now := <-ticker.C:
			w.evaluateSafe(ctx, now.UTC())
		}
	}
}

func (w *HealthWorker) evaluateSafe(ctx context.Context, now time.Time) {
	if err := w.EvaluateOnce(ctx, now); err != nil {
		w.logger.Error("observability health evaluation failed", "error", err)
	}
}

func (w *HealthWorker) EvaluateOnce(ctx context.Context, now time.Time) error {
	rows, err := w.pool.Query(ctx, `
		SELECT
			server_id::text,
			schema_version,
			collected_at,
			received_at,
			host_load_1,
			host_load_5,
			host_load_15,
			host_logical_cpus,
			host_memory_total_bytes,
			host_memory_available_bytes,
			host_root_fs_total_bytes,
			host_root_fs_free_bytes,
			host_uptime_seconds,
			vpn_core_type,
			vpn_core_installed,
			COALESCE(vpn_core_version, ''),
			vpn_core_service_state
		FROM observability_agent_telemetry
	`)
	if err != nil {
		return err
	}
	defer rows.Close()

	for rows.Next() {
		var (
			serverID       string
			schemaVersion  int
			collectedAt    time.Time
			receivedAt     time.Time
			load1          *float64
			load5          *float64
			load15         *float64
			logicalCPUs    *int
			memoryTotal    *int64
			memoryAvailable *int64
			rootTotal      *int64
			rootFree       *int64
			uptime         *int64
			coreType       string
			coreInstalled  bool
			coreVersion    string
			coreState      string
		)
		if err := rows.Scan(
			&serverID,
			&schemaVersion,
			&collectedAt,
			&receivedAt,
			&load1,
			&load5,
			&load15,
			&logicalCPUs,
			&memoryTotal,
			&memoryAvailable,
			&rootTotal,
			&rootFree,
			&uptime,
			&coreType,
			&coreInstalled,
			&coreVersion,
			&coreState,
		); err != nil {
			return err
		}

		snapshot := AgentTelemetrySnapshot{
			SchemaVersion: schemaVersion,
			CollectedAt:   collectedAt,
			Host: AgentHostTelemetry{
				Load1:                load1,
				Load5:                load5,
				Load15:               load15,
				LogicalCPUs:           logicalCPUs,
				MemoryTotalBytes:      uint64FromInt64(memoryTotal),
				MemoryAvailableBytes:  uint64FromInt64(memoryAvailable),
				RootFSTotalBytes:      uint64FromInt64(rootTotal),
				RootFSFreeBytes:       uint64FromInt64(rootFree),
				UptimeSeconds:         uint64FromInt64(uptime),
			},
			VPNCore: AgentVPNCoreTelemetry{
				Type:         coreType,
				Installed:    coreInstalled,
				Version:      coreVersion,
				ServiceState: coreState,
			},
		}
		checks := EvaluateAgentTelemetry(ResourceRef{Type: "server", ID: serverID}, snapshot, receivedAt)
		if !now.Before(receivedAt.Add(AgentTelemetryHealthTTL)) {
			checks = markTelemetryChecksStale(checks, now, receivedAt)
		}
		if err := w.repository.ApplyChecks(ctx, checks); err != nil {
			return err
		}
	}
	return rows.Err()
}

func markTelemetryChecksStale(checks []HealthCheck, now, lastReceivedAt time.Time) []HealthCheck {
	for index := range checks {
		checks[index].State = HealthUnknown
		checks[index].ReasonCode = "telemetry_stale"
		checks[index].Summary = "Agent telemetry is stale."
		checks[index].RecommendedAction = "check_agent_connectivity"
		checks[index].Evidence = healthEvidence(map[string]any{
			"lastReceivedAt": lastReceivedAt.UTC(),
			"ttlSeconds":     int(AgentTelemetryHealthTTL.Seconds()),
		})
		checks[index].ObservedAt = now.UTC()
		checks[index].ExpiresAt = nil
	}
	return checks
}

func uint64FromInt64(value *int64) *uint64 {
	if value == nil || *value < 0 {
		return nil
	}
	converted := uint64(*value)
	return &converted
}
