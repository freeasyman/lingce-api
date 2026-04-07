# 架构设计

> 本文档定义 lingce-api 的技术选型、项目结构、模块设计和关键约定。

## 一、技术选型

### 1.1 语言与运行时

| 选项 | 选择 | 理由 |
|------|------|------|
| 语言 | Go 1.21+ | 与 llm-gateway / badge-middleware / recording-worker 保持一致 |
| 构建 | 单二进制 | 零依赖部署，systemd 管理 |
| 并发模型 | goroutine + channel | 天然适合高并发 HTTP 服务 |

### 1.2 核心依赖

| 功能 | 库 | 说明 |
|------|-----|------|
| HTTP 路由 | `net/http` + 轻量路由 | 标准库为主，可选 chi/httprouter |
| 数据库 | `jackc/pgx/v5` | PostgreSQL 原生驱动，连接池内置 |
| JWT | `golang-jwt/jwt/v5` | 令牌生成与验证 |
| 密码哈希 | `golang.org/x/crypto/bcrypt` | 新密码用 bcrypt，兼容读取旧 SHA256 |
| RBAC | `casbin/casbin/v2` | Go 原生 Casbin，比 Python 版更成熟 |
| 配置 | 环境变量 + `.env` | 与现有 Go 服务一致 |
| 日志 | `log/slog`（Go 1.21+） | 标准库结构化日志 |
| 验证码 | `mojocn/base64Captcha` | 图形验证码生成 |
| UUID | `google/uuid` | 主键生成 |

### 1.3 不引入的依赖

| 类别 | 不用 | 原因 |
|------|------|------|
| ORM | GORM / ent | 直接写 SQL，pgx 足够，避免 ORM 魔法 |
| Web 框架 | Gin / Echo / Fiber | 标准库 + 轻量路由即可，减少依赖 |
| 迁移工具 | golang-migrate | 复用现有表结构，不需要迁移 |
| Redis | go-redis | 当前单服务器，进程内缓存即可 |
| 消息队列 | — | 异步任务由 recording-worker 处理 |

---

## 二、项目结构

```
lingce-api/
├── cmd/lingce-api/
│   └── main.go              # 程序入口：配置加载 → 数据库连接 → 路由注册 → 启动服务
│
├── internal/                 # 内部包（Go 约定，不对外暴露）
│   ├── config/               # 配置结构体 + 加载逻辑
│   │   └── config.go
│   │
│   ├── middleware/            # HTTP 中间件
│   │   ├── auth.go           # JWT 认证
│   │   ├── rbac.go           # 权限检查
│   │   ├── logger.go         # 请求日志
│   │   ├── recovery.go       # panic 恢复
│   │   ├── cors.go           # 跨域
│   │   ├── requestid.go      # 请求 ID
│   │   └── tenant.go         # 租户上下文注入
│   │
│   ├── domain/               # 跨模块共享的领域类型
│   │   ├── user.go           # 用户上下文（从 JWT 解析）
│   │   └── pagination.go     # 分页参数
│   │
│   ├── store/                # 数据库连接池管理
│   │   └── postgres.go       # pgx 连接池初始化
│   │
│   ├── auth/                 # 认证模块
│   │   ├── handler.go
│   │   ├── service.go
│   │   ├── store.go
│   │   ├── model.go
│   │   └── dto.go
│   │
│   ├── organization/         # 组织架构
│   ├── rbac/                 # 权限管理
│   ├── sysconfig/            # 系统配置
│   ├── recording/            # 录音管理
│   ├── badge/                # 智能工牌
│   ├── content/              # 内容中心
│   ├── customer/             # 客户中心
│   └── support/              # 支撑模块
│
├── pkg/                      # 可复用工具包（可被外部引用）
│   ├── httputil/             # HTTP 响应、分页、错误码
│   │   ├── response.go       # 统一成功/错误响应
│   │   ├── pagination.go     # 分页解析
│   │   └── errors.go         # 错误码定义
│   │
│   ├── auth/                 # JWT 工具
│   │   └── jwt.go
│   │
│   └── sqlutil/              # SQL 构建辅助
│       └── builder.go        # 动态 WHERE / ORDER BY 构建
│
├── configs/                  # 配置文件模板
│   └── .env.example
│
├── deploy/                   # 部署文件
│   └── lingce-api.service    # systemd 服务文件
│
├── scripts/                  # 构建、部署脚本
├── docs/                     # 项目文档
├── go.mod
├── go.sum
└── Makefile
```

---

## 三、模块内部结构

每个业务模块遵循三层架构：

```
internal/{module}/
├── handler.go    # HTTP 层：路由注册、请求解析、响应序列化
├── service.go    # 业务层：业务逻辑、数据组装、外部服务调用
├── store.go      # 数据层：SQL 查询、数据库交互
├── model.go      # 数据模型：数据库行映射结构体
└── dto.go        # 传输对象：请求/响应 JSON 结构体
```

