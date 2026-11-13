package his

import (
	"context"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
)

// 科室链路（P0-4）：临床科室字典 → 医生科室 → 排班 → 挂号/encounter → 门诊按科室自动匹配 EMR 模板。
//
// 设计约束：
//   - departments 是共享表，只读取 type/template_code 两个新增列，不改共享 department 产品的接口。
//   - 模板解析顺序：租户自有模板（tenant_id=租户）优先，其次全局模板（tenant_id=0）；
//     科室未配置 template_code / 模板未发布时，降级到 standard_outpatient 并给出提示。
//   - 历史数据（排班/挂号/encounter 无 department_id）不报错：返回 Department=nil，前端提示补科室。

const (
	departmentTypeClinical = "clinical"
	fallbackTemplateCode   = "standard_outpatient"
)

// 模板降级原因（服务层据此生成给医生看的提示）。
const (
	templateReasonNone                 = ""
	templateReasonNoDepartment         = "no_department"
	templateReasonNoTemplateCode       = "no_template_code"
	templateReasonTemplateNotPublished = "template_not_published"
	templateReasonNoFallback           = "no_fallback_template"
)

// templateResolution 一次就诊的科室 + 模板解析结果（内部结构，不直接出 JSON）。
type templateResolution struct {
	Department *DepartmentBrief
	Template   *EMRTemplate
	Matched    bool
	Reason     string
}

// ListClinicalDepartments 管理台/排班可选临床科室：departments.type='clinical'，带可用的模板名。
func (s *Store) ListClinicalDepartments(ctx context.Context, tenantID int64) ([]ClinicalDepartment, error) {
	rows, err := s.pool.Query(ctx, `
		SELECT d.id, d.name, COALESCE(d.code, ''), COALESCE(d.template_code, ''),
		       COALESCE((
		           SELECT t.name FROM emr_templates t
		           WHERE t.code = d.template_code
		             AND t.status = 'published' AND t.is_current
		             AND t.tenant_id IN (0, $1)
		           ORDER BY t.tenant_id DESC
		           LIMIT 1
		       ), '') AS template_name
		FROM departments d
		WHERE d.tenant_id = $1
		  AND d.deleted_at IS NULL
		  AND COALESCE(d.is_active, TRUE)
		  AND d.type = $2
		ORDER BY d.id`, tenantID, departmentTypeClinical)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	items := make([]ClinicalDepartment, 0)
	for rows.Next() {
		var item ClinicalDepartment
		if err := rows.Scan(&item.ID, &item.Name, &item.Code, &item.TemplateCode, &item.TemplateName); err != nil {
			return nil, err
		}
		items = append(items, item)
	}
	return items, rows.Err()
}

// findPublishedTemplateByCode 按 code 查找租户可用模板：租户模板优先，全局模板兜底。
func (s *Store) findPublishedTemplateByCode(ctx context.Context, q querier, tenantID int64, code string) (*EMRTemplate, error) {
	code = strings.TrimSpace(code)
	if code == "" {
		return nil, nil
	}
	var item EMRTemplate
	err := q.QueryRow(ctx, `
		SELECT id::text, code, name, COALESCE(version_no, ''), document_type, visit_type,
		       COALESCE(specialty, ''), department_id
		FROM emr_templates
		WHERE code = $1 AND status = 'published' AND is_current
		  AND tenant_id IN (0, $2)
		ORDER BY tenant_id DESC
		LIMIT 1`, code, tenantID).
		Scan(&item.ID, &item.Code, &item.Name, &item.VersionNo, &item.DocumentType,
			&item.VisitType, &item.Specialty, &item.DepartmentID)
	if err == pgx.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &item, nil
}

// departmentBrief 读取科室基础信息。
func (s *Store) departmentBrief(ctx context.Context, q querier, tenantID int64, departmentID *int64) (*DepartmentBrief, error) {
	if departmentID == nil || *departmentID <= 0 {
		return nil, nil
	}
	var brief DepartmentBrief
	err := q.QueryRow(ctx, `
		SELECT d.id, d.name, COALESCE(d.code, ''), COALESCE(d.template_code, '')
		FROM departments d
		WHERE d.tenant_id = $1 AND d.id = $2 AND d.deleted_at IS NULL`,
		tenantID, *departmentID).
		Scan(&brief.ID, &brief.Name, &brief.Code, &brief.TemplateCode)
	if err == pgx.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &brief, nil
}

