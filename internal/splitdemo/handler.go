package splitdemo

import (
	"context"
	"embed"
	"encoding/json"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/freeasyman/lingce-api/pkg/httputil"
)

//go:embed static/index.html
var splitDemoFS embed.FS

type Handler struct {
	service *Service
	jobs    *JobManager
	page    []byte
}

func NewHandler(service *Service) *Handler {
	page, _ := splitDemoFS.ReadFile("static/index.html")
	return &Handler{service: service, jobs: NewJobManager(), page: page}
}

func (h *Handler) RegisterRoutes(mux *http.ServeMux) {
	mux.HandleFunc("GET /internal/split-demo", h.ServePage)
	mux.HandleFunc("GET /internal/split-demo/recordings", h.ListRecordings)
	mux.HandleFunc("GET /internal/split-demo/recordings/{id}", h.GetRecording)
	mux.HandleFunc("POST /internal/split-demo/split", h.SplitRecording)
	mux.HandleFunc("GET /internal/split-demo/split-jobs/{id}", h.GetSplitJob)
	mux.HandleFunc("POST /internal/split-demo/save-annotation", h.SaveAnnotation)
	mux.HandleFunc("GET /internal/split-demo/annotation-overview", h.AnnotationOverview)
	mux.HandleFunc("GET /internal/split-demo/synthetic/candidates", h.ListSyntheticCandidates)
	mux.HandleFunc("POST /internal/split-demo/synthetic/preview", h.PreviewSyntheticCase)
	mux.HandleFunc("POST /internal/split-demo/synthetic/cases", h.SaveSyntheticCase)
	mux.HandleFunc("GET /internal/split-demo/synthetic/cases", h.ListSyntheticCases)
	mux.HandleFunc("GET /internal/split-demo/synthetic/cases/{id}", h.GetSyntheticCase)
	mux.HandleFunc("DELETE /internal/split-demo/synthetic/cases/{id}", h.DeleteSyntheticCase)
	mux.HandleFunc("POST /internal/split-demo/synthetic/run", h.RunSyntheticCase)
	mux.HandleFunc("GET /internal/split-demo/synthetic/runs/{id}", h.GetSyntheticRun)
	mux.HandleFunc("POST /internal/split-demo/encounter-summary", h.SummarizeEncounter)
}

func (h *Handler) ServePage(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	_, _ = w.Write(h.page)
}

func (h *Handler) ListRecordings(w http.ResponseWriter, r *http.Request) {
	tenantID := parseInt64Query(r, "tenant_id", 1)
	minDuration := parseIntQuery(r, "min_duration_seconds", 600)
	page := parseIntQuery(r, "page", 1)
	pageSize := parseIntQuery(r, "page_size", 20)
	items, total, err := h.service.ListRecordings(r.Context(), tenantID, minDuration, page, pageSize)
	if err != nil {
		httputil.WriteInternalError(w, err.Error())
		return
	}
	httputil.WritePaginated(w, items, total, page, pageSize)
}

func (h *Handler) GetRecording(w http.ResponseWriter, r *http.Request) {
	id, ok := parsePathID(w, r.PathValue("id"))
	if !ok {
		return
	}
	item, err := h.service.GetRecording(r.Context(), id)
	if err != nil {
		httputil.WriteInternalError(w, err.Error())
		return
	}
	httputil.WriteSuccess(w, item)
}

func (h *Handler) SplitRecording(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		w.Header().Set("Allow", http.MethodPost)
		httputil.WriteError(w, http.StatusMethodNotAllowed, "METHOD_NOT_ALLOWED", "method not allowed", nil)
		return
	}
	var req SplitRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		httputil.WriteBadRequest(w, "invalid request body")
		return
	}
	if strings.TrimSpace(req.Model) == "" {
		req.Model = defaultModelName
	}
	if strings.TrimSpace(req.PromptVersion) == "" {
		req.PromptVersion = defaultPromptVersion
	}
	job := h.jobs.Create(req)
	go h.runSplitJob(job.ID, req)
	httputil.WriteSuccess(w, job)
}

func (h *Handler) GetSplitJob(w http.ResponseWriter, r *http.Request) {
	id := strings.TrimSpace(r.PathValue("id"))
	if id == "" {
		httputil.WriteBadRequest(w, "invalid id")
		return
	}
	job, ok := h.jobs.Get(id)
	if !ok {
		httputil.WriteNotFound(w, "split job not found")
		return
	}
	httputil.WriteSuccess(w, job)
}

