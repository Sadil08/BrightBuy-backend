START TRANSACTION;


INSERT INTO category (category_id, name, description, is_active) VALUES
(1, 'Laptops', 'Portable computers', TRUE),
(2, 'Smartphones', 'Mobile phones', TRUE),
(3, 'Tablets', 'Tablet computers', TRUE),
(4, 'Audio', 'Headphones and speakers', TRUE),
(5, 'Cameras', 'Digital cameras and accessories', TRUE),
(6, 'Gaming', 'Gaming consoles and accessories', TRUE),
(7, 'Computer Accessories', 'Keyboards, mice and accessories', TRUE),
(8, 'Smart Home', 'Connected home devices', TRUE),
(9, 'Toys', 'Toys and educational products', TRUE),
(10, 'Office Equipment', 'Equipment for home and office', TRUE)
ON DUPLICATE KEY UPDATE
    name = VALUES(name),
    description = VALUES(description),
    is_active = VALUES(is_active);
-- ON DUPLICATE KEY UPDATE allows the seed to run more than once without creating duplicates.


INSERT INTO product
    (product_id, name, description, is_active)
VALUES
(1, 'BrightBook Air 13', 'Lightweight 13-inch laptop', TRUE),
(2, 'BrightBook Pro 14', 'Performance laptop for professionals', TRUE),
(3, 'BrightBook Studio 16', 'Large-screen creative laptop', TRUE),
(4, 'BrightBook Student 15', 'Affordable laptop for students', TRUE),
(5, 'BrightBook Mini 12', 'Compact everyday laptop', TRUE),
(6, 'BrightPhone X1', '5G Android smartphone', TRUE),
(7, 'BrightPhone X1 Pro', 'Premium smartphone with advanced camera', TRUE),
(8, 'BrightPhone Lite', 'Affordable smartphone', TRUE),
(9, 'BrightPhone Max', 'Large-screen smartphone', TRUE),
(10, 'BrightPhone Secure', 'Business smartphone', TRUE),
(11, 'BrightTab 10', '10-inch tablet', TRUE),
(12, 'BrightTab Pro', 'Professional tablet', TRUE),
(13, 'BrightTab Kids', 'Child-friendly tablet', TRUE),
(14, 'BrightTab Mini', 'Compact tablet', TRUE),
(15, 'BrightTab Reader', 'Tablet for reading and media', TRUE),
(16, 'BrightBuds Wireless', 'Wireless earbuds', TRUE),
(17, 'BrightHeadphones Studio', 'Noise-cancelling headphones', TRUE),
(18, 'BrightSpeaker Go', 'Portable Bluetooth speaker', TRUE),
(19, 'BrightSpeaker Home', 'Smart home speaker', TRUE),
(20, 'BrightMic USB', 'USB desktop microphone', TRUE),
(21, 'BrightCam 4K', '4K digital camera', TRUE),
(22, 'BrightCam Pocket', 'Compact travel camera', TRUE),
(23, 'BrightLens 50', '50mm camera lens', TRUE),
(24, 'BrightTripod Pro', 'Adjustable camera tripod', TRUE),
(25, 'BrightLight Panel', 'LED photography light', TRUE),
(26, 'BrightConsole One', 'Home gaming console', TRUE),
(27, 'BrightController', 'Wireless gaming controller', TRUE),
(28, 'BrightGame Racer', 'Racing game', TRUE),
(29, 'BrightGame Quest', 'Adventure game', TRUE),
(30, 'BrightVR Headset', 'Virtual reality headset', TRUE),
(31, 'BrightKeyboard', 'Mechanical keyboard', TRUE),
(32, 'BrightMouse', 'Wireless computer mouse', TRUE),
(33, 'BrightMonitor 27', '27-inch office monitor', TRUE),
(34, 'BrightDock USB-C', 'USB-C docking station', TRUE),
(35, 'BrightRouter WiFi', 'Dual-band wireless router', TRUE),
(36, 'BrightHome Hub', 'Smart home controller', TRUE),
(37, 'BrightBulb Pack', 'Smart LED bulb pack', TRUE),
(38, 'BrightRobot Kit', 'Educational robotics kit', TRUE),
(39, 'BrightDesk Organizer', 'Desktop organizer', TRUE),
(40, 'BrightLegacy Camera', 'Discontinued camera model', FALSE)
ON DUPLICATE KEY UPDATE
    name = VALUES(name),
    description = VALUES(description),
    is_active = VALUES(is_active);


