package badge

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net"
	"net/http"
	"strings"
	"time"

	"github.com/freeasyman/lingce-api/internal/employee"
	"github.com/freeasyman/lingce-api/internal/tenancy"
)

type Service struct {
	store            *Store
	employeeStore    *employee.Store
	middlewareClient *MiddlewareClient
	workerURL        string
	workerToken      string
	httpClient       *http.Client
	notifier         *TicketEmailNotifier
	sessionBinder    interface {
		BindBadgeRecordingByOrderNo(context.Context, int64, string, int64) error
		BindBadgeRecording(context.Context, int64, int64, string, int64) error
	}
}

func NewService(store *Store, employeeStore *employee.Store) *Service {
	return &Service{
		store:            store,
		employeeStore:    employeeStore,
		middlewareClient: NewMiddlewareClient("", ""),
		httpClient:       &http.Client{Timeout: 15 * time.Second},
	}
}

// SetBadgeSessionBinder connects callback ingestion to the EMR workbench
// session without changing the legacy Worker or badge middleware protocol.
func (s *Service) SetBadgeSessionBinder(binder interface {
	BindBadgeRecordingByOrderNo(context.Context, int64, string, int64) error
	BindBadgeRecording(context.Context, int64, int64, string, int64) error
}) {
	s.sessionBinder = binder
}

func NewServiceWithMiddleware(
	store *Store,
	employeeStore *employee.Store,
	middlewareURL,
	middlewareToken,
	workerURL,
	workerToken string,
	notifier *TicketEmailNotifier,
) *Service {
	return &Service{
		store:            store,
		employeeStore:    employeeStore,
		middlewareClient: NewMiddlewareClient(middlewareURL, middlewareToken),
		workerURL:        strings.TrimRight(strings.TrimSpace(workerURL), "/"),
		workerToken:      strings.TrimSpace(workerToken),
		httpClient:       &http.Client{Timeout: 15 * time.Second},
		notifier:         notifier,
	}
}

func (s *Service) ListTenantEmployees(ctx context.Context, tenantIDs []int64) ([]map[string]interface{}, error) {
	if len(tenantIDs) == 0 {
		return []map[string]interface{}{}, nil
	}
	if s.employeeStore == nil {
		return nil, fmt.Errorf("employee store is not configured")
	}

	employees, err := s.employeeStore.ListTenantEmployeesByTenantIDs(ctx, tenantIDs)
	if err != nil {
		return nil, err
	}

	resp := make([]map[string]interface{}, 0, len(employees))
	for _, emp := range employees {
		resp = append(resp, map[string]interface{}{
			"employee_id": emp.EmployeeID,
			"tenant_id":   emp.TenantID,
			"name":        emp.Name,
		})
	}
	return resp, nil
}

// Device Services

// ListDevices retrieves a paginated list of devices
func (s *Service) ListDevices(ctx context.Context, req DeviceListRequest) ([]*DeviceResponse, int, error) {
	// Set default pagination
	if req.Page <= 0 {
		req.Page = 1
	}
	if req.PageSize <= 0 {
		req.PageSize = 20
	}
	if req.PageSize > 100 {
		req.PageSize = 100
	}

	devices, total, err := s.store.ListDevices(ctx, req)
	if err != nil {
		return nil, 0, err
	}

	responses := make([]*DeviceResponse, len(devices))
	for i, d := range devices {
		responses[i] = toDeviceResponse(d)
	}

	return responses, total, nil
}

// GetDeviceByID retrieves a device by ID
func (s *Service) GetDeviceByID(ctx context.Context, id int64) (*DeviceResponse, error) {
	device, err := s.store.GetDeviceByID(ctx, id)
	if err != nil {
		return nil, err
	}

	return toDeviceResponse(device), nil
}

// GetDeviceByDeviceNo retrieves a device by device number
func (s *Service) GetDeviceByDeviceNo(ctx context.Context, deviceNo string) (*DeviceResponse, error) {
	device, err := s.store.GetDeviceByDeviceNo(ctx, deviceNo)
	if err != nil {
		return nil, err
	}

	return toDeviceResponse(device), nil
}

