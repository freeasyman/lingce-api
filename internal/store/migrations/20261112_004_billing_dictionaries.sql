-- 运营端计费：行业字典 / 科室字典 + 机构-科室多对多（幂等）。
--
-- 产品文档 4.6：两个字典都是全局的，只有「名称 + 排序 + 启用/停用」，不做层级；
-- 停用不删除（历史机构仍显示原名），运营可随时增删，不用改代码。
-- 机构建档时「涉及科室」多选，先确定机构有哪些科室，后续建员工时从这里选，
-- 因此用关联表 tenant_departments 存 id 而不是把名字冗余在 tenants 上。
--
-- 种子数据照产品文档 4.6 的初始数据（17 个行业、19 个科室）；
-- ON CONFLICT (name) DO NOTHING 保证重复执行、以及与已人工维护的字典共存时不覆盖。

CREATE TABLE IF NOT EXISTS billing_industry_dict (
    id BIGSERIAL PRIMARY KEY,
    name TEXT NOT NULL,
    sort_order INTEGER NOT NULL DEFAULT 0,
    is_active BOOLEAN NOT NULL DEFAULT TRUE,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);
CREATE UNIQUE INDEX IF NOT EXISTS billing_industry_dict_name_uq
    ON billing_industry_dict (name);
CREATE INDEX IF NOT EXISTS billing_industry_dict_active_sort_idx
    ON billing_industry_dict (is_active, sort_order, id);

CREATE TABLE IF NOT EXISTS billing_department_dict (
    id BIGSERIAL PRIMARY KEY,
    name TEXT NOT NULL,
    sort_order INTEGER NOT NULL DEFAULT 0,
    is_active BOOLEAN NOT NULL DEFAULT TRUE,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);
CREATE UNIQUE INDEX IF NOT EXISTS billing_department_dict_name_uq
    ON billing_department_dict (name);
CREATE INDEX IF NOT EXISTS billing_department_dict_active_sort_idx
    ON billing_department_dict (is_active, sort_order, id);

-- 机构「涉及哪些科室」：tenant_id 按计费模块既有风格只建索引不做外键
-- （billing_* 系列表都没有 tenant_id 外键，避免历史脏数据阻塞迁移）；
-- department_id 是本迁移新建的字典表，加外键防止引用不存在的科室。
CREATE TABLE IF NOT EXISTS tenant_departments (
    tenant_id BIGINT NOT NULL,
    department_id BIGINT NOT NULL REFERENCES billing_department_dict (id) ON DELETE RESTRICT,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    PRIMARY KEY (tenant_id, department_id)
);
CREATE INDEX IF NOT EXISTS tenant_departments_department_idx
    ON tenant_departments (department_id);

INSERT INTO billing_industry_dict (name, sort_order) VALUES
    ('口腔', 10),
    ('医疗美容', 20),
    ('眼科', 30),
    ('中医', 40),
    ('综合医院', 50),
    ('综合门诊', 60),
    ('专科医院', 70),
    ('体检中心', 80),
    ('康复', 90),
    ('儿科', 100),
    ('皮肤科', 110),
    ('耳鼻喉', 120),
    ('骨科', 130),
    ('妇产', 140),
    ('精神心理', 150),
    ('护理养老', 160),
    ('其他', 170)
ON CONFLICT (name) DO NOTHING;

INSERT INTO billing_department_dict (name, sort_order) VALUES
    ('内科', 10),
    ('外科', 20),
    ('妇科', 30),
    ('儿科', 40),
    ('口腔科', 50),
    ('眼科', 60),
    ('耳鼻喉科', 70),
    ('皮肤科', 80),
    ('中医科', 90),
    ('全科', 100),
    ('医疗美容科', 110),
    ('体检中心', 120),
    ('康复科', 130),
    ('咨询客服', 140),
    ('前台导诊', 150),
    ('医务科', 160),
    ('信息科', 170),
    ('运营市场', 180),
    ('其他', 190)
ON CONFLICT (name) DO NOTHING;
