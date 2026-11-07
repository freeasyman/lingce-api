package his

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/freeasyman/lingce-api/internal/emrrecord"
	"github.com/jackc/pgx/v5"
)

// ErrInvalidRequest 表示请求参数不满足业务规则。
var ErrInvalidRequest = errors.New("invalid request")

// ErrNotFound 表示目标记录不存在。
var ErrNotFound = errors.New("not found")

// ErrScheduleHasBookings 表示排班已有已约/已挂号号源，禁止修改时间或间隔。
var ErrScheduleHasBookings = errors.New("schedule has bookings")

// ErrScheduleNotOpen 表示排班未开放（已停诊）。
var ErrScheduleNotOpen = errors.New("schedule not open")

// ErrSlotUnavailable 表示号源已被占用或不可用。
var ErrSlotUnavailable = errors.New("slot unavailable")

// ErrInvalidState 表示当前状态不允许该操作。
var ErrInvalidState = errors.New("invalid state")

// ErrDispensedCannotRefund 表示处方已发药，不允许退费。
var ErrDispensedCannotRefund = errors.New("dispensed prescription cannot be refunded")

// ErrInsufficientStock 表示库存不足。
var ErrInsufficientStock = errors.New("insufficient stock")

// ErrSignatureMismatch 表示文书内容与签名时的哈希不一致（内容被改动）。
var ErrSignatureMismatch = errors.New("signature mismatch")

// ErrForbiddenRole 表示员工没有执行该操作的角色权限。
var ErrForbiddenRole = errors.New("forbidden role")

// Service 是 HIS 域的业务层。
type Service struct {
	store *Store
	emr   *emrrecord.Service
}

// NewService 创建 Service。emr 为共享病历域服务（HIS 复用其病历生命周期）。
func NewService(store *Store, emr ...*emrrecord.Service) *Service {
	service := &Service{store: store}
	if len(emr) > 0 {
		service.emr = emr[0]
	}
	return service
}

// DashboardMetrics 返回门户卡片指标。
func (s *Service) DashboardMetrics(ctx context.Context, tenantID int64) (DashboardMetrics, error) {
	return s.store.DashboardMetrics(ctx, tenantID)
}

// ---------------------------------------------------------------------------
// 医生

func (s *Service) ListDoctors(ctx context.Context, tenantID int64) ([]Doctor, error) {
	return s.store.ListDoctors(ctx, tenantID)
}

func (s *Service) CreateDoctor(ctx context.Context, tenantID int64, req CreateDoctorRequest) (Doctor, error) {
	if req.EmployeeID <= 0 {
		return Doctor{}, ErrInvalidRequest
	}
	return s.store.CreateDoctor(ctx, tenantID, req)
}

func (s *Service) UpdateDoctor(ctx context.Context, tenantID, id int64, req UpdateDoctorRequest) (Doctor, error) {
	doctor, err := s.store.UpdateDoctor(ctx, tenantID, id, req)
	if err != nil {
		if err == pgx.ErrNoRows {
			return Doctor{}, ErrNotFound
		}
		return Doctor{}, err
	}
	return doctor, nil
}

// ---------------------------------------------------------------------------
// 房间

func (s *Service) ListRooms(ctx context.Context, tenantID int64) ([]Room, error) {
	return s.store.ListRooms(ctx, tenantID)
}

func (s *Service) CreateRoom(ctx context.Context, tenantID int64, req CreateRoomRequest) (Room, error) {
	if strings.TrimSpace(req.Name) == "" {
		return Room{}, ErrInvalidRequest
	}
	return s.store.CreateRoom(ctx, tenantID, req)
}

func (s *Service) UpdateRoom(ctx context.Context, tenantID, id int64, req UpdateRoomRequest) (Room, error) {
	room, err := s.store.UpdateRoom(ctx, tenantID, id, req)
	if err != nil {
		if err == pgx.ErrNoRows {
			return Room{}, ErrNotFound
		}
		return Room{}, err
	}
	return room, nil
}

// ---------------------------------------------------------------------------
// 药品

func (s *Service) ListDrugs(ctx context.Context, tenantID int64, keyword string) ([]Drug, error) {
	return s.store.ListDrugs(ctx, tenantID, keyword)
}

func (s *Service) CreateDrug(ctx context.Context, tenantID int64, req CreateDrugRequest) (Drug, error) {
	if strings.TrimSpace(req.Code) == "" || strings.TrimSpace(req.Name) == "" {
		return Drug{}, ErrInvalidRequest
	}
	return s.store.CreateDrug(ctx, tenantID, req)
}

func (s *Service) UpdateDrug(ctx context.Context, tenantID, id int64, req UpdateDrugRequest) (Drug, error) {
	drug, err := s.store.UpdateDrug(ctx, tenantID, id, req)
	if err != nil {
		if err == pgx.ErrNoRows {
			return Drug{}, ErrNotFound
		}
		return Drug{}, err
	}
	return drug, nil
}

// ---------------------------------------------------------------------------
// 原子收费项目

func (s *Service) ListChargeItems(ctx context.Context, tenantID int64) ([]ChargeItem, error) {
	return s.store.ListChargeItems(ctx, tenantID)
}

