# 机构端权限体系一次性收口实施方案

本文档是 [INSTITUTION_PERMISSION_UNIFICATION_PLAN.md](/Users/yiliiang/Documents/lingce-api/docs/INSTITUTION_PERMISSION_UNIFICATION_PLAN.md) 的落地执行版。

目标不是再次讨论方向，而是把数据库、后端、前端、worker、验证、上线拆成可以逐项执行的实施计划。

## 1. 实施原则

1. 先冻结终态模型，再开始迁移
2. 数据库迁移与代码切换分开执行
3. 每一阶段都必须有对账 SQL
4. 每一步都要能回滚到上一步
5. 旧表删除必须是最后一步

## 2. 实施范围

### 2.1 数据库表

本次涉及：

1. `institution_employee_roles`
2. `institution_roles`
3. `institution_menus`
4. `institution_role_menus`
5. `institution_department_roles`
6. `tenant_feature_groups`
7. `tenant_feature_group_items`
8. `tenant_feature_assignments`
9. `tenant_feature_overrides`

本次待退出：

1. `inst_employee_roles`
2. `inst_roles`
3. `inst_menus`
4. `inst_role_menus`
5. `inst_department_roles`
6. `institution_employee_roles` 旧结构中的 `role_id` 用法
7. `institution_department_roles` 旧结构中的 `role_id` 用法

### 2.2 后端模块

1. `internal/rbac`
2. `internal/tenant`
3. `internal/sysconfig`
4. `internal/employee`
5. `internal/department`
6. `internal/recording`
7. `internal/badge`
8. `internal/sandbox`
9. `internal/store/compat_migration.go`

### 2.3 前端模块

1. `lingce-web/apps/institution`
2. `lingce-web/apps/operation`

### 2.4 Worker / 其他服务

1. `lingce-worker`
2. `recording-worker`

## 3. 执行阶段

## 3.1 阶段 A：设计冻结与脏数据盘点

目标：

1. 固定最终表结构
2. 盘点开发库 / 测试库 / 生产库的旧表依赖与脏数据规模
3. 输出迁移前审计报告

交付物：

1. 本文档
2. 统一审计 SQL
3. 角色编码白名单
4. 菜单编码白名单

必须确认：

1. `lingce_sales` 是否保留
2. 多角色员工收敛优先级
3. 默认管理员菜单基线

## 3.2 阶段 B：数据库结构准备

目标：

1. 不改业务代码
2. 先把终态表结构补齐

### 3.2.1 新增 / 改造 SQL 脚本建议

建议新增以下脚本：

1. `scripts/sql/2026-06-04_prepare_institution_permission_unification.sql`
2. `scripts/sql/2026-06-04_audit_institution_permission_unification.sql`
3. `scripts/sql/2026-06-04_backfill_institution_roles.sql`
4. `scripts/sql/2026-06-04_backfill_institution_menus.sql`
5. `scripts/sql/2026-06-04_backfill_institution_role_menus.sql`
6. `scripts/sql/2026-06-04_backfill_institution_employee_roles.sql`
7. `scripts/sql/2026-06-04_backfill_institution_department_roles.sql`
8. `scripts/sql/2026-06-04_finalize_institution_permission_unification.sql`

### 3.2.2 结构改造内容

#### `institution_employee_roles`

需要补：

1. `tenant_id bigint`
2. `role_code varchar(64)`
3. `source varchar(32)`
4. `updated_at timestamp`

需要移除旧约束影响：

1. 旧 `employee_id -> role_id` 单链路假设

最终约束：

1. `unique (tenant_id, employee_id)`

#### `institution_menus`

需要新建。

#### `institution_role_menus`

需要新建。

字段：

1. `tenant_id`
2. `role_code`
3. `menu_code`
4. `created_at`
5. `updated_at`

#### `institution_department_roles`

需要补：

1. `tenant_id bigint`
2. `role_code varchar(64)`
3. `updated_at timestamp`

## 3.3 阶段 C：数据迁移与回填

目标：

1. 在不切主读写代码的前提下，把终态表数据准备完整

### 3.3.1 角色定义迁移

来源：

1. `institution_roles`
2. `inst_roles`

规则：

1. 以 `institution_roles` 为主
2. 用 `inst_roles` 补齐缺失编码
3. 所有编码转小写并去空格
4. 废弃角色单独列清单，不自动合并

输出：

1. 每租户最终角色定义数
2. 无法归类角色编码清单

### 3.3.2 菜单字典迁移

来源：

1. `inst_menus`

迁移规则：

1. `code` 原样迁移
2. `parent_id` 通过旧菜单映射成 `parent_code`
3. `order_index -> sort_order`
4. 保留 `feature_code`
5. 保留 `feature_name`
6. 保留 `is_feature_assignable`
7. 保留 `is_default_for_admin`

