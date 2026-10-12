package db

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/ikaevus/routegate/backend/internal/agents"
	"github.com/ikaevus/routegate/backend/internal/revocations"
	"github.com/jackc/pgx/v5/pgconn"
)

func TestRuntimeMutationFencingRecoveryTransportCurrentCapability(t *testing.T) {
	for _, mode := range []string{"old_agent", "false", "omitted", "nil", "identity_only", "replacement"} {
		t.Run(mode, func(t *testing.T) {
			ctx, pool, in, u, done := observationApprovalFixture(t)
			defer done()
			var token, agentID string
			if err := pool.QueryRow(ctx, `SELECT a.token_hash,a.id::text FROM agents a JOIN runtime_recovery_preparations p ON p.agent_id=a.id WHERE p.id=$1::uuid`, in.PreparationID).Scan(&token, &agentID); err != nil {
				t.Fatal(err)
			}
			svc := revocations.NewService(pool)
			repo := agents.NewRepository(pool)
			if mode == "replacement" {
				if _, err := svc.CancelRuntimeRecovery(ctx, in.PreparationID, u.ID); err != nil {
					t.Fatal(err)
				}
				var serverID string
				if err := pool.QueryRow(ctx, `SELECT server_id::text FROM agents WHERE id=$1::uuid`, agentID).Scan(&serverID); err != nil {
					t.Fatal(err)
				}
				if _, err := repo.CreateOrReplaceAgentForServer(ctx, agents.CreateOrReplaceAgentInput{ServerID: serverID, TokenHash: "replacement-isolated", Capabilities: agents.Capabilities{"runtimeMutationFencingV1": true, "runtimeRecoveryObservationV1": true}}); err != nil {
					t.Fatal(err)
				}
			} else {
				input := agents.UpdateAgentHeartbeatInput{TokenHash: token, Capabilities: agents.Capabilities{"runtimeMutationFencingV1": true}}
				switch mode {
				case "false":
					input.Capabilities["runtimeRecoveryObservationV1"] = false
				case "nil":
					input.Capabilities = nil
				case "identity_only":
					input.TokenHash = ""
					input.AgentID = agentID
					input.Capabilities["runtimeRecoveryObservationV1"] = true
				}
				if _, err := repo.UpdateAgentHeartbeat(ctx, input); err != nil {
					t.Fatal(err)
				}
			}
			var capable, sticky bool
			if err := pool.QueryRow(ctx, `SELECT capabilities @> '{"runtimeRecoveryObservationV1":true}'::jsonb,capabilities @> '{"runtimeMutationFencingV1":true}'::jsonb FROM agents WHERE id=$1::uuid`, agentID).Scan(&capable, &sticky); err != nil || capable || !sticky {
				t.Fatal("current support retained or sticky safety lost", err)
			}
			if _, err := svc.ApproveRecoveryObservation(ctx, in); err == nil {
				t.Fatal("unsupported transport approved")
			}
		})
	}
}

