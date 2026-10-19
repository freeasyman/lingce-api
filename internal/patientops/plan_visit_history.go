package patientops

import (
	"context"
	"strings"
)

// loadPlanVisitHistory 只对即将生成内容的候选客户加载最近就诊，避免筛选阶段的全量开销。
func (s *Service) loadPlanVisitHistory(ctx context.Context, tenantID int64, candidates []PlanCandidate) map[int64][]PlanVisitFact {
	result := make(map[int64][]PlanVisitFact, len(candidates))
	if len(candidates) == 0 {
		return result
	}
	ids := make([]int64, 0, len(candidates))
	seen := map[int64]struct{}{}
	for _, cand := range candidates {
		if _, ok := seen[cand.CustomerID]; ok {
			continue
		}
		seen[cand.CustomerID] = struct{}{}
		ids = append(ids, cand.CustomerID)
	}

	rows, err := s.pool.Query(ctx, `
		SELECT i.customer_id,
		       COALESCE(d.payload->'data'->>'visit_at',''),
		       COALESCE(d.payload->'data'->>'visit_type',''),
		       COALESCE(d.payload->'data'->>'department',''),
		       COALESCE(d.payload->'data'->>'doctor',''),
		       COALESCE(d.payload->'data'->>'diagnosis',''),
		       COALESCE(d.payload->'data'->>'chief_complaint',''),
		       COALESCE(d.payload->'data'->>'treatment_plan',''),
		       COALESCE(d.payload->'data'->>'fee','')
		FROM external_data_source_identities i
		JOIN external_data_deliveries d
		  ON d.tenant_id=i.tenant_id
		 AND d.source_system=i.source_system
		 AND d.object_type='visits'
		 AND d.payload->'patient'->>'source_patient_id'=i.source_patient_id
		WHERE i.tenant_id=$1 AND i.customer_id = ANY($2)
		ORDER BY i.customer_id, d.source_updated_at DESC NULLS LAST, d.created_at DESC
	`, tenantID, ids)
	if err != nil {
		return result
	}
	for rows.Next() {
		var customerID int64
		var item PlanVisitFact
		if err := rows.Scan(&customerID, &item.VisitAt, &item.VisitType, &item.Department, &item.Doctor, &item.Diagnosis, &item.ChiefComplaint, &item.TreatmentPlan, &item.Fee); err != nil {
			continue
		}
		if len(result[customerID]) >= 6 {
			continue
		}
		result[customerID] = append(result[customerID], item)
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return result
	}

	// 没有外部就诊投递的客户，用系统 encounters 补最近就诊；不覆盖外部更完整的诊断。
	var missing []int64
	for _, id := range ids {
		if _, ok := result[id]; !ok {
			missing = append(missing, id)
		}
	}
	if len(missing) == 0 {
		return result
	}
	encRows, err := s.pool.Query(ctx, `
		SELECT patient_id,
		       to_char(started_at at time zone 'Asia/Shanghai','YYYY-MM-DD'),
		       COALESCE(visit_type,''),
		       '',
		       COALESCE(provider_name,''),
		       '',
		       '',
		       '',
		       ''
		FROM encounters
		WHERE tenant_id=$1 AND patient_id = ANY($2)
		ORDER BY started_at DESC
	`, tenantID, missing)
	if err != nil {
		return result
	}
	defer encRows.Close()
	for encRows.Next() {
		var customerID int64
		var item PlanVisitFact
		if err := encRows.Scan(&customerID, &item.VisitAt, &item.VisitType, &item.Department, &item.Doctor, &item.Diagnosis, &item.ChiefComplaint, &item.TreatmentPlan, &item.Fee); err != nil {
			continue
		}
		if len(result[customerID]) >= 6 {
			continue
		}
		result[customerID] = append(result[customerID], item)
	}
	return result
}

func planVisitHistorySummary(items []PlanVisitFact) string {
	parts := make([]string, 0, len(items))
	for _, item := range items {
		line := strings.TrimSpace(item.VisitAt)
		if item.VisitType != "" {
			line += " " + item.VisitType
		}
		if item.Department != "" {
			line += " " + item.Department
		}
		if item.Doctor != "" {
			line += " " + item.Doctor
		}
		if item.Diagnosis != "" {
			line += " 诊断：" + item.Diagnosis
		}
		if item.Fee != "" {
			line += " 费用：" + item.Fee
		}
		if strings.TrimSpace(line) != "" {
			parts = append(parts, line)
		}
	}
	if len(parts) == 0 {
		return "无可用就诊历史"
	}
	return strings.Join(parts, "；")
}