// ResolveEncounterTemplate 解析本次就诊的科室与模板：
//   - encounter.department_id 缺失时回退到挂号单 department_id（历史数据兼容）；
//   - 科室没有 template_code 或模板未发布 → 标准门急诊病历 + 原因；
//   - 找不到任何模板 → Template 为 nil（调用方返回错误，不静默写空病历）。
func (s *Store) ResolveEncounterTemplate(ctx context.Context, tenantID, encounterID int64) (*templateResolution, error) {
	if encounterID <= 0 {
		return nil, ErrInvalidRequest
	}

	var departmentID *int64
	err := s.pool.QueryRow(ctx, `
		SELECT COALESCE(e.department_id, r.department_id)
		FROM encounters e
		LEFT JOIN his_registrations r
		  ON r.tenant_id = e.tenant_id AND e.source_type = 'his' AND r.id = e.source_id
		WHERE e.tenant_id = $1 AND e.id = $2`, tenantID, encounterID).Scan(&departmentID)
	if err == pgx.ErrNoRows {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, err
	}

	resolution := &templateResolution{}
	brief, err := s.departmentBrief(ctx, s.pool, tenantID, departmentID)
	if err != nil {
		return nil, err
	}
	resolution.Department = brief

	code := ""
	if brief != nil {
		code = strings.TrimSpace(brief.TemplateCode)
	}

	if brief == nil {
		resolution.Reason = templateReasonNoDepartment
	} else if code == "" {
		resolution.Reason = templateReasonNoTemplateCode
	} else {
		template, err := s.findPublishedTemplateByCode(ctx, s.pool, tenantID, code)
		if err != nil {
			return nil, err
		}
		if template != nil {
			resolution.Template = template
			resolution.Matched = true
			brief.TemplateID = template.ID
			brief.TemplateName = template.Name
			brief.Matched = true
			return resolution, nil
		}
		resolution.Reason = templateReasonTemplateNotPublished
	}

	// 降级：标准门急诊病历（科室未匹配模板时也保证能书写病历）。
	fallback, err := s.findPublishedTemplateByCode(ctx, s.pool, tenantID, fallbackTemplateCode)
	if err != nil {
		return nil, err
	}
	if fallback == nil {
		resolution.Reason = templateReasonNoFallback
		return resolution, nil
	}
	resolution.Template = fallback
	if brief != nil {
		brief.TemplateID = fallback.ID
		brief.TemplateName = fallback.Name
		brief.Matched = false
	}
	return resolution, nil
}

// encounterVisitType 把 encounter.visit_type 映射为 EMR 的初诊/复诊。
func (s *Store) encounterVisitType(ctx context.Context, tenantID, encounterID int64) (string, error) {
	var visitType string
	err := s.pool.QueryRow(ctx, `
		SELECT COALESCE(visit_type, '') FROM encounters
		WHERE tenant_id = $1 AND id = $2`, tenantID, encounterID).Scan(&visitType)
	if err == pgx.ErrNoRows {
		return "", ErrNotFound
	}
	if err != nil {
		return "", err
	}
	if strings.TrimSpace(visitType) == "return_visit" {
		return "复诊", nil
	}
	return "初诊", nil
}

// SwitchEMRTemplate 更换病历模板：仅允许「草稿 + 内容为空」的病历，避免已书写内容与模板字段错位。
func (s *Store) SwitchEMRTemplate(ctx context.Context, tenantID int64, recordID, templateID string) (*EMRRecordSummary, error) {
	recordID = strings.TrimSpace(recordID)
	templateID = strings.TrimSpace(templateID)
	if recordID == "" || templateID == "" {
		return nil, ErrInvalidRequest
	}

	var published bool
	if err := s.pool.QueryRow(ctx, `
		SELECT EXISTS(
			SELECT 1 FROM emr_templates
			WHERE id = $1::uuid AND status = 'published' AND is_current AND tenant_id IN (0, $2)
		)`, templateID, tenantID).Scan(&published); err != nil {
		return nil, err
	}
	if !published {
		return nil, ErrNotFound
	}

	tag, err := s.pool.Exec(ctx, `
		UPDATE emr_records r
		SET template_id = $3::uuid, updated_at = NOW()
		WHERE r.tenant_id = $1 AND r.id = $2::uuid
		  AND r.status = '草稿'
		  AND (r.content_json IS NULL OR r.content_json = '{}'::jsonb)`,
		tenantID, recordID, templateID)
	if err != nil {
		return nil, err
	}
	if tag.RowsAffected() == 0 {
		// 区分「不存在」与「已有内容/非草稿」，便于前端给出准确提示。
		var status string
		var empty bool
		err := s.pool.QueryRow(ctx, `
			SELECT status, (content_json IS NULL OR content_json = '{}'::jsonb)
			FROM emr_records WHERE tenant_id = $1 AND id = $2::uuid`, tenantID, recordID).Scan(&status, &empty)
		if err == pgx.ErrNoRows {
			return nil, ErrNotFound
		}
		if err != nil {
			return nil, err
		}
		return nil, ErrInvalidState
	}
	return s.GetEMRRecordSummary(ctx, tenantID, recordID)
}

