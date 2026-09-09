package vpnaccounts

import (
	"errors"
	"net/http"
	"strings"

	"github.com/jackc/pgx/v5"

	"github.com/ikaevus/routegate/backend/internal/httpx"
)

type clientRoutingImportResponse struct {
	ClientType string `json:"clientType"`
	DeepLink   string `json:"deepLink"`
}

// GetClientRoutingImport prepares a client-native routing import artifact for
// the authenticated admin UI. This deliberately avoids fetching the public
// RG-115 bearer URL from browser JavaScript, which may be on another origin.
// Routing Profiles remain the single policy source used by the native adapter.
func (h *Handler) GetClientRoutingImport(w http.ResponseWriter, r *http.Request) {
	accountID := strings.TrimSpace(r.PathValue("id"))
	clientType := normalizeClientType(r.URL.Query().Get("client"))
	if clientType != ClientTypeV2RayTun {
		writeInvalidRequest(w, "Only v2raytun routing import is currently supported by this endpoint.")
		return
	}

	if _, err := h.accounts.GetAccountByID(r.Context(), accountID); errors.Is(err, pgx.ErrNoRows) {
		writeAccountNotFound(w)
		return
	} else if err != nil {
		h.databaseError(w, "get vpn account for client routing import", err)
		return
	}

	profile, err := h.accounts.GetSubscriptionProfileByAccountID(r.Context(), accountID)
	if errors.Is(err, pgx.ErrNoRows) {
		writeAccountNotFound(w)
		return
	}
	if err != nil {
		h.databaseError(w, "get subscription profile for client routing import", err)
		return
	}

	deepLink, ok, err := renderV2RayTunRoutingDeepLink(profile.RoutingProfile)
	if err != nil {
		h.logger.Warn("render V2RayTun routing import failed", "vpn_account_id", accountID, "error", err)
		httpx.WriteJSON(w, http.StatusServiceUnavailable, httpx.Error("client_routing_unavailable", "Client routing data is temporarily unavailable."))
		return
	}
	if !ok {
		httpx.WriteJSON(w, http.StatusConflict, httpx.Error("client_routing_unavailable", "No effective RouteGate routing rules are available for this VPN account."))
		return
	}

	httpx.WriteJSON(w, http.StatusOK, clientRoutingImportResponse{
		ClientType: ClientTypeV2RayTun,
		DeepLink:   deepLink,
	})
}
