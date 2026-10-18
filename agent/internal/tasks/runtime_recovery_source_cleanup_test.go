package tasks

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"testing"
)

func sourceCleanupFixture(t *testing.T) (CredentialRemovalExecutor, *removalAdapter, RuntimeRecoveryGrantBinding, *RuntimeRecoveryFinalization, string) {
	t.Helper()
	e, a, b, m := recoveryHistoryFixture(t)
	archiveHistory(t, b, m)
	path, _ := recoveryHistoryArchivePath(m.dir, m.authorization.OperationID)
	return e, a, b, m, path
}

func TestArchivedSourceCleanupComplete(t *testing.T) {
	e, _, b, m, path := sourceCleanupFixture(t)
	backup := terminalHistoryTree(t, path)
	original := terminalHistoryTree(t, m.dir)
	var first RuntimeRecoverySourceCleanupStatus
	var escaped *RuntimeRecoveryPreparedReleaseGuard
	for range 2 {
		if err := m.WithSourceCleanupSubmission(context.Background(), b, m.authorizationHash, func(g *RuntimeRecoveryPreparedReleaseGuard) error {
			escaped = g
			p, err := g.CleanupPreparedProof(context.Background())
			if err != nil || p != m.preparedFrozen {
				t.Fatal("prepared binding", err)
			}
			r, err := g.CleanupRetentionProof(context.Background())
			if err != nil || r != m.retentionFrozen {
				t.Fatal("retention binding", err)
			}
			s, err := g.CleanupArchivedSources(context.Background())
			if err != nil {
				return err
			}
			if !s.SourcesRemoved || s.RemovedFiles != s.TotalFiles || s.TotalFiles != 17 || s.OperationID != m.authorization.OperationID || s.ManifestHash != markerDigest(m.historyArchive) {
				t.Fatal("cleanup status")
			}
			if first.ManifestHash != "" && first != s {
				t.Fatal("retry changed status")
			}
			first = s
			return nil
		}); err != nil {
			t.Fatal(err)
		}
	}
	remaining := terminalHistoryTree(t, m.dir)
	if len(remaining) != 4 {
		t.Fatal("unexpected remaining tree", len(remaining))
	}
	for _, name := range []string{"runtime.lock", "mutation-inflight.json", runtimeRecoveryReleaseIntentFile} {
		if !reflect.DeepEqual(original[name], remaining[name]) {
			t.Fatal("changed preserved barrier")
		}
	}
	copyGuard := *escaped
	for _, g := range []*RuntimeRecoveryPreparedReleaseGuard{nil, escaped, &copyGuard} {
		if s, err := g.CleanupArchivedSources(context.Background()); err != ErrRuntimeMutationBlocked || s != (RuntimeRecoverySourceCleanupStatus{}) {
			t.Fatal("escaped cleanup", err)
		}
	}
	if err := m.WithPreparedReleaseSubmission(context.Background(), b, m.authorizationHash, func(*RuntimeRecoveryPreparedReleaseGuard) error { t.Error("old phase reentered"); return nil }); err != ErrRuntimeMutationBlocked {
		t.Fatal("old phase", err)
	}
	m.Close()
	os.Remove(e.activePath)
	if err := WithRecoverySourceCleanupHistory(context.Background(), m.dir, m.authorization.OperationID, b, func(s RuntimeRecoverySourceCleanupStatus, h RuntimeRecoveryRetentionHistory) error {
		if s != first || !h.LocalReceipt.Recorded {
			t.Error("historical status drift")
		}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	if l, err := BeginRuntimeMutation(m.dir, m.authorization.OperationID, TaskKindVPNCoreService); err != ErrRuntimeMutationBlocked {
		if l != nil {
			l.Close()
		}
		t.Fatal("cleanup released admission")
	}
	assertTerminalHistoryTree(t, path, backup)
	assertTerminalHistoryTree(t, m.dir, remaining)
}

func TestArchivedSourceCleanupFailures(t *testing.T) {
	for _, mode := range []string{"before", "first", "last", "directory", "cancel", "runtime", "source_inode", "copy_inode", "foreign"} {
		t.Run(mode, func(t *testing.T) {
			_, a, b, m, path := sourceCleanupFixture(t)
			backup := terminalHistoryTree(t, path)
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			var result RuntimeRecoverySourceCleanupStatus
			err := m.WithSourceCleanupSubmission(ctx, b, m.authorizationHash, func(g *RuntimeRecoveryPreparedReleaseGuard) error {
				var err error
				result, err = g.cleanupArchivedSources(ctx, func(step string) error {
					if step == mode || mode == "first" && step == "file-1" || mode == "last" && m.sourceCleanup.removed == len(m.sourceCleanup.sources) {
						return errors.New("private cut")
					}
					if step == "before" {
						switch mode {
						case "cancel":
							cancel()
						case "runtime":
							a.generation.PID++
						case "source_inode":
							replaceHistoryFile(t, filepath.Join(m.dir, runtimeRecoveryRetirementFile))
						case "copy_inode":
							replaceHistoryFile(t, filepath.Join(path, "history", runtimeRecoveryRetentionSubmissionFile))
						case "foreign":
							os.WriteFile(filepath.Join(m.dir, "foreign"), []byte("private"), 0600)
						}
					}
					return nil
				})
				return err
			})
			if err != ErrRuntimeMutationBlocked || result != (RuntimeRecoverySourceCleanupStatus{}) {
				t.Fatal("uncertainty accepted", err)
			}
			before := terminalHistoryTree(t, m.dir)
			if err := m.WithSourceCleanupSubmission(context.Background(), b, m.authorizationHash, func(*RuntimeRecoveryPreparedReleaseGuard) error { t.Error("resumed uncertain cleanup"); return nil }); err != ErrRuntimeMutationBlocked {
				t.Fatal("resumed", err)
			}
			m.Close()
			if mode != "foreign" {
				if err := WithRecoverySourceCleanupHistory(context.Background(), m.dir, m.authorization.OperationID, b, func(s RuntimeRecoverySourceCleanupStatus, _ RuntimeRecoveryRetentionHistory) error {
					if mode == "first" && s.RemovedFiles != 1 {
						t.Error("lost prefix")
					}
					return nil
				}); err != nil {
					t.Fatal("read-only crash history", err)
				}
			}
			if l, err := BeginRuntimeMutation(m.dir, m.authorization.OperationID, TaskKindVPNCoreService); err != ErrRuntimeMutationBlocked {
				if l != nil {
					l.Close()
				}
				t.Fatal("failure released fence")
			}
			assertTerminalHistoryTree(t, m.dir, before)
			assertTerminalHistoryTree(t, path, backup)
		})
	}
}

func TestArchivedSourceCleanupHistoryRefusals(t *testing.T) {
	for _, mode := range []string{"hole", "missing_barrier", "changed_barrier", "foreign", "lock_inode", "source_inode", "manifest_inode", "cancel"} {
		t.Run(mode, func(t *testing.T) {
			_, _, b, m, path := sourceCleanupFixture(t)
			m.Close()
			switch mode {
			case "hole":
				os.Remove(filepath.Join(m.dir, runtimeRecoveryTerminalFile))
			case "missing_barrier":
				os.Remove(filepath.Join(m.dir, "mutation-inflight.json"))
			case "changed_barrier":
				os.WriteFile(filepath.Join(m.dir, runtimeRecoveryReleaseIntentFile), []byte("{}"), 0600)
			case "foreign":
				os.WriteFile(filepath.Join(m.dir, "foreign"), []byte("private"), 0600)
			}
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			err := WithRecoverySourceCleanupHistory(ctx, m.dir, m.authorization.OperationID, b, func(RuntimeRecoverySourceCleanupStatus, RuntimeRecoveryRetentionHistory) error {
				switch mode {
				case "lock_inode":
					replaceHistoryFile(t, filepath.Join(m.dir, "runtime.lock"))
				case "source_inode":
					replaceHistoryFile(t, filepath.Join(m.dir, runtimeRecoveryRetentionSubmissionFile))
				case "manifest_inode":
					replaceHistoryFile(t, filepath.Join(path, recoveryHistoryArchiveManifest))
				case "cancel":
					cancel()
				default:
					t.Error("invalid state reached callback")
				}
				return nil
			})
			if err != ErrRuntimeMutationBlocked {
				t.Fatal("invalid history", err)
			}
		})
	}
}
