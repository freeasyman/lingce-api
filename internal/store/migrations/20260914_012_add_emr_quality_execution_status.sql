-- 区分病历质量结论与质检执行状态。
--
-- “manual_required”表示确实需要医生或质控人员复核；
-- “executor_not_implemented”表示系统还没有实现该程序规则执行器，
-- 不能把它展示为病历问题，也不能进入医生待处理列表。
--
-- 署名：Codex
-- 时间：2026-09-14

ALTER TABLE public.emr_check_results
  ADD COLUMN execution_status VARCHAR(32) NOT NULL DEFAULT 'completed';

ALTER TABLE public.emr_check_results
  DROP CONSTRAINT IF EXISTS emr_check_results_conclusion_check,
  DROP CONSTRAINT IF EXISTS emr_check_results_handling_check,
  DROP CONSTRAINT IF EXISTS emr_check_results_incomplete_reason_check,
  ADD CONSTRAINT emr_check_results_execution_status_check
    CHECK (execution_status IN ('completed', 'manual_required', 'executor_not_implemented', 'execution_failed')),
  ADD CONSTRAINT emr_check_results_conclusion_check
    CHECK (conclusion IN ('符合', '不符合', '无法判断', '无法完成')),
  ADD CONSTRAINT emr_check_results_handling_check
    CHECK (handling_result IN ('仅提示', '待医生处理', '待人工判断', '允许继续', '阻断', '无需处理', '需要医生处理', '需要人工复核', '系统配置缺失')),
  ADD CONSTRAINT emr_check_results_incomplete_reason_check
    CHECK (
      (check_status = '已完成' AND incomplete_reason = '')
      OR (check_status = '未完成' AND incomplete_reason IN ('执行失败', '人工待处理', '程序执行器未实现', '执行器未调用'))
    );

DO $$
BEGIN
  IF NOT EXISTS (
    SELECT 1
    FROM information_schema.columns
    WHERE table_schema = 'public'
      AND table_name = 'emr_check_results'
      AND column_name = 'execution_status'
  ) THEN
    RAISE EXCEPTION 'emr_check_results.execution_status 添加失败';
  END IF;
END $$;
