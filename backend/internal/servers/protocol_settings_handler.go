package servers

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"net/http"
	"strings"

	"github.com/jackc/pgx/v5"

	"github.com/ikaevus/routegate/backend/internal/httpx"
)

const (
	recommendedVLESSPort    = 8443
	recommendedVLESSFlow    = "xtls-rprx-vision"
	recommendedVLESSNetwork = "tcp"
	realityShortIDBytes     = 8
)

type protocolSettingsRepository interface {
	GetProtocolSettings(context.Context, string) (ProtocolSettings, error)
	UpdateProtocolSettings(context.Context, string, UpdateProtocolSettingsInput) (ProtocolSettings, error)
	UpdateRealityKeypair(context.Context, string, UpdateRealityKeypairInput) (ProtocolSettings, error)
}

func (h *Handler) GetProtocolSettings(w http.ResponseWriter, r *http.Request) {
	repository, ok := h.protocolSettingsRepository()
	if !ok {
		h.databaseError(w, "get protocol settings repository", errors.New("protocol settings repository is unavailable"))
		return
	}

	settings, err := repository.GetProtocolSettings(r.Context(), r.PathValue("server_id"))
	if errors.Is(err, pgx.ErrNoRows) {
		writeServerNotFound(w)
		return
	}
	if err != nil {
		h.databaseError(w, "get protocol settings", err)
		return
	}

	httpx.WriteJSON(w, http.StatusOK, newProtocolSettingsResponse(settings))
}

func (h *Handler) UpdateProtocolSettings(w http.ResponseWriter, r *http.Request) {
	repository, ok := h.protocolSettingsRepository()
	if !ok {
		h.databaseError(w, "get protocol settings repository", errors.New("protocol settings repository is unavailable"))
		return
	}

	var request UpdateProtocolSettingsRequest
	if err := json.NewDecoder(r.Body).Decode(&request); err != nil {
		writeInvalidRequest(w, "Request body must be valid JSON.")
		return
	}

	trimStringPointer(request.VLESSFlow)
	trimStringPointer(request.VLESSNetwork)
	trimStringPointer(request.RealityPublicKey)
	trimStringPointer(request.RealityShortID)
	trimStringPointer(request.RealityServerName)
	if request.VLESSNetwork != nil {
		*request.VLESSNetwork = strings.ToLower(*request.VLESSNetwork)
	}

	input := UpdateProtocolSettingsInput{
		VLESSPort:         request.VLESSPort,
		VLESSFlow:         request.VLESSFlow,
		VLESSNetwork:      request.VLESSNetwork,
		RealityPublicKey:  request.RealityPublicKey,
		RealityShortID:    request.RealityShortID,
		RealityServerName: request.RealityServerName,
	}
	if err := validateProtocolSettingsInput(input); err != nil {
		writeInvalidRequest(w, err.Error())
		return
	}

	settings, err := repository.UpdateProtocolSettings(r.Context(), r.PathValue("server_id"), input)
	if errors.Is(err, pgx.ErrNoRows) {
		writeServerNotFound(w)
		return
	}
	if err != nil {
		h.databaseError(w, "update protocol settings", err)
		return
	}

	httpx.WriteJSON(w, http.StatusOK, newProtocolSettingsResponse(settings))
}

