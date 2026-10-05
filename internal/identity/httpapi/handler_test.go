package httpapi

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/go-chi/chi/v5"

	"brightbuy-backend/internal/identity/app"
	"brightbuy-backend/internal/identity/domain"
	identauth "brightbuy-backend/internal/shared/auth"
	"brightbuy-backend/internal/shared/ratelimit"
)

// --- local fakes, same idea as catalog/app's own: hand-written stand-ins for identity/app's port
// interfaces, kept local to this test file since app's own fakes are unexported to its package.

type fakeUserRepository struct {
	byEmail map[string]*domain.Account
	byID    map[int]*domain.Account
	nextID  int
}

func newFakeUserRepository() *fakeUserRepository {
	return &fakeUserRepository{byEmail: map[string]*domain.Account{}, byID: map[int]*domain.Account{}}
}

func (f *fakeUserRepository) CreateCustomer(ctx context.Context, email, passwordHash, name, phone string) (*domain.Account, error) {
	if _, exists := f.byEmail[email]; exists {
		return nil, app.ErrEmailAlreadyRegistered
	}
	f.nextID++
	cid := f.nextID + 1000
	acct := &domain.Account{ID: f.nextID, Email: email, PasswordHash: passwordHash, RoleName: domain.RoleNameCustomer, Name: name, IsActive: true, CustomerID: &cid}
	f.byEmail[email] = acct
	f.byID[acct.ID] = acct
	return acct, nil
}

func (f *fakeUserRepository) CreateStaffAccount(ctx context.Context, email, passwordHash, name string, roleID int) (*domain.Account, error) {
	if _, exists := f.byEmail[email]; exists {
		return nil, app.ErrEmailAlreadyRegistered
	}
	f.nextID++
	roleName := "WAREHOUSE_STAFF"
	if roleID == adminRoleIDForTest {
		roleName = domain.RoleNameAdmin
	}
	acct := &domain.Account{ID: f.nextID, Email: email, PasswordHash: passwordHash, RoleID: roleID, RoleName: roleName, Name: name, IsActive: true}
	f.byEmail[email] = acct
	f.byID[acct.ID] = acct
	return acct, nil
}

func (f *fakeUserRepository) FindByEmail(ctx context.Context, email string) (*domain.Account, error) {
	acct, ok := f.byEmail[email]
	if !ok {
		return nil, errTestNotFound
	}
	return acct, nil
}

func (f *fakeUserRepository) FindByID(ctx context.Context, id int) (*domain.Account, error) {
	acct, ok := f.byID[id]
	if !ok {
		return nil, errTestNotFound
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
	count := 0
	for _, a := range f.byID {
		if a.RoleName == roleName {
			count++
		}
	}
	return count, nil
}

const unknownRoleIDForTest = 999999
const adminRoleIDForTest = 5

type fakeRoleRepository struct{}

func (f *fakeRoleRepository) FindByID(ctx context.Context, id int) (*domain.Role, error) {
	if id == unknownRoleIDForTest {
		return nil, errTestNotFound
	}
	name := "WAREHOUSE_STAFF"
	if id == adminRoleIDForTest {
		name = domain.RoleNameAdmin
	}
	return &domain.Role{ID: id, Name: name}, nil
}
func (f *fakeRoleRepository) FindByName(ctx context.Context, name string) (*domain.Role, error) {
	return &domain.Role{ID: 1, Name: name}, nil
}
func (f *fakeRoleRepository) ResolvePermissionCodes(ctx context.Context, roleID int) ([]string, error) {
	return []string{"catalog:write"}, nil
}
func (f *fakeRoleRepository) ListRolesWithPermissions(ctx context.Context) ([]domain.RoleWithPermissions, error) {
	return []domain.RoleWithPermissions{{Role: domain.Role{ID: 1, Name: "CUSTOMER"}}}, nil
}
func (f *fakeRoleRepository) ListPermissions(ctx context.Context) ([]domain.Permission, error) {
	return []domain.Permission{{ID: 1, Code: "catalog:write", Description: "x"}}, nil
}
func (f *fakeRoleRepository) SetRolePermissions(ctx context.Context, roleID int, codes []string, grantedBy int) (*domain.RoleWithPermissions, error) {
	if roleID == unknownRoleIDForTest {
		return nil, app.ErrUnknownRole
	}
	for _, c := range codes {
		if c == "not:a:real:code" {
			return nil, app.ErrUnknownPermissionCode
		}
	}
	return &domain.RoleWithPermissions{Role: domain.Role{ID: roleID, Name: "WAREHOUSE_STAFF"}, PermissionCodes: codes}, nil
}

type fakeRefreshTokenRepository struct {
	byHash map[string]*domain.RefreshToken
	nextID int
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
		return nil, errTestNotFound
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
	now := time.Now()
	for _, t := range f.byHash {
		if t.UserID == userID {
			t.RevokedAt = &now
		}
	}
	return nil
}

type testNotFoundError struct{}

func (e *testNotFoundError) Error() string { return "test: not found" }

var errTestNotFound = &testNotFoundError{}

// --- test harness ----------------------------------------------------------------------------

type testHarness struct {
	router      http.Handler
	users       *fakeUserRepository
	roles       *fakeRoleRepository
	refreshRepo *fakeRefreshTokenRepository
	tokens      *identauth.TokenIssuer
}

func newTestHarness() *testHarness {
	users := newFakeUserRepository()
	roles := &fakeRoleRepository{}
	refreshRepo := newFakeRefreshTokenRepository()
	tokens := identauth.NewTokenIssuer("test-signing-key")

	authService := app.NewAuthService(users, roles, refreshRepo, tokens)
	accountService := app.NewAccountService(users, roles)

	authHandler := NewAuthHandler(authService, false, ratelimit.New(1000, 1000))
	adminHandler := NewAdminHandler(accountService)

	r := chi.NewRouter()
	RegisterRoutes(r, authHandler, adminHandler, tokens, ratelimit.New(1000, 1000), ratelimit.New(1000, 1000))

	return &testHarness{router: r, users: users, roles: roles, refreshRepo: refreshRepo, tokens: tokens}
}

func (h *testHarness) accessTokenFor(t *testing.T, userID int, role string, perms []string) string {
	t.Helper()
	token, err := h.tokens.IssueAccessToken(userID, role, perms)
	if err != nil {
		t.Fatalf("IssueAccessToken: %v", err)
	}
	return token
}

func doJSON(t *testing.T, router http.Handler, method, path, body string, cookies ...*http.Cookie) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest(method, path, strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	for _, c := range cookies {
		req.AddCookie(c)
	}
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)
	return rec
}

