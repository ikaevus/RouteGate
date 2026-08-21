package vpnaccounts

import (
	"context"
	"database/sql"
	"errors"
	"net/http"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/ikaevus/routegate/backend/internal/audit"
	"github.com/ikaevus/routegate/backend/internal/httpx"
	"github.com/ikaevus/routegate/backend/internal/nodegroups"
	wgcredentials "github.com/ikaevus/routegate/backend/internal/wireguard"
)

const (
	SelectionStatusSelected          = "selected"
	SelectionStatusCurrent           = "current"
	SelectionStatusNoEligible        = "no_eligible_candidates"
	SelectionStatusNodeGroupRequired = "node_group_required"
	SelectionStatusCooldown          = "cooldown"
)

var (
	ErrAutomaticSelectionDisabled = errors.New("automatic selection is disabled")
	ErrAutomaticSelectionCooldown = errors.New("automatic selection cooldown is active")
	ErrAutomaticSelectionNoTarget = errors.New("automatic selection has no eligible target")
	ErrAutomaticSelectionStatus   = errors.New("VPN account status does not allow automatic selection")
	ErrAutomaticSelectionChanged  = errors.New("VPN account assignment changed during selection")
	ErrAutomaticSelectionGroup    = errors.New("automatic selection requires a node group")
)

type AutomaticSelectionPolicy struct {
	Enabled              bool       `json:"enabled"`
	AllowDegraded        bool       `json:"allowDegraded"`
	CooldownSeconds      int        `json:"cooldownSeconds"`
	LastSelectedAt       *time.Time `json:"lastSelectedAt,omitempty"`
	LastSelectedServerID string     `json:"lastSelectedServerId,omitempty"`
}

type UpdateAutomaticSelectionPolicyRequest struct {
	Enabled         bool `json:"enabled"`
	AllowDegraded   bool `json:"allowDegraded"`
	CooldownSeconds int  `json:"cooldownSeconds"`
}

type UpdateAutomaticSelectionPolicyInput = UpdateAutomaticSelectionPolicyRequest

type AutomaticSelectionCandidate struct {
	ServerID   string   `json:"serverId"`
	ServerName string   `json:"serverName"`
	Protocol   string   `json:"protocol"`
	Health     string   `json:"health"`
	Priority   int      `json:"priority"`
	Weight     int      `json:"weight"`
	Signals    []string `json:"signals"`
}

type AutomaticSelectionDecision struct {
	VPNAccountID       string                       `json:"vpnAccountId"`
	NodeGroupID        string                       `json:"nodeGroupId,omitempty"`
	SelectionStrategy  string                       `json:"selectionStrategy,omitempty"`
	Status             string                       `json:"status"`
	CurrentServerID    string                       `json:"currentServerId,omitempty"`
	SelectedCandidate  *AutomaticSelectionCandidate `json:"selectedCandidate,omitempty"`
	Reasons            []string                     `json:"reasons"`
	EligibleCandidates int                          `json:"eligibleCandidates"`
	EvaluatedAt        time.Time                    `json:"evaluatedAt"`
	BlockedUntil       *time.Time                   `json:"blockedUntil,omitempty"`
	CanApply           bool                         `json:"canApply"`
}

type AutomaticSelectionApplyResponse struct {
	Decision                 AutomaticSelectionDecision `json:"decision"`
	PreviousServerID         string                     `json:"previousServerId,omitempty"`
	SelectedServerID         string                     `json:"selectedServerId"`
	Changed                  bool                       `json:"changed"`
	AffectedServerIDs        []string                   `json:"affectedServerIds"`
	ConfigDeploymentRequired bool                       `json:"configDeploymentRequired"`
}

type automaticSelectionContext struct {
	AccountID       string
	Status          string
	CurrentServerID string
	NodeGroupID     string
	Policy          AutomaticSelectionPolicy
}

