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

type recoveryTerminalFixture struct {
	recoveryFinalizationFixture
	terminal revocations.RecoveryTerminalProof
}

func newRecoveryTerminalFixture(t *testing.T, lifetime time.Duration) recoveryTerminalFixture {
	return recoveryTerminalFixtureWithFinal(t, lifetime, true)
}
func recoveryTerminalFixtureWithFinal(t *testing.T, lifetime time.Duration, recordFinal bool) recoveryTerminalFixture {
	t.Helper()
	f := newRecoveryFinalizationFixture(t, lifetime)
	wire, _ := json.Marshal(f.proof)
	receipt := revocations.RecoveryFinalizationReceipt{SchemaVersion: 1, Recorded: true, OperationID: f.proof.OperationID, AuthorizationHash: f.proof.AuthorizationHash, FinalConsumptionHash: f.proof.FinalConsumptionHash, ProofHash: recoveryTestHash(wire), RequestHash: recoveryTestHash(wire)}
	var err error
	if recordFinal {
		receipt, err = f.svc.AcceptRecoveryFinalization(f.ctx, f.token, f.proof.OperationID, wire)
		if err != nil {
			t.Fatal(err)
		}
	}
	ack, _ := json.Marshal(receipt)
	var now time.Time
	if err = f.pool.QueryRow(f.ctx, `SELECT clock_timestamp()`).Scan(&now); err != nil {
		t.Fatal(err)
	}
	return recoveryTerminalFixture{f, revocations.RecoveryTerminalProof{
		SchemaVersion: 1, Purpose: revocations.RecoveryTerminalPurpose, OperationID: f.proof.OperationID,
		AgentCredentialGeneration: f.authorization.Authorization.AgentCredentialGeneration,
		AuthorizationHash:         f.proof.AuthorizationHash, FinalConsumptionHash: f.proof.FinalConsumptionHash,
		FinalProofHash: receipt.ProofHash, FinalReceiptHash: recoveryTestHash(ack),
		IntentHash: strings.Repeat("b", 64), TerminalHash: strings.Repeat("c", 64),
		ObservedAt: now.UTC().Add(321 * time.Nanosecond), Observation: f.proof.Observation}}
}
func (f recoveryTerminalFixture) snapshot(t *testing.T) string {
	t.Helper()
	var final string
	if err := f.pool.QueryRow(f.ctx, `SELECT jsonb_agg(to_jsonb(e))::text FROM runtime_recovery_finalization_evidence e`).Scan(&final); err != nil {
		t.Fatal(err)
	}
	return f.recoveryFinalizationFixture.snapshot(t) + final
}
func (f recoveryTerminalFixture) insert(p revocations.RecoveryTerminalProof) error {
	wire, _ := json.Marshal(p)
	_, err := f.pool.Exec(f.ctx, `INSERT INTO runtime_recovery_terminal_evidence(preparation_id,proof,proof_hash,observed_at) VALUES($1::uuid,$2::text,$3,($2::text::jsonb->>'observedAt')::timestamptz)`, f.terminal.OperationID, string(wire), recoveryTestHash(wire))
	return err
}
func TestRecoveryTerminalEvidence(t *testing.T) {
	f := newRecoveryTerminalFixture(t, 0)
	wire, _ := json.Marshal(f.terminal)
	before := f.snapshot(t)
	if _, err := f.svc.VerifyRecoveryTerminal(f.ctx, f.token, f.terminal.OperationID, wire); err == nil {
		t.Fatal("unrecorded terminal accepted")
	}
	results := make(chan revocations.RecoveryTerminalReceipt, 2)
	var wg sync.WaitGroup
	for range 2 {
		wg.Go(func() {
			r, e := f.svc.AcceptRecoveryTerminal(f.ctx, f.token, f.terminal.OperationID, wire)
			if e != nil {
				t.Error(e)
			}
			results <- r
		})
	}
	wg.Wait()
	a, b := <-results, <-results
	if !a.Recorded || a != b || a.ProofHash != recoveryTestHash(wire) || a.RequestHash != a.ProofHash || a.TerminalHash != f.terminal.TerminalHash {
		t.Fatal("receipt binding")
	}
	var saved string
	var accepted time.Time
	if err := f.pool.QueryRow(f.ctx, `SELECT proof,accepted_at FROM runtime_recovery_terminal_evidence`).Scan(&saved, &accepted); err != nil || saved != string(wire) {
		t.Fatal("immutable wire", err)
	}
	var pretty bytes.Buffer
	json.Indent(&pretty, wire, "", "  ")
	r, err := f.svc.AcceptRecoveryTerminal(f.ctx, f.token, f.terminal.OperationID, pretty.Bytes())
	if err != nil || r.ProofHash != a.ProofHash || r.RequestHash != recoveryTestHash(pretty.Bytes()) {
		t.Fatal("retry", err)
	}
	verified, err := f.svc.VerifyRecoveryTerminal(f.ctx, f.token, f.terminal.OperationID, pretty.Bytes())
	if err != nil || verified != r {
		t.Fatal("readback", err)
	}
	var after time.Time
	if err = f.pool.QueryRow(f.ctx, `SELECT accepted_at FROM runtime_recovery_terminal_evidence`).Scan(&after); err != nil || !after.Equal(accepted) {
		t.Fatal("retry renewed evidence", err)
	}
	for _, q := range []string{`UPDATE runtime_recovery_terminal_evidence SET proof=proof`, `DELETE FROM runtime_recovery_terminal_evidence`} {
		if _, err = f.pool.Exec(f.ctx, q); err == nil {
			t.Fatal("mutable history")
		}
	}
	down, err := os.ReadFile("../../migrations/000168_recovery_terminal_evidence.down.sql")
	if err != nil {
		t.Fatal(err)
	}
	if _, err = f.pool.Exec(f.ctx, string(down)); err == nil {
		t.Fatal("downgrade erased history")
	}
	if _, err = f.svc.CancelRuntimeRecovery(f.ctx, f.terminal.OperationID, f.actor.ID); err == nil {
		t.Fatal("terminal storage released reservation")
	}
	if f.snapshot(t) != before {
		t.Fatal("terminal storage mutated protected state")
	}
	p := f.terminal
	p.TerminalHash = strings.Repeat("d", 64)
	changed, _ := json.Marshal(p)
	if _, err = f.svc.AcceptRecoveryTerminal(f.ctx, f.token, p.OperationID, changed); err == nil {
		t.Fatal("conflicting evidence replaced")
	}
	if _, err = f.svc.VerifyRecoveryTerminal(f.ctx, f.token, p.OperationID, changed); err == nil {
		t.Fatal("conflicting evidence verified")
	}
}

