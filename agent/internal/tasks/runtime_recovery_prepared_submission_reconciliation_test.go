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

func preparedSubmissionHistoryFixture(t *testing.T, acknowledged bool) (CredentialRemovalExecutor, RuntimeRecoveryGrantBinding, *RuntimeRecoveryFinalization, RuntimeRecoveryPreparedReleaseProof) {
	t.Helper()
	e, _, b, _, m := preparedSubmissionFixture(t)
	var p RuntimeRecoveryPreparedReleaseProof
	if err := m.WithPreparedReleaseSubmission(context.Background(), b, m.authorizationHash, func(g *RuntimeRecoveryPreparedReleaseGuard) error {
		var err error
		p, err = g.PinProof(context.Background())
		if err != nil || !acknowledged {
			return err
		}
		return g.RecordReceipt(context.Background(), receiptForPreparedProof(p))
	}); err != nil {
		m.Close()
		t.Fatal(err)
	}
	m.Close()
	return e, b, m, p
}

func TestPreparedSubmissionHistoryReadOnly(t *testing.T) {
	for _, acknowledged := range []bool{false, true} {
		t.Run(map[bool]string{false: "pin_only", true: "acknowledged"}[acknowledged], func(t *testing.T) {
			e, b, m, p := preparedSubmissionHistoryFixture(t, acknowledged)
			if err := os.Remove(e.activePath); err != nil {
				t.Fatal(err)
			}
			before := terminalHistoryTree(t, m.dir)
			for range 2 {
				err := WithHistoricalRecoveryPreparedSubmission(context.Background(), m.dir, b, func(h RuntimeRecoveryPreparedSubmissionHistory) error {
					if h.Proof != p || (h.LocalReceipt.SchemaVersion != 0) != acknowledged {
						t.Error("historical commitment changed")
					}
					if acknowledged && h.LocalReceipt != receiptForPreparedProof(p) {
						t.Error("receipt changed")
					}
					if WithHistoricalRecoveryPreparedSubmission(context.Background(), m.dir, b, func(RuntimeRecoveryPreparedSubmissionHistory) error {
						t.Error("nested reader acquired lock")
						return nil
					}) != ErrRuntimeMutationBlocked {
						t.Error("flock not retained")
					}
					return nil
				})
				if err != nil {
					t.Fatal("history required live runtime", err)
				}
			}
			assertTerminalHistoryTree(t, m.dir, before)
			if lease, err := BeginRuntimeMutation(m.dir, resultTaskID, TaskKindVPNCoreService); err != ErrRuntimeMutationBlocked {
				if lease != nil {
					lease.Close()
				}
				t.Fatal("historical read released fence", err)
			}
			if err := WithHistoricalRecoveryPreparedRelease(context.Background(), m.dir, b, func(RuntimeRecoveryPreparedReleaseHistory) error { t.Error("old reader adopted new phase"); return nil }); err != ErrRuntimeMutationBlocked {
				t.Fatal(err)
			}
		})
	}
}

func TestPreparedSubmissionHistoryLiveOwner(t *testing.T) {
	_, _, b, _, m := preparedSubmissionFixture(t)
	defer m.Close()
	if err := m.WithPreparedReleaseSubmission(context.Background(), b, m.authorizationHash, func(g *RuntimeRecoveryPreparedReleaseGuard) error {
		_, err := g.PinProof(context.Background())
		return err
	}); err != nil {
		t.Fatal(err)
	}
	before := terminalHistoryTree(t, m.dir)
	if err := WithHistoricalRecoveryPreparedSubmission(context.Background(), m.dir, b, func(RuntimeRecoveryPreparedSubmissionHistory) error {
		t.Error("historical reader bypassed live owner")
		return nil
	}); err != ErrRuntimeMutationBlocked {
		t.Fatal(err)
	}
	m.Close()
	if err := WithHistoricalRecoveryPreparedSubmission(context.Background(), m.dir, b, nil); err != ErrRuntimeMutationBlocked {
		t.Fatal("nil callback", err)
	}
	assertTerminalHistoryTree(t, m.dir, before)
}

