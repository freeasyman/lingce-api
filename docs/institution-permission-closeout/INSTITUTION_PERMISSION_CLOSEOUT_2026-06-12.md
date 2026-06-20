# Institution Permission Closeout (2026-06-12)

## Purpose

This document records the formal closeout boundary for the current institution menu-permission unification task.

It exists to answer three questions clearly:

1. What was completed in this task
2. What was intentionally left out of scope
3. Whether the current task can be considered closed

## Final closeout decision

The current task is considered **closed** under the scope:

`institution menu-permission system unification`

This closeout does **not** mean every historical use of `inst_employee_roles` has been removed from every runtime in the wider system.

It means the institution-side permission system itself has been successfully converged to the new `institution_*` truth model, and remaining non-permission uses have been explicitly split out as a separate follow-up concern.

## Scope completed

### 1. Permission truth convergence in `lingce-api`

The API-side institution permission chain has been converged to the `institution_*` model:

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
6. tenant upper-bound menu policy:
   `tenant_feature_*`

### 2. Backend permission computation convergence

The backend now owns final institution menu decision logic.

Completed behavior:

1. tenant package / feature-group absence is handled fail-closed
2. package-layer implicit role-menu synchronization was removed
3. role permissions are filtered by tenant package policy on backend
4. current employee effective menus are exposed by:
   `GET /api/v1/menus/effective?scope=institution`
5. effective menu computation is based on:
   `tenant_allowed_menu_codes ∩ role_granted_menu_codes ∩ active_menu_codes`

### 3. Employee-role runtime convergence

Within `lingce-api`, normal institution permission runtime no longer depends on `inst_employee_roles` as the active truth.

Completed behavior:

1. employee-role reads were switched to `institution_employee_roles`
2. explicit RBAC fallback to legacy employee-role rows was removed when the new table exists
3. RBAC employee-role writes no longer dual-write to `inst_employee_roles`
4. tenant-init default admin employee-role writes no longer dual-write to `inst_employee_roles`

### 4. Development database convergence

The development database permission data required for cutover was prepared and backfilled.

Completed database work:

1. final structures prepared
2. missing final tables created
3. menus backfilled
4. role-menu grants backfilled
5. employee roles backfilled
6. department roles backfilled
7. hidden invalid legacy roles excluded from new truth
8. permission-related verification and reconciliation executed

See also:

1. [INSTITUTION_PERMISSION_UNIFICATION_DEV_EXECUTION_LOG_2026-06-07.md](/Users/yiliiang/Documents/lingce-api/docs/INSTITUTION_PERMISSION_UNIFICATION_DEV_EXECUTION_LOG_2026-06-07.md)
2. [INSTITUTION_PERMISSION_CODE_CUTOVER_CHECKLIST.md](/Users/yiliiang/Documents/lingce-api/docs/INSTITUTION_PERMISSION_CODE_CUTOVER_CHECKLIST.md)

### 5. Worker-side role-code migration progress

Worker/runtime reads that depend directly on employee `role_code` were substantially migrated away from `inst_employee_roles`.

Completed local worker migration work:

1. `../lingce-worker` role-code reads for route resolution and recording-role inference were switched to `institution_employee_roles`
2. `../recording-worker` role-code reads for customer/frontdesk/consultant/therapist/lingce_sales/doctor matching and route audit were switched to `institution_employee_roles`
3. local `go build ./...` passed for:
   `lingce-api`
   `lingce-worker`
   `recording-worker`

## Out of scope / intentionally not closed here

### Doctor specialty legacy fields

The following legacy fields were **not** included in the permission closeout boundary:

1. `medical_specialty_code`
2. `specialty_group`

Reason:

1. they are not permission facts
2. they are not part of the finalized `institution_employee_roles` target model
3. in current worker behavior they are used only as doctor-analysis routing hints / prompt inputs
4. they therefore belong to a separate doctor-analysis data-model task, not to menu-permission convergence

### Remaining runtime legacy dependency

At closeout time, remaining runtime source-code dependency on `inst_employee_roles` is narrowed to doctor specialty lookup paths:

1. `../lingce-worker/internal/analysis/doctor.go`
2. `../recording-worker/internal/pipeline/doctor/pipeline.go`

These reads are retained only because they access legacy specialty fields, not because permission truth still depends on the old table.

## Explicit closeout boundary

This task is considered complete when evaluated by the following standard:

1. institution permission truth is unified
2. institution menu decision logic is unified
3. employee current-role permission truth is unified
4. API-side permission runtime no longer relies on legacy employee-role truth
5. remaining doctor specialty legacy usage is tracked as a follow-up task, not as an open blocker for permission convergence

This task is **not** blocked by:

1. doctor specialty migration design
2. specialty-field storage redesign
3. complete physical retirement of every non-permission historical column on `inst_employee_roles`

## Follow-up task to open separately

Recommended separate follow-up title:

`Doctor specialty hint source decoupling from inst_employee_roles`

Recommended objective:

1. decide whether doctor analysis should stop using employee-level specialty hints entirely
2. or introduce a dedicated doctor-profile truth
3. then remove the final specialty-field runtime reads from worker code

## Final statement

The institution menu-permission unification task is closed as of `2026-06-12`.

Remaining legacy specialty-field handling is a separate follow-up task and should not be used to reopen or redefine the completed permission-convergence work.
