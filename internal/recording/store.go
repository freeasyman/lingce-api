package recording

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/freeasyman/lingce-api/internal/menuaccess"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

type Store struct {
	pool *pgxpool.Pool
}

type RecordingMediaRef struct {
	RecordingID int64
	TenantID    int64
	FileURL     string
	FileName    string
	OSSKey      string
}

type TrialTenantProfile struct {
	TenantID            int64
	AccountMode         string
	ValidTo             *time.Time
	TrialMaxRecordings  int
	TrialUsedRecordings int
}

func (s *Store) GetTrialAgreementStatus(ctx context.Context, tenantID int64, agreementType, agreementVersion string) (*TrialAgreementStatusResponse, error) {
	status := &TrialAgreementStatusResponse{
		AgreementType:    strings.TrimSpace(agreementType),
		AgreementVersion: strings.TrimSpace(agreementVersion),
		Accepted:         false,
	}
	if status.AgreementType == "" {
		status.AgreementType = "trial_privacy_notice"
	}
	if status.AgreementVersion == "" {
		status.AgreementVersion = "v1"
	}

	var accepted bool
	var acceptedAt sql.NullTime
	err := s.pool.QueryRow(ctx, `
		SELECT accepted, accepted_at
		FROM tenant_trial_agreements
		WHERE tenant_id = $1
		  AND agreement_type = $2
		  AND agreement_version = $3
		LIMIT 1
	`, tenantID, status.AgreementType, status.AgreementVersion).Scan(&accepted, &acceptedAt)
	if err != nil {
		if err == pgx.ErrNoRows {
			return status, nil
		}
		return nil, fmt.Errorf("failed to get trial agreement status: %w", err)
	}
	status.Accepted = accepted
	if acceptedAt.Valid {
		v := acceptedAt.Time.Format(time.RFC3339)
		status.AcceptedAt = &v
	}
	return status, nil
}

func (s *Store) AcceptTrialAgreement(ctx context.Context, tenantID, acceptedBy int64, agreementType, agreementVersion string) error {
	agreementType = strings.TrimSpace(agreementType)
	agreementVersion = strings.TrimSpace(agreementVersion)
	if agreementType == "" {
		agreementType = "trial_privacy_notice"
	}
	if agreementVersion == "" {
		agreementVersion = "v1"
	}
	_, err := s.pool.Exec(ctx, `
		INSERT INTO tenant_trial_agreements (
			tenant_id, agreement_type, agreement_version, accepted, accepted_at, accepted_by, created_at, updated_at
		) VALUES (
			$1, $2, $3, TRUE, NOW(), $4, NOW(), NOW()
		)
		ON CONFLICT (tenant_id, agreement_type, agreement_version)
		DO UPDATE SET
			accepted = TRUE,
			accepted_at = NOW(),
			accepted_by = EXCLUDED.accepted_by,
			updated_at = NOW()
	`, tenantID, agreementType, agreementVersion, acceptedBy)
	if err != nil {
		return fmt.Errorf("failed to accept trial agreement: %w", err)
	}
	return nil
}

var (
	doctorScopeRoleCodes     = []string{"doctor", "doctor_assistant"}
	consultantScopeRoleCodes = []string{"consultant"}
	frontdeskScopeRoleCodes  = []string{"frontdesk", "receptionist", "reception"}
	therapistScopeRoleCodes  = []string{"therapist"}
)

func NewStore(pool *pgxpool.Pool) *Store {
	return &Store{pool: pool}
}

func (s *Store) GetTrialTenantProfile(ctx context.Context, tenantID int64) (*TrialTenantProfile, error) {
	hasRecordingDeletedAt := false
	if err := s.pool.QueryRow(ctx, `
		SELECT EXISTS (
			SELECT 1
			FROM information_schema.columns
			WHERE table_schema = 'public'
			  AND table_name = 'recordings'
			  AND column_name = 'deleted_at'
		)
	`).Scan(&hasRecordingDeletedAt); err != nil {
		return nil, fmt.Errorf("failed to inspect recordings.deleted_at: %w", err)
	}

	recordingCountFilter := ""
	if hasRecordingDeletedAt {
		recordingCountFilter = "AND r.deleted_at IS NULL"
	}

	var profile TrialTenantProfile
	var validTo sql.NullTime
	query := fmt.Sprintf(`
		SELECT t.id,
		       COALESCE(NULLIF(trim(t.account_mode), ''), 'formal') AS account_mode,
		       COALESCE(t.valid_to, t.service_expired_on) AS valid_to,
		       5 AS trial_max_recordings,
		       (
		         SELECT COUNT(*)
		         FROM recordings r
		         WHERE r.tenant_id = t.id
		           %s
		       ) AS trial_used_recordings
		FROM tenants t
		WHERE t.id = $1
		  AND t.deleted_at IS NULL
		LIMIT 1
	`, recordingCountFilter)
	err := s.pool.QueryRow(ctx, query, tenantID).Scan(&profile.TenantID, &profile.AccountMode, &validTo, &profile.TrialMaxRecordings, &profile.TrialUsedRecordings)
	if err != nil {
		return nil, fmt.Errorf("failed to get trial tenant profile: %w", err)
	}
	if validTo.Valid {
		t := validTo.Time
		profile.ValidTo = &t
	}
	return &profile, nil
}

func (s *Store) GetTrialEmployeeID(ctx context.Context, tenantID int64, usernamePrefix string) (int64, error) {
	var employeeID int64
	err := s.pool.QueryRow(ctx, `
		SELECT id
		FROM employees
		WHERE tenant_id = $1
		  AND deleted_at IS NULL
		  AND username = $2
		ORDER BY id ASC
		LIMIT 1
	`, tenantID, fmt.Sprintf("%s_%d", strings.TrimSpace(usernamePrefix), tenantID)).Scan(&employeeID)
	if err != nil {
		return 0, fmt.Errorf("failed to get trial employee %s: %w", usernamePrefix, err)
	}
	return employeeID, nil
}

func (s *Store) getLatestEmployeeRoleCode(ctx context.Context, employeeID int64) (int64, string, error) {
	var tenantID int64
	var roleCode string
	err := s.pool.QueryRow(ctx, `
		SELECT er.tenant_id, lower(trim(er.role_code)) AS role_code
		FROM institution_employee_roles er
		JOIN employees e
		  ON e.id = er.employee_id
		 AND e.tenant_id = er.tenant_id
		 AND e.deleted_at IS NULL
		WHERE er.employee_id = $1
		  AND trim(COALESCE(er.role_code, '')) <> ''
		ORDER BY er.updated_at DESC NULLS LAST, er.created_at DESC, er.id DESC
		LIMIT 1
	`, employeeID).Scan(&tenantID, &roleCode)
	if err != nil {
		return 0, "", err
	}
	return tenantID, roleCode, nil
}

func (s *Store) isTenantMenuAllowed(ctx context.Context, tenantID int64, menuCode string) (bool, error) {
	menuCode = strings.ToLower(strings.TrimSpace(menuCode))
	if menuCode == "" {
		return false, nil
	}

	var groupID *int64
	err := s.pool.QueryRow(ctx, `
		SELECT g.id
		FROM tenant_feature_assignments a
		JOIN tenant_feature_groups g ON g.id = a.group_id
		WHERE a.tenant_id = $1
		  AND CASE
		      WHEN g.is_active::text IN ('1','t','true','TRUE') THEN true
		      ELSE false
		  END = true
	`, tenantID).Scan(&groupID)
	if err != nil && err != pgx.ErrNoRows {
		return false, fmt.Errorf("query tenant feature assignment: %w", err)
	}
	if groupID == nil {
		return false, nil
	}

	menuDefs, err := s.listTenantMenuDefinitions(ctx, tenantID)
	if err != nil {
		return false, err
	}

	items := make([]menuaccess.PolicyItem, 0)
	groupRows, err := s.pool.Query(ctx, `
		SELECT COALESCE(NULLIF(item_type, ''), 'feature') AS item_type,
		       lower(trim(COALESCE(NULLIF(item_code, ''), COALESCE(feature_code, '')))) AS code,
		       COALESCE(is_enabled, true) AS is_enabled
		FROM tenant_feature_group_items
		WHERE group_id = $1
	`, *groupID)
	if err != nil {
		return false, fmt.Errorf("query feature-group items: %w", err)
	}
	defer groupRows.Close()
	for groupRows.Next() {
		var itemType, code string
		var enabled bool
		if err := groupRows.Scan(&itemType, &code, &enabled); err != nil {
			return false, fmt.Errorf("scan feature-group item: %w", err)
		}
		items = append(items, menuaccess.PolicyItem{ItemType: itemType, ItemCode: code, Enabled: enabled})
	}
	if err := groupRows.Err(); err != nil {
		return false, fmt.Errorf("iterate feature-group items: %w", err)
	}

	overrides := make([]menuaccess.PolicyOverride, 0)
	overrideRows, err := s.pool.Query(ctx, `
		SELECT COALESCE(NULLIF(item_type, ''), 'feature') AS item_type,
		       lower(trim(COALESCE(NULLIF(item_code, ''), COALESCE(feature_code, '')))) AS code,
		       COALESCE(NULLIF(override_mode, ''), CASE WHEN COALESCE(is_enabled, true) THEN 'allow' ELSE 'deny' END) AS override_mode
		FROM tenant_feature_overrides
		WHERE tenant_id = $1
	`, tenantID)
	if err != nil {
		return false, fmt.Errorf("query feature overrides: %w", err)
	}
	defer overrideRows.Close()
	for overrideRows.Next() {
		var itemType, code, mode string
		if err := overrideRows.Scan(&itemType, &code, &mode); err != nil {
			return false, fmt.Errorf("scan feature override: %w", err)
		}
		overrides = append(overrides, menuaccess.PolicyOverride{ItemType: itemType, ItemCode: code, OverrideMode: mode})
	}
	if err := overrideRows.Err(); err != nil {
		return false, fmt.Errorf("iterate feature overrides: %w", err)
	}

	allowed := menuaccess.ResolveAllowedMenuCodes(menuDefs, items, overrides)
	_, ok := allowed[menuCode]
	return ok, nil
}

func (s *Store) listTenantMenuDefinitions(ctx context.Context, tenantID int64) ([]menuaccess.MenuDefinition, error) {
	var institutionMenusExists bool
	if err := s.pool.QueryRow(ctx, "SELECT to_regclass('public.institution_menus') IS NOT NULL").Scan(&institutionMenusExists); err != nil {
		return nil, fmt.Errorf("detect institution menu table: %w", err)
	}

	query := `
		SELECT lower(trim(code)) AS code,
		       lower(trim(COALESCE(feature_code, ''))) AS feature_code
		FROM inst_menus
		WHERE COALESCE(is_active, true) = true
	`
	args := []interface{}{}
	if institutionMenusExists {
		query = `
			SELECT lower(trim(code)) AS code,
			       lower(trim(COALESCE(feature_code, ''))) AS feature_code
			FROM institution_menus
			WHERE deleted_at IS NULL
			  AND COALESCE(is_active, true) = true
			  AND (tenant_id = $1 OR tenant_id IS NULL)
		`
		args = append(args, tenantID)
	}

	rows, err := s.pool.Query(ctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("query tenant menu definitions: %w", err)
	}
	defer rows.Close()

	defs := make([]menuaccess.MenuDefinition, 0)
	for rows.Next() {
		var def menuaccess.MenuDefinition
		if err := rows.Scan(&def.Code, &def.FeatureCode); err != nil {
			return nil, fmt.Errorf("scan tenant menu definition: %w", err)
		}
		defs = append(defs, def)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate tenant menu definitions: %w", err)
	}
	return defs, nil
}

func (s *Store) EmployeeHasInstitutionMenuAccess(ctx context.Context, employeeID int64, menuCode string) (bool, error) {
	menuCode = strings.ToLower(strings.TrimSpace(menuCode))
	if menuCode == "" {
		return false, nil
	}

	tenantID, roleCode, err := s.getLatestEmployeeRoleCode(ctx, employeeID)
	if err != nil {
		if err == pgx.ErrNoRows {
			return false, nil
		}
		return false, fmt.Errorf("load employee role for menu access: %w", err)
	}

	var menuActive bool
	if err := s.pool.QueryRow(ctx, `
		SELECT EXISTS (
			SELECT 1
			FROM institution_menus m
			WHERE lower(trim(m.code)) = $1
			  AND m.deleted_at IS NULL
			  AND COALESCE(m.is_active, true) = true
			  AND (m.tenant_id = $2 OR m.tenant_id IS NULL)
		)
	`, menuCode, tenantID).Scan(&menuActive); err != nil {
		return false, fmt.Errorf("check institution menu active: %w", err)
	}
	if !menuActive {
		return false, nil
	}

	var granted bool
	if err := s.pool.QueryRow(ctx, `
		SELECT EXISTS (
			SELECT 1
			FROM institution_role_menus
			WHERE tenant_id = $1
			  AND lower(trim(role_code)) = $2
			  AND lower(trim(menu_code)) = $3
		)
	`, tenantID, roleCode, menuCode).Scan(&granted); err != nil {
		return false, fmt.Errorf("check role menu grant: %w", err)
	}
	if !granted {
		return false, nil
	}

	return s.isTenantMenuAllowed(ctx, tenantID, menuCode)
}

func buildOverallSegueScoreSQL() string {
	// Keep this aligned with frontend normalizeScoreToHundred:
	// 0~1 => *100, 0~5 => *20, else keep original numeric value.
	return `(
		CASE
			WHEN COALESCE(r.analysis_result->>'segue_percent', '') ~ '^-?[0-9]+(\.[0-9]+)?$'
				THEN (r.analysis_result->>'segue_percent')::double precision
			WHEN COALESCE(r.analysis_result->>'segue_score', '') ~ '^-?[0-9]+(\.[0-9]+)?$'
				THEN (r.analysis_result->>'segue_score')::double precision
			WHEN COALESCE(r.analysis_result->>'communication_score', '') ~ '^-?[0-9]+(\.[0-9]+)?$'
				THEN (r.analysis_result->>'communication_score')::double precision
			WHEN COALESCE(r.analysis_result->'segue'->>'overall_score', '') ~ '^-?[0-9]+(\.[0-9]+)?$'
				THEN (r.analysis_result->'segue'->>'overall_score')::double precision
			WHEN COALESCE(r.analysis_result->'segue'->>'overall', '') ~ '^-?[0-9]+(\.[0-9]+)?$'
				THEN (r.analysis_result->'segue'->>'overall')::double precision
			WHEN COALESCE(r.analysis_result->'analysis_summary'->>'segue_percent', '') ~ '^-?[0-9]+(\.[0-9]+)?$'
				THEN (r.analysis_result->'analysis_summary'->>'segue_percent')::double precision
			WHEN COALESCE(r.analysis_result->'analysis_summary'->>'segue_score', '') ~ '^-?[0-9]+(\.[0-9]+)?$'
				THEN (r.analysis_result->'analysis_summary'->>'segue_score')::double precision
			WHEN COALESCE(r.analysis_display->>'segue_percent', '') ~ '^-?[0-9]+(\.[0-9]+)?$'
				THEN (r.analysis_display->>'segue_percent')::double precision
			WHEN COALESCE(r.analysis_display->>'segue_score', '') ~ '^-?[0-9]+(\.[0-9]+)?$'
				THEN (r.analysis_display->>'segue_score')::double precision
			WHEN COALESCE(r.analysis_display->'segue_scores'->>'overall', '') ~ '^-?[0-9]+(\.[0-9]+)?$'
				THEN (r.analysis_display->'segue_scores'->>'overall')::double precision
			WHEN COALESCE(r.analysis_display->'analysis_result'->>'segue_percent', '') ~ '^-?[0-9]+(\.[0-9]+)?$'
				THEN (r.analysis_display->'analysis_result'->>'segue_percent')::double precision
			WHEN COALESCE(r.analysis_display->'analysis_result'->>'segue_score', '') ~ '^-?[0-9]+(\.[0-9]+)?$'
				THEN (r.analysis_display->'analysis_result'->>'segue_score')::double precision
			WHEN COALESCE(r.analysis_display->'analysis_result'->>'communication_score', '') ~ '^-?[0-9]+(\.[0-9]+)?$'
				THEN (r.analysis_display->'analysis_result'->>'communication_score')::double precision
			WHEN COALESCE(r.analysis_display->'analysis_result'->'segue'->>'overall_score', '') ~ '^-?[0-9]+(\.[0-9]+)?$'
				THEN (r.analysis_display->'analysis_result'->'segue'->>'overall_score')::double precision
			WHEN COALESCE(r.analysis_display->'analysis_result'->'segue'->>'overall', '') ~ '^-?[0-9]+(\.[0-9]+)?$'
				THEN (r.analysis_display->'analysis_result'->'segue'->>'overall')::double precision
			WHEN COALESCE(r.analysis_display->'analysis_result'->'analysis_summary'->>'segue_percent', '') ~ '^-?[0-9]+(\.[0-9]+)?$'
				THEN (r.analysis_display->'analysis_result'->'analysis_summary'->>'segue_percent')::double precision
			WHEN COALESCE(r.analysis_display->'analysis_result'->'analysis_summary'->>'segue_score', '') ~ '^-?[0-9]+(\.[0-9]+)?$'
				THEN (r.analysis_display->'analysis_result'->'analysis_summary'->>'segue_score')::double precision
			ELSE NULL
		END
	)`
}

