package db

import (
	"context"
	"encoding/json"
	"os"
	"strings"
	"testing"

	"github.com/ikaevus/routegate/backend/internal/agents"
	"github.com/ikaevus/routegate/backend/internal/revocations"
	"github.com/jackc/pgx/v5/pgxpool"
)

func recoveryPreparationFixture(t *testing.T) (context.Context, *pgxpool.Pool, revocations.RuntimeRecoveryInput, revocations.PrepareInput, func()) {
	t.Helper()
	ctx, pool, removal, _, done := removalPreparationFixture(t)
	in := revocations.RuntimeRecoveryInput{ServerID: removal.ServerID, MarkerHash: strings.Repeat("a", 64), ReceiptHash: strings.Repeat("b", 64), Actor: "operator", Envelope: json.RawMessage(`{"status":"succeeded","resultPayload":{"value":9007199254740993}}`)}
	if err := pool.QueryRow(ctx, `UPDATE agents SET capabilities=capabilities || '{"runtimeMutationFencingV1":true}'::jsonb WHERE server_id=$1::uuid RETURNING id::text`, in.ServerID).Scan(&in.AgentID); err != nil {
		done()
		t.Fatal(err)
	}
	var token string
	if err := pool.QueryRow(ctx, `SELECT token_hash FROM agents WHERE id=$1::uuid`, in.AgentID).Scan(&token); err != nil {
		done()
		t.Fatal(err)
	}
	repo := agents.NewRepository(pool)
	agent, err := repo.FindAgentByTokenHash(ctx, token)
	if err != nil {
		done()
		t.Fatal(err)
	}
	if _, err = repo.UpdateAgentHeartbeat(ctx, agents.UpdateAgentHeartbeatInput{TokenHash: token, Capabilities: agent.Capabilities}); err != nil {
		done()
		t.Fatal(err)
	}
	if err := pool.QueryRow(ctx, `INSERT INTO agent_operation_jobs(server_id,agent_id,kind,operation,status,result_payload,completed_at) VALUES($1::uuid,$2::uuid,'vpn_core_service','restart','succeeded','{"value":9007199254740993}',now()) RETURNING id::text`, in.ServerID, in.AgentID).Scan(&in.TaskID); err != nil {
		done()
		t.Fatal(err)
	}
	return ctx, pool, in, removal, done
}

