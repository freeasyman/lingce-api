package content

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"time"

	"github.com/freeasyman/lingce-api/internal/router"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

// ContentAnalysisEnvelope mirrors the external-data delivery contract used by
// EMR/follow-up integrations: source identity + event + idempotency + payload.
type ContentAnalysisEnvelope struct {
	SourceSystem   string                 `json:"source_system"`
	SourceRecordID string                 `json:"source_record_id"`
	EventType      string                 `json:"event_type"`
	OccurredAt     time.Time              `json:"occurred_at"`
	UpdatedAt      time.Time              `json:"updated_at"`
	IdempotencyKey string                 `json:"idempotency_key"`
	Data           ContentAnalysisPayload `json:"data"`
}

type ContentAnalysisPayload struct {
	// TenantID 是信号的归属租户（worker 产生信号时带上录音真实租户）。
	// 为空时接收方按 recording_id 反查录音，再兜底到配置租户。
	TenantID    int64                     `json:"tenant_id,omitempty"`
	RecordingID int64                     `json:"recording_id"`
	EmployeeID  *int64                    `json:"employee_id,omitempty"`
	Questions   []ContentAnalysisQuestion `json:"questions"`
}

type ContentAnalysisQuestion struct {
	Topic              string   `json:"topic"`
	SeedType           string   `json:"seed_type"`
	ContentAngle       string   `json:"content_angle,omitempty"`
	PatientConcern     string   `json:"patient_concern,omitempty"`
	KeyQuotes          []string `json:"key_quotes,omitempty"`
	SuggestedPlatforms []string `json:"suggested_platforms,omitempty"`
	ViralPotential     string   `json:"viral_potential,omitempty"`
}

type ContentAnalysisDeliveryResponse struct {
	DeliveryID     string `json:"delivery_id"`
	Status         string `json:"status"`
	TenantID       int64  `json:"tenant_id"`
	SourceRecordID string `json:"source_record_id"`
	ResourceID     string `json:"resource_id,omitempty"`
	Accepted       int    `json:"accepted"`
	Error          string `json:"error,omitempty"`
}

type ExternalHandler struct {
	pool     *pgxpool.Pool
	tenantID int64
	service  *Service
}

func NewExternalHandler(pool *pgxpool.Pool, tenantID int64, service *Service) *ExternalHandler {
	return &ExternalHandler{pool: pool, tenantID: tenantID, service: service}
}

func (h *ExternalHandler) RegisterInternalRoutes(mux *http.ServeMux, token string) {
	deps := router.RouteDeps{InternalToken: strings.TrimSpace(token)}
	router.Register(mux, []router.Route{
		{Method: http.MethodPost, Path: "/internal/v1/content-analysis/signals", Handler: h.receive, AuthMode: "internal"},
		{Method: http.MethodGet, Path: "/internal/v1/content-analysis/deliveries", Handler: h.listDeliveries, AuthMode: "internal"},
		{Method: http.MethodGet, Path: "/internal/v1/content-analysis/deliveries/{id}", Handler: h.getDelivery, AuthMode: "internal"},
		{Method: http.MethodPost, Path: "/internal/v1/content-analysis/deliveries/{id}/retry", Handler: h.retryDelivery, AuthMode: "internal"},
		{Method: http.MethodGet, Path: "/internal/v1/content-analysis/stats", Handler: h.deliveryStats, AuthMode: "internal"},
	}, deps)
}

