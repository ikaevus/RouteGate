package tasks

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func recoveryHistoryFixture(t *testing.T) (CredentialRemovalExecutor, *removalAdapter, RuntimeRecoveryGrantBinding, *RuntimeRecoveryFinalization) {
	t.Helper()
	e, a, b, m := retentionSubmissionFixture(t)
	err := m.WithPreparedReleaseSubmission(context.Background(), b, m.authorizationHash, func(g *RuntimeRecoveryPreparedReleaseGuard) error {
		p, err := g.PinRetentionProof(context.Background())
		if err != nil {
			return err
		}
		return g.RecordRetentionReceipt(context.Background(), receiptForRetentionProof(p))
	})
	if err != nil {
		t.Fatal(err)
	}
	return e, a, b, m
}

func archiveHistory(t *testing.T, b RuntimeRecoveryGrantBinding, m *RuntimeRecoveryFinalization) string {
	t.Helper()
	var hash string
	if err := m.WithPreparedReleaseSubmission(context.Background(), b, m.authorizationHash, func(g *RuntimeRecoveryPreparedReleaseGuard) error {
		var err error
		hash, err = g.ArchiveRetainedHistory(context.Background())
		return err
	}); err != nil {
		t.Fatal(err)
	}
	return hash
}

func replaceHistoryFile(t *testing.T, path string) {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	tmp := path + ".replacement"
	if err = os.WriteFile(tmp, data, 0600); err != nil {
		t.Fatal(err)
	}
	if err = os.Rename(tmp, path); err != nil {
		t.Fatal(err)
	}
}

