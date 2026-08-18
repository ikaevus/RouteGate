package tasks

import (
	"context"
	"encoding/json"
	"os"
	"strings"
	"testing"
)

const testHysteria2Password = "0123456789abcdef0123456789abcdef0123456789abcdef"

func testHysteria2Config(t *testing.T) string {
	t.Helper()
	payload := hysteria2ServerConfig{
		Listen: ":443",
		ACME: hysteria2ACMEConfig{Domains: []string{"vpn.example.com"}, Email: "ops@example.com", CA: "letsencrypt", Dir: hysteria2ACMEDir, Type: "http"},
		Auth: hysteria2AuthConfig{Type: "userpass", Userpass: map[string]string{"11111111-1111-1111-1111-111111111111": testHysteria2Password}},
		Masquerade: hysteria2MasqueradeConfig{Type: "proxy", Proxy: hysteria2MasqueradeProxyConfig{URL: "https://www.cloudflare.com/", RewriteHost: true}},
	}
	rendered, err := json.Marshal(payload)
	if err != nil { t.Fatal(err) }
	return string(rendered)
}

func TestHysteria2AdapterStagesStrictJSONAndChecksBinary(t *testing.T) {
	adapter := NewHysteria2Adapter(t.TempDir(), "hysteria-test", "ss-test", "hysteria-test").(hysteria2Adapter)
	task := ConfigTask{ID: "task-id", ConfigVersionID: "version-id", RenderedConfig: []byte(`{"schemaVersion":"routegate.config.v1","hysteria2":` + mustJSONString(t, testHysteria2Config(t)) + `}`)}
	staged, err := adapter.Stage(task)
	if err != nil { t.Fatalf("stage: %v", err) }
	content, err := os.ReadFile(staged.StagedPath)
	if err != nil || !strings.Contains(string(content), `"userpass"`) { t.Fatalf("staged config: %v %s", err, content) }
	adapter.run = func(_ context.Context, name string, args ...string) ([]byte, error) {
		if name != "hysteria-test" || len(args) != 1 || args[0] != "version" { t.Fatalf("unexpected command: %s %v", name, args) }
		return []byte("Hysteria v2"), nil
	}
	if _, err := adapter.Validate(context.Background(), staged.StagedPath); err != nil { t.Fatalf("validate: %v", err) }
}

func TestHysteria2ParserRejectsUnknownFieldsAndUnsafeMasquerade(t *testing.T) {
	payload := strings.Replace(testHysteria2Config(t), `"listen":":443"`, `"listen":":443","command":"curl example.com"`, 1)
	if _, err := parseHysteria2Config(payload); err == nil { t.Fatal("expected unknown field rejection") }
	payload = strings.Replace(testHysteria2Config(t), `https://www.cloudflare.com/`, `http://example.com/`, 1)
	if _, err := parseHysteria2Config(payload); err == nil { t.Fatal("expected unsafe masquerade rejection") }
}

func TestSelectVPNCoreAdapterUsesHysteria2Descriptor(t *testing.T) {
	vless := NewSingBoxVLESSAdapter(t.TempDir(), "sing-box", "sing-box")
	wireGuard := NewWireGuardAdapter(t.TempDir(), "wg-quick", "wg", "wg-quick@test", "test0")
	hysteria2 := NewHysteria2Adapter(t.TempDir(), "hysteria", "ss", "hysteria-server")
	task := ConfigTask{RenderedConfig: []byte(`{"metadata":{"vpnCore":{"core":"hysteria","protocol":"hysteria2","transport":"quic","security":"tls"}}}`)}
	selected, err := SelectVPNCoreAdapter(task, vless, wireGuard, hysteria2)
	if err != nil { t.Fatalf("select: %v", err) }
	if selected.Descriptor().Core != "hysteria" { t.Fatalf("selected %+v", selected.Descriptor()) }
}
