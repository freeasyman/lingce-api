package dealexpert

import (
	"context"
	"encoding/json"
	"net/http"
	"sort"
	"strings"

	"github.com/freeasyman/lingce-api/pkg/httputil"
)

// BlockerBucket 把模型顾虑类型归入阻碍八类固定枚举（与前端同口径）。
func BlockerBucket(concernType string) string {
	key := strings.TrimSpace(concernType)
	switch {
	case key == "":
		return "其他"
	case strings.Contains(key, "效果") || strings.Contains(key, "适配") || key == "effect_fit":
		return "效果与适配"
	case strings.Contains(key, "安全") || strings.Contains(key, "恢复") || key == "safety_recovery":
		return "安全与恢复"
	case strings.Contains(key, "价格") || strings.Contains(key, "价值") || strings.Contains(key, "费用") || key == "price_value":
		return "价格与价值"
	case strings.Contains(key, "信任") || strings.Contains(key, "医生") || strings.Contains(key, "机构") || key == "doctor_trust":
		return "医生与机构信任"
	case strings.Contains(key, "家属") || strings.Contains(key, "决策") || key == "family_decision":
		return "家属或共同决策人"
	case strings.Contains(key, "时间") || strings.Contains(key, "流程") || key == "time_process":
		return "时间与流程"
	case strings.Contains(key, "竞品") || strings.Contains(key, "比较") || key == "competitor":
		return "竞品比较"
	case strings.Contains(key, "疼痛") || strings.Contains(key, "隐私") || strings.Contains(key, "情绪") || key == "pain_privacy":
		return "疼痛隐私情绪"
	default:
		return "其他"
	}
}

type DiagnosisBucket struct {
	Label string `json:"label"`
	Count int    `json:"count"`
}

type DiagnosisResponse struct {
	WindowDays     int               `json:"window_days"`
	Total          int               `json:"total"`
	Chosen         int               `json:"chosen"`
	Undecided      int               `json:"undecided"`
	NotChosen      int               `json:"not_chosen"`
	BlockerBuckets []DiagnosisBucket `json:"blocker_buckets"`
	TopProjects    []DiagnosisBucket `json:"top_projects"`
}

// GetDiagnosis 经营诊断：近 30 天最新已分析卡的三个数字 + 阻碍八类分布 + 项目卡点。
func (s *Service) GetDiagnosis(ctx context.Context, tenantID int64) (*DiagnosisResponse, error) {
	rows, err := s.store.pool.Query(ctx, `
		SELECT result FROM (
			SELECT DISTINCT ON (recording_id) result, analyzed_at
			FROM deal_expert_analyses
			WHERE tenant_id = $1 AND status = 'analyzed' AND result IS NOT NULL
			  AND analyzed_at >= NOW() - INTERVAL '30 days'
			ORDER BY recording_id, id DESC
		) latest
	`, tenantID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	out := &DiagnosisResponse{WindowDays: 30, BlockerBuckets: []DiagnosisBucket{}, TopProjects: []DiagnosisBucket{}}
	buckets := map[string]int{}
	projects := map[string]int{}
	for rows.Next() {
		var raw []byte
		if err := rows.Scan(&raw); err != nil {
			return nil, err
		}
		var card map[string]any
		if json.Unmarshal(raw, &card) != nil {
			continue
		}
		out.Total++
		decision, _ := card["decision"].(map[string]any)
		state := ""
		if decision != nil {
			state, _ = decision["decision_state"].(string)
		}
		switch {
		case strings.Contains(state, "已付款") || strings.Contains(state, "已成交") || strings.Contains(state, "已预约") || strings.Contains(state, "已交定金"):
			out.Chosen++
		case strings.Contains(state, "明确不考虑") || strings.Contains(state, "已不选") || strings.Contains(state, "放弃"):
			out.NotChosen++
		default:
			out.Undecided++
		}
		if concerns, ok := decision["concerns"].([]any); ok {
			for _, rawConcern := range concerns {
				concern, _ := rawConcern.(map[string]any)
				concernType, _ := concern["type"].(string)
				buckets[BlockerBucket(concernType)]++
			}
		}
		if findings, ok := card["product_findings"].([]any); ok {
			for _, rawFinding := range findings {
				finding, _ := rawFinding.(map[string]any)
				name, _ := finding["product_name"].(string)
				if strings.TrimSpace(name) != "" {
					projects[strings.TrimSpace(name)]++
				}
			}
		}
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	for label, count := range buckets {
		out.BlockerBuckets = append(out.BlockerBuckets, DiagnosisBucket{Label: label, Count: count})
	}
	for label, count := range projects {
		out.TopProjects = append(out.TopProjects, DiagnosisBucket{Label: label, Count: count})
	}
	sort.Slice(out.BlockerBuckets, func(i, j int) bool { return out.BlockerBuckets[i].Count > out.BlockerBuckets[j].Count })
	sort.Slice(out.TopProjects, func(i, j int) bool { return out.TopProjects[i].Count > out.TopProjects[j].Count })
	if len(out.TopProjects) > 5 {
		out.TopProjects = out.TopProjects[:5]
	}
	return out, nil
}

func (h *Handler) GetDiagnosis(w http.ResponseWriter, r *http.Request) {
	scope, httpErr := h.resolveDealScope(r)
	if httpErr != nil {
		writeTenantError(w, httpErr)
		return
	}
	result, err := h.service.GetDiagnosis(r.Context(), scope.TenantID)
	if err != nil {
		writeServiceError(w, err)
		return
	}
	httputil.WriteSuccess(w, result)
}
