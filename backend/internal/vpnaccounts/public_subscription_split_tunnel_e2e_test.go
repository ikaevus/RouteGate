package vpnaccounts

import (
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"reflect"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
)

const publicSubscriptionSplitTunnelE2EToken = "split-tunnel-token"

type publicSubscriptionSplitTunnelE2ERepository struct {
	account        Account
	server         SubscriptionServer
	token          SubscriptionToken
	routingProfile *RoutingProfile

	markedUsedTokenID string
}

func newPublicSubscriptionSplitTunnelE2ERepository(routingProfile *RoutingProfile) *publicSubscriptionSplitTunnelE2ERepository {
	now := time.Now().UTC()
	repo := &publicSubscriptionSplitTunnelE2ERepository{
		server: SubscriptionServer{
			ID:                "server-split-tunnel",
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
		routingProfile: routingProfile,
	}
	repo.account = Account{
		ID:          "account-split-tunnel",
		DisplayName: "Alice Split",
		Email:       "alice@example.com",
		Status:      StatusActive,
		ServerID:    repo.server.ID,
		VLESSUUID:   "11111111-1111-1111-1111-111111111111",
		CreatedAt:   now,
		UpdatedAt:   now,
	}
	repo.token = SubscriptionToken{
		ID:           "token-split-tunnel",
		VPNAccountID: repo.account.ID,
		TokenHash:    HashSubscriptionToken(publicSubscriptionSplitTunnelE2EToken),
		Status:       SubscriptionTokenStatusActive,
		CreatedAt:    now,
		UpdatedAt:    now,
	}
	return repo
}

func (r *publicSubscriptionSplitTunnelE2ERepository) CreateAccount(context.Context, CreateAccountInput) (Account, error) {
	return Account{}, pgx.ErrNoRows
}

func (r *publicSubscriptionSplitTunnelE2ERepository) ListAccounts(context.Context, AccountFilter) ([]Account, error) {
	return nil, nil
}

func (r *publicSubscriptionSplitTunnelE2ERepository) GetAccountByID(_ context.Context, id string) (Account, error) {
	if r.account.ID == id {
		return r.account, nil
	}
	return Account{}, pgx.ErrNoRows
}

func (r *publicSubscriptionSplitTunnelE2ERepository) UpdateAccount(context.Context, string, UpdateAccountInput) (Account, error) {
	return Account{}, pgx.ErrNoRows
}

func (r *publicSubscriptionSplitTunnelE2ERepository) SetAccountStatus(context.Context, string, string) (Account, error) {
	return Account{}, pgx.ErrNoRows
}

func (r *publicSubscriptionSplitTunnelE2ERepository) DeleteAccount(context.Context, string) error {
	return pgx.ErrNoRows
}

func (r *publicSubscriptionSplitTunnelE2ERepository) CreateSubscriptionToken(context.Context, CreateSubscriptionTokenInput) (SubscriptionToken, error) {
	return SubscriptionToken{}, pgx.ErrNoRows
}

func (r *publicSubscriptionSplitTunnelE2ERepository) RevokeActiveSubscriptionTokens(context.Context, string) error {
	return pgx.ErrNoRows
}

func (r *publicSubscriptionSplitTunnelE2ERepository) GetActiveSubscriptionTokenByHash(context.Context, string, string) (SubscriptionToken, error) {
	return SubscriptionToken{}, pgx.ErrNoRows
}

func (r *publicSubscriptionSplitTunnelE2ERepository) FindActiveSubscriptionTokenByHash(_ context.Context, tokenHash string) (SubscriptionToken, error) {
	if r.token.TokenHash == tokenHash && r.token.Status == SubscriptionTokenStatusActive {
		return r.token, nil
	}
	return SubscriptionToken{}, pgx.ErrNoRows
}

func (r *publicSubscriptionSplitTunnelE2ERepository) GetSubscriptionProfileByAccountID(_ context.Context, id string) (SubscriptionProfile, error) {
	if r.account.ID != id {
		return SubscriptionProfile{}, pgx.ErrNoRows
	}
	profile := SubscriptionProfile{
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
	}
	if r.routingProfile != nil {
		routingProfile := *r.routingProfile
		profile.RoutingProfile = &routingProfile
	}
	return profile, nil
}

func (r *publicSubscriptionSplitTunnelE2ERepository) MarkSubscriptionTokenUsed(_ context.Context, id string) error {
	if r.token.ID != id {
		return pgx.ErrNoRows
	}
	r.markedUsedTokenID = id
	return nil
}

func TestPublicSubscriptionSplitTunnelE2ERendersRoutingProfileRules(t *testing.T) {
	repo := newPublicSubscriptionSplitTunnelE2ERepository(&RoutingProfile{
		ID:          "profile-split-tunnel",
		Name:        "Client split tunnel",
		Description: "Public subscription routing profile",
		Rules: []RoutingProfileRule{
			{
				ID:             "rule-direct-domestic",
				Name:           "Domestic services direct",
				Priority:       10,
				Action:         RoutingActionDirect,
				DomainSuffixes: []string{"gosuslugi.ru", "mos.ru"},
				IPCIDRs:        []string{"10.0.0.0/8"},
			},
			{
				ID:             "rule-vpn-video",
				Name:           "Video through VPN",
				Priority:       20,
				Action:         RoutingActionVPN,
				DomainSuffixes: []string{"youtube.com"},
				DomainKeywords: []string{"googlevideo"},
			},
			{
				ID:       "rule-block-test-net",
				Name:     "Block test net",
				Priority: 30,
				Action:   RoutingActionBlock,
				IPCIDRs:  []string{"203.0.113.0/24"},
			},
		},
	})
	handler := newPublicSubscriptionSplitTunnelE2EHandler(repo)

	response := getPublicSubscriptionSplitTunnelE2E(t, handler, publicSubscriptionSplitTunnelE2EToken)

	var subscription PublicSubscriptionResponse
	decodePublicSubscriptionSplitTunnelE2EJSON(t, response, &subscription)
	if subscription.Format != "routegate.subscription.v1" || subscription.Status != "ok" {
		t.Fatalf("unexpected subscription envelope: %+v", subscription)
	}
	if subscription.Config.Format != ClientConfigFormat || subscription.Config.Status != "rendered" || subscription.Config.Rendered == nil {
		t.Fatalf("unexpected subscription config metadata: %+v", subscription.Config)
	}
	if subscription.Config.Rendered.Format != SingBoxClientConfigFormat {
		t.Fatalf("unexpected rendered config format: %q", subscription.Config.Rendered.Format)
	}

	config := subscription.Config.Rendered.Content
	if config.Route.Final != singBoxOutboundTag {
		t.Fatalf("expected final route %q, got %q", singBoxOutboundTag, config.Route.Final)
	}
	if len(config.Route.Rules) != 3 {
		t.Fatalf("route rules = %d, want 3 split-tunnel rules: %+v", len(config.Route.Rules), config.Route.Rules)
	}

	expectPublicSubscriptionSplitTunnelE2ERouteOutbound(t, config.Route.Rules[0], singBoxDirectTag)
	expectPublicSubscriptionSplitTunnelE2ERouteStrings(t, config.Route.Rules[0], "domain_suffix", []string{"gosuslugi.ru", "mos.ru"})
	expectPublicSubscriptionSplitTunnelE2ERouteStrings(t, config.Route.Rules[0], "ip_cidr", []string{"10.0.0.0/8"})

	expectPublicSubscriptionSplitTunnelE2ERouteOutbound(t, config.Route.Rules[1], singBoxOutboundTag)
	expectPublicSubscriptionSplitTunnelE2ERouteStrings(t, config.Route.Rules[1], "domain_suffix", []string{"youtube.com"})
	expectPublicSubscriptionSplitTunnelE2ERouteStrings(t, config.Route.Rules[1], "domain_keyword", []string{"googlevideo"})

	expectPublicSubscriptionSplitTunnelE2ERouteOutbound(t, config.Route.Rules[2], singBoxBlockTag)
	expectPublicSubscriptionSplitTunnelE2ERouteStrings(t, config.Route.Rules[2], "ip_cidr", []string{"203.0.113.0/24"})

	if !hasPublicSubscriptionSplitTunnelE2EOutbound(config.Outbounds, "vless", singBoxOutboundTag) {
		t.Fatalf("expected VLESS proxy outbound %q, got %+v", singBoxOutboundTag, config.Outbounds)
	}
	if !hasPublicSubscriptionSplitTunnelE2EOutbound(config.Outbounds, "direct", singBoxDirectTag) {
		t.Fatalf("expected direct outbound %q, got %+v", singBoxDirectTag, config.Outbounds)
	}
	if !hasPublicSubscriptionSplitTunnelE2EOutbound(config.Outbounds, "block", singBoxBlockTag) {
		t.Fatalf("expected block outbound %q, got %+v", singBoxBlockTag, config.Outbounds)
	}
	if repo.markedUsedTokenID != repo.token.ID {
		t.Fatalf("expected token %q marked used, got %q", repo.token.ID, repo.markedUsedTokenID)
	}
}

func TestPublicSubscriptionSplitTunnelE2EAllowsMissingRoutingProfile(t *testing.T) {
	repo := newPublicSubscriptionSplitTunnelE2ERepository(nil)
	handler := newPublicSubscriptionSplitTunnelE2EHandler(repo)

	response := getPublicSubscriptionSplitTunnelE2E(t, handler, publicSubscriptionSplitTunnelE2EToken)

	var subscription PublicSubscriptionResponse
	decodePublicSubscriptionSplitTunnelE2EJSON(t, response, &subscription)
	if subscription.Config.Rendered == nil {
		t.Fatal("expected rendered sing-box config without routing profile")
	}
	config := subscription.Config.Rendered.Content
	if len(config.Route.Rules) != 0 {
		t.Fatalf("expected no route rules without routing profile, got %+v", config.Route.Rules)
	}
	if config.Route.Final != singBoxOutboundTag {
		t.Fatalf("expected final route %q, got %q", singBoxOutboundTag, config.Route.Final)
	}
	if !hasPublicSubscriptionSplitTunnelE2EOutbound(config.Outbounds, "vless", singBoxOutboundTag) {
		t.Fatalf("expected VLESS proxy outbound %q, got %+v", singBoxOutboundTag, config.Outbounds)
	}
	if !hasPublicSubscriptionSplitTunnelE2EOutbound(config.Outbounds, "direct", singBoxDirectTag) {
		t.Fatalf("expected direct outbound %q, got %+v", singBoxDirectTag, config.Outbounds)
	}
}

func TestPublicSubscriptionSplitTunnelE2ERejectsInvalidToken(t *testing.T) {
	repo := newPublicSubscriptionSplitTunnelE2ERepository(nil)
	handler := newPublicSubscriptionSplitTunnelE2EHandler(repo)

	request := httptest.NewRequest(http.MethodGet, "/api/v1/subscriptions/wrong-token", nil)
	request.SetPathValue("token", "wrong-token")
	response := httptest.NewRecorder()
	handler.GetPublicSubscription(response, request)
	requirePublicSubscriptionSplitTunnelE2EHTTPStatus(t, response, http.StatusNotFound)

	if repo.markedUsedTokenID != "" {
		t.Fatalf("expected invalid token not to be marked used, got %q", repo.markedUsedTokenID)
	}
}

func newPublicSubscriptionSplitTunnelE2EHandler(repo *publicSubscriptionSplitTunnelE2ERepository) *Handler {
	return &Handler{
		logger:                    slog.New(slog.NewTextHandler(io.Discard, nil)),
		accounts:                  repo,
		generateSubscriptionToken: func() (string, error) { return publicSubscriptionSplitTunnelE2EToken, nil },
	}
}

func getPublicSubscriptionSplitTunnelE2E(t *testing.T, handler *Handler, token string) *httptest.ResponseRecorder {
	t.Helper()
	request := httptest.NewRequest(http.MethodGet, "/api/v1/subscriptions/"+token, nil)
	request.SetPathValue("token", token)
	response := httptest.NewRecorder()
	handler.GetPublicSubscription(response, request)
	requirePublicSubscriptionSplitTunnelE2EHTTPStatus(t, response, http.StatusOK)
	return response
}

func requirePublicSubscriptionSplitTunnelE2EHTTPStatus(t *testing.T, response *httptest.ResponseRecorder, want int) {
	t.Helper()
	if response.Code != want {
		t.Fatalf("status = %d, want %d; body=%s", response.Code, want, response.Body.String())
	}
}

func decodePublicSubscriptionSplitTunnelE2EJSON(t *testing.T, response *httptest.ResponseRecorder, target any) {
	t.Helper()
	if err := json.NewDecoder(response.Body).Decode(target); err != nil {
		t.Fatalf("decode response: %v; body=%s", err, response.Body.String())
	}
}

func expectPublicSubscriptionSplitTunnelE2ERouteOutbound(t *testing.T, rule map[string]any, want string) {
	t.Helper()
	if rule["outbound"] != want {
		t.Fatalf("route rule outbound = %v, want %q: %+v", rule["outbound"], want, rule)
	}
}

func expectPublicSubscriptionSplitTunnelE2ERouteStrings(t *testing.T, rule map[string]any, key string, want []string) {
	t.Helper()

	var got []string
	switch values := rule[key].(type) {
	case []string:
		got = values
	case []any:
		got = make([]string, 0, len(values))
		for _, value := range values {
			text, ok := value.(string)
			if !ok {
				t.Fatalf("route rule %q contains non-string value %#v", key, value)
			}
			got = append(got, text)
		}
	default:
		t.Fatalf("route rule %q = %#v, want string slice", key, rule[key])
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("route rule %q = %+v, want %+v", key, got, want)
	}
}

func hasPublicSubscriptionSplitTunnelE2EOutbound(outbounds []SingBoxOutbound, outboundType string, tag string) bool {
	for _, outbound := range outbounds {
		if outbound.Type == outboundType && outbound.Tag == tag {
			return true
		}
	}
	return false
}
