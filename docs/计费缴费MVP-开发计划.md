# 计费缴费 MVP · 开发计划

编写日期：2026-11-10
依据：《计费缴费设计-MVP.md》（定稿）
涉及仓库：`lingce-api`（后端）、`lingce-web`（前端）
前端落点：医院侧 `apps/institution`（机构端）、运营侧 `apps/operation-console`（新运营端，不用旧 apps/operation）

> 每条任务 = 目标 / 改动点 / 验收标准 / 依赖 / 估时。估时按 1 名全栈 + Codex 辅助。

---

## 0. 范围声明

```text
做：4 张新表、价目表、订单、回款确认、开通（建服务期+按档位发额度）、
    交付扣量（80% 提醒 / 100% 暂停）、赠送挂包（自动算金额）、
    续费结转一次、试用到期清零、医院侧剩余额度页、运营后台 6 个页面。

不做（MVP 后再说）：支付网关、审批系统、自动对账任务、纠正/异议流程、
    渠道佣金系统、发票系统、销售侧报价器、轻量档。

新增（本轮补入）：**每个产品应用内"本次消耗"提示**——像聊天框下的 Token 计数一样，
用户用一次，当场看到"本次扣了多少、还剩多少、什么时候到期"。
```

---

## 1. 后端任务（lingce-api）

### 阶段 A：数据模型

| # | 任务 | 目标 | 改动点 | 验收 | 依赖 | 估时 |
|---|---|---|---|---|---|---|
| A1 | `product_catalog` 表 | 价目表：产品+档位 → 额度/年价/增量包 | migration：product_code、edition(standard/light/trial)、quota(单位→数量)、annual_price、increment_quota、increment_price、price_version、is_active | 建表成功，索引与唯一约束(product_code, edition)生效 | — | 0.5d |
| A2 | `product_entitlements` 表 | 额度余额：每产品每单位一行 | migration：tenant_id、product_code、unit、edition、quota_type、granted、used、rollover_bal、balance、period_end、status、updated_at；UNIQUE(tenant, product, unit, edition, quota_type) | 建表成功；balance 由服务维护不裸改 | A1 | 0.5d |
| A3 | `entitlement_ledger` 表 | 流水：发/扣一张表 | migration：type(grant/usage)、amount(±)、source(purchase/gift/trial/rollover/correction)、reason、linked_order_id、idempotency_key、operator、created_at；UNIQUE(tenant, idempotency_key) | 建表成功 | A2 | 0.5d |
| A4 | `billing_orders` 表 | 订单+支付合并 | migration：order_no、order_type(single/bundle/increment/hardware/gift/trial)、items(jsonb)、total/payable、source、salesman_id、channel_id、price_list_version、paid_amount/paid_at/confirmed_by、status、reason、approved_by | 建表成功 | A1 | 0.5d |
| A5 | 服务期复用 `tenant_subscriptions` | 统一到期日落在现有订阅表 | 开通时写一行：plan 固定 standard、started_on=开通日、expired_on=统一到期日；查询用 expired_on | 开通动作能写入并读回服务期 | A4 | 0.5d |
| A6 | 价目表种子数据 | 六产品 standard/trial 两档 + 增量包 | 种子：六产品 standard 额度与年价（12,600/10,800/5,400/21,600/8,400/15,600）、增量包（1,500/1,300/650/2,600/1,000/1,900）、trial=额度×7/365 年价 0；price_version=2026 | 测试库可查全量价目表，数值与销售速查一致 | A1 | 0.5d |

**阶段 A 小计：3d**

### 阶段 B：价目表与查询 API

| # | 任务 | 目标 | 改动点 | 验收 | 依赖 | 估时 |
|---|---|---|---|---|---|---|
| B1 | 价目表查询/编辑接口 | 运营后台管理价目表 | `GET/PUT /api/v1/ops/catalog`（含版本列表）；修改只加新版本，不改旧版本 | 接口返回六产品×档位×增量包；改价产生新版本号 | A6 | 1d |
| B2 | 租户额度查询接口 | 医院侧+运营侧看余额 | `GET /api/v1/tenants/{id}/entitlements`：每产品每单位 granted/used/balance/rollover_bal/period_end/status | 返回结构与前端页面对齐 | A2 | 1d |
| B2.1 | 单产品额度轻量查询 | 各产品应用内联显示"剩余" | `GET /api/v1/tenants/{id}/entitlements?product=X`：只返回该产品各单位余额与到期日，响应极简（页面内联用，高频） | 各产品应用一行接口拿到剩余额度 | A2 | 0.5d |
| B3 | 流水查询接口 | 明细可查可对账 | `GET /api/v1/tenants/{id}/entitlement-ledger`（分页、按产品/单位/时间过滤） | 能倒查任意一笔发/扣 | A3 | 0.5d |

