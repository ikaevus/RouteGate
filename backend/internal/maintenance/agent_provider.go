package maintenance

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"regexp"
	"sort"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/ikaevus/routegate/backend/internal/agents"
)

const (
	agentRuntimeRetentionDays = 30
	agentResultSchemaVersion  = 1
	agentCandidateLimit       = 256
	agentJobPollInterval      = 500 * time.Millisecond
)

var agentArtifactName = regexp.MustCompile(`^[0-9a-fA-F]{8}-[0-9a-fA-F]{4}-[1-5][0-9a-fA-F]{3}-[89aAbB][0-9a-fA-F]{3}-[0-9a-fA-F]{12}(?:\.previous)?\.(?:json|conf|toml)$`)
var canonicalAgentServerID = regexp.MustCompile(`^[0-9a-fA-F]{8}-[0-9a-fA-F]{4}-[1-5][0-9a-fA-F]{3}-[89aAbB][0-9a-fA-F]{3}-[0-9a-fA-F]{12}$`)

var agentArtifactRoots = map[string]struct{}{
	"sing_box_staging": {}, "sing_box_backups": {},
	"wireguard_staging": {}, "wireguard_backups": {},
	"hysteria2_staging": {}, "hysteria2_backups": {},
	"mtproto_staging": {}, "mtproto_backups": {},
}

type agentOperationRepository interface {
	CreateAgentOperationJobs(context.Context, []agents.CreateAgentOperationJobInput) ([]agents.AgentConfigTask, error)
	GetAgentOperationJob(context.Context, string, string) (agents.AgentConfigTask, error)
}

type agentRuntimeProvider struct {
	pool *pgxpool.Pool
	repo agentOperationRepository
}

type agentArtifactCandidate struct {
	RootID string `json:"rootId"`
	Name   string `json:"name"`
}

type agentResult struct {
	SchemaVersion  int                      `json:"schemaVersion"`
	Operation      string                   `json:"operation"`
	CandidateCount int64                    `json:"candidateCount"`
	EstimatedBytes int64                    `json:"estimatedBytes"`
	DeletedCount   int64                    `json:"deletedCount"`
	ReclaimedBytes int64                    `json:"reclaimedBytes"`
	RemainingCount int64                    `json:"remainingCount"`
	Candidates     []agentArtifactCandidate `json:"candidates"`
}

type agentSnapshot struct {
	ServerID   string                   `json:"serverId"`
	Candidates []agentArtifactCandidate `json:"candidates"`
}

type pendingAgentJob struct {
	serverID  string
	jobID     string
	operation string
}

type agentJobSpec struct {
	serverID   string
	candidates []agentArtifactCandidate
}

func newAgentRuntimeProvider(pool *pgxpool.Pool) Provider {
	return agentRuntimeProvider{pool: pool, repo: agents.NewRepository(pool)}
}

func (p agentRuntimeProvider) Category() Category {
	return Category{ID: "agent_runtime_artifacts", Scope: "agent", Selectable: true, RetentionDays: agentRuntimeRetentionDays}
}

func (p agentRuntimeProvider) Inventory(ctx context.Context, cutoff time.Time) (PlanItem, error) {
	if _, err := p.eligibleServers(ctx); err != nil {
		return PlanItem{}, err
	}
	var count, bytes int64
	err := p.pool.QueryRow(ctx, `
		WITH latest AS (
			SELECT DISTINCT ON (server_id)
				result_payload
			FROM agent_operation_jobs
			WHERE kind = 'maintenance'
			  AND operation = 'analyze_runtime_artifacts'
			  AND status = 'succeeded'
			  AND completed_at >= now() - interval '15 minutes'
			ORDER BY server_id, completed_at DESC, id DESC
		)
		SELECT
			COALESCE(sum((result_payload ->> 'candidateCount')::bigint), 0),
			COALESCE(sum((result_payload ->> 'estimatedBytes')::bigint), 0)
		FROM latest
	`).Scan(&count, &bytes)
	if err != nil {
		return PlanItem{}, err
	}
	return PlanItem{CategoryID: p.Category().ID, Scope: p.Category().Scope, RetentionDays: agentRuntimeRetentionDays, Cutoff: cutoff, CandidateCount: count, EstimatedBytes: bytes}, nil
}

