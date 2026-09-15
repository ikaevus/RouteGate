package maintenance

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"regexp"
	"strings"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/ikaevus/routegate/backend/internal/audit"
	"github.com/ikaevus/routegate/backend/internal/auth"
	"github.com/ikaevus/routegate/backend/internal/httpx"
)

const maintenanceOperationTimeout = 3 * time.Minute

var canonicalPlanID = regexp.MustCompile(`^[0-9a-fA-F]{8}-[0-9a-fA-F]{4}-[1-5][0-9a-fA-F]{3}-[89aAbB][0-9a-fA-F]{3}-[0-9a-fA-F]{12}$`)

type maintenanceService interface {
	Inventory(context.Context) InventoryResponse
	CreatePlan(context.Context, string, CreatePlanRequest) (CreatePlanResponse, error)
	GetPlan(context.Context, string) (Plan, error)
	Execute(context.Context, string, string, string) (Plan, error)
}

type Handler struct {
	logger  *slog.Logger
	service maintenanceService
}

func NewHandler(logger *slog.Logger, pool *pgxpool.Pool) *Handler {
	return &Handler{logger: logger, service: NewService(pool, audit.NewRecorder(logger, pool))}
}

func newHandler(logger *slog.Logger, service maintenanceService) *Handler {
	return &Handler{logger: logger, service: service}
}

func (h *Handler) Inventory(w http.ResponseWriter, r *http.Request) {
	ctx, cancel := context.WithTimeout(r.Context(), 15*time.Second)
	defer cancel()
	httpx.WriteJSON(w, http.StatusOK, h.service.Inventory(ctx))
}

func (h *Handler) CreatePlan(w http.ResponseWriter, r *http.Request) {
	user, ok := auth.UserFromContext(r.Context())
	if !ok {
		httpx.WriteJSON(w, http.StatusUnauthorized, httpx.Error("unauthorized", "Authentication is required."))
		return
	}
	var request CreatePlanRequest
	if err := decodeBody(r, &request); err != nil {
		httpx.WriteJSON(w, http.StatusBadRequest, httpx.Error("invalid_maintenance_request", "Maintenance request is invalid."))
		return
	}
	ctx, cancel := context.WithTimeout(context.WithoutCancel(r.Context()), 75*time.Second)
	defer cancel()
	response, err := h.service.CreatePlan(ctx, user.ID, request)
	if err != nil {
		h.writeError(w, err)
		return
	}
	response.Plan = publicPlan(response.Plan)
	httpx.WriteJSON(w, http.StatusCreated, response)
}

func (h *Handler) GetPlan(w http.ResponseWriter, r *http.Request) {
	id := strings.TrimSpace(r.PathValue("plan_id"))
	if !canonicalPlanID.MatchString(id) {
		httpx.WriteJSON(w, http.StatusBadRequest, httpx.Error("invalid_maintenance_plan_id", "Maintenance plan ID is invalid."))
		return
	}
	plan, err := h.service.GetPlan(r.Context(), id)
	if err != nil {
		h.writeError(w, err)
		return
	}
	httpx.WriteJSON(w, http.StatusOK, publicPlan(plan))
}

func (h *Handler) Execute(w http.ResponseWriter, r *http.Request) {
	user, ok := auth.UserFromContext(r.Context())
	if !ok {
		httpx.WriteJSON(w, http.StatusUnauthorized, httpx.Error("unauthorized", "Authentication is required."))
		return
	}
	id := strings.TrimSpace(r.PathValue("plan_id"))
	if !canonicalPlanID.MatchString(id) {
		httpx.WriteJSON(w, http.StatusBadRequest, httpx.Error("invalid_maintenance_plan_id", "Maintenance plan ID is invalid."))
		return
	}
	var request ExecutePlanRequest
	if err := decodeBody(r, &request); err != nil || strings.TrimSpace(request.ConfirmationToken) == "" {
		httpx.WriteJSON(w, http.StatusBadRequest, httpx.Error("confirmation_required", "A valid cleanup confirmation is required."))
		return
	}
	ctx, cancel := context.WithTimeout(context.WithoutCancel(r.Context()), maintenanceOperationTimeout)
	defer cancel()
	plan, err := h.service.Execute(ctx, id, user.ID, request.ConfirmationToken)
	if err != nil {
		h.writeError(w, err)
		return
	}
	httpx.WriteJSON(w, http.StatusOK, publicPlan(plan))
}

func publicPlan(plan Plan) Plan {
	items := append([]PlanItem(nil), plan.Payload.Items...)
	for index := range items {
		items[index].Metadata = nil
	}
	plan.Payload.Items = items
	return plan
}

func (h *Handler) writeError(w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, ErrPlanNotFound):
		httpx.WriteJSON(w, http.StatusNotFound, httpx.Error("maintenance_plan_not_found", "Maintenance plan was not found."))
	case errors.Is(err, ErrPlanExpired):
		httpx.WriteJSON(w, http.StatusConflict, httpx.Error("maintenance_plan_expired", "Maintenance plan expired. Analyze storage again."))
	case errors.Is(err, ErrPlanNotExecutable):
		httpx.WriteJSON(w, http.StatusConflict, httpx.Error("maintenance_plan_not_executable", "Maintenance plan cannot be executed."))
	case errors.Is(err, ErrConfirmationRequired):
		httpx.WriteJSON(w, http.StatusForbidden, httpx.Error("confirmation_required", "A valid cleanup confirmation is required."))
	case errors.Is(err, ErrInvalidMode), errors.Is(err, ErrEmptySelection), errors.Is(err, ErrUnknownCategory), errors.Is(err, ErrCategoryUnavailable):
		httpx.WriteJSON(w, http.StatusBadRequest, httpx.Error("invalid_maintenance_request", "Maintenance selection is invalid or unavailable."))
	default:
		if h.logger != nil {
			h.logger.Error("maintenance operation failed", "error", err)
		}
		httpx.WriteJSON(w, http.StatusInternalServerError, httpx.Error("maintenance_failed", "Maintenance operation failed safely."))
	}
}

func decodeBody(r *http.Request, target any) error {
	decoder := json.NewDecoder(io.LimitReader(r.Body, 16*1024))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(target); err != nil {
		return err
	}
	if err := decoder.Decode(&struct{}{}); !errors.Is(err, io.EOF) {
		return errors.New("multiple JSON values")
	}
	return nil
}
