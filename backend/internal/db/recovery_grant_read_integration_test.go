package db

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/ikaevus/routegate/backend/internal/agents"
	"github.com/ikaevus/routegate/backend/internal/revocations"
)

func recoveryTestHash(data []byte) string {
	sum := sha256.Sum256(data)
	return hex.EncodeToString(sum[:])
}

func TestRuntimeMutationFencingRecoveryGrantRead(t *testing.T) {
	for _, mode := range []string{"valid", "wrong_token", "wrong_generation", "wrong_agent", "wrong_server", "wrong_operation", "missing_grant", "false_capability", "omitted_capability", "nil_capability", "identity_heartbeat", "stale_agent", "revoked_issuer", "expired_session", "removed_role", "removed_permission", "disabled_issuer", "expired_grant"} {
		t.Run(mode, func(t *testing.T) {
			lifetime := time.Duration(0)
			if mode == "expired_grant" {
				lifetime = time.Second
			}
			ctx, pool, in, u, done := recoveryGrantFixture(t, lifetime, true)
			defer done()
			svc := revocations.NewService(pool)
			repo := agents.NewRepository(pool)
			var g revocations.RecoveryMutationGrantApproval
			var err error
			if mode != "missing_grant" {
				g, err = svc.IssueRecoveryMutationGrant(ctx, in)
				if err != nil {
					t.Fatal(err)
				}
			}
			var token, agentID, serverID string
			var generation int64
			if err = pool.QueryRow(ctx, `SELECT a.token_hash,a.id::text,a.server_id::text,a.credential_generation FROM agents a JOIN runtime_recovery_preparations p ON p.agent_id=a.id WHERE p.id=$1::uuid`, in.PreparationID).Scan(&token, &agentID, &serverID, &generation); err != nil {
				t.Fatal(err)
			}
			caps := agents.Capabilities{"runtimeMutationFencingV1": true, "runtimeRecoveryObservationV1": true, "runtimeRecoveryGrantReadV1": true}
			if _, err = repo.UpdateAgentHeartbeat(ctx, agents.UpdateAgentHeartbeatInput{TokenHash: token, Capabilities: caps}); err != nil {
				t.Fatal(err)
			}
			identity, err := repo.ReadCredentialIdentity(ctx, token, agents.CredentialIdentityInput{SchemaVersion: 1, AgentID: agentID, ServerID: serverID, Nonce: strings.Repeat("a", 64)})
			if err != nil || identity.AgentCredentialGeneration != generation || identity.AgentID != agentID || identity.ServerID != serverID {
				t.Fatal("current identity", err)
			}
			request := revocations.RecoveryGrantReadInput{SchemaVersion: 1, OperationID: in.PreparationID, AgentID: agentID, ServerID: serverID, AgentCredentialGeneration: identity.AgentCredentialGeneration, Nonce: strings.Repeat("a", 64)}
			exec := func(sql string, args ...any) {
				t.Helper()
				if _, e := pool.Exec(ctx, sql, args...); e != nil {
					t.Fatal(e)
				}
			}
			switch mode {
			case "wrong_token":
				token = "retired-token"
			case "wrong_generation":
				request.AgentCredentialGeneration++
			case "wrong_agent":
				request.AgentID = u.ID
			case "wrong_server":
				request.ServerID = u.ID
			case "wrong_operation":
				request.OperationID = u.ID
			case "false_capability", "omitted_capability", "nil_capability", "identity_heartbeat":
				hb := agents.UpdateAgentHeartbeatInput{TokenHash: token, Capabilities: agents.Capabilities{"runtimeMutationFencingV1": true, "runtimeRecoveryObservationV1": true}}
				if mode == "false_capability" {
					hb.Capabilities["runtimeRecoveryGrantReadV1"] = false
				}
				if mode == "nil_capability" {
					hb.Capabilities = nil
				}
				if mode == "identity_heartbeat" {
					hb.TokenHash = ""
					hb.AgentID = agentID
					hb.Capabilities = caps
				}
				if _, err = repo.UpdateAgentHeartbeat(ctx, hb); err != nil {
					t.Fatal(err)
				}
				var supported, sticky bool
				if err = pool.QueryRow(ctx, `SELECT capabilities @> '{"runtimeRecoveryGrantReadV1":true}'::jsonb,capabilities @> '{"runtimeMutationFencingV1":true}'::jsonb FROM agents WHERE id=$1::uuid`, agentID).Scan(&supported, &sticky); err != nil || supported || !sticky {
					t.Fatal("current capability retained/sticky safety lost", err)
				}
			case "stale_agent":
				exec(`UPDATE agents SET last_authenticated_heartbeat_at=clock_timestamp()-interval '10 minutes' WHERE id=$1::uuid`, agentID)
			case "revoked_issuer":
				exec(`UPDATE auth_sessions SET revoked_at=clock_timestamp() WHERE id=$1::uuid`, u.SessionID)
			case "expired_session":
				exec(`UPDATE auth_sessions SET expires_at=clock_timestamp()-interval '1 second' WHERE id=$1::uuid`, u.SessionID)
			case "removed_role":
				exec(`DELETE FROM user_roles WHERE user_id=$1::uuid`, u.ID)
			case "removed_permission":
				exec(`DELETE FROM role_permissions`)
			case "disabled_issuer":
				exec(`UPDATE users SET status='disabled' WHERE id=$1::uuid`, u.ID)
			case "expired_grant":
				exec(`SELECT pg_sleep(1.1)`)
			}
			snapshot := func() string {
				t.Helper()
				var s string
				if e := pool.QueryRow(ctx, `SELECT jsonb_build_array((SELECT jsonb_agg(to_jsonb(a)) FROM agents a),(SELECT jsonb_agg(to_jsonb(p)) FROM runtime_recovery_preparations p),(SELECT jsonb_agg(to_jsonb(g)) FROM runtime_recovery_mutation_grants g),(SELECT jsonb_agg(to_jsonb(a)) FROM vpn_accounts a),(SELECT jsonb_agg(to_jsonb(c)) FROM config_versions c),(SELECT jsonb_agg(to_jsonb(j)) FROM config_apply_jobs j),(SELECT jsonb_agg(to_jsonb(j)) FROM agent_operation_jobs j),(SELECT jsonb_agg(to_jsonb(t)) FROM vpn_subscription_tokens t))::text`).Scan(&s); e != nil {
					t.Fatal(e)
				}
				return s
			}
			before := snapshot()
			data, _ := json.Marshal(request)
			if mode == "valid" {
				var wg sync.WaitGroup
				for range 2 {
					wg.Go(func() {
						out, e := svc.ReadRecoveryGrant(ctx, token, in.PreparationID, data)
						if e != nil || out.Grant != g.Grant || out.GrantHash != g.GrantHash || out.RequestHash != recoveryTestHash(data) || out.AgentCredentialGeneration != generation {
							t.Error("concurrent read lost binding", e)
						}
					})
				}
				wg.Wait()
				request.Nonce = strings.Repeat("b", 64)
				retry, _ := json.Marshal(request)
				out, e := svc.ReadRecoveryGrant(ctx, token, in.PreparationID, retry)
				if e != nil || out.Grant != g.Grant || out.RequestHash != recoveryTestHash(retry) {
					t.Fatal("retry renewed or lost request hash", e)
				}
			} else if _, err = svc.ReadRecoveryGrant(ctx, token, in.PreparationID, data); err == nil {
				t.Fatal("unsafe grant read accepted")
			}
			if snapshot() != before {
				t.Fatal("read mutated reservation/grant/Agent/account/token/config/job")
			}
			if mode == "wrong_token" {
				if _, err = repo.ReadCredentialIdentity(ctx, token, agents.CredentialIdentityInput{AgentID: agentID, ServerID: serverID}); err == nil {
					t.Fatal("retired identity token accepted")
				}
			}
		})
	}
}