// --- Register ----------------------------------------------------------------------------------

func TestRegister_Returns201AndCreatesCustomer(t *testing.T) {
	h := newTestHarness()
	rec := doJSON(t, h.router, http.MethodPost, "/auth/register",
		`{"name":"Ada Lovelace","email":"ada@example.com","password":"password123","phone":"555-0100"}`)

	if rec.Code != http.StatusCreated {
		t.Fatalf("status = %d, want %d; body = %s", rec.Code, http.StatusCreated, rec.Body.String())
	}
	var profile UserProfileDTO
	if err := json.Unmarshal(rec.Body.Bytes(), &profile); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if profile.Role != domain.RoleNameCustomer || profile.CustomerID == nil {
		t.Errorf("profile = %+v, want role CUSTOMER with a CustomerID", profile)
	}
}

func TestRegister_Returns400ForShortPassword(t *testing.T) {
	h := newTestHarness()
	rec := doJSON(t, h.router, http.MethodPost, "/auth/register",
		`{"name":"Ada","email":"ada@example.com","password":"short","phone":"555-0100"}`)

	if rec.Code != http.StatusBadRequest {
		t.Errorf("status = %d, want %d (FR-AUTH-7: 8-char minimum); body = %s", rec.Code, http.StatusBadRequest, rec.Body.String())
	}
}

func TestRegister_Returns409ForDuplicateEmail(t *testing.T) {
	h := newTestHarness()
	body := `{"name":"Ada","email":"dup@example.com","password":"password123","phone":"555-0100"}`
	if rec := doJSON(t, h.router, http.MethodPost, "/auth/register", body); rec.Code != http.StatusCreated {
		t.Fatalf("first register: status = %d", rec.Code)
	}

	rec := doJSON(t, h.router, http.MethodPost, "/auth/register", body)
	if rec.Code != http.StatusConflict {
		t.Errorf("status = %d, want %d (AC-AUTH-2)", rec.Code, http.StatusConflict)
	}
}

// --- Login / cookies -----------------------------------------------------------------------------

func TestLogin_SetsHttpOnlySecureStrictCookiesAndReturnsProfile(t *testing.T) {
	h := newTestHarness()
	registerBody := `{"name":"Staff","email":"login@example.com","password":"password123","phone":"555-0100"}`
	doJSON(t, h.router, http.MethodPost, "/auth/register", registerBody)

	rec := doJSON(t, h.router, http.MethodPost, "/auth/login", `{"email":"login@example.com","password":"password123"}`)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d; body = %s", rec.Code, http.StatusOK, rec.Body.String())
	}

	cookies := rec.Result().Cookies()
	var sawAccess, sawRefresh bool
	for _, c := range cookies {
		if c.Name == identauth.AccessTokenCookie {
			sawAccess = true
			if !c.HttpOnly || c.SameSite != http.SameSiteStrictMode {
				t.Errorf("access cookie = %+v, want HttpOnly=true, SameSite=Strict (02_SECURITY_BASELINE.md §2)", c)
			}
		}
		if c.Name == identauth.RefreshTokenCookie {
			sawRefresh = true
		}
	}
	if !sawAccess || !sawRefresh {
		t.Errorf("cookies = %v, want both %s and %s set", cookies, identauth.AccessTokenCookie, identauth.RefreshTokenCookie)
	}
}

