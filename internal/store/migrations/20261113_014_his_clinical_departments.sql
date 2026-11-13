-- 014：临床科室字典 + EMR 模板映射（P0-4 科室链路）。
--
-- 背景：departments 是全产品共享表（机构/权限/内容等都在用），这里只做「加列 + 加字典行」，
-- 不改语义、不动既有非临床科室；历史租户的科室 type/template_code 为空时，HIS 侧按“未匹配”降级。
--
-- 约定：
--   type='clinical' 表示临床科室（HIS 排班/挂号/病历模板按它选择）；为空表示非临床部门。
--   template_code 只存 EMR 模板 code，真实模板仍以 emr_templates(code, tenant_id IN (0, tenant)) 解析。
--   7 个标准科室 code 固定：gyn/tcm/internal/pediatrics/dental/dermatology/surgery。

ALTER TABLE departments ADD COLUMN IF NOT EXISTS type TEXT;
ALTER TABLE departments ADD COLUMN IF NOT EXISTS template_code TEXT;

-- 同一租户内 code 唯一（仅对已填 code 且未删除的部门生效；历史脏数据不阻塞迁移）。
CREATE UNIQUE INDEX IF NOT EXISTS uq_departments_tenant_code
    ON departments (tenant_id, code)
    WHERE code IS NOT NULL AND code <> '' AND deleted_at IS NULL;

CREATE INDEX IF NOT EXISTS idx_departments_tenant_type
    ON departments (tenant_id, type)
    WHERE deleted_at IS NULL;

-- tenant 1 已有 妇科/儿科/内科 三个临床科室（挂在“医疗部”下）：
-- 复用原 id，仅补 type/template_code；妇科 code 由历史值 gynecology 归一到标准 code gyn。
UPDATE departments
SET code = 'gyn', type = 'clinical', template_code = 'gyn_first', updated_at = NOW()
WHERE tenant_id = 1 AND name = '妇科' AND code = 'gynecology' AND deleted_at IS NULL;

UPDATE departments
SET type = 'clinical', template_code = 'pediatrics', updated_at = NOW()
WHERE tenant_id = 1 AND name = '儿科' AND code = 'pediatrics' AND deleted_at IS NULL;

UPDATE departments
SET type = 'clinical', template_code = 'internal_medicine', updated_at = NOW()
WHERE tenant_id = 1 AND name = '内科' AND code = 'internal' AND deleted_at IS NULL;

-- 幂等补齐：tenant 1 缺的 4 个 + tenant 5001 全部 7 个。
-- 按 (tenant_id, code) 判断，已存在则跳过；parent_id 挂到同租户“医疗部”（没有则为 NULL）。
INSERT INTO departments (tenant_id, name, code, type, template_code, parent_id, is_active)
SELECT v.tenant_id,
       v.name,
       v.code,
       'clinical',
       v.template_code,
       (
           SELECT p.id FROM departments p
           WHERE p.tenant_id = v.tenant_id
             AND p.name = '医疗部'
             AND p.deleted_at IS NULL
           ORDER BY p.id
           LIMIT 1
       ),
       TRUE
FROM (VALUES
    (1,    '中医科', 'tcm',         'tcm_first'),
    (1,    '口腔科', 'dental',      'dental'),
    (1,    '皮肤科', 'dermatology', 'dermatology'),
    (1,    '外科',   'surgery',     'surgery'),
    (5001, '妇科',   'gyn',         'gyn_first'),
    (5001, '中医科', 'tcm',         'tcm_first'),
    (5001, '内科',   'internal',    'internal_medicine'),
    (5001, '儿科',   'pediatrics',  'pediatrics'),
    (5001, '口腔科', 'dental',      'dental'),
    (5001, '皮肤科', 'dermatology', 'dermatology'),
    (5001, '外科',   'surgery',     'surgery')
) AS v(tenant_id, name, code, template_code)
WHERE NOT EXISTS (
    SELECT 1 FROM departments d
    WHERE d.tenant_id = v.tenant_id AND d.code = v.code AND d.deleted_at IS NULL
);

-- 兜底：上面按 name/code 精确匹配没能补到的历史科室（例如租户 1 的“妇科”code 已被人工改成别的值），
-- 按 name 再补一次 type/template_code，保证 7 个标准科室在 HIS 侧都能选到。
UPDATE departments d
SET type = 'clinical',
    template_code = v.template_code,
    updated_at = NOW()
FROM (VALUES
    ('妇科', 'gyn_first'),
    ('中医科', 'tcm_first'),
    ('内科', 'internal_medicine'),
    ('儿科', 'pediatrics'),
    ('口腔科', 'dental'),
    ('皮肤科', 'dermatology'),
    ('外科', 'surgery')
) AS v(name, template_code)
WHERE d.tenant_id IN (1, 5001)
  AND d.name = v.name
  AND d.deleted_at IS NULL
  AND (d.type IS DISTINCT FROM 'clinical' OR d.template_code IS DISTINCT FROM v.template_code);