// ListOutpatientItems 门诊页「已开项目」：检查/检验/治疗 + 未结单中的收费项目（全部读库）。
// 收费行排除已被检验/检查/治疗引用的行，避免同一笔在页面重复展示。
func (s *Store) ListOutpatientItems(ctx context.Context, tenantID, encounterID int64) (*OutpatientItems, error) {
	if encounterID <= 0 {
		return nil, ErrInvalidRequest
	}
	resolution, err := s.ResolveEncounterTemplate(ctx, tenantID, encounterID)
	if err != nil {
		return nil, err
	}

	items := make([]OutpatientItem, 0)
	total := 0.0
	appendRow := func(item OutpatientItem) {
		items = append(items, item)
		total += item.Amount
	}

	// 检验
	labRows, err := s.pool.Query(ctx, `
		SELECT l.id, l.item_name, COALESCE(l.charge_amount, 0), l.status, l.created_at, l.order_id, l.charge_item_id,
		       COALESCE((SELECT o.status FROM his_orders o WHERE o.id = l.order_id), '')
		FROM his_lab_requests l
		WHERE l.tenant_id = $1 AND l.encounter_id = $2 AND l.status <> 'cancelled'
		ORDER BY l.id`, tenantID, encounterID)
	if err != nil {
		return nil, err
	}
	for labRows.Next() {
		var id int64
		var name, status, orderStatus string
		var amount float64
		var createdAt time.Time
		var orderID, chargeItemID *int64
		if err := labRows.Scan(&id, &name, &amount, &status, &createdAt, &orderID, &chargeItemID, &orderStatus); err != nil {
			labRows.Close()
			return nil, err
		}
		appendRow(OutpatientItem{
			Source: "lab", RefID: id, OrderID: orderID, ChargeItemID: chargeItemID,
			Name: name, Amount: amount, Status: status, CreatedAt: createdAt,
			Cancellable: status == labStatusPending && (orderStatus == "" || orderStatus == "pending"),
		})
	}
	labRows.Close()
	if err := labRows.Err(); err != nil {
		return nil, err
	}

	// 检查
	examRows, err := s.pool.Query(ctx, `
		SELECT x.id, x.item_name, COALESCE(x.charge_amount, 0), x.status, x.created_at, x.order_id, x.charge_item_id,
		       COALESCE((SELECT o.status FROM his_orders o WHERE o.id = x.order_id), '')
		FROM his_exam_requests x
		WHERE x.tenant_id = $1 AND x.encounter_id = $2 AND x.status <> 'cancelled'
		ORDER BY x.id`, tenantID, encounterID)
	if err != nil {
		return nil, err
	}
	for examRows.Next() {
		var id int64
		var name, status, orderStatus string
		var amount float64
		var createdAt time.Time
		var orderID, chargeItemID *int64
		if err := examRows.Scan(&id, &name, &amount, &status, &createdAt, &orderID, &chargeItemID, &orderStatus); err != nil {
			examRows.Close()
			return nil, err
		}
		appendRow(OutpatientItem{
			Source: "exam", RefID: id, OrderID: orderID, ChargeItemID: chargeItemID,
			Name: name, Amount: amount, Status: status, CreatedAt: createdAt,
			Cancellable: status == examStatusPending && (orderStatus == "" || orderStatus == "pending"),
		})
	}
	examRows.Close()
	if err := examRows.Err(); err != nil {
		return nil, err
	}

	// 治疗
	treatmentRows, err := s.pool.Query(ctx, `
		SELECT t.id, COALESCE(ci.name, ''), COALESCE(ci.price, 0), t.status, t.created_at,
		       t.order_id, t.charge_item_id, t.completed_times,
		       COALESCE((SELECT o.status FROM his_orders o WHERE o.id = t.order_id), '')
		FROM his_treatment_orders t
		LEFT JOIN his_charge_items ci ON ci.tenant_id = t.tenant_id AND ci.id = t.charge_item_id
		WHERE t.tenant_id = $1 AND t.encounter_id = $2 AND t.status <> 'cancelled'
		ORDER BY t.id`, tenantID, encounterID)
	if err != nil {
		return nil, err
	}
	for treatmentRows.Next() {
		var id int64
		var name, status, orderStatus string
		var amount float64
		var createdAt time.Time
		var orderID, chargeItemID *int64
		var completed int
		if err := treatmentRows.Scan(&id, &name, &amount, &status, &createdAt, &orderID, &chargeItemID, &completed, &orderStatus); err != nil {
			treatmentRows.Close()
			return nil, err
		}
		appendRow(OutpatientItem{
			Source: "treatment", RefID: id, OrderID: orderID, ChargeItemID: chargeItemID,
			Name: name, Amount: amount, Status: status, CreatedAt: createdAt,
			Cancellable: completed == 0 && status != "cancelled" && status != "completed" && (orderStatus == "" || orderStatus == "pending"),
		})
	}
	treatmentRows.Close()
	if err := treatmentRows.Err(); err != nil {
		return nil, err
	}

	// 收费项目（耗材/其他）：未结单里未被检验/检查/治疗引用的 charge_item 行。
	chargeRows, err := s.pool.Query(ctx, `
		SELECT oi.id, oi.order_id, oi.ref_id, oi.name, oi.payable, o.status, o.created_at
		FROM his_order_items oi
		JOIN his_orders o ON o.id = oi.order_id
		WHERE o.tenant_id = $1 AND o.encounter_id = $2 AND o.status IN ('pending', 'partial', 'paid')
		  AND oi.item_type = 'charge_item'
		  AND NOT EXISTS (
		      SELECT 1 FROM his_lab_requests l
		      WHERE l.tenant_id = o.tenant_id AND l.encounter_id = o.encounter_id
		        AND l.order_id = oi.order_id AND l.charge_item_id = oi.ref_id
		  )
		  AND NOT EXISTS (
		      SELECT 1 FROM his_exam_requests x
		      WHERE x.tenant_id = o.tenant_id AND x.encounter_id = o.encounter_id
		        AND x.order_id = oi.order_id AND x.charge_item_id = oi.ref_id
		  )
		  AND NOT EXISTS (
		      SELECT 1 FROM his_treatment_orders t
		      WHERE t.tenant_id = o.tenant_id AND t.encounter_id = o.encounter_id
		        AND t.order_id = oi.order_id AND t.charge_item_id = oi.ref_id
		  )
		ORDER BY oi.id`, tenantID, encounterID)
	if err != nil {
		return nil, err
	}
	for chargeRows.Next() {
		var itemID, orderID int64
		var refID *int64
		var name, status string
		var amount float64
		var createdAt time.Time
		if err := chargeRows.Scan(&itemID, &orderID, &refID, &name, &amount, &status, &createdAt); err != nil {
			chargeRows.Close()
			return nil, err
		}
		itemIDCopy, orderIDCopy := itemID, orderID
		appendRow(OutpatientItem{
			Source: "charge", RefID: itemID, OrderID: &orderIDCopy, OrderItemID: &itemIDCopy,
			ChargeItemID: refID, Name: name, Amount: amount, Status: status, CreatedAt: createdAt,
			Cancellable: status == "pending",
		})
	}
	chargeRows.Close()
	if err := chargeRows.Err(); err != nil {
		return nil, err
	}

	return &OutpatientItems{
		Department: resolution.Department,
		Items:      items,
		Total:      total,
	}, nil
}

