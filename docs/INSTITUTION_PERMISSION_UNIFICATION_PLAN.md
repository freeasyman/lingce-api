# 机构端权限体系一次性收口方案

本文档用于冻结机构端菜单权限体系的最终模型与实施顺序，目标是一次性收口当前 `inst_*` / `institution_*` 混合、半迁移、语义不一致的问题。

本文档不是代码草稿，而是后续数据库迁移、后端改造、前端改造、联调与上线的统一依据。

## 1. 目标

本次改造完成后，系统必须满足：

1. 机构端权限体系只保留一套命名：`institution_*`
2. 员工当前角色只有一个真源
3. 角色定义只有一个真源
4. 菜单字典只有一个真源
5. 角色菜单授权只有一个真源
6. 套餐/功能包只表达租户菜单上限，不再隐式给角色补权
7. 前端菜单显示、路由守卫、后端权限判断三者基于同一套事实
8. 空配置一律 fail-closed，不允许再出现“空角色=全权限”或“未绑功能包=全权限”

## 2. 当前开发库现状

开发库：`lingce_dev`

截至 2026-06-04，实际数据库状态如下：

### 2.1 已存在的表

1. `institution_employee_roles`
2. `institution_roles`
3. `institution_department_roles`
4. `inst_employee_roles`
5. `inst_roles`
6. `inst_menus`
7. `inst_role_menus`
8. `inst_department_roles`
9. `tenant_feature_groups`
10. `tenant_feature_group_items`
11. `tenant_feature_assignments`
12. `tenant_feature_overrides`

### 2.2 不存在的表

1. `institution_menus`
2. `institution_role_menus`

### 2.3 已确认的数据分叉

1. `institution_employee_roles` 有 42 行
2. `inst_employee_roles` 有 73 行
3. 两边完全对上的只有 24 条
4. `inst_employee_roles` 独有 49 条
5. `institution_employee_roles` 独有 18 条
6. `inst_role_menus` 有 1604 行
7. `institution_role_menus` 不存在
8. `inst_menus` 有 102 行
9. `institution_menus` 不存在

### 2.4 已确认的脏数据

1. `inst_employee_roles` 存在一员工多角色
2. 存在无法映射到 `institution_roles` 的角色编码：`lingce_sales`
3. 菜单授权真实运行面仍落在 `inst_role_menus`
4. 菜单字典真实运行面仍落在 `inst_menus`

结论：当前开发库不是“新表已接管，旧表待删除”，而是“角色事实、角色定义、菜单字典、菜单授权分别分布在不同表层，且已经发生分叉”。

## 3. 最终模型

本次收口后的最终权限模型全部统一为 `institution_*` 前缀。

### 3.1 员工当前角色真源

表：`institution_employee_roles`

建议字段：

1. `tenant_id bigint not null`
2. `employee_id bigint not null`
3. `role_code varchar(64) not null`
4. `source varchar(32) not null`
5. `created_at timestamp not null`
6. `updated_at timestamp not null`

建议约束：

1. `unique (tenant_id, employee_id)`

说明：

1. 不再使用 `role_id`
2. 当前角色事实直接用 `role_code`
3. 这是为了适配 worker、analysis、badge、sandbox 等大量按角色编码判断业务的链路

### 3.2 角色定义真源

表：`institution_roles`

建议字段：

1. `id bigserial primary key`
2. `tenant_id bigint not null`
3. `code varchar(64) not null`
4. `name text not null`
5. `description text`
6. `is_active boolean not null default true`
7. `created_at timestamp not null`
8. `updated_at timestamp not null`
9. `deleted_at timestamp`

建议约束：

1. `unique (tenant_id, code) where deleted_at is null`

职责：

1. 只承担角色定义
2. 不再承担员工当前角色事实

### 3.3 菜单字典真源

表：`institution_menus`

建议字段：

1. `id bigserial primary key`
2. `tenant_id bigint`
3. `code varchar(128) not null`
4. `name text not null`
5. `path text`
6. `icon text`
7. `parent_code varchar(128)`
8. `sort_order integer not null default 0`
9. `is_active boolean not null default true`
10. `is_feature_assignable boolean not null default false`
11. `is_default_for_admin boolean not null default false`
12. `feature_code varchar(128)`
13. `feature_name varchar(128)`
14. `created_at timestamp not null`
15. `updated_at timestamp not null`
16. `deleted_at timestamp`