func TestRuntimeMutationFencingRecoveryTransportWaitInvalidation(t *testing.T) {
	for _, mode := range []string{"cancelled", "expired"} {
		t.Run(mode, func(t *testing.T) {
			ctx, pool, in, u, done := observationApprovalFixture(t)
			defer done()
			var o revocations.RecoveryObservation
			if json.Unmarshal(in.Expected, &o) != nil {
				t.Fatal("bad expected")
			}
			var now time.Time
			if err := pool.QueryRow(ctx, `SELECT clock_timestamp()`).Scan(&now); err != nil {
				t.Fatal(err)
			}
			c := revocations.RecoveryChallenge{SchemaVersion: 1, OperationID: in.PreparationID, Nonce: strings.Repeat("d", 64), IssuedAt: now.UTC().Add(-time.Second), ExpiresAt: now.UTC().Add(750 * time.Millisecond), Expected: o}
			wire, _ := json.Marshal(c)
			sum := sha256.Sum256(wire)
			hash := hex.EncodeToString(sum[:])
			if _, err := pool.Exec(ctx, `INSERT INTO runtime_recovery_observation_approvals(preparation_id,actor_user_id,actor_session_id,challenge,challenge_hash,issued_at,expires_at) VALUES($1::uuid,$2::uuid,$3::uuid,$4,$5,$6,$7)`, in.PreparationID, u.ID, u.SessionID, string(wire), hash, c.IssuedAt, c.ExpiresAt); err != nil {
				t.Fatal(err)
			}
			var token string
			if err := pool.QueryRow(ctx, `SELECT token_hash FROM agents WHERE id=$1::uuid`, o.AgentID).Scan(&token); err != nil {
				t.Fatal(err)
			}
			response, _ := json.Marshal(revocations.RecoveryChallengeResponse{SchemaVersion: 1, OperationID: in.PreparationID, ChallengeHash: hash, ObservedAt: now.UTC(), Observation: o})
			blocker, err := pool.Begin(ctx)
			if err != nil {
				t.Fatal(err)
			}
			defer blocker.Rollback(ctx)
			if _, err = blocker.Exec(ctx, `SELECT id FROM servers WHERE id=$1::uuid FOR UPDATE`, o.ServerID); err != nil {
				t.Fatal(err)
			}
			result := make(chan error, 1)
			go func() {
				_, err := revocations.NewService(pool).AcceptRecoveryEvidence(ctx, token, in.PreparationID, response)
				result <- err
			}()
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
					t.Fatal("acceptance did not wait")
				}
				if _, err = pool.Exec(ctx, `SELECT pg_sleep(0.01)`); err != nil {
					t.Fatal(err)
				}
			}
			if mode == "cancelled" {
				if _, err = blocker.Exec(ctx, `UPDATE runtime_recovery_preparations SET state='cancelled',cancelled_at=clock_timestamp(),last_action_by=$2 WHERE id=$1::uuid`, in.PreparationID, u.ID); err != nil {
					t.Fatal(err)
				}
			} else {
				if _, err = pool.Exec(ctx, `SELECT pg_sleep(0.8)`); err != nil {
					t.Fatal(err)
				}
			}
			if err = blocker.Commit(ctx); err != nil {
				t.Fatal(err)
			}
			if err = <-result; err == nil {
				t.Fatal("accepted invalidated request after lock wait")
			}
			var count int
			if err = pool.QueryRow(ctx, `SELECT count(*) FROM runtime_recovery_observation_evidence`).Scan(&count); err != nil || count != 0 {
				t.Fatal("partial evidence", err)
			}
		})
	}
}

