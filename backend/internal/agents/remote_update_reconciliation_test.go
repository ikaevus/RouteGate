package agents

import (
	"strings"
	"testing"
)

func TestReconcilePlatformUpdateEvidence(t *testing.T) {
	const taskID = "123e4567-e89b-42d3-a456-426614174000"
	const version = "1.2.3"

	tests := []struct {
		name     string
		evidence PlatformUpdateReconciliationEvidence
		want     string
		wantErr  bool
	}{
		{name: "pending remains mutation dispatched", evidence: PlatformUpdateReconciliationEvidence{TaskID: taskID, TargetVersion: version, Status: PlatformUpdateReceiptStatusPending}, want: AgentOperationJobStatusMutationDispatched},
		{name: "success", evidence: PlatformUpdateReconciliationEvidence{TaskID: taskID, TargetVersion: version, Status: PlatformUpdateReceiptStatusSucceeded}, want: AgentOperationJobStatusSucceeded},
		{name: "failure", evidence: PlatformUpdateReconciliationEvidence{TaskID: taskID, TargetVersion: version, Status: PlatformUpdateReceiptStatusFailed, Code: "update_failed"}, want: AgentOperationJobStatusFailed},
		{name: "unknown", evidence: PlatformUpdateReconciliationEvidence{TaskID: taskID, TargetVersion: version, Status: PlatformUpdateReceiptStatusOutcomeUnknown, Code: "agent_restart_after_mutation_started"}, want: AgentOperationJobStatusOutcomeUnknown},
		{name: "task mismatch", evidence: PlatformUpdateReconciliationEvidence{TaskID: "123e4567-e89b-42d3-a456-426614174001", TargetVersion: version, Status: PlatformUpdateReceiptStatusSucceeded}, wantErr: true},
		{name: "version mismatch", evidence: PlatformUpdateReconciliationEvidence{TaskID: taskID, TargetVersion: "1.2.4", Status: PlatformUpdateReceiptStatusSucceeded}, wantErr: true},
		{name: "success with code", evidence: PlatformUpdateReconciliationEvidence{TaskID: taskID, TargetVersion: version, Status: PlatformUpdateReceiptStatusSucceeded, Code: "unexpected"}, wantErr: true},
		{name: "failed without code", evidence: PlatformUpdateReconciliationEvidence{TaskID: taskID, TargetVersion: version, Status: PlatformUpdateReceiptStatusFailed}, wantErr: true},
		{name: "failed code outside persistence contract", evidence: PlatformUpdateReconciliationEvidence{TaskID: taskID, TargetVersion: version, Status: PlatformUpdateReceiptStatusFailed, Code: "update-failed"}, wantErr: true},
		{name: "unsupported status", evidence: PlatformUpdateReconciliationEvidence{TaskID: taskID, TargetVersion: version, Status: "mutation_started"}, wantErr: true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := ReconcilePlatformUpdateEvidence(taskID, version, tt.evidence)
			if tt.wantErr {
				if err == nil {
					t.Fatal("expected error")
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if got != tt.want {
				t.Fatalf("got status %q, want %q", got, tt.want)
			}
		})
	}
}

func TestReconcilePlatformUpdateEvidenceCanonicalVersionParity(t *testing.T) {
	const taskID = "123e4567-e89b-42d3-a456-426614174000"
	const version = "v1.2.3-rc.1+build.7"
	evidence := PlatformUpdateReconciliationEvidence{TaskID: taskID, TargetVersion: version, Status: PlatformUpdateReceiptStatusSucceeded}
	if got, err := ReconcilePlatformUpdateEvidence(taskID, version, evidence); err != nil || got != AgentOperationJobStatusSucceeded {
		t.Fatalf("expected Agent-compatible version to reconcile, got status=%q err=%v", got, err)
	}
}

func TestReconcilePlatformUpdateEvidenceRejectsNonCanonicalIdentity(t *testing.T) {
	validEvidence := PlatformUpdateReconciliationEvidence{
		TaskID:        "123e4567-e89b-42d3-a456-426614174000",
		TargetVersion: "1.2.3",
		Status:        PlatformUpdateReceiptStatusPending,
	}
	if _, err := ReconcilePlatformUpdateEvidence("123E4567-E89B-42D3-A456-426614174000", "1.2.3", validEvidence); err == nil {
		t.Fatal("expected uppercase UUID to be rejected")
	}
	if _, err := ReconcilePlatformUpdateEvidence(validEvidence.TaskID, " 1.2.3", validEvidence); err == nil {
		t.Fatal("expected whitespace version to be rejected")
	}
}

func TestDecodePlatformUpdateReconciliationEvidenceFailClosed(t *testing.T) {
	valid := map[string]any{
		"taskId":        "123e4567-e89b-42d3-a456-426614174000",
		"targetVersion": "1.2.3",
		"status":        PlatformUpdateReceiptStatusPending,
	}
	if evidence, err := DecodePlatformUpdateReconciliationEvidence(valid); err != nil || evidence.Status != PlatformUpdateReceiptStatusPending {
		t.Fatalf("expected valid evidence, got evidence=%+v err=%v", evidence, err)
	}

	withPrivilegedSelector := map[string]any{
		"taskId":        valid["taskId"],
		"targetVersion": valid["targetVersion"],
		"status":        valid["status"],
		"path":          "/tmp/forbidden",
	}
	if _, err := DecodePlatformUpdateReconciliationEvidence(withPrivilegedSelector); err == nil {
		t.Fatal("expected unknown privileged selector to be rejected")
	}

	oversized := map[string]any{
		"taskId":        valid["taskId"],
		"targetVersion": valid["targetVersion"],
		"status":        PlatformUpdateReceiptStatusFailed,
		"code":          strings.Repeat("a", maxPlatformUpdateReconciliationBytes),
	}
	if _, err := DecodePlatformUpdateReconciliationEvidence(oversized); err == nil {
		t.Fatal("expected oversized evidence to be rejected")
	}
}
