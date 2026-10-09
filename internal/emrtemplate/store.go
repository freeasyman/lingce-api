package emrtemplate

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

type Store struct{ pool *pgxpool.Pool }

func NewStore(pool *pgxpool.Pool) *Store { return &Store{pool: pool} }

type builtinSection struct {
	Code, Name, Category, InputType string
	Common, Visible                 bool
	Order                           int
}

type builtinBinding struct {
	Code, ExecutionMode, DeadlineAction string
	Order                               int
}

var requiredCommonSections = []struct {
	Code string
	Name string
}{
	{"chief_complaint", "主诉"},
	{"present_illness", "现病史"},
	{"past_history", "既往史"},
	{"personal_history", "个人史"},
	{"family_history", "家族史"},
	{"allergy_history", "过敏史"},
	{"physical_exam", "体格检查"},
	{"auxiliary_exam", "辅助检查"},
	{"diagnosis", "诊断"},
	{"prescription", "处方"},
	{"disposition", "处置"},
	{"medical_advice", "医嘱"},
	{"followup", "复诊安排"},
	{"supplement", "补充说明"},
}

var builtinCommonSections = []builtinSection{
	{"chief_complaint", "主诉", "病史", "长文本", true, true, 10},
	{"present_illness", "现病史", "病史", "长文本", true, true, 20},
	{"past_history", "既往史", "病史", "长文本", true, true, 30},
	{"personal_history", "个人史", "病史", "长文本", true, true, 40},
	{"family_history", "家族史", "病史", "长文本", true, true, 50},
	{"allergy_history", "过敏史", "病史", "长文本", true, true, 60},
	{"physical_exam", "体格检查", "诊疗依据", "结构化内容", true, true, 70},
	{"auxiliary_exam", "辅助检查", "诊疗依据", "长文本", true, true, 80},
	{"diagnosis", "西医诊断", "医疗判断", "结构化内容", true, true, 90},
	{"prescription", "处方", "医疗措施", "长文本", true, true, 100},
	{"disposition", "处置", "医疗措施", "长文本", true, true, 110},
	{"medical_advice", "医嘱", "医疗措施", "长文本", true, true, 120},
	{"followup", "复诊安排", "医疗措施", "长文本", true, true, 130},
	{"supplement", "补充说明", "医疗措施", "长文本", true, true, 140},
}

