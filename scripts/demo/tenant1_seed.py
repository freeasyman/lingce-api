# -*- coding: utf-8 -*-
import json, os, sys, datetime as dt
import psycopg2
from psycopg2.extras import Json, RealDictCursor

DSN=os.environ["DATABASE_URL"]
now=dt.datetime(2026,10,20,20,30)
demo_day=dt.date(2026,10,21)
def at(day,h,m=0): return dt.datetime.combine(day,dt.time(h,m))
def ts(day,h,m=0): return at(day,h,m)

PEOPLE=[
 ("王雨桐","女",28,"眼科","周明","魏惠铭","全飞秒Pro近视手术","已付款",33800,"已归档",96,"客户已确认全飞秒Pro，计划10月23日手术。","价格已接受，但担心术后恢复和干眼。","盖珊珊今天18:00前确认术前检查与手术排期，并回复客户。","效果与适配","担心术后干眼和夜间视力。","付款"),
 ("张子涵","男",9,"儿科","陈静","林晓","茁壮挺拔·儿童增高·3个月24次","有意向但未决定",0,"待归档",88,"家长带孩子复诊，骨龄和生长激素检查已完成。","家长在比较3个月和6个月疗程，担心费用。","林晓明天上午把两个疗程的身高增长数据发给家长。","价格与价值","3个月和6个月差价大，家长要看效果数据。",""),
 ("赵一诺","女",7,"儿科","陈静","魏惠铭","儿童过敏性鼻炎·1个月","已交定金",1000,"需补全",76,"孩子过敏性鼻炎反复，医生建议先做1个月疗程。","家长担心激素和依赖性，只愿先交定金。","魏惠铭今天下班前补全过敏原检查，并解释用药安全性。","安全与恢复","担心中药/激素长期使用有依赖。","定金"),
 ("周敏","女",32,"妇科","刘芳","林晓","盆底肌健康·10次","暂缓",0,"待归档",90,"产后42天复查，盆底肌力偏低。","客户要等产假结束再安排治疗时间。","林晓下周一下午确认客户产假结束时间，再约体验课。","时间与流程","产假结束前没有固定时间到院。",""),
 ("刘洋","男",26,"眼科","周明","魏惠铭","半飞秒近视手术","明确不考虑",0,"已归档",92,"客户对比了三家机构，最后选择别家。","客户认为别家便宜3000元，且承诺当天手术。","魏惠铭记录流失原因，不再主动联系。","竞品比较","别家便宜且能当天手术。",""),
 ("孙悦","女",38,"医美","周宁","林晓","医学细胞焕颜·3次","有意向但未决定",0,"草稿",None,"客户想做细胞焕颜，担心效果维持时间。","客户对价格不敏感，主要担心做完不自然。","林晓明天下午发同年龄真实案例，并约周宁面诊。","效果与适配","担心做完不自然、维持时间短。",""),
 ("李静","女",34,"眼科","周明","魏惠铭","干眼OPT治疗","已付款",980,"退回",85,"客户干眼明显，已完成一次OPT治疗。","病历缺少过敏史，被质控退回补充。","周明今天17:00前补全过敏史并重新提交病历。","时间与流程","病历退回，影响下次治疗安排。","付款"),
 ("陈致远","男",59,"内科","孙浩","魏惠铭","","",0,"已归档",95,"高血压复诊，血压控制一般。","客户近期漏服药，需要提醒。","孙浩明天上午电话随访用药情况。","安全与恢复","担心长期吃药伤肝肾功能。",""),
 ("郑好","男",5,"儿科","陈静","林晓","茁壮挺拔·儿童增高·6个月48次","已预约",0,"已归档",90,"孩子身高低于同龄，医生建议6个月疗程。","家长担心打针和副作用。","林晓明天上午解释疗程安排和安全性。","安全与恢复","担心治疗副作用。",""),
 ("许静","女",30,"妇科","刘芳","魏惠铭","经期健康·10次","有意向但未决定",0,"草稿",None,"客户月经不规律，想调理。","客户在比较中医和西药方案。","魏惠铭本周五前发两种方案对比。","效果与适配","担心中药调理太慢。",""),
 ("高远","男",42,"医美","周宁","林晓","医学细胞焕颜·5次","暂缓",0,"待归档",87,"客户想改善面部松弛，先做一次体验。","客户要等出差回来再决定。","林晓下周三确认客户回程时间。","时间与流程","出差频繁，没有恢复时间。",""),
 ("唐糖","女",33,"眼科","周明","魏惠铭","ICL晶体植入术","已交定金",2000,"已归档",93,"高度近视，选择ICL，已完成术前检查。","客户担心晶体移位和术后护理。","周明术前一天讲解术后注意事项。","安全与恢复","担心晶体移位。","定金"),
]
status_map={"已预约":"qualified","已付款":"converted","已交定金":"qualified","有意向但未决定":"qualified","暂缓":"contacted","明确不考虑":"lost","":"contacted"}
decision_map={"已预约":"已预约","已付款":"已付款","已交定金":"已交定金","有意向但未决定":"有意向但未决定","暂缓":"暂缓","明确不考虑":"明确不考虑","":"无法判断"}
templates={"眼科":"standard_outpatient","儿科":"pediatrics","妇科":"gynecology","内科":"internal_medicine","医美":"standard_outpatient"}

