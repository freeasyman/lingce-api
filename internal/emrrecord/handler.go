package emrrecord

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/freeasyman/lingce-api/internal/billing"
	"github.com/freeasyman/lingce-api/internal/emrinput"
	"github.com/freeasyman/lingce-api/internal/emrpermission"
	"github.com/freeasyman/lingce-api/internal/emrtemplate"
	"github.com/freeasyman/lingce-api/internal/middleware"
	"github.com/freeasyman/lingce-api/internal/router"
	"github.com/freeasyman/lingce-api/internal/tenancy"
	"github.com/freeasyman/lingce-api/pkg/httputil"
)

type Handler struct {
	service     *Service
	permissions *emrpermission.Service
	internal    *InternalHandler
}

func NewHandler(service *Service, permissions *emrpermission.Service, internal *InternalHandler) *Handler {
	return &Handler{service: service, permissions: permissions, internal: internal}
}

func (h *Handler) RegisterRoutes(mux *http.ServeMux, jwtSecret string) {
	router.Register(mux, []router.Route{
		{Method: "GET", Path: "/api/v1/emr/records", Handler: h.List, Auth: true, AllowedUserTypes: []string{"admin", "employee"}},
		{Method: "GET", Path: "/api/v1/customers/{id}/emr-records", Handler: h.ListByPatient, Auth: true, AllowedUserTypes: []string{"admin", "employee"}},
		{Method: "GET", Path: "/api/v1/customers/{id}/emr-record-status", Handler: h.ListRecordStatusByPatient, Auth: true, AllowedUserTypes: []string{"admin", "employee"}},
		{Method: "POST", Path: "/api/v1/emr/records", Handler: h.Create, Auth: true, AllowedUserTypes: []string{"admin", "employee"}},
		{Method: "POST", Path: "/api/v1/emr/records/import-from-recording", Handler: h.ImportFromRecording, Auth: true, AllowedUserTypes: []string{"admin", "employee"}},
		{Method: "GET", Path: "/api/v1/emr/records/{id}", Handler: h.Get, Auth: true, AllowedUserTypes: []string{"admin", "employee"}},
		{Method: "GET", Path: "/api/v1/emr/records/{id}/versions", Handler: h.Snapshots, Auth: true, AllowedUserTypes: []string{"admin", "employee"}},
		{Method: "GET", Path: "/api/v1/emr/records/{id}/export", Handler: h.ExportAudit, Auth: true, AllowedUserTypes: []string{"admin", "employee"}},
		{Method: "PATCH", Path: "/api/v1/emr/records/{id}/working-draft", Handler: h.AutoSave, Auth: true, AllowedUserTypes: []string{"admin", "employee"}},
		{Method: "PATCH", Path: "/api/v1/emr/records/{id}/visit-type", Handler: h.UpdateVisitType, Auth: true, AllowedUserTypes: []string{"admin", "employee"}},
		{Method: "POST", Path: "/api/v1/emr/records/{id}/regenerate", Handler: h.Regenerate, Auth: true, AllowedUserTypes: []string{"admin", "employee"}},
		{Method: "POST", Path: "/api/v1/emr/records/{id}/save", Handler: h.ManualSave, Auth: true, AllowedUserTypes: []string{"admin", "employee"}},
		{Method: "POST", Path: "/api/v1/emr/records/{id}/submit", Handler: h.Submit, Auth: true, AllowedUserTypes: []string{"admin", "employee"}},
		{Method: "POST", Path: "/api/v1/emr/records/{id}/complete", Handler: h.Complete, Auth: true, AllowedUserTypes: []string{"admin", "employee"}},
		{Method: "POST", Path: "/api/v1/emr/records/{id}/confirm", Handler: h.Confirm, Auth: true, AllowedUserTypes: []string{"admin", "employee"}},
		{Method: "POST", Path: "/api/v1/emr/records/{id}/archive", Handler: h.Archive, Auth: true, AllowedUserTypes: []string{"admin", "employee"}},
		{Method: "POST", Path: "/api/v1/emr/records/{id}/return", Handler: h.Return, Auth: true, AllowedUserTypes: []string{"admin", "employee"}},
		{Method: "POST", Path: "/api/v1/emr/records/{id}/revise", Handler: h.Revise, Auth: true, AllowedUserTypes: []string{"admin", "employee"}},
		{Method: "POST", Path: "/api/v1/emr/records/{id}/void", Handler: h.Void, Auth: true, AllowedUserTypes: []string{"admin", "employee"}},
	}, router.RouteDeps{JWTSecret: jwtSecret})
}

