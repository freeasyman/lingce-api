package badge

import (
	"context"
	"fmt"
)

type Service struct {
	store *Store
}

func NewService(store *Store) *Service {
	return &Service{store: store}
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

// AcceptDevices accepts devices
func (s *Service) AcceptDevices(ctx context.Context, devices []AcceptanceDeviceInput, operatorID int64) error {
	// Validate request
	if len(devices) == 0 {
		return fmt.Errorf("no devices to accept")
	}

	for _, device := range devices {
		if device.DeviceNo == "" {
			return fmt.Errorf("device_no is required")
		}
		if device.ManufacturerCode == "" {
			return fmt.Errorf("manufacturer_code is required")
		}
	}

	return s.store.AcceptDevices(ctx, devices, operatorID)
}

// AssignToTenant assigns devices to tenant
func (s *Service) AssignToTenant(ctx context.Context, deviceIDs []int64, tenantID, operatorID int64) error {
	if len(deviceIDs) == 0 {
		return fmt.Errorf("no devices to assign")
	}

	return s.store.AssignToTenant(ctx, deviceIDs, tenantID, operatorID)
}

// AssignToEmployee assigns devices to employee
func (s *Service) AssignToEmployee(ctx context.Context, deviceIDs []int64, employeeID, operatorID int64) error {
	if len(deviceIDs) == 0 {
		return fmt.Errorf("no devices to assign")
	}

	return s.store.AssignToEmployee(ctx, deviceIDs, employeeID, operatorID)
}

// ReclaimFromEmployee reclaims devices from employee
func (s *Service) ReclaimFromEmployee(ctx context.Context, deviceIDs []int64, operatorID int64, notes *string) error {
	if len(deviceIDs) == 0 {
		return fmt.Errorf("no devices to reclaim")
	}

	return s.store.ReclaimFromEmployee(ctx, deviceIDs, operatorID, notes)
}

// ReclaimFromTenant reclaims devices from tenant
func (s *Service) ReclaimFromTenant(ctx context.Context, deviceIDs []int64, operatorID int64, notes *string) error {
	if len(deviceIDs) == 0 {
		return fmt.Errorf("no devices to reclaim")
	}

	return s.store.ReclaimFromTenant(ctx, deviceIDs, operatorID, notes)
}

// Ticket Services

// CreateTicket creates a ticket
func (s *Service) CreateTicket(ctx context.Context, submitterID int64, req TicketSubmitRequest) (*TicketResponse, error) {
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

	ticket, err := s.store.CreateTicket(ctx, submitterID, req)
	if err != nil {
		return nil, err
	}

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

// ExecuteTicket executes a ticket
func (s *Service) ExecuteTicket(ctx context.Context, id, executorID int64, notes *string) error {
	return s.store.ExecuteTicket(ctx, id, executorID, notes)
}

// GetDashboardSummary retrieves dashboard summary
func (s *Service) GetDashboardSummary(ctx context.Context) (*DashboardSummaryResponse, error) {
	return s.store.GetDashboardSummary(ctx)
}

// Recording Control Services

// StartRecording starts recording
func (s *Service) StartRecording(ctx context.Context, deviceNo string, operatorID *int64, extraData JSONObject) error {
	// TODO: Call badge-middleware API to start recording
	// For now, just log the action
	return s.store.CreateRecordingControlLog(ctx, deviceNo, "start", "success", operatorID, nil, extraData)
}

// StopRecording stops recording
func (s *Service) StopRecording(ctx context.Context, deviceNo string, operatorID *int64, extraData JSONObject) error {
	// TODO: Call badge-middleware API to stop recording
	// For now, just log the action
	return s.store.CreateRecordingControlLog(ctx, deviceNo, "stop", "success", operatorID, nil, extraData)
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

// Manufacturer Services

// ListManufacturers retrieves all manufacturers
func (s *Service) ListManufacturers(ctx context.Context) ([]*ManufacturerResponse, error) {
	manufacturers, err := s.store.ListManufacturers(ctx)
	if err != nil {
		return nil, err
	}

	responses := make([]*ManufacturerResponse, len(manufacturers))
	for i, m := range manufacturers {
		responses[i] = toManufacturerResponse(m)
	}

	return responses, nil
}

// GetManufacturerByCode retrieves a manufacturer by code
func (s *Service) GetManufacturerByCode(ctx context.Context, code string) (*ManufacturerResponse, error) {
	manufacturer, err := s.store.GetManufacturerByCode(ctx, code)
	if err != nil {
		return nil, err
	}

	return toManufacturerResponse(manufacturer), nil
}

// UpdateManufacturerConfig updates manufacturer config
func (s *Service) UpdateManufacturerConfig(ctx context.Context, code string, config JSONObject) error {
	return s.store.UpdateManufacturerConfig(ctx, code, config)
}

// GetLifecycleLogs retrieves lifecycle logs for a device
func (s *Service) GetLifecycleLogs(ctx context.Context, deviceID int64) ([]*LifecycleLogResponse, error) {
	logs, err := s.store.GetLifecycleLogs(ctx, deviceID)
	if err != nil {
		return nil, err
	}

	responses := make([]*LifecycleLogResponse, len(logs))
	for i, l := range logs {
		responses[i] = toLifecycleLogResponse(l)
	}

	return responses, nil
}

// Helper functions

// toDeviceResponse converts a BadgeDevice to DeviceResponse
func toDeviceResponse(d *BadgeDevice) *DeviceResponse {
	resp := &DeviceResponse{
		ID:               d.ID,
		DeviceNo:         d.DeviceNo,
		DeviceID:         d.DeviceID,
		ManufacturerCode: d.ManufacturerCode,
		ManufacturerName: "", // TODO: Join with manufacturers table
		Model:            d.Model,
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

	if d.AssignedToTenantAt != nil {
		formatted := d.AssignedToTenantAt.Format("2006-01-02T15:04:05Z07:00")
		resp.AssignedToTenantAt = &formatted
	}

	if d.AssignedToEmpAt != nil {
		formatted := d.AssignedToEmpAt.Format("2006-01-02T15:04:05Z07:00")
		resp.AssignedToEmpAt = &formatted
	}

	if d.LastOnlineAt != nil {
		formatted := d.LastOnlineAt.Format("2006-01-02T15:04:05Z07:00")
		resp.LastOnlineAt = &formatted
	}

	return resp
}

// toTicketResponse converts a BadgeTicket to TicketResponse
func toTicketResponse(t *BadgeTicket) *TicketResponse {
	resp := &TicketResponse{
		ID:            t.ID,
		TicketNo:      t.TicketNo,
		Type:          t.Type,
		Status:        t.Status,
		DeviceID:      t.DeviceID,
		DeviceNo:      t.DeviceNo,
		TenantID:      t.TenantID,
		TenantName:    nil, // TODO: Join with tenants table
		EmployeeID:    t.EmployeeID,
		EmployeeName:  nil, // TODO: Join with employees table
		SubmitterID:   t.SubmitterID,
		SubmitterName: "", // TODO: Join with users table
		ReviewerID:    t.ReviewerID,
		ReviewerName:  nil, // TODO: Join with users table
		ExecutorID:    t.ExecutorID,
		ExecutorName:  nil, // TODO: Join with users table
		Title:         t.Title,
		Description:   t.Description,
		ReviewNotes:   t.ReviewNotes,
		ExecuteNotes:  t.ExecuteNotes,
		SubmittedAt:   t.SubmittedAt.Format("2006-01-02T15:04:05Z07:00"),
		ExtraData:     t.ExtraData,
		CreatedAt:     t.CreatedAt.Format("2006-01-02T15:04:05Z07:00"),
		UpdatedAt:     t.UpdatedAt.Format("2006-01-02T15:04:05Z07:00"),
	}

	if t.ReviewedAt != nil {
		formatted := t.ReviewedAt.Format("2006-01-02T15:04:05Z07:00")
		resp.ReviewedAt = &formatted
	}

	if t.ExecutedAt != nil {
		formatted := t.ExecutedAt.Format("2006-01-02T15:04:05Z07:00")
		resp.ExecutedAt = &formatted
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
