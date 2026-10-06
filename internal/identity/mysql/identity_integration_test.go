//go:build integration

// Same pattern as internal/catalog/mysql's integration suite: a disposable MySQL 8 container, the
// real db/migrations/*.up.sql applied via the golang-migrate LIBRARY (not testcontainers' WithScripts
// — see that package's TestMain comment for exactly why that distinction matters). All migrations in
// the directory run together regardless of which feature's test this is, so this container also gets
// 01-catalog's schema/function along the way — hence the same --log-bin-trust-function-creators=1
// flag this test needs too, even though identity itself has no stored functions.
package mysql

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"testing"
	"time"

	"github.com/golang-migrate/migrate/v4"
	_ "github.com/golang-migrate/migrate/v4/database/mysql"
	_ "github.com/golang-migrate/migrate/v4/source/file"
	"github.com/testcontainers/testcontainers-go"
	tcmysql "github.com/testcontainers/testcontainers-go/modules/mysql"

	"brightbuy-backend/internal/identity/app"
	"brightbuy-backend/internal/identity/domain"
	"brightbuy-backend/internal/shared/dbx"
)

var testDB *sql.DB

func TestMain(m *testing.M) {
	os.Exit(runTestMain(m))
}

func runTestMain(m *testing.M) int {
	ctx := context.Background()

	container, err := tcmysql.Run(ctx, "mysql:8.0",
		tcmysql.WithDatabase("brightbuy_test"),
		tcmysql.WithUsername("brightbuy_test"),
		tcmysql.WithPassword("test-password"),
		testcontainers.WithCmd("--log-bin-trust-function-creators=1", "--default-time-zone=+00:00"),
	)
	if err != nil {
		fmt.Fprintln(os.Stderr, "start mysql container:", err)
		return 1
	}
	defer func() {
		if err := testcontainers.TerminateContainer(container); err != nil {
			fmt.Fprintln(os.Stderr, "terminate mysql container:", err)
		}
	}()

	dsn, err := container.ConnectionString(ctx, "parseTime=true")
	if err != nil {
		fmt.Fprintln(os.Stderr, "build connection string:", err)
		return 1
	}

	migrationsDir, err := migrationsDirPath()
	if err != nil {
		fmt.Fprintln(os.Stderr, "locate migrations dir:", err)
		return 1
	}
	migrator, err := migrate.New("file://"+migrationsDir, "mysql://"+dsn)
	if err != nil {
		fmt.Fprintln(os.Stderr, "create migrate instance:", err)
		return 1
	}
	if err := migrator.Up(); err != nil && err != migrate.ErrNoChange {
		fmt.Fprintln(os.Stderr, "run migrations:", err)
		return 1
	}

	db, err := openWithRetry(dsn, 5, 2*time.Second)
	if err != nil {
		fmt.Fprintln(os.Stderr, "connect to mysql container:", err)
		return 1
	}
	defer db.Close()

	testDB = db
	return m.Run()
}

func openWithRetry(dsn string, attempts int, delay time.Duration) (*sql.DB, error) {
	var lastErr error
	for i := 0; i < attempts; i++ {
		db, err := dbx.Open(dsn)
		if err == nil {
			return db, nil
		}
		lastErr = err
		time.Sleep(delay)
	}
	return nil, lastErr
}

// migrationsDirPath resolves db/migrations relative to THIS file (internal/identity/mysql/...) —
// three levels up to brightbuy-backend/, same reasoning as catalog's identical helper.
func migrationsDirPath() (string, error) {
	_, thisFile, _, ok := runtime.Caller(0)
	if !ok {
		return "", fmt.Errorf("runtime.Caller failed")
	}
	return filepath.Join(filepath.Dir(thisFile), "..", "..", "..", "db", "migrations"), nil
}

func uniqueSuffix() string {
	return fmt.Sprintf("%d", time.Now().UnixNano())
}

// --- UserRepository ------------------------------------------------------------------------------

func TestUserRepository_CreateCustomer_CreatesAccountAndCustomerProfile(t *testing.T) {
	ctx := context.Background()
	repo := NewUserRepository(testDB)
	email := "customer-" + uniqueSuffix() + "@example.com"

	acct, err := repo.CreateCustomer(ctx, email, "hashed-password", "Ada Lovelace", "555-0100")
	if err != nil {
		t.Fatalf("CreateCustomer: %v", err)
	}

	if acct.Email != email || acct.Name != "Ada Lovelace" || acct.RoleName != domain.RoleNameCustomer {
		t.Errorf("acct = %+v, want matching email/name/role", acct)
	}
	if acct.CustomerID == nil {
		t.Fatal("CustomerID is nil, want a real customer row created")
	}
	if !acct.IsActive {
		t.Error("IsActive = false, want true for a newly created account")
	}

	found, err := repo.FindByEmail(ctx, email)
	if err != nil {
		t.Fatalf("FindByEmail: %v", err)
	}
	if found.ID != acct.ID {
		t.Errorf("FindByEmail returned a different account than CreateCustomer produced")
	}
}

