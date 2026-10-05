package tasks

import (
	"context"
	"net"
	"testing"
	"time"

	"github.com/ikaevus/routegate/agent/internal/platform"
)

func TestSingBoxVLESSAdapterDescriptorMatchesManagedCapability(t *testing.T) {
	adapter := NewSingBoxVLESSAdapter(t.TempDir(), "sing-box", "sing-box")
	descriptor := adapter.Descriptor()
	if descriptor.Core != platform.VPNCoreSingBox || descriptor.Protocol != platform.VPNProtocolVLESS {
		t.Fatalf("unexpected adapter descriptor: %+v", descriptor)
	}
}

func TestSingBoxAdapterReportsOwnListenerInSharedConfig(t *testing.T) {
	vless, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer vless.Close()
	shadowsocks, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer shadowsocks.Close()
	vlessPort := vless.Addr().(*net.TCPAddr).Port
	shadowsocksPort := shadowsocks.Addr().(*net.TCPAddr).Port
	vlessInbound := map[string]any{"type": "vless", "listen_port": vlessPort}
	shadowsocksInbound := map[string]any{"type": "shadowsocks", "listen_port": shadowsocksPort}
	for name, inbounds := range map[string][]map[string]any{
		"vless_first": {vlessInbound, shadowsocksInbound},
		"vless_last":  {shadowsocksInbound, vlessInbound},
	} {
		t.Run(name, func(t *testing.T) {
			path := writeListenerTestConfig(t, inbounds)
			for _, tc := range []struct {
				adapter VPNCoreAdapter
				port    int
			}{
				{NewSingBoxVLESSAdapter(t.TempDir(), "sing-box", "sing-box"), vlessPort},
				{NewSingBoxShadowsocksAdapter(t.TempDir(), "sing-box", "sing-box"), shadowsocksPort},
			} {
				result, err := tc.adapter.CheckHealth(context.Background(), path)
				if err != nil {
					t.Fatal(err)
				}
				if result.Port != tc.port {
					t.Fatalf("%s evidence reports port %d, want %d", tc.adapter.Descriptor().Protocol, result.Port, tc.port)
				}
			}
		})
	}
}

func TestSingBoxVLESSAdapterStillRejectsFailedSharedListener(t *testing.T) {
	vless, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer vless.Close()
	closed, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	closedPort := closed.Addr().(*net.TCPAddr).Port
	if err := closed.Close(); err != nil {
		t.Fatal(err)
	}
	path := writeListenerTestConfig(t, []map[string]any{
		{"type": "vless", "listen_port": vless.Addr().(*net.TCPAddr).Port},
		{"type": "shadowsocks", "listen_port": closedPort},
	})
	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel()
	adapter := NewSingBoxVLESSAdapter(t.TempDir(), "sing-box", "sing-box")
	if _, err := adapter.CheckHealth(ctx, path); err == nil {
		t.Fatal("healthy VLESS must not hide a failed Shadowsocks listener")
	}
}

func TestSingBoxVLESSAdapterDelegatesStageAndValidation(t *testing.T) {
	adapter := NewSingBoxVLESSAdapter(t.TempDir(), "sing-box-test", "sing-box")
	result, err := adapter.Stage(ConfigTask{
		ID:              "task-id",
		ConfigVersionID: "version-id",
		RenderedConfig:  []byte(`{"schemaVersion":"routegate.config.v1","singBox":{"log":{"level":"info"}}}`),
	})
	if err != nil {
		t.Fatalf("stage through adapter: %v", err)
	}
	if result.ConfigVersionID != "version-id" || result.StagedPath == "" {
		t.Fatalf("unexpected stage result: %+v", result)
	}

	concrete := adapter.(singBoxVLESSAdapter)
	concrete.validator.run = func(_ context.Context, name string, args ...string) ([]byte, error) {
		if name != "sing-box-test" || len(args) != 3 || args[0] != "check" || args[1] != "-c" {
			t.Fatalf("unexpected validation command: %s %v", name, args)
		}
		return nil, nil
	}
	if _, err := concrete.Validate(context.Background(), result.StagedPath); err != nil {
		t.Fatalf("validate through adapter: %v", err)
	}
}
