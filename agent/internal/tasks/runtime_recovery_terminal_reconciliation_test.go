package tasks

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func terminalHistoryTree(t *testing.T, dir string) map[string][]byte {
	t.Helper()
	snapshot := map[string][]byte{}
	if err := filepath.WalkDir(dir, func(path string, entry fs.DirEntry, e error) error {
		if e != nil {
			return e
		}
		name, _ := filepath.Rel(dir, path)
		if entry.IsDir() {
			snapshot[name+"/"] = nil
			return nil
		}
		data, e := os.ReadFile(path)
		if e != nil {
			return e
		}
		snapshot[name] = data
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	return snapshot
}
func assertTerminalHistoryTree(t *testing.T, dir string, before map[string][]byte) {
	t.Helper()
	after := terminalHistoryTree(t, dir)
	if len(after) != len(before) {
		t.Fatal("read changed file tree")
	}
	for path, data := range before {
		got, ok := after[path]
		if !ok || !bytes.Equal(got, data) {
			t.Fatal("read changed history", path)
		}
	}
}
func TestRuntimeMutationHistoricalTerminalReadOnly(t *testing.T) {
	for _, ack := range []bool{false, true} {
		t.Run(map[bool]string{false: "pin_only", true: "with_ack"}[ack], func(t *testing.T) {
			e, a, b, auth := finalizationFixture(t)
			auth.ExpiresAt = time.Now().UTC().Add(2 * time.Second)
			wire, _ := json.Marshal(auth)
			m, err := BeginRuntimeRecoveryFinalization(context.Background(), e.receiptDir, b, wire, e.activePath, a)
			if err != nil {
				t.Fatal(err)
			}
			defer m.Close()
			if err = m.WithSubmission(context.Background(), b, markerDigest(wire), func(g *RuntimeRecoveryFinalizationGuard) error {
				p, e := g.PinProof(context.Background())
				if e != nil {
					return e
				}
				if e = g.RecordReceipt(context.Background(), receiptForFinalizationProof(p)); e != nil {
					return e
				}
				_, e = g.PrepareRetirement(context.Background())
				return e
			}); err != nil {
				t.Fatal(err)
			}
			if _, err = m.RetirePrepared(context.Background()); err != nil {
				t.Fatal(err)
			}
			var pinned RuntimeRecoveryTerminalProof
			if err = m.WithTerminalSubmission(context.Background(), b, markerDigest(wire), func(g *RuntimeRecoveryTerminalGuard) error {
				var e error
				pinned, e = g.PinProof(context.Background())
				if e != nil {
					return e
				}
				if ack {
					return g.RecordReceipt(context.Background(), receiptForTerminalProof(pinned))
				}
				return nil
			}); err != nil {
				t.Fatal(err)
			}
			if WithHistoricalRecoveryTerminal(context.Background(), m.dir, b, func(RuntimeRecoveryTerminalProof) error { t.Error("live owner bypass"); return nil }) != ErrRuntimeMutationBlocked {
				t.Fatal("reader ignored live flock")
			}
			m.Close()
			time.Sleep(time.Until(auth.ExpiresAt) + 10*time.Millisecond)
			if err = os.WriteFile(e.activePath, []byte("{}"), 0600); err != nil {
				t.Fatal(err)
			}
			before := terminalHistoryTree(t, m.dir)
			lockInfo, _ := os.Lstat(filepath.Join(m.dir, "runtime.lock"))
			for range 2 {
				err = WithHistoricalRecoveryTerminal(context.Background(), m.dir, b, func(p RuntimeRecoveryTerminalProof) error {
					if p != pinned {
						t.Error("historical proof changed")
					}
					if lock, e := lockRuntimeMutation(m.dir); e == nil {
						lock.Close()
						t.Error("reader dropped original flock")
					}
					return nil
				})
				if err != nil {
					t.Fatal("expired archival history not readable", err)
				}
			}
			assertTerminalHistoryTree(t, m.dir, before)
			afterLock, _ := os.Lstat(filepath.Join(m.dir, "runtime.lock"))
			if !os.SameFile(lockInfo, afterLock) {
				t.Fatal("reader recreated lock inode")
			}
			if _, err = BeginRuntimeRecoveryFinalization(context.Background(), m.dir, b, wire, e.activePath, a); err != ErrRuntimeMutationBlocked {
				t.Fatal("reader resumed live owner", err)
			}
			assertTerminalArchive(t, m.dir, b)
		})
	}
}

func TestRuntimeMutationHistoricalTerminalRejects(t *testing.T) {
	for _, mode := range []string{"missing_pin", "missing_terminal", "generation", "manager", "agent", "server", "cancelled", "nil_callback", "pin_changed", "terminal_changed", "archive_changed", "ack_created", "ack_changed", "pin_removed", "legacy_removed", "callback_error", "concurrent_reader"} {
		t.Run(mode, func(t *testing.T) {
			_, _, b, _, m := terminalSubmissionFixture(t)
			var pinned RuntimeRecoveryTerminalProof
			if mode != "missing_pin" {
				if err := m.WithTerminalSubmission(context.Background(), b, m.authorizationHash, func(g *RuntimeRecoveryTerminalGuard) error {
					var e error
					pinned, e = g.PinProof(context.Background())
					if e != nil {
						return e
					}
					if mode == "ack_changed" {
						return g.RecordReceipt(context.Background(), receiptForTerminalProof(pinned))
					}
					return nil
				}); err != nil {
					t.Fatal(err)
				}
			}
			m.Close()
			ctx := context.Background()
			switch mode {
			case "missing_terminal":
				os.Remove(filepath.Join(m.dir, runtimeRecoveryTerminalFile))
			case "generation":
				b.AgentCredentialGeneration++
			case "manager":
				b.ManagerURL = "https://foreign.invalid"
			case "agent":
				b.AgentID = resultTaskID
			case "server":
				b.ServerID = resultTaskID
			case "cancelled":
				var cancel context.CancelFunc
				ctx, cancel = context.WithCancel(ctx)
				cancel()
			}
			called := false
			fn := func(p RuntimeRecoveryTerminalProof) error {
				called = true
				if p != pinned {
					t.Error("reader reconstructed another proof")
				}
				switch mode {
				case "pin_changed":
					os.WriteFile(filepath.Join(m.dir, runtimeRecoveryTerminalSubmissionFile), []byte("{}"), 0600)
				case "terminal_changed":
					os.WriteFile(filepath.Join(m.dir, runtimeRecoveryTerminalFile), []byte("{}"), 0600)
				case "archive_changed":
					os.WriteFile(filepath.Join(m.archive, runtimeRecoveryFinalSubmissionFile), []byte("{}"), 0600)
				case "ack_created", "ack_changed":
					os.WriteFile(filepath.Join(m.dir, runtimeRecoveryTerminalAcknowledgementFile), []byte("{}"), 0600)
				case "pin_removed":
					os.Remove(filepath.Join(m.dir, runtimeRecoveryTerminalSubmissionFile))
				case "legacy_removed":
					os.Remove(filepath.Join(m.dir, "mutation-inflight.json"))
				case "callback_error":
					return errors.New("secret-local-path")
				case "concurrent_reader":
					if err := WithHistoricalRecoveryTerminal(ctx, m.dir, b, func(RuntimeRecoveryTerminalProof) error { t.Error("concurrent flock bypass"); return nil }); err != ErrRuntimeMutationBlocked {
						t.Error("parallel reader admitted", err)
					}
					return errors.New("stop")
				}
				return nil
			}
			if mode == "nil_callback" {
				fn = nil
			}
			err := WithHistoricalRecoveryTerminal(ctx, m.dir, b, fn)
			expectCall := mode == "pin_changed" || mode == "terminal_changed" || mode == "archive_changed" || mode == "ack_created" || mode == "ack_changed" || mode == "pin_removed" || mode == "legacy_removed" || mode == "callback_error" || mode == "concurrent_reader"
			if err != ErrRuntimeMutationBlocked || called != expectCall {
				t.Fatal("unsafe historical read", err, called)
			}
		})
	}
}
