-- 听写病历：质检是 AI 增强，核心操作（保存/提交病历）不得被质检额度阻断。
--
-- 提交病历在额度不足 / 服务暂停 / 到期时跳过质检并照常保存，但必须把
-- 「这份病历为什么没质检」留在业务表上，供医生、质控和管理者事后查看：
--   quality_status        checked / skipped_no_quota /
--                         skipped_subscription_paused / skipped_subscription_expired
--   quality_status_reason 面向客户的说明（含续费后可恢复的提示）
--   quality_status_at     状态变更时间
ALTER TABLE public.emr_records
  ADD COLUMN IF NOT EXISTS quality_status VARCHAR(32) NOT NULL DEFAULT '',
  ADD COLUMN IF NOT EXISTS quality_status_reason TEXT NOT NULL DEFAULT '',
  ADD COLUMN IF NOT EXISTS quality_status_at TIMESTAMPTZ;

COMMENT ON COLUMN public.emr_records.quality_status IS
  '最近一次完成级质检状态：checked=已质检，skipped_no_quota=额度不足未质检，skipped_subscription_paused/expired=服务暂停或到期未质检';
COMMENT ON COLUMN public.emr_records.quality_status_reason IS
  '质检状态原因（客户口径文案），供病历详情/列表明示降级原因';
COMMENT ON COLUMN public.emr_records.quality_status_at IS
  '质检状态最近一次变更时间';

-- 质控队列按「未质检」筛选：只索引有状态的行，避免全表膨胀。
CREATE INDEX IF NOT EXISTS emr_records_quality_status_idx
  ON public.emr_records (tenant_id, quality_status, quality_status_at DESC)
  WHERE quality_status <> '';
