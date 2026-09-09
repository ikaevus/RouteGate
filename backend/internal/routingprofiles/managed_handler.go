package routingprofiles

import (
	"encoding/json"
	"errors"
	"net/http"

	"github.com/ikaevus/routegate/backend/internal/httpx"
	"github.com/jackc/pgx/v5"
)

func (h *Handler) managedRepository() *Repository { return h.profiles.(*Repository) }
func (h *Handler) Providers(w http.ResponseWriter, r *http.Request) {
	httpx.WriteJSON(w, http.StatusOK, map[string]any{"items": SourceProviders})
}
func (h *Handler) CreateManaged(w http.ResponseWriter, r *http.Request) {
	input, ok := decodeManaged(w, r)
	if !ok {
		return
	}
	s, err := h.managedRepository().CreateManagedSet(r.Context(), r.PathValue("profile_id"), input)
	if h.managedError(w, err) {
		return
	}
	httpx.WriteJSON(w, http.StatusCreated, s)
}
func (h *Handler) UpdateManaged(w http.ResponseWriter, r *http.Request) {
	input, ok := decodeManaged(w, r)
	if !ok {
		return
	}
	s, err := h.managedRepository().UpdateManagedSet(r.Context(), r.PathValue("profile_id"), r.PathValue("set_id"), input)
	if h.managedError(w, err) {
		return
	}
	httpx.WriteJSON(w, http.StatusOK, s)
}
func (h *Handler) DeleteManaged(w http.ResponseWriter, r *http.Request) {
	if h.managedError(w, h.managedRepository().DeleteManagedSet(r.Context(), r.PathValue("profile_id"), r.PathValue("set_id"))) {
		return
	}
	w.WriteHeader(http.StatusNoContent)
}
func (h *Handler) RefreshManaged(w http.ResponseWriter, r *http.Request) {
	s, err := h.managedRepository().RefreshManagedSet(r.Context(), r.PathValue("profile_id"), r.PathValue("set_id"), NewSourceFetcher())
	if h.managedError(w, err) {
		return
	}
	// A completed attempt returns the persisted health, including lastError on failure.
	httpx.WriteJSON(w, http.StatusOK, s)
}
func decodeManaged(w http.ResponseWriter, r *http.Request) (ManagedSetInput, bool) {
	var input ManagedSetInput
	dec := json.NewDecoder(http.MaxBytesReader(w, r.Body, 16<<10))
	dec.DisallowUnknownFields()
	if err := dec.Decode(&input); err != nil {
		writeInvalidRequest(w, "Invalid managed rule set request.")
		return input, false
	}
	if err := input.validate(); err != nil {
		writeInvalidRequest(w, err.Error())
		return input, false
	}
	return input, true
}
func (h *Handler) managedError(w http.ResponseWriter, err error) bool {
	if err == nil {
		return false
	}
	if errors.Is(err, ErrManagedSetLimit) {
		writeInvalidRequest(w, err.Error())
		return true
	}
	if pgErr, ok := postgresError(err); ok && pgErr.Code == "22P02" {
		writeInvalidRequest(w, "Invalid profile or managed set ID.")
		return true
	}
	if errors.Is(err, pgx.ErrNoRows) {
		httpx.WriteJSON(w, http.StatusNotFound, httpx.Error("not_found", "Profile or managed set not found, source identity changed, or refresh already running."))
		return true
	}
	h.databaseError(w, "managed rule set", err)
	return true
}
func (h *Handler) Diagnose(w http.ResponseWriter, r *http.Request) {
	var input struct {
		Destination string `json:"destination"`
		ResolvedIP  string `json:"resolvedIp"`
	}
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 4096)).Decode(&input); err != nil {
		writeInvalidRequest(w, "Invalid diagnostic request.")
		return
	}
	profile, err := h.profiles.GetProfile(r.Context(), r.PathValue("profile_id"))
	if h.managedError(w, err) {
		return
	}
	policy, err := CompilePolicy(profile)
	if err != nil {
		httpx.WriteJSON(w, http.StatusConflict, httpx.Error("routing_policy_unavailable", err.Error()))
		return
	}
	result, err := policy.Diagnose(input.Destination, input.ResolvedIP)
	if err != nil {
		writeInvalidRequest(w, err.Error())
		return
	}
	httpx.WriteJSON(w, http.StatusOK, result)
}
