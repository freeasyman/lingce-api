# lingce-api 重构开发计划

> 状态：草案，待审阅
> 日期：2026-04-14
> 前置文档：docs/RESOURCE_DESIGN.md
> 替代文档：本文档替代原 DEVELOPMENT_PLAN.md 中的旧排期

## 一、总体策略

- 逐个资源完整走完 Phase 1 → Phase 2 → Phase 3，测试通过后再进入下一个资源。
- 在第一个资源开始之前，先完成共享基础设施（Sprint 0）。
- 三个优先级批次，确保运营端和机构管理端尽快可用。
- RBAC 权限检查用原生 SQL + Go 实现，不使用 casbin。

---

## 二、Sprint 0 — 基础设施

所有资源重构的前置依赖，必须先完成。

### S0-1. Employee Store 补全

**原因：** recording 的 Phase 1 需要调用 `employee.Store`，但当前缺少 recording 需要的查询方法。

**改动：**
- `internal/employee/store.go` — 新增方法：
  - `ListByRole(ctx, tenantIDs, role)` — 按角色过滤员工
  - `GetAbilityRanking(ctx, tenantID, opts)` — 员工能力排名（SQL 从 recording/service.go 迁移）
  - `GetByIDs(ctx, ids)` — 批量查询

**验收：** `go build` 通过，新方法有单元测试。

### S0-2. 声明式路由注册框架

**改动：**
- 新建 `internal/router/router.go`：

```go
type Route struct {
    Method           string
    Path             string
    Handler          http.HandlerFunc
    Auth             bool
    AllowedUserTypes []string   // admin, employee, mobile
    Permission       string     // "recording:write"，空字符串不检查
    TenantScoped     bool
}

func Register(mux *http.ServeMux, routes []Route, deps RouteDeps)
```

- 支持新旧路由共存：旧路由用 `Permission: ""` 跳过权限检查。

**验收：** 框架编译通过，有基础测试。

### S0-3. RBAC 权限检查（原生实现，不用 casbin）

**改动：**
- 新建 `internal/rbac/checker.go`：

```go
type PermissionChecker struct {
    store *Store
}

// admin: admins → admin_roles → roles → role_permissions → permissions（一条 JOIN）
func (c *PermissionChecker) GetAdminPermissions(ctx, adminID) ([]Permission, error)

// employee: employees → employee_roles → roles → role_permissions → permissions（一条 JOIN）
func (c *PermissionChecker) GetEmployeePermissions(ctx, employeeID) ([]Permission, error)

// 通用检查
func (c *PermissionChecker) HasPermission(ctx, userID, userType, resource, action) (bool, error)
```

- 集成到 S0-2 的路由框架中间件。

**验收：** 权限查询有单元测试，中间件能正确拦截无权限请求。

### S0-4. 租户隔离中间件标准化

**改动：**
- 将 `tenancy.ResolveScope()` 集成到路由框架，`TenantScoped: true` 的路由自动注入 scope。
- handler 通过 `tenancy.GetScope(ctx)` 获取，不再手动调用 `ResolveScope`。

**验收：** 现有租户隔离行为不变。

---

## 三、第一批 — 核心业务资源

目标：运营端和机构管理端进入可用状态。

每个资源执行流程：
1. **Phase 1** — Store 层收敛（删除跨模块重复 SQL，改为调用归属 store）
2. **Phase 2** — 新路由上线（按 RESOURCE_DESIGN.md 注册新端点，旧路由加代理和 Sunset 头）
3. **Phase 3** — 清理（删除旧路由和兼容代码）
4. **测试** — 接口测试通过，前端验证

---

### R1. auth — 认证

| 阶段 | 工作内容 |
|------|---------|
| Phase 1 | auth store 已独立，无需收敛。确认无跨模块 SQL。 |
| Phase 2 | 按 RESOURCE_DESIGN.md 4.1 注册新路由。删除 `/login/employee`（与 `/login/institution` 重复）。SMS 路径从 `/mobile/sms/*` 调整为 `/sms/*`。使用声明式框架。 |
| Phase 3 | 删除旧路由别名。 |
| 改动量 | 小 |