func TestRecoveryTerminalRejects(t *testing.T) {
	for _, mode := range []string{"token", "path", "generation", "authorization", "consumption", "final_proof", "final_receipt", "before_final", "before_final_nanosecond", "future", "expired", "agent", "server", "task", "observation", "offline", "stale", "heartbeat_generation", "fencing", "capability", "issuer", "role", "permission", "missing_final"} {
		t.Run(mode, func(t *testing.T) {
			f := recoveryTerminalFixtureWithFinal(t, 0, mode != "missing_final")
			p := f.terminal
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
			case "consumption":
				p.FinalConsumptionHash = strings.Repeat("d", 64)
			case "final_proof":
				p.FinalProofHash = strings.Repeat("d", 64)
			case "final_receipt":
				p.FinalReceiptHash = strings.Repeat("d", 64)
			case "before_final":
				p.ObservedAt = f.proof.ObservedAt.Add(-time.Microsecond)
			case "before_final_nanosecond":
				p.ObservedAt = f.proof.ObservedAt.Add(-time.Nanosecond)
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
			case "issuer":
				f.exec(t, `UPDATE auth_sessions SET revoked_at=clock_timestamp() WHERE id=$1::uuid`, f.actor.SessionID)
			case "role":
				f.exec(t, `DELETE FROM user_roles WHERE user_id=$1::uuid`, f.actor.ID)
			case "permission":
				f.exec(t, `DELETE FROM role_permissions`)
			}
			// Missing final has no snapshot row; all other refusals preserve protected state.
			before := ""
			if mode != "missing_final" {
				before = f.snapshot(t)
			}
			wire, _ := json.Marshal(p)
			if _, err := f.svc.AcceptRecoveryTerminal(f.ctx, token, operation, wire); err == nil {
				t.Fatal("unsafe terminal recorded")
			}
			// Independent SQL admission guard must reject the same forged/expired statement.
			if mode != "token" && mode != "path" && f.insert(p) == nil {
				t.Fatal("SQL bypass admitted evidence")
			}
			var n int
			if err := f.pool.QueryRow(f.ctx, `SELECT count(*) FROM runtime_recovery_terminal_evidence`).Scan(&n); err != nil || n != 0 {
				t.Fatal("partial evidence", err)
			}
			if mode != "missing_final" && f.snapshot(t) != before {
				t.Fatal("refusal changed protected state")
			}
		})
	}
}