func (s *Store) EnsureBuiltin(ctx context.Context) error {
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return fmt.Errorf("begin builtin emr template: %w", err)
	}
	defer tx.Rollback(ctx)

	for _, section := range builtinCommonSections {
		var fieldID string
		err := tx.QueryRow(ctx, `
			SELECT id
			FROM emr_fields
			WHERE tenant_id=0 AND code=$1
			FOR UPDATE
		`, section.Code).Scan(&fieldID)
		if err == pgx.ErrNoRows {
			err = tx.QueryRow(ctx, `
				INSERT INTO emr_fields
				(tenant_id, code, name, content_category, input_type, description, structure_json, status, created_by)
				VALUES (0, $1, $2, $3, $4, $5, $6::jsonb, 'enabled', 0)
				RETURNING id
			`, section.Code, section.Name, section.Category, section.InputType, section.Name, mustJSON(builtinStructure(section.Code))).Scan(&fieldID)
		}
		if err != nil {
			return fmt.Errorf("ensure builtin emr field %s: %w", section.Code, err)
		}
		structureJSON := mustJSON(builtinStructure(section.Code))
		if _, err := tx.Exec(ctx, `
			UPDATE emr_fields
			SET name=$2, content_category=$3, input_type=$4, structure_json=$6::jsonb,
			    description=CASE
			        WHEN BTRIM(COALESCE(description, '')) = '' THEN $5
			        WHEN BTRIM(description) = $5 THEN $5
			        ELSE description
			    END,
			    status='enabled', updated_at=NOW()
			WHERE id=$1
		`, fieldID, section.Name, section.Category, section.InputType, section.Name, structureJSON); err != nil {
			return fmt.Errorf("refresh builtin emr field %s: %w", section.Code, err)
		}
	}

	var templateID string
	err = tx.QueryRow(ctx, `
		SELECT id
		FROM emr_templates
		WHERE tenant_id=0 AND code='standard_outpatient' AND version_no='1.0'
		FOR UPDATE
	`).Scan(&templateID)
	if err == pgx.ErrNoRows {
		err = tx.QueryRow(ctx, `
			INSERT INTO emr_templates
				(tenant_id, code, name, version_no, document_type, visit_type, status,
				 print_title, description, created_by, published_at, published_by, is_current)
			VALUES (0, 'standard_outpatient', '标准门急诊病历', '1.0', '门诊病历',
				'通用', 'published', '门诊病历', '平台提供的基础门急诊病历模板', 0, NOW(), 0, TRUE)
			RETURNING id
		`).Scan(&templateID)
	}
	if err != nil {
		return fmt.Errorf("ensure builtin emr template: %w", err)
	}
	if _, err := tx.Exec(ctx, `
		UPDATE emr_templates
		SET name='标准门急诊病历', document_type='门诊病历', visit_type='通用',
		    status='published', print_title='门诊病历',
		    description='平台提供的基础门急诊病历模板',
		    published_at=COALESCE(published_at,NOW()),
		    published_by=COALESCE(published_by,0),
		    updated_at=NOW()
		WHERE id=$1
	`, templateID); err != nil {
		return fmt.Errorf("refresh builtin emr template: %w", err)
	}
	var hasCurrent bool
	if err := tx.QueryRow(ctx, `
		SELECT EXISTS(
			SELECT 1
			FROM emr_templates
			WHERE tenant_id=0 AND code='standard_outpatient' AND is_current=true
		)
	`).Scan(&hasCurrent); err != nil {
		return fmt.Errorf("check builtin current emr template: %w", err)
	}
	if !hasCurrent {
		if _, err := tx.Exec(ctx, `
			UPDATE emr_templates
			SET is_current=true, updated_at=NOW()
			WHERE id=$1 AND status='published'
		`, templateID); err != nil {
			return fmt.Errorf("set builtin current emr template: %w", err)
		}
	}

	for _, section := range builtinCommonSections {
		var fieldID string
		if err := tx.QueryRow(ctx, `
			SELECT id
			FROM emr_fields
			WHERE tenant_id=0 AND code=$1 AND status='enabled'
		`, section.Code).Scan(&fieldID); err != nil {
			return fmt.Errorf("load builtin template field %s: %w", section.Code, err)
		}
		var bindingID string
		err := tx.QueryRow(ctx, `
			SELECT id
			FROM emr_template_fields
			WHERE tenant_id=0 AND template_id=$1 AND field_id=$2
			FOR UPDATE
		`, templateID, fieldID).Scan(&bindingID)
		if err == pgx.ErrNoRows {
			err = tx.QueryRow(ctx, `
				INSERT INTO emr_template_fields
					(tenant_id, template_id, field_id, display_order, is_visible, is_required, description, created_by)
				VALUES (0,$1,$2,$3,$4,FALSE,$5,0)
				RETURNING id
			`, templateID, fieldID, section.Order, section.Visible, section.Name).Scan(&bindingID)
		}
		if err != nil {
			return fmt.Errorf("ensure builtin template field %s: %w", section.Code, err)
		}
		if _, err := tx.Exec(ctx, `
			UPDATE emr_template_fields
			SET display_order=$2, is_visible=$3, description=$4
			WHERE id=$1
		`, bindingID, section.Order, section.Visible, section.Name); err != nil {
			return fmt.Errorf("refresh builtin template field %s: %w", section.Code, err)
		}
	}

	if err := tx.Commit(ctx); err != nil {
		return fmt.Errorf("commit builtin emr template: %w", err)
	}
	return nil
}

