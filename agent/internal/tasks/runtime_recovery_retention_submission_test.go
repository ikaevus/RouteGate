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

func retentionSubmissionFixture(t *testing.T) (CredentialRemovalExecutor, *removalAdapter, RuntimeRecoveryGrantBinding, *RuntimeRecoveryFinalization) {
	t.Helper()
	e, a, b, m := releaseRetentionFixture(t)
	if err := m.WithPreparedReleaseSubmission(context.Background(), b, m.authorizationHash, func(g *RuntimeRecoveryPreparedReleaseGuard) error {
		_, e := g.RetainHistory(context.Background())
		return e
	}); err != nil {
		m.Close()
		t.Fatal(err)
	}
	t.Cleanup(func() { m.Close() })
	return e, a, b, m
}

func receiptForRetentionProof(p RuntimeRecoveryRetentionProof) RuntimeRecoveryRetentionReceipt {
	wire, _ := json.Marshal(p)
	return RuntimeRecoveryRetentionReceipt{1, true, p.OperationID, p.RetentionHash, markerDigest(wire), markerDigest(wire)}
}

func TestRetentionSubmissionDurability(t *testing.T) {
	e, _, b, m := retentionSubmissionFixture(t)
	before := terminalHistoryTree(t, m.dir)
	var frozen RuntimeRecoveryRetentionProof
	var escaped *RuntimeRecoveryPreparedReleaseGuard
	for range 2 {
		if err := m.WithPreparedReleaseSubmission(context.Background(), b, m.authorizationHash, func(g *RuntimeRecoveryPreparedReleaseGuard) error {
			escaped = g
			p, e := g.PinRetentionProof(context.Background())
			if e != nil {
				return e
			}
			if frozen.SchemaVersion != 0 && p != frozen {
				t.Error("retry renewed proof")
			}
			frozen = p
			data, e := readTerminalConfirmationFile(filepath.Join(m.dir, runtimeRecoveryRetentionSubmissionFile), 8192)
			if e != nil || !bytes.Equal(data, m.retentionSubmission) {
				t.Fatal("pin not durable", e)
			}
			return g.RecordRetentionReceipt(context.Background(), receiptForRetentionProof(p))
		}); err != nil {
			t.Fatal(err)
		}
	}
	before[runtimeRecoveryRetentionSubmissionFile] = m.retentionSubmission
	before[runtimeRecoveryRetentionAcknowledgementFile] = m.retentionReceipt
	assertTerminalHistoryTree(t, m.dir, before)
	if p, e := escaped.RetentionProof(context.Background()); e != ErrRuntimeMutationBlocked || p != (RuntimeRecoveryRetentionProof{}) {
		t.Fatal("escaped proof")
	}
	copyGuard := *escaped
	if e := copyGuard.RecordRetentionReceipt(context.Background(), receiptForRetentionProof(frozen)); e != ErrRuntimeMutationBlocked {
		t.Fatal("copied guard retained authority")
	}
	m.Close()
	if err := os.Remove(e.activePath); err != nil {
		t.Fatal(err)
	}
	if err := WithHistoricalRecoveryRetention(context.Background(), m.dir, b, func(h RuntimeRecoveryRetentionHistory) error {
		if h.Proof != frozen || h.LocalReceipt != receiptForRetentionProof(frozen) {
			t.Error("history drift")
		}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	if err := WithHistoricalRecoveryPreparedSubmission(context.Background(), m.dir, b, func(RuntimeRecoveryPreparedSubmissionHistory) error {
		t.Error("old reader adopted newer phase")
		return nil
	}); err != ErrRuntimeMutationBlocked {
		t.Fatal("old reader", err)
	}
	if l, err := BeginRuntimeMutation(m.dir, frozen.Observation.TaskID, TaskKindVPNCoreService); err != ErrRuntimeMutationBlocked {
		if l != nil {
			l.Close()
		}
		t.Fatal("ack released fence")
	}
	assertTerminalHistoryTree(t, m.dir, before)
}

func TestRetentionSubmissionFailures(t *testing.T) {
	for _, mode := range []string{"missing_package", "cancelled", "expired", "backwards", "runtime", "existing_pin", "existing_ack", "pin_checkpoint", "ack_checkpoint", "post_pin_cancel", "post_ack_expiry", "post_pin_source", "wrong_receipt"} {
		t.Run(mode, func(t *testing.T) {
			_, a, b, m := retentionSubmissionFixture(t)
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			if mode == "missing_package" {
				os.Remove(filepath.Join(m.dir, runtimeRecoveryReleaseRetentionFile))
			}
			if mode == "cancelled" {
				cancel()
			}
			if mode == "expired" {
				m.now = func() time.Time { return m.authorization.ExpiresAt }
			}
			if mode == "backwards" {
				m.now = func() time.Time { return m.lastChecked.Add(-time.Second) }
			}
			if mode == "runtime" {
				a.generation.PID++
			}
			if mode == "existing_pin" {
				os.WriteFile(filepath.Join(m.dir, runtimeRecoveryRetentionSubmissionFile), []byte("{}"), 0600)
			}
			if mode == "existing_ack" {
				os.WriteFile(filepath.Join(m.dir, runtimeRecoveryRetentionAcknowledgementFile), []byte("{}"), 0600)
			}
			err := m.WithPreparedReleaseSubmission(ctx, b, m.authorizationHash, func(g *RuntimeRecoveryPreparedReleaseGuard) error {
				p, err := g.pinRetentionProof(ctx, func() error {
					switch mode {
					case "pin_checkpoint":
						return errors.New("private checkpoint")
					case "post_pin_cancel":
						cancel()
					case "post_pin_source":
						path := filepath.Join(m.archive, runtimeRecoveryFinalSubmissionFile)
						data, _ := os.ReadFile(path)
						tmp := filepath.Join(m.archive, ".replace")
						os.WriteFile(tmp, data, 0600)
						os.Rename(tmp, path)
					}
					return nil
				})
				if err != nil {
					return err
				}
				r := receiptForRetentionProof(p)
				if mode == "wrong_receipt" {
					r.RetentionHash = strings.Repeat("0", 64)
				}
				return g.recordRetentionReceipt(ctx, r, func() error {
					if mode == "ack_checkpoint" {
						return errors.New("private ack checkpoint")
					}
					if mode == "post_ack_expiry" {
						m.now = func() time.Time { return m.authorization.ExpiresAt }
					}
					return nil
				})
			})
			if err != ErrRuntimeMutationBlocked {
				t.Fatal("unsafe retention submission", err)
			}
			if runtimeMarkerAbsent(filepath.Join(m.dir, "mutation-inflight.json")) || runtimeMarkerAbsent(filepath.Join(m.dir, runtimeRecoveryReleaseIntentFile)) {
				t.Fatal("failure removed barriers")
			}
			if mode == "pin_checkpoint" || mode == "post_pin_cancel" || mode == "post_pin_source" || mode == "ack_checkpoint" || mode == "post_ack_expiry" {
				if !m.poisoned || runtimeMarkerAbsent(filepath.Join(m.dir, runtimeRecoveryRetentionSubmissionFile)) {
					t.Fatal("publication uncertainty lost pin or owner poison")
				}
			}
		})
	}
}

func TestRetentionSubmissionHistoricalRefusals(t *testing.T) {
	for _, mode := range []string{"operation", "generation", "authorization", "prepared_proof", "prepared_receipt", "package_hash", "captured", "observed_before", "observed_expired", "purpose", "schema", "duplicate", "unknown", "symlink", "hardlink", "mode", "receipt", "missing_source", "missing_package", "foreign_root", "replace_pin", "replace_ack", "replace_source", "cancelled"} {
		t.Run(mode, func(t *testing.T) {
			_, _, b, m := retentionSubmissionFixture(t)
			if err := m.WithPreparedReleaseSubmission(context.Background(), b, m.authorizationHash, func(g *RuntimeRecoveryPreparedReleaseGuard) error {
				p, e := g.PinRetentionProof(context.Background())
				if e != nil {
					return e
				}
				return g.RecordRetentionReceipt(context.Background(), receiptForRetentionProof(p))
			}); err != nil {
				t.Fatal(err)
			}
			m.Close()
			p := m.retentionFrozen
			switch mode {
			case "operation":
				p.OperationID = p.Observation.AgentID
			case "generation":
				p.AgentCredentialGeneration++
			case "authorization":
				p.AuthorizationHash = strings.Repeat("0", 64)
			case "prepared_proof":
				p.PreparedProofHash = strings.Repeat("0", 64)
			case "prepared_receipt":
				p.PreparedReceiptHash = strings.Repeat("0", 64)
			case "package_hash":
				p.RetentionHash = strings.Repeat("0", 64)
			case "captured":
				p.RetainedAt = p.RetainedAt.Add(time.Nanosecond)
			case "observed_before":
				p.ObservedAt = p.RetainedAt.Add(-time.Nanosecond)
			case "observed_expired":
				p.ObservedAt = m.authorization.ExpiresAt
			case "purpose":
				p.Purpose = "released"
			case "schema":
				p.SchemaVersion++
			}
			wire, _ := json.Marshal(p)
			if mode == "duplicate" {
				wire = append([]byte(`{"schemaVersion":1,`), wire[1:]...)
			}
			if mode == "unknown" {
				wire = append([]byte(`{"private":true,`), wire[1:]...)
			}
			path := filepath.Join(m.dir, runtimeRecoveryRetentionSubmissionFile)
			if err := os.WriteFile(path, wire, 0600); err != nil {
				t.Fatal(err)
			}
			ack, _ := json.Marshal(receiptForRetentionProof(p))
			if mode == "receipt" {
				ack = []byte("{}")
			}
			os.WriteFile(filepath.Join(m.dir, runtimeRecoveryRetentionAcknowledgementFile), ack, 0600)
			if mode == "mode" {
				os.Chmod(path, 0644)
			}
			if mode == "hardlink" {
				os.Link(path, filepath.Join(t.TempDir(), "linked"))
			}
			if mode == "symlink" {
				os.Remove(path)
				outside := filepath.Join(t.TempDir(), "outside")
				os.WriteFile(outside, wire, 0600)
				os.Symlink(outside, path)
			}
			if mode == "missing_source" {
				os.Remove(filepath.Join(m.archive, runtimeRecoveryFinalSubmissionFile))
			}
			if mode == "missing_package" {
				os.Remove(filepath.Join(m.dir, runtimeRecoveryReleaseRetentionFile))
			}
			if mode == "foreign_root" {
				os.WriteFile(filepath.Join(m.dir, "foreign"), []byte("private"), 0600)
			}
			before := terminalHistoryTree(t, m.dir)
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			called := false
			err := WithHistoricalRecoveryRetention(ctx, m.dir, b, func(RuntimeRecoveryRetentionHistory) error {
				called = true
				if mode == "cancelled" {
					cancel()
				}
				var replace string
				switch mode {
				case "replace_pin":
					replace = path
				case "replace_ack":
					replace = filepath.Join(m.dir, runtimeRecoveryRetentionAcknowledgementFile)
				case "replace_source":
					replace = filepath.Join(m.archive, runtimeRecoveryFinalSubmissionFile)
				}
				if replace != "" {
					data, _ := os.ReadFile(replace)
					tmp := filepath.Join(filepath.Dir(replace), ".replace")
					os.WriteFile(tmp, data, 0600)
					os.Rename(tmp, replace)
				}
				return nil
			})
			if err != ErrRuntimeMutationBlocked {
				t.Fatal("unsafe historical proof", err)
			}
			if called != (strings.HasPrefix(mode, "replace_") || mode == "cancelled") {
				t.Fatal("invalid history reached callback", mode)
			}
			assertTerminalHistoryTree(t, m.dir, before)
		})
	}
}
