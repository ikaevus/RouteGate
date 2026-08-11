package dashboard

import (
	"context"
	"database/sql"
	"time"
)

const serverLoadFreshnessWindow = 90 * time.Second

type NodeLocationCount struct {
	Location string `json:"location"`
	Count    int    `json:"count"`
}

type ServerLoad struct {
	ServerID    string     `json:"serverId"`
	Load1       *float64   `json:"load1,omitempty"`
	Load5       *float64   `json:"load5,omitempty"`
	Load15      *float64   `json:"load15,omitempty"`
	LogicalCPUs *int       `json:"logicalCpus,omitempty"`
	CollectedAt *time.Time `json:"collectedAt,omitempty"`
}

type NodeDistribution struct {
	TotalServers int                 `json:"totalServers"`
	Locations    []NodeLocationCount `json:"locations"`
	ServerLoads  []ServerLoad        `json:"serverLoads"`
}

type nodeReader interface {
	GetNodeDistribution(context.Context, int, int) (NodeDistribution, error)
}

func (r *Repository) GetNodeDistribution(ctx context.Context, locationLimit, serverLimit int) (NodeDistribution, error) {
	rows, err := r.pool.Query(ctx, `
		SELECT
			COALESCE(NULLIF(BTRIM(location), ''), '') AS location,
			COUNT(*)::int AS node_count,
			SUM(COUNT(*)) OVER ()::int AS total_servers
		FROM servers
		GROUP BY COALESCE(NULLIF(BTRIM(location), ''), '')
		ORDER BY node_count DESC, location ASC
		LIMIT $1
	`, locationLimit)
	if err != nil {
		return NodeDistribution{}, err
	}

	result := NodeDistribution{
		Locations:   make([]NodeLocationCount, 0, locationLimit),
		ServerLoads: make([]ServerLoad, 0, serverLimit),
	}
	for rows.Next() {
		var item NodeLocationCount
		var totalServers int
		if err := rows.Scan(&item.Location, &item.Count, &totalServers); err != nil {
			rows.Close()
			return NodeDistribution{}, err
		}
		result.TotalServers = totalServers
		result.Locations = append(result.Locations, item)
	}
	if err := rows.Err(); err != nil {
		rows.Close()
		return NodeDistribution{}, err
	}
	rows.Close()

	loadRows, err := r.pool.Query(ctx, `
		WITH recent_servers AS (
			SELECT id, created_at
			FROM servers
			ORDER BY created_at DESC
			LIMIT $1
		)
		SELECT
			s.id::text,
			CASE WHEN a.last_seen_at >= now() - ($2 * interval '1 second') THEN a.runtime_load_1 ELSE NULL END,
			CASE WHEN a.last_seen_at >= now() - ($2 * interval '1 second') THEN a.runtime_load_5 ELSE NULL END,
			CASE WHEN a.last_seen_at >= now() - ($2 * interval '1 second') THEN a.runtime_load_15 ELSE NULL END,
			CASE WHEN a.last_seen_at >= now() - ($2 * interval '1 second') THEN a.runtime_logical_cpus ELSE NULL END,
			CASE WHEN a.last_seen_at >= now() - ($2 * interval '1 second') THEN a.runtime_collected_at ELSE NULL END
		FROM recent_servers s
		LEFT JOIN agents a ON a.server_id = s.id
		ORDER BY s.created_at DESC
	`, serverLimit, int(serverLoadFreshnessWindow/time.Second))
	if err != nil {
		return NodeDistribution{}, err
	}
	defer loadRows.Close()

	for loadRows.Next() {
		var item ServerLoad
		var load1, load5, load15 sql.NullFloat64
		var logicalCPUs sql.NullInt32
		var collectedAt sql.NullTime
		if err := loadRows.Scan(&item.ServerID, &load1, &load5, &load15, &logicalCPUs, &collectedAt); err != nil {
			return NodeDistribution{}, err
		}
		if load1.Valid {
			value := load1.Float64
			item.Load1 = &value
		}
		if load5.Valid {
			value := load5.Float64
			item.Load5 = &value
		}
		if load15.Valid {
			value := load15.Float64
			item.Load15 = &value
		}
		if logicalCPUs.Valid {
			value := int(logicalCPUs.Int32)
			item.LogicalCPUs = &value
		}
		if collectedAt.Valid {
			value := collectedAt.Time
			item.CollectedAt = &value
		}
		result.ServerLoads = append(result.ServerLoads, item)
	}
	if err := loadRows.Err(); err != nil {
		return NodeDistribution{}, err
	}

	return result, nil
}