func TestLogin_Returns401ForWrongPassword(t *testing.T) {
	h := newTestHarness()
	doJSON(t, h.router, http.MethodPost, "/auth/register",
		`{"name":"Staff","email":"wrongpw@example.com","password":"password123","phone":"555-0100"}`)

	rec := doJSON(t, h.router, http.MethodPost, "/auth/login", `{"email":"wrongpw@example.com","password":"wrong-password"}`)
	if rec.Code != http.StatusUnauthorized {
		t.Errorf("status = %d, want %d", rec.Code, http.StatusUnauthorized)
	}
}

func TestLogin_Returns401ForUnknownEmailWithIdenticalBodyToWrongPassword(t *testing.T) {
	h := newTestHarness()
	doJSON(t, h.router, http.MethodPost, "/auth/register",
		`{"name":"Staff","email":"known@example.com","password":"password123","phone":"555-0100"}`)

	wrongPasswordRec := doJSON(t, h.router, http.MethodPost, "/auth/login", `{"email":"known@example.com","password":"wrong"}`)
	unknownEmailRec := doJSON(t, h.router, http.MethodPost, "/auth/login", `{"email":"nobody@example.com","password":"wrong"}`)

	if wrongPasswordRec.Code != unknownEmailRec.Code || wrongPasswordRec.Body.String() != unknownEmailRec.Body.String() {
		t.Errorf("FR-AUTH-8 violated: wrong-password response (%d, %s) differs from unknown-email response (%d, %s)",
			wrongPasswordRec.Code, wrongPasswordRec.Body.String(), unknownEmailRec.Code, unknownEmailRec.Body.String())
	}
}

// --- Refresh / Logout / Me -------------------------------------------------------------------------

func TestRefresh_RequiresRefreshCookie(t *testing.T) {
	h := newTestHarness()
	rec := doJSON(t, h.router, http.MethodPost, "/auth/refresh", "")
	if rec.Code != http.StatusUnauthorized {
		t.Errorf("status = %d, want %d", rec.Code, http.StatusUnauthorized)
	}
}

func TestLogout_RequiresAuthentication(t *testing.T) {
	h := newTestHarness()
	rec := doJSON(t, h.router, http.MethodPost, "/auth/logout", "")
	if rec.Code != http.StatusUnauthorized {
		t.Errorf("status = %d, want %d (no access token cookie)", rec.Code, http.StatusUnauthorized)
	}
}

func TestLogoutThenRefresh_RefreshIsRejected(t *testing.T) {
	// The real end-to-end shape of AC-AUTH-4: log in, log out, then try to refresh with the OLD
	// refresh cookie — must fail, proving logout actually revoked it server-side.
	h := newTestHarness()
	doJSON(t, h.router, http.MethodPost, "/auth/register",
		`{"name":"Customer","email":"logout-flow@example.com","password":"password123","phone":"555-0100"}`)
	loginRec := doJSON(t, h.router, http.MethodPost, "/auth/login", `{"email":"logout-flow@example.com","password":"password123"}`)

	var accessCookie, refreshCookie *http.Cookie
	for _, c := range loginRec.Result().Cookies() {
		switch c.Name {
		case identauth.AccessTokenCookie:
			accessCookie = c
		case identauth.RefreshTokenCookie:
			refreshCookie = c
		}
	}
	if accessCookie == nil || refreshCookie == nil {
		t.Fatal("login didn't set both cookies")
	}

	logoutRec := doJSON(t, h.router, http.MethodPost, "/auth/logout", "", accessCookie, refreshCookie)
	if logoutRec.Code != http.StatusNoContent {
		t.Fatalf("logout status = %d, want %d", logoutRec.Code, http.StatusNoContent)
	}

	refreshRec := doJSON(t, h.router, http.MethodPost, "/auth/refresh", "", refreshCookie)
	if refreshRec.Code != http.StatusUnauthorized {
		t.Errorf("refresh-after-logout status = %d, want %d (AC-AUTH-4)", refreshRec.Code, http.StatusUnauthorized)
	}
}

