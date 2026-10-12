package revocations

import (
	"context"

	"github.com/ikaevus/routegate/backend/internal/auth"
	"github.com/jackc/pgx/v5"
)

func validRecoveryActor(u auth.AuthenticatedUser) bool {
	return auth.IsAdminUser(u.UserProfile) &&
		(auth.HasRole(u.UserProfile, auth.RoleAdmin) || auth.HasRole(u.UserProfile, auth.RoleSuperAdmin)) &&
		auth.HasPermission(u.UserProfile, "configs:apply") && evidenceID.MatchString(u.ID) && evidenceID.MatchString(u.SessionID)
}

// Call only after server/preparation/Agent locks. Authority remains stabilized
// through the transaction; unrelated user/session rows remain writable.
func lockRecoveryAuthority(ctx context.Context, tx pgx.Tx, u auth.AuthenticatedUser) error {
	if !validRecoveryActor(u) {
		return ErrPreflight
	}
	return lockRecoveryAuthoritySubject(ctx, tx, u.ID, u.SessionID)
}

// Subject comes only from immutable grant attribution during read-only delivery.
// Database checks, not ledger context claims, determine current authority.
func lockRecoveryAuthoritySubject(ctx context.Context, tx pgx.Tx, userID, sessionID string) error {
	if _, err := tx.Exec(ctx, `LOCK TABLE user_roles,roles,role_permissions,permissions IN SHARE MODE`); err != nil {
		return err
	}
	var locked string
	if err := tx.QueryRow(ctx, `SELECT u.id::text FROM users u JOIN auth_sessions s ON s.user_id=u.id WHERE u.id=$1::uuid AND s.id=$2::uuid FOR SHARE OF u,s`, userID, sessionID).Scan(&locked); err != nil {
		return err
	}
	var allowed bool
	err := tx.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM users u JOIN auth_sessions s ON s.user_id=u.id
 WHERE u.id=$1::uuid AND s.id=$2::uuid AND u.status='active' AND u.user_type='human'
 AND s.revoked_at IS NULL AND s.expires_at>clock_timestamp()
 AND EXISTS(SELECT 1 FROM user_roles ur JOIN roles r ON r.id=ur.role_id WHERE ur.user_id=u.id AND r.code IN ('admin','super_admin'))
 AND EXISTS(SELECT 1 FROM user_roles ur JOIN role_permissions rp ON rp.role_id=ur.role_id JOIN permissions p ON p.id=rp.permission_id WHERE ur.user_id=u.id AND p.code='configs:apply'))`, userID, sessionID).Scan(&allowed)
	if err != nil {
		return err
	}
	if !allowed {
		return ErrPreflight
	}
	return nil
}
