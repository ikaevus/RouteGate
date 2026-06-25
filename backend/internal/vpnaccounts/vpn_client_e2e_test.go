package vpnaccounts

import (
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
)

const vpnClientE2EToken = "fixed-client-token"

type vpnClientE2ERepository struct {
	account Account
	server  SubscriptionServer
	token   SubscriptionToken

	markedUsedTokenID string
}

func newVPNClientE2ERepository() *vpnClientE2ERepository {
	return &vpnClientE2ERepository{
		server: SubscriptionServer{
			ID:                "server-e2e",
			Name:              "Finland VPS",
			Hostname:          "fi.routegate.example",
			PublicIP:          "203.0.113.10",
			Location:          "Finland",
			Provider:          "RouteGate Test",
			VLESSPort:         443,
			VLESSFlow:         "xtls-rprx-vision",
			VLESSNetwork:      "tcp",
			RealityPublicKey:  "jNXHt1yRo0vDuchQlIP6Z0ZvjT3KtzVI-T4E7RoLJS0",
			RealityShortID:    "0123456789abcdef",
			RealityServerName: "www.example.com",
		},
	}
}

func (r *vpnClientE2ERepository) CreateAccount(_ context.Context, input CreateAccountInput) (Account, error) {
	now := time.Now().UTC()
	status := input.Status
	if status == "" {
		status = StatusCreated
	}
	serverID := input.ServerID
	if serverID == "" {
		serverID = r.server.ID
	}
	r.account = Account{
		ID:          "account-e2e",
		DisplayName: input.DisplayName,
		Email:       input.Email,
		Status:      status,
		ExpiresAt:   input.ExpiresAt,
		MaxDevices:  input.MaxDevices,
		ServerID:    serverID,
		VLESSUUID:   "11111111-1111-1111-1111-111111111111",
		CreatedAt:   now,
		UpdatedAt:   now,
	}
	return r.account, nil
}

func (r *vpnClientE2ERepository) ListAccounts(context.Context, AccountFilter) ([]Account, error) {
	if r.account.ID == "" {
		return []Account{}, nil
	}
	return []Account{r.account}, nil
}

func (r *vpnClientE2ERepository) GetAccountByID(_ context.Context, id string) (Account, error) {
	if r.account.ID == "" || r.account.ID != id {
		return Account{}, pgx.ErrNoRows
	}
	return r.account, nil
}

func (r *vpnClientE2ERepository) UpdateAccount(_ context.Context, id string, input UpdateAccountInput) (Account, error) {
	if r.account.ID == "" || r.account.ID != id {
		return Account{}, pgx.ErrNoRows
	}
	if input.DisplayName != nil {
		r.account.DisplayName = *input.DisplayName
	}
	if input.Email != nil {
		r.account.Email = *input.Email
	}
	if input.Status != nil {
		r.account.Status = *input.Status
	}
	if input.ExpiresAt != nil {
		r.account.ExpiresAt = input.ExpiresAt
	}
	if input.MaxDevices != nil {
		r.account.MaxDevices = input.MaxDevices
	}
	if input.ServerID != nil {
		r.account.ServerID = *input.ServerID
	}
	r.account.UpdatedAt = time.Now().UTC()
	return r.account, nil
}

func (r *vpnClientE2ERepository) SetAccountStatus(_ context.Context, id string, status string) (Account, error) {
	if r.account.ID == "" || r.account.ID != id {
		return Account{}, pgx.ErrNoRows
	}
	r.account.Status = status
	r.account.UpdatedAt = time.Now().UTC()
	return r.account, nil
}

func (r *vpnClientE2ERepository) DeleteAccount(_ context.Context, id string) error {
	if r.account.ID == "" || r.account.ID != id {
		return pgx.ErrNoRows
	}
	r.account = Account{}
	return nil
}

func (r *vpnClientE2ERepository) CreateSubscriptionToken(_ context.Context, input CreateSubscriptionTokenInput) (SubscriptionToken, error) {
	now := time.Now().UTC()
	r.token = SubscriptionToken{
		ID:           "token-e2e",
		VPNAccountID: input.VPNAccountID,
		TokenHash:    input.TokenHash,
		Status:       SubscriptionTokenStatusActive,
		ExpiresAt:    input.ExpiresAt,
		CreatedAt:    now,
		UpdatedAt:    now,
	}
	return r.token, nil
}

func (r *vpnClientE2ERepository) RevokeActiveSubscriptionTokens(_ context.Context, vpnAccountID string) error {
	if r.token.ID == "" || r.token.VPNAccountID != vpnAccountID || r.token.Status != SubscriptionTokenStatusActive {
		return pgx.ErrNoRows
	}
	now := time.Now().UTC()
	r.token.Status = SubscriptionTokenStatusRevoked
	r.token.RevokedAt = &now
	r.token.UpdatedAt = now
	return nil
}

