package revocations

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
)

// RecoveryPreparation reserves Manager admission only. It is not an approval,
// challenge, runtime observation, or authority to clear an Agent fence.
type RecoveryPreparation struct {
	ID          string    `json:"id"`
	ServerID    string    `json:"serverId"`
	AgentID     string    `json:"agentId"`
	TaskID      string    `json:"taskId"`
	State       string    `json:"state"`
	RequestedBy string    `json:"requestedBy"`
	CreatedAt   time.Time `json:"createdAt"`
}

// RuntimeRecoveryInput is internal caller context. A future route must derive
// Actor from authenticated administrator identity, never from a request body.
// Hashes are claimed Agent receipt bindings, NOT independently verified proof.
// Envelope may contain secrets; it is compared then discarded, never persisted.
type RuntimeRecoveryInput struct {
	ServerID, AgentID, TaskID, MarkerHash, ReceiptHash, Actor string
	Envelope                                                  json.RawMessage
}

const recoverySelection = `SELECT id::text,server_id::text,agent_id::text,task_id::text,state,requested_by,created_at FROM runtime_recovery_preparations `

func scanRecovery(row pgx.Row) (RecoveryPreparation, error) {
	var p RecoveryPreparation
	err := row.Scan(&p.ID, &p.ServerID, &p.AgentID, &p.TaskID, &p.State, &p.RequestedBy, &p.CreatedAt)
	return p, err
}

// PrepareRuntimeRecovery is not connected to HTTP or a task producer. Exact
// retries return the existing reservation without claiming renewed readiness.
func (s *Service) PrepareRuntimeRecovery(ctx context.Context, in RuntimeRecoveryInput) (RecoveryPreparation, error) {
	p, err := s.prepareRuntimeRecovery(ctx, in)
	if err != nil {
		return RecoveryPreparation{}, ErrPreflight
	}
	return p, nil
}

