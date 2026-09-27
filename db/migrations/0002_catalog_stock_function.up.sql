DROP FUNCTION IF EXISTS fn_is_variant_in_stock;

DELIMITER $$

CREATE FUNCTION fn_is_variant_in_stock(p_variant_id INT)
RETURNS BOOLEAN
DETERMINISTIC
READS SQL DATA
BEGIN
    RETURN COALESCE(
        (
            SELECT stock_quantity > 0
            FROM product_variant
            WHERE variant_id = p_variant_id
        ),
        FALSE
    );
END$$

DELIMITER ;

/*This SQL code creates a database function called fn_is_variant_in_stock
 that checks whether a specific product variant has stock available. It takes
a variant_id as input and checks the stock_quantity in the product_variant table.
If the quantity is greater than 0, it returns TRUE, meaning the variant is in stock; 
otherwise, it returns FALSE. If the variant does not exist, COALESCE also makes the 
function return FALSE.*/