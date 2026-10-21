package revocations

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"

	"github.com/jackc/pgx/v5"
)

// A storage acknowledgement, not Manager reservation completion and not an
// independent observation of the filesystem or of client VPN traffic.
type RecoveryAdmissionReleaseReceipt struct {
	SchemaVersion int    `json:"schemaVersion"`
	Recorded      bool   `json:"recorded"`
	OperationID   string `json:"operationId"`
	ProofHash     string `json:"proofHash"`
	RequestHash   string `json:"requestHash"`
}

func recoveryAdmissionReleaseReceipt(p RecoveryAdmissionReleaseProof, wire, request []byte) RecoveryAdmissionReleaseReceipt {
	proofSum := sha256.Sum256(wire)
	requestSum := sha256.Sum256(request)
	return RecoveryAdmissionReleaseReceipt{1, true, p.OperationID, hex.EncodeToString(proofSum[:]), hex.EncodeToString(requestSum[:])}
}

// Stored predecessors are immutable under their own SQL guards. Recheck their
// exact SHA-256 digests and the complete finalization -> terminal -> prepared
// -> retention -> cleaned chain, rather than trusting a single cleaned hash.
func releaseMatchesStoredHistory(p RecoveryAdmissionReleaseProof,
	cleaned, cleanedHash, retention, retentionHash, prepared, preparedHash,
	terminal, terminalHash, final, finalHash string) bool {
	c, e1 := DecodeRecoveryCleanedCommitProof([]byte(cleaned))
	r, e2 := DecodeRecoveryRetentionProof([]byte(retention))
	pr, e3 := DecodeRecoveryPreparedReleaseProof([]byte(prepared))
	t, e4 := DecodeRecoveryTerminalProof([]byte(terminal))
	if e1 != nil || e2 != nil || e3 != nil || e4 != nil ||
		!admissionReleaseMatchesCleaned(p, cleaned, cleanedHash) ||
		!cleanedCommitMatchesRetention(c, retention, retentionHash, prepared, preparedHash) ||
		!retentionMatchesPrepared(r, prepared, preparedHash) ||
		!preparedReleaseMatchesTerminal(pr, terminal, terminalHash) ||
		!terminalMatchesFinal(t, final, finalHash) {
		return false
	}
	return true
}

