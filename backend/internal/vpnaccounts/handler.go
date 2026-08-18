package vpnaccounts

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/ikaevus/routegate/backend/internal/audit"
	"github.com/ikaevus/routegate/backend/internal/auth"
	"github.com/ikaevus/routegate/backend/internal/httpx"
)

type accountRepository interface {
	CreateAccount(context.Context, CreateAccountInput) (Account, error)
	ListAccounts(context.Context, AccountFilter) ([]Account, error)
	CountAccounts(context.Context, AccountFilter) (int, error)
	GetAccountByID(context.Context, string) (Account, error)
	UpdateAccount(context.Context, string, UpdateAccountInput) (Account, error)
	SetAccountStatus(context.Context, string, string) (Account, error)
	DeleteAccount(context.Context, string) error
	CreateSubscriptionToken(context.Context, CreateSubscriptionTokenInput) (SubscriptionToken, error)
	RevokeActiveSubscriptionTokens(context.Context, string) error
	GetActiveSubscriptionTokenByHash(context.Context, string, string) (SubscriptionToken, error)
	FindActiveSubscriptionTokenByHash(context.Context, string) (SubscriptionToken, error)
	GetSubscriptionProfileByAccountID(context.Context, string) (SubscriptionProfile, error)
	MarkSubscriptionTokenUsed(context.Context, string) error
}

type Handler struct {
	logger                    *slog.Logger
	accounts                  accountRepository
	audit                     *audit.Recorder
	generateSubscriptionToken func() (string, error)
}

func NewHandler(logger *slog.Logger, pool *pgxpool.Pool) *Handler {
	return &Handler{
		logger:                    logger,
		accounts:                  NewRepository(pool),
		audit:                     audit.NewRecorder(logger, pool),
		generateSubscriptionToken: GenerateSubscriptionToken,
	}
}

func (h *Handler) List(w http.ResponseWriter, r *http.Request) {
	filter, page, pageSize, err := parseAccountListFilter(r)
	if err != nil {
		writeInvalidRequest(w, err.Error())
		return
	}

	items, err := h.accounts.ListAccounts(r.Context(), filter)
	if err != nil {
		h.databaseError(w, "list vpn accounts", err)
		return
	}
	total, err := h.accounts.CountAccounts(r.Context(), filter)
	if err != nil {
		h.databaseError(w, "count vpn accounts", err)
		return
	}

	httpx.WriteJSON(w, http.StatusOK, ListAccountsResponse{
		Items:      items,
		Total:      total,
		Page:       page,
		PageSize:   pageSize,
		TotalPages: totalPages(total, pageSize),
	})
}

func (h *Handler) Create(w http.ResponseWriter, r *http.Request) {
	var request CreateAccountRequest
	if err := json.NewDecoder(r.Body).Decode(&request); err != nil {
		writeInvalidRequest(w, "Request body must be valid JSON.")
		return
	}

	input := CreateAccountInput{
		DisplayName: strings.TrimSpace(request.DisplayName),
		Email:       strings.TrimSpace(request.Email),
		Status:      strings.TrimSpace(request.Status),
		ExpiresAt:   request.ExpiresAt,
		MaxDevices:  request.MaxDevices,
		ServerID:    strings.TrimSpace(request.ServerID),
	}
	if input.Status == "" {
		input.Status = StatusCreated
	}
	if err := validateCreateInput(input); err != nil {
		writeInvalidRequest(w, err.Error())
		return
	}

	account, err := h.accounts.CreateAccount(r.Context(), input)
	if err != nil {
		h.databaseError(w, "create vpn account", err)
		return
	}

	h.recordAudit(r, audit.EventInput{
		Action:       "vpn_account.created",
		ResourceType: "vpn_account",
		ResourceID:   account.ID,
		Result:       audit.ResultSuccess,
		Metadata: map[string]any{
			"display_name": account.DisplayName,
			"email":        account.Email,
			"server_id":    account.ServerID,
			"status":       account.Status,
		},
	})
	h.logger.Info("vpn account created", "id", account.ID, "display_name", account.DisplayName)
	httpx.WriteJSON(w, http.StatusCreated, account)
}

func (h *Handler) Get(w http.ResponseWriter, r *http.Request) {
	account, err := h.accounts.GetAccountByID(r.Context(), r.PathValue("id"))
	if errors.Is(err, pgx.ErrNoRows) {
		writeAccountNotFound(w)
		return
	}
	if err != nil {
		h.databaseError(w, "get vpn account", err)
		return
	}

	httpx.WriteJSON(w, http.StatusOK, account)
}

