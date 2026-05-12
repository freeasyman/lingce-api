# 角色真源统一开发计划

本文档不是对外说明，而是本次改造的执行手册。目标是把 Go 体系员工当前业务角色统一到 `inst_employee_roles`，并在改造完成后通过重命名 `institution_employee_roles` 强制暴露残留依赖。

## 1. 目标

本次改造完成后，系统必须满足：

1. 员工当前业务角色唯一真源是 `inst_employee_roles`
2. 一个员工只存在一个当前业务角色
3. `institution_roles` 继续保留，承担角色定义、角色名、角色菜单/权限绑定
4. `institution_employee_roles` 不再参与业务主读主写
5. `lingce-api`、`lingce-worker`、`recording-worker` 对同一员工看到同一角色事实

## 2. 范围

本次直接负责的代码仓库：

1. `lingce-api`
2. `lingce-worker`

本次需要联动核验但暂不在本仓直接开发的仓库：

1. `recording-worker`

本次高优先级链路：

1. 租户创建默认管理员
2. 员工角色设置 / 删除
3. 部门默认角色应用到员工
4. 员工列表 / 详情角色展示
5. RBAC 员工权限判定
6. 录音列表角色分域
7. worker 正常分析 / 重放分析角色判路由
8. badge / sandbox 角色判断

## 3. 事实模型

### 3.1 真源决策

真源定为：

`inst_employee_roles`

理由：

1. `lingce-worker` 路由解析直接读它
2. `lingce-worker` doctor 分支专科读取直接读它
3. `lingce-api` 录音、badge、sandbox 大量逻辑已经读它
4. 它携带 `role_code`，更贴近录音分析和业务判断

### 3.2 单角色语义

业务语义统一为：

1. 一个员工只有一个当前业务角色
2. 所有主路径读取都按“当前角色”工作
3. `inst_employee_roles` 未来要收敛成每员工唯一有效记录

过渡期可接受物理上仍有历史脏数据，但主写与主读都要按单角色模型实现。

### 3.3 保留的定义层

以下结构继续保留：

1. `institution_roles`
2. `institution_role_permissions`
3. `institution_department_roles`
4. `inst_role_menus`

解释：

1. 员工当前角色真相来自 `inst_employee_roles`
2. 角色定义、角色名、权限/菜单绑定仍来自角色定义层
3. 后续员工权限链改为 `inst_employee_roles.role_code -> institution_roles.code -> 权限/菜单`

## 4. 当前已知代码点

### 4.1 `institution_employee_roles` 直接读写点

`lingce-api`

1. [internal/tenant/store.go](/Users/yiliiang/Documents/lingce-api/internal/tenant/store.go)
2. [internal/rbac/store_institution.go](/Users/yiliiang/Documents/lingce-api/internal/rbac/store_institution.go)
3. [internal/rbac/checker.go](/Users/yiliiang/Documents/lingce-api/internal/rbac/checker.go)
4. [internal/employee/store.go](/Users/yiliiang/Documents/lingce-api/internal/employee/store.go)
5. [internal/recording/store.go](/Users/yiliiang/Documents/lingce-api/internal/recording/store.go)
6. [internal/store/compat_migration.go](/Users/yiliiang/Documents/lingce-api/internal/store/compat_migration.go)

### 4.2 `inst_employee_roles` 已有主读点

`lingce-api`

1. [internal/rbac/store_institution.go](/Users/yiliiang/Documents/lingce-api/internal/rbac/store_institution.go)
2. [internal/recording/store.go](/Users/yiliiang/Documents/lingce-api/internal/recording/store.go)
3. [internal/recording/service.go](/Users/yiliiang/Documents/lingce-api/internal/recording/service.go)
4. [internal/badge/store.go](/Users/yiliiang/Documents/lingce-api/internal/badge/store.go)
5. [internal/sandbox/service.go](/Users/yiliiang/Documents/lingce-api/internal/sandbox/service.go)

`lingce-worker`

1. [internal/repository/routes.go](/Users/yiliiang/Documents/lingce-worker/internal/repository/routes.go)
2. [internal/analysis/route_resolver.go](/Users/yiliiang/Documents/lingce-worker/internal/analysis/route_resolver.go)
3. [internal/analysis/doctor.go](/Users/yiliiang/Documents/lingce-worker/internal/analysis/doctor.go)

## 5. 实施总顺序

严格按以下顺序推进，不允许跳步：

1. 盘点与数据审计
2. 写路径统一
3. 主读路径统一
4. 数据校正
5. 租户管理专项回归
6. 测试环境断路
7. 残留修复
8. 生产切换

原因：