func (s *Service) CreateChargeItem(ctx context.Context, tenantID int64, req CreateChargeItemRequest) (ChargeItem, error) {
	if strings.TrimSpace(req.Code) == "" || strings.TrimSpace(req.Name) == "" {
		return ChargeItem{}, ErrInvalidRequest
	}
	return s.store.CreateChargeItem(ctx, tenantID, req)
}

func (s *Service) UpdateChargeItem(ctx context.Context, tenantID, id int64, req UpdateChargeItemRequest) (ChargeItem, error) {
	item, err := s.store.UpdateChargeItem(ctx, tenantID, id, req)
	if err != nil {
		if err == pgx.ErrNoRows {
			return ChargeItem{}, ErrNotFound
		}
		return ChargeItem{}, err
	}
	return item, nil
}

// ---------------------------------------------------------------------------
// 打包产品

func (s *Service) ListBundles(ctx context.Context, tenantID int64) ([]Bundle, error) {
	return s.store.ListBundles(ctx, tenantID)
}

func (s *Service) GetBundle(ctx context.Context, tenantID, id int64) (Bundle, error) {
	bundle, err := s.store.GetBundle(ctx, tenantID, id)
	if err != nil {
		if err == pgx.ErrNoRows {
			return Bundle{}, ErrNotFound
		}
		return Bundle{}, err
	}
	return bundle, nil
}

func (s *Service) CreateBundle(ctx context.Context, tenantID int64, req CreateBundleRequest) (Bundle, error) {
	if strings.TrimSpace(req.Name) == "" {
		return Bundle{}, ErrInvalidRequest
	}
	if len(req.Items) == 0 {
		return Bundle{}, ErrInvalidRequest
	}
	for _, item := range req.Items {
		if item.ChargeItemID <= 0 || item.Quantity <= 0 {
			return Bundle{}, ErrInvalidRequest
		}
	}
	return s.store.CreateBundle(ctx, tenantID, req)
}

func (s *Service) UpdateBundle(ctx context.Context, tenantID, id int64, req UpdateBundleRequest) (Bundle, error) {
	bundle, err := s.store.UpdateBundle(ctx, tenantID, id, req)
	if err != nil {
		if err == pgx.ErrNoRows {
			return Bundle{}, ErrNotFound
		}
		return Bundle{}, err
	}
	return bundle, nil
}

// ---------------------------------------------------------------------------
// 患者建档与病案号

// RegisterPatient 建档：手机号精确查重 → 已存在则复用并补发病案号，否则建档 + 首诊发放病案号。
func (s *Service) RegisterPatient(ctx context.Context, tenantID, createdBy int64, req RegisterPatientRequest) (RegisterPatientResponse, error) {
	if strings.TrimSpace(req.Name) == "" || strings.TrimSpace(req.Phone) == "" {
		return RegisterPatientResponse{}, ErrInvalidRequest
	}
	phone := strings.TrimSpace(req.Phone)

	customerID, err := s.store.FindCustomerByPhone(ctx, tenantID, phone)
	isExisting := true
	if err == pgx.ErrNoRows {
		customerID, err = s.store.CreateCustomer(ctx, tenantID, createdBy, req)
		isExisting = false
	}
	if err != nil {
		return RegisterPatientResponse{}, err
	}

	mrn, err := s.store.EnsureMRN(ctx, tenantID, customerID)
	if err != nil {
		return RegisterPatientResponse{}, err
	}
	identifiers, err := s.store.ListPatientIdentifiers(ctx, tenantID, customerID)
	if err != nil {
		return RegisterPatientResponse{}, err
	}
	return RegisterPatientResponse{
		CustomerID:  customerID,
		MRN:         mrn,
		IsExisting:  isExisting,
		Identifiers: identifiers,
	}, nil
}

func (s *Service) ListPatientIdentifiers(ctx context.Context, tenantID, customerID int64) ([]PatientIdentifier, error) {
	return s.store.ListPatientIdentifiers(ctx, tenantID, customerID)
}

// ---------------------------------------------------------------------------
// 排班与号源

func (s *Service) ListSchedules(ctx context.Context, tenantID int64, q scheduleQuery) ([]Schedule, error) {
	return s.store.ListSchedules(ctx, tenantID, q)
}

func (s *Service) GetSchedule(ctx context.Context, tenantID, id int64) (Schedule, error) {
	sched, err := s.store.GetSchedule(ctx, tenantID, id)
	if err != nil {
		if err == pgx.ErrNoRows {
			return Schedule{}, ErrNotFound
		}
		return Schedule{}, err
	}
	return sched, nil
}

func (s *Service) CreateSchedule(ctx context.Context, tenantID int64, req CreateScheduleRequest) (Schedule, error) {
	if req.DoctorID <= 0 {
		return Schedule{}, ErrInvalidRequest
	}
	workDate, err := time.Parse("2006-01-02", req.WorkDate)
	if err != nil {
		return Schedule{}, ErrInvalidRequest
	}
	start, err := time.Parse("15:04", req.StartTime)
	if err != nil {
		return Schedule{}, ErrInvalidRequest
	}
	end, err := time.Parse("15:04", req.EndTime)
	if err != nil {
		return Schedule{}, ErrInvalidRequest
	}
	if !end.After(start) {
		return Schedule{}, ErrInvalidRequest
	}
	if req.SlotIntervalMinutes < 5 || req.SlotIntervalMinutes > 120 {
		return Schedule{}, ErrInvalidRequest
	}
	if workDate.IsZero() {
		return Schedule{}, ErrInvalidRequest
	}
	switch req.Period {
	case "", "morning", "afternoon", "evening":
	default:
		return Schedule{}, ErrInvalidRequest
	}
	return s.store.CreateSchedule(ctx, tenantID, req)
}

