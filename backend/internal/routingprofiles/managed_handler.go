package routingprofiles

import (
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/netip"
	"net/url"
	"strings"

	"github.com/jackc/pgx/v5"

	"github.com/ikaevus/routegate/backend/internal/httpx"
	"github.com/ikaevus/routegate/backend/internal/routingpolicy"
)

func (h *Handler) PublicManagedRuleSet(w http.ResponseWriter, r *http.Request) {
	snapshot, err := h.repository.GetPublicManagedRuleSetSnapshot(r.Context(), r.PathValue("set_id"))
	if errors.Is(err, pgx.ErrNoRows) {
		http.NotFound(w, r)
		return
	}
	if err != nil {
		h.logger.Error("get public managed routing rule set", "error", err)
		http.Error(w, "Rule set temporarily unavailable.", http.StatusServiceUnavailable)
		return
	}
	hash := sha256.Sum256(snapshot)
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.Header().Set("Cache-Control", "public, max-age=3600")
	w.Header().Set("ETag", fmt.Sprintf("\"%x\"", hash[:]))
	w.Header().Set("X-Content-Type-Options", "nosniff")
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write(snapshot)
}

func (h *Handler) ListManagedRuleSets(w http.ResponseWriter, r *http.Request) {
	items, err := h.repository.ListManagedRuleSets(r.Context(), r.URL.Query().Get("profileId"))
	if err != nil {
		h.databaseError(w, "list managed routing rule sets", err)
		return
	}
	httpx.WriteJSON(w, http.StatusOK, ListManagedRuleSetsResponse{Items: items})
}

func (h *Handler) CreateManagedRuleSet(w http.ResponseWriter, r *http.Request) {
	var request CreateManagedRuleSetRequest
	if err := json.NewDecoder(r.Body).Decode(&request); err != nil {
		writeInvalidRequest(w, "Request body must be valid JSON.")
		return
	}
	request.Name = strings.TrimSpace(request.Name)
	request.Provider = strings.ToLower(strings.TrimSpace(request.Provider))
	request.SourceURL = strings.TrimSpace(request.SourceURL)
	if request.Provider == "" {
		request.Provider = "custom"
	}
	if request.Action == "" {
		request.Action = ActionVPN
	}
	if request.Priority == 0 {
		request.Priority = 1000
	}
	if request.RefreshIntervalHours == 0 {
		request.RefreshIntervalHours = 24
	}
	if err := validateManagedRuleSet(request.Name, request.Provider, request.SourceURL, request.Priority, request.Action, request.RefreshIntervalHours); err != nil {
		writeInvalidRequest(w, err.Error())
		return
	}
	item, err := h.repository.CreateManagedRuleSet(r.Context(), r.PathValue("profile_id"), request)
	if errors.Is(err, pgx.ErrNoRows) {
		writeProfileNotFound(w)
		return
	}
	if err != nil {
		h.databaseError(w, "create managed routing rule set", err)
		return
	}
	httpx.WriteJSON(w, http.StatusCreated, item)
}

func (h *Handler) UpdateManagedRuleSet(w http.ResponseWriter, r *http.Request) {
	var request UpdateManagedRuleSetRequest
	if err := json.NewDecoder(r.Body).Decode(&request); err != nil {
		writeInvalidRequest(w, "Request body must be valid JSON.")
		return
	}
	trimStringPointer(request.Name)
	trimStringPointer(request.Provider)
	trimStringPointer(request.SourceURL)
	trimStringPointer(request.Action)
	current, err := h.repository.GetManagedRuleSet(r.Context(), r.PathValue("set_id"))
	if errors.Is(err, pgx.ErrNoRows) {
		httpx.WriteJSON(w, http.StatusNotFound, httpx.Error("managed_rule_set_not_found", "Managed rule set not found."))
		return
	}
	if err != nil {
		h.databaseError(w, "get managed routing rule set", err)
		return
	}
	if request.SourceURL != nil && *request.SourceURL != current.SourceURL {
		writeInvalidRequest(w, "sourceUrl cannot be changed in place; create and validate a new managed rule set first")
		return
	}
	merged := CreateManagedRuleSetRequest{Name: current.Name, Provider: current.Provider, SourceURL: current.SourceURL, Priority: current.Priority, Action: current.Action, Enabled: current.Enabled, RefreshIntervalHours: current.RefreshIntervalHours}
	if request.Name != nil {
		merged.Name = *request.Name
	}
	if request.Provider != nil {
		merged.Provider = strings.ToLower(*request.Provider)
	}
	if request.SourceURL != nil {
		merged.SourceURL = *request.SourceURL
	}
	if request.Priority != nil {
		merged.Priority = *request.Priority
	}
	if request.Action != nil {
		merged.Action = *request.Action
	}
	if request.RefreshIntervalHours != nil {
		merged.RefreshIntervalHours = *request.RefreshIntervalHours
	}
	if err := validateManagedRuleSet(merged.Name, merged.Provider, merged.SourceURL, merged.Priority, merged.Action, merged.RefreshIntervalHours); err != nil {
		writeInvalidRequest(w, err.Error())
		return
	}
	item, err := h.repository.UpdateManagedRuleSet(r.Context(), current.ID, request)
	if err != nil {
		h.databaseError(w, "update managed routing rule set", err)
		return
	}
	httpx.WriteJSON(w, http.StatusOK, item)
}

