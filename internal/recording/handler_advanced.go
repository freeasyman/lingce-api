package recording

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strconv"
	"time"

	"github.com/freeasyman/lingce-api/internal/middleware"
	"github.com/freeasyman/lingce-api/pkg/auth"
	"github.com/freeasyman/lingce-api/pkg/httputil"
)

// Advanced Recording Handlers

// GetStatsByTenant handles getting statistics by tenant
func (h *Handler) GetStatsByTenant(w http.ResponseWriter, r *http.Request) {
	claims := middleware.GetUserClaims(r.Context())
	if claims == nil {
		httputil.WriteUnauthorized(w, "Invalid token")
		return
	}

	if claims.UserType == auth.UserTypeAdmin {
		rows, err := h.service.store.pool.Query(r.Context(), `
			SELECT t.id, t.name, COUNT(mr.id), COALESCE(SUM(mr.duration), 0)
			FROM tenants t
			LEFT JOIN recordings mr ON mr.tenant_id = t.id
			GROUP BY t.id, t.name
			ORDER BY COUNT(mr.id) DESC
		`)
		if err != nil {
			httputil.WriteInternalError(w, err.Error())
			return
		}
		defer rows.Close()
		var items []RecordingStatsByTenantResponse
		for rows.Next() {
			var item RecordingStatsByTenantResponse
			if err := rows.Scan(&item.TenantID, &item.TenantName, &item.Count, &item.Duration); err != nil {
				httputil.WriteInternalError(w, err.Error())
				return
			}
			items = append(items, item)
		}
		httputil.WriteSuccess(w, items)
		return
	}

	tenantID, err := getTaskTenantIDFromClaimsOrQuery(claims, r)
	if err != nil {
		httputil.WriteBadRequest(w, err.Error())
		return
	}
	stats, err := h.service.store.GetStatsOverview(r.Context(), tenantID, nil, nil)
	if err != nil {
		httputil.WriteInternalError(w, err.Error())
		return
	}
	httputil.WriteSuccess(w, []RecordingStatsByTenantResponse{{
		TenantID: tenantID,
		Count:    stats.TotalRecordings,
		Duration: stats.TotalDuration,
	}})
}

// GetDurationDistribution handles getting duration distribution
func (h *Handler) GetDurationDistribution(w http.ResponseWriter, r *http.Request) {
	claims := middleware.GetUserClaims(r.Context())
	if claims == nil {
		httputil.WriteUnauthorized(w, "Invalid token")
		return
	}

	tenantID, err := getTaskTenantIDFromClaimsOrQuery(claims, r)
	if err != nil {
		httputil.WriteBadRequest(w, err.Error())
		return
	}
	rows, err := h.service.store.pool.Query(r.Context(), `
		SELECT
			CASE
				WHEN COALESCE(duration, 0) < 300 THEN '0-5min'
				WHEN COALESCE(duration, 0) < 600 THEN '5-10min'
				WHEN COALESCE(duration, 0) < 1200 THEN '10-20min'
				ELSE '20min+'
			END AS duration_range,
			COUNT(*) AS cnt
		FROM recordings
		WHERE tenant_id = $1
		GROUP BY duration_range
		ORDER BY cnt DESC
	`, tenantID)
	if err != nil {
		httputil.WriteInternalError(w, err.Error())
		return
	}
	defer rows.Close()

	var (
		total int64
		raw   []DurationDistributionResponse
	)
	for rows.Next() {
		var item DurationDistributionResponse
		if err := rows.Scan(&item.Range, &item.Count); err != nil {
			httputil.WriteInternalError(w, err.Error())
			return
		}
		total += item.Count
		raw = append(raw, item)
	}
	for i := range raw {
		if total > 0 {
			raw[i].Percentage = float64(raw[i].Count) / float64(total) * 100
		}
	}
	httputil.WriteSuccess(w, raw)
}