def main():
    conn=psycopg2.connect(DSN); conn.autocommit=False
    cur=conn.cursor(cursor_factory=RealDictCursor)
    cur.execute("select current_database()"); db=cur.fetchone()["current_database"]
    if db!="lingce_dev": raise SystemExit("not dev db: "+db)
    cur.execute("select password_hash from employees where id=1")
    pwd=cur.fetchone()["password_hash"]
    # departments
    cur.execute("select id,code from departments where tenant_id=1")
    dept={r["id"]:r for r in cur.fetchall()}
    cur.execute("select id,code from departments where tenant_id=1 and code=%s",("eye",))
    # create sub departments if missing
    sub=[("眼科","eye",3),("儿科","pediatrics",3),("妇科","gynecology",3),("医美科","medical_beauty",3),("内科","internal",3),("随访中心","followup_center",1)]
    dept_ids={}
    for name,code,parent in sub:
        cur.execute("select id from departments where tenant_id=1 and code=%s",(code,))
        r=cur.fetchone()
        if r: dept_ids[code]=r["id"]
        else:
            cur.execute("insert into departments(tenant_id,name,parent_id,code,is_active,created_at,updated_at) values(1,%s,%s,%s,true,now(),now()) returning id",(name,parent,code))
            dept_ids[code]=cur.fetchone()["id"]
    # roles
    cur.execute("select id,code from institution_roles where tenant_id=1")
    roles={r["code"]:r["id"] for r in cur.fetchall()}
    # employees
    staff=[("赵敏","13900001001","operating_manager","运营部",4),("王丽","13900001002","customer_service","客服部",1),
           ("王倩","13900001003","customer_service","随访中心",dept_ids["followup_center"]),("魏惠铭","13900001004","consultant","销售部",16),
           ("林晓","13900001005","consultant","销售部",16),("周明","13900001006","doctor","眼科",dept_ids["eye"]),
           ("陈静","13900001007","doctor","儿科",dept_ids["pediatrics"]),("刘芳","13900001008","doctor","妇科",dept_ids["gynecology"]),
           ("周宁","13900001009","doctor","医美科",dept_ids["medical_beauty"]),("孙浩","13900001010","doctor","内科",dept_ids["internal"])]
    emp={}
    for name,phone,role,deptname,did in staff:
        cur.execute("select id from employees where phone=%s and deleted_at is null",(phone,))
        r=cur.fetchone()
        if not r:
            cur.execute("""insert into employees(tenant_id,name,full_name,phone,username,password_hash,role,department_id,is_active,session_version,session_version_institution,session_version_mobile,created_at,updated_at)
            values(1,%s,%s,%s,%s,%s,%s,%s,1,1,1,1,now(),now()) returning id""",(name,name,phone,phone,pwd,role,did))
            eid=cur.fetchone()["id"]
        else: eid=r["id"]
        emp[name]=eid
        cur.execute("insert into institution_employee_roles(tenant_id,employee_id,role_id,role_code,source,created_at,updated_at) values(1,%s,%s,%s,'demo',now(),now()) on conflict do nothing",(eid,roles.get(role),role))
    # permissions
    for name,role in [("李院长","admin"),("赵敏","operating_manager")]:
        pass
    cur.execute("select id from employees where id=1")
    admin=1
    for eid,role in [(emp["周明"],"doctor"),(emp["陈静"],"doctor"),(emp["刘芳"],"doctor"),(emp["周宁"],"doctor"),(emp["孙浩"],"doctor")]:
        cur.execute("insert into emr_permission_assignments(tenant_id,employee_id,enabled,emr_role_code,scope,abilities_json,created_by,updated_by,created_at,updated_at) values(1,%s,true,'doctor','self','[]'::jsonb,1,1,now(),now()) on conflict do nothing",(eid,))
        cur.execute("insert into followup_permission_assignments(tenant_id,employee_id,enabled,role_code,abilities_json,created_by,updated_by,created_at,updated_at) values(1,%s,true,'followup_staff','[]'::jsonb,1,1,now(),now()) on conflict do nothing",(eid,))
    for name in ["魏惠铭","林晓"]:
        cur.execute("insert into followup_permission_assignments(tenant_id,employee_id,enabled,role_code,abilities_json,created_by,updated_by,created_at,updated_at) values(1,%s,true,'followup_staff','[]'::jsonb,1,1,now(),now()) on conflict do nothing",(emp[name],))
        cur.execute("insert into patient_ops_permission_assignments(tenant_id,employee_id,enabled,role_code,abilities_json,created_by,updated_by,created_at,updated_at) values(1,%s,true,'patient_ops_staff','[]'::jsonb,1,1,now(),now()) on conflict do nothing",(emp[name],))
    # cleanup business data (soft where possible)
    cur.execute("update customers set deleted_at=now() where tenant_id=1 and deleted_at is null")
    cur.execute("update recordings set deleted_at=now() where tenant_id=1 and deleted_at is null")
    cur.execute("update emr_records set status='已作废',voided_at=now(),voided_by=1 where tenant_id=1 and status<>'已作废'")
    cur.execute("delete from recording_analysis_results where tenant_id=1")
    cur.execute("delete from deal_follow_up_events where tenant_id=1")
    cur.execute("delete from deal_outcome_events where tenant_id=1")
    cur.execute("delete from deal_follow_ups where tenant_id=1")
    cur.execute("delete from deal_expert_analyses where tenant_id=1")
    cur.execute("delete from compliance_check_item_process_records where tenant_id=1")
    cur.execute("delete from compliance_check_items where tenant_id=1")
    cur.execute("delete from compliance_findings where tenant_id=1")
    cur.execute("delete from compliance_check_runs where tenant_id=1")
    cur.execute("delete from patient_operation_task_drafts where tenant_id=1")
    cur.execute("delete from patient_operation_tasks where tenant_id=1")
    cur.execute("delete from patient_operation_records where tenant_id=1")
    cur.execute("delete from patient_operation_recording_runs where tenant_id=1")
    cur.execute("delete from patient_ops_plan_members where tenant_id=1")
    cur.execute("delete from patient_ops_plans where tenant_id=1")
    cur.execute("delete from recording_tasks where tenant_id=1")
    cur.execute("delete from followup_generation_runs where tenant_id=1")
    # seed
    product_ids={}
    cur.execute("select id,name from products where tenant_id=1")
    for r in cur.fetchall(): product_ids[r["name"]]=r["id"]
    for idx,(name,gender,age,dept,doctor,consultant,product,deal_status,amount,emr_status,score,situation,blocker,next_step,ctype,csummary,outcome) in enumerate(PEOPLE):
        day=demo_day-dt.timedelta(days=(idx%4))
        phone=f"1990000{1001+idx}"
        cur.execute("""insert into patients(tenant_id,name,phone,gender,birth_date,age,source_system,patient_category,first_visit_date,last_visit_date,visit_count,total_paid_amount,created_at,updated_at)
        values(1,%s,%s,%s,%s,%s,'demo',%s,%s,%s,1,%s,now(),now()) returning id""",(name,phone,"男" if gender=="男" else "女",dt.date(2026-age,1,1),age,dept,day,day,amount))
        pid=cur.fetchone()["id"]
        cur.execute("""insert into customers(tenant_id,name,phone,gender,lifecycle_stage,status,source,first_channel,patient_id,assigned_to,assigned_to_name,extra_data,tags,created_at,updated_at)
        values(1,%s,%s,%s,'customer',%s,'recording_auto_archive','录音自动归档',%s,%s,%s,%s,%s,now(),now()) returning id""",(name,phone,gender,status_map[deal_status],pid,emp[consultant],consultant,Json({"demo_seed":"tenant1_20261021","dept":dept}),Json([dept,product or "无"])))
        cid=cur.fetchone()["id"]
        cur.execute("""insert into encounters(tenant_id,source_type,source_id,patient_id,patient_name,provider_id,provider_name,provider_role,department_id,channel,visit_type,started_at,ended_at,status,created_at,updated_at)
        values(1,'recording',%s,%s,%s,%s,%s,'consultant',%s,'store','consultation',%s,%s,'analyzed',now(),now()) returning id""",(20000+idx,pid,name,emp[consultant],consultant,dept_ids[{"眼科":"eye","儿科":"pediatrics","妇科":"gynecology","医美":"medical_beauty","内科":"internal"}[dept]],ts(day,10),ts(day,10,40)))
        enc=cur.fetchone()["id"]
        segs=[{"speaker_role":"consultant","text":f"{consultant}：您好，我是{consultant}，今天主要想了解您对{product or '这次就诊'}的想法。","start_time":0},
              {"speaker_role":"customer","text":f"{name}：{situation}","start_time":45},
              {"speaker_role":"consultant","text":f"{consultant}：{next_step}","start_time":90}]
        text="\n".join(s["text"] for s in segs)
        cur.execute("""insert into recordings(tenant_id,employee_id,file_url,file_name,file_size,duration,mime_type,patient_id,customer_id,encounter_id,source,status,transcription_status,transcription_text,cleaned_transcription,transcription_segments,recorded_at,created_at,updated_at,notes,analysis_status,business_scope)
        values(1,%s,%s,%s,1000,120,'audio/wav',%s,%s,%s,'demo','completed','completed',%s,%s,%s,%s,now(),now(),%s,'completed','consultant') returning id""",(emp[consultant],f"demo://{idx}.wav",f"{name}.wav",pid,cid,enc,text,Json(segs),Json(segs),ts(day,10),f"demo_seed=tenant1_20261021"))
        rid=cur.fetchone()["id"]
        # EMR
        tcode=templates[dept]
        cur.execute("select id from emr_templates where code=%s and is_current=true limit 1",(tcode,))
        tpl=cur.fetchone()["id"]
        content={"chief_complaint":situation,"present_illness":situation,"past_history":"既往体健。","physical_exam":"生命体征平稳。","diagnosis":{"diagnosis_name":product or dept+"复诊"},"medical_advice":next_step,"followup":"2周后复诊","allergy_history":"否认药物过敏。"}
        snap={"name":name,"gender":gender,"age":age,"phone":phone}
        cur.execute("""insert into emr_records(tenant_id,encounter_id,customer_id,patient_snapshot_json,template_id,document_type,visit_type,department_id,doctor_id,started_at,ended_at,content_json,status,created_by,created_at,updated_at,current_score_status,current_total_score,current_grade)
        values(1,%s,%s,%s,%s,'门诊病历','初诊',%s,%s,%s,%s,%s,%s,%s,now(),now(),%s,%s,%s) returning id""",(enc,cid,Json(snap),tpl,dept_ids[{"眼科":"eye","儿科":"pediatrics","妇科":"gynecology","医美":"medical_beauty","内科":"internal"}[dept]],emp[doctor],ts(day,10),ts(day,10,50),Json(content),emr_status,emp[doctor], "已评分" if score else "未评分",score, "甲级" if score and score>=90 else ("乙级" if score and score>=80 else ("丙级" if score else None))))
        # compliance for some
        if idx in (1,2,3,4,5,6):
            rule=["communication.fear.exaggeration","communication.guarantee.duration","communication.guarantee.absolute","communication.misleading.disparage","communication.guarantee.case","communication.misleading.data_unverified"][idx-1]
            st="pending" if idx in (1,3) else ("in_progress" if idx in (5,6) else "processed")
            concl="confirmed" if st=="processed" and idx==4 else ("not_confirmed" if st=="processed" else None)
            cur.execute("""insert into compliance_check_items(tenant_id,subject,source_type,source_id,source_version,scene_code,role_code,employee_id,theme_key,question,risk_hypothesis,primary_risk_type,related_risk_types,evidence,check_basis,suggested_actions,priority,status,conclusion,process_reason,created_at,updated_at)
            values(1,'communication','recording',%s,'demo_v1','communication.consultant.consultation','consultant',%s,%s,%s,%s,%s,'[]'::jsonb,%s,'[]'::jsonb,'[]'::jsonb,%s,%s,%s,%s,now(),now())""",(str(rid),emp[consultant],rule, f"沟通中是否存在{rule}风险？", csummary, ("medical_safety" if idx in (1,3) else ("business_reputation" if idx in (2,4,5) else "legal_evidence")), Json([{"quote":segs[1]["text"],"speaker":"客户"}]), "urgent" if st=="pending" else "important", st, concl, "演示处理记录" if st=="processed" else None))
        # patient task
        tstatus="待处理" if idx<4 else ("已完成" if idx<7 else "已取消")
        cur.execute("""insert into patient_operation_tasks(tenant_id,customer_id,employee_id,task_type,goal,action,content,evidence,due_at,status,result,result_note,completed_at,dedupe_key,priority,created_at,updated_at)
        values(1,%s,%s,'咨询推进',%s,%s,%s,%s,%s,%s,%s,%s,%s,%s,5,now(),now())""",(cid,emp[consultant],next_step,next_step,situation,csummary,ts(demo_day,18) if idx<4 else ts(day,18),tstatus,"已回复" if tstatus=="已完成" else "", "演示结果" if tstatus=="已完成" else "", ts(day,18) if tstatus=="已完成" else None, f"demo_{idx}"))
        # followup task
        if idx < 8:
            fstatus="to_execute" if idx!=6 else "completed"
            cur.execute("""insert into recording_tasks(tenant_id,recording_id,customer_id,customer_name,customer_phone,assigned_to,assigned_by,title,description,script,status,priority,due_at,source_type,source_detail,encounter_id,contact_reason,doctor_name,task_category,created_at,updated_at)
            values(1,%s,%s,%s,%s,%s,'demo',%s,%s,%s,%s,'high',%s,'follow_up','demo_v1',%s,%s,%s,'care',now(),now())""",(rid,cid,name,phone,emp[consultant],next_step,blocker,next_step,fstatus,ts(demo_day,17) if idx!=6 else ts(day,17),enc,csummary,doctor))
        if product and deal_status:
            brief={"situation":situation,"blocker":blocker,"next_step":next_step,"evidence_refs":["ev_1"]}
            card={"schema_version":"deal-card-v1.1","brief":brief,"conclusion":{"current_state":situation,"business_impact":"仍可推进" if deal_status!="明确不考虑" else "已流失","main_basis":["ev_1"],"verification":"核对业务系统结果。"},"decision":{"decision_state":decision_map[deal_status],"state_basis":"transcript_judgment","evidence_refs":["ev_1"],"condition":None,"condition_due_quote":None,"trust":[],"need_state":"明确","need_summary":situation,"need_evidence_refs":["ev_1"],"concerns":[{"type":ctype,"summary":csummary,"is_primary":True,"evidence_refs":["ev_1"]}],"outcome_signals":[]},"clinical_conflict":{"active":False,"reason":None,"evidence_refs":[]},"contact_policy":"allow","product_findings":[],"primary_action":{"type":"sales_follow_up","goal":next_step,"action":next_step,"suggested_content":f"{name}您好，关于{product}，我们再确认一下下一步安排。","owner_employee_id":None,"due_basis":"today","due_quote":None,"source_evidence_refs":["ev_1"]},"secondary_findings":[],"evidence_refs":[{"id":"ev_1","quote":segs[1]["text"][:40],"speaker":"客户","source_ref":"s1","start_ms":45000,"used_for":["结论","需求"]}],"quality":{"standard_version":"compass-sales-v1.1","dimensions":{k:{"assessment":"not_applicable","score":None,"comment":"演示数据，未评分。","evidence_refs":[],"impact_on_deal":None,"highlight_narrative":None} for k in ["开场建立权威","需求探索","问题放大","专业呈现","方案定制","异议化解","促成与收尾"]}},"coach":{"successful_practices":[],"improvement":None,"refund_review":None},"confidence":"high","unknowns":[]}
            cur.execute("""insert into deal_expert_analyses(tenant_id,recording_id,encounter_id,employee_id,customer_id,customer_name,business_scope,role_code,recorded_at,cleaned_transcript,analysis_pipeline_version,prompt_version,status,result,analyzed_at,model_code,updated_at,attempts)
            values(1,%s,%s,%s,%s,%s,'consultant','consultant',%s,%s,'recording-analysis-v1','deal-expert-prompt-v1.4','analyzed',%s,now(),'demo',now(),1)""",(rid,enc,emp[consultant],cid,name,ts(day,10),text,Json(card)))
            if amount>0:
                otype="付款" if outcome=="付款" else ("定金" if outcome=="定金" else "预约")
                cur.execute("insert into deal_outcome_events(tenant_id,encounter_id,recording_id,type,amount,occurred_at,source,source_ref,created_at) values(1,%s,%s,%s,%s,%s,'demo','demo',now())",(enc,rid,otype,amount,ts(day,15)))
            cur.execute("""insert into deal_follow_ups(tenant_id,encounter_id,recording_id,customer_id,customer_name,objective,action,suggested_content,owner_employee_id,owner_name,due_at,status,created_by,created_at,updated_at)
            values(1,%s,%s,%s,%s,%s,%s,%s,%s,%s,%s,'待处理',1,now(),now())""",(enc,rid,cid,name,next_step,next_step,f"{name}您好，关于{product}再确认下一步。",emp[consultant],consultant,ts(demo_day,18)))
    # plans
    cur.execute("""insert into patient_ops_plans(tenant_id,name,plan_type,scenario,period_start,period_end,target_count,target_amount,product_ids,filter_config,status,published_at,created_by,created_at,updated_at)
    values(1,'十月儿童生长发育专场','sales','growth',%s,%s,20,60000,'[]'::jsonb,'{}'::jsonb,'published',now(),1,now(),now())""",(demo_day-dt.timedelta(days=5),demo_day+dt.timedelta(days=10)))
    cur.execute("""insert into patient_ops_plans(tenant_id,name,plan_type,scenario,period_start,period_end,target_count,target_amount,product_ids,filter_config,status,created_by,created_at,updated_at)
    values(1,'术后复查关怀','status','post_op',%s,%s,30,0,'[]'::jsonb,'{}'::jsonb,'draft',1,now(),now())""",(demo_day,demo_day+dt.timedelta(days=14)))
    conn.commit()
    print("seed committed")
    cur.execute("select count(*) c from customers where tenant_id=1 and deleted_at is null"); print("customers",cur.fetchone()["c"])
    cur.execute("select count(*) c from recordings where tenant_id=1 and deleted_at is null"); print("recordings",cur.fetchone()["c"])
    cur.execute("select count(*) c from emr_records where tenant_id=1 and status<>'已作废'"); print("emr",cur.fetchone()["c"])
    cur.execute("select count(*) c from compliance_check_items where tenant_id=1"); print("compliance",cur.fetchone()["c"])
    cur.execute("select count(*) c from patient_operation_tasks where tenant_id=1"); print("patient_tasks",cur.fetchone()["c"])
    cur.execute("select count(*) c from recording_tasks where tenant_id=1"); print("followup",cur.fetchone()["c"])
    cur.execute("select count(*) c from deal_expert_analyses where tenant_id=1 and status='analyzed'"); print("deal_cards",cur.fetchone()["c"])
    cur.close(); conn.close()
if __name__=="__main__": main()
