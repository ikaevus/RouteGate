package routingprofiles

import (
	"context"
	"errors"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

type Repository struct {
	pool *pgxpool.Pool
}

func NewRepository(pool *pgxpool.Pool) *Repository {
	return &Repository{pool: pool}
}

func (r *Repository) ListProfiles(ctx context.Context) ([]RoutingProfile, error) {
	rows, err := r.pool.Query(ctx, routingProfileSelect+`
		ORDER BY is_default DESC, name ASC, created_at ASC
	`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	profiles := make([]RoutingProfile, 0)
	for rows.Next() {
		profile, err := scanRoutingProfile(rows)
		if err != nil {
			return nil, err
		}
		profiles = append(profiles, profile)
	}
	return profiles, rows.Err()
}

func (r *Repository) GetProfile(ctx context.Context, id string) (RoutingProfile, error) {
	profile, err := scanRoutingProfile(r.pool.QueryRow(ctx, routingProfileSelect+`
		WHERE id = $1::uuid
	`, id))
	if err != nil {
		return RoutingProfile{}, err
	}

	rules, err := r.ListRules(ctx, id)
	if err != nil {
		return RoutingProfile{}, err
	}
	profile.Rules = rules
	return profile, nil
}

func (r *Repository) CreateProfile(ctx context.Context, input CreateRoutingProfileInput) (RoutingProfile, error) {
	if input.IsDefault {
		return r.createDefaultProfile(ctx, input)
	}

	return scanRoutingProfile(r.pool.QueryRow(ctx, `
		INSERT INTO routing_profiles (name, description, is_default)
		VALUES ($1, NULLIF($2, ''), FALSE)
		RETURNING id::text, name, COALESCE(description, ''), is_default, created_at, updated_at
	`, input.Name, input.Description))
}

func (r *Repository) createDefaultProfile(ctx context.Context, input CreateRoutingProfileInput) (RoutingProfile, error) {
	tx, err := r.pool.Begin(ctx)
	if err != nil {
		return RoutingProfile{}, err
	}
	defer tx.Rollback(ctx)

	if _, err := tx.Exec(ctx, `UPDATE routing_profiles SET is_default = FALSE, updated_at = now()`); err != nil {
		return RoutingProfile{}, err
	}

	profile, err := scanRoutingProfile(tx.QueryRow(ctx, `
		INSERT INTO routing_profiles (name, description, is_default)
		VALUES ($1, NULLIF($2, ''), TRUE)
		RETURNING id::text, name, COALESCE(description, ''), is_default, created_at, updated_at
	`, input.Name, input.Description))
	if err != nil {
		return RoutingProfile{}, err
	}
	if err := tx.Commit(ctx); err != nil {
		return RoutingProfile{}, err
	}
	return profile, nil
}

func (r *Repository) UpdateProfile(ctx context.Context, id string, input UpdateRoutingProfileInput) (RoutingProfile, error) {
	if input.IsDefault != nil && *input.IsDefault {
		return r.updateProfileAsDefault(ctx, id, input)
	}

	return scanRoutingProfile(r.pool.QueryRow(ctx, `
		UPDATE routing_profiles
		SET
			name = CASE WHEN $2 THEN $3 ELSE name END,
			description = CASE WHEN $4 THEN NULLIF($5, '') ELSE description END,
			is_default = CASE WHEN $6 THEN $7 ELSE is_default END,
			updated_at = now()
		WHERE id = $1::uuid
		RETURNING id::text, name, COALESCE(description, ''), is_default, created_at, updated_at
	`,
		id,
		input.Name != nil, stringValue(input.Name),
		input.Description != nil, stringValue(input.Description),
		input.IsDefault != nil, boolValue(input.IsDefault),
	))
}

func (r *Repository) updateProfileAsDefault(ctx context.Context, id string, input UpdateRoutingProfileInput) (RoutingProfile, error) {
	tx, err := r.pool.Begin(ctx)
	if err != nil {
		return RoutingProfile{}, err
	}
	defer tx.Rollback(ctx)

	if _, err := tx.Exec(ctx, `UPDATE routing_profiles SET is_default = FALSE, updated_at = now() WHERE id <> $1::uuid`, id); err != nil {
		return RoutingProfile{}, err
	}

	profile, err := scanRoutingProfile(tx.QueryRow(ctx, `
		UPDATE routing_profiles
		SET
			name = CASE WHEN $2 THEN $3 ELSE name END,
			description = CASE WHEN $4 THEN NULLIF($5, '') ELSE description END,
			is_default = TRUE,
			updated_at = now()
		WHERE id = $1::uuid
		RETURNING id::text, name, COALESCE(description, ''), is_default, created_at, updated_at
	`,
		id,
		input.Name != nil, stringValue(input.Name),
		input.Description != nil, stringValue(input.Description),
	))
	if err != nil {
		return RoutingProfile{}, err
	}
	if err := tx.Commit(ctx); err != nil {
		return RoutingProfile{}, err
	}
	return profile, nil
}

func (r *Repository) DeleteProfile(ctx context.Context, id string) error {
	result, err := r.pool.Exec(ctx, `DELETE FROM routing_profiles WHERE id = $1::uuid AND is_default = FALSE`, id)
	if err != nil {
		return err
	}
	if result.RowsAffected() == 0 {
		if exists, err := r.profileExists(ctx, id); err != nil {
			return err
		} else if exists {
			return ErrDefaultProfileCannotBeDeleted
		}
		return pgx.ErrNoRows
	}
	return nil
}

func (r *Repository) profileExists(ctx context.Context, id string) (bool, error) {
	var exists bool
	err := r.pool.QueryRow(ctx, `SELECT EXISTS (SELECT 1 FROM routing_profiles WHERE id = $1::uuid)`, id).Scan(&exists)
	return exists, err
}

func (r *Repository) ListRules(ctx context.Context, profileID string) ([]RoutingProfileRule, error) {
	rows, err := r.pool.Query(ctx, routingProfileRuleSelect+`
		WHERE routing_profile_id = $1::uuid
		ORDER BY priority ASC, created_at ASC
	`, profileID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	rules := make([]RoutingProfileRule, 0)
	for rows.Next() {
		rule, err := scanRoutingProfileRule(rows)
		if err != nil {
			return nil, err
		}
		rules = append(rules, rule)
	}
	return rules, rows.Err()
}

func (r *Repository) CreateRule(ctx context.Context, input CreateRoutingProfileRuleInput) (RoutingProfileRule, error) {
	return scanRoutingProfileRule(r.pool.QueryRow(ctx, `
		INSERT INTO routing_profile_rules (
			routing_profile_id, name, priority, action, domains, domain_suffixes,
			domain_keywords, ip_cidrs, geo_sites, geo_ips, enabled
		)
		VALUES ($1::uuid, $2, $3, $4, $5::text[], $6::text[], $7::text[], $8::text[], $9::text[], $10::text[], $11)
		RETURNING
			id::text, routing_profile_id::text, name, priority, action, domains,
			domain_suffixes, domain_keywords, ip_cidrs, geo_sites, geo_ips,
			enabled, created_at, updated_at
	`,
		input.RoutingProfileID,
		input.Name,
		input.Priority,
		input.Action,
		input.Domains,
		input.DomainSuffixes,
		input.DomainKeywords,
		input.IPCIDRs,
		input.GeoSites,
		input.GeoIPs,
		input.Enabled,
	))
}

func (r *Repository) UpdateRule(ctx context.Context, profileID string, ruleID string, input UpdateRoutingProfileRuleInput) (RoutingProfileRule, error) {
	return scanRoutingProfileRule(r.pool.QueryRow(ctx, `
		UPDATE routing_profile_rules
		SET
			name = CASE WHEN $3 THEN $4 ELSE name END,
			priority = CASE WHEN $5 THEN $6 ELSE priority END,
			action = CASE WHEN $7 THEN $8 ELSE action END,
			domains = CASE WHEN $9 THEN $10::text[] ELSE domains END,
			domain_suffixes = CASE WHEN $11 THEN $12::text[] ELSE domain_suffixes END,
			domain_keywords = CASE WHEN $13 THEN $14::text[] ELSE domain_keywords END,
			ip_cidrs = CASE WHEN $15 THEN $16::text[] ELSE ip_cidrs END,
			geo_sites = CASE WHEN $17 THEN $18::text[] ELSE geo_sites END,
			geo_ips = CASE WHEN $19 THEN $20::text[] ELSE geo_ips END,
			enabled = CASE WHEN $21 THEN $22 ELSE enabled END,
			updated_at = now()
		WHERE id = $1::uuid AND routing_profile_id = $2::uuid
		RETURNING
			id::text, routing_profile_id::text, name, priority, action, domains,
			domain_suffixes, domain_keywords, ip_cidrs, geo_sites, geo_ips,
			enabled, created_at, updated_at
	`,
		ruleID,
		profileID,
		input.Name != nil, stringValue(input.Name),
		input.Priority != nil, intValue(input.Priority),
		input.Action != nil, stringValue(input.Action),
		input.Domains != nil, stringSliceValue(input.Domains),
		input.DomainSuffixes != nil, stringSliceValue(input.DomainSuffixes),
		input.DomainKeywords != nil, stringSliceValue(input.DomainKeywords),
		input.IPCIDRs != nil, stringSliceValue(input.IPCIDRs),
		input.GeoSites != nil, stringSliceValue(input.GeoSites),
		input.GeoIPs != nil, stringSliceValue(input.GeoIPs),
		input.Enabled != nil, boolValue(input.Enabled),
	))
}

func (r *Repository) DeleteRule(ctx context.Context, profileID string, ruleID string) error {
	result, err := r.pool.Exec(ctx, `DELETE FROM routing_profile_rules WHERE id = $1::uuid AND routing_profile_id = $2::uuid`, ruleID, profileID)
	if err != nil {
		return err
	}
	if result.RowsAffected() == 0 {
		return pgx.ErrNoRows
	}
	return nil
}

var ErrDefaultProfileCannotBeDeleted = errors.New("default routing profile cannot be deleted")

const routingProfileSelect = `
	SELECT id::text, name, COALESCE(description, ''), is_default, created_at, updated_at
	FROM routing_profiles`

const routingProfileRuleSelect = `
	SELECT
		id::text,
		routing_profile_id::text,
		name,
		priority,
		action,
		domains,
		domain_suffixes,
		domain_keywords,
		ip_cidrs,
		geo_sites,
		geo_ips,
		enabled,
		created_at,
		updated_at
	FROM routing_profile_rules`

type scanner interface {
	Scan(dest ...any) error
}

func scanRoutingProfile(row scanner) (RoutingProfile, error) {
	var profile RoutingProfile
	err := row.Scan(
		&profile.ID,
		&profile.Name,
		&profile.Description,
		&profile.IsDefault,
		&profile.CreatedAt,
		&profile.UpdatedAt,
	)
	if err != nil {
		return RoutingProfile{}, err
	}
	return profile, nil
}

func scanRoutingProfileRule(row scanner) (RoutingProfileRule, error) {
	var rule RoutingProfileRule
	err := row.Scan(
		&rule.ID,
		&rule.RoutingProfileID,
		&rule.Name,
		&rule.Priority,
		&rule.Action,
		&rule.Domains,
		&rule.DomainSuffixes,
		&rule.DomainKeywords,
		&rule.IPCIDRs,
		&rule.GeoSites,
		&rule.GeoIPs,
		&rule.Enabled,
		&rule.CreatedAt,
		&rule.UpdatedAt,
	)
	if err != nil {
		return RoutingProfileRule{}, err
	}
	return rule, nil
}

func stringValue(value *string) string {
	if value == nil {
		return ""
	}
	return *value
}

func intValue(value *int) int {
	if value == nil {
		return 0
	}
	return *value
}

func boolValue(value *bool) bool {
	return value != nil && *value
}

func stringSliceValue(value *[]string) []string {
	if value == nil {
		return nil
	}
	return *value
}
