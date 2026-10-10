CREATE TABLE product_image (
    image_id BIGINT AUTO_INCREMENT PRIMARY KEY,
    product_id INT NOT NULL,
    object_key VARCHAR(512) NOT NULL,
    content_type VARCHAR(100) NOT NULL,
    byte_size BIGINT NOT NULL,
    width INT NOT NULL,
    height INT NOT NULL,
    created_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
    CONSTRAINT fk_product_image_product
        FOREIGN KEY (product_id) REFERENCES product(product_id)
        ON DELETE CASCADE,
    CONSTRAINT uq_product_image_object_key UNIQUE (object_key),
    CONSTRAINT chk_product_image_byte_size CHECK (byte_size > 0),
    CONSTRAINT chk_product_image_dimensions CHECK (width > 0 AND height > 0)
) ENGINE=InnoDB;

CREATE INDEX ix_product_image_product
    ON product_image(product_id);
