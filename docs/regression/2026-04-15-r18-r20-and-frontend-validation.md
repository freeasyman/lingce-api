# 2026-04-15 R18/R19/R20 与前端逐页验收留痕

## 范围
- DoD 第4条（接口测试）补齐：R18 `operation-log`、R19 `visit`、R20 `department`。
- DoD 第5条（前端页面）逐页验证：工牌库存/分配/监控、内容生成、客户标签等关键页面。
- 前端联调收口确认：`apps/operation` 运行实例未发现运行时代码中的 `/api/v2/badges/*` 调用。

## 执行时间
- 2026-04-15

## 结果摘要
- `frontend_badge_page_smoke.sh`：`pass=8 fail=0`
- `frontend_ops_page_smoke.sh`：`pass=19 fail=0`
- `operation_log_resource_regression.sh`：`pass=4 fail=0`
- `visit_resource_regression.sh`：`pass=4 fail=0`
- `department_resource_regression.sh`：`pass=5 fail=0`

## 前端关键页面验收
- 工牌库存：`/badges-v2/inventory` 通过
- 工牌分配：`/badges-v2/assignment` 通过
- 工牌监控：`/badges-v2/monitoring` 通过
- 内容生成：`/content/article/generate` 通过
- 客户标签关联页：`/customers`、`/customers/segments` 通过

## 代理 API 验证（3004）
- 客户标签：`/api/v1/customers/tags?...` 返回 `200`
- 内容提示词：`/api/v1/content-items/prompts?...` 返回 `200`
- 录音列表：`/api/v1/recordings?...` 返回 `200`
- 操作日志：`/api/v1/operation-logs?...` 返回 `403`（当前 employee 角色预期）
- 就诊记录：`/api/v1/visits?...` 返回 `200`
- 科室：`/api/v1/departments?...` 返回 `200`

## 留痕日志
- `docs/regression/logs/2026-04-15-frontend-badge-page-smoke.log`
- `docs/regression/logs/2026-04-15-frontend-ops-page-smoke.log`
- `docs/regression/logs/2026-04-15-operation-log-resource-regression.log`
- `docs/regression/logs/2026-04-15-visit-resource-regression.log`
- `docs/regression/logs/2026-04-15-department-resource-regression.log`
- `docs/regression/logs/2026-04-15-customer-resource-regression.log`

## 关联矩阵
- `docs/regression/2026-04-15-resource-test-matrix.md`
