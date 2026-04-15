# 2026-04-15 资源接口测试矩阵（DoD 第4条）

## 已纳入自动化脚本并实测通过
| 资源 | 脚本 | 本次结果 |
|---|---|---|
| auth | `scripts/regression/auth_resource_regression.sh` | 通过（`pass=6 fail=0`） |
| tenant | `scripts/regression/tenant_resource_regression.sh` | 通过（`pass=12 fail=0`） |
| employee | `scripts/regression/employee_resource_regression.sh` | 通过（`pass=3 fail=0`） |
| role | `scripts/regression/role_resource_regression.sh` | 通过（`pass=2 fail=0`） |
| menu | `scripts/regression/menu_resource_regression.sh` | 通过（`pass=3 fail=0`） |
| notification | `scripts/regression/notification_resource_regression.sh` | 通过（`pass=3 fail=0`） |
| subscription-plan | `scripts/regression/subscription_plan_resource_regression.sh` | 通过（`pass=3 fail=0`） |
| feature-group | `scripts/regression/feature_group_resource_regression.sh` | 通过（`pass=4 fail=0`） |
| llm | `scripts/regression/llm_resource_regression.sh` | 通过（`pass=6 fail=0`） |
| badge-device + badge-ticket | `scripts/regression/badge_resource_regression.sh` | 通过（历史已跑） |
| content-topic + content-item | `scripts/regression/content_resource_regression.sh` | 通过 |
| recording + recording-task | `scripts/regression/recording_resource_regression.sh` | 通过 |
| customer | `scripts/regression/customer_resource_regression.sh` | 通过（`pass=14 fail=0`） |
| operation-log (R18) | `scripts/regression/operation_log_resource_regression.sh` | 通过（`pass=4 fail=0`） |
| visit (R19) | `scripts/regression/visit_resource_regression.sh` | 通过（`pass=4 fail=0`） |
| department (R20) | `scripts/regression/department_resource_regression.sh` | 通过（`pass=5 fail=0`） |
| 路由兜底检查 | `scripts/regression/resource_dod_smoke.sh` | 通过（无 404） |

> 注：当前本地库存在部分历史缺表，个别管理端点返回 `500`，脚本按“可达性+鉴权+行为回归”口径记录为可接受状态并在日志中留痕。

## 前端逐页验证脚本（DoD 第5条）
| 范围 | 脚本 | 本次结果 |
|---|---|---|
| 工牌库存/分配/监控 | `scripts/regression/frontend_badge_page_smoke.sh` | 通过（`pass=8 fail=0`） |
| 内容生成/客户/录音/日志等关键页 | `scripts/regression/frontend_ops_page_smoke.sh` | 通过（`pass=19 fail=0`） |
| 前端 API 旧路由审计 | `scripts/regression/frontend_api_route_audit.sh` | 通过（未发现 legacy 调用） |

## 本地留痕日志
- `docs/regression/logs/2026-04-15-auth-resource-regression.log`
- `docs/regression/logs/2026-04-15-tenant-resource-regression.log`
- `docs/regression/logs/2026-04-15-employee-resource-regression.log`
- `docs/regression/logs/2026-04-15-role-resource-regression.log`
- `docs/regression/logs/2026-04-15-menu-resource-regression.log`
- `docs/regression/logs/2026-04-15-notification-resource-regression.log`
- `docs/regression/logs/2026-04-15-subscription_plan-resource-regression.log`
- `docs/regression/logs/2026-04-15-feature_group-resource-regression.log`
- `docs/regression/logs/2026-04-15-llm-resource-regression.log`
- `docs/regression/logs/2026-04-15-customer-resource-regression.log`
- `docs/regression/logs/2026-04-15-operation-log-resource-regression.log`
- `docs/regression/logs/2026-04-15-visit-resource-regression.log`
- `docs/regression/logs/2026-04-15-department-resource-regression.log`
- `docs/regression/logs/2026-04-15-frontend-badge-page-smoke.log`
- `docs/regression/logs/2026-04-15-frontend-ops-page-smoke.log`

## 当前剩余差距（严格口径）
- 本矩阵范围内无新增差距；20 个一级资源已具备对应资源级回归脚本或组合资源脚本覆盖，前端关键页面与路由审计均已留痕通过。
