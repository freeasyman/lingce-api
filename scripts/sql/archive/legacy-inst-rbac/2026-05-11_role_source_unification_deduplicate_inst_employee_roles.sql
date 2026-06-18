-- 角色真源统一: inst_employee_roles 多角色收敛脚本
-- 时间: 2026-05-11
-- 目的:
-- 1. 将 inst_employee_roles 按 (tenant_id, employee_id) 收敛为一条当前角色
-- 2. 保留来源优先级最高且时间最新的一条
-- 3. 删除其余重复/冲突记录
--
-- 来源优先级:
-- 1. manual
-- 2. tenant_init
-- 3. department
-- 4. backfill_from_institution_employee_roles
-- 5. legacy_unknown
-- 6. 其他未知来源
--
-- 注意:
-- 1. 执行前先跑审计脚本确认影响面
-- 2. 本脚本会删数据，必须在备份或可回滚前提下执行
-- 3. 如果后续确认某些租户需要人工裁决，不要先执行本脚本

-- 1. 预览将保留哪一条
WITH ranked AS (
    SELECT
        er.*,
        ROW_NUMBER() OVER (
            PARTITION BY er.tenant_id, er.employee_id
            ORDER BY
                CASE
                    WHEN COALESCE(er.source, '') = 'manual' THEN 1
                    WHEN COALESCE(er.source, '') = 'tenant_init' THEN 2
                    WHEN COALESCE(er.source, '') = 'department' THEN 3
                    WHEN COALESCE(er.source, '') = 'backfill_from_institution_employee_roles' THEN 4
                    WHEN COALESCE(er.source, '') = 'legacy_unknown' THEN 5
                    ELSE 6
                END ASC,
                COALESCE(er.updated_at, er.created_at, NOW()) DESC,
                COALESCE(er.created_at, NOW()) DESC,
                lower(trim(er.role_code)) ASC
        ) AS rn,
        COUNT(*) OVER (PARTITION BY er.tenant_id, er.employee_id) AS row_count
    FROM inst_employee_roles er
)
SELECT tenant_id,
       employee_id,
       role_code,
       source,
       created_at,
       updated_at,
       row_count
FROM ranked
WHERE row_count > 1
  AND rn = 1
ORDER BY tenant_id, employee_id;

-- 2. 预览将被删除的记录
WITH ranked AS (
    SELECT
        er.*,
        ROW_NUMBER() OVER (
            PARTITION BY er.tenant_id, er.employee_id
            ORDER BY
                CASE
                    WHEN COALESCE(er.source, '') = 'manual' THEN 1
                    WHEN COALESCE(er.source, '') = 'tenant_init' THEN 2
                    WHEN COALESCE(er.source, '') = 'department' THEN 3
                    WHEN COALESCE(er.source, '') = 'backfill_from_institution_employee_roles' THEN 4
                    WHEN COALESCE(er.source, '') = 'legacy_unknown' THEN 5
                    ELSE 6
                END ASC,
                COALESCE(er.updated_at, er.created_at, NOW()) DESC,
                COALESCE(er.created_at, NOW()) DESC,
                lower(trim(er.role_code)) ASC
        ) AS rn,
        COUNT(*) OVER (PARTITION BY er.tenant_id, er.employee_id) AS row_count
    FROM inst_employee_roles er
)
SELECT tenant_id,
       employee_id,
       role_code,
       source,
       created_at,
       updated_at,
       row_count
FROM ranked
WHERE row_count > 1
  AND rn > 1
ORDER BY tenant_id, employee_id, rn;

-- 3. 实际删除
WITH ranked AS (
    SELECT
        er.ctid,
        ROW_NUMBER() OVER (
            PARTITION BY er.tenant_id, er.employee_id
            ORDER BY
                CASE
                    WHEN COALESCE(er.source, '') = 'manual' THEN 1
                    WHEN COALESCE(er.source, '') = 'tenant_init' THEN 2
                    WHEN COALESCE(er.source, '') = 'department' THEN 3
                    WHEN COALESCE(er.source, '') = 'backfill_from_institution_employee_roles' THEN 4
                    WHEN COALESCE(er.source, '') = 'legacy_unknown' THEN 5
                    ELSE 6
                END ASC,
                COALESCE(er.updated_at, er.created_at, NOW()) DESC,
                COALESCE(er.created_at, NOW()) DESC,
                lower(trim(er.role_code)) ASC
        ) AS rn
    FROM inst_employee_roles er
)
DELETE FROM inst_employee_roles er
USING ranked r
WHERE er.ctid = r.ctid
  AND r.rn > 1;
