# -*- coding: utf-8 -*-
import os, json, psycopg2
from psycopg2.extras import Json

# name, product keyword, outcome type, amount, day
ROWS = [
 ("王雨桐","全飞秒Pro", "付款", 33800, 3), ("唐糖","ICL", "定金", 2000, 8),
 ("张子涵","儿童增高·3个月", "付款", 3600, 5), ("郑好","儿童增高·6个月", "", 0, 12),
 ("李静","干眼OPT", "付款", 980, 7), ("周敏","盆底肌", "", 0, 11),
 ("刘洋","半飞秒", "", 0, 9), ("高远","半飞秒", "", 0, 16),
 ("孙悦","细胞焕颜·3次", "付款", 3980, 10), ("高远","细胞焕颜·5次", "", 0, 18),
 ("赵一诺","儿童过敏性鼻炎", "", 0, 14), ("许静","经期健康", "", 0, 15),
 ("王雨桐","全飞秒Pro", "退款", 0, 20), ("张子涵","儿童增高·3个月", "", 0, 22),
 ("周敏","盆底肌", "", 0, 24), ("唐糖","ICL", "", 0, 26),
]

def main():
    c=psycopg2.connect(os.environ['DATABASE_URL']);cur=c.cursor()
    cur.execute("select current_database()")
    if cur.fetchone()[0]!='lingce_dev': raise SystemExit('not dev')
    cur.execute("select id,name from customers where tenant_id=1 and deleted_at is null")
    customers={r[1]:r[0] for r in cur.fetchall()}
    cur.execute("select id,name from products where tenant_id=1")
    products={r[1]:r[0] for r in cur.fetchall()}
    cur.execute("select id,name from employees where tenant_id=1 and role='consultant' and deleted_at is null order by id")
    consultants=cur.fetchall()
    cur.execute("select result from deal_expert_analyses where tenant_id=1 and status='analyzed' order by length(result::text) desc limit 1")
    template=cur.fetchone()[0]
    if isinstance(template,str): template=json.loads(template)
    cur.execute("select id from encounters where tenant_id=1 and source_id between 40000 and 40020")
    if cur.fetchall(): print('prior data exists'); return
    for i,(cust,product,outcome,amount,day) in enumerate(ROWS):
        cid=customers.get(cust); emp,name=consultants[i%len(consultants)]
        prod=next((v for k,v in products.items() if product in k), None)
        cur.execute("""insert into encounters(tenant_id,source_type,source_id,patient_id,patient_name,provider_id,provider_name,provider_role,department_id,channel,visit_type,started_at,ended_at,status,created_at,updated_at)
        values(1,'recording',%s,(select patient_id from customers where id=%s),%s,%s,%s,'consultant',3,'store','first_visit',%s,%s,'analyzed',now(),now()) returning id""",(40000+i,cid,cust,emp,name,f"2026-09-{day:02d} 10:00",f"2026-09-{day:02d} 10:40"))
        enc=cur.fetchone()[0]
        cur.execute("""insert into recordings(tenant_id,employee_id,file_url,file_name,file_size,duration,mime_type,patient_id,customer_id,encounter_id,source,status,transcription_status,transcription_text,cleaned_transcription,transcription_segments,recorded_at,created_at,updated_at,notes,analysis_status,business_scope,confirmed_deal_status)
        values(1,%s,'demo://prior.wav',%s,1000,2400,'audio/wav',(select patient_id from customers where id=%s),%s,%s,'demo','completed','completed','9月历史接待','[]'::jsonb,'[]'::jsonb,%s,now(),now(),'demo_prior','completed','consultant',%s) returning id""",(emp,f"{cust}-09.wav",cid,cid,enc,f"2026-09-{day:02d} 10:00","成交了" if outcome=="付款" else ("还在跟进" if outcome=="定金" else "未知")))
        rid=cur.fetchone()[0]
        card=json.loads(json.dumps(template,ensure_ascii=False))
        for f in card.get('product_findings') or []:
            f['product_name']=product; f['product_id']=prod
        card['brief']['situation']=f"{cust}，9月历史接待，围绕{product}。"
        card['brief']['blocker']=f"9月记录：客户在{product}上仍有顾虑，最终结果见结果事件。"
        card['brief']['next_step']=f"9月跟进：{name}按约定回访。"
        card['primary_action']['goal']=f"9月跟进：{name}按约定回访。"
        card['primary_action']['action']=f"9月跟进：{name}按约定回访。"
        legacy={"business_title":product,"persuasive":{"block_point":card['brief']['blocker'],"deal_status":outcome or "未知"},"status_summary":card['brief']['blocker']}
        cur.execute("update recordings set analysis_result=%s where id=%s",(Json(legacy),rid))
        cur.execute("""insert into deal_expert_analyses(tenant_id,recording_id,encounter_id,employee_id,customer_id,customer_name,business_scope,role_code,recorded_at,cleaned_transcript,analysis_pipeline_version,prompt_version,status,result,analyzed_at,model_code,updated_at,attempts)
        values(1,%s,%s,%s,%s,%s,'consultant','consultant',%s,'9月历史接待','recording-analysis-v1','deal-expert-prompt-v1.4','analyzed',%s,now(),'demo-prior',now(),1)""",(rid,enc,emp,cid,cust,f"2026-09-{day:02d} 10:00",Json(card)))
        if outcome in ('付款','定金'):
            cur.execute("insert into deal_outcome_events(tenant_id,encounter_id,recording_id,type,amount,occurred_at,source,source_ref,note,confirmed_by) values(1,%s,%s,%s,%s,%s,'人工确认','demo_prior','9月历史确认',1)",(enc,rid,outcome,amount,f"2026-09-{day:02d} 15:00"))
        elif outcome=='退款':
            cur.execute("insert into deal_outcome_events(tenant_id,encounter_id,recording_id,type,amount,occurred_at,source,source_ref,note,confirmed_by) values(1,%s,%s,'退款',33800,%s,'人工确认','demo_prior','9月退款',1)",(enc,rid,f"2026-09-{day:02d} 16:00"))
    c.commit();print('prior seeded',len(ROWS));cur.close();c.close()

if __name__=='__main__': main()
