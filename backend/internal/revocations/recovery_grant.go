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

const recoveryGrantPurpose = "runtime_mutation_fence_release_v1"

// RecoveryMutationGrant is a future single-use authorization contract. Issuance
// alone cannot clear a fence: there is no delivery, consumer or Agent unlock
// endpoint. A future consumer MUST re-observe under a held Agent mutation lock
// and durably consume this nonce before any crash-safe release protocol.
type RecoveryMutationGrant struct {
	SchemaVersion             int                 `json:"schemaVersion"`
	Purpose                   string              `json:"purpose"`
	OperationID               string              `json:"operationId"`
	Nonce                     string              `json:"nonce"`
	AgentCredentialGeneration int64               `json:"agentCredentialGeneration"`
	ChallengeHash             string              `json:"challengeHash"`
	EvidenceHash              string              `json:"evidenceHash"`
	IssuedAt                  time.Time           `json:"issuedAt"`
	ExpiresAt                 time.Time           `json:"expiresAt"`
	Expected                  RecoveryObservation `json:"expected"`
}
type RecoveryMutationGrantInput struct {
	PreparationID        string
	ChallengeHash        string
	EvidenceHash         string
	ConfirmFenceRecovery bool
}
type RecoveryMutationGrantApproval struct {
	Grant     RecoveryMutationGrant `json:"grant"`
	GrantHash string                `json:"grantHash"`
}

func decodeRecoveryMutationGrant(data []byte) (RecoveryMutationGrant, error) {
	var g RecoveryMutationGrant
	if len(data) > 8192 {
		return g, ErrPreflight
	}
	_, canonical, _, err := canonicalRemovalJSON(data)
	if err != nil {
		return g, ErrPreflight
	}
	d := json.NewDecoder(bytes.NewReader(data))
	d.DisallowUnknownFields()
	if d.Decode(&g) != nil {
		return RecoveryMutationGrant{}, ErrPreflight
	}
	wire, err := json.Marshal(g)
	if err != nil {
		return RecoveryMutationGrant{}, ErrPreflight
	}
	_, typed, _, err := canonicalRemovalJSON(wire)
	if err != nil || !bytes.Equal(canonical, typed) || g.SchemaVersion != 1 || g.Purpose != recoveryGrantPurpose || !evidenceID.MatchString(g.OperationID) || !validRemovalHash(g.Nonce) || !validRemovalHash(g.ChallengeHash) || !validRemovalHash(g.EvidenceHash) || g.AgentCredentialGeneration < 1 || g.IssuedAt.IsZero() || !g.ExpiresAt.After(g.IssuedAt) || g.ExpiresAt.Sub(g.IssuedAt) > 2*time.Minute {
		return RecoveryMutationGrant{}, ErrPreflight
	}
	expected, _ := json.Marshal(g.Expected)
	if _, err = decodeRecoveryObservation(expected); err != nil {
		return RecoveryMutationGrant{}, ErrPreflight
	}
	return g, nil
}

// IssueRecoveryMutationGrant requires fresh explicit human authority independent
// of the read-only approval. One immutable issuance is allowed per preparation;
// retries never renew nonce or deadline. No HTTP/queue/Agent dispatch exists.
func (s *Service) IssueRecoveryMutationGrant(ctx context.Context, in RecoveryMutationGrantInput) (RecoveryMutationGrantApproval, error) {
	result, err := s.issueRecoveryMutationGrant(ctx, in)
	if err != nil {
		return RecoveryMutationGrantApproval{}, ErrPreflight
	}
	return result, nil
}

