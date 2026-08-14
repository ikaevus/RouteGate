package agents

import "github.com/jackc/pgx/v5/pgxpool"

func NewTelemetryStore(pool *pgxpool.Pool) *telemetryStore {
	return newTelemetryStore(pool)
}
