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
# 交叉编译
make build-linux

# 部署为 systemd 服务
scp bin/lingce-api server:/opt/lingce-api/
scp deploy/lingce-api.service server:/etc/systemd/system/
systemctl enable --now lingce-api
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
| [迁移历史背景](docs/MIGRATION_BACKGROUND.md) | 为什么要做这次迁移，旧系统的问题，目标 |
| [开发计划](docs/DEVELOPMENT_PLAN.md) | 分阶段实施计划、时间线、里程碑 |
| [迁移清单](docs/MIGRATION_INVENTORY.md) | 完整的 API 端点和数据表清单 |
| [架构设计](docs/ARCHITECTURE.md) | 技术选型、模块设计、约定 |
| [开发规范](docs/DEVELOPMENT_GUIDE.md) | 编码规范、目录约定、开发流程 |

## 相关服务

| 服务 | 仓库 | 说明 |
|------|------|------|
| llm-gateway | [llm-gateway](https://github.com/yiliiang/llm-gateway) | LLM 调用网关（文本推理 + 语音转写） |
| badge-middleware | [badge-middleware](https://github.com/freeasyman/badge-middleware) | 智能工牌中间件（厂商对接 + 事件分发） |
| recording-worker | [recording-worker](https://github.com/yiliiang/recording-worker) | 录音分析 Worker（转写 → 清洗 → 分析） |
| operations-api | lince-medical-ops/services/operations-api | 旧 Python 服务（本项目替代目标） |
