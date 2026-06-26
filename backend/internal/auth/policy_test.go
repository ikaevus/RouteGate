package auth

import (
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestHasRoleAndPermission(t *testing.T) {
	user := UserProfile{
		Roles:       []string{RoleAdmin, RoleOperator},
		Permissions: []string{"servers:read", "configs:apply"},
	}

	if !HasRole(user, RoleAdmin) {
		t.Fatal("expected user to have admin role")
	}
	if HasRole(user, RoleAgent) {
		t.Fatal("did not expect user to have agent role")
	}
	if !HasPermission(user, "configs:apply") {
		t.Fatal("expected user to have configs:apply permission")
	}
	if HasPermission(user, "system:manage") {
		t.Fatal("did not expect user to have system:manage permission")
	}
}

func TestIsAdminUser(t *testing.T) {
	cases := []struct {
		name string
		user UserProfile
		want bool
	}{
		{
			name: "super admin human",
			user: UserProfile{UserType: UserTypeHuman, Status: "active", Roles: []string{RoleSuperAdmin}},
			want: true,
		},
		{
			name: "read only human",
			user: UserProfile{UserType: UserTypeHuman, Status: "active", Roles: []string{RoleReadOnly}},
			want: true,
		},
		{
			name: "portal user",
			user: UserProfile{UserType: UserTypeHuman, Status: "active", Roles: []string{RoleVPNUser}},
			want: false,
		},
		{
			name: "agent user type",
			user: UserProfile{UserType: "agent", Status: "active", Roles: []string{RoleAgent}},
			want: false,
		},
		{
			name: "disabled admin",
			user: UserProfile{UserType: UserTypeHuman, Status: "disabled", Roles: []string{RoleAdmin}},
			want: false,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := IsAdminUser(tc.user); got != tc.want {
				t.Fatalf("IsAdminUser() = %v, want %v", got, tc.want)
			}
		})
	}
}

func TestRequireAdminSession(t *testing.T) {
	nextCalled := false
	next := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		nextCalled = true
		w.WriteHeader(http.StatusNoContent)
	})

	request := httptest.NewRequest(http.MethodGet, "/api/admin/me", nil)
	request = request.WithContext(ContextWithUser(request.Context(), AuthenticatedUser{
		UserProfile: UserProfile{ID: "user-id", UserType: UserTypeHuman, Status: "active", Roles: []string{RoleAdmin}},
		SessionID:   "session-id",
	}))
	response := httptest.NewRecorder()

	RequireAdminSession(next).ServeHTTP(response, request)

	if response.Code != http.StatusNoContent {
		t.Fatalf("status = %d, want %d", response.Code, http.StatusNoContent)
	}
	if !nextCalled {
		t.Fatal("expected next handler to be called")
	}
}

func TestRequireAdminSessionRejectsPortalUser(t *testing.T) {
	next := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		t.Fatal("next handler must not be called")
	})

	request := httptest.NewRequest(http.MethodGet, "/api/admin/me", nil)
	request = request.WithContext(ContextWithUser(request.Context(), AuthenticatedUser{
		UserProfile: UserProfile{ID: "user-id", UserType: UserTypeHuman, Status: "active", Roles: []string{RoleVPNUser}},
		SessionID:   "session-id",
	}))
	response := httptest.NewRecorder()

	RequireAdminSession(next).ServeHTTP(response, request)

	if response.Code != http.StatusForbidden {
		t.Fatalf("status = %d, want %d; body=%s", response.Code, http.StatusForbidden, response.Body.String())
	}
}

func TestRequireAdminSessionRejectsMissingAuthContext(t *testing.T) {
	next := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		t.Fatal("next handler must not be called")
	})
	request := httptest.NewRequest(http.MethodGet, "/api/admin/me", nil)
	response := httptest.NewRecorder()

	RequireAdminSession(next).ServeHTTP(response, request)

	if response.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d, want %d; body=%s", response.Code, http.StatusUnauthorized, response.Body.String())
	}
}
