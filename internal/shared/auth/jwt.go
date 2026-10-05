package auth

import (
	"errors"
	"fmt"
	"strconv"
	"time"

	"github.com/golang-jwt/jwt/v5"
)

// accessTokenTTL is fixed, not configurable — specs/global/02_SECURITY_BASELINE.md §2: "Access
// token: short-lived (<=15 min)." The whole point of a short TTL is bounding how stale a permission
// list can get after an ADMIN revokes one (§1.1) — making this a tunable setting would make that
// bound someone's config mistake away from being meaningless.
const accessTokenTTL = 15 * time.Minute

// jwtClaims is the actual structure encoded into the token. Deliberately separate from the public
// Claims type (claims.go): jwtClaims embeds jwt.RegisteredClaims purely to satisfy golang-jwt's
// encoding needs (exp/iat/iss/sub) — nothing outside this file ever sees a jwtClaims value or
// imports golang-jwt. The rest of the backend works with Claims, which has no JWT-library
// fingerprints on it at all.
type jwtClaims struct {
	jwt.RegisteredClaims
	Role        string   `json:"role"`
	Permissions []string `json:"perms"`
}

// ErrInvalidToken covers every way a token can fail verification — expired, bad signature,
// malformed, wrong issuer. Deliberately one sentinel, not several: shared/auth's Authenticate
// middleware (middleware.go) only ever needs to know "is this request authenticated or not," never
// which specific reason a token failed (specs/global/02_SECURITY_BASELINE.md §2: "expired/invalid ->
// 401" — one response, one message, for every failure shape, so there's no oracle a caller could use
// to tell "expired" apart from "forged").
var ErrInvalidToken = errors.New("auth: invalid token")

// TokenIssuer is the one thing in the whole backend that holds the JWT signing key — constructed
// once in cmd/api's composition root from config.JWTSigningKey, then handed to whatever needs to
// issue or verify a token (identity's AuthService to issue; shared/auth's own middleware to verify).
type TokenIssuer struct {
	signingKey []byte
	issuer     string
}

func NewTokenIssuer(signingKey string) *TokenIssuer {
	return &TokenIssuer{signingKey: []byte(signingKey), issuer: "brightbuy"}
}

// AccessTokenTTL exposes the fixed TTL above to callers that need to know it without duplicating the
// constant — e.g. identity/httpapi setting the access-token cookie's own Max-Age to match exactly.
func (t *TokenIssuer) AccessTokenTTL() time.Duration {
	return accessTokenTTL
}

// IssueAccessToken signs a new access token carrying userID/role/permissions, expiring
// AccessTokenTTL from now. permissions should already be fully resolved (plan.md §5.1/§5.2) — this
// function has no idea role_permission exists, it just encodes whatever list it's handed.
func (t *TokenIssuer) IssueAccessToken(userID int, role string, permissions []string) (string, error) {
	now := time.Now()
	claims := jwtClaims{
		RegisteredClaims: jwt.RegisteredClaims{
			Subject:   strconv.Itoa(userID),
			Issuer:    t.issuer,
			IssuedAt:  jwt.NewNumericDate(now),
			ExpiresAt: jwt.NewNumericDate(now.Add(accessTokenTTL)),
		},
		Role:        role,
		Permissions: permissions,
	}

	token := jwt.NewWithClaims(jwt.SigningMethodHS256, claims)
	signed, err := token.SignedString(t.signingKey)
	if err != nil {
		return "", fmt.Errorf("auth: sign access token: %w", err)
	}
	return signed, nil
}

// VerifyAccessToken parses and fully validates tokenString (signature, expiry, issuer), returning
// the Claims it carries if valid, or ErrInvalidToken for any failure at all.
func (t *TokenIssuer) VerifyAccessToken(tokenString string) (*Claims, error) {
	var claims jwtClaims
	token, err := jwt.ParseWithClaims(tokenString, &claims, func(token *jwt.Token) (any, error) {
		// Pinning the expected signing METHOD here, not just trusting whatever the token itself
		// claims, closes off the classic JWT "algorithm confusion" attack: a library that blindly
		// uses the algorithm named in the token's own header can be tricked into verifying a forged
		// token under a different (or deliberately weak/absent) algorithm. We only ever sign with
		// HMAC, so we only ever accept HMAC back.
		if _, ok := token.Method.(*jwt.SigningMethodHMAC); !ok {
			return nil, fmt.Errorf("auth: unexpected signing method %v", token.Header["alg"])
		}
		return t.signingKey, nil
	}, jwt.WithIssuer(t.issuer))
	if err != nil || !token.Valid {
		return nil, ErrInvalidToken
	}

	userID, err := strconv.Atoi(claims.Subject)
	if err != nil {
		return nil, ErrInvalidToken
	}

	return &Claims{UserID: userID, Role: claims.Role, Permissions: claims.Permissions}, nil
}
