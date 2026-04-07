# API 文档

本目录包含灵策医疗运营系统的完整API文档。

## 文档格式

我们使用 **OpenAPI 3.0** 规范来描述API，这是业界标准的API文档格式。

## 文档文件

- `openapi.yaml` - OpenAPI 3.0 规范文档（包含认证模块完整文档）
- 其他模块文档正在补充中...

## 如何查看文档

### 方法1：使用 Swagger UI（推荐）

1. 安装 Swagger UI：
```bash
npm install -g swagger-ui-watcher
```

2. 启动文档服务器：
```bash
swagger-ui-watcher docs/openapi.yaml
```

3. 在浏览器中打开 http://localhost:8000

### 方法2：使用在线工具

访问 [Swagger Editor](https://editor.swagger.io/)，将 `openapi.yaml` 内容粘贴进去即可查看和测试。

### 方法3：使用 VS Code 插件

安装 VS Code 插件：
- [OpenAPI (Swagger) Editor](https://marketplace.visualstudio.com/items?itemName=42Crunch.vscode-openapi)
- [Swagger Viewer](https://marketplace.visualstudio.com/items?itemName=Arjun.swagger-viewer)

## 文档内容

### 已完成模块

- ✅ **认证模块** (9个端点)
  - 运维管理员登录
  - 机构员工登录
  - 移动端登录
  - 获取用户信息
  - 修改密码
  - 图形验证码
  - 短信验证码登录

### 待补充模块

由于API端点数量较多（384个），完整文档正在逐步补充中。当前已提供：

1. **完整的基础架构**
   - 认证方式说明
   - 通用响应格式
   - 错误码定义
   - 数据模型定义

2. **认证模块完整文档**
   - 所有9个认证端点
   - 详细的请求/响应示例
   - 错误处理说明

3. **核心数据模型**
   - 用户信息
   - 机构信息
   - 部门信息
   - 员工信息
   - 录音信息

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
