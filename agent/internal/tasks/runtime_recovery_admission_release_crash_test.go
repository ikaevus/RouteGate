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

func TestAdmissionReleaseCrashEarly(t *testing.T) {
	testAdmissionReleaseCrash(t, []string{"before", "pinned-1", "unlinked-1", "file-1", "pinned-2", "unlinked-2"})
}
func TestAdmissionReleaseCrashMiddle(t *testing.T) {
	testAdmissionReleaseCrash(t, []string{"file-2", "pinned-3", "unlinked-3", "file-3", "pinned-4", "unlinked-4"})
}
func TestAdmissionReleaseCrashFinal(t *testing.T) {
	testAdmissionReleaseCrash(t, []string{"file-4", "before-final", "pinned-5", "unlinked-5", "file-5", "done"})
}
func testAdmissionReleaseCrash(t *testing.T, steps []string) {
	for _, step := range steps {
		for _, kill := range []bool{false, true} {
			t.Run(step+map[bool]string{false: "/exit", true: "/SIGKILL"}[kill], func(t *testing.T) {
				e, a, b, grant, _ := recoveryCommitFixture(t)
				input, _ := json.Marshal(struct {
					Dir, Active string
					Binding     RuntimeRecoveryGrantBinding
					Grant       RuntimeRecoveryMutationGrant
				}{e.receiptDir, e.activePath, b, grant})
				ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
				defer cancel()
				mode := "terminal_confirm_release_intent_submit_ack_retention_submit_ack_archive_done_cleanup_done_commit_intent_done_commit_proof_done_commit_ack_done_cleaned_archive_done_admission_" + step
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
							t.Fatal("child missed admission cut")
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
				path, _ := cleanedCommitArchivePath(e.receiptDir, grant.OperationID)
				backup, copied := terminalHistoryTree(t, previous), terminalHistoryTree(t, path)
				if err := WithArchivedRecoveryCleanedCommit(ctx, e.receiptDir, grant.OperationID, b, func(h RuntimeRecoveryCleanedCommitArchivedHistory) error {
					if !h.Receipt.Recorded || h.Proof.OperationID != grant.OperationID || h.ManifestHash == "" {
						t.Error("lost crash audit")
					}
					return nil
				}); err != nil {
					t.Fatal("archived crash chain", err)
				}
				data, err := readTerminalConfirmationFile(filepath.Join(previous, "history", "retired-"+grant.OperationID, runtimeRecoveryFinalConsumptionFile), runtimeRecoveryConsumptionLimit)
				var consumed runtimeRecoveryFinalConsumption
				if err != nil || !exactHistoricalJSON(data, &consumed) {
					t.Fatal("lost original authorization", err)
				}
				wire, _ := json.Marshal(consumed.Authorization)
				if m, err := BeginRuntimeRecoveryFinalization(ctx, e.receiptDir, b, wire, e.activePath, a); err != ErrRuntimeMutationBlocked {
					if m != nil {
						m.Close()
					}
					t.Fatal("crash reconstructed owner")
				}
				assertTerminalHistoryTree(t, e.receiptDir, before)
				committed := step == "unlinked-5" || step == "file-5" || step == "done"
				if committed != runtimeMarkerAbsent(filepath.Join(e.receiptDir, "mutation-inflight.json")) {
					t.Fatal("wrong crash prefix")
				}
				if runtimeMarkerAbsent(filepath.Join(e.receiptDir, "runtime.lock")) {
					t.Fatal("original lock removed")
				}
				lease, err := BeginRuntimeMutation(e.receiptDir, resultTaskID, TaskKindVPNCoreService)
				if committed {
					// SIGKILL is a process crash, NOT a power-loss fsync test.
					// Last unlink can open admission even before its durability is known.
					if err != nil {
						t.Fatal("final unlink still fenced", err)
					}
					lease.Complete()
					lease.Close()
				} else if err != ErrRuntimeMutationBlocked {
					if lease != nil {
						lease.Close()
					}
					t.Fatal("partial prefix admitted mutation")
				}
				assertTerminalHistoryTree(t, previous, backup)
				assertTerminalHistoryTree(t, path, copied)
			})
		}
	}
}
