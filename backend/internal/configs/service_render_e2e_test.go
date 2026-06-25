package configs

import (
	"context"
	"encoding/json"
	"reflect"
	"testing"
	"time"

	"github.com/ikaevus/routegate/backend/internal/traffic"
)

func TestRenderE2EPersistsAssignedRoutingProfileSplitTunnelRules(t *testing.T) {
	renderedAt := time.Date(2026, time.June, 26, 12, 0, 0, 0, time.UTC)
	repository := &renderE2EFakeRepository{
		info: ServerConfigInfo{
			ID:        "server-id",
			Name:      "fi-01",
			Status:    "active",
			VLESSPort: 443,
			Agent: &AgentConfigInfo{
				ID:           "agent-id",
				Hostname:     "fi-01",
				OS:           "linux",
				Arch:         "amd64",
				AgentVersion: "0.1.0",
				Status:       "online",
				Capabilities: map[string]any{"config_apply": true},
			},
			VPNAccounts: []VPNAccountConfigInfo{
				{
					ID:                       "account-active",
					DisplayName:              "Alice",
					Status:                   "active",
					VLESSUUID:                "11111111-1111-1111-1111-111111111111",
					VLESSFlow:                "xtls-rprx-vision",
					TrafficEnforcementStatus: traffic.TrafficLimitEnforcementWithinLimit,
				},
			},
			RoutingProfile: &RoutingProfileConfigInfo{
				ID:          "assigned-profile",
				Name:        "Assigned split tunnel",
				Description: "Server-specific routing profile",
				IsDefault:   false,
				Rules: []RoutingProfileRuleConfigInfo{
					{
						ID:             "rule-direct",
						Name:           "Domestic services direct",
						Priority:       10,
						Action:         routingActionDirect,
						DomainSuffixes: []string{"gosuslugi.ru", "mos.ru"},
						IPCIDRs:        []string{"10.0.0.0/8"},
					},
					{
						ID:             "rule-vpn",
						Name:           "Video via VPN",
						Priority:       20,
						Action:         routingActionVPN,
						DomainSuffixes: []string{"youtube.com"},
						DomainKeywords: []string{"googlevideo"},
					},
					{
						ID:       "rule-block",
						Name:     "Block suspicious net",
						Priority: 30,
						Action:   routingActionBlock,
						IPCIDRs:  []string{"203.0.113.0/24"},
					},
				},
			},
		},
	}
	service := NewService(repository)
	service.now = func() time.Time { return renderedAt }

	response, err := service.Render(context.Background(), "server-id")
	if err != nil {
		t.Fatalf("render config: %v", err)
	}

	if repository.requestedServerID != "server-id" {
		t.Fatalf("requested server ID = %q, want server-id", repository.requestedServerID)
	}
	if !response.ValidationResult.Valid {
		t.Fatalf("rendered config should be valid: %+v", response.ValidationResult)
	}
	if response.ConfigVersion.Status != StatusValidated {
		t.Fatalf("config version status = %q, want %q", response.ConfigVersion.Status, StatusValidated)
	}
	if repository.createdInput.ServerID != "server-id" {
		t.Fatalf("persisted server ID = %q, want server-id", repository.createdInput.ServerID)
	}
	if repository.createdInput.Status != StatusValidated {
		t.Fatalf("persisted status = %q, want %q", repository.createdInput.Status, StatusValidated)
	}
	if repository.createdInput.ConfigHash == "" {
		t.Fatal("expected persisted config hash")
	}

	rendered := repository.createdInput.RenderedConfig
	if !rendered.Metadata.RenderedAt.Equal(renderedAt) {
		t.Fatalf("renderedAt = %s, want %s", rendered.Metadata.RenderedAt, renderedAt)
	}
	if rendered.RoutingProfile == nil {
		t.Fatal("expected routing profile in rendered config")
	}
	if rendered.RoutingProfile.ID != "assigned-profile" || rendered.RoutingProfile.IsDefault {
		t.Fatalf("unexpected rendered routing profile: %+v", rendered.RoutingProfile)
	}
	if len(rendered.RoutingProfile.Rules) != 3 {
		t.Fatalf("routing profile rules = %d, want 3: %+v", len(rendered.RoutingProfile.Rules), rendered.RoutingProfile.Rules)
	}
	if rendered.RoutingProfile.Rules[0].Outbound != singBoxDirectTag {
		t.Fatalf("direct metadata outbound = %q, want %q", rendered.RoutingProfile.Rules[0].Outbound, singBoxDirectTag)
	}
	if rendered.RoutingProfile.Rules[1].Outbound != routingActionVPN {
		t.Fatalf("vpn metadata outbound = %q, want %q", rendered.RoutingProfile.Rules[1].Outbound, routingActionVPN)
	}
	if rendered.RoutingProfile.Rules[2].Outbound != singBoxBlockTag {
		t.Fatalf("block metadata outbound = %q, want %q", rendered.RoutingProfile.Rules[2].Outbound, singBoxBlockTag)
	}

	if len(rendered.SingBox.Route.Rules) != 2 {
		t.Fatalf("server sing-box route rules = %d, want 2 direct/block rules: %+v", len(rendered.SingBox.Route.Rules), rendered.SingBox.Route.Rules)
	}
	if rendered.SingBox.Route.Rules[0]["outbound"] != singBoxDirectTag {
		t.Fatalf("first sing-box route outbound = %v, want %q", rendered.SingBox.Route.Rules[0]["outbound"], singBoxDirectTag)
	}
	expectRouteRuleStringSlice(t, rendered.SingBox.Route.Rules[0], "domain_suffix", []string{"gosuslugi.ru", "mos.ru"})
	expectRouteRuleStringSlice(t, rendered.SingBox.Route.Rules[0], "ip_cidr", []string{"10.0.0.0/8"})
	if rendered.SingBox.Route.Rules[1]["outbound"] != singBoxBlockTag {
		t.Fatalf("second sing-box route outbound = %v, want %q", rendered.SingBox.Route.Rules[1]["outbound"], singBoxBlockTag)
	}
	expectRouteRuleStringSlice(t, rendered.SingBox.Route.Rules[1], "ip_cidr", []string{"203.0.113.0/24"})
	if len(rendered.SingBox.Outbounds) != 2 || rendered.SingBox.Outbounds[1].Tag != singBoxBlockTag {
		t.Fatalf("expected direct and block outbounds, got %+v", rendered.SingBox.Outbounds)
	}

	var saved RenderedConfig
	if err := json.Unmarshal(response.ConfigVersion.RenderedConfig, &saved); err != nil {
		t.Fatalf("unmarshal persisted rendered config: %v", err)
	}
	if saved.RoutingProfile == nil || saved.RoutingProfile.ID != "assigned-profile" {
		t.Fatalf("persisted config lost routing profile: %+v", saved.RoutingProfile)
	}
}

