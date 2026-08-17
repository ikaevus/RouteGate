package portal

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

type fakePortalTrafficRepository struct {
	usage TrafficUsageSummary
	err   error
	email string
}

func (f *fakePortalTrafficRepository) GetTrafficUsageForUser(_ context.Context, email string) (TrafficUsageSummary, error) {
	f.email = email
	return f.usage, f.err
}

func TestDashboardIncludesOwnerScopedTrafficUsage(t *testing.T) {
	periodStart := time.Date(2026, time.August, 1, 0, 0, 0, 0, time.UTC)
	lastObservedAt := time.Date(2026, time.August, 17, 18, 0, 0, 0, time.UTC)
	repo := &fakePortalRepository{
		profiles: []PortalProfile{{
			ID:           "account-1",
			DisplayName:  "Alice VPN",
			Status:       "active",
			AccessStatus: AccessStatusActive,
			Protocol:     "vless",
			UpdatedAt:    time.Now().UTC(),
		}},
	}
	traffic := &fakePortalTrafficRepository{usage: TrafficUsageSummary{
		Enabled:        true,
		RXBytes:        1024,
		TXBytes:        2048,
		TotalBytes:     3072,
		PeriodStart:    periodStart,
		PeriodEnd:      periodStart.AddDate(0, 1, 0),
		LastObservedAt: &lastObservedAt,
	}}
	handler := newTestHandler(repo)
	handler.traffic = traffic

	request := httptest.NewRequest(http.MethodGet, "/api/portal/dashboard", nil)
	request = withTestUser(request, "alice@example.com")
	response := httptest.NewRecorder()

	handler.Dashboard(response, request)

	if response.Code != http.StatusOK {
		t.Fatalf("expected status %d, got %d", http.StatusOK, response.Code)
	}
	if traffic.email != "alice@example.com" {
		t.Fatalf("expected owner email alice@example.com, got %q", traffic.email)
	}

	var body DashboardResponse
	if err := json.NewDecoder(response.Body).Decode(&body); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if body.Dashboard.TrafficUsage == nil {
		t.Fatal("expected traffic usage summary")
	}
	if body.Dashboard.TrafficUsage.TotalBytes != 3072 {
		t.Fatalf("expected 3072 total bytes, got %d", body.Dashboard.TrafficUsage.TotalBytes)
	}
	if body.Dashboard.TrafficUsage.RXBytes != 1024 || body.Dashboard.TrafficUsage.TXBytes != 2048 {
		t.Fatalf("unexpected traffic split: rx=%d tx=%d", body.Dashboard.TrafficUsage.RXBytes, body.Dashboard.TrafficUsage.TXBytes)
	}
}
