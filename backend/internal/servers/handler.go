package servers

import (
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"net"
	"net/http"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/ikaevus/routegate/backend/internal/agents"
	"github.com/ikaevus/routegate/backend/internal/audit"
	"github.com/ikaevus/routegate/backend/internal/auth"
	"github.com/ikaevus/routegate/backend/internal/httpx"
	"github.com/ikaevus/routegate/backend/internal/platform"
)

type serverRepository interface {
	CreateServer(context.Context, CreateServerInput) (Server, error)
	ListServersWithAgent(context.Context, ServerFilter) ([]ServerWithAgent, error)
	GetServerByID(context.Context, string) (Server, error)
	GetServerWithAgent(context.Context, string) (ServerWithAgent, error)
	UpdateServer(context.Context, string, UpdateServerInput) (Server, error)
	DeleteServer(context.Context, string) error
}

type registrationTokenRepository interface {
	CreateRegistrationToken(context.Context, agents.CreateRegistrationTokenInput) (agents.ServerRegistrationToken, error)
}

type Handler struct {
	logger                    *slog.Logger
	publicURL                 string
	service                   *Service
	servers                   serverRepository
	registrationTokens        registrationTokenRepository
	audit                     *audit.Recorder
	generateRegistrationToken func() (string, error)
	generateRealityKeypair    func() (RealityKeypair, error)
	now                       func() time.Time
}

func NewHandler(logger *slog.Logger, pool *pgxpool.Pool, publicURL string) *Handler {
	repository := NewRepository(pool)
	return &Handler{
		logger:                    logger,
		publicURL:                 publicURL,
		service:                   NewService(repository),
		servers:                   repository,
		registrationTokens:        agents.NewRepository(pool),
		audit:                     audit.NewRecorder(logger, pool),
		generateRegistrationToken: agents.GenerateRegistrationToken,
		generateRealityKeypair:    GenerateRealityKeypair,
		now:                       time.Now,
	}
}

// LegacyList preserves the original /api/admin/servers response while clients
// migrate to the authenticated /api/v1 server registry.
func (h *Handler) LegacyList(w http.ResponseWriter, r *http.Request) {
	items, err := h.service.List(r.Context())
	if err != nil {
		h.databaseError(w, "list legacy servers", err)
		return
	}
	httpx.WriteJSON(w, http.StatusOK, LegacyListServersResponse{Items: items})
}

// LegacyGet returns one server for the original admin endpoint.
func (h *Handler) LegacyGet(w http.ResponseWriter, r *http.Request) {
	server, err := h.servers.GetServerByID(r.Context(), r.PathValue("id"))
	if errors.Is(err, pgx.ErrNoRows) {
		writeServerNotFound(w)
		return
	}
	if err != nil {
		h.databaseError(w, "get legacy server", err)
		return
	}

	httpx.WriteJSON(w, http.StatusOK, server)
}

// LegacyCreate preserves hostname handling for the original admin endpoint.
func (h *Handler) LegacyCreate(w http.ResponseWriter, r *http.Request) {
	var request CreateServerRequest
	if err := json.NewDecoder(r.Body).Decode(&request); err != nil {
		writeInvalidRequest(w, "Request body must be valid JSON.")
		return
	}

	server, err := h.service.Create(r.Context(), request)
	if errors.Is(err, ErrServerNameRequired) {
		httpx.WriteJSON(w, http.StatusBadRequest, httpx.Error("name_required", "Server name is required."))
		return
	}
	if err != nil {
		h.databaseError(w, "create legacy server", err)
		return
	}

	h.logger.Info("server created", "id", server.ID, "name", server.Name)
	httpx.WriteJSON(w, http.StatusCreated, server)
}

func (h *Handler) List(w http.ResponseWriter, r *http.Request) {
	items, err := h.servers.ListServersWithAgent(r.Context(), ServerFilter{
		Status:         strings.TrimSpace(r.URL.Query().Get("status")),
		DeploymentRole: strings.TrimSpace(r.URL.Query().Get("deploymentRole")),
		Provider:       strings.TrimSpace(r.URL.Query().Get("provider")),
		Location:       strings.TrimSpace(r.URL.Query().Get("location")),
		Search:         strings.TrimSpace(r.URL.Query().Get("search")),
	})
	if err != nil {
		h.databaseError(w, "list servers", err)
		return
	}

	responseItems := make([]ServerResponse, len(items))
	for i := range items {
		responseItems[i] = newServerResponse(items[i], h.now().UTC())
	}
	httpx.WriteJSON(w, http.StatusOK, ListServersResponse{Items: responseItems})
}

