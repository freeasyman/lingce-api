package store

import (
	"context"
	"fmt"
	"log/slog"

	"github.com/jackc/pgx/v5/pgxpool"
)

// ApplyCompatMigrations applies minimal schema compatibility fixes for 18080 API integration.
// All statements are idempotent.
func ApplyCompatMigrations(ctx context.Context, pool *pgxpool.Pool) error {
	stmts := []string{
		// Customer module soft-delete compatibility
		`ALTER TABLE IF EXISTS customers ADD COLUMN IF NOT EXISTS deleted_at TIMESTAMP`,
		`ALTER TABLE IF EXISTS customers ADD COLUMN IF NOT EXISTS email TEXT`,
		`ALTER TABLE IF EXISTS customers ADD COLUMN IF NOT EXISTS source TEXT`,
		`ALTER TABLE IF EXISTS customers ADD COLUMN IF NOT EXISTS status TEXT NOT NULL DEFAULT 'lead'`,
		`ALTER TABLE IF EXISTS customers ADD COLUMN IF NOT EXISTS momentum INTEGER NOT NULL DEFAULT 0`,
		`ALTER TABLE IF EXISTS customers ADD COLUMN IF NOT EXISTS assigned_to BIGINT`,
		`ALTER TABLE IF EXISTS customers ADD COLUMN IF NOT EXISTS assigned_at TIMESTAMP`,
		`ALTER TABLE IF EXISTS customers ADD COLUMN IF NOT EXISTS converted_at TIMESTAMP`,
		`ALTER TABLE IF EXISTS customers ADD COLUMN IF NOT EXISTS last_contacted_at TIMESTAMP`,
		`ALTER TABLE IF EXISTS customers ADD COLUMN IF NOT EXISTS next_follow_up_at TIMESTAMP`,
		`ALTER TABLE IF EXISTS customers ADD COLUMN IF NOT EXISTS notes TEXT`,
		`ALTER TABLE IF EXISTS customers ADD COLUMN IF NOT EXISTS extra_data JSONB NOT NULL DEFAULT '{}'::jsonb`,
		`ALTER TABLE IF EXISTS customers ADD COLUMN IF NOT EXISTS created_by BIGINT NOT NULL DEFAULT 0`,
		`ALTER TABLE IF EXISTS customers ADD COLUMN IF NOT EXISTS updated_at TIMESTAMP NOT NULL DEFAULT NOW()`,
		`ALTER TABLE IF EXISTS customer_tags ADD COLUMN IF NOT EXISTS deleted_at TIMESTAMP`,
		`ALTER TABLE IF EXISTS customer_groups ADD COLUMN IF NOT EXISTS deleted_at TIMESTAMP`,
		`ALTER TABLE IF EXISTS customer_groups ADD COLUMN IF NOT EXISTS member_count INTEGER NOT NULL DEFAULT 0`,
		`ALTER TABLE IF EXISTS customer_interactions ADD COLUMN IF NOT EXISTS interacted_at TIMESTAMP`,
		`ALTER TABLE IF EXISTS customer_interactions ADD COLUMN IF NOT EXISTS updated_at TIMESTAMP`,
		`ALTER TABLE IF EXISTS customer_follow_ups ADD COLUMN IF NOT EXISTS status TEXT`,
		`ALTER TABLE IF EXISTS customer_group_members ADD COLUMN IF NOT EXISTS created_at TIMESTAMP`,
		`ALTER TABLE IF EXISTS customer_group_members ADD COLUMN IF NOT EXISTS updated_at TIMESTAMP`,

		// Support module compatibility
		`CREATE TABLE IF NOT EXISTS device_tokens (
			id BIGSERIAL PRIMARY KEY,
			user_id BIGINT NOT NULL,
			user_type TEXT NOT NULL DEFAULT 'mobile',
			token TEXT NOT NULL,
			platform TEXT NOT NULL,
			device_model TEXT,
			app_version TEXT,
			is_active BOOLEAN NOT NULL DEFAULT TRUE,
			last_used_at TIMESTAMP NOT NULL DEFAULT NOW(),
			created_at TIMESTAMP NOT NULL DEFAULT NOW(),
			updated_at TIMESTAMP NOT NULL DEFAULT NOW()
		)`,
		`CREATE UNIQUE INDEX IF NOT EXISTS uk_device_tokens_user_type_token ON device_tokens(user_id, user_type, token)`,
		`CREATE INDEX IF NOT EXISTS idx_device_tokens_user_active ON device_tokens(user_id, user_type, is_active)`,
		`CREATE TABLE IF NOT EXISTS notifications (
			id BIGSERIAL PRIMARY KEY,
			tenant_id BIGINT,
			user_id BIGINT NOT NULL,
			user_type TEXT NOT NULL DEFAULT 'mobile',
			title TEXT NOT NULL DEFAULT '',
			content TEXT NOT NULL DEFAULT '',
			message_type TEXT,
			type TEXT NOT NULL DEFAULT 'system',
			related_id BIGINT,
			related_type TEXT,
			is_read BOOLEAN NOT NULL DEFAULT FALSE,
			read_at TIMESTAMP,
			extra_data JSONB NOT NULL DEFAULT '{}'::jsonb,
			created_at TIMESTAMP NOT NULL DEFAULT NOW(),
			updated_at TIMESTAMP NOT NULL DEFAULT NOW()
		)`,
		`ALTER TABLE IF EXISTS notifications ADD COLUMN IF NOT EXISTS user_type TEXT NOT NULL DEFAULT 'mobile'`,
		`ALTER TABLE IF EXISTS notifications ADD COLUMN IF NOT EXISTS type TEXT NOT NULL DEFAULT ''`,
		`ALTER TABLE IF EXISTS notifications ADD COLUMN IF NOT EXISTS extra_data JSONB NOT NULL DEFAULT '{}'::jsonb`,
		`UPDATE notifications
		SET type = COALESCE(NULLIF(type, ''), NULLIF(message_type, ''), 'system')
		WHERE type IS NULL OR trim(type) = ''`,
		`UPDATE notifications
		SET user_type = 'mobile'
		WHERE user_type IS NULL OR trim(user_type) = ''`,
		`ALTER TABLE notifications ALTER COLUMN type SET DEFAULT 'system'`,
		`CREATE INDEX IF NOT EXISTS idx_notifications_user_type_read ON notifications(user_id, user_type, is_read)`,
		`CREATE INDEX IF NOT EXISTS idx_notifications_user_type_created_at ON notifications(user_id, user_type, created_at DESC)`,

		// Recording business scope compatibility
		`ALTER TABLE IF EXISTS recordings ADD COLUMN IF NOT EXISTS business_scope TEXT NOT NULL DEFAULT 'unknown'`,
		`UPDATE recordings SET business_scope = 'unknown' WHERE business_scope IS NULL OR trim(business_scope) = ''`,
		`CREATE INDEX IF NOT EXISTS idx_recordings_tenant_business_scope_created_at ON recordings(tenant_id, business_scope, created_at DESC)`,
		`DO $$
		BEGIN
			IF NOT EXISTS (
				SELECT 1 FROM pg_constraint WHERE conname = 'chk_recordings_business_scope_enum'
			) THEN
				ALTER TABLE recordings
				ADD CONSTRAINT chk_recordings_business_scope_enum
				CHECK (business_scope IN ('doctor','consultant','frontdesk','therapist','nurse','lingce_sales','unknown'));
			END IF;
		END $$`,
		`UPDATE recordings r
		SET business_scope = CASE
			WHEN lower(COALESCE(NULLIF(r.scene, ''), '')) IN ('frontdesk','reception','receptionist','customer_service','service') THEN 'frontdesk'
			WHEN lower(COALESCE(NULLIF(r.scene, ''), '')) IN ('doctor','diagnosis','treatment','medical') THEN 'doctor'
			WHEN lower(COALESCE(NULLIF(r.scene, ''), '')) IN ('consultant','consultation','sales') THEN 'consultant'
			WHEN lower(COALESCE(NULLIF(r.scene, ''), '')) IN ('therapist') THEN 'therapist'
			WHEN lower(COALESCE(NULLIF(r.scene, ''), '')) IN ('nurse') THEN 'nurse'
			WHEN lower(COALESCE(NULLIF(r.scene, ''), '')) IN ('lingce_sales') THEN 'lingce_sales'
			ELSE 'unknown'
		END
		WHERE r.business_scope = 'unknown' OR r.business_scope IS NULL`,

		// Organization module compatibility
		`ALTER TABLE IF EXISTS tenants ADD COLUMN IF NOT EXISTS deleted_at TIMESTAMP`,
		`ALTER TABLE IF EXISTS tenants ADD COLUMN IF NOT EXISTS is_active BOOLEAN NOT NULL DEFAULT TRUE`,
		`ALTER TABLE IF EXISTS tenants ADD COLUMN IF NOT EXISTS valid_from TIMESTAMP`,
		`ALTER TABLE IF EXISTS tenants ADD COLUMN IF NOT EXISTS valid_to TIMESTAMP`,
		`ALTER TABLE IF EXISTS tenants ADD COLUMN IF NOT EXISTS contact_name TEXT`,
		`ALTER TABLE IF EXISTS tenants ADD COLUMN IF NOT EXISTS contact_phone TEXT`,
		`ALTER TABLE IF EXISTS tenants ADD COLUMN IF NOT EXISTS contact_email TEXT`,
		`ALTER TABLE IF EXISTS tenants ADD COLUMN IF NOT EXISTS industry TEXT`,
		`ALTER TABLE IF EXISTS tenants ADD COLUMN IF NOT EXISTS account_mode TEXT NOT NULL DEFAULT 'formal'`,
		`UPDATE tenants SET account_mode = 'formal' WHERE account_mode IS NULL OR trim(account_mode) = ''`,
		`ALTER TABLE IF EXISTS tenants ADD COLUMN IF NOT EXISTS updated_at TIMESTAMP NOT NULL DEFAULT NOW()`,
		`ALTER TABLE IF EXISTS departments ADD COLUMN IF NOT EXISTS code TEXT`,
		`ALTER TABLE IF EXISTS departments ADD COLUMN IF NOT EXISTS parent_id BIGINT`,
		`ALTER TABLE IF EXISTS departments ADD COLUMN IF NOT EXISTS is_active BOOLEAN NOT NULL DEFAULT TRUE`,
		`ALTER TABLE IF EXISTS departments ADD COLUMN IF NOT EXISTS deleted_at TIMESTAMP`,
		`ALTER TABLE IF EXISTS departments ADD COLUMN IF NOT EXISTS updated_at TIMESTAMP NOT NULL DEFAULT NOW()`,
		`ALTER TABLE IF EXISTS employees ADD COLUMN IF NOT EXISTS username TEXT`,
		`ALTER TABLE IF EXISTS employees ADD COLUMN IF NOT EXISTS password_hash TEXT`,
		`ALTER TABLE IF EXISTS employees ADD COLUMN IF NOT EXISTS full_name TEXT`,
		`ALTER TABLE IF EXISTS employees ADD COLUMN IF NOT EXISTS phone TEXT`,
		`ALTER TABLE IF EXISTS employees ADD COLUMN IF NOT EXISTS email TEXT`,
		`ALTER TABLE IF EXISTS employees ADD COLUMN IF NOT EXISTS department_id BIGINT`,
		`ALTER TABLE IF EXISTS employees ADD COLUMN IF NOT EXISTS session_version INTEGER NOT NULL DEFAULT 0`,
		`ALTER TABLE IF EXISTS employees ADD COLUMN IF NOT EXISTS is_active BOOLEAN NOT NULL DEFAULT TRUE`,
		`ALTER TABLE IF EXISTS employees ADD COLUMN IF NOT EXISTS deleted_at TIMESTAMP`,
		`ALTER TABLE IF EXISTS employees ADD COLUMN IF NOT EXISTS updated_at TIMESTAMP NOT NULL DEFAULT NOW()`,
		`CREATE TABLE IF NOT EXISTS tenant_trial_agreements (
			id BIGSERIAL PRIMARY KEY,
			tenant_id BIGINT NOT NULL,
			agreement_type TEXT NOT NULL,
			agreement_version TEXT NOT NULL,
			accepted BOOLEAN NOT NULL DEFAULT FALSE,
			accepted_at TIMESTAMP,
			accepted_by BIGINT,
			created_at TIMESTAMP NOT NULL DEFAULT NOW(),
			updated_at TIMESTAMP NOT NULL DEFAULT NOW(),
			UNIQUE(tenant_id, agreement_type, agreement_version)
		)`,
		`CREATE INDEX IF NOT EXISTS idx_tenant_trial_agreements_tenant ON tenant_trial_agreements(tenant_id, updated_at DESC)`,
		`CREATE TABLE IF NOT EXISTS institution_roles (
			id BIGSERIAL PRIMARY KEY,
			tenant_id BIGINT NOT NULL,
			name TEXT NOT NULL,
			code TEXT NOT NULL,
			description TEXT,
			is_active BOOLEAN NOT NULL DEFAULT TRUE,
			created_at TIMESTAMP NOT NULL DEFAULT NOW(),
			updated_at TIMESTAMP NOT NULL DEFAULT NOW(),
			deleted_at TIMESTAMP
		)`,
		`CREATE UNIQUE INDEX IF NOT EXISTS uk_institution_roles_tenant_code ON institution_roles(tenant_id, code) WHERE deleted_at IS NULL`,
		`CREATE INDEX IF NOT EXISTS idx_institution_roles_tenant_id ON institution_roles(tenant_id)`,
		`CREATE TABLE IF NOT EXISTS institution_department_roles (
			id BIGSERIAL PRIMARY KEY,
			department_id BIGINT NOT NULL,
			role_id BIGINT NOT NULL,
			is_default BOOLEAN NOT NULL DEFAULT TRUE,
			created_at TIMESTAMP NOT NULL DEFAULT NOW()
		)`,
		`CREATE UNIQUE INDEX IF NOT EXISTS uk_institution_department_roles_department_id ON institution_department_roles(department_id)`,
		`CREATE INDEX IF NOT EXISTS idx_institution_department_roles_role_id ON institution_department_roles(role_id)`,

		// Content module compatibility (deleted_at + missing content_items table)
		`ALTER TABLE IF EXISTS content_topics ADD COLUMN IF NOT EXISTS deleted_at TIMESTAMP`,
		`ALTER TABLE IF EXISTS content_topics ADD COLUMN IF NOT EXISTS category TEXT`,
		`ALTER TABLE IF EXISTS content_topics ADD COLUMN IF NOT EXISTS tags JSONB NOT NULL DEFAULT '[]'::jsonb`,
		`ALTER TABLE IF EXISTS content_topics ADD COLUMN IF NOT EXISTS source TEXT NOT NULL DEFAULT 'manual'`,
		`ALTER TABLE IF EXISTS content_topics ADD COLUMN IF NOT EXISTS status TEXT NOT NULL DEFAULT 'draft'`,
		`ALTER TABLE IF EXISTS content_topics ADD COLUMN IF NOT EXISTS priority INTEGER`,
		`ALTER TABLE IF EXISTS content_topics ADD COLUMN IF NOT EXISTS target_date TIMESTAMP`,
		`ALTER TABLE IF EXISTS content_topics ADD COLUMN IF NOT EXISTS extra_data JSONB NOT NULL DEFAULT '{}'::jsonb`,
		`ALTER TABLE IF EXISTS content_topics ADD COLUMN IF NOT EXISTS created_by BIGINT NOT NULL DEFAULT 0`,
		`ALTER TABLE IF EXISTS content_topics ADD COLUMN IF NOT EXISTS updated_at TIMESTAMP NOT NULL DEFAULT NOW()`,
		`ALTER TABLE IF EXISTS content_publish_tasks ADD COLUMN IF NOT EXISTS deleted_at TIMESTAMP`,
		`CREATE TABLE IF NOT EXISTS content_items (
			id BIGSERIAL PRIMARY KEY,
			tenant_id BIGINT NOT NULL,
			topic_id BIGINT,
			content_type TEXT,
			platform TEXT,
			title TEXT NOT NULL DEFAULT '',
			content TEXT NOT NULL DEFAULT '',
			summary TEXT,
			category TEXT,
			tags JSONB DEFAULT '[]'::jsonb,
			status TEXT NOT NULL DEFAULT 'draft',
			published_at TIMESTAMP,
			unpublished_at TIMESTAMP,
			view_count INTEGER NOT NULL DEFAULT 0,
			like_count INTEGER NOT NULL DEFAULT 0,
			share_count INTEGER NOT NULL DEFAULT 0,
			images JSONB DEFAULT '[]'::jsonb,
			extra_data JSONB DEFAULT '{}'::jsonb,
			created_by BIGINT,
			created_at TIMESTAMP NOT NULL DEFAULT NOW(),
			updated_at TIMESTAMP NOT NULL DEFAULT NOW(),
			deleted_at TIMESTAMP
		)`,
		`ALTER TABLE IF EXISTS content_items ADD COLUMN IF NOT EXISTS content_type TEXT`,
		`ALTER TABLE IF EXISTS content_items ADD COLUMN IF NOT EXISTS platform TEXT`,
		`UPDATE content_items
		   SET content_type = COALESCE(
		         NULLIF(content_type, ''),
		         NULLIF(extra_data->>'content_type', '')
		       )
		 WHERE COALESCE(content_type, '') = ''
		   AND extra_data IS NOT NULL`,
		`UPDATE content_items
		   SET platform = COALESCE(
		         NULLIF(platform, ''),
		         NULLIF(extra_data->>'platform', ''),
		         NULLIF(extra_data->>'target_platform', '')
		       )
		 WHERE COALESCE(platform, '') = ''
		   AND extra_data IS NOT NULL`,
		// 试用客户指标重算按 tenant_id 聚合 content_items / recording_tasks，补索引避免全表扫描。
		// content_items 由本迁移建表，直接建索引；recording_tasks 由其他模块建表，
		// 用 to_regclass 判断表存在才建索引，避免迁移在该表尚未创建时中断。
		`CREATE INDEX IF NOT EXISTS idx_content_items_tenant ON content_items(tenant_id)`,
		`DO $$
		BEGIN
			IF to_regclass('public.recording_tasks') IS NOT NULL THEN
				CREATE INDEX IF NOT EXISTS idx_recording_tasks_tenant_recording ON recording_tasks(tenant_id, recording_id);
			END IF;
		END $$;`,

		// Badge module compatibility (missing columns/table)
		`ALTER TABLE IF EXISTS badge_devices ADD COLUMN IF NOT EXISTS manufacturer_code TEXT NOT NULL DEFAULT ''`,
		`ALTER TABLE IF EXISTS badge_devices ADD COLUMN IF NOT EXISTS status TEXT NOT NULL DEFAULT 'in_use'`,
		`ALTER TABLE IF EXISTS badge_devices ADD COLUMN IF NOT EXISTS tenant_id BIGINT`,
		`ALTER TABLE IF EXISTS badge_devices ADD COLUMN IF NOT EXISTS employee_id BIGINT`,
		`ALTER TABLE IF EXISTS badge_devices ADD COLUMN IF NOT EXISTS deleted_at TIMESTAMP`,
		`ALTER TABLE IF EXISTS badge_devices ADD COLUMN IF NOT EXISTS battery_level INTEGER`,
		`ALTER TABLE IF EXISTS badge_devices ADD COLUMN IF NOT EXISTS firmware_version TEXT`,
		`ALTER TABLE IF EXISTS badge_devices ADD COLUMN IF NOT EXISTS extra_data JSONB DEFAULT '{}'::jsonb`,
		`ALTER TABLE IF EXISTS badge_devices ADD COLUMN IF NOT EXISTS accepted_at TIMESTAMP`,
		`ALTER TABLE IF EXISTS badge_devices ADD COLUMN IF NOT EXISTS last_online_at TIMESTAMP`,
		`ALTER TABLE IF EXISTS badge_devices ADD COLUMN IF NOT EXISTS created_at TIMESTAMP NOT NULL DEFAULT NOW()`,
		`ALTER TABLE IF EXISTS badge_devices ADD COLUMN IF NOT EXISTS updated_at TIMESTAMP NOT NULL DEFAULT NOW()`,
		`ALTER TABLE IF EXISTS badge_manufacturers ADD COLUMN IF NOT EXISTS contact_person TEXT`,
		`ALTER TABLE IF EXISTS badge_manufacturers ADD COLUMN IF NOT EXISTS contact_phone TEXT`,
		`ALTER TABLE IF EXISTS badge_manufacturers ADD COLUMN IF NOT EXISTS contact_email TEXT`,
		`ALTER TABLE IF EXISTS badge_manufacturers ADD COLUMN IF NOT EXISTS api_endpoint TEXT`,
		`ALTER TABLE IF EXISTS badge_manufacturers ADD COLUMN IF NOT EXISTS api_key TEXT`,
		`ALTER TABLE IF EXISTS badge_manufacturers ADD COLUMN IF NOT EXISTS is_active BOOLEAN NOT NULL DEFAULT TRUE`,
		`ALTER TABLE IF EXISTS badge_manufacturers ADD COLUMN IF NOT EXISTS config JSONB DEFAULT '{}'::jsonb`,
		`CREATE TABLE IF NOT EXISTS badge_tickets (
			id BIGSERIAL PRIMARY KEY,
			ticket_no TEXT NOT NULL,
			type TEXT NOT NULL,
			status TEXT NOT NULL DEFAULT 'pending',
			device_id BIGINT,
			device_no TEXT,
			tenant_id BIGINT,
			employee_id BIGINT,
			title TEXT NOT NULL DEFAULT '',
			description TEXT NOT NULL DEFAULT '',
			submitter_id BIGINT NOT NULL,
			reviewer_id BIGINT,
			executor_id BIGINT,
			review_notes TEXT,
			execute_notes TEXT,
			submitted_at TIMESTAMP,
			reviewed_at TIMESTAMP,
			executed_at TIMESTAMP,
			completed_at TIMESTAMP,
			extra_data JSONB DEFAULT '{}'::jsonb,
			created_at TIMESTAMP NOT NULL DEFAULT NOW(),
			updated_at TIMESTAMP NOT NULL DEFAULT NOW()
		)`,
		`CREATE UNIQUE INDEX IF NOT EXISTS idx_badge_tickets_ticket_no ON badge_tickets(ticket_no)`,
		`ALTER TABLE IF EXISTS badge_device_lifecycle_logs ADD COLUMN IF NOT EXISTS action TEXT`,
		`ALTER TABLE IF EXISTS badge_device_lifecycle_logs ADD COLUMN IF NOT EXISTS created_at TIMESTAMP NOT NULL DEFAULT NOW()`,
		// Badge V2 refactoring schema compatibility
		`ALTER TABLE IF EXISTS badge_devices ADD COLUMN IF NOT EXISTS manufacturer_name TEXT`,
		`ALTER TABLE IF EXISTS badge_devices ADD COLUMN IF NOT EXISTS hardware_model TEXT`,
		`ALTER TABLE IF EXISTS badge_devices ADD COLUMN IF NOT EXISTS health_status TEXT NOT NULL DEFAULT 'unknown'`,
		`ALTER TABLE IF EXISTS badge_devices ADD COLUMN IF NOT EXISTS health_check_result JSONB`,
		`ALTER TABLE IF EXISTS badge_devices ADD COLUMN IF NOT EXISTS tenant_name TEXT`,
		`ALTER TABLE IF EXISTS badge_devices ADD COLUMN IF NOT EXISTS employee_name TEXT`,
		`ALTER TABLE IF EXISTS badge_devices ADD COLUMN IF NOT EXISTS employee_phone TEXT`,
		`ALTER TABLE IF EXISTS badge_devices ADD COLUMN IF NOT EXISTS assigned_at TIMESTAMP`,
		`ALTER TABLE IF EXISTS badge_devices ADD COLUMN IF NOT EXISTS last_check_at TIMESTAMP`,
		`ALTER TABLE IF EXISTS badge_devices ADD COLUMN IF NOT EXISTS import_batch_no TEXT`,
		`ALTER TABLE IF EXISTS badge_devices ADD COLUMN IF NOT EXISTS metadata JSONB NOT NULL DEFAULT '{}'::jsonb`,
		`DO $$
		BEGIN
			IF NOT EXISTS (
				SELECT 1
				FROM pg_constraint
				WHERE conname = 'chk_badge_devices_health_status_enum'
			) THEN
				ALTER TABLE badge_devices
					ADD CONSTRAINT chk_badge_devices_health_status_enum
					CHECK (health_status IN ('unknown', 'healthy', 'warning', 'error'));
			END IF;
		END $$`,
		`CREATE TABLE IF NOT EXISTS badge_device_logs (
			id BIGSERIAL PRIMARY KEY,
			device_id BIGINT NOT NULL,
			device_no TEXT NOT NULL,
			operation TEXT NOT NULL,
			from_status TEXT,
			to_status TEXT,
			operator_id BIGINT,
			operator_name TEXT,
			operator_type TEXT,
			detail JSONB,
			created_at TIMESTAMP NOT NULL DEFAULT NOW()
		)`,
		`CREATE INDEX IF NOT EXISTS idx_badge_device_logs_device_id ON badge_device_logs(device_id)`,
		`CREATE INDEX IF NOT EXISTS idx_badge_device_logs_operation ON badge_device_logs(operation)`,
		`CREATE INDEX IF NOT EXISTS idx_badge_device_logs_created_at_desc ON badge_device_logs(created_at DESC)`,
		`CREATE INDEX IF NOT EXISTS idx_badge_devices_status ON badge_devices(status)`,
		`CREATE INDEX IF NOT EXISTS idx_badge_devices_health_status ON badge_devices(health_status)`,
		`CREATE INDEX IF NOT EXISTS idx_badge_devices_manufacturer_code ON badge_devices(manufacturer_code)`,
		`CREATE INDEX IF NOT EXISTS idx_badge_devices_tenant_id ON badge_devices(tenant_id)`,
		`CREATE INDEX IF NOT EXISTS idx_badge_devices_employee_id ON badge_devices(employee_id)`,
		`CREATE INDEX IF NOT EXISTS idx_badge_devices_created_at ON badge_devices(created_at)`,

		// Sandbox recording transfer compatibility
		`ALTER TABLE IF EXISTS recordings ADD COLUMN IF NOT EXISTS source_tenant_id BIGINT`,
		`ALTER TABLE IF EXISTS recordings ADD COLUMN IF NOT EXISTS source_recording_id BIGINT`,
		`CREATE INDEX IF NOT EXISTS idx_recordings_source_tenant_id ON recordings(source_tenant_id)`,
		`CREATE INDEX IF NOT EXISTS idx_recordings_source_recording_id ON recordings(source_recording_id)`,
		`CREATE TABLE IF NOT EXISTS sandbox_transfer_tasks (
			id BIGSERIAL PRIMARY KEY,
			task_no TEXT NOT NULL UNIQUE,
			source_tenant_id BIGINT NOT NULL,
			target_tenant_id BIGINT NOT NULL,
			filter_snapshot_json JSONB,
			assignment_mode TEXT NOT NULL DEFAULT 'single',
			assignment_snapshot_json JSONB,
			status TEXT NOT NULL DEFAULT 'pending',
			total_count INTEGER NOT NULL DEFAULT 0,
			success_count INTEGER NOT NULL DEFAULT 0,
			failed_count INTEGER NOT NULL DEFAULT 0,
			created_by BIGINT NOT NULL,
			created_at TIMESTAMP NOT NULL DEFAULT NOW(),
			updated_at TIMESTAMP NOT NULL DEFAULT NOW(),
			started_at TIMESTAMP,
			finished_at TIMESTAMP,
			ttl_days INTEGER NOT NULL DEFAULT 15,
			expire_at TIMESTAMP,
			rollback_status TEXT,
			rollback_at TIMESTAMP,
			rollback_by BIGINT
		)`,
		`CREATE INDEX IF NOT EXISTS idx_sandbox_transfer_tasks_source_tenant_id ON sandbox_transfer_tasks(source_tenant_id)`,
		`CREATE INDEX IF NOT EXISTS idx_sandbox_transfer_tasks_target_tenant_id ON sandbox_transfer_tasks(target_tenant_id)`,
		`CREATE INDEX IF NOT EXISTS idx_sandbox_transfer_tasks_status ON sandbox_transfer_tasks(status)`,
		`CREATE TABLE IF NOT EXISTS sandbox_transfer_task_items (
			id BIGSERIAL PRIMARY KEY,
			task_id BIGINT NOT NULL,
			source_recording_id BIGINT NOT NULL,
			target_recording_id BIGINT,
			source_object_key TEXT,
			target_object_key TEXT,
			target_employee_id BIGINT,
			status TEXT NOT NULL DEFAULT 'pending',
			error_code TEXT,
			error_message TEXT,
			copied_at TIMESTAMP,
			created_at TIMESTAMP NOT NULL DEFAULT NOW(),
			updated_at TIMESTAMP NOT NULL DEFAULT NOW()
		)`,
		`CREATE INDEX IF NOT EXISTS idx_sandbox_transfer_task_items_task_id ON sandbox_transfer_task_items(task_id)`,
		`CREATE INDEX IF NOT EXISTS idx_sandbox_transfer_task_items_source_recording_id ON sandbox_transfer_task_items(source_recording_id)`,
		`CREATE INDEX IF NOT EXISTS idx_sandbox_transfer_task_items_target_recording_id ON sandbox_transfer_task_items(target_recording_id)`,
		`CREATE TABLE IF NOT EXISTS sandbox_employee_mappings (
			id BIGSERIAL PRIMARY KEY,
			source_tenant_id BIGINT NOT NULL,
			source_employee_id BIGINT NOT NULL,
			target_tenant_id BIGINT NOT NULL,
			target_employee_id BIGINT NOT NULL,
			is_active BOOLEAN NOT NULL DEFAULT TRUE,
			created_at TIMESTAMP NOT NULL DEFAULT NOW(),
			updated_at TIMESTAMP NOT NULL DEFAULT NOW(),
			UNIQUE(source_tenant_id, source_employee_id, target_tenant_id, target_employee_id)
		)`,
		`CREATE INDEX IF NOT EXISTS idx_sandbox_employee_mappings_target_tenant_id ON sandbox_employee_mappings(target_tenant_id)`,
		`CREATE INDEX IF NOT EXISTS idx_sandbox_employee_mappings_source_tenant_id ON sandbox_employee_mappings(source_tenant_id)`,

		// Sysconfig compatibility
		`ALTER TABLE IF EXISTS tenant_subscription_plans ADD COLUMN IF NOT EXISTS description TEXT`,
		`ALTER TABLE IF EXISTS tenant_subscription_plans ADD COLUMN IF NOT EXISTS duration_days INTEGER NOT NULL DEFAULT 30`,
		`ALTER TABLE IF EXISTS tenant_subscription_plans ADD COLUMN IF NOT EXISTS price NUMERIC(10,2) NOT NULL DEFAULT 0`,
		`ALTER TABLE IF EXISTS tenant_subscription_plans ADD COLUMN IF NOT EXISTS sort_order INTEGER NOT NULL DEFAULT 0`,
		`ALTER TABLE IF EXISTS tenant_subscription_plans ADD COLUMN IF NOT EXISTS is_active BOOLEAN NOT NULL DEFAULT TRUE`,
		`ALTER TABLE IF EXISTS tenant_subscription_plans ADD COLUMN IF NOT EXISTS feature_group_id BIGINT`,
		`ALTER TABLE IF EXISTS tenant_subscription_plans ADD COLUMN IF NOT EXISTS created_at TIMESTAMP NOT NULL DEFAULT NOW()`,
		`ALTER TABLE IF EXISTS tenant_subscription_plans ADD COLUMN IF NOT EXISTS updated_at TIMESTAMP NOT NULL DEFAULT NOW()`,
		`CREATE TABLE IF NOT EXISTS tenant_subscriptions (
			id BIGSERIAL PRIMARY KEY,
			tenant_id BIGINT NOT NULL,
			plan_id BIGINT,
			status TEXT NOT NULL DEFAULT 'active',
			start_date TIMESTAMP NOT NULL DEFAULT NOW(),
			end_date TIMESTAMP NOT NULL DEFAULT NOW(),
			grace_end_date TIMESTAMP,
			created_at TIMESTAMP NOT NULL DEFAULT NOW(),
			updated_at TIMESTAMP NOT NULL DEFAULT NOW()
		)`,
		`ALTER TABLE IF EXISTS tenant_subscriptions ADD COLUMN IF NOT EXISTS start_date TIMESTAMP`,
		`ALTER TABLE IF EXISTS tenant_subscriptions ADD COLUMN IF NOT EXISTS end_date TIMESTAMP`,
		`ALTER TABLE IF EXISTS tenant_subscriptions ADD COLUMN IF NOT EXISTS grace_end_date TIMESTAMP`,
		`ALTER TABLE IF EXISTS tenant_subscriptions ADD COLUMN IF NOT EXISTS created_at TIMESTAMP NOT NULL DEFAULT NOW()`,
		`ALTER TABLE IF EXISTS tenant_subscriptions ADD COLUMN IF NOT EXISTS updated_at TIMESTAMP NOT NULL DEFAULT NOW()`,
		`CREATE TABLE IF NOT EXISTS tenant_subscription_events (
			id BIGSERIAL PRIMARY KEY,
			tenant_id BIGINT NOT NULL,
			event_type TEXT NOT NULL,
			old_status TEXT,
			new_status TEXT,
			old_end_date TIMESTAMP,
			new_end_date TIMESTAMP,
			operator_id BIGINT,
			operator_type TEXT,
			notes TEXT,
			created_at TIMESTAMP NOT NULL DEFAULT NOW()
		)`,
		`ALTER TABLE IF EXISTS tenant_subscription_events ADD COLUMN IF NOT EXISTS old_status TEXT`,
		`ALTER TABLE IF EXISTS tenant_subscription_events ADD COLUMN IF NOT EXISTS new_status TEXT`,
		`ALTER TABLE IF EXISTS tenant_subscription_events ADD COLUMN IF NOT EXISTS old_end_date TIMESTAMP`,
		`ALTER TABLE IF EXISTS tenant_subscription_events ADD COLUMN IF NOT EXISTS new_end_date TIMESTAMP`,
		`ALTER TABLE IF EXISTS tenant_subscription_events ADD COLUMN IF NOT EXISTS subscription_id BIGINT`,
		`ALTER TABLE IF EXISTS tenant_subscription_events ADD COLUMN IF NOT EXISTS request_id TEXT`,
		`ALTER TABLE IF EXISTS tenant_subscription_events ADD COLUMN IF NOT EXISTS operator_id BIGINT`,
		`ALTER TABLE IF EXISTS tenant_subscription_events ADD COLUMN IF NOT EXISTS operator_type TEXT`,
		`ALTER TABLE IF EXISTS tenant_subscription_events ADD COLUMN IF NOT EXISTS operator_name TEXT`,
		`ALTER TABLE IF EXISTS tenant_subscription_events ADD COLUMN IF NOT EXISTS notes TEXT`,
		`ALTER TABLE IF EXISTS tenant_subscription_events ADD COLUMN IF NOT EXISTS created_at TIMESTAMP NOT NULL DEFAULT NOW()`,
		`CREATE TABLE IF NOT EXISTS tenant_validity_change_logs (
			id BIGSERIAL PRIMARY KEY,
			tenant_id BIGINT NOT NULL,
			old_valid_from TIMESTAMP,
			new_valid_from TIMESTAMP,
			old_valid_to TIMESTAMP,
			new_valid_to TIMESTAMP,
			operator_id BIGINT,
			operator_type TEXT,
			reason TEXT,
			created_at TIMESTAMP NOT NULL DEFAULT NOW()
		)`,
		`ALTER TABLE IF EXISTS tenant_validity_change_logs ADD COLUMN IF NOT EXISTS old_valid_from TIMESTAMP`,
		`ALTER TABLE IF EXISTS tenant_validity_change_logs ADD COLUMN IF NOT EXISTS new_valid_from TIMESTAMP`,
		`ALTER TABLE IF EXISTS tenant_validity_change_logs ADD COLUMN IF NOT EXISTS old_valid_to TIMESTAMP`,
		`ALTER TABLE IF EXISTS tenant_validity_change_logs ADD COLUMN IF NOT EXISTS new_valid_to TIMESTAMP`,
		`ALTER TABLE IF EXISTS tenant_validity_change_logs ADD COLUMN IF NOT EXISTS operator_id BIGINT`,
		`ALTER TABLE IF EXISTS tenant_validity_change_logs ADD COLUMN IF NOT EXISTS operator_type TEXT`,
		`ALTER TABLE IF EXISTS tenant_validity_change_logs ADD COLUMN IF NOT EXISTS reason TEXT`,
		`ALTER TABLE IF EXISTS tenant_validity_change_logs ADD COLUMN IF NOT EXISTS created_at TIMESTAMP NOT NULL DEFAULT NOW()`,
		`CREATE TABLE IF NOT EXISTS tenant_feature_groups (
			id BIGSERIAL PRIMARY KEY,
			name TEXT NOT NULL,
			code TEXT NOT NULL UNIQUE,
			description TEXT,
			is_active BOOLEAN NOT NULL DEFAULT TRUE,
			created_at TIMESTAMP NOT NULL DEFAULT NOW(),
			updated_at TIMESTAMP NOT NULL DEFAULT NOW()
		)`,
		`ALTER TABLE IF EXISTS tenant_feature_groups ADD COLUMN IF NOT EXISTS is_active BOOLEAN NOT NULL DEFAULT TRUE`,
		`ALTER TABLE IF EXISTS tenant_feature_groups ADD COLUMN IF NOT EXISTS created_at TIMESTAMP NOT NULL DEFAULT NOW()`,
		`ALTER TABLE IF EXISTS tenant_feature_groups ADD COLUMN IF NOT EXISTS updated_at TIMESTAMP NOT NULL DEFAULT NOW()`,
		`CREATE TABLE IF NOT EXISTS tenant_feature_group_items (
			id BIGSERIAL PRIMARY KEY,
			group_id BIGINT NOT NULL,
			feature_code TEXT NOT NULL,
			is_enabled BOOLEAN NOT NULL DEFAULT TRUE
		)`,
		`ALTER TABLE IF EXISTS tenant_feature_group_items ADD COLUMN IF NOT EXISTS feature_code TEXT`,
		`ALTER TABLE IF EXISTS tenant_feature_group_items ADD COLUMN IF NOT EXISTS item_type VARCHAR(16)`,
		`ALTER TABLE IF EXISTS tenant_feature_group_items ADD COLUMN IF NOT EXISTS item_code VARCHAR(128)`,
		`ALTER TABLE IF EXISTS tenant_feature_group_items ADD COLUMN IF NOT EXISTS created_at TIMESTAMP NOT NULL DEFAULT NOW()`,
		`ALTER TABLE IF EXISTS tenant_feature_group_items ADD COLUMN IF NOT EXISTS is_enabled BOOLEAN NOT NULL DEFAULT TRUE`,
		`CREATE TABLE IF NOT EXISTS tenant_feature_assignments (
			tenant_id BIGINT PRIMARY KEY,
			group_id BIGINT NOT NULL,
			created_at TIMESTAMP NOT NULL DEFAULT NOW(),
			updated_at TIMESTAMP NOT NULL DEFAULT NOW()
		)`,
		`CREATE TABLE IF NOT EXISTS tenant_feature_overrides (
			id BIGSERIAL PRIMARY KEY,
			tenant_id BIGINT NOT NULL,
			feature_code TEXT NOT NULL,
			is_enabled BOOLEAN NOT NULL DEFAULT TRUE,
			created_at TIMESTAMP NOT NULL DEFAULT NOW(),
			updated_at TIMESTAMP NOT NULL DEFAULT NOW()
		)`,
		`ALTER TABLE IF EXISTS tenant_feature_overrides ADD COLUMN IF NOT EXISTS feature_code TEXT`,
		`ALTER TABLE IF EXISTS tenant_feature_overrides ADD COLUMN IF NOT EXISTS item_type VARCHAR(16)`,
		`ALTER TABLE IF EXISTS tenant_feature_overrides ADD COLUMN IF NOT EXISTS item_code VARCHAR(128)`,
		`ALTER TABLE IF EXISTS tenant_feature_overrides ADD COLUMN IF NOT EXISTS override_mode VARCHAR(16)`,
		`ALTER TABLE IF EXISTS tenant_feature_overrides ADD COLUMN IF NOT EXISTS is_enabled BOOLEAN NOT NULL DEFAULT TRUE`,
		`ALTER TABLE IF EXISTS tenant_feature_overrides ADD COLUMN IF NOT EXISTS created_at TIMESTAMP NOT NULL DEFAULT NOW()`,
		`ALTER TABLE IF EXISTS tenant_feature_overrides ADD COLUMN IF NOT EXISTS updated_at TIMESTAMP NOT NULL DEFAULT NOW()`,
		`ALTER TABLE IF EXISTS inst_menus ADD COLUMN IF NOT EXISTS is_feature_assignable BOOLEAN NOT NULL DEFAULT FALSE`,
		`ALTER TABLE IF EXISTS inst_menus ADD COLUMN IF NOT EXISTS is_default_for_admin BOOLEAN NOT NULL DEFAULT FALSE`,
		`ALTER TABLE IF EXISTS inst_menus ADD COLUMN IF NOT EXISTS feature_code VARCHAR(128)`,
		`ALTER TABLE IF EXISTS inst_menus ADD COLUMN IF NOT EXISTS feature_name VARCHAR(128)`,
		`ALTER TABLE IF EXISTS institution_menus ADD COLUMN IF NOT EXISTS is_feature_assignable BOOLEAN NOT NULL DEFAULT FALSE`,
		`ALTER TABLE IF EXISTS institution_menus ADD COLUMN IF NOT EXISTS is_default_for_admin BOOLEAN NOT NULL DEFAULT FALSE`,
		`ALTER TABLE IF EXISTS institution_menus ADD COLUMN IF NOT EXISTS feature_code VARCHAR(128)`,
		`ALTER TABLE IF EXISTS institution_menus ADD COLUMN IF NOT EXISTS feature_name VARCHAR(128)`,
		// LLM model config compatibility (old schema -> support module schema)
		`ALTER TABLE IF EXISTS llm_model_configs ADD COLUMN IF NOT EXISTS api_endpoint VARCHAR(500)`,
		`ALTER TABLE IF EXISTS llm_model_configs ADD COLUMN IF NOT EXISTS api_key VARCHAR(500)`,
		`ALTER TABLE IF EXISTS llm_model_configs ADD COLUMN IF NOT EXISTS model_params JSON`,
		`ALTER TABLE IF EXISTS llm_model_configs ADD COLUMN IF NOT EXISTS description TEXT`,
		`ALTER TABLE IF EXISTS llm_model_configs ADD COLUMN IF NOT EXISTS created_by BIGINT NOT NULL DEFAULT 0`,
		`ALTER TABLE IF EXISTS llm_model_configs ADD COLUMN IF NOT EXISTS deleted_at TIMESTAMP`,
		`ALTER TABLE IF EXISTS llm_model_configs ALTER COLUMN tenant_id SET DEFAULT 0`,
		`ALTER TABLE IF EXISTS llm_model_configs ALTER COLUMN model_code SET DEFAULT ''`,
		`ALTER TABLE IF EXISTS llm_model_configs ALTER COLUMN function_type SET DEFAULT 'general'`,
		`UPDATE llm_model_configs
		   SET model_params = jsonb_set(
		         COALESCE(model_params, '{}'::json)::jsonb,
		         '{timeout_seconds}',
		         '120'::jsonb,
		         true
		       )::json
		 WHERE deleted_at IS NULL
		   AND function_type IN ('content_article', 'content_script', 'content_graphic_note', 'content_generation')
		   AND COALESCE(NULLIF(model_params->>'timeout_seconds', ''), '0')::int < 120`,
		`UPDATE llm_model_configs
		   SET model_params = jsonb_set(
		         COALESCE(model_params, '{}'::json)::jsonb,
		         '{timeout_seconds}',
		         '90'::jsonb,
		         true
		       )::json
		 WHERE deleted_at IS NULL
		   AND function_type IN ('content_topic', 'topic_generation')
		   AND COALESCE(NULLIF(model_params->>'timeout_seconds', ''), '0')::int < 90`,
		`INSERT INTO inst_menus (code, name, path, order_index, is_active, created_at)
		 SELECT 'learning_center_benchmarks', '标杆学习', '/learning-center/benchmarks', 901, true, NOW()
		 WHERE NOT EXISTS (
		 	SELECT 1 FROM inst_menus WHERE code = 'learning_center_benchmarks'
		 )`,
		`INSERT INTO inst_menus (code, name, path, order_index, is_active, created_at)
		 SELECT 'management_dashboard_overview', '团队总览', '/management-dashboard/overview', 951, true, NOW()
		 WHERE NOT EXISTS (
		 	SELECT 1 FROM inst_menus WHERE code = 'management_dashboard_overview'
		 )`,
		`INSERT INTO inst_menus (code, name, path, order_index, is_active, created_at)
		 SELECT 'management_dashboard_tracking', '变化追踪', '/management-dashboard/tracking', 952, true, NOW()
		 WHERE NOT EXISTS (
		 	SELECT 1 FROM inst_menus WHERE code = 'management_dashboard_tracking'
		 )`,
		`INSERT INTO inst_menus (code, name, path, order_index, is_active, created_at)
		 SELECT 'management_dashboard_benchmarks', '标杆库', '/management-dashboard/benchmarks', 953, true, NOW()
		 WHERE NOT EXISTS (
		 	SELECT 1 FROM inst_menus WHERE code = 'management_dashboard_benchmarks'
		 )`,
		`INSERT INTO inst_menus (code, name, path, order_index, is_active, created_at)
		 SELECT 'management_dashboard_risks', '风险与待办', '/management-dashboard/risks', 954, true, NOW()
		 WHERE NOT EXISTS (
		 	SELECT 1 FROM inst_menus WHERE code = 'management_dashboard_risks'
		 )`,
		`INSERT INTO inst_menus (code, name, path, order_index, is_active, created_at)
		 SELECT 'management_dashboard_coaching_tasks', '辅导任务', '/management-dashboard/coaching-tasks', 955, true, NOW()
		 WHERE NOT EXISTS (
		 	SELECT 1 FROM inst_menus WHERE code = 'management_dashboard_coaching_tasks'
		 )`,
		`INSERT INTO inst_menus (code, name, path, order_index, is_active, created_at)
		 SELECT 'management_dashboard_meetings_morning', '早会', '/management-dashboard/meetings/morning', 956, true, NOW()
		 WHERE NOT EXISTS (
		 	SELECT 1 FROM inst_menus WHERE code = 'management_dashboard_meetings_morning'
		 )`,
		`INSERT INTO inst_menus (code, name, path, order_index, is_active, created_at)
		 SELECT 'management_dashboard_meetings_weekly', '周会', '/management-dashboard/meetings/weekly', 957, true, NOW()
		 WHERE NOT EXISTS (
		 	SELECT 1 FROM inst_menus WHERE code = 'management_dashboard_meetings_weekly'
		 )`,
		`INSERT INTO inst_menus (code, name, path, order_index, is_active, created_at)
		 SELECT 'frontdesk_recordings', '录音列表', '/frontdesk-recordings', 1151, true, NOW()
		 WHERE NOT EXISTS (
		 	SELECT 1 FROM inst_menus WHERE code = 'frontdesk_recordings'
		 )`,
		`INSERT INTO inst_menus (code, name, path, order_index, is_active, created_at)
		 SELECT 'therapist_recordings', '录音列表', '/therapist-recordings', 1152, true, NOW()
		 WHERE NOT EXISTS (
		 	SELECT 1 FROM inst_menus WHERE code = 'therapist_recordings'
		 )`,
		`UPDATE inst_menus
		    SET name = '标杆学习',
		        path = '/learning-center/benchmarks',
		        order_index = COALESCE(order_index, 901),
		        is_active = true
		  WHERE code = 'learning_center_benchmarks'`,
		`UPDATE inst_menus
		    SET name = '团队总览',
		        path = '/management-dashboard/overview',
		        order_index = COALESCE(order_index, 951),
		        is_active = true
		  WHERE code = 'management_dashboard_overview'`,
		`UPDATE inst_menus
		    SET name = '变化追踪',
		        path = '/management-dashboard/tracking',
		        order_index = COALESCE(order_index, 952),
		        is_active = true
		  WHERE code = 'management_dashboard_tracking'`,
		`UPDATE inst_menus
		    SET name = '标杆库',
		        path = '/management-dashboard/benchmarks',
		        order_index = COALESCE(order_index, 953),
		        is_active = true
		  WHERE code = 'management_dashboard_benchmarks'`,
		`UPDATE inst_menus
		    SET name = '风险与待办',
		        path = '/management-dashboard/risks',
		        order_index = COALESCE(order_index, 954),
		        is_active = true
		  WHERE code = 'management_dashboard_risks'`,
		`UPDATE inst_menus
		    SET name = '辅导任务',
		        path = '/management-dashboard/coaching-tasks',
		        order_index = COALESCE(order_index, 955),
		        is_active = true
		  WHERE code = 'management_dashboard_coaching_tasks'`,
		`UPDATE inst_menus
		    SET name = '早会',
		        path = '/management-dashboard/meetings/morning',
		        order_index = COALESCE(order_index, 956),
		        is_active = true
		  WHERE code = 'management_dashboard_meetings_morning'`,
		`UPDATE inst_menus
		    SET name = '周会',
		        path = '/management-dashboard/meetings/weekly',
		        order_index = COALESCE(order_index, 957),
		        is_active = true
		  WHERE code = 'management_dashboard_meetings_weekly'`,
		`UPDATE inst_menus
		    SET name = '录音列表',
		        path = '/frontdesk-recordings',
		        order_index = COALESCE(order_index, 1151),
		        is_active = true
		  WHERE code = 'frontdesk_recordings'`,
		`UPDATE inst_menus
		    SET name = '录音列表',
		        path = '/therapist-recordings',
		        order_index = COALESCE(order_index, 1152),
		        is_active = true
		  WHERE code = 'therapist_recordings'`,
		`INSERT INTO inst_menus (code, name, path, order_index, is_active, created_at)
		 SELECT 'knowledge', '知识条目', '/knowledge', 1450, true, NOW()
		 WHERE NOT EXISTS (
		 	SELECT 1 FROM inst_menus WHERE code = 'knowledge'
		 )`,
		`UPDATE inst_menus
		    SET name = '知识条目',
		        path = '/knowledge',
		        order_index = COALESCE(order_index, 1450),
		        is_active = true
		  WHERE code = 'knowledge'`,
		`UPDATE inst_menus
		    SET is_feature_assignable = false,
		        is_default_for_admin = false,
		        feature_code = NULL,
		        feature_name = NULL`,
		`UPDATE inst_menus
		    SET is_feature_assignable = true,
		        is_default_for_admin = true,
		        feature_code = 'learning_center',
		        feature_name = '学习中心'
		  WHERE code = 'learning_center_benchmarks'`,
		`UPDATE inst_menus
		    SET is_feature_assignable = true,
		        is_default_for_admin = true,
		        feature_code = 'management_dashboard',
		        feature_name = '管理看板'
		  WHERE code IN (
		    'management_dashboard_overview',
		    'management_dashboard_tracking',
		    'management_dashboard_benchmarks',
		    'management_dashboard_risks',
		    'management_dashboard_coaching_tasks',
		    'management_dashboard_meetings_morning',
		    'management_dashboard_meetings_weekly'
		  )`,
		`UPDATE inst_menus
		    SET is_feature_assignable = true,
		        is_default_for_admin = true,
		        feature_code = 'medical_recording_center',
		        feature_name = '医疗录音'
		  WHERE code IN ('doctor_recordings', 'doctor_recordings_ability', 'doctor_recordings_team_trends', 'doctor_recordings_weekly_summary')`,
		`UPDATE inst_menus
		    SET is_feature_assignable = true,
		        is_default_for_admin = true,
		        feature_code = 'recording_center',
		        feature_name = '咨询录音'
		  WHERE code IN ('consultant_recordings', 'consultant_recordings_team_ability', 'consultant_recordings_dashboard')`,
		`UPDATE inst_menus
		    SET is_feature_assignable = true,
		        is_default_for_admin = true,
		        feature_code = 'frontdesk_recording_center',
		        feature_name = '前台录音'
		  WHERE code = 'frontdesk_recordings'`,
		`UPDATE inst_menus
		    SET is_feature_assignable = true,
		        is_default_for_admin = true,
		        feature_code = 'therapist_recording_center',
		        feature_name = '治疗师录音'
		  WHERE code = 'therapist_recordings'`,
		`UPDATE inst_menus
		    SET is_feature_assignable = true,
		        is_default_for_admin = true,
		        feature_code = 'tasks_recording',
		        feature_name = '录音任务'
		  WHERE code IN ('tasks', 'tasks_board', 'tasks_partnerships', 'tasks_assignment')`,
		`UPDATE inst_menus
		    SET is_feature_assignable = true,
		        is_default_for_admin = true,
		        feature_code = 'customer_center',
		        feature_name = '客户中心'
		  WHERE code = 'customers'`,
		`UPDATE inst_menus
		    SET is_feature_assignable = true,
		        is_default_for_admin = true,
		        feature_code = 'content_center',
		        feature_name = '内容中心'
		  WHERE code IN ('content_create', 'content_seeds', 'content_library')`,
		`UPDATE inst_menus
		    SET is_feature_assignable = true,
		        is_default_for_admin = true,
		        feature_code = 'knowledge_center',
		        feature_name = '知识库'
		  WHERE code = 'knowledge'`,
		`UPDATE inst_menus
		    SET is_feature_assignable = true,
		        is_default_for_admin = true,
		        feature_code = 'smart_badge',
		        feature_name = '工牌管理'
		  WHERE code IN ('badges_overview', 'badges_recording_control', 'badges_recording_stats', 'badges_tickets')`,
		`UPDATE inst_menus
		    SET is_feature_assignable = true,
		        is_default_for_admin = true,
		        feature_code = 'system_management',
		        feature_name = '系统管理'
		  WHERE code IN ('departments', 'roles', 'menus')`,
		`UPDATE tenant_feature_group_items
		    SET item_code = CASE item_code
		      WHEN 'knowledge-list' THEN 'knowledge'
		      WHEN 'knowledge_center' THEN 'knowledge'
		      WHEN 'customer-list' THEN 'customers'
		      WHEN 'products' THEN 'recordings_products'
		      WHEN 'recordings_products' THEN 'recordings_products'
		      WHEN 'content-workbench' THEN 'content_create'
		      WHEN 'content-list' THEN 'content_library'
		      WHEN 'content-center' THEN 'content_create'
		      WHEN 'content-recording-seeds' THEN 'content_seeds'
		      WHEN 'frontdesk' THEN 'frontdesk_recordings'
		      WHEN 'frontdesk_recordings' THEN 'frontdesk_recordings'
		      WHEN 'frontdesk_recording_list' THEN 'frontdesk_recordings'
		      WHEN 'recording_list' THEN 'consultant_recordings'
		      WHEN 'recording_team_ability' THEN 'consultant_recordings_team_ability'
		      WHEN 'recording_dashboard' THEN 'consultant_recordings_dashboard'
		      WHEN 'medical_recording_list' THEN 'doctor_recordings'
		      WHEN 'medical_doctor_list' THEN 'doctor_recordings_ability'
		      WHEN 'medical_team_trends' THEN 'doctor_recordings_team_trends'
		      WHEN 'medical_weekly_summary' THEN 'doctor_recordings_weekly_summary'
		      WHEN 'tasks_recording_list' THEN 'tasks'
		      WHEN 'tasks_recording_dashboard' THEN 'tasks_board'
		      WHEN 'tasks_recording_partnerships' THEN 'tasks_partnerships'
		      WHEN 'tasks_recording_assign' THEN 'tasks_assignment'
		      WHEN 'recordings_smart_badge_overview' THEN 'badges_overview'
		      WHEN 'recordings_smart_badge_recording_control' THEN 'badges_recording_control'
		      WHEN 'recording_stats' THEN 'badges_recording_stats'
		      WHEN 'recordings_smart_badge_tickets' THEN 'badges_tickets'
		      WHEN 'organization' THEN 'departments'
		      ELSE item_code
		    END
		  WHERE COALESCE(NULLIF(item_type, ''), 'feature') = 'menu'`,
		`UPDATE tenant_feature_overrides
		    SET item_code = CASE item_code
		      WHEN 'knowledge-list' THEN 'knowledge'
		      WHEN 'knowledge_center' THEN 'knowledge'
		      WHEN 'customer-list' THEN 'customers'
		      WHEN 'products' THEN 'recordings_products'
		      WHEN 'recordings_products' THEN 'recordings_products'
		      WHEN 'content-workbench' THEN 'content_create'
		      WHEN 'content-list' THEN 'content_library'
		      WHEN 'content-center' THEN 'content_create'
		      WHEN 'content-recording-seeds' THEN 'content_seeds'
		      WHEN 'frontdesk' THEN 'frontdesk_recordings'
		      WHEN 'frontdesk_recordings' THEN 'frontdesk_recordings'
		      WHEN 'frontdesk_recording_list' THEN 'frontdesk_recordings'
		      WHEN 'recording_list' THEN 'consultant_recordings'
		      WHEN 'recording_team_ability' THEN 'consultant_recordings_team_ability'
		      WHEN 'recording_dashboard' THEN 'consultant_recordings_dashboard'
		      WHEN 'medical_recording_list' THEN 'doctor_recordings'
		      WHEN 'medical_doctor_list' THEN 'doctor_recordings_ability'
		      WHEN 'medical_team_trends' THEN 'doctor_recordings_team_trends'
		      WHEN 'medical_weekly_summary' THEN 'doctor_recordings_weekly_summary'
		      WHEN 'tasks_recording_list' THEN 'tasks'
		      WHEN 'tasks_recording_dashboard' THEN 'tasks_board'
		      WHEN 'tasks_recording_partnerships' THEN 'tasks_partnerships'
		      WHEN 'tasks_recording_assign' THEN 'tasks_assignment'
		      WHEN 'recordings_smart_badge_overview' THEN 'badges_overview'
		      WHEN 'recordings_smart_badge_recording_control' THEN 'badges_recording_control'
		      WHEN 'recording_stats' THEN 'badges_recording_stats'
		      WHEN 'recordings_smart_badge_tickets' THEN 'badges_tickets'
		      WHEN 'organization' THEN 'departments'
		      ELSE item_code
		    END
		  WHERE COALESCE(NULLIF(item_type, ''), 'feature') = 'menu'`,
		`DO $$
		BEGIN
			IF EXISTS (
				SELECT 1 FROM information_schema.tables
				WHERE table_schema = 'public' AND table_name = 'institution_menus'
			) THEN
				DELETE FROM tenant_feature_group_items
				WHERE COALESCE(NULLIF(item_type, ''), 'feature') = 'menu'
				  AND COALESCE(NULLIF(item_code, ''), '') NOT IN (
					SELECT lower(trim(code))
					FROM institution_menus
					WHERE deleted_at IS NULL
					  AND COALESCE(is_feature_assignable, false) = true
					UNION
					SELECT 'trial_home'
					UNION
					SELECT 'recording_upload'
				  );

				DELETE FROM tenant_feature_overrides
				WHERE COALESCE(NULLIF(item_type, ''), 'feature') = 'menu'
				  AND COALESCE(NULLIF(item_code, ''), '') NOT IN (
					SELECT lower(trim(code))
					FROM institution_menus
					WHERE deleted_at IS NULL
					  AND COALESCE(is_feature_assignable, false) = true
					UNION
					SELECT 'trial_home'
					UNION
					SELECT 'recording_upload'
				  );
			ELSE
				DELETE FROM tenant_feature_group_items
				WHERE COALESCE(NULLIF(item_type, ''), 'feature') = 'menu'
				  AND COALESCE(NULLIF(item_code, ''), '') NOT IN (
					SELECT lower(trim(code))
					FROM inst_menus
					WHERE COALESCE(is_feature_assignable, false) = true
					UNION
					SELECT 'trial_home'
					UNION
					SELECT 'recording_upload'
				  );

				DELETE FROM tenant_feature_overrides
				WHERE COALESCE(NULLIF(item_type, ''), 'feature') = 'menu'
				  AND COALESCE(NULLIF(item_code, ''), '') NOT IN (
					SELECT lower(trim(code))
					FROM inst_menus
					WHERE COALESCE(is_feature_assignable, false) = true
					UNION
					SELECT 'trial_home'
					UNION
					SELECT 'recording_upload'
				  );
			END IF;
		END $$`,
		`DO $$
		DECLARE
			v_parent_id BIGINT;
			v_group_id BIGINT;
		BEGIN
			IF EXISTS (
				SELECT 1 FROM pg_tables WHERE schemaname = 'public' AND tablename = 'institution_menus'
			) THEN
				INSERT INTO institution_menus (
					tenant_id, name, code, path, icon, parent_id, sort_order, is_active,
					is_feature_assignable, is_default_for_admin, feature_code, feature_name, created_at, updated_at
				)
				SELECT NULL, '试用体验', 'trial_experience', NULL, NULL, NULL, 0, true,
				       false, false, NULL, NULL, NOW(), NOW()
				WHERE NOT EXISTS (
					SELECT 1 FROM institution_menus
					WHERE tenant_id IS NULL AND lower(code) = 'trial_experience' AND deleted_at IS NULL
				);

				SELECT id INTO v_parent_id
				FROM institution_menus
				WHERE tenant_id IS NULL AND lower(code) = 'trial_experience' AND deleted_at IS NULL
				ORDER BY id ASC
				LIMIT 1;

				IF v_parent_id IS NOT NULL THEN
					UPDATE institution_menus
					SET name = '试用体验',
					    path = NULL,
					    parent_id = NULL,
					    sort_order = 0,
					    is_active = true,
					    is_feature_assignable = false,
					    updated_at = NOW()
					WHERE id = v_parent_id;

					INSERT INTO institution_menus (
						tenant_id, name, code, path, icon, parent_id, sort_order, is_active,
						is_feature_assignable, is_default_for_admin, feature_code, feature_name, created_at, updated_at
					)
					SELECT NULL, '试用首页', 'trial_home', '/trial-home', NULL, v_parent_id, 0, true,
					       true, false, NULL, NULL, NOW(), NOW()
					WHERE NOT EXISTS (
						SELECT 1 FROM institution_menus
						WHERE tenant_id IS NULL AND lower(code) = 'trial_home' AND deleted_at IS NULL
					);

					UPDATE institution_menus
					SET name = '试用首页',
					    path = '/trial-home',
					    parent_id = v_parent_id,
					    sort_order = 0,
					    is_active = true,
					    is_feature_assignable = true,
					    updated_at = NOW()
					WHERE tenant_id IS NULL AND lower(code) = 'trial_home' AND deleted_at IS NULL;

					INSERT INTO institution_menus (
						tenant_id, name, code, path, icon, parent_id, sort_order, is_active,
						is_feature_assignable, is_default_for_admin, feature_code, feature_name, created_at, updated_at
					)
					SELECT NULL, '上传录音', 'recording_upload', '/recording-upload', NULL, v_parent_id, 1, true,
					       true, false, NULL, NULL, NOW(), NOW()
					WHERE NOT EXISTS (
						SELECT 1 FROM institution_menus
						WHERE tenant_id IS NULL AND lower(code) = 'recording_upload' AND deleted_at IS NULL
					);

					UPDATE institution_menus
					SET name = '上传录音',
					    path = '/recording-upload',
					    parent_id = v_parent_id,
					    sort_order = 1,
					    is_active = true,
					    is_feature_assignable = true,
					    updated_at = NOW()
					WHERE tenant_id IS NULL AND lower(code) = 'recording_upload' AND deleted_at IS NULL;
				END IF;
			END IF;

			INSERT INTO tenant_feature_groups (name, code, description, is_active, created_at, updated_at)
			SELECT '试用功能包', 'trial_experience', '试用租户默认功能包', true, NOW(), NOW()
			WHERE NOT EXISTS (
				SELECT 1 FROM tenant_feature_groups WHERE lower(code) = 'trial_experience'
			);

			SELECT id INTO v_group_id
			FROM tenant_feature_groups
			WHERE lower(code) = 'trial_experience'
			ORDER BY id ASC
			LIMIT 1;

			IF v_group_id IS NOT NULL THEN
				UPDATE tenant_feature_groups
				SET name = '试用功能包',
				    description = '试用租户默认功能包',
				    is_active = true,
				    updated_at = NOW()
				WHERE id = v_group_id;

				INSERT INTO tenant_feature_group_items (group_id, item_type, item_code, feature_code, is_enabled, created_at)
				SELECT v_group_id, 'menu', v.item_code, '', true, NOW()
				FROM (
					VALUES
						('trial_home'),
						('recording_upload'),
						('doctor_recordings'),
						('consultant_recordings'),
						('customers'),
						('tasks'),
						('departments')
				) AS v(item_code)
				WHERE NOT EXISTS (
					SELECT 1
					FROM tenant_feature_group_items existing
					WHERE existing.group_id = v_group_id
					  AND COALESCE(NULLIF(existing.item_type, ''), 'feature') = 'menu'
					  AND lower(COALESCE(NULLIF(existing.item_code, ''), '')) = lower(v.item_code)
				);
			END IF;
		END $$`,
		`DO $$
		BEGIN
			IF EXISTS (
				SELECT 1 FROM pg_tables WHERE schemaname = 'public' AND tablename = 'institution_role_menus'
			) AND EXISTS (
				SELECT 1 FROM pg_tables WHERE schemaname = 'public' AND tablename = 'institution_roles'
			) AND EXISTS (
				SELECT 1 FROM pg_tables WHERE schemaname = 'public' AND tablename = 'institution_menus'
			) THEN
				INSERT INTO institution_role_menus (tenant_id, role_code, menu_code, created_at, updated_at)
				SELECT DISTINCT r.tenant_id, 'admin', m.code, NOW(), NOW()
				FROM institution_roles r
				JOIN institution_menus m
				  ON COALESCE(m.is_active, true) = true
				 AND COALESCE(m.is_default_for_admin, false) = true
				 AND m.deleted_at IS NULL
				 AND (m.tenant_id = r.tenant_id OR m.tenant_id IS NULL)
				WHERE lower(r.code) = 'admin'
				  AND r.deleted_at IS NULL
				  AND NOT EXISTS (
					SELECT 1
					FROM institution_role_menus rm
					WHERE rm.tenant_id = r.tenant_id
					  AND lower(rm.role_code) = 'admin'
					  AND lower(rm.menu_code) = lower(m.code)
				  );
			END IF;
		END $$`,
		`CREATE TABLE IF NOT EXISTS tenant_feature_change_logs (
			id BIGSERIAL PRIMARY KEY,
			tenant_id BIGINT NOT NULL,
			feature_code TEXT NOT NULL,
			new_value BOOLEAN,
			created_at TIMESTAMP NOT NULL DEFAULT NOW()
		)`,

		// RBAC compatibility
		`CREATE TABLE IF NOT EXISTS operations_roles (
			id BIGSERIAL PRIMARY KEY,
			name TEXT NOT NULL,
			code TEXT NOT NULL UNIQUE,
			description TEXT,
			is_active BOOLEAN NOT NULL DEFAULT TRUE,
			created_at TIMESTAMP NOT NULL DEFAULT NOW(),
			updated_at TIMESTAMP NOT NULL DEFAULT NOW(),
			deleted_at TIMESTAMP
		)`,
		`CREATE TABLE IF NOT EXISTS operations_menus (
			id BIGSERIAL PRIMARY KEY,
			name TEXT NOT NULL,
			code TEXT NOT NULL UNIQUE,
			path TEXT NOT NULL DEFAULT '',
			icon TEXT,
			parent_id BIGINT,
			sort_order INTEGER NOT NULL DEFAULT 0,
			is_active BOOLEAN NOT NULL DEFAULT TRUE,
			created_at TIMESTAMP NOT NULL DEFAULT NOW(),
			updated_at TIMESTAMP NOT NULL DEFAULT NOW(),
			deleted_at TIMESTAMP
		)`,
		`CREATE TABLE IF NOT EXISTS operations_permissions (
			id BIGSERIAL PRIMARY KEY,
			name TEXT NOT NULL,
			code TEXT NOT NULL UNIQUE,
			resource TEXT NOT NULL DEFAULT '',
			action TEXT NOT NULL DEFAULT '',
			description TEXT,
			created_at TIMESTAMP NOT NULL DEFAULT NOW(),
			updated_at TIMESTAMP NOT NULL DEFAULT NOW()
		)`,
		`CREATE TABLE IF NOT EXISTS operations_role_permissions (
			role_id BIGINT NOT NULL,
			permission_id BIGINT NOT NULL,
			created_at TIMESTAMP NOT NULL DEFAULT NOW(),
			PRIMARY KEY (role_id, permission_id)
		)`,
		`CREATE TABLE IF NOT EXISTS operations_role_menus (
			role_id BIGINT NOT NULL,
			menu_id BIGINT NOT NULL,
			created_at TIMESTAMP NOT NULL DEFAULT NOW(),
			PRIMARY KEY (role_id, menu_id)
		)`,
		`CREATE TABLE IF NOT EXISTS operations_admins (
			id BIGSERIAL PRIMARY KEY,
			name VARCHAR(255) NOT NULL DEFAULT '',
			phone VARCHAR(50) NOT NULL DEFAULT '',
			username TEXT,
			password_hash TEXT NOT NULL DEFAULT '',
			email VARCHAR(255),
			is_active INTEGER DEFAULT 1,
			org_id BIGINT,
			tenant_id INTEGER,
			last_login_at TIMESTAMP,
			session_version INTEGER NOT NULL DEFAULT 1,
			created_at TIMESTAMP NOT NULL DEFAULT NOW(),
			updated_at TIMESTAMP,
			deleted_at TIMESTAMP
		)`,
		`ALTER TABLE IF EXISTS operations_admins ADD COLUMN IF NOT EXISTS org_id BIGINT`,
		`CREATE TABLE IF NOT EXISTS ops_organizations (
			id BIGSERIAL PRIMARY KEY,
			name TEXT NOT NULL,
			type TEXT NOT NULL DEFAULT 'agency',
			parent_id BIGINT,
			status TEXT NOT NULL DEFAULT 'active',
			created_at TIMESTAMP NOT NULL DEFAULT NOW(),
			updated_at TIMESTAMP NOT NULL DEFAULT NOW()
		)`,
		`CREATE TABLE IF NOT EXISTS operations_admin_roles (
			admin_id BIGINT NOT NULL,
			role_id BIGINT NOT NULL,
			created_at TIMESTAMP NOT NULL DEFAULT NOW(),
			PRIMARY KEY (admin_id, role_id)
		)`,

		// Customer detail compatibility
		`ALTER TABLE IF EXISTS customer_memberships ADD COLUMN IF NOT EXISTS tenant_id BIGINT`,
		`ALTER TABLE IF EXISTS customer_memberships ADD COLUMN IF NOT EXISTS level TEXT`,
		`ALTER TABLE IF EXISTS customer_memberships ADD COLUMN IF NOT EXISTS points INTEGER NOT NULL DEFAULT 0`,
		`ALTER TABLE IF EXISTS customer_memberships ADD COLUMN IF NOT EXISTS start_date TIMESTAMP`,
		`ALTER TABLE IF EXISTS customer_memberships ADD COLUMN IF NOT EXISTS end_date TIMESTAMP`,
		`ALTER TABLE IF EXISTS customer_memberships ADD COLUMN IF NOT EXISTS is_active BOOLEAN NOT NULL DEFAULT TRUE`,
		`ALTER TABLE IF EXISTS customer_memberships ADD COLUMN IF NOT EXISTS created_at TIMESTAMP NOT NULL DEFAULT NOW()`,
		`ALTER TABLE IF EXISTS customer_memberships ADD COLUMN IF NOT EXISTS updated_at TIMESTAMP NOT NULL DEFAULT NOW()`,
		`ALTER TABLE IF EXISTS customer_follow_ups ADD COLUMN IF NOT EXISTS tenant_id BIGINT`,
		`ALTER TABLE IF EXISTS customer_follow_ups ADD COLUMN IF NOT EXISTS scheduled_at TIMESTAMP`,
		`ALTER TABLE IF EXISTS customer_follow_ups ADD COLUMN IF NOT EXISTS completed_at TIMESTAMP`,
		`ALTER TABLE IF EXISTS customer_follow_ups ADD COLUMN IF NOT EXISTS employee_id BIGINT`,
		`ALTER TABLE IF EXISTS customer_follow_ups ADD COLUMN IF NOT EXISTS created_at TIMESTAMP NOT NULL DEFAULT NOW()`,
		`ALTER TABLE IF EXISTS customer_follow_ups ADD COLUMN IF NOT EXISTS updated_at TIMESTAMP NOT NULL DEFAULT NOW()`,

		// Badge lifecycle log compatibility
		`ALTER TABLE IF EXISTS badge_device_lifecycle_logs ADD COLUMN IF NOT EXISTS from_status TEXT`,
		`ALTER TABLE IF EXISTS badge_device_lifecycle_logs ADD COLUMN IF NOT EXISTS to_status TEXT`,
		`ALTER TABLE IF EXISTS badge_device_lifecycle_logs ADD COLUMN IF NOT EXISTS tenant_id BIGINT`,
		`ALTER TABLE IF EXISTS badge_device_lifecycle_logs ADD COLUMN IF NOT EXISTS employee_id BIGINT`,
		`ALTER TABLE IF EXISTS badge_device_lifecycle_logs ADD COLUMN IF NOT EXISTS operator_id BIGINT`,
		`ALTER TABLE IF EXISTS badge_device_lifecycle_logs ADD COLUMN IF NOT EXISTS notes TEXT`,
		`ALTER TABLE IF EXISTS badge_device_lifecycle_logs ADD COLUMN IF NOT EXISTS extra_data JSONB DEFAULT '{}'::jsonb`,

		// Recording module compatibility
		`ALTER TABLE IF EXISTS recordings ADD COLUMN IF NOT EXISTS updated_at TIMESTAMP`,
		`ALTER TABLE IF EXISTS recordings ADD COLUMN IF NOT EXISTS scene TEXT`,
		`ALTER TABLE IF EXISTS recordings ADD COLUMN IF NOT EXISTS notes TEXT`,
		`ALTER TABLE IF EXISTS recordings ADD COLUMN IF NOT EXISTS status TEXT NOT NULL DEFAULT 'pending'`,

		`CREATE TABLE IF NOT EXISTS frontdesk_knowledge_bases (
			id BIGSERIAL PRIMARY KEY,
			tenant_id BIGINT NOT NULL,
			kb_type VARCHAR(30) NOT NULL,
			content JSONB NOT NULL,
			version INTEGER NOT NULL DEFAULT 1,
			status VARCHAR(20) DEFAULT 'active',
			updated_by BIGINT,
			created_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
			updated_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
			UNIQUE(tenant_id, kb_type, version)
		)`,
		`CREATE INDEX IF NOT EXISTS idx_knowledge_bases_tenant_id ON frontdesk_knowledge_bases(tenant_id)`,
		`CREATE INDEX IF NOT EXISTS idx_knowledge_bases_kb_type ON frontdesk_knowledge_bases(kb_type)`,

		`CREATE TABLE IF NOT EXISTS frontdesk_prompt_knowledge_mapping (
			id BIGSERIAL PRIMARY KEY,
			prompt_id VARCHAR(50) NOT NULL,
			kb_type VARCHAR(30) NOT NULL,
			injection_template TEXT,
			UNIQUE(prompt_id, kb_type)
		)`,

		// Analysis routing and audit compatibility
		`CREATE TABLE IF NOT EXISTS callback_inbox (
			id BIGSERIAL PRIMARY KEY,
			vendor VARCHAR(64) NOT NULL,
			source VARCHAR(64) NOT NULL,
			event_type VARCHAR(128) NOT NULL,
			device_no VARCHAR(128),
			event_id VARCHAR(128),
			order_no VARCHAR(128),
			signature_ok BOOLEAN NOT NULL DEFAULT TRUE,
			idempotency_key VARCHAR(256) NOT NULL,
			payload_raw JSONB NOT NULL,
			processed_status VARCHAR(32) NOT NULL DEFAULT 'received',
			error_code VARCHAR(64),
			error_message TEXT,
			trace_id VARCHAR(128),
			recording_id BIGINT,
			created_at TIMESTAMP NOT NULL DEFAULT NOW(),
			updated_at TIMESTAMP NOT NULL DEFAULT NOW()
		)`,
		`CREATE UNIQUE INDEX IF NOT EXISTS idx_callback_inbox_idempotency_key ON callback_inbox (idempotency_key)`,
		`CREATE INDEX IF NOT EXISTS idx_callback_inbox_vendor_created_at ON callback_inbox (vendor, created_at DESC)`,
		`CREATE INDEX IF NOT EXISTS idx_callback_inbox_order_no ON callback_inbox (order_no)`,
		`CREATE INDEX IF NOT EXISTS idx_callback_inbox_recording_id ON callback_inbox (recording_id)`,
		`CREATE TABLE IF NOT EXISTS analysis_pipelines (
			id BIGSERIAL PRIMARY KEY,
			pipeline_code VARCHAR(128) NOT NULL,
			pipeline_version VARCHAR(64) NOT NULL,
			name VARCHAR(255) NOT NULL,
			scene_scope VARCHAR(64) NOT NULL,
			status VARCHAR(32) NOT NULL DEFAULT 'draft',
			description TEXT,
			release_note TEXT,
			is_default_candidate BOOLEAN NOT NULL DEFAULT FALSE,
			created_by BIGINT,
			updated_by BIGINT,
			created_at TIMESTAMP NOT NULL DEFAULT NOW(),
			updated_at TIMESTAMP NOT NULL DEFAULT NOW(),
			deleted_at TIMESTAMP
		)`,
		`CREATE UNIQUE INDEX IF NOT EXISTS idx_analysis_pipelines_code_version ON analysis_pipelines (pipeline_code, pipeline_version)`,
		`CREATE INDEX IF NOT EXISTS idx_analysis_pipelines_status_scope ON analysis_pipelines (status, scene_scope, updated_at DESC)`,
		`ALTER TABLE IF EXISTS analysis_pipelines ADD COLUMN IF NOT EXISTS release_note TEXT`,
		`ALTER TABLE IF EXISTS analysis_pipelines ADD COLUMN IF NOT EXISTS deleted_at TIMESTAMP`,
		`CREATE TABLE IF NOT EXISTS analysis_role_routes (
			id BIGSERIAL PRIMARY KEY,
			tenant_id BIGINT NOT NULL,
			role_id BIGINT NOT NULL,
			role_code_snapshot VARCHAR(128),
			scene_scope VARCHAR(64) NOT NULL,
			pipeline_code VARCHAR(128) NOT NULL,
			pipeline_version VARCHAR(64) NOT NULL,
			enabled BOOLEAN NOT NULL DEFAULT TRUE,
			status VARCHAR(32) NOT NULL DEFAULT 'draft',
			effective_at TIMESTAMP NOT NULL,
			published_at TIMESTAMP,
			published_by BIGINT,
			rolled_back_from_route_id BIGINT,
			notes TEXT,
			created_by BIGINT,
			updated_by BIGINT,
			created_at TIMESTAMP NOT NULL DEFAULT NOW(),
			updated_at TIMESTAMP NOT NULL DEFAULT NOW()
		)`,
		`CREATE UNIQUE INDEX IF NOT EXISTS idx_analysis_role_routes_unique_version ON analysis_role_routes (tenant_id, role_id, scene_scope, effective_at)`,
		`CREATE INDEX IF NOT EXISTS idx_analysis_role_routes_lookup ON analysis_role_routes (tenant_id, role_id, scene_scope, enabled, effective_at DESC)`,
		`ALTER TABLE IF EXISTS analysis_role_routes ADD COLUMN IF NOT EXISTS status VARCHAR(32) NOT NULL DEFAULT 'draft'`,
		`ALTER TABLE IF EXISTS analysis_role_routes ADD COLUMN IF NOT EXISTS published_at TIMESTAMP`,
		`ALTER TABLE IF EXISTS analysis_role_routes ADD COLUMN IF NOT EXISTS published_by BIGINT`,
		`ALTER TABLE IF EXISTS analysis_role_routes ADD COLUMN IF NOT EXISTS rolled_back_from_route_id BIGINT`,
		`ALTER TABLE IF EXISTS analysis_role_routes ADD COLUMN IF NOT EXISTS notes TEXT`,
		`CREATE TABLE IF NOT EXISTS analysis_runs (
			id BIGSERIAL PRIMARY KEY,
			recording_id BIGINT NOT NULL,
			tenant_id BIGINT NOT NULL,
			employee_id BIGINT,
			resolved_role_id BIGINT,
			resolved_role_code VARCHAR(128),
			scene_scope VARCHAR(64),
			pipeline_code VARCHAR(128) NOT NULL,
			pipeline_version VARCHAR(64) NOT NULL,
			status VARCHAR(32) NOT NULL DEFAULT 'queued',
			trigger_source VARCHAR(64) NOT NULL,
			trace_id VARCHAR(128),
			route_id BIGINT,
			snapshot_version VARCHAR(128),
			vendor_recording_id VARCHAR(128),
			route_snapshot JSONB NOT NULL DEFAULT '{}'::jsonb,
			error_code VARCHAR(64),
			error_message TEXT,
			started_at TIMESTAMP,
			ended_at TIMESTAMP,
			created_at TIMESTAMP NOT NULL DEFAULT NOW(),
			updated_at TIMESTAMP NOT NULL DEFAULT NOW()
		)`,
		`CREATE INDEX IF NOT EXISTS idx_analysis_runs_recording_id ON analysis_runs (recording_id, created_at DESC)`,
		`CREATE INDEX IF NOT EXISTS idx_analysis_runs_trace_id ON analysis_runs (trace_id)`,
		`CREATE INDEX IF NOT EXISTS idx_analysis_runs_status ON analysis_runs (status, created_at DESC)`,
		`CREATE INDEX IF NOT EXISTS idx_analysis_runs_pipeline ON analysis_runs (pipeline_code, pipeline_version, created_at DESC)`,
		`ALTER TABLE IF EXISTS analysis_runs ADD COLUMN IF NOT EXISTS route_id BIGINT`,
		`ALTER TABLE IF EXISTS analysis_runs ADD COLUMN IF NOT EXISTS snapshot_version VARCHAR(128)`,
		`ALTER TABLE IF EXISTS analysis_runs ADD COLUMN IF NOT EXISTS vendor_recording_id VARCHAR(128)`,
		`ALTER TABLE IF EXISTS analysis_runs ADD COLUMN IF NOT EXISTS route_snapshot JSONB NOT NULL DEFAULT '{}'::jsonb`,
		`CREATE TABLE IF NOT EXISTS analysis_step_runs (
			id BIGSERIAL PRIMARY KEY,
			run_id BIGINT NOT NULL,
			recording_id BIGINT NOT NULL,
			step_code VARCHAR(128) NOT NULL,
			step_name VARCHAR(255),
			step_type VARCHAR(64) NOT NULL,
			prompt_code VARCHAR(128),
			prompt_version VARCHAR(64),
			status VARCHAR(32) NOT NULL,
			attempt INTEGER NOT NULL DEFAULT 1,
			input_digest TEXT,
			output_digest TEXT,
			tokens_used INTEGER,
			cost NUMERIC(18, 6),
			execution_time_ms INTEGER,
			started_at TIMESTAMP,
			ended_at TIMESTAMP,
			error_code VARCHAR(64),
			error_message TEXT,
			created_at TIMESTAMP NOT NULL DEFAULT NOW(),
			updated_at TIMESTAMP NOT NULL DEFAULT NOW()
		)`,
		`CREATE INDEX IF NOT EXISTS idx_analysis_step_runs_run_id ON analysis_step_runs (run_id, created_at ASC)`,
		`CREATE INDEX IF NOT EXISTS idx_analysis_step_runs_recording_id ON analysis_step_runs (recording_id, created_at DESC)`,
		`CREATE INDEX IF NOT EXISTS idx_analysis_step_runs_prompt_code ON analysis_step_runs (prompt_code, created_at DESC)`,
		`ALTER TABLE IF EXISTS analysis_step_runs ADD COLUMN IF NOT EXISTS step_name VARCHAR(255)`,
		`ALTER TABLE IF EXISTS analysis_step_runs ADD COLUMN IF NOT EXISTS started_at TIMESTAMP`,
		`ALTER TABLE IF EXISTS analysis_step_runs ADD COLUMN IF NOT EXISTS ended_at TIMESTAMP`,
		`ALTER TABLE IF EXISTS recordings ADD COLUMN IF NOT EXISTS resolved_role_id BIGINT`,
		`ALTER TABLE IF EXISTS recordings ADD COLUMN IF NOT EXISTS resolved_role_code VARCHAR(128)`,
		`ALTER TABLE IF EXISTS recordings ADD COLUMN IF NOT EXISTS resolved_scene_scope VARCHAR(64)`,
		`ALTER TABLE IF EXISTS recordings ADD COLUMN IF NOT EXISTS resolved_pipeline_code VARCHAR(128)`,
		`ALTER TABLE IF EXISTS recordings ADD COLUMN IF NOT EXISTS resolved_pipeline_version VARCHAR(64)`,
		`ALTER TABLE IF EXISTS recordings ADD COLUMN IF NOT EXISTS analysis_trace_id VARCHAR(128)`,
		`CREATE INDEX IF NOT EXISTS idx_recordings_resolved_pipeline ON recordings (resolved_pipeline_code, resolved_pipeline_version)`,
		`CREATE INDEX IF NOT EXISTS idx_recordings_analysis_trace_id ON recordings (analysis_trace_id)`,
		`ALTER TABLE IF EXISTS recording_analysis_results ADD COLUMN IF NOT EXISTS run_id BIGINT`,
		`ALTER TABLE IF EXISTS recording_analysis_results ADD COLUMN IF NOT EXISTS step_run_id BIGINT`,
		`ALTER TABLE IF EXISTS recording_analysis_results ADD COLUMN IF NOT EXISTS pipeline_code VARCHAR(128)`,
		`ALTER TABLE IF EXISTS recording_analysis_results ADD COLUMN IF NOT EXISTS pipeline_version VARCHAR(64)`,
		`ALTER TABLE IF EXISTS recording_analysis_results ADD COLUMN IF NOT EXISTS is_active BOOLEAN NOT NULL DEFAULT FALSE`,
		`CREATE INDEX IF NOT EXISTS idx_recording_analysis_results_run_id ON recording_analysis_results (run_id, created_at DESC)`,
		`CREATE INDEX IF NOT EXISTS idx_recording_analysis_results_active ON recording_analysis_results (recording_id, is_active, created_at DESC)`,
		`CREATE TABLE IF NOT EXISTS trial_demo_recording_assets (
			id BIGSERIAL PRIMARY KEY,
			template_code TEXT NOT NULL,
			asset_code TEXT NOT NULL,
			role_code TEXT NOT NULL,
			title TEXT NOT NULL,
			source_tenant_id BIGINT NOT NULL,
			source_recording_id BIGINT NOT NULL,
			source_customer_id BIGINT,
			version TEXT NOT NULL DEFAULT 'v1',
			is_active BOOLEAN NOT NULL DEFAULT TRUE,
			sort_order INT NOT NULL DEFAULT 0,
			metadata JSONB NOT NULL DEFAULT '{}'::jsonb,
			created_by BIGINT,
			created_at TIMESTAMP NOT NULL DEFAULT NOW(),
			updated_at TIMESTAMP NOT NULL DEFAULT NOW()
		)`,
		`CREATE UNIQUE INDEX IF NOT EXISTS uk_trial_demo_recording_assets_template_asset ON trial_demo_recording_assets(template_code, asset_code)`,
		`CREATE UNIQUE INDEX IF NOT EXISTS uk_trial_demo_recording_assets_template_role_version ON trial_demo_recording_assets(template_code, role_code, version)`,
		`CREATE INDEX IF NOT EXISTS idx_trial_demo_recording_assets_active ON trial_demo_recording_assets(template_code, is_active, sort_order, id)`,
		`CREATE TABLE IF NOT EXISTS tenant_trial_demo_recordings (
			id BIGSERIAL PRIMARY KEY,
			tenant_id BIGINT NOT NULL,
			template_code TEXT NOT NULL,
			role_code TEXT NOT NULL,
			asset_id BIGINT NOT NULL REFERENCES trial_demo_recording_assets(id),
			recording_id BIGINT NOT NULL,
			employee_id BIGINT NOT NULL,
			customer_id BIGINT,
			title TEXT NOT NULL,
			viewed_at TIMESTAMP,
			viewed_by_employee_id BIGINT,
			created_at TIMESTAMP NOT NULL DEFAULT NOW()
		)`,
		`ALTER TABLE IF EXISTS tenant_trial_demo_recordings ADD COLUMN IF NOT EXISTS viewed_at TIMESTAMP`,
		`ALTER TABLE IF EXISTS tenant_trial_demo_recordings ADD COLUMN IF NOT EXISTS viewed_by_employee_id BIGINT`,
		`CREATE UNIQUE INDEX IF NOT EXISTS uk_tenant_trial_demo_recordings_tenant_template_role ON tenant_trial_demo_recordings(tenant_id, template_code, role_code)`,
		`CREATE INDEX IF NOT EXISTS idx_tenant_trial_demo_recordings_tenant ON tenant_trial_demo_recordings(tenant_id, created_at DESC)`,
		`CREATE TABLE IF NOT EXISTS employee_login_events (
			id BIGSERIAL PRIMARY KEY,
			tenant_id BIGINT NOT NULL,
			employee_id BIGINT NOT NULL,
			login_at TIMESTAMP NOT NULL DEFAULT NOW(),
			login_source TEXT NOT NULL DEFAULT 'institution_web',
			ip TEXT NOT NULL DEFAULT '',
			user_agent TEXT NOT NULL DEFAULT '',
			created_at TIMESTAMP NOT NULL DEFAULT NOW()
		)`,
		`CREATE INDEX IF NOT EXISTS idx_employee_login_events_tenant_login_at ON employee_login_events(tenant_id, login_at DESC)`,
		`CREATE INDEX IF NOT EXISTS idx_employee_login_events_employee_login_at ON employee_login_events(employee_id, login_at DESC)`,
		`CREATE TABLE IF NOT EXISTS trial_customer_assignments (
			id BIGSERIAL PRIMARY KEY,
			tenant_id BIGINT NOT NULL,
			sales_owner_admin_id BIGINT,
			sales_owner_name_snapshot TEXT NOT NULL DEFAULT '',
			source TEXT NOT NULL DEFAULT '',
			notes TEXT NOT NULL DEFAULT '',
			assigned_at TIMESTAMP NOT NULL DEFAULT NOW(),
			assigned_by BIGINT,
			updated_at TIMESTAMP NOT NULL DEFAULT NOW(),
			UNIQUE(tenant_id)
		)`,
		`ALTER TABLE IF EXISTS trial_customer_assignments ADD COLUMN IF NOT EXISTS source TEXT NOT NULL DEFAULT ''`,
		`ALTER TABLE IF EXISTS trial_customer_assignments ADD COLUMN IF NOT EXISTS notes TEXT NOT NULL DEFAULT ''`,
		`CREATE INDEX IF NOT EXISTS idx_trial_customer_assignments_owner ON trial_customer_assignments(sales_owner_admin_id, updated_at DESC)`,
		`CREATE TABLE IF NOT EXISTS trial_customer_ownerships (
			tenant_id BIGINT PRIMARY KEY,
			owner_org_id BIGINT,
			sales_owner_user_id BIGINT,
			created_source TEXT NOT NULL DEFAULT '',
			assigned_at TIMESTAMP NOT NULL DEFAULT NOW(),
			assigned_by BIGINT,
			updated_at TIMESTAMP NOT NULL DEFAULT NOW()
		)`,
		`ALTER TABLE IF EXISTS trial_customer_ownerships ALTER COLUMN owner_org_id DROP NOT NULL`,
		`CREATE INDEX IF NOT EXISTS idx_trial_customer_ownerships_owner_org ON trial_customer_ownerships(owner_org_id, updated_at DESC)`,
		`CREATE INDEX IF NOT EXISTS idx_trial_customer_ownerships_sales_owner ON trial_customer_ownerships(sales_owner_user_id, updated_at DESC)`,
		`CREATE TABLE IF NOT EXISTS trial_customer_metrics (
			tenant_id BIGINT PRIMARY KEY,
			trial_started_at TIMESTAMP,
			trial_expires_at TIMESTAMP,
			first_login_at TIMESTAMP,
			last_login_at TIMESTAMP,
			login_count INTEGER NOT NULL DEFAULT 0,
			doctor_demo_viewed_at TIMESTAMP,
			consultant_demo_viewed_at TIMESTAMP,
			first_upload_at TIMESTAMP,
			last_upload_at TIMESTAMP,
			upload_count INTEGER NOT NULL DEFAULT 0,
			analysis_count INTEGER NOT NULL DEFAULT 0,
			doctor_upload_count INTEGER NOT NULL DEFAULT 0,
			consultant_upload_count INTEGER NOT NULL DEFAULT 0,
			generated_customer_count INTEGER NOT NULL DEFAULT 0,
			generated_task_count INTEGER NOT NULL DEFAULT 0,
			generated_content_count INTEGER NOT NULL DEFAULT 0,
			wechat_content_count INTEGER NOT NULL DEFAULT 0,
			xiaohongshu_content_count INTEGER NOT NULL DEFAULT 0,
			video_script_content_count INTEGER NOT NULL DEFAULT 0,
			last_activity_at TIMESTAMP,
			current_stage TEXT NOT NULL DEFAULT 'not_started',
			priority_level TEXT NOT NULL DEFAULT 'normal',
			blocking_reason TEXT NOT NULL DEFAULT '',
			next_action_hint TEXT NOT NULL DEFAULT '',
			updated_at TIMESTAMP NOT NULL DEFAULT NOW()
		)`,
		`ALTER TABLE IF EXISTS trial_customer_metrics ADD COLUMN IF NOT EXISTS generated_content_count INTEGER NOT NULL DEFAULT 0`,
		`ALTER TABLE IF EXISTS trial_customer_metrics ADD COLUMN IF NOT EXISTS wechat_content_count INTEGER NOT NULL DEFAULT 0`,
		`ALTER TABLE IF EXISTS trial_customer_metrics ADD COLUMN IF NOT EXISTS xiaohongshu_content_count INTEGER NOT NULL DEFAULT 0`,
		`ALTER TABLE IF EXISTS trial_customer_metrics ADD COLUMN IF NOT EXISTS video_script_content_count INTEGER NOT NULL DEFAULT 0`,
		`CREATE INDEX IF NOT EXISTS idx_trial_customer_metrics_stage ON trial_customer_metrics(current_stage, updated_at DESC)`,
		`CREATE INDEX IF NOT EXISTS idx_trial_customer_metrics_last_activity ON trial_customer_metrics(last_activity_at DESC)`,
		`CREATE TABLE IF NOT EXISTS trial_customer_follow_ups (
			id BIGSERIAL PRIMARY KEY,
			tenant_id BIGINT NOT NULL,
			sales_owner_admin_id BIGINT,
			follow_up_type TEXT NOT NULL DEFAULT '',
			summary TEXT NOT NULL DEFAULT '',
			result TEXT NOT NULL DEFAULT '',
			next_follow_up_at TIMESTAMP,
			created_at TIMESTAMP NOT NULL DEFAULT NOW(),
			created_by BIGINT
		)`,
		`CREATE INDEX IF NOT EXISTS idx_trial_customer_follow_ups_tenant_created_at ON trial_customer_follow_ups(tenant_id, created_at DESC)`,
		`CREATE TABLE IF NOT EXISTS management_events (
			id BIGSERIAL PRIMARY KEY,
			tenant_id BIGINT NOT NULL,
			event_date DATE NOT NULL,
			event_type VARCHAR(50) NOT NULL,
			title VARCHAR(255) NOT NULL,
			description TEXT,
			role_type VARCHAR(32) NOT NULL,
			dimension_code VARCHAR(32) NOT NULL,
			status VARCHAR(20) NOT NULL DEFAULT 'active',
			meta JSONB NOT NULL DEFAULT '{}'::jsonb,
			created_by BIGINT NOT NULL DEFAULT 0,
			created_at TIMESTAMP NOT NULL DEFAULT NOW(),
			updated_at TIMESTAMP NOT NULL DEFAULT NOW()
		)`,
		`CREATE INDEX IF NOT EXISTS idx_management_events_tenant_role_dim_date ON management_events (tenant_id, role_type, dimension_code, event_date DESC)`,
		`CREATE INDEX IF NOT EXISTS idx_management_events_tenant_status_date ON management_events (tenant_id, status, event_date DESC)`,
		`CREATE TABLE IF NOT EXISTS benchmark_clips (
			id BIGSERIAL PRIMARY KEY,
			tenant_id BIGINT NOT NULL,
			recording_id BIGINT NOT NULL,
			employee_id BIGINT NOT NULL,
			role_code VARCHAR(50) NOT NULL,
			dimension VARCHAR(100) NOT NULL,
			score DECIMAL(5,2),
			clip_text TEXT NOT NULL,
			ai_comment TEXT,
			learning_points JSONB NOT NULL DEFAULT '[]'::jsonb,
			source VARCHAR(20) NOT NULL,
			status VARCHAR(20) NOT NULL,
			confidence VARCHAR(20),
			audio_start_seconds INTEGER,
			audio_end_seconds INTEGER,
			used_in_meetings INTEGER NOT NULL DEFAULT 0,
			accepted_at TIMESTAMP,
			rejected_at TIMESTAMP,
			created_at TIMESTAMP NOT NULL DEFAULT NOW(),
			updated_at TIMESTAMP NOT NULL DEFAULT NOW()
		)`,
		`CREATE UNIQUE INDEX IF NOT EXISTS uk_benchmark_clips_tenant_recording_role_dim ON benchmark_clips(tenant_id, recording_id, role_code, dimension)`,
		`CREATE INDEX IF NOT EXISTS idx_benchmark_clips_status ON benchmark_clips(tenant_id, status)`,
		`CREATE INDEX IF NOT EXISTS idx_benchmark_clips_role_dim ON benchmark_clips(tenant_id, role_code, dimension)`,
		`CREATE TABLE IF NOT EXISTS benchmark_clip_pushes (
			id BIGSERIAL PRIMARY KEY,
			tenant_id BIGINT NOT NULL,
			benchmark_clip_id BIGINT NOT NULL,
			target_employee_id BIGINT NOT NULL,
			target_employee_name TEXT,
			note TEXT,
			status VARCHAR(20) NOT NULL DEFAULT 'sent',
			pushed_by BIGINT NOT NULL DEFAULT 0,
			pushed_at TIMESTAMP NOT NULL DEFAULT NOW(),
			acknowledged_at TIMESTAMP,
			created_at TIMESTAMP NOT NULL DEFAULT NOW(),
			updated_at TIMESTAMP NOT NULL DEFAULT NOW()
		)`,
		`CREATE INDEX IF NOT EXISTS idx_benchmark_clip_pushes_clip ON benchmark_clip_pushes(tenant_id, benchmark_clip_id, created_at DESC)`,
		`CREATE INDEX IF NOT EXISTS idx_benchmark_clip_pushes_target ON benchmark_clip_pushes(tenant_id, target_employee_id, status, created_at DESC)`,
		`ALTER TABLE IF EXISTS benchmark_clip_pushes ADD COLUMN IF NOT EXISTS status VARCHAR(20)`,
		`ALTER TABLE IF EXISTS benchmark_clip_pushes ALTER COLUMN status SET DEFAULT 'sent'`,
		`UPDATE benchmark_clip_pushes SET status = 'sent' WHERE status IS NULL OR status = ''`,
		`ALTER TABLE IF EXISTS benchmark_clip_pushes ADD COLUMN IF NOT EXISTS acknowledged_at TIMESTAMP`,
		`CREATE INDEX IF NOT EXISTS idx_pushes_employee_status ON benchmark_clip_pushes(target_employee_id, status, pushed_at DESC)`,
		`CREATE TABLE IF NOT EXISTS management_risk_handled (
			id BIGSERIAL PRIMARY KEY,
			tenant_id BIGINT NOT NULL,
			risk_id VARCHAR(255) NOT NULL,
			handled_by BIGINT NOT NULL DEFAULT 0,
			handled_at TIMESTAMP NOT NULL DEFAULT NOW(),
			created_at TIMESTAMP NOT NULL DEFAULT NOW(),
			updated_at TIMESTAMP NOT NULL DEFAULT NOW(),
			UNIQUE (tenant_id, risk_id)
		)`,
		`CREATE INDEX IF NOT EXISTS idx_management_risk_handled_tenant_time ON management_risk_handled(tenant_id, handled_at DESC)`,
		`CREATE TABLE IF NOT EXISTS morning_meeting_materials (
			id BIGSERIAL PRIMARY KEY,
			tenant_id BIGINT NOT NULL,
			role_code VARCHAR(32) NOT NULL,
			meeting_date DATE NOT NULL,
			review_date DATE NOT NULL,
			recording_id BIGINT NOT NULL,
			employee_id BIGINT NOT NULL DEFAULT 0,
			employee_name TEXT,
			scene_name TEXT,
			selection_reason TEXT,
			overall_score DOUBLE PRECISION NOT NULL DEFAULT 0,
			overall_score_text VARCHAR(64),
			primary_dimension VARCHAR(64),
			primary_score DOUBLE PRECISION NOT NULL DEFAULT 0,
			primary_score_text VARCHAR(64),
			evidence JSONB NOT NULL DEFAULT '[]'::jsonb,
			diagnosis TEXT,
			coaching_script TEXT,
			generation_method VARCHAR(16) NOT NULL DEFAULT 'rule',
			generation_status VARCHAR(16) NOT NULL DEFAULT 'success',
			fallback_reason TEXT,
			prompt_code VARCHAR(128),
			model_code VARCHAR(128),
			llm_request_id VARCHAR(128),
			used_at TIMESTAMP,
			used_by BIGINT,
			is_edited BOOLEAN NOT NULL DEFAULT false,
			created_at TIMESTAMP NOT NULL DEFAULT NOW(),
			updated_at TIMESTAMP NOT NULL DEFAULT NOW(),
			UNIQUE (tenant_id, role_code, meeting_date)
		)`,
		`CREATE INDEX IF NOT EXISTS idx_morning_meeting_materials_lookup ON morning_meeting_materials(tenant_id, role_code, meeting_date DESC)`,
		`ALTER TABLE IF EXISTS morning_meeting_materials ADD COLUMN IF NOT EXISTS used_at TIMESTAMP`,
		`ALTER TABLE IF EXISTS morning_meeting_materials ADD COLUMN IF NOT EXISTS used_by BIGINT`,

		// WeCom third-party integration
		`CREATE TABLE IF NOT EXISTS wecom_suite_tickets (
			id BIGSERIAL PRIMARY KEY,
			suite_id TEXT NOT NULL,
			suite_ticket TEXT NOT NULL,
			created_at TIMESTAMP NOT NULL DEFAULT NOW()
		)`,
		`CREATE INDEX IF NOT EXISTS idx_wecom_suite_tickets_suite_created_at ON wecom_suite_tickets(suite_id, created_at DESC)`,
		`CREATE TABLE IF NOT EXISTS wecom_corp_installs (
			corp_id TEXT PRIMARY KEY,
			corp_name TEXT,
			permanent_code TEXT NOT NULL,
			agent_id BIGINT NOT NULL DEFAULT 0,
			status TEXT NOT NULL DEFAULT 'active',
			created_at TIMESTAMP NOT NULL DEFAULT NOW(),
			updated_at TIMESTAMP NOT NULL DEFAULT NOW(),
			cancelled_at TIMESTAMP
		)`,
		`CREATE INDEX IF NOT EXISTS idx_wecom_corp_installs_status ON wecom_corp_installs(status, updated_at DESC)`,
		`CREATE TABLE IF NOT EXISTS wecom_user_bindings (
			id BIGSERIAL PRIMARY KEY,
			corp_id TEXT NOT NULL,
			wecom_user_id TEXT NOT NULL,
			employee_id BIGINT NOT NULL,
			tenant_id BIGINT NOT NULL,
			source TEXT NOT NULL DEFAULT 'manual',
			created_at TIMESTAMP NOT NULL DEFAULT NOW(),
			updated_at TIMESTAMP NOT NULL DEFAULT NOW(),
			UNIQUE (corp_id, wecom_user_id)
		)`,
		`CREATE INDEX IF NOT EXISTS idx_wecom_user_bindings_employee_id ON wecom_user_bindings(employee_id)`,
		`CREATE TABLE IF NOT EXISTS wecom_event_logs (
			id BIGSERIAL PRIMARY KEY,
			corp_id TEXT,
			info_type TEXT NOT NULL,
			raw_payload TEXT NOT NULL,
			created_at TIMESTAMP NOT NULL DEFAULT NOW()
		)`,
		`CREATE INDEX IF NOT EXISTS idx_wecom_event_logs_created_at ON wecom_event_logs(created_at DESC)`,
		`CREATE TABLE IF NOT EXISTS wecom_message_logs (
			id BIGSERIAL PRIMARY KEY,
			corp_id TEXT,
			tenant_id BIGINT NOT NULL DEFAULT 0,
			employee_id BIGINT NOT NULL DEFAULT 0,
			wecom_user_id TEXT,
			message_scene TEXT NOT NULL,
			dedupe_key TEXT NOT NULL,
			title TEXT NOT NULL,
			content TEXT NOT NULL,
			target_url TEXT,
			status TEXT NOT NULL DEFAULT 'pending',
			error_message TEXT,
			request_payload JSONB NOT NULL DEFAULT '{}'::jsonb,
			response_payload JSONB NOT NULL DEFAULT '{}'::jsonb,
			biz_date DATE,
			created_at TIMESTAMP NOT NULL DEFAULT NOW(),
			updated_at TIMESTAMP NOT NULL DEFAULT NOW()
		)`,
		`CREATE UNIQUE INDEX IF NOT EXISTS uk_wecom_message_logs_dedupe_key ON wecom_message_logs(dedupe_key)`,
		`CREATE INDEX IF NOT EXISTS idx_wecom_message_logs_employee_created_at ON wecom_message_logs(employee_id, created_at DESC)`,
	}

	for i, stmt := range stmts {
		if _, err := pool.Exec(ctx, stmt); err != nil {
			return fmt.Errorf("compat migration failed at step %d: %w", i+1, err)
		}
	}

	if err := seedBuiltinAnalysisPipelines(ctx, pool); err != nil {
		return fmt.Errorf("compat migration seed builtin analysis pipelines: %w", err)
	}
	if err := seedDefaultAnalysisRoutes(ctx, pool); err != nil {
		return fmt.Errorf("compat migration seed default analysis routes: %w", err)
	}
	if err := seedBenchmarkReviewPrompt(ctx, pool); err != nil {
		return fmt.Errorf("compat migration seed benchmark review prompt: %w", err)
	}
	if err := seedBenchmarkCommentModelConfig(ctx, pool); err != nil {
		return fmt.Errorf("compat migration seed benchmark model config: %w", err)
	}
	if err := seedMorningMeetingPrompt(ctx, pool); err != nil {
		return fmt.Errorf("compat migration seed morning meeting prompt: %w", err)
	}
	if err := seedMorningMeetingModelConfig(ctx, pool); err != nil {
		return fmt.Errorf("compat migration seed morning meeting model config: %w", err)
	}
	if err := ensureProductLibrarySchema(ctx, pool); err != nil {
		return fmt.Errorf("compat migration ensure product library schema: %w", err)
	}
	if err := seedTenantProductCatalog(ctx, pool); err != nil {
		return fmt.Errorf("compat migration seed tenant product catalog: %w", err)
	}
	if err := backfillTrialCustomerOrgOwnership(ctx, pool); err != nil {
		return fmt.Errorf("compat migration backfill trial customer org ownership: %w", err)
	}
	if err := normalizeOperationsMenus(ctx, pool); err != nil {
		return fmt.Errorf("compat migration normalize operations menus: %w", err)
	}
	if err := ensureOperationsNavigationMenus(ctx, pool); err != nil {
		return fmt.Errorf("compat migration ensure operations navigation menus: %w", err)
	}

	slog.Info("compatibility migrations applied", "steps", len(stmts))
	return nil
}

