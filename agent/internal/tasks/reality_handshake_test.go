package tasks

import (
	"context"
	"crypto/tls"
	"errors"
	"net"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"
)

func writeRealityTestConfig(t *testing.T, server string, port int) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "config.json")
	payload := `{"inbounds":[{"type":"vless","listen_port":8443,"tls":{"enabled":true,"server_name":"` + server +
		`","reality":{"enabled":true,"handshake":{"server":"` + server + `","server_port":` + strconv.Itoa(port) + `}}}}]}`
	if err := os.WriteFile(path, []byte(payload), 0o600); err != nil {
		t.Fatal(err)
	}
	return path
}

func TestCheckRealityHandshakeTargetsAcceptsTLS13Target(t *testing.T) {
	server := httptest.NewUnstartedServer(nil)
	server.TLS = &tls.Config{MinVersion: tls.VersionTLS13}
	server.StartTLS()
	defer server.Close()
	_, port, _ := net.SplitHostPort(server.Listener.Addr().String())
	portNumber, _ := strconv.Atoi(port)

	path := writeRealityTestConfig(t, "127.0.0.1", portNumber)
	if err := checkRealityHandshakeTargets(context.Background(), path, 5*time.Second, dialRealityHandshakeTarget); err != nil {
		t.Fatalf("expected reachable TLS 1.3 target to pass: %v", err)
	}
}

func TestCheckRealityHandshakeTargetsRejectsUnreachableTarget(t *testing.T) {
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	port := listener.Addr().(*net.TCPAddr).Port
	listener.Close()

	path := writeRealityTestConfig(t, "127.0.0.1", port)
	err = checkRealityHandshakeTargets(context.Background(), path, 2*time.Second, dialRealityHandshakeTarget)
	if err == nil || !strings.Contains(err.Error(), "Reality handshake target 127.0.0.1:"+strconv.Itoa(port)+" is not usable from this node") {
		t.Fatalf("expected unreachable target error, got %v", err)
	}
}

func TestCheckRealityHandshakeTargetsReportsNXDOMAIN(t *testing.T) {
	path := writeRealityTestConfig(t, "ru.example.invalid", 443)
	dial := func(context.Context, realityHandshakeTarget) error {
		return &net.DNSError{Err: "no such host", Name: "ru.example.invalid", IsNotFound: true}
	}
	err := checkRealityHandshakeTargets(context.Background(), path, time.Second, dial)
	if err == nil || !strings.Contains(err.Error(), "NXDOMAIN") || !strings.Contains(err.Error(), "render and apply a new version") {
		t.Fatalf("expected actionable NXDOMAIN error, got %v", err)
	}
}

func TestCheckRealityHandshakeTargetsSkipsConfigsWithoutReality(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.json")
	if err := os.WriteFile(path, []byte(`{"inbounds":[{"type":"vless","listen_port":8443},{"type":"shadowsocks","listen_port":8388}]}`), 0o600); err != nil {
		t.Fatal(err)
	}
	dial := func(context.Context, realityHandshakeTarget) error { return errors.New("must not dial") }
	if err := checkRealityHandshakeTargets(context.Background(), path, time.Second, dial); err != nil {
		t.Fatalf("expected no probe without Reality: %v", err)
	}
}

func TestSingBoxVLESSAdapterValidateRunsRealityCheckAfterSingBoxCheck(t *testing.T) {
	adapter := NewSingBoxVLESSAdapter(t.TempDir(), "sing-box-test", "sing-box").(singBoxVLESSAdapter)
	adapter.validator.run = func(context.Context, string, ...string) ([]byte, error) { return nil, nil }
	called := ""
	adapter.realityCheck = func(_ context.Context, path string) error {
		called = path
		return errors.New("probe failed")
	}
	if _, err := adapter.Validate(context.Background(), "/staged/config.json"); err == nil || err.Error() != "probe failed" {
		t.Fatalf("expected Reality probe error, got %v", err)
	}
	if called != "/staged/config.json" {
		t.Fatalf("Reality probe path = %q", called)
	}

	adapter.validator.run = func(context.Context, string, ...string) ([]byte, error) {
		return []byte("bad config"), errors.New("exit 1")
	}
	called = ""
	if _, err := adapter.Validate(context.Background(), "/staged/config.json"); err == nil || !strings.Contains(err.Error(), "sing-box check failed") {
		t.Fatalf("expected sing-box check error, got %v", err)
	}
	if called != "" {
		t.Fatal("Reality probe must not run after sing-box check fails")
	}
}