1. 先改读不改写会继续制造新脏数据
2. 先断路不补数据会把老租户直接打坏
3. 租户创建默认管理员链路是系统最敏感入口，必须单独回归

## 6. 开发任务拆分

### T1. 数据审计与前置 SQL

目标：

1. 找出 `inst_employee_roles` 缺失员工
2. 找出双表不一致员工
3. 找出 `inst_employee_roles` 多角色员工

需要产出：

1. 审计 SQL
2. 冲突清单 SQL
3. 回填 SQL 草案

注意：

1. 不先执行 destructive SQL
2. 先观察数据分布，再决定最终清理策略

### T2. 租户创建链路改造

目标：

新租户创建后，默认管理员角色必须主写到 `inst_employee_roles`。

修改文件：

1. [internal/tenant/store.go](/Users/yiliiang/Documents/lingce-api/internal/tenant/store.go)

具体动作：

1. `createDefaultTenantAdminTx` 中移除 `institution_employee_roles` 主写
2. 改为插入 `inst_employee_roles`
3. `source` 建议使用 `tenant_init`
4. 默认 admin 角色编码固定为 `admin`
5. 默认部门角色配置仍可继续写定义层，但不能再依赖 `institution_employee_roles`

风险：

1. 默认 admin 无法登录后访问租户内资源
2. 新租户创建后员工列表和权限为空
3. 后续功能组、profile、录音等页面无入口

完成标准：

1. 新租户创建后默认 admin 在 `inst_employee_roles` 中有且只有一条角色
2. 默认 admin 登录正常
3. 默认 admin 可以访问租户内核心管理接口

### T3. 员工角色写路径统一

目标：

员工改角色 / 删角色只操作 `inst_employee_roles`。

修改文件：

1. [internal/rbac/store_institution.go](/Users/yiliiang/Documents/lingce-api/internal/rbac/store_institution.go)

具体动作：

1. `SetEmployeeRole` 主写改为：
   1. 先查 `employee.tenant_id`
   2. 把 `roleID` 映射成 `roleCode`
   3. 删除该员工现有 `inst_employee_roles`
   4. 插入一条新角色，`source='manual'`
2. `RemoveEmployeeRole` 统一删除 `inst_employee_roles`
3. 不再把 `institution_employee_roles` 作为主存储

完成标准：

1. 页面改角色后 worker 立刻能读到
2. 页面删角色后 worker 立刻读不到
3. 不存在“页面显示改成功但分析链路没改”的情况

### T4. 部门默认角色应用链路统一

目标：

部门默认角色对员工的实际影响只通过 `inst_employee_roles` 落地。

修改文件：

1. [internal/rbac/store_institution.go](/Users/yiliiang/Documents/lingce-api/internal/rbac/store_institution.go)

具体动作：

1. `SetDepartmentRole` 中 `UpdateExisting=true` 时：
   1. 删除该部门员工旧的 `source='department'` 角色记录
   2. 如果采用“单员工单当前角色”强覆盖，则进一步删除员工其他角色记录并插入部门角色
   3. 明确当前策略，不能保持模糊
2. 部门默认角色配置层可继续保留 `institution_department_roles` 与 `inst_department_roles`
3. 员工角色事实层只认 `inst_employee_roles`

决策要求：

需要明确：

1. 部门批量套角色是否覆盖手工角色
2. 若员工已有 `manual` 角色，`update_existing=true` 是强覆盖还是只覆盖 `department`

当前建议：

1. 保持现有语义，先只覆盖 `source='department'`
2. 不在本次额外改变业务规则

原因：

避免在真源统一时顺手引入新的组织管理语义变化。

### T5. 员工展示读路径统一

目标：

员工列表、详情、批量员工查询全部改从 `inst_employee_roles` 读角色。

修改文件：

1. [internal/employee/store.go](/Users/yiliiang/Documents/lingce-api/internal/employee/store.go)

具体动作：

1. `ListEmployees` 角色筛选改为基于 `inst_employee_roles.role_code`
2. `role_code` 直接取 `inst_employee_roles.role_code`
3. `role_name` 优先按 `tenant_id + role_code` join `institution_roles`
4. 若 `institution_roles` 无匹配，再回退 `inst_roles`
5. `GetEmployeeByID`
6. `GetByIDs`

完成标准：

1. 员工列表展示角色和 worker 判路由角色一致
2. 角色筛选结果和录音分析角色一致

### T6. RBAC 员工角色读取统一

目标：

RBAC 不再通过 `institution_employee_roles` 找员工角色。

修改文件：

1. [internal/rbac/store_institution.go](/Users/yiliiang/Documents/lingce-api/internal/rbac/store_institution.go)
2. [internal/rbac/checker.go](/Users/yiliiang/Documents/lingce-api/internal/rbac/checker.go)

