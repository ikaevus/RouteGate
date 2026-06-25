package traffic

import (
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/ikaevus/routegate/backend/internal/agents"
)

type fakeTrafficRepository struct {
	reportTokenHash string
	reportEvents    []CreateUsageEventInput
	reportResult    TrafficUsageReport
	reportErr       error

	summaryAccountID string
	summaryFrom      time.Time
	summaryTo        time.Time
	summaryResult    TrafficUsageSummary
	summaryErr       error

	limitAccountID string
	limitInput     UpsertTrafficLimitInput
	limitResult    TrafficLimit
	limitErr       error
}

func (f *fakeTrafficRepository) ReportUsage(_ context.Context, tokenHash string, events []CreateUsageEventInput) (TrafficUsageReport, error) {
	f.reportTokenHash = tokenHash
	f.reportEvents = events
	if f.reportErr != nil {
		return TrafficUsageReport{}, f.reportErr
	}
	if f.reportResult.AgentID != "" {
		return f.reportResult, nil
	}
	return TrafficUsageReport{OK: true, AgentID: "agent-1", ServerID: "server-1", Accepted: len(events)}, nil
}

func (f *fakeTrafficRepository) GetUsageSummary(_ context.Context, vpnAccountID string, from time.Time, to time.Time) (TrafficUsageSummary, error) {
	f.summaryAccountID = vpnAccountID
	f.summaryFrom = from
	f.summaryTo = to
	if f.summaryErr != nil {
		return TrafficUsageSummary{}, f.summaryErr
	}
	if f.summaryResult.VPNAccountID != "" {
		return f.summaryResult, nil
	}
	return TrafficUsageSummary{
		VPNAccountID: vpnAccountID,
		Period:      TrafficPeriod{From: from, To: to},
		Usage:       TrafficUsageTotals{RxBytes: 10, TxBytes: 20, TotalBytes: 30},
	}, nil
}

func (f *fakeTrafficRepository) UpsertLimit(_ context.Context, vpnAccountID string, input UpsertTrafficLimitInput) (TrafficLimit, error) {
	f.limitAccountID = vpnAccountID
	f.limitInput = input
	if f.limitErr != nil {
		return TrafficLimit{}, f.limitErr
	}
	if f.limitResult.VPNAccountID != "" {
		return f.limitResult, nil
	}
	return TrafficLimit{VPNAccountID: vpnAccountID, MonthlyLimitBytes: input.MonthlyLimitBytes, HardLimitEnabled: input.HardLimitEnabled, SpeedLimitBps: input.SpeedLimitBps, ResetDay: input.ResetDay, CreatedAt: time.Now(), UpdatedAt: time.Now()}, nil
}

func newTestHandler(repo *fakeTrafficRepository) *Handler {
	return &Handler{
		logger:  slog.New(slog.NewTextHandler(io.Discard, nil)),
		traffic: repo,
		now: func() time.Time {
			return time.Date(2026, time.June, 25, 12, 0, 0, 0, time.UTC)
		},
	}
}

func TestReportUsageRequiresBearerToken(t *testing.T) {
	handler := newTestHandler(&fakeTrafficRepository{})
	request := httptest.NewRequest(http.MethodPost, "/api/v1/agent/traffic-usage", strings.NewReader(`{"events":[]}`))
	response := httptest.NewRecorder()

	handler.ReportUsage(response, request)

	if response.Code != http.StatusUnauthorized {
		t.Fatalf("expected status %d, got %d", http.StatusUnauthorized, response.Code)
	}
}

func TestReportUsageStoresAgentUsageEvents(t *testing.T) {
	repo := &fakeTrafficRepository{}
	handler := newTestHandler(repo)
	request := httptest.NewRequest(http.MethodPost, "/api/v1/agent/traffic-usage", strings.NewReader(`{
		"events": [
			{"vpnAccountId":"account-1","rxBytes":1024,"txBytes":2048,"observedAt":"2026-06-25T10:00:00Z","metadata":{"source":"test"}}
		]
	}`))
	request.Header.Set("Authorization", "Bearer raw-agent-token")
	response := httptest.NewRecorder()

	handler.ReportUsage(response, request)

	if response.Code != http.StatusAccepted {
		t.Fatalf("expected status %d, got %d", http.StatusAccepted, response.Code)
	}
	if repo.reportTokenHash != agents.HashToken("raw-agent-token") {
		t.Fatalf("expected hashed agent token, got %q", repo.reportTokenHash)
	}
	if len(repo.reportEvents) != 1 {
		t.Fatalf("expected one usage event, got %d", len(repo.reportEvents))
	}
	event := repo.reportEvents[0]
	if event.VPNAccountID != "account-1" || event.RxBytes != 1024 || event.TxBytes != 2048 {
		t.Fatalf("unexpected usage event: %+v", event)
	}
	if event.ObservedAt != time.Date(2026, time.June, 25, 10, 0, 0, 0, time.UTC) {
		t.Fatalf("unexpected observedAt: %s", event.ObservedAt)
	}
}