func (s *Service) issueRecoveryMutationGrant(ctx context.Context, in RecoveryMutationGrantInput) (RecoveryMutationGrantApproval, error) {
	var zero RecoveryMutationGrantApproval
	u, ok := auth.UserFromContext(ctx)
	if !ok || !validRecoveryActor(u) || !in.ConfirmFenceRecovery || !evidenceID.MatchString(in.PreparationID) || !validRemovalHash(in.ChallengeHash) || !validRemovalHash(in.EvidenceHash) {
		return zero, ErrPreflight
	}
	// The hash is read internally; the administrator supplies no Agent bearer.
	// lockRecoveryTransport rechecks it after node/preparation/Agent admission.
	var tokenHash string
	if err := s.pool.QueryRow(ctx, `SELECT a.token_hash FROM agents a JOIN runtime_recovery_preparations p ON p.agent_id=a.id AND p.server_id=a.server_id WHERE p.id=$1::uuid`, in.PreparationID).Scan(&tokenHash); err != nil {
		return zero, err
	}
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return zero, err
	}
	defer tx.Rollback(ctx)
	c, challengeHash, _, err := lockRecoveryTransport(ctx, tx, tokenHash, in.PreparationID)
	if err != nil || challengeHash != in.ChallengeHash {
		return zero, ErrPreflight
	}
	if err = lockRecoveryAuthority(ctx, tx, u); err != nil {
		return zero, err
	}
	var response, evidenceHash, actor string
	var generation int64
	err = tx.QueryRow(ctx, `SELECT e.response,e.response_hash,o.actor_user_id::text,p.agent_credential_generation
 FROM runtime_recovery_observation_evidence e JOIN runtime_recovery_observation_approvals o ON o.preparation_id=e.preparation_id
 JOIN runtime_recovery_preparations p ON p.id=e.preparation_id WHERE e.preparation_id=$1::uuid`, in.PreparationID).Scan(&response, &evidenceHash, &actor, &generation)
	if err != nil || evidenceHash != in.EvidenceHash || actor != u.ID {
		return zero, ErrPreflight
	}
	measured, err := DecodeRecoveryChallengeResponse([]byte(response))
	sum := sha256.Sum256([]byte(response))
	if err != nil || evidenceHash != hex.EncodeToString(sum[:]) || measured.OperationID != in.PreparationID || measured.ChallengeHash != challengeHash || measured.Observation != c.Expected || measured.ObservedAt.Before(c.IssuedAt) || !measured.ObservedAt.Before(c.ExpiresAt) {
		return zero, ErrPreflight
	}
	var now time.Time
	var stillValid bool
	if err = tx.QueryRow(ctx, `SELECT clock_timestamp(), EXISTS(SELECT 1 FROM auth_sessions WHERE id=$1::uuid AND revoked_at IS NULL AND expires_at>clock_timestamp())
 AND EXISTS(SELECT 1 FROM agents WHERE id=$2::uuid AND last_authenticated_heartbeat_at>clock_timestamp()-interval '90 seconds')`, u.SessionID, c.Expected.AgentID).Scan(&now, &stillValid); err != nil {
		return zero, err
	}
	if !stillValid || now.Before(c.IssuedAt) || !now.Before(c.ExpiresAt) || measured.ObservedAt.After(now) {
		return zero, ErrPreflight
	}
	var saved, savedHash, savedActor string
	err = tx.QueryRow(ctx, `SELECT grant_payload,grant_hash,actor_user_id::text FROM runtime_recovery_mutation_grants WHERE preparation_id=$1::uuid`, in.PreparationID).Scan(&saved, &savedHash, &savedActor)
	if err == nil {
		g, decodeErr := decodeRecoveryMutationGrant([]byte(saved))
		if decodeErr != nil || savedActor != u.ID || g.OperationID != in.PreparationID || g.ChallengeHash != challengeHash || g.EvidenceHash != evidenceHash || g.Expected != measured.Observation || g.AgentCredentialGeneration != generation || !now.Before(g.ExpiresAt) {
			return zero, ErrPreflight
		}
		digest := sha256.Sum256([]byte(saved))
		if savedHash != hex.EncodeToString(digest[:]) {
			return zero, ErrPreflight
		}
		if err = tx.Commit(ctx); err != nil {
			return zero, err
		}
		return RecoveryMutationGrantApproval{g, savedHash}, nil
	}
	if err != pgx.ErrNoRows {
		return zero, err
	}
	nonce := make([]byte, 32)
	if _, err = rand.Read(nonce); err != nil {
		return zero, err
	}
	// Keep the original observation deadline. Grant issuance cannot extend it.
	g := RecoveryMutationGrant{1, recoveryGrantPurpose, in.PreparationID, hex.EncodeToString(nonce), generation, challengeHash, evidenceHash, now.UTC(), c.ExpiresAt, measured.Observation}
	data, err := json.Marshal(g)
	if err != nil {
		return zero, err
	}
	digest := sha256.Sum256(data)
	hash := hex.EncodeToString(digest[:])
	_, err = tx.Exec(ctx, `INSERT INTO runtime_recovery_mutation_grants(preparation_id,actor_user_id,actor_session_id,grant_payload,grant_hash,issued_at,expires_at)
 VALUES($1::uuid,$2::uuid,$3::uuid,$4,$5,$6,$7)`, in.PreparationID, u.ID, u.SessionID, string(data), hash, g.IssuedAt, g.ExpiresAt)
	if err != nil {
		return zero, err
	}
	if err = tx.Commit(ctx); err != nil {
		return zero, err
	}
	return RecoveryMutationGrantApproval{g, hash}, nil
}
