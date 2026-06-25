package traffic

import (
	"bytes"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestReportUsageRejectsNegativeUsageCounters(t *testing.T) {
	repo := &fakeTrafficRepository{}
	handler := newTestHandler(repo)
	request := httptest.NewRequest(http.MethodPost, "/api/v1/agent/traffic-usage", strings.NewReader(`{
		"events": [
			{"vpnAccountId":"account-1","rxBytes":-1,"txBytes":2048}
		]
	}`))
	request.Header.Set("Authorization", "Bearer raw-agent-token")
	response := httptest.NewRecorder()

	handler.ReportUsage(response, request)

	if response.Code != http.StatusBadRequest {
		t.Fatalf("expected status %d, got %d", http.StatusBadRequest, response.Code)
	}
	if len(repo.reportEvents) != 0 {
		t.Fatalf("expected invalid report not to reach repository, got %d events", len(repo.reportEvents))
	}
}

func TestReportUsageRejectsTooManyEvents(t *testing.T) {
	handler := newTestHandler(&fakeTrafficRepository{})
	var body bytes.Buffer
	body.WriteString(`{"events":[`)
	for i := 0; i < MaxUsageReportEvents+1; i++ {
		if i > 0 {
			body.WriteByte(',')
		}
		body.WriteString(`{"vpnAccountId":"account-1","rxBytes":1,"txBytes":1}`)
	}
	body.WriteString(`]}`)
	request := httptest.NewRequest(http.MethodPost, "/api/v1/agent/traffic-usage", &body)
	request.Header.Set("Authorization", "Bearer raw-agent-token")
	response := httptest.NewRecorder()

	handler.ReportUsage(response, request)

	if response.Code != http.StatusBadRequest {
		t.Fatalf("expected status %d, got %d", http.StatusBadRequest, response.Code)
	}
}

func TestReportUsageReturnsUnauthorizedForUnknownAgent(t *testing.T) {
	handler := newTestHandler(&fakeTrafficRepository{reportErr: ErrUnauthorizedAgent})
	request := httptest.NewRequest(http.MethodPost, "/api/v1/agent/traffic-usage", strings.NewReader(`{
		"events": [
			{"vpnAccountId":"account-1","rxBytes":1024,"txBytes":2048}
		]
	}`))
	request.Header.Set("Authorization", "Bearer raw-agent-token")
	response := httptest.NewRecorder()

	handler.ReportUsage(response, request)

	if response.Code != http.StatusUnauthorized {
		t.Fatalf("expected status %d, got %d", http.StatusUnauthorized, response.Code)
	}
}
