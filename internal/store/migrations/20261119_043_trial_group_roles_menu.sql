-- 043: 试用功能包补齐「角色权限」菜单
--
-- 现象：机构管理员打开门户 /system 时，GET /api/v1/roles?scope=institution 返回 403，
--       前端提示「当前账号没有执行此操作的权限」。
-- 原因：租户功能组里只有 menu|departments，没有 menu|roles；机构侧菜单鉴权按功能组
--       过滤，roles 被排除。付费组（含 departments/menus/roles）不受影响，试用组缺项。
-- 处理：所有已包含 menu|departments 的功能组补齐 menu|roles（幂等，不重复插入）。

INSERT INTO tenant_feature_group_items (group_id, item_type, item_code, is_enabled)
SELECT g.id, 'menu', 'roles', TRUE
FROM tenant_feature_groups g
WHERE EXISTS (
    SELECT 1 FROM tenant_feature_group_items d
    WHERE d.group_id = g.id AND d.item_type = 'menu' AND d.item_code = 'departments'
)
AND NOT EXISTS (
    SELECT 1 FROM tenant_feature_group_items r
    WHERE r.group_id = g.id AND r.item_type = 'menu' AND r.item_code = 'roles'
);
