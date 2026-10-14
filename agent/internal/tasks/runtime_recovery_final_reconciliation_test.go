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

func assertHistoricalFinalizationAfterExit(t *testing.T, dir string, b RuntimeRecoveryGrantBinding) {
	t.Helper()
	pinned, err := os.ReadFile(filepath.Join(dir, runtimeRecoveryFinalSubmissionFile))
	if err != nil {
		t.Fatal(err)
	}
	if WithHistoricalRecoveryFinalization(context.Background(), dir, b, func(p RuntimeRecoveryFinalizationProof) error {
		wire, err := json.Marshal(p)
		if err != nil || !bytes.Equal(wire, pinned) {
			t.Error("post-exit historical bytes changed")
		}
		return nil
	}) != nil {
		t.Fatal("post-exit historical read failed")
	}
}

func TestRuntimeMutationHistoricalFinalRecoveryReadOnly(t *testing.T) {
	for _, ack := range []bool{false, true} {
		e, a, b, auth := finalizationFixture(t)
		auth.ExpiresAt = time.Now().UTC().Add(time.Second)
		wire, _ := json.Marshal(auth)
		m, err := BeginRuntimeRecoveryFinalization(context.Background(), e.receiptDir, b, wire, e.activePath, a)
		if err != nil {
			t.Fatal(err)
		}
		var pinned RuntimeRecoveryFinalizationProof
		err = m.WithSubmission(context.Background(), b, markerDigest(wire), func(g *RuntimeRecoveryFinalizationGuard) error {
			var e error
			pinned, e = g.PinProof(context.Background())
			if e == nil && ack {
				e = g.RecordReceipt(context.Background(), receiptForFinalizationProof(pinned))
			}
			return e
		})
		if err != nil {
			t.Fatal(err)
		}
		if WithHistoricalRecoveryFinalization(context.Background(), m.dir, b, func(RuntimeRecoveryFinalizationProof) error {
			t.Error("live owner admitted historical reader")
			return nil
		}) == nil {
			t.Fatal("live lock bypass")
		}
		m.Close()
		time.Sleep(time.Until(auth.ExpiresAt) + 10*time.Millisecond)
		// Runtime drift prevents a new live proof but not historical storage read.
		if os.WriteFile(e.activePath, []byte(`{}`), 0600) != nil {
			t.Fatal("drift")
		}
		entries, _ := os.ReadDir(m.dir)
		before := map[string][]byte{}
		for _, entry := range entries {
			before[entry.Name()], err = os.ReadFile(filepath.Join(m.dir, entry.Name()))
			if err != nil {
				t.Fatal(err)
			}
		}
		for range 2 {
			err = WithHistoricalRecoveryFinalization(context.Background(), m.dir, b, func(p RuntimeRecoveryFinalizationProof) error {
				if p != pinned {
					t.Error("historical proof changed")
				}
				if lock, e := lockRuntimeMutation(m.dir); e == nil {
					lock.Close()
					t.Error("historical callback lost flock")
				}
				return nil
			})
			if err != nil {
				t.Fatal(err)
			}
		}
		after, _ := os.ReadDir(m.dir)
		if len(after) != len(entries) {
			t.Fatal("read created state")
		}
		for name, expected := range before {
			data, e := os.ReadFile(filepath.Join(m.dir, name))
			if e != nil || !bytes.Equal(data, expected) {
				t.Fatal("read changed state", name)
			}
		}
		if lease, e := BeginRuntimeMutation(m.dir, pinned.Observation.TaskID, TaskKindVPNCoreService); e == nil {
			lease.Close()
			t.Fatal("historical read unlocked fence")
		}
		if _, e := BeginRuntimeRecoveryFinalization(context.Background(), m.dir, b, wire, e.activePath, a); e == nil {
			t.Fatal("historical read resumed owner")
		}
	}
}

