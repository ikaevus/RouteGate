package portal

import (
	"errors"
	"log/slog"
	"net/http"
	"strings"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/ikaevus/routegate/backend/internal/auth"
	"github.com/ikaevus/routegate/backend/internal/httpx"
)

type Handler struct {
	logger   *slog.Logger
	profiles *Repository
}

func NewHandler(logger *slog.Logger, pool *pgxpool.Pool) *Handler {
	return &Handler{
		logger:   logger,
		profiles: NewRepository(pool),
	}
}

func (h *Handler) Me(w http.ResponseWriter, r *http.Request) {
	user, ok := auth.UserFromContext(r.Context())
	if !ok {
		writeUnauthorized(w)
		return
	}

	httpx.WriteJSON(w, http.StatusOK, MeResponse{User: portalUserFromAuthUser(user)})
}

func (h *Handler) Dashboard(w http.ResponseWriter, r *http.Request) {
	user, ok := auth.UserFromContext(r.Context())
	if !ok {
		writeUnauthorized(w)
		return
	}

	profiles, err := h.profiles.ListProfilesForUser(r.Context(), user.Email)
	if err != nil {
		h.databaseError(w, "list portal profiles", err)
		return
	}

	httpx.WriteJSON(w, http.StatusOK, DashboardResponse{Dashboard: buildDashboard(profiles)})
}

func (h *Handler) ListProfiles(w http.ResponseWriter, r *http.Request) {
	user, ok := auth.UserFromContext(r.Context())
	if !ok {
		writeUnauthorized(w)
		return
	}

	profiles, err := h.profiles.ListProfilesForUser(r.Context(), user.Email)
	if err != nil {
		h.databaseError(w, "list portal profiles", err)
		return
	}

	httpx.WriteJSON(w, http.StatusOK, ProfilesResponse{Items: profiles})
}

func (h *Handler) GetProfile(w http.ResponseWriter, r *http.Request) {
	profile, ok := h.profileForUser(w, r)
	if !ok {
		return
	}

	httpx.WriteJSON(w, http.StatusOK, ProfileResponse{Profile: profile})
}

func (h *Handler) GetSubscription(w http.ResponseWriter, r *http.Request) {
	profile, ok := h.profileForUser(w, r)
	if !ok {
		return
	}

	response := PortalSubscription{
		ProfileID:             profile.ID,
		Available:             false,
		Format:                "routegate.subscription.v1",
		ExpiresAt:             profile.ExpiresAt,
		RequiresTokenRotation: true,
		Message:               "Self-service subscription link retrieval is not enabled yet. Ask an administrator to issue or rotate the subscription token.",
	}

	httpx.WriteJSON(w, http.StatusOK, SubscriptionResponse{Subscription: response})
}

func (h *Handler) GetQRCode(w http.ResponseWriter, r *http.Request) {
	profile, ok := h.profileForUser(w, r)
	if !ok {
		return
	}

	response := PortalQRCode{
		ProfileID: profile.ID,
		Available: false,
		Format:    "subscription-url",
		Message:   "QR rendering requires an available user-facing subscription link. Self-service subscription link retrieval is not enabled yet.",
	}

	httpx.WriteJSON(w, http.StatusOK, QRCodeResponse{QR: response})
}

func (h *Handler) ListInstructions(w http.ResponseWriter, r *http.Request) {
	httpx.WriteJSON(w, http.StatusOK, InstructionsResponse{Items: instructionPlatforms})
}

func (h *Handler) GetInstruction(w http.ResponseWriter, r *http.Request) {
	platform := strings.ToLower(strings.TrimSpace(r.PathValue("platform")))
	instruction, ok := instructionsByPlatform[platform]
	if !ok {
		httpx.WriteJSON(w, http.StatusNotFound, httpx.Error("instruction_not_found", "Device setup instructions not found."))
		return
	}

	httpx.WriteJSON(w, http.StatusOK, InstructionResponse{Instruction: instruction})
}

func (h *Handler) profileForUser(w http.ResponseWriter, r *http.Request) (PortalProfile, bool) {
	user, ok := auth.UserFromContext(r.Context())
	if !ok {
		writeUnauthorized(w)
		return PortalProfile{}, false
	}

	profile, err := h.profiles.GetProfileForUser(r.Context(), user.Email, r.PathValue("id"))
	if errors.Is(err, pgx.ErrNoRows) {
		httpx.WriteJSON(w, http.StatusNotFound, httpx.Error("portal_profile_not_found", "VPN profile not found."))
		return PortalProfile{}, false
	}
	if err != nil {
		h.databaseError(w, "get portal profile", err)
		return PortalProfile{}, false
	}

	return profile, true
}

func buildDashboard(profiles []PortalProfile) PortalDashboard {
	dashboard := PortalDashboard{
		AccessStatus:  AccessStatusNoAccess,
		ProfilesTotal: len(profiles),
		Notices:       []PortalNotice{},
	}

	for _, profile := range profiles {
		switch profile.AccessStatus {
		case AccessStatusActive:
			dashboard.ProfilesActive++
			dashboard.AccessStatus = AccessStatusActive
		case AccessStatusExpired:
			if dashboard.AccessStatus == AccessStatusNoAccess {
				dashboard.AccessStatus = AccessStatusExpired
			}
		case AccessStatusSuspended:
			if dashboard.AccessStatus == AccessStatusNoAccess || dashboard.AccessStatus == AccessStatusExpired {
				dashboard.AccessStatus = AccessStatusSuspended
			}
		case AccessStatusPending:
			if dashboard.AccessStatus == AccessStatusNoAccess {
				dashboard.AccessStatus = AccessStatusPending
			}
		}

		if profile.ExpiresAt != nil && (dashboard.NearestExpiration == nil || profile.ExpiresAt.Before(*dashboard.NearestExpiration)) {
			dashboard.NearestExpiration = profile.ExpiresAt
		}
	}

	if dashboard.ProfilesTotal == 0 {
		dashboard.Notices = append(dashboard.Notices, PortalNotice{
			Type:    "info",
			Message: "No VPN profiles are available for this user yet.",
		})
	}

	return dashboard
}

func writeUnauthorized(w http.ResponseWriter) {
	httpx.WriteJSON(w, http.StatusUnauthorized, httpx.Error("unauthorized", "Authentication is required."))
}

func (h *Handler) databaseError(w http.ResponseWriter, operation string, err error) {
	h.logger.Error(operation+" failed", "error", err)
	httpx.WriteJSON(w, http.StatusInternalServerError, httpx.Error("database_error", "Database operation failed."))
}