建议约束：

1. `unique (tenant_id, code) where deleted_at is null`

说明：

1. `tenant_id is null` 表示公共菜单
2. 当前阶段默认只维护公共菜单
3. `parent_code` 比 `parent_id` 更适合迁移、对账和跨环境同步

### 3.4 角色菜单授权真源

表：`institution_role_menus`

建议字段：

1. `tenant_id bigint not null`
2. `role_code varchar(64) not null`
3. `menu_code varchar(128) not null`
4. `created_at timestamp not null`
5. `updated_at timestamp not null`

建议约束：

1. `unique (tenant_id, role_code, menu_code)`

说明：

1. 不使用 `role_id + menu_id`
2. 直接使用业务键 `role_code + menu_code`
3. 避免再次引入 ID/code 混用

### 3.5 部门默认角色真源

表：`institution_department_roles`

建议字段：

1. `tenant_id bigint not null`
2. `department_id bigint not null`
3. `role_code varchar(64) not null`
4. `is_default boolean not null default true`
5. `created_at timestamp not null`
6. `updated_at timestamp not null`

建议约束：

1. `unique (tenant_id, department_id)`

### 3.6 套餐 / 功能包层

保留现有：

1. `tenant_feature_groups`
2. `tenant_feature_group_items`
3. `tenant_feature_assignments`
4. `tenant_feature_overrides`

职责：

1. 只表达“某租户菜单能力上限”
2. 不再承担角色补权

## 4. 最终权限语义

### 4.1 有效菜单公式

固定公式：

`effective_menu_codes = tenant_allowed_menu_codes ∩ role_granted_menu_codes ∩ active_menu_codes`

其中：

1. `tenant_allowed_menu_codes` 来自 `tenant_feature_*`
2. `role_granted_menu_codes` 来自 `institution_role_menus`
3. `active_menu_codes` 来自 `institution_menus`

### 4.2 必须冻结的语义

1. 角色没有菜单授权时，结果为空
2. 租户没有功能包分配时，结果为空
3. 不允许再通过“空配置”隐式放开权限
4. 前端只做展示，不做权限真相推断

## 5. 初始化与变更动作

### 5.1 新建租户

必须执行：

1. 创建 `institution_roles` 中的默认 `admin`
2. 创建默认管理员员工
3. 向 `institution_employee_roles` 写入 `admin`
4. 按 `institution_menus.is_default_for_admin = true` 初始化 `institution_role_menus`

### 5.2 修改员工角色

只改：

1. `institution_employee_roles`

不改：

1. `institution_roles`
2. `institution_role_menus`

### 5.3 修改角色菜单

只改：

1. `institution_role_menus`

### 5.4 修改套餐 / 功能包

只改：

1. `tenant_feature_*`

不再执行：

1. 自动往角色菜单表补数据
2. 自动给角色追加菜单授权

### 5.5 降配或解绑功能包

只影响：

1. `tenant_allowed_menu_codes`

不回写：

1. `institution_role_menus`

原因：

1. 角色授权只是候选授权
2. 真正生效由交集公式决定

## 6. 数据迁移原则

### 6.1 总原则

1. 先建终态表
2. 先迁数据
3. 再切代码
4. 最后删旧表

### 6.2 不允许的做法

1. 长期双写
2. 边迁移边改权限语义
3. 用前端规则补后端缺口
4. 未对账就删旧表

## 7. 一次性迁移顺序

### 阶段 1：结构准备

1. 新建终态版 `institution_menus`
2. 新建终态版 `institution_role_menus`
3. 改造 `institution_employee_roles`
   - 增加 `tenant_id`
   - 增加 `role_code`
   - 增加 `source`
   - 增加 `updated_at`
4. 改造 `institution_department_roles`
   - 增加 `tenant_id`
   - 增加 `role_code`
   - 增加 `updated_at`

### 阶段 2：角色定义迁移

来源：

1. `institution_roles`
2. `inst_roles`

动作：

