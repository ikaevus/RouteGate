package geoip

import (
	"errors"
	"log/slog"
	"net/http"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/ikaevus/routegate/backend/internal/audit"
	"github.com/ikaevus/routegate/backend/internal/auth"
	"github.com/ikaevus/routegate/backend/internal/httpx"
	"github.com/ikaevus/routegate/backend/internal/servers"
)

type Handler struct {
	logger   *slog.Logger
	detector *Detector
	audit    *audit.Recorder
	enabled  bool
}

func NewHandler(logger *slog.Logger, pool *pgxpool.Pool, enabled bool) *Handler {
	repository := servers.NewRepository(pool)
	return &Handler{
		logger:   logger,
		detector: NewDetector(repository, NewIPWhoisResolver(nil)),
		audit:    audit.NewRecorder(logger, pool),
		enabled:  enabled,
	}
}

func (h *Handler) AutoDetect(w http.ResponseWriter, r *http.Request) {
	if !h.enabled {
		httpx.WriteJSON(w, http.StatusConflict, httpx.Error("geoip_disabled", "Automatic GeoIP detection is disabled."))
		return
	}

	server, err := h.detector.Detect(r.Context(), r.PathValue("server_id"))
	switch {
	case errors.Is(err, pgx.ErrNoRows):
		httpx.WriteJSON(w, http.StatusNotFound, httpx.Error("server_not_found", "Server not found."))
		return
	case errors.Is(err, ErrPublicIPRequired), errors.Is(err, ErrNotPublicIP):
		httpx.WriteJSON(w, http.StatusBadRequest, httpx.Error("public_ip_required", "A public routable server IP is required for automatic geolocation."))
		return
	case errors.Is(err, ErrRateLimited):
		httpx.WriteJSON(w, http.StatusServiceUnavailable, httpx.Error("geoip_rate_limited", "GeoIP provider rate limit was reached. Try again later."))
		return
	case err != nil:
		h.logger.Warn("automatic server geolocation request failed", "server_id", r.PathValue("server_id"), "error", err)
		httpx.WriteJSON(w, http.StatusBadGateway, httpx.Error("geoip_lookup_failed", "Automatic server geolocation failed."))
		return
	}

	input := audit.EventInput{
		Action:       "server.geography.auto_detected",
		ResourceType: "server",
		ResourceID:   server.ID,
		Result:       audit.ResultSuccess,
		Metadata: map[string]any{
			"country":         server.LocationCountry,
			"region":          server.LocationRegion,
			"city":            server.LocationCity,
			"location_source": server.LocationSource,
			"has_coordinates": server.LocationLatitude != nil && server.LocationLongitude != nil,
		},
	}
	if user, ok := auth.UserFromContext(r.Context()); ok {
		input.ActorUserID = user.ID
		input.ActorType = audit.ActorTypeUser
	} else {
		input.ActorType = audit.ActorTypeSystem
	}
	h.audit.RecordSafe(r.Context(), input)

	httpx.WriteJSON(w, http.StatusOK, server)
}
