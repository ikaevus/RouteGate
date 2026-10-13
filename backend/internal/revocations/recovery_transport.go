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

type RecoveryChallengeResponse struct {
	SchemaVersion int                 `json:"schemaVersion"`
	OperationID   string              `json:"operationId"`
	ChallengeHash string              `json:"challengeHash"`
	ObservedAt    time.Time           `json:"observedAt"`
	Observation   RecoveryObservation `json:"observation"`
}

type RecoveryEvidenceAcknowledgement struct {
	SchemaVersion int    `json:"schemaVersion"`
	Accepted      bool   `json:"accepted"`
	OperationID   string `json:"operationId"`
	ChallengeHash string `json:"challengeHash"`
	ResponseHash  string `json:"responseHash"`
}

// NextRecoveryChallenge is read-only polling, not a task claim or mutation
// grant. Delivery stops after evidence acceptance; every retry uses the same
// immutable approval. Operator routes and automatic approval remain absent.
func (s *Service) NextRecoveryChallenge(ctx context.Context, tokenHash string) (*RecoveryChallenge, error) {
	var operation string
	err := s.pool.QueryRow(ctx, `SELECT COALESCE((SELECT p.id::text FROM runtime_recovery_preparations p
 JOIN runtime_recovery_observation_approvals o ON o.preparation_id=p.id
 WHERE p.agent_id=a.id AND p.server_id=a.server_id AND p.state='prepared'
 AND o.expires_at>clock_timestamp() AND NOT EXISTS(SELECT 1 FROM runtime_recovery_observation_evidence e WHERE e.preparation_id=p.id)), '')
 FROM agents a WHERE a.token_hash=$1`, tokenHash).Scan(&operation)
	if err != nil || operation == "" {
		return nil, err
	}
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback(ctx)
	c, _, _, err := lockRecoveryTransport(ctx, tx, tokenHash, operation)
	if err != nil {
		return nil, err
	}
	var accepted bool
	if err = tx.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM runtime_recovery_observation_evidence WHERE preparation_id=$1::uuid)`, operation).Scan(&accepted); err != nil {
		return nil, err
	}
	if err = tx.Commit(ctx); err != nil {
		return nil, err
	}
	if accepted {
		return nil, nil
	}
	return &c, nil
}

// lockRecoveryTransport follows the reservation cancellation/approval lock
// order. Token/generation and readiness are rechecked after lock waits using
// wall time. No auth tables, runtime job or network operation is touched.
func lockRecoveryTransport(ctx context.Context, tx pgx.Tx, tokenHash, operation string) (RecoveryChallenge, string, time.Time, error) {
	var c RecoveryChallenge
	p, err := lockRecoveryPreparation(ctx, tx, tokenHash, operation)
	if err != nil {
		return c, "", time.Time{}, err
	}
	var saved, hash string
	var now time.Time
	err = tx.QueryRow(ctx, `SELECT challenge,challenge_hash,clock_timestamp() FROM runtime_recovery_observation_approvals WHERE preparation_id=$1::uuid`, operation).Scan(&saved, &hash, &now)
	if err != nil {
		return c, "", now, err
	}
	sum := sha256.Sum256([]byte(saved))
	if json.Unmarshal([]byte(saved), &c) != nil || hash != hex.EncodeToString(sum[:]) || c.SchemaVersion != 1 || c.OperationID != operation || !validRemovalHash(c.Nonce) || now.Before(c.IssuedAt) || !now.Before(c.ExpiresAt) || !c.ExpiresAt.After(c.IssuedAt) || c.ExpiresAt.Sub(c.IssuedAt) > 2*time.Minute {
		return c, "", now, ErrPreflight
	}
	if !p.matches(c.Expected) {
		return c, "", now, ErrPreflight
	}
	return c, hash, now, nil
}

type recoveryPreparationBinding struct {
	agent, node, task, marker, receipt, envelope string
	generation                                   int64
}

func (p recoveryPreparationBinding) matches(o RecoveryObservation) bool {
	return o.AgentID == p.agent && o.ServerID == p.node && o.TaskID == p.task && o.MarkerHash == p.marker && o.ReceiptHash == p.receipt && o.EnvelopeHash == p.envelope
}

// Shared current admission; historical authority deadlines are checked only by
// the caller that uses them. Fresh final authorization must not renew old grants.
func lockRecoveryPreparation(ctx context.Context, tx pgx.Tx, tokenHash, operation string) (recoveryPreparationBinding, error) {
	var p recoveryPreparationBinding
	if _, err := tx.Exec(ctx, `SET LOCAL lock_timeout='2s'`); err != nil {
		return p, err
	}
	var node string
	if err := tx.QueryRow(ctx, `SELECT server_id::text FROM runtime_recovery_preparations WHERE id=$1::uuid`, operation).Scan(&node); err != nil {
		return p, err
	}
	if _, err := tx.Exec(ctx, `SELECT id FROM servers WHERE id=$1::uuid FOR UPDATE`, node); err != nil {
		return p, err
	}
	var agent, task, marker, receipt, envelope, state string
	var generation int64
	err := tx.QueryRow(ctx, `SELECT agent_id::text,task_id::text,marker_hash,receipt_hash,envelope_hash,state,agent_credential_generation
 FROM runtime_recovery_preparations WHERE id=$1::uuid FOR UPDATE`, operation).Scan(&agent, &task, &marker, &receipt, &envelope, &state, &generation)
	if err != nil {
		return p, err
	}
	var ready bool
	err = tx.QueryRow(ctx, `SELECT COALESCE(status='online' AND credential_generation=$4 AND current_runtime_mutation_fencing
 AND capabilities @> '{"runtimeMutationFencingV1":true}'::jsonb
 AND capabilities @> '{"runtimeRecoveryObservationV1":true}'::jsonb
 AND last_authenticated_heartbeat_generation=credential_generation
 AND last_authenticated_heartbeat_at>clock_timestamp()-interval '90 seconds',false)
 FROM agents WHERE id=$1::uuid AND server_id=$2::uuid AND token_hash=$3 FOR UPDATE`, agent, node, tokenHash, generation).Scan(&ready)
	if err != nil {
		return p, err
	}
	if state != "prepared" || !ready {
		return p, ErrPreflight
	}
	return recoveryPreparationBinding{agent, node, task, marker, receipt, envelope, generation}, nil
}

func DecodeRecoveryChallengeResponse(data []byte) (RecoveryChallengeResponse, error) {
	var r RecoveryChallengeResponse
	if len(data) > 8192 {
		return r, ErrPreflight
	}
	_, inputCanonical, _, inputErr := canonicalRemovalJSON(data)
	if inputErr != nil {
		return r, ErrPreflight
	}
	d := json.NewDecoder(bytes.NewReader(data))
	d.DisallowUnknownFields()
	if d.Decode(&r) != nil {
		return RecoveryChallengeResponse{}, ErrPreflight
	}
	typed, err := json.Marshal(r)
	if err != nil {
		return RecoveryChallengeResponse{}, ErrPreflight
	}
	_, typedCanonical, _, err := canonicalRemovalJSON(typed)
	if err != nil || !bytes.Equal(inputCanonical, typedCanonical) {
		return RecoveryChallengeResponse{}, ErrPreflight
	}
	o, err := json.Marshal(r.Observation)
	if err != nil {
		return r, ErrPreflight
	}
	if _, err = decodeRecoveryObservation(o); err != nil || r.SchemaVersion != 1 || !evidenceID.MatchString(r.OperationID) || !validRemovalHash(r.ChallengeHash) || r.ObservedAt.IsZero() {
		return RecoveryChallengeResponse{}, ErrPreflight
	}
	return r, nil
}

// AcceptRecoveryEvidence authenticates the current Agent and retains one exact
// response. It is historical evidence only: reservation and Agent fence remain.
func (s *Service) AcceptRecoveryEvidence(ctx context.Context, tokenHash, operation string, data []byte) (RecoveryEvidenceAcknowledgement, error) {
	var ack RecoveryEvidenceAcknowledgement
	r, err := DecodeRecoveryChallengeResponse(data)
	if err != nil || r.OperationID != operation {
		return ack, ErrPreflight
	}
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return ack, err
	}
	defer tx.Rollback(ctx)
	c, hash, now, err := lockRecoveryTransport(ctx, tx, tokenHash, operation)
	if err != nil {
		return ack, err
	}
	if r.ChallengeHash != hash || r.Observation != c.Expected || r.ObservedAt.Before(c.IssuedAt) || !r.ObservedAt.Before(c.ExpiresAt) || r.ObservedAt.After(now) {
		return ack, ErrPreflight
	}
	canonical, err := json.Marshal(r)
	if err != nil {
		return ack, err
	}
	sum := sha256.Sum256(canonical)
	responseHash := hex.EncodeToString(sum[:])
	var saved string
	err = tx.QueryRow(ctx, `SELECT response FROM runtime_recovery_observation_evidence WHERE preparation_id=$1::uuid`, operation).Scan(&saved)
	if err == nil {
		if saved != string(canonical) {
			return ack, ErrPreflight
		}
	} else if err == pgx.ErrNoRows {
		// Derive the indexed timestamp from the retained JSON in PostgreSQL.
		// pgx binary time encoding truncates sub-microsecond precision, whereas
		// PostgreSQL's text timestamp parser rounds it. Using the same parser
		// for column and trigger avoids rejecting valid nanosecond responses.
		_, err = tx.Exec(ctx, `INSERT INTO runtime_recovery_observation_evidence(preparation_id,challenge_hash,response,response_hash,observed_at)
 VALUES($1::uuid,$2,$3::text,$4,($3::text::jsonb->>'observedAt')::timestamptz)`, operation, hash, string(canonical), responseHash)
		if err != nil {
			return ack, err
		}
	} else {
		return ack, err
	}
	if err = tx.Commit(ctx); err != nil {
		return ack, err
	}
	return RecoveryEvidenceAcknowledgement{1, true, operation, hash, responseHash}, nil
}