**阶段 B 小计：2.5d**

### 阶段 C：订单、回款与开通

| # | 任务 | 目标 | 改动点 | 验收 | 依赖 | 估时 |
|---|---|---|---|---|---|---|
| C1 | 订单接口 | 录单（含折算录入） | `POST/GET /api/v1/ops/orders`；items 由运营录入（折算后的额度与价格）；系统从价目表校验并算 total/payable，写 price_list_version | 录一单增量（按剩余天数折算）金额正确 | B1 | 1.5d |
| C2 | 回款确认接口 | 财务确认到账 | `POST /api/v1/ops/orders/{id}/settle`：paid_amount/paid_at/confirmed_by → status=settled；全款校验 | 确认后订单 settled，留确认人 | C1 | 0.5d |
| C3 | 开通接口 | 开通租户+按档位发额度（事务） | `POST /api/v1/ops/orders/{id}/activate`：①写 tenant_subscriptions（建服务期，后买产品只延用同一到期日）②按 items 写 ledger grant + 更新 entitlements；中途加购按剩余天数折算 | 开通后：服务期正确、额度到账、余额=授予值 | C2,A5 | 2d |
| C4 | 退款（开通前全额退） | 支付了未开通可退 | `POST /api/v1/ops/orders/{id}/refund`：仅 settled 且未 activate 可退；已开通拒绝 | 开通前可退、开通后退不了 | C3 | 0.5d |

**阶段 C 小计：4.5d**

### 阶段 D：交付扣量引擎（核心）

| # | 任务 | 目标 | 改动点 | 验收 | 依赖 | 估时 |
|---|---|---|---|---|---|---|
| D1 | 扣量服务（内部） | 幂等扣量+行锁防超扣 | `internal/billing/deduct.go`：入参(tenant, product, unit, amount, idempotency_key)；事务：查余额→按顺序扣(rollover→trial→official)→写 usage ledger→更新 balance；余额不足返回"额度不足"；**返回本次扣量明细（各 unit 扣了多少）供交付接口透传给前端** | 并发重复扣只扣一次；余额不足不超扣；返回明细与 ledger 一致 | A3 | 2d |
| D2 | 交付判定接入（六产品） | 每个产品交付成功点挂扣量 | 各模块交付成功处调用 D1：病历草稿生成(emrrecord)、完整质控(emrquality, ActionComplete 且未复用)、成交分析(deal)、随访(followup)、合规检查(compliance)、截图解读/专项建议/经营方案(客户总监)、洞察/AI诊断/文稿/图片(获客) | 每类交付成功扣一次，量符合《销售速查》计量例子 | D1 | 3d |
| D3 | 病历质控特例 | 提交新跑扣 1 轮、复用不扣 | emrrecord Submit 里：ReusableReport 分支不扣；新跑 Evaluate 后扣 1 轮；保存级不扣 | 提交复用 QualityRunID 不重复扣轮次 | D1 | 1d |
| D4 | 阈值拦截 | 80% 提醒、100% 暂停 | 扣量后检查：≤20% 记提醒(站内)；=0 时对应产品交付入口返回"额度已用完"；进行中任务跑完 | 余额 0 后新交付被拒，提醒可见 | D2 | 1d |

**阶段 D 小计：7d**

### 阶段 E：赠送、续费与到期

| # | 任务 | 目标 | 改动点 | 验收 | 依赖 | 估时 |
|---|---|---|---|---|---|---|
| E1 | 赠送挂包接口 | 按包数说、自动算金额 | `POST /api/v1/ops/orders`（order_type=gift）：选产品+档位+包数 → 系统从价目表算"相当于 X 元"返回；≤1 包直接、>1 包记 approved_by；再 `POST /api/v1/ops/orders/{id}/activate` 发 gift 额度 | 送 1 个听写包自动显示 2,600 元并下发 | C3,B1 | 1.5d |
| E2 | 续费结转接口 | 结转一次（三步） | `POST /api/v1/ops/tenants/{id}/renew`：①rollover_bal=上期 balance（ledger 记 rollover）②按订单发新年度额度 ③period_end 延一年；trial 额度不结转直接清零 | 续费后余额=结转+新年包，扣量先扣结转 | C3 | 1.5d |
| E3 | 到期处理 | 到期清规则 | 每日任务：period_end 过期的额度：official 且未续费→清零；trial→清零；rollover_bal 第二周期结束未用完→清零；均写 ledger expired | 到期 30 天窗口内续费可结转；否则清零 | E2 | 1d |

