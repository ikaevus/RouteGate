package db

import (
	"bytes"
	"context"
	"encoding/json"
	"os"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/ikaevus/routegate/backend/internal/agents"
	"github.com/ikaevus/routegate/backend/internal/auth"
	"github.com/ikaevus/routegate/backend/internal/revocations"
	"github.com/jackc/pgx/v5/pgxpool"
)

type recoveryCommitFixture struct {
	ctx   context.Context
	pool  *pgxpool.Pool
	svc   *revocations.Service
	grant revocations.RecoveryMutationGrantApproval
	proof revocations.RecoveryCommitProof
	token string
	actor auth.AuthenticatedUser
}

func newRecoveryCommitFixture(t *testing.T, lifetime time.Duration) recoveryCommitFixture {
	t.Helper()
	ctx, pool, in, u, done := recoveryGrantFixture(t, lifetime, true)
	t.Cleanup(done)
	f := recoveryCommitFixture{ctx: ctx, pool: pool, svc: revocations.NewService(pool), actor: u}
	var err error
	f.grant, err = f.svc.IssueRecoveryMutationGrant(ctx, in)
	if err != nil {
		t.Fatal(err)
	}
	if err = pool.QueryRow(ctx, `SELECT token_hash FROM agents WHERE id=$1::uuid`, f.grant.Grant.Expected.AgentID).Scan(&f.token); err != nil {
		t.Fatal(err)
	}
	if _, err = agents.NewRepository(pool).UpdateAgentHeartbeat(ctx, agents.UpdateAgentHeartbeatInput{TokenHash: f.token, Capabilities: agents.Capabilities{"runtimeMutationFencingV1": true, "runtimeRecoveryObservationV1": true, "runtimeRecoveryGrantReadV1": true}}); err != nil {
		t.Fatal(err)
	}
	var now time.Time
	if err = pool.QueryRow(ctx, `SELECT clock_timestamp()`).Scan(&now); err != nil {
		t.Fatal(err)
	}
	f.proof = revocations.RecoveryCommitProof{SchemaVersion: 1, OperationID: in.PreparationID, GrantHash: f.grant.GrantHash, ConsumptionHash: strings.Repeat("9", 64), ObservedAt: now.UTC().Add(321 * time.Nanosecond), Observation: f.grant.Grant.Expected}
	return f
}
func (f recoveryCommitFixture) snapshot(t *testing.T) string {
	t.Helper()
	var s string
	if err := f.pool.QueryRow(f.ctx, `SELECT jsonb_build_array((SELECT jsonb_agg(to_jsonb(a)) FROM agents a),(SELECT jsonb_agg(to_jsonb(p)) FROM runtime_recovery_preparations p),(SELECT jsonb_agg(to_jsonb(g)) FROM runtime_recovery_mutation_grants g),(SELECT jsonb_agg(to_jsonb(e)) FROM runtime_recovery_observation_evidence e),(SELECT jsonb_agg(to_jsonb(a)) FROM vpn_accounts a),(SELECT jsonb_agg(to_jsonb(c)) FROM config_versions c),(SELECT jsonb_agg(to_jsonb(j)) FROM config_apply_jobs j),(SELECT jsonb_agg(to_jsonb(j)) FROM agent_operation_jobs j),(SELECT jsonb_agg(to_jsonb(t)) FROM vpn_subscription_tokens t))::text`).Scan(&s); err != nil {
		t.Fatal(err)
	}
	return s
}
func (f recoveryCommitFixture) exec(t *testing.T, sql string, args ...any) {
	t.Helper()
	if _, err := f.pool.Exec(f.ctx, sql, args...); err != nil {
		t.Fatal(err)
	}
}

