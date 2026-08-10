package dashboard

import "context"

type NodeLocationCount struct {
	Location string `json:"location"`
	Count    int    `json:"count"`
}

type NodeDistribution struct {
	TotalServers int                 `json:"totalServers"`
	Locations    []NodeLocationCount `json:"locations"`
}

type nodeReader interface {
	GetNodeDistribution(context.Context, int) (NodeDistribution, error)
}

func (r *Repository) GetNodeDistribution(ctx context.Context, limit int) (NodeDistribution, error) {
	rows, err := r.pool.Query(ctx, `
		SELECT
			COALESCE(NULLIF(BTRIM(location), ''), '') AS location,
			COUNT(*)::int AS node_count,
			SUM(COUNT(*)) OVER ()::int AS total_servers
		FROM servers
		GROUP BY COALESCE(NULLIF(BTRIM(location), ''), '')
		ORDER BY node_count DESC, location ASC
		LIMIT $1
	`, limit)
	if err != nil {
		return NodeDistribution{}, err
	}
	defer rows.Close()

	result := NodeDistribution{Locations: make([]NodeLocationCount, 0, limit)}
	for rows.Next() {
		var item NodeLocationCount
		var totalServers int
		if err := rows.Scan(&item.Location, &item.Count, &totalServers); err != nil {
			return NodeDistribution{}, err
		}
		result.TotalServers = totalServers
		result.Locations = append(result.Locations, item)
	}
	if err := rows.Err(); err != nil {
		return NodeDistribution{}, err
	}

	return result, nil
}
