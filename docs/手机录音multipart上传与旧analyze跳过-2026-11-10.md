# 手机录音 multipart 上传 + 旧 analyze 跳过（2026-11-10）

> 范围：只改 `lingce-api`。未改 Expo/Web/H5，未改 `lingce-worker`，未真实发短信，未碰生产。

## 1. 上传接口契约

`POST /api/v1/recordings/actions/upload`，按 `Content-Type` 分流：

### 1.1 `multipart/form-data`（员工 App 直传）

| 字段 | 必填 | 说明 |
| --- | --- | --- |
| `file` | 是 | 音频文件；支持 `mp3` / `wav` / `m4a`，单文件 ≤ 200MB |
| `duration` | 否 | 客户端录音时长（秒）；兼容 `duration_seconds` |
| `source` | 否 | 默认 `mobile_app`；服务端转小写 |
| `patient_name` | 否 | 写入 `recordings.notes` |
| `tenant_id` | admin 可传 | 非 admin 强制使用 JWT 的 tenant_id |
| `employee_id` | admin 可传 | 非 admin 强制使用 JWT 的 UserID；admin 不传时用 JWT UserID |

鉴权：`Authorization: Bearer <token>`，支持 `admin` / `employee` / `mobile`。
multipart 分支**不自动入队 transcribe**，由 App 随后调用
`POST /api/v1/recordings/{id}/actions/transcribe`；避免 App 的 transcribe 请求撞上
`transcription is already queued`。

成功：HTTP 200

```json
{
  "id": 6370,
  "recording_id": 6370,
  "recording_url": "https://lince.oss-cn-beijing.aliyuncs.com/recordings/4/mobile/2026/11/10/20261110170707_910000.m4a",
  "file_url": "https://lince.oss-cn-beijing.aliyuncs.com/recordings/4/mobile/2026/11/10/20261110170707_910000.m4a",
  "duration": 100,
  "source": "mobile_app",
  "status": "uploaded",
  "message": "上传成功"
}
```

错误：

- 400：缺 `file`、格式不支持、`duration` 非法、multipart 表单无效；
- 413：文件超过 200MB（`{"code":"FILE_TOO_LARGE","message":"录音文件过大，最大支持 200MB"}`）；
- 500：OSS/数据库失败（返回通用可读提示，技术细节只写日志）。

### 1.2 `application/json`（现有契约，保持不变）

继续使用 `CreateRecordingRequest`：`recording_url` + `patient_name` 必填，
行为与之前一致。

## 2. OSS 路径

```text
recordings/{tenant_id}/mobile/{YYYY/MM/DD}/{timestamp}_{nano}{ext}
```

示例：

```text
recordings/4/mobile/2026/11/10/20261110170707_910000.m4a
```

## 3. 旧 analyze 跳过条件

Worker 的真实链路是：

```text
transcribe 完成
  -> MarkTranscriptionCompleted: cleaned_transcription_status=queued
  -> clean 完成
  -> MarkCleaningCompleted: analysis_status=queued
  -> 旧 analyze 认领；没有匹配的 worker_analysis_routes 时
     "no active analysis route" -> analysis_status=failed
```

`lingce-api` 侧新增迁移：

```text
internal/store/migrations/20261110_003_mobile_app_skip_legacy_analysis.sql
```

触发器 `trg_skip_legacy_analysis_for_mobile_app`：

- 只对 `source='mobile_app'` 生效；
- `INSERT`/`UPDATE OF analysis_status` 时，把 `pending/queued/processing/running/failed`
  收敛为 `skipped`；
- 允许 `completed`（后续新链路显式回写时可用）；
- `UPDATE` 时如果 `analysis_status` 值没有变化，直接放行，不重置已存在的
  `failed` 历史记录；
- 其他 `source` 完全不受影响。

效果：转写、clean 继续执行；旧 analyze 不会认领该录音，`analysis_status`
保持 `skipped`，不会变成 `failed`。App/列表按派生 `status` 看到的是
`processing`，不是失败。

## 4. 超时与 nginx

- 全局 `http.Server.ReadTimeout = 30s`，`WriteTimeout = 5m`。
- multipart 上传分支用 `http.ResponseController.SetReadDeadline/SetWriteDeadline`
  只对本次请求放宽到 15 分钟；全局值不变。
- 请求体用 `http.MaxBytesReader` 限制在 200MB + 2MB multipart 余量。
- 仓库内没有 API nginx 配置文件（只有其他仓库的 `patients.nginx.conf` 示例，
  `client_max_body_size 100m`）。**线上 API 域名需要在服务器执行
  `nginx -T | grep client_max_body_size` 确认**；nginx 默认值为 1m，若要支持
  200MB 直传，需要在对应 `location /api/` 或 server 级显式放大（本提交未改服务器）。

## 5. mobile token 访问赢客专家

`/auth/login/mobile` 签发 `type=mobile` 的 token。此前 deal-expert 路由的
`AllowedUserTypes` 只有 `admin/employee`，App 调 `/deal-expert/*` 会 403。

本次放行范围：

- `internal/dealexpert/followup_handler.go`：员工可用业务接口
  （encounters、follow-ups、outcomes、recordings/{id}/analysis、analyze、
  analyses、analysis-actions、overview、my-actions、diagnosis、
  forecast-calibration 等）允许 `admin/employee/mobile`；
- `internal/dealintervention/handler.go`：干预列表/详情/动作允许 mobile；
- `internal/dealexpertpermission/handler.go`：只放行 `GET /deal-expert/access/me`。

保持不放行（仍 admin/employee）：

