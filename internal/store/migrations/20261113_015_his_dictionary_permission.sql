-- 015：字典维护权限点 dictionary（role_modules 模块键，不占导航）。
--
-- 默认口径：tenant 0 模板只有 admin=true；其他 8 个角色没有行，即默认无字典维护权限。
-- admin 在代码里固定拥有全部模块（HISModules 含 dictionary），这里补一行只用于显式声明与审计；
-- 租户可在「角色与模块」里为 doctor/nurse 等角色勾选「字典维护」，写入租户覆盖行即可生效。
INSERT INTO his_role_modules (tenant_id, role, module) VALUES
  (0, 'admin', 'dictionary')
ON CONFLICT (tenant_id, role, module) DO NOTHING;