func (h *ExternalHandler) receive(w http.ResponseWriter, r *http.Request) {
	var env ContentAnalysisEnvelope
	dec := json.NewDecoder(http.MaxBytesReader(w, r.Body, 4<<20))
	if err := dec.Decode(&env); err != nil {
		writeContentJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid json"})
		return
	}
	if h.tenantID <= 0 {
		writeContentJSON(w, http.StatusInternalServerError, map[string]string{"error": "content analysis tenant is not configured"})
		return
	}
	if err := validateContentAnalysisEnvelope(env); err != nil {
		writeContentJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
		return
	}

	// 租户归属优先级：信封自带的 tenant_id → 按 recording_id 反查录音 → 配置兜底。
	// 这样跨租户录音不会被误判到配置租户下导致记录找不到。
	tenantID := h.tenantID
	if env.Data.TenantID > 0 {
		tenantID = env.Data.TenantID
	} else {
		var recordingTenantID int64
		if err := h.pool.QueryRow(r.Context(), `SELECT tenant_id FROM recordings WHERE id = $1`, env.Data.RecordingID).Scan(&recordingTenantID); err == nil && recordingTenantID > 0 {
			tenantID = recordingTenantID
		}
	}

	payload, _ := json.Marshal(env)
	tx, err := h.pool.Begin(r.Context())
	if err != nil {
		writeContentJSON(w, http.StatusInternalServerError, map[string]string{"error": "begin transaction failed"})
		return
	}
	defer tx.Rollback(r.Context())

	var existingID string
	var existingStatus string
	var existingUpdated time.Time
	err = tx.QueryRow(r.Context(), `
		SELECT id, status, COALESCE(source_updated_at, 'epoch'::timestamptz)
		FROM content_analysis_deliveries
		WHERE tenant_id=$1 AND source_system=$2 AND source_record_id=$3
	`, tenantID, env.SourceSystem, env.SourceRecordID).Scan(&existingID, &existingStatus, &existingUpdated)
	if err == nil && !env.UpdatedAt.After(existingUpdated) && existingStatus == "succeeded" {
		_ = tx.Rollback(r.Context())
		writeContentJSON(w, http.StatusOK, ContentAnalysisDeliveryResponse{
			DeliveryID: existingID, Status: "duplicate", TenantID: tenantID,
			SourceRecordID: env.SourceRecordID, ResourceID: fmt.Sprintf("%d", env.Data.RecordingID),
		})
		return
	}
	if err != nil && err != pgx.ErrNoRows {
		writeContentJSON(w, http.StatusInternalServerError, map[string]string{"error": "query delivery failed"})
		return
	}

	var deliveryID string
	err = tx.QueryRow(r.Context(), `
		INSERT INTO content_analysis_deliveries
			(tenant_id, source_system, source_record_id, idempotency_key, event_type, payload, status, source_updated_at, updated_at)
		VALUES ($1,$2,$3,$4,$5,$6::jsonb,'processing',$7,NOW())
		ON CONFLICT (tenant_id, source_system, source_record_id) DO UPDATE SET
			idempotency_key = EXCLUDED.idempotency_key,
			event_type = EXCLUDED.event_type,
			payload = EXCLUDED.payload,
			status = 'processing',
			error_reason = NULL,
			source_updated_at = EXCLUDED.source_updated_at,
			updated_at = NOW()
		RETURNING id
	`, tenantID, env.SourceSystem, env.SourceRecordID, env.IdempotencyKey, env.EventType, string(payload), env.UpdatedAt).Scan(&deliveryID)
	if err != nil {
		writeContentJSON(w, http.StatusInternalServerError, map[string]string{"error": "persist delivery failed"})
		return
	}

	accepted := 0
	for _, q := range env.Data.Questions {
		if err := upsertContentSeed(r.Context(), tx, tenantID, env, q, env.Data.EmployeeID); err != nil {
			_, _ = tx.Exec(r.Context(), `UPDATE content_analysis_deliveries SET status='failed', error_reason=$2, updated_at=NOW() WHERE id=$1`, deliveryID, err.Error())
			_ = tx.Commit(r.Context())
			writeContentJSON(w, http.StatusUnprocessableEntity, ContentAnalysisDeliveryResponse{
				DeliveryID: deliveryID, Status: "failed", TenantID: tenantID,
				SourceRecordID: env.SourceRecordID, Error: err.Error(),
			})
			return
		}
		accepted++
	}

	if _, err := tx.Exec(r.Context(), `
		UPDATE content_analysis_deliveries
		SET status='succeeded', resource_id=$2, accepted=$3, updated_at=NOW()
		WHERE id=$1
	`, deliveryID, fmt.Sprintf("%d", env.Data.RecordingID), accepted); err != nil {
		// 兼容没有 accepted 列的旧库结构
		_, _ = tx.Exec(r.Context(), `UPDATE content_analysis_deliveries SET status='succeeded', resource_id=$2, updated_at=NOW() WHERE id=$1`, deliveryID, fmt.Sprintf("%d", env.Data.RecordingID))
	}
	if err := tx.Commit(r.Context()); err != nil {
		writeContentJSON(w, http.StatusInternalServerError, map[string]string{"error": "commit failed"})
		return
	}

	// 新链路：Worker 分析结束 → 信号到达 → 触发新 28 字段洞察提取 + 选题（异步，不阻塞信号响应）。
	if h.service != nil {
		recordingID := env.Data.RecordingID
		slog.Info("content insight extraction triggered", "tenant_id", tenantID, "recording_id", recordingID)
		go func() {
			ctx, cancel := context.WithTimeout(context.Background(), 10*time.Minute)
			defer cancel()
			if _, err := h.service.AnalyzeRecording(ctx, tenantID, recordingID); err != nil {
				slog.Warn("content insight extraction after worker signal failed", "error", err, "tenant_id", tenantID, "recording_id", recordingID)
			} else {
				slog.Info("content insight extraction completed", "tenant_id", tenantID, "recording_id", recordingID)
			}
		}()
	} else {
		slog.Warn("content insight extraction skipped: service is nil", "recording_id", env.Data.RecordingID)
	}

	writeContentJSON(w, http.StatusOK, ContentAnalysisDeliveryResponse{
		DeliveryID: deliveryID, Status: "succeeded", TenantID: tenantID,
		SourceRecordID: env.SourceRecordID, ResourceID: fmt.Sprintf("%d", env.Data.RecordingID), Accepted: accepted,
	})
}

