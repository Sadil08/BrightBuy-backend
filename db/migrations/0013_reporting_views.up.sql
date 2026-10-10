/*
   Management Reporting views.

   The current backend uses snake_case table names. The operational order/payment/delivery
   migrations are owned by their respective modules; this migration intentionally contains
   reporting views only and never creates or mutates operational rows.

   The operational table is `order` (backticked: ORDER is a MySQL reserved word).
*/

CREATE OR REPLACE VIEW vw_quarterly_sales AS
SELECT
    o.order_id,
    o.order_date,
    YEAR(o.order_date) AS report_year,
    QUARTER(o.order_date) AS report_quarter,
    (
        COALESCE(o.total_amount, 0)
        - COALESCE(o.tax_amount, 0)
        - COALESCE(o.delivery_fee, 0)
    ) AS sales_value
FROM `order` o
WHERE o.status <> 'Cancelled';

CREATE OR REPLACE VIEW vw_top_selling_products AS
SELECT
    o.order_date,
    pv.product_id,
    p.name AS product_name,
    oi.variant_id,
    pv.sku,
    oi.quantity AS quantity_sold,
    (oi.quantity * oi.unit_price_at_order) AS line_revenue
FROM `order` o
JOIN order_item oi ON oi.order_id = o.order_id
JOIN product_variant pv ON pv.variant_id = oi.variant_id
JOIN product p ON p.product_id = pv.product_id
WHERE o.status <> 'Cancelled';

CREATE OR REPLACE VIEW vw_category_wise_orders AS
SELECT DISTINCT
    o.order_id,
    o.order_date,
    c.category_id,
    c.name AS category_name
FROM `order` o
JOIN order_item oi ON oi.order_id = o.order_id
JOIN product_variant pv ON pv.variant_id = oi.variant_id
JOIN product_category pc ON pc.product_id = pv.product_id
JOIN category c ON c.category_id = pc.category_id
WHERE o.status <> 'Cancelled';

CREATE OR REPLACE VIEW vw_upcoming_deliveries AS
SELECT
    o.order_id,
    c.name AS customer_name,
    ua.email AS customer_email,
    d.mode AS delivery_mode,
    d.address,
    d.estimated_date,
    o.status AS order_status
FROM `order` o
JOIN customer c ON c.customer_id = o.customer_id
JOIN user_account ua ON ua.user_account_id = c.user_account_id
JOIN delivery d ON d.order_id = o.order_id
WHERE o.status IN ('Placed', 'Confirmed', 'Processing', 'Shipped', 'ReadyForPickup')
  AND d.estimated_date >= CURRENT_DATE();

CREATE OR REPLACE VIEW vw_customer_order_summary AS
SELECT
    c.customer_id,
    c.name AS customer_name,
    ua.email AS customer_email,
    o.order_id,
    o.order_date,
    o.status AS order_status,
    CASE
        WHEN o.status = 'Cancelled' THEN 0
        ELSE (
            COALESCE(o.total_amount, 0)
            - COALESCE(o.tax_amount, 0)
            - COALESCE(o.delivery_fee, 0)
        )
    END AS sales_value,
    p.method AS payment_method,
    p.status AS payment_status,
    COALESCE(p.amount, 0) AS payment_amount
FROM `order` o
JOIN customer c ON c.customer_id = o.customer_id
JOIN user_account ua ON ua.user_account_id = c.user_account_id
JOIN payment p ON p.order_id = o.order_id;
