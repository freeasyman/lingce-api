-- 门诊病历评分标准运行时数据。
--
-- docs 中的整理稿用于审核和留档；运行时由本表提供标准正文。
-- 本版本只保存已确认的正式评分部分，不包含文档中的“暂行执行准则”。

CREATE TABLE IF NOT EXISTS public.emr_score_standards (
  id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
  code VARCHAR(128) NOT NULL,
  version VARCHAR(64) NOT NULL,
  name VARCHAR(255) NOT NULL,
  source_name VARCHAR(255) NOT NULL DEFAULT '',
  source_reference TEXT NOT NULL DEFAULT '',
  standard_text TEXT NOT NULL,
  status VARCHAR(16) NOT NULL DEFAULT 'published',
  published_at TIMESTAMPTZ,
  created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
  updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
  CONSTRAINT emr_score_standards_status_check
    CHECK (status IN ('draft', 'published', 'disabled'))
);

CREATE UNIQUE INDEX IF NOT EXISTS emr_score_standards_code_version_uq
  ON public.emr_score_standards (code, version);

CREATE INDEX IF NOT EXISTS emr_score_standards_active_idx
  ON public.emr_score_standards (code, status, published_at DESC, updated_at DESC);

INSERT INTO public.emr_score_standards
(
  code,
  version,
  name,
  source_name,
  source_reference,
  standard_text,
  status,
  published_at
)
VALUES
(
  'outpatient_record_quality',
  '2017',
  '门诊病历质量评定标准',
  '门诊病历质量评定标准-1.jpg、门诊病历质量评定标准-2.jpg、门诊病历质量评定标准-3.jpg',
  'lingce-web/apps/emr/docs/门诊病历质量评定标准-图片整理稿-2026-09-22.md',
  $standard$
门诊病历质量评定标准

一、评分说明

总分：100 分。

病历等级：
- 甲级病历：大于 85 分。
- 乙级病历：70-85 分。
- 丙级病历：低于 70 分。

主要项目降级规则：
主诉、病史、体检、诊断、处理治疗五个主要项目的标准分合计为 80 分。
- 五项合计低于 70 分，病历为乙级。
- 五项合计低于 65 分，病历为丙级。

扣分原则：
- 各项目扣分以扣完该项目标准分值为止。
- 不实行倒扣。

二、一般项目

标准分值：5 分。

基本要求：
1. 内容包括姓名、性别、出生年月、民族、婚姻状况、职业、工作单位、住址、药物过敏史。
2. 每份病历应有患者姓名、病历编号。
3. 每次就诊应有就诊日期；急诊病例应具体到分钟。
4. 应记录科别。

扣分标准：
- 各项医疗信息未填写一项，扣 2 分。
- 一项医疗信息填写不正确，扣 1 分。
- 一项医疗信息填写不完整，扣 1 分。
- 急诊病历就诊日期未写到分钟，扣 3 分。

三、主诉

标准分值：5 分。

基本要求：
1. 内容完整，包括主要症状、部位和时间。
2. 重点突出，简明扼要。
3. 属于本专业、诊断明确且没有新症状体征的复诊或取药病历，主诉可写“病史同前”。

扣分标准：
- 缺少主诉内容，扣 5 分。
- 描述不确切、不完整或缺少某一部分，扣 2 分。
- 以诊断代替主诉，扣 2 分。
- 有新症状体征而缺主诉的复诊、取药病历，扣 3 分。

四、病史

标准分值：20 分。

基本要求：
1. 初诊病史要求应与主诉相关、相符，能够反映本次患病的起始、演变、主要症状、诊疗经过和疗效，有必要的鉴别诊断资料。
2. 复诊病史应重点记录经过治疗后的病情变化和治疗效果，未确诊病历出现新的症状体征及鉴别诊断资料的补充。
3. 与本次疾病相关的既往史、婚育史、过敏史和家族史。

扣分标准：
- 缺现病史，扣 20 分。
- 初诊现病史与主诉不相符，扣 4 分。
- 初诊现病史描述混乱、重点不突出，扣 4 分。
- 初诊缺少主要鉴别诊断资料，扣 3 分。
- 复诊未记录治疗后病情变化或治疗效果，扣 5 分。
- 未确诊复诊病历未记录新出现的症状体征或补充鉴别诊断资料，扣 3 分。
- 缺少与本次疾病相关的既往史及其他病史记录，扣 5 分。
- 与本次疾病相关的既往史及其他病史有重要不足，每项扣 3 分。

五、体检

标准分值：20 分。

基本要求：
1. 初诊应记录一般情况。
2. 应记录与主诉有关的常规项目、阳性体征，以及有助于诊断的主要阴性体征；专科应针对性检查。
3. 复诊应记录必要的体检项目，以及上次治疗后阳性体征的变化和新出现的阳性体征。

扣分标准：
- 缺少必要的体检项目，扣 20 分。
- 记录不准确、不全面，扣 5 分。
- 阳性体征或主要阴性体征未描述，扣 5 分。
- 阳性体征或主要阴性体征描述不准确，扣 3 分。
- 缺少专科检查记录，扣 10 分。
- 复诊缺少原有阳性体征变化或必要的体检记录，扣 20 分。

六、辅助检查

标准分值：5 分。

基本要求：
1. 记录必要的相关检查及专科检查。
2. 各项检查结果应抄写在病历中。
3. 复诊应记录需要补充的相关检查。

扣分标准：
- 初诊或复诊必需的检查缺一项，扣 2 分。
- 与诊断和鉴别诊断相关的检查结果未抄写，扣 3 分。
- 相关检查结果抄写遗漏或不准确，扣 2 分。

七、诊断

标准分值：15 分。

基本要求：
1. 已明确诊断，应写出诊断全称。
2. 未明确诊断应写待查，并提出进一步检查措施、建议或临床上首先考虑的可能诊断。
3. 复诊已明确诊断，应写出诊断。
4. 三次不能确诊，应请上级医师会诊，并注明会诊意见。

扣分标准：
- 未明确诊断病历，没有进一步检查意见和建议，扣 10 分。
- 诊断名称不全、不规范，扣 5 分。
- 复诊已明确诊断但未书写诊断，扣 10 分。
- 三次不能确诊，却未请上级医师会诊，扣 10 分。
- 上级医师会诊后，未记录检查所见、诊断和处理意见，扣 10 分。

八、处理治疗

标准分值：20 分。

基本要求：
1. 各项治疗措施应合理、正确、及时。
2. 处方应写药物全称、用量、用法等，符合处方规则要求。
3. 需要住院治疗的，应注明住院原因。
4. 出具医学证明书，应记录其内容。
5. 在门诊实施特殊检查、治疗、手术的患者，应有患者知情同意签名，特殊检查、治疗的操作记录或手术记录。
6. 应记录与患者交代的重要事项。
7. 应按规定记录法定传染病疫情和肿瘤病例报告。

扣分标准：
- 缺治疗意见，扣 20 分。
- 各项治疗措施不合理、不正确或不及时，每项扣 3 分。
- 处方书写未按规则要求，扣 3 分。
- 处方与病历中医嘱不相符，扣 5 分。
- 未注明住院原因，扣 5 分。
- 出具医学证明书与本次疾病不相符，扣 5 分。
- 缺少特殊检查（治疗）操作、手术的记录，扣 10 分。
- 特殊检查、治疗或手术缺少患者签名，扣 5 分。
- 缺少应当向患者交代的重要事项记录，扣 5 分。
- 未按规定报告传染病疫情和肿瘤病例，每项扣 4 分。

九、医师签名

标准分值：5 分。

基本要求：
1. 医师应签全名，且能够清楚辨认。
2. 实习医师、试用期医师书写病历，应有上级医师签名。

扣分标准：
- 医师未签名，扣 5 分。
- 签名无法辨认或未签全名，扣 3 分。
- 上级医师无签名，扣 3 分。

十、病历书写要求

标准分值：5 分。

基本要求：
1. 使用蓝黑墨水、碳素墨水及蓝色或黑色油水圆珠笔书写。
2. 文字工整、字迹清楚、整洁，不得涂改。

扣分标准：
- 字迹潦草，无法辨认，扣 3 分。
- 随意书写影响整洁，扣 3 分。
- 字段涂改不符合规定，扣 3 分。
$standard$,
  'published',
  NOW()
)
ON CONFLICT (code, version) DO UPDATE SET
  name = EXCLUDED.name,
  source_name = EXCLUDED.source_name,
  source_reference = EXCLUDED.source_reference,
  standard_text = EXCLUDED.standard_text,
  status = EXCLUDED.status,
  published_at = EXCLUDED.published_at,
  updated_at = NOW();

DO $$
BEGIN
  IF NOT EXISTS (
    SELECT 1
    FROM public.emr_score_standards
    WHERE code = 'outpatient_record_quality'
      AND version = '2017'
      AND status = 'published'
      AND BTRIM(standard_text) <> ''
  ) THEN
    RAISE EXCEPTION '门诊病历评分标准写入失败或内容为空';
  END IF;
END $$;