func builtinStructure(code string) map[string]any {
	switch code {
	case "diagnosis":
		return map[string]any{"items": []any{"diagnosis_name", "icd10_code", "basis"}}
	case "tcm_diagnosis":
		return map[string]any{"items": []any{"diagnosis_name", "pattern_name", "basis"}}
	case "physical_exam":
		return map[string]any{"vitals": []any{"temperature", "blood_pressure", "pulse", "respiration", "blood_glucose"}, "metrics": []any{"height", "weight", "bmi"}, "text_items": []any{"general_exam", "positive_findings", "necessary_negative_findings", "specialty_exam", "supplement"}}
	default:
		return map[string]any{}
	}
}

func mustJSON(value any) string {
	if value == nil {
		return "{}"
	}
	raw, err := json.Marshal(value)
	if err != nil || string(raw) == "null" {
		return "{}"
	}
	return string(raw)
}

func (s *Store) List(ctx context.Context, tenantID int64) ([]*Template, error) {
	rows, err := s.pool.Query(ctx, `
		SELECT id, tenant_id, code, name, version_no, document_type, visit_type,
		       department_id, COALESCE(specialty, ''), status, is_current,
		       created_by, created_at, updated_at
		FROM emr_templates
		WHERE tenant_id IN (0, $1)
		ORDER BY code, version_no
	`, tenantID)
	if err != nil {
		return nil, fmt.Errorf("list emr templates: %w", err)
	}
	defer rows.Close()
	items := make([]*Template, 0)
	for rows.Next() {
		item := &Template{}
		if err := rows.Scan(&item.ID, &item.TenantID, &item.Code, &item.Name, &item.VersionNo,
			&item.DocumentType, &item.VisitType, &item.DepartmentID, &item.Specialty,
			&item.Status, &item.IsCurrent, &item.CreatedBy,
			&item.CreatedAt, &item.UpdatedAt); err != nil {
			return nil, err
		}
		items = append(items, item)
	}
	return items, rows.Err()
}

func (s *Store) Get(ctx context.Context, tenantID int64, id string) (*TemplateDetail, error) {
	version, err := s.GetVersion(ctx, tenantID, id)
	if err != nil || version == nil {
		return nil, err
	}
	template := templateFromVersion(version)
	sections, err := s.listSections(ctx, id)
	if err != nil {
		return nil, err
	}
	versions, err := s.ListVersions(ctx, tenantID, id)
	if err != nil {
		return nil, err
	}
	return &TemplateDetail{Template: template, Versions: versions, Sections: sections}, nil
}

func (s *Store) ListVersions(ctx context.Context, tenantID int64, templateID string) ([]*TemplateVersion, error) {
	base, err := s.GetVersion(ctx, tenantID, templateID)
	if err != nil || base == nil {
		return nil, err
	}
	rows, err := s.pool.Query(ctx, `
		SELECT id, id, code, version_no, name, document_type, visit_type, department_id,
		       specialty, print_title, description, status, created_by,
		       published_at, published_by, disabled_at, disabled_by, created_at, is_current
		FROM emr_templates
		WHERE tenant_id IN (0, $1) AND code=$2
		ORDER BY created_at DESC, version_no DESC
	`, tenantID, base.TemplateCode)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := make([]*TemplateVersion, 0)
	for rows.Next() {
		v := &TemplateVersion{}
		if err := scanTemplateVersion(rows, v); err != nil {
			return nil, err
		}
		out = append(out, v)
	}
	return out, rows.Err()
}

func (s *Store) GetVersion(ctx context.Context, tenantID int64, versionID string) (*TemplateVersion, error) {
	v := &TemplateVersion{}
	err := s.pool.QueryRow(ctx, `
		SELECT id, id, code, version_no, name, document_type, visit_type, department_id,
		       specialty, print_title, description, status, created_by,
		       published_at, published_by, disabled_at, disabled_by, created_at, is_current
		FROM emr_templates
		WHERE id=$1 AND tenant_id IN (0, $2)
	`, strings.TrimSpace(versionID), tenantID).Scan(&v.ID, &v.TemplateID, &v.TemplateCode, &v.VersionNo, &v.Name, &v.DocumentType, &v.VisitType, &v.DepartmentID, &v.SpecialtyModule, &v.PrintTitle, &v.Description, &v.Status, &v.CreatedBy, &v.PublishedAt, &v.PublishedBy, &v.DisabledAt, &v.DisabledBy, &v.CreatedAt, &v.IsCurrent)
	if err == pgx.ErrNoRows {
		return nil, nil
	}
	return v, err
}

