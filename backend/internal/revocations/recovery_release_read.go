package revocations

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"time"
)

type RecoveryReleaseReadInput struct {
	RecoveryGrantReadInput
	GrantHash       string `json:"grantHash"`
	ConsumptionHash string `json:"consumptionHash"`
	CommitProofHash string `json:"commitProofHash"`
}
type RecoveryReleaseRead struct {
	SchemaVersion             int                          `json:"schemaVersion"`
	OperationID               string                       `json:"operationId"`
	AgentID                   string                       `json:"agentId"`
	ServerID                  string                       `json:"serverId"`
	AgentCredentialGeneration int64                        `json:"agentCredentialGeneration"`
	RequestHash               string                       `json:"requestHash"`
	AuthorizationHash         string                       `json:"authorizationHash"`
	Authorization             RecoveryReleaseAuthorization `json:"authorization"`
}

func decodeRecoveryReleaseRead(data []byte) (RecoveryReleaseReadInput, error) {
	var in RecoveryReleaseReadInput
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
	base, _ := json.Marshal(in.RecoveryGrantReadInput)
	_, baseErr := decodeRecoveryGrantRead(base)
	if err != nil || baseErr != nil || !bytes.Equal(canonical, typed) || !validRemovalHash(in.GrantHash) || !validRemovalHash(in.ConsumptionHash) || !validRemovalHash(in.CommitProofHash) {
		return RecoveryReleaseReadInput{}, ErrPreflight
	}
	return in, nil
}

// Deliver only existing conditional authority. No original grant renewal,
// Agent nonce consumption, acknowledgement adoption or runtime action occurs.
func (s *Service) ReadRecoveryReleaseAuthorization(ctx context.Context, token, operation string, data []byte) (RecoveryReleaseRead, error) {
	var zero RecoveryReleaseRead
	in, err := decodeRecoveryReleaseRead(data)
	if err != nil || in.OperationID != operation {
		return zero, ErrPreflight
	}
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return zero, err
	}
	defer tx.Rollback(ctx)
	p, err := lockRecoveryPreparation(ctx, tx, token, operation)
	if err != nil {
		return zero, recoveryAdmissionError(err)
	}
	if p.agent != in.AgentID || p.node != in.ServerID || p.generation != in.AgentCredentialGeneration {
		return zero, ErrPreflight
	}
	var supported bool
	if err = tx.QueryRow(ctx, `SELECT capabilities @> '{"runtimeRecoveryGrantReadV1":true}'::jsonb FROM agents WHERE id=$1::uuid`, p.agent).Scan(&supported); err != nil {
		return zero, err
	}
	if !supported {
		return zero, ErrPreflight
	}
	var saved, hash, actor, session, proofData, proofHash, consumptionHash, grantHash string
	err = tx.QueryRow(ctx, `SELECT a.authorization_payload,a.authorization_hash,a.actor_user_id::text,a.actor_session_id::text,c.proof,c.proof_hash,c.consumption_hash,c.grant_hash FROM runtime_recovery_release_authorizations a JOIN runtime_recovery_commit_evidence c ON c.preparation_id=a.preparation_id WHERE a.preparation_id=$1::uuid`, operation).Scan(&saved, &hash, &actor, &session, &proofData, &proofHash, &consumptionHash, &grantHash)
	if err != nil {
		return zero, recoveryAdmissionError(err)
	}
	if err = lockRecoveryAuthoritySubject(ctx, tx, actor, session); err != nil {
		return zero, recoveryAdmissionError(err)
	}
	a, err := decodeRecoveryReleaseAuthorization([]byte(saved))
	sum := sha256.Sum256([]byte(saved))
	proofSum := sha256.Sum256([]byte(proofData))
	proof, proofErr := DecodeRecoveryCommitProof([]byte(proofData))
	if err != nil || proofErr != nil || hash != hex.EncodeToString(sum[:]) || proofHash != hex.EncodeToString(proofSum[:]) || a.OperationID != operation || a.AgentCredentialGeneration != p.generation || !p.matches(a.Expected) || a.GrantHash != grantHash || a.ConsumptionHash != consumptionHash || a.CommitProofHash != proofHash || grantHash != in.GrantHash || consumptionHash != in.ConsumptionHash || proofHash != in.CommitProofHash || proof.OperationID != operation || proof.GrantHash != grantHash || proof.ConsumptionHash != consumptionHash || proof.Observation != a.Expected || a.IssuedAt.Before(proof.ObservedAt) {
		return zero, ErrPreflight
	}
	var now, sessionExpiry time.Time
	var current bool
	err = tx.QueryRow(ctx, `SELECT clock_timestamp(),s.expires_at,s.revoked_at IS NULL AND s.expires_at>clock_timestamp() AND EXISTS(SELECT 1 FROM agents WHERE id=$2::uuid AND last_authenticated_heartbeat_at>clock_timestamp()-interval '90 seconds') FROM auth_sessions s WHERE s.id=$1::uuid`, session, p.agent).Scan(&now, &sessionExpiry, &current)
	if err != nil {
		return zero, recoveryAdmissionError(err)
	}
	if !current || now.Before(a.IssuedAt) || !now.Before(a.ExpiresAt) || a.ExpiresAt.After(sessionExpiry) {
		return zero, ErrPreflight
	}
	if err = tx.Commit(ctx); err != nil {
		return zero, err
	}
	requestSum := sha256.Sum256(data)
	return RecoveryReleaseRead{1, operation, p.agent, p.node, p.generation, hex.EncodeToString(requestSum[:]), hash, a}, nil
}