1. 以 `institution_roles` 为主集合
2. 补齐 `inst_roles` 中仍需保留的角色编码
3. 清理废弃角色编码
4. 冻结角色编码字典

特别处理：

1. `lingce_sales` 必须显式决策
2. 不能让其继续成为“只存在于事实层、不存在于定义层”的角色

### 阶段 3：菜单字典迁移

来源：

1. `inst_menus`

目标：

1. `institution_menus`

动作：

1. `menu_id -> code`
2. `parent_id -> parent_code`
3. 保留 `is_feature_assignable`
4. 保留 `is_default_for_admin`
5. 保留 `feature_code / feature_name`

### 阶段 4：角色菜单授权迁移

来源：

1. `inst_role_menus`
2. `inst_menus`

目标：

1. `institution_role_menus`

映射方式：

1. `tenant_id` 原样
2. `role_code` 原样
3. `menu_id -> inst_menus.code -> institution_menus.code`

### 阶段 5：员工角色事实迁移

来源优先级：

1. `inst_employee_roles`
2. `institution_employee_roles`

动作：

1. 以 `inst_employee_roles` 为主源
2. 把数据回填到新版 `institution_employee_roles`
3. 对冲突记录人工决策

### 阶段 6：部门默认角色迁移

来源：

1. `institution_department_roles`
2. `inst_department_roles`

目标：

1. 统一成 `tenant_id + department_id + role_code`

### 阶段 7：代码切换

顺序建议：

1. 后端写路径
2. 后端读路径
3. worker / analysis / badge / sandbox
4. institution 前端
5. operation 前端

### 阶段 8：观察期

1. 只读写 `institution_*`
2. `inst_*` 停写但保留
3. 观察并对账

### 阶段 9：最终下线

1. 删除旧表依赖代码
2. 删除兼容分支
3. 删除旧表

## 8. 脏数据收敛规则

### 8.1 一员工多角色

开发库已确认存在一员工多角色。

建议规则：

1. `manual` 优先于 `department`
2. `admin` 高于普通角色
3. 若同时存在多个同优先级角色，列清单人工处理

### 8.2 无法映射角色编码

当前已发现：

1. `lingce_sales`

规则：

1. 不自动丢弃
2. 先人工确认是保留还是废弃
3. 迁移脚本必须输出异常清单

### 8.3 双表冲突

若 `inst_employee_roles` 与 `institution_employee_roles` 冲突：

1. 默认以 `inst_employee_roles` 为准
2. 但必须输出冲突清单供人工复核

## 9. 对账要求

每次迁移演练必须输出以下对账结果：

1. 每租户角色定义数量
2. 每租户员工角色数量
3. 每租户部门默认角色数量
4. 每角色菜单授权数量
5. 每租户功能包菜单数量
6. 员工实际有效菜单数量抽样
7. 无法映射角色编码清单
8. 无法映射菜单编码清单
9. 多角色员工清单

## 10. 上线顺序

### 10.1 开发库

1. 完整演练迁移
2. 跑对账
3. 修正脚本

### 10.2 测试 / 预发

1. 做一次真实数据迁移
2. 回归角色管理、菜单管理、租户套餐、录音业务链路

### 10.3 生产

分三步：

1. 建新表与迁移数据
2. 切代码读写
3. 观察稳定后删除旧表

## 11. 回滚原则

1. 数据迁移与代码切换分批执行
2. 在旧表未删除前，必须可回滚到旧代码
3. 回滚不允许依赖人工重新拼数据
4. 旧表删除必须作为最后一步

## 12. 当前冻结结论

截至本文档版本，冻结以下结论：

1. 最终命名全部统一为 `institution_*`
2. 员工当前角色真源定为 `institution_employee_roles(role_code)`
3. 角色定义真源定为 `institution_roles`
4. 菜单字典真源定为 `institution_menus`
5. 角色菜单授权真源定为 `institution_role_menus(role_code, menu_code)`
6. 部门默认角色真源定为 `institution_department_roles(role_code)`
7. 套餐/功能包只控制租户菜单上限
8. 有效权限统一使用交集公式
9. 空配置统一 fail-closed

后续所有数据库脚本、后端代码、前端代码、worker 改造都必须以本方案为准。
