CREATE TABLE cart(
    cart_id INT AUTO_INCREMENT PRIMARY KEY,
    customer_id INT NOT NULL,

    updated_at DATETIME NOT NULL             -- THS STORES THE LAST TIME THE CART WAS UPDATED

    DEFAULT CURRENT_TIMESTAMP ON UPDATE CURRENT_TIMESTAMP,

    CONSTRAINT fk_cart_customer
      FOREIGN KEY (customer_id)
      REFERENCES customer(customer_id)      -- This establishes a foreign key relationship with the customer table, ensuring that each cart is associated with a valid customer. The ON DELETE CASCADE option means that if a customer is deleted, their associated cart will also be automatically deleted.
      ON DELETE CASCADE,

    CONSTRAINT uq_cart_customer UNIQUE (customer_id)
) ENGINE=InnoDB;


CREATE TABLE cart_item(

    cart_item_id INT AUTO_INCREMENT PRIMARY KEY,  
    cart_id INT NOT NULL,
    variant_id INT NOT NULL,
    quantity INT NOT NULL,

    CONSTRAINT fk_cart_item_cart  -- create foreign key constraint for cart_item table referencing cart table
      FOREIGN KEY (cart_id)
      REFERENCES cart(cart_id)
      ON DELETE CASCADE,


    CONSTRAINT fk_cart_item_variant   -- create foreign key constraint for cart_item table referencing product_variant table
      FOREIGN KEY (variant_id)
      REFERENCES product_variant(variant_id)
      ON DELETE CASCADE,

    
    CONSTRAINT uq_cart_item UNIQUE (cart_id, variant_id),  -- This constraint ensures that each cart can only have one entry for a specific product variant, preventing duplicate entries for the same variant in the same cart.

    CONSTRAINT chk_cart_item_quantity CHECK (quantity >= 1),

)ENGINE =InnoDB;

  