func (h *Handler) ConfigureRecommendedProtocolSettings(w http.ResponseWriter, r *http.Request) {
	repository, ok := h.protocolSettingsRepository()
	if !ok {
		h.databaseError(w, "get protocol settings repository", errors.New("protocol settings repository is unavailable"))
		return
	}

	serverID := r.PathValue("server_id")
	server, err := h.servers.GetServerByID(r.Context(), serverID)
	if errors.Is(err, pgx.ErrNoRows) {
		writeServerNotFound(w)
		return
	}
	if err != nil {
		h.databaseError(w, "get server for recommended protocol settings", err)
		return
	}

	serverName := recommendedRealityServerName(server)
	if serverName == "" {
		writeInvalidRequest(w, "A valid server hostname is required for recommended Reality setup.")
		return
	}

	keypair, err := h.generateRealityKeypair()
	if err != nil {
		h.logger.Error("generate Reality keypair failed", "server_id", serverID, "error", err)
		httpx.WriteJSON(w, http.StatusInternalServerError, httpx.Error("keypair_generation_failed", "Failed to generate Reality keypair."))
		return
	}

	shortID, err := generateRealityShortID()
	if err != nil {
		h.logger.Error("generate Reality short ID failed", "server_id", serverID, "error", err)
		httpx.WriteJSON(w, http.StatusInternalServerError, httpx.Error("short_id_generation_failed", "Failed to generate Reality short ID."))
		return
	}

	if _, err := repository.UpdateRealityKeypair(r.Context(), serverID, UpdateRealityKeypairInput{
		PrivateKey: keypair.PrivateKey,
		PublicKey:  keypair.PublicKey,
	}); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			writeServerNotFound(w)
			return
		}
		h.databaseError(w, "store recommended Reality keypair", err)
		return
	}

	port := recommendedVLESSPort
	flow := recommendedVLESSFlow
	network := recommendedVLESSNetwork
	settings, err := repository.UpdateProtocolSettings(r.Context(), serverID, UpdateProtocolSettingsInput{
		VLESSPort:         &port,
		VLESSFlow:         &flow,
		VLESSNetwork:      &network,
		RealityShortID:    &shortID,
		RealityServerName: &serverName,
	})
	if errors.Is(err, pgx.ErrNoRows) {
		writeServerNotFound(w)
		return
	}
	if err != nil {
		h.databaseError(w, "apply recommended protocol settings", err)
		return
	}

	httpx.WriteJSON(w, http.StatusOK, newProtocolSettingsResponse(settings))
}

func (h *Handler) GenerateRealityKeypair(w http.ResponseWriter, r *http.Request) {
	repository, ok := h.protocolSettingsRepository()
	if !ok {
		h.databaseError(w, "get protocol settings repository", errors.New("protocol settings repository is unavailable"))
		return
	}

	keypair, err := h.generateRealityKeypair()
	if err != nil {
		h.logger.Error("generate Reality keypair failed", "server_id", r.PathValue("server_id"), "error", err)
		httpx.WriteJSON(w, http.StatusInternalServerError, httpx.Error("keypair_generation_failed", "Failed to generate Reality keypair."))
		return
	}

	settings, err := repository.UpdateRealityKeypair(r.Context(), r.PathValue("server_id"), UpdateRealityKeypairInput{
		PrivateKey: keypair.PrivateKey,
		PublicKey:  keypair.PublicKey,
	})
	if errors.Is(err, pgx.ErrNoRows) {
		writeServerNotFound(w)
		return
	}
	if err != nil {
		h.databaseError(w, "update Reality keypair", err)
		return
	}

	httpx.WriteJSON(w, http.StatusOK, newProtocolSettingsResponse(settings))
}

func (h *Handler) protocolSettingsRepository() (protocolSettingsRepository, bool) {
	repository, ok := h.servers.(protocolSettingsRepository)
	return repository, ok
}

func validateProtocolSettingsInput(input UpdateProtocolSettingsInput) error {
	if input.VLESSPort != nil && (*input.VLESSPort < 1 || *input.VLESSPort > 65535) {
		return errors.New("vlessPort must be between 1 and 65535")
	}
	if input.VLESSNetwork != nil && !validVLESSNetwork(*input.VLESSNetwork) {
		return errors.New("vlessNetwork must be one of: tcp, ws, grpc, http")
	}
	if input.VLESSFlow != nil && !validVLESSFlow(*input.VLESSFlow) {
		return errors.New("vlessFlow must be empty or xtls-rprx-vision")
	}
	return nil
}

func validVLESSNetwork(network string) bool {
	switch strings.ToLower(network) {
	case "", "tcp", "ws", "grpc", "http":
		return true
	default:
		return false
	}
}

func validVLESSFlow(flow string) bool {
	switch flow {
	case "", "xtls-rprx-vision":
		return true
	default:
		return false
	}
}

func generateRealityShortID() (string, error) {
	value := make([]byte, realityShortIDBytes)
	if _, err := rand.Read(value); err != nil {
		return "", err
	}
	return hex.EncodeToString(value), nil
}

func recommendedRealityServerName(server Server) string {
	for _, candidate := range []string{server.Hostname, server.Name} {
		candidate = strings.ToLower(strings.TrimSpace(candidate))
		if validRecommendedRealityServerName(candidate) {
			return candidate
		}
	}
	return ""
}

func validRecommendedRealityServerName(value string) bool {
	return strings.Contains(value, ".") && !strings.ContainsAny(value, " \t\r\n/:\\")
}