func TestRuntimeMutationFencingRecoveryCommitEvidence(t *testing.T) {
	f := newRecoveryCommitFixture(t, 0)
	wire, _ := json.Marshal(f.proof)
	before := f.snapshot(t)
	if _, err := f.svc.VerifyRecoveryCommit(f.ctx, f.token, f.proof.OperationID, wire); err == nil {
		t.Fatal("unrecorded proof verified")
	}
	results := make(chan revocations.RecoveryCommitReceipt, 2)
	var wg sync.WaitGroup
	for range 2 {
		wg.Go(func() {
			r, e := f.svc.AcceptRecoveryCommit(f.ctx, f.token, f.proof.OperationID, wire)
			if e != nil {
				t.Error("concurrent recording", e)
			}
			results <- r
		})
	}
	wg.Wait()
	a, b := <-results, <-results
	if !a.Recorded || a != b || a.ProofHash != recoveryTestHash(wire) || a.RequestHash != a.ProofHash || a.GrantHash != f.grant.GrantHash || a.ConsumptionHash != f.proof.ConsumptionHash {
		t.Fatal("receipt binding/idempotence")
	}
	var saved, hash, consumption string
	var count int
	var accepted time.Time
	var precision bool
	if err := f.pool.QueryRow(f.ctx, `SELECT proof,proof_hash,consumption_hash,accepted_at,observed_at=(proof::jsonb->>'observedAt')::timestamptz FROM runtime_recovery_commit_evidence WHERE preparation_id=$1::uuid`, f.proof.OperationID).Scan(&saved, &hash, &consumption, &accepted, &precision); err != nil || saved != string(wire) || hash != a.ProofHash || consumption != a.ConsumptionHash || !precision {
		t.Fatal("retained wire/index precision", err)
	}
	var pretty bytes.Buffer
	json.Indent(&pretty, wire, "", "  ")
	r, err := f.svc.AcceptRecoveryCommit(f.ctx, f.token, f.proof.OperationID, pretty.Bytes())
	if err != nil || r.ProofHash != a.ProofHash || r.RequestHash != recoveryTestHash(pretty.Bytes()) || r.RequestHash == r.ProofHash {
		t.Fatal("retry request/canonical hash", err)
	}
	verified, err := f.svc.VerifyRecoveryCommit(f.ctx, f.token, f.proof.OperationID, pretty.Bytes())
	if err != nil || verified != r {
		t.Fatal("exact readback", err)
	}
	for _, change := range []string{"time", "consumption", "grant", "observation"} {
		p := f.proof
		switch change {
		case "time":
			p.ObservedAt = p.ObservedAt.Add(time.Microsecond)
		case "consumption":
			p.ConsumptionHash = strings.Repeat("a", 64)
		case "grant":
			p.GrantHash = strings.Repeat("a", 64)
		case "observation":
			p.Observation.Witness.ListenerPort++
		}
		data, _ := json.Marshal(p)
		if _, err = f.svc.AcceptRecoveryCommit(f.ctx, f.token, p.OperationID, data); err == nil {
			t.Fatal("conflicting proof replaced ledger", change)
		}
		if _, err = f.svc.VerifyRecoveryCommit(f.ctx, f.token, p.OperationID, data); err == nil {
			t.Fatal("changed proof verified", change)
		}
	}
	for _, sql := range []string{`UPDATE runtime_recovery_commit_evidence SET proof_hash=proof_hash`, `DELETE FROM runtime_recovery_commit_evidence`} {
		if _, err = f.pool.Exec(f.ctx, sql); err == nil {
			t.Fatal("commit history mutable")
		}
	}
	down, err := os.ReadFile("../../migrations/000165_recovery_commit_evidence.down.sql")
	if err != nil {
		t.Fatal(err)
	}
	if _, err = f.pool.Exec(f.ctx, string(down)); err == nil {
		t.Fatal("downgrade erased retained evidence")
	}
	if _, err = f.svc.CancelRuntimeRecovery(f.ctx, f.proof.OperationID, f.actor.ID); err == nil {
		t.Fatal("recording released grant reservation")
	}
	if _, err = agents.NewRepository(f.pool).CreateOrReplaceAgentForServer(f.ctx, agents.CreateOrReplaceAgentInput{ServerID: f.proof.Observation.ServerID, TokenHash: "replacement-isolated"}); err == nil {
		t.Fatal("pending recovery allowed credential replacement")
	}
	var after time.Time
	if err = f.pool.QueryRow(f.ctx, `SELECT count(*),min(accepted_at) FROM runtime_recovery_commit_evidence`).Scan(&count, &after); err != nil || count != 1 || !after.Equal(accepted) {
		t.Fatal("retry renewed acceptance", err)
	}
	if f.snapshot(t) != before {
		t.Fatal("recording mutated Agent/reservation/grant/account/token/config/job")
	}
}

