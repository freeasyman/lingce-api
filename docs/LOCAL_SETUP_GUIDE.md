# 本地运行和测试指南

本指南将帮助你在本地运行 lingce-api 服务并进行测试。

## 📋 前置条件

### 必需
- **Go 1.21+** - [安装指南](https://golang.org/doc/install)
- **PostgreSQL 16+** - [安装指南](https://www.postgresql.org/download/)

### 可选（用于完整功能）
- llm-gateway（LLM 调用网关）
- badge-middleware（智能工牌中间件）
- recording-worker（录音分析 Worker）

## 🚀 快速开始（最小化配置）

### 步骤 1：检查 Go 环境

```bash
go version
# 应该显示 go1.21 或更高版本
```

### 步骤 2：准备数据库

#### 方式 A：使用现有数据库（推荐）

如果你已经有运行中的 PostgreSQL 数据库：

```bash
# 测试数据库连接
psql -h localhost -U lince -d lince_medical -c "SELECT version();"
```

#### 方式 B：创建新数据库

```bash
# 创建数据库
createdb lingce_medical

# 或使用 psql
psql -U postgres
CREATE DATABASE lingce_medical;
\q
```

### 步骤 3：配置环境变量

```bash
cd /Users/yiliiang/Documents/lingce-api

# 复制配置文件
cp configs/.env.example configs/.env

# 编辑配置文件
nano configs/.env  # 或使用你喜欢的编辑器
```

**最小化配置**（只需修改数据库连接）：

```bash
# configs/.env
SERVER_PORT=18080
SERVER_HOST=0.0.0.0

# 修改为你的数据库连接信息
DATABASE_URL=postgresql://lince:password@localhost:5432/lince_medical?sslmode=disable

# JWT 密钥（开发环境可以保持默认）
JWT_SECRET=dev-secret-key-for-testing
JWT_EXPIRY_HOURS=24

# 外部服务（暂时留空，不影响核心功能）
LLM_GATEWAY_URL=http://localhost:8080
LLM_GATEWAY_API_KEY=

# 日志级别
LOG_LEVEL=debug
```

### 步骤 4：构建并运行

```bash
# 方式 1：使用 Makefile（推荐）
make build
./bin/lingce-api

# 方式 2：直接运行
make run

# 方式 3：使用 go run
go run cmd/lingce-api/main.go
```

**成功启动的标志**：

```
2024/04/08 10:00:00 INFO Server starting on :18080
2024/04/08 10:00:00 INFO Database connected successfully
2024/04/08 10:00:00 INFO All routes registered
```

### 步骤 5：验证服务运行

打开新终端，测试健康检查：

```bash
curl http://localhost:18080/healthz
```

应该返回：
```json
{"status":"ok"}
```

## 🧪 API 测试

### 1. 测试登录（管理员）

```bash
# 登录请求
curl -X POST http://localhost:18080/api/v1/auth/login \
  -H "Content-Type: application/json" \
  -d '{
    "username": "admin",
    "password": "your_password"
  }'
```

**成功响应**：
```json
{
  "data": {
    "token": "eyJhbGciOiJIUzI1NiIsInR5cCI6IkpXVCJ9...",
    "user_type": "admin",
    "user_id": 1,
    "username": "admin",
    "expires_at": "2024-04-09T10:00:00Z",
    "user_info": {
      "real_name": "管理员",
      "email": "admin@example.com"
    }
  }
}
```

**保存 token**：
```bash
# 将返回的 token 保存到变量
export TOKEN="eyJhbGciOiJIUzI1NiIsInR5cCI6IkpXVCJ9..."
```

### 2. 测试获取当前用户信息

```bash
curl -X GET http://localhost:18080/api/v1/auth/me \
  -H "Authorization: Bearer $TOKEN"
```

### 3. 测试机构列表（需要管理员权限）

```bash
curl -X GET "http://localhost:18080/api/v1/tenants?page=1&page_size=10" \
  -H "Authorization: Bearer $TOKEN"
```

### 4. 测试部门列表

```bash
curl -X GET "http://localhost:18080/api/v1/departments?page=1&page_size=10" \
  -H "Authorization: Bearer $TOKEN"
```

### 5. 测试员工列表

```bash
curl -X GET "http://localhost:18080/api/v1/employees?page=1&page_size=10" \
  -H "Authorization: Bearer $TOKEN"
```

## 📊 使用 Swagger UI 测试

### 启动 API 文档服务

```bash
# 在新终端中运行
cd /Users/yiliiang/Documents/lingce-api
make docs
```

访问 http://localhost:8000 查看交互式 API 文档。

### 在 Swagger UI 中测试

1. 点击右上角的 **Authorize** 按钮
2. 输入格式：`Bearer YOUR_TOKEN`
3. 点击 **Authorize**
4. 现在可以直接在 UI 中测试所有端点

## 🔍 常见问题排查

### 问题 1：数据库连接失败

**错误信息**：
```
failed to connect to database: connection refused
```

**解决方案**：
```bash
# 检查 PostgreSQL 是否运行
pg_isready

# 如果未运行，启动 PostgreSQL
# macOS (Homebrew)
brew services start postgresql@16

# Linux (systemd)
sudo systemctl start postgresql

# 测试连接
psql -h localhost -U lince -d lince_medical
```

### 问题 2：端口已被占用

**错误信息**：
```
bind: address already in use
```

**解决方案**：
```bash
# 查找占用端口的进程
lsof -i :18080

# 杀死进程
kill -9 <PID>

# 或修改配置文件中的端口
# configs/.env
SERVER_PORT=18081
```

### 问题 3：JWT token 无效

**错误信息**：
```json
{"code":"UNAUTHORIZED","message":"Invalid token"}
```

**解决方案**：
- 检查 token 是否过期（默认24小时）
- 重新登录获取新 token
- 确保 `JWT_SECRET` 配置正确

### 问题 4：找不到数据

**错误信息**：
```json
{"code":"NOT_FOUND","message":"tenant not found"}
```

**解决方案**：
- 确认数据库中有数据
- 检查是否使用了正确的租户 ID
- 查看数据库日志

## 📝 测试脚本

创建一个测试脚本方便快速测试：

```bash
# 创建测试脚本
cat > test-api.sh << 'EOF'
#!/bin/bash

API_URL="http://localhost:18080"

echo "=== 测试 lingce-api ==="
echo ""

# 1. 健康检查
echo "1. 健康检查..."
curl -s $API_URL/healthz | jq .
echo ""

# 2. 登录
echo "2. 管理员登录..."
LOGIN_RESPONSE=$(curl -s -X POST $API_URL/api/v1/auth/login \
  -H "Content-Type: application/json" \
  -d '{"username":"admin","password":"your_password"}')

TOKEN=$(echo $LOGIN_RESPONSE | jq -r '.data.token')

if [ "$TOKEN" != "null" ] && [ -n "$TOKEN" ]; then
  echo "✅ 登录成功"
  echo "Token: ${TOKEN:0:50}..."
else
  echo "❌ 登录失败"
  echo $LOGIN_RESPONSE | jq .
  exit 1
fi
echo ""

# 3. 获取当前用户
echo "3. 获取当前用户信息..."
curl -s -X GET $API_URL/api/v1/auth/me \
  -H "Authorization: Bearer $TOKEN" | jq .
echo ""

# 4. 获取机构列表
echo "4. 获取机构列表..."
curl -s -X GET "$API_URL/api/v1/tenants?page=1&page_size=5" \
  -H "Authorization: Bearer $TOKEN" | jq .
echo ""

# 5. 获取部门列表
echo "5. 获取部门列表..."
curl -s -X GET "$API_URL/api/v1/departments?page=1&page_size=5" \
  -H "Authorization: Bearer $TOKEN" | jq .
echo ""

echo "=== 测试完成 ==="
EOF

chmod +x test-api.sh
```

运行测试：
```bash
./test-api.sh
```

## 🎯 已实现功能测试清单

### ✅ 完全可用的功能

- [x] 用户认证
  - 管理员登录
  - 机构员工登录
  - 移动端登录
  - 获取当前用户信息
  - 修改密码

- [x] 组织架构
  - 机构 CRUD
  - 部门 CRUD
  - 员工 CRUD
  - 医学专科目录
  - 医助绑定
  - 机构统计

- [x] 权限管理
  - 运营端 RBAC（角色、权限、菜单）
  - 机构端 RBAC

- [x] 系统配置
  - 租户配置
  - 订阅管理
  - 功能控制

- [x] 客户管理（基本功能）
- [x] 支撑模块（通知、日志、LLM配置）

### ⚠️ 部分可用的功能

- [ ] 录音管理（基础 CRUD 可用，高级功能待实现）
- [ ] 智能工牌（部分功能可用）
- [ ] 内容管理（框架可用，业务逻辑待实现）

## 📚 相关文档

- [API 完整文档](API_DOCUMENTATION.md)
- [API 端点列表](API_ENDPOINTS.md)
- [架构设计](ARCHITECTURE.md)
- [开发规范](DEVELOPMENT_GUIDE.md)

## 💡 提示

1. **开发模式**：使用 `LOG_LEVEL=debug` 可以看到详细的 SQL 查询日志
2. **热重载**：推荐使用 [air](https://github.com/cosmtrek/air) 实现代码热重载
3. **数据库工具**：推荐使用 [pgAdmin](https://www.pgadmin.org/) 或 [DBeaver](https://dbeaver.io/) 查看数据库
4. **API 测试工具**：推荐使用 [Postman](https://www.postman.com/) 或 [Insomnia](https://insomnia.rest/)

## 🆘 获取帮助

如果遇到问题：
1. 查看服务日志输出
2. 检查数据库连接
3. 确认配置文件正确
4. 查看 [GitHub Issues](https://github.com/freeasyman/lingce-api/issues)
