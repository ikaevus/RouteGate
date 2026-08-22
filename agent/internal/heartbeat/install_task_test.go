package heartbeat

import (
	"strings"
	"testing"

	"github.com/ikaevus/routegate/agent/internal/vpncoreinstall"
)

func TestInstallationReportMapIsStructuredAndOmitsCommandOutput(t *testing.T) {
	result := reportMap(vpncoreinstall.Report{
		Kind:           vpncoreinstall.TaskKind,
		Operation:      vpncoreinstall.OperationInstall,
		Status:         "succeeded",
		Platform:       vpncoreinstall.Platform{ID: "debian", Version: "12", Architecture: "amd64"},
		SingBoxVersion: "sing-box version 1.12.0",
		BinaryPath:     "/usr/bin/sing-box",
		ServiceName:    vpncoreinstall.DefaultServiceName,
		Stages:         []vpncoreinstall.StageResult{{Stage: "complete", Status: "succeeded"}},
	})
	for _, forbidden := range []string{"command", "output", "url", "package"} {
		if _, exists := result[forbidden]; exists {
			t.Fatalf("unsafe field %q exposed in result: %#v", forbidden, result)
		}
	}
	if result["kind"] != vpncoreinstall.TaskKind || result["operation"] != vpncoreinstall.OperationInstall {
		t.Fatalf("unexpected result: %#v", result)
	}
}

func TestInstalledRuntimeServiceNameUsesReportedService(t *testing.T) {
	report := &vpncoreinstall.Report{Operation: vpncoreinstall.OperationInstallMTG, ServiceName: "routegate-mtproto.service"}
	if got := installedRuntimeServiceName(report); got != "routegate-mtproto.service" {
		t.Fatalf("service = %q, want routegate-mtproto.service", got)
	}
}

func TestInstalledRuntimeServiceNameMapsWireGuardTemplateInstance(t *testing.T) {
	report := &vpncoreinstall.Report{Operation: vpncoreinstall.OperationInstallWireGuard}
	if got := installedRuntimeServiceName(report); got != "wg-quick@routegate-wg0.service" {
		t.Fatalf("service = %q, want wg-quick@routegate-wg0.service", got)
	}
}

func TestInstalledRuntimeServiceNameLeavesUnknownRuntimeEmpty(t *testing.T) {
	report := &vpncoreinstall.Report{Operation: "unknown"}
	if got := installedRuntimeServiceName(report); got != "" {
		t.Fatalf("service = %q, want empty", got)
	}
}

func TestMTProtoCredentialOverrideKeepsSecretRootOnly(t *testing.T) {
	for _, required := range []string{
		"LoadCredential=mtg-config:/etc/routegate-mtproto/config.toml",
		"ExecStart=/usr/local/bin/mtg run ${CREDENTIALS_DIRECTORY}/mtg-config",
	} {
		if !strings.Contains(mtprotoServiceCredentialOverride, required) {
			t.Fatalf("MTProto service override missing %q: %s", required, mtprotoServiceCredentialOverride)
		}
	}
	if strings.Contains(mtprotoServiceCredentialOverride, "ExecStart=/usr/local/bin/mtg run /etc/routegate-mtproto/config.toml") {
		t.Fatalf("MTProto override must not expose the root-only config directly to DynamicUser")
	}
}
