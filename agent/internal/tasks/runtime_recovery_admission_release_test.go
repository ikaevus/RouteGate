package tasks

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func releaseAdmissionForTest(m *RuntimeRecoveryFinalization, b RuntimeRecoveryGrantBinding, ctx context.Context, cut func(string) error) (RuntimeRecoveryAdmissionReleaseResult, error) {
	r := RuntimeRecoveryAdmissionReleaseResult{Outcome: RuntimeRecoveryAdmissionNotCommitted}
	e := m.WithSourceCleanupSubmission(ctx, b, m.authorizationHash, func(g *RuntimeRecoveryPreparedReleaseGuard) error {
		var e error
		r, e = g.releaseAdmission(ctx, cut)
		return e
	})
	return r, e
}

func TestAdmissionReleaseComplete(t *testing.T) {
	_, _, b, m, previous, path := cleanedArchiveFixture(t)
	hash := archiveCleanedForTest(t, b, m)
	backup, copied := terminalHistoryTree(t, previous), terminalHistoryTree(t, path)
	lockBefore, e := os.Lstat(filepath.Join(m.dir, "runtime.lock"))
	if e != nil {
		t.Fatal(e)
	}
	r, e := releaseAdmissionForTest(m, b, context.Background(), nil)
	if e != nil || r.Outcome != RuntimeRecoveryAdmissionDurable || r.RemovedFiles != 5 || r.OperationID != m.authorization.OperationID || r.ArchiveHash != hash {
		t.Fatal("release result", r, e)
	}
	entries, e := os.ReadDir(m.dir)
	if e != nil || len(entries) != 1 || entries[0].Name() != "runtime.lock" {
		t.Fatal("remaining admission records", e)
	}
	lockAfter, e := os.Lstat(filepath.Join(m.dir, "runtime.lock"))
	if e != nil || !os.SameFile(lockBefore, lockAfter) {
		t.Fatal("original lock replaced")
	}
	if lease, e := BeginRuntimeMutation(m.dir, resultTaskID, TaskKindVPNCoreService); e != ErrRuntimeMutationBlocked {
		if lease != nil {
			lease.Close()
		}
		t.Fatal("original flock released early")
	}
	if _, e := releaseAdmissionForTest(m, b, context.Background(), nil); e != ErrRuntimeMutationBlocked {
		t.Fatal("completed owner reentered")
	}
	m.Close()
	if e := WithArchivedRecoveryCleanedCommit(context.Background(), m.dir, m.authorization.OperationID, b, func(h RuntimeRecoveryCleanedCommitArchivedHistory) error {
		if h.ManifestHash != hash {
			t.Error("retained chain")
		}
		return nil
	}); e != nil {
		t.Fatal(e)
	}
	lease, e := BeginRuntimeMutation(m.dir, resultTaskID, TaskKindVPNCoreService)
	if e != nil {
		t.Fatal("durable release still fenced", e)
	}
	if e := lease.Complete(); e != nil {
		t.Fatal(e)
	}
	lease.Close()
	assertTerminalHistoryTree(t, previous, backup)
	assertTerminalHistoryTree(t, path, copied)
}

func TestAdmissionReleaseScope(t *testing.T) {
	_, _, b, m, _, _ := cleanedArchiveFixture(t)
	archiveCleanedForTest(t, b, m)
	var escaped *RuntimeRecoveryPreparedReleaseGuard
	if e := m.WithSourceCleanupSubmission(context.Background(), b, m.authorizationHash, func(g *RuntimeRecoveryPreparedReleaseGuard) error { escaped = g; return nil }); e != nil {
		t.Fatal(e)
	}
	copy := *escaped
	for _, g := range []*RuntimeRecoveryPreparedReleaseGuard{nil, escaped, &copy} {
		if r, e := g.ReleaseAdmission(context.Background()); e != ErrRuntimeMutationBlocked || r.Outcome != RuntimeRecoveryAdmissionNotCommitted {
			t.Fatal("escaped release", r, e)
		}
	}
	entered, proceed, done, closed := make(chan struct{}), make(chan struct{}), make(chan error, 1), make(chan struct{})
	go func() {
		_, e := releaseAdmissionForTest(m, b, context.Background(), func(step string) error {
			if step == "before-final" {
				close(entered)
				<-proceed
			}
			return nil
		})
		done <- e
	}()
	<-entered
	go func() { m.Close(); close(closed) }()
	select {
	case <-closed:
		t.Error("Close escaped in-flight release")
	case <-time.After(30 * time.Millisecond):
	}
	close(proceed)
	if e := <-done; e != nil {
		t.Fatal(e)
	}
	<-closed
	if _, e := releaseAdmissionForTest(m, b, context.Background(), nil); e != ErrRuntimeMutationBlocked {
		t.Fatal("closed owner resumed")
	}
}

