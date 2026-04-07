# API 文档

本目录包含灵策医疗运营系统的完整API文档。

## 文档格式

我们使用 **OpenAPI 3.0** 规范来描述API，这是业界标准的API文档格式。

## 文档文件

- `openapi_complete.yaml` - **完整的 OpenAPI 3.0 规范文档（369个端点）** ⭐️
- `openapi.yaml` - 基础模板（包含认证模块示例）
- `openapi_full.yaml` - 手动维护的完整文档（50个端点）

**推荐使用**: `openapi_complete.yaml` - 这是从代码自动提取生成的最新完整文档。

## 如何查看文档

### 方法1：使用 Swagger UI（推荐）

1. 安装 Swagger UI：
```bash
npm install -g swagger-ui-watcher
```

2. 启动文档服务器：
```bash
cd /Users/yiliiang/Documents/lingce-api
make docs
```

3. 在浏览器中打开 http://localhost:8000

### 方法2：使用在线工具

访问 [Swagger Editor](https://editor.swagger.io/)，将 `openapi_complete.yaml` 内容粘贴进去即可查看和测试。

### 方法3：使用 VS Code 插件

安装 VS Code 插件：
- [OpenAPI (Swagger) Editor](https://marketplace.visualstudio.com/items?itemName=42Crunch.vscode-openapi)
- [Swagger Viewer](https://marketplace.visualstudio.com/items?itemName=Arjun.swagger-viewer)

## 文档内容

### ✅ 已完成（369个端点）

所有端点已自动从代码中提取并生成文档：

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

### 文档特性

1. **自动生成** - 从代码中自动提取，保证与实际代码一致
2. **完整覆盖** - 包含所有已实现的端点
3. **标准格式** - 符合 OpenAPI 3.0 规范
4. **可交互** - 通过 Swagger UI 可直接测试
5. **易维护** - 代码更新后重新运行脚本即可更新文档

## 更新文档

当代码中的路由发生变化时，运行以下命令重新生成文档：

```bash
cd /Users/yiliiang/Documents/lingce-api
python3 scripts/extract_and_generate_openapi.py
```

## 快速开始

### 1. 获取访问令牌

```bash
curl -X POST http://localhost:18080/api/v1/auth/login \
  -H "Content-Type: application/json" \
  -d '{
    "username": "admin",
    "password": "password123"
  }'
```

响应：
```json
{
  "data": {
    "token": "eyJhbGciOiJIUzI1NiIsInR5cCI6IkpXVCJ9...",
    "user": {
      "id": 1,
      "username": "admin",
      "user_type": "admin"
    }
  }
}
```

### 2. 使用令牌访问API

```bash
curl -X GET http://localhost:18080/api/v1/auth/me \
  -H "Authorization: Bearer <your-token>"
```

## API 概览

### 模块列表

| 模块 | 端点数 | 说明 |
|------|--------|------|
| 认证模块 | 9 | 用户登录、令牌管理 |
| 组织架构 | 41 | 机构、部门、员工、医生、患者 |
| 录音管理 | 72 | 问诊录音、转写、分析 |
| 权限管理 | 44 | RBAC权限控制 |
| 系统配置 | 22 | 租户配置、订阅管理 |
| 智能工牌 | 46 | 设备管理、录音控制 |
| 内容管理 | 74 | 选题、内容生成、发布 |
| 客户管理 | 35 | 客户信息、标签、分组 |
| 支撑模块 | 36 | 通知、日志、LLM配置 |
| 问诊记录 | 5 | 问诊记录CRUD |
| **总计** | **384** | |

## 认证方式

所有需要认证的接口都需要在请求头中携带JWT令牌：

```
Authorization: Bearer <token>
```

### 用户类型

- `admin`: 运维管理员，可访问所有租户数据
- `employee`: 机构员工（Web端），只能访问自己租户的数据
- `mobile`: 移动端用户，与employee权限相同但会话独立

## 响应格式

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

## 常见错误码

| 错误码 | HTTP状态码 | 说明 |
|--------|-----------|------|
| UNAUTHORIZED | 401 | 未授权，令牌无效或已过期 |
| FORBIDDEN | 403 | 禁止访问，权限不足 |
| NOT_FOUND | 404 | 资源不存在 |
| BAD_REQUEST | 400 | 请求参数错误 |
| INTERNAL_ERROR | 500 | 服务器内部错误 |

## 生成客户端SDK

使用 OpenAPI Generator 可以自动生成各种语言的客户端SDK：

```bash
# 安装 OpenAPI Generator
npm install -g @openapitools/openapi-generator-cli

# 生成 TypeScript 客户端
openapi-generator-cli generate \
  -i docs/openapi.yaml \
  -g typescript-axios \
  -o clients/typescript

# 生成 Python 客户端
openapi-generator-cli generate \
  -i docs/openapi.yaml \
  -g python \
  -o clients/python

# 生成 Java 客户端
openapi-generator-cli generate \
  -i docs/openapi.yaml \
  -g java \
  -o clients/java
```

## 贡献指南

如需补充或修改API文档，请：

1. 编辑 `openapi.yaml` 文件
2. 确保符合 OpenAPI 3.0 规范
3. 使用 Swagger Editor 验证文档有效性
4. 提交 Pull Request

## 相关资源

- [OpenAPI 3.0 规范](https://swagger.io/specification/)
- [Swagger UI](https://swagger.io/tools/swagger-ui/)
- [OpenAPI Generator](https://openapi-generator.tech/)
- [项目主文档](../README.md)
