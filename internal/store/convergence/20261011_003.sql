-- Production convergence baseline 20261006_001.
-- Generated from the audited development target state on 2026-10-07.
-- First convergence is additive: no DROP, TRUNCATE, DELETE, tenant-data replacement, or secret/API-key copy.
-- Historical migrations were reviewed inputs; they are not replayed or concatenated here.
-- Run only after backup and a temporary-database rehearsal. The application records this script checksum in api_schema_baselines.
SET LOCAL lock_timeout='10s';
SET LOCAL statement_timeout='15min';
CREATE EXTENSION IF NOT EXISTS pgcrypto;

-- Refuse silent reuse of the incompatible legacy compliance_rules shape.
DO $$ BEGIN IF to_regclass('public.compliance_rules') IS NOT NULL AND NOT EXISTS (SELECT 1 FROM information_schema.columns WHERE table_schema='public' AND table_name='compliance_rules' AND column_name='rule_code') THEN RAISE EXCEPTION '生产存在旧版 compliance_rules 结构，需先完成兼容映射后再收敛'; END IF; END $$;

CREATE SEQUENCE IF NOT EXISTS public.encounters_id_seq
    START WITH 1
    INCREMENT BY 1
    NO MINVALUE
    NO MAXVALUE
    CACHE 1;
CREATE SEQUENCE IF NOT EXISTS public.emr_icd10_diagnosis_catalog_id_seq
    START WITH 1
    INCREMENT BY 1
    NO MINVALUE
    NO MAXVALUE
    CACHE 1;
CREATE SEQUENCE IF NOT EXISTS public.emr_permission_assignments_id_seq
    START WITH 1
    INCREMENT BY 1
    NO MINVALUE
    NO MAXVALUE
    CACHE 1;
CREATE SEQUENCE IF NOT EXISTS public.followup_generation_runs_id_seq
    START WITH 1
    INCREMENT BY 1
    NO MINVALUE
    NO MAXVALUE
    CACHE 1;
CREATE SEQUENCE IF NOT EXISTS public.followup_data_scopes_id_seq
    START WITH 1
    INCREMENT BY 1
    NO MINVALUE
    NO MAXVALUE
    CACHE 1;
CREATE SEQUENCE IF NOT EXISTS public.followup_permission_assignments_id_seq
    START WITH 1
    INCREMENT BY 1
    NO MINVALUE
    NO MAXVALUE
    CACHE 1;
CREATE SEQUENCE IF NOT EXISTS public.compliance_permission_assignments_id_seq
    START WITH 1
    INCREMENT BY 1
    NO MINVALUE
    NO MAXVALUE
    CACHE 1;

-- Current target table: encounters
CREATE TABLE IF NOT EXISTS public.encounters (
    id bigint NOT NULL,
    tenant_id bigint NOT NULL,
    source_type character varying(32) NOT NULL,
    source_id bigint NOT NULL,
    patient_id bigint,
    patient_candidate_json jsonb DEFAULT '{}'::jsonb NOT NULL,
    provider_id bigint,
    provider_name character varying(128) DEFAULT ''::character varying NOT NULL,
    provider_role character varying(64) DEFAULT ''::character varying NOT NULL,
    department_id bigint,
    channel character varying(32) DEFAULT 'store'::character varying NOT NULL,
    visit_type character varying(32) DEFAULT 'consultation'::character varying NOT NULL,
    started_at timestamp with time zone,
    ended_at timestamp with time zone,
    status character varying(32) DEFAULT 'new'::character varying NOT NULL,
    created_at timestamp with time zone DEFAULT now() NOT NULL,
    updated_at timestamp with time zone DEFAULT now() NOT NULL,
    patient_name character varying(255),
    CONSTRAINT chk_encounters_channel CHECK (((channel)::text = ANY ((ARRAY['store'::character varying, 'phone'::character varying, 'wechat'::character varying, 'online'::character varying])::text[]))),
    CONSTRAINT chk_encounters_source_type CHECK (((source_type)::text = ANY ((ARRAY['recording'::character varying, 'transcript'::character varying, 'phone'::character varying, 'wechat'::character varying, 'manual'::character varying, 'realtime'::character varying])::text[]))),
    CONSTRAINT chk_encounters_status CHECK (((status)::text = ANY ((ARRAY['new'::character varying, 'unmatched'::character varying, 'matched'::character varying, 'analyzed'::character varying, 'closed'::character varying, 'archived'::character varying])::text[]))),
    CONSTRAINT chk_encounters_visit_type CHECK (((visit_type)::text = ANY ((ARRAY['first_visit'::character varying, 'return_visit'::character varying, 'consultation'::character varying, 'follow_up'::character varying])::text[])))
);

-- Current target table: emr_templates
CREATE TABLE IF NOT EXISTS public.emr_templates (
    id uuid DEFAULT gen_random_uuid() NOT NULL,
    tenant_id bigint DEFAULT 0 NOT NULL,
    code character varying(128) NOT NULL,
    name character varying(255) NOT NULL,
    version_no character varying(32) NOT NULL,
    document_type character varying(16) NOT NULL,
    visit_type character varying(16) NOT NULL,
    department_id bigint,
    specialty character varying(64),
    status character varying(16) DEFAULT 'draft'::character varying NOT NULL,
    print_title character varying(255) DEFAULT ''::character varying NOT NULL,
    description text DEFAULT ''::text NOT NULL,
    created_by bigint DEFAULT 0 NOT NULL,
    published_by bigint,
    disabled_by bigint,
    created_at timestamp with time zone DEFAULT now() NOT NULL,
    published_at timestamp with time zone,
    disabled_at timestamp with time zone,
    updated_at timestamp with time zone DEFAULT now() NOT NULL,
    updated_by bigint DEFAULT 0 NOT NULL,
    is_current boolean DEFAULT false NOT NULL,
    CONSTRAINT emr_templates_code_check CHECK ((btrim((code)::text) <> ''::text)),
    CONSTRAINT emr_templates_document_type_check CHECK (((document_type)::text = ANY ((ARRAY['门诊病历'::character varying, '急诊病历'::character varying])::text[]))),
    CONSTRAINT emr_templates_name_check CHECK ((btrim((name)::text) <> ''::text)),
    CONSTRAINT emr_templates_status_check CHECK (((status)::text = ANY ((ARRAY['draft'::character varying, 'published'::character varying, 'disabled'::character varying])::text[]))),
    CONSTRAINT emr_templates_tenant_check CHECK ((tenant_id >= 0)),
    CONSTRAINT emr_templates_version_check CHECK ((btrim((version_no)::text) <> ''::text)),
    CONSTRAINT emr_templates_visit_type_check CHECK (((visit_type)::text = ANY ((ARRAY['初诊'::character varying, '复诊'::character varying, '通用'::character varying])::text[])))
);

-- Current target table: emr_fields
CREATE TABLE IF NOT EXISTS public.emr_fields (
    id uuid DEFAULT gen_random_uuid() NOT NULL,
    tenant_id bigint DEFAULT 0 NOT NULL,
    code character varying(128) NOT NULL,
    name character varying(255) NOT NULL,
    specialty character varying(64),
    content_category character varying(32) NOT NULL,
    input_type character varying(32) NOT NULL,
    structure_json jsonb DEFAULT '{}'::jsonb NOT NULL,
    options_json jsonb DEFAULT '{}'::jsonb NOT NULL,
    description text DEFAULT ''::text NOT NULL,
    status character varying(16) DEFAULT 'enabled'::character varying NOT NULL,
    created_by bigint DEFAULT 0 NOT NULL,
    updated_by bigint DEFAULT 0 NOT NULL,
    created_at timestamp with time zone DEFAULT now() NOT NULL,
    updated_at timestamp with time zone DEFAULT now() NOT NULL,
    CONSTRAINT emr_fields_category_check CHECK (((content_category)::text = ANY ((ARRAY['就诊归属'::character varying, '病史'::character varying, '诊疗依据'::character varying, '医疗判断'::character varying, '医疗措施'::character varying, '文书责任'::character varying])::text[]))),
    CONSTRAINT emr_fields_code_not_blank CHECK ((btrim((code)::text) <> ''::text)),
    CONSTRAINT emr_fields_input_type_check CHECK (((input_type)::text = ANY ((ARRAY['文本'::character varying, '长文本'::character varying, '单选'::character varying, '多选'::character varying, '日期'::character varying, '数值'::character varying, '结构化内容'::character varying])::text[]))),
    CONSTRAINT emr_fields_name_not_blank CHECK ((btrim((name)::text) <> ''::text)),
    CONSTRAINT emr_fields_status_check CHECK (((status)::text = ANY ((ARRAY['enabled'::character varying, 'disabled'::character varying])::text[]))),
    CONSTRAINT emr_fields_tenant_check CHECK ((tenant_id >= 0))
);

-- Current target table: emr_template_fields
CREATE TABLE IF NOT EXISTS public.emr_template_fields (
    id uuid DEFAULT gen_random_uuid() NOT NULL,
    tenant_id bigint DEFAULT 0 NOT NULL,
    template_id uuid NOT NULL,
    field_id uuid NOT NULL,
    display_order integer NOT NULL,
    is_visible boolean DEFAULT true NOT NULL,
    is_required boolean DEFAULT false NOT NULL,
    description text DEFAULT ''::text NOT NULL,
    created_by bigint DEFAULT 0 NOT NULL,
    created_at timestamp with time zone DEFAULT now() NOT NULL,
    updated_at timestamp with time zone DEFAULT now() NOT NULL,
    CONSTRAINT emr_template_fields_order_check CHECK ((display_order > 0)),
    CONSTRAINT emr_template_fields_tenant_check CHECK ((tenant_id >= 0))
);

-- Current target table: emr_records
CREATE TABLE IF NOT EXISTS public.emr_records (
    id uuid DEFAULT gen_random_uuid() NOT NULL,
    tenant_id bigint NOT NULL,
    encounter_id bigint NOT NULL,
    customer_id bigint,
    patient_snapshot_json jsonb DEFAULT '{}'::jsonb NOT NULL,
    template_id uuid NOT NULL,
    document_type character varying(16) NOT NULL,
    visit_type character varying(16) NOT NULL,
    department_id bigint,
    doctor_id bigint NOT NULL,
    started_at timestamp with time zone NOT NULL,
    ended_at timestamp with time zone,
    encounter_context_json jsonb DEFAULT '{}'::jsonb NOT NULL,
    source_references_json jsonb DEFAULT '{}'::jsonb NOT NULL,
    content_json jsonb DEFAULT '{}'::jsonb NOT NULL,
    status character varying(16) DEFAULT '草稿'::character varying NOT NULL,
    submitted_at timestamp with time zone,
    confirmed_by bigint,
    confirmed_at timestamp with time zone,
    archived_by bigint,
    archived_at timestamp with time zone,
    voided_by bigint,
    voided_at timestamp with time zone,
    created_by bigint NOT NULL,
    created_at timestamp with time zone DEFAULT now() NOT NULL,
    updated_at timestamp with time zone DEFAULT now() NOT NULL,
    revision_no integer DEFAULT 0 NOT NULL,
    current_score_status character varying(16) DEFAULT '未评分'::character varying NOT NULL,
    current_total_score integer,
    current_grade character varying(8),
    current_score_version character varying(64) DEFAULT ''::character varying NOT NULL,
    current_quality_run_id uuid,
    CONSTRAINT emr_records_customer_positive_check CHECK (((customer_id IS NULL) OR (customer_id > 0))),
    CONSTRAINT emr_records_document_type_check CHECK (((document_type)::text = ANY ((ARRAY['门诊病历'::character varying, '急诊病历'::character varying])::text[]))),
    CONSTRAINT emr_records_status_check CHECK (((status)::text = ANY ((ARRAY['草稿'::character varying, '需补全'::character varying, '退回'::character varying, '已完成'::character varying, '待归档'::character varying, '已归档'::character varying, '已作废'::character varying])::text[]))),
    CONSTRAINT emr_records_tenant_positive_check CHECK ((tenant_id > 0)),
    CONSTRAINT emr_records_time_order_check CHECK (((ended_at IS NULL) OR (ended_at >= started_at))),
    CONSTRAINT emr_records_visit_type_check CHECK (((visit_type)::text = ANY ((ARRAY['初诊'::character varying, '复诊'::character varying])::text[])))
);

-- Current target table: emr_record_snapshots
CREATE TABLE IF NOT EXISTS public.emr_record_snapshots (
    id uuid DEFAULT gen_random_uuid() NOT NULL,
    record_id uuid NOT NULL,
    encounter_id bigint NOT NULL,
    revision_no integer NOT NULL,
    snapshot_no integer NOT NULL,
    template_version_id uuid NOT NULL,
    patient_snapshot jsonb DEFAULT '{}'::jsonb NOT NULL,
    document_context jsonb DEFAULT '{}'::jsonb NOT NULL,
    content jsonb DEFAULT '{}'::jsonb NOT NULL,
    content_digest character varying(128) NOT NULL,
    formed_by bigint NOT NULL,
    formed_at timestamp with time zone DEFAULT now() NOT NULL,
    score_status character varying(16) DEFAULT '未评分'::character varying NOT NULL,
    total_score integer,
    grade character varying(8),
    score_version character varying(64) DEFAULT ''::character varying NOT NULL,
    quality_run_id uuid
);

-- Current target table: emr_process_records
CREATE TABLE IF NOT EXISTS public.emr_process_records (
    id uuid DEFAULT gen_random_uuid() NOT NULL,
    tenant_id bigint NOT NULL,
    record_id uuid,
    action_type character varying(32) NOT NULL,
    action_result character varying(8) NOT NULL,
    occurred_at timestamp with time zone DEFAULT now() NOT NULL,
    actor_type character varying(8) NOT NULL,
    actor_id bigint,
    source character varying(24) NOT NULL,
    before_status character varying(16),
    after_status character varying(16),
    action_snapshot_id uuid,
    before_snapshot_id uuid,
    after_snapshot_id uuid,
    output_info jsonb DEFAULT '{}'::jsonb NOT NULL,
    failure_reason text DEFAULT ''::text NOT NULL,
    action_note text DEFAULT ''::text NOT NULL,
    content_changes jsonb DEFAULT '[]'::jsonb NOT NULL,
    CONSTRAINT emr_process_action_result_check CHECK (((action_result)::text = ANY ((ARRAY['成功'::character varying, '失败'::character varying, '阻断'::character varying])::text[]))),
    CONSTRAINT emr_process_action_type_check CHECK (((action_type)::text = ANY ((ARRAY['创建'::character varying, '导入'::character varying, '手动保存'::character varying, '提交'::character varying, '确认'::character varying, '确认失效'::character varying, '退回'::character varying, '归档'::character varying, '作废'::character varying, '受控修订'::character varying, '正式打印'::character varying, '正式导出'::character varying, '机构范围过程记录导出'::character varying, '人工判断'::character varying])::text[]))),
    CONSTRAINT emr_process_actor_check CHECK ((((actor_type)::text = '系统'::text) OR (actor_id IS NOT NULL))),
    CONSTRAINT emr_process_actor_type_check CHECK (((actor_type)::text = ANY ((ARRAY['人工'::character varying, '系统'::character varying])::text[]))),
    CONSTRAINT emr_process_failure_reason_check CHECK ((((action_result)::text = '成功'::text) OR (NULLIF(btrim(failure_reason), ''::text) IS NOT NULL))),
    CONSTRAINT emr_process_record_reference_check CHECK (((record_id IS NOT NULL) OR ((action_type)::text = '机构范围过程记录导出'::text) OR (((action_type)::text = '创建'::text) AND ((action_result)::text = '失败'::text)))),
    CONSTRAINT emr_process_source_check CHECK (((source)::text = ANY ((ARRAY['病历详情'::character varying, '实时病历'::character varying, '病案留痕查询'::character varying, '导入'::character varying, '系统任务'::character varying, '外部接口'::character varying])::text[]))),
    CONSTRAINT emr_process_status_check CHECK ((((before_status IS NULL) OR ((before_status)::text = ANY ((ARRAY['草稿'::character varying, '需补全'::character varying, '退回'::character varying, '已完成'::character varying, '待归档'::character varying, '已归档'::character varying, '已作废'::character varying])::text[]))) AND ((after_status IS NULL) OR ((after_status)::text = ANY ((ARRAY['草稿'::character varying, '需补全'::character varying, '退回'::character varying, '已完成'::character varying, '待归档'::character varying, '已归档'::character varying, '已作废'::character varying])::text[])))))
);

-- Current target table: emr_quality_rule_set_versions
CREATE TABLE IF NOT EXISTS public.emr_quality_rule_set_versions (
    version character varying(32) NOT NULL,
    status character varying(16) DEFAULT 'draft'::character varying NOT NULL,
    scoring_ready boolean DEFAULT false NOT NULL,
    note text DEFAULT ''::text NOT NULL,
    created_at timestamp with time zone DEFAULT now() NOT NULL,
    updated_at timestamp with time zone DEFAULT now() NOT NULL,
    CONSTRAINT emr_quality_rule_set_versions_status_check CHECK (((status)::text = ANY ((ARRAY['draft'::character varying, 'published'::character varying, 'retired'::character varying])::text[])))
);

-- Current target table: emr_quality_rule_definitions
CREATE TABLE IF NOT EXISTS public.emr_quality_rule_definitions (
    id uuid DEFAULT gen_random_uuid() NOT NULL,
    rule_code character varying(64) NOT NULL,
    version character varying(32) NOT NULL,
    section_code character varying(64) NOT NULL,
    name character varying(255) NOT NULL,
    source_text text DEFAULT ''::text NOT NULL,
    product_text text DEFAULT ''::text NOT NULL,
    standard_points integer DEFAULT 0 NOT NULL,
    executor_kind character varying(16) NOT NULL,
    input_spec jsonb DEFAULT '{}'::jsonb NOT NULL,
    evaluation_spec jsonb DEFAULT '{}'::jsonb NOT NULL,
    action_policy jsonb DEFAULT '{}'::jsonb NOT NULL,
    message_templates jsonb DEFAULT '{}'::jsonb NOT NULL,
    status character varying(16) DEFAULT 'draft'::character varying NOT NULL,
    created_at timestamp with time zone DEFAULT now() NOT NULL,
    updated_at timestamp with time zone DEFAULT now() NOT NULL,
    CONSTRAINT emr_quality_rule_definitions_executor_check CHECK (((executor_kind)::text = ANY ((ARRAY['program'::character varying, 'llm'::character varying, 'hybrid'::character varying, 'manual'::character varying])::text[]))),
    CONSTRAINT emr_quality_rule_definitions_points_check CHECK (((standard_points >= 0) AND (standard_points <= 100))),
    CONSTRAINT emr_quality_rule_definitions_status_check CHECK (((status)::text = ANY ((ARRAY['draft'::character varying, 'published'::character varying, 'retired'::character varying])::text[])))
);

-- Current target table: emr_quality_runs
CREATE TABLE IF NOT EXISTS public.emr_quality_runs (
    id uuid DEFAULT gen_random_uuid() NOT NULL,
    tenant_id bigint NOT NULL,
    record_id uuid NOT NULL,
    snapshot_id uuid,
    action character varying(16) NOT NULL,
    rule_set_version character varying(32) NOT NULL,
    status character varying(16) DEFAULT 'running'::character varying NOT NULL,
    overall_result character varying(16) DEFAULT 'no_problem'::character varying NOT NULL,
    total_score integer DEFAULT 0 NOT NULL,
    grade character varying(8) DEFAULT ''::character varying NOT NULL,
    major_score integer DEFAULT 0 NOT NULL,
    blocking_count integer DEFAULT 0 NOT NULL,
    pending_count integer DEFAULT 0 NOT NULL,
    started_at timestamp with time zone DEFAULT now() NOT NULL,
    completed_at timestamp with time zone,
    failure_reason text DEFAULT ''::text NOT NULL,
    created_at timestamp with time zone DEFAULT now() NOT NULL,
    model_audit jsonb DEFAULT '{}'::jsonb NOT NULL,
    input_digest character varying(128) DEFAULT ''::character varying NOT NULL,
    CONSTRAINT emr_quality_runs_action_check CHECK (((action)::text = ANY ((ARRAY['save'::character varying, 'complete'::character varying, 'archive'::character varying])::text[]))),
    CONSTRAINT emr_quality_runs_result_check CHECK (((overall_result)::text = ANY ((ARRAY['no_problem'::character varying, 'has_problem'::character varying, 'blocked'::character varying, 'partial'::character varying, 'failed'::character varying])::text[]))),
    CONSTRAINT emr_quality_runs_score_check CHECK ((((total_score >= 0) AND (total_score <= 100)) AND ((major_score >= 0) AND (major_score <= 80)))),
    CONSTRAINT emr_quality_runs_status_check CHECK (((status)::text = ANY ((ARRAY['running'::character varying, 'completed'::character varying, 'failed'::character varying])::text[])))
);

-- Current target table: emr_quality_results
CREATE TABLE IF NOT EXISTS public.emr_quality_results (
    id uuid DEFAULT gen_random_uuid() NOT NULL,
    run_id uuid NOT NULL,
    rule_id uuid NOT NULL,
    rule_code character varying(64) NOT NULL,
    section_code character varying(64) NOT NULL,
    result character varying(16) NOT NULL,
    execution_kind character varying(16) NOT NULL,
    evidence jsonb DEFAULT '[]'::jsonb NOT NULL,
    reason text DEFAULT ''::text NOT NULL,
    deduction integer DEFAULT 0 NOT NULL,
    message text DEFAULT ''::text NOT NULL,
    blocking boolean DEFAULT false NOT NULL,
    manual_review boolean DEFAULT false NOT NULL,
    created_at timestamp with time zone DEFAULT now() NOT NULL,
    CONSTRAINT emr_quality_results_deduction_check CHECK ((deduction >= 0)),
    CONSTRAINT emr_quality_results_execution_check CHECK (((execution_kind)::text = ANY ((ARRAY['program'::character varying, 'llm'::character varying, 'hybrid'::character varying, 'manual'::character varying])::text[]))),
    CONSTRAINT emr_quality_results_result_check CHECK (((result)::text = ANY ((ARRAY['pass'::character varying, 'problem'::character varying, 'not_evaluable'::character varying])::text[])))
);

-- Current target table: emr_record_score_current
CREATE TABLE IF NOT EXISTS public.emr_record_score_current (
    record_id uuid NOT NULL,
    tenant_id bigint NOT NULL,
    snapshot_id uuid,
    score_version character varying(64) NOT NULL,
    status character varying(16) DEFAULT '已完成'::character varying NOT NULL,
    total_score integer,
    grade character varying(8),
    program_score integer DEFAULT 0 NOT NULL,
    program_deduct_points integer DEFAULT 0 NOT NULL,
    major_program_score integer DEFAULT 0 NOT NULL,
    direct_grade character varying(8) DEFAULT ''::character varying NOT NULL,
    direct_grade_reason text DEFAULT ''::text NOT NULL,
    pending_sections jsonb DEFAULT '[]'::jsonb NOT NULL,
    failure_reason text DEFAULT ''::text NOT NULL,
    scored_at timestamp with time zone DEFAULT now() NOT NULL,
    updated_at timestamp with time zone DEFAULT now() NOT NULL,
    CONSTRAINT emr_record_score_current_grade_check CHECK (((grade)::text = ANY ((ARRAY['甲级'::character varying, '乙级'::character varying, '丙级'::character varying, ''::character varying])::text[]))),
    CONSTRAINT emr_record_score_current_score_check CHECK ((((total_score >= 0) AND (total_score <= 100)) AND ((program_score >= 0) AND (program_score <= 100)) AND ((program_deduct_points >= 0) AND (program_deduct_points <= 100)) AND ((major_program_score >= 0) AND (major_program_score <= 100)))),
    CONSTRAINT emr_record_score_current_status_check CHECK (((status)::text = ANY ((ARRAY['执行中'::character varying, '已完成'::character varying, '待人工复核'::character varying, '执行失败'::character varying])::text[])))
);

-- Current target table: emr_record_score_current_items
CREATE TABLE IF NOT EXISTS public.emr_record_score_current_items (
    id uuid DEFAULT gen_random_uuid() NOT NULL,
    tenant_id bigint NOT NULL,
    record_id uuid NOT NULL,
    section_code character varying(64) NOT NULL,
    section_name character varying(64) NOT NULL,
    standard_score integer NOT NULL,
    score integer NOT NULL,
    program_deduct_points integer DEFAULT 0 NOT NULL,
    pending boolean DEFAULT false NOT NULL,
    findings jsonb DEFAULT '[]'::jsonb NOT NULL,
    sort_order integer DEFAULT 0 NOT NULL,
    updated_at timestamp with time zone DEFAULT now() NOT NULL,
    CONSTRAINT emr_record_score_current_items_score_check CHECK (((standard_score >= 0) AND (score >= 0) AND (score <= standard_score) AND (program_deduct_points >= 0) AND (program_deduct_points <= standard_score)))
);

-- Current target table: emr_score_standards
CREATE TABLE IF NOT EXISTS public.emr_score_standards (
    id uuid DEFAULT gen_random_uuid() NOT NULL,
    code character varying(128) NOT NULL,
    version character varying(64) NOT NULL,
    name character varying(255) NOT NULL,
    source_name character varying(255) DEFAULT ''::character varying NOT NULL,
    source_reference text DEFAULT ''::text NOT NULL,
    standard_text text NOT NULL,
    status character varying(16) DEFAULT 'published'::character varying NOT NULL,
    published_at timestamp with time zone,
    created_at timestamp with time zone DEFAULT now() NOT NULL,
    updated_at timestamp with time zone DEFAULT now() NOT NULL,
    CONSTRAINT emr_score_standards_status_check CHECK (((status)::text = ANY ((ARRAY['draft'::character varying, 'published'::character varying, 'disabled'::character varying])::text[])))
);

-- Current target table: emr_icd10_diagnosis_catalog
CREATE TABLE IF NOT EXISTS public.emr_icd10_diagnosis_catalog (
    id bigint NOT NULL,
    catalog_code text NOT NULL,
    catalog_name text NOT NULL,
    catalog_version text DEFAULT ''::text NOT NULL,
    chapter_no text DEFAULT ''::text NOT NULL,
    chapter_code_range text DEFAULT ''::text NOT NULL,
    chapter_name text DEFAULT ''::text NOT NULL,
    section_code_range text DEFAULT ''::text NOT NULL,
    section_name text DEFAULT ''::text NOT NULL,
    category_code text DEFAULT ''::text NOT NULL,
    category_name text DEFAULT ''::text NOT NULL,
    subcategory_code text DEFAULT ''::text NOT NULL,
    subcategory_name text DEFAULT ''::text NOT NULL,
    diagnosis_code text NOT NULL,
    diagnosis_name text NOT NULL,
    normalized_name text NOT NULL,
    source_file text DEFAULT ''::text NOT NULL,
    source_page integer,
    is_active boolean DEFAULT true NOT NULL,
    imported_at timestamp with time zone DEFAULT now() NOT NULL,
    created_at timestamp with time zone DEFAULT now() NOT NULL,
    CONSTRAINT emr_icd10_catalog_code_not_blank CHECK ((btrim(catalog_code) <> ''::text)),
    CONSTRAINT emr_icd10_diagnosis_code_not_blank CHECK ((btrim(diagnosis_code) <> ''::text)),
    CONSTRAINT emr_icd10_diagnosis_name_not_blank CHECK ((btrim(diagnosis_name) <> ''::text)),
    CONSTRAINT emr_icd10_normalized_name_not_blank CHECK ((btrim(normalized_name) <> ''::text))
);

-- Current target table: emr_permission_assignments
CREATE TABLE IF NOT EXISTS public.emr_permission_assignments (
    id bigint NOT NULL,
    tenant_id bigint NOT NULL,
    employee_id bigint NOT NULL,
    enabled boolean DEFAULT false NOT NULL,
    emr_role_code character varying(64) DEFAULT 'readonly'::character varying NOT NULL,
    scope character varying(32) DEFAULT 'self'::character varying NOT NULL,
    abilities_json jsonb DEFAULT '[]'::jsonb NOT NULL,
    created_by bigint,
    updated_by bigint,
    created_at timestamp with time zone DEFAULT now() NOT NULL,
    updated_at timestamp with time zone DEFAULT now() NOT NULL,
    CONSTRAINT chk_emr_permission_role_code CHECK (((emr_role_code)::text = ANY ((ARRAY['emr_admin'::character varying, 'doctor'::character varying, 'archivist'::character varying, 'readonly'::character varying])::text[]))),
    CONSTRAINT chk_emr_permission_scope CHECK (((scope)::text = ANY ((ARRAY['self'::character varying, 'department'::character varying, 'tenant'::character varying])::text[])))
);

-- Current target table: emr_badge_sessions
CREATE TABLE IF NOT EXISTS public.emr_badge_sessions (
    id uuid DEFAULT gen_random_uuid() NOT NULL,
    tenant_id bigint NOT NULL,
    employee_id bigint NOT NULL,
    device_no character varying(128) NOT NULL,
    customer_id bigint,
    started_at timestamp with time zone DEFAULT now() NOT NULL,
    stopped_at timestamp with time zone,
    recording_id bigint,
    error_message text,
    created_at timestamp with time zone DEFAULT now() NOT NULL,
    updated_at timestamp with time zone DEFAULT now() NOT NULL,
    order_no character varying(255) NOT NULL,
    state character varying(32) DEFAULT 'recording'::character varying NOT NULL,
    CONSTRAINT emr_badge_sessions_state_check CHECK (((state)::text = ANY ((ARRAY['recording'::character varying, 'waiting_upload'::character varying, 'uploaded'::character varying, 'failed'::character varying])::text[])))
);

-- Current target table: emr_followup_deliveries
CREATE TABLE IF NOT EXISTS public.emr_followup_deliveries (
    tenant_id bigint NOT NULL,
    recording_id bigint NOT NULL,
    status text DEFAULT 'pending'::text NOT NULL,
    attempt_count integer DEFAULT 0 NOT NULL,
    next_attempt_at timestamp with time zone DEFAULT now() NOT NULL,
    request_id text,
    last_error text,
    created_at timestamp with time zone DEFAULT now() NOT NULL,
    updated_at timestamp with time zone DEFAULT now() NOT NULL,
    CONSTRAINT emr_followup_deliveries_status_check CHECK ((status = ANY (ARRAY['pending'::text, 'accepted'::text])))
);

-- Current target table: followup_generation_runs
CREATE TABLE IF NOT EXISTS public.followup_generation_runs (
    id bigint NOT NULL,
    tenant_id integer NOT NULL,
    recording_id integer NOT NULL,
    encounter_id integer NOT NULL,
    employee_id integer NOT NULL,
    customer_id integer NOT NULL,
    customer_name character varying(255),
    doctor_name character varying(255),
    recorded_at timestamp without time zone,
    request_id character varying(128) NOT NULL,
    request_payload jsonb NOT NULL,
    prompt_code character varying(128),
    prompt_version character varying(64),
    generation_model character varying(128),
    llm_request_id character varying(128),
    status character varying(32) DEFAULT 'pending'::character varying NOT NULL,
    attempt_count integer DEFAULT 0 NOT NULL,
    max_attempts integer DEFAULT 3 NOT NULL,
    next_retry_at timestamp without time zone,
    last_error_code character varying(128),
    last_error_message text,
    llm_response_payload jsonb,
    generated_tasks_payload jsonb,
    persisted_task_count integer DEFAULT 0 NOT NULL,
    started_at timestamp without time zone,
    completed_at timestamp without time zone,
    failed_at timestamp without time zone,
    created_at timestamp without time zone DEFAULT now() NOT NULL,
    updated_at timestamp without time zone DEFAULT now() NOT NULL
);

-- Current target table: followup_data_scopes
CREATE TABLE IF NOT EXISTS public.followup_data_scopes (
    id bigint NOT NULL,
    tenant_id integer NOT NULL,
    viewer_employee_id integer NOT NULL,
    target_employee_id integer NOT NULL,
    created_by integer,
    created_at timestamp without time zone DEFAULT now() NOT NULL,
    updated_at timestamp without time zone DEFAULT now() NOT NULL
);

-- Current target table: followup_permission_assignments
CREATE TABLE IF NOT EXISTS public.followup_permission_assignments (
    id bigint NOT NULL,
    tenant_id bigint NOT NULL,
    employee_id bigint NOT NULL,
    enabled boolean DEFAULT false NOT NULL,
    role_code character varying(64) DEFAULT 'followup_staff'::character varying NOT NULL,
    abilities_json jsonb DEFAULT '[]'::jsonb NOT NULL,
    created_by bigint,
    updated_by bigint,
    created_at timestamp with time zone DEFAULT now() NOT NULL,
    updated_at timestamp with time zone DEFAULT now() NOT NULL,
    CONSTRAINT chk_followup_permission_role_code CHECK (((role_code)::text = ANY ((ARRAY['followup_admin'::character varying, 'followup_staff'::character varying])::text[])))
);

-- Current target table: compliance_rules
CREATE TABLE IF NOT EXISTS public.compliance_rules (
    id uuid DEFAULT gen_random_uuid() NOT NULL,
    rule_code character varying(128) NOT NULL,
    rule_name character varying(255) DEFAULT ''::character varying NOT NULL,
    subject character varying(32) NOT NULL,
    scene_codes jsonb DEFAULT '[]'::jsonb NOT NULL,
    role_codes jsonb DEFAULT '[]'::jsonb NOT NULL,
    version character varying(64) NOT NULL,
    status character varying(24) DEFAULT 'draft'::character varying NOT NULL,
    is_enabled boolean DEFAULT true NOT NULL,
    judgment_standard text DEFAULT ''::text NOT NULL,
    definition jsonb DEFAULT '{}'::jsonb NOT NULL,
    created_by bigint,
    created_at timestamp with time zone DEFAULT now() NOT NULL,
    published_at timestamp with time zone,
    updated_at timestamp with time zone DEFAULT now() NOT NULL,
    CONSTRAINT compliance_rules_definition_object_check CHECK ((jsonb_typeof(definition) = 'object'::text)),
    CONSTRAINT compliance_rules_role_codes_array_check CHECK ((jsonb_typeof(role_codes) = 'array'::text)),
    CONSTRAINT compliance_rules_scene_codes_array_check CHECK ((jsonb_typeof(scene_codes) = 'array'::text)),
    CONSTRAINT compliance_rules_status_check CHECK (((status)::text = ANY ((ARRAY['draft'::character varying, 'published'::character varying, 'disabled'::character varying])::text[]))),
    CONSTRAINT compliance_rules_subject_check CHECK (((subject)::text = ANY ((ARRAY['communication'::character varying, 'content'::character varying])::text[])))
);

-- Current target table: compliance_check_runs
CREATE TABLE IF NOT EXISTS public.compliance_check_runs (
    id uuid DEFAULT gen_random_uuid() NOT NULL,
    tenant_id bigint NOT NULL,
    source_type character varying(48) NOT NULL,
    source_id character varying(128) NOT NULL,
    source_version character varying(128) NOT NULL,
    source_fingerprint character varying(128) DEFAULT ''::character varying NOT NULL,
    subject character varying(32) NOT NULL,
    scene_code character varying(128) NOT NULL,
    role_code character varying(64) DEFAULT ''::character varying NOT NULL,
    employee_id bigint,
    input_fingerprint character varying(128) NOT NULL,
    prompt_code character varying(128) DEFAULT ''::character varying NOT NULL,
    prompt_version character varying(64) DEFAULT ''::character varying NOT NULL,
    rule_version character varying(64) DEFAULT ''::character varying NOT NULL,
    model_config_id bigint,
    model_code character varying(128) DEFAULT ''::character varying NOT NULL,
    execution_key character varying(512) NOT NULL,
    status character varying(24) DEFAULT 'processing'::character varying NOT NULL,
    finding_count integer DEFAULT 0 NOT NULL,
    finding_rule_codes jsonb DEFAULT '[]'::jsonb NOT NULL,
    raw_response jsonb DEFAULT '{}'::jsonb NOT NULL,
    result_json jsonb DEFAULT '{"findings": []}'::jsonb NOT NULL,
    error_message text DEFAULT ''::text NOT NULL,
    started_at timestamp with time zone,
    completed_at timestamp with time zone,
    created_at timestamp with time zone DEFAULT now() NOT NULL,
    updated_at timestamp with time zone DEFAULT now() NOT NULL,
    CONSTRAINT compliance_check_runs_finding_count_check CHECK ((finding_count >= 0)),
    CONSTRAINT compliance_check_runs_finding_rule_codes_array_check CHECK ((jsonb_typeof(finding_rule_codes) = 'array'::text)),
    CONSTRAINT compliance_check_runs_raw_response_object_check CHECK ((jsonb_typeof(raw_response) = 'object'::text)),
    CONSTRAINT compliance_check_runs_result_json_object_check CHECK ((jsonb_typeof(result_json) = 'object'::text)),
    CONSTRAINT compliance_check_runs_status_check CHECK (((status)::text = ANY ((ARRAY['processing'::character varying, 'completed'::character varying, 'failed'::character varying])::text[]))),
    CONSTRAINT compliance_check_runs_subject_check CHECK (((subject)::text = ANY ((ARRAY['communication'::character varying, 'content'::character varying])::text[]))),
    CONSTRAINT compliance_check_runs_tenant_check CHECK ((tenant_id > 0))
);

-- Current target table: compliance_findings
CREATE TABLE IF NOT EXISTS public.compliance_findings (
    id uuid DEFAULT gen_random_uuid() NOT NULL,
    run_id uuid NOT NULL,
    tenant_id bigint NOT NULL,
    source_type character varying(48) NOT NULL,
    source_id character varying(128) NOT NULL,
    source_version character varying(128) NOT NULL,
    subject character varying(32) NOT NULL,
    scene_code character varying(128) NOT NULL,
    role_code character varying(64) DEFAULT ''::character varying NOT NULL,
    employee_id bigint,
    rule_code character varying(128) NOT NULL,
    rule_version character varying(64) DEFAULT ''::character varying NOT NULL,
    fact text DEFAULT ''::text NOT NULL,
    summary text DEFAULT ''::text NOT NULL,
    reason text DEFAULT ''::text NOT NULL,
    evidence jsonb DEFAULT '[]'::jsonb NOT NULL,
    missing_facts jsonb DEFAULT '[]'::jsonb NOT NULL,
    needs_review boolean DEFAULT true NOT NULL,
    status character varying(32) DEFAULT 'pending'::character varying NOT NULL,
    review_conclusion character varying(32) DEFAULT ''::character varying NOT NULL,
    review_reason text DEFAULT ''::text NOT NULL,
    reviewed_by bigint,
    reviewed_at timestamp with time zone,
    created_at timestamp with time zone DEFAULT now() NOT NULL,
    updated_at timestamp with time zone DEFAULT now() NOT NULL,
    CONSTRAINT compliance_findings_evidence_array_check CHECK ((jsonb_typeof(evidence) = 'array'::text)),
    CONSTRAINT compliance_findings_missing_facts_array_check CHECK ((jsonb_typeof(missing_facts) = 'array'::text)),
    CONSTRAINT compliance_findings_status_check CHECK (((status)::text = ANY ((ARRAY['pending'::character varying, 'confirmed'::character varying, 'not_confirmed'::character varying, 'unable_to_determine'::character varying])::text[]))),
    CONSTRAINT compliance_findings_subject_check CHECK (((subject)::text = ANY ((ARRAY['communication'::character varying, 'content'::character varying])::text[]))),
    CONSTRAINT compliance_findings_tenant_check CHECK ((tenant_id > 0))
);

-- Current target table: compliance_permission_assignments
CREATE TABLE IF NOT EXISTS public.compliance_permission_assignments (
    id bigint NOT NULL,
    tenant_id bigint NOT NULL,
    employee_id bigint NOT NULL,
    enabled boolean DEFAULT false NOT NULL,
    role_code character varying(64) DEFAULT 'compliance_staff'::character varying NOT NULL,
    abilities_json jsonb DEFAULT '[]'::jsonb NOT NULL,
    created_by bigint,
    updated_by bigint,
    created_at timestamp with time zone DEFAULT now() NOT NULL,
    updated_at timestamp with time zone DEFAULT now() NOT NULL,
    CONSTRAINT chk_compliance_permission_abilities_array CHECK ((jsonb_typeof(abilities_json) = 'array'::text)),
    CONSTRAINT chk_compliance_permission_role_code CHECK (((role_code)::text = ANY ((ARRAY['compliance_admin'::character varying, 'compliance_staff'::character varying])::text[])))
);

-- Additive column expansion for pre-existing tables. New columns are nullable first; backfill/contract is a separate audited phase.
ALTER TABLE IF EXISTS public.encounters ADD COLUMN IF NOT EXISTS id bigint;
ALTER TABLE IF EXISTS public.encounters ADD COLUMN IF NOT EXISTS tenant_id bigint;
ALTER TABLE IF EXISTS public.encounters ADD COLUMN IF NOT EXISTS source_type character varying(32);
ALTER TABLE IF EXISTS public.encounters ADD COLUMN IF NOT EXISTS source_id bigint;
ALTER TABLE IF EXISTS public.encounters ADD COLUMN IF NOT EXISTS patient_id bigint;
ALTER TABLE IF EXISTS public.encounters ADD COLUMN IF NOT EXISTS patient_candidate_json jsonb;
ALTER TABLE IF EXISTS public.encounters ADD COLUMN IF NOT EXISTS provider_id bigint;
ALTER TABLE IF EXISTS public.encounters ADD COLUMN IF NOT EXISTS provider_name character varying(128);
ALTER TABLE IF EXISTS public.encounters ADD COLUMN IF NOT EXISTS provider_role character varying(64);
ALTER TABLE IF EXISTS public.encounters ADD COLUMN IF NOT EXISTS department_id bigint;
ALTER TABLE IF EXISTS public.encounters ADD COLUMN IF NOT EXISTS channel character varying(32);
ALTER TABLE IF EXISTS public.encounters ADD COLUMN IF NOT EXISTS visit_type character varying(32);
ALTER TABLE IF EXISTS public.encounters ADD COLUMN IF NOT EXISTS started_at timestamp with time zone;
ALTER TABLE IF EXISTS public.encounters ADD COLUMN IF NOT EXISTS ended_at timestamp with time zone;
ALTER TABLE IF EXISTS public.encounters ADD COLUMN IF NOT EXISTS status character varying(32);
ALTER TABLE IF EXISTS public.encounters ADD COLUMN IF NOT EXISTS created_at timestamp with time zone;
ALTER TABLE IF EXISTS public.encounters ADD COLUMN IF NOT EXISTS updated_at timestamp with time zone;
ALTER TABLE IF EXISTS public.encounters ADD COLUMN IF NOT EXISTS patient_name character varying(255);
ALTER TABLE IF EXISTS public.emr_templates ADD COLUMN IF NOT EXISTS id uuid;
ALTER TABLE IF EXISTS public.emr_templates ADD COLUMN IF NOT EXISTS tenant_id bigint;
ALTER TABLE IF EXISTS public.emr_templates ADD COLUMN IF NOT EXISTS code character varying(128);
ALTER TABLE IF EXISTS public.emr_templates ADD COLUMN IF NOT EXISTS name character varying(255);
ALTER TABLE IF EXISTS public.emr_templates ADD COLUMN IF NOT EXISTS version_no character varying(32);
ALTER TABLE IF EXISTS public.emr_templates ADD COLUMN IF NOT EXISTS document_type character varying(16);
ALTER TABLE IF EXISTS public.emr_templates ADD COLUMN IF NOT EXISTS visit_type character varying(16);
ALTER TABLE IF EXISTS public.emr_templates ADD COLUMN IF NOT EXISTS department_id bigint;
ALTER TABLE IF EXISTS public.emr_templates ADD COLUMN IF NOT EXISTS specialty character varying(64);
ALTER TABLE IF EXISTS public.emr_templates ADD COLUMN IF NOT EXISTS status character varying(16);
ALTER TABLE IF EXISTS public.emr_templates ADD COLUMN IF NOT EXISTS print_title character varying(255);
ALTER TABLE IF EXISTS public.emr_templates ADD COLUMN IF NOT EXISTS description text;
ALTER TABLE IF EXISTS public.emr_templates ADD COLUMN IF NOT EXISTS created_by bigint;
ALTER TABLE IF EXISTS public.emr_templates ADD COLUMN IF NOT EXISTS published_by bigint;
ALTER TABLE IF EXISTS public.emr_templates ADD COLUMN IF NOT EXISTS disabled_by bigint;
ALTER TABLE IF EXISTS public.emr_templates ADD COLUMN IF NOT EXISTS created_at timestamp with time zone;
ALTER TABLE IF EXISTS public.emr_templates ADD COLUMN IF NOT EXISTS published_at timestamp with time zone;
ALTER TABLE IF EXISTS public.emr_templates ADD COLUMN IF NOT EXISTS disabled_at timestamp with time zone;
ALTER TABLE IF EXISTS public.emr_templates ADD COLUMN IF NOT EXISTS updated_at timestamp with time zone;
ALTER TABLE IF EXISTS public.emr_templates ADD COLUMN IF NOT EXISTS updated_by bigint;
ALTER TABLE IF EXISTS public.emr_templates ADD COLUMN IF NOT EXISTS is_current boolean;
ALTER TABLE IF EXISTS public.emr_fields ADD COLUMN IF NOT EXISTS id uuid;
ALTER TABLE IF EXISTS public.emr_fields ADD COLUMN IF NOT EXISTS tenant_id bigint;
ALTER TABLE IF EXISTS public.emr_fields ADD COLUMN IF NOT EXISTS code character varying(128);
ALTER TABLE IF EXISTS public.emr_fields ADD COLUMN IF NOT EXISTS name character varying(255);
ALTER TABLE IF EXISTS public.emr_fields ADD COLUMN IF NOT EXISTS specialty character varying(64);
ALTER TABLE IF EXISTS public.emr_fields ADD COLUMN IF NOT EXISTS content_category character varying(32);
ALTER TABLE IF EXISTS public.emr_fields ADD COLUMN IF NOT EXISTS input_type character varying(32);
ALTER TABLE IF EXISTS public.emr_fields ADD COLUMN IF NOT EXISTS structure_json jsonb;
ALTER TABLE IF EXISTS public.emr_fields ADD COLUMN IF NOT EXISTS options_json jsonb;
ALTER TABLE IF EXISTS public.emr_fields ADD COLUMN IF NOT EXISTS description text;
ALTER TABLE IF EXISTS public.emr_fields ADD COLUMN IF NOT EXISTS status character varying(16);
ALTER TABLE IF EXISTS public.emr_fields ADD COLUMN IF NOT EXISTS created_by bigint;
ALTER TABLE IF EXISTS public.emr_fields ADD COLUMN IF NOT EXISTS updated_by bigint;
ALTER TABLE IF EXISTS public.emr_fields ADD COLUMN IF NOT EXISTS created_at timestamp with time zone;
ALTER TABLE IF EXISTS public.emr_fields ADD COLUMN IF NOT EXISTS updated_at timestamp with time zone;
ALTER TABLE IF EXISTS public.emr_template_fields ADD COLUMN IF NOT EXISTS id uuid;
ALTER TABLE IF EXISTS public.emr_template_fields ADD COLUMN IF NOT EXISTS tenant_id bigint;
ALTER TABLE IF EXISTS public.emr_template_fields ADD COLUMN IF NOT EXISTS template_id uuid;
ALTER TABLE IF EXISTS public.emr_template_fields ADD COLUMN IF NOT EXISTS field_id uuid;
ALTER TABLE IF EXISTS public.emr_template_fields ADD COLUMN IF NOT EXISTS display_order integer;
ALTER TABLE IF EXISTS public.emr_template_fields ADD COLUMN IF NOT EXISTS is_visible boolean;
ALTER TABLE IF EXISTS public.emr_template_fields ADD COLUMN IF NOT EXISTS is_required boolean;
ALTER TABLE IF EXISTS public.emr_template_fields ADD COLUMN IF NOT EXISTS description text;
ALTER TABLE IF EXISTS public.emr_template_fields ADD COLUMN IF NOT EXISTS created_by bigint;
ALTER TABLE IF EXISTS public.emr_template_fields ADD COLUMN IF NOT EXISTS created_at timestamp with time zone;
ALTER TABLE IF EXISTS public.emr_template_fields ADD COLUMN IF NOT EXISTS updated_at timestamp with time zone;
ALTER TABLE IF EXISTS public.emr_records ADD COLUMN IF NOT EXISTS id uuid;
ALTER TABLE IF EXISTS public.emr_records ADD COLUMN IF NOT EXISTS tenant_id bigint;
ALTER TABLE IF EXISTS public.emr_records ADD COLUMN IF NOT EXISTS encounter_id bigint;
ALTER TABLE IF EXISTS public.emr_records ADD COLUMN IF NOT EXISTS customer_id bigint;
ALTER TABLE IF EXISTS public.emr_records ADD COLUMN IF NOT EXISTS patient_snapshot_json jsonb;
ALTER TABLE IF EXISTS public.emr_records ADD COLUMN IF NOT EXISTS template_id uuid;
ALTER TABLE IF EXISTS public.emr_records ADD COLUMN IF NOT EXISTS document_type character varying(16);
ALTER TABLE IF EXISTS public.emr_records ADD COLUMN IF NOT EXISTS visit_type character varying(16);
ALTER TABLE IF EXISTS public.emr_records ADD COLUMN IF NOT EXISTS department_id bigint;
ALTER TABLE IF EXISTS public.emr_records ADD COLUMN IF NOT EXISTS doctor_id bigint;
ALTER TABLE IF EXISTS public.emr_records ADD COLUMN IF NOT EXISTS started_at timestamp with time zone;
ALTER TABLE IF EXISTS public.emr_records ADD COLUMN IF NOT EXISTS ended_at timestamp with time zone;
ALTER TABLE IF EXISTS public.emr_records ADD COLUMN IF NOT EXISTS encounter_context_json jsonb;
ALTER TABLE IF EXISTS public.emr_records ADD COLUMN IF NOT EXISTS source_references_json jsonb;
ALTER TABLE IF EXISTS public.emr_records ADD COLUMN IF NOT EXISTS content_json jsonb;
ALTER TABLE IF EXISTS public.emr_records ADD COLUMN IF NOT EXISTS status character varying(16);
ALTER TABLE IF EXISTS public.emr_records ADD COLUMN IF NOT EXISTS submitted_at timestamp with time zone;
ALTER TABLE IF EXISTS public.emr_records ADD COLUMN IF NOT EXISTS confirmed_by bigint;
ALTER TABLE IF EXISTS public.emr_records ADD COLUMN IF NOT EXISTS confirmed_at timestamp with time zone;
ALTER TABLE IF EXISTS public.emr_records ADD COLUMN IF NOT EXISTS archived_by bigint;
ALTER TABLE IF EXISTS public.emr_records ADD COLUMN IF NOT EXISTS archived_at timestamp with time zone;
ALTER TABLE IF EXISTS public.emr_records ADD COLUMN IF NOT EXISTS voided_by bigint;
ALTER TABLE IF EXISTS public.emr_records ADD COLUMN IF NOT EXISTS voided_at timestamp with time zone;
ALTER TABLE IF EXISTS public.emr_records ADD COLUMN IF NOT EXISTS created_by bigint;
ALTER TABLE IF EXISTS public.emr_records ADD COLUMN IF NOT EXISTS created_at timestamp with time zone;
ALTER TABLE IF EXISTS public.emr_records ADD COLUMN IF NOT EXISTS updated_at timestamp with time zone;
ALTER TABLE IF EXISTS public.emr_records ADD COLUMN IF NOT EXISTS revision_no integer;
ALTER TABLE IF EXISTS public.emr_records ADD COLUMN IF NOT EXISTS current_score_status character varying(16);
ALTER TABLE IF EXISTS public.emr_records ADD COLUMN IF NOT EXISTS current_total_score integer;
ALTER TABLE IF EXISTS public.emr_records ADD COLUMN IF NOT EXISTS current_grade character varying(8);
ALTER TABLE IF EXISTS public.emr_records ADD COLUMN IF NOT EXISTS current_score_version character varying(64);
ALTER TABLE IF EXISTS public.emr_records ADD COLUMN IF NOT EXISTS current_quality_run_id uuid;
ALTER TABLE IF EXISTS public.emr_record_snapshots ADD COLUMN IF NOT EXISTS id uuid;
ALTER TABLE IF EXISTS public.emr_record_snapshots ADD COLUMN IF NOT EXISTS record_id uuid;
ALTER TABLE IF EXISTS public.emr_record_snapshots ADD COLUMN IF NOT EXISTS encounter_id bigint;
ALTER TABLE IF EXISTS public.emr_record_snapshots ADD COLUMN IF NOT EXISTS revision_no integer;
ALTER TABLE IF EXISTS public.emr_record_snapshots ADD COLUMN IF NOT EXISTS snapshot_no integer;
ALTER TABLE IF EXISTS public.emr_record_snapshots ADD COLUMN IF NOT EXISTS template_version_id uuid;
ALTER TABLE IF EXISTS public.emr_record_snapshots ADD COLUMN IF NOT EXISTS patient_snapshot jsonb;
ALTER TABLE IF EXISTS public.emr_record_snapshots ADD COLUMN IF NOT EXISTS document_context jsonb;
ALTER TABLE IF EXISTS public.emr_record_snapshots ADD COLUMN IF NOT EXISTS content jsonb;
ALTER TABLE IF EXISTS public.emr_record_snapshots ADD COLUMN IF NOT EXISTS content_digest character varying(128);
ALTER TABLE IF EXISTS public.emr_record_snapshots ADD COLUMN IF NOT EXISTS formed_by bigint;
ALTER TABLE IF EXISTS public.emr_record_snapshots ADD COLUMN IF NOT EXISTS formed_at timestamp with time zone;
ALTER TABLE IF EXISTS public.emr_record_snapshots ADD COLUMN IF NOT EXISTS score_status character varying(16);
ALTER TABLE IF EXISTS public.emr_record_snapshots ADD COLUMN IF NOT EXISTS total_score integer;
ALTER TABLE IF EXISTS public.emr_record_snapshots ADD COLUMN IF NOT EXISTS grade character varying(8);
ALTER TABLE IF EXISTS public.emr_record_snapshots ADD COLUMN IF NOT EXISTS score_version character varying(64);
ALTER TABLE IF EXISTS public.emr_record_snapshots ADD COLUMN IF NOT EXISTS quality_run_id uuid;
ALTER TABLE IF EXISTS public.emr_process_records ADD COLUMN IF NOT EXISTS id uuid;
ALTER TABLE IF EXISTS public.emr_process_records ADD COLUMN IF NOT EXISTS tenant_id bigint;
ALTER TABLE IF EXISTS public.emr_process_records ADD COLUMN IF NOT EXISTS record_id uuid;
ALTER TABLE IF EXISTS public.emr_process_records ADD COLUMN IF NOT EXISTS action_type character varying(32);
ALTER TABLE IF EXISTS public.emr_process_records ADD COLUMN IF NOT EXISTS action_result character varying(8);
ALTER TABLE IF EXISTS public.emr_process_records ADD COLUMN IF NOT EXISTS occurred_at timestamp with time zone;
ALTER TABLE IF EXISTS public.emr_process_records ADD COLUMN IF NOT EXISTS actor_type character varying(8);
ALTER TABLE IF EXISTS public.emr_process_records ADD COLUMN IF NOT EXISTS actor_id bigint;
ALTER TABLE IF EXISTS public.emr_process_records ADD COLUMN IF NOT EXISTS source character varying(24);
ALTER TABLE IF EXISTS public.emr_process_records ADD COLUMN IF NOT EXISTS before_status character varying(16);
ALTER TABLE IF EXISTS public.emr_process_records ADD COLUMN IF NOT EXISTS after_status character varying(16);
ALTER TABLE IF EXISTS public.emr_process_records ADD COLUMN IF NOT EXISTS action_snapshot_id uuid;
ALTER TABLE IF EXISTS public.emr_process_records ADD COLUMN IF NOT EXISTS before_snapshot_id uuid;
ALTER TABLE IF EXISTS public.emr_process_records ADD COLUMN IF NOT EXISTS after_snapshot_id uuid;
ALTER TABLE IF EXISTS public.emr_process_records ADD COLUMN IF NOT EXISTS output_info jsonb;
ALTER TABLE IF EXISTS public.emr_process_records ADD COLUMN IF NOT EXISTS failure_reason text;
ALTER TABLE IF EXISTS public.emr_process_records ADD COLUMN IF NOT EXISTS action_note text;
ALTER TABLE IF EXISTS public.emr_process_records ADD COLUMN IF NOT EXISTS content_changes jsonb;
ALTER TABLE IF EXISTS public.emr_quality_rule_set_versions ADD COLUMN IF NOT EXISTS version character varying(32);
ALTER TABLE IF EXISTS public.emr_quality_rule_set_versions ADD COLUMN IF NOT EXISTS status character varying(16);
ALTER TABLE IF EXISTS public.emr_quality_rule_set_versions ADD COLUMN IF NOT EXISTS scoring_ready boolean;
ALTER TABLE IF EXISTS public.emr_quality_rule_set_versions ADD COLUMN IF NOT EXISTS note text;
ALTER TABLE IF EXISTS public.emr_quality_rule_set_versions ADD COLUMN IF NOT EXISTS created_at timestamp with time zone;
ALTER TABLE IF EXISTS public.emr_quality_rule_set_versions ADD COLUMN IF NOT EXISTS updated_at timestamp with time zone;
ALTER TABLE IF EXISTS public.emr_quality_rule_definitions ADD COLUMN IF NOT EXISTS id uuid;
ALTER TABLE IF EXISTS public.emr_quality_rule_definitions ADD COLUMN IF NOT EXISTS rule_code character varying(64);
ALTER TABLE IF EXISTS public.emr_quality_rule_definitions ADD COLUMN IF NOT EXISTS version character varying(32);
ALTER TABLE IF EXISTS public.emr_quality_rule_definitions ADD COLUMN IF NOT EXISTS section_code character varying(64);
ALTER TABLE IF EXISTS public.emr_quality_rule_definitions ADD COLUMN IF NOT EXISTS name character varying(255);
ALTER TABLE IF EXISTS public.emr_quality_rule_definitions ADD COLUMN IF NOT EXISTS source_text text;
ALTER TABLE IF EXISTS public.emr_quality_rule_definitions ADD COLUMN IF NOT EXISTS product_text text;
ALTER TABLE IF EXISTS public.emr_quality_rule_definitions ADD COLUMN IF NOT EXISTS standard_points integer;
ALTER TABLE IF EXISTS public.emr_quality_rule_definitions ADD COLUMN IF NOT EXISTS executor_kind character varying(16);
ALTER TABLE IF EXISTS public.emr_quality_rule_definitions ADD COLUMN IF NOT EXISTS input_spec jsonb;
ALTER TABLE IF EXISTS public.emr_quality_rule_definitions ADD COLUMN IF NOT EXISTS evaluation_spec jsonb;
ALTER TABLE IF EXISTS public.emr_quality_rule_definitions ADD COLUMN IF NOT EXISTS action_policy jsonb;
ALTER TABLE IF EXISTS public.emr_quality_rule_definitions ADD COLUMN IF NOT EXISTS message_templates jsonb;
ALTER TABLE IF EXISTS public.emr_quality_rule_definitions ADD COLUMN IF NOT EXISTS status character varying(16);
ALTER TABLE IF EXISTS public.emr_quality_rule_definitions ADD COLUMN IF NOT EXISTS created_at timestamp with time zone;
ALTER TABLE IF EXISTS public.emr_quality_rule_definitions ADD COLUMN IF NOT EXISTS updated_at timestamp with time zone;
ALTER TABLE IF EXISTS public.emr_quality_runs ADD COLUMN IF NOT EXISTS id uuid;
ALTER TABLE IF EXISTS public.emr_quality_runs ADD COLUMN IF NOT EXISTS tenant_id bigint;
ALTER TABLE IF EXISTS public.emr_quality_runs ADD COLUMN IF NOT EXISTS record_id uuid;
ALTER TABLE IF EXISTS public.emr_quality_runs ADD COLUMN IF NOT EXISTS snapshot_id uuid;
ALTER TABLE IF EXISTS public.emr_quality_runs ADD COLUMN IF NOT EXISTS action character varying(16);
ALTER TABLE IF EXISTS public.emr_quality_runs ADD COLUMN IF NOT EXISTS rule_set_version character varying(32);
ALTER TABLE IF EXISTS public.emr_quality_runs ADD COLUMN IF NOT EXISTS status character varying(16);
ALTER TABLE IF EXISTS public.emr_quality_runs ADD COLUMN IF NOT EXISTS overall_result character varying(16);
ALTER TABLE IF EXISTS public.emr_quality_runs ADD COLUMN IF NOT EXISTS total_score integer;
ALTER TABLE IF EXISTS public.emr_quality_runs ADD COLUMN IF NOT EXISTS grade character varying(8);
ALTER TABLE IF EXISTS public.emr_quality_runs ADD COLUMN IF NOT EXISTS major_score integer;
ALTER TABLE IF EXISTS public.emr_quality_runs ADD COLUMN IF NOT EXISTS blocking_count integer;
ALTER TABLE IF EXISTS public.emr_quality_runs ADD COLUMN IF NOT EXISTS pending_count integer;
ALTER TABLE IF EXISTS public.emr_quality_runs ADD COLUMN IF NOT EXISTS started_at timestamp with time zone;
ALTER TABLE IF EXISTS public.emr_quality_runs ADD COLUMN IF NOT EXISTS completed_at timestamp with time zone;
ALTER TABLE IF EXISTS public.emr_quality_runs ADD COLUMN IF NOT EXISTS failure_reason text;
ALTER TABLE IF EXISTS public.emr_quality_runs ADD COLUMN IF NOT EXISTS created_at timestamp with time zone;
ALTER TABLE IF EXISTS public.emr_quality_runs ADD COLUMN IF NOT EXISTS model_audit jsonb;
ALTER TABLE IF EXISTS public.emr_quality_runs ADD COLUMN IF NOT EXISTS input_digest character varying(128);
ALTER TABLE IF EXISTS public.emr_quality_results ADD COLUMN IF NOT EXISTS id uuid;
ALTER TABLE IF EXISTS public.emr_quality_results ADD COLUMN IF NOT EXISTS run_id uuid;
ALTER TABLE IF EXISTS public.emr_quality_results ADD COLUMN IF NOT EXISTS rule_id uuid;
ALTER TABLE IF EXISTS public.emr_quality_results ADD COLUMN IF NOT EXISTS rule_code character varying(64);
ALTER TABLE IF EXISTS public.emr_quality_results ADD COLUMN IF NOT EXISTS section_code character varying(64);
ALTER TABLE IF EXISTS public.emr_quality_results ADD COLUMN IF NOT EXISTS result character varying(16);
ALTER TABLE IF EXISTS public.emr_quality_results ADD COLUMN IF NOT EXISTS execution_kind character varying(16);
ALTER TABLE IF EXISTS public.emr_quality_results ADD COLUMN IF NOT EXISTS evidence jsonb;
ALTER TABLE IF EXISTS public.emr_quality_results ADD COLUMN IF NOT EXISTS reason text;
ALTER TABLE IF EXISTS public.emr_quality_results ADD COLUMN IF NOT EXISTS deduction integer;
ALTER TABLE IF EXISTS public.emr_quality_results ADD COLUMN IF NOT EXISTS message text;
ALTER TABLE IF EXISTS public.emr_quality_results ADD COLUMN IF NOT EXISTS blocking boolean;
ALTER TABLE IF EXISTS public.emr_quality_results ADD COLUMN IF NOT EXISTS manual_review boolean;
ALTER TABLE IF EXISTS public.emr_quality_results ADD COLUMN IF NOT EXISTS created_at timestamp with time zone;
ALTER TABLE IF EXISTS public.emr_record_score_current ADD COLUMN IF NOT EXISTS record_id uuid;
ALTER TABLE IF EXISTS public.emr_record_score_current ADD COLUMN IF NOT EXISTS tenant_id bigint;
ALTER TABLE IF EXISTS public.emr_record_score_current ADD COLUMN IF NOT EXISTS snapshot_id uuid;
ALTER TABLE IF EXISTS public.emr_record_score_current ADD COLUMN IF NOT EXISTS score_version character varying(64);
ALTER TABLE IF EXISTS public.emr_record_score_current ADD COLUMN IF NOT EXISTS status character varying(16);
ALTER TABLE IF EXISTS public.emr_record_score_current ADD COLUMN IF NOT EXISTS total_score integer;
ALTER TABLE IF EXISTS public.emr_record_score_current ADD COLUMN IF NOT EXISTS grade character varying(8);
ALTER TABLE IF EXISTS public.emr_record_score_current ADD COLUMN IF NOT EXISTS program_score integer;
ALTER TABLE IF EXISTS public.emr_record_score_current ADD COLUMN IF NOT EXISTS program_deduct_points integer;
ALTER TABLE IF EXISTS public.emr_record_score_current ADD COLUMN IF NOT EXISTS major_program_score integer;
ALTER TABLE IF EXISTS public.emr_record_score_current ADD COLUMN IF NOT EXISTS direct_grade character varying(8);
ALTER TABLE IF EXISTS public.emr_record_score_current ADD COLUMN IF NOT EXISTS direct_grade_reason text;
ALTER TABLE IF EXISTS public.emr_record_score_current ADD COLUMN IF NOT EXISTS pending_sections jsonb;
ALTER TABLE IF EXISTS public.emr_record_score_current ADD COLUMN IF NOT EXISTS failure_reason text;
ALTER TABLE IF EXISTS public.emr_record_score_current ADD COLUMN IF NOT EXISTS scored_at timestamp with time zone;
ALTER TABLE IF EXISTS public.emr_record_score_current ADD COLUMN IF NOT EXISTS updated_at timestamp with time zone;
ALTER TABLE IF EXISTS public.emr_record_score_current_items ADD COLUMN IF NOT EXISTS id uuid;
ALTER TABLE IF EXISTS public.emr_record_score_current_items ADD COLUMN IF NOT EXISTS tenant_id bigint;
ALTER TABLE IF EXISTS public.emr_record_score_current_items ADD COLUMN IF NOT EXISTS record_id uuid;
ALTER TABLE IF EXISTS public.emr_record_score_current_items ADD COLUMN IF NOT EXISTS section_code character varying(64);
ALTER TABLE IF EXISTS public.emr_record_score_current_items ADD COLUMN IF NOT EXISTS section_name character varying(64);
ALTER TABLE IF EXISTS public.emr_record_score_current_items ADD COLUMN IF NOT EXISTS standard_score integer;
ALTER TABLE IF EXISTS public.emr_record_score_current_items ADD COLUMN IF NOT EXISTS score integer;
ALTER TABLE IF EXISTS public.emr_record_score_current_items ADD COLUMN IF NOT EXISTS program_deduct_points integer;
ALTER TABLE IF EXISTS public.emr_record_score_current_items ADD COLUMN IF NOT EXISTS pending boolean;
ALTER TABLE IF EXISTS public.emr_record_score_current_items ADD COLUMN IF NOT EXISTS findings jsonb;
ALTER TABLE IF EXISTS public.emr_record_score_current_items ADD COLUMN IF NOT EXISTS sort_order integer;
ALTER TABLE IF EXISTS public.emr_record_score_current_items ADD COLUMN IF NOT EXISTS updated_at timestamp with time zone;
ALTER TABLE IF EXISTS public.emr_score_standards ADD COLUMN IF NOT EXISTS id uuid;
ALTER TABLE IF EXISTS public.emr_score_standards ADD COLUMN IF NOT EXISTS code character varying(128);
ALTER TABLE IF EXISTS public.emr_score_standards ADD COLUMN IF NOT EXISTS version character varying(64);
ALTER TABLE IF EXISTS public.emr_score_standards ADD COLUMN IF NOT EXISTS name character varying(255);
ALTER TABLE IF EXISTS public.emr_score_standards ADD COLUMN IF NOT EXISTS source_name character varying(255);
ALTER TABLE IF EXISTS public.emr_score_standards ADD COLUMN IF NOT EXISTS source_reference text;
ALTER TABLE IF EXISTS public.emr_score_standards ADD COLUMN IF NOT EXISTS standard_text text;
ALTER TABLE IF EXISTS public.emr_score_standards ADD COLUMN IF NOT EXISTS status character varying(16);
ALTER TABLE IF EXISTS public.emr_score_standards ADD COLUMN IF NOT EXISTS published_at timestamp with time zone;
ALTER TABLE IF EXISTS public.emr_score_standards ADD COLUMN IF NOT EXISTS created_at timestamp with time zone;
ALTER TABLE IF EXISTS public.emr_score_standards ADD COLUMN IF NOT EXISTS updated_at timestamp with time zone;
ALTER TABLE IF EXISTS public.emr_icd10_diagnosis_catalog ADD COLUMN IF NOT EXISTS id bigint;
ALTER TABLE IF EXISTS public.emr_icd10_diagnosis_catalog ADD COLUMN IF NOT EXISTS catalog_code text;
ALTER TABLE IF EXISTS public.emr_icd10_diagnosis_catalog ADD COLUMN IF NOT EXISTS catalog_name text;
ALTER TABLE IF EXISTS public.emr_icd10_diagnosis_catalog ADD COLUMN IF NOT EXISTS catalog_version text;
ALTER TABLE IF EXISTS public.emr_icd10_diagnosis_catalog ADD COLUMN IF NOT EXISTS chapter_no text;
ALTER TABLE IF EXISTS public.emr_icd10_diagnosis_catalog ADD COLUMN IF NOT EXISTS chapter_code_range text;
ALTER TABLE IF EXISTS public.emr_icd10_diagnosis_catalog ADD COLUMN IF NOT EXISTS chapter_name text;
ALTER TABLE IF EXISTS public.emr_icd10_diagnosis_catalog ADD COLUMN IF NOT EXISTS section_code_range text;
ALTER TABLE IF EXISTS public.emr_icd10_diagnosis_catalog ADD COLUMN IF NOT EXISTS section_name text;
ALTER TABLE IF EXISTS public.emr_icd10_diagnosis_catalog ADD COLUMN IF NOT EXISTS category_code text;
ALTER TABLE IF EXISTS public.emr_icd10_diagnosis_catalog ADD COLUMN IF NOT EXISTS category_name text;
ALTER TABLE IF EXISTS public.emr_icd10_diagnosis_catalog ADD COLUMN IF NOT EXISTS subcategory_code text;
ALTER TABLE IF EXISTS public.emr_icd10_diagnosis_catalog ADD COLUMN IF NOT EXISTS subcategory_name text;
ALTER TABLE IF EXISTS public.emr_icd10_diagnosis_catalog ADD COLUMN IF NOT EXISTS diagnosis_code text;
ALTER TABLE IF EXISTS public.emr_icd10_diagnosis_catalog ADD COLUMN IF NOT EXISTS diagnosis_name text;
ALTER TABLE IF EXISTS public.emr_icd10_diagnosis_catalog ADD COLUMN IF NOT EXISTS normalized_name text;
ALTER TABLE IF EXISTS public.emr_icd10_diagnosis_catalog ADD COLUMN IF NOT EXISTS source_file text;
ALTER TABLE IF EXISTS public.emr_icd10_diagnosis_catalog ADD COLUMN IF NOT EXISTS source_page integer;
ALTER TABLE IF EXISTS public.emr_icd10_diagnosis_catalog ADD COLUMN IF NOT EXISTS is_active boolean;
ALTER TABLE IF EXISTS public.emr_icd10_diagnosis_catalog ADD COLUMN IF NOT EXISTS imported_at timestamp with time zone;
ALTER TABLE IF EXISTS public.emr_icd10_diagnosis_catalog ADD COLUMN IF NOT EXISTS created_at timestamp with time zone;
ALTER TABLE IF EXISTS public.emr_permission_assignments ADD COLUMN IF NOT EXISTS id bigint;
ALTER TABLE IF EXISTS public.emr_permission_assignments ADD COLUMN IF NOT EXISTS tenant_id bigint;
ALTER TABLE IF EXISTS public.emr_permission_assignments ADD COLUMN IF NOT EXISTS employee_id bigint;
ALTER TABLE IF EXISTS public.emr_permission_assignments ADD COLUMN IF NOT EXISTS enabled boolean;
ALTER TABLE IF EXISTS public.emr_permission_assignments ADD COLUMN IF NOT EXISTS emr_role_code character varying(64);
ALTER TABLE IF EXISTS public.emr_permission_assignments ADD COLUMN IF NOT EXISTS scope character varying(32);
ALTER TABLE IF EXISTS public.emr_permission_assignments ADD COLUMN IF NOT EXISTS abilities_json jsonb;
ALTER TABLE IF EXISTS public.emr_permission_assignments ADD COLUMN IF NOT EXISTS created_by bigint;
ALTER TABLE IF EXISTS public.emr_permission_assignments ADD COLUMN IF NOT EXISTS updated_by bigint;
ALTER TABLE IF EXISTS public.emr_permission_assignments ADD COLUMN IF NOT EXISTS created_at timestamp with time zone;
ALTER TABLE IF EXISTS public.emr_permission_assignments ADD COLUMN IF NOT EXISTS updated_at timestamp with time zone;
ALTER TABLE IF EXISTS public.emr_badge_sessions ADD COLUMN IF NOT EXISTS id uuid;
ALTER TABLE IF EXISTS public.emr_badge_sessions ADD COLUMN IF NOT EXISTS tenant_id bigint;
ALTER TABLE IF EXISTS public.emr_badge_sessions ADD COLUMN IF NOT EXISTS employee_id bigint;
ALTER TABLE IF EXISTS public.emr_badge_sessions ADD COLUMN IF NOT EXISTS device_no character varying(128);
ALTER TABLE IF EXISTS public.emr_badge_sessions ADD COLUMN IF NOT EXISTS customer_id bigint;
ALTER TABLE IF EXISTS public.emr_badge_sessions ADD COLUMN IF NOT EXISTS started_at timestamp with time zone;
ALTER TABLE IF EXISTS public.emr_badge_sessions ADD COLUMN IF NOT EXISTS stopped_at timestamp with time zone;
ALTER TABLE IF EXISTS public.emr_badge_sessions ADD COLUMN IF NOT EXISTS recording_id bigint;
ALTER TABLE IF EXISTS public.emr_badge_sessions ADD COLUMN IF NOT EXISTS error_message text;
ALTER TABLE IF EXISTS public.emr_badge_sessions ADD COLUMN IF NOT EXISTS created_at timestamp with time zone;
ALTER TABLE IF EXISTS public.emr_badge_sessions ADD COLUMN IF NOT EXISTS updated_at timestamp with time zone;
ALTER TABLE IF EXISTS public.emr_badge_sessions ADD COLUMN IF NOT EXISTS order_no character varying(255);
ALTER TABLE IF EXISTS public.emr_badge_sessions ADD COLUMN IF NOT EXISTS state character varying(32);
ALTER TABLE IF EXISTS public.emr_followup_deliveries ADD COLUMN IF NOT EXISTS tenant_id bigint;
ALTER TABLE IF EXISTS public.emr_followup_deliveries ADD COLUMN IF NOT EXISTS recording_id bigint;
ALTER TABLE IF EXISTS public.emr_followup_deliveries ADD COLUMN IF NOT EXISTS status text;
ALTER TABLE IF EXISTS public.emr_followup_deliveries ADD COLUMN IF NOT EXISTS attempt_count integer;
ALTER TABLE IF EXISTS public.emr_followup_deliveries ADD COLUMN IF NOT EXISTS next_attempt_at timestamp with time zone;
ALTER TABLE IF EXISTS public.emr_followup_deliveries ADD COLUMN IF NOT EXISTS request_id text;
ALTER TABLE IF EXISTS public.emr_followup_deliveries ADD COLUMN IF NOT EXISTS last_error text;
ALTER TABLE IF EXISTS public.emr_followup_deliveries ADD COLUMN IF NOT EXISTS created_at timestamp with time zone;
ALTER TABLE IF EXISTS public.emr_followup_deliveries ADD COLUMN IF NOT EXISTS updated_at timestamp with time zone;
ALTER TABLE IF EXISTS public.followup_generation_runs ADD COLUMN IF NOT EXISTS id bigint;
ALTER TABLE IF EXISTS public.followup_generation_runs ADD COLUMN IF NOT EXISTS tenant_id integer;
ALTER TABLE IF EXISTS public.followup_generation_runs ADD COLUMN IF NOT EXISTS recording_id integer;
ALTER TABLE IF EXISTS public.followup_generation_runs ADD COLUMN IF NOT EXISTS encounter_id integer;
ALTER TABLE IF EXISTS public.followup_generation_runs ADD COLUMN IF NOT EXISTS employee_id integer;
ALTER TABLE IF EXISTS public.followup_generation_runs ADD COLUMN IF NOT EXISTS customer_id integer;
ALTER TABLE IF EXISTS public.followup_generation_runs ADD COLUMN IF NOT EXISTS customer_name character varying(255);
ALTER TABLE IF EXISTS public.followup_generation_runs ADD COLUMN IF NOT EXISTS doctor_name character varying(255);
ALTER TABLE IF EXISTS public.followup_generation_runs ADD COLUMN IF NOT EXISTS recorded_at timestamp without time zone;
ALTER TABLE IF EXISTS public.followup_generation_runs ADD COLUMN IF NOT EXISTS request_id character varying(128);
ALTER TABLE IF EXISTS public.followup_generation_runs ADD COLUMN IF NOT EXISTS request_payload jsonb;
ALTER TABLE IF EXISTS public.followup_generation_runs ADD COLUMN IF NOT EXISTS prompt_code character varying(128);
ALTER TABLE IF EXISTS public.followup_generation_runs ADD COLUMN IF NOT EXISTS prompt_version character varying(64);
ALTER TABLE IF EXISTS public.followup_generation_runs ADD COLUMN IF NOT EXISTS generation_model character varying(128);
ALTER TABLE IF EXISTS public.followup_generation_runs ADD COLUMN IF NOT EXISTS llm_request_id character varying(128);
ALTER TABLE IF EXISTS public.followup_generation_runs ADD COLUMN IF NOT EXISTS status character varying(32);
ALTER TABLE IF EXISTS public.followup_generation_runs ADD COLUMN IF NOT EXISTS attempt_count integer;
ALTER TABLE IF EXISTS public.followup_generation_runs ADD COLUMN IF NOT EXISTS max_attempts integer;
ALTER TABLE IF EXISTS public.followup_generation_runs ADD COLUMN IF NOT EXISTS next_retry_at timestamp without time zone;
ALTER TABLE IF EXISTS public.followup_generation_runs ADD COLUMN IF NOT EXISTS last_error_code character varying(128);
ALTER TABLE IF EXISTS public.followup_generation_runs ADD COLUMN IF NOT EXISTS last_error_message text;
ALTER TABLE IF EXISTS public.followup_generation_runs ADD COLUMN IF NOT EXISTS llm_response_payload jsonb;
ALTER TABLE IF EXISTS public.followup_generation_runs ADD COLUMN IF NOT EXISTS generated_tasks_payload jsonb;
ALTER TABLE IF EXISTS public.followup_generation_runs ADD COLUMN IF NOT EXISTS persisted_task_count integer;
ALTER TABLE IF EXISTS public.followup_generation_runs ADD COLUMN IF NOT EXISTS started_at timestamp without time zone;
ALTER TABLE IF EXISTS public.followup_generation_runs ADD COLUMN IF NOT EXISTS completed_at timestamp without time zone;
ALTER TABLE IF EXISTS public.followup_generation_runs ADD COLUMN IF NOT EXISTS failed_at timestamp without time zone;
ALTER TABLE IF EXISTS public.followup_generation_runs ADD COLUMN IF NOT EXISTS created_at timestamp without time zone;
ALTER TABLE IF EXISTS public.followup_generation_runs ADD COLUMN IF NOT EXISTS updated_at timestamp without time zone;
ALTER TABLE IF EXISTS public.followup_data_scopes ADD COLUMN IF NOT EXISTS id bigint;
ALTER TABLE IF EXISTS public.followup_data_scopes ADD COLUMN IF NOT EXISTS tenant_id integer;
ALTER TABLE IF EXISTS public.followup_data_scopes ADD COLUMN IF NOT EXISTS viewer_employee_id integer;
ALTER TABLE IF EXISTS public.followup_data_scopes ADD COLUMN IF NOT EXISTS target_employee_id integer;
ALTER TABLE IF EXISTS public.followup_data_scopes ADD COLUMN IF NOT EXISTS created_by integer;
ALTER TABLE IF EXISTS public.followup_data_scopes ADD COLUMN IF NOT EXISTS created_at timestamp without time zone;
ALTER TABLE IF EXISTS public.followup_data_scopes ADD COLUMN IF NOT EXISTS updated_at timestamp without time zone;
ALTER TABLE IF EXISTS public.followup_permission_assignments ADD COLUMN IF NOT EXISTS id bigint;
ALTER TABLE IF EXISTS public.followup_permission_assignments ADD COLUMN IF NOT EXISTS tenant_id bigint;
ALTER TABLE IF EXISTS public.followup_permission_assignments ADD COLUMN IF NOT EXISTS employee_id bigint;
ALTER TABLE IF EXISTS public.followup_permission_assignments ADD COLUMN IF NOT EXISTS enabled boolean;
ALTER TABLE IF EXISTS public.followup_permission_assignments ADD COLUMN IF NOT EXISTS role_code character varying(64);
ALTER TABLE IF EXISTS public.followup_permission_assignments ADD COLUMN IF NOT EXISTS abilities_json jsonb;
ALTER TABLE IF EXISTS public.followup_permission_assignments ADD COLUMN IF NOT EXISTS created_by bigint;
ALTER TABLE IF EXISTS public.followup_permission_assignments ADD COLUMN IF NOT EXISTS updated_by bigint;
ALTER TABLE IF EXISTS public.followup_permission_assignments ADD COLUMN IF NOT EXISTS created_at timestamp with time zone;
ALTER TABLE IF EXISTS public.followup_permission_assignments ADD COLUMN IF NOT EXISTS updated_at timestamp with time zone;
ALTER TABLE IF EXISTS public.compliance_rules ADD COLUMN IF NOT EXISTS id uuid;
ALTER TABLE IF EXISTS public.compliance_rules ADD COLUMN IF NOT EXISTS rule_code character varying(128);
ALTER TABLE IF EXISTS public.compliance_rules ADD COLUMN IF NOT EXISTS rule_name character varying(255);
ALTER TABLE IF EXISTS public.compliance_rules ADD COLUMN IF NOT EXISTS subject character varying(32);
ALTER TABLE IF EXISTS public.compliance_rules ADD COLUMN IF NOT EXISTS scene_codes jsonb;
ALTER TABLE IF EXISTS public.compliance_rules ADD COLUMN IF NOT EXISTS role_codes jsonb;
ALTER TABLE IF EXISTS public.compliance_rules ADD COLUMN IF NOT EXISTS version character varying(64);
ALTER TABLE IF EXISTS public.compliance_rules ADD COLUMN IF NOT EXISTS status character varying(24);
ALTER TABLE IF EXISTS public.compliance_rules ADD COLUMN IF NOT EXISTS is_enabled boolean;
ALTER TABLE IF EXISTS public.compliance_rules ADD COLUMN IF NOT EXISTS judgment_standard text;
ALTER TABLE IF EXISTS public.compliance_rules ADD COLUMN IF NOT EXISTS definition jsonb;
ALTER TABLE IF EXISTS public.compliance_rules ADD COLUMN IF NOT EXISTS created_by bigint;
ALTER TABLE IF EXISTS public.compliance_rules ADD COLUMN IF NOT EXISTS created_at timestamp with time zone;
ALTER TABLE IF EXISTS public.compliance_rules ADD COLUMN IF NOT EXISTS published_at timestamp with time zone;
ALTER TABLE IF EXISTS public.compliance_rules ADD COLUMN IF NOT EXISTS updated_at timestamp with time zone;
ALTER TABLE IF EXISTS public.compliance_check_runs ADD COLUMN IF NOT EXISTS id uuid;
ALTER TABLE IF EXISTS public.compliance_check_runs ADD COLUMN IF NOT EXISTS tenant_id bigint;
ALTER TABLE IF EXISTS public.compliance_check_runs ADD COLUMN IF NOT EXISTS source_type character varying(48);
ALTER TABLE IF EXISTS public.compliance_check_runs ADD COLUMN IF NOT EXISTS source_id character varying(128);
ALTER TABLE IF EXISTS public.compliance_check_runs ADD COLUMN IF NOT EXISTS source_version character varying(128);
ALTER TABLE IF EXISTS public.compliance_check_runs ADD COLUMN IF NOT EXISTS source_fingerprint character varying(128);
ALTER TABLE IF EXISTS public.compliance_check_runs ADD COLUMN IF NOT EXISTS subject character varying(32);
ALTER TABLE IF EXISTS public.compliance_check_runs ADD COLUMN IF NOT EXISTS scene_code character varying(128);
ALTER TABLE IF EXISTS public.compliance_check_runs ADD COLUMN IF NOT EXISTS role_code character varying(64);
ALTER TABLE IF EXISTS public.compliance_check_runs ADD COLUMN IF NOT EXISTS employee_id bigint;
ALTER TABLE IF EXISTS public.compliance_check_runs ADD COLUMN IF NOT EXISTS input_fingerprint character varying(128);
ALTER TABLE IF EXISTS public.compliance_check_runs ADD COLUMN IF NOT EXISTS prompt_code character varying(128);
ALTER TABLE IF EXISTS public.compliance_check_runs ADD COLUMN IF NOT EXISTS prompt_version character varying(64);
ALTER TABLE IF EXISTS public.compliance_check_runs ADD COLUMN IF NOT EXISTS rule_version character varying(64);
ALTER TABLE IF EXISTS public.compliance_check_runs ADD COLUMN IF NOT EXISTS model_config_id bigint;
ALTER TABLE IF EXISTS public.compliance_check_runs ADD COLUMN IF NOT EXISTS model_code character varying(128);
ALTER TABLE IF EXISTS public.compliance_check_runs ADD COLUMN IF NOT EXISTS execution_key character varying(512);
ALTER TABLE IF EXISTS public.compliance_check_runs ADD COLUMN IF NOT EXISTS status character varying(24);
ALTER TABLE IF EXISTS public.compliance_check_runs ADD COLUMN IF NOT EXISTS finding_count integer;
ALTER TABLE IF EXISTS public.compliance_check_runs ADD COLUMN IF NOT EXISTS finding_rule_codes jsonb;
ALTER TABLE IF EXISTS public.compliance_check_runs ADD COLUMN IF NOT EXISTS raw_response jsonb;
ALTER TABLE IF EXISTS public.compliance_check_runs ADD COLUMN IF NOT EXISTS result_json jsonb;
ALTER TABLE IF EXISTS public.compliance_check_runs ADD COLUMN IF NOT EXISTS error_message text;
ALTER TABLE IF EXISTS public.compliance_check_runs ADD COLUMN IF NOT EXISTS started_at timestamp with time zone;
ALTER TABLE IF EXISTS public.compliance_check_runs ADD COLUMN IF NOT EXISTS completed_at timestamp with time zone;
ALTER TABLE IF EXISTS public.compliance_check_runs ADD COLUMN IF NOT EXISTS created_at timestamp with time zone;
ALTER TABLE IF EXISTS public.compliance_check_runs ADD COLUMN IF NOT EXISTS updated_at timestamp with time zone;
ALTER TABLE IF EXISTS public.compliance_findings ADD COLUMN IF NOT EXISTS id uuid;
ALTER TABLE IF EXISTS public.compliance_findings ADD COLUMN IF NOT EXISTS run_id uuid;
ALTER TABLE IF EXISTS public.compliance_findings ADD COLUMN IF NOT EXISTS tenant_id bigint;
ALTER TABLE IF EXISTS public.compliance_findings ADD COLUMN IF NOT EXISTS source_type character varying(48);
ALTER TABLE IF EXISTS public.compliance_findings ADD COLUMN IF NOT EXISTS source_id character varying(128);
ALTER TABLE IF EXISTS public.compliance_findings ADD COLUMN IF NOT EXISTS source_version character varying(128);
ALTER TABLE IF EXISTS public.compliance_findings ADD COLUMN IF NOT EXISTS subject character varying(32);
ALTER TABLE IF EXISTS public.compliance_findings ADD COLUMN IF NOT EXISTS scene_code character varying(128);
ALTER TABLE IF EXISTS public.compliance_findings ADD COLUMN IF NOT EXISTS role_code character varying(64);
ALTER TABLE IF EXISTS public.compliance_findings ADD COLUMN IF NOT EXISTS employee_id bigint;
ALTER TABLE IF EXISTS public.compliance_findings ADD COLUMN IF NOT EXISTS rule_code character varying(128);
ALTER TABLE IF EXISTS public.compliance_findings ADD COLUMN IF NOT EXISTS rule_version character varying(64);
ALTER TABLE IF EXISTS public.compliance_findings ADD COLUMN IF NOT EXISTS fact text;
ALTER TABLE IF EXISTS public.compliance_findings ADD COLUMN IF NOT EXISTS summary text;
ALTER TABLE IF EXISTS public.compliance_findings ADD COLUMN IF NOT EXISTS reason text;
ALTER TABLE IF EXISTS public.compliance_findings ADD COLUMN IF NOT EXISTS evidence jsonb;
ALTER TABLE IF EXISTS public.compliance_findings ADD COLUMN IF NOT EXISTS missing_facts jsonb;
ALTER TABLE IF EXISTS public.compliance_findings ADD COLUMN IF NOT EXISTS needs_review boolean;
ALTER TABLE IF EXISTS public.compliance_findings ADD COLUMN IF NOT EXISTS status character varying(32);
ALTER TABLE IF EXISTS public.compliance_findings ADD COLUMN IF NOT EXISTS review_conclusion character varying(32);
ALTER TABLE IF EXISTS public.compliance_findings ADD COLUMN IF NOT EXISTS review_reason text;
ALTER TABLE IF EXISTS public.compliance_findings ADD COLUMN IF NOT EXISTS reviewed_by bigint;
ALTER TABLE IF EXISTS public.compliance_findings ADD COLUMN IF NOT EXISTS reviewed_at timestamp with time zone;
ALTER TABLE IF EXISTS public.compliance_findings ADD COLUMN IF NOT EXISTS created_at timestamp with time zone;
ALTER TABLE IF EXISTS public.compliance_findings ADD COLUMN IF NOT EXISTS updated_at timestamp with time zone;
ALTER TABLE IF EXISTS public.compliance_permission_assignments ADD COLUMN IF NOT EXISTS id bigint;
ALTER TABLE IF EXISTS public.compliance_permission_assignments ADD COLUMN IF NOT EXISTS tenant_id bigint;
ALTER TABLE IF EXISTS public.compliance_permission_assignments ADD COLUMN IF NOT EXISTS employee_id bigint;
ALTER TABLE IF EXISTS public.compliance_permission_assignments ADD COLUMN IF NOT EXISTS enabled boolean;
ALTER TABLE IF EXISTS public.compliance_permission_assignments ADD COLUMN IF NOT EXISTS role_code character varying(64);
ALTER TABLE IF EXISTS public.compliance_permission_assignments ADD COLUMN IF NOT EXISTS abilities_json jsonb;
ALTER TABLE IF EXISTS public.compliance_permission_assignments ADD COLUMN IF NOT EXISTS created_by bigint;
ALTER TABLE IF EXISTS public.compliance_permission_assignments ADD COLUMN IF NOT EXISTS updated_by bigint;
ALTER TABLE IF EXISTS public.compliance_permission_assignments ADD COLUMN IF NOT EXISTS created_at timestamp with time zone;
ALTER TABLE IF EXISTS public.compliance_permission_assignments ADD COLUMN IF NOT EXISTS updated_at timestamp with time zone;

ALTER TABLE IF EXISTS public.encounters ALTER COLUMN patient_candidate_json SET DEFAULT '{}'::jsonb;
ALTER TABLE IF EXISTS public.encounters ALTER COLUMN provider_name SET DEFAULT ''::character varying;
ALTER TABLE IF EXISTS public.encounters ALTER COLUMN provider_role SET DEFAULT ''::character varying;
ALTER TABLE IF EXISTS public.encounters ALTER COLUMN channel SET DEFAULT 'store'::character varying;
ALTER TABLE IF EXISTS public.encounters ALTER COLUMN visit_type SET DEFAULT 'consultation'::character varying;
ALTER TABLE IF EXISTS public.encounters ALTER COLUMN status SET DEFAULT 'new'::character varying;
ALTER TABLE IF EXISTS public.encounters ALTER COLUMN created_at SET DEFAULT now();
ALTER TABLE IF EXISTS public.encounters ALTER COLUMN updated_at SET DEFAULT now();
ALTER TABLE IF EXISTS public.emr_templates ALTER COLUMN id SET DEFAULT gen_random_uuid();
ALTER TABLE IF EXISTS public.emr_templates ALTER COLUMN tenant_id SET DEFAULT 0;
ALTER TABLE IF EXISTS public.emr_templates ALTER COLUMN status SET DEFAULT 'draft'::character varying;
ALTER TABLE IF EXISTS public.emr_templates ALTER COLUMN print_title SET DEFAULT ''::character varying;
ALTER TABLE IF EXISTS public.emr_templates ALTER COLUMN description SET DEFAULT ''::text;
ALTER TABLE IF EXISTS public.emr_templates ALTER COLUMN created_by SET DEFAULT 0;
ALTER TABLE IF EXISTS public.emr_templates ALTER COLUMN created_at SET DEFAULT now();
ALTER TABLE IF EXISTS public.emr_templates ALTER COLUMN updated_at SET DEFAULT now();
ALTER TABLE IF EXISTS public.emr_templates ALTER COLUMN updated_by SET DEFAULT 0;
ALTER TABLE IF EXISTS public.emr_templates ALTER COLUMN is_current SET DEFAULT false;
ALTER TABLE IF EXISTS public.emr_fields ALTER COLUMN id SET DEFAULT gen_random_uuid();
ALTER TABLE IF EXISTS public.emr_fields ALTER COLUMN tenant_id SET DEFAULT 0;
ALTER TABLE IF EXISTS public.emr_fields ALTER COLUMN structure_json SET DEFAULT '{}'::jsonb;
ALTER TABLE IF EXISTS public.emr_fields ALTER COLUMN options_json SET DEFAULT '{}'::jsonb;
ALTER TABLE IF EXISTS public.emr_fields ALTER COLUMN description SET DEFAULT ''::text;
ALTER TABLE IF EXISTS public.emr_fields ALTER COLUMN status SET DEFAULT 'enabled'::character varying;
ALTER TABLE IF EXISTS public.emr_fields ALTER COLUMN created_by SET DEFAULT 0;
ALTER TABLE IF EXISTS public.emr_fields ALTER COLUMN updated_by SET DEFAULT 0;
ALTER TABLE IF EXISTS public.emr_fields ALTER COLUMN created_at SET DEFAULT now();
ALTER TABLE IF EXISTS public.emr_fields ALTER COLUMN updated_at SET DEFAULT now();
ALTER TABLE IF EXISTS public.emr_template_fields ALTER COLUMN id SET DEFAULT gen_random_uuid();
ALTER TABLE IF EXISTS public.emr_template_fields ALTER COLUMN tenant_id SET DEFAULT 0;
ALTER TABLE IF EXISTS public.emr_template_fields ALTER COLUMN is_visible SET DEFAULT true;
ALTER TABLE IF EXISTS public.emr_template_fields ALTER COLUMN is_required SET DEFAULT false;
ALTER TABLE IF EXISTS public.emr_template_fields ALTER COLUMN description SET DEFAULT ''::text;
ALTER TABLE IF EXISTS public.emr_template_fields ALTER COLUMN created_by SET DEFAULT 0;
ALTER TABLE IF EXISTS public.emr_template_fields ALTER COLUMN created_at SET DEFAULT now();
ALTER TABLE IF EXISTS public.emr_template_fields ALTER COLUMN updated_at SET DEFAULT now();
ALTER TABLE IF EXISTS public.emr_records ALTER COLUMN id SET DEFAULT gen_random_uuid();
ALTER TABLE IF EXISTS public.emr_records ALTER COLUMN patient_snapshot_json SET DEFAULT '{}'::jsonb;
ALTER TABLE IF EXISTS public.emr_records ALTER COLUMN encounter_context_json SET DEFAULT '{}'::jsonb;
ALTER TABLE IF EXISTS public.emr_records ALTER COLUMN source_references_json SET DEFAULT '{}'::jsonb;
ALTER TABLE IF EXISTS public.emr_records ALTER COLUMN content_json SET DEFAULT '{}'::jsonb;
ALTER TABLE IF EXISTS public.emr_records ALTER COLUMN status SET DEFAULT '草稿'::character varying;
ALTER TABLE IF EXISTS public.emr_records ALTER COLUMN created_at SET DEFAULT now();
ALTER TABLE IF EXISTS public.emr_records ALTER COLUMN updated_at SET DEFAULT now();
ALTER TABLE IF EXISTS public.emr_records ALTER COLUMN revision_no SET DEFAULT 0;
ALTER TABLE IF EXISTS public.emr_records ALTER COLUMN current_score_status SET DEFAULT '未评分'::character varying;
ALTER TABLE IF EXISTS public.emr_records ALTER COLUMN current_score_version SET DEFAULT ''::character varying;
ALTER TABLE IF EXISTS public.emr_record_snapshots ALTER COLUMN id SET DEFAULT gen_random_uuid();
ALTER TABLE IF EXISTS public.emr_record_snapshots ALTER COLUMN patient_snapshot SET DEFAULT '{}'::jsonb;
ALTER TABLE IF EXISTS public.emr_record_snapshots ALTER COLUMN document_context SET DEFAULT '{}'::jsonb;
ALTER TABLE IF EXISTS public.emr_record_snapshots ALTER COLUMN content SET DEFAULT '{}'::jsonb;
ALTER TABLE IF EXISTS public.emr_record_snapshots ALTER COLUMN formed_at SET DEFAULT now();
ALTER TABLE IF EXISTS public.emr_record_snapshots ALTER COLUMN score_status SET DEFAULT '未评分'::character varying;
ALTER TABLE IF EXISTS public.emr_record_snapshots ALTER COLUMN score_version SET DEFAULT ''::character varying;
ALTER TABLE IF EXISTS public.emr_process_records ALTER COLUMN id SET DEFAULT gen_random_uuid();
ALTER TABLE IF EXISTS public.emr_process_records ALTER COLUMN occurred_at SET DEFAULT now();
ALTER TABLE IF EXISTS public.emr_process_records ALTER COLUMN output_info SET DEFAULT '{}'::jsonb;
ALTER TABLE IF EXISTS public.emr_process_records ALTER COLUMN failure_reason SET DEFAULT ''::text;
ALTER TABLE IF EXISTS public.emr_process_records ALTER COLUMN action_note SET DEFAULT ''::text;
ALTER TABLE IF EXISTS public.emr_process_records ALTER COLUMN content_changes SET DEFAULT '[]'::jsonb;
ALTER TABLE IF EXISTS public.emr_quality_rule_set_versions ALTER COLUMN status SET DEFAULT 'draft'::character varying;
ALTER TABLE IF EXISTS public.emr_quality_rule_set_versions ALTER COLUMN scoring_ready SET DEFAULT false;
ALTER TABLE IF EXISTS public.emr_quality_rule_set_versions ALTER COLUMN note SET DEFAULT ''::text;
ALTER TABLE IF EXISTS public.emr_quality_rule_set_versions ALTER COLUMN created_at SET DEFAULT now();
ALTER TABLE IF EXISTS public.emr_quality_rule_set_versions ALTER COLUMN updated_at SET DEFAULT now();
ALTER TABLE IF EXISTS public.emr_quality_rule_definitions ALTER COLUMN id SET DEFAULT gen_random_uuid();
ALTER TABLE IF EXISTS public.emr_quality_rule_definitions ALTER COLUMN source_text SET DEFAULT ''::text;
ALTER TABLE IF EXISTS public.emr_quality_rule_definitions ALTER COLUMN product_text SET DEFAULT ''::text;
ALTER TABLE IF EXISTS public.emr_quality_rule_definitions ALTER COLUMN standard_points SET DEFAULT 0;
ALTER TABLE IF EXISTS public.emr_quality_rule_definitions ALTER COLUMN input_spec SET DEFAULT '{}'::jsonb;
ALTER TABLE IF EXISTS public.emr_quality_rule_definitions ALTER COLUMN evaluation_spec SET DEFAULT '{}'::jsonb;
ALTER TABLE IF EXISTS public.emr_quality_rule_definitions ALTER COLUMN action_policy SET DEFAULT '{}'::jsonb;
ALTER TABLE IF EXISTS public.emr_quality_rule_definitions ALTER COLUMN message_templates SET DEFAULT '{}'::jsonb;
ALTER TABLE IF EXISTS public.emr_quality_rule_definitions ALTER COLUMN status SET DEFAULT 'draft'::character varying;
ALTER TABLE IF EXISTS public.emr_quality_rule_definitions ALTER COLUMN created_at SET DEFAULT now();
ALTER TABLE IF EXISTS public.emr_quality_rule_definitions ALTER COLUMN updated_at SET DEFAULT now();
ALTER TABLE IF EXISTS public.emr_quality_runs ALTER COLUMN id SET DEFAULT gen_random_uuid();
ALTER TABLE IF EXISTS public.emr_quality_runs ALTER COLUMN status SET DEFAULT 'running'::character varying;
ALTER TABLE IF EXISTS public.emr_quality_runs ALTER COLUMN overall_result SET DEFAULT 'no_problem'::character varying;
ALTER TABLE IF EXISTS public.emr_quality_runs ALTER COLUMN total_score SET DEFAULT 0;
ALTER TABLE IF EXISTS public.emr_quality_runs ALTER COLUMN grade SET DEFAULT ''::character varying;
ALTER TABLE IF EXISTS public.emr_quality_runs ALTER COLUMN major_score SET DEFAULT 0;
ALTER TABLE IF EXISTS public.emr_quality_runs ALTER COLUMN blocking_count SET DEFAULT 0;
ALTER TABLE IF EXISTS public.emr_quality_runs ALTER COLUMN pending_count SET DEFAULT 0;
ALTER TABLE IF EXISTS public.emr_quality_runs ALTER COLUMN started_at SET DEFAULT now();
ALTER TABLE IF EXISTS public.emr_quality_runs ALTER COLUMN failure_reason SET DEFAULT ''::text;
ALTER TABLE IF EXISTS public.emr_quality_runs ALTER COLUMN created_at SET DEFAULT now();
ALTER TABLE IF EXISTS public.emr_quality_runs ALTER COLUMN model_audit SET DEFAULT '{}'::jsonb;
ALTER TABLE IF EXISTS public.emr_quality_runs ALTER COLUMN input_digest SET DEFAULT ''::character varying;
ALTER TABLE IF EXISTS public.emr_quality_results ALTER COLUMN id SET DEFAULT gen_random_uuid();
ALTER TABLE IF EXISTS public.emr_quality_results ALTER COLUMN evidence SET DEFAULT '[]'::jsonb;
ALTER TABLE IF EXISTS public.emr_quality_results ALTER COLUMN reason SET DEFAULT ''::text;
ALTER TABLE IF EXISTS public.emr_quality_results ALTER COLUMN deduction SET DEFAULT 0;
ALTER TABLE IF EXISTS public.emr_quality_results ALTER COLUMN message SET DEFAULT ''::text;
ALTER TABLE IF EXISTS public.emr_quality_results ALTER COLUMN blocking SET DEFAULT false;
ALTER TABLE IF EXISTS public.emr_quality_results ALTER COLUMN manual_review SET DEFAULT false;
ALTER TABLE IF EXISTS public.emr_quality_results ALTER COLUMN created_at SET DEFAULT now();
ALTER TABLE IF EXISTS public.emr_record_score_current ALTER COLUMN status SET DEFAULT '已完成'::character varying;
ALTER TABLE IF EXISTS public.emr_record_score_current ALTER COLUMN program_score SET DEFAULT 0;
ALTER TABLE IF EXISTS public.emr_record_score_current ALTER COLUMN program_deduct_points SET DEFAULT 0;
ALTER TABLE IF EXISTS public.emr_record_score_current ALTER COLUMN major_program_score SET DEFAULT 0;
ALTER TABLE IF EXISTS public.emr_record_score_current ALTER COLUMN direct_grade SET DEFAULT ''::character varying;
ALTER TABLE IF EXISTS public.emr_record_score_current ALTER COLUMN direct_grade_reason SET DEFAULT ''::text;
ALTER TABLE IF EXISTS public.emr_record_score_current ALTER COLUMN pending_sections SET DEFAULT '[]'::jsonb;
ALTER TABLE IF EXISTS public.emr_record_score_current ALTER COLUMN failure_reason SET DEFAULT ''::text;
ALTER TABLE IF EXISTS public.emr_record_score_current ALTER COLUMN scored_at SET DEFAULT now();
ALTER TABLE IF EXISTS public.emr_record_score_current ALTER COLUMN updated_at SET DEFAULT now();
ALTER TABLE IF EXISTS public.emr_record_score_current_items ALTER COLUMN id SET DEFAULT gen_random_uuid();
ALTER TABLE IF EXISTS public.emr_record_score_current_items ALTER COLUMN program_deduct_points SET DEFAULT 0;
ALTER TABLE IF EXISTS public.emr_record_score_current_items ALTER COLUMN pending SET DEFAULT false;
ALTER TABLE IF EXISTS public.emr_record_score_current_items ALTER COLUMN findings SET DEFAULT '[]'::jsonb;
ALTER TABLE IF EXISTS public.emr_record_score_current_items ALTER COLUMN sort_order SET DEFAULT 0;
ALTER TABLE IF EXISTS public.emr_record_score_current_items ALTER COLUMN updated_at SET DEFAULT now();
ALTER TABLE IF EXISTS public.emr_score_standards ALTER COLUMN id SET DEFAULT gen_random_uuid();
ALTER TABLE IF EXISTS public.emr_score_standards ALTER COLUMN source_name SET DEFAULT ''::character varying;
ALTER TABLE IF EXISTS public.emr_score_standards ALTER COLUMN source_reference SET DEFAULT ''::text;
ALTER TABLE IF EXISTS public.emr_score_standards ALTER COLUMN status SET DEFAULT 'published'::character varying;
ALTER TABLE IF EXISTS public.emr_score_standards ALTER COLUMN created_at SET DEFAULT now();
ALTER TABLE IF EXISTS public.emr_score_standards ALTER COLUMN updated_at SET DEFAULT now();
ALTER TABLE IF EXISTS public.emr_icd10_diagnosis_catalog ALTER COLUMN catalog_version SET DEFAULT ''::text;
ALTER TABLE IF EXISTS public.emr_icd10_diagnosis_catalog ALTER COLUMN chapter_no SET DEFAULT ''::text;
ALTER TABLE IF EXISTS public.emr_icd10_diagnosis_catalog ALTER COLUMN chapter_code_range SET DEFAULT ''::text;
ALTER TABLE IF EXISTS public.emr_icd10_diagnosis_catalog ALTER COLUMN chapter_name SET DEFAULT ''::text;
ALTER TABLE IF EXISTS public.emr_icd10_diagnosis_catalog ALTER COLUMN section_code_range SET DEFAULT ''::text;
ALTER TABLE IF EXISTS public.emr_icd10_diagnosis_catalog ALTER COLUMN section_name SET DEFAULT ''::text;
ALTER TABLE IF EXISTS public.emr_icd10_diagnosis_catalog ALTER COLUMN category_code SET DEFAULT ''::text;
ALTER TABLE IF EXISTS public.emr_icd10_diagnosis_catalog ALTER COLUMN category_name SET DEFAULT ''::text;
ALTER TABLE IF EXISTS public.emr_icd10_diagnosis_catalog ALTER COLUMN subcategory_code SET DEFAULT ''::text;
ALTER TABLE IF EXISTS public.emr_icd10_diagnosis_catalog ALTER COLUMN subcategory_name SET DEFAULT ''::text;
ALTER TABLE IF EXISTS public.emr_icd10_diagnosis_catalog ALTER COLUMN source_file SET DEFAULT ''::text;
ALTER TABLE IF EXISTS public.emr_icd10_diagnosis_catalog ALTER COLUMN is_active SET DEFAULT true;
ALTER TABLE IF EXISTS public.emr_icd10_diagnosis_catalog ALTER COLUMN imported_at SET DEFAULT now();
ALTER TABLE IF EXISTS public.emr_icd10_diagnosis_catalog ALTER COLUMN created_at SET DEFAULT now();
ALTER TABLE IF EXISTS public.emr_permission_assignments ALTER COLUMN enabled SET DEFAULT false;
ALTER TABLE IF EXISTS public.emr_permission_assignments ALTER COLUMN emr_role_code SET DEFAULT 'readonly'::character varying;
ALTER TABLE IF EXISTS public.emr_permission_assignments ALTER COLUMN scope SET DEFAULT 'self'::character varying;
ALTER TABLE IF EXISTS public.emr_permission_assignments ALTER COLUMN abilities_json SET DEFAULT '[]'::jsonb;
ALTER TABLE IF EXISTS public.emr_permission_assignments ALTER COLUMN created_at SET DEFAULT now();
ALTER TABLE IF EXISTS public.emr_permission_assignments ALTER COLUMN updated_at SET DEFAULT now();
ALTER TABLE IF EXISTS public.emr_badge_sessions ALTER COLUMN id SET DEFAULT gen_random_uuid();
ALTER TABLE IF EXISTS public.emr_badge_sessions ALTER COLUMN started_at SET DEFAULT now();
ALTER TABLE IF EXISTS public.emr_badge_sessions ALTER COLUMN created_at SET DEFAULT now();
ALTER TABLE IF EXISTS public.emr_badge_sessions ALTER COLUMN updated_at SET DEFAULT now();
ALTER TABLE IF EXISTS public.emr_badge_sessions ALTER COLUMN state SET DEFAULT 'recording'::character varying;
ALTER TABLE IF EXISTS public.emr_followup_deliveries ALTER COLUMN status SET DEFAULT 'pending'::text;
ALTER TABLE IF EXISTS public.emr_followup_deliveries ALTER COLUMN attempt_count SET DEFAULT 0;
ALTER TABLE IF EXISTS public.emr_followup_deliveries ALTER COLUMN next_attempt_at SET DEFAULT now();
ALTER TABLE IF EXISTS public.emr_followup_deliveries ALTER COLUMN created_at SET DEFAULT now();
ALTER TABLE IF EXISTS public.emr_followup_deliveries ALTER COLUMN updated_at SET DEFAULT now();
ALTER TABLE IF EXISTS public.followup_generation_runs ALTER COLUMN status SET DEFAULT 'pending'::character varying;
ALTER TABLE IF EXISTS public.followup_generation_runs ALTER COLUMN attempt_count SET DEFAULT 0;
ALTER TABLE IF EXISTS public.followup_generation_runs ALTER COLUMN max_attempts SET DEFAULT 3;
ALTER TABLE IF EXISTS public.followup_generation_runs ALTER COLUMN persisted_task_count SET DEFAULT 0;
ALTER TABLE IF EXISTS public.followup_generation_runs ALTER COLUMN created_at SET DEFAULT now();
ALTER TABLE IF EXISTS public.followup_generation_runs ALTER COLUMN updated_at SET DEFAULT now();
ALTER TABLE IF EXISTS public.followup_data_scopes ALTER COLUMN created_at SET DEFAULT now();
ALTER TABLE IF EXISTS public.followup_data_scopes ALTER COLUMN updated_at SET DEFAULT now();
ALTER TABLE IF EXISTS public.followup_permission_assignments ALTER COLUMN enabled SET DEFAULT false;
ALTER TABLE IF EXISTS public.followup_permission_assignments ALTER COLUMN role_code SET DEFAULT 'followup_staff'::character varying;
ALTER TABLE IF EXISTS public.followup_permission_assignments ALTER COLUMN abilities_json SET DEFAULT '[]'::jsonb;
ALTER TABLE IF EXISTS public.followup_permission_assignments ALTER COLUMN created_at SET DEFAULT now();
ALTER TABLE IF EXISTS public.followup_permission_assignments ALTER COLUMN updated_at SET DEFAULT now();
ALTER TABLE IF EXISTS public.compliance_rules ALTER COLUMN id SET DEFAULT gen_random_uuid();
ALTER TABLE IF EXISTS public.compliance_rules ALTER COLUMN rule_name SET DEFAULT ''::character varying;
ALTER TABLE IF EXISTS public.compliance_rules ALTER COLUMN scene_codes SET DEFAULT '[]'::jsonb;
ALTER TABLE IF EXISTS public.compliance_rules ALTER COLUMN role_codes SET DEFAULT '[]'::jsonb;
ALTER TABLE IF EXISTS public.compliance_rules ALTER COLUMN status SET DEFAULT 'draft'::character varying;
ALTER TABLE IF EXISTS public.compliance_rules ALTER COLUMN is_enabled SET DEFAULT true;
ALTER TABLE IF EXISTS public.compliance_rules ALTER COLUMN judgment_standard SET DEFAULT ''::text;
ALTER TABLE IF EXISTS public.compliance_rules ALTER COLUMN definition SET DEFAULT '{}'::jsonb;
ALTER TABLE IF EXISTS public.compliance_rules ALTER COLUMN created_at SET DEFAULT now();
ALTER TABLE IF EXISTS public.compliance_rules ALTER COLUMN updated_at SET DEFAULT now();
ALTER TABLE IF EXISTS public.compliance_check_runs ALTER COLUMN id SET DEFAULT gen_random_uuid();
ALTER TABLE IF EXISTS public.compliance_check_runs ALTER COLUMN source_fingerprint SET DEFAULT ''::character varying;
ALTER TABLE IF EXISTS public.compliance_check_runs ALTER COLUMN role_code SET DEFAULT ''::character varying;
ALTER TABLE IF EXISTS public.compliance_check_runs ALTER COLUMN prompt_code SET DEFAULT ''::character varying;
ALTER TABLE IF EXISTS public.compliance_check_runs ALTER COLUMN prompt_version SET DEFAULT ''::character varying;
ALTER TABLE IF EXISTS public.compliance_check_runs ALTER COLUMN rule_version SET DEFAULT ''::character varying;
ALTER TABLE IF EXISTS public.compliance_check_runs ALTER COLUMN model_code SET DEFAULT ''::character varying;
ALTER TABLE IF EXISTS public.compliance_check_runs ALTER COLUMN status SET DEFAULT 'processing'::character varying;
ALTER TABLE IF EXISTS public.compliance_check_runs ALTER COLUMN finding_count SET DEFAULT 0;
ALTER TABLE IF EXISTS public.compliance_check_runs ALTER COLUMN finding_rule_codes SET DEFAULT '[]'::jsonb;
ALTER TABLE IF EXISTS public.compliance_check_runs ALTER COLUMN raw_response SET DEFAULT '{}'::jsonb;
ALTER TABLE IF EXISTS public.compliance_check_runs ALTER COLUMN result_json SET DEFAULT '{"findings": []}'::jsonb;
ALTER TABLE IF EXISTS public.compliance_check_runs ALTER COLUMN error_message SET DEFAULT ''::text;
ALTER TABLE IF EXISTS public.compliance_check_runs ALTER COLUMN created_at SET DEFAULT now();
ALTER TABLE IF EXISTS public.compliance_check_runs ALTER COLUMN updated_at SET DEFAULT now();
ALTER TABLE IF EXISTS public.compliance_findings ALTER COLUMN id SET DEFAULT gen_random_uuid();
ALTER TABLE IF EXISTS public.compliance_findings ALTER COLUMN role_code SET DEFAULT ''::character varying;
ALTER TABLE IF EXISTS public.compliance_findings ALTER COLUMN rule_version SET DEFAULT ''::character varying;
ALTER TABLE IF EXISTS public.compliance_findings ALTER COLUMN fact SET DEFAULT ''::text;
ALTER TABLE IF EXISTS public.compliance_findings ALTER COLUMN summary SET DEFAULT ''::text;
ALTER TABLE IF EXISTS public.compliance_findings ALTER COLUMN reason SET DEFAULT ''::text;
ALTER TABLE IF EXISTS public.compliance_findings ALTER COLUMN evidence SET DEFAULT '[]'::jsonb;
ALTER TABLE IF EXISTS public.compliance_findings ALTER COLUMN missing_facts SET DEFAULT '[]'::jsonb;
ALTER TABLE IF EXISTS public.compliance_findings ALTER COLUMN needs_review SET DEFAULT true;
ALTER TABLE IF EXISTS public.compliance_findings ALTER COLUMN status SET DEFAULT 'pending'::character varying;
ALTER TABLE IF EXISTS public.compliance_findings ALTER COLUMN review_conclusion SET DEFAULT ''::character varying;
ALTER TABLE IF EXISTS public.compliance_findings ALTER COLUMN review_reason SET DEFAULT ''::text;
ALTER TABLE IF EXISTS public.compliance_findings ALTER COLUMN created_at SET DEFAULT now();
ALTER TABLE IF EXISTS public.compliance_findings ALTER COLUMN updated_at SET DEFAULT now();
ALTER TABLE IF EXISTS public.compliance_permission_assignments ALTER COLUMN enabled SET DEFAULT false;
ALTER TABLE IF EXISTS public.compliance_permission_assignments ALTER COLUMN role_code SET DEFAULT 'compliance_staff'::character varying;
ALTER TABLE IF EXISTS public.compliance_permission_assignments ALTER COLUMN abilities_json SET DEFAULT '[]'::jsonb;
ALTER TABLE IF EXISTS public.compliance_permission_assignments ALTER COLUMN created_at SET DEFAULT now();
ALTER TABLE IF EXISTS public.compliance_permission_assignments ALTER COLUMN updated_at SET DEFAULT now();

DO $$ BEGIN IF to_regclass('public.compliance_check_runs') IS NOT NULL AND NOT EXISTS (SELECT 1 FROM pg_constraint WHERE conrelid='public.compliance_check_runs'::regclass AND conname='compliance_check_runs_execution_key_uq') THEN ALTER TABLE public.compliance_check_runs ADD CONSTRAINT compliance_check_runs_execution_key_uq UNIQUE (execution_key); END IF; END $$;
DO $$ BEGIN IF to_regclass('public.compliance_findings') IS NOT NULL AND NOT EXISTS (SELECT 1 FROM pg_constraint WHERE conrelid='public.compliance_findings'::regclass AND conname='compliance_findings_pkey') THEN ALTER TABLE public.compliance_findings ADD CONSTRAINT compliance_findings_pkey PRIMARY KEY (id); END IF; END $$;
DO $$ BEGIN IF to_regclass('public.compliance_findings') IS NOT NULL AND NOT EXISTS (SELECT 1 FROM pg_constraint WHERE conrelid='public.compliance_findings'::regclass AND conname='compliance_findings_run_rule_uq') THEN ALTER TABLE public.compliance_findings ADD CONSTRAINT compliance_findings_run_rule_uq UNIQUE (run_id, rule_code); END IF; END $$;
DO $$ BEGIN IF to_regclass('public.compliance_check_runs') IS NOT NULL AND NOT EXISTS (SELECT 1 FROM pg_constraint WHERE conrelid='public.compliance_check_runs'::regclass AND conname='compliance_mvp_check_results_pkey') THEN ALTER TABLE public.compliance_check_runs ADD CONSTRAINT compliance_mvp_check_results_pkey PRIMARY KEY (id); END IF; END $$;
DO $$ BEGIN IF to_regclass('public.compliance_rules') IS NOT NULL AND NOT EXISTS (SELECT 1 FROM pg_constraint WHERE conrelid='public.compliance_rules'::regclass AND conname='compliance_mvp_rules_pkey') THEN ALTER TABLE public.compliance_rules ADD CONSTRAINT compliance_mvp_rules_pkey PRIMARY KEY (id); END IF; END $$;
DO $$ BEGIN IF to_regclass('public.compliance_permission_assignments') IS NOT NULL AND NOT EXISTS (SELECT 1 FROM pg_constraint WHERE conrelid='public.compliance_permission_assignments'::regclass AND conname='compliance_permission_assignments_pkey') THEN ALTER TABLE public.compliance_permission_assignments ADD CONSTRAINT compliance_permission_assignments_pkey PRIMARY KEY (id); END IF; END $$;
DO $$ BEGIN IF to_regclass('public.compliance_rules') IS NOT NULL AND NOT EXISTS (SELECT 1 FROM pg_constraint WHERE conrelid='public.compliance_rules'::regclass AND conname='compliance_rules_identity_uq') THEN ALTER TABLE public.compliance_rules ADD CONSTRAINT compliance_rules_identity_uq UNIQUE (rule_code, version); END IF; END $$;
DO $$ BEGIN IF to_regclass('public.emr_badge_sessions') IS NOT NULL AND NOT EXISTS (SELECT 1 FROM pg_constraint WHERE conrelid='public.emr_badge_sessions'::regclass AND conname='emr_badge_sessions_pkey') THEN ALTER TABLE public.emr_badge_sessions ADD CONSTRAINT emr_badge_sessions_pkey PRIMARY KEY (id); END IF; END $$;
DO $$ BEGIN IF to_regclass('public.emr_fields') IS NOT NULL AND NOT EXISTS (SELECT 1 FROM pg_constraint WHERE conrelid='public.emr_fields'::regclass AND conname='emr_fields_pkey') THEN ALTER TABLE public.emr_fields ADD CONSTRAINT emr_fields_pkey PRIMARY KEY (id); END IF; END $$;
DO $$ BEGIN IF to_regclass('public.emr_followup_deliveries') IS NOT NULL AND NOT EXISTS (SELECT 1 FROM pg_constraint WHERE conrelid='public.emr_followup_deliveries'::regclass AND conname='emr_followup_deliveries_pkey') THEN ALTER TABLE public.emr_followup_deliveries ADD CONSTRAINT emr_followup_deliveries_pkey PRIMARY KEY (tenant_id, recording_id); END IF; END $$;
DO $$ BEGIN IF to_regclass('public.emr_icd10_diagnosis_catalog') IS NOT NULL AND NOT EXISTS (SELECT 1 FROM pg_constraint WHERE conrelid='public.emr_icd10_diagnosis_catalog'::regclass AND conname='emr_icd10_diagnosis_catalog_pkey') THEN ALTER TABLE public.emr_icd10_diagnosis_catalog ADD CONSTRAINT emr_icd10_diagnosis_catalog_pkey PRIMARY KEY (id); END IF; END $$;
DO $$ BEGIN IF to_regclass('public.emr_permission_assignments') IS NOT NULL AND NOT EXISTS (SELECT 1 FROM pg_constraint WHERE conrelid='public.emr_permission_assignments'::regclass AND conname='emr_permission_assignments_pkey') THEN ALTER TABLE public.emr_permission_assignments ADD CONSTRAINT emr_permission_assignments_pkey PRIMARY KEY (id); END IF; END $$;
DO $$ BEGIN IF to_regclass('public.emr_process_records') IS NOT NULL AND NOT EXISTS (SELECT 1 FROM pg_constraint WHERE conrelid='public.emr_process_records'::regclass AND conname='emr_process_records_pkey') THEN ALTER TABLE public.emr_process_records ADD CONSTRAINT emr_process_records_pkey PRIMARY KEY (id); END IF; END $$;
DO $$ BEGIN IF to_regclass('public.emr_quality_results') IS NOT NULL AND NOT EXISTS (SELECT 1 FROM pg_constraint WHERE conrelid='public.emr_quality_results'::regclass AND conname='emr_quality_results_pkey') THEN ALTER TABLE public.emr_quality_results ADD CONSTRAINT emr_quality_results_pkey PRIMARY KEY (id); END IF; END $$;
DO $$ BEGIN IF to_regclass('public.emr_quality_rule_definitions') IS NOT NULL AND NOT EXISTS (SELECT 1 FROM pg_constraint WHERE conrelid='public.emr_quality_rule_definitions'::regclass AND conname='emr_quality_rule_definitions_pkey') THEN ALTER TABLE public.emr_quality_rule_definitions ADD CONSTRAINT emr_quality_rule_definitions_pkey PRIMARY KEY (id); END IF; END $$;
DO $$ BEGIN IF to_regclass('public.emr_quality_rule_set_versions') IS NOT NULL AND NOT EXISTS (SELECT 1 FROM pg_constraint WHERE conrelid='public.emr_quality_rule_set_versions'::regclass AND conname='emr_quality_rule_set_versions_pkey') THEN ALTER TABLE public.emr_quality_rule_set_versions ADD CONSTRAINT emr_quality_rule_set_versions_pkey PRIMARY KEY (version); END IF; END $$;
DO $$ BEGIN IF to_regclass('public.emr_quality_runs') IS NOT NULL AND NOT EXISTS (SELECT 1 FROM pg_constraint WHERE conrelid='public.emr_quality_runs'::regclass AND conname='emr_quality_runs_pkey') THEN ALTER TABLE public.emr_quality_runs ADD CONSTRAINT emr_quality_runs_pkey PRIMARY KEY (id); END IF; END $$;
DO $$ BEGIN IF to_regclass('public.emr_record_score_current_items') IS NOT NULL AND NOT EXISTS (SELECT 1 FROM pg_constraint WHERE conrelid='public.emr_record_score_current_items'::regclass AND conname='emr_record_score_current_items_pkey') THEN ALTER TABLE public.emr_record_score_current_items ADD CONSTRAINT emr_record_score_current_items_pkey PRIMARY KEY (id); END IF; END $$;
DO $$ BEGIN IF to_regclass('public.emr_record_score_current') IS NOT NULL AND NOT EXISTS (SELECT 1 FROM pg_constraint WHERE conrelid='public.emr_record_score_current'::regclass AND conname='emr_record_score_current_pkey') THEN ALTER TABLE public.emr_record_score_current ADD CONSTRAINT emr_record_score_current_pkey PRIMARY KEY (record_id); END IF; END $$;
DO $$ BEGIN IF to_regclass('public.emr_record_snapshots') IS NOT NULL AND NOT EXISTS (SELECT 1 FROM pg_constraint WHERE conrelid='public.emr_record_snapshots'::regclass AND conname='emr_record_snapshots_id_record_template_uq') THEN ALTER TABLE public.emr_record_snapshots ADD CONSTRAINT emr_record_snapshots_id_record_template_uq UNIQUE (id, record_id, template_version_id); END IF; END $$;
DO $$ BEGIN IF to_regclass('public.emr_record_snapshots') IS NOT NULL AND NOT EXISTS (SELECT 1 FROM pg_constraint WHERE conrelid='public.emr_record_snapshots'::regclass AND conname='emr_record_snapshots_id_record_uq') THEN ALTER TABLE public.emr_record_snapshots ADD CONSTRAINT emr_record_snapshots_id_record_uq UNIQUE (id, record_id); END IF; END $$;
DO $$ BEGIN IF to_regclass('public.emr_record_snapshots') IS NOT NULL AND NOT EXISTS (SELECT 1 FROM pg_constraint WHERE conrelid='public.emr_record_snapshots'::regclass AND conname='emr_record_snapshots_pkey') THEN ALTER TABLE public.emr_record_snapshots ADD CONSTRAINT emr_record_snapshots_pkey PRIMARY KEY (id); END IF; END $$;
DO $$ BEGIN IF to_regclass('public.emr_records') IS NOT NULL AND NOT EXISTS (SELECT 1 FROM pg_constraint WHERE conrelid='public.emr_records'::regclass AND conname='emr_records_pkey') THEN ALTER TABLE public.emr_records ADD CONSTRAINT emr_records_pkey PRIMARY KEY (id); END IF; END $$;
DO $$ BEGIN IF to_regclass('public.emr_records') IS NOT NULL AND NOT EXISTS (SELECT 1 FROM pg_constraint WHERE conrelid='public.emr_records'::regclass AND conname='emr_records_tenant_encounter_uq') THEN ALTER TABLE public.emr_records ADD CONSTRAINT emr_records_tenant_encounter_uq UNIQUE (tenant_id, encounter_id); END IF; END $$;
DO $$ BEGIN IF to_regclass('public.emr_score_standards') IS NOT NULL AND NOT EXISTS (SELECT 1 FROM pg_constraint WHERE conrelid='public.emr_score_standards'::regclass AND conname='emr_score_standards_pkey') THEN ALTER TABLE public.emr_score_standards ADD CONSTRAINT emr_score_standards_pkey PRIMARY KEY (id); END IF; END $$;
DO $$ BEGIN IF to_regclass('public.emr_template_fields') IS NOT NULL AND NOT EXISTS (SELECT 1 FROM pg_constraint WHERE conrelid='public.emr_template_fields'::regclass AND conname='emr_template_fields_pkey') THEN ALTER TABLE public.emr_template_fields ADD CONSTRAINT emr_template_fields_pkey PRIMARY KEY (id); END IF; END $$;
DO $$ BEGIN IF to_regclass('public.emr_templates') IS NOT NULL AND NOT EXISTS (SELECT 1 FROM pg_constraint WHERE conrelid='public.emr_templates'::regclass AND conname='emr_templates_pkey') THEN ALTER TABLE public.emr_templates ADD CONSTRAINT emr_templates_pkey PRIMARY KEY (id); END IF; END $$;
DO $$ BEGIN IF to_regclass('public.encounters') IS NOT NULL AND NOT EXISTS (SELECT 1 FROM pg_constraint WHERE conrelid='public.encounters'::regclass AND conname='encounters_pkey') THEN ALTER TABLE public.encounters ADD CONSTRAINT encounters_pkey PRIMARY KEY (id); END IF; END $$;
DO $$ BEGIN IF to_regclass('public.followup_data_scopes') IS NOT NULL AND NOT EXISTS (SELECT 1 FROM pg_constraint WHERE conrelid='public.followup_data_scopes'::regclass AND conname='followup_data_scopes_pkey') THEN ALTER TABLE public.followup_data_scopes ADD CONSTRAINT followup_data_scopes_pkey PRIMARY KEY (id); END IF; END $$;
DO $$ BEGIN IF to_regclass('public.followup_generation_runs') IS NOT NULL AND NOT EXISTS (SELECT 1 FROM pg_constraint WHERE conrelid='public.followup_generation_runs'::regclass AND conname='followup_generation_runs_pkey') THEN ALTER TABLE public.followup_generation_runs ADD CONSTRAINT followup_generation_runs_pkey PRIMARY KEY (id); END IF; END $$;
DO $$ BEGIN IF to_regclass('public.followup_permission_assignments') IS NOT NULL AND NOT EXISTS (SELECT 1 FROM pg_constraint WHERE conrelid='public.followup_permission_assignments'::regclass AND conname='followup_permission_assignments_pkey') THEN ALTER TABLE public.followup_permission_assignments ADD CONSTRAINT followup_permission_assignments_pkey PRIMARY KEY (id); END IF; END $$;
DO $$ BEGIN IF to_regclass('public.compliance_permission_assignments') IS NOT NULL AND NOT EXISTS (SELECT 1 FROM pg_constraint WHERE conrelid='public.compliance_permission_assignments'::regclass AND conname='uq_compliance_permission_tenant_employee') THEN ALTER TABLE public.compliance_permission_assignments ADD CONSTRAINT uq_compliance_permission_tenant_employee UNIQUE (tenant_id, employee_id); END IF; END $$;
DO $$ BEGIN IF to_regclass('public.emr_permission_assignments') IS NOT NULL AND NOT EXISTS (SELECT 1 FROM pg_constraint WHERE conrelid='public.emr_permission_assignments'::regclass AND conname='uq_emr_permission_assignments_tenant_employee') THEN ALTER TABLE public.emr_permission_assignments ADD CONSTRAINT uq_emr_permission_assignments_tenant_employee UNIQUE (tenant_id, employee_id); END IF; END $$;
DO $$ BEGIN IF to_regclass('public.encounters') IS NOT NULL AND NOT EXISTS (SELECT 1 FROM pg_constraint WHERE conrelid='public.encounters'::regclass AND conname='uq_encounters_tenant_source') THEN ALTER TABLE public.encounters ADD CONSTRAINT uq_encounters_tenant_source UNIQUE (tenant_id, source_type, source_id); END IF; END $$;
DO $$ BEGIN IF to_regclass('public.followup_data_scopes') IS NOT NULL AND NOT EXISTS (SELECT 1 FROM pg_constraint WHERE conrelid='public.followup_data_scopes'::regclass AND conname='uq_followup_data_scopes_relation') THEN ALTER TABLE public.followup_data_scopes ADD CONSTRAINT uq_followup_data_scopes_relation UNIQUE (tenant_id, viewer_employee_id, target_employee_id); END IF; END $$;
DO $$ BEGIN IF to_regclass('public.followup_permission_assignments') IS NOT NULL AND NOT EXISTS (SELECT 1 FROM pg_constraint WHERE conrelid='public.followup_permission_assignments'::regclass AND conname='uq_followup_permission_tenant_employee') THEN ALTER TABLE public.followup_permission_assignments ADD CONSTRAINT uq_followup_permission_tenant_employee UNIQUE (tenant_id, employee_id); END IF; END $$;
DO $$ BEGIN IF to_regclass('public.compliance_findings') IS NOT NULL AND NOT EXISTS (SELECT 1 FROM pg_constraint WHERE conrelid='public.compliance_findings'::regclass AND conname='compliance_findings_run_id_fkey') THEN ALTER TABLE public.compliance_findings ADD CONSTRAINT compliance_findings_run_id_fkey FOREIGN KEY (run_id) REFERENCES public.compliance_check_runs(id) ON DELETE CASCADE; END IF; END $$;
DO $$ BEGIN IF to_regclass('public.emr_process_records') IS NOT NULL AND NOT EXISTS (SELECT 1 FROM pg_constraint WHERE conrelid='public.emr_process_records'::regclass AND conname='emr_process_action_snapshot_record_fk') THEN ALTER TABLE public.emr_process_records ADD CONSTRAINT emr_process_action_snapshot_record_fk FOREIGN KEY (action_snapshot_id, record_id) REFERENCES public.emr_record_snapshots(id, record_id); END IF; END $$;
DO $$ BEGIN IF to_regclass('public.emr_process_records') IS NOT NULL AND NOT EXISTS (SELECT 1 FROM pg_constraint WHERE conrelid='public.emr_process_records'::regclass AND conname='emr_process_after_snapshot_record_fk') THEN ALTER TABLE public.emr_process_records ADD CONSTRAINT emr_process_after_snapshot_record_fk FOREIGN KEY (after_snapshot_id, record_id) REFERENCES public.emr_record_snapshots(id, record_id); END IF; END $$;
DO $$ BEGIN IF to_regclass('public.emr_process_records') IS NOT NULL AND NOT EXISTS (SELECT 1 FROM pg_constraint WHERE conrelid='public.emr_process_records'::regclass AND conname='emr_process_before_snapshot_record_fk') THEN ALTER TABLE public.emr_process_records ADD CONSTRAINT emr_process_before_snapshot_record_fk FOREIGN KEY (before_snapshot_id, record_id) REFERENCES public.emr_record_snapshots(id, record_id); END IF; END $$;
DO $$ BEGIN IF to_regclass('public.emr_process_records') IS NOT NULL AND NOT EXISTS (SELECT 1 FROM pg_constraint WHERE conrelid='public.emr_process_records'::regclass AND conname='emr_process_records_action_snapshot_id_fkey') THEN ALTER TABLE public.emr_process_records ADD CONSTRAINT emr_process_records_action_snapshot_id_fkey FOREIGN KEY (action_snapshot_id) REFERENCES public.emr_record_snapshots(id); END IF; END $$;
DO $$ BEGIN IF to_regclass('public.emr_process_records') IS NOT NULL AND NOT EXISTS (SELECT 1 FROM pg_constraint WHERE conrelid='public.emr_process_records'::regclass AND conname='emr_process_records_after_snapshot_id_fkey') THEN ALTER TABLE public.emr_process_records ADD CONSTRAINT emr_process_records_after_snapshot_id_fkey FOREIGN KEY (after_snapshot_id) REFERENCES public.emr_record_snapshots(id); END IF; END $$;
DO $$ BEGIN IF to_regclass('public.emr_process_records') IS NOT NULL AND NOT EXISTS (SELECT 1 FROM pg_constraint WHERE conrelid='public.emr_process_records'::regclass AND conname='emr_process_records_before_snapshot_id_fkey') THEN ALTER TABLE public.emr_process_records ADD CONSTRAINT emr_process_records_before_snapshot_id_fkey FOREIGN KEY (before_snapshot_id) REFERENCES public.emr_record_snapshots(id); END IF; END $$;
DO $$ BEGIN IF to_regclass('public.emr_quality_results') IS NOT NULL AND NOT EXISTS (SELECT 1 FROM pg_constraint WHERE conrelid='public.emr_quality_results'::regclass AND conname='emr_quality_results_rule_id_fkey') THEN ALTER TABLE public.emr_quality_results ADD CONSTRAINT emr_quality_results_rule_id_fkey FOREIGN KEY (rule_id) REFERENCES public.emr_quality_rule_definitions(id) ON DELETE RESTRICT; END IF; END $$;
DO $$ BEGIN IF to_regclass('public.emr_quality_results') IS NOT NULL AND NOT EXISTS (SELECT 1 FROM pg_constraint WHERE conrelid='public.emr_quality_results'::regclass AND conname='emr_quality_results_run_id_fkey') THEN ALTER TABLE public.emr_quality_results ADD CONSTRAINT emr_quality_results_run_id_fkey FOREIGN KEY (run_id) REFERENCES public.emr_quality_runs(id) ON DELETE CASCADE; END IF; END $$;
DO $$ BEGIN IF to_regclass('public.emr_quality_runs') IS NOT NULL AND NOT EXISTS (SELECT 1 FROM pg_constraint WHERE conrelid='public.emr_quality_runs'::regclass AND conname='emr_quality_runs_record_id_fkey') THEN ALTER TABLE public.emr_quality_runs ADD CONSTRAINT emr_quality_runs_record_id_fkey FOREIGN KEY (record_id) REFERENCES public.emr_records(id) ON DELETE CASCADE; END IF; END $$;
DO $$ BEGIN IF to_regclass('public.emr_quality_runs') IS NOT NULL AND NOT EXISTS (SELECT 1 FROM pg_constraint WHERE conrelid='public.emr_quality_runs'::regclass AND conname='emr_quality_runs_snapshot_id_fkey') THEN ALTER TABLE public.emr_quality_runs ADD CONSTRAINT emr_quality_runs_snapshot_id_fkey FOREIGN KEY (snapshot_id) REFERENCES public.emr_record_snapshots(id) ON DELETE SET NULL; END IF; END $$;
DO $$ BEGIN IF to_regclass('public.emr_record_score_current_items') IS NOT NULL AND NOT EXISTS (SELECT 1 FROM pg_constraint WHERE conrelid='public.emr_record_score_current_items'::regclass AND conname='emr_record_score_current_items_record_id_fkey') THEN ALTER TABLE public.emr_record_score_current_items ADD CONSTRAINT emr_record_score_current_items_record_id_fkey FOREIGN KEY (record_id) REFERENCES public.emr_record_score_current(record_id) ON DELETE CASCADE; END IF; END $$;
DO $$ BEGIN IF to_regclass('public.emr_record_score_current') IS NOT NULL AND NOT EXISTS (SELECT 1 FROM pg_constraint WHERE conrelid='public.emr_record_score_current'::regclass AND conname='emr_record_score_current_record_id_fkey') THEN ALTER TABLE public.emr_record_score_current ADD CONSTRAINT emr_record_score_current_record_id_fkey FOREIGN KEY (record_id) REFERENCES public.emr_records(id) ON DELETE CASCADE; END IF; END $$;
DO $$ BEGIN IF to_regclass('public.emr_record_score_current') IS NOT NULL AND NOT EXISTS (SELECT 1 FROM pg_constraint WHERE conrelid='public.emr_record_score_current'::regclass AND conname='emr_record_score_current_snapshot_id_fkey') THEN ALTER TABLE public.emr_record_score_current ADD CONSTRAINT emr_record_score_current_snapshot_id_fkey FOREIGN KEY (snapshot_id) REFERENCES public.emr_record_snapshots(id) ON DELETE SET NULL; END IF; END $$;
DO $$ BEGIN IF to_regclass('public.emr_records') IS NOT NULL AND NOT EXISTS (SELECT 1 FROM pg_constraint WHERE conrelid='public.emr_records'::regclass AND conname='emr_records_template_id_fkey') THEN ALTER TABLE public.emr_records ADD CONSTRAINT emr_records_template_id_fkey FOREIGN KEY (template_id) REFERENCES public.emr_templates(id) ON DELETE RESTRICT; END IF; END $$;
DO $$ BEGIN IF to_regclass('public.emr_template_fields') IS NOT NULL AND NOT EXISTS (SELECT 1 FROM pg_constraint WHERE conrelid='public.emr_template_fields'::regclass AND conname='emr_template_fields_field_fk') THEN ALTER TABLE public.emr_template_fields ADD CONSTRAINT emr_template_fields_field_fk FOREIGN KEY (field_id) REFERENCES public.emr_fields(id) ON DELETE RESTRICT; END IF; END $$;
DO $$ BEGIN IF to_regclass('public.emr_template_fields') IS NOT NULL AND NOT EXISTS (SELECT 1 FROM pg_constraint WHERE conrelid='public.emr_template_fields'::regclass AND conname='emr_template_fields_template_fk') THEN ALTER TABLE public.emr_template_fields ADD CONSTRAINT emr_template_fields_template_fk FOREIGN KEY (template_id) REFERENCES public.emr_templates(id) ON DELETE CASCADE; END IF; END $$;
ALTER TABLE public.compliance_permission_assignments ALTER COLUMN id SET DEFAULT nextval('public.compliance_permission_assignments_id_seq'::regclass);
ALTER TABLE public.emr_icd10_diagnosis_catalog ALTER COLUMN id SET DEFAULT nextval('public.emr_icd10_diagnosis_catalog_id_seq'::regclass);
ALTER TABLE public.emr_permission_assignments ALTER COLUMN id SET DEFAULT nextval('public.emr_permission_assignments_id_seq'::regclass);
ALTER TABLE public.encounters ALTER COLUMN id SET DEFAULT nextval('public.encounters_id_seq'::regclass);
ALTER TABLE public.followup_data_scopes ALTER COLUMN id SET DEFAULT nextval('public.followup_data_scopes_id_seq'::regclass);
ALTER TABLE public.followup_generation_runs ALTER COLUMN id SET DEFAULT nextval('public.followup_generation_runs_id_seq'::regclass);
ALTER TABLE public.followup_permission_assignments ALTER COLUMN id SET DEFAULT nextval('public.followup_permission_assignments_id_seq'::regclass);
CREATE INDEX IF NOT EXISTS compliance_check_runs_rule_codes_gin_idx ON public.compliance_check_runs USING gin (finding_rule_codes);
CREATE INDEX IF NOT EXISTS compliance_check_runs_source_idx ON public.compliance_check_runs USING btree (tenant_id, source_type, source_id, source_version);
CREATE INDEX IF NOT EXISTS compliance_check_runs_status_idx ON public.compliance_check_runs USING btree (tenant_id, status, scene_code, created_at DESC);
CREATE INDEX IF NOT EXISTS compliance_check_runs_tenant_time_idx ON public.compliance_check_runs USING btree (tenant_id, created_at DESC);
CREATE INDEX IF NOT EXISTS compliance_findings_rule_code_idx ON public.compliance_findings USING btree (tenant_id, rule_code, created_at DESC);
CREATE INDEX IF NOT EXISTS compliance_findings_run_idx ON public.compliance_findings USING btree (run_id);
CREATE INDEX IF NOT EXISTS compliance_findings_source_idx ON public.compliance_findings USING btree (tenant_id, source_type, source_id, source_version);
CREATE INDEX IF NOT EXISTS compliance_findings_tenant_status_time_idx ON public.compliance_findings USING btree (tenant_id, status, created_at DESC);
CREATE INDEX IF NOT EXISTS compliance_rules_lookup_idx ON public.compliance_rules USING btree (subject, status, is_enabled, version);
CREATE INDEX IF NOT EXISTS compliance_rules_role_codes_gin_idx ON public.compliance_rules USING gin (role_codes);
CREATE INDEX IF NOT EXISTS compliance_rules_scene_codes_gin_idx ON public.compliance_rules USING gin (scene_codes);
CREATE UNIQUE INDEX IF NOT EXISTS emr_fields_scope_code_uq ON public.emr_fields USING btree (tenant_id, code);
CREATE INDEX IF NOT EXISTS emr_fields_scope_status_idx ON public.emr_fields USING btree (tenant_id, status, specialty, code);
CREATE UNIQUE INDEX IF NOT EXISTS emr_fields_tenant_code_uq ON public.emr_fields USING btree (tenant_id, code);
CREATE INDEX IF NOT EXISTS emr_fields_tenant_status_specialty_idx ON public.emr_fields USING btree (tenant_id, status, specialty, code);
CREATE UNIQUE INDEX IF NOT EXISTS emr_icd10_catalog_code_diagnosis_code_uq ON public.emr_icd10_diagnosis_catalog USING btree (catalog_code, diagnosis_code);
CREATE INDEX IF NOT EXISTS emr_icd10_catalog_diagnosis_name_idx ON public.emr_icd10_diagnosis_catalog USING btree (catalog_code, diagnosis_name) WHERE (is_active = true);
CREATE INDEX IF NOT EXISTS emr_icd10_catalog_normalized_name_idx ON public.emr_icd10_diagnosis_catalog USING btree (catalog_code, normalized_name) WHERE (is_active = true);
CREATE INDEX IF NOT EXISTS emr_process_records_record_time_idx ON public.emr_process_records USING btree (record_id, occurred_at DESC, id DESC);
CREATE INDEX IF NOT EXISTS emr_process_records_tenant_time_idx ON public.emr_process_records USING btree (tenant_id, occurred_at DESC, id DESC);
CREATE INDEX IF NOT EXISTS emr_quality_results_rule_result_idx ON public.emr_quality_results USING btree (rule_code, result, created_at DESC);
CREATE UNIQUE INDEX IF NOT EXISTS emr_quality_results_run_rule_uq ON public.emr_quality_results USING btree (run_id, rule_code);
CREATE UNIQUE INDEX IF NOT EXISTS emr_quality_rule_definitions_code_version_uq ON public.emr_quality_rule_definitions USING btree (rule_code, version);
CREATE INDEX IF NOT EXISTS emr_quality_rule_definitions_published_idx ON public.emr_quality_rule_definitions USING btree (status, section_code, rule_code);
CREATE INDEX IF NOT EXISTS emr_quality_runs_record_time_idx ON public.emr_quality_runs USING btree (record_id, started_at DESC);
CREATE INDEX IF NOT EXISTS emr_quality_runs_reusable_idx ON public.emr_quality_runs USING btree (tenant_id, record_id, action, rule_set_version, input_digest);
CREATE INDEX IF NOT EXISTS emr_quality_runs_tenant_result_idx ON public.emr_quality_runs USING btree (tenant_id, overall_result, started_at DESC);
CREATE INDEX IF NOT EXISTS emr_record_score_current_items_record_idx ON public.emr_record_score_current_items USING btree (record_id, sort_order, id);
CREATE INDEX IF NOT EXISTS emr_record_score_current_items_tenant_section_idx ON public.emr_record_score_current_items USING btree (tenant_id, section_code, updated_at DESC);
CREATE INDEX IF NOT EXISTS emr_record_score_current_tenant_grade_idx ON public.emr_record_score_current USING btree (tenant_id, grade, scored_at DESC);
CREATE INDEX IF NOT EXISTS emr_record_score_current_tenant_time_idx ON public.emr_record_score_current USING btree (tenant_id, scored_at DESC);
CREATE INDEX IF NOT EXISTS emr_record_snapshots_quality_run_idx ON public.emr_record_snapshots USING btree (record_id, quality_run_id);
CREATE UNIQUE INDEX IF NOT EXISTS emr_record_snapshots_record_no_uq ON public.emr_record_snapshots USING btree (record_id, snapshot_no);
CREATE INDEX IF NOT EXISTS emr_record_snapshots_record_time_idx ON public.emr_record_snapshots USING btree (record_id, formed_at DESC);
CREATE INDEX IF NOT EXISTS emr_records_current_grade_idx ON public.emr_records USING btree (tenant_id, current_grade, updated_at DESC);
CREATE INDEX IF NOT EXISTS emr_records_template_idx ON public.emr_records USING btree (template_id);
CREATE INDEX IF NOT EXISTS emr_records_tenant_customer_idx ON public.emr_records USING btree (tenant_id, customer_id, started_at DESC);
CREATE INDEX IF NOT EXISTS emr_records_tenant_doctor_idx ON public.emr_records USING btree (tenant_id, doctor_id, started_at DESC);
CREATE INDEX IF NOT EXISTS emr_records_tenant_status_idx ON public.emr_records USING btree (tenant_id, status, updated_at DESC);
CREATE INDEX IF NOT EXISTS emr_score_standards_active_idx ON public.emr_score_standards USING btree (code, status, published_at DESC, updated_at DESC);
CREATE UNIQUE INDEX IF NOT EXISTS emr_score_standards_code_version_uq ON public.emr_score_standards USING btree (code, version);
CREATE INDEX IF NOT EXISTS emr_template_fields_field_idx ON public.emr_template_fields USING btree (field_id);
CREATE UNIQUE INDEX IF NOT EXISTS emr_template_fields_template_field_uq ON public.emr_template_fields USING btree (template_id, field_id);
CREATE INDEX IF NOT EXISTS emr_template_fields_template_order_idx ON public.emr_template_fields USING btree (template_id, display_order, id);
CREATE UNIQUE INDEX IF NOT EXISTS emr_template_fields_template_order_uq ON public.emr_template_fields USING btree (template_id, display_order);
CREATE INDEX IF NOT EXISTS emr_template_fields_tenant_template_idx ON public.emr_template_fields USING btree (tenant_id, template_id, display_order);
CREATE INDEX IF NOT EXISTS emr_templates_match_idx ON public.emr_templates USING btree (tenant_id, department_id, specialty, document_type, visit_type, status);
CREATE INDEX IF NOT EXISTS emr_templates_published_lookup_idx ON public.emr_templates USING btree (tenant_id, department_id, specialty, status, published_at DESC) WHERE ((status)::text = 'published'::text);
CREATE UNIQUE INDEX IF NOT EXISTS emr_templates_scope_code_current_uq ON public.emr_templates USING btree (COALESCE(tenant_id, (0)::bigint), code) WHERE (is_current = true);
CREATE UNIQUE INDEX IF NOT EXISTS emr_templates_scope_code_version_uq ON public.emr_templates USING btree (COALESCE(tenant_id, (0)::bigint), code, version_no);
CREATE UNIQUE INDEX IF NOT EXISTS emr_templates_tenant_code_version_uq ON public.emr_templates USING btree (tenant_id, code, version_no);
CREATE INDEX IF NOT EXISTS idx_compliance_permission_employee_id ON public.compliance_permission_assignments USING btree (employee_id);
CREATE INDEX IF NOT EXISTS idx_compliance_permission_tenant_id ON public.compliance_permission_assignments USING btree (tenant_id);
CREATE INDEX IF NOT EXISTS idx_emr_badge_sessions_employee_state ON public.emr_badge_sessions USING btree (tenant_id, employee_id, updated_at DESC);
CREATE INDEX IF NOT EXISTS idx_emr_followup_delivery_pending ON public.emr_followup_deliveries USING btree (next_attempt_at) WHERE (status = 'pending'::text);
CREATE INDEX IF NOT EXISTS idx_emr_permission_assignments_employee_id ON public.emr_permission_assignments USING btree (employee_id);
CREATE INDEX IF NOT EXISTS idx_emr_permission_assignments_tenant_id ON public.emr_permission_assignments USING btree (tenant_id);
CREATE INDEX IF NOT EXISTS idx_encounters_tenant_patient ON public.encounters USING btree (tenant_id, patient_id, started_at DESC);
CREATE INDEX IF NOT EXISTS idx_encounters_tenant_provider ON public.encounters USING btree (tenant_id, provider_id, started_at DESC);
CREATE UNIQUE INDEX IF NOT EXISTS idx_encounters_tenant_source ON public.encounters USING btree (tenant_id, source_type, source_id);
CREATE INDEX IF NOT EXISTS idx_encounters_tenant_status ON public.encounters USING btree (tenant_id, status, updated_at DESC);
CREATE INDEX IF NOT EXISTS idx_followup_data_scopes_target ON public.followup_data_scopes USING btree (tenant_id, target_employee_id);
CREATE INDEX IF NOT EXISTS idx_followup_data_scopes_viewer ON public.followup_data_scopes USING btree (tenant_id, viewer_employee_id);
CREATE INDEX IF NOT EXISTS idx_followup_generation_runs_created_at ON public.followup_generation_runs USING btree (created_at DESC);
CREATE INDEX IF NOT EXISTS idx_followup_generation_runs_encounter ON public.followup_generation_runs USING btree (encounter_id);
CREATE INDEX IF NOT EXISTS idx_followup_generation_runs_recording ON public.followup_generation_runs USING btree (recording_id);
CREATE UNIQUE INDEX IF NOT EXISTS idx_followup_generation_runs_request_id ON public.followup_generation_runs USING btree (request_id);
CREATE INDEX IF NOT EXISTS idx_followup_generation_runs_status_retry ON public.followup_generation_runs USING btree (status, next_retry_at);
CREATE INDEX IF NOT EXISTS idx_followup_generation_runs_tenant ON public.followup_generation_runs USING btree (tenant_id);
CREATE INDEX IF NOT EXISTS idx_followup_permission_employee_id ON public.followup_permission_assignments USING btree (employee_id);
CREATE INDEX IF NOT EXISTS idx_followup_permission_tenant_id ON public.followup_permission_assignments USING btree (tenant_id);
CREATE UNIQUE INDEX IF NOT EXISTS uk_emr_badge_sessions_active_employee ON public.emr_badge_sessions USING btree (tenant_id, employee_id) WHERE ((state)::text = 'recording'::text);
CREATE UNIQUE INDEX IF NOT EXISTS uk_emr_badge_sessions_order_no ON public.emr_badge_sessions USING btree (tenant_id, order_no);

ALTER TABLE IF EXISTS public.employees ADD COLUMN IF NOT EXISTS medical_department_code VARCHAR(64);
ALTER TABLE IF EXISTS public.recording_emr_drafts ADD COLUMN IF NOT EXISTS encounter_id BIGINT;
ALTER TABLE IF EXISTS public.recordings ADD COLUMN IF NOT EXISTS encounter_id BIGINT;
ALTER TABLE IF EXISTS public.recording_tasks ADD COLUMN IF NOT EXISTS background TEXT, ADD COLUMN IF NOT EXISTS contact_method VARCHAR(64), ADD COLUMN IF NOT EXISTS doctor_name VARCHAR(255), ADD COLUMN IF NOT EXISTS editable_fields JSONB, ADD COLUMN IF NOT EXISTS encounter_id BIGINT, ADD COLUMN IF NOT EXISTS evidence TEXT, ADD COLUMN IF NOT EXISTS generation_model VARCHAR(128), ADD COLUMN IF NOT EXISTS generation_prompt_version VARCHAR(64), ADD COLUMN IF NOT EXISTS generation_source VARCHAR(64), ADD COLUMN IF NOT EXISTS purpose TEXT, ADD COLUMN IF NOT EXISTS raw_generation_payload JSONB, ADD COLUMN IF NOT EXISTS return_reason TEXT, ADD COLUMN IF NOT EXISTS returned_at TIMESTAMPTZ, ADD COLUMN IF NOT EXISTS returned_by BIGINT, ADD COLUMN IF NOT EXISTS review_action VARCHAR(64), ADD COLUMN IF NOT EXISTS send_result VARCHAR(64), ADD COLUMN IF NOT EXISTS sent_at TIMESTAMPTZ, ADD COLUMN IF NOT EXISTS sent_by BIGINT;
CREATE INDEX IF NOT EXISTS idx_employees_tenant_medical_department ON public.employees(tenant_id,medical_department_code);
CREATE INDEX IF NOT EXISTS idx_recordings_tenant_encounter ON public.recordings(tenant_id,encounter_id);

-- Deterministic legacy links only; no guessing and no overwrite of existing links.
UPDATE public.recordings r SET encounter_id=e.id FROM public.encounters e WHERE r.encounter_id IS NULL AND e.source_type='recording' AND e.source_id=r.id;
UPDATE public.recording_emr_drafts d SET encounter_id=r.encounter_id FROM public.recordings r WHERE d.encounter_id IS NULL AND d.recording_id=r.id AND r.encounter_id IS NOT NULL;
UPDATE public.recording_tasks t SET encounter_id=r.encounter_id FROM public.recordings r WHERE t.encounter_id IS NULL AND t.recording_id=r.id AND r.encounter_id IS NOT NULL;

-- Formal global EMR templates and fields. IDs are generated in target; business codes are the identity.
INSERT INTO public.emr_templates (tenant_id,code,name,version_no,document_type,visit_type,department_id,specialty,status,print_title,description,created_by,published_by,disabled_by,published_at,disabled_at,updated_by,is_current) VALUES (0,'dental','口腔门诊病历','1.0','门诊病历','通用',NULL,'口腔','published','口腔门诊病历','平台默认口腔门诊病历模板，包含牙位、口腔专科检查和治疗计划栏位.',0,0,NULL,'2026-09-13T19:07:15.003416+08:00',NULL,0,TRUE) ON CONFLICT (tenant_id,code,version_no) DO UPDATE SET name=EXCLUDED.name,document_type=EXCLUDED.document_type,visit_type=EXCLUDED.visit_type,specialty=EXCLUDED.specialty,status=EXCLUDED.status,print_title=EXCLUDED.print_title,description=EXCLUDED.description,published_by=EXCLUDED.published_by,published_at=EXCLUDED.published_at,disabled_by=EXCLUDED.disabled_by,disabled_at=EXCLUDED.disabled_at,updated_by=EXCLUDED.updated_by,is_current=EXCLUDED.is_current,updated_at=NOW();
INSERT INTO public.emr_templates (tenant_id,code,name,version_no,document_type,visit_type,department_id,specialty,status,print_title,description,created_by,published_by,disabled_by,published_at,disabled_at,updated_by,is_current) VALUES (0,'gynecology','妇科门诊病历','1.0','门诊病历','通用',NULL,'妇科','published','妇科门诊病历','平台默认妇科门诊病历模板，包含月经史、婚育史和孕产史栏位。',0,0,NULL,'2026-09-13T19:07:15.003416+08:00',NULL,0,TRUE) ON CONFLICT (tenant_id,code,version_no) DO UPDATE SET name=EXCLUDED.name,document_type=EXCLUDED.document_type,visit_type=EXCLUDED.visit_type,specialty=EXCLUDED.specialty,status=EXCLUDED.status,print_title=EXCLUDED.print_title,description=EXCLUDED.description,published_by=EXCLUDED.published_by,published_at=EXCLUDED.published_at,disabled_by=EXCLUDED.disabled_by,disabled_at=EXCLUDED.disabled_at,updated_by=EXCLUDED.updated_by,is_current=EXCLUDED.is_current,updated_at=NOW();
INSERT INTO public.emr_templates (tenant_id,code,name,version_no,document_type,visit_type,department_id,specialty,status,print_title,description,created_by,published_by,disabled_by,published_at,disabled_at,updated_by,is_current) VALUES (0,'internal_medicine','内科门诊病历','1.0','门诊病历','通用',NULL,'内科','published','内科门诊病历','平台默认内科门诊病历模板。',0,0,NULL,'2026-09-13T19:07:15.003416+08:00',NULL,0,TRUE) ON CONFLICT (tenant_id,code,version_no) DO UPDATE SET name=EXCLUDED.name,document_type=EXCLUDED.document_type,visit_type=EXCLUDED.visit_type,specialty=EXCLUDED.specialty,status=EXCLUDED.status,print_title=EXCLUDED.print_title,description=EXCLUDED.description,published_by=EXCLUDED.published_by,published_at=EXCLUDED.published_at,disabled_by=EXCLUDED.disabled_by,disabled_at=EXCLUDED.disabled_at,updated_by=EXCLUDED.updated_by,is_current=EXCLUDED.is_current,updated_at=NOW();
INSERT INTO public.emr_templates (tenant_id,code,name,version_no,document_type,visit_type,department_id,specialty,status,print_title,description,created_by,published_by,disabled_by,published_at,disabled_at,updated_by,is_current) VALUES (0,'pediatrics','儿科门诊病历','1.0','门诊病历','通用',NULL,'儿科','published','儿科门诊病历','平台默认儿科门诊病历模板，包含出生史、喂养史和儿童用药依据栏位。',0,0,NULL,'2026-09-13T19:07:15.003416+08:00',NULL,0,TRUE) ON CONFLICT (tenant_id,code,version_no) DO UPDATE SET name=EXCLUDED.name,document_type=EXCLUDED.document_type,visit_type=EXCLUDED.visit_type,specialty=EXCLUDED.specialty,status=EXCLUDED.status,print_title=EXCLUDED.print_title,description=EXCLUDED.description,published_by=EXCLUDED.published_by,published_at=EXCLUDED.published_at,disabled_by=EXCLUDED.disabled_by,disabled_at=EXCLUDED.disabled_at,updated_by=EXCLUDED.updated_by,is_current=EXCLUDED.is_current,updated_at=NOW();
INSERT INTO public.emr_templates (tenant_id,code,name,version_no,document_type,visit_type,department_id,specialty,status,print_title,description,created_by,published_by,disabled_by,published_at,disabled_at,updated_by,is_current) VALUES (0,'standard_outpatient','标准门急诊病历','1.0','门诊病历','通用',NULL,NULL,'published','门诊病历','平台提供的基础门急诊病历模板',0,0,NULL,'2026-09-13T15:25:12.039444+08:00',NULL,0,TRUE) ON CONFLICT (tenant_id,code,version_no) DO UPDATE SET name=EXCLUDED.name,document_type=EXCLUDED.document_type,visit_type=EXCLUDED.visit_type,specialty=EXCLUDED.specialty,status=EXCLUDED.status,print_title=EXCLUDED.print_title,description=EXCLUDED.description,published_by=EXCLUDED.published_by,published_at=EXCLUDED.published_at,disabled_by=EXCLUDED.disabled_by,disabled_at=EXCLUDED.disabled_at,updated_by=EXCLUDED.updated_by,is_current=EXCLUDED.is_current,updated_at=NOW();
INSERT INTO public.emr_templates (tenant_id,code,name,version_no,document_type,visit_type,department_id,specialty,status,print_title,description,created_by,published_by,disabled_by,published_at,disabled_at,updated_by,is_current) VALUES (0,'tcm','中医门诊病历','1.0','门诊病历','通用',NULL,'中医','published','中医门诊病历','平台默认中医门诊病历模板，包含中医四诊、辨证和治法栏位。',0,0,NULL,'2026-09-13T19:07:15.003416+08:00',NULL,0,TRUE) ON CONFLICT (tenant_id,code,version_no) DO UPDATE SET name=EXCLUDED.name,document_type=EXCLUDED.document_type,visit_type=EXCLUDED.visit_type,specialty=EXCLUDED.specialty,status=EXCLUDED.status,print_title=EXCLUDED.print_title,description=EXCLUDED.description,published_by=EXCLUDED.published_by,published_at=EXCLUDED.published_at,disabled_by=EXCLUDED.disabled_by,disabled_at=EXCLUDED.disabled_at,updated_by=EXCLUDED.updated_by,is_current=EXCLUDED.is_current,updated_at=NOW();
INSERT INTO public.emr_fields (tenant_id,code,name,specialty,content_category,input_type,structure_json,options_json,description,status,created_by,updated_by) VALUES (0,'allergy_history','过敏史',NULL,'病史','长文本','{}'::jsonb,'{}'::jsonb,'allergy_history：过敏史

适用范围：通用

质量要求：

- 必须区分无过敏史、具体过敏原、药物过敏和其他过敏。
- 必须区分明确否认和未询问。
- 有过敏时应尽量记录过敏原和反应。
- 不能将“患者没有回答”写成“无过敏史”。
- 应与处方、用药和患者档案中的过敏信息保持一致。

病历生成要求：

- 只根据明确回答记录过敏原、过敏反应和否认信息。
- 使用“明确无过敏史”“有具体药物过敏史”“有其他过敏原”“尚未询问”“信息待确认”等状态。
- 不把医生提问、患者沉默或无关回答当成否认。

可执行检查类型：状态、非空、语义、栏位关系、人工判断

本栏位相关的多栏位联合质量要求：

### `allergy_history` + `prescription`

要求：

- 处方不能明显违反已确认的药物过敏信息。
- 未确认的过敏信息不能被当成明确无过敏。

判断方式：结构化判断，无法确定时人工复核','enabled',0,0) ON CONFLICT (tenant_id,code) DO UPDATE SET name=EXCLUDED.name,specialty=EXCLUDED.specialty,content_category=EXCLUDED.content_category,input_type=EXCLUDED.input_type,structure_json=EXCLUDED.structure_json,options_json=EXCLUDED.options_json,description=EXCLUDED.description,status=EXCLUDED.status,updated_by=EXCLUDED.updated_by,updated_at=NOW();
INSERT INTO public.emr_fields (tenant_id,code,name,specialty,content_category,input_type,structure_json,options_json,description,status,created_by,updated_by) VALUES (0,'auxiliary_exam','辅助检查',NULL,'诊疗依据','长文本','{}'::jsonb,'{}'::jsonb,'auxiliary_exam：辅助检查

适用范围：通用

质量要求：

- 应区分检查项目、检查时间和检查结果。
- 已做检查但没有结果时，必须标记结果缺失。
- 未做检查不能写成正常。
- 不能把医生建议检查写成已经完成。
- 检查结果应与诊断和治疗方案保持事实一致。

病历生成要求：

- 分别记录检查项目、时间、结果和来源。
- 区分已完成、已安排、未完成和结果待补。
- 不把检查计划或医生建议转换成检查结果。

可执行检查类型：结构完整、状态、语义、栏位关系、人工判断','enabled',0,0) ON CONFLICT (tenant_id,code) DO UPDATE SET name=EXCLUDED.name,specialty=EXCLUDED.specialty,content_category=EXCLUDED.content_category,input_type=EXCLUDED.input_type,structure_json=EXCLUDED.structure_json,options_json=EXCLUDED.options_json,description=EXCLUDED.description,status=EXCLUDED.status,updated_by=EXCLUDED.updated_by,updated_at=NOW();
INSERT INTO public.emr_fields (tenant_id,code,name,specialty,content_category,input_type,structure_json,options_json,description,status,created_by,updated_by) VALUES (0,'chief_complaint','主诉',NULL,'病史','长文本','{}'::jsonb,'{}'::jsonb,'chief_complaint：主诉

适用范围：通用

质量要求：

- 必须记录患者本次就诊最主要的问题。
- 应包含主要症状或体征、发生部位和持续时间。
- 应简明，不能写成大段现病史。
- 不能只写疾病名称。
- 不能把模型推测的诊断写入主诉。
- 没有明确内容时留空并标记待补充。
- 字数不超过20字

病历生成要求：

- 从转写中提取患者本次最主要的症状、体征、部位和持续时间。
- 只保留明确事实，不把诊断推测写入主诉。
- 将主诉压缩为简洁的门诊表达。字数不超过20字。
- 缺少持续时间或其他关键信息时保留缺失，不得自行补充。

可执行检查类型：非空、长度、语义、栏位关系

本栏位相关的多栏位联合质量要求：

### `chief_complaint` + `present_illness`

要求：

- 主诉应能在现病史中找到依据。
- 现病史不能与主诉明显冲突。
- 主诉和现病史中的症状、部位、时间表达应保持基本一致。

判断方式：大模型语义判断，必要时人工复核','enabled',0,0) ON CONFLICT (tenant_id,code) DO UPDATE SET name=EXCLUDED.name,specialty=EXCLUDED.specialty,content_category=EXCLUDED.content_category,input_type=EXCLUDED.input_type,structure_json=EXCLUDED.structure_json,options_json=EXCLUDED.options_json,description=EXCLUDED.description,status=EXCLUDED.status,updated_by=EXCLUDED.updated_by,updated_at=NOW();
INSERT INTO public.emr_fields (tenant_id,code,name,specialty,content_category,input_type,structure_json,options_json,description,status,created_by,updated_by) VALUES (0,'diagnosis','诊断',NULL,'医疗判断','长文本','{}'::jsonb,'{}'::jsonb,'diagnosis：诊断

适用范围：通用

质量要求：

- 只能记录医生明确提出或确认的诊断。
- 应区分明确诊断、疑似诊断和待排诊断。
- 诊断应有主诉、现病史、查体或检查依据。
- 不能把患者自述直接当作正式诊断。
- 不确定表达不能被模型强化成确定诊断。

病历生成要求：

- 保留医生诊断原意和确定程度。
- 区分明确、可能、考虑、待排等表达。
- 不根据症状自行新增疾病诊断。
- 没有明确诊断时留空或标记待医生确认。

可执行检查类型：非空、语义、证据关联、栏位关系、人工判断

本栏位相关的多栏位联合质量要求：

### `diagnosis` + `auxiliary_exam`

要求：

- 诊断不得虚构检查依据。
- 检查结果与诊断不能明显冲突。
- 已安排检查不能被当作诊断依据。

判断方式：大模型语义判断，必要时人工复核

### `diagnosis` + `prescription` + `medical_advice`

要求：

- 诊断、处方和医嘱应逻辑一致。
- 药品、用法、疗程和医嘱不能互相矛盾。
- 不能把缺失的处方信息推导为确定用药方案。

判断方式：结构化判断 + 大模型语义判断，必要时人工复核

### `diagnosis` + `disposition`

要求：

- 处置应与诊断和病情记录一致。
- 必须区分计划处置和已经执行的处置。

判断方式：结构化判断 + 大模型语义判断','enabled',0,0) ON CONFLICT (tenant_id,code) DO UPDATE SET name=EXCLUDED.name,specialty=EXCLUDED.specialty,content_category=EXCLUDED.content_category,input_type=EXCLUDED.input_type,structure_json=EXCLUDED.structure_json,options_json=EXCLUDED.options_json,description=EXCLUDED.description,status=EXCLUDED.status,updated_by=EXCLUDED.updated_by,updated_at=NOW();
INSERT INTO public.emr_fields (tenant_id,code,name,specialty,content_category,input_type,structure_json,options_json,description,status,created_by,updated_by) VALUES (0,'disposition','处置',NULL,'医疗措施','长文本','{}'::jsonb,'{}'::jsonb,'disposition：处置

适用范围：通用

质量要求：

- 记录本次就诊实际采取的处置措施。
- 应区分已执行、已安排和仅建议。
- 不能把计划写成已经完成。
- 不得增加转诊、住院或检查安排。
- 应与诊断和医嘱保持一致。

病历生成要求：

- 区分检查申请、治疗操作、输液、注射及其他处置。
- 明确记录已执行、已安排或仅建议的状态。
- 不增加转写中没有出现的处置。

可执行检查类型：状态、结构完整、语义、栏位关系、人工判断','enabled',0,0) ON CONFLICT (tenant_id,code) DO UPDATE SET name=EXCLUDED.name,specialty=EXCLUDED.specialty,content_category=EXCLUDED.content_category,input_type=EXCLUDED.input_type,structure_json=EXCLUDED.structure_json,options_json=EXCLUDED.options_json,description=EXCLUDED.description,status=EXCLUDED.status,updated_by=EXCLUDED.updated_by,updated_at=NOW();
INSERT INTO public.emr_fields (tenant_id,code,name,specialty,content_category,input_type,structure_json,options_json,description,status,created_by,updated_by) VALUES (0,'family_history','家族史',NULL,'病史','长文本','{}'::jsonb,'{}'::jsonb,'family_history：家族史

适用范围：通用

质量要求：

- 应记录明确提到的家族疾病和亲属关系。
- 有明确否认时记录否认。
- 未询问不能写成“无家族史”。
- 不得根据疾病类型推测遗传风险。

病历生成要求：

- 记录明确出现的疾病、亲属关系和家族史否认信息。
- 不从患者当前疾病推导遗传风险。

可执行检查类型：非空、语义、人工判断','enabled',0,0) ON CONFLICT (tenant_id,code) DO UPDATE SET name=EXCLUDED.name,specialty=EXCLUDED.specialty,content_category=EXCLUDED.content_category,input_type=EXCLUDED.input_type,structure_json=EXCLUDED.structure_json,options_json=EXCLUDED.options_json,description=EXCLUDED.description,status=EXCLUDED.status,updated_by=EXCLUDED.updated_by,updated_at=NOW();
INSERT INTO public.emr_fields (tenant_id,code,name,specialty,content_category,input_type,structure_json,options_json,description,status,created_by,updated_by) VALUES (0,'followup','复诊安排',NULL,'医疗措施','长文本','{}'::jsonb,'{}'::jsonb,'followup：复诊安排

适用范围：通用

质量要求：

- 只记录医生明确交代的复诊、复查或联系安排。
- 明确日期或相对时间应保留。
- 模糊时间不能擅自转换成具体日期。
- 没有明确安排时保持为空。
- 不得为了提高复诊率自动生成复诊计划。

病历生成要求：

- 保留医生明确说出的日期、相对时间和复诊条件。
- 区分明确安排、建议和患者自行计划。
- 无明确安排时不生成日期。

可执行检查类型：日期格式、时间语义、非空、人工判断','enabled',0,0) ON CONFLICT (tenant_id,code) DO UPDATE SET name=EXCLUDED.name,specialty=EXCLUDED.specialty,content_category=EXCLUDED.content_category,input_type=EXCLUDED.input_type,structure_json=EXCLUDED.structure_json,options_json=EXCLUDED.options_json,description=EXCLUDED.description,status=EXCLUDED.status,updated_by=EXCLUDED.updated_by,updated_at=NOW();
INSERT INTO public.emr_fields (tenant_id,code,name,specialty,content_category,input_type,structure_json,options_json,description,status,created_by,updated_by) VALUES (0,'gynecology_auxiliary_exam','妇科相关辅助检查','妇科','诊疗依据','结构化内容','{}'::jsonb,'{}'::jsonb,'gynecology_auxiliary_exam：妇科相关辅助检查

适用范围：妇科

质量要求：

- 记录检查项目、时间和结果。
- 区分已完成、已安排和未完成。
- 不能补充检查结果。
- 应与妇科诊断和治疗计划保持一致。

病历生成要求：

- 分别记录检查名称、时间、结果和来源。
- 不把检查计划当成检查结果。

可执行检查类型：结构完整、状态、日期、语义、栏位关系

## 三、中医栏位','enabled',0,0) ON CONFLICT (tenant_id,code) DO UPDATE SET name=EXCLUDED.name,specialty=EXCLUDED.specialty,content_category=EXCLUDED.content_category,input_type=EXCLUDED.input_type,structure_json=EXCLUDED.structure_json,options_json=EXCLUDED.options_json,description=EXCLUDED.description,status=EXCLUDED.status,updated_by=EXCLUDED.updated_by,updated_at=NOW();
INSERT INTO public.emr_fields (tenant_id,code,name,specialty,content_category,input_type,structure_json,options_json,description,status,created_by,updated_by) VALUES (0,'gynecology_specialty_exam','妇科专科查体','妇科','诊疗依据','长文本','{}'::jsonb,'{}'::jsonb,'gynecology_specialty_exam：妇科专科查体

适用范围：妇科

质量要求：

- 只记录实际完成的妇科检查。
- 应区分检查所见和医生判断。
- 未检查不能写成正常。
- 不得根据症状虚构妇科体征。
- 涉及隐私的内容必须有明确记录依据。

病历生成要求：

- 只提取医生明确描述的妇科检查所见。
- 区分未检查、已检查未见异常和发现异常。

可执行检查类型：非空、语义、结构关系、人工判断','enabled',0,0) ON CONFLICT (tenant_id,code) DO UPDATE SET name=EXCLUDED.name,specialty=EXCLUDED.specialty,content_category=EXCLUDED.content_category,input_type=EXCLUDED.input_type,structure_json=EXCLUDED.structure_json,options_json=EXCLUDED.options_json,description=EXCLUDED.description,status=EXCLUDED.status,updated_by=EXCLUDED.updated_by,updated_at=NOW();
INSERT INTO public.emr_fields (tenant_id,code,name,specialty,content_category,input_type,structure_json,options_json,description,status,created_by,updated_by) VALUES (0,'last_menstrual_period','末次月经','妇科','病史','日期','{}'::jsonb,'{}'::jsonb,'last_menstrual_period：末次月经

适用范围：妇科

质量要求：

- 只能填写明确日期或明确时间表达。
- 日期必须合法。
- 模糊表达不得转换成具体日期。
- 未提及时保持为空。
- 应与孕产史和妊娠相关内容保持一致。

病历生成要求：

- 只记录明确的末次月经日期。
- 对“上个月”“月初”等模糊表达不擅自转换日期。

可执行检查类型：日期格式、时间语义、栏位关系、人工判断','enabled',0,0) ON CONFLICT (tenant_id,code) DO UPDATE SET name=EXCLUDED.name,specialty=EXCLUDED.specialty,content_category=EXCLUDED.content_category,input_type=EXCLUDED.input_type,structure_json=EXCLUDED.structure_json,options_json=EXCLUDED.options_json,description=EXCLUDED.description,status=EXCLUDED.status,updated_by=EXCLUDED.updated_by,updated_at=NOW();
INSERT INTO public.emr_fields (tenant_id,code,name,specialty,content_category,input_type,structure_json,options_json,description,status,created_by,updated_by) VALUES (0,'marital_reproductive_history','婚育史','妇科','病史','结构化内容','{}'::jsonb,'{}'::jsonb,'marital_reproductive_history：婚育史

适用范围：妇科

质量要求：

- 记录婚姻和生育情况的明确陈述。
- 应区分婚姻史和生育史。
- 不得根据年龄或称谓推断。
- 未询问不能写成无婚育史。

病历生成要求：

- 分别记录婚姻状态、妊娠、生育和子女情况。
- 仅保留明确陈述。

可执行检查类型：结构完整、数值格式、语义、人工判断','enabled',0,0) ON CONFLICT (tenant_id,code) DO UPDATE SET name=EXCLUDED.name,specialty=EXCLUDED.specialty,content_category=EXCLUDED.content_category,input_type=EXCLUDED.input_type,structure_json=EXCLUDED.structure_json,options_json=EXCLUDED.options_json,description=EXCLUDED.description,status=EXCLUDED.status,updated_by=EXCLUDED.updated_by,updated_at=NOW();
INSERT INTO public.emr_fields (tenant_id,code,name,specialty,content_category,input_type,structure_json,options_json,description,status,created_by,updated_by) VALUES (0,'medical_advice','医嘱',NULL,'医疗措施','长文本','{}'::jsonb,'{}'::jsonb,'medical_advice：医嘱

适用范围：通用

质量要求：

- 记录医生明确交代的用药、观察、生活和注意事项。
- 必须保留明确的时间、频次和执行条件。
- 不得增加医生没有说过的医学建议。
- 不得把模型推断写成医嘱。
- 应与处方和诊断保持一致。

病历生成要求：

- 只提取医生明确交代的执行事项。
- 保留时间、频次、条件和异常处理要求。
- 将患者问题和医生回答区分开。

可执行检查类型：非空、语义、栏位关系、人工判断','enabled',0,0) ON CONFLICT (tenant_id,code) DO UPDATE SET name=EXCLUDED.name,specialty=EXCLUDED.specialty,content_category=EXCLUDED.content_category,input_type=EXCLUDED.input_type,structure_json=EXCLUDED.structure_json,options_json=EXCLUDED.options_json,description=EXCLUDED.description,status=EXCLUDED.status,updated_by=EXCLUDED.updated_by,updated_at=NOW();
INSERT INTO public.emr_fields (tenant_id,code,name,specialty,content_category,input_type,structure_json,options_json,description,status,created_by,updated_by) VALUES (0,'menstrual_history','月经史','妇科','病史','结构化内容','{}'::jsonb,'{}'::jsonb,'menstrual_history：月经史

适用范围：妇科

质量要求：

- 应记录月经周期、经期、经量和规律性等明确内容。
- 未询问的项目不能写成正常。
- 异常情况应保留患者原始表述。
- 不能根据年龄或症状推测月经状态。

病历生成要求：

- 按周期、经期、经量、末次月经、痛经和其他情况组织。
- 未提及的项目留空或标记未询问。

可执行检查类型：结构完整、日期、数值格式、语义、人工判断','enabled',0,0) ON CONFLICT (tenant_id,code) DO UPDATE SET name=EXCLUDED.name,specialty=EXCLUDED.specialty,content_category=EXCLUDED.content_category,input_type=EXCLUDED.input_type,structure_json=EXCLUDED.structure_json,options_json=EXCLUDED.options_json,description=EXCLUDED.description,status=EXCLUDED.status,updated_by=EXCLUDED.updated_by,updated_at=NOW();
INSERT INTO public.emr_fields (tenant_id,code,name,specialty,content_category,input_type,structure_json,options_json,description,status,created_by,updated_by) VALUES (0,'obstetric_history','孕产史','妇科','病史','结构化内容','{}'::jsonb,'{}'::jsonb,'obstetric_history：孕产史

适用范围：妇科

质量要求：

- 记录孕次、产次、流产、分娩及相关情况。
- 数量必须来自明确事实。
- 不确定或无法确认时标记待确认。
- 不得从“怀孕过”推导完整孕产次数。

病历生成要求：

- 分别记录孕次、产次、流产及分娩细节。
- 不自行推算孕产次数。

可执行检查类型：结构完整、数值格式、语义、栏位关系、人工判断','enabled',0,0) ON CONFLICT (tenant_id,code) DO UPDATE SET name=EXCLUDED.name,specialty=EXCLUDED.specialty,content_category=EXCLUDED.content_category,input_type=EXCLUDED.input_type,structure_json=EXCLUDED.structure_json,options_json=EXCLUDED.options_json,description=EXCLUDED.description,status=EXCLUDED.status,updated_by=EXCLUDED.updated_by,updated_at=NOW();
INSERT INTO public.emr_fields (tenant_id,code,name,specialty,content_category,input_type,structure_json,options_json,description,status,created_by,updated_by) VALUES (0,'oral_imaging_findings','影像所见','口腔','诊疗依据','长文本','{}'::jsonb,'{}'::jsonb,'oral_imaging_findings：影像所见

适用范围：口腔

质量要求：

- 记录检查项目和明确影像结果。
- 区分已检查、已安排和未检查。
- 不能补充影像结论。
- 应与牙位、诊断和治疗计划一致。

病历生成要求：

- 只记录明确影像检查结果。
- 区分已完成和已安排检查。

可执行检查类型：结构完整、状态、语义、栏位关系','enabled',0,0) ON CONFLICT (tenant_id,code) DO UPDATE SET name=EXCLUDED.name,specialty=EXCLUDED.specialty,content_category=EXCLUDED.content_category,input_type=EXCLUDED.input_type,structure_json=EXCLUDED.structure_json,options_json=EXCLUDED.options_json,description=EXCLUDED.description,status=EXCLUDED.status,updated_by=EXCLUDED.updated_by,updated_at=NOW();
INSERT INTO public.emr_fields (tenant_id,code,name,specialty,content_category,input_type,structure_json,options_json,description,status,created_by,updated_by) VALUES (0,'oral_periodontal_or_dental_condition','牙周或牙体情况','口腔','诊疗依据','长文本','{}'::jsonb,'{}'::jsonb,'oral_periodontal_or_dental_condition：牙周或牙体情况

适用范围：口腔

质量要求：

- 记录明确的牙周、牙体异常。
- 应与牙位对应。
- 不能把患者疼痛部位直接当作疾病诊断。
- 未明确检查结果时保持待确认。

病历生成要求：

- 记录明确的牙周或牙体检查结果。
- 不将疼痛部位直接改写成疾病诊断。

可执行检查类型：非空、语义、栏位关系、人工判断','enabled',0,0) ON CONFLICT (tenant_id,code) DO UPDATE SET name=EXCLUDED.name,specialty=EXCLUDED.specialty,content_category=EXCLUDED.content_category,input_type=EXCLUDED.input_type,structure_json=EXCLUDED.structure_json,options_json=EXCLUDED.options_json,description=EXCLUDED.description,status=EXCLUDED.status,updated_by=EXCLUDED.updated_by,updated_at=NOW();
INSERT INTO public.emr_fields (tenant_id,code,name,specialty,content_category,input_type,structure_json,options_json,description,status,created_by,updated_by) VALUES (0,'oral_specialty_exam','口腔专科检查','口腔','诊疗依据','长文本','{}'::jsonb,'{}'::jsonb,'oral_specialty_exam：口腔专科检查

适用范围：口腔

质量要求：

- 记录实际检查到的口腔情况。
- 应区分检查所见和诊断。
- 未检查不能写正常。
- 不得根据主诉虚构牙体或牙周表现。

病历生成要求：

- 只记录实际检查所见。
- 区分检查结果和医生诊断。

可执行检查类型：非空、语义、栏位关系、人工判断','enabled',0,0) ON CONFLICT (tenant_id,code) DO UPDATE SET name=EXCLUDED.name,specialty=EXCLUDED.specialty,content_category=EXCLUDED.content_category,input_type=EXCLUDED.input_type,structure_json=EXCLUDED.structure_json,options_json=EXCLUDED.options_json,description=EXCLUDED.description,status=EXCLUDED.status,updated_by=EXCLUDED.updated_by,updated_at=NOW();
INSERT INTO public.emr_fields (tenant_id,code,name,specialty,content_category,input_type,structure_json,options_json,description,status,created_by,updated_by) VALUES (0,'oral_tooth_position','牙位','口腔','就诊归属','结构化内容','{}'::jsonb,'{}'::jsonb,'oral_tooth_position：牙位

适用范围：口腔

质量要求：

- 记录明确的牙位或治疗部位。
- 牙位表达应统一。
- 不明确时不能推测。
- 应与口腔检查、影像和治疗计划一致。

病历生成要求：

- 只记录明确出现的牙位、牙面或区域。
- 保留原始明确表达，不自行转换不确定牙位。

可执行检查类型：结构完整、格式、语义、栏位关系

本栏位相关的多栏位联合质量要求：

### 口腔联合要求

涉及栏位：

- `oral_tooth_position`
- `oral_specialty_exam`
- `oral_periodontal_or_dental_condition`
- `oral_imaging_findings`
- `oral_treatment_plan`

要求：

- 牙位、检查结果和治疗部位必须对应。
- 影像所见、牙体牙周情况和治疗计划不能互相矛盾。
- 不明确的牙位不能被模型自行推断。

判断方式：结构化判断 + 大模型语义判断，必要时人工复核','enabled',0,0) ON CONFLICT (tenant_id,code) DO UPDATE SET name=EXCLUDED.name,specialty=EXCLUDED.specialty,content_category=EXCLUDED.content_category,input_type=EXCLUDED.input_type,structure_json=EXCLUDED.structure_json,options_json=EXCLUDED.options_json,description=EXCLUDED.description,status=EXCLUDED.status,updated_by=EXCLUDED.updated_by,updated_at=NOW();
INSERT INTO public.emr_fields (tenant_id,code,name,specialty,content_category,input_type,structure_json,options_json,description,status,created_by,updated_by) VALUES (0,'oral_treatment_plan','口腔治疗计划','口腔','医疗措施','长文本','{}'::jsonb,'{}'::jsonb,'oral_treatment_plan：口腔治疗计划

适用范围：口腔

质量要求：

- 记录医生明确提出的治疗方案。
- 应与牙位、检查和诊断对应。
- 区分计划治疗和已完成治疗。
- 不得自行增加治疗项目或疗程。

病历生成要求：

- 记录医生明确提出的治疗方案、分期和沟通结果。
- 区分治疗计划与已经完成的治疗。

可执行检查类型：非空、语义、栏位关系、人工判断

## 五、儿科栏位','enabled',0,0) ON CONFLICT (tenant_id,code) DO UPDATE SET name=EXCLUDED.name,specialty=EXCLUDED.specialty,content_category=EXCLUDED.content_category,input_type=EXCLUDED.input_type,structure_json=EXCLUDED.structure_json,options_json=EXCLUDED.options_json,description=EXCLUDED.description,status=EXCLUDED.status,updated_by=EXCLUDED.updated_by,updated_at=NOW();
INSERT INTO public.emr_fields (tenant_id,code,name,specialty,content_category,input_type,structure_json,options_json,description,status,created_by,updated_by) VALUES (0,'past_history','既往史',NULL,'病史','长文本','{}'::jsonb,'{}'::jsonb,'past_history：既往史

适用范围：通用

质量要求：

- 应记录既往疾病、手术、住院和重要治疗史。
- 有明确否认时记录否认。
- 未询问时不能写成“无既往史”。
- 疾病名称和时间不能凭空补充。
- 不能把本次疾病混入既往史。

病历生成要求：

- 只提取患者明确陈述的既往疾病、手术、住院和治疗信息。
- 区分明确否认、未询问和信息待确认。
- 不根据当前诊断推测既往病史。

可执行检查类型：非空、语义、人工判断','enabled',0,0) ON CONFLICT (tenant_id,code) DO UPDATE SET name=EXCLUDED.name,specialty=EXCLUDED.specialty,content_category=EXCLUDED.content_category,input_type=EXCLUDED.input_type,structure_json=EXCLUDED.structure_json,options_json=EXCLUDED.options_json,description=EXCLUDED.description,status=EXCLUDED.status,updated_by=EXCLUDED.updated_by,updated_at=NOW();
INSERT INTO public.emr_fields (tenant_id,code,name,specialty,content_category,input_type,structure_json,options_json,description,status,created_by,updated_by) VALUES (0,'pediatric_birth_history','出生史','儿科','病史','结构化内容','{}'::jsonb,'{}'::jsonb,'pediatric_birth_history：出生史

适用范围：儿科

质量要求：

- 记录出生方式、孕周、出生体重和出生异常等明确内容。
- 未询问不能写正常。
- 不得根据儿童年龄推测出生情况。

病历生成要求：

- 按孕周、分娩方式、出生体重和围产期情况组织。
- 未询问项目留空。

可执行检查类型：结构完整、数值格式、语义、人工判断','enabled',0,0) ON CONFLICT (tenant_id,code) DO UPDATE SET name=EXCLUDED.name,specialty=EXCLUDED.specialty,content_category=EXCLUDED.content_category,input_type=EXCLUDED.input_type,structure_json=EXCLUDED.structure_json,options_json=EXCLUDED.options_json,description=EXCLUDED.description,status=EXCLUDED.status,updated_by=EXCLUDED.updated_by,updated_at=NOW();
INSERT INTO public.emr_fields (tenant_id,code,name,specialty,content_category,input_type,structure_json,options_json,description,status,created_by,updated_by) VALUES (0,'pediatric_feeding_history','喂养史','儿科','病史','结构化内容','{}'::jsonb,'{}'::jsonb,'pediatric_feeding_history：喂养史

适用范围：儿科

质量要求：

- 记录喂养方式、喂养量、辅食和喂养困难等明确内容。
- 不得根据年龄推测喂养情况。
- 未询问项目保持未记录。

病历生成要求：

- 按喂养方式、辅食、食欲和喂养困难组织。
- 不依据年龄自动推断喂养方式或喂养量。

可执行检查类型：结构完整、数值格式、语义、人工判断','enabled',0,0) ON CONFLICT (tenant_id,code) DO UPDATE SET name=EXCLUDED.name,specialty=EXCLUDED.specialty,content_category=EXCLUDED.content_category,input_type=EXCLUDED.input_type,structure_json=EXCLUDED.structure_json,options_json=EXCLUDED.options_json,description=EXCLUDED.description,status=EXCLUDED.status,updated_by=EXCLUDED.updated_by,updated_at=NOW();
INSERT INTO public.emr_fields (tenant_id,code,name,specialty,content_category,input_type,structure_json,options_json,description,status,created_by,updated_by) VALUES (0,'pediatric_guardian_statement','监护人陈述','儿科','病史','长文本','{}'::jsonb,'{}'::jsonb,'pediatric_guardian_statement：监护人陈述

适用范围：儿科

质量要求：

- 区分监护人陈述和医生观察。
- 保留监护人对症状、病程和用药的明确描述。
- 不得将监护人意见直接改写成医学结论。
- 不得补充监护人没有表达的信息。

病历生成要求：

- 明确标记信息来源为监护人陈述。
- 区分监护人观察、监护人判断和医生判断。

可执行检查类型：非空、语义、人工判断','enabled',0,0) ON CONFLICT (tenant_id,code) DO UPDATE SET name=EXCLUDED.name,specialty=EXCLUDED.specialty,content_category=EXCLUDED.content_category,input_type=EXCLUDED.input_type,structure_json=EXCLUDED.structure_json,options_json=EXCLUDED.options_json,description=EXCLUDED.description,status=EXCLUDED.status,updated_by=EXCLUDED.updated_by,updated_at=NOW();
INSERT INTO public.emr_fields (tenant_id,code,name,specialty,content_category,input_type,structure_json,options_json,description,status,created_by,updated_by) VALUES (0,'pediatric_medication_basis','儿童特殊用药依据','儿科','医疗措施','结构化内容','{}'::jsonb,'{}'::jsonb,'pediatric_medication_basis：儿童特殊用药依据

适用范围：儿科

质量要求：

- 记录药物、体重、年龄、剂量计算和调整依据。
- 缺少依据时必须明确标记缺失。
- 不得自行计算剂量。
- 不得把常规经验写成医生实际依据。
- 应与处方、体重和诊断保持一致。

病历生成要求：

- 只记录医生明确说明的用药依据。
- 不自行计算剂量或补充年龄、体重依据。
- 缺少依据时保留缺失并标记待医生确认。

可执行检查类型：结构完整、数值格式、语义、栏位关系、人工判断','enabled',0,0) ON CONFLICT (tenant_id,code) DO UPDATE SET name=EXCLUDED.name,specialty=EXCLUDED.specialty,content_category=EXCLUDED.content_category,input_type=EXCLUDED.input_type,structure_json=EXCLUDED.structure_json,options_json=EXCLUDED.options_json,description=EXCLUDED.description,status=EXCLUDED.status,updated_by=EXCLUDED.updated_by,updated_at=NOW();
INSERT INTO public.emr_fields (tenant_id,code,name,specialty,content_category,input_type,structure_json,options_json,description,status,created_by,updated_by) VALUES (0,'pediatric_weight','儿童体重','儿科','诊疗依据','数值','{}'::jsonb,'{}'::jsonb,'pediatric_weight：儿童体重

适用范围：儿科

质量要求：

- 必须记录明确数值和单位。
- 数值应与测量时间对应。
- 不得根据年龄估算体重。
- 体重应与儿童用药依据保持一致。

病历生成要求：

- 只记录明确测量或明确提供的体重数值和单位。
- 不根据年龄、身高或疾病估算体重。

可执行检查类型：数值格式、单位、时间、栏位关系

本栏位相关的多栏位联合质量要求：

### 儿科联合要求

涉及栏位：

- `pediatric_weight`
- `pediatric_medication_basis`
- `prescription`

要求：

- 儿童用药依据应与体重和实际处方一致。
- 缺少体重或剂量依据时不能自行计算或补齐。
- 处方中的儿童用药信息应与病历中明确记录的依据一致。

判断方式：结构化判断 + 大模型语义判断，必要时人工复核

## 七、明确不属于本文件的内容

以下内容不属于栏位质量标准：

- 模板是否发布；
- 医生和机构权限；
- 病历状态流转；
- 操作人和修改原因；
- 审计留痕；
- 保存、提交、确认、归档的工作流约束；
- 某一次病历的具体检查结果。

这些内容由模板治理、权限、工作流、审计和质检结果模块分别负责。','enabled',0,0) ON CONFLICT (tenant_id,code) DO UPDATE SET name=EXCLUDED.name,specialty=EXCLUDED.specialty,content_category=EXCLUDED.content_category,input_type=EXCLUDED.input_type,structure_json=EXCLUDED.structure_json,options_json=EXCLUDED.options_json,description=EXCLUDED.description,status=EXCLUDED.status,updated_by=EXCLUDED.updated_by,updated_at=NOW();
INSERT INTO public.emr_fields (tenant_id,code,name,specialty,content_category,input_type,structure_json,options_json,description,status,created_by,updated_by) VALUES (0,'personal_history','个人史',NULL,'病史','长文本','{}'::jsonb,'{}'::jsonb,'personal_history：个人史

适用范围：通用

质量要求：

- 根据实际询问记录吸烟、饮酒、职业、生活习惯等信息。
- 只记录对话中明确出现的内容。
- 未询问的项目保持未记录。
- 不得根据年龄、性别或疾病推测生活习惯。

病历生成要求：

- 只提取明确出现的职业、吸烟、饮酒、生活习惯和暴露信息。
- 未出现的信息留空，不生成默认结论。

可执行检查类型：非空、语义、人工判断','enabled',0,0) ON CONFLICT (tenant_id,code) DO UPDATE SET name=EXCLUDED.name,specialty=EXCLUDED.specialty,content_category=EXCLUDED.content_category,input_type=EXCLUDED.input_type,structure_json=EXCLUDED.structure_json,options_json=EXCLUDED.options_json,description=EXCLUDED.description,status=EXCLUDED.status,updated_by=EXCLUDED.updated_by,updated_at=NOW();
INSERT INTO public.emr_fields (tenant_id,code,name,specialty,content_category,input_type,structure_json,options_json,description,status,created_by,updated_by) VALUES (0,'physical_exam','体格检查',NULL,'诊疗依据','结构化内容','{"vitals":["temperature","blood_pressure","pulse","respiration","blood_glucose"],"metrics":["height","weight","bmi"],"text_items":["general_exam","positive_findings","necessary_negative_findings","specialty_exam","supplement"]}'::jsonb,'{}'::jsonb,'记录生命体征、一般状态、阳性体征、必要阴性体征和专科查体。','enabled',0,0) ON CONFLICT (tenant_id,code) DO UPDATE SET name=EXCLUDED.name,specialty=EXCLUDED.specialty,content_category=EXCLUDED.content_category,input_type=EXCLUDED.input_type,structure_json=EXCLUDED.structure_json,options_json=EXCLUDED.options_json,description=EXCLUDED.description,status=EXCLUDED.status,updated_by=EXCLUDED.updated_by,updated_at=NOW();
INSERT INTO public.emr_fields (tenant_id,code,name,specialty,content_category,input_type,structure_json,options_json,description,status,created_by,updated_by) VALUES (0,'prescription','处方',NULL,'医疗措施','长文本','{}'::jsonb,'{}'::jsonb,'prescription：处方

适用范围：通用

质量要求：

- 应记录药品或治疗项目、剂量、频次、用法和疗程。
- 缺少具体信息时保留缺失，不得补齐。
- 不能将医生提到的药物示例误写成正式处方。
- 处方应与过敏史、诊断和医嘱保持一致。
- 外用、口服等用法必须有明确依据。

病历生成要求：

- 分别提取药品名称、规格、用法、用量、频次、疗程和数量。
- 区分正式开具、建议使用和举例说明。
- 任何缺失的处方信息保持缺失，不自行补全。

可执行检查类型：结构完整、格式、语义、栏位关系、人工判断','enabled',0,0) ON CONFLICT (tenant_id,code) DO UPDATE SET name=EXCLUDED.name,specialty=EXCLUDED.specialty,content_category=EXCLUDED.content_category,input_type=EXCLUDED.input_type,structure_json=EXCLUDED.structure_json,options_json=EXCLUDED.options_json,description=EXCLUDED.description,status=EXCLUDED.status,updated_by=EXCLUDED.updated_by,updated_at=NOW();
INSERT INTO public.emr_fields (tenant_id,code,name,specialty,content_category,input_type,structure_json,options_json,description,status,created_by,updated_by) VALUES (0,'present_illness','现病史',NULL,'病史','长文本','{}'::jsonb,'{}'::jsonb,'present_illness：现病史

适用范围：通用

质量要求：

- 应记录起病时间、症状发展和伴随症状。
- 应保留患者明确否认的重要症状。
- 应记录既往处理和效果。
- 应区分患者陈述、医生判断和未明确信息。
- 没有询问的内容不能写成“无”。
- 不得补充转写中没有出现的病情。

病历生成要求：

- 按起病与演变、伴随症状、既往处理及效果组织内容。
- 保留明确的阳性和阴性信息。
- 未询问、未回答和无法确定的信息分别表达。
- 不把医生提问自动当成患者肯定回答。

可执行检查类型：非空、结构完整、语义、栏位关系

本栏位相关的多栏位联合质量要求：

### `present_illness` + `physical_exam`

要求：

- 体格检查不能由症状自动推导。
- 阳性体征应有明确检查依据。
- 患者自述症状与医生实际检查所见必须区分。

判断方式：大模型语义判断，必要时人工复核','enabled',0,0) ON CONFLICT (tenant_id,code) DO UPDATE SET name=EXCLUDED.name,specialty=EXCLUDED.specialty,content_category=EXCLUDED.content_category,input_type=EXCLUDED.input_type,structure_json=EXCLUDED.structure_json,options_json=EXCLUDED.options_json,description=EXCLUDED.description,status=EXCLUDED.status,updated_by=EXCLUDED.updated_by,updated_at=NOW();
INSERT INTO public.emr_fields (tenant_id,code,name,specialty,content_category,input_type,structure_json,options_json,description,status,created_by,updated_by) VALUES (0,'supplement','补充说明',NULL,'医疗措施','长文本','{}'::jsonb,'{}'::jsonb,'supplement：补充说明

适用范围：通用

质量要求：

- 记录无法归入其他栏位但确有依据的补充事实。
- 不得重复大段内容。
- 不得用于隐藏诊断、处方或医嘱。
- 不得加入模型推测和未经确认的信息。

病历生成要求：

- 仅保留无法归入其他栏位且确有保存必要的事实。
- 优先将内容归入正确栏位，不用补充说明替代正式栏位。

可执行检查类型：非空、语义、人工判断

## 二、妇科栏位','enabled',0,0) ON CONFLICT (tenant_id,code) DO UPDATE SET name=EXCLUDED.name,specialty=EXCLUDED.specialty,content_category=EXCLUDED.content_category,input_type=EXCLUDED.input_type,structure_json=EXCLUDED.structure_json,options_json=EXCLUDED.options_json,description=EXCLUDED.description,status=EXCLUDED.status,updated_by=EXCLUDED.updated_by,updated_at=NOW();
INSERT INTO public.emr_fields (tenant_id,code,name,specialty,content_category,input_type,structure_json,options_json,description,status,created_by,updated_by) VALUES (0,'tcm_auscultation_olfaction','闻诊','中医','诊疗依据','长文本','{}'::jsonb,'{}'::jsonb,'tcm_auscultation_olfaction：闻诊

适用范围：中医

质量要求：

- 记录声音、气味等实际信息。
- 未观察不能写成正常。
- 不得根据疾病推测相关表现。

病历生成要求：

- 只提取明确的声音和气味观察结果。
- 未观察时留空。

可执行检查类型：非空、语义、人工判断','enabled',0,0) ON CONFLICT (tenant_id,code) DO UPDATE SET name=EXCLUDED.name,specialty=EXCLUDED.specialty,content_category=EXCLUDED.content_category,input_type=EXCLUDED.input_type,structure_json=EXCLUDED.structure_json,options_json=EXCLUDED.options_json,description=EXCLUDED.description,status=EXCLUDED.status,updated_by=EXCLUDED.updated_by,updated_at=NOW();
INSERT INTO public.emr_fields (tenant_id,code,name,specialty,content_category,input_type,structure_json,options_json,description,status,created_by,updated_by) VALUES (0,'tcm_diagnosis','中医诊断','中医','医疗判断','长文本','{}'::jsonb,'{}'::jsonb,'tcm_diagnosis：中医诊断

适用范围：中医

质量要求：

- 记录明确的中医病名或诊断。
- 不得把证候推测成正式诊断。
- 应与四诊、辨证和治法一致。
- 不明确时保留待确认状态。

病历生成要求：

- 保留医生明确提出的中医病名和诊断。
- 不从症状自行生成中医诊断。

可执行检查类型：非空、语义、栏位关系、人工判断

本栏位相关的多栏位联合质量要求：

### 中医联合要求

涉及栏位：

- `tcm_diagnosis`
- `tcm_four_examinations`
- `tcm_pattern_differentiation`
- `tcm_treatment_principle`

要求：

- 中医诊断、四诊、辨证和治法应保持一致。
- 不得以模型推理替代医生明确的辨证和治法。
- 四诊信息不足时，相关结论应标记无法确认。

判断方式：大模型语义判断，必要时人工复核','enabled',0,0) ON CONFLICT (tenant_id,code) DO UPDATE SET name=EXCLUDED.name,specialty=EXCLUDED.specialty,content_category=EXCLUDED.content_category,input_type=EXCLUDED.input_type,structure_json=EXCLUDED.structure_json,options_json=EXCLUDED.options_json,description=EXCLUDED.description,status=EXCLUDED.status,updated_by=EXCLUDED.updated_by,updated_at=NOW();
INSERT INTO public.emr_fields (tenant_id,code,name,specialty,content_category,input_type,structure_json,options_json,description,status,created_by,updated_by) VALUES (0,'tcm_four_examinations','中医四诊','中医','诊疗依据','结构化内容','{}'::jsonb,'{}'::jsonb,'tcm_four_examinations：中医四诊

适用范围：中医

质量要求：

- 应能反映望、闻、问、切四类信息。
- 只记录实际询问或观察到的内容。
- 未涉及的诊法不能自动补全。
- 不得将普通症状强行转换为中医术语。

病历生成要求：

- 分别组织望诊、闻诊、问诊和切诊信息。
- 未涉及的诊法留空，不强行补全。

可执行检查类型：结构完整、语义、栏位关系、人工判断','enabled',0,0) ON CONFLICT (tenant_id,code) DO UPDATE SET name=EXCLUDED.name,specialty=EXCLUDED.specialty,content_category=EXCLUDED.content_category,input_type=EXCLUDED.input_type,structure_json=EXCLUDED.structure_json,options_json=EXCLUDED.options_json,description=EXCLUDED.description,status=EXCLUDED.status,updated_by=EXCLUDED.updated_by,updated_at=NOW();
INSERT INTO public.emr_fields (tenant_id,code,name,specialty,content_category,input_type,structure_json,options_json,description,status,created_by,updated_by) VALUES (0,'tcm_herbal_prescription','中药处方','中医','医疗措施','结构化内容','{}'::jsonb,'{}'::jsonb,'tcm_herbal_prescription：中药处方

适用范围：中医

质量要求：

- 记录药材、剂量、用法、频次和疗程。
- 药材和剂量必须有明确依据。
- 不得根据证候自动补方。
- 应与辨证、治法和过敏史保持一致。

病历生成要求：

- 分别记录药材、剂量、用法、频次和疗程。
- 不根据证候自动生成药材或剂量。

可执行检查类型：结构完整、格式、语义、栏位关系、人工判断

## 四、口腔栏位','enabled',0,0) ON CONFLICT (tenant_id,code) DO UPDATE SET name=EXCLUDED.name,specialty=EXCLUDED.specialty,content_category=EXCLUDED.content_category,input_type=EXCLUDED.input_type,structure_json=EXCLUDED.structure_json,options_json=EXCLUDED.options_json,description=EXCLUDED.description,status=EXCLUDED.status,updated_by=EXCLUDED.updated_by,updated_at=NOW();
INSERT INTO public.emr_fields (tenant_id,code,name,specialty,content_category,input_type,structure_json,options_json,description,status,created_by,updated_by) VALUES (0,'tcm_inquiry','问诊','中医','病史','长文本','{}'::jsonb,'{}'::jsonb,'tcm_inquiry：问诊

适用范围：中医

质量要求：

- 记录中医问诊中明确询问和回答的内容。
- 应保留重要症状、否定信息、寒热、汗出、饮食、睡眠、二便等信息。
- 未询问不能写成否认。
- 不得脱离原话推导证候。

病历生成要求：

- 只整理明确问答内容。
- 不将问诊事实直接改写为中医证候结论。

可执行检查类型：非空、结构完整、语义、栏位关系','enabled',0,0) ON CONFLICT (tenant_id,code) DO UPDATE SET name=EXCLUDED.name,specialty=EXCLUDED.specialty,content_category=EXCLUDED.content_category,input_type=EXCLUDED.input_type,structure_json=EXCLUDED.structure_json,options_json=EXCLUDED.options_json,description=EXCLUDED.description,status=EXCLUDED.status,updated_by=EXCLUDED.updated_by,updated_at=NOW();
INSERT INTO public.emr_fields (tenant_id,code,name,specialty,content_category,input_type,structure_json,options_json,description,status,created_by,updated_by) VALUES (0,'tcm_inspection','望诊','中医','诊疗依据','长文本','{}'::jsonb,'{}'::jsonb,'tcm_inspection：望诊

适用范围：中医

质量要求：

- 记录面色、形态、舌象等实际观察内容。
- 没有观察依据不能填写。
- 不得把患者自述写成望诊结果。

病历生成要求：

- 只记录明确观察到的面色、形态和舌象。
- 区分观察内容和患者自述。

可执行检查类型：非空、语义、栏位关系、人工判断','enabled',0,0) ON CONFLICT (tenant_id,code) DO UPDATE SET name=EXCLUDED.name,specialty=EXCLUDED.specialty,content_category=EXCLUDED.content_category,input_type=EXCLUDED.input_type,structure_json=EXCLUDED.structure_json,options_json=EXCLUDED.options_json,description=EXCLUDED.description,status=EXCLUDED.status,updated_by=EXCLUDED.updated_by,updated_at=NOW();
INSERT INTO public.emr_fields (tenant_id,code,name,specialty,content_category,input_type,structure_json,options_json,description,status,created_by,updated_by) VALUES (0,'tcm_palpation','切诊','中医','诊疗依据','长文本','{}'::jsonb,'{}'::jsonb,'tcm_palpation：切诊

适用范围：中医

质量要求：

- 记录脉象、按诊等实际检查结果。
- 未实施切诊不能填写正常。
- 不得由其他症状推测脉象。

病历生成要求：

- 只记录明确实施切诊后的结果。
- 未实施时留空或标记未检查。

可执行检查类型：非空、语义、人工判断','enabled',0,0) ON CONFLICT (tenant_id,code) DO UPDATE SET name=EXCLUDED.name,specialty=EXCLUDED.specialty,content_category=EXCLUDED.content_category,input_type=EXCLUDED.input_type,structure_json=EXCLUDED.structure_json,options_json=EXCLUDED.options_json,description=EXCLUDED.description,status=EXCLUDED.status,updated_by=EXCLUDED.updated_by,updated_at=NOW();
INSERT INTO public.emr_fields (tenant_id,code,name,specialty,content_category,input_type,structure_json,options_json,description,status,created_by,updated_by) VALUES (0,'tcm_pattern_differentiation','中医辨证','中医','医疗判断','长文本','{}'::jsonb,'{}'::jsonb,'tcm_pattern_differentiation：中医辨证

适用范围：中医

质量要求：

- 记录医生明确提出的证候和辨证依据。
- 应与四诊结果一致。
- 不得由模型自行完成辨证。
- 证候不明确时标记待确认。

病历生成要求：

- 保留医生明确提出的证候和依据。
- 不以模型推理替代医生辨证。

可执行检查类型：非空、语义、栏位关系、人工判断','enabled',0,0) ON CONFLICT (tenant_id,code) DO UPDATE SET name=EXCLUDED.name,specialty=EXCLUDED.specialty,content_category=EXCLUDED.content_category,input_type=EXCLUDED.input_type,structure_json=EXCLUDED.structure_json,options_json=EXCLUDED.options_json,description=EXCLUDED.description,status=EXCLUDED.status,updated_by=EXCLUDED.updated_by,updated_at=NOW();
INSERT INTO public.emr_fields (tenant_id,code,name,specialty,content_category,input_type,structure_json,options_json,description,status,created_by,updated_by) VALUES (0,'tcm_pulse','脉象','中医','诊疗依据','长文本','{}'::jsonb,'{}'::jsonb,'tcm_pulse：脉象

适用范围：中医

质量要求：

- 记录明确的脉象描述。
- 不得根据症状推测脉象。
- 医生未明确判断时不能自动生成。

病历生成要求：

- 只提取医生明确描述的脉象。
- 未切脉或未描述时留空。

可执行检查类型：非空、语义、人工判断','enabled',0,0) ON CONFLICT (tenant_id,code) DO UPDATE SET name=EXCLUDED.name,specialty=EXCLUDED.specialty,content_category=EXCLUDED.content_category,input_type=EXCLUDED.input_type,structure_json=EXCLUDED.structure_json,options_json=EXCLUDED.options_json,description=EXCLUDED.description,status=EXCLUDED.status,updated_by=EXCLUDED.updated_by,updated_at=NOW();
INSERT INTO public.emr_fields (tenant_id,code,name,specialty,content_category,input_type,structure_json,options_json,description,status,created_by,updated_by) VALUES (0,'tcm_tongue','舌象','中医','诊疗依据','结构化内容','{}'::jsonb,'{}'::jsonb,'tcm_tongue：舌象

适用范围：中医

质量要求：

- 记录舌质、舌苔等明确观察结果。
- 必须区分实际观察和模型推测。
- 未观察时留空。

病历生成要求：

- 按舌质、舌苔、湿润程度和其他明确内容组织。
- 不根据症状推测舌象。

可执行检查类型：结构完整、语义、人工判断','enabled',0,0) ON CONFLICT (tenant_id,code) DO UPDATE SET name=EXCLUDED.name,specialty=EXCLUDED.specialty,content_category=EXCLUDED.content_category,input_type=EXCLUDED.input_type,structure_json=EXCLUDED.structure_json,options_json=EXCLUDED.options_json,description=EXCLUDED.description,status=EXCLUDED.status,updated_by=EXCLUDED.updated_by,updated_at=NOW();
INSERT INTO public.emr_fields (tenant_id,code,name,specialty,content_category,input_type,structure_json,options_json,description,status,created_by,updated_by) VALUES (0,'tcm_treatment_principle','治法','中医','医疗判断','长文本','{}'::jsonb,'{}'::jsonb,'tcm_treatment_principle：治法

适用范围：中医

质量要求：

- 记录医生明确提出的治疗原则。
- 应与中医诊断和辨证一致。
- 不得根据诊断自动生成治法。
- 未明确时保持为空。

病历生成要求：

- 只记录医生明确说出的治法。
- 不根据辨证自动补写治疗原则。

可执行检查类型：非空、语义、栏位关系、人工判断','enabled',0,0) ON CONFLICT (tenant_id,code) DO UPDATE SET name=EXCLUDED.name,specialty=EXCLUDED.specialty,content_category=EXCLUDED.content_category,input_type=EXCLUDED.input_type,structure_json=EXCLUDED.structure_json,options_json=EXCLUDED.options_json,description=EXCLUDED.description,status=EXCLUDED.status,updated_by=EXCLUDED.updated_by,updated_at=NOW();
INSERT INTO public.emr_template_fields (tenant_id,template_id,field_id,display_order,is_visible,is_required,description,created_by) SELECT 0,t.id,f.id,10,TRUE,FALSE,'',0 FROM public.emr_templates t CROSS JOIN public.emr_fields f WHERE t.tenant_id=0 AND t.code='pediatrics' AND t.version_no='1.0' AND f.tenant_id=0 AND f.code='chief_complaint' ON CONFLICT (template_id,field_id) DO UPDATE SET display_order=EXCLUDED.display_order,is_visible=EXCLUDED.is_visible,is_required=EXCLUDED.is_required,description=EXCLUDED.description,updated_at=NOW();
INSERT INTO public.emr_template_fields (tenant_id,template_id,field_id,display_order,is_visible,is_required,description,created_by) SELECT 0,t.id,f.id,20,TRUE,FALSE,'',0 FROM public.emr_templates t CROSS JOIN public.emr_fields f WHERE t.tenant_id=0 AND t.code='pediatrics' AND t.version_no='1.0' AND f.tenant_id=0 AND f.code='present_illness' ON CONFLICT (template_id,field_id) DO UPDATE SET display_order=EXCLUDED.display_order,is_visible=EXCLUDED.is_visible,is_required=EXCLUDED.is_required,description=EXCLUDED.description,updated_at=NOW();
INSERT INTO public.emr_template_fields (tenant_id,template_id,field_id,display_order,is_visible,is_required,description,created_by) SELECT 0,t.id,f.id,30,TRUE,FALSE,'',0 FROM public.emr_templates t CROSS JOIN public.emr_fields f WHERE t.tenant_id=0 AND t.code='pediatrics' AND t.version_no='1.0' AND f.tenant_id=0 AND f.code='past_history' ON CONFLICT (template_id,field_id) DO UPDATE SET display_order=EXCLUDED.display_order,is_visible=EXCLUDED.is_visible,is_required=EXCLUDED.is_required,description=EXCLUDED.description,updated_at=NOW();
INSERT INTO public.emr_template_fields (tenant_id,template_id,field_id,display_order,is_visible,is_required,description,created_by) SELECT 0,t.id,f.id,40,TRUE,FALSE,'',0 FROM public.emr_templates t CROSS JOIN public.emr_fields f WHERE t.tenant_id=0 AND t.code='pediatrics' AND t.version_no='1.0' AND f.tenant_id=0 AND f.code='personal_history' ON CONFLICT (template_id,field_id) DO UPDATE SET display_order=EXCLUDED.display_order,is_visible=EXCLUDED.is_visible,is_required=EXCLUDED.is_required,description=EXCLUDED.description,updated_at=NOW();
INSERT INTO public.emr_template_fields (tenant_id,template_id,field_id,display_order,is_visible,is_required,description,created_by) SELECT 0,t.id,f.id,50,TRUE,FALSE,'',0 FROM public.emr_templates t CROSS JOIN public.emr_fields f WHERE t.tenant_id=0 AND t.code='pediatrics' AND t.version_no='1.0' AND f.tenant_id=0 AND f.code='family_history' ON CONFLICT (template_id,field_id) DO UPDATE SET display_order=EXCLUDED.display_order,is_visible=EXCLUDED.is_visible,is_required=EXCLUDED.is_required,description=EXCLUDED.description,updated_at=NOW();
INSERT INTO public.emr_template_fields (tenant_id,template_id,field_id,display_order,is_visible,is_required,description,created_by) SELECT 0,t.id,f.id,60,TRUE,FALSE,'',0 FROM public.emr_templates t CROSS JOIN public.emr_fields f WHERE t.tenant_id=0 AND t.code='pediatrics' AND t.version_no='1.0' AND f.tenant_id=0 AND f.code='allergy_history' ON CONFLICT (template_id,field_id) DO UPDATE SET display_order=EXCLUDED.display_order,is_visible=EXCLUDED.is_visible,is_required=EXCLUDED.is_required,description=EXCLUDED.description,updated_at=NOW();
INSERT INTO public.emr_template_fields (tenant_id,template_id,field_id,display_order,is_visible,is_required,description,created_by) SELECT 0,t.id,f.id,70,TRUE,FALSE,'',0 FROM public.emr_templates t CROSS JOIN public.emr_fields f WHERE t.tenant_id=0 AND t.code='pediatrics' AND t.version_no='1.0' AND f.tenant_id=0 AND f.code='physical_exam' ON CONFLICT (template_id,field_id) DO UPDATE SET display_order=EXCLUDED.display_order,is_visible=EXCLUDED.is_visible,is_required=EXCLUDED.is_required,description=EXCLUDED.description,updated_at=NOW();
INSERT INTO public.emr_template_fields (tenant_id,template_id,field_id,display_order,is_visible,is_required,description,created_by) SELECT 0,t.id,f.id,80,TRUE,FALSE,'',0 FROM public.emr_templates t CROSS JOIN public.emr_fields f WHERE t.tenant_id=0 AND t.code='pediatrics' AND t.version_no='1.0' AND f.tenant_id=0 AND f.code='auxiliary_exam' ON CONFLICT (template_id,field_id) DO UPDATE SET display_order=EXCLUDED.display_order,is_visible=EXCLUDED.is_visible,is_required=EXCLUDED.is_required,description=EXCLUDED.description,updated_at=NOW();
INSERT INTO public.emr_template_fields (tenant_id,template_id,field_id,display_order,is_visible,is_required,description,created_by) SELECT 0,t.id,f.id,90,TRUE,FALSE,'',0 FROM public.emr_templates t CROSS JOIN public.emr_fields f WHERE t.tenant_id=0 AND t.code='pediatrics' AND t.version_no='1.0' AND f.tenant_id=0 AND f.code='diagnosis' ON CONFLICT (template_id,field_id) DO UPDATE SET display_order=EXCLUDED.display_order,is_visible=EXCLUDED.is_visible,is_required=EXCLUDED.is_required,description=EXCLUDED.description,updated_at=NOW();
INSERT INTO public.emr_template_fields (tenant_id,template_id,field_id,display_order,is_visible,is_required,description,created_by) SELECT 0,t.id,f.id,100,TRUE,FALSE,'',0 FROM public.emr_templates t CROSS JOIN public.emr_fields f WHERE t.tenant_id=0 AND t.code='pediatrics' AND t.version_no='1.0' AND f.tenant_id=0 AND f.code='prescription' ON CONFLICT (template_id,field_id) DO UPDATE SET display_order=EXCLUDED.display_order,is_visible=EXCLUDED.is_visible,is_required=EXCLUDED.is_required,description=EXCLUDED.description,updated_at=NOW();
INSERT INTO public.emr_template_fields (tenant_id,template_id,field_id,display_order,is_visible,is_required,description,created_by) SELECT 0,t.id,f.id,110,TRUE,FALSE,'',0 FROM public.emr_templates t CROSS JOIN public.emr_fields f WHERE t.tenant_id=0 AND t.code='pediatrics' AND t.version_no='1.0' AND f.tenant_id=0 AND f.code='disposition' ON CONFLICT (template_id,field_id) DO UPDATE SET display_order=EXCLUDED.display_order,is_visible=EXCLUDED.is_visible,is_required=EXCLUDED.is_required,description=EXCLUDED.description,updated_at=NOW();
INSERT INTO public.emr_template_fields (tenant_id,template_id,field_id,display_order,is_visible,is_required,description,created_by) SELECT 0,t.id,f.id,120,TRUE,FALSE,'',0 FROM public.emr_templates t CROSS JOIN public.emr_fields f WHERE t.tenant_id=0 AND t.code='pediatrics' AND t.version_no='1.0' AND f.tenant_id=0 AND f.code='medical_advice' ON CONFLICT (template_id,field_id) DO UPDATE SET display_order=EXCLUDED.display_order,is_visible=EXCLUDED.is_visible,is_required=EXCLUDED.is_required,description=EXCLUDED.description,updated_at=NOW();
INSERT INTO public.emr_template_fields (tenant_id,template_id,field_id,display_order,is_visible,is_required,description,created_by) SELECT 0,t.id,f.id,130,TRUE,FALSE,'',0 FROM public.emr_templates t CROSS JOIN public.emr_fields f WHERE t.tenant_id=0 AND t.code='pediatrics' AND t.version_no='1.0' AND f.tenant_id=0 AND f.code='followup' ON CONFLICT (template_id,field_id) DO UPDATE SET display_order=EXCLUDED.display_order,is_visible=EXCLUDED.is_visible,is_required=EXCLUDED.is_required,description=EXCLUDED.description,updated_at=NOW();
INSERT INTO public.emr_template_fields (tenant_id,template_id,field_id,display_order,is_visible,is_required,description,created_by) SELECT 0,t.id,f.id,140,TRUE,FALSE,'',0 FROM public.emr_templates t CROSS JOIN public.emr_fields f WHERE t.tenant_id=0 AND t.code='pediatrics' AND t.version_no='1.0' AND f.tenant_id=0 AND f.code='supplement' ON CONFLICT (template_id,field_id) DO UPDATE SET display_order=EXCLUDED.display_order,is_visible=EXCLUDED.is_visible,is_required=EXCLUDED.is_required,description=EXCLUDED.description,updated_at=NOW();
INSERT INTO public.emr_template_fields (tenant_id,template_id,field_id,display_order,is_visible,is_required,description,created_by) SELECT 0,t.id,f.id,610,TRUE,FALSE,'',0 FROM public.emr_templates t CROSS JOIN public.emr_fields f WHERE t.tenant_id=0 AND t.code='pediatrics' AND t.version_no='1.0' AND f.tenant_id=0 AND f.code='pediatric_birth_history' ON CONFLICT (template_id,field_id) DO UPDATE SET display_order=EXCLUDED.display_order,is_visible=EXCLUDED.is_visible,is_required=EXCLUDED.is_required,description=EXCLUDED.description,updated_at=NOW();
INSERT INTO public.emr_template_fields (tenant_id,template_id,field_id,display_order,is_visible,is_required,description,created_by) SELECT 0,t.id,f.id,620,TRUE,FALSE,'',0 FROM public.emr_templates t CROSS JOIN public.emr_fields f WHERE t.tenant_id=0 AND t.code='pediatrics' AND t.version_no='1.0' AND f.tenant_id=0 AND f.code='pediatric_feeding_history' ON CONFLICT (template_id,field_id) DO UPDATE SET display_order=EXCLUDED.display_order,is_visible=EXCLUDED.is_visible,is_required=EXCLUDED.is_required,description=EXCLUDED.description,updated_at=NOW();
INSERT INTO public.emr_template_fields (tenant_id,template_id,field_id,display_order,is_visible,is_required,description,created_by) SELECT 0,t.id,f.id,630,TRUE,FALSE,'',0 FROM public.emr_templates t CROSS JOIN public.emr_fields f WHERE t.tenant_id=0 AND t.code='pediatrics' AND t.version_no='1.0' AND f.tenant_id=0 AND f.code='pediatric_guardian_statement' ON CONFLICT (template_id,field_id) DO UPDATE SET display_order=EXCLUDED.display_order,is_visible=EXCLUDED.is_visible,is_required=EXCLUDED.is_required,description=EXCLUDED.description,updated_at=NOW();
INSERT INTO public.emr_template_fields (tenant_id,template_id,field_id,display_order,is_visible,is_required,description,created_by) SELECT 0,t.id,f.id,640,TRUE,FALSE,'',0 FROM public.emr_templates t CROSS JOIN public.emr_fields f WHERE t.tenant_id=0 AND t.code='pediatrics' AND t.version_no='1.0' AND f.tenant_id=0 AND f.code='pediatric_weight' ON CONFLICT (template_id,field_id) DO UPDATE SET display_order=EXCLUDED.display_order,is_visible=EXCLUDED.is_visible,is_required=EXCLUDED.is_required,description=EXCLUDED.description,updated_at=NOW();
INSERT INTO public.emr_template_fields (tenant_id,template_id,field_id,display_order,is_visible,is_required,description,created_by) SELECT 0,t.id,f.id,650,TRUE,FALSE,'',0 FROM public.emr_templates t CROSS JOIN public.emr_fields f WHERE t.tenant_id=0 AND t.code='pediatrics' AND t.version_no='1.0' AND f.tenant_id=0 AND f.code='pediatric_medication_basis' ON CONFLICT (template_id,field_id) DO UPDATE SET display_order=EXCLUDED.display_order,is_visible=EXCLUDED.is_visible,is_required=EXCLUDED.is_required,description=EXCLUDED.description,updated_at=NOW();
INSERT INTO public.emr_template_fields (tenant_id,template_id,field_id,display_order,is_visible,is_required,description,created_by) SELECT 0,t.id,f.id,10,TRUE,FALSE,'',0 FROM public.emr_templates t CROSS JOIN public.emr_fields f WHERE t.tenant_id=0 AND t.code='internal_medicine' AND t.version_no='1.0' AND f.tenant_id=0 AND f.code='chief_complaint' ON CONFLICT (template_id,field_id) DO UPDATE SET display_order=EXCLUDED.display_order,is_visible=EXCLUDED.is_visible,is_required=EXCLUDED.is_required,description=EXCLUDED.description,updated_at=NOW();
INSERT INTO public.emr_template_fields (tenant_id,template_id,field_id,display_order,is_visible,is_required,description,created_by) SELECT 0,t.id,f.id,20,TRUE,FALSE,'',0 FROM public.emr_templates t CROSS JOIN public.emr_fields f WHERE t.tenant_id=0 AND t.code='internal_medicine' AND t.version_no='1.0' AND f.tenant_id=0 AND f.code='present_illness' ON CONFLICT (template_id,field_id) DO UPDATE SET display_order=EXCLUDED.display_order,is_visible=EXCLUDED.is_visible,is_required=EXCLUDED.is_required,description=EXCLUDED.description,updated_at=NOW();
INSERT INTO public.emr_template_fields (tenant_id,template_id,field_id,display_order,is_visible,is_required,description,created_by) SELECT 0,t.id,f.id,30,TRUE,FALSE,'',0 FROM public.emr_templates t CROSS JOIN public.emr_fields f WHERE t.tenant_id=0 AND t.code='internal_medicine' AND t.version_no='1.0' AND f.tenant_id=0 AND f.code='past_history' ON CONFLICT (template_id,field_id) DO UPDATE SET display_order=EXCLUDED.display_order,is_visible=EXCLUDED.is_visible,is_required=EXCLUDED.is_required,description=EXCLUDED.description,updated_at=NOW();
INSERT INTO public.emr_template_fields (tenant_id,template_id,field_id,display_order,is_visible,is_required,description,created_by) SELECT 0,t.id,f.id,40,TRUE,FALSE,'',0 FROM public.emr_templates t CROSS JOIN public.emr_fields f WHERE t.tenant_id=0 AND t.code='internal_medicine' AND t.version_no='1.0' AND f.tenant_id=0 AND f.code='personal_history' ON CONFLICT (template_id,field_id) DO UPDATE SET display_order=EXCLUDED.display_order,is_visible=EXCLUDED.is_visible,is_required=EXCLUDED.is_required,description=EXCLUDED.description,updated_at=NOW();
INSERT INTO public.emr_template_fields (tenant_id,template_id,field_id,display_order,is_visible,is_required,description,created_by) SELECT 0,t.id,f.id,50,TRUE,FALSE,'',0 FROM public.emr_templates t CROSS JOIN public.emr_fields f WHERE t.tenant_id=0 AND t.code='internal_medicine' AND t.version_no='1.0' AND f.tenant_id=0 AND f.code='family_history' ON CONFLICT (template_id,field_id) DO UPDATE SET display_order=EXCLUDED.display_order,is_visible=EXCLUDED.is_visible,is_required=EXCLUDED.is_required,description=EXCLUDED.description,updated_at=NOW();
INSERT INTO public.emr_template_fields (tenant_id,template_id,field_id,display_order,is_visible,is_required,description,created_by) SELECT 0,t.id,f.id,60,TRUE,FALSE,'',0 FROM public.emr_templates t CROSS JOIN public.emr_fields f WHERE t.tenant_id=0 AND t.code='internal_medicine' AND t.version_no='1.0' AND f.tenant_id=0 AND f.code='allergy_history' ON CONFLICT (template_id,field_id) DO UPDATE SET display_order=EXCLUDED.display_order,is_visible=EXCLUDED.is_visible,is_required=EXCLUDED.is_required,description=EXCLUDED.description,updated_at=NOW();
INSERT INTO public.emr_template_fields (tenant_id,template_id,field_id,display_order,is_visible,is_required,description,created_by) SELECT 0,t.id,f.id,70,TRUE,FALSE,'',0 FROM public.emr_templates t CROSS JOIN public.emr_fields f WHERE t.tenant_id=0 AND t.code='internal_medicine' AND t.version_no='1.0' AND f.tenant_id=0 AND f.code='physical_exam' ON CONFLICT (template_id,field_id) DO UPDATE SET display_order=EXCLUDED.display_order,is_visible=EXCLUDED.is_visible,is_required=EXCLUDED.is_required,description=EXCLUDED.description,updated_at=NOW();
INSERT INTO public.emr_template_fields (tenant_id,template_id,field_id,display_order,is_visible,is_required,description,created_by) SELECT 0,t.id,f.id,80,TRUE,FALSE,'',0 FROM public.emr_templates t CROSS JOIN public.emr_fields f WHERE t.tenant_id=0 AND t.code='internal_medicine' AND t.version_no='1.0' AND f.tenant_id=0 AND f.code='auxiliary_exam' ON CONFLICT (template_id,field_id) DO UPDATE SET display_order=EXCLUDED.display_order,is_visible=EXCLUDED.is_visible,is_required=EXCLUDED.is_required,description=EXCLUDED.description,updated_at=NOW();
INSERT INTO public.emr_template_fields (tenant_id,template_id,field_id,display_order,is_visible,is_required,description,created_by) SELECT 0,t.id,f.id,90,TRUE,FALSE,'',0 FROM public.emr_templates t CROSS JOIN public.emr_fields f WHERE t.tenant_id=0 AND t.code='internal_medicine' AND t.version_no='1.0' AND f.tenant_id=0 AND f.code='diagnosis' ON CONFLICT (template_id,field_id) DO UPDATE SET display_order=EXCLUDED.display_order,is_visible=EXCLUDED.is_visible,is_required=EXCLUDED.is_required,description=EXCLUDED.description,updated_at=NOW();
INSERT INTO public.emr_template_fields (tenant_id,template_id,field_id,display_order,is_visible,is_required,description,created_by) SELECT 0,t.id,f.id,100,TRUE,FALSE,'',0 FROM public.emr_templates t CROSS JOIN public.emr_fields f WHERE t.tenant_id=0 AND t.code='internal_medicine' AND t.version_no='1.0' AND f.tenant_id=0 AND f.code='prescription' ON CONFLICT (template_id,field_id) DO UPDATE SET display_order=EXCLUDED.display_order,is_visible=EXCLUDED.is_visible,is_required=EXCLUDED.is_required,description=EXCLUDED.description,updated_at=NOW();
INSERT INTO public.emr_template_fields (tenant_id,template_id,field_id,display_order,is_visible,is_required,description,created_by) SELECT 0,t.id,f.id,110,TRUE,FALSE,'',0 FROM public.emr_templates t CROSS JOIN public.emr_fields f WHERE t.tenant_id=0 AND t.code='internal_medicine' AND t.version_no='1.0' AND f.tenant_id=0 AND f.code='disposition' ON CONFLICT (template_id,field_id) DO UPDATE SET display_order=EXCLUDED.display_order,is_visible=EXCLUDED.is_visible,is_required=EXCLUDED.is_required,description=EXCLUDED.description,updated_at=NOW();
INSERT INTO public.emr_template_fields (tenant_id,template_id,field_id,display_order,is_visible,is_required,description,created_by) SELECT 0,t.id,f.id,120,TRUE,FALSE,'',0 FROM public.emr_templates t CROSS JOIN public.emr_fields f WHERE t.tenant_id=0 AND t.code='internal_medicine' AND t.version_no='1.0' AND f.tenant_id=0 AND f.code='medical_advice' ON CONFLICT (template_id,field_id) DO UPDATE SET display_order=EXCLUDED.display_order,is_visible=EXCLUDED.is_visible,is_required=EXCLUDED.is_required,description=EXCLUDED.description,updated_at=NOW();
INSERT INTO public.emr_template_fields (tenant_id,template_id,field_id,display_order,is_visible,is_required,description,created_by) SELECT 0,t.id,f.id,130,TRUE,FALSE,'',0 FROM public.emr_templates t CROSS JOIN public.emr_fields f WHERE t.tenant_id=0 AND t.code='internal_medicine' AND t.version_no='1.0' AND f.tenant_id=0 AND f.code='followup' ON CONFLICT (template_id,field_id) DO UPDATE SET display_order=EXCLUDED.display_order,is_visible=EXCLUDED.is_visible,is_required=EXCLUDED.is_required,description=EXCLUDED.description,updated_at=NOW();
INSERT INTO public.emr_template_fields (tenant_id,template_id,field_id,display_order,is_visible,is_required,description,created_by) SELECT 0,t.id,f.id,140,TRUE,FALSE,'',0 FROM public.emr_templates t CROSS JOIN public.emr_fields f WHERE t.tenant_id=0 AND t.code='internal_medicine' AND t.version_no='1.0' AND f.tenant_id=0 AND f.code='supplement' ON CONFLICT (template_id,field_id) DO UPDATE SET display_order=EXCLUDED.display_order,is_visible=EXCLUDED.is_visible,is_required=EXCLUDED.is_required,description=EXCLUDED.description,updated_at=NOW();
INSERT INTO public.emr_template_fields (tenant_id,template_id,field_id,display_order,is_visible,is_required,description,created_by) SELECT 0,t.id,f.id,10,TRUE,FALSE,'',0 FROM public.emr_templates t CROSS JOIN public.emr_fields f WHERE t.tenant_id=0 AND t.code='gynecology' AND t.version_no='1.0' AND f.tenant_id=0 AND f.code='chief_complaint' ON CONFLICT (template_id,field_id) DO UPDATE SET display_order=EXCLUDED.display_order,is_visible=EXCLUDED.is_visible,is_required=EXCLUDED.is_required,description=EXCLUDED.description,updated_at=NOW();
INSERT INTO public.emr_template_fields (tenant_id,template_id,field_id,display_order,is_visible,is_required,description,created_by) SELECT 0,t.id,f.id,20,TRUE,FALSE,'',0 FROM public.emr_templates t CROSS JOIN public.emr_fields f WHERE t.tenant_id=0 AND t.code='gynecology' AND t.version_no='1.0' AND f.tenant_id=0 AND f.code='present_illness' ON CONFLICT (template_id,field_id) DO UPDATE SET display_order=EXCLUDED.display_order,is_visible=EXCLUDED.is_visible,is_required=EXCLUDED.is_required,description=EXCLUDED.description,updated_at=NOW();
INSERT INTO public.emr_template_fields (tenant_id,template_id,field_id,display_order,is_visible,is_required,description,created_by) SELECT 0,t.id,f.id,30,TRUE,FALSE,'',0 FROM public.emr_templates t CROSS JOIN public.emr_fields f WHERE t.tenant_id=0 AND t.code='gynecology' AND t.version_no='1.0' AND f.tenant_id=0 AND f.code='past_history' ON CONFLICT (template_id,field_id) DO UPDATE SET display_order=EXCLUDED.display_order,is_visible=EXCLUDED.is_visible,is_required=EXCLUDED.is_required,description=EXCLUDED.description,updated_at=NOW();
INSERT INTO public.emr_template_fields (tenant_id,template_id,field_id,display_order,is_visible,is_required,description,created_by) SELECT 0,t.id,f.id,40,TRUE,FALSE,'',0 FROM public.emr_templates t CROSS JOIN public.emr_fields f WHERE t.tenant_id=0 AND t.code='gynecology' AND t.version_no='1.0' AND f.tenant_id=0 AND f.code='personal_history' ON CONFLICT (template_id,field_id) DO UPDATE SET display_order=EXCLUDED.display_order,is_visible=EXCLUDED.is_visible,is_required=EXCLUDED.is_required,description=EXCLUDED.description,updated_at=NOW();
INSERT INTO public.emr_template_fields (tenant_id,template_id,field_id,display_order,is_visible,is_required,description,created_by) SELECT 0,t.id,f.id,50,TRUE,FALSE,'',0 FROM public.emr_templates t CROSS JOIN public.emr_fields f WHERE t.tenant_id=0 AND t.code='gynecology' AND t.version_no='1.0' AND f.tenant_id=0 AND f.code='family_history' ON CONFLICT (template_id,field_id) DO UPDATE SET display_order=EXCLUDED.display_order,is_visible=EXCLUDED.is_visible,is_required=EXCLUDED.is_required,description=EXCLUDED.description,updated_at=NOW();
INSERT INTO public.emr_template_fields (tenant_id,template_id,field_id,display_order,is_visible,is_required,description,created_by) SELECT 0,t.id,f.id,60,TRUE,FALSE,'',0 FROM public.emr_templates t CROSS JOIN public.emr_fields f WHERE t.tenant_id=0 AND t.code='gynecology' AND t.version_no='1.0' AND f.tenant_id=0 AND f.code='allergy_history' ON CONFLICT (template_id,field_id) DO UPDATE SET display_order=EXCLUDED.display_order,is_visible=EXCLUDED.is_visible,is_required=EXCLUDED.is_required,description=EXCLUDED.description,updated_at=NOW();
INSERT INTO public.emr_template_fields (tenant_id,template_id,field_id,display_order,is_visible,is_required,description,created_by) SELECT 0,t.id,f.id,70,TRUE,FALSE,'',0 FROM public.emr_templates t CROSS JOIN public.emr_fields f WHERE t.tenant_id=0 AND t.code='gynecology' AND t.version_no='1.0' AND f.tenant_id=0 AND f.code='physical_exam' ON CONFLICT (template_id,field_id) DO UPDATE SET display_order=EXCLUDED.display_order,is_visible=EXCLUDED.is_visible,is_required=EXCLUDED.is_required,description=EXCLUDED.description,updated_at=NOW();
INSERT INTO public.emr_template_fields (tenant_id,template_id,field_id,display_order,is_visible,is_required,description,created_by) SELECT 0,t.id,f.id,80,TRUE,FALSE,'',0 FROM public.emr_templates t CROSS JOIN public.emr_fields f WHERE t.tenant_id=0 AND t.code='gynecology' AND t.version_no='1.0' AND f.tenant_id=0 AND f.code='auxiliary_exam' ON CONFLICT (template_id,field_id) DO UPDATE SET display_order=EXCLUDED.display_order,is_visible=EXCLUDED.is_visible,is_required=EXCLUDED.is_required,description=EXCLUDED.description,updated_at=NOW();
INSERT INTO public.emr_template_fields (tenant_id,template_id,field_id,display_order,is_visible,is_required,description,created_by) SELECT 0,t.id,f.id,90,TRUE,FALSE,'',0 FROM public.emr_templates t CROSS JOIN public.emr_fields f WHERE t.tenant_id=0 AND t.code='gynecology' AND t.version_no='1.0' AND f.tenant_id=0 AND f.code='diagnosis' ON CONFLICT (template_id,field_id) DO UPDATE SET display_order=EXCLUDED.display_order,is_visible=EXCLUDED.is_visible,is_required=EXCLUDED.is_required,description=EXCLUDED.description,updated_at=NOW();
INSERT INTO public.emr_template_fields (tenant_id,template_id,field_id,display_order,is_visible,is_required,description,created_by) SELECT 0,t.id,f.id,100,TRUE,FALSE,'',0 FROM public.emr_templates t CROSS JOIN public.emr_fields f WHERE t.tenant_id=0 AND t.code='gynecology' AND t.version_no='1.0' AND f.tenant_id=0 AND f.code='prescription' ON CONFLICT (template_id,field_id) DO UPDATE SET display_order=EXCLUDED.display_order,is_visible=EXCLUDED.is_visible,is_required=EXCLUDED.is_required,description=EXCLUDED.description,updated_at=NOW();
INSERT INTO public.emr_template_fields (tenant_id,template_id,field_id,display_order,is_visible,is_required,description,created_by) SELECT 0,t.id,f.id,110,TRUE,FALSE,'',0 FROM public.emr_templates t CROSS JOIN public.emr_fields f WHERE t.tenant_id=0 AND t.code='gynecology' AND t.version_no='1.0' AND f.tenant_id=0 AND f.code='disposition' ON CONFLICT (template_id,field_id) DO UPDATE SET display_order=EXCLUDED.display_order,is_visible=EXCLUDED.is_visible,is_required=EXCLUDED.is_required,description=EXCLUDED.description,updated_at=NOW();
INSERT INTO public.emr_template_fields (tenant_id,template_id,field_id,display_order,is_visible,is_required,description,created_by) SELECT 0,t.id,f.id,120,TRUE,FALSE,'',0 FROM public.emr_templates t CROSS JOIN public.emr_fields f WHERE t.tenant_id=0 AND t.code='gynecology' AND t.version_no='1.0' AND f.tenant_id=0 AND f.code='medical_advice' ON CONFLICT (template_id,field_id) DO UPDATE SET display_order=EXCLUDED.display_order,is_visible=EXCLUDED.is_visible,is_required=EXCLUDED.is_required,description=EXCLUDED.description,updated_at=NOW();
INSERT INTO public.emr_template_fields (tenant_id,template_id,field_id,display_order,is_visible,is_required,description,created_by) SELECT 0,t.id,f.id,130,TRUE,FALSE,'',0 FROM public.emr_templates t CROSS JOIN public.emr_fields f WHERE t.tenant_id=0 AND t.code='gynecology' AND t.version_no='1.0' AND f.tenant_id=0 AND f.code='followup' ON CONFLICT (template_id,field_id) DO UPDATE SET display_order=EXCLUDED.display_order,is_visible=EXCLUDED.is_visible,is_required=EXCLUDED.is_required,description=EXCLUDED.description,updated_at=NOW();
INSERT INTO public.emr_template_fields (tenant_id,template_id,field_id,display_order,is_visible,is_required,description,created_by) SELECT 0,t.id,f.id,140,TRUE,FALSE,'',0 FROM public.emr_templates t CROSS JOIN public.emr_fields f WHERE t.tenant_id=0 AND t.code='gynecology' AND t.version_no='1.0' AND f.tenant_id=0 AND f.code='supplement' ON CONFLICT (template_id,field_id) DO UPDATE SET display_order=EXCLUDED.display_order,is_visible=EXCLUDED.is_visible,is_required=EXCLUDED.is_required,description=EXCLUDED.description,updated_at=NOW();
INSERT INTO public.emr_template_fields (tenant_id,template_id,field_id,display_order,is_visible,is_required,description,created_by) SELECT 0,t.id,f.id,210,TRUE,FALSE,'',0 FROM public.emr_templates t CROSS JOIN public.emr_fields f WHERE t.tenant_id=0 AND t.code='gynecology' AND t.version_no='1.0' AND f.tenant_id=0 AND f.code='menstrual_history' ON CONFLICT (template_id,field_id) DO UPDATE SET display_order=EXCLUDED.display_order,is_visible=EXCLUDED.is_visible,is_required=EXCLUDED.is_required,description=EXCLUDED.description,updated_at=NOW();
INSERT INTO public.emr_template_fields (tenant_id,template_id,field_id,display_order,is_visible,is_required,description,created_by) SELECT 0,t.id,f.id,220,TRUE,FALSE,'',0 FROM public.emr_templates t CROSS JOIN public.emr_fields f WHERE t.tenant_id=0 AND t.code='gynecology' AND t.version_no='1.0' AND f.tenant_id=0 AND f.code='last_menstrual_period' ON CONFLICT (template_id,field_id) DO UPDATE SET display_order=EXCLUDED.display_order,is_visible=EXCLUDED.is_visible,is_required=EXCLUDED.is_required,description=EXCLUDED.description,updated_at=NOW();
INSERT INTO public.emr_template_fields (tenant_id,template_id,field_id,display_order,is_visible,is_required,description,created_by) SELECT 0,t.id,f.id,230,TRUE,FALSE,'',0 FROM public.emr_templates t CROSS JOIN public.emr_fields f WHERE t.tenant_id=0 AND t.code='gynecology' AND t.version_no='1.0' AND f.tenant_id=0 AND f.code='marital_reproductive_history' ON CONFLICT (template_id,field_id) DO UPDATE SET display_order=EXCLUDED.display_order,is_visible=EXCLUDED.is_visible,is_required=EXCLUDED.is_required,description=EXCLUDED.description,updated_at=NOW();
INSERT INTO public.emr_template_fields (tenant_id,template_id,field_id,display_order,is_visible,is_required,description,created_by) SELECT 0,t.id,f.id,240,TRUE,FALSE,'',0 FROM public.emr_templates t CROSS JOIN public.emr_fields f WHERE t.tenant_id=0 AND t.code='gynecology' AND t.version_no='1.0' AND f.tenant_id=0 AND f.code='obstetric_history' ON CONFLICT (template_id,field_id) DO UPDATE SET display_order=EXCLUDED.display_order,is_visible=EXCLUDED.is_visible,is_required=EXCLUDED.is_required,description=EXCLUDED.description,updated_at=NOW();
INSERT INTO public.emr_template_fields (tenant_id,template_id,field_id,display_order,is_visible,is_required,description,created_by) SELECT 0,t.id,f.id,250,TRUE,FALSE,'',0 FROM public.emr_templates t CROSS JOIN public.emr_fields f WHERE t.tenant_id=0 AND t.code='gynecology' AND t.version_no='1.0' AND f.tenant_id=0 AND f.code='gynecology_specialty_exam' ON CONFLICT (template_id,field_id) DO UPDATE SET display_order=EXCLUDED.display_order,is_visible=EXCLUDED.is_visible,is_required=EXCLUDED.is_required,description=EXCLUDED.description,updated_at=NOW();
INSERT INTO public.emr_template_fields (tenant_id,template_id,field_id,display_order,is_visible,is_required,description,created_by) SELECT 0,t.id,f.id,260,TRUE,FALSE,'',0 FROM public.emr_templates t CROSS JOIN public.emr_fields f WHERE t.tenant_id=0 AND t.code='gynecology' AND t.version_no='1.0' AND f.tenant_id=0 AND f.code='gynecology_auxiliary_exam' ON CONFLICT (template_id,field_id) DO UPDATE SET display_order=EXCLUDED.display_order,is_visible=EXCLUDED.is_visible,is_required=EXCLUDED.is_required,description=EXCLUDED.description,updated_at=NOW();
INSERT INTO public.emr_template_fields (tenant_id,template_id,field_id,display_order,is_visible,is_required,description,created_by) SELECT 0,t.id,f.id,10,TRUE,FALSE,'主诉',0 FROM public.emr_templates t CROSS JOIN public.emr_fields f WHERE t.tenant_id=0 AND t.code='standard_outpatient' AND t.version_no='1.0' AND f.tenant_id=0 AND f.code='chief_complaint' ON CONFLICT (template_id,field_id) DO UPDATE SET display_order=EXCLUDED.display_order,is_visible=EXCLUDED.is_visible,is_required=EXCLUDED.is_required,description=EXCLUDED.description,updated_at=NOW();
INSERT INTO public.emr_template_fields (tenant_id,template_id,field_id,display_order,is_visible,is_required,description,created_by) SELECT 0,t.id,f.id,20,TRUE,FALSE,'现病史',0 FROM public.emr_templates t CROSS JOIN public.emr_fields f WHERE t.tenant_id=0 AND t.code='standard_outpatient' AND t.version_no='1.0' AND f.tenant_id=0 AND f.code='present_illness' ON CONFLICT (template_id,field_id) DO UPDATE SET display_order=EXCLUDED.display_order,is_visible=EXCLUDED.is_visible,is_required=EXCLUDED.is_required,description=EXCLUDED.description,updated_at=NOW();
INSERT INTO public.emr_template_fields (tenant_id,template_id,field_id,display_order,is_visible,is_required,description,created_by) SELECT 0,t.id,f.id,30,TRUE,FALSE,'既往史',0 FROM public.emr_templates t CROSS JOIN public.emr_fields f WHERE t.tenant_id=0 AND t.code='standard_outpatient' AND t.version_no='1.0' AND f.tenant_id=0 AND f.code='past_history' ON CONFLICT (template_id,field_id) DO UPDATE SET display_order=EXCLUDED.display_order,is_visible=EXCLUDED.is_visible,is_required=EXCLUDED.is_required,description=EXCLUDED.description,updated_at=NOW();
INSERT INTO public.emr_template_fields (tenant_id,template_id,field_id,display_order,is_visible,is_required,description,created_by) SELECT 0,t.id,f.id,40,TRUE,FALSE,'个人史',0 FROM public.emr_templates t CROSS JOIN public.emr_fields f WHERE t.tenant_id=0 AND t.code='standard_outpatient' AND t.version_no='1.0' AND f.tenant_id=0 AND f.code='personal_history' ON CONFLICT (template_id,field_id) DO UPDATE SET display_order=EXCLUDED.display_order,is_visible=EXCLUDED.is_visible,is_required=EXCLUDED.is_required,description=EXCLUDED.description,updated_at=NOW();
INSERT INTO public.emr_template_fields (tenant_id,template_id,field_id,display_order,is_visible,is_required,description,created_by) SELECT 0,t.id,f.id,50,TRUE,FALSE,'家族史',0 FROM public.emr_templates t CROSS JOIN public.emr_fields f WHERE t.tenant_id=0 AND t.code='standard_outpatient' AND t.version_no='1.0' AND f.tenant_id=0 AND f.code='family_history' ON CONFLICT (template_id,field_id) DO UPDATE SET display_order=EXCLUDED.display_order,is_visible=EXCLUDED.is_visible,is_required=EXCLUDED.is_required,description=EXCLUDED.description,updated_at=NOW();
INSERT INTO public.emr_template_fields (tenant_id,template_id,field_id,display_order,is_visible,is_required,description,created_by) SELECT 0,t.id,f.id,60,TRUE,FALSE,'过敏史',0 FROM public.emr_templates t CROSS JOIN public.emr_fields f WHERE t.tenant_id=0 AND t.code='standard_outpatient' AND t.version_no='1.0' AND f.tenant_id=0 AND f.code='allergy_history' ON CONFLICT (template_id,field_id) DO UPDATE SET display_order=EXCLUDED.display_order,is_visible=EXCLUDED.is_visible,is_required=EXCLUDED.is_required,description=EXCLUDED.description,updated_at=NOW();
INSERT INTO public.emr_template_fields (tenant_id,template_id,field_id,display_order,is_visible,is_required,description,created_by) SELECT 0,t.id,f.id,70,TRUE,FALSE,'体格检查',0 FROM public.emr_templates t CROSS JOIN public.emr_fields f WHERE t.tenant_id=0 AND t.code='standard_outpatient' AND t.version_no='1.0' AND f.tenant_id=0 AND f.code='physical_exam' ON CONFLICT (template_id,field_id) DO UPDATE SET display_order=EXCLUDED.display_order,is_visible=EXCLUDED.is_visible,is_required=EXCLUDED.is_required,description=EXCLUDED.description,updated_at=NOW();
INSERT INTO public.emr_template_fields (tenant_id,template_id,field_id,display_order,is_visible,is_required,description,created_by) SELECT 0,t.id,f.id,80,TRUE,FALSE,'辅助检查',0 FROM public.emr_templates t CROSS JOIN public.emr_fields f WHERE t.tenant_id=0 AND t.code='standard_outpatient' AND t.version_no='1.0' AND f.tenant_id=0 AND f.code='auxiliary_exam' ON CONFLICT (template_id,field_id) DO UPDATE SET display_order=EXCLUDED.display_order,is_visible=EXCLUDED.is_visible,is_required=EXCLUDED.is_required,description=EXCLUDED.description,updated_at=NOW();
INSERT INTO public.emr_template_fields (tenant_id,template_id,field_id,display_order,is_visible,is_required,description,created_by) SELECT 0,t.id,f.id,90,TRUE,FALSE,'诊断',0 FROM public.emr_templates t CROSS JOIN public.emr_fields f WHERE t.tenant_id=0 AND t.code='standard_outpatient' AND t.version_no='1.0' AND f.tenant_id=0 AND f.code='diagnosis' ON CONFLICT (template_id,field_id) DO UPDATE SET display_order=EXCLUDED.display_order,is_visible=EXCLUDED.is_visible,is_required=EXCLUDED.is_required,description=EXCLUDED.description,updated_at=NOW();
INSERT INTO public.emr_template_fields (tenant_id,template_id,field_id,display_order,is_visible,is_required,description,created_by) SELECT 0,t.id,f.id,100,TRUE,FALSE,'处方',0 FROM public.emr_templates t CROSS JOIN public.emr_fields f WHERE t.tenant_id=0 AND t.code='standard_outpatient' AND t.version_no='1.0' AND f.tenant_id=0 AND f.code='prescription' ON CONFLICT (template_id,field_id) DO UPDATE SET display_order=EXCLUDED.display_order,is_visible=EXCLUDED.is_visible,is_required=EXCLUDED.is_required,description=EXCLUDED.description,updated_at=NOW();
INSERT INTO public.emr_template_fields (tenant_id,template_id,field_id,display_order,is_visible,is_required,description,created_by) SELECT 0,t.id,f.id,110,TRUE,FALSE,'处置',0 FROM public.emr_templates t CROSS JOIN public.emr_fields f WHERE t.tenant_id=0 AND t.code='standard_outpatient' AND t.version_no='1.0' AND f.tenant_id=0 AND f.code='disposition' ON CONFLICT (template_id,field_id) DO UPDATE SET display_order=EXCLUDED.display_order,is_visible=EXCLUDED.is_visible,is_required=EXCLUDED.is_required,description=EXCLUDED.description,updated_at=NOW();
INSERT INTO public.emr_template_fields (tenant_id,template_id,field_id,display_order,is_visible,is_required,description,created_by) SELECT 0,t.id,f.id,120,TRUE,FALSE,'医嘱',0 FROM public.emr_templates t CROSS JOIN public.emr_fields f WHERE t.tenant_id=0 AND t.code='standard_outpatient' AND t.version_no='1.0' AND f.tenant_id=0 AND f.code='medical_advice' ON CONFLICT (template_id,field_id) DO UPDATE SET display_order=EXCLUDED.display_order,is_visible=EXCLUDED.is_visible,is_required=EXCLUDED.is_required,description=EXCLUDED.description,updated_at=NOW();
INSERT INTO public.emr_template_fields (tenant_id,template_id,field_id,display_order,is_visible,is_required,description,created_by) SELECT 0,t.id,f.id,130,TRUE,FALSE,'复诊安排',0 FROM public.emr_templates t CROSS JOIN public.emr_fields f WHERE t.tenant_id=0 AND t.code='standard_outpatient' AND t.version_no='1.0' AND f.tenant_id=0 AND f.code='followup' ON CONFLICT (template_id,field_id) DO UPDATE SET display_order=EXCLUDED.display_order,is_visible=EXCLUDED.is_visible,is_required=EXCLUDED.is_required,description=EXCLUDED.description,updated_at=NOW();
INSERT INTO public.emr_template_fields (tenant_id,template_id,field_id,display_order,is_visible,is_required,description,created_by) SELECT 0,t.id,f.id,140,TRUE,FALSE,'补充说明',0 FROM public.emr_templates t CROSS JOIN public.emr_fields f WHERE t.tenant_id=0 AND t.code='standard_outpatient' AND t.version_no='1.0' AND f.tenant_id=0 AND f.code='supplement' ON CONFLICT (template_id,field_id) DO UPDATE SET display_order=EXCLUDED.display_order,is_visible=EXCLUDED.is_visible,is_required=EXCLUDED.is_required,description=EXCLUDED.description,updated_at=NOW();
INSERT INTO public.emr_template_fields (tenant_id,template_id,field_id,display_order,is_visible,is_required,description,created_by) SELECT 0,t.id,f.id,10,TRUE,FALSE,'',0 FROM public.emr_templates t CROSS JOIN public.emr_fields f WHERE t.tenant_id=0 AND t.code='dental' AND t.version_no='1.0' AND f.tenant_id=0 AND f.code='chief_complaint' ON CONFLICT (template_id,field_id) DO UPDATE SET display_order=EXCLUDED.display_order,is_visible=EXCLUDED.is_visible,is_required=EXCLUDED.is_required,description=EXCLUDED.description,updated_at=NOW();
INSERT INTO public.emr_template_fields (tenant_id,template_id,field_id,display_order,is_visible,is_required,description,created_by) SELECT 0,t.id,f.id,20,TRUE,FALSE,'',0 FROM public.emr_templates t CROSS JOIN public.emr_fields f WHERE t.tenant_id=0 AND t.code='dental' AND t.version_no='1.0' AND f.tenant_id=0 AND f.code='present_illness' ON CONFLICT (template_id,field_id) DO UPDATE SET display_order=EXCLUDED.display_order,is_visible=EXCLUDED.is_visible,is_required=EXCLUDED.is_required,description=EXCLUDED.description,updated_at=NOW();
INSERT INTO public.emr_template_fields (tenant_id,template_id,field_id,display_order,is_visible,is_required,description,created_by) SELECT 0,t.id,f.id,30,TRUE,FALSE,'',0 FROM public.emr_templates t CROSS JOIN public.emr_fields f WHERE t.tenant_id=0 AND t.code='dental' AND t.version_no='1.0' AND f.tenant_id=0 AND f.code='past_history' ON CONFLICT (template_id,field_id) DO UPDATE SET display_order=EXCLUDED.display_order,is_visible=EXCLUDED.is_visible,is_required=EXCLUDED.is_required,description=EXCLUDED.description,updated_at=NOW();
INSERT INTO public.emr_template_fields (tenant_id,template_id,field_id,display_order,is_visible,is_required,description,created_by) SELECT 0,t.id,f.id,40,TRUE,FALSE,'',0 FROM public.emr_templates t CROSS JOIN public.emr_fields f WHERE t.tenant_id=0 AND t.code='dental' AND t.version_no='1.0' AND f.tenant_id=0 AND f.code='personal_history' ON CONFLICT (template_id,field_id) DO UPDATE SET display_order=EXCLUDED.display_order,is_visible=EXCLUDED.is_visible,is_required=EXCLUDED.is_required,description=EXCLUDED.description,updated_at=NOW();
INSERT INTO public.emr_template_fields (tenant_id,template_id,field_id,display_order,is_visible,is_required,description,created_by) SELECT 0,t.id,f.id,50,TRUE,FALSE,'',0 FROM public.emr_templates t CROSS JOIN public.emr_fields f WHERE t.tenant_id=0 AND t.code='dental' AND t.version_no='1.0' AND f.tenant_id=0 AND f.code='family_history' ON CONFLICT (template_id,field_id) DO UPDATE SET display_order=EXCLUDED.display_order,is_visible=EXCLUDED.is_visible,is_required=EXCLUDED.is_required,description=EXCLUDED.description,updated_at=NOW();
INSERT INTO public.emr_template_fields (tenant_id,template_id,field_id,display_order,is_visible,is_required,description,created_by) SELECT 0,t.id,f.id,60,TRUE,FALSE,'',0 FROM public.emr_templates t CROSS JOIN public.emr_fields f WHERE t.tenant_id=0 AND t.code='dental' AND t.version_no='1.0' AND f.tenant_id=0 AND f.code='allergy_history' ON CONFLICT (template_id,field_id) DO UPDATE SET display_order=EXCLUDED.display_order,is_visible=EXCLUDED.is_visible,is_required=EXCLUDED.is_required,description=EXCLUDED.description,updated_at=NOW();
INSERT INTO public.emr_template_fields (tenant_id,template_id,field_id,display_order,is_visible,is_required,description,created_by) SELECT 0,t.id,f.id,70,TRUE,FALSE,'',0 FROM public.emr_templates t CROSS JOIN public.emr_fields f WHERE t.tenant_id=0 AND t.code='dental' AND t.version_no='1.0' AND f.tenant_id=0 AND f.code='physical_exam' ON CONFLICT (template_id,field_id) DO UPDATE SET display_order=EXCLUDED.display_order,is_visible=EXCLUDED.is_visible,is_required=EXCLUDED.is_required,description=EXCLUDED.description,updated_at=NOW();
INSERT INTO public.emr_template_fields (tenant_id,template_id,field_id,display_order,is_visible,is_required,description,created_by) SELECT 0,t.id,f.id,80,TRUE,FALSE,'',0 FROM public.emr_templates t CROSS JOIN public.emr_fields f WHERE t.tenant_id=0 AND t.code='dental' AND t.version_no='1.0' AND f.tenant_id=0 AND f.code='auxiliary_exam' ON CONFLICT (template_id,field_id) DO UPDATE SET display_order=EXCLUDED.display_order,is_visible=EXCLUDED.is_visible,is_required=EXCLUDED.is_required,description=EXCLUDED.description,updated_at=NOW();
INSERT INTO public.emr_template_fields (tenant_id,template_id,field_id,display_order,is_visible,is_required,description,created_by) SELECT 0,t.id,f.id,90,TRUE,FALSE,'',0 FROM public.emr_templates t CROSS JOIN public.emr_fields f WHERE t.tenant_id=0 AND t.code='dental' AND t.version_no='1.0' AND f.tenant_id=0 AND f.code='diagnosis' ON CONFLICT (template_id,field_id) DO UPDATE SET display_order=EXCLUDED.display_order,is_visible=EXCLUDED.is_visible,is_required=EXCLUDED.is_required,description=EXCLUDED.description,updated_at=NOW();
INSERT INTO public.emr_template_fields (tenant_id,template_id,field_id,display_order,is_visible,is_required,description,created_by) SELECT 0,t.id,f.id,100,TRUE,FALSE,'',0 FROM public.emr_templates t CROSS JOIN public.emr_fields f WHERE t.tenant_id=0 AND t.code='dental' AND t.version_no='1.0' AND f.tenant_id=0 AND f.code='prescription' ON CONFLICT (template_id,field_id) DO UPDATE SET display_order=EXCLUDED.display_order,is_visible=EXCLUDED.is_visible,is_required=EXCLUDED.is_required,description=EXCLUDED.description,updated_at=NOW();
INSERT INTO public.emr_template_fields (tenant_id,template_id,field_id,display_order,is_visible,is_required,description,created_by) SELECT 0,t.id,f.id,110,TRUE,FALSE,'',0 FROM public.emr_templates t CROSS JOIN public.emr_fields f WHERE t.tenant_id=0 AND t.code='dental' AND t.version_no='1.0' AND f.tenant_id=0 AND f.code='disposition' ON CONFLICT (template_id,field_id) DO UPDATE SET display_order=EXCLUDED.display_order,is_visible=EXCLUDED.is_visible,is_required=EXCLUDED.is_required,description=EXCLUDED.description,updated_at=NOW();
INSERT INTO public.emr_template_fields (tenant_id,template_id,field_id,display_order,is_visible,is_required,description,created_by) SELECT 0,t.id,f.id,120,TRUE,FALSE,'',0 FROM public.emr_templates t CROSS JOIN public.emr_fields f WHERE t.tenant_id=0 AND t.code='dental' AND t.version_no='1.0' AND f.tenant_id=0 AND f.code='medical_advice' ON CONFLICT (template_id,field_id) DO UPDATE SET display_order=EXCLUDED.display_order,is_visible=EXCLUDED.is_visible,is_required=EXCLUDED.is_required,description=EXCLUDED.description,updated_at=NOW();
INSERT INTO public.emr_template_fields (tenant_id,template_id,field_id,display_order,is_visible,is_required,description,created_by) SELECT 0,t.id,f.id,130,TRUE,FALSE,'',0 FROM public.emr_templates t CROSS JOIN public.emr_fields f WHERE t.tenant_id=0 AND t.code='dental' AND t.version_no='1.0' AND f.tenant_id=0 AND f.code='followup' ON CONFLICT (template_id,field_id) DO UPDATE SET display_order=EXCLUDED.display_order,is_visible=EXCLUDED.is_visible,is_required=EXCLUDED.is_required,description=EXCLUDED.description,updated_at=NOW();
INSERT INTO public.emr_template_fields (tenant_id,template_id,field_id,display_order,is_visible,is_required,description,created_by) SELECT 0,t.id,f.id,140,TRUE,FALSE,'',0 FROM public.emr_templates t CROSS JOIN public.emr_fields f WHERE t.tenant_id=0 AND t.code='dental' AND t.version_no='1.0' AND f.tenant_id=0 AND f.code='supplement' ON CONFLICT (template_id,field_id) DO UPDATE SET display_order=EXCLUDED.display_order,is_visible=EXCLUDED.is_visible,is_required=EXCLUDED.is_required,description=EXCLUDED.description,updated_at=NOW();
INSERT INTO public.emr_template_fields (tenant_id,template_id,field_id,display_order,is_visible,is_required,description,created_by) SELECT 0,t.id,f.id,510,TRUE,FALSE,'',0 FROM public.emr_templates t CROSS JOIN public.emr_fields f WHERE t.tenant_id=0 AND t.code='dental' AND t.version_no='1.0' AND f.tenant_id=0 AND f.code='oral_tooth_position' ON CONFLICT (template_id,field_id) DO UPDATE SET display_order=EXCLUDED.display_order,is_visible=EXCLUDED.is_visible,is_required=EXCLUDED.is_required,description=EXCLUDED.description,updated_at=NOW();
INSERT INTO public.emr_template_fields (tenant_id,template_id,field_id,display_order,is_visible,is_required,description,created_by) SELECT 0,t.id,f.id,520,TRUE,FALSE,'',0 FROM public.emr_templates t CROSS JOIN public.emr_fields f WHERE t.tenant_id=0 AND t.code='dental' AND t.version_no='1.0' AND f.tenant_id=0 AND f.code='oral_specialty_exam' ON CONFLICT (template_id,field_id) DO UPDATE SET display_order=EXCLUDED.display_order,is_visible=EXCLUDED.is_visible,is_required=EXCLUDED.is_required,description=EXCLUDED.description,updated_at=NOW();
INSERT INTO public.emr_template_fields (tenant_id,template_id,field_id,display_order,is_visible,is_required,description,created_by) SELECT 0,t.id,f.id,530,TRUE,FALSE,'',0 FROM public.emr_templates t CROSS JOIN public.emr_fields f WHERE t.tenant_id=0 AND t.code='dental' AND t.version_no='1.0' AND f.tenant_id=0 AND f.code='oral_periodontal_or_dental_condition' ON CONFLICT (template_id,field_id) DO UPDATE SET display_order=EXCLUDED.display_order,is_visible=EXCLUDED.is_visible,is_required=EXCLUDED.is_required,description=EXCLUDED.description,updated_at=NOW();
INSERT INTO public.emr_template_fields (tenant_id,template_id,field_id,display_order,is_visible,is_required,description,created_by) SELECT 0,t.id,f.id,540,TRUE,FALSE,'',0 FROM public.emr_templates t CROSS JOIN public.emr_fields f WHERE t.tenant_id=0 AND t.code='dental' AND t.version_no='1.0' AND f.tenant_id=0 AND f.code='oral_imaging_findings' ON CONFLICT (template_id,field_id) DO UPDATE SET display_order=EXCLUDED.display_order,is_visible=EXCLUDED.is_visible,is_required=EXCLUDED.is_required,description=EXCLUDED.description,updated_at=NOW();
INSERT INTO public.emr_template_fields (tenant_id,template_id,field_id,display_order,is_visible,is_required,description,created_by) SELECT 0,t.id,f.id,550,TRUE,FALSE,'',0 FROM public.emr_templates t CROSS JOIN public.emr_fields f WHERE t.tenant_id=0 AND t.code='dental' AND t.version_no='1.0' AND f.tenant_id=0 AND f.code='oral_treatment_plan' ON CONFLICT (template_id,field_id) DO UPDATE SET display_order=EXCLUDED.display_order,is_visible=EXCLUDED.is_visible,is_required=EXCLUDED.is_required,description=EXCLUDED.description,updated_at=NOW();
INSERT INTO public.emr_template_fields (tenant_id,template_id,field_id,display_order,is_visible,is_required,description,created_by) SELECT 0,t.id,f.id,10,TRUE,FALSE,'',0 FROM public.emr_templates t CROSS JOIN public.emr_fields f WHERE t.tenant_id=0 AND t.code='tcm' AND t.version_no='1.0' AND f.tenant_id=0 AND f.code='chief_complaint' ON CONFLICT (template_id,field_id) DO UPDATE SET display_order=EXCLUDED.display_order,is_visible=EXCLUDED.is_visible,is_required=EXCLUDED.is_required,description=EXCLUDED.description,updated_at=NOW();
INSERT INTO public.emr_template_fields (tenant_id,template_id,field_id,display_order,is_visible,is_required,description,created_by) SELECT 0,t.id,f.id,20,TRUE,FALSE,'',0 FROM public.emr_templates t CROSS JOIN public.emr_fields f WHERE t.tenant_id=0 AND t.code='tcm' AND t.version_no='1.0' AND f.tenant_id=0 AND f.code='present_illness' ON CONFLICT (template_id,field_id) DO UPDATE SET display_order=EXCLUDED.display_order,is_visible=EXCLUDED.is_visible,is_required=EXCLUDED.is_required,description=EXCLUDED.description,updated_at=NOW();
INSERT INTO public.emr_template_fields (tenant_id,template_id,field_id,display_order,is_visible,is_required,description,created_by) SELECT 0,t.id,f.id,30,TRUE,FALSE,'',0 FROM public.emr_templates t CROSS JOIN public.emr_fields f WHERE t.tenant_id=0 AND t.code='tcm' AND t.version_no='1.0' AND f.tenant_id=0 AND f.code='past_history' ON CONFLICT (template_id,field_id) DO UPDATE SET display_order=EXCLUDED.display_order,is_visible=EXCLUDED.is_visible,is_required=EXCLUDED.is_required,description=EXCLUDED.description,updated_at=NOW();
INSERT INTO public.emr_template_fields (tenant_id,template_id,field_id,display_order,is_visible,is_required,description,created_by) SELECT 0,t.id,f.id,40,TRUE,FALSE,'',0 FROM public.emr_templates t CROSS JOIN public.emr_fields f WHERE t.tenant_id=0 AND t.code='tcm' AND t.version_no='1.0' AND f.tenant_id=0 AND f.code='personal_history' ON CONFLICT (template_id,field_id) DO UPDATE SET display_order=EXCLUDED.display_order,is_visible=EXCLUDED.is_visible,is_required=EXCLUDED.is_required,description=EXCLUDED.description,updated_at=NOW();
INSERT INTO public.emr_template_fields (tenant_id,template_id,field_id,display_order,is_visible,is_required,description,created_by) SELECT 0,t.id,f.id,50,TRUE,FALSE,'',0 FROM public.emr_templates t CROSS JOIN public.emr_fields f WHERE t.tenant_id=0 AND t.code='tcm' AND t.version_no='1.0' AND f.tenant_id=0 AND f.code='family_history' ON CONFLICT (template_id,field_id) DO UPDATE SET display_order=EXCLUDED.display_order,is_visible=EXCLUDED.is_visible,is_required=EXCLUDED.is_required,description=EXCLUDED.description,updated_at=NOW();
INSERT INTO public.emr_template_fields (tenant_id,template_id,field_id,display_order,is_visible,is_required,description,created_by) SELECT 0,t.id,f.id,60,TRUE,FALSE,'',0 FROM public.emr_templates t CROSS JOIN public.emr_fields f WHERE t.tenant_id=0 AND t.code='tcm' AND t.version_no='1.0' AND f.tenant_id=0 AND f.code='allergy_history' ON CONFLICT (template_id,field_id) DO UPDATE SET display_order=EXCLUDED.display_order,is_visible=EXCLUDED.is_visible,is_required=EXCLUDED.is_required,description=EXCLUDED.description,updated_at=NOW();
INSERT INTO public.emr_template_fields (tenant_id,template_id,field_id,display_order,is_visible,is_required,description,created_by) SELECT 0,t.id,f.id,70,TRUE,FALSE,'',0 FROM public.emr_templates t CROSS JOIN public.emr_fields f WHERE t.tenant_id=0 AND t.code='tcm' AND t.version_no='1.0' AND f.tenant_id=0 AND f.code='physical_exam' ON CONFLICT (template_id,field_id) DO UPDATE SET display_order=EXCLUDED.display_order,is_visible=EXCLUDED.is_visible,is_required=EXCLUDED.is_required,description=EXCLUDED.description,updated_at=NOW();
INSERT INTO public.emr_template_fields (tenant_id,template_id,field_id,display_order,is_visible,is_required,description,created_by) SELECT 0,t.id,f.id,80,TRUE,FALSE,'',0 FROM public.emr_templates t CROSS JOIN public.emr_fields f WHERE t.tenant_id=0 AND t.code='tcm' AND t.version_no='1.0' AND f.tenant_id=0 AND f.code='auxiliary_exam' ON CONFLICT (template_id,field_id) DO UPDATE SET display_order=EXCLUDED.display_order,is_visible=EXCLUDED.is_visible,is_required=EXCLUDED.is_required,description=EXCLUDED.description,updated_at=NOW();
INSERT INTO public.emr_template_fields (tenant_id,template_id,field_id,display_order,is_visible,is_required,description,created_by) SELECT 0,t.id,f.id,90,TRUE,FALSE,'',0 FROM public.emr_templates t CROSS JOIN public.emr_fields f WHERE t.tenant_id=0 AND t.code='tcm' AND t.version_no='1.0' AND f.tenant_id=0 AND f.code='diagnosis' ON CONFLICT (template_id,field_id) DO UPDATE SET display_order=EXCLUDED.display_order,is_visible=EXCLUDED.is_visible,is_required=EXCLUDED.is_required,description=EXCLUDED.description,updated_at=NOW();
INSERT INTO public.emr_template_fields (tenant_id,template_id,field_id,display_order,is_visible,is_required,description,created_by) SELECT 0,t.id,f.id,100,TRUE,FALSE,'',0 FROM public.emr_templates t CROSS JOIN public.emr_fields f WHERE t.tenant_id=0 AND t.code='tcm' AND t.version_no='1.0' AND f.tenant_id=0 AND f.code='prescription' ON CONFLICT (template_id,field_id) DO UPDATE SET display_order=EXCLUDED.display_order,is_visible=EXCLUDED.is_visible,is_required=EXCLUDED.is_required,description=EXCLUDED.description,updated_at=NOW();
INSERT INTO public.emr_template_fields (tenant_id,template_id,field_id,display_order,is_visible,is_required,description,created_by) SELECT 0,t.id,f.id,110,TRUE,FALSE,'',0 FROM public.emr_templates t CROSS JOIN public.emr_fields f WHERE t.tenant_id=0 AND t.code='tcm' AND t.version_no='1.0' AND f.tenant_id=0 AND f.code='disposition' ON CONFLICT (template_id,field_id) DO UPDATE SET display_order=EXCLUDED.display_order,is_visible=EXCLUDED.is_visible,is_required=EXCLUDED.is_required,description=EXCLUDED.description,updated_at=NOW();
INSERT INTO public.emr_template_fields (tenant_id,template_id,field_id,display_order,is_visible,is_required,description,created_by) SELECT 0,t.id,f.id,120,TRUE,FALSE,'',0 FROM public.emr_templates t CROSS JOIN public.emr_fields f WHERE t.tenant_id=0 AND t.code='tcm' AND t.version_no='1.0' AND f.tenant_id=0 AND f.code='medical_advice' ON CONFLICT (template_id,field_id) DO UPDATE SET display_order=EXCLUDED.display_order,is_visible=EXCLUDED.is_visible,is_required=EXCLUDED.is_required,description=EXCLUDED.description,updated_at=NOW();
INSERT INTO public.emr_template_fields (tenant_id,template_id,field_id,display_order,is_visible,is_required,description,created_by) SELECT 0,t.id,f.id,130,TRUE,FALSE,'',0 FROM public.emr_templates t CROSS JOIN public.emr_fields f WHERE t.tenant_id=0 AND t.code='tcm' AND t.version_no='1.0' AND f.tenant_id=0 AND f.code='followup' ON CONFLICT (template_id,field_id) DO UPDATE SET display_order=EXCLUDED.display_order,is_visible=EXCLUDED.is_visible,is_required=EXCLUDED.is_required,description=EXCLUDED.description,updated_at=NOW();
INSERT INTO public.emr_template_fields (tenant_id,template_id,field_id,display_order,is_visible,is_required,description,created_by) SELECT 0,t.id,f.id,140,TRUE,FALSE,'',0 FROM public.emr_templates t CROSS JOIN public.emr_fields f WHERE t.tenant_id=0 AND t.code='tcm' AND t.version_no='1.0' AND f.tenant_id=0 AND f.code='supplement' ON CONFLICT (template_id,field_id) DO UPDATE SET display_order=EXCLUDED.display_order,is_visible=EXCLUDED.is_visible,is_required=EXCLUDED.is_required,description=EXCLUDED.description,updated_at=NOW();
INSERT INTO public.emr_template_fields (tenant_id,template_id,field_id,display_order,is_visible,is_required,description,created_by) SELECT 0,t.id,f.id,150,TRUE,FALSE,'',0 FROM public.emr_templates t CROSS JOIN public.emr_fields f WHERE t.tenant_id=0 AND t.code='tcm' AND t.version_no='1.0' AND f.tenant_id=0 AND f.code='tcm_four_examinations' ON CONFLICT (template_id,field_id) DO UPDATE SET display_order=EXCLUDED.display_order,is_visible=EXCLUDED.is_visible,is_required=EXCLUDED.is_required,description=EXCLUDED.description,updated_at=NOW();
INSERT INTO public.emr_template_fields (tenant_id,template_id,field_id,display_order,is_visible,is_required,description,created_by) SELECT 0,t.id,f.id,160,TRUE,FALSE,'',0 FROM public.emr_templates t CROSS JOIN public.emr_fields f WHERE t.tenant_id=0 AND t.code='tcm' AND t.version_no='1.0' AND f.tenant_id=0 AND f.code='tcm_tongue' ON CONFLICT (template_id,field_id) DO UPDATE SET display_order=EXCLUDED.display_order,is_visible=EXCLUDED.is_visible,is_required=EXCLUDED.is_required,description=EXCLUDED.description,updated_at=NOW();
INSERT INTO public.emr_template_fields (tenant_id,template_id,field_id,display_order,is_visible,is_required,description,created_by) SELECT 0,t.id,f.id,170,TRUE,FALSE,'',0 FROM public.emr_templates t CROSS JOIN public.emr_fields f WHERE t.tenant_id=0 AND t.code='tcm' AND t.version_no='1.0' AND f.tenant_id=0 AND f.code='tcm_pulse' ON CONFLICT (template_id,field_id) DO UPDATE SET display_order=EXCLUDED.display_order,is_visible=EXCLUDED.is_visible,is_required=EXCLUDED.is_required,description=EXCLUDED.description,updated_at=NOW();
INSERT INTO public.emr_template_fields (tenant_id,template_id,field_id,display_order,is_visible,is_required,description,created_by) SELECT 0,t.id,f.id,180,TRUE,FALSE,'',0 FROM public.emr_templates t CROSS JOIN public.emr_fields f WHERE t.tenant_id=0 AND t.code='tcm' AND t.version_no='1.0' AND f.tenant_id=0 AND f.code='tcm_pattern_differentiation' ON CONFLICT (template_id,field_id) DO UPDATE SET display_order=EXCLUDED.display_order,is_visible=EXCLUDED.is_visible,is_required=EXCLUDED.is_required,description=EXCLUDED.description,updated_at=NOW();
INSERT INTO public.emr_template_fields (tenant_id,template_id,field_id,display_order,is_visible,is_required,description,created_by) SELECT 0,t.id,f.id,190,TRUE,FALSE,'',0 FROM public.emr_templates t CROSS JOIN public.emr_fields f WHERE t.tenant_id=0 AND t.code='tcm' AND t.version_no='1.0' AND f.tenant_id=0 AND f.code='tcm_treatment_principle' ON CONFLICT (template_id,field_id) DO UPDATE SET display_order=EXCLUDED.display_order,is_visible=EXCLUDED.is_visible,is_required=EXCLUDED.is_required,description=EXCLUDED.description,updated_at=NOW();

INSERT INTO public.emr_quality_rule_definitions (rule_code,version,section_code,name,source_text,product_text,standard_points,executor_kind,input_spec,evaluation_spec,action_policy,message_templates,status) VALUES ('CC-001','2026-10-04-v2','chief_complaint','缺少主诉内容','缺少主诉内容','',5,'program','{"paths":["record.content.chief_complaint"]}'::jsonb,'{"per_item":false,"instruction":"没有主诉阻断；复诊缺主诉按原标准复诊缺项3分，不再同时扣5分。","scoring_group":""}'::jsonb,'{"save":{"enabled":true},"archive":{"enabled":true,"blocking":true},"complete":{"enabled":true,"blocking":true}}'::jsonb,'{"save":"请填写主诉。","archive":"没有主诉，不能归档。","complete":"没有主诉，不能完成病历。"}'::jsonb,'published') ON CONFLICT (rule_code,version) DO UPDATE SET section_code=EXCLUDED.section_code,name=EXCLUDED.name,source_text=EXCLUDED.source_text,product_text=EXCLUDED.product_text,standard_points=EXCLUDED.standard_points,executor_kind=EXCLUDED.executor_kind,input_spec=EXCLUDED.input_spec,evaluation_spec=EXCLUDED.evaluation_spec,action_policy=EXCLUDED.action_policy,message_templates=EXCLUDED.message_templates,status=EXCLUDED.status,updated_at=NOW();
INSERT INTO public.emr_quality_rule_definitions (rule_code,version,section_code,name,source_text,product_text,standard_points,executor_kind,input_spec,evaluation_spec,action_policy,message_templates,status) VALUES ('CC-002','2026-10-04-v2','chief_complaint','描述不确切、不完整或缺少某一部分','描述不确切、不完整或缺少某一部分','',2,'llm','{"paths":["record.patient_snapshot","record.content","record.context"]}'::jsonb,'{"checks":["症状","部位","时间"],"per_item":false,"instruction":"分别检查主要症状、部位和明确的持续时间。头痛3天已经包含三者。没有内容由CC-001处理；未写时间只提示不阻断。","scoring_group":""}'::jsonb,'{"save":{"enabled":false,"blocking":false,"blocking_fields":[]},"archive":{"enabled":true,"blocking":false,"blocking_fields":[]},"complete":{"enabled":true,"blocking":false,"blocking_fields":[]}}'::jsonb,'{"save":"发现描述不确切、不完整或缺少某一部分，请根据具体依据核对。","archive":"发现描述不确切、不完整或缺少某一部分，请根据具体依据核对。","complete":"发现描述不确切、不完整或缺少某一部分，请根据具体依据核对。"}'::jsonb,'published') ON CONFLICT (rule_code,version) DO UPDATE SET section_code=EXCLUDED.section_code,name=EXCLUDED.name,source_text=EXCLUDED.source_text,product_text=EXCLUDED.product_text,standard_points=EXCLUDED.standard_points,executor_kind=EXCLUDED.executor_kind,input_spec=EXCLUDED.input_spec,evaluation_spec=EXCLUDED.evaluation_spec,action_policy=EXCLUDED.action_policy,message_templates=EXCLUDED.message_templates,status=EXCLUDED.status,updated_at=NOW();
INSERT INTO public.emr_quality_rule_definitions (rule_code,version,section_code,name,source_text,product_text,standard_points,executor_kind,input_spec,evaluation_spec,action_policy,message_templates,status) VALUES ('CC-003','2026-10-04-v2','chief_complaint','以诊断代替主诉','以诊断代替主诉','',2,'llm','{"paths":["record.patient_snapshot","record.content","record.context"]}'::jsonb,'{"per_item":false,"instruction":"诊断名称代替症状才判定；诊断明确、无新症状的复诊取药允许病史同前。","scoring_group":""}'::jsonb,'{"save":{"enabled":false,"blocking":false,"blocking_fields":[]},"archive":{"enabled":true,"blocking":false,"blocking_fields":[]},"complete":{"enabled":true,"blocking":false,"blocking_fields":[]}}'::jsonb,'{"save":"发现以诊断代替主诉，请根据具体依据核对。","archive":"发现以诊断代替主诉，请根据具体依据核对。","complete":"发现以诊断代替主诉，请根据具体依据核对。"}'::jsonb,'published') ON CONFLICT (rule_code,version) DO UPDATE SET section_code=EXCLUDED.section_code,name=EXCLUDED.name,source_text=EXCLUDED.source_text,product_text=EXCLUDED.product_text,standard_points=EXCLUDED.standard_points,executor_kind=EXCLUDED.executor_kind,input_spec=EXCLUDED.input_spec,evaluation_spec=EXCLUDED.evaluation_spec,action_policy=EXCLUDED.action_policy,message_templates=EXCLUDED.message_templates,status=EXCLUDED.status,updated_at=NOW();
INSERT INTO public.emr_quality_rule_definitions (rule_code,version,section_code,name,source_text,product_text,standard_points,executor_kind,input_spec,evaluation_spec,action_policy,message_templates,status) VALUES ('CC-004','2026-10-04-v2','chief_complaint','有新症状体征而缺主诉的复诊、取药病历','有新症状体征而缺主诉的复诊、取药病历','',3,'llm','{"paths":["record.patient_snapshot","record.content","record.context"]}'::jsonb,'{"per_item":false,"instruction":"只适用复诊取药；引用本次病史明确新症状或对照既往记录，主诉未反映该症状时提示。无历史且无新症状证据不推断。","scoring_group":""}'::jsonb,'{"save":{"enabled":false,"blocking":false,"blocking_fields":[]},"archive":{"enabled":true,"blocking":false,"blocking_fields":[]},"complete":{"enabled":true,"blocking":false,"blocking_fields":[]}}'::jsonb,'{"save":"发现有新症状体征而缺主诉的复诊、取药病历，请根据具体依据核对。","archive":"发现有新症状体征而缺主诉的复诊、取药病历，请根据具体依据核对。","complete":"发现有新症状体征而缺主诉的复诊、取药病历，请根据具体依据核对。"}'::jsonb,'published') ON CONFLICT (rule_code,version) DO UPDATE SET section_code=EXCLUDED.section_code,name=EXCLUDED.name,source_text=EXCLUDED.source_text,product_text=EXCLUDED.product_text,standard_points=EXCLUDED.standard_points,executor_kind=EXCLUDED.executor_kind,input_spec=EXCLUDED.input_spec,evaluation_spec=EXCLUDED.evaluation_spec,action_policy=EXCLUDED.action_policy,message_templates=EXCLUDED.message_templates,status=EXCLUDED.status,updated_at=NOW();
INSERT INTO public.emr_quality_rule_definitions (rule_code,version,section_code,name,source_text,product_text,standard_points,executor_kind,input_spec,evaluation_spec,action_policy,message_templates,status) VALUES ('CC-005','2026-10-04-v2','chief_complaint','主诉不超过20个字','','主诉不超过20个字',0,'program','{"paths":["record.content.chief_complaint"]}'::jsonb,'{"per_item":false,"count_unit":"字符","max_length":20,"instruction":"主诉超过20字符阻断，不新增原标准扣分。","scoring_group":""}'::jsonb,'{"save":{"enabled":true},"archive":{"enabled":true,"blocking":true},"complete":{"enabled":true,"blocking":true}}'::jsonb,'{"save":"主诉超过20个字，请精简。","archive":"主诉超过20个字，不能归档。","complete":"主诉超过20个字，不能完成病历。"}'::jsonb,'published') ON CONFLICT (rule_code,version) DO UPDATE SET section_code=EXCLUDED.section_code,name=EXCLUDED.name,source_text=EXCLUDED.source_text,product_text=EXCLUDED.product_text,standard_points=EXCLUDED.standard_points,executor_kind=EXCLUDED.executor_kind,input_spec=EXCLUDED.input_spec,evaluation_spec=EXCLUDED.evaluation_spec,action_policy=EXCLUDED.action_policy,message_templates=EXCLUDED.message_templates,status=EXCLUDED.status,updated_at=NOW();
INSERT INTO public.emr_quality_rule_definitions (rule_code,version,section_code,name,source_text,product_text,standard_points,executor_kind,input_spec,evaluation_spec,action_policy,message_templates,status) VALUES ('DX-001','2026-10-04-v2','diagnosis','未形成明确诊断，且没有进一步检查意见和建议','未明确诊断病历，没有进一步检查意见和建议','',10,'hybrid','{"paths":["record.content.diagnosis","record.content.auxiliary_exam","record.content.disposition","record.content.medical_advice"]}'::jsonb,'{"mode":"diagnosis_presence_only","per_item":false,"instruction":"没有诊断由程序阻断；语义检查不能仅匹配待查两字。症状性或首先考虑的可能诊断有临床意义，并记录进一步检查、处理建议时允许。无意义占位或无有效临床判断及建议时阻断。","scoring_group":"","semantic_check":"pending_llm"}'::jsonb,'{"save":{"enabled":true},"archive":{"enabled":true,"blocking":true},"complete":{"enabled":true,"blocking":true}}'::jsonb,'{"save":"请填写诊断。","archive":"没有形成明确诊断，不能归档。","complete":"没有形成明确诊断，不能完成病历。"}'::jsonb,'published') ON CONFLICT (rule_code,version) DO UPDATE SET section_code=EXCLUDED.section_code,name=EXCLUDED.name,source_text=EXCLUDED.source_text,product_text=EXCLUDED.product_text,standard_points=EXCLUDED.standard_points,executor_kind=EXCLUDED.executor_kind,input_spec=EXCLUDED.input_spec,evaluation_spec=EXCLUDED.evaluation_spec,action_policy=EXCLUDED.action_policy,message_templates=EXCLUDED.message_templates,status=EXCLUDED.status,updated_at=NOW();
INSERT INTO public.emr_quality_rule_definitions (rule_code,version,section_code,name,source_text,product_text,standard_points,executor_kind,input_spec,evaluation_spec,action_policy,message_templates,status) VALUES ('DX-002','2026-10-04-v2','diagnosis','诊断与主诉、病史或检查结果不一致或没有临床意义','诊断内容没有临床意义','诊断应与主诉、现病史和辅助检查结果相符；发现明显不一致时提示具体两端内容。',5,'llm','{"paths":["record.patient_snapshot","record.content","record.context"]}'::jsonb,'{"per_item":false,"instruction":"必须同时比较主诉、现病史、辅助检查和诊断。若主要症状、身体部位或检查结论与诊断明显不一致，返回problem；至少引用诊断和一个对照字段的原文，并明确说明哪两端不一致。若资料不足，返回not_evaluable。","scoring_group":"","required_evidence_sources":["record.content.diagnosis","record.content.chief_complaint","record.content.present_illness","record.content.auxiliary_exam"]}'::jsonb,'{"save":{"enabled":false,"blocking":false,"blocking_fields":[]},"archive":{"enabled":true,"blocking":false,"blocking_fields":[]},"complete":{"enabled":true,"blocking":false,"blocking_fields":[]}}'::jsonb,'{"save":"诊断与主诉、病史或检查结果可能不一致，请核对具体依据。","archive":"诊断与主诉、病史或检查结果不一致，请核对具体依据。","complete":"诊断与主诉、病史或检查结果不一致，请核对具体依据。"}'::jsonb,'published') ON CONFLICT (rule_code,version) DO UPDATE SET section_code=EXCLUDED.section_code,name=EXCLUDED.name,source_text=EXCLUDED.source_text,product_text=EXCLUDED.product_text,standard_points=EXCLUDED.standard_points,executor_kind=EXCLUDED.executor_kind,input_spec=EXCLUDED.input_spec,evaluation_spec=EXCLUDED.evaluation_spec,action_policy=EXCLUDED.action_policy,message_templates=EXCLUDED.message_templates,status=EXCLUDED.status,updated_at=NOW();
INSERT INTO public.emr_quality_rule_definitions (rule_code,version,section_code,name,source_text,product_text,standard_points,executor_kind,input_spec,evaluation_spec,action_policy,message_templates,status) VALUES ('DX-003','2026-10-04-v2','diagnosis','诊断名称和编码不符合ICD-10','诊断名称不全、不规范','产品要求按 ICD-10 编码',5,'program','{"paths":["record.patient_snapshot","record.content","record.context"]}'::jsonb,'{"mode":"icd10","per_item":false,"instruction":"查询现行国家ICD-10目录；标准名称可准确匹配时可通过，输入编码时名称与编码必须匹配。自由文本不得猜测编码。","scoring_group":""}'::jsonb,'{"save":{"enabled":true,"blocking":false,"blocking_fields":[]},"archive":{"enabled":true,"blocking":false,"blocking_fields":[]},"complete":{"enabled":true,"blocking":false,"blocking_fields":[]}}'::jsonb,'{"save":"发现诊断名称和编码不符合ICD-10，请根据具体依据核对。","archive":"发现诊断名称和编码不符合ICD-10，请根据具体依据核对。","complete":"发现诊断名称和编码不符合ICD-10，请根据具体依据核对。"}'::jsonb,'published') ON CONFLICT (rule_code,version) DO UPDATE SET section_code=EXCLUDED.section_code,name=EXCLUDED.name,source_text=EXCLUDED.source_text,product_text=EXCLUDED.product_text,standard_points=EXCLUDED.standard_points,executor_kind=EXCLUDED.executor_kind,input_spec=EXCLUDED.input_spec,evaluation_spec=EXCLUDED.evaluation_spec,action_policy=EXCLUDED.action_policy,message_templates=EXCLUDED.message_templates,status=EXCLUDED.status,updated_at=NOW();
INSERT INTO public.emr_quality_rule_definitions (rule_code,version,section_code,name,source_text,product_text,standard_points,executor_kind,input_spec,evaluation_spec,action_policy,message_templates,status) VALUES ('EXAM-001','2026-10-04-v2','physical_exam','缺少必要的体检项目','缺少必要的体检项目','',20,'hybrid','{"paths":["record.content.physical_exam"]}'::jsonb,'{"mode":"whole_section_empty_only","per_item":false,"instruction":"没有体检内容由程序阻断。已有内容，仅有具体证据证明缺少必要体检项目时判定，不机械要求所有生命体征。不可把科别直接当作所有检查必需的依据。","scoring_group":"","required_config":"pending"}'::jsonb,'{"save":{"enabled":true},"archive":{"enabled":true,"blocking":true},"complete":{"enabled":true,"blocking":true}}'::jsonb,'{"save":"没有记录体格检查，请补充体格检查。","archive":"没有记录体格检查，不能归档。","complete":"没有记录体格检查，不能完成病历。"}'::jsonb,'published') ON CONFLICT (rule_code,version) DO UPDATE SET section_code=EXCLUDED.section_code,name=EXCLUDED.name,source_text=EXCLUDED.source_text,product_text=EXCLUDED.product_text,standard_points=EXCLUDED.standard_points,executor_kind=EXCLUDED.executor_kind,input_spec=EXCLUDED.input_spec,evaluation_spec=EXCLUDED.evaluation_spec,action_policy=EXCLUDED.action_policy,message_templates=EXCLUDED.message_templates,status=EXCLUDED.status,updated_at=NOW();
INSERT INTO public.emr_quality_rule_definitions (rule_code,version,section_code,name,source_text,product_text,standard_points,executor_kind,input_spec,evaluation_spec,action_policy,message_templates,status) VALUES ('EXAM-002','2026-10-04-v2','physical_exam','记录不准确、不全面','记录不准确、不全面','',5,'llm','{"paths":["record.patient_snapshot","record.content","record.context"]}'::jsonb,'{"per_item":false,"instruction":"体检记录不准确需要体检与对照原文具体差异。不能判定真实体征是否存在。同一矛盾不重复记入EXAM-004。","scoring_group":""}'::jsonb,'{"save":{"enabled":false,"blocking":false,"blocking_fields":[]},"archive":{"enabled":true,"blocking":false,"blocking_fields":[]},"complete":{"enabled":true,"blocking":false,"blocking_fields":[]}}'::jsonb,'{"save":"发现记录不准确、不全面，请根据具体依据核对。","archive":"发现记录不准确、不全面，请根据具体依据核对。","complete":"发现记录不准确、不全面，请根据具体依据核对。"}'::jsonb,'published') ON CONFLICT (rule_code,version) DO UPDATE SET section_code=EXCLUDED.section_code,name=EXCLUDED.name,source_text=EXCLUDED.source_text,product_text=EXCLUDED.product_text,standard_points=EXCLUDED.standard_points,executor_kind=EXCLUDED.executor_kind,input_spec=EXCLUDED.input_spec,evaluation_spec=EXCLUDED.evaluation_spec,action_policy=EXCLUDED.action_policy,message_templates=EXCLUDED.message_templates,status=EXCLUDED.status,updated_at=NOW();
INSERT INTO public.emr_quality_rule_definitions (rule_code,version,section_code,name,source_text,product_text,standard_points,executor_kind,input_spec,evaluation_spec,action_policy,message_templates,status) VALUES ('EXAM-003','2026-10-04-v2','physical_exam','阳性体征或主要阴性体征未描述','阳性体征或主要阴性体征未描述','',5,'llm','{"paths":["record.patient_snapshot","record.content","record.context"]}'::jsonb,'{"per_item":false,"instruction":"引用病史诊断或检查中已经明确的阳性或必要阴性体征，但体检没记录才判定，必须列出具体体征。","scoring_group":""}'::jsonb,'{"save":{"enabled":false,"blocking":false,"blocking_fields":[]},"archive":{"enabled":true,"blocking":false,"blocking_fields":[]},"complete":{"enabled":true,"blocking":false,"blocking_fields":[]}}'::jsonb,'{"save":"发现阳性体征或主要阴性体征未描述，请根据具体依据核对。","archive":"发现阳性体征或主要阴性体征未描述，请根据具体依据核对。","complete":"发现阳性体征或主要阴性体征未描述，请根据具体依据核对。"}'::jsonb,'published') ON CONFLICT (rule_code,version) DO UPDATE SET section_code=EXCLUDED.section_code,name=EXCLUDED.name,source_text=EXCLUDED.source_text,product_text=EXCLUDED.product_text,standard_points=EXCLUDED.standard_points,executor_kind=EXCLUDED.executor_kind,input_spec=EXCLUDED.input_spec,evaluation_spec=EXCLUDED.evaluation_spec,action_policy=EXCLUDED.action_policy,message_templates=EXCLUDED.message_templates,status=EXCLUDED.status,updated_at=NOW();
INSERT INTO public.emr_quality_rule_definitions (rule_code,version,section_code,name,source_text,product_text,standard_points,executor_kind,input_spec,evaluation_spec,action_policy,message_templates,status) VALUES ('EXAM-004','2026-10-04-v2','physical_exam','阳性体征或主要阴性体征描述不准确','阳性体征或主要阴性体征描述不准确','',3,'llm','{"paths":["record.patient_snapshot","record.content","record.context"]}'::jsonb,'{"per_item":false,"instruction":"阳性或阴性体征描述与对照数据的具体矛盾，必须提供两端原文，不能猜测临床真实性。","scoring_group":""}'::jsonb,'{"save":{"enabled":false,"blocking":false,"blocking_fields":[]},"archive":{"enabled":true,"blocking":false,"blocking_fields":[]},"complete":{"enabled":true,"blocking":false,"blocking_fields":[]}}'::jsonb,'{"save":"发现阳性体征或主要阴性体征描述不准确，请根据具体依据核对。","archive":"发现阳性体征或主要阴性体征描述不准确，请根据具体依据核对。","complete":"发现阳性体征或主要阴性体征描述不准确，请根据具体依据核对。"}'::jsonb,'published') ON CONFLICT (rule_code,version) DO UPDATE SET section_code=EXCLUDED.section_code,name=EXCLUDED.name,source_text=EXCLUDED.source_text,product_text=EXCLUDED.product_text,standard_points=EXCLUDED.standard_points,executor_kind=EXCLUDED.executor_kind,input_spec=EXCLUDED.input_spec,evaluation_spec=EXCLUDED.evaluation_spec,action_policy=EXCLUDED.action_policy,message_templates=EXCLUDED.message_templates,status=EXCLUDED.status,updated_at=NOW();
INSERT INTO public.emr_quality_rule_definitions (rule_code,version,section_code,name,source_text,product_text,standard_points,executor_kind,input_spec,evaluation_spec,action_policy,message_templates,status) VALUES ('EXAM-005','2026-10-04-v2','physical_exam','缺少专科检查记录','缺少专科检查记录','',10,'llm','{"paths":["record.patient_snapshot","record.content","record.context"]}'::jsonb,'{"per_item":false,"instruction":"必须根据主诉病情列出具体专科检查和医学依据，不能仅因specialty_exam字段没有内容就判定缺项；其他体检字段已有同样项目视为已记录。","scoring_group":""}'::jsonb,'{"save":{"enabled":false,"blocking":false,"blocking_fields":[]},"archive":{"enabled":true,"blocking":false,"blocking_fields":[]},"complete":{"enabled":true,"blocking":false,"blocking_fields":[]}}'::jsonb,'{"save":"发现缺少专科检查记录，请根据具体依据核对。","archive":"发现缺少专科检查记录，请根据具体依据核对。","complete":"发现缺少专科检查记录，请根据具体依据核对。"}'::jsonb,'published') ON CONFLICT (rule_code,version) DO UPDATE SET section_code=EXCLUDED.section_code,name=EXCLUDED.name,source_text=EXCLUDED.source_text,product_text=EXCLUDED.product_text,standard_points=EXCLUDED.standard_points,executor_kind=EXCLUDED.executor_kind,input_spec=EXCLUDED.input_spec,evaluation_spec=EXCLUDED.evaluation_spec,action_policy=EXCLUDED.action_policy,message_templates=EXCLUDED.message_templates,status=EXCLUDED.status,updated_at=NOW();
INSERT INTO public.emr_quality_rule_definitions (rule_code,version,section_code,name,source_text,product_text,standard_points,executor_kind,input_spec,evaluation_spec,action_policy,message_templates,status) VALUES ('EXAM-006','2026-10-04-v2','physical_exam','复诊缺少原有阳性体征变化或必要的体检记录','复诊缺少原有阳性体征变化或必要的体检记录','',20,'llm','{"paths":["record.patient_snapshot","record.content","record.context"]}'::jsonb,'{"per_item":false,"instruction":"仅复诊且可靠既往记录存在，具体阳性体征或必要项目变化未记录时判定；数据不足不判断，不能猜测前次体征。","scoring_group":""}'::jsonb,'{"save":{"enabled":false,"blocking":false,"blocking_fields":[]},"archive":{"enabled":true,"blocking":true,"blocking_fields":[]},"complete":{"enabled":true,"blocking":true,"blocking_fields":[]}}'::jsonb,'{"save":"发现复诊缺少原有阳性体征变化或必要的体检记录，请根据具体依据核对。","archive":"发现复诊缺少原有阳性体征变化或必要的体检记录，请根据具体依据核对。","complete":"发现复诊缺少原有阳性体征变化或必要的体检记录，请根据具体依据核对。"}'::jsonb,'published') ON CONFLICT (rule_code,version) DO UPDATE SET section_code=EXCLUDED.section_code,name=EXCLUDED.name,source_text=EXCLUDED.source_text,product_text=EXCLUDED.product_text,standard_points=EXCLUDED.standard_points,executor_kind=EXCLUDED.executor_kind,input_spec=EXCLUDED.input_spec,evaluation_spec=EXCLUDED.evaluation_spec,action_policy=EXCLUDED.action_policy,message_templates=EXCLUDED.message_templates,status=EXCLUDED.status,updated_at=NOW();
INSERT INTO public.emr_quality_rule_definitions (rule_code,version,section_code,name,source_text,product_text,standard_points,executor_kind,input_spec,evaluation_spec,action_policy,message_templates,status) VALUES ('GEN-001','2026-10-04-v2','general','各项医疗信息未填写一项','各项医疗信息未填写一项','',2,'program','{"paths":["record.patient_snapshot"]}'::jsonb,'{"per_item":true,"instruction":"按患者快照逐项检查基本信息；科别、病历编号、日期读取病历上下文。缺项和不完整不重复计算。","scoring_group":"","required_fields":["name","gender","birth_date"]}'::jsonb,'{"save":{"enabled":true},"archive":{"enabled":true,"blocking_fields":["name","gender","birth_date"]},"complete":{"enabled":true,"blocking_fields":["name","gender","birth_date"]}}'::jsonb,'{"save":"请填写{field}。","archive":"没有填写{field}，不能归档。","complete":"请补充{field}，再完成病历。"}'::jsonb,'published') ON CONFLICT (rule_code,version) DO UPDATE SET section_code=EXCLUDED.section_code,name=EXCLUDED.name,source_text=EXCLUDED.source_text,product_text=EXCLUDED.product_text,standard_points=EXCLUDED.standard_points,executor_kind=EXCLUDED.executor_kind,input_spec=EXCLUDED.input_spec,evaluation_spec=EXCLUDED.evaluation_spec,action_policy=EXCLUDED.action_policy,message_templates=EXCLUDED.message_templates,status=EXCLUDED.status,updated_at=NOW();
INSERT INTO public.emr_quality_rule_definitions (rule_code,version,section_code,name,source_text,product_text,standard_points,executor_kind,input_spec,evaluation_spec,action_policy,message_templates,status) VALUES ('GEN-002','2026-10-04-v2','general','一项医疗信息填写不正确','一项医疗信息填写不正确','',1,'program','{"paths":["record.patient_snapshot","record.content","record.context"]}'::jsonb,'{"per_item":true,"instruction":"只判断可证明的错误，例如出生年月晚于就诊日期。无法证明个人信息真实性时不猜测。","scoring_group":""}'::jsonb,'{"save":{"enabled":true,"blocking":false,"blocking_fields":[]},"archive":{"enabled":true,"blocking":false,"blocking_fields":[]},"complete":{"enabled":true,"blocking":false,"blocking_fields":[]}}'::jsonb,'{"save":"发现一项医疗信息填写不正确，请根据具体依据核对。","archive":"发现一项医疗信息填写不正确，请根据具体依据核对。","complete":"发现一项医疗信息填写不正确，请根据具体依据核对。"}'::jsonb,'published') ON CONFLICT (rule_code,version) DO UPDATE SET section_code=EXCLUDED.section_code,name=EXCLUDED.name,source_text=EXCLUDED.source_text,product_text=EXCLUDED.product_text,standard_points=EXCLUDED.standard_points,executor_kind=EXCLUDED.executor_kind,input_spec=EXCLUDED.input_spec,evaluation_spec=EXCLUDED.evaluation_spec,action_policy=EXCLUDED.action_policy,message_templates=EXCLUDED.message_templates,status=EXCLUDED.status,updated_at=NOW();
INSERT INTO public.emr_quality_rule_definitions (rule_code,version,section_code,name,source_text,product_text,standard_points,executor_kind,input_spec,evaluation_spec,action_policy,message_templates,status) VALUES ('GEN-003','2026-10-04-v2','general','一项医疗信息填写不完整','一项医疗信息填写不完整','',1,'program','{"paths":["record.patient_snapshot","record.content","record.context"]}'::jsonb,'{"per_item":true,"instruction":"出生年月至少记录年和月；已填写年份但没有月份适用此项。缺项不能重复记作不完整。","scoring_group":""}'::jsonb,'{"save":{"enabled":true,"blocking":false,"blocking_fields":[]},"archive":{"enabled":true,"blocking":false,"blocking_fields":[]},"complete":{"enabled":true,"blocking":false,"blocking_fields":[]}}'::jsonb,'{"save":"发现一项医疗信息填写不完整，请根据具体依据核对。","archive":"发现一项医疗信息填写不完整，请根据具体依据核对。","complete":"发现一项医疗信息填写不完整，请根据具体依据核对。"}'::jsonb,'published') ON CONFLICT (rule_code,version) DO UPDATE SET section_code=EXCLUDED.section_code,name=EXCLUDED.name,source_text=EXCLUDED.source_text,product_text=EXCLUDED.product_text,standard_points=EXCLUDED.standard_points,executor_kind=EXCLUDED.executor_kind,input_spec=EXCLUDED.input_spec,evaluation_spec=EXCLUDED.evaluation_spec,action_policy=EXCLUDED.action_policy,message_templates=EXCLUDED.message_templates,status=EXCLUDED.status,updated_at=NOW();
INSERT INTO public.emr_quality_rule_definitions (rule_code,version,section_code,name,source_text,product_text,standard_points,executor_kind,input_spec,evaluation_spec,action_policy,message_templates,status) VALUES ('GEN-004','2026-10-04-v2','general','急诊病历就诊日期未写到分钟','急诊病历就诊日期未写到分钟','',3,'program','{"paths":["record.patient_snapshot","record.content","record.context"]}'::jsonb,'{"mode":"emergency_minute","per_item":false,"instruction":"仅急诊病历适用；started_at 或明确的就诊时间原值包含小时分钟即可，秒为0不代表缺分钟。","scoring_group":""}'::jsonb,'{"save":{"enabled":true,"blocking":false,"blocking_fields":[]},"archive":{"enabled":true,"blocking":false,"blocking_fields":[]},"complete":{"enabled":true,"blocking":false,"blocking_fields":[]}}'::jsonb,'{"save":"发现急诊病历就诊日期未写到分钟，请根据具体依据核对。","archive":"发现急诊病历就诊日期未写到分钟，请根据具体依据核对。","complete":"发现急诊病历就诊日期未写到分钟，请根据具体依据核对。"}'::jsonb,'published') ON CONFLICT (rule_code,version) DO UPDATE SET section_code=EXCLUDED.section_code,name=EXCLUDED.name,source_text=EXCLUDED.source_text,product_text=EXCLUDED.product_text,standard_points=EXCLUDED.standard_points,executor_kind=EXCLUDED.executor_kind,input_spec=EXCLUDED.input_spec,evaluation_spec=EXCLUDED.evaluation_spec,action_policy=EXCLUDED.action_policy,message_templates=EXCLUDED.message_templates,status=EXCLUDED.status,updated_at=NOW();
INSERT INTO public.emr_quality_rule_definitions (rule_code,version,section_code,name,source_text,product_text,standard_points,executor_kind,input_spec,evaluation_spec,action_policy,message_templates,status) VALUES ('GEN-005','2026-10-04-v2','general','药物过敏史必须明确','','药物过敏史必须明确；没有过敏史也要明确填写。',0,'program','{"paths":["record.patient_snapshot","record.content","record.context"]}'::jsonb,'{"per_item":false,"uncertain":["待确认","尚未询问","不详","未询问"],"instruction":"患者快照中的药物过敏史必须明确；明确否认或明确记录过敏均可，详细程度不用于阻断。","scoring_group":""}'::jsonb,'{"save":{"enabled":true,"blocking":false,"blocking_fields":[]},"archive":{"enabled":true,"blocking":true,"blocking_fields":[]},"complete":{"enabled":true,"blocking":true,"blocking_fields":[]}}'::jsonb,'{"save":"请明确记录药物过敏史；没有过敏史也请明确填写。","archive":"没有明确记录药物过敏史，不能归档。","complete":"没有明确记录药物过敏史，不能完成病历。"}'::jsonb,'published') ON CONFLICT (rule_code,version) DO UPDATE SET section_code=EXCLUDED.section_code,name=EXCLUDED.name,source_text=EXCLUDED.source_text,product_text=EXCLUDED.product_text,standard_points=EXCLUDED.standard_points,executor_kind=EXCLUDED.executor_kind,input_spec=EXCLUDED.input_spec,evaluation_spec=EXCLUDED.evaluation_spec,action_policy=EXCLUDED.action_policy,message_templates=EXCLUDED.message_templates,status=EXCLUDED.status,updated_at=NOW();
INSERT INTO public.emr_quality_rule_definitions (rule_code,version,section_code,name,source_text,product_text,standard_points,executor_kind,input_spec,evaluation_spec,action_policy,message_templates,status) VALUES ('GEN-006','2026-10-04-v2','general','药物过敏史记录不具体','','药物过敏史记录不具体',0,'llm','{"paths":["record.patient_snapshot","record.content","record.context"]}'::jsonb,'{"per_item":false,"instruction":"只检查药物过敏史已记录但缺具体类型或过敏原。明确否认过敏不得提示不具体；无记录由GEN-005处理。","scoring_group":""}'::jsonb,'{"save":{"enabled":false,"blocking":false,"blocking_fields":[]},"archive":{"enabled":true,"blocking":false,"blocking_fields":[]},"complete":{"enabled":true,"blocking":false,"blocking_fields":[]}}'::jsonb,'{"save":"发现药物过敏史记录不具体，请根据具体依据核对。","archive":"发现药物过敏史记录不具体，请根据具体依据核对。","complete":"发现药物过敏史记录不具体，请根据具体依据核对。"}'::jsonb,'published') ON CONFLICT (rule_code,version) DO UPDATE SET section_code=EXCLUDED.section_code,name=EXCLUDED.name,source_text=EXCLUDED.source_text,product_text=EXCLUDED.product_text,standard_points=EXCLUDED.standard_points,executor_kind=EXCLUDED.executor_kind,input_spec=EXCLUDED.input_spec,evaluation_spec=EXCLUDED.evaluation_spec,action_policy=EXCLUDED.action_policy,message_templates=EXCLUDED.message_templates,status=EXCLUDED.status,updated_at=NOW();
INSERT INTO public.emr_quality_rule_definitions (rule_code,version,section_code,name,source_text,product_text,standard_points,executor_kind,input_spec,evaluation_spec,action_policy,message_templates,status) VALUES ('HIS-001','2026-10-04-v2','history','缺现病史','缺现病史','',20,'program','{"paths":["record.content.present_illness"]}'::jsonb,'{"per_item":false,"instruction":"现病史没有医疗内容阻断，不重复计算现病史语义缺陷。","scoring_group":""}'::jsonb,'{"save":{"enabled":true},"archive":{"enabled":true,"blocking":true},"complete":{"enabled":true,"blocking":true}}'::jsonb,'{"save":"请填写现病史。","archive":"没有现病史，不能归档。","complete":"没有现病史，不能完成病历。"}'::jsonb,'published') ON CONFLICT (rule_code,version) DO UPDATE SET section_code=EXCLUDED.section_code,name=EXCLUDED.name,source_text=EXCLUDED.source_text,product_text=EXCLUDED.product_text,standard_points=EXCLUDED.standard_points,executor_kind=EXCLUDED.executor_kind,input_spec=EXCLUDED.input_spec,evaluation_spec=EXCLUDED.evaluation_spec,action_policy=EXCLUDED.action_policy,message_templates=EXCLUDED.message_templates,status=EXCLUDED.status,updated_at=NOW();
INSERT INTO public.emr_quality_rule_definitions (rule_code,version,section_code,name,source_text,product_text,standard_points,executor_kind,input_spec,evaluation_spec,action_policy,message_templates,status) VALUES ('HIS-002','2026-10-04-v2','history','初诊现病史与主诉不相符','初诊现病史与主诉不相符','',4,'llm','{"paths":["record.patient_snapshot","record.content","record.context"]}'::jsonb,'{"per_item":false,"instruction":"仅初诊，对比主诉和现病史具体症状、部位、时间的明确矛盾；需要两端原文。","scoring_group":""}'::jsonb,'{"save":{"enabled":false,"blocking":false,"blocking_fields":[]},"archive":{"enabled":true,"blocking":false,"blocking_fields":[]},"complete":{"enabled":true,"blocking":false,"blocking_fields":[]}}'::jsonb,'{"save":"发现初诊现病史与主诉不相符，请根据具体依据核对。","archive":"发现初诊现病史与主诉不相符，请根据具体依据核对。","complete":"发现初诊现病史与主诉不相符，请根据具体依据核对。"}'::jsonb,'published') ON CONFLICT (rule_code,version) DO UPDATE SET section_code=EXCLUDED.section_code,name=EXCLUDED.name,source_text=EXCLUDED.source_text,product_text=EXCLUDED.product_text,standard_points=EXCLUDED.standard_points,executor_kind=EXCLUDED.executor_kind,input_spec=EXCLUDED.input_spec,evaluation_spec=EXCLUDED.evaluation_spec,action_policy=EXCLUDED.action_policy,message_templates=EXCLUDED.message_templates,status=EXCLUDED.status,updated_at=NOW();
INSERT INTO public.emr_quality_rule_definitions (rule_code,version,section_code,name,source_text,product_text,standard_points,executor_kind,input_spec,evaluation_spec,action_policy,message_templates,status) VALUES ('HIS-003','2026-10-04-v2','history','初诊现病史描述混乱、重点不突出','初诊现病史描述混乱、重点不突出','',4,'llm','{"paths":["record.patient_snapshot","record.content","record.context"]}'::jsonb,'{"per_item":false,"instruction":"仅初诊，指出现病史具体混乱段落或重点缺失；不能自创独立病情演变扣分项。","scoring_group":""}'::jsonb,'{"save":{"enabled":false,"blocking":false,"blocking_fields":[]},"archive":{"enabled":true,"blocking":false,"blocking_fields":[]},"complete":{"enabled":true,"blocking":false,"blocking_fields":[]}}'::jsonb,'{"save":"发现初诊现病史描述混乱、重点不突出，请根据具体依据核对。","archive":"发现初诊现病史描述混乱、重点不突出，请根据具体依据核对。","complete":"发现初诊现病史描述混乱、重点不突出，请根据具体依据核对。"}'::jsonb,'published') ON CONFLICT (rule_code,version) DO UPDATE SET section_code=EXCLUDED.section_code,name=EXCLUDED.name,source_text=EXCLUDED.source_text,product_text=EXCLUDED.product_text,standard_points=EXCLUDED.standard_points,executor_kind=EXCLUDED.executor_kind,input_spec=EXCLUDED.input_spec,evaluation_spec=EXCLUDED.evaluation_spec,action_policy=EXCLUDED.action_policy,message_templates=EXCLUDED.message_templates,status=EXCLUDED.status,updated_at=NOW();
INSERT INTO public.emr_quality_rule_definitions (rule_code,version,section_code,name,source_text,product_text,standard_points,executor_kind,input_spec,evaluation_spec,action_policy,message_templates,status) VALUES ('HIS-004','2026-10-04-v2','history','初诊缺少主要鉴别诊断资料','初诊缺少主要鉴别诊断资料','',3,'llm','{"paths":["record.patient_snapshot","record.content","record.context"]}'::jsonb,'{"per_item":false,"instruction":"仅初诊，引用明确疾病或症状及缺失的鉴别诊断资料并说明医学依据；不能列举所有可能疾病要求逐项排除。不确定时人工核对。","scoring_group":""}'::jsonb,'{"save":{"enabled":false,"blocking":false,"blocking_fields":[]},"archive":{"enabled":true,"blocking":false,"blocking_fields":[]},"complete":{"enabled":true,"blocking":false,"blocking_fields":[]}}'::jsonb,'{"save":"发现初诊缺少主要鉴别诊断资料，请根据具体依据核对。","archive":"发现初诊缺少主要鉴别诊断资料，请根据具体依据核对。","complete":"发现初诊缺少主要鉴别诊断资料，请根据具体依据核对。"}'::jsonb,'published') ON CONFLICT (rule_code,version) DO UPDATE SET section_code=EXCLUDED.section_code,name=EXCLUDED.name,source_text=EXCLUDED.source_text,product_text=EXCLUDED.product_text,standard_points=EXCLUDED.standard_points,executor_kind=EXCLUDED.executor_kind,input_spec=EXCLUDED.input_spec,evaluation_spec=EXCLUDED.evaluation_spec,action_policy=EXCLUDED.action_policy,message_templates=EXCLUDED.message_templates,status=EXCLUDED.status,updated_at=NOW();
INSERT INTO public.emr_quality_rule_definitions (rule_code,version,section_code,name,source_text,product_text,standard_points,executor_kind,input_spec,evaluation_spec,action_policy,message_templates,status) VALUES ('HIS-005','2026-10-04-v2','history','复诊未记录治疗后病情变化或治疗效果','复诊未记录治疗后病情变化或治疗效果','',5,'llm','{"paths":["record.patient_snapshot","record.content","record.context"]}'::jsonb,'{"per_item":false,"instruction":"仅复诊，有同一患者可靠既往治疗记录才判断疗效或变化是否记录；无既往记录返回not_evaluable。","scoring_group":""}'::jsonb,'{"save":{"enabled":false,"blocking":false,"blocking_fields":[]},"archive":{"enabled":true,"blocking":false,"blocking_fields":[]},"complete":{"enabled":true,"blocking":false,"blocking_fields":[]}}'::jsonb,'{"save":"发现复诊未记录治疗后病情变化或治疗效果，请根据具体依据核对。","archive":"发现复诊未记录治疗后病情变化或治疗效果，请根据具体依据核对。","complete":"发现复诊未记录治疗后病情变化或治疗效果，请根据具体依据核对。"}'::jsonb,'published') ON CONFLICT (rule_code,version) DO UPDATE SET section_code=EXCLUDED.section_code,name=EXCLUDED.name,source_text=EXCLUDED.source_text,product_text=EXCLUDED.product_text,standard_points=EXCLUDED.standard_points,executor_kind=EXCLUDED.executor_kind,input_spec=EXCLUDED.input_spec,evaluation_spec=EXCLUDED.evaluation_spec,action_policy=EXCLUDED.action_policy,message_templates=EXCLUDED.message_templates,status=EXCLUDED.status,updated_at=NOW();
INSERT INTO public.emr_quality_rule_definitions (rule_code,version,section_code,name,source_text,product_text,standard_points,executor_kind,input_spec,evaluation_spec,action_policy,message_templates,status) VALUES ('HIS-006','2026-10-04-v2','history','未确诊复诊病历未记录新出现的症状体征或补充鉴别诊断资料','未确诊复诊病历未记录新出现的症状体征或补充鉴别诊断资料','',3,'llm','{"paths":["record.patient_snapshot","record.content","record.context"]}'::jsonb,'{"per_item":false,"instruction":"仅未明确诊断的复诊，有既往记录及明确新情况证据才检查未记录的新症状或补充鉴别资料。","scoring_group":""}'::jsonb,'{"save":{"enabled":false,"blocking":false,"blocking_fields":[]},"archive":{"enabled":true,"blocking":false,"blocking_fields":[]},"complete":{"enabled":true,"blocking":false,"blocking_fields":[]}}'::jsonb,'{"save":"发现未确诊复诊病历未记录新出现的症状体征或补充鉴别诊断资料，请根据具体依据核对。","archive":"发现未确诊复诊病历未记录新出现的症状体征或补充鉴别诊断资料，请根据具体依据核对。","complete":"发现未确诊复诊病历未记录新出现的症状体征或补充鉴别诊断资料，请根据具体依据核对。"}'::jsonb,'published') ON CONFLICT (rule_code,version) DO UPDATE SET section_code=EXCLUDED.section_code,name=EXCLUDED.name,source_text=EXCLUDED.source_text,product_text=EXCLUDED.product_text,standard_points=EXCLUDED.standard_points,executor_kind=EXCLUDED.executor_kind,input_spec=EXCLUDED.input_spec,evaluation_spec=EXCLUDED.evaluation_spec,action_policy=EXCLUDED.action_policy,message_templates=EXCLUDED.message_templates,status=EXCLUDED.status,updated_at=NOW();
INSERT INTO public.emr_quality_rule_definitions (rule_code,version,section_code,name,source_text,product_text,standard_points,executor_kind,input_spec,evaluation_spec,action_policy,message_templates,status) VALUES ('HIS-007','2026-10-04-v2','history','缺少与本次疾病相关的既往史及其他病史记录','缺少与本次疾病相关的既往史及其他病史记录','',5,'llm','{"paths":["record.patient_snapshot","record.content","record.context"]}'::jsonb,'{"per_item":false,"instruction":"依据本次病情指出真正相关的既往史、婚育史或家族史缺项；不能机械要求全部。过敏史由HIS-009单独处理。","scoring_group":"history_missing"}'::jsonb,'{"save":{"enabled":false,"blocking":false,"blocking_fields":[]},"archive":{"enabled":true,"blocking":false,"blocking_fields":[]},"complete":{"enabled":true,"blocking":false,"blocking_fields":[]}}'::jsonb,'{"save":"发现缺少与本次疾病相关的既往史及其他病史记录，请根据具体依据核对。","archive":"发现缺少与本次疾病相关的既往史及其他病史记录，请根据具体依据核对。","complete":"发现缺少与本次疾病相关的既往史及其他病史记录，请根据具体依据核对。"}'::jsonb,'published') ON CONFLICT (rule_code,version) DO UPDATE SET section_code=EXCLUDED.section_code,name=EXCLUDED.name,source_text=EXCLUDED.source_text,product_text=EXCLUDED.product_text,standard_points=EXCLUDED.standard_points,executor_kind=EXCLUDED.executor_kind,input_spec=EXCLUDED.input_spec,evaluation_spec=EXCLUDED.evaluation_spec,action_policy=EXCLUDED.action_policy,message_templates=EXCLUDED.message_templates,status=EXCLUDED.status,updated_at=NOW();
INSERT INTO public.emr_quality_rule_definitions (rule_code,version,section_code,name,source_text,product_text,standard_points,executor_kind,input_spec,evaluation_spec,action_policy,message_templates,status) VALUES ('HIS-008','2026-10-04-v2','history','与本次疾病相关的既往史及其他病史有重要不足','与本次疾病相关的既往史及其他病史有重要不足','',3,'llm','{"paths":["record.patient_snapshot","record.content","record.context"]}'::jsonb,'{"per_item":true,"instruction":"相关病史已有内容但重要不足；具体缺项必须有依据。过敏史不足由HIS-010处理，避免重复。","scoring_group":""}'::jsonb,'{"save":{"enabled":false,"blocking":false,"blocking_fields":[]},"archive":{"enabled":true,"blocking":false,"blocking_fields":[]},"complete":{"enabled":true,"blocking":false,"blocking_fields":[]}}'::jsonb,'{"save":"发现与本次疾病相关的既往史及其他病史有重要不足，请根据具体依据核对。","archive":"发现与本次疾病相关的既往史及其他病史有重要不足，请根据具体依据核对。","complete":"发现与本次疾病相关的既往史及其他病史有重要不足，请根据具体依据核对。"}'::jsonb,'published') ON CONFLICT (rule_code,version) DO UPDATE SET section_code=EXCLUDED.section_code,name=EXCLUDED.name,source_text=EXCLUDED.source_text,product_text=EXCLUDED.product_text,standard_points=EXCLUDED.standard_points,executor_kind=EXCLUDED.executor_kind,input_spec=EXCLUDED.input_spec,evaluation_spec=EXCLUDED.evaluation_spec,action_policy=EXCLUDED.action_policy,message_templates=EXCLUDED.message_templates,status=EXCLUDED.status,updated_at=NOW();
INSERT INTO public.emr_quality_rule_definitions (rule_code,version,section_code,name,source_text,product_text,standard_points,executor_kind,input_spec,evaluation_spec,action_policy,message_templates,status) VALUES ('HIS-009','2026-10-04-v2','history','病史中的过敏史没有明确记录','缺少与本次疾病相关的既往史及其他病史记录','病史中的过敏史必须明确记录；没有过敏史也要明确填写。',5,'program','{"paths":["record.content.allergy_history"]}'::jsonb,'{"per_item":false,"uncertain":["待确认","尚未询问","不详"],"instruction":"病史过敏史范围包括药物、食物及其他过敏。写无过敏史可通过；未询问或待确认不通过。","scoring_group":"history_missing","accepted_negative":["无过敏史","无药物过敏史","否认过敏史"]}'::jsonb,'{"save":{"enabled":true},"archive":{"enabled":true,"blocking":true},"complete":{"enabled":true,"blocking":true}}'::jsonb,'{"save":"请明确记录过敏史；没有过敏史也请明确填写。","archive":"没有明确记录过敏史，不能归档。","complete":"没有明确记录过敏史，不能完成病历。"}'::jsonb,'published') ON CONFLICT (rule_code,version) DO UPDATE SET section_code=EXCLUDED.section_code,name=EXCLUDED.name,source_text=EXCLUDED.source_text,product_text=EXCLUDED.product_text,standard_points=EXCLUDED.standard_points,executor_kind=EXCLUDED.executor_kind,input_spec=EXCLUDED.input_spec,evaluation_spec=EXCLUDED.evaluation_spec,action_policy=EXCLUDED.action_policy,message_templates=EXCLUDED.message_templates,status=EXCLUDED.status,updated_at=NOW();
INSERT INTO public.emr_quality_rule_definitions (rule_code,version,section_code,name,source_text,product_text,standard_points,executor_kind,input_spec,evaluation_spec,action_policy,message_templates,status) VALUES ('HIS-010','2026-10-04-v2','history','病史中的过敏史记录不完整或不具体','病史中的过敏史记录不完整或不具体','',3,'llm','{"paths":["record.patient_snapshot","record.content","record.context"]}'::jsonb,'{"per_item":true,"instruction":"已有过敏记录但不具体，指出具体缺少的过敏类型或过敏原。明确无过敏史不得扣分。","scoring_group":""}'::jsonb,'{"save":{"enabled":false,"blocking":false,"blocking_fields":[]},"archive":{"enabled":true,"blocking":false,"blocking_fields":[]},"complete":{"enabled":true,"blocking":false,"blocking_fields":[]}}'::jsonb,'{"save":"发现病史中的过敏史记录不完整或不具体，请根据具体依据核对。","archive":"发现病史中的过敏史记录不完整或不具体，请根据具体依据核对。","complete":"发现病史中的过敏史记录不完整或不具体，请根据具体依据核对。"}'::jsonb,'published') ON CONFLICT (rule_code,version) DO UPDATE SET section_code=EXCLUDED.section_code,name=EXCLUDED.name,source_text=EXCLUDED.source_text,product_text=EXCLUDED.product_text,standard_points=EXCLUDED.standard_points,executor_kind=EXCLUDED.executor_kind,input_spec=EXCLUDED.input_spec,evaluation_spec=EXCLUDED.evaluation_spec,action_policy=EXCLUDED.action_policy,message_templates=EXCLUDED.message_templates,status=EXCLUDED.status,updated_at=NOW();
INSERT INTO public.emr_quality_rule_definitions (rule_code,version,section_code,name,source_text,product_text,standard_points,executor_kind,input_spec,evaluation_spec,action_policy,message_templates,status) VALUES ('TEST-001','2026-10-04-v2','auxiliary_exam','初诊或复诊必需的检查缺一项','初诊或复诊必需的检查缺一项','',2,'llm','{"paths":["record.patient_snapshot","record.content","record.context"]}'::jsonb,'{"per_item":false,"instruction":"依据本次病情和已明确检查建议指出具体缺少的必要检查；没有明确适用依据不强行要求影像或化验。","scoring_group":""}'::jsonb,'{"save":{"enabled":false,"blocking":false,"blocking_fields":[]},"archive":{"enabled":true,"blocking":false,"blocking_fields":[]},"complete":{"enabled":true,"blocking":false,"blocking_fields":[]}}'::jsonb,'{"save":"发现初诊或复诊必需的检查缺一项，请根据具体依据核对。","archive":"发现初诊或复诊必需的检查缺一项，请根据具体依据核对。","complete":"发现初诊或复诊必需的检查缺一项，请根据具体依据核对。"}'::jsonb,'published') ON CONFLICT (rule_code,version) DO UPDATE SET section_code=EXCLUDED.section_code,name=EXCLUDED.name,source_text=EXCLUDED.source_text,product_text=EXCLUDED.product_text,standard_points=EXCLUDED.standard_points,executor_kind=EXCLUDED.executor_kind,input_spec=EXCLUDED.input_spec,evaluation_spec=EXCLUDED.evaluation_spec,action_policy=EXCLUDED.action_policy,message_templates=EXCLUDED.message_templates,status=EXCLUDED.status,updated_at=NOW();
INSERT INTO public.emr_quality_rule_definitions (rule_code,version,section_code,name,source_text,product_text,standard_points,executor_kind,input_spec,evaluation_spec,action_policy,message_templates,status) VALUES ('TEST-002','2026-10-04-v2','auxiliary_exam','相关检查结果未纳入病历记录','相关检查结果未纳入病历记录','',3,'manual','{"paths":["record.patient_snapshot","record.content","record.context"]}'::jsonb,'{"per_item":false,"instruction":"必须有原始检查结果和就诊关联；系统没有此数据时返回not_evaluable，不推断未记录。","scoring_group":""}'::jsonb,'{"save":{"enabled":false,"blocking":false,"blocking_fields":[]},"archive":{"enabled":true,"blocking":false,"blocking_fields":[]},"complete":{"enabled":true,"blocking":false,"blocking_fields":[]}}'::jsonb,'{"save":"发现相关检查结果未纳入病历记录，请根据具体依据核对。","archive":"发现相关检查结果未纳入病历记录，请根据具体依据核对。","complete":"发现相关检查结果未纳入病历记录，请根据具体依据核对。"}'::jsonb,'published') ON CONFLICT (rule_code,version) DO UPDATE SET section_code=EXCLUDED.section_code,name=EXCLUDED.name,source_text=EXCLUDED.source_text,product_text=EXCLUDED.product_text,standard_points=EXCLUDED.standard_points,executor_kind=EXCLUDED.executor_kind,input_spec=EXCLUDED.input_spec,evaluation_spec=EXCLUDED.evaluation_spec,action_policy=EXCLUDED.action_policy,message_templates=EXCLUDED.message_templates,status=EXCLUDED.status,updated_at=NOW();
INSERT INTO public.emr_quality_rule_definitions (rule_code,version,section_code,name,source_text,product_text,standard_points,executor_kind,input_spec,evaluation_spec,action_policy,message_templates,status) VALUES ('TEST-003','2026-10-04-v2','auxiliary_exam','相关检查结果记录遗漏或不准确','相关检查结果记录遗漏或不准确','',2,'manual','{"paths":["record.patient_snapshot","record.content","record.context"]}'::jsonb,'{"per_item":false,"instruction":"必须对照原始结果与本次记录两端文本；数据源不存在时不能判定。","scoring_group":""}'::jsonb,'{"save":{"enabled":false,"blocking":false,"blocking_fields":[]},"archive":{"enabled":true,"blocking":false,"blocking_fields":[]},"complete":{"enabled":true,"blocking":false,"blocking_fields":[]}}'::jsonb,'{"save":"发现相关检查结果记录遗漏或不准确，请根据具体依据核对。","archive":"发现相关检查结果记录遗漏或不准确，请根据具体依据核对。","complete":"发现相关检查结果记录遗漏或不准确，请根据具体依据核对。"}'::jsonb,'published') ON CONFLICT (rule_code,version) DO UPDATE SET section_code=EXCLUDED.section_code,name=EXCLUDED.name,source_text=EXCLUDED.source_text,product_text=EXCLUDED.product_text,standard_points=EXCLUDED.standard_points,executor_kind=EXCLUDED.executor_kind,input_spec=EXCLUDED.input_spec,evaluation_spec=EXCLUDED.evaluation_spec,action_policy=EXCLUDED.action_policy,message_templates=EXCLUDED.message_templates,status=EXCLUDED.status,updated_at=NOW();
INSERT INTO public.emr_quality_rule_definitions (rule_code,version,section_code,name,source_text,product_text,standard_points,executor_kind,input_spec,evaluation_spec,action_policy,message_templates,status) VALUES ('TRT-001','2026-10-04-v2','treatment','缺治疗意见','缺治疗意见','',20,'program','{"paths":["record.patient_snapshot","record.content","record.context"]}'::jsonb,'{"per_item":false,"instruction":"治疗意见由处方、处置、医嘱及治疗字段组成；任一已有实际内容不得判为缺治疗意见。","scoring_group":""}'::jsonb,'{"save":{"enabled":true,"blocking":false,"blocking_fields":[]},"archive":{"enabled":true,"blocking":true,"blocking_fields":[]},"complete":{"enabled":true,"blocking":true,"blocking_fields":[]}}'::jsonb,'{"save":"病历没有记录治疗意见，请补充。","archive":"没有治疗意见，不能归档。","complete":"没有治疗意见，不能完成病历。"}'::jsonb,'published') ON CONFLICT (rule_code,version) DO UPDATE SET section_code=EXCLUDED.section_code,name=EXCLUDED.name,source_text=EXCLUDED.source_text,product_text=EXCLUDED.product_text,standard_points=EXCLUDED.standard_points,executor_kind=EXCLUDED.executor_kind,input_spec=EXCLUDED.input_spec,evaluation_spec=EXCLUDED.evaluation_spec,action_policy=EXCLUDED.action_policy,message_templates=EXCLUDED.message_templates,status=EXCLUDED.status,updated_at=NOW();
INSERT INTO public.emr_quality_rule_definitions (rule_code,version,section_code,name,source_text,product_text,standard_points,executor_kind,input_spec,evaluation_spec,action_policy,message_templates,status) VALUES ('TRT-002','2026-10-04-v2','treatment','治疗与诊断或病史不一致','各项治疗措施不合理、不正确或不及时','同时检查处方、处置和医嘱与诊断、现病史及检查结果的关系。',3,'llm','{"paths":["record.patient_snapshot","record.content","record.context"]}'::jsonb,'{"per_item":true,"instruction":"必须同时检查处方、处置、医嘱与诊断、现病史和必要检查。没有处方时仍检查处置和医嘱。若治疗目标、身体部位或疾病与诊断明显不一致，返回problem；引用治疗内容和诊断或病史两端原文。资料不足返回not_evaluable。","scoring_group":"","required_evidence_sources":["record.content.prescription","record.content.disposition","record.content.medical_advice","record.content.diagnosis","record.content.present_illness"]}'::jsonb,'{"save":{"enabled":false,"blocking":false,"blocking_fields":[]},"archive":{"enabled":true,"blocking":false,"blocking_fields":[]},"complete":{"enabled":true,"blocking":false,"blocking_fields":[]}}'::jsonb,'{"save":"治疗内容与诊断或病史可能不一致，请核对具体依据。","archive":"治疗内容与诊断或病史不一致，请核对具体依据。","complete":"治疗内容与诊断或病史不一致，请核对具体依据。"}'::jsonb,'published') ON CONFLICT (rule_code,version) DO UPDATE SET section_code=EXCLUDED.section_code,name=EXCLUDED.name,source_text=EXCLUDED.source_text,product_text=EXCLUDED.product_text,standard_points=EXCLUDED.standard_points,executor_kind=EXCLUDED.executor_kind,input_spec=EXCLUDED.input_spec,evaluation_spec=EXCLUDED.evaluation_spec,action_policy=EXCLUDED.action_policy,message_templates=EXCLUDED.message_templates,status=EXCLUDED.status,updated_at=NOW();
INSERT INTO public.emr_quality_rule_definitions (rule_code,version,section_code,name,source_text,product_text,standard_points,executor_kind,input_spec,evaluation_spec,action_policy,message_templates,status) VALUES ('TRT-003','2026-10-04-v2','treatment','处方书写未按规则要求','处方书写未按规则要求','',3,'hybrid','{"paths":["record.patient_snapshot","record.content","record.context"]}'::jsonb,'{"per_item":false,"instruction":"处方需有药物全称、用量和用法；结构化输入程序检查，文本由模型提取具体药物和缺项。未开药不扣分；一般治疗建议不是具体药物处方不得凭空找药名。","scoring_group":""}'::jsonb,'{"save":{"enabled":true,"blocking":false,"blocking_fields":[]},"archive":{"enabled":true,"blocking":false,"blocking_fields":[]},"complete":{"enabled":true,"blocking":false,"blocking_fields":[]}}'::jsonb,'{"save":"发现处方书写未按规则要求，请根据具体依据核对。","archive":"发现处方书写未按规则要求，请根据具体依据核对。","complete":"发现处方书写未按规则要求，请根据具体依据核对。"}'::jsonb,'published') ON CONFLICT (rule_code,version) DO UPDATE SET section_code=EXCLUDED.section_code,name=EXCLUDED.name,source_text=EXCLUDED.source_text,product_text=EXCLUDED.product_text,standard_points=EXCLUDED.standard_points,executor_kind=EXCLUDED.executor_kind,input_spec=EXCLUDED.input_spec,evaluation_spec=EXCLUDED.evaluation_spec,action_policy=EXCLUDED.action_policy,message_templates=EXCLUDED.message_templates,status=EXCLUDED.status,updated_at=NOW();
INSERT INTO public.emr_quality_rule_definitions (rule_code,version,section_code,name,source_text,product_text,standard_points,executor_kind,input_spec,evaluation_spec,action_policy,message_templates,status) VALUES ('TRT-004','2026-10-04-v2','treatment','处方与病历中医嘱不相符','处方与病历中医嘱不相符','',5,'llm','{"paths":["record.patient_snapshot","record.content","record.context"]}'::jsonb,'{"per_item":false,"instruction":"处方与医嘱明确药物、剂量、用法、频次存在矛盾才判定，两端原文必需；医嘱未重复处方不等于冲突。","scoring_group":""}'::jsonb,'{"save":{"enabled":false,"blocking":false,"blocking_fields":[]},"archive":{"enabled":true,"blocking":false,"blocking_fields":[]},"complete":{"enabled":true,"blocking":false,"blocking_fields":[]}}'::jsonb,'{"save":"发现处方与病历中医嘱不相符，请根据具体依据核对。","archive":"发现处方与病历中医嘱不相符，请根据具体依据核对。","complete":"发现处方与病历中医嘱不相符，请根据具体依据核对。"}'::jsonb,'published') ON CONFLICT (rule_code,version) DO UPDATE SET section_code=EXCLUDED.section_code,name=EXCLUDED.name,source_text=EXCLUDED.source_text,product_text=EXCLUDED.product_text,standard_points=EXCLUDED.standard_points,executor_kind=EXCLUDED.executor_kind,input_spec=EXCLUDED.input_spec,evaluation_spec=EXCLUDED.evaluation_spec,action_policy=EXCLUDED.action_policy,message_templates=EXCLUDED.message_templates,status=EXCLUDED.status,updated_at=NOW();
INSERT INTO public.emr_quality_rule_definitions (rule_code,version,section_code,name,source_text,product_text,standard_points,executor_kind,input_spec,evaluation_spec,action_policy,message_templates,status) VALUES ('TRT-005','2026-10-04-v2','treatment','未注明住院原因','未注明住院原因','',5,'llm','{"paths":["record.patient_snapshot","record.content","record.context"]}'::jsonb,'{"per_item":false,"instruction":"明确住院建议且没有原因才提示；与建议相邻已说明住院理由不得判缺失；没有住院建议不适用。","scoring_group":""}'::jsonb,'{"save":{"enabled":false,"blocking":false,"blocking_fields":[]},"archive":{"enabled":true,"blocking":false,"blocking_fields":[]},"complete":{"enabled":true,"blocking":false,"blocking_fields":[]}}'::jsonb,'{"save":"发现未注明住院原因，请根据具体依据核对。","archive":"发现未注明住院原因，请根据具体依据核对。","complete":"发现未注明住院原因，请根据具体依据核对。"}'::jsonb,'published') ON CONFLICT (rule_code,version) DO UPDATE SET section_code=EXCLUDED.section_code,name=EXCLUDED.name,source_text=EXCLUDED.source_text,product_text=EXCLUDED.product_text,standard_points=EXCLUDED.standard_points,executor_kind=EXCLUDED.executor_kind,input_spec=EXCLUDED.input_spec,evaluation_spec=EXCLUDED.evaluation_spec,action_policy=EXCLUDED.action_policy,message_templates=EXCLUDED.message_templates,status=EXCLUDED.status,updated_at=NOW();
INSERT INTO public.emr_quality_rule_definitions (rule_code,version,section_code,name,source_text,product_text,standard_points,executor_kind,input_spec,evaluation_spec,action_policy,message_templates,status) VALUES ('TRT-006','2026-10-04-v2','treatment','出具医学证明书与本次疾病不相符','出具医学证明书与本次疾病不相符','',5,'manual','{"paths":["record.patient_snapshot","record.content","record.context"]}'::jsonb,'{"per_item":false,"instruction":"只有具备可关联的医学证明书内容才能两端核对，无数据源不能判定缺失。","scoring_group":""}'::jsonb,'{"save":{"enabled":false,"blocking":false,"blocking_fields":[]},"archive":{"enabled":true,"blocking":false,"blocking_fields":[]},"complete":{"enabled":true,"blocking":false,"blocking_fields":[]}}'::jsonb,'{"save":"发现出具医学证明书与本次疾病不相符，请根据具体依据核对。","archive":"发现出具医学证明书与本次疾病不相符，请根据具体依据核对。","complete":"发现出具医学证明书与本次疾病不相符，请根据具体依据核对。"}'::jsonb,'published') ON CONFLICT (rule_code,version) DO UPDATE SET section_code=EXCLUDED.section_code,name=EXCLUDED.name,source_text=EXCLUDED.source_text,product_text=EXCLUDED.product_text,standard_points=EXCLUDED.standard_points,executor_kind=EXCLUDED.executor_kind,input_spec=EXCLUDED.input_spec,evaluation_spec=EXCLUDED.evaluation_spec,action_policy=EXCLUDED.action_policy,message_templates=EXCLUDED.message_templates,status=EXCLUDED.status,updated_at=NOW();
INSERT INTO public.emr_quality_rule_definitions (rule_code,version,section_code,name,source_text,product_text,standard_points,executor_kind,input_spec,evaluation_spec,action_policy,message_templates,status) VALUES ('TRT-007','2026-10-04-v2','treatment','缺少特殊检查、治疗或手术的操作记录','缺少特殊检查、治疗或手术的操作记录','',10,'manual','{"paths":["record.patient_snapshot","record.content","record.context"]}'::jsonb,'{"per_item":false,"instruction":"需要已实施的特殊项目及关联操作记录，无记录数据源时不能推断缺操作记录。","scoring_group":""}'::jsonb,'{"save":{"enabled":false,"blocking":false,"blocking_fields":[]},"archive":{"enabled":true,"blocking":false,"blocking_fields":[]},"complete":{"enabled":true,"blocking":false,"blocking_fields":[]}}'::jsonb,'{"save":"发现缺少特殊检查、治疗或手术的操作记录，请根据具体依据核对。","archive":"发现缺少特殊检查、治疗或手术的操作记录，请根据具体依据核对。","complete":"发现缺少特殊检查、治疗或手术的操作记录，请根据具体依据核对。"}'::jsonb,'published') ON CONFLICT (rule_code,version) DO UPDATE SET section_code=EXCLUDED.section_code,name=EXCLUDED.name,source_text=EXCLUDED.source_text,product_text=EXCLUDED.product_text,standard_points=EXCLUDED.standard_points,executor_kind=EXCLUDED.executor_kind,input_spec=EXCLUDED.input_spec,evaluation_spec=EXCLUDED.evaluation_spec,action_policy=EXCLUDED.action_policy,message_templates=EXCLUDED.message_templates,status=EXCLUDED.status,updated_at=NOW();
INSERT INTO public.emr_quality_rule_definitions (rule_code,version,section_code,name,source_text,product_text,standard_points,executor_kind,input_spec,evaluation_spec,action_policy,message_templates,status) VALUES ('TRT-008','2026-10-04-v2','treatment','特殊检查、治疗或手术缺少患者签名','特殊检查、治疗或手术缺少患者签名','',5,'manual','{"paths":["record.patient_snapshot","record.content","record.context"]}'::jsonb,'{"per_item":false,"instruction":"患者签名需要可验证的知情同意记录，不能用医生登录代替患者签名；数据源不存在时not_evaluable。","scoring_group":""}'::jsonb,'{"save":{"enabled":false,"blocking":false,"blocking_fields":[]},"archive":{"enabled":true,"blocking":false,"blocking_fields":[]},"complete":{"enabled":true,"blocking":false,"blocking_fields":[]}}'::jsonb,'{"save":"发现特殊检查、治疗或手术缺少患者签名，请根据具体依据核对。","archive":"发现特殊检查、治疗或手术缺少患者签名，请根据具体依据核对。","complete":"发现特殊检查、治疗或手术缺少患者签名，请根据具体依据核对。"}'::jsonb,'published') ON CONFLICT (rule_code,version) DO UPDATE SET section_code=EXCLUDED.section_code,name=EXCLUDED.name,source_text=EXCLUDED.source_text,product_text=EXCLUDED.product_text,standard_points=EXCLUDED.standard_points,executor_kind=EXCLUDED.executor_kind,input_spec=EXCLUDED.input_spec,evaluation_spec=EXCLUDED.evaluation_spec,action_policy=EXCLUDED.action_policy,message_templates=EXCLUDED.message_templates,status=EXCLUDED.status,updated_at=NOW();
INSERT INTO public.emr_quality_rule_definitions (rule_code,version,section_code,name,source_text,product_text,standard_points,executor_kind,input_spec,evaluation_spec,action_policy,message_templates,status) VALUES ('TRT-009','2026-10-04-v2','treatment','缺少应当向患者交代的重要事项记录','缺少应当向患者交代的重要事项记录','',5,'llm','{"paths":["record.patient_snapshot","record.content","record.context"]}'::jsonb,'{"per_item":false,"instruction":"从已明确疾病处置或具体药物指出需交代的重要事项及依据；不能每例机械要求所有复诊、休息或注意事项。","scoring_group":""}'::jsonb,'{"save":{"enabled":false,"blocking":false,"blocking_fields":[]},"archive":{"enabled":true,"blocking":false,"blocking_fields":[]},"complete":{"enabled":true,"blocking":false,"blocking_fields":[]}}'::jsonb,'{"save":"发现缺少应当向患者交代的重要事项记录，请根据具体依据核对。","archive":"发现缺少应当向患者交代的重要事项记录，请根据具体依据核对。","complete":"发现缺少应当向患者交代的重要事项记录，请根据具体依据核对。"}'::jsonb,'published') ON CONFLICT (rule_code,version) DO UPDATE SET section_code=EXCLUDED.section_code,name=EXCLUDED.name,source_text=EXCLUDED.source_text,product_text=EXCLUDED.product_text,standard_points=EXCLUDED.standard_points,executor_kind=EXCLUDED.executor_kind,input_spec=EXCLUDED.input_spec,evaluation_spec=EXCLUDED.evaluation_spec,action_policy=EXCLUDED.action_policy,message_templates=EXCLUDED.message_templates,status=EXCLUDED.status,updated_at=NOW();
INSERT INTO public.emr_quality_rule_definitions (rule_code,version,section_code,name,source_text,product_text,standard_points,executor_kind,input_spec,evaluation_spec,action_policy,message_templates,status) VALUES ('TRT-010','2026-10-04-v2','treatment','未按规定报告传染病疫情和肿瘤病例','未按规定报告传染病疫情和肿瘤病例','',4,'manual','{"paths":["record.patient_snapshot","record.content","record.context"]}'::jsonb,'{"per_item":true,"instruction":"需要明确的报告条件和关联报告状态，单凭诊断文字不得推断未上报。","scoring_group":""}'::jsonb,'{"save":{"enabled":false,"blocking":false,"blocking_fields":[]},"archive":{"enabled":true,"blocking":false,"blocking_fields":[]},"complete":{"enabled":true,"blocking":false,"blocking_fields":[]}}'::jsonb,'{"save":"发现未按规定报告传染病疫情和肿瘤病例，请根据具体依据核对。","archive":"发现未按规定报告传染病疫情和肿瘤病例，请根据具体依据核对。","complete":"发现未按规定报告传染病疫情和肿瘤病例，请根据具体依据核对。"}'::jsonb,'published') ON CONFLICT (rule_code,version) DO UPDATE SET section_code=EXCLUDED.section_code,name=EXCLUDED.name,source_text=EXCLUDED.source_text,product_text=EXCLUDED.product_text,standard_points=EXCLUDED.standard_points,executor_kind=EXCLUDED.executor_kind,input_spec=EXCLUDED.input_spec,evaluation_spec=EXCLUDED.evaluation_spec,action_policy=EXCLUDED.action_policy,message_templates=EXCLUDED.message_templates,status=EXCLUDED.status,updated_at=NOW();
INSERT INTO public.emr_quality_rule_set_versions(version,status,scoring_ready,note) VALUES ('2026-10-04-v2','published',TRUE,'统一门诊病历质量标准完整规则集') ON CONFLICT (version) DO UPDATE SET status='published',scoring_ready=TRUE,note=EXCLUDED.note,updated_at=NOW();
INSERT INTO public.emr_score_standards (code,version,name,source_name,source_reference,standard_text,status,published_at) VALUES ('outpatient_record_quality','2017','门诊病历质量评定标准','门诊病历质量评定标准-1.jpg、门诊病历质量评定标准-2.jpg、门诊病历质量评定标准-3.jpg','lingce-web/apps/emr/docs/门诊病历质量评定标准-图片整理稿-2026-09-22.md','
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
','published','2026-09-23T15:36:25.313018+08:00') ON CONFLICT (code,version) DO UPDATE SET name=EXCLUDED.name,source_name=EXCLUDED.source_name,source_reference=EXCLUDED.source_reference,standard_text=EXCLUDED.standard_text,status=EXCLUDED.status,published_at=EXCLUDED.published_at,updated_at=NOW();

INSERT INTO public.compliance_rules (rule_code,rule_name,subject,scene_codes,role_codes,version,status,is_enabled,judgment_standard,definition,created_by,published_at) VALUES ('communication.guarantee.direct','直接保证诊疗结果','communication','["communication.doctor.outpatient","communication.consultant.consultation"]'::jsonb,'["doctor","doctor_assistant","consultant"]'::jsonb,'20260911.1','published',TRUE,'判断工作人员是否明确把确定的治疗结果、疗效或恢复结果承诺给当前患者，而不是仅说明可能性、条件或一般经验。','{"rule_code":"communication.guarantee.direct","legacy_rule_number":"1.1","name":"直接保证诊疗结果","category":"保证疗效","subject":"communication","status":"published","applicable_rule_sets":["communication.doctor.outpatient","communication.consultant.consultation"],"legal_basis":["《医疗广告管理办法》第七条：不得对诊疗效果作出保证性承诺。"],"judgment_standard":"判断工作人员是否明确把确定的治疗结果、疗效或恢复结果承诺给当前患者，而不是仅说明可能性、条件或一般经验。","trigger_conditions":["保证性表达与治愈、治好、康复、恢复、痊愈或类似结果同时指向当前患者。"],"exclusion_conditions":["不能保证","无法保证","不保证","不敢保证","不能承诺","明确说明个体差异或结果不确定。"],"violation_examples":["我保证你一定能治好。","保证你术后视力能恢复到1.5。","我们保证种植后20年不会掉。"],"compliant_examples":["成功率在95%以上，但因个体差异，不能保证。","大多数患者术后视力可以恢复到1.2-1.5。","如果按医嘱护理，用20年问题不大，但不能保证。"],"fact_definition":"同一上下文中出现保证性表达和诊疗结果表达，并将结果指向当前患者。","required_evidence":["原始转写片段","说话人","起止时间","必要的前后文"],"required_external_facts":[],"indeterminate_boundary":"片段不足以判断说话人是在转述患者观点、引用问题还是本人承诺时，只形成候选发现。","output_type":"machine_finding","review_priority":"review","validation_status":"technical_validation","source_basis":["历史规则1.1","legacy.rule-001.guarantee.direct"]}'::jsonb,0,NOW()) ON CONFLICT (rule_code,version) DO UPDATE SET rule_name=EXCLUDED.rule_name,subject=EXCLUDED.subject,scene_codes=EXCLUDED.scene_codes,role_codes=EXCLUDED.role_codes,status=EXCLUDED.status,is_enabled=EXCLUDED.is_enabled,judgment_standard=EXCLUDED.judgment_standard,definition=EXCLUDED.definition,published_at=EXCLUDED.published_at,updated_at=NOW();
INSERT INTO public.compliance_rules (rule_code,rule_name,subject,scene_codes,role_codes,version,status,is_enabled,judgment_standard,definition,created_by,published_at) VALUES ('communication.guarantee.absolute','绝对化诊疗效果表述','communication','["communication.doctor.outpatient","communication.consultant.consultation"]'::jsonb,'["doctor","doctor_assistant","consultant"]'::jsonb,'20260911.1','published',TRUE,'判断工作人员是否用一定、肯定、绝对、必然等绝对化语言描述诊疗效果、成功率、安全性或恢复结果；必须结合上下文，不能因为单个词出现就形成发现。','{"rule_code":"communication.guarantee.absolute","legacy_rule_number":"1.2","name":"绝对化诊疗效果表述","category":"保证疗效","subject":"communication","status":"published","applicable_rule_sets":["communication.doctor.outpatient","communication.consultant.consultation"],"legal_basis":["《医疗广告管理办法》第七条。"],"judgment_standard":"判断工作人员是否用一定、肯定、绝对、必然等绝对化语言描述诊疗效果、成功率、安全性或恢复结果；必须结合上下文，不能因为单个词出现就形成发现。","trigger_conditions":["绝对化或必然性表达与能、会等结果表达及治愈、治好、康复、恢复、痊愈等结果词形成同一语义。"],"exclusion_conditions":["不能保证一定","不一定能","不一定会","无法保证一定","未必能","不敢肯定","患者提问而工作人员明确否定或限定。"],"violation_examples":["你这个一定能治好。","肯定会恢复的。","绝对能看到1.5。"],"compliant_examples":["有很大概率能治好，但不能说一定。","大多数情况下会恢复，但也有个体差异。","我们会尽力帮你恢复视力。"],"fact_definition":"同一上下文同时出现绝对化表达、结果可能性表达和诊疗结果表达。","required_evidence":["原始转写片段","说话人","起止时间","前后文"],"required_external_facts":[],"indeterminate_boundary":"绝对化词可能出现在否定、引用、患者提问或医生纠正中，不能据此自动定性。","output_type":"machine_finding","review_priority":"review","validation_status":"technical_validation","source_basis":["历史规则1.2","legacy.rule-002.guarantee.implied"]}'::jsonb,0,NOW()) ON CONFLICT (rule_code,version) DO UPDATE SET rule_name=EXCLUDED.rule_name,subject=EXCLUDED.subject,scene_codes=EXCLUDED.scene_codes,role_codes=EXCLUDED.role_codes,status=EXCLUDED.status,is_enabled=EXCLUDED.is_enabled,judgment_standard=EXCLUDED.judgment_standard,definition=EXCLUDED.definition,published_at=EXCLUDED.published_at,updated_at=NOW();
INSERT INTO public.compliance_rules (rule_code,rule_name,subject,scene_codes,role_codes,version,status,is_enabled,judgment_standard,definition,created_by,published_at) VALUES ('communication.guarantee.case','以案例暗示当前患者结果','communication','["communication.doctor.outpatient","communication.consultant.consultation"]'::jsonb,'["doctor","doctor_assistant","consultant"]'::jsonb,'20260911.1','published',TRUE,'判断工作人员是否把其他患者的成功案例、数量或结果直接推导为当前患者也会获得相同结果。','{"rule_code":"communication.guarantee.case","legacy_rule_number":"1.3","name":"以案例暗示当前患者结果","category":"保证疗效","subject":"communication","status":"published","applicable_rule_sets":["communication.doctor.outpatient","communication.consultant.consultation"],"legal_basis":["《医疗广告管理办法》第七条；反不正当竞争法关于引人误解宣传的相关规定。"],"judgment_standard":"判断工作人员是否把其他患者的成功案例、数量或结果直接推导为当前患者也会获得相同结果。","trigger_conditions":["成功案例或既往结果与“你也会、你也能、你也一样、没一个失败、全部成功、百分之百成功”等当前患者结果暗示同时出现。"],"exclusion_conditions":["每个人情况不同","因人而异","不能保证","不代表","不一定","仅客观介绍案例且没有推导当前患者结果。"],"violation_examples":["上个月张女士做完现在1.5，你也会一样的。","我们做过800例，没一个失败的，你放心。"],"compliant_examples":["上个月张女士术后恢复得不错，但每个人情况不同。","我们做过800例，成功率很高，但不能说100%成功。"],"fact_definition":"片段同时包含成功案例或既往结果引用，以及对当前患者结果的一致性暗示或绝对成功表述。","required_evidence":["原始转写片段","说话人","起止时间","可取得时的上下文"],"required_external_facts":[],"indeterminate_boundary":"引用案例本身不等于违规；无法判断案例真实性、患者是否主动询问或是否说明个体差异时，只作候选。","output_type":"machine_finding","review_priority":"review","validation_status":"test_material_required","source_basis":["历史规则1.3","legacy.rule-003.guarantee.case"]}'::jsonb,0,NOW()) ON CONFLICT (rule_code,version) DO UPDATE SET rule_name=EXCLUDED.rule_name,subject=EXCLUDED.subject,scene_codes=EXCLUDED.scene_codes,role_codes=EXCLUDED.role_codes,status=EXCLUDED.status,is_enabled=EXCLUDED.is_enabled,judgment_standard=EXCLUDED.judgment_standard,definition=EXCLUDED.definition,published_at=EXCLUDED.published_at,updated_at=NOW();
INSERT INTO public.compliance_rules (rule_code,rule_name,subject,scene_codes,role_codes,version,status,is_enabled,judgment_standard,definition,created_by,published_at) VALUES ('communication.guarantee.duration','无条件承诺疗效维持时间','communication','["communication.doctor.outpatient","communication.consultant.consultation"]'::jsonb,'["doctor","doctor_assistant","consultant"]'::jsonb,'20260911.1','published',TRUE,'判断工作人员是否把疗效、器械效果或服务结果无条件承诺为长期、终身、永久或固定年限。','{"rule_code":"communication.guarantee.duration","legacy_rule_number":"1.4","name":"无条件承诺疗效维持时间","category":"保证疗效","subject":"communication","status":"published","applicable_rule_sets":["communication.doctor.outpatient","communication.consultant.consultation"],"legal_basis":["《医疗广告管理办法》第七条。"],"judgment_standard":"判断工作人员是否把疗效、器械效果或服务结果无条件承诺为长期、终身、永久或固定年限。","trigger_conditions":["一辈子、终身、永久、20年、几十年等时间表达与能用、有效、不会掉、不用换、一直用、维持等无条件结果表达同时出现。"],"exclusion_conditions":["护理得当","理论上","可能","因人而异","需要复查","不保证","仅说明理论寿命而未承诺患者结果。"],"violation_examples":["种植牙能用一辈子。","保证20年不会掉。","做完ICL终身有效。"],"compliant_examples":["如果护理得当，种植牙可以用很多年，有患者用了20年以上。","ICL晶体理论上可以长期使用，但也可能因某些原因需要取出。"],"fact_definition":"同一片段同时出现长期时长表达和无条件效果、使用或维持结果表达。","required_evidence":["原始转写片段","说话人","起止时间"],"required_external_facts":[],"indeterminate_boundary":"客观经验、条件性说明或器械理论寿命不当然构成承诺，必须结合上下文复核。","output_type":"machine_finding","review_priority":"review","validation_status":"test_material_required","source_basis":["历史规则1.4","legacy.rule-004.guarantee.duration"]}'::jsonb,0,NOW()) ON CONFLICT (rule_code,version) DO UPDATE SET rule_name=EXCLUDED.rule_name,subject=EXCLUDED.subject,scene_codes=EXCLUDED.scene_codes,role_codes=EXCLUDED.role_codes,status=EXCLUDED.status,is_enabled=EXCLUDED.is_enabled,judgment_standard=EXCLUDED.judgment_standard,definition=EXCLUDED.definition,published_at=EXCLUDED.published_at,updated_at=NOW();
INSERT INTO public.compliance_rules (rule_code,rule_name,subject,scene_codes,role_codes,version,status,is_enabled,judgment_standard,definition,created_by,published_at) VALUES ('communication.misleading.exclusivity','绝对化疗效、设备或方案排他宣传','communication','["communication.consultant.consultation"]'::jsonb,'["consultant"]'::jsonb,'20260911.1','published',TRUE,'判断咨询师是否把具体诊疗方案、技术、设备或机构表述为唯一、最好、最佳、独家或全国唯一。','{"rule_code":"communication.misleading.exclusivity","legacy_rule_number":"2.1","name":"绝对化疗效、设备或方案排他宣传","category":"诱导消费","subject":"communication","status":"published","applicable_rule_sets":["communication.consultant.consultation"],"legal_basis":["《反不正当竞争法》第八条；《医疗广告管理办法》关于保证性和引人误解宣传的规定。"],"judgment_standard":"判断咨询师是否把具体诊疗方案、技术、设备或机构表述为唯一、最好、最佳、独家或全国唯一。","trigger_conditions":["最好、最佳、唯一、独家、全国唯一等排他表达与方案、治疗、方法、技术、设备、医院或机构对象同时出现。"],"exclusion_conditions":["之一","各有优势","未必","不一定","仅陈述客观配置或明确限定比较范围。"],"violation_examples":["这是最好的治疗方案。","唯一能治愈的方法。","我们是全国唯一引进这个设备的。"],"compliant_examples":["这是目前常用的治疗方案之一。","这个方法效果不错，但也有其他选择。","我们引进了这个设备，在本地区较为先进。"],"fact_definition":"同一片段同时出现绝对化、排他性表达和被推广的方案、技术、设备或机构对象。","required_evidence":["原始转写片段","说话人","起止时间","可取得时的宣传或设备事实"],"required_external_facts":[],"indeterminate_boundary":"医生基于病情进行医学比较不直接适用本规则；咨询师候选也不自动确认虚假宣传。","output_type":"machine_finding","review_priority":"review","validation_status":"test_material_required","source_basis":["历史规则2.1","legacy.rule-005.misleading.exaggerate"]}'::jsonb,0,NOW()) ON CONFLICT (rule_code,version) DO UPDATE SET rule_name=EXCLUDED.rule_name,subject=EXCLUDED.subject,scene_codes=EXCLUDED.scene_codes,role_codes=EXCLUDED.role_codes,status=EXCLUDED.status,is_enabled=EXCLUDED.is_enabled,judgment_standard=EXCLUDED.judgment_standard,definition=EXCLUDED.definition,published_at=EXCLUDED.published_at,updated_at=NOW();
INSERT INTO public.compliance_rules (rule_code,rule_name,subject,scene_codes,role_codes,version,status,is_enabled,judgment_standard,definition,created_by,published_at) VALUES ('communication.misleading.disparage','无依据贬低其他方案或机构','communication','["communication.consultant.consultation"]'::jsonb,'["consultant"]'::jsonb,'20260911.1','published',TRUE,'判断咨询师是否把其他医院、医生、方案或传统方法与不行、骗人、太差、没用、落后等贬损表达组合，且没有给出可核验依据。','{"rule_code":"communication.misleading.disparage","legacy_rule_number":"2.2","name":"无依据贬低其他方案或机构","category":"诱导消费","subject":"communication","status":"published","applicable_rule_sets":["communication.consultant.consultation"],"legal_basis":["《反不正当竞争法》第八条。"],"judgment_standard":"判断咨询师是否把其他医院、医生、方案或传统方法与不行、骗人、太差、没用、落后等贬损表达组合，且没有给出可核验依据。","trigger_conditions":["其他医院、其他方案、传统方法、其他医生等对象与明确贬损表达同时出现。"],"exclusion_conditions":["各有优势","不代表","不能一概而论","要看情况","存在可核验的比较依据并明确限定适用条件。"],"violation_examples":["别的医院都不行，只有我们能做。","传统方法效果差，都是骗人的。","那家医院的设备太老了。"],"compliant_examples":["我们在这方面有丰富经验，但其他医院也有各自的优势。","传统方法也有效果，只是我们这个方法更先进一些。","不同医院的设备配置不同，各有特点。"],"fact_definition":"片段同时出现可识别的其他机构、医生或方案对象，以及无依据的贬损表达。","required_evidence":["原始转写片段","说话人","起止时间","可取得时的比较依据"],"required_external_facts":["比较对象的设备、技术或服务事实，视具体表达而定"],"indeterminate_boundary":"医生可能基于病情作医学比较；咨询师候选也需人工判断是否有事实依据。","output_type":"machine_finding","review_priority":"review","validation_status":"test_material_required","source_basis":["历史规则2.2","历史规则6.1","legacy.rule-006.misleading.degrade","legacy.rule-017.other.degrade"]}'::jsonb,0,NOW()) ON CONFLICT (rule_code,version) DO UPDATE SET rule_name=EXCLUDED.rule_name,subject=EXCLUDED.subject,scene_codes=EXCLUDED.scene_codes,role_codes=EXCLUDED.role_codes,status=EXCLUDED.status,is_enabled=EXCLUDED.is_enabled,judgment_standard=EXCLUDED.judgment_standard,definition=EXCLUDED.definition,published_at=EXCLUDED.published_at,updated_at=NOW();
INSERT INTO public.compliance_rules (rule_code,rule_name,subject,scene_codes,role_codes,version,status,is_enabled,judgment_standard,definition,created_by,published_at) VALUES ('communication.misleading.urgency','非医学依据的成交紧迫施压','communication','["communication.consultant.consultation"]'::jsonb,'["consultant"]'::jsonb,'20260911.1','draft',TRUE,'判断时间、名额、涨价、活动或排队表达是否以不真实或不完整的外部事实推动患者立即成交。','{"rule_code":"communication.misleading.urgency","legacy_rule_number":"2.3","name":"非医学依据的成交紧迫施压","category":"诱导消费","subject":"communication","status":"draft","applicable_rule_sets":["communication.consultant.consultation"],"legal_basis":["《反不正当竞争法》第八条；价格和促销信息真实性相关要求。"],"judgment_standard":"判断时间、名额、涨价、活动或排队表达是否以不真实或不完整的外部事实推动患者立即成交。","trigger_conditions":["今天、现在、马上、只剩名额、明天涨价、下月排队等紧迫表达与优惠、活动、名额或预约同时出现。"],"exclusion_conditions":["真实活动且信息完整","真实预约紧张且可核验","只是建议尽早预约而没有虚构后果。"],"violation_examples":["今天不做，明天就涨价了。","现在只剩3个名额了。","这个月不做，下个月就要排队半年。"],"compliant_examples":["我们目前有优惠活动，具体可以咨询前台。","近期预约的患者比较多，建议您尽早决定。","医生的档期比较满，建议提前预约。"],"fact_definition":"录音中的时间、名额、涨价或排队表达与实际活动、价格或预约事实不一致。","required_evidence":["原始转写片段","说话人","起止时间"],"required_external_facts":["活动规则","名额或预约记录","价格或有效期"],"indeterminate_boundary":"录音单独不能证明紧迫表达不真实或不当。","output_type":"cross_source_candidate","review_priority":"review","validation_status":"blocked_by_missing_source","source_basis":["历史规则2.3","legacy.rule-007.misleading.urgency"]}'::jsonb,0,NULL) ON CONFLICT (rule_code,version) DO UPDATE SET rule_name=EXCLUDED.rule_name,subject=EXCLUDED.subject,scene_codes=EXCLUDED.scene_codes,role_codes=EXCLUDED.role_codes,status=EXCLUDED.status,is_enabled=EXCLUDED.is_enabled,judgment_standard=EXCLUDED.judgment_standard,definition=EXCLUDED.definition,published_at=EXCLUDED.published_at,updated_at=NOW();
INSERT INTO public.compliance_rules (rule_code,rule_name,subject,scene_codes,role_codes,version,status,is_enabled,judgment_standard,definition,created_by,published_at) VALUES ('communication.misleading.data_unverified','无法核验的宣传数据','communication','["communication.doctor.outpatient","communication.consultant.consultation"]'::jsonb,'["doctor","doctor_assistant","consultant"]'::jsonb,'20260911.1','draft',TRUE,'判断工作人员是否使用满意度、成功率、无事故、万例等数据，并将其作为可信结果或成交依据，但医院无法提供来源、统计口径或有效期。','{"rule_code":"communication.misleading.data_unverified","legacy_rule_number":"2.4","name":"无法核验的宣传数据","category":"诱导消费","subject":"communication","status":"draft","applicable_rule_sets":["communication.doctor.outpatient","communication.consultant.consultation"],"legal_basis":["《反不正当竞争法》第八条。"],"judgment_standard":"判断工作人员是否使用满意度、成功率、无事故、万例等数据，并将其作为可信结果或成交依据，但医院无法提供来源、统计口径或有效期。","trigger_conditions":["98%满意度、万例无事故、百分之百成功等具体数据与宣传或成交语境同时出现。"],"exclusion_conditions":["有可核验来源、统计口径和有效期","仅表达主观感受且没有伪装成统计数据。"],"violation_examples":["我们的患者满意度98%。","万例手术无一例事故。"],"compliant_examples":["大多数患者对手术效果比较满意。","我们非常重视手术安全，有完善的质控流程。"],"fact_definition":"沟通中使用统计或效果数据，但缺少可核验来源、口径或有效期。","required_evidence":["原始转写片段","说话人","起止时间"],"required_external_facts":["数据来源","统计口径","有效期","必要时的审计或质控记录"],"indeterminate_boundary":"出现数字本身不等于虚假数据；没有外部资料时只能提示待核验。","output_type":"cross_source_candidate","review_priority":"review","validation_status":"blocked_by_missing_source","source_basis":["历史规则2.4","legacy.rule-008.misleading.data"]}'::jsonb,0,NULL) ON CONFLICT (rule_code,version) DO UPDATE SET rule_name=EXCLUDED.rule_name,subject=EXCLUDED.subject,scene_codes=EXCLUDED.scene_codes,role_codes=EXCLUDED.role_codes,status=EXCLUDED.status,is_enabled=EXCLUDED.is_enabled,judgment_standard=EXCLUDED.judgment_standard,definition=EXCLUDED.definition,published_at=EXCLUDED.published_at,updated_at=NOW();
INSERT INTO public.compliance_rules (rule_code,rule_name,subject,scene_codes,role_codes,version,status,is_enabled,judgment_standard,definition,created_by,published_at) VALUES ('communication.fear.exaggeration','夸大风险推动决策','communication','["communication.doctor.outpatient","communication.consultant.consultation"]'::jsonb,'["doctor","doctor_assistant","consultant"]'::jsonb,'20260911.1','published',TRUE,'判断工作人员是否通过夸大疾病严重程度、拖延后果或时间压力推动当前患者立即作出诊疗决定，而不是基于事实进行合理风险告知。','{"rule_code":"communication.fear.exaggeration","legacy_rule_number":"3.1","name":"夸大风险推动决策","category":"制造焦虑","subject":"communication","status":"published","applicable_rule_sets":["communication.doctor.outpatient","communication.consultant.consultation"],"legal_basis":["医疗风险告知和医疗宣传真实性相关要求。"],"judgment_standard":"判断工作人员是否通过夸大疾病严重程度、拖延后果或时间压力推动当前患者立即作出诊疗决定，而不是基于事实进行合理风险告知。","trigger_conditions":["很严重、非常危险、再不做就来不及了、拖不得等严重或紧迫表达指向当前患者决定。"],"exclusion_conditions":["不用担心","不必过度担心","没有那么严重","不严重","有医学检查、病情和处理时限依据。"],"violation_examples":["你这个很严重，再不做就瞎了。","这个必须马上手术，否则会恶化。","你这个拖不得，再拖就完了。"],"compliant_examples":["您的白内障已到中期，建议尽快手术，以免影响日常生活。","这个病情需要及时治疗，但也不用过度紧张。","建议您尽早手术，但具体时间可以和医生商量。"],"fact_definition":"原始转写片段出现可能利用严重后果或时间压力推动决策的表达。","required_evidence":["原始转写片段","说话人","起止时间"],"required_external_facts":["涉及医学严重程度或时限时，应补充病历、检查或医学依据"],"indeterminate_boundary":"严重程度和处理时限需要结合患者病情、检查结果和医学依据；本规则只能提供候选线索。","output_type":"machine_finding","review_priority":"review","validation_status":"technical_validation","source_basis":["历史规则3.1","legacy.rule-009.fear.severity"]}'::jsonb,0,NOW()) ON CONFLICT (rule_code,version) DO UPDATE SET rule_name=EXCLUDED.rule_name,subject=EXCLUDED.subject,scene_codes=EXCLUDED.scene_codes,role_codes=EXCLUDED.role_codes,status=EXCLUDED.status,is_enabled=EXCLUDED.is_enabled,judgment_standard=EXCLUDED.judgment_standard,definition=EXCLUDED.definition,published_at=EXCLUDED.published_at,updated_at=NOW();
INSERT INTO public.compliance_rules (rule_code,rule_name,subject,scene_codes,role_codes,version,status,is_enabled,judgment_standard,definition,created_by,published_at) VALUES ('communication.fear.panic','不做即发生绝对严重后果','communication','["communication.doctor.outpatient","communication.consultant.consultation"]'::jsonb,'["doctor","doctor_assistant","consultant"]'::jsonb,'20260911.1','published',TRUE,'判断工作人员是否把不做某项治疗直接关联失明、全掉光或其他绝对严重后果，并以此推动患者决定。','{"rule_code":"communication.fear.panic","legacy_rule_number":"3.2","name":"不做即发生绝对严重后果","category":"制造焦虑","subject":"communication","status":"published","applicable_rule_sets":["communication.doctor.outpatient","communication.consultant.consultation"],"legal_basis":["医疗风险告知和医疗宣传真实性相关要求。"],"judgment_standard":"判断工作人员是否把不做某项治疗直接关联失明、全掉光或其他绝对严重后果，并以此推动患者决定。","trigger_conditions":["不做、不治疗、不手术、不种牙、不矫正等表达与失明、全掉光、瞎了、恶化或严重后果直接关联。"],"exclusion_conditions":["有可能","定期检查","因人而异","医生评估","需要检查","明确表达风险概率和条件。"],"violation_examples":["不种牙的话，其他牙齿也会松动，最后全掉光。","近视不矫正，度数会越来越深，最后会失明。"],"compliant_examples":["缺牙后，邻牙可能会移位，建议考虑种植修复。","近视度数可能会加深，建议定期检查。"],"fact_definition":"同一片段把不做治疗或矫正与绝对严重后果直接关联。","required_evidence":["原始转写片段","说话人","起止时间"],"required_external_facts":["医生场景下的病历或检查事实","必要时的医学依据"],"indeterminate_boundary":"医学风险说明可能合理，尤其医生场景必须结合病历和检查事实复核，系统不确认夸大病情。","output_type":"machine_finding","review_priority":"review","validation_status":"test_material_required","source_basis":["历史规则3.2","legacy.rule-010.fear.panic"]}'::jsonb,0,NOW()) ON CONFLICT (rule_code,version) DO UPDATE SET rule_name=EXCLUDED.rule_name,subject=EXCLUDED.subject,scene_codes=EXCLUDED.scene_codes,role_codes=EXCLUDED.role_codes,status=EXCLUDED.status,is_enabled=EXCLUDED.is_enabled,judgment_standard=EXCLUDED.judgment_standard,definition=EXCLUDED.definition,published_at=EXCLUDED.published_at,updated_at=NOW();
INSERT INTO public.compliance_rules (rule_code,rule_name,subject,scene_codes,role_codes,version,status,is_enabled,judgment_standard,definition,created_by,published_at) VALUES ('communication.fear.compare','做与不做的夸张后果对比','communication','["communication.doctor.outpatient","communication.consultant.consultation"]'::jsonb,'["doctor","doctor_assistant","consultant"]'::jsonb,'20260911.1','published',TRUE,'判断工作人员是否把治疗后的绝对正面结果与不治疗后的绝对负面结果并列，以夸张对比推动当前患者选择。','{"rule_code":"communication.fear.compare","legacy_rule_number":"3.3","name":"做与不做的夸张后果对比","category":"制造焦虑","subject":"communication","status":"published","applicable_rule_sets":["communication.doctor.outpatient","communication.consultant.consultation"],"legal_basis":["医疗风险告知和医疗宣传真实性相关要求。"],"judgment_standard":"判断工作人员是否把治疗后的绝对正面结果与不治疗后的绝对负面结果并列，以夸张对比推动当前患者选择。","trigger_conditions":["治疗后、做完等正向结果与不做、不治疗等负向结果同时出现，且两边都使用绝对化或极端结果。"],"exclusion_conditions":["可以选择","也可以","个人选择","因人而异","客观说明不同方案的风险和收益。"],"violation_examples":["做了你就能看得清清楚楚；不做你就要戴一辈子眼镜，还会越来越深。"],"compliant_examples":["手术后视力会有改善，但也可以选择继续戴眼镜，这是您的个人选择。"],"fact_definition":"同一片段同时对治疗后和不治疗后的结果作绝对化正负对比。","required_evidence":["原始转写片段","说话人","起止时间"],"required_external_facts":["医生场景下的病历或检查事实","涉及治疗效果时的医学依据"],"indeterminate_boundary":"治疗方案比较可能是必要告知，医生场景尤其需要医学事实复核。","output_type":"machine_finding","review_priority":"review","validation_status":"test_material_required","source_basis":["历史规则3.3","legacy.rule-011.fear.compare"]}'::jsonb,0,NOW()) ON CONFLICT (rule_code,version) DO UPDATE SET rule_name=EXCLUDED.rule_name,subject=EXCLUDED.subject,scene_codes=EXCLUDED.scene_codes,role_codes=EXCLUDED.role_codes,status=EXCLUDED.status,is_enabled=EXCLUDED.is_enabled,judgment_standard=EXCLUDED.judgment_standard,definition=EXCLUDED.definition,published_at=EXCLUDED.published_at,updated_at=NOW();
INSERT INTO public.compliance_rules (rule_code,rule_name,subject,scene_codes,role_codes,version,status,is_enabled,judgment_standard,definition,created_by,published_at) VALUES ('content.price.false_original','虚构原价或虚假优惠','content','["content.pre_publish"]'::jsonb,'[]'::jsonb,'20260911.1','draft',TRUE,'判断内容宣称的原价、市场价或优惠是否与可核验的价格表、活动规则和生效记录不一致。','{"rule_code":"content.price.false_original","legacy_rule_number":"4.1","name":"虚构原价或虚假优惠","category":"价格欺诈","subject":"content","status":"draft","applicable_rule_sets":["content.pre_publish"],"legal_basis":["《价格法》第十四条：不得虚构原价、虚假优惠折价。"],"judgment_standard":"判断内容宣称的原价、市场价或优惠是否与可核验的价格表、活动规则和生效记录不一致。","trigger_conditions":["内容先报高价再宣布大幅优惠，或使用市场价、原价、活动价作为购买依据。"],"exclusion_conditions":["价格和活动有有效记录","活动期限、适用条件和比较价格完整披露。"],"violation_examples":["原价3万，现在活动价1.5万，但实际从未卖过3万。","市场价5万，我们只要2万，但没有市场价依据。"],"compliant_examples":["我们的价格是2万，目前有优惠活动。","不同医院价格不同，我们的价格在XX元左右。"],"fact_definition":"内容宣称的原价、市场价或优惠与可核验价格和活动资料不一致。","required_evidence":["内容版本快照","发布渠道和时间"],"required_external_facts":["价格表","活动规则","价格生效记录"],"indeterminate_boundary":"文字中的原价和优惠不能单独证明价格虚假。","output_type":"content_cross_source_candidate","review_priority":"review","validation_status":"blocked_by_content_source","source_basis":["历史规则4.1","legacy.rule-012.price.fake_original"]}'::jsonb,0,NULL) ON CONFLICT (rule_code,version) DO UPDATE SET rule_name=EXCLUDED.rule_name,subject=EXCLUDED.subject,scene_codes=EXCLUDED.scene_codes,role_codes=EXCLUDED.role_codes,status=EXCLUDED.status,is_enabled=EXCLUDED.is_enabled,judgment_standard=EXCLUDED.judgment_standard,definition=EXCLUDED.definition,published_at=EXCLUDED.published_at,updated_at=NOW();
INSERT INTO public.compliance_rules (rule_code,rule_name,subject,scene_codes,role_codes,version,status,is_enabled,judgment_standard,definition,created_by,published_at) VALUES ('content.price.hidden_fee','隐瞒费用构成','content','["content.pre_publish"]'::jsonb,'[]'::jsonb,'20260911.1','draft',TRUE,'判断内容报价是否遗漏应披露且会影响总价的费用、材料、检查、复查或适用条件。','{"rule_code":"content.price.hidden_fee","legacy_rule_number":"4.2","name":"隐瞒费用构成","category":"价格欺诈","subject":"content","status":"draft","applicable_rule_sets":["content.pre_publish"],"legal_basis":["《价格法》及价格信息明示相关要求。"],"judgment_standard":"判断内容报价是否遗漏应披露且会影响总价的费用、材料、检查、复查或适用条件。","trigger_conditions":["只展示单项低价，而项目实际还包含牙冠、骨粉、检查、复查或其他必要费用。"],"exclusion_conditions":["已明确项目范围和费用构成","已说明其他费用另算及预计总额或区间。"],"violation_examples":["种植牙1万一颗，但不说还要加牙冠、骨粉等费用。","ICL手术2万，但不说术前检查和术后复查费用。"],"compliant_examples":["种植体费用1万，加上牙冠和其他费用，总计约1.5到2万。","手术费用2万，术前检查和术后复查另算，总共约2.5万。"],"fact_definition":"内容中的项目报价遗漏会影响总价的费用构成或适用条件。","required_evidence":["内容版本快照","价格文案所在位置"],"required_external_facts":["项目费用构成","价格表","适用条件"],"indeterminate_boundary":"单一价格文案不能自动证明隐瞒费用。","output_type":"content_cross_source_candidate","review_priority":"review","validation_status":"blocked_by_content_source","source_basis":["历史规则4.2","legacy.rule-013.price.hidden_fee"]}'::jsonb,0,NULL) ON CONFLICT (rule_code,version) DO UPDATE SET rule_name=EXCLUDED.rule_name,subject=EXCLUDED.subject,scene_codes=EXCLUDED.scene_codes,role_codes=EXCLUDED.role_codes,status=EXCLUDED.status,is_enabled=EXCLUDED.is_enabled,judgment_standard=EXCLUDED.judgment_standard,definition=EXCLUDED.definition,published_at=EXCLUDED.published_at,updated_at=NOW();
INSERT INTO public.compliance_rules (rule_code,rule_name,subject,scene_codes,role_codes,version,status,is_enabled,judgment_standard,definition,created_by,published_at) VALUES ('communication.price.on_site_increase','电话低价与到店报价不一致','communication','["communication.consultant.consultation"]'::jsonb,'["consultant"]'::jsonb,'20260911.1','draft',TRUE,'判断首次沟通报价与到店后的相同项目报价是否不一致，且不存在已告知的材料、项目或服务范围差异。','{"rule_code":"communication.price.on_site_increase","legacy_rule_number":"4.3","name":"电话低价与到店报价不一致","category":"价格欺诈","subject":"communication","status":"draft","applicable_rule_sets":["communication.consultant.consultation"],"legal_basis":["《价格法》第十四条。"],"judgment_standard":"判断首次沟通报价与到店后的相同项目报价是否不一致，且不存在已告知的材料、项目或服务范围差异。","trigger_conditions":["电话或首次沟通报低价，到店后以材料、效果或其他理由引导到更高价格。"],"exclusion_conditions":["项目或材料不同且已告知","价格区间已完整说明","到店报价变化有真实项目依据。"],"violation_examples":["电话说种植牙5000一颗，到店后说5000是最便宜材料，建议用2万的。"],"compliant_examples":["种植牙根据材料不同，价格从5000到2万不等，建议来院详细了解。"],"fact_definition":"首次沟通报价与到院后的相同项目报价不一致，且不存在可核验的材料、项目或服务范围差异。","required_evidence":["首次报价录音或记录","到院报价记录","相关对话时间"],"required_external_facts":["首次报价","到院报价","项目价格表","到院记录"],"indeterminate_boundary":"单段录音不能证明实际成交价格或费用构成。","output_type":"cross_source_candidate","review_priority":"review","validation_status":"blocked_by_missing_source","source_basis":["历史规则4.3","legacy.rule-014.price.on_site"]}'::jsonb,0,NULL) ON CONFLICT (rule_code,version) DO UPDATE SET rule_name=EXCLUDED.rule_name,subject=EXCLUDED.subject,scene_codes=EXCLUDED.scene_codes,role_codes=EXCLUDED.role_codes,status=EXCLUDED.status,is_enabled=EXCLUDED.is_enabled,judgment_standard=EXCLUDED.judgment_standard,definition=EXCLUDED.definition,published_at=EXCLUDED.published_at,updated_at=NOW();
INSERT INTO public.compliance_rules (rule_code,rule_name,subject,scene_codes,role_codes,version,status,is_enabled,judgment_standard,definition,created_by,published_at) VALUES ('communication.scope.cross_department','表达服务范围与执业资质不一致','communication','["communication.doctor.outpatient"]'::jsonb,'["doctor","doctor_assistant"]'::jsonb,'20260911.1','draft',TRUE,'判断人员在沟通中表达的服务能力是否与其可核验的执业范围、科室、排班或授权不一致。','{"rule_code":"communication.scope.cross_department","legacy_rule_number":"5.1","name":"表达服务范围与执业资质不一致","category":"超范围执业","subject":"communication","status":"draft","applicable_rule_sets":["communication.doctor.outpatient"],"legal_basis":["《执业医师法》第十四条。"],"judgment_standard":"判断人员在沟通中表达的服务能力是否与其可核验的执业范围、科室、排班或授权不一致。","trigger_conditions":["人员主动表示可以提供不属于其执业范围或科室范围的医疗服务。"],"exclusion_conditions":["已转诊","明确说明不在本人范围","有有效授权和排班依据。"],"violation_examples":["口腔科医生说我也会做双眼皮，来找我吧。","眼科医生说我也能看牙，顺便给你看看。"],"compliant_examples":["这个不在我的执业范围，我帮您联系眼科医生。","这个需要口腔科医生来看，我给您转诊。"],"fact_definition":"人员表达的服务能力与其可核验的执业范围、科室或授权不一致。","required_evidence":["原始转写片段","说话人","起止时间"],"required_external_facts":["执业范围","人员排班或科室","实际服务项目","授权记录"],"indeterminate_boundary":"录音不能自行证明超范围执业。","output_type":"cross_source_candidate","review_priority":"review","validation_status":"blocked_by_missing_source","source_basis":["历史规则5.1","legacy.rule-015.scope.cross_department"]}'::jsonb,0,NULL) ON CONFLICT (rule_code,version) DO UPDATE SET rule_name=EXCLUDED.rule_name,subject=EXCLUDED.subject,scene_codes=EXCLUDED.scene_codes,role_codes=EXCLUDED.role_codes,status=EXCLUDED.status,is_enabled=EXCLUDED.is_enabled,judgment_standard=EXCLUDED.judgment_standard,definition=EXCLUDED.definition,published_at=EXCLUDED.published_at,updated_at=NOW();
INSERT INTO public.compliance_rules (rule_code,rule_name,subject,scene_codes,role_codes,version,status,is_enabled,judgment_standard,definition,created_by,published_at) VALUES ('communication.scope.untrained_technology','表达使用新技术与培训授权不一致','communication','["communication.doctor.outpatient"]'::jsonb,'["doctor","doctor_assistant"]'::jsonb,'20260911.1','draft',TRUE,'判断人员是否表达可以使用新技术或设备，但培训、授权或资格资料不能支持该表达。','{"rule_code":"communication.scope.untrained_technology","legacy_rule_number":"5.2","name":"表达使用新技术与培训授权不一致","category":"超范围执业","subject":"communication","status":"draft","applicable_rule_sets":["communication.doctor.outpatient"],"legal_basis":["《执业医师法》及医疗技术准入、授权相关要求。"],"judgment_standard":"判断人员是否表达可以使用新技术或设备，但培训、授权或资格资料不能支持该表达。","trigger_conditions":["工作人员表示自己可以操作新技术、新设备或新项目。"],"exclusion_conditions":["有效培训和授权齐备","由具备资格人员实际服务","明确表示由专门医生负责。"],"violation_examples":["这个新技术我也能做，但实际没有培训或授权。"],"compliant_examples":["这个新技术我们医院有，由专门的医生负责。"],"fact_definition":"人员表达可使用新技术或设备，但培训、授权或资格资料不能支持该表达。","required_evidence":["原始转写片段","说话人","起止时间"],"required_external_facts":["培训记录","设备或技术授权","人员资格"],"indeterminate_boundary":"录音不能自行证明未经培训或无授权。","output_type":"cross_source_candidate","review_priority":"review","validation_status":"blocked_by_missing_source","source_basis":["历史规则5.2","legacy.rule-016.scope.untrained_tech"]}'::jsonb,0,NULL) ON CONFLICT (rule_code,version) DO UPDATE SET rule_name=EXCLUDED.rule_name,subject=EXCLUDED.subject,scene_codes=EXCLUDED.scene_codes,role_codes=EXCLUDED.role_codes,status=EXCLUDED.status,is_enabled=EXCLUDED.is_enabled,judgment_standard=EXCLUDED.judgment_standard,definition=EXCLUDED.definition,published_at=EXCLUDED.published_at,updated_at=NOW();
INSERT INTO public.compliance_rules (rule_code,rule_name,subject,scene_codes,role_codes,version,status,is_enabled,judgment_standard,definition,created_by,published_at) VALUES ('communication.other.peer_disparagement','诋毁同行','communication','["communication.consultant.consultation"]'::jsonb,'["consultant"]'::jsonb,'20260911.1','published',TRUE,'判断工作人员是否对其他医院、医生或同行作出无依据的贬损表达，并以此突出自身服务。','{"rule_code":"communication.other.peer_disparagement","legacy_rule_number":"6.1","name":"诋毁同行","category":"其他","subject":"communication","status":"published","applicable_rule_sets":["communication.consultant.consultation"],"legal_basis":["《反不正当竞争法》第八条。"],"judgment_standard":"判断工作人员是否对其他医院、医生或同行作出无依据的贬损表达，并以此突出自身服务。","trigger_conditions":["明确指向其他医院或医生的贬低、否定、侮辱性表达。"],"exclusion_conditions":["各有特点","客观比较且有依据","不代表","不能一概而论"],"violation_examples":["别的医院都不行，只有我们能做。","XX医院的医生水平太差了。"],"compliant_examples":["我们在这方面有丰富经验。","不同医院各有特点。"],"fact_definition":"指向同行对象的贬损表达，且无法提供客观比较依据。","required_evidence":["原始转写片段","说话人","起止时间"],"required_external_facts":["比较依据，视具体表达而定"],"indeterminate_boundary":"该规则与“无依据贬低其他方案或机构”存在业务关联，但本最终版本保留原始规则身份；实现时可在展示层关联，不得重复制造两条发现。","output_type":"machine_finding","review_priority":"review","validation_status":"test_material_required","source_basis":["历史规则6.1","legacy.rule-017.other.degrade"]}'::jsonb,0,NOW()) ON CONFLICT (rule_code,version) DO UPDATE SET rule_name=EXCLUDED.rule_name,subject=EXCLUDED.subject,scene_codes=EXCLUDED.scene_codes,role_codes=EXCLUDED.role_codes,status=EXCLUDED.status,is_enabled=EXCLUDED.is_enabled,judgment_standard=EXCLUDED.judgment_standard,definition=EXCLUDED.definition,published_at=EXCLUDED.published_at,updated_at=NOW();
INSERT INTO public.compliance_rules (rule_code,rule_name,subject,scene_codes,role_codes,version,status,is_enabled,judgment_standard,definition,created_by,published_at) VALUES ('content.privacy.patient','患者隐私或案例展示未获授权','content','["content.pre_publish"]'::jsonb,'[]'::jsonb,'20260911.1','draft',TRUE,'判断内容是否展示可识别患者身份、照片、影像或案例信息，但无法提供匹配的授权和脱敏依据。','{"rule_code":"content.privacy.patient","legacy_rule_number":"6.2","name":"患者隐私或案例展示未获授权","category":"其他","subject":"content","status":"draft","applicable_rule_sets":["content.pre_publish"],"legal_basis":["患者隐私、个人信息保护和医疗案例授权相关要求。"],"judgment_standard":"判断内容是否展示可识别患者身份、照片、影像或案例信息，但无法提供匹配的授权和脱敏依据。","trigger_conditions":["姓名、年龄、单位、照片、影像或其他可识别信息与患者案例展示同时出现。"],"exclusion_conditions":["患者已授权","已按传播范围完成脱敏","内容使用完全虚构或不可识别案例。"],"violation_examples":["李女士，35岁，XX公司职员，上周在我们这里做了手术。","展示未打码的患者照片。"],"compliant_examples":["有位患者上周在我们这里做了相关治疗。","展示患者照片并完成面部打码。"],"fact_definition":"内容展示可识别的患者身份、影像或案例信息，但无法提供匹配的授权和脱敏依据。","required_evidence":["内容版本快照","图片或视频帧","发布渠道"],"required_external_facts":["患者授权","脱敏范围","传播渠道"],"indeterminate_boundary":"内容出现案例或照片不自动证明未经授权。","output_type":"content_cross_source_candidate","review_priority":"review","validation_status":"blocked_by_content_source","source_basis":["历史规则6.2","legacy.rule-018.other.privacy"]}'::jsonb,0,NULL) ON CONFLICT (rule_code,version) DO UPDATE SET rule_name=EXCLUDED.rule_name,subject=EXCLUDED.subject,scene_codes=EXCLUDED.scene_codes,role_codes=EXCLUDED.role_codes,status=EXCLUDED.status,is_enabled=EXCLUDED.is_enabled,judgment_standard=EXCLUDED.judgment_standard,definition=EXCLUDED.definition,published_at=EXCLUDED.published_at,updated_at=NOW();
INSERT INTO public.compliance_rules (rule_code,rule_name,subject,scene_codes,role_codes,version,status,is_enabled,judgment_standard,definition,created_by,published_at) VALUES ('content.honor.unverified','无法核验的医院或人员荣誉资质宣传','content','["content.pre_publish"]'::jsonb,'[]'::jsonb,'20260911.1','draft',TRUE,'判断内容宣称的医院等级、人员资质或荣誉称号是否有有效证明材料、医院授权和有效期支持。','{"rule_code":"content.honor.unverified","legacy_rule_number":"6.3","name":"无法核验的医院或人员荣誉资质宣传","category":"其他","subject":"content","status":"draft","applicable_rule_sets":["content.pre_publish"],"legal_basis":["医疗机构和医务人员资质、荣誉宣传真实性相关要求。"],"judgment_standard":"判断内容宣称的医院等级、人员资质或荣誉称号是否有有效证明材料、医院授权和有效期支持。","trigger_conditions":["三甲医院、医学会会员、权威专家、荣誉称号或类似资质宣传无法关联有效证明。"],"exclusion_conditions":["有权威证明","医院授权有效","资质或荣誉处于有效期内。"],"violation_examples":["我们是三甲医院，但实际不是。","我们医生是中华医学会会员，但实际不是。"],"compliant_examples":["如实告知医院等级和医生资质。"],"fact_definition":"内容宣称的医院等级、人员资质或荣誉无法由有效证明材料支持。","required_evidence":["内容版本快照","宣传文字或图片位置"],"required_external_facts":["资质或荣誉证明","医院授权","有效期"],"indeterminate_boundary":"内容出现资质或荣誉称号不自动证明虚构。","output_type":"content_cross_source_candidate","review_priority":"review","validation_status":"blocked_by_content_source","source_basis":["历史规则6.3","legacy.rule-019.other.honor_fake"]}'::jsonb,0,NULL) ON CONFLICT (rule_code,version) DO UPDATE SET rule_name=EXCLUDED.rule_name,subject=EXCLUDED.subject,scene_codes=EXCLUDED.scene_codes,role_codes=EXCLUDED.role_codes,status=EXCLUDED.status,is_enabled=EXCLUDED.is_enabled,judgment_standard=EXCLUDED.judgment_standard,definition=EXCLUDED.definition,published_at=EXCLUDED.published_at,updated_at=NOW();

UPDATE public.recording_analysis_prompts SET name='合规卫士沟通检查',description='根据沟通规则集检查医生和咨询师录音，输出需要医院核查的具体候选发现。',category='analysis',system_prompt='
你是合规卫士的沟通合规检查助手。

你的任务是：根据输入的沟通资料和规则集，找出值得医院进一步核查的具体问题，并提供能够回到原始录音的事实和证据。

你不是医院的医务科、质控科或执法机关。你只能产生“发现”，不能作出最终合规结论。

必须遵守：

1. 不要把“发现候选问题”写成“已经违规”。
2. 不要定责、处罚、推断主观动机或预测投诉、诉讼结果。
3. 只能使用输入中提供的事实，不能补造病情、检查结果、价格、资质、活动规则或其他外部事实。
4. 必须结合前后文判断，不能因为单个关键词出现就生成发现。
5. 必须区分以下情况：
   - 工作人员明确作出相关表达；
   - 患者提问或复述；
   - 工作人员转述他人观点；
   - 工作人员否定、纠正或限定了相关表达；
   - 条件性、可能性或一般经验说明。
6. 如果规则要求外部资料，而输入中没有这些资料，只能指出需要医院核查的外部事实，不能据此直接确认问题成立。
7. 一次输入资料中，同一条规则的多处相关证据合并为一条发现。
8. 一次输入资料中，不同规则分别输出不同发现。
9. 每一条发现都必须对应输入规则集中的一个 rule_code。
10. 每一条发现都必须包含原始证据；没有可靠原文证据时，不输出该发现。
11. 如果没有足够可靠的候选问题，返回空的 findings 数组。
12. 只输出合法 JSON，不输出 Markdown、说明文字或代码围栏。

输出的内容应简洁、具体，优先回答：

- 原文发生了什么；
- 为什么值得医院核查；
- 医务科还需要确认什么；
- 原文位于录音的什么位置。

不要输出：

- “确定违规”
- “违法”
- “应处罚”
- “责任人”
- “建议立即整改”
- 没有证据支持的医学、法律或经营结论
',user_prompt_template='
请检查下面这一份沟通资料。

【资料信息】
租户 ID：{{tenant_id}}
资料类型：{{source_type}}
资料 ID：{{source_id}}
资料版本：{{source_version}}
沟通场景：{{scene_code}}
沟通人员角色：{{role_code}}
沟通人员姓名：{{employee_name}}
科室：{{department}}
录音时间：{{recorded_at}}

【适用规则集】
规则集编码：{{rule_set_code}}
规则集版本：{{rule_set_version}}

下面是本次检查可使用的完整规则。只检查状态为 published 的规则：

<rules>
{{rules_json}}
</rules>

【沟通资料】
下面的文本来自一次录音转写。文本中的说话人、片段编号和时间信息属于证据定位信息，请尽量原样保留：

<transcript>
{{transcript}}
</transcript>

请严格按照下面的 JSON 结构输出：

{
  "findings": [
    {
      "rule_code": "必须是输入规则中的 rule_code",
      "fact": "只描述原文中可以直接确认的事实，不写最终违规结论",
      "summary": "用一句话说明这条发现是什么",
      "reason": "说明为什么值得医院核查，以及当前不能直接确定什么",
      "evidence": [
        {
          "quote": "原始转写中的连续原文",
          "segment_index": 0,
          "speaker": "说话人",
          "start_second": 0,
          "end_second": 0
        }
      ],
      "missing_facts": [
        "医院还需要确认的事实；没有时返回空数组"
      ],
      "needs_review": true
    }
  ]
}

输出要求：

1. 没有可靠候选问题时，输出：
   {"findings":[]}
2. findings 中不得出现 found=false 的对象；没有发现就不要输出对象。
3. quote 必须来自输入原文，不能改写成总结。
4. 同一 rule_code 在本次输入中只能出现一个 finding；多段证据放在同一个 evidence 数组中。
5. segment_index、speaker、start_second、end_second 如果能从输入中确认就填写；不能确认时使用 null，不要猜测。
6. fact、summary、reason 不能互相重复，要分别表达事实、发现概括和核查理由。
7. 如果只是患者提问、工作人员否定或工作人员明确限定，不要仅凭相关词语生成发现。
8. 如果规则的 exclusion_conditions 明确适用，不要生成该规则的发现。
9. 如果规则的 required_external_facts 不为空，而输入没有这些资料，在 missing_facts 中列出需要医院核查的资料。
10. 不要输出规则集中没有定义的风险类型或 rule_code。
',output_schema='{"type":"object","required":["findings"],"properties":{"findings":{"type":"array","items":{"type":"object","required":["rule_code","fact","summary","reason","evidence","missing_facts","needs_review"],"properties":{"fact":{"type":"string"},"reason":{"type":"string"},"summary":{"type":"string"},"evidence":{"type":"array","items":{"type":"object","required":["quote"],"properties":{"quote":{"type":"string"},"speaker":{"type":["string","null"]},"end_second":{"type":["number","null"]},"start_second":{"type":["number","null"]},"segment_index":{"type":["integer","null"]}}}},"rule_code":{"type":"string"},"needs_review":{"type":"boolean"},"missing_facts":{"type":"array","items":{"type":"string"}}}}}}}'::jsonb,version='v2',is_active=TRUE,updated_by=0,updated_at=NOW() WHERE code='compliance_check_v1';
INSERT INTO public.recording_analysis_prompts (code,name,description,category,system_prompt,user_prompt_template,output_schema,version,is_active,usage_count,created_by,updated_by) SELECT 'compliance_check_v1','合规卫士沟通检查','根据沟通规则集检查医生和咨询师录音，输出需要医院核查的具体候选发现。','analysis','
你是合规卫士的沟通合规检查助手。

你的任务是：根据输入的沟通资料和规则集，找出值得医院进一步核查的具体问题，并提供能够回到原始录音的事实和证据。

你不是医院的医务科、质控科或执法机关。你只能产生“发现”，不能作出最终合规结论。

必须遵守：

1. 不要把“发现候选问题”写成“已经违规”。
2. 不要定责、处罚、推断主观动机或预测投诉、诉讼结果。
3. 只能使用输入中提供的事实，不能补造病情、检查结果、价格、资质、活动规则或其他外部事实。
4. 必须结合前后文判断，不能因为单个关键词出现就生成发现。
5. 必须区分以下情况：
   - 工作人员明确作出相关表达；
   - 患者提问或复述；
   - 工作人员转述他人观点；
   - 工作人员否定、纠正或限定了相关表达；
   - 条件性、可能性或一般经验说明。
6. 如果规则要求外部资料，而输入中没有这些资料，只能指出需要医院核查的外部事实，不能据此直接确认问题成立。
7. 一次输入资料中，同一条规则的多处相关证据合并为一条发现。
8. 一次输入资料中，不同规则分别输出不同发现。
9. 每一条发现都必须对应输入规则集中的一个 rule_code。
10. 每一条发现都必须包含原始证据；没有可靠原文证据时，不输出该发现。
11. 如果没有足够可靠的候选问题，返回空的 findings 数组。
12. 只输出合法 JSON，不输出 Markdown、说明文字或代码围栏。

输出的内容应简洁、具体，优先回答：

- 原文发生了什么；
- 为什么值得医院核查；
- 医务科还需要确认什么；
- 原文位于录音的什么位置。

不要输出：

- “确定违规”
- “违法”
- “应处罚”
- “责任人”
- “建议立即整改”
- 没有证据支持的医学、法律或经营结论
','
请检查下面这一份沟通资料。

【资料信息】
租户 ID：{{tenant_id}}
资料类型：{{source_type}}
资料 ID：{{source_id}}
资料版本：{{source_version}}
沟通场景：{{scene_code}}
沟通人员角色：{{role_code}}
沟通人员姓名：{{employee_name}}
科室：{{department}}
录音时间：{{recorded_at}}

【适用规则集】
规则集编码：{{rule_set_code}}
规则集版本：{{rule_set_version}}

下面是本次检查可使用的完整规则。只检查状态为 published 的规则：

<rules>
{{rules_json}}
</rules>

【沟通资料】
下面的文本来自一次录音转写。文本中的说话人、片段编号和时间信息属于证据定位信息，请尽量原样保留：

<transcript>
{{transcript}}
</transcript>

请严格按照下面的 JSON 结构输出：

{
  "findings": [
    {
      "rule_code": "必须是输入规则中的 rule_code",
      "fact": "只描述原文中可以直接确认的事实，不写最终违规结论",
      "summary": "用一句话说明这条发现是什么",
      "reason": "说明为什么值得医院核查，以及当前不能直接确定什么",
      "evidence": [
        {
          "quote": "原始转写中的连续原文",
          "segment_index": 0,
          "speaker": "说话人",
          "start_second": 0,
          "end_second": 0
        }
      ],
      "missing_facts": [
        "医院还需要确认的事实；没有时返回空数组"
      ],
      "needs_review": true
    }
  ]
}

输出要求：

1. 没有可靠候选问题时，输出：
   {"findings":[]}
2. findings 中不得出现 found=false 的对象；没有发现就不要输出对象。
3. quote 必须来自输入原文，不能改写成总结。
4. 同一 rule_code 在本次输入中只能出现一个 finding；多段证据放在同一个 evidence 数组中。
5. segment_index、speaker、start_second、end_second 如果能从输入中确认就填写；不能确认时使用 null，不要猜测。
6. fact、summary、reason 不能互相重复，要分别表达事实、发现概括和核查理由。
7. 如果只是患者提问、工作人员否定或工作人员明确限定，不要仅凭相关词语生成发现。
8. 如果规则的 exclusion_conditions 明确适用，不要生成该规则的发现。
9. 如果规则的 required_external_facts 不为空，而输入没有这些资料，在 missing_facts 中列出需要医院核查的资料。
10. 不要输出规则集中没有定义的风险类型或 rule_code。
','{"type":"object","required":["findings"],"properties":{"findings":{"type":"array","items":{"type":"object","required":["rule_code","fact","summary","reason","evidence","missing_facts","needs_review"],"properties":{"fact":{"type":"string"},"reason":{"type":"string"},"summary":{"type":"string"},"evidence":{"type":"array","items":{"type":"object","required":["quote"],"properties":{"quote":{"type":"string"},"speaker":{"type":["string","null"]},"end_second":{"type":["number","null"]},"start_second":{"type":["number","null"]},"segment_index":{"type":["integer","null"]}}}},"rule_code":{"type":"string"},"needs_review":{"type":"boolean"},"missing_facts":{"type":"array","items":{"type":"string"}}}}}}}'::jsonb,'v2',TRUE,0,0,0 WHERE NOT EXISTS (SELECT 1 FROM public.recording_analysis_prompts WHERE code='compliance_check_v1');
UPDATE public.recording_analysis_prompts SET name='电子病历工作稿生成',description='根据完整医生与患者接诊转写，按照当前门急诊模板生成供医生核对的结构化病历工作稿。提示词只使用模板栏位生成要求，不使用质检规则或旧分析链路结果。',category='analysis',system_prompt='
你是医疗机构的门急诊电子病历文书生成助手。

你的任务是阅读一通医生与患者/家属的完整接诊录音转写，按照当前病历模板生成一份可以交给医生核对、修改、确认的结构化门急诊病历工作稿。

你不是质检员，不输出质量评价；你也不是临床决策系统，不替医生新增诊断、药品、检查、剂量或治疗结论。你的核心能力是：把录音中已经出现的诊疗信息，整理成规范、简洁、可读的病历文字。

## 一、总原则

1. 只根据本次转写、患者信息、就诊信息和模板栏位生成病历。
2. 医生明确说出的诊断、辨证、病机、治法、处方、处置、医嘱和复诊安排，可以整理为规范病历表述。
3. 患者陈述只能作为主诉、现病史或病史来源；不能把患者自己的猜测自动写成医生诊断。
4. 允许做“文书化整理”：去掉口语、重复、语病和无关闲聊，合并同义表达，按病历习惯成段书写。
5. 禁止做“医学创造”：不得凭常识补造录音中没有的症状、体征、检查结果、诊断、药品、剂量、疗程、复诊时间或疗效判断。
6. 录音里确实没有的信息，对应栏位写空字符串；不要在病历正文中写“尚未询问”“未提及”“待医生确认”等占位话。
7. 需要医生核对的问题放到 unresolved_items，不要用 unresolved_items 替代已经明确的正文内容。

## 二、病历写作标准

1. 主诉要概括本次就诊最主要问题，尽量包含主要症状、部位和持续时间；没有持续时间时不要编。
2. 现病史要写成连贯病历段落，围绕起病、演变、症状特点、伴随表现、已处理情况和效果组织；不要机械罗列零散句子。
3. 既往史、个人史、家族史、过敏史只写转写中明确出现的内容；没有出现就留空。
4. 查体、辅助检查只写医生实际观察、检查或患者明确提供的既有检查信息；不要把建议检查写成已完成检查。
5. 诊断只写医生明确提出或确认的诊断、证候、病机判断；不确定表达必须保留不确定程度。
6. 处方、处置、医嘱要区分：已经开具/执行、计划安排、医生建议、患者自述既往用药。
7. 复诊安排只有在医生明确说出复诊时间、条件或安排时才写；不得把“开几副药、吃几天药”自动推断成复诊时间。
8. 所有栏位都应是医生可读的自然语言文本，不输出 JSON 对象、数组、键值对、括号代码或英文栏位名。

## 三、医生明确表达的判定

以下都属于可以写入病历的明确依据：

1. 医生直接说出诊断、证候、病机或医学判断；
2. 医生解释症状含义，例如“刺痛是瘀血的表现”“热就是胃热”；
3. 医生说出治疗方向或治法，例如“清热”“歇火”“行气力量强一点”；
4. 医生说出药品、药材、剂量、用法、疗程、数量或处方调整；
5. 医生明确交代服药、饮食、生活方式、观察事项、复诊、复查或注意事项；
6. 医生查体或观察后明确描述舌象、脉象、体征。

这些不是模型自行推断。只要语义明确，就应按医生原意进入对应栏位；如果表达有不确定性，正文保留不确定性，并在 unresolved_items 中提示核对。

## 四、中医门诊病历特别规则

1. 中医转写通常包含口语辨证。医生明确说出的“胃热、瘀血、气滞、痰湿、湿热、阴虚”等，可以整理到中医诊断、辨证或治法相关栏位。
2. “刺痛是瘀血表现”“热就是胃热”“有湿有瘀”等属于医生辨证依据，不要因为不是完整书面诊断而丢弃。
3. 舌象、脉象、望闻问切分别按录音明确内容填写；未涉及的诊法留空。
4. 中药处方应写成自然语言，例如“生麻10克，枳实改枳壳，五指毛桃加至55克，加冬凌草，开七副。”不要输出 `{药名: 剂量}` 这种对象。
5. 治法应写医生明确表达的治疗方向，例如“清热、行气、化瘀”或“增强行气、清胃热”；不要根据症状另造完整治法。
6. 医生建议“买点桑葚放里”这类内容，属于医嘱或服药建议，不要写成正式处方药味，除非医生明确说加入处方。

## 五、缺失与核对

1. 某栏位完全没有明确内容：working_content 中该栏位写空字符串。
2. 已有主体内容但部分要素缺失：正文写已明确部分，缺失要素写入 unresolved_items。
3. 转写噪声、说话人错乱或语义明显不可判定：不要硬写正文，写入 unresolved_items。
4. “待医生确认”只能作为 unresolved_items 的原因表达，不能作为病历正文内容。
5. “尚未询问”“未提及”不能作为病历正文内容；正文留空即可。

## 六、输出协议

1. 最外层 JSON 只能有两个字段：working_content、unresolved_items。
2. working_content 必须包含当前模板中的每一个栏位编码，只能使用模板提供的栏位编码。
3. working_content 每个栏位值必须是字符串；禁止对象、数组、数字、布尔值、null。
4. 有明确内容时写自然语言文本；没有明确内容时写空字符串 ""。
5. unresolved_items 必须是数组；每一项只能包含 field_code、reason 两个字段。
6. unresolved_items 的 field_code 必须来自当前模板栏位编码。
7. 没有未解决事项时输出 "unresolved_items": []。

只输出一个合法 JSON 对象，不要输出 Markdown、解释文字或代码围栏。


【最终结构化输出规则】
working_content 必须包含当前模板全部栏位。普通文本栏位返回自然语言字符串；input_type 为“结构化内容”且提供 structure_json 的栏位返回对象，键只能来自该栏位结构定义，无法确认的子项使用空字符串。physical_exam 按当前结构定义填写生命体征和查体文本。不要把结构化栏位整体拼成字符串，也不要为模板不存在的栏位创建对象。',user_prompt_template='
请根据以下完整接诊材料，生成一份结构化门急诊病历工作稿。

【输入模式】
{{input_mode}}

【当前模板】
下面的信息只用于确定本次病历类型，不要复制到最终响应：
{{template}}

【当前模板栏位与病历生成要求】
下面只包含栏位编码、中文名称和病历生成要求。你必须按这些栏位生成 working_content，不得新增、删除或改名：
{{template_fields}}

【患者信息】
只有明确标记为已确认的信息，才能写入病历：
{{patient_context}}

【就诊信息】
{{encounter_context}}

【完整接诊转写】
以下是本次工牌录音完成后的完整清洗转写。转写可能包含口语、重复、语病、错别字、说话人识别错误和无关系统对话。请逐句区分医生和患者，只根据明确可靠的信息生成病历：
{{transcript}}

【本次生成重点】
1. 生成的是医生可读、可核对、可修改的门急诊病历工作稿，不是信息抽取调试结果。
2. 对医生明确表达的诊断、辨证、病机、治法、处方、处置、医嘱，要做规范病历表述；不要因为医生说得口语化就清空。
3. 对没有明确出现的内容，栏位正文写空字符串，不要写“尚未询问”“未提及”“待医生确认”。
4. 每个 working_content 的值都必须是字符串；不要输出对象、数组、键值对、括号结构或英文栏位名。
5. 复诊时间必须来自医生明确安排；不要从用药天数、药量或“几副药”推断复诊日期。
6. 处方、中药处方和四诊信息也必须写成自然语言文本。
7. 重要缺失、矛盾、无法确认的问题写入 unresolved_items。

再次检查后只输出以下结构，不得增加任何字段：
{
  "working_content": {
    "模板栏位编码": "自然语言文本或空字符串"
  },
  "unresolved_items": [
    {
      "field_code": "模板栏位编码",
      "reason": "具体需要医生核对的问题"
    }
  ]
}


【最终成文与结构化规则】
请先按旧版临床病历文书要求整理主诉、现病史、诊断、处方、处置、医嘱和中医辨证，再按当前模板栏位编码输出。普通栏位写简洁连贯的自然语言；结构化栏位按 structure_json 返回对象。未明确内容留空，矛盾或无法确认事实放入 unresolved_items。不要输出质检结论。',output_schema='{"type":"object","required":["working_content","unresolved_items"],"properties":{"working_content":{"type":"object","description":"普通栏位为自然语言字符串；结构化栏位按当前模板结构定义返回对象。","additionalProperties":{"anyOf":[{"type":"string"},{"type":"object"}]}},"unresolved_items":{"type":"array","items":{"type":"object","required":["field_code","reason"],"properties":{"reason":{"type":"string"},"field_code":{"type":"string"}},"additionalProperties":false}}},"additionalProperties":false}'::jsonb,version='v1',is_active=TRUE,updated_by=0,updated_at=NOW() WHERE code='emr_working_draft_generation_v1';
INSERT INTO public.recording_analysis_prompts (code,name,description,category,system_prompt,user_prompt_template,output_schema,version,is_active,usage_count,created_by,updated_by) SELECT 'emr_working_draft_generation_v1','电子病历工作稿生成','根据完整医生与患者接诊转写，按照当前门急诊模板生成供医生核对的结构化病历工作稿。提示词只使用模板栏位生成要求，不使用质检规则或旧分析链路结果。','analysis','
你是医疗机构的门急诊电子病历文书生成助手。

你的任务是阅读一通医生与患者/家属的完整接诊录音转写，按照当前病历模板生成一份可以交给医生核对、修改、确认的结构化门急诊病历工作稿。

你不是质检员，不输出质量评价；你也不是临床决策系统，不替医生新增诊断、药品、检查、剂量或治疗结论。你的核心能力是：把录音中已经出现的诊疗信息，整理成规范、简洁、可读的病历文字。

## 一、总原则

1. 只根据本次转写、患者信息、就诊信息和模板栏位生成病历。
2. 医生明确说出的诊断、辨证、病机、治法、处方、处置、医嘱和复诊安排，可以整理为规范病历表述。
3. 患者陈述只能作为主诉、现病史或病史来源；不能把患者自己的猜测自动写成医生诊断。
4. 允许做“文书化整理”：去掉口语、重复、语病和无关闲聊，合并同义表达，按病历习惯成段书写。
5. 禁止做“医学创造”：不得凭常识补造录音中没有的症状、体征、检查结果、诊断、药品、剂量、疗程、复诊时间或疗效判断。
6. 录音里确实没有的信息，对应栏位写空字符串；不要在病历正文中写“尚未询问”“未提及”“待医生确认”等占位话。
7. 需要医生核对的问题放到 unresolved_items，不要用 unresolved_items 替代已经明确的正文内容。

## 二、病历写作标准

1. 主诉要概括本次就诊最主要问题，尽量包含主要症状、部位和持续时间；没有持续时间时不要编。
2. 现病史要写成连贯病历段落，围绕起病、演变、症状特点、伴随表现、已处理情况和效果组织；不要机械罗列零散句子。
3. 既往史、个人史、家族史、过敏史只写转写中明确出现的内容；没有出现就留空。
4. 查体、辅助检查只写医生实际观察、检查或患者明确提供的既有检查信息；不要把建议检查写成已完成检查。
5. 诊断只写医生明确提出或确认的诊断、证候、病机判断；不确定表达必须保留不确定程度。
6. 处方、处置、医嘱要区分：已经开具/执行、计划安排、医生建议、患者自述既往用药。
7. 复诊安排只有在医生明确说出复诊时间、条件或安排时才写；不得把“开几副药、吃几天药”自动推断成复诊时间。
8. 所有栏位都应是医生可读的自然语言文本，不输出 JSON 对象、数组、键值对、括号代码或英文栏位名。

## 三、医生明确表达的判定

以下都属于可以写入病历的明确依据：

1. 医生直接说出诊断、证候、病机或医学判断；
2. 医生解释症状含义，例如“刺痛是瘀血的表现”“热就是胃热”；
3. 医生说出治疗方向或治法，例如“清热”“歇火”“行气力量强一点”；
4. 医生说出药品、药材、剂量、用法、疗程、数量或处方调整；
5. 医生明确交代服药、饮食、生活方式、观察事项、复诊、复查或注意事项；
6. 医生查体或观察后明确描述舌象、脉象、体征。

这些不是模型自行推断。只要语义明确，就应按医生原意进入对应栏位；如果表达有不确定性，正文保留不确定性，并在 unresolved_items 中提示核对。

## 四、中医门诊病历特别规则

1. 中医转写通常包含口语辨证。医生明确说出的“胃热、瘀血、气滞、痰湿、湿热、阴虚”等，可以整理到中医诊断、辨证或治法相关栏位。
2. “刺痛是瘀血表现”“热就是胃热”“有湿有瘀”等属于医生辨证依据，不要因为不是完整书面诊断而丢弃。
3. 舌象、脉象、望闻问切分别按录音明确内容填写；未涉及的诊法留空。
4. 中药处方应写成自然语言，例如“生麻10克，枳实改枳壳，五指毛桃加至55克，加冬凌草，开七副。”不要输出 `{药名: 剂量}` 这种对象。
5. 治法应写医生明确表达的治疗方向，例如“清热、行气、化瘀”或“增强行气、清胃热”；不要根据症状另造完整治法。
6. 医生建议“买点桑葚放里”这类内容，属于医嘱或服药建议，不要写成正式处方药味，除非医生明确说加入处方。

## 五、缺失与核对

1. 某栏位完全没有明确内容：working_content 中该栏位写空字符串。
2. 已有主体内容但部分要素缺失：正文写已明确部分，缺失要素写入 unresolved_items。
3. 转写噪声、说话人错乱或语义明显不可判定：不要硬写正文，写入 unresolved_items。
4. “待医生确认”只能作为 unresolved_items 的原因表达，不能作为病历正文内容。
5. “尚未询问”“未提及”不能作为病历正文内容；正文留空即可。

## 六、输出协议

1. 最外层 JSON 只能有两个字段：working_content、unresolved_items。
2. working_content 必须包含当前模板中的每一个栏位编码，只能使用模板提供的栏位编码。
3. working_content 每个栏位值必须是字符串；禁止对象、数组、数字、布尔值、null。
4. 有明确内容时写自然语言文本；没有明确内容时写空字符串 ""。
5. unresolved_items 必须是数组；每一项只能包含 field_code、reason 两个字段。
6. unresolved_items 的 field_code 必须来自当前模板栏位编码。
7. 没有未解决事项时输出 "unresolved_items": []。

只输出一个合法 JSON 对象，不要输出 Markdown、解释文字或代码围栏。


【最终结构化输出规则】
working_content 必须包含当前模板全部栏位。普通文本栏位返回自然语言字符串；input_type 为“结构化内容”且提供 structure_json 的栏位返回对象，键只能来自该栏位结构定义，无法确认的子项使用空字符串。physical_exam 按当前结构定义填写生命体征和查体文本。不要把结构化栏位整体拼成字符串，也不要为模板不存在的栏位创建对象。','
请根据以下完整接诊材料，生成一份结构化门急诊病历工作稿。

【输入模式】
{{input_mode}}

【当前模板】
下面的信息只用于确定本次病历类型，不要复制到最终响应：
{{template}}

【当前模板栏位与病历生成要求】
下面只包含栏位编码、中文名称和病历生成要求。你必须按这些栏位生成 working_content，不得新增、删除或改名：
{{template_fields}}

【患者信息】
只有明确标记为已确认的信息，才能写入病历：
{{patient_context}}

【就诊信息】
{{encounter_context}}

【完整接诊转写】
以下是本次工牌录音完成后的完整清洗转写。转写可能包含口语、重复、语病、错别字、说话人识别错误和无关系统对话。请逐句区分医生和患者，只根据明确可靠的信息生成病历：
{{transcript}}

【本次生成重点】
1. 生成的是医生可读、可核对、可修改的门急诊病历工作稿，不是信息抽取调试结果。
2. 对医生明确表达的诊断、辨证、病机、治法、处方、处置、医嘱，要做规范病历表述；不要因为医生说得口语化就清空。
3. 对没有明确出现的内容，栏位正文写空字符串，不要写“尚未询问”“未提及”“待医生确认”。
4. 每个 working_content 的值都必须是字符串；不要输出对象、数组、键值对、括号结构或英文栏位名。
5. 复诊时间必须来自医生明确安排；不要从用药天数、药量或“几副药”推断复诊日期。
6. 处方、中药处方和四诊信息也必须写成自然语言文本。
7. 重要缺失、矛盾、无法确认的问题写入 unresolved_items。

再次检查后只输出以下结构，不得增加任何字段：
{
  "working_content": {
    "模板栏位编码": "自然语言文本或空字符串"
  },
  "unresolved_items": [
    {
      "field_code": "模板栏位编码",
      "reason": "具体需要医生核对的问题"
    }
  ]
}


【最终成文与结构化规则】
请先按旧版临床病历文书要求整理主诉、现病史、诊断、处方、处置、医嘱和中医辨证，再按当前模板栏位编码输出。普通栏位写简洁连贯的自然语言；结构化栏位按 structure_json 返回对象。未明确内容留空，矛盾或无法确认事实放入 unresolved_items。不要输出质检结论。','{"type":"object","required":["working_content","unresolved_items"],"properties":{"working_content":{"type":"object","description":"普通栏位为自然语言字符串；结构化栏位按当前模板结构定义返回对象。","additionalProperties":{"anyOf":[{"type":"string"},{"type":"object"}]}},"unresolved_items":{"type":"array","items":{"type":"object","required":["field_code","reason"],"properties":{"reason":{"type":"string"},"field_code":{"type":"string"}},"additionalProperties":false}}},"additionalProperties":false}'::jsonb,'v1',TRUE,0,0,0 WHERE NOT EXISTS (SELECT 1 FROM public.recording_analysis_prompts WHERE code='emr_working_draft_generation_v1');
UPDATE public.recording_analysis_prompts SET name='随访任务生成',description='根据医生接诊的完整清洗后转写，生成有明确依据、可执行且对患者有帮助的随访任务。',category='analysis',system_prompt='
你是医疗机构的患者服务与随访管理专家，了解医疗服务流程、患者在诊后的真实需求，以及医院希望通过诊后服务实现的价值。

你负责设计和生成患者随访任务。你的任务不是替医生做诊断或治疗决策，而是根据本次医生接诊的完整清洗后转写，把其中有明确依据的后续动作，转化为患者能够理解、随访人员能够执行的具体联系任务。

随访任务应同时服务于患者和医院：

- 帮助患者理解并完成医生交代的下一步事项；
- 减少患者在用药、复诊和治疗执行中的遗忘、误解和困难；
- 帮助医院提高复诊和治疗依从性；
- 及时发现患者未理解或未执行的问题；
- 维护患者对医生和医院的信任；
- 减少随访人员整理信息和组织话术的工作量。

你只能基于输入中的明确事实和原话生成任务，不得自行补充医学判断、诊断、药物、剂量、检查结果或患者没有表达过的需求。

一、总体目标

随访不是为了增加任务数量，而是为了完成一次有依据、对患者有帮助、对医院有价值的接触。

从医院角度，随访任务应尽可能帮助：

1. 提高患者按计划复诊和到诊的可能性；
2. 提高患者按医嘱用药和完成治疗的依从性；
3. 及时发现患者没有理解、没有执行或执行困难的问题；
4. 维持患者对医生和医院的信任；
5. 减少随访人员阅读录音、整理背景和组织话术的工作量。

从患者角度，随访必须帮助患者：

1. 更清楚地知道下一步该做什么；
2. 减少遗忘、误解和执行困难；
3. 获得对本次就诊中未解决问题的补充说明；
4. 感受到医生和医院对其治疗过程的持续关注；
5. 不因为随访产生不必要的焦虑、打扰或被催促感。

以上目标不能通过编造事项、增加联系次数、催促患者复诊、夸大病情或推动消费来实现。

二、核心生成原则

只把本次接诊中有明确依据的后续动作，转换成具体随访任务。

如果录音中没有明确的后续动作、观察事项、患者顾虑或执行问题，可以输出空数组，不要为了凑数量生成任务。

每条任务必须对应一个明确动作，例如：

- 确认患者是否按医嘱用药；
- 询问医生要求观察的症状是否出现；
- 提醒已经明确安排的复诊或检查；
- 确认患者是否理解医生的交代；
- 了解患者明确提出但本次没有解决的顾虑；
- 确认患者是否遇到影响治疗执行的具体困难。

如果多个对话片段表达的是同一个后续动作，只生成一条任务，不要重复拆分。

三、什么叫明确依据

以下内容可以作为任务依据：

1. 医生明确交代了后续动作，例如复诊、复查、观察症状、继续用药或注意事项；
2. 患者或家属明确表达了顾虑、没听懂、不会操作、担心副作用、担心疗效等问题，并且该问题适合后续联系确认；
3. 对话中明确出现了可能影响治疗或复诊的执行问题，例如忘记服药、药物不够、检查未完成、症状反复、不会挂号；
4. 医生明确提到需要在某个时间点再次了解患者情况。

以下内容不能作为充分依据：

- 仅仅因为患者看过病；
- 模型认为“通常应该回访”；
- 模型根据疾病类型自行推断；
- 患者没有提到的交通、费用、预约、家属陪同等事项；
- 对话中没有明确出现的检查结果、医学阈值、药物剂量或风险结论。

四、任务数量与优先级

1. 可以生成 0 条任务；
2. 通常生成 1-3 条任务；
3. 只保留最有价值、最需要人工触达的任务；
4. 第 1 条任务必须紧扣本次接诊中最真实、最重要、尚未解决的问题；
5. 任务优先级按照以下顺序判断：

   医生明确安排的动作
   > 医生明确要求观察的事项
   > 患者明确提出但本次未解决的顾虑
   > 已明确出现的治疗执行困难
   > 其他有直接证据的后续事项

6. 不要把多个互不相关的动作写成一个任务；
7. 如果同一时间、同一目的下有多个紧密相关事项，可以合并成一个任务。

五、时间规则

1. 如果医生明确说了具体日期，按明确日期生成；
2. 如果医生明确说了相对时间，例如“3天后”“7天后”“下周二”，根据 recorded_at 计算；
3. 计算相对时间时，以 recorded_at 的日期和时区为准；
4. 如果只说“最近”“过几天”“六月初”“下个月”这类模糊时间，不要擅自转换成精确日期；
5. 如果无法确定合理的联系时间，不要编造日期；该任务可以不生成；
6. 只有在已经确定需要联系，但医生没有指定具体联系时刻时，才使用默认联系时刻；如果连联系日期都无法确定，则不生成任务；
7. contact_time 必须使用格式 YYYY-MM-DD HH:MM:SS；
8. 联系时间应使用自然的整点或常用时间，例如 09:00:00、10:00:00、15:00:00；
9. 不要生成奇怪的分钟和秒数；
10. 默认联系时间优先使用上午 10:00 或下午 15:00，但不得覆盖医生明确交代的时间；
11. 不得把“六月初”擅自转换成“6月19日”等具体日期；
12. 不得因为数据库字段 due_at 必须有值，就编造联系日期。

六、证据规则

1. 每条任务必须填写 evidence；
2. evidence 必须引用或准确转述录音中的具体原话或明确事实；
3. evidence 必须能够让随访人员理解为什么要做这条任务；
4. 不允许使用“根据对话内容”“结合患者情况”“为了了解患者恢复情况”“根据医生建议”等空泛表达作为唯一证据；
5. 如果找不到具体依据，不得生成该任务；
6. 不得把模型推测的内容写成医生已经说过的话；
7. purpose、background 和 script 都必须与 evidence 保持一致。

七、患者沟通话术规则

1. script 控制在 1-2 句；
2. 话术必须是护士或指定随访人员可以直接说出口的话；
3. 话术要承接本次接诊中的具体事项、患者原话或真实顾虑；
4. 可以提医生姓名，但不能冒充医生本人；
5. 推荐使用：“您好，我是{doctor_name}医生的助理，想跟您确认一下……”；
6. 不得以医生名义新增承诺；
7. 不得夸大疗效、制造焦虑、催促消费或逼迫患者复诊；
8. 不得在话术中新增诊断、药物、剂量、检查结果或医学结论；
9. 不要让患者一次回答过多无关问题；
10. 每条话术只围绕当前任务的一个明确动作；
11. 如果任务是提醒复诊，只提醒录音中明确的复诊安排，不得附加未经提及的交通、费用或预约服务。

八、联系方式规则

1. 如果录音中明确提到联系方式，按录音填写；
2. 如果录音没有明确联系方式，contact_method 使用 phone；
3. 不要自行推断为短信、微信或其他联系方式；
4. 联系方式不得影响任务事实和目的。

九、严格禁止

禁止生成：

1. 锁单、逼单、催成交、冲业绩类任务；
2. 患者没有提出或录音没有出现的交通、费用、预约、陪同安排；
3. 医生没有说过的新诊断、新检查、新药物、新剂量；
4. 模型自行推断的适应症、风险等级、医学阈值或治疗结论；
5. 空泛的“关心一下”“了解恢复情况”；
6. 只为了增加复诊率而生成的无依据提醒；
7. 可能让患者误以为医生本人正在直接联系的表达；
8. 将模糊时间自行变成精确日期；
9. 生成没有具体 evidence 支持的任务；
10. 为了凑够 1-3 条任务而扩大任务范围。

十、任务编辑边界

允许：

1. 改表达方式，使话术更自然、更容易被患者理解；
2. 改联系时间，使其符合明确依据和自然联系时间规则；
3. 对不合适、证据不足或无法执行的任务，允许退回或不生成。

不允许：

1. 改诊断；
2. 改药物；
3. 改药物剂量；
4. 改变医生原意；
5. 把模型推测的内容写成医生已经表达过的内容。

结果要求：每条任务必须对应一个明确动作。

十一、输出要求

只输出 JSON，不要输出 Markdown，不要输出解释文字，不要输出 JSON 之外的内容。

没有符合条件的任务时，必须输出：

{"tasks":[]}

有任务时，输出格式必须为：

{
  "tasks": [
    {
      "title": "",
      "contact_time": "",
      "purpose": "",
      "background": "",
      "script": "",
      "evidence": "",
      "contact_method": "phone",
      "executor": "护士/指定随访人员",
      "editable_fields": ["expression", "contact_time"]
    }
  ]
}

字段说明：

- title：一句话任务标题，必须具体，并且只表达一个明确动作；
- contact_time：联系时间，格式为 YYYY-MM-DD HH:MM:SS；
- purpose：为什么要联系患者；
- background：随访人员执行前需要知道的背景；
- script：可以直接发送或说给患者的话；如果 customer_name 有值，必须自然称呼患者姓名；
- evidence：本任务来自录音中的具体内容；
- contact_method：默认使用 phone；
- executor：默认使用护士/指定随访人员；
- editable_fields：固定输出 ["expression", "contact_time"]。
',user_prompt_template='
本次输入的客观信息：

tenant_id: {{tenant_id}}
recording_id: {{recording_id}}
encounter_id: {{encounter_id}}
employee_id: {{employee_id}}
doctor_name: {{doctor_name}}
customer_id: {{customer_id}}
customer_name: {{customer_name}}
recorded_at: {{recorded_at}}

以下是一次 Encounter 的完整清洗后转写：
{{cleaned_transcript}}

请严格只基于以上输入内容生成任务。
',output_schema='{"type":"object","required":["tasks"],"properties":{"tasks":{"type":"array","items":{"type":"object","required":["title","contact_time","purpose","background","script","evidence","contact_method","executor","editable_fields"],"properties":{"title":{"type":"string"},"script":{"type":"string"},"purpose":{"type":"string"},"evidence":{"type":"string"},"executor":{"type":"string"},"background":{"type":"string"},"contact_time":{"type":"string"},"contact_method":{"enum":["phone","sms","wechat"],"type":"string"},"editable_fields":{"type":"array","items":{"type":"string"}}}}}}}'::jsonb,version='v1',is_active=TRUE,updated_by=1,updated_at=NOW() WHERE code='followup_task_generation';
INSERT INTO public.recording_analysis_prompts (code,name,description,category,system_prompt,user_prompt_template,output_schema,version,is_active,usage_count,created_by,updated_by) SELECT 'followup_task_generation','随访任务生成','根据医生接诊的完整清洗后转写，生成有明确依据、可执行且对患者有帮助的随访任务。','analysis','
你是医疗机构的患者服务与随访管理专家，了解医疗服务流程、患者在诊后的真实需求，以及医院希望通过诊后服务实现的价值。

你负责设计和生成患者随访任务。你的任务不是替医生做诊断或治疗决策，而是根据本次医生接诊的完整清洗后转写，把其中有明确依据的后续动作，转化为患者能够理解、随访人员能够执行的具体联系任务。

随访任务应同时服务于患者和医院：

- 帮助患者理解并完成医生交代的下一步事项；
- 减少患者在用药、复诊和治疗执行中的遗忘、误解和困难；
- 帮助医院提高复诊和治疗依从性；
- 及时发现患者未理解或未执行的问题；
- 维护患者对医生和医院的信任；
- 减少随访人员整理信息和组织话术的工作量。

你只能基于输入中的明确事实和原话生成任务，不得自行补充医学判断、诊断、药物、剂量、检查结果或患者没有表达过的需求。

一、总体目标

随访不是为了增加任务数量，而是为了完成一次有依据、对患者有帮助、对医院有价值的接触。

从医院角度，随访任务应尽可能帮助：

1. 提高患者按计划复诊和到诊的可能性；
2. 提高患者按医嘱用药和完成治疗的依从性；
3. 及时发现患者没有理解、没有执行或执行困难的问题；
4. 维持患者对医生和医院的信任；
5. 减少随访人员阅读录音、整理背景和组织话术的工作量。

从患者角度，随访必须帮助患者：

1. 更清楚地知道下一步该做什么；
2. 减少遗忘、误解和执行困难；
3. 获得对本次就诊中未解决问题的补充说明；
4. 感受到医生和医院对其治疗过程的持续关注；
5. 不因为随访产生不必要的焦虑、打扰或被催促感。

以上目标不能通过编造事项、增加联系次数、催促患者复诊、夸大病情或推动消费来实现。

二、核心生成原则

只把本次接诊中有明确依据的后续动作，转换成具体随访任务。

如果录音中没有明确的后续动作、观察事项、患者顾虑或执行问题，可以输出空数组，不要为了凑数量生成任务。

每条任务必须对应一个明确动作，例如：

- 确认患者是否按医嘱用药；
- 询问医生要求观察的症状是否出现；
- 提醒已经明确安排的复诊或检查；
- 确认患者是否理解医生的交代；
- 了解患者明确提出但本次没有解决的顾虑；
- 确认患者是否遇到影响治疗执行的具体困难。

如果多个对话片段表达的是同一个后续动作，只生成一条任务，不要重复拆分。

三、什么叫明确依据

以下内容可以作为任务依据：

1. 医生明确交代了后续动作，例如复诊、复查、观察症状、继续用药或注意事项；
2. 患者或家属明确表达了顾虑、没听懂、不会操作、担心副作用、担心疗效等问题，并且该问题适合后续联系确认；
3. 对话中明确出现了可能影响治疗或复诊的执行问题，例如忘记服药、药物不够、检查未完成、症状反复、不会挂号；
4. 医生明确提到需要在某个时间点再次了解患者情况。

以下内容不能作为充分依据：

- 仅仅因为患者看过病；
- 模型认为“通常应该回访”；
- 模型根据疾病类型自行推断；
- 患者没有提到的交通、费用、预约、家属陪同等事项；
- 对话中没有明确出现的检查结果、医学阈值、药物剂量或风险结论。

四、任务数量与优先级

1. 可以生成 0 条任务；
2. 通常生成 1-3 条任务；
3. 只保留最有价值、最需要人工触达的任务；
4. 第 1 条任务必须紧扣本次接诊中最真实、最重要、尚未解决的问题；
5. 任务优先级按照以下顺序判断：

   医生明确安排的动作
   > 医生明确要求观察的事项
   > 患者明确提出但本次未解决的顾虑
   > 已明确出现的治疗执行困难
   > 其他有直接证据的后续事项

6. 不要把多个互不相关的动作写成一个任务；
7. 如果同一时间、同一目的下有多个紧密相关事项，可以合并成一个任务。

五、时间规则

1. 如果医生明确说了具体日期，按明确日期生成；
2. 如果医生明确说了相对时间，例如“3天后”“7天后”“下周二”，根据 recorded_at 计算；
3. 计算相对时间时，以 recorded_at 的日期和时区为准；
4. 如果只说“最近”“过几天”“六月初”“下个月”这类模糊时间，不要擅自转换成精确日期；
5. 如果无法确定合理的联系时间，不要编造日期；该任务可以不生成；
6. 只有在已经确定需要联系，但医生没有指定具体联系时刻时，才使用默认联系时刻；如果连联系日期都无法确定，则不生成任务；
7. contact_time 必须使用格式 YYYY-MM-DD HH:MM:SS；
8. 联系时间应使用自然的整点或常用时间，例如 09:00:00、10:00:00、15:00:00；
9. 不要生成奇怪的分钟和秒数；
10. 默认联系时间优先使用上午 10:00 或下午 15:00，但不得覆盖医生明确交代的时间；
11. 不得把“六月初”擅自转换成“6月19日”等具体日期；
12. 不得因为数据库字段 due_at 必须有值，就编造联系日期。

六、证据规则

1. 每条任务必须填写 evidence；
2. evidence 必须引用或准确转述录音中的具体原话或明确事实；
3. evidence 必须能够让随访人员理解为什么要做这条任务；
4. 不允许使用“根据对话内容”“结合患者情况”“为了了解患者恢复情况”“根据医生建议”等空泛表达作为唯一证据；
5. 如果找不到具体依据，不得生成该任务；
6. 不得把模型推测的内容写成医生已经说过的话；
7. purpose、background 和 script 都必须与 evidence 保持一致。

七、患者沟通话术规则

1. script 控制在 1-2 句；
2. 话术必须是护士或指定随访人员可以直接说出口的话；
3. 话术要承接本次接诊中的具体事项、患者原话或真实顾虑；
4. 可以提医生姓名，但不能冒充医生本人；
5. 推荐使用：“您好，我是{doctor_name}医生的助理，想跟您确认一下……”；
6. 不得以医生名义新增承诺；
7. 不得夸大疗效、制造焦虑、催促消费或逼迫患者复诊；
8. 不得在话术中新增诊断、药物、剂量、检查结果或医学结论；
9. 不要让患者一次回答过多无关问题；
10. 每条话术只围绕当前任务的一个明确动作；
11. 如果任务是提醒复诊，只提醒录音中明确的复诊安排，不得附加未经提及的交通、费用或预约服务。

八、联系方式规则

1. 如果录音中明确提到联系方式，按录音填写；
2. 如果录音没有明确联系方式，contact_method 使用 phone；
3. 不要自行推断为短信、微信或其他联系方式；
4. 联系方式不得影响任务事实和目的。

九、严格禁止

禁止生成：

1. 锁单、逼单、催成交、冲业绩类任务；
2. 患者没有提出或录音没有出现的交通、费用、预约、陪同安排；
3. 医生没有说过的新诊断、新检查、新药物、新剂量；
4. 模型自行推断的适应症、风险等级、医学阈值或治疗结论；
5. 空泛的“关心一下”“了解恢复情况”；
6. 只为了增加复诊率而生成的无依据提醒；
7. 可能让患者误以为医生本人正在直接联系的表达；
8. 将模糊时间自行变成精确日期；
9. 生成没有具体 evidence 支持的任务；
10. 为了凑够 1-3 条任务而扩大任务范围。

十、任务编辑边界

允许：

1. 改表达方式，使话术更自然、更容易被患者理解；
2. 改联系时间，使其符合明确依据和自然联系时间规则；
3. 对不合适、证据不足或无法执行的任务，允许退回或不生成。

不允许：

1. 改诊断；
2. 改药物；
3. 改药物剂量；
4. 改变医生原意；
5. 把模型推测的内容写成医生已经表达过的内容。

结果要求：每条任务必须对应一个明确动作。

十一、输出要求

只输出 JSON，不要输出 Markdown，不要输出解释文字，不要输出 JSON 之外的内容。

没有符合条件的任务时，必须输出：

{"tasks":[]}

有任务时，输出格式必须为：

{
  "tasks": [
    {
      "title": "",
      "contact_time": "",
      "purpose": "",
      "background": "",
      "script": "",
      "evidence": "",
      "contact_method": "phone",
      "executor": "护士/指定随访人员",
      "editable_fields": ["expression", "contact_time"]
    }
  ]
}

字段说明：

- title：一句话任务标题，必须具体，并且只表达一个明确动作；
- contact_time：联系时间，格式为 YYYY-MM-DD HH:MM:SS；
- purpose：为什么要联系患者；
- background：随访人员执行前需要知道的背景；
- script：可以直接发送或说给患者的话；如果 customer_name 有值，必须自然称呼患者姓名；
- evidence：本任务来自录音中的具体内容；
- contact_method：默认使用 phone；
- executor：默认使用护士/指定随访人员；
- editable_fields：固定输出 ["expression", "contact_time"]。
','
本次输入的客观信息：

tenant_id: {{tenant_id}}
recording_id: {{recording_id}}
encounter_id: {{encounter_id}}
employee_id: {{employee_id}}
doctor_name: {{doctor_name}}
customer_id: {{customer_id}}
customer_name: {{customer_name}}
recorded_at: {{recorded_at}}

以下是一次 Encounter 的完整清洗后转写：
{{cleaned_transcript}}

请严格只基于以上输入内容生成任务。
','{"type":"object","required":["tasks"],"properties":{"tasks":{"type":"array","items":{"type":"object","required":["title","contact_time","purpose","background","script","evidence","contact_method","executor","editable_fields"],"properties":{"title":{"type":"string"},"script":{"type":"string"},"purpose":{"type":"string"},"evidence":{"type":"string"},"executor":{"type":"string"},"background":{"type":"string"},"contact_time":{"type":"string"},"contact_method":{"enum":["phone","sms","wechat"],"type":"string"},"editable_fields":{"type":"array","items":{"type":"string"}}}}}}}'::jsonb,'v1',TRUE,0,1,1 WHERE NOT EXISTS (SELECT 1 FROM public.recording_analysis_prompts WHERE code='followup_task_generation');
UPDATE public.recording_analysis_prompts SET name='实时转写文字纠错',description='只修正实时 ASR 文字中的错别字、医学词语、标点和断句，不改变原始事实。',category='analysis',system_prompt='你是实时医疗转写文字校对助手。

你的任务只有一件事：校对 ASR 识别出的当前一句文字。

只允许：
1. 修正明显的错别字和同音识别错误；
2. 修正常见医学词语的识别错误；
3. 补充或调整标点和断句；
4. 保持原文的事实和表达范围。

绝对禁止：
1. 修改、猜测或补充姓名、数字、日期、时间、剂量、频次和单位；
2. 添加原文没有出现的症状、检查、诊断、药物或医嘱；
3. 做任何临床判断、用药安全判断或诊疗建议；
4. 改写成病历或总结全文。

如果原文无法可靠纠正，原样返回。
只输出一个合法 JSON 对象，不要 Markdown，不要解释文字。',user_prompt_template='请校对下面这段实时 ASR 文字，并返回纠错后的文字：

原始 ASR 文字：
{{transcript}}',output_schema='{"type":"object","required":["corrected_text"],"properties":{"corrected_text":{"type":"string"}}}'::jsonb,version='v1',is_active=TRUE,updated_by=0,updated_at=NOW() WHERE code='realtime_transcript_correction_v1';
INSERT INTO public.recording_analysis_prompts (code,name,description,category,system_prompt,user_prompt_template,output_schema,version,is_active,usage_count,created_by,updated_by) SELECT 'realtime_transcript_correction_v1','实时转写文字纠错','只修正实时 ASR 文字中的错别字、医学词语、标点和断句，不改变原始事实。','analysis','你是实时医疗转写文字校对助手。

你的任务只有一件事：校对 ASR 识别出的当前一句文字。

只允许：
1. 修正明显的错别字和同音识别错误；
2. 修正常见医学词语的识别错误；
3. 补充或调整标点和断句；
4. 保持原文的事实和表达范围。

绝对禁止：
1. 修改、猜测或补充姓名、数字、日期、时间、剂量、频次和单位；
2. 添加原文没有出现的症状、检查、诊断、药物或医嘱；
3. 做任何临床判断、用药安全判断或诊疗建议；
4. 改写成病历或总结全文。

如果原文无法可靠纠正，原样返回。
只输出一个合法 JSON 对象，不要 Markdown，不要解释文字。','请校对下面这段实时 ASR 文字，并返回纠错后的文字：

原始 ASR 文字：
{{transcript}}','{"type":"object","required":["corrected_text"],"properties":{"corrected_text":{"type":"string"}}}'::jsonb,'v1',TRUE,0,0,0 WHERE NOT EXISTS (SELECT 1 FROM public.recording_analysis_prompts WHERE code='realtime_transcript_correction_v1');
UPDATE public.llm_model_configs SET model_name='通义千问 Max',provider='dashscope',model_params='{"temperature":0.0,"max_tokens":4000,"timeout_seconds":120}'::jsonb,extra_params='{}'::jsonb,is_default=TRUE,is_active=TRUE,description='合规卫士平台默认模型配置',updated_at=NOW() WHERE tenant_id=0 AND function_type='compliance_check' AND model_code='qwen-max' AND deleted_at IS NULL;
INSERT INTO public.llm_model_configs (tenant_id,model_code,function_type,model_name,provider,model_params,extra_params,is_default,is_active,description,created_by) SELECT 0,'qwen-max','compliance_check','通义千问 Max','dashscope','{"temperature":0.0,"max_tokens":4000,"timeout_seconds":120}'::jsonb,'{}'::jsonb,TRUE,TRUE,'合规卫士平台默认模型配置',0 WHERE NOT EXISTS (SELECT 1 FROM public.llm_model_configs WHERE tenant_id=0 AND function_type='compliance_check' AND model_code='qwen-max' AND deleted_at IS NULL);
UPDATE public.llm_model_configs SET model_name='通义千问 Max',provider='dashscope',model_params='{"temperature":0.0,"max_tokens":4000,"timeout_seconds":120}'::jsonb,extra_params='{}'::jsonb,is_default=TRUE,is_active=TRUE,description='电子病历工作稿生成平台默认模型配置',updated_at=NOW() WHERE tenant_id=0 AND function_type='emr_candidate_generation' AND model_code='qwen-max' AND deleted_at IS NULL;
INSERT INTO public.llm_model_configs (tenant_id,model_code,function_type,model_name,provider,model_params,extra_params,is_default,is_active,description,created_by) SELECT 0,'qwen-max','emr_candidate_generation','通义千问 Max','dashscope','{"temperature":0.0,"max_tokens":4000,"timeout_seconds":120}'::jsonb,'{}'::jsonb,TRUE,TRUE,'电子病历工作稿生成平台默认模型配置',0 WHERE NOT EXISTS (SELECT 1 FROM public.llm_model_configs WHERE tenant_id=0 AND function_type='emr_candidate_generation' AND model_code='qwen-max' AND deleted_at IS NULL);
UPDATE public.llm_model_configs SET model_name='通义千问 Max',provider='dashscope',model_params='{"max_tokens":8192,"temperature":0,"response_format":"json","timeout_seconds":300}'::jsonb,extra_params='{}'::jsonb,is_default=TRUE,is_active=TRUE,description='电子病历大模型质检平台默认模型配置',updated_at=NOW() WHERE tenant_id=0 AND function_type='emr_quality_check' AND model_code='qwen-max' AND deleted_at IS NULL;
INSERT INTO public.llm_model_configs (tenant_id,model_code,function_type,model_name,provider,model_params,extra_params,is_default,is_active,description,created_by) SELECT 0,'qwen-max','emr_quality_check','通义千问 Max','dashscope','{"max_tokens":8192,"temperature":0,"response_format":"json","timeout_seconds":300}'::jsonb,'{}'::jsonb,TRUE,TRUE,'电子病历大模型质检平台默认模型配置',0 WHERE NOT EXISTS (SELECT 1 FROM public.llm_model_configs WHERE tenant_id=0 AND function_type='emr_quality_check' AND model_code='qwen-max' AND deleted_at IS NULL);
UPDATE public.llm_model_configs SET model_name='通义千问 Max',provider='dashscope',model_params='{"temperature":0,"max_tokens":6000,"timeout_seconds":120,"response_format":"json"}'::jsonb,extra_params='{}'::jsonb,is_default=TRUE,is_active=FALSE,description='门诊病历质量评分大模型逐项判断',updated_at=NOW() WHERE tenant_id=0 AND function_type='emr_score_evaluation' AND model_code='qwen-max' AND deleted_at IS NULL;
INSERT INTO public.llm_model_configs (tenant_id,model_code,function_type,model_name,provider,model_params,extra_params,is_default,is_active,description,created_by) SELECT 0,'qwen-max','emr_score_evaluation','通义千问 Max','dashscope','{"temperature":0,"max_tokens":6000,"timeout_seconds":120,"response_format":"json"}'::jsonb,'{}'::jsonb,TRUE,FALSE,'门诊病历质量评分大模型逐项判断',0 WHERE NOT EXISTS (SELECT 1 FROM public.llm_model_configs WHERE tenant_id=0 AND function_type='emr_score_evaluation' AND model_code='qwen-max' AND deleted_at IS NULL);
UPDATE public.llm_model_configs SET model_name='通义千问 Plus',provider='dashscope',model_params='{"temperature":0.0,"max_tokens":500,"timeout_seconds":15}'::jsonb,extra_params='{}'::jsonb,is_default=TRUE,is_active=TRUE,description='实时转写文字纠错平台默认模型配置',updated_at=NOW() WHERE tenant_id=0 AND function_type='realtime_transcript_correction' AND model_code='qwen-plus' AND deleted_at IS NULL;
INSERT INTO public.llm_model_configs (tenant_id,model_code,function_type,model_name,provider,model_params,extra_params,is_default,is_active,description,created_by) SELECT 0,'qwen-plus','realtime_transcript_correction','通义千问 Plus','dashscope','{"temperature":0.0,"max_tokens":500,"timeout_seconds":15}'::jsonb,'{}'::jsonb,TRUE,TRUE,'实时转写文字纠错平台默认模型配置',0 WHERE NOT EXISTS (SELECT 1 FROM public.llm_model_configs WHERE tenant_id=0 AND function_type='realtime_transcript_correction' AND model_code='qwen-plus' AND deleted_at IS NULL);

INSERT INTO public.emr_permission_assignments (tenant_id,employee_id,enabled,emr_role_code,scope,abilities_json,created_at,updated_at) SELECT e.tenant_id,e.id,TRUE,'emr_admin','tenant','["record.read","record.read_all","record.create","record.edit","record.submit","record.archive","record.history.read","template.manage","quality.manage"]'::jsonb,NOW(),NOW() FROM public.employees e WHERE e.deleted_at IS NULL AND lower(COALESCE(e.is_active::text,'true')) IN ('1','t','true','yes') AND EXISTS (SELECT 1 FROM public.institution_employee_roles er WHERE er.tenant_id=e.tenant_id AND er.employee_id=e.id AND lower(trim(er.role_code))='admin') ON CONFLICT (tenant_id,employee_id) DO NOTHING;
INSERT INTO public.followup_permission_assignments (tenant_id,employee_id,enabled,role_code,abilities_json,created_at,updated_at) SELECT e.tenant_id,e.id,TRUE,'followup_admin','["followup.task.read","followup.task.execute","followup.task.assign","followup.partnership.manage","followup.scope.manage","followup.permission.manage"]'::jsonb,NOW(),NOW() FROM public.employees e WHERE e.deleted_at IS NULL AND lower(COALESCE(e.is_active::text,'true')) IN ('1','t','true','yes') AND EXISTS (SELECT 1 FROM public.institution_employee_roles er WHERE er.tenant_id=e.tenant_id AND er.employee_id=e.id AND lower(trim(er.role_code))='admin') ON CONFLICT (tenant_id,employee_id) DO NOTHING;
INSERT INTO public.compliance_permission_assignments (tenant_id,employee_id,enabled,role_code,abilities_json,created_at,updated_at) SELECT e.tenant_id,e.id,TRUE,'compliance_admin','["compliance.finding.read","compliance.finding.review","compliance.rule.read","compliance.rule.manage","compliance.run.read","compliance.permission.manage"]'::jsonb,NOW(),NOW() FROM public.employees e WHERE e.deleted_at IS NULL AND lower(COALESCE(e.is_active::text,'true')) IN ('1','t','true','yes') AND EXISTS (SELECT 1 FROM public.institution_employee_roles er WHERE er.tenant_id=e.tenant_id AND er.employee_id=e.id AND lower(trim(er.role_code))='admin') ON CONFLICT (tenant_id,employee_id) DO NOTHING;

-- Project only persisted completed/manual-review scores; all other records remain NULL.
UPDATE public.emr_records r SET current_total_score=s.total_score,current_grade=s.grade,current_score_status=s.status,current_score_version=s.score_version FROM public.emr_record_score_current s WHERE s.record_id=r.id AND s.status IN ('已完成','待人工复核') AND s.total_score IS NOT NULL;
UPDATE public.emr_records SET current_total_score=NULL,current_grade=NULL WHERE current_score_status NOT IN ('已完成','待人工复核') OR current_score_status IS NULL OR current_total_score IS NULL;
UPDATE public.emr_record_snapshots sn SET total_score=s.total_score,grade=s.grade,score_version=s.score_version,score_status=s.status FROM public.emr_record_score_current s WHERE s.record_id=sn.record_id AND s.snapshot_id=sn.id AND s.status IN ('已完成','待人工复核') AND s.total_score IS NOT NULL;

-- ICD-10 catalog is intentionally not copied from development execution data. Import the approved 33,263-row catalog separately from the controlled source file, then run the verification below.
-- Final structural and formal-data checks.
DO $$ DECLARE t TEXT; BEGIN FOR t IN SELECT unnest(ARRAY['encounters','emr_templates','emr_fields','emr_template_fields','emr_records','emr_record_snapshots','emr_process_records','emr_quality_rule_set_versions','emr_quality_rule_definitions','emr_quality_runs','emr_quality_results','emr_record_score_current','emr_record_score_current_items','emr_score_standards','emr_icd10_diagnosis_catalog','emr_permission_assignments','emr_badge_sessions','emr_followup_deliveries','followup_generation_runs','followup_data_scopes','followup_permission_assignments','compliance_rules','compliance_check_runs','compliance_findings','compliance_permission_assignments']) LOOP IF to_regclass('public.'||t) IS NULL THEN RAISE EXCEPTION '收敛后目标表缺失: %',t; END IF; END LOOP; END $$;
DO $$ BEGIN IF (SELECT count(*) FROM public.emr_quality_rule_definitions WHERE version='2026-10-04-v2' AND status='published') <> 43 THEN RAISE EXCEPTION '正式门诊质检规则必须为43条'; END IF; IF NOT EXISTS (SELECT 1 FROM public.emr_score_standards WHERE code='outpatient_record_quality' AND version='2017' AND status='published') THEN RAISE EXCEPTION '门诊病历评分标准缺失'; END IF; END $$;


-- 20261011_003 additive target-state convergence extensions.
-- Historical migration files are not replayed; these statements describe the audited current target.
ALTER TABLE IF EXISTS public.recording_tasks
  ADD COLUMN IF NOT EXISTS task_category VARCHAR(64),
  ADD COLUMN IF NOT EXISTS anonymous_patient BOOLEAN NOT NULL DEFAULT FALSE,
  ADD COLUMN IF NOT EXISTS patient_age INTEGER,
  ADD COLUMN IF NOT EXISTS patient_gender VARCHAR(32),
  ADD COLUMN IF NOT EXISTS phone_script TEXT,
  ADD COLUMN IF NOT EXISTS wechat_content TEXT,
  ADD COLUMN IF NOT EXISTS execution_result VARCHAR(64),
  ADD COLUMN IF NOT EXISTS execution_difficulty TEXT,
  ADD COLUMN IF NOT EXISTS doctor_escalation TEXT,
  ADD COLUMN IF NOT EXISTS next_contact_at TIMESTAMPTZ,
  ADD COLUMN IF NOT EXISTS escalation_status TEXT,
  ADD COLUMN IF NOT EXISTS escalation_resolution TEXT,
  ADD COLUMN IF NOT EXISTS escalation_resolved_by BIGINT,
  ADD COLUMN IF NOT EXISTS escalation_resolved_at TIMESTAMPTZ,
  ADD COLUMN IF NOT EXISTS time_basis TEXT,
  ADD COLUMN IF NOT EXISTS patient_replied BOOLEAN,
  ADD COLUMN IF NOT EXISTS revisit_attended BOOLEAN,
  ADD COLUMN IF NOT EXISTS issue_resolved BOOLEAN,
  ADD COLUMN IF NOT EXISTS patient_understanding TEXT,
  ADD COLUMN IF NOT EXISTS encounter_id INTEGER;
CREATE INDEX IF NOT EXISTS idx_recording_tasks_followup_anonymous ON public.recording_tasks (tenant_id, anonymous_patient, status);
CREATE INDEX IF NOT EXISTS idx_followup_escalation_queue ON public.recording_tasks(tenant_id, escalation_status) WHERE source_type IN ('follow_up','followup');

CREATE TABLE IF NOT EXISTS public.followup_task_audits (
  id BIGSERIAL PRIMARY KEY, tenant_id BIGINT NOT NULL, task_id BIGINT NOT NULL,
  action VARCHAR(64) NOT NULL, detail JSONB NOT NULL DEFAULT '{}'::jsonb,
  created_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);
CREATE INDEX IF NOT EXISTS idx_followup_task_audits_task ON public.followup_task_audits(tenant_id, task_id, created_at DESC);

CREATE TABLE IF NOT EXISTS public.compliance_check_items (
  id UUID PRIMARY KEY DEFAULT gen_random_uuid(), tenant_id BIGINT NOT NULL,
  subject VARCHAR(32) NOT NULL, source_type VARCHAR(48) NOT NULL, source_id VARCHAR(128) NOT NULL,
  source_version VARCHAR(128) NOT NULL, scene_code VARCHAR(128) NOT NULL, role_code VARCHAR(64) NOT NULL DEFAULT '',
  employee_id BIGINT, theme_key VARCHAR(160) NOT NULL, question TEXT NOT NULL, risk_hypothesis TEXT NOT NULL,
  primary_risk_type VARCHAR(64) NOT NULL, related_risk_types JSONB NOT NULL DEFAULT '[]'::jsonb,
  evidence JSONB NOT NULL DEFAULT '[]'::jsonb, check_basis JSONB NOT NULL DEFAULT '[]'::jsonb,
  suggested_actions JSONB NOT NULL DEFAULT '[]'::jsonb, priority VARCHAR(16) NOT NULL,
  status VARCHAR(24) NOT NULL DEFAULT 'pending', started_by BIGINT, started_at TIMESTAMPTZ,
  processed_by BIGINT, processed_at TIMESTAMPTZ, conclusion VARCHAR(64), process_reason TEXT,
  process_actions JSONB NOT NULL DEFAULT '[]'::jsonb, process_note TEXT NOT NULL DEFAULT '',
  created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(), updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
  CONSTRAINT compliance_check_items_tenant_check CHECK (tenant_id > 0),
  CONSTRAINT compliance_check_items_status_check CHECK (status IN ('pending','in_progress','processed')),
  CONSTRAINT compliance_check_items_priority_check CHECK (priority IN ('urgent','important','normal')),
  CONSTRAINT compliance_check_items_risk_check CHECK (primary_risk_type IN ('patient_decision','medical_safety','legal_evidence','business_reputation','management_system')),
  CONSTRAINT compliance_check_items_related_types_check CHECK (jsonb_typeof(related_risk_types) = 'array'),
  CONSTRAINT compliance_check_items_evidence_check CHECK (jsonb_typeof(evidence) = 'array'),
  CONSTRAINT compliance_check_items_basis_check CHECK (jsonb_typeof(check_basis) = 'array'),
  CONSTRAINT compliance_check_items_actions_check CHECK (jsonb_typeof(suggested_actions) = 'array'),
  UNIQUE (tenant_id, source_type, source_id, source_version, theme_key)
);
CREATE TABLE IF NOT EXISTS public.compliance_check_item_sources (
  item_id UUID NOT NULL REFERENCES public.compliance_check_items(id) ON DELETE CASCADE,
  finding_id UUID NOT NULL REFERENCES public.compliance_findings(id) ON DELETE CASCADE,
  run_id UUID NOT NULL REFERENCES public.compliance_check_runs(id) ON DELETE CASCADE,
  rule_code VARCHAR(128) NOT NULL, created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
  PRIMARY KEY (item_id, finding_id)
);
CREATE TABLE IF NOT EXISTS public.compliance_check_item_process_records (
  id UUID PRIMARY KEY DEFAULT gen_random_uuid(), item_id UUID NOT NULL REFERENCES public.compliance_check_items(id) ON DELETE CASCADE,
  tenant_id BIGINT NOT NULL, actor_id BIGINT NOT NULL, idempotency_key VARCHAR(256) NOT NULL,
  conclusion VARCHAR(64) NOT NULL, reason TEXT NOT NULL, actions JSONB NOT NULL DEFAULT '[]'::jsonb,
  note TEXT NOT NULL DEFAULT '', created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(), UNIQUE (item_id, idempotency_key)
);
CREATE INDEX IF NOT EXISTS compliance_check_items_tenant_status_idx ON public.compliance_check_items (tenant_id,status,created_at DESC);
CREATE INDEX IF NOT EXISTS compliance_check_item_sources_finding_idx ON public.compliance_check_item_sources (finding_id);

-- Current prompt/rule contract updates are applied idempotently by the audited migration fragments.

-- Safe formal contract updates only; no runtime backfill or destructive cleanup.

-- BEGIN SAFE TARGET FRAGMENT internal/store/migrations/20261007_001_followup_quality_rules.sql --
-- 随访任务质量规则：限制任务数量，避免销售化和重复打扰。
-- 幂等：按稳定 code 更新现有提示词，不改变历史版本记录。
-- 署名：Codex
-- 时间：2026-10-07

UPDATE recording_analysis_prompts
SET system_prompt = system_prompt || $rules$

十二、客服可执行性和关怀规则（新增）

1. 默认输出 0-1 条任务，特殊情况下最多 2 条；不得为了凑数量生成任务。
2. 同一次就诊多个事项默认合并为一次联系；只有联系时间、紧急程度或执行岗位明显不同才拆分。
3. 不得生成确认患者是否决定手术、是否同意手术、是否被成功率说服、是否准备消费或成交等任务。
4. 只有患者明确表达疑问、犹豫或执行困难时，才生成理解与疑问类任务；重点是了解困难或协助医生解释，不替患者作医疗决定。
5. 任务表达必须体现自然问候、具体关心、明确问题和下一步帮助。关怀必须基于本次就诊的具体事实，不得使用空泛模板或完整复述录音。
6. 有真实姓名时使用中性、尊重的姓名称呼；不得根据姓名、年龄或录音猜测性别、年龄或“阿姨/叔叔”等称呼。无法确认时使用“您好”。
7. 电话话术控制在 1-2 句、约 10 秒内；微信内容使用短段落，先关怀，再说明事项，最后提出一个明确问题和可提供的帮助。老年患者少用术语和 emoji，emoji 不得替代信息。
8. 如果没有明确后续动作、患者疑问或明确时间节点，必须输出空数组。
9. 任务必须优先服务于患者理解、执行和获得帮助；复诊率、忠诚度等医院目标只能通过真实服务自然实现，不得直接转化为催复诊或催成交话术。
$rules$
WHERE code = 'followup_task_generation'
  AND position('十二、客服可执行性和关怀规则（新增）' IN system_prompt) = 0;

-- END SAFE TARGET FRAGMENT internal/store/migrations/20261007_001_followup_quality_rules.sql --

-- BEGIN SAFE TARGET FRAGMENT internal/store/migrations/20261007_003_followup_channel_prompt.sql --
UPDATE recording_analysis_prompts
SET system_prompt = system_prompt || $rules$

十三、结构化渠道输出规则
每项任务必须提供 category（execution 执行类、understanding 理解与疑问类、milestone 节点类）、purpose、evidence、phone_script 和 wechat_content。
phone_script 为自然问候、具体关心和一个明确问题，1～2句，不超过120字；wechat_content 按关怀、事项、明确问题及帮助分段，不超过600字。
script 保留为 phone_script 的兼容副本。内部背景只放在 background，不加入对患者的长篇话术。
同次就诊、同一渠道、同一联系时间的事项合并成一次联系；只有明确不同节点或执行岗位才拆分。没有真实后续服务需求输出 {"tasks":[]}。
不得根据姓名推测年龄性别，不生成催手术、成交、促销、说服消费的任务。理解疑问类须有患者明确疑问或困难证据。
时间优先采用医生明确日期，其次客观复查或治疗节点；缺少时间依据不得任意生成日期。
$rules$
WHERE code='followup_task_generation'
AND position('十三、结构化渠道输出规则' IN system_prompt)=0;

-- END SAFE TARGET FRAGMENT internal/store/migrations/20261007_003_followup_channel_prompt.sql --

-- BEGIN SAFE TARGET FRAGMENT internal/store/migrations/20261009_002_require_western_diagnosis.sql --
BEGIN;
UPDATE public.emr_template_fields tf
SET is_visible=TRUE,is_required=TRUE,updated_at=NOW()
WHERE tf.field_id IN (SELECT id FROM public.emr_fields WHERE tenant_id=0 AND code='diagnosis')
  AND tf.template_id IN (SELECT id FROM public.emr_templates WHERE tenant_id=0 AND status='published');
COMMIT;

-- END SAFE TARGET FRAGMENT internal/store/migrations/20261009_002_require_western_diagnosis.sql --

-- BEGIN SAFE TARGET FRAGMENT internal/store/migrations/20261009_003_tcm_semantic_consistency_rules.sql --
BEGIN;
UPDATE public.emr_quality_rule_definitions
SET executor_kind='llm',
    action_policy=jsonb_build_object(
      'save', jsonb_build_object('enabled', false, 'blocking', false),
      'complete', jsonb_build_object('enabled', true, 'blocking', false),
      'archive', jsonb_build_object('enabled', true, 'blocking', false)
    ),
    evaluation_spec=evaluation_spec || '{"semantic":true}'::jsonb,
    updated_at=NOW()
WHERE version='2026-10-08-v1' AND rule_code IN ('TCM-DX-002','TCM-DX-003');
COMMIT;

-- END SAFE TARGET FRAGMENT internal/store/migrations/20261009_003_tcm_semantic_consistency_rules.sql --

-- BEGIN SAFE TARGET FRAGMENT internal/store/migrations/20261010_003_followup_time_basis_contract.sql --
-- 统一随访任务时间字段契约，避免模型继续返回旧格式。
-- 幂等：按提示词 code 和固定标记追加一次。
-- 署名：Codex；时间：2026-10-10。

UPDATE recording_analysis_prompts
SET
  system_prompt = system_prompt || $followup_time_contract$

十四、时间依据字段（必须输出）
每条任务必须额外输出 time_basis，说明联系时间的事实来源，只能填写以下之一：
- doctor_explicit：医生明确说出的日期或相对时间；
- objective_node：明确的复诊、检查或治疗节点；
- hospital_default：确有必要联系但医生未指定时间，由医院默认规则安排；
如果没有明确后续动作或无法确定合理联系时间，输出空数组，不得编造日期。
最终每条任务必须包含：title、contact_time、time_basis、purpose、background、script、phone_script、wechat_content、evidence、contact_method、executor、category、editable_fields。
$followup_time_contract$,
  user_prompt_template = user_prompt_template || $followup_user_contract$

补充要求：JSON 中每条任务必须包含 time_basis 字段，取值只能是 doctor_explicit、objective_node 或 hospital_default；不得省略。
$followup_user_contract$,
  updated_at = NOW()
WHERE code = 'followup_task_generation'
  AND position('十四、时间依据字段（必须输出）' IN system_prompt) = 0;

-- END SAFE TARGET FRAGMENT internal/store/migrations/20261010_003_followup_time_basis_contract.sql --

-- BEGIN SAFE TARGET FRAGMENT internal/store/migrations/20261011_001_emr_generation_structured_contract.sql --
-- Replace contradictory all-string instructions with the current template contract.
-- Do not rewrite registered migrations or convert legacy diagnosis strings.
UPDATE public.recording_analysis_prompts SET
system_prompt=$system$
你是门急诊电子病历文书生成助手，根据完整接诊转写、患者信息、就诊信息和当前模板生成供医生核对的病历工作稿。

【临床成文原则】
只记录可靠事实及医生明确表达的诊断、辨证、病机、治法、处方、处置、医嘱和复诊安排。允许去除口语、重复和无关对话，合并同义表达，形成简洁连贯的病历段落，但不得补造症状、体征、诊断、药品、剂量、检查结果或疗效。患者猜测不能变为医生诊断；医生的不确定表达必须保留。
主诉概括主要症状、部位和明确的持续时间；现病史按起病、演变、伴随表现和已有处理组织。病史只写明确内容。查体和检查区分实际结果与建议检查。处方、处置和医嘱区分已执行、计划、建议与既往用药。复诊时间不能从药量、疗程或几副药推断。
中医口语辨证可整理为医生原意，例如胃热、气滞、瘀血等；辨证依据放在相应依据字段，不能自行补成未表达的中医病名。四诊、舌象、脉象和治法只写明确内容。饮食或自行购买建议不能当成正式处方。
不做质检，不输出质量评价。缺失、矛盾和需核实的内容放入 unresolved_items，不在正文写“未提及”“尚未询问”“待医生确认”。

【唯一输出协议】
只输出合法 JSON 对象；顶层只有 working_content、unresolved_items。
working_content 必须包含当前模板全部栏位编码，不能新增、删除或改名。每个值的类型由对应栏位定义决定：普通文本栏位是自然语言字符串，缺失时为 ""；结构化栏位必须是 JSON 对象，按提供的结构定义输出子字段，缺失子项为 ""，不能将整个对象变为字符串、null或数组。结构化对象内的文本应当简洁可读，不把栏位名当成正文。

【双诊断契约】
diagnosis 只表示西医诊断，必须为对象，且只能使用 diagnosis_name、icd10_code、basis 三个字符串键。
tcm_diagnosis 只表示中医诊断，必须为对象，且只能使用 diagnosis_name、pattern_name、basis 三个字符串键。
两种诊断独立，不能互相替代、翻译或兜底。只有中医模板 tcm 输出 tcm_diagnosis；其他模板不得新增此栏位。
即使录音未明确诊断，也必须保留对象及空字符串子项，并在 unresolved_items 记录缺失。模板必填不允许模型编造诊断来补齐。不得输出 text、name、diagnosis_code 等别名或顶层 icd10_code；中医诊断不得包含 ICD-10。
ICD-10 不得凭猜测生成；没有可靠编码时 icd10_code 留空。不确定程度保留在诊断文字中。
中医模板双诊断的合法空值示例（仅示范类型，仍须输出模板全部栏位）：
{"diagnosis":{"diagnosis_name":"","icd10_code":"","basis":""},"tcm_diagnosis":{"diagnosis_name":"","pattern_name":"","basis":""}}

unresolved_items 必须为数组，每项只能有 field_code、reason 两个字符串键；field_code 来自当前模板，reason 具体描述需要核实的问题。没有问题时为 []。不要输出 Markdown、代码围栏或解释。
$system$,
user_prompt_template=$user$
请根据下列完整接诊材料生成病历工作稿，只输出合法 JSON。
【输入模式】
{{input_mode}}
【当前模板】
{{template}}
【栏位与结构定义】
{{template_fields}}
【患者信息】
{{patient_context}}
【就诊信息】
{{encounter_context}}
【完整接诊转写】
{{transcript}}

输出顶层只能是 working_content 和 unresolved_items。
working_content 输出模板所有栏位；文本栏位返回字符串，结构化栏位按给定结构返回对象。缺失文本或子项填 ""，缺失结构化栏位保留对象，不用 "" 代替对象。
diagnosis 必须为 {"diagnosis_name":"","icd10_code":"","basis":""} 形状；tcm 模板的 tcm_diagnosis 必须为 {"diagnosis_name":"","pattern_name":"","basis":""} 形状。两种诊断分别从医生明确表达提取，不能翻译或推断补全。
unresolved_items 使用 [{"field_code":"模板栏位编码","reason":"具体原因"}] 形状，无需核实时为 []。
不要复述模板元数据，不输出质检结论，不新增字段。
$user$,
output_schema=$schema${"type":"object","required":["working_content","unresolved_items"],"additionalProperties":false,"properties":{"working_content":{"type":"object","description":"模板文本栏位为字符串，结构化栏位为对象；诊断严格遵循独立双诊断契约。","properties":{"diagnosis":{"type":"object","required":["diagnosis_name","icd10_code","basis"],"additionalProperties":false,"properties":{"diagnosis_name":{"type":"string"},"icd10_code":{"type":"string"},"basis":{"type":"string"}}},"tcm_diagnosis":{"type":"object","required":["diagnosis_name","pattern_name","basis"],"additionalProperties":false,"properties":{"diagnosis_name":{"type":"string"},"pattern_name":{"type":"string"},"basis":{"type":"string"}}}},"additionalProperties":{"anyOf":[{"type":"string"},{"type":"object"}]}},"unresolved_items":{"type":"array","items":{"type":"object","required":["field_code","reason"],"additionalProperties":false,"properties":{"field_code":{"type":"string"},"reason":{"type":"string"}}}}}}$schema$::json,
updated_at=NOW()
WHERE code='emr_working_draft_generation_v1' AND is_active=TRUE;

-- JSON response format requires the word JSON in actual inference messages.
-- Candidate diagnosis content must also obey the strict western diagnosis shape.
UPDATE public.recording_analysis_prompts SET
system_prompt=$candidate$
你是电子病历候选内容整理助手。只根据可靠的医生接诊转写整理待医生核对的内容，不补造事实、不推断诊断、不输出质检结论或新医疗建议。患者猜测不得作为医生诊断。
只输出一个合法 JSON 对象，顶层为 candidates 数组。每项含 section_code、content、source_evidence；source_evidence 含 transcript_excerpt，引用支持该候选的原文。没有可靠内容的栏目不输出，沒有候选时输出 {"candidates":[]}。
只使用请求提供的栏目编码。diagnosis 表示西医诊断，content 必须为 {"diagnosis_name":"医生明确的西医诊断","icd10_code":"","basis":"明确依据或空字符串"}；禁止 text、name、diagnosis_code 等旧别名。不得把中医病名、证候或病机翻译成西医诊断；沒有明确西医诊断时不输出 diagnosis 候选。其他栏目 content 使用对象 {"text":"简洁连贯的病历文字"}。
不要输出 Markdown、代码围栏或解释。
$candidate$,
user_prompt_template=$candidate_user$
根据接诊转写生成候选，仅输出合法 JSON：{"candidates":[{"section_code":"栏目编码","content":{},"source_evidence":{"transcript_excerpt":"转写原文"}}]}。
录音 ID：{{recording_id}}
分析管线：{{pipeline_code}}
允许栏目：chief_complaint、present_illness、past_history、personal_history、family_history、allergy_history、physical_exam、auxiliary_exam、diagnosis、prescription、disposition、medical_advice、followup、supplement。
diagnosis.content 必须是西医诊断对象，使用 diagnosis_name、icd10_code、basis；只整理医生明确表达，无可靠内容则跳过。其他栏目 content 使用 text 字符串。
接诊转写：
{{transcript}}
$candidate_user$,
updated_at=NOW()
WHERE code='emr_candidate_generation_v1' AND is_active=TRUE;

-- END SAFE TARGET FRAGMENT internal/store/migrations/20261011_001_emr_generation_structured_contract.sql --

-- BEGIN SAFE TARGET FRAGMENT internal/store/migrations/20261011_002_seed_emr_candidate_json_contract.sql --
-- Ensure the optional EMR candidate-generation prompt exists and explicitly requests JSON.
INSERT INTO public.recording_analysis_prompts (code,name,description,category,system_prompt,user_prompt_template,output_schema,version,is_active,usage_count,created_by,updated_by,created_at,updated_at)
VALUES ('emr_candidate_generation_v1','电子病历候选生成','根据医生接诊转写生成供医生核对的候选病历内容。','analysis',
$$你是电子病历候选内容整理助手。只根据可靠的医生接诊转写整理待医生核对的内容，不补造事实、不推断诊断、不输出质检结论或新医疗建议。患者猜测不得作为医生诊断。
请只输出一个合法 JSON 对象，顶层必须是 candidates 数组；没有候选时输出 {"candidates":[]}。每项含 section_code、content、source_evidence，source_evidence 含 transcript_excerpt。只使用请求提供的栏目编码。
diagnosis 表示西医诊断，content 必须是 {"diagnosis_name":"","icd10_code":"","basis":""} 结构；只整理医生明确表达的西医诊断，无可靠内容则不输出。禁止 text、name、diagnosis_code 等旧别名，不得把中医病名或证候翻译成西医诊断。其他栏目 content 使用 {"text":"简洁连贯的病历文字"} 结构。
不要输出 Markdown、代码围栏或解释。$$,
$$根据接诊转写生成候选，只输出合法 JSON 对象。
录音 ID：{{recording_id}}
分析管线：{{pipeline_code}}
允许栏目：chief_complaint、present_illness、past_history、personal_history、family_history、allergy_history、physical_exam、auxiliary_exam、diagnosis、prescription、disposition、medical_advice、followup、supplement。
diagnosis.content 只能使用 diagnosis_name、icd10_code、basis，并且必须是西医诊断对象；其他栏目 content 使用 text 字符串。source_evidence.transcript_excerpt 必须引用原文。
接诊转写：
{{transcript}}$$,
'{"type":"object","required":["candidates"],"additionalProperties":false,"properties":{"candidates":{"type":"array","items":{"type":"object","required":["section_code","content","source_evidence"],"additionalProperties":false,"properties":{"section_code":{"type":"string"},"content":{"type":"object"},"source_evidence":{"type":"object","required":["transcript_excerpt"],"additionalProperties":false,"properties":{"transcript_excerpt":{"type":"string"}}}}}}}}'::json,'v2',TRUE,0,0,0,NOW(),NOW())
ON CONFLICT (code) DO UPDATE SET name=EXCLUDED.name,description=EXCLUDED.description,category=EXCLUDED.category,system_prompt=EXCLUDED.system_prompt,user_prompt_template=EXCLUDED.user_prompt_template,output_schema=EXCLUDED.output_schema,version=EXCLUDED.version,is_active=TRUE,updated_at=NOW();

-- END SAFE TARGET FRAGMENT internal/store/migrations/20261011_002_seed_emr_candidate_json_contract.sql --

-- BEGIN SAFE TARGET FRAGMENT internal/store/migrations/20261008_001_dual_diagnosis_contract.sql --
-- 中医门诊病历同时使用西医诊断和中医诊断；两者都是正式独立栏位。

UPDATE public.emr_fields
SET name='西医诊断', input_type='结构化内容',
    structure_json='{"items":["diagnosis_name","icd10_code","basis"]}'::jsonb,
    description='diagnosis：西医诊断；使用 ICD-10 编码。', updated_at=NOW()
WHERE tenant_id=0 AND code='diagnosis';

UPDATE public.emr_fields
SET name='中医诊断', input_type='结构化内容',
    structure_json='{"items":["diagnosis_name","pattern_name","basis"]}'::jsonb,
    description='tcm_diagnosis：中医诊断；填写中医病名、证候和辨证依据，不填写 ICD-10。', updated_at=NOW()
WHERE tenant_id=0 AND code='tcm_diagnosis';

USING public.emr_fields f, public.emr_templates t
WHERE tf.field_id=f.id AND tf.template_id=t.id AND f.tenant_id=0
  AND f.code='tcm_diagnosis' AND t.tenant_id=0 AND t.code<>'tcm';

INSERT INTO public.emr_template_fields (tenant_id,template_id,field_id,display_order,is_visible,is_required,description,created_by)
SELECT 0,t.id,f.id,95,TRUE,TRUE,'中医门诊必须填写中医诊断',0
FROM public.emr_templates t JOIN public.emr_fields f ON f.tenant_id=0 AND f.code='tcm_diagnosis'
WHERE t.tenant_id=0 AND t.code='tcm' AND t.status='published'
ON CONFLICT (template_id,field_id) DO UPDATE SET display_order=EXCLUDED.display_order,is_visible=TRUE,is_required=TRUE,description=EXCLUDED.description,updated_at=NOW();

UPDATE public.emr_template_fields tf
SET display_order=90,is_visible=TRUE,is_required=TRUE,description='必须填写西医诊断',updated_at=NOW()
WHERE tf.template_id IN (SELECT id FROM public.emr_templates WHERE tenant_id=0 AND code='tcm')
  AND tf.field_id IN (SELECT id FROM public.emr_fields WHERE tenant_id=0 AND code='diagnosis');

INSERT INTO public.emr_quality_rule_definitions
(rule_code,version,section_code,name,source_text,product_text,standard_points,executor_kind,input_spec,evaluation_spec,action_policy,message_templates,status)
VALUES
('TCM-DX-001','2026-10-04-v2','diagnosis','未填写中医诊断','中医门诊病历必须填写中医诊断','中医门诊病历必须填写中医诊断',10,'program',
 '{"paths":["record.content.tcm_diagnosis","record.context.template_code"]}'::jsonb,
 '{"mode":"tcm_diagnosis_presence_only","per_item":false,"instruction":"仅中医模板适用；没有中医诊断时阻断。","scoring_group":""}'::jsonb,
 '{"save":{"enabled":true,"blocking":false},"complete":{"enabled":true,"blocking":true},"archive":{"enabled":true,"blocking":true}}'::jsonb,
 '{"save":"请填写中医诊断。","complete":"没有填写中医诊断，不能完成病历。","archive":"没有填写中医诊断，不能归档。"}'::jsonb,'published')
ON CONFLICT (rule_code,version) DO UPDATE SET section_code=EXCLUDED.section_code,name=EXCLUDED.name,source_text=EXCLUDED.source_text,product_text=EXCLUDED.product_text,standard_points=EXCLUDED.standard_points,executor_kind=EXCLUDED.executor_kind,input_spec=EXCLUDED.input_spec,evaluation_spec=EXCLUDED.evaluation_spec,action_policy=EXCLUDED.action_policy,message_templates=EXCLUDED.message_templates,status=EXCLUDED.status,updated_at=NOW();

INSERT INTO public.emr_quality_rule_set_versions(version,status,scoring_ready,note)
VALUES ('2026-10-08-v1','published',TRUE,'双诊断独立字段；中医模板要求中西医诊断分别填写')
ON CONFLICT (version) DO UPDATE SET status='published', scoring_ready=TRUE, note=EXCLUDED.note, updated_at=NOW();
INSERT INTO public.emr_quality_rule_definitions
(rule_code,version,section_code,name,source_text,product_text,standard_points,executor_kind,input_spec,evaluation_spec,action_policy,message_templates,status)
SELECT rule_code,'2026-10-08-v1',section_code,name,source_text,product_text,standard_points,executor_kind,input_spec,evaluation_spec,action_policy,message_templates,'published'
FROM public.emr_quality_rule_definitions WHERE version='2026-10-04-v2'
ON CONFLICT (rule_code,version) DO NOTHING;
INSERT INTO public.emr_quality_rule_definitions
(rule_code,version,section_code,name,source_text,product_text,standard_points,executor_kind,input_spec,evaluation_spec,action_policy,message_templates,status)
VALUES
('TCM-DX-001','2026-10-08-v1','diagnosis','未填写中医诊断','中医门诊病历必须填写中医诊断','中医门诊病历必须填写中医诊断',10,'program','{"paths":["record.content.tcm_diagnosis","record.context.template_code"]}', '{"mode":"tcm_diagnosis_presence_only"}', '{"save":{"enabled":true,"blocking":false},"complete":{"enabled":true,"blocking":true},"archive":{"enabled":true,"blocking":true}}', '{"save":"请填写中医诊断。","complete":"没有填写中医诊断，不能完成病历。","archive":"没有填写中医诊断，不能归档。"}','published'),
('TCM-DX-002','2026-10-08-v1','diagnosis','中医诊断依据不足','中医诊断应有证候和辨证依据','中医诊断应有证候和辨证依据',5,'program','{"paths":["record.content.tcm_diagnosis"]}', '{"mode":"tcm_diagnosis_basis"}', '{"save":{"enabled":true,"blocking":false},"complete":{"enabled":false,"blocking":false},"archive":{"enabled":false,"blocking":false}}', '{"save":"请补充中医诊断的证候或辨证依据。"}','published'),
('TCM-DX-003','2026-10-08-v1','diagnosis','中医诊断填写了西医编码','中医诊断不使用ICD-10编码','中医诊断不使用ICD-10编码',5,'program','{"paths":["record.content.tcm_diagnosis"]}', '{"mode":"tcm_diagnosis_no_icd"}', '{"save":{"enabled":true,"blocking":false},"complete":{"enabled":true,"blocking":true},"archive":{"enabled":true,"blocking":true}}', '{"save":"中医诊断不能填写ICD-10编码。","complete":"中医诊断不能填写ICD-10编码。","archive":"中医诊断不能填写ICD-10编码。"}','published')
ON CONFLICT (rule_code,version) DO UPDATE SET status='published', updated_at=NOW();
UPDATE public.emr_quality_rule_set_versions SET status='retired', updated_at=NOW() WHERE version='2026-10-04-v2';
-- END SAFE TARGET FRAGMENT internal/store/migrations/20261008_001_dual_diagnosis_contract.sql --


-- BEGIN SAFE TARGET FRAGMENT internal/store/migrations/20261009_001_finalize_dual_diagnosis_contract.sql --
UPDATE public.emr_fields SET name='西医诊断',input_type='结构化内容',structure_json='{"items":["diagnosis_name","icd10_code","basis"]}'::jsonb,description='diagnosis：西医诊断；使用 ICD-10 编码。',updated_at=NOW() WHERE tenant_id=0 AND code='diagnosis';
UPDATE public.emr_fields SET name='中医诊断',input_type='结构化内容',structure_json='{"items":["diagnosis_name","pattern_name","basis"]}'::jsonb,description='tcm_diagnosis：中医诊断；填写中医病名、证候和辨证依据，不填写 ICD-10。',updated_at=NOW() WHERE tenant_id=0 AND code='tcm_diagnosis';
UPDATE public.emr_template_fields tf SET display_order=90,is_visible=TRUE,is_required=TRUE,description='必须填写西医诊断',updated_at=NOW() WHERE tf.template_id IN (SELECT id FROM public.emr_templates WHERE tenant_id=0 AND code='tcm') AND tf.field_id IN (SELECT id FROM public.emr_fields WHERE tenant_id=0 AND code='diagnosis');
INSERT INTO public.emr_template_fields (tenant_id,template_id,field_id,display_order,is_visible,is_required,description,created_by) SELECT 0,t.id,f.id,95,TRUE,TRUE,'中医门诊必须填写中医诊断',0 FROM public.emr_templates t JOIN public.emr_fields f ON f.tenant_id=0 AND f.code='tcm_diagnosis' WHERE t.tenant_id=0 AND t.code='tcm' AND t.status='published' ON CONFLICT (template_id,field_id) DO UPDATE SET display_order=95,is_visible=TRUE,is_required=TRUE,description=EXCLUDED.description,updated_at=NOW();
INSERT INTO public.emr_quality_rule_set_versions(version,status,scoring_ready,note) VALUES ('2026-10-08-v1','published',TRUE,'双诊断独立字段；中医模板要求中西医诊断分别填写') ON CONFLICT (version) DO UPDATE SET status='published',scoring_ready=TRUE,note=EXCLUDED.note,updated_at=NOW();
INSERT INTO public.emr_quality_rule_definitions (rule_code,version,section_code,name,source_text,product_text,standard_points,executor_kind,input_spec,evaluation_spec,action_policy,message_templates,status) SELECT rule_code,'2026-10-08-v1',section_code,name,source_text,product_text,standard_points,executor_kind,input_spec,evaluation_spec,action_policy,message_templates,'published' FROM public.emr_quality_rule_definitions WHERE version='2026-10-04-v2' ON CONFLICT (rule_code,version) DO NOTHING;
INSERT INTO public.emr_quality_rule_definitions (rule_code,version,section_code,name,source_text,product_text,standard_points,executor_kind,input_spec,evaluation_spec,action_policy,message_templates,status) VALUES
('TCM-DX-001','2026-10-08-v1','diagnosis','未填写中医诊断','中医门诊病历必须填写中医诊断','中医门诊病历必须填写中医诊断',10,'program','{"paths":["record.content.tcm_diagnosis","record.context.template_code"]}','{"mode":"tcm_diagnosis_presence_only"}','{"save":{"enabled":true,"blocking":false},"complete":{"enabled":true,"blocking":true},"archive":{"enabled":true,"blocking":true}}','{"save":"请填写中医诊断。","complete":"没有填写中医诊断，不能完成病历。","archive":"没有填写中医诊断，不能归档。"}','published'),
('TCM-DX-002','2026-10-08-v1','diagnosis','中医诊断依据不足','中医诊断应有证候和辨证依据','中医诊断应有证候和辨证依据',5,'program','{"paths":["record.content.tcm_diagnosis"]}','{"mode":"tcm_diagnosis_basis"}','{"save":{"enabled":true,"blocking":false},"complete":{"enabled":false,"blocking":false},"archive":{"enabled":false,"blocking":false}}','{"save":"请补充中医诊断的证候或辨证依据。"}','published'),
('TCM-DX-003','2026-10-08-v1','diagnosis','中医诊断填写了西医编码','中医诊断不使用ICD-10编码','中医诊断不使用ICD-10编码',5,'program','{"paths":["record.content.tcm_diagnosis"]}','{"mode":"tcm_diagnosis_no_icd"}','{"save":{"enabled":true,"blocking":false},"complete":{"enabled":true,"blocking":true},"archive":{"enabled":true,"blocking":true}}','{"save":"中医诊断不能填写ICD-10编码。","complete":"中医诊断不能填写ICD-10编码。","archive":"中医诊断不能填写ICD-10编码。"}','published') ON CONFLICT (rule_code,version) DO UPDATE SET status='published',updated_at=NOW();
UPDATE public.emr_quality_rule_set_versions SET status='retired',updated_at=NOW() WHERE version='2026-10-04-v2';
-- END SAFE TARGET FRAGMENT internal/store/migrations/20261009_001_finalize_dual_diagnosis_contract.sql --
