package tasks

import (
	"context"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestPreparedReleaseCrash(t *testing.T) {
	for _, mode := range []string{"terminal_confirm_release_intent_submit_pin_cut", "terminal_confirm_release_intent_submit_pin", "terminal_confirm_release_intent_submit_ack_cut", "terminal_confirm_release_intent_submit_ack", "terminal_confirm_release_intent_submit_ack_retention_cut", "terminal_confirm_release_intent_submit_ack_retention", "terminal_confirm_release_intent_submit_ack_retention_submit_pin_cut", "terminal_confirm_release_intent_submit_ack_retention_submit_pin", "terminal_confirm_release_intent_submit_ack_retention_submit_ack_cut", "terminal_confirm_release_intent_submit_ack_retention_submit_ack"} {
		for _, kill := range []bool{false, true} {
			t.Run(mode+map[bool]string{false: "/exit", true: "/SIGKILL"}[kill], func(t *testing.T) {
				e, a, b, g, grantWire := recoveryCommitFixture(t)
				input, _ := json.Marshal(struct {
					Dir, Active string
					Binding     RuntimeRecoveryGrantBinding
					Grant       RuntimeRecoveryMutationGrant
				}{e.receiptDir, e.activePath, b, g})
				ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
				defer cancel()
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
							t.Fatal("child missed release intent cut")
						}
						time.Sleep(10 * time.Millisecond)
					}
					cmd.Process.Kill()
					cmd.Wait()
				} else if out, err := cmd.CombinedOutput(); err != nil {
					t.Fatal("release intent child", err, string(out))
				}
				pin, e2 := readTerminalConfirmationFile(filepath.Join(e.receiptDir, runtimeRecoveryPreparedSubmissionFile), 8192)
				var proof RuntimeRecoveryPreparedReleaseProof
				if e2 != nil || !exactHistoricalJSON(pin, &proof) || proof.OperationID != g.OperationID || runtimeMarkerAbsent(filepath.Join(e.receiptDir, "mutation-inflight.json")) || runtimeMarkerAbsent(filepath.Join(e.receiptDir, runtimeRecoveryReleaseIntentFile)) {
					t.Fatal("lost prepared fence/proof", e2)
				}
				if strings.Contains(mode, "ack") {
					ack, e2 := readTerminalConfirmationFile(filepath.Join(e.receiptDir, runtimeRecoveryPreparedAcknowledgementFile), 4096)
					var receipt RuntimeRecoveryPreparedReleaseReceipt
					if e2 != nil || !exactHistoricalJSON(ack, &receipt) || !preparedReleaseReceiptMatches(receipt, proof, pin) {
						t.Fatal("lost exact receipt", e2)
					}
				} else if !runtimeMarkerAbsent(filepath.Join(e.receiptDir, runtimeRecoveryPreparedAcknowledgementFile)) {
					t.Fatal("manufactured receipt")
				}
				if err := WithHistoricalRecoveryPreparedRelease(ctx, e.receiptDir, b, func(RuntimeRecoveryPreparedReleaseHistory) error {
					t.Error("old reader adopted submission")
					return nil
				}); err != ErrRuntimeMutationBlocked {
					t.Fatal(err)
				}
				if l, err := BeginRuntimeMutation(e.receiptDir, resultTaskID, TaskKindVPNCoreService); err != ErrRuntimeMutationBlocked {
					if l != nil {
						l.Close()
					}
					t.Fatal("crash released fence")
				}

				before := terminalHistoryTree(t, e.receiptDir)
				if strings.Contains(mode, "retention_submit") {
					pin, err := readTerminalConfirmationFile(filepath.Join(e.receiptDir, runtimeRecoveryRetentionSubmissionFile), 8192)
					var p RuntimeRecoveryRetentionProof
					if err != nil || !exactHistoricalJSON(pin, &p) {
						t.Fatal("lost retention submission", err)
					}
					if err := WithHistoricalRecoveryRetention(ctx, e.receiptDir, b, func(h RuntimeRecoveryRetentionHistory) error {
						if h.Proof != p || (h.LocalReceipt.SchemaVersion != 0) != strings.Contains(mode, "retention_submit_ack") {
							t.Error("crash changed retention phase")
						}
						return nil
					}); err != nil {
						t.Fatal("retention crash history unavailable", err)
					}
				}
				err := WithHistoricalRecoveryPreparedSubmission(ctx, e.receiptDir, b, func(h RuntimeRecoveryPreparedSubmissionHistory) error {
					if h.Proof != proof || (h.LocalReceipt.SchemaVersion != 0) != strings.Contains(mode, "ack") {
						t.Error("crash history changed proof or acknowledgement phase")
					}
					if (h.RetentionHash != "") != strings.Contains(mode, "retention") {
						t.Error("crash changed retention phase")
					}
					return nil
				})
				if strings.Contains(mode, "retention_submit") {
					if err != ErrRuntimeMutationBlocked {
						t.Fatal("old reader adopted retention submission", err)
					}
				} else if err != nil {
					t.Fatal("crash history unreadable", err)
				}
				assertTerminalHistoryTree(t, e.receiptDir, before)
				if m, err := BeginRuntimeRecoveryCommit(ctx, e.receiptDir, b, grantWire, e.activePath, a); err != ErrRuntimeMutationBlocked {
					if m != nil {
						m.Close()
					}
					t.Fatal("crashed release intent resumed commit", err)
				}
				data, err := os.ReadFile(filepath.Join(e.receiptDir, "retired-"+g.OperationID, runtimeRecoveryFinalConsumptionFile))
				var consumed runtimeRecoveryFinalConsumption
				if err != nil || !exactHistoricalJSON(data, &consumed) {
					t.Fatal("lost archived authority", err)
				}
				wire, _ := json.Marshal(consumed.Authorization)
				if m, err := BeginRuntimeRecoveryFinalization(ctx, e.receiptDir, b, wire, e.activePath, a); err != ErrRuntimeMutationBlocked {
					if m != nil {
						m.Close()
					}
					t.Fatal("crashed release intent resumed final owner", err)
				}
				assertTerminalHistoryTree(t, e.receiptDir, before)
			})
		}
	}
}