// CancelOutpatientChargeItem 取消门诊开单产生的一条收费项目（耗材/其他收费项目）。
// 仅允许：未结清 pending 单 + charge_item 行 + 无处方关联 + 未被检验/检查/治疗引用。
// 删除该行后按剩余行重算；删空则整单作废（与检验/检查取消释放收费行的口径一致）。
func (s *Store) CancelOutpatientChargeItem(ctx context.Context, tenantID, orderID, itemID int64) (Order, error) {
	if orderID <= 0 || itemID <= 0 {
		return Order{}, ErrInvalidRequest
	}
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return Order{}, err
	}
	defer tx.Rollback(ctx)

	var status string
	if err := tx.QueryRow(ctx, `
		SELECT status FROM his_orders
		WHERE tenant_id = $1 AND id = $2 FOR UPDATE`, tenantID, orderID).Scan(&status); err != nil {
		if err == pgx.ErrNoRows {
			return Order{}, ErrNotFound
		}
		return Order{}, err
	}
	if status != "pending" {
		return Order{}, ErrInvalidState
	}

	var itemType string
	var refID, prescriptionID *int64
	if err := tx.QueryRow(ctx, `
		SELECT item_type, ref_id, prescription_id FROM his_order_items
		WHERE order_id = $1 AND id = $2`, orderID, itemID).Scan(&itemType, &refID, &prescriptionID); err != nil {
		if err == pgx.ErrNoRows {
			return Order{}, ErrNotFound
		}
		return Order{}, err
	}
	if itemType != chargeItemType || prescriptionID != nil || refID == nil {
		return Order{}, ErrInvalidState
	}

	// 已被检验/检查/治疗引用的收费行不允许从门诊页直接删，必须走各自的取消接口。
	var referenced bool
	if err := tx.QueryRow(ctx, `
		SELECT EXISTS(
			SELECT 1 FROM his_lab_requests l
			WHERE l.tenant_id = $1 AND l.order_id = $2 AND l.charge_item_id = $3
		) OR EXISTS(
			SELECT 1 FROM his_exam_requests x
			WHERE x.tenant_id = $1 AND x.order_id = $2 AND x.charge_item_id = $3
		) OR EXISTS(
			SELECT 1 FROM his_treatment_orders t
			WHERE t.tenant_id = $1 AND t.order_id = $2 AND t.charge_item_id = $3
		)`, tenantID, orderID, *refID).Scan(&referenced); err != nil {
		return Order{}, err
	}
	if referenced {
		return Order{}, ErrInvalidState
	}

	if _, err := tx.Exec(ctx, `DELETE FROM his_order_items WHERE order_id = $1 AND id = $2`, orderID, itemID); err != nil {
		return Order{}, err
	}

	var remaining int
	if err := tx.QueryRow(ctx, `SELECT COUNT(*) FROM his_order_items WHERE order_id = $1`, orderID).Scan(&remaining); err != nil {
		return Order{}, err
	}
	if remaining == 0 {
		if _, err := tx.Exec(ctx, `
			UPDATE his_orders SET status = 'void', updated_at = NOW()
			WHERE tenant_id = $1 AND id = $2`, tenantID, orderID); err != nil {
			return Order{}, err
		}
	} else if err := s.recalcOrderPricingTx(ctx, tx, orderID); err != nil {
		return Order{}, err
	}

	if err := tx.Commit(ctx); err != nil {
		return Order{}, err
	}
	return s.loadOrder(ctx, s.pool, tenantID, orderID)
}

