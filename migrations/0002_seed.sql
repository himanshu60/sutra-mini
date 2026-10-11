-- 0002_seed: demo data — 5 SKUs, 2 job-workers and 4 open issues (ids 1-4 on a fresh database).

INSERT INTO skus (code, name, image_url) VALUES
    ('KUR-101', 'Chikankari Kurta',  'https://picsum.photos/seed/sutra-kur-101/400/500'),
    ('ANK-102', 'Anarkali Suit',     'https://picsum.photos/seed/sutra-ank-102/400/500'),
    ('LEH-103', 'Bridal Lehenga',    'https://picsum.photos/seed/sutra-leh-103/400/500'),
    ('DUP-104', 'Banarasi Dupatta',  'https://picsum.photos/seed/sutra-dup-104/400/500'),
    ('SAR-105', 'Zari Border Saree', 'https://picsum.photos/seed/sutra-sar-105/400/500');

INSERT INTO parties (name, phone) VALUES
    ('Noor Embroidery Works', '+91 90000 00001'),
    ('Sharma Stitching Unit', '+91 90000 00002');

INSERT INTO jobwork_issues (party_id, sku_id, qty, rate_paise, issued_on) VALUES
    ((SELECT id FROM parties WHERE name = 'Noor Embroidery Works'), (SELECT id FROM skus WHERE code = 'KUR-101'), 100,  4500, '2026-10-01'),
    ((SELECT id FROM parties WHERE name = 'Sharma Stitching Unit'), (SELECT id FROM skus WHERE code = 'ANK-102'),  50, 12000, '2026-10-02'),
    ((SELECT id FROM parties WHERE name = 'Noor Embroidery Works'), (SELECT id FROM skus WHERE code = 'LEH-103'),  20, 85000, '2026-10-03'),
    ((SELECT id FROM parties WHERE name = 'Sharma Stitching Unit'), (SELECT id FROM skus WHERE code = 'SAR-105'),  40,  6000, '2026-10-05');
