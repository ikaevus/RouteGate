package revocations

import (
	"bytes"
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"time"

	"github.com/ikaevus/routegate/backend/internal/auth"
	"github.com/jackc/pgx/v5"
)

const recoveryReleasePurpose = "runtime_mutation_fence_final_release_v1"

// Conditional final authority, distinct from consumption grant and historical
// storage receipt. A future Agent consumer MUST independently authenticate the
// generation and freshly observe exact durable state/runtime under its held
// admission lock, durably consume this nonce and use a crash-safe release
// protocol. None of those consumers, delivery routes or unlocks exist here.
type RecoveryReleaseAuthorization struct {
	SchemaVersion             int                 `json:"schemaVersion"`
	Purpose                   string              `json:"purpose"`
	OperationID               string              `json:"operationId"`
	Nonce                     string              `json:"nonce"`
	AgentCredentialGeneration int64               `json:"agentCredentialGeneration"`
	GrantHash                 string              `json:"grantHash"`
	ConsumptionHash           string              `json:"consumptionHash"`
	CommitProofHash           string              `json:"commitProofHash"`
	IssuedAt                  time.Time           `json:"issuedAt"`
	ExpiresAt                 time.Time           `json:"expiresAt"`
	Expected                  RecoveryObservation `json:"expected"`
}

type RecoveryReleaseAuthorizationInput struct {
	PreparationID, GrantHash, ConsumptionHash, CommitProofHash string
	ConfirmFinalFenceRelease                                   bool
}
type RecoveryReleaseApproval struct {
	Authorization     RecoveryReleaseAuthorization `json:"authorization"`
	AuthorizationHash string                       `json:"authorizationHash"`
}

func decodeRecoveryReleaseAuthorization(data []byte) (RecoveryReleaseAuthorization, error) {
	var a RecoveryReleaseAuthorization
	if len(data) > 8192 {
		return a, ErrPreflight
	}
	_, canonical, _, err := canonicalRemovalJSON(data)
	if err != nil {
		return a, ErrPreflight
	}
	d := json.NewDecoder(bytes.NewReader(data))
	d.DisallowUnknownFields()
	if d.Decode(&a) != nil {
		return RecoveryReleaseAuthorization{}, ErrPreflight
	}
	wire, err := json.Marshal(a)
	if err != nil {
		return RecoveryReleaseAuthorization{}, ErrPreflight
	}
	_, typed, _, err := canonicalRemovalJSON(wire)
	if err != nil || !bytes.Equal(canonical, typed) || a.SchemaVersion != 1 || a.Purpose != recoveryReleasePurpose || !evidenceID.MatchString(a.OperationID) || !validRemovalHash(a.Nonce) || a.AgentCredentialGeneration < 1 || !validRemovalHash(a.GrantHash) || !validRemovalHash(a.ConsumptionHash) || !validRemovalHash(a.CommitProofHash) || a.IssuedAt.IsZero() || !a.ExpiresAt.After(a.IssuedAt) || a.ExpiresAt.Sub(a.IssuedAt) > 2*time.Minute {
		return RecoveryReleaseAuthorization{}, ErrPreflight
	}
	expected, _ := json.Marshal(a.Expected)
	if _, err = decodeRecoveryObservation(expected); err != nil {
		return RecoveryReleaseAuthorization{}, ErrPreflight
	}
	return a, nil
}

// Internal only. Fresh explicit human authority is required even when exact
// historical commit evidence exists. Issuance never resumes an Agent owner or
// changes a reservation, runtime, account, token, config or job.
func (s *Service) IssueRecoveryReleaseAuthorization(ctx context.Context, in RecoveryReleaseAuthorizationInput) (RecoveryReleaseApproval, error) {
	out, err := s.issueRecoveryReleaseAuthorization(ctx, in)
	if err != nil {
		return RecoveryReleaseApproval{}, ErrPreflight
	}
	return out, nil
}

