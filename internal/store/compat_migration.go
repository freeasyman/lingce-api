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

		// Organization module compatibility
		`ALTER TABLE IF EXISTS tenants ADD COLUMN IF NOT EXISTS deleted_at TIMESTAMP`,
		`ALTER TABLE IF EXISTS tenants ADD COLUMN IF NOT EXISTS is_active BOOLEAN NOT NULL DEFAULT TRUE`,
		`ALTER TABLE IF EXISTS tenants ADD COLUMN IF NOT EXISTS valid_from TIMESTAMP`,
		`ALTER TABLE IF EXISTS tenants ADD COLUMN IF NOT EXISTS valid_to TIMESTAMP`,
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

		// Sysconfig compatibility
		`ALTER TABLE IF EXISTS tenant_subscription_plans ADD COLUMN IF NOT EXISTS description TEXT`,
		`ALTER TABLE IF EXISTS tenant_subscription_plans ADD COLUMN IF NOT EXISTS duration_days INTEGER NOT NULL DEFAULT 30`,
		`ALTER TABLE IF EXISTS tenant_subscription_plans ADD COLUMN IF NOT EXISTS price NUMERIC(10,2) NOT NULL DEFAULT 0`,
		`ALTER TABLE IF EXISTS tenant_subscription_plans ADD COLUMN IF NOT EXISTS sort_order INTEGER NOT NULL DEFAULT 0`,
		`ALTER TABLE IF EXISTS tenant_subscription_plans ADD COLUMN IF NOT EXISTS is_active BOOLEAN NOT NULL DEFAULT TRUE`,
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
		`ALTER TABLE IF EXISTS tenant_subscription_events ADD COLUMN IF NOT EXISTS operator_id BIGINT`,
		`ALTER TABLE IF EXISTS tenant_subscription_events ADD COLUMN IF NOT EXISTS operator_type TEXT`,
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
	}

	for i, stmt := range stmts {
		if _, err := pool.Exec(ctx, stmt); err != nil {
			return fmt.Errorf("compat migration failed at step %d: %w", i+1, err)
		}
	}

	slog.Info("compatibility migrations applied", "steps", len(stmts))
	return nil
}
