package db

import (
	"context"
	"encoding/json"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/ikaevus/routegate/backend/internal/auth"
	"github.com/ikaevus/routegate/backend/internal/revocations"
)

func releaseFixture(t *testing.T, lifetime time.Duration, record bool) (recoveryCommitFixture, revocations.RecoveryReleaseAuthorizationInput) {
	t.Helper()
	f := newRecoveryCommitFixture(t, lifetime)
	wire, _ := json.Marshal(f.proof)
	if record {
		if _, err := f.svc.AcceptRecoveryCommit(f.ctx, f.token, f.proof.OperationID, wire); err != nil {
			t.Fatal(err)
		}
	}
	return f, revocations.RecoveryReleaseAuthorizationInput{PreparationID: f.proof.OperationID, GrantHash: f.proof.GrantHash, ConsumptionHash: f.proof.ConsumptionHash, CommitProofHash: recoveryTestHash(wire), ConfirmFinalFenceRelease: true}
}

func TestRecoveryReleaseAuthorization(t *testing.T) {
	f, in := releaseFixture(t, 0, true)
	before := f.snapshot(t)
	type result struct {
		a   revocations.RecoveryReleaseApproval
		err error
	}
	ch := make(chan result, 2)
	for range 2 {
		go func() { a, e := f.svc.IssueRecoveryReleaseAuthorization(f.ctx, in); ch <- result{a, e} }()
	}
	x, y := <-ch, <-ch
	if x.err != nil || y.err != nil || x.a != y.a {
		t.Fatal("concurrent authorization not idempotent", x.err, y.err)
	}
	a := x.a.Authorization
	wire, _ := json.Marshal(a)
	if a.SchemaVersion != 1 || a.Purpose != "runtime_mutation_fence_final_release_v1" || a.OperationID != in.PreparationID || a.GrantHash != in.GrantHash || a.ConsumptionHash != in.ConsumptionHash || a.CommitProofHash != in.CommitProofHash || a.AgentCredentialGeneration != f.grant.Grant.AgentCredentialGeneration || a.Expected != f.proof.Observation || len(a.Nonce) != 64 || a.Nonce == f.grant.Grant.Nonce || x.a.AuthorizationHash != recoveryTestHash(wire) || a.ExpiresAt.Sub(a.IssuedAt) != 2*time.Minute {
		t.Fatal("authorization bindings/deadline")
	}
	var saved, hash, actor, session string
	var count int
	if err := f.pool.QueryRow(f.ctx, `SELECT authorization,authorization_hash,actor_user_id::text,actor_session_id::text FROM runtime_recovery_release_authorizations`).Scan(&saved, &hash, &actor, &session); err != nil || saved != string(wire) || hash != x.a.AuthorizationHash || actor != f.actor.ID || session != f.actor.SessionID {
		t.Fatal("attribution lost", err)
	}
	for _, sql := range []string{`UPDATE runtime_recovery_release_authorizations SET authorization=authorization`, `DELETE FROM runtime_recovery_release_authorizations`} {
		if _, err := f.pool.Exec(f.ctx, sql); err == nil {
			t.Fatal("authorization history mutable")
		}
	}
	down, err := os.ReadFile("../../migrations/000166_recovery_release_authorizations.down.sql")
	if err != nil {
		t.Fatal(err)
	}
	if _, err = f.pool.Exec(f.ctx, string(down)); err == nil {
		t.Fatal("downgrade erased authority")
	}
	if _, err = f.svc.CancelRuntimeRecovery(f.ctx, in.PreparationID, f.actor.ID); err == nil {
		t.Fatal("issuance released reservation")
	}
	if err = f.pool.QueryRow(f.ctx, `SELECT count(*) FROM runtime_recovery_release_authorizations`).Scan(&count); err != nil || count != 1 {
		t.Fatal("multiple authorizations", err)
	}
	if f.snapshot(t) != before {
		t.Fatal("issuance changed protected state")
	}
}