func (h *Handler) SaveAnnotation(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		w.Header().Set("Allow", http.MethodPost)
		httputil.WriteError(w, http.StatusMethodNotAllowed, "METHOD_NOT_ALLOWED", "method not allowed", nil)
		return
	}
	var record AnnotationRecord
	if err := json.NewDecoder(r.Body).Decode(&record); err != nil {
		httputil.WriteBadRequest(w, "invalid request body")
		return
	}
	if record.RecordingID <= 0 {
		httputil.WriteBadRequest(w, "recording_id is required")
		return
	}
	if strings.TrimSpace(record.AnnotatedAt) == "" {
		record.AnnotatedAt = time.Now().Format(time.RFC3339)
	}
	if err := h.service.SaveAnnotation(r.Context(), &record); err != nil {
		httputil.WriteInternalError(w, err.Error())
		return
	}
	httputil.WriteSuccess(w, record)
}

func (h *Handler) SummarizeEncounter(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		w.Header().Set("Allow", http.MethodPost)
		httputil.WriteError(w, http.StatusMethodNotAllowed, "METHOD_NOT_ALLOWED", "method not allowed", nil)
		return
	}
	var req struct {
		Model         string `json:"model"`
		EncounterText string `json:"encounter_text"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		httputil.WriteBadRequest(w, "invalid request body")
		return
	}
	summary, err := h.service.SummarizeEncounterText(r.Context(), req.Model, req.EncounterText)
	if err != nil {
		httputil.WriteInternalError(w, err.Error())
		return
	}
	httputil.WriteSuccess(w, summary)
}

func (h *Handler) AnnotationOverview(w http.ResponseWriter, r *http.Request) {
	recordingID := parseInt64Query(r, "recording_id", 0)
	overview, err := h.service.GetAnnotationOverview(r.Context(), recordingID)
	if err != nil {
		httputil.WriteInternalError(w, err.Error())
		return
	}
	httputil.WriteSuccess(w, overview)
}

func (h *Handler) ListSyntheticCandidates(w http.ResponseWriter, r *http.Request) {
	tenantID := parseInt64Query(r, "tenant_id", 1)
	minDuration := parseIntQuery(r, "min_duration_seconds", 180)
	maxDuration := parseIntQuery(r, "max_duration_seconds", 900)
	limit := parseIntQuery(r, "limit", 50)
	items, err := h.service.ListSyntheticCandidates(r.Context(), tenantID, minDuration, maxDuration, limit)
	if err != nil {
		httputil.WriteInternalError(w, err.Error())
		return
	}
	httputil.WriteSuccess(w, items)
}

func (h *Handler) PreviewSyntheticCase(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		w.Header().Set("Allow", http.MethodPost)
		httputil.WriteError(w, http.StatusMethodNotAllowed, "METHOD_NOT_ALLOWED", "method not allowed", nil)
		return
	}
	var draft SyntheticCase
	if err := json.NewDecoder(r.Body).Decode(&draft); err != nil {
		httputil.WriteBadRequest(w, "invalid request body")
		return
	}
	result, err := h.service.BuildSyntheticPreview(r.Context(), &draft)
	if err != nil {
		httputil.WriteInternalError(w, err.Error())
		return
	}
	httputil.WriteSuccess(w, result)
}

func (h *Handler) SaveSyntheticCase(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		w.Header().Set("Allow", http.MethodPost)
		httputil.WriteError(w, http.StatusMethodNotAllowed, "METHOD_NOT_ALLOWED", "method not allowed", nil)
		return
	}
	var record SyntheticCase
	if err := json.NewDecoder(r.Body).Decode(&record); err != nil {
		httputil.WriteBadRequest(w, "invalid request body")
		return
	}
	if record.ID == "" {
		httputil.WriteBadRequest(w, "case id is required")
		return
	}
	if strings.TrimSpace(record.Name) == "" {
		record.Name = record.ID
	}
	if err := h.service.SaveSyntheticCase(r.Context(), &record); err != nil {
		httputil.WriteInternalError(w, err.Error())
		return
	}
	httputil.WriteSuccess(w, record)
}

func (h *Handler) ListSyntheticCases(w http.ResponseWriter, r *http.Request) {
	items, err := h.service.ListSyntheticCases(r.Context())
	if err != nil {
		httputil.WriteInternalError(w, err.Error())
		return
	}
	summaries := make([]SyntheticCaseSummary, 0, len(items))
	for _, item := range items {
		if item == nil {
			continue
		}
		summary := SyntheticCaseSummary{
			ID:              item.ID,
			Name:            item.Name,
			Description:     item.Description,
			Segments:        len(item.Segments),
			DurationSeconds: item.DurationSeconds,
			GroundTruth:     len(item.GroundTruth),
			UpdatedAt:       item.UpdatedAt,
		}
		if latest, err := h.service.LatestSyntheticRun(r.Context(), item.ID); err == nil && latest != nil && latest.Eval != nil {
			summary.LastPrecision = latest.Eval.Precision
			summary.LastRecall = latest.Eval.Recall
			summary.LastRunAt = latest.UpdatedAt.Format(time.RFC3339)
		}
		summaries = append(summaries, summary)
	}
	httputil.WriteSuccess(w, summaries)
}

func (h *Handler) GetSyntheticCase(w http.ResponseWriter, r *http.Request) {
	id := strings.TrimSpace(r.PathValue("id"))
	if id == "" {
		httputil.WriteBadRequest(w, "invalid id")
		return
	}
	item, err := h.service.GetSyntheticCase(r.Context(), id)
	if err != nil {
		httputil.WriteInternalError(w, err.Error())
		return
	}
	if item == nil {
		httputil.WriteNotFound(w, "synthetic case not found")
		return
	}
	resp := SyntheticCaseDetail{Case: *item}
	if latest, err := h.service.LatestSyntheticRun(r.Context(), id); err == nil {
		resp.LatestRun = latest
	}
	if runs, err := h.service.ListSyntheticRuns(r.Context(), id, 2); err == nil && len(runs) > 1 {
		resp.PreviousRuns = runs[1:]
	}
	httputil.WriteSuccess(w, resp)
}

func (h *Handler) DeleteSyntheticCase(w http.ResponseWriter, r *http.Request) {
	id := strings.TrimSpace(r.PathValue("id"))
	if id == "" {
		httputil.WriteBadRequest(w, "invalid id")
		return
	}
	if err := h.service.DeleteSyntheticCase(r.Context(), id); err != nil {
		httputil.WriteInternalError(w, err.Error())
		return
	}
	httputil.WriteSuccess(w, map[string]string{"id": id, "deleted": "true"})
}

func (h *Handler) RunSyntheticCase(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		w.Header().Set("Allow", http.MethodPost)
		httputil.WriteError(w, http.StatusMethodNotAllowed, "METHOD_NOT_ALLOWED", "method not allowed", nil)
		return
	}
	var req SyntheticRunRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		httputil.WriteBadRequest(w, "invalid request body")
		return
	}
	if strings.TrimSpace(req.CaseID) == "" {
		httputil.WriteBadRequest(w, "case_id is required")
		return
	}
	jobReq := SplitRequest{
		Model:         req.Model,
		SystemPrompt:  req.SystemPrompt,
		UserPrompt:    req.UserPrompt,
		PromptVersion: req.PromptVersion,
		Temperature:   req.Temperature,
		MaxChunkChars: req.MaxChunkChars,
	}
	job := h.jobs.Create(jobReq)
	h.jobs.Update(job.ID, func(job *SplitJob) {
		job.CaseID = req.CaseID
		job.Message = "合成测试任务已创建"
	})
	go h.runSyntheticJob(job.ID, req)
	httputil.WriteSuccess(w, job)
}

func (h *Handler) GetSyntheticRun(w http.ResponseWriter, r *http.Request) {
	id := strings.TrimSpace(r.PathValue("id"))
	if id == "" {
		httputil.WriteBadRequest(w, "invalid id")
		return
	}
	job, ok := h.jobs.Get(id)
	if !ok {
		httputil.WriteNotFound(w, "synthetic run not found")
		return
	}
	httputil.WriteSuccess(w, job)
}

func (h *Handler) runSplitJob(jobID string, req SplitRequest) {
	h.jobs.Update(jobID, func(job *SplitJob) {
		job.Status = SplitJobRunning
		job.Stage = "starting"
		job.Message = "任务开始执行"
	})
	result, err := h.service.SplitRecordingWithProgress(context.Background(), req, func(progress SplitProgress) {
		h.jobs.Update(jobID, func(job *SplitJob) {
			job.Status = SplitJobRunning
			job.Stage = progress.Stage
			job.Message = progress.Message
			job.TotalChunks = progress.TotalChunks
			job.CompletedChunks = progress.CompletedChunks
			job.CurrentChunk = progress.CurrentChunk
			job.SummaryTotal = progress.SummaryTotal
			job.SummaryDone = progress.SummaryDone
			job.CurrentSummary = progress.CurrentSummary
			job.PartialSegments = progress.PartialSegments
		})
	})
	if err != nil {
		h.jobs.Update(jobID, func(job *SplitJob) {
			job.Status = SplitJobFailed
			job.Message = "切分失败"
			job.ErrorMessage = err.Error()
		})
		return
	}
	h.jobs.Update(jobID, func(job *SplitJob) {
		job.Status = SplitJobCompleted
		job.Stage = "completed"
		job.Message = fmt.Sprintf("切分完成，共 %d 段", len(result.Segments))
		job.TotalChunks = result.ChunkCount
		job.CompletedChunks = result.ChunkCount
		job.CurrentChunk = result.ChunkCount
		job.SummaryTotal = len(result.Encounters)
		job.SummaryDone = len(result.Encounters)
		job.CurrentSummary = len(result.Encounters)
		job.PartialSegments = len(result.Segments)
		job.Result = result
	})
	if err := h.service.store.SaveRun(context.Background(), &SplitRunRecord{
		RecordingID:   req.RecordingID,
		Model:         req.Model,
		PromptVersion: req.PromptVersion,
		Status:        string(SplitJobCompleted),
		Result:        result,
		RawOutput:     result.RawOutput,
	}); err != nil {
		h.jobs.Update(jobID, func(job *SplitJob) {
			job.Status = SplitJobFailed
			job.Message = "切分完成但保存失败"
			job.ErrorMessage = err.Error()
		})
		return
	}
}

func (h *Handler) runSyntheticJob(jobID string, req SyntheticRunRequest) {
	h.jobs.Update(jobID, func(job *SplitJob) {
		job.Status = SplitJobRunning
		job.Stage = "starting"
		job.Message = "合成测试开始执行"
		job.CaseID = req.CaseID
	})
	result, caseData, err := h.service.SplitSyntheticCase(context.Background(), req.CaseID, SplitRequest{
		Model:         req.Model,
		SystemPrompt:  req.SystemPrompt,
		UserPrompt:    req.UserPrompt,
		PromptVersion: req.PromptVersion,
		Temperature:   req.Temperature,
		MaxChunkChars: req.MaxChunkChars,
	}, func(progress SplitProgress) {
		h.jobs.Update(jobID, func(job *SplitJob) {
			job.Status = SplitJobRunning
			job.Stage = progress.Stage
			job.Message = progress.Message
			job.TotalChunks = progress.TotalChunks
			job.CompletedChunks = progress.CompletedChunks
			job.CurrentChunk = progress.CurrentChunk
			job.SummaryTotal = progress.SummaryTotal
			job.SummaryDone = progress.SummaryDone
			job.CurrentSummary = progress.CurrentSummary
			job.PartialSegments = progress.PartialSegments
		})
	})
	if err != nil {
		h.jobs.Update(jobID, func(job *SplitJob) {
			job.Status = SplitJobFailed
			job.Message = "合成测试失败"
			job.ErrorMessage = err.Error()
		})
		return
	}
	eval := EvaluateSyntheticCase(caseData, result, valueOrDefault(req.ToleranceSeconds, 15))
	matchReport := buildSyntheticMatchReport(caseData, result)
	h.jobs.Update(jobID, func(job *SplitJob) {
		job.Status = SplitJobCompleted
		job.Stage = "completed"
		job.Message = fmt.Sprintf("合成测试完成，共 %d 段", len(result.Segments))
		job.TotalChunks = result.ChunkCount
		job.CompletedChunks = result.ChunkCount
		job.CurrentChunk = result.ChunkCount
		job.SummaryTotal = len(result.Encounters)
		job.SummaryDone = len(result.Encounters)
		job.CurrentSummary = len(result.Encounters)
		job.PartialSegments = len(result.Segments)
		job.Result = result
		job.Eval = eval
		job.MatchReport = matchReport
	})
	_ = h.service.store.SaveRun(context.Background(), &SplitRunRecord{
		CaseID:        req.CaseID,
		Model:         req.Model,
		PromptVersion: req.PromptVersion,
		Status:        string(SplitJobCompleted),
		Result:        result,
		Eval:          eval,
		MatchReport:   matchReport,
		RawOutput:     result.RawOutput,
	})
}

func valueOrDefault(value *int, fallback int) int {
	if value == nil || *value <= 0 {
		return fallback
	}
	return *value
}

func parsePathID(w http.ResponseWriter, raw string) (int64, bool) {
	id, err := strconv.ParseInt(strings.TrimSpace(raw), 10, 64)
	if err != nil || id <= 0 {
		httputil.WriteBadRequest(w, "invalid id")
		return 0, false
	}
	return id, true
}

func parseInt64Query(r *http.Request, key string, fallback int64) int64 {
	if raw := strings.TrimSpace(r.URL.Query().Get(key)); raw != "" {
		if v, err := strconv.ParseInt(raw, 10, 64); err == nil && v > 0 {
			return v
		}
	}
	return fallback
}

func parseIntQuery(r *http.Request, key string, fallback int) int {
	if raw := strings.TrimSpace(r.URL.Query().Get(key)); raw != "" {
		if v, err := strconv.Atoi(raw); err == nil && v >= 0 {
			return v
		}
	}
	return fallback
}
