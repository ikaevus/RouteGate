package vpnaccounts

import (
	"net/url"
	"strings"
	"testing"
)

func TestBuildClientVLESSLinkUsesResolvedAutoFingerprint(t *testing.T) {
	subscription := SubscriptionProfile{
		Account: Account{ID: "account-1", DisplayName: "Felix", VLESSUUID: "0038dfb4-5a0f-44d8-b26a-70d4772443b1"},
		Server: &SubscriptionServer{
			Name:              "US",
			PublicIP:          "139.60.162.138",
			VLESSPort:         8443,
			VLESSNetwork:      "tcp",
			RealityPublicKey:  "SDFokzQk7i2g6jSevNCGTtgyQponyhh5P-1PhwpNbC4",
			RealityShortID:    "97b245084c3978ea",
			RealityServerName: "github.com",
		},
	}
	profile := ClientProfile{FingerprintMode: FingerprintModeAuto, Fingerprint: "chrome", SpiderX: "/"}

	link, endpoint, serverName, network, flow, err := buildClientVLESSLink(subscription, profile)
	if err != nil {
		t.Fatalf("build link: %v", err)
	}
	if endpoint != "139.60.162.138:8443" || serverName != "github.com" || network != "tcp" || flow != "" {
		t.Fatalf("unexpected metadata endpoint=%q serverName=%q network=%q flow=%q", endpoint, serverName, network, flow)
	}
	parsed, err := url.Parse(link)
	if err != nil {
		t.Fatalf("parse link: %v", err)
	}
	if got := parsed.Query().Get("fp"); got != DefaultAutoFingerprint {
		t.Fatalf("expected auto fingerprint %q, got %q", DefaultAutoFingerprint, got)
	}
	if got := parsed.Query().Get("sni"); got != "github.com" {
		t.Fatalf("expected github.com SNI, got %q", got)
	}
}

func TestBuildClientVLESSLinkNormalizesFullLengthCIDREndpoint(t *testing.T) {
	subscription := SubscriptionProfile{
		Account: Account{
			DisplayName: "Felix",
			VLESSUUID:   "0038dfb4-5a0f-44d8-b26a-70d4772443b1",
		},
		Server: &SubscriptionServer{
			PublicIP:          "139.60.162.138/32",
			VLESSPort:         8443,
			VLESSNetwork:      "tcp",
			RealityPublicKey:  "public-key",
			RealityServerName: "github.com",
		},
	}
	profile := ClientProfile{
		FingerprintMode: FingerprintModeAuto,
		Fingerprint:     "firefox",
		SpiderX:         "/",
	}

	link, endpoint, _, _, _, err := buildClientVLESSLink(subscription, profile)
	if err != nil {
		t.Fatalf("build link: %v", err)
	}
	if endpoint != "139.60.162.138:8443" {
		t.Fatalf("expected normalized endpoint, got %q", endpoint)
	}
	if strings.Contains(link, "/32") {
		t.Fatalf("VLESS link contains CIDR suffix: %q", link)
	}

	parsed, err := url.Parse(link)
	if err != nil {
		t.Fatalf("parse link: %v", err)
	}
	if parsed.Hostname() != "139.60.162.138" || parsed.Port() != "8443" {
		t.Fatalf("unexpected VLESS authority: %q", parsed.Host)
	}
}

func TestBuildClientVLESSLinkUsesManualFingerprintAndStableOutput(t *testing.T) {
	subscription := SubscriptionProfile{
		Account: Account{DisplayName: "Demo", VLESSUUID: "0038dfb4-5a0f-44d8-b26a-70d4772443b1"},
		Server: &SubscriptionServer{
			Hostname:          "us.routegate.org",
			VLESSPort:         443,
			VLESSNetwork:      "tcp",
			RealityPublicKey:  "public-key",
			RealityShortID:    "0123456789abcdef",
			RealityServerName: "github.com",
		},
	}
	profile := ClientProfile{FingerprintMode: FingerprintModeManual, Fingerprint: "chrome", SpiderX: "/routegate"}

	first, _, _, _, _, err := buildClientVLESSLink(subscription, profile)
	if err != nil {
		t.Fatalf("build first link: %v", err)
	}
	second, _, _, _, _, err := buildClientVLESSLink(subscription, profile)
	if err != nil {
		t.Fatalf("build second link: %v", err)
	}
	if first != second {
		t.Fatalf("expected stable link, got %q and %q", first, second)
	}
	if !strings.Contains(first, "fp=chrome") || !strings.Contains(first, "spx=%2Froutegate") {
		t.Fatalf("unexpected link %q", first)
	}
}

func TestValidateClientProfileRequestRejectsInvalidMTU(t *testing.T) {
	request := UpdateClientProfileRequest{
		Name:            "Default",
		ClientType:      "v2rayn",
		DeviceType:      "windows",
		FingerprintMode: FingerprintModeAuto,
		Fingerprint:     "firefox",
		SpiderX:         "/",
		MTU:             intPointer(500),
	}
	if err := validateClientProfileRequest(request); err == nil {
		t.Fatal("expected invalid MTU error")
	}
}

func intPointer(value int) *int {
	return &value
}