// ListRecordings retrieves a paginated list of medical recordings
func (s *Store) ListRecordings(ctx context.Context, req RecordingListRequest) ([]*MedicalRecording, int, error) {
	var conditions []string
	var args []interface{}
	argIndex := 1

	conditions = append(conditions, "1=1")

	if len(req.TenantIDs) > 0 {
		conditions = append(conditions, fmt.Sprintf("r.tenant_id = ANY($%d)", argIndex))
		args = append(args, req.TenantIDs)
		argIndex++
	} else if req.TenantID > 0 {
		conditions = append(conditions, fmt.Sprintf("r.tenant_id = $%d", argIndex))
		args = append(args, req.TenantID)
		argIndex++
	}

	if req.EmployeeID != nil {
		conditions = append(conditions, fmt.Sprintf("r.employee_id = $%d", argIndex))
		args = append(args, *req.EmployeeID)
		argIndex++
	}

	if req.BusinessScope != nil && strings.TrimSpace(*req.BusinessScope) != "" {
		conditions = append(conditions, fmt.Sprintf("lower(coalesce(r.business_scope, '')) = lower($%d)", argIndex))
		args = append(args, strings.TrimSpace(*req.BusinessScope))
		argIndex++
	}

	if req.Scope != nil {
		switch *req.Scope {
		case RecordingScopeDoctor:
			conditions = append(conditions, "lower(coalesce(r.business_scope, '')) = 'doctor'")
			conditions = append(conditions, fmt.Sprintf(`EXISTS (
				SELECT 1
				FROM institution_employee_roles ier
				WHERE ier.employee_id = r.employee_id
				  AND lower(ier.role_code) = ANY($%d)
			)`, argIndex))
			args = append(args, doctorScopeRoleCodes)
			argIndex++
		case RecordingScopeConsultant:
			conditions = append(conditions, "lower(coalesce(r.business_scope, '')) = 'consultant'")
			conditions = append(conditions, fmt.Sprintf(`EXISTS (
				SELECT 1
				FROM institution_employee_roles ier
				WHERE ier.employee_id = r.employee_id
				  AND lower(ier.role_code) = ANY($%d)
			)`, argIndex))
			args = append(args, consultantScopeRoleCodes)
			argIndex++

			// Doctor/frontdesk scope wins on dual-role employees, so consultant scope excludes them.
			conditions = append(conditions, fmt.Sprintf(`NOT EXISTS (
				SELECT 1
				FROM institution_employee_roles ier
				WHERE ier.employee_id = r.employee_id
				  AND lower(ier.role_code) = ANY($%d)
			)`, argIndex))
			args = append(args, doctorScopeRoleCodes)
			argIndex++

			conditions = append(conditions, fmt.Sprintf(`NOT EXISTS (
				SELECT 1
				FROM institution_employee_roles ier
				WHERE ier.employee_id = r.employee_id
				  AND lower(ier.role_code) = ANY($%d)
			)`, argIndex))
			args = append(args, frontdeskScopeRoleCodes)
			argIndex++
		case RecordingScopeFrontdesk:
			conditions = append(conditions, "lower(coalesce(r.business_scope, '')) = 'frontdesk'")
			conditions = append(conditions, fmt.Sprintf(`EXISTS (
				SELECT 1
				FROM institution_employee_roles ier
				WHERE ier.employee_id = r.employee_id
				  AND lower(ier.role_code) = ANY($%d)
			)`, argIndex))
			args = append(args, frontdeskScopeRoleCodes)
			argIndex++

			// Doctor scope wins on dual-role employees.
			conditions = append(conditions, fmt.Sprintf(`NOT EXISTS (
				SELECT 1
				FROM institution_employee_roles ier
				WHERE ier.employee_id = r.employee_id
				  AND lower(ier.role_code) = ANY($%d)
			)`, argIndex))
			args = append(args, doctorScopeRoleCodes)
			argIndex++
		case RecordingScopeTherapist:
			conditions = append(conditions, "lower(coalesce(r.business_scope, '')) = 'therapist'")
			conditions = append(conditions, fmt.Sprintf(`EXISTS (
				SELECT 1
				FROM institution_employee_roles ier
				WHERE ier.employee_id = r.employee_id
				  AND lower(ier.role_code) = ANY($%d)
			)`, argIndex))
			args = append(args, therapistScopeRoleCodes)
			argIndex++
		}
	}

	if req.PatientName != nil {
		conditions = append(conditions, fmt.Sprintf("COALESCE(c.name, '') ILIKE $%d", argIndex))
		args = append(args, "%"+*req.PatientName+"%")
		argIndex++
	}

	if req.RecordingID != nil && *req.RecordingID > 0 {
		conditions = append(conditions, fmt.Sprintf("r.id = $%d", argIndex))
		args = append(args, *req.RecordingID)
		argIndex++
	}

	if req.Keyword != nil && strings.TrimSpace(*req.Keyword) != "" {
		keyword := "%" + strings.TrimSpace(*req.Keyword) + "%"
		conditions = append(conditions, fmt.Sprintf(`(
			r.id::text ILIKE $%d OR
			COALESCE(c.name, '') ILIKE $%d OR
			COALESCE(c.phone, '') ILIKE $%d OR
			COALESCE(e.name, '') ILIKE $%d OR
			COALESCE(e.full_name, '') ILIKE $%d OR
			COALESCE(oa.username, '') ILIKE $%d OR
			COALESCE(oa.email, '') ILIKE $%d OR
			COALESCE(sbe.device_no, '') ILIKE $%d OR
			COALESCE(r.file_url, '') ILIKE $%d OR
			COALESCE(r.transcription_text, '') ILIKE $%d OR
			COALESCE(r.analysis_result::text, '') ILIKE $%d OR
			COALESCE(r.analysis_display::text, '') ILIKE $%d OR
			COALESCE(r.analysis_display->>'summary', '') ILIKE $%d OR
			COALESCE(r.analysis_display->>'subjective_summary', '') ILIKE $%d OR
			COALESCE(r.analysis_display->>'doctor_summary', '') ILIKE $%d OR
			COALESCE(r.analysis_display->>'therapist_summary', '') ILIKE $%d OR
			COALESCE(r.analysis_display->>'consultant_summary', '') ILIKE $%d OR
			COALESCE(r.analysis_display->>'conversation_summary', '') ILIKE $%d OR
			COALESCE(r.analysis_display->>'status_summary', '') ILIKE $%d OR
			COALESCE(r.analysis_display->>'chief_complaint', '') ILIKE $%d OR
			COALESCE(r.analysis_display->>'diagnosis', '') ILIKE $%d OR
			COALESCE(r.analysis_display->>'current_state', '') ILIKE $%d OR
			COALESCE(r.analysis_display->>'decision_status', '') ILIKE $%d OR
			COALESCE(r.analysis_display->>'visit_outcome', '') ILIKE $%d OR
			COALESCE(r.analysis_display->>'visit_outcome_status', '') ILIKE $%d
		)`, argIndex, argIndex, argIndex, argIndex, argIndex, argIndex, argIndex, argIndex, argIndex, argIndex, argIndex, argIndex, argIndex, argIndex, argIndex, argIndex, argIndex, argIndex, argIndex, argIndex, argIndex, argIndex, argIndex, argIndex, argIndex))
		args = append(args, keyword)
		argIndex++
	}

	if req.SceneType != nil && strings.TrimSpace(*req.SceneType) != "" {
		scene := strings.TrimSpace(*req.SceneType)
		conditions = append(conditions, fmt.Sprintf(`(
			COALESCE(NULLIF(r.analysis_display->>'scene_type', ''), NULLIF(r.analysis_display->>'scene', ''), COALESCE(r.scene, '')) = $%d
		)`, argIndex))
		args = append(args, scene)
		argIndex++
	}

	if req.VisitOutcome != nil && strings.TrimSpace(*req.VisitOutcome) != "" {
		outcome := strings.TrimSpace(*req.VisitOutcome)
		conditions = append(conditions, fmt.Sprintf(`(
			COALESCE(NULLIF(r.analysis_display->>'visit_outcome', ''), NULLIF(r.analysis_display->>'decision_status', ''), '') = $%d
		)`, argIndex))
		args = append(args, outcome)
		argIndex++
	}

	if req.Status != nil {
		conditions = append(conditions, fmt.Sprintf(`
			(CASE
				WHEN r.analysis_status = 'completed' THEN 'completed'
				WHEN r.analysis_status = 'failed' OR r.transcription_status = 'failed' THEN 'failed'
				WHEN r.analysis_status = 'pending' OR r.transcription_status = 'pending' THEN 'pending'
				ELSE 'processing'
			END) = $%d`, argIndex))
		args = append(args, string(*req.Status))
		argIndex++
	}

	if req.StartDate != nil {
		conditions = append(conditions, fmt.Sprintf("COALESCE(r.recorded_at, r.created_at) >= $%d", argIndex))
		args = append(args, *req.StartDate)
		argIndex++
	}

	if req.EndDate != nil {
		conditions = append(conditions, fmt.Sprintf("COALESCE(r.recorded_at, r.created_at) <= $%d", argIndex))
		args = append(args, *req.EndDate)
		argIndex++
	}

	if req.HasTask != nil {
		if *req.HasTask {
			conditions = append(conditions, "EXISTS (SELECT 1 FROM recording_tasks rt WHERE rt.recording_id = r.id)")
		} else {
			conditions = append(conditions, "NOT EXISTS (SELECT 1 FROM recording_tasks rt WHERE rt.recording_id = r.id)")
		}
	}

	if req.HasContentSeed != nil {
		if *req.HasContentSeed {
			conditions = append(conditions, "EXISTS (SELECT 1 FROM recording_content_seeds rcs WHERE rcs.recording_id = r.id)")
		} else {
			conditions = append(conditions, "NOT EXISTS (SELECT 1 FROM recording_content_seeds rcs WHERE rcs.recording_id = r.id)")
		}
	}

	if req.CriticalGapOnly != nil {
		conditions = append(conditions, fmt.Sprintf(`(
			COALESCE(
				NULLIF(r.analysis_display->>'critical_gap', '')::boolean,
				NULLIF(r.analysis_result->>'critical_gap', '')::boolean,
				false
			) = $%d
		)`, argIndex))
		args = append(args, *req.CriticalGapOnly)
		argIndex++
	}

	if req.IncludeShort != nil && !*req.IncludeShort {
		conditions = append(conditions, "COALESCE(r.duration, 0) >= 60")
	}

	if req.SegueMin != nil {
		scoreExpr := buildOverallSegueScoreSQL()
		conditions = append(conditions, fmt.Sprintf(`(
			CASE
				WHEN %s IS NULL THEN NULL
				WHEN %s > 0 AND %s <= 1 THEN %s * 100
				WHEN %s > 0 AND %s <= 5 THEN %s * 20
				ELSE %s
			END
		) >= $%d`, scoreExpr, scoreExpr, scoreExpr, scoreExpr, scoreExpr, scoreExpr, scoreExpr, scoreExpr, argIndex))
		args = append(args, *req.SegueMin)
		argIndex++
	}

	if req.SegueMax != nil {
		scoreExpr := buildOverallSegueScoreSQL()
		conditions = append(conditions, fmt.Sprintf(`(
			CASE
				WHEN %s IS NULL THEN NULL
				WHEN %s > 0 AND %s <= 1 THEN %s * 100
				WHEN %s > 0 AND %s <= 5 THEN %s * 20
				ELSE %s
			END
		) < $%d`, scoreExpr, scoreExpr, scoreExpr, scoreExpr, scoreExpr, scoreExpr, scoreExpr, scoreExpr, argIndex))
		args = append(args, *req.SegueMax)
		argIndex++
	}

	whereClause := strings.Join(conditions, " AND ")

	countQuery := fmt.Sprintf(`
		SELECT COUNT(*)
		FROM recordings r
		LEFT JOIN customers c ON c.id = r.customer_id
		LEFT JOIN employees e ON e.id = r.employee_id
		LEFT JOIN operations_admins oa ON oa.id = r.employee_id
		LEFT JOIN LATERAL (
			SELECT sae.device_no
			FROM smart_badge_audio_events sae
			WHERE sae.recording_id = r.id
			ORDER BY sae.updated_at DESC NULLS LAST, sae.created_at DESC NULLS LAST, sae.id DESC
			LIMIT 1
		) sbe ON TRUE
		WHERE %s
	`, whereClause)
	var total int
	if err := s.pool.QueryRow(ctx, countQuery, args...).Scan(&total); err != nil {
		return nil, 0, fmt.Errorf("failed to count recordings: %w", err)
	}

	offset := (req.Page - 1) * req.PageSize
	orderBy := "COALESCE(r.recorded_at, r.created_at) DESC, r.id DESC"
	if req.Sort != nil {
		switch strings.TrimSpace(*req.Sort) {
		case "recorded_at_asc":
			orderBy = "COALESCE(r.recorded_at, r.created_at) ASC, r.id ASC"
		case "duration_desc":
			orderBy = "COALESCE(r.duration, 0) DESC, r.id DESC"
		case "duration_asc":
			orderBy = "COALESCE(r.duration, 0) ASC, r.id ASC"
		}
	}
	query := fmt.Sprintf(`
		SELECT
			r.id,
			r.tenant_id,
			COALESCE(NULLIF(t.name, ''), CONCAT('租户#', r.tenant_id::text)) AS tenant_name,
			r.employee_id,
			COALESCE(
				NULLIF(NULLIF(e.full_name, 'unknown'), ''),
				NULLIF(NULLIF(e.name, 'unknown'), ''),
				NULLIF(e.phone, ''),
				NULLIF(oa.username, ''),
				NULLIF(oa.email, ''),
				'未知员工'
			) AS employee_name,
			COALESCE(NULLIF(d.name, ''), '-') AS department_name,
			COALESCE(
				NULLIF(sbe.device_no, ''),
				NULLIF((regexp_match(COALESCE(r.file_url, ''), '(SSYX[0-9]+)'))[1], ''),
				''
			) AS device_no,
			r.customer_id,
			NULLIF(c.name, '') AS customer_name,
			COALESCE(c.name, '') AS patient_name,
			c.age AS patient_age,
			c.gender AS patient_gender,
			c.phone AS patient_phone,
			r.file_url AS recording_url,
			r.duration AS recording_duration,
			r.transcription_text AS transcript_text,
			NULLIF(r.analysis_display->>'doctor_summary', '') AS doctor_summary,
			NULLIF(r.analysis_display->>'therapist_summary', '') AS therapist_summary,
			NULLIF(r.analysis_display->>'consultant_summary', '') AS consultant_summary,
			COALESCE(NULLIF(r.business_scope, ''), 'unknown') AS business_scope,
			COALESCE(r.analysis_result, '{}'::json) AS analysis_result,
			COALESCE(r.analysis_display, '{}'::jsonb) AS analysis_display,
			NULLIF(r.analysis_status, '') AS analysis_status,
			CASE
				WHEN r.analysis_status = 'completed' THEN 'completed'
				WHEN r.analysis_status = 'failed' OR r.transcription_status = 'failed' THEN 'failed'
				WHEN r.analysis_status = 'pending' OR r.transcription_status = 'pending' THEN 'pending'
				ELSE 'processing'
			END AS status,
			NULL::text AS processing_error,
			r.recorded_at AS recording_started_at,
			NULL::timestamp AS recording_ended_at,
			CASE WHEN r.analysis_status = 'completed' THEN COALESCE(r.updated_at, r.created_at, NOW()) ELSE NULL::timestamp END AS processed_at,
			r.created_at,
			COALESCE(r.updated_at, r.created_at, NOW()) AS updated_at,
			NULL::timestamp AS deleted_at
		FROM recordings r
		LEFT JOIN customers c ON c.id = r.customer_id
		LEFT JOIN employees e ON e.id = r.employee_id
		LEFT JOIN departments d ON d.id = e.department_id
		LEFT JOIN operations_admins oa ON oa.id = r.employee_id
		LEFT JOIN LATERAL (
			SELECT sae.device_no
			FROM smart_badge_audio_events sae
			WHERE sae.recording_id = r.id
			ORDER BY sae.updated_at DESC NULLS LAST, sae.created_at DESC NULLS LAST, sae.id DESC
			LIMIT 1
		) sbe ON TRUE
		LEFT JOIN tenants t ON t.id = r.tenant_id
		WHERE %s
		ORDER BY %s
		LIMIT $%d OFFSET $%d
	`, whereClause, orderBy, argIndex, argIndex+1)

	args = append(args, req.PageSize, offset)

	rows, err := s.pool.Query(ctx, query, args...)
	if err != nil {
		return nil, 0, fmt.Errorf("failed to query recordings: %w", err)
	}
	defer rows.Close()

	var recordings []*MedicalRecording
	for rows.Next() {
		var r MedicalRecording
		if err := rows.Scan(
			&r.ID,
			&r.TenantID,
			&r.TenantName,
			&r.EmployeeID,
			&r.EmployeeName,
			&r.DepartmentName,
			&r.DeviceNo,
			&r.CustomerID,
			&r.CustomerName,
			&r.PatientName,
			&r.PatientAge,
			&r.PatientGender,
			&r.PatientPhone,
			&r.RecordingURL,
			&r.RecordingDuration,
			&r.TranscriptText,
			&r.DoctorSummary,
			&r.TherapistSummary,
			&r.ConsultantSummary,
			&r.BusinessScope,
			&r.AnalysisResult,
			&r.AnalysisDisplay,
			&r.AnalysisStatus,
			&r.Status,
			&r.ProcessingError,
			&r.RecordingStartedAt,
			&r.RecordingEndedAt,
			&r.ProcessedAt,
			&r.CreatedAt,
			&r.UpdatedAt,
			&r.DeletedAt,
		); err != nil {
			return nil, 0, fmt.Errorf("failed to scan recording: %w", err)
		}
		recordings = append(recordings, &r)
	}

	return recordings, total, nil
}

