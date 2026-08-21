package nodegroups

import (
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/ikaevus/routegate/backend/internal/httpx"
)

type nodeGroupRepository interface {
	List(context.Context) ([]NodeGroup, error)
	Get(context.Context, string) (NodeGroup, error)
	Create(context.Context, CreateNodeGroupInput) (NodeGroup, error)
	Update(context.Context, string, UpdateNodeGroupInput) (NodeGroup, error)
	Delete(context.Context, string) error
	UpsertMember(context.Context, UpsertNodeGroupMemberInput) (NodeGroupMember, error)
	DeleteMember(context.Context, string, string) error
	Candidates(context.Context, string, time.Time) (ListNodeGroupCandidatesResponse, error)
}

type Handler struct {
	logger *slog.Logger
	groups nodeGroupRepository
	now    func() time.Time
}

func NewHandler(logger *slog.Logger, pool *pgxpool.Pool) *Handler {
	return &Handler{logger: logger, groups: NewRepository(pool), now: time.Now}
}

func (h *Handler) List(w http.ResponseWriter, r *http.Request) {
	items, err := h.groups.List(r.Context())
	if err != nil {
		h.databaseError(w, "list node groups", err)
		return
	}
	httpx.WriteJSON(w, http.StatusOK, ListNodeGroupsResponse{Items: items})
}

func (h *Handler) Get(w http.ResponseWriter, r *http.Request) {
	group, err := h.groups.Get(r.Context(), r.PathValue("group_id"))
	if errors.Is(err, pgx.ErrNoRows) {
		writeNotFound(w)
		return
	}
	if err != nil {
		h.databaseError(w, "get node group", err)
		return
	}
	httpx.WriteJSON(w, http.StatusOK, group)
}

func (h *Handler) Create(w http.ResponseWriter, r *http.Request) {
	var request CreateNodeGroupRequest
	if !decodeJSON(w, r, &request) {
		return
	}
	input := CreateNodeGroupInput{
		Name: strings.TrimSpace(request.Name),
		Description: strings.TrimSpace(request.Description),
		SelectionStrategy: strings.ToLower(strings.TrimSpace(request.SelectionStrategy)),
	}
	if input.SelectionStrategy == "" {
		input.SelectionStrategy = SelectionStrategyPriority
	}
	if err := validateCreate(input); err != nil {
		writeInvalid(w, err.Error())
		return
	}
	group, err := h.groups.Create(r.Context(), input)
	if errors.Is(err, ErrNodeGroupNameExists) {
		writeNameConflict(w)
		return
	}
	if err != nil {
		h.databaseError(w, "create node group", err)
		return
	}
	httpx.WriteJSON(w, http.StatusCreated, group)
}

func (h *Handler) Update(w http.ResponseWriter, r *http.Request) {
	var request UpdateNodeGroupRequest
	if !decodeJSON(w, r, &request) {
		return
	}
	trimPointer(request.Name)
	trimPointer(request.Description)
	trimLowerPointer(request.SelectionStrategy)
	input := UpdateNodeGroupInput{
		Name: request.Name,
		Description: request.Description,
		SelectionStrategy: request.SelectionStrategy,
	}
	if err := validateUpdate(input); err != nil {
		writeInvalid(w, err.Error())
		return
	}
	group, err := h.groups.Update(r.Context(), r.PathValue("group_id"), input)
	if errors.Is(err, pgx.ErrNoRows) {
		writeNotFound(w)
		return
	}
	if errors.Is(err, ErrNodeGroupNameExists) {
		writeNameConflict(w)
		return
	}
	if err != nil {
		h.databaseError(w, "update node group", err)
		return
	}
	httpx.WriteJSON(w, http.StatusOK, group)
}

