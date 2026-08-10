package vpnaccounts

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"strings"

	"github.com/ikaevus/routegate/backend/internal/audit"
	"github.com/ikaevus/routegate/backend/internal/httpx"
)

type bulkAccountRepository interface {
	BulkAction(context.Context, BulkAccountActionInput) (BulkAccountActionResult, error)
}

type BulkAccountSelectionRequest struct {
	IDs         []string `json:"ids"`
	AllMatching bool     `json:"allMatching"`
	Search      string   `json:"search"`
	Status      string   `json:"status"`
	ServerID    string   `json:"serverId"`
}

type BulkAccountActionRequest struct {
	Action         string                      `json:"action"`
	Selection      BulkAccountSelectionRequest `json:"selection"`
	TargetServerID string                      `json:"targetServerId"`
}

type BulkAccountActionResponse struct {
	AffectedCount         int64    `json:"affectedCount"`
	AffectedServerIDs     []string `json:"affectedServerIds"`
	ConfigurationChanged  bool     `json:"configurationChanged"`
}

func (h *Handler) BulkAction(w http.ResponseWriter, r *http.Request) {
	repository, ok := h.accounts.(bulkAccountRepository)
	if !ok {
		httpx.WriteJSON(w, http.StatusNotImplemented, httpx.Error("bulk_actions_unavailable", "Bulk VPN account actions are unavailable."))
		return
	}

	var request BulkAccountActionRequest
	if err := json.NewDecoder(r.Body).Decode(&request); err != nil {
		writeInvalidRequest(w, "Request body must be valid JSON.")
		return
	}
	input, err := validateBulkActionRequest(request)
	if err != nil {
		writeInvalidRequest(w, err.Error())
		return
	}

	result, err := repository.BulkAction(r.Context(), input)
	if err != nil {
		h.databaseError(w, "bulk vpn account action", err)
		return
	}

	h.recordAudit(r, audit.EventInput{
		Action:       "vpn_account.bulk_" + input.Action,
		ResourceType: "vpn_account",
		ResourceID:   "bulk",
		Result:       audit.ResultSuccess,
		Metadata: map[string]any{
			"affected_count":      result.AffectedCount,
			"affected_server_ids": result.AffectedServerIDs,
			"all_matching":        input.Selection.AllMatching,
			"explicit_id_count":   len(input.Selection.IDs),
		},
	})

	httpx.WriteJSON(w, http.StatusOK, BulkAccountActionResponse{
		AffectedCount:        result.AffectedCount,
		AffectedServerIDs:    result.AffectedServerIDs,
		ConfigurationChanged: result.ConfigurationChanged,
	})
}

func validateBulkActionRequest(request BulkAccountActionRequest) (BulkAccountActionInput, error) {
	action := strings.TrimSpace(request.Action)
	switch action {
	case BulkActionActivate, BulkActionSuspend, BulkActionRevoke, BulkActionDelete, BulkActionAssignServer:
	default:
		return BulkAccountActionInput{}, errors.New("action must be one of: activate, suspend, revoke, delete, assign_server")
	}

	selection, err := validateBulkSelection(request.Selection)
	if err != nil {
		return BulkAccountActionInput{}, err
	}

	targetServerID := strings.TrimSpace(request.TargetServerID)
	if action == BulkActionAssignServer {
		if targetServerID == "" || !uuidSearchPattern.MatchString(targetServerID) {
			return BulkAccountActionInput{}, errors.New("targetServerId must be a UUID for assign_server")
		}
	}

	return BulkAccountActionInput{
		Action:         action,
		Selection:      selection,
		TargetServerID: targetServerID,
	}, nil
}

func validateBulkSelection(request BulkAccountSelectionRequest) (BulkAccountSelection, error) {
	if request.AllMatching && len(request.IDs) > 0 {
		return BulkAccountSelection{}, errors.New("selection must use either ids or allMatching, not both")
	}
	if !request.AllMatching && len(request.IDs) == 0 {
		return BulkAccountSelection{}, errors.New("selection.ids must contain at least one VPN account ID")
	}

	ids := make([]string, 0, len(request.IDs))
	seen := make(map[string]struct{}, len(request.IDs))
	for _, id := range request.IDs {
		id = strings.TrimSpace(id)
		if !uuidSearchPattern.MatchString(id) {
			return BulkAccountSelection{}, errors.New("selection.ids must contain UUID values")
		}
		if _, exists := seen[id]; exists {
			continue
		}
		seen[id] = struct{}{}
		ids = append(ids, id)
	}

	status := strings.TrimSpace(request.Status)
	if status != "" && !ValidStatus(status) {
		return BulkAccountSelection{}, errors.New("selection.status is invalid")
	}
	serverID := strings.TrimSpace(request.ServerID)
	if serverID != "" && !uuidSearchPattern.MatchString(serverID) {
		return BulkAccountSelection{}, errors.New("selection.serverId must be a UUID")
	}
	search := strings.TrimSpace(request.Search)
	searchUUID := nilSearchUUID
	if uuidSearchPattern.MatchString(search) {
		searchUUID = search
	}

	return BulkAccountSelection{
		IDs:         ids,
		AllMatching: request.AllMatching,
		Filter: AccountFilter{
			Status:     status,
			ServerID:   serverID,
			Search:     search,
			SearchUUID: searchUUID,
		},
	}, nil
}
