package tasks

import (
	"context"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"
)

func confirmTerminalForReleasePlan(t *testing.T, m *RuntimeRecoveryFinalization, b RuntimeRecoveryGrantBinding) {
	t.Helper()
	if e := m.WithTerminalSubmission(context.Background(), b, m.authorizationHash, func(g *RuntimeRecoveryTerminalGuard) error {
		p, e := g.PinProof(context.Background())
		if e != nil {
			return e
		}
		return g.RecordReceipt(context.Background(), receiptForTerminalProof(p))
	}); e != nil {
		t.Fatal(e)
	}
}

func TestRuntimeMutationFenceReleasePlanReadOnly(t *testing.T) {
	_, _, b, auth, m := terminalSubmissionFixture(t)
	defer m.Close()
	confirmTerminalForReleasePlan(t, m, b)
	before := terminalHistoryTree(t, m.dir)
	lock, _ := os.Lstat(filepath.Join(m.dir, "runtime.lock"))
	first, err := m.PlanFenceRelease(context.Background(), b, m.authorizationHash)
	if err != nil {
		t.Fatal(err)
	}
	if first.OperationID != auth.OperationID || first.Binding != b || first.AuthorizationHash != m.authorizationHash || first.ExpiresAt != auth.ExpiresAt || first.ArchiveName != filepath.Base(m.archive) || first.TerminalProofHash != markerDigest(m.terminalSubmission) || first.TerminalReceiptHash != markerDigest(m.terminalAcknowledgement) {
		t.Fatal("plan bindings")
	}
	for _, f := range first.Files {
		if markerDigest(before[f.Name]) != f.Hash {
			t.Fatal("plan inventory", f.Name)
		}
	}
	// Returned values have no alias to owner state and cannot renew authority.
	copy := first
	copy.Files[0].Hash = "foreign"
	for range 2 {
		p, e := m.PlanFenceRelease(context.Background(), b, m.authorizationHash)
		if e != nil || p != first {
			t.Fatal("repeat changed plan", e)
		}
	}
	assertTerminalHistoryTree(t, m.dir, before)
	after, _ := os.Lstat(filepath.Join(m.dir, "runtime.lock"))
	if !os.SameFile(lock, after) {
		t.Fatal("lock replaced")
	}
	if lease, e := BeginRuntimeMutation(m.dir, resultTaskID, TaskKindVPNCoreService); e == nil {
		lease.Close()
		t.Fatal("planner released flock")
	}
	m.Close()
	if _, e := m.PlanFenceRelease(context.Background(), b, m.authorizationHash); e != ErrRuntimeMutationBlocked {
		t.Fatal("closed owner adopted plan", e)
	}
	assertTerminalArchive(t, m.dir, b)
	assertTerminalHistoryTree(t, m.dir, before)
}

