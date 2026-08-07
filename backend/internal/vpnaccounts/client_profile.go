package vpnaccounts

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/ikaevus/routegate/backend/internal/audit"
	"github.com/ikaevus/routegate/backend/internal/httpx"
)

const (
	FingerprintModeAuto   = "auto"
	FingerprintModeManual = "manual"

	DefaultAutoFingerprint = "firefox"
)

var allowedFingerprints = map[string]struct{}{
	"chrome": {}, "firefox": {}, "safari": {}, "ios": {}, "android": {}, "edge": {}, "random": {}, "randomized": {},
}

var allowedClientTypes = map[string]struct{}{
	"v2rayn": {}, "v2raytun": {}, "v2box": {}, "sing-box": {}, "other": {},
}

var allowedDeviceTypes = map[string]struct{}{
	"windows": {}, "ios": {}, "android": {}, "macos": {}, "linux": {}, "other": {},
}

type ClientProfile struct {
	ID                  string    `json:"id"`
	VPNAccountID        string    `json:"vpnAccountId"`
	Name                string    `json:"name"`
	ClientType          string    `json:"clientType"`
	DeviceType          string    `json:"deviceType"`
	FingerprintMode     string    `json:"fingerprintMode"`
	Fingerprint         string    `json:"fingerprint"`
	ResolvedFingerprint string    `json:"resolvedFingerprint"`
	ServerNameOverride  string    `json:"serverNameOverride,omitempty"`
	SpiderX             string    `json:"spiderX"`
	MTU                 *int      `json:"mtu,omitempty"`
	CreatedAt           time.Time `json:"createdAt"`
	UpdatedAt           time.Time `json:"updatedAt"`
}

type UpdateClientProfileRequest struct {
	Name               string `json:"name"`
	ClientType         string `json:"clientType"`
	DeviceType         string `json:"deviceType"`
	FingerprintMode    string `json:"fingerprintMode"`
	Fingerprint        string `json:"fingerprint"`
	ServerNameOverride string `json:"serverNameOverride"`
	SpiderX            string `json:"spiderX"`
	MTU                *int   `json:"mtu"`
}

type ClientConnectionResponse struct {
	VPNAccountID string        `json:"vpnAccountId"`
	Format       string        `json:"format"`
	VLESSLink    string        `json:"vlessLink"`
	Profile      ClientProfile `json:"profile"`
	Endpoint     string        `json:"endpoint"`
	ServerName   string        `json:"serverName"`
	Network      string        `json:"network"`
	Flow         string        `json:"flow,omitempty"`
}

type clientProfileRepository interface {
	GetOrCreateClientProfile(context.Context, string) (ClientProfile, error)
	UpdateClientProfile(context.Context, string, UpdateClientProfileRequest) (ClientProfile, error)
}

func (r *Repository) GetOrCreateClientProfile(ctx context.Context, vpnAccountID string) (ClientProfile, error) {
	return scanClientProfile(r.pool.QueryRow(ctx, `
		INSERT INTO vpn_client_profiles (vpn_account_id)
		VALUES ($1::uuid)
		ON CONFLICT (vpn_account_id) DO UPDATE
		SET vpn_account_id = EXCLUDED.vpn_account_id
		RETURNING
			id::text,
			vpn_account_id::text,
			name,
			client_type,
			device_type,
			fingerprint_mode,
			fingerprint,
			server_name_override,
			spider_x,
			mtu,
			created_at,
			updated_at
	`, vpnAccountID))
}

func (r *Repository) UpdateClientProfile(ctx context.Context, vpnAccountID string, request UpdateClientProfileRequest) (ClientProfile, error) {
	if _, err := r.GetOrCreateClientProfile(ctx, vpnAccountID); err != nil {
		return ClientProfile{}, err
	}
	return scanClientProfile(r.pool.QueryRow(ctx, `
		UPDATE vpn_client_profiles
		SET
			name = $2,
			client_type = $3,
			device_type = $4,
			fingerprint_mode = $5,
			fingerprint = $6,
			server_name_override = NULLIF($7, ''),
			spider_x = $8,
			mtu = $9,
			updated_at = now()
		WHERE vpn_account_id = $1::uuid
		RETURNING
			id::text,
			vpn_account_id::text,
			name,
			client_type,
			device_type,
			fingerprint_mode,
			fingerprint,
			server_name_override,
			spider_x,
			mtu,
			created_at,
			updated_at
	`, vpnAccountID, request.Name, request.ClientType, request.DeviceType, request.FingerprintMode, request.Fingerprint, request.ServerNameOverride, request.SpiderX, request.MTU))
}

