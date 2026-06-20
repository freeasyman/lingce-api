-- Institution menu permission unification prepare DDL
-- Purpose:
-- 1) Create final institution_* permission tables
-- 2) Add final business-key columns and constraints
-- 3) Avoid cutting over reads/writes in this step
--
-- This script is intentionally non-destructive.

BEGIN;

-- =========================================================
-- A. institution_employee_roles
-- Final truth target:
--   tenant_id + employee_id -> role_code
-- =========================================================

ALTER TABLE institution_employee_roles
  ADD COLUMN IF NOT EXISTS tenant_id bigint,
  ADD COLUMN IF NOT EXISTS role_code varchar(64),
  ADD COLUMN IF NOT EXISTS source varchar(32),
  ADD COLUMN IF NOT EXISTS updated_at timestamp without time zone;

UPDATE institution_employee_roles
SET updated_at = COALESCE(updated_at, created_at, NOW())
WHERE updated_at IS NULL;

CREATE INDEX IF NOT EXISTS idx_institution_employee_roles_tenant_employee
  ON institution_employee_roles (tenant_id, employee_id);

CREATE INDEX IF NOT EXISTS idx_institution_employee_roles_tenant_role_code
  ON institution_employee_roles (tenant_id, role_code);

DO $$
BEGIN
  IF NOT EXISTS (
    SELECT 1
    FROM pg_constraint
    WHERE conname = 'uq_institution_employee_roles_tenant_employee'
  ) THEN
    ALTER TABLE institution_employee_roles
      ADD CONSTRAINT uq_institution_employee_roles_tenant_employee
      UNIQUE (tenant_id, employee_id);
  END IF;
END $$;

-- =========================================================
-- B. institution_menus
-- Final menu dictionary truth
-- =========================================================

CREATE TABLE IF NOT EXISTS institution_menus (
  id bigserial PRIMARY KEY,
  tenant_id bigint,
  code varchar(128) NOT NULL,
  name text NOT NULL,
  path text,
  icon text,
  parent_id bigint,
  parent_code varchar(128),
  sort_order integer NOT NULL DEFAULT 0,
  is_active boolean NOT NULL DEFAULT true,
  is_feature_assignable boolean NOT NULL DEFAULT false,
  is_default_for_admin boolean NOT NULL DEFAULT false,
  feature_code varchar(128),
  feature_name varchar(128),
  created_at timestamp without time zone NOT NULL DEFAULT NOW(),
  updated_at timestamp without time zone NOT NULL DEFAULT NOW(),
  deleted_at timestamp without time zone
);

CREATE INDEX IF NOT EXISTS idx_institution_menus_tenant_code
  ON institution_menus (tenant_id, code);

CREATE INDEX IF NOT EXISTS idx_institution_menus_parent_code
  ON institution_menus (parent_code);

CREATE INDEX IF NOT EXISTS idx_institution_menus_feature_code
  ON institution_menus (feature_code);

CREATE UNIQUE INDEX IF NOT EXISTS uq_institution_menus_tenant_code_active
  ON institution_menus (COALESCE(tenant_id, 0), code)
  WHERE deleted_at IS NULL;

-- =========================================================
-- C. institution_role_menus
-- Final role-menu grant truth
-- =========================================================

CREATE TABLE IF NOT EXISTS institution_role_menus (
  tenant_id bigint NOT NULL,
  role_code varchar(64) NOT NULL,
  menu_code varchar(128) NOT NULL,
  created_at timestamp without time zone NOT NULL DEFAULT NOW(),
  updated_at timestamp without time zone NOT NULL DEFAULT NOW(),
  PRIMARY KEY (tenant_id, role_code, menu_code)
);

CREATE INDEX IF NOT EXISTS idx_institution_role_menus_tenant_role
  ON institution_role_menus (tenant_id, role_code);

CREATE INDEX IF NOT EXISTS idx_institution_role_menus_tenant_menu
  ON institution_role_menus (tenant_id, menu_code);

-- =========================================================
-- D. institution_department_roles
-- Final department default role truth
-- =========================================================

ALTER TABLE institution_department_roles
  ADD COLUMN IF NOT EXISTS tenant_id bigint,
  ADD COLUMN IF NOT EXISTS role_code varchar(64),
  ADD COLUMN IF NOT EXISTS is_default boolean NOT NULL DEFAULT true,
  ADD COLUMN IF NOT EXISTS updated_at timestamp without time zone;

UPDATE institution_department_roles
SET updated_at = COALESCE(updated_at, created_at, NOW())
WHERE updated_at IS NULL;

CREATE INDEX IF NOT EXISTS idx_institution_department_roles_tenant_department
  ON institution_department_roles (tenant_id, department_id);

CREATE INDEX IF NOT EXISTS idx_institution_department_roles_tenant_role_code
  ON institution_department_roles (tenant_id, role_code);

DO $$
BEGIN
  IF NOT EXISTS (
    SELECT 1
    FROM pg_constraint
    WHERE conname = 'uq_institution_department_roles_tenant_department'
  ) THEN
    ALTER TABLE institution_department_roles
      ADD CONSTRAINT uq_institution_department_roles_tenant_department
      UNIQUE (tenant_id, department_id);
  END IF;
END $$;

-- =========================================================
-- E. Integrity helpers for cutover phase
-- =========================================================

CREATE INDEX IF NOT EXISTS idx_institution_roles_tenant_code_active
  ON institution_roles (tenant_id, code)
  WHERE deleted_at IS NULL;

COMMENT ON TABLE institution_menus IS
  'Final menu dictionary truth for institution permission unification.';

COMMENT ON TABLE institution_role_menus IS
  'Final role-to-menu grant truth for institution permission unification.';

COMMENT ON COLUMN institution_employee_roles.role_code IS
  'Final business-key role fact. role_id is transitional and must be retired in cutover.';

COMMENT ON COLUMN institution_department_roles.role_code IS
  'Final business-key default role fact. role_id is transitional and must be retired in cutover.';

COMMIT;
