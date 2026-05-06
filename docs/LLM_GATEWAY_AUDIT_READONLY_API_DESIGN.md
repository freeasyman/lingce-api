# llm-gateway 只读审计 API 详细开发方案（评审稿）

## 1. 背景与目标

### 1.1 背景

当前架构中：

1. `llm-gateway` 负责推理执行（`/v1/inference/text`、`/v1/inference/audio`）并将审计写入本地 SQLite（`gateway_call_records`）。
2. 业务侧（旧版 `lince-medical-ops` / 新版 `lingce-api`）仍保留 `llm_call_records` 查询与成本统计能力（PostgreSQL）。
3. 造成事实数据分散：真实推理审计在 `llm-gateway`，LLM 管理页依赖的记录/成本接口仍主要基于业务库。

### 1.2 目标

新增 `llm-gateway` 的**只读审计 API**，用于查询：

1. 调用记录列表
2. 调用记录详情
3. 调用统计
4. 成本汇总
5. 租户成本视图

并保证与旧版 LLM 管理页的查询语义尽量兼容，便于后续平滑切换。

### 1.3 明确非目标

本期不做：

1. 模型配置管理（不新增 `/models` 写接口）
2. 提示词管理（不新增 `/prompts` 写接口）
3. 推理协议变更（不修改 `/v1/inference/*` 入参出参）
4. 业务 RBAC（gateway 层仅 token 鉴权，不做业务身份权限）

---

## 2. 旧版能力复盘（作为兼容基线）

旧版 `lince-medical-ops`：

1. 记录列表：`GET /api/v1/llm/records`
2. 记录统计：`GET /api/v1/llm/records/stats`
3. 记录详情：`GET /api/v1/llm/records/{record_id}`
4. 成本汇总：`GET /api/v1/llm/cost/summary`
5. 租户成本：`GET /api/v1/llm/cost/tenant/{tenant_id}`

常用筛选：

1. `tenant_id`
2. `function_type`
3. `module`
4. `model_code`
5. `success`
6. `trace_id`
7. `start_date` / `end_date`
8. 分页 `page` / `size`

兼容原则：

1. 新增 API 输出结构与旧版尽量一致（`items/total/page/size/pages`、`stats` 字段命名一致）。
2. 若 gateway 无对应字段，返回 `null` 或通过 metadata 补充，不伪造数据。

---

## 3. 当前 llm-gateway 数据模型与约束

### 3.1 数据源

SQLite 文件：`AUDIT_DB_PATH`（默认 `./data/audit.db`）

表：`gateway_call_records`

关键列：

1. `id`（自增）
2. `request_id`（唯一）
3. `trace_id`
4. `tenant_id`
5. `caller_service`
6. `caller_module`
7. `function_type`
8. `provider`
9. `model_code`
10. `success`
11. `error_code`, `error_message`
12. `input_tokens`, `output_tokens`, `total_tokens`
13. `input_cost`, `output_cost`, `total_cost`
14. `latency_ms`
15. `usage_metadata`（JSON string）
16. `request_metadata`（JSON string）
17. `created_at`

### 3.2 已有索引

1. `idx_trace_id`
2. `idx_tenant_id`
3. `idx_created_at`
4. `idx_function_type`
5. `idx_success`

### 3.3 缺失点

1. 无 HTTP 查询路由
2. 无统一分页查询封装
3. 无统计聚合接口
4. 无与旧版前端一致的输出模型

---

## 4. 新增 API 设计（只读）

统一前缀：`/v1/audit`

### 4.1 记录列表

`GET /v1/audit/records`

Query 参数：

