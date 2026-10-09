BEGIN;
UPDATE public.emr_template_fields tf
SET is_visible=TRUE,is_required=TRUE,updated_at=NOW()
WHERE tf.field_id IN (SELECT id FROM public.emr_fields WHERE tenant_id=0 AND code='diagnosis')
  AND tf.template_id IN (SELECT id FROM public.emr_templates WHERE tenant_id=0 AND status='published');
COMMIT;
