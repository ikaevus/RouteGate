package tasks

import (
	"testing"

	"github.com/ikaevus/routegate/agent/internal/platform"
)

func TestSingBoxShadowsocksAdapterDescriptor(t *testing.T) {
	adapter := NewSingBoxShadowsocksAdapter(t.TempDir(), "sing-box", "sing-box")
	descriptor := adapter.Descriptor()
	if descriptor.Core != platform.VPNCoreSingBox || descriptor.Protocol != platform.VPNProtocolShadowsocks || descriptor.Transports[0] != platform.VPNTransportTCP || descriptor.SecurityModes[0] != platform.VPNSecurityAEAD2022 {
		t.Fatalf("unexpected Shadowsocks descriptor: %+v", descriptor)
	}
}

func TestSelectVPNCoreAdapterUsesShadowsocksDescriptor(t *testing.T) {
	vless := NewSingBoxVLESSAdapter(t.TempDir(), "sing-box", "sing-box")
	shadowsocks := NewSingBoxShadowsocksAdapter(t.TempDir(), "sing-box", "sing-box")
	task := ConfigTask{RenderedConfig: []byte(`{"metadata":{"vpnCore":{"core":"sing-box","protocol":"shadowsocks","transport":"tcp","security":"aead-2022"}}}`)}
	selected, err := SelectVPNCoreAdapter(task, vless, nil, shadowsocks)
	if err != nil { t.Fatalf("select Shadowsocks: %v", err) }
	if selected.Descriptor().Protocol != platform.VPNProtocolShadowsocks { t.Fatalf("unexpected adapter: %+v", selected.Descriptor()) }
}