1. `tenant_id` `int`（可选）
2. `function_type` `string`（可选）
3. `module` `string`（可选，对应 `caller_module`）
4. `model_code` `string`（可选）
5. `provider` `string`（可选）
6. `success` `bool`（可选）
7. `trace_id` `string`（可选）
8. `start_date` `YYYY-MM-DD`（可选，含当天 00:00:00）
9. `end_date` `YYYY-MM-DD`（可选，含当天 23:59:59）
10. `page` `int`（默认 1）
11. `size` `int`（默认 50，最大 100）
12. `sort_by`（默认 `created_at`）
13. `sort_order`（`asc|desc`，默认 `desc`）

返回：

```json
{
  "items": [
    {
      "id": 20,
      "request_id": "xxx",
      "tenant_id": 123,
      "function_type": "chat",
      "module": "content",
      "provider": "dashscope",
      "model_code": "qwen-turbo",
      "success": true,
      "input_tokens": 26,
      "output_tokens": 18,
      "total_tokens": 44,
      "input_cost": 0.00001,
      "output_cost": 0.000008,
      "total_cost": 0.000018,
      "latency_ms": 588,
      "created_at": "2026-04-05 05:19:03"
    }
  ],
  "total": 100,
  "page": 1,
  "size": 50,
  "pages": 2
}
```

### 4.2 记录详情

`GET /v1/audit/records/{request_id}`

说明：

1. 使用 `request_id`（全局唯一）作为详情主键。
2. 同时保留 `id` 字段用于兼容展示。

返回字段（完整）：

1. 基础字段：与列表一致
2. 错误字段：`error_code`, `error_message`
3. 元数据字段：`usage_metadata`, `request_metadata`（解析为 JSON 对象）
4. `trace_id`, `caller_service`, `caller_module`

### 4.3 调用统计

`GET /v1/audit/records/stats`

Query 参数：

1. `tenant_id`
2. `function_type`
3. `module`
4. `start_date`
5. `end_date`

返回：

```json
{
  "total_calls": 1200,
  "total_tokens": 998877,
  "total_input_tokens": 600000,
  "total_output_tokens": 398877,
  "total_cost": 321.1234,
  "avg_latency_ms": 512.3,
  "success_rate": 98.75,
  "success_count": 1185,
  "failure_count": 15
}
```

### 4.4 成本汇总

`GET /v1/audit/cost/summary`

Query 参数：

1. `start_date`
2. `end_date`

返回：

1. `total_cost`, `total_calls`, `total_tokens`
2. `cost_by_tenant`
3. `cost_by_function_type`
4. `cost_by_module`
5. `cost_by_model`

### 4.5 租户成本

`GET /v1/audit/cost/by-tenant/{tenant_id}`

Query 参数：

1. `start_date`
2. `end_date`

返回：

1. 租户总成本/调用/Token
2. 各维度分组（function/module/model）
3. `cost_trend`（按日）

---

## 5. 字段映射与兼容策略

## 5.1 旧版字段映射

1. `module` -> `caller_module`
2. `function_type` -> `function_type`
3. `model_code` -> `model_code`
4. `success` -> `success`
5. `trace_id` -> `trace_id`
6. `cost/tokens/latency` 同名映射

## 5.2 缺失字段处理

旧版详情里的：

1. `request_text_preview`
2. `response_text_preview`
3. `prompt_template`

gateway 当前没有独立列，处理方案：

1. 不伪造顶层字段值（默认 `null`）
2. 将可用内容从 `request_metadata/usage_metadata` 原样返回，供前端决定展示策略

---

## 6. 鉴权与安全

沿用现有中间件：

1. `Authorization: Bearer <GATEWAY_AUTH_TOKEN>`
2. `/healthz` 免鉴权
3. 新增 `/v1/audit/*` 同样需要鉴权

安全原则：

1. 只读接口，无写操作
2. SQL 仅参数化查询，禁止拼接动态值
3. `sort_by` 白名单校验（仅允许安全列）

---

## 7. SQL 与存储层设计

新增 `internal/service/audit_query.go`（建议）：

1. `ListRecords(req)`：构造 where + count + page query
2. `GetRecordByRequestID(requestID)`
3. `GetStats(req)`
4. `GetCostSummary(req)`
5. `GetCostByTenant(tenantID, req)`

