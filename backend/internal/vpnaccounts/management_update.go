package vpnaccounts

import (
	"encoding/json"
	"errors"
	"net/http"

	"github.com/jackc/pgx/v5"

	"github.com/ikaevus/routegate/backend/internal/audit"
	"github.com/ikaevus/routegate/backend/internal/httpx"
)

// UpdateManaged is the admin-management PATCH path. It preserves the existing
// partial-update semantics while also allowing nullable account policy fields
// to be explicitly cleared.
func (h *Handler) UpdateManaged(w http.ResponseWriter, r *http.Request) {
	var request UpdateAccountRequest
	if err := json.NewDecoder(r.Body).Decode(&request); err != nil {
		writeInvalidRequest(w, "Request body must be valid JSON.")
		return
	}

	trimStringPointer(request.DisplayName)
	trimStringPointer(request.Email)
	trimStringPointer(request.Status)
	trimStringPointer(request.ServerID)

	if request.ClearExpiresAt && request.ExpiresAt != nil {
		writeInvalidRequest(w, "expiresAt and clearExpiresAt cannot be used together")
		return
	}
	if request.ClearMaxDevices && request.MaxDevices != nil {
		writeInvalidRequest(w, "maxDevices and clearMaxDevices cannot be used together")
		return
	}

	input := UpdateAccountInput{
		DisplayName:     request.DisplayName,
		Email:           request.Email,
		Status:          request.Status,
		ExpiresAt:       request.ExpiresAt,
		ClearExpiresAt:  request.ClearExpiresAt,
		MaxDevices:      request.MaxDevices,
		ClearMaxDevices: request.ClearMaxDevices,
		ServerID:        request.ServerID,
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
			"display_name":      account.DisplayName,
			"email":             account.Email,
			"server_id":         account.ServerID,
			"status":            account.Status,
			"expires_at":        account.ExpiresAt,
			"max_devices":       account.MaxDevices,
			"clear_expires_at":  request.ClearExpiresAt,
			"clear_max_devices": request.ClearMaxDevices,
		},
	})

	httpx.WriteJSON(w, http.StatusOK, account)
}