func (s *Store) ListManagementEvents(
	ctx context.Context,
	tenantID int64,
	roleType string,
	dimensionCode string,
	startDate *time.Time,
	endDate *time.Time,
) ([]ManagementEvent, error) {
	if tenantID <= 0 {
		return []ManagementEvent{}, nil
	}
	conditions := []string{"tenant_id = $1"}
	args := []interface{}{tenantID}
	argIndex := 2
	if strings.TrimSpace(roleType) != "" {
		conditions = append(conditions, fmt.Sprintf("role_type = $%d", argIndex))
		args = append(args, strings.TrimSpace(roleType))
		argIndex++
	}
	if strings.TrimSpace(dimensionCode) != "" {
		conditions = append(conditions, fmt.Sprintf("dimension_code = $%d", argIndex))
		args = append(args, strings.TrimSpace(dimensionCode))
		argIndex++
	}
	if startDate != nil {
		conditions = append(conditions, fmt.Sprintf("event_date >= $%d", argIndex))
		args = append(args, *startDate)
		argIndex++
	}
	if endDate != nil {
		conditions = append(conditions, fmt.Sprintf("event_date <= $%d", argIndex))
		args = append(args, *endDate)
		argIndex++
	}
	query := fmt.Sprintf(`
		SELECT
			id, tenant_id, event_date, event_type, title, COALESCE(description, ''),
			role_type, dimension_code, status, COALESCE(meta, '{}'::jsonb),
			COALESCE(created_by, 0), created_at, updated_at
		FROM management_events
		WHERE %s
		ORDER BY event_date ASC, id ASC
	`, strings.Join(conditions, " AND "))
	rows, err := s.pool.Query(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := make([]ManagementEvent, 0)
	for rows.Next() {
		var (
			item      ManagementEvent
			eventDate time.Time
			createdAt time.Time
			updatedAt time.Time
			metaRaw   []byte
		)
		if scanErr := rows.Scan(
			&item.ID, &item.TenantID, &eventDate, &item.EventType, &item.Title, &item.Description,
			&item.RoleType, &item.DimensionCode, &item.Status, &metaRaw, &item.CreatedBy, &createdAt, &updatedAt,
		); scanErr != nil {
			return nil, scanErr
		}
		item.EventDate = eventDate.Format("2006-01-02")
		item.CreatedAt = createdAt.Format(time.RFC3339)
		item.UpdatedAt = updatedAt.Format(time.RFC3339)
		if len(metaRaw) > 0 {
			var meta map[string]interface{}
			if err := json.Unmarshal(metaRaw, &meta); err == nil {
				item.Meta = meta
			}
		}
		out = append(out, item)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return out, nil
}

func (s *Store) CreateManagementEvent(ctx context.Context, tenantID int64, createdBy int64, req CreateManagementEventRequest) (*ManagementEvent, error) {
	var eventDate time.Time
	parsed, err := time.Parse("2006-01-02", strings.TrimSpace(req.EventDate))
	if err != nil {
		return nil, fmt.Errorf("invalid event_date, expected YYYY-MM-DD")
	}
	eventDate = parsed
	metaBytes, _ := json.Marshal(req.Meta)
	if len(metaBytes) == 0 {
		metaBytes = []byte("{}")
	}
	var (
		item      ManagementEvent
		createdAt time.Time
		updatedAt time.Time
		metaRaw   []byte
	)
	if err := s.pool.QueryRow(ctx, `
		INSERT INTO management_events (
			tenant_id, event_date, event_type, title, description,
			role_type, dimension_code, status, meta, created_by, created_at, updated_at
		) VALUES (
			$1, $2, $3, $4, $5,
			$6, $7, 'active', $8::jsonb, $9, NOW(), NOW()
		)
		RETURNING id, tenant_id, event_date, event_type, title, COALESCE(description, ''),
		          role_type, dimension_code, status, COALESCE(meta, '{}'::jsonb),
		          COALESCE(created_by, 0), created_at, updated_at
	`, tenantID, eventDate, strings.TrimSpace(req.EventType), strings.TrimSpace(req.Title), strings.TrimSpace(req.Description), strings.TrimSpace(req.RoleType), strings.TrimSpace(req.DimensionCode), string(metaBytes), createdBy).Scan(
		&item.ID, &item.TenantID, &eventDate, &item.EventType, &item.Title, &item.Description,
		&item.RoleType, &item.DimensionCode, &item.Status, &metaRaw, &item.CreatedBy, &createdAt, &updatedAt,
	); err != nil {
		return nil, err
	}
	item.EventDate = eventDate.Format("2006-01-02")
	item.CreatedAt = createdAt.Format(time.RFC3339)
	item.UpdatedAt = updatedAt.Format(time.RFC3339)
	if len(metaRaw) > 0 {
		var meta map[string]interface{}
		if err := json.Unmarshal(metaRaw, &meta); err == nil {
			item.Meta = meta
		}
	}
	return &item, nil
}

func (s *Store) ListBenchmarkClips(
	ctx context.Context,
	tenantID int64,
	status string,
	source string,
	roleCode string,
	dimension string,
	keyword string,
	page int,
	pageSize int,
) ([]BenchmarkClip, int64, error) {
	if tenantID <= 0 {
		return []BenchmarkClip{}, 0, nil
	}
	if page <= 0 {
		page = 1
	}
	if pageSize <= 0 {
		pageSize = 20
	}
	if pageSize > 200 {
		pageSize = 200
	}
	offset := (page - 1) * pageSize

	conditions := []string{"bc.tenant_id = $1"}
	args := []interface{}{tenantID}
	argIndex := 2
	if v := strings.TrimSpace(status); v != "" {
		conditions = append(conditions, fmt.Sprintf("bc.status = $%d", argIndex))
		args = append(args, v)
		argIndex++
	}
	if v := strings.TrimSpace(source); v != "" {
		conditions = append(conditions, fmt.Sprintf("bc.source = $%d", argIndex))
		args = append(args, v)
		argIndex++
	}
	if v := strings.TrimSpace(roleCode); v != "" {
		conditions = append(conditions, fmt.Sprintf("bc.role_code = $%d", argIndex))
		args = append(args, v)
		argIndex++
	}
	if v := strings.TrimSpace(dimension); v != "" {
		conditions = append(conditions, fmt.Sprintf("bc.dimension = $%d", argIndex))
		args = append(args, v)
		argIndex++
	}
	if v := strings.TrimSpace(keyword); v != "" {
		conditions = append(conditions, fmt.Sprintf("(bc.clip_text ILIKE $%d OR COALESCE(e.full_name,'') ILIKE $%d OR COALESCE(e.name,'') ILIKE $%d OR COALESCE(NULLIF(r.scene_name,''), NULLIF(r.scene,''), '') ILIKE $%d OR COALESCE(bc.dimension,'') ILIKE $%d OR bc.recording_id::text ILIKE $%d)", argIndex, argIndex, argIndex, argIndex, argIndex, argIndex))
		args = append(args, "%"+v+"%")
		argIndex++
	}
	whereClause := strings.Join(conditions, " AND ")

	var total int64
	if err := s.pool.QueryRow(ctx, fmt.Sprintf(`
		SELECT COUNT(1)
		FROM benchmark_clips bc
		LEFT JOIN employees e ON e.id = bc.employee_id
		LEFT JOIN recordings r ON r.id = bc.recording_id
		WHERE %s
	`, whereClause), args...).Scan(&total); err != nil {
		return nil, 0, err
	}

	query := fmt.Sprintf(`
		SELECT
			bc.id, bc.tenant_id, bc.recording_id, bc.employee_id,
			COALESCE(NULLIF(e.full_name,''), NULLIF(e.name,''), e.phone, '') AS employee_name,
			bc.role_code, bc.dimension, COALESCE(bc.score, 0),
			COALESCE(bc.clip_text, ''), COALESCE(bc.ai_comment, ''),
			COALESCE(bc.learning_points, '[]'::jsonb), bc.source, bc.status, COALESCE(bc.confidence, ''),
			r.recorded_at, COALESCE(NULLIF(r.scene_name,''), NULLIF(r.scene,''), ''),
			bc.audio_start_seconds, bc.audio_end_seconds, COALESCE(bc.used_in_meetings, 0),
			bc.accepted_at, bc.rejected_at, bc.created_at, bc.updated_at
		FROM benchmark_clips bc
		LEFT JOIN employees e ON e.id = bc.employee_id
		LEFT JOIN recordings r ON r.id = bc.recording_id
		WHERE %s
		ORDER BY bc.created_at DESC, bc.id DESC
		LIMIT $%d OFFSET $%d
	`, whereClause, argIndex, argIndex+1)
	args = append(args, pageSize, offset)

	rows, err := s.pool.Query(ctx, query, args...)
	if err != nil {
		return nil, 0, err
	}
	defer rows.Close()

	out := make([]BenchmarkClip, 0)
	for rows.Next() {
		var (
			item        BenchmarkClip
			createdAt   time.Time
			updatedAt   time.Time
			acceptedAt  *time.Time
			rejectedAt  *time.Time
			recordedAt  *time.Time
			sceneType   string
			learningRaw []byte
		)
		if err := rows.Scan(
			&item.ID, &item.TenantID, &item.RecordingID, &item.EmployeeID,
			&item.EmployeeName, &item.RoleCode, &item.Dimension, &item.Score,
			&item.ClipText, &item.AIComment, &learningRaw, &item.Source, &item.Status, &item.Confidence,
			&recordedAt, &sceneType,
			&item.AudioStartSecond, &item.AudioEndSecond, &item.UsedInMeetings,
			&acceptedAt, &rejectedAt, &createdAt, &updatedAt,
		); err != nil {
			return nil, 0, err
		}
		item.CreatedAt = createdAt.Format(time.RFC3339)
		item.UpdatedAt = updatedAt.Format(time.RFC3339)
		if acceptedAt != nil {
			v := acceptedAt.Format(time.RFC3339)
			item.AcceptedAt = &v
		}
		if rejectedAt != nil {
			v := rejectedAt.Format(time.RFC3339)
			item.RejectedAt = &v
		}
		if recordedAt != nil {
			item.RecordedAt = recordedAt.Format(time.RFC3339)
		}
		item.SceneType = sceneType
		if len(learningRaw) > 0 {
			_ = json.Unmarshal(learningRaw, &item.LearningPoints)
		}
		out = append(out, item)
	}
	if err := rows.Err(); err != nil {
		return nil, 0, err
	}
	return out, total, nil
}

func (s *Store) GetBenchmarkClipByID(ctx context.Context, tenantID int64, id int64) (*BenchmarkClip, error) {
	var (
		item        BenchmarkClip
		createdAt   time.Time
		updatedAt   time.Time
		acceptedAt  *time.Time
		rejectedAt  *time.Time
		recordedAt  *time.Time
		sceneType   string
		learningRaw []byte
	)
	if err := s.pool.QueryRow(ctx, `
		SELECT
			bc.id, bc.tenant_id, bc.recording_id, bc.employee_id,
			COALESCE(NULLIF(e.full_name,''), NULLIF(e.name,''), e.phone, '') AS employee_name,
			bc.role_code, bc.dimension, COALESCE(bc.score, 0),
			COALESCE(bc.clip_text, ''), COALESCE(bc.ai_comment, ''),
			COALESCE(bc.learning_points, '[]'::jsonb), bc.source, bc.status, COALESCE(bc.confidence, ''),
			r.recorded_at, COALESCE(NULLIF(r.scene_name,''), NULLIF(r.scene,''), ''),
			bc.audio_start_seconds, bc.audio_end_seconds, COALESCE(bc.used_in_meetings, 0),
			bc.accepted_at, bc.rejected_at, bc.created_at, bc.updated_at
		FROM benchmark_clips bc
		LEFT JOIN employees e ON e.id = bc.employee_id
		LEFT JOIN recordings r ON r.id = bc.recording_id
		WHERE bc.tenant_id = $1 AND bc.id = $2
	`, tenantID, id).Scan(
		&item.ID, &item.TenantID, &item.RecordingID, &item.EmployeeID,
		&item.EmployeeName, &item.RoleCode, &item.Dimension, &item.Score,
		&item.ClipText, &item.AIComment, &learningRaw, &item.Source, &item.Status, &item.Confidence,
		&recordedAt, &sceneType,
		&item.AudioStartSecond, &item.AudioEndSecond, &item.UsedInMeetings,
		&acceptedAt, &rejectedAt, &createdAt, &updatedAt,
	); err != nil {
		return nil, err
	}
	item.CreatedAt = createdAt.Format(time.RFC3339)
	item.UpdatedAt = updatedAt.Format(time.RFC3339)
	if acceptedAt != nil {
		v := acceptedAt.Format(time.RFC3339)
		item.AcceptedAt = &v
	}
	if rejectedAt != nil {
		v := rejectedAt.Format(time.RFC3339)
		item.RejectedAt = &v
	}
	if recordedAt != nil {
		item.RecordedAt = recordedAt.Format(time.RFC3339)
	}
	item.SceneType = sceneType
	if len(learningRaw) > 0 {
		_ = json.Unmarshal(learningRaw, &item.LearningPoints)
	}
	return &item, nil
}

type benchmarkCandidateInput struct {
	TenantID    int64
	RecordingID int64
	EmployeeID  int64
	RoleCode    string
	Dimension   string
	Score       float64
	ClipText    string
	Confidence  string
	Source      string
}

func (s *Store) InsertBenchmarkClipIfNotExists(ctx context.Context, in benchmarkCandidateInput) (bool, error) {
	if strings.TrimSpace(in.ClipText) == "" || strings.TrimSpace(in.Dimension) == "" || strings.TrimSpace(in.RoleCode) == "" {
		return false, nil
	}
	learningBytes := []byte("[]")
	tag := `auto`
	if strings.TrimSpace(in.Source) != "" {
		tag = strings.TrimSpace(in.Source)
	}
	cmd, err := s.pool.Exec(ctx, `
		INSERT INTO benchmark_clips (
			tenant_id, recording_id, employee_id, role_code, dimension, score,
			clip_text, learning_points, source, status, confidence, used_in_meetings, created_at, updated_at
		) VALUES (
			$1, $2, $3, $4, $5, $6,
			$7, $8::jsonb, $9, 'pending', $10, 0, NOW(), NOW()
		)
		ON CONFLICT (tenant_id, recording_id, role_code, dimension) DO NOTHING
	`, in.TenantID, in.RecordingID, in.EmployeeID, in.RoleCode, in.Dimension, in.Score, strings.TrimSpace(in.ClipText), string(learningBytes), tag, strings.TrimSpace(in.Confidence))
	if err != nil {
		return false, err
	}
	return cmd.RowsAffected() > 0, nil
}

func (s *Store) CreateManualBenchmarkClip(ctx context.Context, tenantID int64, recordingID int64, employeeID int64, req CreateManualBenchmarkClipRequest) (*BenchmarkClip, error) {
	learningBytes := []byte("[]")
	var (
		item        BenchmarkClip
		createdAt   time.Time
		updatedAt   time.Time
		acceptedAt  *time.Time
		learningRaw []byte
	)
	if err := s.pool.QueryRow(ctx, `
		INSERT INTO benchmark_clips (
			tenant_id, recording_id, employee_id, role_code, dimension, score,
			clip_text, learning_points, source, status, confidence, used_in_meetings, accepted_at, created_at, updated_at
		) VALUES (
			$1, $2, $3, $4, $5, $6,
			$7, $8::jsonb, 'manual', 'accepted', $9, 0, NOW(), NOW(), NOW()
		)
		RETURNING id, tenant_id, recording_id, employee_id, role_code, dimension, score, clip_text,
		          COALESCE(ai_comment, ''), COALESCE(learning_points, '[]'::jsonb), source, status, COALESCE(confidence,''),
		          audio_start_seconds, audio_end_seconds, used_in_meetings, accepted_at, rejected_at, created_at, updated_at
	`, tenantID, recordingID, employeeID, strings.TrimSpace(req.RoleCode), strings.TrimSpace(req.Dimension), req.Score, strings.TrimSpace(req.ClipText), string(learningBytes), strings.TrimSpace(req.Confidence)).Scan(
		&item.ID, &item.TenantID, &item.RecordingID, &item.EmployeeID, &item.RoleCode, &item.Dimension, &item.Score, &item.ClipText,
		&item.AIComment, &learningRaw, &item.Source, &item.Status, &item.Confidence,
		&item.AudioStartSecond, &item.AudioEndSecond, &item.UsedInMeetings, &acceptedAt, &item.RejectedAt, &createdAt, &updatedAt,
	); err != nil {
		return nil, err
	}
	item.CreatedAt = createdAt.Format(time.RFC3339)
	item.UpdatedAt = updatedAt.Format(time.RFC3339)
	if acceptedAt != nil {
		v := acceptedAt.Format(time.RFC3339)
		item.AcceptedAt = &v
	}
	if len(learningRaw) > 0 {
		_ = json.Unmarshal(learningRaw, &item.LearningPoints)
	}
	return &item, nil
}

func (s *Store) UpdateBenchmarkClipStatus(
	ctx context.Context,
	tenantID int64,
	id int64,
	status string,
	aiComment string,
	learningPoints []string,
) (*BenchmarkClip, error) {
	learningBytes, _ := json.Marshal(learningPoints)
	if len(learningBytes) == 0 {
		learningBytes = []byte("[]")
	}
	var (
		item        BenchmarkClip
		createdAt   time.Time
		updatedAt   time.Time
		acceptedAt  *time.Time
		rejectedAt  *time.Time
		learningRaw []byte
	)
	if err := s.pool.QueryRow(ctx, `
		UPDATE benchmark_clips
		SET status = $3::text,
		    ai_comment = CASE WHEN $3::text = 'accepted' THEN $4 ELSE ai_comment END,
		    learning_points = CASE WHEN $3::text = 'accepted' THEN $5::jsonb ELSE learning_points END,
		    accepted_at = CASE WHEN $3::text = 'accepted' THEN NOW() ELSE accepted_at END,
		    rejected_at = CASE WHEN $3::text = 'rejected' THEN NOW() ELSE rejected_at END,
		    updated_at = NOW()
		WHERE tenant_id = $1 AND id = $2
		RETURNING id, tenant_id, recording_id, employee_id, role_code, dimension, score, clip_text,
		          COALESCE(ai_comment, ''), COALESCE(learning_points, '[]'::jsonb), source, status, COALESCE(confidence,''),
		          audio_start_seconds, audio_end_seconds, used_in_meetings, accepted_at, rejected_at, created_at, updated_at
	`, tenantID, id, status, strings.TrimSpace(aiComment), string(learningBytes)).Scan(
		&item.ID, &item.TenantID, &item.RecordingID, &item.EmployeeID, &item.RoleCode, &item.Dimension, &item.Score, &item.ClipText,
		&item.AIComment, &learningRaw, &item.Source, &item.Status, &item.Confidence,
		&item.AudioStartSecond, &item.AudioEndSecond, &item.UsedInMeetings, &acceptedAt, &rejectedAt, &createdAt, &updatedAt,
	); err != nil {
		return nil, err
	}
	item.CreatedAt = createdAt.Format(time.RFC3339)
	item.UpdatedAt = updatedAt.Format(time.RFC3339)
	if acceptedAt != nil {
		v := acceptedAt.Format(time.RFC3339)
		item.AcceptedAt = &v
	}
	if rejectedAt != nil {
		v := rejectedAt.Format(time.RFC3339)
		item.RejectedAt = &v
	}
	if len(learningRaw) > 0 {
		_ = json.Unmarshal(learningRaw, &item.LearningPoints)
	}
	return &item, nil
}

func (s *Store) UpdateBenchmarkClipContent(
	ctx context.Context,
	tenantID int64,
	id int64,
	clipText string,
	aiComment string,
	learningPoints []string,
	audioStartSeconds *int,
	audioEndSeconds *int,
) (*BenchmarkClip, error) {
	learningBytes, _ := json.Marshal(learningPoints)
	if len(learningBytes) == 0 {
		learningBytes = []byte("[]")
	}
	if _, err := s.pool.Exec(ctx, `
		UPDATE benchmark_clips
		SET clip_text = CASE WHEN NULLIF($3::text, '') IS NOT NULL THEN $3::text ELSE clip_text END,
		    ai_comment = CASE WHEN NULLIF($4::text, '') IS NOT NULL THEN $4::text ELSE ai_comment END,
		    learning_points = CASE WHEN jsonb_array_length($5::jsonb) > 0 THEN $5::jsonb ELSE learning_points END,
		    audio_start_seconds = COALESCE($6, audio_start_seconds),
		    audio_end_seconds = COALESCE($7, audio_end_seconds),
		    updated_at = NOW()
		WHERE tenant_id = $1 AND id = $2
	`, tenantID, id, strings.TrimSpace(clipText), strings.TrimSpace(aiComment), string(learningBytes), audioStartSeconds, audioEndSeconds); err != nil {
		return nil, err
	}
	return s.GetBenchmarkClipByID(ctx, tenantID, id)
}

func (s *Store) MarkBenchmarkUsedInMeeting(ctx context.Context, tenantID int64, recordingID int64, roleCode string) (*BenchmarkClip, error) {
	var id int64
	if err := s.pool.QueryRow(ctx, `
		WITH target AS (
			SELECT bc.id
			FROM benchmark_clips bc
			WHERE bc.tenant_id = $1
			  AND bc.recording_id = $2
			  AND bc.status = 'accepted'
			  AND ($3 = '' OR bc.role_code = $3)
			ORDER BY COALESCE(bc.used_in_meetings, 0) ASC, bc.created_at DESC
			LIMIT 1
		)
		UPDATE benchmark_clips bc
		SET used_in_meetings = COALESCE(bc.used_in_meetings, 0) + 1,
		    updated_at = NOW()
		FROM target
		WHERE bc.id = target.id
		RETURNING bc.id
	`, tenantID, recordingID, strings.TrimSpace(roleCode)).Scan(&id); err != nil {
		return nil, err
	}
	return s.GetBenchmarkClipByID(ctx, tenantID, id)
}

func (s *Store) CreateBenchmarkClipPushes(ctx context.Context, tenantID int64, clipID int64, pushedBy int64, targetEmployeeIDs []int64, note string) (int64, error) {
	if len(targetEmployeeIDs) == 0 {
		return 0, nil
	}
	var inserted int64
	for _, eid := range targetEmployeeIDs {
		if eid <= 0 {
			continue
		}
		var name string
		_ = s.pool.QueryRow(ctx, `
			SELECT COALESCE(NULLIF(full_name,''), NULLIF(name,''), phone, '')
			FROM employees
			WHERE id = $1 AND tenant_id = $2
		`, eid, tenantID).Scan(&name)
		cmd, err := s.pool.Exec(ctx, `
			INSERT INTO benchmark_clip_pushes (
				tenant_id, benchmark_clip_id, target_employee_id, target_employee_name,
				note, status, pushed_by, pushed_at, created_at, updated_at
			) VALUES (
				$1, $2, $3, $4,
				$5, 'sent', $6, NOW(), NOW(), NOW()
			)
		`, tenantID, clipID, eid, strings.TrimSpace(name), strings.TrimSpace(note), pushedBy)
		if err != nil {
			return inserted, err
		}
		inserted += cmd.RowsAffected()
	}
	return inserted, nil
}

func (s *Store) ListBenchmarkClipPushes(ctx context.Context, tenantID int64, clipID int64) ([]BenchmarkClipPushRecord, error) {
	rows, err := s.pool.Query(ctx, `
		SELECT id, benchmark_clip_id, target_employee_id, COALESCE(target_employee_name, ''),
		       COALESCE(note, ''), status, pushed_by, pushed_at, acknowledged_at, created_at, updated_at
		FROM benchmark_clip_pushes
		WHERE tenant_id = $1 AND benchmark_clip_id = $2
		ORDER BY created_at DESC, id DESC
	`, tenantID, clipID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := make([]BenchmarkClipPushRecord, 0)
	for rows.Next() {
		var (
			item         BenchmarkClipPushRecord
			pushedAt     time.Time
			createdAt    time.Time
			updatedAt    time.Time
			acknowledged *time.Time
		)
		if err := rows.Scan(&item.ID, &item.BenchmarkClipID, &item.TargetEmployeeID, &item.TargetEmployeeName,
			&item.Note, &item.Status, &item.PushedBy, &pushedAt, &acknowledged, &createdAt, &updatedAt); err != nil {
			return nil, err
		}
		item.PushedAt = pushedAt.Format(time.RFC3339)
		item.CreatedAt = createdAt.Format(time.RFC3339)
		item.UpdatedAt = updatedAt.Format(time.RFC3339)
		if acknowledged != nil {
			v := acknowledged.Format(time.RFC3339)
			item.AcknowledgedAt = &v
		}
		out = append(out, item)
	}
	return out, rows.Err()
}

func (s *Store) GetBenchmarkClipPushStatistics(ctx context.Context, tenantID int64, clipID int64) (BenchmarkClipPushStatistics, error) {
	var stats BenchmarkClipPushStatistics
	if err := s.pool.QueryRow(ctx, `
		SELECT
			COUNT(*) AS total_pushed,
			COUNT(CASE WHEN status = 'acknowledged' THEN 1 END) AS acknowledged_count,
			COALESCE(ROUND(
				COUNT(CASE WHEN status = 'acknowledged' THEN 1 END)::numeric / NULLIF(COUNT(*), 0) * 100,
				2
			), 0)::float8 AS learning_rate
		FROM benchmark_clip_pushes
		WHERE tenant_id = $1 AND benchmark_clip_id = $2
	`, tenantID, clipID).Scan(&stats.TotalPushed, &stats.AcknowledgedCount, &stats.LearningRate); err != nil {
		return BenchmarkClipPushStatistics{}, err
	}
	return stats, nil
}

func (s *Store) ListEmployeeLearningTasks(ctx context.Context, tenantID int64, employeeID int64, status string, page int, pageSize int) ([]EmployeeLearningTask, int64, error) {
	if page <= 0 {
		page = 1
	}
	if pageSize <= 0 {
		pageSize = 20
	}
	offset := (page - 1) * pageSize
	var total int64
	if err := s.pool.QueryRow(ctx, `
		SELECT COUNT(*)
		FROM benchmark_clip_pushes p
		WHERE p.tenant_id = $1
		  AND p.target_employee_id = $2
		  AND ($3::text = '' OR p.status = $3::text)
	`, tenantID, employeeID, strings.TrimSpace(status)).Scan(&total); err != nil {
		return nil, 0, err
	}
	rows, err := s.pool.Query(ctx, `
		SELECT
			p.id,
			p.benchmark_clip_id,
			bc.dimension,
			COALESCE(bc.score, 0),
			COALESCE(bc.role_code, ''),
			COALESCE(bc.employee_name, ''),
			COALESCE(bc.clip_text, ''),
			COALESCE(bc.ai_comment, ''),
			COALESCE(bc.learning_points, '[]'::jsonb),
			COALESCE(pusher.full_name, pusher.name, ''),
			COALESCE(p.note, ''),
			p.pushed_at,
			COALESCE(p.status, 'sent'),
			p.acknowledged_at
		FROM benchmark_clip_pushes p
		JOIN benchmark_clips bc ON bc.id = p.benchmark_clip_id
		LEFT JOIN employees pusher ON pusher.id = p.pushed_by
		WHERE p.tenant_id = $1
		  AND p.target_employee_id = $2
		  AND ($3::text = '' OR p.status = $3::text)
		ORDER BY
			CASE WHEN p.status = 'sent' THEN 0 ELSE 1 END,
			p.pushed_at DESC
		LIMIT $4 OFFSET $5
	`, tenantID, employeeID, strings.TrimSpace(status), pageSize, offset)
	if err != nil {
		return nil, 0, err
	}
	defer rows.Close()
	out := make([]EmployeeLearningTask, 0)
	for rows.Next() {
		var (
			item         EmployeeLearningTask
			pushedAt     time.Time
			acknowledged *time.Time
			learningRaw  []byte
		)
		if err := rows.Scan(
			&item.PushID,
			&item.ClipID,
			&item.Dimension,
			&item.Score,
			&item.RoleCode,
			&item.EmployeeName,
			&item.ClipText,
			&item.AIComment,
			&learningRaw,
			&item.PushedByName,
			&item.Note,
			&pushedAt,
			&item.Status,
			&acknowledged,
		); err != nil {
			return nil, 0, err
		}
		item.PushedAt = pushedAt.Format(time.RFC3339)
		if acknowledged != nil {
			v := acknowledged.Format(time.RFC3339)
			item.AcknowledgedAt = &v
		}
		if len(learningRaw) > 0 {
			_ = json.Unmarshal(learningRaw, &item.LearningPoints)
		}
		out = append(out, item)
	}
	return out, total, rows.Err()
}

func (s *Store) AckEmployeeLearningTask(ctx context.Context, tenantID int64, employeeID int64, pushID int64) (*BenchmarkClipPushRecord, error) {
	var (
		item         BenchmarkClipPushRecord
		pushedAt     time.Time
		createdAt    time.Time
		updatedAt    time.Time
		acknowledged *time.Time
	)
	if err := s.pool.QueryRow(ctx, `
		UPDATE benchmark_clip_pushes
		SET
			status = 'acknowledged',
			acknowledged_at = COALESCE(acknowledged_at, NOW()),
			updated_at = NOW()
		WHERE tenant_id = $1
		  AND target_employee_id = $2
		  AND id = $3
		RETURNING id, benchmark_clip_id, target_employee_id, COALESCE(target_employee_name, ''),
		          COALESCE(note, ''), status, pushed_by, pushed_at, acknowledged_at, created_at, updated_at
	`, tenantID, employeeID, pushID).Scan(
		&item.ID, &item.BenchmarkClipID, &item.TargetEmployeeID, &item.TargetEmployeeName,
		&item.Note, &item.Status, &item.PushedBy, &pushedAt, &acknowledged, &createdAt, &updatedAt,
	); err != nil {
		return nil, err
	}
	item.PushedAt = pushedAt.Format(time.RFC3339)
	item.CreatedAt = createdAt.Format(time.RFC3339)
	item.UpdatedAt = updatedAt.Format(time.RFC3339)
	if acknowledged != nil {
		v := acknowledged.Format(time.RFC3339)
		item.AcknowledgedAt = &v
	}
	return &item, nil
}

func (s *Store) AckBenchmarkClipPush(ctx context.Context, tenantID int64, pushID int64) (*BenchmarkClipPushRecord, error) {
	var (
		item         BenchmarkClipPushRecord
		pushedAt     time.Time
		createdAt    time.Time
		updatedAt    time.Time
		acknowledged *time.Time
	)
	if err := s.pool.QueryRow(ctx, `
		UPDATE benchmark_clip_pushes
		SET status = 'acknowledged', acknowledged_at = NOW(), updated_at = NOW()
		WHERE tenant_id = $1 AND id = $2
		RETURNING id, benchmark_clip_id, target_employee_id, COALESCE(target_employee_name, ''),
		          COALESCE(note, ''), status, pushed_by, pushed_at, acknowledged_at, created_at, updated_at
	`, tenantID, pushID).Scan(
		&item.ID, &item.BenchmarkClipID, &item.TargetEmployeeID, &item.TargetEmployeeName,
		&item.Note, &item.Status, &item.PushedBy, &pushedAt, &acknowledged, &createdAt, &updatedAt,
	); err != nil {
		return nil, err
	}
	item.PushedAt = pushedAt.Format(time.RFC3339)
	item.CreatedAt = createdAt.Format(time.RFC3339)
	item.UpdatedAt = updatedAt.Format(time.RFC3339)
	if acknowledged != nil {
		v := acknowledged.Format(time.RFC3339)
		item.AcknowledgedAt = &v
	}
	return &item, nil
}

type benchmarkSourceRow struct {
	RecordingID   int64
	EmployeeID    int64
	RoleCode      string
	AnalysisRaw   []byte
	StructuredRaw []byte
	RecordedAtRaw *time.Time
}

func (s *Store) ListBenchmarkSourceRows(ctx context.Context, tenantID int64, days int) ([]benchmarkSourceRow, error) {
	if days <= 0 {
		days = 30
	}
	rows, err := s.pool.Query(ctx, `
		SELECT
			r.id,
			COALESCE(r.employee_id, 0),
			COALESCE(NULLIF(r.business_scope,''), 'unknown') AS role_code,
			COALESCE(r.analysis_result, '{}'::json)::text,
			COALESCE(rar.result_data, '{}'::json)::text,
			r.recorded_at
		FROM recordings r
		LEFT JOIN LATERAL (
			SELECT result_data
			FROM recording_analysis_results
			WHERE recording_id = r.id
			  AND prompt_code = 'doctor_segue_structured'
			ORDER BY is_active DESC, created_at DESC, id DESC
			LIMIT 1
		) rar ON TRUE
		WHERE r.tenant_id = $1
		  AND r.recorded_at >= NOW() - make_interval(days => $2)
		  AND COALESCE(r.analysis_status, '') = 'completed'
		ORDER BY r.recorded_at DESC, r.id DESC
		LIMIT 1000
	`, tenantID, days)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := make([]benchmarkSourceRow, 0)
	for rows.Next() {
		var item benchmarkSourceRow
		if err := rows.Scan(&item.RecordingID, &item.EmployeeID, &item.RoleCode, &item.AnalysisRaw, &item.StructuredRaw, &item.RecordedAtRaw); err != nil {
			return nil, err
		}
		out = append(out, item)
	}
	return out, rows.Err()
}

// GetRecordingByID retrieves a medical recording by ID
func (s *Store) GetRecordingByID(ctx context.Context, id int64) (*MedicalRecording, error) {
	query := `
		SELECT
			r.id,
			r.tenant_id,
			COALESCE(NULLIF(t.name, ''), CONCAT('租户#', r.tenant_id::text)) AS tenant_name,
			r.employee_id,
			COALESCE(
				NULLIF(NULLIF(e.full_name, 'unknown'), ''),
				NULLIF(NULLIF(e.name, 'unknown'), ''),
				NULLIF(e.phone, ''),
				NULLIF(oa.username, ''),
				NULLIF(oa.email, ''),
				'未知员工'
			) AS employee_name,
			COALESCE(
				NULLIF(sbe.device_no, ''),
				NULLIF((regexp_match(COALESCE(r.file_url, ''), '(SSYX[0-9]+)'))[1], ''),
				''
			) AS device_no,
			r.customer_id,
			NULLIF(c.name, '') AS customer_name,
			COALESCE(c.name, '') AS patient_name,
			c.age AS patient_age,
			c.gender AS patient_gender,
			c.phone AS patient_phone,
			r.file_url AS recording_url,
			r.duration AS recording_duration,
			r.transcription_text AS transcript_text,
			NULLIF(r.analysis_display->>'doctor_summary', '') AS doctor_summary,
			NULLIF(r.analysis_display->>'therapist_summary', '') AS therapist_summary,
			NULLIF(r.analysis_display->>'consultant_summary', '') AS consultant_summary,
			COALESCE(NULLIF(r.business_scope, ''), 'unknown') AS business_scope,
			COALESCE(r.analysis_result, '{}'::json) AS analysis_result,
			COALESCE(r.analysis_display, '{}'::jsonb) AS analysis_display,
			NULLIF(r.analysis_status, '') AS analysis_status,
			CASE
				WHEN r.analysis_status = 'completed' THEN 'completed'
				WHEN r.analysis_status = 'failed' OR r.transcription_status = 'failed' THEN 'failed'
				WHEN r.analysis_status = 'pending' OR r.transcription_status = 'pending' THEN 'pending'
				ELSE 'processing'
			END AS status,
			NULL::text AS processing_error,
			r.recorded_at AS recording_started_at,
			NULL::timestamp AS recording_ended_at,
			CASE WHEN r.analysis_status = 'completed' THEN COALESCE(r.updated_at, r.created_at, NOW()) ELSE NULL::timestamp END AS processed_at,
			r.created_at,
			COALESCE(r.updated_at, r.created_at, NOW()) AS updated_at,
			NULL::timestamp AS deleted_at
		FROM recordings r
		LEFT JOIN customers c ON c.id = r.customer_id
		LEFT JOIN employees e ON e.id = r.employee_id
		LEFT JOIN operations_admins oa ON oa.id = r.employee_id
		LEFT JOIN LATERAL (
			SELECT sae.device_no
			FROM smart_badge_audio_events sae
			WHERE sae.recording_id = r.id
			ORDER BY sae.updated_at DESC NULLS LAST, sae.created_at DESC NULLS LAST, sae.id DESC
			LIMIT 1
		) sbe ON TRUE
		LEFT JOIN tenants t ON t.id = r.tenant_id
		WHERE r.id = $1
	`

	var r MedicalRecording
	err := s.pool.QueryRow(ctx, query, id).Scan(
		&r.ID,
		&r.TenantID,
		&r.TenantName,
		&r.EmployeeID,
		&r.EmployeeName,
		&r.DeviceNo,
		&r.CustomerID,
		&r.CustomerName,
		&r.PatientName,
		&r.PatientAge,
		&r.PatientGender,
		&r.PatientPhone,
		&r.RecordingURL,
		&r.RecordingDuration,
		&r.TranscriptText,
		&r.DoctorSummary,
		&r.TherapistSummary,
		&r.ConsultantSummary,
		&r.BusinessScope,
		&r.AnalysisResult,
		&r.AnalysisDisplay,
		&r.AnalysisStatus,
		&r.Status,
		&r.ProcessingError,
		&r.RecordingStartedAt,
		&r.RecordingEndedAt,
		&r.ProcessedAt,
		&r.CreatedAt,
		&r.UpdatedAt,
		&r.DeletedAt,
	)

	if err == pgx.ErrNoRows {
		return nil, fmt.Errorf("recording not found")
	}
	if err != nil {
		return nil, fmt.Errorf("failed to query recording: %w", err)
	}

	return &r, nil
}

func (s *Store) GetRecordingMediaRef(ctx context.Context, id int64) (*RecordingMediaRef, error) {
	const query = `
		SELECT id, tenant_id, COALESCE(file_url, ''), COALESCE(file_name, ''), COALESCE(oss_key, '')
		FROM recordings
		WHERE id = $1
		LIMIT 1
	`
	var ref RecordingMediaRef
	if err := s.pool.QueryRow(ctx, query, id).Scan(&ref.RecordingID, &ref.TenantID, &ref.FileURL, &ref.FileName, &ref.OSSKey); err != nil {
		if err == pgx.ErrNoRows {
			return nil, fmt.Errorf("recording not found")
		}
		return nil, fmt.Errorf("failed to query recording media ref: %w", err)
	}
	return &ref, nil
}

func (s *Store) UpdateRecordingMediaRef(ctx context.Context, id int64, fileURL, ossKey string) error {
	const query = `
		UPDATE recordings
		SET file_url = $2,
		    oss_key = NULLIF($3, ''),
		    updated_at = NOW()
		WHERE id = $1
	`
	if _, err := s.pool.Exec(ctx, query, id, strings.TrimSpace(fileURL), strings.TrimSpace(ossKey)); err != nil {
		return fmt.Errorf("failed to update recording media ref: %w", err)
	}
	return nil
}

func (s *Store) ResolveEmployeeBusinessScope(ctx context.Context, tenantID, employeeID int64) (string, error) {
	var scope string
	err := s.pool.QueryRow(ctx, `
		SELECT CASE
			WHEN EXISTS (
				SELECT 1 FROM institution_employee_roles ier
				WHERE ier.tenant_id = $1 AND ier.employee_id = $2
				  AND lower(ier.role_code) IN ('frontdesk','receptionist','reception')
			) THEN 'frontdesk'
			WHEN EXISTS (
				SELECT 1 FROM institution_employee_roles ier
				WHERE ier.tenant_id = $1 AND ier.employee_id = $2
				  AND lower(ier.role_code) IN ('doctor','doctor_assistant')
			) THEN 'doctor'
			WHEN EXISTS (
				SELECT 1 FROM institution_employee_roles ier
				WHERE ier.tenant_id = $1 AND ier.employee_id = $2
				  AND lower(ier.role_code) = 'consultant'
			) THEN 'consultant'
			WHEN EXISTS (
				SELECT 1 FROM institution_employee_roles ier
				WHERE ier.tenant_id = $1 AND ier.employee_id = $2
				  AND lower(ier.role_code) = 'therapist'
			) THEN 'therapist'
			WHEN EXISTS (
				SELECT 1 FROM institution_employee_roles ier
				WHERE ier.tenant_id = $1 AND ier.employee_id = $2
				  AND lower(ier.role_code) = 'nurse'
			) THEN 'nurse'
			WHEN EXISTS (
				SELECT 1 FROM institution_employee_roles ier
				WHERE ier.tenant_id = $1 AND ier.employee_id = $2
				  AND lower(ier.role_code) = 'lingce_sales'
			) THEN 'lingce_sales'
			ELSE 'unknown'
		END
	`, tenantID, employeeID).Scan(&scope)
	if err != nil {
		return "", fmt.Errorf("resolve employee business scope: %w", err)
	}
	return scope, nil
}

func (s *Store) CreateOwnedAudioRecording(ctx context.Context, req OwnedAudioIngestRequest) (int64, bool, error) {
	var recordingID int64
	var created bool
	err := s.pool.QueryRow(ctx, `
		WITH ins AS (
			INSERT INTO recordings (
				tenant_id, employee_id, file_url, file_name, duration, mime_type,
				source, scene, business_scope, status,
				transcription_status, cleaned_transcription_status, analysis_status,
				recorded_at, order_no, oss_key, created_at, updated_at
			)
			SELECT
				$1::bigint, $2::bigint, $3::text, $4::text, NULLIF($5::integer, 0), $6::text,
				$7::text, $8::text, $9::text, 'uploaded',
				'queued', 'pending', 'pending',
				$10::timestamp, NULLIF($11::text, ''), NULLIF($12::text, ''), NOW(), NOW()
			WHERE NULLIF($11::text, '') IS NULL
			   OR NOT EXISTS (SELECT 1 FROM recordings WHERE order_no = $11::text)
			RETURNING id
		)
		SELECT id, true FROM ins
		UNION ALL
		SELECT id, false FROM recordings
		WHERE NULLIF($11::text, '') IS NOT NULL
		  AND order_no = $11::text
		  AND NOT EXISTS (SELECT 1 FROM ins)
		LIMIT 1
	`, req.TenantID, req.EmployeeID, req.FileURL, req.FileName, req.DurationSeconds, req.MIMEType, req.Source, req.Scene, req.BusinessScope, req.RecordedAt, req.OrderNo, req.OSSKey).Scan(&recordingID, &created)
	if err != nil {
		return 0, false, fmt.Errorf("create owned audio recording: %w", err)
	}
	return recordingID, created, nil
}

// CreateRecording creates a new medical recording
func (s *Store) CreateRecording(ctx context.Context, req CreateRecordingRequest) (*MedicalRecording, error) {
	fileName := req.RecordingURL
	if req.RecordingFileName != nil && strings.TrimSpace(*req.RecordingFileName) != "" {
		fileName = strings.TrimSpace(*req.RecordingFileName)
	}
	if idx := strings.LastIndex(fileName, "/"); idx >= 0 && idx < len(fileName)-1 {
		fileName = fileName[idx+1:]
	}
	if fileName == "" {
		fileName = "recording.wav"
	}
	mimeType := "audio/wav"
	if req.RecordingMimeType != nil && strings.TrimSpace(*req.RecordingMimeType) != "" {
		mimeType = strings.TrimSpace(*req.RecordingMimeType)
	}
	source := "manual"
	if req.Source != nil && strings.TrimSpace(*req.Source) != "" {
		source = strings.TrimSpace(*req.Source)
	}
	scene := "consultation"
	if req.Scene != nil && strings.TrimSpace(*req.Scene) != "" {
		scene = strings.TrimSpace(*req.Scene)
	}
	businessScope := "consultant"
	if req.BusinessScope != nil && strings.TrimSpace(*req.BusinessScope) != "" {
		businessScope = strings.TrimSpace(*req.BusinessScope)
	}

	query := `
		INSERT INTO recordings (
			tenant_id, employee_id, file_url, file_name, duration, mime_type,
			source, scene, business_scope, notes, status, transcription_status, analysis_status,
			recorded_at, created_at, updated_at
		)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, 'uploaded', 'pending', 'pending', NOW(), NOW(), NOW())
		RETURNING id
	`
	var newID int64
	err := s.pool.QueryRow(ctx, query,
		req.TenantID,
		req.EmployeeID,
		req.RecordingURL,
		fileName,
		req.RecordingDuration,
		mimeType,
		source,
		scene,
		businessScope,
		req.PatientName,
	).Scan(&newID)

	if err != nil {
		return nil, fmt.Errorf("failed to create recording: %w", err)
	}
	return s.GetRecordingByID(ctx, newID)
}

// UpdateRecording updates a medical recording
func (s *Store) UpdateRecording(ctx context.Context, id int64, req UpdateRecordingRequest) (*MedicalRecording, error) {
	var setClauses []string
	var args []interface{}
	argIndex := 1

	if req.TranscriptText != nil {
		setClauses = append(setClauses, fmt.Sprintf("transcription_text = $%d", argIndex))
		args = append(args, *req.TranscriptText)
		argIndex++
	}

	if req.CustomerID != nil {
		setClauses = append(setClauses, fmt.Sprintf("customer_id = $%d", argIndex))
		args = append(args, *req.CustomerID)
		argIndex++
	}

	if req.Status != nil {
		if *req.Status == StatusCompleted {
			setClauses = append(setClauses, fmt.Sprintf("analysis_status = $%d", argIndex))
			args = append(args, "completed")
			argIndex++
			setClauses = append(setClauses, fmt.Sprintf("transcription_status = $%d", argIndex))
			args = append(args, "completed")
			argIndex++
		} else if *req.Status == StatusFailed {
			setClauses = append(setClauses, fmt.Sprintf("analysis_status = $%d", argIndex))
			args = append(args, "failed")
			argIndex++
		} else {
			setClauses = append(setClauses, fmt.Sprintf("analysis_status = $%d", argIndex))
			args = append(args, "pending")
			argIndex++
		}
		setClauses = append(setClauses, fmt.Sprintf("status = $%d", argIndex))
		args = append(args, "uploaded")
		argIndex++
	}

	if len(setClauses) == 0 {
		return s.GetRecordingByID(ctx, id)
	}

	setClauses = append(setClauses, "updated_at = NOW()")
	args = append(args, id)

	query := fmt.Sprintf(`
		UPDATE recordings
		SET %s
		WHERE id = $%d
	`, strings.Join(setClauses, ", "), argIndex)
	_, err := s.pool.Exec(ctx, query, args...)

	if err != nil {
		return nil, fmt.Errorf("failed to update recording: %w", err)
	}
	return s.GetRecordingByID(ctx, id)
}

// DeleteRecording deletes a recording
func (s *Store) DeleteRecording(ctx context.Context, id int64) error {
	query := `DELETE FROM recordings WHERE id = $1`

	result, err := s.pool.Exec(ctx, query, id)
	if err != nil {
		return fmt.Errorf("failed to delete recording: %w", err)
	}

	if result.RowsAffected() == 0 {
		return fmt.Errorf("recording not found")
	}

	return nil
}

// Recording Statistics Methods

// GetStatsOverview retrieves overview statistics
func (s *Store) GetStatsOverview(ctx context.Context, tenantID int64, startDate, endDate *string) (*RecordingStatsOverviewResponse, error) {
	query := `
		SELECT 
			COUNT(*) as total_recordings,
			COALESCE(SUM(duration), 0) as total_duration,
			COUNT(CASE WHEN analysis_status = 'completed' THEN 1 END) as completed_recordings,
			COUNT(CASE WHEN analysis_status = 'pending' THEN 1 END) as pending_recordings,
			COUNT(CASE WHEN analysis_status = 'failed' OR transcription_status = 'failed' THEN 1 END) as failed_recordings,
			COALESCE(AVG(duration), 0) as avg_duration,
			COUNT(CASE WHEN DATE(created_at) = CURRENT_DATE THEN 1 END) as today_recordings
		FROM recordings
		WHERE tenant_id = $1
	`

	var stats RecordingStatsOverviewResponse
	err := s.pool.QueryRow(ctx, query, tenantID).Scan(
		&stats.TotalRecordings,
		&stats.TotalDuration,
		&stats.CompletedRecordings,
		&stats.PendingRecordings,
		&stats.FailedRecordings,
		&stats.AvgDuration,
		&stats.TodayRecordings,
	)

	if err != nil {
		return nil, fmt.Errorf("failed to get stats overview: %w", err)
	}

	return &stats, nil
}

// GetStatsByScene retrieves statistics by scene
func (s *Store) GetStatsByScene(ctx context.Context, tenantID int64) ([]RecordingStatsBySceneResponse, error) {
	query := `
		SELECT 
			COALESCE(scene, 'unknown') as scene,
			COUNT(*) as count,
			COALESCE(SUM(duration), 0) as duration,
			ROUND(COUNT(*) * 100.0 / SUM(COUNT(*)) OVER(), 2) as percentage
		FROM recordings
		WHERE tenant_id = $1
		GROUP BY scene
		ORDER BY count DESC
	`

	rows, err := s.pool.Query(ctx, query, tenantID)
	if err != nil {
		return nil, fmt.Errorf("failed to get stats by scene: %w", err)
	}
	defer rows.Close()

	var stats []RecordingStatsBySceneResponse
	for rows.Next() {
		var stat RecordingStatsBySceneResponse
		if err := rows.Scan(&stat.Scene, &stat.Count, &stat.Duration, &stat.Percentage); err != nil {
			return nil, fmt.Errorf("failed to scan stat: %w", err)
		}
		stats = append(stats, stat)
	}

	return stats, nil
}

// GetStatsBySource retrieves statistics by source
func (s *Store) GetStatsBySource(ctx context.Context, tenantID int64) ([]RecordingStatsBySourceResponse, error) {
	query := `
		SELECT 
			COALESCE(source, 'unknown') as source,
			COUNT(*) as count,
			COALESCE(SUM(duration), 0) as duration,
			ROUND(COUNT(*) * 100.0 / SUM(COUNT(*)) OVER(), 2) as percentage
		FROM recordings
		WHERE tenant_id = $1
		GROUP BY source
		ORDER BY count DESC
	`

	rows, err := s.pool.Query(ctx, query, tenantID)
	if err != nil {
		return nil, fmt.Errorf("failed to get stats by source: %w", err)
	}
	defer rows.Close()

	var stats []RecordingStatsBySourceResponse
	for rows.Next() {
		var stat RecordingStatsBySourceResponse
		if err := rows.Scan(&stat.Source, &stat.Count, &stat.Duration, &stat.Percentage); err != nil {
			return nil, fmt.Errorf("failed to scan stat: %w", err)
		}
		stats = append(stats, stat)
	}

	return stats, nil
}

// Recording Task Methods

// ListRecordingTasks retrieves a paginated list of recording tasks
func (s *Store) ListRecordingTasks(ctx context.Context, req TaskListRequest) ([]*RecordingTask, int, error) {
	var conditions []string
	var args []interface{}
	argIndex := 1

	conditions = append(conditions, "1=1")

	if len(req.TenantIDs) > 0 {
		conditions = append(conditions, fmt.Sprintf("tenant_id = ANY($%d)", argIndex))
		args = append(args, req.TenantIDs)
		argIndex++
	} else if req.TenantID != nil {
		conditions = append(conditions, fmt.Sprintf("tenant_id = $%d", argIndex))
		args = append(args, *req.TenantID)
		argIndex++
	}

	if req.RecordingID != nil {
		conditions = append(conditions, fmt.Sprintf("recording_id = $%d", argIndex))
		args = append(args, *req.RecordingID)
		argIndex++
	}

	if req.AssignedTo != nil {
		conditions = append(conditions, fmt.Sprintf("assigned_to = $%d", argIndex))
		args = append(args, *req.AssignedTo)
		argIndex++
	}

	if req.Keyword != nil && strings.TrimSpace(*req.Keyword) != "" {
		conditions = append(conditions, fmt.Sprintf(`(
			COALESCE(title, '') ILIKE $%d OR
			COALESCE(description, '') ILIKE $%d OR
			COALESCE(customer_name, '') ILIKE $%d OR
			COALESCE(script, '') ILIKE $%d OR
			COALESCE(contact_reason, '') ILIKE $%d
		)`, argIndex, argIndex, argIndex, argIndex, argIndex))
		args = append(args, "%"+strings.TrimSpace(*req.Keyword)+"%")
		argIndex++
	}

	if req.Status != nil {
		conditions = append(conditions, fmt.Sprintf("status = $%d", argIndex))
		args = append(args, *req.Status)
		argIndex++
	}

	if req.TaskType != nil {
		conditions = append(conditions, fmt.Sprintf("source_type = $%d", argIndex))
		args = append(args, string(*req.TaskType))
		argIndex++
	}

	if req.Priority != nil && strings.TrimSpace(*req.Priority) != "" {
		switch strings.TrimSpace(*req.Priority) {
		case "high":
			conditions = append(conditions, "lower(COALESCE(priority, 'medium')) IN ('high', 'h', '1', '高')")
		case "low":
			conditions = append(conditions, "lower(COALESCE(priority, 'medium')) IN ('low', 'l', '3', '低')")
		case "medium":
			conditions = append(conditions, "lower(COALESCE(priority, 'medium')) NOT IN ('high', 'h', '1', '高', 'low', 'l', '3', '低')")
		}
	}

	if req.StartDate != nil {
		conditions = append(conditions, fmt.Sprintf("due_at >= $%d", argIndex))
		args = append(args, *req.StartDate)
		argIndex++
	}

	if req.EndDate != nil {
		conditions = append(conditions, fmt.Sprintf("due_at <= $%d", argIndex))
		args = append(args, *req.EndDate)
		argIndex++
	}

	if req.DueBucket != nil && strings.TrimSpace(*req.DueBucket) != "" {
		switch strings.TrimSpace(*req.DueBucket) {
		case "overdue":
			conditions = append(conditions, "due_at IS NOT NULL AND due_at < NOW()")
		case "today":
			conditions = append(conditions, "due_at IS NOT NULL AND due_at >= date_trunc('day', NOW()) AND due_at < date_trunc('day', NOW()) + INTERVAL '1 day'")
		case "tomorrow":
			conditions = append(conditions, "due_at IS NOT NULL AND due_at >= date_trunc('day', NOW()) + INTERVAL '1 day' AND due_at < date_trunc('day', NOW()) + INTERVAL '2 day'")
		case "this_week":
			conditions = append(conditions, "due_at IS NOT NULL AND due_at >= date_trunc('day', NOW()) AND due_at < date_trunc('day', NOW()) + INTERVAL '7 day'")
		case "no_due_date":
			conditions = append(conditions, "due_at IS NULL")
		}
	}

	whereClause := strings.Join(conditions, " AND ")

	// Count total
	countQuery := fmt.Sprintf("SELECT COUNT(*) FROM recording_tasks WHERE %s", whereClause)
	var total int
	if err := s.pool.QueryRow(ctx, countQuery, args...).Scan(&total); err != nil {
		return nil, 0, fmt.Errorf("failed to count tasks: %w", err)
	}

	// Query tasks
	offset := (req.Page - 1) * req.PageSize
	orderBy := "created_at DESC"
	if req.Sort != nil {
		switch strings.TrimSpace(*req.Sort) {
		case "due_at_asc":
			orderBy = "due_at ASC NULLS LAST, created_at DESC"
		case "due_at_desc":
			orderBy = "due_at DESC NULLS LAST, created_at DESC"
		case "priority_desc":
			orderBy = `CASE
				WHEN lower(COALESCE(priority, 'medium')) IN ('high', 'h', '1', '高') THEN 3
				WHEN lower(COALESCE(priority, 'medium')) IN ('low', 'l', '3', '低') THEN 1
				ELSE 2
			END DESC, due_at ASC NULLS LAST, created_at DESC`
		}
	}
	query := fmt.Sprintf(`
		SELECT id, tenant_id, recording_id, source_type AS task_type, title, description,
		       customer_name, priority, script, contact_reason, source_type, source_detail,
		       assigned_to, NULL::bigint AS assigned_by,
		       status, due_at AS due_date, completed_at, NULL::bigint AS completed_by, NULL::timestamp AS cancelled_at, NULL::text AS cancel_reason, created_at, updated_at
		FROM recording_tasks
		WHERE %s
		ORDER BY %s
		LIMIT $%d OFFSET $%d
	`, whereClause, orderBy, argIndex, argIndex+1)

	args = append(args, req.PageSize, offset)

	rows, err := s.pool.Query(ctx, query, args...)
	if err != nil {
		return nil, 0, fmt.Errorf("failed to query tasks: %w", err)
	}
	defer rows.Close()

	var tasks []*RecordingTask
	for rows.Next() {
		var t RecordingTask
		if err := rows.Scan(
			&t.ID, &t.TenantID, &t.RecordingID, &t.TaskType, &t.Title, &t.Description,
			&t.CustomerName, &t.Priority, &t.Script, &t.ContactReason, &t.SourceType, &t.SourceDetail,
			&t.AssignedTo, &t.AssignedBy, &t.Status, &t.DueDate, &t.CompletedAt,
			&t.CompletedBy, &t.CancelledAt, &t.CancelReason, &t.CreatedAt, &t.UpdatedAt,
		); err != nil {
			return nil, 0, fmt.Errorf("failed to scan task: %w", err)
		}
		tasks = append(tasks, &t)
	}

	return tasks, total, nil
}

// GetTaskByID retrieves a recording task by ID
func (s *Store) GetTaskByID(ctx context.Context, id int64) (*RecordingTask, error) {
	query := `
		SELECT id, tenant_id, recording_id, source_type AS task_type, title, description,
		       customer_name, priority, script, contact_reason, source_type, source_detail,
		       assigned_to, NULL::bigint AS assigned_by,
		       status, due_at AS due_date, completed_at, NULL::bigint AS completed_by, NULL::timestamp AS cancelled_at, NULL::text AS cancel_reason, created_at, updated_at
		FROM recording_tasks
		WHERE id = $1
	`

	var t RecordingTask
	err := s.pool.QueryRow(ctx, query, id).Scan(
		&t.ID, &t.TenantID, &t.RecordingID, &t.TaskType, &t.Title, &t.Description,
		&t.CustomerName, &t.Priority, &t.Script, &t.ContactReason, &t.SourceType, &t.SourceDetail,
		&t.AssignedTo, &t.AssignedBy, &t.Status, &t.DueDate, &t.CompletedAt,
		&t.CompletedBy, &t.CancelledAt, &t.CancelReason, &t.CreatedAt, &t.UpdatedAt,
	)

	if err == pgx.ErrNoRows {
		return nil, fmt.Errorf("task not found")
	}
	if err != nil {
		return nil, fmt.Errorf("failed to query task: %w", err)
	}

	return &t, nil
}

// CompleteTask marks a task as completed
func (s *Store) CompleteTask(ctx context.Context, id int64, completedBy int64) error {
	query := `
		UPDATE recording_tasks
		SET status = $1, completed_at = NOW(), feedback = CONCAT(COALESCE(feedback, ''), CASE WHEN COALESCE(feedback, '') = '' THEN '' ELSE E'\n' END, 'completed_by=', $2::text), updated_at = NOW()
		WHERE id = $3 AND status != $4
	`

	result, err := s.pool.Exec(ctx, query, TaskStatusCompleted, completedBy, id, TaskStatusCompleted)
	if err != nil {
		return fmt.Errorf("failed to complete task: %w", err)
	}

	if result.RowsAffected() == 0 {
		return fmt.Errorf("task not found or already completed")
	}

	return nil
}

// CancelTask marks a task as cancelled
func (s *Store) CancelTask(ctx context.Context, id int64, reason string) error {
	query := `
		UPDATE recording_tasks
		SET status = $1, feedback = $2, updated_at = NOW()
		WHERE id = $3 AND status NOT IN ($4, $5)
	`

	result, err := s.pool.Exec(ctx, query, TaskStatusCancelled, reason, id, TaskStatusCompleted, TaskStatusCancelled)
	if err != nil {
		return fmt.Errorf("failed to cancel task: %w", err)
	}

	if result.RowsAffected() == 0 {
		return fmt.Errorf("task not found or cannot be cancelled")
	}

	return nil
}

// GetTaskStats retrieves task statistics for a tenant
func (s *Store) GetTaskStats(ctx context.Context, tenantID int64, tenantIDs []int64, assignedTo *int64) (*RecordingTaskStatsResponse, error) {
	var conditions []string
	var args []interface{}
	argIndex := 1

	if len(tenantIDs) > 0 {
		conditions = append(conditions, fmt.Sprintf("t.tenant_id = ANY($%d)", argIndex))
		args = append(args, tenantIDs)
		argIndex++
	} else {
		conditions = append(conditions, fmt.Sprintf("t.tenant_id = $%d", argIndex))
		args = append(args, tenantID)
		argIndex++
	}

	if assignedTo != nil {
		conditions = append(conditions, fmt.Sprintf("t.assigned_to = $%d", argIndex))
		args = append(args, *assignedTo)
		argIndex++
	}

	whereClause := strings.Join(conditions, " AND ")

	query := fmt.Sprintf(`
		SELECT
			COUNT(*) AS total_tasks,
			COUNT(CASE WHEN t.status = 'pending' THEN 1 END) AS pending_tasks,
			COUNT(CASE WHEN t.status = 'assigned' THEN 1 END) AS assigned_tasks,
			COUNT(CASE WHEN t.status = 'completed' THEN 1 END) AS completed_tasks,
			COUNT(CASE WHEN t.status = 'cancelled' THEN 1 END) AS cancelled_tasks,
			COUNT(CASE
				WHEN t.due_at < NOW() AND t.status NOT IN ('completed', 'cancelled')
				THEN 1
			END) AS overdue_tasks
		FROM recording_tasks t
		WHERE %s
	`, whereClause)

	stats := RecordingTaskStatsResponse{
		ByPriority: map[string]int64{
			"high":   0,
			"medium": 0,
			"low":    0,
		},
		ByRecordingRole: map[string]int64{
			"consultant":       0,
			"customer_service": 0,
			"doctor":           0,
			"therapist":        0,
			"doctor_assistant": 0,
			"other":            0,
		},
	}
	if err := s.pool.QueryRow(ctx, query, args...).Scan(
		&stats.TotalTasks,
		&stats.PendingTasks,
		&stats.AssignedTasks,
		&stats.CompletedTasks,
		&stats.CancelledTasks,
		&stats.OverdueTasks,
	); err != nil {
		return nil, fmt.Errorf("failed to get task stats: %w", err)
	}

	priorityQuery := fmt.Sprintf(`
		SELECT
			CASE
				WHEN lower(COALESCE(t.priority, 'medium')) IN ('high', 'h', '高') THEN 'high'
				WHEN lower(COALESCE(t.priority, 'medium')) IN ('low', 'l', '低') THEN 'low'
				ELSE 'medium'
			END AS priority_bucket,
			COUNT(*) AS cnt
		FROM recording_tasks t
		WHERE %s
		GROUP BY 1
	`, whereClause)
	if rows, err := s.pool.Query(ctx, priorityQuery, args...); err != nil {
		return nil, fmt.Errorf("failed to get task priority stats: %w", err)
	} else {
		defer rows.Close()
		for rows.Next() {
			var bucket string
			var cnt int64
			if err := rows.Scan(&bucket, &cnt); err != nil {
				return nil, fmt.Errorf("failed to scan task priority stats: %w", err)
			}
			stats.ByPriority[bucket] = cnt
		}
	}

	roleQuery := fmt.Sprintf(`
		SELECT role_category, COUNT(*) AS cnt
		FROM (
			SELECT
					CASE
						WHEN EXISTS (
							SELECT 1 FROM institution_employee_roles ier
							WHERE ier.employee_id = r.employee_id
							  AND lower(ier.role_code) = 'doctor'
						) THEN 'doctor'
						WHEN EXISTS (
							SELECT 1 FROM institution_employee_roles ier
							WHERE ier.employee_id = r.employee_id
							  AND lower(ier.role_code) = 'therapist'
						) THEN 'therapist'
						WHEN EXISTS (
							SELECT 1 FROM institution_employee_roles ier
							WHERE ier.employee_id = r.employee_id
							  AND lower(ier.role_code) = 'doctor_assistant'
						) THEN 'doctor_assistant'
						WHEN EXISTS (
							SELECT 1 FROM institution_employee_roles ier
							WHERE ier.employee_id = r.employee_id
							  AND lower(ier.role_code) = ANY($%d)
						) THEN 'customer_service'
						WHEN EXISTS (
							SELECT 1 FROM institution_employee_roles ier
							WHERE ier.employee_id = r.employee_id
							  AND lower(ier.role_code) = ANY($%d)
						) THEN 'consultant'
					ELSE 'other'
				END AS role_category
			FROM recording_tasks t
			LEFT JOIN recordings r ON r.id = t.recording_id AND r.tenant_id = t.tenant_id
			WHERE %s
		) role_rows
		GROUP BY role_category
	`, argIndex, argIndex+1, whereClause)
	roleArgs := append(append([]interface{}{}, args...), []string{"customer_service", "service", "cs", "frontdesk", "reception"}, []string{"consultant"})
	if rows, err := s.pool.Query(ctx, roleQuery, roleArgs...); err != nil {
		return nil, fmt.Errorf("failed to get task recording role stats: %w", err)
	} else {
		defer rows.Close()
		for rows.Next() {
			var category string
			var cnt int64
			if err := rows.Scan(&category, &cnt); err != nil {
				return nil, fmt.Errorf("failed to scan task recording role stats: %w", err)
			}
			if _, ok := stats.ByRecordingRole[category]; ok {
				stats.ByRecordingRole[category] = cnt
			}
		}
	}

	return &stats, nil
}

// GetDailyBriefing retrieves daily task briefing for a tenant
func (s *Store) GetDailyBriefing(ctx context.Context, tenantID int64, assignedTo *int64, date time.Time) (*DailyBriefingResponse, error) {
	var conditions []string
	var args []interface{}
	argIndex := 1

	conditions = append(conditions, fmt.Sprintf("tenant_id = $%d", argIndex))
	args = append(args, tenantID)
	argIndex++

	if assignedTo != nil {
		conditions = append(conditions, fmt.Sprintf("assigned_to = $%d", argIndex))
		args = append(args, *assignedTo)
		argIndex++
	}

	whereClause := strings.Join(conditions, " AND ")
	dayStart := time.Date(date.Year(), date.Month(), date.Day(), 0, 0, 0, 0, date.Location())
	dayEnd := dayStart.Add(24 * time.Hour)

	// Reuse day bounds multiple times in the same query.
	startIdx1, endIdx1 := argIndex, argIndex+1
	startIdx2, endIdx2 := argIndex+2, argIndex+3
	endIdx3 := argIndex + 4
	args = append(args, dayStart, dayEnd, dayStart, dayEnd, dayEnd)

	query := fmt.Sprintf(`
		SELECT
			COUNT(CASE WHEN created_at >= $%d AND created_at < $%d THEN 1 END) AS today_tasks,
			COUNT(CASE WHEN status = 'completed' AND completed_at >= $%d AND completed_at < $%d THEN 1 END) AS completed_tasks,
			COUNT(CASE WHEN status IN ('pending', 'assigned') THEN 1 END) AS pending_tasks,
			COUNT(CASE
				WHEN due_at IS NOT NULL
					AND due_at < $%d
					AND status IN ('pending', 'assigned')
				THEN 1
			END) AS high_priority_tasks
		FROM recording_tasks
		WHERE %s
	`, startIdx1, endIdx1, startIdx2, endIdx2, endIdx3, whereClause)

	briefing := &DailyBriefingResponse{
		Date: dayStart.Format("2006-01-02"),
	}
	if err := s.pool.QueryRow(ctx, query, args...).Scan(
		&briefing.TodayTasks,
		&briefing.CompletedTasks,
		&briefing.PendingTasks,
		&briefing.HighPriorityTasks,
	); err != nil {
		return nil, fmt.Errorf("failed to get daily briefing: %w", err)
	}

	briefing.Summary = fmt.Sprintf(
		"今日新增%d项任务，完成%d项，待处理%d项，高优先级%d项。",
		briefing.TodayTasks,
		briefing.CompletedTasks,
		briefing.PendingTasks,
		briefing.HighPriorityTasks,
	)

	return briefing, nil
}

// Recording Prompt Methods

// ListRecordingPrompts retrieves a paginated list of recording prompts
func (s *Store) ListRecordingPrompts(ctx context.Context, req RecordingPromptListRequest) ([]*RecordingPrompt, int, error) {
	var conditions []string
	var args []interface{}
	argIndex := 1

	conditions = append(conditions, "1=1")

	if req.Code != nil {
		conditions = append(conditions, fmt.Sprintf("code ILIKE $%d", argIndex))
		args = append(args, "%"+*req.Code+"%")
		argIndex++
	}

	if req.Name != nil {
		conditions = append(conditions, fmt.Sprintf("name ILIKE $%d", argIndex))
		args = append(args, "%"+*req.Name+"%")
		argIndex++
	}

	if req.IsActive != nil {
		conditions = append(conditions, fmt.Sprintf("is_active = $%d", argIndex))
		args = append(args, *req.IsActive)
		argIndex++
	}

	whereClause := strings.Join(conditions, " AND ")

	// Count total
	countQuery := fmt.Sprintf("SELECT COUNT(*) FROM recording_analysis_prompts WHERE %s", whereClause)
	var total int
	if err := s.pool.QueryRow(ctx, countQuery, args...).Scan(&total); err != nil {
		return nil, 0, fmt.Errorf("failed to count prompts: %w", err)
	}

	// Query prompts
	offset := (req.Page - 1) * req.PageSize
	query := fmt.Sprintf(`
		SELECT id, code, name, description, COALESCE(category, 'default') AS category,
		       COALESCE(system_prompt, '') AS system_prompt,
		       user_prompt_template AS prompt_text, COALESCE(output_schema, '{}'::json) AS output_schema,
		       COALESCE(version, 'v1') AS version, '[]'::json AS variables, is_active, created_at, updated_at
		FROM recording_analysis_prompts
		WHERE %s
		ORDER BY created_at DESC
		LIMIT $%d OFFSET $%d
	`, whereClause, argIndex, argIndex+1)

	args = append(args, req.PageSize, offset)

	rows, err := s.pool.Query(ctx, query, args...)
	if err != nil {
		return nil, 0, fmt.Errorf("failed to query prompts: %w", err)
	}
	defer rows.Close()

	var prompts []*RecordingPrompt
	for rows.Next() {
		var p RecordingPrompt
		if err := rows.Scan(
			&p.ID, &p.Code, &p.Name, &p.Description, &p.Category, &p.SystemPrompt,
			&p.PromptText, &p.OutputSchema, &p.Version, &p.Variables, &p.IsActive, &p.CreatedAt, &p.UpdatedAt,
		); err != nil {
			return nil, 0, fmt.Errorf("failed to scan prompt: %w", err)
		}
		prompts = append(prompts, &p)
	}

	return prompts, total, nil
}

// GetRecordingPromptByCode retrieves a recording prompt by code
func (s *Store) GetRecordingPromptByCode(ctx context.Context, code string) (*RecordingPrompt, error) {
	query := `
		SELECT id, code, name, description, COALESCE(category, 'default') AS category,
		       COALESCE(system_prompt, '') AS system_prompt,
		       user_prompt_template AS prompt_text, COALESCE(output_schema, '{}'::json) AS output_schema,
		       COALESCE(version, 'v1') AS version, '[]'::json AS variables, is_active, created_at, updated_at
		FROM recording_analysis_prompts
		WHERE code = $1
	`

	var p RecordingPrompt
	err := s.pool.QueryRow(ctx, query, code).Scan(
		&p.ID, &p.Code, &p.Name, &p.Description, &p.Category, &p.SystemPrompt,
		&p.PromptText, &p.OutputSchema, &p.Version, &p.Variables, &p.IsActive, &p.CreatedAt, &p.UpdatedAt,
	)

	if err == pgx.ErrNoRows {
		return nil, fmt.Errorf("prompt not found")
	}
	if err != nil {
		return nil, fmt.Errorf("failed to query prompt: %w", err)
	}

	return &p, nil
}

// CreateRecordingPrompt creates a new recording prompt
func (s *Store) CreateRecordingPrompt(ctx context.Context, req CreateRecordingPromptRequest) (*RecordingPrompt, error) {
	query := `
		INSERT INTO recording_analysis_prompts (code, name, description, category, system_prompt, user_prompt_template, output_schema, version, is_active, created_by, updated_by, created_at, updated_at)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, 1, 1, NOW(), NOW())
		RETURNING id, code, name, description, COALESCE(category, 'default') AS category,
		          COALESCE(system_prompt, '') AS system_prompt,
		          user_prompt_template AS prompt_text, COALESCE(output_schema, '{}'::json) AS output_schema,
		          COALESCE(version, 'v1') AS version, '[]'::json AS variables, is_active, created_at, updated_at
	`

	var p RecordingPrompt
	category := "default"
	if req.Category != nil && strings.TrimSpace(*req.Category) != "" {
		category = strings.TrimSpace(*req.Category)
	}
	version := "v1"
	if req.Version != nil && strings.TrimSpace(*req.Version) != "" {
		version = strings.TrimSpace(*req.Version)
	}
	outputSchema := JSONObject{}
	if req.OutputSchema != nil {
		outputSchema = req.OutputSchema
	}
	err := s.pool.QueryRow(
		ctx, query, req.Code, req.Name, req.Description, category, req.SystemPrompt, req.PromptText, outputSchema, version, req.IsActive,
	).Scan(
		&p.ID, &p.Code, &p.Name, &p.Description, &p.Category, &p.SystemPrompt,
		&p.PromptText, &p.OutputSchema, &p.Version, &p.Variables, &p.IsActive, &p.CreatedAt, &p.UpdatedAt,
	)

	if err != nil {
		return nil, fmt.Errorf("failed to create prompt: %w", err)
	}

	return &p, nil
}

// UpdateRecordingPrompt updates a recording prompt
func (s *Store) UpdateRecordingPrompt(ctx context.Context, code string, req UpdateRecordingPromptRequest) (*RecordingPrompt, error) {
	var setClauses []string
	var args []interface{}
	argIndex := 1

	if req.Name != nil {
		setClauses = append(setClauses, fmt.Sprintf("name = $%d", argIndex))
		args = append(args, *req.Name)
		argIndex++
	}

	if req.Description != nil {
		setClauses = append(setClauses, fmt.Sprintf("description = $%d", argIndex))
		args = append(args, *req.Description)
		argIndex++
	}

	if req.PromptText != nil {
		setClauses = append(setClauses, fmt.Sprintf("user_prompt_template = $%d", argIndex))
		args = append(args, *req.PromptText)
		argIndex++
	}
	if req.SystemPrompt != nil {
		setClauses = append(setClauses, fmt.Sprintf("system_prompt = $%d", argIndex))
		args = append(args, *req.SystemPrompt)
		argIndex++
	}
	if req.Category != nil {
		setClauses = append(setClauses, fmt.Sprintf("category = $%d", argIndex))
		args = append(args, *req.Category)
		argIndex++
	}
	if req.Version != nil {
		setClauses = append(setClauses, fmt.Sprintf("version = $%d", argIndex))
		args = append(args, *req.Version)
		argIndex++
	}
	if req.OutputSchema != nil {
		setClauses = append(setClauses, fmt.Sprintf("output_schema = $%d", argIndex))
		args = append(args, req.OutputSchema)
		argIndex++
	}

	if req.IsActive != nil {
		setClauses = append(setClauses, fmt.Sprintf("is_active = $%d", argIndex))
		args = append(args, *req.IsActive)
		argIndex++
	}

	if len(setClauses) == 0 {
		return s.GetRecordingPromptByCode(ctx, code)
	}

	setClauses = append(setClauses, "updated_at = NOW()")
	args = append(args, code)

	query := fmt.Sprintf(`
		UPDATE recording_analysis_prompts
		SET %s
		WHERE code = $%d
		RETURNING id, code, name, description, COALESCE(category, 'default') AS category,
		          COALESCE(system_prompt, '') AS system_prompt,
		          user_prompt_template AS prompt_text, COALESCE(output_schema, '{}'::json) AS output_schema,
		          COALESCE(version, 'v1') AS version, '[]'::json AS variables, is_active, created_at, updated_at
	`, strings.Join(setClauses, ", "), argIndex)

	var p RecordingPrompt
	err := s.pool.QueryRow(ctx, query, args...).Scan(
		&p.ID, &p.Code, &p.Name, &p.Description, &p.Category, &p.SystemPrompt,
		&p.PromptText, &p.OutputSchema, &p.Version, &p.Variables, &p.IsActive, &p.CreatedAt, &p.UpdatedAt,
	)

	if err == pgx.ErrNoRows {
		return nil, fmt.Errorf("prompt not found")
	}
	if err != nil {
		return nil, fmt.Errorf("failed to update prompt: %w", err)
	}

	return &p, nil
}

// DeleteRecordingPrompt soft deletes a recording prompt
func (s *Store) DeleteRecordingPrompt(ctx context.Context, code string) error {
	query := `
		DELETE FROM recording_analysis_prompts
		WHERE code = $1
	`

	result, err := s.pool.Exec(ctx, query, code)
	if err != nil {
		return fmt.Errorf("failed to delete prompt: %w", err)
	}

	if result.RowsAffected() == 0 {
		return fmt.Errorf("prompt not found")
	}

	return nil
}

// Best Practice Methods

// ListBestPractices retrieves a list of best practices
func (s *Store) ListBestPractices(ctx context.Context, tenantID int64) ([]*RecordingBestPractice, error) {
	query := `
		SELECT id, tenant_id, recording_id, dimension AS title, note AS description, NULL::text AS category, '[]'::json AS tags, created_by, created_at, created_at AS updated_at
		FROM recording_best_practices
		WHERE tenant_id = $1
		ORDER BY created_at DESC
	`

	rows, err := s.pool.Query(ctx, query, tenantID)
	if err != nil {
		return nil, fmt.Errorf("failed to query best practices: %w", err)
	}
	defer rows.Close()

	var practices []*RecordingBestPractice
	for rows.Next() {
		var p RecordingBestPractice
		if err := rows.Scan(&p.ID, &p.TenantID, &p.RecordingID, &p.Title, &p.Description, &p.Category, &p.Tags, &p.CreatedBy, &p.CreatedAt, &p.UpdatedAt); err != nil {
			return nil, fmt.Errorf("failed to scan best practice: %w", err)
		}
		practices = append(practices, &p)
	}

	return practices, nil
}

// AddBestPractice adds a recording to best practices
func (s *Store) AddBestPractice(ctx context.Context, tenantID, recordingID, createdBy int64, req AddBestPracticeRequest) (*RecordingBestPractice, error) {
	query := `
		INSERT INTO recording_best_practices (tenant_id, recording_id, dimension, note, created_by, created_at)
		VALUES ($1, $2, $3, $4, $5, NOW())
		RETURNING id, tenant_id, recording_id, dimension AS title, note AS description, NULL::text AS category, '[]'::json AS tags, created_by, created_at, created_at AS updated_at
	`

	var p RecordingBestPractice
	err := s.pool.QueryRow(ctx, query, tenantID, recordingID, req.Title, req.Description, createdBy).Scan(
		&p.ID, &p.TenantID, &p.RecordingID, &p.Title, &p.Description, &p.Category, &p.Tags, &p.CreatedBy, &p.CreatedAt, &p.UpdatedAt,
	)

	if err != nil {
		return nil, fmt.Errorf("failed to add best practice: %w", err)
	}

	return &p, nil
}

// DeleteBestPractice removes a recording from best practices
func (s *Store) DeleteBestPractice(ctx context.Context, recordingID int64) error {
	query := `
		DELETE FROM recording_best_practices
		WHERE recording_id = $1
	`

	result, err := s.pool.Exec(ctx, query, recordingID)
	if err != nil {
		return fmt.Errorf("failed to delete best practice: %w", err)
	}

	if result.RowsAffected() == 0 {
		return fmt.Errorf("best practice not found")
	}

	return nil
}

// Front-desk analysis methods

// ListShiftAnalyses lists shift analyses for a tenant
func (s *Store) ListShiftAnalyses(ctx context.Context, tenantID int64, page, pageSize int, employeeID *int64, dateStr string) ([]map[string]interface{}, int64, error) {
	offset := (page - 1) * pageSize

	query := `
		SELECT id, tenant_id, recording_id, employee_id, shift_date::text, shift_type,
		       recording_duration_seconds, estimated_interaction_count, estimated_appointment_count,
		       estimated_walkin_count, analysis_json, created_at
		FROM frontdesk_shift_analyses
		WHERE tenant_id = $1
	`
	countQuery := `SELECT COUNT(*) FROM frontdesk_shift_analyses WHERE tenant_id = $1`

	args := []interface{}{tenantID}
	countArgs := []interface{}{tenantID}

	if employeeID != nil {
		query += ` AND employee_id = $2`
		countQuery += ` AND employee_id = $2`
		args = append(args, *employeeID)
		countArgs = append(countArgs, *employeeID)
	}

	if dateStr != "" {
		argIdx := len(args) + 1
		query += fmt.Sprintf(` AND shift_date = $%d`, argIdx)
		countQuery += fmt.Sprintf(` AND shift_date = $%d`, argIdx)
		args = append(args, dateStr)
		countArgs = append(countArgs, dateStr)
	}

	query += ` ORDER BY shift_date DESC, created_at DESC LIMIT $` + fmt.Sprintf("%d", len(args)+1) + ` OFFSET $` + fmt.Sprintf("%d", len(args)+2)
	args = append(args, pageSize, offset)

	rows, err := s.pool.Query(ctx, query, args...)
	if err != nil {
		return nil, 0, fmt.Errorf("query shift analyses: %w", err)
	}
	defer rows.Close()

	var analyses []map[string]interface{}
	for rows.Next() {
		var id, tenantID, recordingID, employeeID int64
		var shiftDate, shiftType string
		var recordingDurationSeconds, estimatedInteractionCount, estimatedAppointmentCount, estimatedWalkinCount int
		var analysisJSON map[string]interface{}
		var createdAt time.Time

		if err := rows.Scan(&id, &tenantID, &recordingID, &employeeID, &shiftDate, &shiftType,
			&recordingDurationSeconds, &estimatedInteractionCount, &estimatedAppointmentCount,
			&estimatedWalkinCount, &analysisJSON, &createdAt); err != nil {
			return nil, 0, fmt.Errorf("scan shift analysis: %w", err)
		}

		analyses = append(analyses, map[string]interface{}{
			"id":                          id,
			"tenant_id":                   tenantID,
			"recording_id":                recordingID,
			"employee_id":                 employeeID,
			"shift_date":                  shiftDate,
			"shift_type":                  shiftType,
			"recording_duration_seconds":  recordingDurationSeconds,
			"estimated_interaction_count": estimatedInteractionCount,
			"estimated_appointment_count": estimatedAppointmentCount,
			"estimated_walkin_count":      estimatedWalkinCount,
			"analysis_json":               normalizeFrontdeskShiftAnalysisJSON(analysisJSON),
			"created_at":                  createdAt.Format("2006-01-02 15:04:05"),
		})
	}

	var total int64
	if err := s.pool.QueryRow(ctx, countQuery, countArgs...).Scan(&total); err != nil {
		return nil, 0, fmt.Errorf("count shift analyses: %w", err)
	}

	return analyses, total, nil
}

// GetShiftAnalysis gets a single shift analysis
func (s *Store) GetShiftAnalysis(ctx context.Context, tenantID, id int64) (map[string]interface{}, error) {
	query := `
		SELECT id, tenant_id, recording_id, employee_id, shift_date::text, shift_type,
		       recording_duration_seconds, estimated_interaction_count, estimated_appointment_count,
		       estimated_walkin_count, analysis_json, transcript, created_at
		FROM frontdesk_shift_analyses
		WHERE tenant_id = $1 AND id = $2
	`

	var recordingID, employeeID int64
	var shiftDate, shiftType string
	var recordingDurationSeconds, estimatedInteractionCount, estimatedAppointmentCount, estimatedWalkinCount int
	var analysisJSON map[string]interface{}
	var transcript *string
	var createdAt time.Time

	if err := s.pool.QueryRow(ctx, query, tenantID, id).Scan(&id, &tenantID, &recordingID, &employeeID, &shiftDate, &shiftType,
		&recordingDurationSeconds, &estimatedInteractionCount, &estimatedAppointmentCount,
		&estimatedWalkinCount, &analysisJSON, &transcript, &createdAt); err != nil {
		if err == pgx.ErrNoRows {
			return nil, fmt.Errorf("shift analysis not found")
		}
		return nil, fmt.Errorf("query shift analysis: %w", err)
	}

	return map[string]interface{}{
		"id":                          id,
		"tenant_id":                   tenantID,
		"recording_id":                recordingID,
		"employee_id":                 employeeID,
		"shift_date":                  shiftDate,
		"shift_type":                  shiftType,
		"recording_duration_seconds":  recordingDurationSeconds,
		"estimated_interaction_count": estimatedInteractionCount,
		"estimated_appointment_count": estimatedAppointmentCount,
		"estimated_walkin_count":      estimatedWalkinCount,
		"analysis_json":               normalizeFrontdeskShiftAnalysisJSON(analysisJSON),
		"transcript":                  transcript,
		"created_at":                  createdAt.Format("2006-01-02 15:04:05"),
	}, nil
}

// CreateShiftAnalysis creates a new shift analysis
func (s *Store) CreateShiftAnalysis(ctx context.Context, tenantID int64, req *ShiftAnalysisCreateReq) (map[string]interface{}, error) {
	query := `
		INSERT INTO frontdesk_shift_analyses (tenant_id, recording_id, employee_id, shift_date, shift_type,
		                            recording_duration_seconds, estimated_interaction_count,
		                            estimated_appointment_count, estimated_walkin_count,
		                            analysis_json, transcript, created_at)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, NOW())
		RETURNING id, tenant_id, recording_id, employee_id, shift_date::text, shift_type,
		          recording_duration_seconds, estimated_interaction_count, estimated_appointment_count,
		          estimated_walkin_count, analysis_json, created_at
	`

	var id, recordingID, employeeID int64
	var shiftDate, shiftType string
	var recordingDurationSeconds, estimatedInteractionCount, estimatedAppointmentCount, estimatedWalkinCount int
	var analysisJSON map[string]interface{}
	var createdAt time.Time

	if err := s.pool.QueryRow(ctx, query, tenantID, req.RecordingID, req.EmployeeID, req.ShiftDate, req.ShiftType,
		req.RecordingDurationSeconds, req.EstimatedInteractionCount, req.EstimatedAppointmentCount,
		req.EstimatedWalkinCount, req.AnalysisJSON, req.Transcript).Scan(&id, &tenantID, &recordingID, &employeeID,
		&shiftDate, &shiftType, &recordingDurationSeconds, &estimatedInteractionCount, &estimatedAppointmentCount,
		&estimatedWalkinCount, &analysisJSON, &createdAt); err != nil {
		return nil, fmt.Errorf("insert shift analysis: %w", err)
	}

	return map[string]interface{}{
		"id":                          id,
		"tenant_id":                   tenantID,
		"recording_id":                recordingID,
		"employee_id":                 employeeID,
		"shift_date":                  shiftDate,
		"shift_type":                  shiftType,
		"recording_duration_seconds":  recordingDurationSeconds,
		"estimated_interaction_count": estimatedInteractionCount,
		"estimated_appointment_count": estimatedAppointmentCount,
		"estimated_walkin_count":      estimatedWalkinCount,
		"analysis_json":               normalizeFrontdeskShiftAnalysisJSON(analysisJSON),
		"created_at":                  createdAt.Format("2006-01-02 15:04:05"),
	}, nil
}

// ListDailyReports lists daily reports for a tenant
func (s *Store) ListDailyReports(ctx context.Context, tenantID int64, page, pageSize int) ([]map[string]interface{}, int64, error) {
	offset := (page - 1) * pageSize

	query := `
		SELECT id, tenant_id, report_date, total_estimated_interactions, estimated_appointment_count,
		       estimated_walkin_count, estimated_walkin_capture_rate, top_questions, competitor_mentions,
		       doctor_inquiries, channel_feedback, lost_reasons, risk_event_count, testimonial_materials, created_at
		FROM frontdesk_daily_reports
		WHERE tenant_id = $1
		ORDER BY report_date DESC
		LIMIT $2 OFFSET $3
	`

	rows, err := s.pool.Query(ctx, query, tenantID, pageSize, offset)
	if err != nil {
		return nil, 0, fmt.Errorf("query daily reports: %w", err)
	}
	defer rows.Close()

	var reports []map[string]interface{}
	for rows.Next() {
		var id, tenantID int64
		var reportDate time.Time
		var totalEstimatedInteractions, estimatedAppointmentCount, estimatedWalkinCount, riskEventCount int
		var estimatedWalkinCaptureRate sql.NullFloat64
		var topQuestions, competitorMentions, doctorInquiries, channelFeedback, lostReasons, testimonialMaterials map[string]interface{}
		var createdAt time.Time

		if err := rows.Scan(&id, &tenantID, &reportDate, &totalEstimatedInteractions, &estimatedAppointmentCount,
			&estimatedWalkinCount, &estimatedWalkinCaptureRate, &topQuestions, &competitorMentions,
			&doctorInquiries, &channelFeedback, &lostReasons, &riskEventCount, &testimonialMaterials, &createdAt); err != nil {
			return nil, 0, fmt.Errorf("scan daily report: %w", err)
		}

		walkinCaptureRate := 0.0
		if estimatedWalkinCaptureRate.Valid {
			walkinCaptureRate = estimatedWalkinCaptureRate.Float64
		}

		item := map[string]interface{}{
			"id":                            id,
			"tenant_id":                     tenantID,
			"report_date":                   reportDate.Format("2006-01-02"),
			"total_estimated_interactions":  totalEstimatedInteractions,
			"estimated_appointment_count":   estimatedAppointmentCount,
			"estimated_walkin_count":        estimatedWalkinCount,
			"estimated_walkin_capture_rate": walkinCaptureRate,
			"top_questions":                 topQuestions,
			"competitor_mentions":           competitorMentions,
			"doctor_inquiries":              doctorInquiries,
			"channel_feedback":              channelFeedback,
			"lost_reasons":                  lostReasons,
			"risk_event_count":              riskEventCount,
			"testimonial_materials":         testimonialMaterials,
			"created_at":                    createdAt.Format("2006-01-02 15:04:05"),
		}
		mergeDailyExtendedFields(item, testimonialMaterials)
		reports = append(reports, item)
	}

	var total int64
	if err := s.pool.QueryRow(ctx, `SELECT COUNT(*) FROM frontdesk_daily_reports WHERE tenant_id = $1`, tenantID).Scan(&total); err != nil {
		return nil, 0, fmt.Errorf("count daily reports: %w", err)
	}

	return reports, total, nil
}

// GetFrontdeskDailyReport gets a frontdesk daily report by date
func (s *Store) GetFrontdeskDailyReport(ctx context.Context, tenantID int64, dateStr string) (map[string]interface{}, error) {
	query := `
		SELECT id, tenant_id, report_date, total_estimated_interactions, estimated_appointment_count,
		       estimated_walkin_count, estimated_walkin_capture_rate, top_questions, competitor_mentions,
		       doctor_inquiries, channel_feedback, lost_reasons, risk_event_count, testimonial_materials, created_at
		FROM frontdesk_daily_reports
		WHERE tenant_id = $1 AND report_date = $2
	`

	var id int64
	var reportDate time.Time
	var totalEstimatedInteractions, estimatedAppointmentCount, estimatedWalkinCount, riskEventCount int
	var estimatedWalkinCaptureRate sql.NullFloat64
	var topQuestions, competitorMentions, doctorInquiries, channelFeedback, lostReasons, testimonialMaterials map[string]interface{}
	var createdAt time.Time

	if err := s.pool.QueryRow(ctx, query, tenantID, dateStr).Scan(&id, &tenantID, &reportDate, &totalEstimatedInteractions,
		&estimatedAppointmentCount, &estimatedWalkinCount, &estimatedWalkinCaptureRate, &topQuestions, &competitorMentions,
		&doctorInquiries, &channelFeedback, &lostReasons, &riskEventCount, &testimonialMaterials, &createdAt); err != nil {
		if err == pgx.ErrNoRows {
			return nil, fmt.Errorf("daily report not found")
		}
		return nil, fmt.Errorf("query daily report: %w", err)
	}

	walkinCaptureRate := 0.0
	if estimatedWalkinCaptureRate.Valid {
		walkinCaptureRate = estimatedWalkinCaptureRate.Float64
	}

	item := map[string]interface{}{
		"id":                            id,
		"tenant_id":                     tenantID,
		"report_date":                   reportDate.Format("2006-01-02"),
		"total_estimated_interactions":  totalEstimatedInteractions,
		"estimated_appointment_count":   estimatedAppointmentCount,
		"estimated_walkin_count":        estimatedWalkinCount,
		"estimated_walkin_capture_rate": walkinCaptureRate,
		"top_questions":                 topQuestions,
		"competitor_mentions":           competitorMentions,
		"doctor_inquiries":              doctorInquiries,
		"channel_feedback":              channelFeedback,
		"lost_reasons":                  lostReasons,
		"risk_event_count":              riskEventCount,
		"testimonial_materials":         testimonialMaterials,
		"created_at":                    createdAt.Format("2006-01-02 15:04:05"),
	}
	mergeDailyExtendedFields(item, testimonialMaterials)
	return item, nil
}

func mergeDailyExtendedFields(target map[string]interface{}, testimonial map[string]interface{}) {
	if target == nil {
		return
	}
	var source map[string]interface{}
	if testimonial != nil {
		source = testimonial
	} else {
		source = map[string]interface{}{}
	}
	target["regular_response_coverage"] = source["regular_response_coverage"]
	target["response_accuracy"] = source["response_accuracy"]
	target["response_completeness"] = source["response_completeness"]
	target["response_compliance"] = source["response_compliance"]
	actions := valueOrStringSlice(source["priority_actions"])
	if len(actions) == 0 {
		actions = derivePriorityActions(target)
	}
	target["priority_actions"] = actions
	fillMissingDailyQuality(target)
}

func fillMissingDailyQuality(target map[string]interface{}) {
	if target == nil {
		return
	}
	total := asFloat(target["total_estimated_interactions"])
	appointments := asFloat(target["estimated_appointment_count"])
	walkins := asFloat(target["estimated_walkin_count"])
	risks := asFloat(target["risk_event_count"])
	if target["regular_response_coverage"] == nil && total > 0 {
		target["regular_response_coverage"] = round2((appointments + walkins) / total * 100.0)
	}
	if target["response_accuracy"] == nil && total > 0 {
		target["response_accuracy"] = round2(appointments / total * 100.0)
	}
	if target["response_completeness"] == nil && total > 0 {
		target["response_completeness"] = round2(walkins / total * 100.0)
	}
	if target["response_compliance"] == nil {
		score := 100.0 - risks*5.0
		if score < 0 {
			score = 0
		}
		target["response_compliance"] = round2(score)
	}
}

func derivePriorityActions(target map[string]interface{}) []string {
	if target == nil {
		return []string{}
	}
	out := make([]string, 0, 3)
	if asFloat(target["risk_event_count"]) > 0 {
		out = append(out, "优先复盘高风险事件并明确责任人")
	}
	topQuestions := valueOrStringSlice(target["top_questions"])
	if len(topQuestions) > 0 {
		out = append(out, "更新高频问题应答知识并同步培训")
	}
	if asFloat(target["estimated_walkin_count"]) > 0 {
		out = append(out, "优化 walk-in 承接话术与转化流程")
	}
	if len(out) == 0 {
		out = append(out, "数据不足，建议先补齐录音样本")
	}
	return out
}

func asFloat(v interface{}) float64 {
	switch n := v.(type) {
	case int:
		return float64(n)
	case int64:
		return float64(n)
	case float64:
		return n
	case float32:
		return float64(n)
	default:
		return 0
	}
}

func round2(v float64) float64 {
	return float64(int(v*100+0.5)) / 100
}

// ListKnowledgeBases lists knowledge bases for a tenant
func (s *Store) ListKnowledgeBases(ctx context.Context, tenantID int64, kbType string) ([]map[string]interface{}, error) {
	query := `
		SELECT id, tenant_id, kb_type, content, version, status, updated_at, created_at
		FROM frontdesk_knowledge_bases
		WHERE tenant_id = $1 AND status = 'active'
	`
	args := []interface{}{tenantID}

	if kbType != "" {
		query += ` AND kb_type = $2`
		args = append(args, kbType)
	}

	query += ` ORDER BY kb_type, version DESC`

	rows, err := s.pool.Query(ctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("query knowledge bases: %w", err)
	}
	defer rows.Close()

	var bases []map[string]interface{}
	for rows.Next() {
		var id, tenantID int64
		var kbType, status string
		var updatedAt, createdAt time.Time
		var content map[string]interface{}
		var version int

		if err := rows.Scan(&id, &tenantID, &kbType, &content, &version, &status, &updatedAt, &createdAt); err != nil {
			return nil, fmt.Errorf("scan knowledge base: %w", err)
		}

		bases = append(bases, map[string]interface{}{
			"id":         id,
			"tenant_id":  tenantID,
			"kb_type":    kbType,
			"content":    content,
			"version":    version,
			"status":     status,
			"updated_at": updatedAt.Format("2006-01-02 15:04:05"),
			"created_at": createdAt.Format("2006-01-02 15:04:05"),
		})
	}

	return bases, nil
}

// GetKnowledgeBase gets a knowledge base by type
func (s *Store) GetKnowledgeBase(ctx context.Context, tenantID int64, kbType string) (map[string]interface{}, error) {
	query := `
		SELECT id, tenant_id, kb_type, content, version, status, updated_at, created_at
		FROM frontdesk_knowledge_bases
		WHERE tenant_id = $1 AND kb_type = $2 AND status = 'active'
		ORDER BY version DESC
		LIMIT 1
	`

	var id int64
	var content map[string]interface{}
	var version int
	var status string
	var updatedAt, createdAt time.Time

	if err := s.pool.QueryRow(ctx, query, tenantID, kbType).Scan(&id, &tenantID, &kbType, &content, &version, &status, &updatedAt, &createdAt); err != nil {
		if err == pgx.ErrNoRows {
			return nil, fmt.Errorf("knowledge base not found")
		}
		return nil, fmt.Errorf("query knowledge base: %w", err)
	}

	return map[string]interface{}{
		"id":         id,
		"tenant_id":  tenantID,
		"kb_type":    kbType,
		"content":    content,
		"version":    version,
		"status":     status,
		"updated_at": updatedAt.Format("2006-01-02 15:04:05"),
		"created_at": createdAt.Format("2006-01-02 15:04:05"),
	}, nil
}

// CreateKnowledgeBase creates a new knowledge base
func (s *Store) CreateKnowledgeBase(ctx context.Context, tenantID int64, kbType string, content map[string]interface{}) (map[string]interface{}, error) {
	query := `
		INSERT INTO frontdesk_knowledge_bases (tenant_id, kb_type, content, version, status, created_at, updated_at)
		VALUES ($1, $2, $3, COALESCE((SELECT MAX(version)+1 FROM frontdesk_knowledge_bases WHERE tenant_id=$1 AND kb_type=$2), 1), 'active', NOW(), NOW())
		RETURNING id, tenant_id, kb_type, content, version, status, updated_at, created_at
	`

	var id int64
	var version int
	var status string
	var updatedAt, createdAt time.Time

	if err := s.pool.QueryRow(ctx, query, tenantID, kbType, content).Scan(&id, &tenantID, &kbType, &content, &version, &status, &updatedAt, &createdAt); err != nil {
		return nil, fmt.Errorf("insert knowledge base: %w", err)
	}

	return map[string]interface{}{
		"id":         id,
		"tenant_id":  tenantID,
		"kb_type":    kbType,
		"content":    content,
		"version":    version,
		"status":     status,
		"updated_at": updatedAt.Format("2006-01-02 15:04:05"),
		"created_at": createdAt.Format("2006-01-02 15:04:05"),
	}, nil
}

// UpdateKnowledgeBase updates a knowledge base
func (s *Store) UpdateKnowledgeBase(ctx context.Context, tenantID int64, kbType string, content map[string]interface{}) (map[string]interface{}, error) {
	// Get current version
	var currentVersion int
	if err := s.pool.QueryRow(ctx, `
		SELECT COALESCE(MAX(version), 0)
		FROM frontdesk_knowledge_bases
		WHERE tenant_id = $1 AND kb_type = $2
	`, tenantID, kbType).Scan(&currentVersion); err != nil {
		return nil, fmt.Errorf("get current version: %w", err)
	}

	newVersion := currentVersion + 1

	query := `
		INSERT INTO frontdesk_knowledge_bases (tenant_id, kb_type, content, version, status, created_at, updated_at)
		VALUES ($1, $2, $3, $4, 'active', NOW(), NOW())
		RETURNING id, tenant_id, kb_type, content, version, status, updated_at, created_at
	`

	var id int64
	var version int
	var status string
	var updatedAt, createdAt time.Time

	if err := s.pool.QueryRow(ctx, query, tenantID, kbType, content, newVersion).Scan(&id, &tenantID, &kbType, &content, &version, &status, &updatedAt, &createdAt); err != nil {
		return nil, fmt.Errorf("insert knowledge base version: %w", err)
	}

	return map[string]interface{}{
		"id":         id,
		"tenant_id":  tenantID,
		"kb_type":    kbType,
		"content":    content,
		"version":    version,
		"status":     status,
		"updated_at": updatedAt.Format("2006-01-02 15:04:05"),
		"created_at": createdAt.Format("2006-01-02 15:04:05"),
	}, nil
}

// ListWeeklyReports lists weekly reports for a tenant
func (s *Store) ListWeeklyReports(ctx context.Context, tenantID int64, page, pageSize int) ([]map[string]interface{}, int64, error) {
	offset := (page - 1) * pageSize

	// Get total count
	var total int64
	if err := s.pool.QueryRow(ctx, `
		SELECT COUNT(*)
		FROM frontdesk_daily_reports
		WHERE tenant_id = $1
		  AND report_date >= DATE_TRUNC('week', CURRENT_DATE - INTERVAL '12 weeks')
	`, tenantID).Scan(&total); err != nil {
		return nil, 0, fmt.Errorf("count weekly reports: %w", err)
	}

	query := `
		SELECT id, tenant_id, report_date,
		       total_estimated_interactions, estimated_appointment_count, estimated_walkin_count, risk_event_count,
		       top_questions, competitor_mentions, doctor_inquiries, channel_feedback, testimonial_materials,
		       created_at, created_at
		FROM frontdesk_daily_reports
		WHERE tenant_id = $1
		  AND report_date >= DATE_TRUNC('week', CURRENT_DATE - INTERVAL '12 weeks')
		ORDER BY report_date DESC
		LIMIT $2 OFFSET $3
	`
	rows, err := s.pool.Query(ctx, query, tenantID, pageSize, offset)
	if err != nil {
		return nil, 0, fmt.Errorf("query weekly reports: %w", err)
	}
	defer rows.Close()

	var reports []map[string]interface{}
	for rows.Next() {
		var (
			id, totalInteractions, appointments, walkIns, riskEvents                                 int64
			reportDate                                                                               time.Time
			createdAt, updatedAt                                                                     time.Time
			topQuestions, competitorMentions, doctorInquiries, channelFeedback, testimonialMaterials []byte
		)
		if err := rows.Scan(
			&id, &tenantID, &reportDate,
			&totalInteractions, &appointments, &walkIns, &riskEvents,
			&topQuestions, &competitorMentions, &doctorInquiries, &channelFeedback, &testimonialMaterials,
			&createdAt, &updatedAt,
		); err != nil {
			return nil, 0, fmt.Errorf("scan weekly report: %w", err)
		}
		analysis := normalizeFrontdeskWeeklyAnalysis(parseJSONAsMap(testimonialMaterials), totalInteractions, appointments, walkIns, riskEvents)
		reports = append(reports, map[string]interface{}{
			"id":                id,
			"tenant_id":         tenantID,
			"report_date":       reportDate.Format("2006-01-02"),
			"analysis":          analysis,
			"status":            firstNonEmptyString(analysis["status"], "draft"),
			"manager_comment":   firstNonEmptyString(analysis["manager_comment"], ""),
			"published_at":      firstNonEmptyString(analysis["published_at"], ""),
			"published_by":      firstNonEmptyString(analysis["published_by"], ""),
			"published_by_name": firstNonEmptyString(analysis["published_by_name"], ""),
			"created_at":        createdAt.Format(time.RFC3339),
			"updated_at":        updatedAt.Format(time.RFC3339),
		})
	}

	return reports, total, nil
}

// GetWeeklyReport gets a weekly report by date
func (s *Store) GetWeeklyReport(ctx context.Context, tenantID int64, dateStr string) (map[string]interface{}, error) {
	query := `
		SELECT id, tenant_id, report_date,
		       total_estimated_interactions, estimated_appointment_count, estimated_walkin_count, risk_event_count,
		       top_questions, competitor_mentions, doctor_inquiries, channel_feedback, testimonial_materials,
		       created_at, created_at
		FROM frontdesk_daily_reports
		WHERE tenant_id = $1 AND report_date = $2
		LIMIT 1
	`
	var (
		id, totalInteractions, appointments, walkIns, riskEvents                                 int64
		reportDate                                                                               time.Time
		createdAt, updatedAt                                                                     time.Time
		topQuestions, competitorMentions, doctorInquiries, channelFeedback, testimonialMaterials []byte
	)
	if err := s.pool.QueryRow(ctx, query, tenantID, dateStr).Scan(
		&id, &tenantID, &reportDate,
		&totalInteractions, &appointments, &walkIns, &riskEvents,
		&topQuestions, &competitorMentions, &doctorInquiries, &channelFeedback, &testimonialMaterials,
		&createdAt, &updatedAt,
	); err != nil {
		if err == pgx.ErrNoRows {
			return nil, fmt.Errorf("weekly report not found")
		}
		return nil, fmt.Errorf("query weekly report: %w", err)
	}
	analysis := normalizeFrontdeskWeeklyAnalysis(parseJSONAsMap(testimonialMaterials), totalInteractions, appointments, walkIns, riskEvents)
	status, _ := analysis["status"].(string)
	if status == "" {
		status = "draft"
	}

	return map[string]interface{}{
		"id":                id,
		"tenant_id":         tenantID,
		"report_date":       reportDate.Format("2006-01-02"),
		"analysis":          analysis,
		"status":            status,
		"manager_comment":   "",
		"published_at":      "",
		"published_by":      "",
		"published_by_name": "",
		"created_at":        createdAt.Format(time.RFC3339),
		"updated_at":        updatedAt.Format(time.RFC3339),
	}, nil
}

func parseJSONAsStringSlice(raw []byte) []string {
	if len(raw) == 0 {
		return []string{}
	}
	var arr []interface{}
	if err := json.Unmarshal(raw, &arr); err != nil {
		return []string{}
	}
	out := make([]string, 0, len(arr))
	for _, item := range arr {
		text := strings.TrimSpace(fmt.Sprintf("%v", item))
		if text != "" && text != "<nil>" {
			out = append(out, text)
		}
	}
	return out
}

func parseJSONAsMap(raw []byte) map[string]interface{} {
	if len(raw) == 0 {
		return map[string]interface{}{}
	}
	var obj map[string]interface{}
	if err := json.Unmarshal(raw, &obj); err != nil {
		return map[string]interface{}{}
	}
	return obj
}

func normalizeFrontdeskShiftAnalysisJSON(input map[string]interface{}) map[string]interface{} {
	if input == nil {
		input = map[string]interface{}{}
	}
	out := map[string]interface{}{
		"summary":          firstNonEmptyString(input["summary"], ""),
		"highlights":       valueOrStringSlice(input["highlights"]),
		"next_actions":     valueOrStringSlice(input["next_actions"]),
		"scenario":         firstNonEmptyString(input["scenario"], "consult_only"),
		"key_insights":     normalizeFrontdeskInsightItems(input["key_insights"]),
		"qa_quality_notes": valueOrStringSlice(input["qa_quality_notes"]),
		"loss_signals":     valueOrStringSlice(input["loss_signals"]),
		"risk_points":      valueOrStringSlice(input["risk_points"]),
		"scores":           normalizeFrontdeskScoreItems(input["scores"]),
	}
	if v, ok := input["source_recording_id"]; ok {
		out["source_recording_id"] = v
	}
	return out
}

func normalizeFrontdeskInsightItems(v interface{}) []map[string]interface{} {
	switch arr := v.(type) {
	case []interface{}:
		out := make([]map[string]interface{}, 0, len(arr))
		for _, raw := range arr {
			obj, ok := raw.(map[string]interface{})
			if !ok {
				continue
			}
			item := map[string]interface{}{
				"fact":     firstNonEmptyString(obj["fact"], ""),
				"evidence": firstNonEmptyString(obj["evidence"], ""),
				"meaning":  firstNonEmptyString(obj["meaning"], ""),
			}
			out = append(out, item)
		}
		return out
	default:
		return []map[string]interface{}{}
	}
}

func normalizeFrontdeskScoreItems(v interface{}) []map[string]interface{} {
	switch arr := v.(type) {
	case []interface{}:
		out := make([]map[string]interface{}, 0, len(arr))
		for _, raw := range arr {
			obj, ok := raw.(map[string]interface{})
			if !ok {
				continue
			}
			item := map[string]interface{}{
				"name":   firstNonEmptyString(obj["name"], ""),
				"score":  obj["score"],
				"reason": firstNonEmptyString(obj["reason"], ""),
			}
			out = append(out, item)
		}
		return out
	default:
		return []map[string]interface{}{}
	}
}

func normalizeFrontdeskWeeklyAnalysis(input map[string]interface{}, totalInteractions, appointments, walkIns, riskEvents int64) map[string]interface{} {
	if input == nil {
		input = map[string]interface{}{}
	}
	return map[string]interface{}{
		"summary":                 firstNonEmptyString(input["summary"], ""),
		"key_changes":             valueOrStringSlice(input["key_changes"]),
		"next_actions":            valueOrStringSlice(input["next_actions"]),
		"total_appointments":      appointments,
		"total_walk_ins":          walkIns,
		"total_interactions":      totalInteractions,
		"question_structure":      valueOrStringSlice(input["question_structure"]),
		"response_quality":        valueOrStringSlice(input["response_quality"]),
		"key_insights":            valueOrStringSlice(input["key_insights"]),
		"risk_event_count":        riskEvents,
		"issue_summary":           valueOrStringSlice(input["issue_summary"]),
		"staff_highlights":        valueOrStringSlice(input["staff_highlights"]),
		"staff_variances":         valueOrStringSlice(input["staff_variances"]),
		"staff_suggestions":       valueOrStringSlice(input["staff_suggestions"]),
		"improvement_suggestions": valueOrStringSlice(input["improvement_suggestions"]),
		"action_owners":           valueOrStringSlice(input["action_owners"]),
		"action_effects":          valueOrStringSlice(input["action_effects"]),
		"evidence_count":          asInt64(input["evidence_count"]),
		"evidence_items":          normalizeWeeklyEvidenceItems(input["evidence_items"]),
		"status":                  firstNonEmptyString(input["status"], "draft"),
		"manager_comment":         firstNonEmptyString(input["manager_comment"], ""),
		"published_at":            firstNonEmptyString(input["published_at"], ""),
		"published_by":            firstNonEmptyString(input["published_by"], ""),
		"published_by_name":       firstNonEmptyString(input["published_by_name"], ""),
	}
}

func normalizeWeeklyEvidenceItems(v interface{}) []map[string]interface{} {
	switch arr := v.(type) {
	case []interface{}:
		out := make([]map[string]interface{}, 0, len(arr))
		for _, raw := range arr {
			obj, ok := raw.(map[string]interface{})
			if !ok {
				continue
			}
			out = append(out, map[string]interface{}{
				"recording_id": asInt64(obj["recording_id"]),
				"shift_date":   firstNonEmptyString(obj["shift_date"], ""),
				"summary":      firstNonEmptyString(obj["summary"], ""),
			})
		}
		return out
	default:
		return []map[string]interface{}{}
	}
}

func valueOrStringSlice(v interface{}) []string {
	list, ok := v.([]string)
	if ok {
		return list
	}
	switch arr := v.(type) {
	case []interface{}:
		out := make([]string, 0, len(arr))
		for _, item := range arr {
			text := strings.TrimSpace(fmt.Sprintf("%v", item))
			if text != "" && text != "<nil>" {
				out = append(out, text)
			}
		}
		return out
	default:
		return []string{}
	}
}

func asInt64(v interface{}) int64 {
	switch n := v.(type) {
	case int64:
		return n
	case int:
		return int64(n)
	case float64:
		return int64(n)
	case float32:
		return int64(n)
	case json.Number:
		if parsed, err := n.Int64(); err == nil {
			return parsed
		}
	}
	return 0
}

func firstNonEmptyString(v interface{}, fallback string) string {
	text := strings.TrimSpace(fmt.Sprintf("%v", v))
	if text == "" || text == "<nil>" {
		return fallback
	}
	return text
}
