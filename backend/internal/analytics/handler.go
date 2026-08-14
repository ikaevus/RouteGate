package analytics

import (
	"log/slog"
	"net/http"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/ikaevus/routegate/backend/internal/httpx"
)

type Handler struct {
	logger     *slog.Logger
	repository *Repository
}

func NewHandler(logger *slog.Logger, pool *pgxpool.Pool) *Handler {
	return &Handler{logger: logger, repository: NewRepository(pool)}
}

func (h *Handler) Overview(w http.ResponseWriter, r *http.Request) {
	overview, err := h.repository.Overview(r.Context())
	if err != nil {
		h.logger.Error("analytics overview failed", "error", err)
		httpx.WriteJSON(w, http.StatusInternalServerError, httpx.Error("database_error", "Failed to load Analytics overview."))
		return
	}
	w.Header().Set("Cache-Control", "no-store")
	httpx.WriteJSON(w, http.StatusOK, overview)
}