输出：

1. 旧菜单数与新菜单数
2. 无法映射父菜单清单

### 3.3.3 角色菜单授权迁移

来源：

1. `inst_role_menus`
2. `inst_menus`

迁移规则：

1. `menu_id -> inst_menus.code`
2. 写入 `institution_role_menus.menu_code`
3. `role_code` 原样迁移
4. 去重写入

输出：

1. 每租户每角色菜单数
2. 无法映射 menu_id 清单

### 3.3.4 员工当前角色迁移

来源优先级：

1. `inst_employee_roles`
2. `institution_employee_roles`

迁移规则：

1. 先把 `inst_employee_roles` 视为主源
2. 将其写入新版 `institution_employee_roles(tenant_id, employee_id, role_code, source)`
3. 若 `institution_employee_roles` 旧结构存在且冲突，单独列清单

特别规则：

1. 一员工多角色不直接自动吞并
2. 必须按优先级规则收敛

### 3.3.5 部门默认角色迁移

来源：

1. `institution_department_roles`
2. `inst_department_roles`

规则：

1. 统一写成 `tenant_id + department_id + role_code`
2. 若两边冲突，列清单人工处理

## 3.4 阶段 D：后端代码切换

目标：

1. 所有读写从旧表切到终态表

### 3.4.1 `internal/rbac`

必须改：

1. 删除 `hasInstitutionRolesTable`
2. 删除 `hasInstitutionMenusTable`
3. 删除 legacy role hash id 兼容
4. 删除 `inst_role_menus` 作为 canonical store 的逻辑
5. 所有角色菜单读写改成 `institution_role_menus`
6. 所有菜单 CRUD 改成 `institution_menus`
7. 所有员工角色读写改成新版 `institution_employee_roles`
8. 所有部门默认角色读写改成新版 `institution_department_roles`

### 3.4.2 `internal/tenant`

必须改：

1. 新建租户时默认 admin 角色写 `institution_employee_roles`
2. 默认菜单写 `institution_role_menus`
3. 不再写 `inst_role_menus`

### 3.4.3 `internal/sysconfig`

必须改：

1. feature options 改读 `institution_menus`
2. 套餐功能组只提供菜单上限
3. 删除自动向角色补菜单的逻辑
4. 删除功能包变更时对 `inst_role_menus` 的同步逻辑

### 3.4.4 `internal/employee`

必须改：

1. 列表、详情、按角色筛选全部改读新版 `institution_employee_roles`

### 3.4.5 `internal/department`

必须改：

1. 部门默认角色展示与保存改读写新版 `institution_department_roles`

### 3.4.6 `internal/recording` / `badge` / `sandbox`

必须改：

1. 所有角色判断统一从新版 `institution_employee_roles` 取 `role_code`

## 3.5 阶段 E：前端切换

### 3.5.1 `apps/institution`

必须改：

1. 菜单页改为后端 `institution_menus` 返回结果
2. 角色权限页改为后端真实菜单字典，不再用本地 manifest 当真相
3. 删除“空角色菜单=继承租户菜单”的逻辑
4. 路由守卫改为严格交集
5. 快捷入口、侧边栏、页面直达统一同一套判定

### 3.5.2 `apps/operation`

必须改：

1. 功能包菜单选项改用后端 `institution_menus`
2. 减少前端硬编码 feature grouping 逻辑
3. 如果仍保留 grouping，只作为展示分组，不能作为权限语义来源

## 3.6 阶段 F：统一权限校验

目标：

1. 后端成为权限真相

必须新增：

1. 统一权限计算函数
2. 统一有效菜单计算函数
3. 敏感接口后端强校验

至少覆盖：

1. 角色管理
2. 菜单管理
3. 员工角色管理
4. 部门默认角色管理
5. 套餐 / 功能包配置

## 3.7 阶段 G：观察与下线旧表

目标：

1. 所有读写稳定后，移除旧表与兼容代码

动作：

1. 停写旧表
2. 保留只读观察期
3. 对账连续通过
4. 删除旧代码分支
5. 删除旧表

## 4. 脏数据处理规则

## 4.1 多角色员工收敛规则

建议优先级：

1. `manual` 高于 `department`
2. `admin` 高于普通角色
3. 若同一员工存在多个普通角色，输出人工清单

### 4.1.1 不允许自动判定的情况

1. `doctor` 与 `employee`
2. `employee` 与 `operating_manager`
3. `admin` 与 `marketing_manager`

这些都要列清单，不要脚本静默覆盖。

## 4.2 无法映射角色编码

当前开发库已确认：

1. `lingce_sales`

规则：

1. 不自动删除
2. 必须先决定保留还是废弃
3. 若保留，先补到 `institution_roles`
4. 若废弃，输出受影响员工清单与替代规则