### R2. tenant — 租户

| 阶段 | 工作内容 |
|------|---------|
| Phase 1 | 确认 tenants 表的 SQL 归属。sysconfig 中操作 tenant_subscriptions、tenant_feature_* 等独立表的 SQL 保留在 sysconfig store（它们不是 tenants 表）。 |
| Phase 2 | 合并 organization 和 sysconfig 中的 tenant 端点到 `/api/v1/tenants`。订阅 → `/tenants/{id}/subscription/actions/*`。功能配置 → `/tenants/{id}/features`。机构档案 → `/tenants/{id}/profile`。旧路由（`/sysconfig/tenants/*`、`/config/tenants/*`、`/config/subscriptions`、`/organization/profile`、`/institutions/statistics`）全部代理。 |
| Phase 3 | 删除 sysconfig 中 tenant 端点、`/config/*` 全部别名、organization 中 profile/statistics 端点。 |
| 改动量 | 中等 |

### R3. recording — 录音

| 阶段 | 工作内容 |
|------|---------|
| Phase 1 | 删除 `recording/service.go` 中直接查 `employees` 表的代码（GetDoctorAbilityRanking、GetDoctorAbilityDetail、GetTeamTrends），改为调用 `employee.Store`。service 新增 `employeeStore` 依赖。修改 `main.go` 接线。 |
| Phase 2 | 按 RESOURCE_DESIGN.md 4.6 注册新路由。合并 `handler.go`、`handler_advanced.go`、`handler_medical.go`、`handler_compat.go` 到统一路径。`/medical-recordings/*` 合并到 `/recordings/*`。录音提示词 → `/recordings/prompts/*`。旧路由代理。 |
| Phase 3 | 删除 `handler_compat.go`、`/medical-recordings/*` 旧路由、尾部斜杠重复。 |
| 改动量 | 大（端点最多的模块，78 个） |

### R4. recording-task — 录音任务

| 阶段 | 工作内容 |
|------|---------|
| Phase 1 | 评估是否将 task 相关 SQL 从 recording/store.go 拆为独立 store 文件。 |
| Phase 2 | 按 RESOURCE_DESIGN.md 4.7 注册新路由。从 recording handler 拆出 task 端点。旧路由代理。 |
| Phase 3 | 删除 recording handler 中的 task 旧路由、尾部斜杠重复。 |
| 改动量 | 小到中等 |

### R5. badge-device — 工牌设备

| 阶段 | 工作内容 |
|------|---------|
| Phase 1 | 删除 `badge/handler.go` 中 `ListTenantEmployees`，改为调用 `employee.Store`。确认无其他跨模块 SQL。 |
| Phase 2 | 按 RESOURCE_DESIGN.md 4.8 注册新路由。统一 v1（`/badge-control/*`、`/smart-badge/*`）和 v2（`/api/v2/badges/*`）到 `/api/v1/badge-devices/*`。厂商、设备池、回调作为子资源。旧路由全部代理。 |
| Phase 3 | 合并 v2 代码到主文件，删除 `handler_v2.go`、`service_v2.go`、`store_v2.go`。删除 v1 旧路由、`/smart-badge/*` 旧路由。 |
| 改动量 | 大（v1/v2 合并） |

### R6. badge-ticket — 设备工单

| 阶段 | 工作内容 |
|------|---------|
| Phase 1 | 评估是否将 ticket SQL 从 badge/store.go 拆为独立 store。 |
| Phase 2 | 按 RESOURCE_DESIGN.md 4.9 注册新路由。从 badge handler 拆出 ticket 端点。旧路由代理。 |
| Phase 3 | 删除 badge handler 中 ticket 旧路由。 |
| 改动量 | 小 |

### R7. content-topic — 内容选题

| 阶段 | 工作内容 |
|------|---------|
| Phase 1 | 确认 content store 无跨模块 SQL。 |
| Phase 2 | 按 RESOURCE_DESIGN.md 4.10 注册新路由。topic 端点 → `/content-topics/*`。对话洞察作为子资源。旧路由代理。 |
| Phase 3 | 删除 content handler 中 topic 旧路由。 |
| 改动量 | 中等 |

