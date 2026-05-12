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
		`UPDATE employees SET full_name = COALESCE(NULLIF(full_name, ''), NULLIF(name, ''), username, 'unknown')`,
		`UPDATE employees
		 SET full_name = phone
		 WHERE NULLIF(phone, '') IS NOT NULL
		   AND lower(trim(COALESCE(full_name, ''))) = 'unknown'
		   AND lower(trim(COALESCE(name, ''))) = 'unknown'`,
		`UPDATE employees
		 SET name = full_name
		 WHERE COALESCE(NULLIF(full_name, ''), '') <> ''
		   AND COALESCE(NULLIF(full_name, ''), '') <> COALESCE(NULLIF(name, ''), '')`,
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
		`INSERT INTO institution_department_roles (department_id, role_id, is_default, created_at)
		 SELECT dr.department_id, ir.id, COALESCE(dr.is_default, true), COALESCE(dr.created_at, NOW())
		 FROM inst_department_roles dr
		 JOIN institution_roles ir ON ir.tenant_id = dr.tenant_id AND ir.code = dr.role_code AND ir.deleted_at IS NULL
		 JOIN departments d ON d.id = dr.department_id AND d.deleted_at IS NULL
		 ON CONFLICT (department_id)
		 DO UPDATE SET role_id = EXCLUDED.role_id, is_default = EXCLUDED.is_default`,
		`UPDATE inst_roles
		 SET description = CASE lower(code)
		     WHEN 'consultant' THEN '负责患者咨询与跟进'
		     WHEN 'customer_service' THEN '负责电话与在线接待，处理客户咨询'
		     WHEN 'doctor_assistant' THEN '协助医生完成接诊记录与患者沟通'
		     WHEN 'employee' THEN '普通员工角色'
		     WHEN 'marketing_manager' THEN '负责内容运营、客资管理、客户档案与知识库'
		     WHEN 'operating_manager' THEN '负责运营数据、任务与报表管理'
		     WHEN 'therapist' THEN '负责康复治疗与治疗记录'
		     ELSE description
		 END
		 WHERE lower(code) IN (
		     'consultant',
		     'customer_service',
		     'doctor_assistant',
		     'employee',
		     'marketing_manager',
		     'operating_manager',
		     'therapist'
		 )`,
		`UPDATE institution_roles
		 SET description = CASE lower(code)
		     WHEN 'consultant' THEN '负责患者咨询与跟进'
		     WHEN 'customer_service' THEN '负责电话与在线接待，处理客户咨询'
		     WHEN 'doctor_assistant' THEN '协助医生完成接诊记录与患者沟通'
		     WHEN 'employee' THEN '普通员工角色'
		     WHEN 'marketing_manager' THEN '负责内容运营、客资管理、客户档案与知识库'
		     WHEN 'operating_manager' THEN '负责运营数据、任务与报表管理'
		     WHEN 'therapist' THEN '负责康复治疗与治疗记录'
		     ELSE description
		 END
		 WHERE lower(code) IN (
		     'consultant',
		     'customer_service',
		     'doctor_assistant',
		     'employee',
		     'marketing_manager',
		     'operating_manager',
		     'therapist'
		 )`,

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
		`INSERT INTO inst_menus (code, name, path, order_index, is_active, created_at)
		 SELECT 'frontdesk_recordings', '录音列表', '/frontdesk-recordings', 1151, true, NOW()
		 WHERE NOT EXISTS (
		 	SELECT 1 FROM inst_menus WHERE code = 'frontdesk_recordings'
		 )`,
		`UPDATE inst_menus
		    SET name = '录音列表',
		        path = '/frontdesk-recordings',
		        order_index = COALESCE(order_index, 1151),
		        is_active = true
		  WHERE code = 'frontdesk_recordings'`,
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
		      WHEN 'content-workbench' THEN 'content_create'
		      WHEN 'content-list' THEN 'content_library'
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
		      WHEN 'content-workbench' THEN 'content_create'
		      WHEN 'content-list' THEN 'content_library'
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
		`DELETE FROM tenant_feature_group_items
		  WHERE COALESCE(NULLIF(item_type, ''), 'feature') = 'menu'
		    AND COALESCE(NULLIF(item_code, ''), '') NOT IN (
		      SELECT code FROM inst_menus WHERE COALESCE(is_feature_assignable, false) = true
		    )`,
		`DELETE FROM tenant_feature_overrides
		  WHERE COALESCE(NULLIF(item_type, ''), 'feature') = 'menu'
		    AND COALESCE(NULLIF(item_code, ''), '') NOT IN (
		      SELECT code FROM inst_menus WHERE COALESCE(is_feature_assignable, false) = true
		    )`,
		`INSERT INTO inst_role_menus (tenant_id, role_code, menu_id, created_at)
		 SELECT DISTINCT r.tenant_id, 'admin', m.id, NOW()
		 FROM institution_roles r
		 JOIN inst_menus m
		   ON COALESCE(m.is_active, true) = true
		  AND COALESCE(m.is_default_for_admin, false) = true
		 WHERE lower(r.code) = 'admin'
		   AND NOT EXISTS (
		     SELECT 1
		     FROM inst_role_menus rm
		     WHERE rm.tenant_id = r.tenant_id
		       AND lower(rm.role_code) = 'admin'
		       AND rm.menu_id = m.id
		   )`,
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

	slog.Info("compatibility migrations applied", "steps", len(stmts))
	return nil
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
		{Code: "doctor", Version: "v1", Name: "医生录音分析 v1", SceneScope: "post_call_analysis", Description: "default doctor pipeline", ReleaseNote: "builtin pipeline"},
		{Code: "therapist", Version: "v1", Name: "治疗师录音分析 v1", SceneScope: "post_call_analysis", Description: "default therapist pipeline", ReleaseNote: "builtin pipeline"},
		{Code: "frontdesk", Version: "v1", Name: "前台录音分析 v1", SceneScope: "frontdesk_reception", Description: "default frontdesk pipeline", ReleaseNote: "builtin pipeline"},
		{Code: "consultant", Version: "v1", Name: "咨询录音分析 v1", SceneScope: "admission_consult", Description: "default consultant pipeline", ReleaseNote: "builtin pipeline"},
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
					WHEN role_code IN ('doctor', 'doctor_assistant', 'therapist') THEN 'post_call_analysis'
					WHEN role_code IN ('frontdesk', 'reception', 'receptionist') THEN 'frontdesk_reception'
					WHEN role_code IN ('consultant', 'lingce_sales') THEN 'admission_consult'
					WHEN role_code IN ('customer', 'customer_service', 'nurse') THEN 'followup_quality'
					ELSE 'post_call_analysis'
				END AS scene_scope,
				CASE
					WHEN role_code IN ('doctor', 'doctor_assistant') THEN 'doctor'
					WHEN role_code = 'therapist' THEN 'therapist'
					WHEN role_code IN ('frontdesk', 'reception', 'receptionist') THEN 'frontdesk'
					WHEN role_code = 'consultant' THEN 'consultant'
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

	var seededCount int64
	if err := pool.QueryRow(ctx, `SELECT COUNT(*) FROM analysis_role_routes WHERE status = 'published' AND enabled = TRUE`).Scan(&seededCount); err != nil {
		return err
	}
	slog.Info("analysis route defaults ensured", "published_routes", seededCount)
	return nil
}