func TestGetAccountUsageUsesCurrentMonthByDefault(t *testing.T) {
	repo := &fakeTrafficRepository{}
	handler := newTestHandler(repo)
	request := httptest.NewRequest(http.MethodGet, "/api/v1/vpn-accounts/account-1/traffic", nil)
	request.SetPathValue("id", "account-1")
	response := httptest.NewRecorder()

	handler.GetAccountUsage(response, request)

	if response.Code != http.StatusOK {
		t.Fatalf("expected status %d, got %d", http.StatusOK, response.Code)
	}
	if repo.summaryAccountID != "account-1" {
		t.Fatalf("expected account-1 summary, got %q", repo.summaryAccountID)
	}
	expectedFrom := time.Date(2026, time.June, 1, 0, 0, 0, 0, time.UTC)
	expectedTo := time.Date(2026, time.July, 1, 0, 0, 0, 0, time.UTC)
	if !repo.summaryFrom.Equal(expectedFrom) || !repo.summaryTo.Equal(expectedTo) {
		t.Fatalf("unexpected period: from=%s to=%s", repo.summaryFrom, repo.summaryTo)
	}

	var body TrafficUsageSummary
	if err := json.NewDecoder(response.Body).Decode(&body); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if body.Usage.TotalBytes != 30 {
		t.Fatalf("expected total usage 30, got %d", body.Usage.TotalBytes)
	}
}

func TestUpdateAccountLimitRejectsInvalidLimit(t *testing.T) {
	handler := newTestHandler(&fakeTrafficRepository{})
	request := httptest.NewRequest(http.MethodPatch, "/api/v1/vpn-accounts/account-1/traffic-limit", strings.NewReader(`{"monthlyLimitBytes":-1}`))
	request.SetPathValue("id", "account-1")
	response := httptest.NewRecorder()

	handler.UpdateAccountLimit(response, request)

	if response.Code != http.StatusBadRequest {
		t.Fatalf("expected status %d, got %d", http.StatusBadRequest, response.Code)
	}
}

func TestUpdateAccountLimitPersistsLimit(t *testing.T) {
	repo := &fakeTrafficRepository{}
	handler := newTestHandler(repo)
	request := httptest.NewRequest(http.MethodPatch, "/api/v1/vpn-accounts/account-1/traffic-limit", strings.NewReader(`{"monthlyLimitBytes":1073741824,"hardLimitEnabled":true,"speedLimitBps":1048576,"resetDay":1}`))
	request.SetPathValue("id", "account-1")
	response := httptest.NewRecorder()

	handler.UpdateAccountLimit(response, request)

	if response.Code != http.StatusOK {
		t.Fatalf("expected status %d, got %d", http.StatusOK, response.Code)
	}
	if repo.limitAccountID != "account-1" {
		t.Fatalf("expected account-1 limit update, got %q", repo.limitAccountID)
	}
	if repo.limitInput.MonthlyLimitBytes == nil || *repo.limitInput.MonthlyLimitBytes != 1073741824 {
		t.Fatalf("unexpected monthly limit input: %+v", repo.limitInput.MonthlyLimitBytes)
	}
	if repo.limitInput.SpeedLimitBps == nil || *repo.limitInput.SpeedLimitBps != 1048576 {
		t.Fatalf("unexpected speed limit input: %+v", repo.limitInput.SpeedLimitBps)
	}
	if !repo.limitInput.HardLimitEnabled || repo.limitInput.ResetDay != 1 {
		t.Fatalf("unexpected limit flags: %+v", repo.limitInput)
	}
}