func TestRuntimeMutationFencingRecoveryPreparation(t *testing.T) {
	ctx, pool, in, removal, done := recoveryPreparationFixture(t)
	defer done()
	snapshot := func() string {
		t.Helper()
		var v string
		if err := pool.QueryRow(ctx, `SELECT jsonb_build_array((SELECT jsonb_agg(to_jsonb(a)) FROM vpn_accounts a),(SELECT jsonb_agg(to_jsonb(j)) FROM config_apply_jobs j),(SELECT jsonb_agg(to_jsonb(j)) FROM agent_operation_jobs j),(SELECT jsonb_agg(to_jsonb(c)) FROM config_versions c),(SELECT jsonb_agg(to_jsonb(t)) FROM vpn_subscription_tokens t))::text`).Scan(&v); err != nil {
			t.Fatal(err)
		}
		return v
	}
	before := snapshot()
	svc := revocations.NewService(pool)
	p, err := svc.PrepareRuntimeRecovery(ctx, in)
	if err != nil || p.State != "prepared" {
		t.Fatal(p, err)
	}
	again, err := revocations.NewService(pool).PrepareRuntimeRecovery(ctx, in)
	if err != nil || again.ID != p.ID {
		t.Fatal("restart retry", again, err)
	}
	if snapshot() != before {
		t.Fatal("preparation changed accounts, tokens, versions or jobs")
	}
	altered := in
	altered.ReceiptHash = strings.Repeat("c", 64)
	if _, err = svc.PrepareRuntimeRecovery(ctx, altered); err == nil {
		t.Fatal("rebound existing reservation")
	}
	if _, err = svc.Prepare(ctx, removal); err == nil {
		t.Fatal("revocation bypassed recovery reservation")
	}
	for _, sql := range []string{
		`UPDATE servers SET vless_port=19443 WHERE id=$1::uuid`,
		`UPDATE agents SET token_hash='changed' WHERE server_id=$1::uuid`,
		`UPDATE vpn_accounts SET status='revoked' WHERE server_id=$1::uuid`,
		`UPDATE config_versions SET config_hash=repeat('f',64) WHERE server_id=$1::uuid`,
		`DELETE FROM agent_operation_jobs WHERE server_id=$1::uuid`,
		`INSERT INTO agent_operation_jobs(server_id,agent_id,kind,operation) SELECT server_id,id,'vpn_core_service','restart' FROM agents WHERE server_id=$1::uuid`,
	} {
		if _, err = pool.Exec(ctx, sql, in.ServerID); err == nil {
			t.Fatal("conflict accepted", sql)
		}
	}
	if _, err = pool.Exec(ctx, `INSERT INTO routing_profiles(name) VALUES('blocked recovery routing')`); err == nil {
		t.Fatal("routing mutation accepted")
	}
	if _, err = pool.Exec(ctx, `UPDATE agents SET last_seen_at=now(),last_authenticated_heartbeat_at=now() WHERE server_id=$1::uuid`, in.ServerID); err != nil {
		t.Fatal("heartbeat blocked", err)
	}
	for _, sql := range []string{`UPDATE runtime_recovery_preparations SET state='confirmed' WHERE id=$1::uuid`, `UPDATE runtime_recovery_preparations SET receipt_hash=repeat('f',64) WHERE id=$1::uuid`, `DELETE FROM runtime_recovery_preparations WHERE id=$1::uuid`, `DELETE FROM runtime_recovery_events WHERE operation_id=$1::uuid`} {
		if _, err = pool.Exec(ctx, sql, p.ID); err == nil {
			t.Fatal("history changed", sql)
		}
	}
	down, err := os.ReadFile("../../migrations/000161_runtime_recovery_preparations.down.sql")
	if err != nil {
		t.Fatal(err)
	}
	if _, err = pool.Exec(ctx, string(down)); err == nil {
		t.Fatal("downgrade discarded reservation")
	}
	for range 2 {
		if _, err = svc.CancelRuntimeRecovery(ctx, p.ID, "operator-2"); err != nil {
			t.Fatal(err)
		}
	}
	var events int
	if err = pool.QueryRow(ctx, `SELECT count(*) FROM runtime_recovery_events WHERE operation_id=$1::uuid`, p.ID).Scan(&events); err != nil || events != 2 {
		t.Fatal("non-idempotent audit", events, err)
	}
	if _, err = pool.Exec(ctx, string(down)); err == nil {
		t.Fatal("downgrade discarded cancelled history")
	}
	if _, err = pool.Exec(ctx, `UPDATE servers SET vless_port=19443 WHERE id=$1::uuid`, in.ServerID); err != nil {
		t.Fatal("cancellation retained Manager reservation", err)
	}
}

func TestRuntimeMutationFencingRecoveryRejectsUnsafePreparation(t *testing.T) {
	for _, mode := range []string{"payload", "duplicates", "failed", "wrong_agent", "stale", "legacy", "pending", "revocation", "unsupported"} {
		t.Run(mode, func(t *testing.T) {
			ctx, pool, in, removal, done := recoveryPreparationFixture(t)
			defer done()
			exec := func(sql string, args ...any) {
				t.Helper()
				if _, err := pool.Exec(ctx, sql, args...); err != nil {
					t.Fatal(err)
				}
			}
			switch mode {
			case "payload":
				in.Envelope = json.RawMessage(`{"status":"succeeded","resultPayload":{"value":9007199254740992}}`)
			case "duplicates":
				in.Envelope = json.RawMessage(`{"status":"failed","status":"succeeded"}`)
			case "failed":
				exec(`UPDATE agent_operation_jobs SET status='failed' WHERE id=$1::uuid`, in.TaskID)
			case "wrong_agent":
				in.AgentID = "aaaaaaaa-aaaa-4aaa-8aaa-aaaaaaaaaaaa"
			case "stale":
				exec(`UPDATE agents SET last_authenticated_heartbeat_at=now()-interval '10 minutes' WHERE id=$1::uuid`, in.AgentID)
			case "legacy":
				exec(`UPDATE agents SET capabilities=capabilities-'runtimeMutationFencingV1' WHERE id=$1::uuid`, in.AgentID)
			case "pending":
				exec(`INSERT INTO agent_operation_jobs(server_id,agent_id,kind,operation) VALUES($1::uuid,$2::uuid,'vpn_core_service','restart')`, in.ServerID, in.AgentID)
			case "unsupported":
				exec(`UPDATE agent_operation_jobs SET operation='stop' WHERE id=$1::uuid`, in.TaskID)
			case "revocation":
				if _, err := revocations.NewService(pool).Prepare(ctx, removal); err != nil {
					t.Fatal(err)
				}
			}
			if _, err := revocations.NewService(pool).PrepareRuntimeRecovery(ctx, in); err == nil {
				t.Fatal("unsafe preparation accepted")
			}
			var count int
			if err := pool.QueryRow(ctx, `SELECT (SELECT count(*) FROM runtime_recovery_preparations)+(SELECT count(*) FROM runtime_recovery_events)`).Scan(&count); err != nil || count != 0 {
				t.Fatal("failed preparation left durable state", count, err)
			}
		})
	}
}

