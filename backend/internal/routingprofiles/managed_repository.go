package routingprofiles

import (
	"context"
	"encoding/json"

	"github.com/jackc/pgx/v5"
)

type managedRuleSetRecord struct {
	ManagedRuleSet
	Snapshot json.RawMessage
}

const managedRuleSetSelect = `
	SELECT id::text, routing_profile_id::text, name, provider, source_url,
		source_format, priority, action, enabled, refresh_interval_hours,
		status, COALESCE(last_error, ''),
		COALESCE(jsonb_array_length(snapshot->'rules'), 0),
		last_refresh_at, last_success_at, created_at, updated_at, snapshot
	FROM managed_routing_rule_sets`

func (r *Repository) ListManagedRuleSets(ctx context.Context, profileID string) ([]ManagedRuleSet, error) {
	rows, err := r.pool.Query(ctx, managedRuleSetSelect+`
		WHERE (NULLIF($1, '') IS NULL OR routing_profile_id = NULLIF($1, '')::uuid)
		ORDER BY priority ASC, name ASC, id ASC
	`, profileID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	items := make([]ManagedRuleSet, 0)
	for rows.Next() {
		record, scanErr := scanManagedRuleSet(rows)
		if scanErr != nil {
			return nil, scanErr
		}
		items = append(items, record.ManagedRuleSet)
	}
	return items, rows.Err()
}

func (r *Repository) GetManagedRuleSet(ctx context.Context, id string) (managedRuleSetRecord, error) {
	return scanManagedRuleSet(r.pool.QueryRow(ctx, managedRuleSetSelect+` WHERE id = $1::uuid`, id))
}

func (r *Repository) GetPublicManagedRuleSetSnapshot(ctx context.Context, id string) (json.RawMessage, error) {
	var snapshot json.RawMessage
	err := r.pool.QueryRow(ctx, `SELECT snapshot FROM managed_routing_rule_sets WHERE id = $1::uuid AND enabled = TRUE AND last_success_at IS NOT NULL`, id).Scan(&snapshot)
	return snapshot, err
}

func (r *Repository) CreateManagedRuleSet(ctx context.Context, profileID string, request CreateManagedRuleSetRequest) (ManagedRuleSet, error) {
	record, err := scanManagedRuleSet(r.pool.QueryRow(ctx, `
		INSERT INTO managed_routing_rule_sets
			(routing_profile_id, name, provider, source_url, priority, action, enabled, refresh_interval_hours)
		VALUES ($1::uuid, $2, $3, $4, $5, $6, $7, $8)
		RETURNING id::text, routing_profile_id::text, name, provider, source_url,
			source_format, priority, action, enabled, refresh_interval_hours,
			status, COALESCE(last_error, ''), 0, last_refresh_at,
			last_success_at, created_at, updated_at, snapshot
	`, profileID, request.Name, request.Provider, request.SourceURL, request.Priority,
		request.Action, request.Enabled, request.RefreshIntervalHours))
	if err != nil {
		return ManagedRuleSet{}, mapRuleWriteError(err)
	}
	return record.ManagedRuleSet, nil
}

func (r *Repository) UpdateManagedRuleSet(ctx context.Context, id string, request UpdateManagedRuleSetRequest) (ManagedRuleSet, error) {
	record, err := scanManagedRuleSet(r.pool.QueryRow(ctx, `
		UPDATE managed_routing_rule_sets SET
			name = COALESCE($2, name), provider = COALESCE($3, provider),
			source_url = COALESCE($4, source_url), priority = COALESCE($5, priority),
			action = COALESCE($6, action), enabled = COALESCE($7, enabled),
			refresh_interval_hours = COALESCE($8, refresh_interval_hours), updated_at = now()
		WHERE id = $1::uuid
		RETURNING id::text, routing_profile_id::text, name, provider, source_url,
			source_format, priority, action, enabled, refresh_interval_hours,
			status, COALESCE(last_error, ''), COALESCE(jsonb_array_length(snapshot->'rules'), 0),
			last_refresh_at, last_success_at, created_at, updated_at, snapshot
	`, id, request.Name, request.Provider, request.SourceURL, request.Priority,
		request.Action, request.Enabled, request.RefreshIntervalHours))
	if err != nil {
		return ManagedRuleSet{}, mapRuleWriteError(err)
	}
	return record.ManagedRuleSet, nil
}

func (r *Repository) DeleteManagedRuleSet(ctx context.Context, id string) error {
	result, err := r.pool.Exec(ctx, `DELETE FROM managed_routing_rule_sets WHERE id = $1::uuid`, id)
	if err != nil {
		return err
	}
	if result.RowsAffected() == 0 {
		return pgx.ErrNoRows
	}
	return nil
}

func (r *Repository) MarkManagedRuleSetRefreshFailed(ctx context.Context, id, message string) (ManagedRuleSet, error) {
	record, err := scanManagedRuleSet(r.pool.QueryRow(ctx, `
		UPDATE managed_routing_rule_sets SET status = 'error', last_error = $2,
			last_refresh_at = now(), updated_at = now() WHERE id = $1::uuid
		RETURNING id::text, routing_profile_id::text, name, provider, source_url,
			source_format, priority, action, enabled, refresh_interval_hours,
			status, COALESCE(last_error, ''), COALESCE(jsonb_array_length(snapshot->'rules'), 0),
			last_refresh_at, last_success_at, created_at, updated_at, snapshot
	`, id, message))
	if err != nil {
		return ManagedRuleSet{}, err
	}
	return record.ManagedRuleSet, nil
}

func (r *Repository) MarkManagedRuleSetRefreshSucceeded(ctx context.Context, id string, snapshot json.RawMessage) (ManagedRuleSet, error) {
	record, err := scanManagedRuleSet(r.pool.QueryRow(ctx, `
		UPDATE managed_routing_rule_sets SET snapshot = $2::jsonb, status = 'healthy',
			last_error = NULL, last_refresh_at = now(), last_success_at = now(), updated_at = now()
		WHERE id = $1::uuid
		RETURNING id::text, routing_profile_id::text, name, provider, source_url,
			source_format, priority, action, enabled, refresh_interval_hours,
			status, COALESCE(last_error, ''), COALESCE(jsonb_array_length(snapshot->'rules'), 0),
			last_refresh_at, last_success_at, created_at, updated_at, snapshot
	`, id, snapshot))
	if err != nil {
		return ManagedRuleSet{}, err
	}
	return record.ManagedRuleSet, nil
}

func (r *Repository) ListManagedRuleSetsDue(ctx context.Context, limit int) ([]managedRuleSetRecord, error) {
	rows, err := r.pool.Query(ctx, managedRuleSetSelect+`
		WHERE enabled = TRUE AND (last_refresh_at IS NULL OR last_refresh_at <= now() - make_interval(hours => refresh_interval_hours))
		ORDER BY last_refresh_at NULLS FIRST, id ASC LIMIT $1
	`, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	items := make([]managedRuleSetRecord, 0)
	for rows.Next() {
		item, scanErr := scanManagedRuleSet(rows)
		if scanErr != nil {
			return nil, scanErr
		}
		items = append(items, item)
	}
	return items, rows.Err()
}

func (r *Repository) ListManagedRuleSetRecordsForProfile(ctx context.Context, profileID string) ([]managedRuleSetRecord, error) {
	rows, err := r.pool.Query(ctx, managedRuleSetSelect+` WHERE routing_profile_id = $1::uuid AND enabled = TRUE ORDER BY priority, id`, profileID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	items := make([]managedRuleSetRecord, 0)
	for rows.Next() {
		item, scanErr := scanManagedRuleSet(rows)
		if scanErr != nil {
			return nil, scanErr
		}
		items = append(items, item)
	}
	return items, rows.Err()
}

func scanManagedRuleSet(row scanner) (managedRuleSetRecord, error) {
	var record managedRuleSetRecord
	err := row.Scan(&record.ID, &record.RoutingProfileID, &record.Name, &record.Provider,
		&record.SourceURL, &record.SourceFormat, &record.Priority, &record.Action, &record.Enabled,
		&record.RefreshIntervalHours, &record.Status, &record.LastError, &record.RuleCount,
		&record.LastRefreshAt, &record.LastSuccessfulAt, &record.CreatedAt, &record.UpdatedAt, &record.Snapshot)
	return record, err
}