// Ticket Services

// CreateTicket creates a ticket
func (s *Service) CreateTicket(
	ctx context.Context,
	submitterID int64,
	requesterTenantID *int64,
	enforceTenantMatch bool,
	req TicketSubmitRequest,
) (*TicketResponse, error) {
	// Validate request
	if req.Type == "" {
		return nil, fmt.Errorf("type is required")
	}
	if req.Title == "" {
		return nil, fmt.Errorf("title is required")
	}
	if req.Description == "" {
		return nil, fmt.Errorf("description is required")
	}
	if enforceTenantMatch {
		if requesterTenantID == nil {
			return nil, fmt.Errorf("tenant_id is required")
		}
		if err := tenancy.RequirePositiveID("tenant_id", *requesterTenantID); err != nil {
			return nil, err
		}
		if req.DeviceID != nil {
			device, err := s.store.GetDeviceByID(ctx, *req.DeviceID)
			if err != nil {
				return nil, err
			}
			if device.TenantID == nil || *device.TenantID != *requesterTenantID {
				return nil, fmt.Errorf("device does not belong to current tenant")
			}
		} else if req.DeviceNo != nil && strings.TrimSpace(*req.DeviceNo) != "" {
			device, err := s.store.GetDeviceByDeviceNo(ctx, strings.TrimSpace(*req.DeviceNo))
			if err != nil {
				return nil, err
			}
			if device.TenantID == nil || *device.TenantID != *requesterTenantID {
				return nil, fmt.Errorf("device does not belong to current tenant")
			}
		}
		req.TenantID = requesterTenantID
	}

	ticket, err := s.store.CreateTicket(ctx, submitterID, req)
	if err != nil {
		return nil, err
	}
	s.notifyTicketCreated(ticket)

	return toTicketResponse(ticket), nil
}

// ListTickets retrieves a paginated list of tickets
func (s *Service) ListTickets(ctx context.Context, req TicketListRequest) ([]*TicketResponse, int, error) {
	// Set default pagination
	if req.Page <= 0 {
		req.Page = 1
	}
	if req.PageSize <= 0 {
		req.PageSize = 20
	}
	if req.PageSize > 100 {
		req.PageSize = 100
	}

	tickets, total, err := s.store.ListTickets(ctx, req)
	if err != nil {
		return nil, 0, err
	}

	responses := make([]*TicketResponse, len(tickets))
	for i, t := range tickets {
		responses[i] = toTicketResponse(t)
	}

	return responses, total, nil
}

// GetTicketByID retrieves a ticket by ID
func (s *Service) GetTicketByID(ctx context.Context, id int64) (*TicketResponse, error) {
	ticket, err := s.store.GetTicketByID(ctx, id)
	if err != nil {
		return nil, err
	}

	return toTicketResponse(ticket), nil
}

// ReviewTicket reviews a ticket
func (s *Service) ReviewTicket(ctx context.Context, id, reviewerID int64, approved bool, notes *string) error {
	return s.store.ReviewTicket(ctx, id, reviewerID, approved, notes)
}