func (h *Handler) Create(w http.ResponseWriter, r *http.Request) {
	var request CreateServerRequest
	if err := json.NewDecoder(r.Body).Decode(&request); err != nil {
		writeInvalidRequest(w, "Request body must be valid JSON.")
		return
	}

	deploymentRole, err := platform.ParseDeploymentRole(request.DeploymentRole, platform.DeploymentRoleVPN)
	if err != nil {
		writeInvalidRequest(w, err.Error())
		return
	}

	input := CreateServerInput{
		Name:           strings.TrimSpace(request.Name),
		DeploymentRole: string(deploymentRole),
		Description:    strings.TrimSpace(request.Description),
		Location:       strings.TrimSpace(request.Location),
		Provider:       strings.TrimSpace(request.Provider),
		PublicIP:       strings.TrimSpace(request.PublicIP),
		PrivateIP:      strings.TrimSpace(request.PrivateIP),
	}
	if input.Name == "" {
		httpx.WriteJSON(w, http.StatusBadRequest, httpx.Error("name_required", "Server name is required."))
		return
	}
	if field, ok := invalidIPField(input.PublicIP, input.PrivateIP); ok {
		writeInvalidRequest(w, field+" must be a valid IP address.")
		return
	}

	server, err := h.servers.CreateServer(r.Context(), input)
	if err != nil {
		h.databaseError(w, "create server", err)
		return
	}

	h.recordAudit(r, audit.EventInput{
		Action:       "server.created",
		ResourceType: "server",
		ResourceID:   server.ID,
		Result:       audit.ResultSuccess,
		Metadata: map[string]any{
			"server_name":     server.Name,
			"deployment_role": server.DeploymentRole,
			"provider":        server.Provider,
			"location":        server.Location,
		},
	})
	h.logger.Info("server created", "id", server.ID, "name", server.Name)
	httpx.WriteJSON(w, http.StatusCreated, server)
}

func (h *Handler) Get(w http.ResponseWriter, r *http.Request) {
	server, err := h.servers.GetServerWithAgent(r.Context(), r.PathValue("server_id"))
	if errors.Is(err, pgx.ErrNoRows) {
		writeServerNotFound(w)
		return
	}
	if err != nil {
		h.databaseError(w, "get server", err)
		return
	}

	httpx.WriteJSON(w, http.StatusOK, newServerResponse(server, h.now().UTC()))
}

func (h *Handler) Update(w http.ResponseWriter, r *http.Request) {
	var request UpdateServerRequest
	if err := json.NewDecoder(r.Body).Decode(&request); err != nil {
		writeInvalidRequest(w, "Request body must be valid JSON.")
		return
	}

	trimStringPointer(request.Name)
	trimStringPointer(request.Description)
	trimStringPointer(request.Location)
	trimStringPointer(request.Provider)
	trimStringPointer(request.PublicIP)
	trimStringPointer(request.PrivateIP)
	trimStringPointer(request.Status)

	if request.Name != nil && *request.Name == "" {
		httpx.WriteJSON(w, http.StatusBadRequest, httpx.Error("name_required", "Server name is required."))
		return
	}
	if request.Status != nil && !validServerStatus(*request.Status) {
		writeInvalidRequest(w, "Status must be one of: pending, active, offline, disabled, error.")
		return
	}
	if request.PublicIP != nil && *request.PublicIP != "" && net.ParseIP(*request.PublicIP) == nil {
		writeInvalidRequest(w, "publicIp must be a valid IP address.")
		return
	}
	if request.PrivateIP != nil && *request.PrivateIP != "" && net.ParseIP(*request.PrivateIP) == nil {
		writeInvalidRequest(w, "privateIp must be a valid IP address.")
		return
	}

	server, err := h.servers.UpdateServer(r.Context(), r.PathValue("server_id"), UpdateServerInput{
		Name:        request.Name,
		Description: request.Description,
		Location:    request.Location,
		Provider:    request.Provider,
		PublicIP:    request.PublicIP,
		PrivateIP:   request.PrivateIP,
		Status:      request.Status,
	})
	if errors.Is(err, pgx.ErrNoRows) {
		writeServerNotFound(w)
		return
	}
	if err != nil {
		h.databaseError(w, "update server", err)
		return
	}

	h.recordAudit(r, audit.EventInput{
		Action:       "server.updated",
		ResourceType: "server",
		ResourceID:   server.ID,
		Result:       audit.ResultSuccess,
		Metadata: map[string]any{
			"server_name":    server.Name,
			"changed_fields": changedServerFields(request),
		},
	})
	httpx.WriteJSON(w, http.StatusOK, server)
}

func (h *Handler) Delete(w http.ResponseWriter, r *http.Request) {
	serverID := r.PathValue("server_id")
	err := h.servers.DeleteServer(r.Context(), serverID)
	if errors.Is(err, pgx.ErrNoRows) {
		writeServerNotFound(w)
		return
	}
	if errors.Is(err, ErrServerInUse) {
		httpx.WriteJSON(w, http.StatusConflict, httpx.Error(
			"server_in_use",
			"Server cannot be deleted while it has an Agent, VPN accounts, or config versions.",
		))
		return
	}
	if err != nil {
		h.databaseError(w, "delete server", err)
		return
	}

	h.recordAudit(r, audit.EventInput{
		Action:       "server.deleted",
		ResourceType: "server",
		ResourceID:   serverID,
		Result:       audit.ResultSuccess,
	})
	w.WriteHeader(http.StatusNoContent)
}

