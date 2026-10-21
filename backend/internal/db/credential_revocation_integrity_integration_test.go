package db

import (
	"errors"
	"strings"
	"testing"

	"github.com/ikaevus/routegate/backend/internal/revocations"
)

// Integrity is a read-only snapshot inspection, never a request to dispatch a
// pending credential-removal task or an assertion of VPN traffic termination.
func TestPreparedCredentialRemovalIntegrity(t *testing.T) {
	for _, status := range []string{"active", "suspended", "revoked"} {
		t.Run(status, func(t *testing.T) {
			ctx, pool, in, _, done := removalPreparationFixture(t)
			defer done()
			if _, err := pool.Exec(ctx, `UPDATE vpn_accounts SET status=$2 WHERE id=$1::uuid`, in.AccountID, status); err != nil {
				t.Fatal(err)
			}
			svc := revocations.NewService(pool)
			prepared, err := svc.Prepare(ctx, in)
			if err != nil {
				t.Fatal(err)
			}
			var beforeJobs, beforeVersions, beforeTokens int
			if err = pool.QueryRow(ctx, `SELECT
			 (SELECT count(*) FROM config_apply_jobs),
			 (SELECT count(*) FROM config_versions),
			 (SELECT count(*) FROM vpn_subscription_tokens)`).Scan(
				&beforeJobs, &beforeVersions, &beforeTokens); err != nil {
				t.Fatal(err)
			}
			checked, err := svc.InspectPreparedRemovalIntegrity(ctx, prepared.ID)
			if err != nil || checked.OperationID != prepared.ID ||
				checked.AccountID != in.AccountID ||
				checked.ServerID != in.ServerID ||
				checked.AgentID != prepared.AgentID ||
				checked.BaselineVersionID != in.BaselineVersionID ||
				checked.BaselineHash != in.BaselineHash ||
				checked.AgentGeneration < 1 ||
				checked.CandidateHash == checked.BaselineHash ||
				checked.CandidateRuntimeHash == checked.BaselineRuntimeHash {
				t.Fatal("prepared candidate failed immutable pin integrity", err)
			}
			var savedCandidateHash, savedRuntimeHash string
			if err = pool.QueryRow(ctx, `SELECT candidate_hash,candidate_runtime_hash
			 FROM vpn_credential_revocations WHERE id=$1::uuid`, prepared.ID).Scan(&savedCandidateHash, &savedRuntimeHash); err != nil ||
				checked.CandidateHash != savedCandidateHash ||
				checked.CandidateRuntimeHash != savedRuntimeHash {
				t.Fatal("inspection returned a different candidate", err)
			}
			if again, err := svc.InspectPreparedRemovalIntegrity(ctx, prepared.ID); err != nil || again != checked {
				t.Fatal("read-only inspection was not stable", err)
			}
			var afterJobs, afterVersions, afterTokens int
			if err = pool.QueryRow(ctx, `SELECT
			 (SELECT count(*) FROM config_apply_jobs),
			 (SELECT count(*) FROM config_versions),
			 (SELECT count(*) FROM vpn_subscription_tokens)`).Scan(
				&afterJobs, &afterVersions, &afterTokens); err != nil ||
				beforeJobs != afterJobs || beforeVersions != afterVersions || beforeTokens != afterTokens {
				t.Fatal("integrity inspection created task, version or bearer", err)
			}
			if _, err = pool.Exec(ctx, `UPDATE vpn_credential_revocations
			 SET candidate_hash=$2 WHERE id=$1::uuid`, prepared.ID, strings.Repeat("f", 64)); err == nil {
				t.Fatal("the immutable preparation ledger permitted candidate replacement")
			}
			for _, invalid := range []string{"bogus", "11111111-1111-4111-8111-111111111111"} {
				if p, e := svc.InspectPreparedRemovalIntegrity(ctx, invalid); !errors.Is(e, revocations.ErrPreflight) || p != (revocations.PreparedRemovalIntegrity{}) {
					t.Fatal("foreign operation exposed prepared candidate", e)
				}
			}
			if _, err = svc.Cancel(ctx, prepared.ID, "operator"); err != nil {
				t.Fatal(err)
			}
			if p, e := svc.InspectPreparedRemovalIntegrity(ctx, prepared.ID); !errors.Is(e, revocations.ErrPreflight) || p != (revocations.PreparedRemovalIntegrity{}) {
				t.Fatal("cancelled preparation exposed an active candidate", e)
			}
		})
	}
}