func (s *Service) UpdateSchedule(ctx context.Context, tenantID, id int64, req UpdateScheduleRequest) (Schedule, error) {
	if req.StartTime != nil {
		if _, err := time.Parse("15:04", *req.StartTime); err != nil {
			return Schedule{}, ErrInvalidRequest
		}
	}
	if req.EndTime != nil {
		if _, err := time.Parse("15:04", *req.EndTime); err != nil {
			return Schedule{}, ErrInvalidRequest
		}
	}
	if req.SlotIntervalMinutes != nil && (*req.SlotIntervalMinutes < 5 || *req.SlotIntervalMinutes > 120) {
		return Schedule{}, ErrInvalidRequest
	}
	if req.Status != nil && *req.Status != "open" && *req.Status != "suspended" {
		return Schedule{}, ErrInvalidRequest
	}
	sched, err := s.store.UpdateSchedule(ctx, tenantID, id, req)
	if err != nil {
		if err == pgx.ErrNoRows {
			return Schedule{}, ErrNotFound
		}
		return Schedule{}, err
	}
	return sched, nil
}

func (s *Service) ListSlots(ctx context.Context, tenantID, scheduleID int64) ([]Slot, error) {
	if _, err := s.store.GetSchedule(ctx, tenantID, scheduleID); err != nil {
		return nil, ErrNotFound
	}
	return s.store.ListSlots(ctx, tenantID, scheduleID)
}

// ---------------------------------------------------------------------------
// 挂号与分诊队列

func (s *Service) ListRegistrations(ctx context.Context, tenantID int64, date, status string, doctorID, departmentID int64) ([]Registration, error) {
	return s.store.ListRegistrations(ctx, tenantID, date, status, doctorID, departmentID)
}

func (s *Service) CreateRegistration(ctx context.Context, tenantID, createdBy int64, req CreateRegistrationRequest) (Registration, error) {
	if req.CustomerID <= 0 || req.ScheduleID <= 0 {
		return Registration{}, ErrInvalidRequest
	}
	switch req.RegType {
	case "", "walkin", "appointment":
	default:
		return Registration{}, ErrInvalidRequest
	}
	if req.Source == "" {
		req.Source = "desk"
	}
	if req.Fee < 0 {
		return Registration{}, ErrInvalidRequest
	}
	return s.store.CreateRegistration(ctx, tenantID, createdBy, req)
}

func (s *Service) ConfirmRegistration(ctx context.Context, tenantID, id int64) (Registration, error) {
	return s.store.ConfirmRegistration(ctx, tenantID, id)
}

func (s *Service) RefundRegistration(ctx context.Context, tenantID, id int64) (Registration, error) {
	return s.store.RefundRegistration(ctx, tenantID, id)
}

func (s *Service) NoShowRegistration(ctx context.Context, tenantID, id int64) (Registration, error) {
	return s.store.NoShowRegistration(ctx, tenantID, id)
}

func (s *Service) ListQueue(ctx context.Context, tenantID int64, date string, doctorID int64, status string) ([]QueueEntry, error) {
	return s.store.ListQueue(ctx, tenantID, date, doctorID, status)
}

func (s *Service) StartQueueItem(ctx context.Context, tenantID, registrationID int64) (QueueEntry, error) {
	return s.store.StartQueueItem(ctx, tenantID, registrationID)
}

func (s *Service) CompleteQueueItem(ctx context.Context, tenantID, registrationID int64) (QueueEntry, error) {
	return s.store.CompleteQueueItem(ctx, tenantID, registrationID)
}

// ---------------------------------------------------------------------------
// 医生工作站

func (s *Service) ListTemplates(ctx context.Context, tenantID int64, visitType, specialty string) ([]EMRTemplate, error) {
	return s.store.ListPublishedTemplates(ctx, tenantID, visitType, specialty)
}

func (s *Service) WorkbenchContext(ctx context.Context, tenantID, encounterID int64) (*WorkbenchContext, error) {
	return s.store.WorkbenchContext(ctx, tenantID, encounterID)
}

