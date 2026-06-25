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

type fakeAccountRepository struct {
	createdInput CreateAccountInput
	created      Account
	statusID     string
	status       string
	statusResult Account
	getErr       error

	createdTokenInput CreateSubscriptionTokenInput
	createdToken      SubscriptionToken
	revokeTokenID     string
	revokeTokenErr    error
	getTokenAccountID string
	getTokenHash      string
	getTokenErr       error
	findTokenHash     string
	findToken         SubscriptionToken
	findTokenErr      error
	profile           SubscriptionProfile
	profileErr        error
	usedTokenID       string
	markUsedErr       error
}

func (f *fakeAccountRepository) CreateAccount(_ context.Context, input CreateAccountInput) (Account, error) {
	f.createdInput = input
	if f.created.ID != "" {
		return f.created, nil
	}
	return Account{ID: "account-1", DisplayName: input.DisplayName, Status: input.Status, CreatedAt: time.Now(), UpdatedAt: time.Now()}, nil
}

func (f *fakeAccountRepository) ListAccounts(context.Context, AccountFilter) ([]Account, error) {
	return nil, nil
}

func (f *fakeAccountRepository) GetAccountByID(context.Context, string) (Account, error) {
	if f.getErr != nil {
		return Account{}, f.getErr
	}
	return Account{ID: "account-1", DisplayName: "Demo", Status: StatusActive, CreatedAt: time.Now(), UpdatedAt: time.Now()}, nil
}

func (f *fakeAccountRepository) UpdateAccount(context.Context, string, UpdateAccountInput) (Account, error) {
	return Account{}, nil
}

func (f *fakeAccountRepository) SetAccountStatus(_ context.Context, id string, status string) (Account, error) {
	f.statusID = id
	f.status = status
	if f.statusResult.ID != "" {
		return f.statusResult, nil
	}
	return Account{ID: id, DisplayName: "Demo", Status: status, CreatedAt: time.Now(), UpdatedAt: time.Now()}, nil
}

func (f *fakeAccountRepository) DeleteAccount(context.Context, string) error {
	return nil
}

func (f *fakeAccountRepository) CreateSubscriptionToken(_ context.Context, input CreateSubscriptionTokenInput) (SubscriptionToken, error) {
	f.createdTokenInput = input
	if f.createdToken.ID != "" {
		return f.createdToken, nil
	}
	return SubscriptionToken{
		ID:           "token-1",
		VPNAccountID: input.VPNAccountID,
		TokenHash:    input.TokenHash,
		Status:       SubscriptionTokenStatusActive,
		ExpiresAt:    input.ExpiresAt,
		CreatedAt:    time.Now(),
		UpdatedAt:    time.Now(),
	}, nil
}

func (f *fakeAccountRepository) RevokeActiveSubscriptionTokens(_ context.Context, vpnAccountID string) error {
	f.revokeTokenID = vpnAccountID
	return f.revokeTokenErr
}

func (f *fakeAccountRepository) GetActiveSubscriptionTokenByHash(_ context.Context, vpnAccountID string, tokenHash string) (SubscriptionToken, error) {
	f.getTokenAccountID = vpnAccountID
	f.getTokenHash = tokenHash
	if f.getTokenErr != nil {
		return SubscriptionToken{}, f.getTokenErr
	}
	return SubscriptionToken{ID: "token-1", VPNAccountID: vpnAccountID, TokenHash: tokenHash, Status: SubscriptionTokenStatusActive}, nil
}

func (f *fakeAccountRepository) FindActiveSubscriptionTokenByHash(_ context.Context, tokenHash string) (SubscriptionToken, error) {
	f.findTokenHash = tokenHash
	if f.findTokenErr != nil {
		return SubscriptionToken{}, f.findTokenErr
	}
	if f.findToken.ID != "" {
		return f.findToken, nil
	}
	return SubscriptionToken{ID: "token-1", VPNAccountID: "account-1", TokenHash: tokenHash, Status: SubscriptionTokenStatusActive}, nil
}

