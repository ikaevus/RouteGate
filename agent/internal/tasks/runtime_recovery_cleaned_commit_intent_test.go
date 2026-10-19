package tasks

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func cleanedCommitFixture(t *testing.T) (CredentialRemovalExecutor, *removalAdapter, RuntimeRecoveryGrantBinding, *RuntimeRecoveryFinalization, string) {
	t.Helper()
	e, a, b, m, path := sourceCleanupFixture(t)
	if err := m.WithSourceCleanupSubmission(context.Background(), b, m.authorizationHash, func(g *RuntimeRecoveryPreparedReleaseGuard) error {
		_, err := g.CleanupArchivedSources(context.Background())
		return err
	}); err != nil {
		t.Fatal(err)
	}
	return e, a, b, m, path
}

func prepareCleanedCommit(t *testing.T, b RuntimeRecoveryGrantBinding, m *RuntimeRecoveryFinalization) RuntimeRecoveryCleanedCommitIntent {
	t.Helper()
	var p RuntimeRecoveryCleanedCommitIntent
	if err := m.WithSourceCleanupSubmission(context.Background(), b, m.authorizationHash, func(g *RuntimeRecoveryPreparedReleaseGuard) error {
		var err error
		p, err = g.PrepareCleanedCommit(context.Background())
		return err
	}); err != nil {
		t.Fatal(err)
	}
	return p
}

