package tasks

import (
	"bytes"
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

func TestRuntimeMutationFinalAcknowledgementRetained(t *testing.T) {
	e, a, b, auth := finalizationFixture(t)
	wire, _ := json.Marshal(auth)
	m, err := BeginRuntimeRecoveryFinalization(context.Background(), e.receiptDir, b, wire, e.activePath, a)
	if err != nil {
		t.Fatal(err)
	}
	defer m.Close()
	paths := []string{e.activePath, filepath.Join(m.dir, "mutation-inflight.json"), filepath.Join(m.dir, runtimeResultFile), filepath.Join(m.dir, runtimeRecoveryConsumptionFile)}
	before := make([][]byte, len(paths))
	for i, path := range paths {
		before[i], err = os.ReadFile(path)
		if err != nil {
			t.Fatal(err)
		}
	}
	var r RuntimeRecoveryFinalizationReceipt
	var saved []byte
	var escaped, copied *RuntimeRecoveryFinalizationGuard
	for i := 0; i < 2; i++ {
		err = m.WithSubmission(context.Background(), b, markerDigest(wire), func(g *RuntimeRecoveryFinalizationGuard) error {
			escaped = g
			clone := *g
			copied = &clone
			p, e := g.PinProof(context.Background())
			if e != nil {
				return e
			}
			r = receiptForFinalizationProof(p)
			if e = g.RecordReceipt(context.Background(), r); e != nil {
				return e
			}
			data, e := readRemovalFile(filepath.Join(m.dir, runtimeRecoveryFinalAcknowledgementFile), 4096, true)
			if e != nil {
				return e
			}
			if i == 0 {
				saved = data
			} else if !bytes.Equal(data, saved) {
				t.Error("retry changed acknowledgement")
			}
			if lock, e := lockRuntimeMutation(m.dir); e == nil {
				lock.Close()
				t.Error("ack persistence dropped flock")
			}
			return nil
		})
		if err != nil {
			t.Fatal(err)
		}
	}
	if escaped.RecordReceipt(context.Background(), r) == nil || copied.RecordReceipt(context.Background(), r) == nil {
		t.Fatal("expired/copy guard wrote acknowledgement")
	}
	want, _ := json.Marshal(r)
	if !bytes.Equal(saved, want) {
		t.Fatal("receipt wire not retained exactly")
	}
	if err = m.Close(); err != nil {
		t.Fatal(err)
	}
	for i, path := range paths {
		data, e := os.ReadFile(path)
		if e != nil || !bytes.Equal(data, before[i]) {
			t.Fatal("ack mutated runtime/receipt/fence/consumption", e)
		}
	}
	if runtimeMarkerAbsent(filepath.Join(m.dir, runtimeRecoveryFinalAcknowledgementFile)) || runtimeMarkerAbsent(filepath.Join(m.dir, runtimeRecoveryFinalSubmissionFile)) {
		t.Fatal("Close erased acknowledgement/submission")
	}
}

func TestRuntimeMutationFinalAcknowledgementRejects(t *testing.T) {
	for _, mode := range []string{"not_pinned", "schema", "recorded", "operation", "grant", "consumption", "proof", "request", "cancelled"} {
		t.Run(mode, func(t *testing.T) {
			e, a, b, auth := finalizationFixture(t)
			wire, _ := json.Marshal(auth)
			m, err := BeginRuntimeRecoveryFinalization(context.Background(), e.receiptDir, b, wire, e.activePath, a)
			if err != nil {
				t.Fatal(err)
			}
			defer m.Close()
			err = m.WithSubmission(context.Background(), b, markerDigest(wire), func(g *RuntimeRecoveryFinalizationGuard) error {
				p, e := g.Proof(context.Background())
				if e != nil {
					return e
				}
				if mode != "not_pinned" {
					p, e = g.PinProof(context.Background())
					if e != nil {
						return e
					}
				}
				r := receiptForFinalizationProof(p)
				ctx := context.Background()
				switch mode {
				case "schema":
					r.SchemaVersion = 2
				case "recorded":
					r.Recorded = false
				case "operation":
					r.OperationID = b.AgentID
				case "grant":
					r.AuthorizationHash = "bad"
				case "consumption":
					r.FinalConsumptionHash = "bad"
				case "proof":
					r.ProofHash = "bad"
				case "request":
					r.RequestHash = "bad"
				case "cancelled":
					cancelled, cancel := context.WithCancel(ctx)
					cancel()
					ctx = cancelled
				}
				return g.RecordReceipt(ctx, r)
			})
			if err == nil {
				t.Fatal("invalid acknowledgement persisted")
			}
			if !runtimeMarkerAbsent(filepath.Join(m.dir, runtimeRecoveryFinalAcknowledgementFile)) {
				t.Fatal("invalid receipt retained")
			}
		})
	}
}

func TestRuntimeMutationFinalAcknowledgementPoisoned(t *testing.T) {
	for _, mode := range []string{"tampered", "deleted", "post_write_drift"} {
		t.Run(mode, func(t *testing.T) {
			e, a, b, auth := finalizationFixture(t)
			wire, _ := json.Marshal(auth)
			m, err := BeginRuntimeRecoveryFinalization(context.Background(), e.receiptDir, b, wire, e.activePath, a)
			if err != nil {
				t.Fatal(err)
			}
			defer m.Close()
			if mode == "post_write_drift" {
				a.healthHook = func() error {
					if !runtimeMarkerAbsent(filepath.Join(m.dir, runtimeRecoveryFinalAcknowledgementFile)) {
						return os.WriteFile(e.activePath, []byte("{}"), 0600)
					}
					return nil
				}
			}
			err = m.WithSubmission(context.Background(), b, markerDigest(wire), func(g *RuntimeRecoveryFinalizationGuard) error {
				p, e := g.PinProof(context.Background())
				if e != nil {
					return e
				}
				if e = g.RecordReceipt(context.Background(), receiptForFinalizationProof(p)); e != nil {
					return e
				}
				switch mode {
				case "tampered":
					return os.WriteFile(filepath.Join(m.dir, runtimeRecoveryFinalAcknowledgementFile), []byte("{}"), 0600)
				case "deleted":
					return os.Remove(filepath.Join(m.dir, runtimeRecoveryFinalAcknowledgementFile))
				}
				return nil
			})
			if err == nil {
				t.Fatal("ack tamper/drift accepted")
			}
			if _, err = m.Proof(context.Background()); err == nil {
				t.Fatal("poisoned ack owner revived")
			}
			if runtimeMarkerAbsent(filepath.Join(m.dir, runtimeRecoveryConsumptionFile)) || runtimeMarkerAbsent(filepath.Join(m.dir, runtimeRecoveryFinalSubmissionFile)) || runtimeMarkerAbsent(filepath.Join(m.dir, "mutation-inflight.json")) {
				t.Fatal("ack error erased durable fence")
			}
			if mode == "post_write_drift" && runtimeMarkerAbsent(filepath.Join(m.dir, runtimeRecoveryFinalAcknowledgementFile)) {
				t.Fatal("post-publication failure erased acknowledgement")
			}
		})
	}
}