func backfillTrialCustomerOrgOwnership(ctx context.Context, pool *pgxpool.Pool) error {
	// 不再在兼容迁移阶段自动创建组织、绑定管理员或回填组织归属。
	// 组织及绑定关系由产品界面显式维护，避免系统替用户做隐式决策。
	_ = ctx
	_ = pool
	return nil
}

func normalizeOperationsMenus(ctx context.Context, pool *pgxpool.Pool) error {
	type menuNormalization struct {
		path        string
		canonicalID int64
		legacyIDs   []int64
	}

	targets := []menuNormalization{
		{path: "/content/library", canonicalID: 125},
		{path: "/content/topics", canonicalID: 126},
		{path: "/customers", canonicalID: 50},
		{path: "/llm/cost", canonicalID: 35},
		{path: "/llm/models", canonicalID: 33},
		{path: "/llm/records", canonicalID: 34},
		{path: "/recordings", canonicalID: 46, legacyIDs: []int64{150}},
		{path: "/recordings/dashboard", canonicalID: 141, legacyIDs: []int64{151}},
		{path: "/recordings/stats", canonicalID: 47, legacyIDs: []int64{152}},
		{path: "/recordings/team-ability", canonicalID: 142, legacyIDs: []int64{153}},
		{path: "/subscription-plans", canonicalID: 144},
		{path: "/tenants", canonicalID: 2},
	}

	tx, err := pool.Begin(ctx)
	if err != nil {
		return fmt.Errorf("begin operations menu normalization tx: %w", err)
	}
	defer tx.Rollback(ctx)

	for _, target := range targets {
		var canonicalExists bool
		if err := tx.QueryRow(ctx, `
			SELECT EXISTS (
				SELECT 1
				FROM operations_menus
				WHERE id = $1
				  AND deleted_at IS NULL
				  AND path = $2
			)
		`, target.canonicalID, target.path).Scan(&canonicalExists); err != nil {
			return fmt.Errorf("check canonical menu %s(%d): %w", target.path, target.canonicalID, err)
		}
		if !canonicalExists {
			slog.Warn("skip operations menu normalization because canonical menu is missing", "path", target.path, "canonical_id", target.canonicalID)
			continue
		}

		legacyIDs := target.legacyIDs
		if len(legacyIDs) == 0 {
			rows, err := tx.Query(ctx, `
				SELECT id
				FROM operations_menus
				WHERE deleted_at IS NULL
				  AND path = $1
				  AND id <> $2
				ORDER BY id
			`, target.path, target.canonicalID)
			if err != nil {
				return fmt.Errorf("list duplicate menus for %s: %w", target.path, err)
			}
			for rows.Next() {
				var id int64
				if err := rows.Scan(&id); err != nil {
					rows.Close()
					return fmt.Errorf("scan duplicate menu for %s: %w", target.path, err)
				}
				legacyIDs = append(legacyIDs, id)
			}
			if err := rows.Err(); err != nil {
				rows.Close()
				return fmt.Errorf("iterate duplicate menus for %s: %w", target.path, err)
			}
			rows.Close()
		}

		for _, legacyID := range legacyIDs {
			if legacyID == target.canonicalID {
				continue
			}

			var legacyExists bool
			if err := tx.QueryRow(ctx, `
				SELECT EXISTS (
					SELECT 1
					FROM operations_menus
					WHERE id = $1
					  AND deleted_at IS NULL
					  AND path = $2
				)
			`, legacyID, target.path).Scan(&legacyExists); err != nil {
				return fmt.Errorf("check legacy menu %s(%d): %w", target.path, legacyID, err)
			}
			if !legacyExists {
				continue
			}

			if _, err := tx.Exec(ctx, `
				INSERT INTO operations_role_menus (role_id, menu_id, created_at)
				SELECT rm.role_id, $2, NOW()
				FROM operations_role_menus rm
				WHERE rm.menu_id = $1
				  AND NOT EXISTS (
					SELECT 1
					FROM operations_role_menus existing
					WHERE existing.role_id = rm.role_id
					  AND existing.menu_id = $2
				  )
			`, legacyID, target.canonicalID); err != nil {
				return fmt.Errorf("migrate role menus from %d to %d: %w", legacyID, target.canonicalID, err)
			}

			if _, err := tx.Exec(ctx, `DELETE FROM operations_role_menus WHERE menu_id = $1`, legacyID); err != nil {
				return fmt.Errorf("delete legacy role menus for %d: %w", legacyID, err)
			}

			if _, err := tx.Exec(ctx, `
				UPDATE operations_menus
				SET deleted_at = COALESCE(deleted_at, NOW()),
				    updated_at = NOW()
				WHERE id = $1
				  AND deleted_at IS NULL
			`, legacyID); err != nil {
				return fmt.Errorf("soft delete legacy menu %d: %w", legacyID, err)
			}
		}
	}

	if err := tx.Commit(ctx); err != nil {
		return fmt.Errorf("commit operations menu normalization: %w", err)
	}
	return nil
}

