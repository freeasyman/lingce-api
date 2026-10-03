-- 开发阶段统一默认中医门诊病历的栏位关系。
--
-- 中医问诊、诊断和处方沿用公共病历入口：
--   present_illness / diagnosis / prescription
-- 中医模板只额外保留四诊、舌象、脉象、辨证和治法。
-- 原始 emr_fields 数据不删除，旧病历仍可读取；本迁移只调整默认模板的绑定关系。
-- 迁移可重复执行，兼容当前编码 tcm 和历史编码 tcm_outpatient。

BEGIN;

DO $$
DECLARE
  template_record RECORD;
BEGIN
  FOR template_record IN
    SELECT DISTINCT ON (t.tenant_id, t.code)
      t.id,
      t.tenant_id,
      t.code
    FROM public.emr_templates t
    WHERE COALESCE(t.tenant_id, 0) = 0
      AND t.code IN ('tcm', 'tcm_outpatient')
      AND t.status = 'published'
      AND t.is_current = TRUE
    ORDER BY t.tenant_id, t.code, t.updated_at DESC NULLS LAST, t.id DESC
  LOOP
    DELETE FROM public.emr_template_fields tf
    USING public.emr_fields f
    WHERE tf.template_id = template_record.id
      AND tf.field_id = f.id
      AND f.tenant_id = 0
      AND f.code IN (
        'tcm_diagnosis',
        'tcm_inspection',
        'tcm_auscultation_olfaction',
        'tcm_inquiry',
        'tcm_palpation',
        'tcm_herbal_prescription'
      );

    INSERT INTO public.emr_template_fields
      (tenant_id, template_id, field_id, display_order, is_visible, is_required, description, created_by)
    SELECT
      0,
      template_record.id,
      f.id,
      desired.display_order,
      TRUE,
      FALSE,
      '',
      0
    FROM (
      VALUES
        ('tcm_four_examinations', 150),
        ('tcm_tongue', 160),
        ('tcm_pulse', 170),
        ('tcm_pattern_differentiation', 180),
        ('tcm_treatment_principle', 190)
    ) AS desired(code, display_order)
    JOIN public.emr_fields f
      ON f.tenant_id = 0
     AND f.code = desired.code
     AND f.status = 'enabled'
    ON CONFLICT (template_id, field_id) DO UPDATE
      SET display_order = EXCLUDED.display_order,
          is_visible = TRUE,
          is_required = FALSE,
          updated_at = NOW();
  END LOOP;
END $$;

COMMIT;
