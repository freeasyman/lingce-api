-- customer_identities 曾经使用 channel_type/external_* 字段；当前客户中心
-- 正式模型使用 channel/channel_id/nickname/avatar/extra_data。保留旧字段，
-- 补齐新字段并双向同步，避免历史模块和新患者运营同时读写时丢失身份信息。
ALTER TABLE customer_identities
    ADD COLUMN IF NOT EXISTS channel VARCHAR(30),
    ADD COLUMN IF NOT EXISTS channel_id VARCHAR(200),
    ADD COLUMN IF NOT EXISTS nickname VARCHAR(100),
    ADD COLUMN IF NOT EXISTS avatar VARCHAR(500),
    ADD COLUMN IF NOT EXISTS extra_data JSONB,
    ADD COLUMN IF NOT EXISTS updated_at TIMESTAMP WITHOUT TIME ZONE;

UPDATE customer_identities
SET channel = COALESCE(NULLIF(channel, ''), channel_type),
    channel_id = COALESCE(NULLIF(channel_id, ''), external_id),
    nickname = COALESCE(NULLIF(nickname, ''), external_name),
    avatar = COALESCE(NULLIF(avatar, ''), external_avatar),
    extra_data = COALESCE(extra_data, '{}'::jsonb),
    updated_at = COALESCE(updated_at, created_at, NOW())
WHERE channel IS NULL
   OR channel_id IS NULL
   OR nickname IS NULL
   OR avatar IS NULL
   OR extra_data IS NULL
   OR updated_at IS NULL;

CREATE OR REPLACE FUNCTION sync_customer_identity_compatibility()
RETURNS TRIGGER
LANGUAGE plpgsql
AS $$
BEGIN
    NEW.channel = COALESCE(NULLIF(NEW.channel, ''), NEW.channel_type);
    NEW.channel_type = COALESCE(NULLIF(NEW.channel_type, ''), NEW.channel);
    NEW.channel_id = COALESCE(NULLIF(NEW.channel_id, ''), NEW.external_id);
    NEW.external_id = COALESCE(NULLIF(NEW.external_id, ''), NEW.channel_id);
    NEW.nickname = COALESCE(NULLIF(NEW.nickname, ''), NEW.external_name);
    NEW.external_name = COALESCE(NULLIF(NEW.external_name, ''), NEW.nickname);
    NEW.avatar = COALESCE(NULLIF(NEW.avatar, ''), NEW.external_avatar);
    NEW.external_avatar = COALESCE(NULLIF(NEW.external_avatar, ''), NEW.avatar);
    NEW.extra_data = COALESCE(NEW.extra_data, '{}'::jsonb);
    NEW.updated_at = COALESCE(NEW.updated_at, NOW());
    IF NEW.created_at IS NULL THEN
        NEW.created_at = NOW();
    END IF;
    RETURN NEW;
END;
$$;

DROP TRIGGER IF EXISTS trg_customer_identity_compatibility ON customer_identities;
CREATE TRIGGER trg_customer_identity_compatibility
BEFORE INSERT OR UPDATE ON customer_identities
FOR EACH ROW EXECUTE FUNCTION sync_customer_identity_compatibility();

CREATE INDEX IF NOT EXISTS idx_identity_channel_id ON customer_identities(channel, channel_id);
CREATE INDEX IF NOT EXISTS idx_identity_customer_channel ON customer_identities(customer_id, channel);
