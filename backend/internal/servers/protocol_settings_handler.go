package servers

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"net/mail"
	"net/netip"
	"net/http"
	"net/url"
	"strings"

	"github.com/jackc/pgx/v5"

	"github.com/ikaevus/routegate/backend/internal/httpx"
	wgcredentials "github.com/ikaevus/routegate/backend/internal/wireguard"
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
	ConfigureRecommendedWireGuard(context.Context, string, UpdateWireGuardKeypairInput) (ProtocolSettings, error)
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
	trimStringPointer(request.Protocol)
	trimStringPointer(request.RealityPublicKey)
	trimStringPointer(request.RealityShortID)
	trimStringPointer(request.RealityServerName)
	trimStringPointer(request.WireGuardAddress)
	trimStringPointer(request.WireGuardDNS)
	trimStringPointer(request.Hysteria2Domain)
	trimStringPointer(request.Hysteria2ACMEEmail)
	trimStringPointer(request.Hysteria2MasqueradeURL)
	if request.Protocol != nil {
		*request.Protocol = strings.ToLower(*request.Protocol)
	}
	if request.VLESSNetwork != nil {
		*request.VLESSNetwork = strings.ToLower(*request.VLESSNetwork)
	}

	input := UpdateProtocolSettingsInput{
		Protocol:          request.Protocol,
		VLESSPort:         request.VLESSPort,
		VLESSFlow:         request.VLESSFlow,
		VLESSNetwork:      request.VLESSNetwork,
		RealityPublicKey:  request.RealityPublicKey,
		RealityShortID:    request.RealityShortID,
		RealityServerName: request.RealityServerName,
		WireGuardPort:      request.WireGuardPort,
		WireGuardAddress:   request.WireGuardAddress,
		WireGuardDNS:       request.WireGuardDNS,
		Hysteria2Port:       request.Hysteria2Port,
		Hysteria2Domain:     request.Hysteria2Domain,
		Hysteria2ACMEEmail:  request.Hysteria2ACMEEmail,
		Hysteria2MasqueradeURL: request.Hysteria2MasqueradeURL,
		ShadowsocksPort:         request.ShadowsocksPort,
		MTProtoPort:              request.MTProtoPort,
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
	protocol := "vless"
	settings, err := repository.UpdateProtocolSettings(r.Context(), serverID, UpdateProtocolSettingsInput{
		Protocol:          &protocol,
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

func (h *Handler) ConfigureRecommendedWireGuard(w http.ResponseWriter, r *http.Request) {
	repository, ok := h.protocolSettingsRepository()
	if !ok {
		h.databaseError(w, "get protocol settings repository", errors.New("protocol settings repository is unavailable"))
		return
	}

	keypair, err := wgcredentials.GenerateKeypair()
	if err != nil {
		h.logger.Error("generate WireGuard keypair failed", "server_id", r.PathValue("server_id"), "error", err)
		httpx.WriteJSON(w, http.StatusInternalServerError, httpx.Error("keypair_generation_failed", "Failed to generate WireGuard keypair."))
		return
	}

	settings, err := repository.ConfigureRecommendedWireGuard(r.Context(), r.PathValue("server_id"), UpdateWireGuardKeypairInput{
		PrivateKey: keypair.PrivateKey,
		PublicKey:  keypair.PublicKey,
	})
	if errors.Is(err, pgx.ErrNoRows) {
		writeServerNotFound(w)
		return
	}
	if err != nil {
		h.databaseError(w, "configure recommended WireGuard settings", err)
		return
	}

	httpx.WriteJSON(w, http.StatusOK, newProtocolSettingsResponse(settings))
}

func (h *Handler) protocolSettingsRepository() (protocolSettingsRepository, bool) {
	repository, ok := h.servers.(protocolSettingsRepository)
	return repository, ok
}

func validateProtocolSettingsInput(input UpdateProtocolSettingsInput) error {
	if input.Protocol != nil && *input.Protocol != "vless" && *input.Protocol != "wireguard" && *input.Protocol != "hysteria2" && *input.Protocol != "shadowsocks" && *input.Protocol != "mtproto" {
		return errors.New("protocol must be one of: vless, wireguard, hysteria2, shadowsocks, mtproto")
	}
	if input.VLESSPort != nil && (*input.VLESSPort < 1 || *input.VLESSPort > 65535) {
		return errors.New("vlessPort must be between 1 and 65535")
	}
	if input.VLESSNetwork != nil && !validVLESSNetwork(*input.VLESSNetwork) {
		return errors.New("vlessNetwork must be one of: tcp, ws, grpc, http")
	}
	if input.VLESSFlow != nil && !validVLESSFlow(*input.VLESSFlow) {
		return errors.New("vlessFlow must be empty or xtls-rprx-vision")
	}
	if input.WireGuardPort != nil && (*input.WireGuardPort < 1 || *input.WireGuardPort > 65535) {
		return errors.New("wireGuardPort must be between 1 and 65535")
	}
	if input.WireGuardAddress != nil {
		prefix, err := netip.ParsePrefix(*input.WireGuardAddress)
		if err != nil || !prefix.Addr().Is4() || prefix.Bits() > 30 {
			return errors.New("wireGuardAddress must be an IPv4 prefix with room for peers")
		}
	}
	if input.WireGuardDNS != nil {
		address, err := netip.ParseAddr(*input.WireGuardDNS)
		if err != nil || !address.IsValid() {
			return errors.New("wireGuardDns must be an IP address")
		}
	}
	if input.Hysteria2Port != nil && (*input.Hysteria2Port < 1 || *input.Hysteria2Port > 65535) {
		return errors.New("hysteria2Port must be between 1 and 65535")
	}
	if input.Hysteria2Domain != nil && !validHysteria2ServerName(*input.Hysteria2Domain) {
		return errors.New("hysteria2Domain must be a valid DNS hostname")
	}
	if input.Hysteria2ACMEEmail != nil && !validHysteria2Email(*input.Hysteria2ACMEEmail) {
		return errors.New("hysteria2AcmeEmail must be a valid email address")
	}
	if input.Hysteria2MasqueradeURL != nil && !validHysteria2MasqueradeURL(*input.Hysteria2MasqueradeURL) {
		return errors.New("hysteria2MasqueradeUrl must match the fixed RouteGate masquerade target")
	}
	if input.ShadowsocksPort != nil && (*input.ShadowsocksPort < 1 || *input.ShadowsocksPort > 65535) {
		return errors.New("shadowsocksPort must be between 1 and 65535")
	}
	if input.MTProtoPort != nil && (*input.MTProtoPort < 1 || *input.MTProtoPort > 65535) {
		return errors.New("mtprotoPort must be between 1 and 65535")
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

func validHysteria2ServerName(value string) bool {
	value = strings.ToLower(strings.TrimSpace(value))
	labels := strings.Split(value, ".")
	if len(value) < 4 || len(value) > 253 || len(labels) < 2 { return false }
	for _, label := range labels {
		if len(label) == 0 || len(label) > 63 || label[0] == '-' || label[len(label)-1] == '-' { return false }
		for _, char := range label {
			if !((char >= 'a' && char <= 'z') || (char >= '0' && char <= '9') || char == '-') { return false }
		}
	}
	return true
}

func validHysteria2Email(value string) bool {
	value = strings.TrimSpace(value)
	address, err := mail.ParseAddress(value)
	return err == nil && address.Address == value
}

func validHysteria2MasqueradeURL(value string) bool {
	parsed, err := url.Parse(strings.TrimSpace(value))
	return err == nil && parsed.String() == "https://www.cloudflare.com/" && parsed.User == nil && parsed.Fragment == ""
}