func TestRuntimeMutationFencingRecoveryTransport(t *testing.T) {
	ctx, pool, in, u, done := observationApprovalFixture(t)
	defer done()
	snapshot := func() string {
		t.Helper()
		var value string
		if err := pool.QueryRow(ctx, `SELECT jsonb_build_array((SELECT jsonb_agg(to_jsonb(a)) FROM vpn_accounts a),(SELECT jsonb_agg(to_jsonb(j)) FROM config_apply_jobs j),(SELECT jsonb_agg(to_jsonb(j)) FROM agent_operation_jobs j),(SELECT jsonb_agg(to_jsonb(c)) FROM config_versions c),(SELECT jsonb_agg(to_jsonb(t)) FROM vpn_subscription_tokens t))::text`).Scan(&value); err != nil {
			t.Fatal(err)
		}
		return value
	}
	before := snapshot()
	svc := revocations.NewService(pool)
	approval, err := svc.ApproveRecoveryObservation(ctx, in)
	if err != nil {
		t.Fatal(err)
	}
	var token string
	if err = pool.QueryRow(ctx, `SELECT token_hash FROM agents WHERE id=$1::uuid`, approval.Challenge.Expected.AgentID).Scan(&token); err != nil {
		t.Fatal(err)
	}
	c, err := svc.NextRecoveryChallenge(ctx, token)
	if err != nil || c == nil || *c != approval.Challenge {
		t.Fatal("delivery changed challenge", err)
	}
	// Force text rounding to differ from pgx binary timestamp truncation.
	// The exact nanoseconds remain in immutable response/hash; the column is
	// derived with PostgreSQL's same parser used by the binding trigger.
	r := revocations.RecoveryChallengeResponse{SchemaVersion: 1, OperationID: in.PreparationID, ChallengeHash: approval.ChallengeHash, ObservedAt: time.Now().UTC().Truncate(time.Microsecond).Add(999 * time.Nanosecond), Observation: c.Expected}
	data, _ := json.Marshal(r)
	type result struct {
		ack revocations.RecoveryEvidenceAcknowledgement
		err error
	}
	ch := make(chan result, 2)
	for range 2 {
		go func() {
			a, e := revocations.NewService(pool).AcceptRecoveryEvidence(ctx, token, in.PreparationID, data)
			ch <- result{a, e}
		}()
	}
	a, b := <-ch, <-ch
	if a.err != nil || b.err != nil || a.ack != b.ack || !a.ack.Accepted {
		t.Fatal("concurrent retry failed", a.err, b.err)
	}
	if c, err = svc.NextRecoveryChallenge(ctx, token); err != nil || c != nil {
		t.Fatal("accepted challenge redelivered", err)
	}
	r.ObservedAt = r.ObservedAt.Add(time.Nanosecond)
	changed, _ := json.Marshal(r)
	if _, err = svc.AcceptRecoveryEvidence(ctx, token, in.PreparationID, changed); err == nil {
		t.Fatal("accepted evidence overwritten")
	}
	var saved, state string
	var count int
	if err = pool.QueryRow(ctx, `SELECT response FROM runtime_recovery_observation_evidence WHERE preparation_id=$1::uuid`, in.PreparationID).Scan(&saved); err != nil || saved != string(data) {
		t.Fatal("exact evidence lost", err)
	}
	if err = pool.QueryRow(ctx, `SELECT state FROM runtime_recovery_preparations WHERE id=$1::uuid`, in.PreparationID).Scan(&state); err != nil || state != "prepared" {
		t.Fatal("evidence released reservation", err)
	}
	if err = pool.QueryRow(ctx, `SELECT count(*) FROM runtime_recovery_observation_evidence`).Scan(&count); err != nil || count != 1 {
		t.Fatal(count, err)
	}
	for _, sql := range []string{`DELETE FROM runtime_recovery_observation_evidence`, `UPDATE runtime_recovery_observation_evidence SET accepted_at=clock_timestamp()`} {
		if _, err = pool.Exec(ctx, sql); err == nil {
			t.Fatal("evidence mutable")
		}
	}
	down, err := os.ReadFile("../../migrations/000163_recovery_observation_evidence.down.sql")
	if err != nil {
		t.Fatal(err)
	}
	if _, err = pool.Exec(ctx, string(down)); err == nil {
		t.Fatal("downgrade discarded evidence")
	}
	if snapshot() != before {
		t.Fatal("observation transport changed account/token/config/job state")
	}
	if _, err = svc.CancelRuntimeRecovery(ctx, in.PreparationID, u.ID); err != nil {
		t.Fatal(err)
	}
	if _, err = svc.AcceptRecoveryEvidence(ctx, token, in.PreparationID, data); err == nil {
		t.Fatal("cancelled retry accepted")
	}
}