func (s *Service) issueRecoveryReleaseAuthorization(ctx context.Context, in RecoveryReleaseAuthorizationInput) (RecoveryReleaseApproval, error) {
	var zero RecoveryReleaseApproval
	u, ok := auth.UserFromContext(ctx)
	if !ok || !validRecoveryActor(u) || !in.ConfirmFinalFenceRelease || !evidenceID.MatchString(in.PreparationID) || !validRemovalHash(in.GrantHash) || !validRemovalHash(in.ConsumptionHash) || !validRemovalHash(in.CommitProofHash) {
		return zero, ErrPreflight
	}
	var token string
	if err := s.pool.QueryRow(ctx, `SELECT a.token_hash FROM agents a JOIN runtime_recovery_preparations p ON p.agent_id=a.id AND p.server_id=a.server_id WHERE p.id=$1::uuid`, in.PreparationID).Scan(&token); err != nil {
		return zero, err
	}
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return zero, err
	}
	defer tx.Rollback(ctx)
	p, err := lockRecoveryPreparation(ctx, tx, token, in.PreparationID)
	if err != nil {
		return zero, err
	}
	var supported bool
	if err = tx.QueryRow(ctx, `SELECT capabilities @> '{"runtimeRecoveryGrantReadV1":true}'::jsonb FROM agents WHERE id=$1::uuid`, p.agent).Scan(&supported); err != nil {
		return zero, err
	}
	if !supported {
		return zero, ErrPreflight
	}
	if err = lockRecoveryAuthority(ctx, tx, u); err != nil {
		return zero, err
	}
	var proofData, proofHash, consumptionHash, grantHash, grantData string
	var accepted time.Time
	err = tx.QueryRow(ctx, `SELECT c.proof,c.proof_hash,c.consumption_hash,c.grant_hash,g.grant_payload,c.accepted_at
 FROM runtime_recovery_commit_evidence c JOIN runtime_recovery_mutation_grants g ON g.preparation_id=c.preparation_id AND g.grant_hash=c.grant_hash WHERE c.preparation_id=$1::uuid`, in.PreparationID).Scan(&proofData, &proofHash, &consumptionHash, &grantHash, &grantData, &accepted)
	if err != nil {
		return zero, err
	}
	proof, err := DecodeRecoveryCommitProof([]byte(proofData))
	grant, grantErr := decodeRecoveryMutationGrant([]byte(grantData))
	proofSum := sha256.Sum256([]byte(proofData))
	grantSum := sha256.Sum256([]byte(grantData))
	if err != nil || grantErr != nil || proofHash != in.CommitProofHash || consumptionHash != in.ConsumptionHash || grantHash != in.GrantHash || proofHash != hex.EncodeToString(proofSum[:]) || grantHash != hex.EncodeToString(grantSum[:]) || proof.OperationID != in.PreparationID || proof.GrantHash != grantHash || proof.ConsumptionHash != consumptionHash || grant.OperationID != in.PreparationID || grant.AgentCredentialGeneration != p.generation || !p.matches(grant.Expected) || proof.Observation != grant.Expected || proof.ObservedAt.Before(grant.IssuedAt) || !proof.ObservedAt.Before(grant.ExpiresAt) {
		return zero, ErrPreflight
	}
	var now, sessionExpiry time.Time
	var current bool
	err = tx.QueryRow(ctx, `SELECT clock_timestamp(),s.expires_at,s.revoked_at IS NULL AND s.expires_at>clock_timestamp()
 AND EXISTS(SELECT 1 FROM agents WHERE id=$2::uuid AND last_authenticated_heartbeat_at>clock_timestamp()-interval '90 seconds') FROM auth_sessions s WHERE s.id=$1::uuid`, u.SessionID, p.agent).Scan(&now, &sessionExpiry, &current)
	if err != nil {
		return zero, err
	}
	if !current || now.Before(accepted) || now.Before(proof.ObservedAt) {
		return zero, ErrPreflight
	}
	var saved, hash, actor, session string
	err = tx.QueryRow(ctx, `SELECT authorization,authorization_hash,actor_user_id::text,actor_session_id::text FROM runtime_recovery_release_authorizations WHERE preparation_id=$1::uuid`, in.PreparationID).Scan(&saved, &hash, &actor, &session)
	if err == nil {
		a, e := decodeRecoveryReleaseAuthorization([]byte(saved))
		sum := sha256.Sum256([]byte(saved))
		if e != nil || hash != hex.EncodeToString(sum[:]) || actor != u.ID || session != u.SessionID || a.OperationID != in.PreparationID || a.AgentCredentialGeneration != p.generation || a.GrantHash != grantHash || a.ConsumptionHash != consumptionHash || a.CommitProofHash != proofHash || a.Expected != proof.Observation || now.Before(a.IssuedAt) || !now.Before(a.ExpiresAt) || a.ExpiresAt.After(sessionExpiry) {
			return zero, ErrPreflight
		}
		if err = tx.Commit(ctx); err != nil {
			return zero, err
		}
		return RecoveryReleaseApproval{a, hash}, nil
	}
	if err != pgx.ErrNoRows {
		return zero, err
	}
	nonce := make([]byte, 32)
	if _, err = rand.Read(nonce); err != nil {
		return zero, err
	}
	expires := now.Add(2 * time.Minute)
	if sessionExpiry.Before(expires) {
		expires = sessionExpiry
	}
	a := RecoveryReleaseAuthorization{1, recoveryReleasePurpose, in.PreparationID, hex.EncodeToString(nonce), p.generation, grantHash, consumptionHash, proofHash, now.UTC(), expires.UTC(), proof.Observation}
	data, err := json.Marshal(a)
	if err != nil {
		return zero, err
	}
	sum := sha256.Sum256(data)
	hash = hex.EncodeToString(sum[:])
	_, err = tx.Exec(ctx, `INSERT INTO runtime_recovery_release_authorizations(preparation_id,actor_user_id,actor_session_id,authorization,authorization_hash,issued_at,expires_at) VALUES($1::uuid,$2::uuid,$3::uuid,$4::text,$5,($4::text::jsonb->>'issuedAt')::timestamptz,($4::text::jsonb->>'expiresAt')::timestamptz)`, in.PreparationID, u.ID, u.SessionID, string(data), hash)
	if err != nil {
		return zero, err
	}
	if err = tx.Commit(ctx); err != nil {
		return zero, err
	}
	return RecoveryReleaseApproval{a, hash}, nil
}
