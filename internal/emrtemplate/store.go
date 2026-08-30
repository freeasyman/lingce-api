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
	{"physical_exam", "体格检查", "诊疗依据", "长文本", true, true, 70},
	{"auxiliary_exam", "辅助检查", "诊疗依据", "结构化内容", true, true, 80},
	{"diagnosis", "诊断", "医疗判断", "长文本", true, true, 90},
	{"prescription", "处方", "医疗措施", "结构化内容", true, true, 100},
	{"disposition", "处置", "医疗措施", "长文本", true, true, 110},
	{"medical_advice", "医嘱", "医疗措施", "长文本", true, true, 120},
	{"followup", "复诊安排", "医疗措施", "长文本", true, true, 130},
	{"supplement", "补充说明", "医疗措施", "长文本", true, true, 140},
}

var builtinQualityBindings = []builtinBinding{
	{"emr.chief_complaint.required", "程序判断", "归档前处理", 10},
	{"emr.chief_complaint.max_length", "程序判断", "归档前处理", 20},
	{"emr.allergy.required.status", "程序判断", "归档前处理", 30},
	{"emr.allergy.required.drug_detail", "程序判断", "归档前处理", 40},
	{"emr.allergy.required.other_detail", "程序判断", "归档前处理", 50},
	{"emr.allergy.required.unconfirmed", "程序判断", "归档前处理", 60},
	{"emr.diagnosis.present", "程序判断", "归档前处理", 70},
	{"emr.signature.doctor", "程序判断", "归档前处理", 80},
}

func (s *Store) EnsureBuiltin(ctx context.Context) error {
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return fmt.Errorf("begin builtin emr template: %w", err)
	}
	defer tx.Rollback(ctx)

	if _, err := tx.Exec(ctx, `
		INSERT INTO emr_templates (tenant_id, code, name, status, created_by, updated_by)
		VALUES (NULL, 'standard_outpatient', '标准门急诊病历', 'enabled', 0, 0)
		ON CONFLICT ((COALESCE(tenant_id, 0)), code) DO NOTHING
	`); err != nil {
		return fmt.Errorf("ensure builtin emr template: %w", err)
	}
	var templateID string
	err = tx.QueryRow(ctx, `
		SELECT id FROM emr_templates
		WHERE tenant_id IS NULL AND code='standard_outpatient'
	`).Scan(&templateID)
	if err != nil {
		return fmt.Errorf("load builtin emr template: %w", err)
	}

	var versionID, versionStatus string
	createdVersion := false
	err = tx.QueryRow(ctx, `
		SELECT id, status FROM emr_template_versions
		WHERE template_id=$1 AND version_no='1.0'
	`, templateID).Scan(&versionID, &versionStatus)
	if err == pgx.ErrNoRows {
		createdVersion = true
		err = tx.QueryRow(ctx, `
		INSERT INTO emr_template_versions
		(template_id, version_no, name, document_type, visit_type, print_title,
		 description, status, created_by, published_at, published_by)
		VALUES ($1, '1.0', '标准门急诊病历 v1.0', '门诊病历', '通用',
		 '门诊病历', '平台提供的基础门诊病历模板', 'published', 0, NOW(), 0)
		RETURNING id
	`, templateID).Scan(&versionID)
	}
	if err != nil {
		return fmt.Errorf("ensure builtin emr template version: %w", err)
	}
	if !createdVersion && versionStatus != "published" {
		return fmt.Errorf("内置模板版本不是已发布状态，请按电子病历清理重建清单重置开发数据库")
	}

	if createdVersion {
		for _, section := range builtinCommonSections {
			if _, err := tx.Exec(ctx, `
			INSERT INTO emr_template_sections
			(template_version_id, code, name, content_category, input_type,
			 is_common, is_visible, display_order)
			VALUES ($1,$2,$3,$4,$5,$6,$7,$8)
			`, versionID, section.Code, section.Name, section.Category, section.InputType,
				section.Common, section.Visible, section.Order); err != nil {
				return fmt.Errorf("ensure builtin template section %s: %w", section.Code, err)
			}
		}
	} else if err := validatePublishableVersion(ctx, tx, versionID); err != nil {
		return fmt.Errorf("内置模板存在旧结构，请按电子病历清理重建清单重置开发数据库：%w", err)
	}

	if createdVersion {
		for _, binding := range builtinQualityBindings {
			command, err := tx.Exec(ctx, `
			INSERT INTO emr_template_quality_requirements
			(template_version_id, quality_requirement_id, execution_mode, deadline_action, display_order)
			SELECT $1, id, $3, $4, $5
			FROM emr_quality_requirements
			WHERE code=$2 AND tenant_id IS NULL AND status='published'
		`, versionID, binding.Code, binding.ExecutionMode, binding.DeadlineAction, binding.Order)
			if err != nil {
				return fmt.Errorf("ensure builtin template binding %s: %w", binding.Code, err)
			}
			if command.RowsAffected() != 1 {
				return fmt.Errorf("ensure builtin template binding %s: quality requirement not found", binding.Code)
			}
		}
	}
	var bindingCount int
	if err := tx.QueryRow(ctx, `SELECT COUNT(*) FROM emr_template_quality_requirements WHERE template_version_id=$1`, versionID).Scan(&bindingCount); err != nil {
		return fmt.Errorf("count builtin template bindings: %w", err)
	}
	if bindingCount != len(builtinQualityBindings) {
		return fmt.Errorf("内置模板存在旧质量要求绑定，请按电子病历清理重建清单重置开发数据库：got %d, want %d", bindingCount, len(builtinQualityBindings))
	}
	if err := validateBuiltinBindings(ctx, tx, versionID); err != nil {
		return err
	}

	if err := tx.Commit(ctx); err != nil {
		return fmt.Errorf("commit builtin emr template: %w", err)
	}
	return nil
}