func TestRuntimeMutationFencingRecoveryCommitRejects(t *testing.T) {
	for _, mode := range []string{"wrong_token", "wrong_path", "wrong_grant", "foreign_agent", "foreign_server", "foreign_task", "runtime_drift", "before_grant", "future_observation", "after_expiry", "stale_agent", "offline_agent", "fencing_lost", "observation_capability_lost", "read_capability_lost", "revoked_issuer", "expired_session", "disabled_issuer", "service_issuer", "removed_role", "removed_permission"} {
		t.Run(mode, func(t *testing.T) {
			f := newRecoveryCommitFixture(t, 0)
			p := f.proof
			token := f.token
			operation := p.OperationID
			switch mode {
			case "wrong_token":
				token = "retired-isolated"
			case "wrong_path":
				operation = f.actor.ID
			case "wrong_grant":
				p.GrantHash = strings.Repeat("e", 64)
			case "foreign_agent":
				p.Observation.AgentID = f.actor.ID
			case "foreign_server":
				p.Observation.ServerID = f.actor.ID
			case "foreign_task":
				p.Observation.TaskID = f.actor.ID
			case "runtime_drift":
				p.Observation.Witness.ListenerPort++
			case "before_grant":
				p.ObservedAt = f.grant.Grant.IssuedAt.Add(-time.Nanosecond)
			case "future_observation":
				p.ObservedAt = f.grant.Grant.ExpiresAt.Add(-time.Millisecond)
			case "after_expiry":
				p.ObservedAt = f.grant.Grant.ExpiresAt
			case "stale_agent":
				f.exec(t, `UPDATE agents SET last_authenticated_heartbeat_at=clock_timestamp()-interval '10 minutes' WHERE id=$1::uuid`, p.Observation.AgentID)
			case "offline_agent":
				f.exec(t, `UPDATE agents SET status='offline' WHERE id=$1::uuid`, p.Observation.AgentID)
			case "fencing_lost":
				f.exec(t, `UPDATE agents SET current_runtime_mutation_fencing=false WHERE id=$1::uuid`, p.Observation.AgentID)
			case "observation_capability_lost":
				f.exec(t, `UPDATE agents SET capabilities=capabilities-'runtimeRecoveryObservationV1' WHERE id=$1::uuid`, p.Observation.AgentID)
			case "read_capability_lost":
				f.exec(t, `UPDATE agents SET capabilities=capabilities-'runtimeRecoveryGrantReadV1' WHERE id=$1::uuid`, p.Observation.AgentID)
			case "revoked_issuer":
				f.exec(t, `UPDATE auth_sessions SET revoked_at=clock_timestamp() WHERE id=$1::uuid`, f.actor.SessionID)
			case "expired_session":
				f.exec(t, `UPDATE auth_sessions SET expires_at=clock_timestamp()-interval '1 second' WHERE id=$1::uuid`, f.actor.SessionID)
			case "disabled_issuer":
				f.exec(t, `UPDATE users SET status='disabled' WHERE id=$1::uuid`, f.actor.ID)
			case "service_issuer":
				f.exec(t, `UPDATE users SET user_type='agent' WHERE id=$1::uuid`, f.actor.ID)
			case "removed_role":
				f.exec(t, `DELETE FROM user_roles WHERE user_id=$1::uuid`, f.actor.ID)
			case "removed_permission":
				f.exec(t, `DELETE FROM role_permissions`)
			}
			before := f.snapshot(t)
			wire, _ := json.Marshal(p)
			if _, err := f.svc.AcceptRecoveryCommit(f.ctx, token, operation, wire); err == nil {
				t.Fatal("unsafe commit proof recorded")
			}
			var count int
			if err := f.pool.QueryRow(f.ctx, `SELECT count(*) FROM runtime_recovery_commit_evidence`).Scan(&count); err != nil || count != 0 {
				t.Fatal("partial proof persisted", err)
			}
			if f.snapshot(t) != before {
				t.Fatal("refusal mutated protected state")
			}
		})
	}
}