func TestRuntimeMutationHistoricalFinalRecoveryRejects(t *testing.T) {
	for _, mode := range []string{"generation", "manager", "agent", "server", "missing_pin", "missing_consumption", "marker", "receipt", "duplicate", "alias", "unknown", "missing_field", "ack", "symlink", "permissions", "inflight", "cancelled", "changed_during_read", "ack_created_during_read", "original_changed_during_read", "original_ack_created_during_read", "consumption_changed_during_read", "callback_error"} {
		t.Run(mode, func(t *testing.T) {
			e, a, b, auth := finalizationFixture(t)
			wire, _ := json.Marshal(auth)
			m, err := BeginRuntimeRecoveryFinalization(context.Background(), e.receiptDir, b, wire, e.activePath, a)
			if err != nil {
				t.Fatal(err)
			}
			if m.WithSubmission(context.Background(), b, markerDigest(wire), func(g *RuntimeRecoveryFinalizationGuard) error { _, e := g.PinProof(context.Background()); return e }) != nil {
				t.Fatal("pin")
			}
			m.Close()
			path := filepath.Join(m.dir, runtimeRecoveryFinalSubmissionFile)
			data, _ := os.ReadFile(path)
			ctx := context.Background()
			switch mode {
			case "generation":
				b.AgentCredentialGeneration++
			case "manager":
				b.ManagerURL = "https://foreign.invalid"
			case "agent":
				b.AgentID = resultTaskID
			case "server":
				b.ServerID = resultTaskID
			case "missing_pin":
				os.Remove(path)
			case "missing_consumption":
				os.Remove(filepath.Join(m.dir, runtimeRecoveryFinalConsumptionFile))
			case "marker":
				os.WriteFile(filepath.Join(m.dir, "mutation-inflight.json"), []byte(`{}`), 0600)
			case "receipt":
				os.WriteFile(filepath.Join(m.dir, runtimeResultFile), []byte(`{}`), 0600)
			case "duplicate":
				os.WriteFile(path, append([]byte(`{"schemaVersion":1,`), data[1:]...), 0600)
			case "alias":
				os.WriteFile(path, bytes.Replace(data, []byte(`"schemaVersion"`), []byte(`"SchemaVersion"`), 1), 0600)
			case "unknown":
				os.WriteFile(path, append([]byte(`{"secret":"payload",`), data[1:]...), 0600)
			case "missing_field":
				var p map[string]any
				json.Unmarshal(data, &p)
				delete(p, "observedAt")
				bad, _ := json.Marshal(p)
				os.WriteFile(path, bad, 0600)
			case "ack":
				os.WriteFile(filepath.Join(m.dir, runtimeRecoveryFinalAcknowledgementFile), []byte(`{}`), 0600)
			case "symlink":
				os.Remove(path)
				os.Symlink(e.activePath, path)
			case "permissions":
				os.Chmod(path, 0644)
			case "inflight":
				os.WriteFile(filepath.Join(m.dir, "inflight.json"), []byte(`{}`), 0600)
			case "cancelled":
				c, cancel := context.WithCancel(ctx)
				cancel()
				ctx = c
			}
			called := false
			err = WithHistoricalRecoveryFinalization(ctx, m.dir, b, func(RuntimeRecoveryFinalizationProof) error {
				called = true
				switch mode {
				case "changed_during_read":
					os.WriteFile(path, []byte(`{}`), 0600)
				case "ack_created_during_read":
					os.WriteFile(filepath.Join(m.dir, runtimeRecoveryFinalAcknowledgementFile), []byte(`{}`), 0600)
				case "original_changed_during_read":
					os.WriteFile(filepath.Join(m.dir, runtimeRecoverySubmissionFile), []byte(`{}`), 0600)
				case "original_ack_created_during_read":
					os.WriteFile(filepath.Join(m.dir, runtimeRecoveryAcknowledgementFile), []byte(`{}`), 0600)
				case "consumption_changed_during_read":
					os.WriteFile(filepath.Join(m.dir, runtimeRecoveryFinalConsumptionFile), []byte(`{}`), 0600)
				case "callback_error":
					return errors.New("secret-network-payload")
				}
				return nil
			})
			if err != ErrRuntimeMutationBlocked {
				t.Fatal("rejection not sanitized", err)
			}
			if called && mode != "changed_during_read" && mode != "ack_created_during_read" && mode != "original_changed_during_read" && mode != "original_ack_created_during_read" && mode != "consumption_changed_during_read" && mode != "callback_error" {
				t.Fatal("invalid state reached callback")
			}
		})
	}
}