func scanTemplateVersion(row pgx.Row, v *TemplateVersion) error {
	return row.Scan(&v.ID, &v.TemplateID, &v.TemplateCode, &v.VersionNo, &v.Name, &v.DocumentType, &v.VisitType, &v.DepartmentID, &v.SpecialtyModule, &v.PrintTitle, &v.Description, &v.Status, &v.CreatedBy, &v.PublishedAt, &v.PublishedBy, &v.DisabledAt, &v.DisabledBy, &v.CreatedAt, &v.IsCurrent)
}

func templateFromVersion(v *TemplateVersion) *Template {
	return &Template{ID: v.ID, TenantID: nil, Code: v.TemplateCode, Name: v.Name, VersionNo: v.VersionNo, DocumentType: v.DocumentType, VisitType: v.VisitType, DepartmentID: v.DepartmentID, Specialty: derefString(v.SpecialtyModule), IsCurrent: v.IsCurrent, Status: v.Status, CreatedBy: v.CreatedBy, CreatedAt: v.CreatedAt}
}

func derefString(value *string) string {
	if value == nil {
		return ""
	}
	return *value
}

func (s *Store) GetPublishedVersionDetail(ctx context.Context, tenantID int64, versionID string) (*PublishedVersionDetail, error) {
	version, err := s.GetVersion(ctx, tenantID, versionID)
	if err != nil || version == nil {
		return nil, err
	}
	if version.Status != "published" {
		return nil, fmt.Errorf("template version is not published")
	}
	sections, err := s.listSections(ctx, version.ID)
	if err != nil {
		return nil, err
	}
	return &PublishedVersionDetail{Version: version, Sections: sections}, nil
}

func (s *Store) GetCurrentPublishedByCode(ctx context.Context, tenantID int64, code string) (*PublishedVersionDetail, error) {
	version := &TemplateVersion{}
	err := s.pool.QueryRow(ctx, `
		SELECT id, id, code, version_no, name, document_type, visit_type, department_id,
		       specialty, print_title, description, status, created_by,
		       published_at, published_by, disabled_at, disabled_by, created_at, is_current
		FROM emr_templates
		WHERE tenant_id IN ($1, 0) AND code=$2 AND status='published' AND is_current=true
		ORDER BY CASE WHEN tenant_id=$1 THEN 0 ELSE 1 END
		LIMIT 1
	`, tenantID, strings.TrimSpace(code)).Scan(&version.ID, &version.TemplateID, &version.TemplateCode, &version.VersionNo, &version.Name, &version.DocumentType, &version.VisitType, &version.DepartmentID, &version.SpecialtyModule, &version.PrintTitle, &version.Description, &version.Status, &version.CreatedBy, &version.PublishedAt, &version.PublishedBy, &version.DisabledAt, &version.DisabledBy, &version.CreatedAt, &version.IsCurrent)
	if err == pgx.ErrNoRows {
		return nil, fmt.Errorf("没有可用的当前已发布病历模板：%s", strings.TrimSpace(code))
	}
	if err != nil {
		return nil, fmt.Errorf("load current emr template: %w", err)
	}
	return s.GetPublishedVersionDetail(ctx, tenantID, version.ID)
}

func (s *Store) CreateTemplate(ctx context.Context, tenantID, actorID int64, req SaveTemplateRequest) (*TemplateDetail, error) {
	var id string
	err := s.pool.QueryRow(ctx, `
		INSERT INTO emr_templates
			(tenant_id, code, name, version_no, document_type, visit_type,
			 status, print_title, created_by)
		VALUES ($1,$2,$3,'1.0','门诊病历','通用','draft',$3,$4)
		RETURNING id
	`, tenantID, req.Code, req.Name, actorID).Scan(&id)
	if err != nil {
		return nil, err
	}
	return s.Get(ctx, tenantID, id)
}

