package client

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/ikaevus/routegate/agent/internal/config"
	"github.com/ikaevus/routegate/agent/internal/systeminfo"
	"github.com/ikaevus/routegate/agent/internal/traffic"
)

func TestRegisterSendsVersionAndProtocol(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/v1/agent/register" {
			t.Fatalf("unexpected path: %s", r.URL.Path)
		}
		var request struct {
			AgentVersion    string `json:"agentVersion"`
			ProtocolVersion int    `json:"protocolVersion"`
		}
		if err := json.NewDecoder(r.Body).Decode(&request); err != nil {
			t.Fatalf("decode request: %v", err)
		}
		if request.AgentVersion != "dev" || request.ProtocolVersion != 1 {
			t.Fatalf("unexpected version payload: %+v", request)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"agentId":"agent-1","serverId":"server-1","agentToken":"rg_agent_secret"}`))
	}))
	defer server.Close()

	client := New(server.URL)
	_, err := client.Register(context.Background(), config.Config{RegistrationToken: "rg_reg_secret"}, systeminfo.Info{
		AgentVersion:    "dev",
		ProtocolVersion: 1,
	})
	if err != nil {
		t.Fatalf("register: %v", err)
	}
}

func TestHeartbeatSendsVersionProtocolAndRuntimeMetrics(t *testing.T) {
	collectedAt := time.Date(2026, time.August, 11, 0, 0, 0, 0, time.UTC)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/v1/agent/heartbeat" {
			t.Fatalf("unexpected path: %s", r.URL.Path)
		}
		var request struct {
			AgentVersion    string                     `json:"agentVersion"`
			ProtocolVersion int                        `json:"protocolVersion"`
			RuntimeMetrics  *systeminfo.RuntimeMetrics `json:"runtimeMetrics"`
		}
		if err := json.NewDecoder(r.Body).Decode(&request); err != nil {
			t.Fatalf("decode request: %v", err)
		}
		if request.AgentVersion != "dev" || request.ProtocolVersion != 1 {
			t.Fatalf("unexpected version payload: %+v", request)
		}
		if request.RuntimeMetrics == nil || request.RuntimeMetrics.Load1 != 0.42 || request.RuntimeMetrics.LogicalCPUs != 4 || !request.RuntimeMetrics.CollectedAt.Equal(collectedAt) {
			t.Fatalf("unexpected runtime metrics: %+v", request.RuntimeMetrics)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"ok":true,"agentId":"agent-1","serverId":"server-1","serverStatus":"active"}`))
	}))
	defer server.Close()

	client := New(server.URL)
	_, err := client.Heartbeat(context.Background(), "rg_agent_secret", systeminfo.Info{
		AgentVersion:    "dev",
		ProtocolVersion: 1,
		RuntimeMetrics: &systeminfo.RuntimeMetrics{
			Load1: 0.42, Load5: 0.25, Load15: 0.17, LogicalCPUs: 4, CollectedAt: collectedAt,
		},
	})
	if err != nil {
		t.Fatalf("heartbeat: %v", err)
	}
}

func TestReportTrafficUsageSendsEvents(t *testing.T) {
	observedAt := time.Date(2026, time.June, 25, 10, 0, 0, 0, time.UTC)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			t.Fatalf("expected POST, got %s", r.Method)
		}
		if r.URL.Path != "/api/v1/agent/traffic-usage" {
			t.Fatalf("unexpected path: %s", r.URL.Path)
		}
		if r.Header.Get("Authorization") != "Bearer agent-token" {
			t.Fatalf("unexpected authorization header: %q", r.Header.Get("Authorization"))
		}

		var request struct {
			Events []traffic.UsageEvent `json:"events"`
		}
		if err := json.NewDecoder(r.Body).Decode(&request); err != nil {
			t.Fatalf("decode request: %v", err)
		}
		if len(request.Events) != 1 {
			t.Fatalf("expected one event, got %d", len(request.Events))
		}
		if request.Events[0].VPNAccountID != "account-1" || request.Events[0].RxBytes != 128 || request.Events[0].TxBytes != 256 {
			t.Fatalf("unexpected traffic event: %+v", request.Events[0])
		}

		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"ok":true,"agentId":"agent-1","serverId":"server-1","accepted":1}`))
	}))
	defer server.Close()

	client := New(server.URL)
	response, err := client.ReportTrafficUsage(context.Background(), "agent-token", []traffic.UsageEvent{{
		VPNAccountID: "account-1",
		RxBytes:      128,
		TxBytes:      256,
		ObservedAt:   observedAt,
	}})
	if err != nil {
		t.Fatalf("report traffic usage: %v", err)
	}
	if !response.OK || response.Accepted != 1 || response.AgentID != "agent-1" || response.ServerID != "server-1" {
		t.Fatalf("unexpected response: %+v", response)
	}
}

func TestReportTrafficUsageSkipsEmptyEvents(t *testing.T) {
	client := New("http://127.0.0.1:1")

	response, err := client.ReportTrafficUsage(context.Background(), "agent-token", nil)
	if err != nil {
		t.Fatalf("report empty traffic usage: %v", err)
	}
	if !response.OK || response.Accepted != 0 {
		t.Fatalf("unexpected response: %+v", response)
	}
}