func (f *fakeAccountRepository) GetSubscriptionProfileByAccountID(_ context.Context, id string) (SubscriptionProfile, error) {
	if f.profileErr != nil {
		return SubscriptionProfile{}, f.profileErr
	}
	if f.profile.Account.ID != "" {
		return f.profile, nil
	}
	return SubscriptionProfile{
		Account: Account{ID: id, DisplayName: "Demo", Status: StatusActive, ServerID: "server-1", VLESSUUID: testVLESSUUID},
		Server: &SubscriptionServer{
			ID:           "server-1",
			Name:         "Finland",
			PublicIP:     "203.0.113.10",
			Location:     "Finland",
			Provider:     "Demo",
			VLESSPort:    defaultSingBoxServerPort,
			VLESSNetwork: "tcp",
		},
	}, nil
}

func (f *fakeAccountRepository) MarkSubscriptionTokenUsed(_ context.Context, id string) error {
	f.usedTokenID = id
	return f.markUsedErr
}

func newTestHandler(repo *fakeAccountRepository) *Handler {
	return &Handler{
		logger:                    slog.New(slog.NewTextHandler(io.Discard, nil)),
		accounts:                  repo,
		generateSubscriptionToken: func() (string, error) { return "fixed-token", nil },
	}
}

func TestCreateRejectsMissingDisplayName(t *testing.T) {
	handler := newTestHandler(&fakeAccountRepository{})
	request := httptest.NewRequest(http.MethodPost, "/api/v1/vpn-accounts", strings.NewReader("{\"email\":\"user@example.com\"}"))
	response := httptest.NewRecorder()

	handler.Create(response, request)

	if response.Code != http.StatusBadRequest {
		t.Fatalf("expected status %d, got %d", http.StatusBadRequest, response.Code)
	}
}

func TestCreateDefaultsStatusToCreated(t *testing.T) {
	repo := &fakeAccountRepository{}
	handler := newTestHandler(repo)
	request := httptest.NewRequest(http.MethodPost, "/api/v1/vpn-accounts", strings.NewReader("{\"displayName\":\"Alice\",\"email\":\"alice@example.com\"}"))
	response := httptest.NewRecorder()

	handler.Create(response, request)

	if response.Code != http.StatusCreated {
		t.Fatalf("expected status %d, got %d", http.StatusCreated, response.Code)
	}
	if repo.createdInput.Status != StatusCreated {
		t.Fatalf("expected default status %q, got %q", StatusCreated, repo.createdInput.Status)
	}
}

func TestSuspendSetsSuspendedStatus(t *testing.T) {
	repo := &fakeAccountRepository{}
	handler := newTestHandler(repo)
	request := httptest.NewRequest(http.MethodPost, "/api/v1/vpn-accounts/account-1/suspend", nil)
	request.SetPathValue("id", "account-1")
	response := httptest.NewRecorder()

	handler.Suspend(response, request)

	if response.Code != http.StatusOK {
		t.Fatalf("expected status %d, got %d", http.StatusOK, response.Code)
	}
	if repo.statusID != "account-1" || repo.status != StatusSuspended {
		t.Fatalf("expected suspended account-1, got id=%q status=%q", repo.statusID, repo.status)
	}
}

func TestGetReturnsNotFound(t *testing.T) {
	repo := &fakeAccountRepository{getErr: pgx.ErrNoRows}
	handler := newTestHandler(repo)
	request := httptest.NewRequest(http.MethodGet, "/api/v1/vpn-accounts/missing", nil)
	request.SetPathValue("id", "missing")
	response := httptest.NewRecorder()

	handler.Get(response, request)

	if response.Code != http.StatusNotFound {
		t.Fatalf("expected status %d, got %d", http.StatusNotFound, response.Code)
	}
}

