BEGIN;
INSERT INTO customers (id,tenant_id,name,phone,lifecycle_stage,status,source,created_by,extra_data)
SELECT v.id,9001,v.name,v.phone,v.stage,v.status,'deal_acceptance',v.employee_id,'{}'::jsonb
FROM (VALUES
 (6001,'王女士','13990006001','consulting','lead',9101),(6002,'张女士','13990006002','converted','converted',9102),
 (6003,'李先生','13990006003','consulting','lead',9101),(6004,'刘先生','13990006004','lost','lost',9102),
 (6005,'未识别客户','','consulting','lead',9101),(6006,'赵女士','13990006006','consulting','lead',9101),
 (6007,'赵女士','13990006007','consulting','lead',9102),(6008,'陈女士','13990006008','refunded','refunded',9102)
) v(id,name,phone,stage,status,employee_id)
ON CONFLICT (id) DO UPDATE SET name=EXCLUDED.name,phone=EXCLUDED.phone,status=EXCLUDED.status,lifecycle_stage=EXCLUDED.lifecycle_stage,deleted_at=NULL,updated_at=NOW();
INSERT INTO encounters (id,tenant_id,source_type,source_id,patient_id,provider_id,provider_name,provider_role,department_id,channel,visit_type,started_at,ended_at,status,patient_name)
SELECT v.id,9001,'recording',v.recording_id,v.customer_id,v.employee_id,v.employee_name,'consultant',99101,'store',v.visit_type,v.started_at::timestamptz,v.ended_at::timestamptz,v.status,v.patient_name
FROM (VALUES
 (8001,7001,6001,9101,'王晓','first_visit','2026-10-15 09:00+08','2026-10-15 09:35+08','analyzed','王女士'),
 (8002,7002,6002,9102,'陈琳','first_visit','2026-10-15 09:40+08','2026-10-15 10:10+08','analyzed','张女士'),
 (8003,7003,6003,9101,'王晓','first_visit','2026-10-15 10:20+08','2026-10-15 10:45+08','analyzed','李先生'),
 (8004,7004,6004,9102,'陈琳','first_visit','2026-10-15 11:00+08','2026-10-15 11:20+08','analyzed','刘先生'),
 (8005,7005,NULL,9101,'王晓','consultation','2026-10-15 11:30+08','2026-10-15 11:45+08','unmatched','未识别客户'),
 (8006,7006,6006,9101,'王晓','first_visit','2026-10-15 13:00+08','2026-10-15 13:20+08','analyzed','赵女士'),
 (8007,7007,6007,9102,'陈琳','return_visit','2026-10-15 14:00+08','2026-10-15 14:25+08','analyzed','赵女士'),
 (8008,7008,6008,9102,'陈琳','first_visit','2026-10-15 15:00+08','2026-10-15 15:30+08','analyzed','陈女士')
) v(id,recording_id,customer_id,employee_id,employee_name,visit_type,started_at,ended_at,status,patient_name)
ON CONFLICT (id) DO UPDATE SET patient_id=EXCLUDED.patient_id,provider_id=EXCLUDED.provider_id,status=EXCLUDED.status,updated_at=NOW();
INSERT INTO recordings (id,tenant_id,employee_id,file_url,file_name,file_size,duration,mime_type,patient_id,source,scene,scene_name,status,transcription_status,transcription_text,analysis_status,recorded_at,created_at,updated_at,customer_id,cleaned_transcription,cleaned_transcription_status,confirmed_deal_status,converted_amount,refund_amount,business_scope,resolved_role_code,resolved_scene_scope,resolved_pipeline_version,encounter_id)
SELECT v.id,9001,v.employee_id,'https://example.invalid/deal-acceptance/'||v.file_name,v.file_name,1,v.duration,'audio/mpeg',v.customer_id,'deal_acceptance','consultant','咨询师','completed','completed',v.transcript,'completed',v.recorded_at::timestamp,NOW(),NOW(),v.customer_id,jsonb_build_array(jsonb_build_object('speaker_role','客户','text',v.transcript,'start_ms',0)),'completed',v.deal_status,v.amount,v.refund,'consultant','consultant','consultant','recording-analysis-v1',v.encounter_id
FROM (VALUES
 (7001,'E001.mp3',9101,6001,210,'2026-10-15 09:00','王女士明确想暑假做全飞秒，但要回去和老公商量。咨询师介绍了全飞秒 Pro。','还在跟进',0,0,8001),
 (7002,'E002.mp3',9102,6002,180,'2026-10-15 09:40','张女士认可全飞秒 Pro 方案并完成付款。','成交了',23800,0,8002),
 (7003,'E003.mp3',9101,6003,150,'2026-10-15 10:20','李先生多次表达眼干，咨询师未完成干眼评估联合推荐。','还在跟进',0,0,8003),
 (7004,'E004.mp3',9102,6004,120,'2026-10-15 11:00','刘先生已选择其他机构并明确要求停止联系。','没成交',0,0,8004),
 (7005,'E005.mp3',9101,NULL,90,'2026-10-15 11:30','客户身份信息不足，无法关联客户。',NULL,0,0,8005),
 (7006,'E006.mp3',9101,6006,240,'2026-10-15 13:00','三个连续时间片属于同一次接待，客户关心恢复时间和价格。','还在跟进',0,0,8006),
 (7007,'E007.mp3',9102,6007,200,'2026-10-15 14:00','客户存在未解决术后问题，咨询师未完成信任修复。','还在跟进',0,0,8007),
 (7008,'E008.mp3',9102,6008,180,'2026-10-15 15:00','客户付款后申请退款，咨询师未及时识别核心顾虑。','成交了',16800,16800,8008)
) v(id,file_name,employee_id,customer_id,duration,recorded_at,transcript,deal_status,amount,refund,encounter_id)
ON CONFLICT (id) DO UPDATE SET encounter_id=EXCLUDED.encounter_id,customer_id=EXCLUDED.customer_id,cleaned_transcription=EXCLUDED.cleaned_transcription,transcription_text=EXCLUDED.transcription_text,confirmed_deal_status=EXCLUDED.confirmed_deal_status,converted_amount=EXCLUDED.converted_amount,refund_amount=EXCLUDED.refund_amount,deleted_at=NULL,updated_at=NOW();
COMMIT;