func ensureOperationsNavigationMenus(ctx context.Context, pool *pgxpool.Pool) error {
	_ = ctx
	_ = pool
	return nil
}

func ensureProductLibrarySchema(ctx context.Context, pool *pgxpool.Pool) error {
	statements := []string{
		`DO $$
		BEGIN
			IF EXISTS (
				SELECT 1
				FROM information_schema.tables
				WHERE table_schema = 'public' AND table_name = 'products'
			) AND NOT EXISTS (
				SELECT 1
				FROM information_schema.tables
				WHERE table_schema = 'public' AND table_name = 'legacy_products'
			) THEN
				ALTER TABLE products RENAME TO legacy_products;
			END IF;
		END $$`,
		`DO $$
		BEGIN
			IF EXISTS (
				SELECT 1 FROM pg_class WHERE relname = 'products_id_seq'
			) AND NOT EXISTS (
				SELECT 1 FROM pg_class WHERE relname = 'legacy_products_id_seq'
			) THEN
				ALTER SEQUENCE products_id_seq RENAME TO legacy_products_id_seq;
			END IF;
		END $$`,
		`DO $$
		BEGIN
			IF EXISTS (
				SELECT 1
				FROM information_schema.columns
				WHERE table_name = 'legacy_products' AND column_name = 'id'
			) THEN
				ALTER TABLE legacy_products ALTER COLUMN id SET DEFAULT nextval('legacy_products_id_seq'::regclass);
			END IF;
		END $$`,
		`DO $$
		BEGIN
			IF EXISTS (
				SELECT 1 FROM pg_constraint WHERE conname = 'product_recommendations_product_id_fkey'
			) THEN
				ALTER TABLE product_recommendations DROP CONSTRAINT product_recommendations_product_id_fkey;
			END IF;
			IF EXISTS (
				SELECT 1 FROM information_schema.tables WHERE table_name = 'legacy_products'
			) THEN
				ALTER TABLE product_recommendations
					ADD CONSTRAINT product_recommendations_product_id_fkey
					FOREIGN KEY (product_id) REFERENCES legacy_products(id);
			END IF;
		END $$`,
		`DO $$
		BEGIN
			IF EXISTS (
				SELECT 1 FROM pg_constraint WHERE conname = 'product_sales_product_id_fkey'
			) THEN
				ALTER TABLE product_sales DROP CONSTRAINT product_sales_product_id_fkey;
			END IF;
			IF EXISTS (
				SELECT 1 FROM information_schema.tables WHERE table_name = 'legacy_products'
			) THEN
				ALTER TABLE product_sales
					ADD CONSTRAINT product_sales_product_id_fkey
					FOREIGN KEY (product_id) REFERENCES legacy_products(id);
			END IF;
		END $$`,
		`CREATE TABLE IF NOT EXISTS products (
			id BIGSERIAL PRIMARY KEY,
			tenant_id BIGINT NOT NULL,
			industry TEXT NOT NULL DEFAULT '',
			name TEXT NOT NULL,
			aliases JSONB NOT NULL DEFAULT '[]'::jsonb,
			price NUMERIC(12,2) NOT NULL DEFAULT 0,
			applicable TEXT NOT NULL DEFAULT '',
			selling_point TEXT NOT NULL DEFAULT '',
			upgrade_to TEXT NOT NULL DEFAULT '',
			combine_with TEXT NOT NULL DEFAULT '',
			status TEXT NOT NULL DEFAULT 'active',
			is_template BOOLEAN NOT NULL DEFAULT FALSE,
			created_at TIMESTAMP NOT NULL DEFAULT NOW(),
			updated_at TIMESTAMP NOT NULL DEFAULT NOW()
		)`,
		`CREATE INDEX IF NOT EXISTS idx_products_tenant_id ON products(tenant_id)`,
		`CREATE INDEX IF NOT EXISTS idx_products_industry ON products(industry)`,
		`CREATE INDEX IF NOT EXISTS idx_products_status ON products(status)`,
		`CREATE INDEX IF NOT EXISTS idx_products_is_template ON products(is_template)`,
		`CREATE UNIQUE INDEX IF NOT EXISTS uk_products_tenant_name_template ON products(tenant_id, lower(name), is_template)`,
	}

	for i, stmt := range statements {
		if _, err := pool.Exec(ctx, stmt); err != nil {
			return fmt.Errorf("product library migration failed at step %d: %w", i+1, err)
		}
	}
	return nil
}

