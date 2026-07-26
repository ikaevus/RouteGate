package heartbeat

import (
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