func (p agentRuntimeProvider) Analyze(ctx context.Context, cutoff time.Time) (PlanItem, error) {
	serverIDs, err := p.eligibleServers(ctx)
	if err != nil {
		return PlanItem{}, err
	}
	specs := make([]agentJobSpec, 0, len(serverIDs))
	for _, serverID := range serverIDs {
		specs = append(specs, agentJobSpec{serverID: serverID})
	}
	jobs, err := p.createJobs(ctx, specs, agents.MaintenanceOperationAnalyze, cutoff)
	if err != nil {
		return PlanItem{}, err
	}
	var snapshots []agentSnapshot
	var count, bytes int64
	for _, job := range jobs {
		result, err := p.wait(ctx, job)
		if err != nil {
			return PlanItem{}, err
		}
		count += result.CandidateCount
		bytes += result.EstimatedBytes
		snapshots = append(snapshots, agentSnapshot{ServerID: job.serverID, Candidates: result.Candidates})
	}
	return PlanItem{
		CategoryID: p.Category().ID, Scope: p.Category().Scope, RetentionDays: agentRuntimeRetentionDays,
		Cutoff: cutoff, CandidateCount: count, EstimatedBytes: bytes,
		Metadata: map[string]any{"agentSnapshots": snapshots},
	}, nil
}

func (p agentRuntimeProvider) Cleanup(ctx context.Context, item PlanItem) (CleanupResult, error) {
	snapshots, err := decodeAgentSnapshots(item.Metadata)
	if err != nil {
		return CleanupResult{}, err
	}
	specs := make([]agentJobSpec, 0, len(snapshots))
	for _, snapshot := range snapshots {
		specs = append(specs, agentJobSpec{serverID: snapshot.ServerID, candidates: snapshot.Candidates})
	}
	jobs, err := p.createJobs(ctx, specs, agents.MaintenanceOperationCleanup, item.Cutoff)
	if err != nil {
		return CleanupResult{}, err
	}
	var result CleanupResult
	for _, job := range jobs {
		response, err := p.wait(ctx, job)
		if err != nil {
			return result, err
		}
		result.DeletedCount += response.DeletedCount
		result.ReclaimedBytes += response.ReclaimedBytes
	}
	return result, nil
}

func (p agentRuntimeProvider) Verify(ctx context.Context, item PlanItem) (int64, error) {
	snapshots, err := decodeAgentSnapshots(item.Metadata)
	if err != nil {
		return 0, err
	}
	specs := make([]agentJobSpec, 0, len(snapshots))
	for _, snapshot := range snapshots {
		specs = append(specs, agentJobSpec{serverID: snapshot.ServerID, candidates: snapshot.Candidates})
	}
	jobs, err := p.createJobs(ctx, specs, agents.MaintenanceOperationVerify, item.Cutoff)
	if err != nil {
		return 0, err
	}
	var remaining int64
	for _, job := range jobs {
		response, err := p.wait(ctx, job)
		if err != nil {
			return remaining, err
		}
		remaining += response.RemainingCount
	}
	return remaining, nil
}

func (p agentRuntimeProvider) eligibleServers(ctx context.Context) ([]string, error) {
	capabilities, err := json.Marshal(map[string]any{"maintenanceOperations": []string{
		agents.MaintenanceOperationAnalyze, agents.MaintenanceOperationCleanup, agents.MaintenanceOperationVerify,
	}})
	if err != nil {
		return nil, err
	}
	var total, eligible int64
	if err := p.pool.QueryRow(ctx, `
		SELECT
			count(*),
			count(*) FILTER (
				WHERE status = 'online'
				  AND last_seen_at >= now() - interval '2 minutes'
				  AND capabilities @> $1::jsonb
			)
		FROM agents
		WHERE status <> 'disabled'
	`, capabilities).Scan(&total, &eligible); err != nil {
		return nil, err
	}
	if total != eligible {
		return nil, fmt.Errorf("%w: not every active Agent supports online maintenance", ErrCategoryUnavailable)
	}

	rows, err := p.pool.Query(ctx, `
		SELECT server_id::text
		FROM agents
		WHERE status = 'online'
		  AND last_seen_at >= now() - interval '2 minutes'
		  AND capabilities @> $1::jsonb
		ORDER BY server_id
	`, capabilities)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var serverIDs []string
	for rows.Next() {
		var serverID string
		if err := rows.Scan(&serverID); err != nil {
			return nil, err
		}
		serverIDs = append(serverIDs, serverID)
	}
	return serverIDs, rows.Err()
}

