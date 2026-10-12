package db

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/ikaevus/routegate/backend/internal/auth"
	"github.com/ikaevus/routegate/backend/internal/revocations"
	"github.com/jackc/pgx/v5/pgxpool"
)

func recoveryGrantFixture(t *testing.T, lifetime time.Duration, evidence bool) (context.Context, *pgxpool.Pool, revocations.RecoveryMutationGrantInput, auth.AuthenticatedUser, func()) {
	t.Helper()
	ctx, pool, input, u, done := observationApprovalFixture(t)
	svc := revocations.NewService(pool)
	var a revocations.RecoveryObservationApproval
	var err error
	if lifetime == 0 {
		a, err = svc.ApproveRecoveryObservation(ctx, input)
	} else {
		var expected revocations.RecoveryObservation
		if json.Unmarshal(input.Expected, &expected) != nil {
			done()
			t.Fatal("invalid fixture observation")
		}
		var now time.Time
		if err = pool.QueryRow(ctx, `SELECT clock_timestamp()`).Scan(&now); err != nil {
			done()
			t.Fatal(err)
		}
		c := revocations.RecoveryChallenge{SchemaVersion: 1, OperationID: input.PreparationID, Nonce: strings.Repeat("d", 64), IssuedAt: now.UTC().Add(-time.Second), ExpiresAt: now.UTC().Add(lifetime), Expected: expected}
		data, _ := json.Marshal(c)
		sum := sha256.Sum256(data)
		hash := hex.EncodeToString(sum[:])
		_, err = pool.Exec(ctx, `INSERT INTO runtime_recovery_observation_approvals(preparation_id,actor_user_id,actor_session_id,challenge,challenge_hash,issued_at,expires_at) VALUES($1::uuid,$2::uuid,$3::uuid,$4,$5,$6,$7)`, input.PreparationID, u.ID, u.SessionID, string(data), hash, c.IssuedAt, c.ExpiresAt)
		a = revocations.RecoveryObservationApproval{Challenge: c, ChallengeHash: hash}
	}
	if err != nil {
		done()
		t.Fatal(err)
	}
	in := revocations.RecoveryMutationGrantInput{PreparationID: input.PreparationID, ChallengeHash: a.ChallengeHash, EvidenceHash: strings.Repeat("f", 64), ConfirmFenceRecovery: true}
	if evidence {
		var token string
		if err = pool.QueryRow(ctx, `SELECT token_hash FROM agents WHERE id=$1::uuid`, a.Challenge.Expected.AgentID).Scan(&token); err != nil {
			done()
			t.Fatal(err)
		}
		response, _ := json.Marshal(revocations.RecoveryChallengeResponse{SchemaVersion: 1, OperationID: input.PreparationID, ChallengeHash: a.ChallengeHash, ObservedAt: time.Now().UTC(), Observation: a.Challenge.Expected})
		ack, e := svc.AcceptRecoveryEvidence(ctx, token, input.PreparationID, response)
		if e != nil {
			done()
			t.Fatal(e)
		}
		in.EvidenceHash = ack.ResponseHash
	}
	return ctx, pool, in, u, done
}

