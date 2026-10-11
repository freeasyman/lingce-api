import json, re, urllib.request, uuid, pathlib, concurrent.futures, hashlib
base=pathlib.Path(__file__).resolve().parents[2]
baseline=json.loads(pathlib.Path('/private/tmp/compliance-phase2-baseline.json').read_text())
model=next(m for m in baseline['models'] if m['tenant_id']==0 and m['is_default'])
secret=(base/'configs/secrets.local.toml').read_text()
token=re.search(r'^llm_gateway_api_key\s*=\s*"([^"]*)"',secret,re.M).group(1)
candidate=json.loads((base/'docs/compliance/候选回放/医生风险线索提示词-v3.json').read_text())
system=candidate['system_prompt']; template=candidate['user_prompt_template']
rules=[r for r in baseline['rules'] if r['status']=='published' and r['is_enabled']]
samples=[
('normal-risk',0,'医生：手术存在感染等风险，我们会采取措施降低风险，但不能保证没有并发症。患者：我理解。'),
('normal-rate',0,'医生：医院统计成功率约95%，这是群体数据，不代表您个人一定成功，还需结合您的检查情况。'),
('conditional-duration',0,'医生：护理得当时种植牙可以使用很多年，但存在个体差异，需要定期复查，不能保证固定年限。'),
('absolute-promise',1,'医生：我保证你一定能治好，百分之百有效，不会失败，你放心。'),
('urgency',1,'患者：检查没做完，可以等结果再决定吗？医生：不用看检查，今天不交钱做手术就来不及了，一定会失明，必须现在决定。'),
('missing-basis',1,'患者：您说我肯定三天恢复，有什么依据？医生：没有检查也没有医学数据，我就是保证你三天完全恢复，不需要考虑个体差异。'),
('short-evidence',0,'医生：肯定。患者：谢谢。'),
('multi-rule',1,'医生：我保证你一定治好，百分之百成功，效果永久维持，不会复发。'),
('independent',2,'医生：我保证你这次治疗百分之百成功。患者：我想先等另一份检查。医生：不必等，今天不做就一定失明，我不需要检查依据，必须现在决定。'),
('unbound-patient',1,'医生：我保证这次治疗百分之百有效，不会失败。'),
('external-unknown',0,'医生：这类治疗的总体成功率通常较高，具体数值需核对院内统计和您的检查，存在个体差异，您可以考虑其他方案。'),
('patient-quote',0,'患者：有人说保证百分之百治好？医生：不能这样保证，结果因人而异，需要结合检查。')]
samples += [
('serious-only',0,'医生：您的病情比较严重，需要进一步检查后确定治疗方案。'),
('bed-eight-weeks',0,'医生：这个情况可能需要卧床八周，具体恢复时间因人而异，复查后再调整。'),
('early-treatment',0,'医生：建议越早治疗越好，具体时间可以结合检查结果和您的安排商量。'),
('success-rate-no-record',0,'医生：这项治疗成功率较高，但不是人人有效，存在个体差异。'),
('long-effect',0,'医生：治疗效果可以维持较长时间，但因人而异，需要复查和护理。'),
('records-absent',0,'医生：病情严重，建议尽早治疗。'),
('disclosure-absent',0,'医生：我们先安排检查，下次再讨论治疗方案。'),
('bed-pressure',1,'医生：不交钱今天就手术，你肯定会永久瘫痪，我不需要看检查，必须马上决定。'),
('long-guarantee',1,'医生：保证你一辈子有效，不需要复查，也不会复发。')
]
out=pathlib.Path('/private/tmp/compliance-phase2-review-20261010');out.mkdir(exist_ok=True)
(out/'samples.json').write_text(json.dumps(samples,ensure_ascii=False,indent=2))
def run(sample):
 name,expected,transcript=sample
 values=dict(tenant_id='4',source_type='synthetic',source_id=name,source_version='phase2-v3',scene_code='communication.doctor.outpatient',role_code='doctor',employee_name='测试医生',department='测试科室',recorded_at='2026-10-09',transcript=transcript,rules_json=json.dumps(rules,ensure_ascii=False))
 user=template
 for k,v in values.items():user=user.replace('{{'+k+'}}',v)
 payload=dict(tenant_id=4,caller_service='lingce-api',caller_module='compliance.phase2.replay',trace_id=str(uuid.uuid4()),function_type=model['function_type'],provider=model['provider'],model_code=model['model_code'],messages=[dict(role='system',content=system),dict(role='user',content=user)],params=dict(model['model_params'],response_format='json'))
 request=urllib.request.Request('http://localhost:8080/v1/inference/text',data=json.dumps(payload).encode(),headers={'Authorization':'Bearer '+token,'Content-Type':'application/json'})
 try:
  with urllib.request.urlopen(request,timeout=135) as r:response=json.load(r)
  (out/(name+'.json')).write_text(json.dumps(response,ensure_ascii=False,indent=2))
  content=response['content'].strip(); content=re.sub(r'^```(?:json)?\s*|\s*```$','',content.strip(),flags=re.S)
  payload=json.loads(content)
  findings=payload.get('findings')
  issues=[]
  if not isinstance(findings, list):
   raise ValueError('findings must be array')
  for f in findings:
   if not isinstance(f, dict):
    issues.append('finding-not-object'); continue
   for field in ['theme_key','rule_code','question','risk_hypothesis','primary_risk_type','evidence','check_basis','suggested_actions','priority']:
    if not f.get(field):issues.append('missing:'+field)
   for field in ['source_rule_codes','related_risk_types','evidence','check_basis','suggested_actions']:
    if not isinstance(f.get(field), list): issues.append(field+'-not-array')
   for e in f.get('evidence',[]) if isinstance(f.get('evidence'), list) else []:
    if not isinstance(e, dict): issues.append('evidence-not-object'); continue
    if e.get('quote','') not in transcript:issues.append('quote-not-verbatim')
  result=dict(sample=name,input=transcript,expected=expected,actual=len(findings),passed=len(findings)==expected and not issues,issues=issues,findings=findings,request_id=response.get('request_id'))
 except Exception as e:result=dict(sample=name,input=transcript,expected=expected,error=str(e),passed=False)
 print(name, result.get('actual'), result['passed'], result.get('issues',result.get('error')),flush=True)
 return result
with concurrent.futures.ThreadPoolExecutor(max_workers=3) as pool:results=list(pool.map(run,samples))
(out/'summary.json').write_text(json.dumps(dict(rules=[r['rule_code'] for r in rules],prompt_sha256=hashlib.sha256((system+template).encode()).hexdigest(),model=model,results=results),ensure_ascii=False,indent=2))
print('SUMMARY',sum(r['passed'] for r in results),'/',len(results),flush=True)
