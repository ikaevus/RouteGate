package observability

import (
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"
	"strings"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/ikaevus/routegate/backend/internal/audit"
	"github.com/ikaevus/routegate/backend/internal/auth"
	"github.com/ikaevus/routegate/backend/internal/httpx"
)

type DiagnosticHandler struct {
	logger     *slog.Logger
	repository *DiagnosticRepository
	audit      *audit.Recorder
}

type createDiagnosticRequest struct {
	ProfileKey string `json:"profileKey"`
}

type diagnosticListResponse struct {
	Items []DiagnosticRunRecord `json:"items"`
}

func NewDiagnosticHandler(logger *slog.Logger, pool *pgxpool.Pool) *DiagnosticHandler {
	return &DiagnosticHandler{
		logger:     logger,
		repository: NewDiagnosticRepository(pool),
		audit:      audit.NewRecorder(logger, pool),
	}
}

func (h *DiagnosticHandler) Create(w http.ResponseWriter, r *http.Request) {
	var request createDiagnosticRequest
	if err := json.NewDecoder(r.Body).Decode(&request); err != nil {
		httpx.WriteJSON(w, http.StatusBadRequest, httpx.Error("invalid_request", "Request body must be valid JSON."))
		return
	}
	request.ProfileKey = strings.TrimSpace(request.ProfileKey)
	if !ValidDiagnosticProfile(request.ProfileKey) {
		httpx.WriteJSON(w, http.StatusBadRequest, httpx.Error("invalid_diagnostic_profile", "Diagnostic profile must be one of: host_overview, vpn_core_status, manager_certificate."))
		return
	}

	requestedBy := ""
	if user, ok := auth.UserFromContext(r.Context()); ok {
		requestedBy = user.ID
	}
	item, err := h.repository.Create(r.Context(), r.PathValue("server_id"), request.ProfileKey, requestedBy)
	if errors.Is(err, pgx.ErrNoRows) {
		httpx.WriteJSON(w, http.StatusConflict, httpx.Error("diagnostic_unavailable", "No compatible Agent is available for this diagnostic profile."))
		return
	}
	if err != nil {
		var pgErr *pgconn.PgError
		if errors.As(err, &pgErr) && pgErr.Code == "23505" {
			httpx.WriteJSON(w, http.StatusConflict, httpx.Error("agent_operation_busy", "Another Agent operation is already active for this server."))
			return
		}
		h.logger.Error("create diagnostic run failed", "error", err)
		httpx.WriteJSON(w, http.StatusInternalServerError, httpx.Error("database_error", "Failed to start diagnostic run."))
		return
	}

	if h.audit != nil {
		h.audit.RecordSafe(r.Context(), audit.EventInput{
			ActorType:    audit.ActorTypeUser,
			ActorUserID:  requestedBy,
			Action:       "observability.diagnostic.requested",
			ResourceType: "diagnostic_run",
			ResourceID:   item.ID,
			Result:       audit.ResultSuccess,
			Metadata: map[string]any{
				"server_id":   item.ServerID,
				"profile_key": item.ProfileKey,
			},
		})
	}
	httpx.WriteJSON(w, http.StatusAccepted, item)
}

func (h *DiagnosticHandler) Get(w http.ResponseWriter, r *http.Request) {
	item, err := h.repository.Get(r.Context(), r.PathValue("server_id"), r.PathValue("run_id"))
	if errors.Is(err, pgx.ErrNoRows) {
		httpx.WriteJSON(w, http.StatusNotFound, httpx.Error("diagnostic_not_found", "Diagnostic run was not found."))
		return
	}
	if err != nil {
		h.logger.Error("get diagnostic run failed", "error", err)
		httpx.WriteJSON(w, http.StatusInternalServerError, httpx.Error("database_error", "Failed to load diagnostic run."))
		return
	}
	httpx.WriteJSON(w, http.StatusOK, item)
}

func (h *DiagnosticHandler) List(w http.ResponseWriter, r *http.Request) {
	items, err := h.repository.List(r.Context(), r.PathValue("server_id"), 20)
	if err != nil {
		h.logger.Error("list diagnostic runs failed", "error", err)
		httpx.WriteJSON(w, http.StatusInternalServerError, httpx.Error("database_error", "Failed to list diagnostic runs."))
		return
	}
	httpx.WriteJSON(w, http.StatusOK, diagnosticListResponse{Items: items})
}
