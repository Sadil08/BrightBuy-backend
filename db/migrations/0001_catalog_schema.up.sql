CREATE TABLE category (
    category_id INT AUTO_INCREMENT PRIMARY KEY,
    name VARCHAR(100) NOT NULL,
    description VARCHAR(500) NULL,
    is_active BOOLEAN NOT NULL DEFAULT TRUE,
    UNIQUE KEY uq_category_name (name)
) ENGINE=InnoDB
  DEFAULT CHARACTER SET utf8mb4
  COLLATE utf8mb4_0900_ai_ci;

CREATE TABLE product (
    product_id INT AUTO_INCREMENT PRIMARY KEY,
    name VARCHAR(200) NOT NULL,
    description TEXT NULL,
    is_active BOOLEAN NOT NULL DEFAULT TRUE,
    created_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
    updated_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP
        ON UPDATE CURRENT_TIMESTAMP
) ENGINE=InnoDB
  DEFAULT CHARACTER SET utf8mb4
  COLLATE utf8mb4_0900_ai_ci;

CREATE FULLTEXT INDEX ft_product_name_description
    ON product(name, description);

CREATE TABLE product_category (
    product_id INT NOT NULL,
    category_id INT NOT NULL,
    PRIMARY KEY (product_id, category_id),
    CONSTRAINT fk_product_category_product
        FOREIGN KEY (product_id) REFERENCES product(product_id)
        ON DELETE CASCADE,
    CONSTRAINT fk_product_category_category
        FOREIGN KEY (category_id) REFERENCES category(category_id)
        ON DELETE CASCADE
) ENGINE=InnoDB;

CREATE INDEX ix_product_category_category
    ON product_category(category_id);

CREATE TABLE product_variant (
    variant_id INT AUTO_INCREMENT PRIMARY KEY,
    product_id INT NOT NULL,
    sku VARCHAR(64) NOT NULL,
    price DECIMAL(10,2) NOT NULL,
    stock_quantity INT NOT NULL DEFAULT 0,
    is_active BOOLEAN NOT NULL DEFAULT TRUE,
    CONSTRAINT fk_product_variant_product
        FOREIGN KEY (product_id) REFERENCES product(product_id)
        ON DELETE CASCADE,
    CONSTRAINT uq_product_variant_sku UNIQUE (sku),
    CONSTRAINT chk_product_variant_price CHECK (price >= 0),
    CONSTRAINT chk_product_variant_stock CHECK (stock_quantity >= 0)
) ENGINE=InnoDB;

CREATE TABLE attribute_name (
    attribute_name_id INT AUTO_INCREMENT PRIMARY KEY,
    name VARCHAR(50) NOT NULL,
    CONSTRAINT uq_attribute_name UNIQUE (name)
) ENGINE=InnoDB
  DEFAULT CHARACTER SET utf8mb4
  COLLATE utf8mb4_0900_ai_ci;

CREATE TABLE attribute_value (
    attribute_value_id INT AUTO_INCREMENT PRIMARY KEY,
    attribute_name_id INT NOT NULL,
    value VARCHAR(100) NOT NULL,
    CONSTRAINT fk_attribute_value_name
        FOREIGN KEY (attribute_name_id)
        REFERENCES attribute_name(attribute_name_id)
        ON DELETE CASCADE,
    CONSTRAINT uq_attribute_name_value
        UNIQUE (attribute_name_id, value)
) ENGINE=InnoDB
  DEFAULT CHARACTER SET utf8mb4
  COLLATE utf8mb4_0900_ai_ci;

CREATE TABLE variant_attribute (
    variant_id INT NOT NULL,
    attribute_value_id INT NOT NULL,
    PRIMARY KEY (variant_id, attribute_value_id),
    CONSTRAINT fk_variant_attribute_variant
        FOREIGN KEY (variant_id) REFERENCES product_variant(variant_id)
        ON DELETE CASCADE,
    CONSTRAINT fk_variant_attribute_value
        FOREIGN KEY (attribute_value_id)
        REFERENCES attribute_value(attribute_value_id)
        ON DELETE CASCADE
) ENGINE=InnoDB;