// ExecuteTicket executes a ticket. 我方问题的换新会在这里完成换机并留下设备号/物流单号。
func (s *Service) ExecuteTicket(ctx context.Context, id, executorID int64, success bool, notes *string, resolutionType string, swapDeviceID *int64, outboundTracking, inboundTracking string) error {
	ticket, err := s.store.GetTicketByID(ctx, id)
	if err != nil {
		return err
	}
	if success && resolutionType == "our_issue" {
		swapDeviceNo := ""
		if swapDeviceID != nil && *swapDeviceID > 0 {
			if ticket.DeviceID == nil || *ticket.DeviceID <= 0 {
				return fmt.Errorf("工单没有关联设备，无法换新")
			}
			tenantID := int64(0)
			if ticket.TenantID != nil {
				tenantID = *ticket.TenantID
			}
			device, err := s.store.RebuildSwapBadgeDevices(ctx, *ticket.DeviceID, *swapDeviceID, "报修换新", executorID, "", &tenantID)
			if err != nil {
				return err
			}
			if device != nil && device.DeviceNo != "" {
				swapDeviceNo = device.DeviceNo
			}
		}
		if swapDeviceNo == "" && strings.TrimSpace(outboundTracking) == "" {
			return fmt.Errorf("我方问题换新时，请填写换上的设备号或物流单号")
		}
		if err := s.store.SetTicketReplacement(ctx, id, resolutionType, swapDeviceID, swapDeviceNo, outboundTracking, inboundTracking); err != nil {
			return err
		}
	}
	return s.store.ExecuteTicket(ctx, id, executorID, success, notes)
}

func (s *Service) notifyTicketCreated(ticket *BadgeTicket) {
	if s.notifier == nil || ticket == nil {
		return
	}
	go func(t BadgeTicket) {
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		if err := s.notifier.SendTicketCreated(ctx, &t); err != nil {
			slog.Warn("failed to send badge ticket notification email", "ticket_id", t.ID, "ticket_no", t.TicketNo, "error", err)
			return
		}
		slog.Info("badge ticket notification email sent", "ticket_id", t.ID, "ticket_no", t.TicketNo)
	}(*ticket)
}

// AdminDisplayName 取运营账号显示名（回复客户时署名）。
func (s *Service) AdminDisplayName(ctx context.Context, adminID int64) (string, error) {
	var name string
	err := s.store.pool.QueryRow(ctx, `
		SELECT COALESCE(NULLIF(trim(name), ''), NULLIF(trim(username), ''), '') FROM operations_admins WHERE id = $1
	`, adminID).Scan(&name)
	return name, err
}

// ReplyTicket 保存运营回复并邮件通知客户；失败时仍保留消息，返回 emailed=false。
func (s *Service) ReplyTicket(ctx context.Context, ticketID, authorID int64, authorName, body string) (*BadgeTicketMessage, bool, error) {
	body = strings.TrimSpace(body)
	if body == "" {
		return nil, false, fmt.Errorf("回复内容不能为空")
	}
	ticket, err := s.store.GetTicketByID(ctx, ticketID)
	if err != nil {
		return nil, false, err
	}
	emailed := false
	var emailedAt *time.Time
	if s.notifier != nil {
		to := []string{}
		if email, err := s.store.TicketContactEmail(ctx, ticket); err == nil && email != "" {
			to = append(to, email)
		}
		if err := s.notifier.SendTicketReply(ctx, ticket, body, to); err != nil {
			slog.Warn("failed to send badge ticket reply email", "ticket_id", ticketID, "error", err)
		} else {
			emailed = true
			now := time.Now()
			emailedAt = &now
		}
	}
	msg, err := s.store.AddTicketMessage(ctx, ticketID, "ops", &authorID, authorName, body, emailedAt)
	if err != nil {
		return nil, emailed, err
	}
	return msg, emailed, nil
}

// ListTicketMessages 返回工单的消息线程。
func (s *Service) ListTicketMessages(ctx context.Context, ticketID int64) ([]*BadgeTicketMessage, error) {
	return s.store.ListTicketMessages(ctx, ticketID)
}

// GetDashboardSummary retrieves dashboard summary
// Recording Control Services

// StartRecording starts recording
func (s *Service) StartRecording(ctx context.Context, deviceNo string, operatorID *int64, extraData JSONObject) error {
	return s.controlRecording(ctx, "start", deviceNo, operatorID, extraData)
}

// StopRecording stops recording
func (s *Service) StopRecording(ctx context.Context, deviceNo string, operatorID *int64, extraData JSONObject) error {
	return s.controlRecording(ctx, "stop", deviceNo, operatorID, extraData)
}

