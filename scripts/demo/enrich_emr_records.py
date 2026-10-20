# -*- coding: utf-8 -*-
import os, json, psycopg2
from psycopg2.extras import Json

CASES = {
"王雨桐": {
 "chief_complaint":"双眼视物模糊8年，要求术前检查。",
 "present_illness":"患者8年前无明显诱因出现双眼视物模糊，戴镜矫正后视力可，近2年近视度数稳定。今日到院行近视手术术前评估。否认眼红、眼痛、畏光、流泪，否认视野缺损及眼前黑影飘动。",
 "past_history":"既往体健。否认高血压、糖尿病、心脏病史。否认眼部手术及外伤史。",
 "personal_history":"长期佩戴软性隐形眼镜，近2周已停戴。无吸烟饮酒史。",
 "family_history":"父母均有近视，否认家族性眼病。",
 "allergy_history":"否认药物及食物过敏史。",
 "physical_exam":"双眼视力：裸眼0.08/0.08，矫正1.0/1.0；眼压15/16mmHg；睑结膜无充血，角膜透明，前房深度3.2mm，瞳孔圆，对光反射灵敏；眼底视盘边界清，C/D 0.3，黄斑区未见异常。",
 "auxiliary_exam":"角膜地形图：右眼中央厚度523μm，左眼520μm；暗瞳6.1mm；泪膜破裂时间5秒；眼轴右26.8mm、左26.6mm；角膜内皮细胞计数正常。",
 "diagnosis":{"diagnosis_name":"双眼近视伴散光","icd10_code":"H52.201"},
 "treatment_plan":"拟行全飞秒SMILE手术；术前左氧氟沙星滴眼液点眼3天，术前1天加用玻璃酸钠；术后按频次点药，1周、1个月、3个月复查。",
 "prescription":"左氧氟沙星滴眼液 0.5% 点双眼 每日4次 术前3天；玻璃酸钠滴眼液 点双眼 每日4次 术前1天起。",
 "medical_advice":"术前避免佩戴隐形眼镜，手术当日不要化妆；术后1周内避免揉眼、游泳，夜间外出可戴防护镜；如出现眼痛、视力骤降及时就诊。",
 "followup":"术后1天、1周、1个月、3个月复查；干眼明显时加用人工泪液。",
 "reason":"术前检查数据完整，术式与角膜条件匹配；干眼风险需在术前宣教中说明。",
 "quality":[("GEN-001","pass",0,"各项医疗信息完整。"),("CC-001","pass",0,"主诉、现病史完整。"),("HIS-001","pass",0,"既往史、过敏史已记录。")],
},
"张子涵": {
 "chief_complaint":"身高增长缓慢2年，家长要求评估。",
 "present_illness":"患儿近2年身高增长约4cm/年，明显低于同龄儿童，家长诉其挑食、睡眠偏晚。无头痛、呕吐、视力下降，无多饮多尿。足月顺产，出生身长50cm。",
 "past_history":"既往体健，否认慢性病、长期用药史。",
 "personal_history":"足月顺产，母乳喂养至10个月，添加辅食后偏食；睡眠约22:30，户外活动少。",
 "family_history":"父亲身高168cm，母亲158cm，否认家族性矮小。",
 "allergy_history":"否认药物及食物过敏史。",
 "physical_exam":"身高118cm（低于同龄P3），体重21kg；神清，面容无特殊，甲状腺不大，心肺腹未见异常；外生殖器Tanner I期。",
 "auxiliary_exam":"骨龄7.5岁；IGF-1 98ng/mL（偏低）；生长激素激发试验峰值8.2ng/mL；肝肾功能、血糖、甲功未见异常。",
 "diagnosis":{"diagnosis_name":"生长迟缓","icd10_code":"R62.501"},
 "treatment_plan":"建议儿童增高综合管理3个月，包含营养、睡眠、运动指导及生长激素评估；每3个月复评身高、体重、骨龄。",
 "prescription":"暂未予生长激素；补充维生素D 400IU 每日1次，赖氨酸颗粒按说明服用。",
 "medical_advice":"保证22:00前入睡，每日户外活动1小时，减少含糖饮料；每月固定时间测量身高并记录。",
 "followup":"3个月后复诊，复查身高、体重、IGF-1；如增长仍不足，再评估生长激素治疗。",
 "reason":"骨龄落后、IGF-1偏低，需先做3个月生活方式干预和复评，不宜直接承诺增高幅度。",
 "quality":[("GEN-001","pass",0,"信息完整。"),("CC-001","pass",0,"主诉、现病史完整。"),("HIS-001","problem",8,"生长曲线和既往治疗经过记录不完整。")],
},
"赵一诺": {
 "chief_complaint":"反复鼻痒、喷嚏、流清涕2年，加重1周。",
 "present_illness":"患儿2年来每于晨起及接触尘螨后出现鼻痒、连续喷嚏、清水样鼻涕，伴鼻塞，无发热、脓涕、头痛。近1周症状加重，夜间张口呼吸。曾用糠酸莫米松喷鼻，症状可缓解，停药后反复。",
 "past_history":"既往体健，否认哮喘、湿疹史。",
 "personal_history":"足月顺产，奶粉喂养；家中养猫，卧室地毯。",
 "family_history":"母亲有过敏性鼻炎史。",
 "allergy_history":"否认药物过敏；尘螨4级、花粉2级。",
 "physical_exam":"鼻黏膜苍白水肿，下鼻甲肿大，总鼻道见清水样分泌物；咽部无充血，扁桃体I度；心肺未见异常。",
 "auxiliary_exam":"过敏原皮肤点刺：尘螨4级、花粉2级；鼻内镜：腺样体肥大约50%，鼻道通畅；血常规嗜酸性粒细胞5.8%。",
 "diagnosis":{"diagnosis_name":"变应性鼻炎（尘螨为主）","icd10_code":"J30.401"},
 "treatment_plan":"避免过敏原，生理海水冲洗鼻腔，糠酸莫米松喷鼻4周，口服西替利嗪按体重；1个月后复评，必要时评估腺样体。",
 "prescription":"糠酸莫米松鼻喷雾剂 每侧1喷 每日1次 晨起；西替利嗪滴剂 5mg 每晚1次；生理海水鼻腔喷雾 每侧2喷 每日2次。",
 "medical_advice":"卧室撤地毯、除螨，避免接触猫毛；喷鼻药规律使用，不要症状一好就停；如出现鼻出血、头痛及时复诊。",
 "followup":"1个月后复诊，评估鼻塞、夜间张口呼吸及腺样体情况。",
 "reason":"诊断明确，先保守治疗1个月；家长担心激素依赖，需解释用量和减量方案。",
 "quality":[("GEN-001","pass",0,"信息完整。"),("CC-001","pass",0,"主诉、现病史完整。"),("HIS-001","problem",12,"既往用药疗程和腺样体分级记录不完整。")],
},
"周敏": {
 "chief_complaint":"产后42天复查，咳嗽漏尿1个月。",
 "present_illness":"患者顺产后42天，近1个月咳嗽、打喷嚏时漏尿，量少，每日需用护垫；伴下腹坠胀、腰骶酸痛，无尿频尿急尿痛，无发热。产后未行盆底康复。",
 "past_history":"孕2产1，否认慢性咳嗽、便秘史；否认糖尿病、高血压。",
 "personal_history":"产后母乳喂养，夜间喂奶2-3次，久坐抱娃。",
 "family_history":"否认家族性遗传病。",
 "allergy_history":"否认药物及食物过敏史。",
 "physical_exam":"血压112/70mmHg；腹软，下腹无压痛；妇科检查：外阴已婚已产式，阴道前壁轻度膨出，宫颈光滑，子宫前位、复旧可；盆底肌力2级，阴道壁压痛（+）。",
 "auxiliary_exam":"盆底肌电评估：前静息电位偏高，快肌收缩力2级，慢肌耐力差；腹直肌分离2指；尿常规未见异常。",
 "diagnosis":{"diagnosis_name":"产后盆底功能障碍","icd10_code":"N39.301"},
 "treatment_plan":"建议盆底肌电刺激+生物反馈10次，每周2次；配合凯格尔训练和腹式呼吸；产后6周后开始。",
 "prescription":"无特殊用药；必要时阴道润滑剂。",
 "medical_advice":"避免提重物、久站久坐；每日凯格尔训练3组；如漏尿加重或出现尿痛及时就诊。",
 "followup":"完成3次后复评肌力，10次后行盆底肌电复查。",
 "reason":"盆底肌力2级，康复治疗有指征；客户产假期间时间不固定，先约体验再排课。",
 "quality":[("GEN-001","pass",0,"信息完整。"),("CC-001","pass",0,"主诉、现病史完整。"),("HIS-001","pass",0,"既往史、过敏史已记录。")],
},
"刘洋": {
 "chief_complaint":"双眼近视10年，要求手术矫正。",
 "present_illness":"患者10年来双眼视物模糊，戴镜矫正1.0，近2年度数稳定。今日咨询近视手术，已对比外院方案。否认眼红、眼痛、畏光、流泪。",
 "past_history":"既往体健，否认眼部手术及外伤史。",
 "personal_history":"长期佩戴软性隐形眼镜，已停戴1周；吸烟每日5支。",
 "family_history":"父母近视。",
 "allergy_history":"否认药物过敏。",
 "physical_exam":"裸眼视力0.06/0.06，矫正1.0/1.0；眼压14/15mmHg；角膜透明，前房深度3.0mm，眼底未见明显异常。",
 "auxiliary_exam":"角膜厚度538/535μm，暗瞳6.5mm；角膜地形图未见圆锥角膜；泪膜破裂时间6秒。",
 "diagnosis":{"diagnosis_name":"双眼近视伴散光","icd10_code":"H52.201"},
 "treatment_plan":"可行半飞秒或全飞秒；患者因价格和排期选择外院，已告知手术风险及复查要求。",
 "prescription":"未在本院开药。",
 "medical_advice":"如在外院手术，术后按医嘱复查；出现眼痛、视力下降及时就诊。",
 "followup":"如改变决定可再评估；术后1个月可回院复查。",
 "reason":"检查资料完整，患者已明确选择外院，本次不追单。",
 "quality":[("GEN-001","pass",0,"信息完整。"),("CC-001","pass",0,"主诉、现病史完整。"),("HIS-001","pass",0,"既往史已记录。")],
},
"孙悦": {
 "chief_complaint":"面部松弛、法令纹加深1年。",
 "present_illness":"患者近1年自觉面部松弛，法令纹、眼周细纹明显，皮肤干燥。无红斑、瘙痒、疼痛。曾使用护肤品，未行医美治疗。",
 "past_history":"既往体健，否认瘢痕体质、光敏性疾病。",
 "personal_history":"长期熬夜，日晒较多，吸烟史否认。",
 "family_history":"否认家族性皮肤病。",
 "allergy_history":"否认药物及护肤品过敏。",
 "physical_exam":"面部皮肤光老化，颧部色斑，法令纹深，下颌缘轻度松弛；皮肤屏障功能下降，无破溃、无感染。",
 "auxiliary_exam":"VISIA皮肤检测：胶原密度偏低， UV色斑明显，毛孔粗大；无水光、无创治疗禁忌。",
 "diagnosis":{"diagnosis_name":"面部皮肤光老化","icd10_code":"L57.801"},
 "treatment_plan":"建议医学细胞焕颜3次，间隔28天；联合屏障修复和防晒；首次治疗前做过敏测试。",
 "prescription":"重组人表皮生长因子凝胶 外用 每日2次；医用修复敷料 每晚1次。",
 "medical_advice":"治疗后48小时避免化妆、暴晒；严格防晒；如出现持续红肿、水疱及时复诊。",
 "followup":"每次治疗后1周复诊评估恢复情况，3次后评估效果。",
 "reason":"皮肤检测支持光老化和屏障受损，先修复再治疗；客户担心不自然，需案例和面诊。",
 "quality":[("GEN-001","pass",0,"信息完整。"),("CC-001","pass",0,"主诉、现病史完整。"),("HIS-001","problem",6,"既往医美治疗史记录不完整。")],
},
"李静": {
 "chief_complaint":"双眼干涩、异物感1年，加重1个月。",
 "present_illness":"患者1年来双眼干涩、异物感、烧灼感，下午及用眼后加重，伴间歇性视物模糊，无眼痛、畏光、流泪。近1个月每日使用电脑10小时。曾自行滴用人工泪液，症状反复。",
 "past_history":"既往体健，否认类风湿、干燥综合征。",
 "personal_history":"长期佩戴隐形眼镜，已停戴3个月；每日屏幕时间10小时。",
 "family_history":"否认家族性眼病。",
 "allergy_history":"未记录。",
 "physical_exam":"双眼视力0.8/0.8，矫正1.0；眼压13/14mmHg；睑缘充血，睑板腺开口堵塞，角膜透明，前房正常，眼底未见异常。",
 "auxiliary_exam":"泪膜破裂时间4秒，Schirmer I试验5mm/5min；睑板腺红外成像：上睑腺体缺失约1/3；共聚焦显微镜示睑板腺功能障碍。",
 "diagnosis":{"diagnosis_name":"双眼干眼症（睑板腺功能障碍）","icd10_code":"H04.101"},
 "treatment_plan":"OPT强脉冲光治疗，每3周1次，共3次；配合睑缘清洁、热敷和人工泪液。",
 "prescription":"玻璃酸钠滴眼液 0.3% 点双眼 每日4次；睑缘清洁湿巾 每日1次。",
 "medical_advice":"减少屏幕时间，每40分钟休息；热敷每日2次；治疗后1周避免暴晒；如眼痛、视力下降及时就诊。",
 "followup":"3周后复诊行第2次OPT，治疗结束后复查泪膜破裂时间。",
 "reason":"干眼诊断明确，OPT有指征；病历过敏史未记录，需补全后归档。",
 "quality":[("GEN-001","pass",0,"信息基本完整。"),("CC-001","pass",0,"主诉、现病史完整。"),("HIS-009","problem",15,"过敏史未记录，病历被退回补全。")],
},
"陈致远": {
 "chief_complaint":"发现血压升高8年，头晕1周。",
 "present_illness":"患者8年前体检发现血压升高，最高160/100mmHg，长期口服氨氯地平5mg每日1次。近2周自行减为半片，今测血压150/95mmHg，伴轻度头晕，无胸痛、心悸、肢体无力、言语不清。",
 "past_history":"高血压8年，否认糖尿病、冠心病、脑卒中史。",
 "personal_history":"吸烟20年，每日10支；饮酒少量；口味偏咸。",
 "family_history":"父亲有高血压、脑卒中史。",
 "allergy_history":"否认药物及食物过敏。",
 "physical_exam":"血压150/95mmHg，心率76次/分；神清，颈静脉无怒张，双肺呼吸音清，心界不大，律齐，未闻及杂音；腹软，双下肢无水肿；神经系统查体阴性。",
 "auxiliary_exam":"心电图：窦性心律，左室高电压；尿常规：蛋白（-）；肾功能：肌酐86μmol/L，eGFR 82；血脂：LDL-C 3.6mmol/L；眼底：动脉变细。",
 "diagnosis":{"diagnosis_name":"原发性高血压2级（高危）","icd10_code":"I10.x00"},
 "treatment_plan":"恢复氨氯地平5mg每日1次，加用缬沙坦80mg每日1次；低盐低脂饮食，家庭血压监测；2周后复诊，必要时调整。",
 "prescription":"苯磺酸氨氯地平片 5mg 口服 每日1次；缬沙坦胶囊 80mg 口服 每日1次；阿托伐他汀钙片 20mg 每晚1次。",
 "medical_advice":"每日早晚测血压并记录；不要自行减药；如血压≥180/110或出现胸痛、肢体无力立即就诊；限制饮酒、戒烟。",
 "followup":"2周后复诊，复查血压、肾功能、血脂；3个月后评估靶器官。",
 "reason":"血压控制不佳，存在自行减药；需恢复用药并做依从性随访。",
 "quality":[("GEN-001","pass",0,"信息完整。"),("CC-001","pass",0,"主诉、现病史完整。"),("HIS-001","pass",0,"既往史、过敏史已记录。")],
},
"郑好": {
 "chief_complaint":"身高增长缓慢1年，家长要求评估。",
 "present_illness":"患儿近1年身高增长约3cm，明显低于同龄，家长诉其偏食、入睡晚。无头痛、呕吐、视物模糊，无多饮多尿。足月顺产，出生身长49cm。",
 "past_history":"既往体健，否认慢性病及长期用药。",
 "personal_history":"足月顺产，混合喂养，偏食；睡眠约23:00，户外活动少。",
 "family_history":"父亲170cm，母亲156cm，否认家族性矮小。",
 "allergy_history":"否认药物及食物过敏。",
 "physical_exam":"身高103cm（低于P3），体重16kg；神清，面容无特殊，甲状腺不大，心肺腹未见异常；外生殖器Tanner I期。",
 "auxiliary_exam":"骨龄4岁；IGF-1 85ng/mL；生长激素激发试验峰值7.8ng/mL；甲功、肝肾功能未见异常；垂体MRI未见占位。",
 "diagnosis":{"diagnosis_name":"生长迟缓","icd10_code":"R62.501"},
 "treatment_plan":"建议6个月综合管理，包含营养、睡眠、运动及生长激素评估；每3个月复评身高、体重、骨龄。",
 "prescription":"维生素D 400IU 每日1次；赖氨酸颗粒按说明服用。",
 "medical_advice":"21:30前入睡，每日户外活动1小时，保证蛋白质摄入；每月固定测量身高。",
 "followup":"3个月后复诊，复查身高、IGF-1；6个月评估是否需要生长激素。",
 "reason":"骨龄明显落后，需长期管理；家长担心打针和接送，先解释复评节点。",
 "quality":[("GEN-001","pass",0,"信息完整。"),("CC-001","pass",0,"主诉、现病史完整。"),("HIS-001","pass",0,"既往史已记录。")],
},
"许静": {
 "chief_complaint":"月经周期紊乱3个月，伴痛经。",
 "present_illness":"患者近3个月月经周期20-40天不等，经量少、色暗，伴下腹冷痛、血块，热敷后稍缓解；无发热、异常阴道出血。近半年工作压力大，作息不规律。",
 "past_history":"既往月经规律，否认妇科手术史、盆腔炎史。",
 "personal_history":"未婚，无性生活；长期熬夜，喜冷饮。",
 "family_history":"母亲有痛经史。",
 "allergy_history":"否认药物过敏。",
 "physical_exam":"血压108/68mmHg；面色偏暗，舌暗红、苔薄白，脉弦细；腹软，下腹无压痛；妇科未行内诊。",
 "auxiliary_exam":"妇科B超：子宫及双附件未见器质性病变，内膜5mm；性激素六项：FSH 6.2、LH 5.8、E2 45pg/mL、P 0.8ng/mL，基本正常；血常规未见贫血。",
 "diagnosis":{"diagnosis_name":"月经不调（气滞血瘀证）","icd10_code":"N92.601"},
 "treatment_plan":"中医调理10次，隔日1次；配合规律作息、避寒凉；3次后复评痛经和周期。",
 "prescription":"中药免煎颗粒（当归、川芎、赤芍、香附等）每日1剂，分2次温服；具体方药随证调整。",
 "medical_advice":"经期避免生冷、剧烈运动；记录月经周期和痛经评分；如出现经量骤增、持续腹痛及时就诊。",
 "followup":"3次后复诊评估痛经，10次后复查月经周期；必要时复查B超。",
 "reason":"排除器质性病变，中医辨证明确；客户担心中药慢，先做3次评估。",
 "quality":[("GEN-001","pass",0,"信息完整。"),("CC-001","pass",0,"主诉、现病史完整。"),("HIS-001","pass",0,"既往史已记录。")],
},
"高远": {
 "chief_complaint":"面部松弛、眼周细纹2年。",
 "present_illness":"患者近2年面部松弛、眼周细纹明显，肤色不均，皮肤干燥。无红斑、瘙痒、疼痛。曾使用抗衰护肤品，未行医美治疗。",
 "past_history":"既往体健，否认瘢痕体质、光敏性疾病。",
 "personal_history":"长期出差，日晒多，吸烟10年每日10支。",
 "family_history":"否认家族性皮肤病。",
 "allergy_history":"否认药物及护肤品过敏。",
 "physical_exam":"面部皮肤光老化，眼周细纹，法令纹深，下颌缘松弛；皮肤屏障一般，无破溃、感染。",
 "auxiliary_exam":"VISIA：胶原流失明显，UV色斑、毛孔粗大；无水光、激光治疗禁忌。",
 "diagnosis":{"diagnosis_name":"面部皮肤光老化","icd10_code":"L57.801"},
 "treatment_plan":"建议医学细胞焕颜5次，间隔28天；先做单次体验，确认恢复期后再排疗程；联合屏障修复和防晒。",
 "prescription":"重组人表皮生长因子凝胶 外用 每日2次；医用修复敷料 每晚1次。",
 "medical_advice":"治疗前后严格防晒；48小时内避免化妆、剧烈运动；如持续红肿、水疱及时复诊。",
 "followup":"单次体验后1周复诊，评估恢复和效果后再决定是否做5次。",
 "reason":"皮肤光老化明确，但客户下月连续出差，先单次体验，不卖5次。",
 "quality":[("GEN-001","pass",0,"信息完整。"),("CC-001","pass",0,"主诉、现病史完整。"),("HIS-001","problem",8,"既往医美治疗史和吸烟史记录不完整。")],
},
"唐糖": {
 "chief_complaint":"双眼高度近视15年，要求ICL术前评估。",
 "present_illness":"患者15年来双眼高度近视，戴镜矫正0.8，近2年度数稳定。今日行ICL术前检查。否认眼红、眼痛、畏光、流泪，否认视野缺损。",
 "past_history":"既往体健，否认眼部手术及外伤史。",
 "personal_history":"长期佩戴软性隐形眼镜，已停戴2周；运动以跑步为主。",
 "family_history":"父母高度近视。",
 "allergy_history":"否认药物过敏。",
 "physical_exam":"裸眼视力0.02/0.02，矫正0.8/0.8；眼压16/17mmHg；角膜透明，前房深度3.1mm，瞳孔圆，眼底视盘边界清，C/D 0.2，黄斑区未见异常。",
 "auxiliary_exam":"角膜内皮细胞计数2850/mm²；眼轴29.5/29.3mm；前房角开放；角膜厚度520/518μm；B超未见视网膜脱离。",
 "diagnosis":{"diagnosis_name":"双眼高度近视","icd10_code":"H52.101"},
 "treatment_plan":"拟行ICL晶体植入术；术前点左氧氟沙星3天；术后1天、1周、1个月、3个月复查；前2周避免剧烈运动。",
 "prescription":"左氧氟沙星滴眼液 0.5% 点双眼 每日4次 术前3天；玻璃酸钠滴眼液 术后按需。",
 "medical_advice":"术前停戴隐形眼镜；术后1周避免揉眼、游泳，1个月内避免对抗性运动；如眼痛、视力下降及时就诊。",
 "followup":"术后1天、1周、1个月、3个月复查；每年查眼底。",
 "reason":"高度近视、前房深度足够，ICL有指征；需重点交代晶体移位和夜间光圈。",
 "quality":[("GEN-001","pass",0,"信息完整。"),("CC-001","pass",0,"主诉、现病史完整。"),("HIS-001","pass",0,"既往史、过敏史已记录。")],
},
}