func TestRuntimeMutationFencingRecoveryGrantReadWaitExpiry(t *testing.T) {
	ctx, pool, in, _, done := recoveryGrantFixture(t, time.Second, true)
	defer done()
	svc := revocations.NewService(pool)
	g, err := svc.IssueRecoveryMutationGrant(ctx, in)
	if err != nil {
		t.Fatal(err)
	}
	var token string
	if err = pool.QueryRow(ctx, `SELECT token_hash FROM agents WHERE id=$1::uuid`, g.Grant.Expected.AgentID).Scan(&token); err != nil {
		t.Fatal(err)
	}
	if _, err = agents.NewRepository(pool).UpdateAgentHeartbeat(ctx, agents.UpdateAgentHeartbeatInput{TokenHash: token, Capabilities: agents.Capabilities{"runtimeMutationFencingV1": true, "runtimeRecoveryObservationV1": true, "runtimeRecoveryGrantReadV1": true}}); err != nil {
		t.Fatal(err)
	}
	data, _ := json.Marshal(revocations.RecoveryGrantReadInput{SchemaVersion: 1, OperationID: in.PreparationID, AgentID: g.Grant.Expected.AgentID, ServerID: g.Grant.Expected.ServerID, AgentCredentialGeneration: g.Grant.AgentCredentialGeneration, Nonce: strings.Repeat("a", 64)})
	tx, err := pool.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer tx.Rollback(ctx)
	if _, err = tx.Exec(ctx, `SELECT id FROM servers WHERE id=$1::uuid FOR UPDATE`, g.Grant.Expected.ServerID); err != nil {
		t.Fatal(err)
	}
	ch := make(chan error, 1)
	go func() { _, e := svc.ReadRecoveryGrant(ctx, token, in.PreparationID, data); ch <- e }()
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
			t.Fatal("read did not wait for node")
		}
		if _, err = pool.Exec(ctx, `SELECT pg_sleep(0.01)`); err != nil {
			t.Fatal(err)
		}
	}
	// Read-only delivery must not hold global authority locks while waiting.
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
		t.Fatal("expired grant delivered after node wait")
	}
	var saved, state string
	if err = pool.QueryRow(ctx, `SELECT g.grant_payload,p.state FROM runtime_recovery_mutation_grants g JOIN runtime_recovery_preparations p ON p.id=g.preparation_id WHERE p.id=$1::uuid`, in.PreparationID).Scan(&saved, &state); err != nil {
		t.Fatal(err)
	}
	wire, _ := json.Marshal(g.Grant)
	if saved != string(wire) || state != "prepared" {
		t.Fatal("failed delivery altered authority")
	}
}
