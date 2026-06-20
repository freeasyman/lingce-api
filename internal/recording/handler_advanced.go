package recording

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/freeasyman/lingce-api/internal/middleware"
	"github.com/freeasyman/lingce-api/pkg/auth"
	"github.com/freeasyman/lingce-api/pkg/httputil"
	ossutil "github.com/freeasyman/lingce-api/pkg/oss"
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

	ref, err := h.service.store.GetRecordingMediaRef(r.Context(), id)
	if err != nil {
		httputil.WriteNotFound(w, err.Error())
		return
	}
	if claims.UserType != auth.UserTypeAdmin {
		recording, recErr := h.service.GetRecording(r.Context(), id)
		if recErr != nil {
			httputil.WriteNotFound(w, recErr.Error())
			return
		}
		if claims.TenantID == nil || *claims.TenantID != recording.TenantID {
			httputil.WriteForbidden(w, "Access denied")
			return
		}
		if err := h.service.ValidateBusinessScopeAccess(r.Context(), claims.UserType, claims.UserID, recording.BusinessScope); err != nil {
			httputil.WriteForbidden(w, err.Error())
			return
		}
	}
	ossEndpoint := strings.TrimSpace(h.ossConfig.Endpoint)
	ossBucket := strings.TrimSpace(h.ossConfig.Bucket)
	ossAccessKeyID := strings.TrimSpace(h.ossConfig.AccessKeyID)
	ossAccessKeySecret := strings.TrimSpace(h.ossConfig.AccessKeySecret)
	requireOwned := h.playURLRequireOwned

	if strings.TrimSpace(ref.OSSKey) == "" && strings.TrimSpace(ref.FileURL) != "" && ossEndpoint != "" && ossBucket != "" && ossAccessKeyID != "" && ossAccessKeySecret != "" {
		refreshed, refreshErr := h.migrateRecordingMediaToOwnedStorage(r.Context(), ref)
		if refreshErr == nil && refreshed != nil {
			ref = refreshed
		}
	}
	if strings.TrimSpace(ref.OSSKey) != "" && ossEndpoint != "" && ossBucket != "" && ossAccessKeyID != "" && ossAccessKeySecret != "" {
		client, cErr := ossutil.NewClient(ossEndpoint, ossAccessKeyID, ossAccessKeySecret, ossBucket, strings.TrimSpace(h.ossConfig.PublicBaseURL))
		if cErr != nil {
			httputil.WriteInternalError(w, "failed to init oss client")
			return
		}
		signedURL, sErr := client.GetSignedURL(ref.OSSKey, 3600)
		if sErr != nil {
			httputil.WriteInternalError(w, "failed to sign play url")
			return
		}
		httputil.WriteSuccess(w, PlayURLResponse{
			URL:       signedURL,
			ExpiresAt: time.Now().Add(1 * time.Hour).Format(time.RFC3339),
		})
		return
	}
	if requireOwned {
		httputil.WriteError(w, http.StatusPreconditionFailed, "MEDIA_NOT_MIGRATED", "recording media has not been migrated to owned storage", map[string]any{"recording_id": id})
		return
	}
	if strings.TrimSpace(ref.FileURL) == "" {
		httputil.WriteError(w, http.StatusNotFound, "PLAY_URL_NOT_FOUND", "recording file url is empty", map[string]any{"recording_id": id})
		return
	}
	httputil.WriteSuccess(w, PlayURLResponse{
		URL:       ref.FileURL,
		ExpiresAt: time.Now().Add(1 * time.Hour).Format(time.RFC3339),
	})
}