func (h *Handler) GetCredentials(w http.ResponseWriter, r *http.Request) {
	profile, err := h.accounts.GetSubscriptionProfileByAccountID(r.Context(), r.PathValue("id"))
	if errors.Is(err, pgx.ErrNoRows) {
		writeAccountNotFound(w)
		return
	}
	if err != nil {
		h.databaseError(w, "get vpn account credentials", err)
		return
	}

	httpx.WriteJSON(w, http.StatusOK, adminCredentialsResponse(profile))
}

func (h *Handler) Update(w http.ResponseWriter, r *http.Request) {
	var request UpdateAccountRequest
	if err := json.NewDecoder(r.Body).Decode(&request); err != nil {
		writeInvalidRequest(w, "Request body must be valid JSON.")
		return
	}

	trimStringPointer(request.DisplayName)
	trimStringPointer(request.Email)
	trimStringPointer(request.Status)
	trimStringPointer(request.ServerID)

	input := UpdateAccountInput{
		DisplayName: request.DisplayName,
		Email:       request.Email,
		Status:      request.Status,
		ExpiresAt:   request.ExpiresAt,
		MaxDevices:  request.MaxDevices,
		ServerID:    request.ServerID,
	}
	if err := validateUpdateInput(input); err != nil {
		writeInvalidRequest(w, err.Error())
		return
	}

	account, err := h.accounts.UpdateAccount(r.Context(), r.PathValue("id"), input)
	if errors.Is(err, pgx.ErrNoRows) {
		writeAccountNotFound(w)
		return
	}
	if err != nil {
		h.databaseError(w, "update vpn account", err)
		return
	}

	h.recordAudit(r, audit.EventInput{
		Action:       "vpn_account.updated",
		ResourceType: "vpn_account",
		ResourceID:   account.ID,
		Result:       audit.ResultSuccess,
		Metadata: map[string]any{
			"display_name": account.DisplayName,
			"email":        account.Email,
			"server_id":    account.ServerID,
			"status":       account.Status,
		},
	})
	httpx.WriteJSON(w, http.StatusOK, account)
}

func (h *Handler) Suspend(w http.ResponseWriter, r *http.Request) {
	h.setStatus(w, r, StatusSuspended)
}

func (h *Handler) Activate(w http.ResponseWriter, r *http.Request) {
	h.setStatus(w, r, StatusActive)
}

func (h *Handler) Revoke(w http.ResponseWriter, r *http.Request) {
	h.setStatus(w, r, StatusRevoked)
}

func (h *Handler) Delete(w http.ResponseWriter, r *http.Request) {
	accountID := r.PathValue("id")
	err := h.accounts.DeleteAccount(r.Context(), accountID)
	if errors.Is(err, pgx.ErrNoRows) {
		writeAccountNotFound(w)
		return
	}
	if err != nil {
		h.databaseError(w, "delete vpn account", err)
		return
	}

	h.recordAudit(r, audit.EventInput{
		Action:       "vpn_account.deleted",
		ResourceType: "vpn_account",
		ResourceID:   accountID,
		Result:       audit.ResultSuccess,
	})
	w.WriteHeader(http.StatusNoContent)
}

func (h *Handler) CreateSubscriptionToken(w http.ResponseWriter, r *http.Request) {
	h.createOrRotateSubscriptionToken(w, r, "subscription_token.created")
}

func (h *Handler) RotateSubscriptionToken(w http.ResponseWriter, r *http.Request) {
	h.createOrRotateSubscriptionToken(w, r, "subscription_token.rotated")
}

func (h *Handler) RevokeSubscriptionToken(w http.ResponseWriter, r *http.Request) {
	accountID := r.PathValue("id")
	if _, err := h.accounts.GetAccountByID(r.Context(), accountID); errors.Is(err, pgx.ErrNoRows) {
		writeAccountNotFound(w)
		return
	} else if err != nil {
		h.databaseError(w, "get vpn account for subscription token revoke", err)
		return
	}

	if err := h.accounts.RevokeActiveSubscriptionTokens(r.Context(), accountID); errors.Is(err, pgx.ErrNoRows) {
		httpx.WriteJSON(w, http.StatusNotFound, httpx.Error("subscription_token_not_found", "Active subscription token not found."))
		return
	} else if err != nil {
		h.databaseError(w, "revoke subscription token", err)
		return
	}

	h.recordAudit(r, audit.EventInput{
		Action:       "subscription_token.revoked",
		ResourceType: "vpn_account",
		ResourceID:   accountID,
		Result:       audit.ResultSuccess,
	})
	w.WriteHeader(http.StatusNoContent)
}

