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

const RecoveryCleanedCommitPurpose = "runtime_mutation_cleaned_commit_evidence_v1"

// An authenticated Agent statement about local cleaned commit intent. Opaque
// intent/manifest/marker hashes are not filesystem proof or release authority.
type RecoveryCleanedCommitProof struct {
	SchemaVersion             int                 `json:"schemaVersion"`
	Purpose                   string              `json:"purpose"`
	OperationID               string              `json:"operationId"`
	AgentCredentialGeneration int64               `json:"agentCredentialGeneration"`
	AuthorizationHash         string              `json:"authorizationHash"`
	RetentionProofHash        string              `json:"retentionProofHash"`
	RetentionReceiptHash      string              `json:"retentionReceiptHash"`
	ReleaseIntentHash         string              `json:"releaseIntentHash"`
	CleanedIntentHash         string              `json:"cleanedIntentHash"`
	ManifestHash              string              `json:"manifestHash"`
	MarkerHash                string              `json:"markerHash"`
	IntentAt                  time.Time           `json:"intentAt"`
	ObservedAt                time.Time           `json:"observedAt"`
	Observation               RecoveryObservation `json:"observation"`
}

type RecoveryCleanedCommitReceipt struct {
	SchemaVersion     int    `json:"schemaVersion"`
	Recorded          bool   `json:"recorded"`
	OperationID       string `json:"operationId"`
	CleanedIntentHash string `json:"cleanedIntentHash"`
	ProofHash         string `json:"proofHash"`
	RequestHash       string `json:"requestHash"`
}

func DecodeRecoveryCleanedCommitProof(data []byte) (RecoveryCleanedCommitProof, error) {
	var p RecoveryCleanedCommitProof
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
		return RecoveryCleanedCommitProof{}, ErrPreflight
	}
	wire, err := json.Marshal(p)
	if err != nil {
		return RecoveryCleanedCommitProof{}, ErrPreflight
	}
	_, typed, _, err := canonicalRemovalJSON(wire)
	if err != nil || !bytes.Equal(canonical, typed) || p.SchemaVersion != 1 || p.Purpose != RecoveryCleanedCommitPurpose || !evidenceID.MatchString(p.OperationID) || p.AgentCredentialGeneration < 1 || p.ObservedAt.IsZero() || p.IntentAt.IsZero() || p.ObservedAt.Before(p.IntentAt) {
		return RecoveryCleanedCommitProof{}, ErrPreflight
	}
	for _, hash := range []string{p.AuthorizationHash, p.RetentionProofHash, p.RetentionReceiptHash, p.ReleaseIntentHash, p.CleanedIntentHash, p.ManifestHash, p.MarkerHash} {
		if !validRemovalHash(hash) {
			return RecoveryCleanedCommitProof{}, ErrPreflight
		}
	}
	o, _ := json.Marshal(p.Observation)
	if _, err = decodeRecoveryObservation(o); err != nil {
		return RecoveryCleanedCommitProof{}, ErrPreflight
	}
	return p, nil
}

func recoveryCleanedCommitReceipt(p RecoveryCleanedCommitProof, wire, data []byte) RecoveryCleanedCommitReceipt {
	sum := sha256.Sum256(wire)
	req := sha256.Sum256(data)
	return RecoveryCleanedCommitReceipt{1, true, p.OperationID, p.CleanedIntentHash, hex.EncodeToString(sum[:]), hex.EncodeToString(req[:])}
}

func cleanedCommitMatchesRetention(p RecoveryCleanedCommitProof, retentionData, retentionHash, preparedData, preparedHash string) bool {
	t, err := DecodeRecoveryRetentionProof([]byte(retentionData))
	sum := sha256.Sum256([]byte(retentionData))
	prepared, e := DecodeRecoveryPreparedReleaseProof([]byte(preparedData))
	if err != nil || e != nil || retentionHash != hex.EncodeToString(sum[:]) || !retentionMatchesPrepared(t, preparedData, preparedHash) || p.RetentionProofHash != retentionHash || p.ReleaseIntentHash != prepared.ReleaseIntentHash || p.OperationID != t.OperationID || p.AgentCredentialGeneration != t.AgentCredentialGeneration || p.AuthorizationHash != t.AuthorizationHash || p.Observation != t.Observation || p.IntentAt.Before(t.ObservedAt) || p.ObservedAt.Before(p.IntentAt) {
		return false
	}
	ack, _ := json.Marshal(recoveryRetentionReceipt(t, []byte(retentionData), []byte(retentionData)))
	digest := sha256.Sum256(ack)
	return p.RetentionReceiptHash == hex.EncodeToString(digest[:])
}