func TestRuntimeMutationHistoricalFinalRecoveryConcurrent(t *testing.T) {
	e, a, b, auth := finalizationFixture(t)
	wire, _ := json.Marshal(auth)
	m, err := BeginRuntimeRecoveryFinalization(context.Background(), e.receiptDir, b, wire, e.activePath, a)
	if err != nil {
		t.Fatal(err)
	}
	if err = m.WithSubmission(context.Background(), b, markerDigest(wire), func(g *RuntimeRecoveryFinalizationGuard) error { _, e := g.PinProof(context.Background()); return e }); err != nil {
		m.Close()
		t.Fatal(err)
	}
	m.Close()
	entered, release, done := make(chan struct{}), make(chan struct{}), make(chan error, 1)
	unblock := func() {
		select {
		case <-release:
		default:
			close(release)
		}
	}
	defer unblock()
	go func() {
		done <- WithHistoricalRecoveryFinalization(context.Background(), m.dir, b, func(RuntimeRecoveryFinalizationProof) error { close(entered); <-release; return nil })
	}()
	select {
	case <-entered:
	case <-time.After(time.Second):
		t.Fatal("reader did not enter callback")
	}
	if WithHistoricalRecoveryFinalization(context.Background(), m.dir, b, func(RuntimeRecoveryFinalizationProof) error { t.Error("concurrent reader bypassed flock"); return nil }) != ErrRuntimeMutationBlocked {
		t.Fatal("concurrent reader admitted")
	}
	unblock()
	if err = <-done; err != nil {
		t.Fatal(err)
	}
	assertHistoricalFinalizationAfterExit(t, m.dir, b)
}

func TestRuntimeMutationHistoricalFinalRecoveryNeverProvisions(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "absent")
	if WithHistoricalRecoveryFinalization(context.Background(), dir, RuntimeRecoveryGrantBinding{}, func(RuntimeRecoveryFinalizationProof) error { return nil }) == nil {
		t.Fatal("absent accepted")
	}
	if _, err := os.Lstat(dir); !os.IsNotExist(err) {
		t.Fatal("read created directory")
	}
	os.Mkdir(dir, 0700)
	if WithHistoricalRecoveryFinalization(context.Background(), dir, RuntimeRecoveryGrantBinding{}, func(RuntimeRecoveryFinalizationProof) error { return nil }) == nil {
		t.Fatal("missing lock accepted")
	}
	if _, err := os.Lstat(filepath.Join(dir, "runtime.lock")); !os.IsNotExist(err) {
		t.Fatal("read created lock")
	}
}

