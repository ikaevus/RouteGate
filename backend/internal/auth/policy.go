package auth

import (
	"net/http"

	"github.com/ikaevus/routegate/backend/internal/httpx"
)

const (
	RoleSuperAdmin = "super_admin"
	RoleAdmin      = "admin"
	RoleOperator   = "operator"
	RoleReadOnly   = "read_only"
	RoleVPNUser    = "vpn_user"
	RoleAgent      = "agent"

	UserTypeHuman = "human"
)

var AdminRoles = map[string]struct{}{
	RoleSuperAdmin: {},
	RoleAdmin:      {},
	RoleOperator:   {},
	RoleReadOnly:   {},
}

func HasRole(user UserProfile, role string) bool {
	for _, candidate := range user.Roles {
		if candidate == role {
			return true
		}
	}
	return false
}

func HasPermission(user UserProfile, permission string) bool {
	for _, candidate := range user.Permissions {
		if candidate == permission {
			return true
		}
	}
	return false
}

func IsAdminUser(user UserProfile) bool {
	if user.UserType != UserTypeHuman || user.Status != "active" {
		return false
	}
	for _, role := range user.Roles {
		if _, ok := AdminRoles[role]; ok {
			return true
		}
	}
	return false
}

func RequireAdminSession(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		user, ok := UserFromContext(r.Context())
		if !ok {
			httpx.WriteJSON(w, http.StatusUnauthorized, httpx.Error("unauthorized", "Authentication is required."))
			return
		}
		if !IsAdminUser(user.UserProfile) {
			httpx.WriteJSON(w, http.StatusForbidden, httpx.Error("forbidden", "Admin session is required."))
			return
		}
		next.ServeHTTP(w, r)
	})
}
