package app

import (
	"context"
	"errors"
	"testing"

	"brightbuy-backend/internal/identity/domain"
)

func TestCreateStaffAccountSucceedsForKnownRole(t *testing.T) {
	users := newFakeUserRepository()
	roles := &fakeRoleRepository{byID: map[int]*domain.Role{7: {ID: 7, Name: "WAREHOUSE_STAFF"}}}
	svc := NewAccountService(users, roles)

	acct, err := svc.CreateStaffAccount(context.Background(), "New Staffer", "Staff@Example.com", "password123", 7)
	if err != nil {
		t.Fatalf("CreateStaffAccount: %v", err)
	}
	if acct.Email != "staff@example.com" {
		t.Errorf("Email = %q, want lowercased", acct.Email)
	}
	if acct.RoleID != 7 {
		t.Errorf("RoleID = %d, want 7", acct.RoleID)
	}
}

func TestCreateStaffAccountRejectsUnknownRole(t *testing.T) {
	svc := NewAccountService(newFakeUserRepository(), &fakeRoleRepository{byID: map[int]*domain.Role{}})

	_, err := svc.CreateStaffAccount(context.Background(), "Someone", "someone@example.com", "password123", 999)
	if !errors.Is(err, ErrUnknownRole) {
		t.Errorf("error = %v, want ErrUnknownRole (plan.md §6: 400, not 500)", err)
	}
}

// TestSetRolePermissionsAcceptsAdminRoleWithNoSpecialCase is AC-AUTH-7: a write against ADMIN's own
// row is accepted like any other role — there's no branch in AccountService that treats ADMIN
// differently, because enforcement (where the real protection lives) never reads this data for that
// role in the first place (resolvePermissions, auth_service.go).
func TestSetRolePermissionsAcceptsAdminRoleWithNoSpecialCase(t *testing.T) {
	adminRoleID := 5
	roles := &fakeRoleRepository{byID: map[int]*domain.Role{adminRoleID: {ID: adminRoleID, Name: domain.RoleNameAdmin}}}
	svc := NewAccountService(newFakeUserRepository(), roles)

	result, err := svc.SetRolePermissions(context.Background(), adminRoleID, []string{}, 1)
	if err != nil {
		t.Fatalf("SetRolePermissions against ADMIN returned an error, want it accepted: %v", err)
	}
	if len(result.PermissionCodes) != 0 {
		t.Errorf("PermissionCodes = %v, want the empty set that was just written", result.PermissionCodes)
	}
}

func TestListRolesAndListPermissionsPassThrough(t *testing.T) {
	roles := &fakeRoleRepository{
		rolesWithPermissions: []domain.RoleWithPermissions{{Role: domain.Role{ID: 1, Name: "CUSTOMER"}}},
		permissions:          []domain.Permission{{ID: 1, Code: "catalog:write", Description: "x"}},
	}
	svc := NewAccountService(newFakeUserRepository(), roles)

	gotRoles, err := svc.ListRoles(context.Background())
	if err != nil || len(gotRoles) != 1 {
		t.Errorf("ListRoles = %v, %v", gotRoles, err)
	}

	gotPerms, err := svc.ListPermissions(context.Background())
	if err != nil || len(gotPerms) != 1 {
		t.Errorf("ListPermissions = %v, %v", gotPerms, err)
	}
}
