package observability

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

const (
	DiagnosticProfileHostOverview       = "host_overview"
	DiagnosticProfileVPNCoreStatus      = "vpn_core_status"
	DiagnosticProfileManagerCertificate = "manager_certificate"
)

type DiagnosticRunRecord struct {
	ID                  string         `json:"id"`
	ServerID            string         `json:"serverId"`
	AgentOperationJobID string         `json:"agentOperationJobId,omitempty"`
	ProfileKey          string         `json:"profileKey"`
	Status              string         `json:"status"`
	State               *HealthState   `json:"state,omitempty"`
	ResultPayload       map[string]any `json:"resultPayload,omitempty"`
	ReasonCode          string         `json:"reasonCode,omitempty"`
	Summary             string         `json:"summary,omitempty"`
	RecommendedAction   string         `json:"recommendedAction,omitempty"`
	ErrorMessage        string         `json:"errorMessage,omitempty"`
	RequestedByUserID   string         `json:"requestedByUserId,omitempty"`
	RequestedAt         time.Time      `json:"requestedAt"`
	StartedAt           *time.Time     `json:"startedAt,omitempty"`
	CompletedAt         *time.Time     `json:"completedAt,omitempty"`
	UpdatedAt           time.Time      `json:"updatedAt"`
}

type DiagnosticRepository struct {
	pool *pgxpool.Pool
}

func NewDiagnosticRepository(pool *pgxpool.Pool) *DiagnosticRepository {
	return &DiagnosticRepository{pool: pool}
}

func ValidDiagnosticProfile(profileKey string) bool {
	switch strings.TrimSpace(profileKey) {
	case DiagnosticProfileHostOverview, DiagnosticProfileVPNCoreStatus, DiagnosticProfileManagerCertificate:
		return true
	default:
		return false
	}
}

func (r *DiagnosticRepository) Create(ctx context.Context, serverID, profileKey, requestedByUserID string) (DiagnosticRunRecord, error) {
	profileKey = strings.TrimSpace(profileKey)
	if !ValidDiagnosticProfile(profileKey) {
		return DiagnosticRunRecord{}, fmt.Errorf("unsupported diagnostic profile %q", profileKey)
	}
	capability, err := json.Marshal(map[string]any{"diagnosticProfiles": []string{profileKey}})
	if err != nil {
		return DiagnosticRunRecord{}, err
	}

	return scanDiagnosticRun(r.pool.QueryRow(ctx, `
		WITH candidate_agent AS (
			SELECT id
			FROM agents
			WHERE server_id=$1::uuid
			  AND status <> 'disabled'
			  AND capabilities @> $3::jsonb
			ORDER BY updated_at DESC
			LIMIT 1
		), operation_job AS (
			INSERT INTO agent_operation_jobs (server_id, agent_id, kind, operation)
			SELECT $1::uuid, id, 'diagnostic', $2
			FROM candidate_agent
			RETURNING id
		)
		INSERT INTO observability_diagnostic_runs (
			server_id, agent_operation_job_id, profile_key, requested_by_user_id
		)
		SELECT $1::uuid, id, $2, NULLIF($4,'')::uuid
		FROM operation_job
		RETURNING
			id::text, server_id::text, COALESCE(agent_operation_job_id::text,''),
			profile_key, status, state, result_payload, COALESCE(reason_code,''),
			COALESCE(summary,''), COALESCE(recommended_action,''), COALESCE(error_message,''),
			COALESCE(requested_by_user_id::text,''), requested_at, started_at, completed_at, updated_at
	`, serverID, profileKey, capability, strings.TrimSpace(requestedByUserID)))
}

func (r *DiagnosticRepository) Get(ctx context.Context, serverID, runID string) (DiagnosticRunRecord, error) {
	return scanDiagnosticRun(r.pool.QueryRow(ctx, diagnosticSelect+`
		WHERE id=$1::uuid AND server_id=$2::uuid
	`, runID, serverID))
}

func (r *DiagnosticRepository) List(ctx context.Context, serverID string, limit int) ([]DiagnosticRunRecord, error) {
	if limit <= 0 || limit > 100 {
		limit = 20
	}
	rows, err := r.pool.Query(ctx, diagnosticSelect+`
		WHERE server_id=$1::uuid
		ORDER BY requested_at DESC, id DESC
		LIMIT $2
	`, serverID, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	items := make([]DiagnosticRunRecord, 0)
	for rows.Next() {
		item, err := scanDiagnosticRun(rows)
		if err != nil {
			return nil, err
		}
		items = append(items, item)
	}
	return items, rows.Err()
}

const diagnosticSelect = `
	SELECT
		id::text, server_id::text, COALESCE(agent_operation_job_id::text,''),
		profile_key, status, state, result_payload, COALESCE(reason_code,''),
		COALESCE(summary,''), COALESCE(recommended_action,''), COALESCE(error_message,''),
		COALESCE(requested_by_user_id::text,''), requested_at, started_at, completed_at, updated_at
	FROM observability_diagnostic_runs
`

func scanDiagnosticRun(row pgx.Row) (DiagnosticRunRecord, error) {
	var item DiagnosticRunRecord
	var state *string
	var payload []byte
	if err := row.Scan(
		&item.ID, &item.ServerID, &item.AgentOperationJobID,
		&item.ProfileKey, &item.Status, &state, &payload, &item.ReasonCode,
		&item.Summary, &item.RecommendedAction, &item.ErrorMessage,
		&item.RequestedByUserID, &item.RequestedAt, &item.StartedAt, &item.CompletedAt, &item.UpdatedAt,
	); err != nil {
		return DiagnosticRunRecord{}, err
	}
	if state != nil {
		value := HealthState(*state)
		item.State = &value
	}
	if len(payload) > 0 {
		if err := json.Unmarshal(payload, &item.ResultPayload); err != nil {
			return DiagnosticRunRecord{}, err
		}
	}
	return item, nil
}