func (s *Service) prepareRuntimeRecovery(ctx context.Context, in RuntimeRecoveryInput) (RecoveryPreparation, error) {
	for _, id := range []string{in.ServerID, in.AgentID, in.TaskID} {
		if !evidenceID.MatchString(id) {
			return RecoveryPreparation{}, ErrPreflight
		}
	}
	if !validRemovalHash(in.MarkerHash) || !validRemovalHash(in.ReceiptHash) || strings.TrimSpace(in.Actor) == "" || len(in.Actor) > 200 || len(in.Envelope) > 512*1024 {
		return RecoveryPreparation{}, ErrPreflight
	}
	// Reject duplicate keys and ambiguous JSON before decoding raw payload numbers.
	if _, _, _, err := canonicalRemovalJSON(in.Envelope); err != nil {
		return RecoveryPreparation{}, ErrPreflight
	}
	var envelope struct {
		Status  string          `json:"status"`
		Error   string          `json:"errorMessage"`
		Payload json.RawMessage `json:"resultPayload"`
	}
	d := json.NewDecoder(bytes.NewReader(in.Envelope))
	d.DisallowUnknownFields()
	if d.Decode(&envelope) != nil || envelope.Status != "succeeded" || envelope.Error != "" {
		return RecoveryPreparation{}, ErrPreflight
	}
	envelope.Payload = bytes.TrimSpace(envelope.Payload)
	if len(envelope.Payload) == 0 || bytes.Equal(envelope.Payload, []byte("null")) {
		envelope.Payload = json.RawMessage(`{}`)
	}
	if envelope.Payload[0] != '{' {
		return RecoveryPreparation{}, ErrPreflight
	}
	digest := sha256.Sum256(in.Envelope)
	hash := hex.EncodeToString(digest[:])
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return RecoveryPreparation{}, err
	}
	defer tx.Rollback(ctx)
	if _, err = tx.Exec(ctx, `SELECT pg_advisory_xact_lock(140159); LOCK TABLE routing_profiles,routing_profile_rules,server_routing_profiles,managed_routing_rule_sets IN SHARE MODE`); err != nil {
		return RecoveryPreparation{}, err
	}
	if _, err = tx.Exec(ctx, `SELECT id FROM servers WHERE id=$1::uuid FOR UPDATE`, in.ServerID); err != nil {
		return RecoveryPreparation{}, err
	}
	var generation int64
	if err = tx.QueryRow(ctx, `SELECT credential_generation FROM agents WHERE id=$1::uuid AND server_id=$2::uuid FOR UPDATE`, in.AgentID, in.ServerID).Scan(&generation); err != nil {
		return RecoveryPreparation{}, err
	}
	var existing string
	err = tx.QueryRow(ctx, `SELECT id::text FROM runtime_recovery_preparations WHERE server_id=$1::uuid AND state='prepared'`, in.ServerID).Scan(&existing)
	if err == nil {
		p, e := scanRecovery(tx.QueryRow(ctx, recoverySelection+`WHERE id=$1::uuid AND agent_id=$2::uuid AND task_id=$3::uuid AND marker_hash=$4 AND receipt_hash=$5 AND envelope_hash=$6 AND requested_by=$7 AND agent_credential_generation=$8`, existing, in.AgentID, in.TaskID, in.MarkerHash, in.ReceiptHash, hash, in.Actor, generation))
		if e != nil {
			return RecoveryPreparation{}, e
		}
		return p, tx.Commit(ctx)
	}
	if err != pgx.ErrNoRows {
		return RecoveryPreparation{}, err
	}
	var matches bool
	err = tx.QueryRow(ctx, `WITH eligible AS (
 SELECT result_payload FROM config_apply_jobs WHERE id=$1::uuid AND server_id=$2::uuid AND agent_id=$3::uuid AND action='apply' AND status='succeeded' AND COALESCE(error_message,'')=''
 UNION ALL SELECT result_payload FROM agent_operation_jobs WHERE id=$1::uuid AND server_id=$2::uuid AND agent_id=$3::uuid AND kind='vpn_core_service' AND operation='restart' AND status='succeeded' AND COALESCE(error_message,'')=''
 ) SELECT count(*)=1 AND COALESCE(bool_and(COALESCE(result_payload,'{}'::jsonb)=$4::jsonb),false) FROM eligible`, in.TaskID, in.ServerID, in.AgentID, envelope.Payload).Scan(&matches)
	if err != nil || !matches {
		return RecoveryPreparation{}, ErrPreflight
	}
	p, err := scanRecovery(tx.QueryRow(ctx, `WITH inserted AS (INSERT INTO runtime_recovery_preparations(server_id,agent_id,agent_credential_generation,task_id,marker_hash,receipt_hash,envelope_hash,requested_by,last_action_by)
 VALUES($1::uuid,$2::uuid,$3,$4::uuid,$5,$6,$7,$8,$8) RETURNING *) SELECT id::text,server_id::text,agent_id::text,task_id::text,state,requested_by,created_at FROM inserted`, in.ServerID, in.AgentID, generation, in.TaskID, in.MarkerHash, in.ReceiptHash, hash, in.Actor))
	if err != nil {
		return RecoveryPreparation{}, err
	}
	return p, tx.Commit(ctx)
}

// CancelRuntimeRecovery only cancels a never-dispatched Manager preparation.
// No Agent contact, automatic expiry, background release or credential restore.
func (s *Service) CancelRuntimeRecovery(ctx context.Context, id, actor string) (RecoveryPreparation, error) {
	if !evidenceID.MatchString(id) || strings.TrimSpace(actor) == "" || len(actor) > 200 {
		return RecoveryPreparation{}, ErrPreflight
	}
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return RecoveryPreparation{}, ErrPreflight
	}
	defer tx.Rollback(ctx)
	var node string
	if err = tx.QueryRow(ctx, `SELECT server_id::text FROM runtime_recovery_preparations WHERE id=$1::uuid`, id).Scan(&node); err != nil {
		return RecoveryPreparation{}, ErrPreflight
	}
	if _, err = tx.Exec(ctx, `SELECT id FROM servers WHERE id=$1::uuid FOR UPDATE`, node); err != nil {
		return RecoveryPreparation{}, ErrPreflight
	}
	p, err := scanRecovery(tx.QueryRow(ctx, recoverySelection+`WHERE id=$1::uuid FOR UPDATE`, id))
	if err != nil {
		return RecoveryPreparation{}, ErrPreflight
	}
	if p.State == "prepared" {
		if _, err = tx.Exec(ctx, `UPDATE runtime_recovery_preparations SET state='cancelled',cancelled_at=now(),last_action_by=$2 WHERE id=$1::uuid`, id, actor); err != nil {
			return RecoveryPreparation{}, ErrPreflight
		}
		p.State = "cancelled"
	}
	if tx.Commit(ctx) != nil {
		return RecoveryPreparation{}, ErrPreflight
	}
	return p, nil
}
