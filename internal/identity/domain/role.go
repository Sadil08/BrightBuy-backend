package domain

// RoleNameAdmin is the one role name shared/auth's permission-resolution logic treats specially —
// SEC-AUTH-1's unconditional bypass. Defined here, not hardcoded as a string literal at each call
// site, so the one place that decides "what does ADMIN mean" can't drift from the actual seeded row
// name (plan.md §2.1: `INSERT INTO role (name) VALUES (..., 'ADMIN')`).
const RoleNameAdmin = "ADMIN"

// RoleNameCustomer is the role every self-serve registration (AuthService.Register) creates —
// there's no other way to become a CUSTOMER, and no self-serve way to become anything else
// (FR-AUTH-9: only ADMIN creates staff/manager/admin accounts).
const RoleNameCustomer = "CUSTOMER"

// Role is one role row — a fixed, small catalog (CUSTOMER, WAREHOUSE_STAFF, ORDER_MANAGER, MANAGER,
// ADMIN per plan.md §2.1), never created/deleted at runtime, only read.
type Role struct {
	ID   int
	Name string
}

// Permission is one row from the capability catalog (07_SQL_DATABASE_STANDARDS.md §1a) — a fixed
// code like "catalog:write" plus a human-readable description, for the admin console to display.
type Permission struct {
	ID          int
	Code        string
	Description string
}

// RoleWithPermissions pairs a Role with the permission codes currently granted to it — what
// GET /admin/roles and PUT .../permissions both return (openapi.yaml's Role schema). ADMIN's entry
// here still reflects whatever role_permission actually holds (for console display, SEC-AUTH-1's own
// comment in the migration) — this type carries the DATA, it's shared/auth's ResolvePermissions
// (app/role_service.go) that applies the ADMIN bypass for actual enforcement.
type RoleWithPermissions struct {
	Role
	PermissionCodes []string
}
