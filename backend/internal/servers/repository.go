package servers

import (
	"context"

	"github.com/jackc/pgx/v5/pgxpool"
)

type Repository struct {
	pool *pgxpool.Pool
}

func NewRepository(pool *pgxpool.Pool) *Repository {
	return &Repository{pool: pool}
}

func (r *Repository) List(ctx context.Context) ([]Server, error) {
	rows, err := r.pool.Query(ctx, `
		SELECT
			id::text,
			name,
			COALESCE(hostname, ''),
			COALESCE(public_ip::text, ''),
			COALESCE(location, ''),
			COALESCE(provider, ''),
			status,
			created_at
		FROM servers
		ORDER BY created_at DESC;
	`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	items := make([]Server, 0)

	for rows.Next() {
		var item Server

		if err := rows.Scan(
			&item.ID,
			&item.Name,
			&item.Hostname,
			&item.PublicIP,
			&item.Location,
			&item.Provider,
			&item.Status,
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

func (r *Repository) Create(ctx context.Context, request CreateServerRequest) (Server, error) {
	var server Server

	err := r.pool.QueryRow(ctx, `
		INSERT INTO servers (
			name,
			hostname,
			public_ip,
			location,
			provider,
			status
		)
		VALUES (
			$1,
			NULLIF($2, ''),
			NULLIF($3, '')::inet,
			NULLIF($4, ''),
			NULLIF($5, ''),
			'unknown'
		)
		RETURNING
			id::text,
			name,
			COALESCE(hostname, ''),
			COALESCE(public_ip::text, ''),
			COALESCE(location, ''),
			COALESCE(provider, ''),
			status,
			created_at;
	`,
		request.Name,
		request.Hostname,
		request.PublicIP,
		request.Location,
		request.Provider,
	).Scan(
		&server.ID,
		&server.Name,
		&server.Hostname,
		&server.PublicIP,
		&server.Location,
		&server.Provider,
		&server.Status,
		&server.CreatedAt,
	)

	return server, err
}

func (r *Repository) SeedDemo(ctx context.Context) error {
	_, err := r.pool.Exec(ctx, `
		INSERT INTO servers (
			name,
			hostname,
			public_ip,
			location,
			provider,
			status
		)
		VALUES (
			'Demo Finland VPS',
			'fi-demo.routegate.local',
			'203.0.113.10'::inet,
			'Finland',
			'Demo',
			'online'
		);
	`)

	return err
}