func TestRecoveryHistoryArchiveIndependent(t *testing.T) {
	e, _, b, m := recoveryHistoryFixture(t)
	before := terminalHistoryTree(t, m.dir)
	originalLock, _ := os.Lstat(filepath.Join(m.dir, "runtime.lock"))
	hash := archiveHistory(t, b, m)
	path, _ := recoveryHistoryArchivePath(m.dir, m.authorization.OperationID)
	copyBefore := terminalHistoryTree(t, path)
	if len(hash) != 64 || archiveHistory(t, b, m) != hash {
		t.Fatal("archive retry changed commitment")
	}
	assertTerminalHistoryTree(t, m.dir, before)
	assertTerminalHistoryTree(t, path, copyBefore)
	cloneLock, _ := os.Lstat(filepath.Join(path, "history", "runtime.lock"))
	if os.SameFile(originalLock, cloneLock) {
		t.Fatal("copied admission lock")
	}
	if err := filepath.WalkDir(path, func(p string, d os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		info, err := d.Info()
		if err != nil {
			return err
		}
		want := os.FileMode(0600)
		if d.IsDir() {
			want = 0700
		}
		if info.Mode().Perm() != want {
			t.Errorf("unsafe archive mode")
		}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	m.Close()
	// Test-only removal demonstrates independence; production API never deletes.
	for name := range before {
		if strings.Contains(name, string(filepath.Separator)) || name == "runtime.lock" || name == "mutation-inflight.json" {
			continue
		}
		if err := os.Remove(filepath.Join(m.dir, name)); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.RemoveAll(m.archive); err != nil {
		t.Fatal(err)
	}
	if err := os.Remove(e.activePath); err != nil {
		t.Fatal(err)
	}
	remaining := terminalHistoryTree(t, m.dir)
	called := false
	if err := WithArchivedRecoveryRetention(context.Background(), m.dir, m.authorization.OperationID, b, func(h RuntimeRecoveryArchivedHistory) error {
		called = true
		if h.ManifestHash != hash || !h.History.LocalReceipt.Recorded {
			t.Error("wrong archived history")
		}
		return nil
	}); err != nil || !called {
		t.Fatal("independent history unavailable", err)
	}
	if err := WithHistoricalRecoveryRetention(context.Background(), m.dir, b, func(RuntimeRecoveryRetentionHistory) error { return nil }); err != ErrRuntimeMutationBlocked {
		t.Fatal("original reader accepted missing chain", err)
	}
	if lease, err := BeginRuntimeMutation(m.dir, m.authorization.OperationID, TaskKindVPNCoreService); err != ErrRuntimeMutationBlocked {
		if lease != nil {
			lease.Close()
		}
		t.Fatal("archive released fence")
	}
	assertTerminalHistoryTree(t, m.dir, remaining)
	assertTerminalHistoryTree(t, path, copyBefore)
}

func TestRecoveryHistoryArchivePublicationFailures(t *testing.T) {
	for _, mode := range []string{"missing_ack", "existing", "directory", "copy", "copied", "published", "cancel", "runtime", "source_inode", "copy_inode", "manifest_inode", "lock_inode"} {
		t.Run(mode, func(t *testing.T) {
			_, a, b, m := recoveryHistoryFixture(t)
			path, _ := recoveryHistoryArchivePath(m.dir, m.authorization.OperationID)
			if mode == "missing_ack" {
				os.Remove(filepath.Join(m.dir, runtimeRecoveryRetentionAcknowledgementFile))
			}
			if mode == "existing" {
				if err := os.Mkdir(path, 0700); err != nil {
					t.Fatal(err)
				}
			}
			before := terminalHistoryTree(t, m.dir)
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			var hash string
			err := m.WithPreparedReleaseSubmission(ctx, b, m.authorizationHash, func(g *RuntimeRecoveryPreparedReleaseGuard) error {
				var err error
				hash, err = g.archiveRetainedHistory(ctx, func(step string) error {
					if step == mode || (mode == "copy" && strings.HasPrefix(step, "copy-")) {
						return errors.New("private checkpoint details")
					}
					if step == "copied" {
						switch mode {
						case "cancel":
							cancel()
						case "runtime":
							a.generation.PID++
						case "source_inode":
							replaceHistoryFile(t, filepath.Join(m.dir, runtimeRecoveryRetentionSubmissionFile))
						case "copy_inode":
							replaceHistoryFile(t, filepath.Join(path, "history", runtimeRecoveryRetentionSubmissionFile))
						case "lock_inode":
							replaceHistoryFile(t, filepath.Join(path, "history", "runtime.lock"))
						}
					}
					if step == "published" && mode == "manifest_inode" {
						replaceHistoryFile(t, filepath.Join(path, recoveryHistoryArchiveManifest))
					}
					return nil
				})
				return err
			})
			if err != ErrRuntimeMutationBlocked || hash != "" {
				t.Fatal("uncertain archive accepted", err, hash)
			}
			assertTerminalHistoryTree(t, m.dir, before)
			m.Close()
			if lease, err := BeginRuntimeMutation(m.dir, m.authorization.OperationID, TaskKindVPNCoreService); err != ErrRuntimeMutationBlocked {
				if lease != nil {
					lease.Close()
				}
				t.Fatal("failure released fence")
			}
			if mode == "directory" || mode == "copy" || mode == "copied" || mode == "copy_inode" || mode == "lock_inode" {
				if err := WithArchivedRecoveryRetention(context.Background(), m.dir, m.authorization.OperationID, b, func(RuntimeRecoveryArchivedHistory) error { t.Error("partial archive callback"); return nil }); err != ErrRuntimeMutationBlocked {
					t.Fatal("partial archive accepted", err)
				}
			}
		})
	}
}

func TestRecoveryHistoryArchiveReadRefusals(t *testing.T) {
	for _, mode := range []string{"purpose", "binding", "operation", "hash", "order", "duplicate", "traversal", "unknown", "missing", "symlink", "hardlink", "permissions", "foreign", "manifest_inode", "copy_inode", "lock_inode", "original_lock_inode", "cancelled"} {
		t.Run(mode, func(t *testing.T) {
			_, _, b, m := recoveryHistoryFixture(t)
			archiveHistory(t, b, m)
			m.Close()
			path, _ := recoveryHistoryArchivePath(m.dir, m.authorization.OperationID)
			manifest := filepath.Join(path, recoveryHistoryArchiveManifest)
			data, _ := os.ReadFile(manifest)
			var r recoveryHistoryArchiveRecord
			json.Unmarshal(data, &r)
			mutate := true
			switch mode {
			case "purpose":
				r.Purpose = "other"
			case "binding":
				r.Binding.AgentCredentialGeneration++
			case "operation":
				r.OperationID = "00000000-0000-4000-8000-000000000099"
			case "hash":
				r.ProofHash = strings.Repeat("0", 64)
			case "order":
				r.Files[0], r.Files[1] = r.Files[1], r.Files[0]
			case "duplicate":
				r.Files = append(r.Files, r.Files[0])
			case "traversal":
				r.Files[0].Name = "../../outside"
			default:
				mutate = false
			}
			if mutate {
				data, _ = json.Marshal(r)
				os.WriteFile(manifest, data, 0600)
			}
			file := filepath.Join(path, "history", runtimeRecoveryRetentionSubmissionFile)
			switch mode {
			case "unknown":
				os.WriteFile(manifest, append(data[:len(data)-1], []byte(`,"unknown":true}`)...), 0600)
			case "missing":
				os.Remove(file)
			case "permissions":
				os.Chmod(file, 0644)
			case "foreign":
				os.WriteFile(filepath.Join(path, "foreign"), []byte("private"), 0600)
			case "hardlink":
				os.Link(file, filepath.Join(t.TempDir(), "link"))
			case "symlink":
				os.Rename(file, file+".saved")
				os.Symlink(file+".saved", file)
			}
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			err := WithArchivedRecoveryRetention(ctx, m.dir, m.authorization.OperationID, b, func(RuntimeRecoveryArchivedHistory) error {
				switch mode {
				case "manifest_inode":
					replaceHistoryFile(t, manifest)
				case "copy_inode":
					replaceHistoryFile(t, file)
				case "lock_inode":
					replaceHistoryFile(t, filepath.Join(path, "history", "runtime.lock"))
				case "original_lock_inode":
					replaceHistoryFile(t, filepath.Join(m.dir, "runtime.lock"))
				case "cancelled":
					cancel()
				default:
					t.Error("invalid history reached callback")
				}
				return nil
			})
			if err != ErrRuntimeMutationBlocked {
				t.Fatal("invalid archive accepted", err)
			}
		})
	}
}

func TestRecoveryHistoryArchiveAuthority(t *testing.T) {
	for _, mode := range []string{"expired", "backwards", "unsafe_parent", "escaped"} {
		t.Run(mode, func(t *testing.T) {
			_, _, b, m := recoveryHistoryFixture(t)
			var escaped *RuntimeRecoveryPreparedReleaseGuard
			if mode == "expired" {
				m.now = func() time.Time { return m.authorization.ExpiresAt }
			}
			if mode == "backwards" {
				m.now = func() time.Time { return m.lastChecked.Add(-time.Second) }
			}
			if mode == "unsafe_parent" {
				parent := filepath.Dir(m.dir)
				if err := os.Chmod(parent, 0770); err != nil {
					t.Fatal(err)
				}
				defer os.Chmod(parent, 0700)
			}
			err := m.WithPreparedReleaseSubmission(context.Background(), b, m.authorizationHash, func(g *RuntimeRecoveryPreparedReleaseGuard) error {
				escaped = g
				if mode == "escaped" {
					return nil
				}
				hash, err := g.ArchiveRetainedHistory(context.Background())
				if hash != "" {
					t.Error("invalid authority returned archive")
				}
				return err
			})
			if mode == "escaped" {
				if err != nil {
					t.Fatal(err)
				}
				copyGuard := *escaped
				for _, guard := range []*RuntimeRecoveryPreparedReleaseGuard{escaped, &copyGuard} {
					if hash, err := guard.ArchiveRetainedHistory(context.Background()); hash != "" || err != ErrRuntimeMutationBlocked {
						t.Fatal("escaped archive authority", err)
					}
				}
			} else if err != ErrRuntimeMutationBlocked {
				t.Fatal("invalid authority accepted", err)
			}
		})
	}
}
