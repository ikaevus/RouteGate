package vpnaccounts

import (
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"
	"strings"

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
}

type Handler struct {
	logger   *slog.Logger
	accounts accountRepository
}

func NewHandler(logger *slog.Logger, pool *pgxpool.Pool) *Handler {
	return &Handler{
		logger:   logger,
		accounts: NewRepository(pool),
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