func TestUserRepository_CreateCustomer_RejectsDuplicateEmail(t *testing.T) {
	ctx := context.Background()
	repo := NewUserRepository(testDB)
	email := "dup-" + uniqueSuffix() + "@example.com"

	if _, err := repo.CreateCustomer(ctx, email, "hash1", "First", "555-0100"); err != nil {
		t.Fatalf("first CreateCustomer: %v", err)
	}

	_, err := repo.CreateCustomer(ctx, email, "hash2", "Second", "555-0200")
	if !errors.Is(err, app.ErrEmailAlreadyRegistered) {
		t.Errorf("second CreateCustomer error = %v, want app.ErrEmailAlreadyRegistered (FR-AUTH-2)", err)
	}

	// And no duplicate row was actually created — the unique index really did its job, not just the
	// Go-level error translation.
	var count int
	if err := testDB.QueryRowContext(ctx, `SELECT COUNT(*) FROM user_account WHERE email = ?`, email).Scan(&count); err != nil {
		t.Fatalf("count user_account rows: %v", err)
	}
	if count != 1 {
		t.Errorf("user_account rows with email %q = %d, want exactly 1", email, count)
	}
}

func TestUserRepository_CreateCustomer_EmailUniquenessIsCaseInsensitive(t *testing.T) {
	ctx := context.Background()
	repo := NewUserRepository(testDB)
	suffix := uniqueSuffix()
	lower := "case-" + suffix + "@example.com"
	upper := "CASE-" + suffix + "@EXAMPLE.COM"

	if _, err := repo.CreateCustomer(ctx, lower, "hash", "First", "555-0100"); err != nil {
		t.Fatalf("CreateCustomer (lowercase): %v", err)
	}

	_, err := repo.CreateCustomer(ctx, upper, "hash", "Second", "555-0200")
	if !errors.Is(err, app.ErrEmailAlreadyRegistered) {
		t.Errorf("CreateCustomer with different-case email error = %v, want ErrEmailAlreadyRegistered (utf8mb4_0900_ai_ci)", err)
	}
}

func TestUserRepository_CreateStaffAccount_CreatesAccountAndStaffProfile(t *testing.T) {
	ctx := context.Background()
	roleRepo := NewRoleRepository(testDB)
	userRepo := NewUserRepository(testDB)

	warehouseStaffRole := findRoleByName(t, ctx, roleRepo, "WAREHOUSE_STAFF")
	email := "staff-" + uniqueSuffix() + "@example.com"

	acct, err := userRepo.CreateStaffAccount(ctx, email, "hashed-password", "New Staffer", warehouseStaffRole.ID)
	if err != nil {
		t.Fatalf("CreateStaffAccount: %v", err)
	}
	if acct.RoleName != "WAREHOUSE_STAFF" || acct.CustomerID != nil {
		t.Errorf("acct = %+v, want role WAREHOUSE_STAFF and no CustomerID (staff, not customer)", acct)
	}

	var staffProfileCount int
	if err := testDB.QueryRowContext(ctx, `SELECT COUNT(*) FROM staff_profile WHERE user_account_id = ?`, acct.ID).Scan(&staffProfileCount); err != nil {
		t.Fatalf("count staff_profile: %v", err)
	}
	if staffProfileCount != 1 {
		t.Errorf("staff_profile rows for account %d = %d, want 1", acct.ID, staffProfileCount)
	}
}

func TestUserRepository_ListAccountsAndCountByRoleName(t *testing.T) {
	ctx := context.Background()
	repo := NewUserRepository(testDB)
	suffix := uniqueSuffix()

	before, err := repo.CountByRoleName(ctx, domain.RoleNameCustomer)
	if err != nil {
		t.Fatalf("CountByRoleName: %v", err)
	}

	if _, err := repo.CreateCustomer(ctx, "list-"+suffix+"@example.com", "hash", "Lister", "555-0100"); err != nil {
		t.Fatalf("CreateCustomer: %v", err)
	}

	after, err := repo.CountByRoleName(ctx, domain.RoleNameCustomer)
	if err != nil {
		t.Fatalf("CountByRoleName: %v", err)
	}
	if after != before+1 {
		t.Errorf("CountByRoleName(CUSTOMER) = %d, want %d (before + the one just created)", after, before+1)
	}

	accounts, total, err := repo.ListAccounts(ctx, 1, 1000)
	if err != nil {
		t.Fatalf("ListAccounts: %v", err)
	}
	if total != len(accounts) {
		// with pageSize 1000 and far fewer than 1000 test accounts ever created, total should equal
		// the length of the one page fetched
		t.Errorf("total = %d, len(accounts) = %d, want equal", total, len(accounts))
	}
}