func (r *Repository) getAutomaticSelectionPolicy(ctx context.Context, accountID string) (AutomaticSelectionPolicy, error) {
	var policy AutomaticSelectionPolicy
	var lastSelectedAt sql.NullTime
	var lastSelectedServerID sql.NullString
	err := r.pool.QueryRow(ctx, `
		SELECT
			COALESCE(p.enabled, FALSE),
			COALESCE(p.allow_degraded, FALSE),
			COALESCE(p.cooldown_seconds, 300),
			p.last_selected_at,
			p.last_selected_server_id::text
		FROM vpn_accounts a
		LEFT JOIN vpn_account_automatic_selection_policies p ON p.vpn_account_id = a.id
		WHERE a.id = $1::uuid
	`, accountID).Scan(
		&policy.Enabled,
		&policy.AllowDegraded,
		&policy.CooldownSeconds,
		&lastSelectedAt,
		&lastSelectedServerID,
	)
	if lastSelectedAt.Valid {
		value := lastSelectedAt.Time
		policy.LastSelectedAt = &value
	}
	if lastSelectedServerID.Valid {
		policy.LastSelectedServerID = lastSelectedServerID.String
	}
	return policy, err
}

func (r *Repository) UpdateAutomaticSelectionPolicy(ctx context.Context, accountID string, input UpdateAutomaticSelectionPolicyInput) (VPNAccountRoutingPolicy, error) {
	if input.CooldownSeconds < 60 || input.CooldownSeconds > 86400 {
		return VPNAccountRoutingPolicy{}, ErrAutomaticSelectionCooldown
	}
	tx, err := r.pool.Begin(ctx)
	if err != nil {
		return VPNAccountRoutingPolicy{}, err
	}
	defer tx.Rollback(ctx)
	if err := lockVPNAccount(ctx, tx, accountID); err != nil {
		return VPNAccountRoutingPolicy{}, err
	}
	result, err := tx.Exec(ctx, `
		INSERT INTO vpn_account_automatic_selection_policies (
			vpn_account_id, enabled, allow_degraded, cooldown_seconds
		)
		SELECT a.id, $2, $3, $4
		FROM vpn_accounts a
		WHERE a.id = $1::uuid
		  AND (
			NOT $2 OR EXISTS (
				SELECT 1 FROM vpn_account_node_groups ang WHERE ang.vpn_account_id = a.id
			)
		  )
		ON CONFLICT (vpn_account_id) DO UPDATE
		SET enabled = EXCLUDED.enabled,
			allow_degraded = EXCLUDED.allow_degraded,
			cooldown_seconds = EXCLUDED.cooldown_seconds,
			updated_at = now()
	`, accountID, input.Enabled, input.AllowDegraded, input.CooldownSeconds)
	if err != nil {
		return VPNAccountRoutingPolicy{}, err
	}
	if result.RowsAffected() == 0 {
		return VPNAccountRoutingPolicy{}, ErrAutomaticSelectionGroup
	}
	if err := tx.Commit(ctx); err != nil {
		return VPNAccountRoutingPolicy{}, err
	}
	return r.GetRoutingPolicy(ctx, accountID)
}

func lockVPNAccount(ctx context.Context, tx pgx.Tx, accountID string) error {
	var lockedID string
	return tx.QueryRow(ctx, `
		SELECT id::text FROM vpn_accounts WHERE id = $1::uuid FOR UPDATE
	`, accountID).Scan(&lockedID)
}