func TestMe_RequiresAuthenticationAndReturnsProfile(t *testing.T) {
	h := newTestHarness()
	acct, err := h.users.CreateCustomer(context.Background(), "me@example.com", "hash", "Me Myself", "555-0100")
	if err != nil {
		t.Fatalf("CreateCustomer: %v", err)
	}

	unauthRec := doJSON(t, h.router, http.MethodGet, "/auth/me", "")
	if unauthRec.Code != http.StatusUnauthorized {
		t.Errorf("unauthenticated: status = %d, want %d", unauthRec.Code, http.StatusUnauthorized)
	}

	token := h.accessTokenFor(t, acct.ID, domain.RoleNameCustomer, nil)
	rec := doJSON(t, h.router, http.MethodGet, "/auth/me", "", &http.Cookie{Name: identauth.AccessTokenCookie, Value: token})
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d; body = %s", rec.Code, http.StatusOK, rec.Body.String())
	}
	var profile UserProfileDTO
	if err := json.Unmarshal(rec.Body.Bytes(), &profile); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if profile.Name != "Me Myself" {
		t.Errorf("Name = %q, want \"Me Myself\"", profile.Name)
	}
}

// --- Admin endpoints -------------------------------------------------------------------------------

func TestCreateAccount_Returns403WithoutPermission(t *testing.T) {
	h := newTestHarness()
	token := h.accessTokenFor(t, 1, domain.RoleNameCustomer, nil) // no account:manage_users

	rec := doJSON(t, h.router, http.MethodPost, "/admin/users",
		`{"name":"New Staff","email":"staff@example.com","password":"password123","roleId":7}`,
		&http.Cookie{Name: identauth.AccessTokenCookie, Value: token})
	if rec.Code != http.StatusForbidden {
		t.Errorf("status = %d, want %d", rec.Code, http.StatusForbidden)
	}
}

func TestCreateAccount_SucceedsWithPermission(t *testing.T) {
	h := newTestHarness()
	token := h.accessTokenFor(t, 1, domain.RoleNameAdmin, []string{"*"})

	rec := doJSON(t, h.router, http.MethodPost, "/admin/users",
		`{"name":"New Staff","email":"newstaff@example.com","password":"password123","roleId":7}`,
		&http.Cookie{Name: identauth.AccessTokenCookie, Value: token})
	if rec.Code != http.StatusCreated {
		t.Fatalf("status = %d, want %d; body = %s", rec.Code, http.StatusCreated, rec.Body.String())
	}
}

func TestCreateAccount_Returns400ForUnknownRole(t *testing.T) {
	h := newTestHarness()
	token := h.accessTokenFor(t, 1, domain.RoleNameAdmin, []string{"*"})

	body := `{"name":"X","email":"x@example.com","password":"password123","roleId":` +
		strconv.Itoa(unknownRoleIDForTest) + `}`
	rec := doJSON(t, h.router, http.MethodPost, "/admin/users", body,
		&http.Cookie{Name: identauth.AccessTokenCookie, Value: token})
	if rec.Code != http.StatusBadRequest {
		t.Errorf("status = %d, want %d (plan.md §6: unknown roleId -> 400)", rec.Code, http.StatusBadRequest)
	}
}

func TestSetRolePermissions_AcceptsAdminRoleWrite(t *testing.T) {
	h := newTestHarness()
	token := h.accessTokenFor(t, 1, domain.RoleNameAdmin, []string{"*"})

	rec := doJSON(t, h.router, http.MethodPut, "/admin/roles/5/permissions",
		`{"permissionCodes":[]}`,
		&http.Cookie{Name: identauth.AccessTokenCookie, Value: token})
	if rec.Code != http.StatusOK {
		t.Errorf("status = %d, want %d (AC-AUTH-7: accepted, not rejected)", rec.Code, http.StatusOK)
	}
}

// --- Security hygiene checks -------------------------------------------------------------------

// TestDTOsNeverExposePassword is this feature's version of catalog's
// TestDTOsNeverDeclareAStockQuantityField: walk every response DTO's actual struct tags via
// reflection, so a future field addition (e.g. someone adding PasswordHash to UserProfileDTO for a
// debugging convenience) fails this test immediately, rather than relying on code review to catch it
// (FR-AUTH-3: "never stored or logged" — extending that to "never returned in any response" means
// this needs to be structurally impossible, not just avoided by habit).
func TestDTOsNeverExposePassword(t *testing.T) {
	dtoTypes := []any{UserProfileDTO{}, PageDTO{}, UserListDTO{}, RoleDTO{}, PermissionDTO{}}
	for _, dto := range dtoTypes {
		typ := reflect.TypeOf(dto)
		for i := 0; i < typ.NumField(); i++ {
			field := typ.Field(i)
			tag := strings.ToLower(field.Tag.Get("json"))
			if strings.Contains(tag, "password") {
				t.Errorf("%s.%s has json tag %q — a password must never be a response DTO field (FR-AUTH-3)",
					typ.Name(), field.Name, field.Tag.Get("json"))
			}
		}
	}
}
