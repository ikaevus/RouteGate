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

func observationApprovalFixture(t *testing.T) (context.Context, *pgxpool.Pool, revocations.RecoveryObservationApprovalInput, auth.AuthenticatedUser, func()) {
	t.Helper()
	ctx, pool, in, _, done := recoveryPreparationFixture(t)
	p, err := revocations.NewService(pool).PrepareRuntimeRecovery(ctx, in)
	if err != nil {
		done()
		t.Fatal(err)
	}
	u := auth.AuthenticatedUser{UserProfile: auth.UserProfile{UserType: "human", Status: "active", Roles: []string{"admin"}, Permissions: []string{"configs:apply"}}}
	if err = pool.QueryRow(ctx, `INSERT INTO users(email,display_name,user_type,status) VALUES('recovery-admin@isolated.invalid','Test administrator','human','active') RETURNING id::text`).Scan(&u.ID); err != nil {
		done()
		t.Fatal(err)
	}
	if err = pool.QueryRow(ctx, `INSERT INTO auth_sessions(user_id,token_hash,expires_at) VALUES($1::uuid,'isolated-approval-token',clock_timestamp()+interval '1 hour') RETURNING id::text`, u.ID).Scan(&u.SessionID); err != nil {
		done()
		t.Fatal(err)
	}
	if _, err = pool.Exec(ctx, `INSERT INTO roles(code,name) VALUES('admin','Admin') ON CONFLICT(code) DO NOTHING; INSERT INTO permissions(code,name) VALUES('configs:apply','Apply config') ON CONFLICT(code) DO NOTHING`); err != nil {
		done()
		t.Fatal(err)
	}
	if _, err = pool.Exec(ctx, `INSERT INTO user_roles(user_id,role_id) SELECT $1::uuid,id FROM roles WHERE code='admin'`, u.ID); err != nil {
		done()
		t.Fatal(err)
	}
	if _, err = pool.Exec(ctx, `INSERT INTO role_permissions(role_id,permission_id) SELECT r.id,p.id FROM roles r,permissions p WHERE r.code='admin' AND p.code='configs:apply' ON CONFLICT DO NOTHING`); err != nil {
		done()
		t.Fatal(err)
	}
	hash := sha256.Sum256(in.Envelope)
	o := revocations.RecoveryObservation{SchemaVersion: 1, TaskID: in.TaskID, AgentID: in.AgentID, ServerID: in.ServerID, MarkerHash: in.MarkerHash, ReceiptHash: in.ReceiptHash, EnvelopeHash: hex.EncodeToString(hash[:]), Witness: revocations.RecoveryWitness{SchemaVersion: 1, RuntimeHash: strings.Repeat("c", 64), Generation: revocations.RuntimeGeneration{InvocationID: strings.Repeat("b", 32), PID: 124, StartedMonotonic: "200"}, PreviousGeneration: revocations.RuntimeGeneration{InvocationID: strings.Repeat("a", 32), PID: 123, StartedMonotonic: "100"}, ListenerPort: 443}}
	raw, err := json.Marshal(o)
	if err != nil {
		done()
		t.Fatal(err)
	}
	return auth.ContextWithUser(ctx, u), pool, revocations.RecoveryObservationApprovalInput{PreparationID: p.ID, Expected: raw, ConfirmReadOnlyObservation: true}, u, done
}

