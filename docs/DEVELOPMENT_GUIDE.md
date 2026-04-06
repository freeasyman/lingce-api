# 开发规范

> 本文档定义 lingce-api 的编码规范、目录约定和开发流程。

## 一、Go 编码规范

### 1.1 基本规则

- 遵循 [Effective Go](https://go.dev/doc/effective_go) 和 [Go Code Review Comments](https://go.dev/wiki/CodeReviewComments)
- 使用 `gofmt` 格式化代码（IDE 保存时自动执行）
- 使用 `golangci-lint` 做静态检查
- 所有导出的类型和函数必须有注释

### 1.2 命名约定

| 类别 | 风格 | 示例 |
|------|------|------|
| 包名 | 小写单词 | `auth`, `sysconfig`, `httputil` |
| 文件名 | 小写 + 下划线 | `handler_topics.go`, `store.go` |
| 结构体 | PascalCase | `CreateTenantReq`, `RecordingAnalysis` |
| 接口 | PascalCase + er 后缀 | `Store`（数据层接口不强制 er） |
| 常量 | PascalCase | `MaxPageSize`, `DefaultTimeout` |
| 局部变量 | camelCase | `tenantID`, `pageSize` |
| 错误变量 | Err 前缀 | `ErrNotFound`, `ErrUnauthorized` |

### 1.3 JSON 字段命名

与旧 Python API 保持一致，使用 `snake_case`：

```go
type Employee struct {
    ID        int64  `json:"id"`
    TenantID  int64  `json:"tenant_id"`
    FullName  string `json:"full_name"`
    CreatedAt string `json:"created_at"`
}
```

### 1.4 错误处理

```go
// 好：明确处理错误
user, err := s.store.GetUser(ctx, id)
if err != nil {
    if errors.Is(err, pgx.ErrNoRows) {
        return nil, httputil.ErrNotFound("用户不存在")
    }
    return nil, fmt.Errorf("查询用户失败: %w", err)
}

// 坏：忽略错误
user, _ := s.store.GetUser(ctx, id)
```

### 1.5 Context 使用

- 所有数据库操作和 HTTP 调用必须传递 `context.Context`
- 不在 context 中存储业务数据（租户 ID 除外，通过中间件注入）
- 外部 HTTP 调用设置合理超时

---

## 二、目录约定

### 2.1 新增模块

添加新模块时，在 `internal/` 下创建目录：

```bash
mkdir internal/newmodule
touch internal/newmodule/{handler,service,store,model,dto}.go
```

每个文件的职责：

| 文件 | 职责 | 依赖 |
|------|------|------|
| handler.go | 路由注册、请求解析、响应序列化 | service, dto |
| service.go | 业务逻辑、数据组装 | store, model, 外部 client |
| store.go | SQL 查询、数据库交互 | model, pgx |
| model.go | 数据库行映射结构体 | — |
| dto.go | 请求/响应 JSON 结构体 | — |

### 2.2 文件拆分

当单个文件超过 300 行时，按子域拆分：

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

### 2.3 pkg 包规则

`pkg/` 下的包可被外部项目引用，因此：

- 不依赖 `internal/` 下的任何包
- 不依赖具体的业务逻辑
- 保持接口稳定，谨慎修改

---

## 三、API 兼容规则

### 3.1 路径兼容

所有 API 路径必须与旧 Python 服务完全一致：

```
旧：POST http://localhost:8003/api/v1/auth/login
新：POST http://localhost:18080/api/v1/auth/login
```

### 3.2 响应格式兼容

分页响应：

```json
{
  "items": [...],
  "total": 100,
  "page": 1,
  "page_size": 20
}
```

错误响应：

```json
{
  "code": "ERROR_CODE",
  "message": "人类可读的错误描述",
  "details": {}
}
```

### 3.3 兼容性验证方法

开发每个端点时，对比旧系统的实际响应：

```bash
# 1. 调用旧系统
curl -s http://localhost:8003/api/v1/xxx | jq . > old.json

# 2. 调用新系统
curl -s http://localhost:18080/api/v1/xxx | jq . > new.json

# 3. 对比
diff old.json new.json
```

---

## 四、数据库规范

### 4.1 SQL 编写

- 使用参数化查询（`$1`, `$2`），禁止字符串拼接
- SELECT 明确列出字段，不用 `SELECT *`
- 复杂查询加注释说明意图
- 大表查询确保走索引

```go
// 好
rows, err := pool.Query(ctx,
    `SELECT id, name, tenant_id FROM employees
     WHERE tenant_id = $1 AND department_id = $2
     ORDER BY created_at DESC
     LIMIT $3 OFFSET $4`,
    tenantID, deptID, limit, offset,
)

// 坏
query := fmt.Sprintf("SELECT * FROM employees WHERE name = '%s'", name)
```

### 4.2 NULL 处理

数据库中可能为 NULL 的字段，使用指针或 `pgtype`：

```go
type Doctor struct {
    ID        int64
    Name      string
    Phone     *string  // 可能为 NULL
    DeletedAt *time.Time
}
```

### 4.3 软删除

遵循现有表的 `deleted_at` 模式：

```sql
-- 查询时排除已删除
WHERE deleted_at IS NULL

-- 软删除
UPDATE employees SET deleted_at = NOW() WHERE id = $1
```

---

## 五、开发流程

### 5.1 开发一个端点的步骤

1. 查阅 MIGRATION_INVENTORY.md 确认端点规格
2. 阅读旧 Python 代码，理解业务逻辑
3. 在 model.go 中定义数据库模型
4. 在 dto.go 中定义请求/响应结构体
5. 在 store.go 中实现数据库查询
6. 在 service.go 中实现业务逻辑
7. 在 handler.go 中注册路由和处理函数
8. 调用旧系统对比响应格式
9. 编写单元测试（至少覆盖 service 层）

### 5.2 Git 工作流

```
main ← 稳定分支，可部署
  └── dev ← 开发分支
       ├── feat/auth-login
       ├── feat/org-employees
       └── fix/jwt-expiry
```

- 功能分支从 dev 创建，完成后合并回 dev
- dev 测试通过后合并到 main
- 提交信息格式：`feat(auth): implement login endpoint`

### 5.3 提交信息规范

```
<type>(<scope>): <description>

type:
  feat     新功能
  fix      修复
  refactor 重构
  docs     文档
  test     测试
  chore    构建/工具

scope:
  auth, organization, rbac, sysconfig, recording,
  badge, content, customer, support, middleware, config
```

### 5.4 构建与测试

```bash
# 构建
make build

# 运行
make run

# 测试
make test

# 代码检查
make lint

# 交叉编译（Linux amd64）
make build-linux
```

---

## 六、安全规范

### 6.1 认证

- 所有非公开端点必须验证 JWT
- JWT 密钥从环境变量读取，不硬编码
- Token 过期时间：24 小时（可配置）

### 6.2 密码

- 新密码使用 bcrypt 哈希（cost=10）
- 登录时兼容旧 SHA256 哈希：先尝试 bcrypt，失败后尝试 SHA256
- SHA256 验证通过后，自动升级为 bcrypt

### 6.3 输入验证

- 所有用户输入必须验证（长度、格式、范围）
- SQL 参数化查询，防止注入
- 文件上传验证类型和大小

### 6.4 数据隔离

- 租户数据查询必须带 `tenant_id` 条件
- 中间件层自动注入，store 层强制使用
- 运营端管理员可跨租户查询（需 RBAC 权限）

---

## 七、Makefile 参考

```makefile
.PHONY: build run test lint clean build-linux

APP_NAME := lingce-api
BUILD_DIR := bin

build:
	go build -o $(BUILD_DIR)/$(APP_NAME) ./cmd/lingce-api

run: build
	$(BUILD_DIR)/$(APP_NAME)

test:
	go test ./... -v -count=1

lint:
	golangci-lint run ./...

clean:
	rm -rf $(BUILD_DIR)

build-linux:
	GOOS=linux GOARCH=amd64 go build -o $(BUILD_DIR)/$(APP_NAME) ./cmd/lingce-api
```
