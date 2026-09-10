# Worker 调用业务 API 链路与接入说明

> 适用范围：`lingce-worker` 分析完成后调用随访中心、电子病历、合规卫士及其他业务 API。  
> 文档维护：Codex  
> 更新时间：2026-09-10

## 1. 目的

本文说明录音分析完成后，Worker 如何调用下游业务 API，以及新增业务时应该复用哪些代码。

业务方不应在 `doctor.go`、`consultant.go`、`therapist.go` 等具体分析流程中直接写 HTTP 调用。所有业务都应接入统一的分析完成回调和业务 API 分发器。

## 2. 总体链路

```text
标准分析 / 重新分析
        ↓
EntryService.ExecuteRecording 或 ReplayRecording
        ↓
Engine.ExecuteRecording
        ↓
分析结果写回 recordings
        ↓
analysisCompletedHook
        ↓
BusinessAPIDispatcher.Dispatch
        ↓
重新读取分析完成后的最终录音数据
        ↓
组装 BusinessAPIInput
        ↓
调用随访、电子病历、合规卫士等业务 API
```

标准分析和重新分析最终都调用同一个 `Engine.ExecuteRecording`，因此业务 API 只接入统一完成回调，就能覆盖两种入口。

## 3. Worker 的统一触发位置

### 3.1 标准分析入口

文件：

```text
lingce-worker/internal/analysis/entry.go
```

函数：

```go
func (s *EntryService) ExecuteRecording(
    ctx context.Context,
    recordingID int64,
    triggerSource string,
) (*PrepareAnalysisResult, error)
```

分析成功并完成结果落库后执行：

```go
s.runAnalysisCompletedHook(ctx, recordingID)
```

### 3.2 重新分析入口

文件相同：

```text
lingce-worker/internal/analysis/entry.go
```

函数：

```go
func (s *EntryService) ReplayRecording(
    ctx context.Context,
    recordingID int64,
    triggerSource string,
) (*PrepareAnalysisResult, error)
```

后台分析成功后执行：

```go
s.runAnalysisCompletedHook(bgCtx, rec.ID)
```

### 3.3 统一完成回调

回调函数类型：

```go
type AnalysisCompletedHook func(context.Context, int64) error
```

回调失败只记录日志，不把已经成功的录音分析改成失败。

装配文件：

```text
lingce-worker/internal/app/bootstrap.go
```

当前装配关系：

```go
businessAPIDispatcher := job.NewBusinessAPIDispatcher(
    params.Logger,
    recordingsRepo,
    lingceAPIClient,
)

analysisEntry := analysis.NewEntryService(
    params.Logger,
    recordingsRepo,
    routeResolver,
    analysisAuditManager,
    analysisEngine,
    businessAPIDispatcher.Dispatch,
)
```

## 4. 业务 API 分发器

文件：

```text
lingce-worker/internal/job/business_api.go
```

核心函数：

```go
func (d *BusinessAPIDispatcher) Dispatch(
    ctx context.Context,
    recordingID int64,
) error
```

该函数负责：

1. 重新读取分析完成后的录音；
2. 校验必要的关联数据；
3. 组装统一的 `BusinessAPIInput`；
4. 按顺序调用已接入的业务 API。

当前调用结构：

```go
input, err := d.buildBusinessAPIInput(ctx, recordingID)
if err != nil {
    return err
}

if err := d.callFollowupAPI(ctx, input); err != nil {
    return err
}

if err := d.callEMRAPI(ctx, input); err != nil {
    return err
}

if err := d.callComplianceAPI(ctx, input); err != nil {
    return err
}
```

后续新增业务，应在这里增加对应的 `callXXXAPI`，不要改动具体分析流程。

## 5. 统一输入数据

结构体：

```go
type BusinessAPIInput struct {
    TenantID          int64
    RecordingID       int64
    EncounterID       int64
    EmployeeID        int64
    CustomerID        int64
    CustomerName      string
    DoctorName        string
    RecordedAt        time.Time
    CleanedTranscript string
}
```

数据组装函数：

```go
func (d *BusinessAPIDispatcher) buildBusinessAPIInput(
    ctx context.Context,
    recordingID int64,
) (*BusinessAPIInput, error)
```

文件：

```text
lingce-worker/internal/job/business_api.go
```

