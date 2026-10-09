package mysql

import (
	"context"
	"database/sql"
	"fmt"
	"time"

	"brightbuy-backend/internal/identity/domain"
)

type RefreshTokenRepository struct {
	db *sql.DB
}

func NewRefreshTokenRepository(db *sql.DB) *RefreshTokenRepository {
	return &RefreshTokenRepository{db: db}
}

func (r *RefreshTokenRepository) Create(ctx context.Context, userID int, tokenHash string, expiresAt time.Time) error {
	_, err := r.db.ExecContext(ctx,
		`INSERT INTO refresh_token (user_account_id, token_hash, expires_at) VALUES (?, ?, ?)`,
		userID, tokenHash, expiresAt,
	)
	if err != nil {
		return fmt.Errorf("mysql: create refresh token: %w", err)
	}
	return nil
}

func (r *RefreshTokenRepository) FindByHash(ctx context.Context, tokenHash string) (*domain.RefreshToken, error) {
	var (
		t         domain.RefreshToken
		revokedAt sql.NullTime
	)
	err := r.db.QueryRowContext(ctx,
		`SELECT refresh_token_id, user_account_id, token_hash, expires_at, revoked_at
		 FROM refresh_token WHERE token_hash = ?`,
		tokenHash,
	).Scan(&t.ID, &t.UserID, &t.TokenHash, &t.ExpiresAt, &revokedAt)
	if err != nil {
		return nil, fmt.Errorf("mysql: find refresh token: %w", err)
	}
	if revokedAt.Valid {
		t.RevokedAt = &revokedAt.Time
	}
	return &t, nil
}

// Revoke marks one refresh token revoked — `AND revoked_at IS NULL` makes this safely idempotent
// (revoking an already-revoked token is a no-op, not an error) and means the UPDATE never
// overwrites an earlier revocation's timestamp with a later one.
func (r *RefreshTokenRepository) Revoke(ctx context.Context, id int) error {
	_, err := r.db.ExecContext(ctx,
		`UPDATE refresh_token SET revoked_at = CURRENT_TIMESTAMP WHERE refresh_token_id = ? AND revoked_at IS NULL`,
		id,
	)
	if err != nil {
		return fmt.Errorf("mysql: revoke refresh token %d: %w", id, err)
	}
	return nil
}

// RevokeAllForUser is REQ-2.5's "log out everywhere," and also plan.md §6's reused-token response:
// every still-valid refresh token this account has, revoked in one statement.
func (r *RefreshTokenRepository) RevokeAllForUser(ctx context.Context, userID int) error {
	_, err := r.db.ExecContext(ctx,
		`UPDATE refresh_token SET revoked_at = CURRENT_TIMESTAMP WHERE user_account_id = ? AND revoked_at IS NULL`,
		userID,
	)
	if err != nil {
		return fmt.Errorf("mysql: revoke all refresh tokens for user %d: %w", userID, err)
	}
	return nil
}
