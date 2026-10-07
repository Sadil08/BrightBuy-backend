CREATE TABLE role (
    role_id INT AUTO_INCREMENT PRIMARY KEY,
    name VARCHAR(30) NOT NULL UNIQUE
) ENGINE=InnoDB;

INSERT INTO role (name) VALUES ('CUSTOMER');

CREATE TABLE user_account (
    user_account_id INT AUTO_INCREMENT PRIMARY KEY,
    email VARCHAR(255) NOT NULL,
    password_hash VARCHAR(255) NOT NULL,
    role_id INT NOT NULL,
    is_active BOOLEAN NOT NULL DEFAULT TRUE,
    created_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
    CONSTRAINT fk_user_account_role
        FOREIGN KEY (role_id) REFERENCES role(role_id),
    CONSTRAINT uq_user_account_email UNIQUE (email)
) ENGINE=InnoDB
  DEFAULT CHARACTER SET utf8mb4
  COLLATE utf8mb4_0900_ai_ci;

ALTER TABLE customer
    ADD COLUMN user_account_id INT NULL,
    ADD COLUMN name VARCHAR(100) NOT NULL DEFAULT '',
    ADD CONSTRAINT uq_customer_user UNIQUE (user_account_id),
    ADD CONSTRAINT fk_customer_user
        FOREIGN KEY (user_account_id) REFERENCES user_account(user_account_id)
        ON DELETE CASCADE;
