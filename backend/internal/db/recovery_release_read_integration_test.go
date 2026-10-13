package db

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/ikaevus/routegate/backend/internal/revocations"
)

func TestRecoveryReleaseDelivery(t *testing.T) {
	for _, mode := range []string{"valid", "expired_original", "wrong_token", "wrong_path", "wrong_generation", "wrong_agent", "wrong_server", "wrong_grant", "wrong_consumption", "wrong_proof", "missing_authorization", "stale", "offline", "fencing_lost", "observation_lost", "grant_read_lost", "revoked", "disabled", "service_actor", "role_lost", "permission_lost", "session_shortened", "expired", "cancelled_context"} {
		t.Run(mode, func(t *testing.T) {
			lifetime := time.Duration(0)
			if mode == "expired_original" {
				lifetime = time.Second
			}
			f, in := releaseFixture(t, lifetime, true)
			if mode == "expired_original" {
				f.exec(t, `SELECT pg_sleep(1.1)`)
			}
			if mode == "expired" {
				f.exec(t, `UPDATE auth_sessions SET expires_at=clock_timestamp()+interval '1 second' WHERE id=$1::uuid`, f.actor.SessionID)
			}
			var approved revocations.RecoveryReleaseApproval
			var err error
			if mode != "missing_authorization" {
				approved, err = f.svc.IssueRecoveryReleaseAuthorization(f.ctx, in)
				if err != nil {
					t.Fatal(err)
				}
			}
			r := revocations.RecoveryReleaseReadInput{RecoveryGrantReadInput: revocations.RecoveryGrantReadInput{SchemaVersion: 1, OperationID: in.PreparationID, AgentID: f.proof.Observation.AgentID, ServerID: f.proof.Observation.ServerID, AgentCredentialGeneration: f.grant.Grant.AgentCredentialGeneration, Nonce: strings.Repeat("a", 64)}, GrantHash: in.GrantHash, ConsumptionHash: in.ConsumptionHash, CommitProofHash: in.CommitProofHash}
			token, path, ctx := f.token, in.PreparationID, f.ctx
			switch mode {
			case "wrong_token":
				token = "retired"
			case "wrong_path":
				path = f.actor.ID
			case "wrong_generation":
				r.AgentCredentialGeneration++
			case "wrong_agent":
				r.AgentID = f.actor.ID
			case "wrong_server":
				r.ServerID = f.actor.ID
			case "wrong_grant":
				r.GrantHash = strings.Repeat("e", 64)
			case "wrong_consumption":
				r.ConsumptionHash = strings.Repeat("e", 64)
			case "wrong_proof":
				r.CommitProofHash = strings.Repeat("e", 64)
			case "stale":
				f.exec(t, `UPDATE agents SET last_authenticated_heartbeat_at=clock_timestamp()-interval '10 minutes'`)
			case "offline":
				f.exec(t, `UPDATE agents SET status='offline'`)
			case "fencing_lost":
				f.exec(t, `UPDATE agents SET current_runtime_mutation_fencing=false`)
			case "observation_lost":
				f.exec(t, `UPDATE agents SET capabilities=capabilities-'runtimeRecoveryObservationV1'`)
			case "grant_read_lost":
				f.exec(t, `UPDATE agents SET capabilities=capabilities-'runtimeRecoveryGrantReadV1'`)
			case "revoked":
				f.exec(t, `UPDATE auth_sessions SET revoked_at=clock_timestamp() WHERE id=$1::uuid`, f.actor.SessionID)
			case "disabled":
				f.exec(t, `UPDATE users SET status='disabled' WHERE id=$1::uuid`, f.actor.ID)
			case "service_actor":
				f.exec(t, `UPDATE users SET user_type='agent' WHERE id=$1::uuid`, f.actor.ID)
			case "role_lost":
				f.exec(t, `DELETE FROM user_roles WHERE user_id=$1::uuid`, f.actor.ID)
			case "permission_lost":
				f.exec(t, `DELETE FROM role_permissions`)
			case "session_shortened":
				f.exec(t, `UPDATE auth_sessions SET expires_at=clock_timestamp()+interval '30 seconds' WHERE id=$1::uuid`, f.actor.SessionID)
			case "expired":
				f.exec(t, `SELECT pg_sleep(1.1)`)
			case "cancelled_context":
				c, cancel := context.WithCancel(ctx)
				cancel()
				ctx = c
			}
			before := f.snapshot(t)
			wire, _ := json.Marshal(r)
			for range 2 {
				out, e := f.svc.ReadRecoveryReleaseAuthorization(ctx, token, path, wire)
				if mode == "valid" || mode == "expired_original" {
					if e != nil || out.Authorization != approved.Authorization || out.AuthorizationHash != approved.AuthorizationHash || out.RequestHash != recoveryTestHash(wire) {
						t.Fatal("delivery/retry changed authority", e)
					}
				} else if e == nil || out != (revocations.RecoveryReleaseRead{}) {
					t.Fatal("unsafe delivery", mode, e)
				}
				if mode == "cancelled_context" && errors.Is(e, revocations.ErrPreflight) {
					t.Fatal("uncertainty became definitive rejection")
				}
			}
			if mode == "valid" {
				type result struct {
					out revocations.RecoveryReleaseRead
					err error
				}
				ch := make(chan result, 2)
				for range 2 {
					go func() { out, e := f.svc.ReadRecoveryReleaseAuthorization(ctx, token, path, wire); ch <- result{out, e} }()
				}
				for range 2 {
					r := <-ch
					if r.err != nil || r.out.Authorization != approved.Authorization || r.out.AuthorizationHash != approved.AuthorizationHash {
						t.Fatal("concurrent read changed authority", r.err)
					}
				}
			}
			if f.snapshot(t) != before {
				t.Fatal("read mutated protected state")
			}
			if mode != "missing_authorization" {
				var saved string
				if e := f.pool.QueryRow(f.ctx, `SELECT authorization_payload FROM runtime_recovery_release_authorizations`).Scan(&saved); e != nil {
					t.Fatal(e)
				}
				want, _ := json.Marshal(approved.Authorization)
				if saved != string(want) {
					t.Fatal("read renewed authorization")
				}
			}
		})
	}
}

