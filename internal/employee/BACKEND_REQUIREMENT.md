# 员工 API 数据字段修复需求

## 问题描述

当前 `/api/v1/employees` 接口返回的员工数据中，`full_name` 字段显示为 "unknown"，导致前端无法正确显示员工姓名。

## 现状分析

### 数据库实际情况
```
employees 表有两个姓名相关字段：
- name: 存储实际姓名（易亮、李明、刘强医生等）
- full_name: 大部分记录存储的是字符串 "unknown"（不是 NULL）

示例数据：
ID: 1, name: "易亮", full_name: "unknown", phone: "18610815948"
ID: 2, name: "李明", full_name: "unknown", phone: "13800138002"
ID: 13, name: "机构联调一号", full_name: "机构联调一号", phone: "13910010001"
```

### 当前 API 返回
```json
{
  "id": 1,
  "full_name": "unknown",
  "phone": "18610815948"
}
```

### 期望 API 返回
```json
{
  "id": 1,
  "full_name": "易亮",
  "phone": "18610815948"
}
```

## 修复方案

需要修改 `internal/employee/store.go` 中的 SQL 查询，使 `full_name` 字段优先使用 `full_name` 列的值，但当 `full_name` 为 "unknown" 或空时，回退使用 `name` 列的值。

### 需要修改的位置

#### 1. ListEmployees 方法（约第 80-93 行）

**当前代码：**
```sql
SELECT id, tenant_id, COALESCE(username, ''), COALESCE(password_hash, ''),
       COALESCE(full_name, ''), COALESCE(phone, ''), COALESCE(email, ''),
       ...
```

**修改为：**
```sql
SELECT id, tenant_id, COALESCE(username, ''), COALESCE(password_hash, ''),
       CASE
           WHEN full_name IS NULL OR full_name = '' OR full_name = 'unknown' THEN name
           ELSE full_name
       END AS full_name,
       COALESCE(phone, ''), COALESCE(email, ''),
       ...
```

#### 2. GetEmployeeByID 方法（约第 131-142 行）

**当前代码：**
```sql
SELECT id, tenant_id, COALESCE(username, ''), COALESCE(password_hash, ''),
       COALESCE(full_name, ''), COALESCE(phone, ''), COALESCE(email, ''),
       ...
```

**修改为：**
```sql
SELECT id, tenant_id, COALESCE(username, ''), COALESCE(password_hash, ''),
       CASE
           WHEN full_name IS NULL OR full_name = '' OR full_name = 'unknown' THEN name
           ELSE full_name
       END AS full_name,
       COALESCE(phone, ''), COALESCE(email, ''),
       ...
```

## 验证方法

修改后，调用 `/api/v1/employees?tenant_id=1` 应该返回：
```json
{
  "items": [
    {
      "id": 1,
      "full_name": "易亮",
      "phone": "18610815948"
    },
    {
      "id": 2,
      "full_name": "李明",
      "phone": "13800138002"
    }
  ]
}
```

## 影响范围

- 文件：`internal/employee/store.go`
- 方法：`ListEmployees`, `GetEmployeeByID`
- 影响：所有调用员工 API 的前端页面将正确显示员工姓名

## 优先级

高 - 影响工牌分配功能的用户体验
