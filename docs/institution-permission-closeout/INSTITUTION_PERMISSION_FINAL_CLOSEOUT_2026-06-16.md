# Institution Permission Final Closeout (2026-06-16)

## Purpose

This document freezes the final runtime state of the institution menu-permission system as of `2026-06-16`.

It answers four concrete questions:

1. What is the current runtime truth model
2. What was fully removed from runtime dependence
3. What legacy references still exist and why
4. What database and migration cleanup should happen next

## Final decision

As of `2026-06-16`, the institution permission runtime in `lingce-api` is considered **fully converged** to the `institution_*` truth model.

This statement is stronger than the earlier `2026-06-12` phase closeout:

1. institution permission runtime no longer depends on `inst_employee_roles`
2. institution permission runtime no longer depends on `inst_role_menus`
3. institution permission runtime no longer writes compatibility mirrors to those legacy tables
4. remaining `inst_*` references in `internal/` are limited to migration compatibility code, not active permission runtime

## Runtime truth model

The active institution permission runtime truth is now:

1. employee current role truth:
   `institution_employee_roles`
2. role definition truth:
   `institution_roles`
3. menu dictionary truth:
   `institution_menus`
4. role-menu grant truth:
   `institution_role_menus`
5. department default role truth:
   `institution_department_roles`
6. tenant upper-bound policy truth:
   `tenant_feature_groups`
   `tenant_feature_group_items`
   `tenant_feature_assignments`
   `tenant_feature_overrides`

The fixed formula remains:

`effective_menu_codes = tenant_allowed_menu_codes ∩ role_granted_menu_codes ∩ active_menu_codes`

## Runtime properties now enforced

### 1. Backend fail-closed

Completed behavior:

1. missing tenant feature assignment no longer means unrestricted
2. missing explicit role-menu grant no longer means inherit tenant menus
3. package-layer implicit role-menu synchronization was removed
4. institution permission runtime now errors if final truth tables required for mainline execution are missing

### 2. Frontend fail-closed

Completed behavior in institution frontend:

1. sidebar filtering now uses final effective menu intersection
2. route guard no longer falls back to tenant-only visibility when explicit role grants are empty
3. role grant page now uses backend menu data instead of local manifest-derived authorization truth

### 3. Employee-role runtime convergence

Completed behavior:

1. institution permission-related role reads were switched to `institution_employee_roles`
2. institution permission-related role writes were switched to `institution_employee_roles`
3. compatibility mirror writes to `inst_employee_roles` were removed from institution permission flows
4. legacy employee-role runtime fallback inside institution RBAC was removed

### 4. Role-menu runtime convergence

Completed behavior:

1. institution RBAC menu-grant runtime now uses `institution_role_menus` only
2. institution tenant-init admin menu grant path now uses `institution_role_menus` only
3. institution RBAC role-menu fallback to `inst_role_menus` was removed
4. institution permission runtime no longer writes compatibility mirrors to `inst_role_menus`

## Verified code-level end state

At closeout time, `rg -n "inst_employee_roles|inst_role_menus" internal` in `lingce-api` shows:

1. no runtime `inst_employee_roles` dependency under `internal/`
2. no runtime `inst_role_menus` dependency under `internal/`
3. only migration compatibility references remain in:
   `internal/store/compat_migration.go`

This is the required threshold for declaring permission runtime convergence complete.

## Build verification

Local verification completed on `2026-06-16`:

1. `go build ./...` in `lingce-api` passed
2. institution frontend production build passed after permission fail-closed changes

## What still exists intentionally

The following legacy items may still physically exist in database or compatibility code, but are no longer runtime truths for institution permission:

1. database tables:
   `inst_employee_roles`
   `inst_role_menus`
   `inst_menus`
   `inst_roles`
2. migration compatibility code:
   `internal/store/compat_migration.go`

These remaining items are not blockers for permission correctness anymore.

## Database cleanup recommendation

Recommended cleanup sequence after rollout confidence:

### Phase A: read-only audit freeze

1. export row counts and schema snapshots for:
   `institution_employee_roles`
   `institution_role_menus`
   `institution_menus`
   `inst_employee_roles`
   `inst_role_menus`
2. verify no institution permission runtime service is still reading legacy tables
3. keep legacy tables read-only during observation window

### Phase B: compatibility code retirement

1. remove `internal/store/compat_migration.go` branches that still backfill or inspect `inst_role_menus`
2. remove any deployment or repair scripts that assume legacy permission tables are still active runtime stores
3. freeze schema ownership docs so new work cannot reintroduce `inst_*` runtime reads

### Phase C: legacy table decommissioning

Only after audit and rollback confidence:

1. archive `inst_role_menus`
2. archive `inst_employee_roles`
3. archive or drop `inst_menus` only after confirming no non-permission runtime still depends on it
4. archive or drop `inst_roles` only after confirming no legacy role-definition fallback remains

## What is out of scope even now

This final closeout still does **not** include:

1. worker-side doctor specialty hint field redesign
2. complete deletion of every old `inst_*` database object
3. unrelated non-permission legacy runtime cleanup outside the institution permission system

## Final statement

As of `2026-06-16`, the institution menu-permission runtime is fully closed on the `institution_*` model.

The remaining work is no longer permission-runtime convergence.

It is now strictly:

1. migration compatibility retirement
2. database decommission planning
3. optional physical legacy table cleanup
