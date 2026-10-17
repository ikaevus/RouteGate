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

const RecoveryRetentionPurpose = "runtime_mutation_fence_release_retention_evidence_v1"

// An authenticated Agent statement about a local backup. Its digest does not
// prove filesystem durability, package contents, deletion or live VPN traffic.
type RecoveryRetentionProof struct {
	SchemaVersion             int                 `json:"schemaVersion"`
	Purpose                   string              `json:"purpose"`
	OperationID               string              `json:"operationId"`
	AgentCredentialGeneration int64               `json:"agentCredentialGeneration"`
	AuthorizationHash         string              `json:"authorizationHash"`
	PreparedProofHash         string              `json:"preparedProofHash"`
	PreparedReceiptHash       string              `json:"preparedReceiptHash"`
	RetentionHash             string              `json:"retentionHash"`
	RetainedAt                time.Time           `json:"retainedAt"`
	ObservedAt                time.Time           `json:"observedAt"`
	Observation               RecoveryObservation `json:"observation"`
}

type RecoveryRetentionReceipt struct {
	SchemaVersion int    `json:"schemaVersion"`
	Recorded      bool   `json:"recorded"`
	OperationID   string `json:"operationId"`
	RetentionHash string `json:"retentionHash"`
	ProofHash     string `json:"proofHash"`
	RequestHash   string `json:"requestHash"`
}

func DecodeRecoveryRetentionProof(data []byte) (RecoveryRetentionProof, error) {
	var p RecoveryRetentionProof
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
		return RecoveryRetentionProof{}, ErrPreflight
	}
	wire, err := json.Marshal(p)
	if err != nil {
		return RecoveryRetentionProof{}, ErrPreflight
	}
	_, typed, _, err := canonicalRemovalJSON(wire)
	if err != nil || !bytes.Equal(canonical, typed) || p.SchemaVersion != 1 || p.Purpose != RecoveryRetentionPurpose || !evidenceID.MatchString(p.OperationID) || p.AgentCredentialGeneration < 1 || p.ObservedAt.IsZero() || p.RetainedAt.IsZero() || p.ObservedAt.Before(p.RetainedAt) {
		return RecoveryRetentionProof{}, ErrPreflight
	}
	for _, hash := range []string{p.AuthorizationHash, p.PreparedProofHash, p.PreparedReceiptHash, p.RetentionHash} {
		if !validRemovalHash(hash) {
			return RecoveryRetentionProof{}, ErrPreflight
		}
	}
	o, _ := json.Marshal(p.Observation)
	if _, err = decodeRecoveryObservation(o); err != nil {
		return RecoveryRetentionProof{}, ErrPreflight
	}
	return p, nil
}

func recoveryRetentionReceipt(p RecoveryRetentionProof, wire, data []byte) RecoveryRetentionReceipt {
	sum := sha256.Sum256(wire)
	req := sha256.Sum256(data)
	return RecoveryRetentionReceipt{1, true, p.OperationID, p.RetentionHash, hex.EncodeToString(sum[:]), hex.EncodeToString(req[:])}
}

func retentionMatchesPrepared(p RecoveryRetentionProof, preparedData, preparedHash string) bool {
	t, err := DecodeRecoveryPreparedReleaseProof([]byte(preparedData))
	sum := sha256.Sum256([]byte(preparedData))
	if err != nil || preparedHash != hex.EncodeToString(sum[:]) || p.PreparedProofHash != preparedHash || p.OperationID != t.OperationID || p.AgentCredentialGeneration != t.AgentCredentialGeneration || p.AuthorizationHash != t.AuthorizationHash || p.Observation != t.Observation || p.RetainedAt.Before(t.ObservedAt) || p.ObservedAt.Before(p.RetainedAt) {
		return false
	}
	ack, _ := json.Marshal(recoveryPreparedReleaseReceipt(t, []byte(preparedData), []byte(preparedData)))
	digest := sha256.Sum256(ack)
	return p.PreparedReceiptHash == hex.EncodeToString(digest[:])
}

