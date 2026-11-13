-- 文书中心：模板库 + 就诊文书（8 类门诊文书）。
-- 说明：his_documents 已在 20261107_081 建立，本迁移只做增量扩展，保持既有签名/归档接口兼容。

CREATE TABLE IF NOT EXISTS his_document_templates (
    id BIGSERIAL PRIMARY KEY,
    tenant_id BIGINT NOT NULL REFERENCES tenants(id),
    code TEXT NOT NULL,
    name TEXT NOT NULL,
    category TEXT NOT NULL DEFAULT '',
    body_template TEXT NOT NULL DEFAULT '',
    fields_json JSONB NOT NULL DEFAULT '[]'::jsonb,
    active BOOLEAN NOT NULL DEFAULT TRUE,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    UNIQUE (tenant_id, code)
);
CREATE INDEX IF NOT EXISTS idx_his_document_templates_tenant
    ON his_document_templates (tenant_id, active, code);

ALTER TABLE his_documents ADD COLUMN IF NOT EXISTS template_code TEXT NOT NULL DEFAULT '';
ALTER TABLE his_documents ADD COLUMN IF NOT EXISTS title TEXT NOT NULL DEFAULT '';
ALTER TABLE his_documents ADD COLUMN IF NOT EXISTS content_html TEXT NOT NULL DEFAULT '';
ALTER TABLE his_documents ADD COLUMN IF NOT EXISTS data_json JSONB NOT NULL DEFAULT '{}'::jsonb;
ALTER TABLE his_documents ADD COLUMN IF NOT EXISTS created_by BIGINT;

CREATE INDEX IF NOT EXISTS idx_his_documents_encounter_center
    ON his_documents (tenant_id, encounter_id, template_code);