func (h *ExternalHandler) getDelivery(w http.ResponseWriter, r *http.Request) {
	id := strings.TrimSpace(r.PathValue("id"))
	if id == "" {
		writeContentJSON(w, http.StatusBadRequest, map[string]string{"error": "delivery id is required"})
		return
	}
	var resp struct {
		ID             string    `json:"delivery_id"`
		Status         string    `json:"status"`
		TenantID       int64     `json:"tenant_id"`
		SourceSystem   string    `json:"source_system"`
		SourceRecordID string    `json:"source_record_id"`
		ResourceID     *string   `json:"resource_id,omitempty"`
		Error          *string   `json:"error,omitempty"`
		Attempts       int       `json:"attempts"`
		CreatedAt      time.Time `json:"created_at"`
		UpdatedAt      time.Time `json:"updated_at"`
	}
	err := h.pool.QueryRow(r.Context(), `
		SELECT id, status, tenant_id, source_system, source_record_id, resource_id, error_reason, attempts, created_at, updated_at
		FROM content_analysis_deliveries WHERE id=$1 AND tenant_id=$2
	`, id, h.tenantID).Scan(&resp.ID, &resp.Status, &resp.TenantID, &resp.SourceSystem, &resp.SourceRecordID, &resp.ResourceID, &resp.Error, &resp.Attempts, &resp.CreatedAt, &resp.UpdatedAt)
	if err != nil {
		writeContentJSON(w, http.StatusNotFound, map[string]string{"error": "delivery not found"})
		return
	}
	writeContentJSON(w, http.StatusOK, resp)
}

func validateContentAnalysisEnvelope(env ContentAnalysisEnvelope) error {
	if strings.TrimSpace(env.SourceSystem) == "" || strings.TrimSpace(env.SourceRecordID) == "" || strings.TrimSpace(env.IdempotencyKey) == "" || strings.TrimSpace(env.EventType) == "" || env.UpdatedAt.IsZero() {
		return fmt.Errorf("source_system, source_record_id, event_type, updated_at and idempotency_key are required")
	}
	if env.Data.RecordingID <= 0 {
		return fmt.Errorf("data.recording_id is required")
	}
	if len(env.Data.Questions) == 0 {
		return fmt.Errorf("data.questions is required")
	}
	for i, q := range env.Data.Questions {
		if strings.TrimSpace(q.Topic) == "" {
			return fmt.Errorf("data.questions[%d].topic is required", i)
		}
	}
	return nil
}

