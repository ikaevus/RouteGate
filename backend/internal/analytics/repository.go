package analytics

import (
	"context"
	"database/sql"
	"sort"
	"strings"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/ikaevus/routegate/backend/internal/observability"
)

type Repository struct {
	pool *pgxpool.Pool
	now  func() time.Time
}

func NewRepository(pool *pgxpool.Pool) *Repository {
	return &Repository{pool: pool, now: time.Now}
}

func (r *Repository) Overview(ctx context.Context) (Overview, error) {
	now := r.now().UTC()
	nodes, err := r.listNodes(ctx, now)
	if err != nil {
		return Overview{}, err
	}
	checks, err := r.listHealth(ctx)
	if err != nil {
		return Overview{}, err
	}
	alerts, err := r.listAlerts(ctx)
	if err != nil {
		return Overview{}, err
	}

	checksByServer := make(map[string][]observability.HealthCheck)
	for _, check := range checks {
		checksByServer[check.Resource.ID] = append(checksByServer[check.Resource.ID], check)
	}
	alertCountByServer := make(map[string]int)
	criticalByServer := make(map[string]bool)
	for _, alert := range alerts {
		alertCountByServer[alert.ServerID]++
		if alert.Severity == string(observability.SeverityCritical) && alert.State == string(observability.AlertFiring) {
			criticalByServer[alert.ServerID] = true
		}
	}

	summary := OverviewSummary{TotalNodes: len(nodes), ActiveAlerts: len(alerts)}
	for _, alert := range alerts {
		if alert.Severity == string(observability.SeverityCritical) && alert.State == string(observability.AlertFiring) {
			summary.CriticalAlerts++
		}
	}
	for i := range nodes {
		node := &nodes[i]
		node.AlertCount = alertCountByServer[node.ID]
		node.HasCritical = criticalByServer[node.ID]
		node.Health = analyticsNodeHealth(checksByServer[node.ID], now)
		if node.Location.Latitude != nil && node.Location.Longitude != nil {
			summary.LocatedNodes++
		}
		switch observability.HealthState(node.Health.State) {
		case observability.HealthHealthy:
			summary.HealthyNodes++
		case observability.HealthDegraded:
			summary.DegradedNodes++
		case observability.HealthUnhealthy:
			summary.UnhealthyNodes++
		default:
			summary.UnknownNodes++
		}
	}

	sort.SliceStable(nodes, func(i, j int) bool {
		left := analyticsHealthRank(nodes[i].Health.State)
		right := analyticsHealthRank(nodes[j].Health.State)
		if left != right {
			return left > right
		}
		return strings.ToLower(nodes[i].Name) < strings.ToLower(nodes[j].Name)
	})

	return Overview{GeneratedAt: now, Summary: summary, Nodes: nodes, Alerts: alerts}, nil
}

