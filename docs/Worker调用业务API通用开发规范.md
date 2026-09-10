# Worker 调用业务 API 通用开发规范

**版本：** v1  
**适用范围：** 由 `lingce-worker` 完成录音分析，再交给具体业务模块继续处理的场景  
**示例业务：** 随访任务生成  
**编写人：** Codex  
**编写时间：** 2026-09-10

## 1. 这套模式解决什么问题

Worker 负责完成通用的录音处理链路：

1. 读取录音；
2. 转写；
3. 清洗转写；
4. 生成或获得 `Encounter`；
5. 解析出可关联的租户、录音、员工、客户等客观 ID。

业务模块不把自己的规则、提示词和数据库处理塞进 Worker，而是提供一个独立的业务 API。Worker 在资料准备完成后调用这个 API，把原始业务输入一次性传过去。业务 API 收到后立即确认，再由业务 API 自己完成提示词读取、LLM 调用、结果解析和业务入库。

数据流：

```text
Worker 完成清洗转写
        ↓
Worker 调用业务 API
        ↓
业务 API 校验请求并立即返回“已接收”
        ↓
业务 API 自己读取提示词和模型配置
        ↓
业务 API 调用 LLM 网关
        ↓
业务 API 解析结果并写入自己的业务表
```

## 2. Worker 在什么环节调用

调用时机必须满足以下条件：

1. 本次录音的清洗后转写已经完成；
2. `Encounter ID` 已经生成或已经能够确定；
3. `Customer ID` 和客户姓名已经能够确定；
4. 录音所属租户和员工已经能够确定；
5. Worker 已经拿到本次录音的最终 `recorded_at`。

调用位置应放在“分析主流程完成、清洗后转写和关联 ID 已经落库之后”，而不是：

- 原始转写刚完成时；
- LLM 分析尚未完成时；
- 只有录音 ID、还没有客户或 Encounter 时；
- 前端轮询接口中。

Worker 调用业务 API 只等待 HTTP 接收确认，不等待业务 API 的 LLM 结果。

## 3. Worker 请求业务 API 的标准方式

### 3.1 HTTP

```http
POST /api/v1/{business}/...
Content-Type: application/json
X-Internal-Token: <内部服务令牌>
```

业务 API 使用内部服务认证，不使用普通用户 JWT。当前系统的内部认证头是：

```text
X-Internal-Token
```

### 3.2 请求体设计原则

请求体只传业务 API 完成工作所需的客观输入，不传以下内容：

- Worker 内部的数据库连接信息；
- LLM 网关地址或密钥；
- 业务模块的系统提示词；
- 业务模块的用户提示词；
- 业务模块的模型参数；
- 业务模块的生成结果字段。

Worker 传“事实和素材”，业务 API 决定如何处理这些素材。

### 3.3 随访业务请求示例

```json
{
  "tenant_id": 5,
  "recording_id": 1710,
  "encounter_id": 24,
  "employee_id": 104,
  "doctor_name": "胡欣",
  "customer_id": 8828,
  "customer_name": "患者姓名",
  "recorded_at": "2026-05-28T14:04:38+08:00",
  "cleaned_transcript": "一次 Encounter 的完整清洗后转写文本"
}
```

### 3.4 字段定义

| 字段 | 类型 | 必填 | 说明 |
|---|---|---:|---|
| `tenant_id` | integer | 是 | 当前租户 ID |
| `recording_id` | integer | 是 | 原始录音 ID |
| `encounter_id` | integer | 是 | 本次就诊 ID |
| `employee_id` | integer | 是 | 录音所属员工 ID |
| `doctor_name` | string | 是 | 本次接诊医生姓名；业务话术可使用，但不能冒充医生本人 |
| `customer_id` | integer | 是 | 客户表中的客户 ID |
| `customer_name` | string | 是 | 客户姓名 |
| `recorded_at` | string | 是 | 录音发生时间，建议使用带时区的 RFC3339 |
| `cleaned_transcript` | string | 是 | Worker 清洗后的完整转写；不得传原始转写替代它 |

`cleaned_transcript` 是业务 API 的主要素材。Worker 不应把摘要、截断片段或原始转写误当成清洗后的完整转写。

## 4. 业务 API 必须如何接收

### 4.1 接收层职责

HTTP Handler 只负责：

1. 解析 JSON；
2. 校验必填字段和 ID 是否为正数；
3. 调用业务 Service；
4. 返回接收确认或错误。

不要在 Handler 中直接写 LLM 调用、提示词拼接或复杂数据库业务逻辑。

### 4.2 引用校验

业务 API 至少应校验：

1. `tenant_id` 存在；
2. `recording_id` 存在且属于该租户；
3. `encounter_id` 存在且与该录音属于同一租户和同一业务链路；
4. `employee_id` 存在且属于该租户；
5. `customer_id` 存在且属于该租户；
6. 录音表中的员工、客户和 Encounter 与请求体一致。

引用校验失败时，不启动 LLM 调用。

### 4.3 Service 接收后的职责

Service 接收到合法请求后：