// EnsureEMRRecord 确保就诊病历存在：已有则返回，否则经 emrrecord 服务创建草稿。
func (s *Service) EnsureEMRRecord(ctx context.Context, tenantID, actorID int64, req EnsureEMRRequest) (*EMRRecordSummary, error) {
	if req.EncounterID <= 0 || strings.TrimSpace(req.TemplateID) == "" {
		return nil, ErrInvalidRequest
	}
	if req.VisitType != "初诊" && req.VisitType != "复诊" {
		return nil, ErrInvalidRequest
	}
	if s.emr == nil {
		return nil, ErrNotFound
	}
	existing, err := s.store.FindEMRRecordByEncounter(ctx, tenantID, req.EncounterID)
	if err != nil {
		return nil, err
	}
	if existing != nil {
		return s.store.GetEMRRecordSummary(ctx, tenantID, *existing)
	}

	// 从 HIS 挂号/就诊解析患者与医生信息，构造病历创建请求。
	var customerID, doctorEmployeeID int64
	var patientName, patientPhone, patientGender, patientAllergy string
	var patientAge int
	var departmentID *int64
	err = s.store.pool.QueryRow(ctx, `
		SELECT r.customer_id, COALESCE(c.name, ''), COALESCE(c.phone, ''), COALESCE(c.gender, ''),
		       COALESCE(c.age, 0), COALESCE(c.extra_data->>'allergy', ''),
		       COALESCE(d.employee_id, 0), r.department_id
		FROM his_registrations r
		JOIN customers c ON c.id = r.customer_id
		LEFT JOIN his_doctors d ON d.id = r.doctor_id
		WHERE r.tenant_id = $1 AND r.id = (
			SELECT e.source_id FROM encounters e
			WHERE e.tenant_id = $1 AND e.source_type = 'his' AND e.id = $2
		)`, tenantID, req.EncounterID).
		Scan(&customerID, &patientName, &patientPhone, &patientGender, &patientAge,
			&patientAllergy, &doctorEmployeeID, &departmentID)
	if err != nil {
		if err == pgx.ErrNoRows {
			return nil, ErrNotFound
		}
		return nil, err
	}
	if patientAllergy == "" {
		patientAllergy = "无"
	}

	patientSnapshot := map[string]any{
		"customer_id": customerID,
		"name":        patientName,
		"phone":       patientPhone,
		"gender":      patientGender,
		"age":         patientAge,
		"allergy":     patientAllergy,
	}
	startedAt := time.Now()
	record, err := s.emr.Create(ctx, tenantID, actorID, emrrecord.CreateRequest{
		EncounterID:       req.EncounterID,
		PatientID:         &customerID,
		PatientSnapshot:   patientSnapshot,
		TemplateVersionID: req.TemplateID,
		DocumentType:      "门诊病历",
		VisitType:         req.VisitType,
		DepartmentID:      departmentID,
		DoctorID:          &doctorEmployeeID,
		StartedAt:         &startedAt,
		EncounterContext:  map[string]any{"source": "his"},
		SourceReferences:  map[string]any{"source_type": "his"},
		Content:           map[string]any{},
	})
	if err != nil {
		return nil, err
	}
	return s.store.GetEMRRecordSummary(ctx, tenantID, record.ID)
}

// SaveEMRRecord 保存病历内容（复用 emrrecord 手动保存）。
func (s *Service) SaveEMRRecord(ctx context.Context, tenantID, actorID int64, recordID string, req SaveEMRRequest) (*EMRRecordSummary, error) {
	if s.emr == nil {
		return nil, ErrNotFound
	}
	outcome, err := s.emr.ManualSave(ctx, tenantID, actorID, recordID, emrrecord.ActionRequest{
		Content:         req.Content,
		PatientSnapshot: req.PatientSnapshot,
		Note:            req.Note,
	})
	if err != nil {
		return nil, err
	}
	return s.store.GetEMRRecordSummary(ctx, tenantID, outcome.Record.ID)
}

// CompleteEMRRecord 完成病历（复用 emrrecord 完成动作，含质检）。
func (s *Service) CompleteEMRRecord(ctx context.Context, tenantID, actorID int64, recordID string, req SaveEMRRequest) (*EMRRecordSummary, error) {
	if s.emr == nil {
		return nil, ErrNotFound
	}
	outcome, err := s.emr.Complete(ctx, tenantID, actorID, recordID, emrrecord.ActionRequest{
		Content:         req.Content,
		PatientSnapshot: req.PatientSnapshot,
		Note:            req.Note,
	})
	if err != nil {
		return nil, err
	}
	return s.store.GetEMRRecordSummary(ctx, tenantID, outcome.Record.ID)
}

// ---------------------------------------------------------------------------
// 处方

func (s *Service) ListPrescriptions(ctx context.Context, tenantID, encounterID int64) ([]Prescription, error) {
	return s.store.ListPrescriptionsByEncounter(ctx, tenantID, encounterID)
}

func (s *Service) CreatePrescription(ctx context.Context, tenantID int64, req CreatePrescriptionRequest) (Prescription, error) {
	if req.EncounterID <= 0 || len(req.Items) == 0 {
		return Prescription{}, ErrInvalidRequest
	}
	for _, item := range req.Items {
		if item.DrugID <= 0 || item.Quantity <= 0 {
			return Prescription{}, ErrInvalidRequest
		}
	}
	return s.store.CreatePrescription(ctx, tenantID, req)
}

func (s *Service) IssuePrescription(ctx context.Context, tenantID, id int64) (Prescription, error) {
	return s.store.IssuePrescription(ctx, tenantID, id)
}

func (s *Service) VoidPrescription(ctx context.Context, tenantID, id int64) (Prescription, error) {
	return s.store.VoidPrescription(ctx, tenantID, id)
}

func (s *Service) UpdatePrescription(ctx context.Context, tenantID, id int64, req UpdatePrescriptionRequest) (Prescription, error) {
	if req.Items != nil {
		for _, item := range req.Items {
			if item.DrugID <= 0 || item.Quantity <= 0 {
				return Prescription{}, ErrInvalidRequest
			}
		}
	}
	return s.store.UpdatePrescription(ctx, tenantID, id, req)
}

