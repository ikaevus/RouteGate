package configs

import "context"

func (r *Repository) DeleteUnusedConfigVersion(ctx context.Context, serverID, versionID string) (bool, error) {
	result, err := r.pool.Exec(ctx, `
		DELETE FROM config_versions cv
		WHERE cv.server_id = $1::uuid
		  AND cv.id = $2::uuid
		  AND cv.applied_at IS NULL
		  AND NOT EXISTS (
			SELECT 1
			FROM config_apply_jobs j
			WHERE j.config_version_id = cv.id
		  )
	`, serverID, versionID)
	if err != nil {
		return false, err
	}
	return result.RowsAffected() == 1, nil
}
