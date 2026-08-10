package dashboard

import (
	"log/slog"
	"net/http"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/ikaevus/routegate/backend/internal/httpx"
)

const (
	recentActivityLimit = 5
	dashboardServerLimit = 5
	nodeLocationLimit    = 8
)

type Handler struct {
	logger   *slog.Logger
	activity activityReader
	traffic  trafficReader
	nodes    nodeReader
	now      func() time.Time
}

type ActivityResponse struct {
	RecentDeployments []RecentDeployment `json:"recentDeployments"`
	RecentAuditEvents []RecentAuditEvent `json:"recentAuditEvents"`
}

func NewHandler(logger *slog.Logger, pool *pgxpool.Pool) *Handler {
	repository := NewRepository(pool)
	return &Handler{
		logger:   logger,
		activity: repository,
		traffic:  repository,
		nodes:    repository,
		now:      time.Now,
	}
}

func (h *Handler) Activity(w http.ResponseWriter, r *http.Request) {
	deployments, err := h.activity.ListRecentDeployments(r.Context(), recentActivityLimit)
	if err != nil {
		h.writeDatabaseError(w, err, "Failed to load dashboard activity.")
		return
	}

	auditEvents, err := h.activity.ListRecentAuditEvents(r.Context(), recentActivityLimit)
	if err != nil {
		h.writeDatabaseError(w, err, "Failed to load dashboard activity.")
		return
	}

	httpx.WriteJSON(w, http.StatusOK, ActivityResponse{
		RecentDeployments: deployments,
		RecentAuditEvents: auditEvents,
	})
}

func (h *Handler) Traffic(w http.ResponseWriter, r *http.Request) {
	snapshot, err := h.traffic.GetTrafficSnapshot(r.Context(), h.now(), dashboardServerLimit)
	if err != nil {
		h.writeDatabaseError(w, err, "Failed to load dashboard traffic.")
		return
	}

	httpx.WriteJSON(w, http.StatusOK, snapshot)
}

func (h *Handler) Nodes(w http.ResponseWriter, r *http.Request) {
	distribution, err := h.nodes.GetNodeDistribution(r.Context(), nodeLocationLimit, dashboardServerLimit)
	if err != nil {
		h.writeDatabaseError(w, err, "Failed to load dashboard node distribution.")
		return
	}

	httpx.WriteJSON(w, http.StatusOK, distribution)
}

func (h *Handler) writeDatabaseError(w http.ResponseWriter, err error, message string) {
	h.logger.Error("dashboard request failed", "error", err)
	httpx.WriteJSON(w, http.StatusInternalServerError, httpx.Error("database_error", message))
}