func TestRuntimeMutationFenceReleasePlanRejects(t *testing.T) {
	modes := []string{"missing_pin", "missing_ack", "generation", "manager", "agent", "server", "authorization", "closed", "poisoned", "cancelled", "expired", "clock_rollback", "runtime", "legacy_missing", "pin_changed", "ack_changed", "terminal_changed", "intent_changed", "archive_changed", "partial_cleanup", "unknown_file", "unknown_directory", "temporary_file", "root_mode", "legacy_mode", "legacy_symlink", "legacy_hardlink", "same_bytes_replaced_during_observation", "file_created_during_observation", "expiry_during_observation"}
	for _, mode := range modes {
		t.Run(mode, func(t *testing.T) {
			e, _, b, auth, m := terminalSubmissionFixture(t)
			defer m.Close()
			if mode != "missing_pin" {
				confirmTerminalForReleasePlan(t, m, b)
			}
			ctx := context.Background()
			hash := m.authorizationHash
			legacy := filepath.Join(m.dir, "mutation-inflight.json")
			write := func(path string, data []byte) {
				t.Helper()
				if e := os.WriteFile(path, data, 0600); e != nil {
					t.Fatal(e)
				}
			}
			switch mode {
			case "missing_ack":
				os.Remove(filepath.Join(m.dir, runtimeRecoveryTerminalAcknowledgementFile))
			case "generation":
				b.AgentCredentialGeneration++
			case "manager":
				b.ManagerURL = "https://foreign.invalid"
			case "agent":
				b.AgentID = resultTaskID
			case "server":
				b.ServerID = resultTaskID
			case "authorization":
				hash = "foreign"
			case "closed":
				m.Close()
			case "poisoned":
				m.poisoned = true
			case "cancelled":
				var cancel context.CancelFunc
				ctx, cancel = context.WithCancel(ctx)
				cancel()
			case "expired":
				m.now = func() time.Time { return auth.ExpiresAt }
			case "clock_rollback":
				m.now = func() time.Time { return m.lastChecked.Add(-time.Second) }
			case "runtime":
				write(e.activePath, []byte("{}"))
			case "legacy_missing":
				os.Remove(legacy)
			case "pin_changed":
				write(filepath.Join(m.dir, runtimeRecoveryTerminalSubmissionFile), []byte("{}"))
			case "ack_changed":
				write(filepath.Join(m.dir, runtimeRecoveryTerminalAcknowledgementFile), []byte("{}"))
			case "terminal_changed":
				write(filepath.Join(m.dir, runtimeRecoveryTerminalFile), []byte("{}"))
			case "intent_changed":
				write(filepath.Join(m.dir, runtimeRecoveryRetirementFile), []byte("{}"))
			case "archive_changed":
				write(filepath.Join(m.archive, runtimeRecoveryFinalSubmissionFile), []byte("{}"))
			case "partial_cleanup":
				data, err := os.ReadFile(filepath.Join(m.archive, runtimeResultFile))
				if err != nil {
					t.Fatal(err)
				}
				write(filepath.Join(m.dir, runtimeResultFile), data)
			case "unknown_file":
				write(filepath.Join(m.dir, "foreign.json"), []byte("{}"))
			case "unknown_directory":
				if err := os.Mkdir(filepath.Join(m.dir, "retired-foreign"), 0700); err != nil {
					t.Fatal(err)
				}
			case "temporary_file":
				write(filepath.Join(m.dir, ".retirement-interrupted"), []byte("{}"))
			case "root_mode":
				os.Chmod(m.dir, 0755)
			case "legacy_mode":
				os.Chmod(legacy, 0644)
			case "legacy_symlink":
				os.Remove(legacy)
				if err := os.Symlink(filepath.Join(m.archive, "mutation-inflight.json"), legacy); err != nil {
					t.Fatal(err)
				}
			case "legacy_hardlink":
				if err := os.Link(legacy, filepath.Join(t.TempDir(), "copy")); err != nil {
					t.Fatal(err)
				}
			case "same_bytes_replaced_during_observation", "file_created_during_observation", "expiry_during_observation":
				calls := 0
				m.now = func() time.Time {
					calls++
					if calls == 3 {
						switch mode {
						case "same_bytes_replaced_during_observation":
							data, err := os.ReadFile(legacy)
							if err != nil {
								t.Fatal(err)
							}
							replacement := filepath.Join(m.dir, ".replacement")
							write(replacement, data)
							if err := os.Rename(replacement, legacy); err != nil {
								t.Fatal(err)
							}
						case "file_created_during_observation":
							write(filepath.Join(m.dir, "foreign.json"), []byte("{}"))
						}
					}
					if mode == "expiry_during_observation" && calls >= 3 {
						return auth.ExpiresAt
					}
					return time.Now()
				}
			}
			before := terminalHistoryTree(t, m.dir)
			p, err := m.PlanFenceRelease(ctx, b, hash)
			if err != ErrRuntimeMutationBlocked || p != (RuntimeRecoveryFenceReleasePlan{}) {
				t.Fatal("unsafe plan accepted", err)
			}
			if mode != "file_created_during_observation" {
				assertTerminalHistoryTree(t, m.dir, before)
			}
			if mode == "same_bytes_replaced_during_observation" || mode == "file_created_during_observation" || mode == "expiry_during_observation" {
				if !m.poisoned {
					t.Fatal("uncertain owner not poisoned")
				}
			}
		})
	}
}

func TestRuntimeMutationFenceReleasePlanSerializesClose(t *testing.T) {
	_, _, b, _, m := terminalSubmissionFixture(t)
	defer m.Close()
	confirmTerminalForReleasePlan(t, m, b)
	entered, resume := make(chan struct{}), make(chan struct{})
	var once sync.Once
	m.now = func() time.Time { once.Do(func() { close(entered); <-resume }); return time.Now() }
	planned, closed := make(chan error, 1), make(chan struct{})
	go func() { _, err := m.PlanFenceRelease(context.Background(), b, m.authorizationHash); planned <- err }()
	<-entered
	go func() { m.Close(); close(closed) }()
	if lock, err := lockRuntimeMutation(m.dir); err == nil {
		lock.Close()
		t.Error("planner dropped original flock")
	}
	select {
	case <-closed:
		t.Error("Close bypassed planning mutex")
	default:
	}
	close(resume)
	if err := <-planned; err != nil {
		t.Fatal(err)
	}
	<-closed
	assertTerminalArchive(t, m.dir, b)
}