func (h *Handler) GetSubscriptionQRCode(w http.ResponseWriter, r *http.Request) {
	accountID := r.PathValue("id")
	rawToken := strings.TrimSpace(r.URL.Query().Get("token"))
	if rawToken == "" {
		writeInvalidRequest(w, "token query parameter is required")
		return
	}
	if _, err := h.accounts.GetAccountByID(r.Context(), accountID); errors.Is(err, pgx.ErrNoRows) {
		writeAccountNotFound(w)
		return
	} else if err != nil {
		h.databaseError(w, "get vpn account for subscription qr", err)
		return
	}
	token, err := h.accounts.GetActiveSubscriptionTokenByHash(r.Context(), accountID, HashSubscriptionToken(rawToken))
	if errors.Is(err, pgx.ErrNoRows) {
		httpx.WriteJSON(w, http.StatusNotFound, httpx.Error("subscription_token_not_found", "Active subscription token not found."))
		return
	} else if err != nil {
		h.databaseError(w, "get subscription token for qr", err)
		return
	}
	if subscriptionTokenExpired(token, time.Now()) {
		httpx.WriteJSON(w, http.StatusNotFound, httpx.Error("subscription_token_not_found", "Active subscription token not found."))
		return
	}

	subscriptionURL := h.subscriptionURL(r, rawToken)
	httpx.WriteJSON(w, http.StatusOK, SubscriptionQRCodeResponse{
		VPNAccountID:    accountID,
		SubscriptionURL: subscriptionURL,
		QRText:          subscriptionURL,
		Format:          "subscription-url",
	})
}

func (h *Handler) GetPublicSubscription(w http.ResponseWriter, r *http.Request) {
	rawToken := strings.TrimSpace(r.PathValue("token"))
	if rawToken == "" {
		writePublicSubscriptionNotFound(w)
		return
	}

	token, err := h.accounts.FindActiveSubscriptionTokenByHash(r.Context(), HashSubscriptionToken(rawToken))
	if errors.Is(err, pgx.ErrNoRows) {
		writePublicSubscriptionNotFound(w)
		return
	}
	if err != nil {
		h.databaseError(w, "get public subscription", err)
		return
	}

	now := time.Now()
	if subscriptionTokenExpired(token, now) {
		writePublicSubscriptionNotFound(w)
		return
	}

	profile, err := h.accounts.GetSubscriptionProfileByAccountID(r.Context(), token.VPNAccountID)
	if errors.Is(err, pgx.ErrNoRows) {
		writePublicSubscriptionNotFound(w)
		return
	}
	if err != nil {
		h.databaseError(w, "get subscription profile", err)
		return
	}
	if profile.Account.Status != StatusActive {
		writePublicSubscriptionNotFound(w)
		return
	}
	if profile.Account.ExpiresAt != nil && !profile.Account.ExpiresAt.After(now) {
		writePublicSubscriptionNotFound(w)
		return
	}

	if err := h.accounts.MarkSubscriptionTokenUsed(r.Context(), token.ID); err != nil {
		h.databaseError(w, "mark subscription token used", err)
		return
	}

	httpx.WriteJSON(w, http.StatusOK, PublicSubscriptionResponse{
		Status:       "ok",
		Format:       "routegate.subscription.v1",
		GeneratedAt:  now,
		VPNAccountID: token.VPNAccountID,
		Account: PublicSubscriptionAccount{
			ID:          profile.Account.ID,
			DisplayName: profile.Account.DisplayName,
			Status:      profile.Account.Status,
			ExpiresAt:   profile.Account.ExpiresAt,
			MaxDevices:  profile.Account.MaxDevices,
		},
		Server: publicSubscriptionServer(profile.Server),
		Config: renderPublicSubscriptionConfig(profile),
	})
}

