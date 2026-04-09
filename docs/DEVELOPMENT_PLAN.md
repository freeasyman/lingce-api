# 开发计划（优先级版）

> 更新日期：2026-04-09  
> 计划周期：2 周（10 个工作日）  
> 目标：优先补齐高影响壳实现端点，形成可上线主链路。

## 一、当前基线

- `/api/v1/*` 端点总数：`376`
- 健康检查端点：`GET /healthz`
- 当前实现统计（基于 `docs/IMPLEMENTATION_AUDIT.json`）：
  - 已实现：`376`
  - 壳实现：`0`

模块现状：

| 模块 | 总端点 | 已实现 | 壳实现 |
|------|--------|--------|--------|
| auth | 9 | 9 | 0 |
| badge | 46 | 46 | 0 |
| content | 74 | 74 | 0 |
| customer | 34 | 34 | 0 |
| department | 5 | 5 | 0 |
| employee | 6 | 6 | 0 |
| organization | 30 | 30 | 0 |
| rbac | 39 | 39 | 0 |
| recording | 78 | 78 | 0 |
| support | 36 | 36 | 0 |
| sysconfig | 19 | 19 | 0 |

## 二、开发优先级（P0/P1/P2）

### P0（先做，阻塞业务主链路）

1. `content` 模块（最高优先）
2. `badge` 模块（第二优先）

### P1（次优先，补齐组织运营能力）

3. `organization` 模块
4. `support` 模块

### P2（增强能力与收尾）

5. `customer` 模块剩余壳实现
6. 全模块测试、文档、稳定性收尾

## 三、按模块的实施顺序

### 1) content（P0）

先后顺序：

1. `topics/hot-topics`（话题主流程）
2. `contents`（CRUD + generate）
3. `publish_tasks`（发布任务主流程）
4. `templates/content_templates`
5. `seeds/insights/geo`

阶段验收：

- 不再返回占位 `TODO` 或空列表固定响应。
- 至少覆盖：列表、详情、创建/更新、核心动作（generate/publish）。
- 与 `llm-gateway`、OSS 的最小闭环跑通（开发环境）。

### 2) badge（P0）

先后顺序：

1. `inspection` 系列
2. `vendor_pool` 系列
3. `smart_badge` 高级能力（history/callback/process）
4. `tenant overview` 与 `me` 状态

阶段验收：

- 工单、巡检、设备池三个链路具备真实数据读写。
- 与 `badge-middleware` 的核心交互可观测（日志 + 错误处理）。

### 3) organization（P1）

先后顺序：

1. `patients` 列表与详情
2. `doctors` 列表与详情
3. 同步与绩效统计
4. 医生-员工映射关系

阶段验收：

- `patients/doctors` 查询链路由数据库驱动，不再返回占位数据。
- 同步接口具备幂等与失败重试策略。

### 4) support（P1）

先后顺序：

1. `data-browser`（表结构/数据/统计）
2. `visits` 查询与统计
3. 导出与清理类接口

阶段验收：

- 数据浏览与访问统计可用于日常排障。
- 高风险操作（truncate/clear）具备权限和安全校验。

### 5) customer（P2）

先后顺序：

1. `momentum-history`
2. `duplicates/merge`
3. `consultation-records/emr-records`
4. `group members/rules`

阶段验收：

- 形成客户高级运营能力闭环（识别、合并、分组规则）。

## 四、两周冲刺排期（建议）

### Week 1

1. Day 1-2：`content/topics + contents`
2. Day 3：`content/publish_tasks`
3. Day 4：`content/templates + content_templates`
4. Day 5：`badge/inspection`

### Week 2

1. Day 6：`badge/vendor_pool`
2. Day 7：`badge/smart_badge advanced + overview/me`
3. Day 8：`organization/patients + doctors`
4. Day 9：`support/data-browser + visits`
5. Day 10：`customer` 收尾 + 全链路回归 + 文档更新

## 五、执行标准（DoD）

每个完成端点必须满足：

1. Handler 不包含 `TODO: Implement` 占位逻辑。
2. Service/Store 落地真实业务或真实外部调用。
3. 返回结构与既有 API 约定一致（成功/分页/错误）。
4. 至少有接口级回归验证（脚本或自动化测试）。
5. `openapi_complete.yaml` 和 `API_ENDPOINTS.md` 已更新。

## 六、风险与依赖

1. 外部依赖：`llm-gateway`、`badge-middleware`、`recording-worker` 可用性。
2. 数据依赖：部分高级统计依赖历史数据质量。
3. 范围膨胀风险：P0 未收敛前，不并行引入新模块需求。

## 七、文档维护约定

路由或实现状态变化后执行：

```bash
cd /Users/yiliiang/Documents/lingce-api
python3 scripts/extract_and_generate_openapi.py
python3 scripts/generate_markdown_docs.py
```

然后提交以下文件：

- `docs/openapi_complete.yaml`
- `docs/API_ENDPOINTS.md`
- `docs/IMPLEMENTATION_AUDIT.json`（若同步更新审计）
- `docs/DEVELOPMENT_PLAN.md`

关联执行文档：

- `docs/SPRINT_TASK_BREAKDOWN.md`（两周端点级任务拆解）
