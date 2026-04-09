# 两周冲刺任务拆解（端点级）

> 基线来源：`docs/DEVELOPMENT_PLAN.md`（2026-04-09）  
> 周期：10 个工作日  
> 目标：优先清理高影响壳实现端点，确保主流程可用。

## 1. 负责人建议（可直接分配）

- Backend-A：`content` 主流程（LLM/OSS 相关）
- Backend-B：`badge` 主流程（badge-middleware 相关）
- Backend-C：`organization + support + customer` 收尾
- QA：接口回归、权限回归、错误码回归

## 2. 每日执行清单

### Day 1（P0）

模块：`content`

1. `GET /api/v1/content/hot-topics`
2. `POST /api/v1/content/hot-topics/refresh`
3. `POST /api/v1/content/idea-topics/start`
4. `POST /api/v1/content/idea-topics/parse-files`
5. `POST /api/v1/content/idea-topics/generate`
6. `POST /api/v1/content/idea-topics/save-topics`

交付物：

- 话题链路从占位响应改为真实服务调用。
- 增加失败重试与超时日志（LLM 调用）。

### Day 2（P0）

模块：`content`

1. `GET /api/v1/content/contents`
2. `GET /api/v1/content/contents/{content_id}`
3. `POST /api/v1/content/contents`
4. `PUT /api/v1/content/contents/{content_id}`
5. `DELETE /api/v1/content/contents/{content_id}`
6. `POST /api/v1/content/contents/generate`

交付物：

- `contents` CRUD + `generate` 可跑通。
- 租户隔离与分页参数校验完成。

### Day 3（P0）

模块：`content`

1. `POST /api/v1/content/contents/{content_id}/generate-images`
2. `POST /api/v1/content/contents/{content_id}/generate-single-image`
3. `POST /api/v1/content/contents/{content_id}/save-composed-images`
4. `POST /api/v1/content/contents/{content_id}/publish`
5. `POST /api/v1/content/contents/{content_id}/unpublish`

交付物：

- 图片生成、存储、发布/下线闭环。
- OSS 上传失败可观测、可回滚。

### Day 4（P0）

模块：`content`

1. `GET /api/v1/content/publish-tasks/dashboard`
2. `GET /api/v1/content/publish-tasks`
3. `GET /api/v1/content/publish-tasks/{task_id}`
4. `POST /api/v1/content/publish-tasks`
5. `POST /api/v1/content/publish-tasks/batch`
6. `PUT /api/v1/content/publish-tasks/{task_id}/status`
7. `PUT /api/v1/content/publish-tasks/{task_id}/cancel`
8. `PUT /api/v1/content/publish-tasks/{task_id}/retry`
9. `DELETE /api/v1/content/publish-tasks/{task_id}`

交付物：

- 发布任务全生命周期可用。
- 批量任务具备幂等键或重复提交保护。

### Day 5（P0）

模块：`badge`

1. `POST /api/v1/badge-control/acceptance/validate`
2. `POST /api/v1/badge-control/acceptance/vendor-check`
3. `POST /api/v1/badge-control/inspection/device/{device_id}`
4. `POST /api/v1/badge-control/inspection/batch`
5. `GET /api/v1/badge-control/inspection/devices`
6. `GET /api/v1/badge-control/inspection/device/{device_id}/live-status`
7. `POST /api/v1/badge-control/inspection/device/{device_id}/recording-test`
8. `POST /api/v1/badge-control/tickets/submit-by-device`

交付物：

- 巡检与验收流程改为真实 badge-middleware 对接。
- 错误码统一、设备状态变更可追踪。

### Day 6（P0）

模块：`badge`

1. `POST /api/v1/badge-control/vendor-pool/sync-devices`
2. `POST /api/v1/badge-control/vendor-pool/sync-and-diff`
3. `GET /api/v1/badge-control/vendor-pool/diff`
4. `GET /api/v1/badge-control/vendor-pool/sync-batches`
5. `GET /api/v1/badge-control/vendor-pool/sync-batches/{id}/items`
6. `POST /api/v1/badge-control/vendor-pool/sync-batches/{id}/rollback-drafts`
7. `POST /api/v1/badge-control/vendor-pool/actions/create-acceptance-drafts`
8. `POST /api/v1/badge-control/vendor-pool/actions/mark-pending-assignment`
9. `POST /api/v1/badge-control/vendor-pool/actions/create-exception-tickets`
10. `GET /api/v1/badge-control/tenant-employees`