func (r *Repository) listNodes(ctx context.Context, now time.Time) ([]Node, error) {
	rows, err := r.pool.Query(ctx, `
		SELECT
			s.id::text,
			s.name,
			s.status,
			COALESCE(s.provider,''),
			COALESCE(s.public_ip::text,''),
			COALESCE(s.location,''),
			COALESCE(s.location_country,''),
			COALESCE(s.location_region,''),
			COALESCE(s.location_city,''),
			s.location_latitude,
			s.location_longitude,
			COALESCE(s.location_source,''),
			COALESCE(a.status,''),
			COALESCE(a.agent_version,''),
			a.last_seen_at,
			t.received_at,
			t.host_load_1,
			t.host_logical_cpus,
			t.host_memory_total_bytes,
			t.host_memory_available_bytes,
			t.host_root_fs_total_bytes,
			t.host_root_fs_free_bytes,
			t.host_uptime_seconds,
			COALESCE(t.vpn_core_type,''),
			COALESCE(t.vpn_core_installed,false),
			COALESCE(t.vpn_core_version,''),
			COALESCE(t.vpn_core_service_state,'')
		FROM servers s
		LEFT JOIN agents a ON a.server_id=s.id
		LEFT JOIN observability_agent_telemetry t ON t.server_id=s.id
		ORDER BY s.name, s.id
	`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	nodes := make([]Node, 0)
	for rows.Next() {
		var node Node
		var latitude, longitude sql.NullFloat64
		var lastSeen, receivedAt sql.NullTime
		var load1 sql.NullFloat64
		var logicalCPUs sql.NullInt64
		var memoryTotal, memoryAvailable, rootTotal, rootFree, uptime sql.NullInt64
		if err := rows.Scan(
			&node.ID,
			&node.Name,
			&node.Status,
			&node.Provider,
			&node.PublicIP,
			&node.Location.Label,
			&node.Location.Country,
			&node.Location.Region,
			&node.Location.City,
			&latitude,
			&longitude,
			&node.Location.Source,
			&node.Agent.Status,
			&node.Agent.Version,
			&lastSeen,
			&receivedAt,
			&load1,
			&logicalCPUs,
			&memoryTotal,
			&memoryAvailable,
			&rootTotal,
			&rootFree,
			&uptime,
			&node.VPNCore.Type,
			&node.VPNCore.Installed,
			&node.VPNCore.Version,
			&node.VPNCore.ServiceState,
		); err != nil {
			return nil, err
		}
		if latitude.Valid {
			value := latitude.Float64
			node.Location.Latitude = &value
		}
		if longitude.Valid {
			value := longitude.Float64
			node.Location.Longitude = &value
		}
		if lastSeen.Valid {
			value := lastSeen.Time.UTC()
			node.Agent.LastSeenAt = &value
		}
		if receivedAt.Valid {
			value := receivedAt.Time.UTC()
			node.Agent.ObservationReceivedAt = &value
			age := now.Sub(value).Seconds()
			if age < 0 {
				age = 0
			}
			node.Agent.ObservationAgeSeconds = &age
			node.Agent.ObservationFresh = age <= observability.AgentTelemetryHealthTTL.Seconds()
		}
		if load1.Valid {
			value := load1.Float64
			node.Resources.Load1 = &value
		}
		if logicalCPUs.Valid {
			value := logicalCPUs.Int64
			node.Resources.LogicalCPUs = &value
		}
		if memoryTotal.Valid && memoryAvailable.Valid && memoryTotal.Int64 > 0 {
			value := analyticsUsageRatio(memoryAvailable.Int64, memoryTotal.Int64)
			node.Resources.MemoryUsageRatio = &value
		}
		if rootTotal.Valid && rootFree.Valid && rootTotal.Int64 > 0 {
			value := analyticsUsageRatio(rootFree.Int64, rootTotal.Int64)
			node.Resources.RootFSUsageRatio = &value
		}
		if uptime.Valid {
			value := uptime.Int64
			node.Resources.HostUptimeSeconds = &value
		}
		nodes = append(nodes, node)
	}
	return nodes, rows.Err()
}

func (r *Repository) listHealth(ctx context.Context) ([]observability.HealthCheck, error) {
	rows, err := r.pool.Query(ctx, `
		SELECT resource_id, check_key, component, state, required,
		       COALESCE(reason_code,''), COALESCE(summary,''), COALESCE(recommended_action,''),
		       observed_at, expires_at
		FROM observability_current_health
		WHERE resource_type='server'
		ORDER BY resource_id, check_key
	`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	items := make([]observability.HealthCheck, 0)
	for rows.Next() {
		var check observability.HealthCheck
		var state string
		if err := rows.Scan(
			&check.Resource.ID,
			&check.Key,
			&check.Component,
			&state,
			&check.Required,
			&check.ReasonCode,
			&check.Summary,
			&check.RecommendedAction,
			&check.ObservedAt,
			&check.ExpiresAt,
		); err != nil {
			return nil, err
		}
		check.Resource.Type = "server"
		check.State = observability.HealthState(state)
		items = append(items, check)
	}
	return items, rows.Err()
}

func (r *Repository) listAlerts(ctx context.Context) ([]Alert, error) {
	rows, err := r.pool.Query(ctx, `
		SELECT
			a.id::text,
			a.resource_id,
			COALESCE(s.name,a.resource_id),
			a.severity,
			a.condition_state,
			a.summary,
			COALESCE(a.reason_code,''),
			a.started_at,
			a.firing_at,
			EXISTS (SELECT 1 FROM observability_alert_acknowledgements ack WHERE ack.alert_id=a.id)
		FROM observability_alerts a
		LEFT JOIN servers s ON s.id::text=a.resource_id
		WHERE a.resource_type='server'
		  AND a.condition_state IN ('pending','firing')
		ORDER BY
			CASE a.severity WHEN 'critical' THEN 0 ELSE 1 END,
			CASE a.condition_state WHEN 'firing' THEN 0 ELSE 1 END,
			a.started_at DESC
		LIMIT 100
	`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	items := make([]Alert, 0)
	for rows.Next() {
		var alert Alert
		if err := rows.Scan(
			&alert.ID,
			&alert.ServerID,
			&alert.ServerName,
			&alert.Severity,
			&alert.State,
			&alert.Summary,
			&alert.ReasonCode,
			&alert.StartedAt,
			&alert.FiringAt,
			&alert.Acknowledged,
		); err != nil {
			return nil, err
		}
		items = append(items, alert)
	}
	return items, rows.Err()
}

func analyticsNodeHealth(checks []observability.HealthCheck, now time.Time) NodeHealth {
	if len(checks) == 0 {
		return NodeHealth{State: string(observability.HealthUnknown), ReasonCode: "health_not_evaluated", Summary: "RouteGate has not evaluated this server yet.", RecommendedAction: "check_agent_connectivity"}
	}
	aggregate := observability.AggregateRequiredHealth(checks, now)
	result := NodeHealth{State: string(aggregate.State)}
	for _, check := range checks {
		if !check.Required || check.EffectiveState(now) != aggregate.State {
			continue
		}
		result.ReasonCode = check.ReasonCode
		result.Summary = check.Summary
		result.RecommendedAction = check.RecommendedAction
		break
	}
	if result.Summary == "" && aggregate.State == observability.HealthHealthy {
		result.Summary = "All required RouteGate health checks are healthy."
	}
	return result
}

func analyticsHealthRank(state string) int {
	switch observability.HealthState(state) {
	case observability.HealthUnhealthy:
		return 4
	case observability.HealthDegraded:
		return 3
	case observability.HealthUnknown:
		return 2
	case observability.HealthHealthy:
		return 1
	default:
		return 0
	}
}

func analyticsUsageRatio(free, total int64) float64 {
	if total <= 0 {
		return 0
	}
	value := 1 - (float64(free) / float64(total))
	if value < 0 {
		return 0
	}
	if value > 1 {
		return 1
	}
	return value
}
