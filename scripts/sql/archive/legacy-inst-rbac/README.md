# legacy-inst-rbac

These SQL files are preserved only for historical audit, rollback research, and
data forensics around the old `inst_*` institution permission model.

They are archived because the current dev environment has been cut over to the
`institution_*` runtime model, and the legacy tables:

- `inst_employee_roles`
- `inst_role_menus`

are intentionally no longer available by their original names in dev.

Do not run these scripts as part of normal dev setup or current permission
operations.
