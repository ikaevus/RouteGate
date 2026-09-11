package vpnaccounts

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"net/http"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/ikaevus/routegate/backend/internal/audit"
	"github.com/ikaevus/routegate/backend/internal/httpx"
	"github.com/ikaevus/routegate/backend/internal/publicurl"
)

// Access & Devices (RG-116): a VPN account may have several devices/access
// instances, each with its own opaque subscription token, client type, and
// independent revoke/rotate lifecycle. A device is an access/delivery
// credential; it deliberately does not duplicate the account's underlying
// VPN protocol identity (vpn_client_profiles, vpn_account_protocols).

const (
	DeviceStatusActive  = "active"
	DeviceStatusRevoked = "revoked"

	DevicePlatformWindows = "windows"
	DevicePlatformIOS     = "ios"
	DevicePlatformAndroid = "android"
	DevicePlatformMacOS   = "macos"
	DevicePlatformLinux   = "linux"
	DevicePlatformOther   = "other"
)

// allowedDeviceClientTypes is deliberately narrower than allowedClientTypes:
// new devices may only select an officially supported client. V2RayTun,
// V2Box, and anything else use Generic best-effort compatibility instead of a
// bespoke adapter.
var allowedDeviceClientTypes = map[string]struct{}{
	ClientTypeHiddify: {}, ClientTypeV2RayN: {}, ClientTypeV2RayNG: {}, ClientTypeGeneric: {},
}

var allowedDevicePlatforms = map[string]struct{}{
	DevicePlatformWindows: {}, DevicePlatformIOS: {}, DevicePlatformAndroid: {},
	DevicePlatformMacOS: {}, DevicePlatformLinux: {}, DevicePlatformOther: {},
}

type Device struct {
	ID           string     `json:"id"`
	VPNAccountID string     `json:"vpnAccountId"`
	Name         string     `json:"name"`
	ClientType   string     `json:"clientType"`
	DeviceType   string     `json:"deviceType"`
	Status       string     `json:"status"`
	CreatedAt    time.Time  `json:"createdAt"`
	UpdatedAt    time.Time  `json:"updatedAt"`
	LastUsedAt   *time.Time `json:"lastUsedAt,omitempty"`
	RevokedAt    *time.Time `json:"revokedAt,omitempty"`
}

type CreateDeviceInput struct {
	VPNAccountID string
	Name         string
	ClientType   string
	DeviceType   string
}

type UpdateDeviceInput struct {
	Name       *string
	ClientType *string
	DeviceType *string
}

type CreateDeviceRequest struct {
	Name       string `json:"name"`
	ClientType string `json:"clientType"`
	DeviceType string `json:"deviceType"`
}

type UpdateDeviceRequest struct {
	Name       *string `json:"name"`
	ClientType *string `json:"clientType"`
	DeviceType *string `json:"deviceType"`
}

// DeviceAccess is the read model for the Access & Devices panel: the device
// plus its current subscription URL and a product-simplified compatibility
// assessment (Full RouteGate / Compatible / Generic), never the raw token.
type DeviceAccess struct {
	Device          Device                        `json:"device"`
	SubscriptionURL string                        `json:"subscriptionUrl,omitempty"`
	TokenPreview    string                        `json:"tokenPreview,omitempty"`
	TokenExpiresAt  *time.Time                    `json:"tokenExpiresAt,omitempty"`
	TokenLastUsedAt *time.Time                    `json:"tokenLastUsedAt,omitempty"`
	HasActiveToken  bool                          `json:"hasActiveToken"`
	Compatibility   ClientCompatibilityAssessment `json:"compatibility"`
}

// DeviceSubscriptionTokenResponse returns the plaintext subscription token
// exactly once, on device creation or rotation, matching the existing RG-115
// subscription-token issuance contract.
type DeviceSubscriptionTokenResponse struct {
	Device            Device     `json:"device"`
	SubscriptionToken string     `json:"subscriptionToken"`
	TokenPreview      string     `json:"tokenPreview"`
	SubscriptionURL   string     `json:"subscriptionUrl"`
	ExpiresAt         *time.Time `json:"expiresAt,omitempty"`
}

