-- 计费缴费 MVP 数据模型（只建表，不含种子数据）。
-- 范围：价目表、产品额度、额度流水、订单 4 张表。

-- 价目表：产品 + 档位 + 价目版本对应的年价、各单位额度与增量包规格，开通/赠送时据此带出额度与价格。
CREATE TABLE IF NOT EXISTS product_catalog (
  id BIGSERIAL PRIMARY KEY,
  product_code VARCHAR(32) NOT NULL,
  edition VARCHAR(16) NOT NULL DEFAULT 'standard',
  price_version VARCHAR(32) NOT NULL DEFAULT '2026',
  annual_price NUMERIC(12,2) NOT NULL DEFAULT 0,
  quotas JSONB NOT NULL DEFAULT '{}'::jsonb,
  increment_price NUMERIC(12,2) NOT NULL DEFAULT 0,
  increments JSONB NOT NULL DEFAULT '{}'::jsonb,
  is_active BOOLEAN NOT NULL DEFAULT TRUE,
  created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
  updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
  CONSTRAINT product_catalog_edition_check CHECK (edition IN ('standard','light','trial'))
);
CREATE UNIQUE INDEX IF NOT EXISTS product_catalog_identity_uq
  ON product_catalog (product_code, edition, price_version);

-- 产品额度：客户每个产品、每个单位的剩余额度（正式/试用分开，含结转余额与统一到期日）。
CREATE TABLE IF NOT EXISTS product_entitlements (
  id BIGSERIAL PRIMARY KEY,
  tenant_id BIGINT NOT NULL,
  product_code VARCHAR(32) NOT NULL,
  unit VARCHAR(16) NOT NULL,
  edition VARCHAR(16) NOT NULL DEFAULT 'standard',
  quota_type VARCHAR(16) NOT NULL DEFAULT 'official',
  granted NUMERIC(14,2) NOT NULL DEFAULT 0,
  used NUMERIC(14,2) NOT NULL DEFAULT 0,
  rollover_bal NUMERIC(14,2) NOT NULL DEFAULT 0,
  balance NUMERIC(14,2) NOT NULL DEFAULT 0,
  period_end DATE NOT NULL,
  status VARCHAR(16) NOT NULL DEFAULT 'active',
  created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
  updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
  CONSTRAINT product_entitlements_tenant_check CHECK (tenant_id > 0),
  CONSTRAINT product_entitlements_unit_check CHECK (unit IN ('hour','record','round','shot','prompt','person')),
  CONSTRAINT product_entitlements_quota_type_check CHECK (quota_type IN ('official','trial')),
  CONSTRAINT product_entitlements_status_check CHECK (status IN ('active','paused','expired','cleared'))
);
CREATE UNIQUE INDEX IF NOT EXISTS product_entitlements_identity_uq
  ON product_entitlements (tenant_id, product_code, unit, edition, quota_type);
CREATE INDEX IF NOT EXISTS product_entitlements_tenant_idx
  ON product_entitlements (tenant_id, period_end);

-- 额度流水：发放与扣减的唯一记账入口，余额只能由流水推导；idempotency_key 保证重试不重复扣。
CREATE TABLE IF NOT EXISTS entitlement_ledger (
  id BIGSERIAL PRIMARY KEY,
  tenant_id BIGINT NOT NULL,
  product_code VARCHAR(32) NOT NULL,
  unit VARCHAR(16) NOT NULL,
  type VARCHAR(16) NOT NULL,
  amount NUMERIC(14,2) NOT NULL,
  source VARCHAR(16) NOT NULL,
  reason TEXT NOT NULL DEFAULT '',
  linked_order_id BIGINT,
  idempotency_key VARCHAR(128),
  operator VARCHAR(64) NOT NULL DEFAULT '',
  created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
  CONSTRAINT entitlement_ledger_tenant_check CHECK (tenant_id > 0),
  CONSTRAINT entitlement_ledger_type_check CHECK (type IN ('grant','usage')),
  CONSTRAINT entitlement_ledger_source_check CHECK (source IN ('purchase','gift','trial','rollover','correction','expired'))
);
CREATE UNIQUE INDEX IF NOT EXISTS entitlement_ledger_idempotency_uq
  ON entitlement_ledger (tenant_id, idempotency_key) WHERE idempotency_key IS NOT NULL;
CREATE INDEX IF NOT EXISTS entitlement_ledger_tenant_time_idx
  ON entitlement_ledger (tenant_id, created_at DESC);
CREATE INDEX IF NOT EXISTS entitlement_ledger_tenant_product_idx
  ON entitlement_ledger (tenant_id, product_code, created_at DESC);

-- 订单：单品/组合/增量/硬件/赠送/试用 6 类订单，支付信息合并在订单上（不做支付网关与支付表）。
CREATE TABLE IF NOT EXISTS billing_orders (
  id BIGSERIAL PRIMARY KEY,
  order_no VARCHAR(32) NOT NULL,
  tenant_id BIGINT NOT NULL,
  order_type VARCHAR(16) NOT NULL,
  items JSONB NOT NULL DEFAULT '[]'::jsonb,
  total_amount NUMERIC(12,2) NOT NULL DEFAULT 0,
  payable_amount NUMERIC(12,2) NOT NULL DEFAULT 0,
  source VARCHAR(16) NOT NULL DEFAULT 'direct',
  salesman_id BIGINT,
  channel_id BIGINT,
  price_list_version VARCHAR(32) NOT NULL DEFAULT '',
  paid_amount NUMERIC(12,2) NOT NULL DEFAULT 0,
  paid_at TIMESTAMPTZ,
  confirmed_by BIGINT,
  status VARCHAR(16) NOT NULL DEFAULT 'created',
  reason TEXT NOT NULL DEFAULT '',
  approved_by BIGINT,
  created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
  updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
  CONSTRAINT billing_orders_tenant_check CHECK (tenant_id > 0),
  CONSTRAINT billing_orders_type_check CHECK (order_type IN ('single','bundle','increment','hardware','gift','trial')),
  CONSTRAINT billing_orders_source_check CHECK (source IN ('direct','channel')),
  CONSTRAINT billing_orders_status_check CHECK (status IN ('created','paid','settled','refunded','cancelled'))
);
CREATE UNIQUE INDEX IF NOT EXISTS billing_orders_no_uq ON billing_orders (order_no);
CREATE INDEX IF NOT EXISTS billing_orders_tenant_time_idx ON billing_orders (tenant_id, created_at DESC);
CREATE INDEX IF NOT EXISTS billing_orders_status_idx ON billing_orders (status, created_at DESC);