func TestRuntimeMutationFencingRecoveryGrant(t *testing.T) {
	ctx, pool, in, u, done := recoveryGrantFixture(t, 0, true)
	defer done()
	snapshot := func() string {
		t.Helper()
		var value string
		if err := pool.QueryRow(ctx, `SELECT jsonb_build_array((SELECT jsonb_agg(to_jsonb(a)) FROM vpn_accounts a),(SELECT jsonb_agg(to_jsonb(c)) FROM config_versions c),(SELECT jsonb_agg(to_jsonb(j)) FROM config_apply_jobs j),(SELECT jsonb_agg(to_jsonb(j)) FROM agent_operation_jobs j),(SELECT jsonb_agg(to_jsonb(t)) FROM vpn_subscription_tokens t))::text`).Scan(&value); err != nil {
			t.Fatal(err)
		}
		return value
	}
	before := snapshot()
	type result struct {
		grant revocations.RecoveryMutationGrantApproval
		err   error
	}
	results := make(chan result, 2)
	for range 2 {
		go func() {
			g, e := revocations.NewService(pool).IssueRecoveryMutationGrant(ctx, in)
			results <- result{g, e}
		}()
	}
	a, b := <-results, <-results
	if a.err != nil || b.err != nil || a.grant != b.grant {
		t.Fatal("concurrent issuance was not idempotent", a.err, b.err)
	}
	g := a.grant.Grant
	wire, _ := json.Marshal(g)
	sum := sha256.Sum256(wire)
	if g.SchemaVersion != 1 || g.Purpose != "runtime_mutation_fence_release_v1" || g.OperationID != in.PreparationID || g.ChallengeHash != in.ChallengeHash || g.EvidenceHash != in.EvidenceHash || len(g.Nonce) != 64 || g.AgentCredentialGeneration < 1 || a.grant.GrantHash != hex.EncodeToString(sum[:]) {
		t.Fatal("grant not pinned to exact evidence")
	}
	var deadline time.Time
	var actor, saved, state string
	var count int
	if err := pool.QueryRow(ctx, `SELECT expires_at FROM runtime_recovery_observation_approvals WHERE preparation_id=$1::uuid`, in.PreparationID).Scan(&deadline); err != nil || !g.ExpiresAt.Equal(deadline) {
		t.Fatal("grant extended observation deadline", err)
	}
	if err := pool.QueryRow(ctx, `SELECT actor_user_id::text,grant_payload FROM runtime_recovery_mutation_grants WHERE preparation_id=$1::uuid`, in.PreparationID).Scan(&actor, &saved); err != nil || actor != u.ID || saved != string(wire) {
		t.Fatal("immutable attribution/wire lost", err)
	}
	for _, sql := range []string{`UPDATE runtime_recovery_mutation_grants SET expires_at=expires_at+interval '1 second'`, `DELETE FROM runtime_recovery_mutation_grants`} {
		if _, err := pool.Exec(ctx, sql); err == nil {
			t.Fatal("grant history mutable")
		}
	}
	if _, err := revocations.NewService(pool).CancelRuntimeRecovery(ctx, in.PreparationID, u.ID); err == nil {
		t.Fatal("grant-bearing reservation cancelled")
	}
	if err := pool.QueryRow(ctx, `SELECT state FROM runtime_recovery_preparations WHERE id=$1::uuid`, in.PreparationID).Scan(&state); err != nil || state != "prepared" {
		t.Fatal("grant released reservation", err)
	}
	if err := pool.QueryRow(ctx, `SELECT count(*) FROM runtime_recovery_mutation_grants`).Scan(&count); err != nil || count != 1 {
		t.Fatal("multiple grants", count, err)
	}
	down, err := os.ReadFile("../../migrations/000164_recovery_mutation_grants.down.sql")
	if err != nil {
		t.Fatal(err)
	}
	if _, err = pool.Exec(ctx, string(down)); err == nil {
		t.Fatal("downgrade discarded grant")
	}
	if snapshot() != before {
		t.Fatal("grant issuance mutated account/token/config/job")
	}
}