func (s *Service) controlRecording(ctx context.Context, action, deviceNo string, operatorID *int64, extraData JSONObject) error {
	if strings.TrimSpace(deviceNo) == "" {
		return fmt.Errorf("device_no is required")
	}

	err := s.middlewareClient.ControlRecording(ctx, action, deviceNo, operatorID, extraData)
	if err != nil && shouldRetryRecordingControl(err) {
		// Start/stop may hit transient upstream timeout from vendor API; retry with short backoff.
		time.Sleep(800 * time.Millisecond)
		err = s.middlewareClient.ControlRecording(ctx, action, deviceNo, operatorID, extraData)
		if err != nil && shouldRetryRecordingControl(err) {
			time.Sleep(1500 * time.Millisecond)
			err = s.middlewareClient.ControlRecording(ctx, action, deviceNo, operatorID, extraData)
		}
	}
	if err != nil {
		msg := err.Error()
		if logErr := s.store.CreateRecordingControlLog(ctx, deviceNo, action, "failed", operatorID, &msg, extraData); logErr != nil {
			return fmt.Errorf("badge-middleware call failed: %v; failed to create recording control log: %w", err, logErr)
		}
		return fmt.Errorf("failed to %s recording via badge-middleware: %w", action, err)
	}

	return s.store.CreateRecordingControlLog(ctx, deviceNo, action, "success", operatorID, nil, extraData)
}

func shouldRetryRecordingControl(err error) bool {
	if err == nil {
		return false
	}
	var netErr net.Error
	if errors.As(err, &netErr) {
		return netErr.Timeout() || netErr.Temporary()
	}
	msg := strings.ToLower(err.Error())
	return strings.Contains(msg, "context deadline exceeded") ||
		strings.Contains(msg, "client.timeout exceeded") ||
		strings.Contains(msg, "timeout")
}

// ListRecordingControlLogs retrieves a paginated list of recording control logs
func (s *Service) ListRecordingControlLogs(ctx context.Context, req RecordingControlLogListRequest) ([]*RecordingControlLogResponse, int, error) {
	// Set default pagination
	if req.Page <= 0 {
		req.Page = 1
	}
	if req.PageSize <= 0 {
		req.PageSize = 20
	}
	if req.PageSize > 100 {
		req.PageSize = 100
	}

	logs, total, err := s.store.ListRecordingControlLogs(ctx, req)
	if err != nil {
		return nil, 0, err
	}

	responses := make([]*RecordingControlLogResponse, len(logs))
	for i, l := range logs {
		responses[i] = toRecordingControlLogResponse(l)
	}

	return responses, total, nil
}

func (s *Service) GetLatestAudioEventStatus(ctx context.Context, deviceNo string) (bool, bool, error) {
	deviceNo = strings.TrimSpace(deviceNo)
	if deviceNo == "" {
		return false, false, fmt.Errorf("device_no is required")
	}
	return s.store.GetLatestAudioEventStatus(ctx, deviceNo)
}

// CreateCallbackLog stores callback event as a recording-control log record.
func (s *Service) CreateCallbackLog(ctx context.Context, payload CallbackPayload, source string) error {
	extra := payload.Data
	if extra == nil {
		extra = JSONObject{}
	}
	extra["source"] = source
	extra["event_type"] = payload.EventType

	// Device may not exist yet (e.g. upstream callback arrives before local sync),
	// keep callback observability by logging best-effort.
	if device, err := s.store.GetDeviceByDeviceNo(ctx, payload.DeviceNo); err == nil && device != nil {
		extra["device_id"] = device.ID
	}

	if err := s.store.CreateRecordingControlLog(ctx, payload.DeviceNo, "callback", "success", nil, nil, extra); err != nil {
		return err
	}

	// AUDIO callback should return quickly. Persist callback event first, then
	// process the heavy recording pipeline asynchronously.
	if !isAudioCallback(payload) {
		return nil
	}
	eventRef, err := s.store.UpsertAudioCallbackEvent(ctx, payload)
	if err != nil {
		return err
	}
	if eventRef == nil {
		return nil
	}
	core, _, err := buildAudioCallbackCoreFields(payload)
	if err != nil {
		return err
	}
	if core != nil && !core.ReadyForIngest {
		slog.Warn("audio callback accepted with incomplete payload",
			"device_no", eventRef.DeviceNo,
			"app_id", eventRef.AppID,
			"event_id", eventRef.EventID,
			"order_no", eventRef.OrderNo,
			"reason", core.ValidationError,
		)
		return nil
	}
	slog.Info("audio callback accepted for async processing",
		"device_no", eventRef.DeviceNo,
		"app_id", eventRef.AppID,
		"event_id", eventRef.EventID,
		"order_no", eventRef.OrderNo,
	)
	go s.processAudioCallbackAsync(*eventRef, payload)
	return nil
}

