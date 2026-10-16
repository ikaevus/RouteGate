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

func TestRuntimeMutationTerminalConfirmationCrash(t *testing.T) {
	for _, mode := range []string{"terminal_confirm_pin", "terminal_confirm_ack"} {
		for _, kill := range []bool{true, false} {
			t.Run(mode+map[bool]string{true: "/SIGKILL", false: "/exit"}[kill], func(t *testing.T) {
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
					for runtimeMarkerAbsent(filepath.Join(e.receiptDir, "test-commit-ready")) {
						if ctx.Err() != nil {
							t.Fatal("child missed terminal cut")
						}
						time.Sleep(10 * time.Millisecond)
					}
					cmd.Process.Kill()
					cmd.Wait()
				} else if out, err := cmd.CombinedOutput(); err != nil {
					t.Fatal("child terminal confirmation", err, string(out))
				}
				pin, err := readTerminalConfirmationFile(filepath.Join(e.receiptDir, runtimeRecoveryTerminalSubmissionFile), 8192)
				var p RuntimeRecoveryTerminalProof
				if err != nil || !exactHistoricalJSON(pin, &p) {
					t.Fatal("lost pin", err)
				}
				ackPath := filepath.Join(e.receiptDir, runtimeRecoveryTerminalAcknowledgementFile)
				if mode == "terminal_confirm_ack" {
					ack, err := readTerminalConfirmationFile(ackPath, 4096)
					var r RuntimeRecoveryTerminalReceipt
					if err != nil || !exactHistoricalJSON(ack, &r) || !terminalReceiptMatches(r, p, pin) {
						t.Fatal("lost receipt", err)
					}
				} else if !runtimeMarkerAbsent(ackPath) {
					t.Fatal("unconfirmed pin manufactured receipt")
				}
				assertTerminalArchive(t, e.receiptDir, b)
				before := terminalHistoryTree(t, e.receiptDir)
				if err := WithHistoricalRecoveryTerminal(ctx, e.receiptDir, b, func(got RuntimeRecoveryTerminalProof) error {
					if got != p {
						t.Error("post-crash terminal bytes changed")
					}
					return nil
				}); err != nil {
					t.Fatal("post-crash terminal read", err)
				}
				assertTerminalHistoryTree(t, e.receiptDir, before)
				if _, err := BeginRuntimeRecoveryCommit(ctx, e.receiptDir, b, grantWire, e.activePath, a); err != ErrRuntimeMutationBlocked {
					t.Fatal("crashed terminal adopted fresh commit", err)
				}
				finalWire, _ := os.ReadFile(filepath.Join(e.receiptDir, "retired-"+p.OperationID, runtimeRecoveryFinalConsumptionFile))
				var consumed runtimeRecoveryFinalConsumption
				if !exactHistoricalJSON(finalWire, &consumed) {
					t.Fatal("lost archived authority")
				}
				authWire, _ := json.Marshal(consumed.Authorization)
				if _, err := BeginRuntimeRecoveryFinalization(ctx, e.receiptDir, b, authWire, e.activePath, a); err != ErrRuntimeMutationBlocked {
					t.Fatal("crashed terminal resumed", err)
				}
			})
		}
	}
}