func TestRuntimeMutationFencingRecoveryCommitHistoricalReadback(t *testing.T) {
	f := newRecoveryCommitFixture(t, time.Second)
	wire, _ := json.Marshal(f.proof)
	r, err := f.svc.AcceptRecoveryCommit(f.ctx, f.token, f.proof.OperationID, wire)
	if err != nil {
		t.Fatal(err)
	}
	f.exec(t, `SELECT pg_sleep(1.1)`)
	if _, err = f.svc.AcceptRecoveryCommit(f.ctx, f.token, f.proof.OperationID, wire); err == nil {
		t.Fatal("expiry retry renewed admission")
	}
	before := f.snapshot(t)
	verified, err := f.svc.VerifyRecoveryCommit(f.ctx, f.token, f.proof.OperationID, wire)
	if err != nil || verified != r {
		t.Fatal("expired exact storage not readable", err)
	}
	if f.snapshot(t) != before {
		t.Fatal("expiry readback changed protected state")
	}
	f.exec(t, `UPDATE auth_sessions SET revoked_at=clock_timestamp() WHERE id=$1::uuid`, f.actor.SessionID)
	f.exec(t, `DELETE FROM role_permissions`)
	f.exec(t, `UPDATE agents SET status='offline',current_runtime_mutation_fencing=false,capabilities='{}',last_authenticated_heartbeat_at=clock_timestamp()-interval '10 minutes' WHERE id=$1::uuid`, f.proof.Observation.AgentID)
	before = f.snapshot(t)
	verified, err = f.svc.VerifyRecoveryCommit(f.ctx, f.token, f.proof.OperationID, wire)
	if err != nil || verified != r {
		t.Fatal("historical readback requires fresh authority", err)
	}
	if _, err = f.svc.VerifyRecoveryCommit(f.ctx, "retired-isolated", f.proof.OperationID, wire); err == nil {
		t.Fatal("retired token read evidence")
	}
	if _, err = f.svc.VerifyRecoveryCommit(f.ctx, f.token, f.actor.ID, wire); err == nil {
		t.Fatal("foreign operation read evidence")
	}
	if f.snapshot(t) != before {
		t.Fatal("historical readback changed protected state")
	}
}

