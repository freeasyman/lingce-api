# R2 (tenant) 前端迁移说明

## 概述

本文档说明如何将前端代码从旧的 tenant API 迁移到新的统一 API。

**迁移原因：**
- 统一 tenant 资源的 API 路径
- 遵循 RESTful 资源导向设计
- 简化 API 结构

**迁移时间：** R2 Phase 3 开始前

---

## API 路径变更对照表

### 1. Tenant CRUD 操作

| 旧 API | 新 API | 方法 | 说明 |
|--------|--------|------|------|
| `GET /api/v1/config/tenants` | `GET /api/v1/tenants` | GET | 获取租户列表 |
| `GET /api/v1/config/tenants/{id}` | `GET /api/v1/tenants/{id}` | GET | 获取单个租户 |
| `PUT /api/v1/config/tenants/{id}` | `PUT /api/v1/tenants/{id}` | PUT | 更新租户 |
| `GET /api/v1/config/subscriptions` | `GET /api/v1/tenants` | GET | 获取租户列表（别名） |

**注意：** 新 API 还支持 `POST /api/v1/tenants` (创建) 和 `DELETE /api/v1/tenants/{id}` (删除)

### 2. 订阅计划管理

| 旧 API | 新 API | 方法 | 说明 |
|--------|--------|------|------|
| `GET /api/v1/config/subscription/plans` | `GET /api/v1/sysconfig/subscription-plans` | GET | 获取订阅计划列表 |

**注意：** 订阅计划管理保持在 sysconfig 模块，路径略有调整

### 3. 租户订阅管理

| 旧 API | 新 API | 方法 | 说明 |
|--------|--------|------|------|
| `GET /api/v1/config/tenants/{id}/subscription` | `GET /api/v1/sysconfig/tenants/{id}/subscription` | GET | 获取租户订阅信息 |
| `POST /api/v1/config/tenants/{id}/subscription/actions` | `POST /api/v1/sysconfig/tenants/{id}/subscription/action` | POST | 执行订阅操作 |
| `GET /api/v1/config/tenants/{id}/subscription/logs` | `GET /api/v1/sysconfig/tenants/{id}/subscription/events` | GET | 获取订阅事件日志 |
| `GET /api/v1/subscriptions/{id}/logs` | `GET /api/v1/sysconfig/tenants/{id}/subscription/events` | GET | 获取订阅事件日志（别名） |

### 4. 租户有效期日志

| 旧 API | 新 API | 方法 | 说明 |
|--------|--------|------|------|
| `GET /api/v1/config/tenants/{id}/validity-logs` | `GET /api/v1/sysconfig/tenants/{id}/validity-logs` | GET | 获取有效期变更日志 |

### 5. 功能组管理

| 旧 API | 新 API | 方法 | 说明 |
|--------|--------|------|------|
| `GET /api/v1/config/tenant-feature-groups` | `GET /api/v1/sysconfig/feature-groups` | GET | 获取功能组列表 |
| `GET /api/v1/config/feature-groups` | `GET /api/v1/sysconfig/feature-groups` | GET | 获取功能组列表（别名） |
| `POST /api/v1/config/tenant-feature-groups` | `POST /api/v1/sysconfig/feature-groups` | POST | 创建功能组 |
| `PUT /api/v1/config/tenant-feature-groups/{id}` | `PUT /api/v1/sysconfig/feature-groups/{id}` | PUT | 更新功能组 |
| `DELETE /api/v1/config/tenant-feature-groups/{id}` | `DELETE /api/v1/sysconfig/feature-groups/{id}` | DELETE | 删除功能组 |

### 6. 租户功能控制

| 旧 API | 新 API | 方法 | 说明 |
|--------|--------|------|------|
| `PUT /api/v1/config/tenants/{id}/feature-group` | `POST /api/v1/sysconfig/tenants/{id}/feature-group` | POST | 分配功能组 |
| `GET /api/v1/config/tenants/{id}/feature-overrides` | `GET /api/v1/sysconfig/tenants/{id}/feature-overrides` | GET | 获取功能覆盖 |
| `PUT /api/v1/config/tenants/{id}/feature-overrides` | `PUT /api/v1/sysconfig/tenants/{id}/feature-overrides` | PUT | 设置功能覆盖 |
| `GET /api/v1/config/tenants/{id}/effective-feature-policy` | `GET /api/v1/sysconfig/tenants/{id}/effective-features` | GET | 获取有效功能策略 |

