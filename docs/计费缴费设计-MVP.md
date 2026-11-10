# 计费缴费系统 · MVP 最小方案

编写日期：2026-11-10
状态：**定稿**（2026-11-10 与业务方逐条确认）
原则：**系统只做"额度 + 扣量 + 订单"，其余全部人肉。**

```text
已定规则一览：
· 赠送 = 挂包：≤1 个增量包运营直接挂，>1 个老板点头；系统自动算金额
· 正式额度结转一次：到期冻结 → 续费解冻接着用 → 第二周期结束清零
· 试用额度到期清零：不冻结、不结转、不退款
· 扣量顺序：结转 → 试用 → 正式
· 试用包默认规格：任一单品 7 天，额度 = 年额度 × 7/365（先这么做，向上取整）
```
（完整演进版见同目录《计费缴费设计.md》，MVP 不实现它的审批流/对账/纠正/异议等系统功能。）

---

## 1. 只建 4 张表

### 1.1 `product_entitlements` 产品额度（客户还剩多少）

```text
tenant_id / product_code / unit（hour/record/round/shot/prompt/person）
edition（standard/light/trial）
quota_type（official 正式 / trial 试用）
granted / used / balance（= rollover_bal + granted − used）
rollover_bal（结转余额：续费时从上期 balance 挪入，优先扣，第二周期清零）
period_end（统一到期日）
```

```text
一个产品一行一个单位（听写病历 = 3 行：份/轮/小时）。
扣量顺序：结转 → 试用 → 正式。
```

### 1.2 `entitlement_ledger` 额度流水（发和扣都记这里，一张表）

```text
tenant_id / product_code / unit
type        grant（发额度）/ usage（扣量）
amount      正数（grant）或负数（usage）
source      purchase / gift / trial / rollover / correction
reason      原因（"回款确认按单下发" / "赠送：老板同意" / "交付扣量"）
linked_order_id / idempotency_key（幂等：重试不重复扣）
operator    操作人（人工）或"系统"
created_at
```

```text
balance 始终 = Σgrant（含结转）− Σusage，改余额只许写 ledger，禁止直接 UPDATE balance。
```

### 1.3 `billing_orders` 订单（支付信息合并进来）

```text
order_no / tenant_id
order_type   single / bundle / increment / hardware / gift / trial
items        jsonb：产品、档位、单位、数量、价格
total_amount / payable_amount（gift 和 trial 为 0）
source       direct / channel
salesman_id / channel_id
price_list_version（价目表版本，对账用）
paid_amount / paid_at / confirmed_by（财务确认人）
status       created / paid / settled / refunded / cancelled
reason       赠送原因（gift 必填）
approved_by  赠送同意人（仅 >1 个增量包时老板点头，≤1 包运营直接挂）
created_at
```

```text
支付：全款一次。客户转账截图 → 财务在订单上点"回款确认"（confirmed_by 记名字）。
不接支付网关，不建支付表。
```

### 1.4 `product_catalog` 价目表（开通和赠送都靠它）

```text
product_code + edition（standard/light/trial）
→ 各单位的额度、年价、增量包规格

例：听写病历 standard = 2,500 份 + 5,000 轮 + 400 小时，年价 21,600，增量包 2,600
    听写病历 trial    = 额度 × 7/365，年价 0

作用：
· 开通时选"产品 + 档位"，系统自动带出额度和价格，运营不用手填
· 赠送录"500 份病历"，系统自动换算"相当于 2,600 元"
· 新增档位（如轻量版）= 加一行 + 版本号，不动存量数据
```

---

## 2. 四个流程

### 2.1 购买与开通（全人工，但"开通"是明确的一步）

```text
销售报价（官方报价单）→ 下单（订单表录一笔，选好产品 + 档位）
→ 客户转账 → 财务回款确认
→ 运营"开通"（后台一个动作，两步）：
   ① 开通租户：建立服务期（首个产品开通日 + 12 个月 = 统一到期日）
   ② 按档位下发额度：选"产品 + 档位"→ 系统从价目表自动带出额度和价格
      → 写 ledger（grant，到期日 = 统一到期日）
→ 客户可用
```

```text
中途加购（增量/后买产品）：Excel 按"剩余天数 ÷ 365"算好价格和额度，运营照录。
开通时选档位；试用 = trial 档位，按 2.3 挂包方式下发，到期按 2.4 清零。
```

### 2.2 交付扣量（唯一自动化的地方）

```text
交付成功 → 写 ledger（usage，负数）→ 更新 balance
80% 用完提醒 / 100% 暂停交付入口
失败/草稿不扣；重试不重复扣（幂等键）
```

### 2.3 赠送（按"包/份/张"说，系统自动算钱）

```text
销售对客户说："我送你 500 份病历。"（说额度，不说钱）
运营在后台录入：产品 + 档位 + 数量（如 500 份，或直接选"1 个听写增量包"）
系统按价目表自动显示："相当于 2,600 元" → 销售/客户一眼看懂价值
≤ 1 个增量包：运营直接挂（订单记 reason + 操作人）
> 1 个增量包：老板点头（订单记 approved_by）
→ 运营写 ledger（grant，source=gift，reason 必填）
```

### 2.4 续费与结转（人工，规则：结转一次）

```text
到期不续费 → 余额清零（ledger 记 expired）

到期续费 → 三步：
  ① 冻结的余额变"结转"：rollover_bal = 上期 balance
  ② 发新年度额度（grant，正常下发）
  ③ 扣量先扣 rollover_bal，再扣试用，再扣正式
  第二个周期结束时 rollover_bal 没用完 → 清零，不再结转

试用额度（trial）：到期直接清零，不冻结、不结转。
```

---

## 3. MVP 明确不做的（全部人肉）

| 不做 | MVP 替代办法 |
|---|---|
| 审批系统 | 微信/口头，订单记 approved_by |
| 对账任务 | 月底导出 ledger 用 Excel 对 |
| 纠正/异议流程 | 运营直接写 ledger 纠正，留 reason |
| 权限矩阵 | 一个运营后台，操作人记名字 |
| 渠道佣金结算 | Excel 算 |
| 发票系统 | 人工开票 |
| 报价计算器 | 销售速查里的公式 / Excel |
| 销售客户额度视图 | 运营帮查 |
| 支付网关 | 转账截图 + 财务人工确认 |
| 分级审批/状态机/SLA 系统 | 口头承诺 |

---

## 4. 前端最小

```text
客户侧：剩余额度页（每产品还剩多少、到期日、用量明细）—— 读 product_entitlements + ledger
运营侧：一个简单后台 —— 订单列表、录订单、回款确认、**开通租户（建服务期 + 按档位发额度）**、录赠送、查任意客户余额
```

---

## 5. 上线顺序

```text
① 四张表 + 交付扣量（自动）+ 剩余额度页
② 价目表 + 订单录入 + 回款确认 + 开通（建服务期 + 按档位发额度）
③ 赠送录入（自动算金额）+ 100% 暂停
④ 到期/结转的人工操作入口
```

---

## 6. 遗留问题（都不阻塞 MVP）

```text
已定（按 2026-11-10 讨论执行）：
· 赠送阈值：≤1 个增量包直接挂，>1 个老板点头
· 试用包默认规格：任一单品 7 天，额度 = 年额度 × 7/365（先这么做）
· 扣量顺序：结转 → 试用 → 正式
· 正式额度结转一次；试用额度到期清零

仍待定：
· 轻量档（light edition）的规格与价格：将来再说，价目表已留字段
· 渠道佣金、发票税率：Excel/人工，不建系统
```
