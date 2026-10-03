package vpnaccounts

import (
	"context"
	"errors"
	"net/http"

	"github.com/jackc/pgx/v5"

	"github.com/ikaevus/routegate/backend/internal/httpx"
)

// Node access states of the client access summary.
const (
	NodeClientAccessServed    = "served"
	NodeClientAccessNotServed = "not_served"
	// NodeClientAccessUnknown means the evaluation could not finish (an error,
	// a lock or statement timeout, or the per-node budget). It never means
	// that the node serves no account.
	NodeClientAccessUnknown = "unknown"
)

// clientAccessEvaluationsPerNode bounds the accounts evaluated per node and
// request. Accounts the node's applied configuration lists are evaluated
// first, so a node that serves accounts is normally decided by its first one.
const clientAccessEvaluationsPerNode = 200

// NodeClientAccess reports whether a node issues client access to at least one
// of its active accounts. It carries account ids only, never client links or
// credentials.
type NodeClientAccess struct {
	ServerID       string `json:"serverId"`
	ActiveAccounts int    `json:"activeAccounts"`
	State          string `json:"state"`
	// ServedAccountID is an active account the node issues client access to.
	ServedAccountID string `json:"servedAccountId,omitempty"`
	// Pending* describe the first evaluated account without access when the
	// node serves none: its client-profile connection status and message.
	PendingAccountID string `json:"pendingAccountId,omitempty"`
	PendingStatus    string `json:"pendingStatus,omitempty"`
	PendingMessage   string `json:"pendingMessage,omitempty"`
}

type ClientAccessSummaryResponse struct {
	Items []NodeClientAccess `json:"items"`
}

type clientAccessSummaryRepository interface {
	ClientAccessSummary(context.Context) ([]NodeClientAccess, error)
}

// clientConnectionStatus maps a client connection evaluation to the status
// GET /client-profile reports. ok is false for errors that are not a client
// access decision (storage failures, a missing account).
func clientConnectionStatus(err error) (status string, message string, ok bool) {
	switch {
	case err == nil:
		return ClientConnectionStatusReady, "", true
	case errors.Is(err, ErrVPNAccountUnassigned):
		return ClientConnectionStatusUnassigned, "Assign a VPN node before creating a client connection.", true
	case errors.Is(err, ErrNodeConfigNotApplied):
		return ClientConnectionStatusAwaitingFirstApply, clientConnectionUnavailableMessage(err), true
	case errors.Is(err, ErrAccountProtocolNotDeployed):
		return ClientConnectionStatusAwaitingApply, clientConnectionUnavailableMessage(err), true
	case errors.Is(err, ErrClientConnectionUnavailable):
		return ClientConnectionStatusUnavailable, clientConnectionUnavailableMessage(err), true
	default:
		return "", "", false
	}
}