func seedTenantProductCatalog(ctx context.Context, pool *pgxpool.Pool) error {
	seedSQL := `
	WITH seed_rows AS (
		SELECT *
		FROM (
			VALUES
				(4::BIGINT, '屈光'::TEXT, '全飞秒近视手术'::TEXT, '["全飞","普通全飞","smile","全飞秒"]'::jsonb, 25800::numeric, '角膜条件合适、希望切口更小的近视/散光患者'::TEXT, '切口小、恢复快、主流术式'::TEXT, '散光>100度或追求更高视觉质量时建议升全飞Pro'::TEXT, '伴睑板腺堵塞或干眼时可联合干眼OPT'::TEXT, 'active'::TEXT, FALSE),
				(4::BIGINT, '屈光'::TEXT, '半飞秒近视手术'::TEXT, '["半飞","半飞秒","lasik"]'::jsonb, 19800::numeric, '角膜条件更适合角膜瓣方案或预算更敏感人群'::TEXT, '适应范围广、角膜利用率更灵活'::TEXT, '若散光高、追求视觉质量可升级全飞Pro或晶体方案'::TEXT, '伴干眼症状时可联合干眼OPT'::TEXT, 'active'::TEXT, FALSE),
				(4::BIGINT, '屈光'::TEXT, '全飞秒Pro近视手术'::TEXT, '["全飞pro","全飞PRO","全飞秒pro","全飞秒PRO","pro","9秒的"]'::jsonb, 33800::numeric, '散光较高、追求更高精准度与视觉质量的人群'::TEXT, '9秒激光、旋转补偿、中心定位更精准'::TEXT, ''::TEXT, '伴干眼或睑板腺问题可联合干眼OPT'::TEXT, 'active'::TEXT, FALSE),
				(4::BIGINT, '屈光'::TEXT, 'ICL晶体植入术'::TEXT, '["晶体","icl","晶体植入","icl晶体"]'::jsonb, 36000::numeric, '高度近视、角膜偏薄或不适合角膜切削者'::TEXT, '不切削角膜、适合高度近视'::TEXT, ''::TEXT, '术前干眼管理可联合干眼OPT'::TEXT, 'active'::TEXT, FALSE),
				(4::BIGINT, '屈光'::TEXT, '干眼OPT治疗'::TEXT, '["opt","干眼opt","睑板腺疏通","opt治疗"]'::jsonb, 980::numeric, '伴睑板腺堵塞、蒸发型干眼、术前需改善眼表者'::TEXT, '改善眼表状态、帮助术前准备和术后体验'::TEXT, ''::TEXT, ''::TEXT, 'active'::TEXT, FALSE),
				(0::BIGINT, '屈光'::TEXT, '全飞秒近视手术'::TEXT, '["全飞","普通全飞","smile","全飞秒"]'::jsonb, 25800::numeric, '角膜条件合适、希望切口更小的近视/散光患者'::TEXT, '切口小、恢复快、主流术式'::TEXT, '散光>100度或追求更高视觉质量时建议升全飞Pro'::TEXT, '伴睑板腺堵塞或干眼时可联合干眼OPT'::TEXT, 'active'::TEXT, TRUE),
				(0::BIGINT, '屈光'::TEXT, '半飞秒近视手术'::TEXT, '["半飞","半飞秒","lasik"]'::jsonb, 19800::numeric, '角膜条件更适合角膜瓣方案或预算更敏感人群'::TEXT, '适应范围广、角膜利用率更灵活'::TEXT, '若散光高、追求视觉质量可升级全飞Pro或晶体方案'::TEXT, '伴干眼症状时可联合干眼OPT'::TEXT, 'active'::TEXT, TRUE),
				(0::BIGINT, '屈光'::TEXT, '全飞秒Pro近视手术'::TEXT, '["全飞pro","全飞PRO","全飞秒pro","全飞秒PRO","pro","9秒的"]'::jsonb, 33800::numeric, '散光较高、追求更高精准度与视觉质量的人群'::TEXT, '9秒激光、旋转补偿、中心定位更精准'::TEXT, ''::TEXT, '伴干眼或睑板腺问题可联合干眼OPT'::TEXT, 'active'::TEXT, TRUE),
				(0::BIGINT, '屈光'::TEXT, 'ICL晶体植入术'::TEXT, '["晶体","icl","晶体植入","icl晶体"]'::jsonb, 36000::numeric, '高度近视、角膜偏薄或不适合角膜切削者'::TEXT, '不切削角膜、适合高度近视'::TEXT, ''::TEXT, '术前干眼管理可联合干眼OPT'::TEXT, 'active'::TEXT, TRUE),
				(0::BIGINT, '屈光'::TEXT, '干眼OPT治疗'::TEXT, '["opt","干眼opt","睑板腺疏通","opt治疗"]'::jsonb, 980::numeric, '伴睑板腺堵塞、蒸发型干眼、术前需改善眼表者'::TEXT, '改善眼表状态、帮助术前准备和术后体验'::TEXT, ''::TEXT, ''::TEXT, 'active'::TEXT, TRUE)
		) AS t(tenant_id, industry, name, aliases, price, applicable, selling_point, upgrade_to, combine_with, status, is_template)
	)
	INSERT INTO products (
		tenant_id, industry, name, aliases, price, applicable, selling_point,
		upgrade_to, combine_with, status, is_template, created_at, updated_at
	)
	SELECT
		tenant_id, industry, name, aliases, price, applicable, selling_point,
		upgrade_to, combine_with, status, is_template, NOW(), NOW()
	FROM seed_rows s
	WHERE NOT EXISTS (
		SELECT 1
		FROM products p
		WHERE p.tenant_id = s.tenant_id
		  AND lower(p.name) = lower(s.name)
		  AND p.is_template = s.is_template
	)
	`
	if _, err := pool.Exec(ctx, seedSQL); err != nil {
		return fmt.Errorf("seed products: %w", err)
	}
	return nil
}

