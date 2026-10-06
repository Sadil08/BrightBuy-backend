package app

import (
	"context"
	"errors"
	"testing"

	"brightbuy-backend/internal/identity/domain"
	"brightbuy-backend/internal/shared/auth"
)

func newTestAuthService(users *fakeUserRepository, roles *fakeRoleRepository, refresh *fakeRefreshTokenRepository) *AuthService {
	return NewAuthService(users, roles, refresh, auth.NewTokenIssuer("test-signing-key"))
}

func TestRegisterCreatesCustomerAndLowercasesEmail(t *testing.T) {
	users := newFakeUserRepository()
	svc := newTestAuthService(users, &fakeRoleRepository{}, newFakeRefreshTokenRepository())

	acct, err := svc.Register(context.Background(), RegisterInput{
		Name: "Ada Lovelace", Email: "Ada@Example.COM", Password: "password123", Phone: "555-0100",
	})
	if err != nil {
		t.Fatalf("Register: %v", err)
	}

	if acct.Email != "ada@example.com" {
		t.Errorf("Email = %q, want lowercased \"ada@example.com\"", acct.Email)
	}
	if acct.PasswordHash == "password123" {
		t.Error("PasswordHash is the plaintext password — Register must hash it")
	}
	if acct.CustomerID == nil {
		t.Error("CustomerID is nil, want a customer profile created alongside the account")
	}
}

func TestRegisterRejectsDuplicateEmail(t *testing.T) {
	users := newFakeUserRepository()
	svc := newTestAuthService(users, &fakeRoleRepository{}, newFakeRefreshTokenRepository())

	ctx := context.Background()
	in := RegisterInput{Name: "First", Email: "dup@example.com", Password: "password123", Phone: "555-0100"}
	if _, err := svc.Register(ctx, in); err != nil {
		t.Fatalf("first Register: %v", err)
	}

	in.Name = "Second"
	_, err := svc.Register(ctx, in)
	if !errors.Is(err, ErrEmailAlreadyRegistered) {
		t.Errorf("second Register error = %v, want ErrEmailAlreadyRegistered", err)
	}
}

func TestLoginSucceedsWithCorrectPassword(t *testing.T) {
	users := newFakeUserRepository()
	hash, _ := auth.HashPassword("correct-password")
	users.byEmail["customer@example.com"] = &domain.Account{ID: 1, Email: "customer@example.com", PasswordHash: hash, RoleName: "CUSTOMER", IsActive: true}
	users.byID[1] = users.byEmail["customer@example.com"]

	svc := newTestAuthService(users, &fakeRoleRepository{permissionCodes: map[int][]string{}}, newFakeRefreshTokenRepository())

	tokens, err := svc.Login(context.Background(), "customer@example.com", "correct-password")
	if err != nil {
		t.Fatalf("Login: %v", err)
	}
	if tokens.AccessToken == "" || tokens.RefreshToken == "" {
		t.Error("Login succeeded but returned an empty token")
	}
}

func TestLoginFailsIdenticallyForUnknownEmailWrongPasswordAndInactiveAccount(t *testing.T) {
	users := newFakeUserRepository()
	hash, _ := auth.HashPassword("correct-password")
	users.byEmail["active@example.com"] = &domain.Account{ID: 1, Email: "active@example.com", PasswordHash: hash, RoleName: "CUSTOMER", IsActive: true}
	users.byEmail["inactive@example.com"] = &domain.Account{ID: 2, Email: "inactive@example.com", PasswordHash: hash, RoleName: "CUSTOMER", IsActive: false}

	svc := newTestAuthService(users, &fakeRoleRepository{}, newFakeRefreshTokenRepository())
	ctx := context.Background()

	cases := []struct {
		name     string
		email    string
		password string
	}{
		{"unknown email", "nobody@example.com", "whatever"},
		{"wrong password for a real account", "active@example.com", "wrong-password"},
		{"correct password but deactivated account", "inactive@example.com", "correct-password"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_, err := svc.Login(ctx, tc.email, tc.password)
			if !errors.Is(err, ErrInvalidCredentials) {
				t.Errorf("error = %v, want ErrInvalidCredentials (FR-AUTH-8: same message every time)", err)
			}
		})
	}
}

// TestResolvePermissionsAdminBypassNeverQueriesTheRepository is the test tasks.md's T3 entry
// specifically calls for: proving SEC-AUTH-1's bypass isn't just "happens to return the right
// answer today" but structurally never reaches the database for ADMIN at all. panicIfResolveCalled
// turns "the fake would have returned something" into "the test crashes if this code path is ever
// taken," which is a much stronger guarantee.
func TestResolvePermissionsAdminBypassNeverQueriesTheRepository(t *testing.T) {
	roles := &fakeRoleRepository{panicIfResolveCalled: true}
	adminAccount := &domain.Account{ID: 1, RoleID: 999, RoleName: domain.RoleNameAdmin}

	perms, err := resolvePermissions(context.Background(), roles, adminAccount)
	if err != nil {
		t.Fatalf("resolvePermissions: %v", err)
	}
	if len(perms) != 1 || perms[0] != "*" {
		t.Errorf("perms = %v, want [\"*\"]", perms)
	}
}

