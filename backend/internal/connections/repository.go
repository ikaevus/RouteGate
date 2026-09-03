package connections

import (
	"context"
	"errors"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

var (
	ErrUnauthorizedAgent  = errors.New("unauthorized agent")
	ErrVPNAccountNotFound = errors.New("vpn account not found")
)

type Repository struct{ pool *pgxpool.Pool }

func NewRepository(pool *pgxpool.Pool) *Repository { return &Repository{pool: pool} }

func (r *Repository) ReplaceSnapshot(ctx context.Context, tokenHash string, input SnapshotInput) (SnapshotResponse, error) {
	var agentID, serverID string
	err := r.pool.QueryRow(ctx, `
		SELECT id::text, server_id::text
		FROM agents
		WHERE agent_key_hash=$1 AND status <> 'disabled'
	`, tokenHash).Scan(&agentID, &serverID)
	if errors.Is(err, pgx.ErrNoRows) { return SnapshotResponse{}, ErrUnauthorizedAgent }
	if err != nil { return SnapshotResponse{}, err }

	tx, err := r.pool.Begin(ctx)
	if err != nil { return SnapshotResponse{}, err }
	defer tx.Rollback(ctx)
	claim, err := tx.Exec(ctx, `
		UPDATE agents
		SET client_presence_observed_at=$2, last_seen_at=now(), status='online', updated_at=now()
		WHERE id=$1::uuid
		  AND (client_presence_observed_at IS NULL OR client_presence_observed_at <= $2)
	`, agentID, input.ObservedAt)
	if err != nil {
		return SnapshotResponse{}, err
	}
	if claim.RowsAffected() == 0 {
		if err := tx.Commit(ctx); err != nil { return SnapshotResponse{}, err }
		return SnapshotResponse{OK: true, AgentID: agentID, ServerID: serverID, Accepted: 0}, nil
	}

	if _, err := tx.Exec(ctx, `DELETE FROM vpn_account_presence WHERE agent_id=$1::uuid`, agentID); err != nil {
		return SnapshotResponse{}, err
	}
	expiresAt := input.ObservedAt.Add(PresenceTTL)
	for _, item := range input.Items {
		result, err := tx.Exec(ctx, `
			INSERT INTO vpn_account_presence (
				agent_id, server_id, vpn_account_id, protocol, connection_count,
				source, confidence, connected_at, last_activity_at, observed_at, expires_at
			)
			SELECT $1::uuid, $2::uuid, a.id, $4, $5, $6, $7, $8, $9, $10, $11
			FROM vpn_accounts a
			WHERE a.id=$3::uuid AND a.server_id=$2::uuid AND a.status='active'
		`, agentID, serverID, item.VPNAccountID, item.Protocol, item.ConnectionCount,
			item.Source, item.Confidence, item.ConnectedAt, item.LastActivityAt, input.ObservedAt, expiresAt)
		if err != nil { return SnapshotResponse{}, err }
		if result.RowsAffected() == 0 { return SnapshotResponse{}, ErrVPNAccountNotFound }
	}
	if err := tx.Commit(ctx); err != nil { return SnapshotResponse{}, err }
	return SnapshotResponse{OK: true, AgentID: agentID, ServerID: serverID, Accepted: len(input.Items)}, nil
}

func (r *Repository) List(ctx context.Context, now time.Time, limit int) (ListResponse, error) {
	var summary Summary
	if err := r.pool.QueryRow(ctx, `
		WITH live AS (
			SELECT vpn_account_id, connection_count, confidence
			FROM vpn_account_presence
			WHERE expires_at > $1
		), recent AS (
			SELECT DISTINCT e.vpn_account_id
			FROM traffic_usage_events e
			WHERE e.observed_at > $1 - make_interval(secs => $2)
			  AND NOT EXISTS (SELECT 1 FROM live l WHERE l.vpn_account_id=e.vpn_account_id)
		)
		SELECT
			(SELECT COUNT(DISTINCT vpn_account_id)::int FROM live WHERE confidence='exact'),
			(SELECT COALESCE(SUM(connection_count), 0)::int FROM live WHERE confidence='exact'),
			(SELECT COUNT(DISTINCT vpn_account_id)::int FROM (
				SELECT vpn_account_id FROM live WHERE confidence='heuristic'
				UNION ALL SELECT vpn_account_id FROM recent
			) recent_users)
	`, now, int(RecentActivityTTL.Seconds())).Scan(&summary.OnlineUsers, &summary.OnlineConnections, &summary.RecentlyActiveUsers); err != nil {
		return ListResponse{}, err
	}
	rows, err := r.pool.Query(ctx, `
		WITH live AS (
			SELECT p.vpn_account_id, p.server_id, p.agent_id, p.protocol,
			       p.connection_count, p.source, p.confidence, p.connected_at,
			       p.last_activity_at, p.observed_at,
			       CASE p.confidence WHEN 'exact' THEN 'online'::text ELSE 'recently_active'::text END AS state
			FROM vpn_account_presence p
			WHERE p.expires_at > $1
		), recent_traffic AS (
			SELECT DISTINCT ON (e.vpn_account_id)
			       e.vpn_account_id, e.server_id, e.agent_id, a.protocol,
			       1 AS connection_count, 'traffic'::text AS source,
			       'heuristic'::text AS confidence, NULL::timestamptz AS connected_at,
			       e.observed_at AS last_activity_at, e.observed_at, 'recently_active'::text AS state
			FROM traffic_usage_events e
			JOIN vpn_accounts a ON a.id=e.vpn_account_id
			WHERE e.observed_at > $1 - make_interval(secs => $2)
			  AND NOT EXISTS (SELECT 1 FROM live l WHERE l.vpn_account_id=e.vpn_account_id)
			ORDER BY e.vpn_account_id, e.observed_at DESC
		), combined AS (
			SELECT * FROM live UNION ALL SELECT * FROM recent_traffic
		)
		SELECT c.vpn_account_id::text, COALESCE(NULLIF(a.display_name,''), a.username), COALESCE(a.email,''),
		       c.server_id::text, s.name, COALESCE(c.agent_id::text,''), COALESCE(ag.name,''), c.protocol,
		       c.state, c.connection_count, c.source, c.confidence, c.connected_at,
		       c.last_activity_at, c.observed_at
		FROM combined c
		JOIN vpn_accounts a ON a.id=c.vpn_account_id
		JOIN servers s ON s.id=c.server_id
		LEFT JOIN agents ag ON ag.id=c.agent_id
		ORDER BY CASE c.state WHEN 'online' THEN 0 ELSE 1 END, c.observed_at DESC, lower(a.username)
		LIMIT $3
	`, now, int(RecentActivityTTL.Seconds()), limit)
	if err != nil { return ListResponse{}, err }
	defer rows.Close()
	items := make([]Connection, 0)
	for rows.Next() {
		var item Connection
		if err := rows.Scan(&item.VPNAccountID, &item.AccountName, &item.Email, &item.ServerID, &item.ServerName,
			&item.AgentID, &item.AgentName, &item.Protocol, &item.State, &item.ConnectionCount,
			&item.Source, &item.Confidence, &item.ConnectedAt, &item.LastActivityAt, &item.ObservedAt); err != nil {
			return ListResponse{}, err
		}
		items = append(items, item)
	}
	if err := rows.Err(); err != nil { return ListResponse{}, err }
	return ListResponse{GeneratedAt: now, Summary: summary, Items: items}, nil
}