-- This assigns all 40 products across all 10 categories.
INSERT INTO product_category (product_id, category_id) VALUES
(1,1),(2,1),(3,1),(4,1),(5,1),
(6,2),(7,2),(8,2),(9,2),(10,2),
(11,3),(12,3),(13,3),(14,3),(15,3),
(16,4),(17,4),(18,4),(19,4),(20,4),
(21,5),(22,5),(23,5),(24,5),(25,5),
(26,6),(27,6),(28,6),(29,6),(30,6),
(31,7),(32,7),(33,7),(34,7),(35,7),
(36,8),(37,8),
(38,9),
(39,10),
(40,5)
ON DUPLICATE KEY UPDATE
    product_id = VALUES(product_id);


INSERT INTO product_variant
    (variant_id, product_id, sku, price, stock_quantity, is_active)
VALUES
(1, 1, 'LAP-AIR-13', 799.99, 12, TRUE),
(2, 2, 'LAP-PRO-14', 1199.99, 8, TRUE),
(3, 3, 'LAP-STUDIO-16', 1599.99, 0, TRUE),
(4, 4, 'LAP-STUDENT-15', 599.99, 15, TRUE),
(5, 5, 'LAP-MINI-12', 499.99, 4, TRUE),

(6, 6, 'PHONE-X1-BLK', 699.99, 20, TRUE),
(7, 6, 'PHONE-X1-WHT', 699.99, 0, TRUE),
(8, 7, 'PHONE-X1P-BLK', 999.99, 7, TRUE),
(9, 8, 'PHONE-LITE-BLU', 299.99, 18, TRUE),
(10, 9, 'PHONE-MAX-GRY', 799.99, 0, TRUE),

(11, 11, 'TAB-10-GRY', 299.99, 10, TRUE),
(12, 12, 'TAB-PRO-SLV', 699.99, 5, TRUE),
(13, 13, 'TAB-KIDS-BLU', 199.99, 9, TRUE),
(14, 14, 'TAB-MINI-BLK', 249.99, 6, TRUE),
(15, 15, 'TAB-READER-WHT', 179.99, 3, TRUE),

(16, 16, 'BUDS-WLS-BLK', 89.99, 25, TRUE),
(17, 17, 'HEAD-STUDIO-BLK', 249.99, 11, TRUE),
(18, 18, 'SPK-GO-RED', 59.99, 14, TRUE),
(19, 19, 'SPK-HOME-WHT', 129.99, 0, TRUE),
(20, 20, 'MIC-USB-BLK', 79.99, 8, TRUE),

(21, 21, 'CAM-4K-BLK', 899.99, 4, TRUE),
(22, 22, 'CAM-POCKET-SLV', 399.99, 6, TRUE),
(23, 23, 'LENS-50MM', 349.99, 2, TRUE),
(24, 24, 'TRIPOD-PRO', 119.99, 10, TRUE),
(25, 25, 'LIGHT-PANEL', 149.99, 0, TRUE),

(26, 26, 'CONSOLE-ONE', 499.99, 5, TRUE),
(27, 27, 'CTRL-WLS-BLK', 69.99, 12, TRUE),
(28, 28, 'GAME-RACER', 59.99, 20, TRUE),
(29, 29, 'GAME-QUEST', 59.99, 18, TRUE),
(30, 30, 'VR-HEADSET', 399.99, 3, TRUE),

(31, 31, 'KEY-MECH-BLK', 109.99, 7, TRUE),
(32, 32, 'MOUSE-WLS-BLK', 49.99, 16, TRUE),
(33, 33, 'MONITOR-27', 279.99, 6, TRUE),
(34, 34, 'DOCK-USBC', 129.99, 8, TRUE),
(35, 35, 'ROUTER-WIFI', 99.99, 13, TRUE),

(36, 36, 'HOME-HUB', 149.99, 5, TRUE),
(37, 37, 'BULB-PACK', 39.99, 21, TRUE),
(38, 38, 'ROBOT-KIT', 89.99, 4, TRUE),
(39, 39, 'DESK-ORG', 24.99, 30, TRUE),
(40, 40, 'CAM-LEGACY', 199.99, 0, FALSE),

-- Previously missing entirely: product 10 (BrightPhone Secure) had ZERO variants — a real data bug
-- found while broadening variant coverage below, not a hypothetical. plan.md §6/REQ-10.3 both say
-- this "should never occur"; it did, here, until now.
(41, 10, 'PHONE-SECURE-BLK', 849.99, 9, TRUE),

