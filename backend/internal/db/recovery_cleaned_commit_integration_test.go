package db

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"os"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/ikaevus/routegate/backend/internal/revocations"
	"github.com/jackc/pgx/v5/pgconn"
)

type recoveryCleanedCommitFixture struct {
	recoveryRetentionFixture
	cleaned revocations.RecoveryCleanedCommitProof
}

func newRecoveryCleanedCommitFixture(t *testing.T, lifetime time.Duration) recoveryCleanedCommitFixture {
	return cleanedCommitFixtureWithRetention(t, lifetime, true)
}
func cleanedCommitFixtureWithRetention(t *testing.T, lifetime time.Duration, record bool) recoveryCleanedCommitFixture {
	t.Helper()
	f := newRecoveryRetentionFixture(t, lifetime)
	wire, _ := json.Marshal(f.retention)
	receipt := revocations.RecoveryRetentionReceipt{SchemaVersion: 1, Recorded: true, OperationID: f.retention.OperationID, RetentionHash: f.retention.RetentionHash, ProofHash: recoveryTestHash(wire), RequestHash: recoveryTestHash(wire)}
	if record {
		var err error
		receipt, err = f.svc.AcceptRecoveryRetention(f.ctx, f.token, f.retention.OperationID, wire)
		if err != nil {
			t.Fatal(err)
		}
	}
	ack, _ := json.Marshal(receipt)
	var now time.Time
	if err := f.pool.QueryRow(f.ctx, `SELECT clock_timestamp()`).Scan(&now); err != nil {
		t.Fatal(err)
	}
	return recoveryCleanedCommitFixture{f, revocations.RecoveryCleanedCommitProof{SchemaVersion: 1, Purpose: revocations.RecoveryCleanedCommitPurpose, OperationID: f.retention.OperationID, AgentCredentialGeneration: f.retention.AgentCredentialGeneration, AuthorizationHash: f.retention.AuthorizationHash, RetentionProofHash: receipt.ProofHash, RetentionReceiptHash: recoveryTestHash(ack), ReleaseIntentHash: f.prepared.ReleaseIntentHash, CleanedIntentHash: strings.Repeat("1", 64), ManifestHash: strings.Repeat("2", 64), MarkerHash: strings.Repeat("3", 64), IntentAt: f.retention.ObservedAt.Add(time.Nanosecond), ObservedAt: now.UTC().Add(321 * time.Nanosecond), Observation: f.retention.Observation}}
}
func (f recoveryCleanedCommitFixture) snapshot(t *testing.T) string {
	t.Helper()
	var retained string
	if err := f.pool.QueryRow(f.ctx, `SELECT COALESCE(jsonb_agg(to_jsonb(e))::text,'null') FROM runtime_recovery_retention_evidence e`).Scan(&retained); err != nil {
		t.Fatal(err)
	}
	return f.recoveryRetentionFixture.snapshot(t) + retained
}
func (f recoveryCleanedCommitFixture) insert(p revocations.RecoveryCleanedCommitProof) error {
	wire, _ := json.Marshal(p)
	_, err := f.pool.Exec(f.ctx, `INSERT INTO runtime_recovery_cleaned_commit_evidence(preparation_id,proof,proof_hash,observed_at) VALUES($1::uuid,$2::text,$3,($2::text::jsonb->>'observedAt')::timestamptz)`, f.cleaned.OperationID, string(wire), recoveryTestHash(wire))
	return err
}
func TestRecoveryCleanedCommitEvidence(t *testing.T) {
	f := newRecoveryCleanedCommitFixture(t, 0)
	wire, _ := json.Marshal(f.cleaned)
	before := f.snapshot(t)
	if _, err := f.svc.VerifyRecoveryCleanedCommit(f.ctx, f.token, f.cleaned.OperationID, wire); err == nil {
		t.Fatal("unrecorded cleaned commit statement accepted")
	}
	results := make(chan revocations.RecoveryCleanedCommitReceipt, 2)
	var wg sync.WaitGroup
	for range 2 {
		wg.Go(func() {
			r, e := f.svc.AcceptRecoveryCleanedCommit(f.ctx, f.token, f.cleaned.OperationID, wire)
			if e != nil {
				t.Error(e)
			}
			results <- r
		})
	}
	wg.Wait()
	a, b := <-results, <-results
	if !a.Recorded || a != b || a.ProofHash != recoveryTestHash(wire) || a.RequestHash != a.ProofHash || a.CleanedIntentHash != f.cleaned.CleanedIntentHash {
		t.Fatal("receipt binding")
	}
	var saved string
	var accepted time.Time
	if err := f.pool.QueryRow(f.ctx, `SELECT proof,accepted_at FROM runtime_recovery_cleaned_commit_evidence`).Scan(&saved, &accepted); err != nil || saved != string(wire) {
		t.Fatal("immutable wire", err)
	}
	var pretty bytes.Buffer
	json.Indent(&pretty, wire, "", "  ")
	r, err := f.svc.AcceptRecoveryCleanedCommit(f.ctx, f.token, f.cleaned.OperationID, pretty.Bytes())
	if err != nil || r.ProofHash != a.ProofHash || r.RequestHash != recoveryTestHash(pretty.Bytes()) {
		t.Fatal("retry", err)
	}
	verified, err := f.svc.VerifyRecoveryCleanedCommit(f.ctx, f.token, f.cleaned.OperationID, pretty.Bytes())
	if err != nil || verified != r {
		t.Fatal("readback", err)
	}
	var after time.Time
	if err = f.pool.QueryRow(f.ctx, `SELECT accepted_at FROM runtime_recovery_cleaned_commit_evidence`).Scan(&after); err != nil || !after.Equal(accepted) {
		t.Fatal("retry renewed evidence", err)
	}
	for _, q := range []string{`UPDATE runtime_recovery_cleaned_commit_evidence SET proof=proof`, `DELETE FROM runtime_recovery_cleaned_commit_evidence`} {
		if _, err = f.pool.Exec(f.ctx, q); err == nil {
			t.Fatal("mutable history")
		}
	}
	down, err := os.ReadFile("../../migrations/000171_recovery_cleaned_commit_evidence.down.sql")
	if err != nil {
		t.Fatal(err)
	}
	if _, err = f.pool.Exec(f.ctx, string(down)); err == nil {
		t.Fatal("downgrade erased history")
	}
	if _, err = f.svc.CancelRuntimeRecovery(f.ctx, f.cleaned.OperationID, f.actor.ID); err == nil {
		t.Fatal("statement storage released reservation")
	}
	if f.snapshot(t) != before {
		t.Fatal("statement storage mutated protected state")
	}
	p := f.cleaned
	p.CleanedIntentHash = strings.Repeat("e", 64)
	changed, _ := json.Marshal(p)
	if _, err = f.svc.AcceptRecoveryCleanedCommit(f.ctx, f.token, p.OperationID, changed); err == nil {
		t.Fatal("conflicting evidence replaced")
	}
	if _, err = f.svc.VerifyRecoveryCleanedCommit(f.ctx, f.token, p.OperationID, changed); err == nil {
		t.Fatal("conflicting evidence verified")
	}
}