func TestRenderE2EIncludesDefaultRoutingProfileFallback(t *testing.T) {
	repository := &renderE2EFakeRepository{
		info: ServerConfigInfo{
			ID:     "server-id",
			Name:   "fi-01",
			Status: "active",
			RoutingProfile: &RoutingProfileConfigInfo{
				ID:        "default-profile",
				Name:      "Default split tunnel",
				IsDefault: true,
				Rules: []RoutingProfileRuleConfigInfo{
					{
						ID:             "rule-default-direct",
						Name:           "Default domestic direct",
						Priority:       100,
						Action:         routingActionDirect,
						DomainSuffixes: []string{"example.ru"},
					},
				},
			},
		},
	}

	response, err := NewService(repository).Render(context.Background(), "server-id")
	if err != nil {
		t.Fatalf("render config: %v", err)
	}

	if response.ConfigVersion.Status != StatusValidated {
		t.Fatalf("config version status = %q, want %q", response.ConfigVersion.Status, StatusValidated)
	}
	rendered := repository.createdInput.RenderedConfig
	if rendered.RoutingProfile == nil || rendered.RoutingProfile.ID != "default-profile" || !rendered.RoutingProfile.IsDefault {
		t.Fatalf("expected default routing profile fallback in rendered config, got %+v", rendered.RoutingProfile)
	}
	if len(rendered.SingBox.Route.Rules) != 1 || rendered.SingBox.Route.Rules[0]["outbound"] != singBoxDirectTag {
		t.Fatalf("expected one default direct sing-box route rule, got %+v", rendered.SingBox.Route.Rules)
	}
}

func expectRouteRuleStringSlice(t *testing.T, rule map[string]any, key string, want []string) {
	t.Helper()
	got, ok := rule[key].([]string)
	if !ok {
		t.Fatalf("route rule %q = %#v, want []string", key, rule[key])
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("route rule %q = %+v, want %+v", key, got, want)
	}
}

type renderE2EFakeRepository struct {
	requestedServerID string
	info              ServerConfigInfo
	createdInput      CreateConfigVersionInput
}

func (r *renderE2EFakeRepository) GetServerConfigInfo(_ context.Context, serverID string) (ServerConfigInfo, error) {
	r.requestedServerID = serverID
	return r.info, nil
}

func (r *renderE2EFakeRepository) CreateConfigVersion(_ context.Context, input CreateConfigVersionInput) (ConfigVersion, error) {
	r.createdInput = input
	renderedConfig, err := json.Marshal(input.RenderedConfig)
	if err != nil {
		return ConfigVersion{}, err
	}
	return ConfigVersion{
		ID:             "config-version-id",
		ServerID:       input.ServerID,
		Version:        1,
		ConfigHash:     input.ConfigHash,
		Status:         input.Status,
		RenderedConfig: renderedConfig,
		CreatedAt:      input.RenderedConfig.Metadata.RenderedAt,
	}, nil
}

func (r *renderE2EFakeRepository) ListConfigVersions(context.Context, string) ([]ConfigVersion, error) {
	return nil, nil
}

func (r *renderE2EFakeRepository) GetConfigVersion(context.Context, string, string) (ConfigVersion, error) {
	return ConfigVersion{}, nil
}

func (r *renderE2EFakeRepository) MarkConfigVersionValidated(context.Context, string, string) (ConfigVersion, error) {
	return ConfigVersion{}, nil
}

func (r *renderE2EFakeRepository) CreateConfigApplyJob(context.Context, CreateConfigApplyJobInput) (ConfigApplyJob, error) {
	return ConfigApplyJob{}, nil
}

func (r *renderE2EFakeRepository) ListConfigApplyJobs(context.Context, string) ([]ConfigApplyJob, error) {
	return nil, nil
}

func (r *renderE2EFakeRepository) GetConfigApplyJob(context.Context, string, string) (ConfigApplyJob, error) {
	return ConfigApplyJob{}, nil
}
