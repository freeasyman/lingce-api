-- 将病历修改留痕规则从电子病历质量规则体系中移除。
--
-- 这五条规则描述的是系统操作审计，不是病历内容质量：
--   emr.audit.trail.operator
--   emr.audit.trail.time
--   emr.audit.trail.reason
--   emr.audit.trail.before_value
--   emr.audit.trail.after_value
--
-- 审计信息仍由 emr_process_records 保存，并由病历保存/提交链路独立校验。
-- 本迁移只清理平台默认规则及其模板绑定，不影响租户自定义数据。
--
-- 署名：Codex
-- 时间：2026-09-14

DO $$
DECLARE
    audit_rule_ids UUID[];
BEGIN
    IF to_regclass('public.emr_quality_rules') IS NULL
       OR to_regclass('public.emr_template_quality_rules') IS NULL THEN
        RAISE EXCEPTION '缺少 EMR 质量规则表或模板绑定表，无法清理审计规则';
    END IF;

    SELECT COALESCE(array_agg(id), ARRAY[]::UUID[])
    INTO audit_rule_ids
    FROM public.emr_quality_rules
    WHERE tenant_id = 0
      AND code IN (
        'emr.audit.trail.operator',
        'emr.audit.trail.time',
        'emr.audit.trail.reason',
        'emr.audit.trail.before_value',
        'emr.audit.trail.after_value'
      );

    DELETE FROM public.emr_template_quality_rules
    WHERE tenant_id = 0
      AND quality_rule_id = ANY(audit_rule_ids);

    -- 有任何历史质检结果或租户模板绑定引用的规则不能物理删除，
    -- 否则会破坏外键或租户历史配置。此类规则保留为 disabled；
    -- 没有任何引用的规则才物理删除。
    UPDATE public.emr_quality_rules q
    SET status = 'disabled',
        disabled_at = COALESCE(disabled_at, NOW())
    WHERE q.tenant_id = 0
      AND q.code IN (
        'emr.audit.trail.operator',
        'emr.audit.trail.time',
        'emr.audit.trail.reason',
        'emr.audit.trail.before_value',
        'emr.audit.trail.after_value'
      )
      AND (
        EXISTS (
          SELECT 1
          FROM public.emr_check_results r
          WHERE r.quality_requirement_id = q.id
        )
        OR EXISTS (
          SELECT 1
          FROM public.emr_template_quality_rules b
          WHERE b.quality_rule_id = q.id
        )
      );

    DELETE FROM public.emr_quality_rules q
    WHERE q.tenant_id = 0
      AND q.code IN (
        'emr.audit.trail.operator',
        'emr.audit.trail.time',
        'emr.audit.trail.reason',
        'emr.audit.trail.before_value',
        'emr.audit.trail.after_value'
      )
      AND NOT EXISTS (
        SELECT 1
        FROM public.emr_check_results r
        WHERE r.quality_requirement_id = q.id
      );
END $$;
