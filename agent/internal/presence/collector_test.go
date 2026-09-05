package presence

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestFileCollectorMissingFileReturnsAuthoritativeEmptySnapshot(t *testing.T) {
	collector := NewFileCollector(filepath.Join(t.TempDir(), "missing.json"))
	now := time.Date(2026, 9, 3, 20, 0, 0, 0, time.UTC)
	collector.now = func() time.Time { return now }

	snapshot, err := collector.Collect(context.Background())
	if err != nil { t.Fatal(err) }
	if !snapshot.ObservedAt.Equal(now) { t.Fatalf("observedAt=%s want %s", snapshot.ObservedAt, now) }
	if snapshot.Items == nil || len(snapshot.Items) != 0 { t.Fatalf("items=%v want an explicit empty slice", snapshot.Items) }
}

func TestSingBoxCollectorReportsOnlyAuthenticatedEstablishedSessions(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.json")
	data := []byte(`{
  "inbounds": [{
    "type": "vless",
    "listen_port": 8443,
    "tls": {"reality": {"enabled": true}},
    "users": [
      {"name": "Felix", "uuid": "523446e8-0351-4c0a-a9ec-19a269a8848f"},
      {"name": "Alice", "uuid": "d06eef43-b782-4f9d-b590-c2a991b95f31"}
    ]
  }]
}`)
	if err := os.WriteFile(path, data, 0o600); err != nil {
		t.Fatal(err)
	}
	collector := NewSingBoxCollector(path, "sing-box")
	now := time.Date(2026, 9, 4, 12, 0, 0, 0, time.UTC)
	collector.now = func() time.Time { return now }
	collector.run = func(_ context.Context, name string, _ ...string) ([]byte, error) {
		switch name {
		case "journalctl":
			return []byte(`INFO[0001] [1001 0ms] inbound/vless[vless-in]: inbound connection from 203.0.113.10:51001
INFO[0001] [1001 88ms] inbound/vless[vless-in]: [Felix] inbound connection to example.com:443
INFO[0002] [1002 0ms] inbound/vless[vless-in]: inbound connection from 203.0.113.10:51002
INFO[0002] [1002 91ms] inbound/vless[vless-in]: [Felix] inbound multiplex connection to sp.mux.sing-box.arpa:444
INFO[0003] [1003 0ms] inbound/vless[vless-in]: inbound connection from 203.0.113.11:52001
`), nil
		case "ss":
			return []byte(`0 0 [::]:8443 [::ffff:203.0.113.10]:51001
0 0 10.0.0.1:8443 203.0.113.10:51002
0 0 10.0.0.1:8443 203.0.113.11:52001
0 0 10.0.0.1:22 203.0.113.12:53001
`), nil
		default:
			return nil, errors.New("unexpected command")
		}
	}

	snapshot, err := collector.Collect(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if !snapshot.ObservedAt.Equal(now) || len(snapshot.Items) != 1 {
		t.Fatalf("snapshot=%+v", snapshot)
	}
	item := snapshot.Items[0]
	if item.VPNAccountID != "523446e8-0351-4c0a-a9ec-19a269a8848f" || item.Protocol != "vless-reality" || item.ConnectionCount != 2 {
		t.Fatalf("item=%+v", item)
	}
	if item.Source != SingBoxSocketCollectorSource || item.Confidence != "exact" {
		t.Fatalf("item=%+v", item)
	}
}

func TestSingBoxCollectorReturnsEmptyAfterDisconnect(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.json")
	if err := os.WriteFile(path, []byte(`{"inbounds":[{"type":"vless","listen_port":8443,"users":[{"name":"Felix","uuid":"523446e8-0351-4c0a-a9ec-19a269a8848f"}]}]}`), 0o600); err != nil {
		t.Fatal(err)
	}
	collector := NewSingBoxCollector(path, "sing-box.service")
	collector.run = func(_ context.Context, _ string, _ ...string) ([]byte, error) { return []byte{}, nil }
	snapshot, err := collector.Collect(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if snapshot.Items == nil || len(snapshot.Items) != 0 {
		t.Fatalf("items=%v, want authoritative empty snapshot", snapshot.Items)
	}
}

func TestParseAuthenticatedPeersClearsReusedSocketBeforeAuthentication(t *testing.T) {
	journal := `INFO [1 0ms] inbound/vless[vless-in]: inbound connection from 203.0.113.10:51001
INFO [1 50ms] inbound/vless[vless-in]: [Felix] inbound connection to example.com:443
INFO [2 0ms] inbound/vless[vless-in]: inbound connection from 203.0.113.10:51001`
	peers := parseAuthenticatedPeers(journal)
	peer, err := parseAddrPort("203.0.113.10:51001")
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := peers[peer]; ok {
		t.Fatal("reused socket must not inherit authentication from an older connection")
	}
}

func TestParseAuthenticatedPeersClearsReusedContextBeforeAuthentication(t *testing.T) {
	journal := `INFO [1 0ms] inbound/vless[vless-in]: inbound connection from 203.0.113.10:51001
INFO [1 50ms] inbound/vless[vless-in]: [Felix] inbound connection to example.com:443
INFO [1 0ms] inbound/vless[vless-in]: inbound connection from 203.0.113.11:52001`
	peers := parseAuthenticatedPeers(journal)
	oldPeer, err := parseAddrPort("203.0.113.10:51001")
	if err != nil {
		t.Fatal(err)
	}
	newPeer, err := parseAddrPort("203.0.113.11:52001")
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := peers[oldPeer]; ok {
		t.Fatal("reused context must remove the previous peer")
	}
	if _, ok := peers[newPeer]; ok {
		t.Fatal("reused context must authenticate the new peer independently")
	}
}

func TestFileCollectorNormalizesSafeDefaults(t *testing.T) {
	path := filepath.Join(t.TempDir(), "presence.json")
	data := []byte(`{"items":[{"vpnAccountId":" account-id ","protocol":" vless-reality ","connectionCount":2}]}`)
	if err := os.WriteFile(path, data, 0o600); err != nil { t.Fatal(err) }
	collector := NewFileCollector(path)
	collector.now = func() time.Time { return time.Date(2026, 9, 3, 20, 0, 0, 0, time.UTC) }

	snapshot, err := collector.Collect(context.Background())
	if err != nil { t.Fatal(err) }
	if len(snapshot.Items) != 1 { t.Fatalf("items=%d want 1", len(snapshot.Items)) }
	item := snapshot.Items[0]
	if item.VPNAccountID != "account-id" || item.Protocol != "vless-reality" { t.Fatalf("item=%+v", item) }
	if item.Source != FileCollectorSource || item.Confidence != "exact" { t.Fatalf("defaults=%+v", item) }
}

func TestFileCollectorRejectsInvalidConfidence(t *testing.T) {
	path := filepath.Join(t.TempDir(), "presence.json")
	data := []byte(`{"items":[{"vpnAccountId":"account-id","protocol":"wireguard","connectionCount":1,"confidence":"certain"}]}`)
	if err := os.WriteFile(path, data, 0o600); err != nil { t.Fatal(err) }
	_, err := NewFileCollector(path).Collect(context.Background())
	if err == nil { t.Fatal("expected invalid confidence error") }
}
