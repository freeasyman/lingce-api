-- 20261023_003_phase3_drop_old.sql
-- Phase 3 下架清理：DROP 旧表 + 去掉 _v2 后缀（新表接管正式表名）。

-- 1) DROP 旧表（CASCADE 处理历史外键）
DROP TABLE IF EXISTS content_plan_items CASCADE;
DROP TABLE IF EXISTS content_account_columns CASCADE;
DROP TABLE IF EXISTS content_accounts CASCADE;
DROP TABLE IF EXISTS content_item_versions CASCADE;
DROP TABLE IF EXISTS content_items CASCADE;
DROP TABLE IF EXISTS content_topics CASCADE;
DROP TABLE IF EXISTS recording_content_seeds CASCADE;
DROP TABLE IF EXISTS content_prompt_templates CASCADE;

-- 2) RENAME _v2 表为正式表名（先释放旧名，再接管）
ALTER TABLE content_topics_v2 RENAME TO content_topics;
ALTER SEQUENCE content_topics_v2_id_seq RENAME TO content_topics_id_seq;
ALTER TABLE content_topics ALTER COLUMN id SET DEFAULT nextval('content_topics_id_seq'::regclass);

ALTER TABLE content_items_v2 RENAME TO content_items;
ALTER SEQUENCE content_items_v2_id_seq RENAME TO content_items_id_seq;
ALTER TABLE content_items ALTER COLUMN id SET DEFAULT nextval('content_items_id_seq'::regclass);

ALTER TABLE content_plan_items_v2 RENAME TO content_plan_items;
ALTER SEQUENCE content_plan_items_v2_id_seq RENAME TO content_plan_items_id_seq;
ALTER TABLE content_plan_items ALTER COLUMN id SET DEFAULT nextval('content_plan_items_id_seq'::regclass);
