package tasks

import (
	"bytes"
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func retirementFixture(t *testing.T) (CredentialRemovalExecutor, *removalAdapter, RuntimeRecoveryGrantBinding, RuntimeRecoveryReleaseAuthorization, *RuntimeRecoveryFinalization) {
	t.Helper()
	e, a, b, auth := finalizationFixture(t)
	wire, _ := json.Marshal(auth)
	m, err := BeginRuntimeRecoveryFinalization(context.Background(), e.receiptDir, b, wire, e.activePath, a)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { m.Close() })
	return e, a, b, auth, m
}
func prepareRetirementFixture(t *testing.T, m *RuntimeRecoveryFinalization, b RuntimeRecoveryGrantBinding) string {
	t.Helper()
	var hash string
	err := m.WithSubmission(context.Background(), b, m.authorizationHash, func(g *RuntimeRecoveryFinalizationGuard) error {
		p, e := g.PinProof(context.Background())
		if e != nil {
			return e
		}
		if e = g.RecordReceipt(context.Background(), receiptForFinalizationProof(p)); e != nil {
			return e
		}
		hash, e = g.PrepareRetirement(context.Background())
		return e
	})
	if err != nil {
		t.Fatal(err)
	}
	return hash
}
func TestRuntimeMutationRetirementIntentRetained(t *testing.T) {
	e, a, b, auth, m := retirementFixture(t)
	before := map[string][]byte{}
	entries, _ := os.ReadDir(m.dir)
	for _, entry := range entries {
		data, err := os.ReadFile(filepath.Join(m.dir, entry.Name()))
		if err != nil {
			t.Fatal(err)
		}
		before[entry.Name()] = data
	}
	hash := prepareRetirementFixture(t, m, b)
	data, err := os.ReadFile(filepath.Join(m.dir, runtimeRecoveryRetirementFile))
	if err != nil || hash != markerDigest(data) {
		t.Fatal("intent hash", err)
	}
	var record runtimeRecoveryRetirementIntent
	if !exactHistoricalJSON(data, &record) || record.Binding != b || record.AuthorizationHash != m.authorizationHash || record.Observation != auth.Expected || len(record.Files) != 8 {
		t.Fatal("intent binding")
	}
	var escaped, copied *RuntimeRecoveryFinalizationGuard
	if err = m.WithSubmission(context.Background(), b, m.authorizationHash, func(g *RuntimeRecoveryFinalizationGuard) error {
		escaped = g
		clone := *g
		copied = &clone
		got, e := g.PrepareRetirement(context.Background())
		if e != nil || got != hash {
			t.Error("intent repeat", e)
		}
		if lock, e := lockRuntimeMutation(m.dir); e == nil {
			lock.Close()
			t.Error("intent preparation dropped flock")
		}
		return e
	}); err != nil {
		t.Fatal(err)
	}
	for _, g := range []*RuntimeRecoveryFinalizationGuard{escaped, copied, nil, {}} {
		if h, e := g.PrepareRetirement(context.Background()); h != "" || e != ErrRuntimeMutationBlocked {
			t.Fatal("inactive guard prepared intent", e)
		}
	}
	repeated, _ := os.ReadFile(filepath.Join(m.dir, runtimeRecoveryRetirementFile))
	if !bytes.Equal(data, repeated) {
		t.Fatal("intent repeat renewed time/bytes")
	}
	for name, want := range before {
		got, err := os.ReadFile(filepath.Join(m.dir, name))
		if err != nil || !bytes.Equal(got, want) {
			t.Fatal("intent changed original state", name, err)
		}
	}
	if a.restarts != 1 || a.validations != 0 {
		t.Fatal("intent mutated runtime")
	}
	m.Close()
	assertHistoricalFinalizationAfterExit(t, m.dir, b)
	if _, e := BeginRuntimeRecoveryFinalization(context.Background(), m.dir, b, mustRetirementJSON(t, auth), e.activePath, a); e == nil {
		t.Fatal("intent resumed owner")
	}
	if lease, e := BeginRuntimeMutation(m.dir, auth.Expected.TaskID, TaskKindVPNCoreService); e == nil {
		lease.Close()
		t.Fatal("prepared intent released fence")
	}
}
func mustRetirementJSON(t *testing.T, v any) []byte {
	t.Helper()
	data, e := json.Marshal(v)
	if e != nil {
		t.Fatal(e)
	}
	return data
}

