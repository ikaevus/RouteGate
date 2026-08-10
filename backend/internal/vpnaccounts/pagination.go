package vpnaccounts

import (
	"errors"
	"net/http"
	"regexp"
	"strconv"
	"strings"
)

const (
	defaultAccountPageSize = 100
	maxAccountPageSize     = 100
	nilSearchUUID          = "00000000-0000-0000-0000-000000000000"
)

var uuidSearchPattern = regexp.MustCompile(`(?i)^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$`)

func parseAccountListFilter(r *http.Request) (AccountFilter, int, int, error) {
	query := r.URL.Query()
	page, err := positiveQueryInt(query.Get("page"), 1)
	if err != nil {
		return AccountFilter{}, 0, 0, errors.New("page must be a positive integer")
	}
	pageSize, err := positiveQueryInt(query.Get("pageSize"), defaultAccountPageSize)
	if err != nil || pageSize > maxAccountPageSize {
		return AccountFilter{}, 0, 0, errors.New("pageSize must be an integer between 1 and 100")
	}

	status := strings.TrimSpace(query.Get("status"))
	if status != "" && !ValidStatus(status) {
		return AccountFilter{}, 0, 0, errors.New("status must be one of: created, active, suspended, expired, revoked")
	}

	serverID := strings.TrimSpace(query.Get("serverId"))
	if serverID != "" && !uuidSearchPattern.MatchString(serverID) {
		return AccountFilter{}, 0, 0, errors.New("serverId must be a UUID")
	}

	search := strings.TrimSpace(query.Get("search"))
	searchUUID := nilSearchUUID
	if uuidSearchPattern.MatchString(search) {
		searchUUID = search
	}

	return AccountFilter{
		Status:     status,
		ServerID:   serverID,
		Search:     search,
		SearchUUID: searchUUID,
		Limit:      pageSize,
		Offset:     (page - 1) * pageSize,
	}, page, pageSize, nil
}

func positiveQueryInt(raw string, fallback int) (int, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return fallback, nil
	}
	value, err := strconv.Atoi(raw)
	if err != nil || value < 1 {
		return 0, errors.New("value must be a positive integer")
	}
	return value, nil
}

func totalPages(total, pageSize int) int {
	if total <= 0 || pageSize <= 0 {
		return 0
	}
	return (total + pageSize - 1) / pageSize
}