func (s *Service) AcceptRecoveryCleanedCommit(ctx context.Context, token, operation string, data []byte) (RecoveryCleanedCommitReceipt, error) {
	var zero RecoveryCleanedCommitReceipt
	p, err := DecodeRecoveryCleanedCommitProof(data)
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
	if p.AgentCredentialGeneration != a.AgentCredentialGeneration || p.AuthorizationHash != hash || p.Observation != a.Expected || p.IntentAt.Before(a.IssuedAt) || !p.ObservedAt.Before(a.ExpiresAt) || p.ObservedAt.After(now) {
		return zero, ErrPreflight
	}
	var retentionData, retentionHash, preparedData, preparedHash string
	err = tx.QueryRow(ctx, `SELECT r.proof,r.proof_hash,p.proof,p.proof_hash FROM runtime_recovery_retention_evidence r JOIN runtime_recovery_prepared_release_evidence p USING(preparation_id) WHERE r.preparation_id=$1::uuid`, operation).Scan(&retentionData, &retentionHash, &preparedData, &preparedHash)
	if err != nil {
		return zero, recoveryAdmissionError(err)
	}
	if !cleanedCommitMatchesRetention(p, retentionData, retentionHash, preparedData, preparedHash) {
		return zero, ErrPreflight
	}
	wire, err := json.Marshal(p)
	if err != nil {
		return zero, err
	}
	receipt := recoveryCleanedCommitReceipt(p, wire, data)
	var saved string
	err = tx.QueryRow(ctx, `SELECT proof FROM runtime_recovery_cleaned_commit_evidence WHERE preparation_id=$1::uuid`, operation).Scan(&saved)
	if err == nil {
		if saved != string(wire) {
			return zero, ErrPreflight
		}
	} else if err == pgx.ErrNoRows {
		_, err = tx.Exec(ctx, `INSERT INTO runtime_recovery_cleaned_commit_evidence(preparation_id,proof,proof_hash,observed_at) VALUES($1::uuid,$2::text,$3,($2::text::jsonb->>'observedAt')::timestamptz)`, operation, string(wire), receipt.ProofHash)
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
func (s *Service) VerifyRecoveryCleanedCommit(ctx context.Context, token, operation string, data []byte) (RecoveryCleanedCommitReceipt, error) {
	var zero RecoveryCleanedCommitReceipt
	p, err := DecodeRecoveryCleanedCommitProof(data)
	if err != nil || p.OperationID != operation {
		return zero, ErrPreflight
	}
	wire, err := json.Marshal(p)
	if err != nil {
		return zero, err
	}
	var saved, hash, retentionData, retentionHash, preparedData, preparedHash string
	err = s.pool.QueryRow(ctx, `SELECT e.proof,e.proof_hash,r.proof,r.proof_hash,f.proof,f.proof_hash FROM runtime_recovery_cleaned_commit_evidence e JOIN runtime_recovery_retention_evidence r ON r.preparation_id=e.preparation_id JOIN runtime_recovery_prepared_release_evidence f ON f.preparation_id=e.preparation_id JOIN runtime_recovery_preparations p ON p.id=e.preparation_id JOIN agents a ON a.id=p.agent_id AND a.server_id=p.server_id AND a.credential_generation=p.agent_credential_generation WHERE e.preparation_id=$1::uuid AND a.token_hash=$2 AND a.credential_generation=$3 AND a.credential_generation>0 AND a.id=$4::uuid AND a.server_id=$5::uuid`, operation, token, p.AgentCredentialGeneration, p.Observation.AgentID, p.Observation.ServerID).Scan(&saved, &hash, &retentionData, &retentionHash, &preparedData, &preparedHash)
	if err != nil {
		return zero, recoveryAdmissionError(err)
	}
	receipt := recoveryCleanedCommitReceipt(p, wire, data)
	if saved != string(wire) || hash != receipt.ProofHash || !cleanedCommitMatchesRetention(p, retentionData, retentionHash, preparedData, preparedHash) {
		return zero, ErrPreflight
	}
	return receipt, nil
}