### 5.1 数据来源和要求

| 字段 | 来源或用途 |
|---|---|
| `TenantID` | 录音所属租户 |
| `RecordingID` | 原始录音主键 |
| `EncounterID` | 分析完成后录音关联的就诊主键 |
| `EmployeeID` | 产生或负责该录音的员工主键 |
| `CustomerID` | 分析完成后确定的客户主键 |
| `CustomerName` | 客户主数据 |
| `DoctorName` | Encounter 的医生信息，缺失时按现有仓储规则回退 |
| `RecordedAt` | 原始录音发生时间 |
| `CleanedTranscript` | 清洗后的完整转写，不使用原始转写，不在 Worker 侧截断 |

`EncounterID` 和 `CustomerID` 必须在分析完成后重新读取，不能使用分析开始时内存对象中的旧值。

如果以下数据缺失，分发器应返回错误，不向下游发送不完整请求：

```text
encounter_id
employee_id
customer_id
recorded_at
cleaned_transcript
customer_name
doctor_name
```

## 6. Worker 调用随访 API

### 6.1 请求客户端文件

```text
lingce-worker/internal/gateway/lingce_api.go
```

请求结构：

```go
type GenerateFollowupRequest struct {
    TenantID          int64  `json:"tenant_id"`
    RecordingID       int64  `json:"recording_id"`
    EncounterID       int64  `json:"encounter_id"`
    EmployeeID        int64  `json:"employee_id"`
    CustomerID        int64  `json:"customer_id"`
    CustomerName      string `json:"customer_name"`
    DoctorName        string `json:"doctor_name"`
    RecordedAt        string `json:"recorded_at"`
    CleanedTranscript string `json:"cleaned_transcript"`
}
```

调用函数：

```go
func (c *LingceAPIClient) GenerateFollowup(
    ctx context.Context,
    payload GenerateFollowupRequest,
) error
```

请求地址：

```http
POST /api/v1/followup/tasks/generate
```

请求头：

```http
Content-Type: application/json
X-Internal-Token: <Worker 与 lingce-api 共享的内部令牌>
```

Worker 只负责：

1. 序列化请求；
2. 发起 HTTP 调用；
3. 检查返回状态是否为 2xx。

Worker 不等待随访任务生成结果，也不解析生成了多少任务。随访 API 返回 2xx，只表示请求已经被接收。

### 6.2 随访分发函数

文件：

```text
lingce-worker/internal/job/business_api.go
```

函数：

```go
func (d *BusinessAPIDispatcher) callFollowupAPI(
    ctx context.Context,
    input *BusinessAPIInput,
) error
```

它把 `BusinessAPIInput` 映射为 `GenerateFollowupRequest`，然后调用 `LingceAPIClient.GenerateFollowup`。

## 7. lingce-api 的随访 API 入口

### 7.1 路由和 HTTP Handler

文件：

```text
lingce-api/internal/followup/handler.go
```

路由：

```http
POST /api/v1/followup/tasks/generate
```

入口函数：

```go
func (h *Handler) Generate(
    w http.ResponseWriter,
    r *http.Request,
)
```

Handler 负责：

1. 解析 JSON 请求体；
2. 校验必填字段；
3. 校验租户、录音、Encounter、员工、客户之间的数据库关联；
4. 调用 `Service.AcceptRequest`；
5. 立即返回接收确认。

### 7.2 随访业务服务

文件：

```text
lingce-api/internal/followup/service.go
```

入口函数：

```go
func (s *Service) AcceptRequest(
    ctx context.Context,
    req GenerateRequest,
) (*GenerateResponse, error)
```

处理过程：

```text
校验引用
→ 写入 followup_generation_runs
→ 返回 ACCEPTED
→ 后台读取数据库提示词
→ 调用 LLM 网关
→ 解析 JSON 任务
→ 写入 recording_tasks
→ 保存运行结果
```

随访 API 的异步处理、失败记录和补偿不属于 Worker 的职责。

## 8. 新增其他业务 API 的步骤

以电子病历为例，新增业务需要修改以下位置。

### 第一步：扩展 Worker 的 HTTP 客户端

文件：

```text
lingce-worker/internal/gateway/lingce_api.go
```

新增：

