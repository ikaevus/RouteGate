package routingprofiles

import (
	"context"

	"github.com/jackc/pgx/v5"
)

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
