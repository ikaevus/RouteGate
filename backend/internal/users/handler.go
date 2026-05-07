package users

import (
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"
	"strings"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/artuazh/routegate/backend/internal/auth"
	"github.com/artuazh/routegate/backend/internal/httpx"
)

type Handler struct {
	logger *slog.Logger
	pool   *pgxpool.Pool
}

func NewHandler(logger *slog.Logger, pool *pgxpool.Pool) *Handler {
	return &Handler{logger: logger, pool: pool}
}

type User struct {
	ID          string   `json:"id"`
	Email       string   `json:"email"`
	Username    string   `json:"username,omitempty"`
	DisplayName string   `json:"display_name,omitempty"`
	UserType    string   `json:"user_type"`
	Status      string   `json:"status"`
	Roles       []string `json:"roles"`
}
type ListResponse struct {
	Items []User `json:"items"`
}
type UpsertRequest struct {
	Email       string   `json:"email"`
	Username    string   `json:"username"`
	Password    string   `json:"password"`
	DisplayName string   `json:"display_name"`
	UserType    string   `json:"user_type"`
	Status      string   `json:"status"`
	Roles       []string `json:"roles"`
}

func (h *Handler) List(w http.ResponseWriter, r *http.Request) {
	rows, err := h.pool.Query(r.Context(), `SELECT id::text, email, COALESCE(username,''), COALESCE(display_name,''), user_type, status FROM users ORDER BY created_at DESC`)
	if err != nil {
		h.dbErr(w, "list users", err)
		return
	}
	defer rows.Close()
	items := []User{}
	for rows.Next() {
		var u User
		if err := rows.Scan(&u.ID, &u.Email, &u.Username, &u.DisplayName, &u.UserType, &u.Status); err != nil {
			h.dbErr(w, "scan users", err)
			return
		}
		u.Roles, _ = h.roles(r.Context(), u.ID)
		items = append(items, u)
	}
	httpx.WriteJSON(w, http.StatusOK, ListResponse{Items: items})
}
func (h *Handler) Get(w http.ResponseWriter, r *http.Request) {
	u, err := h.user(r, r.PathValue("id"))
	if errors.Is(err, pgx.ErrNoRows) {
		http.NotFound(w, r)
		return
	}
	if err != nil {
		h.dbErr(w, "get user", err)
		return
	}
	httpx.WriteJSON(w, http.StatusOK, u)
}
func (h *Handler) Create(w http.ResponseWriter, r *http.Request) {
	var req UpsertRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		bad(w, "Request body must be valid JSON.")
		return
	}
	if strings.TrimSpace(req.Email) == "" || req.Password == "" {
		bad(w, "Email and password are required.")
		return
	}
	if req.UserType == "" {
		req.UserType = "human"
	}
	if req.Status == "" {
		req.Status = "active"
	}
	if len(req.Roles) > 0 && !hasPermission(r, "roles:assign") {
		httpx.WriteJSON(w, http.StatusForbidden, httpx.Error("forbidden", "Assigning roles requires roles:assign."))
		return
	}
	hash, err := auth.HashPassword(req.Password)
	if err != nil {
		h.dbErr(w, "hash password", err)
		return
	}
	tx, err := h.pool.Begin(r.Context())
	if err != nil {
		h.dbErr(w, "begin", err)
		return
	}
	defer tx.Rollback(r.Context())
	var id string
	err = tx.QueryRow(r.Context(), `INSERT INTO users (email, username, password_hash, display_name, user_type, status) VALUES ($1,NULLIF($2,''),$3,NULLIF($4,''),$5,$6) RETURNING id::text`, req.Email, req.Username, string(hash), req.DisplayName, req.UserType, req.Status).Scan(&id)
	if err != nil {
		h.dbErr(w, "create user", err)
		return
	}
	if err := assignRoles(r.Context(), tx, id, req.Roles); err != nil {
		bad(w, err.Error())
		return
	}
	if err := tx.Commit(r.Context()); err != nil {
		h.dbErr(w, "commit", err)
		return
	}
	u, _ := h.user(r, id)
	httpx.WriteJSON(w, http.StatusCreated, u)
}
func (h *Handler) Update(w http.ResponseWriter, r *http.Request) {
	var req UpsertRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		bad(w, "Request body must be valid JSON.")
		return
	}
	id := r.PathValue("id")
	if len(req.Roles) > 0 && !hasPermission(r, "roles:assign") {
		httpx.WriteJSON(w, http.StatusForbidden, httpx.Error("forbidden", "Assigning roles requires roles:assign."))
		return
	}
	tx, err := h.pool.Begin(r.Context())
	if err != nil {
		h.dbErr(w, "begin", err)
		return
	}
	defer tx.Rollback(r.Context())
	if req.Status == "disabled" {
		ok, err := h.isLastActiveSuperAdmin(r.Context(), id)
		if err != nil {
			h.dbErr(w, "check superadmin", err)
			return
		}
		if ok {
			httpx.WriteJSON(w, http.StatusBadRequest, httpx.Error("last_super_admin", "Cannot disable the last active SuperAdmin."))
			return
		}
	}
	if len(req.Roles) > 0 {
		if err := h.ensureNotRemovingLastSuperAdmin(r.Context(), tx, id, req.Roles); err != nil {
			httpx.WriteJSON(w, http.StatusBadRequest, httpx.Error("last_super_admin", err.Error()))
			return
		}
	}
	_, err = tx.Exec(r.Context(), `UPDATE users SET email=COALESCE(NULLIF($2,''), email), username=COALESCE(NULLIF($3,''), username), display_name=COALESCE(NULLIF($4,''), display_name), user_type=COALESCE(NULLIF($5,''), user_type), status=COALESCE(NULLIF($6,''), status), updated_at=now() WHERE id=$1`, id, req.Email, req.Username, req.DisplayName, req.UserType, req.Status)
	if err != nil {
		h.dbErr(w, "update user", err)
		return
	}
	if req.Password != "" {
		hash, err := auth.HashPassword(req.Password)
		if err != nil {
			h.dbErr(w, "hash password", err)
			return
		}
		if _, err := tx.Exec(r.Context(), `UPDATE users SET password_hash=$2, updated_at=now() WHERE id=$1`, id, string(hash)); err != nil {
			h.dbErr(w, "update password", err)
			return
		}
	}
	if len(req.Roles) > 0 {
		if _, err := tx.Exec(r.Context(), `DELETE FROM user_roles WHERE user_id=$1`, id); err != nil {
			h.dbErr(w, "delete roles", err)
			return
		}
		if err := assignRoles(r.Context(), tx, id, req.Roles); err != nil {
			bad(w, err.Error())
			return
		}
	}
	if err := tx.Commit(r.Context()); err != nil {
		h.dbErr(w, "commit", err)
		return
	}
	u, _ := h.user(r, id)
	httpx.WriteJSON(w, http.StatusOK, u)
}
func (h *Handler) Disable(w http.ResponseWriter, r *http.Request) { h.setStatus(w, r, "disabled") }
func (h *Handler) Enable(w http.ResponseWriter, r *http.Request)  { h.setStatus(w, r, "active") }
func (h *Handler) setStatus(w http.ResponseWriter, r *http.Request, status string) {
	id := r.PathValue("id")
	if status == "disabled" {
		ok, err := h.isLastActiveSuperAdmin(r.Context(), id)
		if err != nil {
			h.dbErr(w, "check superadmin", err)
			return
		}
		if ok {
			httpx.WriteJSON(w, http.StatusBadRequest, httpx.Error("last_super_admin", "Cannot disable the last active SuperAdmin."))
			return
		}
	}
	_, err := h.pool.Exec(r.Context(), `UPDATE users SET status=$2, updated_at=now() WHERE id=$1`, id, status)
	if err != nil {
		h.dbErr(w, "set status", err)
		return
	}
	u, _ := h.user(r, id)
	httpx.WriteJSON(w, http.StatusOK, u)
}