func (h *Handler) tenant(r *http.Request) (int64, int64, error) {
	claims := middleware.GetUserClaims(r.Context())
	tenantID, err := tenancy.RequireTenantID(claims, r.URL.Query().Get("tenant_id"))
	if err != nil {
		return 0, 0, err
	}
	return tenantID, claims.UserID, nil
}

func (h *Handler) authorize(r *http.Request, ability string) (*emrpermission.Access, int64, error) {
	claims := middleware.GetUserClaims(r.Context())
	tenantID, _, err := h.tenant(r)
	if err != nil {
		return nil, 0, err
	}
	access, err := h.permissions.Authorize(r.Context(), claims, tenantID, ability)
	if err != nil {
		return nil, 0, err
	}
	return access, tenantID, nil
}

func (h *Handler) authorizeRecord(r *http.Request, ability string) (*emrpermission.Access, int64, error) {
	access, tenantID, err := h.authorize(r, ability)
	if err != nil {
		return nil, 0, err
	}
	allowed, err := h.permissions.CanAccessRecord(r.Context(), access, recordID(r))
	if err != nil {
		return nil, 0, err
	}
	if !allowed {
		return nil, 0, fmt.Errorf("emr record access denied")
	}
	return access, tenantID, nil
}

func decodeBody(r *http.Request, value any) error { return json.NewDecoder(r.Body).Decode(value) }
func recordID(r *http.Request) string             { return strings.TrimSpace(r.PathValue("id")) }

func (h *Handler) List(w http.ResponseWriter, r *http.Request) {
	access, tenantID, err := h.authorize(r, "record.read")
	if err != nil {
		httputil.WriteForbidden(w, err.Error())
		return
	}
	status := r.URL.Query().Get("status")
	// 列表契约统一：q 与 keyword 等价（q 优先）。
	keyword := strings.TrimSpace(r.URL.Query().Get("q"))
	if keyword == "" {
		keyword = strings.TrimSpace(r.URL.Query().Get("keyword"))
	}

	// 带 page 参数走服务端分页；不带则返回全部（兼容内部 list() 的整表读取场景）。
	if r.URL.Query().Get("page") == "" {
		items, listErr := h.service.ListScoped(r.Context(), tenantID, status, access)
		if listErr != nil {
			httputil.WriteInternalError(w, listErr.Error())
			return
		}
		httputil.WriteSuccess(w, map[string]any{"items": items})
		return
	}

	pagination := httputil.ParsePagination(r)
	filter := ListFilter{
		Status:         status,
		Queue:          r.URL.Query().Get("queue"),
		Keyword:        keyword,
		DoctorID:       parseOptionalID(r.URL.Query().Get("doctor_id")),
		DepartmentCode: r.URL.Query().Get("department_code"),
		ScoreStatus:    r.URL.Query().Get("score_status"),
	}
	items, total, listErr := h.service.ListScopedPaginated(r.Context(), tenantID, filter, pagination.Page, pagination.PageSize, access)
	if listErr != nil {
		httputil.WriteInternalError(w, listErr.Error())
		return
	}
	resp := map[string]any{
		"items":     items,
		"total":     total,
		"page":      pagination.Page,
		"page_size": pagination.PageSize,
	}
	if r.URL.Query().Get("with_counts") == "1" {
		var employeeID int64
		if claims := middleware.GetUserClaims(r.Context()); claims != nil {
			employeeID = claims.UserID
		}
		pending, archived, returned, unread, countErr := h.service.CountByState(r.Context(), tenantID, access, employeeID)
		if countErr != nil {
			httputil.WriteInternalError(w, countErr.Error())
			return
		}
		resp["counts"] = map[string]int{"pending": pending, "archived": archived, "returned": returned, "unread": unread}
	}
	httputil.WriteSuccess(w, resp)
}

func parseOptionalID(raw string) int64 {
	id, err := strconv.ParseInt(strings.TrimSpace(raw), 10, 64)
	if err != nil || id <= 0 {
		return 0
	}
	return id
}

