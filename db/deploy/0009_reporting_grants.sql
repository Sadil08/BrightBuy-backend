-- Management Reporting read-only database credential.
-- Run this as a privileged deployment step after the five reporting views exist.
-- This is intentionally NOT a golang-migrate migration: it manages a database principal.

CREATE USER IF NOT EXISTS 'brightbuy_reporting'@'%' IDENTIFIED BY 'devreportingpass';

GRANT SELECT ON brightbuy.vw_quarterly_sales
    TO 'brightbuy_reporting'@'%';
GRANT SELECT ON brightbuy.vw_top_selling_products
    TO 'brightbuy_reporting'@'%';
GRANT SELECT ON brightbuy.vw_category_wise_orders
    TO 'brightbuy_reporting'@'%';
GRANT SELECT ON brightbuy.vw_upcoming_deliveries
    TO 'brightbuy_reporting'@'%';
GRANT SELECT ON brightbuy.vw_customer_order_summary
    TO 'brightbuy_reporting'@'%';

-- No base-table grants are given to this user. In production, replace the development password
-- above with a secret-manager generated password and do not commit it in a real deployment file.