具体动作：

1. `GetEmployeeRole` 改为读取 `inst_employee_roles`
2. `PermissionChecker.GetEmployeePermissions` 改为：
   1. 读员工 `tenant_id`
   2. 从 `inst_employee_roles` 取当前 `role_code`
   3. join `institution_roles` on `tenant_id + code`
   4. 再 join `institution_role_permissions`
3. `HasPermission(userType=employee)` 同步改造

注意：

1. 必须使用 `tenant_id` 约束 `institution_roles`
2. 必须过滤 `deleted_at IS NULL`
3. 如果角色定义缺失，要返回明确错误或无权限

### T7. 录音读路径收口

目标：

录音列表角色分域最终只依赖 `inst_employee_roles`。

修改文件：

1. [internal/recording/store.go](/Users/yiliiang/Documents/lingce-api/internal/recording/store.go)

具体动作：

1. 删除对 `institution_employee_roles` 的兼容读取
2. 仅保留 `inst_employee_roles`
3. 继续保留 doctor/frontdesk/consultant 的优先级逻辑

注意：

1. 在确认数据对齐前，不立即删除兼容逻辑
2. 可以先把新表读取删掉，旧表作为唯一读取来源

### T8. 兼容迁移与数据回填

目标：

用一次性脚本把 `institution_employee_roles` 中仍有效但 `inst_employee_roles` 缺失的角色补回去。

建议规则：

1. `inst_employee_roles` 已存在时，以它为准
2. `inst_employee_roles` 缺失时，使用 `institution_employee_roles -> institution_roles.code` 回填
3. 双边不一致时，只出冲突清单，不自动覆盖

需要产出：

1. 幂等回填 SQL
2. 冲突清单 SQL
3. 多角色清理 SQL

已落地脚本：

1. [scripts/sql/2026-05-11_role_source_unification_audit.sql](/Users/yiliiang/Documents/lingce-api/scripts/sql/2026-05-11_role_source_unification_audit.sql)
2. [scripts/sql/2026-05-11_role_source_unification_backfill.sql](/Users/yiliiang/Documents/lingce-api/scripts/sql/2026-05-11_role_source_unification_backfill.sql)
3. [scripts/sql/2026-05-11_role_source_unification_deduplicate_inst_employee_roles.sql](/Users/yiliiang/Documents/lingce-api/scripts/sql/2026-05-11_role_source_unification_deduplicate_inst_employee_roles.sql)

### T9. 测试环境断路

目标：

通过重命名表暴露所有漏改点。

执行动作：

1. 测试环境执行：

```sql
ALTER TABLE institution_employee_roles RENAME TO institution_employee_roles_disabled;
```

2. 观察 API / worker / 脚本错误
3. 修复所有残留引用

要求：

1. 不创建兼容视图
2. 不保留同名镜像
3. 就让漏改直接炸

原因：

这是本次收口的关键验收手段。

### T10. 生产切换

建议拆两次发布：

第一次发布：

1. 上线写路径统一
2. 上线主读路径统一
3. 执行数据校正
4. 观察稳定性

第二次发布：

1. 生产重命名 `institution_employee_roles`
2. 观察日志
3. 修残留

## 7. 文件级待办清单

### `lingce-api`

#### 必改

1. [internal/tenant/store.go](/Users/yiliiang/Documents/lingce-api/internal/tenant/store.go)
2. [internal/rbac/store_institution.go](/Users/yiliiang/Documents/lingce-api/internal/rbac/store_institution.go)
3. [internal/rbac/checker.go](/Users/yiliiang/Documents/lingce-api/internal/rbac/checker.go)
4. [internal/employee/store.go](/Users/yiliiang/Documents/lingce-api/internal/employee/store.go)
5. [internal/recording/store.go](/Users/yiliiang/Documents/lingce-api/internal/recording/store.go)

#### 需要确认但大概率只做验证

1. [internal/badge/store.go](/Users/yiliiang/Documents/lingce-api/internal/badge/store.go)
2. [internal/sandbox/service.go](/Users/yiliiang/Documents/lingce-api/internal/sandbox/service.go)
3. [internal/recording/service.go](/Users/yiliiang/Documents/lingce-api/internal/recording/service.go)
4. [internal/store/compat_migration.go](/Users/yiliiang/Documents/lingce-api/internal/store/compat_migration.go)

#### 路由/接口层一般无需逻辑修改，但需回归

