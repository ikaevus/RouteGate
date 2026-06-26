package audit

import (
	"context"
	"encoding/json"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
)

type Repository struct {
	pool *pgxpool.Pool
}

type scanner interface {
	Scan(dest ...any) error
}

func NewRepository(pool *pgxpool.Pool) *Repository {
	return &Repository{pool: pool}
}

func (r *Repository) Create(ctx context.Context, input EventInput) (Event, error) {
	metadata := SanitizeMetadata(input.Metadata)
	metadataJSON, err := json.Marshal(metadata)
	if err != nil {
		return Event{}, err
	}

	return scanEvent(r.pool.QueryRow(
		ctx,
		`
		INSERT INTO audit_events (
			actor_user_id,
			actor_type,
			action,
			resource_type,
			resource_id,
			result,
			metadata
		)
		VALUES (
			NULLIF($1, '')::uuid,
			$2,
			$3,
			$4,
			NULLIF($5, '')::uuid,
			$6,
			$7::jsonb
		)
		RETURNING
			id::text,
			COALESCE(actor_user_id::text, ''),
			actor_type,
			action,
			resource_type,
			COALESCE(resource_id::text, ''),
			result,
			metadata,
			created_at
		`,
		input.ActorUserID,
		normalizeActorType(input.ActorType),
		input.Action,
		input.ResourceType,
		input.ResourceID,
		normalizeResult(input.Result),
		string(metadataJSON),
	))
}

func scanEvent(row scanner) (Event, error) {
	var event Event
	var metadataJSON []byte
	var createdAt time.Time

	err := row.Scan(
		&event.ID,
		&event.ActorUserID,
		&event.ActorType,
		&event.Action,
		&event.ResourceType,
		&event.ResourceID,
		&event.Result,
		&metadataJSON,
		&createdAt,
	)
	if err != nil {
		return Event{}, err
	}

	metadata := map[string]any{}
	if len(metadataJSON) > 0 {
		if err := json.Unmarshal(metadataJSON, &metadata); err != nil {
			return Event{}, err
		}
	}
	event.Metadata = metadata
	event.CreatedAt = createdAt.UTC()

	return event, nil
}

func normalizeActorType(value string) string {
	switch value {
	case ActorTypeUser, ActorTypeAgent, ActorTypeSystem, ActorTypeAnonymous:
		return value
	default:
		return ActorTypeSystem
	}
}

func normalizeResult(value string) string {
	switch value {
	case ResultSuccess, ResultFailure:
		return value
	default:
		return ResultSuccess
	}
}
