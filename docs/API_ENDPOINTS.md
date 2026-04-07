# 灵策医疗运营系统 API 端点文档
**版本**: 1.0.0  
**端点总数**: 369  
**模块数量**: 11  
**生成时间**: 1775559899.1936533

---

## 目录
- [auth](#auth) (2 个端点)
- [badge](#badge) (46 个端点)
- [content](#content) (74 个端点)
- [customer](#customer) (34 个端点)
- [department](#department) (5 个端点)
- [employee](#employee) (6 个端点)
- [organization](#organization) (30 个端点)
- [rbac](#rbac) (39 个端点)
- [recording](#recording) (78 个端点)
- [support](#support) (36 个端点)
- [sysconfig](#sysconfig) (19 个端点)

---

## 认证方式

所有需要认证的接口都需要在请求头中携带 JWT 令牌：

```http
Authorization: Bearer <your-token>
```

### 用户类型

- **admin**: 运维管理员，可访问所有租户数据
- **employee**: 机构员工（Web端），只能访问自己租户的数据
- **mobile**: 移动端用户，与employee权限相同但会话独立

---

## auth

**端点数量**: 2

### `POST /api/v1/auth/change-password`

**描述**: 修改当前用户密码

**Handler**: `ChangePassword`

**请求体示例**:
```json
{
  "old_password": "old_password",
  "new_password": "new_password123"
}
```

**响应示例**:
```json
{
  "data": {
    "message": "操作成功"
  }
}
```

---
### `GET /api/v1/auth/me`

**描述**: 获取当前登录用户信息

**Handler**: `GetMe`

**响应示例**:
```json
{
  "data": {
    "id": 1,
    "name": "示例名称",
    "created_at": "2024-01-01T00:00:00Z"
  }
}
```

---

## badge

**端点数量**: 46

### `POST /api/v1/badge-control/acceptance/import`

**描述**: 导入数据

**Handler**: `AcceptanceImport`

**请求体示例**:
```json
{
  "data": "请求数据"
}
```

**响应示例**:
```json
{
  "data": {
    "message": "操作成功"
  }
}
```

---
### `POST /api/v1/badge-control/acceptance/validate`

**描述**: 执行 ValidateAcceptance 操作

**Handler**: `ValidateAcceptance`

**请求体示例**:
```json
{
  "data": "请求数据"
}
```

**响应示例**:
```json
{
  "data": {
    "message": "操作成功"
  }
}
```

---
### `POST /api/v1/badge-control/acceptance/vendor-check`

**描述**: 执行 VendorCheck 操作

**Handler**: `VendorCheck`

**请求体示例**:
```json
{
  "data": "请求数据"
}
```

**响应示例**:
```json
{
  "data": {
    "message": "操作成功"
  }
}
```

---
### `POST /api/v1/badge-control/assign/employee`

**描述**: 执行 AssignToEmployee 操作

**Handler**: `AssignToEmployee`

**请求体示例**:
```json
{
  "data": "请求数据"
}
```

**响应示例**:
```json
{
  "data": {
    "message": "操作成功"
  }
}
```

---
### `POST /api/v1/badge-control/assign/tenant`

**描述**: 执行 AssignToTenant 操作

**Handler**: `AssignToTenant`

**请求体示例**:
```json
{
  "data": "请求数据"
}
```

**响应示例**:
```json
{
  "data": {
    "message": "操作成功"
  }
}
```

---
### `GET /api/v1/badge-control/dashboard/summary`

**描述**: 获取详细信息

**Handler**: `GetDashboardSummary`

**响应示例**:
```json
{
  "data": {
    "id": 1,
    "name": "示例名称",
    "created_at": "2024-01-01T00:00:00Z"
  }
}
```

---
### `GET /api/v1/badge-control/devices`

**描述**: 获取列表（支持分页和筛选）

**Handler**: `ListDevices`

**查询参数**:
- `page` (integer): 页码，默认 1
- `page_size` (integer): 每页记录数，默认 20

**响应示例**:
```json
{
  "items": [
    {"id": 1, "name": "示例1"},
    {"id": 2, "name": "示例2"}
  ],
  "total": 100,
  "page": 1,
  "page_size": 20
}
```

---
### `GET /api/v1/badge-control/devices/{device_id}/lifecycle`

**描述**: 获取详细信息

**Handler**: `GetDeviceLifecycle`

**路径参数**:
- `device_id` (integer): device_id参数

**响应示例**:
```json
{
  "data": {
    "id": 1,
    "name": "示例名称",
    "created_at": "2024-01-01T00:00:00Z"
  }
}
```

---
### `POST /api/v1/badge-control/inspection/batch`

**描述**: 执行 BatchInspect 操作

**Handler**: `BatchInspect`

**请求体示例**:
```json
{
  "data": "请求数据"
}
```

**响应示例**:
```json
{
  "data": {
    "message": "操作成功"
  }
}
```

---
### `POST /api/v1/badge-control/inspection/device/{device_id}`

**描述**: 执行 InspectDevice 操作

**Handler**: `InspectDevice`

**路径参数**:
- `device_id` (integer): device_id参数

**请求体示例**:
```json
{
  "data": "请求数据"
}
```

**响应示例**:
```json
{
  "data": {
    "message": "操作成功"
  }
}
```

---
### `GET /api/v1/badge-control/inspection/device/{device_id}/live-status`

**描述**: 获取详细信息

**Handler**: `GetDeviceLiveStatus`

**路径参数**:
- `device_id` (integer): device_id参数

**响应示例**:
```json
{
  "data": {
    "id": 1,
    "name": "示例名称",
    "created_at": "2024-01-01T00:00:00Z"
  }
}
```

---
### `POST /api/v1/badge-control/inspection/device/{device_id}/recording-test`

**描述**: 执行 TestDeviceRecording 操作

**Handler**: `TestDeviceRecording`

**路径参数**:
- `device_id` (integer): device_id参数

**请求体示例**:
```json
{
  "data": "请求数据"
}
```

**响应示例**:
```json
{
  "data": {
    "message": "操作成功"
  }
}
```

---
### `GET /api/v1/badge-control/inspection/devices`

**描述**: 获取列表（支持分页和筛选）

**Handler**: `ListInspectionDevices`

**查询参数**:
- `page` (integer): 页码，默认 1
- `page_size` (integer): 每页记录数，默认 20

**响应示例**:
```json
{
  "items": [
    {"id": 1, "name": "示例1"},
    {"id": 2, "name": "示例2"}
  ],
  "total": 100,
  "page": 1,
  "page_size": 20
}
```

---
### `GET /api/v1/badge-control/manufacturers`

**描述**: 获取列表（支持分页和筛选）

**Handler**: `ListManufacturers`

**查询参数**:
- `page` (integer): 页码，默认 1
- `page_size` (integer): 每页记录数，默认 20

**响应示例**:
```json
{
  "items": [
    {"id": 1, "name": "示例1"},
    {"id": 2, "name": "示例2"}
  ],
  "total": 100,
  "page": 1,
  "page_size": 20
}
```

---
### `PUT /api/v1/badge-control/manufacturers/{manufacturer_code}/config`

**描述**: 更新记录信息

**Handler**: `UpdateManufacturerConfig`

**路径参数**:
- `manufacturer_code` (string): manufacturer_code参数

**请求体示例**:
```json
{
  "name": "示例名称",
  "description": "示例描述"
}
```

**响应示例**:
```json
{
  "data": {
    "message": "操作成功"
  }
}
```

---
### `POST /api/v1/badge-control/reclaim/employee`

**描述**: 执行 ReclaimFromEmployee 操作

**Handler**: `ReclaimFromEmployee`

**请求体示例**:
```json
{
  "data": "请求数据"
}
```

**响应示例**:
```json
{
  "data": {
    "message": "操作成功"
  }
}
```

---
### `POST /api/v1/badge-control/reclaim/tenant`

**描述**: 执行 ReclaimFromTenant 操作

**Handler**: `ReclaimFromTenant`

**请求体示例**:
```json
{
  "data": "请求数据"
}
```

**响应示例**:
```json
{
  "data": {
    "message": "操作成功"
  }
}
```

---
### `GET /api/v1/badge-control/tenant-employees`

**描述**: 获取列表（支持分页和筛选）

**Handler**: `ListTenantEmployees`

**查询参数**:
- `page` (integer): 页码，默认 1
- `page_size` (integer): 每页记录数，默认 20

**响应示例**:
```json
{
  "items": [
    {"id": 1, "name": "示例1"},
    {"id": 2, "name": "示例2"}
  ],
  "total": 100,
  "page": 1,
  "page_size": 20
}
```

---
### `GET /api/v1/badge-control/tickets`

**描述**: 获取列表（支持分页和筛选）

**Handler**: `ListTickets`

**查询参数**:
- `page` (integer): 页码，默认 1
- `page_size` (integer): 每页记录数，默认 20

**响应示例**:
```json
{
  "items": [
    {"id": 1, "name": "示例1"},
    {"id": 2, "name": "示例2"}
  ],
  "total": 100,
  "page": 1,
  "page_size": 20
}
```

---
### `GET /api/v1/badge-control/tickets/my`

**描述**: 获取详细信息

**Handler**: `GetMyTickets`

**响应示例**:
```json
{
  "data": {
    "id": 1,
    "name": "示例名称",
    "created_at": "2024-01-01T00:00:00Z"
  }
}
```

---
### `POST /api/v1/badge-control/tickets/submit`

**描述**: 执行 SubmitTicket 操作

**Handler**: `SubmitTicket`

**请求体示例**:
```json
{
  "data": "请求数据"
}
```

**响应示例**:
```json
{
  "data": {
    "message": "操作成功"
  }
}
```

---
### `POST /api/v1/badge-control/tickets/submit-by-device`

**描述**: 执行 SubmitTicketByDevice 操作

**Handler**: `SubmitTicketByDevice`

**请求体示例**:
```json
{
  "data": "请求数据"
}
```

**响应示例**:
```json
{
  "data": {
    "message": "操作成功"
  }
}
```

---
### `POST /api/v1/badge-control/tickets/{ticket_id}/execute`

**描述**: 执行 ExecuteTicket 操作

**Handler**: `ExecuteTicket`

**路径参数**:
- `ticket_id` (integer): ticket_id参数

**请求体示例**:
```json
{
  "data": "请求数据"
}
```

**响应示例**:
```json
{
  "data": {
    "message": "操作成功"
  }
}
```

---
### `POST /api/v1/badge-control/tickets/{ticket_id}/review`

**描述**: 执行 ReviewTicket 操作

**Handler**: `ReviewTicket`

**路径参数**:
- `ticket_id` (integer): ticket_id参数

**请求体示例**:
```json
{
  "data": "请求数据"
}
```

**响应示例**:
```json
{
  "data": {
    "message": "操作成功"
  }
}
```

---
### `POST /api/v1/badge-control/vendor-pool/actions/create-acceptance-drafts`

**描述**: 创建新记录

**Handler**: `CreateAcceptanceDrafts`

**请求体示例**:
```json
{
  "name": "示例名称",
  "description": "示例描述"
}
```

**响应示例**:
```json
{
  "data": {
    "message": "操作成功"
  }
}
```

---
### `POST /api/v1/badge-control/vendor-pool/actions/create-exception-tickets`

**描述**: 创建新记录

**Handler**: `CreateExceptionTickets`

**请求体示例**:
```json
{
  "name": "示例名称",
  "description": "示例描述"
}
```

**响应示例**:
```json
{
  "data": {
    "message": "操作成功"
  }
}
```

---
### `POST /api/v1/badge-control/vendor-pool/actions/mark-pending-assignment`

**描述**: 执行 MarkPendingAssignment 操作

**Handler**: `MarkPendingAssignment`

**请求体示例**:
```json
{
  "data": "请求数据"
}
```

**响应示例**:
```json
{
  "data": {
    "message": "操作成功"
  }
}
```

---
### `GET /api/v1/badge-control/vendor-pool/diff`

**描述**: 获取详细信息

**Handler**: `GetVendorPoolDiff`

**响应示例**:
```json
{
  "data": {
    "id": 1,
    "name": "示例名称",
    "created_at": "2024-01-01T00:00:00Z"
  }
}
```

---
### `POST /api/v1/badge-control/vendor-pool/sync-and-diff`

**描述**: 同步数据

**Handler**: `SyncAndDiff`

**请求体示例**:
```json
{
  "data": "请求数据"
}
```

**响应示例**:
```json
{
  "data": {
    "message": "操作成功"
  }
}
```

---
### `GET /api/v1/badge-control/vendor-pool/sync-batches`

**描述**: 获取列表（支持分页和筛选）

**Handler**: `ListSyncBatches`

**查询参数**:
- `page` (integer): 页码，默认 1
- `page_size` (integer): 每页记录数，默认 20

**响应示例**:
```json
{
  "items": [
    {"id": 1, "name": "示例1"},
    {"id": 2, "name": "示例2"}
  ],
  "total": 100,
  "page": 1,
  "page_size": 20
}
```

---
### `GET /api/v1/badge-control/vendor-pool/sync-batches/{id}/items`

**描述**: 获取详细信息

**Handler**: `GetSyncBatchItems`

**路径参数**:
- `id` (integer): id参数

**响应示例**:
```json
{
  "data": {
    "id": 1,
    "name": "示例名称",
    "created_at": "2024-01-01T00:00:00Z"
  }
}
```

---
### `POST /api/v1/badge-control/vendor-pool/sync-batches/{id}/rollback-drafts`

**描述**: 获取列表数据（支持分页）

**Handler**: `RollbackDrafts`

**路径参数**:
- `id` (integer): id参数

**请求体示例**:
```json
{
  "data": "请求数据"
}
```

**响应示例**:
```json
{
  "data": {
    "message": "操作成功"
  }
}
```

---
### `POST /api/v1/badge-control/vendor-pool/sync-devices`

**描述**: 同步数据

**Handler**: `SyncVendorDevices`

**请求体示例**:
```json
{
  "data": "请求数据"
}
```

**响应示例**:
```json
{
  "data": {
    "message": "操作成功"
  }
}
```

---
### `POST /api/v1/smart-badge/callback/audio`

**描述**: 执行 AudioCallback 操作

**Handler**: `AudioCallback`

**请求体示例**:
```json
{
  "data": "请求数据"
}
```

**响应示例**:
```json
{
  "data": {
    "message": "操作成功"
  }
}
```

---
### `POST /api/v1/smart-badge/callback/developer`

**描述**: 执行 DeveloperCallback 操作

**Handler**: `DeveloperCallback`

**请求体示例**:
```json
{
  "data": "请求数据"
}
```

**响应示例**:
```json
{
  "data": {
    "message": "操作成功"
  }
}
```

---
### `GET /api/v1/smart-badge/me`

**描述**: 获取详细信息

**Handler**: `GetMyBadgeStatus`

**响应示例**:
```json
{
  "data": {
    "id": 1,
    "name": "示例名称",
    "created_at": "2024-01-01T00:00:00Z"
  }
}
```

---
### `POST /api/v1/smart-badge/me/recording/start`

**描述**: 执行 StartMyRecording 操作

**Handler**: `StartMyRecording`

**请求体示例**:
```json
{
  "data": "请求数据"
}
```

**响应示例**:
```json
{
  "data": {
    "message": "操作成功"
  }
}
```

---
### `POST /api/v1/smart-badge/me/recording/stop`

**描述**: 执行 StopMyRecording 操作

**Handler**: `StopMyRecording`

**请求体示例**:
```json
{
  "data": "请求数据"
}
```

**响应示例**:
```json
{
  "data": {
    "message": "操作成功"
  }
}
```

---
### `POST /api/v1/smart-badge/process/pending`

**描述**: 执行 ProcessPendingEvents 操作

**Handler**: `ProcessPendingEvents`

**请求体示例**:
```json
{
  "data": "请求数据"
}
```

**响应示例**:
```json
{
  "data": {
    "message": "操作成功"
  }
}
```

---
### `GET /api/v1/smart-badge/tenant/devices`

**描述**: 获取详细信息

**Handler**: `GetTenantDevices`

**响应示例**:
```json
{
  "data": {
    "id": 1,
    "name": "示例名称",
    "created_at": "2024-01-01T00:00:00Z"
  }
}
```

---
### `GET /api/v1/smart-badge/tenant/devices/overview`

**描述**: 获取详细信息

**Handler**: `GetTenantDeviceOverview`

**响应示例**:
```json
{
  "data": {
    "id": 1,
    "name": "示例名称",
    "created_at": "2024-01-01T00:00:00Z"
  }
}
```

---
### `GET /api/v1/smart-badge/tenant/devices/{device_no}/history`

**描述**: 获取详细信息

**Handler**: `GetDeviceHistory`

**路径参数**:
- `device_no` (string): device_no参数

**响应示例**:
```json
{
  "data": {
    "id": 1,
    "name": "示例名称",
    "created_at": "2024-01-01T00:00:00Z"
  }
}
```

---
### `POST /api/v1/smart-badge/tenant/devices/{device_no}/recording/start`

**描述**: 执行 StartRecording 操作

**Handler**: `StartRecording`

**路径参数**:
- `device_no` (string): device_no参数

**请求体示例**:
```json
{
  "data": "请求数据"
}
```

**响应示例**:
```json
{
  "data": {
    "message": "操作成功"
  }
}
```

---
### `POST /api/v1/smart-badge/tenant/devices/{device_no}/recording/stop`

**描述**: 执行 StopRecording 操作

**Handler**: `StopRecording`

**路径参数**:
- `device_no` (string): device_no参数

**请求体示例**:
```json
{
  "data": "请求数据"
}
```

**响应示例**:
```json
{
  "data": {
    "message": "操作成功"
  }
}
```

---
### `GET /api/v1/smart-badge/tenant/recording-control/devices`

**描述**: 获取详细信息

**Handler**: `GetRecordingControlDevices`

**响应示例**:
```json
{
  "data": {
    "id": 1,
    "name": "示例名称",
    "created_at": "2024-01-01T00:00:00Z"
  }
}
```

---
### `GET /api/v1/smart-badge/tenant/recording-control/logs`

**描述**: 获取详细信息

**Handler**: `GetRecordingControlLogs`

**响应示例**:
```json
{
  "data": {
    "id": 1,
    "name": "示例名称",
    "created_at": "2024-01-01T00:00:00Z"
  }
}
```

---

## content

**端点数量**: 74

### `GET /api/v1/content-prompt-templates`

**描述**: 获取列表（支持分页和筛选）

**Handler**: `ListContentTemplates`

**查询参数**:
- `page` (integer): 页码，默认 1
- `page_size` (integer): 每页记录数，默认 20

**响应示例**:
```json
{
  "items": [
    {"id": 1, "name": "示例1"},
    {"id": 2, "name": "示例2"}
  ],
  "total": 100,
  "page": 1,
  "page_size": 20
}
```

---
### `POST /api/v1/content-prompt-templates`

**描述**: 创建新记录

**Handler**: `CreateContentTemplate`

**请求体示例**:
```json
{
  "name": "示例名称",
  "description": "示例描述"
}
```

**响应示例**:
```json
{
  "data": {
    "message": "操作成功"
  }
}
```

---
### `POST /api/v1/content-prompt-templates/initialize-defaults`

**描述**: 获取列表数据（支持分页）

**Handler**: `InitializeDefaultTemplates`

**请求体示例**:
```json
{
  "data": "请求数据"
}
```

**响应示例**:
```json
{
  "data": {
    "message": "操作成功"
  }
}
```

---
### `GET /api/v1/content-prompt-templates/{template_id}`

**描述**: 获取详细信息

**Handler**: `GetContentTemplate`

**路径参数**:
- `template_id` (integer): template_id参数

**响应示例**:
```json
{
  "data": {
    "id": 1,
    "name": "示例名称",
    "created_at": "2024-01-01T00:00:00Z"
  }
}
```

---
### `PUT /api/v1/content-prompt-templates/{template_id}`

**描述**: 更新记录信息

**Handler**: `UpdateContentTemplate`

**路径参数**:
- `template_id` (integer): template_id参数

**请求体示例**:
```json
{
  "name": "示例名称",
  "description": "示例描述"
}
```

**响应示例**:
```json
{
  "data": {
    "message": "操作成功"
  }
}
```

---
### `DELETE /api/v1/content-prompt-templates/{template_id}`

**描述**: 删除记录（软删除）

**Handler**: `DeleteContentTemplate`

**路径参数**:
- `template_id` (integer): template_id参数

**响应示例**:
```json
{
  "data": {
    "message": "操作成功"
  }
}
```

---
### `POST /api/v1/content-prompt-templates/{template_id}/clone`

**描述**: 执行 CloneContentTemplate 操作

**Handler**: `CloneContentTemplate`

**路径参数**:
- `template_id` (integer): template_id参数

**请求体示例**:
```json
{
  "data": "请求数据"
}
```

**响应示例**:
```json
{
  "data": {
    "message": "操作成功"
  }
}
```

---
### `GET /api/v1/content-prompt-templates/{template_id}/stats`

**描述**: 获取详细信息

**Handler**: `GetContentTemplateStats`

**路径参数**:
- `template_id` (integer): template_id参数

**响应示例**:
```json
{
  "data": {
    "id": 1,
    "name": "示例名称",
    "created_at": "2024-01-01T00:00:00Z"
  }
}
```

---
### `POST /api/v1/content-prompt-templates/{template_id}/test`

**描述**: 执行 TestContentTemplate 操作

**Handler**: `TestContentTemplate`

**路径参数**:
- `template_id` (integer): template_id参数

**请求体示例**:
```json
{
  "data": "请求数据"
}
```

**响应示例**:
```json
{
  "data": {
    "message": "操作成功"
  }
}
```

---
### `GET /api/v1/content-seeds`

**描述**: 获取列表（支持分页和筛选）

**Handler**: `ListSeeds`

**查询参数**:
- `page` (integer): 页码，默认 1
- `page_size` (integer): 每页记录数，默认 20

**响应示例**:
```json
{
  "items": [
    {"id": 1, "name": "示例1"},
    {"id": 2, "name": "示例2"}
  ],
  "total": 100,
  "page": 1,
  "page_size": 20
}
```

---
### `GET /api/v1/content-seeds/clusters`

**描述**: 获取详细信息

**Handler**: `GetClusters`

**响应示例**:
```json
{
  "data": {
    "id": 1,
    "name": "示例名称",
    "created_at": "2024-01-01T00:00:00Z"
  }
}
```

---
### `GET /api/v1/content-seeds/honor-list`

**描述**: 获取列表（支持分页和筛选）

**Handler**: `GetHonorList`

**查询参数**:
- `page` (integer): 页码，默认 1
- `page_size` (integer): 每页记录数，默认 20

**响应示例**:
```json
{
  "items": [
    {"id": 1, "name": "示例1"},
    {"id": 2, "name": "示例2"}
  ],
  "total": 100,
  "page": 1,
  "page_size": 20
}
```

---
### `GET /api/v1/content-seeds/my-adopted`

**描述**: 获取详细信息

**Handler**: `GetMyAdopted`

**响应示例**:
```json
{
  "data": {
    "id": 1,
    "name": "示例名称",
    "created_at": "2024-01-01T00:00:00Z"
  }
}
```

---
### `GET /api/v1/content-seeds/my-inspirations`

**描述**: 获取详细信息

**Handler**: `GetMyInspirations`

**响应示例**:
```json
{
  "data": {
    "id": 1,
    "name": "示例名称",
    "created_at": "2024-01-01T00:00:00Z"
  }
}
```

---
### `GET /api/v1/content-seeds/my-stats`

**描述**: 获取详细信息

**Handler**: `GetMyStats`

**响应示例**:
```json
{
  "data": {
    "id": 1,
    "name": "示例名称",
    "created_at": "2024-01-01T00:00:00Z"
  }
}
```

---
### `GET /api/v1/content-seeds/stats`

**描述**: 获取详细信息

**Handler**: `GetSeedStats`

**响应示例**:
```json
{
  "data": {
    "id": 1,
    "name": "示例名称",
    "created_at": "2024-01-01T00:00:00Z"
  }
}
```

---
### `GET /api/v1/content-seeds/{seed_id}`

**描述**: 获取详细信息

**Handler**: `GetSeed`

**路径参数**:
- `seed_id` (integer): seed_id参数

**响应示例**:
```json
{
  "data": {
    "id": 1,
    "name": "示例名称",
    "created_at": "2024-01-01T00:00:00Z"
  }
}
```

---
### `POST /api/v1/content-seeds/{seed_id}/dismiss`

**描述**: 获取列表数据（支持分页）

**Handler**: `DismissSeed`

**路径参数**:
- `seed_id` (integer): seed_id参数

**请求体示例**:
```json
{
  "data": "请求数据"
}
```

**响应示例**:
```json
{
  "data": {
    "message": "操作成功"
  }
}
```

---
### `POST /api/v1/content-seeds/{seed_id}/generate-draft`

**描述**: 执行 GenerateDraftFromSeed 操作

**Handler**: `GenerateDraftFromSeed`

**路径参数**:
- `seed_id` (integer): seed_id参数

**请求体示例**:
```json
{
  "data": "请求数据"
}
```

**响应示例**:
```json
{
  "data": {
    "message": "操作成功"
  }
}
```

---
### `PATCH /api/v1/content-seeds/{seed_id}/status`

**描述**: 更新记录信息

**Handler**: `UpdateSeedStatus`

**路径参数**:
- `seed_id` (integer): seed_id参数

**请求体示例**:
```json
{
  "name": "示例名称",
  "description": "示例描述"
}
```

**响应示例**:
```json
{
  "data": {
    "message": "操作成功"
  }
}
```

---
### `GET /api/v1/content/contents`

**描述**: 获取列表（支持分页和筛选）

**Handler**: `ListContents`

**查询参数**:
- `page` (integer): 页码，默认 1
- `page_size` (integer): 每页记录数，默认 20

**响应示例**:
```json
{
  "items": [
    {"id": 1, "name": "示例1"},
    {"id": 2, "name": "示例2"}
  ],
  "total": 100,
  "page": 1,
  "page_size": 20
}
```

---
### `POST /api/v1/content/contents`

**描述**: 创建新记录

**Handler**: `CreateContent`

**请求体示例**:
```json
{
  "name": "示例名称",
  "description": "示例描述"
}
```

**响应示例**:
```json
{
  "data": {
    "message": "操作成功"
  }
}
```

---
### `POST /api/v1/content/contents/generate`

**描述**: 执行 GenerateContent 操作

**Handler**: `GenerateContent`

**请求体示例**:
```json
{
  "data": "请求数据"
}
```

**响应示例**:
```json
{
  "data": {
    "message": "操作成功"
  }
}
```

---
### `GET /api/v1/content/contents/{content_id}`

**描述**: 获取详细信息

**Handler**: `GetContent`

**路径参数**:
- `content_id` (integer): content_id参数

**响应示例**:
```json
{
  "data": {
    "id": 1,
    "name": "示例名称",
    "created_at": "2024-01-01T00:00:00Z"
  }
}
```

---
### `PUT /api/v1/content/contents/{content_id}`

**描述**: 更新记录信息

**Handler**: `UpdateContent`

**路径参数**:
- `content_id` (integer): content_id参数

**请求体示例**:
```json
{
  "name": "示例名称",
  "description": "示例描述"
}
```

**响应示例**:
```json
{
  "data": {
    "message": "操作成功"
  }
}
```

---
### `DELETE /api/v1/content/contents/{content_id}`

**描述**: 删除记录（软删除）

**Handler**: `DeleteContent`

**路径参数**:
- `content_id` (integer): content_id参数

**响应示例**:
```json
{
  "data": {
    "message": "操作成功"
  }
}
```

---
### `POST /api/v1/content/contents/{content_id}/generate-images`

**描述**: 获取列表数据（支持分页）

**Handler**: `GenerateImages`

**路径参数**:
- `content_id` (integer): content_id参数

**请求体示例**:
```json
{
  "data": "请求数据"
}
```

**响应示例**:
```json
{
  "data": {
    "message": "操作成功"
  }
}
```

---
### `POST /api/v1/content/contents/{content_id}/generate-single-image`

**描述**: 执行 GenerateSingleImage 操作

**Handler**: `GenerateSingleImage`

**路径参数**:
- `content_id` (integer): content_id参数

**请求体示例**:
```json
{
  "data": "请求数据"
}
```

**响应示例**:
```json
{
  "data": {
    "message": "操作成功"
  }
}
```

---
### `POST /api/v1/content/contents/{content_id}/publish`

**描述**: 执行 PublishContent 操作

**Handler**: `PublishContent`

**路径参数**:
- `content_id` (integer): content_id参数

**请求体示例**:
```json
{
  "data": "请求数据"
}
```

**响应示例**:
```json
{
  "data": {
    "message": "操作成功"
  }
}
```

---
### `POST /api/v1/content/contents/{content_id}/save-composed-images`

**描述**: 获取列表数据（支持分页）

**Handler**: `SaveComposedImages`

**路径参数**:
- `content_id` (integer): content_id参数

**请求体示例**:
```json
{
  "data": "请求数据"
}
```

**响应示例**:
```json
{
  "data": {
    "message": "操作成功"
  }
}
```

---
### `POST /api/v1/content/contents/{content_id}/unpublish`

**描述**: 执行 UnpublishContent 操作

**Handler**: `UnpublishContent`

**路径参数**:
- `content_id` (integer): content_id参数

**请求体示例**:
```json
{
  "data": "请求数据"
}
```

**响应示例**:
```json
{
  "data": {
    "message": "操作成功"
  }
}
```

---
### `GET /api/v1/content/conversation-insights/frequent-questions`

**描述**: 获取详细信息

**Handler**: `GetFrequentQuestions`

**响应示例**:
```json
{
  "data": {
    "id": 1,
    "name": "示例名称",
    "created_at": "2024-01-01T00:00:00Z"
  }
}
```

---
### `POST /api/v1/content/conversation-insights/mine-topics`

**描述**: 获取列表数据（支持分页）

**Handler**: `MineTopics`

**请求体示例**:
```json
{
  "data": "请求数据"
}
```

**响应示例**:
```json
{
  "data": {
    "message": "操作成功"
  }
}
```

---
### `POST /api/v1/content/conversation-insights/save-topics`

**描述**: 获取列表数据（支持分页）

**Handler**: `SaveMinedTopics`

**请求体示例**:
```json
{
  "data": "请求数据"
}
```

**响应示例**:
```json
{
  "data": {
    "message": "操作成功"
  }
}
```

---
### `GET /api/v1/content/conversation-insights/stats`

**描述**: 获取详细信息

**Handler**: `GetInsightsStats`

**响应示例**:
```json
{
  "data": {
    "id": 1,
    "name": "示例名称",
    "created_at": "2024-01-01T00:00:00Z"
  }
}
```

---
### `POST /api/v1/content/geo/analyze`

**描述**: 执行 AnalyzeGEO 操作

**Handler**: `AnalyzeGEO`

**请求体示例**:
```json
{
  "data": "请求数据"
}
```

**响应示例**:
```json
{
  "data": {
    "message": "操作成功"
  }
}
```

---
### `POST /api/v1/content/geo/analyze-by-id/{content_id}`

**描述**: 执行 AnalyzeGEOByID 操作

**Handler**: `AnalyzeGEOByID`

**路径参数**:
- `content_id` (integer): content_id参数

**请求体示例**:
```json
{
  "data": "请求数据"
}
```

**响应示例**:
```json
{
  "data": {
    "message": "操作成功"
  }
}
```

---
### `POST /api/v1/content/geo/optimize`

**描述**: 执行 OptimizeGEO 操作

**Handler**: `OptimizeGEO`

**请求体示例**:
```json
{
  "data": "请求数据"
}
```

**响应示例**:
```json
{
  "data": {
    "message": "操作成功"
  }
}
```

---
### `GET /api/v1/content/geo/prompt-injection`

**描述**: 获取详细信息

**Handler**: `GetGEOPromptInjection`

**响应示例**:
```json
{
  "data": {
    "id": 1,
    "name": "示例名称",
    "created_at": "2024-01-01T00:00:00Z"
  }
}
```

---
### `GET /api/v1/content/hot-topics`

**描述**: 获取详细信息

**Handler**: `GetHotTopics`

**响应示例**:
```json
{
  "data": {
    "id": 1,
    "name": "示例名称",
    "created_at": "2024-01-01T00:00:00Z"
  }
}
```

---
### `POST /api/v1/content/hot-topics/refresh`

**描述**: 执行 RefreshHotTopics 操作

**Handler**: `RefreshHotTopics`

**请求体示例**:
```json
{
  "data": "请求数据"
}
```

**响应示例**:
```json
{
  "data": {
    "message": "操作成功"
  }
}
```

---
### `POST /api/v1/content/idea-topics/generate`

**描述**: 执行 IdeaGenerateTopics 操作

**Handler**: `IdeaGenerateTopics`

**请求体示例**:
```json
{
  "data": "请求数据"
}
```

**响应示例**:
```json
{
  "data": {
    "message": "操作成功"
  }
}
```

---
### `POST /api/v1/content/idea-topics/parse-files`

**描述**: 获取列表数据（支持分页）

**Handler**: `ParseFiles`

**请求体示例**:
```json
{
  "data": "请求数据"
}
```

**响应示例**:
```json
{
  "data": {
    "message": "操作成功"
  }
}
```

---
### `POST /api/v1/content/idea-topics/save-topics`

**描述**: 获取列表数据（支持分页）

**Handler**: `SaveIdeaTopics`

**请求体示例**:
```json
{
  "data": "请求数据"
}
```

**响应示例**:
```json
{
  "data": {
    "message": "操作成功"
  }
}
```

---
### `POST /api/v1/content/idea-topics/start`

**描述**: 执行 IdeaTopicStart 操作

**Handler**: `IdeaTopicStart`

**请求体示例**:
```json
{
  "data": "请求数据"
}
```

**响应示例**:
```json
{
  "data": {
    "message": "操作成功"
  }
}
```

---
### `GET /api/v1/content/publish-tasks`

**描述**: 获取列表（支持分页和筛选）

**Handler**: `ListPublishTasks`

**查询参数**:
- `page` (integer): 页码，默认 1
- `page_size` (integer): 每页记录数，默认 20

**响应示例**:
```json
{
  "items": [
    {"id": 1, "name": "示例1"},
    {"id": 2, "name": "示例2"}
  ],
  "total": 100,
  "page": 1,
  "page_size": 20
}
```

---
### `POST /api/v1/content/publish-tasks`

**描述**: 创建新记录

**Handler**: `CreatePublishTask`

**请求体示例**:
```json
{
  "name": "示例名称",
  "description": "示例描述"
}
```

**响应示例**:
```json
{
  "data": {
    "message": "操作成功"
  }
}
```

---
### `POST /api/v1/content/publish-tasks/batch`

**描述**: 创建新记录

**Handler**: `BatchCreatePublishTasks`

**请求体示例**:
```json
{
  "name": "示例名称",
  "description": "示例描述"
}
```

**响应示例**:
```json
{
  "data": {
    "message": "操作成功"
  }
}
```

---
### `GET /api/v1/content/publish-tasks/dashboard`

**描述**: 获取详细信息

**Handler**: `GetPublishDashboard`

**响应示例**:
```json
{
  "data": {
    "id": 1,
    "name": "示例名称",
    "created_at": "2024-01-01T00:00:00Z"
  }
}
```

---
### `GET /api/v1/content/publish-tasks/{task_id}`

**描述**: 获取详细信息

**Handler**: `GetPublishTask`

**路径参数**:
- `task_id` (integer): task_id参数

**响应示例**:
```json
{
  "data": {
    "id": 1,
    "name": "示例名称",
    "created_at": "2024-01-01T00:00:00Z"
  }
}
```

---
### `DELETE /api/v1/content/publish-tasks/{task_id}`

**描述**: 删除记录（软删除）

**Handler**: `DeletePublishTask`

**路径参数**:
- `task_id` (integer): task_id参数

**响应示例**:
```json
{
  "data": {
    "message": "操作成功"
  }
}
```

---
### `PUT /api/v1/content/publish-tasks/{task_id}/cancel`

**描述**: 执行 CancelPublishTask 操作

**Handler**: `CancelPublishTask`

**路径参数**:
- `task_id` (integer): task_id参数

**请求体示例**:
```json
{
  "data": "请求数据"
}
```

**响应示例**:
```json
{
  "data": {
    "message": "操作成功"
  }
}
```

---
### `PUT /api/v1/content/publish-tasks/{task_id}/retry`

**描述**: 执行 RetryPublishTask 操作

**Handler**: `RetryPublishTask`

**路径参数**:
- `task_id` (integer): task_id参数

**请求体示例**:
```json
{
  "data": "请求数据"
}
```

**响应示例**:
```json
{
  "data": {
    "message": "操作成功"
  }
}
```

---
### `PUT /api/v1/content/publish-tasks/{task_id}/status`

**描述**: 更新记录信息

**Handler**: `UpdatePublishTaskStatus`

**路径参数**:
- `task_id` (integer): task_id参数

**请求体示例**:
```json
{
  "name": "示例名称",
  "description": "示例描述"
}
```

**响应示例**:
```json
{
  "data": {
    "message": "操作成功"
  }
}
```

---
### `GET /api/v1/content/topics`

**描述**: 获取列表（支持分页和筛选）

**Handler**: `ListTopics`

**查询参数**:
- `page` (integer): 页码，默认 1
- `page_size` (integer): 每页记录数，默认 20

**响应示例**:
```json
{
  "items": [
    {"id": 1, "name": "示例1"},
    {"id": 2, "name": "示例2"}
  ],
  "total": 100,
  "page": 1,
  "page_size": 20
}
```

---
### `POST /api/v1/content/topics`

**描述**: 创建新记录

**Handler**: `CreateTopic`

**请求体示例**:
```json
{
  "name": "示例名称",
  "description": "示例描述"
}
```

**响应示例**:
```json
{
  "data": {
    "message": "操作成功"
  }
}
```

---
### `POST /api/v1/content/topics/generate`

**描述**: 执行 GenerateTopics 操作

**Handler**: `GenerateTopics`

**请求体示例**:
```json
{
  "data": "请求数据"
}
```

**响应示例**:
```json
{
  "data": {
    "message": "操作成功"
  }
}
```

---
### `GET /api/v1/content/topics/{topic_id}`

**描述**: 获取详细信息

**Handler**: `GetTopic`

**路径参数**:
- `topic_id` (integer): topic_id参数

**响应示例**:
```json
{
  "data": {
    "id": 1,
    "name": "示例名称",
    "created_at": "2024-01-01T00:00:00Z"
  }
}
```

---
### `PUT /api/v1/content/topics/{topic_id}`

**描述**: 更新记录信息

**Handler**: `UpdateTopic`

**路径参数**:
- `topic_id` (integer): topic_id参数

**请求体示例**:
```json
{
  "name": "示例名称",
  "description": "示例描述"
}
```

**响应示例**:
```json
{
  "data": {
    "message": "操作成功"
  }
}
```

---
### `DELETE /api/v1/content/topics/{topic_id}`

**描述**: 删除记录（软删除）

**Handler**: `DeleteTopic`

**路径参数**:
- `topic_id` (integer): topic_id参数

**响应示例**:
```json
{
  "data": {
    "message": "操作成功"
  }
}
```

---
### `PUT /api/v1/content/topics/{topic_id}/select`

**描述**: 执行 SelectTopic 操作

**Handler**: `SelectTopic`

**路径参数**:
- `topic_id` (integer): topic_id参数

**请求体示例**:
```json
{
  "data": "请求数据"
}
```

**响应示例**:
```json
{
  "data": {
    "message": "操作成功"
  }
}
```

---
### `GET /api/v1/prompt-templates`

**描述**: 获取列表（支持分页和筛选）

**Handler**: `ListTemplates`

**查询参数**:
- `page` (integer): 页码，默认 1
- `page_size` (integer): 每页记录数，默认 20

**响应示例**:
```json
{
  "items": [
    {"id": 1, "name": "示例1"},
    {"id": 2, "name": "示例2"}
  ],
  "total": 100,
  "page": 1,
  "page_size": 20
}
```

---
### `POST /api/v1/prompt-templates`

**描述**: 创建新记录

**Handler**: `CreateTemplate`

**请求体示例**:
```json
{
  "name": "示例名称",
  "description": "示例描述"
}
```

**响应示例**:
```json
{
  "data": {
    "message": "操作成功"
  }
}
```

---
### `POST /api/v1/prompt-templates/preview`

**描述**: 执行 PreviewTemplate 操作

**Handler**: `PreviewTemplate`

**请求体示例**:
```json
{
  "data": "请求数据"
}
```

**响应示例**:
```json
{
  "data": {
    "message": "操作成功"
  }
}
```

---
### `GET /api/v1/prompt-templates/{template_id}`

**描述**: 获取详细信息

**Handler**: `GetTemplate`

**路径参数**:
- `template_id` (integer): template_id参数

**响应示例**:
```json
{
  "data": {
    "id": 1,
    "name": "示例名称",
    "created_at": "2024-01-01T00:00:00Z"
  }
}
```

---
### `PUT /api/v1/prompt-templates/{template_id}`

**描述**: 更新记录信息

**Handler**: `UpdateTemplate`

**路径参数**:
- `template_id` (integer): template_id参数

**请求体示例**:
```json
{
  "name": "示例名称",
  "description": "示例描述"
}
```

**响应示例**:
```json
{
  "data": {
    "message": "操作成功"
  }
}
```

---
### `DELETE /api/v1/prompt-templates/{template_id}`

**描述**: 删除记录（软删除）

**Handler**: `DeleteTemplate`

**路径参数**:
- `template_id` (integer): template_id参数

**响应示例**:
```json
{
  "data": {
    "message": "操作成功"
  }
}
```

---
### `POST /api/v1/prompt-templates/{template_id}/clone`

**描述**: 执行 CloneTemplate 操作

**Handler**: `CloneTemplate`

**路径参数**:
- `template_id` (integer): template_id参数

**请求体示例**:
```json
{
  "data": "请求数据"
}
```

**响应示例**:
```json
{
  "data": {
    "message": "操作成功"
  }
}
```

---
### `POST /api/v1/prompt-templates/{template_id}/publish`

**描述**: 执行 PublishTemplate 操作

**Handler**: `PublishTemplate`

**路径参数**:
- `template_id` (integer): template_id参数

**请求体示例**:
```json
{
  "data": "请求数据"
}
```

**响应示例**:
```json
{
  "data": {
    "message": "操作成功"
  }
}
```

---
### `POST /api/v1/prompt-templates/{template_id}/rollback/{version}`

**描述**: 执行 RollbackTemplate 操作

**Handler**: `RollbackTemplate`

**路径参数**:
- `template_id` (integer): template_id参数
- `version` (string): version参数

**请求体示例**:
```json
{
  "data": "请求数据"
}
```

**响应示例**:
```json
{
  "data": {
    "message": "操作成功"
  }
}
```

---
### `GET /api/v1/prompt-templates/{template_id}/stats`

**描述**: 获取详细信息

**Handler**: `GetTemplateStats`

**路径参数**:
- `template_id` (integer): template_id参数

**响应示例**:
```json
{
  "data": {
    "id": 1,
    "name": "示例名称",
    "created_at": "2024-01-01T00:00:00Z"
  }
}
```

---
### `POST /api/v1/prompt-templates/{template_id}/test`

**描述**: 执行 TestTemplate 操作

**Handler**: `TestTemplate`

**路径参数**:
- `template_id` (integer): template_id参数

**请求体示例**:
```json
{
  "data": "请求数据"
}
```

**响应示例**:
```json
{
  "data": {
    "message": "操作成功"
  }
}
```

---
### `POST /api/v1/prompt-templates/{template_id}/versions`

**描述**: 创建新记录

**Handler**: `CreateTemplateVersion`

**路径参数**:
- `template_id` (integer): template_id参数

**请求体示例**:
```json
{
  "name": "示例名称",
  "description": "示例描述"
}
```

**响应示例**:
```json
{
  "data": {
    "message": "操作成功"
  }
}
```

---
### `GET /api/v1/prompt-templates/{template_id}/versions`

**描述**: 获取列表（支持分页和筛选）

**Handler**: `ListTemplateVersions`

**路径参数**:
- `template_id` (integer): template_id参数

**查询参数**:
- `page` (integer): 页码，默认 1
- `page_size` (integer): 每页记录数，默认 20

**响应示例**:
```json
{
  "items": [
    {"id": 1, "name": "示例1"},
    {"id": 2, "name": "示例2"}
  ],
  "total": 100,
  "page": 1,
  "page_size": 20
}
```

---

## customer

**端点数量**: 34

### `GET /api/v1/customer-groups`

**描述**: 获取列表（支持分页和筛选）

**Handler**: `ListCustomerGroups`

**查询参数**:
- `page` (integer): 页码，默认 1
- `page_size` (integer): 每页记录数，默认 20

**响应示例**:
```json
{
  "items": [
    {"id": 1, "name": "示例1"},
    {"id": 2, "name": "示例2"}
  ],
  "total": 100,
  "page": 1,
  "page_size": 20
}
```

---
### `POST /api/v1/customer-groups`

**描述**: 创建新记录

**Handler**: `CreateCustomerGroup`

**请求体示例**:
```json
{
  "name": "示例名称",
  "description": "示例描述"
}
```

**响应示例**:
```json
{
  "data": {
    "message": "操作成功"
  }
}
```

---
### `GET /api/v1/customer-groups/rules/fields`

**描述**: 获取详细信息

**Handler**: `GetRuleFields`

**响应示例**:
```json
{
  "data": {
    "id": 1,
    "name": "示例名称",
    "created_at": "2024-01-01T00:00:00Z"
  }
}
```

---
### `GET /api/v1/customer-groups/rules/operators`

**描述**: 获取详细信息

**Handler**: `GetRuleOperators`

**响应示例**:
```json
{
  "data": {
    "id": 1,
    "name": "示例名称",
    "created_at": "2024-01-01T00:00:00Z"
  }
}
```

---
### `POST /api/v1/customer-groups/rules/preview`

**描述**: 执行 PreviewGroupRules 操作

**Handler**: `PreviewGroupRules`

**请求体示例**:
```json
{
  "data": "请求数据"
}
```

**响应示例**:
```json
{
  "data": {
    "message": "操作成功"
  }
}
```

---
### `POST /api/v1/customer-groups/rules/validate`

**描述**: 执行 ValidateGroupRules 操作

**Handler**: `ValidateGroupRules`

**请求体示例**:
```json
{
  "data": "请求数据"
}
```

**响应示例**:
```json
{
  "data": {
    "message": "操作成功"
  }
}
```

---
### `PUT /api/v1/customer-groups/{id}`

**描述**: 更新记录信息

**Handler**: `UpdateCustomerGroup`

**路径参数**:
- `id` (integer): id参数

**请求体示例**:
```json
{
  "name": "示例名称",
  "description": "示例描述"
}
```

**响应示例**:
```json
{
  "data": {
    "message": "操作成功"
  }
}
```

---
### `DELETE /api/v1/customer-groups/{id}`

**描述**: 删除记录（软删除）

**Handler**: `DeleteCustomerGroup`

**路径参数**:
- `id` (integer): id参数

**响应示例**:
```json
{
  "data": {
    "message": "操作成功"
  }
}
```

---
### `GET /api/v1/customer-groups/{id}/members`

**描述**: 获取详细信息

**Handler**: `GetGroupMembers`

**路径参数**:
- `id` (integer): id参数

**响应示例**:
```json
{
  "data": {
    "id": 1,
    "name": "示例名称",
    "created_at": "2024-01-01T00:00:00Z"
  }
}
```

---
### `POST /api/v1/customer-groups/{id}/members`

**描述**: 获取列表数据（支持分页）

**Handler**: `AddGroupMembers`

**路径参数**:
- `id` (integer): id参数

**请求体示例**:
```json
{
  "data": "请求数据"
}
```

**响应示例**:
```json
{
  "data": {
    "message": "操作成功"
  }
}
```

---
### `DELETE /api/v1/customer-groups/{id}/members`

**描述**: 获取列表数据（支持分页）

**Handler**: `RemoveGroupMembers`

**路径参数**:
- `id` (integer): id参数

**响应示例**:
```json
{
  "data": {
    "message": "操作成功"
  }
}
```

---
### `GET /api/v1/customer-tags`

**描述**: 获取列表（支持分页和筛选）

**Handler**: `ListCustomerTags`

**查询参数**:
- `page` (integer): 页码，默认 1
- `page_size` (integer): 每页记录数，默认 20

**响应示例**:
```json
{
  "items": [
    {"id": 1, "name": "示例1"},
    {"id": 2, "name": "示例2"}
  ],
  "total": 100,
  "page": 1,
  "page_size": 20
}
```

---
### `POST /api/v1/customer-tags`

**描述**: 创建新记录

**Handler**: `CreateCustomerTag`

**请求体示例**:
```json
{
  "name": "示例名称",
  "description": "示例描述"
}
```

**响应示例**:
```json
{
  "data": {
    "message": "操作成功"
  }
}
```

---
### `POST /api/v1/customer-tags/batch`

**描述**: 执行 BatchTagCustomers 操作

**Handler**: `BatchTagCustomers`

**请求体示例**:
```json
{
  "data": "请求数据"
}
```

**响应示例**:
```json
{
  "data": {
    "message": "操作成功"
  }
}
```

---
### `GET /api/v1/customer-tags/stats`

**描述**: 获取详细信息

**Handler**: `GetTagStats`

**响应示例**:
```json
{
  "data": {
    "id": 1,
    "name": "示例名称",
    "created_at": "2024-01-01T00:00:00Z"
  }
}
```

---
### `PUT /api/v1/customer-tags/{id}`

**描述**: 更新记录信息

**Handler**: `UpdateCustomerTag`

**路径参数**:
- `id` (integer): id参数

**请求体示例**:
```json
{
  "name": "示例名称",
  "description": "示例描述"
}
```

**响应示例**:
```json
{
  "data": {
    "message": "操作成功"
  }
}
```

---
### `DELETE /api/v1/customer-tags/{id}`

**描述**: 删除记录（软删除）

**Handler**: `DeleteCustomerTag`

**路径参数**:
- `id` (integer): id参数

**响应示例**:
```json
{
  "data": {
    "message": "操作成功"
  }
}
```

---
### `GET /api/v1/customers`

**描述**: 获取列表（支持分页和筛选）

**Handler**: `ListCustomers`

**查询参数**:
- `page` (integer): 页码，默认 1
- `page_size` (integer): 每页记录数，默认 20

**响应示例**:
```json
{
  "items": [
    {"id": 1, "name": "示例1"},
    {"id": 2, "name": "示例2"}
  ],
  "total": 100,
  "page": 1,
  "page_size": 20
}
```

---
### `POST /api/v1/customers`

**描述**: 创建新记录

**Handler**: `CreateCustomer`

**请求体示例**:
```json
{
  "name": "示例名称",
  "description": "示例描述"
}
```

**响应示例**:
```json
{
  "data": {
    "message": "操作成功"
  }
}
```

---
### `GET /api/v1/customers/duplicates`

**描述**: 获取列表数据（支持分页）

**Handler**: `CheckDuplicates`

**响应示例**:
```json
{
  "data": {
    "message": "操作成功"
  }
}
```

---
### `POST /api/v1/customers/merge`

**描述**: 执行 MergeCustomers 操作

**Handler**: `MergeCustomers`

**请求体示例**:
```json
{
  "data": "请求数据"
}
```

**响应示例**:
```json
{
  "data": {
    "message": "操作成功"
  }
}
```

---
### `GET /api/v1/customers/stats/overview`

**描述**: 获取详细信息

**Handler**: `GetCustomerStats`

**响应示例**:
```json
{
  "data": {
    "id": 1,
    "name": "示例名称",
    "created_at": "2024-01-01T00:00:00Z"
  }
}
```

---
### `GET /api/v1/customers/{id}`

**描述**: 获取详细信息

**Handler**: `GetCustomerByID`

**路径参数**:
- `id` (integer): id参数

**响应示例**:
```json
{
  "data": {
    "id": 1,
    "name": "示例名称",
    "created_at": "2024-01-01T00:00:00Z"
  }
}
```

---
### `PUT /api/v1/customers/{id}`

**描述**: 更新记录信息

**Handler**: `UpdateCustomer`

**路径参数**:
- `id` (integer): id参数

**请求体示例**:
```json
{
  "name": "示例名称",
  "description": "示例描述"
}
```

**响应示例**:
```json
{
  "data": {
    "message": "操作成功"
  }
}
```

---
### `GET /api/v1/customers/{id}/consultation-records`

**描述**: 获取详细信息

**Handler**: `GetConsultationRecords`

**路径参数**:
- `id` (integer): id参数

**响应示例**:
```json
{
  "data": {
    "id": 1,
    "name": "示例名称",
    "created_at": "2024-01-01T00:00:00Z"
  }
}
```

---
### `PUT /api/v1/customers/{id}/converted`

**描述**: 执行 MarkCustomerConverted 操作

**Handler**: `MarkCustomerConverted`

**路径参数**:
- `id` (integer): id参数

**请求体示例**:
```json
{
  "data": "请求数据"
}
```

**响应示例**:
```json
{
  "data": {
    "message": "操作成功"
  }
}
```

---
### `GET /api/v1/customers/{id}/emr-records`

**描述**: 获取详细信息

**Handler**: `GetEMRRecords`

**路径参数**:
- `id` (integer): id参数

**响应示例**:
```json
{
  "data": {
    "id": 1,
    "name": "示例名称",
    "created_at": "2024-01-01T00:00:00Z"
  }
}
```

---
### `GET /api/v1/customers/{id}/follow-ups`

**描述**: 获取列表（支持分页和筛选）

**Handler**: `ListCustomerFollowUps`

**路径参数**:
- `id` (integer): id参数

**查询参数**:
- `page` (integer): 页码，默认 1
- `page_size` (integer): 每页记录数，默认 20

**响应示例**:
```json
{
  "items": [
    {"id": 1, "name": "示例1"},
    {"id": 2, "name": "示例2"}
  ],
  "total": 100,
  "page": 1,
  "page_size": 20
}
```

---
### `POST /api/v1/customers/{id}/follow-ups`

**描述**: 创建新记录

**Handler**: `CreateCustomerFollowUp`

**路径参数**:
- `id` (integer): id参数

**请求体示例**:
```json
{
  "name": "示例名称",
  "description": "示例描述"
}
```

**响应示例**:
```json
{
  "data": {
    "message": "操作成功"
  }
}
```

---
### `POST /api/v1/customers/{id}/identities`

**描述**: 获取列表数据（支持分页）

**Handler**: `AddCustomerIdentity`

**路径参数**:
- `id` (integer): id参数

**请求体示例**:
```json
{
  "data": "请求数据"
}
```

**响应示例**:
```json
{
  "data": {
    "message": "操作成功"
  }
}
```

---
### `GET /api/v1/customers/{id}/interactions`

**描述**: 获取列表（支持分页和筛选）

**Handler**: `ListCustomerInteractions`

**路径参数**:
- `id` (integer): id参数

**查询参数**:
- `page` (integer): 页码，默认 1
- `page_size` (integer): 每页记录数，默认 20

**响应示例**:
```json
{
  "items": [
    {"id": 1, "name": "示例1"},
    {"id": 2, "name": "示例2"}
  ],
  "total": 100,
  "page": 1,
  "page_size": 20
}
```

---
### `POST /api/v1/customers/{id}/interactions`

**描述**: 创建新记录

**Handler**: `CreateCustomerInteraction`

**路径参数**:
- `id` (integer): id参数

**请求体示例**:
```json
{
  "name": "示例名称",
  "description": "示例描述"
}
```

**响应示例**:
```json
{
  "data": {
    "message": "操作成功"
  }
}
```

---
### `GET /api/v1/customers/{id}/membership`

**描述**: 获取详细信息

**Handler**: `GetCustomerMembership`

**路径参数**:
- `id` (integer): id参数

**响应示例**:
```json
{
  "data": {
    "id": 1,
    "name": "示例名称",
    "created_at": "2024-01-01T00:00:00Z"
  }
}
```

---
### `GET /api/v1/customers/{id}/momentum-history`

**描述**: 获取详细信息

**Handler**: `GetCustomerMomentumHistory`

**路径参数**:
- `id` (integer): id参数

**响应示例**:
```json
{
  "data": {
    "id": 1,
    "name": "示例名称",
    "created_at": "2024-01-01T00:00:00Z"
  }
}
```

---

## department

**端点数量**: 5

### `GET /api/v1/departments`

**描述**: 获取列表（支持分页和筛选）

**Handler**: `ListDepartments`

**查询参数**:
- `page` (integer): 页码，默认 1
- `page_size` (integer): 每页记录数，默认 20

**响应示例**:
```json
{
  "items": [
    {"id": 1, "name": "示例1"},
    {"id": 2, "name": "示例2"}
  ],
  "total": 100,
  "page": 1,
  "page_size": 20
}
```

---
### `POST /api/v1/departments`

**描述**: 创建新记录

**Handler**: `CreateDepartment`

**请求体示例**:
```json
{
  "name": "示例名称",
  "description": "示例描述"
}
```

**响应示例**:
```json
{
  "data": {
    "message": "操作成功"
  }
}
```

---
### `GET /api/v1/departments/{id}`

**描述**: 获取详细信息

**Handler**: `GetDepartment`

**路径参数**:
- `id` (integer): id参数

**响应示例**:
```json
{
  "data": {
    "id": 1,
    "name": "示例名称",
    "created_at": "2024-01-01T00:00:00Z"
  }
}
```

---
### `PUT /api/v1/departments/{id}`

**描述**: 更新记录信息

**Handler**: `UpdateDepartment`

**路径参数**:
- `id` (integer): id参数

**请求体示例**:
```json
{
  "name": "示例名称",
  "description": "示例描述"
}
```

**响应示例**:
```json
{
  "data": {
    "message": "操作成功"
  }
}
```

---
### `DELETE /api/v1/departments/{id}`

**描述**: 删除记录（软删除）

**Handler**: `DeleteDepartment`

**路径参数**:
- `id` (integer): id参数

**响应示例**:
```json
{
  "data": {
    "message": "操作成功"
  }
}
```

---

## employee

**端点数量**: 6

### `GET /api/v1/employees`

**描述**: 获取列表（支持分页和筛选）

**Handler**: `ListEmployees`

**查询参数**:
- `page` (integer): 页码，默认 1
- `page_size` (integer): 每页记录数，默认 20

**响应示例**:
```json
{
  "items": [
    {"id": 1, "name": "示例1"},
    {"id": 2, "name": "示例2"}
  ],
  "total": 100,
  "page": 1,
  "page_size": 20
}
```

---
### `POST /api/v1/employees`

**描述**: 创建新记录

**Handler**: `CreateEmployee`

**请求体示例**:
```json
{
  "name": "示例名称",
  "description": "示例描述"
}
```

**响应示例**:
```json
{
  "data": {
    "message": "操作成功"
  }
}
```

---
### `GET /api/v1/employees/{id}`

**描述**: 获取详细信息

**Handler**: `GetEmployee`

**路径参数**:
- `id` (integer): id参数

**响应示例**:
```json
{
  "data": {
    "id": 1,
    "name": "示例名称",
    "created_at": "2024-01-01T00:00:00Z"
  }
}
```

---
### `PUT /api/v1/employees/{id}`

**描述**: 更新记录信息

**Handler**: `UpdateEmployee`

**路径参数**:
- `id` (integer): id参数

**请求体示例**:
```json
{
  "name": "示例名称",
  "description": "示例描述"
}
```

**响应示例**:
```json
{
  "data": {
    "message": "操作成功"
  }
}
```

---
### `DELETE /api/v1/employees/{id}`

**描述**: 删除记录（软删除）

**Handler**: `DeleteEmployee`

**路径参数**:
- `id` (integer): id参数

**响应示例**:
```json
{
  "data": {
    "message": "操作成功"
  }
}
```

---
### `POST /api/v1/employees/{id}/reset-password`

**描述**: 执行 ResetPassword 操作

**Handler**: `ResetPassword`

**路径参数**:
- `id` (integer): id参数

**请求体示例**:
```json
{
  "data": "请求数据"
}
```

**响应示例**:
```json
{
  "data": {
    "message": "操作成功"
  }
}
```

---

## organization

**端点数量**: 30

### `GET /api/v1/departments/health`

**描述**: 执行 DepartmentHealthCheck 操作

**Handler**: `DepartmentHealthCheck`

**响应示例**:
```json
{
  "data": {
    "message": "操作成功"
  }
}
```

---
### `POST /api/v1/departments/sync-from-visits`

**描述**: 同步数据

**Handler**: `SyncDepartmentsFromVisits`

**请求体示例**:
```json
{
  "data": "请求数据"
}
```

**响应示例**:
```json
{
  "data": {
    "message": "操作成功"
  }
}
```

---
### `GET /api/v1/departments/{id}/performance`

**描述**: 获取详细信息

**Handler**: `GetDepartmentPerformance`

**路径参数**:
- `id` (integer): id参数

**响应示例**:
```json
{
  "data": {
    "id": 1,
    "name": "示例名称",
    "created_at": "2024-01-01T00:00:00Z"
  }
}
```

---
### `GET /api/v1/doctors`

**描述**: 获取列表（支持分页和筛选）

**Handler**: `ListDoctors`

**查询参数**:
- `page` (integer): 页码，默认 1
- `page_size` (integer): 每页记录数，默认 20

**响应示例**:
```json
{
  "items": [
    {"id": 1, "name": "示例1"},
    {"id": 2, "name": "示例2"}
  ],
  "total": 100,
  "page": 1,
  "page_size": 20
}
```

---
### `POST /api/v1/doctors`

**描述**: 创建新记录

**Handler**: `CreateDoctor`

**请求体示例**:
```json
{
  "name": "示例名称",
  "description": "示例描述"
}
```

**响应示例**:
```json
{
  "data": {
    "message": "操作成功"
  }
}
```

---
### `GET /api/v1/doctors/health`

**描述**: 执行 DoctorHealthCheck 操作

**Handler**: `DoctorHealthCheck`

**响应示例**:
```json
{
  "data": {
    "message": "操作成功"
  }
}
```

---
### `GET /api/v1/doctors/performance/summary`

**描述**: 获取详细信息

**Handler**: `GetDoctorPerformanceSummary`

**响应示例**:
```json
{
  "data": {
    "id": 1,
    "name": "示例名称",
    "created_at": "2024-01-01T00:00:00Z"
  }
}
```

---
### `POST /api/v1/doctors/sync-from-visits`

**描述**: 同步数据

**Handler**: `SyncDoctorsFromVisits`

**请求体示例**:
```json
{
  "data": "请求数据"
}
```

**响应示例**:
```json
{
  "data": {
    "message": "操作成功"
  }
}
```

---
### `GET /api/v1/doctors/{id}`

**描述**: 获取详细信息

**Handler**: `GetDoctor`

**路径参数**:
- `id` (integer): id参数

**响应示例**:
```json
{
  "data": {
    "id": 1,
    "name": "示例名称",
    "created_at": "2024-01-01T00:00:00Z"
  }
}
```

---
### `PUT /api/v1/doctors/{id}`

**描述**: 更新记录信息

**Handler**: `UpdateDoctor`

**路径参数**:
- `id` (integer): id参数

**请求体示例**:
```json
{
  "name": "示例名称",
  "description": "示例描述"
}
```

**响应示例**:
```json
{
  "data": {
    "message": "操作成功"
  }
}
```

---
### `DELETE /api/v1/doctors/{id}`

**描述**: 删除记录（软删除）

**Handler**: `DeleteDoctor`

**路径参数**:
- `id` (integer): id参数

**响应示例**:
```json
{
  "data": {
    "message": "操作成功"
  }
}
```

---
### `GET /api/v1/doctors/{id}/employees`

**描述**: 获取详细信息

**Handler**: `GetDoctorEmployees`

**路径参数**:
- `id` (integer): id参数

**响应示例**:
```json
{
  "data": {
    "id": 1,
    "name": "示例名称",
    "created_at": "2024-01-01T00:00:00Z"
  }
}
```

---
### `POST /api/v1/doctors/{id}/employees`

**描述**: 更新记录信息

**Handler**: `UpdateDoctorEmployees`

**路径参数**:
- `id` (integer): id参数

**请求体示例**:
```json
{
  "name": "示例名称",
  "description": "示例描述"
}
```

**响应示例**:
```json
{
  "data": {
    "message": "操作成功"
  }
}
```

---
### `GET /api/v1/doctors/{id}/performance`

**描述**: 获取详细信息

**Handler**: `GetDoctorPerformance`

**路径参数**:
- `id` (integer): id参数

**响应示例**:
```json
{
  "data": {
    "id": 1,
    "name": "示例名称",
    "created_at": "2024-01-01T00:00:00Z"
  }
}
```

---
### `GET /api/v1/institutions/statistics`

**描述**: 获取详细信息

**Handler**: `GetInstitutionStatistics`

**响应示例**:
```json
{
  "data": {
    "id": 1,
    "name": "示例名称",
    "created_at": "2024-01-01T00:00:00Z"
  }
}
```

---
### `GET /api/v1/organization/employees/{id}/assistants`

**描述**: 获取详细信息

**Handler**: `GetEmployeeAssistants`

**路径参数**:
- `id` (integer): id参数

**响应示例**:
```json
{
  "data": {
    "id": 1,
    "name": "示例名称",
    "created_at": "2024-01-01T00:00:00Z"
  }
}
```

---
### `PUT /api/v1/organization/employees/{id}/assistants`

**描述**: 更新记录信息

**Handler**: `UpdateEmployeeAssistants`

**路径参数**:
- `id` (integer): id参数

**请求体示例**:
```json
{
  "name": "示例名称",
  "description": "示例描述"
}
```

**响应示例**:
```json
{
  "data": {
    "message": "操作成功"
  }
}
```

---
### `GET /api/v1/organization/medical-specialties`

**描述**: 获取列表（支持分页和筛选）

**Handler**: `ListMedicalSpecialties`

**查询参数**:
- `page` (integer): 页码，默认 1
- `page_size` (integer): 每页记录数，默认 20

**响应示例**:
```json
{
  "items": [
    {"id": 1, "name": "示例1"},
    {"id": 2, "name": "示例2"}
  ],
  "total": 100,
  "page": 1,
  "page_size": 20
}
```

---
### `GET /api/v1/patients`

**描述**: 获取列表（支持分页和筛选）

**Handler**: `ListPatients`

**查询参数**:
- `page` (integer): 页码，默认 1
- `page_size` (integer): 每页记录数，默认 20

**响应示例**:
```json
{
  "items": [
    {"id": 1, "name": "示例1"},
    {"id": 2, "name": "示例2"}
  ],
  "total": 100,
  "page": 1,
  "page_size": 20
}
```

---
### `POST /api/v1/patients`

**描述**: 创建新记录

**Handler**: `CreatePatient`

**请求体示例**:
```json
{
  "name": "示例名称",
  "description": "示例描述"
}
```

**响应示例**:
```json
{
  "data": {
    "message": "操作成功"
  }
}
```

---
### `POST /api/v1/patients/sync-from-visits`

**描述**: 同步数据

**Handler**: `SyncPatientsFromVisits`

**请求体示例**:
```json
{
  "data": "请求数据"
}
```

**响应示例**:
```json
{
  "data": {
    "message": "操作成功"
  }
}
```

---
### `GET /api/v1/patients/{id}`

**描述**: 获取详细信息

**Handler**: `GetPatient`

**路径参数**:
- `id` (integer): id参数

**响应示例**:
```json
{
  "data": {
    "id": 1,
    "name": "示例名称",
    "created_at": "2024-01-01T00:00:00Z"
  }
}
```

---
### `PUT /api/v1/patients/{id}`

**描述**: 更新记录信息

**Handler**: `UpdatePatient`

**路径参数**:
- `id` (integer): id参数

**请求体示例**:
```json
{
  "name": "示例名称",
  "description": "示例描述"
}
```

**响应示例**:
```json
{
  "data": {
    "message": "操作成功"
  }
}
```

---
### `DELETE /api/v1/patients/{id}`

**描述**: 删除记录（软删除）

**Handler**: `DeletePatient`

**路径参数**:
- `id` (integer): id参数

**响应示例**:
```json
{
  "data": {
    "message": "操作成功"
  }
}
```

---
### `GET /api/v1/patients/{id}/360`

**描述**: 获取详细信息

**Handler**: `GetPatient360View`

**路径参数**:
- `id` (integer): id参数

**响应示例**:
```json
{
  "data": {
    "id": 1,
    "name": "示例名称",
    "created_at": "2024-01-01T00:00:00Z"
  }
}
```

---
### `GET /api/v1/tenants`

**描述**: 获取列表（支持分页和筛选）

**Handler**: `ListTenants`

**查询参数**:
- `page` (integer): 页码，默认 1
- `page_size` (integer): 每页记录数，默认 20

**响应示例**:
```json
{
  "items": [
    {"id": 1, "name": "示例1"},
    {"id": 2, "name": "示例2"}
  ],
  "total": 100,
  "page": 1,
  "page_size": 20
}
```

---
### `POST /api/v1/tenants`

**描述**: 创建新记录

**Handler**: `CreateTenant`

**请求体示例**:
```json
{
  "name": "示例名称",
  "description": "示例描述"
}
```

**响应示例**:
```json
{
  "data": {
    "message": "操作成功"
  }
}
```

---
### `GET /api/v1/tenants/{id}`

**描述**: 获取详细信息

**Handler**: `GetTenant`

**路径参数**:
- `id` (integer): id参数

**响应示例**:
```json
{
  "data": {
    "id": 1,
    "name": "示例名称",
    "created_at": "2024-01-01T00:00:00Z"
  }
}
```

---
### `PUT /api/v1/tenants/{id}`

**描述**: 更新记录信息

**Handler**: `UpdateTenant`

**路径参数**:
- `id` (integer): id参数

**请求体示例**:
```json
{
  "name": "示例名称",
  "description": "示例描述"
}
```

**响应示例**:
```json
{
  "data": {
    "message": "操作成功"
  }
}
```

---
### `DELETE /api/v1/tenants/{id}`

**描述**: 删除记录（软删除）

**Handler**: `DeleteTenant`

**路径参数**:
- `id` (integer): id参数

**响应示例**:
```json
{
  "data": {
    "message": "操作成功"
  }
}
```

---

## rbac

**端点数量**: 39

### `GET /api/v1/institution/rbac/employees/{id}/role`

**描述**: 获取详细信息

**Handler**: `GetEmployeeRole`

**路径参数**:
- `id` (integer): id参数

**响应示例**:
```json
{
  "data": {
    "id": 1,
    "name": "示例名称",
    "created_at": "2024-01-01T00:00:00Z"
  }
}
```

---
### `PUT /api/v1/institution/rbac/employees/{id}/role`

**描述**: 执行 SetEmployeeRole 操作

**Handler**: `SetEmployeeRole`

**路径参数**:
- `id` (integer): id参数

**请求体示例**:
```json
{
  "data": "请求数据"
}
```

**响应示例**:
```json
{
  "data": {
    "message": "操作成功"
  }
}
```

---
### `DELETE /api/v1/institution/rbac/employees/{id}/role`

**描述**: 执行 RemoveEmployeeRole 操作

**Handler**: `RemoveEmployeeRole`

**路径参数**:
- `id` (integer): id参数

**响应示例**:
```json
{
  "data": {
    "message": "操作成功"
  }
}
```

---
### `GET /api/v1/institution/rbac/menus`

**描述**: 获取列表（支持分页和筛选）

**Handler**: `ListInstitutionMenus`

**查询参数**:
- `page` (integer): 页码，默认 1
- `page_size` (integer): 每页记录数，默认 20

**响应示例**:
```json
{
  "items": [
    {"id": 1, "name": "示例1"},
    {"id": 2, "name": "示例2"}
  ],
  "total": 100,
  "page": 1,
  "page_size": 20
}
```

---
### `POST /api/v1/institution/rbac/menus`

**描述**: 创建新记录

**Handler**: `CreateInstitutionMenu`

**请求体示例**:
```json
{
  "name": "示例名称",
  "description": "示例描述"
}
```

**响应示例**:
```json
{
  "data": {
    "message": "操作成功"
  }
}
```

---
### `GET /api/v1/institution/rbac/menus/{id}`

**描述**: 获取详细信息

**Handler**: `GetInstitutionMenu`

**路径参数**:
- `id` (integer): id参数

**响应示例**:
```json
{
  "data": {
    "id": 1,
    "name": "示例名称",
    "created_at": "2024-01-01T00:00:00Z"
  }
}
```

---
### `PUT /api/v1/institution/rbac/menus/{id}`

**描述**: 更新记录信息

**Handler**: `UpdateInstitutionMenu`

**路径参数**:
- `id` (integer): id参数

**请求体示例**:
```json
{
  "name": "示例名称",
  "description": "示例描述"
}
```

**响应示例**:
```json
{
  "data": {
    "message": "操作成功"
  }
}
```

---
### `DELETE /api/v1/institution/rbac/menus/{id}`

**描述**: 删除记录（软删除）

**Handler**: `DeleteInstitutionMenu`

**路径参数**:
- `id` (integer): id参数

**响应示例**:
```json
{
  "data": {
    "message": "操作成功"
  }
}
```

---
### `GET /api/v1/institution/rbac/roles`

**描述**: 获取列表（支持分页和筛选）

**Handler**: `ListInstitutionRoles`

**查询参数**:
- `page` (integer): 页码，默认 1
- `page_size` (integer): 每页记录数，默认 20

**响应示例**:
```json
{
  "items": [
    {"id": 1, "name": "示例1"},
    {"id": 2, "name": "示例2"}
  ],
  "total": 100,
  "page": 1,
  "page_size": 20
}
```

---
### `POST /api/v1/institution/rbac/roles`

**描述**: 创建新记录

**Handler**: `CreateInstitutionRole`

**请求体示例**:
```json
{
  "name": "示例名称",
  "description": "示例描述"
}
```

**响应示例**:
```json
{
  "data": {
    "message": "操作成功"
  }
}
```

---
### `GET /api/v1/institution/rbac/roles/{id}`

**描述**: 获取详细信息

**Handler**: `GetInstitutionRole`

**路径参数**:
- `id` (integer): id参数

**响应示例**:
```json
{
  "data": {
    "id": 1,
    "name": "示例名称",
    "created_at": "2024-01-01T00:00:00Z"
  }
}
```

---
### `PUT /api/v1/institution/rbac/roles/{id}`

**描述**: 更新记录信息

**Handler**: `UpdateInstitutionRole`

**路径参数**:
- `id` (integer): id参数

**请求体示例**:
```json
{
  "name": "示例名称",
  "description": "示例描述"
}
```

**响应示例**:
```json
{
  "data": {
    "message": "操作成功"
  }
}
```

---
### `DELETE /api/v1/institution/rbac/roles/{id}`

**描述**: 删除记录（软删除）

**Handler**: `DeleteInstitutionRole`

**路径参数**:
- `id` (integer): id参数

**响应示例**:
```json
{
  "data": {
    "message": "操作成功"
  }
}
```

---
### `POST /api/v1/institution/rbac/roles/{id}/permissions`

**描述**: 获取列表数据（支持分页）

**Handler**: `AssignPermissionsToInstitutionRole`

**路径参数**:
- `id` (integer): id参数

**请求体示例**:
```json
{
  "data": "请求数据"
}
```

**响应示例**:
```json
{
  "data": {
    "message": "操作成功"
  }
}
```

---
### `DELETE /api/v1/institution/rbac/roles/{id}/permissions`

**描述**: 获取列表数据（支持分页）

**Handler**: `RemovePermissionsFromInstitutionRole`

**路径参数**:
- `id` (integer): id参数

**响应示例**:
```json
{
  "data": {
    "message": "操作成功"
  }
}
```

---
### `GET /api/v1/institution/rbac/roles/{id}/permissions`

**描述**: 获取详细信息

**Handler**: `GetInstitutionRolePermissions`

**路径参数**:
- `id` (integer): id参数

**响应示例**:
```json
{
  "data": {
    "id": 1,
    "name": "示例名称",
    "created_at": "2024-01-01T00:00:00Z"
  }
}
```

---
### `GET /api/v1/rbac/admins`

**描述**: 获取列表（支持分页和筛选）

**Handler**: `ListOperationsAdmins`

**查询参数**:
- `page` (integer): 页码，默认 1
- `page_size` (integer): 每页记录数，默认 20

**响应示例**:
```json
{
  "items": [
    {"id": 1, "name": "示例1"},
    {"id": 2, "name": "示例2"}
  ],
  "total": 100,
  "page": 1,
  "page_size": 20
}
```

---
### `POST /api/v1/rbac/admins`

**描述**: 创建新记录

**Handler**: `CreateOperationsAdmin`

**请求体示例**:
```json
{
  "name": "示例名称",
  "description": "示例描述"
}
```

**响应示例**:
```json
{
  "data": {
    "message": "操作成功"
  }
}
```

---
### `GET /api/v1/rbac/admins/{id}`

**描述**: 获取详细信息

**Handler**: `GetOperationsAdmin`

**路径参数**:
- `id` (integer): id参数

**响应示例**:
```json
{
  "data": {
    "id": 1,
    "name": "示例名称",
    "created_at": "2024-01-01T00:00:00Z"
  }
}
```

---
### `PUT /api/v1/rbac/admins/{id}`

**描述**: 更新记录信息

**Handler**: `UpdateOperationsAdmin`

**路径参数**:
- `id` (integer): id参数

**请求体示例**:
```json
{
  "name": "示例名称",
  "description": "示例描述"
}
```

**响应示例**:
```json
{
  "data": {
    "message": "操作成功"
  }
}
```

---
### `DELETE /api/v1/rbac/admins/{id}`

**描述**: 删除记录（软删除）

**Handler**: `DeleteOperationsAdmin`

**路径参数**:
- `id` (integer): id参数

**响应示例**:
```json
{
  "data": {
    "message": "操作成功"
  }
}
```

---
### `PUT /api/v1/rbac/admins/{id}/reset-password`

**描述**: 执行 ResetAdminPassword 操作

**Handler**: `ResetAdminPassword`

**路径参数**:
- `id` (integer): id参数

**请求体示例**:
```json
{
  "data": "请求数据"
}
```

**响应示例**:
```json
{
  "data": {
    "message": "操作成功"
  }
}
```

---
### `GET /api/v1/rbac/menus`

**描述**: 获取列表（支持分页和筛选）

**Handler**: `ListOperationsMenus`

**查询参数**:
- `page` (integer): 页码，默认 1
- `page_size` (integer): 每页记录数，默认 20

**响应示例**:
```json
{
  "items": [
    {"id": 1, "name": "示例1"},
    {"id": 2, "name": "示例2"}
  ],
  "total": 100,
  "page": 1,
  "page_size": 20
}
```

---
### `POST /api/v1/rbac/menus`

**描述**: 创建新记录

**Handler**: `CreateOperationsMenu`

**请求体示例**:
```json
{
  "name": "示例名称",
  "description": "示例描述"
}
```

**响应示例**:
```json
{
  "data": {
    "message": "操作成功"
  }
}
```

---
### `PUT /api/v1/rbac/menus/sort`

**描述**: 更新记录信息

**Handler**: `UpdateMenuSort`

**请求体示例**:
```json
{
  "name": "示例名称",
  "description": "示例描述"
}
```

**响应示例**:
```json
{
  "data": {
    "message": "操作成功"
  }
}
```

---
### `GET /api/v1/rbac/menus/tree`

**描述**: 获取详细信息

**Handler**: `GetOperationsMenuTree`

**响应示例**:
```json
{
  "data": {
    "id": 1,
    "name": "示例名称",
    "created_at": "2024-01-01T00:00:00Z"
  }
}
```

---
### `GET /api/v1/rbac/menus/{id}`

**描述**: 获取详细信息

**Handler**: `GetOperationsMenu`

**路径参数**:
- `id` (integer): id参数

**响应示例**:
```json
{
  "data": {
    "id": 1,
    "name": "示例名称",
    "created_at": "2024-01-01T00:00:00Z"
  }
}
```

---
### `PUT /api/v1/rbac/menus/{id}`

**描述**: 更新记录信息

**Handler**: `UpdateOperationsMenu`

**路径参数**:
- `id` (integer): id参数

**请求体示例**:
```json
{
  "name": "示例名称",
  "description": "示例描述"
}
```

**响应示例**:
```json
{
  "data": {
    "message": "操作成功"
  }
}
```

---
### `DELETE /api/v1/rbac/menus/{id}`

**描述**: 删除记录（软删除）

**Handler**: `DeleteOperationsMenu`

**路径参数**:
- `id` (integer): id参数

**响应示例**:
```json
{
  "data": {
    "message": "操作成功"
  }
}
```

---
### `GET /api/v1/rbac/roles`

**描述**: 获取列表（支持分页和筛选）

**Handler**: `ListOperationsRoles`

**查询参数**:
- `page` (integer): 页码，默认 1
- `page_size` (integer): 每页记录数，默认 20

**响应示例**:
```json
{
  "items": [
    {"id": 1, "name": "示例1"},
    {"id": 2, "name": "示例2"}
  ],
  "total": 100,
  "page": 1,
  "page_size": 20
}
```

---
### `POST /api/v1/rbac/roles`

**描述**: 创建新记录

**Handler**: `CreateOperationsRole`

**请求体示例**:
```json
{
  "name": "示例名称",
  "description": "示例描述"
}
```

**响应示例**:
```json
{
  "data": {
    "message": "操作成功"
  }
}
```

---
### `GET /api/v1/rbac/roles/{id}`

**描述**: 获取详细信息

**Handler**: `GetOperationsRole`

**路径参数**:
- `id` (integer): id参数

**响应示例**:
```json
{
  "data": {
    "id": 1,
    "name": "示例名称",
    "created_at": "2024-01-01T00:00:00Z"
  }
}
```

---
### `PUT /api/v1/rbac/roles/{id}`

**描述**: 更新记录信息

**Handler**: `UpdateOperationsRole`

**路径参数**:
- `id` (integer): id参数

**请求体示例**:
```json
{
  "name": "示例名称",
  "description": "示例描述"
}
```

**响应示例**:
```json
{
  "data": {
    "message": "操作成功"
  }
}
```

---
### `DELETE /api/v1/rbac/roles/{id}`

**描述**: 删除记录（软删除）

**Handler**: `DeleteOperationsRole`

**路径参数**:
- `id` (integer): id参数

**响应示例**:
```json
{
  "data": {
    "message": "操作成功"
  }
}
```

---
### `GET /api/v1/rbac/roles/{id}/menus`

**描述**: 获取详细信息

**Handler**: `GetRoleMenus`

**路径参数**:
- `id` (integer): id参数

**响应示例**:
```json
{
  "data": {
    "id": 1,
    "name": "示例名称",
    "created_at": "2024-01-01T00:00:00Z"
  }
}
```

---
### `PUT /api/v1/rbac/roles/{id}/menus`

**描述**: 获取列表数据（支持分页）

**Handler**: `AssignMenusToRole`

**路径参数**:
- `id` (integer): id参数

**请求体示例**:
```json
{
  "data": "请求数据"
}
```

**响应示例**:
```json
{
  "data": {
    "message": "操作成功"
  }
}
```

---
### `POST /api/v1/rbac/roles/{id}/permissions`

**描述**: 获取列表数据（支持分页）

**Handler**: `AssignPermissionsToRole`

**路径参数**:
- `id` (integer): id参数

**请求体示例**:
```json
{
  "data": "请求数据"
}
```

**响应示例**:
```json
{
  "data": {
    "message": "操作成功"
  }
}
```

---
### `DELETE /api/v1/rbac/roles/{id}/permissions`

**描述**: 获取列表数据（支持分页）

**Handler**: `RemovePermissionsFromRole`

**路径参数**:
- `id` (integer): id参数

**响应示例**:
```json
{
  "data": {
    "message": "操作成功"
  }
}
```

---
### `GET /api/v1/rbac/roles/{id}/permissions`

**描述**: 获取详细信息

**Handler**: `GetRolePermissions`

**路径参数**:
- `id` (integer): id参数

**响应示例**:
```json
{
  "data": {
    "id": 1,
    "name": "示例名称",
    "created_at": "2024-01-01T00:00:00Z"
  }
}
```

---

## recording

**端点数量**: 78

### `GET /api/v1/medical-recordings`

**描述**: 获取列表（支持分页和筛选）

**Handler**: `ListMedicalRecordings`

**查询参数**:
- `page` (integer): 页码，默认 1
- `page_size` (integer): 每页记录数，默认 20

**响应示例**:
```json
{
  "items": [
    {"id": 1, "name": "示例1"},
    {"id": 2, "name": "示例2"}
  ],
  "total": 100,
  "page": 1,
  "page_size": 20
}
```

---
### `GET /api/v1/medical-recordings/analysis`

**描述**: 获取详细信息

**Handler**: `GetCommunicationAnalysis`

**响应示例**:
```json
{
  "data": {
    "id": 1,
    "name": "示例名称",
    "created_at": "2024-01-01T00:00:00Z"
  }
}
```

---
### `GET /api/v1/medical-recordings/best-practices`

**描述**: 获取列表（支持分页和筛选）

**Handler**: `ListBestPractices`

**查询参数**:
- `page` (integer): 页码，默认 1
- `page_size` (integer): 每页记录数，默认 20

**响应示例**:
```json
{
  "items": [
    {"id": 1, "name": "示例1"},
    {"id": 2, "name": "示例2"}
  ],
  "total": 100,
  "page": 1,
  "page_size": 20
}
```

---
### `GET /api/v1/medical-recordings/doctor-ability`

**描述**: 获取详细信息

**Handler**: `GetDoctorAbilityRanking`

**响应示例**:
```json
{
  "data": {
    "id": 1,
    "name": "示例名称",
    "created_at": "2024-01-01T00:00:00Z"
  }
}
```

---
### `GET /api/v1/medical-recordings/doctor-ability/{employee_id}`

**描述**: 获取详细信息

**Handler**: `GetDoctorAbilityDetail`

**路径参数**:
- `employee_id` (integer): employee_id参数

**响应示例**:
```json
{
  "data": {
    "id": 1,
    "name": "示例名称",
    "created_at": "2024-01-01T00:00:00Z"
  }
}
```

---
### `GET /api/v1/medical-recordings/followup-generation-mode`

**描述**: 获取详细信息

**Handler**: `GetFollowUpGenerationMode`

**响应示例**:
```json
{
  "data": {
    "id": 1,
    "name": "示例名称",
    "created_at": "2024-01-01T00:00:00Z"
  }
}
```

---
### `POST /api/v1/medical-recordings/followup-generation-mode`

**描述**: 更新记录信息

**Handler**: `UpdateFollowUpGenerationMode`

**请求体示例**:
```json
{
  "name": "示例名称",
  "description": "示例描述"
}
```

**响应示例**:
```json
{
  "data": {
    "message": "操作成功"
  }
}
```

---
### `GET /api/v1/medical-recordings/institution-rule-configs`

**描述**: 获取列表（支持分页和筛选）

**Handler**: `ListInstitutionRuleConfigs`

**查询参数**:
- `page` (integer): 页码，默认 1
- `page_size` (integer): 每页记录数，默认 20

**响应示例**:
```json
{
  "items": [
    {"id": 1, "name": "示例1"},
    {"id": 2, "name": "示例2"}
  ],
  "total": 100,
  "page": 1,
  "page_size": 20
}
```

---
### `POST /api/v1/medical-recordings/institution-rule-configs`

**描述**: 创建新记录

**Handler**: `CreateInstitutionRuleConfig`

**请求体示例**:
```json
{
  "name": "示例名称",
  "description": "示例描述"
}
```

**响应示例**:
```json
{
  "data": {
    "message": "操作成功"
  }
}
```

---
### `PUT /api/v1/medical-recordings/institution-rule-configs/{id}`

**描述**: 更新记录信息

**Handler**: `UpdateInstitutionRuleConfig`

**路径参数**:
- `id` (integer): id参数

**请求体示例**:
```json
{
  "name": "示例名称",
  "description": "示例描述"
}
```

**响应示例**:
```json
{
  "data": {
    "message": "操作成功"
  }
}
```

---
### `GET /api/v1/medical-recordings/quality-control`

**描述**: 获取详细信息

**Handler**: `GetQualityControlDashboard`

**响应示例**:
```json
{
  "data": {
    "id": 1,
    "name": "示例名称",
    "created_at": "2024-01-01T00:00:00Z"
  }
}
```

---
### `GET /api/v1/medical-recordings/recording-analysis/settings`

**描述**: 获取详细信息

**Handler**: `GetAnalysisSettings`

**响应示例**:
```json
{
  "data": {
    "id": 1,
    "name": "示例名称",
    "created_at": "2024-01-01T00:00:00Z"
  }
}
```

---
### `GET /api/v1/medical-recordings/team-trends`

**描述**: 获取详细信息

**Handler**: `GetTeamTrends`

**响应示例**:
```json
{
  "data": {
    "id": 1,
    "name": "示例名称",
    "created_at": "2024-01-01T00:00:00Z"
  }
}
```

---
### `GET /api/v1/medical-recordings/weekly-meeting`

**描述**: 获取详细信息

**Handler**: `GetWeeklyMeetingMaterial`

**响应示例**:
```json
{
  "data": {
    "id": 1,
    "name": "示例名称",
    "created_at": "2024-01-01T00:00:00Z"
  }
}
```

---
### `GET /api/v1/medical-recordings/weekly-summary`

**描述**: 获取详细信息

**Handler**: `GetWeeklySummary`

**响应示例**:
```json
{
  "data": {
    "id": 1,
    "name": "示例名称",
    "created_at": "2024-01-01T00:00:00Z"
  }
}
```

---
### `POST /api/v1/medical-recordings/{id}/best-practice`

**描述**: 执行 AddBestPractice 操作

**Handler**: `AddBestPractice`

**路径参数**:
- `id` (integer): id参数

**请求体示例**:
```json
{
  "data": "请求数据"
}
```

**响应示例**:
```json
{
  "data": {
    "message": "操作成功"
  }
}
```

---
### `DELETE /api/v1/medical-recordings/{id}/best-practice`

**描述**: 删除记录（软删除）

**Handler**: `DeleteBestPractice`

**路径参数**:
- `id` (integer): id参数

**响应示例**:
```json
{
  "data": {
    "message": "操作成功"
  }
}
```

---
### `POST /api/v1/medical-recordings/{id}/follow-up-tasks/confirm`

**描述**: 执行 ConfirmFollowUpTasks 操作

**Handler**: `ConfirmFollowUpTasks`

**路径参数**:
- `id` (integer): id参数

**请求体示例**:
```json
{
  "data": "请求数据"
}
```

**响应示例**:
```json
{
  "data": {
    "message": "操作成功"
  }
}
```

---
### `POST /api/v1/medical-recordings/{id}/mark-highlight`

**描述**: 执行 MarkHighlight 操作

**Handler**: `MarkHighlight`

**路径参数**:
- `id` (integer): id参数

**请求体示例**:
```json
{
  "data": "请求数据"
}
```

**响应示例**:
```json
{
  "data": {
    "message": "操作成功"
  }
}
```

---
### `GET /api/v1/recording-prompts`

**描述**: 获取列表（支持分页和筛选）

**Handler**: `ListRecordingPrompts`

**查询参数**:
- `page` (integer): 页码，默认 1
- `page_size` (integer): 每页记录数，默认 20

**响应示例**:
```json
{
  "items": [
    {"id": 1, "name": "示例1"},
    {"id": 2, "name": "示例2"}
  ],
  "total": 100,
  "page": 1,
  "page_size": 20
}
```

---
### `POST /api/v1/recording-prompts`

**描述**: 创建新记录

**Handler**: `CreateRecordingPrompt`

**请求体示例**:
```json
{
  "name": "示例名称",
  "description": "示例描述"
}
```

**响应示例**:
```json
{
  "data": {
    "message": "操作成功"
  }
}
```

---
### `GET /api/v1/recording-prompts/codes/{code}`

**描述**: 获取详细信息

**Handler**: `GetRecordingPrompt`

**路径参数**:
- `code` (string): code参数

**响应示例**:
```json
{
  "data": {
    "id": 1,
    "name": "示例名称",
    "created_at": "2024-01-01T00:00:00Z"
  }
}
```

---
### `PUT /api/v1/recording-prompts/codes/{code}`

**描述**: 更新记录信息

**Handler**: `UpdateRecordingPrompt`

**路径参数**:
- `code` (string): code参数

**请求体示例**:
```json
{
  "name": "示例名称",
  "description": "示例描述"
}
```

**响应示例**:
```json
{
  "data": {
    "message": "操作成功"
  }
}
```

---
### `DELETE /api/v1/recording-prompts/codes/{code}`

**描述**: 删除记录（软删除）

**Handler**: `DeleteRecordingPrompt`

**路径参数**:
- `code` (string): code参数

**响应示例**:
```json
{
  "data": {
    "message": "操作成功"
  }
}
```

---
### `POST /api/v1/recording-prompts/codes/{code}/test`

**描述**: 执行 TestRecordingPrompt 操作

**Handler**: `TestRecordingPrompt`

**路径参数**:
- `code` (string): code参数

**请求体示例**:
```json
{
  "data": "请求数据"
}
```

**响应示例**:
```json
{
  "data": {
    "message": "操作成功"
  }
}
```

---
### `GET /api/v1/recording-prompts/tenant-configs`

**描述**: 获取列表（支持分页和筛选）

**Handler**: `ListTenantPromptConfigs`

**查询参数**:
- `page` (integer): 页码，默认 1
- `page_size` (integer): 每页记录数，默认 20

**响应示例**:
```json
{
  "items": [
    {"id": 1, "name": "示例1"},
    {"id": 2, "name": "示例2"}
  ],
  "total": 100,
  "page": 1,
  "page_size": 20
}
```

---
### `POST /api/v1/recording-prompts/tenant-configs`

**描述**: 创建新记录

**Handler**: `CreateTenantPromptConfig`

**请求体示例**:
```json
{
  "name": "示例名称",
  "description": "示例描述"
}
```

**响应示例**:
```json
{
  "data": {
    "message": "操作成功"
  }
}
```

---
### `PUT /api/v1/recording-prompts/tenant-configs/{id}`

**描述**: 更新记录信息

**Handler**: `UpdateTenantPromptConfig`

**路径参数**:
- `id` (integer): id参数

**请求体示例**:
```json
{
  "name": "示例名称",
  "description": "示例描述"
}
```

**响应示例**:
```json
{
  "data": {
    "message": "操作成功"
  }
}
```

---
### `DELETE /api/v1/recording-prompts/tenant-configs/{id}`

**描述**: 删除记录（软删除）

**Handler**: `DeleteTenantPromptConfig`

**路径参数**:
- `id` (integer): id参数

**响应示例**:
```json
{
  "data": {
    "message": "操作成功"
  }
}
```

---
### `GET /api/v1/recording-tasks`

**描述**: 获取列表（支持分页和筛选）

**Handler**: `ListRecordingTasks`

**查询参数**:
- `page` (integer): 页码，默认 1
- `page_size` (integer): 每页记录数，默认 20

**响应示例**:
```json
{
  "items": [
    {"id": 1, "name": "示例1"},
    {"id": 2, "name": "示例2"}
  ],
  "total": 100,
  "page": 1,
  "page_size": 20
}
```

---
### `POST /api/v1/recording-tasks/assign`

**描述**: 执行 BatchAssignTasks 操作

**Handler**: `BatchAssignTasks`

**请求体示例**:
```json
{
  "data": "请求数据"
}
```

**响应示例**:
```json
{
  "data": {
    "message": "操作成功"
  }
}
```

---
### `GET /api/v1/recording-tasks/daily-briefing`

**描述**: 获取详细信息

**Handler**: `GetDailyBriefing`

**响应示例**:
```json
{
  "data": {
    "id": 1,
    "name": "示例名称",
    "created_at": "2024-01-01T00:00:00Z"
  }
}
```

---
### `GET /api/v1/recording-tasks/employee-partnerships`

**描述**: 获取列表（支持分页和筛选）

**Handler**: `ListEmployeePartnerships`

**查询参数**:
- `page` (integer): 页码，默认 1
- `page_size` (integer): 每页记录数，默认 20

**响应示例**:
```json
{
  "items": [
    {"id": 1, "name": "示例1"},
    {"id": 2, "name": "示例2"}
  ],
  "total": 100,
  "page": 1,
  "page_size": 20
}
```

---
### `POST /api/v1/recording-tasks/employee-partnerships`

**描述**: 创建新记录

**Handler**: `CreateEmployeePartnership`

**请求体示例**:
```json
{
  "name": "示例名称",
  "description": "示例描述"
}
```

**响应示例**:
```json
{
  "data": {
    "message": "操作成功"
  }
}
```

---
### `DELETE /api/v1/recording-tasks/employee-partnerships/{id}`

**描述**: 删除记录（软删除）

**Handler**: `DeleteEmployeePartnership`

**路径参数**:
- `id` (integer): id参数

**响应示例**:
```json
{
  "data": {
    "message": "操作成功"
  }
}
```

---
### `GET /api/v1/recording-tasks/employees`

**描述**: 获取列表（支持分页和筛选）

**Handler**: `ListTaskEmployees`

**查询参数**:
- `page` (integer): 页码，默认 1
- `page_size` (integer): 每页记录数，默认 20

**响应示例**:
```json
{
  "items": [
    {"id": 1, "name": "示例1"},
    {"id": 2, "name": "示例2"}
  ],
  "total": 100,
  "page": 1,
  "page_size": 20
}
```

---
### `GET /api/v1/recording-tasks/my-tasks`

**描述**: 获取详细信息

**Handler**: `GetMyTasks`

**响应示例**:
```json
{
  "data": {
    "id": 1,
    "name": "示例名称",
    "created_at": "2024-01-01T00:00:00Z"
  }
}
```

---
### `GET /api/v1/recording-tasks/recordings/{id}/tasks`

**描述**: 获取详细信息

**Handler**: `GetRecordingTasksByRecordingID`

**路径参数**:
- `id` (integer): id参数

**响应示例**:
```json
{
  "data": {
    "id": 1,
    "name": "示例名称",
    "created_at": "2024-01-01T00:00:00Z"
  }
}
```

---
### `GET /api/v1/recording-tasks/stats`

**描述**: 获取详细信息

**Handler**: `GetTaskStats`

**响应示例**:
```json
{
  "data": {
    "id": 1,
    "name": "示例名称",
    "created_at": "2024-01-01T00:00:00Z"
  }
}
```

---
### `GET /api/v1/recording-tasks/{id}`

**描述**: 获取详细信息

**Handler**: `GetTask`

**路径参数**:
- `id` (integer): id参数

**响应示例**:
```json
{
  "data": {
    "id": 1,
    "name": "示例名称",
    "created_at": "2024-01-01T00:00:00Z"
  }
}
```

---
### `POST /api/v1/recording-tasks/{id}/cancel`

**描述**: 执行 CancelTask 操作

**Handler**: `CancelTask`

**路径参数**:
- `id` (integer): id参数

**请求体示例**:
```json
{
  "data": "请求数据"
}
```

**响应示例**:
```json
{
  "data": {
    "message": "操作成功"
  }
}
```

---
### `POST /api/v1/recording-tasks/{id}/complete`

**描述**: 执行 CompleteTask 操作

**Handler**: `CompleteTask`

**路径参数**:
- `id` (integer): id参数

**请求体示例**:
```json
{
  "data": "请求数据"
}
```

**响应示例**:
```json
{
  "data": {
    "message": "操作成功"
  }
}
```

---
### `GET /api/v1/recordings`

**描述**: 获取列表（支持分页和筛选）

**Handler**: `ListRecordings`

**查询参数**:
- `page` (integer): 页码，默认 1
- `page_size` (integer): 每页记录数，默认 20

**响应示例**:
```json
{
  "items": [
    {"id": 1, "name": "示例1"},
    {"id": 2, "name": "示例2"}
  ],
  "total": 100,
  "page": 1,
  "page_size": 20
}
```

---
### `POST /api/v1/recordings`

**描述**: 创建新记录

**Handler**: `CreateRecording`

**请求体示例**:
```json
{
  "name": "示例名称",
  "description": "示例描述"
}
```

**响应示例**:
```json
{
  "data": {
    "message": "操作成功"
  }
}
```

---
### `POST /api/v1/recordings/batch-delete`

**描述**: 删除记录（软删除）

**Handler**: `BatchDelete`

**请求体示例**:
```json
{
  "data": "请求数据"
}
```

**响应示例**:
```json
{
  "data": {
    "message": "操作成功"
  }
}
```

---
### `POST /api/v1/recordings/batch-transcribe`

**描述**: 执行 BatchTranscribe 操作

**Handler**: `BatchTranscribe`

**请求体示例**:
```json
{
  "data": "请求数据"
}
```

**响应示例**:
```json
{
  "data": {
    "message": "操作成功"
  }
}
```

---
### `GET /api/v1/recordings/dashboard/daily-report`

**描述**: 获取详细信息

**Handler**: `GetDailyReport`

**响应示例**:
```json
{
  "data": {
    "id": 1,
    "name": "示例名称",
    "created_at": "2024-01-01T00:00:00Z"
  }
}
```

---
### `GET /api/v1/recordings/dashboard/diagnosis`

**描述**: 获取详细信息

**Handler**: `GetOperationsDiagnosis`

**响应示例**:
```json
{
  "data": {
    "id": 1,
    "name": "示例名称",
    "created_at": "2024-01-01T00:00:00Z"
  }
}
```

---
### `GET /api/v1/recordings/dashboard/employee-diagnosis`

**描述**: 获取详细信息

**Handler**: `GetEmployeeDiagnosis`

**响应示例**:
```json
{
  "data": {
    "id": 1,
    "name": "示例名称",
    "created_at": "2024-01-01T00:00:00Z"
  }
}
```

---
### `GET /api/v1/recordings/dashboard/employee-growth`

**描述**: 获取详细信息

**Handler**: `GetEmployeeGrowth`

**响应示例**:
```json
{
  "data": {
    "id": 1,
    "name": "示例名称",
    "created_at": "2024-01-01T00:00:00Z"
  }
}
```

---
### `GET /api/v1/recordings/dashboard/funnel-detail`

**描述**: 获取详细信息

**Handler**: `GetFunnelDetail`

**响应示例**:
```json
{
  "data": {
    "id": 1,
    "name": "示例名称",
    "created_at": "2024-01-01T00:00:00Z"
  }
}
```

---
### `GET /api/v1/recordings/dashboard/morning-meeting`

**描述**: 获取详细信息

**Handler**: `GetMorningMeetingMaterial`

**响应示例**:
```json
{
  "data": {
    "id": 1,
    "name": "示例名称",
    "created_at": "2024-01-01T00:00:00Z"
  }
}
```

---
### `PATCH /api/v1/recordings/dashboard/target`

**描述**: 更新记录信息

**Handler**: `UpdateMonthlyTarget`

**请求体示例**:
```json
{
  "name": "示例名称",
  "description": "示例描述"
}
```

**响应示例**:
```json
{
  "data": {
    "message": "操作成功"
  }
}
```

---
### `GET /api/v1/recordings/dashboard/team-ability`

**描述**: 获取详细信息

**Handler**: `GetTeamAbility`

**响应示例**:
```json
{
  "data": {
    "id": 1,
    "name": "示例名称",
    "created_at": "2024-01-01T00:00:00Z"
  }
}
```

---
### `GET /api/v1/recordings/stats/by-scene`

**描述**: 获取详细信息

**Handler**: `GetStatsByScene`

**响应示例**:
```json
{
  "data": {
    "id": 1,
    "name": "示例名称",
    "created_at": "2024-01-01T00:00:00Z"
  }
}
```

---
### `GET /api/v1/recordings/stats/by-source`

**描述**: 获取详细信息

**Handler**: `GetStatsBySource`

**响应示例**:
```json
{
  "data": {
    "id": 1,
    "name": "示例名称",
    "created_at": "2024-01-01T00:00:00Z"
  }
}
```

---
### `GET /api/v1/recordings/stats/by-tenant`

**描述**: 获取详细信息

**Handler**: `GetStatsByTenant`

**响应示例**:
```json
{
  "data": {
    "id": 1,
    "name": "示例名称",
    "created_at": "2024-01-01T00:00:00Z"
  }
}
```

---
### `GET /api/v1/recordings/stats/daily`

**描述**: 获取详细信息

**Handler**: `GetDailyStats`

**响应示例**:
```json
{
  "data": {
    "id": 1,
    "name": "示例名称",
    "created_at": "2024-01-01T00:00:00Z"
  }
}
```

---
### `GET /api/v1/recordings/stats/duration-distribution`

**描述**: 获取详细信息

**Handler**: `GetDurationDistribution`

**响应示例**:
```json
{
  "data": {
    "id": 1,
    "name": "示例名称",
    "created_at": "2024-01-01T00:00:00Z"
  }
}
```

---
### `GET /api/v1/recordings/stats/overview`

**描述**: 获取详细信息

**Handler**: `GetStatsOverview`

**响应示例**:
```json
{
  "data": {
    "id": 1,
    "name": "示例名称",
    "created_at": "2024-01-01T00:00:00Z"
  }
}
```

---
### `POST /api/v1/recordings/upload`

**描述**: 执行 UploadRecording 操作

**Handler**: `UploadRecording`

**请求体示例**:
```json
{
  "data": "请求数据"
}
```

**响应示例**:
```json
{
  "data": {
    "message": "操作成功"
  }
}
```

---
### `GET /api/v1/recordings/{id}`

**描述**: 获取详细信息

**Handler**: `GetRecording`

**路径参数**:
- `id` (integer): id参数

**响应示例**:
```json
{
  "data": {
    "id": 1,
    "name": "示例名称",
    "created_at": "2024-01-01T00:00:00Z"
  }
}
```

---
### `PUT /api/v1/recordings/{id}`

**描述**: 更新记录信息

**Handler**: `UpdateRecording`

**路径参数**:
- `id` (integer): id参数

**请求体示例**:
```json
{
  "name": "示例名称",
  "description": "示例描述"
}
```

**响应示例**:
```json
{
  "data": {
    "message": "操作成功"
  }
}
```

---
### `DELETE /api/v1/recordings/{id}`

**描述**: 删除记录（软删除）

**Handler**: `DeleteRecording`

**路径参数**:
- `id` (integer): id参数

**响应示例**:
```json
{
  "data": {
    "message": "操作成功"
  }
}
```

---
### `GET /api/v1/recordings/{id}/analysis`

**描述**: 获取详细信息

**Handler**: `GetAnalysisResult`

**路径参数**:
- `id` (integer): id参数

**响应示例**:
```json
{
  "data": {
    "id": 1,
    "name": "示例名称",
    "created_at": "2024-01-01T00:00:00Z"
  }
}
```

---
### `POST /api/v1/recordings/{id}/analysis/feedback`

**描述**: 执行 SubmitAnalysisFeedback 操作

**Handler**: `SubmitAnalysisFeedback`

**路径参数**:
- `id` (integer): id参数

**请求体示例**:
```json
{
  "data": "请求数据"
}
```

**响应示例**:
```json
{
  "data": {
    "message": "操作成功"
  }
}
```

---
### `POST /api/v1/recordings/{id}/analyze`

**描述**: 执行 TriggerAnalyze 操作

**Handler**: `TriggerAnalyze`

**路径参数**:
- `id` (integer): id参数

**请求体示例**:
```json
{
  "data": "请求数据"
}
```

**响应示例**:
```json
{
  "data": {
    "message": "操作成功"
  }
}
```

---
### `POST /api/v1/recordings/{id}/clean`

**描述**: 执行 TriggerClean 操作

**Handler**: `TriggerClean`

**路径参数**:
- `id` (integer): id参数

**请求体示例**:
```json
{
  "data": "请求数据"
}
```

**响应示例**:
```json
{
  "data": {
    "message": "操作成功"
  }
}
```

---
### `POST /api/v1/recordings/{id}/confirm-action`

**描述**: 执行 ConfirmFollowUpAction 操作

**Handler**: `ConfirmFollowUpAction`

**路径参数**:
- `id` (integer): id参数

**请求体示例**:
```json
{
  "data": "请求数据"
}
```

**响应示例**:
```json
{
  "data": {
    "message": "操作成功"
  }
}
```

---
### `GET /api/v1/recordings/{id}/file-test`

**描述**: 执行 TestPlayback 操作

**Handler**: `TestPlayback`

**路径参数**:
- `id` (integer): id参数

**响应示例**:
```json
{
  "data": {
    "message": "操作成功"
  }
}
```

---
### `POST /api/v1/recordings/{id}/follow-up-tasks/dispatch`

**描述**: 执行 DispatchFollowUpTasks 操作

**Handler**: `DispatchFollowUpTasks`

**路径参数**:
- `id` (integer): id参数

**请求体示例**:
```json
{
  "data": "请求数据"
}
```

**响应示例**:
```json
{
  "data": {
    "message": "操作成功"
  }
}
```

---
### `POST /api/v1/recordings/{id}/generate-opening-script`

**描述**: 执行 GenerateOpeningScript 操作

**Handler**: `GenerateOpeningScript`

**路径参数**:
- `id` (integer): id参数

**请求体示例**:
```json
{
  "data": "请求数据"
}
```

**响应示例**:
```json
{
  "data": {
    "message": "操作成功"
  }
}
```

---
### `POST /api/v1/recordings/{id}/generate-operations-plan`

**描述**: 执行 GenerateOperationsPlan 操作

**Handler**: `GenerateOperationsPlan`

**路径参数**:
- `id` (integer): id参数

**请求体示例**:
```json
{
  "data": "请求数据"
}
```

**响应示例**:
```json
{
  "data": {
    "message": "操作成功"
  }
}
```

---
### `GET /api/v1/recordings/{id}/learning-recommendation`

**描述**: 获取详细信息

**Handler**: `GetLearningRecommendation`

**路径参数**:
- `id` (integer): id参数

**响应示例**:
```json
{
  "data": {
    "id": 1,
    "name": "示例名称",
    "created_at": "2024-01-01T00:00:00Z"
  }
}
```

---
### `GET /api/v1/recordings/{id}/operations-plan-jobs/{job_id}`

**描述**: 获取详细信息

**Handler**: `GetOperationsPlanJobStatus`

**路径参数**:
- `id` (integer): id参数
- `job_id` (integer): job_id参数

**响应示例**:
```json
{
  "data": {
    "id": 1,
    "name": "示例名称",
    "created_at": "2024-01-01T00:00:00Z"
  }
}
```

---
### `GET /api/v1/recordings/{id}/play-url`

**描述**: 获取详细信息

**Handler**: `GetPlayURL`

**路径参数**:
- `id` (integer): id参数

**响应示例**:
```json
{
  "data": {
    "id": 1,
    "name": "示例名称",
    "created_at": "2024-01-01T00:00:00Z"
  }
}
```

---
### `GET /api/v1/recordings/{id}/tasks`

**描述**: 获取详细信息

**Handler**: `GetRecordingTasks`

**路径参数**:
- `id` (integer): id参数

**响应示例**:
```json
{
  "data": {
    "id": 1,
    "name": "示例名称",
    "created_at": "2024-01-01T00:00:00Z"
  }
}
```

---
### `POST /api/v1/recordings/{id}/transcribe`

**描述**: 执行 TriggerTranscribe 操作

**Handler**: `TriggerTranscribe`

**路径参数**:
- `id` (integer): id参数

**请求体示例**:
```json
{
  "data": "请求数据"
}
```

**响应示例**:
```json
{
  "data": {
    "message": "操作成功"
  }
}
```

---

## support

**端点数量**: 36

### `POST /api/v1/data-browser/clear-import-data`

**描述**: 导入数据

**Handler**: `ClearImportData`

**请求体示例**:
```json
{
  "data": "请求数据"
}
```

**响应示例**:
```json
{
  "data": {
    "message": "操作成功"
  }
}
```

---
### `GET /api/v1/data-browser/statistics`

**描述**: 获取详细信息

**Handler**: `GetDatabaseStatistics`

**响应示例**:
```json
{
  "data": {
    "id": 1,
    "name": "示例名称",
    "created_at": "2024-01-01T00:00:00Z"
  }
}
```

---
### `GET /api/v1/data-browser/tables`

**描述**: 获取列表（支持分页和筛选）

**Handler**: `ListTables`

**查询参数**:
- `page` (integer): 页码，默认 1
- `page_size` (integer): 每页记录数，默认 20

**响应示例**:
```json
{
  "items": [
    {"id": 1, "name": "示例1"},
    {"id": 2, "name": "示例2"}
  ],
  "total": 100,
  "page": 1,
  "page_size": 20
}
```

---
### `GET /api/v1/data-browser/tables/{table_name}/data`

**描述**: 获取详细信息

**Handler**: `GetTableData`

**路径参数**:
- `table_name` (string): table_name参数

**响应示例**:
```json
{
  "data": {
    "id": 1,
    "name": "示例名称",
    "created_at": "2024-01-01T00:00:00Z"
  }
}
```

---
### `GET /api/v1/data-browser/tables/{table_name}/export`

**描述**: 导出数据

**Handler**: `ExportTableData`

**路径参数**:
- `table_name` (string): table_name参数

**响应示例**:
```json
{
  "data": {
    "message": "操作成功"
  }
}
```

---
### `GET /api/v1/data-browser/tables/{table_name}/structure`

**描述**: 获取详细信息

**Handler**: `GetTableStructure`

**路径参数**:
- `table_name` (string): table_name参数

**响应示例**:
```json
{
  "data": {
    "id": 1,
    "name": "示例名称",
    "created_at": "2024-01-01T00:00:00Z"
  }
}
```

---
### `DELETE /api/v1/data-browser/tables/{table_name}/truncate`

**描述**: 执行 TruncateTable 操作

**Handler**: `TruncateTable`

**路径参数**:
- `table_name` (string): table_name参数

**响应示例**:
```json
{
  "data": {
    "message": "操作成功"
  }
}
```

---
### `GET /api/v1/llm/cost/summary`

**描述**: 获取详细信息

**Handler**: `GetLLMCostSummary`

**响应示例**:
```json
{
  "data": {
    "id": 1,
    "name": "示例名称",
    "created_at": "2024-01-01T00:00:00Z"
  }
}
```

---
### `GET /api/v1/llm/cost/tenant/{tenant_id}`

**描述**: 获取详细信息

**Handler**: `GetLLMCostByTenant`

**路径参数**:
- `tenant_id` (integer): tenant_id参数

**响应示例**:
```json
{
  "data": {
    "id": 1,
    "name": "示例名称",
    "created_at": "2024-01-01T00:00:00Z"
  }
}
```

---
### `GET /api/v1/llm/models`

**描述**: 获取列表（支持分页和筛选）

**Handler**: `ListLLMModelConfigs`

**查询参数**:
- `page` (integer): 页码，默认 1
- `page_size` (integer): 每页记录数，默认 20

**响应示例**:
```json
{
  "items": [
    {"id": 1, "name": "示例1"},
    {"id": 2, "name": "示例2"}
  ],
  "total": 100,
  "page": 1,
  "page_size": 20
}
```

---
### `POST /api/v1/llm/models`

**描述**: 创建新记录

**Handler**: `CreateLLMModelConfig`

**请求体示例**:
```json
{
  "name": "示例名称",
  "description": "示例描述"
}
```

**响应示例**:
```json
{
  "data": {
    "message": "操作成功"
  }
}
```

---
### `GET /api/v1/llm/models/{id}`

**描述**: 获取详细信息

**Handler**: `GetLLMModelConfig`

**路径参数**:
- `id` (integer): id参数

**响应示例**:
```json
{
  "data": {
    "id": 1,
    "name": "示例名称",
    "created_at": "2024-01-01T00:00:00Z"
  }
}
```

---
### `PUT /api/v1/llm/models/{id}`

**描述**: 更新记录信息

**Handler**: `UpdateLLMModelConfig`

**路径参数**:
- `id` (integer): id参数

**请求体示例**:
```json
{
  "name": "示例名称",
  "description": "示例描述"
}
```

**响应示例**:
```json
{
  "data": {
    "message": "操作成功"
  }
}
```

---
### `DELETE /api/v1/llm/models/{id}`

**描述**: 删除记录（软删除）

**Handler**: `DeleteLLMModelConfig`

**路径参数**:
- `id` (integer): id参数

**响应示例**:
```json
{
  "data": {
    "message": "操作成功"
  }
}
```

---
### `POST /api/v1/llm/models/{id}/set-default`

**描述**: 执行 SetDefaultLLMModelConfig 操作

**Handler**: `SetDefaultLLMModelConfig`

**路径参数**:
- `id` (integer): id参数

**请求体示例**:
```json
{
  "data": "请求数据"
}
```

**响应示例**:
```json
{
  "data": {
    "message": "操作成功"
  }
}
```

---
### `GET /api/v1/llm/records`

**描述**: 获取列表（支持分页和筛选）

**Handler**: `ListLLMCallRecords`

**查询参数**:
- `page` (integer): 页码，默认 1
- `page_size` (integer): 每页记录数，默认 20

**响应示例**:
```json
{
  "items": [
    {"id": 1, "name": "示例1"},
    {"id": 2, "name": "示例2"}
  ],
  "total": 100,
  "page": 1,
  "page_size": 20
}
```

---
### `GET /api/v1/llm/records/stats`

**描述**: 获取详细信息

**Handler**: `GetLLMCallRecordStats`

**响应示例**:
```json
{
  "data": {
    "id": 1,
    "name": "示例名称",
    "created_at": "2024-01-01T00:00:00Z"
  }
}
```

---
### `GET /api/v1/llm/records/{id}`

**描述**: 获取详细信息

**Handler**: `GetLLMCallRecordByID`

**路径参数**:
- `id` (integer): id参数

**响应示例**:
```json
{
  "data": {
    "id": 1,
    "name": "示例名称",
    "created_at": "2024-01-01T00:00:00Z"
  }
}
```

---
### `GET /api/v1/logs/operations`

**描述**: 获取列表（支持分页和筛选）

**Handler**: `ListOperationLogs`

**查询参数**:
- `page` (integer): 页码，默认 1
- `page_size` (integer): 每页记录数，默认 20

**响应示例**:
```json
{
  "items": [
    {"id": 1, "name": "示例1"},
    {"id": 2, "name": "示例2"}
  ],
  "total": 100,
  "page": 1,
  "page_size": 20
}
```

---
### `GET /api/v1/logs/operations/stats`

**描述**: 获取详细信息

**Handler**: `GetOperationLogStats`

**响应示例**:
```json
{
  "data": {
    "id": 1,
    "name": "示例名称",
    "created_at": "2024-01-01T00:00:00Z"
  }
}
```

---
### `GET /api/v1/logs/operations/{id}`

**描述**: 获取详细信息

**Handler**: `GetOperationLogByID`

**路径参数**:
- `id` (integer): id参数

**响应示例**:
```json
{
  "data": {
    "id": 1,
    "name": "示例名称",
    "created_at": "2024-01-01T00:00:00Z"
  }
}
```

---
### `GET /api/v1/metadata/fields`

**描述**: 获取当前登录用户信息

**Handler**: `GetMetadataFields`

**响应示例**:
```json
{
  "data": {
    "id": 1,
    "name": "示例名称",
    "created_at": "2024-01-01T00:00:00Z"
  }
}
```

---
### `POST /api/v1/metadata/validate-template`

**描述**: 执行 ValidateTemplate 操作

**Handler**: `ValidateTemplate`

**请求体示例**:
```json
{
  "data": "请求数据"
}
```

**响应示例**:
```json
{
  "data": {
    "message": "操作成功"
  }
}
```

---
### `GET /api/v1/notifications`

**描述**: 获取列表（支持分页和筛选）

**Handler**: `ListNotifications`

**查询参数**:
- `page` (integer): 页码，默认 1
- `page_size` (integer): 每页记录数，默认 20

**响应示例**:
```json
{
  "items": [
    {"id": 1, "name": "示例1"},
    {"id": 2, "name": "示例2"}
  ],
  "total": 100,
  "page": 1,
  "page_size": 20
}
```

---
### `GET /api/v1/notifications/device-tokens/me`

**描述**: 获取详细信息

**Handler**: `GetMyDeviceTokens`

**响应示例**:
```json
{
  "data": {
    "id": 1,
    "name": "示例名称",
    "created_at": "2024-01-01T00:00:00Z"
  }
}
```

---
### `POST /api/v1/notifications/device-tokens/register`

**描述**: 执行 RegisterDeviceToken 操作

**Handler**: `RegisterDeviceToken`

**请求体示例**:
```json
{
  "data": "请求数据"
}
```

**响应示例**:
```json
{
  "data": {
    "message": "操作成功"
  }
}
```

---
### `POST /api/v1/notifications/device-tokens/unregister`

**描述**: 执行 UnregisterDeviceToken 操作

**Handler**: `UnregisterDeviceToken`

**请求体示例**:
```json
{
  "data": "请求数据"
}
```

**响应示例**:
```json
{
  "data": {
    "message": "操作成功"
  }
}
```

---
### `PUT /api/v1/notifications/mark-all-read`

**描述**: 执行 MarkAllNotificationsAsRead 操作

**Handler**: `MarkAllNotificationsAsRead`

**请求体示例**:
```json
{
  "data": "请求数据"
}
```

**响应示例**:
```json
{
  "data": {
    "message": "操作成功"
  }
}
```

---
### `POST /api/v1/notifications/push-to-app`

**描述**: 执行 PushNotification 操作

**Handler**: `PushNotification`

**请求体示例**:
```json
{
  "data": "请求数据"
}
```

**响应示例**:
```json
{
  "data": {
    "message": "操作成功"
  }
}
```

---
### `GET /api/v1/notifications/unread-count`

**描述**: 获取详细信息

**Handler**: `GetUnreadCount`

**响应示例**:
```json
{
  "data": {
    "id": 1,
    "name": "示例名称",
    "created_at": "2024-01-01T00:00:00Z"
  }
}
```

---
### `PUT /api/v1/notifications/{id}/read`

**描述**: 执行 MarkNotificationAsRead 操作

**Handler**: `MarkNotificationAsRead`

**路径参数**:
- `id` (integer): id参数

**请求体示例**:
```json
{
  "data": "请求数据"
}
```

**响应示例**:
```json
{
  "data": {
    "message": "操作成功"
  }
}
```

---
### `GET /api/v1/visits`

**描述**: 获取列表（支持分页和筛选）

**Handler**: `ListVisits`

**查询参数**:
- `page` (integer): 页码，默认 1
- `page_size` (integer): 每页记录数，默认 20

**响应示例**:
```json
{
  "items": [
    {"id": 1, "name": "示例1"},
    {"id": 2, "name": "示例2"}
  ],
  "total": 100,
  "page": 1,
  "page_size": 20
}
```

---
### `GET /api/v1/visits/filters`

**描述**: 获取详细信息

**Handler**: `GetVisitFilters`

**响应示例**:
```json
{
  "data": {
    "id": 1,
    "name": "示例名称",
    "created_at": "2024-01-01T00:00:00Z"
  }
}
```

---
### `GET /api/v1/visits/health`

**描述**: 执行 VisitsHealthCheck 操作

**Handler**: `VisitsHealthCheck`

**响应示例**:
```json
{
  "data": {
    "message": "操作成功"
  }
}
```

---
### `GET /api/v1/visits/statistics`

**描述**: 获取详细信息

**Handler**: `GetVisitStatistics`

**响应示例**:
```json
{
  "data": {
    "id": 1,
    "name": "示例名称",
    "created_at": "2024-01-01T00:00:00Z"
  }
}
```

---
### `GET /api/v1/visits/{visit_id}`

**描述**: 获取详细信息

**Handler**: `GetVisitByID`

**路径参数**:
- `visit_id` (integer): visit_id参数

**响应示例**:
```json
{
  "data": {
    "id": 1,
    "name": "示例名称",
    "created_at": "2024-01-01T00:00:00Z"
  }
}
```

---

## sysconfig

**端点数量**: 19

### `GET /api/v1/sysconfig/feature-groups`

**描述**: 获取列表（支持分页和筛选）

**Handler**: `ListFeatureGroups`

**查询参数**:
- `page` (integer): 页码，默认 1
- `page_size` (integer): 每页记录数，默认 20

**响应示例**:
```json
{
  "items": [
    {"id": 1, "name": "示例1"},
    {"id": 2, "name": "示例2"}
  ],
  "total": 100,
  "page": 1,
  "page_size": 20
}
```

---
### `POST /api/v1/sysconfig/feature-groups`

**描述**: 创建新记录

**Handler**: `CreateFeatureGroup`

**请求体示例**:
```json
{
  "name": "示例名称",
  "description": "示例描述"
}
```

**响应示例**:
```json
{
  "data": {
    "message": "操作成功"
  }
}
```

---
### `GET /api/v1/sysconfig/feature-groups/{id}`

**描述**: 获取详细信息

**Handler**: `GetFeatureGroup`

**路径参数**:
- `id` (integer): id参数

**响应示例**:
```json
{
  "data": {
    "id": 1,
    "name": "示例名称",
    "created_at": "2024-01-01T00:00:00Z"
  }
}
```

---
### `PUT /api/v1/sysconfig/feature-groups/{id}`

**描述**: 更新记录信息

**Handler**: `UpdateFeatureGroup`

**路径参数**:
- `id` (integer): id参数

**请求体示例**:
```json
{
  "name": "示例名称",
  "description": "示例描述"
}
```

**响应示例**:
```json
{
  "data": {
    "message": "操作成功"
  }
}
```

---
### `GET /api/v1/sysconfig/feature-options`

**描述**: 获取详细信息

**Handler**: `GetFeatureOptions`

**响应示例**:
```json
{
  "data": {
    "id": 1,
    "name": "示例名称",
    "created_at": "2024-01-01T00:00:00Z"
  }
}
```

---
### `GET /api/v1/sysconfig/subscription-plans`

**描述**: 获取列表（支持分页和筛选）

**Handler**: `ListSubscriptionPlans`

**查询参数**:
- `page` (integer): 页码，默认 1
- `page_size` (integer): 每页记录数，默认 20

**响应示例**:
```json
{
  "items": [
    {"id": 1, "name": "示例1"},
    {"id": 2, "name": "示例2"}
  ],
  "total": 100,
  "page": 1,
  "page_size": 20
}
```

---
### `POST /api/v1/sysconfig/subscription-plans`

**描述**: 创建新记录

**Handler**: `CreateSubscriptionPlan`

**请求体示例**:
```json
{
  "name": "示例名称",
  "description": "示例描述"
}
```

**响应示例**:
```json
{
  "data": {
    "message": "操作成功"
  }
}
```

---
### `PUT /api/v1/sysconfig/subscription-plans/{id}`

**描述**: 更新记录信息

**Handler**: `UpdateSubscriptionPlan`

**路径参数**:
- `id` (integer): id参数

**请求体示例**:
```json
{
  "name": "示例名称",
  "description": "示例描述"
}
```

**响应示例**:
```json
{
  "data": {
    "message": "操作成功"
  }
}
```

---
### `GET /api/v1/sysconfig/tenants`

**描述**: 获取列表（支持分页和筛选）

**Handler**: `ListTenants`

**查询参数**:
- `page` (integer): 页码，默认 1
- `page_size` (integer): 每页记录数，默认 20

**响应示例**:
```json
{
  "items": [
    {"id": 1, "name": "示例1"},
    {"id": 2, "name": "示例2"}
  ],
  "total": 100,
  "page": 1,
  "page_size": 20
}
```

---
### `GET /api/v1/sysconfig/tenants/{id}`

**描述**: 获取详细信息

**Handler**: `GetTenant`

**路径参数**:
- `id` (integer): id参数

**响应示例**:
```json
{
  "data": {
    "id": 1,
    "name": "示例名称",
    "created_at": "2024-01-01T00:00:00Z"
  }
}
```

---
### `PUT /api/v1/sysconfig/tenants/{id}`

**描述**: 更新记录信息

**Handler**: `UpdateTenant`

**路径参数**:
- `id` (integer): id参数

**请求体示例**:
```json
{
  "name": "示例名称",
  "description": "示例描述"
}
```

**响应示例**:
```json
{
  "data": {
    "message": "操作成功"
  }
}
```

---
### `GET /api/v1/sysconfig/tenants/{id}/effective-features`

**描述**: 获取详细信息

**Handler**: `GetEffectiveFeaturePolicy`

**路径参数**:
- `id` (integer): id参数

**响应示例**:
```json
{
  "data": {
    "id": 1,
    "name": "示例名称",
    "created_at": "2024-01-01T00:00:00Z"
  }
}
```

---
### `POST /api/v1/sysconfig/tenants/{id}/feature-group`

**描述**: 执行 AssignFeatureGroup 操作

**Handler**: `AssignFeatureGroup`

**路径参数**:
- `id` (integer): id参数

**请求体示例**:
```json
{
  "data": "请求数据"
}
```

**响应示例**:
```json
{
  "data": {
    "message": "操作成功"
  }
}
```

---
### `PUT /api/v1/sysconfig/tenants/{id}/feature-overrides`

**描述**: 获取列表数据（支持分页）

**Handler**: `SetFeatureOverrides`

**路径参数**:
- `id` (integer): id参数

**请求体示例**:
```json
{
  "data": "请求数据"
}
```

**响应示例**:
```json
{
  "data": {
    "message": "操作成功"
  }
}
```

---
### `GET /api/v1/sysconfig/tenants/{id}/feature-overrides`

**描述**: 获取详细信息

**Handler**: `GetFeatureOverrides`

**路径参数**:
- `id` (integer): id参数

**响应示例**:
```json
{
  "data": {
    "id": 1,
    "name": "示例名称",
    "created_at": "2024-01-01T00:00:00Z"
  }
}
```

---
### `GET /api/v1/sysconfig/tenants/{id}/subscription`

**描述**: 获取详细信息

**Handler**: `GetTenantSubscription`

**路径参数**:
- `id` (integer): id参数

**响应示例**:
```json
{
  "data": {
    "id": 1,
    "name": "示例名称",
    "created_at": "2024-01-01T00:00:00Z"
  }
}
```

---
### `POST /api/v1/sysconfig/tenants/{id}/subscription/action`

**描述**: 执行 PerformSubscriptionAction 操作

**Handler**: `PerformSubscriptionAction`

**路径参数**:
- `id` (integer): id参数

**请求体示例**:
```json
{
  "data": "请求数据"
}
```

**响应示例**:
```json
{
  "data": {
    "message": "操作成功"
  }
}
```

---
### `GET /api/v1/sysconfig/tenants/{id}/subscription/events`

**描述**: 获取详细信息

**Handler**: `GetSubscriptionEvents`

**路径参数**:
- `id` (integer): id参数

**响应示例**:
```json
{
  "data": {
    "id": 1,
    "name": "示例名称",
    "created_at": "2024-01-01T00:00:00Z"
  }
}
```

---
### `GET /api/v1/sysconfig/tenants/{id}/validity-logs`

**描述**: 获取详细信息

**Handler**: `GetValidityChangeLogs`

**路径参数**:
- `id` (integer): id参数

**响应示例**:
```json
{
  "data": {
    "id": 1,
    "name": "示例名称",
    "created_at": "2024-01-01T00:00:00Z"
  }
}
```

---
