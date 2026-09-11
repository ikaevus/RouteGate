package vpnaccounts

import (
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
)

// fakeDeviceRepository is a minimal in-memory implementation of
// accountRepository (Handler requires it statically) plus deviceRepository
// (asserted dynamically), enough to unit-test the Access & Devices handlers
// without a real database.
type fakeDeviceRepository struct {
	devices      map[string]Device
	tokensByID   map[string]SubscriptionToken
	nextDeviceID int
	nextTokenID  int
}

func newFakeDeviceRepository() *fakeDeviceRepository {
	return &fakeDeviceRepository{
		devices:    map[string]Device{},
		tokensByID: map[string]SubscriptionToken{},
	}
}

func (f *fakeDeviceRepository) CreateAccount(context.Context, CreateAccountInput) (Account, error) { return Account{}, nil }
func (f *fakeDeviceRepository) ListAccounts(context.Context, AccountFilter) ([]Account, error)    { return nil, nil }
func (f *fakeDeviceRepository) CountAccounts(context.Context, AccountFilter) (int, error)         { return 0, nil }
func (f *fakeDeviceRepository) GetAccountByID(context.Context, string) (Account, error) {
	return Account{ID: "account-1", DisplayName: "Demo", Status: StatusActive, CreatedAt: time.Now(), UpdatedAt: time.Now()}, nil
}
func (f *fakeDeviceRepository) UpdateAccount(context.Context, string, UpdateAccountInput) (Account, error) {
	return Account{}, nil
}
func (f *fakeDeviceRepository) SetAccountStatus(context.Context, string, string) (Account, error) {
	return Account{}, nil
}
func (f *fakeDeviceRepository) DeleteAccount(context.Context, string) error { return nil }
func (f *fakeDeviceRepository) CreateSubscriptionToken(context.Context, CreateSubscriptionTokenInput) (SubscriptionToken, error) {
	return SubscriptionToken{}, nil
}
func (f *fakeDeviceRepository) RevokeActiveSubscriptionTokens(context.Context, string) error { return nil }
func (f *fakeDeviceRepository) GetActiveSubscriptionTokenByHash(context.Context, string, string) (SubscriptionToken, error) {
	return SubscriptionToken{}, pgx.ErrNoRows
}
func (f *fakeDeviceRepository) FindActiveSubscriptionTokenByHash(context.Context, string) (SubscriptionToken, error) {
	return SubscriptionToken{}, pgx.ErrNoRows
}
func (f *fakeDeviceRepository) GetSubscriptionProfileByAccountID(context.Context, string) (SubscriptionProfile, error) {
	return SubscriptionProfile{}, pgx.ErrNoRows
}
func (f *fakeDeviceRepository) MarkSubscriptionTokenUsed(context.Context, string) error { return nil }

func (f *fakeDeviceRepository) GetDeviceByID(_ context.Context, id string) (Device, error) {
	device, ok := f.devices[id]
	if !ok {
		return Device{}, pgx.ErrNoRows
	}
	return device, nil
}
func (f *fakeDeviceRepository) MarkDeviceUsed(context.Context, string) error { return nil }

func (f *fakeDeviceRepository) ListDevices(_ context.Context, vpnAccountID string) ([]Device, error) {
	items := make([]Device, 0)
	for _, device := range f.devices {
		if device.VPNAccountID == vpnAccountID {
			items = append(items, device)
		}
	}
	return items, nil
}

func (f *fakeDeviceRepository) GetDevice(_ context.Context, vpnAccountID, deviceID string) (Device, error) {
	device, ok := f.devices[deviceID]
	if !ok || device.VPNAccountID != vpnAccountID {
		return Device{}, pgx.ErrNoRows
	}
	return device, nil
}

func (f *fakeDeviceRepository) CreateDevice(_ context.Context, input CreateDeviceInput) (Device, error) {
	f.nextDeviceID++
	device := Device{
		ID:           "device-" + strconv.Itoa(f.nextDeviceID),
		VPNAccountID: input.VPNAccountID,
		Name:         input.Name,
		ClientType:   input.ClientType,
		DeviceType:   input.DeviceType,
		Status:       DeviceStatusActive,
		CreatedAt:    time.Now(),
		UpdatedAt:    time.Now(),
	}
	f.devices[device.ID] = device
	return device, nil
}

