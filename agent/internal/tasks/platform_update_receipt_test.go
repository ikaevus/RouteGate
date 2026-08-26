package tasks

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

func testPlatformUpdateReceiptStore(t *testing.T) platformUpdateReceiptStore {
	t.Helper()
	root := filepath.Join(t.TempDir(), "receipts")
	return platformUpdateReceiptStore{
		root: root,
		ownerUID: uint32(os.Geteuid()),
		now: func() time.Time { return time.Date(2026, 8, 26, 2, 0, 0, 0, time.UTC) },
	}
}

func TestPlatformUpdateReceiptLifecycle(t *testing.T) {
	store := testPlatformUpdateReceiptStore(t)
	taskID := "550e8400-e29b-41d4-a716-446655440000"

	prepared, err := store.CreatePrepared(taskID, "v1.2.3")
	if err != nil {
		t.Fatalf("CreatePrepared: %v", err)
	}
	if prepared.Phase != PlatformUpdateReceiptPrepared || prepared.MutationStarted {
		t.Fatalf("unexpected prepared receipt: %+v", prepared)
	}

	started, err := store.MarkMutationStarted(taskID)
	if err != nil {
		t.Fatalf("MarkMutationStarted: %v", err)
	}
	if started.Phase != PlatformUpdateReceiptMutationStarted || !started.MutationStarted {
		t.Fatalf("unexpected mutation-started receipt: %+v", started)
	}

	succeeded, err := store.MarkSucceeded(taskID)
	if err != nil {
		t.Fatalf("MarkSucceeded: %v", err)
	}
	if succeeded.Phase != PlatformUpdateReceiptSucceeded || !succeeded.MutationStarted {
		t.Fatalf("unexpected success receipt: %+v", succeeded)
	}
	if _, err := store.MarkFailed(taskID, "late_failure"); err == nil {
		t.Fatal("terminal receipt accepted a later transition")
	}

	info, err := os.Lstat(store.path(taskID))
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode().Perm() != 0600 {
		t.Fatalf("receipt mode = %o, want 0600", info.Mode().Perm())
	}
}

func TestPlatformUpdateReceiptRejectsDuplicateTask(t *testing.T) {
	store := testPlatformUpdateReceiptStore(t)
	taskID := "550e8400-e29b-41d4-a716-446655440000"
	if _, err := store.CreatePrepared(taskID, "v1.2.3"); err != nil {
		t.Fatal(err)
	}
	if _, err := store.CreatePrepared(taskID, "v1.2.4"); err == nil {
		t.Fatal("duplicate task receipt was accepted")
	}
}

func TestPlatformUpdateReceiptReconcilesStartedToUnknown(t *testing.T) {
	store := testPlatformUpdateReceiptStore(t)
	taskID := "550e8400-e29b-41d4-a716-446655440000"
	if _, err := store.CreatePrepared(taskID, "v1.2.3"); err != nil {
		t.Fatal(err)
	}
	if _, err := store.MarkMutationStarted(taskID); err != nil {
		t.Fatal(err)
	}
	receipt, err := store.ReconcileInterrupted(taskID)
	if err != nil {
		t.Fatal(err)
	}
	if receipt.Phase != PlatformUpdateReceiptOutcomeUnknown || receipt.Code != "agent_restart_after_mutation_started" {
		t.Fatalf("unexpected reconciled receipt: %+v", receipt)
	}
	if _, err := store.MarkMutationStarted(taskID); err == nil {
		t.Fatal("outcome_unknown receipt became runnable again")
	}
}

func TestPlatformUpdateReceiptRejectsUnsafeRoot(t *testing.T) {
	base := t.TempDir()
	target := filepath.Join(base, "target")
	if err := os.Mkdir(target, 0700); err != nil {
		t.Fatal(err)
	}
	link := filepath.Join(base, "receipts")
	if err := os.Symlink(target, link); err != nil {
		t.Fatal(err)
	}
	store := platformUpdateReceiptStore{root: link, ownerUID: uint32(os.Geteuid()), now: time.Now}
	if _, err := store.CreatePrepared("550e8400-e29b-41d4-a716-446655440000", "v1.2.3"); err == nil {
		t.Fatal("symlink receipt root was accepted")
	}
}

func TestPlatformUpdateReceiptCodeIsBounded(t *testing.T) {
	for _, code := range []string{"", "contains space", "../escape", string(make([]byte, 65))} {
		if validPlatformUpdateReceiptCode(code) {
			t.Fatalf("invalid receipt code %q was accepted", code)
		}
	}
	for _, code := range []string{"rollback_failed", "update-error-1"} {
		if !validPlatformUpdateReceiptCode(code) {
			t.Fatalf("valid receipt code %q was rejected", code)
		}
	}
}