// --- RoleRepository -------------------------------------------------------------------------------

func findRoleByName(t *testing.T, ctx context.Context, repo *RoleRepository, name string) domain.Role {
	t.Helper()
	roles, err := repo.ListRolesWithPermissions(ctx)
	if err != nil {
		t.Fatalf("ListRolesWithPermissions: %v", err)
	}
	for _, r := range roles {
		if r.Name == name {
			return r.Role
		}
	}
	t.Fatalf("no role named %q found among seeded roles", name)
	return domain.Role{}
}

func TestRoleRepository_ResolvePermissionCodes_MatchesSeededGrants(t *testing.T) {
	ctx := context.Background()
	repo := NewRoleRepository(testDB)

	warehouseStaff := findRoleByName(t, ctx, repo, "WAREHOUSE_STAFF")
	codes, err := repo.ResolvePermissionCodes(ctx, warehouseStaff.ID)
	if err != nil {
		t.Fatalf("ResolvePermissionCodes: %v", err)
	}

	want := map[string]bool{"catalog:write": true, "catalog:image:write": true, "stock:adjust": true}
	if len(codes) != len(want) {
		t.Fatalf("codes = %v, want exactly %v (migration 0004's seeded default grants)", codes, want)
	}
	for _, c := range codes {
		if !want[c] {
			t.Errorf("unexpected permission code %q for WAREHOUSE_STAFF", c)
		}
	}
}

func TestRoleRepository_ListPermissions_ReturnsTheFullSeededCatalog(t *testing.T) {
	ctx := context.Background()
	repo := NewRoleRepository(testDB)

	perms, err := repo.ListPermissions(ctx)
	if err != nil {
		t.Fatalf("ListPermissions: %v", err)
	}
	if len(perms) != 8 {
		t.Errorf("len(perms) = %d, want 8 (migration 0004's seeded permission catalog)", len(perms))
	}
}

func TestRoleRepository_SetRolePermissions_ReplacesTheFullSet(t *testing.T) {
	ctx := context.Background()
	repo := NewRoleRepository(testDB)
	managerRole := findRoleByName(t, ctx, repo, "MANAGER")

	// MANAGER starts with exactly reports:view (migration 0004's seed) — replace it with a
	// DIFFERENT single permission, and confirm the old one is actually gone, not just appended to.
	result, err := repo.SetRolePermissions(ctx, managerRole.ID, []string{"catalog:write"}, 1)
	if err != nil {
		t.Fatalf("SetRolePermissions: %v", err)
	}
	if len(result.PermissionCodes) != 1 || result.PermissionCodes[0] != "catalog:write" {
		t.Errorf("result.PermissionCodes = %v, want exactly [catalog:write]", result.PermissionCodes)
	}

	codes, err := repo.ResolvePermissionCodes(ctx, managerRole.ID)
	if err != nil {
		t.Fatalf("ResolvePermissionCodes after replace: %v", err)
	}
	if len(codes) != 1 || codes[0] != "catalog:write" {
		t.Errorf("codes after replace = %v, want exactly [catalog:write] (reports:view should be GONE, not kept alongside)", codes)
	}

	// Restore the seed's original grant so this test doesn't leave MANAGER permanently changed for
	// whichever test runs next in the same container.
	if _, err := repo.SetRolePermissions(ctx, managerRole.ID, []string{"reports:view"}, 1); err != nil {
		t.Fatalf("restore SetRolePermissions: %v", err)
	}
}

func TestRoleRepository_SetRolePermissions_RejectsUnknownRoleAndUnknownCode(t *testing.T) {
	ctx := context.Background()
	repo := NewRoleRepository(testDB)
	managerRole := findRoleByName(t, ctx, repo, "MANAGER")

	if _, err := repo.SetRolePermissions(ctx, 999999, []string{"reports:view"}, 1); !errors.Is(err, app.ErrUnknownRole) {
		t.Errorf("unknown role: err = %v, want app.ErrUnknownRole", err)
	}

	if _, err := repo.SetRolePermissions(ctx, managerRole.ID, []string{"not:a:real:code"}, 1); !errors.Is(err, app.ErrUnknownPermissionCode) {
		t.Errorf("unknown permission code: err = %v, want app.ErrUnknownPermissionCode", err)
	}

	// Confirm the rejected call didn't partially apply — MANAGER should still have its original
	// grant untouched, since the whole point of resolving codes BEFORE deleting anything is that a
	// failure here leaves the existing rows alone.
	codes, err := repo.ResolvePermissionCodes(ctx, managerRole.ID)
	if err != nil {
		t.Fatalf("ResolvePermissionCodes: %v", err)
	}
	if len(codes) != 1 || codes[0] != "reports:view" {
		t.Errorf("MANAGER's permissions after a rejected SetRolePermissions = %v, want untouched [reports:view]", codes)
	}
}

