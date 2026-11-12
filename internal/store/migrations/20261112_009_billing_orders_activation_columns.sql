-- 计费缴费：补齐 billing_orders 的开通标记列（幂等）。
--
-- 背景：20261110_002 用 CREATE TABLE IF NOT EXISTS 建 billing_orders，建表语句里
-- 已包含 activated_at / activated_by；但部分环境的 billing_orders 在这个迁移之前
-- 就已被旧版本脚本建出来，IF NOT EXISTS 直接跳过，导致表上永远缺这两列。
-- 运营端开通/加量/续期会写 activated_at，缺列时首次下单直接 500：
--   column "activated_at" of relation "billing_orders" does not exist
--
-- 本迁移只做加法，已有列时是 no-op，可安全重复执行。
ALTER TABLE IF EXISTS billing_orders
    ADD COLUMN IF NOT EXISTS activated_at TIMESTAMPTZ,
    ADD COLUMN IF NOT EXISTS activated_by BIGINT;
