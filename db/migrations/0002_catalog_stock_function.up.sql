-- fn_is_variant_in_stock (specs/global/07_SQL_DATABASE_STANDARDS.md §5): derives In Stock/Out of
-- Stock at query time from stock_quantity, rather than storing a redundant flag that could drift.
-- COALESCE(..., FALSE) means an unknown variant_id reads as "out of stock" instead of NULL, matching
-- plan.md §6's rule that a data issue should fail safe, not error.
--
-- No `DELIMITER $$` here on purpose: DELIMITER is a `mysql` command-line-client instruction that
-- tells THAT CLIENT to stop splitting input on `;` until it sees `$$` instead — it lets you type a
-- CREATE FUNCTION body (which contains its own semicolons) as one block when typing interactively.
-- It is not real SQL and MySQL's server has never heard of it. golang-migrate sends this file's
-- contents straight to the server as a query (not through the `mysql` CLI), and the server already
-- knows how to parse a semicolon inside a CREATE FUNCTION/PROCEDURE body correctly — DELIMITER would
-- just be sent as a literal statement and rejected with a syntax error, which is exactly what
-- happened before this fix.
DROP FUNCTION IF EXISTS fn_is_variant_in_stock;

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
END;