-- 8 个模板：ABC 5 个（正文照 abc.html DOC_TEMPLATES 条款）+ 派特 3 个（医疗文书体系 / 门诊运营流程手册）。
WITH templates(code, name, category, body_template, fields_json) AS (
    VALUES
    ('infusion_notice', '静脉输液知情同意书', '知情同意', $doc$
<h3>一、治疗介绍</h3>
<p>静脉输液是一种常见的治疗方式，通过将药物直接输入静脉血管内，使药物快速进入血液循环，达到治疗疾病的目的。本治疗过程需在严格的医疗指导和监测下进行。</p>
<h3>二、风险与注意事项</h3>
<p>1. 输液反应：少数患者可能出现发热、寒战、皮疹等；严重者可能出现过敏性休克。</p>
<p>2. 药物过敏反应：如出现皮疹、瘙痒、呼吸困难等，应立即停止输液并告知医护人员。</p>
<p>3. 感染：输液过程中需严格执行无菌操作，如出现穿刺部位红肿、疼痛，请及时告知。</p>
<p>4. 循环系统影响：输液速度过快可能引起心悸、胸闷等不适，请勿自行调节滴速。</p>
<p>本次输液方案：</p>
<div class="doc-items">{{prescription_items}}</div>
<h3>三、签署同意书</h3>
<p>我充分了解以上信息，自愿接受静脉输液治疗。我明白输液的风险和注意事项，并同意遵循医生的指导和建议。本知情同意书经我本人阅读（或由医生宣读）后签署。</p>
<div class="doc-sign"><span>患者签名：____________</span><span>日期：____年__月__日</span></div>
<div class="doc-sign"><span>医师签名：____________</span><span>日期：____年__月__日</span></div>
$doc$, $json$[]$json$::jsonb),

    ('antibiotic_notice', '抗生素使用知情同意书', '知情同意', $doc$
<h3>一、诊断与治疗方案</h3>
<p>经医生诊断，您患有 {{diagnosis}}，需要使用抗生素进行治疗。医生为您开具的抗生素及使用方式如下：</p>
<div class="doc-items">{{prescription_items}}</div>
<h3>二、注意事项</h3>
<p>1. 过敏反应：使用抗生素可能导致过敏反应，包括皮疹、瘙痒、呼吸困难、过敏性休克等。如有过敏史，请立即告知医生。</p>
<p>2. 肝肾损害：大多数抗生素需经肝脏代谢、肾脏排泄，可能导致肝肾功能损害。</p>
<p>3. 胃肠道反应：可引起恶心、呕吐、腹泻等。</p>
<p>4. 血液系统：可能影响血细胞生成，用药期间建议复查血常规。</p>
<h3>三、签署同意书</h3>
<p>我已了解抗生素使用的风险与注意事项，同意在医生指导下使用，并愿意配合用药期间的观察与复查。</p>
<div class="doc-sign"><span>患者签名：____________</span><span>日期：____年__月__日</span></div>
<div class="doc-sign"><span>医师签名：____________</span><span>日期：____年__月__日</span></div>
$doc$, $json$[]$json$::jsonb),

    ('herbal_notice', '中药煎煮、临方定制加工及理疗服务知情同意书', '知情同意', $doc$
<h3>一、服务内容</h3>
<p>您选择的中药煎煮/临方定制加工/理疗服务，由本机构药房或治疗室按处方与操作规程完成。</p>
<p>本次服务项目：{{treatment_items}}</p>
<h3>二、风险提示</h3>
<p>1. 中药煎煮可能出现药液浓度差异、沉淀等；</p>
<p>2. 临方定制加工可能因个体差异出现口感、服用不便；</p>
<p>3. 理疗可能出现局部皮肤发红、轻微灼热感。</p>
<h3>三、签署同意书</h3>
<p>我已了解上述服务内容与风险，同意接受相关服务。</p>
<div class="doc-sign"><span>患者签名：____________</span><span>日期：____年__月__日</span></div>
<div class="doc-sign"><span>医师签名：____________</span><span>日期：____年__月__日</span></div>
$doc$, $json$[
  {"key":"treatment_items","label":"本次服务项目","type":"textarea","required":false,"default":"{{treatment_items}}","placeholder":"中药煎煮 / 临方定制加工 / 理疗"}
]$json$::jsonb),

    ('diagnosis_certificate', '诊断证明书', '证明', $doc$
<p>兹证明患者 {{patient_name}}（性别：{{gender}}，年龄：{{age}}）于 {{date}} 在我院就诊，经检查诊断如下：</p>
<p class="doc-em">诊断：{{diagnosis}}</p>
<p>建议：{{advice}}</p>
<p>本证明仅用于病情说明，不作为其他用途。</p>
<div class="doc-sign"><span>医师签名：{{doctor_name}}</span><span>日期：{{date}}</span></div>
$doc$, $json$[
  {"key":"advice","label":"建议","type":"textarea","required":true,"default":"{{advice}}","placeholder":"休息、对症治疗，避免劳累，一周后复诊。"},
  {"key":"date","label":"开具日期","type":"date","required":true,"default":"{{date}}"}
]$json$::jsonb),

    ('sick_leave', '病假条', '证明', $doc$
<p>兹证明患者 {{patient_name}} 因 {{diagnosis}} 需休息治疗，建议休息 {{sick_leave_days}} 天（自 {{start_date}} 起至 {{end_date}} 止）。</p>
<p>特此证明。</p>
<div class="doc-sign"><span>医师签名：{{doctor_name}}</span><span>日期：{{date}}</span></div>
$doc$, $json$[
  {"key":"sick_leave_days","label":"休息天数","type":"number","required":true,"default":"3"},
  {"key":"start_date","label":"开始日期","type":"date","required":true,"default":"{{date}}"},
  {"key":"end_date","label":"结束日期","type":"date","required":true,"default":"{{end_date}}"},
  {"key":"advice","label":"医嘱/建议","type":"textarea","required":false,"default":"{{advice}}"}
]$json$::jsonb),

    ('gyn_consent', '妇科专项知情同意书', '知情同意', $doc$
<h3>病情说明</h3>
<p>姓名：{{patient_name}}　　性别：{{gender}}　　年龄：{{age}}　　联系电话：{{phone}}　　身份证号码：{{id_card}}</p>
<p>经检查诊断为：{{diagnosis}}，拟行 {{treatment_items}} 治疗。</p>
<p>治疗目的：{{treatment_purpose}}</p>
<h3>治疗相关风险告知</h3>
<p>1. 常规不良反应：{{routine_reactions}}</p>
<p>2. 潜在风险：{{potential_risks}}</p>
<p>3. 疗效说明：临床疗效存在个体差异，无法保证完全治愈。</p>
<p>替代治疗方案：可选择①保守观察、定期复查；②至上级医疗机构进一步检查及治疗。已告知不同方案各自利弊以及不接受治疗的相关风险。</p>
<h3>患者知情声明</h3>
<p>本人已清楚知晓病情、治疗方案、风险、获益及替代方案，自愿接受本次治疗，遵医嘱复诊，如出现不适及时就诊。</p>
<p>{{disclosure}}</p>
<div class="doc-sign"><span>患者签字：____________</span><span>日期：____年__月__日</span></div>
<div class="doc-sign"><span>代理人签字：____________</span><span>与患者关系：____________</span><span>日期：____年__月__日</span></div>
<div class="doc-sign"><span>医师签字：{{doctor_name}}</span><span>日期：{{date}}</span></div>
$doc$, $json$[
  {"key":"treatment_items","label":"拟行治疗项目","type":"textarea","required":true,"default":"{{treatment_items}}","placeholder":"如：红光治疗、阴道冲洗上药等"},
  {"key":"treatment_purpose","label":"治疗目的","type":"textarea","required":true,"default":"","placeholder":"填写本次治疗目的"},
  {"key":"routine_reactions","label":"常规不良反应","type":"textarea","required":true,"default":"","placeholder":"填写药物/操作常见轻微不适"},
  {"key":"potential_risks","label":"潜在风险","type":"textarea","required":true,"default":"","placeholder":"填写严重不良事件、病情加重、出血、感染等"},
  {"key":"disclosure","label":"补充告知内容","type":"textarea","required":false,"default":"","placeholder":"其他需要患者知情的说明"}
]$json$::jsonb),

    ('referral', '转诊单', '转诊', $doc$
<p>姓名：{{patient_name}}　　性别：{{gender}}　　年龄：{{age}}</p>
<p>转诊日期：{{date}}</p>
<p>简要病史及检查结果：{{referral_summary}}</p>
<p>转诊原因：{{referral_reason}}</p>
<p>建议转诊机构/科室：{{target_hospital}}</p>
<p>转诊注意事项：{{referral_notes}}</p>
<p>携带资料清单：☐门诊病历　☐检验报告单　☐影像报告　☐其他：{{carried_documents}}</p>
<p>患者知情告知：已告知转诊目的、风险，患者同意转诊　☐是</p>
<div class="doc-sign"><span>患者/家属签名：____________</span></div>
<div class="doc-sign"><span>转出医师签名：{{doctor_name}}</span><span>日期：{{date}}</span></div>
<p>接诊医师签名（能获取则填）：____________</p>
<p class="doc-note">第一联：医疗机构留存；第二联：患者携带</p>
$doc$, $json$[
  {"key":"referral_summary","label":"简要病史及检查结果","type":"textarea","required":true,"default":"{{referral_summary}}"},
  {"key":"referral_reason","label":"转诊原因","type":"textarea","required":true,"default":"","placeholder":"填写转诊原因"},
  {"key":"target_hospital","label":"建议转诊机构/科室","type":"text","required":true,"default":"","placeholder":"如：XX 医院 妇科"},
  {"key":"referral_notes","label":"转诊注意事项","type":"textarea","required":false,"default":""},
  {"key":"carried_documents","label":"其他携带资料","type":"text","required":false,"default":"门诊病历、检验报告单、影像报告"}
]$json$::jsonb),

    ('result_notice', '结果告知与下一步记录', '告知', $doc$
<h3>一、结果类型</h3>
<p>结果类型：{{result_type}}</p>
<p>结果（按审核版本）：{{result_summary}}</p>
<p>分型/相关结果：{{result_subtype}}</p>
<h3>二、结果边界说明</h3>
<p>能说明什么：{{result_meaning}}</p>
<p>不能说明什么：{{result_limits}}</p>
<h3>三、患者最担心的问题</h3>
<p>患者最担心的问题：{{patient_concern}}</p>
<p>处理情况：{{concern_response}}</p>
<h3>四、情绪状态与异常出口</h3>
<p>情绪状态：{{emotion_status}}</p>
<p>异常出口（转心理支持/绿通/复诊）：{{abnormal_exit}}</p>
<h3>五、下一步与责任人</h3>
<p>下一节点：{{next_step}}</p>
<p>复查时间：{{next_visit_date}}</p>
<p>责任人：{{owner}}</p>
<p>联系授权：{{contact_authorization}}</p>
<h3>六、告知内容</h3>
<p>{{disclosure}}</p>
<h3>七、花物执行</h3>
<p>花物：{{flower_type}}</p>
<p>患者选择（领取/拒绝/纸质/电子/扫码/分享）：{{flower_choice}}</p>
<div class="doc-audit"><span>填写人：{{doctor_name}}</span><span>日期：{{date}}</span><span>审核人：____________</span><span>审核日期：____________</span></div>
<div class="doc-cut">（以下为患者留存页，可沿虚线撕下）</div>
<h3>患者留存页 · 结果与下一步摘要</h3>
<p>本次结果：{{result_summary}}</p>
<p>这个结果说明什么：{{result_meaning}}</p>
<p>还需要观察/确认：{{result_limits}}</p>
<p>下一步：{{next_step}}</p>
<p>复查时间：{{next_visit_date}}</p>
<p>联系人：{{owner}}</p>
<p>出现哪些情况要提前联系：{{abnormal_exit}}</p>
$doc$, $json$[
  {"key":"result_type","label":"结果类型","type":"text","required":false,"default":"本诊所首次 HPV 阳性告知"},
  {"key":"result_summary","label":"结果（按审核版本）","type":"textarea","required":true,"default":"{{diagnosis}}"},
  {"key":"result_subtype","label":"分型/相关结果","type":"text","required":false,"default":""},
  {"key":"result_meaning","label":"能说明什么","type":"textarea","required":true,"default":""},
  {"key":"result_limits","label":"不能说明什么","type":"textarea","required":true,"default":""},
  {"key":"patient_concern","label":"患者最担心的问题","type":"textarea","required":false,"default":""},
  {"key":"concern_response","label":"处理情况","type":"textarea","required":false,"default":""},
  {"key":"emotion_status","label":"情绪状态","type":"text","required":false,"default":"稳定"},
  {"key":"abnormal_exit","label":"异常出口（转心理支持/绿通/复诊）","type":"textarea","required":false,"default":""},
  {"key":"next_step","label":"下一节点","type":"textarea","required":true,"default":"{{advice}}"},
  {"key":"next_visit_date","label":"复查时间","type":"date","required":false,"default":""},
  {"key":"owner","label":"责任人","type":"text","required":false,"default":"{{doctor_name}}"},
  {"key":"contact_authorization","label":"联系授权","type":"text","required":false,"default":""},
  {"key":"disclosure","label":"告知内容","type":"textarea","required":false,"default":""},
  {"key":"flower_type","label":"花物","type":"text","required":false,"default":""},
  {"key":"flower_choice","label":"患者选择（领取/拒绝/纸质/电子/扫码/分享）","type":"text","required":false,"default":""}
]$json$::jsonb)
)
INSERT INTO his_document_templates (tenant_id, code, name, category, body_template, fields_json)
SELECT t.id, tpl.code, tpl.name, tpl.category, tpl.body_template, tpl.fields_json
FROM tenants t
CROSS JOIN templates tpl
WHERE t.id IN (1, 5001)
ON CONFLICT (tenant_id, code) DO NOTHING;
