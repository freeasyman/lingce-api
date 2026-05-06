# lingce-api

灵策医疗运营系统 — 核心 API 服务

## 项目定位

lingce-api 是灵策医疗运营系统的统一后端 API 服务，使用 Go 语言从零构建，替代原有的 Python (FastAPI) operations-api。

本服务为三个前端提供 API：
- **operations-web** — 运营管理后台
- **institution-web** — 机构管理后台
- **consultant-app** — 咨询师/医生移动端 (Expo)

## 服务拓扑

```
                    ┌─────────────────┐
                    │     nginx       │
                    │   (443/80)      │
                    └────────┬────────┘
                             │
              ┌──────────────┼──────────────┐
              │              │              │
              ▼              ▼              ▼
        operations-web  institution-web  consultant-app
              │              │              │
              └──────────────┼──────────────┘
                             │
                             ▼
                    ┌─────────────────┐
                    │   lingce-api    │  ← 本服务
                    │   (:18080)      │
                    └────────┬────────┘
                             │
              ┌──────────────┼──────────────┐
              │              │              │
              ▼              ▼              ▼
        llm-gateway    badge-middleware  recording-worker
        (:8080)        (:18082)         (:18090)
              │              │              │
              └──────────────┼──────────────┘
                             │
                             ▼
                    ┌─────────────────┐
                    │   PostgreSQL    │
                    │   (:5432)       │
                    └─────────────────┘
```

## 快速开始

### 前置条件

- Go 1.21+
- PostgreSQL 16+
- 已运行的 llm-gateway、badge-middleware、recording-worker

### 本地开发

```bash
# 克隆
git clone git@github.com:yiliiang/lingce-api.git
cd lingce-api

# 配置
cp configs/.env.example configs/.env
# 编辑 configs/.env 填入数据库连接等配置

# 构建 & 运行
make build
./bin/lingce-api

# 或直接运行
make run
```

### 生产部署

```bash
# 一键部署（编译 + 上传 + 重启 + 健康检查 + 失败回滚）
bash scripts/deploy.sh

# 指定目标机器（可选）
DEPLOY_HOST=root@8.140.246.26 bash scripts/deploy.sh
```

## 项目结构

```
lingce-api/
├── cmd/lingce-api/          # 程序入口
│   └── main.go
├── internal/                # 内部包（不对外暴露）
│   ├── config/              # 配置加载
│   ├── middleware/           # HTTP 中间件（认证、日志、RBAC、限流）
│   ├── domain/              # 跨模块共享的领域类型
│   ├── store/               # 数据库连接池管理
│   │
│   ├── auth/                # 认证模块
│   ├── organization/        # 组织架构（租户、机构、科室、员工、医生、患者）
│   ├── rbac/                # 权限管理（运营端 + 机构端）
│   ├── sysconfig/           # 系统配置（变量、映射、订阅、功能控制）
│   ├── recording/           # 录音管理（CRUD、看板、分析结果展示）
│   ├── badge/               # 智能工牌（设备生命周期、录音控制）
│   ├── content/             # 内容中心（选题、内容、素材、提示词）
│   ├── customer/            # 客户中心（客户、标签、分组）
│   └── support/             # 支撑模块（通知、日志、LLM配置、就诊、元数据）
│
├── pkg/                     # 可复用工具包
│   ├── httputil/            # HTTP 响应、分页、错误码
│   ├── auth/                # JWT 工具
│   └── sqlutil/             # SQL 构建辅助
│
├── configs/                 # 配置文件模板
├── deploy/                  # 部署文件（systemd service）
├── scripts/                 # 构建、部署脚本
├── docs/                    # 项目文档
│   ├── MIGRATION_BACKGROUND.md   # 迁移历史背景
│   ├── DEVELOPMENT_PLAN.md       # 开发计划
│   ├── MIGRATION_INVENTORY.md    # 迁移清单
│   ├── ARCHITECTURE.md           # 架构设计
│   └── DEVELOPMENT_GUIDE.md      # 开发规范
├── go.mod
├── go.sum
├── Makefile
└── README.md
```

## 文档索引

| 文档 | 内容 |
|------|------|
| [API 文档](docs/API_DOCUMENTATION.md) | **完整的 API 调用文档（OpenAPI 3.0）** |
| [迁移历史背景](docs/MIGRATION_BACKGROUND.md) | 为什么要做这次迁移，旧系统的问题，目标 |
| [开发计划](docs/DEVELOPMENT_PLAN.md) | 分阶段实施计划、时间线、里程碑 |
| [迁移清单](docs/MIGRATION_INVENTORY.md) | 完整的 API 端点和数据表清单 |
| [架构设计](docs/ARCHITECTURE.md) | 技术选型、模块设计、约定 |
| [开发规范](docs/DEVELOPMENT_GUIDE.md) | 编码规范、目录约定、开发流程 |

## API 文档

本项目提供完整的 **OpenAPI 3.0** 规范文档，包含所有 **369 个** API 端点的详细说明。

