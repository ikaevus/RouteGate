package db

import (
	"bytes"
	"encoding/json"
	"os"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/ikaevus/routegate/backend/internal/revocations"
)

type recoveryAdmissionReleaseFixture struct {
	recoveryCleanedCommitFixture
	release revocations.RecoveryAdmissionReleaseProof
}

func newRecoveryAdmissionReleaseFixture(t *testing.T) recoveryAdmissionReleaseFixture {
	t.Helper()
	f := newRecoveryCleanedCommitFixture(t, 0)
	cleanedWire, _ := json.Marshal(f.cleaned)
	ack, err := f.svc.AcceptRecoveryCleanedCommit(f.ctx, f.token, f.cleaned.OperationID, cleanedWire)
	if err != nil {
		t.Fatal(err)
	}
	ackWire, _ := json.Marshal(ack)
	var now time.Time
	if err := f.pool.QueryRow(f.ctx, `SELECT clock_timestamp()`).Scan(&now); err != nil {
		t.Fatal(err)
	}
	p := revocations.RecoveryAdmissionReleaseProof{
		SchemaVersion: 1, Purpose: revocations.RecoveryAdmissionReleasePurpose,
		OperationID: f.cleaned.OperationID, AgentCredentialGeneration: f.cleaned.AgentCredentialGeneration,
		AuthorizationHash: f.cleaned.AuthorizationHash,
		CleanedProofHash:  ack.ProofHash, CleanedReceiptHash: recoveryTestHash(ackWire),
		CleanedArchiveHash: strings.Repeat("4", 64),
		ReleasedAt:         now.UTC().Add(time.Nanosecond), ObservedAt: now.UTC().Add(321 * time.Nanosecond),
		Observation: f.cleaned.Observation,
	}
	return recoveryAdmissionReleaseFixture{f, p}
}

func TestRecoveryAdmissionReleaseEvidence(t *testing.T) {
	f := newRecoveryAdmissionReleaseFixture(t)
	wire, _ := json.Marshal(f.release)
	before := f.snapshot(t)
	if _, err := f.svc.VerifyRecoveryAdmissionRelease(f.ctx, f.token, f.release.OperationID, wire); err == nil {
		t.Fatal("unrecorded release proof accepted")
	}
	var wg sync.WaitGroup
	results := make(chan revocations.RecoveryAdmissionReleaseReceipt, 2)
	for i := 0; i < 2; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			r, err := f.svc.AcceptRecoveryAdmissionRelease(f.ctx, f.token, f.release.OperationID, wire)
			if err != nil {
				t.Error(err)
			}
			results <- r
		}()
	}
	wg.Wait()
	a, b := <-results, <-results
	if !a.Recorded || a != b || a.ProofHash != recoveryTestHash(wire) || a.RequestHash != a.ProofHash {
		t.Fatal("concurrent exact retry/receipt mismatch")
	}
	verified, err := f.svc.VerifyRecoveryAdmissionRelease(f.ctx, f.token, f.release.OperationID, wire)
	if err != nil || verified != a {
		t.Fatal("historical readback", err)
	}
	var saved, hash, state string
	var accepted time.Time
	if err := f.pool.QueryRow(f.ctx, `SELECT e.proof,e.proof_hash,e.accepted_at,p.state
		FROM runtime_recovery_admission_release_evidence e
		JOIN runtime_recovery_preparations p ON p.id=e.preparation_id`).Scan(&saved, &hash, &accepted, &state); err != nil || saved != string(wire) || hash != a.ProofHash || state != "prepared" {
		t.Fatal("evidence replaced or reservation completed", err)
	}
	if f.snapshot(t) != before {
		t.Fatal("evidence storage changed predecessors")
	}
	if _, err := f.svc.AcceptRecoveryAdmissionRelease(f.ctx, f.token, f.release.OperationID, wire); err != nil {
		t.Fatal("exact retry", err)
	}
	var after time.Time
	if err := f.pool.QueryRow(f.ctx, `SELECT accepted_at FROM runtime_recovery_admission_release_evidence`).Scan(&after); err != nil || !after.Equal(accepted) {
		t.Fatal("retry renewed historical evidence", err)
	}
	var pretty bytes.Buffer
	json.Indent(&pretty, wire, "", "  ")
	if _, err := f.svc.AcceptRecoveryAdmissionRelease(f.ctx, f.token, f.release.OperationID, pretty.Bytes()); err == nil {
		t.Fatal("noncanonical request accepted")
	}
	for _, q := range []string{`UPDATE runtime_recovery_admission_release_evidence SET proof=proof`, `DELETE FROM runtime_recovery_admission_release_evidence`} {
		if _, err := f.pool.Exec(f.ctx, q); err == nil {
			t.Fatal("mutable post-release history")
		}
	}
	down, err := os.ReadFile("../../migrations/000172_recovery_admission_release_evidence.down.sql")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := f.pool.Exec(f.ctx, string(down)); err == nil {
		t.Fatal("downgrade erased post-release history")
	}
	changed := f.release
	changed.CleanedArchiveHash = strings.Repeat("5", 64)
	changedWire, _ := json.Marshal(changed)
	if _, err := f.svc.AcceptRecoveryAdmissionRelease(f.ctx, f.token, changed.OperationID, changedWire); err == nil {
		t.Fatal("changed release proof replaced existing evidence")
	}
}

