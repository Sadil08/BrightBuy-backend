DROP PROCEDURE IF EXISTS sp_cancel_order;
DROP TRIGGER IF EXISTS trg_order_status_history;
DROP TRIGGER IF EXISTS trg_order_status_transition;

CREATE TRIGGER trg_order_status_transition
BEFORE UPDATE ON `order`
FOR EACH ROW
BEGIN
    IF NEW.status <> OLD.status AND NOT (
        (OLD.status = 'Placed' AND NEW.status IN ('Confirmed', 'Cancelled')) OR
        (OLD.status = 'Confirmed' AND NEW.status IN ('Processing', 'Cancelled')) OR
        (OLD.status = 'Processing' AND NEW.status IN ('Shipped', 'ReadyForPickup', 'Cancelled')) OR
        (OLD.status IN ('Shipped', 'ReadyForPickup') AND NEW.status IN ('Delivered', 'Cancelled')) OR
        (OLD.status = 'Delivered' AND NEW.status = 'Completed')
    ) THEN
        SIGNAL SQLSTATE '45000'
            SET MESSAGE_TEXT = 'INVALID_ORDER_STATUS_TRANSITION', MYSQL_ERRNO = 4010;
    END IF;
END;

CREATE TRIGGER trg_order_status_history
AFTER UPDATE ON `order`
FOR EACH ROW
BEGIN
    IF NEW.status <> OLD.status THEN
        INSERT INTO order_status_history (order_id, status, changed_by_user_id, changed_at)
        VALUES (NEW.order_id, NEW.status, NULLIF(@acting_user_id, 0), UTC_TIMESTAMP());
    END IF;
END;

CREATE PROCEDURE sp_cancel_order(
    IN p_order_id INT,
    IN p_acting_user_id INT
)
SQL SECURITY INVOKER
BEGIN
    DECLARE v_order_id INT DEFAULT NULL;

    SET @acting_user_id = p_acting_user_id;

    SELECT order_id INTO v_order_id
      FROM `order`
     WHERE order_id = p_order_id
     FOR UPDATE;

    IF v_order_id IS NULL THEN
        SIGNAL SQLSTATE '45000'
            SET MESSAGE_TEXT = 'ORDER_NOT_FOUND', MYSQL_ERRNO = 4011;
    END IF;

    UPDATE `order` SET status = 'Cancelled' WHERE order_id = p_order_id;

    INSERT INTO stock_movement
        (variant_id, change_qty, reason, related_order_id, acting_user_id, created_at)
    SELECT variant_id, quantity, 'Order cancelled', p_order_id,
           NULLIF(p_acting_user_id, 0), UTC_TIMESTAMP()
      FROM order_item
     WHERE order_id = p_order_id;

    UPDATE product_variant pv
    JOIN order_item oi ON oi.variant_id = pv.variant_id
       SET pv.stock_quantity = pv.stock_quantity + oi.quantity
     WHERE oi.order_id = p_order_id;
END;