func lockedAutomaticSelectionContext(ctx context.Context, tx pgx.Tx, accountID string) (automaticSelectionContext, error) {
	if err := lockVPNAccount(ctx, tx, accountID); err != nil {
		return automaticSelectionContext{}, err
	}
	var value automaticSelectionContext
	var nodeGroupID sql.NullString
	var lastSelectedAt sql.NullTime
	err := tx.QueryRow(ctx, `
		SELECT
			a.id::text,
			a.status,
			COALESCE(a.server_id::text, ''),
			ang.node_group_id::text,
			COALESCE(p.enabled, FALSE),
			COALESCE(p.allow_degraded, FALSE),
			COALESCE(p.cooldown_seconds, 300),
			p.last_selected_at,
			COALESCE(p.last_selected_server_id::text, '')
		FROM vpn_accounts a
		LEFT JOIN vpn_account_node_groups ang ON ang.vpn_account_id = a.id
		LEFT JOIN vpn_account_automatic_selection_policies p ON p.vpn_account_id = a.id
		WHERE a.id = $1::uuid
	`, accountID).Scan(
		&value.AccountID,
		&value.Status,
		&value.CurrentServerID,
		&nodeGroupID,
		&value.Policy.Enabled,
		&value.Policy.AllowDegraded,
		&value.Policy.CooldownSeconds,
		&lastSelectedAt,
		&value.Policy.LastSelectedServerID,
	)
	if err != nil {
		return automaticSelectionContext{}, err
	}
	if nodeGroupID.Valid {
		value.NodeGroupID = nodeGroupID.String
	}
	if lastSelectedAt.Valid {
		selectedAt := lastSelectedAt.Time
		value.Policy.LastSelectedAt = &selectedAt
	}
	return value, nil
}

func (r *Repository) automaticSelectionContext(ctx context.Context, accountID string) (automaticSelectionContext, error) {
	var value automaticSelectionContext
	var nodeGroupID sql.NullString
	err := r.pool.QueryRow(ctx, `
		SELECT a.id::text, a.status, COALESCE(a.server_id::text, ''), ang.node_group_id::text
		FROM vpn_accounts a
		LEFT JOIN vpn_account_node_groups ang ON ang.vpn_account_id = a.id
		WHERE a.id = $1::uuid
	`, accountID).Scan(&value.AccountID, &value.Status, &value.CurrentServerID, &nodeGroupID)
	if err != nil {
		return automaticSelectionContext{}, err
	}
	if nodeGroupID.Valid {
		value.NodeGroupID = nodeGroupID.String
	}
	value.Policy, err = r.getAutomaticSelectionPolicy(ctx, accountID)
	return value, err
}

func (r *Repository) PreviewAutomaticSelection(ctx context.Context, accountID string) (AutomaticSelectionDecision, error) {
	return r.previewAutomaticSelectionAt(ctx, accountID, time.Now().UTC())
}

func (r *Repository) previewAutomaticSelectionAt(ctx context.Context, accountID string, now time.Time) (AutomaticSelectionDecision, error) {
	selectionContext, err := r.automaticSelectionContext(ctx, accountID)
	if err != nil {
		return AutomaticSelectionDecision{}, err
	}
	return evaluateAutomaticSelectionAt(ctx, selectionContext, now, nodegroups.NewRepository(r.pool))
}

