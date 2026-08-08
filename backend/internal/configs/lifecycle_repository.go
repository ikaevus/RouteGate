package configs

import "context"

func (r *Repository) DeleteConfigVersion(ctx context.Context, serverID, versionID string) (bool, error) {
	result, err := r.pool.Exec(ctx, `
		DELETE FROM config_versions cv
		USING servers s
		WHERE cv.server_id = $1::uuid
		  AND cv.id = $2::uuid
		  AND s.id = cv.server_id
		  AND cv.pinned = FALSE
		  AND cv.id IS DISTINCT FROM s.active_config_version_id
		  AND NOT EXISTS (
			SELECT 1
			FROM config_apply_jobs j
			WHERE j.config_version_id = cv.id
			  AND j.status IN ('pending', 'in_progress')
		  )
	`, serverID, versionID)
	if err != nil {
		return false, err
	}
	return result.RowsAffected() == 1, nil
}

func (r *Repository) HasActiveConfigApplyJob(ctx context.Context, serverID, versionID string) (bool, error) {
	var active bool
	err := r.pool.QueryRow(ctx, `
		SELECT EXISTS (
			SELECT 1
			FROM config_apply_jobs
			WHERE server_id = $1::uuid
			  AND config_version_id = $2::uuid
			  AND status IN ('pending', 'in_progress')
		)
	`, serverID, versionID).Scan(&active)
	return active, err
}

func (r *Repository) SetConfigVersionPinned(ctx context.Context, serverID, versionID string, pinned bool) (ConfigVersion, error) {
	return scanConfigVersion(r.pool.QueryRow(ctx, `
		UPDATE config_versions
		SET pinned = $3
		WHERE server_id = $1::uuid
		  AND id = $2::uuid
		RETURNING
			id::text,
			server_id::text,
			version,
			config_hash,
			status,
			rendered_config,
			created_at,
			applied_at,
			pinned
	`, serverID, versionID, pinned))
}
