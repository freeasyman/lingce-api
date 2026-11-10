-- 价目表种子：六产品 × 标准档/试用档（price_version = 2026）。
-- 试用档额度 = 标准档 × 7/365 向上取整，年价 0、无增量包。

INSERT INTO product_catalog (product_code, product_name, edition, price_version, annual_price, quotas, increment_price, increments)
VALUES
  ('yingke',  '赢客专家', 'standard', '2026', 12600, '{"hour":400}'::jsonb,                              1500, '{"hour":80}'::jsonb),
  ('yingke',  '赢客专家', 'trial',    '2026',     0, '{"hour":8}'::jsonb,                                  0, '{}'::jsonb),
  ('kehu',    '客户总监', 'standard', '2026', 10800, '{"hour":400,"shot":5000,"record":10000,"person":10000}'::jsonb, 1200, '{"hour":80,"shot":1000,"record":2000,"person":2000}'::jsonb),
  ('kehu',    '客户总监', 'trial',    '2026',     0, '{"hour":8,"shot":96,"record":192,"person":192}'::jsonb,          0, '{}'::jsonb),
  ('suifang', '随访顾问', 'standard', '2026',  5400, '{"hour":400}'::jsonb,                               600, '{"hour":80}'::jsonb),
  ('suifang', '随访顾问', 'trial',    '2026',     0, '{"hour":8}'::jsonb,                                  0, '{}'::jsonb),
  ('tingxie', '听写病历', 'standard', '2026', 21600, '{"record":2500,"round":5000,"hour":400}'::jsonb,   2500, '{"record":500,"round":1000,"hour":80}'::jsonb),
  ('tingxie', '听写病历', 'trial',    '2026',     0, '{"record":48,"round":96,"hour":8}'::jsonb,            0, '{}'::jsonb),
  ('hegui',   '合规卫士', 'standard', '2026',  8400, '{"hour":400}'::jsonb,                              1000, '{"hour":80}'::jsonb),
  ('hegui',   '合规卫士', 'trial',    '2026',     0, '{"hour":8}'::jsonb,                                  0, '{}'::jsonb),
  ('huoke',   '获客先锋', 'standard', '2026', 15600, '{"hour":400,"prompt":240,"record":600,"shot":1200}'::jsonb, 1800, '{"hour":80,"prompt":48,"record":120,"shot":240}'::jsonb),
  ('huoke',   '获客先锋', 'trial',    '2026',     0, '{"hour":8,"prompt":5,"record":12,"shot":24}'::jsonb,           0, '{}'::jsonb)
ON CONFLICT (product_code, edition, price_version) DO NOTHING;