func (h *Handler) migrateRecordingMediaToOwnedStorage(ctx context.Context, ref *RecordingMediaRef) (*RecordingMediaRef, error) {
	if ref == nil {
		return nil, fmt.Errorf("recording media ref is nil")
	}
	sourceURL := strings.TrimSpace(ref.FileURL)
	if sourceURL == "" {
		return nil, fmt.Errorf("source url is empty")
	}
	clientOSS, err := ossutil.NewClient(
		strings.TrimSpace(h.ossConfig.Endpoint),
		strings.TrimSpace(h.ossConfig.AccessKeyID),
		strings.TrimSpace(h.ossConfig.AccessKeySecret),
		strings.TrimSpace(h.ossConfig.Bucket),
		strings.TrimSpace(h.ossConfig.PublicBaseURL),
	)
	if err != nil {
		return nil, fmt.Errorf("init oss client: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, sourceURL, nil)
	if err != nil {
		return nil, fmt.Errorf("build source request: %w", err)
	}
	resp, err := (&http.Client{Timeout: 45 * time.Second}).Do(req)
	if err != nil {
		return nil, fmt.Errorf("download source audio: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, fmt.Errorf("download source audio status=%d", resp.StatusCode)
	}
	maxBytes := int64(150 * 1024 * 1024)
	body, err := io.ReadAll(io.LimitReader(resp.Body, maxBytes+1))
	if err != nil {
		return nil, fmt.Errorf("read source audio: %w", err)
	}
	if int64(len(body)) > maxBytes {
		return nil, fmt.Errorf("source audio exceeds max bytes")
	}

	ext := strings.ToLower(strings.TrimSpace(filepath.Ext(ref.FileName)))
	if ext == "" {
		ext = ".mp3"
	}
	ossKey := ossutil.GenerateObjectKey(fmt.Sprintf("recordings/%d/%s", ref.TenantID, time.Now().UTC().Format("2006/01/02")), ext)
	ownedURL, err := clientOSS.UploadBytes(ctx, ossKey, body, &ossutil.UploadOptions{
		ContentType: "audio/mpeg",
		Metadata: map[string]string{
			"source-recording-id": strconv.FormatInt(ref.RecordingID, 10),
			"source-url":          sourceURL,
		},
	})
	if err != nil {
		return nil, fmt.Errorf("upload source audio to oss: %w", err)
	}
	if err := h.service.store.UpdateRecordingMediaRef(ctx, ref.RecordingID, ownedURL, ossKey); err != nil {
		return nil, err
	}
	ref.FileURL = ownedURL
	ref.OSSKey = ossKey
	return ref, nil
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

// ReanalyzeRecording triggers re-analysis for a recording resource.
func (h *Handler) ReanalyzeRecording(w http.ResponseWriter, r *http.Request) {
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

	jobID, err := h.service.ReanalyzeRecording(r.Context(), id)
	if err != nil {
		if IsRecordingValidationError(err) {
			code := "BAD_REQUEST"
			var validationErr interface{ Code() string }
			if errors.As(err, &validationErr) {
				code = validationErr.Code()
			}
			httputil.WriteError(w, http.StatusBadRequest, code, err.Error(), map[string]any{"recording_id": id})
			return
		}
		if IsWorkerUnavailable(err) {
			httputil.WriteError(w, http.StatusServiceUnavailable, "WORKER_UNAVAILABLE", "recording worker unavailable, please retry", nil)
			return
		}
		httputil.WriteInternalError(w, err.Error())
		return
	}

	httputil.WriteSuccess(w, map[string]string{
		"message": "Reanalysis triggered",
		"job_id":  jobID,
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
		if IsWorkerUnavailable(err) {
			httputil.WriteError(w, http.StatusServiceUnavailable, "WORKER_UNAVAILABLE", "recording worker unavailable, please retry", nil)
			return
		}
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
	requestPayloadJSON, _ := json.Marshal(requestPayload)
	idempotencyKey := r.Header.Get("Idempotency-Key")

	var jobID int64
	if err := h.service.store.pool.QueryRow(r.Context(), `
		INSERT INTO recording_operations_plan_jobs (
			recording_id, tenant_id, requested_by, status, request_payload, result_payload,
			idempotency_key, started_at, completed_at, created_at, updated_at
		)
		VALUES ($1, $2, $3, 'pending', $4::jsonb, NULL, NULLIF($5, ''), NULL, NULL, NOW(), NOW())
		RETURNING id
	`, rec.ID, rec.TenantID, claims.UserID, string(requestPayloadJSON), idempotencyKey).Scan(&jobID); err != nil {
		httputil.WriteInternalError(w, err.Error())
		return
	}
	if _, err := h.service.submitLingceWorkerJob(r.Context(), rec.ID, "ops_plan", "manual_ops_plan"); err != nil {
		_, _ = h.service.store.pool.Exec(r.Context(), `
			UPDATE recording_operations_plan_jobs
			SET status = 'failed', error_message = $2, updated_at = NOW()
			WHERE id = $1
		`, jobID, err.Error())
		if IsWorkerUnavailable(err) {
			httputil.WriteError(w, http.StatusServiceUnavailable, "WORKER_UNAVAILABLE", "recording worker unavailable, please retry", nil)
			return
		}
		if IsRecordingValidationError(err) {
			httputil.WriteError(w, http.StatusBadRequest, "OPS_PLAN_REJECTED", err.Error(), nil)
			return
		}
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

	if status != "completed" {
		var operationsPlanJSON []byte
		checkErr := h.service.store.pool.QueryRow(r.Context(), `
			SELECT COALESCE(analysis_result->'operations_plan', '{}'::jsonb)::text::jsonb
			FROM recordings
			WHERE id = $1
			  AND analysis_result IS NOT NULL
			  AND analysis_result ? 'operations_plan'
		`, id).Scan(&operationsPlanJSON)
		if checkErr == nil && len(operationsPlanJSON) > 0 {
			_, _ = h.service.store.pool.Exec(r.Context(), `
				UPDATE recording_operations_plan_jobs
				SET status = 'completed',
				    result_payload = $2::jsonb,
				    completed_at = NOW(),
				    updated_at = NOW()
				WHERE id = $1
			`, jobIDInt, string(operationsPlanJSON))
			status = "completed"
			resultJSON = operationsPlanJSON
			errorMessage = nil
			updatedAt = time.Now()
		}
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
