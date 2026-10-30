package traffic

import (
	"context"
	"fmt"
	"net"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/ikaevus/routegate/agent/internal/traffic/v2rayapi"
	"google.golang.org/grpc"
)

type statsFixture struct {
	v2rayapi.UnimplementedStatsServiceServer
	up int64
}

func (s *statsFixture) QueryStats(_ context.Context, r *v2rayapi.QueryStatsRequest) (*v2rayapi.QueryStatsResponse, error) {
	if r.Reset_ || len(r.Patterns) != 1 || r.Patterns[0] != "user>>>" {
		return nil, fmt.Errorf("unsafe statistics query")
	}
	return &v2rayapi.QueryStatsResponse{Stat: []*v2rayapi.Stat{
		{Name: "user>>>account-1>>>traffic>>>uplink", Value: s.up},
		{Name: "user>>>account-1>>>traffic>>>downlink", Value: 200},
		{Name: "user>>>other>>>traffic>>>uplink", Value: 9999},
		{Name: "user>>>other>>>traffic>>>downlink", Value: 9999},
	}}, nil
}

func TestRuntimeCollectorQueriesV2RayCompatibleService(t *testing.T) {
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	server := grpc.NewServer()
	fixture := &statsFixture{up: 100}
	desc := v2rayapi.StatsService_ServiceDesc
	desc.ServiceName = "v2ray.core.app.stats.command.StatsService"
	server.RegisterService(&desc, fixture)
	go server.Serve(listener)
	t.Cleanup(server.Stop)
	path := filepath.Join(t.TempDir(), "config.json")
	if err := os.WriteFile(path, []byte(fmt.Sprintf(`{"experimental":{"v2ray_api":{"listen":%q,"stats":{"enabled":true,"users":["account-1"]}}}}`, listener.Addr().String())), 0600); err != nil {
		t.Fatal(err)
	}
	collector := NewRuntimeCollector(path, "")
	first, err := collector.Collect(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if len(first) != 1 || first[0].RxBytes != 100 || first[0].TxBytes != 200 {
		t.Fatalf("unexpected counters: %+v", first)
	}
	tracker := NewDeltaTracker()
	tracker.BuildUsageEvents(first)
	// Stop the first connection before changing fixture state; each Collect closes it.
	fixture.up = 180
	next, err := collector.Collect(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	events := tracker.BuildUsageEvents(next)
	if len(events) != 1 || events[0].RxBytes != 80 || events[0].TxBytes != 0 {
		t.Fatalf("unexpected delta: %+v", events)
	}
}

func TestRuntimeCollectorRejectsRemoteAPI(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.json")
	for _, addr := range []string{"0.0.0.0:8080", "192.0.2.1:8080", "localhost:8080", "127.0.0.1:0"} {
		if err := os.WriteFile(path, []byte(fmt.Sprintf(`{"experimental":{"v2ray_api":{"listen":%q,"stats":{"enabled":true}}}}`, addr)), 0600); err != nil {
			t.Fatal(err)
		}
		if _, err := NewRuntimeCollector(path, "").Collect(context.Background()); err == nil {
			t.Fatalf("accepted %s", addr)
		}
	}
}

func TestRuntimeCollectorRetainsFileSource(t *testing.T) {
	dir := t.TempDir()
	file := filepath.Join(dir, "traffic.json")
	os.WriteFile(file, []byte(`[{"vpnAccountId":"account-1","rxBytes":10,"txBytes":20}]`), 0600)
	result, err := NewRuntimeCollector(filepath.Join(dir, "missing-config.json"), file).Collect(context.Background())
	if err != nil || len(result) != 1 {
		t.Fatalf("fallback: %+v, %v", result, err)
	}
}

func TestAccountSnapshotsRejectsPartialAndInvalidCounters(t *testing.T) {
	pair := []*v2rayapi.Stat{{Name: "user>>>a>>>traffic>>>uplink", Value: 10}, {Name: "user>>>a>>>traffic>>>downlink", Value: 20}}
	result, err := accountSnapshots([]string{"a"}, pair[:1], time.Now())
	if err != nil || len(result) != 0 {
		t.Fatalf("partial counters: %+v %v", result, err)
	}
	_, err = accountSnapshots([]string{"a"}, append(pair, pair[0]), time.Now())
	if err == nil {
		t.Fatal("accepted duplicate")
	}
	pair[0].Value = -1
	_, err = accountSnapshots([]string{"a"}, pair, time.Now())
	if err == nil {
		t.Fatal("accepted negative")
	}
}
