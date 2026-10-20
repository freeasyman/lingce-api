-- 赢客专家阶段 0 验收种子数据说明
--
-- 本脚本只负责建立可重复的验收租户、产品和员工基础资料。
-- E001-E008 录音数据应通过 recording 接口或专用 fixture 导入，避免在不同版本的 recordings
-- 表上假设不存在的列。执行前必须在独立验收数据库运行，不得在生产库执行。
-- 回滚：删除 tenant_id = 9001 的验收数据（请先确认没有其他验收任务使用该租户）。

BEGIN;

INSERT INTO tenants (id, name, code, account_mode, industry, is_active, created_at, updated_at)
VALUES (9001, '明视眼科测试机构', 'deal-acceptance-9001', 'formal', '消费医疗', 1, NOW(), NOW())
ON CONFLICT (id) DO UPDATE SET
  name = EXCLUDED.name,
  code = EXCLUDED.code,
  account_mode = EXCLUDED.account_mode,
  industry = EXCLUDED.industry,
  is_active = 1,
  deleted_at = NULL,
  updated_at = NOW();

INSERT INTO departments (id, tenant_id, name, code, is_active, created_at, updated_at)
VALUES
  (99101, 9001, '咨询部', 'consulting_department', TRUE, NOW(), NOW()),
  (99102, 9001, '运营部', 'operations_department', TRUE, NOW(), NOW()),
  (99103, 9001, '医疗部', 'medical_department', TRUE, NOW(), NOW())
ON CONFLICT (id) DO UPDATE SET name = EXCLUDED.name, code = EXCLUDED.code, is_active = TRUE, updated_at = NOW();

INSERT INTO institution_roles (id, tenant_id, name, code, description, is_active, created_at, updated_at)
VALUES
  (99201, 9001, '咨询师', 'consultant', '赢客专家咨询师验收角色', TRUE, NOW(), NOW()),
  (99202, 9001, '销售主管', 'sales_manager', '赢客专家主管验收角色', TRUE, NOW(), NOW()),
  (99203, 9001, '院长', 'director', '赢客专家院长验收角色', TRUE, NOW(), NOW()),
  (99204, 9001, '随访人员', 'followup', '随访协作验收角色', TRUE, NOW(), NOW()),
  (99205, 9001, '普通员工', 'staff', '无赢客专家权限验收角色', TRUE, NOW(), NOW())
ON CONFLICT (id) DO UPDATE SET name = EXCLUDED.name, code = EXCLUDED.code, is_active = TRUE, updated_at = NOW();

-- 统一验收密码：DealAccept#2026（只用于独立验收环境）。
-- bcrypt hash 由 lingce-api 的 employee service 生成；此处不写明文密码。
-- 请使用下方变量替换 DEAL_ACCEPTANCE_BCRYPT_HASH 后执行员工插入。
-- :DEAL_ACCEPTANCE_BCRYPT_HASH

INSERT INTO products (id, tenant_id, industry, name, aliases, price, applicable, selling_point, upgrade_to, combine_with, status, is_template, created_at, updated_at)
VALUES
  (9201, 9001, '屈光', '全飞秒基础', '["全飞秒基础","全飞"]'::jsonb, 18800, '适合基础屈光矫正评估通过者', '切口小、恢复快', '', '干眼评估', 'active', FALSE, NOW(), NOW()),
  (9202, 9001, '屈光', '全飞秒 Pro', '["全飞秒 Pro","全飞秒PRO"]'::jsonb, 23800, '适合追求更高视觉质量者', '视觉质量和定位能力更高', '', '干眼评估', 'active', FALSE, NOW(), NOW()),
  (9203, 9001, '屈光', '干眼评估', '["干眼评估","眼表评估"]'::jsonb, 1300, '客户表达眼干或眼表不适时', '先确认眼表状态', '', '', 'active', FALSE, NOW(), NOW()),
  (9204, 9001, '口腔', '牙种植基础', '["牙种植基础"]'::jsonb, 16800, '种植适应证待确认者', '基础种植方案', '', '', 'active', FALSE, NOW(), NOW())
ON CONFLICT (id) DO UPDATE SET name = EXCLUDED.name, price = EXCLUDED.price, applicable = EXCLUDED.applicable, selling_point = EXCLUDED.selling_point, status = 'active', updated_at = NOW();

COMMIT;

-- 员工种子需要通过 POST /api/v1/employees 创建，以确保密码哈希、角色关联和审计字段符合当前服务实现：
-- 9101 王晓 consultant 99101
-- 9102 陈琳 consultant 99101
-- 9103 赵主管 sales_manager 99102
-- 9104 周院长 director 99102
-- 9105 李护士 followup 99103
-- 9106 无权限员工 staff 99102