func TestRecoveryReleaseRejects(t *testing.T) {
	for _, mode := range []string{"no_context", "confirmation", "context_permission", "context_role", "wrong_operation", "wrong_grant", "wrong_consumption", "wrong_proof", "missing_commit", "revoked_session", "expired_session", "disabled_user", "service_user", "removed_role", "removed_permission", "stale_agent", "offline_agent", "fencing_lost", "observation_lost", "grant_read_lost", "heartbeat_generation", "credential_generation"} {
		t.Run(mode, func(t *testing.T) {
			f, in := releaseFixture(t, 0, mode != "missing_commit")
			ctx := f.ctx
			u := f.actor
			switch mode {
			case "no_context":
				ctx = context.Background()
			case "confirmation":
				in.ConfirmFinalFenceRelease = false
			case "context_permission":
				u.Permissions = nil
				ctx = auth.ContextWithUser(ctx, u)
			case "context_role":
				u.Roles = []string{"read_only"}
				ctx = auth.ContextWithUser(ctx, u)
			case "wrong_operation":
				in.PreparationID = f.actor.ID
			case "wrong_grant":
				in.GrantHash = strings.Repeat("e", 64)
			case "wrong_consumption":
				in.ConsumptionHash = strings.Repeat("e", 64)
			case "wrong_proof":
				in.CommitProofHash = strings.Repeat("e", 64)
			case "revoked_session":
				f.exec(t, `UPDATE auth_sessions SET revoked_at=clock_timestamp() WHERE id=$1::uuid`, u.SessionID)
			case "expired_session":
				f.exec(t, `UPDATE auth_sessions SET expires_at=clock_timestamp()-interval '1 second' WHERE id=$1::uuid`, u.SessionID)
			case "disabled_user":
				f.exec(t, `UPDATE users SET status='disabled' WHERE id=$1::uuid`, u.ID)
			case "service_user":
				f.exec(t, `UPDATE users SET user_type='agent' WHERE id=$1::uuid`, u.ID)
			case "removed_role":
				f.exec(t, `DELETE FROM user_roles WHERE user_id=$1::uuid`, u.ID)
			case "removed_permission":
				f.exec(t, `DELETE FROM role_permissions`)
			case "stale_agent":
				f.exec(t, `UPDATE agents SET last_authenticated_heartbeat_at=clock_timestamp()-interval '10 minutes'`)
			case "offline_agent":
				f.exec(t, `UPDATE agents SET status='offline'`)
			case "fencing_lost":
				f.exec(t, `UPDATE agents SET current_runtime_mutation_fencing=false`)
			case "observation_lost":
				f.exec(t, `UPDATE agents SET capabilities=capabilities-'runtimeRecoveryObservationV1'`)
			case "grant_read_lost":
				f.exec(t, `UPDATE agents SET capabilities=capabilities-'runtimeRecoveryGrantReadV1'`)
			case "heartbeat_generation":
				f.exec(t, `UPDATE agents SET last_authenticated_heartbeat_generation=credential_generation+1`)
			case "credential_generation":
				f.exec(t, `UPDATE agents SET credential_generation=credential_generation+1`)
			}
			before := f.snapshot(t)
			if out, err := f.svc.IssueRecoveryReleaseAuthorization(ctx, in); err != revocations.ErrPreflight || out != (revocations.RecoveryReleaseApproval{}) {
				t.Fatal("unsafe/unsanitized issuance", err)
			}
			var count int
			if err := f.pool.QueryRow(f.ctx, `SELECT count(*) FROM runtime_recovery_release_authorizations`).Scan(&count); err != nil || count != 0 {
				t.Fatal("partial authority", err)
			}
			if f.snapshot(t) != before {
				t.Fatal("refusal changed protected state")
			}
		})
	}
}

