-- 20261120_002_ops_admin_menu_codes.sql
-- 运营管理台账号权限：给运营端账号增加「可访问菜单 code」字段（TEXT[]，前端 menuCatalog 的 code）。
-- 与旧的 operations_admin_roles / operations_role_menus 互不影响，本字段只承载新运营台的菜单级授权。
ALTER TABLE operations_admins ADD COLUMN IF NOT EXISTS menu_codes TEXT[] NOT NULL DEFAULT '{}';
