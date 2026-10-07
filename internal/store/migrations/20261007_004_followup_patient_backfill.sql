-- 只更新随访任务快照，不修改患者主数据。
UPDATE recording_tasks t
SET patient_age=COALESCE(t.patient_age,c.age),
    patient_gender=COALESCE(t.patient_gender,c.gender),
    customer_phone=COALESCE(NULLIF(t.customer_phone,''),c.phone),
    anonymous_patient=t.anonymous_patient OR COALESCE(c.extra_data->>'anonymous','false')='true'
      OR trim(COALESCE(t.customer_name,'')) IN ('','匿名患者','匿名客户','未命名患者','未命名客户','未知患者')
FROM customers c
WHERE c.id=t.customer_id AND c.tenant_id=t.tenant_id
AND t.source_type IN ('follow_up','followup');