func seedBenchmarkReviewPrompt(ctx context.Context, pool *pgxpool.Pool) error {
	const promptCode = "benchmark_clip_review_v1"
	const systemPrompt = "你是一名医疗管理培训教练。请仅根据提供的证据生成可复用、可执行的点评，不得杜撰事实。"
	const userPrompt = `请基于以下标杆收录上下文生成点评，返回严格 JSON：
{
  "ai_comment": "80-160字，说明这段表达为什么值得团队学习，必须引用证据，不要空话，不要使用“缺失/不足/未...”等负面诊断语气",
  "learning_points": [
    "学习要点1（可执行动作）",
    "学习要点2（可执行动作）",
    "学习要点3（可执行动作）"
  ]
}

约束：
1) 只能依据输入内容，不得编造；
2) 学习要点输出3-5条，每条不超过32字；
3) 优先使用片段原文与维度证据；
4) 输出必须是 JSON，不要 Markdown。

上下文：
{{benchmark_context_json}}`
	const outputSchema = `{"type":"object","required":["ai_comment","learning_points"],"properties":{"ai_comment":{"type":"string"},"learning_points":{"type":"array","items":{"type":"string"},"minItems":3,"maxItems":5}}}`

	_, err := pool.Exec(ctx, `
		INSERT INTO recording_analysis_prompts (
			code, name, description, category, system_prompt, user_prompt_template,
			output_schema, version, is_active, usage_count, created_by, updated_by, created_at, updated_at
		)
		SELECT
			$1::text, $2::text, $3::text, $4::text, $5::text, $6::text, $7::jsonb, $8::text,
			true, 0, 1, 1, NOW(), NOW()
		WHERE NOT EXISTS (SELECT 1 FROM recording_analysis_prompts WHERE code = $1::text)
	`, promptCode, "标杆收录点评生成", "标杆收录后生成AI点评与学习要点", "management_dashboard", systemPrompt, userPrompt, outputSchema, "v1")
	return err
}