func (s *Service) AcceptRecoveryRetention(ctx context.Context, token, operation string, data []byte) (RecoveryRetentionReceipt, error) {
	var zero RecoveryRetentionReceipt
	p, err := DecodeRecoveryRetentionProof(data)
	if err != nil || p.OperationID != operation {
		return zero, ErrPreflight
	}
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return zero, err
	}
	defer tx.Rollback(ctx)
	// Reuse the established server -> preparation -> Agent -> roles -> actor
	// lock order and current token/generation, reservation and human authority.
	a, hash, now, err := lockRecoveryReleaseDelivery(ctx, tx, token, operation)
	if err != nil {
		return zero, err
	}
	if p.AgentCredentialGeneration != a.AgentCredentialGeneration || p.AuthorizationHash != hash || p.Observation != a.Expected || p.RetainedAt.Before(a.IssuedAt) || !p.ObservedAt.Before(a.ExpiresAt) || p.ObservedAt.After(now) {
		return zero, ErrPreflight
	}
	var preparedData, preparedHash string
	err = tx.QueryRow(ctx, `SELECT proof,proof_hash FROM runtime_recovery_prepared_release_evidence WHERE preparation_id=$1::uuid`, operation).Scan(&preparedData, &preparedHash)
	if err != nil {
		return zero, recoveryAdmissionError(err)
	}
	if !retentionMatchesPrepared(p, preparedData, preparedHash) {
		return zero, ErrPreflight
	}
	wire, err := json.Marshal(p)
	if err != nil {
		return zero, err
	}
	receipt := recoveryRetentionReceipt(p, wire, data)
	var saved string
	err = tx.QueryRow(ctx, `SELECT proof FROM runtime_recovery_retention_evidence WHERE preparation_id=$1::uuid`, operation).Scan(&saved)
	if err == nil {
		if saved != string(wire) {
			return zero, ErrPreflight
		}
	} else if err == pgx.ErrNoRows {
		_, err = tx.Exec(ctx, `INSERT INTO runtime_recovery_retention_evidence(preparation_id,proof,proof_hash,observed_at) VALUES($1::uuid,$2::text,$3,($2::text::jsonb->>'observedAt')::timestamptz)`, operation, string(wire), receipt.ProofHash)
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

// Exact historical readback requires current credentials and bindings. It can
// read expired history, but cannot renew authority or complete the reservation.
func (s *Service) VerifyRecoveryRetention(ctx context.Context, token, operation string, data []byte) (RecoveryRetentionReceipt, error) {
	var zero RecoveryRetentionReceipt
	p, err := DecodeRecoveryRetentionProof(data)
	if err != nil || p.OperationID != operation {
		return zero, ErrPreflight
	}
	wire, err := json.Marshal(p)
	if err != nil {
		return zero, err
	}
	var saved, hash, preparedData, preparedHash string
	err = s.pool.QueryRow(ctx, `SELECT e.proof,e.proof_hash,f.proof,f.proof_hash FROM runtime_recovery_retention_evidence e JOIN runtime_recovery_prepared_release_evidence f ON f.preparation_id=e.preparation_id JOIN runtime_recovery_preparations p ON p.id=e.preparation_id JOIN agents a ON a.id=p.agent_id AND a.server_id=p.server_id AND a.credential_generation=p.agent_credential_generation WHERE e.preparation_id=$1::uuid AND a.token_hash=$2 AND a.credential_generation=$3 AND a.credential_generation>0 AND a.id=$4::uuid AND a.server_id=$5::uuid`, operation, token, p.AgentCredentialGeneration, p.Observation.AgentID, p.Observation.ServerID).Scan(&saved, &hash, &preparedData, &preparedHash)
	if err != nil {
		return zero, recoveryAdmissionError(err)
	}
	receipt := recoveryRetentionReceipt(p, wire, data)
	if saved != string(wire) || hash != receipt.ProofHash || !retentionMatchesPrepared(p, preparedData, preparedHash) {
		return zero, ErrPreflight
	}
	return receipt, nil
}