func TestRuntimeMutationFencingRecoveryConcurrentPreparation(t *testing.T) {
	ctx, pool, in, _, done := recoveryPreparationFixture(t)
	defer done()
	type result struct {
		p   revocations.RecoveryPreparation
		err error
	}
	results := make(chan result, 2)
	for range 2 {
		go func() {
			p, err := revocations.NewService(pool).PrepareRuntimeRecovery(ctx, in)
			results <- result{p, err}
		}()
	}
	a, b := <-results, <-results
	if a.err != nil || b.err != nil || a.p.ID != b.p.ID {
		t.Fatal("concurrent requests not idempotent", a, b)
	}
	var events int
	if err := pool.QueryRow(ctx, `SELECT count(*) FROM runtime_recovery_events`).Scan(&events); err != nil || events != 1 {
		t.Fatal(events, err)
	}
}

func TestRuntimeMutationFencingRecoveryFreshnessUsesAdmissionTime(t *testing.T) {
	ctx, pool, in, _, done := recoveryPreparationFixture(t)
	defer done()
	tx, err := pool.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer tx.Rollback(ctx)
	// now() is frozen at transaction start: it must not prolong readiness while
	// admission waits. Only this disposable test transaction delays for 2s.
	if _, err = tx.Exec(ctx, `UPDATE agents SET last_authenticated_heartbeat_at=now()-interval '89 seconds' WHERE id=$1::uuid`, in.AgentID); err != nil {
		t.Fatal(err)
	}
	if _, err = tx.Exec(ctx, `SELECT pg_sleep(2)`); err != nil {
		t.Fatal(err)
	}
	_, err = tx.Exec(ctx, `INSERT INTO runtime_recovery_preparations(server_id,agent_id,agent_credential_generation,task_id,marker_hash,receipt_hash,envelope_hash,requested_by,last_action_by)
	SELECT server_id,id,credential_generation,$2::uuid,repeat('a',64),repeat('b',64),repeat('c',64),'operator','operator' FROM agents WHERE id=$1::uuid`, in.AgentID, in.TaskID)
	if err == nil || !strings.Contains(err.Error(), "runtime_recovery_agent_not_ready") {
		t.Fatal("expired heartbeat admitted", err)
	}
}

func TestRuntimeMutationFencingRecoveryRequiresCurrentAdvertisement(t *testing.T) {
	for _, mode := range []string{"omitted", "false", "empty", "replacement", "unauthenticated"} {
		t.Run(mode, func(t *testing.T) {
			ctx, pool, in, _, done := recoveryPreparationFixture(t)
			defer done()
			var token string
			if err := pool.QueryRow(ctx, `SELECT token_hash FROM agents WHERE id=$1::uuid`, in.AgentID).Scan(&token); err != nil {
				t.Fatal(err)
			}
			repo := agents.NewRepository(pool)
			input := agents.UpdateAgentHeartbeatInput{TokenHash: token}
			switch mode {
			case "false":
				input.Capabilities = agents.Capabilities{"runtimeMutationFencingV1": false}
			case "empty":
				input.Capabilities = agents.Capabilities{}
			case "replacement":
				if _, err := repo.CreateOrReplaceAgentForServer(ctx, agents.CreateOrReplaceAgentInput{ServerID: in.ServerID, AgentVersion: "old", TokenHash: "recovery-replacement", Capabilities: agents.Capabilities{}}); err != nil {
					t.Fatal(err)
				}
				input.TokenHash = "recovery-replacement"
			case "unauthenticated":
				input.TokenHash = ""
				input.AgentID = in.AgentID
				input.Capabilities = agents.Capabilities{"runtimeMutationFencingV1": true}
			}
			a, err := repo.UpdateAgentHeartbeat(ctx, input)
			if err != nil {
				t.Fatal(err)
			}
			if !a.Capabilities.RuntimeMutationFencingEnabled() {
				t.Fatal("sticky protection lost")
			}
			var supported bool
			if err = pool.QueryRow(ctx, `SELECT current_runtime_mutation_fencing FROM agents WHERE id=$1::uuid`, in.AgentID).Scan(&supported); err != nil || supported {
				t.Fatal("unsupported current advert", supported, err)
			}
			if _, err = revocations.NewService(pool).PrepareRuntimeRecovery(ctx, in); err == nil {
				t.Fatal("sticky policy mistaken for current support")
			}
		})
	}
}
