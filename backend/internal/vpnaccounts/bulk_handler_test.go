package vpnaccounts

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func (f *fakeAccountRepository) BulkAction(context.Context, BulkAccountActionInput) (BulkAccountActionResult, error) {
	return BulkAccountActionResult{}, nil
}

func TestBulkUpdateRejectsDisableClassAction(t *testing.T) {
	handler := &Handler{accounts: &fakeAccountRepository{}}
	request := httptest.NewRequest(http.MethodPost, "/api/v1/vpn-accounts/bulk-update", strings.NewReader(`{
		"action":"delete",
		"selection":{"ids":["523446e8-0351-4c0a-a9ec-19a269a8848f"]}
	}`))
	response := httptest.NewRecorder()

	handler.BulkUpdate(response, request)

	if response.Code != http.StatusForbidden {
		t.Fatalf("expected 403, got %d: %s", response.Code, response.Body.String())
	}
}

func TestBulkDisableRejectsUpdateClassAction(t *testing.T) {
	handler := &Handler{accounts: &fakeAccountRepository{}}
	request := httptest.NewRequest(http.MethodPost, "/api/v1/vpn-accounts/bulk-disable", strings.NewReader(`{
		"action":"activate",
		"selection":{"ids":["523446e8-0351-4c0a-a9ec-19a269a8848f"]}
	}`))
	response := httptest.NewRecorder()

	handler.BulkDisable(response, request)

	if response.Code != http.StatusForbidden {
		t.Fatalf("expected 403, got %d: %s", response.Code, response.Body.String())
	}
}

func TestValidateBulkSelectionSupportsAllMatchingFilters(t *testing.T) {
	selection, err := validateBulkSelection(BulkAccountSelectionRequest{
		AllMatching: true,
		Search:      "felix",
		Status:      StatusActive,
		ServerID:    "04a9d5db-1dfe-461a-8ddd-fe94a0480a0b",
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !selection.AllMatching || selection.Filter.Search != "felix" || selection.Filter.Status != StatusActive {
		t.Fatalf("unexpected selection: %#v", selection)
	}
}