- `/deal-expert/approval-rules`；
- `/deal-expert/permissions`、`/deal-expert/permissions/{employee_id}`；
- `/deal-expert/push-events`、`/deal-expert/push-events/{id}/send`。

scope 解析不依赖 user type：mobile token 的 `claims.UserID` 是员工 ID，
`claims.TenantID` 是租户 ID，`resolveDealScope` / `VisibilityFor` /
`checkRecordingAccess` 均按员工身份处理。

### 5.1 encounter_id=0 的销售任务访问修复

手机录音可能尚未关联 encounter（tenant 10 陈敏的录音 encounter_id 全为 0，
tenant 4 也有一批 encounter_id=0 的任务）。原逻辑对 `encounterID<=0` 调
`EncounterConsultant(0)`，必然 404，导致咨询师看不了/完不成自己的任务。

修复：

- `GetSalesTask`、`CompleteSalesTask`、`ConfirmSalesTaskDraft`：
  - `encounter_id>0` 仍走 `checkEncounterAccess`；
  - `encounter_id<=0` 走 `checkSalesTaskAccess`：先按关联录音归属校验，
    录音已删除/缺失时回落到任务负责人；不是简单放开 admin。
- `CreateSalesTask`、`CreateOutcome`：
  - `encounter_id>0` 仍走 `checkEncounterAccess`；
  - `encounter_id<=0` 按 `recording_id` 的录音归属校验。
- `CreateSalesTask` 服务层允许 `encounter_id=0`，但仍要求 `recording_id>0`。
- 修正 `sales_tasks` 的幂等冲突目标：从已删除的
  `(tenant_id, encounter_id)` 改为当前唯一索引
  `(tenant_id, recording_id, objective)`，并同步修正冲突后的回查条件。

越权测试：

| 用例 | 结果 |
| --- | --- |
| mobile 91 `GET /deal-expert/follow-ups/181`（encounter_id=0，recording 533 属于 91） | 200 |
| employee 88 `GET /deal-expert/follow-ups/181` | 403 `无权访问该跟进任务` |
| mobile 91 创建 encounter_id=0 任务（recording 6370） | 200，`inserted=true` |
| employee 88 读取/完成该任务 | 403 |
| mobile 91 完成该任务 | 200，状态变 `等待客户` |

测试任务 211 与事件已删除。

## 6. 测试记录（dev，2026-11-10）

环境：本机 dev API `http://127.0.0.1:18080`（已用新构建重启，lingce_dev），Worker 18083。

| 用例 | 结果 |
| --- | --- |
| employee token multipart 上传 100s TTS m4a | 200，recording `1998`，OSS `recordings/4/mobile/2026/11/10/20261110170448_748000.m4a`，`analysis_status=skipped` |
| 1998 transcribe + 轮询 | transcript 429 字，`ts=completed cs=completed` |
| 1998 旧 analyze | `worker_analysis_runs` 为空，`worker_base_audits` 为空，`analysis_status` 仍 `skipped`，未 failed |
| 1998 新 analyze | 200 `status=analyzed`，`consultation_analyses.id=247`，`prompt_version=v0.17`，`model_code=qwen3.8-max` |
| mobile token（tenant 4 廖晓琴）multipart 上传 | 200，recording `6370`，`analysis_status=skipped` |
| 6370 transcribe + 新 analyze | transcript 433 字；新分析 200，`consultation_analyses.id=248`，v0.17 |
| JSON 上传回归 | 200，recording `6369`（测试后已软删除） |
| 缺 file / 不支持格式 / duration 非法 | 400，错误文案可读 |
| 201MB 文件 | 413 `FILE_TOO_LARGE` |
| mobile token：`GET /deal-expert/encounters`（tenant 4/10） | 200 |
| mobile token：`GET /deal-expert/recordings/{id}/analysis`（tenant 4/10） | 200 |
| mobile token：`GET /recordings/{id}/analysis`（tenant 4/10） | 200 |
| mobile token：`GET /deal-expert/access/me` | 200，`data_scope=self`，UserID 91/135 |
| mobile token：analysis-actions GET/POST | 200，`actor_employee_id=91` |
| mobile token：`GET /deal-expert/permissions`、`approval-rules`、`push-events` | 仍 403 |
| 18080 重启后 mobile 91 `GET /deal-expert/encounters` | 200 |
| 18080 重启后 mobile 91 `GET /deal-expert/follow-ups/181`（encounter 0） | 200 |
| 18080 重启后 employee 88 `GET /deal-expert/follow-ups/181` | 403 |
| 18080 重启后 mobile 91 `GET /deal-expert/recordings/1746/analysis`、`GET /recordings/1746/analysis` | 200 |

构建/测试：

```text
go build ./...                                      PASS
go test ./internal/recording/ ./internal/dealexpert/ \
       ./internal/dealexpertpermission/ \
       ./internal/dealintervention/ ./internal/router/   PASS
go test ./...                                       仅 internal/content 预置测试
                                                    billing_test.go 编译失败（与本次无关）
```

## 7. 部署注意

- 目标环境需执行 `lingce-api --migrate-only` 应用
  `20261110_003_mobile_app_skip_legacy_analysis.sql`（dev 已应用并登记
  `api_schema_migrations`）。
- 触发器只阻止 mobile_app 的旧 analyze；Worker 的转写/clean 不变。
- 已存在的 `analysis_status=failed` 历史记录不会重置（例如 recording 1995）。
- 本机 18080 已用新构建重启。注意：本机直接执行重建后的 `bin/lingce-api`
  会被系统 SIGKILL（同名文件运行保护），18080 当前用同一构建的
  `/tmp/lingce-api-final` 运行；`bin/lingce-api` 已更新为同一构建，部署到
  普通服务器不受此影响。
