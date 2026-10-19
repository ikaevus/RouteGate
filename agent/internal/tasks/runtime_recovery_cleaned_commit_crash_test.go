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

func TestCleanedCommitCrash(t *testing.T) {
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
				cmd := exec.CommandContext(ctx, os.Args[0], "-test.run=^TestRuntimeMutationRecoveryCommitProcessExit$")
				cmd.Env = append(os.Environ(), "RG_TEST_RECOVERY_COMMIT_CHILD="+string(input), "RG_TEST_RECOVERY_FINAL_CHILD=1", "RG_TEST_RECOVERY_FINAL_SUBMIT_CHILD=terminal_confirm_release_intent_submit_ack_retention_submit_ack_archive_done_cleanup_done_commit_intent_"+step)
				if kill {
					cmd.Env = append(cmd.Env, "RG_TEST_RECOVERY_COMMIT_KILL=1")
					if err := cmd.Start(); err != nil {
						t.Fatal(err)
					}
					defer func() { cmd.Process.Kill(); cmd.Wait() }()
					for runtimeMarkerAbsent(filepath.Join(filepath.Dir(e.receiptDir), "test-release-intent-ready")) {
						if ctx.Err() != nil {
							t.Fatal("child missed intent cut")
						}
						time.Sleep(10 * time.Millisecond)
					}
					cmd.Process.Kill()
					cmd.Wait()
				} else if out, err := cmd.CombinedOutput(); err != nil {
					t.Fatal("intent child", err, string(out))
				}
				before := terminalHistoryTree(t, e.receiptDir)
				copyPath, _ := recoveryHistoryArchivePath(e.receiptDir, grant.OperationID)
				backup := terminalHistoryTree(t, copyPath)
				called := false
				err := WithRecoveryCleanedCommitHistory(ctx, e.receiptDir, grant.OperationID, b, func(p RuntimeRecoveryCleanedCommitIntent, _ RuntimeRecoveryRetentionHistory) error {
					called = true
					if p.Binding != b || p.OperationID != grant.OperationID || p.ManifestHash == "" {
						t.Error("crash lost bindings")
					}
					return nil
				})
				// A surviving temp/link is ambiguous and the exact tree rejects it.
				// Once only the intent name remains, history reports its bytes but
				// cannot prove whether the interrupted parent fsync completed.
				readable := step != "before" && step != "file" && step != "linked"
				if readable && (err != nil || !called) || !readable && (err != ErrRuntimeMutationBlocked || called) {
					t.Fatal("unsafe crash readback", err)
				}
				for _, name := range []string{"runtime.lock", "mutation-inflight.json", runtimeRecoveryReleaseIntentFile} {
					if runtimeMarkerAbsent(filepath.Join(e.receiptDir, name)) {
						t.Fatal("lost admission barrier")
					}
				}
				if lease, err := BeginRuntimeMutation(e.receiptDir, resultTaskID, TaskKindVPNCoreService); err != ErrRuntimeMutationBlocked {
					if lease != nil {
						lease.Close()
					}
					t.Fatal("crash released fence")
				}
				if owner, err := BeginRuntimeRecoveryFinalization(ctx, e.receiptDir, b, nil, e.activePath, a); err != ErrRuntimeMutationBlocked {
					if owner != nil {
						owner.Close()
					}
					t.Fatal("crash reconstructed owner")
				}
				assertTerminalHistoryTree(t, e.receiptDir, before)
				assertTerminalHistoryTree(t, copyPath, backup)
			})
		}
	}
}
