-- 眼视光专科病历模板（租户 5001 灵策综合医院）
--
-- 固定句式 + 括号内可编辑：括号外的文字只读，括号内是医生填写位，
-- 括号里的内容为默认值。存储值为渲染后的纯字符串，质检/留痕/打印均不受影响。
--
-- 实现：复用全局公共字段（chief_complaint 等），在 emr_template_fields 的
-- 覆盖列（name_override / input_type_override / structure_json）里写入本模板句式；
-- 三个眼科专属字段（发病日期 / 眼别 / 其他诊断）建在全局 tenant 0，与皮肤科、外科
-- 的专属字段同约定。其他模板不写覆盖列，完全不受影响。
--
-- 幂等：可重复执行。

DO $$
DECLARE
  v_tenant BIGINT := 5001;
  v_template UUID;
  v_field UUID;
  seed RECORD;
BEGIN
  -- 1) 眼科专属字段（全局 tenant 0，编码带 ophthalmology_ 前缀，避免与公共字段冲突）
  INSERT INTO public.emr_fields
    (tenant_id, code, name, specialty, content_category, input_type,
     description, options_json, structure_json, status, created_by, updated_by)
  VALUES
    (0, 'ophthalmology_onset_date',    '发病日期', '眼科', '病史',     '日期',   '眼视光病历发病日期',         '{}'::jsonb,                              '{}'::jsonb, 'enabled', 0, 0),
    (0, 'ophthalmology_eye_laterality','眼别',     '眼科', '医疗判断', '单选',   '眼别 OU/OD/OS',               '{"options":["OU","OD","OS"]}'::jsonb,    '{}'::jsonb, 'enabled', 0, 0),
    (0, 'ophthalmology_other_diagnosis','其他诊断','眼科', '医疗判断', '长文本', '眼视光病历其他诊断（自由填写）', '{}'::jsonb,                              '{}'::jsonb, 'enabled', 0, 0)
  ON CONFLICT (tenant_id, code) DO UPDATE
    SET name = EXCLUDED.name,
        specialty = EXCLUDED.specialty,
        content_category = EXCLUDED.content_category,
        input_type = EXCLUDED.input_type,
        options_json = EXCLUDED.options_json,
        status = 'enabled',
        updated_at = NOW();

  -- 2) 租户模板（一行 = 一个版本）
  SELECT id INTO v_template
  FROM public.emr_templates
  WHERE COALESCE(tenant_id, 0) = v_tenant AND code = 'ophthalmology' AND version_no = '1.0';

  IF v_template IS NULL THEN
    INSERT INTO public.emr_templates
      (tenant_id, code, name, version_no, document_type, visit_type, specialty,
       status, is_current, print_title, description, created_by, published_by, updated_by, published_at)
    VALUES
      (v_tenant, 'ophthalmology', '眼视光专科病历', '1.0', '门诊病历', '初诊', '眼科',
       'published', TRUE, '眼视光专科病历', '眼视光（屈光手术）专科病历模板', 0, 0, 0, NOW())
    RETURNING id INTO v_template;
  END IF;

  -- 3) 绑定栏目（覆盖句式 / 显示名 / 填写方式）
  CREATE TEMP TABLE _opt_bindings (
    code TEXT NOT NULL,
    display_order INTEGER NOT NULL,
    is_required BOOLEAN NOT NULL DEFAULT FALSE,
    name_override TEXT,
    input_type_override TEXT,
    structure_json JSONB
  ) ON COMMIT DROP;

  INSERT INTO _opt_bindings (code, display_order, is_required, name_override, input_type_override, structure_json) VALUES
    ('chief_complaint',            10, FALSE, NULL, NULL, '{"template":"双眼视力差（　）余年，近2年每年增加（<）0.5D，要求手术"}'::jsonb),
    ('ophthalmology_onset_date',       15, FALSE, NULL, NULL, NULL),
    ('present_illness',            20, FALSE, NULL, NULL, '{"template":"曾戴角膜接触镜（　）年，摘镜（　）周（　）天。硬镜（　）年，摘镜（　）周（　）天。眼干涩（有/无）。"}'::jsonb),
    ('past_history',               30, FALSE, NULL, NULL, '{"template":"眼外伤手术史（/），视网膜脱离病史（/）。全身病史如：风湿病、糖尿病、结缔组织病、甲亢、免疫缺陷病等（/），癫痫体质（/），其他："}'::jsonb),
    ('personal_history',           40, FALSE, NULL, NULL, '{"template":"无"}'::jsonb),
    ('family_history',             50, FALSE, NULL, NULL, '{"template":"角膜营养不良（/）、圆锥角膜家族史（/），青光眼家族史（/）。"}'::jsonb),
    ('allergy_history',            60, FALSE, NULL, NULL, '{"template":"药物过敏（有/无/不详）"}'::jsonb),
    ('physical_exam',              70, FALSE, '眼部检查', '长文本', '{"template":"睑缘形态（正常/肥厚/过度角化/充血及扩张/新生血管/不规则浅切口），鳞屑（+/-）。睑板腺开口（通畅/脂帽/隆起/脂栓），睑脂质量（清亮/浑浊/粥样/膏状）。结膜充血（-），双眼角膜清，上皮光滑，前房中深，KP（-），Tyn（-），虹膜纹理清，瞳孔等大等圆、直径约（3）mm，对光反射（+），晶状体（清），散瞳后晶状体（），视盘色可，C/D=（0.3），中心凹反光（+）"}'::jsonb),
    ('auxiliary_exam',             80, FALSE, NULL, NULL, '{"template":"无"}'::jsonb),
    ('diagnosis',                  90, TRUE,  NULL, NULL, NULL),
    ('ophthalmology_other_diagnosis',  95, FALSE, NULL, NULL, NULL),
    ('ophthalmology_eye_laterality',  100, TRUE,  NULL, NULL, NULL),
    ('disposition',               120, FALSE, NULL, NULL, '{"template":"术前检查"}'::jsonb);

  FOR seed IN SELECT * FROM _opt_bindings LOOP
    SELECT id INTO v_field
    FROM public.emr_fields
    WHERE tenant_id = 0 AND code = seed.code AND status = 'enabled';

    IF v_field IS NULL THEN
      RAISE EXCEPTION '眼视光模板栏目字段不存在或已停用：%', seed.code;
    END IF;

    INSERT INTO public.emr_template_fields
      (tenant_id, template_id, field_id, display_order, is_visible, is_required,
       description, name_override, input_type_override, structure_json, created_by)
    VALUES
      (0, v_template, v_field, seed.display_order, TRUE, seed.is_required,
       '', seed.name_override, seed.input_type_override, seed.structure_json, 0)
    ON CONFLICT (template_id, field_id) DO UPDATE
      SET display_order = EXCLUDED.display_order,
          is_visible = EXCLUDED.is_visible,
          is_required = EXCLUDED.is_required,
          name_override = EXCLUDED.name_override,
          input_type_override = EXCLUDED.input_type_override,
          structure_json = EXCLUDED.structure_json,
          updated_at = NOW();
  END LOOP;
END $$;
