package agents

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

	"github.com/artuazh/routegate/backend/internal/httpx"
)

const activeServerStatus = "active"

type agentAPIRepository interface {
	ConsumeValidRegistrationTokenByHash(context.Context, string) (ServerRegistrationToken, error)
	CreateOrReplaceAgentForServer(context.Context, CreateOrReplaceAgentInput) (Agent, error)
	ActivateServer(context.Context, string) error
	UpdateAgentHeartbeat(context.Context, UpdateAgentHeartbeatInput) (Agent, error)
}

type Handler struct {
	logger             *slog.Logger
	service            *Service
	repository         agentAPIRepository
	generateAgentToken func() (string, error)
	now                func() time.Time
}

func NewHandler(logger *slog.Logger, pool *pgxpool.Pool) *Handler {
	repository := NewRepository(pool)

	return &Handler{
		logger:             logger,
		service:            NewService(repository),
		repository:         repository,
		generateAgentToken: GenerateAgentToken,
		now:                time.Now,
	}
}

func (h *Handler) List(w http.ResponseWriter, r *http.Request) {
	items, err := h.service.List(r.Context())
	if err != nil {
		h.logger.Error("list agents failed", "error", err)
		httpx.WriteJSON(w, http.StatusInternalServerError, httpx.Error(
			"database_error",
			"Failed to list agents.",
		))
		return
	}

	httpx.WriteJSON(w, http.StatusOK, ListAgentsResponse{Items: items})
}

func (h *Handler) Register(w http.ResponseWriter, r *http.Request) {
	var request AgentRegistrationRequest
	if err := json.NewDecoder(r.Body).Decode(&request); err != nil {
		httpx.WriteJSON(w, http.StatusBadRequest, httpx.Error(
			"invalid_request",
			"Request body must be valid JSON.",
		))
		return
	}

	request.RegistrationToken = strings.TrimSpace(request.RegistrationToken)
	if request.RegistrationToken == "" {
		httpx.WriteJSON(w, http.StatusBadRequest, httpx.Error(
			"registration_token_required",
			"Registration token is required.",
		))
		return
	}

	registrationToken, err := h.repository.ConsumeValidRegistrationTokenByHash(
		r.Context(),
		HashToken(request.RegistrationToken),
	)
	if errors.Is(err, pgx.ErrNoRows) {
		httpx.WriteJSON(w, http.StatusUnauthorized, httpx.Error(
			"invalid_registration_token",
			"Registration token is invalid, expired, or already used.",
		))
		return
	}
	if err != nil {
		h.logger.Error("validate agent registration token failed", "error", err)
		httpx.WriteJSON(w, http.StatusInternalServerError, httpx.Error(
			"database_error",
			"Failed to register agent.",
		))
		return
	}

	agentToken, err := h.generateAgentToken()
	if err != nil {
		h.logger.Error("generate agent token failed", "error", err)
		httpx.WriteJSON(w, http.StatusInternalServerError, httpx.Error(
			"token_generation_failed",
			"Failed to register agent.",
		))
		return
	}

	now := h.now().UTC()
	// TODO: Create the agent and activate its server in one transaction when the
	// repository exposes transaction-scoped registration operations. The one-time
	// registration token has already been consumed atomically.
	agent, err := h.repository.CreateOrReplaceAgentForServer(r.Context(), CreateOrReplaceAgentInput{
		ServerID:     registrationToken.ServerID,
		Hostname:     strings.TrimSpace(request.Hostname),
		OS:           strings.TrimSpace(request.OS),
		Arch:         strings.TrimSpace(request.Arch),
		AgentVersion: strings.TrimSpace(request.AgentVersion),
		TokenHash:    HashToken(agentToken),
		Capabilities: request.Capabilities,
		Status:       StatusOnline,
		RegisteredAt: &now,
		LastSeenAt:   &now,
	})
	if err != nil {
		h.logger.Error("create agent registration failed", "error", err, "server_id", registrationToken.ServerID)
		httpx.WriteJSON(w, http.StatusInternalServerError, httpx.Error(
			"database_error",
			"Failed to register agent.",
		))
		return
	}

	if err := h.repository.ActivateServer(r.Context(), registrationToken.ServerID); err != nil {
		h.logger.Error("activate registered agent server failed", "error", err, "server_id", registrationToken.ServerID)
		httpx.WriteJSON(w, http.StatusInternalServerError, httpx.Error(
			"database_error",
			"Failed to register agent.",
		))
		return
	}

	h.logger.Info("agent registered", "agent_id", agent.ID, "server_id", agent.ServerID)
	httpx.WriteJSON(w, http.StatusCreated, AgentRegistrationResponse{
		AgentID: agent.ID, ServerID: agent.ServerID, AgentToken: agentToken,
	})
}

func (h *Handler) Heartbeat(w http.ResponseWriter, r *http.Request) {
	token, ok := bearerToken(r.Header.Get("Authorization"))
	if !ok {
		httpx.WriteJSON(w, http.StatusUnauthorized, httpx.Error(
			"unauthorized",
			"A valid agent bearer token is required.",
		))
		return
	}

	var request AgentHeartbeatRequest
	if err := json.NewDecoder(r.Body).Decode(&request); err != nil {
		httpx.WriteJSON(w, http.StatusBadRequest, httpx.Error(
			"invalid_request",
			"Request body must be valid JSON.",
		))
		return
	}

	agent, err := h.repository.UpdateAgentHeartbeat(r.Context(), UpdateAgentHeartbeatInput{
		TokenHash: HashToken(token), AgentVersion: request.AgentVersion, Capabilities: request.Capabilities,
	})
	if errors.Is(err, pgx.ErrNoRows) {
		httpx.WriteJSON(w, http.StatusUnauthorized, httpx.Error(
			"unauthorized",
			"A valid agent bearer token is required.",
		))
		return
	}
	if err != nil {
		h.logger.Error("agent heartbeat failed", "error", err)
		httpx.WriteJSON(w, http.StatusInternalServerError, httpx.Error(
			"database_error",
			"Failed to update agent heartbeat.",
		))
		return
	}

	h.logger.Debug("agent heartbeat accepted", "agent_id", agent.ID, "server_id", agent.ServerID)
	httpx.WriteJSON(w, http.StatusOK, AgentHeartbeatResponse{
		OK: true, AgentID: agent.ID, ServerID: agent.ServerID, ServerStatus: activeServerStatus,
	})
}

func bearerToken(header string) (string, bool) {
	parts := strings.Fields(header)
	if len(parts) != 2 || !strings.EqualFold(parts[0], "Bearer") || parts[1] == "" {
		return "", false
	}
	return parts[1], true
}