// ---------------------------------------------------------------------------
// 检查申请与治疗医嘱

func (s *Service) CreateServiceRequest(ctx context.Context, tenantID int64, req CreateServiceRequestRequest) (ServiceRequest, error) {
	if req.EncounterID <= 0 || strings.TrimSpace(req.ItemName) == "" {
		return ServiceRequest{}, ErrInvalidRequest
	}
	return s.store.CreateServiceRequest(ctx, tenantID, req)
}

func (s *Service) ListServiceRequests(ctx context.Context, tenantID, encounterID int64) ([]ServiceRequest, error) {
	return s.store.ListServiceRequests(ctx, tenantID, encounterID)
}

func (s *Service) CreateTreatmentOrder(ctx context.Context, tenantID int64, req CreateTreatmentOrderRequest) (TreatmentOrder, error) {
	if req.EncounterID <= 0 || req.ChargeItemID <= 0 || req.PlanTotalTimes <= 0 {
		return TreatmentOrder{}, ErrInvalidRequest
	}
	return s.store.CreateTreatmentOrder(ctx, tenantID, req)
}

func (s *Service) ListTreatmentOrders(ctx context.Context, tenantID, encounterID int64) ([]TreatmentOrder, error) {
	return s.store.ListTreatmentOrders(ctx, tenantID, encounterID)
}

func (s *Service) ExecuteTreatment(ctx context.Context, tenantID, orderID, executorID int64, note string) (TreatmentOrder, error) {
	return s.store.AddTreatmentExecution(ctx, tenantID, orderID, executorID, note)
}

// ---------------------------------------------------------------------------
// 打包收费

func (s *Service) ListOrders(ctx context.Context, tenantID int64, status string, encounterID, customerID int64) ([]Order, error) {
	return s.store.ListOrders(ctx, tenantID, status, encounterID, customerID)
}

func (s *Service) GetOrder(ctx context.Context, tenantID, id int64) (Order, error) {
	order, err := s.store.loadOrder(ctx, s.store.pool, tenantID, id)
	if err != nil {
		if err == pgx.ErrNoRows {
			return Order{}, ErrNotFound
		}
		return Order{}, err
	}
	return order, nil
}

func (s *Service) CreateOrder(ctx context.Context, tenantID, createdBy int64, req CreateOrderRequest) (Order, error) {
	if req.EncounterID <= 0 || len(req.Items) == 0 {
		return Order{}, ErrInvalidRequest
	}
	for _, item := range req.Items {
		if item.Quantity <= 0 {
			return Order{}, ErrInvalidRequest
		}
		switch item.ItemType {
		case "charge_item", "bundle", "drug":
		default:
			return Order{}, ErrInvalidRequest
		}
	}
	return s.store.CreateOrder(ctx, tenantID, createdBy, req)
}

func (s *Service) UpdateOrder(ctx context.Context, tenantID, id int64, req UpdateOrderRequest) (Order, error) {
	return s.store.UpdateOrder(ctx, tenantID, id, req)
}

func (s *Service) AddPayment(ctx context.Context, tenantID, operatorID, orderID int64, req CreatePaymentRequest) (Order, error) {
	if req.Amount <= 0 {
		return Order{}, ErrInvalidRequest
	}
	switch req.Method {
	case "", "cash", "wechat", "alipay", "insurance":
	default:
		return Order{}, ErrInvalidRequest
	}
	return s.store.AddPayment(ctx, tenantID, operatorID, orderID, req)
}

func (s *Service) RefundOrder(ctx context.Context, tenantID, operatorID, orderID int64, reason string) (Order, error) {
	return s.store.RefundOrder(ctx, tenantID, operatorID, orderID, reason)
}

func (s *Service) VoidOrder(ctx context.Context, tenantID, orderID int64) (Order, error) {
	return s.store.VoidOrder(ctx, tenantID, orderID)
}

// ---------------------------------------------------------------------------
// 药房

func (s *Service) ListPharmacyPrescriptions(ctx context.Context, tenantID int64, status string) ([]PharmacyPrescription, error) {
	return s.store.ListPharmacyPrescriptions(ctx, tenantID, status)
}

func (s *Service) DispensePrescription(ctx context.Context, tenantID, operatorID, prescriptionID int64) (PharmacyPrescription, error) {
	return s.store.DispensePrescription(ctx, tenantID, operatorID, prescriptionID)
}

func (s *Service) ListStock(ctx context.Context, tenantID int64, keyword string) ([]StockItem, error) {
	return s.store.ListStock(ctx, tenantID, keyword)
}

func (s *Service) InboundStock(ctx context.Context, tenantID, operatorID int64, req InboundStockRequest) (StockItem, error) {
	if req.DrugID <= 0 || req.Quantity <= 0 {
		return StockItem{}, ErrInvalidRequest
	}
	return s.store.InboundStock(ctx, tenantID, operatorID, req)
}

// ---------------------------------------------------------------------------
// 医疗文书

// contentHash 文书内容哈希（Go json.Marshal 对 map 键排序，结果确定）。
func contentHash(raw string) string {
	sum := sha256.Sum256([]byte(raw))
	return hex.EncodeToString(sum[:])
}

