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

func TestCleanedCommitProofCrash(t *testing.T) { testCleanedCommitSubmissionCrash(t, false) }
func TestCleanedCommitAckCrash(t *testing.T)   { testCleanedCommitSubmissionCrash(t, true) }

func testCleanedCommitSubmissionCrash(t *testing.T, ack bool) {
	for _, step := range []string{"before", "file", "linked", "unlinked-temp", "durable", "published", "done"} {
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
				mode := "terminal_confirm_release_intent_submit_ack_retention_submit_ack_archive_done_cleanup_done_commit_intent_done_commit_proof_" + step
				if ack {
					mode = "terminal_confirm_release_intent_submit_ack_retention_submit_ack_archive_done_cleanup_done_commit_intent_done_commit_proof_done_commit_ack_" + step
				}
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
							t.Fatal("child missed publication cut")
						}
						time.Sleep(10 * time.Millisecond)
					}
					cmd.Process.Kill()
					cmd.Wait()
				} else if out, err := cmd.CombinedOutput(); err != nil {
					t.Fatal(err, string(out))
				}
				before := terminalHistoryTree(t, e.receiptDir)
				copyPath, _ := recoveryHistoryArchivePath(e.receiptDir, grant.OperationID)
				backup := terminalHistoryTree(t, copyPath)
				called := false
				err := WithRecoveryCleanedCommitEvidenceHistory(ctx, e.receiptDir, grant.OperationID, b, func(p RuntimeRecoveryCleanedCommitProof, r RuntimeRecoveryCleanedCommitReceipt) error {
					called = true
					if p.OperationID != grant.OperationID || p.AgentCredentialGeneration != b.AgentCredentialGeneration {
						t.Error("lost proof bindings")
					}
					wantAck := ack && step != "before" && step != "file" && step != "linked"
					if r.Recorded != wantAck {
						t.Error("fabricated/lost acknowledgement")
					}
					return nil
				})
				readable := step != "file" && step != "linked" && (ack || step != "before")
				if readable && (err != nil || !called) || !readable && (err != ErrRuntimeMutationBlocked || called) {
					t.Fatal("unsafe publication history", err)
				}
				for _, name := range []string{"runtime.lock", "mutation-inflight.json", runtimeRecoveryReleaseIntentFile} {
					if runtimeMarkerAbsent(filepath.Join(e.receiptDir, name)) {
						t.Fatal("lost fence")
					}
				}
				if lease, err := BeginRuntimeMutation(e.receiptDir, resultTaskID, TaskKindVPNCoreService); err != ErrRuntimeMutationBlocked {
					if lease != nil {
						lease.Close()
					}
					t.Fatal("crash released fence")
				}
				data, err := readTerminalConfirmationFile(filepath.Join(copyPath, "history", "retired-"+grant.OperationID, runtimeRecoveryFinalConsumptionFile), runtimeRecoveryConsumptionLimit)
				var consumed runtimeRecoveryFinalConsumption
				if err != nil || !exactHistoricalJSON(data, &consumed) {
					t.Fatal("lost original authority", err)
				}
				wire, _ := json.Marshal(consumed.Authorization)
				if m, err := BeginRuntimeRecoveryFinalization(ctx, e.receiptDir, b, wire, e.activePath, a); err != ErrRuntimeMutationBlocked {
					if m != nil {
						m.Close()
					}
					t.Fatal("historical owner adopted")
				}
				assertTerminalHistoryTree(t, e.receiptDir, before)
				assertTerminalHistoryTree(t, copyPath, backup)
			})
		}
	}
}
