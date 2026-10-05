package mysql

import (
	"context"
	"database/sql"
	"fmt"

	"brightbuy-backend/internal/identity/app"
	"brightbuy-backend/internal/identity/domain"
	"brightbuy-backend/internal/shared/dbx"
)

// UserRepository is the ADAPTER for app.UserRepository.
type UserRepository struct {
	db *sql.DB
}

func NewUserRepository(db *sql.DB) *UserRepository {
	return &UserRepository{db: db}
}

// accountSelectSQL is the one query every account-fetching method builds on — a LEFT JOIN to BOTH
// customer and staff_profile, since an account has exactly one of the two depending on its role
// (never both, never neither — see domain.Account's own comment). COALESCE picks whichever one is
// actually present.
const accountSelectSQL = `
SELECT ua.user_account_id, ua.email, ua.password_hash, ua.role_id, r.name, ua.is_active,
       c.customer_id, COALESCE(c.name, sp.name, '')
FROM user_account ua
JOIN role r ON r.role_id = ua.role_id
LEFT JOIN customer c ON c.user_account_id = ua.user_account_id
LEFT JOIN staff_profile sp ON sp.user_account_id = ua.user_account_id
`

// scanner is satisfied by both *sql.Row (QueryRowContext) and *sql.Rows (QueryContext) — letting
// scanAccount handle both "fetch one" and "fetch many" call sites with the same scanning logic
// instead of duplicating the column list and null-handling twice.
type scanner interface {
	Scan(dest ...any) error
}

func scanAccount(row scanner) (*domain.Account, error) {
	var (
		acct       domain.Account
		customerID sql.NullInt64
	)
	err := row.Scan(&acct.ID, &acct.Email, &acct.PasswordHash, &acct.RoleID, &acct.RoleName,
		&acct.IsActive, &customerID, &acct.Name)
	if err != nil {
		return nil, err
	}
	if customerID.Valid {
		id := int(customerID.Int64)
		acct.CustomerID = &id
	}
	return &acct, nil
}