func TestRoleRepository_SetRolePermissions_AcceptsAdminRole(t *testing.T) {
	// AC-AUTH-7: a write against ADMIN's own row is accepted, even though enforcement
	// (identity/app's resolvePermissions) never reads this table for that role.
	ctx := context.Background()
	repo := NewRoleRepository(testDB)
	adminRole := findRoleByName(t, ctx, repo, domain.RoleNameAdmin)

	original, err := repo.ResolvePermissionCodes(ctx, adminRole.ID)
	if err != nil {
		t.Fatalf("ResolvePermissionCodes: %v", err)
	}

	if _, err := repo.SetRolePermissions(ctx, adminRole.ID, []string{}, 1); err != nil {
		t.Fatalf("SetRolePermissions against ADMIN: %v, want accepted (AC-AUTH-7)", err)
	}

	// Restore, so later tests in this same container still see ADMIN's full seeded grant.
	if _, err := repo.SetRolePermissions(ctx, adminRole.ID, original, 1); err != nil {
		t.Fatalf("restore ADMIN permissions: %v", err)
	}
}

// --- RefreshTokenRepository ------------------------------------------------------------------------

func TestRefreshTokenRepository_CreateFindRevokeRoundTrip(t *testing.T) {
	ctx := context.Background()
	userRepo := NewUserRepository(testDB)
	tokenRepo := NewRefreshTokenRepository(testDB)

	acct, err := userRepo.CreateCustomer(ctx, "token-"+uniqueSuffix()+"@example.com", "hash", "Token Owner", "555-0100")
	if err != nil {
		t.Fatalf("CreateCustomer: %v", err)
	}

	hash := "test-hash-" + uniqueSuffix()
	expiresAt := time.Now().Add(7 * 24 * time.Hour)
	if err := tokenRepo.Create(ctx, acct.ID, hash, expiresAt); err != nil {
		t.Fatalf("Create: %v", err)
	}

	found, err := tokenRepo.FindByHash(ctx, hash)
	if err != nil {
		t.Fatalf("FindByHash: %v", err)
	}
	if found.UserID != acct.ID || found.RevokedAt != nil {
		t.Errorf("found = %+v, want UserID %d and RevokedAt nil (not yet revoked)", found, acct.ID)
	}
	if !found.IsValid(time.Now()) {
		t.Error("freshly created token reports IsValid = false")
	}

	if err := tokenRepo.Revoke(ctx, found.ID); err != nil {
		t.Fatalf("Revoke: %v", err)
	}

	afterRevoke, err := tokenRepo.FindByHash(ctx, hash)
	if err != nil {
		t.Fatalf("FindByHash after revoke: %v", err)
	}
	if afterRevoke.RevokedAt == nil {
		t.Error("RevokedAt is nil after Revoke — revocation didn't persist")
	}
	if afterRevoke.IsValid(time.Now()) {
		t.Error("IsValid = true for a revoked token, want false")
	}
}

func TestRefreshTokenRepository_RevokeAllForUser(t *testing.T) {
	ctx := context.Background()
	userRepo := NewUserRepository(testDB)
	tokenRepo := NewRefreshTokenRepository(testDB)

	acct, err := userRepo.CreateCustomer(ctx, "revokeall-"+uniqueSuffix()+"@example.com", "hash", "Multi Session", "555-0100")
	if err != nil {
		t.Fatalf("CreateCustomer: %v", err)
	}

	hash1 := "hash1-" + uniqueSuffix()
	hash2 := "hash2-" + uniqueSuffix()
	expiresAt := time.Now().Add(7 * 24 * time.Hour)
	if err := tokenRepo.Create(ctx, acct.ID, hash1, expiresAt); err != nil {
		t.Fatalf("Create (1): %v", err)
	}
	if err := tokenRepo.Create(ctx, acct.ID, hash2, expiresAt); err != nil {
		t.Fatalf("Create (2): %v", err)
	}

	if err := tokenRepo.RevokeAllForUser(ctx, acct.ID); err != nil {
		t.Fatalf("RevokeAllForUser: %v", err)
	}

	for _, hash := range []string{hash1, hash2} {
		token, err := tokenRepo.FindByHash(ctx, hash)
		if err != nil {
			t.Fatalf("FindByHash(%q): %v", hash, err)
		}
		if token.RevokedAt == nil {
			t.Errorf("token %q not revoked after RevokeAllForUser", hash)
		}
	}
}