func snapshotHash(snapshot map[string]any) (string, error) {
	raw, err := json.Marshal(snapshot)
	if err != nil {
		return "", err
	}
	return contentHash(string(raw)), nil
}

// GenerateDocument 生成文书快照（幂等）。
func (s *Service) GenerateDocument(ctx context.Context, tenantID int64, req GenerateDocumentRequest) (*Document, error) {
	existing, err := s.store.FindDocumentByRef(ctx, tenantID, req.DocType, req.RefID)
	if err != nil {
		return nil, err
	}
	if existing != nil {
		return s.store.LoadDocument(ctx, tenantID, *existing)
	}

	snapshot, refID, encounterID, customerID, err := s.buildDocumentSnapshot(ctx, tenantID, req)
	if err != nil {
		return nil, err
	}
	id, err := s.store.CreateDocument(ctx, tenantID, req.DocType, refID, encounterID, customerID, snapshot)
	if err != nil {
		return nil, err
	}
	return s.store.LoadDocument(ctx, tenantID, id)
}

// buildDocumentSnapshot 按文书类型渲染内容快照。
func (s *Service) buildDocumentSnapshot(ctx context.Context, tenantID int64, req GenerateDocumentRequest) (map[string]any, *int64, *int64, *int64, error) {
	switch req.DocType {
	case "门诊处方":
		p, err := s.store.loadPrescription(ctx, s.store.pool, tenantID, req.RefID)
		if err != nil {
			if err == pgx.ErrNoRows {
				return nil, nil, nil, nil, ErrNotFound
			}
			return nil, nil, nil, nil, err
		}
		customer, err := s.store.CustomerSummary(ctx, tenantID, p.CustomerID)
		if err != nil {
			return nil, nil, nil, nil, err
		}
		items := make([]map[string]any, 0, len(p.Items))
		for _, item := range p.Items {
			items = append(items, map[string]any{
				"name": item.Name, "spec": item.Spec, "quantity": item.Quantity,
				"unit": item.Unit, "usage": item.Dose + " " + item.Frequency + " " + item.UsageText,
			})
		}
		snapshot := map[string]any{
			"prescription_no": p.PrescriptionNo,
			"date":            time.Now().Format("2006-01-02"),
			"name":            customer.Name,
			"gender":          customer.Gender,
			"age":             customer.Age,
			"diagnosis":       p.Diagnosis,
			"items":           items,
			"allergy_notice":  customer.Allergy,
		}
		ref := p.ID
		return snapshot, &ref, &p.EncounterID, &p.CustomerID, nil

	case "检查申请单":
		var request ServiceRequest
		err := s.store.pool.QueryRow(ctx, `
			SELECT id, encounter_id, customer_id, request_type, item_name, specimen_type, purpose, status, created_at
			FROM his_service_requests WHERE tenant_id = $1 AND id = $2`, tenantID, req.RefID).
			Scan(&request.ID, &request.EncounterID, &request.CustomerID, &request.RequestType, &request.ItemName,
				&request.SpecimenType, &request.Purpose, &request.Status, &request.CreatedAt)
		if err != nil {
			if err == pgx.ErrNoRows {
				return nil, nil, nil, nil, ErrNotFound
			}
			return nil, nil, nil, nil, err
		}
		customer, err := s.store.CustomerSummary(ctx, tenantID, request.CustomerID)
		if err != nil {
			return nil, nil, nil, nil, err
		}
		diagnosis := s.latestDiagnosis(ctx, tenantID, request.CustomerID)
		snapshot := map[string]any{
			"request_no":    fmt.Sprintf("SQ%08d", request.ID),
			"request_date":  request.CreatedAt.Format("2006-01-02"),
			"name":          customer.Name,
			"gender":        customer.Gender,
			"age":           customer.Age,
			"diagnosis":     diagnosis,
			"specimen_type": request.SpecimenType,
			"items":         request.ItemName,
			"purpose":       request.Purpose,
		}
		ref := request.ID
		return snapshot, &ref, &request.EncounterID, &request.CustomerID, nil

	case "门诊病历":
		recordID, err := s.store.FindEMRRecordByEncounter(ctx, tenantID, req.RefID)
		if err != nil {
			return nil, nil, nil, nil, err
		}
		if recordID == nil {
			return nil, nil, nil, nil, ErrNotFound
		}
		record, err := s.store.GetEMRRecordSummary(ctx, tenantID, *recordID)
		if err != nil {
			return nil, nil, nil, nil, err
		}
		snapshot := map[string]any{
			"visit_date":    record.StartedAt.Format("2006-01-02"),
			"visit_type":    record.VisitType,
			"patient":       record.PatientSnapshot,
			"content":       record.Content,
			"template_name": record.TemplateName,
		}
		ref := req.RefID
		return snapshot, &ref, &record.EncounterID, &record.CustomerID, nil

	default:
		return nil, nil, nil, nil, ErrInvalidRequest
	}
}

// latestDiagnosis 患者最近一次病历诊断名。
func (s *Service) latestDiagnosis(ctx context.Context, tenantID, customerID int64) string {
	history, err := s.store.ListEMRHistory(ctx, tenantID, customerID)
	if err != nil || len(history) == 0 {
		return ""
	}
	return history[0].Diagnosis
}

