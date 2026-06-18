-- 角色真源统一: 最终收口脚本
-- 时间: 2026-05-11
-- 前提:
-- 1. prepare_inst_employee_roles 已执行
-- 2. audit / backfill 已执行
-- 3. 多角色与冲突员工已人工处理
-- 4. 确认每个 (tenant_id, employee_id) 只剩一条当前角色
--
-- 本脚本做两件事:
-- 1. 给 inst_employee_roles 加唯一约束
-- 2. 断路 institution_employee_roles

-- 0. 保护性检查: 若仍存在多角色员工，不要继续执行后续语句
-- 手工先执行以下查询确认结果为空:
-- SELECT tenant_id, employee_id, COUNT(*) AS cnt
-- FROM inst_employee_roles
-- GROUP BY tenant_id, employee_id
-- HAVING COUNT(*) > 1;

CREATE UNIQUE INDEX IF NOT EXISTS uk_inst_employee_roles_tenant_employee
    ON inst_employee_roles (tenant_id, employee_id);

-- 断路旧表:
-- 生产上建议单独窗口执行这一句，并在执行后观察日志。
ALTER TABLE IF EXISTS institution_employee_roles
    RENAME TO institution_employee_roles_disabled;