func (p agentRuntimeProvider) createJobs(ctx context.Context, specs []agentJobSpec, operation string, cutoff time.Time) ([]pendingAgentJob, error) {
	inputs := make([]agents.CreateAgentOperationJobInput, 0, len(specs))
	for _, spec := range specs {
		payload := map[string]any{"schemaVersion": agentResultSchemaVersion, "cutoff": cutoff.UTC()}
		if operation != agents.MaintenanceOperationAnalyze {
			payload["candidates"] = spec.candidates
		}
		inputs = append(inputs, agents.CreateAgentOperationJobInput{
			ServerID: spec.serverID, Kind: agents.AgentTaskKindMaintenance, Operation: operation, RequestPayload: payload,
		})
	}
	created, err := p.repo.CreateAgentOperationJobs(ctx, inputs)
	if err != nil {
		return nil, fmt.Errorf("%w: create Agent maintenance jobs", ErrCategoryUnavailable)
	}
	if len(created) != len(specs) {
		return nil, ErrCategoryUnavailable
	}
	jobs := make([]pendingAgentJob, 0, len(created))
	for index, job := range created {
		jobs = append(jobs, pendingAgentJob{serverID: specs[index].serverID, jobID: job.ID, operation: operation})
	}
	return jobs, nil
}

func (p agentRuntimeProvider) wait(ctx context.Context, pending pendingAgentJob) (agentResult, error) {
	ticker := time.NewTicker(agentJobPollInterval)
	defer ticker.Stop()
	for {
		task, err := p.repo.GetAgentOperationJob(ctx, pending.serverID, pending.jobID)
		if err != nil && !errors.Is(err, pgx.ErrNoRows) {
			return agentResult{}, err
		}
		if err == nil {
			switch task.Status {
			case agents.AgentOperationJobStatusSucceeded:
				return validateAgentResult(task.ResultPayload, pending.operation)
			case agents.AgentOperationJobStatusFailed:
				return agentResult{}, fmt.Errorf("%w: Agent maintenance job failed", ErrCategoryUnavailable)
			}
		}
		select {
		case <-ctx.Done():
			return agentResult{}, ctx.Err()
		case <-ticker.C:
		}
	}
}

func validateAgentResult(payload map[string]any, operation string) (agentResult, error) {
	raw, err := json.Marshal(payload)
	if err != nil {
		return agentResult{}, err
	}
	var result agentResult
	if err := json.Unmarshal(raw, &result); err != nil {
		return agentResult{}, err
	}
	if result.SchemaVersion != agentResultSchemaVersion || result.Operation != operation || result.CandidateCount < 0 ||
		result.EstimatedBytes < 0 || result.DeletedCount < 0 || result.ReclaimedBytes < 0 || result.RemainingCount < 0 ||
		len(result.Candidates) > agentCandidateLimit || (operation == agents.MaintenanceOperationAnalyze && int64(len(result.Candidates)) != result.CandidateCount) {
		return agentResult{}, ErrUnsafeOwnership
	}
	seen := make(map[agentArtifactCandidate]struct{}, len(result.Candidates))
	for _, candidate := range result.Candidates {
		if _, ok := agentArtifactRoots[candidate.RootID]; !ok || !agentArtifactName.MatchString(candidate.Name) {
			return agentResult{}, ErrUnsafeOwnership
		}
		if _, duplicate := seen[candidate]; duplicate {
			return agentResult{}, ErrUnsafeOwnership
		}
		seen[candidate] = struct{}{}
	}
	return result, nil
}

func decodeAgentSnapshots(metadata map[string]any) ([]agentSnapshot, error) {
	value, ok := metadata["agentSnapshots"]
	if !ok {
		return nil, ErrUnsafeOwnership
	}
	raw, err := json.Marshal(value)
	if err != nil {
		return nil, ErrUnsafeOwnership
	}
	var snapshots []agentSnapshot
	if err := json.Unmarshal(raw, &snapshots); err != nil {
		return nil, ErrUnsafeOwnership
	}
	sort.Slice(snapshots, func(i, j int) bool { return snapshots[i].ServerID < snapshots[j].ServerID })
	seen := map[string]struct{}{}
	for _, snapshot := range snapshots {
		if !canonicalAgentServerID.MatchString(snapshot.ServerID) || len(snapshot.Candidates) > agentCandidateLimit {
			return nil, ErrUnsafeOwnership
		}
		if _, duplicate := seen[snapshot.ServerID]; duplicate {
			return nil, ErrUnsafeOwnership
		}
		seen[snapshot.ServerID] = struct{}{}
		if _, err := validateAgentResult(map[string]any{
			"schemaVersion": agentResultSchemaVersion, "operation": agents.MaintenanceOperationAnalyze,
			"candidateCount": len(snapshot.Candidates), "candidates": snapshot.Candidates,
		}, agents.MaintenanceOperationAnalyze); err != nil {
			return nil, err
		}
	}
	return snapshots, nil
}
