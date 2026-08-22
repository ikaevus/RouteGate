package tasks

import (
	"context"
	"encoding/json"
	"os"
	"strings"
	"testing"
)

const testWireGuardKey = "AAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA="

func testWireGuardConfig() string {
	return "[Interface]\nPrivateKey = " + testWireGuardKey + "\nAddress = 10.66.0.1/24\nListenPort = 51820\nSaveConfig = false\nPostUp = iptables -A FORWARD -i %i -j ACCEPT; iptables -A FORWARD -o %i -j ACCEPT; iptables -t nat -A POSTROUTING -s 10.66.0.0/24 -j MASQUERADE\nPostDown = iptables -D FORWARD -i %i -j ACCEPT; iptables -D FORWARD -o %i -j ACCEPT; iptables -t nat -D POSTROUTING -s 10.66.0.0/24 -j MASQUERADE\n\n[Peer]\nPublicKey = " + testWireGuardKey + "\nAllowedIPs = 10.66.0.2/32\n"
}

func TestNewWireGuardAdapterNormalizesLegacyBinaryPaths(t *testing.T) {
	adapter := NewWireGuardAdapter(t.TempDir(), "wg-quick", "wg", "wg-quick@test", "test0").(wireGuardAdapter)
	if adapter.wgQuickPath != "/usr/bin/wg-quick" || adapter.wgPath != "/usr/bin/wg" {
		t.Fatalf("expected canonical WireGuard paths, got wg-quick=%q wg=%q", adapter.wgQuickPath, adapter.wgPath)
	}
}

func TestWireGuardAdapterStagesAndValidatesWithoutReturningKeyMaterial(t *testing.T) {
	adapter := NewWireGuardAdapter(t.TempDir(), "wg-quick-test", "wg-test", "wg-quick@test", "test0").(wireGuardAdapter)
	task := ConfigTask{ID: "task-id", ConfigVersionID: "version-id", RenderedConfig: []byte(`{"schemaVersion":"routegate.config.v1","wireGuard":` + mustJSONString(t, testWireGuardConfig()) + `}`)}
	staged, err := adapter.Stage(task)
	if err != nil { t.Fatalf("stage: %v", err) }
	content, err := os.ReadFile(staged.StagedPath)
	if err != nil || !strings.Contains(string(content), "[Peer]") { t.Fatalf("staged config: %v %s", err, content) }
	adapter.run = func(_ context.Context, name string, args ...string) ([]byte, error) {
		if name != "wg-quick-test" || len(args) != 2 || args[0] != "strip" { t.Fatalf("unexpected command: %s %v", name, args) }
		return []byte(testWireGuardKey), nil
	}
	result, err := adapter.Validate(context.Background(), staged.StagedPath)
	if err != nil { t.Fatalf("validate: %v", err) }
	if result.Output != "" || strings.Contains(result.Command, testWireGuardKey) { t.Fatalf("validation leaked key material: %+v", result) }
}

func TestWireGuardParserRejectsArbitraryHooks(t *testing.T) {
	payload := strings.Replace(testWireGuardConfig(), "PostUp = iptables", "PostUp = curl https://example.test; iptables", 1)
	if _, err := parseWireGuardConfig(payload); err == nil { t.Fatal("expected arbitrary hook rejection") }
}

func TestSelectVPNCoreAdapterUsesWireGuardDescriptor(t *testing.T) {
	vless := NewSingBoxVLESSAdapter(t.TempDir(), "sing-box", "sing-box")
	wireGuard := NewWireGuardAdapter(t.TempDir(), "wg-quick", "wg", "wg-quick@test", "test0")
	task := ConfigTask{RenderedConfig: []byte(`{"metadata":{"vpnCore":{"core":"wireguard","protocol":"wireguard","transport":"udp","security":"wireguard"}}}`)}
	selected, err := SelectVPNCoreAdapter(task, vless, wireGuard)
	if err != nil { t.Fatalf("select: %v", err) }
	if selected.Descriptor().Core != "wireguard" { t.Fatalf("selected %+v", selected.Descriptor()) }
}

func mustJSONString(t *testing.T, value string) string {
	t.Helper()
	payload, err := json.Marshal(value)
	if err != nil { t.Fatal(err) }
	return string(payload)
}
