# 企业微信三模式审计脚本

更新时间：2026-08-16

本目录用于企业微信三模式重构前的数据审计。

目标：

1. 审计 `wecom_user_bindings`
2. 审计 `wecom_corp_installs`
3. 审计 `wecom_suite_tickets`
4. 审计 `tenant_wecom_apps`
5. 识别会阻塞删除历史兼容代码的脏数据

## 文件说明

- `01-wecom-user-bindings-audit.sql`
  - 审计绑定表的模式字段、provider_app、重复绑定、空值、跨租户风险
- `02-wecom-corp-installs-audit.sql`
  - 审计企业安装实例的模式字段、provider_app、tenant_id、重复安装、空关键字段
- `03-wecom-suite-tickets-audit.sql`
  - 审计 suite ticket 的 mode/provider_app 结构完整性
- `04-tenant-wecom-apps-audit.sql`
  - 审计自建应用配置表的租户配置完整性
- `05-wecom-cross-table-audit.sql`
  - 做跨表一致性检查，识别绑定、安装、租户配置之间的不一致

## 运行方式

使用 `psql` 执行，例如：

```bash
psql "$DATABASE_URL" -f scripts/sql/wecom_mode_audit/01-wecom-user-bindings-audit.sql
```

建议依次执行 1 到 5，保留每个脚本的输出结果。

## 输出用途

本目录的脚本输出将用于生成：

- 企业微信三模式审计结果文档
- 企业微信三模式正式迁移方案
- 删除历史兼容代码前的阻塞项清单
