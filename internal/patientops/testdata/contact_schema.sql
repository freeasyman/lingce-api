-- Minimal isolated schema for the contact integration tests. No Patient table.
CREATE TABLE customers (
 id bigserial PRIMARY KEY, tenant_id bigint NOT NULL, name text NOT NULL DEFAULT '',
 phone text, email text, gender text, age int, birth_date date, tags jsonb DEFAULT '[]',
 source text, status text NOT NULL DEFAULT 'lead', momentum int NOT NULL DEFAULT 50,
 assigned_to bigint, assigned_at timestamptz, converted_at timestamptz,
 last_contacted_at timestamptz, next_follow_up_at timestamptz, lifecycle_stage text,
 value_score int, notes text, extra_data jsonb DEFAULT '{}', created_by bigint DEFAULT 1,
 created_at timestamptz DEFAULT now(), updated_at timestamptz DEFAULT now(), deleted_at timestamptz
);
CREATE TABLE customer_identities (
 id bigserial PRIMARY KEY, customer_id bigint REFERENCES customers(id),
 channel_type text NOT NULL, external_id text, external_name text, external_avatar text,
 channel text, channel_id text, nickname text, avatar text, extra_data jsonb DEFAULT '{}',
 created_at timestamptz DEFAULT now(), updated_at timestamptz DEFAULT now()
);
CREATE TABLE patient_conversation_sources (
 id bigserial PRIMARY KEY, tenant_id bigint NOT NULL, source_type text DEFAULT 'screenshot',
 customer_id bigint REFERENCES customers(id), match_status text NOT NULL DEFAULT 'pending',
 recognition_payload jsonb NOT NULL DEFAULT '{}', captured_at timestamptz DEFAULT now(),
 content_hash text, analysis_status text DEFAULT 'succeeded', original_name text,
 media_data text, analysis_error text DEFAULT '', analysis_completed_at timestamptz, analysis_started_at timestamptz, created_by bigint DEFAULT 1, updated_at timestamptz DEFAULT now(),
 UNIQUE(tenant_id,content_hash)
);
CREATE TABLE recordings (
 id bigserial PRIMARY KEY, customer_id bigint, confirmed_deal_status text,
 analysis_result jsonb, recorded_at timestamptz, created_at timestamptz DEFAULT now(),
 converted_amount numeric, converted_item text
);
CREATE TABLE customer_interactions (
 id bigserial PRIMARY KEY, customer_id bigint, type text,
 direction text, content text, duration int, recording_id bigint, employee_id bigint,
 interacted_at timestamptz DEFAULT now(), created_at timestamptz DEFAULT now(), updated_at timestamptz DEFAULT now()
);
CREATE TABLE encounters (
 id bigserial PRIMARY KEY, tenant_id bigint, patient_id bigint, started_at timestamptz,
 visit_type text, provider_name text, patient_candidate_json jsonb
);
CREATE TABLE external_data_source_identities (
 id bigserial PRIMARY KEY, tenant_id bigint, customer_id bigint, source_system text,
 source_patient_id text, match_method text, updated_at timestamptz
);
CREATE TABLE external_data_deliveries (
 id bigserial PRIMARY KEY, tenant_id bigint, source_system text, object_type text,
 payload jsonb, source_updated_at timestamptz, created_at timestamptz
);
CREATE TABLE employees (id bigserial PRIMARY KEY, tenant_id bigint, name text, full_name text, phone text, deleted_at timestamptz);
CREATE TABLE products (id bigserial PRIMARY KEY, name text);
CREATE TABLE patient_ops_plans (id bigserial PRIMARY KEY, tenant_id bigint, status text, deleted_at timestamptz);
CREATE TABLE patient_ops_plan_members (
 id bigserial PRIMARY KEY, tenant_id bigint, plan_id bigint, customer_id bigint,
 recommended_product_id bigint, match_score int, reason text DEFAULT '', reason_source text DEFAULT 'fact',
 risk_medical text DEFAULT '', risk_commercial text DEFAULT '', task_id bigint,
 status text DEFAULT 'selected', created_at timestamptz DEFAULT now()
);
CREATE TABLE patient_operation_tasks (
 id bigserial PRIMARY KEY, tenant_id bigint, customer_id bigint, employee_id bigint,
 source_id bigint, task_type text DEFAULT '咨询推进', goal text DEFAULT '', action text DEFAULT '',
 content text DEFAULT '', evidence text DEFAULT '', due_at timestamptz, status text DEFAULT '待处理',
 created_at timestamptz DEFAULT now(), result text DEFAULT '', result_note text DEFAULT '',
 completed_at timestamptz, parent_task_id bigint, plan_id bigint, plan_member_id bigint,
 task_subtype text DEFAULT '', product_id bigint, priority int DEFAULT 0, match_score int,
 risk_medical text DEFAULT '', risk_commercial text DEFAULT '', next_action text DEFAULT '',
 next_due_at timestamptz, expected_result text DEFAULT '', result_amount numeric,
 result_product_id bigint, amount_source text DEFAULT ''
);
