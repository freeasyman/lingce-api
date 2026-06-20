# Institution Permission Unification Dev Execution Log (2026-06-07)

## Scope
- Environment: development database only (`lingce_dev`)
- Production database: not touched
- Production deployment: not performed
- Code changes: local only in `lingce-api`

## Executed SQL
1. `scripts/sql/2026-06-07_prepare_institution_permission_unification.sql`
2. `scripts/sql/2026-06-07_backfill_institution_roles.sql`
3. `scripts/sql/2026-06-07_backfill_institution_menus.sql`
4. `scripts/sql/2026-06-07_backfill_institution_role_menus.sql`
5. `scripts/sql/2026-06-07_backfill_institution_employee_roles.sql`
6. `scripts/sql/2026-06-07_backfill_institution_department_roles.sql`

## Real schema differences discovered in dev
### 1. `institution_role_menus` and `institution_menus` did not exist before prepare
The development database was behind the intended target schema. `prepare` created the missing final tables.

### 2. `institution_employee_roles` and `institution_department_roles` were transitional old versions
Before `prepare`, they lacked:
- `tenant_id`
- `role_code`
- `source` / `updated_at` on employee-role table
- `role_code` / `updated_at` on department-role table

### 3. `inst_roles` is a global template table, not a tenant fact table
Actual dev schema:
- no `tenant_id`
- no `id`

This invalidated the original `backfill_institution_roles.sql` assumption and required rewriting the script into a template-sync script.

### 4. `inst_menus` has no `updated_at`
The menu backfill script originally assumed an `updated_at` column on legacy menus. This was corrected.

### 5. `institution_employee_roles.role_id` and `institution_department_roles.role_id` are still `NOT NULL`
This means any backfill row without a resolvable target `institution_roles.id` will fail. Hidden/invalid roles had to be explicitly excluded.

## Data-quality issues discovered in dev
### Hidden legacy role still present in old truth
Found legacy rows with role code:
- `lingce_sales`

This role is explicitly excluded by the code path and should not enter the final institution-side permission truth.

Affected rows found during execution:
- employee role: `tenant_id=1, employee_id=6, role_code=lingce_sales`
- department role: `tenant_id=1, department_id=16, role_code=lingce_sales`

The backfill scripts were updated to skip:
- `lingce_sales`
- `inst_test_225052`

## Script corrections made during execution
### `2026-06-07_backfill_institution_roles.sql`
Changed from tenant-based legacy import to global-template sync:
- insert global template rows into `institution_roles` with `tenant_id=0`
- update existing `institution_roles` rows by `code`

### `2026-06-07_backfill_institution_menus.sql`
Removed dependency on non-existent `inst_menus.updated_at`.

### `2026-06-07_backfill_institution_employee_roles.sql`
Corrected multiple issues:
- now updates `role_id` as well as `tenant_id`, `role_code`, `source`
- no longer preserves stale mismatched `role_id`
- fixed CTE scoping bug
- excludes hidden invalid legacy roles
- only inserts rows with resolvable `institution_roles.id`

### `2026-06-07_backfill_institution_department_roles.sql`
Corrected multiple issues:
- now treats legacy business key (`tenant_id + department_id + role_code`) as source truth
- updates `role_id`, `tenant_id`, `role_code`, `is_default`
- fixed CTE scoping bug
- excludes hidden invalid legacy roles
- only inserts rows with resolvable `institution_roles.id`

## Result after execution
### Row counts
- `institution_menus = 102`
- `inst_menus = 102`
- `institution_role_menus = 1637`
- `inst_role_menus = 1637`
- `institution_department_roles = 23`
- `inst_department_roles = 7`
- `institution_employee_roles = 70`
- `inst_employee_roles = 74`

### Verified alignment
#### Employee roles
Using the final deduplicated old truth, excluding hidden invalid roles:
- missing in new: `0`
- role code mismatches: `0`

#### Department roles
Using the old truth, excluding hidden invalid roles:
- missing in new: `0`
- role code mismatches: `0`

#### Null tenant IDs
- `institution_employee_roles.tenant_id IS NULL`: `0`
- `institution_department_roles.tenant_id IS NULL`: `0`

## Residual gap explanation
`institution_employee_roles` row count is not expected to equal raw `inst_employee_roles` row count because old truth contains multiple rows for some employees.

Observed breakdown:
- raw legacy rows: `74`
- deduplicated per employee effective legacy rows: `69`
- excluded hidden invalid roles: `1`
- resulting expected migrated legacy rows: `68`
- actual new rows: `70`

One confirmed extra row exists only in the new table:
- `tenant_id=6, employee_id=109, role_code=admin`

This appears to be pre-existing new-table data rather than a migration miss. It should be reviewed in the later cleanup phase, but it does not block the current truth cutover work because it does not create a legacy mismatch.