func TestRuntimeMutationHistoricalFinalRecoveryBindings(t *testing.T) {
	for _, mode := range []string{"record_schema", "record_generation", "authorization_hash", "record_time", "record_observation", "nonce_reuse", "purpose", "authorization_generation", "original_grant", "original_consumption", "original_proof", "lifetime", "proof_time", "proof_schema", "proof_operation", "proof_observation", "proof_authorization", "proof_consumption"} {
		t.Run(mode, func(t *testing.T) {
			e, a, b, auth := finalizationFixture(t)
			wire, _ := json.Marshal(auth)
			m, err := BeginRuntimeRecoveryFinalization(context.Background(), e.receiptDir, b, wire, e.activePath, a)
			if err != nil {
				t.Fatal(err)
			}
			if err = m.WithSubmission(context.Background(), b, markerDigest(wire), func(g *RuntimeRecoveryFinalizationGuard) error { _, e := g.PinProof(context.Background()); return e }); err != nil {
				m.Close()
				t.Fatal(err)
			}
			m.Close()
			read := func(name string, out any) {
				data, e := os.ReadFile(filepath.Join(m.dir, name))
				if e != nil || json.Unmarshal(data, out) != nil {
					t.Fatal("fixture read", e)
				}
			}
			var record runtimeRecoveryFinalConsumption
			var proof RuntimeRecoveryFinalizationProof
			var original runtimeRecoveryConsumption
			read(runtimeRecoveryFinalConsumptionFile, &record)
			read(runtimeRecoveryFinalSubmissionFile, &proof)
			read(runtimeRecoveryConsumptionFile, &original)
			switch mode {
			case "record_schema":
				record.SchemaVersion = 2
			case "record_generation":
				record.Binding.AgentCredentialGeneration++
			case "record_time":
				record.ObservedAt = auth.IssuedAt.Add(-time.Nanosecond)
			case "record_observation":
				record.Observation.Witness.ListenerPort++
			case "nonce_reuse":
				record.Authorization.Nonce = original.Grant.Nonce
			case "purpose":
				record.Authorization.Purpose = "untrusted"
			case "authorization_generation":
				record.Authorization.AgentCredentialGeneration++
			case "original_grant":
				record.Authorization.GrantHash = strings.Repeat("f", 64)
			case "original_consumption":
				record.Authorization.ConsumptionHash = strings.Repeat("f", 64)
			case "original_proof":
				record.Authorization.CommitProofHash = strings.Repeat("f", 64)
			case "lifetime":
				record.Authorization.ExpiresAt = auth.IssuedAt.Add(3 * time.Minute)
			case "proof_time":
				proof.ObservedAt = auth.ExpiresAt
			case "proof_schema":
				proof.SchemaVersion = 2
			case "proof_operation":
				proof.OperationID = b.AgentID
			case "proof_observation":
				proof.Observation.Witness.ListenerPort++
			}
			wire, _ = json.Marshal(record.Authorization)
			record.AuthorizationHash = markerDigest(wire)
			if mode == "authorization_hash" {
				record.AuthorizationHash = strings.Repeat("f", 64)
			}
			recordWire, _ := json.Marshal(record)
			proof.AuthorizationHash = record.AuthorizationHash
			proof.FinalConsumptionHash = markerDigest(recordWire)
			if mode == "proof_authorization" {
				proof.AuthorizationHash = strings.Repeat("e", 64)
			}
			if mode == "proof_consumption" {
				proof.FinalConsumptionHash = strings.Repeat("e", 64)
			}
			proofWire, _ := json.Marshal(proof)
			if os.WriteFile(filepath.Join(m.dir, runtimeRecoveryFinalConsumptionFile), recordWire, 0600) != nil || os.WriteFile(filepath.Join(m.dir, runtimeRecoveryFinalSubmissionFile), proofWire, 0600) != nil {
				t.Fatal("fixture write")
			}
			if err = WithHistoricalRecoveryFinalization(context.Background(), m.dir, b, func(RuntimeRecoveryFinalizationProof) error { t.Error("invalid binding reached callback"); return nil }); err != ErrRuntimeMutationBlocked {
				t.Fatal("binding accepted", err)
			}
			for name, want := range map[string][]byte{runtimeRecoveryFinalConsumptionFile: recordWire, runtimeRecoveryFinalSubmissionFile: proofWire} {
				got, e := os.ReadFile(filepath.Join(m.dir, name))
				if e != nil || !bytes.Equal(got, want) {
					t.Fatal("refusal changed history", name, e)
				}
			}
		})
	}
}