func TestRuntimeMutationFencingRecoveryGrantRejects(t *testing.T) {
	for _, mode := range []string{"no_context", "no_confirmation", "context_permission", "readonly", "expired_session", "revoked_session", "disabled_user", "service_user", "removed_role", "removed_permission", "missing_evidence", "wrong_challenge", "wrong_evidence", "wrong_preparation", "cancelled", "stale_agent", "unsupported_agent", "unsupported_transport"} {
		t.Run(mode, func(t *testing.T) {
			ctx, pool, in, u, done := recoveryGrantFixture(t, 0, mode != "missing_evidence")
			defer done()
			exec := func(sql string, args ...any) {
				t.Helper()
				if _, err := pool.Exec(ctx, sql, args...); err != nil {
					t.Fatal(err)
				}
			}
			switch mode {
			case "no_context":
				ctx = context.Background()
			case "no_confirmation":
				in.ConfirmFenceRecovery = false
			case "context_permission":
				u.Permissions = nil
				ctx = auth.ContextWithUser(ctx, u)
			case "readonly":
				u.Roles = []string{"read_only"}
				ctx = auth.ContextWithUser(ctx, u)
			case "expired_session":
				exec(`UPDATE auth_sessions SET expires_at=clock_timestamp()-interval '1 second' WHERE id=$1::uuid`, u.SessionID)
			case "revoked_session":
				exec(`UPDATE auth_sessions SET revoked_at=clock_timestamp() WHERE id=$1::uuid`, u.SessionID)
			case "disabled_user":
				exec(`UPDATE users SET status='disabled' WHERE id=$1::uuid`, u.ID)
			case "service_user":
				exec(`UPDATE users SET user_type='agent' WHERE id=$1::uuid`, u.ID)
			case "removed_role":
				exec(`DELETE FROM user_roles WHERE user_id=$1::uuid`, u.ID)
			case "removed_permission":
				exec(`DELETE FROM role_permissions`)
			case "wrong_challenge":
				in.ChallengeHash = strings.Repeat("e", 64)
			case "wrong_evidence":
				in.EvidenceHash = strings.Repeat("e", 64)
			case "wrong_preparation":
				in.PreparationID = u.ID
			case "cancelled":
				if _, err := revocations.NewService(pool).CancelRuntimeRecovery(ctx, in.PreparationID, u.ID); err != nil {
					t.Fatal(err)
				}
			case "stale_agent":
				exec(`UPDATE agents SET last_authenticated_heartbeat_at=clock_timestamp()-interval '10 minutes'`)
			case "unsupported_agent":
				exec(`UPDATE agents SET current_runtime_mutation_fencing=false`)
			case "unsupported_transport":
				exec(`UPDATE agents SET capabilities=capabilities-'runtimeRecoveryObservationV1'`)
			}
			if _, err := revocations.NewService(pool).IssueRecoveryMutationGrant(ctx, in); err == nil {
				t.Fatal("unsafe issuance accepted")
			}
			var count int
			if err := pool.QueryRow(ctx, `SELECT count(*) FROM runtime_recovery_mutation_grants`).Scan(&count); err != nil || count != 0 {
				t.Fatal("partial grant persisted", count, err)
			}
		})
	}
}

func TestRuntimeMutationFencingRecoveryGrantExpiryRetainsReservation(t *testing.T) {
	ctx, pool, in, u, done := recoveryGrantFixture(t, time.Second, true)
	defer done()
	svc := revocations.NewService(pool)
	a, err := svc.IssueRecoveryMutationGrant(ctx, in)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = pool.Exec(ctx, `SELECT pg_sleep(1.1)`); err != nil {
		t.Fatal(err)
	}
	if _, err = svc.IssueRecoveryMutationGrant(ctx, in); err == nil {
		t.Fatal("expired grant renewed")
	}
	if _, err = svc.CancelRuntimeRecovery(ctx, in.PreparationID, u.ID); err == nil {
		t.Fatal("expired grant automatically released reservation")
	}
	var saved string
	if err = pool.QueryRow(ctx, `SELECT grant_payload FROM runtime_recovery_mutation_grants WHERE preparation_id=$1::uuid`, in.PreparationID).Scan(&saved); err != nil {
		t.Fatal(err)
	}
	wire, _ := json.Marshal(a.Grant)
	if saved != string(wire) {
		t.Fatal("expiry changed nonce/deadline")
	}
}

func TestRuntimeMutationFencingRecoveryGrantRetryRequiresCurrentAuthority(t *testing.T) {
	ctx, pool, in, u, done := recoveryGrantFixture(t, 0, true)
	defer done()
	svc := revocations.NewService(pool)
	a, err := svc.IssueRecoveryMutationGrant(ctx, in)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = pool.Exec(ctx, `DELETE FROM role_permissions`); err != nil {
		t.Fatal(err)
	}
	if _, err = svc.IssueRecoveryMutationGrant(ctx, in); err == nil {
		t.Fatal("retry reused stale authority")
	}
	if _, err = svc.CancelRuntimeRecovery(ctx, in.PreparationID, u.ID); err == nil {
		t.Fatal("authority loss released grant reservation")
	}
	var saved string
	if err = pool.QueryRow(ctx, `SELECT grant_payload FROM runtime_recovery_mutation_grants WHERE preparation_id=$1::uuid`, in.PreparationID).Scan(&saved); err != nil {
		t.Fatal(err)
	}
	wire, _ := json.Marshal(a.Grant)
	if saved != string(wire) {
		t.Fatal("authority loss changed grant")
	}
}

