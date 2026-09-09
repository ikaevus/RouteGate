package routingprofiles

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"log/slog"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
)

type ManagedRuleSet struct {
	ID               string          `json:"id"`
	RoutingProfileID string          `json:"routingProfileId"`
	Name             string          `json:"name"`
	Provider         string          `json:"provider"`
	SourceURL        string          `json:"sourceUrl"`
	Priority         int             `json:"priority"`
	Action           string          `json:"action"`
	Enabled          bool            `json:"enabled"`
	RefreshHours     int             `json:"refreshHours"`
	Snapshot         *SourceDocument `json:"-"`
	SnapshotSHA256   string          `json:"snapshotSha256"`
	LastAttemptAt    *time.Time      `json:"lastAttemptAt"`
	LastSuccessAt    *time.Time      `json:"lastSuccessAt"`
	LastError        string          `json:"lastError"`
	CreatedAt        time.Time       `json:"createdAt"`
	UpdatedAt        time.Time       `json:"updatedAt"`
}
type ManagedSetInput struct {
	Name         string `json:"name"`
	Provider     string `json:"provider"`
	SourceURL    string `json:"sourceUrl"`
	Priority     int    `json:"priority"`
	Action       string `json:"action"`
	Enabled      bool   `json:"enabled"`
	RefreshHours int    `json:"refreshHours"`
}

func (input *ManagedSetInput) validate() error {
	input.Name = strings.TrimSpace(input.Name)
	input.SourceURL = strings.TrimSpace(input.SourceURL)
	if err := validateRuleName(input.Name); err != nil {
		return err
	}
	if err := validateRulePriority(input.Priority); err != nil {
		return err
	}
	if !ValidAction(input.Action) {
		return errors.New("invalid action")
	}
	if input.RefreshHours < 1 || input.RefreshHours > 168 {
		return errors.New("refreshHours must be between 1 and 168")
	}
	known := false
	for _, p := range SourceProviders {
		if input.Provider == p.ID {
			known = true
			if input.SourceURL == "" {
				input.SourceURL = p.URL
			}
		}
	}
	if !known {
		return errors.New("unsupported provider")
	}
	return validateSourceURL(input.SourceURL)
}

var ErrManagedSetLimit = errors.New("a profile supports at most 16 managed rule sets")

const managedColumns = `id::text, routing_profile_id::text, name, provider, source_url, priority, action, enabled, refresh_hours,
 snapshot, snapshot_sha256, last_attempt_at, last_success_at, last_error, created_at, updated_at`