func scanClientProfile(row scanner) (ClientProfile, error) {
	var profile ClientProfile
	var serverNameOverride sql.NullString
	var mtu sql.NullInt32
	if err := row.Scan(
		&profile.ID,
		&profile.VPNAccountID,
		&profile.Name,
		&profile.ClientType,
		&profile.DeviceType,
		&profile.FingerprintMode,
		&profile.Fingerprint,
		&serverNameOverride,
		&profile.SpiderX,
		&mtu,
		&profile.CreatedAt,
		&profile.UpdatedAt,
	); err != nil {
		return ClientProfile{}, err
	}
	if serverNameOverride.Valid {
		profile.ServerNameOverride = serverNameOverride.String
	}
	if mtu.Valid {
		value := int(mtu.Int32)
		profile.MTU = &value
	}
	profile.ResolvedFingerprint = resolveClientFingerprint(profile)
	return profile, nil
}

func (h *Handler) GetClientConnection(w http.ResponseWriter, r *http.Request) {
	accountID := r.PathValue("id")
	response, err := h.clientConnection(r.Context(), accountID)
	if errors.Is(err, pgx.ErrNoRows) {
		writeAccountNotFound(w)
		return
	}
	if err != nil {
		h.databaseError(w, "get vpn client connection", err)
		return
	}
	httpx.WriteJSON(w, http.StatusOK, response)
}

func (h *Handler) UpdateClientProfile(w http.ResponseWriter, r *http.Request) {
	var request UpdateClientProfileRequest
	decoder := json.NewDecoder(http.MaxBytesReader(w, r.Body, 32<<10))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&request); err != nil {
		writeInvalidRequest(w, "Request body must be valid JSON.")
		return
	}
	normalizeClientProfileRequest(&request)
	if err := validateClientProfileRequest(request); err != nil {
		writeInvalidRequest(w, err.Error())
		return
	}

	accountID := r.PathValue("id")
	if _, err := h.accounts.GetAccountByID(r.Context(), accountID); errors.Is(err, pgx.ErrNoRows) {
		writeAccountNotFound(w)
		return
	} else if err != nil {
		h.databaseError(w, "get vpn account for client profile", err)
		return
	}

	repository, ok := h.accounts.(clientProfileRepository)
	if !ok {
		httpx.WriteJSON(w, http.StatusInternalServerError, httpx.Error("client_profile_unavailable", "Client profile storage is unavailable."))
		return
	}
	if _, err := repository.UpdateClientProfile(r.Context(), accountID, request); err != nil {
		h.databaseError(w, "update vpn client profile", err)
		return
	}

	response, err := h.clientConnection(r.Context(), accountID)
	if err != nil {
		h.databaseError(w, "render updated vpn client connection", err)
		return
	}
	if h.audit != nil {
		h.recordAudit(r, audit.EventInput{
			Action:       "vpn_client_profile.updated",
			ResourceType: "vpn_account",
			ResourceID:   accountID,
			Result:       audit.ResultSuccess,
			Metadata: map[string]any{
				"client_type":          response.Profile.ClientType,
				"device_type":          response.Profile.DeviceType,
				"fingerprint_mode":     response.Profile.FingerprintMode,
				"resolved_fingerprint": response.Profile.ResolvedFingerprint,
			},
		})
	}
	httpx.WriteJSON(w, http.StatusOK, response)
}

func (h *Handler) clientConnection(ctx context.Context, accountID string) (ClientConnectionResponse, error) {
	subscription, err := h.accounts.GetSubscriptionProfileByAccountID(ctx, accountID)
	if err != nil {
		return ClientConnectionResponse{}, err
	}
	if subscription.Server == nil {
		return ClientConnectionResponse{}, fmt.Errorf("vpn account is not assigned to a server")
	}
	repository, ok := h.accounts.(clientProfileRepository)
	if !ok {
		return ClientConnectionResponse{}, errors.New("client profile storage is unavailable")
	}
	profile, err := repository.GetOrCreateClientProfile(ctx, accountID)
	if err != nil {
		return ClientConnectionResponse{}, err
	}
	link, endpoint, serverName, network, flow, err := buildClientVLESSLink(subscription, profile)
	if err != nil {
		return ClientConnectionResponse{}, err
	}
	return ClientConnectionResponse{
		VPNAccountID: accountID,
		Format:       "vless-reality-uri",
		VLESSLink:    link,
		Profile:      profile,
		Endpoint:     endpoint,
		ServerName:   serverName,
		Network:      network,
		Flow:         flow,
	}, nil
}