func TestRecoveryCleanedCommitRejects(t *testing.T) {
	for _, mode := range []string{"token", "path", "generation", "authorization", "retention_proof", "retention_receipt", "release_intent", "intent_hash", "manifest_hash", "marker_hash", "before_retention", "before_retention_nanosecond", "before_intent_nanosecond", "future_intent", "future", "expired", "agent", "server", "task", "observation", "offline", "stale", "heartbeat_generation", "fencing", "capability", "observation_capability", "issuer", "disabled_issuer", "service_issuer", "session_shortened", "role", "permission", "missing_retention"} {
		t.Run(mode, func(t *testing.T) {
			f := cleanedCommitFixtureWithRetention(t, 0, mode != "missing_retention")
			p := f.cleaned
			token, operation := f.token, p.OperationID
			switch mode {
			case "token":
				token = "retired-isolated"
			case "path":
				operation = f.actor.ID
			case "generation":
				p.AgentCredentialGeneration++
			case "authorization":
				p.AuthorizationHash = strings.Repeat("d", 64)
			case "retention_proof":
				p.RetentionProofHash = strings.Repeat("e", 64)
			case "retention_receipt":
				p.RetentionReceiptHash = strings.Repeat("e", 64)
			case "release_intent":
				p.ReleaseIntentHash = strings.Repeat("e", 64)
			case "intent_hash":
				p.CleanedIntentHash = "bad"
			case "manifest_hash":
				p.ManifestHash = "bad"
			case "marker_hash":
				p.MarkerHash = "bad"
			case "before_retention":
				p.IntentAt = f.retention.ObservedAt.Add(-time.Microsecond)
			case "before_retention_nanosecond":
				p.IntentAt = f.retention.ObservedAt.Add(-time.Nanosecond)
			case "before_intent_nanosecond":
				p.ObservedAt = p.IntentAt.Add(-time.Nanosecond)
			case "future_intent":
				p.IntentAt = p.ObservedAt.Add(time.Nanosecond)
			case "future":
				p.ObservedAt = f.authorization.Authorization.ExpiresAt.Add(-time.Millisecond)
			case "expired":
				p.ObservedAt = f.authorization.Authorization.ExpiresAt
			case "agent":
				p.Observation.AgentID = f.actor.ID
			case "server":
				p.Observation.ServerID = f.actor.ID
			case "task":
				p.Observation.TaskID = f.actor.ID
			case "observation":
				p.Observation.Witness.ListenerPort++
			case "offline":
				f.exec(t, `UPDATE agents SET status='offline'`)
			case "stale":
				f.exec(t, `UPDATE agents SET last_authenticated_heartbeat_at=clock_timestamp()-interval '10 minutes'`)
			case "heartbeat_generation":
				// Existing schema already prevents forged cross-generation heartbeats.
				if _, err := f.pool.Exec(f.ctx, `UPDATE agents SET last_authenticated_heartbeat_generation=credential_generation+1`); err == nil {
					t.Fatal("cross-generation heartbeat admitted by schema")
				}
				f.exec(t, `UPDATE agents SET last_authenticated_heartbeat_at=NULL,last_authenticated_heartbeat_generation=NULL`)
			case "fencing":
				f.exec(t, `UPDATE agents SET current_runtime_mutation_fencing=false`)
			case "capability":
				f.exec(t, `UPDATE agents SET capabilities=capabilities-'runtimeRecoveryGrantReadV1'`)
			case "observation_capability":
				f.exec(t, `UPDATE agents SET capabilities=capabilities-'runtimeRecoveryObservationV1'`)
			case "issuer":
				f.exec(t, `UPDATE auth_sessions SET revoked_at=clock_timestamp() WHERE id=$1::uuid`, f.actor.SessionID)
			case "disabled_issuer":
				f.exec(t, `UPDATE users SET status='disabled' WHERE id=$1::uuid`, f.actor.ID)
			case "service_issuer":
				f.exec(t, `UPDATE users SET user_type='agent' WHERE id=$1::uuid`, f.actor.ID)
			case "session_shortened":
				f.exec(t, `UPDATE auth_sessions SET expires_at=$1 WHERE id=$2::uuid`, f.authorization.Authorization.ExpiresAt.Add(-time.Millisecond), f.actor.SessionID)
			case "role":
				f.exec(t, `DELETE FROM user_roles WHERE user_id=$1::uuid`, f.actor.ID)
			case "permission":
				f.exec(t, `DELETE FROM role_permissions`)
			}
			// Missing retention has no snapshot row; all other refusals preserve protected state.
			before := ""
			if mode != "missing_retention" {
				before = f.snapshot(t)
			}
			wire, _ := json.Marshal(p)
			if _, err := f.svc.AcceptRecoveryCleanedCommit(f.ctx, token, operation, wire); err == nil {
				t.Fatal("unsafe cleaned commit statement recorded")
			}
			// Independent SQL admission guard must reject the same forged/expired statement.
			if mode != "token" && mode != "path" && f.insert(p) == nil {
				t.Fatal("SQL bypass admitted evidence")
			}
			var n int
			if err := f.pool.QueryRow(f.ctx, `SELECT count(*) FROM runtime_recovery_cleaned_commit_evidence`).Scan(&n); err != nil || n != 0 {
				t.Fatal("partial evidence", err)
			}
			if mode != "missing_retention" && f.snapshot(t) != before {
				t.Fatal("refusal changed protected state")
			}
		})
	}
}

