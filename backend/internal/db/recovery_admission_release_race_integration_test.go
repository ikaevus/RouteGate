package db

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/ikaevus/routegate/backend/internal/revocations"
	"github.com/jackc/pgx/v5/pgxpool"
)

// A statement waiting on the original server admission lock must not be
// accepted using an authority or Agent snapshot that became invalid while
// blocked. Keep all original deadlines, row locks and guards in force.
func TestRecoveryAdmissionReleaseLockInvalidation(t *testing.T) {
	for _, mode := range []string{"human_session_revoked", "agent_offline"} {
		t.Run(mode, func(t *testing.T) {
			f := newRecoveryAdmissionReleaseFixture(t)
			wire, err := json.Marshal(f.release)
			if err != nil {
				t.Fatal(err)
			}
			ctx, cancel := context.WithTimeout(f.ctx, 12*time.Second)
			defer cancel()
			blocker, err := f.pool.Begin(ctx)
			if err != nil {
				t.Fatal(err)
			}
			defer blocker.Rollback(ctx)
			if _, err = blocker.Exec(ctx, `SELECT id FROM servers WHERE id=$1::uuid FOR UPDATE`, f.release.Observation.ServerID); err != nil {
				t.Fatal(err)
			}

			cfg, err := pgxpool.ParseConfig(os.Getenv("ROUTEGATE_TEST_DATABASE_URL"))
			if err != nil {
				t.Fatal(err)
			}
			cfg.ConnConfig.RuntimeParams["application_name"] = "rg140-release-accept-wait"
			contender, err := pgxpool.NewWithConfig(ctx, cfg)
			if err != nil {
				t.Fatal(err)
			}
			defer contender.Close()
			result := make(chan error, 1)
			go func() {
				_, e := revocations.NewService(contender).AcceptRecoveryAdmissionRelease(ctx, f.token, f.release.OperationID, wire)
				result <- e
			}()

			waiting := false
			deadline := time.Now().Add(5 * time.Second)
			for time.Now().Before(deadline) {
				var active bool
				err = f.pool.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM pg_stat_activity
					WHERE application_name='rg140-release-accept-wait'
					AND wait_event_type='Lock')`).Scan(&active)
				if err != nil {
					t.Fatal(err)
				}
				if active {
					waiting = true
					break
				}
				select {
				case e := <-result:
					t.Fatalf("statement did not wait for original lock: %v", e)
				case <-time.After(10 * time.Millisecond):
				}
			}
			if !waiting {
				t.Fatal("admission release statement did not wait on server lock")
			}
			if mode == "human_session_revoked" {
				_, err = blocker.Exec(ctx, `UPDATE auth_sessions SET revoked_at=clock_timestamp() WHERE id=$1::uuid`, f.actor.SessionID)
			} else {
				_, err = blocker.Exec(ctx, `UPDATE agents SET status='offline' WHERE id=$1::uuid`, f.release.Observation.AgentID)
			}
			if err != nil {
				t.Fatal(err)
			}
			if err = blocker.Commit(ctx); err != nil {
				t.Fatal(err)
			}
			select {
			case e := <-result:
				if e == nil {
					t.Fatal("invalidated Agent/human authority admitted after lock wait")
				}
			case <-ctx.Done():
				t.Fatal("locked admission did not finish", ctx.Err())
			}
			var count int
			if err = f.pool.QueryRow(ctx, `SELECT count(*) FROM runtime_recovery_admission_release_evidence`).Scan(&count); err != nil || count != 0 {
				t.Fatal("rejected evidence persisted", count, err)
			}
		})
	}
}

// Two individually well-formed but conflicting proof statements must never
// create two Manager histories or silently overwrite the first accepted wire.
func TestRecoveryAdmissionReleaseLockConflictingProof(t *testing.T) {
	f := newRecoveryAdmissionReleaseFixture(t)
	p := f.release
	foreign := p
	foreign.CleanedArchiveHash = strings.Repeat("9", 64)
	a, _ := json.Marshal(p)
	b, _ := json.Marshal(foreign)
	type outcome struct {
		wire []byte
		err  error
	}
	start := make(chan struct{})
	answers := make(chan outcome, 2)
	var wg sync.WaitGroup
	for _, wire := range [][]byte{a, b} {
		wg.Add(1)
		go func(candidate []byte) {
			defer wg.Done()
			<-start
			_, err := f.svc.AcceptRecoveryAdmissionRelease(f.ctx, f.token, p.OperationID, candidate)
			answers <- outcome{candidate, err}
		}(wire)
	}
	close(start)
	wg.Wait()
	close(answers)
	var success, refused int
	var winner string
	for answer := range answers {
		if answer.err == nil {
			success++
			winner = string(answer.wire)
		} else {
			refused++
			if !errors.Is(answer.err, revocations.ErrPreflight) {
				// Database constraint errors are also fail-closed, but this
				// is a Service preflight path and should retain its contract.
				t.Fatal("conflicting immutable proof returned unexpected error", answer.err)
			}
		}
	}
	if success != 1 || refused != 1 {
		t.Fatalf("conflicting proofs accepted/failed: %d/%d", success, refused)
	}
	var saved, state string
	var count int
	if err := f.pool.QueryRow(f.ctx, `SELECT e.proof,p.state FROM runtime_recovery_admission_release_evidence e
		JOIN runtime_recovery_preparations p ON p.id=e.preparation_id`).Scan(&saved, &state); err != nil || saved != winner || state != "prepared" {
		t.Fatal("first-writer proof or reserved state changed", err)
	}
	if err := f.pool.QueryRow(f.ctx, `SELECT count(*) FROM runtime_recovery_admission_release_evidence`).Scan(&count); err != nil || count != 1 {
		t.Fatal("more than one immutable release evidence row", count, err)
	}
}