func seedBenchmarkCommentModelConfig(ctx context.Context, pool *pgxpool.Pool) error {
	_, err := pool.Exec(ctx, `
		INSERT INTO llm_model_configs (
			tenant_id, model_code, function_type, model_name, provider,
			model_params, extra_params, is_default, is_active, description, created_by, created_at, updated_at
		)
		SELECT
			0,
			'qwen-plus',
			'recording_benchmark_comment',
			'通义千问 Plus',
			'dashscope',
			'{"temperature":0.2,"max_tokens":900,"timeout_seconds":45}'::json,
			'{}'::json,
			true,
			true,
			'标杆收录点评生成默认模型配置',
			0,
			NOW(),
			NOW()
		WHERE NOT EXISTS (
			SELECT 1
			FROM llm_model_configs
			WHERE deleted_at IS NULL
			  AND tenant_id = 0
			  AND function_type = 'recording_benchmark_comment'
		)
	`)
	return err
}

func seedMorningMeetingPrompt(ctx context.Context, pool *pgxpool.Pool) error {
	const promptCode = "morning_meeting_review_v1"
	const systemPrompt = "你是一名医疗场景晨会带教主管。请基于提供的评分、证据和分析摘要，为管理者生成可直接拿去讲评的内容。不得编造事实。"
	const userPrompt = `请基于以下早会讲评上下文输出严格 JSON：

{
  "diagnosis": "2-3句话，明确指出问题所在或值得表扬之处，并说明为什么这条录音值得今天讲评",
  "coaching_script": "一段可直接在早会上说的话，语气像主管带教，先点出现状，再给出具体改进动作或复用动作"
}

要求：
1) 只能使用输入中的真实信息；
2) 诊断要具体，不要空话，不要泛泛而谈；
3) 讲评话术要口语化、可执行，长度控制在120-220字；
4) 如果是低分录音，要点出最需要改的一处；如果是高分录音，要点出最值得团队复用的一处；
5) 输出必须是 JSON，不要 Markdown。

上下文：
{{morning_meeting_context_json}}`
	const outputSchema = `{"type":"object","required":["diagnosis","coaching_script"],"properties":{"diagnosis":{"type":"string"},"coaching_script":{"type":"string"}}}`

	_, err := pool.Exec(ctx, `
		INSERT INTO recording_analysis_prompts (
			code, name, description, category, system_prompt, user_prompt_template,
			output_schema, version, is_active, usage_count, created_by, updated_by, created_at, updated_at
		)
		SELECT
			$1::text, $2::text, $3::text, $4::text, $5::text, $6::text, $7::jsonb, $8::text,
			true, 0, 1, 1, NOW(), NOW()
		WHERE NOT EXISTS (SELECT 1 FROM recording_analysis_prompts WHERE code = $1::text)
	`, promptCode, "早会讲评生成", "早会低分讲评录音的系统诊断与讲评话术生成", "management_dashboard", systemPrompt, userPrompt, outputSchema, "v1")
	return err
}

