package tasks

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func releaseRetentionFixture(t *testing.T) (CredentialRemovalExecutor, *removalAdapter, RuntimeRecoveryGrantBinding, *RuntimeRecoveryFinalization) {
	t.Helper()
	e, a, b, _, m := preparedSubmissionFixture(t)
	if err := m.WithPreparedReleaseSubmission(context.Background(), b, m.authorizationHash, func(g *RuntimeRecoveryPreparedReleaseGuard) error {
		p, err := g.PinProof(context.Background())
		if err != nil {
			return err
		}
		return g.RecordReceipt(context.Background(), receiptForPreparedProof(p))
	}); err != nil {
		m.Close()
		t.Fatal(err)
	}
	return e, a, b, m
}

func TestReleaseRetentionDurableReadOnly(t *testing.T) {
	e, _, b, m := releaseRetentionFixture(t)
	defer m.Close()
	before := terminalHistoryTree(t, m.dir)
	var hash string
	var escaped *RuntimeRecoveryPreparedReleaseGuard
	var frozen []byte
	for range 2 {
		if err := m.WithPreparedReleaseSubmission(context.Background(), b, m.authorizationHash, func(g *RuntimeRecoveryPreparedReleaseGuard) error {
			escaped = g
			got, err := g.RetainHistory(context.Background())
			if err != nil {
				return err
			}
			if hash != "" && hash != got {
				t.Error("retry changed retention")
			}
			hash = got
			if _, err := g.AcknowledgedProof(context.Background()); err != nil {
				return err
			}
			return nil
		}); err != nil {
			t.Fatal(err)
		}
		data, err := readTerminalConfirmationFile(filepath.Join(m.dir, runtimeRecoveryReleaseRetentionFile), runtimeRecoveryReleaseRetentionLimit)
		if err != nil {
			t.Fatal(err)
		}
		if frozen != nil && !bytes.Equal(frozen, data) {
			t.Fatal("retry renewed retention")
		}
		frozen = data
	}
	var r runtimeRecoveryReleaseRetention
	if !exactHistoricalJSON(frozen, &r) || hash != markerDigest(frozen) || r.Binding != b || r.OperationID != m.authorization.OperationID {
		t.Fatal("retention binding")
	}
	seen := map[string]bool{}
	for _, f := range r.Files {
		path := f.Name
		if f.Scope == "archive" {
			path = filepath.Join(filepath.Base(m.archive), f.Name)
		} else if f.Scope != "root" {
			t.Fatal("foreign scope")
		}
		if seen[path] || !bytes.Equal(before[path], f.Data) || f.Hash != markerDigest(f.Data) {
			t.Fatal("incomplete/changed audit copy", path)
		}
		seen[path] = true
	}
	for path := range before {
		if strings.HasSuffix(path, "/") || path == "runtime.lock" {
			continue
		}
		if !seen[path] {
			t.Fatal("lost retained file", path)
		}
	}
	before[runtimeRecoveryReleaseRetentionFile] = frozen
	assertTerminalHistoryTree(t, m.dir, before)
	if h, err := escaped.RetainHistory(context.Background()); err != ErrRuntimeMutationBlocked || h != "" {
		t.Fatal("escaped retention")
	}
	m.Close()
	if err := os.Remove(e.activePath); err != nil {
		t.Fatal(err)
	}
	if err := WithHistoricalRecoveryPreparedSubmission(context.Background(), m.dir, b, func(h RuntimeRecoveryPreparedSubmissionHistory) error {
		if h.RetentionHash != hash || h.Proof != m.preparedFrozen {
			t.Error("retained history changed")
		}
		return nil
	}); err != nil {
		t.Fatal("retained history unreadable", err)
	}
	assertTerminalHistoryTree(t, m.dir, before)
	if l, err := BeginRuntimeMutation(m.dir, resultTaskID, TaskKindVPNCoreService); err != ErrRuntimeMutationBlocked {
		if l != nil {
			l.Close()
		}
		t.Fatal("retention released fence")
	}
}