// GetDailyStats handles getting daily statistics
func (h *Handler) GetDailyStats(w http.ResponseWriter, r *http.Request) {
	claims := middleware.GetUserClaims(r.Context())
	if claims == nil {
		httputil.WriteUnauthorized(w, "Invalid token")
		return
	}

	tenantID, err := getTaskTenantIDFromClaimsOrQuery(claims, r)
	if err != nil {
		httputil.WriteBadRequest(w, err.Error())
		return
	}
	rows, err := h.service.store.pool.Query(r.Context(), `
		SELECT DATE(created_at), COUNT(*), COALESCE(SUM(duration), 0)
		FROM recordings
		WHERE tenant_id = $1 AND created_at >= NOW() - INTERVAL '30 days'
		GROUP BY DATE(created_at)
		ORDER BY DATE(created_at) ASC
	`, tenantID)
	if err != nil {
		httputil.WriteInternalError(w, err.Error())
		return
	}
	defer rows.Close()
	var items []DailyStatsResponse
	for rows.Next() {
		var (
			dt   time.Time
			item DailyStatsResponse
		)
		if err := rows.Scan(&dt, &item.Count, &item.Duration); err != nil {
			httputil.WriteInternalError(w, err.Error())
			return
		}
		item.Date = dt.Format("2006-01-02")
		items = append(items, item)
	}
	httputil.WriteSuccess(w, items)
}

// UploadRecording handles uploading recording
func (h *Handler) UploadRecording(w http.ResponseWriter, r *http.Request) {
	claims := middleware.GetUserClaims(r.Context())
	if claims == nil {
		httputil.WriteUnauthorized(w, "Invalid token")
		return
	}

	var req CreateRecordingRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		httputil.WriteBadRequest(w, "Invalid request body")
		return
	}
	if claims.UserType != auth.UserTypeAdmin {
		if claims.TenantID == nil {
			httputil.WriteForbidden(w, "No tenant access")
			return
		}
		req.TenantID = *claims.TenantID
		req.EmployeeID = claims.UserID
	}
	if req.RecordingURL == "" {
		httputil.WriteBadRequest(w, "recording_url is required")
		return
	}
	rec, err := h.service.CreateRecording(r.Context(), req)
	if err != nil {
		httputil.WriteInternalError(w, err.Error())
		return
	}
	httputil.WriteSuccess(w, rec)
}

// GetPlayURL handles getting play URL
func (h *Handler) GetPlayURL(w http.ResponseWriter, r *http.Request) {
	claims := middleware.GetUserClaims(r.Context())
	if claims == nil {
		httputil.WriteUnauthorized(w, "Invalid token")
		return
	}

	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil {
		httputil.WriteBadRequest(w, "Invalid recording ID")
		return
	}

	rec, err := h.service.GetRecording(r.Context(), id)
	if err != nil {
		httputil.WriteNotFound(w, err.Error())
		return
	}
	httputil.WriteSuccess(w, PlayURLResponse{
		URL:       rec.RecordingURL,
		ExpiresAt: time.Now().Add(1 * time.Hour).Format(time.RFC3339),
	})
}

// TestPlayback handles testing playback
func (h *Handler) TestPlayback(w http.ResponseWriter, r *http.Request) {
	claims := middleware.GetUserClaims(r.Context())
	if claims == nil {
		httputil.WriteUnauthorized(w, "Invalid token")
		return
	}

	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil {
		httputil.WriteBadRequest(w, "Invalid recording ID")
		return
	}

	rec, err := h.service.GetRecording(r.Context(), id)
	if err != nil {
		httputil.WriteNotFound(w, err.Error())
		return
	}
	status := "ok"
	if rec.RecordingURL == "" {
		status = "invalid_url"
	}
	httputil.WriteSuccess(w, map[string]interface{}{
		"status": status,
		"url":    rec.RecordingURL,
	})
}

// TriggerTranscribe handles triggering transcription
func (h *Handler) TriggerTranscribe(w http.ResponseWriter, r *http.Request) {
	claims := middleware.GetUserClaims(r.Context())
	if claims == nil {
		httputil.WriteUnauthorized(w, "Invalid token")
		return
	}

	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil {
		httputil.WriteBadRequest(w, "Invalid recording ID")
		return
	}

	var req TriggerTranscribeRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil && err.Error() != "EOF" {
		httputil.WriteBadRequest(w, "Invalid request body")
		return
	}

	jobID, err := h.service.TriggerTranscribe(r.Context(), id, req)
	if err != nil {
		httputil.WriteInternalError(w, err.Error())
		return
	}

	httputil.WriteSuccess(w, map[string]string{
		"message": "Transcription triggered",
		"job_id":  jobID,
	})
}