def main():
    c=psycopg2.connect(os.environ['DATABASE_URL']);cur=c.cursor()
    cur.execute("select current_database()")
    if cur.fetchone()[0]!='lingce_dev': raise SystemExit('not dev')
    cur.execute("select id::text,rule_code,section_code from emr_quality_rule_definitions where rule_code in ('GEN-001','CC-001','CC-005','HIS-001','HIS-009')")
    rules={r[1]:(r[0],r[2]) for r in cur.fetchall()}
    cur.execute("""select e.id,e.patient_snapshot_json->>'name',e.visit_type,e.template_id,e.encounter_id,e.doctor_id,e.patient_snapshot_json from emr_records e where e.tenant_id=1""")
    for rid,name,visit,tpl,enc,doc,snap in cur.fetchall():
        case=CASES.get(name)
        if not case: continue
        content={k:v for k,v in case.items() if k not in ('reason','quality')}
        if visit=='复诊':
            content['chief_complaint']=f"复诊：{case['chief_complaint']}"
            content['present_illness']='患者按上次方案回院复诊。'+case['present_illness']
            content['medical_advice']='继续按上次方案执行；如出现不适及时就诊。'
            content['followup']='1周后电话随访，1个月后复诊。'
        cur.execute("update emr_records set content_json=%s,current_score_status='已评分' where id=%s",(Json(content),rid))
        cur.execute("update emr_record_snapshots set content=%s,patient_snapshot=%s where record_id=%s",(Json(content),Json(snap),rid))
        cur.execute("select id from emr_quality_runs where record_id=%s",(rid,))
        run=cur.fetchone()
        if run:
            runid=run[0]
            cur.execute("delete from emr_quality_results where run_id=%s",(runid,))
            cur.execute("update emr_quality_runs set overall_result=%s,status='completed' where id=%s",('has_problem' if any(x[1]=='problem' for x in case['quality']) else 'no_problem',runid))
            for code,result,deduction,msg in case['quality']:
                if code not in rules: continue
                rule_id,section=rules[code]
                cur.execute("""insert into emr_quality_results(run_id,rule_id,rule_code,section_code,result,execution_kind,evidence,reason,deduction,message,blocking,manual_review,created_at)
                values(%s,%s,%s,%s,%s,'program','[]'::jsonb,%s,%s,%s,false,false,now())""",(runid,rule_id,code,section,result,case['reason'],deduction,msg))
    c.commit();print('emr enriched');cur.close();c.close()

if __name__=='__main__': main()
