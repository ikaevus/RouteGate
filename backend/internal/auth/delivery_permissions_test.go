package auth

import "testing"

func TestDeliveryPermissionsAreAssignedToBuiltInRoles(t *testing.T) {
	assertRoleHasPermission(t, "super_admin", "deliveries:read")
	assertRoleHasPermission(t, "super_admin", "deliveries:send")
	assertRoleHasPermission(t, "admin", "deliveries:read")
	assertRoleHasPermission(t, "admin", "deliveries:send")
	assertRoleHasPermission(t, "operator", "deliveries:read")
	assertRoleHasPermission(t, "operator", "deliveries:send")
	assertRoleHasPermission(t, "read_only", "deliveries:read")
	assertRoleLacksPermission(t, "read_only", "deliveries:send")
	assertRoleLacksPermission(t, "vpn_user", "deliveries:read")
	assertRoleLacksPermission(t, "vpn_user", "deliveries:send")
	assertRoleLacksPermission(t, "agent", "deliveries:read")
}

func assertRoleHasPermission(t *testing.T, role, permission string) {
	t.Helper()
	for _, candidate := range BuiltInRoles[role] {
		if candidate == permission {
			return
		}
	}
	t.Fatalf("role %q is missing permission %q", role, permission)
}

func assertRoleLacksPermission(t *testing.T, role, permission string) {
	t.Helper()
	for _, candidate := range BuiltInRoles[role] {
		if candidate == permission {
			t.Fatalf("role %q unexpectedly has permission %q", role, permission)
		}
	}
}
