-- 第一批规则允许执行和留痕，但在完整规则集发布前不得生成正式百分制评分。
CREATE TABLE IF NOT EXISTS public.emr_quality_rule_set_versions (
  version VARCHAR(32) PRIMARY KEY,
  status VARCHAR(16) NOT NULL DEFAULT 'draft',
  scoring_ready BOOLEAN NOT NULL DEFAULT FALSE,
  note TEXT NOT NULL DEFAULT '',
  created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
  updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
  CONSTRAINT emr_quality_rule_set_versions_status_check
    CHECK (status IN ('draft', 'published', 'retired'))
);

INSERT INTO public.emr_quality_rule_set_versions (version, status, scoring_ready, note)
VALUES (
  '2026-10-04',
  'published',
  FALSE,
  '第一批程序规则，仅用于质量检查和留痕；完整规则集确认后才允许写入正式百分制评分。'
)
ON CONFLICT (version) DO UPDATE SET
  status = EXCLUDED.status,
  scoring_ready = EXCLUDED.scoring_ready,
  note = EXCLUDED.note,
  updated_at = NOW();