// TriggerAnalyze handles triggering analysis
func (h *Handler) TriggerAnalyze(w http.ResponseWriter, r *http.Request) {
	claims := middleware.GetUserClaims(r.Context())
	if claims == nil {
		httputil.WriteUnauthorized(w, "Invalid token")
		return
	}

	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil {
		httputil.WriteBadRequest(w, "Invalid recording ID")
		return
	}

	var req TriggerAnalyzeRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil && err.Error() != "EOF" {
		httputil.WriteBadRequest(w, "Invalid request body")
		return
	}

	jobID, err := h.service.TriggerAnalyze(r.Context(), id, req)
	if err != nil {
		httputil.WriteInternalError(w, err.Error())
		return
	}

	httputil.WriteSuccess(w, map[string]string{
		"message": "Analysis triggered",
		"job_id":  jobID,
	})
}

// TriggerClean handles triggering cleaning
func (h *Handler) TriggerClean(w http.ResponseWriter, r *http.Request) {
	claims := middleware.GetUserClaims(r.Context())
	if claims == nil {
		httputil.WriteUnauthorized(w, "Invalid token")
		return
	}

	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil {
		httputil.WriteBadRequest(w, "Invalid recording ID")
		return
	}

	var req TriggerCleanRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil && err.Error() != "EOF" {
		httputil.WriteBadRequest(w, "Invalid request body")
		return
	}

	jobID, err := h.service.TriggerClean(r.Context(), id, req)
	if err != nil {
		httputil.WriteInternalError(w, err.Error())
		return
	}

	httputil.WriteSuccess(w, map[string]string{
		"message": "Cleaning triggered",
		"job_id":  jobID,
	})
}

// GetAnalysisResult handles getting analysis result
func (h *Handler) GetAnalysisResult(w http.ResponseWriter, r *http.Request) {
	claims := middleware.GetUserClaims(r.Context())
	if claims == nil {
		httputil.WriteUnauthorized(w, "Invalid token")
		return
	}

	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil {
		httputil.WriteBadRequest(w, "Invalid recording ID")
		return
	}

	result, err := h.service.GetAnalysisResult(r.Context(), id)
	if err != nil {
		httputil.WriteNotFound(w, err.Error())
		return
	}

	httputil.WriteSuccess(w, result)
}

// DispatchFollowUpTasks handles dispatching follow-up tasks
func (h *Handler) DispatchFollowUpTasks(w http.ResponseWriter, r *http.Request) {
	claims := middleware.GetUserClaims(r.Context())
	if claims == nil {
		httputil.WriteUnauthorized(w, "Invalid token")
		return
	}

	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil {
		httputil.WriteBadRequest(w, "Invalid recording ID")
		return
	}

	rec, err := h.service.store.GetRecordingByID(r.Context(), id)
	if err != nil {
		httputil.WriteNotFound(w, err.Error())
		return
	}
	title := "录音跟进任务"
	desc := "请基于录音分析结果执行后续跟进"
	_, err = h.service.store.pool.Exec(r.Context(), `
		INSERT INTO recording_tasks (
			tenant_id, recording_id, customer_id, customer_name, customer_phone,
			title, description, assigned_to, assigned_by, status, priority, due_at, source_type, created_at, updated_at
		)
		SELECT
			r.tenant_id,
			r.id,
			COALESCE(r.customer_id, 0),
			COALESCE(c.name, ''),
			COALESCE(c.phone, ''),
			$2,
			$3,
			r.employee_id,
			$4::text,
			'pending',
			'medium',
			NOW() + INTERVAL '24 hours',
			'manual',
			NOW(),
			NOW()
		FROM recordings r
		LEFT JOIN customers c ON c.id = r.customer_id
		WHERE r.id = $1
	`, rec.ID, title, desc, claims.UserID)
	if err != nil {
		httputil.WriteInternalError(w, err.Error())
		return
	}
	httputil.WriteSuccess(w, map[string]string{"message": "Follow-up tasks dispatched"})
}