func evaluateAutomaticSelectionAt(ctx context.Context, selectionContext automaticSelectionContext, now time.Time, groups *nodegroups.Repository) (AutomaticSelectionDecision, error) {
	decision := AutomaticSelectionDecision{
		VPNAccountID:    selectionContext.AccountID,
		NodeGroupID:     selectionContext.NodeGroupID,
		Status:          SelectionStatusNodeGroupRequired,
		CurrentServerID: selectionContext.CurrentServerID,
		Reasons:         []string{SelectionStatusNodeGroupRequired},
		EvaluatedAt:     now,
	}
	if selectionContext.NodeGroupID == "" {
		return decision, nil
	}

	candidates, err := groups.Candidates(ctx, selectionContext.NodeGroupID, now)
	if err != nil {
		return AutomaticSelectionDecision{}, err
	}
	decision.SelectionStrategy = candidates.SelectionStrategy
	currentEligible := false
	for _, candidate := range candidates.Candidates {
		if candidate.Eligible && (candidate.Health == nodegroups.CandidateHealthReady || (selectionContext.Policy.AllowDegraded && candidate.Health == nodegroups.CandidateHealthDegraded)) {
			decision.EligibleCandidates++
			if candidate.ServerID == selectionContext.CurrentServerID {
				currentEligible = true
			}
		}
	}
	selection, ok := nodegroups.SelectCandidate(selectionContext.AccountID, candidates.SelectionStrategy, candidates.Candidates, selectionContext.Policy.AllowDegraded)
	if !ok {
		decision.Status = SelectionStatusNoEligible
		decision.Reasons = []string{SelectionStatusNoEligible}
		return decision, nil
	}
	decision.SelectedCandidate = &AutomaticSelectionCandidate{
		ServerID: selection.Candidate.ServerID, ServerName: selection.Candidate.ServerName,
		Protocol: selection.Candidate.Protocol, Health: selection.Candidate.Health,
		Priority: selection.Candidate.Priority, Weight: selection.Candidate.Weight,
		Signals: append([]string(nil), selection.Candidate.Signals...),
	}
	decision.Reasons = selection.Reasons
	if selection.Candidate.ServerID == selectionContext.CurrentServerID {
		decision.Status = SelectionStatusCurrent
		return decision, nil
	}
	if currentEligible && selectionContext.Policy.LastSelectedAt != nil {
		blockedUntil := selectionContext.Policy.LastSelectedAt.Add(time.Duration(selectionContext.Policy.CooldownSeconds) * time.Second)
		if now.Before(blockedUntil) {
			decision.Status = SelectionStatusCooldown
			decision.BlockedUntil = &blockedUntil
			return decision, nil
		}
	}
	decision.Status = SelectionStatusSelected
	decision.CanApply = selectionContext.Policy.Enabled && (selectionContext.Status == StatusCreated || selectionContext.Status == StatusActive)
	return decision, nil
}

func (r *Repository) ApplyAutomaticSelection(ctx context.Context, accountID string) (AutomaticSelectionApplyResponse, error) {
	var expectedCurrentServerID string
	if err := r.pool.QueryRow(ctx, `
		SELECT COALESCE(server_id::text, '') FROM vpn_accounts WHERE id = $1::uuid
	`, accountID).Scan(&expectedCurrentServerID); err != nil {
		return AutomaticSelectionApplyResponse{}, err
	}
	tx, err := r.pool.Begin(ctx)
	if err != nil {
		return AutomaticSelectionApplyResponse{}, err
	}
	defer tx.Rollback(ctx)
	selectionContext, err := lockedAutomaticSelectionContext(ctx, tx, accountID)
	if err != nil {
		return AutomaticSelectionApplyResponse{}, err
	}
	if selectionContext.CurrentServerID != expectedCurrentServerID {
		return AutomaticSelectionApplyResponse{}, ErrAutomaticSelectionChanged
	}
	if !selectionContext.Policy.Enabled {
		return AutomaticSelectionApplyResponse{}, ErrAutomaticSelectionDisabled
	}
	if selectionContext.Status != StatusCreated && selectionContext.Status != StatusActive {
		return AutomaticSelectionApplyResponse{}, ErrAutomaticSelectionStatus
	}
	decision, err := evaluateAutomaticSelectionAt(ctx, selectionContext, time.Now().UTC(), nodegroups.NewTransactionalRepository(tx))
	if err != nil {
		return AutomaticSelectionApplyResponse{}, err
	}
	if decision.Status == SelectionStatusCooldown {
		return AutomaticSelectionApplyResponse{}, ErrAutomaticSelectionCooldown
	}
	if decision.Status == SelectionStatusNodeGroupRequired {
		return AutomaticSelectionApplyResponse{}, ErrAutomaticSelectionGroup
	}
	if decision.SelectedCandidate == nil {
		return AutomaticSelectionApplyResponse{}, ErrAutomaticSelectionNoTarget
	}
	response := AutomaticSelectionApplyResponse{
		Decision: decision, PreviousServerID: selectionContext.CurrentServerID,
		SelectedServerID: decision.SelectedCandidate.ServerID,
	}
	if decision.Status == SelectionStatusCurrent {
		response.AffectedServerIDs = []string{decision.SelectedCandidate.ServerID}
		return response, nil
	}
	selectedWireGuardAddress := ""
	if decision.SelectedCandidate.Protocol == "wireguard" {
		selectedWireGuardAddress, err = allocateAutomaticSelectionWireGuardAddress(ctx, tx, decision.SelectedCandidate.ServerID)
		if err != nil {
			return AutomaticSelectionApplyResponse{}, err
		}
	}
	if _, err := tx.Exec(ctx, `
		UPDATE vpn_accounts
		SET server_id = $2::uuid,
			wireguard_address = NULLIF($4, '')::inet,
			updated_at = $3,
			config_updated_at = $3
		WHERE id = $1::uuid
	`, accountID, decision.SelectedCandidate.ServerID, decision.EvaluatedAt, selectedWireGuardAddress); err != nil {
		return AutomaticSelectionApplyResponse{}, err
	}
	if _, err := tx.Exec(ctx, `
		UPDATE vpn_account_automatic_selection_policies
		SET last_selected_at = $2,
			last_selected_server_id = $3::uuid,
			updated_at = $2
		WHERE vpn_account_id = $1::uuid
	`, accountID, decision.EvaluatedAt, decision.SelectedCandidate.ServerID); err != nil {
		return AutomaticSelectionApplyResponse{}, err
	}
	if err := tx.Commit(ctx); err != nil {
		return AutomaticSelectionApplyResponse{}, err
	}

	response.Changed = true
	response.ConfigDeploymentRequired = true
	response.AffectedServerIDs = orderedUniqueServerIDs(decision.SelectedCandidate.ServerID, selectionContext.CurrentServerID)
	return response, nil
}

