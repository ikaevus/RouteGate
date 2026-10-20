package tasks

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func cleanedArchiveFixture(t *testing.T) (CredentialRemovalExecutor, *removalAdapter, RuntimeRecoveryGrantBinding, *RuntimeRecoveryFinalization, string, string) {
	t.Helper()
	e, a, b, m, previous := cleanedCommitFixture(t)
	acknowledgeCleanedCommitForPlan(t, b, m)
	path, err := cleanedCommitArchivePath(m.dir, m.authorization.OperationID)
	if err != nil {
		t.Fatal(err)
	}
	return e, a, b, m, previous, path
}

func archiveCleanedForTest(t *testing.T, b RuntimeRecoveryGrantBinding, m *RuntimeRecoveryFinalization) string {
	t.Helper()
	var hash string
	if e := m.WithSourceCleanupSubmission(context.Background(), b, m.authorizationHash, func(g *RuntimeRecoveryPreparedReleaseGuard) error {
		var e error
		hash, e = g.ArchiveCleanedCommitHistory(context.Background())
		return e
	}); e != nil {
		t.Fatal(e)
	}
	return hash
}

func TestCleanedArchiveComplete(t *testing.T) {
	e, _, b, m, previous, path := cleanedArchiveFixture(t)
	before, backup := terminalHistoryTree(t, m.dir), terminalHistoryTree(t, previous)
	first := archiveCleanedForTest(t, b, m)
	if len(first) != 64 || first != markerDigest(m.cleanedCommitArchive) {
		t.Fatal("archive digest")
	}
	archived := terminalHistoryTree(t, path)
	for range 2 {
		if hash := archiveCleanedForTest(t, b, m); hash != first {
			t.Fatal("archive retry changed bytes")
		}
	}
	var escaped *RuntimeRecoveryPreparedReleaseGuard
	if err := m.WithSourceCleanupSubmission(context.Background(), b, m.authorizationHash, func(g *RuntimeRecoveryPreparedReleaseGuard) error {
		escaped = g
		_, e := g.PlanCleanedCommit(context.Background())
		return e
	}); err != nil {
		t.Fatal("owned archive invalidated plan", err)
	}
	copyGuard := *escaped
	for _, g := range []*RuntimeRecoveryPreparedReleaseGuard{nil, escaped, &copyGuard} {
		if hash, err := g.ArchiveCleanedCommitHistory(context.Background()); err != ErrRuntimeMutationBlocked || hash != "" {
			t.Fatal("escaped archive", err)
		}
	}
	assertTerminalHistoryTree(t, m.dir, before)
	assertTerminalHistoryTree(t, previous, backup)
	assertTerminalHistoryTree(t, path, archived)
	m.Close()
	os.Remove(e.activePath)
	// The copied final records remain inspectable when their root duplicates
	// have disappeared. Original admission barriers stay present in this test.
	for _, name := range []string{runtimeRecoveryCleanedCommitIntentFile, runtimeRecoveryCleanedCommitSubmissionFile, runtimeRecoveryCleanedCommitAcknowledgementFile} {
		if err := os.Remove(filepath.Join(m.dir, name)); err != nil {
			t.Fatal(err)
		}
		delete(before, name)
	}
	if err := WithArchivedRecoveryCleanedCommit(context.Background(), m.dir, m.authorization.OperationID, b, func(h RuntimeRecoveryCleanedCommitArchivedHistory) error {
		if h.ManifestHash != first || h.Proof != m.cleanedCommitFrozen || !h.Receipt.Recorded || h.Proof.CleanedIntentHash != markerDigest(archived["state/"+runtimeRecoveryCleanedCommitIntentFile]) {
			t.Error("copied chain bindings")
		}
		return nil
	}); err != nil {
		t.Fatal("independent cleaned history", err)
	}
	if lease, err := BeginRuntimeMutation(m.dir, resultTaskID, TaskKindVPNCoreService); err != ErrRuntimeMutationBlocked {
		if lease != nil {
			lease.Close()
		}
		t.Fatal("archive released fence")
	}
	assertTerminalHistoryTree(t, m.dir, before)
	assertTerminalHistoryTree(t, previous, backup)
	assertTerminalHistoryTree(t, path, archived)
}

func TestCleanedArchiveRefusals(t *testing.T) {
	for _, mode := range []string{"before", "directory", "state", "lock", "copy-0", "copy-1", "copy-2", "copy-3", "copy-4", "copied", "published", "done", "existing", "directory_replaced", "copy_replaced", "manifest_replaced", "runtime", "cancelled", "expiry", "backward", "source_replaced"} {
		t.Run(mode, func(t *testing.T) {
			_, a, b, m, previous, path := cleanedArchiveFixture(t)
			before, backup := terminalHistoryTree(t, m.dir), terminalHistoryTree(t, previous)
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			var hash string
			err := m.WithSourceCleanupSubmission(ctx, b, m.authorizationHash, func(g *RuntimeRecoveryPreparedReleaseGuard) error {
				var e error
				hash, e = g.archiveCleanedCommitHistory(ctx, func(step string) error {
					if step == mode {
						return errors.New("private failure")
					}
					if step == "before" && mode == "existing" {
						if e := os.Mkdir(path, 0700); e != nil {
							t.Fatal(e)
						}
					}
					if step == "directory" && mode == "directory_replaced" {
						if e := os.Rename(path, path+".moved"); e != nil {
							t.Fatal(e)
						}
						if e := os.Mkdir(path, 0700); e != nil {
							t.Fatal(e)
						}
					}
					if step == "copy-4" && mode == "copy_replaced" {
						replaceHistoryFile(t, filepath.Join(path, "state", runtimeRecoveryCleanedCommitSubmissionFile))
					}
					if step == "published" && mode == "manifest_replaced" {
						replaceHistoryFile(t, filepath.Join(path, recoveryHistoryArchiveManifest))
					}
					if step == "copied" {
						switch mode {
						case "runtime":
							a.generation.PID++
						case "cancelled":
							cancel()
						case "expiry":
							m.now = func() time.Time { return m.authorization.ExpiresAt }
						case "backward":
							m.now = func() time.Time { return m.lastChecked.Add(-time.Second) }
						case "source_replaced":
							replaceHistoryFile(t, filepath.Join(m.dir, runtimeRecoveryCleanedCommitSubmissionFile))
						}
					}
					return nil
				})
				return e
			})
			if err != ErrRuntimeMutationBlocked || hash != "" || !m.poisoned {
				t.Fatal("unsafe archive", err)
			}
			assertTerminalHistoryTree(t, m.dir, before)
			assertTerminalHistoryTree(t, previous, backup)
		})
	}
}

