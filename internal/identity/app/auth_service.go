package app

import (
	"context"
	"strings"
	"time"

	"brightbuy-backend/internal/identity/domain"
	"brightbuy-backend/internal/shared/auth"
)

// refreshTokenTTL is fixed, same reasoning as shared/auth's accessTokenTTL: a security policy,
// not a tunable setting. plan.md describes it only as "longer-lived" than the 15-minute access
// token; 7 days is this project's concrete choice for that.
const refreshTokenTTL = 7 * 24 * time.Hour

// RegisterInput is the self-serve registration use case's input — already validated (required
// fields, email format, 8-char minimum) by httpapi's request DTO before this is ever constructed
// (specs/global/02_SECURITY_BASELINE.md §4: validation happens at the boundary, via struct tags, not
// re-checked here).
type RegisterInput struct {
	Name     string
	Email    string
	Password string
	Phone    string
}

// Tokens is what a successful Login/Refresh hands back to httpapi: the two token values (to set as
// cookies) plus the account they belong to (for the UserProfile response body). Neither token value
// is ever logged — httpapi's handler is the only thing that ever sees this struct, and it goes
// straight into Set-Cookie headers, never a log line (02_SECURITY_BASELINE.md §7).
type Tokens struct {
	AccessToken     string
	AccessTokenTTL  time.Duration
	RefreshToken    string
	RefreshTokenTTL time.Duration
	Account         *domain.Account
}

// AuthService is the self-serve half of identity: register, login, refresh, logout. Never touches
// role/permission MANAGEMENT (that's AccountService, admin-only) — just resolving an account's
// CURRENT permissions at issue/refresh time.
type AuthService struct {
	users         UserRepository
	roles         RoleRepository
	refreshTokens RefreshTokenRepository
	tokens        *auth.TokenIssuer
}

func NewAuthService(users UserRepository, roles RoleRepository, refreshTokens RefreshTokenRepository, tokens *auth.TokenIssuer) *AuthService {
	return &AuthService{users: users, roles: roles, refreshTokens: refreshTokens, tokens: tokens}
}

// Register is FR-AUTH-1: self-serve signup, always and only as role CUSTOMER — there is no
// "register as staff" path anywhere in this service (FR-AUTH-9: only ADMIN creates those, via
// AccountService.CreateStaffAccount). Returns ErrEmailAlreadyRegistered (mapped from the
// repository's own translation of MySQL's unique-index violation) rather than creating a duplicate.
func (s *AuthService) Register(ctx context.Context, in RegisterInput) (*domain.Account, error) {
	hash, err := auth.HashPassword(in.Password)
	if err != nil {
		return nil, err
	}
	return s.users.CreateCustomer(ctx, normalizeEmail(in.Email), hash, in.Name, in.Phone)
}

// Login is plan.md §5.1's generic-failure-message logic, made real: unknown email, wrong password,
// and a deactivated account all fail identically (FR-AUTH-8) — ComparePassword is still called even
// when acct is nil's error path already short-circuits, specifically so a nonexistent email takes
// roughly the same code path (and therefore roughly the same time) as a wrong password for a real
// account, rather than returning instantly on "no such user" and only hashing on the slow path —
// SEC-AUTH-3's "timing-insensitive enough not to leak which factor was wrong."
func (s *AuthService) Login(ctx context.Context, email, password string) (*Tokens, error) {
	acct, err := s.users.FindByEmail(ctx, normalizeEmail(email))
	if err != nil {
		// Still run a bcrypt comparison against a fixed dummy hash, even though we already know
		// this will fail — without this, "unknown email" returns in microseconds while "wrong
		// password for a real account" takes bcrypt's ~100ms, which IS a timing oracle an attacker
		// could use to enumerate valid emails, defeating FR-AUTH-8's entire point.
		auth.ComparePassword(dummyBcryptHash, password)
		return nil, ErrInvalidCredentials
	}
	if !auth.ComparePassword(acct.PasswordHash, password) || !acct.IsActive {
		return nil, ErrInvalidCredentials
	}

	return s.issueTokens(ctx, acct)
}

// dummyBcryptHash is a fixed, valid BCrypt hash of an arbitrary, never-used string — it doesn't
// correspond to any real account, it exists purely so Login's "unknown email" branch can spend
// roughly the same CPU time as its "wrong password" branch (see Login's comment).
const dummyBcryptHash = "$2a$12$WtawG60S6STNdRIQxbzyp.NHtoiTYlk7TsmHuIBuIo43SyMiNAYQW"

