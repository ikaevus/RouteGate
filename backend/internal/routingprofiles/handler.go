package routingprofiles

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

type routingProfileRepository interface {
	ListProfiles(context.Context) ([]RoutingProfile, error)
	GetProfile(context.Context, string) (RoutingProfile, error)
	CreateProfile(context.Context, CreateRoutingProfileInput) (RoutingProfile, error)
	UpdateProfile(context.Context, string, UpdateRoutingProfileInput) (RoutingProfile, error)
	DeleteProfile(context.Context, string) error
	CreateRule(context.Context, CreateRoutingProfileRuleInput) (RoutingProfileRule, error)
	UpdateRule(context.Context, string, string, UpdateRoutingProfileRuleInput) (RoutingProfileRule, error)
	DeleteRule(context.Context, string, string) error
}

type Handler struct {
	logger   *slog.Logger
	profiles routingProfileRepository
}

func NewHandler(logger *slog.Logger, pool *pgxpool.Pool) *Handler {
	return &Handler{
		logger:   logger,
		profiles: NewRepository(pool),
	}
}

func (h *Handler) List(w http.ResponseWriter, r *http.Request) {
	items, err := h.profiles.ListProfiles(r.Context())
	if err != nil {
		h.databaseError(w, "list routing profiles", err)
		return
	}

	httpx.WriteJSON(w, http.StatusOK, ListRoutingProfilesResponse{Items: items})
}

func (h *Handler) Get(w http.ResponseWriter, r *http.Request) {
	profile, err := h.profiles.GetProfile(r.Context(), r.PathValue("profile_id"))
	if errors.Is(err, pgx.ErrNoRows) {
		writeProfileNotFound(w)
		return
	}
	if err != nil {
		h.databaseError(w, "get routing profile", err)
		return
	}

	httpx.WriteJSON(w, http.StatusOK, profile)
}

func (h *Handler) Create(w http.ResponseWriter, r *http.Request) {
	var request CreateRoutingProfileRequest
	if err := json.NewDecoder(r.Body).Decode(&request); err != nil {
		writeInvalidRequest(w, "Request body must be valid JSON.")
		return
	}

	input := CreateRoutingProfileInput{
		Name:        strings.TrimSpace(request.Name),
		Description: strings.TrimSpace(request.Description),
		IsDefault:   request.IsDefault,
	}
	if err := validateCreateProfileInput(input); err != nil {
		writeInvalidRequest(w, err.Error())
		return
	}

	profile, err := h.profiles.CreateProfile(r.Context(), input)
	if errors.Is(err, ErrRoutingProfileNameAlreadyExists) {
		writeProfileNameConflict(w)
		return
	}
	if err != nil {
		h.databaseError(w, "create routing profile", err)
		return
	}

	httpx.WriteJSON(w, http.StatusCreated, profile)
}

func (h *Handler) Update(w http.ResponseWriter, r *http.Request) {
	var request UpdateRoutingProfileRequest
	if err := json.NewDecoder(r.Body).Decode(&request); err != nil {
		writeInvalidRequest(w, "Request body must be valid JSON.")
		return
	}
	trimStringPointer(request.Name)
	trimStringPointer(request.Description)

	input := UpdateRoutingProfileInput{
		Name:        request.Name,
		Description: request.Description,
		IsDefault:   request.IsDefault,
	}
	if err := validateUpdateProfileInput(input); err != nil {
		writeInvalidRequest(w, err.Error())
		return
	}

	profile, err := h.profiles.UpdateProfile(r.Context(), r.PathValue("profile_id"), input)
	if errors.Is(err, pgx.ErrNoRows) {
		writeProfileNotFound(w)
		return
	}
	if errors.Is(err, ErrRoutingProfileNameAlreadyExists) {
		writeProfileNameConflict(w)
		return
	}
	if err != nil {
		h.databaseError(w, "update routing profile", err)
		return
	}

	httpx.WriteJSON(w, http.StatusOK, profile)
}

func (h *Handler) Delete(w http.ResponseWriter, r *http.Request) {
	err := h.profiles.DeleteProfile(r.Context(), r.PathValue("profile_id"))
	if errors.Is(err, pgx.ErrNoRows) {
		writeProfileNotFound(w)
		return
	}
	if errors.Is(err, ErrDefaultProfileCannotBeDeleted) {
		httpx.WriteJSON(w, http.StatusBadRequest, httpx.Error("default_profile", "Default routing profile cannot be deleted."))
		return
	}
	if errors.Is(err, ErrRoutingProfileAssigned) {
		httpx.WriteJSON(w, http.StatusConflict, httpx.Error("routing_profile_assigned", "Routing profile is assigned to one or more servers or VPN accounts and cannot be deleted."))
		return
	}
	if err != nil {
		h.databaseError(w, "delete routing profile", err)
		return
	}

	w.WriteHeader(http.StatusNoContent)
}