// AcceptRecoveryAdmissionRelease stores a statement from the authenticated
// Agent only while the original human release authorization is still live.
// It does NOT mark runtime credentials revoked, finish the reservation, remove
// a fence or dispatch a VPN mutation. It is not wired into an HTTP route yet.
func (s *Service) AcceptRecoveryAdmissionRelease(ctx context.Context, token, operation string, data []byte) (RecoveryAdmissionReleaseReceipt, error) {
	var zero RecoveryAdmissionReleaseReceipt
	p, err := DecodeRecoveryAdmissionReleaseProof(data)
	if err != nil || p.OperationID != operation {
		return zero, ErrPreflight
	}
	wire, err := json.Marshal(p)
	if err != nil || !bytes.Equal(data, wire) {
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
	if p.AgentCredentialGeneration != a.AgentCredentialGeneration ||
		p.AuthorizationHash != hash || p.Observation != a.Expected ||
		p.ReleasedAt.Before(a.IssuedAt) || !p.ObservedAt.Before(a.ExpiresAt) ||
		p.ReleasedAt.After(now) || p.ObservedAt.After(now) {
		return zero, ErrPreflight
	}
	var cleaned, cleanedHash, retention, retentionHash, prepared, preparedHash string
	var terminal, terminalHash, final, finalHash string
	err = tx.QueryRow(ctx, `SELECT c.proof,c.proof_hash,r.proof,r.proof_hash,
		t.proof,t.proof_hash,n.proof,n.proof_hash,f.proof,f.proof_hash
		FROM runtime_recovery_cleaned_commit_evidence c
		JOIN runtime_recovery_retention_evidence r USING(preparation_id)
		JOIN runtime_recovery_prepared_release_evidence t USING(preparation_id)
		JOIN runtime_recovery_terminal_evidence n USING(preparation_id)
		JOIN runtime_recovery_finalization_evidence f USING(preparation_id)
		WHERE c.preparation_id=$1::uuid`, operation).Scan(
		&cleaned, &cleanedHash, &retention, &retentionHash,
		&prepared, &preparedHash, &terminal, &terminalHash, &final, &finalHash)
	if err != nil {
		return zero, recoveryAdmissionError(err)
	}
	if !releaseMatchesStoredHistory(p, cleaned, cleanedHash, retention, retentionHash,
		prepared, preparedHash, terminal, terminalHash, final, finalHash) {
		return zero, ErrPreflight
	}
	receipt := recoveryAdmissionReleaseReceipt(p, wire, data)
	var saved string
	err = tx.QueryRow(ctx, `SELECT proof FROM runtime_recovery_admission_release_evidence
		WHERE preparation_id=$1::uuid`, operation).Scan(&saved)
	switch err {
	case nil:
		if saved != string(wire) {
			return zero, ErrPreflight
		}
	case pgx.ErrNoRows:
		_, err = tx.Exec(ctx, `INSERT INTO runtime_recovery_admission_release_evidence
			(preparation_id,proof,proof_hash,observed_at)
			VALUES($1::uuid,$2::text,$3,($2::text::jsonb->>'observedAt')::timestamptz)`,
			operation, string(wire), receipt.ProofHash)
		if err != nil {
			return zero, recoveryAdmissionError(err)
		}
	default:
		return zero, err
	}
	// Preserve the original half-open authority window across the entire write.
	// The row may be retained after expiry but this fresh acceptance cannot.
	var current bool
	err = tx.QueryRow(ctx, `SELECT clock_timestamp()<$1 AND clock_timestamp()>=$4
		AND EXISTS(SELECT 1 FROM auth_sessions s JOIN runtime_recovery_release_authorizations a
		ON a.actor_session_id=s.id WHERE a.preparation_id=$2::uuid
		AND s.revoked_at IS NULL AND s.expires_at>clock_timestamp()
		AND s.expires_at>=$1)
		AND EXISTS(SELECT 1 FROM agents WHERE id=$3::uuid
		AND last_authenticated_heartbeat_at>clock_timestamp()-interval '90 seconds')`,
		a.ExpiresAt, operation, a.Expected.AgentID, p.ObservedAt).Scan(&current)
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

// VerifyRecoveryAdmissionRelease is historical readback, not fresh runtime
// or authorization verification. Current Agent identity is still required.
func (s *Service) VerifyRecoveryAdmissionRelease(ctx context.Context, token, operation string, data []byte) (RecoveryAdmissionReleaseReceipt, error) {
	var zero RecoveryAdmissionReleaseReceipt
	p, err := DecodeRecoveryAdmissionReleaseProof(data)
	if err != nil || p.OperationID != operation {
		return zero, ErrPreflight
	}
	wire, err := json.Marshal(p)
	if err != nil || !bytes.Equal(data, wire) {
		return zero, ErrPreflight
	}
	var saved, hash, cleaned, cleanedHash, retention, retentionHash, prepared, preparedHash string
	var terminal, terminalHash, final, finalHash string
	err = s.pool.QueryRow(ctx, `SELECT e.proof,e.proof_hash,c.proof,c.proof_hash,
		r.proof,r.proof_hash,t.proof,t.proof_hash,n.proof,n.proof_hash,f.proof,f.proof_hash
		FROM runtime_recovery_admission_release_evidence e
		JOIN runtime_recovery_cleaned_commit_evidence c USING(preparation_id)
		JOIN runtime_recovery_retention_evidence r USING(preparation_id)
		JOIN runtime_recovery_prepared_release_evidence t USING(preparation_id)
		JOIN runtime_recovery_terminal_evidence n USING(preparation_id)
		JOIN runtime_recovery_finalization_evidence f USING(preparation_id)
		JOIN runtime_recovery_preparations p ON p.id=e.preparation_id
		JOIN agents a ON a.id=p.agent_id AND a.server_id=p.server_id
		AND a.credential_generation=p.agent_credential_generation
		WHERE e.preparation_id=$1::uuid AND a.token_hash=$2 AND a.credential_generation=$3
		AND a.credential_generation>0 AND a.id=$4::uuid AND a.server_id=$5::uuid`,
		operation, token, p.AgentCredentialGeneration, p.Observation.AgentID,
		p.Observation.ServerID).Scan(&saved, &hash, &cleaned, &cleanedHash,
		&retention, &retentionHash, &prepared, &preparedHash,
		&terminal, &terminalHash, &final, &finalHash)
	if err != nil {
		return zero, recoveryAdmissionError(err)
	}
	receipt := recoveryAdmissionReleaseReceipt(p, wire, data)
	if saved != string(wire) || hash != receipt.ProofHash ||
		!releaseMatchesStoredHistory(p, cleaned, cleanedHash, retention, retentionHash,
			prepared, preparedHash, terminal, terminalHash, final, finalHash) {
		return zero, ErrPreflight
	}
	return receipt, nil
}
