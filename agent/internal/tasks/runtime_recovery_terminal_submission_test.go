package tasks

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"
)

func terminalSubmissionFixture(t *testing.T) (CredentialRemovalExecutor, *removalAdapter, RuntimeRecoveryGrantBinding, RuntimeRecoveryReleaseAuthorization, *RuntimeRecoveryFinalization) {
	t.Helper()
	e, a, b, auth, m := terminalFixture(t)
	if _, err := m.RetirePrepared(context.Background()); err != nil {
		t.Fatal(err)
	}
	return e, a, b, auth, m
}
func receiptForTerminalProof(p RuntimeRecoveryTerminalProof) RuntimeRecoveryTerminalReceipt {
	wire, _ := json.Marshal(p)
	hash := markerDigest(wire)
	return RuntimeRecoveryTerminalReceipt{1, true, p.OperationID, p.TerminalHash, hash, hash}
}
func TestRuntimeMutationTerminalSubmissionGolden(t *testing.T) {
	data, err := os.ReadFile("../../../testdata/recovery-terminal-evidence-v1.json")
	var v struct{ Wire, SHA256 string }
	if err != nil || json.Unmarshal(data, &v) != nil {
		t.Fatal("fixture", err)
	}
	var p RuntimeRecoveryTerminalProof
	if json.Unmarshal([]byte(v.Wire), &p) != nil {
		t.Fatal("proof")
	}
	wire, err := json.Marshal(p)
	if err != nil || string(wire) != v.Wire || markerDigest(wire) != v.SHA256 {
		t.Fatal("Manager/Agent terminal wire drift", err)
	}
	r := receiptForTerminalProof(p)
	if !terminalReceiptMatches(r, p, wire) {
		t.Fatal("receipt")
	}
	data, err = os.ReadFile("../../../testdata/recovery-terminal-acknowledgement-v1.json")
	if err != nil || json.Unmarshal(data, &v) != nil {
		t.Fatal("receipt fixture", err)
	}
	ack, err := json.Marshal(r)
	if err != nil || string(ack) != v.Wire || markerDigest(ack) != v.SHA256 {
		t.Fatal("Manager/Agent receipt wire/hash drift", err)
	}
}
func TestRuntimeMutationTerminalSubmissionDurability(t *testing.T) {
	_, a, b, auth, m := terminalSubmissionFixture(t)
	defer m.Close()
	var escaped *RuntimeRecoveryTerminalGuard
	var copied RuntimeRecoveryTerminalGuard
	var frozen RuntimeRecoveryTerminalProof
	var pin, ack []byte
	for i := 0; i < 2; i++ {
		err := m.WithTerminalSubmission(context.Background(), b, m.authorizationHash, func(g *RuntimeRecoveryTerminalGuard) error {
			escaped = g
			copied = *g
			p, e := g.PinProof(context.Background())
			if e != nil {
				return e
			}
			wire, _ := json.Marshal(p)
			if p.Purpose != runtimeRecoveryTerminalEvidencePurpose || p.AgentCredentialGeneration != b.AgentCredentialGeneration || p.AuthorizationHash != m.authorizationHash || p.TerminalHash != markerDigest(m.terminal) || p.IntentHash != markerDigest(m.retirement) || p.FinalProofHash != markerDigest(m.submission) || p.FinalReceiptHash != markerDigest(m.acknowledgement) || p.FinalConsumptionHash != m.frozen.FinalConsumptionHash || p.Observation != auth.Expected {
				t.Fatal("projection bindings")
			}
			if i == 0 {
				frozen = p
				pin = wire
			} else if p != frozen || !bytes.Equal(pin, wire) {
				t.Fatal("repeat changed pinned proof")
			}
			if e = g.RecordReceipt(context.Background(), receiptForTerminalProof(p)); e != nil {
				return e
			}
			ack, _ = json.Marshal(receiptForTerminalProof(p))
			return nil
		})
		if err != nil {
			t.Fatal(err)
		}
		for _, g := range []*RuntimeRecoveryTerminalGuard{escaped, &copied, nil, {}} {
			if _, e := g.Proof(context.Background()); e != ErrRuntimeMutationBlocked {
				t.Fatal("escaped/empty proof guard")
			}
			if _, e := g.PinProof(context.Background()); e != ErrRuntimeMutationBlocked {
				t.Fatal("escaped/empty pin guard")
			}
			if e := g.RecordReceipt(context.Background(), RuntimeRecoveryTerminalReceipt{}); e != ErrRuntimeMutationBlocked {
				t.Fatal("escaped/empty receipt guard")
			}
		}
	}
	for name, want := range map[string][]byte{runtimeRecoveryTerminalSubmissionFile: pin, runtimeRecoveryTerminalAcknowledgementFile: ack} {
		data, e := readRemovalFile(filepath.Join(m.dir, name), 8192, true)
		if e != nil || !bytes.Equal(data, want) {
			t.Fatal("durable private state", name, e)
		}
	}
	if a.restarts != 1 || a.validations != 0 {
		t.Fatal("submission mutated runtime")
	}
	if lock, e := lockRuntimeMutation(m.dir); e == nil {
		lock.Close()
		t.Fatal("submission released flock")
	}
	m.Close()
	assertTerminalArchive(t, m.dir, b)
}
func TestRuntimeMutationTerminalSubmissionRejects(t *testing.T) {
	for _, mode := range []string{"unretired", "closed", "cancelled", "expired", "backwards", "binding", "authorization", "runtime", "pin_preexisting", "valid_pin_preexisting", "pin_symlink", "pin_hardlink", "pin_tamper", "ack_preexisting", "ack_without_pin", "ack_symlink", "bad_receipt", "receipt_before_pin", "drift_after_pin", "drift_after_receipt", "pin_permissions", "ack_directory", "callback_failure"} {
		t.Run(mode, func(t *testing.T) {
			e, _, b, auth, m := terminalFixture(t)
			defer m.Close()
			if mode != "unretired" {
				if _, err := m.RetirePrepared(context.Background()); err != nil {
					t.Fatal(err)
				}
			}
			ctx := context.Background()
			hash := m.authorizationHash
			clock := time.Now().UTC()
			m.now = func() time.Time { return clock }
			switch mode {
			case "closed":
				m.Close()
			case "cancelled":
				c, cancel := context.WithCancel(ctx)
				cancel()
				ctx = c
			case "expired":
				clock = auth.ExpiresAt
			case "backwards":
				clock = clock.Add(-time.Second)
			case "binding":
				b.AgentCredentialGeneration++
			case "authorization":
				hash = strings.Repeat("f", 64)
			case "runtime":
				os.WriteFile(e.activePath, []byte("{}"), 0600)
			case "pin_preexisting":
				os.WriteFile(filepath.Join(m.dir, runtimeRecoveryTerminalSubmissionFile), []byte("{}"), 0600)
			case "valid_pin_preexisting":
				p, err := terminalProofProjection(m.terminal, m.frozen, m.binding)
				if err != nil {
					t.Fatal(err)
				}
				wire, _ := json.Marshal(p)
				os.WriteFile(filepath.Join(m.dir, runtimeRecoveryTerminalSubmissionFile), wire, 0600)
			case "pin_symlink":
				os.Symlink(filepath.Join(m.dir, runtimeRecoveryTerminalFile), filepath.Join(m.dir, runtimeRecoveryTerminalSubmissionFile))
			case "ack_preexisting", "ack_without_pin":
				os.WriteFile(filepath.Join(m.dir, runtimeRecoveryTerminalAcknowledgementFile), []byte("{}"), 0600)
			case "ack_symlink":
				os.Symlink(filepath.Join(m.dir, runtimeRecoveryTerminalFile), filepath.Join(m.dir, runtimeRecoveryTerminalAcknowledgementFile))
			}
			err := m.WithTerminalSubmission(ctx, b, hash, func(g *RuntimeRecoveryTerminalGuard) error {
				if mode == "receipt_before_pin" {
					return g.RecordReceipt(ctx, RuntimeRecoveryTerminalReceipt{})
				}
				p, x := g.PinProof(ctx)
				if x != nil {
					return x
				}
				switch mode {
				case "pin_hardlink":
					os.Link(filepath.Join(m.dir, runtimeRecoveryTerminalSubmissionFile), filepath.Join(m.dir, "other-link"))
				case "pin_tamper":
					os.WriteFile(filepath.Join(m.dir, runtimeRecoveryTerminalSubmissionFile), []byte("{}"), 0600)
				case "drift_after_pin":
					os.WriteFile(e.activePath, []byte("{}"), 0600)
				case "pin_permissions":
					// Existing file with wrong private metadata is never overwritten/adopted.
					os.Chmod(filepath.Join(m.dir, runtimeRecoveryTerminalSubmissionFile), 0644)
				case "ack_directory":
					os.Mkdir(filepath.Join(m.dir, runtimeRecoveryTerminalAcknowledgementFile), 0700)
				case "callback_failure":
					return errors.New("secret-payload")
				}
				r := receiptForTerminalProof(p)
				if mode == "bad_receipt" {
					r.TerminalHash = strings.Repeat("f", 64)
				}
				x = g.RecordReceipt(ctx, r)
				if mode == "drift_after_receipt" {
					os.WriteFile(e.activePath, []byte("{}"), 0600)
				}
				return x
			})
			if err != ErrRuntimeMutationBlocked {
				t.Fatal("unsafe terminal submission", err)
			}
			if runtimeMarkerAbsent(filepath.Join(m.dir, "mutation-inflight.json")) || (mode != "unretired" && runtimeMarkerAbsent(filepath.Join(m.dir, runtimeRecoveryTerminalFile))) {
				t.Fatal("refusal cleared fence")
			}
		})
	}
}
func TestRuntimeMutationTerminalReceiptBindings(t *testing.T) {
	for _, mode := range []string{"schema", "recorded", "operation", "terminal", "proof", "request"} {
		t.Run(mode, func(t *testing.T) {
			_, _, b, _, m := terminalSubmissionFixture(t)
			defer m.Close()
			if err := m.WithTerminalSubmission(context.Background(), b, m.authorizationHash, func(g *RuntimeRecoveryTerminalGuard) error {
				p, e := g.PinProof(context.Background())
				if e != nil {
					return e
				}
				r := receiptForTerminalProof(p)
				switch mode {
				case "schema":
					r.SchemaVersion = 2
				case "recorded":
					r.Recorded = false
				case "operation":
					r.OperationID = b.AgentID
				case "terminal":
					r.TerminalHash = strings.Repeat("f", 64)
				case "proof":
					r.ProofHash = strings.Repeat("f", 64)
				case "request":
					r.RequestHash = strings.Repeat("f", 64)
				}
				if g.RecordReceipt(context.Background(), r) != ErrRuntimeMutationBlocked {
					t.Fatal("foreign receipt")
				}
				if !runtimeMarkerAbsent(filepath.Join(m.dir, runtimeRecoveryTerminalAcknowledgementFile)) {
					t.Fatal("bad receipt retained")
				}
				return g.RecordReceipt(context.Background(), receiptForTerminalProof(p))
			}); err != nil {
				t.Fatal(err)
			}
		})
	}
}
func TestRuntimeMutationTerminalSubmissionConcurrency(t *testing.T) {
	_, _, b, _, m := terminalSubmissionFixture(t)
	defer m.Close()
	var wg sync.WaitGroup
	for range 2 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			if err := m.WithTerminalSubmission(context.Background(), b, m.authorizationHash, func(g *RuntimeRecoveryTerminalGuard) error {
				p, e := g.PinProof(context.Background())
				if e != nil {
					return e
				}
				return g.RecordReceipt(context.Background(), receiptForTerminalProof(p))
			}); err != nil {
				t.Error(err)
			}
		}()
	}
	wg.Wait()
}
func TestRuntimeMutationTerminalSubmissionCloseWaits(t *testing.T) {
	_, _, b, _, m := terminalSubmissionFixture(t)
	entered, finish, closed := make(chan struct{}), make(chan struct{}), make(chan struct{})
	done := make(chan error, 1)
	go func() {
		done <- m.WithTerminalSubmission(context.Background(), b, m.authorizationHash, func(g *RuntimeRecoveryTerminalGuard) error {
			close(entered)
			<-finish
			p, e := g.PinProof(context.Background())
			if e != nil {
				return e
			}
			return g.RecordReceipt(context.Background(), receiptForTerminalProof(p))
		})
	}()
	<-entered
	go func() { m.Close(); close(closed) }()
	select {
	case <-closed:
		t.Fatal("Close interrupted live submission")
	case <-time.After(20 * time.Millisecond):
	}
	close(finish)
	if err := <-done; err != nil {
		t.Fatal(err)
	}
	<-closed
	assertTerminalArchive(t, m.dir, b)
}

