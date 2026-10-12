package revocations

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"time"
)

type RecoveryGrantReadInput struct {
	SchemaVersion             int    `json:"schemaVersion"`
	OperationID               string `json:"operationId"`
	AgentID                   string `json:"agentId"`
	ServerID                  string `json:"serverId"`
	AgentCredentialGeneration int64  `json:"agentCredentialGeneration"`
	Nonce                     string `json:"nonce"`
}
type RecoveryGrantRead struct {
	SchemaVersion             int                   `json:"schemaVersion"`
	OperationID               string                `json:"operationId"`
	AgentID                   string                `json:"agentId"`
	ServerID                  string                `json:"serverId"`
	AgentCredentialGeneration int64                 `json:"agentCredentialGeneration"`
	RequestHash               string                `json:"requestHash"`
	GrantHash                 string                `json:"grantHash"`
	Grant                     RecoveryMutationGrant `json:"grant"`
}

func decodeRecoveryGrantRead(data []byte) (RecoveryGrantReadInput, error) {
	var in RecoveryGrantReadInput
	if len(data) > 4096 {
		return in, ErrPreflight
	}
	_, canonical, _, err := canonicalRemovalJSON(data)
	if err != nil {
		return in, ErrPreflight
	}
	d := json.NewDecoder(bytes.NewReader(data))
	d.DisallowUnknownFields()
	if d.Decode(&in) != nil {
		return in, ErrPreflight
	}
	wire, _ := json.Marshal(in)
	_, typed, _, err := canonicalRemovalJSON(wire)
	if err != nil || !bytes.Equal(canonical, typed) || in.SchemaVersion != 1 || !evidenceID.MatchString(in.OperationID) || !evidenceID.MatchString(in.AgentID) || !evidenceID.MatchString(in.ServerID) || in.AgentCredentialGeneration < 1 || !validRemovalHash(in.Nonce) {
		return RecoveryGrantReadInput{}, ErrPreflight
	}
	return in, nil
}

// Read-only delivery of existing authority, never consumption or fence release.
// Rechecks current Agent credentials and the grant issuer's live authority.
func (s *Service) ReadRecoveryGrant(ctx context.Context, tokenHash, operation string, data []byte) (RecoveryGrantRead, error) {
	var zero RecoveryGrantRead
	in, err := decodeRecoveryGrantRead(data)
	if err != nil || in.OperationID != operation {
		return zero, ErrPreflight
	}
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return zero, err
	}
	defer tx.Rollback(ctx)
	c, challengeHash, _, err := lockRecoveryTransport(ctx, tx, tokenHash, operation)
	if err != nil || c.Expected.AgentID != in.AgentID || c.Expected.ServerID != in.ServerID {
		return zero, ErrPreflight
	}
	var saved, hash, actor, session, evidenceHash string
	var generation int64
	err = tx.QueryRow(ctx, `SELECT g.grant_payload,g.grant_hash,g.actor_user_id::text,g.actor_session_id::text,p.agent_credential_generation,e.response_hash
 FROM runtime_recovery_mutation_grants g JOIN runtime_recovery_preparations p ON p.id=g.preparation_id
 JOIN runtime_recovery_observation_evidence e ON e.preparation_id=p.id WHERE g.preparation_id=$1::uuid`, operation).Scan(&saved, &hash, &actor, &session, &generation, &evidenceHash)
	if err != nil || generation != in.AgentCredentialGeneration {
		return zero, ErrPreflight
	}
	var supported bool
	if err = tx.QueryRow(ctx, `SELECT capabilities @> '{"runtimeRecoveryGrantReadV1":true}'::jsonb FROM agents WHERE id=$1::uuid AND credential_generation=$2`, in.AgentID, generation).Scan(&supported); err != nil || !supported {
		return zero, ErrPreflight
	}
	if err = lockRecoveryAuthoritySubject(ctx, tx, actor, session); err != nil {
		return zero, ErrPreflight
	}
	g, err := decodeRecoveryMutationGrant([]byte(saved))
	sum := sha256.Sum256([]byte(saved))
	if err != nil || hash != hex.EncodeToString(sum[:]) || g.OperationID != operation || g.AgentCredentialGeneration != generation || g.Expected != c.Expected || g.ChallengeHash != challengeHash || g.EvidenceHash != evidenceHash {
		return zero, ErrPreflight
	}
	var now time.Time
	var current bool
	if err = tx.QueryRow(ctx, `SELECT clock_timestamp(),EXISTS(SELECT 1 FROM auth_sessions WHERE id=$1::uuid AND revoked_at IS NULL AND expires_at>clock_timestamp()) AND EXISTS(SELECT 1 FROM agents WHERE id=$2::uuid AND last_authenticated_heartbeat_at>clock_timestamp()-interval '90 seconds')`, session, in.AgentID).Scan(&now, &current); err != nil || !current || now.Before(g.IssuedAt) || !now.Before(g.ExpiresAt) {
		return zero, ErrPreflight
	}
	if err = tx.Commit(ctx); err != nil {
		return zero, err
	}
	digest := sha256.Sum256(data)
	return RecoveryGrantRead{1, operation, in.AgentID, in.ServerID, generation, hex.EncodeToString(digest[:]), hash, g}, nil
}