func (h *Handler) ListByPatient(w http.ResponseWriter, r *http.Request) {
	access, tenantID, err := h.authorize(r, "record.read")
	if err != nil {
		httputil.WriteForbidden(w, err.Error())
		return
	}
	patientID, err := parsePositiveID(recordID(r), "patient_id")
	if err != nil {
		httputil.WriteBadRequest(w, err.Error())
		return
	}
	pagination := httputil.ParsePagination(r)
	page, pageSize := pagination.Page, pagination.PageSize
	items, total, err := h.service.ListByPatientScoped(r.Context(), tenantID, patientID, page, pageSize, access)
	if err != nil {
		httputil.WriteInternalError(w, err.Error())
		return
	}
	httputil.WritePaginated(w, items, int64(total), page, pageSize)
}

// ListRecordStatusByPatient 返回病历状态白名单，不返回病历正文。
func (h *Handler) ListRecordStatusByPatient(w http.ResponseWriter, r *http.Request) {
	_, tenantID, err := h.authorize(r, "record.read")
	if err != nil {
		httputil.WriteForbidden(w, err.Error())
		return
	}
	customerID, err := parsePositiveID(recordID(r), "patient_id")
	if err != nil {
		httputil.WriteBadRequest(w, err.Error())
		return
	}
	items, err := h.service.ListRecordStatusByPatient(r.Context(), tenantID, customerID)
	if err != nil {
		httputil.WriteInternalError(w, err.Error())
		return
	}
	httputil.WriteSuccess(w, map[string]any{"items": items})
}

func parsePositiveID(raw, name string) (int64, error) {
	id, err := strconv.ParseInt(strings.TrimSpace(raw), 10, 64)
	if err != nil || id <= 0 {
		return 0, fmt.Errorf("invalid %s", name)
	}
	return id, nil
}
func (h *Handler) Create(w http.ResponseWriter, r *http.Request) {
	access, tenantID, err := h.authorize(r, "record.create")
	if err != nil {
		httputil.WriteForbidden(w, err.Error())
		return
	}
	var req CreateRequest
	if decodeBody(r, &req) != nil {
		httputil.WriteBadRequest(w, "invalid request body")
		return
	}
	doctorID := access.UserID
	if req.DoctorID != nil && *req.DoctorID > 0 {
		doctorID = *req.DoctorID
	}
	if !access.CanRecord(doctorID, req.DepartmentID) {
		httputil.WriteForbidden(w, "emr record access denied")
		return
	}
	item, err := h.service.Create(r.Context(), tenantID, access.UserID, req)
	if err != nil {
		httputil.WriteBadRequest(w, err.Error())
		return
	}
	httputil.WriteSuccess(w, map[string]any{"data": item})
}