func buildClientVLESSLink(subscription SubscriptionProfile, profile ClientProfile) (string, string, string, string, string, error) {
	if subscription.Server == nil {
		return "", "", "", "", "", errors.New("server is required")
	}
	server := subscription.Server
	host := normalizeServerEndpoint(server.PublicIP)
	if host == "" {
		host = normalizeServerEndpoint(server.Hostname)
	}
	if host == "" {
		return "", "", "", "", "", errors.New("server endpoint is required")
	}
	port := server.VLESSPort
	if port < 1 || port > 65535 {
		return "", "", "", "", "", errors.New("server VLESS port is invalid")
	}
	uuid := strings.TrimSpace(subscription.Account.VLESSUUID)
	if uuid == "" {
		return "", "", "", "", "", errors.New("VLESS UUID is required")
	}
	serverName := strings.TrimSpace(profile.ServerNameOverride)
	if serverName == "" {
		serverName = strings.TrimSpace(server.RealityServerName)
	}
	if serverName == "" {
		return "", "", "", "", "", errors.New("Reality server name is required")
	}
	publicKey := strings.TrimSpace(server.RealityPublicKey)
	if publicKey == "" {
		return "", "", "", "", "", errors.New("Reality public key is required")
	}
	network := strings.TrimSpace(server.VLESSNetwork)
	if network == "" {
		network = "tcp"
	}
	flow := strings.TrimSpace(server.VLESSFlow)
	fingerprint := resolveClientFingerprint(profile)

	parameters := url.Values{}
	parameters.Set("encryption", "none")
	parameters.Set("security", "reality")
	parameters.Set("type", network)
	parameters.Set("sni", serverName)
	parameters.Set("fp", fingerprint)
	parameters.Set("pbk", publicKey)
	if shortID := strings.TrimSpace(server.RealityShortID); shortID != "" {
		parameters.Set("sid", shortID)
	}
	if flow != "" {
		parameters.Set("flow", flow)
	}
	if spiderX := strings.TrimSpace(profile.SpiderX); spiderX != "" && spiderX != "/" {
		parameters.Set("spx", spiderX)
	}

	formattedHost := host
	if parsed := net.ParseIP(host); parsed != nil && strings.Contains(host, ":") {
		formattedHost = "[" + host + "]"
	}
	label := strings.TrimSpace(subscription.Account.DisplayName)
	if label == "" {
		label = strings.TrimSpace(server.Name)
	}
	if label == "" {
		label = "RouteGate"
	}
	endpoint := net.JoinHostPort(host, strconv.Itoa(port))
	link := "vless://" + url.PathEscape(uuid) + "@" + formattedHost + ":" + strconv.Itoa(port) + "?" + parameters.Encode() + "#" + url.QueryEscape(label)
	return link, endpoint, serverName, network, flow, nil
}

func normalizeClientProfileRequest(request *UpdateClientProfileRequest) {
	request.Name = strings.TrimSpace(request.Name)
	request.ClientType = strings.ToLower(strings.TrimSpace(request.ClientType))
	request.DeviceType = strings.ToLower(strings.TrimSpace(request.DeviceType))
	request.FingerprintMode = strings.ToLower(strings.TrimSpace(request.FingerprintMode))
	request.Fingerprint = strings.ToLower(strings.TrimSpace(request.Fingerprint))
	request.ServerNameOverride = strings.TrimSpace(request.ServerNameOverride)
	request.SpiderX = strings.TrimSpace(request.SpiderX)
	if request.Name == "" {
		request.Name = "Default"
	}
	if request.ClientType == "" {
		request.ClientType = "other"
	}
	if request.DeviceType == "" {
		request.DeviceType = "other"
	}
	if request.FingerprintMode == "" {
		request.FingerprintMode = FingerprintModeAuto
	}
	if request.Fingerprint == "" {
		request.Fingerprint = DefaultAutoFingerprint
	}
	if request.SpiderX == "" {
		request.SpiderX = "/"
	}
}

func validateClientProfileRequest(request UpdateClientProfileRequest) error {
	if len(request.Name) > 100 {
		return errors.New("name must be at most 100 characters")
	}
	if _, ok := allowedClientTypes[request.ClientType]; !ok {
		return errors.New("clientType is not supported")
	}
	if _, ok := allowedDeviceTypes[request.DeviceType]; !ok {
		return errors.New("deviceType is not supported")
	}
	if request.FingerprintMode != FingerprintModeAuto && request.FingerprintMode != FingerprintModeManual {
		return errors.New("fingerprintMode must be auto or manual")
	}
	if _, ok := allowedFingerprints[request.Fingerprint]; !ok {
		return errors.New("fingerprint is not supported")
	}
	if request.ServerNameOverride != "" {
		if len(request.ServerNameOverride) > 253 || strings.ContainsAny(request.ServerNameOverride, " \t\r\n/\\") {
			return errors.New("serverNameOverride is invalid")
		}
	}
	if len(request.SpiderX) > 1024 || !strings.HasPrefix(request.SpiderX, "/") {
		return errors.New("spiderX must start with / and be at most 1024 characters")
	}
	if request.MTU != nil && (*request.MTU < 576 || *request.MTU > 9000) {
		return errors.New("mtu must be between 576 and 9000")
	}
	return nil
}

func resolveClientFingerprint(profile ClientProfile) string {
	if profile.FingerprintMode == FingerprintModeManual {
		if _, ok := allowedFingerprints[profile.Fingerprint]; ok {
			return profile.Fingerprint
		}
	}
	return DefaultAutoFingerprint
}
