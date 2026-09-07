-- Complete the rebuilt EMR schema with constraints required by the confirmed
-- target structures. This migration contains no compatibility path for former
-- EMR tables or records.

UPDATE emr_quality_requirements
SET rule_type = '归档阻断'
WHERE rule_type = '归档拦截';

ALTER TABLE emr_quality_requirements
  DROP CONSTRAINT IF EXISTS emr_quality_requirements_type_check,
  ADD CONSTRAINT emr_quality_requirements_type_check
    CHECK (rule_type IN ('缺项', '逻辑冲突', '风险提醒', '归档阻断', '专科要求'));

ALTER TABLE emr_template_sections
  ADD CONSTRAINT emr_template_sections_input_type_check
    CHECK (input_type IN ('文本', '长文本', '单选', '多选', '日期', '数值', '结构化内容'));

ALTER TABLE emr_records
  ADD CONSTRAINT emr_records_encounter_fk
    FOREIGN KEY (encounter_id) REFERENCES encounters(id);

ALTER TABLE emr_record_snapshots
  ADD CONSTRAINT emr_record_snapshots_id_record_uq UNIQUE (id, record_id),
  ADD CONSTRAINT emr_record_snapshots_id_record_template_uq UNIQUE (id, record_id, template_version_id);

ALTER TABLE emr_records
  DROP CONSTRAINT IF EXISTS emr_records_current_snapshot_fk,
  DROP CONSTRAINT IF EXISTS emr_records_confirmed_snapshot_fk,
  DROP CONSTRAINT IF EXISTS emr_records_archived_snapshot_fk,
  ADD CONSTRAINT emr_records_current_snapshot_fk
    FOREIGN KEY (current_snapshot_id, id) REFERENCES emr_record_snapshots(id, record_id),
  ADD CONSTRAINT emr_records_confirmed_snapshot_fk
    FOREIGN KEY (confirmed_snapshot_id, id) REFERENCES emr_record_snapshots(id, record_id),
  ADD CONSTRAINT emr_records_archived_snapshot_fk
    FOREIGN KEY (archived_snapshot_id, id) REFERENCES emr_record_snapshots(id, record_id);

ALTER TABLE emr_check_runs
  ADD CONSTRAINT emr_check_runs_snapshot_context_fk
    FOREIGN KEY (snapshot_id, record_id, template_version_id)
    REFERENCES emr_record_snapshots(id, record_id, template_version_id),
  ADD CONSTRAINT emr_check_runs_id_record_uq UNIQUE (id, record_id);

UPDATE emr_check_results
SET incomplete_reason = CASE
  WHEN check_status = '未完成' THEN '人工待处理'
  ELSE ''
END
WHERE (check_status = '未完成' AND incomplete_reason NOT IN ('执行失败', '人工待处理'))
   OR (check_status = '已完成' AND incomplete_reason <> '');

ALTER TABLE emr_check_results
  ADD CONSTRAINT emr_check_results_incomplete_reason_check
    CHECK (
      (check_status = '已完成' AND incomplete_reason = '')
      OR (check_status = '未完成' AND incomplete_reason IN ('执行失败', '人工待处理'))
    ),
  ADD CONSTRAINT emr_check_results_manual_conclusion_check
    CHECK (
      manual_conclusion IS NULL
      OR (actual_execution_mode = '人工判断' AND check_status = '已完成' AND manual_by IS NOT NULL AND manual_at IS NOT NULL)
    );

ALTER TABLE emr_process_records
  ADD CONSTRAINT emr_process_action_type_check
    CHECK (action_type IN ('创建', '导入', '手动保存', '提交', '确认', '确认失效', '退回', '归档', '作废', '受控修订', '正式打印', '正式导出', '机构范围过程记录导出', '人工判断')),
  ADD CONSTRAINT emr_process_source_check
    CHECK (source IN ('病历详情', '实时病历', '病案留痕查询', '导入', '系统任务', '外部接口')),
  ADD CONSTRAINT emr_process_status_check
    CHECK (
      (before_status IS NULL OR before_status IN ('草稿', '需补全', '待确认', '退回', '已确认', '已归档', '已作废'))
      AND (after_status IS NULL OR after_status IN ('草稿', '需补全', '待确认', '退回', '已确认', '已归档', '已作废'))
    ),
  ADD CONSTRAINT emr_process_record_context_check
    CHECK (
      record_id IS NOT NULL
      OR (action_snapshot_id IS NULL AND before_snapshot_id IS NULL AND after_snapshot_id IS NULL AND check_run_id IS NULL)
    ),
  ADD CONSTRAINT emr_process_action_snapshot_record_fk
    FOREIGN KEY (action_snapshot_id, record_id) REFERENCES emr_record_snapshots(id, record_id),
  ADD CONSTRAINT emr_process_before_snapshot_record_fk
    FOREIGN KEY (before_snapshot_id, record_id) REFERENCES emr_record_snapshots(id, record_id),
  ADD CONSTRAINT emr_process_after_snapshot_record_fk
    FOREIGN KEY (after_snapshot_id, record_id) REFERENCES emr_record_snapshots(id, record_id),
  ADD CONSTRAINT emr_process_check_run_record_fk
    FOREIGN KEY (check_run_id, record_id) REFERENCES emr_check_runs(id, record_id);