1. [internal/tenant/handler.go](/Users/yiliiang/Documents/lingce-api/internal/tenant/handler.go)
2. [internal/employee/handler.go](/Users/yiliiang/Documents/lingce-api/internal/employee/handler.go)
3. [internal/rbac/handler.go](/Users/yiliiang/Documents/lingce-api/internal/rbac/handler.go)
4. [internal/rbac/handler_institution.go](/Users/yiliiang/Documents/lingce-api/internal/rbac/handler_institution.go)
5. [internal/organization/handler.go](/Users/yiliiang/Documents/lingce-api/internal/organization/handler.go)

### `lingce-worker`

#### 预计无需改主逻辑，但要核验

1. [internal/repository/routes.go](/Users/yiliiang/Documents/lingce-worker/internal/repository/routes.go)
2. [internal/analysis/route_resolver.go](/Users/yiliiang/Documents/lingce-worker/internal/analysis/route_resolver.go)
3. [internal/analysis/doctor.go](/Users/yiliiang/Documents/lingce-worker/internal/analysis/doctor.go)

核验重点：

1. 在 `inst_employee_roles` 单角色化后是否仍能正常判路由
2. doctor 专科字段是否仍可读取

## 8. 租户管理专项验收

本节必须逐项手工核验。

### 8.1 新租户创建

验证项：

1. `POST /api/v1/tenants` 成功
2. 默认 admin 员工成功创建
3. `inst_employee_roles` 中存在 `tenant_init/admin`
4. 默认部门创建成功
5. 默认部门角色配置成功
6. 默认 admin 能登录
7. 默认 admin 登录后能访问：
   1. 员工列表
   2. 部门列表
   3. 角色列表
   4. 租户 profile
   5. 录音列表

### 8.2 老租户 CRUD

验证项：

1. `GET /api/v1/tenants`
2. `GET /api/v1/tenants/{id}`
3. `PUT /api/v1/tenants/{id}`
4. `DELETE /api/v1/tenants/{id}`
5. `GET /api/v1/tenants/{id}/profile`
6. `PUT /api/v1/tenants/{id}/profile`
7. `GET /api/v1/tenants/{id}/statistics`
8. `GET /api/v1/tenants/{id}/medical-specialties`
9. subscription / features / overrides

### 8.3 组织代理接口

验证项：

1. `internal/organization` 下 tenant CRUD 路径全部走通
2. 返回数据中租户信息正确
3. 不因角色源切换而出现权限异常

## 9. 回归矩阵

### 9.1 员工角色

1. 创建员工
2. 修改员工角色
3. 删除员工角色
4. 员工列表按角色筛选
5. 员工详情角色展示

### 9.2 部门角色

1. 设置部门默认角色
2. `update_existing=false`
3. `update_existing=true`
4. 部门员工角色是否符合既有语义

### 9.3 权限

1. 员工拥有角色后可访问对应资源
2. 删除角色后权限消失
3. 同租户不同角色员工权限分离

### 9.4 录音与分析

1. 录音列表 `recording_scope=doctor`
2. 录音列表 `recording_scope=consultant`
3. 录音列表 `recording_scope=frontdesk`
4. 正常分析命中正确 pipeline
5. 重新分析命中正确 pipeline

### 9.5 badge / sandbox

1. badge 回调生成的 `business_scope` 正确
2. sandbox 搜索显示员工角色正确
3. sandbox 目标员工角色一致性校验正常

## 10. 断路策略

### 10.1 测试环境

在以下条件全部满足后执行断路：

1. 写路径已统一
2. 主读路径已统一
3. 数据校正已执行
4. 租户管理专项回归通过

执行语句：

```sql
ALTER TABLE institution_employee_roles RENAME TO institution_employee_roles_disabled;
```

### 10.2 断路后观察点

1. API 500 日志
2. worker 执行错误
3. 低频管理接口
4. 后台脚本
5. 报表/管理页冷门查询

### 10.3 生产环境

生产也按同样方式改名，不保留兼容视图。

原因：

1. 兼容视图会掩盖漏改
2. 这次目标就是彻底收口

## 11. 暂不处理

1. Python 体系角色源统一
2. 角色历史审计模型
3. 多角色并行的全新产品语义
4. `institution_roles` / `inst_roles` 彻底并表

## 12. 我的执行顺序

实际开发按下面顺序落代码：

1. 先做 T2 租户创建
2. 再做 T3 员工角色写路径
3. 再做 T4 部门角色应用
4. 再做 T5 员工展示
5. 再做 T6 RBAC 权限判断
6. 再做 T7 录音收口
7. 再补 T8 数据校正 SQL
8. 最后执行 T9 测试环境断路

原因：

1. 租户创建是最高风险入口
2. 写路径优先，避免继续制造新脏数据
3. 员工展示和 RBAC 改完后，租户内主功能才真正一致
