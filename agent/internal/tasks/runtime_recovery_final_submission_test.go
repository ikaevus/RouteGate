package tasks

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestRuntimeMutationFinalSubmissionPinnedAcrossCalls(t *testing.T) {
	e, a, b, auth := finalizationFixture(t)
	wire, _ := json.Marshal(auth)
	m, err := BeginRuntimeRecoveryFinalization(context.Background(), e.receiptDir, b, wire, e.activePath, a)
	if err != nil {
		t.Fatal(err)
	}
	defer m.Close()
	before, err := os.ReadFile(filepath.Join(e.receiptDir, runtimeRecoveryFinalConsumptionFile))
	if err != nil {
		t.Fatal(err)
	}
	var first RuntimeRecoveryFinalizationProof
	var escaped *RuntimeRecoveryFinalizationGuard
	var copied RuntimeRecoveryFinalizationGuard
	for i := 0; i < 2; i++ {
		err = m.WithSubmission(context.Background(), b, markerDigest(wire), func(g *RuntimeRecoveryFinalizationGuard) error {
			escaped = g
			copied = *g
			p, e := g.PinProof(context.Background())
			if e != nil {
				return e
			}
			if i == 0 {
				first = p
			} else if p != first {
				t.Error("repeat changed pinned proof")
			}
			fresh, e := g.Proof(context.Background())
			if e != nil || fresh.ObservedAt.Before(p.ObservedAt) || fresh.FinalConsumptionHash != p.FinalConsumptionHash {
				t.Error("fresh observation lost binding", e)
			}
			if lock, e := lockRuntimeMutation(m.dir); e == nil {
				lock.Close()
				t.Error("guard dropped admission lock")
			}
			if i == 0 {
				return errors.New("secret-server-payload")
			}
			return nil
		})
		if (i == 0 && err != ErrRuntimeMutationBlocked) || (i == 1 && err != nil) {
			t.Fatal(err)
		}
	}
	if _, err = escaped.Proof(context.Background()); err == nil {
		t.Fatal("escaped guard remained active")
	}
	if _, err = escaped.PinProof(context.Background()); err == nil {
		t.Fatal("escaped guard pinned again")
	}
	if _, err = copied.Proof(context.Background()); err == nil {
		t.Fatal("copied guard retained expired owner")
	}
	if _, err = copied.PinProof(context.Background()); err == nil {
		t.Fatal("copied guard pinned after callback")
	}
	var zero RuntimeRecoveryFinalizationGuard
	if _, err = zero.Proof(context.Background()); err == nil {
		t.Fatal("zero guard accepted")
	}
	data, err := readRemovalFile(filepath.Join(e.receiptDir, runtimeRecoveryFinalSubmissionFile), 8192, true)
	want, _ := json.Marshal(first)
	if err != nil || !bytes.Equal(data, want) {
		t.Fatal("submission not retained exactly", err)
	}
	after, _ := os.ReadFile(filepath.Join(e.receiptDir, runtimeRecoveryFinalConsumptionFile))
	if !bytes.Equal(before, after) {
		t.Fatal("pin changed consumption digest")
	}
	bad := b
	bad.ManagerURL = "https://foreign.invalid"
	if m.WithSubmission(context.Background(), bad, markerDigest(wire), func(*RuntimeRecoveryFinalizationGuard) error { t.Error("foreign binding invoked callback"); return nil }) == nil {
		t.Fatal("foreign binding accepted")
	}
}