func TestCreateSubscriptionTokenReturnsRawTokenOnce(t *testing.T) {
	repo := &fakeAccountRepository{}
	handler := newTestHandler(repo)
	request := httptest.NewRequest(http.MethodPost, "/api/v1/vpn-accounts/account-1/subscription-token", strings.NewReader("{}"))
	request.SetPathValue("id", "account-1")
	request.Header.Set("X-Forwarded-Proto", "https")
	request.Header.Set("X-Forwarded-Host", "routegate.example")
	response := httptest.NewRecorder()

	handler.CreateSubscriptionToken(response, request)

	if response.Code != http.StatusCreated {
		t.Fatalf("expected status %d, got %d", http.StatusCreated, response.Code)
	}
	if repo.createdTokenInput.VPNAccountID != "account-1" {
		t.Fatalf("expected account-1 token input, got %q", repo.createdTokenInput.VPNAccountID)
	}
	if repo.createdTokenInput.TokenHash != HashSubscriptionToken("fixed-token") {
		t.Fatalf("expected hashed token storage, got %q", repo.createdTokenInput.TokenHash)
	}

	var body SubscriptionTokenResponse
	if err := json.NewDecoder(response.Body).Decode(&body); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if body.SubscriptionToken != "fixed-token" {
		t.Fatalf("expected raw token in creation response, got %q", body.SubscriptionToken)
	}
	if body.SubscriptionURL != "https://routegate.example/api/v1/subscriptions/fixed-token" {
		t.Fatalf("unexpected subscription URL %q", body.SubscriptionURL)
	}
}

func TestGetSubscriptionQRCodeRejectsMissingToken(t *testing.T) {
	handler := newTestHandler(&fakeAccountRepository{})
	request := httptest.NewRequest(http.MethodGet, "/api/v1/vpn-accounts/account-1/qr", nil)
	request.SetPathValue("id", "account-1")
	response := httptest.NewRecorder()

	handler.GetSubscriptionQRCode(response, request)

	if response.Code != http.StatusBadRequest {
		t.Fatalf("expected status %d, got %d", http.StatusBadRequest, response.Code)
	}
}

func TestGetSubscriptionQRCodeReturnsPayloadForValidToken(t *testing.T) {
	repo := &fakeAccountRepository{}
	handler := newTestHandler(repo)
	request := httptest.NewRequest(http.MethodGet, "/api/v1/vpn-accounts/account-1/qr?token=fixed-token", nil)
	request.SetPathValue("id", "account-1")
	request.Header.Set("X-Forwarded-Proto", "https")
	request.Header.Set("X-Forwarded-Host", "routegate.example")
	response := httptest.NewRecorder()

	handler.GetSubscriptionQRCode(response, request)

	if response.Code != http.StatusOK {
		t.Fatalf("expected status %d, got %d", http.StatusOK, response.Code)
	}
	if repo.getTokenAccountID != "account-1" || repo.getTokenHash != HashSubscriptionToken("fixed-token") {
		t.Fatalf("expected validated token, got account=%q hash=%q", repo.getTokenAccountID, repo.getTokenHash)
	}

	var body SubscriptionQRCodeResponse
	if err := json.NewDecoder(response.Body).Decode(&body); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if body.QRText != "https://routegate.example/api/v1/subscriptions/fixed-token" || body.Format != "subscription-url" {
		t.Fatalf("unexpected qr response: %+v", body)
	}
}

func TestGetPublicSubscriptionMarksTokenUsed(t *testing.T) {
	repo := &fakeAccountRepository{findToken: SubscriptionToken{ID: "token-1", VPNAccountID: "account-1", Status: SubscriptionTokenStatusActive}}
	handler := newTestHandler(repo)
	request := httptest.NewRequest(http.MethodGet, "/api/v1/subscriptions/fixed-token", nil)
	request.SetPathValue("token", "fixed-token")
	response := httptest.NewRecorder()

	handler.GetPublicSubscription(response, request)

	if response.Code != http.StatusOK {
		t.Fatalf("expected status %d, got %d", http.StatusOK, response.Code)
	}
	if repo.findTokenHash != HashSubscriptionToken("fixed-token") {
		t.Fatalf("expected public lookup by token hash, got %q", repo.findTokenHash)
	}
	if repo.usedTokenID != "token-1" {
		t.Fatalf("expected token-1 marked used, got %q", repo.usedTokenID)
	}

	var body PublicSubscriptionResponse
	if err := json.NewDecoder(response.Body).Decode(&body); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if body.Format != "routegate.subscription.v1" || body.Config.Format != ClientConfigFormat || body.Config.Status != "rendered" {
		t.Fatalf("unexpected subscription response: %+v", body)
	}
	if body.Server == nil || body.Server.Endpoint != "203.0.113.10" {
		t.Fatalf("expected server endpoint in subscription response, got %+v", body.Server)
	}
	if body.Config.Rendered == nil {
		t.Fatal("expected rendered sing-box config in subscription response")
	}
	if body.Config.Rendered.Format != SingBoxClientConfigFormat {
		t.Fatalf("expected rendered format %q, got %q", SingBoxClientConfigFormat, body.Config.Rendered.Format)
	}
	config := body.Config.Rendered.Content
	if len(config.Outbounds) == 0 {
		t.Fatal("expected sing-box outbounds")
	}
	if config.Outbounds[0].Type != "vless" || config.Outbounds[0].Server != "203.0.113.10" || config.Outbounds[0].ServerPort != 443 || config.Outbounds[0].UUID != testVLESSUUID {
		t.Fatalf("unexpected sing-box vless outbound: %+v", config.Outbounds[0])
	}
	if config.Route.Final != "routegate-out" {
		t.Fatalf("expected route final routegate-out, got %q", config.Route.Final)
	}
}