func TestRecoveryCleanedCommitHistoricalReadback(t *testing.T) {
	f := newRecoveryCleanedCommitFixture(t, time.Second)
	wire, _ := json.Marshal(f.cleaned)
	r, err := f.svc.AcceptRecoveryCleanedCommit(f.ctx, f.token, f.cleaned.OperationID, wire)
	if err != nil {
		t.Fatal(err)
	}
	f.exec(t, `SELECT pg_sleep(1.1)`)
	if _, err = f.svc.AcceptRecoveryCleanedCommit(f.ctx, f.token, f.cleaned.OperationID, wire); err == nil {
		t.Fatal("expired retry admitted")
	}
	f.exec(t, `UPDATE auth_sessions SET revoked_at=clock_timestamp() WHERE id=$1::uuid`, f.actor.SessionID)
	f.exec(t, `UPDATE agents SET status='offline',current_runtime_mutation_fencing=false,capabilities='{}',last_authenticated_heartbeat_at=clock_timestamp()-interval '10 minutes'`)
	before := f.snapshot(t)
	got, err := f.svc.VerifyRecoveryCleanedCommit(f.ctx, f.token, f.cleaned.OperationID, wire)
	if err != nil || got != r || f.snapshot(t) != before {
		t.Fatal("historical readback renewed authority", err)
	}
	p := f.cleaned
	p.AgentCredentialGeneration++
	changed, _ := json.Marshal(p)
	if _, err = f.svc.VerifyRecoveryCleanedCommit(f.ctx, f.token, p.OperationID, changed); err == nil {
		t.Fatal("generation changed")
	}
	if _, err = f.svc.VerifyRecoveryCleanedCommit(f.ctx, "retired-isolated", p.OperationID, wire); err == nil {
		t.Fatal("retired token read")
	}
}

