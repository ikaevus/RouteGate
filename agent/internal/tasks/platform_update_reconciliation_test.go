package tasks

import "testing"

func TestReadPlatformUpdateReconciliationLifecycle(t *testing.T) {
	store := testPlatformUpdateReceiptStore(t)
	taskID := "550e8400-e29b-41d4-a716-446655440000"
	if _, err := store.CreatePrepared(taskID, "v1.2.3"); err != nil {
		t.Fatal(err)
	}

	pending, err := readPlatformUpdateReconciliation(store, taskID, "v1.2.3")
	if err != nil {
		t.Fatal(err)
	}
	if pending.State != PlatformUpdateReconciliationPending || pending.Code != "" {
		t.Fatalf("unexpected pending reconciliation: %+v", pending)
	}

	if _, err := store.MarkMutationStarted(taskID); err != nil {
		t.Fatal(err)
	}
	pending, err = readPlatformUpdateReconciliation(store, taskID, "v1.2.3")
	if err != nil {
		t.Fatal(err)
	}
	if pending.State != PlatformUpdateReconciliationPending {
		t.Fatalf("mutation_started became terminal: %+v", pending)
	}

	if _, err := store.MarkSucceeded(taskID); err != nil {
		t.Fatal(err)
	}
	succeeded, err := readPlatformUpdateReconciliation(store, taskID, "v1.2.3")
	if err != nil {
		t.Fatal(err)
	}
	if succeeded.State != PlatformUpdateReconciliationSucceeded || succeeded.TaskID != taskID || succeeded.TargetVersion != "v1.2.3" {
		t.Fatalf("unexpected success reconciliation: %+v", succeeded)
	}
}

func TestReadPlatformUpdateReconciliationOutcomeUnknown(t *testing.T) {
	store := testPlatformUpdateReceiptStore(t)
	taskID := "550e8400-e29b-41d4-a716-446655440000"
	if _, err := store.CreatePrepared(taskID, "v1.2.3"); err != nil {
		t.Fatal(err)
	}
	if _, err := store.MarkMutationStarted(taskID); err != nil {
		t.Fatal(err)
	}
	if _, err := store.MarkOutcomeUnknown(taskID, "verified_updater_signaled"); err != nil {
		t.Fatal(err)
	}
	result, err := readPlatformUpdateReconciliation(store, taskID, "v1.2.3")
	if err != nil {
		t.Fatal(err)
	}
	if result.State != PlatformUpdateReconciliationOutcomeUnknown || result.Code != "verified_updater_signaled" {
		t.Fatalf("unexpected unknown reconciliation: %+v", result)
	}
}

func TestReadPlatformUpdateReconciliationRejectsVersionMismatch(t *testing.T) {
	store := testPlatformUpdateReceiptStore(t)
	taskID := "550e8400-e29b-41d4-a716-446655440000"
	if _, err := store.CreatePrepared(taskID, "v1.2.3"); err != nil {
		t.Fatal(err)
	}
	if _, err := readPlatformUpdateReconciliation(store, taskID, "v1.2.4"); err == nil {
		t.Fatal("receipt was accepted for a different target version")
	}
}

func TestReadPlatformUpdateReconciliationRejectsNonCanonicalInputs(t *testing.T) {
	store := testPlatformUpdateReceiptStore(t)
	for _, tc := range []struct {
		taskID string
		version string
	}{
		{"../escape", "v1.2.3"},
		{"550e8400-e29b-41d4-a716-446655440000", " v1.2.3"},
	} {
		if _, err := readPlatformUpdateReconciliation(store, tc.taskID, tc.version); err == nil {
			t.Fatalf("non-canonical input accepted: %+v", tc)
		}
	}
}
