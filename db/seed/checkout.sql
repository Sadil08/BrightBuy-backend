-- Local/test defaults for checkout. Keep these values explicitly provisional until the business
-- confirms delivery pricing and tax policy.
START TRANSACTION;

INSERT INTO city (name, classification) VALUES
    ('Houston', 'Main'),
    ('San Antonio', 'Main'),
    ('Dallas', 'Main'),
    ('Austin', 'Main'),
    ('Fort Worth', 'Main'),
    ('El Paso', 'Main')
ON DUPLICATE KEY UPDATE
    classification = VALUES(classification);

INSERT INTO app_config (config_key, config_value) VALUES
    ('standard_delivery_fee', '9.99'),
    ('tax_rate_percent', '8.25')
ON DUPLICATE KEY UPDATE
    config_value = VALUES(config_value);

COMMIT;
