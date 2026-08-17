package portal

import (
	"context"
	"errors"
	"log/slog"
	"net/http"
	"net/url"
	"strings"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/ikaevus/routegate/backend/internal/auth"
	"github.com/ikaevus/routegate/backend/internal/httpx"
	"github.com/ikaevus/routegate/backend/internal/vpnaccounts"
)

const portalAccessPermission = "portal:access"

type portalRepository interface {
	ListProfilesForUser(context.Context, string) ([]PortalProfile, error)
	GetProfileForUser(context.Context, string, string) (PortalProfile, error)
	CreateSubscriptionToken(context.Context, CreateSubscriptionTokenInput) (PortalSubscriptionToken, error)
}

type portalTrafficRepository interface {
	GetTrafficUsageForUser(context.Context, string) (TrafficUsageSummary, error)
}

type Handler struct {
	logger                    *slog.Logger
	profiles                  portalRepository
	traffic                   portalTrafficRepository
	generateSubscriptionToken func() (string, error)
}

func NewHandler(logger *slog.Logger, pool *pgxpool.Pool) *Handler {
	repository := NewRepository(pool)
	return &Handler{
		logger:                    logger,
		profiles:                  repository,
		traffic:                   repository,
		generateSubscriptionToken: vpnaccounts.GenerateSubscriptionToken,
	}
}

func (h *Handler) Me(w http.ResponseWriter, r *http.Request) {
	user, ok := activePortalUserFromRequest(w, r)
	if !ok {
		return
	}

	httpx.WriteJSON(w, http.StatusOK, MeResponse{User: portalUserFromAuthUser(user)})
}

func (h *Handler) Dashboard(w http.ResponseWriter, r *http.Request) {
	user, ok := activePortalUserFromRequest(w, r)
	if !ok {
		return
	}

	profiles, err := h.profiles.ListProfilesForUser(r.Context(), user.Email)
	if err != nil {
		h.databaseError(w, "list portal profiles", err)
		return
	}

	dashboard := buildDashboard(profiles)
	if h.traffic != nil {
		trafficUsage, trafficErr := h.traffic.GetTrafficUsageForUser(r.Context(), user.Email)
		if trafficErr != nil {
			h.logger.Warn("load portal traffic usage failed", "user_email", user.Email, "error", trafficErr)
		} else {
			dashboard.TrafficUsage = &trafficUsage
		}
	}

	httpx.WriteJSON(w, http.StatusOK, DashboardResponse{Dashboard: dashboard})
}

