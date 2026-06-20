# 机构端权限体系代码切换清单

本文档是机构端菜单权限全链路重构的代码切换清单。

它不再讨论“方向”，只回答以下问题：

1. 后端哪些读写点必须切到最终真源
2. 前端哪些 fail-open 逻辑必须移除
3. 运营平台、机构端、worker 如何按阶段切换
4. 哪些兼容逻辑必须最后删除

配套交付物：

1. 审计 SQL：
   [2026-06-07_audit_institution_permission_unification.sql](/Users/yiliiang/Documents/lingce-api/scripts/sql/2026-06-07_audit_institution_permission_unification.sql)
2. 终态 DDL：
   [2026-06-07_prepare_institution_permission_unification.sql](/Users/yiliiang/Documents/lingce-api/scripts/sql/2026-06-07_prepare_institution_permission_unification.sql)
3. 上位总纲：
   [INSTITUTION_MENU_PERMISSION_REARCHITECTURE.md](/Users/yiliiang/Documents/lingce-api/docs/INSTITUTION_MENU_PERMISSION_REARCHITECTURE.md)

## 1. 切换总原则

1. 先补终态表结构，再切代码主读写
2. 先让后端产出单一权限事实，再让前端被动消费
3. 套餐层必须先去除对角色授权层的隐式写入
4. 所有空配置语义一律改成 fail-closed
5. 旧表删除必须在读写切换、对账、回归全部完成后执行

## 2. 最终真源冻结

代码切换前，必须先冻结最终真源，不再允许讨论“临时主源”。

最终真源如下：

1. 员工当前角色事实：
   `institution_employee_roles`
2. 角色定义：
   `institution_roles`
3. 菜单字典：
   `institution_menus`
4. 角色菜单授权：
   `institution_role_menus`
5. 部门默认角色：
   `institution_department_roles`
6. 租户菜单上限：
   `tenant_feature_groups` / `tenant_feature_group_items` / `tenant_feature_assignments` / `tenant_feature_overrides`

最终固定公式：

`effective_menu_codes = tenant_allowed_menu_codes ∩ role_granted_menu_codes ∩ active_menu_codes`

## 3. 后端切换清单

## 3.1 套餐层

责任模块：

1. `internal/sysconfig`
2. `internal/rbac`
3. `internal/tenant`

必须修改：

1. 禁止 `AssignFeatureGroupToTenant` 再隐式同步角色菜单授权
2. `GetTenantAllowedMenuCodes` 在未绑定功能组时，不得再返回 unrestricted
3. `GetEffectiveFeaturePolicy` 在无有效功能组时，不得再默认放开
4. 运营平台只修改租户菜单上限事实，不再触碰角色菜单真源

完成标准：

1. 套餐层只表达 tenant upper bound
2. 套餐改动不会改写角色菜单事实
3. 空功能组状态对机构端表现为 fail-closed

## 3.2 角色定义与角色菜单授权层

责任模块：

1. `internal/rbac/store_institution.go`
2. `internal/store/compat_migration.go`

必须修改：

1. 新增 `institution_menus` 主读写
2. 新增 `institution_role_menus` 主读写
3. `GetInstitutionRolePermissions` 读 `institution_menus + institution_role_menus`
4. `AssignPermissionsToInstitutionRole` 写 `institution_role_menus`
5. `RemovePermissionsFromInstitutionRole` 删 `institution_role_menus`
6. 所有 `inst_role_menus` 注释、兜底和 canonical 标记全部退出

完成标准：

1. 角色定义与角色菜单授权进入同一 `institution_*` 体系
2. `inst_role_menus` 不再承担运行时主授权职责
3. `inst_menus` 不再承担菜单字典真源职责

## 3.3 员工当前角色事实层

责任模块：

1. `internal/rbac`
2. `internal/employee`
3. `internal/department`
4. `internal/tenant`
5. `internal/recording`
6. `internal/badge`
7. `internal/sandbox`

必须修改：

1. 所有主读路径统一读 `institution_employee_roles(tenant_id, employee_id, role_code)`
2. 所有主写路径统一写 `institution_employee_roles`
3. `SetEmployeeRole` 必须按 `tenant_id + employee_id` 覆盖单条当前角色事实
4. `RemoveEmployeeRole` 必须删除该员工当前角色事实
5. 部门默认角色应用逻辑只生成一条最终角色事实，不再容忍多角色叠加

完成标准：

1. 一个员工只有一个当前角色真源
2. worker / API / institution frontend 看到同一角色事实
3. `inst_employee_roles` 不再参与业务主读主写

