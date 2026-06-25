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

	"github.com/ikaevus/routegate/backend/internal/httpx"
)

type accountRepository interface {
	CreateAccount(context.Context, CreateAccountInput) (Account, error)
	ListAccounts(context.Context, AccountFilter) ([]Account, error)
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
	generateSubscriptionToken func() (string, error)
}

func NewHandler(logger *slog.Logger, pool *pgxpool.Pool) *Handler {
	return &Handler{
		logger:                    logger,
		accounts:                  NewRepository(pool),
		generateSubscriptionToken: GenerateSubscriptionToken,
	}
}

func (h *Handler) List(w http.ResponseWriter, r *http.Request) {
	items, err := h.accounts.ListAccounts(r.Context(), AccountFilter{
		Status:   strings.TrimSpace(r.URL.Query().Get("status")),
		ServerID: strings.TrimSpace(r.URL.Query().Get("serverId")),
		Search:   strings.TrimSpace(r.URL.Query().Get("search")),
	})
	if err != nil {
		h.databaseError(w, "list vpn accounts", err)
		return
	}

	httpx.WriteJSON(w, http.StatusOK, ListAccountsResponse{Items: items})
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
	err := h.accounts.DeleteAccount(r.Context(), r.PathValue("id"))
	if errors.Is(err, pgx.ErrNoRows) {
		writeAccountNotFound(w)
		return
	}
	if err != nil {
		h.databaseError(w, "delete vpn account", err)
		return
	}

	w.WriteHeader(http.StatusNoContent)
}

func (h *Handler) CreateSubscriptionToken(w http.ResponseWriter, r *http.Request) {
	h.createOrRotateSubscriptionToken(w, r)
}

func (h *Handler) RotateSubscriptionToken(w http.ResponseWriter, r *http.Request) {
	h.createOrRotateSubscriptionToken(w, r)
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
	if _, err := h.accounts.GetActiveSubscriptionTokenByHash(r.Context(), accountID, HashSubscriptionToken(rawToken)); errors.Is(err, pgx.ErrNoRows) {
		httpx.WriteJSON(w, http.StatusNotFound, httpx.Error("subscription_token_not_found", "Active subscription token not found."))
		return
	} else if err != nil {
		h.databaseError(w, "get subscription token for qr", err)
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
		writeInvalidRequest(w, "Subscription token is required.")
		return
	}

	token, err := h.accounts.FindActiveSubscriptionTokenByHash(r.Context(), HashSubscriptionToken(rawToken))
	if errors.Is(err, pgx.ErrNoRows) {
		httpx.WriteJSON(w, http.StatusNotFound, httpx.Error("subscription_not_found", "Subscription token not found."))
		return
	}
	if err != nil {
		h.databaseError(w, "get public subscription", err)
		return
	}

	now := time.Now()
	profile, err := h.accounts.GetSubscriptionProfileByAccountID(r.Context(), token.VPNAccountID)
	if errors.Is(err, pgx.ErrNoRows) {
		httpx.WriteJSON(w, http.StatusNotFound, httpx.Error("subscription_account_not_found", "VPN account for subscription token not found."))
		return
	}
	if err != nil {
		h.databaseError(w, "get subscription profile", err)
		return
	}
	if profile.Account.Status != StatusActive {
		httpx.WriteJSON(w, http.StatusForbidden, httpx.Error("subscription_inactive", "VPN account is not active."))
		return
	}
	if profile.Account.ExpiresAt != nil && !profile.Account.ExpiresAt.After(now) {
		httpx.WriteJSON(w, http.StatusForbidden, httpx.Error("subscription_expired", "VPN account is expired."))
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
		Config: PublicSubscriptionConfig{
			Type:    "sing-box",
			Status:  "pending",
			Message: "Client config generation is not implemented yet.",
		},
	})
}

func (h *Handler) createOrRotateSubscriptionToken(w http.ResponseWriter, r *http.Request) {
	accountID := r.PathValue("id")
	var request CreateSubscriptionTokenRequest
	if err := decodeOptionalJSON(r.Body, &request); err != nil {
		writeInvalidRequest(w, "Request body must be valid JSON.")
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

	httpx.WriteJSON(w, http.StatusCreated, SubscriptionTokenResponse{
		VPNAccountID:      accountID,
		SubscriptionToken: rawToken,
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

	httpx.WriteJSON(w, http.StatusOK, account)
}

func publicSubscriptionServer(server *SubscriptionServer) *PublicSubscriptionServer {
	if server == nil {
		return nil
	}
	endpoint := server.PublicIP
	if endpoint == "" {
		endpoint = server.Hostname
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
	scheme := strings.TrimSpace(r.Header.Get("X-Forwarded-Proto"))
	if scheme == "" {
		if r.TLS != nil {
			scheme = "https"
		} else {
			scheme = "http"
		}
	}
	host := strings.TrimSpace(r.Header.Get("X-Forwarded-Host"))
	if host == "" {
		host = r.Host
	}
	return (&url.URL{
		Scheme: scheme,
		Host:   host,
		Path:   "/api/v1/subscriptions/" + token,
	}).String()
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

func writeInvalidRequest(w http.ResponseWriter, message string) {
	httpx.WriteJSON(w, http.StatusBadRequest, httpx.Error("invalid_request", message))
}

func writeAccountNotFound(w http.ResponseWriter) {
	httpx.WriteJSON(w, http.StatusNotFound, httpx.Error("vpn_account_not_found", "VPN account not found."))
}

func (h *Handler) databaseError(w http.ResponseWriter, operation string, err error) {
	h.logger.Error(operation+" failed", "error", err)
	httpx.WriteJSON(w, http.StatusInternalServerError, httpx.Error("database_error", "Database operation failed."))
}