func TestRecoveryCleanedCommitRawSQLWire(t *testing.T) {
	f := newRecoveryCleanedCommitFixture(t, 0)
	wire, _ := json.Marshal(f.cleaned)
	base := string(wire)
	for _, bad := range []string{
		`{"schemaVersion":2,` + base[1:],
		strings.Replace(base, `"witness":{"schemaVersion":1`, `"witness":{"schemaVersion":2,"schemaVersion":1`, 1),
		`{"schema\u0056ersion":2,` + base[1:],
		" " + base,
		strings.Replace(base, `"schemaVersion":1`, `"schemaVersion":1.0`, 1),
		strings.Replace(base, `"operationId"`, `"operation\u0049d"`, 1),
		strings.Replace(base, `"listenerPort":443`, `"listenerPort":443.0`, 1),
		strings.Replace(base, `"cleanedIntentHash":`, `"unexpected":`, 1),
		strings.Replace(base, `"intentAt":`, `"IntentAt":`, 1),
	} {
		_, err := f.pool.Exec(f.ctx, `INSERT INTO runtime_recovery_cleaned_commit_evidence(preparation_id,proof,proof_hash,observed_at) VALUES($1::uuid,$2::text,$3,($2::text::jsonb->>'observedAt')::timestamptz)`, f.cleaned.OperationID, bad, recoveryTestHash([]byte(bad)))
		var pe *pgconn.PgError
		if !errors.As(err, &pe) || pe.Code != "P0151" {
			t.Fatal("SQL raw wire accepted", err)
		}
	}
	for _, mismatch := range []string{
		`INSERT INTO runtime_recovery_cleaned_commit_evidence(preparation_id,proof,proof_hash,observed_at) VALUES($1::uuid,$2::text,repeat('0',64),($2::text::jsonb->>'observedAt')::timestamptz)`,
		`INSERT INTO runtime_recovery_cleaned_commit_evidence(preparation_id,proof,proof_hash,observed_at) VALUES($1::uuid,$2::text,encode(digest($2::text,'sha256'),'hex'),clock_timestamp()+interval '1 second')`,
	} {
		if _, err := f.pool.Exec(f.ctx, mismatch, f.cleaned.OperationID, base); err == nil {
			t.Fatal("SQL columns bypassed proof binding")
		}
	}
	if err := f.insert(f.cleaned); err != nil {
		t.Fatal("valid raw SQL rejected", err)
	}
	if r, err := f.svc.VerifyRecoveryCleanedCommit(f.ctx, f.token, f.cleaned.OperationID, wire); err != nil || !r.Recorded {
		t.Fatal("SQL retained evidence unreadable", err)
	}
}

func TestRecoveryCleanedCommitCancelledContext(t *testing.T) {
	f := newRecoveryCleanedCommitFixture(t, 0)
	wire, _ := json.Marshal(f.cleaned)
	ctx, cancel := context.WithCancel(f.ctx)
	cancel()
	if _, err := f.svc.AcceptRecoveryCleanedCommit(ctx, f.token, f.cleaned.OperationID, wire); err == nil {
		t.Fatal("cancelled admission succeeded")
	}
	var n int
	if err := f.pool.QueryRow(f.ctx, `SELECT count(*) FROM runtime_recovery_cleaned_commit_evidence`).Scan(&n); err != nil || n != 0 {
		t.Fatal("cancelled partial evidence", err)
	}
}

func TestRecoveryCleanedCommitRetryAuthorityLoss(t *testing.T) {
	f := newRecoveryCleanedCommitFixture(t, 0)
	wire, _ := json.Marshal(f.cleaned)
	r, err := f.svc.AcceptRecoveryCleanedCommit(f.ctx, f.token, f.cleaned.OperationID, wire)
	if err != nil {
		t.Fatal(err)
	}
	f.exec(t, `DELETE FROM user_roles WHERE user_id=$1::uuid`, f.actor.ID)
	before := f.snapshot(t)
	if _, err = f.svc.AcceptRecoveryCleanedCommit(f.ctx, f.token, f.cleaned.OperationID, wire); err == nil {
		t.Fatal("recorded proof bypassed revoked current authority")
	}
	verified, err := f.svc.VerifyRecoveryCleanedCommit(f.ctx, f.token, f.cleaned.OperationID, wire)
	if err != nil || verified != r {
		t.Fatal("historical storage lost after authority revocation", err)
	}
	if f.snapshot(t) != before {
		t.Fatal("retry/readback changed protected state")
	}
}

