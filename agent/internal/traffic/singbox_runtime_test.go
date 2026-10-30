package traffic

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"

	"golang.org/x/net/proxy"
)

// Optional integration test uses disposable sing-box processes and loopback only.
func TestSingBoxRuntimeTraffic(t *testing.T) {
	binary := os.Getenv("ROUTEGATE_TEST_SINGBOX")
	if binary == "" {
		t.Skip("set ROUTEGATE_TEST_SINGBOX to a with_v2ray_api binary")
	}
	port := func() int {
		l, e := net.Listen("tcp", "127.0.0.1:0")
		if e != nil {
			t.Fatal(e)
		}
		p := l.Addr().(*net.TCPAddr).Port
		l.Close()
		return p
	}
	vpnPort, apiPort, socksPort := port(), port(), port()
	const account = "11111111-1111-4111-8111-111111111111"
	const credential = "22222222-2222-4222-8222-222222222222"
	dir := t.TempDir()
	start := func(name string, cfg any) string {
		path := filepath.Join(dir, name+".json")
		data, e := json.Marshal(cfg)
		if e != nil {
			t.Fatal(e)
		}
		if e = os.WriteFile(path, data, 0600); e != nil {
			t.Fatal(e)
		}
		log, e := os.Create(filepath.Join(dir, name+".log"))
		if e != nil {
			t.Fatal(e)
		}
		cmd := exec.Command(binary, "run", "-c", path)
		cmd.Stdout = log
		cmd.Stderr = log
		if e := cmd.Start(); e != nil {
			t.Fatal(e)
		}
		t.Cleanup(func() {
			cmd.Process.Kill()
			cmd.Wait()
			log.Close()
			if t.Failed() {
				data, _ := os.ReadFile(filepath.Join(dir, name+".log"))
				t.Logf("%s: %s", name, data)
			}
		})
		return path
	}
	serverPath := start("server", map[string]any{
		"inbounds":     []any{map[string]any{"type": "vless", "tag": "vless-in", "listen": "127.0.0.1", "listen_port": vpnPort, "users": []any{map[string]any{"name": account, "uuid": credential}}}},
		"outbounds":    []any{map[string]any{"type": "direct", "tag": "direct"}},
		"experimental": map[string]any{"v2ray_api": map[string]any{"listen": fmt.Sprintf("127.0.0.1:%d", apiPort), "stats": map[string]any{"enabled": true, "users": []string{account}}}},
	})
	start("client", map[string]any{
		"inbounds":  []any{map[string]any{"type": "socks", "listen": "127.0.0.1", "listen_port": socksPort}},
		"outbounds": []any{map[string]any{"type": "vless", "tag": "proxy", "server": "127.0.0.1", "server_port": vpnPort, "uuid": credential}},
	})
	wait := func(p int) {
		deadline := time.Now().Add(5 * time.Second)
		for time.Now().Before(deadline) {
			c, e := net.DialTimeout("tcp", fmt.Sprintf("127.0.0.1:%d", p), 100*time.Millisecond)
			if e == nil {
				c.Close()
				return
			}
			time.Sleep(20 * time.Millisecond)
		}
		t.Fatalf("listener %d not ready", p)
	}
	wait(vpnPort)
	wait(apiPort)
	wait(socksPort)
	payload := bytes.Repeat([]byte("routegate-traffic-verification"), 4096)
	origin := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { w.Write(payload) }))
	defer origin.Close()
	dialer, e := proxy.SOCKS5("tcp", fmt.Sprintf("127.0.0.1:%d", socksPort), nil, proxy.Direct)
	if e != nil {
		t.Fatal(e)
	}
	transport := &http.Transport{Dial: dialer.Dial, DisableKeepAlives: true}
	defer transport.CloseIdleConnections()
	client := &http.Client{Transport: transport, Timeout: 5 * time.Second}
	transfer := func() {
		response, e := client.Get(origin.URL)
		if e != nil {
			t.Fatal(e)
		}
		body, e := io.ReadAll(response.Body)
		response.Body.Close()
		if e != nil || !bytes.Equal(body, payload) {
			t.Fatalf("transfer failed: bytes=%d err=%v", len(body), e)
		}
	}
	collector := NewRuntimeCollector(serverPath, "")
	transfer() // Creates both counters before establishing a baseline.
	first, e := collector.Collect(context.Background())
	if e != nil || len(first) != 1 {
		t.Fatalf("first snapshot: %+v %v", first, e)
	}
	tracker := NewDeltaTracker()
	tracker.BuildUsageEvents(first)
	transfer()
	next, e := collector.Collect(context.Background())
	if e != nil {
		t.Fatal(e)
	}
	events := tracker.BuildUsageEvents(next)
	if len(events) != 1 || events[0].RxBytes <= 0 || events[0].TxBytes < int64(len(payload)) {
		t.Fatalf("traffic not accounted: %+v", events)
	}
	repeated, e := collector.Collect(context.Background())
	if e != nil {
		t.Fatal(e)
	}
	if duplicate := tracker.BuildUsageEvents(repeated); len(duplicate) != 0 {
		t.Fatalf("idle poll repeated usage: %+v", duplicate)
	}
	t.Logf("real VLESS transfer: rx=%d tx=%d", events[0].RxBytes, events[0].TxBytes)
}
