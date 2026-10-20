package tasks

import (
	"context"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"
)

func TestCleanedArchiveCrashBeforeManifest(t *testing.T) {
	testCleanedArchiveCrash(t, []string{"before", "directory", "state", "lock", "copy-0", "copy-1"})
}
func TestCleanedArchiveCrashAfterCopies(t *testing.T) {
	testCleanedArchiveCrash(t, []string{"copy-2", "copy-3", "copy-4", "copied", "published", "done"})
}
func testCleanedArchiveCrash(t *testing.T, steps []string) {
	for _, step := range steps {
		for _, kill := range []bool{false, true} {
			t.Run(step+map[bool]string{false: "/exit", true: "/SIGKILL"}[kill], func(t *testing.T) {
				e, a, b, grant, _ := recoveryCommitFixture(t)
				input, _ := json.Marshal(struct {
					Dir, Active string
					Binding     RuntimeRecoveryGrantBinding
					Grant       RuntimeRecoveryMutationGrant
				}{e.receiptDir, e.activePath, b, grant})
				ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
				defer cancel()
				mode := "terminal_confirm_release_intent_submit_ack_retention_submit_ack_archive_done_cleanup_done_commit_intent_done_commit_proof_done_commit_ack_done_cleaned_archive_" + step
				cmd := exec.CommandContext(ctx, os.Args[0], "-test.run=^TestRuntimeMutationRecoveryCommitProcessExit$")
				cmd.Env = append(os.Environ(), "RG_TEST_RECOVERY_COMMIT_CHILD="+string(input), "RG_TEST_RECOVERY_FINAL_CHILD=1", "RG_TEST_RECOVERY_FINAL_SUBMIT_CHILD="+mode)
				if kill {
					cmd.Env = append(cmd.Env, "RG_TEST_RECOVERY_COMMIT_KILL=1")
					if err := cmd.Start(); err != nil {
						t.Fatal(err)
					}
					defer func() { cmd.Process.Kill(); cmd.Wait() }()
					for runtimeMarkerAbsent(filepath.Join(filepath.Dir(e.receiptDir), "test-release-intent-ready")) {
						if ctx.Err() != nil {
							t.Fatal("child missed archive cut")
						}
						time.Sleep(10 * time.Millisecond)
					}
					cmd.Process.Kill()
					cmd.Wait()
				} else if out, err := cmd.CombinedOutput(); err != nil {
					t.Fatal(err, string(out))
				}
				before := terminalHistoryTree(t, e.receiptDir)
				previous, _ := recoveryHistoryArchivePath(e.receiptDir, grant.OperationID)
				backup := terminalHistoryTree(t, previous)
				path, _ := cleanedCommitArchivePath(e.receiptDir, grant.OperationID)
				var newCopy map[string][]byte
				if !runtimeMarkerAbsent(path) {
					newCopy = terminalHistoryTree(t, path)
				}
				called := false
				err := WithArchivedRecoveryCleanedCommit(ctx, e.receiptDir, grant.OperationID, b, func(h RuntimeRecoveryCleanedCommitArchivedHistory) error {
					called = true
					if h.Proof.OperationID != grant.OperationID || !h.Receipt.Recorded || h.ManifestHash == "" {
						t.Error("crash lost chain")
					}
					return nil
				})
				readable := step == "published" || step == "done"
				if readable && (err != nil || !called) || !readable && (err != ErrRuntimeMutationBlocked || called) {
					t.Fatal("partial archive adopted", err)
				}
				for _, name := range []string{"runtime.lock", "mutation-inflight.json", runtimeRecoveryReleaseIntentFile, runtimeRecoveryCleanedCommitIntentFile, runtimeRecoveryCleanedCommitSubmissionFile, runtimeRecoveryCleanedCommitAcknowledgementFile} {
					if runtimeMarkerAbsent(filepath.Join(e.receiptDir, name)) {
						t.Fatal("crash lost source", name)
					}
				}
				if lease, err := BeginRuntimeMutation(e.receiptDir, resultTaskID, TaskKindVPNCoreService); err != ErrRuntimeMutationBlocked {
					if lease != nil {
						lease.Close()
					}
					t.Fatal("crash released fence")
				}
				data, err := readTerminalConfirmationFile(filepath.Join(previous, "history", "retired-"+grant.OperationID, runtimeRecoveryFinalConsumptionFile), runtimeRecoveryConsumptionLimit)
				var consumed runtimeRecoveryFinalConsumption
				if err != nil || !exactHistoricalJSON(data, &consumed) {
					t.Fatal("lost original authority", err)
				}
				wire, _ := json.Marshal(consumed.Authorization)
				if m, err := BeginRuntimeRecoveryFinalization(ctx, e.receiptDir, b, wire, e.activePath, a); err != ErrRuntimeMutationBlocked {
					if m != nil {
						m.Close()
					}
					t.Fatal("archive reconstructed owner")
				}
				assertTerminalHistoryTree(t, e.receiptDir, before)
				assertTerminalHistoryTree(t, previous, backup)
				if newCopy != nil {
					assertTerminalHistoryTree(t, path, newCopy)
				}
			})
		}
	}
}