// CreateCustomer inserts user_account + customer together, in one transaction (dbx.WithTx) — a
// customer profile can't exist without its account or vice versa, so nothing else can observe one
// without the other, even under a concurrent read.
func (r *UserRepository) CreateCustomer(ctx context.Context, email, passwordHash, name, phone string) (*domain.Account, error) {
	var account *domain.Account
	err := dbx.WithTx(ctx, r.db, func(tx *sql.Tx) error {
		var roleID int
		if err := tx.QueryRowContext(ctx, `SELECT role_id FROM role WHERE name = ?`, domain.RoleNameCustomer).Scan(&roleID); err != nil {
			return fmt.Errorf("mysql: find CUSTOMER role: %w", err)
		}

		res, err := tx.ExecContext(ctx,
			`INSERT INTO user_account (email, password_hash, role_id) VALUES (?, ?, ?)`,
			email, passwordHash, roleID,
		)
		if err != nil {
			if isDuplicateKeyError(err) {
				return app.ErrEmailAlreadyRegistered
			}
			return fmt.Errorf("mysql: insert user_account: %w", err)
		}
		userAccountID, err := res.LastInsertId()
		if err != nil {
			return fmt.Errorf("mysql: last insert id (user_account): %w", err)
		}

		customerRes, err := tx.ExecContext(ctx,
			`INSERT INTO customer (user_account_id, name, phone) VALUES (?, ?, ?)`,
			userAccountID, name, phone,
		)
		if err != nil {
			return fmt.Errorf("mysql: insert customer: %w", err)
		}
		customerID, err := customerRes.LastInsertId()
		if err != nil {
			return fmt.Errorf("mysql: last insert id (customer): %w", err)
		}

		cid := int(customerID)
		account = &domain.Account{
			ID: int(userAccountID), Email: email, PasswordHash: passwordHash,
			RoleID: roleID, RoleName: domain.RoleNameCustomer, IsActive: true,
			Name: name, CustomerID: &cid,
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	return account, nil
}

// CreateStaffAccount is CreateCustomer's admin-only counterpart: user_account + staff_profile
// instead of + customer, under whatever role the caller (AccountService, already having validated
// the role exists) specifies.
func (r *UserRepository) CreateStaffAccount(ctx context.Context, email, passwordHash, name string, roleID int) (*domain.Account, error) {
	var account *domain.Account
	err := dbx.WithTx(ctx, r.db, func(tx *sql.Tx) error {
		res, err := tx.ExecContext(ctx,
			`INSERT INTO user_account (email, password_hash, role_id) VALUES (?, ?, ?)`,
			email, passwordHash, roleID,
		)
		if err != nil {
			if isDuplicateKeyError(err) {
				return app.ErrEmailAlreadyRegistered
			}
			return fmt.Errorf("mysql: insert user_account: %w", err)
		}
		userAccountID, err := res.LastInsertId()
		if err != nil {
			return fmt.Errorf("mysql: last insert id (user_account): %w", err)
		}

		if _, err := tx.ExecContext(ctx,
			`INSERT INTO staff_profile (user_account_id, name) VALUES (?, ?)`,
			userAccountID, name,
		); err != nil {
			return fmt.Errorf("mysql: insert staff_profile: %w", err)
		}

		var roleName string
		if err := tx.QueryRowContext(ctx, `SELECT name FROM role WHERE role_id = ?`, roleID).Scan(&roleName); err != nil {
			return fmt.Errorf("mysql: find role %d name: %w", roleID, err)
		}

		account = &domain.Account{
			ID: int(userAccountID), Email: email, PasswordHash: passwordHash,
			RoleID: roleID, RoleName: roleName, IsActive: true, Name: name,
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	return account, nil
}

func (r *UserRepository) FindByEmail(ctx context.Context, email string) (*domain.Account, error) {
	row := r.db.QueryRowContext(ctx, accountSelectSQL+" WHERE ua.email = ?", email)
	acct, err := scanAccount(row)
	if err != nil {
		return nil, fmt.Errorf("mysql: find account by email: %w", err)
	}
	return acct, nil
}

func (r *UserRepository) FindByID(ctx context.Context, id int) (*domain.Account, error) {
	row := r.db.QueryRowContext(ctx, accountSelectSQL+" WHERE ua.user_account_id = ?", id)
	acct, err := scanAccount(row)
	if err != nil {
		return nil, fmt.Errorf("mysql: find account %d: %w", id, err)
	}
	return acct, nil
}

func (r *UserRepository) ListAccounts(ctx context.Context, page, pageSize int) ([]domain.Account, int, error) {
	var total int
	if err := r.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM user_account`).Scan(&total); err != nil {
		return nil, 0, fmt.Errorf("mysql: count accounts: %w", err)
	}

	offset := (page - 1) * pageSize
	rows, err := r.db.QueryContext(ctx, accountSelectSQL+` ORDER BY ua.user_account_id LIMIT ? OFFSET ?`, pageSize, offset)
	if err != nil {
		return nil, 0, fmt.Errorf("mysql: list accounts: %w", err)
	}
	defer rows.Close()

	var accounts []domain.Account
	for rows.Next() {
		acct, err := scanAccount(rows)
		if err != nil {
			return nil, 0, fmt.Errorf("mysql: scan account: %w", err)
		}
		accounts = append(accounts, *acct)
	}
	if err := rows.Err(); err != nil {
		return nil, 0, fmt.Errorf("mysql: list accounts: rows: %w", err)
	}
	return accounts, total, nil
}

func (r *UserRepository) CountByRoleName(ctx context.Context, roleName string) (int, error) {
	var count int
	err := r.db.QueryRowContext(ctx,
		`SELECT COUNT(*) FROM user_account ua JOIN role r ON r.role_id = ua.role_id WHERE r.name = ?`,
		roleName,
	).Scan(&count)
	if err != nil {
		return 0, fmt.Errorf("mysql: count accounts by role %q: %w", roleName, err)
	}
	return count, nil
}
