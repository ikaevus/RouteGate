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

const RecoveryTerminalPurpose = "runtime_mutation_retirement_evidence_v1"

// An authenticated Agent statement about retained archival history, not proof
// of filesystem durability, continued ownership, current traffic or release.
// IntentHash and TerminalHash are opaque commitments to private Agent records.
type RecoveryTerminalProof struct {
	SchemaVersion             int                 `json:"schemaVersion"`
	Purpose                   string              `json:"purpose"`
	OperationID               string              `json:"operationId"`
	AgentCredentialGeneration int64               `json:"agentCredentialGeneration"`
	AuthorizationHash         string              `json:"authorizationHash"`
	FinalConsumptionHash      string              `json:"finalConsumptionHash"`
	FinalProofHash            string              `json:"finalProofHash"`
	FinalReceiptHash          string              `json:"finalReceiptHash"`
	IntentHash                string              `json:"intentHash"`
	TerminalHash              string              `json:"terminalHash"`
	ObservedAt                time.Time           `json:"observedAt"`
	Observation               RecoveryObservation `json:"observation"`
}
type RecoveryTerminalReceipt struct {
	SchemaVersion int    `json:"schemaVersion"`
	Recorded      bool   `json:"recorded"`
	OperationID   string `json:"operationId"`
	TerminalHash  string `json:"terminalHash"`
	ProofHash     string `json:"proofHash"`
	RequestHash   string `json:"requestHash"`
}

func DecodeRecoveryTerminalProof(data []byte) (RecoveryTerminalProof, error) {
	var p RecoveryTerminalProof
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
		return RecoveryTerminalProof{}, ErrPreflight
	}
	wire, err := json.Marshal(p)
	if err != nil {
		return RecoveryTerminalProof{}, ErrPreflight
	}
	_, typed, _, err := canonicalRemovalJSON(wire)
	if err != nil || !bytes.Equal(canonical, typed) || p.SchemaVersion != 1 || p.Purpose != RecoveryTerminalPurpose || !evidenceID.MatchString(p.OperationID) || p.AgentCredentialGeneration < 1 || p.ObservedAt.IsZero() {
		return RecoveryTerminalProof{}, ErrPreflight
	}
	for _, hash := range []string{p.AuthorizationHash, p.FinalConsumptionHash, p.FinalProofHash, p.FinalReceiptHash, p.IntentHash, p.TerminalHash} {
		if !validRemovalHash(hash) {
			return RecoveryTerminalProof{}, ErrPreflight
		}
	}
	o, _ := json.Marshal(p.Observation)
	if _, err = decodeRecoveryObservation(o); err != nil {
		return RecoveryTerminalProof{}, ErrPreflight
	}
	return p, nil
}
func recoveryTerminalReceipt(p RecoveryTerminalProof, wire, data []byte) RecoveryTerminalReceipt {
	sum := sha256.Sum256(wire)
	req := sha256.Sum256(data)
	return RecoveryTerminalReceipt{1, true, p.OperationID, p.TerminalHash, hex.EncodeToString(sum[:]), hex.EncodeToString(req[:])}
}
func terminalMatchesFinal(p RecoveryTerminalProof, finalData string, finalHash string) bool {
	f, err := DecodeRecoveryFinalizationProof([]byte(finalData))
	sum := sha256.Sum256([]byte(finalData))
	if err != nil || finalHash != hex.EncodeToString(sum[:]) || p.FinalProofHash != finalHash || p.OperationID != f.OperationID || p.AuthorizationHash != f.AuthorizationHash || p.FinalConsumptionHash != f.FinalConsumptionHash || p.Observation != f.Observation || p.ObservedAt.Before(f.ObservedAt) {
		return false
	}
	// Agent pins the canonical final request; its private receipt has identical
	// request/proof hashes. Arbitrary transport formatting isn't release evidence.
	ack, _ := json.Marshal(recoveryFinalizationReceipt(f, []byte(finalData), []byte(finalData)))
	digest := sha256.Sum256(ack)
	return p.FinalReceiptHash == hex.EncodeToString(digest[:])
}

func (s *Service) AcceptRecoveryTerminal(ctx context.Context, token, operation string, data []byte) (RecoveryTerminalReceipt, error) {
	var zero RecoveryTerminalReceipt
	p, err := DecodeRecoveryTerminalProof(data)
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
	if p.AgentCredentialGeneration != a.AgentCredentialGeneration || p.AuthorizationHash != hash || p.Observation != a.Expected || p.ObservedAt.Before(a.IssuedAt) || !p.ObservedAt.Before(a.ExpiresAt) || p.ObservedAt.After(now) {
		return zero, ErrPreflight
	}
	var finalData, finalHash string
	err = tx.QueryRow(ctx, `SELECT proof,proof_hash FROM runtime_recovery_finalization_evidence WHERE preparation_id=$1::uuid`, operation).Scan(&finalData, &finalHash)
	if err != nil {
		return zero, recoveryAdmissionError(err)
	}
	if !terminalMatchesFinal(p, finalData, finalHash) {
		return zero, ErrPreflight
	}
	wire, err := json.Marshal(p)
	if err != nil {
		return zero, err
	}
	receipt := recoveryTerminalReceipt(p, wire, data)
	var saved string
	err = tx.QueryRow(ctx, `SELECT proof FROM runtime_recovery_terminal_evidence WHERE preparation_id=$1::uuid`, operation).Scan(&saved)
	if err == nil {
		if saved != string(wire) {
			return zero, ErrPreflight
		}
	} else if err == pgx.ErrNoRows {
		_, err = tx.Exec(ctx, `INSERT INTO runtime_recovery_terminal_evidence(preparation_id,proof,proof_hash,observed_at) VALUES($1::uuid,$2::text,$3,($2::text::jsonb->>'observedAt')::timestamptz)`, operation, string(wire), receipt.ProofHash)
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
func (s *Service) VerifyRecoveryTerminal(ctx context.Context, token, operation string, data []byte) (RecoveryTerminalReceipt, error) {
	var zero RecoveryTerminalReceipt
	p, err := DecodeRecoveryTerminalProof(data)
	if err != nil || p.OperationID != operation {
		return zero, ErrPreflight
	}
	wire, err := json.Marshal(p)
	if err != nil {
		return zero, err
	}
	var saved, hash, finalData, finalHash string
	err = s.pool.QueryRow(ctx, `SELECT e.proof,e.proof_hash,f.proof,f.proof_hash FROM runtime_recovery_terminal_evidence e JOIN runtime_recovery_finalization_evidence f ON f.preparation_id=e.preparation_id JOIN runtime_recovery_preparations p ON p.id=e.preparation_id JOIN agents a ON a.id=p.agent_id AND a.server_id=p.server_id AND a.credential_generation=p.agent_credential_generation WHERE e.preparation_id=$1::uuid AND a.token_hash=$2 AND a.credential_generation=$3 AND a.credential_generation>0 AND a.id=$4::uuid AND a.server_id=$5::uuid`, operation, token, p.AgentCredentialGeneration, p.Observation.AgentID, p.Observation.ServerID).Scan(&saved, &hash, &finalData, &finalHash)
	if err != nil {
		return zero, recoveryAdmissionError(err)
	}
	receipt := recoveryTerminalReceipt(p, wire, data)
	if saved != string(wire) || hash != receipt.ProofHash || !terminalMatchesFinal(p, finalData, finalHash) {
		return zero, ErrPreflight
	}
	return receipt, nil
}