交付物：

- 供应池同步与差异链路可完整执行。
- 批次明细可回放、可审计。

### Day 7（P0）

模块：`badge`

1. `GET /api/v1/smart-badge/tenant/devices/overview`
2. `GET /api/v1/smart-badge/me`
3. `POST /api/v1/smart-badge/me/recording/start`
4. `POST /api/v1/smart-badge/me/recording/stop`
5. `GET /api/v1/smart-badge/tenant/devices/{device_no}/history`
6. `POST /api/v1/smart-badge/callback/developer`
7. `POST /api/v1/smart-badge/callback/audio`
8. `POST /api/v1/smart-badge/process/pending`

交付物：

- smart-badge 高级端点脱离占位逻辑。
- 回调处理具备鉴权校验和重放保护。

### Day 8（P1）

模块：`organization`

1. `GET /api/v1/patients`
2. `GET /api/v1/patients/{id}`
3. `POST /api/v1/patients`
4. `PUT /api/v1/patients/{id}`
5. `DELETE /api/v1/patients/{id}`
6. `GET /api/v1/patients/{id}/360`
7. `POST /api/v1/patients/sync-from-visits`
8. `GET /api/v1/doctors`
9. `GET /api/v1/doctors/{id}`
10. `POST /api/v1/doctors`
11. `PUT /api/v1/doctors/{id}`
12. `DELETE /api/v1/doctors/{id}`

交付物：

- 医患核心 CRUD 与查询接口可用。
- 同步任务具备幂等和失败告警。

### Day 9（P1）

模块：`organization` + `support`

1. `POST /api/v1/doctors/sync-from-visits`
2. `GET /api/v1/doctors/{id}/performance`
3. `GET /api/v1/doctors/performance/summary`
4. `GET /api/v1/doctors/{id}/employees`
5. `POST /api/v1/doctors/{id}/employees`
6. `POST /api/v1/departments/sync-from-visits`
7. `GET /api/v1/departments/{id}/performance`
8. `GET /api/v1/data-browser/tables`
9. `GET /api/v1/data-browser/tables/{table_name}/structure`
10. `GET /api/v1/data-browser/tables/{table_name}/data`
11. `GET /api/v1/data-browser/tables/{table_name}/export`
12. `GET /api/v1/data-browser/statistics`
13. `DELETE /api/v1/data-browser/tables/{table_name}/truncate`
14. `POST /api/v1/data-browser/clear-import-data`
15. `GET /api/v1/visits`
16. `GET /api/v1/visits/statistics`
17. `GET /api/v1/visits/filters`
18. `GET /api/v1/visits/{visit_id}`

交付物：

- 组织高级统计和映射能力可用。
- Support 数据浏览链路具备权限与安全保护。

### Day 10（P2 + 收尾）

模块：`customer` + 全量回归

1. `GET /api/v1/customers/{id}/momentum-history`
2. `GET /api/v1/customers/duplicates`
3. `POST /api/v1/customers/merge`
4. `GET /api/v1/customers/{id}/consultation-records`
5. `GET /api/v1/customers/{id}/emr-records`
6. `POST /api/v1/customer-tags/batch`
7. `GET /api/v1/customer-tags/stats`
8. `GET /api/v1/customer-groups/{id}/members`
9. `POST /api/v1/customer-groups/{id}/members`
10. `DELETE /api/v1/customer-groups/{id}/members`
11. `POST /api/v1/customer-groups/rules/preview`
12. `POST /api/v1/customer-groups/rules/validate`

交付物：

- customer 高级能力补齐。
- 全链路回归、文档回刷、上线检查完成。

## 3. 每日完成定义（Checklist）

每天收工前必须完成：

1. 对应端点移除占位实现（`TODO: Implement`）。
2. 接口自测通过（成功/参数错误/权限错误至少三类）。
3. 更新回归脚本或测试用例。
4. 提交变更说明（端点、影响表、外部依赖）。

## 4. 冲刺末交付清单

1. 代码：壳实现端点按计划转为真实实现。
2. 文档：
   - `docs/openapi_complete.yaml`
   - `docs/API_ENDPOINTS.md`
   - `docs/IMPLEMENTATION_AUDIT.json`
   - `docs/DEVELOPMENT_PLAN.md`
3. 验证材料：接口回归结果、关键链路演示记录。