func (s *Store) CreateVersion(ctx context.Context, tenantID, actorID int64, templateID string, req SaveVersionRequest) (*TemplateVersion, error) {
	var id string
	err := s.pool.QueryRow(ctx, `
		INSERT INTO emr_templates
		(tenant_id, code, name, version_no, document_type, visit_type, department_id,
		 specialty, status, print_title, description, created_by)
		SELECT $1, code, $3, $4, $5, $6, $7, $8, 'draft', $9, $10, $11
		FROM emr_templates
		WHERE id=$2 AND tenant_id IN (0, $1)
		RETURNING id
	`, tenantID, strings.TrimSpace(templateID), req.Name, req.VersionNo, req.DocumentType, req.VisitType, req.DepartmentID, req.SpecialtyModule, req.PrintTitle, req.Description, actorID).Scan(&id)
	if err != nil {
		return nil, err
	}
	return s.GetVersion(ctx, tenantID, id)
}

func (s *Store) UpdateVersion(ctx context.Context, tenantID int64, versionID string, req SaveVersionRequest) (*TemplateVersion, error) {
	if err := s.requireDraftVersion(ctx, tenantID, versionID); err != nil {
		return nil, err
	}
	_, err := s.pool.Exec(ctx, `
		UPDATE emr_templates
		SET version_no=$2, name=$3, document_type=$4, visit_type=$5, department_id=$6,
		    specialty=$7, print_title=$8, description=$9, updated_at=NOW()
		WHERE id=$1 AND tenant_id IN (0, $10)
	`, versionID, req.VersionNo, req.Name, req.DocumentType, req.VisitType, req.DepartmentID, req.SpecialtyModule, req.PrintTitle, req.Description, tenantID)
	if err != nil {
		return nil, err
	}
	return s.GetVersion(ctx, tenantID, versionID)
}

func (s *Store) SaveSections(ctx context.Context, tenantID int64, versionID string, items []SectionInput) ([]*Section, error) {
	if err := s.requireDraftVersion(ctx, tenantID, versionID); err != nil {
		return nil, err
	}
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback(ctx)
	if _, err = tx.Exec(ctx, `DELETE FROM emr_template_fields WHERE template_id=$1`, versionID); err != nil {
		return nil, err
	}
	var templateTenant int64
	if err := tx.QueryRow(ctx, `SELECT tenant_id FROM emr_templates WHERE id=$1`, versionID).Scan(&templateTenant); err != nil {
		return nil, err
	}
	for _, item := range items {
		command, execErr := tx.Exec(ctx, `
			INSERT INTO emr_template_fields
			(tenant_id, template_id, field_id, display_order, is_visible, is_required, description, created_by)
			SELECT $2, $1, id, $4, $5, $6, $7, 0
			FROM emr_fields
			WHERE code=$3 AND tenant_id IN ($2, 0) AND status='enabled'
			ORDER BY CASE WHEN tenant_id=$2 THEN 0 ELSE 1 END
			LIMIT 1
		`, versionID, templateTenant, item.Code, item.DisplayOrder, item.IsVisible, item.IsRequired, item.Description)
		if execErr != nil {
			return nil, execErr
		}
		if command.RowsAffected() != 1 {
			return nil, fmt.Errorf("模板栏目不存在或已停用：%s", item.Code)
		}
	}
	if err = tx.Commit(ctx); err != nil {
		return nil, err
	}
	return s.listSections(ctx, versionID)
}

func (s *Store) requireDraftVersion(ctx context.Context, tenantID int64, versionID string) error {
	var status string
	err := s.pool.QueryRow(ctx, `SELECT status FROM emr_templates WHERE id=$1 AND tenant_id IN (0, $2)`, versionID, tenantID).Scan(&status)
	if err == pgx.ErrNoRows {
		return fmt.Errorf("template version not found")
	}
	if err != nil {
		return err
	}
	if status != "draft" {
		return fmt.Errorf("template version is not editable")
	}
	return nil
}