// SignDocument 签署文书（绑定当前快照哈希）。
func (s *Service) SignDocument(ctx context.Context, tenantID, employeeID, documentID int64, req CreateSignatureRequest) (*Document, error) {
	switch req.SignerType {
	case "patient", "agent", "doctor", "pharmacist", "nurse":
	default:
		return nil, ErrInvalidRequest
	}
	if strings.TrimSpace(req.SignaturePath) == "" {
		return nil, ErrInvalidRequest
	}
	doc, err := s.store.LoadDocument(ctx, tenantID, documentID)
	if err != nil {
		if err == pgx.ErrNoRows {
			return nil, ErrNotFound
		}
		return nil, err
	}
	if doc.Status == "archived" || doc.Status == "printed" {
		return nil, ErrInvalidState
	}
	hash, err := snapshotHash(doc.ContentSnapshot)
	if err != nil {
		return nil, err
	}
	if _, err := s.store.AddSignature(ctx, tenantID, req, documentID, hash, employeeID); err != nil {
		return nil, err
	}
	return s.store.LoadDocument(ctx, tenantID, documentID)
}

// ArchiveDocument 归档（签名存在时校验内容未被篡改）。
func (s *Service) ArchiveDocument(ctx context.Context, tenantID, documentID int64) (*Document, error) {
	intact, err := s.store.VerifySignaturesIntact(ctx, tenantID, documentID)
	if err != nil {
		return nil, err
	}
	if !intact {
		return nil, ErrSignatureMismatch
	}
	return s.store.ArchiveDocument(ctx, tenantID, documentID)
}

// PrintDocument 标记打印（纸质归档兜底）。
func (s *Service) PrintDocument(ctx context.Context, tenantID, documentID int64) (*Document, error) {
	doc, err := s.store.LoadDocument(ctx, tenantID, documentID)
	if err != nil {
		if err == pgx.ErrNoRows {
			return nil, ErrNotFound
		}
		return nil, err
	}
	if doc.Status == "archived" || doc.Status == "printed" {
		intact, err := s.store.VerifySignaturesIntact(ctx, tenantID, documentID)
		if err != nil {
			return nil, err
		}
		if !intact {
			return nil, ErrSignatureMismatch
		}
	}
	return s.store.MarkDocumentPrinted(ctx, tenantID, documentID)
}

// SubmitFollowupDocument 随访文书补录 + 生成（外接随访产品）。
func (s *Service) SubmitFollowupDocument(ctx context.Context, tenantID int64, req FollowupDocumentRequest) (*Document, error) {
	if req.FollowupID <= 0 || req.CustomerID <= 0 {
		return nil, ErrInvalidRequest
	}
	if err := s.store.UpsertFollowupExtension(ctx, req.FollowupID, req); err != nil {
		return nil, err
	}
	customer, err := s.store.CustomerSummary(ctx, tenantID, req.CustomerID)
	if err != nil {
		return nil, err
	}
	snapshot := map[string]any{
		"followup_id":       req.FollowupID,
		"followup_date":     req.FollowupAt,
		"followup_method":   req.FollowupMethod,
		"name":              customer.Name,
		"gender":            customer.Gender,
		"age":               customer.Age,
		"diagnosis":         s.latestDiagnosis(ctx, tenantID, req.CustomerID),
		"symptoms":          req.Symptoms,
		"adherence":         req.Adherence,
		"adverse_reactions": req.AdverseReactions,
		"assessment":        req.Assessment,
		"education":         req.Education,
		"next_followup_at":  req.NextFollowupAt,
	}
	ref := req.FollowupID
	id, err := s.store.CreateDocument(ctx, tenantID, "随访记录单", &ref, req.EncounterID, &req.CustomerID, snapshot)
	if err != nil {
		return nil, err
	}
	return s.store.LoadDocument(ctx, tenantID, id)
}

// ---------------------------------------------------------------------------
// 基础报表

func (s *Service) ReportSummary(ctx context.Context, tenantID int64, dateFrom, dateTo string) (ReportSummary, error) {
	return s.store.ReportSummary(ctx, tenantID, dateFrom, dateTo)
}

// ---------------------------------------------------------------------------
// 开放接口

// GenerateAPIKey 生成 API Key（明文仅返回一次，库存哈希）。
func GenerateAPIKey() (string, error) {
	buf := make([]byte, 24)
	if _, err := rand.Read(buf); err != nil {
		return "", err
	}
	return "lk_" + hex.EncodeToString(buf), nil
}

// APIKeyHash 计算 API Key 哈希。
func APIKeyHash(key string) string {
	return contentHash(key)
}

func (s *Service) ListOpenClients(ctx context.Context, tenantID int64) ([]OpenClient, error) {
	return s.store.ListOpenClients(ctx, tenantID)
}

func (s *Service) CreateOpenClient(ctx context.Context, tenantID int64, req CreateOpenClientRequest) (CreateOpenClientResponse, error) {
	if strings.TrimSpace(req.Name) == "" {
		return CreateOpenClientResponse{}, ErrInvalidRequest
	}
	apiKey, err := GenerateAPIKey()
	if err != nil {
		return CreateOpenClientResponse{}, err
	}
	client, err := s.store.CreateOpenClient(ctx, tenantID, req, APIKeyHash(apiKey))
	if err != nil {
		return CreateOpenClientResponse{}, err
	}
	return CreateOpenClientResponse{Client: client, APIKey: apiKey}, nil
}

