package observability

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

type fakePrometheusRepository struct {
	manager ManagerMetricsSnapshot
	fleet   []FleetNodeMetrics
	health  []HealthCheck
}

func (f fakePrometheusRepository) ManagerSnapshot(context.Context) ManagerMetricsSnapshot {
	return f.manager
}

func (f fakePrometheusRepository) FleetSnapshot(context.Context) ([]FleetNodeMetrics, error) {
	return f.fleet, nil
}

func (f fakePrometheusRepository) CurrentHealth(context.Context) ([]HealthCheck, error) {
	return f.health, nil
}

func TestPrometheusSurfaceFailsClosedAndUsesSeparateBearerToken(t *testing.T) {
	repository := fakePrometheusRepository{manager: ManagerMetricsSnapshot{PostgreSQLUp: true, CollectionSuccessful: true}}

	disabled := NewPrometheusHandler(repository, false, "monitoring-secret")
	response := httptest.NewRecorder()
	disabled.Manager(response, httptest.NewRequest(http.MethodGet, "/metrics", nil))
	if response.Code != http.StatusNotFound {
		t.Fatalf("disabled status=%d, want 404", response.Code)
	}

	missingToken := NewPrometheusHandler(repository, true, "")
	response = httptest.NewRecorder()
	missingToken.Manager(response, httptest.NewRequest(http.MethodGet, "/metrics", nil))
	if response.Code != http.StatusNotFound {
		t.Fatalf("missing token status=%d, want 404", response.Code)
	}

	handler := NewPrometheusHandler(repository, true, "monitoring-secret")
	response = httptest.NewRecorder()
	handler.Manager(response, httptest.NewRequest(http.MethodGet, "/metrics", nil))
	if response.Code != http.StatusUnauthorized {
		t.Fatalf("missing bearer status=%d, want 401", response.Code)
	}

	request := httptest.NewRequest(http.MethodGet, "/metrics", nil)
	request.Header.Set("Authorization", "Bearer wrong-secret")
	response = httptest.NewRecorder()
	handler.Manager(response, request)
	if response.Code != http.StatusUnauthorized {
		t.Fatalf("wrong bearer status=%d, want 401", response.Code)
	}

	request = httptest.NewRequest(http.MethodGet, "/metrics", nil)
	request.Header.Set("Authorization", "Bearer monitoring-secret")
	response = httptest.NewRecorder()
	handler.Manager(response, request)
	if response.Code != http.StatusOK {
		t.Fatalf("valid bearer status=%d body=%s", response.Code, response.Body.String())
	}
	if got := response.Header().Get("Content-Type"); got != prometheusContentType {
		t.Fatalf("content type=%q, want %q", got, prometheusContentType)
	}
}

func TestPrometheusManagerMetricsContractHasBoundedLabels(t *testing.T) {
	handler := NewPrometheusHandler(fakePrometheusRepository{manager: ManagerMetricsSnapshot{
		PostgreSQLUp:         true,
		CollectionSuccessful: true,
		AppliedSchemaVersion: 122,
		Agents: []MetricCount{{Labels: map[string]string{"status": "online"}, Value: 3}},
		Alerts: []MetricCount{{Labels: map[string]string{"severity": "critical", "state": "firing"}, Value: 1}},
		Diagnostics: []MetricCount{{Labels: map[string]string{"profile": DiagnosticProfileHostOverview, "status": "succeeded"}, Value: 2}},
		Deliveries: []MetricCount{{Labels: map[string]string{"status": "pending"}, Value: 1}},
	}}, true, "secret")
	request := httptest.NewRequest(http.MethodGet, "/metrics", nil)
	request.Header.Set("Authorization", "Bearer secret")
	response := httptest.NewRecorder()
	handler.Manager(response, request)
	body := response.Body.String()

	for _, want := range []string{
		"routegate_manager_up 1",
		"routegate_postgresql_up 1",
		"routegate_database_schema_version 122",
		"routegate_agents{status=\"online\"} 3",
		"routegate_alerts_active{severity=\"critical\",state=\"firing\"} 1",
		"routegate_diagnostic_runs{profile=\"host_overview\",status=\"succeeded\"} 2",
		"routegate_delivery_requests{status=\"pending\"} 1",
	} {
		if !strings.Contains(body, want) {
			t.Fatalf("manager metrics missing %q:\n%s", want, body)
		}
	}
	assertPrometheusOutputContainsNoForbiddenIdentityLabels(t, body)
}