func TestRuntimeMutationFencingRecoveryGrantForeignActor(t *testing.T) {
	ctx, pool, in, u, done := recoveryGrantFixture(t, 0, true)
	defer done()
	var other, session string
	if err := pool.QueryRow(ctx, `INSERT INTO users(email,display_name,user_type,status) VALUES('other-grant-admin@isolated.invalid','Other admin','human','active') RETURNING id::text`).Scan(&other); err != nil {
		t.Fatal(err)
	}
	if err := pool.QueryRow(ctx, `INSERT INTO auth_sessions(user_id,token_hash,expires_at) VALUES($1::uuid,'other-grant-token',clock_timestamp()+interval '1 hour') RETURNING id::text`, other).Scan(&session); err != nil {
		t.Fatal(err)
	}
	if _, err := pool.Exec(ctx, `INSERT INTO user_roles(user_id,role_id) SELECT $1::uuid,id FROM roles WHERE code='admin'`, other); err != nil {
		t.Fatal(err)
	}
	u.ID = other
	u.SessionID = session
	if _, err := revocations.NewService(pool).IssueRecoveryMutationGrant(auth.ContextWithUser(ctx, u), in); err == nil {
		t.Fatal("foreign authorized actor rebound original approval")
	}
	var count int
	if err := pool.QueryRow(ctx, `SELECT count(*) FROM runtime_recovery_mutation_grants`).Scan(&count); err != nil || count != 0 {
		t.Fatal("foreign actor left partial grant", err)
	}
}

func TestRuntimeMutationFencingRecoveryGrantWaitExpiry(t *testing.T) {
	ctx, pool, in, _, done := recoveryGrantFixture(t, time.Second, true)
	defer done()
	var server string
	if err := pool.QueryRow(ctx, `SELECT server_id::text FROM runtime_recovery_preparations WHERE id=$1::uuid`, in.PreparationID).Scan(&server); err != nil {
		t.Fatal(err)
	}
	tx, err := pool.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer tx.Rollback(ctx)
	if _, err = tx.Exec(ctx, `SELECT id FROM servers WHERE id=$1::uuid FOR UPDATE`, server); err != nil {
		t.Fatal(err)
	}
	ch := make(chan error, 1)
	go func() { _, e := revocations.NewService(pool).IssueRecoveryMutationGrant(ctx, in); ch <- e }()
	deadline := time.Now().Add(time.Second)
	for {
		var waiting bool
		if err = pool.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM pg_stat_activity WHERE query LIKE '%SELECT id FROM servers WHERE id=%FOR UPDATE%' AND wait_event_type='Lock' AND pid<>pg_backend_pid())`).Scan(&waiting); err != nil {
			t.Fatal(err)
		}
		if waiting {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("issuer did not wait for node")
		}
		if _, err = pool.Exec(ctx, `SELECT pg_sleep(0.01)`); err != nil {
			t.Fatal(err)
		}
	}
	// Authority locks must not already be held behind the node wait.
	short, cancel := context.WithTimeout(ctx, 500*time.Millisecond)
	defer cancel()
	if _, err = pool.Exec(short, `UPDATE roles SET name=name WHERE code='admin'`); err != nil {
		t.Fatal("node wait blocked role maintenance", err)
	}
	if _, err = pool.Exec(ctx, `SELECT pg_sleep(1.1)`); err != nil {
		t.Fatal(err)
	}
	if err = tx.Commit(ctx); err != nil {
		t.Fatal(err)
	}
	if err = <-ch; err == nil {
		t.Fatal("expired evidence authorized after lock wait")
	}
	var count int
	if err = pool.QueryRow(ctx, `SELECT count(*) FROM runtime_recovery_mutation_grants`).Scan(&count); err != nil || count != 0 {
		t.Fatal("partial expired grant", err)
	}
}