func TestRecoveryCleanedCommitDowngradeRace(t *testing.T) {
	f := newRecoveryCleanedCommitFixture(t, 0)
	wire, _ := json.Marshal(f.cleaned)
	down, err := os.ReadFile("../../migrations/000171_recovery_cleaned_commit_evidence.down.sql")
	if err != nil {
		t.Fatal(err)
	}
	tx, err := f.pool.Begin(f.ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer tx.Rollback(f.ctx)
	if _, err = tx.Exec(f.ctx, `INSERT INTO runtime_recovery_cleaned_commit_evidence(preparation_id,proof,proof_hash,observed_at) VALUES($1::uuid,$2::text,$3,($2::text::jsonb->>'observedAt')::timestamptz)`, f.cleaned.OperationID, string(wire), recoveryTestHash(wire)); err != nil {
		t.Fatal(err)
	}
	bounded, cancel := context.WithTimeout(f.ctx, 3*time.Second)
	defer cancel()
	ch := make(chan error, 1)
	go func() { _, e := f.pool.Exec(bounded, string(down)); ch <- e }()
	deadline := time.Now().Add(time.Second)
	for {
		var waiting bool
		if err = f.pool.QueryRow(f.ctx, `SELECT EXISTS(SELECT 1 FROM pg_stat_activity WHERE query LIKE '%recovery_cleaned_commit_evidence_retained%' AND wait_event_type='Lock' AND pid<>pg_backend_pid())`).Scan(&waiting); err != nil {
			t.Fatal(err)
		}
		if waiting {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("downgrade did not serialize with insertion")
		}
		f.exec(t, `SELECT pg_sleep(0.01)`)
	}
	if err = tx.Commit(f.ctx); err != nil {
		t.Fatal(err)
	}
	if err = <-ch; err == nil {
		t.Fatal("raced downgrade discarded committed proof")
	}
	r, err := f.svc.VerifyRecoveryCleanedCommit(f.ctx, f.token, f.cleaned.OperationID, wire)
	if err != nil || !r.Recorded || r.ProofHash != recoveryTestHash(wire) {
		t.Fatal("raced evidence not retained", err)
	}
}

func TestRecoveryCleanedCommitWaitExpiry(t *testing.T) {
	f := newRecoveryCleanedCommitFixture(t, time.Second)
	wire, _ := json.Marshal(f.cleaned)
	tx, err := f.pool.Begin(f.ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer tx.Rollback(f.ctx)
	if _, err = tx.Exec(f.ctx, `SELECT id FROM servers WHERE id=$1::uuid FOR UPDATE`, f.cleaned.Observation.ServerID); err != nil {
		t.Fatal(err)
	}
	ch := make(chan error, 1)
	go func() {
		_, e := f.svc.AcceptRecoveryCleanedCommit(f.ctx, f.token, f.cleaned.OperationID, wire)
		ch <- e
	}()
	// Check actual lock wait before testing absence of global auth locks.
	deadline := time.Now().Add(time.Second)
	for {
		var waiting bool
		if err = f.pool.QueryRow(f.ctx, `SELECT EXISTS(SELECT 1 FROM pg_stat_activity WHERE datname=current_database() AND wait_event_type='Lock' AND pid<>pg_backend_pid())`).Scan(&waiting); err != nil {
			t.Fatal(err)
		}
		if waiting {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("no admission wait")
		}
		f.exec(t, `SELECT pg_sleep(0.01)`)
	}
	short, cancel := context.WithTimeout(f.ctx, 500*time.Millisecond)
	defer cancel()
	if _, err = f.pool.Exec(short, `UPDATE roles SET name=name WHERE code='admin'`); err != nil {
		t.Fatal("global role lock while node wait", err)
	}
	f.exec(t, `SELECT pg_sleep(1.1)`)
	if err = tx.Commit(f.ctx); err != nil {
		t.Fatal(err)
	}
	if err = <-ch; err == nil {
		t.Fatal("expired waiting evidence admitted")
	}
	var n int
	if err = f.pool.QueryRow(f.ctx, `SELECT count(*) FROM runtime_recovery_cleaned_commit_evidence`).Scan(&n); err != nil || n != 0 {
		t.Fatal("expired partial evidence", err)
	}
}
