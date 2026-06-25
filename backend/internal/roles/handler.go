package roles

import (
	"log/slog"
	"net/http"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/ikaevus/routegate/backend/internal/httpx"
)

type Handler struct {
	logger *slog.Logger
	pool   *pgxpool.Pool
}

func NewHandler(logger *slog.Logger, pool *pgxpool.Pool) *Handler {
	return &Handler{logger: logger, pool: pool}
}

type Role struct {
	ID          string   `json:"id"`
	Code        string   `json:"code"`
	Name        string   `json:"name"`
	Description string   `json:"description,omitempty"`
	BuiltIn     bool     `json:"built_in"`
	Permissions []string `json:"permissions"`
}
type Permission struct {
	ID          string `json:"id"`
	Code        string `json:"code"`
	Name        string `json:"name"`
	Description string `json:"description,omitempty"`
}

type RolesResponse struct {
	Items []Role `json:"items"`
}
type PermissionsResponse struct {
	Items []Permission `json:"items"`
}

func (h *Handler) ListRoles(w http.ResponseWriter, r *http.Request) {
	rows, err := h.pool.Query(r.Context(), `SELECT id::text, code, name, COALESCE(description,''), built_in FROM roles ORDER BY code`)
	if err != nil {
		h.dbErr(w, err)
		return
	}
	defer rows.Close()
	items := []Role{}
	for rows.Next() {
		var item Role
		if err := rows.Scan(&item.ID, &item.Code, &item.Name, &item.Description, &item.BuiltIn); err != nil {
			h.dbErr(w, err)
			return
		}
		item.Permissions, _ = h.permissionsForRole(r, item.ID)
		items = append(items, item)
	}
	httpx.WriteJSON(w, http.StatusOK, RolesResponse{Items: items})
}
func (h *Handler) ListPermissions(w http.ResponseWriter, r *http.Request) {
	rows, err := h.pool.Query(r.Context(), `SELECT id::text, code, name, COALESCE(description,'') FROM permissions ORDER BY code`)
	if err != nil {
		h.dbErr(w, err)
		return
	}
	defer rows.Close()
	items := []Permission{}
	for rows.Next() {
		var item Permission
		if err := rows.Scan(&item.ID, &item.Code, &item.Name, &item.Description); err != nil {
			h.dbErr(w, err)
			return
		}
		items = append(items, item)
	}
	httpx.WriteJSON(w, http.StatusOK, PermissionsResponse{Items: items})
}
func (h *Handler) permissionsForRole(r *http.Request, id string) ([]string, error) {
	rows, err := h.pool.Query(r.Context(), `SELECT p.code FROM permissions p JOIN role_permissions rp ON rp.permission_id=p.id WHERE rp.role_id=$1 ORDER BY p.code`, id)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []string{}
	for rows.Next() {
		var c string
		if err := rows.Scan(&c); err != nil {
			return nil, err
		}
		out = append(out, c)
	}
	return out, rows.Err()
}
func (h *Handler) dbErr(w http.ResponseWriter, err error) {
	h.logger.Error("roles request failed", "error", err)
	httpx.WriteJSON(w, http.StatusInternalServerError, httpx.Error("database_error", "Database operation failed."))
}