func (h *Handler) ListProfiles(w http.ResponseWriter, r *http.Request) {
	user, ok := activePortalUserFromRequest(w, r)
	if !ok {
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

	locale := requestLocale(r)
	message := localizedSubscriptionWarning(locale)
	requiresTokenRotation := profile.AccessStatus == AccessStatusActive
	if profile.AccessStatus != AccessStatusActive {
		message = localizedSubscriptionInactive(locale)
	}

	response := PortalSubscription{
		ProfileID:             profile.ID,
		Available:             false,
		AccessStatus:          profile.AccessStatus,
		Format:                PortalSubscriptionFormat,
		ExpiresAt:             profile.ExpiresAt,
		RequiresTokenRotation: requiresTokenRotation,
		Message:               message,
	}

	httpx.WriteJSON(w, http.StatusOK, SubscriptionResponse{Subscription: response})
}

func (h *Handler) GenerateSubscriptionAccess(w http.ResponseWriter, r *http.Request) {
	profile, ok := h.profileForUser(w, r)
	if !ok {
		return
	}
	if profile.AccessStatus != AccessStatusActive {
		httpx.WriteJSON(w, http.StatusForbidden, httpx.Error("portal_subscription_unavailable", "Subscription self-service is available only for active VPN profiles."))
		return
	}

	rawToken, err := h.generateSubscriptionToken()
	if err != nil {
		h.logger.Error("generate portal subscription token failed", "profile_id", profile.ID, "error", err)
		httpx.WriteJSON(w, http.StatusInternalServerError, httpx.Error("token_generation_failed", "Failed to generate subscription token."))
		return
	}

	created, err := h.profiles.CreateSubscriptionToken(r.Context(), CreateSubscriptionTokenInput{
		VPNAccountID: profile.ID,
		TokenHash:    vpnaccounts.HashSubscriptionToken(rawToken),
		ExpiresAt:    profile.ExpiresAt,
	})
	if err != nil {
		h.databaseError(w, "create portal subscription token", err)
		return
	}

	subscriptionURL := portalSubscriptionURL(r, rawToken)
	locale := requestLocale(r)
	response := SubscriptionAccessResponse{
		Subscription: PortalSubscription{
			ProfileID:             profile.ID,
			Available:             true,
			AccessStatus:          profile.AccessStatus,
			SubscriptionURL:       subscriptionURL,
			Format:                PortalSubscriptionFormat,
			ExpiresAt:             created.ExpiresAt,
			RequiresTokenRotation: false,
			Message:               localizedSubscriptionGenerated(locale),
		},
		QR: PortalQRCode{
			ProfileID:    profile.ID,
			Available:    true,
			AccessStatus: profile.AccessStatus,
			QRText:       subscriptionURL,
			Format:       PortalQRFormat,
		},
	}

	httpx.WriteJSON(w, http.StatusCreated, response)
}

func (h *Handler) GetQRCode(w http.ResponseWriter, r *http.Request) {
	profile, ok := h.profileForUser(w, r)
	if !ok {
		return
	}

	locale := requestLocale(r)
	message := localizedQRCodeWarning(locale)
	if profile.AccessStatus != AccessStatusActive {
		message = localizedQRCodeInactive(locale)
	}

	response := PortalQRCode{
		ProfileID:    profile.ID,
		Available:    false,
		AccessStatus: profile.AccessStatus,
		Format:       PortalQRFormat,
		Message:      message,
	}

	httpx.WriteJSON(w, http.StatusOK, QRCodeResponse{QR: response})
}

func (h *Handler) ListInstructions(w http.ResponseWriter, r *http.Request) {
	if _, ok := activePortalUserFromRequest(w, r); !ok {
		return
	}

	locale := requestLocale(r)
	httpx.WriteJSON(w, http.StatusOK, InstructionsResponse{Items: localizedInstructionPlatforms(locale)})
}

func (h *Handler) GetInstruction(w http.ResponseWriter, r *http.Request) {
	if _, ok := activePortalUserFromRequest(w, r); !ok {
		return
	}

	platform := strings.ToLower(strings.TrimSpace(r.PathValue("platform")))
	locale := requestLocale(r)
	instruction, ok := localizedInstruction(locale, platform)
	if !ok {
		httpx.WriteJSON(w, http.StatusNotFound, httpx.Error("instruction_not_found", "Device setup instructions not found."))
		return
	}

	httpx.WriteJSON(w, http.StatusOK, InstructionResponse{Instruction: instruction})
}

func (h *Handler) profileForUser(w http.ResponseWriter, r *http.Request) (PortalProfile, bool) {
	user, ok := activePortalUserFromRequest(w, r)
	if !ok {
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

func activePortalUserFromRequest(w http.ResponseWriter, r *http.Request) (auth.AuthenticatedUser, bool) {
	user, ok := auth.UserFromContext(r.Context())
	if !ok {
		writeUnauthorized(w)
		return auth.AuthenticatedUser{}, false
	}
	if user.Status != "active" {
		httpx.WriteJSON(w, http.StatusForbidden, httpx.Error("portal_user_inactive", "Portal access is available only for active users."))
		return auth.AuthenticatedUser{}, false
	}
	if !hasPermission(user, portalAccessPermission) {
		httpx.WriteJSON(w, http.StatusForbidden, httpx.Error("forbidden", "Required permission is missing."))
		return auth.AuthenticatedUser{}, false
	}

	return user, true
}

func hasPermission(user auth.AuthenticatedUser, permission string) bool {
	for _, p := range user.Permissions {
		if p == permission {
			return true
		}
	}
	return false
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

func portalSubscriptionURL(r *http.Request, token string) string {
	return (&url.URL{
		Scheme: portalSubscriptionScheme(r),
		Host:   portalSubscriptionHost(r),
		Path:   "/api/v1/subscriptions/" + token,
	}).String()
}

func portalSubscriptionScheme(r *http.Request) string {
	scheme := strings.ToLower(firstForwardedValue(r.Header.Get("X-Forwarded-Proto")))
	if scheme == "https" || scheme == "http" {
		return scheme
	}
	if r.TLS != nil {
		return "https"
	}
	return "http"
}

func portalSubscriptionHost(r *http.Request) string {
	if host := firstForwardedValue(r.Header.Get("X-Forwarded-Host")); validURLHost(host) {
		return host
	}
	if host := strings.TrimSpace(r.Host); validURLHost(host) {
		return host
	}
	return "localhost"
}

func firstForwardedValue(value string) string {
	value = strings.TrimSpace(value)
	if comma := strings.Index(value, ","); comma >= 0 {
		value = value[:comma]
	}
	return strings.TrimSpace(value)
}

func validURLHost(host string) bool {
	if host == "" || strings.ContainsAny(host, "/\\@") {
		return false
	}
	for _, r := range host {
		if r <= 31 || r == 127 {
			return false
		}
	}
	parsed, err := url.Parse("//" + host)
	return err == nil && parsed.Host == host && parsed.Hostname() != "" && parsed.Path == ""
}

func writeUnauthorized(w http.ResponseWriter) {
	httpx.WriteJSON(w, http.StatusUnauthorized, httpx.Error("unauthorized", "Authentication is required."))
}

func (h *Handler) databaseError(w http.ResponseWriter, operation string, err error) {
	h.logger.Error(operation+" failed", "error", err)
	httpx.WriteJSON(w, http.StatusInternalServerError, httpx.Error("database_error", "Database operation failed."))
}
