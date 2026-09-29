package vpnaccounts

import (
	"errors"
	"net/http"

	"github.com/jackc/pgx/v5"

	"github.com/ikaevus/routegate/backend/internal/httpx"
)

// Client connection states reported next to the saved profile.
const (
	ClientConnectionStatusReady              = "ready"
	ClientConnectionStatusAwaitingApply      = "awaiting_apply"
	ClientConnectionStatusAwaitingFirstApply = "awaiting_first_apply"
	ClientConnectionStatusUnassigned         = "unassigned"
	ClientConnectionStatusUnavailable        = "unavailable"
)

// ClientProfileStateResponse lets administrators read and edit an account's
// saved protocol preferences whether or not client access can be issued yet.
// It never carries client links or credentials: those stay behind
// GET /client-connection and token subscriptions, which refuse access the
// node has not been given.
type ClientProfileStateResponse struct {
	VPNAccountID string `json:"vpnAccountId"`
	// Profile.EnabledProtocols is the saved desired set; Profile.ActiveProtocols
	// is the set the node actually serves for the account right now.
	Profile ClientProfile `json:"profile"`
	// ActiveProtocol is the primary protocol clients get once access is ready.
	ActiveProtocol string `json:"activeProtocol"`
	// ConnectionStatus is one of ready, awaiting_apply, awaiting_first_apply,
	// unassigned or unavailable; ConnectionMessage names the next action.
	ConnectionStatus  string `json:"connectionStatus"`
	ConnectionMessage string `json:"connectionMessage,omitempty"`
}

func (h *Handler) GetClientProfile(w http.ResponseWriter, r *http.Request) {
	accountID := r.PathValue("id")
	subscription, err := h.accounts.GetSubscriptionProfileByAccountID(r.Context(), accountID)
	if errors.Is(err, pgx.ErrNoRows) {
		writeAccountNotFound(w)
		return
	}
	if err != nil {
		h.databaseError(w, "get vpn subscription profile for client profile", err)
		return
	}
	repository, ok := h.accounts.(clientProfileRepository)
	if !ok {
		httpx.WriteJSON(w, http.StatusInternalServerError, httpx.Error("client_profile_unavailable", "Client profile storage is unavailable."))
		return
	}
	profile, err := repository.GetOrCreateClientProfile(r.Context(), accountID)
	if errors.Is(err, pgx.ErrNoRows) {
		writeAccountNotFound(w)
		return
	}
	if err != nil {
		h.databaseError(w, "get vpn client profile", err)
		return
	}
	if err := hydrateClientProtocolSets(r.Context(), h.accounts, accountID, &profile); err != nil {
		h.databaseError(w, "load vpn client protocol sets", err)
		return
	}
	activeProtocol, err := resolveActiveClientProtocol(r.Context(), h.accounts, accountID, profile, subscription.Server)
	if err != nil {
		h.databaseError(w, "resolve active vpn client protocol", err)
		return
	}

	response := ClientProfileStateResponse{VPNAccountID: accountID, ActiveProtocol: activeProtocol}
	// Evaluate exactly the connection GET /client-connection serves, so both
	// endpoints agree; the rendered links are discarded.
	connection, err := h.clientConnection(r.Context(), accountID)
	switch {
	case err == nil:
		response.ConnectionStatus = ClientConnectionStatusReady
	case errors.Is(err, pgx.ErrNoRows):
		writeAccountNotFound(w)
		return
	case errors.Is(err, ErrVPNAccountUnassigned):
		response.ConnectionStatus = ClientConnectionStatusUnassigned
		response.ConnectionMessage = "Assign a VPN node before creating a client connection."
	case errors.Is(err, ErrNodeConfigNotApplied):
		response.ConnectionStatus = ClientConnectionStatusAwaitingFirstApply
		response.ConnectionMessage = clientConnectionUnavailableMessage(err)
	case errors.Is(err, ErrAccountProtocolNotDeployed):
		response.ConnectionStatus = ClientConnectionStatusAwaitingApply
		response.ConnectionMessage = clientConnectionUnavailableMessage(err)
	case errors.Is(err, ErrClientConnectionUnavailable):
		response.ConnectionStatus = ClientConnectionStatusUnavailable
		response.ConnectionMessage = clientConnectionUnavailableMessage(err)
	default:
		h.databaseError(w, "evaluate vpn client connection state", err)
		return
	}
	// Report as active exactly what the connection serves: nothing while it
	// is withheld, otherwise every protocol it carries. This includes MTProto
	// served through the applied node-wide proxy without an active_enabled row.
	profile.ActiveProtocols = servedProtocols(connection)
	if err == nil {
		response.ActiveProtocol = connection.Protocol
	}
	profile.ResolvedFingerprint = resolveClientFingerprint(profile)
	response.Profile = profile
	httpx.WriteJSON(w, http.StatusOK, response)
}

// servedProtocols lists the protocols a built client connection carries.
func servedProtocols(connection ClientConnectionResponse) []string {
	served := make([]string, 0, len(connection.Connections))
	for _, item := range connection.Connections {
		if item.Protocol != "" && !containsClientProtocol(served, item.Protocol) {
			served = append(served, item.Protocol)
		}
	}
	return served
}