1. 读取业务提示词；
2. 读取租户和全局配置；
3. 拼接系统提示词和用户提示词；
4. 启动后续业务处理；
5. 立即向 Worker 返回接收确认。

当前随访 API 的处理模型是“同步校验 + 后台处理”：

```text
同步：请求解析、字段校验、引用校验、提示词是否存在
异步：LLM 调用、结果解析、任务入库、业务日志
```

## 5. 返回给 Worker 的内容

### 5.1 成功返回

成功只表示业务 API 已经接收请求并开始处理，不表示 LLM 已经完成，也不表示业务数据已经入库。

当前随访接口返回示例：

```json
{
  "code": "ACCEPTED",
  "status": "accepted",
  "message": "received",
  "request_id": "followup-1788924562186801000",
  "tenant_id": 5,
  "recording_id": 1710,
  "encounter_id": 24,
  "employee_id": 104,
  "customer_id": 8828,
  "customer_name": "患者姓名",
  "llm_status": "accepted"
}
```

必须返回的客观关联字段：

- `request_id`
- `tenant_id`
- `recording_id`
- `encounter_id`
- `employee_id`
- `customer_id`

这些字段用于日志检索和链路排查。

当前通用 HTTP 响应工具返回 HTTP `200`。如果后续统一改造为异步接收语义，可以使用 HTTP `202 Accepted`，但必须同步更新调用方和契约。

### 5.2 请求格式错误

```http
400 Bad Request
```

示例：

```json
{
  "code": "BAD_REQUEST",
  "message": "cleaned_transcript is required"
}
```

适用情况：

- JSON 无法解析；
- 必填字段缺失；
- ID 小于等于 0；
- 文本为空。

### 5.3 引用无效

```http
422 Unprocessable Entity
```

示例：

```json
{
  "code": "INVALID_REFERENCE",
  "message": "invalid references: tenant_id=5, recording_id=1710, encounter_id=24, employee_id=104, customer_id=8828"
}
```

适用情况：

- ID 不存在；
- ID 不属于当前租户；
- 录音、Encounter、员工、客户之间的关系不一致。

### 5.4 认证失败

```http
401 Unauthorized
```

适用情况：

- 缺少 `X-Internal-Token`；
- 内部令牌错误。

### 5.5 服务端无法接收

```http
500 Internal Server Error
```

适用情况：

- 数据库不可用；
- 全局提示词不存在或未启用；
- 租户配置查询失败；
- 业务服务无法启动后续处理。

此时 Worker 可以记录失败并按调用方的重试策略处理，但不要把 400/422 当成网络重试错误。

## 6. 业务 API 如何读取提示词

业务 API 不在代码中硬编码提示词，也不在运行时读取项目目录中的文本文件。

统一使用系统已有提示词管理：

```text
recording_analysis_prompts
```

全局提示词记录至少包含：

```text
code
name
description
category
system_prompt
user_prompt_template
output_schema
version
is_active
usage_count
```

业务 API 为自己的功能定义固定 `code`，例如随访使用：

```text
followup_task_generation
```

运行时读取：

```text
system_prompt
user_prompt_template
version
```

动态字段填入 `user_prompt_template`。模板变量必须由业务 API 明确定义，例如：

```text
{{tenant_id}}
{{recording_id}}
{{encounter_id}}
{{employee_id}}
{{doctor_name}}
{{customer_id}}
{{customer_name}}
{{recorded_at}}
{{cleaned_transcript}}
```

`output_schema` 用于保存期望的 JSON 输出结构，便于提示词管理、测试和文档展示。业务 API 仍然必须在 Go 代码中对大模型输出做实际 JSON 和业务字段校验。

## 7. 租户提示词配置

业务 API沿用现有租户配置机制，不新建提示词表：

```text
recording_analysis_tenant_configs
```

读取规则：

```text
全局 recording_analysis_prompts
        ↓
当前 tenant_id 的启用配置
        ↓
非空字段覆盖全局对应字段
```

现有运行逻辑支持以下字段：

- `custom_system_prompt`：非空时覆盖全局 `system_prompt`；
- `custom_user_prompt_template`：非空时覆盖全局 `user_prompt_template`；
- `custom_output_schema`：非空时覆盖全局 `output_schema`；
- `additional_instructions`：追加到系统提示词末尾。

如果没有当前租户的启用配置，则使用全局提示词。全局提示词不存在或未启用时，业务 API 应报错，不应回退到代码内置文本。

## 8. LLM 调用要求

业务 API 必须调用内部 LLM 网关，不直接调用 DashScope 或其他厂商接口。

模型选择从系统配置读取：

```text
llm_model_configs
```

查询优先级：

```text
当前租户 + function_type
        ↓ 没有
全局 tenant_id = 0 + function_type
```

业务方需要定义自己的 `function_type`，例如：

```text
task_script_generation
```

调用网关时必须传完整的网关契约字段，包括：

- `tenant_id`
- `caller_service`
- `caller_module`
- `trace_id`
- `function_type`
- `provider`
- `model_code`
- `messages`
- `params`
- 业务计费元数据（如适用）