func TestRuntimeMutationFencingObservationApproval(t *testing.T) {
	ctx, pool, in, u, done := observationApprovalFixture(t)
	defer done()
	svc := revocations.NewService(pool)
	a, err := svc.ApproveRecoveryObservation(ctx, in)
	if err != nil {
		t.Fatal(err)
	}
	b, err := revocations.NewService(pool).ApproveRecoveryObservation(ctx, in)
	if err != nil || a != b {
		t.Fatal("retry changed challenge", err)
	}
	data, _ := json.Marshal(a.Challenge)
	sum := sha256.Sum256(data)
	if a.ChallengeHash != hex.EncodeToString(sum[:]) || a.Challenge.OperationID != in.PreparationID || len(a.Challenge.Nonce) != 64 || a.Challenge.ExpiresAt.Sub(a.Challenge.IssuedAt) != 2*time.Minute {
		t.Fatal("incorrect challenge binding", a)
	}
	var count int
	var actor, state string
	if err = pool.QueryRow(ctx, `SELECT actor_user_id::text FROM runtime_recovery_observation_approvals WHERE preparation_id=$1::uuid`, in.PreparationID).Scan(&actor); err != nil || actor != u.ID {
		t.Fatal("actor was not authenticated user", actor, err)
	}
	if err = pool.QueryRow(ctx, `SELECT state FROM runtime_recovery_preparations WHERE id=$1::uuid`, in.PreparationID).Scan(&state); err != nil || state != "prepared" {
		t.Fatal("approval released reservation", state, err)
	}
	if err = pool.QueryRow(ctx, `SELECT count(*) FROM runtime_recovery_observation_approvals`).Scan(&count); err != nil || count != 1 {
		t.Fatal(count, err)
	}
	for _, sql := range []string{`DELETE FROM runtime_recovery_observation_approvals WHERE preparation_id=$1::uuid`, `UPDATE runtime_recovery_observation_approvals SET expires_at=expires_at+interval '1 second' WHERE preparation_id=$1::uuid`} {
		if _, err = pool.Exec(ctx, sql, in.PreparationID); err == nil {
			t.Fatal("approval was mutable")
		}
	}
	if _, err = svc.CancelRuntimeRecovery(ctx, in.PreparationID, u.ID); err != nil {
		t.Fatal(err)
	}
	if _, err = svc.ApproveRecoveryObservation(ctx, in); err == nil {
		t.Fatal("cancelled preparation reused approval")
	}
	down, err := os.ReadFile("../../migrations/000162_recovery_observation_approvals.down.sql")
	if err != nil {
		t.Fatal(err)
	}
	if _, err = pool.Exec(ctx, string(down)); err == nil {
		t.Fatal("downgrade discarded approval audit")
	}
}

func TestRuntimeMutationFencingObservationApprovalRejects(t *testing.T) {
	for _, mode := range []string{"no_context", "no_confirmation", "readonly", "permission_context", "expired_session", "revoked_session", "disabled_user", "removed_role", "removed_permission", "stale_agent", "unsupported_agent", "wrong_task", "wrong_receipt", "malformed", "cancelled"} {
		t.Run(mode, func(t *testing.T) {
			ctx, pool, in, u, done := observationApprovalFixture(t)
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
				in.ConfirmReadOnlyObservation = false
			case "readonly":
				u.Roles = []string{"read_only"}
				ctx = auth.ContextWithUser(ctx, u)
			case "permission_context":
				u.Permissions = nil
				ctx = auth.ContextWithUser(ctx, u)
			case "expired_session":
				exec(`UPDATE auth_sessions SET expires_at=clock_timestamp()-interval '1 second' WHERE id=$1::uuid`, u.SessionID)
			case "revoked_session":
				exec(`UPDATE auth_sessions SET revoked_at=clock_timestamp() WHERE id=$1::uuid`, u.SessionID)
			case "disabled_user":
				exec(`UPDATE users SET status='disabled' WHERE id=$1::uuid`, u.ID)
			case "removed_role":
				exec(`DELETE FROM user_roles WHERE user_id=$1::uuid`, u.ID)
			case "removed_permission":
				exec(`DELETE FROM role_permissions`)
			case "stale_agent":
				exec(`UPDATE agents SET last_authenticated_heartbeat_at=clock_timestamp()-interval '10 minutes'`)
			case "unsupported_agent":
				exec(`UPDATE agents SET current_runtime_mutation_fencing=false`)
			case "malformed":
				in.Expected = json.RawMessage(`{"schemaVersion":1,"schemaVersion":1}`)
			case "wrong_task", "wrong_receipt":
				var o revocations.RecoveryObservation
				if err := json.Unmarshal(in.Expected, &o); err != nil {
					t.Fatal(err)
				}
				if mode == "wrong_task" {
					o.TaskID = u.ID
				} else {
					o.ReceiptHash = strings.Repeat("f", 64)
				}
				in.Expected, _ = json.Marshal(o)
			case "cancelled":
				if _, err := revocations.NewService(pool).CancelRuntimeRecovery(ctx, in.PreparationID, u.ID); err != nil {
					t.Fatal(err)
				}
			}
			if _, err := revocations.NewService(pool).ApproveRecoveryObservation(ctx, in); err == nil {
				t.Fatal("unsafe approval accepted")
			}
			var count int
			if err := pool.QueryRow(ctx, `SELECT count(*) FROM runtime_recovery_observation_approvals`).Scan(&count); err != nil || count != 0 {
				t.Fatal("partial approval persisted", count, err)
			}
		})
	}
}

func TestRuntimeMutationFencingObservationApprovalConcurrent(t *testing.T) {
	ctx, pool, in, _, done := observationApprovalFixture(t)
	defer done()
	type result struct {
		a   revocations.RecoveryObservationApproval
		err error
	}
	ch := make(chan result, 2)
	for range 2 {
		go func() {
			a, err := revocations.NewService(pool).ApproveRecoveryObservation(ctx, in)
			ch <- result{a, err}
		}()
	}
	a, b := <-ch, <-ch
	if a.err != nil || b.err != nil || a.a != b.a {
		t.Fatal("concurrent approval differs", a.err, b.err)
	}
}