## 3.4 有效菜单裁决层

责任模块：

1. `internal/rbac`
2. `internal/auth`
3. 机构端相关 handler / service

必须新增：

1. 统一的 `GetEmployeeEffectiveMenuCodes`
2. 统一的 `GetRoleGrantedMenuCodes`
3. 统一的 `GetTenantAllowedMenuCodes`
4. 统一的后端菜单裁决输出 DTO

必须修改：

1. 前端不再分别请求“租户菜单”和“角色菜单”后自行推导
2. 敏感菜单对应接口增加后端权限校验
3. 角色为空、套餐为空、菜单停用都必须返回空权限，而不是默认放开

完成标准：

1. 后端成为菜单权限最终裁决者
2. 前端只消费最终权限结果

## 4. 前端切换清单

## 4.1 机构端前端

责任模块：

1. `apps/institution/src/routes/__root.tsx`
2. `apps/institution/src/routes/roles/index.tsx`
3. `apps/institution/src/menu-manifest.ts`
4. `apps/institution/src/navigation.tsx`

必须修改：

1. 删除“空角色菜单 = 租户菜单全可见”的回退逻辑
2. 删除路由守卫中“无显式角色授权则只看租户菜单”的回退逻辑
3. 角色页不再把 `!policy || unrestricted` 解释为“所有本地菜单可选”
4. 侧边栏、快捷入口、路由守卫统一消费同一套 `effective_menu_codes`

允许保留：

1. 本地 manifest 作为纯展示元数据
2. path 到 menu code 的纯静态映射

禁止继续保留：

1. 前端自己发明权限语义
2. 前端决定空配置是否放开

完成标准：

1. institution 前端全链路 fail-closed
2. 菜单显示与路由守卫使用同一权限事实

## 4.2 运营平台前端

责任模块：

1. `apps/operation`

必须修改：

1. 套餐页只编辑租户菜单上限，不再假设会同步角色权限
2. 套餐保存后的预期文案和交互说明必须更新
3. 若租户未绑定功能组，运营端必须显式展示“未授权”而不是默认全开

## 5. Worker 切换清单

责任模块：

1. `lingce-worker`
2. `recording-worker`

必须修改：

1. 所有按员工当前角色判断分析路由、数据分域、badge/sandbox 逻辑的代码，统一读 `institution_employee_roles.role_code`
2. 不再把 `inst_employee_roles` 当主真源
3. 若 worker 内部需要角色定义映射，统一走 `institution_roles.code`

完成标准：

1. worker 与 API 在角色事实上完全一致
2. 不再出现“后台改角色成功，但分析链路仍按旧角色运行”的情况

## 6. 切换顺序

严格顺序如下：

1. 执行审计 SQL，冻结问题规模
2. 执行终态 DDL，补齐 `institution_menus` / `institution_role_menus` 等结构
3. 回填新表数据，不切主读写
4. 切套餐层，移除角色菜单隐式同步
5. 切角色菜单授权主读写
6. 切员工当前角色主读写
7. 新增统一有效菜单裁决接口
8. 切 institution 前端到统一权限接口
9. 切 worker 角色主读路径
10. 对账并回归
11. 删除旧兼容逻辑
12. 删除旧表

## 7. 必须删除的兼容逻辑

以下逻辑不允许长期保留：

1. `inst_*` / `institution_*` 双读双写
2. 套餐层同步角色菜单
3. 无功能组即 unrestricted
4. 空角色授权即继承租户菜单
5. 角色定义在新表、菜单授权在旧表的混搭读取
6. `role_id` 与 `role_code` 并存为长期主键语义
7. `menu_id` 与 `menu_code` 并存为长期主键语义

## 8. 验收标准

改造完成后，必须同时满足：

1. 任意员工最终可见菜单可由单一公式稳定推导
2. 套餐层、角色层、员工当前角色层职责边界清晰
3. institution 前端不存在 fail-open
4. 后端敏感接口存在服务端权限校验
5. worker 与 API 读取同一员工角色事实
6. `inst_employee_roles`、`inst_menus`、`inst_role_menus`、`inst_department_roles` 全部退出主链路
7. 最终体系命名统一为 `institution_*`

## 9. 下一步实施建议

基于本文档，下一批直接开发交付物应为：

1. 数据回填 SQL：
   `backfill_institution_roles`
   `backfill_institution_menus`
   `backfill_institution_role_menus`
   `backfill_institution_employee_roles`
   `backfill_institution_department_roles`
2. 后端统一权限裁决 service
3. institution 前端 fail-open 移除补丁
4. worker 角色读路径切换补丁