func TestRecoveryAdmissionReleaseRejects(t *testing.T) {
	for _, mode := range []string{"token", "path", "generation", "authorization", "cleaned_hash", "cleaned_receipt", "archive_hash", "release_before_cleaned", "backward_time", "expired", "future", "wrong_agent", "offline", "stale", "fencing", "revoked_session"} {
		t.Run(mode, func(t *testing.T) {
			f := newRecoveryAdmissionReleaseFixture(t)
			p := f.release
			token, operation := f.token, p.OperationID
			switch mode {
			case "token":
				token = "invalid-agent-token"
			case "path":
				operation = f.actor.ID
			case "generation":
				p.AgentCredentialGeneration++
			case "authorization":
				p.AuthorizationHash = strings.Repeat("d", 64)
			case "cleaned_hash":
				p.CleanedProofHash = strings.Repeat("e", 64)
			case "cleaned_receipt":
				p.CleanedReceiptHash = strings.Repeat("e", 64)
			case "archive_hash":
				p.CleanedArchiveHash = "bad"
			case "release_before_cleaned":
				p.ReleasedAt = f.cleaned.ObservedAt.Add(-time.Nanosecond)
			case "backward_time":
				p.ObservedAt = p.ReleasedAt.Add(-time.Nanosecond)
			case "expired":
				p.ObservedAt = f.authorization.Authorization.ExpiresAt
			case "future":
				p.ObservedAt = f.authorization.Authorization.ExpiresAt.Add(-time.Nanosecond)
			case "wrong_agent":
				p.Observation.AgentID = f.actor.ID
			case "offline":
				f.exec(t, `UPDATE agents SET status='offline'`)
			case "stale":
				f.exec(t, `UPDATE agents SET last_authenticated_heartbeat_at=clock_timestamp()-interval '10 minutes'`)
			case "fencing":
				f.exec(t, `UPDATE agents SET current_runtime_mutation_fencing=false`)
			case "revoked_session":
				f.exec(t, `UPDATE auth_sessions SET revoked_at=clock_timestamp() WHERE id=$1::uuid`, f.actor.SessionID)
			}
			wire, _ := json.Marshal(p)
			if _, err := f.svc.AcceptRecoveryAdmissionRelease(f.ctx, token, operation, wire); err == nil {
				t.Fatal("unsafe post-release evidence accepted", mode)
			}
			var count int
			if err := f.pool.QueryRow(f.ctx, `SELECT count(*) FROM runtime_recovery_admission_release_evidence`).Scan(&count); err != nil || count != 0 {
				t.Fatal("rejected statement persisted", err)
			}
		})
	}
}