// 旧 worker 信号入口：写入 content_insights（原 recording_content_seeds 已在 Phase 3 下线）。
func upsertContentSeed(ctx context.Context, tx pgx.Tx, tenantID int64, env ContentAnalysisEnvelope, q ContentAnalysisQuestion, employeeID *int64) error {
	insightType := mapSeedTypeToInsightType(q.SeedType)
	topic := strings.TrimSpace(q.Topic)
	if topic == "" {
		return fmt.Errorf("topic is required")
	}
	quotes := make([]map[string]string, 0, len(q.KeyQuotes))
	for _, text := range q.KeyQuotes {
		if strings.TrimSpace(text) != "" {
			quotes = append(quotes, map[string]string{"text": text, "speaker": "patient"})
		}
	}
	evidence := map[string]any{
		"source_system":    env.SourceSystem,
		"source_record_id": env.SourceRecordID,
		"event_type":       env.EventType,
		"quotes":           quotes,
	}
	if a := strings.TrimSpace(q.ContentAngle); a != "" {
		evidence["angle"] = a
	}
	if c := strings.TrimSpace(q.PatientConcern); c != "" {
		evidence["patient_concern"] = c
	}
	if v := strings.TrimSpace(q.ViralPotential); v != "" {
		evidence["viral_potential"] = v
	}
	evidenceJSON, _ := json.Marshal(evidence)
	platforms := q.SuggestedPlatforms
	if platforms == nil {
		platforms = []string{}
	}
	targetsJSON, _ := json.Marshal(platforms)

	// 幂等：同租户 + 录音 + topic 就地更新
	res, err := tx.Exec(ctx, `
		UPDATE content_insights
		SET type=$4, evidence=$5::jsonb, suggested_targets=$6::jsonb
		WHERE tenant_id=$1 AND recording_id=$2 AND topic=$3`,
		tenantID, env.Data.RecordingID, topic, insightType, string(evidenceJSON), string(targetsJSON))
	if err != nil {
		return fmt.Errorf("update content insight: %w", err)
	}
	if res.RowsAffected() > 0 {
		return nil
	}
	if _, err := tx.Exec(ctx, `
		INSERT INTO content_insights
			(tenant_id, recording_id, source_type, type, topic, evidence, suggested_forms, suggested_targets)
		VALUES ($1,$2,'worker',$3,$4,$5::jsonb,'[]'::jsonb,$6::jsonb)`,
		tenantID, env.Data.RecordingID, insightType, topic, string(evidenceJSON), string(targetsJSON)); err != nil {
		return fmt.Errorf("insert content insight: %w", err)
	}
	return nil
}

// mapSeedTypeToInsightType worker 的 seed_type → 洞察 type。
func mapSeedTypeToInsightType(seedType string) string {
	switch strings.ToLower(strings.TrimSpace(seedType)) {
	case "question", "practical_qa", "qa":
		return "question"
	case "concern", "concern_handling":
		return "concern"
	case "quote", "golden_quote":
		return "quote"
	case "scene", "relatable_scene":
		return "scene"
	case "aha", "aha_moment":
		return "aha"
	case "viewpoint", "explanation":
		return "viewpoint"
	case "story", "fact":
		return strings.ToLower(strings.TrimSpace(seedType))
	default:
		if validInsightTypes[strings.ToLower(strings.TrimSpace(seedType))] {
			return strings.ToLower(strings.TrimSpace(seedType))
		}
		return "question"
	}
}

func writeContentJSON(w http.ResponseWriter, status int, payload any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(payload)
}