**阶段 E 小计：4d**

### 阶段 F：保障与收尾

| # | 任务 | 目标 | 改动点 | 验收 | 依赖 | 估时 |
|---|---|---|---|---|---|---|
| F1 | 幂等与并发测试 | 防重复扣/超扣 | 单元+集成测试：同幂等键并发 10 次只扣 1 次；余额不足并发不超扣 | 测试全绿 | D1 | 1d |
| F2 | 流水导出 | 人工对账 | `GET /api/v1/ops/entitlement-ledger/export`（CSV） | 月底导出可对账 | B3 | 0.5d |
| F3 | 接口文档 | 交付给前端 | API_DOCUMENTATION 补计费章节 | 前端可按文档开发 | 全部 | 0.5d |

**阶段 F 小计：2d**

**后端合计：约 23 人日**

---

## 2. 前端任务（lingce-web）

### 医院侧（apps/institution，机构端）

| # | 任务 | 目标 | 改动点 | 验收 | 依赖 | 估时 |
|---|---|---|---|---|---|---|
| G1 | 剩余额度页 | 客户看到每产品还剩多少 | 新页面 + 路由 + 菜单：产品卡片（额度/单位/余额/到期日）+ 用量明细列表；调 B2/B3 接口 | 听写病历显示三行（份/轮/小时），余额与后台一致 | B2,B3 | 2d |
| G2 | 额度用完提示 | 100% 暂停的用户可见反馈 | 交付入口拦截提示 + 余额页置灰态 | 余额 0 时页面给出"联系销售加购"提示 | D4 | 0.5d |

### 共享组件（packages/product-ui，一次开发六产品复用）

| # | 任务 | 目标 | 改动点 | 验收 | 依赖 | 估时 |
|---|---|---|---|---|---|---|
| G3 | `QuotaUsageLine` 组件 | "本次消耗"提示行，类似 Token 计数 | product-ui 新增组件：交付结果下方一行小字——`本次消耗 1 份病历 · 12 分钟录音 · 1 轮质控`＋`剩余 2,499 份｜397.8h｜4,999 轮`＋`到期 2027-06-30`；余额 0 时变红提示"额度已用完，联系销售加购增量包"；props 支持隐藏无消耗的单位 | 组件 Storybook/文档齐全，六产品可直接复用 | B2.1 | 1d |

### 六产品接入（每个产品应用在"交付成功处"挂上 G3）

| # | 任务 | 应用 | 挂载位置 | 验收 | 依赖 | 估时 |
|---|---|---|---|---|---|---|
| G4.1 | 听写病历 | apps/emr | 病历草稿生成完成下方；提交质控完成下方（复用则不显示消耗） | 生成病历当场看到"扣 1 份 + X 分钟"；提交新跑质控看到"扣 1 轮" | D1,B2.1,G3 | 1d |
| G4.2 | 赢客专家 | apps/deal-v2 | 成交分析结果下方 | 分析完成看到"扣 X 分钟录音分析" | 同上 | 0.5d |
| G4.3 | 随访顾问 | apps/followup | 随访分析结果下方 | 同上 | 同上 | 0.5d |
| G4.4 | 合规卫士 | apps/compliance | 合规检查报告下方 | 同上 | 同上 | 0.5d |
| G4.5 | 获客先锋 | apps/content-studio | 文稿交付后、图片交付后、AI 诊断完成后 | 文稿扣 1 份、图片扣 1 张、诊断扣 1 题次分别显示 | 同上 | 1d |
| G4.6 | 客户总监 | apps/patients | 截图解读、专项建议、经营方案生成后 | 截图扣 1 张/建议扣 1 次/方案扣 1 人次分别显示 | 同上 | 1d |

**医院侧小计：2.5d＋共享组件与接入 5.5d＝8d**

### 运营侧（apps/operation-console，新运营端）