func (s *Service) processAudioCallbackAsync(eventRef AudioCallbackEventRef, payload CallbackPayload) {
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Minute)
	defer cancel()

	claimed, err := s.store.ClaimAudioCallbackEvent(ctx, eventRef.AppID, eventRef.EventID)
	if err != nil {
		slog.Error("audio callback async claim failed",
			"device_no", eventRef.DeviceNo,
			"app_id", eventRef.AppID,
			"event_id", eventRef.EventID,
			"order_no", eventRef.OrderNo,
			"error", err,
		)
		return
	}
	if !claimed {
		slog.Info("audio callback async skip unclaimable event",
			"device_no", eventRef.DeviceNo,
			"app_id", eventRef.AppID,
			"event_id", eventRef.EventID,
			"order_no", eventRef.OrderNo,
		)
		return
	}

	ingestResult, err := s.store.UpsertRecordingFromAudioCallback(ctx, payload)
	if err != nil {
		_ = s.store.MarkAudioCallbackEventFailed(ctx, eventRef.AppID, eventRef.EventID, err.Error())
		slog.Error("audio callback async ingest failed",
			"device_no", eventRef.DeviceNo,
			"app_id", eventRef.AppID,
			"event_id", eventRef.EventID,
			"order_no", eventRef.OrderNo,
			"error", err,
		)
		return
	}
	if ingestResult == nil {
		_ = s.store.MarkAudioCallbackEventCompleted(ctx, eventRef.AppID, eventRef.EventID, 0)
		return
	}
	if s.sessionBinder != nil {
		if err := s.sessionBinder.BindBadgeRecordingByOrderNo(ctx, ingestResult.TenantID, eventRef.OrderNo, ingestResult.RecordingID); err != nil {
			slog.Warn("failed to bind badge recording to EMR session", "order_no", eventRef.OrderNo, "recording_id", ingestResult.RecordingID, "error", err)
		}
		// Some legacy vendor callbacks replace the workbench order number with
		// their event id. Bind the only stopped, unbound session for this doctor
		// and device so the generated Encounter can still be returned to the UI.
		if err := s.sessionBinder.BindBadgeRecording(ctx, ingestResult.TenantID, ingestResult.EmployeeID, eventRef.DeviceNo, ingestResult.RecordingID); err != nil {
			slog.Warn("failed to fallback-bind badge recording to EMR session", "device_no", eventRef.DeviceNo, "recording_id", ingestResult.RecordingID, "error", err)
		}
	}
	if err := s.store.MarkAudioCallbackEventCompleted(ctx, eventRef.AppID, eventRef.EventID, ingestResult.RecordingID); err != nil {
		slog.Warn("audio callback async mark completed failed",
			"device_no", eventRef.DeviceNo,
			"app_id", eventRef.AppID,
			"event_id", eventRef.EventID,
			"recording_id", ingestResult.RecordingID,
			"error", err,
		)
	}
	if !ingestResult.Created {
		slog.Info("skip transcribe enqueue for duplicate audio callback",
			"recording_id", ingestResult.RecordingID,
			"tenant_id", ingestResult.TenantID,
			"device_no", eventRef.DeviceNo,
		)
		return
	}
	if err := s.enqueueTranscribeJob(ctx, ingestResult.RecordingID, ingestResult.TenantID); err != nil {
		slog.Warn("failed to enqueue worker transcribe job from audio callback",
			"recording_id", ingestResult.RecordingID,
			"tenant_id", ingestResult.TenantID,
			"error", err,
		)
	}
}

