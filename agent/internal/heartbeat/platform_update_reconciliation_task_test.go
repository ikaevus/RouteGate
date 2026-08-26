package heartbeat

import (
	"testing"
	"time"

	"github.com/ikaevus/routegate/agent/internal/tasks"
)

func TestPlatformUpdateReconciliationReportIsBoundedProjection(t *testing.T) {
	reconciliation := tasks.PlatformUpdateReconciliation{
		TaskID:        "123e4567-e89b-42d3-a456-426614174000",
		TargetVersion: "v1.2.3+build.7",
		State:         tasks.PlatformUpdateReconciliationOutcomeUnknown,
		Code:          "agent_restart_after_mutation_started",
		CreatedAt:     time.Now().UTC(),
		UpdatedAt:     time.Now().UTC(),
	}
	report := platformUpdateReconciliationReport(reconciliation)
	if len(report) != 4 {
		t.Fatalf("expected exactly four bounded fields, got %#v", report)
	}
	for _, key := range []string{"taskId", "targetVersion", "status", "code"} {
		if _, ok := report[key]; !ok {
			t.Fatalf("expected field %q in report", key)
		}
	}
	for _, forbidden := range []string{"path", "url", "command", "role", "signer", "trustRoot", "output", "createdAt", "updatedAt"} {
		if _, ok := report[forbidden]; ok {
			t.Fatalf("forbidden field %q leaked into report", forbidden)
		}
	}
}

func TestPlatformUpdateReconciliationReportOmitsEmptyCode(t *testing.T) {
	report := platformUpdateReconciliationReport(tasks.PlatformUpdateReconciliation{
		TaskID:        "123e4567-e89b-42d3-a456-426614174000",
		TargetVersion: "1.2.3",
		State:         tasks.PlatformUpdateReconciliationPending,
	})
	if _, ok := report["code"]; ok {
		t.Fatal("pending reconciliation report must not carry a code")
	}
}