// ClientAccessSummary evaluates, per node, whether at least one active account
// gets client access, with exactly the evaluation that issues client links
// (BuildClientConnection: applied configuration, protocol sets, MTProto proxy,
// topology). It has no lasting effect: profile and protocol rows that the
// evaluation creates on first read are written inside a transaction that is
// always rolled back, one savepoint per account.
func (r *Repository) ClientAccessSummary(ctx context.Context) ([]NodeClientAccess, error) {
	type candidate struct{ serverID, accountID string }
	// Accounts listed by the node's applied configuration first: only an
	// evaluation order, never a substitute for the evaluation itself.
	rows, err := r.db.Query(ctx, `
		SELECT a.server_id::text, a.id::text
		FROM vpn_accounts a
		JOIN servers s ON s.id = a.server_id
		LEFT JOIN config_versions acv ON acv.id = s.active_config_version_id
		WHERE a.status = 'active'
		ORDER BY a.server_id,
			COALESCE(jsonb_typeof(acv.client_settings -> 'accounts') = 'object'
				AND (acv.client_settings -> 'accounts') ? a.id::text, FALSE) DESC,
			a.created_at, a.id
	`)
	if err != nil {
		return nil, err
	}
	var candidates []candidate
	for rows.Next() {
		var item candidate
		if err := rows.Scan(&item.serverID, &item.accountID); err != nil {
			rows.Close()
			return nil, err
		}
		candidates = append(candidates, item)
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return nil, err
	}

	summaries := []NodeClientAccess{}
	byServer := map[string]int{}
	for _, item := range candidates {
		index, ok := byServer[item.serverID]
		if !ok {
			index = len(summaries)
			byServer[item.serverID] = index
			summaries = append(summaries, NodeClientAccess{ServerID: item.serverID, State: NodeClientAccessNotServed})
		}
		summaries[index].ActiveAccounts++
	}
	if len(candidates) == 0 {
		return summaries, nil
	}

	tx, err := r.db.Begin(ctx)
	if err != nil {
		return nil, err
	}
	defer func() { _ = tx.Rollback(ctx) }()
	// Never wait long behind an administrator's profile change.
	for _, setting := range []string{`SET LOCAL lock_timeout = '2s'`, `SET LOCAL statement_timeout = '10s'`} {
		if _, err := tx.Exec(ctx, setting); err != nil {
			return nil, err
		}
	}

	evaluated := map[string]int{}
	decided := map[string]bool{}
	for _, item := range candidates {
		summary := &summaries[byServer[item.serverID]]
		if decided[item.serverID] {
			continue
		}
		if evaluated[item.serverID] >= clientAccessEvaluationsPerNode {
			summary.State = NodeClientAccessUnknown
			decided[item.serverID] = true
			continue
		}
		evaluated[item.serverID]++

		savepoint, err := tx.Begin(ctx)
		if err != nil {
			// The transaction cannot continue: nothing further is decided.
			for index := range summaries {
				if !decided[summaries[index].ServerID] {
					summaries[index].State = NodeClientAccessUnknown
				}
			}
			return summaries, nil
		}
		_, evaluation := BuildClientConnection(ctx, &Repository{db: savepoint, pool: r.pool}, item.accountID)
		rollbackErr := savepoint.Rollback(ctx)

		status, message, known := clientConnectionStatus(evaluation)
		switch {
		case errors.Is(evaluation, pgx.ErrNoRows):
			// Removed or reassigned meanwhile: not an access decision.
		case !known || rollbackErr != nil:
			summary.State = NodeClientAccessUnknown
			decided[item.serverID] = true
		case status == ClientConnectionStatusReady:
			summary.State = NodeClientAccessServed
			summary.ServedAccountID = item.accountID
			summary.PendingAccountID, summary.PendingStatus, summary.PendingMessage = "", "", ""
			decided[item.serverID] = true
		default:
			if summary.PendingAccountID == "" {
				summary.PendingAccountID, summary.PendingStatus, summary.PendingMessage = item.accountID, status, message
			}
			// Without an applied configuration no account of the node is served.
			if errors.Is(evaluation, ErrNodeConfigNotApplied) {
				decided[item.serverID] = true
			}
		}
		if ctx.Err() != nil {
			for index := range summaries {
				if !decided[summaries[index].ServerID] {
					summaries[index].State = NodeClientAccessUnknown
				}
			}
			return summaries, nil
		}
	}
	return summaries, nil
}

// GetClientAccessSummary reports, per node, whether it issues client access to
// at least one active account. It is read-only for callers (see
// ClientAccessSummary) and returns no links or credentials.
func (h *Handler) GetClientAccessSummary(w http.ResponseWriter, r *http.Request) {
	repository, ok := h.accounts.(clientAccessSummaryRepository)
	if !ok {
		httpx.WriteJSON(w, http.StatusInternalServerError, httpx.Error("client_access_summary_unavailable", "Client access summary is unavailable."))
		return
	}
	items, err := repository.ClientAccessSummary(r.Context())
	if err != nil {
		h.databaseError(w, "summarize vpn client access", err)
		return
	}
	httpx.WriteJSON(w, http.StatusOK, ClientAccessSummaryResponse{Items: items})
}
