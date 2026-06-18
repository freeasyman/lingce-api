-- 灵策销售角色配置
-- 创建时间: 2026-04-30
-- 用途: 为灵策销售岗位添加角色定义和录音分析链路支持

-- 1. 添加 lingce_sales 角色到 inst_roles 表（如果表存在）
-- 注意：如果 inst_roles 表不存在，可以跳过此步骤
INSERT INTO inst_roles (code, name_cn, description, is_active, created_at, updated_at)
VALUES (
    'lingce_sales',
    '销售代表',
    '灵策销售代表角色，负责销售对话和客户沟通',
    true,
    NOW(),
    NOW()
) ON CONFLICT (code) DO UPDATE SET
    name_cn = EXCLUDED.name_cn,
    description = EXCLUDED.description,
    updated_at = NOW();

-- 2. 为现有销售员工添加 lingce_sales 角色
-- 示例：假设销售部门的 department_id 为 100
-- 请根据实际情况修改查询条件

-- 方式1：根据部门ID添加角色
-- INSERT INTO institution_employee_roles (tenant_id, employee_id, role_id, role_code, source, created_at, updated_at)
-- SELECT
--     e.tenant_id,
--     e.id as employee_id,
--     ir.id as role_id,
--     'lingce_sales' as role_code,
--     'manual' as source,
--     NOW() as created_at,
--     NOW() as updated_at
-- FROM employees e
-- JOIN institution_roles ir ON ir.tenant_id = e.tenant_id AND lower(ir.code) = 'lingce_sales' AND ir.deleted_at IS NULL
-- WHERE e.department_id = 100  -- 销售部门ID
--   AND NOT EXISTS (
--       SELECT 1 FROM institution_employee_roles r
--       WHERE r.employee_id = e.id AND r.role_code = 'lingce_sales'
--   );

-- 方式2：根据员工ID列表添加角色
-- INSERT INTO institution_employee_roles (tenant_id, employee_id, role_id, role_code, source, created_at, updated_at)
-- SELECT
--     1 as tenant_id,  -- 替换为实际的 tenant_id
--     employee_id,
--     ir.id as role_id,
--     'lingce_sales' as role_code,
--     'manual' as source,
--     NOW() as created_at,
--     NOW() as updated_at
-- FROM unnest(ARRAY[101, 102, 103]) as employee_id  -- 替换为实际的员工ID列表
-- JOIN institution_roles ir ON ir.tenant_id = 1 AND lower(ir.code) = 'lingce_sales' AND ir.deleted_at IS NULL
-- WHERE NOT EXISTS (
--     SELECT 1 FROM institution_employee_roles r
--     WHERE r.employee_id = employee_id AND r.role_code = 'lingce_sales'
-- );

-- 3. 验证角色添加结果
-- SELECT
--     e.id as employee_id,
--     e.name as employee_name,
--     r.role_code,
--     r.created_at
-- FROM institution_employee_roles r
-- JOIN employees e ON e.id = r.employee_id
-- WHERE r.role_code = 'lingce_sales'
-- ORDER BY r.created_at DESC;

-- 4. 查看现有录音数据（可选）
-- SELECT
--     r.id,
--     r.employee_id,
--     e.name as employee_name,
--     r.transcription_status,
--     r.analysis_status,
--     r.created_at
-- FROM recordings r
-- JOIN employees e ON e.id = r.employee_id
-- JOIN institution_employee_roles er ON er.employee_id = r.employee_id
-- WHERE er.role_code = 'lingce_sales'
-- ORDER BY r.created_at DESC
-- LIMIT 10;
