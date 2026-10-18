package tasks

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"
)

func TestArchivedSourcePlanReadOnly(t *testing.T) {
	_, _, b, m := recoveryHistoryFixture(t)
	hash := archiveHistory(t, b, m)
	path, _ := recoveryHistoryArchivePath(m.dir, m.authorization.OperationID)
	before, backup := terminalHistoryTree(t, m.dir), terminalHistoryTree(t, path)
	expectedCandidates := 0
	for name := range before {
		if !strings.HasSuffix(name, "/") && name != "runtime.lock" && name != "mutation-inflight.json" && name != runtimeRecoveryReleaseIntentFile {
			expectedCandidates++
		}
	}
	var first RuntimeRecoverySourceCleanupPlan
	var escaped *RuntimeRecoveryPreparedReleaseGuard
	for range 2 {
		if err := m.WithPreparedReleaseSubmission(context.Background(), b, m.authorizationHash, func(g *RuntimeRecoveryPreparedReleaseGuard) error {
			escaped = g
			p, err := g.PlanArchivedSourceCleanup(context.Background())
			if err != nil {
				return err
			}
			if first.ManifestHash != "" && !reflect.DeepEqual(first, p) {
				t.Error("repeat changed plan")
			}
			first = p
			if p.OperationID != m.authorization.OperationID || p.Binding != b || p.AuthorizationHash != m.authorizationHash || p.ManifestHash != hash || p.RetentionProofHash != markerDigest(m.retentionSubmission) || p.RetentionReceiptHash != markerDigest(m.retentionReceipt) || !p.ExpiresAt.Equal(m.authorization.ExpiresAt) || p.IndependentArchiveName != filepath.Base(path) || p.OriginalArchiveName != filepath.Base(m.archive) || p.AdmissionLockName != "runtime.lock" {
				t.Fatal("unbound cleanup plan")
			}
			// The older commit acknowledgement is explicitly optional in the
			// validated predecessor chain; never invent a missing file.
			if len(p.Candidates) != expectedCandidates || p.RootBarriers[0].Name != "mutation-inflight.json" || p.RootBarriers[1].Name != runtimeRecoveryReleaseIntentFile {
				t.Fatal("incorrect candidate/barrier partition")
			}
			seen := map[string]bool{}
			for _, f := range p.Candidates {
				base := m.dir
				if f.Scope == "archive" {
					base = m.archive
				} else if f.Scope != "root" {
					t.Fatal("unknown scope")
				}
				key := f.Scope + "/" + f.Name
				data, e := os.ReadFile(filepath.Join(base, f.Name))
				if seen[key] || e != nil || markerDigest(data) != f.Hash || f.Name == "runtime.lock" || f.Scope == "root" && (f.Name == "mutation-inflight.json" || f.Name == runtimeRecoveryReleaseIntentFile) {
					t.Fatal("unsafe cleanup candidate")
				}
				seen[key] = true
			}
			for _, f := range p.RootBarriers {
				data, e := os.ReadFile(filepath.Join(m.dir, f.Name))
				if e != nil || markerDigest(data) != f.Hash {
					t.Fatal("unbound barrier")
				}
			}
			return nil
		}); err != nil {
			t.Fatal(err)
		}
	}
	first.Candidates[0] = RuntimeRecoverySourceCleanupFile{"root", "runtime.lock", "caller-edited"}
	first.RootBarriers[0].Hash = "caller-edited"
	if err := m.WithPreparedReleaseSubmission(context.Background(), b, m.authorizationHash, func(g *RuntimeRecoveryPreparedReleaseGuard) error {
		p, e := g.PlanArchivedSourceCleanup(context.Background())
		if e != nil {
			return e
		}
		if p.Candidates[0].Name == "runtime.lock" || p.RootBarriers[0].Hash == "caller-edited" {
			t.Error("caller changed owned plan")
		}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	copyGuard := *escaped
	for _, g := range []*RuntimeRecoveryPreparedReleaseGuard{nil, escaped, &copyGuard} {
		p, e := g.PlanArchivedSourceCleanup(context.Background())
		if e != ErrRuntimeMutationBlocked || !reflect.DeepEqual(p, RuntimeRecoverySourceCleanupPlan{}) {
			t.Fatal("escaped plan", e)
		}
	}
	m.Close()
	if lease, err := BeginRuntimeMutation(m.dir, m.authorization.OperationID, TaskKindVPNCoreService); err != ErrRuntimeMutationBlocked {
		if lease != nil {
			lease.Close()
		}
		t.Fatal("read-only plan released fence")
	}
	assertTerminalHistoryTree(t, m.dir, before)
	assertTerminalHistoryTree(t, path, backup)
}

func TestArchivedSourcePlanRefusals(t *testing.T) {
	for _, mode := range []string{"missing_archive", "unowned_archive", "source_missing", "foreign_source", "source_inode", "copy_inode", "manifest_inode", "copy_missing", "runtime", "expired", "backwards", "cancelled", "checkpoint_error", "during_source", "during_copy", "during_manifest", "during_runtime", "during_expiry", "during_cancel"} {
		t.Run(mode, func(t *testing.T) {
			_, a, b, m := recoveryHistoryFixture(t)
			path, _ := recoveryHistoryArchivePath(m.dir, m.authorization.OperationID)
			if mode != "missing_archive" {
				archiveHistory(t, b, m)
			}
			switch mode {
			case "unowned_archive":
				m.historyArchive = nil
				m.historyArchiveUnchanged = nil
			case "source_missing":
				os.Remove(filepath.Join(m.dir, runtimeRecoveryRetentionSubmissionFile))
			case "foreign_source":
				os.WriteFile(filepath.Join(m.dir, "foreign"), []byte("private evidence"), 0600)
			case "source_inode":
				replaceHistoryFile(t, filepath.Join(m.dir, runtimeRecoveryRetentionSubmissionFile))
			case "copy_inode":
				replaceHistoryFile(t, filepath.Join(path, "history", runtimeRecoveryRetentionSubmissionFile))
			case "manifest_inode":
				replaceHistoryFile(t, filepath.Join(path, recoveryHistoryArchiveManifest))
			case "copy_missing":
				os.Remove(filepath.Join(path, "history", runtimeRecoveryRetentionSubmissionFile))
			case "runtime":
				a.generation.PID++
			case "expired":
				m.now = func() time.Time { return m.authorization.ExpiresAt }
			case "backwards":
				m.now = func() time.Time { return m.lastChecked.Add(-time.Second) }
			}
			before := terminalHistoryTree(t, m.dir)
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			if mode == "cancelled" {
				cancel()
			}
			var result RuntimeRecoverySourceCleanupPlan
			err := m.WithPreparedReleaseSubmission(ctx, b, m.authorizationHash, func(g *RuntimeRecoveryPreparedReleaseGuard) error {
				var err error
				result, err = g.planArchivedSourceCleanup(ctx, func() error {
					switch mode {
					case "checkpoint_error":
						return errors.New("private failure details")
					case "during_source":
						replaceHistoryFile(t, filepath.Join(m.dir, runtimeRecoveryRetentionSubmissionFile))
					case "during_copy":
						replaceHistoryFile(t, filepath.Join(path, "history", runtimeRecoveryRetentionSubmissionFile))
					case "during_manifest":
						replaceHistoryFile(t, filepath.Join(path, recoveryHistoryArchiveManifest))
					case "during_runtime":
						a.generation.PID++
					case "during_expiry":
						m.now = func() time.Time { return m.authorization.ExpiresAt }
					case "during_cancel":
						cancel()
					}
					return nil
				})
				return err
			})
			if err != ErrRuntimeMutationBlocked || !reflect.DeepEqual(result, RuntimeRecoverySourceCleanupPlan{}) {
				t.Fatal("unsafe plan", err)
			}
			assertTerminalHistoryTree(t, m.dir, before)
			m.Close()
			if lease, err := BeginRuntimeMutation(m.dir, m.authorization.OperationID, TaskKindVPNCoreService); err != ErrRuntimeMutationBlocked {
				if lease != nil {
					lease.Close()
				}
				t.Fatal("refusal released fence")
			}
		})
	}
}
