CREATE TABLE city (
    city_id INT AUTO_INCREMENT PRIMARY KEY,
    name VARCHAR(100) NOT NULL UNIQUE,
    classification ENUM('Main', 'Other') NOT NULL DEFAULT 'Other'
) ENGINE=InnoDB
  DEFAULT CHARACTER SET utf8mb4
  COLLATE utf8mb4_0900_ai_ci;

CREATE TABLE app_config (
    config_key VARCHAR(64) PRIMARY KEY,
    config_value VARCHAR(255) NOT NULL
) ENGINE=InnoDB
  DEFAULT CHARACTER SET utf8mb4
  COLLATE utf8mb4_0900_ai_ci;

CREATE TABLE `order` (
    order_id INT AUTO_INCREMENT PRIMARY KEY,
    customer_id INT NOT NULL,
    status ENUM(
        'Placed', 'Confirmed', 'Processing', 'Shipped', 'ReadyForPickup',
        'Delivered', 'Completed', 'Cancelled'
    ) NOT NULL DEFAULT 'Confirmed',
    subtotal DECIMAL(10,2) NOT NULL,
    tax_amount DECIMAL(10,2) NOT NULL,
    delivery_fee DECIMAL(10,2) NOT NULL,
    total_amount DECIMAL(10,2) NOT NULL,
    idempotency_key VARCHAR(64) NOT NULL,
    order_date DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
    CONSTRAINT fk_order_customer
        FOREIGN KEY (customer_id) REFERENCES customer(customer_id),
    CONSTRAINT uq_order_idempotency
        UNIQUE (customer_id, idempotency_key),
    INDEX ix_order_customer (customer_id),
    INDEX ix_order_date (order_date)
) ENGINE=InnoDB
  DEFAULT CHARACTER SET utf8mb4
  COLLATE utf8mb4_0900_ai_ci;

CREATE TABLE order_item (
    order_item_id INT AUTO_INCREMENT PRIMARY KEY,
    order_id INT NOT NULL,
    variant_id INT NOT NULL,
    quantity INT NOT NULL,
    unit_price_at_order DECIMAL(10,2) NOT NULL,
    CONSTRAINT fk_order_item_order
        FOREIGN KEY (order_id) REFERENCES `order`(order_id) ON DELETE CASCADE,
    CONSTRAINT fk_order_item_variant
        FOREIGN KEY (variant_id) REFERENCES product_variant(variant_id),
    INDEX ix_order_item_order (order_id),
    INDEX ix_order_item_variant (variant_id)
) ENGINE=InnoDB
  DEFAULT CHARACTER SET utf8mb4
  COLLATE utf8mb4_0900_ai_ci;

CREATE TABLE delivery (
    delivery_id INT AUTO_INCREMENT PRIMARY KEY,
    order_id INT NOT NULL,
    mode ENUM('StorePickup', 'StandardDelivery') NOT NULL,
    city_id INT NULL,
    address VARCHAR(255) NULL,
    estimated_days INT NOT NULL,
    estimated_date DATE NOT NULL,
    CONSTRAINT fk_delivery_order
        FOREIGN KEY (order_id) REFERENCES `order`(order_id) ON DELETE CASCADE,
    CONSTRAINT fk_delivery_city
        FOREIGN KEY (city_id) REFERENCES city(city_id),
    CONSTRAINT uq_delivery_order UNIQUE (order_id)
) ENGINE=InnoDB
  DEFAULT CHARACTER SET utf8mb4
  COLLATE utf8mb4_0900_ai_ci;

CREATE TABLE stock_movement (
    stock_movement_id INT AUTO_INCREMENT PRIMARY KEY,
    variant_id INT NOT NULL,
    change_qty INT NOT NULL,
    reason VARCHAR(255) NOT NULL,
    related_order_id INT NULL,
    acting_user_id INT NULL,
    created_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
    CONSTRAINT fk_stock_movement_variant
        FOREIGN KEY (variant_id) REFERENCES product_variant(variant_id),
    CONSTRAINT fk_stock_movement_order
        FOREIGN KEY (related_order_id) REFERENCES `order`(order_id),
    CONSTRAINT fk_stock_movement_user
        FOREIGN KEY (acting_user_id) REFERENCES user_account(user_account_id),
    INDEX ix_stock_movement_variant (variant_id),
    INDEX ix_stock_movement_order (related_order_id)
) ENGINE=InnoDB
  DEFAULT CHARACTER SET utf8mb4
  COLLATE utf8mb4_0900_ai_ci;

CREATE TABLE order_status_history (
    order_status_history_id INT AUTO_INCREMENT PRIMARY KEY,
    order_id INT NOT NULL,
    status ENUM(
        'Placed', 'Confirmed', 'Processing', 'Shipped', 'ReadyForPickup',
        'Delivered', 'Completed', 'Cancelled'
    ) NOT NULL,
    changed_by_user_id INT NULL,
    changed_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
    CONSTRAINT fk_order_status_history_order
        FOREIGN KEY (order_id) REFERENCES `order`(order_id) ON DELETE CASCADE,
    CONSTRAINT fk_order_status_history_user
        FOREIGN KEY (changed_by_user_id) REFERENCES user_account(user_account_id),
    INDEX ix_order_status_history_order (order_id)
) ENGINE=InnoDB
  DEFAULT CHARACTER SET utf8mb4
  COLLATE utf8mb4_0900_ai_ci;

CREATE TABLE payment (
    payment_id INT AUTO_INCREMENT PRIMARY KEY,
    order_id INT NOT NULL,
    method ENUM('COD', 'Card') NOT NULL,
    status ENUM('Pending', 'Authorized', 'Paid', 'Failed', 'Refunded') NOT NULL,
    amount DECIMAL(10,2) NOT NULL,
    provider_reference VARCHAR(100) NULL,
    card_last_four CHAR(4) NULL,
    created_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
    CONSTRAINT fk_payment_order
        FOREIGN KEY (order_id) REFERENCES `order`(order_id) ON DELETE CASCADE,
    CONSTRAINT uq_payment_order UNIQUE (order_id)
) ENGINE=InnoDB
  DEFAULT CHARACTER SET utf8mb4
  COLLATE utf8mb4_0900_ai_ci;
