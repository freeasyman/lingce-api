-- 20261014_003 已应用，后续兼容修正必须使用新版本迁移，不能修改已登记文件。
-- 避免历史 metadata_json 非 JSON 时触发转换失败；保留旧字段，不强制互转 extra_data。
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