func (h *Handler) createOrRotateSubscriptionToken(w http.ResponseWriter, r *http.Request, action string) {
	accountID := r.PathValue("id")
	var request CreateSubscriptionTokenRequest
	if err := decodeOptionalJSON(r.Body, &request); err != nil {
		writeInvalidRequest(w, "Request body must be valid JSON.")
		return
	}
	if subscriptionTokenExpired(SubscriptionToken{ExpiresAt: request.ExpiresAt}, time.Now()) {
		writeInvalidRequest(w, "expiresAt must be in the future")
		return
	}
	if _, err := h.accounts.GetAccountByID(r.Context(), accountID); errors.Is(err, pgx.ErrNoRows) {
		writeAccountNotFound(w)
		return
	} else if err != nil {
		h.databaseError(w, "get vpn account for subscription token", err)
		return
	}

	rawToken, err := h.generateSubscriptionToken()
	if err != nil {
		h.logger.Error("generate subscription token failed", "vpn_account_id", accountID, "error", err)
		httpx.WriteJSON(w, http.StatusInternalServerError, httpx.Error("token_generation_failed", "Failed to generate subscription token."))
		return
	}

	created, err := h.accounts.CreateSubscriptionToken(r.Context(), CreateSubscriptionTokenInput{
		VPNAccountID: accountID,
		TokenHash:    HashSubscriptionToken(rawToken),
		ExpiresAt:    request.ExpiresAt,
	})
	if err != nil {
		h.databaseError(w, "create subscription token", err)
		return
	}

	tokenPreview := MaskSubscriptionToken(rawToken)
	h.recordAudit(r, audit.EventInput{
		Action:       action,
		ResourceType: "vpn_account",
		ResourceID:   accountID,
		Result:       audit.ResultSuccess,
		Metadata: map[string]any{
			"token_id":      created.ID,
			"token_preview": tokenPreview,
			"expires_at":    created.ExpiresAt,
		},
	})
	httpx.WriteJSON(w, http.StatusCreated, SubscriptionTokenResponse{
		VPNAccountID:      accountID,
		SubscriptionToken: rawToken,
		TokenPreview:      tokenPreview,
		SubscriptionURL:   h.subscriptionURL(r, rawToken),
		ExpiresAt:         created.ExpiresAt,
	})
}

func (h *Handler) setStatus(w http.ResponseWriter, r *http.Request, status string) {
	account, err := h.accounts.SetAccountStatus(r.Context(), r.PathValue("id"), status)
	if errors.Is(err, pgx.ErrNoRows) {
		writeAccountNotFound(w)
		return
	}
	if err != nil {
		h.databaseError(w, "set vpn account status", err)
		return
	}

	h.recordAudit(r, audit.EventInput{
		Action:       vpnAccountStatusAuditAction(status),
		ResourceType: "vpn_account",
		ResourceID:   account.ID,
		Result:       audit.ResultSuccess,
		Metadata: map[string]any{
			"status": status,
		},
	})
	httpx.WriteJSON(w, http.StatusOK, account)
}

func adminCredentialsResponse(profile SubscriptionProfile) VLESSRealityCredentialsResponse {
	protocol := "vless"
	if profile.Server != nil && (profile.Server.VPNProtocol == "wireguard" || profile.Server.VPNProtocol == "hysteria2") {
		protocol = profile.Server.VPNProtocol
	}
	response := VLESSRealityCredentialsResponse{
		VPNAccountID: profile.Account.ID,
		Protocol:     protocol,
		VLESS: AdminVLESSCredentials{
			UUID:    profile.Credentials.VLESS.UUID,
			Flow:    profile.Credentials.VLESS.Flow,
			Network: profile.Credentials.VLESS.Network,
		},
		Reality: AdminRealityCredentials{
			PublicKey:  profile.Credentials.Reality.PublicKey,
			ShortID:    profile.Credentials.Reality.ShortID,
			ServerName: profile.Credentials.Reality.ServerName,
		},
		WireGuard: AdminWireGuardCredentials{
			PrivateKey: profile.Credentials.WireGuard.PrivateKey,
			PublicKey:  profile.Credentials.WireGuard.PublicKey,
			Address:    profile.Credentials.WireGuard.Address,
		},
		Hysteria2: AdminHysteria2Credentials{
			Username: profile.Credentials.Hysteria2.Username,
			Password: profile.Credentials.Hysteria2.Password,
		},
	}
	response.Reality.Enabled = response.Reality.PublicKey != ""
	if profile.Server != nil {
		response.ServerID = profile.Server.ID
		response.Endpoint = subscriptionServerEndpoint(profile.Server)
		response.WireGuard.ServerPublicKey = profile.Server.WireGuardPublicKey
		response.WireGuard.DNS = profile.Server.WireGuardDNS
		response.Hysteria2.Domain = profile.Server.Hysteria2Domain
		response.Hysteria2.Port = profile.Server.Hysteria2Port
		response.Hysteria2.ACMEEmail = profile.Server.Hysteria2ACMEEmail
		if profile.Server.VPNProtocol == "hysteria2" {
			response.Endpoint = profile.Server.Hysteria2Domain
		}
	}
	return response
}

