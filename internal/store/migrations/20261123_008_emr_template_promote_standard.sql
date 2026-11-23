-- 模板标准清理：把租户 1 / 5001 的历史专科副本提升为平台标准，退役重复副本。
-- 已核实：两租户副本内容完全一致（名称/版本/打印标题/描述/绑定/覆盖句式）。
-- 历史病历：5001 的引用因 id 不变而零影响；租户 1 的引用先改指到同内容平台行再删副本。

-- 3.1 提升 5001 的专科模板为平台标准（id 不变）
UPDATE public.emr_templates
SET tenant_id = 0, updated_at = NOW()
WHERE tenant_id = 5001
  AND code IN ('dermatology', 'surgery', 'gynecology', 'tcm');

-- 3.2 租户 1 的历史病历/快照改指到同内容平台行
WITH mapping AS (
    SELECT t1.id AS old_id, tp.id AS new_id
    FROM public.emr_templates t1
    JOIN public.emr_templates tp
      ON tp.tenant_id = 0
     AND tp.code = t1.code
     AND tp.visit_type = t1.visit_type
     AND tp.status = 'published'
     AND tp.is_current = true
    WHERE t1.tenant_id = 1
      AND t1.code IN ('dermatology', 'surgery', 'gynecology', 'tcm')
)
UPDATE public.emr_records r
SET template_id = m.new_id, updated_at = NOW()
FROM mapping m
WHERE r.template_id = m.old_id;

WITH mapping AS (
    SELECT t1.id AS old_id, tp.id AS new_id
    FROM public.emr_templates t1
    JOIN public.emr_templates tp
      ON tp.tenant_id = 0
     AND tp.code = t1.code
     AND tp.visit_type = t1.visit_type
     AND tp.status = 'published'
     AND tp.is_current = true
    WHERE t1.tenant_id = 1
      AND t1.code IN ('dermatology', 'surgery', 'gynecology', 'tcm')
)
UPDATE public.emr_record_snapshots s
SET template_version_id = m.new_id
FROM mapping m
WHERE s.template_version_id = m.old_id;

-- 删除租户 1 的重复副本及其绑定
DELETE FROM public.emr_template_fields
WHERE template_id IN (
    SELECT id FROM public.emr_templates
    WHERE tenant_id = 1 AND code IN ('dermatology', 'surgery', 'gynecology', 'tcm')
);
DELETE FROM public.emr_templates
WHERE tenant_id = 1 AND code IN ('dermatology', 'surgery', 'gynecology', 'tcm');

-- 3.3 历史描述清理
UPDATE public.emr_templates
SET description = '平台标准' || name || '，包含专科栏位。', updated_at = NOW()
WHERE description LIKE '%派特生物%';

-- 3.4 就诊类型归一：默认通用、禁止空值
ALTER TABLE public.emr_templates ALTER COLUMN visit_type SET DEFAULT '通用';
UPDATE public.emr_templates SET visit_type = '通用' WHERE visit_type IS NULL OR visit_type = '';
ALTER TABLE public.emr_templates ALTER COLUMN visit_type SET NOT NULL;