func seedMorningMeetingModelConfig(ctx context.Context, pool *pgxpool.Pool) error {
	_, err := pool.Exec(ctx, `
		INSERT INTO llm_model_configs (
			tenant_id, model_code, function_type, model_name, provider,
			model_params, extra_params, is_default, is_active, description, created_by, created_at, updated_at
		)
		SELECT
			0,
			'qwen-plus',
			'recording_morning_meeting_comment',
			'通义千问 Plus',
			'dashscope',
			'{"temperature":0.2,"max_tokens":1000,"timeout_seconds":45}'::json,
			'{}'::json,
			true,
			true,
			'早会讲评生成默认模型配置',
			0,
			NOW(),
			NOW()
		WHERE NOT EXISTS (
			SELECT 1
			FROM llm_model_configs
			WHERE deleted_at IS NULL
			  AND tenant_id = 0
			  AND function_type = 'recording_morning_meeting_comment'
		)
	`)
	return err
}

type builtinAnalysisPipelineSeed struct {
	Code        string
	Version     string
	Name        string
	SceneScope  string
	Description string
	ReleaseNote string
}

func seedBuiltinAnalysisPipelines(ctx context.Context, pool *pgxpool.Pool) error {
	seeds := []builtinAnalysisPipelineSeed{
		{Code: "doctor", Version: "v1", Name: "医生录音分析 v1", SceneScope: "post_call_analysis", Description: "legacy doctor pipeline", ReleaseNote: "builtin pipeline"},
		{Code: "doctor_patient", Version: "v1", Name: "医生我与患者分析 v1", SceneScope: "recording", Description: "default doctor patient pipeline", ReleaseNote: "builtin pipeline"},
		{Code: "therapist", Version: "v1", Name: "治疗师录音分析 v1", SceneScope: "post_call_analysis", Description: "default therapist pipeline", ReleaseNote: "builtin pipeline"},
		{Code: "frontdesk", Version: "v1", Name: "前台录音分析 v1", SceneScope: "frontdesk_reception", Description: "default frontdesk pipeline", ReleaseNote: "builtin pipeline"},
		{Code: "consultant", Version: "v1", Name: "咨询录音分析 v1", SceneScope: "admission_consult", Description: "legacy consultant pipeline", ReleaseNote: "builtin pipeline"},
		{Code: "consultant_conversion", Version: "v1", Name: "咨询成交转化分析 v1", SceneScope: "recording", Description: "default consultant conversion pipeline", ReleaseNote: "builtin pipeline"},
		{Code: "lingce_sales", Version: "v1", Name: "销售录音分析 v1", SceneScope: "admission_consult", Description: "default lingce sales pipeline", ReleaseNote: "builtin pipeline"},
		{Code: "customer", Version: "v1", Name: "客户录音分析 v1", SceneScope: "followup_quality", Description: "default customer pipeline", ReleaseNote: "builtin pipeline"},
	}
	for _, seed := range seeds {
		if _, err := pool.Exec(ctx, `
			INSERT INTO analysis_pipelines (
				pipeline_code, pipeline_version, name, scene_scope, status,
				description, release_note, is_default_candidate,
				created_at, updated_at, deleted_at
			)
			VALUES ($1, $2, $3, $4, 'published', $5, $6, TRUE, NOW(), NOW(), NULL)
			ON CONFLICT (pipeline_code, pipeline_version) DO UPDATE
			SET name = EXCLUDED.name,
			    scene_scope = EXCLUDED.scene_scope,
			    status = 'published',
			    description = COALESCE(NULLIF(EXCLUDED.description, ''), analysis_pipelines.description),
			    release_note = COALESCE(NULLIF(EXCLUDED.release_note, ''), analysis_pipelines.release_note),
			    is_default_candidate = TRUE,
			    deleted_at = NULL,
			    updated_at = NOW()
		`, seed.Code, seed.Version, seed.Name, seed.SceneScope, seed.Description, seed.ReleaseNote); err != nil {
			return err
		}
	}
	return nil
}