func (h *Handler) user(r *http.Request, id string) (User, error) {
	var u User
	err := h.pool.QueryRow(r.Context(), `SELECT id::text,email,COALESCE(username,''),COALESCE(display_name,''),user_type,status FROM users WHERE id=$1`, id).Scan(&u.ID, &u.Email, &u.Username, &u.DisplayName, &u.UserType, &u.Status)
	if err != nil {
		return u, err
	}
	u.Roles, _ = h.roles(r.Context(), id)
	return u, nil
}
func (h *Handler) roles(ctx context.Context, id string) ([]string, error) {
	rows, err := h.pool.Query(ctx, `SELECT r.code FROM roles r JOIN user_roles ur ON ur.role_id=r.id WHERE ur.user_id=$1 ORDER BY r.code`, id)
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

func assignRoles(ctx context.Context, tx pgx.Tx, userID string, roles []string) error {
	for _, role := range roles {
		role = strings.TrimSpace(role)
		if role == "" {
			continue
		}
		result, err := tx.Exec(ctx, `INSERT INTO user_roles (user_id, role_id) SELECT $1::uuid, id FROM roles WHERE code=$2`, userID, role)
		if err != nil {
			return err
		}
		if result.RowsAffected() == 0 {
			return errors.New("unknown role: " + role)
		}
	}
	return nil
}

func (h *Handler) isLastActiveSuperAdmin(ctx context.Context, userID string) (bool, error) {
	var isSuper bool
	if err := h.pool.QueryRow(ctx, `SELECT EXISTS (SELECT 1 FROM user_roles ur JOIN roles r ON r.id=ur.role_id WHERE ur.user_id=$1 AND r.code='super_admin')`, userID).Scan(&isSuper); err != nil {
		return false, err
	}
	if !isSuper {
		return false, nil
	}
	var count int
	if err := h.pool.QueryRow(ctx, `SELECT count(DISTINCT u.id) FROM users u JOIN user_roles ur ON ur.user_id=u.id JOIN roles r ON r.id=ur.role_id WHERE r.code='super_admin' AND u.status='active'`).Scan(&count); err != nil {
		return false, err
	}
	return count <= 1, nil
}

func (h *Handler) ensureNotRemovingLastSuperAdmin(ctx context.Context, tx pgx.Tx, userID string, newRoles []string) error {
	keepsSuper := false
	for _, role := range newRoles {
		if role == "super_admin" {
			keepsSuper = true
		}
	}
	if keepsSuper {
		return nil
	}
	var isSuper bool
	if err := tx.QueryRow(ctx, `SELECT EXISTS (SELECT 1 FROM user_roles ur JOIN roles r ON r.id=ur.role_id WHERE ur.user_id=$1 AND r.code='super_admin')`, userID).Scan(&isSuper); err != nil {
		return err
	}
	if !isSuper {
		return nil
	}
	var count int
	if err := tx.QueryRow(ctx, `SELECT count(DISTINCT u.id) FROM users u JOIN user_roles ur ON ur.user_id=u.id JOIN roles r ON r.id=ur.role_id WHERE r.code='super_admin'`).Scan(&count); err != nil {
		return err
	}
	if count <= 1 {
		return errors.New("Cannot remove the last SuperAdmin role from the last SuperAdmin account.")
	}
	return nil
}

func hasPermission(r *http.Request, permission string) bool {
	user, ok := auth.UserFromContext(r.Context())
	if !ok {
		return false
	}
	for _, p := range user.Permissions {
		if p == permission {
			return true
		}
	}
	return false
}
func bad(w http.ResponseWriter, message string) {
	httpx.WriteJSON(w, http.StatusBadRequest, httpx.Error("invalid_request", message))
}
func (h *Handler) dbErr(w http.ResponseWriter, action string, err error) {
	h.logger.Error(action+" failed", "error", err)
	httpx.WriteJSON(w, http.StatusInternalServerError, httpx.Error("database_error", "Database operation failed."))
}
