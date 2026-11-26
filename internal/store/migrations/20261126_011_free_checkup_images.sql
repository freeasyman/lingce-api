-- 长图上传：前端 canvas 生成后上传，拿到 https 图片地址，微信里长按才有「保存图片」
CREATE TABLE IF NOT EXISTS free_checkup_images (
  id BIGSERIAL PRIMARY KEY,
  content_type TEXT NOT NULL DEFAULT 'image/jpeg',
  data BYTEA NOT NULL,
  created_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);
CREATE INDEX IF NOT EXISTS idx_free_checkup_images_created ON free_checkup_images (created_at);
