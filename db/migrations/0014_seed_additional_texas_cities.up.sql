INSERT INTO city (name, classification)
VALUES
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