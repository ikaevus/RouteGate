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

func TestRuntimeMutationFenceReleaseIntentCrash(t *testing.T) {
	for _, mode := range []string{"terminal_confirm_release_intent_cut", "terminal_confirm_release_intent"} {
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
				assertReleaseIntentFenced(t, e.receiptDir, b)
				before := terminalHistoryTree(t, e.receiptDir)
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

func TestRuntimeMutationFenceReleaseIntentSerializesClose(t *testing.T) {
	_, _, b, _, m := releaseIntentFixture(t)
	defer m.Close()
	entered, resume, closed := make(chan struct{}), make(chan struct{}), make(chan struct{})
	prepared := make(chan error, 1)
	go func() {
		_, err := m.prepareFenceRelease(context.Background(), b, m.authorizationHash, func() error { close(entered); <-resume; return nil })
		prepared <- err
	}()
	<-entered
	go func() { m.Close(); close(closed) }()
	if l, err := lockRuntimeMutation(m.dir); err == nil {
		l.Close()
		t.Error("publication dropped original flock")
	}
	select {
	case <-closed:
		t.Error("Close bypassed publication mutex")
	default:
	}
	close(resume)
	if err := <-prepared; err != nil {
		t.Fatal(err)
	}
	<-closed
	assertReleaseIntentFenced(t, m.dir, b)
}
