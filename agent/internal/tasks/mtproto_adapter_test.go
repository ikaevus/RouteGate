package tasks

import (
	"context"
	"encoding/hex"
	"os"
	"strings"
	"testing"

	"github.com/ikaevus/routegate/agent/internal/platform"
)

func testMTProtoConfig() string {
	secret := "ee" + strings.Repeat("ab", 16) + hex.EncodeToString([]byte(mtprotoFrontingDomain))
	return "debug = false\nsecret = \"" + secret + "\"\nbind-to = \"0.0.0.0:8443\"\nconcurrency = 8192\nprefer-ip = \"prefer-ipv4\"\nauto-update = false\n"
}

func TestNewMTProtoAdapterNormalizesLegacyBinaryPath(t *testing.T) {
	adapter := NewMTProtoAdapter(t.TempDir(), "mtg", "routegate-mtproto").(mtprotoAdapter)
	if adapter.binary != "/usr/local/bin/mtg" {
		t.Fatalf("expected canonical MTG path, got %q", adapter.binary)
	}
}

func TestMTProtoAdapterStagesStrictConfigAndChecksBinary(t *testing.T) {
	adapter := NewMTProtoAdapter(t.TempDir(), "mtg-test", "routegate-mtproto-test").(mtprotoAdapter)
	adapter.run = func(_ context.Context, name string, args ...string) ([]byte, error) {
		if name != "mtg-test" || strings.Join(args, " ") != "--version" {
			t.Fatalf("unexpected command: %s %v", name, args)
		}
		return []byte("mtg 2.2.8"), nil
	}
	task := ConfigTask{ID: "task-id", ConfigVersionID: "version-id", RenderedConfig: []byte(`{"schemaVersion":"routegate.config.v1","mtproto":` + mustJSONString(t, testMTProtoConfig()) + `}`)}
	staged, err := adapter.Stage(task)
	if err != nil {
		t.Fatalf("stage MTProto: %v", err)
	}
	if _, err := os.Stat(staged.StagedPath); err != nil {
		t.Fatalf("staged config missing: %v", err)
	}
	validated, err := adapter.Validate(context.Background(), staged.StagedPath)
	if err != nil {
		t.Fatalf("validate MTProto: %v", err)
	}
	if validated.Command != "mtg-test --version" {
		t.Fatalf("unexpected validation command: %q", validated.Command)
	}
}

func TestMTProtoParserRejectsUnknownAndUnsafeFields(t *testing.T) {
	if _, err := parseMTProtoConfig(testMTProtoConfig() + "ad-tag = \"x\"\n"); err == nil {
		t.Fatal("expected unknown field rejection")
	}
	unsafe := strings.Replace(testMTProtoConfig(), "auto-update = false", "auto-update = true", 1)
	if _, err := parseMTProtoConfig(unsafe); err == nil {
		t.Fatal("expected auto update rejection")
	}
}

func TestSelectVPNCoreAdapterUsesMTProtoDescriptor(t *testing.T) {
	vless := NewSingBoxVLESSAdapter(t.TempDir(), "sing-box", "sing-box")
	mtproto := NewMTProtoAdapter(t.TempDir(), "mtg", "routegate-mtproto")
	task := ConfigTask{RenderedConfig: []byte(`{"metadata":{"vpnCore":{"core":"mtg","protocol":"mtproto","transport":"tcp","security":"faketls"}}}`)}
	selected, err := SelectVPNCoreAdapter(task, vless, nil, mtproto)
	if err != nil {
		t.Fatalf("select MTProto: %v", err)
	}
	if selected.Descriptor().Protocol != platform.VPNProtocolMTProto {
		t.Fatalf("unexpected adapter: %+v", selected.Descriptor())
	}
}
