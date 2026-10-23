-- 20261023_001_content_center_v2_tables.sql
-- 内容获客 v2 数据模型（Phase 1）：建新表 + 迁移旧数据，暂不删旧表（Phase 3 再删）。

-- 编码约定（存英文码，前端展示中文）：
--   platform: wechat/xiaohongshu/douyin/shipinhao/moments/website/community/offline
--   role: brand(品牌)/doctor_ip(医生IP)/conversion(转化)/website_fact(官网事实)
--   insight type: question/concern/misconception/scene/quote/aha/viewpoint/explanation/story/fact
--   goal: science(科普)/brand(品牌)/doctor_ip(医生IP)/conversion(转化)/geo(GEO)
--   form: article/graphic/poster/video/script/storyboard

-- ============ 1. 媒体账号 ============
CREATE TABLE IF NOT EXISTS content_media_accounts (
    id            bigserial PRIMARY KEY,
    tenant_id     bigint NOT NULL,
    platform      text NOT NULL,
    name          text NOT NULL,
    role          text NOT NULL,
    monthly_target int,
    is_active     boolean NOT NULL DEFAULT true,
    created_at    timestamptz NOT NULL DEFAULT now(),
    updated_at    timestamptz NOT NULL DEFAULT now(),
    deleted_at    timestamptz
);

INSERT INTO content_media_accounts (id, tenant_id, platform, name, role, monthly_target, is_active, created_at, updated_at, deleted_at)
SELECT
    id, tenant_id,
    CASE platform WHEN 'wechat' THEN 'wechat' WHEN 'shipinhao' THEN 'shipinhao' WHEN 'website' THEN 'website' ELSE lower(platform) END,
    name,
    CASE account_type WHEN 'institution' THEN 'brand' WHEN 'doctor' THEN 'doctor_ip' WHEN 'website' THEN 'website_fact' ELSE 'brand' END,
    monthly_target,
    COALESCE(status = 'active', true),
    created_at, updated_at, deleted_at
FROM content_accounts
WHERE deleted_at IS NULL;

-- ============ 2. 单次洞察（灵感，从 recording_content_seeds 迁）============
CREATE TABLE IF NOT EXISTS content_insights (
    id                bigserial PRIMARY KEY,
    tenant_id         bigint NOT NULL,
    recording_id      bigint,
    source_type       text NOT NULL DEFAULT 'recording',
    type              text NOT NULL,
    topic             text NOT NULL,
    evidence          jsonb,
    suggested_forms   jsonb,
    suggested_targets jsonb,
    created_at        timestamptz NOT NULL DEFAULT now()
);

INSERT INTO content_insights (id, tenant_id, recording_id, source_type, type, topic, evidence, suggested_targets, created_at)
SELECT
    id, tenant_id, recording_id, 'recording',
    CASE seed_type
        WHEN 'practical_qa' THEN 'question'
        WHEN 'concern_handling' THEN 'concern'
        WHEN 'relatable_scene' THEN 'scene'
        WHEN 'golden_quote' THEN 'quote'
        WHEN 'aha_moment' THEN 'aha'
        ELSE seed_type
    END,
    topic,
    jsonb_build_object(
        'quotes', COALESCE(seed_data->'key_quotes', '[]'::jsonb),
        'patient_concern', to_jsonb(patient_concern),
        'angle', to_jsonb(content_angle)
    ),
    suggested_platforms,
    created_at
FROM recording_content_seeds;

-- ============ 3. 问题簇（聚合洞察 = 反复问）============
CREATE TABLE IF NOT EXISTS content_question_clusters (
    id                     bigserial PRIMARY KEY,
    tenant_id              bigint NOT NULL,
    topic                  text NOT NULL,
    frequency              jsonb,
    representative_quotes  jsonb,
    linked_recording_ids   jsonb,
    updated_at             timestamptz NOT NULL DEFAULT now()
);

-- ============ 4. 选题（判断产物）============
CREATE TABLE IF NOT EXISTS content_topics_v2 (
    id              bigserial PRIMARY KEY,
    tenant_id       bigint NOT NULL,
    title           text NOT NULL,
    description     text,
    form            text,
    account_id      bigint,
    goal            text,
    source          text,
    recommendation  text,
    expected_help   jsonb,
    evidence_ref    bigint,
    created_at      timestamptz NOT NULL DEFAULT now()
);

INSERT INTO content_topics_v2 (id, tenant_id, title, description, form, goal, source, created_at)
SELECT
    id, tenant_id, title,
    COALESCE(NULLIF(description, ''), angle),
    CASE
        WHEN content_type IN ('article','文章') THEN 'article'
        WHEN content_type IN ('graphic','图文','小红书图文') THEN 'graphic'
        WHEN content_type IN ('poster','海报') THEN 'poster'
        WHEN content_type IN ('script','口播稿') THEN 'script'
        WHEN content_type IN ('storyboard','分镜') THEN 'storyboard'
        ELSE 'article'
    END,
    NULL, 'plan', created_at