// SubmitAnalysisFeedback handles submitting analysis feedback
func (h *Handler) SubmitAnalysisFeedback(w http.ResponseWriter, r *http.Request) {
	claims := middleware.GetUserClaims(r.Context())
	if claims == nil {
		httputil.WriteUnauthorized(w, "Invalid token")
		return
	}

	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil {
		httputil.WriteBadRequest(w, "Invalid recording ID")
		return
	}

	var req AnalysisFeedbackRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		httputil.WriteBadRequest(w, "Invalid request body")
		return
	}
	comment := ""
	if req.Comment != nil {
		comment = *req.Comment
	}
	msg := "analysis feedback"
	if comment != "" {
		msg = msg + ": " + comment
	}
	processingError := msg
	if _, err := h.service.store.UpdateRecording(r.Context(), id, UpdateRecordingRequest{
		ProcessingError: &processingError,
	}); err != nil {
		httputil.WriteInternalError(w, err.Error())
		return
	}
	httputil.WriteSuccess(w, map[string]string{"message": "Feedback submitted"})
}

// BatchTranscribe handles batch transcription
func (h *Handler) BatchTranscribe(w http.ResponseWriter, r *http.Request) {
	claims := middleware.GetUserClaims(r.Context())
	if claims == nil {
		httputil.WriteUnauthorized(w, "Invalid token")
		return
	}

	var req BatchTranscribeRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		httputil.WriteBadRequest(w, "Invalid request body")
		return
	}
	if len(req.RecordingIDs) == 0 {
		httputil.WriteBadRequest(w, "recording_ids is required")
		return
	}
	results := map[string]interface{}{
		"success": 0,
		"failed":  0,
		"jobs":    map[int64]string{},
	}
	jobs := results["jobs"].(map[int64]string)
	for _, rid := range req.RecordingIDs {
		jobID, err := h.service.TriggerTranscribe(r.Context(), rid, TriggerTranscribeRequest{})
		if err != nil {
			results["failed"] = results["failed"].(int) + 1
			continue
		}
		results["success"] = results["success"].(int) + 1
		jobs[rid] = jobID
	}
	httputil.WriteSuccess(w, results)
}

// BatchDelete handles batch deletion
func (h *Handler) BatchDelete(w http.ResponseWriter, r *http.Request) {
	claims := middleware.GetUserClaims(r.Context())
	if claims == nil {
		httputil.WriteUnauthorized(w, "Invalid token")
		return
	}

	var req BatchDeleteRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		httputil.WriteBadRequest(w, "Invalid request body")
		return
	}
	if len(req.RecordingIDs) == 0 {
		httputil.WriteBadRequest(w, "recording_ids is required")
		return
	}
	var success, failed int
	for _, rid := range req.RecordingIDs {
		if err := h.service.DeleteRecording(r.Context(), rid); err != nil {
			failed++
			continue
		}
		success++
	}
	httputil.WriteSuccess(w, map[string]int{"success": success, "failed": failed})
}

// GetLearningRecommendation handles getting learning recommendation
func (h *Handler) GetLearningRecommendation(w http.ResponseWriter, r *http.Request) {
	claims := middleware.GetUserClaims(r.Context())
	if claims == nil {
		httputil.WriteUnauthorized(w, "Invalid token")
		return
	}

	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil {
		httputil.WriteBadRequest(w, "Invalid recording ID")
		return
	}

	result, err := h.service.GetAnalysisResult(r.Context(), id)
	if err != nil {
		httputil.WriteNotFound(w, err.Error())
		return
	}
	recs := []RecommendationItem{
		{
			Title:       "复盘关键对话片段",
			Description: "聚焦高价值沟通节点，形成标准话术。",
			Priority:    "high",
		},
	}
	if result.DoctorSummary != nil {
		recs = append(recs, RecommendationItem{
			Title:       "结合医生总结优化提问顺序",
			Description: *result.DoctorSummary,
			Priority:    "medium",
		})
	}
	httputil.WriteSuccess(w, LearningRecommendationResponse{Recommendations: recs})
}