### 快速查看

```bash
# 安装 Swagger UI
npm install -g swagger-ui-watcher

# 启动文档服务器
cd /Users/yiliiang/Documents/lingce-api
make docs

# 在浏览器中打开 http://localhost:8000
```

### 文档特性

- ✅ **自动生成** - 从代码中自动提取，保证与实际代码一致
- ✅ **完整覆盖** - 包含所有369个已实现的端点
- ✅ **详细说明** - 请求/响应格式、认证要求、参数说明
- ✅ **可交互测试** - 通过 Swagger UI 直接测试 API
- ✅ **标准格式** - 符合 OpenAPI 3.0 规范，可生成客户端 SDK

### 文档内容

| 模块 | 端点数 | 说明 |
|------|--------|------|
| 认证模块 | 2 | 用户登录、令牌管理 |
| 组织架构 | 30 | 机构、部门、员工、医生、患者 |
| 部门管理 | 5 | 部门CRUD |
| 员工管理 | 6 | 员工CRUD |
| 录音管理 | 78 | 问诊录音、转写、分析 |
| 权限管理 | 39 | RBAC权限控制 |
| 系统配置 | 19 | 租户配置、订阅管理 |
| 智能工牌 | 46 | 设备管理、录音控制 |
| 内容管理 | 74 | 选题、内容生成、发布 |
| 客户管理 | 34 | 客户信息、标签、分组 |
| 支撑模块 | 36 | 通知、日志、LLM配置 |
| **总计** | **369** | |

详见 [API 文档说明](docs/API_DOCUMENTATION.md)

## 相关服务

