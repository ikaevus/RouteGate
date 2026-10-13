package revocations

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"time"

	"github.com/jackc/pgx/v5"
)

// Mirrors Agent's measured final owner. Storage never proves continued flock
// ownership or grants release authority; no reservation/fence is released.
type RecoveryFinalizationProof struct {
	SchemaVersion        int                 `json:"schemaVersion"`
	OperationID          string              `json:"operationId"`
	AuthorizationHash    string              `json:"authorizationHash"`
	FinalConsumptionHash string              `json:"finalConsumptionHash"`
	ObservedAt           time.Time           `json:"observedAt"`
	Observation          RecoveryObservation `json:"observation"`
}
type RecoveryFinalizationReceipt struct {
	SchemaVersion        int    `json:"schemaVersion"`
	Recorded             bool   `json:"recorded"`
	OperationID          string `json:"operationId"`
	AuthorizationHash    string `json:"authorizationHash"`
	FinalConsumptionHash string `json:"finalConsumptionHash"`
	ProofHash            string `json:"proofHash"`
	RequestHash          string `json:"requestHash"`
}

func DecodeRecoveryFinalizationProof(data []byte) (RecoveryFinalizationProof, error) {
	var p RecoveryFinalizationProof
	if len(data) > 8192 {
		return p, ErrPreflight
	}
	_, canonical, _, err := canonicalRemovalJSON(data)
	if err != nil {
		return p, ErrPreflight
	}
	d := json.NewDecoder(bytes.NewReader(data))
	d.DisallowUnknownFields()
	if d.Decode(&p) != nil {
		return p, ErrPreflight
	}
	wire, err := json.Marshal(p)
	if err != nil {
		return p, err
	}
	_, typed, _, err := canonicalRemovalJSON(wire)
	if err != nil || !bytes.Equal(canonical, typed) || p.SchemaVersion != 1 || !evidenceID.MatchString(p.OperationID) || !validRemovalHash(p.AuthorizationHash) || !validRemovalHash(p.FinalConsumptionHash) || p.ObservedAt.IsZero() {
		return RecoveryFinalizationProof{}, ErrPreflight
	}
	o, _ := json.Marshal(p.Observation)
	if _, err = decodeRecoveryObservation(o); err != nil {
		return RecoveryFinalizationProof{}, ErrPreflight
	}
	return p, nil
}
func recoveryFinalizationReceipt(p RecoveryFinalizationProof, wire, data []byte) RecoveryFinalizationReceipt {
	sum := sha256.Sum256(wire)
	req := sha256.Sum256(data)
	return RecoveryFinalizationReceipt{1, true, p.OperationID, p.AuthorizationHash, p.FinalConsumptionHash, hex.EncodeToString(sum[:]), hex.EncodeToString(req[:])}
}
func (s *Service) AcceptRecoveryFinalization(ctx context.Context, token, operation string, data []byte) (RecoveryFinalizationReceipt, error) {
	var zero RecoveryFinalizationReceipt
	p, err := DecodeRecoveryFinalizationProof(data)
	if err != nil || p.OperationID != operation {
		return zero, ErrPreflight
	}
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return zero, err
	}
	defer tx.Rollback(ctx)
	a, hash, now, err := lockRecoveryReleaseDelivery(ctx, tx, token, operation)
	if err != nil {
		return zero, err
	}
	if p.AuthorizationHash != hash || p.Observation != a.Expected || p.ObservedAt.Before(a.IssuedAt) || !p.ObservedAt.Before(a.ExpiresAt) || p.ObservedAt.After(now) {
		return zero, ErrPreflight
	}
	wire, err := json.Marshal(p)
	if err != nil {
		return zero, err
	}
	receipt := recoveryFinalizationReceipt(p, wire, data)
	var saved string
	err = tx.QueryRow(ctx, `SELECT proof FROM runtime_recovery_finalization_evidence WHERE preparation_id=$1::uuid`, operation).Scan(&saved)
	if err == nil {
		if saved != string(wire) {
			return zero, ErrPreflight
		}
	} else if err == pgx.ErrNoRows {
		_, err = tx.Exec(ctx, `INSERT INTO runtime_recovery_finalization_evidence(preparation_id,authorization_hash,final_consumption_hash,proof,proof_hash,observed_at) VALUES($1::uuid,$2,$3,$4::text,$5,($4::text::jsonb->>'observedAt')::timestamptz)`, operation, p.AuthorizationHash, p.FinalConsumptionHash, string(wire), receipt.ProofHash)
		if err != nil {
			return zero, err
		}
	} else {
		return zero, err
	}
	var current bool
	err = tx.QueryRow(ctx, `SELECT clock_timestamp()<$1 AND clock_timestamp()>=$4 AND EXISTS(SELECT 1 FROM auth_sessions s JOIN runtime_recovery_release_authorizations a ON a.actor_session_id=s.id WHERE a.preparation_id=$2::uuid AND s.revoked_at IS NULL AND s.expires_at>clock_timestamp() AND s.expires_at>=$1) AND EXISTS(SELECT 1 FROM agents WHERE id=$3::uuid AND last_authenticated_heartbeat_at>clock_timestamp()-interval '90 seconds')`, a.ExpiresAt, operation, a.Expected.AgentID, p.ObservedAt).Scan(&current)
	if err != nil {
		return zero, err
	}
	if !current {
		return zero, ErrPreflight
	}
	if err = tx.Commit(ctx); err != nil {
		return zero, err
	}
	return receipt, nil
}

// Exact historical storage readback authenticates current token/generation,
// Agent/server. Expired authority or lost heartbeat is never renewed here.
func (s *Service) VerifyRecoveryFinalization(ctx context.Context, token, operation string, data []byte) (RecoveryFinalizationReceipt, error) {
	var zero RecoveryFinalizationReceipt
	p, err := DecodeRecoveryFinalizationProof(data)
	if err != nil || p.OperationID != operation {
		return zero, ErrPreflight
	}
	wire, err := json.Marshal(p)
	if err != nil {
		return zero, err
	}
	var saved, hash, authorizationHash, finalConsumptionHash string
	err = s.pool.QueryRow(ctx, `SELECT e.proof,e.proof_hash,e.authorization_hash,e.final_consumption_hash FROM runtime_recovery_finalization_evidence e JOIN runtime_recovery_release_authorizations r ON r.preparation_id=e.preparation_id AND r.authorization_hash=e.authorization_hash JOIN runtime_recovery_preparations p ON p.id=e.preparation_id JOIN agents a ON a.id=p.agent_id AND a.server_id=p.server_id AND a.credential_generation=p.agent_credential_generation WHERE e.preparation_id=$1::uuid AND a.token_hash=$2 AND a.credential_generation>0 AND a.id=$3::uuid AND a.server_id=$4::uuid`, operation, token, p.Observation.AgentID, p.Observation.ServerID).Scan(&saved, &hash, &authorizationHash, &finalConsumptionHash)
	if err != nil {
		return zero, recoveryAdmissionError(err)
	}
	receipt := recoveryFinalizationReceipt(p, wire, data)
	if saved != string(wire) || hash != receipt.ProofHash || authorizationHash != p.AuthorizationHash || finalConsumptionHash != p.FinalConsumptionHash {
		return zero, ErrPreflight
	}
	return receipt, nil
}