// listDeliveries exposes delivery status for operations/monitoring.
func (h *ExternalHandler) listDeliveries(w http.ResponseWriter, r *http.Request) {
	status := strings.TrimSpace(r.URL.Query().Get("status"))
	source := strings.TrimSpace(r.URL.Query().Get("source_system"))
	limit := 50
	if raw := r.URL.Query().Get("limit"); raw != "" {
		if parsed, err := strconv.Atoi(raw); err == nil && parsed > 0 && parsed <= 200 {
			limit = parsed
		}
	}
	conditions := []string{"tenant_id=$1"}
	args := []any{h.tenantID}
	argIndex := 2
	if status != "" {
		conditions = append(conditions, fmt.Sprintf("status=$%d", argIndex))
		args = append(args, status)
		argIndex++
	}
	if source != "" {
		conditions = append(conditions, fmt.Sprintf("source_system=$%d", argIndex))
		args = append(args, source)
		argIndex++
	}
	where := strings.Join(conditions, " AND ")
	var total int
	if err := h.pool.QueryRow(r.Context(), "SELECT COUNT(*) FROM content_analysis_deliveries WHERE "+where, args...).Scan(&total); err != nil {
		writeContentJSON(w, http.StatusInternalServerError, map[string]string{"error": "count deliveries failed"})
		return
	}
	rows, err := h.pool.Query(r.Context(), `
		SELECT id, status, tenant_id, source_system, source_record_id, event_type, resource_id, error_reason,
		       attempts, accepted, created_at, updated_at
		FROM content_analysis_deliveries
		WHERE `+where+`
		ORDER BY created_at DESC
		LIMIT $`+fmt.Sprintf("%d", argIndex), append(args, limit)...)
	if err != nil {
		writeContentJSON(w, http.StatusInternalServerError, map[string]string{"error": "list deliveries failed"})
		return
	}
	defer rows.Close()
	items := make([]map[string]any, 0, limit)
	for rows.Next() {
		var id, status, sourceSystem, sourceRecordID, eventType string
		var tenantID int64
		var resourceID, errorReason *string
		var attempts, accepted int
		var createdAt, updatedAt time.Time
		if err := rows.Scan(&id, &status, &tenantID, &sourceSystem, &sourceRecordID, &eventType, &resourceID, &errorReason, &attempts, &accepted, &createdAt, &updatedAt); err != nil {
			writeContentJSON(w, http.StatusInternalServerError, map[string]string{"error": "scan delivery failed"})
			return
		}
		items = append(items, map[string]any{
			"delivery_id": id, "status": status, "tenant_id": tenantID, "source_system": sourceSystem,
			"source_record_id": sourceRecordID, "event_type": eventType, "resource_id": resourceID,
			"error": errorReason, "attempts": attempts, "accepted": accepted,
			"created_at": createdAt, "updated_at": updatedAt,
		})
	}
	writeContentJSON(w, http.StatusOK, map[string]any{"items": items, "total": total})
}

// retryDelivery re-runs the exact stored envelope through the same receive
// path. Idempotency and delivery bookkeeping stay in one place.
func (h *ExternalHandler) retryDelivery(w http.ResponseWriter, r *http.Request) {
	id := strings.TrimSpace(r.PathValue("id"))
	var payload []byte
	var status string
	if err := h.pool.QueryRow(r.Context(), `
		SELECT payload, status FROM content_analysis_deliveries WHERE id=$1 AND tenant_id=$2
	`, id, h.tenantID).Scan(&payload, &status); err != nil {
		writeContentJSON(w, http.StatusNotFound, map[string]string{"error": "delivery not found"})
		return
	}
	if status == "succeeded" {
		writeContentJSON(w, http.StatusConflict, map[string]string{"error": "delivery already succeeded"})
		return
	}
	_, _ = h.pool.Exec(r.Context(), `UPDATE content_analysis_deliveries SET attempts=attempts+1, updated_at=NOW() WHERE id=$1`, id)
	req := httptest.NewRequest(http.MethodPost, "/internal/v1/content-analysis/signals", bytes.NewReader(payload))
	req = req.WithContext(r.Context())
	rec := httptest.NewRecorder()
	h.receive(rec, req)
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(rec.Code)
	_, _ = w.Write(rec.Body.Bytes())
}

func (h *ExternalHandler) deliveryStats(w http.ResponseWriter, r *http.Request) {
	rows, err := h.pool.Query(r.Context(), `
		SELECT status, COUNT(*), COALESCE(SUM(attempts),0)
		FROM content_analysis_deliveries WHERE tenant_id=$1 GROUP BY status
	`, h.tenantID)
	if err != nil {
		writeContentJSON(w, http.StatusInternalServerError, map[string]string{"error": "stats failed"})
		return
	}
	defer rows.Close()
	byStatus := map[string]map[string]int64{}
	var total int64
	for rows.Next() {
		var status string
		var count, attempts int64
		if err := rows.Scan(&status, &count, &attempts); err != nil {
			writeContentJSON(w, http.StatusInternalServerError, map[string]string{"error": "scan stats failed"})
			return
		}
		byStatus[status] = map[string]int64{"count": count, "attempts": attempts}
		total += count
	}
	writeContentJSON(w, http.StatusOK, map[string]any{"total": total, "by_status": byStatus})
}