// DefaultDoctorDepartment 排班未显式选科室时的默认值：医生档案上的临床科室。
func (s *Store) DefaultDoctorDepartment(ctx context.Context, tenantID, doctorID int64) (*int64, error) {
	var departmentID *int64
	err := s.pool.QueryRow(ctx, `
		SELECT d.department_id
		FROM his_doctors d
		JOIN departments dep ON dep.id = d.department_id
			AND dep.tenant_id = d.tenant_id AND dep.deleted_at IS NULL
			AND COALESCE(dep.is_active, TRUE) AND dep.type = $3
		WHERE d.tenant_id = $1 AND d.id = $2`, tenantID, doctorID, departmentTypeClinical).Scan(&departmentID)
	if err == pgx.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return departmentID, nil
}

// IsClinicalDepartment 校验科室属于本租户且是启用中的临床科室。
func (s *Store) IsClinicalDepartment(ctx context.Context, tenantID, departmentID int64) (bool, error) {
	var exists bool
	err := s.pool.QueryRow(ctx, `
		SELECT EXISTS(
			SELECT 1 FROM departments
			WHERE tenant_id = $1 AND id = $2 AND deleted_at IS NULL
			  AND COALESCE(is_active, TRUE) AND type = $3
		)`, tenantID, departmentID, departmentTypeClinical).Scan(&exists)
	return exists, err
}
