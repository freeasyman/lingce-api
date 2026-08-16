# 企业微信三模式运行态重置 SQL

更新日期：2026-08-16

## 1. 目的

这组 SQL 用于执行企业微信三模式重构前的运行态备份与清理。

本次策略：

- 保留 `tenant_wecom_apps`
- 清空企业微信运行态
- 不动业务主数据
- 不默认清理消息日志和事件日志

## 2. 文件说明

1. `01-wecom-runtime-backup.sql`
   - 在数据库内创建备份 schema
   - 备份运行态表和相关配置表

2. `02-wecom-runtime-reset.sql`
   - 清空运行态表

3. `03-wecom-runtime-postcheck.sql`
   - 检查清空结果

## 3. 清空范围

本次清空：

- `wecom_user_bindings`
- `wecom_corp_installs`
- `wecom_suite_tickets`
- `wecom_directory_members`

本次保留：

- `tenant_wecom_apps`
- `wecom_event_logs`
- `wecom_message_logs`

## 4. 推荐执行顺序

1. 先执行 `01-wecom-runtime-backup.sql`
2. 确认备份表已生成
3. 再执行 `02-wecom-runtime-reset.sql`
4. 最后执行 `03-wecom-runtime-postcheck.sql`

## 5. 示例

```bash
psql "$DATABASE_URL" -f scripts/sql/wecom_mode_reset/01-wecom-runtime-backup.sql
psql "$DATABASE_URL" -f scripts/sql/wecom_mode_reset/02-wecom-runtime-reset.sql
psql "$DATABASE_URL" -f scripts/sql/wecom_mode_reset/03-wecom-runtime-postcheck.sql
```

## 6. 注意事项

1. 先备份，再清空。
2. 这组脚本不会删除表结构，只删除运行态数据。
3. 清理完成后，应立即进入兼容代码删除和三模式从零验证。