| # | 任务 | 目标 | 改动点 | 验收 | 依赖 | 估时 |
|---|---|---|---|---|---|---|
| H1 | 价目表页 | 管理产品+档位+增量包价格 | `CatalogPage.tsx` + `catalogApi.ts`；列表/编辑（新增版本） | 六产品两档一页看完，改价生新版本 | B1 | 1.5d |
| H2 | 订单页 | 录单、看单、回款确认 | `OrdersPage.tsx` + `ordersApi.ts`：列表（状态筛选）、录单（选产品/档位/数量→自动带价）、回款确认按钮、退款按钮 | 完整走通"录单→确认→开通" | C1,C2 | 2d |
| H3 | 开通页 | 开通租户+按档位发额度 | `ActivationPage.tsx`：选租户+产品+档位→预览额度与价格→确认开通；显示统一到期日 | 开通后客户侧立即可见额度 | C3 | 1.5d |
| H4 | 额度页 | 查任意客户余额与流水 | `EntitlementsPage.tsx`：租户搜索→余额卡片→流水表；导出 CSV 按钮 | 可倒查任意一笔发/扣 | B2,B3,F2 | 1.5d |
| H5 | 赠送挂包页 | 按包数赠送、自动算金额 | `GiftPage.tsx`：选租户+产品+包数→自动显示"相当于 X 元"→reason→≤1 包直接提交、>1 包记 approved_by | 送 1 包无需审批直接到账 | E1 | 1.5d |
| H6 | 续费结转入口 | 续费时结转一次 | `RenewPage.tsx`：选租户→显示上期余额→确认续费（自动结转+发新年包） | 续费后余额=结转+新额度 | E2 | 1d |

**运营侧小计：9d**

**前端合计：约 17 人日**

---

## 3. 里程碑（按依赖排序）

```text
M1（数据模型）  A1–A6                     第 1 周前 3 天
M2（订单开通）  B1–B3 → C1–C4            第 1 周末 – 第 2 周
M3（扣量）      D1–D4（核心，与 M2 并行）  第 2 周
M4（赠送续费）  E1–E3                     第 3 周
M5（前端）      G1–G3、G4.1–G4.6、H1–H6 随接口就绪插空开发，最后 1 周联调
M6（收尾）      F1–F3 + 全流程回归          第 3 周末
```

**总周期：约 3.5 周（1 名全栈 + Codex 辅助，前后端并行约 3 周）**

---

## 4. 风险与决策点（实现时确认）

| # | 风险/决策 | 说明 |
|---|---|---|
| 1 | 六产品交付成功点的具体代码位置 | D2 接入时逐个确认模块（emrrecord/emrquality/deal/followup/compliance/content 等），以"交付物落库成功"为钩子 |
| 2 | 服务期落在 tenant_subscriptions | 复用现有表（plan 固定 standard），实现时确认旧订阅数据的兼容处理 |
| 3 | 老客户迁移 | 现有租户（5000/账号包干）如何映射到新产品额度——不阻塞 MVP，但开通前要定 |
| 4 | 交付判定与旧"免费"逻辑冲突 | 现有代码里 trial 按录音条数限额（TrialMaxRecordings），新扣量上线后需替换，避免双轨 |
| 5 | 幂等键选取 | 各产品用"业务对象 ID + 交付类型"作幂等键，实现时统一约定 |
| 6 | 测试环境先行 | 全部开发在测试库完成，生产只跑 migration + 种子，不写数据 |

---

## 5. 验收场景（端到端，上线前必过）

```text
① 录单一单"听写病历 standard"21,600 → 转账 → 回款确认 → 开通
   → 医院侧看到 2,500 份 / 5,000 轮 / 400 小时，到期日 = 开通日 + 12 个月
② 生成 1 份病历草稿 → 份数 -1、录音时长相应扣；提交（复用质控）→ 质控不扣
   提交（新跑）→ 质控 -1 轮
③ 试用客户（trial 档）用满 → 100% 暂停；试用到期 → 清零
④ 送 1 个听写增量包 → 自动显示 2,600 元 → 直接到账；送 2 个 → 需老板点头
⑤ 到期续费 → 上期余额结转、先扣结转、新额度到账；第二周期结束结转清零
⑥ 开通前退款成功；开通后退款被拒
⑦ 并发重复扣量只扣一次
⑧ 每个产品应用内使用一次，当场看到"本次消耗 + 剩余 + 到期"，
   与 DeepSeek Harness 的 Token 计数体验一致；余额 0 时红字提示加购
```