func (h *Handler) ImportFromRecording(w http.ResponseWriter, r *http.Request) {
	access, tenantID, err := h.authorize(r, "record.create")
	if err != nil {
		httputil.WriteForbidden(w, err.Error())
		return
	}
	var req struct {
		RecordingID       int64  `json:"recording_id"`
		EncounterID       *int64 `json:"encounter_id,omitempty"`
		TemplateVersionID string `json:"template_version_id"`
		DocumentType      string `json:"document_type"`
		VisitType         string `json:"visit_type"`
	}
	if decodeBody(r, &req) != nil {
		httputil.WriteBadRequest(w, "invalid request body")
		return
	}
	if req.RecordingID <= 0 {
		httputil.WriteBadRequest(w, "recording_id is required")
		return
	}
	var rec struct {
		EncounterID    sql.NullInt64
		CustomerID     sql.NullInt64
		PatientID      sql.NullInt64
		PatientName    string
		PatientPhone   sql.NullString
		PatientGender  sql.NullString
		PatientAge     sql.NullInt64
		TranscriptText sql.NullString
		RecordedAt     sql.NullTime
		Status         string
	}
	if err := h.service.store.pool.QueryRow(r.Context(), `
		SELECT
			COALESCE(r.encounter_id, 0),
			COALESCE(r.customer_id, 0),
			COALESCE(r.patient_id, 0),
			COALESCE(NULLIF(c.name, ''), ''),
			COALESCE(c.phone, ''),
			COALESCE(c.gender, ''),
			COALESCE(c.age, 0),
			COALESCE(r.transcription_text, ''),
			r.recorded_at,
			COALESCE(r.status, '')
		FROM recordings r
		LEFT JOIN customers c ON c.id = r.customer_id
		WHERE r.id = $1 AND r.deleted_at IS NULL
	`, req.RecordingID).Scan(
		&rec.EncounterID,
		&rec.CustomerID,
		&rec.PatientID,
		&rec.PatientName,
		&rec.PatientPhone,
		&rec.PatientGender,
		&rec.PatientAge,
		&rec.TranscriptText,
		&rec.RecordedAt,
		&rec.Status,
	); err != nil {
		httputil.WriteNotFound(w, "recording not found")
		return
	}
	transcript := strings.TrimSpace(rec.TranscriptText.String)
	encounterID := req.EncounterID
	if encounterID == nil && rec.EncounterID.Valid && rec.EncounterID.Int64 > 0 {
		value := rec.EncounterID.Int64
		encounterID = &value
	}
	var customerID *int64
	if rec.CustomerID.Valid && rec.CustomerID.Int64 > 0 {
		value := rec.CustomerID.Int64
		customerID = &value
	}
	var patientID *int64
	if rec.PatientID.Valid && rec.PatientID.Int64 > 0 {
		value := rec.PatientID.Int64
		patientID = &value
	}
	patient := &emrinput.Patient{
		CustomerID: customerID,
		PatientID:  patientID,
		Name:       rec.PatientName,
	}
	if rec.PatientPhone.Valid && strings.TrimSpace(rec.PatientPhone.String) != "" {
		value := rec.PatientPhone.String
		patient.Phone = &value
	}
	if rec.PatientGender.Valid && strings.TrimSpace(rec.PatientGender.String) != "" {
		value := rec.PatientGender.String
		patient.Gender = &value
	}
	if rec.PatientAge.Valid && rec.PatientAge.Int64 > 0 {
		value := int(rec.PatientAge.Int64)
		patient.Age = &value
	}
	input := emrinput.NewHistoricalInput(
		strconv.FormatInt(req.RecordingID, 10),
		tenantID,
		encounterID,
		nil,
		patient,
		transcript,
		transcript,
		nil,
		strconv.FormatInt(req.RecordingID, 10),
		strconv.FormatInt(req.RecordingID, 10),
		map[string]any{"recording_id": req.RecordingID, "recording_status": rec.Status},
		time.Now(),
	)
	item, err := h.service.CreateFromHistoricalRecording(r.Context(), tenantID, access.UserID, CreateRequest{
		EncounterID: func() int64 {
			if encounterID == nil {
				return 0
			}
			return *encounterID
		}(),
		PatientID:         patientID,
		PatientSnapshot:   map[string]any{"name": rec.PatientName, "phone": rec.PatientPhone.String, "gender": rec.PatientGender.String, "age": rec.PatientAge.Int64},
		TemplateVersionID: req.TemplateVersionID,
		DocumentType:      req.DocumentType,
		VisitType:         req.VisitType,
		DoctorID:          &access.UserID,
		StartedAt:         timePtr(rec.RecordedAt),
		Content:           map[string]any{},
	}, input)
	if err != nil {
		httputil.WriteBadRequest(w, err.Error())
		return
	}
	httputil.WriteSuccess(w, map[string]any{"data": item})
}

func timePtr(value sql.NullTime) *time.Time {
	if !value.Valid {
		return nil
	}
	t := value.Time
	return &t
}

func (h *Handler) Get(w http.ResponseWriter, r *http.Request) {
	_, tenantID, err := h.authorizeRecord(r, "record.read")
	if err != nil {
		httputil.WriteForbidden(w, err.Error())
		return
	}
	item, err := h.service.Get(r.Context(), tenantID, recordID(r))
	if err != nil {
		httputil.WriteInternalError(w, err.Error())
		return
	}
	if item == nil {
		httputil.WriteNotFound(w, "record not found")
		return
	}
	// P0-2：详情页只读聚合 HIS 全部单据；聚合失败不影响病历正文查看。
	if related, relErr := h.service.RelatedOrders(r.Context(), tenantID, item); relErr != nil {
		slog.Error("emrrecord: load related orders", "tenant_id", tenantID, "record_id", item.ID, "error", relErr)
	} else {
		item.RelatedOrders = related
	}
	httputil.WriteSuccess(w, map[string]any{"data": item})
}
func (h *Handler) Snapshots(w http.ResponseWriter, r *http.Request) {
	_, tenantID, err := h.authorizeRecord(r, "record.history.read")
	if err != nil {
		httputil.WriteForbidden(w, err.Error())
		return
	}
	items, err := h.service.Snapshots(r.Context(), tenantID, recordID(r))
	if err != nil {
		httputil.WriteInternalError(w, err.Error())
		return
	}
	httputil.WriteSuccess(w, map[string]any{"items": items})
}

