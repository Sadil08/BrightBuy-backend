package auth

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"errors"
	"net/http"
	"strconv"
	"strings"
	"time"

	"brightbuy-backend/internal/shared/httpx"
)

const (
	sessionCookie = "brightbuy_session"
	tokenIssuer   = "brightbuy-api"
	sessionTTL    = 15 * time.Minute
)

var ErrInvalidToken = errors.New("invalid session token")

type Claims struct {
	UserID     int
	CustomerID int
	Role       string
	ExpiresAt  time.Time
}

type tokenPayload struct {
	Issuer     string `json:"iss"`
	Subject    string `json:"sub"`
	CustomerID int    `json:"customer_id"`
	Role       string `json:"role"`
	IssuedAt   int64  `json:"iat"`
	ExpiresAt  int64  `json:"exp"`
}

func IssueCustomerToken(signingKey []byte, userID, customerID int, now time.Time) (string, error) {
	if len(signingKey) < 32 || userID <= 0 || customerID <= 0 {
		return "", ErrInvalidToken
	}
	header, err := json.Marshal(map[string]string{"alg": "HS256", "typ": "JWT"})
	if err != nil {
		return "", err
	}
	payload, err := json.Marshal(tokenPayload{
		Issuer: tokenIssuer, Subject: strconv.Itoa(userID), CustomerID: customerID,
		Role: "CUSTOMER", IssuedAt: now.Unix(), ExpiresAt: now.Add(sessionTTL).Unix(),
	})
	if err != nil {
		return "", err
	}
	unsigned := base64.RawURLEncoding.EncodeToString(header) + "." + base64.RawURLEncoding.EncodeToString(payload)
	mac := hmac.New(sha256.New, signingKey)
	_, _ = mac.Write([]byte(unsigned))
	return unsigned + "." + base64.RawURLEncoding.EncodeToString(mac.Sum(nil)), nil
}

func VerifyCustomerToken(token string, signingKey []byte, now time.Time) (Claims, error) {
	parts := strings.Split(token, ".")
	if len(parts) != 3 || len(signingKey) < 32 {
		return Claims{}, ErrInvalidToken
	}
	header, err := base64.RawURLEncoding.DecodeString(parts[0])
	if err != nil {
		return Claims{}, ErrInvalidToken
	}
	var headerFields map[string]string
	if json.Unmarshal(header, &headerFields) != nil || headerFields["alg"] != "HS256" {
		return Claims{}, ErrInvalidToken
	}
	provided, err := base64.RawURLEncoding.DecodeString(parts[2])
	if err != nil {
		return Claims{}, ErrInvalidToken
	}
	mac := hmac.New(sha256.New, signingKey)
	_, _ = mac.Write([]byte(parts[0] + "." + parts[1]))
	if !hmac.Equal(provided, mac.Sum(nil)) {
		return Claims{}, ErrInvalidToken
	}
	payloadBytes, err := base64.RawURLEncoding.DecodeString(parts[1])
	if err != nil {
		return Claims{}, ErrInvalidToken
	}
	var payload tokenPayload
	if json.Unmarshal(payloadBytes, &payload) != nil || payload.Issuer != tokenIssuer ||
		payload.Role != "CUSTOMER" || payload.ExpiresAt <= now.Unix() || payload.IssuedAt > now.Add(time.Minute).Unix() {
		return Claims{}, ErrInvalidToken
	}
	userID, err := strconv.Atoi(payload.Subject)
	if err != nil || userID <= 0 || payload.CustomerID <= 0 {
		return Claims{}, ErrInvalidToken
	}
	return Claims{
		UserID: userID, CustomerID: payload.CustomerID, Role: payload.Role,
		ExpiresAt: time.Unix(payload.ExpiresAt, 0),
	}, nil
}

func SetCustomerCookie(w http.ResponseWriter, token string, secure bool) {
	http.SetCookie(w, &http.Cookie{
		Name: sessionCookie, Value: token, Path: "/", HttpOnly: true, Secure: secure,
		SameSite: http.SameSiteStrictMode, MaxAge: int(sessionTTL.Seconds()),
	})
}

type claimsContextKey struct{}

func CustomerClaims(r *http.Request) (Claims, bool) {
	claims, ok := r.Context().Value(claimsContextKey{}).(Claims)
	return claims, ok
}

func RequireCustomer(signingKey []byte, next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		cookie, err := r.Cookie(sessionCookie)
		if err != nil {
			httpx.WriteError(w, http.StatusUnauthorized, "UNAUTHENTICATED", "authentication required")
			return
		}
		claims, err := VerifyCustomerToken(cookie.Value, signingKey, time.Now())
		if err != nil {
			httpx.WriteError(w, http.StatusUnauthorized, "UNAUTHENTICATED", "authentication required")
			return
		}
		next.ServeHTTP(w, r.WithContext(context.WithValue(r.Context(), claimsContextKey{}, claims)))
	})
}
