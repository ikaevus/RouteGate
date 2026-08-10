package dashboard

import (
	"log/slog"
	"net/http"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/ikaevus/routegate/backend/internal/httpx"
)

const recentActivityLimit = 5

type Handler struct {
	logger *slog.Logger
	reader activityReader
}

type ActivityResponse struct {
	RecentDeployments []RecentDeployment `json:"recentDeployments"`
	RecentAuditEvents []RecentAuditEvent `json:"recentAuditEvents"`
}

func NewHandler(logger *slog.Logger, pool *pgxpool.Pool) *Handler {
	return &Handler{
		logger: logger,
		reader: NewRepository(pool),
	}
}

func (h *Handler) Activity(w http.ResponseWriter, r *http.Request) {
	deployments, err := h.reader.ListRecentDeployments(r.Context(), recentActivityLimit)
	if err != nil {
		h.writeDatabaseError(w, err)
		return
	}

	auditEvents, err := h.reader.ListRecentAuditEvents(r.Context(), recentActivityLimit)
	if err != nil {
		h.writeDatabaseError(w, err)
		return
	}

	httpx.WriteJSON(w, http.StatusOK, ActivityResponse{
		RecentDeployments: deployments,
		RecentAuditEvents: auditEvents,
	})
}

func (h *Handler) writeDatabaseError(w http.ResponseWriter, err error) {
	h.logger.Error("dashboard activity request failed", "error", err)
	httpx.WriteJSON(w, http.StatusInternalServerError, httpx.Error("database_error", "Failed to load dashboard activity."))
}