type deviceRepository interface {
	ListDevices(context.Context, string) ([]Device, error)
	GetDevice(context.Context, string, string) (Device, error)
	CreateDevice(context.Context, CreateDeviceInput) (Device, error)
	UpdateDevice(context.Context, string, string, UpdateDeviceInput) (Device, error)
	RevokeDevice(context.Context, string, string) (Device, error)
	GetDeviceByID(context.Context, string) (Device, error)
	MarkDeviceUsed(context.Context, string) error
	CreateDeviceSubscriptionToken(context.Context, string, string, *time.Time) (SubscriptionToken, error)
	GetActiveDeviceSubscriptionToken(context.Context, string) (SubscriptionToken, error)
}

const deviceSelect = `
	SELECT
		id::text,
		vpn_account_id::text,
		name,
		client_type,
		device_type,
		status,
		created_at,
		updated_at,
		last_used_at,
		revoked_at
	FROM vpn_account_devices`

func scanDevice(row scanner) (Device, error) {
	var device Device
	var lastUsedAt, revokedAt sql.NullTime
	if err := row.Scan(
		&device.ID,
		&device.VPNAccountID,
		&device.Name,
		&device.ClientType,
		&device.DeviceType,
		&device.Status,
		&device.CreatedAt,
		&device.UpdatedAt,
		&lastUsedAt,
		&revokedAt,
	); err != nil {
		return Device{}, err
	}
	if lastUsedAt.Valid {
		device.LastUsedAt = &lastUsedAt.Time
	}
	if revokedAt.Valid {
		device.RevokedAt = &revokedAt.Time
	}
	return device, nil
}