func (h *Handler) CreateRegistrationToken(w http.ResponseWriter, r *http.Request) {
	serverID := r.PathValue("server_id")
	server, err := h.servers.GetServerByID(r.Context(), serverID)
	if errors.Is(err, pgx.ErrNoRows) {
		writeServerNotFound(w)
		return
	} else if err != nil {
		h.databaseError(w, "get server for registration token", err)
		return
	}
	if !platform.EffectiveDeploymentRole(server.DeploymentRole).HostsVPNPlane() {
		httpx.WriteJSON(w, http.StatusConflict, httpx.Error(
			"node_role_incompatible",
			"A Management Node does not host RouteGate Agent or VPN Core. Choose a VPN or Hybrid Node.",
		))
		return
	}

	rawToken, err := h.generateRegistrationToken()
	if err != nil {
		h.logger.Error("generate server registration token failed", "server_id", serverID, "error", err)
		httpx.WriteJSON(w, http.StatusInternalServerError, httpx.Error("token_generation_failed", "Failed to generate registration token."))
		return
	}

	expiresAt := h.now().UTC().Add(24 * time.Hour)
	created, err := h.registrationTokens.CreateRegistrationToken(r.Context(), agents.CreateRegistrationTokenInput{
		ServerID:  serverID,
		TokenHash: agents.HashToken(rawToken),
		ExpiresAt: &expiresAt,
	})
	if err != nil {
		h.databaseError(w, "create server registration token", err)
		return
	}
	if created.ExpiresAt != nil {
		expiresAt = created.ExpiresAt.UTC()
	}

	h.recordAudit(r, audit.EventInput{
		Action:       "agent.registration_token.created",
		ResourceType: "server",
		ResourceID:   serverID,
		Result:       audit.ResultSuccess,
		Metadata: map[string]any{
			"token_preview":      audit.MaskSecret(rawToken),
			"expires_at":         expiresAt,
			"bootstrap_available": agentBootstrapAvailable(h.publicURL),
		},
	})
	managerURL, bootstrapCommand := buildAgentBootstrapCommand(h.publicURL, rawToken)
	httpx.WriteJSON(w, http.StatusCreated, RegistrationTokenResponse{
		ServerID:          serverID,
		RegistrationToken: rawToken,
		ExpiresAt:         expiresAt,
		ManagerURL:        managerURL,
		BootstrapCommand:  bootstrapCommand,
	})
}

func (h *Handler) recordAudit(r *http.Request, input audit.EventInput) {
	if user, ok := auth.UserFromContext(r.Context()); ok {
		input.ActorUserID = user.ID
		input.ActorType = audit.ActorTypeUser
	} else if input.ActorType == "" {
		input.ActorType = audit.ActorTypeSystem
	}
	h.audit.RecordSafe(r.Context(), input)
}

func changedServerFields(request UpdateServerRequest) []string {
	fields := make([]string, 0, 7)
	if request.Name != nil {
		fields = append(fields, "name")
	}
	if request.Description != nil {
		fields = append(fields, "description")
	}
	if request.Location != nil {
		fields = append(fields, "location")
	}
	if request.Provider != nil {
		fields = append(fields, "provider")
	}
	if request.PublicIP != nil {
		fields = append(fields, "public_ip")
	}
	if request.PrivateIP != nil {
		fields = append(fields, "private_ip")
	}
	if request.Status != nil {
		fields = append(fields, "status")
	}
	return fields
}

func (h *Handler) databaseError(w http.ResponseWriter, operation string, err error) {
	h.logger.Error(operation+" failed", "error", err)
	httpx.WriteJSON(w, http.StatusInternalServerError, httpx.Error("database_error", "Database operation failed."))
}

func writeInvalidRequest(w http.ResponseWriter, message string) {
	httpx.WriteJSON(w, http.StatusBadRequest, httpx.Error("invalid_request", message))
}

func writeServerNotFound(w http.ResponseWriter) {
	httpx.WriteJSON(w, http.StatusNotFound, httpx.Error("server_not_found", "Server not found."))
}

func trimStringPointer(value *string) {
	if value != nil {
		*value = strings.TrimSpace(*value)
	}
}

func invalidIPField(publicIP, privateIP string) (string, bool) {
	if publicIP != "" && net.ParseIP(publicIP) == nil {
		return "publicIp", true
	}
	if privateIP != "" && net.ParseIP(privateIP) == nil {
		return "privateIp", true
	}
	return "", false
}

func validServerStatus(status string) bool {
	switch status {
	case StatusPending, StatusActive, StatusOffline, StatusDisabled, StatusError:
		return true
	default:
		return false
	}
}