## 4.3 菜单编码异常

规则：

1. 所有 `inst_role_menus.menu_id` 必须可映射到 `inst_menus.code`
2. 所有 `tenant_feature_group_items.item_code` 必须可映射到 `institution_menus.code`
3. 否则阻断 final cutover

## 5. SQL 脚本清单

## 5.1 建议新增脚本

1. `2026-06-04_prepare_institution_permission_unification.sql`
2. `2026-06-04_audit_institution_permission_unification.sql`
3. `2026-06-04_backfill_institution_roles.sql`
4. `2026-06-04_backfill_institution_menus.sql`
5. `2026-06-04_backfill_institution_role_menus.sql`
6. `2026-06-04_backfill_institution_employee_roles.sql`
7. `2026-06-04_backfill_institution_department_roles.sql`
8. `2026-06-04_finalize_institution_permission_unification.sql`

## 5.2 需复用的旧脚本参考

1. `2026-05-11_role_source_unification_audit.sql`
2. `2026-05-11_role_source_unification_backfill.sql`
3. `2026-05-11_role_source_unification_deduplicate_inst_employee_roles.sql`
4. `2026-05-12_backfill_menu_feature_flags.sql`
5. `2026-05-12_seed_frontdesk_and_feature_group_menu_codes.sql`

注意：

1. 这些旧脚本只能参考逻辑
2. 不可直接作为最终版执行
3. 因为本次目标已变为全 `institution_*`

## 6. 对账 SQL 清单

每次迁移演练后必须跑以下对账：

1. `institution_roles` 与角色编码白名单对账
2. `institution_menus` 与前端菜单清单对账
3. `institution_role_menus` 与 `inst_role_menus` 菜单数对账
4. `institution_employee_roles` 与 `inst_employee_roles` 员工数对账
5. `institution_department_roles` 与 `inst_department_roles` 部门默认角色对账
6. `tenant_feature_group_items` 是否全可映射 `institution_menus.code`
7. 每租户有效菜单数抽样对账

建议把对账 SQL 独立保存成：

1. `scripts/sql/2026-06-04_check_institution_permission_unification.sql`

## 7. 测试与验收

## 7.1 开发库验收

必须回归：

1. 角色列表
2. 菜单列表
3. 角色授权保存
4. 员工改角色
5. 部门默认角色
6. 默认管理员初始化
7. 套餐功能包绑定
8. 侧边栏显示
9. 路由守卫
10. 录音业务分域

## 7.2 测试 / 预发验收

必须回归：

1. 新建租户
2. 默认 admin 登录
3. 修改员工角色后 worker 生效
4. 功能包降配后页面即时收缩
5. 空角色菜单用户无法进入任意业务页

## 7.3 生产验收

必须观察：

1. API 500
2. worker 分析失败
3. 权限页面报错
4. 租户菜单异常放开
5. 角色保存后菜单不生效

## 8. 上线步骤

## 8.1 第一次上线

内容：

1. 仅上结构准备 SQL
2. 不切代码
3. 跑审计

## 8.2 第二次上线

内容：

1. 跑回填 SQL
2. 跑对账 SQL
3. 处理冲突清单

## 8.3 第三次上线

内容：

1. 切后端读写到 `institution_*`
2. 切 worker 读写到 `institution_*`
3. 切前端到新接口语义

## 8.4 第四次上线

内容：

1. 停写旧表
2. 保留观察期

## 8.5 最终上线

内容：

1. 删除旧兼容代码
2. 删除旧表

## 9. 回滚方案

### 9.1 结构阶段回滚

1. 新表保留
2. 业务代码不切换即可回滚

### 9.2 数据阶段回滚

1. 回填脚本必须幂等
2. 回填前保留快照或备份

### 9.3 代码切换阶段回滚

1. 旧表仍在时，允许回滚到旧代码
2. 不允许在未完成观察前删除旧表

### 9.4 最终删除阶段

1. 旧表删除前必须确认至少一个观察周期无异常
2. 删除旧表后，不再承诺低成本回滚

## 10. 当前执行建议

建议按以下实际顺序开始：

1. 先写结构准备 SQL
2. 再写审计 SQL
3. 跑开发库演练
4. 输出开发库冲突清单
5. 再开始代码改造

原因：

1. 当前开发库已经证明数据并存且分叉
2. 如果先改代码，不先把迁移与脏数据规则落地，后面会反复返工

## 11. 文档关系

1. 总方案：`docs/INSTITUTION_PERMISSION_UNIFICATION_PLAN.md`
2. 本文档：`docs/INSTITUTION_PERMISSION_UNIFICATION_EXECUTION_PLAN.md`

后续还应补充：

1. SQL 脚本设计稿
2. 后端改造任务单
3. 前端改造任务单
4. 验收清单