func TestPreparedSubmissionHistoryRefusals(t *testing.T) {
	for _, mode := range []string{"schema", "purpose", "operation", "generation", "authorization", "terminal_proof", "terminal_receipt", "intent_hash", "prepared_at", "before_prepared", "at_expiry", "observation", "duplicate", "ack_schema", "ack_recorded", "ack_operation", "ack_intent", "ack_proof", "ack_request", "missing_pin", "missing_intent", "missing_terminal_ack", "pin_mode", "pin_hardlink", "pin_symlink", "unknown", "binding", "cancelled", "callback_error", "same_bytes_replaced", "ack_removed", "archive_changed", "cancel_in_callback"} {
		t.Run(mode, func(t *testing.T) {
			_, b, m, p := preparedSubmissionHistoryFixture(t, true)
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			path := filepath.Join(m.dir, runtimeRecoveryPreparedSubmissionFile)
			pin, err := os.ReadFile(path)
			if err != nil {
				t.Fatal(err)
			}
			write := func(name string, data []byte) {
				t.Helper()
				if err := os.WriteFile(filepath.Join(m.dir, name), data, 0600); err != nil {
					t.Fatal(err)
				}
			}
			changed := true
			switch mode {
			case "schema":
				p.SchemaVersion++
			case "purpose":
				p.Purpose = "foreign"
			case "operation":
				p.OperationID = "ffffffff-ffff-4fff-8fff-ffffffffffff"
			case "generation":
				p.AgentCredentialGeneration++
			case "authorization":
				p.AuthorizationHash = p.ReleaseIntentHash
			case "terminal_proof":
				p.TerminalProofHash = p.ReleaseIntentHash
			case "terminal_receipt":
				p.TerminalReceiptHash = p.ReleaseIntentHash
			case "intent_hash":
				p.ReleaseIntentHash = p.TerminalProofHash
			case "prepared_at":
				p.PreparedAt = p.PreparedAt.Add(time.Nanosecond)
			case "before_prepared":
				p.ObservedAt = p.PreparedAt.Add(-time.Nanosecond)
			case "at_expiry":
				p.ObservedAt = m.authorization.ExpiresAt
			case "observation":
				p.Observation.MarkerHash = p.ReleaseIntentHash
			default:
				changed = false
			}
			if changed {
				data, _ := json.Marshal(p)
				write(runtimeRecoveryPreparedSubmissionFile, data)
				data, _ = json.Marshal(receiptForPreparedProof(p))
				write(runtimeRecoveryPreparedAcknowledgementFile, data)
			}
			r := receiptForPreparedProof(p)
			ackChanged := true
			switch mode {
			case "ack_schema":
				r.SchemaVersion++
			case "ack_recorded":
				r.Recorded = false
			case "ack_operation":
				r.OperationID = "ffffffff-ffff-4fff-8fff-ffffffffffff"
			case "ack_intent":
				r.ReleaseIntentHash = p.TerminalProofHash
			case "ack_proof":
				r.ProofHash = p.TerminalProofHash
			case "ack_request":
				r.RequestHash = p.TerminalProofHash
			default:
				ackChanged = false
			}
			if ackChanged {
				data, _ := json.Marshal(r)
				write(runtimeRecoveryPreparedAcknowledgementFile, data)
			}
			switch mode {
			case "duplicate":
				write(runtimeRecoveryPreparedSubmissionFile, append([]byte(`{"schemaVersion":1,`), pin[1:]...))
			case "missing_pin":
				os.Remove(path)
			case "missing_intent":
				os.Remove(filepath.Join(m.dir, runtimeRecoveryReleaseIntentFile))
			case "missing_terminal_ack":
				os.Remove(filepath.Join(m.dir, runtimeRecoveryTerminalAcknowledgementFile))
			case "pin_mode":
				os.Chmod(path, 0644)
			case "pin_hardlink":
				if err := os.Link(path, filepath.Join(t.TempDir(), "alias")); err != nil {
					t.Fatal(err)
				}
			case "pin_symlink":
				os.Remove(path)
				if err := os.Symlink(filepath.Join(m.dir, runtimeRecoveryTerminalSubmissionFile), path); err != nil {
					t.Fatal(err)
				}
			case "unknown":
				write("unknown.json", []byte("{}"))
			case "binding":
				b.AgentCredentialGeneration++
			case "cancelled":
				cancel()
			}
			before := terminalHistoryTree(t, m.dir)
			called := false
			err = WithHistoricalRecoveryPreparedSubmission(ctx, m.dir, b, func(RuntimeRecoveryPreparedSubmissionHistory) error {
				called = true
				switch mode {
				case "callback_error":
					return errors.New("sensitive fixture payload")
				case "same_bytes_replaced":
					write(".replacement", pin)
					if err := os.Rename(filepath.Join(m.dir, ".replacement"), path); err != nil {
						t.Fatal(err)
					}
				case "ack_removed":
					os.Remove(filepath.Join(m.dir, runtimeRecoveryPreparedAcknowledgementFile))
				case "archive_changed":
					write(filepath.Join(filepath.Base(m.archive), runtimeRecoveryFinalSubmissionFile), []byte("{}"))
				case "cancel_in_callback":
					cancel()
				default:
					t.Error("invalid history reached callback")
				}
				before = terminalHistoryTree(t, m.dir)
				return nil
			})
			if err != ErrRuntimeMutationBlocked {
				t.Fatal("invalid history accepted", err)
			}
			callbackCase := mode == "callback_error" || mode == "same_bytes_replaced" || mode == "ack_removed" || mode == "archive_changed" || mode == "cancel_in_callback"
			if called != callbackCase {
				t.Fatal("unexpected callback boundary")
			}
			assertTerminalHistoryTree(t, m.dir, before)
		})
	}
}