func (s *Store) List(ctx context.Context, tenantID int64) ([]*Template, error) {
	rows, err := s.pool.Query(ctx, `SELECT id, tenant_id, code, name, status, created_by, updated_by, created_at, updated_at
 FROM emr_templates WHERE (tenant_id IS NULL OR tenant_id=$1) ORDER BY code`, tenantID)
	if err != nil {
		return nil, fmt.Errorf("list emr templates: %w", err)
	}
	defer rows.Close()
	items := make([]*Template, 0)
	for rows.Next() {
		item := &Template{}
		if err := rows.Scan(&item.ID, &item.TenantID, &item.Code, &item.Name, &item.Status, &item.CreatedBy, &item.UpdatedBy, &item.CreatedAt, &item.UpdatedAt); err != nil {
			return nil, err
		}
		items = append(items, item)
	}
	return items, rows.Err()
}

func (s *Store) Get(ctx context.Context, tenantID int64, id string) (*TemplateDetail, error) {
	t := &Template{}
	err := s.pool.QueryRow(ctx, `SELECT id, tenant_id, code, name, status, created_by, updated_by, created_at, updated_at
 FROM emr_templates WHERE id=$1 AND (tenant_id IS NULL OR tenant_id=$2)`, id, tenantID).Scan(&t.ID, &t.TenantID, &t.Code, &t.Name, &t.Status, &t.CreatedBy, &t.UpdatedBy, &t.CreatedAt, &t.UpdatedAt)
	if err == pgx.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("get emr template: %w", err)
	}
	versions, err := s.listVersions(ctx, id)
	if err != nil {
		return nil, err
	}
	detail := &TemplateDetail{Template: t, Versions: versions}
	for _, version := range versions {
		if version.Status == "draft" {
			detail.Sections, _ = s.listSections(ctx, version.ID)
			detail.Bindings, _ = s.listBindings(ctx, version.ID)
			break
		}
	}
	if len(detail.Sections) == 0 && len(versions) > 0 {
		detail.Sections, _ = s.listSections(ctx, versions[0].ID)
		detail.Bindings, _ = s.listBindings(ctx, versions[0].ID)
	}
	return detail, nil
}

