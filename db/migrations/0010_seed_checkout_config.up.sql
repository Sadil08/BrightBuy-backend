-- Reference data checkout cannot work without (04-checkout-orders/plan.md §2, tasks T1): the delivery
-- fee and tax rate (TBD-1) and the Main-City list (TBD-2). Without these rows every checkout fails —
-- sp_place_order's caller can't read tax_rate_percent — so a freshly migrated database must have them.
-- Values are the SRS's explicit PLACEHOLDERS, not BrightBuy-confirmed. Idempotent, and it never
-- overwrites a value someone has already changed (ON DUPLICATE KEY ... = existing value).
INSERT INTO app_config (config_key, config_value) VALUES
    ('standard_delivery_fee', '9.99'),
    ('tax_rate_percent', '8.25')
ON DUPLICATE KEY UPDATE config_value = config_value;

INSERT INTO city (name, classification) VALUES
    ('Houston', 'Main'),
    ('San Antonio', 'Main'),
    ('Dallas', 'Main'),
    ('Austin', 'Main'),
    ('Fort Worth', 'Main'),
    ('El Paso', 'Main')
ON DUPLICATE KEY UPDATE classification = classification;
