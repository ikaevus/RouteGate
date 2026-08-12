package delivery

import (
	"errors"
	"net/http"

	"github.com/jackc/pgx/v5"

	"github.com/ikaevus/routegate/backend/internal/audit"
	"github.com/ikaevus/routegate/backend/internal/auth"
	"github.com/ikaevus/routegate/backend/internal/httpx"
)

func (h *Handler) telegramPairingManager() *TelegramPairingManager {
	return NewTelegramPairingManager(h.repository.pool, h.settings)
}

func (h *Handler) StartTelegramPairing(w http.ResponseWriter, r *http.Request) {
	createdBy := ""
	if user, ok := auth.UserFromContext(r.Context()); ok {
		createdBy = user.ID
	}
	view, err := h.telegramPairingManager().Start(r.Context(), createdBy)
	if err != nil {
		h.writeTelegramPairingError(w, err)
		return
	}
	h.recordAudit(r, audit.EventInput{
		Action:       "delivery.telegram_pairing.started",
		ResourceType: "delivery_provider",
		ResourceID:   "telegram",
		Result:       audit.ResultSuccess,
		Metadata: map[string]any{
			"provider":     "telegram",
			"bot_username": view.BotUsername,
		},
	})
	httpx.WriteJSON(w, http.StatusCreated, view)
}

func (h *Handler) GetTelegramPairing(w http.ResponseWriter, r *http.Request) {
	view, err := h.telegramPairingManager().Get(r.Context(), r.PathValue("pairing_id"))
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			httpx.WriteJSON(w, http.StatusNotFound, httpx.Error("telegram_pairing_not_found", "Telegram pairing session was not found."))
			return
		}
		h.writeTelegramPairingError(w, err)
		return
	}
	httpx.WriteJSON(w, http.StatusOK, view)
}

func (h *Handler) ListTelegramRecipients(w http.ResponseWriter, r *http.Request) {
	items, err := h.telegramPairingManager().ListRecipients(r.Context())
	if err != nil {
		h.databaseError(w, "list_telegram_recipients")
		return
	}
	httpx.WriteJSON(w, http.StatusOK, DeliveryRecipientListResponse{Items: items})
}

func (h *Handler) TestTelegramRecipient(w http.ResponseWriter, r *http.Request) {
	result := h.telegramPairingManager().TestRecipient(r.Context(), r.PathValue("recipient_id"))
	h.recordAudit(r, audit.EventInput{
		Action:       "delivery.telegram_recipient.tested",
		ResourceType: "delivery_recipient",
		ResourceID:   r.PathValue("recipient_id"),
		Result:       map[bool]string{true: audit.ResultSuccess, false: audit.ResultFailure}[result.OK],
		Metadata: map[string]any{
			"provider":   "telegram",
			"ok":         result.OK,
			"error_code": normalizeSafeCode(result.ErrorCode),
		},
	})
	httpx.WriteJSON(w, http.StatusOK, result)
}

func (h *Handler) DeleteTelegramRecipient(w http.ResponseWriter, r *http.Request) {
	deleted, err := h.telegramPairingManager().DeleteRecipient(r.Context(), r.PathValue("recipient_id"))
	if err != nil {
		h.databaseError(w, "delete_telegram_recipient")
		return
	}
	if !deleted {
		httpx.WriteJSON(w, http.StatusNotFound, httpx.Error("delivery_recipient_not_found", "Telegram recipient was not found."))
		return
	}
	h.recordAudit(r, audit.EventInput{
		Action:       "delivery.telegram_recipient.removed",
		ResourceType: "delivery_recipient",
		ResourceID:   r.PathValue("recipient_id"),
		Result:       audit.ResultSuccess,
		Metadata:     map[string]any{"provider": "telegram"},
	})
	w.WriteHeader(http.StatusNoContent)
}

func (h *Handler) writeTelegramPairingError(w http.ResponseWriter, err error) {
	var failure Failure
	if errors.As(err, &failure) {
		code := normalizeSafeCode(failure.Code)
		status := http.StatusBadGateway
		message := "Telegram pairing is temporarily unavailable."
		switch code {
		case "telegram_not_configured", "delivery_provider_disabled":
			status = http.StatusBadRequest
			message = "Telegram delivery must be configured before pairing a recipient."
		case "telegram_unauthorized", "telegram_forbidden":
			status = http.StatusBadRequest
			message = "Telegram rejected the configured bot credential."
		case "telegram_pairing_webhook_conflict":
			status = http.StatusConflict
			message = "Telegram getUpdates cannot be used while another webhook is configured for this bot."
		}
		httpx.WriteJSON(w, status, httpx.Error(code, message))
		return
	}
	h.databaseError(w, "telegram_pairing")
}
