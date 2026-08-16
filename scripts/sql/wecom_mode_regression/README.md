# 企业微信三模式回归验证 SQL

更新日期：2026-08-16

## 1. 目的

这组 SQL 用于企业微信三模式从零回归验证时快速核对数据库结果。

适用场景：

1. 自建应用验证
2. 标准应用验证
3. 代开发应用验证

## 2. 文件说明

1. `01-wecom-regression-bindings.sql`
   - 查看绑定结果
2. `02-wecom-regression-installs.sql`
   - 查看企业实例结果
3. `03-wecom-regression-suite-tickets.sql`
   - 查看 suite ticket 结果
4. `04-wecom-regression-message-logs.sql`
   - 查看消息发送结果
5. `05-wecom-regression-directory-members.sql`
   - 查看通讯录同步与预绑定结果

## 3. 使用方式

执行前，先按需要替换 SQL 顶部注释里的筛选条件，例如：

- `corp_id`
- `tenant_id`
- `employee_id`
- `mode`
- `provider_app`

## 4. 推荐顺序

1. 登录/绑定验证后跑 `01`
2. 安装/授权验证后跑 `02`
3. 第三方回调验证后跑 `03`
4. 发消息验证后跑 `04`
5. 通讯录同步验证后跑 `05`

## 5. 说明

这些 SQL 默认是只读查询，不修改数据。
