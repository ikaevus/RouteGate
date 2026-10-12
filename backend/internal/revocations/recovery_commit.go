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

// Mirrors Agent's measured live-owner proof. Authenticated receipt of this
// claim does not prove continued flock ownership or authorize any unlock.
type RecoveryCommitProof struct {
	SchemaVersion   int                 `json:"schemaVersion"`
	OperationID     string              `json:"operationId"`
	GrantHash       string              `json:"grantHash"`
	ConsumptionHash string              `json:"consumptionHash"`
	ObservedAt      time.Time           `json:"observedAt"`
	Observation     RecoveryObservation `json:"observation"`
}

// Receipt proves immutable ledger storage only, never permission to release.
type RecoveryCommitReceipt struct {
	SchemaVersion   int    `json:"schemaVersion"`
	Recorded        bool   `json:"recorded"`
	OperationID     string `json:"operationId"`
	GrantHash       string `json:"grantHash"`
	ConsumptionHash string `json:"consumptionHash"`
	ProofHash       string `json:"proofHash"`
	RequestHash     string `json:"requestHash"`
}

func DecodeRecoveryCommitProof(data []byte) (RecoveryCommitProof, error) {
	var p RecoveryCommitProof
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
		return p, ErrPreflight
	}
	_, typed, _, err := canonicalRemovalJSON(wire)
	if err != nil || !bytes.Equal(canonical, typed) || p.SchemaVersion != 1 || !evidenceID.MatchString(p.OperationID) || !validRemovalHash(p.GrantHash) || !validRemovalHash(p.ConsumptionHash) || p.ObservedAt.IsZero() {
		return RecoveryCommitProof{}, ErrPreflight
	}
	o, _ := json.Marshal(p.Observation)
	if _, err = decodeRecoveryObservation(o); err != nil {
		return RecoveryCommitProof{}, ErrPreflight
	}
	return p, nil
}

func recoveryCommitReceipt(p RecoveryCommitProof, wire, data []byte) RecoveryCommitReceipt {
	proofHash := sha256.Sum256(wire)
	requestHash := sha256.Sum256(data)
	return RecoveryCommitReceipt{1, true, p.OperationID, p.GrantHash, p.ConsumptionHash, hex.EncodeToString(proofHash[:]), hex.EncodeToString(requestHash[:])}
}

// AcceptRecoveryCommit records a fresh authenticated proof once. Retries must
// match the retained typed bytes exactly and still satisfy current admission.
// Reservation/grant/fence are retained, including after expiry or authority loss.
func (s *Service) AcceptRecoveryCommit(ctx context.Context, tokenHash, operation string, data []byte) (RecoveryCommitReceipt, error) {
	var zero RecoveryCommitReceipt
	p, err := DecodeRecoveryCommitProof(data)
	if err != nil || p.OperationID != operation {
		return zero, ErrPreflight
	}
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return zero, err
	}
	defer tx.Rollback(ctx)
	g, hash, now, err := lockRecoveryGrantDelivery(ctx, tx, tokenHash, operation)
	if err != nil || p.GrantHash != hash || p.Observation != g.Expected || p.ObservedAt.Before(g.IssuedAt) || !p.ObservedAt.Before(g.ExpiresAt) || p.ObservedAt.After(now) {
		return zero, ErrPreflight
	}
	wire, err := json.Marshal(p)
	if err != nil {
		return zero, err
	}
	receipt := recoveryCommitReceipt(p, wire, data)
	var saved string
	err = tx.QueryRow(ctx, `SELECT proof FROM runtime_recovery_commit_evidence WHERE preparation_id=$1::uuid`, operation).Scan(&saved)
	if err == nil {
		if saved != string(wire) {
			return zero, ErrPreflight
		}
	} else if err == pgx.ErrNoRows {
		// Use PostgreSQL's JSON timestamp parser for both the indexed timestamp
		// and trigger comparison, preserving sub-microsecond wire precision.
		_, err = tx.Exec(ctx, `INSERT INTO runtime_recovery_commit_evidence(preparation_id,grant_hash,consumption_hash,proof,proof_hash,observed_at)
 VALUES($1::uuid,$2,$3,$4::text,$5,($4::text::jsonb->>'observedAt')::timestamptz)`, operation, p.GrantHash, p.ConsumptionHash, string(wire), receipt.ProofHash)
		if err != nil {
			return zero, err
		}
	} else {
		return zero, err
	}
	// Even an identical retry cannot turn expired historical storage into fresh
	// admission after a slow query or lock wait.
	var current bool
	if err = tx.QueryRow(ctx, `SELECT clock_timestamp()<$1 AND clock_timestamp()>=$4 AND EXISTS(SELECT 1 FROM auth_sessions s JOIN runtime_recovery_mutation_grants g ON g.actor_session_id=s.id WHERE g.preparation_id=$2::uuid AND s.revoked_at IS NULL AND s.expires_at>clock_timestamp()) AND EXISTS(SELECT 1 FROM agents WHERE id=$3::uuid AND last_authenticated_heartbeat_at>clock_timestamp()-interval '90 seconds')`, g.ExpiresAt, operation, g.Expected.AgentID, p.ObservedAt).Scan(&current); err != nil || !current {
		return zero, ErrPreflight
	}
	if err = tx.Commit(ctx); err != nil {
		return zero, err
	}
	return receipt, nil
}

// VerifyRecoveryCommit is historical readback of an exact retained proof.
// Current credentials/generation and exact Agent/server bindings are mandatory,
// but expired/revoked issuer authority or lost heartbeat is not renewed here.
// No admission locks, writes, capability renewal or runtime action occurs.
func (s *Service) VerifyRecoveryCommit(ctx context.Context, tokenHash, operation string, data []byte) (RecoveryCommitReceipt, error) {
	var zero RecoveryCommitReceipt
	p, err := DecodeRecoveryCommitProof(data)
	if err != nil || p.OperationID != operation {
		return zero, ErrPreflight
	}
	wire, err := json.Marshal(p)
	if err != nil {
		return zero, err
	}
	var saved, grantHash, proofHash, consumptionHash string
	err = s.pool.QueryRow(ctx, `SELECT c.proof,c.grant_hash,c.proof_hash,c.consumption_hash FROM runtime_recovery_commit_evidence c
 JOIN runtime_recovery_mutation_grants g ON g.preparation_id=c.preparation_id AND g.grant_hash=c.grant_hash
 JOIN runtime_recovery_preparations p ON p.id=c.preparation_id
 JOIN agents a ON a.id=p.agent_id AND a.server_id=p.server_id AND a.credential_generation=p.agent_credential_generation
 WHERE c.preparation_id=$1::uuid AND a.token_hash=$2 AND a.credential_generation>0 AND a.id=$3::uuid AND a.server_id=$4::uuid`, operation, tokenHash, p.Observation.AgentID, p.Observation.ServerID).Scan(&saved, &grantHash, &proofHash, &consumptionHash)
	if err != nil {
		return zero, ErrPreflight
	}
	receipt := recoveryCommitReceipt(p, wire, data)
	if saved != string(wire) || grantHash != p.GrantHash || consumptionHash != p.ConsumptionHash || proofHash != receipt.ProofHash {
		return zero, ErrPreflight
	}
	return receipt, nil
}
