# API 文档

本目录包含 lingce-api 的 API 规范与端点清单。

## 文档格式

项目使用 **OpenAPI 3.0** 规范。

## 文档文件

- `openapi_complete.yaml` - 从代码自动提取生成的完整文档（**376 个 API 端点**）⭐
- `API_ENDPOINTS.md` - 从代码自动提取生成的 Markdown 端点清单（**376 个 API 端点**）
- `openapi.yaml` - 基础模板（示例/组件定义）
- `openapi_full.yaml` - 历史手动维护版本（非当前事实源）

推荐使用 `openapi_complete.yaml`。

## 如何查看文档

### 方法1：Swagger UI（推荐）

```bash
cd /Users/yiliiang/Documents/lingce-api
make docs
```

浏览器访问 http://localhost:8000

### 方法2：在线工具

打开 https://editor.swagger.io/ 并粘贴 `openapi_complete.yaml`。

## 当前实现统计（基于代码扫描）

统计时间：2026-04-09

| 模块 | 端点数 |
|------|--------|
| auth | 9 |
| organization | 30 |
| department | 5 |
| employee | 6 |
| recording | 78 |
| rbac | 39 |
| sysconfig | 19 |
| badge | 46 |
| content | 74 |
| customer | 34 |
| support | 36 |
| **合计（/api/v1/*）** | **376** |

补充：服务还提供 `GET /healthz` 健康检查端点（不计入 `/api/v1/*`）。

## 更新文档

当路由发生变化时，重新生成：

```bash
cd /Users/yiliiang/Documents/lingce-api
python3 scripts/extract_and_generate_openapi.py
python3 scripts/generate_markdown_docs.py
```

## 认证方式

所有受保护接口使用 JWT：

```http
Authorization: Bearer <token>
```

用户类型：
- `admin`
- `employee`
- `mobile`

## 响应格式

成功响应：

```json
{
  "data": {}
}
```

分页响应：

```json
{
  "items": [],
  "total": 0,
  "page": 1,
  "page_size": 20
}
```

错误响应：

```json
{
  "code": "ERROR_CODE",
  "message": "错误描述",
  "details": null
}
```

## 相关资源

- [API 端点列表](API_ENDPOINTS.md)
- [OpenAPI 规范](openapi_complete.yaml)
- [项目主文档](../README.md)

## 计费缴费（billing）

实现位置：`internal/billing/`（`module.go` 装配路由与 Service）；建表语句见迁移 `internal/store/migrations/20261110_002_billing_foundation.sql`，价目表种子见 `20261110_004_seed_product_catalog.sql`。

### 概览

| 方法 | 路径 | 鉴权 | 说明 |
|------|------|------|------|
| GET | `/api/v1/ops/billing/catalog` | admin | 价目表查询（默认每个 product+edition 返回最新版本） |
| POST | `/api/v1/ops/billing/catalog` | admin | 新增价目版本（只增不改，返回 201） |
| GET | `/api/v1/tenants/{id}/entitlements` | admin + employee | 租户额度余额 + 服务期 |
| GET | `/api/v1/tenants/{id}/entitlement-ledger` | admin + employee | 额度流水分页查询 |
| GET | `/api/v1/ops/entitlement-ledger/export` | admin | 额度流水 CSV 导出（单次上限 10 万行） |
| POST | `/api/v1/ops/orders` | admin | 录单（状态 created，返回 201） |
| GET | `/api/v1/ops/orders` | admin | 订单列表（分页） |
| GET | `/api/v1/ops/orders/{id}` | admin | 订单详情（含 items） |
| POST | `/api/v1/ops/orders/{id}/settle` | admin | 回款确认（MVP 只支持全款） |
| POST | `/api/v1/ops/orders/{id}/activate` | admin | 开通订单，`mode=grant`（默认）/ `mode=renew` |
| POST | `/api/v1/ops/orders/{id}/refund` | admin | 开通前全额退款 |
| POST | `/api/v1/ops/orders/gift/preview` | admin | 赠送挂包预览（额度 + 等价金额） |
| POST | `/api/v1/internal/billing/deduct` | internal | 幂等扣量（`X-Internal-Token`） |
| POST | `/api/v1/internal/billing/check-quota` | internal | 额度预检（只读，不写流水） |

通用约定：

- **鉴权**：`admin` / `admin + employee` 走 JWT（`Authorization: Bearer <token>`）；`internal` 只校验 `X-Internal-Token: <token>`，不校验 JWT。
- **越权**：租户额度 / 流水允许 admin 查任意租户，employee 只能查 JWT 绑定的本租户，越权返回 403。
- **分页**：`page` 默认 1；`page_size` 默认 50、上限 200，越界自动收敛不报错。
- **数值**：金额与额度是 JSON 数字（如 `12600.00`），服务端用 NUMERIC 文本承载，不经过 float64，不丢精度。
- **时间**：`from` / `to` 入参支持 RFC3339（`2026-11-01T00:00:00Z`）或 `YYYY-MM-DD`（按 UTC 零点理解）；输出为 RFC3339。
- **错误格式**：沿用上文统一结构 `{"code","message","details"}`。运营端/租户端参数校验错误 code 为 `BAD_REQUEST`，内部扣量接口为 `INVALID_REQUEST`。

---

### 价目表查询

`GET /api/v1/ops/billing/catalog` ｜ 鉴权：admin

| 参数 | 位置 | 必填 | 说明 |
|------|------|------|------|
| product_code | query | 否 | 产品编码，最长 32 |
| edition | query | 否 | `standard` / `light` / `trial` |
| price_version | query | 否 | 指定版本；不传时每个 product+edition 只返回最新版本 |

请求示例：

```http
GET /api/v1/ops/billing/catalog?product_code=yingke&edition=standard&price_version=2026
Authorization: Bearer <admin token>
```

响应示例（200）：

```json
{
  "items": [
    {
      "product_code": "yingke",
      "product_name": "赢客专家",
      "edition": "standard",
      "price_version": "2026",
      "annual_price": 12600.00,
      "quotas": {"hour": 400},
      "increment_price": 1500.00,
      "increments": {"hour": 80},
      "is_active": true
    }
  ]
}
```

主要错误码：400 `BAD_REQUEST`（edition 非法、product_code/price_version 超长）、401 `UNAUTHORIZED`、403 `FORBIDDEN`（非 admin）。

### 价目表新增版本

`POST /api/v1/ops/billing/catalog` ｜ 鉴权：admin

请求示例：

```json
{
  "product_code": "yingke",
  "product_name": "赢客专家",
  "edition": "standard",
  "price_version": "2027",
  "annual_price": 13800,
  "quotas": {"hour": 520},
  "increment_price": 1600,
  "increments": {"hour": 90}
}
```

说明：`product_name` 可不传，服务端沿用同产品已有版本的名字（没有历史版本时退回 `product_code`）；价目表只允许加新版本，不提供修改历史版本的入口。

响应示例（201）：

```json
{
  "product_code": "yingke",
  "product_name": "赢客专家",
  "edition": "standard",
  "price_version": "2027",
  "annual_price": 13800.00,
  "quotas": {"hour": 520},
  "increment_price": 1600.00,
  "increments": {"hour": 90},
  "is_active": true
}
```

主要错误码：400 `BAD_REQUEST`（缺 product_code/price_version、edition/单位非法、金额为负或超范围）、409 `CATALOG_VERSION_EXISTS`（同 product+edition+price_version 已存在）。

### 租户额度

`GET /api/v1/tenants/{id}/entitlements` ｜ 鉴权：admin + employee

| 参数 | 位置 | 必填 | 说明 |
|------|------|------|------|
| id | path | 是 | 租户 id |
| product | query | 否 | 只看某个产品 |
| include_zero | query | 否 | `true` 时包含余额为 0 的额度行，默认 false |

请求示例：

```http
GET /api/v1/tenants/9/entitlements?product=yingke&include_zero=false
Authorization: Bearer <admin|employee token>
```

响应示例（200）：

```json
{
  "items": [
    {
      "product_code": "yingke",
      "product_name": "赢客专家",
      "unit": "hour",
      "edition": "standard",
      "quota_type": "official",
      "granted": 400.00,
      "used": 12.50,
      "rollover_bal": 0.00,
      "balance": 387.50,
      "period_end": "2027-10-31",
      "status": "active"
    }
  ],
  "service_period": {"started_on": "2026-11-01", "expired_on": "2027-10-31"}
}
```

说明：租户没有任何订阅记录时 `service_period` 为 `null`；`status` 取值 `active` / `paused` / `expired` / `cleared`。

主要错误码：400 `BAD_REQUEST`（`Invalid tenant ID`、`include_zero` 非布尔、product 超长）、401 `UNAUTHORIZED`、403 `FORBIDDEN`（employee 查别家租户）。

### 额度流水

`GET /api/v1/tenants/{id}/entitlement-ledger` ｜ 鉴权：admin + employee

| 参数 | 位置 | 必填 | 说明 |
|------|------|------|------|
| id | path | 是 | 租户 id |
| product / unit | query | 否 | 按产品、计量单位过滤（unit 需为 `hour`/`record`/`round`/`shot`/`prompt`/`person`） |
| from / to | query | 否 | 时间范围，from 不能晚于 to |
| page / page_size | query | 否 | 默认 1 / 50，page_size 上限 200 |

请求示例：

```http
GET /api/v1/tenants/9/entitlement-ledger?product=yingke&unit=hour&from=2026-11-01&to=2026-11-30&page=1&page_size=50
Authorization: Bearer <admin|employee token>
```

响应示例（200）：

```json
{
  "items": [
    {
      "id": 501,
      "product_code": "yingke",
      "unit": "hour",
      "type": "usage",
      "amount": -12.50,
      "source": "purchase",
      "reason": "交付扣量：成交分析",
      "linked_order_id": null,
      "operator": "系统",
      "created_at": "2026-11-10T02:30:00Z"
    }
  ],
  "total": 1,
  "page": 1,
  "page_size": 50
}
```

说明：`type` 为 `grant`（发放，金额为正）或 `usage`（扣减，金额为负）；`source` 取值 `purchase` / `gift` / `trial` / `rollover` / `correction` / `expired`。

主要错误码：400 `BAD_REQUEST`（tenant_id 非法、unit 不在枚举内、from 晚于 to、from/to 格式错误）、401、403。

### 额度流水导出（CSV）

`GET /api/v1/ops/entitlement-ledger/export` ｜ 鉴权：admin

| 参数 | 位置 | 必填 | 说明 |
|------|------|------|------|
| tenant_id | query | 是 | 正整数（导出是运营跨租户对账，租户在 query 里） |
| product / unit | query | 否 | 同流水分页查询 |
| from / to | query | 否 | 同流水分页查询 |

请求示例：

```http
GET /api/v1/ops/entitlement-ledger/export?tenant_id=9&product=yingke&unit=hour&from=2026-11-01&to=2026-11-30
Authorization: Bearer <admin token>
```

响应（200）：

- `Content-Type: text/csv; charset=utf-8`
- `Content-Disposition: attachment; filename="entitlement-ledger-9-20261110.csv"`
- 文件以 UTF-8 BOM（`EF BB BF`）开头，Excel 直接打开中文不乱码；字段由 `encoding/csv` 转义，`reason` 中的逗号、双引号、换行会按 CSV 规则加引号。

```csv
tenant_id,product_code,product_name,unit,type,amount,source,reason,linked_order_id,operator,created_at
9,yingke,赢客专家,hour,usage,-12.50,purchase,"交付扣量：录音,分析",1234,张三,2026-11-10 08:30:00
```

说明：表头固定为 `tenant_id,product_code,product_name,unit,type,amount,source,reason,linked_order_id,operator,created_at`；`created_at` 统一按 UTC 输出（`YYYY-MM-DD HH:MM:SS`）；`linked_order_id` 为空时输出空列。数据按 `created_at DESC, id DESC` 排序，单次最多 100000 行。

主要错误码：400 `BAD_REQUEST`（缺 tenant_id / tenant_id 非法、from/to 格式错误、unit 非法、**超过 100000 行上限**——提示收窄时间范围）、401、403（非 admin）。

### 录单

`POST /api/v1/ops/orders` ｜ 鉴权：admin

| 字段 | 必填 | 说明 |
|------|------|------|
| tenant_id | 是 | 租户 id |
| order_type | 是 | `single` / `bundle` / `increment` / `hardware` / `gift` / `trial` |
| items | 是 | 订单行，至少 1 行 |
| items[].product_code | 是 | 产品编码 |
| items[].edition | 否 | 默认 `standard` |
| items[].quota_type | 否 | `official` / `trial`；`trial` 订单强制为 `trial` |
| items[].quotas | 是 | 单位 → 数量，数量必须 > 0 |
| items[].price | 否 | 行金额；不传时由价目表带出（increment 用增量价，其余用年价） |
| source | 否 | `direct`（默认）/ `channel` |
| salesman_id / channel_id / approved_by | 否 | 正整数 |
| reason | gift 单必填 | 备注 |
| price_list_version | 否 | 指定价目版本，不传时按价目表自动带出 |

请求示例：

```json
{
  "tenant_id": 9,
  "order_type": "single",
  "items": [
    {
      "product_code": "yingke",
      "edition": "standard",
      "quota_type": "official",
      "quotas": {"hour": 400}
    }
  ],
  "source": "direct",
  "salesman_id": 12,
  "reason": "2026 年度续签"
}
```

响应示例（201）：

```json
{
  "id": 1001,
  "order_no": "BL20261110103000123",
  "tenant_id": 9,
  "order_type": "single",
  "items": [
    {
      "product_code": "yingke",
      "edition": "standard",
      "quota_type": "official",
      "quotas": {"hour": 400},
      "price": 12600.00
    }
  ],
  "total_amount": 12600.00,
  "payable_amount": 12600.00,
  "source": "direct",
  "salesman_id": 12,
  "channel_id": null,
  "price_list_version": "2026",
  "paid_amount": 0.00,
  "paid_at": null,
  "confirmed_by": null,
  "status": "created",
  "reason": "2026 年度续签",
  "approved_by": null,
  "activated_at": null,
  "activated_by": null,
  "created_at": "2026-11-10T02:30:00Z",
  "updated_at": "2026-11-10T02:30:00Z"
}
```

说明：赠送单（`gift`）的 `total_amount` / `payable_amount` 强制为 0，行金额用于展示"相当于 X 元"；赠送超过 1 个增量包必须带 `approved_by`。

主要错误码：400 `BAD_REQUEST`（tenant_id/order_type/source 非法、gift 单缺 reason）、400 `ORDER_ITEMS_INVALID`（items 缺字段、quota 单位非法、价目表查不到该产品档位）、400 `GIFT_APPROVAL_REQUIRED`（赠送超过 1 个增量包但没有 approved_by）。

### 订单列表

`GET /api/v1/ops/orders` ｜ 鉴权：admin

| 参数 | 位置 | 必填 | 说明 |
|------|------|------|------|
| tenant_id | query | 否 | 只看某个租户 |
| status | query | 否 | `created` / `paid` / `settled` / `refunded` / `cancelled` |
| order_type | query | 否 | 同录单枚举 |
| page / page_size | query | 否 | 默认 1 / 50，上限 200 |

请求示例：

```http
GET /api/v1/ops/orders?tenant_id=9&status=settled&order_type=single&page=1&page_size=50
Authorization: Bearer <admin token>
```

响应示例（200）：

```json
{
  "items": [
    {
      "id": 1001,
      "order_no": "BL20261110103000123",
      "tenant_id": 9,
      "order_type": "single",
      "items": [
        {"product_code": "yingke", "edition": "standard", "quota_type": "official", "quotas": {"hour": 400}, "price": 12600.00}
      ],
      "total_amount": 12600.00,
      "payable_amount": 12600.00,
      "paid_amount": 12600.00,
      "status": "settled",
      "created_at": "2026-11-10T02:30:00Z",
      "updated_at": "2026-11-10T03:00:00Z"
    }
  ],
  "total": 1,
  "page": 1,
  "page_size": 50
}
```

主要错误码：400 `BAD_REQUEST`（tenant_id 非正整数、status/order_type 不在枚举内）。

### 订单详情

`GET /api/v1/ops/orders/{id}` ｜ 鉴权：admin

请求示例：

```http
GET /api/v1/ops/orders/1001
Authorization: Bearer <admin token>
```

响应示例（200）：与录单返回的订单对象结构一致（含 items 与回款/开通字段）。

主要错误码：400 `BAD_REQUEST`（`Invalid order ID`）、404 `ORDER_NOT_FOUND`。

### 回款确认

`POST /api/v1/ops/orders/{id}/settle` ｜ 鉴权：admin

| 字段 | 必填 | 说明 |
|------|------|------|
| paid_amount | 是 | 实收金额，必须等于 `payable_amount`（MVP 只支持全款） |
| paid_at | 否 | 回款时间，不传取服务端当前时间 |
| note | 否 | 备注（当前只做入参兼容，不落库） |

请求示例：

```json
{
  "paid_amount": 12600,
  "paid_at": "2026-11-10T10:00:00+08:00",
  "note": "银行转账"
}
```

响应示例（200）：订单对象，`status` 变为 `settled`，`paid_amount` / `paid_at` / `confirmed_by` 已写入。

```json
{
  "id": 1001,
  "order_no": "BL20261110103000123",
  "status": "settled",
  "paid_amount": 12600.00,
  "paid_at": "2026-11-10T02:00:00Z",
  "confirmed_by": 1,
  "activated_at": null
}
```

主要错误码：400 `BAD_REQUEST`（缺 paid_amount、金额格式非法）、400 `ORDER_AMOUNT_MISMATCH`（回款金额≠应付金额）、404 `ORDER_NOT_FOUND`、409 `ORDER_STATUS_INVALID`（不是 created 状态）。

### 开通（mode=grant / renew）

`POST /api/v1/ops/orders/{id}/activate` ｜ 鉴权：admin

| 参数 | 位置 | 必填 | 说明 |
|------|------|------|------|
| mode | query 或 body | 否 | `grant`（默认，只发额度不延服务期）/ `renew`（延长服务期 + 结转上期余额一次） |

请求示例：

```http
POST /api/v1/ops/orders/1001/activate?mode=grant
Authorization: Bearer <admin token>
```

```json
{"mode": "renew"}
```

响应示例（200）：

```json
{
  "order": {
    "id": 1001,
    "order_no": "BL20261110103000123",
    "tenant_id": 9,
    "order_type": "single",
    "status": "settled",
    "activated_at": "2026-11-10T03:10:00Z",
    "activated_by": 1
  },
  "service_period": {"started_on": "2026-11-01", "expired_on": "2027-10-31"},
  "entitlements": [
    {
      "product_code": "yingke",
      "product_name": "赢客专家",
      "unit": "hour",
      "edition": "standard",
      "quota_type": "official",
      "granted": 400.00,
      "used": 0.00,
      "rollover_bal": 0.00,
      "balance": 400.00,
      "period_end": "2027-10-31",
      "status": "active"
    }
  ]
}
```

说明：

- `mode=grant`：建/读统一服务期（同租户所有产品共用一个到期日），按 items 累加额度，不延期；适用于中途加购、后买产品。
- `mode=renew`：延长服务期（已过期则从今天起算 12 个月）并逐行结转一次上期余额，trial 额度清零；租户还没有订阅记录时返回 `NO_SUBSCRIPTION`。
- 无论哪种模式，"同一订单只能开通一次"由事务内的抢行 UPDATE 保证，重复点击只有 1 次成功。

主要错误码：400 `BAD_REQUEST`（mode 非 grant/renew、`Invalid order ID`）、400 `ORDER_ITEMS_INVALID`（订单 items 无法展开成发放明细）、404 `ORDER_NOT_FOUND`、409 `ORDER_ALREADY_ACTIVATED`（已开通过）、409 `ORDER_STATUS_INVALID`（状态不允许开通，如 bundle 未回款）、409 `NO_SUBSCRIPTION`（renew 但租户无订阅）。

### 退款

`POST /api/v1/ops/orders/{id}/refund` ｜ 鉴权：admin

请求示例：

```http
POST /api/v1/ops/orders/1001/refund
Authorization: Bearer <admin token>
```

响应示例（200）：订单对象，`status` 变为 `refunded`。

```json
{
  "id": 1001,
  "order_no": "BL20261110103000123",
  "status": "refunded",
  "paid_amount": 12600.00,
  "activated_at": null
}
```

说明：只支持"已回款且未开通"的订单全额退款；已开通的订单不能退额度。

主要错误码：400 `BAD_REQUEST`、400 `ORDER_ALREADY_ACTIVATED`（已开通，接口按约定返回 400）、404 `ORDER_NOT_FOUND`、409 `ORDER_STATUS_INVALID`（不是 settled 状态）。

### 赠送挂包预览

`POST /api/v1/ops/orders/gift/preview` ｜ 鉴权：admin

| 字段 | 必填 | 说明 |
|------|------|------|
| tenant_id | 否 | 仅用于标识预览上下文，不参与算价 |
| product_code | 是 | 产品编码 |
| edition | 否 | 默认 `standard` |
| packs | 是 | 增量包个数，1–999 |

请求示例：

```json
{
  "tenant_id": 9,
  "product_code": "yingke",
  "edition": "standard",
  "packs": 2
}
```

响应示例（200）：

```json
{
  "product_code": "yingke",
  "product_name": "赢客专家",
  "edition": "standard",
  "packs": 2,
  "pack_price": 1500.00,
  "equivalent_amount": 3000.00,
  "items": [
    {"unit": "hour", "quotas": 160}
  ],
  "needs_approval": true
}
```

说明：额度 = 价目表 `increments` × 包数；`equivalent_amount` = 包数 × `increment_price`；`needs_approval=true` 表示该赠送单必须记 `approved_by` 才能录入。

主要错误码：400 `BAD_REQUEST`（product_code 缺失、packs 非正数或超 999、价目表查不到该产品档位）。

### 内部扣量

`POST /api/v1/internal/billing/deduct` ｜ 鉴权：internal（`X-Internal-Token`）

| 字段 | 必填 | 说明 |
|------|------|------|
| tenant_id | 是 | 租户 id |
| product_code | 是 | 产品编码 |
| items | 是 | `[{unit, amount}]`，最多 50 个单位，amount > 0 |
| idempotency_key | 是 | 幂等键，格式 `<模块>:<业务对象>:<交付类型>`，最长 128，不能含空白，如 `deal:recording:123:analysis` |
| reason | 否 | 流水原因，默认"交付扣量"，最长 500 字 |

请求示例：

```http
POST /api/v1/internal/billing/deduct
X-Internal-Token: <internal token>
Content-Type: application/json
```

```json
{
  "tenant_id": 9,
  "product_code": "yingke",
  "items": [{"unit": "hour", "amount": 12.5}],
  "idempotency_key": "deal:recording:123:analysis",
  "reason": "成交分析交付"
}
```

响应示例（200）：

```json
{
  "lines": [
    {"unit": "hour", "amount": 12.5, "rollover": 0, "trial": 0, "official": 12.5}
  ],
  "balances": [
    {
      "product_code": "yingke",
      "product_name": "赢客专家",
      "unit": "hour",
      "edition": "standard",
      "quota_type": "official",
      "granted": 400.00,
      "used": 12.50,
      "rollover_bal": 0.00,
      "balance": 387.50,
      "period_end": "2027-10-31",
      "status": "active"
    }
  ],
  "paused": false
}
```

说明：

- 扣减顺序固定为 **结转（rollover）→ 试用（trial）→ 正式（purchase）**，`lines` 里的三段之和等于 `amount`。
- 同一幂等键重复调用只扣一次：第一次 200，之后均返回 409 `ALREADY_DEDUCTED`（调用方应视为成功，不要再重试）。
- 余额允许被最后一笔略微扣成负数，此时 `paused=true`，该单位后续交付会被拒绝；额度已用尽的单位再次扣量直接返回 `INSUFFICIENT_ENTITLEMENT`。

主要错误码：400 `INVALID_REQUEST`（items/unit/amount/idempotency_key 不合法）、401 `UNAUTHORIZED`（缺/错 `X-Internal-Token`，内部路由直接返回纯文本 401）、409 `ALREADY_DEDUCTED`、409 `INSUFFICIENT_ENTITLEMENT`（`details` 带 `unit` / `current` / `required` / `shortfall`）。

### 内部额度预检

`POST /api/v1/internal/billing/check-quota` ｜ 鉴权：internal（`X-Internal-Token`）

请求体与扣量一致（`idempotency_key` / `reason` 可省，预检不使用）：

```json
{
  "tenant_id": 9,
  "product_code": "yingke",
  "items": [{"unit": "hour", "amount": 12.5}]
}
```

响应示例（200，额度足够）：

```json
{"ok": true}
```

说明：只读接口，不写流水、不改余额；额度口径与扣量完全一致（按各来源可扣上限汇总）。额度不足时返回 409，`details` 结构同扣量接口。

主要错误码：400 `INVALID_REQUEST`（同扣量）、401 `UNAUTHORIZED`、409 `INSUFFICIENT_ENTITLEMENT`。

### 错误码清单

| 错误码 | HTTP | 出现接口 | 含义 / 处理建议 |
|--------|------|----------|-----------------|
| `ALREADY_DEDUCTED` | 409 | 内部扣量 | 该 `idempotency_key` 已扣过，视为成功，不要重试 |
| `INSUFFICIENT_ENTITLEMENT` | 409 | 内部扣量、内部预检 | 额度不足，`details` 含 unit/current/required/shortfall |
| `INVALID_REQUEST` | 400 | 内部扣量、内部预检 | 入参不合法（items/unit/amount/幂等键/tenant_id） |
| `ORDER_AMOUNT_MISMATCH` | 400 | 回款确认 | 回款金额必须等于应付金额（MVP 只支持全款） |
| `ORDER_ALREADY_ACTIVATED` | 409（退款为 400） | 开通、退款 | 订单已开通，不能重复开通或退款 |
| `ORDER_STATUS_INVALID` | 409 | 回款、开通、退款 | 订单当前状态不允许该操作 |
| `ORDER_NOT_FOUND` | 404 | 订单详情、回款、开通、退款 | 订单不存在 |
| `ORDER_ITEMS_INVALID` | 400 | 录单、开通 | 订单 items 不合法（缺字段、单位/档位非法、价目表查不到、无法展开发放） |
| `CATALOG_VERSION_EXISTS` | 409 | 价目表新增版本 | 该 product+edition+price_version 已存在，请换新版本号 |
| `GIFT_APPROVAL_REQUIRED` | 400 | 录单 | 赠送超过 1 个增量包，需要老板审批（记 `approved_by`） |
| `NO_SUBSCRIPTION` | 409 | 开通（mode=renew） | 租户尚未开通服务期，需先以 mode=grant 开通再续费 |

补充：非上述业务码的通用错误为 `BAD_REQUEST`（400，参数/格式问题）、`UNAUTHORIZED`（401）、`FORBIDDEN`（403，越权或非 admin）、`INTERNAL_ERROR`（500）。