-- Broadening beyond the single BrightPhone X1 (variants 6/7): one additional multi-variant product
-- per OTHER category (9 more, covering every category this seed touches), so AC-CATALOG-4's
-- single-vs-multi-variant frontend paths have more than one real example to exercise. Each existing
-- single-variant row above that gains a sibling below also gets its own attribute row in the
-- variant_attribute block further down — a "multi-variant" product whose variants have no
-- DISTINGUISHING attribute at all would be a confusing product to actually look at.
(42, 1,  'LAP-AIR-13-SLV',    799.99, 6,  TRUE),  -- Laptops: Air 13 also in Silver
(43, 11, 'TAB-10-BLK',        299.99, 7,  TRUE),  -- Tablets: Tab 10 also in Black
(44, 16, 'BUDS-WLS-WHT',       89.99, 13, TRUE),  -- Audio: Buds also in White
(45, 21, 'CAM-4K-SLV',        899.99, 3,  TRUE),  -- Cameras: 4K cam also in Silver
(46, 26, 'CONSOLE-ONE-1TB',   549.99, 4,  TRUE),  -- Gaming: Console also in 1TB
(47, 31, 'KEY-MECH-WHT',      109.99, 5,  TRUE),  -- Computer Accessories: keyboard also in White
(48, 36, 'HOME-HUB-BLK',      149.99, 6,  TRUE),  -- Smart Home: Hub also in Black
(49, 38, 'ROBOT-KIT-BLU',      89.99, 3,  TRUE),  -- Toys: Robot Kit also in Blue
(50, 39, 'DESK-ORG-WHT',       24.99, 22, TRUE)   -- Office Equipment: Organizer also in White
ON DUPLICATE KEY UPDATE
    product_id = VALUES(product_id),
    sku = VALUES(sku),
    price = VALUES(price),
    stock_quantity = VALUES(stock_quantity),
    is_active = VALUES(is_active);


-- Variant attributes (EAV side tables). Originally these three tables were entirely empty in this
-- seed script — BrightPhone X1's variant picker had nothing to label its options with and fell back
-- to showing a raw SKU ("PHONE-X1-BLK") instead of "Black"/"White" (found by actually loading the
-- product detail page, not by inspecting the SQL). Since broadened to cover one multi-variant
-- product per category (see product_variant rows 42-50 above) — every variant of a multi-variant
-- product gets its own attribute row here, including the ORIGINAL single variant that gained a
-- sibling (e.g. variant 1, LAP-AIR-13, now needs "Color: Black" since LAP-AIR-13-SLV exists to be
-- distinguished from). Genuinely single-variant products (most of the catalog) still have none —
-- FR-CATALOG-5 doesn't need one to distinguish, since there's nothing to distinguish it from.
INSERT INTO attribute_name (attribute_name_id, name) VALUES
(1, 'Color'),
(2, 'Storage')
ON DUPLICATE KEY UPDATE name = VALUES(name);

INSERT INTO attribute_value (attribute_value_id, attribute_name_id, value) VALUES
(1, 1, 'Black'),
(2, 1, 'White'),
(3, 1, 'Silver'),
(4, 1, 'Gray'),
(5, 1, 'Blue'),
(6, 1, 'Red'),
(7, 2, '512GB'),
(8, 2, '1TB')
ON DUPLICATE KEY UPDATE attribute_name_id = VALUES(attribute_name_id), value = VALUES(value);

INSERT INTO variant_attribute (variant_id, attribute_value_id) VALUES
(6, 1),   -- PHONE-X1-BLK        -> Color: Black
(7, 2),   -- PHONE-X1-WHT        -> Color: White
(1, 1),   -- LAP-AIR-13          -> Color: Black
(42, 3),  -- LAP-AIR-13-SLV      -> Color: Silver
(11, 4),  -- TAB-10-GRY          -> Color: Gray
(43, 1),  -- TAB-10-BLK          -> Color: Black
(16, 1),  -- BUDS-WLS-BLK        -> Color: Black
(44, 2),  -- BUDS-WLS-WHT        -> Color: White
(21, 1),  -- CAM-4K-BLK          -> Color: Black
(45, 3),  -- CAM-4K-SLV          -> Color: Silver
(26, 7),  -- CONSOLE-ONE         -> Storage: 512GB
(46, 8),  -- CONSOLE-ONE-1TB     -> Storage: 1TB
(31, 1),  -- KEY-MECH-BLK        -> Color: Black
(47, 2),  -- KEY-MECH-WHT        -> Color: White
(36, 2),  -- HOME-HUB            -> Color: White
(48, 1),  -- HOME-HUB-BLK        -> Color: Black
(38, 6),  -- ROBOT-KIT           -> Color: Red
(49, 5),  -- ROBOT-KIT-BLU       -> Color: Blue
(39, 1),  -- DESK-ORG            -> Color: Black
(50, 2)   -- DESK-ORG-WHT        -> Color: White
ON DUPLICATE KEY UPDATE variant_id = VALUES(variant_id);


COMMIT;