// ConfirmFollowUpAction handles confirming follow-up action
func (h *Handler) ConfirmFollowUpAction(w http.ResponseWriter, r *http.Request) {
	claims := middleware.GetUserClaims(r.Context())
	if claims == nil {
		httputil.WriteUnauthorized(w, "Invalid token")
		return
	}

	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil {
		httputil.WriteBadRequest(w, "Invalid recording ID")
		return
	}

	var req ConfirmActionRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		httputil.WriteBadRequest(w, "Invalid request body")
		return
	}
	if req.Action == "" {
		httputil.WriteBadRequest(w, "action is required")
		return
	}
	note := req.Action
	if req.Notes != nil {
		note = note + ": " + *req.Notes
	}
	result, err := h.service.store.pool.Exec(r.Context(), `
		UPDATE recording_tasks
		SET status = 'completed', completed_at = NOW(), feedback = CONCAT(COALESCE(feedback, ''), CASE WHEN COALESCE(feedback, '') = '' THEN '' ELSE E'\n' END, 'completed_by=', $1::text, E'\n', 'action_note=', $2), updated_at = NOW()
		WHERE recording_id = $3 AND source_type = 'follow_up' AND status IN ('pending', 'assigned')
	`, claims.UserID, note, id)
	if err != nil {
		httputil.WriteInternalError(w, err.Error())
		return
	}
	if result.RowsAffected() == 0 {
		httputil.WriteNotFound(w, "No pending follow-up tasks found for this recording")
		return
	}
	httputil.WriteSuccess(w, map[string]string{"message": "Follow-up action confirmed"})
}

// GenerateOpeningScript handles generating opening script
func (h *Handler) GenerateOpeningScript(w http.ResponseWriter, r *http.Request) {
	claims := middleware.GetUserClaims(r.Context())
	if claims == nil {
		httputil.WriteUnauthorized(w, "Invalid token")
		return
	}

	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil {
		httputil.WriteBadRequest(w, "Invalid recording ID")
		return
	}

	rec, err := h.service.GetRecording(r.Context(), id)
	if err != nil {
		httputil.WriteNotFound(w, err.Error())
		return
	}
	script := "您好，我是灵策助理，今天我们围绕您的情况做一个简短沟通。"
	if rec.PatientName != "" {
		script = "您好，" + rec.PatientName + "，我是灵策助理，今天我们围绕您的情况做一个简短沟通。"
	}
	httputil.WriteSuccess(w, map[string]string{"script": script})
}

// GenerateOperationsPlan handles generating operations plan
func (h *Handler) GenerateOperationsPlan(w http.ResponseWriter, r *http.Request) {
	claims := middleware.GetUserClaims(r.Context())
	if claims == nil {
		httputil.WriteUnauthorized(w, "Invalid token")
		return
	}

	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil {
		httputil.WriteBadRequest(w, "Invalid recording ID")
		return
	}

	rec, err := h.service.store.GetRecordingByID(r.Context(), id)
	if err != nil {
		httputil.WriteNotFound(w, err.Error())
		return
	}

	if claims.UserType != auth.UserTypeAdmin {
		if claims.TenantID == nil || *claims.TenantID != rec.TenantID {
			httputil.WriteForbidden(w, "Access denied")
			return
		}
	}

	var req GenerateOperationsPlanRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil && err.Error() != "EOF" {
		httputil.WriteBadRequest(w, "Invalid request body")
		return
	}

	requestPayload := map[string]interface{}{
		"recording_id": rec.ID,
		"tenant_id":    rec.TenantID,
		"context":      req.Context,
	}
	resultPayload := map[string]interface{}{
		"summary":  fmt.Sprintf("针对录音 %d 生成运营计划", rec.ID),
		"priority": "medium",
		"actions": []map[string]interface{}{
			{
				"title":    "24小时内完成首轮触达",
				"owner_id": rec.EmployeeID,
				"due_in":   "24h",
			},
			{
				"title":    "48小时内复盘转化障碍",
				"owner_id": rec.EmployeeID,
				"due_in":   "48h",
			},
		},
	}
	requestPayloadJSON, _ := json.Marshal(requestPayload)
	resultPayloadJSON, _ := json.Marshal(resultPayload)
	idempotencyKey := r.Header.Get("Idempotency-Key")

	var jobID int64
	if err := h.service.store.pool.QueryRow(r.Context(), `
		INSERT INTO recording_operations_plan_jobs (
			recording_id, tenant_id, requested_by, status, request_payload, result_payload,
			idempotency_key, started_at, completed_at, created_at, updated_at
		)
		VALUES ($1, $2, $3, 'completed', $4::jsonb, $5::jsonb, NULLIF($6, ''), NOW(), NOW(), NOW(), NOW())
		RETURNING id
	`, rec.ID, rec.TenantID, claims.UserID, string(requestPayloadJSON), string(resultPayloadJSON), idempotencyKey).Scan(&jobID); err != nil {
		httputil.WriteInternalError(w, err.Error())
		return
	}

	httputil.WriteSuccess(w, map[string]string{"job_id": strconv.FormatInt(jobID, 10)})
}