func TestRuntimeMutationFencingRecoveryTransportRejects(t *testing.T) {
	for _, mode := range []string{"token", "generation", "capability", "sticky_only", "stale", "heartbeat_generation", "cancelled", "operation", "hash", "nonce", "task", "agent", "server", "marker", "receipt", "envelope", "runtime", "generation_witness", "previous", "port", "future", "before_issued", "expired", "duplicate", "unknown", "trailing", "oversize"} {
		t.Run(mode, func(t *testing.T) {
			ctx, pool, in, u, done := observationApprovalFixture(t)
			defer done()
			svc := revocations.NewService(pool)
			a, err := svc.ApproveRecoveryObservation(ctx, in)
			if err != nil {
				t.Fatal(err)
			}
			var token string
			if err = pool.QueryRow(ctx, `SELECT token_hash FROM agents WHERE id=$1::uuid`, a.Challenge.Expected.AgentID).Scan(&token); err != nil {
				t.Fatal(err)
			}
			r := revocations.RecoveryChallengeResponse{SchemaVersion: 1, OperationID: in.PreparationID, ChallengeHash: a.ChallengeHash, ObservedAt: time.Now().UTC(), Observation: a.Challenge.Expected}
			exec := func(sql string, args ...any) {
				t.Helper()
				if _, e := pool.Exec(ctx, sql, args...); e != nil {
					t.Fatal(e)
				}
			}
			switch mode {
			case "token":
				token = "foreign-token"
			case "generation":
				// Reservation guards already prohibit generation replacement;
				// exercise that rejection rather than bypassing the guard to
				// manufacture an unreachable row for the transport verifier.
				_, err = pool.Exec(ctx, `UPDATE agents SET credential_generation=credential_generation+1,last_authenticated_heartbeat_generation=credential_generation+1`)
				var pgErr *pgconn.PgError
				if !errors.As(err, &pgErr) || pgErr.Code != "P0142" {
					t.Fatal("generation replacement was not fenced", err)
				}
				var unchanged bool
				if err = pool.QueryRow(ctx, `SELECT a.credential_generation=p.agent_credential_generation FROM agents a JOIN runtime_recovery_preparations p ON p.agent_id=a.id WHERE p.id=$1::uuid`, in.PreparationID).Scan(&unchanged); err != nil || !unchanged {
					t.Fatal("generation changed", err)
				}
				return
			case "capability":
				exec(`UPDATE agents SET capabilities='{}'`)
			case "sticky_only":
				exec(`UPDATE agents SET current_runtime_mutation_fencing=false`)
			case "stale":
				exec(`UPDATE agents SET last_authenticated_heartbeat_at=clock_timestamp()-interval '10 minutes'`)
			case "heartbeat_generation":
				// Database constraints prohibit a non-null mismatched generation.
				// Clearing the proof pair models no current authenticated proof.
				exec(`UPDATE agents SET last_authenticated_heartbeat_generation=NULL,last_authenticated_heartbeat_at=NULL`)
			case "cancelled":
				if _, err = svc.CancelRuntimeRecovery(ctx, in.PreparationID, u.ID); err != nil {
					t.Fatal(err)
				}
			case "operation":
				r.OperationID = u.ID
			case "hash", "nonce":
				r.ChallengeHash = strings.Repeat("f", 64)
			case "task":
				r.Observation.TaskID = u.ID
			case "agent":
				r.Observation.AgentID = u.ID
			case "server":
				r.Observation.ServerID = u.ID
			case "marker":
				r.Observation.MarkerHash = strings.Repeat("f", 64)
			case "receipt":
				r.Observation.ReceiptHash = strings.Repeat("f", 64)
			case "envelope":
				r.Observation.EnvelopeHash = strings.Repeat("f", 64)
			case "runtime":
				r.Observation.Witness.RuntimeHash = strings.Repeat("f", 64)
			case "generation_witness":
				r.Observation.Witness.Generation.PID++
			case "previous":
				r.Observation.Witness.PreviousGeneration.PID++
			case "port":
				r.Observation.Witness.ListenerPort++
			case "future":
				r.ObservedAt = time.Now().Add(time.Minute)
			case "before_issued":
				r.ObservedAt = a.Challenge.IssuedAt.Add(-time.Nanosecond)
			case "expired":
				r.ObservedAt = a.Challenge.ExpiresAt
			}
			data, _ := json.Marshal(r)
			switch mode {
			case "duplicate":
				data = append([]byte(`{"schemaVersion":1,`), data[1:]...)
			case "unknown":
				data = append([]byte(`{"untrusted":"secret",`), data[1:]...)
			case "trailing":
				data = append(data, []byte(`{}`)...)
			case "oversize":
				data = append([]byte(strings.Repeat(" ", 8192)), data...)
			}
			if _, err = svc.AcceptRecoveryEvidence(ctx, token, in.PreparationID, data); err == nil {
				t.Fatal("unsafe response accepted")
			}
			var count int
			if err = pool.QueryRow(ctx, `SELECT count(*) FROM runtime_recovery_observation_evidence`).Scan(&count); err != nil || count != 0 {
				t.Fatal("partial evidence persisted", count, err)
			}
		})
	}
}
