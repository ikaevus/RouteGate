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

const RecoveryPreparedReleasePurpose = "runtime_mutation_fence_release_preparation_evidence_v1"

// An authenticated Agent commitment to its prepared release intent, not proof
// of filesystem durability, traffic, deletion or authority to release a fence.
type RecoveryPreparedReleaseProof struct {
	SchemaVersion             int                 `json:"schemaVersion"`
	Purpose                   string              `json:"purpose"`
	OperationID               string              `json:"operationId"`
	AgentCredentialGeneration int64               `json:"agentCredentialGeneration"`
	AuthorizationHash         string              `json:"authorizationHash"`
	TerminalProofHash         string              `json:"terminalProofHash"`
	TerminalReceiptHash       string              `json:"terminalReceiptHash"`
	ReleaseIntentHash         string              `json:"releaseIntentHash"`
	PreparedAt                time.Time           `json:"preparedAt"`
	ObservedAt                time.Time           `json:"observedAt"`
	Observation               RecoveryObservation `json:"observation"`
}
type RecoveryPreparedReleaseReceipt struct {
	SchemaVersion     int    `json:"schemaVersion"`
	Recorded          bool   `json:"recorded"`
	OperationID       string `json:"operationId"`
	ReleaseIntentHash string `json:"releaseIntentHash"`
	ProofHash         string `json:"proofHash"`
	RequestHash       string `json:"requestHash"`
}

func DecodeRecoveryPreparedReleaseProof(data []byte) (RecoveryPreparedReleaseProof, error) {
	var p RecoveryPreparedReleaseProof
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
		return RecoveryPreparedReleaseProof{}, ErrPreflight
	}
	wire, err := json.Marshal(p)
	if err != nil {
		return RecoveryPreparedReleaseProof{}, ErrPreflight
	}
	_, typed, _, err := canonicalRemovalJSON(wire)
	if err != nil || !bytes.Equal(canonical, typed) || p.SchemaVersion != 1 || p.Purpose != RecoveryPreparedReleasePurpose || !evidenceID.MatchString(p.OperationID) || p.AgentCredentialGeneration < 1 || p.ObservedAt.IsZero() || p.PreparedAt.IsZero() {
		return RecoveryPreparedReleaseProof{}, ErrPreflight
	}
	for _, hash := range []string{p.AuthorizationHash, p.TerminalProofHash, p.TerminalReceiptHash, p.ReleaseIntentHash} {
		if !validRemovalHash(hash) {
			return RecoveryPreparedReleaseProof{}, ErrPreflight
		}
	}
	o, _ := json.Marshal(p.Observation)
	if _, err = decodeRecoveryObservation(o); err != nil {
		return RecoveryPreparedReleaseProof{}, ErrPreflight
	}
	return p, nil
}
func recoveryPreparedReleaseReceipt(p RecoveryPreparedReleaseProof, wire, data []byte) RecoveryPreparedReleaseReceipt {
	sum := sha256.Sum256(wire)
	req := sha256.Sum256(data)
	return RecoveryPreparedReleaseReceipt{1, true, p.OperationID, p.ReleaseIntentHash, hex.EncodeToString(sum[:]), hex.EncodeToString(req[:])}
}
func preparedReleaseMatchesTerminal(p RecoveryPreparedReleaseProof, terminalData, terminalHash string) bool {
	t, err := DecodeRecoveryTerminalProof([]byte(terminalData))
	sum := sha256.Sum256([]byte(terminalData))
	if err != nil || terminalHash != hex.EncodeToString(sum[:]) || p.TerminalProofHash != terminalHash || p.OperationID != t.OperationID || p.AgentCredentialGeneration != t.AgentCredentialGeneration || p.AuthorizationHash != t.AuthorizationHash || p.Observation != t.Observation || p.PreparedAt.Before(t.ObservedAt) || p.ObservedAt.Before(p.PreparedAt) {
		return false
	}
	ack, _ := json.Marshal(recoveryTerminalReceipt(t, []byte(terminalData), []byte(terminalData)))
	digest := sha256.Sum256(ack)
	return p.TerminalReceiptHash == hex.EncodeToString(digest[:])
}