func (s *Service) UpdateOpenClient(ctx context.Context, tenantID, id int64, req UpdateOpenClientRequest) (OpenClient, error) {
	client, err := s.store.UpdateOpenClient(ctx, tenantID, id, req)
	if err != nil {
		if err == pgx.ErrNoRows {
			return OpenClient{}, ErrNotFound
		}
		return OpenClient{}, err
	}
	return client, nil
}

func (s *Service) ListOpenDepartments(ctx context.Context, tenantID int64) ([]OpenDepartment, error) {
	return s.store.ListOpenDepartments(ctx, tenantID)
}

func (s *Service) ListOpenDoctors(ctx context.Context, tenantID int64) ([]OpenDoctor, error) {
	return s.store.ListOpenDoctors(ctx, tenantID)
}

// ListOpenSchedules 排班号源（先机会式释放超时预约）。
func (s *Service) ListOpenSchedules(ctx context.Context, tenantID int64, date string, departmentID, doctorID int64) ([]OpenSchedule, error) {
	if err := s.store.ReleaseExpiredReservations(ctx, tenantID, openReservationTimeout); err != nil {
		return nil, err
	}
	return s.store.ListOpenSchedules(ctx, tenantID, date, departmentID, doctorID)
}

// openReservationTimeout 预约锁号超时（未到院确认自动释放）。
const openReservationTimeout = 5 * time.Minute

// CreateOpenAppointment 患者端预约：建档（不发病案号）→ 锁号 → reserved。
func (s *Service) CreateOpenAppointment(ctx context.Context, tenantID int64, req OpenAppointmentRequest) (OpenAppointment, error) {
	if strings.TrimSpace(req.PatientName) == "" || strings.TrimSpace(req.PatientPhone) == "" {
		return OpenAppointment{}, ErrInvalidRequest
	}
	if err := s.store.ReleaseExpiredReservations(ctx, tenantID, openReservationTimeout); err != nil {
		return OpenAppointment{}, err
	}
	customerID, err := s.store.FindOrCreateOpenCustomer(ctx, tenantID, strings.TrimSpace(req.PatientName), strings.TrimSpace(req.PatientPhone))
	if err != nil {
		return OpenAppointment{}, err
	}
	reg, err := s.store.CreateRegistration(ctx, tenantID, 0, CreateRegistrationRequest{
		CustomerID: customerID,
		ScheduleID: req.ScheduleID,
		SlotID:     req.SlotID,
		RegType:    "appointment",
		Source:     "miniprogram",
		Fee:        0,
	})
	if err != nil {
		return OpenAppointment{}, err
	}
	return s.store.LoadOpenAppointment(ctx, tenantID, reg.ID)
}

// CancelOpenAppointment 患者端取消预约。
func (s *Service) CancelOpenAppointment(ctx context.Context, tenantID, registrationID int64) (OpenAppointment, error) {
	if _, err := s.store.CancelAppointment(ctx, tenantID, registrationID); err != nil {
		return OpenAppointment{}, err
	}
	return s.store.LoadOpenAppointment(ctx, tenantID, registrationID)
}

// GetOpenAppointment 挂号单状态。
func (s *Service) GetOpenAppointment(ctx context.Context, tenantID, registrationID int64) (OpenAppointment, error) {
	return s.store.LoadOpenAppointment(ctx, tenantID, registrationID)
}

// ---------------------------------------------------------------------------
// 员工角色

func (s *Service) ListStaffRoles(ctx context.Context, tenantID int64) ([]StaffRole, error) {
	return s.store.ListStaffRoles(ctx, tenantID)
}

func (s *Service) UpdateStaffRoles(ctx context.Context, tenantID, employeeID int64, roles []string) error {
	return s.store.ReplaceStaffRoles(ctx, tenantID, employeeID, roles)
}

// MyRoles 当前员工角色；租户未配置任何角色时返回 nil（前端视为全开放，便于首用）。
func (s *Service) MyRoles(ctx context.Context, tenantID, employeeID int64) ([]string, error) {
	configured, err := s.store.TenantHasRoleAssignments(ctx, tenantID)
	if err != nil {
		return nil, err
	}
	if !configured {
		return nil, nil
	}
	return s.store.MyRoles(ctx, tenantID, employeeID)
}

// RequireRole 关键操作权限校验（未配置角色时放行，首用兼容）。
func (s *Service) RequireRole(ctx context.Context, tenantID, employeeID int64, roles ...string) error {
	configured, err := s.store.TenantHasRoleAssignments(ctx, tenantID)
	if err != nil {
		return err
	}
	if !configured {
		return nil
	}
	has, err := s.store.HasAnyRole(ctx, tenantID, employeeID, roles...)
	if err != nil {
		return err
	}
	if !has {
		return ErrForbiddenRole
	}
	return nil
}

// SearchDiagnosis 诊断检索（工作站诊断录入）。
func (s *Service) SearchDiagnosis(ctx context.Context, tenantID int64, keyword string) ([]ICDDiagnosis, error) {
	return s.store.SearchDiagnosisCatalog(ctx, keyword, 20)
}