func TestCleanedArchiveHistory(t *testing.T) {
	for _, mode := range []string{"expired", "missing_sources", "wrong_generation", "missing_predecessor", "changed_manifest", "changed_proof", "unknown", "hardlink", "callback_manifest", "callback_state", "callback_predecessor", "cancelled"} {
		t.Run(mode, func(t *testing.T) {
			_, _, b, m, previous, path := cleanedArchiveFixture(t)
			hash := archiveCleanedForTest(t, b, m)
			if mode == "expired" {
				m.now = func() time.Time { return m.authorization.ExpiresAt }
				if e := m.WithSourceCleanupSubmission(context.Background(), b, m.authorizationHash, func(*RuntimeRecoveryPreparedReleaseGuard) error { t.Error("expired owner reused archive"); return nil }); e != ErrRuntimeMutationBlocked {
					t.Fatal(e)
				}
			}
			m.Close()
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			manifest := filepath.Join(path, recoveryHistoryArchiveManifest)
			switch mode {
			case "missing_sources":
				for _, name := range []string{runtimeRecoveryCleanedCommitIntentFile, runtimeRecoveryCleanedCommitSubmissionFile, runtimeRecoveryCleanedCommitAcknowledgementFile} {
					os.Remove(filepath.Join(m.dir, name))
				}
			case "wrong_generation":
				b.AgentCredentialGeneration++
			case "missing_predecessor":
				os.Remove(filepath.Join(previous, recoveryHistoryArchiveManifest))
			case "changed_manifest":
				os.WriteFile(manifest, []byte(`{}`), 0600)
			case "changed_proof":
				os.WriteFile(filepath.Join(path, "state", runtimeRecoveryCleanedCommitSubmissionFile), []byte(`{}`), 0600)
			case "unknown":
				os.WriteFile(filepath.Join(path, "foreign"), []byte("private"), 0600)
			case "hardlink":
				os.Link(filepath.Join(path, "state", runtimeRecoveryCleanedCommitSubmissionFile), filepath.Join(t.TempDir(), "linked"))
			case "cancelled":
				cancel()
			}
			before, backup, newCopy := terminalHistoryTree(t, m.dir), terminalHistoryTree(t, previous), terminalHistoryTree(t, path)
			err := WithArchivedRecoveryCleanedCommit(ctx, m.dir, m.authorization.OperationID, b, func(h RuntimeRecoveryCleanedCommitArchivedHistory) error {
				if h.ManifestHash != hash || !h.Receipt.Recorded {
					t.Error("historical chain")
				}
				switch mode {
				case "callback_manifest":
					replaceHistoryFile(t, manifest)
				case "callback_state":
					replaceHistoryFile(t, filepath.Join(path, "state", runtimeRecoveryCleanedCommitSubmissionFile))
				case "callback_predecessor":
					replaceHistoryFile(t, filepath.Join(previous, recoveryHistoryArchiveManifest))
				}
				return nil
			})
			if mode == "expired" || mode == "missing_sources" {
				if err != nil {
					t.Fatal(err)
				}
			} else if err != ErrRuntimeMutationBlocked {
				t.Fatal("unsafe history", err)
			}
			assertTerminalHistoryTree(t, m.dir, before)
			assertTerminalHistoryTree(t, previous, backup)
			assertTerminalHistoryTree(t, path, newCopy)
		})
	}
}

func TestCleanedArchiveOwnedDrift(t *testing.T) {
	for _, name := range []string{"manifest.json", "state/" + runtimeRecoveryCleanedCommitSubmissionFile} {
		t.Run(name, func(t *testing.T) {
			_, _, b, m, previous, path := cleanedArchiveFixture(t)
			archiveCleanedForTest(t, b, m)
			before, backup := terminalHistoryTree(t, m.dir), terminalHistoryTree(t, previous)
			replaceHistoryFile(t, filepath.Join(path, name))
			err := m.WithSourceCleanupSubmission(context.Background(), b, m.authorizationHash, func(*RuntimeRecoveryPreparedReleaseGuard) error {
				t.Error("live owner adopted substituted archive")
				return nil
			})
			if err != ErrRuntimeMutationBlocked {
				t.Fatal(err)
			}
			assertTerminalHistoryTree(t, m.dir, before)
			assertTerminalHistoryTree(t, previous, backup)
		})
	}
}
