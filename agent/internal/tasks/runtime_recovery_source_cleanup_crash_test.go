package tasks

import (
	"context"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"
)

func TestSourceCleanupCrashRoot(t *testing.T) {
	steps := []string{"before", "unlinked-1", "unlinked-9"}
	for i := 1; i <= 9; i++ {
		steps = append(steps, "file-"+strconv.Itoa(i))
	}
	testSourceCleanupCrash(t, steps)
}
func TestSourceCleanupCrashRetired(t *testing.T) {
	var steps []string
	for i := 10; i <= 18; i++ {
		steps = append(steps, "file-"+strconv.Itoa(i))
	}
	testSourceCleanupCrash(t, append(steps, "unlinked-10", "unlinked-18", "directory-unlinked", "directory", "done"))
}

func testSourceCleanupCrash(t *testing.T, steps []string) {
	for _, step := range steps {
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
				cmd.Env = append(os.Environ(), "RG_TEST_RECOVERY_COMMIT_CHILD="+string(input), "RG_TEST_RECOVERY_FINAL_CHILD=1", "RG_TEST_RECOVERY_FINAL_SUBMIT_CHILD=terminal_confirm_release_intent_submit_ack_retention_submit_ack_archive_done_cleanup_"+step)
				if kill {
					cmd.Env = append(cmd.Env, "RG_TEST_RECOVERY_COMMIT_KILL=1")
					if err := cmd.Start(); err != nil {
						t.Fatal(err)
					}
					defer func() { cmd.Process.Kill(); cmd.Wait() }()
					for runtimeMarkerAbsent(filepath.Join(filepath.Dir(e.receiptDir), "test-release-intent-ready")) {
						if ctx.Err() != nil {
							t.Fatal("child missed cleanup cut")
						}
						time.Sleep(10 * time.Millisecond)
					}
					cmd.Process.Kill()
					cmd.Wait()
				} else if out, err := cmd.CombinedOutput(); err != nil {
					t.Fatal("cleanup child", err, string(out))
				}
				before := terminalHistoryTree(t, e.receiptDir)
				copyPath, _ := recoveryHistoryArchivePath(e.receiptDir, g.OperationID)
				backup := terminalHistoryTree(t, copyPath)
				if err := WithRecoverySourceCleanupHistory(ctx, e.receiptDir, g.OperationID, b, func(s RuntimeRecoverySourceCleanupStatus, _ RuntimeRecoveryRetentionHistory) error {
					if step == "before" && s.RemovedFiles != 0 {
						t.Error("unexpected removals")
					}
					if strings.HasPrefix(step, "file-") || strings.HasPrefix(step, "unlinked-") {
						n, _ := strconv.Atoi(strings.TrimPrefix(strings.TrimPrefix(step, "file-"), "unlinked-"))
						if s.RemovedFiles != min(n, s.TotalFiles) {
							t.Error("wrong durable prefix")
						}
					}
					if (step == "directory" || step == "directory-unlinked" || step == "done") && !s.SourcesRemoved {
						t.Error("lost completed cleanup")
					}
					return nil
				}); err != nil {
					t.Fatal("crash history", err)
				}
				for _, name := range []string{"runtime.lock", "mutation-inflight.json", runtimeRecoveryReleaseIntentFile} {
					if runtimeMarkerAbsent(filepath.Join(e.receiptDir, name)) {
						t.Fatal("lost original admission barrier")
					}
				}
				if l, err := BeginRuntimeMutation(e.receiptDir, resultTaskID, TaskKindVPNCoreService); err != ErrRuntimeMutationBlocked {
					if l != nil {
						l.Close()
					}
					t.Fatal("crash released fence")
				}
				assertTerminalHistoryTree(t, e.receiptDir, before)
				assertTerminalHistoryTree(t, copyPath, backup)
			})
		}
	}
}
