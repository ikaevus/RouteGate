package auth

import (
	"context"
	"net/http"
	"strings"

	"github.com/ikaevus/routegate/backend/internal/httpx"
)

type contextKey string

const userContextKey contextKey = "auth_user"

func Middleware(repo *Repository) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			token := BearerToken(r)
			if token == "" {
				httpx.WriteJSON(w, http.StatusUnauthorized, httpx.Error("unauthorized", "Authentication is required."))
				return
			}
			user, err := repo.UserByToken(r.Context(), token)
			if err != nil {
				httpx.WriteJSON(w, http.StatusUnauthorized, httpx.Error("unauthorized", "Authentication is required."))
				return
			}
			next.ServeHTTP(w, r.WithContext(ContextWithUser(r.Context(), user)))
		})
	}
}

func RequirePermission(permission string) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			user, ok := UserFromContext(r.Context())
			if !ok {
				httpx.WriteJSON(w, http.StatusUnauthorized, httpx.Error("unauthorized", "Authentication is required."))
				return
			}
			for _, p := range user.Permissions {
				if p == permission {
					next.ServeHTTP(w, r)
					return
				}
			}
			httpx.WriteJSON(w, http.StatusForbidden, httpx.Error("forbidden", "Required permission is missing."))
		})
	}
}

func ContextWithUser(ctx context.Context, user AuthenticatedUser) context.Context {
	return context.WithValue(ctx, userContextKey, user)
}

func UserFromContext(ctx context.Context) (AuthenticatedUser, bool) {
	u, ok := ctx.Value(userContextKey).(AuthenticatedUser)
	return u, ok
}

func BearerToken(r *http.Request) string {
	h := strings.TrimSpace(r.Header.Get("Authorization"))
	if !strings.HasPrefix(strings.ToLower(h), "bearer ") {
		return ""
	}
	return strings.TrimSpace(h[7:])
}