不要在业务代码中把模型名、Provider 或模型参数写死为某个值。模型配置由后台和 `llm_model_configs` 管理。

## 9. 业务结果处理

LLM 返回后，业务 API 自己负责：

1. 解析 JSON；
2. 校验必填字段；
3. 校验业务字段格式；
4. 保存原始返回内容；
5. 持久化业务结果；
6. 写入可检索日志。

以随访为例：

```text
LLM 返回 tasks
        ↓
校验 title、contact_time、script 等字段
        ↓
按业务规则处理旧任务
        ↓
写入 recording_tasks
        ↓
记录 generation_prompt_version 和 generation_model
```

业务结果失败不再回传给已经收到确认的 Worker。业务 API 应通过自身日志、监控和业务表状态排查。

## 10. 日志和链路追踪

每次请求至少记录：

- `request_id`
- `tenant_id`
- `recording_id`
- `encounter_id`
- `employee_id`
- `customer_id`
- `prompt_code`
- 实际使用的提示词版本
- `function_type`
- Provider 和模型代码
- LLM 请求 ID
- LLM 返回或错误
- 业务结果保存错误

日志中不得只写“调用失败”，必须能够根据 `recording_id` 或 `request_id` 找到完整链路。

推荐日志链路：

```text
Worker 请求
  → 业务 API request_id
  → LLM trace_id
  → LLM request_id
  → 业务结果记录
```

## 11. 业务 API 的推荐代码结构

每个业务模块至少拆成以下层次：

```text
internal/{business}/
├── dto.go       请求和返回结构
├── handler.go   HTTP 接收、字段校验、返回确认
├── service.go   业务编排、提示词读取、LLM 调用、结果处理
└── module.go    模块装配和路由注册
```

职责边界：

| 文件 | 负责内容 |
|---|---|
| `dto.go` | 请求体、响应体、业务结果结构 |
| `handler.go` | HTTP 解析、必填字段校验、调用 Service |
| `service.go` | 引用校验、提示词读取、模板渲染、LLM 调用、业务入库 |
| `module.go` | 创建 Service/Handler、注册路由 |

Handler 不应包含裸 SQL、LLM 网关调用或大段提示词。

## 12. Worker 侧的最小调用代码逻辑

Worker 侧应遵循以下顺序：

```text
1. engine.ExecuteRecording() 完成；
2. 清洗后转写已经确定；
3. Encounter、Customer、Employee 等 ID 已确定；
4. 组装业务 API 请求；
5. 设置 X-Internal-Token；
6. 设置较短的 HTTP 请求超时；
7. 发送请求；
8. 收到 ACCEPTED 后记录 request_id；
9. 不等待业务 API 的 LLM 结果。
```

Worker 不应：

- 自己读取业务提示词；
- 自己调用业务 LLM；
- 等待业务任务生成完成；
- 根据 HTTP 成功响应推断业务数据已经入库；
- 把原始转写替代清洗后转写发送。

## 13. 重试和幂等边界

当前接口的成功响应只代表“已接收”。网络超时可能导致 Worker 不知道业务 API 是否已经收到请求，因此调用方必须记录请求日志。

推荐规则：

- `400`、`401`、`403`、`422`：不自动重试，先修正请求或权限；
- `500`、连接失败、网关超时：按 Worker 的通用重试策略处理；
- 业务 API 自己负责结果替换、去重或幂等；
- 如果业务需要严格防止重复处理，应在请求契约中额外增加幂等键，并由业务 API 落库约束。

当前随访接口使用录音和任务业务规则处理重复生成，其他业务不能直接假设拥有同样的去重规则，必须在自己的业务文档中明确。

## 14. 新业务接入清单

业务开发方开始编码前，必须先确定：

- [ ] 业务 API 路径；
- [ ] 内部认证方式；
- [ ] Worker 调用的准确时机；
- [ ] 请求体字段及字段来源；
- [ ] 哪些字段是必填；
- [ ] 引用关系如何校验；
- [ ] 业务提示词 `code`；
- [ ] 全局提示词内容；
- [ ] 动态用户提示词模板；
- [ ] `output_schema`；
- [ ] LLM `function_type`；
- [ ] 租户配置是否允许覆盖；
- [ ] 业务结果写入哪张表；
- [ ] 重试和去重规则；
- [ ] 日志和链路字段；
- [ ] API 返回的 HTTP 状态码和 `code`。

## 15. 随访接口作为参考实现

当前参考实现：

```text
接口：
POST /api/v1/followup/tasks/generate

请求接收：
lingce-api/internal/followup/handler.go

业务处理：
lingce-api/internal/followup/service.go

请求结构：
lingce-api/internal/followup/dto.go

路由装配：
lingce-api/internal/followup/module.go

全局提示词：
recording_analysis_prompts

租户覆盖：
recording_analysis_tenant_configs

模型配置：
llm_model_configs
```

其他业务应复用这套“Worker 传事实、业务 API 自己处理”的边界，不要复制随访的业务字段、表名或任务规则。
