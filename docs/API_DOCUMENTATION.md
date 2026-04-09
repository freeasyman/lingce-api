# API 文档

本目录包含 lingce-api 的 API 规范与端点清单。

## 文档格式

项目使用 **OpenAPI 3.0** 规范。

## 文档文件

- `openapi_complete.yaml` - 从代码自动提取生成的完整文档（**376 个 API 端点**）⭐
- `API_ENDPOINTS.md` - 从代码自动提取生成的 Markdown 端点清单（**376 个 API 端点**）
- `openapi.yaml` - 基础模板（示例/组件定义）
- `openapi_full.yaml` - 历史手动维护版本（非当前事实源）

推荐使用 `openapi_complete.yaml`。

## 如何查看文档

### 方法1：Swagger UI（推荐）

```bash
cd /Users/yiliiang/Documents/lingce-api
make docs
```

浏览器访问 http://localhost:8000

### 方法2：在线工具

打开 https://editor.swagger.io/ 并粘贴 `openapi_complete.yaml`。

## 当前实现统计（基于代码扫描）

统计时间：2026-04-09

| 模块 | 端点数 |
|------|--------|
| auth | 9 |
| organization | 30 |
| department | 5 |
| employee | 6 |
| recording | 78 |
| rbac | 39 |
| sysconfig | 19 |
| badge | 46 |
| content | 74 |
| customer | 34 |
| support | 36 |
| **合计（/api/v1/*）** | **376** |

补充：服务还提供 `GET /healthz` 健康检查端点（不计入 `/api/v1/*`）。

## 更新文档

当路由发生变化时，重新生成：

```bash
cd /Users/yiliiang/Documents/lingce-api
python3 scripts/extract_and_generate_openapi.py
python3 scripts/generate_markdown_docs.py
```

## 认证方式

所有受保护接口使用 JWT：

```http
Authorization: Bearer <token>
```

用户类型：
- `admin`
- `employee`
- `mobile`

## 响应格式

成功响应：

```json
{
  "data": {}
}
```

分页响应：

```json
{
  "items": [],
  "total": 0,
  "page": 1,
  "page_size": 20
}
```

错误响应：

```json
{
  "code": "ERROR_CODE",
  "message": "错误描述",
  "details": null
}
```

## 相关资源

- [API 端点列表](API_ENDPOINTS.md)
- [OpenAPI 规范](openapi_complete.yaml)
- [项目主文档](../README.md)
