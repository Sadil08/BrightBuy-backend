DELETE FROM app_config WHERE config_key IN ('standard_delivery_fee', 'tax_rate_percent');

-- A city that an existing delivery row points at must stay (foreign key), so only unused ones go.
DELETE FROM city
WHERE name IN ('Houston', 'San Antonio', 'Dallas', 'Austin', 'Fort Worth', 'El Paso')
  AND city_id NOT IN (SELECT city_id FROM delivery WHERE city_id IS NOT NULL);