// Refresh is plan.md §5/§6: verify the presented refresh token is valid and not already superseded,
// rotate it (issue a new one, revoke this one — a refresh token is single-use), and re-resolve the
// account's CURRENT permissions (not whatever was baked into the old access token) — this is what
// makes AC-AUTH-6 (a permission revoked by ADMIN takes effect on next refresh) actually true.
func (s *AuthService) Refresh(ctx context.Context, rawRefreshToken string) (*Tokens, error) {
	hash := auth.HashRefreshToken(rawRefreshToken)
	existing, err := s.refreshTokens.FindByHash(ctx, hash)
	if err != nil {
		return nil, ErrRefreshTokenInvalid
	}

	if existing.RevokedAt != nil {
		// This exact token was already used once before (rotation already happened for it) — plan.md
		// §6 treats presenting it AGAIN as a possible token-theft signal (an attacker replaying a
		// stolen, already-rotated token) rather than a harmless retry: revoke EVERY refresh token
		// this account has, forcing a fresh login, instead of silently accepting it.
		_ = s.refreshTokens.RevokeAllForUser(ctx, existing.UserID)
		return nil, ErrRefreshTokenInvalid
	}
	if !existing.ExpiresAt.After(time.Now()) {
		return nil, ErrRefreshTokenInvalid
	}

	acct, err := s.users.FindByID(ctx, existing.UserID)
	if err != nil {
		return nil, ErrRefreshTokenInvalid
	}
	if !acct.IsActive {
		return nil, ErrRefreshTokenInvalid
	}

	if err := s.refreshTokens.Revoke(ctx, existing.ID); err != nil {
		return nil, err
	}
	return s.issueTokens(ctx, acct)
}

// GetAccount is GET /auth/me's use case: the access token's claims (shared/auth.Claims) only carry
// {sub, role, perms} — enough to authorize a request, not enough to render a profile page (no name,
// no email, no customerId). This re-fetches the full Account the claims' subject refers to.
func (s *AuthService) GetAccount(ctx context.Context, userID int) (*domain.Account, error) {
	return s.users.FindByID(ctx, userID)
}

// Logout is FR-AUTH-5: revoke this one refresh token server-side, so a subsequent /auth/refresh with
// it fails even though the cookie itself might still physically exist somewhere (a browser that
// didn't clear it, a copy an attacker already made). Idempotent on purpose — logging out with an
// already-invalid/unknown token is not an error, it's just already logged out.
func (s *AuthService) Logout(ctx context.Context, rawRefreshToken string) error {
	hash := auth.HashRefreshToken(rawRefreshToken)
	existing, err := s.refreshTokens.FindByHash(ctx, hash)
	if err != nil {
		return nil
	}
	return s.refreshTokens.Revoke(ctx, existing.ID)
}

// issueTokens is the shared tail end of Login and Refresh: resolve current permissions, sign a new
// access token, generate+store a new refresh token. Pulled out once rather than duplicated in both
// callers, since "how a session actually gets issued" is exactly the kind of logic that must never
// drift into two slightly-different copies.
func (s *AuthService) issueTokens(ctx context.Context, acct *domain.Account) (*Tokens, error) {
	perms, err := resolvePermissions(ctx, s.roles, acct)
	if err != nil {
		return nil, err
	}

	customerID := 0 // stays 0 for staff/manager/admin accounts, which have no customer profile
	if acct.CustomerID != nil {
		customerID = *acct.CustomerID
	}
	accessToken, err := s.tokens.IssueAccessTokenForCustomer(acct.ID, customerID, acct.RoleName, perms)
	if err != nil {
		return nil, err
	}

	rawRefresh, refreshHash, err := auth.GenerateRefreshToken()
	if err != nil {
		return nil, err
	}
	if err := s.refreshTokens.Create(ctx, acct.ID, refreshHash, time.Now().Add(refreshTokenTTL)); err != nil {
		return nil, err
	}

	return &Tokens{
		AccessToken:     accessToken,
		AccessTokenTTL:  s.tokens.AccessTokenTTL(),
		RefreshToken:    rawRefresh,
		RefreshTokenTTL: refreshTokenTTL,
		Account:         acct,
	}, nil
}

// resolvePermissions is SEC-AUTH-1 made real: ADMIN's permission list is the hardcoded sentinel
// "*", decided from acct.RoleName alone — roles.ResolvePermissionCodes is NEVER called for that
// role. This is a package-level function, not a method, specifically so a unit test can call it
// directly with a fake RoleRepository that panics if ResolvePermissionCodes is ever invoked, and
// prove the ADMIN branch really does return before reaching it — not just "happens to work today."
func resolvePermissions(ctx context.Context, roles RoleRepository, acct *domain.Account) ([]string, error) {
	if acct.RoleName == domain.RoleNameAdmin {
		return []string{"*"}, nil
	}
	return roles.ResolvePermissionCodes(ctx, acct.RoleID)
}

// normalizeEmail lowercases before every lookup/insert — plan.md §6: the unique index is already
// case-insensitive (the database's collation), but normalizing here too keeps every stored email
// visually consistent for an admin reading /admin/users, rather than whatever casing each user
// happened to type.
func normalizeEmail(email string) string {
	return strings.ToLower(strings.TrimSpace(email))
}
