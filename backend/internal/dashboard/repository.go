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

type TrafficTotals struct {
	RxBytes    int64 `json:"rxBytes"`
	TxBytes    int64 `json:"txBytes"`
	TotalBytes int64 `json:"totalBytes"`
}

type DailyTrafficUsage struct {
	Date string `json:"date"`
	TrafficTotals
}

type ServerTrafficUsage struct {
	ServerID   string `json:"serverId"`
	ServerName string `json:"serverName"`
	Available  bool   `json:"available"`
	TrafficTotals
}

type TrafficSnapshot struct {
	GeneratedAt      time.Time            `json:"generatedAt"`
	MonthStart       string               `json:"monthStart"`
	Last30DaysStart  string               `json:"last30DaysStart"`
	Last30DaysEnd    string               `json:"last30DaysEnd"`
	Server24hFrom    time.Time            `json:"server24hFrom"`
	Server24hTo      time.Time            `json:"server24hTo"`
	MonthlyAvailable bool                 `json:"monthlyAvailable"`
	DailyAvailable   bool                 `json:"dailyAvailable"`
	Monthly          TrafficTotals        `json:"monthly"`
	Daily            []DailyTrafficUsage  `json:"daily"`
	Servers          []ServerTrafficUsage `json:"servers"`
}

type activityReader interface {
	ListRecentDeployments(context.Context, int) ([]RecentDeployment, error)
	ListRecentAuditEvents(context.Context, int) ([]RecentAuditEvent, error)
}

type trafficReader interface {
	GetTrafficSnapshot(context.Context, time.Time, int) (TrafficSnapshot, error)
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

func (r *Repository) GetTrafficSnapshot(ctx context.Context, currentTime time.Time, serverLimit int) (TrafficSnapshot, error) {
	now := currentTime.UTC()
	today := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, time.UTC)
	monthStart := time.Date(now.Year(), now.Month(), 1, 0, 0, 0, 0, time.UTC)
	last30DaysStart := today.AddDate(0, 0, -29)
	server24hFrom := now.Add(-24 * time.Hour)

	snapshot := TrafficSnapshot{
		GeneratedAt:     now,
		MonthStart:      monthStart.Format("2006-01-02"),
		Last30DaysStart: last30DaysStart.Format("2006-01-02"),
		Last30DaysEnd:   today.Format("2006-01-02"),
		Server24hFrom:   server24hFrom,
		Server24hTo:     now,
	}

	if err := r.pool.QueryRow(ctx, `
		SELECT
			COUNT(*) > 0,
			COALESCE(SUM(rx_bytes), 0)::bigint,
			COALESCE(SUM(tx_bytes), 0)::bigint
		FROM traffic_usage_daily
		WHERE usage_date >= $1::date
		  AND usage_date <= $2::date
	`, monthStart, today).Scan(&snapshot.MonthlyAvailable, &snapshot.Monthly.RxBytes, &snapshot.Monthly.TxBytes); err != nil {
		return TrafficSnapshot{}, err
	}
	snapshot.Monthly.TotalBytes = snapshot.Monthly.RxBytes + snapshot.Monthly.TxBytes

	if err := r.pool.QueryRow(ctx, `
		SELECT EXISTS (
			SELECT 1
			FROM traffic_usage_daily
			WHERE usage_date >= $1::date
			  AND usage_date <= $2::date
		)
	`, last30DaysStart, today).Scan(&snapshot.DailyAvailable); err != nil {
		return TrafficSnapshot{}, err
	}

	dailyRows, err := r.pool.Query(ctx, `
		SELECT
			to_char(days.day::date, 'YYYY-MM-DD'),
			COALESCE(SUM(d.rx_bytes), 0)::bigint,
			COALESCE(SUM(d.tx_bytes), 0)::bigint
		FROM generate_series($1::date, $2::date, interval '1 day') AS days(day)
		LEFT JOIN traffic_usage_daily d ON d.usage_date = days.day::date
		GROUP BY days.day
		ORDER BY days.day
	`, last30DaysStart, today)
	if err != nil {
		return TrafficSnapshot{}, err
	}
	defer dailyRows.Close()

	snapshot.Daily = make([]DailyTrafficUsage, 0, 30)
	for dailyRows.Next() {
		var item DailyTrafficUsage
		if err := dailyRows.Scan(&item.Date, &item.RxBytes, &item.TxBytes); err != nil {
			return TrafficSnapshot{}, err
		}
		item.TotalBytes = item.RxBytes + item.TxBytes
		snapshot.Daily = append(snapshot.Daily, item)
	}
	if err := dailyRows.Err(); err != nil {
		return TrafficSnapshot{}, err
	}

	serverRows, err := r.pool.Query(ctx, `
		WITH recent_servers AS (
			SELECT id, name, created_at
			FROM servers
			ORDER BY created_at DESC
			LIMIT $3
		)
		SELECT
			s.id::text,
			s.name,
			COUNT(e.id)::int,
			COALESCE(SUM(e.rx_bytes), 0)::bigint,
			COALESCE(SUM(e.tx_bytes), 0)::bigint
		FROM recent_servers s
		LEFT JOIN traffic_usage_events e
		  ON e.server_id = s.id
		 AND e.observed_at >= $1
		 AND e.observed_at < $2
		GROUP BY s.id, s.name, s.created_at
		ORDER BY s.created_at DESC
	`, server24hFrom, now, serverLimit)
	if err != nil {
		return TrafficSnapshot{}, err
	}
	defer serverRows.Close()

	snapshot.Servers = make([]ServerTrafficUsage, 0, serverLimit)
	for serverRows.Next() {
		var item ServerTrafficUsage
		var eventCount int
		if err := serverRows.Scan(&item.ServerID, &item.ServerName, &eventCount, &item.RxBytes, &item.TxBytes); err != nil {
			return TrafficSnapshot{}, err
		}
		item.Available = eventCount > 0
		item.TotalBytes = item.RxBytes + item.TxBytes
		snapshot.Servers = append(snapshot.Servers, item)
	}
	if err := serverRows.Err(); err != nil {
		return TrafficSnapshot{}, err
	}

	return snapshot, nil
}
