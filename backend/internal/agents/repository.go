package agents

import (
	"context"
	"strings"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
)

type Repository struct {
	pool *pgxpool.Pool
}

func NewRepository(pool *pgxpool.Pool) *Repository {
	return &Repository{pool: pool}
}

func (r *Repository) List(ctx context.Context) ([]Agent, error) {
	rows, err := r.pool.Query(ctx, `
		SELECT
			id::text,
			COALESCE(server_id::text, ''),
			name,
			COALESCE(version, ''),
			COALESCE(hostname, ''),
			status,
			COALESCE(last_seen_at, created_at),
			created_at
		FROM agents
		ORDER BY created_at DESC;
	`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	items := make([]Agent, 0)

	for rows.Next() {
		var item Agent

		if err := rows.Scan(
			&item.ID,
			&item.ServerID,
			&item.Name,
			&item.Version,
			&item.Hostname,
			&item.Status,
			&item.LastSeen,
			&item.CreatedAt,
		); err != nil {
			return nil, err
		}

		items = append(items, item)
	}

	if err := rows.Err(); err != nil {
		return nil, err
	}

	return items, nil
}

func (r *Repository) Register(ctx context.Context, request RegisterAgentRequest) (Agent, error) {
	var agent Agent

	serverID := normalizeServerID(request.ServerID)
	now := time.Now().UTC()

	err := r.pool.QueryRow(ctx, `
		INSERT INTO agents (
			server_id,
			name,
			version,
			hostname,
			status,
			last_seen_at
		)
		VALUES (
			$1,
			$2,
			NULLIF($3, ''),
			NULLIF($4, ''),
			'online',
			$5
		)
		RETURNING
			id::text,
			COALESCE(server_id::text, ''),
			name,
			COALESCE(version, ''),
			COALESCE(hostname, ''),
			status,
			COALESCE(last_seen_at, created_at),
			created_at;
	`,
		serverID,
		request.Name,
		fallback(request.Version, "0.1.0"),
		request.Hostname,
		now,
	).Scan(
		&agent.ID,
		&agent.ServerID,
		&agent.Name,
		&agent.Version,
		&agent.Hostname,
		&agent.Status,
		&agent.LastSeen,
		&agent.CreatedAt,
	)

	return agent, err
}

func (r *Repository) Heartbeat(ctx context.Context, request HeartbeatRequest) (time.Time, bool, error) {
	now := time.Now().UTC()
	status := fallback(request.Status, "online")

	commandTag, err := r.pool.Exec(ctx, `
		UPDATE agents
		SET
			status = $1,
			last_seen_at = $2,
			version = COALESCE(NULLIF($3, ''), version),
			hostname = COALESCE(NULLIF($4, ''), hostname),
			updated_at = now()
		WHERE id = $5::uuid;
	`,
		status,
		now,
		request.Version,
		request.Hostname,
		request.AgentID,
	)
	if err != nil {
		return now, false, err
	}

	if commandTag.RowsAffected() == 0 {
		return now, false, nil
	}

	_, _ = r.pool.Exec(ctx, `
		INSERT INTO agent_heartbeats (
			agent_id,
			payload
		)
		VALUES (
			$1::uuid,
			jsonb_build_object(
				'status', $2::text,
				'version', $3::text,
				'hostname', $4::text
			)
		);
	`,
		request.AgentID,
		status,
		request.Version,
		request.Hostname,
	)

	return now, true, nil
}

func (r *Repository) SeedDemo(ctx context.Context) error {
	var serverID string

	if err := r.pool.QueryRow(ctx, `
		SELECT id::text
		FROM servers
		ORDER BY created_at ASC
		LIMIT 1;
	`).Scan(&serverID); err != nil {
		return err
	}

	_, err := r.pool.Exec(ctx, `
		INSERT INTO agents (
			server_id,
			name,
			version,
			hostname,
			status,
			last_seen_at
		)
		VALUES (
			$1::uuid,
			'Demo Agent',
			'0.1.0',
			'fi-demo-routegate',
			'online',
			now()
		);
	`, serverID)

	return err
}

func normalizeServerID(value string) any {
	value = strings.TrimSpace(value)
	if value == "" || strings.HasPrefix(value, "srv-dev-") {
		return nil
	}

	return value
}

func fallback(value string, defaultValue string) string {
	if value == "" {
		return defaultValue
	}

	return value
}
