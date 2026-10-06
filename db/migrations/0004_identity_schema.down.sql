-- Reverse FK dependency order from the up migration.
DROP TABLE IF EXISTS refresh_token;
DROP TABLE IF EXISTS role_permission;
DROP TABLE IF EXISTS permission;
DROP TABLE IF EXISTS staff_profile;
DROP TABLE IF EXISTS customer;
DROP TABLE IF EXISTS user_account;
DROP TABLE IF EXISTS role;