func (h *Handler) Delete(w http.ResponseWriter, r *http.Request) {
	err := h.groups.Delete(r.Context(), r.PathValue("group_id"))
	if errors.Is(err, pgx.ErrNoRows) {
		writeNotFound(w)
		return
	}
	if errors.Is(err, ErrNodeGroupAssigned) {
		httpx.WriteJSON(w, http.StatusConflict, httpx.Error("node_group_assigned", "Node group is assigned to one or more VPN accounts."))
		return
	}
	if err != nil {
		h.databaseError(w, "delete node group", err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (h *Handler) PutMember(w http.ResponseWriter, r *http.Request) {
	var request UpsertNodeGroupMemberRequest
	if !decodeJSON(w, r, &request) {
		return
	}
	enabled := true
	if request.Enabled != nil {
		enabled = *request.Enabled
	}
	priority := 100
	if request.Priority != nil {
		priority = *request.Priority
	}
	weight := 100
	if request.Weight != nil {
		weight = *request.Weight
	}
	input := UpsertNodeGroupMemberInput{
		NodeGroupID: r.PathValue("group_id"),
		ServerID: r.PathValue("server_id"),
		Priority: priority,
		Weight: weight,
		Enabled: enabled,
	}
	if err := validateMember(input); err != nil {
		writeInvalid(w, err.Error())
		return
	}
	member, err := h.groups.UpsertMember(r.Context(), input)
	if errors.Is(err, pgx.ErrNoRows) {
		httpx.WriteJSON(w, http.StatusNotFound, httpx.Error("node_or_group_not_found", "Node group or server not found."))
		return
	}
	if errors.Is(err, ErrServerNotVPNNode) {
		writeInvalid(w, "Only VPN and Hybrid nodes can be added to a node group.")
		return
	}
	if err != nil {
		h.databaseError(w, "upsert node group member", err)
		return
	}
	httpx.WriteJSON(w, http.StatusOK, member)
}

func (h *Handler) DeleteMember(w http.ResponseWriter, r *http.Request) {
	err := h.groups.DeleteMember(r.Context(), r.PathValue("group_id"), r.PathValue("server_id"))
	if errors.Is(err, pgx.ErrNoRows) {
		httpx.WriteJSON(w, http.StatusNotFound, httpx.Error("node_group_member_not_found", "Node group member not found."))
		return
	}
	if err != nil {
		h.databaseError(w, "delete node group member", err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (h *Handler) Candidates(w http.ResponseWriter, r *http.Request) {
	response, err := h.groups.Candidates(r.Context(), r.PathValue("group_id"), h.now().UTC())
	if errors.Is(err, pgx.ErrNoRows) {
		writeNotFound(w)
		return
	}
	if err != nil {
		h.databaseError(w, "list node group candidates", err)
		return
	}
	httpx.WriteJSON(w, http.StatusOK, response)
}

func validateCreate(input CreateNodeGroupInput) error {
	if len(input.Name) < 1 || len(input.Name) > 120 {
		return errors.New("name must contain between 1 and 120 characters")
	}
	if len(input.Description) > 500 {
		return errors.New("description must not exceed 500 characters")
	}
	if !validSelectionStrategy(input.SelectionStrategy) {
		return errors.New("selectionStrategy must be priority or weighted")
	}
	return nil
}

func validateUpdate(input UpdateNodeGroupInput) error {
	if input.Name == nil && input.Description == nil && input.SelectionStrategy == nil {
		return errors.New("at least one field is required")
	}
	if input.Name != nil && (len(*input.Name) < 1 || len(*input.Name) > 120) {
		return errors.New("name must contain between 1 and 120 characters")
	}
	if input.Description != nil && len(*input.Description) > 500 {
		return errors.New("description must not exceed 500 characters")
	}
	if input.SelectionStrategy != nil && !validSelectionStrategy(*input.SelectionStrategy) {
		return errors.New("selectionStrategy must be priority or weighted")
	}
	return nil
}

func validateMember(input UpsertNodeGroupMemberInput) error {
	if input.NodeGroupID == "" || input.ServerID == "" {
		return errors.New("node group id and server id are required")
	}
	if input.Priority < 0 || input.Priority > 10000 {
		return errors.New("priority must be between 0 and 10000")
	}
	if input.Weight < 1 || input.Weight > 1000 {
		return errors.New("weight must be between 1 and 1000")
	}
	return nil
}

func decodeJSON(w http.ResponseWriter, r *http.Request, target any) bool {
	decoder := json.NewDecoder(http.MaxBytesReader(w, r.Body, 64<<10))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(target); err != nil {
		writeInvalid(w, "Request body must be valid JSON.")
		return false
	}
	return true
}

func trimPointer(value *string) {
	if value != nil {
		*value = strings.TrimSpace(*value)
	}
}

func trimLowerPointer(value *string) {
	if value != nil {
		*value = strings.ToLower(strings.TrimSpace(*value))
	}
}

func writeInvalid(w http.ResponseWriter, message string) {
	httpx.WriteJSON(w, http.StatusBadRequest, httpx.Error("invalid_request", message))
}

func writeNotFound(w http.ResponseWriter) {
	httpx.WriteJSON(w, http.StatusNotFound, httpx.Error("node_group_not_found", "Node group not found."))
}

func writeNameConflict(w http.ResponseWriter) {
	httpx.WriteJSON(w, http.StatusConflict, httpx.Error("node_group_name_exists", "Node group name already exists."))
}

func (h *Handler) databaseError(w http.ResponseWriter, operation string, err error) {
	h.logger.Error(operation+" failed", "error", err)
	httpx.WriteJSON(w, http.StatusInternalServerError, httpx.Error("database_error", "Database operation failed."))
}
