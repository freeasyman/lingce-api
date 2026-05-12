# 角色真源统一生产实施顺序

本文件只描述生产实施顺序，不解释背景。

## 1. 发布代码

先发布当前代码版本，确保应用层已经满足：

1. 租户默认 admin 主写 `inst_employee_roles`
2. 员工角色设置/删除主写 `inst_employee_roles`
3. 部门 `update_existing` 不再给已有角色员工叠加第二条记录
4. 员工展示主读 `inst_employee_roles`
5. RBAC 员工权限主读 `inst_employee_roles`
6. 录音列表角色分域主读 `inst_employee_roles`
7. 应用启动不再自动创建/回填 `institution_employee_roles`

## 2. 执行库结构预备脚本

执行：

[scripts/sql/2026-05-11_role_source_unification_prepare_inst_employee_roles.sql](/Users/yiliiang/Documents/lingce-api/scripts/sql/2026-05-11_role_source_unification_prepare_inst_employee_roles.sql)

目标：

1. 确认 `inst_employee_roles` 具备 `source` / `updated_at`
2. 确认有基础索引
3. 不做唯一约束

## 3. 执行审计

执行：

[scripts/sql/2026-05-11_role_source_unification_audit.sql](/Users/yiliiang/Documents/lingce-api/scripts/sql/2026-05-11_role_source_unification_audit.sql)

需要保存结果：

1. `inst_employee_roles` 缺失清单
2. 双表冲突清单
3. 多角色员工清单
4. 活跃员工无角色清单
5. 无法映射 role_id 清单

## 4. 执行缺失回填

执行：

[scripts/sql/2026-05-11_role_source_unification_backfill.sql](/Users/yiliiang/Documents/lingce-api/scripts/sql/2026-05-11_role_source_unification_backfill.sql)

规则：

1. 只回填缺失
2. 不自动覆盖冲突

## 5. 复跑审计

再次执行审计脚本，确认：

1. 缺失员工明显减少或归零
2. 剩余问题集中在冲突和多角色

## 6. 人工清理冲突与多角色

必须人工确认后清理：

1. 双表不一致员工
2. `inst_employee_roles` 一人多角色员工
3. 活跃员工两边都无角色员工

目标状态：

对每个 `(tenant_id, employee_id)`，只保留一条当前角色记录。

多角色收敛脚本：

[scripts/sql/2026-05-11_role_source_unification_deduplicate_inst_employee_roles.sql](/Users/yiliiang/Documents/lingce-api/scripts/sql/2026-05-11_role_source_unification_deduplicate_inst_employee_roles.sql)

## 7. 关键业务回归

至少回归：

1. 新建租户
2. 默认 admin 登录
3. 员工列表/详情
4. 修改员工角色
5. 修改部门默认角色并 `update_existing=true`
6. 员工权限判断
7. 录音列表 doctor/consultant/frontdesk 分域
8. 正常分析
9. 重新分析
10. sandbox / badge 抽样

## 8. 最终收口

在确认不存在多角色员工后，执行：

[scripts/sql/2026-05-11_role_source_unification_finalize_inst_employee_roles.sql](/Users/yiliiang/Documents/lingce-api/scripts/sql/2026-05-11_role_source_unification_finalize_inst_employee_roles.sql)

该脚本会：

1. 给 `inst_employee_roles(tenant_id, employee_id)` 加唯一索引
2. 把 `institution_employee_roles` 改名为 `institution_employee_roles_disabled`

## 9. 改名后观察

改名后重点观察：

1. API 500
2. worker 任务失败
3. 低频管理接口
4. 冷门脚本
5. 报表类查询

如果出现 `relation "institution_employee_roles" does not exist`，直接按残留依赖修复。
