package app

import (
	"context"

	"brightbuy-backend/internal/identity/domain"
	"brightbuy-backend/internal/shared/auth"
)

// AccountService is the admin-only half of identity: creating staff/manager/admin accounts (FR-AUTH-9)
// and managing the role/permission catalog (FR-AUTH-10). Every method here is gated behind
// RequirePermission("account:manage_users"/"account:manage_roles") in httpapi — this service itself
// has no idea what a permission check even is, it trusts its caller already did one, same as every
// other app-layer service in this codebase trusts its httpapi caller to have validated/authorized
// before calling in.
type AccountService struct {
	users UserRepository
	roles RoleRepository
}

func NewAccountService(users UserRepository, roles RoleRepository) *AccountService {
	return &AccountService{users: users, roles: roles}
}

// CreateStaffAccount is FR-AUTH-9: ADMIN creating a WAREHOUSE_STAFF/ORDER_MANAGER/MANAGER/ADMIN
// account directly — there is no self-registration path for these roles anywhere (that's
// AuthService.Register, which only ever creates CUSTOMER). Returns ErrUnknownRole for a roleId that
// doesn't exist (plan.md §6: "400, not 500") rather than letting a foreign-key violation surface as
// an opaque database error.
func (s *AccountService) CreateStaffAccount(ctx context.Context, name, email, password string, roleID int) (*domain.Account, error) {
	role, err := s.roles.FindByID(ctx, roleID)
	if err != nil {
		return nil, ErrUnknownRole
	}

	hash, err := auth.HashPassword(password)
	if err != nil {
		return nil, err
	}
	return s.users.CreateStaffAccount(ctx, normalizeEmail(email), hash, name, role.ID)
}

// NormalizePage/NormalizePageSize: the same "one piece of clamping logic, two call sites" fix
// 01-catalog's own pagination needed (app.ListFilter.Normalized() there) — ListAccounts clamps its
// own copy of page/pageSize internally, but httpapi ALSO needs the clamped values when building the
// response's pagination metadata (a request with no ?page= at all must report back "page": 1, not
// "page": 0). Calling these same two functions in both places means there's one definition of
// "what's a valid page," not two that could quietly drift apart.
func NormalizePage(page int) int {
	if page < 1 {
		return 1
	}
	return page
}

func NormalizePageSize(pageSize int) int {
	if pageSize < 1 || pageSize > 100 {
		return 20
	}
	return pageSize
}

// ListAccounts is GET /admin/users — a paginated dump of every account, any role.
func (s *AccountService) ListAccounts(ctx context.Context, page, pageSize int) ([]domain.Account, int, error) {
	return s.users.ListAccounts(ctx, NormalizePage(page), NormalizePageSize(pageSize))
}

// ListRoles is GET /admin/roles — every role with its currently-granted permission codes, including
// ADMIN's (for console display; SEC-AUTH-1 means those rows are never what actually gates an ADMIN
// request, but an admin console still needs something to show as "what ADMIN currently holds").
func (s *AccountService) ListRoles(ctx context.Context) ([]domain.RoleWithPermissions, error) {
	return s.roles.ListRolesWithPermissions(ctx)
}

// ListPermissions is GET /admin/permissions — the full, fixed capability catalog.
func (s *AccountService) ListPermissions(ctx context.Context) ([]domain.Permission, error) {
	return s.roles.ListPermissions(ctx)
}

// SetRolePermissions is PUT /admin/roles/{roleId}/permissions — FR-AUTH-10, replacing the FULL set
// of permissions a role holds. Works identically for every role INCLUDING ADMIN (AC-AUTH-7: the
// write is accepted, for console display consistency) — this service has no ADMIN special case at
// all, because the thing that actually matters (enforcement) lives entirely in
// auth_service.go's resolvePermissions and shared/auth's middleware, neither of which this method
// touches. Writing data here can never be "the way to weaken ADMIN," by construction, not by a check.
func (s *AccountService) SetRolePermissions(ctx context.Context, roleID int, permissionCodes []string, grantedByUserID int) (*domain.RoleWithPermissions, error) {
	return s.roles.SetRolePermissions(ctx, roleID, permissionCodes, grantedByUserID)
}
