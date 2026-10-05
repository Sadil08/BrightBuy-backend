-- Dev/test-only seed data for 02-auth (plan.md §2.2) — NEVER run against production. The real
-- production path for the very first ADMIN account is `go run ./cmd/api --create-first-admin`
-- (cmd/api/create_first_admin.go), which generates a random password and refuses to run if any
-- ADMIN already exists. This script instead creates a KNOWN, fixed dev password, matching
-- db/seed/catalog.sql's own established convention (deterministic and idempotent, not random) — a
-- re-runnable local setup needs to log back in with the SAME credentials every time, not a fresh
-- random one per run.
--
-- Password: DevAdmin123! — dev-only, meaningless outside a local/test database, intentionally
-- documented here rather than hidden, since there is nothing sensitive to protect in a value that
-- only ever unlocks a throwaway local container's data.

START TRANSACTION;

-- No explicit user_account_id, unlike catalog.sql's products/categories: catalog seed data is the
-- ONLY source of product rows, so forcing specific IDs is safe. Here, a database that already ran
-- `--create-first-admin` (or this script before) already has accounts with real, unpredictable
-- auto-increment IDs — forcing id=1 could collide with and silently overwrite a DIFFERENT, already-
-- real admin account. Keying this script's own idempotency off the UNIQUE email instead means it's
-- safe to run regardless of what else already exists.
INSERT INTO user_account (email, password_hash, role_id, is_active)
SELECT 'admin@brightbuy.test',
       '$2a$12$Y4Bo0kWM4Ck3Y3qfcUKHae6Jp/hZx3StmyEKycgLXPtE8V1VTTGRq', -- bcrypt("DevAdmin123!", cost 12)
       role_id, TRUE
FROM role WHERE name = 'ADMIN'
ON DUPLICATE KEY UPDATE
    password_hash = VALUES(password_hash),
    is_active = VALUES(is_active);

INSERT INTO staff_profile (user_account_id, name)
SELECT user_account_id, 'Dev Administrator'
FROM user_account WHERE email = 'admin@brightbuy.test'
ON DUPLICATE KEY UPDATE name = VALUES(name);

COMMIT;
