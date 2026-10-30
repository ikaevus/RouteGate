package traffic

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestSequencedReportsRequireValidStreamAndStableObservationTime(t *testing.T) {
	for _, body := range []string{
		`{"streamId":"bad","sequence":1,"events":[{"vpnAccountId":"a","rxBytes":1}]}`,
		`{"streamId":"11111111-1111-4111-8111-111111111111","sequence":0,"events":[{"vpnAccountId":"a","rxBytes":1}]}`,
		`{"streamId":"11111111-1111-4111-8111-111111111111","sequence":1,"events":[{"vpnAccountId":"a","rxBytes":1}]}`,
	} {
		repo := &fakeTrafficRepository{}
		handler := newTestHandler(repo)
		req := httptest.NewRequest(http.MethodPost, "/api/v1/agent/traffic-usage", strings.NewReader(body))
		req.Header.Set("Authorization", "Bearer test")
		response := httptest.NewRecorder()
		handler.ReportUsage(response, req)
		if response.Code != 400 || len(repo.reportEvents) != 0 {
			t.Fatalf("invalid sequenced report: %d %s", response.Code, response.Body.String())
		}
	}
}