func TestGetPublicSubscriptionIncludesRealityCredentials(t *testing.T) {
	repo := &fakeAccountRepository{
		findToken: SubscriptionToken{ID: "token-1", VPNAccountID: "account-1", Status: SubscriptionTokenStatusActive},
		profile: SubscriptionProfile{
			Account: Account{ID: "account-1", DisplayName: "Demo", Status: StatusActive, ServerID: "server-1", VLESSUUID: testVLESSUUID},
			Server: &SubscriptionServer{
				ID:                "server-1",
				Name:              "Finland",
				Hostname:          "fi.routegate.example",
				VLESSPort:         443,
				VLESSFlow:         "xtls-rprx-vision",
				VLESSNetwork:      "tcp",
				RealityPublicKey:  "jNXHt1yRo0vDuchQlIP6Z0ZvjT3KtzVI-T4E7RoLJS0",
				RealityShortID:    "0123456789abcdef",
				RealityServerName: "www.example.com",
			},
		},
	}
	handler := newTestHandler(repo)
	request := httptest.NewRequest(http.MethodGet, "/api/v1/subscriptions/fixed-token", nil)
	request.SetPathValue("token", "fixed-token")
	response := httptest.NewRecorder()

	handler.GetPublicSubscription(response, request)

	if response.Code != http.StatusOK {
		t.Fatalf("expected status %d, got %d", http.StatusOK, response.Code)
	}
	var body PublicSubscriptionResponse
	if err := json.NewDecoder(response.Body).Decode(&body); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if body.Config.Rendered == nil {
		t.Fatal("expected rendered sing-box config")
	}
	vless := body.Config.Rendered.Content.Outbounds[0]
	if vless.Flow != "xtls-rprx-vision" {
		t.Fatalf("expected VLESS flow, got %q", vless.Flow)
	}
	if vless.TLS == nil || vless.TLS.Reality == nil {
		t.Fatalf("expected Reality TLS config, got %+v", vless.TLS)
	}
	if vless.TLS.ServerName != "www.example.com" || vless.TLS.Reality.PublicKey != "jNXHt1yRo0vDuchQlIP6Z0ZvjT3KtzVI-T4E7RoLJS0" || vless.TLS.Reality.ShortID != "0123456789abcdef" {
		t.Fatalf("unexpected Reality TLS config: %+v", vless.TLS)
	}
}

func TestGetPublicSubscriptionRejectsInactiveAccount(t *testing.T) {
	repo := &fakeAccountRepository{
		findToken: SubscriptionToken{ID: "token-1", VPNAccountID: "account-1", Status: SubscriptionTokenStatusActive},
		profile:   SubscriptionProfile{Account: Account{ID: "account-1", DisplayName: "Demo", Status: StatusSuspended}},
	}
	handler := newTestHandler(repo)
	request := httptest.NewRequest(http.MethodGet, "/api/v1/subscriptions/fixed-token", nil)
	request.SetPathValue("token", "fixed-token")
	response := httptest.NewRecorder()

	handler.GetPublicSubscription(response, request)

	if response.Code != http.StatusForbidden {
		t.Fatalf("expected status %d, got %d", http.StatusForbidden, response.Code)
	}
	if repo.usedTokenID != "" {
		t.Fatalf("expected inactive account token not marked used, got %q", repo.usedTokenID)
	}
}
