ALTER TABLE customer
    DROP FOREIGN KEY fk_customer_user,
    DROP INDEX uq_customer_user,
    DROP COLUMN name,
    DROP COLUMN user_account_id;

DROP TABLE IF EXISTS user_account;
DROP TABLE IF EXISTS role;