func (f *fakeDeviceRepository) UpdateDevice(_ context.Context, vpnAccountID, deviceID string, input UpdateDeviceInput) (Device, error) {
	device, ok := f.devices[deviceID]
	if !ok || device.VPNAccountID != vpnAccountID {
		return Device{}, pgx.ErrNoRows
	}
	if input.Name != nil {
		device.Name = *input.Name
	}
	if input.ClientType != nil {
		device.ClientType = *input.ClientType
	}
	if input.DeviceType != nil {
		device.DeviceType = *input.DeviceType
	}
	device.UpdatedAt = time.Now()
	f.devices[deviceID] = device
	return device, nil
}

func (f *fakeDeviceRepository) RevokeDevice(_ context.Context, vpnAccountID, deviceID string) (Device, error) {
	device, ok := f.devices[deviceID]
	if !ok || device.VPNAccountID != vpnAccountID || device.Status != DeviceStatusActive {
		return Device{}, pgx.ErrNoRows
	}
	now := time.Now()
	device.Status = DeviceStatusRevoked
	device.RevokedAt = &now
	f.devices[deviceID] = device
	for id, token := range f.tokensByID {
		if token.DeviceID == deviceID && token.Status == SubscriptionTokenStatusActive {
			token.Status = SubscriptionTokenStatusRevoked
			f.tokensByID[id] = token
		}
	}
	return device, nil
}

func (f *fakeDeviceRepository) CreateDeviceSubscriptionToken(_ context.Context, deviceID, tokenHash string, expiresAt *time.Time) (SubscriptionToken, error) {
	device, ok := f.devices[deviceID]
	if !ok || device.Status != DeviceStatusActive {
		return SubscriptionToken{}, pgx.ErrNoRows
	}
	for id, token := range f.tokensByID {
		if token.DeviceID == deviceID && token.Status == SubscriptionTokenStatusActive {
			token.Status = SubscriptionTokenStatusRevoked
			f.tokensByID[id] = token
		}
	}
	f.nextTokenID++
	token := SubscriptionToken{
		ID:           "token-" + strconv.Itoa(f.nextTokenID),
		VPNAccountID: device.VPNAccountID,
		DeviceID:     deviceID,
		TokenHash:    tokenHash,
		Status:       SubscriptionTokenStatusActive,
		ExpiresAt:    expiresAt,
		CreatedAt:    time.Now(),
		UpdatedAt:    time.Now(),
	}
	f.tokensByID[token.ID] = token
	return token, nil
}

func (f *fakeDeviceRepository) GetActiveDeviceSubscriptionToken(_ context.Context, deviceID string) (SubscriptionToken, error) {
	for _, token := range f.tokensByID {
		if token.DeviceID == deviceID && token.Status == SubscriptionTokenStatusActive {
			return token, nil
		}
	}
	return SubscriptionToken{}, pgx.ErrNoRows
}

// newDeviceTestHandler builds a handler with a valid configured PublicURL,
// since RG-116 device creation/rotation now requires one (see
// device_url_canonicalization_test.go for the tests that specifically cover
// the missing/invalid PublicURL cases).
func newDeviceTestHandler(repo *fakeDeviceRepository) *Handler {
	return &Handler{
		logger:                    slog.New(slog.NewTextHandler(io.Discard, nil)),
		accounts:                  repo,
		generateSubscriptionToken: GenerateSubscriptionToken,
		publicURL:                 "https://vpn.example.com",
	}
}

func TestCreateDeviceIssuesTokenAndRejectsUnsupportedClientType(t *testing.T) {
	repo := newFakeDeviceRepository()
	handler := newDeviceTestHandler(repo)

	request := httptest.NewRequest(http.MethodPost, "/api/v1/vpn-accounts/account-1/devices", strings.NewReader(`{"name":"iPhone","clientType":"hiddify","deviceType":"ios"}`))
	request.SetPathValue("id", "account-1")
	recorder := httptest.NewRecorder()
	handler.CreateDevice(recorder, request)

	if recorder.Code != http.StatusCreated {
		t.Fatalf("status = %d, body = %s", recorder.Code, recorder.Body.String())
	}
	var response DeviceSubscriptionTokenResponse
	if err := json.Unmarshal(recorder.Body.Bytes(), &response); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if response.SubscriptionToken == "" || response.SubscriptionURL == "" {
		t.Fatalf("expected subscription token/url to be issued: %+v", response)
	}
	if response.Device.ClientType != "hiddify" {
		t.Fatalf("device client type = %q, want hiddify", response.Device.ClientType)
	}

	// V2RayTun is retired: devices may not select it, only Generic.
	badRequest := httptest.NewRequest(http.MethodPost, "/api/v1/vpn-accounts/account-1/devices", strings.NewReader(`{"name":"Old client","clientType":"v2raytun","deviceType":"android"}`))
	badRequest.SetPathValue("id", "account-1")
	badRecorder := httptest.NewRecorder()
	handler.CreateDevice(badRecorder, badRequest)
	if badRecorder.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400 for retired client type, body = %s", badRecorder.Code, badRecorder.Body.String())
	}
}

