package configs

import (
	"context"

	"github.com/jackc/pgx/v5"
)

func (r *Repository) ListConfigApplyJobsPage(ctx context.Context, serverID string, limit, offset int) ([]ConfigApplyJob, int, error) {
	var total int
	if err := r.pool.QueryRow(ctx, `
		SELECT COUNT(*)
		FROM config_apply_jobs
		WHERE server_id = $1::uuid
	`, serverID).Scan(&total); err != nil {
		return nil, 0, err
	}

	rows, err := r.pool.Query(ctx, `
		SELECT
			id::text,
			server_id::text,
			COALESCE(agent_id::text, ''),
			config_version_id::text,
			action,
			status,
			request_payload,
			result_payload,
			COALESCE(error_message, ''),
			created_at,
			updated_at,
			started_at,
			completed_at
		FROM config_apply_jobs
		WHERE server_id = $1::uuid
		ORDER BY created_at DESC, id DESC
		LIMIT $2 OFFSET $3
	`, serverID, limit, offset)
	if err != nil {
		return nil, 0, err
	}
	defer rows.Close()

	items := make([]ConfigApplyJob, 0, limit)
	for rows.Next() {
		item, err := scanConfigApplyJob(rows)
		if err != nil {
			return nil, 0, err
		}
		items = append(items, item)
	}
	if err := rows.Err(); err != nil {
		return nil, 0, err
	}
	return items, total, nil
}

func (r *Repository) DeleteTerminalConfigApplyJobs(ctx context.Context, serverID string) (int64, error) {
	var exists bool
	if err := r.pool.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM servers WHERE id = $1::uuid)`, serverID).Scan(&exists); err != nil {
		return 0, err
	}
	if !exists {
		return 0, pgx.ErrNoRows
	}

	result, err := r.pool.Exec(ctx, `
		DELETE FROM config_apply_jobs
		WHERE server_id = $1::uuid
		  AND status IN ('succeeded', 'failed')
	`, serverID)
	if err != nil {
		return 0, err
	}
	return result.RowsAffected(), nil
}