日期逻辑：

1. `start_date` -> `>= YYYY-MM-DD 00:00:00`
2. `end_date` -> `< YYYY-MM-DD + 1 day 00:00:00`

默认窗口（建议）：

1. 未传日期时默认最近 30 天（防止全表扫描）

新增索引（迁移）：

1. `CREATE INDEX IF NOT EXISTS idx_model_code_created_at ON gateway_call_records(model_code, created_at);`
2. `CREATE INDEX IF NOT EXISTS idx_caller_module_created_at ON gateway_call_records(caller_module, created_at);`
3. `CREATE INDEX IF NOT EXISTS idx_provider_created_at ON gateway_call_records(provider, created_at);`

---

## 8. HTTP 层实现设计

新增文件（建议）：

1. `internal/http/handler_audit.go`
2. `internal/http/dto_audit.go`

路由注册：

1. `GET /v1/audit/records`
2. `GET /v1/audit/records/{request_id}`
3. `GET /v1/audit/records/stats`
4. `GET /v1/audit/cost/summary`
5. `GET /v1/audit/cost/by-tenant/{tenant_id}`

错误码策略：

1. 参数错误 -> `INVALID_ARGUMENT`
2. 未找到 -> `NOT_FOUND`
3. 查询失败 -> `INTERNAL_ERROR`

---

## 9. 性能与缓存

1. 列表接口默认 `size=50`，最大 100
2. 统计接口增加 30 秒内存缓存（key 由筛选参数拼装）
3. 读多写多场景下，SQLite 使用 WAL（现有已配置）

---

## 10. 测试计划

### 10.1 单元测试

1. where 条件拼接正确性
2. 分页边界
3. `success_rate` 计算
4. metadata JSON 解析

### 10.2 集成测试

1. 空库返回
2. 多租户过滤
3. 时间范围过滤
4. trace_id 精确命中
5. 错误 request_id 返回 404

### 10.3 回归测试

1. 推理接口 `/v1/inference/text`、`/v1/inference/audio` 行为不变
2. 审计写入不受查询接口影响

---

## 11. 交付计划（分阶段）

### Phase 1：网关只读 API

1. 新增查询 service + handler + router
2. 补索引迁移
3. 补测试

### Phase 2：业务侧接入

1. `lingce-api` 选择：
1. 直接前端调用 gateway（简单，耦合 token 管理）
2. `lingce-api` 代理 gateway 审计接口（推荐，统一鉴权）
2. 将 LLM 管理页“记录/成本”切到新数据源

### Phase 3：历史收敛（可选）

1. `llm_call_records` 转归历史用途
2. 长周期归档策略（gateway SQLite -> 数据仓库/对象存储）

---

## 12. 风险与规避

1. SQLite 大数据量查询性能下降
1. 规避：默认时间窗 + 强制分页 + 新索引 + 统计缓存
2. 双数据源统计不一致（旧库 vs gateway）
1. 规避：切换期明确“以 gateway 为准”
3. 旧前端字段不完全匹配
1. 规避：先兼容核心字段，缺失字段返回 null + metadata 扩展

---

## 13. 验收标准（DoD）

1. `llm-gateway` 新增 5 个只读审计接口可用
2. 列表/详情/统计/成本接口在本地真实返回数据
3. 关键筛选参数可用：`tenant_id/function_type/module/model_code/success/trace_id/date-range`
4. 接口响应结构满足旧版 LLM 管理页最小兼容
5. 推理链路无回归

---

## 14. 待你确认的决策点

1. 详情主键是否确定使用 `request_id`（推荐）还是继续使用 `id`
2. 未传日期时，是否启用“默认最近 30 天”保护
3. 业务侧最终接入方式：前端直连 gateway，还是 `lingce-api` 代理
4. 是否要求在本期补“租户名称映射”（gateway 当前无租户表）

