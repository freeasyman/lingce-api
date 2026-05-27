package store

// bootstrapCoreTableStatements returns CREATE TABLE IF NOT EXISTS statements
// for core tables that were originally created by the legacy Python/Alembic migrations.
// These must run before any ALTER TABLE or INSERT statements in compat_migration.go.
func bootstrapCoreTableStatements() []string {
	return []string{
		// --- Extensions ---
		`CREATE EXTENSION IF NOT EXISTS pg_trgm`,
		`CREATE EXTENSION IF NOT EXISTS vector`,

		// --- Trigger function ---
		`CREATE OR REPLACE FUNCTION normalize_smart_badge_file_url() RETURNS trigger LANGUAGE plpgsql AS $$
		BEGIN
			IF NEW.source = 'smart_badge' AND NEW.oss_key IS NOT NULL AND NEW.oss_key <> ''
			   AND (NEW.file_url LIKE 'http://lince.oss-cn-beijing.aliyuncs.com/recordings%2F%'
			     OR NEW.file_url LIKE 'http://lince.oss-cn-beijing.aliyuncs.com/%2F%') THEN
				NEW.file_url := 'https://lince.oss-cn-beijing.aliyuncs.com/' || NEW.oss_key;
			END IF;
			RETURN NEW;
		END; $$`,

		// --- tenants ---
		`CREATE TABLE IF NOT EXISTS tenants (
			id SERIAL PRIMARY KEY,
			name VARCHAR(255) NOT NULL,
			code VARCHAR(100) NOT NULL UNIQUE,
			contact_name VARCHAR(100),
			contact_phone VARCHAR(50),
			is_active INTEGER,
			created_at TIMESTAMP DEFAULT NOW(),
			updated_at TIMESTAMP,
			monthly_revenue_target NUMERIC(12,2),
			feature_group_id INTEGER,
			service_started_on DATE,
			service_expired_on DATE,
			grace_days INTEGER NOT NULL DEFAULT 0,
			deleted_at TIMESTAMP,
			valid_from TIMESTAMP,
			valid_to TIMESTAMP,
			contact_email TEXT,
			industry TEXT,
			is_active_bool BOOLEAN NOT NULL DEFAULT TRUE
		)`,

		// --- departments ---
		`CREATE TABLE IF NOT EXISTS departments (
			id SERIAL PRIMARY KEY,
			tenant_id INTEGER NOT NULL,
			institution_id INTEGER,
			name VARCHAR(255) NOT NULL,
			parent_id INTEGER,
			created_at TIMESTAMP DEFAULT NOW(),
			code TEXT,
			is_active BOOLEAN NOT NULL DEFAULT TRUE,
			deleted_at TIMESTAMP,
			updated_at TIMESTAMP NOT NULL DEFAULT NOW()
		)`,

		// --- employees ---
		`CREATE TABLE IF NOT EXISTS employees (
			id SERIAL PRIMARY KEY,
			tenant_id INTEGER NOT NULL,
			name VARCHAR(255) NOT NULL,
			phone VARCHAR(50) NOT NULL,
			password_hash VARCHAR(255) NOT NULL DEFAULT '',
			role VARCHAR(50),
			department_id INTEGER,
			is_active INTEGER,
			created_at TIMESTAMP DEFAULT NOW(),
			updated_at TIMESTAMP,
			manager_id INTEGER,
			session_version INTEGER NOT NULL DEFAULT 1,
			session_version_institution INTEGER NOT NULL DEFAULT 1,
			session_version_mobile INTEGER NOT NULL DEFAULT 1,
			username TEXT,
			full_name TEXT,
			email TEXT,
			deleted_at TIMESTAMP
		)`,
		`CREATE UNIQUE INDEX IF NOT EXISTS ix_employees_phone ON employees(phone)`,

		// --- recordings ---
		`CREATE TABLE IF NOT EXISTS recordings (
			id SERIAL PRIMARY KEY,
			tenant_id INTEGER NOT NULL,
			employee_id INTEGER NOT NULL,
			file_url VARCHAR(500) NOT NULL,
			file_name VARCHAR(255) NOT NULL,
			file_size INTEGER,
			duration INTEGER,
			mime_type VARCHAR(100),
			oss_key VARCHAR(500),
			storage_tier VARCHAR(50),
			patient_id INTEGER,
			task_id INTEGER,
			source VARCHAR(50),
			scene VARCHAR(100),
			scene_name VARCHAR(255),
			notes TEXT,
			status VARCHAR(50),
			transcription_status VARCHAR(50),
			transcription_text TEXT,
			transcription_segments JSON,
			transcription_model VARCHAR(100),
			transcription_duration_ms INTEGER,
			transcription_task_id VARCHAR(200),
			speaker_count INTEGER,
			speaker_mapping JSON,
			speaker_stats JSON,
			analysis_status VARCHAR(50),
			analysis_result JSON,
			analysis_model VARCHAR(100),
			analysis_cost DOUBLE PRECISION,
			recorded_at TIMESTAMP,
			created_at TIMESTAMP DEFAULT NOW(),
			updated_at TIMESTAMP,
			quality_score INTEGER,
			customer_id INTEGER,
			analysis_display JSONB,
			cleaned_transcription JSONB,
			cleaned_transcription_status VARCHAR(20) DEFAULT 'pending',
			confirmed_deal_status VARCHAR(20),
			converted_amount NUMERIC(10,2),
			consultation_record_confirmed BOOLEAN DEFAULT FALSE,
			not_closed_reason VARCHAR(30),
			ai_not_closed_reason VARCHAR(30),
			action_confirmed BOOLEAN NOT NULL DEFAULT FALSE,
			action_choice VARCHAR(20),
			action_confirmed_at TIMESTAMP,
			converted_item VARCHAR(100),
			order_no VARCHAR(100),
			device_no TEXT
		)`,
		`DO $$ BEGIN
			IF NOT EXISTS (SELECT 1 FROM pg_trigger WHERE tgname = 'trg_normalize_smart_badge_file_url') THEN
				CREATE TRIGGER trg_normalize_smart_badge_file_url
					BEFORE INSERT OR UPDATE OF file_url, oss_key, source ON recordings
					FOR EACH ROW EXECUTE FUNCTION normalize_smart_badge_file_url();
			END IF;
		END $$`,

		// --- inst_employee_roles ---
		`CREATE TABLE IF NOT EXISTS inst_employee_roles (
			employee_id INTEGER NOT NULL,
			role_code VARCHAR(50) NOT NULL,
			source VARCHAR(50),
			created_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
			tenant_id INTEGER NOT NULL,
			specialty_group VARCHAR(50),
			medical_specialty_code VARCHAR(50),
			PRIMARY KEY (tenant_id, employee_id, role_code)
		)`,
		`CREATE INDEX IF NOT EXISTS idx_inst_employee_roles_tenant_employee ON inst_employee_roles(tenant_id, employee_id)`,

		// --- inst_menus ---
		`CREATE TABLE IF NOT EXISTS inst_menus (
			id SERIAL PRIMARY KEY,
			code VARCHAR(50) NOT NULL UNIQUE,
			name VARCHAR(100) NOT NULL,
			path VARCHAR(200),
			icon VARCHAR(50),
			parent_id INTEGER,
			order_index INTEGER DEFAULT 0,
			permission_code VARCHAR(100),
			is_active BOOLEAN DEFAULT TRUE,
			created_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
			is_feature_assignable BOOLEAN NOT NULL DEFAULT FALSE,
			is_default_for_admin BOOLEAN NOT NULL DEFAULT FALSE,
			feature_code VARCHAR(128),
			feature_name VARCHAR(128)
		)`,

		// --- inst_role_menus ---
		`CREATE TABLE IF NOT EXISTS inst_role_menus (
			role_code VARCHAR(50) NOT NULL,
			menu_id INTEGER NOT NULL,
			created_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
			tenant_id INTEGER NOT NULL,
			PRIMARY KEY (tenant_id, role_code, menu_id)
		)`,
		`CREATE INDEX IF NOT EXISTS idx_inst_role_menus_tenant_menu ON inst_role_menus(tenant_id, menu_id)`,
		`CREATE INDEX IF NOT EXISTS idx_inst_role_menus_tenant_role ON inst_role_menus(tenant_id, role_code)`,

		// --- customers ---
		`CREATE TABLE IF NOT EXISTS customers (
			id SERIAL PRIMARY KEY,
			tenant_id INTEGER NOT NULL,
			name VARCHAR(100),
			phone VARCHAR(50),
			gender VARCHAR(10),
			lifecycle_stage VARCHAR(30) NOT NULL DEFAULT 'unknown',
			value_score INTEGER,
			assigned_to INTEGER,
			assigned_to_name VARCHAR(100),
			tags JSON,
			first_channel VARCHAR(30),
			first_contact_at TIMESTAMP,
			total_interactions INTEGER,
			last_interaction_at TIMESTAMP,
			patient_id INTEGER,
			lead_id INTEGER,
			notes TEXT,
			created_at TIMESTAMP DEFAULT NOW(),
			updated_at TIMESTAMP DEFAULT NOW(),
			age INTEGER,
			birth_date TIMESTAMP,
			momentum_status VARCHAR(20) DEFAULT 'STABLE',
			momentum_updated_at TIMESTAMP,
			momentum_change_reason TEXT,
			risk_score INTEGER DEFAULT 0,
			risk_updated_at TIMESTAMP,
			total_converted_amount DOUBLE PRECISION DEFAULT 0,
			deal_count INTEGER DEFAULT 0
		)`,

		// --- customer_tags ---
		`CREATE TABLE IF NOT EXISTS customer_tags (
			id SERIAL PRIMARY KEY,
			tenant_id INTEGER NOT NULL,
			name VARCHAR(50) NOT NULL,
			color VARCHAR(20) DEFAULT 'gray',
			description VARCHAR(200),
			category VARCHAR(30),
			is_system BOOLEAN DEFAULT FALSE,
			created_by INTEGER,
			created_at TIMESTAMP DEFAULT NOW(),
			updated_at TIMESTAMP DEFAULT NOW(),
			deleted_at TIMESTAMP
		)`,

		// --- customer_groups ---
		`CREATE TABLE IF NOT EXISTS customer_groups (
			id SERIAL PRIMARY KEY,
			tenant_id INTEGER NOT NULL,
			name VARCHAR(100) NOT NULL,
			description VARCHAR(500),
			type VARCHAR(20) NOT NULL DEFAULT 'manual',
			color VARCHAR(20) DEFAULT 'blue',
			rules JSONB,
			customer_count INTEGER DEFAULT 0,
			last_calculated_at TIMESTAMP,
			created_by INTEGER,
			created_at TIMESTAMP DEFAULT NOW(),
			updated_at TIMESTAMP DEFAULT NOW(),
			deleted_at TIMESTAMP,
			member_count INTEGER NOT NULL DEFAULT 0
		)`,

		// --- recording_analysis_results ---
		`CREATE TABLE IF NOT EXISTS recording_analysis_results (
			id SERIAL PRIMARY KEY,
			recording_id INTEGER NOT NULL,
			tenant_id INTEGER NOT NULL,
			prompt_code VARCHAR(100) NOT NULL,
			prompt_version VARCHAR(20),
			result_data JSON NOT NULL,
			confidence_score DOUBLE PRECISION,
			tokens_used INTEGER,
			cost DOUBLE PRECISION,
			execution_time_ms INTEGER,
			user_feedback VARCHAR(20),
			feedback_comment TEXT,
			created_at TIMESTAMP DEFAULT NOW(),
			updated_at TIMESTAMP
		)`,
		`CREATE INDEX IF NOT EXISTS ix_recording_analysis_results_recording_id ON recording_analysis_results(recording_id)`,
		`CREATE INDEX IF NOT EXISTS ix_recording_analysis_results_tenant_id ON recording_analysis_results(tenant_id)`,
		`CREATE INDEX IF NOT EXISTS ix_recording_analysis_results_prompt_code ON recording_analysis_results(prompt_code)`,

		// --- recording_analysis_prompts ---
		`CREATE TABLE IF NOT EXISTS recording_analysis_prompts (
			id SERIAL PRIMARY KEY,
			code VARCHAR(100) NOT NULL,
			name VARCHAR(255) NOT NULL,
			description TEXT,
			category VARCHAR(50) NOT NULL,
			system_prompt TEXT NOT NULL,
			user_prompt_template TEXT NOT NULL,
			output_schema JSON,
			version VARCHAR(20) NOT NULL,
			is_active BOOLEAN NOT NULL,
			usage_count INTEGER NOT NULL DEFAULT 0,
			avg_rating DOUBLE PRECISION,
			created_by INTEGER,
			updated_by INTEGER,
			created_at TIMESTAMP DEFAULT NOW(),
			updated_at TIMESTAMP
		)`,
		`CREATE UNIQUE INDEX IF NOT EXISTS ix_recording_analysis_prompts_code ON recording_analysis_prompts(code)`,

		// --- llm_model_configs ---
		`CREATE TABLE IF NOT EXISTS llm_model_configs (
			id SERIAL PRIMARY KEY,
			tenant_id INTEGER NOT NULL DEFAULT 0,
			model_code VARCHAR(50) NOT NULL DEFAULT '',
			model_name VARCHAR(100),
			provider VARCHAR(50),
			function_type VARCHAR(50) NOT NULL DEFAULT 'general',
			is_default BOOLEAN,
			api_key_encrypted VARCHAR(500),
			api_base_url VARCHAR(500),
			extra_params JSON,
			input_token_price DOUBLE PRECISION,
			output_token_price DOUBLE PRECISION,
			daily_limit INTEGER,
			monthly_limit INTEGER,
			is_active BOOLEAN,
			created_at TIMESTAMP DEFAULT NOW(),
			updated_at TIMESTAMP DEFAULT NOW()
		)`,
		`CREATE INDEX IF NOT EXISTS ix_llm_model_configs_tenant_id ON llm_model_configs(tenant_id)`,
		`CREATE INDEX IF NOT EXISTS ix_llm_model_configs_function_type ON llm_model_configs(function_type)`,

		// --- tenant_subscription_plans ---
		`CREATE TABLE IF NOT EXISTS tenant_subscription_plans (
			id SERIAL PRIMARY KEY,
			code VARCHAR(64) NOT NULL UNIQUE,
			name VARCHAR(128) NOT NULL,
			description TEXT,
			duration_days INTEGER NOT NULL DEFAULT 365,
			grace_days_default INTEGER NOT NULL DEFAULT 0,
			is_active INTEGER NOT NULL DEFAULT 1,
			created_at TIMESTAMP NOT NULL DEFAULT NOW(),
			updated_at TIMESTAMP
		)`,

		// --- knowledge_items ---
		`CREATE TABLE IF NOT EXISTS knowledge_items (
			id BIGSERIAL PRIMARY KEY,
			tenant_id BIGINT NOT NULL,
			scope VARCHAR(30) NOT NULL,
			category VARCHAR(30) NOT NULL,
			title VARCHAR(200) NOT NULL,
			content TEXT NOT NULL,
			tags TEXT[] DEFAULT '{}',
			product_name VARCHAR(200),
			source_type VARCHAR(20) NOT NULL,
			source_ref VARCHAR(200),
			status VARCHAR(10) NOT NULL DEFAULT 'draft',
			expires_at TIMESTAMPTZ,
			created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
			updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
		)`,
		`CREATE INDEX IF NOT EXISTS idx_ki_tenant_scope ON knowledge_items(tenant_id, scope, status)`,

		// --- content_topics ---
		`CREATE TABLE IF NOT EXISTS content_topics (
			id SERIAL PRIMARY KEY,
			tenant_id INTEGER NOT NULL,
			title VARCHAR(200) NOT NULL,
			angle VARCHAR(500),
			description TEXT,
			target_platform VARCHAR(50),
			content_type VARCHAR(50),
			source_hot_topic_ids JSON,
			source_keywords JSON,
			matched_departments JSON,
			matched_doctors JSON,
			matched_diseases JSON,
			status VARCHAR(20),
			priority INTEGER,
			created_at TIMESTAMP DEFAULT NOW(),
			selected_at TIMESTAMP,
			created_by INTEGER,
			source_type VARCHAR(30),
			source_recording_ids JSON,
			source_context JSON
		)`,

		// --- badge_devices ---
		`CREATE TABLE IF NOT EXISTS badge_devices (
			id SERIAL PRIMARY KEY,
			manufacturer_id INTEGER NOT NULL DEFAULT 0,
			app_id VARCHAR(100) NOT NULL DEFAULT '',
			device_no VARCHAR(100) NOT NULL DEFAULT '',
			device_uid VARCHAR(300) NOT NULL DEFAULT '',
			hardware_model VARCHAR(100),
			current_status VARCHAR(32) NOT NULL DEFAULT 'pending_acceptance',
			tenant_id INTEGER,
			employee_id INTEGER,
			acceptance_batch_no VARCHAR(100),
			last_online_time TIMESTAMP,
			remain_power INTEGER,
			ext_json JSONB DEFAULT '{}',
			created_at TIMESTAMP NOT NULL DEFAULT NOW(),
			updated_at TIMESTAMP NOT NULL DEFAULT NOW(),
			lifecycle_status VARCHAR(32) NOT NULL DEFAULT 'pending_acceptance',
			assignment_status VARCHAR(32) NOT NULL DEFAULT 'unassigned',
			last_inspection_result VARCHAR(16) NOT NULL DEFAULT 'unknown',
			inspection_result VARCHAR(16) NOT NULL DEFAULT 'unknown',
			last_inspection_scene VARCHAR(16),
			last_inspection_at TIMESTAMP
		)`,

		// --- badge_manufacturers ---
		`CREATE TABLE IF NOT EXISTS badge_manufacturers (
			id SERIAL PRIMARY KEY,
			code VARCHAR(64) NOT NULL,
			name VARCHAR(128) NOT NULL,
			api_base_url VARCHAR(255),
			app_id VARCHAR(100),
			app_secret VARCHAR(255),
			callback_base_url VARCHAR(255),
			auth_type VARCHAR(32) NOT NULL DEFAULT 'app_secret',
			status VARCHAR(20) NOT NULL DEFAULT 'enabled',
			capabilities JSONB DEFAULT '{}',
			created_at TIMESTAMP NOT NULL DEFAULT NOW(),
			updated_at TIMESTAMP NOT NULL DEFAULT NOW()
		)`,

		// --- mobile_push_devices ---
		`CREATE TABLE IF NOT EXISTS mobile_push_devices (
			id SERIAL PRIMARY KEY,
			user_id INTEGER NOT NULL,
			tenant_id INTEGER NOT NULL,
			platform VARCHAR(20) NOT NULL,
			device_id VARCHAR(120) NOT NULL,
			expo_push_token VARCHAR(255) NOT NULL UNIQUE,
			app_version VARCHAR(64),
			is_active BOOLEAN NOT NULL,
			last_seen_at TIMESTAMP NOT NULL DEFAULT NOW(),
			created_at TIMESTAMP NOT NULL DEFAULT NOW(),
			updated_at TIMESTAMP DEFAULT NOW(),
			UNIQUE(user_id, device_id)
		)`,
		`CREATE INDEX IF NOT EXISTS idx_push_device_tenant_user_active ON mobile_push_devices(tenant_id, user_id, is_active)`,

		// --- Additional tables used by data dump but not created by compat_migration ---

		// --- customer_group_members ---
		`CREATE TABLE IF NOT EXISTS customer_group_members (
			id BIGSERIAL PRIMARY KEY,
			group_id INTEGER NOT NULL,
			customer_id INTEGER NOT NULL,
			created_at TIMESTAMP,
			updated_at TIMESTAMP
		)`,

		// --- customer_interactions ---
		`CREATE TABLE IF NOT EXISTS customer_interactions (
			id BIGSERIAL PRIMARY KEY,
			tenant_id INTEGER NOT NULL,
			customer_id INTEGER NOT NULL,
			type VARCHAR(50),
			channel VARCHAR(50),
			content TEXT,
			employee_id INTEGER,
			recording_id INTEGER,
			interacted_at TIMESTAMP,
			created_at TIMESTAMP DEFAULT NOW(),
			updated_at TIMESTAMP
		)`,

		// --- customer_follow_ups ---
		`CREATE TABLE IF NOT EXISTS customer_follow_ups (
			id BIGSERIAL PRIMARY KEY,
			tenant_id BIGINT,
			customer_id INTEGER NOT NULL,
			type VARCHAR(50),
			content TEXT,
			status TEXT,
			scheduled_at TIMESTAMP,
			completed_at TIMESTAMP,
			employee_id BIGINT,
			created_at TIMESTAMP NOT NULL DEFAULT NOW(),
			updated_at TIMESTAMP NOT NULL DEFAULT NOW()
		)`,

		// --- customer_memberships ---
		`CREATE TABLE IF NOT EXISTS customer_memberships (
			id BIGSERIAL PRIMARY KEY,
			tenant_id BIGINT,
			customer_id INTEGER NOT NULL,
			level TEXT,
			points INTEGER NOT NULL DEFAULT 0,
			start_date TIMESTAMP,
			end_date TIMESTAMP,
			is_active BOOLEAN NOT NULL DEFAULT TRUE,
			created_at TIMESTAMP NOT NULL DEFAULT NOW(),
			updated_at TIMESTAMP NOT NULL DEFAULT NOW()
		)`,

		// --- institution_employee_roles ---
		`CREATE TABLE IF NOT EXISTS institution_employee_roles (
			id BIGSERIAL PRIMARY KEY,
			tenant_id BIGINT NOT NULL,
			employee_id BIGINT NOT NULL,
			role_id BIGINT NOT NULL,
			created_at TIMESTAMP NOT NULL DEFAULT NOW(),
			updated_at TIMESTAMP NOT NULL DEFAULT NOW()
		)`,

		// --- notifications ---
		`CREATE TABLE IF NOT EXISTS notifications (
			id BIGSERIAL PRIMARY KEY,
			tenant_id INTEGER,
			employee_id INTEGER,
			type VARCHAR(50),
			title VARCHAR(255),
			content TEXT,
			is_read BOOLEAN NOT NULL DEFAULT FALSE,
			metadata JSONB DEFAULT '{}',
			created_at TIMESTAMP NOT NULL DEFAULT NOW(),
			updated_at TIMESTAMP DEFAULT NOW()
		)`,

		// --- recording_content_seeds ---
		`CREATE TABLE IF NOT EXISTS recording_content_seeds (
			id BIGSERIAL PRIMARY KEY,
			tenant_id INTEGER NOT NULL,
			recording_id INTEGER NOT NULL,
			concern_cluster_id INTEGER,
			used_content_id INTEGER,
			seed_type VARCHAR(50),
			title VARCHAR(255),
			content TEXT,
			status VARCHAR(30) DEFAULT 'pending',
			created_at TIMESTAMP DEFAULT NOW(),
			updated_at TIMESTAMP DEFAULT NOW()
		)`,

		// --- concern_clusters ---
		`CREATE TABLE IF NOT EXISTS concern_clusters (
			id SERIAL PRIMARY KEY,
			tenant_id INTEGER NOT NULL,
			name VARCHAR(200),
			keywords JSONB DEFAULT '[]',
			recording_count INTEGER DEFAULT 0,
			status VARCHAR(20) DEFAULT 'active',
			created_at TIMESTAMP DEFAULT NOW(),
			updated_at TIMESTAMP DEFAULT NOW()
		)`,

		// --- recording_route_results ---
		`CREATE TABLE IF NOT EXISTS recording_route_results (
			id BIGSERIAL PRIMARY KEY,
			recording_id INTEGER NOT NULL,
			tenant_id INTEGER NOT NULL,
			route_type VARCHAR(50),
			result_data JSONB,
			created_at TIMESTAMP DEFAULT NOW(),
			updated_at TIMESTAMP DEFAULT NOW()
		)`,

		// --- recording_segue_scores ---
		`CREATE TABLE IF NOT EXISTS recording_segue_scores (
			id BIGSERIAL PRIMARY KEY,
			recording_id INTEGER NOT NULL,
			tenant_id INTEGER NOT NULL,
			score DOUBLE PRECISION,
			details JSONB,
			created_at TIMESTAMP DEFAULT NOW(),
			updated_at TIMESTAMP DEFAULT NOW()
		)`,

		// --- recording_emr_drafts ---
		`CREATE TABLE IF NOT EXISTS recording_emr_drafts (
			id BIGSERIAL PRIMARY KEY,
			tenant_id INTEGER NOT NULL,
			recording_id INTEGER NOT NULL,
			patient_id INTEGER,
			confirmed_by INTEGER,
			draft_data JSONB,
			status VARCHAR(30) DEFAULT 'draft',
			created_at TIMESTAMP DEFAULT NOW(),
			updated_at TIMESTAMP DEFAULT NOW()
		)`,

		// --- recording_tasks ---
		`CREATE TABLE IF NOT EXISTS recording_tasks (
			id BIGSERIAL PRIMARY KEY,
			tenant_id INTEGER NOT NULL,
			recording_id INTEGER,
			employee_id INTEGER,
			type VARCHAR(50),
			title VARCHAR(255),
			status VARCHAR(30) DEFAULT 'pending',
			priority INTEGER DEFAULT 0,
			due_at TIMESTAMP,
			completed_at TIMESTAMP,
			created_at TIMESTAMP DEFAULT NOW(),
			updated_at TIMESTAMP DEFAULT NOW()
		)`,

		// --- operation_logs ---
		`CREATE TABLE IF NOT EXISTS operation_logs (
			id BIGSERIAL PRIMARY KEY,
			tenant_id INTEGER,
			operator_id INTEGER,
			operator_type VARCHAR(30),
			action VARCHAR(100),
			resource_type VARCHAR(100),
			resource_id VARCHAR(100),
			detail JSONB,
			ip_address VARCHAR(50),
			created_at TIMESTAMP NOT NULL DEFAULT NOW()
		)`,

		// --- smart_badge_audio_events ---
		`CREATE TABLE IF NOT EXISTS smart_badge_audio_events (
			id BIGSERIAL PRIMARY KEY,
			recording_id INTEGER,
			device_no VARCHAR(100),
			event_type VARCHAR(50),
			event_data JSONB,
			created_at TIMESTAMP NOT NULL DEFAULT NOW()
		)`,

		// --- smart_badge_device_mappings ---
		`CREATE TABLE IF NOT EXISTS smart_badge_device_mappings (
			id BIGSERIAL PRIMARY KEY,
			tenant_id INTEGER NOT NULL,
			employee_id INTEGER NOT NULL,
			device_no VARCHAR(100) NOT NULL,
			app_id VARCHAR(100),
			status VARCHAR(30) DEFAULT 'active',
			created_at TIMESTAMP DEFAULT NOW(),
			updated_at TIMESTAMP DEFAULT NOW()
		)`,

		// --- smart_badge_binding_logs ---
		`CREATE TABLE IF NOT EXISTS smart_badge_binding_logs (
			id BIGSERIAL PRIMARY KEY,
			tenant_id INTEGER NOT NULL,
			device_no VARCHAR(100),
			old_employee_id INTEGER,
			new_employee_id INTEGER,
			action VARCHAR(50),
			created_at TIMESTAMP NOT NULL DEFAULT NOW()
		)`,

		// --- badge_device_assignments ---
		`CREATE TABLE IF NOT EXISTS badge_device_assignments (
			id BIGSERIAL PRIMARY KEY,
			device_id INTEGER NOT NULL,
			tenant_id INTEGER NOT NULL,
			employee_id INTEGER NOT NULL,
			status VARCHAR(30) DEFAULT 'active',
			assigned_at TIMESTAMP DEFAULT NOW(),
			unassigned_at TIMESTAMP,
			created_at TIMESTAMP DEFAULT NOW(),
			updated_at TIMESTAMP DEFAULT NOW()
		)`,

		// --- badge_vendor_sync_batches ---
		`CREATE TABLE IF NOT EXISTS badge_vendor_sync_batches (
			id BIGSERIAL PRIMARY KEY,
			manufacturer_id INTEGER NOT NULL,
			sync_type VARCHAR(50),
			status VARCHAR(30) DEFAULT 'pending',
			total_count INTEGER DEFAULT 0,
			success_count INTEGER DEFAULT 0,
			failed_count INTEGER DEFAULT 0,
			started_at TIMESTAMP,
			finished_at TIMESTAMP,
			created_at TIMESTAMP NOT NULL DEFAULT NOW(),
			updated_at TIMESTAMP DEFAULT NOW()
		)`,

		// --- badge_vendor_sync_items ---
		`CREATE TABLE IF NOT EXISTS badge_vendor_sync_items (
			id BIGSERIAL PRIMARY KEY,
			batch_id INTEGER NOT NULL,
			manufacturer_id INTEGER NOT NULL,
			internal_device_id INTEGER,
			vendor_device_id VARCHAR(200),
			sync_action VARCHAR(50),
			status VARCHAR(30) DEFAULT 'pending',
			error_message TEXT,
			created_at TIMESTAMP NOT NULL DEFAULT NOW(),
			updated_at TIMESTAMP DEFAULT NOW()
		)`,

		// --- badge_recording_control_logs ---
		`CREATE TABLE IF NOT EXISTS badge_recording_control_logs (
			id BIGSERIAL PRIMARY KEY,
			tenant_id INTEGER,
			employee_id INTEGER,
			device_no VARCHAR(100),
			action VARCHAR(50),
			status VARCHAR(30),
			error_message TEXT,
			created_at TIMESTAMP NOT NULL DEFAULT NOW()
		)`,

		// --- smart_badge_recording_control_logs ---
		`CREATE TABLE IF NOT EXISTS smart_badge_recording_control_logs (
			id BIGSERIAL PRIMARY KEY,
			tenant_id INTEGER NOT NULL,
			employee_id INTEGER NOT NULL,
			device_no VARCHAR(100),
			action VARCHAR(50),
			status VARCHAR(30),
			request_payload JSONB,
			response_payload JSONB,
			error_message TEXT,
			created_at TIMESTAMP NOT NULL DEFAULT NOW()
		)`,

		// --- frontdesk_shift_analyses ---
		`CREATE TABLE IF NOT EXISTS frontdesk_shift_analyses (
			id BIGSERIAL PRIMARY KEY,
			tenant_id INTEGER NOT NULL,
			recording_id INTEGER,
			employee_id INTEGER,
			shift_date DATE,
			analysis_data JSONB,
			status VARCHAR(30) DEFAULT 'pending',
			created_at TIMESTAMP DEFAULT NOW(),
			updated_at TIMESTAMP DEFAULT NOW()
		)`,

		// --- scheduled_reports ---
		`CREATE TABLE IF NOT EXISTS scheduled_reports (
			id BIGSERIAL PRIMARY KEY,
			tenant_id INTEGER NOT NULL,
			name VARCHAR(200),
			type VARCHAR(50),
			config JSONB,
			schedule VARCHAR(100),
			is_active BOOLEAN DEFAULT TRUE,
			created_at TIMESTAMP DEFAULT NOW(),
			updated_at TIMESTAMP DEFAULT NOW()
		)`,

		// --- scheduled_report_runs ---
		`CREATE TABLE IF NOT EXISTS scheduled_report_runs (
			id BIGSERIAL PRIMARY KEY,
			report_id BIGINT NOT NULL,
			status VARCHAR(30) DEFAULT 'pending',
			result_data JSONB,
			started_at TIMESTAMP,
			finished_at TIMESTAMP,
			error_message TEXT,
			created_at TIMESTAMP NOT NULL DEFAULT NOW()
		)`,

		// --- llm_call_records ---
		`CREATE TABLE IF NOT EXISTS llm_call_records (
			id BIGSERIAL PRIMARY KEY,
			tenant_id INTEGER,
			function_type VARCHAR(50),
			model_code VARCHAR(100),
			prompt_code VARCHAR(100),
			input_tokens INTEGER,
			output_tokens INTEGER,
			total_tokens INTEGER,
			cost DOUBLE PRECISION,
			execution_time_ms INTEGER,
			status VARCHAR(30),
			error_message TEXT,
			recording_id INTEGER,
			created_at TIMESTAMP NOT NULL DEFAULT NOW()
		)`,
	}
}
