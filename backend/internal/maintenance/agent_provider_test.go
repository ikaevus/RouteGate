package maintenance

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/ikaevus/routegate/backend/internal/agents"
)

type fakeAgentOperationRepository struct {
	jobs   map[string]agents.AgentConfigTask
	inputs []agents.CreateAgentOperationJobInput
}

func (r *fakeAgentOperationRepository) CreateAgentOperationJobs(_ context.Context, inputs []agents.CreateAgentOperationJobInput) ([]agents.AgentConfigTask, error) {
	jobs := make([]agents.AgentConfigTask, 0, len(inputs))
	if r.jobs == nil {
		r.jobs = map[string]agents.AgentConfigTask{}
	}
	for _, input := range inputs {
		r.inputs = append(r.inputs, input)
		id := fmt.Sprintf("job-%d", len(r.inputs))
		result := map[string]any{"schemaVersion": 1, "operation": input.Operation}
		switch input.Operation {
		case agents.MaintenanceOperationCleanup:
			result["deletedCount"] = 1
			result["reclaimedBytes"] = 64
		case agents.MaintenanceOperationVerify:
			result["remainingCount"] = 0
		}
		task := agents.AgentConfigTask{ID: id, ServerID: input.ServerID, Status: agents.AgentOperationJobStatusSucceeded, ResultPayload: result}
		r.jobs[id] = task
		jobs = append(jobs, task)
	}
	return jobs, nil
}

func (r *fakeAgentOperationRepository) GetAgentOperationJob(_ context.Context, _, jobID string) (agents.AgentConfigTask, error) {
	return r.jobs[jobID], nil
}

func TestAgentRuntimeProviderDispatchesExactPlannedCandidates(t *testing.T) {
	repo := &fakeAgentOperationRepository{}
	provider := agentRuntimeProvider{repo: repo}
	candidate := agentArtifactCandidate{RootID: "sing_box_staging", Name: "11111111-1111-4111-8111-111111111111.json"}
	item := PlanItem{
		CategoryID: "agent_runtime_artifacts", Cutoff: fixedNow().Add(-30 * 24 * time.Hour),
		Metadata: map[string]any{"agentSnapshots": []agentSnapshot{{ServerID: "aaaaaaaa-aaaa-4aaa-8aaa-aaaaaaaaaaaa", Candidates: []agentArtifactCandidate{candidate}}}},
	}
	result, err := provider.Cleanup(context.Background(), item)
	if err != nil {
		t.Fatal(err)
	}
	remaining, err := provider.Verify(context.Background(), item)
	if err != nil {
		t.Fatal(err)
	}
	if result.DeletedCount != 1 || result.ReclaimedBytes != 64 || remaining != 0 || len(repo.inputs) != 2 {
		t.Fatalf("result=%#v remaining=%d inputs=%#v", result, remaining, repo.inputs)
	}
	cleanupCandidates, ok := repo.inputs[0].RequestPayload["candidates"].([]agentArtifactCandidate)
	if !ok || len(cleanupCandidates) != 1 || cleanupCandidates[0] != candidate {
		t.Fatalf("cleanup payload lost exact candidates: %#v", repo.inputs[0].RequestPayload)
	}
}

func TestAgentRuntimeMetadataRejectsArbitraryPaths(t *testing.T) {
	_, err := decodeAgentSnapshots(map[string]any{"agentSnapshots": []agentSnapshot{{
		ServerID:   "aaaaaaaa-aaaa-4aaa-8aaa-aaaaaaaaaaaa",
		Candidates: []agentArtifactCandidate{{RootID: "sing_box_staging", Name: "../../etc/passwd"}},
	}}})
	if err == nil {
		t.Fatal("arbitrary Agent path was accepted")
	}
}

func TestAgentRuntimeMetadataRequiresPersistedSnapshot(t *testing.T) {
	if _, err := decodeAgentSnapshots(nil); err == nil {
		t.Fatal("missing Agent snapshot metadata was accepted")
	}
}