func TestRecoveryReleaseFreshAuthorityAfterExpiry(t *testing.T) {
	f, in := releaseFixture(t, time.Second, true)
	f.exec(t, `SELECT pg_sleep(1.1)`)
	f.exec(t, `UPDATE auth_sessions SET revoked_at=clock_timestamp() WHERE id=$1::uuid`, f.actor.SessionID)
	var actor, session string
	if err := f.pool.QueryRow(f.ctx, `INSERT INTO users(email,display_name,user_type,status) VALUES('fresh-release@isolated.invalid','Fresh human admin','human','active') RETURNING id::text`).Scan(&actor); err != nil {
		t.Fatal(err)
	}
	if err := f.pool.QueryRow(f.ctx, `INSERT INTO auth_sessions(user_id,token_hash,expires_at) VALUES($1::uuid,'fresh-release-session',clock_timestamp()+interval '1 hour') RETURNING id::text`, actor).Scan(&session); err != nil {
		t.Fatal(err)
	}
	f.exec(t, `INSERT INTO user_roles(user_id,role_id) SELECT $1::uuid,id FROM roles WHERE code='admin'`, actor)
	u := f.actor
	u.ID = actor
	u.SessionID = session
	ctx := auth.ContextWithUser(f.ctx, u)
	before := f.snapshot(t)
	a, err := f.svc.IssueRecoveryReleaseAuthorization(ctx, in)
	if err != nil || !a.Authorization.IssuedAt.After(f.grant.Grant.ExpiresAt) {
		t.Fatal("fresh explicit authority unavailable after history expiry", err)
	}
	if f.snapshot(t) != before {
		t.Fatal("fresh authority renewed historical grant/evidence")
	}
	if _, err = f.svc.IssueRecoveryReleaseAuthorization(f.ctx, in); err == nil {
		t.Fatal("old revoked issuer adopted new authorization")
	}
	// A different current session cannot silently adopt the original issuance.
	var second string
	if err = f.pool.QueryRow(ctx, `INSERT INTO auth_sessions(user_id,token_hash,expires_at) VALUES($1::uuid,'fresh-release-other-session',clock_timestamp()+interval '1 hour') RETURNING id::text`, actor).Scan(&second); err != nil {
		t.Fatal(err)
	}
	u.SessionID = second
	if _, err = f.svc.IssueRecoveryReleaseAuthorization(auth.ContextWithUser(ctx, u), in); err == nil {
		t.Fatal("new session rebound original authority")
	}
	if _, err = f.svc.CancelRuntimeRecovery(ctx, in.PreparationID, actor); err == nil {
		t.Fatal("fresh issuance cancelled reservation")
	}
}

func TestRecoveryReleaseSessionDeadlineAndRetry(t *testing.T) {
	f, in := releaseFixture(t, 0, true)
	f.exec(t, `UPDATE auth_sessions SET expires_at=clock_timestamp()+interval '1 second' WHERE id=$1::uuid`, f.actor.SessionID)
	var expiry time.Time
	if err := f.pool.QueryRow(f.ctx, `SELECT expires_at FROM auth_sessions WHERE id=$1::uuid`, f.actor.SessionID).Scan(&expiry); err != nil {
		t.Fatal(err)
	}
	a, err := f.svc.IssueRecoveryReleaseAuthorization(f.ctx, in)
	if err != nil || !a.Authorization.ExpiresAt.Equal(expiry) {
		t.Fatal("session deadline not pinned", err)
	}
	f.exec(t, `SELECT pg_sleep(1.1)`)
	// Extend the same session to isolate original authorization expiry.
	f.exec(t, `UPDATE auth_sessions SET expires_at=clock_timestamp()+interval '1 hour' WHERE id=$1::uuid`, f.actor.SessionID)
	if _, err = f.svc.IssueRecoveryReleaseAuthorization(f.ctx, in); err == nil {
		t.Fatal("expired authorization renewed")
	}
	var saved string
	if err = f.pool.QueryRow(f.ctx, `SELECT authorization FROM runtime_recovery_release_authorizations`).Scan(&saved); err != nil {
		t.Fatal(err)
	}
	wire, _ := json.Marshal(a.Authorization)
	if saved != string(wire) {
		t.Fatal("expiry changed nonce/deadline")
	}
}