func TestRuntimeMutationTerminalConfirmationHistoryRejects(t *testing.T) {
	for _, mode := range []string{"pin_tamper", "pin_permissions", "pin_hardlink", "pin_symlink", "ack_tamper", "ack_duplicate", "ack_alias", "ack_unknown", "ack_permissions", "ack_hardlink", "ack_without_pin", "changed_during_read"} {
		t.Run(mode, func(t *testing.T) {
			_, _, b, _, m := terminalSubmissionFixture(t)
			if err := m.WithTerminalSubmission(context.Background(), b, m.authorizationHash, func(g *RuntimeRecoveryTerminalGuard) error {
				p, e := g.PinProof(context.Background())
				if e != nil {
					return e
				}
				return g.RecordReceipt(context.Background(), receiptForTerminalProof(p))
			}); err != nil {
				t.Fatal(err)
			}
			m.Close()
			pinPath, ackPath := filepath.Join(m.dir, runtimeRecoveryTerminalSubmissionFile), filepath.Join(m.dir, runtimeRecoveryTerminalAcknowledgementFile)
			ack, _ := os.ReadFile(ackPath)
			switch mode {
			case "pin_tamper":
				os.WriteFile(pinPath, []byte("{}"), 0600)
			case "pin_permissions":
				os.Chmod(pinPath, 0644)
			case "pin_hardlink":
				os.Link(pinPath, filepath.Join(m.dir, "other-pin"))
			case "pin_symlink":
				os.Remove(pinPath)
				os.Symlink(filepath.Join(m.dir, runtimeRecoveryTerminalFile), pinPath)
			case "ack_tamper":
				os.WriteFile(ackPath, []byte("{}"), 0600)
			case "ack_duplicate":
				os.WriteFile(ackPath, append([]byte(`{"recorded":true,`), ack[1:]...), 0600)
			case "ack_alias":
				os.WriteFile(ackPath, bytes.Replace(ack, []byte("proofHash"), []byte("ProofHash"), 1), 0600)
			case "ack_unknown":
				os.WriteFile(ackPath, append([]byte(`{"secret":"payload",`), ack[1:]...), 0600)
			case "ack_permissions":
				os.Chmod(ackPath, 0644)
			case "ack_hardlink":
				os.Link(ackPath, filepath.Join(m.dir, "other-ack"))
			case "ack_without_pin":
				os.Remove(pinPath)
			}
			called := false
			err := WithHistoricalRecoveryFinalization(context.Background(), m.dir, b, func(RuntimeRecoveryFinalizationProof) error {
				called = true
				if mode == "changed_during_read" {
					os.WriteFile(ackPath, []byte("{}"), 0600)
				}
				return nil
			})
			if err != ErrRuntimeMutationBlocked || called != (mode == "changed_during_read") {
				t.Fatal("unsafe terminal confirmation history", err, called)
			}
		})
	}
}