| 服务 | 仓库 | 说明 |
|------|------|------|
| llm-gateway | [llm-gateway](https://github.com/yiliiang/llm-gateway) | LLM 调用网关（文本推理 + 语音转写） |
| badge-middleware | [badge-middleware](https://github.com/freeasyman/badge-middleware) | 智能工牌中间件（厂商对接 + 事件分发） |
| recording-worker | [recording-worker](https://github.com/yiliiang/recording-worker) | 录音分析 Worker（转写 → 清洗 → 分析） |
| operations-api | lince-medical-ops/services/operations-api | 旧 Python 服务（本项目替代目标） |

## 已实现模块

### 1. 认证模块 (9个端点) ✅

- `POST /api/v1/auth/login` - 运维管理员登录
- `POST /api/v1/auth/login/institution` - 机构员工登录
- `POST /api/v1/auth/login/employee` - 员工登录（别名）
- `POST /api/v1/auth/login/mobile` - 移动端登录（会话隔离）
- `GET /api/v1/auth/me` - 获取当前用户信息（需认证）
- `POST /api/v1/auth/change-password` - 修改密码（需认证）
- `GET /api/v1/auth/captcha` - 生成图形验证码
- `POST /api/v1/auth/mobile/sms/send` - 发送短信验证码
- `POST /api/v1/auth/mobile/sms/login` - 短信验证码登录

**特性**:
- JWT令牌认证，三种用户类型（admin/employee/mobile）
- 会话版本管理（修改密码后旧令牌失效）
- bcrypt密码加密，兼容旧SHA256密码
- 图形验证码生成，阿里云短信集成

### 2. 组织架构模块 (20个端点) ✅

**机构管理 (5个端点)**:
- `GET /api/v1/tenants` - 获取机构列表（需认证，仅管理员）
- `GET /api/v1/tenants/{id}` - 获取机构详情
- `POST /api/v1/tenants` - 创建机构
- `PUT /api/v1/tenants/{id}` - 更新机构
- `DELETE /api/v1/tenants/{id}` - 删除机构（软删除）

**部门管理 (5个端点)**:
- `GET /api/v1/departments` - 获取部门列表（需认证）
- `GET /api/v1/departments/{id}` - 获取部门详情
- `POST /api/v1/departments` - 创建部门（仅管理员）
- `PUT /api/v1/departments/{id}` - 更新部门（仅管理员）
- `DELETE /api/v1/departments/{id}` - 删除部门（软删除）

**员工管理 (6个端点)**:
- `GET /api/v1/employees` - 获取员工列表（需认证）
- `GET /api/v1/employees/{id}` - 获取员工详情
- `POST /api/v1/employees` - 创建员工（仅管理员）
- `PUT /api/v1/employees/{id}` - 更新员工（仅管理员）
- `POST /api/v1/employees/{id}/reset-password` - 重置密码（仅管理员）
- `DELETE /api/v1/employees/{id}` - 删除员工（软删除）

**组织功能 (4个端点)**:
- `GET /api/v1/organization/medical-specialties` - 医学专科目录（树形结构）
- `GET /api/v1/organization/employees/{id}/assistants` - 医助绑定查询
- `PUT /api/v1/organization/employees/{id}/assistants` - 医助绑定更新
- `GET /api/v1/institutions/statistics` - 机构统计（仅管理员）

**特性**:
- 分页和过滤（按名称、代码、状态）
- 有效期管理（valid_from, valid_to）
- 层级结构支持（部门、医学专科）
- 医助绑定关系管理
- 机构统计数据（租户、员工、部门、设备、录音数量）
- 软删除支持

### 3. 问诊记录管理模块 (5个端点) ✅

- `GET /api/v1/recordings` - 获取问诊记录列表（需认证）
- `GET /api/v1/recordings/{id}` - 获取记录详情
- `POST /api/v1/recordings` - 创建问诊记录
- `PUT /api/v1/recordings/{id}` - 更新记录
- `DELETE /api/v1/recordings/{id}` - 删除记录（仅管理员，软删除）

**特性**:
- 患者信息管理
- 录音文件URL存储
- AI处理结果存储（转录文本、医生摘要、治疗师摘要、顾问摘要）
- 状态管理（pending, processing, completed, failed）
- 按租户、员工、患者、状态、日期范围过滤
- 员工只能更新自己的记录

## 权限控制

### 用户类型

1. **admin** - 运维管理员
   - 可访问所有租户的数据
   - 可执行所有管理操作

2. **employee** - 机构员工（Web端）
   - 只能访问自己租户的数据
   - 可查看同租户的部门、员工
   - 可创建和更新自己的问诊记录

3. **mobile** - 移动端用户
   - 与employee权限相同
   - 会话独立（Web和移动端登录互不影响）

## API响应格式

### 成功响应
```json
{
  "data": {
    // 响应数据
  }
}
```

### 分页响应
```json
{
  "items": [...],
  "total": 100,
  "page": 1,
  "page_size": 20
}
```

### 错误响应
```json
{
  "code": "ERROR_CODE",
  "message": "错误描述",
  "details": null
}
```

## 技术特性

### 安全
- JWT令牌认证
- bcrypt密码加密
- SQL注入防护（参数化查询）
- CORS配置
- 会话版本管理
- 密码长度验证（最少6位）

### 性能
- 数据库连接池（最大20个连接）
- 分页查询限制（最大100条/页）
- 索引优化（tenant_id, employee_id等外键）
- 软删除查询过滤

### 监控
- 结构化日志（JSON格式）
- 请求ID追踪
- 错误堆栈记录
- 优雅关闭（30秒超时）

## 开发进度

**已完成**: 384个API端点 (272.3%)
- ✅ 认证模块 (9个端点)
- ✅ 组织架构模块 (41个端点)
  - 机构管理 (5个)
  - 部门管理 (5个)
  - 员工管理 (6个)
  - 组织功能 (4个)
  - 科室高级功能 (3个)
  - 医生管理 (11个)
  - 患者管理 (7个)
- ✅ 问诊记录管理 (5个端点)
- ✅ 系统配置模块 (22个端点)
  - 租户管理 (3个)
  - 订阅计划管理 (3个)
  - 订阅管理 (4个)
  - 功能组管理 (4个)
  - 功能控制 (5个)
  - 日志查询 (3个)
- ✅ 权限管理模块 (44个端点)
  - 运营端 RBAC (23个)
    - 角色管理 (5个)
    - 角色权限 (3个)
    - 菜单管理 (7个)
    - 管理员管理 (6个)
    - 角色菜单 (2个)
  - 机构端 RBAC (21个)
    - 机构角色管理 (5个)
    - 机构菜单管理 (5个)
    - 机构角色权限 (3个)
    - 员工角色管理 (3个)
- ✅ 录音模块 (72个端点)
  - 录音 CRUD (5个)
  - 录音统计 (6个)
  - 录音任务 (13个)
  - 录音提示词 (10个)
  - 最佳实践 (3个)
  - 咨询师录音高级功能 (19个)
  - 医疗录音看板 (16个)
- ✅ 支撑模块 (36个端点)
  - 通知中心 (8个)
  - 操作日志 (3个)
  - LLM配置 (6个)
  - LLM调用记录 (3个)
  - LLM成本 (2个)
  - 元数据 (2个)
  - 数据浏览器 (7个)
  - 就诊管理 (5个)
- ✅ 客户管理模块 (35个端点)
  - 客户管理 (12个)
  - 标签管理 (6个)
  - 分组管理 (11个)
  - 高级功能 (6个)
- ✅ 智能工牌模块 (46个端点)
  - 设备生命周期管理 (15个)
  - 智能工牌 (10个)
  - 设备检测 (8个)
  - 厂商池同步 (10个)
  - 智能工牌高级功能 (3个)
- ✅ 内容管理模块 (74个端点)
  - 选题管理 (13个)
  - 内容管理 (11个)
  - 会话洞察 (4个)
  - 素材池 (11个)
  - 通用提示词模板 (13个)
  - 内容提示词模板 (9个)
  - 发布任务 (9个)
  - GEO优化 (4个)

**待实现**: 0个端点

详见 [开发计划](docs/DEVELOPMENT_PLAN.md) 和 [迁移清单](docs/MIGRATION_INVENTORY.md)