// GetOperationsPlanJobStatus handles getting operations plan job status
func (h *Handler) GetOperationsPlanJobStatus(w http.ResponseWriter, r *http.Request) {
	claims := middleware.GetUserClaims(r.Context())
	if claims == nil {
		httputil.WriteUnauthorized(w, "Invalid token")
		return
	}

	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil {
		httputil.WriteBadRequest(w, "Invalid recording ID")
		return
	}

	jobID := r.PathValue("job_id")
	if jobID == "" {
		httputil.WriteBadRequest(w, "Job ID is required")
		return
	}

	jobIDInt, err := strconv.ParseInt(jobID, 10, 64)
	if err != nil {
		httputil.WriteBadRequest(w, "Invalid job ID")
		return
	}

	rec, err := h.service.store.GetRecordingByID(r.Context(), id)
	if err != nil {
		httputil.WriteNotFound(w, err.Error())
		return
	}
	if claims.UserType != auth.UserTypeAdmin {
		if claims.TenantID == nil || *claims.TenantID != rec.TenantID {
			httputil.WriteForbidden(w, "Access denied")
			return
		}
	}

	var (
		status       string
		resultJSON   []byte
		errorMessage *string
		updatedAt    time.Time
	)
	err = h.service.store.pool.QueryRow(r.Context(), `
		SELECT status, COALESCE(result_payload::text, '{}')::jsonb, error_message, updated_at
		FROM recording_operations_plan_jobs
		WHERE id = $1 AND recording_id = $2
	`, jobIDInt, id).Scan(&status, &resultJSON, &errorMessage, &updatedAt)
	if err != nil {
		httputil.WriteNotFound(w, "Operations plan job not found")
		return
	}

	var result interface{}
	_ = json.Unmarshal(resultJSON, &result)
	httputil.WriteSuccess(w, map[string]interface{}{
		"job_id":  jobID,
		"status":  status,
		"result":  result,
		"error":   errorMessage,
		"updated": updatedAt.Format(time.RFC3339),
	})
}

// GetRecordingTasks handles getting recording tasks
func (h *Handler) GetRecordingTasks(w http.ResponseWriter, r *http.Request) {
	claims := middleware.GetUserClaims(r.Context())
	if claims == nil {
		httputil.WriteUnauthorized(w, "Invalid token")
		return
	}

	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil {
		httputil.WriteBadRequest(w, "Invalid recording ID")
		return
	}

	tenantID, err := getTaskTenantIDFromClaimsOrQuery(claims, r)
	if err != nil {
		httputil.WriteBadRequest(w, err.Error())
		return
	}

	req := TaskListRequest{
		TenantID:    &tenantID,
		RecordingID: &id,
		Page:        1,
		PageSize:    100,
	}
	tasks, _, err := h.service.ListRecordingTasks(r.Context(), req)
	if err != nil {
		httputil.WriteInternalError(w, err.Error())
		return
	}

	httputil.WriteSuccess(w, tasks)
}