func publicSubscriptionServer(server *SubscriptionServer) *PublicSubscriptionServer {
	if server == nil {
		return nil
	}
	endpoint := subscriptionServerEndpoint(server)
	if server.VPNProtocol == "hysteria2" {
		endpoint = server.Hysteria2Domain
	}
	return &PublicSubscriptionServer{
		ID:       server.ID,
		Name:     server.Name,
		Hostname: server.Hostname,
		PublicIP: server.PublicIP,
		Endpoint: endpoint,
		Location: server.Location,
		Provider: server.Provider,
	}
}

func (h *Handler) subscriptionURL(r *http.Request, token string) string {
	return (&url.URL{
		Scheme: subscriptionScheme(r),
		Host:   subscriptionHost(r),
		Path:   "/api/v1/subscriptions/" + token,
	}).String()
}

func subscriptionScheme(r *http.Request) string {
	scheme := strings.ToLower(firstForwardedValue(r.Header.Get("X-Forwarded-Proto")))
	if scheme == "https" || scheme == "http" {
		return scheme
	}
	if r.TLS != nil {
		return "https"
	}
	return "http"
}

func subscriptionHost(r *http.Request) string {
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

func validateCreateInput(input CreateAccountInput) error {
	if input.DisplayName == "" {
		return errors.New("displayName is required")
	}
	if !ValidStatus(input.Status) {
		return errors.New("status must be one of: created, active, suspended, expired, revoked")
	}
	if input.MaxDevices != nil && *input.MaxDevices < 1 {
		return errors.New("maxDevices must be greater than zero")
	}
	return nil
}

func validateUpdateInput(input UpdateAccountInput) error {
	if input.DisplayName != nil && *input.DisplayName == "" {
		return errors.New("displayName is required")
	}
	if input.Status != nil && !ValidStatus(*input.Status) {
		return errors.New("status must be one of: created, active, suspended, expired, revoked")
	}
	if input.MaxDevices != nil && *input.MaxDevices < 1 {
		return errors.New("maxDevices must be greater than zero")
	}
	return nil
}

func subscriptionTokenExpired(token SubscriptionToken, now time.Time) bool {
	return token.ExpiresAt != nil && !token.ExpiresAt.After(now)
}

func decodeOptionalJSON(body io.Reader, target any) error {
	if body == nil {
		return nil
	}
	err := json.NewDecoder(body).Decode(target)
	if errors.Is(err, io.EOF) {
		return nil
	}
	return err
}

func trimStringPointer(value *string) {
	if value != nil {
		*value = strings.TrimSpace(*value)
	}
}

func (h *Handler) recordAudit(r *http.Request, input audit.EventInput) {
	if user, ok := auth.UserFromContext(r.Context()); ok {
		input.ActorUserID = user.ID
		input.ActorType = audit.ActorTypeUser
	} else if input.ActorType == "" {
		input.ActorType = audit.ActorTypeSystem
	}
	h.audit.RecordSafe(r.Context(), input)
}

func vpnAccountStatusAuditAction(status string) string {
	switch status {
	case StatusSuspended:
		return "vpn_account.suspended"
	case StatusActive:
		return "vpn_account.activated"
	case StatusRevoked:
		return "vpn_account.revoked"
	default:
		return "vpn_account.status_updated"
	}
}

func writeInvalidRequest(w http.ResponseWriter, message string) {
	httpx.WriteJSON(w, http.StatusBadRequest, httpx.Error("invalid_request", message))
}

func writeAccountNotFound(w http.ResponseWriter) {
	httpx.WriteJSON(w, http.StatusNotFound, httpx.Error("vpn_account_not_found", "VPN account not found."))
}

func writePublicSubscriptionNotFound(w http.ResponseWriter) {
	httpx.WriteJSON(w, http.StatusNotFound, httpx.Error("subscription_not_found", "Subscription token not found."))
}

func (h *Handler) databaseError(w http.ResponseWriter, operation string, err error) {
	h.logger.Error(operation+" failed", "error", err)
	httpx.WriteJSON(w, http.StatusInternalServerError, httpx.Error("database_error", "Database operation failed."))
}
