package dashboard

import (
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

type fakeActivityReader struct {
	deployments     []RecentDeployment
	auditEvents     []RecentAuditEvent
	deploymentErr   error
	auditErr        error
	deploymentLimit int
	auditLimit      int
}

func (f *fakeActivityReader) ListRecentDeployments(_ context.Context, limit int) ([]RecentDeployment, error) {
	f.deploymentLimit = limit
	return f.deployments, f.deploymentErr
}

func (f *fakeActivityReader) ListRecentAuditEvents(_ context.Context, limit int) ([]RecentAuditEvent, error) {
	f.auditLimit = limit
	return f.auditEvents, f.auditErr
}

type fakeTrafficReader struct {
	snapshot    TrafficSnapshot
	err         error
	currentTime time.Time
	serverLimit int
}

func (f *fakeTrafficReader) GetTrafficSnapshot(_ context.Context, currentTime time.Time, serverLimit int) (TrafficSnapshot, error) {
	f.currentTime = currentTime
	f.serverLimit = serverLimit
	return f.snapshot, f.err
}

func TestActivityReturnsBoundedSanitizedData(t *testing.T) {
	now := time.Date(2026, time.August, 11, 1, 30, 0, 0, time.UTC)
	reader := &fakeActivityReader{
		deployments: []RecentDeployment{{
			ID: "job-1", ServerID: "server-1", ServerName: "US VPS", ConfigVersionID: "config-1",
			ConfigVersion: 7, Action: "apply", Status: "succeeded", CreatedAt: now,
		}},
		auditEvents: []RecentAuditEvent{{
			ID: "audit-1", Actor: "Felix", ActorType: "user", Action: "config.apply",
			ResourceType: "server", ResourceID: "server-1", Result: "success", CreatedAt: now,
		}},
	}
	handler := &Handler{logger: slog.Default(), activity: reader}

	response := httptest.NewRecorder()
	handler.Activity(response, httptest.NewRequest(http.MethodGet, "/api/v1/dashboard/activity", nil))

	if response.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d; body=%s", response.Code, http.StatusOK, response.Body.String())
	}
	if reader.deploymentLimit != recentActivityLimit || reader.auditLimit != recentActivityLimit {
		t.Fatalf("unexpected limits: deployments=%d audit=%d", reader.deploymentLimit, reader.auditLimit)
	}

	var payload ActivityResponse
	if err := json.Unmarshal(response.Body.Bytes(), &payload); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if len(payload.RecentDeployments) != 1 || payload.RecentDeployments[0].ConfigVersion != 7 {
		t.Fatalf("unexpected deployments: %+v", payload.RecentDeployments)
	}
	if len(payload.RecentAuditEvents) != 1 || payload.RecentAuditEvents[0].Actor != "Felix" {
		t.Fatalf("unexpected audit events: %+v", payload.RecentAuditEvents)
	}
}

func TestActivityStopsWhenDeploymentQueryFails(t *testing.T) {
	reader := &fakeActivityReader{deploymentErr: errors.New("boom")}
	handler := &Handler{logger: slog.Default(), activity: reader}

	response := httptest.NewRecorder()
	handler.Activity(response, httptest.NewRequest(http.MethodGet, "/api/v1/dashboard/activity", nil))

	if response.Code != http.StatusInternalServerError {
		t.Fatalf("status = %d, want %d", response.Code, http.StatusInternalServerError)
	}
	if reader.auditLimit != 0 {
		t.Fatalf("audit query should not run after deployment failure; limit=%d", reader.auditLimit)
	}
}

func TestTrafficReturnsBoundedSnapshot(t *testing.T) {
	now := time.Date(2026, time.August, 11, 2, 0, 0, 0, time.UTC)
	reader := &fakeTrafficReader{snapshot: TrafficSnapshot{
		GeneratedAt: now,
		Monthly: TrafficTotals{RxBytes: 100, TxBytes: 50, TotalBytes: 150},
		Daily: []DailyTrafficUsage{{Date: "2026-08-11", TrafficTotals: TrafficTotals{RxBytes: 10, TxBytes: 5, TotalBytes: 15}}},
		Servers: []ServerTrafficUsage{{ServerID: "server-1", ServerName: "US VPS", TrafficTotals: TrafficTotals{RxBytes: 8, TxBytes: 2, TotalBytes: 10}}},
	}}
	handler := &Handler{logger: slog.Default(), traffic: reader, now: func() time.Time { return now }}

	response := httptest.NewRecorder()
	handler.Traffic(response, httptest.NewRequest(http.MethodGet, "/api/v1/dashboard/traffic", nil))

	if response.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d; body=%s", response.Code, http.StatusOK, response.Body.String())
	}
	if !reader.currentTime.Equal(now) || reader.serverLimit != dashboardServerLimit {
		t.Fatalf("unexpected traffic request: time=%s limit=%d", reader.currentTime, reader.serverLimit)
	}

	var payload TrafficSnapshot
	if err := json.Unmarshal(response.Body.Bytes(), &payload); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if payload.Monthly.TotalBytes != 150 || len(payload.Daily) != 1 || len(payload.Servers) != 1 {
		t.Fatalf("unexpected traffic payload: %+v", payload)
	}
}

func TestTrafficReturnsDatabaseError(t *testing.T) {
	reader := &fakeTrafficReader{err: errors.New("boom")}
	handler := &Handler{logger: slog.Default(), traffic: reader, now: time.Now}

	response := httptest.NewRecorder()
	handler.Traffic(response, httptest.NewRequest(http.MethodGet, "/api/v1/dashboard/traffic", nil))

	if response.Code != http.StatusInternalServerError {
		t.Fatalf("status = %d, want %d", response.Code, http.StatusInternalServerError)
	}
}
