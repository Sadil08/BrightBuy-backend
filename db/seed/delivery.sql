-- Feature 05 delivery reference data.
-- The city table is created by Feature 04.

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


--The script uses ON DUPLICATE KEY UPDATE, so it is safe to
--run more than once. Existing cities are updated to the Main 
--classification instead of duplicated.