func TestRuntimeMutationFencingRecoveryCommitWaitExpiry(t *testing.T) {
	f := newRecoveryCommitFixture(t, time.Second)
	wire, _ := json.Marshal(f.proof)
	tx, err := f.pool.Begin(f.ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer tx.Rollback(f.ctx)
	if _, err = tx.Exec(f.ctx, `SELECT id FROM servers WHERE id=$1::uuid FOR UPDATE`, f.proof.Observation.ServerID); err != nil {
		t.Fatal(err)
	}
	ch := make(chan error, 1)
	go func() { _, e := f.svc.AcceptRecoveryCommit(f.ctx, f.token, f.proof.OperationID, wire); ch <- e }()
	deadline := time.Now().Add(time.Second)
	for {
		var waiting bool
		if err = f.pool.QueryRow(f.ctx, `SELECT EXISTS(SELECT 1 FROM pg_stat_activity WHERE query LIKE '%SELECT id FROM servers WHERE id=%FOR UPDATE%' AND wait_event_type='Lock' AND pid<>pg_backend_pid())`).Scan(&waiting); err != nil {
			t.Fatal(err)
		}
		if waiting {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("acceptance did not wait for node")
		}
		f.exec(t, `SELECT pg_sleep(0.01)`)
	}
	short, cancel := context.WithTimeout(f.ctx, 500*time.Millisecond)
	defer cancel()
	if _, err = f.pool.Exec(short, `UPDATE roles SET name=name WHERE code='admin'`); err != nil {
		t.Fatal("node wait holds global role locks", err)
	}
	f.exec(t, `SELECT pg_sleep(1.1)`)
	if err = tx.Commit(f.ctx); err != nil {
		t.Fatal(err)
	}
	if err = <-ch; err == nil {
		t.Fatal("expired commit recorded after node wait")
	}
	var count int
	if err = f.pool.QueryRow(f.ctx, `SELECT count(*) FROM runtime_recovery_commit_evidence`).Scan(&count); err != nil || count != 0 {
		t.Fatal("expired partial proof persisted", err)
	}
}

func TestRuntimeMutationFencingRecoveryCommitSQLGuard(t *testing.T) {
	f := newRecoveryCommitFixture(t, 0)
	insert := func(p revocations.RecoveryCommitProof) error {
		data, _ := json.Marshal(p)
		_, err := f.pool.Exec(f.ctx, `INSERT INTO runtime_recovery_commit_evidence(preparation_id,grant_hash,consumption_hash,proof,proof_hash,observed_at) VALUES($1::uuid,$2,$3,$4::text,$5,($4::text::jsonb->>'observedAt')::timestamptz)`, f.proof.OperationID, p.GrantHash, p.ConsumptionHash, string(data), recoveryTestHash(data))
		return err
	}
	for _, mode := range []string{"grant", "consumption", "schema", "operation", "observation", "future"} {
		p := f.proof
		switch mode {
		case "grant":
			p.GrantHash = strings.Repeat("e", 64)
		case "consumption":
			p.ConsumptionHash = "bad"
		case "schema":
			p.SchemaVersion = 2
		case "operation":
			p.OperationID = f.actor.ID
		case "observation":
			p.Observation.Witness.ListenerPort++
		case "future":
			p.ObservedAt = f.grant.Grant.ExpiresAt
		}
		if err := insert(p); err == nil {
			t.Fatal("SQL guard accepted invalid proof", mode)
		}
	}
	f.exec(t, `UPDATE agents SET capabilities=capabilities-'runtimeRecoveryGrantReadV1' WHERE id=$1::uuid`, f.proof.Observation.AgentID)
	if err := insert(f.proof); err == nil {
		t.Fatal("SQL bypassed current read capability")
	}
	if _, err := agents.NewRepository(f.pool).UpdateAgentHeartbeat(f.ctx, agents.UpdateAgentHeartbeatInput{TokenHash: f.token, Capabilities: agents.Capabilities{"runtimeMutationFencingV1": true, "runtimeRecoveryObservationV1": true, "runtimeRecoveryGrantReadV1": true}}); err != nil {
		t.Fatal(err)
	}
	f.exec(t, `UPDATE auth_sessions SET revoked_at=clock_timestamp() WHERE id=$1::uuid`, f.actor.SessionID)
	if err := insert(f.proof); err == nil {
		t.Fatal("SQL bypassed current issuer authority")
	}
	var count int
	if err := f.pool.QueryRow(f.ctx, `SELECT count(*) FROM runtime_recovery_commit_evidence`).Scan(&count); err != nil || count != 0 {
		t.Fatal("SQL refusal retained partial proof", err)
	}
}

func TestRuntimeMutationFencingRecoveryCommitRetryAuthorityLoss(t *testing.T) {
	f := newRecoveryCommitFixture(t, 0)
	wire, _ := json.Marshal(f.proof)
	r, err := f.svc.AcceptRecoveryCommit(f.ctx, f.token, f.proof.OperationID, wire)
	if err != nil {
		t.Fatal(err)
	}
	f.exec(t, `DELETE FROM user_roles WHERE user_id=$1::uuid`, f.actor.ID)
	before := f.snapshot(t)
	if _, err = f.svc.AcceptRecoveryCommit(f.ctx, f.token, f.proof.OperationID, wire); err == nil {
		t.Fatal("recorded proof bypassed revoked current authority")
	}
	verified, err := f.svc.VerifyRecoveryCommit(f.ctx, f.token, f.proof.OperationID, wire)
	if err != nil || verified != r {
		t.Fatal("historical storage lost after authority revocation", err)
	}
	if f.snapshot(t) != before {
		t.Fatal("retry/readback changed protected state")
	}
}