func (r *Repository) ListDevices(ctx context.Context, vpnAccountID string) ([]Device, error) {
	rows, err := r.pool.Query(ctx, deviceSelect+`
		WHERE vpn_account_id = $1::uuid
		ORDER BY created_at ASC, id ASC
	`, vpnAccountID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	items := make([]Device, 0)
	for rows.Next() {
		item, err := scanDevice(rows)
		if err != nil {
			return nil, err
		}
		items = append(items, item)
	}
	return items, rows.Err()
}

func (r *Repository) GetDevice(ctx context.Context, vpnAccountID, deviceID string) (Device, error) {
	return scanDevice(r.pool.QueryRow(ctx, deviceSelect+`
		WHERE vpn_account_id = $1::uuid AND id = $2::uuid
	`, vpnAccountID, deviceID))
}

func (r *Repository) GetDeviceByID(ctx context.Context, deviceID string) (Device, error) {
	return scanDevice(r.pool.QueryRow(ctx, deviceSelect+`
		WHERE id = $1::uuid
	`, deviceID))
}

func (r *Repository) CreateDevice(ctx context.Context, input CreateDeviceInput) (Device, error) {
	return scanDevice(r.pool.QueryRow(ctx, `
		INSERT INTO vpn_account_devices (vpn_account_id, name, client_type, device_type)
		SELECT a.id, $2, $3, $4
		FROM vpn_accounts a
		WHERE a.id = $1::uuid
		RETURNING
			id::text, vpn_account_id::text, name, client_type, device_type, status,
			created_at, updated_at, last_used_at, revoked_at
	`, input.VPNAccountID, input.Name, input.ClientType, input.DeviceType))
}

func (r *Repository) UpdateDevice(ctx context.Context, vpnAccountID, deviceID string, input UpdateDeviceInput) (Device, error) {
	return scanDevice(r.pool.QueryRow(ctx, `
		UPDATE vpn_account_devices
		SET
			name = CASE WHEN $3 THEN $4 ELSE name END,
			client_type = CASE WHEN $5 THEN $6 ELSE client_type END,
			device_type = CASE WHEN $7 THEN $8 ELSE device_type END,
			updated_at = now()
		WHERE vpn_account_id = $1::uuid AND id = $2::uuid
		RETURNING
			id::text, vpn_account_id::text, name, client_type, device_type, status,
			created_at, updated_at, last_used_at, revoked_at
	`,
		vpnAccountID, deviceID,
		input.Name != nil, stringValue(input.Name),
		input.ClientType != nil, stringValue(input.ClientType),
		input.DeviceType != nil, stringValue(input.DeviceType),
	))
}

func (r *Repository) RevokeDevice(ctx context.Context, vpnAccountID, deviceID string) (Device, error) {
	tx, err := r.pool.Begin(ctx)
	if err != nil {
		return Device{}, err
	}
	defer tx.Rollback(ctx)

	device, err := scanDevice(tx.QueryRow(ctx, `
		UPDATE vpn_account_devices
		SET status = 'revoked', revoked_at = now(), updated_at = now()
		WHERE vpn_account_id = $1::uuid AND id = $2::uuid AND status = 'active'
		RETURNING
			id::text, vpn_account_id::text, name, client_type, device_type, status,
			created_at, updated_at, last_used_at, revoked_at
	`, vpnAccountID, deviceID))
	if err != nil {
		return Device{}, err
	}

	if _, err := tx.Exec(ctx, `
		UPDATE vpn_subscription_tokens
		SET status = 'revoked', revoked_at = now(), updated_at = now()
		WHERE device_id = $1::uuid AND status = 'active'
	`, deviceID); err != nil {
		return Device{}, err
	}

	if err := tx.Commit(ctx); err != nil {
		return Device{}, err
	}
	return device, nil
}

func (r *Repository) MarkDeviceUsed(ctx context.Context, deviceID string) error {
	_, err := r.pool.Exec(ctx, `
		UPDATE vpn_account_devices SET last_used_at = now() WHERE id = $1::uuid
	`, deviceID)
	return err
}

// CreateDeviceSubscriptionToken revokes the device's previous active token
// (if any) and issues a new one, scoped by device_id rather than account_id.
// A compromised device token can therefore be rotated/revoked without
// affecting any other device on the same VPN account.
func (r *Repository) CreateDeviceSubscriptionToken(ctx context.Context, deviceID, tokenHash string, expiresAt *time.Time) (SubscriptionToken, error) {
	tx, err := r.pool.Begin(ctx)
	if err != nil {
		return SubscriptionToken{}, err
	}
	defer tx.Rollback(ctx)

	var vpnAccountID string
	if err := tx.QueryRow(ctx, `
		SELECT vpn_account_id::text FROM vpn_account_devices WHERE id = $1::uuid AND status = 'active'
	`, deviceID).Scan(&vpnAccountID); err != nil {
		return SubscriptionToken{}, err
	}

	if _, err := tx.Exec(ctx, `
		UPDATE vpn_subscription_tokens
		SET status = 'revoked', revoked_at = now(), updated_at = now()
		WHERE device_id = $1::uuid AND status = 'active'
	`, deviceID); err != nil {
		return SubscriptionToken{}, err
	}

	token, err := scanSubscriptionToken(tx.QueryRow(ctx, `
		INSERT INTO vpn_subscription_tokens (vpn_account_id, device_id, token_hash, expires_at)
		VALUES ($1::uuid, $2::uuid, $3, $4)
		RETURNING
			id::text, vpn_account_id::text, token_hash, status, expires_at,
			last_used_at, revoked_at, created_at, updated_at, COALESCE(device_id::text, '')
	`, vpnAccountID, deviceID, tokenHash, expiresAt))
	if err != nil {
		return SubscriptionToken{}, err
	}
	if err := tx.Commit(ctx); err != nil {
		return SubscriptionToken{}, err
	}
	return token, nil
}

func (r *Repository) GetActiveDeviceSubscriptionToken(ctx context.Context, deviceID string) (SubscriptionToken, error) {
	return scanSubscriptionToken(r.pool.QueryRow(ctx, subscriptionTokenSelect+`
		WHERE device_id = $1::uuid AND status = 'active' AND (expires_at IS NULL OR expires_at > now())
	`, deviceID))
}

// --- HTTP handlers ---

func normalizeDeviceRequest(name, clientType, deviceType string) (string, string, string) {
	name = strings.TrimSpace(name)
	if name == "" {
		name = "Device"
	}
	clientType = strings.ToLower(strings.TrimSpace(clientType))
	if clientType == "" {
		clientType = ClientTypeGeneric
	}
	deviceType = strings.ToLower(strings.TrimSpace(deviceType))
	if deviceType == "" {
		deviceType = DevicePlatformOther
	}
	return name, clientType, deviceType
}

func validateDeviceFields(name, clientType, deviceType string) error {
	if len(name) > 100 {
		return errors.New("name must be at most 100 characters")
	}
	if _, ok := allowedDeviceClientTypes[clientType]; !ok {
		return errors.New("clientType is not supported")
	}
	if _, ok := allowedDevicePlatforms[deviceType]; !ok {
		return errors.New("deviceType is not supported")
	}
	return nil
}

func (h *Handler) devicesRepository() (deviceRepository, bool) {
	repository, ok := h.accounts.(deviceRepository)
	return repository, ok
}

func writeDevicesUnavailable(w http.ResponseWriter) {
	httpx.WriteJSON(w, http.StatusInternalServerError, httpx.Error("devices_unavailable", "Device storage is unavailable."))
}

func writeDeviceNotFound(w http.ResponseWriter) {
	httpx.WriteJSON(w, http.StatusNotFound, httpx.Error("device_not_found", "Device not found."))
}

// deviceCompatibilityAssessment is the one compatibility truth shared with
// public subscription delivery (subscription_delivery.go): a device's
// client-type tier is narrowed by the account's actual effective protocol
// via h.clientConnection, exactly like /sub/<token> does, instead of the
// bare client-type-only tier. If the account's effective protocol cannot be
// resolved (for example it has no server assignment yet), this is
// conservative rather than overclaiming: it never reports a smart-routing or
// setup-required tier without a confirmed protocol.
func (h *Handler) deviceCompatibilityAssessment(ctx context.Context, device Device) ClientCompatibilityAssessment {
	connection, err := h.clientConnection(ctx, device.VPNAccountID)
	if err != nil {
		return clientCompatibilityForResolvedProtocol(device.ClientType, "", false)
	}
	return clientCompatibilityForResolvedProtocol(device.ClientType, connection.Protocol, true)
}

func (h *Handler) deviceAccess(r *http.Request, repository deviceRepository, device Device) (DeviceAccess, error) {
	access := DeviceAccess{Device: device, Compatibility: h.deviceCompatibilityAssessment(r.Context(), device)}
	if device.Status != DeviceStatusActive {
		return access, nil
	}
	token, err := repository.GetActiveDeviceSubscriptionToken(r.Context(), device.ID)
	if errors.Is(err, pgx.ErrNoRows) {
		return access, nil
	}
	if err != nil {
		return DeviceAccess{}, err
	}
	access.HasActiveToken = true
	access.TokenExpiresAt = token.ExpiresAt
	access.TokenLastUsedAt = token.LastUsedAt
	return access, nil
}

func (h *Handler) ListDevices(w http.ResponseWriter, r *http.Request) {
	accountID := r.PathValue("id")
	repository, ok := h.devicesRepository()
	if !ok {
		writeDevicesUnavailable(w)
		return
	}
	if _, err := h.accounts.GetAccountByID(r.Context(), accountID); errors.Is(err, pgx.ErrNoRows) {
		writeAccountNotFound(w)
		return
	} else if err != nil {
		h.databaseError(w, "get vpn account for device list", err)
		return
	}
	devices, err := repository.ListDevices(r.Context(), accountID)
	if err != nil {
		h.databaseError(w, "list vpn account devices", err)
		return
	}
	items := make([]DeviceAccess, 0, len(devices))
	for _, device := range devices {
		access, err := h.deviceAccess(r, repository, device)
		if err != nil {
			h.databaseError(w, "load device access", err)
			return
		}
		items = append(items, access)
	}
	httpx.WriteJSON(w, http.StatusOK, struct {
		Items []DeviceAccess `json:"items"`
	}{Items: items})
}

func (h *Handler) CreateDevice(w http.ResponseWriter, r *http.Request) {
	accountID := r.PathValue("id")
	var request CreateDeviceRequest
	decoder := json.NewDecoder(http.MaxBytesReader(w, r.Body, 8<<10))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&request); err != nil {
		writeInvalidRequest(w, "Request body must be valid JSON.")
		return
	}
	name, clientType, deviceType := normalizeDeviceRequest(request.Name, request.ClientType, request.DeviceType)
	if err := validateDeviceFields(name, clientType, deviceType); err != nil {
		writeInvalidRequest(w, err.Error())
		return
	}

	// RG-116 device access links must always be issued on RouteGate's
	// canonical PublicURL (see deviceCanonicalOrigin) - preflight it before
	// creating anything, so a missing/invalid PublicURL never leaves behind
	// a device row with no usable access link.
	if _, err := h.deviceCanonicalOrigin(); err != nil {
		writeDevicePublicURLError(w, err)
		return
	}

	repository, ok := h.devicesRepository()
	if !ok {
		writeDevicesUnavailable(w)
		return
	}
	if _, err := h.accounts.GetAccountByID(r.Context(), accountID); errors.Is(err, pgx.ErrNoRows) {
		writeAccountNotFound(w)
		return
	} else if err != nil {
		h.databaseError(w, "get vpn account for device create", err)
		return
	}

	device, err := repository.CreateDevice(r.Context(), CreateDeviceInput{
		VPNAccountID: accountID, Name: name, ClientType: clientType, DeviceType: deviceType,
	})
	if err != nil {
		h.databaseError(w, "create vpn account device", err)
		return
	}

	response, err := h.issueDeviceSubscriptionToken(r, repository, device, nil)
	if err != nil {
		h.writeDevicePublicURLOrDatabaseError(w, "create device subscription token", err)
		return
	}

	h.recordAudit(r, audit.EventInput{
		Action:       "vpn_account_device.created",
		ResourceType: "vpn_account",
		ResourceID:   accountID,
		Result:       audit.ResultSuccess,
		Metadata: map[string]any{
			"device_id":     device.ID,
			"client_type":   device.ClientType,
			"device_type":   device.DeviceType,
			"token_preview": response.TokenPreview,
		},
	})
	httpx.WriteJSON(w, http.StatusCreated, response)
}

