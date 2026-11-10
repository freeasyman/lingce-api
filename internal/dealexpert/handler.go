package dealexpert

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"strings"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/freeasyman/lingce-api/internal/dealexpertpermission"
	"github.com/freeasyman/lingce-api/internal/router"
	"github.com/freeasyman/lingce-api/pkg/httputil"
)

type Handler struct {
	service      *Service
	pool         *pgxpool.Pool
	consultation *ConsultationAnalysisGenerator
	access       *dealexpertpermission.Service
}

// SetConsultationAnalysisGenerator 设置接待记录分析生成器。
func (h *Handler) SetConsultationAnalysisGenerator(gen *ConsultationAnalysisGenerator) {
	if h != nil {
		h.consultation = gen
	}
}

func NewHandler(service *Service, pool *pgxpool.Pool) *Handler {
	return &Handler{service: service, pool: pool, access: dealexpertpermission.NewService(pool)}
}

func (h *Handler) RegisterInternalRoutes(mux *http.ServeMux, internalToken string) {
	router.Register(mux, []router.Route{
		{Method: "POST", Path: "/api/v1/internal/deal-expert/recordings/{id}/analyze", Handler: h.AnalyzeRecordingInternal, AuthMode: "internal"},
	}, router.RouteDeps{InternalToken: strings.TrimSpace(internalToken)})
}

// RecordingAnalyzeRequest 是 Worker 侧发起接待记录分析的请求体。
type RecordingAnalyzeRequest struct {
	TenantID          int64  `json:"tenant_id"`
	RecordingID       int64  `json:"recording_id"`
	EncounterID       int64  `json:"encounter_id"`
	EmployeeID        int64  `json:"employee_id"`
	CustomerID        int64  `json:"customer_id"`
	CustomerName      string `json:"customer_name"`
	BusinessScope     string `json:"business_scope"`
	RoleCode          string `json:"role_code"`
	RecordedAt        string `json:"recorded_at"`
	CleanedTranscript string `json:"cleaned_transcript"`
}

// AnalyzeRecordingInternal 内部接口：校验引用后落 pending 记录并异步生成接待记录分析，不阻塞 Worker。
func (h *Handler) AnalyzeRecordingInternal(w http.ResponseWriter, r *http.Request) {
	if h.consultation == nil {
		httputil.WriteInternalError(w, "consultation analysis generator unavailable")
		return
	}
	id, err := parsePathID(r.PathValue("id"))
	if err != nil || id <= 0 {
		httputil.WriteBadRequest(w, "invalid recording id")
		return
	}
	var body RecordingAnalyzeRequest
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		httputil.WriteBadRequest(w, "invalid request body")
		return
	}
	if body.TenantID <= 0 {
		httputil.WriteBadRequest(w, "tenant_id is required")
		return
	}
	if body.RecordingID > 0 && body.RecordingID != id {
		httputil.WriteBadRequest(w, "recording_id mismatch")
		return
	}
	if err := h.validateAnalysisRefs(r.Context(), body.TenantID, id, body.EncounterID, body.EmployeeID, body.CustomerID); err != nil {
		httputil.WriteBadRequest(w, err.Error())
		return
	}
	if err := h.consultation.UpsertPending(r.Context(), body.TenantID, id); err != nil {
		httputil.WriteInternalError(w, err.Error())
		return
	}
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), 4*time.Minute)
		defer cancel()
		if _, err := h.consultation.Generate(ctx, body.TenantID, id); err != nil {
			slog.Error("async consultation analysis failed", "recording_id", id, "error", err)
		}
	}()
	httputil.WriteSuccess(w, map[string]any{"code": "ACCEPTED", "status": "accepted"})
}

// validateAnalysisRefs 校验录音、接待、员工、客户引用都存在且同租户。
func (h *Handler) validateAnalysisRefs(ctx context.Context, tenantID, recordingID, encounterID, employeeID, customerID int64) error {
	var recTenant, recEncounter, recEmployee, recCustomer int64
	if err := h.pool.QueryRow(ctx, `
		SELECT tenant_id, COALESCE(encounter_id,0), COALESCE(employee_id,0), COALESCE(customer_id,0)
		FROM recordings WHERE id = $1 AND deleted_at IS NULL
	`, recordingID).Scan(&recTenant, &recEncounter, &recEmployee, &recCustomer); err != nil {
		return fmt.Errorf("recording not found")
	}
	if recTenant != tenantID {
		return fmt.Errorf("recording tenant mismatch")
	}
	if encounterID > 0 {
		var encTenant int64
		if err := h.pool.QueryRow(ctx, `SELECT tenant_id FROM encounters WHERE id = $1`, encounterID).Scan(&encTenant); err != nil {
			return fmt.Errorf("encounter not found")
		}
		if encTenant != tenantID {
			return fmt.Errorf("encounter tenant mismatch")
		}
	}
	if employeeID > 0 {
		var empTenant int64
		if err := h.pool.QueryRow(ctx, `SELECT tenant_id FROM employees WHERE id = $1`, employeeID).Scan(&empTenant); err != nil {
			return fmt.Errorf("employee not found")
		}
		if empTenant != tenantID {
			return fmt.Errorf("employee tenant mismatch")
		}
	}
	if customerID > 0 {
		var cusTenant int64
		if err := h.pool.QueryRow(ctx, `SELECT tenant_id FROM customers WHERE id = $1`, customerID).Scan(&cusTenant); err != nil {
			return fmt.Errorf("customer not found")
		}
		if cusTenant != tenantID {
			return fmt.Errorf("customer tenant mismatch")
		}
	}
	return nil
}

func parsePathID(value string) (int64, error) {
	var id int64
	_, err := fmt.Sscanf(value, "%d", &id)
	return id, err
}