func TestReleaseRetentionFailures(t *testing.T) {
	for _, mode := range []string{"missing_ack", "cancelled", "expired", "runtime", "foreign_existing", "identical_existing", "checkpoint_error", "post_write_cancel", "post_write_expiry", "post_write_runtime", "post_write_source_replace"} {
		t.Run(mode, func(t *testing.T) {
			_, a, b, m := releaseRetentionFixture(t)
			defer m.Close()
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			path := filepath.Join(m.dir, runtimeRecoveryReleaseRetentionFile)
			switch mode {
			case "missing_ack":
				os.Remove(filepath.Join(m.dir, runtimeRecoveryPreparedAcknowledgementFile))
			case "cancelled":
				cancel()
			case "expired":
				m.now = func() time.Time { return m.authorization.ExpiresAt }
			case "runtime":
				a.generation.PID++
			}
			called := false
			err := m.WithPreparedReleaseSubmission(ctx, b, m.authorizationHash, func(g *RuntimeRecoveryPreparedReleaseGuard) error {
				called = true
				if mode == "foreign_existing" || mode == "identical_existing" {
					data := []byte("foreign private payload")
					if mode == "identical_existing" {
						var err error
						data, _, err = buildReleaseHistoryRetention(m.dir, m.archive, b, m.preparedFrozen, m.lastChecked)
						if err != nil {
							t.Fatal(err)
						}
					}
					if err := os.WriteFile(path, data, 0600); err != nil {
						t.Fatal(err)
					}
				}
				hash, err := g.retainHistory(ctx, func() error {
					switch mode {
					case "checkpoint_error":
						return errors.New("private receipt payload")
					case "post_write_cancel":
						cancel()
					case "post_write_expiry":
						m.now = func() time.Time { return m.authorization.ExpiresAt }
					case "post_write_runtime":
						a.generation.PID++
					case "post_write_source_replace":
						p := filepath.Join(m.archive, runtimeRecoveryFinalSubmissionFile)
						data, e := os.ReadFile(p)
						if e != nil {
							t.Fatal(e)
						}
						tmp := filepath.Join(m.archive, ".replacement")
						if e := os.WriteFile(tmp, data, 0600); e != nil {
							t.Fatal(e)
						}
						if e := os.Rename(tmp, p); e != nil {
							t.Fatal(e)
						}
					}
					return nil
				})
				if hash != "" {
					t.Error("failure leaked success hash")
				}
				return err
			})
			if err != ErrRuntimeMutationBlocked {
				t.Fatal("unsafe retention", err)
			}
			if strings.HasPrefix(mode, "post_write") || mode == "checkpoint_error" {
				if !called || runtimeMarkerAbsent(path) {
					t.Fatal("lost durable retention after uncertainty")
				}
			}
			if runtimeMarkerAbsent(filepath.Join(m.dir, "mutation-inflight.json")) || runtimeMarkerAbsent(filepath.Join(m.dir, runtimeRecoveryReleaseIntentFile)) {
				t.Fatal("retention failure lost fence")
			}
		})
	}
}

func TestReleaseRetentionSourceChangesDuringObservation(t *testing.T) {
	_, a, b, m := releaseRetentionFixture(t)
	defer m.Close()
	if err := m.WithPreparedReleaseSubmission(context.Background(), b, m.authorizationHash, func(g *RuntimeRecoveryPreparedReleaseGuard) error {
		_, err := g.RetainHistory(context.Background())
		return err
	}); err != nil {
		t.Fatal(err)
	}
	before := terminalHistoryTree(t, m.dir)
	changed := false
	a.healthHook = func() error {
		if !changed {
			changed = true
			path := filepath.Join(m.archive, runtimeRecoveryFinalSubmissionFile)
			data, err := os.ReadFile(path)
			if err != nil {
				t.Fatal(err)
			}
			tmp := filepath.Join(m.archive, ".replacement")
			if err := os.WriteFile(tmp, data, 0600); err != nil {
				t.Fatal(err)
			}
			if err := os.Rename(tmp, path); err != nil {
				t.Fatal(err)
			}
		}
		return nil
	}
	if err := m.WithPreparedReleaseSubmission(context.Background(), b, m.authorizationHash, func(*RuntimeRecoveryPreparedReleaseGuard) error {
		t.Error("changed source reached callback")
		return nil
	}); err != ErrRuntimeMutationBlocked {
		t.Fatal("source replacement accepted", err)
	}
	if !changed {
		t.Fatal("observation hook missed")
	}
	assertTerminalHistoryTree(t, m.dir, before)
}

func TestReleaseRetentionFinalDeadline(t *testing.T) {
	for _, mode := range []string{"expiry", "cancelled"} {
		t.Run(mode, func(t *testing.T) {
			_, _, b, m := releaseRetentionFixture(t)
			defer m.Close()
			if err := m.WithPreparedReleaseSubmission(context.Background(), b, m.authorizationHash, func(g *RuntimeRecoveryPreparedReleaseGuard) error {
				_, err := g.RetainHistory(context.Background())
				return err
			}); err != nil {
				t.Fatal(err)
			}
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			original := m.retentionUnchanged
			calls := 0
			m.retentionUnchanged = func() bool {
				calls++
				ok := original()
				if calls == 2 {
					if mode == "expiry" {
						m.now = func() time.Time { return m.authorization.ExpiresAt }
					} else {
						cancel()
					}
				}
				return ok
			}
			before := terminalHistoryTree(t, m.dir)
			if err := m.WithPreparedReleaseSubmission(ctx, b, m.authorizationHash, func(*RuntimeRecoveryPreparedReleaseGuard) error {
				t.Error("expired/cancelled final validation reached callback")
				return nil
			}); err != ErrRuntimeMutationBlocked {
				t.Fatal("late deadline accepted", err)
			}
			if calls < 2 {
				t.Fatal("missed final validation")
			}
			assertTerminalHistoryTree(t, m.dir, before)
		})
	}
}