func seedDefaultAnalysisRoutes(ctx context.Context, pool *pgxpool.Pool) error {
	_, err := pool.Exec(ctx, `
		WITH role_candidates AS (
			SELECT tenant_id, id, lower(code) AS role_code
			FROM institution_roles
			WHERE deleted_at IS NULL
			  AND lower(code) IN (
			    'doctor',
			    'doctor_assistant',
			    'therapist',
			    'frontdesk',
			    'reception',
			    'receptionist',
			    'consultant',
			    'lingce_sales',
			    'customer',
			    'customer_service',
			    'nurse'
			  )
		),
		route_seed AS (
			SELECT
				tenant_id,
				id AS role_id,
				role_code AS role_code_snapshot,
				CASE
					WHEN role_code = 'doctor' THEN 'recording'
					WHEN role_code = 'consultant' THEN 'recording'
					WHEN role_code IN ('doctor_assistant', 'therapist') THEN 'post_call_analysis'
					WHEN role_code IN ('frontdesk', 'reception', 'receptionist') THEN 'frontdesk_reception'
					WHEN role_code = 'lingce_sales' THEN 'admission_consult'
					WHEN role_code IN ('customer', 'customer_service', 'nurse') THEN 'followup_quality'
					ELSE 'post_call_analysis'
				END AS scene_scope,
				CASE
					WHEN role_code = 'doctor' THEN 'doctor_patient'
					WHEN role_code = 'doctor_assistant' THEN 'doctor'
					WHEN role_code = 'therapist' THEN 'therapist'
					WHEN role_code IN ('frontdesk', 'reception', 'receptionist') THEN 'frontdesk'
					WHEN role_code = 'consultant' THEN 'consultant_conversion'
					WHEN role_code = 'lingce_sales' THEN 'lingce_sales'
					WHEN role_code IN ('customer', 'customer_service', 'nurse') THEN 'customer'
					ELSE 'doctor'
				END AS pipeline_code,
				'v1'::VARCHAR(64) AS pipeline_version
			FROM role_candidates
		),
		seed_clock AS (
			SELECT NOW() AS ts
		)
		INSERT INTO analysis_role_routes (
			tenant_id,
			role_id,
			role_code_snapshot,
			scene_scope,
			pipeline_code,
			pipeline_version,
			enabled,
			status,
			effective_at,
			published_at,
			created_at,
			updated_at
		)
		SELECT
			rs.tenant_id,
			rs.role_id,
			rs.role_code_snapshot,
			rs.scene_scope,
			rs.pipeline_code,
			rs.pipeline_version,
			TRUE AS enabled,
			'published' AS status,
			sc.ts AS effective_at,
			sc.ts AS published_at,
			sc.ts AS created_at,
			sc.ts AS updated_at
		FROM route_seed rs
		CROSS JOIN seed_clock sc
		WHERE EXISTS (
			SELECT 1
			FROM analysis_pipelines ap
			WHERE ap.pipeline_code = rs.pipeline_code
			  AND ap.pipeline_version = rs.pipeline_version
			  AND ap.deleted_at IS NULL
		)
		  AND NOT EXISTS (
			SELECT 1
			FROM analysis_role_routes arr
			WHERE arr.tenant_id = rs.tenant_id
			  AND arr.role_id = rs.role_id
			  AND arr.scene_scope = rs.scene_scope
			  AND arr.pipeline_code = rs.pipeline_code
			  AND arr.pipeline_version = rs.pipeline_version
			  AND COALESCE(NULLIF(lower(arr.status), ''), 'draft') = 'published'
			  AND arr.enabled = TRUE
		)
	`)
	if err != nil {
		return err
	}

	if _, err := pool.Exec(ctx, `
		WITH target_roles AS (
			SELECT tenant_id, id AS role_id, lower(code) AS role_code
			FROM institution_roles
			WHERE deleted_at IS NULL
			  AND lower(code) IN ('doctor', 'consultant')
		)
		UPDATE analysis_role_routes arr
		SET enabled = FALSE,
		    status = 'disabled',
		    updated_at = NOW()
		FROM target_roles tr
		WHERE arr.tenant_id = tr.tenant_id
		  AND arr.role_id = tr.role_id
		  AND arr.enabled = TRUE
		  AND COALESCE(NULLIF(lower(arr.status), ''), 'draft') = 'published'
		  AND (
			(tr.role_code = 'doctor' AND arr.pipeline_code IN ('doctor', 'doctor_conversion'))
			OR
			(tr.role_code = 'consultant' AND arr.pipeline_code = 'consultant')
		  )
	`); err != nil {
		return err
	}

	if _, err := pool.Exec(ctx, `
		WITH target_roles AS (
			SELECT tenant_id, id AS role_id, lower(code) AS role_code
			FROM institution_roles
			WHERE deleted_at IS NULL
			  AND lower(code) IN ('doctor', 'consultant')
		),
		route_seed AS (
			SELECT tenant_id, role_id, role_code, 'doctor_patient'::text AS pipeline_code, 'v1'::text AS pipeline_version
			FROM target_roles
			WHERE role_code = 'doctor'
			UNION ALL
			SELECT tenant_id, role_id, role_code, 'consultant_conversion'::text AS pipeline_code, 'v1'::text AS pipeline_version
			FROM target_roles
			WHERE role_code = 'consultant'
		)
		INSERT INTO worker_analysis_routes (
			tenant_id, role_id, role_code, pipeline_code, pipeline_version,
			enabled, effective_from, remark, created_at, updated_at
		)
		SELECT
			rs.tenant_id, rs.role_id, rs.role_code, rs.pipeline_code, rs.pipeline_version,
			TRUE, NOW(), 'default route migration', NOW(), NOW()
		FROM route_seed rs
		WHERE NOT EXISTS (
			SELECT 1
			FROM worker_analysis_routes war
			WHERE war.tenant_id = rs.tenant_id
			  AND war.role_id = rs.role_id
			  AND lower(war.role_code) = rs.role_code
			  AND war.pipeline_code = rs.pipeline_code
			  AND COALESCE(war.pipeline_version, '') = rs.pipeline_version
			  AND war.enabled = TRUE
			  AND war.deleted_at IS NULL
		)
	`); err != nil {
		return err
	}

	if _, err := pool.Exec(ctx, `
		WITH target_roles AS (
			SELECT tenant_id, id AS role_id, lower(code) AS role_code
			FROM institution_roles
			WHERE deleted_at IS NULL
			  AND lower(code) IN ('doctor', 'consultant')
		)
		UPDATE worker_analysis_routes war
		SET enabled = FALSE,
		    deleted_at = COALESCE(war.deleted_at, NOW()),
		    updated_at = NOW(),
		    remark = CASE
		    	WHEN COALESCE(NULLIF(war.remark, ''), '') = '' THEN 'disabled by default route migration'
		    	ELSE war.remark || '; disabled by default route migration'
		    END
		FROM target_roles tr
		WHERE war.tenant_id = tr.tenant_id
		  AND war.role_id = tr.role_id
		  AND war.enabled = TRUE
		  AND war.deleted_at IS NULL
		  AND (
			(tr.role_code = 'doctor' AND war.pipeline_code IN ('doctor', 'doctor_conversion'))
			OR
			(tr.role_code = 'consultant' AND war.pipeline_code = 'consultant')
		  )
	`); err != nil {
		return err
	}

	var seededCount int64
	if err := pool.QueryRow(ctx, `SELECT COUNT(*) FROM analysis_role_routes WHERE status = 'published' AND enabled = TRUE`).Scan(&seededCount); err != nil {
		return err
	}
	slog.Info("analysis route defaults ensured", "published_routes", seededCount)
	return nil
}