// ExportAudit 导出病历留痕：操作日志、版本正文、字段变更明细三张表。
func (h *Handler) ExportAudit(w http.ResponseWriter, r *http.Request) {
	_, tenantID, err := h.authorizeRecord(r, "record.history.read")
	if err != nil {
		httputil.WriteForbidden(w, err.Error())
		return
	}
	ctx := r.Context()
	record, err := h.service.Get(ctx, tenantID, recordID(r))
	if err != nil {
		httputil.WriteInternalError(w, err.Error())
		return
	}
	snapshots, err := h.service.Snapshots(ctx, tenantID, recordID(r))
	if err != nil {
		httputil.WriteInternalError(w, err.Error())
		return
	}
	processes, err := h.service.ProcessRecords(ctx, tenantID, recordID(r))
	if err != nil {
		httputil.WriteInternalError(w, err.Error())
		return
	}
	workbook, err := buildAuditWorkbook(record, snapshots, processes)
	if err != nil {
		httputil.WriteInternalError(w, err.Error())
		return
	}
	filename := auditExportFilename(record)
	w.Header().Set("Content-Type", "application/vnd.openxmlformats-officedocument.spreadsheetml.sheet")
	w.Header().Set("Content-Disposition", fmt.Sprintf("attachment; filename=\"audit-export.xlsx\"; filename*=UTF-8''%s", url.PathEscape(filename)))
	if err := workbook.Write(w); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
	}
}

func (h *Handler) AutoSave(w http.ResponseWriter, r *http.Request) {
	access, tenantID, err := h.authorizeRecord(r, "record.edit")
	if err != nil {
		httputil.WriteForbidden(w, err.Error())
		return
	}
	var req ContentRequest
	if decodeBody(r, &req) != nil {
		httputil.WriteBadRequest(w, "invalid request body")
		return
	}
	item, err := h.service.AutoSave(r.Context(), tenantID, access.UserID, recordID(r), req.Content, req.PatientSnapshot)
	if err != nil {
		httputil.WriteBadRequest(w, err.Error())
		return
	}
	httputil.WriteSuccess(w, map[string]any{"data": item})
}
func (h *Handler) ManualSave(w http.ResponseWriter, r *http.Request) {
	access, tenantID, err := h.authorizeRecord(r, "record.edit")
	if err != nil {
		httputil.WriteForbidden(w, err.Error())
		return
	}
	var req ActionRequest
	if decodeBody(r, &req) != nil {
		httputil.WriteBadRequest(w, "invalid request body")
		return
	}
	item, err := h.service.ManualSave(r.Context(), tenantID, access.UserID, recordID(r), req)
	if err != nil {
		writeActionError(w, item, err)
		return
	}
	writeActionSuccess(w, item)
}
func (h *Handler) Submit(w http.ResponseWriter, r *http.Request) {
	h.action(w, r, "record.submit", h.service.Submit)
}

func (h *Handler) Complete(w http.ResponseWriter, r *http.Request) {
	access, tenantID, err := h.authorizeRecord(r, "record.submit")
	if err != nil {
		httputil.WriteForbidden(w, err.Error())
		return
	}
	h.actionWithAccess(w, r, tenantID, access.UserID, h.service.Complete)
}

func (h *Handler) Confirm(w http.ResponseWriter, r *http.Request) {
	h.action(w, r, "record.archive", h.service.Confirm)
}

func (h *Handler) Archive(w http.ResponseWriter, r *http.Request) {
	h.action(w, r, "record.archive", h.service.Archive)
}

func (h *Handler) Return(w http.ResponseWriter, r *http.Request) {
	h.action(w, r, "record.archive", h.service.Return)
}

