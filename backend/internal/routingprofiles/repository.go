package routingprofiles

import (
	"errors"
	"strings"

	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"
)

type Repository struct {
	pool *pgxpool.Pool
}

func NewRepository(pool *pgxpool.Pool) *Repository {
	return &Repository{pool: pool}
}

var (
	ErrDefaultProfileCannotBeDeleted   = errors.New("default routing profile cannot be deleted")
	ErrRoutingProfileAssigned          = errors.New("routing profile is assigned to a server")
	ErrRoutingProfileNameAlreadyExists = errors.New("routing profile name already exists")
)

const routingProfileNameUniqueIndex = "routing_profiles_name_ci_unique"

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

func mapProfileWriteError(err error) error {
	if isUniqueViolation(err, routingProfileNameUniqueIndex) {
		return ErrRoutingProfileNameAlreadyExists
	}
	return err
}

func isUniqueViolation(err error, constraintName string) bool {
	var pgErr *pgconn.PgError
	if !errors.As(err, &pgErr) || pgErr.Code != "23505" {
		return false
	}
	return pgErr.ConstraintName == constraintName || strings.Contains(pgErr.ConstraintName, constraintName)
}