func TestRecoveryReleaseRetryAuthorityLoss(t *testing.T) {
	f, in := releaseFixture(t, 0, true)
	a, err := f.svc.IssueRecoveryReleaseAuthorization(f.ctx, in)
	if err != nil {
		t.Fatal(err)
	}
	f.exec(t, `DELETE FROM user_roles WHERE user_id=$1::uuid`, f.actor.ID)
	before := f.snapshot(t)
	if _, err = f.svc.IssueRecoveryReleaseAuthorization(f.ctx, in); err == nil {
		t.Fatal("retry adopted revoked authority")
	}
	var saved string
	if err = f.pool.QueryRow(f.ctx, `SELECT authorization FROM runtime_recovery_release_authorizations`).Scan(&saved); err != nil {
		t.Fatal(err)
	}
	wire, _ := json.Marshal(a.Authorization)
	if saved != string(wire) || f.snapshot(t) != before {
		t.Fatal("authority loss rewrote history/protected state")
	}
}

func releaseSQLFixture(t *testing.T, f recoveryCommitFixture) revocations.RecoveryReleaseAuthorization {
	t.Helper()
	var now time.Time
	if err := f.pool.QueryRow(f.ctx, `SELECT clock_timestamp()`).Scan(&now); err != nil {
		t.Fatal(err)
	}
	wire, _ := json.Marshal(f.proof)
	return revocations.RecoveryReleaseAuthorization{SchemaVersion: 1, Purpose: "runtime_mutation_fence_final_release_v1", OperationID: f.proof.OperationID, Nonce: strings.Repeat("a", 64), AgentCredentialGeneration: f.grant.Grant.AgentCredentialGeneration, GrantHash: f.proof.GrantHash, ConsumptionHash: f.proof.ConsumptionHash, CommitProofHash: recoveryTestHash(wire), IssuedAt: now.UTC(), ExpiresAt: now.UTC().Add(time.Minute), Expected: f.proof.Observation}
}

const releaseInsertSQL = `INSERT INTO runtime_recovery_release_authorizations(preparation_id,actor_user_id,actor_session_id,authorization,authorization_hash,issued_at,expires_at) VALUES($1::uuid,$2::uuid,$3::uuid,$4::text,$5,($4::text::jsonb->>'issuedAt')::timestamptz,($4::text::jsonb->>'expiresAt')::timestamptz)`

func TestRecoveryReleaseSQLGuard(t *testing.T) {
	f, _ := releaseFixture(t, 0, true)
	for _, mode := range []string{"purpose", "operation", "generation", "grant", "consumption", "proof", "expected", "nonce_reuse", "future", "expired", "before_commit", "lifetime"} {
		a := releaseSQLFixture(t, f)
		switch mode {
		case "purpose":
			a.Purpose = "runtime_mutation_fence_release_v1"
		case "operation":
			a.OperationID = f.actor.ID
		case "generation":
			a.AgentCredentialGeneration++
		case "grant":
			a.GrantHash = strings.Repeat("f", 64)
		case "consumption":
			a.ConsumptionHash = strings.Repeat("f", 64)
		case "proof":
			a.CommitProofHash = strings.Repeat("f", 64)
		case "expected":
			a.Expected.Witness.ListenerPort++
		case "nonce_reuse":
			a.Nonce = f.grant.Grant.Nonce
		case "future":
			a.IssuedAt = a.IssuedAt.Add(time.Hour)
			a.ExpiresAt = a.ExpiresAt.Add(time.Hour)
		case "expired":
			a.ExpiresAt = a.IssuedAt.Add(-time.Second)
		case "before_commit":
			a.IssuedAt = f.grant.Grant.IssuedAt.Add(-time.Second)
		case "lifetime":
			a.ExpiresAt = a.IssuedAt.Add(3 * time.Minute)
		}
		wire, _ := json.Marshal(a)
		if _, err := f.pool.Exec(f.ctx, releaseInsertSQL, f.proof.OperationID, f.actor.ID, f.actor.SessionID, string(wire), recoveryTestHash(wire)); err == nil {
			t.Fatal("SQL bypass", mode)
		}
	}
	for _, mode := range []string{"capability", "authority"} {
		if mode == "capability" {
			f.exec(t, `UPDATE agents SET current_runtime_mutation_fencing=false`)
		} else {
			f.exec(t, `UPDATE agents SET current_runtime_mutation_fencing=true`)
			f.exec(t, `UPDATE auth_sessions SET revoked_at=clock_timestamp() WHERE id=$1::uuid`, f.actor.SessionID)
		}
		a := releaseSQLFixture(t, f)
		wire, _ := json.Marshal(a)
		if _, err := f.pool.Exec(f.ctx, releaseInsertSQL, f.proof.OperationID, f.actor.ID, f.actor.SessionID, string(wire), recoveryTestHash(wire)); err == nil {
			t.Fatal("SQL bypass current authority/readiness", mode)
		}
	}
}