func (h *Handler) UpdateDevice(w http.ResponseWriter, r *http.Request) {
	accountID := r.PathValue("id")
	deviceID := r.PathValue("deviceId")
	var request UpdateDeviceRequest
	decoder := json.NewDecoder(http.MaxBytesReader(w, r.Body, 8<<10))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&request); err != nil {
		writeInvalidRequest(w, "Request body must be valid JSON.")
		return
	}

	repository, ok := h.devicesRepository()
	if !ok {
		writeDevicesUnavailable(w)
		return
	}
	existing, err := repository.GetDevice(r.Context(), accountID, deviceID)
	if errors.Is(err, pgx.ErrNoRows) {
		writeDeviceNotFound(w)
		return
	} else if err != nil {
		h.databaseError(w, "get vpn account device", err)
		return
	}

	update := UpdateDeviceInput{}
	name, clientType, deviceType := existing.Name, existing.ClientType, existing.DeviceType
	if request.Name != nil {
		name = strings.TrimSpace(*request.Name)
	}
	if request.ClientType != nil {
		clientType = strings.ToLower(strings.TrimSpace(*request.ClientType))
	}
	if request.DeviceType != nil {
		deviceType = strings.ToLower(strings.TrimSpace(*request.DeviceType))
	}
	name, clientType, deviceType = normalizeDeviceRequest(name, clientType, deviceType)
	if err := validateDeviceFields(name, clientType, deviceType); err != nil {
		writeInvalidRequest(w, err.Error())
		return
	}
	update.Name = &name
	update.ClientType = &clientType
	update.DeviceType = &deviceType

	device, err := repository.UpdateDevice(r.Context(), accountID, deviceID, update)
	if errors.Is(err, pgx.ErrNoRows) {
		writeDeviceNotFound(w)
		return
	}
	if err != nil {
		h.databaseError(w, "update vpn account device", err)
		return
	}

	h.recordAudit(r, audit.EventInput{
		Action:       "vpn_account_device.updated",
		ResourceType: "vpn_account",
		ResourceID:   accountID,
		Result:       audit.ResultSuccess,
		Metadata: map[string]any{
			"device_id":   device.ID,
			"client_type": device.ClientType,
			"device_type": device.DeviceType,
		},
	})

	access, err := h.deviceAccess(r, repository, device)
	if err != nil {
		h.databaseError(w, "load device access", err)
		return
	}
	httpx.WriteJSON(w, http.StatusOK, access)
}

