package patientops

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"sort"
	"strings"
	"time"

	"github.com/freeasyman/lingce-api/internal/middleware"
	"github.com/freeasyman/lingce-api/pkg/httputil"
)

// CustomerTimelineEvent 是客户详情页使用的统一历程事件。
// 只暴露聚合所需的最小字段；不同业务的原始数据保留在各自系统。
type CustomerTimelineEvent struct {
	ID          string `json:"id"`
	EventType   string `json:"event_type"`
	Scene       string `json:"scene"`
	OccurredAt  string `json:"occurred_at"`
	Title       string `json:"title"`
	Summary     string `json:"summary"`
	Source      string `json:"source"`
	Sensitivity string `json:"sensitivity"`
	Actor       string `json:"actor,omitempty"`
}

type CustomerTimelineResponse struct {
	Items    []CustomerTimelineEvent `json:"items"`
	Partial  bool                    `json:"partial"`
	Warnings []string                `json:"warnings,omitempty"`
}

// CustomerTimeline 聚合客户总监自有数据、录音、电子病历、随访任务和赢客专家接待。
// 不直接跨域读表：外部数据通过同一服务器的 JWT 业务接口读取，目标接口继续做租户/权限校验。
func (h *Handler) CustomerTimeline(w http.ResponseWriter, r *http.Request) {
	claims := middleware.GetUserClaims(r.Context())
	if claims == nil || claims.TenantID == nil || *claims.TenantID <= 0 {
		httputil.WriteUnauthorized(w, "invalid token")
		return
	}
	customerID, err := parseInt64Path(r, "id")
	if err != nil || customerID <= 0 {
		httputil.WriteBadRequest(w, "invalid customer id")
		return
	}

	ctx, cancel := context.WithTimeout(r.Context(), 8*time.Second)
	defer cancel()

	result := CustomerTimelineResponse{Items: []CustomerTimelineEvent{}}
	appendEvent := func(items ...CustomerTimelineEvent) {
		result.Items = append(result.Items, items...)
	}
	addWarning := func(source string, err error) {
		result.Partial = true
		result.Warnings = append(result.Warnings, fmt.Sprintf("%s暂时不可用", source))
	}

	// 客户总监自有数据。
	if sources, err := h.service.ListCustomerSources(ctx, *claims.TenantID, customerID); err != nil {
		addWarning("客户沟通来源", err)
	} else {
		for _, source := range sources {
			summary := source.Recognition.ConversationSummary
			if strings.TrimSpace(summary) == "" {
				summary = source.Recognition.PatientQuestion
			}
			scene := "沟通"
			if source.SourceType == "audio" || source.SourceType == "recording" {
				scene = "咨询"
			}
			appendEvent(CustomerTimelineEvent{
				ID: fmt.Sprintf("source-%d", source.ID), EventType: "source." + source.SourceType,
				Scene: scene, OccurredAt: source.CapturedAt,
				Title: firstNonEmpty(source.OriginalName, "沟通记录"), Summary: summary,
				Source: "客户总监", Sensitivity: "L4",
			})
		}
	}
	if records, err := h.service.ListOperationRecords(ctx, *claims.TenantID, customerID); err != nil {
		addWarning("客户操作记录", err)
	} else {
		for _, record := range records {
			sensitivity := "L4"
			title := firstNonEmpty(record.ActionType, "员工处理")
			if record.RecordType == "insight" {
				sensitivity = "L2"
				title = "智能判断 · 待确认"
			}
			appendEvent(CustomerTimelineEvent{
				ID: fmt.Sprintf("record-%d", record.ID), EventType: "record." + record.RecordType,
				Scene: "跟进", OccurredAt: record.CreatedAt, Title: title,
				Summary: firstNonEmpty(record.Content, record.Goal), Source: "客户总监", Sensitivity: sensitivity,
			})
		}
	}
	if tasks, err := h.service.ListCustomerTasks(ctx, *claims.TenantID, customerID); err != nil {
		addWarning("客户待办", err)
	} else {
		for _, task := range tasks {
			appendEvent(CustomerTimelineEvent{
				ID: fmt.Sprintf("task-%d", task.ID), EventType: "task." + task.TaskType,
				Scene: "待办", OccurredAt: firstNonEmpty(task.DueAt, task.CreatedAt),
				Title: firstNonEmpty(task.Goal, task.Action, "客户待办"), Summary: task.Content,
				Source: "客户总监", Sensitivity: "L1", Actor: task.OwnerName,
			})
		}
	}

	token := r.Header.Get("Authorization")
	base := requestBaseURL(r)
	role := h.employeeRole(ctx, *claims.TenantID, claims.UserID)

	// 已有客户来源里如果已经存在同一条录音，不再重复展示录音业务事件。
	sourceRecordingIDs := map[string]bool{}
	for _, source := range result.Items {
		if strings.HasPrefix(source.Title, "录音 #") {
			sourceRecordingIDs[strings.TrimPrefix(source.Title, "录音 #")] = true
		}
	}

	recordingEventIDs := map[string]bool{}
	// 录音：客户维度的咨询/就诊录音。
	if items, err := fetchTimelineItems(ctx, base+"/api/v1/recordings?customer_id="+fmt.Sprint(customerID)+"&page_size=100", token); err != nil {
		addWarning("录音", err)
	} else {
		for _, item := range items {
			recordingID := asString(item["id"])
			if sourceRecordingIDs[recordingID] {
				continue
			}
			occurred := firstNonEmpty(asString(item["recorded_at"]), asString(item["created_at"]))
			isDoctor := strings.EqualFold(asString(item["business_scope"]), "doctor")
			scene := "咨询"
			title := "咨询接待"
			actorLabel := "咨询师"
			if isDoctor {
				scene = "就诊"
				title = "医生沟通"
				actorLabel = "接诊"
			}
			// 只展示就诊过程元信息：谁接待、是否完成、录音是否归档。
			// 不展示转写、诊断、主诉等医疗内容。
			actor := firstNonEmpty(asString(item["employee_name"]), "待补充")
			department := strings.TrimSpace(asString(item["department_name"]))
			if department != "" {
				summary := fmt.Sprintf("%s：%s（%s） · 录音已归档", actorLabel, actor, department)
				if strings.EqualFold(asString(item["status"]), "completed") {
					summary += " · 已完成"
				}
				appendEvent(CustomerTimelineEvent{
					ID: fmt.Sprintf("recording-%s", recordingID), EventType: "recording.visit",
					Scene: scene, OccurredAt: occurred, Title: title,
					Summary: summary, Source: "录音归档", Sensitivity: "L2", Actor: actor,
				})
				recordingEventIDs[recordingID] = true
				continue
			}
			summary := fmt.Sprintf("%s：%s · 录音已归档", actorLabel, actor)
			if strings.EqualFold(asString(item["status"]), "completed") {
				summary += " · 已完成"
			}
			recordingEventIDs[recordingID] = true
			appendEvent(CustomerTimelineEvent{
				ID: fmt.Sprintf("recording-%s", recordingID), EventType: "recording.visit",
				Scene: scene, OccurredAt: occurred, Title: title,
				Summary: summary, Source: "录音归档", Sensitivity: "L2", Actor: actor,
			})
		}
	}

	// 电子病历：L2；默认只允许医生/主管/管理员/合规查看。
	if canViewMedical(role) {
		if items, err := fetchTimelineItems(ctx, base+"/api/v1/customers/"+fmt.Sprint(customerID)+"/emr-records?page_size=100", token); err != nil {
			addWarning("电子病历", err)
		} else {
			for _, item := range items {
				occurred := firstNonEmpty(asString(item["started_at"]), asString(item["created_at"]))
				title := visitTypeLabel(asString(item["visit_type"]))
				summary := "病历已归档"
				if status := strings.ToLower(strings.TrimSpace(asString(item["status"]))); status != "" && status != "archived" {
					summary = "病历状态：已记录"
				}
				appendEvent(CustomerTimelineEvent{
					ID: fmt.Sprintf("emr-%s", asString(item["id"])), EventType: "encounter.emr",
					Scene: "就诊", OccurredAt: occurred, Title: title, Summary: summary,
					Source: "电子病历", Sensitivity: "L2",
				})
			}
		}
	}

	// 随访任务：L1/L2，内容涉及医疗时按 L2 标注。
	if items, err := fetchTimelineItems(ctx, base+"/api/v1/recording-tasks?customer_id="+fmt.Sprint(customerID)+"&page_size=100", token); err != nil {
		addWarning("随访", err)
	} else {
		for _, item := range items {
			appendEvent(CustomerTimelineEvent{
				ID: fmt.Sprintf("followup-%s", asString(item["id"])), EventType: "followup.task",
				Scene: "随访", OccurredAt: firstNonEmpty(asString(item["due_at"]), asString(item["created_at"])),
				Title: firstNonEmpty(asString(item["title"]), "随访任务"), Summary: asString(item["description"]),
				Source: "随访顾问", Sensitivity: "L2", Actor: asString(item["assigned_to_name"]),
			})
		}
	}

	// 赢客专家接待：L4；成交金额不在摘要中展示，避免把 L3 混入 L4 时间线。
	if items, err := fetchTimelineItems(ctx, base+"/api/v1/deal-expert/encounters?customer_id="+fmt.Sprint(customerID)+"&page_size=100", token); err != nil {
		addWarning("赢客专家", err)
	} else {
		for _, item := range items {
			dealID := asString(item["id"])
			if recordingEventIDs[dealID] {
				continue
			}
			summary := firstNonEmpty(asString(item["effective_deal_status"]), asString(item["next_action"]), "已记录一次咨询")
			appendEvent(CustomerTimelineEvent{
				ID: fmt.Sprintf("deal-%s", dealID), EventType: "deal.encounter",
				Scene: "咨询", OccurredAt: asString(item["recorded_at"]), Title: "咨询接待",
				Summary: summary, Source: "赢客专家", Sensitivity: "L4", Actor: asString(item["employee_name"]),
			})
		}
	}

	sort.SliceStable(result.Items, func(i, j int) bool {
		left, leftErr := time.Parse(time.RFC3339, result.Items[i].OccurredAt)
		right, rightErr := time.Parse(time.RFC3339, result.Items[j].OccurredAt)
		if leftErr == nil && rightErr == nil {
			return left.After(right)
		}
		return result.Items[i].OccurredAt > result.Items[j].OccurredAt
	})
	if len(result.Items) > 200 {
		result.Items = result.Items[:200]
	}
	httputil.WriteSuccess(w, result)
}

