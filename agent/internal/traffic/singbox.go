package traffic

import (
	"context"
	"encoding/json"
	"fmt"
	"net"
	"net/netip"
	"os"
	"sort"
	"strings"
	"time"

	"github.com/ikaevus/routegate/agent/internal/traffic/v2rayapi"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
)

// RuntimeCollector uses the active runtime's local statistics API when configured.
// The file source remains available for existing integrations and development.
type RuntimeCollector struct {
	configPath string
	file       Collector
}

func NewRuntimeCollector(configPath, filePath string) *RuntimeCollector {
	return &RuntimeCollector{configPath: configPath, file: NewFileCollector(filePath)}
}

func (c *RuntimeCollector) Collect(ctx context.Context) ([]CounterSnapshot, error) {
	data, err := os.ReadFile(c.configPath)
	if os.IsNotExist(err) {
		return c.file.Collect(ctx)
	}
	if err != nil {
		return nil, fmt.Errorf("read active traffic config: %w", err)
	}
	var cfg struct {
		Experimental struct {
			API struct {
				Listen string `json:"listen"`
				Stats  struct {
					Enabled bool     `json:"enabled"`
					Users   []string `json:"users"`
				} `json:"stats"`
			} `json:"v2ray_api"`
		} `json:"experimental"`
	}
	if err := json.Unmarshal(data, &cfg); err != nil {
		return nil, fmt.Errorf("parse active traffic config: %w", err)
	}
	api := cfg.Experimental.API
	if api.Listen == "" || !api.Stats.Enabled {
		return c.file.Collect(ctx)
	}
	addr, err := netip.ParseAddrPort(api.Listen)
	if err != nil || !addr.Addr().IsLoopback() || addr.Port() == 0 {
		return nil, fmt.Errorf("traffic statistics API must use a numeric loopback address and nonzero port")
	}
	probeCtx, cancel := context.WithTimeout(ctx, 3*time.Second)
	defer cancel()
	conn, err := grpc.NewClient("passthrough:///"+addr.String(),
		grpc.WithTransportCredentials(insecure.NewCredentials()),
		grpc.WithContextDialer(func(ctx context.Context, _ string) (net.Conn, error) {
			return (&net.Dialer{}).DialContext(ctx, "tcp", addr.String())
		}),
		grpc.WithDefaultCallOptions(grpc.MaxCallRecvMsgSize(4<<20)))
	if err != nil {
		return nil, err
	}
	defer conn.Close()
	// Reading must never reset counters: other consumers and retries share them.
	response := new(v2rayapi.QueryStatsResponse)
	// sing-box registers the V2Ray-compatible service name, overriding its proto package.
	err = conn.Invoke(probeCtx, "/v2ray.core.app.stats.command.StatsService/QueryStats", &v2rayapi.QueryStatsRequest{Patterns: []string{"user>>>"}, Reset_: false}, response)
	if err != nil {
		return nil, fmt.Errorf("query sing-box traffic statistics: %w", err)
	}
	return accountSnapshots(api.Stats.Users, response.Stat, time.Now().UTC())
}

func accountSnapshots(users []string, stats []*v2rayapi.Stat, now time.Time) ([]CounterSnapshot, error) {
	allowed := make(map[string]bool, len(users))
	for _, id := range users {
		allowed[id] = true
	}
	counters := map[string]*CounterSnapshot{}
	seen := map[string]bool{}
	for _, stat := range stats {
		if stat == nil {
			continue
		}
		parts := strings.Split(stat.Name, ">>>")
		if len(parts) != 4 || parts[0] != "user" || parts[2] != "traffic" || !allowed[parts[1]] {
			continue
		}
		if parts[3] != "uplink" && parts[3] != "downlink" {
			continue
		}
		if stat.Value < 0 || seen[stat.Name] {
			return nil, fmt.Errorf("invalid or duplicate traffic counter")
		}
		seen[stat.Name] = true
		id := parts[1]
		if counters[id] == nil {
			counters[id] = &CounterSnapshot{VPNAccountID: id, ObservedAt: now, Metadata: map[string]any{"source": "sing-box-v2ray-api"}}
		}
		// Uplink is received by the VPN server, downlink is sent to the client.
		if parts[3] == "uplink" {
			counters[id].RxBytes = stat.Value
		} else {
			counters[id].TxBytes = stat.Value
		}
	}
	result := make([]CounterSnapshot, 0, len(counters))
	for id, item := range counters {
		// Never manufacture a zero for a missing direction: it would reset baseline.
		if seen["user>>>"+id+">>>traffic>>>uplink"] && seen["user>>>"+id+">>>traffic>>>downlink"] {
			result = append(result, *item)
		}
	}
	sort.Slice(result, func(i, j int) bool { return result[i].VPNAccountID < result[j].VPNAccountID })
	return result, nil
}