```go
type GenerateEMRRequest struct {
    // 使用该业务 API 契约要求的字段。
}

func (c *LingceAPIClient) GenerateEMR(
    ctx context.Context,
    payload GenerateEMRRequest,
) error {
    // JSON 序列化；
    // POST 到 lingce-api 的电子病历内部接口；
    // 设置 Content-Type 和 X-Internal-Token；
    // 检查 2xx；
    // 非 2xx 返回错误。
}
```

不要在这个函数中读取数据库。输入资料已经由 `BusinessAPIDispatcher` 统一准备。

### 第二步：扩展业务客户端接口

当前随访客户端接口为：

```go
type FollowupAPI interface {
    GenerateFollowup(
        context.Context,
        GenerateFollowupRequest,
    ) error
}
```

如果多个业务由同一个客户端统一调用，应将其扩展为统一业务客户端接口，或增加独立的接口类型。例如：

```go
type BusinessAPIClient interface {
    GenerateFollowup(context.Context, GenerateFollowupRequest) error
    GenerateEMR(context.Context, GenerateEMRRequest) error
    GenerateCompliance(context.Context, GenerateComplianceRequest) error
}
```

然后让 `LingceAPIClient` 实现该接口，并将 `BusinessAPIDispatcher` 的字段类型改为统一接口。

### 第三步：实现分发器函数

文件：

```text
lingce-worker/internal/job/business_api.go
```

实现：

```go
func (d *BusinessAPIDispatcher) callEMRAPI(
    ctx context.Context,
    input *BusinessAPIInput,
) error
```

该函数只做两件事：

1. 把统一的 `BusinessAPIInput` 映射成电子病历请求体；
2. 调用客户端的 `GenerateEMR`。

不要重新查询录音、客户、员工或 Encounter。

### 第四步：在 Dispatch 中启用

在以下函数中加入调用：

```go
func (d *BusinessAPIDispatcher) Dispatch(...)
```

是否启用某项业务由产品和配置决定。启用后，该业务 API 的失败处理应明确：

- 如果业务 API 只返回接收确认，调用失败应记录日志；
- 不应把已经成功的录音分析改成失败；
- 业务自身需要有自己的运行记录或补偿机制。

### 第五步：在 lingce-api 新增业务模块

建议目录结构：

```text
lingce-api/internal/<business>/
    dto.go
    handler.go
    service.go
    module.go
```

职责划分：

| 文件 | 职责 |
|---|---|
| `dto.go` | 请求和响应结构 |
| `handler.go` | HTTP 路由、字段校验、鉴权入口 |
| `service.go` | 数据库处理、LLM 调用、业务逻辑 |
| `module.go` | 服务和路由装配 |

新增业务 API 应使用内部服务鉴权，并保持“快速确认接收、后台异步处理”的方式。

## 9. 配置和装配

Worker 客户端创建位置：

```text
lingce-worker/internal/app/bootstrap.go
```

当前使用：

```go
lingceAPIClient := gateway.NewLingceAPIClient(
    params.Config.LingceAPIURL,
    params.Config.InternalToken,
    params.Logger,
)
```

新增业务默认复用：

```text
LingceAPIURL
InternalToken
```

不应为每个业务重复创建基础 HTTP 客户端和鉴权配置，除非业务确实需要不同的服务地址或凭证。

## 10. 接入检查清单

新增业务提交前检查：

- [ ] 触发位置仍然是统一 `analysisCompletedHook`；
- [ ] 没有在具体分析流程中直接写 HTTP 调用；
- [ ] 使用 `BusinessAPIInput` 的统一数据；
- [ ] 使用分析完成后重新读取的 `encounter_id`、`customer_id`；
- [ ] 使用 `cleaned_transcript`，没有误用原始转写；
- [ ] Worker 请求带有 `X-Internal-Token`；
- [ ] Worker 只把 2xx 当作“已接收”；
- [ ] 业务 API 有独立的 Handler、Service 和 DTO；
- [ ] 业务 API 自己记录处理状态和失败原因；
- [ ] 业务 API 的 LLM 调用和补偿不阻塞 Worker 的 HTTP 请求；
- [ ] 标准分析和 Replay 都能触发该业务；
- [ ] 单元测试覆盖请求组装、非 2xx 响应和缺失关联数据。

