package tasks

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"testing"
)

func assertHistoricalRecoveryAfterExit(t *testing.T, dir string, b RuntimeRecoveryGrantBinding) {
	t.Helper()
	pinned, err := os.ReadFile(filepath.Join(dir, runtimeRecoverySubmissionFile))
	if err != nil {
		t.Fatal(err)
	}
	if WithHistoricalRecoverySubmission(context.Background(), dir, b, func(p RuntimeRecoveryCommitProof) error {
		wire, err := json.Marshal(p)
		if err != nil || !bytes.Equal(wire, pinned) {
			t.Error("post-exit historical bytes changed")
		}
		return nil
	}) != nil {
		t.Fatal("post-exit historical read failed")
	}
}

func TestRuntimeMutationHistoricalRecoveryReadOnly(t *testing.T) {
	for _, ack := range []bool{false, true} {
		e, a, b, _, wire := recoveryCommitFixture(t)
		m, err := BeginRuntimeRecoveryCommit(context.Background(), e.receiptDir, b, wire, e.activePath, a)
		if err != nil {
			t.Fatal(err)
		}
		var pinned RuntimeRecoveryCommitProof
		err = m.WithSubmission(context.Background(), b, markerDigest(wire), func(g *RuntimeRecoveryCommitGuard) error {
			var e error
			pinned, e = g.PinProof(context.Background())
			if e == nil && ack {
				e = g.RecordReceipt(context.Background(), receiptForRecoveryProof(pinned))
			}
			return e
		})
		if err != nil {
			t.Fatal(err)
		}
		if WithHistoricalRecoverySubmission(context.Background(), m.dir, b, func(RuntimeRecoveryCommitProof) error { t.Error("live owner admitted historical reader"); return nil }) == nil {
			t.Fatal("live lock bypass")
		}
		m.Close()
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
			err = WithHistoricalRecoverySubmission(context.Background(), m.dir, b, func(p RuntimeRecoveryCommitProof) error {
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
		if _, e := BeginRuntimeRecoveryCommit(context.Background(), m.dir, b, wire, e.activePath, a); e == nil {
			t.Fatal("historical read resumed owner")
		}
	}
}

func TestRuntimeMutationHistoricalRecoveryRejects(t *testing.T) {
	for _, mode := range []string{"generation", "manager", "agent", "server", "missing_pin", "missing_consumption", "marker", "receipt", "duplicate", "alias", "unknown", "missing_field", "ack", "symlink", "permissions", "inflight", "cancelled", "changed_during_read", "ack_created_during_read", "callback_error"} {
		t.Run(mode, func(t *testing.T) {
			e, a, b, _, wire := recoveryCommitFixture(t)
			m, err := BeginRuntimeRecoveryCommit(context.Background(), e.receiptDir, b, wire, e.activePath, a)
			if err != nil {
				t.Fatal(err)
			}
			if m.WithSubmission(context.Background(), b, markerDigest(wire), func(g *RuntimeRecoveryCommitGuard) error { _, e := g.PinProof(context.Background()); return e }) != nil {
				t.Fatal("pin")
			}
			m.Close()
			path := filepath.Join(m.dir, runtimeRecoverySubmissionFile)
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
				os.Remove(filepath.Join(m.dir, runtimeRecoveryConsumptionFile))
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
				os.WriteFile(filepath.Join(m.dir, runtimeRecoveryAcknowledgementFile), []byte(`{}`), 0600)
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
			err = WithHistoricalRecoverySubmission(ctx, m.dir, b, func(RuntimeRecoveryCommitProof) error {
				called = true
				switch mode {
				case "changed_during_read":
					os.WriteFile(path, []byte(`{}`), 0600)
				case "ack_created_during_read":
					os.WriteFile(filepath.Join(m.dir, runtimeRecoveryAcknowledgementFile), []byte(`{}`), 0600)
				case "callback_error":
					return errors.New("secret-network-payload")
				}
				return nil
			})
			if err != ErrRuntimeMutationBlocked {
				t.Fatal("rejection not sanitized", err)
			}
			if called && mode != "changed_during_read" && mode != "ack_created_during_read" && mode != "callback_error" {
				t.Fatal("invalid state reached callback")
			}
		})
	}
}

func TestRuntimeMutationHistoricalRecoveryNeverProvisions(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "absent")
	if WithHistoricalRecoverySubmission(context.Background(), dir, RuntimeRecoveryGrantBinding{}, func(RuntimeRecoveryCommitProof) error { return nil }) == nil {
		t.Fatal("absent accepted")
	}
	if _, err := os.Lstat(dir); !os.IsNotExist(err) {
		t.Fatal("read created directory")
	}
	os.Mkdir(dir, 0700)
	if WithHistoricalRecoverySubmission(context.Background(), dir, RuntimeRecoveryGrantBinding{}, func(RuntimeRecoveryCommitProof) error { return nil }) == nil {
		t.Fatal("missing lock accepted")
	}
	if _, err := os.Lstat(filepath.Join(dir, "runtime.lock")); !os.IsNotExist(err) {
		t.Fatal("read created lock")
	}
}
