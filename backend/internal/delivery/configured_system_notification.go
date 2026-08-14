package delivery

import (
	"log/slog"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/ikaevus/routegate/backend/internal/audit"
)

func NewConfiguredSystemNotificationCreator(logger *slog.Logger, pool *pgxpool.Pool) *SystemNotificationCreator {
	return NewSystemNotificationCreator(
		NewService(NewRepository(pool), audit.NewRecorder(logger, pool)),
	)
}