func TestCleanedCommitIntentComplete(t *testing.T) {
	e, _, b, m, path := cleanedCommitFixture(t)
	before, backup := terminalHistoryTree(t, m.dir), terminalHistoryTree(t, path)
	first := prepareCleanedCommit(t, b, m)
	wire, _ := json.Marshal(first)
	if first.SchemaVersion != 1 || first.Purpose != runtimeRecoveryCleanedCommitIntentPurpose || first.Binding != b || first.OperationID != m.authorization.OperationID || first.AuthorizationHash != m.authorizationHash || first.ManifestHash != markerDigest(m.historyArchive) || first.RetentionProofHash != markerDigest(m.retentionSubmission) || first.RetentionReceiptHash != markerDigest(m.retentionReceipt) || first.MarkerHash != markerDigest(before["mutation-inflight.json"]) || first.ReleaseIntentHash != markerDigest(before[runtimeRecoveryReleaseIntentFile]) || first.Observation != m.authorization.Expected || first.ObservedAt.Before(m.retentionFrozen.ObservedAt) || !first.ObservedAt.Before(first.ExpiresAt) {
		t.Fatal("unbound cleaned commitment")
	}
	for range 2 {
		if prepareCleanedCommit(t, b, m) != first {
			t.Fatal("retry renewed intent")
		}
	}
	before[runtimeRecoveryCleanedCommitIntentFile] = wire
	assertTerminalHistoryTree(t, m.dir, before)
	assertTerminalHistoryTree(t, path, backup)
	var escaped *RuntimeRecoveryPreparedReleaseGuard
	if err := m.WithSourceCleanupSubmission(context.Background(), b, m.authorizationHash, func(g *RuntimeRecoveryPreparedReleaseGuard) error { escaped = g; return nil }); err != nil {
		t.Fatal(err)
	}
	copyGuard := *escaped
	for _, g := range []*RuntimeRecoveryPreparedReleaseGuard{nil, escaped, &copyGuard} {
		if p, err := g.PrepareCleanedCommit(context.Background()); err != ErrRuntimeMutationBlocked || p != (RuntimeRecoveryCleanedCommitIntent{}) {
			t.Fatal("escaped intent", err)
		}
	}
	copyOwner := &RuntimeRecoveryFinalization{self: m.self, binding: m.binding, authorizationHash: m.authorizationHash, sourceCleanup: m.sourceCleanup, cleanedCommitIntent: m.cleanedCommitIntent}
	if err := copyOwner.WithSourceCleanupSubmission(context.Background(), b, m.authorizationHash, func(*RuntimeRecoveryPreparedReleaseGuard) error { t.Error("copied owner adopted intent"); return nil }); err != ErrRuntimeMutationBlocked {
		t.Fatal("copied owner", err)
	}
	m.Close()
	os.Remove(e.activePath)
	if err := WithRecoveryCleanedCommitHistory(context.Background(), m.dir, first.OperationID, b, func(p RuntimeRecoveryCleanedCommitIntent, h RuntimeRecoveryRetentionHistory) error {
		if p != first || !h.LocalReceipt.Recorded {
			t.Error("history drift")
		}
		return nil
	}); err != nil {
		t.Fatal("historical intent", err)
	}
	if err := WithRecoverySourceCleanupHistory(context.Background(), m.dir, first.OperationID, b, func(s RuntimeRecoverySourceCleanupStatus, _ RuntimeRecoveryRetentionHistory) error {
		if !s.SourcesRemoved {
			t.Error("lost cleanup status")
		}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	if lease, err := BeginRuntimeMutation(m.dir, resultTaskID, TaskKindVPNCoreService); err != ErrRuntimeMutationBlocked {
		if lease != nil {
			lease.Close()
		}
		t.Fatal("intent released fence")
	}
	assertTerminalHistoryTree(t, m.dir, before)
	assertTerminalHistoryTree(t, path, backup)
}

func TestCleanedCommitIntentFailures(t *testing.T) {
	for _, mode := range []string{"before", "file", "linked", "unlinked-temp", "durable", "published", "done", "runtime", "cancel", "expiry", "backward", "foreign", "existing", "copy", "barrier", "published_substitution", "published_runtime", "publication_race"} {
		t.Run(mode, func(t *testing.T) {
			_, a, b, m, path := cleanedCommitFixture(t)
			backup := terminalHistoryTree(t, path)
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			var result RuntimeRecoveryCleanedCommitIntent
			err := m.WithSourceCleanupSubmission(ctx, b, m.authorizationHash, func(g *RuntimeRecoveryPreparedReleaseGuard) error {
				var err error
				result, err = g.prepareCleanedCommit(ctx, func(step string) error {
					if step == mode {
						return errors.New("private fault")
					}
					if step == "published" && mode == "published_substitution" {
						replaceHistoryFile(t, filepath.Join(m.dir, runtimeRecoveryCleanedCommitIntentFile))
					}
					if step == "published" && mode == "published_runtime" {
						a.generation.PID++
					}
					if step == "file" && mode == "publication_race" {
						entries, _ := os.ReadDir(m.dir)
						for _, entry := range entries {
							if len(entry.Name()) > 9 && entry.Name()[:9] == ".removal-" {
								data, e := os.ReadFile(filepath.Join(m.dir, entry.Name()))
								if e != nil {
									t.Fatal(e)
								}
								if e = os.WriteFile(filepath.Join(m.dir, runtimeRecoveryCleanedCommitIntentFile), data, 0600); e != nil {
									t.Fatal(e)
								}
							}
						}
					}
					if step != "before" {
						return nil
					}
					f := filepath.Join(m.dir, runtimeRecoveryCleanedCommitIntentFile)
					switch mode {
					case "runtime":
						a.generation.PID++
					case "cancel":
						cancel()
					case "expiry":
						m.now = func() time.Time { return m.authorization.ExpiresAt }
					case "backward":
						m.now = func() time.Time { return m.lastChecked.Add(-time.Second) }
					case "foreign":
						os.WriteFile(filepath.Join(m.dir, "foreign"), []byte("private"), 0600)
					case "existing":
						p, e := m.sourceCleanup.cleanedCommitIntent(m.lastChecked)
						if e != nil {
							t.Fatal(e)
						}
						data, _ := json.Marshal(p)
						os.WriteFile(f, data, 0600)
					case "copy":
						replaceHistoryFile(t, filepath.Join(path, "manifest.json"))
					case "barrier":
						replaceHistoryFile(t, filepath.Join(m.dir, "mutation-inflight.json"))
					}
					return nil
				})
				return err
			})
			if err != ErrRuntimeMutationBlocked || result != (RuntimeRecoveryCleanedCommitIntent{}) {
				t.Fatal("uncertainty accepted", err)
			}
			before := terminalHistoryTree(t, m.dir)
			if err := m.WithSourceCleanupSubmission(context.Background(), b, m.authorizationHash, func(*RuntimeRecoveryPreparedReleaseGuard) error { t.Error("resumed uncertain intent"); return nil }); err != ErrRuntimeMutationBlocked {
				t.Fatal(err)
			}
			m.Close()
			if runtimeMarkerAbsent(filepath.Join(m.dir, "mutation-inflight.json")) || runtimeMarkerAbsent(filepath.Join(m.dir, runtimeRecoveryReleaseIntentFile)) {
				t.Fatal("lost barriers")
			}
			assertTerminalHistoryTree(t, m.dir, before)
			if mode != "copy" {
				assertTerminalHistoryTree(t, path, backup)
			}
		})
	}
}

func TestCleanedCommitIntentBeforeCleanup(t *testing.T) {
	_, _, b, m, _ := sourceCleanupFixture(t)
	before := terminalHistoryTree(t, m.dir)
	if err := m.WithSourceCleanupSubmission(context.Background(), b, m.authorizationHash, func(g *RuntimeRecoveryPreparedReleaseGuard) error {
		if p, e := g.PrepareCleanedCommit(context.Background()); e != ErrRuntimeMutationBlocked || p != (RuntimeRecoveryCleanedCommitIntent{}) {
			t.Error("intent before cleanup", e)
		}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	assertTerminalHistoryTree(t, m.dir, before)
}

func TestCleanedCommitIntentHistoryRefusals(t *testing.T) {
	for _, mode := range []string{"missing", "hash", "binding", "observation", "expired_time", "early_time", "unknown", "unsafe", "linked", "inode", "cancel", "callback", "missing_barrier"} {
		t.Run(mode, func(t *testing.T) {
			_, _, b, m, _ := cleanedCommitFixture(t)
			p := prepareCleanedCommit(t, b, m)
			m.Close()
			path := filepath.Join(m.dir, runtimeRecoveryCleanedCommitIntentFile)
			switch mode {
			case "missing":
				os.Remove(path)
			case "hash":
				p.ManifestHash = p.AuthorizationHash
			case "binding":
				p.Binding.AgentCredentialGeneration++
			case "observation":
				p.Observation.Witness.Generation.PID++
			case "expired_time":
				p.ObservedAt = p.ExpiresAt
			case "early_time":
				p.ObservedAt = time.Time{}
			case "unsafe":
				os.Chmod(path, 0644)
			case "linked":
				os.Link(path, filepath.Join(filepath.Dir(m.dir), "foreign-link"))
			case "missing_barrier":
				os.Remove(filepath.Join(m.dir, "mutation-inflight.json"))
			}
			if mode == "hash" || mode == "binding" || mode == "observation" || mode == "expired_time" || mode == "early_time" || mode == "unknown" {
				data, _ := json.Marshal(p)
				if mode == "unknown" {
					data = append(data[:len(data)-1], []byte(`,"unknown":true}`)...)
				}
				os.WriteFile(path, data, 0600)
			}
			before := terminalHistoryTree(t, m.dir)
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			err := WithRecoveryCleanedCommitHistory(ctx, m.dir, p.OperationID, b, func(RuntimeRecoveryCleanedCommitIntent, RuntimeRecoveryRetentionHistory) error {
				switch mode {
				case "inode":
					replaceHistoryFile(t, path)
				case "cancel":
					cancel()
				case "callback":
					return errors.New("private callback")
				default:
					t.Error("invalid intent reached callback")
				}
				return nil
			})
			if err != ErrRuntimeMutationBlocked {
				t.Fatal("invalid history", err)
			}
			assertTerminalHistoryTree(t, m.dir, before)
		})
	}
}