FROM content_topics
WHERE deleted_at IS NULL;

-- ============ 5. 内容（创作产物）============
CREATE TABLE IF NOT EXISTS content_items_v2 (
    id              bigserial PRIMARY KEY,
    tenant_id       bigint NOT NULL,
    topic_id        bigint,
    form            text NOT NULL,
    title           text,
    body            text,
    publish_package jsonb,
    images          jsonb,
    status          text NOT NULL DEFAULT 'draft',
    reviewed_at     timestamptz,
    published_at    timestamptz,
    created_at      timestamptz NOT NULL DEFAULT now(),
    updated_at      timestamptz NOT NULL DEFAULT now(),
    deleted_at      timestamptz
);

INSERT INTO content_items_v2 (id, tenant_id, topic_id, form, title, body, publish_package, images, status, published_at, created_at, updated_at, deleted_at)
SELECT
    id, tenant_id, topic_id,
    COALESCE(content_type, 'article'),
    title, content,
    jsonb_build_object('tags', COALESCE(tags, '[]'::jsonb), 'summary', to_jsonb(summary)),
    images,
    CASE status WHEN 'published' THEN 'published' ELSE 'draft' END,
    published_at, created_at, updated_at, deleted_at
FROM content_items
WHERE deleted_at IS NULL;

-- ============ 6. 计划周期 ============
CREATE TABLE IF NOT EXISTS content_plan_periods (
    id           bigserial PRIMARY KEY,
    tenant_id    bigint NOT NULL,
    period       text NOT NULL DEFAULT 'month',
    start_date   date NOT NULL,
    summary_text text,
    created_at   timestamptz NOT NULL DEFAULT now(),
    updated_at   timestamptz NOT NULL DEFAULT now(),
    UNIQUE (tenant_id, period, start_date)
);

-- ============ 7. 计划项（排期）============
CREATE TABLE IF NOT EXISTS content_plan_items_v2 (
    id              bigserial PRIMARY KEY,
    plan_period_id  bigint NOT NULL,
    account_id      bigint NOT NULL,
    topic_id        bigint,
    content_id      bigint,
    planned_date    date,
    status          text NOT NULL DEFAULT 'todo',
    sort            int,
    created_at      timestamptz NOT NULL DEFAULT now(),
    updated_at      timestamptz NOT NULL DEFAULT now()
);

-- ============ 8. 风格模板（学模板存的）============
CREATE TABLE IF NOT EXISTS content_style_templates (
    id              bigserial PRIMARY KEY,
    tenant_id       bigint NOT NULL,
    level           text NOT NULL DEFAULT 'global',
    industry        text,
    type            text NOT NULL,
    name            text NOT NULL,
    reference_url   text,
    extracted_style jsonb,
    created_at      timestamptz NOT NULL DEFAULT now(),
    updated_at      timestamptz NOT NULL DEFAULT now()
);

-- ============ 9. 创作方案（六层，从 content_prompt_templates 迁）============
CREATE TABLE IF NOT EXISTS content_creation_schemes (
    id                  bigserial PRIMARY KEY,
    tenant_id           bigint NOT NULL DEFAULT 0,
    name                text NOT NULL,
    version             int NOT NULL DEFAULT 1,
    prompt              text,
    model_ref           text,
    structure_templates jsonb,
    visual_assets       jsonb,
    platform_rules      jsonb,
    knowledge_ref       bigint,
    is_active           boolean NOT NULL DEFAULT true,
    created_at          timestamptz NOT NULL DEFAULT now(),
    updated_at          timestamptz NOT NULL DEFAULT now()
);

INSERT INTO content_creation_schemes (id, tenant_id, name, version, prompt, is_active, created_at, updated_at)
SELECT
    id, COALESCE(tenant_id, 0), name, 1,
    COALESCE(NULLIF(system_prompt, ''), '') || E'\n\n' || COALESCE(prompt_template, ''),
    COALESCE(is_active, true), created_at, COALESCE(updated_at, created_at)
FROM content_prompt_templates;

-- ============ 10. 知识库/规则 ============
CREATE TABLE IF NOT EXISTS content_org_knowledge (
    id               bigserial PRIMARY KEY,
    tenant_id        bigint NOT NULL,
    institution_info jsonb,
    doctors          jsonb,
    specialties      jsonb,
    data             jsonb,
    hard_rules       jsonb,
    soft_prefs       jsonb,
    created_at       timestamptz NOT NULL DEFAULT now(),
    updated_at       timestamptz NOT NULL DEFAULT now()
);

-- ============ 11. 机构资料（复用现有 content_org_profiles，不新建，只补字段）============
-- content_org_profiles 已存在，含 logo_url/qr_code_url/doctor_name 等，Phase 1 保持原样。
