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

func TestAdmissionProofCrashBefore(t *testing.T) {
	testAdmissionProofCrash(t, []string{"before", "file", "linked"})
}
func TestAdmissionProofCrashAfter(t *testing.T) {
	testAdmissionProofCrash(t, []string{"unlinked-temp", "durable", "published", "done"})
}
func testAdmissionProofCrash(t *testing.T, steps []string) {
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
				mode := "terminal_confirm_release_intent_submit_ack_retention_submit_ack_archive_done_cleanup_done_commit_intent_done_commit_proof_done_commit_ack_done_cleaned_archive_done_admission_done_admission_proof_" + step
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
							t.Fatal("child missed proof cut")
						}
						time.Sleep(10 * time.Millisecond)
					}
					cmd.Process.Kill()
					cmd.Wait()
				} else if out, err := cmd.CombinedOutput(); err != nil {
					t.Fatal(err, string(out))
				}
				if !runtimeMarkerAbsent(filepath.Join(e.receiptDir, "mutation-inflight.json")) {
					t.Fatal("proof cut preceded release")
				}
				before := terminalHistoryTree(t, e.receiptDir)
				previous, _ := recoveryHistoryArchivePath(e.receiptDir, grant.OperationID)
				copy, _ := cleanedCommitArchivePath(e.receiptDir, grant.OperationID)
				old, copied := terminalHistoryTree(t, previous), terminalHistoryTree(t, copy)
				called := false
				err := WithRecoveryAdmissionReleaseHistory(ctx, e.receiptDir, grant.OperationID, b, func(p RuntimeRecoveryAdmissionReleaseProof) error {
					called = true
					if p.OperationID != grant.OperationID || p.CleanedArchiveHash == "" {
						t.Error("crash lost binding")
					}
					return nil
				})
				readable := step == "unlinked-temp" || step == "durable" || step == "published" || step == "done"
				if readable && (err != nil || !called) || !readable && (err != ErrRuntimeMutationBlocked || called) {
					t.Fatal("partial proof adopted", err)
				}
				data, err := readTerminalConfirmationFile(filepath.Join(previous, "history", "retired-"+grant.OperationID, runtimeRecoveryFinalConsumptionFile), runtimeRecoveryConsumptionLimit)
				var consumed runtimeRecoveryFinalConsumption
				if err != nil || !exactHistoricalJSON(data, &consumed) {
					t.Fatal("lost authority history", err)
				}
				wire, _ := json.Marshal(consumed.Authorization)
				if owner, err := BeginRuntimeRecoveryFinalization(ctx, e.receiptDir, b, wire, e.activePath, a); err != ErrRuntimeMutationBlocked {
					if owner != nil {
						owner.Close()
					}
					t.Fatal("proof reconstructed owner")
				}
				assertTerminalHistoryTree(t, e.receiptDir, before)
				lease, err := BeginRuntimeMutation(e.receiptDir, resultTaskID, TaskKindVPNCoreService)
				if err != nil {
					t.Fatal("proof crash changed released admission", err)
				}
				if err := lease.Complete(); err != nil {
					t.Fatal(err)
				}
				lease.Close()
				assertTerminalHistoryTree(t, previous, old)
				assertTerminalHistoryTree(t, copy, copied)
			})
		}
	}
}
