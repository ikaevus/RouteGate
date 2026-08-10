package dashboard

import (
	"context"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
)

type RecentDeployment struct {
	ID              string     `json:"id"`
	ServerID        string     `json:"serverId"`
	ServerName      string     `json:"serverName"`
	ConfigVersionID string     `json:"configVersionId"`
	ConfigVersion   int        `json:"configVersion"`
	Action          string     `json:"action"`
	Status          string     `json:"status"`
	CreatedAt       time.Time  `json:"createdAt"`
	CompletedAt     *time.Time `json:"completedAt,omitempty"`
}

type RecentAuditEvent struct {
	ID           string    `json:"id"`
	Actor        string    `json:"actor"`
	ActorType    string    `json:"actorType"`
	Action       string    `json:"action"`
	ResourceType string    `json:"resourceType"`
	ResourceID   string    `json:"resourceId,omitempty"`
	Result       string    `json:"result"`
	CreatedAt    time.Time `json:"createdAt"`
}

type activityReader interface {
	ListRecentDeployments(context.Context, int) ([]RecentDeployment, error)
	ListRecentAuditEvents(context.Context, int) ([]RecentAuditEvent, error)
}

type Repository struct {
	pool *pgxpool.Pool
}

func NewRepository(pool *pgxpool.Pool) *Repository {
	return &Repository{pool: pool}
}

func (r *Repository) ListRecentDeployments(ctx context.Context, limit int) ([]RecentDeployment, error) {
	rows, err := r.pool.Query(ctx, `
		SELECT
			j.id::text,
			j.server_id::text,
			s.name,
			j.config_version_id::text,
			cv.version,
			j.action,
			j.status,
			j.created_at,
			j.completed_at
		FROM config_apply_jobs j
		JOIN servers s ON s.id = j.server_id
		JOIN config_versions cv ON cv.id = j.config_version_id
		ORDER BY j.created_at DESC, j.id DESC
		LIMIT $1`, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	items := make([]RecentDeployment, 0, limit)
	for rows.Next() {
		var item RecentDeployment
		if err := rows.Scan(
			&item.ID,
			&item.ServerID,
			&item.ServerName,
			&item.ConfigVersionID,
			&item.ConfigVersion,
			&item.Action,
			&item.Status,
			&item.CreatedAt,
			&item.CompletedAt,
		); err != nil {
			return nil, err
		}
		items = append(items, item)
	}

	return items, rows.Err()
}

func (r *Repository) ListRecentAuditEvents(ctx context.Context, limit int) ([]RecentAuditEvent, error) {
	rows, err := r.pool.Query(ctx, `
		SELECT
			e.id::text,
			COALESCE(
				NULLIF(u.display_name, ''),
				NULLIF(u.username, ''),
				NULLIF(u.email, ''),
				e.actor_type
			) AS actor,
			e.actor_type,
			e.action,
			e.resource_type,
			COALESCE(e.resource_id::text, ''),
			e.result,
			e.created_at
		FROM audit_events e
		LEFT JOIN users u ON u.id = e.actor_user_id
		ORDER BY e.created_at DESC, e.id DESC
		LIMIT $1`, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	items := make([]RecentAuditEvent, 0, limit)
	for rows.Next() {
		var item RecentAuditEvent
		if err := rows.Scan(
			&item.ID,
			&item.Actor,
			&item.ActorType,
			&item.Action,
			&item.ResourceType,
			&item.ResourceID,
			&item.Result,
			&item.CreatedAt,
		); err != nil {
			return nil, err
		}
		items = append(items, item)
	}

	return items, rows.Err()
}