func TestRevokeDeviceRevokesItsTokenWithoutAffectingOtherDevices(t *testing.T) {
	repo := newFakeDeviceRepository()
	handler := newDeviceTestHandler(repo)

	first, err := repo.CreateDevice(context.Background(), CreateDeviceInput{VPNAccountID: "account-1", Name: "Phone", ClientType: "hiddify", DeviceType: "ios"})
	if err != nil {
		t.Fatalf("create first device: %v", err)
	}
	second, err := repo.CreateDevice(context.Background(), CreateDeviceInput{VPNAccountID: "account-1", Name: "Laptop", ClientType: "v2rayn", DeviceType: "windows"})
	if err != nil {
		t.Fatalf("create second device: %v", err)
	}
	if _, err := repo.CreateDeviceSubscriptionToken(context.Background(), first.ID, "hash-1", nil); err != nil {
		t.Fatalf("issue token for first device: %v", err)
	}
	if _, err := repo.CreateDeviceSubscriptionToken(context.Background(), second.ID, "hash-2", nil); err != nil {
		t.Fatalf("issue token for second device: %v", err)
	}

	request := httptest.NewRequest(http.MethodPost, "/api/v1/vpn-accounts/account-1/devices/"+first.ID+"/revoke", nil)
	request.SetPathValue("id", "account-1")
	request.SetPathValue("deviceId", first.ID)
	recorder := httptest.NewRecorder()
	handler.RevokeDevice(recorder, request)

	if recorder.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s", recorder.Code, recorder.Body.String())
	}

	if _, err := repo.GetActiveDeviceSubscriptionToken(context.Background(), first.ID); err == nil {
		t.Fatal("expected revoked device to have no active token")
	}
	if _, err := repo.GetActiveDeviceSubscriptionToken(context.Background(), second.ID); err != nil {
		t.Fatalf("expected second device's token to remain active, got error: %v", err)
	}
}

func TestRotateDeviceSubscriptionTokenReplacesOnlyThatDevicesToken(t *testing.T) {
	repo := newFakeDeviceRepository()
	handler := newDeviceTestHandler(repo)

	device, err := repo.CreateDevice(context.Background(), CreateDeviceInput{VPNAccountID: "account-1", Name: "Phone", ClientType: "hiddify", DeviceType: "ios"})
	if err != nil {
		t.Fatalf("create device: %v", err)
	}
	firstToken, err := repo.CreateDeviceSubscriptionToken(context.Background(), device.ID, "hash-1", nil)
	if err != nil {
		t.Fatalf("issue first token: %v", err)
	}

	request := httptest.NewRequest(http.MethodPost, "/api/v1/vpn-accounts/account-1/devices/"+device.ID+"/rotate", nil)
	request.SetPathValue("id", "account-1")
	request.SetPathValue("deviceId", device.ID)
	recorder := httptest.NewRecorder()
	handler.RotateDeviceSubscriptionToken(recorder, request)

	if recorder.Code != http.StatusCreated {
		t.Fatalf("status = %d, body = %s", recorder.Code, recorder.Body.String())
	}

	active, err := repo.GetActiveDeviceSubscriptionToken(context.Background(), device.ID)
	if err != nil {
		t.Fatalf("get active token after rotate: %v", err)
	}
	if active.ID == firstToken.ID {
		t.Fatal("rotate did not replace the token")
	}
	if stored := repo.tokensByID[firstToken.ID]; stored.Status != SubscriptionTokenStatusRevoked {
		t.Fatalf("previous token status = %q, want revoked", stored.Status)
	}
}