func requestBaseURL(r *http.Request) string {
	scheme := "http"
	if r.TLS != nil || strings.EqualFold(r.Header.Get("X-Forwarded-Proto"), "https") {
		scheme = "https"
	}
	return scheme + "://" + r.Host
}

func (h *Handler) employeeRole(ctx context.Context, tenantID, employeeID int64) string {
	var role string
	_ = h.service.pool.QueryRow(ctx, `SELECT COALESCE(role,'') FROM employees WHERE id=$1 AND tenant_id=$2`, employeeID, tenantID).Scan(&role)
	return strings.ToLower(strings.TrimSpace(role))
}

func canViewMedical(role string) bool {
	switch role {
	case "admin", "doctor", "operating_manager", "manager", "compliance":
		return true
	default:
		return false
	}
}

func fetchTimelineItems(ctx context.Context, url, token string) ([]map[string]any, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Authorization", token)
	req.Header.Set("Accept", "application/json")
	client := &http.Client{Timeout: 6 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(io.LimitReader(resp.Body, 8<<20))
	if err != nil {
		return nil, err
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, fmt.Errorf("upstream status %d", resp.StatusCode)
	}
	var payload struct {
		Items []map[string]any `json:"items"`
		Data  struct {
			Items []map[string]any `json:"items"`
		} `json:"data"`
	}
	if err := json.Unmarshal(body, &payload); err != nil {
		return nil, err
	}
	if payload.Items != nil {
		return payload.Items, nil
	}
	return payload.Data.Items, nil
}

func visitTypeLabel(value string) string {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case "first_visit", "initial":
		return "初诊"
	case "return_visit", "revisit":
		return "复诊"
	case "follow_up", "followup":
		return "随访"
	default:
		return "诊疗"
	}
}

func nestedSummary(item map[string]any) string {
	for _, key := range []string{"analysis_display", "analysis_result"} {
		if value, ok := item[key].(map[string]any); ok {
			for _, textKey := range []string{"conversation_summary", "summary", "headline", "patient_question"} {
				if text := strings.TrimSpace(asString(value[textKey])); text != "" {
					return text
				}
			}
		}
	}
	return firstNonEmpty(asString(item["cleaned_transcription"]), "已接收一条录音")
}

func encounterSummary(item map[string]any) string {
	ctx, _ := item["encounter_context"].(map[string]any)
	if ctx == nil {
		return ""
	}
	return firstNonEmpty(asString(ctx["chief_complaint"]), asString(ctx["diagnosis"]))
}

func asString(value any) string {
	switch typed := value.(type) {
	case string:
		return typed
	case float64:
		return fmt.Sprintf("%.0f", typed)
	default:
		return ""
	}
}

func parseInt64Path(r *http.Request, key string) (int64, error) {
	raw := strings.TrimSpace(r.PathValue(key))
	var value int64
	if _, err := fmt.Sscan(raw, &value); err != nil {
		return 0, err
	}
	return value, nil
}