func TestResolvePermissionsNonAdminQueriesTheRealRepository(t *testing.T) {
	roles := &fakeRoleRepository{permissionCodes: map[int][]string{7: {"catalog:write", "stock:adjust"}}}
	staffAccount := &domain.Account{ID: 2, RoleID: 7, RoleName: "WAREHOUSE_STAFF"}

	perms, err := resolvePermissions(context.Background(), roles, staffAccount)
	if err != nil {
		t.Fatalf("resolvePermissions: %v", err)
	}
	if len(perms) != 2 || perms[0] != "catalog:write" {
		t.Errorf("perms = %v, want the real repository's list, not the ADMIN wildcard", perms)
	}
}

func TestRefreshRotatesTokenAndReResolvesPermissions(t *testing.T) {
	users := newFakeUserRepository()
	hash, _ := auth.HashPassword("password123")
	acct := &domain.Account{ID: 1, Email: "staff@example.com", PasswordHash: hash, RoleID: 7, RoleName: "WAREHOUSE_STAFF", IsActive: true}
	users.byEmail[acct.Email] = acct
	users.byID[acct.ID] = acct

	roles := &fakeRoleRepository{permissionCodes: map[int][]string{7: {"catalog:write"}}}
	refreshRepo := newFakeRefreshTokenRepository()
	svc := newTestAuthService(users, roles, refreshRepo)

	loginTokens, err := svc.Login(context.Background(), acct.Email, "password123")
	if err != nil {
		t.Fatalf("Login: %v", err)
	}

	// ADMIN just revoked catalog:write from WAREHOUSE_STAFF — simulate that happening between
	// login and refresh, exactly what AC-AUTH-6 describes.
	roles.permissionCodes[7] = []string{}

	refreshedTokens, err := svc.Refresh(context.Background(), loginTokens.RefreshToken)
	if err != nil {
		t.Fatalf("Refresh: %v", err)
	}
	if refreshedTokens.RefreshToken == loginTokens.RefreshToken {
		t.Error("Refresh returned the SAME refresh token — it must rotate to a new one")
	}

	// The real proof AC-AUTH-6 holds: decode the NEW access token and check its permissions,
	// rather than just trusting Refresh "did something."
	issuer := auth.NewTokenIssuer("test-signing-key")
	claims, err := issuer.VerifyAccessToken(refreshedTokens.AccessToken)
	if err != nil {
		t.Fatalf("VerifyAccessToken on the refreshed token: %v", err)
	}
	if len(claims.Permissions) != 0 {
		t.Errorf("refreshed token's permissions = %v, want empty (just revoked)", claims.Permissions)
	}
}

func TestRefreshRejectsAlreadyRevokedTokenAndRevokesEverythingForThatAccount(t *testing.T) {
	users := newFakeUserRepository()
	hash, _ := auth.HashPassword("password123")
	acct := &domain.Account{ID: 1, Email: "customer@example.com", PasswordHash: hash, RoleName: "CUSTOMER", IsActive: true}
	users.byEmail[acct.Email] = acct
	users.byID[acct.ID] = acct

	refreshRepo := newFakeRefreshTokenRepository()
	svc := newTestAuthService(users, &fakeRoleRepository{}, refreshRepo)

	tokens, err := svc.Login(context.Background(), acct.Email, "password123")
	if err != nil {
		t.Fatalf("Login: %v", err)
	}

	// First refresh: legitimate, rotates the token.
	if _, err := svc.Refresh(context.Background(), tokens.RefreshToken); err != nil {
		t.Fatalf("first Refresh: %v", err)
	}

	// Second refresh with the SAME (now-superseded) token: plan.md §6's theft-detection path.
	_, err = svc.Refresh(context.Background(), tokens.RefreshToken)
	if !errors.Is(err, ErrRefreshTokenInvalid) {
		t.Errorf("reused-token Refresh error = %v, want ErrRefreshTokenInvalid", err)
	}
	if len(refreshRepo.revokedAllFor) != 1 || refreshRepo.revokedAllFor[0] != acct.ID {
		t.Errorf("RevokeAllForUser calls = %v, want exactly one call for account %d", refreshRepo.revokedAllFor, acct.ID)
	}
}

func TestLogoutIsIdempotent(t *testing.T) {
	svc := newTestAuthService(newFakeUserRepository(), &fakeRoleRepository{}, newFakeRefreshTokenRepository())

	// Logging out with a token that was never issued at all — must not error (AuthService.Logout's
	// own doc: "logging out with an already-invalid/unknown token is not an error").
	if err := svc.Logout(context.Background(), "never-issued-token"); err != nil {
		t.Errorf("Logout with an unknown token returned an error: %v", err)
	}
}