func (s *Store) Publish(ctx context.Context, tenantID int64, versionID string, actorID int64) error {
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return fmt.Errorf("begin publish template version: %w", err)
	}
	defer tx.Rollback(ctx)
	var status, code string
	var templateTenant int64
	err = tx.QueryRow(ctx, `SELECT status, code, tenant_id FROM emr_templates WHERE id=$1 AND tenant_id IN (0, $2) FOR UPDATE`, versionID, tenantID).Scan(&status, &code, &templateTenant)
	if err == pgx.ErrNoRows {
		return fmt.Errorf("template version not found")
	}
	if err != nil {
		return fmt.Errorf("load template version for publish: %w", err)
	}
	if status != "draft" {
		return fmt.Errorf("template version is not publishable")
	}
	if err := validatePublishableVersion(ctx, tx, versionID); err != nil {
		return err
	}
	if _, err := tx.Exec(ctx, `
		SELECT id
		FROM emr_templates
		WHERE tenant_id=$1 AND code=$2
		FOR UPDATE
	`, templateTenant, code); err != nil {
		return fmt.Errorf("lock template versions for publish: %w", err)
	}
	if _, err := tx.Exec(ctx, `UPDATE emr_templates SET is_current=false, updated_at=NOW() WHERE tenant_id=$1 AND code=$2`, templateTenant, code); err != nil {
		return err
	}
	cmd, err := tx.Exec(ctx, `UPDATE emr_templates SET status='published', is_current=true, published_at=COALESCE(published_at,NOW()), published_by=COALESCE(published_by,$2), updated_at=NOW() WHERE id=$1`, versionID, actorID)
	if err != nil {
		return err
	}
	if cmd.RowsAffected() == 0 {
		return fmt.Errorf("template version not found")
	}
	if err := tx.Commit(ctx); err != nil {
		return fmt.Errorf("commit template version publish: %w", err)
	}
	return nil
}

func (s *Store) SetCurrent(ctx context.Context, tenantID int64, versionID string, actorID int64) error {
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return fmt.Errorf("begin set current template version: %w", err)
	}
	defer tx.Rollback(ctx)

	var code string
	var templateTenant int64
	var status string
	if err := tx.QueryRow(ctx, `
		SELECT code, tenant_id, status
		FROM emr_templates
		WHERE id=$1 AND tenant_id IN (0,$2)
		FOR UPDATE
	`, versionID, tenantID).Scan(&code, &templateTenant, &status); err != nil {
		if err == pgx.ErrNoRows {
			return fmt.Errorf("template version not found")
		}
		return fmt.Errorf("load template version for current selection: %w", err)
	}
	if status != "published" {
		return fmt.Errorf("只有已发布模板版本可以设为当前版本")
	}
	if err := validatePublishableVersion(ctx, tx, versionID); err != nil {
		return err
	}
	if _, err := tx.Exec(ctx, `
		SELECT id
		FROM emr_templates
		WHERE tenant_id=$1 AND code=$2
		FOR UPDATE
	`, templateTenant, code); err != nil {
		return fmt.Errorf("lock template versions for current selection: %w", err)
	}
	if _, err := tx.Exec(ctx, `
		UPDATE emr_templates
		SET is_current=false, updated_at=NOW()
		WHERE tenant_id=$1 AND code=$2
	`, templateTenant, code, actorID); err != nil {
		return fmt.Errorf("clear current template version: %w", err)
	}
	if _, err := tx.Exec(ctx, `
		UPDATE emr_templates
		SET is_current=true, updated_at=NOW()
		WHERE id=$1
	`, versionID, actorID); err != nil {
		return fmt.Errorf("set current template version: %w", err)
	}
	if err := tx.Commit(ctx); err != nil {
		return fmt.Errorf("commit current template version: %w", err)
	}
	return nil
}