## Second-batch read cutover progress
The following direct business read paths were switched from `inst_employee_roles` to `institution_employee_roles`:
- `internal/rbac/checker.go`
- `internal/employee/store.go`
- `internal/sandbox/service.go`
- `internal/recording/store.go`
- `internal/recording/service.go`
- `internal/badge/store.go`
- `internal/mobile/service.go`
- `internal/recording/handler.go`

Role reads that need the latest assignment now use:
- `ORDER BY COALESCE(updated_at, created_at) DESC`

After this cutover, remaining `inst_employee_roles` references are intentionally limited to:
- one final legacy read branch used only when `institution_employee_roles` does not exist at all

## Code state aligned with this execution
Local code now does all of the following:
- fail-closed when tenant package/feature-group assignment is absent
- remove package-layer implicit role-menu synchronization
- prefer `institution_role_menus` / `institution_menus` when present, fallback otherwise
- prefer `institution_employee_roles` for RBAC read path with legacy fallback
- dual-write employee-role mutations to preserve legacy readers during transition
- dual-write tenant-admin initialization into both old and new employee/menu grant stores
- centralize tenant-effective menu filtering in backend service layer
- filter role permission output by tenant package policy on backend
- expose unified current-employee effective menu output at `GET /api/v1/menus/effective?scope=institution`
- compute employee effective menus on backend from:
  `tenant_allowed_menu_codes ∩ role_granted_menu_codes ∩ active_menu_codes`
- include ancestor menus in the returned visible menu set so institution navigation can consume one backend truth
- stop falling back to `inst_employee_roles` reads inside `internal/rbac/store_institution.go` when the new employee-role table exists
- stop dual-writing employee current-role facts to `inst_employee_roles` in RBAC mutations
- stop dual-writing tenant-init default admin employee roles to `inst_employee_roles`

## Next recommended implementation step
Current highest-priority next steps:
1. migrate institution frontend navigation / route guards to `GET /api/v1/menus/effective?scope=institution`
2. verify external worker / service repos no longer depend on `inst_employee_roles`
3. remove the final “table-absent only” legacy read branch after cross-repo verification
4. only then plan destructive cleanup of old tables and old compatibility columns

## Cross-repo dependency audit status
As of the latest local audit, the final `inst_employee_roles` compatibility read in `lingce-api` cannot be removed yet because external worker repos still depend on the legacy table directly.

Confirmed direct dependencies found locally:
- `../lingce-worker/internal/repository/routes.go`
- `../lingce-worker/internal/repository/recordings.go`
- `../lingce-worker/internal/analysis/doctor.go`
- `../recording-worker/internal/engine/analysis_audit.go`
- `../recording-worker/internal/pipeline/customer/pipeline.go`
- `../recording-worker/internal/pipeline/frontdesk/pipeline.go`
- `../recording-worker/internal/pipeline/doctor/pipeline.go`
- `../recording-worker/internal/pipeline/consultant/pipeline.go`
- `../recording-worker/internal/pipeline/therapist/pipeline.go`
- `../recording-worker/internal/pipeline/lingce_sales/pipeline.go`

Meaning:
- `lingce-api` local main read/write cutover is effectively complete for employee current-role facts
- destructive cleanup of `inst_employee_roles` is blocked by cross-repo worker migration
- the last API-side compatibility branch should remain until worker repos are switched to `institution_employee_roles`

## Cross-repo migration progress
During the latest local continuation, the following worker-side runtime reads were switched from `inst_employee_roles` to `institution_employee_roles`:

- `../lingce-worker/internal/repository/routes.go`
- `../lingce-worker/internal/repository/recordings.go`
- `../lingce-worker/internal/analysis/doctor.go` (role-code reads / admin recipient reads)
- `../recording-worker/internal/engine/analysis_audit.go`
- `../recording-worker/internal/pipeline/customer/pipeline.go`
- `../recording-worker/internal/pipeline/frontdesk/pipeline.go`
- `../recording-worker/internal/pipeline/consultant/pipeline.go`
- `../recording-worker/internal/pipeline/therapist/pipeline.go`
- `../recording-worker/internal/pipeline/lingce_sales/pipeline.go`
- `../recording-worker/internal/pipeline/doctor/pipeline.go` (role-code reads)
- `../recording-worker/internal/pipeline/shared/doctor_postprocess.go`

Post-change status:
- `go build ./...` passed in local `../lingce-worker`
- `go build ./...` passed in local `../recording-worker`
- remaining runtime source-code references to `inst_employee_roles` are now narrowed to doctor specialty lookup paths only:
  - `../lingce-worker/internal/analysis/doctor.go`
  - `../recording-worker/internal/pipeline/doctor/pipeline.go`

Current blocker:
- these two doctor specialty paths still read legacy columns `medical_specialty_code` / `specialty_group`
- those columns are not part of the finalized `institution_employee_roles` target model
- final removal of the last API-side compatibility branch therefore still requires a design decision for specialty truth/source