### 层间调用规则

```
handler → service → store
   ↓         ↓
  dto      model
```

- handler 只做 HTTP 协议相关的事：解析参数、调用 service、序列化响应
- service 包含业务逻辑，可调用多个 store 和外部服务
- store 只做数据库操作，不包含业务逻辑
- handler 不直接调用 store
- store 不调用 service

### 已实现模块示例

**auth 模块**（9 个端点）：
```
internal/auth/
├── handler.go          # 9 个登录/认证端点
├── service.go          # 密码验证、JWT 生成、会话管理
├── store.go            # 用户查询、会话版本更新
├── model.go            # OperationsAdmin, Employee, Tenant, SMSLoginCode
├── dto.go              # LoginRequest, LoginResponse, MeResponse
├── captcha_store.go    # 验证码存储（进程内存 + 自动清理）
└── (依赖) pkg/auth/jwt.go, pkg/captcha/captcha.go, pkg/sms/aliyun.go
```

**organization 模块**（16 个端点）：
```
internal/organization/
├── handler.go    # 租户 CRUD（5 个端点）
├── service.go    # 租户业务逻辑、分页处理
├── store.go      # 租户查询、动态条件构建
├── model.go      # Tenant
└── dto.go        # TenantListRequest, TenantResponse, CreateTenantRequest

internal/department/
├── handler.go    # 部门 CRUD（5 个端点）
├── service.go    # 部门业务逻辑
├── store.go      # 部门查询（支持层级结构）
├── model.go      # Department
└── dto.go        # DepartmentListRequest, DepartmentResponse

internal/employee/
├── handler.go    # 员工 CRUD + 密码重置（6 个端点）
├── service.go    # 员工业务逻辑、密码哈希
├── store.go      # 员工查询、会话版本管理
├── model.go      # Employee
└── dto.go        # EmployeeListRequest, EmployeeResponse
```

**recording 模块**（5 个端点，基础版）：
```
internal/recording/
├── handler.go    # 录音 CRUD（5 个端点）
├── service.go    # 录音业务逻辑
├── store.go      # 录音查询（支持多条件过滤）
├── model.go      # MedicalRecording, RecordingStatus
└── dto.go        # RecordingListRequest, RecordingResponse
```

### 文件大小约束

- 单文件不超过 500 行（硬性约束）
- 超过 300 行时考虑拆分（如 `handler_topics.go`、`handler_contents.go`）
- 复杂模块可按子域拆分文件，但保持同一 package

**拆分示例**（recording 模块完整版将采用）：
```
internal/recording/
├── handler.go              # RegisterRoutes + 公共辅助
├── handler_recordings.go   # 录音 CRUD 端点
├── handler_dashboard.go    # 看板端点
├── handler_tasks.go        # 任务端点
├── handler_prompts.go      # 提示词端点
├── service.go              # 公共 service 逻辑
├── service_analysis.go     # 分析相关逻辑
├── store.go                # 公共查询
├── store_recordings.go     # 录音表查询
├── store_tasks.go          # 任务表查询
├── model.go
└── dto.go
```

---

## 四、HTTP 层设计

### 4.1 路由注册

每个模块的 handler 提供 `RegisterRoutes` 函数：

```go
// internal/auth/handler.go
func RegisterRoutes(mux *http.ServeMux, svc *Service, mw *middleware.Chain) {
    mux.Handle("POST /api/v1/auth/login", mw.Public(h.Login))
    mux.Handle("GET /api/v1/auth/me", mw.Auth(h.Me))
}
```

main.go 中统一注册：

```go
auth.RegisterRoutes(mux, authSvc, mw)
organization.RegisterRoutes(mux, orgSvc, mw)
rbac.RegisterRoutes(mux, rbacSvc, mw)
// ...
```

### 4.2 统一响应格式

成功响应：

```json
{
  "items": [...],
  "total": 100,
  "page": 1,
  "page_size": 20
}
```

或单对象：

```json
{
  "id": 1,
  "name": "..."
}
```

错误响应：

```json
{
  "code": "INVALID_CREDENTIALS",
  "message": "用户名或密码错误",
  "details": {}
}
```

### 4.3 中间件链

```
请求 → RequestID → Logger → Recovery → CORS → [Auth] → [RBAC] → Handler
```

- Public 端点：跳过 Auth 和 RBAC
- Auth 端点：需要有效 JWT
- Admin 端点：需要 JWT + RBAC 权限检查

---

## 五、认证与授权

### 5.1 JWT 设计

```
Header: { "alg": "HS256", "typ": "JWT" }
Payload: {
  "sub": "user_id",
  "type": "admin|employee|mobile",
  "tenant_id": 1,
  "session_version": 3,
  "exp": 1234567890
}
```

三种用户类型对应三种登录端点，JWT 中通过 `type` 字段区分。

### 5.2 会话版本