func TestRuntimeMutationFinalSubmissionCloseWaits(t *testing.T) {
	e, a, b, auth := finalizationFixture(t)
	wire, _ := json.Marshal(auth)
	m, err := BeginRuntimeRecoveryFinalization(context.Background(), e.receiptDir, b, wire, e.activePath, a)
	if err != nil {
		t.Fatal(err)
	}
	defer m.Close()
	entered, release, done := make(chan struct{}), make(chan struct{}), make(chan error, 1)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		data, _ := io.ReadAll(r.Body)
		saved, e := readRemovalFile(filepath.Join(m.dir, runtimeRecoveryFinalSubmissionFile), 8192, true)
		if e != nil || !bytes.Equal(data, saved) {
			t.Error("HTTP started before exact durable pin")
		}
		close(entered)
		<-release
		var p RuntimeRecoveryFinalizationProof
		if json.Unmarshal(data, &p) != nil {
			t.Error("invalid pinned HTTP proof")
		}
		json.NewEncoder(w).Encode(receiptForFinalizationProof(p))
	}))
	defer srv.Close()
	unblock := func() {
		select {
		case <-release:
		default:
			close(release)
		}
	}
	defer unblock()
	go func() {
		done <- m.WithSubmission(context.Background(), b, markerDigest(wire), func(g *RuntimeRecoveryFinalizationGuard) error {
			p, e := g.PinProof(context.Background())
			if e != nil {
				return e
			}
			data, _ := json.Marshal(p)
			h := &http.Client{Timeout: 2 * time.Second}
			resp, e := h.Post(srv.URL, "application/json", bytes.NewReader(data))
			if e != nil {
				return e
			}
			var receipt RuntimeRecoveryFinalizationReceipt
			decodeErr := json.NewDecoder(resp.Body).Decode(&receipt)
			resp.Body.Close()
			if decodeErr != nil {
				return decodeErr
			}
			return g.RecordReceipt(context.Background(), receipt)
		})
	}()
	select {
	case <-entered:
	case <-time.After(2 * time.Second):
		t.Fatal("HTTP callback did not start")
	}
	closed := make(chan error, 1)
	go func() { closed <- m.Close() }()
	select {
	case <-closed:
		t.Fatal("Close interrupted in-flight submission")
	case <-time.After(25 * time.Millisecond):
	}
	if lock, e := lockRuntimeMutation(e.receiptDir); e == nil {
		lock.Close()
		t.Fatal("in-flight close dropped flock")
	}
	unblock()
	if err = <-done; err != nil {
		t.Fatal(err)
	}
	if err = <-closed; err != nil {
		t.Fatal(err)
	}
	if runtimeMarkerAbsent(filepath.Join(e.receiptDir, runtimeRecoveryFinalSubmissionFile)) || runtimeMarkerAbsent(filepath.Join(e.receiptDir, runtimeRecoveryFinalConsumptionFile)) {
		t.Fatal("Close erased durable state")
	}
	if runtimeMarkerAbsent(filepath.Join(e.receiptDir, runtimeRecoveryFinalAcknowledgementFile)) {
		t.Fatal("HTTP acknowledgement was not durable before Close")
	}
}

func TestRuntimeMutationFinalSubmissionPoisoned(t *testing.T) {
	for _, mode := range []string{"tampered", "deleted", "runtime", "cancelled"} {
		t.Run(mode, func(t *testing.T) {
			e, a, b, auth := finalizationFixture(t)
			wire, _ := json.Marshal(auth)
			m, err := BeginRuntimeRecoveryFinalization(context.Background(), e.receiptDir, b, wire, e.activePath, a)
			if err != nil {
				t.Fatal(err)
			}
			defer m.Close()
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			err = m.WithSubmission(ctx, b, markerDigest(wire), func(g *RuntimeRecoveryFinalizationGuard) error {
				if _, e := g.PinProof(ctx); e != nil {
					return e
				}
				switch mode {
				case "tampered":
					return os.WriteFile(filepath.Join(m.dir, runtimeRecoveryFinalSubmissionFile), []byte("{}"), 0600)
				case "deleted":
					return os.Remove(filepath.Join(m.dir, runtimeRecoveryFinalSubmissionFile))
				case "runtime":
					return os.WriteFile(e.activePath, []byte("{}"), 0600)
				case "cancelled":
					cancel()
				}
				return nil
			})
			if err == nil {
				t.Fatal("drift/expiry accepted after callback")
			}
			if _, err = m.Proof(context.Background()); err == nil {
				t.Fatal("poisoned owner revived")
			}
			if runtimeMarkerAbsent(filepath.Join(m.dir, runtimeRecoveryFinalConsumptionFile)) || runtimeMarkerAbsent(filepath.Join(m.dir, "mutation-inflight.json")) {
				t.Fatal("poison cleared fence")
			}
		})
	}
}

func receiptForFinalizationProof(p RuntimeRecoveryFinalizationProof) RuntimeRecoveryFinalizationReceipt {
	wire, _ := json.Marshal(p)
	hash := markerDigest(wire)
	return RuntimeRecoveryFinalizationReceipt{1, true, p.OperationID, p.AuthorizationHash, p.FinalConsumptionHash, hash, hash}
}