func validatePublishableVersion(ctx context.Context, tx pgx.Tx, versionID string) error {
	rows, err := tx.Query(ctx, `
		SELECT f.code, f.name, tf.is_visible, tf.is_required, f.content_category, f.input_type
		FROM emr_template_fields tf
		JOIN emr_fields f ON f.id=tf.field_id
		WHERE tf.template_id=$1
	`, versionID)
	if err != nil {
		return fmt.Errorf("load template fields for publish: %w", err)
	}
	defer rows.Close()
	fields := make(map[string]struct {
		name      string
		visible   bool
		required  bool
		category  string
		inputType string
	}, len(requiredCommonSections))
	for rows.Next() {
		var code string
		var field struct {
			name      string
			visible   bool
			required  bool
			category  string
			inputType string
		}
		if err := rows.Scan(&code, &field.name, &field.visible, &field.required, &field.category, &field.inputType); err != nil {
			return fmt.Errorf("read template fields for publish: %w", err)
		}
		fields[code] = field
	}
	if err := rows.Err(); err != nil {
		return fmt.Errorf("read template fields for publish: %w", err)
	}
	rows.Close()
	missing := make([]string, 0)
	invalid := make([]string, 0)
	for _, required := range requiredCommonSections {
		field, ok := fields[required.Code]
		if !ok {
			missing = append(missing, required.Name)
			continue
		}
		if !field.visible {
			invalid = append(invalid, required.Name)
		}
	}
	if len(missing) > 0 {
		return fmt.Errorf("模板发布失败：缺少公共栏目：%s", strings.Join(missing, "、"))
	}
	if len(invalid) > 0 {
		return fmt.Errorf("模板发布失败：公共栏目必须可见：%s", strings.Join(invalid, "、"))
	}
	var templateCode string
	if err := tx.QueryRow(ctx, `SELECT code FROM emr_templates WHERE id=$1`, versionID).Scan(&templateCode); err != nil {
		return err
	}
	{
		if field, ok := fields["diagnosis"]; !ok || !field.visible || !field.required {
			return fmt.Errorf("模板发布失败：西医诊断必须可见且必填")
		}
		if templateCode == "tcm" {
			for _, code := range []string{"diagnosis", "tcm_diagnosis"} {
				field, ok := fields[code]
				if !ok || !field.visible || !field.required {
					return fmt.Errorf("模板发布失败：中医模板的%s必须可见且必填", code)
				}
			}
		} else if _, ok := fields["tcm_diagnosis"]; ok {
			return fmt.Errorf("模板发布失败：非中医模板不能包含中医诊断")
		}
	}

	return nil
}

func (s *Store) Disable(ctx context.Context, tenantID int64, versionID string, actorID int64) error {
	cmd, err := s.pool.Exec(ctx, `
		UPDATE emr_templates
		SET status='disabled', is_current=false, disabled_at=NOW(), disabled_by=$3, updated_at=NOW()
		WHERE id=$1 AND tenant_id IN (0, $2) AND status<>'disabled'
	`, versionID, tenantID, actorID)
	if err != nil {
		return err
	}
	if cmd.RowsAffected() == 0 {
		return fmt.Errorf("template version not found")
	}
	return nil
}

func (s *Store) listSections(ctx context.Context, versionID string) ([]*Section, error) {
	rows, err := s.pool.Query(ctx, `
		SELECT tf.id, tf.template_id, f.code, f.name, f.content_category, f.input_type,
		       TRUE, tf.is_visible, tf.is_required, tf.display_order, COALESCE(NULLIF(tf.description,''), f.description),
		       f.options_json, f.structure_json
		FROM emr_template_fields tf
		JOIN emr_fields f ON f.id=tf.field_id
		WHERE tf.template_id=$1
		ORDER BY tf.display_order
	`, versionID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := make([]*Section, 0)
	for rows.Next() {
		x := &Section{}
		var o, st []byte
		if err := rows.Scan(&x.ID, &x.TemplateVersionID, &x.Code, &x.Name, &x.ContentCategory, &x.InputType, &x.IsCommon, &x.IsVisible, &x.IsRequired, &x.DisplayOrder, &x.Description, &o, &st); err != nil {
			return nil, err
		}
		x.Options = map[string]any{}
		x.Structure = map[string]any{}
		_ = json.Unmarshal(o, &x.Options)
		_ = json.Unmarshal(st, &x.Structure)
		out = append(out, x)
	}
	return out, rows.Err()
}