func TestReleaseRetentionHistoricalRefusals(t *testing.T) {
	for _, mode := range []string{"schema", "purpose", "operation", "binding", "authorization", "proof", "time_before", "time_expiry", "missing_file", "extra_file", "name", "scope", "hash", "payload", "order", "duplicate_key", "mode", "hardlink", "symlink", "unknown", "replace_during_read", "source_replace_during_read"} {
		t.Run(mode, func(t *testing.T) {
			_, _, b, m := releaseRetentionFixture(t)
			if err := m.WithPreparedReleaseSubmission(context.Background(), b, m.authorizationHash, func(g *RuntimeRecoveryPreparedReleaseGuard) error {
				_, err := g.RetainHistory(context.Background())
				return err
			}); err != nil {
				m.Close()
				t.Fatal(err)
			}
			m.Close()
			path := filepath.Join(m.dir, runtimeRecoveryReleaseRetentionFile)
			original, err := os.ReadFile(path)
			if err != nil {
				t.Fatal(err)
			}
			var r runtimeRecoveryReleaseRetention
			if !exactHistoricalJSON(original, &r) {
				t.Fatal("fixture")
			}
			changed := true
			switch mode {
			case "schema":
				r.SchemaVersion++
			case "purpose":
				r.Purpose = "released"
			case "operation":
				r.OperationID = "ffffffff-ffff-4fff-8fff-ffffffffffff"
			case "binding":
				r.Binding.AgentCredentialGeneration++
			case "authorization":
				r.AuthorizationHash = r.PreparedProofHash
			case "proof":
				r.PreparedProofHash = r.AuthorizationHash
			case "time_before":
				r.RetainedAt = m.preparedFrozen.ObservedAt.Add(-time.Nanosecond)
			case "time_expiry":
				r.RetainedAt = m.authorization.ExpiresAt
			case "missing_file":
				r.Files = r.Files[1:]
			case "extra_file":
				r.Files = append(r.Files, r.Files[0])
			case "name":
				r.Files[0].Name = "../foreign"
			case "scope":
				r.Files[0].Scope = "foreign"
			case "hash":
				r.Files[0].Hash = r.PreparedProofHash
			case "payload":
				r.Files[0].Data = []byte("private changed bytes")
			case "order":
				r.Files[0], r.Files[1] = r.Files[1], r.Files[0]
			default:
				changed = false
			}
			if changed {
				data, _ := json.Marshal(r)
				if err := os.WriteFile(path, data, 0600); err != nil {
					t.Fatal(err)
				}
			}
			switch mode {
			case "duplicate_key":
				os.WriteFile(path, append([]byte(`{"schemaVersion":1,`), original[1:]...), 0600)
			case "mode":
				os.Chmod(path, 0644)
			case "hardlink":
				if err := os.Link(path, filepath.Join(t.TempDir(), "alias")); err != nil {
					t.Fatal(err)
				}
			case "symlink":
				os.Remove(path)
				if err := os.Symlink(filepath.Join(m.dir, runtimeRecoveryPreparedSubmissionFile), path); err != nil {
					t.Fatal(err)
				}
			case "unknown":
				os.WriteFile(filepath.Join(m.dir, "foreign.json"), []byte("{}"), 0600)
			}
			before := terminalHistoryTree(t, m.dir)
			called := false
			err = WithHistoricalRecoveryPreparedSubmission(context.Background(), m.dir, b, func(RuntimeRecoveryPreparedSubmissionHistory) error {
				called = true
				target := path
				if mode == "source_replace_during_read" {
					target = filepath.Join(m.archive, runtimeRecoveryFinalSubmissionFile)
				} else if mode != "replace_during_read" {
					t.Error("invalid retention reached callback")
					return nil
				}
				data, e := os.ReadFile(target)
				if e != nil {
					t.Fatal(e)
				}
				tmp := filepath.Join(filepath.Dir(target), ".replacement")
				if e := os.WriteFile(tmp, data, 0600); e != nil {
					t.Fatal(e)
				}
				if e := os.Rename(tmp, target); e != nil {
					t.Fatal(e)
				}
				return nil
			})
			if err != ErrRuntimeMutationBlocked {
				t.Fatal("corrupt retention accepted", err)
			}
			if called != (mode == "replace_during_read" || mode == "source_replace_during_read") {
				t.Fatal("wrong callback boundary")
			}
			assertTerminalHistoryTree(t, m.dir, before)
		})
	}
}
