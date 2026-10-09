-- 02-auth's schema (plan.md §2) plus the RBAC tables 07_SQL_DATABASE_STANDARDS.md §1a already
-- specifies (permission/role_permission — not redefined there, just referenced; this is their one
-- authoritative DDL). Named 0004, not plan.md's stated 0002_identity_schema: 0002/0003 were already
-- taken by 01-catalog's stock function and product_variant.updated_at by the time this was built —
-- plan.md's number assumed a build order that didn't end up matching reality.
--
-- Table creation order matters here for foreign keys: role and user_account must exist before
-- anything references them; role_permission references BOTH role and user_account
-- (granted_by_user_id), so it's created after both.

CREATE TABLE role (
    role_id INT AUTO_INCREMENT PRIMARY KEY,
    name    VARCHAR(30) NOT NULL UNIQUE          -- CUSTOMER, WAREHOUSE_STAFF, ORDER_MANAGER, MANAGER, ADMIN
);

CREATE TABLE user_account (
    user_account_id INT AUTO_INCREMENT PRIMARY KEY,
    email           VARCHAR(255) NOT NULL,
    password_hash   VARCHAR(255) NOT NULL,        -- BCrypt, cost >= 12 (REQ-2.3)
    role_id         INT NOT NULL,
    is_active       BOOLEAN NOT NULL DEFAULT TRUE,
    created_at      DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
    FOREIGN KEY (role_id) REFERENCES role(role_id),
    -- REQ-2.2's uniqueness is case-insensitive "for free": this database's default collation is
    -- utf8mb4_0900_ai_ci (confirmed directly against the running server, not assumed) — the `_ai_ci`
    -- suffix means accent- and case-insensitive comparison, so a plain UNIQUE index here already
    -- rejects "A@x.com" as a duplicate of "a@x.com" with no extra COLLATE clause needed.
    UNIQUE KEY uq_user_account_email (email)
);

-- The ER diagram's single "Customer" entity (id, name, email, phone, address) is split in two:
-- user_account carries login mechanics (shared by every role); customer carries the
-- customer-specific profile. DB-ERD-1 (09_ISSUES_REGISTER.md): the diagram never modeled auth at
-- all, so this split follows the SRS's *text* requirements, not a diagram to stay literally
-- consistent with.
CREATE TABLE customer (
    customer_id     INT AUTO_INCREMENT PRIMARY KEY,
    user_account_id INT NOT NULL,
    name            VARCHAR(150) NOT NULL,
    phone           VARCHAR(30) NOT NULL,
    address         VARCHAR(255) NULL,             -- required later at checkout for Standard Delivery, not at registration
    FOREIGN KEY (user_account_id) REFERENCES user_account(user_account_id) ON DELETE CASCADE,
    UNIQUE KEY uq_customer_user_account (user_account_id)
);

CREATE TABLE staff_profile (
    staff_profile_id INT AUTO_INCREMENT PRIMARY KEY,
    user_account_id  INT NOT NULL,
    name             VARCHAR(150) NOT NULL,
    FOREIGN KEY (user_account_id) REFERENCES user_account(user_account_id) ON DELETE CASCADE,
    UNIQUE KEY uq_staff_profile_user_account (user_account_id)
);

-- RBAC-1 (02_SECURITY_BASELINE.md §1): the capability catalog. Seeded once below; new capabilities
-- added later are new rows here (a migration), but which role HAS which permission is runtime-
-- editable data, changed through PUT /admin/roles/{roleId}/permissions, never a code deploy.
CREATE TABLE permission (
    permission_id INT AUTO_INCREMENT PRIMARY KEY,
    code          VARCHAR(64)  NOT NULL,
    description   VARCHAR(255) NOT NULL,
    UNIQUE KEY uq_permission_code (code)
);

CREATE TABLE role_permission (
    role_id            INT NOT NULL,
    permission_id      INT NOT NULL,
    granted_at         DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
    granted_by_user_id INT NULL,                   -- which ADMIN made this change (audit, 02 §7)
    PRIMARY KEY (role_id, permission_id),
    FOREIGN KEY (role_id)       REFERENCES role(role_id)             ON DELETE CASCADE,
    FOREIGN KEY (permission_id) REFERENCES permission(permission_id) ON DELETE CASCADE
);
-- ADMIN's rows exist so the admin console can DISPLAY what ADMIN can do — enforcement never reads
-- them for that role; shared/auth's RequirePermission short-circuits on role == ADMIN unconditionally
-- (SEC-AUTH-1), so revoking every row here for ADMIN still leaves every admin able to act.

CREATE TABLE refresh_token (
    refresh_token_id INT AUTO_INCREMENT PRIMARY KEY,
    user_account_id  INT NOT NULL,
    token_hash       VARCHAR(255) NOT NULL,        -- SHA-256 of the opaque token; raw value never stored (SEC-AUTH-2)
    expires_at       DATETIME NOT NULL,
    revoked_at       DATETIME NULL,                -- REQ-2.5 "log out everywhere": set on logout
    created_at       DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
    FOREIGN KEY (user_account_id) REFERENCES user_account(user_account_id) ON DELETE CASCADE
);
CREATE INDEX ix_refresh_token_user ON refresh_token(user_account_id);
CREATE INDEX ix_refresh_token_hash ON refresh_token(token_hash);

-- Seed data (plan.md §2.1) — required for the app to function at all, not sample/demo data, so it
-- ships in the migration itself rather than db/seed (which is dev/test-only and never runs in prod).
INSERT INTO role (name) VALUES ('CUSTOMER'), ('WAREHOUSE_STAFF'), ('ORDER_MANAGER'), ('MANAGER'), ('ADMIN');

INSERT INTO permission (code, description) VALUES
  ('catalog:write',        'Create/update/deactivate products, variants, categories'),
  ('catalog:image:write',  'Upload/replace/delete product images'),
  ('stock:adjust',         'Manually adjust variant stock quantity'),
  ('order:status:update',  'Advance or cancel an order''s status'),
  ('order:cancel',         'Cancel an order and return its reserved stock'),
  ('reports:view',         'View the five management reports'),
  ('account:manage_roles', 'View/edit which permissions a role holds'),
  ('account:manage_users', 'Create staff/manager/admin accounts');

-- Default grants (02_SECURITY_BASELINE.md §1 table) — admin-editable after this point via
-- PUT /admin/roles/{roleId}/permissions, never by editing this migration again.
INSERT INTO role_permission (role_id, permission_id)
SELECT r.role_id, p.permission_id FROM role r JOIN permission p ON (
    (r.name = 'WAREHOUSE_STAFF' AND p.code IN ('catalog:write','catalog:image:write','stock:adjust'))
 OR (r.name = 'ORDER_MANAGER'   AND p.code IN ('order:status:update','order:cancel'))
 OR (r.name = 'MANAGER'         AND p.code = 'reports:view')
 OR (r.name = 'ADMIN')                                         -- all, for console display only (SEC-AUTH-1)
);