func (h *Handler) action(w http.ResponseWriter, r *http.Request, ability string, action func(context.Context, int64, int64, string, ActionRequest) (*WriteOutcome, error)) {
	access, tenantID, err := h.authorizeRecord(r, ability)
	if err != nil {
		httputil.WriteForbidden(w, err.Error())
		return
	}
	var req ActionRequest
	if r.Body != nil && r.ContentLength != 0 {
		if decodeBody(r, &req) != nil {
			httputil.WriteBadRequest(w, "invalid request body")
			return
		}
	}
	h.actionWithRequest(w, r, tenantID, access.UserID, req, action)
}

func (h *Handler) actionWithAccess(w http.ResponseWriter, r *http.Request, tenantID, actorID int64, action func(context.Context, int64, int64, string, ActionRequest) (*WriteOutcome, error)) {
	var req ActionRequest
	if r.Body != nil && r.ContentLength != 0 {
		if decodeBody(r, &req) != nil {
			httputil.WriteBadRequest(w, "invalid request body")
			return
		}
	}
	h.actionWithRequest(w, r, tenantID, actorID, req, action)
}

func (h *Handler) actionWithRequest(w http.ResponseWriter, r *http.Request, tenantID, actorID int64, req ActionRequest, action func(context.Context, int64, int64, string, ActionRequest) (*WriteOutcome, error)) {
	item, err := action(r.Context(), tenantID, actorID, recordID(r), req)
	if err != nil {
		writeActionError(w, item, err)
		return
	}
	writeActionSuccess(w, item)
}

// writeActionSuccess 输出保存/提交结果。
//
// 响应统一带 ok=true；质检被降级时 item.QualityStatus 会通过 WriteOutcome.MarshalJSON
// 输出到 quality 字段（status/message），前端据此明示「本次保存未执行质检」。
func writeActionSuccess(w http.ResponseWriter, item *WriteOutcome) {
	httputil.WriteJSON(w, http.StatusOK, map[string]any{"ok": true, "data": item})
}

func writeActionError(w http.ResponseWriter, item *WriteOutcome, actionErr error) {
	details := map[string]any{}
	if item != nil && item.Record != nil {
		details["status"] = item.Record.Status
	}
	if item != nil && item.QualityReport != nil {
		details["quality"] = item.QualityReport
	}
	if errors.Is(actionErr, ErrQualityConfirmationRequired) {
		httputil.WriteError(w, http.StatusConflict, "EMR_QUALITY_CONFIRMATION_REQUIRED", actionErr.Error(), details)
		return
	}
	if gate, ok := billing.GateRejection(actionErr); ok {
		if gate.Insufficient != nil {
			details["unit"] = gate.Insufficient.Unit
			details["current"] = gate.Insufficient.Current
			details["required"] = gate.Insufficient.Required
			details["shortfall"] = gate.Insufficient.Shortfall
		}
		httputil.WriteError(w, gate.Status, gate.Code, gate.Message, details)
		return
	}
	httputil.WriteError(w, http.StatusBadRequest, "EMR_ACTION_FAILED", actionErr.Error(), details)
}
func (h *Handler) Revise(w http.ResponseWriter, r *http.Request) {
	access, tenantID, err := h.authorizeRecord(r, "record.edit")
	if err != nil {
		httputil.WriteForbidden(w, err.Error())
		return
	}
	var req ActionRequest
	_ = decodeBody(r, &req)
	item, err := h.service.Revise(r.Context(), tenantID, access.UserID, recordID(r), req.Note)
	if err != nil {
		httputil.WriteBadRequest(w, err.Error())
		return
	}
	httputil.WriteSuccess(w, map[string]any{"data": item})
}
func (h *Handler) Void(w http.ResponseWriter, r *http.Request) {
	access, tenantID, err := h.authorizeRecord(r, "record.edit")
	if err != nil {
		httputil.WriteForbidden(w, err.Error())
		return
	}
	var req ActionRequest
	_ = decodeBody(r, &req)
	item, err := h.service.Void(r.Context(), tenantID, access.UserID, recordID(r), req.Note)
	if err != nil {
		httputil.WriteBadRequest(w, err.Error())
		return
	}
	httputil.WriteSuccess(w, map[string]any{"data": item})
}