func TestRuntimeMutationFencingObservationApprovalExpiredRetry(t *testing.T) {
	ctx, pool, in, u, done := observationApprovalFixture(t)
	defer done()
	var expected revocations.RecoveryObservation
	if err := json.Unmarshal(in.Expected, &expected); err != nil {
		t.Fatal(err)
	}
	var now time.Time
	if err := pool.QueryRow(ctx, `SELECT clock_timestamp()`).Scan(&now); err != nil {
		t.Fatal(err)
	}
	// Seed a once-valid request near the end of its lifetime, without weakening
	// immutability or disabling triggers; expiry must not cause automatic renewal.
	c := revocations.RecoveryChallenge{SchemaVersion: 1, OperationID: in.PreparationID, Nonce: strings.Repeat("d", 64), IssuedAt: now.UTC().Add(-119 * time.Second), ExpiresAt: now.UTC().Add(time.Second), Expected: expected}
	data, _ := json.Marshal(c)
	sum := sha256.Sum256(data)
	if _, err := pool.Exec(ctx, `INSERT INTO runtime_recovery_observation_approvals(preparation_id,actor_user_id,actor_session_id,challenge,challenge_hash,issued_at,expires_at) VALUES($1::uuid,$2::uuid,$3::uuid,$4,$5,$6,$7)`, in.PreparationID, u.ID, u.SessionID, string(data), hex.EncodeToString(sum[:]), c.IssuedAt, c.ExpiresAt); err != nil {
		t.Fatal(err)
	}
	if _, err := pool.Exec(ctx, `SELECT pg_sleep(1.1)`); err != nil {
		t.Fatal(err)
	}
	if _, err := revocations.NewService(pool).ApproveRecoveryObservation(ctx, in); err == nil {
		t.Fatal("expired challenge renewed")
	}
	var saved string
	if err := pool.QueryRow(ctx, `SELECT challenge FROM runtime_recovery_observation_approvals WHERE preparation_id=$1::uuid`, in.PreparationID).Scan(&saved); err != nil || saved != string(data) {
		t.Fatal("expired challenge changed", err)
	}
}

func TestRuntimeMutationFencingObservationApprovalDoesNotBlockOtherSessions(t *testing.T) {
	ctx, pool, in, _, done := observationApprovalFixture(t)
	defer done()
	var otherUser, otherSession string
	if err := pool.QueryRow(ctx, `INSERT INTO users(email,display_name) VALUES('other@isolated.invalid','Other') RETURNING id::text`).Scan(&otherUser); err != nil {
		t.Fatal(err)
	}
	if err := pool.QueryRow(ctx, `INSERT INTO auth_sessions(user_id,token_hash,expires_at) VALUES($1::uuid,'other-token',clock_timestamp()+interval '1 hour') RETURNING id::text`, otherUser).Scan(&otherSession); err != nil {
		t.Fatal(err)
	}
	blocker, err := pool.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer blocker.Rollback(ctx)
	if _, err = blocker.Exec(ctx, `SELECT id FROM servers WHERE id=(SELECT server_id FROM runtime_recovery_preparations WHERE id=$1::uuid) FOR UPDATE`, in.PreparationID); err != nil {
		t.Fatal(err)
	}
	result := make(chan error, 1)
	go func() { _, err := revocations.NewService(pool).ApproveRecoveryObservation(ctx, in); result <- err }()
	deadline := time.Now().Add(time.Second)
	for {
		var locked bool
		if err = pool.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM pg_locks WHERE relation='roles'::regclass AND mode='ShareLock' AND granted AND pid<>pg_backend_pid())`).Scan(&locked); err != nil {
			t.Fatal(err)
		}
		if locked {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("approval did not acquire authority lock")
		}
		if _, err = pool.Exec(ctx, `SELECT pg_sleep(0.01)`); err != nil {
			t.Fatal(err)
		}
	}
	short, cancel := context.WithTimeout(ctx, 500*time.Millisecond)
	defer cancel()
	if _, err = pool.Exec(short, `UPDATE auth_sessions SET last_used_at=clock_timestamp() WHERE id=$1::uuid`, otherSession); err != nil {
		t.Fatal("unrelated session blocked", err)
	}
	if err = blocker.Commit(ctx); err != nil {
		t.Fatal(err)
	}
	if err = <-result; err != nil {
		t.Fatal("approval failed after node released", err)
	}
}
