package app

import (
	"context"
	"time"

	"brightbuy-backend/internal/identity/domain"
)

// fakeUserRepository/fakeRoleRepository/fakeRefreshTokenRepository are hand-written stand-ins
// satisfying this package's own port interfaces (ports.go) — same pattern as catalog's
// fakeProductRepository: no real database, no mocking library, just a small struct whose methods
// happen to match.
type fakeUserRepository struct {
	byEmail           map[string]*domain.Account
	byID              map[int]*domain.Account
	nextID            int
	createCustomerErr error
	createStaffErr    error
	countByRoleResult int
	countByRoleErr    error
}

func newFakeUserRepository() *fakeUserRepository {
	return &fakeUserRepository{byEmail: map[string]*domain.Account{}, byID: map[int]*domain.Account{}}
}

func (f *fakeUserRepository) CreateCustomer(ctx context.Context, email, passwordHash, name, phone string) (*domain.Account, error) {
	if f.createCustomerErr != nil {
		return nil, f.createCustomerErr
	}
	if _, exists := f.byEmail[email]; exists {
		return nil, ErrEmailAlreadyRegistered
	}
	f.nextID++
	customerID := f.nextID + 1000 // distinct range, just so it's visibly not the same as user_account_id
	acct := &domain.Account{ID: f.nextID, Email: email, PasswordHash: passwordHash, RoleName: "CUSTOMER", Name: name, CustomerID: &customerID}
	f.byEmail[email] = acct
	f.byID[acct.ID] = acct
	return acct, nil
}

func (f *fakeUserRepository) CreateStaffAccount(ctx context.Context, email, passwordHash, name string, roleID int) (*domain.Account, error) {
	if f.createStaffErr != nil {
		return nil, f.createStaffErr
	}
	if _, exists := f.byEmail[email]; exists {
		return nil, ErrEmailAlreadyRegistered
	}
	f.nextID++
	acct := &domain.Account{ID: f.nextID, Email: email, PasswordHash: passwordHash, RoleID: roleID, Name: name}
	f.byEmail[email] = acct
	f.byID[acct.ID] = acct
	return acct, nil
}

func (f *fakeUserRepository) FindByEmail(ctx context.Context, email string) (*domain.Account, error) {
	acct, ok := f.byEmail[email]
	if !ok {
		return nil, errNotFoundForTest
	}
	return acct, nil
}

func (f *fakeUserRepository) FindByID(ctx context.Context, id int) (*domain.Account, error) {
	acct, ok := f.byID[id]
	if !ok {
		return nil, errNotFoundForTest
	}
	return acct, nil
}

func (f *fakeUserRepository) ListAccounts(ctx context.Context, page, pageSize int) ([]domain.Account, int, error) {
	var all []domain.Account
	for _, a := range f.byID {
		all = append(all, *a)
	}
	return all, len(all), nil
}

func (f *fakeUserRepository) CountByRoleName(ctx context.Context, roleName string) (int, error) {
	return f.countByRoleResult, f.countByRoleErr
}

// fakeRoleRepository: panicIfResolveCalled is the whole point of this fake existing — it lets a test
// assert that resolvePermissions' ADMIN branch returns BEFORE ever reaching
// ResolvePermissionCodes, not just that the eventual answer happens to be right.
type fakeRoleRepository struct {
	byID                  map[int]*domain.Role
	permissionCodes       map[int][]string
	panicIfResolveCalled  bool
	rolesWithPermissions  []domain.RoleWithPermissions
	permissions           []domain.Permission
	setRolePermissionsErr error
}

func (f *fakeRoleRepository) FindByID(ctx context.Context, id int) (*domain.Role, error) {
	role, ok := f.byID[id]
	if !ok {
		return nil, errNotFoundForTest
	}
	return role, nil
}

func (f *fakeRoleRepository) FindByName(ctx context.Context, name string) (*domain.Role, error) {
	for _, role := range f.byID {
		if role.Name == name {
			return role, nil
		}
	}
	return nil, errNotFoundForTest
}

func (f *fakeRoleRepository) ResolvePermissionCodes(ctx context.Context, roleID int) ([]string, error) {
	if f.panicIfResolveCalled {
		panic("ResolvePermissionCodes called for a role that should have taken the ADMIN bypass")
	}
	return f.permissionCodes[roleID], nil
}

func (f *fakeRoleRepository) ListRolesWithPermissions(ctx context.Context) ([]domain.RoleWithPermissions, error) {
	return f.rolesWithPermissions, nil
}

func (f *fakeRoleRepository) ListPermissions(ctx context.Context) ([]domain.Permission, error) {
	return f.permissions, nil
}

func (f *fakeRoleRepository) SetRolePermissions(ctx context.Context, roleID int, codes []string, grantedBy int) (*domain.RoleWithPermissions, error) {
	if f.setRolePermissionsErr != nil {
		return nil, f.setRolePermissionsErr
	}
	return &domain.RoleWithPermissions{Role: *f.byID[roleID], PermissionCodes: codes}, nil
}

type fakeRefreshTokenRepository struct {
	byHash        map[string]*domain.RefreshToken
	nextID        int
	revokedAllFor []int // records every userID RevokeAllForUser was called with, for assertions
}

func newFakeRefreshTokenRepository() *fakeRefreshTokenRepository {
	return &fakeRefreshTokenRepository{byHash: map[string]*domain.RefreshToken{}}
}

func (f *fakeRefreshTokenRepository) Create(ctx context.Context, userID int, tokenHash string, expiresAt time.Time) error {
	f.nextID++
	f.byHash[tokenHash] = &domain.RefreshToken{ID: f.nextID, UserID: userID, TokenHash: tokenHash, ExpiresAt: expiresAt}
	return nil
}

func (f *fakeRefreshTokenRepository) FindByHash(ctx context.Context, tokenHash string) (*domain.RefreshToken, error) {
	t, ok := f.byHash[tokenHash]
	if !ok {
		return nil, errNotFoundForTest
	}
	return t, nil
}

func (f *fakeRefreshTokenRepository) Revoke(ctx context.Context, id int) error {
	for _, t := range f.byHash {
		if t.ID == id {
			now := time.Now()
			t.RevokedAt = &now
		}
	}
	return nil
}

func (f *fakeRefreshTokenRepository) RevokeAllForUser(ctx context.Context, userID int) error {
	f.revokedAllFor = append(f.revokedAllFor, userID)
	now := time.Now()
	for _, t := range f.byHash {
		if t.UserID == userID {
			t.RevokedAt = &now
		}
	}
	return nil
}

var errNotFoundForTest = &testNotFoundError{}

type testNotFoundError struct{}

func (e *testNotFoundError) Error() string { return "test: not found" }

// A quick compile-time sanity check that these fakes actually satisfy the real interfaces — if any
// of ports.go's method signatures ever change, this file fails to compile instead of a test silently
// testing the wrong shape.
var (
	_ UserRepository         = (*fakeUserRepository)(nil)
	_ RoleRepository         = (*fakeRoleRepository)(nil)
	_ RefreshTokenRepository = (*fakeRefreshTokenRepository)(nil)
)