func (h *Handler) CreateRule(w http.ResponseWriter, r *http.Request) {
	var request CreateRoutingProfileRuleRequest
	if err := json.NewDecoder(r.Body).Decode(&request); err != nil {
		writeInvalidRequest(w, "Request body must be valid JSON.")
		return
	}

	input := CreateRoutingProfileRuleInput{
		RoutingProfileID: r.PathValue("profile_id"),
		Name:             strings.TrimSpace(request.Name),
		Priority:         request.Priority,
		Action:           strings.TrimSpace(request.Action),
		Domains:          cleanStrings(request.Domains),
		DomainSuffixes:   cleanStrings(request.DomainSuffixes),
		DomainKeywords:   cleanStrings(request.DomainKeywords),
		IPCIDRs:          cleanStrings(request.IPCIDRs),
		GeoSites:         cleanStrings(request.GeoSites),
		GeoIPs:           cleanStrings(request.GeoIPs),
		Enabled:          true,
	}
	if request.Enabled != nil {
		input.Enabled = *request.Enabled
	}
	if input.Priority == 0 {
		input.Priority = 1000
	}
	if err := validateCreateRuleInput(input); err != nil {
		writeInvalidRequest(w, err.Error())
		return
	}

	rule, err := h.profiles.CreateRule(r.Context(), input)
	if errors.Is(err, pgx.ErrNoRows) {
		writeProfileNotFound(w)
		return
	}
	if errors.Is(err, ErrRoutingProfileRuleInvalid) {
		writeInvalidRequest(w, "routing profile rule is invalid")
		return
	}
	if err != nil {
		h.databaseError(w, "create routing profile rule", err)
		return
	}

	httpx.WriteJSON(w, http.StatusCreated, rule)
}

func (h *Handler) UpdateRule(w http.ResponseWriter, r *http.Request) {
	var request UpdateRoutingProfileRuleRequest
	if err := json.NewDecoder(r.Body).Decode(&request); err != nil {
		writeInvalidRequest(w, "Request body must be valid JSON.")
		return
	}
	trimStringPointer(request.Name)
	trimStringPointer(request.Action)
	cleanStringSlicePointer(request.Domains)
	cleanStringSlicePointer(request.DomainSuffixes)
	cleanStringSlicePointer(request.DomainKeywords)
	cleanStringSlicePointer(request.IPCIDRs)
	cleanStringSlicePointer(request.GeoSites)
	cleanStringSlicePointer(request.GeoIPs)

	input := UpdateRoutingProfileRuleInput{
		Name:           request.Name,
		Priority:       request.Priority,
		Action:         request.Action,
		Domains:        request.Domains,
		DomainSuffixes: request.DomainSuffixes,
		DomainKeywords: request.DomainKeywords,
		IPCIDRs:        request.IPCIDRs,
		GeoSites:       request.GeoSites,
		GeoIPs:         request.GeoIPs,
		Enabled:        request.Enabled,
	}
	if err := validateUpdateRuleInput(input); err != nil {
		writeInvalidRequest(w, err.Error())
		return
	}

	rule, err := h.profiles.UpdateRule(r.Context(), r.PathValue("profile_id"), r.PathValue("rule_id"), input)
	if errors.Is(err, pgx.ErrNoRows) {
		writeRuleNotFound(w)
		return
	}
	if errors.Is(err, ErrRoutingProfileRuleInvalid) {
		writeInvalidRequest(w, "routing profile rule is invalid")
		return
	}
	if err != nil {
		h.databaseError(w, "update routing profile rule", err)
		return
	}

	httpx.WriteJSON(w, http.StatusOK, rule)
}

func (h *Handler) DeleteRule(w http.ResponseWriter, r *http.Request) {
	err := h.profiles.DeleteRule(r.Context(), r.PathValue("profile_id"), r.PathValue("rule_id"))
	if errors.Is(err, pgx.ErrNoRows) {
		writeRuleNotFound(w)
		return
	}
	if err != nil {
		h.databaseError(w, "delete routing profile rule", err)
		return
	}

	w.WriteHeader(http.StatusNoContent)
}

func writeInvalidRequest(w http.ResponseWriter, message string) {
	httpx.WriteJSON(w, http.StatusBadRequest, httpx.Error("invalid_request", message))
}

func writeProfileNotFound(w http.ResponseWriter) {
	httpx.WriteJSON(w, http.StatusNotFound, httpx.Error("routing_profile_not_found", "Routing profile not found."))
}

func writeRuleNotFound(w http.ResponseWriter) {
	httpx.WriteJSON(w, http.StatusNotFound, httpx.Error("routing_rule_not_found", "Routing profile rule not found."))
}

func writeProfileNameConflict(w http.ResponseWriter) {
	httpx.WriteJSON(w, http.StatusConflict, httpx.Error("routing_profile_name_exists", "Routing profile name already exists."))
}

func (h *Handler) databaseError(w http.ResponseWriter, operation string, err error) {
	h.logger.Error(operation+" failed", "error", err)
	httpx.WriteJSON(w, http.StatusInternalServerError, httpx.Error("database_error", "Database operation failed."))
}
