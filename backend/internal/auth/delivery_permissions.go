package auth

func init() {
	BuiltInPermissions = append(BuiltInPermissions,
		"deliveries:read",
		"deliveries:send",
	)

	BuiltInRoles["super_admin"] = BuiltInPermissions
	BuiltInRoles["admin"] = append(BuiltInRoles["admin"],
		"deliveries:read",
		"deliveries:send",
	)
	BuiltInRoles["operator"] = append(BuiltInRoles["operator"],
		"deliveries:read",
		"deliveries:send",
	)
	BuiltInRoles["read_only"] = append(BuiltInRoles["read_only"],
		"deliveries:read",
	)
}
