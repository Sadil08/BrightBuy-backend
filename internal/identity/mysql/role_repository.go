package mysql

import (
	"context"
	"database/sql"
	"errors"
	"fmt"

	"brightbuy-backend/internal/identity/app"
	"brightbuy-backend/internal/identity/domain"
	"brightbuy-backend/internal/shared/dbx"
)

type RoleRepository struct {
	db *sql.DB
}

func NewRoleRepository(db *sql.DB) *RoleRepository {
	return &RoleRepository{db: db}
}

func (r *RoleRepository) FindByID(ctx context.Context, id int) (*domain.Role, error) {
	var role domain.Role
	err := r.db.QueryRowContext(ctx, `SELECT role_id, name FROM role WHERE role_id = ?`, id).Scan(&role.ID, &role.Name)
	if err != nil {
		return nil, fmt.Errorf("mysql: find role %d: %w", id, err)
	}
	return &role, nil
}

func (r *RoleRepository) FindByName(ctx context.Context, name string) (*domain.Role, error) {
	var role domain.Role
	err := r.db.QueryRowContext(ctx, `SELECT role_id, name FROM role WHERE name = ?`, name).Scan(&role.ID, &role.Name)
	if err != nil {
		return nil, fmt.Errorf("mysql: find role %q: %w", name, err)
	}
	return &role, nil
}

// ResolvePermissionCodes is the RAW query — no ADMIN bypass here. That bypass lives one layer up,
// in identity/app's resolvePermissions (auth_service.go), specifically so it can be proven (by a
// fake repository that panics if this method is ever called for that role) that the bypass doesn't
// just happen to produce the right answer, it structurally never reaches this far for ADMIN.
func (r *RoleRepository) ResolvePermissionCodes(ctx context.Context, roleID int) ([]string, error) {
	rows, err := r.db.QueryContext(ctx,
		`SELECT p.code
		 FROM role_permission rp
		 JOIN permission p ON p.permission_id = rp.permission_id
		 WHERE rp.role_id = ?
		 ORDER BY p.code`,
		roleID,
	)
	if err != nil {
		return nil, fmt.Errorf("mysql: resolve permissions for role %d: %w", roleID, err)
	}
	defer rows.Close()

	codes := []string{} // non-nil on purpose — see catalog's toVariantAttributeDTOs for why a `nil`
	// slice here would be a real bug: this feeds straight into a JWT claim array, and `null` vs `[]`
	// matters just as much there as it does in an HTTP response body.
	for rows.Next() {
		var code string
		if err := rows.Scan(&code); err != nil {
			return nil, fmt.Errorf("mysql: scan permission code: %w", err)
		}
		codes = append(codes, code)
	}
	return codes, rows.Err()
}

// ListRolesWithPermissions fetches every role, then each one's permission codes — a per-role query
// rather than one giant join, acceptable here (unlike catalog's product list) because there are only
// ever 5 roles total, never a paginated collection that could grow.
func (r *RoleRepository) ListRolesWithPermissions(ctx context.Context) ([]domain.RoleWithPermissions, error) {
	rows, err := r.db.QueryContext(ctx, `SELECT role_id, name FROM role ORDER BY role_id`)
	if err != nil {
		return nil, fmt.Errorf("mysql: list roles: %w", err)
	}
	defer rows.Close()

	var roles []domain.RoleWithPermissions
	for rows.Next() {
		var role domain.Role
		if err := rows.Scan(&role.ID, &role.Name); err != nil {
			return nil, fmt.Errorf("mysql: scan role: %w", err)
		}
		roles = append(roles, domain.RoleWithPermissions{Role: role})
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("mysql: list roles: rows: %w", err)
	}

	for i := range roles {
		codes, err := r.ResolvePermissionCodes(ctx, roles[i].ID)
		if err != nil {
			return nil, err
		}
		roles[i].PermissionCodes = codes
	}
	return roles, nil
}

func (r *RoleRepository) ListPermissions(ctx context.Context) ([]domain.Permission, error) {
	rows, err := r.db.QueryContext(ctx, `SELECT permission_id, code, description FROM permission ORDER BY code`)
	if err != nil {
		return nil, fmt.Errorf("mysql: list permissions: %w", err)
	}
	defer rows.Close()

	var perms []domain.Permission
	for rows.Next() {
		var p domain.Permission
		if err := rows.Scan(&p.ID, &p.Code, &p.Description); err != nil {
			return nil, fmt.Errorf("mysql: scan permission: %w", err)
		}
		perms = append(perms, p)
	}
	return perms, rows.Err()
}

// SetRolePermissions replaces the FULL set of permissions a role holds, atomically: resolve every
// code to its permission_id first (failing the whole operation on any unknown code, before touching
// a single row — plan.md §6's "unknown permission code -> 400, not 500"), then delete-and-reinsert
// inside one transaction, so a reader never observes a role with PART of its new permission set.
func (r *RoleRepository) SetRolePermissions(ctx context.Context, roleID int, permissionCodes []string, grantedByUserID int) (*domain.RoleWithPermissions, error) {
	var result *domain.RoleWithPermissions
	err := dbx.WithTx(ctx, r.db, func(tx *sql.Tx) error {
		var role domain.Role
		err := tx.QueryRowContext(ctx, `SELECT role_id, name FROM role WHERE role_id = ?`, roleID).Scan(&role.ID, &role.Name)
		if err != nil {
			if errors.Is(err, sql.ErrNoRows) {
				return app.ErrUnknownRole
			}
			return fmt.Errorf("mysql: find role %d: %w", roleID, err)
		}

		permissionIDs := make([]int, 0, len(permissionCodes))
		for _, code := range permissionCodes {
			var id int
			err := tx.QueryRowContext(ctx, `SELECT permission_id FROM permission WHERE code = ?`, code).Scan(&id)
			if err != nil {
				if errors.Is(err, sql.ErrNoRows) {
					return app.ErrUnknownPermissionCode
				}
				return fmt.Errorf("mysql: find permission %q: %w", code, err)
			}
			permissionIDs = append(permissionIDs, id)
		}

		if _, err := tx.ExecContext(ctx, `DELETE FROM role_permission WHERE role_id = ?`, roleID); err != nil {
			return fmt.Errorf("mysql: clear role_permission for role %d: %w", roleID, err)
		}
		for _, permissionID := range permissionIDs {
			if _, err := tx.ExecContext(ctx,
				`INSERT INTO role_permission (role_id, permission_id, granted_by_user_id) VALUES (?, ?, ?)`,
				roleID, permissionID, grantedByUserID,
			); err != nil {
				return fmt.Errorf("mysql: insert role_permission: %w", err)
			}
		}

		result = &domain.RoleWithPermissions{Role: role, PermissionCodes: permissionCodes}
		return nil
	})
	if err != nil {
		return nil, err
	}
	return result, nil
}