func TestPrometheusFleetMetricsExposeFreshnessAndManagerHealthMeaning(t *testing.T) {
	now := time.Date(2026, time.August, 14, 8, 0, 0, 0, time.UTC)
	load1 := 0.75
	logicalCPUs := int64(4)
	memoryTotal := int64(1000)
	memoryAvailable := int64(400)
	rootTotal := int64(2000)
	rootFree := int64(200)
	uptime := int64(7200)
	expiresAt := now.Add(time.Minute)

	handler := NewPrometheusHandler(fakePrometheusRepository{
		fleet: []FleetNodeMetrics{
			{
				ServerID: "server-1", AgentStatus: "online", ReceivedAt: now.Add(-2 * time.Minute),
				Load1: &load1, LogicalCPUs: &logicalCPUs,
				MemoryTotalBytes: &memoryTotal, MemoryAvailableBytes: &memoryAvailable,
				RootFSTotalBytes: &rootTotal, RootFSFreeBytes: &rootFree, UptimeSeconds: &uptime,
				VPNCoreType: "sing-box", VPNCoreInstalled: true, VPNCoreVersion: "1.12.0\"test",
				VPNCoreServiceState: "active",
			},
		},
		health: []HealthCheck{
			{
				Key: CheckAgentTelemetryFreshness, Resource: ResourceRef{Type: "server", ID: "server-1"},
				State: HealthHealthy, Required: true, ObservedAt: now, ExpiresAt: &expiresAt,
			},
			{
				Key: CheckHostDiskCapacity, Resource: ResourceRef{Type: "server", ID: "server-1"},
				State: HealthDegraded, Required: true, ObservedAt: now, ExpiresAt: &expiresAt,
			},
		},
	}, true, "secret")
	handler.now = func() time.Time { return now }

	request := httptest.NewRequest(http.MethodGet, "/metrics/fleet", nil)
	request.Header.Set("Authorization", "Bearer secret")
	response := httptest.NewRecorder()
	handler.Fleet(response, request)
	if response.Code != http.StatusOK {
		t.Fatalf("fleet status=%d body=%s", response.Code, response.Body.String())
	}
	body := response.Body.String()
	for _, want := range []string{
		"routegate_agent_up{server_id=\"server-1\"} 0",
		"routegate_agent_observation_age_seconds{server_id=\"server-1\"} 120",
		"routegate_agent_observation_fresh{server_id=\"server-1\"} 0",
		"routegate_host_memory_usage_ratio{server_id=\"server-1\"} 0.6",
		"routegate_host_root_fs_usage_ratio{server_id=\"server-1\"} 0.9",
		"routegate_vpn_core_info{core=\"sing-box\",server_id=\"server-1\",version=\"1.12.0\\\"test\"} 1",
		"routegate_vpn_core_up{core=\"sing-box\",server_id=\"server-1\"} 1",
		"routegate_health_check{check=\"host.disk.capacity\",server_id=\"server-1\",state=\"degraded\"} 1",
		"routegate_server_health{server_id=\"server-1\",state=\"degraded\"} 1",
	} {
		if !strings.Contains(body, want) {
			t.Fatalf("fleet metrics missing %q:\n%s", want, body)
		}
	}
	assertPrometheusOutputContainsNoForbiddenIdentityLabels(t, body)
}

func TestPrometheusHealthCheckMetricIsExplicitlyAllowListed(t *testing.T) {
	now := time.Now().UTC()
	handler := NewPrometheusHandler(fakePrometheusRepository{health: []HealthCheck{
		{Key: "future.free_form.check", Resource: ResourceRef{Type: "server", ID: "server-1"}, State: HealthUnhealthy, Required: true, ObservedAt: now},
	}}, true, "secret")
	handler.now = func() time.Time { return now }
	request := httptest.NewRequest(http.MethodGet, "/metrics/fleet", nil)
	request.Header.Set("Authorization", "Bearer secret")
	response := httptest.NewRecorder()
	handler.Fleet(response, request)
	body := response.Body.String()
	if strings.Contains(body, "future.free_form.check") {
		t.Fatalf("unreviewed health check leaked into public metrics labels:\n%s", body)
	}
	if !strings.Contains(body, "routegate_server_health{server_id=\"server-1\",state=\"unhealthy\"} 1") {
		t.Fatalf("aggregate health must still include required future checks:\n%s", body)
	}
}

func assertPrometheusOutputContainsNoForbiddenIdentityLabels(t *testing.T, body string) {
	t.Helper()
	for _, forbidden := range []string{
		"vpn_account_id=", "account_id=", "user_id=", "username=", "email=",
		"recipient=", "ip=", "request_id=", "job_id=", "error_message=",
	} {
		if strings.Contains(body, forbidden) {
			t.Fatalf("forbidden high-cardinality or sensitive label %q found:\n%s", forbidden, body)
		}
	}
}
