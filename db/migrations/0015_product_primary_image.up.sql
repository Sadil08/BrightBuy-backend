-- Staff choose which uploaded image represents a product on the home page and the shop grid.
-- NULL means "no explicit choice", and the oldest image is used.
ALTER TABLE product
    ADD COLUMN primary_image_id BIGINT NULL,
    ADD CONSTRAINT fk_product_primary_image
        FOREIGN KEY (primary_image_id) REFERENCES product_image(image_id)
        ON DELETE SET NULL;
