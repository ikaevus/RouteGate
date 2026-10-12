package revocations

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"time"

	"github.com/ikaevus/routegate/backend/internal/auth"
	"github.com/jackc/pgx/v5"
)

type RecoveryObservationApprovalInput struct {
	PreparationID              string
	Expected                   json.RawMessage
	ConfirmReadOnlyObservation bool
}

type RecoveryObservationApproval struct {
	Challenge     RecoveryChallenge `json:"challenge"`
	ChallengeHash string            `json:"challengeHash"`
}

// ApproveRecoveryObservation has no HTTP route or dispatcher. Its only authority
// is to prepare a read-only observation request. Expected is a claimed snapshot,
// not verified Agent evidence; nothing here grants mutation or fence release.
// Actor identity comes exclusively from the authenticated context and is checked
// against the current database session, user, roles and permissions under locks.
func (s *Service) ApproveRecoveryObservation(ctx context.Context, in RecoveryObservationApprovalInput) (RecoveryObservationApproval, error) {
	result, err := s.approveRecoveryObservation(ctx, in)
	if err != nil {
		return RecoveryObservationApproval{}, ErrPreflight
	}
	return result, nil
}

func (s *Service) approveRecoveryObservation(ctx context.Context, in RecoveryObservationApprovalInput) (RecoveryObservationApproval, error) {
	u, ok := auth.UserFromContext(ctx)
	if !ok || !auth.IsAdminUser(u.UserProfile) || (!auth.HasRole(u.UserProfile, auth.RoleAdmin) && !auth.HasRole(u.UserProfile, auth.RoleSuperAdmin)) || !auth.HasPermission(u.UserProfile, "configs:apply") || !evidenceID.MatchString(u.ID) || !evidenceID.MatchString(u.SessionID) || !evidenceID.MatchString(in.PreparationID) || !in.ConfirmReadOnlyObservation {
		return RecoveryObservationApproval{}, ErrPreflight
	}
	expected, err := decodeRecoveryObservation(in.Expected)
	if err != nil {
		return RecoveryObservationApproval{}, err
	}
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return RecoveryObservationApproval{}, err
	}
	defer tx.Rollback(ctx)
	// Wait for node/preparation/Agent admission before holding authority locks.
	if _, err = tx.Exec(ctx, `SET LOCAL lock_timeout='2s'`); err != nil {
		return RecoveryObservationApproval{}, err
	}
	var node string
	if err = tx.QueryRow(ctx, `SELECT server_id::text FROM runtime_recovery_preparations WHERE id=$1::uuid`, in.PreparationID).Scan(&node); err != nil {
		return RecoveryObservationApproval{}, err
	}
	if _, err = tx.Exec(ctx, `SELECT id FROM servers WHERE id=$1::uuid FOR UPDATE`, node); err != nil {
		return RecoveryObservationApproval{}, err
	}
	var agent string
	var generation int64
	var marker, receipt, envelope, task, state string
	err = tx.QueryRow(ctx, `SELECT agent_id::text,agent_credential_generation,marker_hash,receipt_hash,envelope_hash,task_id::text,state FROM runtime_recovery_preparations WHERE id=$1::uuid FOR UPDATE`, in.PreparationID).Scan(&agent, &generation, &marker, &receipt, &envelope, &task, &state)
	if err != nil || state != "prepared" || expected.ServerID != node || expected.AgentID != agent || expected.TaskID != task || expected.MarkerHash != marker || expected.ReceiptHash != receipt || expected.EnvelopeHash != envelope {
		return RecoveryObservationApproval{}, ErrPreflight
	}
	var ready bool
	err = tx.QueryRow(ctx, `SELECT COALESCE(status='online' AND credential_generation=$3 AND current_runtime_mutation_fencing
 AND capabilities @> '{"runtimeMutationFencingV1":true}'::jsonb
 AND capabilities @> '{"runtimeRecoveryObservationV1":true}'::jsonb
 AND last_authenticated_heartbeat_generation=credential_generation
 AND last_authenticated_heartbeat_at>clock_timestamp()-interval '90 seconds',false)
 FROM agents WHERE id=$1::uuid AND server_id=$2::uuid FOR UPDATE`, agent, node, generation).Scan(&ready)
	if err != nil || !ready {
		return RecoveryObservationApproval{}, ErrPreflight
	}
	// Other users' login/session updates remain writable. RBAC table locks
	// stabilize permission removal; only the approving user/session rows lock.
	if _, err = tx.Exec(ctx, `LOCK TABLE user_roles,roles,role_permissions,permissions IN SHARE MODE`); err != nil {
		return RecoveryObservationApproval{}, err
	}
	var lockedUser string
	if err = tx.QueryRow(ctx, `SELECT u.id::text FROM users u JOIN auth_sessions s ON s.user_id=u.id WHERE u.id=$1::uuid AND s.id=$2::uuid FOR SHARE OF u,s`, u.ID, u.SessionID).Scan(&lockedUser); err != nil {
		return RecoveryObservationApproval{}, err
	}
	var allowed bool
	err = tx.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM users u JOIN auth_sessions s ON s.user_id=u.id
 WHERE u.id=$1::uuid AND s.id=$2::uuid AND u.status='active' AND u.user_type='human'
 AND s.revoked_at IS NULL AND s.expires_at>clock_timestamp()
 AND EXISTS(SELECT 1 FROM user_roles ur JOIN roles r ON r.id=ur.role_id WHERE ur.user_id=u.id AND r.code IN ('admin','super_admin'))
 AND EXISTS(SELECT 1 FROM user_roles ur JOIN role_permissions rp ON rp.role_id=ur.role_id JOIN permissions p ON p.id=rp.permission_id WHERE ur.user_id=u.id AND p.code='configs:apply'))`, u.ID, u.SessionID).Scan(&allowed)
	if err != nil || !allowed {
		return RecoveryObservationApproval{}, ErrPreflight
	}
	var saved, actor, hash string
	err = tx.QueryRow(ctx, `SELECT challenge,actor_user_id::text,challenge_hash FROM runtime_recovery_observation_approvals WHERE preparation_id=$1::uuid`, in.PreparationID).Scan(&saved, &actor, &hash)
	if err == nil {
		var c RecoveryChallenge
		var valid bool
		if json.Unmarshal([]byte(saved), &c) != nil || c.Expected != expected || actor != u.ID {
			return RecoveryObservationApproval{}, ErrPreflight
		}
		if err = tx.QueryRow(ctx, `SELECT expires_at>clock_timestamp() AND EXISTS(SELECT 1 FROM auth_sessions WHERE id=$2::uuid AND revoked_at IS NULL AND expires_at>clock_timestamp()) FROM runtime_recovery_observation_approvals WHERE preparation_id=$1::uuid`, in.PreparationID, u.SessionID).Scan(&valid); err != nil || !valid {
			return RecoveryObservationApproval{}, ErrPreflight
		}
		if err = tx.Commit(ctx); err != nil {
			return RecoveryObservationApproval{}, err
		}
		return RecoveryObservationApproval{c, hash}, nil
	}
	if err != pgx.ErrNoRows {
		return RecoveryObservationApproval{}, err
	}
	var issued time.Time
	if err = tx.QueryRow(ctx, `SELECT clock_timestamp()`).Scan(&issued); err != nil {
		return RecoveryObservationApproval{}, err
	}
	nonce := make([]byte, 32)
	if _, err = rand.Read(nonce); err != nil {
		return RecoveryObservationApproval{}, err
	}
	c := RecoveryChallenge{SchemaVersion: 1, OperationID: in.PreparationID, Nonce: hex.EncodeToString(nonce), IssuedAt: issued.UTC(), ExpiresAt: issued.UTC().Add(2 * time.Minute), Expected: expected}
	data, err := json.Marshal(c)
	if err != nil {
		return RecoveryObservationApproval{}, err
	}
	digest := sha256.Sum256(data)
	hash = hex.EncodeToString(digest[:])
	if _, err = tx.Exec(ctx, `INSERT INTO runtime_recovery_observation_approvals(preparation_id,actor_user_id,actor_session_id,challenge,challenge_hash,issued_at,expires_at) VALUES($1::uuid,$2::uuid,$3::uuid,$4,$5,$6,$7)`, in.PreparationID, u.ID, u.SessionID, string(data), hash, c.IssuedAt, c.ExpiresAt); err != nil {
		return RecoveryObservationApproval{}, err
	}
	if err = tx.Commit(ctx); err != nil {
		return RecoveryObservationApproval{}, err
	}
	return RecoveryObservationApproval{c, hash}, nil
}
