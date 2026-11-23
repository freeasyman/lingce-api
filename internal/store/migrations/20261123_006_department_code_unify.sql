-- 科室字典收敛：departments.code 对齐 organization_department_dict 统一字典。
-- 仅涉及历史别名：gyn→gynecology、internal→internal_medicine、eye→ophthalmology。
-- 已核实：员工档案与历史病历无旧别名；各租户内不存在目标 code 冲突。
UPDATE public.departments SET code = 'gynecology', updated_at = NOW() WHERE code = 'gyn' AND deleted_at IS NULL;
UPDATE public.departments SET code = 'internal_medicine', updated_at = NOW() WHERE code = 'internal' AND deleted_at IS NULL;
UPDATE public.departments SET code = 'ophthalmology', updated_at = NOW() WHERE code = 'eye' AND deleted_at IS NULL;
