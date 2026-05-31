# Day 10 Customer 回归说明

## 目标

覆盖 `customer` 高级端点的三类回归：

1. 成功场景（`200`）
2. 参数错误场景（`400`）
3. 权限/鉴权错误场景（`401`）

对应冲刺清单中的 Day 10 端点：

- `GET /api/v1/customers/{id}/momentum-history`
- `GET /api/v1/customers/duplicates`
- `POST /api/v1/customers/merge`
- `GET /api/v1/customers/{id}/consultation-records`
- `GET /api/v1/customers/{id}/emr-records`
- `POST /api/v1/customer-tags/batch`
- `GET /api/v1/customer-tags/stats`
- `GET/POST/DELETE /api/v1/customer-groups/{id}/members`
- `POST /api/v1/customer-groups/rules/preview`
- `POST /api/v1/customer-groups/rules/validate`

## 脚本位置

- [day10_customer_regression.sh](/Users/yiliiang/Documents/lingce-api/scripts/regression/day10_customer_regression.sh)

## 执行方式

### 方式 A：直接使用现成 token

```bash
TOKEN='<your_jwt_token>' \
bash scripts/regression/day10_customer_regression.sh
```

### 方式 B：使用 JWT_SECRET 自动生成本地 admin token

脚本会按顺序读取：

1. 环境变量 `JWT_SECRET`
2. `CONFIG_PATH` 指向的 TOML 配置及其 `secrets_file` 中的 `jwt.secret`

示例：

```bash
JWT_SECRET='your-secret-key-change-in-production' \
bash scripts/regression/day10_customer_regression.sh
```

或：

```bash
CONFIG_PATH=./configs/dev.toml \
bash scripts/regression/day10_customer_regression.sh
```

## 常用参数

可选环境变量：

- `BASE_URL`：默认 `http://127.0.0.1:18080`
- `API_PREFIX`：默认 `/api/v1`
- `TENANT_ID`：默认 `1`
- `SESSION_VERSION`：默认 `47`
- `JWT_EXPIRY_HOURS`：默认 `24`

示例：

```bash
BASE_URL='http://127.0.0.1:18080' TENANT_ID=1 \
bash scripts/regression/day10_customer_regression.sh
```

## 输出判定

脚本逐条输出：

- `PASS [category] ...`
- `FAIL [category] ...`

最后输出汇总：

- `Summary: pass=<n> fail=<n>`

当 `fail > 0` 时，脚本返回非 0 退出码，便于接入 CI。
