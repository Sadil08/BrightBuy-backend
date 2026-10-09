-- Feature 05 delivery reference data.
-- The city table is created by Feature 04.

INSERT INTO city (name, classification)
VALUES
    ('Houston', 'Main'),
    ('San Antonio', 'Main'),
    ('Dallas', 'Main'),
    ('Austin', 'Main'),
    ('Fort Worth', 'Main'),
    ('El Paso', 'Main')
ON DUPLICATE KEY UPDATE
    classification = VALUES(classification);