func (h *Handler) RevokeDevice(w http.ResponseWriter, r *http.Request) {
	accountID := r.PathValue("id")
	deviceID := r.PathValue("deviceId")
	repository, ok := h.devicesRepository()
	if !ok {
		writeDevicesUnavailable(w)
		return
	}
	device, err := repository.RevokeDevice(r.Context(), accountID, deviceID)
	if errors.Is(err, pgx.ErrNoRows) {
		writeDeviceNotFound(w)
		return
	}
	if err != nil {
		h.databaseError(w, "revoke vpn account device", err)
		return
	}

	h.recordAudit(r, audit.EventInput{
		Action:       "vpn_account_device.revoked",
		ResourceType: "vpn_account",
		ResourceID:   accountID,
		Result:       audit.ResultSuccess,
		Metadata: map[string]any{
			"device_id": device.ID,
		},
	})
	httpx.WriteJSON(w, http.StatusOK, DeviceAccess{Device: device, Compatibility: h.deviceCompatibilityAssessment(r.Context(), device)})
}

func (h *Handler) RotateDeviceSubscriptionToken(w http.ResponseWriter, r *http.Request) {
	accountID := r.PathValue("id")
	deviceID := r.PathValue("deviceId")
	var request CreateSubscriptionTokenRequest
	if err := decodeOptionalJSON(r.Body, &request); err != nil {
		writeInvalidRequest(w, "Request body must be valid JSON.")
		return
	}
	if subscriptionTokenExpired(SubscriptionToken{ExpiresAt: request.ExpiresAt}, time.Now()) {
		writeInvalidRequest(w, "expiresAt must be in the future")
		return
	}

	repository, ok := h.devicesRepository()
	if !ok {
		writeDevicesUnavailable(w)
		return
	}
	device, err := repository.GetDevice(r.Context(), accountID, deviceID)
	if errors.Is(err, pgx.ErrNoRows) {
		writeDeviceNotFound(w)
		return
	} else if err != nil {
		h.databaseError(w, "get vpn account device for rotate", err)
		return
	}
	if device.Status != DeviceStatusActive {
		writeDeviceNotFound(w)
		return
	}

	response, err := h.issueDeviceSubscriptionToken(r, repository, device, request.ExpiresAt)
	if err != nil {
		h.writeDevicePublicURLOrDatabaseError(w, "rotate device subscription token", err)
		return
	}

	h.recordAudit(r, audit.EventInput{
		Action:       "vpn_account_device.token_rotated",
		ResourceType: "vpn_account",
		ResourceID:   accountID,
		Result:       audit.ResultSuccess,
		Metadata: map[string]any{
			"device_id":     device.ID,
			"token_preview": response.TokenPreview,
		},
	})
	httpx.WriteJSON(w, http.StatusCreated, response)
}