func (s *Service) AcceptRecoveryPreparedRelease(ctx context.Context, token, operation string, data []byte) (RecoveryPreparedReleaseReceipt, error) {
	var zero RecoveryPreparedReleaseReceipt
	p, err := DecodeRecoveryPreparedReleaseProof(data)
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
	if p.AgentCredentialGeneration != a.AgentCredentialGeneration || p.AuthorizationHash != hash || p.Observation != a.Expected || p.PreparedAt.Before(a.IssuedAt) || p.ObservedAt.Before(p.PreparedAt) || !p.ObservedAt.Before(a.ExpiresAt) || p.ObservedAt.After(now) {
		return zero, ErrPreflight
	}
	var terminalData, terminalHash string
	err = tx.QueryRow(ctx, `SELECT proof,proof_hash FROM runtime_recovery_terminal_evidence WHERE preparation_id=$1::uuid`, operation).Scan(&terminalData, &terminalHash)
	if err != nil {
		return zero, recoveryAdmissionError(err)
	}
	if !preparedReleaseMatchesTerminal(p, terminalData, terminalHash) {
		return zero, ErrPreflight
	}
	wire, err := json.Marshal(p)
	if err != nil {
		return zero, err
	}
	receipt := recoveryPreparedReleaseReceipt(p, wire, data)
	var saved string
	err = tx.QueryRow(ctx, `SELECT proof FROM runtime_recovery_prepared_release_evidence WHERE preparation_id=$1::uuid`, operation).Scan(&saved)
	if err == nil {
		if saved != string(wire) {
			return zero, ErrPreflight
		}
	} else if err == pgx.ErrNoRows {
		_, err = tx.Exec(ctx, `INSERT INTO runtime_recovery_prepared_release_evidence(preparation_id,proof,proof_hash,observed_at) VALUES($1::uuid,$2::text,$3,($2::text::jsonb->>'observedAt')::timestamptz)`, operation, string(wire), receipt.ProofHash)
		if err != nil {
			return zero, err
		}
	} else {
		return zero, err
	}
	// Repeat time/session/heartbeat checks after all bounded work and any waits.
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

// Exact historical readback authenticates current credentials/generation but
// does not renew expired authority, finish cleanup or release any reservation.
func (s *Service) VerifyRecoveryPreparedRelease(ctx context.Context, token, operation string, data []byte) (RecoveryPreparedReleaseReceipt, error) {
	var zero RecoveryPreparedReleaseReceipt
	p, err := DecodeRecoveryPreparedReleaseProof(data)
	if err != nil || p.OperationID != operation {
		return zero, ErrPreflight
	}
	wire, err := json.Marshal(p)
	if err != nil {
		return zero, err
	}
	var saved, hash, terminalData, terminalHash string
	err = s.pool.QueryRow(ctx, `SELECT e.proof,e.proof_hash,f.proof,f.proof_hash FROM runtime_recovery_prepared_release_evidence e JOIN runtime_recovery_terminal_evidence f ON f.preparation_id=e.preparation_id JOIN runtime_recovery_preparations p ON p.id=e.preparation_id JOIN agents a ON a.id=p.agent_id AND a.server_id=p.server_id AND a.credential_generation=p.agent_credential_generation WHERE e.preparation_id=$1::uuid AND a.token_hash=$2 AND a.credential_generation=$3 AND a.credential_generation>0 AND a.id=$4::uuid AND a.server_id=$5::uuid`, operation, token, p.AgentCredentialGeneration, p.Observation.AgentID, p.Observation.ServerID).Scan(&saved, &hash, &terminalData, &terminalHash)
	if err != nil {
		return zero, recoveryAdmissionError(err)
	}
	receipt := recoveryPreparedReleaseReceipt(p, wire, data)
	if saved != string(wire) || hash != receipt.ProofHash || !preparedReleaseMatchesTerminal(p, terminalData, terminalHash) {
		return zero, ErrPreflight
	}
	return receipt, nil
}