func (s *Store) listVersions(ctx context.Context, templateID string) ([]*TemplateVersion, error) {
	rows, err := s.pool.Query(ctx, `SELECT id,template_id,version_no,name,document_type,visit_type,department_id,specialty_module,print_title,description,status,created_by,published_at,published_by,disabled_at,disabled_by,created_at FROM emr_template_versions WHERE template_id=$1 ORDER BY created_at DESC`, templateID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := make([]*TemplateVersion, 0)
	for rows.Next() {
		v := &TemplateVersion{}
		if err := rows.Scan(&v.ID, &v.TemplateID, &v.VersionNo, &v.Name, &v.DocumentType, &v.VisitType, &v.DepartmentID, &v.SpecialtyModule, &v.PrintTitle, &v.Description, &v.Status, &v.CreatedBy, &v.PublishedAt, &v.PublishedBy, &v.DisabledAt, &v.DisabledBy, &v.CreatedAt); err != nil {
			return nil, err
		}
		out = append(out, v)
	}
	return out, rows.Err()
}
func (s *Store) ListVersions(ctx context.Context, tenantID int64, templateID string) ([]*TemplateVersion, error) {
	var ok bool
	if err := s.pool.QueryRow(ctx, `SELECT TRUE FROM emr_templates WHERE id=$1 AND (tenant_id IS NULL OR tenant_id=$2)`, templateID, tenantID).Scan(&ok); err != nil {
		return nil, err
	}
	return s.listVersions(ctx, templateID)
}
func (s *Store) GetVersion(ctx context.Context, tenantID int64, versionID string) (*TemplateVersion, error) {
	v := &TemplateVersion{}
	err := s.pool.QueryRow(ctx, `SELECT v.id,v.template_id,v.version_no,v.name,v.document_type,v.visit_type,v.department_id,v.specialty_module,v.print_title,v.description,v.status,v.created_by,v.published_at,v.published_by,v.disabled_at,v.disabled_by,v.created_at FROM emr_template_versions v JOIN emr_templates t ON t.id=v.template_id WHERE v.id=$1 AND (t.tenant_id IS NULL OR t.tenant_id=$2)`, versionID, tenantID).Scan(&v.ID, &v.TemplateID, &v.VersionNo, &v.Name, &v.DocumentType, &v.VisitType, &v.DepartmentID, &v.SpecialtyModule, &v.PrintTitle, &v.Description, &v.Status, &v.CreatedBy, &v.PublishedAt, &v.PublishedBy, &v.DisabledAt, &v.DisabledBy, &v.CreatedAt)
	if err == pgx.ErrNoRows {
		return nil, nil
	}
	return v, err
}

func (s *Store) CreateTemplate(ctx context.Context, tenantID, actorID int64, req SaveTemplateRequest) (*TemplateDetail, error) {
	var id string
	err := s.pool.QueryRow(ctx, `INSERT INTO emr_templates(tenant_id,code,name,status,created_by,updated_by) VALUES($1,$2,$3,$4,$5,$5) RETURNING id`, tenantID, req.Code, req.Name, req.Status, actorID).Scan(&id)
	if err != nil {
		return nil, err
	}
	return s.Get(ctx, tenantID, id)
}
func (s *Store) CreateVersion(ctx context.Context, tenantID, actorID int64, templateID string, req SaveVersionRequest) (*TemplateVersion, error) {
	var id string
	err := s.pool.QueryRow(ctx, `INSERT INTO emr_template_versions(template_id,version_no,name,document_type,visit_type,department_id,specialty_module,print_title,description,created_by) SELECT id,$2,$3,$4,$5,$6,$7,$8,$9,$10 FROM emr_templates WHERE id=$1 AND (tenant_id IS NULL OR tenant_id=$11) RETURNING id`, templateID, req.VersionNo, req.Name, req.DocumentType, req.VisitType, req.DepartmentID, req.SpecialtyModule, req.PrintTitle, req.Description, actorID, tenantID).Scan(&id)
	if err != nil {
		return nil, err
	}
	return s.GetVersion(ctx, tenantID, id)
}
func (s *Store) UpdateVersion(ctx context.Context, tenantID int64, versionID string, req SaveVersionRequest) (*TemplateVersion, error) {
	if err := s.requireDraftVersion(ctx, tenantID, versionID); err != nil {
		return nil, err
	}
	_, err := s.pool.Exec(ctx, `UPDATE emr_template_versions v SET version_no=$2,name=$3,document_type=$4,visit_type=$5,department_id=$6,specialty_module=$7,print_title=$8,description=$9 FROM emr_templates t WHERE v.template_id=t.id AND v.id=$1 AND (t.tenant_id IS NULL OR t.tenant_id=$10)`, versionID, req.VersionNo, req.Name, req.DocumentType, req.VisitType, req.DepartmentID, req.SpecialtyModule, req.PrintTitle, req.Description, tenantID)
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
	if _, err = tx.Exec(ctx, `DELETE FROM emr_template_sections WHERE template_version_id=$1`, versionID); err != nil {
		return nil, err
	}
	for _, item := range items {
		o, _ := json.Marshal(item.Options)
		st, _ := json.Marshal(item.Structure)
		if _, err = tx.Exec(ctx, `INSERT INTO emr_template_sections(template_version_id,code,name,content_category,input_type,is_common,is_visible,display_order,description,options_json,structure_json) VALUES($1,$2,$3,$4,$5,$6,$7,$8,$9,$10::jsonb,$11::jsonb)`, versionID, item.Code, item.Name, item.ContentCategory, item.InputType, item.IsCommon, item.IsVisible, item.DisplayOrder, item.Description, string(o), string(st)); err != nil {
			return nil, err
		}
	}
	if err = tx.Commit(ctx); err != nil {
		return nil, err
	}
	return s.listSections(ctx, versionID)
}
func (s *Store) SaveBindings(ctx context.Context, tenantID int64, versionID string, items []BindingInput) ([]*RequirementBinding, error) {
	if err := s.requireDraftVersion(ctx, tenantID, versionID); err != nil {
		return nil, err
	}
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback(ctx)
	if _, err = tx.Exec(ctx, `DELETE FROM emr_template_quality_requirements WHERE template_version_id=$1`, versionID); err != nil {
		return nil, err
	}
	for _, item := range items {
		if _, err = tx.Exec(ctx, `INSERT INTO emr_template_quality_requirements(template_version_id,quality_requirement_id,execution_mode,deadline_action,display_order) SELECT $1,id,$3,$4,$5 FROM emr_quality_requirements WHERE id=$2 AND (tenant_id IS NULL OR tenant_id=$6) AND status='published'`, versionID, item.QualityRequirementID, item.ExecutionMode, item.DeadlineAction, item.DisplayOrder, tenantID); err != nil {
			return nil, err
		}
	}
	if err = tx.Commit(ctx); err != nil {
		return nil, err
	}
	return s.listBindings(ctx, versionID)
}

func (s *Store) requireDraftVersion(ctx context.Context, tenantID int64, versionID string) error {
	var status string
	err := s.pool.QueryRow(ctx, `
		SELECT v.status
		FROM emr_template_versions v
		JOIN emr_templates t ON t.id=v.template_id
		WHERE v.id=$1 AND (t.tenant_id IS NULL OR t.tenant_id=$2)
	`, versionID, tenantID).Scan(&status)
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
	var status string
	err = tx.QueryRow(ctx, `
		SELECT v.status
		FROM emr_template_versions v
		JOIN emr_templates t ON t.id=v.template_id
		WHERE v.id=$1 AND (t.tenant_id IS NULL OR t.tenant_id=$2)
	`, versionID, tenantID).Scan(&status)
	if err == pgx.ErrNoRows {
		return fmt.Errorf("template version not found")
	}
	if err != nil {
		return fmt.Errorf("load template version for publish: %w", err)
	}
	if status != "draft" {
		return fmt.Errorf("template version is not draft")
	}
	if err := validatePublishableVersion(ctx, tx, versionID); err != nil {
		return err
	}
	cmd, err := tx.Exec(ctx, `UPDATE emr_template_versions SET status='published',published_at=NOW(),published_by=$2 WHERE id=$1 AND status='draft'`, versionID, actorID)
	if err != nil {
		return err
	}
	if cmd.RowsAffected() == 0 {
		return fmt.Errorf("template version not found or not draft")
	}
	if err := tx.Commit(ctx); err != nil {
		return fmt.Errorf("commit template version publish: %w", err)
	}
	return nil
}

func validatePublishableVersion(ctx context.Context, tx pgx.Tx, versionID string) error {
	rows, err := tx.Query(ctx, `
		SELECT code, name, is_common, is_visible
		FROM emr_template_sections
		WHERE template_version_id=$1
	`, versionID)
	if err != nil {
		return fmt.Errorf("load template sections for publish: %w", err)
	}
	defer rows.Close()
	type sectionState struct {
		name    string
		common  bool
		visible bool
	}
	sections := make(map[string]sectionState, len(requiredCommonSections))
	for rows.Next() {
		var code string
		var section sectionState
		if err := rows.Scan(&code, &section.name, &section.common, &section.visible); err != nil {
			return fmt.Errorf("read template sections for publish: %w", err)
		}
		sections[code] = section
	}
	if err := rows.Err(); err != nil {
		return fmt.Errorf("read template sections for publish: %w", err)
	}

	missing := make([]string, 0)
	invalid := make([]string, 0)
	for _, required := range requiredCommonSections {
		section, ok := sections[required.Code]
		if !ok {
			missing = append(missing, required.Name)
			continue
		}
		if !section.common || !section.visible {
			invalid = append(invalid, required.Name)
		}
	}
	if len(missing) > 0 {
		return fmt.Errorf("模板发布失败：缺少公共栏目：%s", strings.Join(missing, "、"))
	}
	if len(invalid) > 0 {
		return fmt.Errorf("模板发布失败：公共栏目必须设置为公共且可见：%s", strings.Join(invalid, "、"))
	}
	return nil
}

func validateBuiltinBindings(ctx context.Context, tx pgx.Tx, versionID string) error {
	rows, err := tx.Query(ctx, `
		SELECT q.code, b.execution_mode, b.deadline_action, b.display_order
		FROM emr_template_quality_requirements b
		JOIN emr_quality_requirements q ON q.id=b.quality_requirement_id
		WHERE b.template_version_id=$1
		ORDER BY b.display_order
	`, versionID)
	if err != nil {
		return fmt.Errorf("load builtin template bindings: %w", err)
	}
	defer rows.Close()
	index := 0
	for rows.Next() {
		if index >= len(builtinQualityBindings) {
			return fmt.Errorf("内置模板存在旧质量要求绑定，请按电子病历清理重建清单重置开发数据库")
		}
		var code, mode, deadline string
		var order int
		if err := rows.Scan(&code, &mode, &deadline, &order); err != nil {
			return fmt.Errorf("read builtin template bindings: %w", err)
		}
		expected := builtinQualityBindings[index]
		if code != expected.Code || mode != expected.ExecutionMode || deadline != expected.DeadlineAction || order != expected.Order {
			return fmt.Errorf("内置模板存在旧质量要求绑定，请按电子病历清理重建清单重置开发数据库")
		}
		index++
	}
	if err := rows.Err(); err != nil {
		return fmt.Errorf("read builtin template bindings: %w", err)
	}
	if index != len(builtinQualityBindings) {
		return fmt.Errorf("内置模板存在旧质量要求绑定，请按电子病历清理重建清单重置开发数据库")
	}
	return nil
}
func (s *Store) Disable(ctx context.Context, tenantID int64, versionID string, actorID int64) error {
	cmd, err := s.pool.Exec(ctx, `UPDATE emr_template_versions v SET status='disabled',disabled_at=NOW(),disabled_by=$3 FROM emr_templates t WHERE v.id=$1 AND v.template_id=t.id AND v.status<>'disabled' AND (t.tenant_id IS NULL OR t.tenant_id=$2)`, versionID, tenantID, actorID)
	if err != nil {
		return err
	}
	if cmd.RowsAffected() == 0 {
		return fmt.Errorf("template version not found")
	}
	return nil
}

func (s *Store) listSections(ctx context.Context, versionID string) ([]*Section, error) {
	rows, err := s.pool.Query(ctx, `SELECT id,template_version_id,code,name,content_category,input_type,is_common,is_visible,display_order,description,options_json,structure_json FROM emr_template_sections WHERE template_version_id=$1 ORDER BY display_order`, versionID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := make([]*Section, 0)
	for rows.Next() {
		x := &Section{}
		var o, st []byte
		if err := rows.Scan(&x.ID, &x.TemplateVersionID, &x.Code, &x.Name, &x.ContentCategory, &x.InputType, &x.IsCommon, &x.IsVisible, &x.DisplayOrder, &x.Description, &o, &st); err != nil {
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
func (s *Store) listBindings(ctx context.Context, versionID string) ([]*RequirementBinding, error) {
	rows, err := s.pool.Query(ctx, `SELECT b.id,b.template_version_id,b.quality_requirement_id,b.execution_mode,b.deadline_action,b.display_order,q.code,q.name,q.rule_type,q.quality_group FROM emr_template_quality_requirements b JOIN emr_quality_requirements q ON q.id=b.quality_requirement_id WHERE b.template_version_id=$1 ORDER BY b.display_order,q.code`, versionID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := make([]*RequirementBinding, 0)
	for rows.Next() {
		x := &RequirementBinding{}
		if err := rows.Scan(&x.ID, &x.TemplateVersionID, &x.QualityRequirementID, &x.ExecutionMode, &x.DeadlineAction, &x.DisplayOrder, &x.RequirementCode, &x.RequirementName, &x.RequirementRuleType, &x.RequirementQualityGroup); err != nil {
			return nil, err
		}
		out = append(out, x)
	}
	return out, rows.Err()
}