func scanManaged(row scanner) (ManagedRuleSet, error) {
	var s ManagedRuleSet
	var raw []byte
	err := row.Scan(&s.ID, &s.RoutingProfileID, &s.Name, &s.Provider, &s.SourceURL, &s.Priority, &s.Action, &s.Enabled, &s.RefreshHours, &raw, &s.SnapshotSHA256, &s.LastAttemptAt, &s.LastSuccessAt, &s.LastError, &s.CreatedAt, &s.UpdatedAt)
	if err != nil {
		return s, err
	}
	if len(raw) > 0 {
		var doc SourceDocument
		if err := json.Unmarshal(raw, &doc); err != nil {
			return s, err
		}
		s.Snapshot = &doc
	}
	return s, nil
}
func (r *Repository) ListManagedSets(ctx context.Context, profileID string) ([]ManagedRuleSet, error) {
	rows, err := r.pool.Query(ctx, `SELECT `+managedColumns+` FROM routing_profile_managed_sets WHERE routing_profile_id=$1::uuid ORDER BY priority,created_at,id`, profileID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	items := make([]ManagedRuleSet, 0)
	for rows.Next() {
		s, err := scanManaged(rows)
		if err != nil {
			return nil, err
		}
		items = append(items, s)
	}
	return items, rows.Err()
}
func (r *Repository) CreateManagedSet(ctx context.Context, profileID string, input ManagedSetInput) (ManagedRuleSet, error) {
	// Serialize count and insert per profile to bound storage and generated config size.
	tx, err := r.pool.Begin(ctx)
	if err != nil {
		return ManagedRuleSet{}, err
	}
	defer tx.Rollback(ctx)
	var id string
	if err = tx.QueryRow(ctx, `SELECT id::text FROM routing_profiles WHERE id=$1::uuid FOR UPDATE`, profileID).Scan(&id); err != nil {
		return ManagedRuleSet{}, err
	}
	var count int
	if err = tx.QueryRow(ctx, `SELECT count(*) FROM routing_profile_managed_sets WHERE routing_profile_id=$1::uuid`, profileID).Scan(&count); err != nil {
		return ManagedRuleSet{}, err
	}
	if count >= 16 {
		return ManagedRuleSet{}, ErrManagedSetLimit
	}
	s, err := scanManaged(tx.QueryRow(ctx, `INSERT INTO routing_profile_managed_sets(routing_profile_id,name,provider,source_url,priority,action,enabled,refresh_hours)
 VALUES($1::uuid,$2,$3,$4,$5,$6,$7,$8) RETURNING `+managedColumns, profileID, input.Name, input.Provider, input.SourceURL, input.Priority, input.Action, input.Enabled, input.RefreshHours))
	if err != nil {
		return s, err
	}
	return s, tx.Commit(ctx)
}
func (r *Repository) UpdateManagedSet(ctx context.Context, profileID, id string, input ManagedSetInput) (ManagedRuleSet, error) {
	// Provider and URL are immutable: replacing a source requires a new set, so a
	// failed replacement can never mislabel an old snapshot as data from a new URL.
	return scanManaged(r.pool.QueryRow(ctx, `UPDATE routing_profile_managed_sets SET name=$3,priority=$4,action=$5,enabled=$6,refresh_hours=$7,updated_at=now()
 WHERE routing_profile_id=$1::uuid AND id=$2::uuid AND provider=$8 AND source_url=$9 RETURNING `+managedColumns, profileID, id, input.Name, input.Priority, input.Action, input.Enabled, input.RefreshHours, input.Provider, input.SourceURL))
}
func (r *Repository) DeleteManagedSet(ctx context.Context, profileID, id string) error {
	result, err := r.pool.Exec(ctx, `DELETE FROM routing_profile_managed_sets WHERE routing_profile_id=$1::uuid AND id=$2::uuid`, profileID, id)
	if err == nil && result.RowsAffected() == 0 {
		return pgx.ErrNoRows
	}
	return err
}
func (r *Repository) RefreshManagedSet(ctx context.Context, profileID, id string, fetcher SourceFetcher) (ManagedRuleSet, error) {
	ctx, cancel := context.WithTimeout(ctx, 40*time.Second)
	defer cancel()
	tx, err := r.pool.Begin(ctx)
	if err != nil {
		return ManagedRuleSet{}, err
	}
	defer tx.Rollback(ctx)
	// A row lock protects concurrent refresh/update/delete and also works across Managers.
	s, err := scanManaged(tx.QueryRow(ctx, `SELECT `+managedColumns+` FROM routing_profile_managed_sets WHERE routing_profile_id=$1::uuid AND id=$2::uuid FOR UPDATE SKIP LOCKED`, profileID, id))
	if err != nil {
		return s, err
	}
	doc, fetchErr := fetcher.Fetch(ctx, s.SourceURL, s.Provider)
	// Validate at the persistence boundary as well, including future provider adapters.
	if fetchErr == nil {
		raw, e := json.Marshal(doc)
		if e != nil {
			fetchErr = e
		} else {
			doc, fetchErr = ParseSource(raw)
		}
	}
	if fetchErr != nil {
		s, err = scanManaged(tx.QueryRow(ctx, `UPDATE routing_profile_managed_sets SET last_attempt_at=now(),last_error=$3,updated_at=now() WHERE routing_profile_id=$1::uuid AND id=$2::uuid RETURNING `+managedColumns, profileID, id, fetchErr.Error()))
	} else {
		raw, e := json.Marshal(doc)
		if e != nil {
			return s, e
		}
		hash := sha256.Sum256(raw)
		s, err = scanManaged(tx.QueryRow(ctx, `UPDATE routing_profile_managed_sets SET snapshot=$3::jsonb,snapshot_sha256=$4,last_attempt_at=now(),last_success_at=now(),last_error='',updated_at=now() WHERE routing_profile_id=$1::uuid AND id=$2::uuid RETURNING `+managedColumns, profileID, id, raw, hex.EncodeToString(hash[:])))
	}
	if err != nil {
		return s, err
	}
	return s, tx.Commit(ctx)
}
func (r *Repository) RunManagedRefresh(ctx context.Context, logger *slog.Logger) error {
	fetcher := NewSourceFetcher()
	ticker := time.NewTicker(time.Minute)
	defer ticker.Stop()
	for {
		if ctx.Err() != nil {
			return nil
		}
		rows, err := r.pool.Query(ctx, `SELECT routing_profile_id::text,id::text FROM routing_profile_managed_sets WHERE enabled=true AND
 (last_attempt_at IS NULL OR last_attempt_at < now() - make_interval(hours=>refresh_hours)) ORDER BY last_attempt_at NULLS FIRST LIMIT 32`)
		if err == nil {
			var ids [][2]string
			for rows.Next() {
				var id [2]string
				if err = rows.Scan(&id[0], &id[1]); err != nil {
					break
				}
				ids = append(ids, id)
			}
			if err == nil {
				err = rows.Err()
			}
			rows.Close()
			if err == nil {
				for _, id := range ids {
					if _, e := r.RefreshManagedSet(ctx, id[0], id[1], fetcher); e != nil && !errors.Is(e, pgx.ErrNoRows) && ctx.Err() == nil {
						logger.Warn("managed routing refresh failed", "id", id[1], "error", e)
					}
				}
			}
		}
		if err != nil && ctx.Err() == nil {
			logger.Warn("managed routing refresh scan failed", "error", err)
		}
		select {
		case <-ctx.Done():
			return nil
		case <-ticker.C:
		}
	}
}
