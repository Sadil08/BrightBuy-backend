DROP PROCEDURE IF EXISTS sp_adjust_stock;

DELIMITER $$

--This is a MySQL stored procedure for adjusting product stock
CREATE PROCEDURE sp_adjust_stock(
    IN p_variant_id INT,
    IN p_delta INT,
    IN p_reason VARCHAR(255),
    IN p_acting_user_id INT
)
SQL SECURITY INVOKER
BEGIN
    DECLARE v_stock INT;

    SELECT stock_quantity
      INTO v_stock
      FROM product_variant
     WHERE variant_id = p_variant_id
     FOR UPDATE;

    IF v_stock IS NULL THEN
        SIGNAL SQLSTATE '45000'
            SET MESSAGE_TEXT = 'VARIANT_NOT_FOUND',
                MYSQL_ERRNO = 4001;
    END IF;

    IF v_stock + p_delta < 0 THEN
        SIGNAL SQLSTATE '45000'
            SET MESSAGE_TEXT = 'ADJUSTMENT_BELOW_ZERO',
                MYSQL_ERRNO = 4002;
    END IF;

    UPDATE product_variant
       SET stock_quantity = stock_quantity + p_delta
     WHERE variant_id = p_variant_id;

    INSERT INTO stock_movement
        (variant_id, change_qty, reason, acting_user_id, created_at)
    VALUES
        (p_variant_id, p_delta, p_reason, p_acting_user_id, UTC_TIMESTAMP());
END$$

DELIMITER ;