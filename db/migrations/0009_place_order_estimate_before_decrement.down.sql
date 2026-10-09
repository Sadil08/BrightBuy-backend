-- Restores sp_place_order exactly as 0008 defined it.
DROP PROCEDURE IF EXISTS sp_place_order;

CREATE PROCEDURE sp_place_order(
    IN p_customer_id INT,
    IN p_items_json JSON,
    IN p_delivery_mode VARCHAR(32) CHARACTER SET utf8mb4 COLLATE utf8mb4_0900_ai_ci,
    IN p_delivery_city_id INT,
    IN p_delivery_address VARCHAR(255),
    IN p_payment_method VARCHAR(8) CHARACTER SET utf8mb4 COLLATE utf8mb4_0900_ai_ci,
    IN p_idempotency_key VARCHAR(64) CHARACTER SET utf8mb4 COLLATE utf8mb4_0900_ai_ci,
    IN p_tax_amount DECIMAL(10,2),
    IN p_delivery_fee DECIMAL(10,2),
    IN p_provider_reference VARCHAR(100),
    IN p_card_last_four CHAR(4),
    OUT p_order_id INT,
    OUT p_created BOOLEAN
)
main: BEGIN
    DECLARE v_existing_order_id INT DEFAULT NULL;
    DECLARE v_subtotal DECIMAL(10,2) DEFAULT 0;
    DECLARE v_variant_id INT;
    DECLARE v_qty INT;
    DECLARE v_price DECIMAL(10,2);
    DECLARE v_stock INT;
    DECLARE v_done BOOLEAN DEFAULT FALSE;
    DECLARE v_not_found BOOLEAN DEFAULT FALSE;
    DECLARE v_estimated_days INT;
    DECLARE v_locked_customer_id INT;
    DECLARE cur CURSOR FOR
        SELECT jt.variant_id, SUM(jt.quantity)
        FROM JSON_TABLE(p_items_json, '$[*]' COLUMNS (
            variant_id INT PATH '$.variant_id',
            quantity INT PATH '$.quantity'
        )) AS jt
        GROUP BY jt.variant_id
        ORDER BY jt.variant_id ASC;
    DECLARE CONTINUE HANDLER FOR NOT FOUND SET v_done = TRUE;

    SET v_not_found = FALSE;
    BEGIN
        DECLARE CONTINUE HANDLER FOR NOT FOUND SET v_not_found = TRUE;
        SELECT customer_id INTO v_locked_customer_id
        FROM customer
        WHERE customer_id = p_customer_id
        FOR UPDATE;
    END;
    IF v_not_found THEN
        SIGNAL SQLSTATE '45000' SET MESSAGE_TEXT = 'CUSTOMER_NOT_FOUND', MYSQL_ERRNO = 4005;
    END IF;

    SELECT order_id INTO v_existing_order_id
    FROM `order`
    WHERE customer_id = p_customer_id AND idempotency_key = p_idempotency_key
    LIMIT 1;

    IF v_existing_order_id IS NOT NULL THEN
        SET p_order_id = v_existing_order_id;
        SET p_created = FALSE;
        LEAVE main;
    END IF;

    IF JSON_LENGTH(p_items_json) IS NULL OR JSON_LENGTH(p_items_json) = 0 THEN
        SIGNAL SQLSTATE '45000' SET MESSAGE_TEXT = 'EMPTY_ORDER', MYSQL_ERRNO = 4003;
    END IF;

    SET v_done = FALSE;
    OPEN cur;
    read_loop: LOOP
        FETCH cur INTO v_variant_id, v_qty;
        IF v_done THEN
            LEAVE read_loop;
        END IF;
        IF v_variant_id IS NULL OR v_variant_id <= 0 OR v_qty IS NULL OR v_qty <= 0 THEN
            SIGNAL SQLSTATE '45000' SET MESSAGE_TEXT = 'INVALID_ORDER_LINE', MYSQL_ERRNO = 4004;
        END IF;

        SET v_not_found = FALSE;
        BEGIN
            DECLARE CONTINUE HANDLER FOR NOT FOUND SET v_not_found = TRUE;
            SELECT price, stock_quantity INTO v_price, v_stock
            FROM product_variant
            WHERE variant_id = v_variant_id AND is_active = TRUE
            FOR UPDATE;
        END;
        IF v_not_found OR v_stock < v_qty THEN
            CLOSE cur;
            SIGNAL SQLSTATE '45000' SET MESSAGE_TEXT = 'STOCK_EXCEEDED', MYSQL_ERRNO = 4001;
        END IF;

        UPDATE product_variant
        SET stock_quantity = stock_quantity - v_qty
        WHERE variant_id = v_variant_id;

        SET v_subtotal = v_subtotal + (v_price * v_qty);
    END LOOP;
    CLOSE cur;

    SET v_estimated_days =
        fn_estimate_delivery_days(p_delivery_mode, p_delivery_city_id, p_items_json);

    INSERT INTO `order` (
        customer_id, status, subtotal, tax_amount, delivery_fee, total_amount,
        idempotency_key, order_date
    ) VALUES (
        p_customer_id, 'Confirmed', v_subtotal, p_tax_amount, p_delivery_fee,
        v_subtotal + p_tax_amount + p_delivery_fee, p_idempotency_key, UTC_TIMESTAMP()
    );
    SET p_order_id = LAST_INSERT_ID();

    INSERT INTO order_item (order_id, variant_id, quantity, unit_price_at_order)
    SELECT p_order_id, jt.variant_id, SUM(jt.quantity), pv.price
    FROM JSON_TABLE(p_items_json, '$[*]' COLUMNS (
        variant_id INT PATH '$.variant_id',
        quantity INT PATH '$.quantity'
    )) AS jt
    JOIN product_variant pv ON pv.variant_id = jt.variant_id
    GROUP BY jt.variant_id, pv.price;

    INSERT INTO stock_movement
        (variant_id, change_qty, reason, related_order_id, acting_user_id, created_at)
    SELECT jt.variant_id, -SUM(jt.quantity), 'Order placed', p_order_id, NULL, UTC_TIMESTAMP()
    FROM JSON_TABLE(p_items_json, '$[*]' COLUMNS (
        variant_id INT PATH '$.variant_id',
        quantity INT PATH '$.quantity'
    )) AS jt
    GROUP BY jt.variant_id;

    INSERT INTO delivery (order_id, mode, city_id, address, estimated_days, estimated_date)
    VALUES (
        p_order_id, p_delivery_mode, p_delivery_city_id, p_delivery_address,
        v_estimated_days, DATE_ADD(CURDATE(), INTERVAL v_estimated_days DAY)
    );

    INSERT INTO payment
        (order_id, method, status, amount, provider_reference, card_last_four, created_at)
    VALUES (
        p_order_id, p_payment_method,
        IF(BINARY p_payment_method = BINARY 'COD', 'Pending', 'Authorized'),
        v_subtotal + p_tax_amount + p_delivery_fee,
        p_provider_reference, p_card_last_four, UTC_TIMESTAMP()
    );

    INSERT INTO order_status_history (order_id, status, changed_by_user_id, changed_at)
    VALUES (p_order_id, 'Confirmed', NULL, UTC_TIMESTAMP());
    SET p_created = TRUE;
END;