### 7. 功能选项

| 旧 API | 新 API | 方法 | 说明 |
|--------|--------|------|------|
| `GET /api/v1/config/tenant-feature-options` | `GET /api/v1/sysconfig/feature-options` | GET | 获取功能选项 |

---

## 参数和响应格式变化

### ✅ 无变化

所有 API 的请求参数和响应格式**保持不变**，只是路径发生了变化。

**示例：**

```javascript
// 旧代码
const response = await fetch('/api/v1/config/tenants?limit=100');

// 新代码
const response = await fetch('/api/v1/tenants?page=1&page_size=100');
```

**注意：** 查询参数从 `limit` 改为标准的分页参数 `page` 和 `page_size`。

---

## 迁移步骤

### 1. 全局搜索替换

在前端代码中搜索以下字符串并替换：

```bash
# Tenant CRUD
/api/v1/config/tenants → /api/v1/tenants
/api/v1/config/subscriptions → /api/v1/tenants

# 订阅计划
/api/v1/config/subscription/plans → /api/v1/sysconfig/subscription-plans

# 租户订阅
/api/v1/config/tenants/{id}/subscription → /api/v1/sysconfig/tenants/{id}/subscription
/api/v1/subscriptions/{id}/logs → /api/v1/sysconfig/tenants/{id}/subscription/events

# 功能组
/api/v1/config/tenant-feature-groups → /api/v1/sysconfig/feature-groups
/api/v1/config/feature-groups → /api/v1/sysconfig/feature-groups

# 功能控制
/api/v1/config/tenants/{id}/feature-group → /api/v1/sysconfig/tenants/{id}/feature-group
/api/v1/config/tenants/{id}/feature-overrides → /api/v1/sysconfig/tenants/{id}/feature-overrides
/api/v1/config/tenants/{id}/effective-feature-policy → /api/v1/sysconfig/tenants/{id}/effective-features
/api/v1/config/tenant-feature-options → /api/v1/sysconfig/feature-options
```

### 2. 特殊注意事项

#### 2.1 订阅操作路径变化

```javascript
// 旧代码
POST /api/v1/config/tenants/{id}/subscription/actions

// 新代码
POST /api/v1/sysconfig/tenants/{id}/subscription/action
```

注意：`actions` 变成了 `action`（单数）

#### 2.2 功能组分配方法变化

```javascript
// 旧代码
PUT /api/v1/config/tenants/{id}/feature-group

// 新代码
POST /api/v1/sysconfig/tenants/{id}/feature-group
```

注意：HTTP 方法从 `PUT` 变成了 `POST`

#### 2.3 分页参数标准化

```javascript
// 旧代码
GET /api/v1/config/tenants?limit=100

// 新代码
GET /api/v1/tenants?page=1&page_size=100
```

---

## 测试检查清单

迁移完成后，请测试以下功能：

### Tenant 管理
- [ ] 获取租户列表
- [ ] 查看单个租户详情
- [ ] 更新租户信息
- [ ] 创建新租户（如果有）
- [ ] 删除租户（如果有）

### 订阅管理
- [ ] 查看订阅计划列表
- [ ] 查看租户订阅信息
- [ ] 执行订阅操作（续费、升级、暂停、取消、激活）
- [ ] 查看订阅事件日志
- [ ] 查看有效期变更日志

### 功能管理
- [ ] 查看功能组列表
- [ ] 创建/编辑/删除功能组
- [ ] 为租户分配功能组
- [ ] 设置租户功能覆盖
- [ ] 查看租户有效功能策略
- [ ] 获取功能选项列表

---

## 验收标准

迁移完成后，需要满足以下条件：

1. ✅ 前端代码中**不再有**任何 `/api/v1/config/*` 的调用
2. ✅ 所有 tenant 相关功能正常工作
3. ✅ 浏览器开发者工具中看不到 404 或 API 错误
4. ✅ 所有测试检查清单项通过

---

## 回滚方案

如果迁移后发现问题，可以临时回滚：

后端保留了旧 API 的兼容路由，所以前端可以先回滚到旧代码，不会影响功能。

但请尽快修复问题并重新迁移，因为旧 API 将在 R2 Phase 3 完成后被删除。

---

## 联系方式

如有问题，请联系后端开发团队。

**文档版本：** 1.0
**创建日期：** 2026-04-14
**适用版本：** R2 Phase 2 完成后
