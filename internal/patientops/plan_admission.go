package patientops

import (
	"context"
	"fmt"
	"sort"
	"strings"

	"github.com/jackc/pgx/v5"
)

type PlanAdmissionError struct {
	Customers []PlanExcluded `json:"excluded_customers"`
}

func preservePlanAgeWarning(c PlanCandidate) PlanCandidate {
	if c.AdmissionStatus == "needs_confirmation" {
		c.RiskMedical = "年龄未知；运营已人工确认本次产品适用性，沟通时仍须核实实际使用者的年龄，医疗适用性由医生评估。" + c.RiskMedical
	}
	return c
}

func (e *PlanAdmissionError) Error() string {
	parts := make([]string, 0, len(e.Customers))
	for _, c := range e.Customers {
		parts = append(parts, fmt.Sprintf("%s（ID %d）：%s", c.CustomerName, c.CustomerID, c.Reason))
	}
	return "本次请求未执行。" + strings.Join(parts, "；")
}

// preview 与发布共用同一匹配结果；明确选择中存在阻断项时整批拒绝。
func selectPlanCandidates(preview *PlanPreview, ids []int64, confirmations []PlanSuitabilityConfirmation) ([]PlanCandidate, error) {
	byID := make(map[int64]PlanCandidate, len(preview.Candidates))
	for _, c := range preview.Candidates {
		byID[c.CustomerID] = c
	}
	reviews := make(map[int64]PlanSuitabilityConfirmation, len(confirmations))
	for _, c := range confirmations {
		if _, exists := reviews[c.CustomerID]; exists {
			return nil, fmt.Errorf("同一客户不能重复确认适用性")
		}
		reviews[c.CustomerID] = c
	}
	selected := make([]PlanCandidate, 0, len(ids))
	seen := map[int64]bool{}
	rejected := &PlanAdmissionError{}
	for _, id := range ids {
		if seen[id] {
			continue
		}
		seen[id] = true
		c, ok := byID[id]
		reason := "客户已不符合当前计划条件，请重新生成预览"
		if ok {
			reason = ""
			// 年龄/性别只作提示，不再阻断发布；已知超范围在匹配阶段已排除。
		}
		if reason != "" {
			rejected.Customers = append(rejected.Customers, PlanExcluded{CustomerID: id, CustomerName: c.CustomerName, Reason: reason})
		} else {
			selected = append(selected, c)
		}
	}
	if len(rejected.Customers) > 0 {
		return nil, rejected
	}
	return selected, nil
}

// 大模型生成期间客户/产品可能改变。写入前锁定并重新核对，防止沿用过期确认。
func validatePlanAdmissionSnapshot(ctx context.Context, tx pgx.Tx, plan *Plan, candidates []PlanCandidate) error {
	var status, updatedAt string
	if err := tx.QueryRow(ctx, `SELECT status,to_char(updated_at,'YYYY-MM-DD"T"HH24:MI:SS.USTZH:TZM') FROM patient_ops_plans WHERE tenant_id=$1 AND id=$2 AND deleted_at IS NULL FOR UPDATE`, plan.TenantID, plan.ID).Scan(&status, &updatedAt); err != nil {
		return err
	}
	if status != plan.Status || updatedAt != plan.UpdatedAt {
		return fmt.Errorf("计划设置或状态已变化，请刷新后重试")
	}
	ordered := append([]PlanCandidate(nil), candidates...)
	sort.Slice(ordered, func(i, j int) bool { return ordered[i].CustomerID < ordered[j].CustomerID })
	for _, c := range ordered {
		var age int
		var identity string
		if err := tx.QueryRow(ctx, `SELECT COALESCE(NULLIF(age,0),EXTRACT(YEAR FROM AGE(CURRENT_DATE,birth_date))::int,0),COALESCE(extra_data->>'identity_status','') FROM customers WHERE tenant_id=$1 AND id=$2 AND deleted_at IS NULL FOR UPDATE`, plan.TenantID, c.CustomerID).Scan(&age, &identity); err != nil {
			return err
		}
		if identity == "pending_confirm" || age != c.Age {
			return &PlanAdmissionError{Customers: []PlanExcluded{{CustomerID: c.CustomerID, CustomerName: c.CustomerName, Reason: "客户资料已变化，请重新生成预览并确认"}}}
		}
		if c.RecommendedProductID != nil {
			var version int
			if err := tx.QueryRow(ctx, `SELECT version FROM products WHERE tenant_id=$1 AND id=$2 AND deleted_at IS NULL AND status='active' AND review_status='approved' AND is_template=false FOR SHARE`, plan.TenantID, *c.RecommendedProductID).Scan(&version); err != nil {
				return err
			}
			if version != c.ProductVersion {
				return &PlanAdmissionError{Customers: []PlanExcluded{{CustomerID: c.CustomerID, CustomerName: c.CustomerName, Reason: "产品资料已变化，请重新生成预览并确认"}}}
			}
		}
	}
	return nil
}

func savePlanSuitabilityReviews(ctx context.Context, tx pgx.Tx, plan *Plan, operatorID int64, candidates []PlanCandidate, reviews []PlanSuitabilityConfirmation) error {
	for _, c := range candidates {
		if c.AdmissionStatus != "needs_confirmation" {
			continue
		}
		for _, review := range reviews {
			if review.CustomerID != c.CustomerID {
				continue
			}
			if _, err := tx.Exec(ctx, `INSERT INTO patient_ops_product_suitability_reviews(tenant_id,plan_id,customer_id,product_id,product_version,operator_id,reason,customer_age) VALUES($1,$2,$3,$4,$5,$6,$7,$8)`, plan.TenantID, plan.ID, c.CustomerID, review.ProductID, review.ProductVersion, operatorID, strings.TrimSpace(review.Reason), c.Age); err != nil {
				return fmt.Errorf("save product suitability review: %w", err)
			}
		}
	}
	return nil
}