func allocateAutomaticSelectionWireGuardAddress(ctx context.Context, tx pgx.Tx, serverID string) (string, error) {
	var serverAddress string
	if err := tx.QueryRow(ctx, `
		SELECT wireguard_address::text FROM servers WHERE id = $1::uuid FOR UPDATE
	`, serverID).Scan(&serverAddress); err != nil {
		return "", err
	}
	rows, err := tx.Query(ctx, `
		SELECT wireguard_address::text
		FROM vpn_accounts
		WHERE server_id = $1::uuid AND wireguard_address IS NOT NULL
		ORDER BY wireguard_address
	`, serverID)
	if err != nil {
		return "", err
	}
	defer rows.Close()
	used := make([]string, 0)
	for rows.Next() {
		var address string
		if err := rows.Scan(&address); err != nil {
			return "", err
		}
		used = append(used, address)
	}
	if err := rows.Err(); err != nil {
		return "", err
	}
	return wgcredentials.NextPeerAddress(serverAddress, used)
}

func orderedUniqueServerIDs(values ...string) []string {
	set := make(map[string]struct{}, len(values))
	result := make([]string, 0, len(values))
	for _, value := range values {
		if value == "" {
			continue
		}
		if _, exists := set[value]; exists {
			continue
		}
		set[value] = struct{}{}
		result = append(result, value)
	}
	return result
}

