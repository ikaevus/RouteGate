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

func TestRuntimeMutationFinalConfirmationCrash(t *testing.T) {
	for _, mode := range []string{"pin", "ack"} {
		t.Run(mode, func(t *testing.T) {
			for _, kill := range []bool{false, true} {
				t.Run(map[bool]string{false: "exit", true: "SIGKILL"}[kill], func(t *testing.T) {
					e, _, b, g, _ := recoveryCommitFixture(t)
					input, _ := json.Marshal(struct {
						Dir, Active string
						Binding     RuntimeRecoveryGrantBinding
						Grant       RuntimeRecoveryMutationGrant
					}{e.receiptDir, e.activePath, b, g})
					ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
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
								t.Fatal("child failed to consume final authority")
							}
							time.Sleep(10 * time.Millisecond)
						}
						cmd.Process.Kill()
						cmd.Wait()
					} else {
						if out, err := cmd.CombinedOutput(); err != nil {
							t.Fatal("child failed", err, string(out))
						}
					}
					for _, name := range []string{"mutation-inflight.json", runtimeResultFile, runtimeRecoveryConsumptionFile, runtimeRecoverySubmissionFile, runtimeRecoveryAcknowledgementFile, runtimeRecoveryFinalConsumptionFile, runtimeRecoveryFinalSubmissionFile} {
						if runtimeMarkerAbsent(filepath.Join(e.receiptDir, name)) {
							t.Fatal("crash lost state", name)
						}
					}
					if mode == "ack" && runtimeMarkerAbsent(filepath.Join(e.receiptDir, runtimeRecoveryFinalAcknowledgementFile)) {
						t.Fatal("crash lost final acknowledgement")
					}
					if lease, err := BeginRuntimeMutation(e.receiptDir, g.Expected.TaskID, TaskKindVPNCoreService); err == nil {
						lease.Close()
						t.Fatal("crash unlocked fence")
					}
				})
			}

		})
	}
}