// issueDeviceSubscriptionToken creates (or rotates) a device's subscription
// token. It resolves the canonical PublicURL origin *before* touching
// CreateDeviceSubscriptionToken (which transactionally revokes the device's
// previous active token, if any, before inserting the new one): if
// PublicURL is missing or invalid, this fails before any existing token is
// revoked, so a rotate can never destroy working access just because the
// server's PublicURL setting is currently broken.
func (h *Handler) issueDeviceSubscriptionToken(r *http.Request, repository deviceRepository, device Device, expiresAt *time.Time) (DeviceSubscriptionTokenResponse, error) {
	canonicalOrigin, err := h.deviceCanonicalOrigin()
	if err != nil {
		return DeviceSubscriptionTokenResponse{}, err
	}
	rawToken, err := h.generateSubscriptionToken()
	if err != nil {
		return DeviceSubscriptionTokenResponse{}, err
	}
	token, err := repository.CreateDeviceSubscriptionToken(r.Context(), device.ID, HashSubscriptionToken(rawToken), expiresAt)
	if err != nil {
		return DeviceSubscriptionTokenResponse{}, err
	}
	return DeviceSubscriptionTokenResponse{
		Device:            device,
		SubscriptionToken: rawToken,
		TokenPreview:      MaskSubscriptionToken(rawToken),
		SubscriptionURL:   canonicalOrigin + "/sub/" + rawToken,
		ExpiresAt:         token.ExpiresAt,
	}, nil
}

