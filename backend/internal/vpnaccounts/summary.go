package vpnaccounts

import (
	"context"
	"net/http"

	"github.com/ikaevus/routegate/backend/internal/httpx"
)

type AccountStatsResponse struct {
	Total  int `json:"total"`
	Active int `json:"active"`
}

type accountSummaryRepository interface {
	ListReadinessAccounts(context.Context) ([]Account, error)
	AccountStats(context.Context) (AccountStatsResponse, error)
}

func (r *Repository) ListReadinessAccounts(ctx context.Context) ([]Account, error) {
	rows, err := r.pool.Query(ctx, `
		SELECT DISTINCT ON (server_id)
			id::text,
			display_name,
			COALESCE(email, ''),
			status,
			expires_at,
			max_devices,
			COALESCE(server_id::text, ''),
			COALESCE(vless_uuid::text, ''),
			created_at,
			updated_at,
			config_updated_at
		FROM vpn_accounts
		WHERE status = 'active'
		  AND server_id IS NOT NULL
		ORDER BY server_id, config_updated_at DESC, created_at DESC, id DESC
	`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	items := make([]Account, 0)
	for rows.Next() {
		item, err := scanAccount(rows)
		if err != nil {
			return nil, err
		}
		items = append(items, item)
	}
	return items, rows.Err()
}

func (r *Repository) AccountStats(ctx context.Context) (AccountStatsResponse, error) {
	var stats AccountStatsResponse
	err := r.pool.QueryRow(ctx, `
		SELECT
			COUNT(*)::int,
			COUNT(*) FILTER (WHERE status = 'active')::int
		FROM vpn_accounts
	`).Scan(&stats.Total, &stats.Active)
	return stats, err
}

func (h *Handler) Readiness(w http.ResponseWriter, r *http.Request) {
	repository, ok := h.accounts.(accountSummaryRepository)
	if !ok {
		httpx.WriteJSON(w, http.StatusNotImplemented, httpx.Error("account_summary_unavailable", "VPN account readiness summary is unavailable."))
		return
	}
	items, err := repository.ListReadinessAccounts(r.Context())
	if err != nil {
		h.databaseError(w, "list vpn account readiness", err)
		return
	}
	httpx.WriteJSON(w, http.StatusOK, ListAccountsResponse{
		Items:      items,
		Total:      len(items),
		Page:       1,
		PageSize:   len(items),
		TotalPages: 1,
	})
}

func (h *Handler) Stats(w http.ResponseWriter, r *http.Request) {
	repository, ok := h.accounts.(accountSummaryRepository)
	if !ok {
		httpx.WriteJSON(w, http.StatusNotImplemented, httpx.Error("account_summary_unavailable", "VPN account statistics are unavailable."))
		return
	}
	stats, err := repository.AccountStats(r.Context())
	if err != nil {
		h.databaseError(w, "get vpn account statistics", err)
		return
	}
	httpx.WriteJSON(w, http.StatusOK, stats)
}