func TestRecoveryTerminalHistoricalReadback(t *testing.T) {
	f := newRecoveryTerminalFixture(t, time.Second)
	wire, _ := json.Marshal(f.terminal)
	r, err := f.svc.AcceptRecoveryTerminal(f.ctx, f.token, f.terminal.OperationID, wire)
	if err != nil {
		t.Fatal(err)
	}
	f.exec(t, `SELECT pg_sleep(1.1)`)
	if _, err = f.svc.AcceptRecoveryTerminal(f.ctx, f.token, f.terminal.OperationID, wire); err == nil {
		t.Fatal("expired retry admitted")
	}
	f.exec(t, `UPDATE auth_sessions SET revoked_at=clock_timestamp() WHERE id=$1::uuid`, f.actor.SessionID)
	f.exec(t, `UPDATE agents SET status='offline',current_runtime_mutation_fencing=false,capabilities='{}',last_authenticated_heartbeat_at=clock_timestamp()-interval '10 minutes'`)
	before := f.snapshot(t)
	got, err := f.svc.VerifyRecoveryTerminal(f.ctx, f.token, f.terminal.OperationID, wire)
	if err != nil || got != r || f.snapshot(t) != before {
		t.Fatal("historical readback renewed authority", err)
	}
	p := f.terminal
	p.AgentCredentialGeneration++
	changed, _ := json.Marshal(p)
	if _, err = f.svc.VerifyRecoveryTerminal(f.ctx, f.token, p.OperationID, changed); err == nil {
		t.Fatal("generation changed")
	}
	if _, err = f.svc.VerifyRecoveryTerminal(f.ctx, "retired-isolated", p.OperationID, wire); err == nil {
		t.Fatal("retired token read")
	}
}

func TestRecoveryTerminalRawSQLWire(t *testing.T) {
	f := newRecoveryTerminalFixture(t, 0)
	wire, _ := json.Marshal(f.terminal)
	base := string(wire)
	for _, bad := range []string{
		`{"schemaVersion":2,` + base[1:],
		strings.Replace(base, `"witness":{"schemaVersion":1`, `"witness":{"schemaVersion":2,"schemaVersion":1`, 1),
		`{"schema\u0056ersion":2,` + base[1:],
		" " + base,
		strings.Replace(base, `"schemaVersion":1`, `"schemaVersion":1.0`, 1),
		strings.Replace(base, `"operationId"`, `"operation\u0049d"`, 1),
		strings.Replace(base, `"listenerPort":443`, `"listenerPort":443.0`, 1),
	} {
		_, err := f.pool.Exec(f.ctx, `INSERT INTO runtime_recovery_terminal_evidence(preparation_id,proof,proof_hash,observed_at) VALUES($1::uuid,$2::text,$3,($2::text::jsonb->>'observedAt')::timestamptz)`, f.terminal.OperationID, bad, recoveryTestHash([]byte(bad)))
		var pe *pgconn.PgError
		if !errors.As(err, &pe) || pe.Code != "P0148" {
			t.Fatal("SQL raw wire accepted", err)
		}
	}
	if err := f.insert(f.terminal); err != nil {
		t.Fatal("valid raw SQL rejected", err)
	}
	if r, err := f.svc.VerifyRecoveryTerminal(f.ctx, f.token, f.terminal.OperationID, wire); err != nil || !r.Recorded {
		t.Fatal("SQL retained evidence unreadable", err)
	}
}

func TestRecoveryTerminalWaitExpiry(t *testing.T) {
	f := newRecoveryTerminalFixture(t, time.Second)
	wire, _ := json.Marshal(f.terminal)
	tx, err := f.pool.Begin(f.ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer tx.Rollback(f.ctx)
	if _, err = tx.Exec(f.ctx, `SELECT id FROM servers WHERE id=$1::uuid FOR UPDATE`, f.terminal.Observation.ServerID); err != nil {
		t.Fatal(err)
	}
	ch := make(chan error, 1)
	go func() { _, e := f.svc.AcceptRecoveryTerminal(f.ctx, f.token, f.terminal.OperationID, wire); ch <- e }()
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
	if err = f.pool.QueryRow(f.ctx, `SELECT count(*) FROM runtime_recovery_terminal_evidence`).Scan(&n); err != nil || n != 0 {
		t.Fatal("expired partial evidence", err)
	}
}