func (s *Service) enqueueTranscribeJob(ctx context.Context, recordingID, tenantID int64) error {
	if err := tenancy.RequirePositiveID("recording_id", recordingID); err != nil {
		return nil
	}
	if err := tenancy.RequirePositiveID("tenant_id", tenantID); err != nil {
		return nil
	}
	if s.workerURL == "" {
		return fmt.Errorf("lingce-worker url is not configured")
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, fmt.Sprintf("%s/internal/jobs/enqueue?recording_id=%d&job_type=transcribe&trigger_source=badge_callback", s.workerURL, recordingID), nil)
	if err != nil {
		return fmt.Errorf("create lingce-worker request: %w", err)
	}
	if s.workerToken != "" {
		req.Header.Set("X-Internal-Token", s.workerToken)
	}
	resp, err := s.httpClient.Do(req)
	if err != nil {
		return fmt.Errorf("call lingce-worker: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode < http.StatusOK || resp.StatusCode >= http.StatusMultipleChoices {
		return fmt.Errorf("lingce-worker status=%d", resp.StatusCode)
	}
	return nil
}

// Helper functions

// toDeviceResponse converts a BadgeDevice to DeviceResponse
func toDeviceResponse(d *BadgeDevice) *DeviceResponse {
	resp := &DeviceResponse{
		ID:               d.ID,
		DeviceNo:         d.DeviceNo,
		ManufacturerCode: d.ManufacturerCode,
		ManufacturerName: "", // TODO: Join with manufacturers table
		HardwareModel:    d.HardwareModel,
		Status:           d.Status,
		TenantID:         d.TenantID,
		TenantName:       nil, // TODO: Join with tenants table
		EmployeeID:       d.EmployeeID,
		EmployeeName:     nil, // TODO: Join with employees table
		BatteryLevel:     d.BatteryLevel,
		FirmwareVersion:  d.FirmwareVersion,
		ExtraData:        d.ExtraData,
		CreatedAt:        d.CreatedAt.Format("2006-01-02T15:04:05Z07:00"),
		UpdatedAt:        d.UpdatedAt.Format("2006-01-02T15:04:05Z07:00"),
	}

	if d.AcceptedAt != nil {
		formatted := d.AcceptedAt.Format("2006-01-02T15:04:05Z07:00")
		resp.AcceptedAt = &formatted
	}

	if d.LastOnlineAt != nil {
		formatted := d.LastOnlineAt.Format("2006-01-02T15:04:05Z07:00")
		resp.LastOnlineAt = &formatted
	}

	return resp
}

// toTicketResponse converts a BadgeTicket to TicketResponse
func toTicketResponse(t *BadgeTicket) *TicketResponse {
	submitterName := ""
	if t.SubmitterName != nil {
		submitterName = *t.SubmitterName
	}
	resp := &TicketResponse{
		ID:                 t.ID,
		TicketNo:           t.TicketNo,
		Type:               t.Type,
		Status:             t.Status,
		DeviceID:           t.DeviceID,
		DeviceNo:           t.DeviceNo,
		TenantID:           t.TenantID,
		TenantName:         t.TenantName,
		EmployeeID:         t.EmployeeID,
		EmployeeName:       t.EmployeeName,
		SubmitterID:        t.SubmitterID,
		SubmitterName:      submitterName,
		ReviewerID:         t.ReviewerID,
		ReviewerName:       t.ReviewerName,
		ExecutorID:         t.ExecutorID,
		ExecutorName:       t.ExecutorName,
		Title:              t.Title,
		Description:        t.Description,
		ReviewNotes:        t.ReviewNotes,
		ExecuteNotes:       t.ExecuteNotes,
		SubmittedAt:        t.SubmittedAt.Format("2006-01-02T15:04:05Z07:00"),
		ExtraData:          t.ExtraData,
		ResolutionType:     t.ResolutionType,
		SwapDeviceID:       t.SwapDeviceID,
		SwapDeviceNo:       t.SwapDeviceNo,
		OutboundTrackingNo: t.OutboundTrackingNo,
		InboundTrackingNo:  t.InboundTrackingNo,
		CreatedAt:          t.CreatedAt.Format("2006-01-02T15:04:05Z07:00"),
		UpdatedAt:          t.UpdatedAt.Format("2006-01-02T15:04:05Z07:00"),
	}

	if t.ReviewedAt != nil {
		formatted := t.ReviewedAt.Format("2006-01-02T15:04:05Z07:00")
		resp.ReviewedAt = &formatted
	}

	if t.ExecutedAt != nil {
		formatted := t.ExecutedAt.Format("2006-01-02T15:04:05Z07:00")
		resp.ExecutedAt = &formatted
	}

	if t.ShippedAt != nil {
		formatted := t.ShippedAt.Format("2006-01-02T15:04:05Z07:00")
		resp.ShippedAt = &formatted
	}

	if t.CompletedAt != nil {
		formatted := t.CompletedAt.Format("2006-01-02T15:04:05Z07:00")
		resp.CompletedAt = &formatted
	}

	return resp
}

// toRecordingControlLogResponse converts a BadgeRecordingControlLog to RecordingControlLogResponse
func toRecordingControlLogResponse(l *BadgeRecordingControlLog) *RecordingControlLogResponse {
	return &RecordingControlLogResponse{
		ID:           l.ID,
		DeviceNo:     l.DeviceNo,
		DeviceID:     l.DeviceID,
		TenantID:     l.TenantID,
		TenantName:   nil, // TODO: Join with tenants table
		EmployeeID:   l.EmployeeID,
		EmployeeName: nil, // TODO: Join with employees table
		Action:       l.Action,
		Status:       l.Status,
		OperatorID:   l.OperatorID,
		OperatorName: nil, // TODO: Join with users table
		ErrorMsg:     l.ErrorMsg,
		ExtraData:    l.ExtraData,
		CreatedAt:    l.CreatedAt.Format("2006-01-02T15:04:05Z07:00"),
	}
}

// toManufacturerResponse converts a BadgeManufacturer to ManufacturerResponse
func toManufacturerResponse(m *BadgeManufacturer) *ManufacturerResponse {
	return &ManufacturerResponse{
		ID:            m.ID,
		Code:          m.Code,
		Name:          m.Name,
		ContactPerson: m.ContactPerson,
		ContactPhone:  m.ContactPhone,
		ContactEmail:  m.ContactEmail,
		IsActive:      m.IsActive,
		Config:        m.Config,
		CreatedAt:     m.CreatedAt.Format("2006-01-02T15:04:05Z07:00"),
		UpdatedAt:     m.UpdatedAt.Format("2006-01-02T15:04:05Z07:00"),
	}
}

// toLifecycleLogResponse converts a BadgeDeviceLifecycleLog to LifecycleLogResponse
func toLifecycleLogResponse(l *BadgeDeviceLifecycleLog) *LifecycleLogResponse {
	return &LifecycleLogResponse{
		ID:           l.ID,
		DeviceID:     l.DeviceID,
		Action:       l.Action,
		FromStatus:   l.FromStatus,
		ToStatus:     l.ToStatus,
		TenantID:     l.TenantID,
		TenantName:   nil, // TODO: Join with tenants table
		EmployeeID:   l.EmployeeID,
		EmployeeName: nil, // TODO: Join with employees table
		OperatorID:   l.OperatorID,
		OperatorName: "", // TODO: Join with users table
		Notes:        l.Notes,
		ExtraData:    l.ExtraData,
		CreatedAt:    l.CreatedAt.Format("2006-01-02T15:04:05Z07:00"),
	}
}
