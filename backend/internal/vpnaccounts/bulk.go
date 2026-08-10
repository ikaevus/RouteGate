package vpnaccounts

import (
	"context"
	"errors"
	"sort"
	"strings"

	"github.com/jackc/pgx/v5"
)

const (
	BulkActionActivate     = "activate"
	BulkActionSuspend      = "suspend"
	BulkActionRevoke       = "revoke"
	BulkActionDelete       = "delete"
	BulkActionAssignServer = "assign_server"
)

type BulkAccountSelection struct {
	IDs         []string
	AllMatching bool
	Filter      AccountFilter
}

type BulkAccountActionInput struct {
	Action         string
	Selection      BulkAccountSelection
	TargetServerID string
}

type BulkAccountActionResult struct {
	AffectedCount        int64
	AffectedServerIDs    []string
	ConfigurationChanged bool
}

func (r *Repository) BulkAction(ctx context.Context, input BulkAccountActionInput) (BulkAccountActionResult, error) {
	tx, err := r.pool.Begin(ctx)
	if err != nil {
		return BulkAccountActionResult{}, err
	}
	defer tx.Rollback(ctx)

	whereSQL, args := bulkSelectionSQL(input.Selection)
	serverIDs := make([]string, 0)
	var commandTag commandTagResult

	switch input.Action {
	case BulkActionActivate:
		serverIDs, err = selectBulkStatusServerIDs(ctx, tx, whereSQL, args, StatusActive)
		if err == nil {
			commandTag, err = execBulkStatus(ctx, tx, whereSQL, args, StatusActive)
		}
	case BulkActionSuspend:
		serverIDs, err = selectBulkStatusServerIDs(ctx, tx, whereSQL, args, StatusSuspended)
		if err == nil {
			commandTag, err = execBulkStatus(ctx, tx, whereSQL, args, StatusSuspended)
		}
	case BulkActionRevoke:
		serverIDs, err = selectBulkStatusServerIDs(ctx, tx, whereSQL, args, StatusRevoked)
		if err == nil {
			commandTag, err = execBulkStatus(ctx, tx, whereSQL, args, StatusRevoked)
		}
	case BulkActionDelete:
		serverIDs, err = selectBulkServerIDs(ctx, tx, `
			SELECT DISTINCT server_id::text
			FROM vpn_accounts
			`+whereSQL+`
			  AND server_id IS NOT NULL
		`, args)
		if err == nil {
			commandTag, err = tx.Exec(ctx, `DELETE FROM vpn_accounts `+whereSQL, args...)
		}
	case BulkActionAssignServer:
		if input.TargetServerID == "" {
			return BulkAccountActionResult{}, errors.New("target server is required")
		}
		targetPosition := len(args) + 1
		argsWithTarget := append(append([]any{}, args...), input.TargetServerID)
		serverIDs, err = selectBulkServerIDs(ctx, tx, `
			SELECT DISTINCT server_id::text
			FROM vpn_accounts
			`+whereSQL+`
			  AND server_id IS NOT NULL
			  AND server_id IS DISTINCT FROM $`+itoa(targetPosition)+`::uuid
		`, argsWithTarget)
		if err == nil {
			commandTag, err = tx.Exec(ctx, `
				UPDATE vpn_accounts
				SET server_id = $`+itoa(targetPosition)+`::uuid,
				    updated_at = now(),
				    config_updated_at = now()
				`+whereSQL+`
				  AND server_id IS DISTINCT FROM $`+itoa(targetPosition)+`::uuid`,
				argsWithTarget...,
			)
			if err == nil && commandTag.RowsAffected() > 0 {
				serverIDs = append(serverIDs, input.TargetServerID)
			}
		}
	default:
		return BulkAccountActionResult{}, errors.New("unsupported bulk action")
	}
	if err != nil {
		return BulkAccountActionResult{}, err
	}

	if err := tx.Commit(ctx); err != nil {
		return BulkAccountActionResult{}, err
	}

	affectedServerIDs := uniqueSortedStrings(serverIDs)
	return BulkAccountActionResult{
		AffectedCount:        commandTag.RowsAffected(),
		AffectedServerIDs:    affectedServerIDs,
		ConfigurationChanged: commandTag.RowsAffected() > 0 && len(affectedServerIDs) > 0,
	}, nil
}

type commandTagResult interface {
	RowsAffected() int64
}

func execBulkStatus(ctx context.Context, tx pgx.Tx, whereSQL string, args []any, status string) (commandTagResult, error) {
	statusPosition := len(args) + 1
	argsWithStatus := append(append([]any{}, args...), status)
	return tx.Exec(ctx, `
		UPDATE vpn_accounts
		SET status = $`+itoa(statusPosition)+`,
		    updated_at = now(),
		    config_updated_at = now()
		`+whereSQL+`
		  AND status IS DISTINCT FROM $`+itoa(statusPosition),
		argsWithStatus...,
	)
}

func selectBulkStatusServerIDs(ctx context.Context, tx pgx.Tx, whereSQL string, args []any, status string) ([]string, error) {
	statusPosition := len(args) + 1
	argsWithStatus := append(append([]any{}, args...), status)
	return selectBulkServerIDs(ctx, tx, `
		SELECT DISTINCT server_id::text
		FROM vpn_accounts
		`+whereSQL+`
		  AND server_id IS NOT NULL
		  AND status IS DISTINCT FROM $`+itoa(statusPosition)+`
	`, argsWithStatus)
}

func selectBulkServerIDs(ctx context.Context, tx pgx.Tx, query string, args []any) ([]string, error) {
	rows, err := tx.Query(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	serverIDs := make([]string, 0)
	for rows.Next() {
		var serverID string
		if err := rows.Scan(&serverID); err != nil {
			return nil, err
		}
		serverIDs = append(serverIDs, serverID)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return serverIDs, nil
}

func bulkSelectionSQL(selection BulkAccountSelection) (string, []any) {
	if !selection.AllMatching {
		return "WHERE id = ANY($1::uuid[])", []any{selection.IDs}
	}
	return `WHERE ($1 = '' OR status = $1)
		AND ($2 = '' OR server_id = NULLIF($2, '')::uuid)
		AND (
			$3 = ''
			OR display_name ILIKE '%' || $3 || '%'
			OR email ILIKE '%' || $3 || '%'
			OR EXISTS (
				SELECT 1
				FROM vpn_account_notes n
				WHERE n.vpn_account_id = vpn_accounts.id
				  AND n.notes ILIKE '%' || $3 || '%'
			)
			OR id = $4::uuid
			OR vless_uuid = $4::uuid
		)`, []any{
		selection.Filter.Status,
		selection.Filter.ServerID,
		selection.Filter.Search,
		selection.Filter.SearchUUID,
	}
}

func uniqueSortedStrings(values []string) []string {
	seen := make(map[string]struct{}, len(values))
	for _, value := range values {
		value = strings.TrimSpace(value)
		if value == "" {
			continue
		}
		seen[value] = struct{}{}
	}
	result := make([]string, 0, len(seen))
	for value := range seen {
		result = append(result, value)
	}
	sort.Strings(result)
	return result
}

func itoa(value int) string {
	if value == 0 {
		return "0"
	}
	var digits [20]byte
	position := len(digits)
	for value > 0 {
		position--
		digits[position] = byte('0' + value%10)
		value /= 10
	}
	return string(digits[position:])
}