### R8. content-item — 内容条目

| 阶段 | 工作内容 |
|------|---------|
| Phase 1 | 确认无跨模块 SQL。 |
| Phase 2 | 按 RESOURCE_DESIGN.md 4.11 注册新路由。内容 CRUD、发布、GEO、内容提示词 → `/content-items/*`。旧路由代理。 |
| Phase 3 | 删除旧路由。 |
| 改动量 | 中等 |

### R9. content-seed — 内容素材

| 阶段 | 工作内容 |
|------|---------|
| Phase 1 | 确认无跨模块 SQL。 |
| Phase 2 | 按 RESOURCE_DESIGN.md 4.12 注册新路由。旧路由代理。 |
| Phase 3 | 删除旧路由。 |
| 改动量 | 小 |

---

## 四、第二批 — 管理与配置资源

### R10. customer — 客户

| 阶段 | 工作内容 |
|------|---------|
| Phase 1 | 删除 `organization/store.go` 中查 `customers` 表的函数（ListPatients、GetPatientByID、CreatePatient、UpdatePatient、DeletePatient），organization service 改为调用 `customer.Store`。 |
| Phase 2 | 按 RESOURCE_DESIGN.md 4.5 注册新路由。`/patients/*` 合并到 `/customers?type=patient`。标签 → `/customers/tags/*`。分组 → `/customers/groups/*`。旧路由代理。 |
| Phase 3 | 删除 organization 中 patient 端点、customer 尾部斜杠重复。 |
| 改动量 | 中等 |

### R11. employee — 员工（完整重构）

| 阶段 | 工作内容 |
|------|---------|
| Phase 1 | 删除 `organization/store.go` 中查 `employees` 表的函数（ListDoctors、GetDoctorByID、CreateDoctor、UpdateDoctor、DeleteDoctor），organization service 改为调用 `employee.Store`。（S0-1 已补全 store 方法，此处完成 organization 侧的清理。） |
| Phase 2 | 按 RESOURCE_DESIGN.md 4.4 注册新路由。`/doctors/*` 合并到 `/employees?role=doctor`。`/organization/employees` 别名删除。管理员管理（原 rbac/admins）评估是否归入 employee 或保留在 role 下。旧路由代理。 |
| Phase 3 | 删除 organization 中 doctor 端点、employee 尾部斜杠重复、`/organization/employees` 别名。 |
| 改动量 | 中等 |

### R12. role — 角色

| 阶段 | 工作内容 |
|------|---------|
| Phase 1 | 确认 rbac store 无跨模块 SQL。 |
| Phase 2 | 按 RESOURCE_DESIGN.md 4.13 注册新路由。合并 `/rbac/roles/*` 和 `/institution/rbac/roles/*` 到 `/roles?scope=ops|institution`。管理员端点 → `/roles/admins/*`。旧路由代理。 |
| Phase 3 | 删除 `handler.go` 和 `handler_institution.go` 中的旧路由。 |
| 改动量 | 中等 |

### R13. menu — 菜单

| 阶段 | 工作内容 |
|------|---------|
| Phase 1 | 确认无跨模块 SQL。 |
| Phase 2 | 按 RESOURCE_DESIGN.md 4.14 注册新路由。合并 `/rbac/menus/*` 和 `/institution/rbac/menus/*` 到 `/menus?scope=ops|institution`。旧路由代理。 |
| Phase 3 | 删除旧路由。 |
| 改动量 | 小到中等 |

### R14. llm — LLM 管理

| 阶段 | 工作内容 |
|------|---------|
| Phase 1 | 确认 support store 中 LLM 相关 SQL 归属。评估是否将 LLM 相关 store 从 support 拆出为独立模块。 |
| Phase 2 | 按 RESOURCE_DESIGN.md 4.19 注册新路由。模型配置 → `/llm/models/*`。调用记录 → `/llm/records/*`。成本 → `/llm/costs/*`。通用提示词 → `/llm/prompts/*`。旧路由代理。 |
| Phase 3 | 删除 support handler 中 LLM 旧路由。 |
| 改动量 | 中等 |

---