func TestRuntimeMutationRetirementIntentConcurrent(t *testing.T) {
	_, _, b, _, m := retirementFixture(t)
	type result struct {
		hash string
		err  error
	}
	results := make(chan result, 2)
	for range 2 {
		go func() {
			var hash string
			err := m.WithSubmission(context.Background(), b, m.authorizationHash, func(g *RuntimeRecoveryFinalizationGuard) error {
				p, e := g.PinProof(context.Background())
				if e != nil {
					return e
				}
				if e = g.RecordReceipt(context.Background(), receiptForFinalizationProof(p)); e != nil {
					return e
				}
				hash, e = g.PrepareRetirement(context.Background())
				return e
			})
			results <- result{hash, err}
		}()
	}
	a, c := <-results, <-results
	if a.err != nil || c.err != nil || a.hash == "" || a.hash != c.hash {
		t.Fatal("concurrent intent diverged", a.err, c.err)
	}
	data, e := os.ReadFile(filepath.Join(m.dir, runtimeRecoveryRetirementFile))
	if e != nil || markerDigest(data) != a.hash {
		t.Fatal("single retained intent", e)
	}
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
		done <- m.WithSubmission(context.Background(), b, m.authorizationHash, func(g *RuntimeRecoveryFinalizationGuard) error {
			close(entered)
			<-release
			_, e := g.PrepareRetirement(context.Background())
			return e
		})
	}()
	select {
	case <-entered:
	case <-time.After(time.Second):
		t.Fatal("callback did not enter")
	}
	closed := make(chan error, 1)
	go func() { closed <- m.Close() }()
	select {
	case <-closed:
		t.Fatal("Close interrupted intent owner")
	case <-time.After(25 * time.Millisecond):
	}
	if lock, e := lockRuntimeMutation(m.dir); e == nil {
		lock.Close()
		t.Fatal("Close dropped flock during callback")
	}
	unblock()
	if e := <-done; e != nil {
		t.Fatal(e)
	}
	if e := <-closed; e != nil {
		t.Fatal(e)
	}
	assertHistoricalFinalizationAfterExit(t, m.dir, b)
}

func TestRuntimeMutationRetirementIntentRejects(t *testing.T) {
	for _, mode := range []string{"no_pin", "no_ack", "cancelled", "expired", "backwards", "runtime", "closed", "existing", "publication_drift", "publication_expiry", "publication_backwards"} {
		t.Run(mode, func(t *testing.T) {
			e, a, b, auth, m := retirementFixture(t)
			clock := time.Now().UTC()
			m.now = func() time.Time { return clock }
			if mode == "closed" {
				m.Close()
			}
			if mode == "existing" {
				os.WriteFile(filepath.Join(m.dir, runtimeRecoveryRetirementFile), []byte(`{}`), 0600)
			}
			ctx := context.Background()
			err := m.WithSubmission(ctx, b, m.authorizationHash, func(g *RuntimeRecoveryFinalizationGuard) error {
				if mode != "no_pin" {
					p, e := g.PinProof(ctx)
					if e != nil {
						return e
					}
					if mode != "no_ack" {
						if e = g.RecordReceipt(ctx, receiptForFinalizationProof(p)); e != nil {
							return e
						}
					}
				}
				switch mode {
				case "cancelled":
					c, cancel := context.WithCancel(ctx)
					cancel()
					ctx = c
				case "expired":
					clock = auth.ExpiresAt
				case "backwards":
					clock = clock.Add(-time.Second)
				case "runtime":
					os.WriteFile(e.activePath, []byte(`{}`), 0600)
				case "publication_drift", "publication_expiry", "publication_backwards":
					a.healthHook = func() error {
						if !runtimeMarkerAbsent(filepath.Join(m.dir, runtimeRecoveryRetirementFile)) {
							switch mode {
							case "publication_drift":
								return os.WriteFile(e.activePath, []byte(`{}`), 0600)
							case "publication_expiry":
								clock = auth.ExpiresAt
							case "publication_backwards":
								clock = clock.Add(-time.Second)
							}
						}
						return nil
					}
				}
				h, e := g.PrepareRetirement(ctx)
				if h != "" || e != ErrRuntimeMutationBlocked {
					t.Error("unsafe intent prepared", e)
				}
				return e
			})
			if err != ErrRuntimeMutationBlocked {
				t.Fatal("unsafe preparation accepted", err)
			}
			published := strings.HasPrefix(mode, "publication_") || mode == "existing"
			if runtimeMarkerAbsent(filepath.Join(m.dir, runtimeRecoveryRetirementFile)) == published {
				t.Fatal("publication uncertainty lost/created intent")
			}
			for _, name := range []string{"mutation-inflight.json", runtimeResultFile, runtimeRecoveryConsumptionFile, runtimeRecoveryFinalConsumptionFile} {
				if runtimeMarkerAbsent(filepath.Join(m.dir, name)) {
					t.Fatal("refusal lost fence", name)
				}
			}
		})
	}
}

