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

func TestRecoveryHistoryArchiveCrash(t *testing.T) {
	for _, step := range []string{"directory", "copy-root-" + runtimeRecoveryRetirementFile, "copied", "published", "done"} {
		for _, kill := range []bool{false, true} {
			t.Run(step+map[bool]string{false: "/exit", true: "/SIGKILL"}[kill], func(t *testing.T) {
				e, _, b, g, _ := recoveryCommitFixture(t)
				input, _ := json.Marshal(struct {
					Dir, Active string
					Binding     RuntimeRecoveryGrantBinding
					Grant       RuntimeRecoveryMutationGrant
				}{e.receiptDir, e.activePath, b, g})
				ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
				defer cancel()
				cmd := exec.CommandContext(ctx, os.Args[0], "-test.run=^TestRuntimeMutationRecoveryCommitProcessExit$")
				cmd.Env = append(os.Environ(), "RG_TEST_RECOVERY_COMMIT_CHILD="+string(input), "RG_TEST_RECOVERY_FINAL_CHILD=1", "RG_TEST_RECOVERY_FINAL_SUBMIT_CHILD=terminal_confirm_release_intent_submit_ack_retention_submit_ack_archive_"+step)
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
					t.Fatal("archive child", err, string(out))
				}
				before := terminalHistoryTree(t, e.receiptDir)
				if err := WithHistoricalRecoveryRetention(ctx, e.receiptDir, b, func(RuntimeRecoveryRetentionHistory) error { return nil }); err != nil {
					t.Fatal("crash lost original chain", err)
				}
				err := WithArchivedRecoveryRetention(ctx, e.receiptDir, g.OperationID, b, func(h RuntimeRecoveryArchivedHistory) error {
					if len(h.ManifestHash) != 64 || !h.History.LocalReceipt.Recorded {
						t.Error("crash lost receipt")
					}
					return nil
				})
				complete := step == "published" || step == "done"
				if complete && err != nil || !complete && err != ErrRuntimeMutationBlocked {
					t.Fatal("incorrect publication boundary", err)
				}
				if l, err := BeginRuntimeMutation(e.receiptDir, resultTaskID, TaskKindVPNCoreService); err != ErrRuntimeMutationBlocked {
					if l != nil {
						l.Close()
					}
					t.Fatal("crash released fence")
				}
				assertTerminalHistoryTree(t, e.receiptDir, before)
			})
		}
	}
}
