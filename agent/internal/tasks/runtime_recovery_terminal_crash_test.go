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

func TestRuntimeMutationTerminalRetirementCrash(t *testing.T) {
	cuts := []string{"archive", "terminal"}
	for _, name := range []string{"mutation-inflight.json", runtimeResultFile, runtimeRecoveryConsumptionFile, runtimeRecoverySubmissionFile, runtimeRecoveryAcknowledgementFile, runtimeRecoveryFinalConsumptionFile, runtimeRecoveryFinalSubmissionFile, runtimeRecoveryFinalAcknowledgementFile} {
		cuts = append(cuts, "copy-"+name)
		if name != "mutation-inflight.json" {
			cuts = append(cuts, "cleanup-"+name)
		}
	}
	for _, cut := range cuts {
		for _, kill := range []bool{true, false} {
			if !kill && cut != "archive" && cut != "terminal" && cut != "cleanup-"+runtimeResultFile {
				continue
			}
			t.Run(cut+map[bool]string{true: "/SIGKILL", false: "/exit"}[kill], func(t *testing.T) {
				e, _, b, g, _ := recoveryCommitFixture(t)
				input, _ := json.Marshal(struct {
					Dir, Active string
					Binding     RuntimeRecoveryGrantBinding
					Grant       RuntimeRecoveryMutationGrant
				}{e.receiptDir, e.activePath, b, g})
				ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
				defer cancel()
				cmd := exec.CommandContext(ctx, os.Args[0], "-test.run=^TestRuntimeMutationRecoveryCommitProcessExit$")
				cmd.Env = append(os.Environ(), "RG_TEST_RECOVERY_COMMIT_CHILD="+string(input), "RG_TEST_RECOVERY_FINAL_CHILD=1", "RG_TEST_RECOVERY_FINAL_SUBMIT_CHILD=terminal", "RG_TEST_RECOVERY_TERMINAL_CUT="+cut)
				if kill {
					cmd.Env = append(cmd.Env, "RG_TEST_RECOVERY_COMMIT_KILL=1")
					if err := cmd.Start(); err != nil {
						t.Fatal(err)
					}
					defer func() { cmd.Process.Kill(); cmd.Wait() }()
					for runtimeMarkerAbsent(filepath.Join(e.receiptDir, "test-commit-ready")) {
						if ctx.Err() != nil {
							t.Fatal("child did not reach cut")
						}
						time.Sleep(10 * time.Millisecond)
					}
					cmd.Process.Kill()
					cmd.Wait()
				} else {
					if out, err := cmd.CombinedOutput(); err != nil {
						t.Fatal("child cut", err, string(out))
					}
				}
				if runtimeMarkerAbsent(filepath.Join(e.receiptDir, runtimeRecoveryRetirementFile)) {
					t.Fatal("cut lost journal fence")
				}
				// Before terminal commit, no source can be removed. After commit, complete
				// archival history must be readable even when active cleanup was interrupted.
				if runtimeMarkerAbsent(filepath.Join(e.receiptDir, runtimeRecoveryTerminalFile)) {
					for _, name := range []string{"mutation-inflight.json", runtimeResultFile, runtimeRecoveryConsumptionFile, runtimeRecoverySubmissionFile, runtimeRecoveryAcknowledgementFile, runtimeRecoveryFinalConsumptionFile, runtimeRecoveryFinalSubmissionFile, runtimeRecoveryFinalAcknowledgementFile} {
						if runtimeMarkerAbsent(filepath.Join(e.receiptDir, name)) {
							t.Fatal("uncommitted cut lost active source", name)
						}
					}
					assertHistoricalFinalizationAfterExit(t, e.receiptDir, b)
				} else {
					assertTerminalArchive(t, e.receiptDir, b)
				}
				if lease, err := BeginRuntimeMutation(e.receiptDir, g.Expected.TaskID, TaskKindVPNCoreService); err == nil {
					lease.Close()
					t.Fatal("cut released fence")
				}
			})
		}
	}
}