func (r *vpnClientE2ERepository) GetActiveSubscriptionTokenByHash(_ context.Context, vpnAccountID string, tokenHash string) (SubscriptionToken, error) {
	if r.token.ID == "" || r.token.VPNAccountID != vpnAccountID || r.token.TokenHash != tokenHash || r.token.Status != SubscriptionTokenStatusActive {
		return SubscriptionToken{}, pgx.ErrNoRows
	}
	return r.token, nil
}

func (r *vpnClientE2ERepository) FindActiveSubscriptionTokenByHash(_ context.Context, tokenHash string) (SubscriptionToken, error) {
	if r.token.ID == "" || r.token.TokenHash != tokenHash || r.token.Status != SubscriptionTokenStatusActive {
		return SubscriptionToken{}, pgx.ErrNoRows
	}
	return r.token, nil
}

func (r *vpnClientE2ERepository) GetSubscriptionProfileByAccountID(_ context.Context, id string) (SubscriptionProfile, error) {
	if r.account.ID == "" || r.account.ID != id {
		return SubscriptionProfile{}, pgx.ErrNoRows
	}
	return SubscriptionProfile{
		Account: r.account,
		Server:  &r.server,
		Credentials: SubscriptionCredentials{
			VLESS: VLESSCredentials{
				UUID:    r.account.VLESSUUID,
				Flow:    r.server.VLESSFlow,
				Network: r.server.VLESSNetwork,
			},
			Reality: RealityCredentials{
				PublicKey:  r.server.RealityPublicKey,
				ShortID:    r.server.RealityShortID,
				ServerName: r.server.RealityServerName,
			},
		},
	}, nil
}

func (r *vpnClientE2ERepository) MarkSubscriptionTokenUsed(_ context.Context, id string) error {
	if r.token.ID == "" || r.token.ID != id {
		return pgx.ErrNoRows
	}
	now := time.Now().UTC()
	r.token.LastUsedAt = &now
	r.token.UpdatedAt = now
	r.markedUsedTokenID = id
	return nil
}

func newVPNClientE2EHandler(repo *vpnClientE2ERepository) *Handler {
	return &Handler{
		logger:                    slog.New(slog.NewTextHandler(io.Discard, nil)),
		accounts:                  repo,
		generateSubscriptionToken: func() (string, error) { return vpnClientE2EToken, nil },
	}
}

