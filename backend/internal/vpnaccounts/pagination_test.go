package vpnaccounts

import (
	"context"
	"net/http/httptest"
	"testing"
)

func (f *fakeAccountRepository) CountAccounts(context.Context, AccountFilter) (int, error) {
	return 0, nil
}

func (r *publicSubscriptionSplitTunnelE2ERepository) CountAccounts(context.Context, AccountFilter) (int, error) {
	return 0, nil
}

func (r *vpnClientE2ERepository) CountAccounts(context.Context, AccountFilter) (int, error) {
	return 0, nil
}

func TestParseAccountListFilterDefaults(t *testing.T) {
	request := httptest.NewRequest("GET", "/api/v1/vpn-accounts", nil)
	filter, page, pageSize, err := parseAccountListFilter(request)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if page != 1 || pageSize != 100 {
		t.Fatalf("expected page=1 pageSize=100, got page=%d pageSize=%d", page, pageSize)
	}
	if filter.Limit != 100 || filter.Offset != 0 {
		t.Fatalf("expected limit=100 offset=0, got limit=%d offset=%d", filter.Limit, filter.Offset)
	}
	if filter.SearchUUID != nilSearchUUID {
		t.Fatalf("expected safe nil UUID sentinel, got %q", filter.SearchUUID)
	}
}

func TestParseAccountListFilterUsesPageOffsetAndUUIDSearch(t *testing.T) {
	request := httptest.NewRequest(
		"GET",
		"/api/v1/vpn-accounts?page=3&pageSize=25&status=active&search=523446e8-0351-4c0a-a9ec-19a269a8848f",
		nil,
	)
	filter, page, pageSize, err := parseAccountListFilter(request)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if page != 3 || pageSize != 25 || filter.Limit != 25 || filter.Offset != 50 {
		t.Fatalf("unexpected pagination: page=%d pageSize=%d limit=%d offset=%d", page, pageSize, filter.Limit, filter.Offset)
	}
	if filter.SearchUUID != "523446e8-0351-4c0a-a9ec-19a269a8848f" {
		t.Fatalf("expected exact UUID search path, got %q", filter.SearchUUID)
	}
}

func TestParseAccountListFilterRejectsOversizedPage(t *testing.T) {
	request := httptest.NewRequest("GET", "/api/v1/vpn-accounts?pageSize=101", nil)
	if _, _, _, err := parseAccountListFilter(request); err == nil {
		t.Fatal("expected invalid pageSize error")
	}
}

func TestTotalPages(t *testing.T) {
	if got := totalPages(1247, 50); got != 25 {
		t.Fatalf("expected 25 total pages, got %d", got)
	}
}