func TestRecoveryReleaseWaitAuthorityExpiry(t *testing.T) {
	f, in := releaseFixture(t, 0, true)
	tx, err := f.pool.Begin(f.ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer tx.Rollback(f.ctx)
	if _, err = tx.Exec(f.ctx, `SELECT id FROM servers WHERE id=$1::uuid FOR UPDATE`, f.proof.Observation.ServerID); err != nil {
		t.Fatal(err)
	}
	ch := make(chan error, 1)
	go func() { _, e := f.svc.IssueRecoveryReleaseAuthorization(f.ctx, in); ch <- e }()
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
			t.Fatal("issuer did not wait for node")
		}
		f.exec(t, `SELECT pg_sleep(0.01)`)
	}
	short, cancel := context.WithTimeout(f.ctx, 500*time.Millisecond)
	defer cancel()
	if _, err = f.pool.Exec(short, `UPDATE roles SET name=name WHERE code='admin'`); err != nil {
		t.Fatal("node wait blocks global role maintenance", err)
	}
	if _, err = f.pool.Exec(short, `UPDATE auth_sessions SET revoked_at=clock_timestamp() WHERE id=$1::uuid`, f.actor.SessionID); err != nil {
		t.Fatal("node wait locked confirmer session early", err)
	}
	if err = tx.Commit(f.ctx); err != nil {
		t.Fatal(err)
	}
	if err = <-ch; err == nil {
		t.Fatal("authority revoked during node wait issued authorization")
	}
}

func TestRecoveryReleaseDowngradeRace(t *testing.T) {
	f, _ := releaseFixture(t, 0, true)
	a := releaseSQLFixture(t, f)
	wire, _ := json.Marshal(a)
	tx, err := f.pool.Begin(f.ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer tx.Rollback(f.ctx)
	if _, err = tx.Exec(f.ctx, releaseInsertSQL, f.proof.OperationID, f.actor.ID, f.actor.SessionID, string(wire), recoveryTestHash(wire)); err != nil {
		t.Fatal(err)
	}
	down, err := os.ReadFile("../../migrations/000166_recovery_release_authorizations.down.sql")
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(f.ctx, 3*time.Second)
	defer cancel()
	ch := make(chan error, 1)
	go func() { _, e := f.pool.Exec(ctx, string(down)); ch <- e }()
	deadline := time.Now().Add(time.Second)
	for {
		var waiting bool
		if err = f.pool.QueryRow(f.ctx, `SELECT EXISTS(SELECT 1 FROM pg_stat_activity WHERE query LIKE '%recovery_release_authorization_retained%' AND wait_event_type='Lock' AND pid<>pg_backend_pid())`).Scan(&waiting); err != nil {
			t.Fatal(err)
		}
		if waiting {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("downgrade did not wait for insertion")
		}
		f.exec(t, `SELECT pg_sleep(0.01)`)
	}
	if err = tx.Commit(f.ctx); err != nil {
		t.Fatal(err)
	}
	if err = <-ch; err == nil {
		t.Fatal("raced downgrade erased authority")
	}
	var saved string
	if err = f.pool.QueryRow(f.ctx, `SELECT authorization FROM runtime_recovery_release_authorizations`).Scan(&saved); err != nil || saved != string(wire) {
		t.Fatal("raced authority lost", err)
	}
}