// UpdateVisitType 在正文生成前修改病历就诊类型，并按（科室, 就诊类型）重选模板。
func (h *Handler) UpdateVisitType(w http.ResponseWriter, r *http.Request) {
	access, tenantID, err := h.authorizeRecord(r, "record.edit")
	if err != nil {
		httputil.WriteForbidden(w, err.Error())
		return
	}
	var req struct {
		VisitType string `json:"visit_type"`
	}
	if decodeBody(r, &req) != nil {
		httputil.WriteBadRequest(w, "invalid request body")
		return
	}
	item, err := h.service.UpdateVisitType(r.Context(), tenantID, access.UserID, recordID(r), strings.TrimSpace(req.VisitType))
	if err != nil {
		httputil.WriteBadRequest(w, err.Error())
		return
	}
	httputil.WriteSuccess(w, map[string]any{"data": item})
}

// Regenerate 按新的就诊类型重选模板，并用同一段听写全文重新生成工作稿。
func (h *Handler) Regenerate(w http.ResponseWriter, r *http.Request) {
	access, tenantID, err := h.authorizeRecord(r, "record.edit")
	if err != nil {
		httputil.WriteForbidden(w, err.Error())
		return
	}
	var req struct {
		VisitType  string `json:"visit_type"`
		Transcript string `json:"transcript"`
	}
	if decodeBody(r, &req) != nil {
		httputil.WriteBadRequest(w, "invalid request body")
		return
	}
	req.VisitType = strings.TrimSpace(req.VisitType)
	req.Transcript = strings.TrimSpace(req.Transcript)
	if req.VisitType != "初诊" && req.VisitType != "复诊" {
		httputil.WriteBadRequest(w, "visit_type 必须是 初诊 或 复诊")
		return
	}
	if req.Transcript == "" {
		httputil.WriteBadRequest(w, "transcript is required")
		return
	}
	if h.internal == nil {
		httputil.WriteInternalError(w, "generation service not configured")
		return
	}
	record, err := h.service.Get(r.Context(), tenantID, recordID(r))
	if err != nil || record == nil {
		httputil.WriteNotFound(w, "病历不存在")
		return
	}
	departmentCode := contextText(record.EncounterContext["medical_department_code"])
	if departmentCode == "" {
		departmentCode, err = h.service.store.DoctorDepartmentCode(r.Context(), tenantID, record.DoctorID)
		if err != nil {
			httputil.WriteBadRequest(w, err.Error())
			return
		}
	}
	resolved, err := emrtemplate.ResolveCurrentTemplate(r.Context(), h.service.store.pool, tenantID, departmentCode, record.DocumentType, req.VisitType)
	if err != nil {
		httputil.WriteBadRequest(w, err.Error())
		return
	}
	if err := h.service.store.SetVisitTypeAndTemplate(r.Context(), tenantID, record.ID, req.VisitType, resolved.ID); err != nil {
		httputil.WriteBadRequest(w, err.Error())
		return
	}
	recordingID := recordingIDFromRecord(record)
	if err := h.internal.ensureDraftQuotaForRecord(r.Context(), tenantID, recordingID, record.ID); err != nil {
		httputil.WriteBadRequest(w, err.Error())
		return
	}
	payload := EmrWorkerRequest{
		TenantID:          tenantID,
		RecordingID:       recordingID,
		EncounterID:       record.EncounterID,
		EmployeeID:        access.UserID,
		CustomerID:        int64Value(record.PatientID),
		CustomerName:      contextText(record.PatientSnapshot["name"]),
		BusinessScope:     "doctor",
		RoleCode:          "doctor",
		RecordedAt:        record.StartedAt.Format(time.RFC3339),
		CleanedTranscript: req.Transcript,
		PatientSnapshot:   record.PatientSnapshot,
	}
	result, err := h.internal.RunRegeneration(r.Context(), payload)
	if err != nil {
		httputil.WriteBadRequest(w, err.Error())
		return
	}
	httputil.WriteSuccess(w, map[string]any{"data": result})
}

func contextText(value any) string {
	switch text := value.(type) {
	case string:
		return strings.TrimSpace(text)
	case fmt.Stringer:
		return strings.TrimSpace(text.String())
	}
	return ""
}

func int64Value(value *int64) int64 {
	if value == nil {
		return 0
	}
	return *value
}