func (h *Handler) DeleteManagedRuleSet(w http.ResponseWriter, r *http.Request) {
	err := h.repository.DeleteManagedRuleSet(r.Context(), r.PathValue("set_id"))
	if errors.Is(err, pgx.ErrNoRows) {
		httpx.WriteJSON(w, http.StatusNotFound, httpx.Error("managed_rule_set_not_found", "Managed rule set not found."))
		return
	}
	if err != nil {
		h.databaseError(w, "delete managed routing rule set", err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (h *Handler) RefreshManagedRuleSet(w http.ResponseWriter, r *http.Request) {
	item, err := h.refresher.Refresh(r.Context(), r.PathValue("set_id"))
	if errors.Is(err, pgx.ErrNoRows) {
		httpx.WriteJSON(w, http.StatusNotFound, httpx.Error("managed_rule_set_not_found", "Managed rule set not found."))
		return
	}
	if err != nil {
		h.logger.Warn("managed rule set refresh failed", "id", r.PathValue("set_id"), "error", err)
		httpx.WriteJSON(w, http.StatusBadGateway, httpx.Error("managed_rule_set_refresh_failed", err.Error()))
		return
	}
	httpx.WriteJSON(w, http.StatusOK, item)
}

func (h *Handler) Diagnose(w http.ResponseWriter, r *http.Request) {
	var request RoutingDiagnosticRequest
	if err := json.NewDecoder(r.Body).Decode(&request); err != nil {
		writeInvalidRequest(w, "Request body must be valid JSON.")
		return
	}
	target := strings.ToLower(strings.TrimSpace(strings.TrimSuffix(request.Target, ".")))
	if !validDiagnosticTarget(target) {
		writeInvalidRequest(w, "target must be a valid domain or IP address")
		return
	}
	profile, err := h.repository.GetProfile(r.Context(), r.PathValue("profile_id"))
	if errors.Is(err, pgx.ErrNoRows) {
		writeProfileNotFound(w)
		return
	}
	if err != nil {
		h.databaseError(w, "get routing profile for diagnostics", err)
		return
	}
	managed, err := h.repository.ListManagedRuleSetRecordsForProfile(r.Context(), profile.ID)
	if err != nil {
		h.databaseError(w, "list managed routing rule sets for diagnostics", err)
		return
	}
	candidates := diagnosticCandidates(profile, managed)
	decision := routingpolicy.Resolve(target, profile.DefaultAction, candidates)
	response := RoutingDiagnosticResponse{Target: target, ProfileID: profile.ID, ProfileName: profile.Name, Action: decision.Action, Source: "profile_default", Precedence: "Lower priority wins; manual rules win ties with managed rule sets."}
	if decision.Candidate != nil {
		response.Matched = true
		response.MatchedID = decision.Candidate.ID
		response.MatchedName = decision.Candidate.Name
		response.Source = decision.Candidate.Source
		priority := decision.Candidate.Priority
		response.Priority = &priority
	}
	httpx.WriteJSON(w, http.StatusOK, response)
}

func diagnosticCandidates(profile RoutingProfile, managed []managedRuleSetRecord) []routingpolicy.Candidate {
	candidates := make([]routingpolicy.Candidate, 0, len(profile.Rules)+len(managed))
	for _, rule := range profile.Rules {
		if !rule.Enabled {
			continue
		}
		candidates = append(candidates, routingpolicy.Candidate{ID: rule.ID, Name: rule.Name, Priority: rule.Priority, Action: rule.Action, Source: routingpolicy.SourceManual, Matchers: routingpolicy.Matchers{Domains: rule.Domains, DomainSuffixes: rule.DomainSuffixes, DomainKeywords: rule.DomainKeywords, IPCIDRs: rule.IPCIDRs}})
	}
	for _, set := range managed {
		var snapshot ManagedRuleSetSnapshot
		if len(set.Snapshot) == 0 || json.Unmarshal(set.Snapshot, &snapshot) != nil {
			continue
		}
		for _, rule := range snapshot.Rules {
			candidates = append(candidates, routingpolicy.Candidate{ID: set.ID, Name: set.Name, Priority: set.Priority, Action: set.Action, Source: routingpolicy.SourceManaged, Matchers: routingpolicy.Matchers{Domains: rule.Domains, DomainSuffixes: rule.DomainSuffixes, DomainKeywords: rule.DomainKeywords, IPCIDRs: rule.IPCIDRs}})
		}
	}
	return candidates
}

func validateManagedRuleSet(name, provider, sourceURL string, priority int, action string, interval int) error {
	if err := validateRuleName(name); err != nil {
		return err
	}
	if provider == "" || len(provider) > 80 {
		return errors.New("provider is required and must be 80 characters or fewer")
	}
	parsed, err := url.Parse(sourceURL)
	if err != nil || parsed.Host == "" || (parsed.Scheme != "https" && parsed.Scheme != "http") || parsed.User != nil {
		return errors.New("sourceUrl must be an HTTP or HTTPS URL without credentials")
	}
	if err := validateRulePriority(priority); err != nil {
		return err
	}
	if !ValidAction(action) {
		return errors.New("action must be one of: direct, vpn, block")
	}
	if interval < 1 || interval > 720 {
		return errors.New("refreshIntervalHours must be between 1 and 720")
	}
	return nil
}

func validDiagnosticTarget(target string) bool {
	if _, err := netip.ParseAddr(target); err == nil {
		return true
	}
	return validateRuleMatchers([]string{target}, nil, nil, nil, nil, nil) == nil
}
