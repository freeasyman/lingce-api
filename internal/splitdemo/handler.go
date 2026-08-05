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