func TestRuntimeMutationRetirementIntentPoisoned(t *testing.T) {
	for _, mode := range []string{"tampered", "deleted", "runtime"} {
		t.Run(mode, func(t *testing.T) {
			e, _, b, _, m := retirementFixture(t)
			prepareRetirementFixture(t, m, b)
			path := filepath.Join(m.dir, runtimeRecoveryRetirementFile)
			saved, _ := os.ReadFile(path)
			switch mode {
			case "tampered":
				os.WriteFile(path, []byte(`{}`), 0600)
			case "deleted":
				os.Remove(path)
			case "runtime":
				os.WriteFile(e.activePath, []byte(`{}`), 0600)
			}
			if _, err := m.Proof(context.Background()); err != ErrRuntimeMutationBlocked {
				t.Fatal("intent drift admitted", err)
			}
			if mode != "runtime" {
				os.WriteFile(path, saved, 0600)
			}
			if _, err := m.Proof(context.Background()); err != ErrRuntimeMutationBlocked {
				t.Fatal("poisoned owner revived", err)
			}
		})
	}
}

func TestRuntimeMutationRetirementIntentHistoricalRejects(t *testing.T) {
	for _, mode := range []string{"schema", "purpose", "operation", "binding", "authorization", "consumption", "proof", "receipt", "time", "observation", "manifest_name", "manifest_hash", "manifest_presence", "manifest_order", "duplicate", "alias", "unknown", "missing_ack", "changed_during_read"} {
		t.Run(mode, func(t *testing.T) {
			_, _, b, auth, m := retirementFixture(t)
			prepareRetirementFixture(t, m, b)
			m.Close()
			path := filepath.Join(m.dir, runtimeRecoveryRetirementFile)
			data, _ := os.ReadFile(path)
			var r runtimeRecoveryRetirementIntent
			json.Unmarshal(data, &r)
			switch mode {
			case "schema":
				r.SchemaVersion = 2
			case "purpose":
				r.Purpose = "released"
			case "operation":
				r.OperationID = b.AgentID
			case "binding":
				r.Binding.AgentCredentialGeneration++
			case "authorization":
				r.AuthorizationHash = strings.Repeat("f", 64)
			case "consumption":
				r.FinalConsumptionHash = strings.Repeat("f", 64)
			case "proof":
				r.FinalProofHash = strings.Repeat("f", 64)
			case "receipt":
				r.FinalReceiptHash = strings.Repeat("f", 64)
			case "time":
				r.ObservedAt = auth.ExpiresAt
			case "observation":
				r.Observation.Witness.ListenerPort++
			case "manifest_name":
				r.Files[0].Name = "../untrusted"
			case "manifest_hash":
				r.Files[0].Hash = strings.Repeat("f", 64)
			case "manifest_presence":
				r.Files[0].Present = false
			case "manifest_order":
				r.Files[0], r.Files[1] = r.Files[1], r.Files[0]
			case "missing_ack":
				os.Remove(filepath.Join(m.dir, runtimeRecoveryFinalAcknowledgementFile))
			}
			data = mustRetirementJSON(t, r)
			switch mode {
			case "duplicate":
				data = append([]byte(`{"schemaVersion":2,`), data[1:]...)
			case "alias":
				data = bytes.Replace(data, []byte(`"schemaVersion"`), []byte(`"SchemaVersion"`), 1)
			case "unknown":
				data = append([]byte(`{"secret":"payload",`), data[1:]...)
			}
			if os.WriteFile(path, data, 0600) != nil {
				t.Fatal("fixture write")
			}
			called := false
			err := WithHistoricalRecoveryFinalization(context.Background(), m.dir, b, func(RuntimeRecoveryFinalizationProof) error {
				called = true
				if mode == "changed_during_read" {
					os.WriteFile(path, []byte(`{}`), 0600)
				}
				return nil
			})
			if err != ErrRuntimeMutationBlocked || called != (mode == "changed_during_read") {
				t.Fatal("unsafe historical intent", err, called)
			}
		})
	}
}