func TestVPNClientSubscriptionE2EFlow(t *testing.T) {
	repo := newVPNClientE2ERepository()
	handler := newVPNClientE2EHandler(repo)

	createRequest := httptest.NewRequest(http.MethodPost, "/api/v1/vpn-accounts", strings.NewReader(`{
		"displayName": "Alice Client",
		"email": "alice@example.com",
		"serverId": "server-e2e"
	}`))
	createResponse := httptest.NewRecorder()
	handler.Create(createResponse, createRequest)
	requireHTTPStatus(t, createResponse, http.StatusCreated)

	var account Account
	decodeJSON(t, createResponse, &account)
	if account.ID == "" || account.Status != StatusCreated || account.VLESSUUID == "" {
		t.Fatalf("unexpected created account: %+v", account)
	}

	activateRequest := httptest.NewRequest(http.MethodPost, "/api/v1/vpn-accounts/"+account.ID+"/activate", nil)
	activateRequest.SetPathValue("id", account.ID)
	activateResponse := httptest.NewRecorder()
	handler.Activate(activateResponse, activateRequest)
	requireHTTPStatus(t, activateResponse, http.StatusOK)

	tokenRequest := httptest.NewRequest(http.MethodPost, "/api/v1/vpn-accounts/"+account.ID+"/subscription-token", nil)
	tokenRequest.SetPathValue("id", account.ID)
	tokenRequest.Header.Set("X-Forwarded-Proto", "https")
	tokenRequest.Header.Set("X-Forwarded-Host", "client.routegate.example")
	tokenResponse := httptest.NewRecorder()
	handler.CreateSubscriptionToken(tokenResponse, tokenRequest)
	requireHTTPStatus(t, tokenResponse, http.StatusCreated)

	var tokenPayload SubscriptionTokenResponse
	decodeJSON(t, tokenResponse, &tokenPayload)
	expectedSubscriptionURL := "https://client.routegate.example/api/v1/subscriptions/" + vpnClientE2EToken
	if tokenPayload.SubscriptionToken != vpnClientE2EToken || tokenPayload.SubscriptionURL != expectedSubscriptionURL {
		t.Fatalf("unexpected subscription token payload: %+v", tokenPayload)
	}
	if repo.token.TokenHash != HashSubscriptionToken(vpnClientE2EToken) {
		t.Fatalf("expected stored token hash, got %q", repo.token.TokenHash)
	}

	qrRequest := httptest.NewRequest(http.MethodGet, "/api/v1/vpn-accounts/"+account.ID+"/qr?token="+vpnClientE2EToken, nil)
	qrRequest.SetPathValue("id", account.ID)
	qrRequest.Header.Set("X-Forwarded-Proto", "https")
	qrRequest.Header.Set("X-Forwarded-Host", "client.routegate.example")
	qrResponse := httptest.NewRecorder()
	handler.GetSubscriptionQRCode(qrResponse, qrRequest)
	requireHTTPStatus(t, qrResponse, http.StatusOK)

	var qrPayload SubscriptionQRCodeResponse
	decodeJSON(t, qrResponse, &qrPayload)
	if qrPayload.QRText != expectedSubscriptionURL || qrPayload.Format != "subscription-url" {
		t.Fatalf("unexpected qr payload: %+v", qrPayload)
	}

	publicRequest := httptest.NewRequest(http.MethodGet, "/api/v1/subscriptions/"+vpnClientE2EToken, nil)
	publicRequest.SetPathValue("token", vpnClientE2EToken)
	publicResponse := httptest.NewRecorder()
	handler.GetPublicSubscription(publicResponse, publicRequest)
	requireHTTPStatus(t, publicResponse, http.StatusOK)

	var subscription PublicSubscriptionResponse
	decodeJSON(t, publicResponse, &subscription)
	if subscription.Format != "routegate.subscription.v1" || subscription.Status != "ok" {
		t.Fatalf("unexpected subscription envelope: %+v", subscription)
	}
	if subscription.Account.ID != account.ID || subscription.Account.Status != StatusActive {
		t.Fatalf("unexpected subscription account: %+v", subscription.Account)
	}
	if subscription.Server == nil || subscription.Server.Endpoint != "203.0.113.10" {
		t.Fatalf("unexpected subscription server: %+v", subscription.Server)
	}
	if subscription.Config.Format != ClientConfigFormat || subscription.Config.Status != "rendered" || subscription.Config.Rendered == nil {
		t.Fatalf("unexpected subscription config metadata: %+v", subscription.Config)
	}
	if subscription.Config.Rendered.Format != SingBoxClientConfigFormat {
		t.Fatalf("unexpected rendered config format: %q", subscription.Config.Rendered.Format)
	}

	config := subscription.Config.Rendered.Content
	if len(config.Inbounds) == 0 || config.Inbounds[0].Type != "mixed" {
		t.Fatalf("expected mixed client inbound, got %+v", config.Inbounds)
	}
	if len(config.Outbounds) < 2 {
		t.Fatalf("expected vless and direct outbounds, got %+v", config.Outbounds)
	}
	vless := config.Outbounds[0]
	if vless.Type != "vless" || vless.Server != "203.0.113.10" || vless.ServerPort != 443 || vless.UUID != account.VLESSUUID {
		t.Fatalf("unexpected vless outbound: %+v", vless)
	}
	if vless.Flow != "xtls-rprx-vision" || vless.Network != "tcp" {
		t.Fatalf("unexpected vless transport fields: %+v", vless)
	}
	if vless.TLS == nil || !vless.TLS.Enabled || vless.TLS.Reality == nil || !vless.TLS.Reality.Enabled {
		t.Fatalf("expected Reality TLS config, got %+v", vless.TLS)
	}
	if vless.TLS.ServerName != "www.example.com" || vless.TLS.Reality.PublicKey != repo.server.RealityPublicKey || vless.TLS.Reality.ShortID != repo.server.RealityShortID {
		t.Fatalf("unexpected Reality TLS config: %+v", vless.TLS)
	}
	if config.Route.Final != "routegate-out" {
		t.Fatalf("expected final routegate-out, got %q", config.Route.Final)
	}
	if repo.markedUsedTokenID != repo.token.ID {
		t.Fatalf("expected token %q marked used, got %q", repo.token.ID, repo.markedUsedTokenID)
	}
}

func requireHTTPStatus(t *testing.T, response *httptest.ResponseRecorder, want int) {
	t.Helper()
	if response.Code != want {
		t.Fatalf("status = %d, want %d; body=%s", response.Code, want, response.Body.String())
	}
}

func decodeJSON(t *testing.T, response *httptest.ResponseRecorder, target any) {
	t.Helper()
	if err := json.NewDecoder(response.Body).Decode(target); err != nil {
		t.Fatalf("decode response: %v; body=%s", err, response.Body.String())
	}
}
