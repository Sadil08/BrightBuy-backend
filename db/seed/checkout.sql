-- Local/test defaults for checkout. Keep these values explicitly provisional until the business
-- confirms delivery pricing and tax policy.
START TRANSACTION;

INSERT INTO city (name, classification)
VALUES
    ('Houston', 'Main'),
    ('San Antonio', 'Main'),
    ('Dallas', 'Main'),
    ('Austin', 'Main'),
    ('Fort Worth', 'Main'),
    ('El Paso', 'Main'),

    ('Arlington', 'Other'),
    ('Plano', 'Other'),
    ('Irving', 'Other'),
    ('Garland', 'Other'),
    ('Frisco', 'Other'),
    ('McKinney', 'Other'),
    ('Round Rock', 'Other'),
    ('Killeen', 'Other'),
    ('Waco', 'Other'),
    ('Corpus Christi', 'Other'),
    ('Laredo', 'Other'),
    ('Amarillo', 'Other'),
    ('Lubbock', 'Other'),
    ('Beaumont', 'Other'),
    ('Tyler', 'Other'),
    ('Odessa', 'Other'),
    ('Midland', 'Other'),
    ('Abilene', 'Other')
ON DUPLICATE KEY UPDATE
    classification = VALUES(classification);



INSERT INTO app_config (config_key, config_value) VALUES
    ('standard_delivery_fee', '9.99'),
    ('tax_rate_percent', '8.25')
ON DUPLICATE KEY UPDATE
    config_value = VALUES(config_value);

COMMIT;
