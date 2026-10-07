package mysql

import (
	"context"
	"database/sql"
	"errors"
	"fmt"

	"brightbuy-backend/internal/identity/app"
	mysqlDriver "github.com/go-sql-driver/mysql"
)

type UserRepository struct {
	db *sql.DB
}

func NewUserRepository(db *sql.DB) *UserRepository {
	return &UserRepository{db: db}
}

func (r *UserRepository) CreateCustomer(ctx context.Context, name, email, passwordHash string) (app.Account, error) {
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return app.Account{}, fmt.Errorf("begin account transaction: %w", err)
	}
	defer tx.Rollback()

	var roleID int
	if err := tx.QueryRowContext(ctx, `SELECT role_id FROM role WHERE name = 'CUSTOMER'`).Scan(&roleID); err != nil {
		return app.Account{}, fmt.Errorf("find CUSTOMER role: %w", err)
	}
	result, err := tx.ExecContext(ctx,
		`INSERT INTO user_account (email, password_hash, role_id, is_active) VALUES (?, ?, ?, TRUE)`,
		email, passwordHash, roleID,
	)
	if err != nil {
		var mysqlErr *mysqlDriver.MySQLError
		if errors.As(err, &mysqlErr) && mysqlErr.Number == 1062 {
			return app.Account{}, app.ErrEmailExists
		}
		return app.Account{}, fmt.Errorf("insert user account: %w", err)
	}
	userID64, err := result.LastInsertId()
	if err != nil {
		return app.Account{}, fmt.Errorf("read new user id: %w", err)
	}
	result, err = tx.ExecContext(ctx,
		`INSERT INTO customer (user_account_id, name) VALUES (?, ?)`,
		userID64, name,
	)
	if err != nil {
		return app.Account{}, fmt.Errorf("insert customer profile: %w", err)
	}
	customerID64, err := result.LastInsertId()
	if err != nil {
		return app.Account{}, fmt.Errorf("read new customer id: %w", err)
	}
	if err := tx.Commit(); err != nil {
		return app.Account{}, fmt.Errorf("commit account transaction: %w", err)
	}
	return app.Account{
		UserID: int(userID64), CustomerID: int(customerID64), Email: email, Active: true,
	}, nil
}

func (r *UserRepository) FindByEmail(ctx context.Context, email string) (app.Account, error) {
	var account app.Account
	err := r.db.QueryRowContext(ctx, `
		SELECT ua.user_account_id, c.customer_id, ua.email, ua.password_hash, ua.is_active
		FROM user_account ua
		JOIN customer c ON c.user_account_id = ua.user_account_id
		JOIN role r ON r.role_id = ua.role_id
		WHERE ua.email = ? AND r.name = 'CUSTOMER'
	`, email).Scan(&account.UserID, &account.CustomerID, &account.Email, &account.PasswordHash, &account.Active)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return app.Account{}, app.ErrInvalidCredentials
		}
		return app.Account{}, fmt.Errorf("find customer account: %w", err)
	}
	return account, nil
}
