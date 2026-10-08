package vpnaccounts

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/ikaevus/routegate/backend/internal/audit"
	"github.com/ikaevus/routegate/backend/internal/httpx"
)

// A preliminary import is NOT a subscription and is NOT a ready connection.
// Only an authenticated operator who explicitly acknowledged this risk can
// inspect a VLESS share link based on saved, not-yet-applied node settings.
var (
	ErrPreImportNotPending = errors.New("VLESS pre-import is only available when the applied snapshot confirms this account has no VLESS access")
	ErrPreImportNotAllowed = errors.New("the account must be active and unexpired, assigned to a node, and configured for VLESS")
	ErrPreImportNotPrepared = errors.New("saved VLESS/Reality node settings are not available")
)

const preImportWarning = "PRELIMINARY ONLY: this VLESS configuration is based on saved, unapplied settings. It may not connect. Do not treat it as a working subscription; changes before apply can make it obsolete. Import the normal subscription once the node confirms apply."

type UnappliedVLESSPreview struct {
	Status   string `json:"status"`
	Protocol string `json:"protocol"`
	Format   string `json:"format"`
	VLESSURI string `json:"vlessUri"`
	Warning  string `json:"warning"`
}

type unappliedVLESSRequest struct {
	AcknowledgeUnapplied bool `json:"acknowledgeUnapplied"`
}

func BuildUnappliedVLESSPreview(ctx context.Context, source ClientConnectionSource, accountID string) (UnappliedVLESSPreview, error) {
	subscription, err := source.GetSubscriptionProfileByAccountID(ctx, accountID)
	if err != nil {
		return UnappliedVLESSPreview{}, err
	}
	if subscription.Account.Status != StatusActive ||
		(subscription.Account.ExpiresAt != nil && !subscription.Account.ExpiresAt.After(time.Now())) ||
		subscription.Server == nil {
		return UnappliedVLESSPreview{}, ErrPreImportNotAllowed
	}
	// Fail closed if deployment is unknown. Never hand out "preview" material
	// for already deployed VLESS access: use the ordinary applied subscription.
	server := subscription.Server
	pending := server.AwaitingFirstApply ||
		(server.deployment != nil && server.deployment.accountsKnown &&
			!containsClientProtocol(server.deployment.protocols, ClientProtocolVLESS))
	if !pending {
		return UnappliedVLESSPreview{}, ErrPreImportNotPending
	}
	if subscription.savedServer == nil {
		return UnappliedVLESSPreview{}, ErrPreImportNotPrepared
	}
	profile, err := source.GetOrCreateClientProfile(ctx, accountID)
	if err != nil {
		return UnappliedVLESSPreview{}, err
	}
	if err := hydrateClientProtocolSets(ctx, source, accountID, &profile); err != nil {
		return UnappliedVLESSPreview{}, err
	}
	saved := subscription.withSavedServerSettings()
	if !containsClientProtocol(effectiveRequestedProtocols(profile, saved.Server), ClientProtocolVLESS) {
		return UnappliedVLESSPreview{}, ErrPreImportNotAllowed
	}
	candidate := profile
	candidate.Protocol = ClientProtocolVLESS
	if err := validateClientProtocolTopologyForSource(ctx, source, saved, candidate); err != nil {
		return UnappliedVLESSPreview{}, ErrPreImportNotAllowed
	}
	// The current sing-box adapter serves VLESS/Reality over TCP; non-TCP
	// saved transports do not reach the node runtime when it is applied.
	saved.Server.VLESSNetwork = "tcp"
	link, _, _, _, _, err := buildClientVLESSLink(saved, profile)
	if err != nil {
		return UnappliedVLESSPreview{}, ErrPreImportNotPrepared
	}
	return UnappliedVLESSPreview{
		Status: "unapplied_preview", Protocol: ClientProtocolVLESS,
		Format: "vless-reality-uri", VLESSURI: link, Warning: preImportWarning,
	}, nil
}

// PreviewUnappliedVLESS is an operator-only, explicit POST. It creates no
// bearer subscription token and never modifies the node's applied state.
func (h *Handler) PreviewUnappliedVLESS(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("Pragma", "no-cache")
	w.Header().Set("X-Content-Type-Options", "nosniff")
	w.Header().Set("Referrer-Policy", "no-referrer")

	var request unappliedVLESSRequest
	decoder := json.NewDecoder(http.MaxBytesReader(w, r.Body, 1024))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&request); err != nil {
		writeInvalidRequest(w, "Request must contain an explicit acknowledgement.")
		return
	}
	var extra any
	if err := decoder.Decode(&extra); !errors.Is(err, io.EOF) {
		writeInvalidRequest(w, "Request must contain exactly one JSON object.")
		return
	}
	if !request.AcknowledgeUnapplied {
		writeInvalidRequest(w, "Acknowledge the risk of importing unapplied access before requesting a preview.")
		return
	}

	accountID := r.PathValue("id")
	source, ok := h.accounts.(ClientConnectionSource)
	if !ok {
		httpx.WriteJSON(w, http.StatusInternalServerError, httpx.Error("preimport_unavailable", "Client profile storage is unavailable."))
		return
	}
	preview, err := BuildUnappliedVLESSPreview(r.Context(), source, accountID)
	if errors.Is(err, pgx.ErrNoRows) {
		writeAccountNotFound(w)
		return
	}
	if err != nil {
		if errors.Is(err, ErrPreImportNotAllowed) || errors.Is(err, ErrPreImportNotPending) || errors.Is(err, ErrPreImportNotPrepared) {
			httpx.WriteJSON(w, http.StatusConflict, httpx.Error("preimport_unavailable", err.Error()))
			return
		}
		h.databaseError(w, "prepare unapplied VLESS preview", err)
		return
	}

	if h.audit != nil {
		h.recordAudit(r, audit.EventInput{
			Action: "vpn_client_profile.unapplied_preimport_previewed",
			ResourceType: "vpn_account", ResourceID: accountID,
			Result: audit.ResultSuccess,
			Metadata: map[string]any{"protocol": ClientProtocolVLESS, "state": "unapplied_preview"},
		})
	}
	httpx.WriteJSON(w, http.StatusOK, preview)
}