func (h *Handler) UpdateAutomaticSelectionPolicy(w http.ResponseWriter, r *http.Request) {
	var request UpdateAutomaticSelectionPolicyRequest
	if !decodeRoutingPolicyJSON(w, r, &request) {
		return
	}
	if request.CooldownSeconds < 60 || request.CooldownSeconds > 86400 {
		writeInvalidRequest(w, "cooldownSeconds must be between 60 and 86400")
		return
	}
	repository, ok := h.accounts.(routingPolicyRepository)
	if !ok {
		writeRoutingPolicyUnavailable(w)
		return
	}
	policy, err := repository.UpdateAutomaticSelectionPolicy(r.Context(), r.PathValue("id"), request)
	if errors.Is(err, ErrAutomaticSelectionGroup) {
		httpx.WriteJSON(w, http.StatusConflict, httpx.Error("node_group_required", "Assign a node group before enabling automatic selection."))
		return
	}
	if errors.Is(err, pgx.ErrNoRows) {
		writeAccountNotFound(w)
		return
	}
	if err != nil {
		h.databaseError(w, "update automatic selection policy", err)
		return
	}
	h.recordAudit(r, audit.EventInput{
		Action: "vpn_account.automatic_selection_policy_updated", ResourceType: "vpn_account",
		ResourceID: r.PathValue("id"), Result: audit.ResultSuccess,
		Metadata: map[string]any{"enabled": request.Enabled, "allow_degraded": request.AllowDegraded, "cooldown_seconds": request.CooldownSeconds},
	})
	httpx.WriteJSON(w, http.StatusOK, policy)
}

func (h *Handler) PreviewAutomaticSelection(w http.ResponseWriter, r *http.Request) {
	repository, ok := h.accounts.(routingPolicyRepository)
	if !ok {
		writeRoutingPolicyUnavailable(w)
		return
	}
	decision, err := repository.PreviewAutomaticSelection(r.Context(), r.PathValue("id"))
	if errors.Is(err, pgx.ErrNoRows) {
		writeAccountNotFound(w)
		return
	}
	if err != nil {
		h.databaseError(w, "preview automatic selection", err)
		return
	}
	httpx.WriteJSON(w, http.StatusOK, decision)
}

func (h *Handler) ApplyAutomaticSelection(w http.ResponseWriter, r *http.Request) {
	repository, ok := h.accounts.(routingPolicyRepository)
	if !ok {
		writeRoutingPolicyUnavailable(w)
		return
	}
	response, err := repository.ApplyAutomaticSelection(r.Context(), r.PathValue("id"))
	if err != nil {
		switch {
		case errors.Is(err, pgx.ErrNoRows):
			writeAccountNotFound(w)
		case errors.Is(err, ErrAutomaticSelectionDisabled):
			httpx.WriteJSON(w, http.StatusConflict, httpx.Error("automatic_selection_disabled", "Enable automatic selection before applying a decision."))
		case errors.Is(err, ErrAutomaticSelectionCooldown):
			httpx.WriteJSON(w, http.StatusConflict, httpx.Error("automatic_selection_cooldown", "The automatic selection cooldown is still active."))
		case errors.Is(err, ErrAutomaticSelectionNoTarget):
			httpx.WriteJSON(w, http.StatusConflict, httpx.Error("no_eligible_candidates", "No eligible node-group candidate is available."))
		case errors.Is(err, ErrAutomaticSelectionStatus):
			httpx.WriteJSON(w, http.StatusConflict, httpx.Error("account_status_not_selectable", "Only created or active accounts can be selected automatically."))
		case errors.Is(err, ErrAutomaticSelectionChanged):
			httpx.WriteJSON(w, http.StatusConflict, httpx.Error("account_assignment_changed", "The account assignment changed; preview the decision again."))
		case errors.Is(err, ErrAutomaticSelectionGroup):
			httpx.WriteJSON(w, http.StatusConflict, httpx.Error("node_group_required", "Assign a node group before applying automatic selection."))
		default:
			h.databaseError(w, "apply automatic selection", err)
		}
		return
	}
	h.recordAudit(r, audit.EventInput{
		Action: "vpn_account.automatic_selection_applied", ResourceType: "vpn_account",
		ResourceID: r.PathValue("id"), Result: audit.ResultSuccess,
		Metadata: map[string]any{
			"previous_server_id": response.PreviousServerID, "selected_server_id": response.SelectedServerID,
			"changed": response.Changed, "strategy": response.Decision.SelectionStrategy,
			"reasons": response.Decision.Reasons, "affected_server_ids": response.AffectedServerIDs,
		},
	})
	httpx.WriteJSON(w, http.StatusOK, response)
}