## 五、第三批 — 辅助资源

### R15. subscription-plan — 订阅计划

| 阶段 | 工作内容 |
|------|---------|
| Phase 2 | 按 RESOURCE_DESIGN.md 4.17 注册新路由 `/subscription-plans/*`。旧路由代理。 |
| Phase 3 | 删除 sysconfig 中订阅计划旧路由。 |
| 改动量 | 小 |

### R16. feature-group — 功能组

| 阶段 | 工作内容 |
|------|---------|
| Phase 2 | 按 RESOURCE_DESIGN.md 4.18 注册新路由 `/feature-groups/*`。旧路由代理。 |
| Phase 3 | 删除 sysconfig 和 `/config/*` 中功能组旧路由。 |
| 改动量 | 小 |

### R17. notification — 通知

| 阶段 | 工作内容 |
|------|---------|
| Phase 2 | 按 RESOURCE_DESIGN.md 4.15 注册新路由。旧路由代理。 |
| Phase 3 | 删除 support 中通知旧路由。 |
| 改动量 | 小 |

### R18. operation-log — 操作日志

| 阶段 | 工作内容 |
|------|---------|
| Phase 2 | 按 RESOURCE_DESIGN.md 4.16 注册新路由。数据浏览器作为子资源。旧路由代理。 |
| Phase 3 | 删除 support 中日志旧路由。 |
| 改动量 | 小 |

### R19. visit — 就诊记录

| 阶段 | 工作内容 |
|------|---------|
| Phase 2 | 按 RESOURCE_DESIGN.md 4.20 注册新路由。旧路由代理。 |
| Phase 3 | 删除 support 中 visit 旧路由。 |
| 改动量 | 小 |

### R20. department — 科室

| 阶段 | 工作内容 |
|------|---------|
| Phase 1 | 确认 department store 无跨模块 SQL。 |
| Phase 2 | 按 RESOURCE_DESIGN.md 4.3 注册新路由。旧路由代理。 |
| Phase 3 | 删除旧路由。 |
| 改动量 | 小 |

---

## 六、执行顺序总览

```
Sprint 0（基础设施）
├── S0-1  Employee Store 补全
├── S0-2  声明式路由注册框架
├── S0-3  RBAC 权限检查（原生实现）
└── S0-4  租户隔离中间件标准化

第一批（核心业务）
├── R1   auth
├── R2   tenant
├── R3   recording
├── R4   recording-task
├── R5   badge-device
├── R6   badge-ticket
├── R7   content-topic
├── R8   content-item
└── R9   content-seed

第二批（管理配置）
├── R10  customer
├── R11  employee（完整）
├── R12  role
├── R13  menu
└── R14  llm

第三批（辅助）
├── R15  subscription-plan
├── R16  feature-group
├── R17  notification
├── R18  operation-log
├── R19  visit
└── R20  department
```

---

## 七、每个资源的完成标准（DoD）

1. Phase 1 完成：该资源对应的表只有一个 store 文件操作，`go build` 通过。
2. Phase 2 完成：新路由按 RESOURCE_DESIGN.md 注册，声明式鉴权生效，旧路由代理正常。
3. Phase 3 完成：旧路由和兼容代码已删除，无残留。
4. 接口测试：核心 CRUD + 关键动作有接口测试脚本。
5. 前端验证：对应前端页面功能正常。

---

## 八、风险与应对

| 风险 | 影响 | 应对 |
|------|------|------|
| recording 模块端点多（78 个），Phase 2 改动大 | 回归测试工作量大 | 分批注册：先 CRUD，再统计，再高级分析 |
| badge v1/v2 合并可能遗漏边界情况 | 设备操作异常 | 合并前梳理 v1/v2 差异清单，逐个确认 |
| 旧路由代理期间前端未及时迁移 | 代理层长期存在 | 每个资源 Phase 3 设截止日期，到期强制下线 |
| RBAC 权限数据未初始化 | 权限检查全部拒绝 | Sprint 0 包含权限种子数据脚本 |
| 外部服务依赖（llm-gateway、badge-middleware） | 集成测试受阻 | 开发环境保持外部服务可用，必要时 mock |