每个用户维护 `session_version` 字段。修改密码或强制下线时递增版本号，旧 JWT 中的版本号不匹配则拒绝。

### 5.3 RBAC 架构

两套独立的 Casbin 体系：

```
运营端 RBAC（ops_casbin_rule）
├── 角色：ops_roles
├── 菜单：ops_menus
├── 关联：ops_role_menus, ops_admin_roles
└── 策略：ops_casbin_rule

机构端 RBAC（inst_casbin_rule）
├── 角色：inst_roles
├── 菜单：inst_menus
├── 关联：inst_role_menus, inst_employee_roles
└── 策略：inst_casbin_rule
```

Casbin 模型（RBAC with domains）：

```ini
[request_definition]
r = sub, dom, obj, act

[policy_definition]
p = sub, dom, obj, act

[role_definition]
g = _, _, _

[policy_effect]
e = some(where (p.eft == allow))

[matchers]
m = g(r.sub, p.sub, r.dom) && r.dom == p.dom && r.obj == p.obj && r.act == p.act
```

---

## 六、数据库层设计

### 6.1 连接池

```go
// pgx/v5 连接池配置
pool, err := pgxpool.New(ctx, connString)
// 默认配置：
// - MaxConns: 20
// - MinConns: 5
// - MaxConnLifetime: 30m
// - MaxConnIdleTime: 5m
```

### 6.2 SQL 风格

直接写 SQL，不用 ORM：

```go
// internal/auth/store.go
func (s *Store) GetAdminByUsername(ctx context.Context, username string) (*Admin, error) {
    row := s.pool.QueryRow(ctx,
        `SELECT id, username, password_hash, session_version
         FROM operations_admins
         WHERE username = $1 AND deleted_at IS NULL`,
        username,
    )
    var a Admin
    err := row.Scan(&a.ID, &a.Username, &a.PasswordHash, &a.SessionVersion)
    return &a, err
}
```

### 6.3 事务管理

service 层管理事务边界：

```go
func (s *Service) CreateTenant(ctx context.Context, req CreateTenantReq) error {
    tx, err := s.pool.Begin(ctx)
    if err != nil {
        return err
    }
    defer tx.Rollback(ctx)

    // 1. 创建租户
    // 2. 初始化订阅
    // 3. 创建默认部门
    // 4. 创建默认角色和菜单

    return tx.Commit(ctx)
}
```

### 6.4 多租户数据隔离

所有租户相关查询必须带 `tenant_id` 条件：

```go
// 中间件从 JWT 中提取 tenant_id 注入 context
tenantID := middleware.TenantIDFromCtx(ctx)

// store 层查询时使用
rows, err := s.pool.Query(ctx,
    `SELECT ... FROM employees WHERE tenant_id = $1 AND ...`,
    tenantID,
)
```

---

## 七、外部服务集成

### 7.1 服务拓扑

```
lingce-api (:18080)
    │
    ├──→ llm-gateway (:8080)
    │    POST /v1/chat/completions    # 文本推理
    │    POST /v1/audio/transcriptions # 语音转写
    │
    ├──→ badge-middleware (:18082)
    │    GET  /api/v1/devices          # 设备查询
    │    POST /api/v1/devices/*/start  # 启动录音
    │    POST /api/v1/devices/*/stop   # 停止录音
    │
    ├──→ recording-worker (:18090)
    │    POST /v1/jobs                 # 触发转写/分析任务
    │
    └──→ 阿里云
         DySMS API                     # 短信发送
         OSS                           # 文件存储
```

### 7.2 HTTP 客户端

每个外部服务封装为独立的 client：

```go
// internal/gateway/llm_client.go
type LLMClient struct {
    baseURL    string
    httpClient *http.Client
    apiKey     string
}
```

统一超时、重试、错误处理。

---

## 八、部署架构

### 8.1 单服务器部署

```
服务器
├── nginx (443/80)
│   ├── /api/v1/*  → lingce-api:18080
│   ├── /ops/*     → operations-web (静态文件)
│   ├── /inst/*    → institution-web (静态文件)
│   └── /app/*     → consultant-app (静态文件)
│
├── lingce-api     (:18080)  ← systemd 服务
├── llm-gateway    (:8080)   ← systemd 服务
├── badge-middleware(:18082) ← systemd 服务
├── recording-worker(:18090) ← systemd 服务
│
└── PostgreSQL     (:5432)
```

### 8.2 systemd 服务

```ini
[Unit]
Description=Lingce API Service
After=network.target postgresql.service

[Service]
Type=simple
User=lingce
ExecStart=/opt/lingce-api/lingce-api
WorkingDirectory=/opt/lingce-api
EnvironmentFile=/opt/lingce-api/.env
Restart=always
RestartSec=5

[Install]
WantedBy=multi-user.target
```

### 8.3 健康检查

```
GET /healthz → 200 {"status": "ok", "version": "1.0.0"}
```

检查项：数据库连接可用。