// deviceCanonicalOrigin resolves RouteGate's configured PublicURL as the
// mandatory origin for RG-116 device access links. Unlike the legacy
// account-level subscriptionURL, this never falls back to the incoming
// request's Host/X-Forwarded-* headers: a device credential's origin must
// always be one delivery.extractCanonicalSubscriptionToken (Device Send's
// own validator) will accept, and RouteGate must never issue a device link
// its own validator later rejects. A missing or invalid PublicURL is
// therefore a hard failure for device creation/rotation, not a silent
// fallback - see writeDevicePublicURLError.
func (h *Handler) deviceCanonicalOrigin() (string, error) {
	return publicurl.Normalize(h.publicURL)
}

// writeDevicePublicURLError writes the stable public_url_missing/
// public_url_invalid API error codes (matching the ones the delivery
// package already uses for the same underlying publicurl policy) for a
// device request rejected before anything was created or changed.
func writeDevicePublicURLError(w http.ResponseWriter, err error) {
	if errors.Is(err, publicurl.ErrMissing) {
		httpx.WriteJSON(w, http.StatusConflict, httpx.Error("public_url_missing", "Configure RouteGate's canonical public HTTPS URL before creating or rotating device access links."))
		return
	}
	httpx.WriteJSON(w, http.StatusConflict, httpx.Error("public_url_invalid", "RouteGate's configured public URL is invalid. It must be an HTTPS origin with no path, query, or fragment."))
}

// writeDevicePublicURLOrDatabaseError distinguishes issueDeviceSubscriptionToken's
// two possible failure modes: a publicurl policy error (device link/token
// generation never touched, nothing to roll back) versus a genuine
// database error.
func (h *Handler) writeDevicePublicURLOrDatabaseError(w http.ResponseWriter, operation string, err error) {
	if errors.Is(err, publicurl.ErrMissing) || errors.Is(err, publicurl.ErrInvalid) {
		writeDevicePublicURLError(w, err)
		return
	}
	h.databaseError(w, operation, err)
}
