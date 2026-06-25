package routingprofiles

import (
	"context"

	"github.com/jackc/pgx/v5"
)

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

	profile, err := scanRoutingProfile(r.pool.QueryRow(ctx, `
		INSERT INTO routing_profiles (name, description, is_default)
		VALUES ($1, NULLIF($2, ''), FALSE)
		RETURNING id::text, name, COALESCE(description, ''), is_default, created_at, updated_at
	`, input.Name, input.Description))
	if err != nil {
		return RoutingProfile{}, mapProfileWriteError(err)
	}
	return profile, nil
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
		return RoutingProfile{}, mapProfileWriteError(err)
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

	profile, err := scanRoutingProfile(r.pool.QueryRow(ctx, `
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
	if err != nil {
		return RoutingProfile{}, mapProfileWriteError(err)
	}
	return profile, nil
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
		return RoutingProfile{}, mapProfileWriteError(err)
	}
	if err := tx.Commit(ctx); err != nil {
		return RoutingProfile{}, err
	}
	return profile, nil
}

func (r *Repository) DeleteProfile(ctx context.Context, id string) error {
	assigned, err := r.profileAssigned(ctx, id)
	if err != nil {
		return err
	}
	if assigned {
		return ErrRoutingProfileAssigned
	}

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

func (r *Repository) profileAssigned(ctx context.Context, id string) (bool, error) {
	var assigned bool
	err := r.pool.QueryRow(ctx, `SELECT EXISTS (SELECT 1 FROM server_routing_profiles WHERE routing_profile_id = $1::uuid)`, id).Scan(&assigned)
	return assigned, err
}