func TestAdmissionReleaseCutsEarly(t *testing.T) {
	testAdmissionReleaseCuts(t, []string{"before", "pinned-1", "unlinked-1", "file-1", "pinned-2", "unlinked-2", "file-2", "pinned-3", "unlinked-3"})
}
func TestAdmissionReleaseCutsFinal(t *testing.T) {
	testAdmissionReleaseCuts(t, []string{"file-3", "pinned-4", "unlinked-4", "file-4", "before-final", "pinned-5", "unlinked-5", "file-5", "done"})
}
func testAdmissionReleaseCuts(t *testing.T, cuts []string) {
	for _, cut := range cuts {
		t.Run(cut, func(t *testing.T) {
			_, _, b, m, previous, path := cleanedArchiveFixture(t)
			archiveCleanedForTest(t, b, m)
			backup, copied := terminalHistoryTree(t, previous), terminalHistoryTree(t, path)
			r, e := releaseAdmissionForTest(m, b, context.Background(), func(step string) error {
				if step == cut {
					return errors.New("private injected failure")
				}
				return nil
			})
			uncertain := cut == "unlinked-5" || cut == "file-5" || cut == "done"
			want := RuntimeRecoveryAdmissionNotCommitted
			if uncertain {
				want = RuntimeRecoveryAdmissionUncertain
			}
			if e != ErrRuntimeMutationBlocked || r.Outcome != want || !m.poisoned {
				t.Fatal("misreported release", r, e)
			}
			if uncertain != runtimeMarkerAbsent(filepath.Join(m.dir, "mutation-inflight.json")) {
				t.Fatal("final marker classification")
			}
			if _, e := releaseAdmissionForTest(m, b, context.Background(), nil); e != ErrRuntimeMutationBlocked {
				t.Fatal("uncertain owner resumed")
			}
			m.Close()
			lease, e := BeginRuntimeMutation(m.dir, resultTaskID, TaskKindVPNCoreService)
			if uncertain {
				if e != nil {
					t.Fatal("last unlink not recognized", e)
				}
				lease.Complete()
				lease.Close()
			} else if e != ErrRuntimeMutationBlocked {
				if lease != nil {
					lease.Close()
				}
				t.Fatal("partial prefix released")
			}
			assertTerminalHistoryTree(t, previous, backup)
			assertTerminalHistoryTree(t, path, copied)
		})
	}
}

func TestAdmissionReleaseRefusalsBefore(t *testing.T) {
	testAdmissionReleaseRefusals(t, []string{"missing_archive", "copy_inode", "predecessor_inode", "unknown", "marker_inode", "lock_inode", "runtime", "expiry"})
}
func TestAdmissionReleaseRefusalsDuring(t *testing.T) {
	testAdmissionReleaseRefusals(t, []string{"backward", "cancelled", "after_prefix_runtime", "after_prefix_source", "after_prefix_copy", "after_prefix_cancelled", "before_final_expiry", "after_final_cancelled"})
}
func testAdmissionReleaseRefusals(t *testing.T, modes []string) {
	for _, mode := range modes {
		t.Run(mode, func(t *testing.T) {
			_, a, b, m, previous, path := cleanedArchiveFixture(t)
			if mode != "missing_archive" {
				archiveCleanedForTest(t, b, m)
			}
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			switch mode {
			case "copy_inode":
				replaceHistoryFile(t, filepath.Join(path, "manifest.json"))
			case "predecessor_inode":
				replaceHistoryFile(t, filepath.Join(previous, "manifest.json"))
			case "unknown":
				os.WriteFile(filepath.Join(m.dir, "unknown"), []byte("private"), 0600)
			case "marker_inode":
				replaceHistoryFile(t, filepath.Join(m.dir, "mutation-inflight.json"))
			case "lock_inode":
				replaceHistoryFile(t, filepath.Join(m.dir, "runtime.lock"))
			case "runtime":
				a.generation.PID++
			case "expiry":
				m.now = func() time.Time { return m.authorization.ExpiresAt }
			case "backward":
				m.now = func() time.Time { return m.lastChecked.Add(-time.Second) }
			case "cancelled":
				cancel()
			}
			r, e := releaseAdmissionForTest(m, b, ctx, func(step string) error {
				if step == "file-1" {
					switch mode {
					case "after_prefix_runtime":
						a.generation.PID++
					case "after_prefix_source":
						replaceHistoryFile(t, filepath.Join(m.dir, runtimeRecoveryCleanedCommitSubmissionFile))
					case "after_prefix_copy":
						replaceHistoryFile(t, filepath.Join(path, "state", runtimeRecoveryCleanedCommitSubmissionFile))
					case "after_prefix_cancelled":
						cancel()
					}
				}
				if step == "before-final" && mode == "before_final_expiry" {
					m.now = func() time.Time { return m.authorization.ExpiresAt }
				}
				if step == "unlinked-5" && mode == "after_final_cancelled" {
					cancel()
				}
				return nil
			})
			want := RuntimeRecoveryAdmissionNotCommitted
			if mode == "after_final_cancelled" {
				want = RuntimeRecoveryAdmissionUncertain
			}
			if e != ErrRuntimeMutationBlocked || r.Outcome != want {
				t.Fatal("accepted unsafe release", r, e)
			}
			if mode != "after_final_cancelled" && runtimeMarkerAbsent(filepath.Join(m.dir, "mutation-inflight.json")) {
				t.Fatal("lost final barrier before commit")
			}
		})
	}
}
