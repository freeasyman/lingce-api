-- 031：员工产品权限覆盖表
--
-- 岗位只决定默认可用产品；这里只存「与默认不同」的手动调整。
-- 有效权限 = 岗位默认 ∪ 手动启用 − 手动停用；机构管理员默认拥有全部产品。

CREATE TABLE IF NOT EXISTS employee_product_overrides (
    tenant_id    bigint      NOT NULL,
    employee_id  bigint      NOT NULL,
    product_code text        NOT NULL,
    enabled      boolean     NOT NULL,
    updated_by   bigint,
    created_at   timestamptz NOT NULL DEFAULT now(),
    updated_at   timestamptz NOT NULL DEFAULT now(),
    PRIMARY KEY (tenant_id, employee_id, product_code)
);