func TestRecoveryReleaseDeliveryWaitExpiry(t *testing.T) {
	f, in := releaseFixture(t, 0, true)
	f.exec(t, `UPDATE auth_sessions SET expires_at=clock_timestamp()+interval '1 second' WHERE id=$1::uuid`, f.actor.SessionID)
	if _, err := f.svc.IssueRecoveryReleaseAuthorization(f.ctx, in); err != nil {
		t.Fatal(err)
	}
	r := revocations.RecoveryReleaseReadInput{RecoveryGrantReadInput: revocations.RecoveryGrantReadInput{SchemaVersion: 1, OperationID: in.PreparationID, AgentID: f.proof.Observation.AgentID, ServerID: f.proof.Observation.ServerID, AgentCredentialGeneration: f.grant.Grant.AgentCredentialGeneration, Nonce: strings.Repeat("a", 64)}, GrantHash: in.GrantHash, ConsumptionHash: in.ConsumptionHash, CommitProofHash: in.CommitProofHash}
	wire, _ := json.Marshal(r)
	tx, err := f.pool.Begin(f.ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer tx.Rollback(f.ctx)
	if _, err = tx.Exec(f.ctx, `SELECT id FROM servers WHERE id=$1::uuid FOR UPDATE`, r.ServerID); err != nil {
		t.Fatal(err)
	}
	ch := make(chan error, 1)
	go func() {
		_, e := f.svc.ReadRecoveryReleaseAuthorization(f.ctx, f.token, in.PreparationID, wire)
		ch <- e
	}()
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
			t.Fatal("reader did not wait")
		}
		f.exec(t, `SELECT pg_sleep(0.01)`)
	}
	short, cancel := context.WithTimeout(f.ctx, 500*time.Millisecond)
	defer cancel()
	if _, err = f.pool.Exec(short, `UPDATE roles SET name=name WHERE code='admin'`); err != nil {
		t.Fatal("node wait locked authority globally", err)
	}
	f.exec(t, `SELECT pg_sleep(1.1)`)
	if err = tx.Commit(f.ctx); err != nil {
		t.Fatal(err)
	}
	if err = <-ch; err == nil {
		t.Fatal("expired authority delivered after node wait")
	}
}
