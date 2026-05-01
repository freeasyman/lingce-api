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
		`UPDATE customers SET status = 'lead' WHERE status IS NULL`,
		`UPDATE customers SET momentum = 0 WHERE momentum IS NULL`,
		`UPDATE customers SET extra_data = '{}'::jsonb WHERE extra_data IS NULL`,
		`UPDATE customers SET created_by = 0 WHERE created_by IS NULL`,
		`UPDATE customers SET updated_at = COALESCE(updated_at, created_at, NOW()) WHERE updated_at IS NULL`,
		`ALTER TABLE IF EXISTS customer_tags ADD COLUMN IF NOT EXISTS deleted_at TIMESTAMP`,
		`ALTER TABLE IF EXISTS customer_groups ADD COLUMN IF NOT EXISTS deleted_at TIMESTAMP`,
		`ALTER TABLE IF EXISTS customer_groups ADD COLUMN IF NOT EXISTS member_count INTEGER NOT NULL DEFAULT 0`,
		`ALTER TABLE IF EXISTS customer_interactions ADD COLUMN IF NOT EXISTS interacted_at TIMESTAMP`,
		`ALTER TABLE IF EXISTS customer_interactions ADD COLUMN IF NOT EXISTS updated_at TIMESTAMP`,
		`UPDATE customer_interactions SET interacted_at = COALESCE(interacted_at, created_at, NOW()) WHERE interacted_at IS NULL`,
		`UPDATE customer_interactions SET updated_at = COALESCE(updated_at, created_at, NOW()) WHERE updated_at IS NULL`,
		`ALTER TABLE IF EXISTS customer_follow_ups ADD COLUMN IF NOT EXISTS status TEXT`,
		`UPDATE customer_follow_ups SET status = 'planned' WHERE status IS NULL`,
		`ALTER TABLE IF EXISTS customer_group_members ADD COLUMN IF NOT EXISTS created_at TIMESTAMP`,
		`ALTER TABLE IF EXISTS customer_group_members ADD COLUMN IF NOT EXISTS updated_at TIMESTAMP`,
		`UPDATE customer_group_members SET created_at = COALESCE(created_at, NOW()) WHERE created_at IS NULL`,
		`UPDATE customer_group_members SET updated_at = COALESCE(updated_at, created_at, NOW()) WHERE updated_at IS NULL`,

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
			ELSE CASE
				WHEN EXISTS (
					SELECT 1 FROM inst_employee_roles ier
					WHERE ier.tenant_id = r.tenant_id AND ier.employee_id = r.employee_id
					AND lower(ier.role_code) IN ('frontdesk','receptionist','reception')
				) THEN 'frontdesk'
				WHEN EXISTS (
					SELECT 1 FROM inst_employee_roles ier
					WHERE ier.tenant_id = r.tenant_id AND ier.employee_id = r.employee_id
					AND lower(ier.role_code) IN ('doctor','therapist','doctor_assistant')
				) THEN 'doctor'
				WHEN EXISTS (
					SELECT 1 FROM inst_employee_roles ier
					WHERE ier.tenant_id = r.tenant_id AND ier.employee_id = r.employee_id
					AND lower(ier.role_code) IN ('consultant')
				) THEN 'consultant'
				WHEN EXISTS (
					SELECT 1 FROM inst_employee_roles ier
					WHERE ier.tenant_id = r.tenant_id AND ier.employee_id = r.employee_id
					AND lower(ier.role_code) IN ('therapist')
				) THEN 'therapist'
				WHEN EXISTS (
					SELECT 1 FROM inst_employee_roles ier
					WHERE ier.tenant_id = r.tenant_id AND ier.employee_id = r.employee_id
					AND lower(ier.role_code) IN ('nurse')
				) THEN 'nurse'
				WHEN EXISTS (
					SELECT 1 FROM inst_employee_roles ier
					WHERE ier.tenant_id = r.tenant_id AND ier.employee_id = r.employee_id
					AND lower(ier.role_code) IN ('lingce_sales')
				) THEN 'lingce_sales'
				ELSE 'unknown'
			END
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
		`ALTER TABLE IF EXISTS tenants ADD COLUMN IF NOT EXISTS updated_at TIMESTAMP NOT NULL DEFAULT NOW()`,
		`UPDATE tenants SET updated_at = COALESCE(updated_at, created_at, NOW()) WHERE updated_at IS NULL`,
		`ALTER TABLE IF EXISTS departments ADD COLUMN IF NOT EXISTS code TEXT`,
		`ALTER TABLE IF EXISTS departments ADD COLUMN IF NOT EXISTS parent_id BIGINT`,
		`ALTER TABLE IF EXISTS departments ADD COLUMN IF NOT EXISTS is_active BOOLEAN NOT NULL DEFAULT TRUE`,
		`ALTER TABLE IF EXISTS departments ADD COLUMN IF NOT EXISTS deleted_at TIMESTAMP`,
		`ALTER TABLE IF EXISTS departments ADD COLUMN IF NOT EXISTS updated_at TIMESTAMP NOT NULL DEFAULT NOW()`,
		`UPDATE departments SET updated_at = COALESCE(updated_at, created_at, NOW()) WHERE updated_at IS NULL`,
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
		`UPDATE employees SET full_name = COALESCE(NULLIF(full_name, ''), username, 'unknown')`,
		`UPDATE employees SET updated_at = COALESCE(updated_at, created_at, NOW()) WHERE updated_at IS NULL`,
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
		`CREATE TABLE IF NOT EXISTS institution_employee_roles (
			id BIGSERIAL PRIMARY KEY,
			employee_id BIGINT NOT NULL,
			role_id BIGINT NOT NULL,
			created_at TIMESTAMP NOT NULL DEFAULT NOW()
		)`,
		`CREATE UNIQUE INDEX IF NOT EXISTS uk_institution_employee_roles_employee_id ON institution_employee_roles(employee_id)`,
		`CREATE INDEX IF NOT EXISTS idx_institution_employee_roles_role_id ON institution_employee_roles(role_id)`,
		`CREATE TABLE IF NOT EXISTS institution_department_roles (
			id BIGSERIAL PRIMARY KEY,
			department_id BIGINT NOT NULL,
			role_id BIGINT NOT NULL,
			is_default BOOLEAN NOT NULL DEFAULT TRUE,
			created_at TIMESTAMP NOT NULL DEFAULT NOW()
		)`,
		`CREATE UNIQUE INDEX IF NOT EXISTS uk_institution_department_roles_department_id ON institution_department_roles(department_id)`,
		`CREATE INDEX IF NOT EXISTS idx_institution_department_roles_role_id ON institution_department_roles(role_id)`,
		`INSERT INTO institution_roles (tenant_id, name, code, description, is_active, created_at, updated_at)
		 SELECT t.id,
		        COALESCE(NULLIF(r.name_cn, ''), r.code),
		        r.code,
		        r.description,
		        COALESCE(r.is_active, true),
		        COALESCE(r.created_at, NOW()),
		        COALESCE(r.updated_at, COALESCE(r.created_at, NOW()))
		 FROM tenants t
		 CROSS JOIN inst_roles r
		 ON CONFLICT (tenant_id, code) WHERE deleted_at IS NULL
		 DO UPDATE SET
		   name = EXCLUDED.name,
		   description = EXCLUDED.description,
		   is_active = EXCLUDED.is_active,
		   updated_at = NOW()`,
		`WITH dedup AS (
		     SELECT DISTINCT ON (er.employee_id)
		            er.employee_id,
		            ir.id AS role_id,
		            COALESCE(er.created_at, NOW()) AS created_at
		     FROM inst_employee_roles er
		     JOIN institution_roles ir ON ir.tenant_id = er.tenant_id AND ir.code = er.role_code AND ir.deleted_at IS NULL
		     JOIN employees e ON e.id = er.employee_id AND e.deleted_at IS NULL
		     ORDER BY er.employee_id, COALESCE(er.created_at, NOW()) DESC, ir.id DESC
		 )
		 INSERT INTO institution_employee_roles (employee_id, role_id, created_at)
		 SELECT employee_id, role_id, created_at
		 FROM dedup
		 ON CONFLICT (employee_id)
		 DO UPDATE SET
		   role_id = EXCLUDED.role_id,
		   created_at = EXCLUDED.created_at`,
		`INSERT INTO institution_department_roles (department_id, role_id, is_default, created_at)
		 SELECT dr.department_id, ir.id, COALESCE(dr.is_default, true), COALESCE(dr.created_at, NOW())
		 FROM inst_department_roles dr
		 JOIN institution_roles ir ON ir.tenant_id = dr.tenant_id AND ir.code = dr.role_code AND ir.deleted_at IS NULL
		 JOIN departments d ON d.id = dr.department_id AND d.deleted_at IS NULL
		 ON CONFLICT (department_id)
		 DO UPDATE SET role_id = EXCLUDED.role_id, is_default = EXCLUDED.is_default`,

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
		`UPDATE content_topics SET tags = '[]'::jsonb WHERE tags IS NULL`,
		`UPDATE content_topics SET source = 'manual' WHERE source IS NULL`,
		`UPDATE content_topics SET status = 'draft' WHERE status IS NULL`,
		`UPDATE content_topics SET extra_data = '{}'::jsonb WHERE extra_data IS NULL`,
		`UPDATE content_topics SET created_by = 0 WHERE created_by IS NULL`,
		`UPDATE content_topics SET updated_at = COALESCE(updated_at, created_at, NOW()) WHERE updated_at IS NULL`,
		`ALTER TABLE IF EXISTS content_publish_tasks ADD COLUMN IF NOT EXISTS deleted_at TIMESTAMP`,
		`CREATE TABLE IF NOT EXISTS content_items (
			id BIGSERIAL PRIMARY KEY,
			tenant_id BIGINT NOT NULL,
			topic_id BIGINT,
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

		// Badge module compatibility (missing columns/table)
		`ALTER TABLE IF EXISTS badge_devices ADD COLUMN IF NOT EXISTS device_id TEXT`,
		`ALTER TABLE IF EXISTS badge_devices ADD COLUMN IF NOT EXISTS manufacturer_code TEXT NOT NULL DEFAULT ''`,
		`ALTER TABLE IF EXISTS badge_devices ADD COLUMN IF NOT EXISTS model TEXT`,
		`ALTER TABLE IF EXISTS badge_devices ADD COLUMN IF NOT EXISTS status TEXT NOT NULL DEFAULT 'in_use'`,
		`ALTER TABLE IF EXISTS badge_devices ADD COLUMN IF NOT EXISTS tenant_id BIGINT`,
		`ALTER TABLE IF EXISTS badge_devices ADD COLUMN IF NOT EXISTS employee_id BIGINT`,
		`ALTER TABLE IF EXISTS badge_devices ADD COLUMN IF NOT EXISTS deleted_at TIMESTAMP`,
		`ALTER TABLE IF EXISTS badge_devices ADD COLUMN IF NOT EXISTS battery_level INTEGER`,
		`ALTER TABLE IF EXISTS badge_devices ADD COLUMN IF NOT EXISTS firmware_version TEXT`,
		`ALTER TABLE IF EXISTS badge_devices ADD COLUMN IF NOT EXISTS extra_data JSONB DEFAULT '{}'::jsonb`,
		`ALTER TABLE IF EXISTS badge_devices ADD COLUMN IF NOT EXISTS accepted_at TIMESTAMP`,
		`ALTER TABLE IF EXISTS badge_devices ADD COLUMN IF NOT EXISTS assigned_to_tenant_at TIMESTAMP`,
		`ALTER TABLE IF EXISTS badge_devices ADD COLUMN IF NOT EXISTS assigned_to_emp_at TIMESTAMP`,
		`ALTER TABLE IF EXISTS badge_devices ADD COLUMN IF NOT EXISTS last_online_at TIMESTAMP`,
		`ALTER TABLE IF EXISTS badge_devices ADD COLUMN IF NOT EXISTS created_at TIMESTAMP NOT NULL DEFAULT NOW()`,
		`ALTER TABLE IF EXISTS badge_devices ADD COLUMN IF NOT EXISTS updated_at TIMESTAMP NOT NULL DEFAULT NOW()`,
		`UPDATE badge_devices SET status = 'in_use' WHERE status IS NULL`,
		`UPDATE badge_devices SET manufacturer_code = '' WHERE manufacturer_code IS NULL`,
		`UPDATE badge_devices SET extra_data = '{}'::jsonb WHERE extra_data IS NULL`,
		`UPDATE badge_devices SET created_at = NOW() WHERE created_at IS NULL`,
		`UPDATE badge_devices SET updated_at = COALESCE(updated_at, created_at, NOW()) WHERE updated_at IS NULL`,
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
		`DO $$
		BEGIN
			IF EXISTS (
				SELECT 1 FROM information_schema.columns
				WHERE table_name = 'badge_device_lifecycle_logs' AND column_name = 'operation'
			) THEN
				EXECUTE 'UPDATE badge_device_lifecycle_logs SET action = COALESCE(action, operation, ''unknown'') WHERE action IS NULL';
			END IF;
		END $$`,
		`UPDATE badge_device_lifecycle_logs SET action = COALESCE(action, 'unknown') WHERE action IS NULL`,
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
		`UPDATE badge_devices SET health_status = 'unknown' WHERE health_status IS NULL`,
		`UPDATE badge_devices SET health_status = 'healthy' WHERE lower(trim(COALESCE(health_status, ''))) = 'normal'`,
		`UPDATE badge_devices
		 SET health_status = 'unknown'
		 WHERE trim(COALESCE(health_status, '')) = ''
		    OR lower(trim(COALESCE(health_status, ''))) NOT IN ('unknown', 'healthy', 'warning', 'error')`,
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
		`UPDATE badge_devices SET metadata = '{}'::jsonb WHERE metadata IS NULL`,
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
		`UPDATE tenant_subscription_plans SET updated_at = COALESCE(updated_at, created_at, NOW()) WHERE updated_at IS NULL`,
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
		`DO $$
		BEGIN
			IF EXISTS (
				SELECT 1 FROM information_schema.columns
				WHERE table_name = 'tenant_subscriptions' AND column_name = 'started_on'
			) THEN
				EXECUTE 'UPDATE tenant_subscriptions
				           SET start_date = COALESCE(start_date, started_on::timestamp, created_at, NOW())
				         WHERE start_date IS NULL';
			END IF;
			IF EXISTS (
				SELECT 1 FROM information_schema.columns
				WHERE table_name = 'tenant_subscriptions' AND column_name = 'expired_on'
			) THEN
				EXECUTE 'UPDATE tenant_subscriptions
				           SET end_date = COALESCE(end_date, expired_on::timestamp, start_date, NOW())
				         WHERE end_date IS NULL';
			END IF;
			IF EXISTS (
				SELECT 1 FROM information_schema.columns
				WHERE table_name = 'tenant_subscriptions' AND column_name = 'grace_end_on'
			) THEN
				EXECUTE 'UPDATE tenant_subscriptions
				           SET grace_end_date = COALESCE(grace_end_date, grace_end_on::timestamp)
				         WHERE grace_end_date IS NULL';
			END IF;
		END $$`,
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
		`UPDATE tenant_subscription_events
		    SET request_id = COALESCE(NULLIF(request_id, ''), CONCAT('compat_', tenant_id::text, '_', EXTRACT(EPOCH FROM created_at)::bigint::text))
		  WHERE request_id IS NULL OR request_id = ''`,
		`UPDATE tenant_subscription_events
		    SET operator_name = COALESCE(NULLIF(operator_name, ''), 'system')
		  WHERE operator_name IS NULL OR operator_name = ''`,
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
		`UPDATE tenant_feature_groups SET updated_at = COALESCE(updated_at, created_at, NOW()) WHERE updated_at IS NULL`,
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
		`DO $$
		BEGIN
			IF EXISTS (
				SELECT 1 FROM information_schema.columns
				WHERE table_name = 'tenant_feature_group_items' AND column_name = 'code'
			) THEN
				EXECUTE 'UPDATE tenant_feature_group_items SET feature_code = COALESCE(feature_code, code) WHERE feature_code IS NULL';
			END IF;
			END $$`,
		`UPDATE tenant_feature_group_items
				SET item_type = COALESCE(NULLIF(item_type, ''), 'feature'),
				    item_code = COALESCE(NULLIF(item_code, ''), feature_code, '')
			  WHERE item_type IS NULL OR item_type = '' OR item_code IS NULL OR item_code = ''`,
		`UPDATE tenant_feature_group_items SET feature_code = COALESCE(feature_code, '') WHERE feature_code IS NULL`,
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
			SET api_endpoint = COALESCE(api_endpoint, api_base_url)
			WHERE api_endpoint IS NULL`,
		`UPDATE llm_model_configs
			SET api_key = COALESCE(api_key, api_key_encrypted)
			WHERE api_key IS NULL`,
		`UPDATE llm_model_configs
			SET model_params = COALESCE(model_params, extra_params)
			WHERE model_params IS NULL`,
		`DO $$
		BEGIN
			IF EXISTS (
				SELECT 1 FROM information_schema.columns
				WHERE table_name = 'tenant_feature_overrides' AND column_name = 'code'
			) THEN
				EXECUTE 'UPDATE tenant_feature_overrides SET feature_code = COALESCE(feature_code, code) WHERE feature_code IS NULL';
			END IF;
			END $$`,
		`UPDATE tenant_feature_overrides
				SET item_type = COALESCE(NULLIF(item_type, ''), 'feature'),
				    item_code = COALESCE(NULLIF(item_code, ''), feature_code, ''),
				    override_mode = COALESCE(NULLIF(override_mode, ''), CASE WHEN COALESCE(is_enabled, true) THEN 'allow' ELSE 'deny' END)
			  WHERE item_type IS NULL OR item_type = '' OR item_code IS NULL OR item_code = '' OR override_mode IS NULL OR override_mode = ''`,
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
			username TEXT,
			password_hash TEXT,
			email TEXT,
			is_active BOOLEAN NOT NULL DEFAULT TRUE,
			session_version INTEGER NOT NULL DEFAULT 1,
			created_at TIMESTAMP NOT NULL DEFAULT NOW(),
			updated_at TIMESTAMP NOT NULL DEFAULT NOW(),
			deleted_at TIMESTAMP
		)`,
		`ALTER TABLE IF EXISTS operations_admins ADD COLUMN IF NOT EXISTS username TEXT`,
		`ALTER TABLE IF EXISTS operations_admins ADD COLUMN IF NOT EXISTS password_hash TEXT`,
		`ALTER TABLE IF EXISTS operations_admins ADD COLUMN IF NOT EXISTS email TEXT`,
		`ALTER TABLE IF EXISTS operations_admins ADD COLUMN IF NOT EXISTS is_active BOOLEAN NOT NULL DEFAULT TRUE`,
		`ALTER TABLE IF EXISTS operations_admins ADD COLUMN IF NOT EXISTS session_version INTEGER NOT NULL DEFAULT 1`,
		`ALTER TABLE IF EXISTS operations_admins ADD COLUMN IF NOT EXISTS created_at TIMESTAMP NOT NULL DEFAULT NOW()`,
		`ALTER TABLE IF EXISTS operations_admins ADD COLUMN IF NOT EXISTS updated_at TIMESTAMP NOT NULL DEFAULT NOW()`,
		`ALTER TABLE IF EXISTS operations_admins ADD COLUMN IF NOT EXISTS deleted_at TIMESTAMP`,
		`CREATE TABLE IF NOT EXISTS operations_admin_roles (
			admin_id BIGINT NOT NULL,
			role_id BIGINT NOT NULL,
			created_at TIMESTAMP NOT NULL DEFAULT NOW(),
			PRIMARY KEY (admin_id, role_id)
		)`,
		`INSERT INTO operations_roles (name, code, description, is_active, created_at, updated_at)
			SELECT
				COALESCE(NULLIF(r.name_cn, ''), r.code),
				r.code,
				r.description,
				COALESCE(r.is_active, true),
				COALESCE(r.created_at, NOW()),
				COALESCE(r.updated_at, NOW())
			FROM ops_roles r
			WHERE NOT EXISTS (
				SELECT 1 FROM operations_roles nr WHERE nr.code = r.code
			)`,
		`INSERT INTO operations_menus (id, name, code, path, icon, parent_id, sort_order, is_active, created_at, updated_at, deleted_at)
			SELECT
				m.id,
				m.name,
				m.code,
				COALESCE(m.path, ''),
				m.icon,
				m.parent_id,
				COALESCE(m.order_index, 0),
				COALESCE(m.is_active, true),
				COALESCE(m.created_at, NOW()),
				COALESCE(m.created_at, NOW()),
				NULL
			FROM ops_menus m
			WHERE NOT EXISTS (
				SELECT 1 FROM operations_menus nm WHERE nm.id = m.id OR nm.code = m.code
			)`,
		`SELECT setval('operations_menus_id_seq', COALESCE((SELECT MAX(id) FROM operations_menus), 1), true)`,
		`INSERT INTO operations_role_menus (role_id, menu_id, created_at)
			SELECT
				r.id,
				rm.menu_id,
				COALESCE(rm.created_at, NOW())
			FROM ops_role_menus rm
			JOIN operations_roles r ON r.code = rm.role_code
			JOIN operations_menus m ON m.id = rm.menu_id
			WHERE NOT EXISTS (
				SELECT 1 FROM operations_role_menus nrm
				WHERE nrm.role_id = r.id AND nrm.menu_id = rm.menu_id
			)`,
		`INSERT INTO operations_admin_roles (admin_id, role_id, created_at)
			SELECT
				ar.admin_id,
				r.id,
				COALESCE(ar.created_at, NOW())
			FROM ops_admin_roles ar
			JOIN operations_roles r ON r.code = ar.role_code
			JOIN operations_admins a ON a.id = ar.admin_id AND a.deleted_at IS NULL
			WHERE NOT EXISTS (
				SELECT 1 FROM operations_admin_roles nar
				WHERE nar.admin_id = ar.admin_id AND nar.role_id = r.id
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
		`UPDATE customer_memberships cm
			SET tenant_id = c.tenant_id
			FROM customers c
			WHERE cm.customer_id = c.id AND cm.tenant_id IS NULL`,
		`UPDATE customer_memberships SET points = 0 WHERE points IS NULL`,
		`UPDATE customer_memberships SET start_date = COALESCE(start_date, created_at, NOW()) WHERE start_date IS NULL`,
		`ALTER TABLE IF EXISTS customer_follow_ups ADD COLUMN IF NOT EXISTS tenant_id BIGINT`,
		`ALTER TABLE IF EXISTS customer_follow_ups ADD COLUMN IF NOT EXISTS scheduled_at TIMESTAMP`,
		`ALTER TABLE IF EXISTS customer_follow_ups ADD COLUMN IF NOT EXISTS completed_at TIMESTAMP`,
		`ALTER TABLE IF EXISTS customer_follow_ups ADD COLUMN IF NOT EXISTS employee_id BIGINT`,
		`ALTER TABLE IF EXISTS customer_follow_ups ADD COLUMN IF NOT EXISTS created_at TIMESTAMP NOT NULL DEFAULT NOW()`,
		`ALTER TABLE IF EXISTS customer_follow_ups ADD COLUMN IF NOT EXISTS updated_at TIMESTAMP NOT NULL DEFAULT NOW()`,
		`UPDATE customer_follow_ups cf
			SET tenant_id = c.tenant_id
			FROM customers c
			WHERE cf.customer_id = c.id AND cf.tenant_id IS NULL`,
		`UPDATE customer_follow_ups SET scheduled_at = COALESCE(scheduled_at, created_at, NOW()) WHERE scheduled_at IS NULL`,

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
		`UPDATE recordings SET status = 'pending' WHERE status IS NULL`,
		`UPDATE recordings SET updated_at = COALESCE(updated_at, created_at, NOW()) WHERE updated_at IS NULL`,

		// Front-desk recording analysis tables
		`CREATE TABLE IF NOT EXISTS frontdesk_shift_analyses (
			id BIGSERIAL PRIMARY KEY,
			tenant_id BIGINT NOT NULL,
			recording_id BIGINT NOT NULL,
			employee_id BIGINT NOT NULL,
			shift_date DATE NOT NULL,
			shift_type VARCHAR(20),
			recording_duration_seconds INTEGER,
			estimated_interaction_count INTEGER,
			estimated_appointment_count INTEGER,
			estimated_walkin_count INTEGER,
			analysis_json JSONB NOT NULL,
			transcript TEXT,
			created_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP
		)`,
		`CREATE INDEX IF NOT EXISTS idx_shift_analyses_tenant_id ON frontdesk_shift_analyses(tenant_id)`,
		`CREATE INDEX IF NOT EXISTS idx_shift_analyses_recording_id ON frontdesk_shift_analyses(recording_id)`,
		`CREATE INDEX IF NOT EXISTS idx_shift_analyses_employee_id ON frontdesk_shift_analyses(employee_id)`,
		`CREATE INDEX IF NOT EXISTS idx_shift_analyses_shift_date ON frontdesk_shift_analyses(shift_date)`,

		`CREATE TABLE IF NOT EXISTS frontdesk_customer_questions (
			id BIGSERIAL PRIMARY KEY,
			tenant_id BIGINT NOT NULL,
			shift_analysis_id BIGINT NOT NULL REFERENCES frontdesk_shift_analyses(id),
			employee_id BIGINT NOT NULL,
			question_text TEXT NOT NULL,
			question_category VARCHAR(30),
			answer_text TEXT,
			answer_quality VARCHAR(30),
			customer_followup BOOLEAN,
			followup_quote TEXT,
			approx_time VARCHAR(10),
			created_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP
		)`,
		`CREATE INDEX IF NOT EXISTS idx_customer_questions_tenant_id ON frontdesk_customer_questions(tenant_id)`,
		`CREATE INDEX IF NOT EXISTS idx_customer_questions_shift_analysis_id ON frontdesk_customer_questions(shift_analysis_id)`,
		`CREATE INDEX IF NOT EXISTS idx_customer_questions_category ON frontdesk_customer_questions(question_category)`,

		`CREATE TABLE IF NOT EXISTS frontdesk_business_signals (
			id BIGSERIAL PRIMARY KEY,
			tenant_id BIGINT NOT NULL,
			shift_analysis_id BIGINT NOT NULL REFERENCES frontdesk_shift_analyses(id),
			signal_type VARCHAR(30),
			signal_value TEXT,
			evidence_quote TEXT,
			sentiment VARCHAR(20),
			approx_time VARCHAR(10),
			created_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP
		)`,
		`CREATE INDEX IF NOT EXISTS idx_business_signals_tenant_id ON frontdesk_business_signals(tenant_id)`,
		`CREATE INDEX IF NOT EXISTS idx_business_signals_shift_analysis_id ON frontdesk_business_signals(shift_analysis_id)`,
		`CREATE INDEX IF NOT EXISTS idx_business_signals_type ON frontdesk_business_signals(signal_type)`,

		`CREATE TABLE IF NOT EXISTS frontdesk_risk_events (
			id BIGSERIAL PRIMARY KEY,
			tenant_id BIGINT NOT NULL,
			recording_id BIGINT NOT NULL,
			employee_id BIGINT NOT NULL,
			event_type VARCHAR(30),
			severity VARCHAR(10),
			event_time TIMESTAMP NOT NULL,
			trigger_quote TEXT,
			context TEXT,
			suggested_action TEXT,
			alerted BOOLEAN DEFAULT FALSE,
			alert_time TIMESTAMP,
			resolved BOOLEAN DEFAULT FALSE,
			resolution_note TEXT,
			created_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP
		)`,
		`CREATE INDEX IF NOT EXISTS idx_risk_events_tenant_id ON frontdesk_risk_events(tenant_id)`,
		`CREATE INDEX IF NOT EXISTS idx_risk_events_recording_id ON frontdesk_risk_events(recording_id)`,
		`CREATE INDEX IF NOT EXISTS idx_risk_events_employee_id ON frontdesk_risk_events(employee_id)`,
		`CREATE INDEX IF NOT EXISTS idx_risk_events_severity ON frontdesk_risk_events(severity)`,

		`CREATE TABLE IF NOT EXISTS frontdesk_shift_assessments (
			id BIGSERIAL PRIMARY KEY,
			tenant_id BIGINT NOT NULL,
			shift_analysis_id BIGINT NOT NULL REFERENCES frontdesk_shift_analyses(id),
			employee_id BIGINT NOT NULL,
			d1_opening_grade VARCHAR(5),
			d2_response_grade VARCHAR(5),
			d3_intelligence_grade VARCHAR(5),
			d4_retention_grade VARCHAR(5),
			d5_compliance_grade VARCHAR(5),
			assessment_details JSONB,
			has_compliance_issue BOOLEAN DEFAULT FALSE,
			created_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP
		)`,
		`CREATE INDEX IF NOT EXISTS idx_shift_assessments_tenant_id ON frontdesk_shift_assessments(tenant_id)`,
		`CREATE INDEX IF NOT EXISTS idx_shift_assessments_shift_analysis_id ON frontdesk_shift_assessments(shift_analysis_id)`,
		`CREATE INDEX IF NOT EXISTS idx_shift_assessments_employee_id ON frontdesk_shift_assessments(employee_id)`,

		`CREATE TABLE IF NOT EXISTS frontdesk_daily_reports (
			id BIGSERIAL PRIMARY KEY,
			tenant_id BIGINT NOT NULL,
			report_date DATE NOT NULL,
			total_estimated_interactions INTEGER,
			estimated_appointment_count INTEGER,
			estimated_walkin_count INTEGER,
			estimated_walkin_capture_rate DECIMAL(4,3),
			top_questions JSONB,
			competitor_mentions JSONB,
			doctor_inquiries JSONB,
			channel_feedback JSONB,
			lost_reasons JSONB,
			risk_event_count INTEGER,
			testimonial_materials JSONB,
			created_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
			UNIQUE(tenant_id, report_date)
		)`,
		`CREATE INDEX IF NOT EXISTS idx_daily_reports_tenant_id ON frontdesk_daily_reports(tenant_id)`,
		`CREATE INDEX IF NOT EXISTS idx_daily_reports_report_date ON frontdesk_daily_reports(report_date)`,

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

		// Front-desk analysis prompts
		`INSERT INTO recording_analysis_prompts (code, name, description, category, system_prompt, user_prompt_template, output_schema, version, is_active, created_by, updated_by, usage_count, created_at, updated_at)
		VALUES ('P-SIGNAL-EXTRACT', '经营信号提取', '从前台录音中提取经营信号', 'analysis',
			'你是一个医疗机构前台录音分析专家。从对话中提取经营信号，只提取有明确证据的信号，禁止推测。',
			'以下是前台客服的工作录音转写文本（约1小时）。\n\n## 你的任务\n从对话中提取以下经营信号。\n\n## 需要提取的信号类型\n\n### 1. 客户问题与前台应答\n识别客户主动提出的实质性问题（关于价格、医生、恢复期、安全性、付款方式等），以及前台的回答。\n\n### 2. 竞品提及\n客户提到其他医院/机构的名称，以及对竞品的评价。\n\n### 3. 价格敏感信号\n客户表达对价格的关注。\n\n### 4. 医生关注\n客户询问特定医生或要求推荐医生。\n\n### 5. 渠道来源\n客户提到如何知道本机构。\n\n### 6. 决策角色\n识别咨询者是否为本人就诊。\n\n### 7. 决策障碍\n客户表达不能当场决定或就诊的原因。\n\n### 8. 口碑素材\n客户的正面评价、转介绍表述、对机构/医生的认可。\n\n## 转写文本\n{{ transcript_chunk }}\n\n## 输出格式\n严格按JSON格式输出，无证据的字段不要输出。',
			'{}', '1', true, 1, 1, 0, NOW(), NOW())
		ON CONFLICT (code) DO NOTHING`,

		`INSERT INTO recording_analysis_prompts (code, name, description, category, system_prompt, user_prompt_template, output_schema, version, is_active, created_by, updated_by, usage_count, created_at, updated_at)
		VALUES ('P-RETENTION-OBSERVE', '留存行为观察', '识别客户留存相关行为', 'analysis',
			'你是一个医疗机构前台录音分析专家。识别其中的客户留存相关行为。',
			'以下是前台客服的工作录音转写文本（约1小时）。\n\n## 你的任务\n识别其中的客户留存相关行为。\n\n## 需要识别的内容\n\n### 1. 场景分类\n判断对话中的客户交互是\"预约到院\"还是\"无预约walk-in\"。\n\n### 2. 留存动作观察\n当客户表达离开意图时：\n- 前台是否做了挽留动作？\n- 是否了解了客户犹豫的原因？\n- 最终是否留下了联系方式？\n- 是否安排了具体的后续动作？\n\n### 3. 对话结束方式\n每段可识别的客户交互结束时：\n- 前台是否有结束收口？\n- 是否引导了留资？\n- 是否给出了具体的后续安排？\n\n## 转写文本\n{{ transcript_chunk }}\n\n## 输出格式\n严格按JSON格式输出。',
			'{}', '1', true, 1, 1, 0, NOW(), NOW())
		ON CONFLICT (code) DO NOTHING`,

		`INSERT INTO recording_analysis_prompts (code, name, description, category, system_prompt, user_prompt_template, output_schema, version, is_active, created_by, updated_by, usage_count, created_at, updated_at)
		VALUES ('P-RISK-DETECT', '风险事件检测', '识别录音中的风险事件', 'analysis',
			'你是一个医疗机构前台录音分析专家。识别其中的风险事件。只标记有明确证据的风险，不要过度标记。',
			'以下是前台客服的工作录音转写文本（约1小时）。\n\n## 你的任务\n识别其中的风险事件。\n\n## 风险类型\n\n### 1. 情绪爆发（emotion_escalation）\n客户表达愤怒、强烈不满、威胁投诉。\n严重度：high（威胁投诉/要求见经理）/ medium（明显不耐烦）/ low（轻微不满）。\n\n### 2. 等待超时（wait_timeout）\n客户明确表示等待时间过长。\n\n### 3. 合规风险（compliance_risk）\n前台做出疗效承诺、越权诊断、虚假宣传。\n严重度：high（明确承诺疗效/越权诊断）/ medium（暗示性承诺）/ low（表述不够严谨）。\n\n### 4. 客户流失（customer_loss）\n客户表达离开意图且前台未有效挽留。\n\n## 转写文本\n{{ transcript_chunk }}\n\n## 输出格式\n严格按JSON格式输出。',
			'{}', '1', true, 1, 1, 0, NOW(), NOW())
		ON CONFLICT (code) DO NOTHING`,

		`INSERT INTO recording_analysis_prompts (code, name, description, category, system_prompt, user_prompt_template, output_schema, version, is_active, created_by, updated_by, usage_count, created_at, updated_at)
		VALUES ('P-SHIFT-ASSESS', '班次表现评估', '对前台班次表现进行五维度评估', 'assessment',
			'你是一个医疗机构前台服务质量评估专家。基于分析结果，对该前台的班次表现做五维度评估。',
			'以下是某前台客服一个班次（半天）的录音分析结果。\n\n## 你的任务\n基于分析结果，对该前台的班次表现做五维度评估。\n\n## 评估维度\n\n### 维度一：开场与需求识别\n主动接触、需求/来意识别、场景适配。\n\n### 维度二：应答与专业呈现\n高频问题应答具体度、医生推荐质量、不确定问题的处理。\n\n### 维度三：信息捕捉与传递\n客户主动释放信号的捕捉、信息深化能力、信息传递与衔接。\n\n### 维度四：留存与承接\n留资动作、后续安排的具体度、犹豫/离场客户的挽留。\n\n### 维度五：合规底线\n疗效承诺、越权诊断、虚假/夸大宣传、严重失礼。\n\n## 评估规则\n1. 每个维度给出等级（A/A-/B+/B/B-/C+/C/D）和简要说明\n2. 每个等级判断必须引用具体证据（时间点+原话）\n3. 选取1-2个亮点时刻和1-2个改进时刻，附转写片段\n4. 改进时刻必须给出具体的改进建议\n5. 亮点在前，改进在后\n\n## 转写文本\n{{ transcript }}\n\n## 输出格式\n严格按JSON格式输出。',
			'{}', '1', true, 1, 1, 0, NOW(), NOW())
		ON CONFLICT (code) DO NOTHING`,
	}

	for i, stmt := range stmts {
		if _, err := pool.Exec(ctx, stmt); err != nil {
			return fmt.Errorf("compat migration failed at step %d: %w", i+1, err)
		}
	}

	slog.Info("compatibility migrations applied", "steps", len(stmts))
	return nil
}
