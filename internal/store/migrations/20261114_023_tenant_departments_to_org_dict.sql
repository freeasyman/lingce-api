-- 023：tenant_departments 指向平台组织字典（唯一真源）
--
-- 背景：运营台「机构涉及科室」原引用 billing_department_dict，现统一到 organization_department_dict。
-- 先摘旧外键 → 按名称映射 id → 再挂新外键（匹配不到会由新外键报错暴露，不静默放过）。

-- 1) 先摘掉旧外键，否则映射新 id 会撞旧表的 FK
ALTER TABLE tenant_departments DROP CONSTRAINT IF EXISTS tenant_departments_department_id_fkey;

-- 2) 按名称映射旧 id → 新 id
UPDATE tenant_departments td
SET department_id = nd.id
FROM billing_department_dict od
JOIN organization_department_dict nd ON nd.name = od.name
WHERE td.department_id = od.id;

-- 3) 挂新外键
ALTER TABLE tenant_departments ADD CONSTRAINT tenant_departments_department_id_fkey
    FOREIGN KEY (department_id) REFERENCES organization_department_dict(id) ON DELETE RESTRICT;
