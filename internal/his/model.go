package his

import "time"

// DashboardMetrics 门户卡片指标（GET /api/v1/his/dashboard/metrics）。
type DashboardMetrics struct {
	TodayVisits          int64 `json:"today_visits"`          // 今日门诊量
	PendingVisits        int64 `json:"pending_visits"`        // 待就诊（已挂号未接诊）
	PendingPrescriptions int64 `json:"pending_prescriptions"` // 待审核处方（药房队列）
}

// Doctor 医生执业信息。
type Doctor struct {
	ID            int64  `json:"id"`
	EmployeeID    int64  `json:"employee_id"`
	DepartmentID  *int64 `json:"department_id"`
	Title         string `json:"title"`
	LicenseNo     string `json:"license_no"`
	IsPrescribing bool   `json:"is_prescribing"`
	Status        string `json:"status"`
}

// CreateDoctorRequest 新增医生。
type CreateDoctorRequest struct {
	EmployeeID    int64  `json:"employee_id"`
	DepartmentID  *int64 `json:"department_id"`
	Title         string `json:"title"`
	LicenseNo     string `json:"license_no"`
	IsPrescribing *bool  `json:"is_prescribing"`
}

// UpdateDoctorRequest 更新医生（部分更新，nil 字段不覆盖）。
type UpdateDoctorRequest struct {
	DepartmentID  *int64  `json:"department_id"`
	Title         *string `json:"title"`
	LicenseNo     *string `json:"license_no"`
	IsPrescribing *bool   `json:"is_prescribing"`
	Status        *string `json:"status"`
}

// Room 诊室/治疗室/药房窗口。
type Room struct {
	ID     int64  `json:"id"`
	Name   string `json:"name"`
	Type   string `json:"type"`
	Status string `json:"status"`
}

// CreateRoomRequest 新增房间。
type CreateRoomRequest struct {
	Name string `json:"name"`
	Type string `json:"type"`
}

// UpdateRoomRequest 更新房间。
type UpdateRoomRequest struct {
	Name   *string `json:"name"`
	Type   *string `json:"type"`
	Status *string `json:"status"`
}

// Drug 药品字典条目。
type Drug struct {
	ID            int64   `json:"id"`
	Code          string  `json:"code"`
	Name          string  `json:"name"`
	Spec          string  `json:"spec"`
	Unit          string  `json:"unit"`
	Manufacturer  string  `json:"manufacturer"`
	DrugType      string  `json:"drug_type"`
	RetailPrice   float64 `json:"retail_price"`
	PurchasePrice float64 `json:"purchase_price"`
	IsRx          bool    `json:"is_rx"`
	Status        string  `json:"status"`
}

// CreateDrugRequest 新增药品。
type CreateDrugRequest struct {
	Code          string  `json:"code"`
	Name          string  `json:"name"`
	Spec          string  `json:"spec"`
	Unit          string  `json:"unit"`
	Manufacturer  string  `json:"manufacturer"`
	DrugType      string  `json:"drug_type"`
	RetailPrice   float64 `json:"retail_price"`
	PurchasePrice float64 `json:"purchase_price"`
	IsRx          *bool   `json:"is_rx"`
}

// UpdateDrugRequest 更新药品。
type UpdateDrugRequest struct {
	Spec          *string  `json:"spec"`
	Unit          *string  `json:"unit"`
	Manufacturer  *string  `json:"manufacturer"`
	DrugType      *string  `json:"drug_type"`
	RetailPrice   *float64 `json:"retail_price"`
	PurchasePrice *float64 `json:"purchase_price"`
	IsRx          *bool    `json:"is_rx"`
	Status        *string  `json:"status"`
}

// ChargeItem 原子收费项目。
type ChargeItem struct {
	ID       int64   `json:"id"`
	Code     string  `json:"code"`
	Name     string  `json:"name"`
	Category string  `json:"category"`
	Unit     string  `json:"unit"`
	Price    float64 `json:"price"`
	Status   string  `json:"status"`
}

// CreateChargeItemRequest 新增原子收费项目。
type CreateChargeItemRequest struct {
	Code     string  `json:"code"`
	Name     string  `json:"name"`
	Category string  `json:"category"`
	Unit     string  `json:"unit"`
	Price    float64 `json:"price"`
}

// UpdateChargeItemRequest 更新原子收费项目。
type UpdateChargeItemRequest struct {
	Name     *string  `json:"name"`
	Category *string  `json:"category"`
	Unit     *string  `json:"unit"`
	Price    *float64 `json:"price"`
	Status   *string  `json:"status"`
}

// BundleItem 打包产品组成（仅引用原子收费项目）。
type BundleItem struct {
	ChargeItemID   int64   `json:"charge_item_id"`
	ChargeItemName string  `json:"charge_item_name,omitempty"`
	Quantity       float64 `json:"quantity"`
}

// Bundle 打包产品：独立定价，打包价与原子价之和解耦。
type Bundle struct {
	ID          int64        `json:"id"`
	Code        string       `json:"code"`
	Name        string       `json:"name"`
	Price       float64      `json:"price"`
	Description string       `json:"description"`
	ValidFrom   *string      `json:"valid_from"`
	ValidTo     *string      `json:"valid_to"`
	Status      string       `json:"status"`
	Items       []BundleItem `json:"items"`
}

// CreateBundleRequest 新增打包产品（items 全量写入）。
type CreateBundleRequest struct {
	Code        string       `json:"code"`
	Name        string       `json:"name"`
	Price       float64      `json:"price"`
	Description string       `json:"description"`
	ValidFrom   *string      `json:"valid_from"`
	ValidTo     *string      `json:"valid_to"`
	Items       []BundleItem `json:"items"`
}

// UpdateBundleRequest 更新打包产品。
type UpdateBundleRequest struct {
	Name        *string      `json:"name"`
	Price       *float64     `json:"price"`
	Description *string      `json:"description"`
	ValidFrom   *string      `json:"valid_from"`
	ValidTo     *string      `json:"valid_to"`
	Status      *string      `json:"status"`
	Items       []BundleItem `json:"items"` // 非 nil 时全量替换组成
}

// PatientIdentifier 病案号/外部标识。
type PatientIdentifier struct {
	ID             int64     `json:"id"`
	CustomerID     int64     `json:"customer_id"`
	IdentifierType string    `json:"identifier_type"`
	Value          string    `json:"value"`
	IssuedBy       string    `json:"issued_by"`
	IssuedAt       time.Time `json:"issued_at"`
	Status         string    `json:"status"`
}

// RegisterPatientRequest 建档挂号患者（P1：手机号必填，用于查重）。
type RegisterPatientRequest struct {
	Name    string `json:"name"`
	Phone   string `json:"phone"`
	Gender  string `json:"gender"` // male/female/other
	Age     *int   `json:"age"`
	Address string `json:"address"`
	Allergy string `json:"allergy"` // 过敏史，无则填"无"（文书体系★必填）
}

// RegisterPatientResponse 建档结果。
type RegisterPatientResponse struct {
	CustomerID  int64               `json:"customer_id"`
	MRN         string              `json:"mrn"`
	IsExisting  bool                `json:"is_existing"`
	Identifiers []PatientIdentifier `json:"identifiers"`
}

// Schedule 医生排班（P1 逐日排班，不做周期模板）。
type Schedule struct {
	ID                  int64  `json:"id"`
	DoctorID            int64  `json:"doctor_id"`
	DoctorName          string `json:"doctor_name"`
	DepartmentID        *int64 `json:"department_id"`
	RoomID              *int64 `json:"room_id"`
	WorkDate            string `json:"work_date"`  // YYYY-MM-DD
	Period              string `json:"period"`     // morning/afternoon/evening/early_night/late_night/full_day
	StartTime           string `json:"start_time"` // HH:MM
	EndTime             string `json:"end_time"`   // HH:MM
	SlotIntervalMinutes int    `json:"slot_interval_minutes"`
	Status              string `json:"status"` // open/suspended
	Remark              string `json:"remark"`
	TotalSlots          int64  `json:"total_slots"`
	FreeSlots           int64  `json:"free_slots"`
	ReservedSlots       int64  `json:"reserved_slots"`
	RegisteredSlots     int64  `json:"registered_slots"`
}

// CreateScheduleRequest 新增排班（保存时按间隔生成号源）。
type CreateScheduleRequest struct {
	DoctorID            int64  `json:"doctor_id"`
	DepartmentID        *int64 `json:"department_id"`
	RoomID              *int64 `json:"room_id"`
	WorkDate            string `json:"work_date"`
	Period              string `json:"period"`
	StartTime           string `json:"start_time"`
	EndTime             string `json:"end_time"`
	SlotIntervalMinutes int    `json:"slot_interval_minutes"`
	Remark              string `json:"remark"`
}

// UpdateScheduleRequest 更新排班（部分更新）。改时间/间隔时若已存在已约/已挂号号源则拒绝。
type UpdateScheduleRequest struct {
	StartTime           *string `json:"start_time"`
	EndTime             *string `json:"end_time"`
	SlotIntervalMinutes *int    `json:"slot_interval_minutes"`
	Status              *string `json:"status"`
	Remark              *string `json:"remark"`
}

// Slot 号源。
type Slot struct {
	ID             int64  `json:"id"`
	ScheduleID     int64  `json:"schedule_id"`
	SeqNo          int    `json:"seq_no"`
	TimeStart      string `json:"time_start"`
	TimeEnd        string `json:"time_end"`
	Status         string `json:"status"` // free/reserved/registered/disabled
	RegistrationID *int64 `json:"registration_id"`
}

// Registration 挂号单。
type Registration struct {
	ID               int64     `json:"id"`
	CustomerID       int64     `json:"customer_id"`
	CustomerName     string    `json:"customer_name"`
	Phone            string    `json:"phone"`
	MRN              string    `json:"mrn"`
	DepartmentID     *int64    `json:"department_id"`
	DoctorID         *int64    `json:"doctor_id"`
	DoctorName       string    `json:"doctor_name"`
	DoctorEmployeeID *int64    `json:"doctor_employee_id"`
	ScheduleID       *int64    `json:"schedule_id"`
	SlotID           *int64    `json:"slot_id"`
	SlotTimeStart    string    `json:"slot_time_start"`
	SlotTimeEnd      string    `json:"slot_time_end"`
	RegType          string    `json:"reg_type"` // walkin/appointment
	Source           string    `json:"source"`   // desk/miniprogram/h5
	VisitDate        string    `json:"visit_date"`
	Fee              float64   `json:"fee"`
	Status           string    `json:"status"` // reserved/registered/visited/refunded/no_show
	EncounterID      *int64    `json:"encounter_id"`
	CreatedBy        *int64    `json:"created_by"`
	CreatedAt        time.Time `json:"created_at"`
}

// CreateRegistrationRequest 创建挂号/预约。
type CreateRegistrationRequest struct {
	CustomerID int64   `json:"customer_id"` // 必填（先建档取得）
	ScheduleID int64   `json:"schedule_id"` // 必填
	SlotID     *int64  `json:"slot_id"`     // 为空则自动取第一个空闲号源
	RegType    string  `json:"reg_type"`    // walkin/appointment
	Source     string  `json:"source"`      // desk/miniprogram/h5
	Fee        float64 `json:"fee"`         // 挂号费
}

// QueueEntry 分诊队列条目。
type QueueEntry struct {
	ID             int64      `json:"id"`
	EncounterID    int64      `json:"encounter_id"`
	RegistrationID int64      `json:"registration_id"`
	CustomerID     int64      `json:"customer_id"`
	CustomerName   string     `json:"customer_name"`
	Phone          string     `json:"phone"`
	Gender         string     `json:"gender"`
	Age            int        `json:"age"`
	SeqNo          int        `json:"seq_no"`
	Status         string     `json:"status"`
	CalledAt       *time.Time `json:"called_at"`
	DoctorID       *int64     `json:"doctor_id"`
	DoctorName     string     `json:"doctor_name"`
}

// EMRTemplate 已发布病历模板摘要（复用 emr_templates）。
type EMRTemplate struct {
	ID           string `json:"id"`
	Code         string `json:"code"`
	Name         string `json:"name"`
	VersionNo    string `json:"version_no"`
	DocumentType string `json:"document_type"`
	VisitType    string `json:"visit_type"`
	Specialty    string `json:"specialty"`
	DepartmentID *int64 `json:"department_id"`
}

// EMRRecordSummary 病历摘要（复用 emr_records）。
type EMRRecordSummary struct {
	ID              string         `json:"id"`
	EncounterID     int64          `json:"encounter_id"`
	CustomerID      int64          `json:"customer_id"`
	TemplateID      string         `json:"template_id,omitempty"`
	DocumentType    string         `json:"document_type"`
	VisitType       string         `json:"visit_type"`
	TemplateName    string         `json:"template_name"`
	Status          string         `json:"status"`
	StartedAt       time.Time      `json:"started_at"`
	UpdatedAt       time.Time      `json:"updated_at"`
	Content         map[string]any `json:"content"`
	PatientSnapshot map[string]any `json:"patient_snapshot"`
}

// EnsureEMRRequest 确保就诊病历存在。
type EnsureEMRRequest struct {
	EncounterID int64  `json:"encounter_id"`
	TemplateID  string `json:"template_id"`
	VisitType   string `json:"visit_type"` // 初诊/复诊
}

// SaveEMRRequest 保存病历内容（走 emrrecord 手动保存）。
type SaveEMRRequest struct {
	Content          map[string]any `json:"content"`
	PatientSnapshot  map[string]any `json:"patient_snapshot,omitempty"`
	Note             string         `json:"note,omitempty"`
	AllowNonBlocking bool           `json:"allow_non_blocking,omitempty"`
	QualityRunID     string         `json:"quality_run_id,omitempty"`
}

// PrescriptionItemInput 处方明细输入（药品必须来自药品字典）。
type PrescriptionItemInput struct {
	DrugID    int64   `json:"drug_id"`
	Dose      string  `json:"dose"`
	DoseUnit  string  `json:"dose_unit"`
	Frequency string  `json:"frequency"`
	Route     string  `json:"route"`
	Days      *int    `json:"days"`
	Quantity  float64 `json:"quantity"`
	UsageText string  `json:"usage_text"` // 中药饮片煎服法/先煎后下
}

// PrescriptionItem 处方明细。
type PrescriptionItem struct {
	ID        int64   `json:"id"`
	DrugID    *int64  `json:"drug_id"`
	ItemType  string  `json:"item_type"` // drug/herb
	Name      string  `json:"name"`
	Spec      string  `json:"spec"`
	Dose      string  `json:"dose"`
	DoseUnit  string  `json:"dose_unit"`
	Frequency string  `json:"frequency"`
	Route     string  `json:"route"`
	Days      *int    `json:"days"`
	Quantity  float64 `json:"quantity"`
	Unit      string  `json:"unit"`
	Price     float64 `json:"price"`
	Amount    float64 `json:"amount"`
	UsageText string  `json:"usage_text"`
}

// Prescription 处方。
type Prescription struct {
	ID             int64              `json:"id"`
	EncounterID    int64              `json:"encounter_id"`
	CustomerID     int64              `json:"customer_id"`
	DoctorID       *int64             `json:"doctor_id"`
	PrescriptionNo string             `json:"prescription_no"`
	Type           string             `json:"type"` // western/herbal/mixed
	Diagnosis      string             `json:"diagnosis"`
	Status         string             `json:"status"`
	Items          []PrescriptionItem `json:"items"`
	CreatedAt      time.Time          `json:"created_at"`
}

// CreatePrescriptionRequest 开具处方。
type CreatePrescriptionRequest struct {
	EncounterID int64                   `json:"encounter_id"`
	Diagnosis   string                  `json:"diagnosis"`
	Items       []PrescriptionItemInput `json:"items"`
}

// UpdatePrescriptionRequest 修改处方明细（未收费前可改）。
type UpdatePrescriptionRequest struct {
	Diagnosis *string                 `json:"diagnosis"`
	Items     []PrescriptionItemInput `json:"items"`
}

// ServiceRequest 检查/检验申请。
type ServiceRequest struct {
	ID           int64     `json:"id"`
	EncounterID  int64     `json:"encounter_id"`
	CustomerID   int64     `json:"customer_id"`
	RequestType  string    `json:"request_type"` // exam/lab
	ItemName     string    `json:"item_name"`
	SpecimenType string    `json:"specimen_type"`
	Purpose      string    `json:"purpose"`
	Status       string    `json:"status"`
	CreatedAt    time.Time `json:"created_at"`
}

// CreateServiceRequestRequest 开检查/检验申请。
type CreateServiceRequestRequest struct {
	EncounterID  int64  `json:"encounter_id"`
	RequestType  string `json:"request_type"`
	ItemName     string `json:"item_name"`
	SpecimenType string `json:"specimen_type"`
	Purpose      string `json:"purpose"`
}

// TreatmentOrder 治疗医嘱（简化版）。
// 执行站列表会额外填充患者姓名/病案号/最近执行信息；工作站按 encounter 查询时这些字段为空。
type TreatmentOrder struct {
	ID               int64      `json:"id"`
	EncounterID      int64      `json:"encounter_id"`
	CustomerID       int64      `json:"customer_id"`
	ChargeItemID     *int64     `json:"charge_item_id"`
	ChargeItemName   string     `json:"charge_item_name"`
	PlanTotalTimes   int        `json:"plan_total_times"`
	CompletedTimes   int        `json:"completed_times"`
	Status           string     `json:"status"`
	CreatedAt        time.Time  `json:"created_at"`
	CustomerName     string     `json:"customer_name,omitempty"`
	MRN              string     `json:"mrn,omitempty"`
	LastExecutedAt   *time.Time `json:"last_executed_at,omitempty"`
	LastExecutorName string     `json:"last_executor_name,omitempty"`
}

// CreateTreatmentOrderRequest 开治疗医嘱。
// charge_item_id 与 charge_item_name 二选一；只传名称时按租户内启用的收费项目精确匹配。
type CreateTreatmentOrderRequest struct {
	EncounterID    int64  `json:"encounter_id"`
	ChargeItemID   int64  `json:"charge_item_id"`
	ChargeItemName string `json:"charge_item_name"`
	PlanTotalTimes int    `json:"plan_total_times"`
}

// ExecuteTreatmentRequest 治疗执行登记（每次治疗一条）。
type ExecuteTreatmentRequest struct {
	Note string `json:"note"`
}

// TreatmentExecution 治疗执行记录（seq 为医嘱内第几次执行）。
type TreatmentExecution struct {
	ID               int64     `json:"id"`
	TreatmentOrderID int64     `json:"treatment_order_id"`
	Seq              int       `json:"seq"`
	ExecutedAt       time.Time `json:"executed_at"`
	ExecutorID       *int64    `json:"executor_id"`
	ExecutorName     string    `json:"executor_name"`
	Note             string    `json:"note"`
	CreatedAt        time.Time `json:"created_at"`
}

// LabRequest 检验申请单（LIS）。
// 列表额外填充患者姓名/病案号/结果项数；详情额外填充 result_items。
type LabRequest struct {
	ID              int64           `json:"id"`
	EncounterID     int64           `json:"encounter_id"`
	CustomerID      int64           `json:"customer_id"`
	RequestNo       string          `json:"request_no"`
	ItemName        string          `json:"item_name"`
	SpecimenType    string          `json:"specimen_type"`
	Purpose         string          `json:"purpose"`
	Status          string          `json:"status"`
	SampledAt       *time.Time      `json:"sampled_at"`
	ReportedAt      *time.Time      `json:"reported_at"`
	CreatedBy       *int64          `json:"created_by"`
	CreatedAt       time.Time       `json:"created_at"`
	UpdatedAt       time.Time       `json:"updated_at"`
	CustomerName    string          `json:"customer_name,omitempty"`
	MRN             string          `json:"mrn,omitempty"`
	ResultItemCount int             `json:"result_item_count"`
	ResultItems     []LabResultItem `json:"result_items,omitempty"`
	// 收费联动：开单时选择/自动匹配的收费项目，以及生成的 pending 收费单（未匹配时为空、金额 0）。
	ChargeItemID *int64  `json:"charge_item_id"`
	ChargeAmount float64 `json:"charge_amount"`
	OrderID      *int64  `json:"order_id"`
}

// CreateLabRequestRequest 开检验申请。
// charge_item_id 可选：传了则按 id 校验（本租户 + active）；不传则按 item_name 精确匹配「检验」类收费项目。
type CreateLabRequestRequest struct {
	EncounterID  int64  `json:"encounter_id"`
	ItemName     string `json:"item_name"`
	SpecimenType string `json:"specimen_type"`
	Purpose      string `json:"purpose"`
	ChargeItemID int64  `json:"charge_item_id,omitempty"`
}

// LabResultItemInput 录入结果项（report 入参，替换式写入）。
type LabResultItemInput struct {
	ItemCode       string `json:"item_code"`
	ItemName       string `json:"item_name"`
	Value          string `json:"value"`
	Unit           string `json:"unit"`
	ReferenceRange string `json:"reference_range"`
	Flag           string `json:"flag"`
}

// ReportLabRequestRequest 录入检验结果。
type ReportLabRequestRequest struct {
	Items []LabResultItemInput `json:"items"`
}

// LabResultItem 检验结果项。
type LabResultItem struct {
	ID             int64  `json:"id"`
	RequestID      int64  `json:"request_id"`
	ItemCode       string `json:"item_code"`
	ItemName       string `json:"item_name"`
	Value          string `json:"value"`
	Unit           string `json:"unit"`
	ReferenceRange string `json:"reference_range"`
	Flag           string `json:"flag"`
}

// ExamRequest 检查申请单（PACS）。
// 列表额外填充患者姓名/病案号/报告数；详情额外填充 report。
type ExamRequest struct {
	ID           int64       `json:"id"`
	EncounterID  int64       `json:"encounter_id"`
	CustomerID   int64       `json:"customer_id"`
	RequestNo    string      `json:"request_no"`
	ItemName     string      `json:"item_name"`
	BodyPart     string      `json:"body_part"`
	Purpose      string      `json:"purpose"`
	Status       string      `json:"status"`
	StartedAt    *time.Time  `json:"started_at"`
	ReportedAt   *time.Time  `json:"reported_at"`
	CreatedBy    *int64      `json:"created_by"`
	CreatedAt    time.Time   `json:"created_at"`
	UpdatedAt    time.Time   `json:"updated_at"`
	CustomerName string      `json:"customer_name,omitempty"`
	MRN          string      `json:"mrn,omitempty"`
	ReportCount  int         `json:"report_count"`
	Report       *ExamReport `json:"report,omitempty"`
	// 收费联动：开单时选择/自动匹配的收费项目，以及生成的 pending 收费单（未匹配时为空、金额 0）。
	ChargeItemID *int64  `json:"charge_item_id"`
	ChargeAmount float64 `json:"charge_amount"`
	OrderID      *int64  `json:"order_id"`
}

// CreateExamRequestRequest 开检查申请。
// charge_item_id 可选：传了则按 id 校验（本租户 + active）；不传则按 item_name 精确匹配「检查」类收费项目。
type CreateExamRequestRequest struct {
	EncounterID  int64  `json:"encounter_id"`
	ItemName     string `json:"item_name"`
	BodyPart     string `json:"body_part"`
	Purpose      string `json:"purpose"`
	ChargeItemID int64  `json:"charge_item_id,omitempty"`
}

// ReportExamRequestRequest 录入检查报告（report 入参）。
// findings 所见 / impression 印象（必填）/ suggestion 建议；image_refs 为影像地址列表。
type ReportExamRequestRequest struct {
	Findings   string   `json:"findings"`
	Impression string   `json:"impression"`
	Suggestion string   `json:"suggestion"`
	ImageRefs  []string `json:"image_refs"`
}

// ExamReport 检查报告（一份申请单对应一份报告）。
type ExamReport struct {
	ID         int64      `json:"id"`
	RequestID  int64      `json:"request_id"`
	Findings   string     `json:"findings"`
	Impression string     `json:"impression"`
	Suggestion string     `json:"suggestion"`
	ImageRefs  []string   `json:"image_refs"`
	ReportedBy *int64     `json:"reported_by"`
	ReportedAt *time.Time `json:"reported_at"`
}

// CustomerSummary 接诊患者摘要。
type CustomerSummary struct {
	CustomerID int64  `json:"customer_id"`
	Name       string `json:"name"`
	Phone      string `json:"phone"`
	Gender     string `json:"gender"`
	Age        int    `json:"age"`
	MRN        string `json:"mrn"`
	Allergy    string `json:"allergy"` // 过敏史，未填写时为"无"
}

// EMRHistoryItem 既往就诊摘要。
type EMRHistoryItem struct {
	RecordID  string    `json:"record_id"`
	StartedAt time.Time `json:"started_at"`
	VisitType string    `json:"visit_type"`
	Status    string    `json:"status"`
	Diagnosis string    `json:"diagnosis"`
}

// WorkbenchContext 接诊上下文。
type WorkbenchContext struct {
	Registration    *Registration     `json:"registration"`
	Customer        CustomerSummary   `json:"customer"`
	Record          *EMRRecordSummary `json:"record"`
	History         []EMRHistoryItem  `json:"history"`
	Prescriptions   []Prescription    `json:"prescriptions"`
	ServiceRequests []ServiceRequest  `json:"service_requests"`
	TreatmentOrders []TreatmentOrder  `json:"treatment_orders"`
}

// OrderLineInput 收费行输入（前端表单逐行提交，价格由服务端解析）：
// charge_item/bundle/drug 按 ref_id 定价；drug 可带 prescription_id；registration_fee 按就诊挂号记录定价。
type OrderLineInput struct {
	ItemType       string  `json:"item_type"` // charge_item/bundle/drug/registration_fee
	RefID          int64   `json:"ref_id"`
	Quantity       float64 `json:"quantity"`
	DiscountAmount float64 `json:"discount_amount,omitempty"`
	PrescriptionID *int64  `json:"prescription_id,omitempty"` // drug 行：关联处方
}

// OrderItem 收费行。
type OrderItem struct {
	ID             int64   `json:"id"`
	OrderID        int64   `json:"order_id"`
	ItemType       string  `json:"item_type"`
	RefID          *int64  `json:"ref_id"`
	PrescriptionID *int64  `json:"prescription_id"`
	Name           string  `json:"name"`
	UnitPrice      float64 `json:"unit_price"`
	Quantity       float64 `json:"quantity"`
	LineTotal      float64 `json:"line_total"`
	DiscountAmount float64 `json:"discount_amount"`
	Payable        float64 `json:"payable"`
}

// Order 收费单。
type Order struct {
	ID            int64       `json:"id"`
	OrderNo       string      `json:"order_no"`
	CustomerID    int64       `json:"customer_id"`
	CustomerName  string      `json:"customer_name"`
	EncounterID   *int64      `json:"encounter_id"`
	ItemsTotal    float64     `json:"items_total"`
	DiscountTotal float64     `json:"discount_total"`
	PayableTotal  float64     `json:"payable_total"`
	PaidTotal     float64     `json:"paid_total"`
	Status        string      `json:"status"`
	Items         []OrderItem `json:"items"`
	CreatedAt     time.Time   `json:"created_at"`
}

// CreateOrderRequest 划价。
type CreateOrderRequest struct {
	EncounterID   int64            `json:"encounter_id"`
	DiscountTotal float64          `json:"discount_total,omitempty"`
	Items         []OrderLineInput `json:"items"`
}

// UpdateOrderRequest 调整收费单（未收费前可改）。
type UpdateOrderRequest struct {
	DiscountTotal *float64         `json:"discount_total,omitempty"`
	Items         []OrderLineInput `json:"items,omitempty"`
}

// Payment 收款记录。
type Payment struct {
	ID         int64     `json:"id"`
	OrderID    int64     `json:"order_id"`
	Method     string    `json:"method"`
	Amount     float64   `json:"amount"`
	Status     string    `json:"status"`
	PaidAt     time.Time `json:"paid_at"`
	OperatorID *int64    `json:"operator_id"`
}

// CreatePaymentRequest 收款。
type CreatePaymentRequest struct {
	Method string  `json:"method"` // cash/wechat/alipay/insurance(预留)
	Amount float64 `json:"amount"`
}

// RefundOrderRequest 退费。
type RefundOrderRequest struct {
	Reason string `json:"reason"`
}

// StockItem 库存条目。
type StockItem struct {
	DrugID       int64   `json:"drug_id"`
	DrugName     string  `json:"drug_name"`
	Spec         string  `json:"spec"`
	Unit         string  `json:"unit"`
	BatchNo      string  `json:"batch_no"`
	Quantity     float64 `json:"quantity"`
	ExpiryDate   *string `json:"expiry_date"`
	Location     string  `json:"location"`
	ExpiringSoon bool    `json:"expiring_soon"`
}

// InboundStockRequest 入库。
type InboundStockRequest struct {
	DrugID     int64   `json:"drug_id"`
	BatchNo    string  `json:"batch_no"`
	Quantity   float64 `json:"quantity"`
	ExpiryDate *string `json:"expiry_date"`
	Location   string  `json:"location"`
}

// PurchaseOrderItem 采购单明细。
type PurchaseOrderItem struct {
	ID            int64   `json:"id"`
	OrderID       int64   `json:"order_id"`
	DrugID        int64   `json:"drug_id"`
	DrugName      string  `json:"drug_name"`
	Quantity      float64 `json:"quantity"`
	PurchasePrice float64 `json:"purchase_price"`
	BatchNo       string  `json:"batch_no"`
	ExpiryDate    *string `json:"expiry_date"`
	Amount        float64 `json:"amount"`
}

// PurchaseOrder 采购单（列表直接带明细，便于前端“查看明细”）。
type PurchaseOrder struct {
	ID          int64               `json:"id"`
	OrderNo     string              `json:"order_no"`
	Supplier    string              `json:"supplier"`
	Status      string              `json:"status"` // draft/ordered/received/cancelled
	TotalAmount float64             `json:"total_amount"`
	CreatedBy   *int64              `json:"created_by"`
	CreatedAt   time.Time           `json:"created_at"`
	UpdatedAt   time.Time           `json:"updated_at"`
	ItemCount   int                 `json:"item_count"`
	Items       []PurchaseOrderItem `json:"items"`
}

// CreatePurchaseOrderItem 新建采购单的明细行。
type CreatePurchaseOrderItem struct {
	DrugID        int64   `json:"drug_id"`
	Quantity      float64 `json:"quantity"`
	PurchasePrice float64 `json:"purchase_price"`
	BatchNo       string  `json:"batch_no"`
	ExpiryDate    *string `json:"expiry_date"`
}

// CreatePurchaseOrderRequest 新建采购单（草稿）。
type CreatePurchaseOrderRequest struct {
	Supplier string                    `json:"supplier"`
	Items    []CreatePurchaseOrderItem `json:"items"`
}

// PharmacyPrescription 药房处方（含患者信息）。
type PharmacyPrescription struct {
	Prescription
	CustomerName string `json:"customer_name"`
	MRN          string `json:"mrn"`
}

// Document 医疗文书（快照式，签名后内容不可再改）。
type Document struct {
	ID              int64          `json:"id"`
	DocType         string         `json:"doc_type"`
	RefID           *int64         `json:"ref_id"`
	EncounterID     *int64         `json:"encounter_id"`
	CustomerID      *int64         `json:"customer_id"`
	ContentSnapshot map[string]any `json:"content_snapshot"`
	Status          string         `json:"status"` // draft/signed/archived/printed
	PrintedAt       *time.Time     `json:"printed_at"`
	CreatedAt       time.Time      `json:"created_at"`
	Signatures      []Signature    `json:"signatures"`
}

// GenerateDocumentRequest 生成文书（幂等：同 doc_type+ref_id 已存在则返回现有）。
type GenerateDocumentRequest struct {
	DocType     string `json:"doc_type"` // 门诊处方/检查申请单/门诊病历/随访记录单
	RefID       int64  `json:"ref_id"`   // 处方/申请单/就诊/随访 ID
	EncounterID int64  `json:"encounter_id,omitempty"`
	CustomerID  int64  `json:"customer_id,omitempty"`
}

// Signature L1 签名留痕（手写板/平板签署；CA 走 provider 抽象）。
type Signature struct {
	ID               int64     `json:"id"`
	DocumentID       int64     `json:"document_id"`
	SignerType       string    `json:"signer_type"` // patient/agent/doctor/pharmacist/nurse
	SignerCustomerID *int64    `json:"signer_customer_id"`
	SignerEmployeeID *int64    `json:"signer_employee_id"`
	SignaturePath    string    `json:"signature_path"`
	ContentHash      string    `json:"content_hash"`
	SignedAt         time.Time `json:"signed_at"`
	DeviceIP         string    `json:"device_ip"`
	Provider         string    `json:"provider"`
}

// CreateSignatureRequest 签署文书。
type CreateSignatureRequest struct {
	SignerType       string `json:"signer_type"`
	SignerCustomerID *int64 `json:"signer_customer_id,omitempty"`
	SignaturePath    string `json:"signature_path"`
	DeviceIP         string `json:"device_ip,omitempty"`
}

// FollowupDocumentRequest 随访文书补录（外接随访产品，不动随访表）。
type FollowupDocumentRequest struct {
	FollowupID       int64   `json:"followup_id"`
	CustomerID       int64   `json:"customer_id"`
	EncounterID      *int64  `json:"encounter_id,omitempty"`
	FollowupMethod   string  `json:"followup_method"` // 门诊复诊/电话/微信
	FollowupAt       string  `json:"followup_at"`
	Symptoms         string  `json:"symptoms"`
	Adherence        string  `json:"adherence"` // 良好/一般/差
	AdverseReactions string  `json:"adverse_reactions"`
	Assessment       string  `json:"assessment"`
	Education        string  `json:"education"`
	NextFollowupAt   *string `json:"next_followup_at"`
}

// PaymentMethodStat 支付方式统计。
type PaymentMethodStat struct {
	Method string  `json:"method"`
	Amount float64 `json:"amount"`
	Count  int64   `json:"count"`
}

// DoctorWorkloadStat 医生工作量。
type DoctorWorkloadStat struct {
	DoctorID        *int64 `json:"doctor_id"`
	DoctorName      string `json:"doctor_name"`
	VisitCount      int64  `json:"visit_count"`
	PrescriptionCnt int64  `json:"prescription_count"`
}

// DrugConsumptionStat 药品消耗。
type DrugConsumptionStat struct {
	DrugID   int64   `json:"drug_id"`
	DrugName string  `json:"drug_name"`
	Quantity float64 `json:"quantity"`
}

// ReportSummary 经营汇总（?date_from=&date_to=，默认本月）。
type ReportSummary struct {
	Visits          int64                 `json:"visits"`
	CompletedVisits int64                 `json:"completed_visits"`
	Revenue         float64               `json:"revenue"`
	RefundTotal     float64               `json:"refund_total"`
	NetRevenue      float64               `json:"net_revenue"`
	PaymentMethods  []PaymentMethodStat   `json:"payment_methods"`
	DoctorWorkload  []DoctorWorkloadStat  `json:"doctor_workload"`
	TopDrugs        []DrugConsumptionStat `json:"top_drugs"`
}

// OpenClient 开放 API 客户端（挂号小程序/H5）。
type OpenClient struct {
	ID        int64     `json:"id"`
	Name      string    `json:"name"`
	Scopes    string    `json:"scopes"`
	Status    string    `json:"status"`
	RateLimit int       `json:"rate_limit"`
	CreatedAt time.Time `json:"created_at"`
}

// CreateOpenClientRequest 创建开放客户端。
type CreateOpenClientRequest struct {
	Name      string `json:"name"`
	Scopes    string `json:"scopes"`
	RateLimit int    `json:"rate_limit"`
}

// CreateOpenClientResponse API Key 仅返回一次。
type CreateOpenClientResponse struct {
	Client OpenClient `json:"client"`
	APIKey string     `json:"api_key"`
}

// UpdateOpenClientRequest 更新开放客户端。
type UpdateOpenClientRequest struct {
	Status    *string `json:"status"`
	RateLimit *int    `json:"rate_limit"`
}

// OpenDepartment 开放接口科室。
type OpenDepartment struct {
	ID   int64  `json:"id"`
	Name string `json:"name"`
}

// OpenDoctor 开放接口医生。
type OpenDoctor struct {
	DoctorID       int64  `json:"doctor_id"`
	Name           string `json:"name"`
	Title          string `json:"title"`
	DepartmentID   *int64 `json:"department_id"`
	DepartmentName string `json:"department_name"`
}

// OpenSchedule 开放接口排班（含号源）。
type OpenSchedule struct {
	Schedule
	DoctorName     string `json:"doctor_name"`
	DepartmentName string `json:"department_name"`
	Slots          []Slot `json:"slots"`
}

// OpenAppointmentRequest 开放接口预约（患者侧）。
type OpenAppointmentRequest struct {
	PatientName  string `json:"patient_name"`
	PatientPhone string `json:"patient_phone"`
	ScheduleID   int64  `json:"schedule_id"`
	SlotID       *int64 `json:"slot_id"`
}

// OpenAppointment 开放接口挂号单状态。
type OpenAppointment struct {
	RegistrationID int64  `json:"registration_id"`
	SlotID         *int64 `json:"slot_id"`
	SlotTimeStart  string `json:"slot_time_start"`
	SlotTimeEnd    string `json:"slot_time_end"`
	VisitDate      string `json:"visit_date"`
	Status         string `json:"status"`
	DoctorName     string `json:"doctor_name"`
	DepartmentName string `json:"department_name"`
}

// StaffRole 员工角色分配。
type StaffRole struct {
	EmployeeID   int64    `json:"employee_id"`
	EmployeeName string   `json:"employee_name"`
	Roles        []string `json:"roles"`
}

// UpdateStaffRolesRequest 覆盖式设置员工角色（角色可叠加）。
type UpdateStaffRolesRequest struct {
	Roles []string `json:"roles"`
}

// UpdateRoleModulesRequest 覆盖式设置某角色的可见模块（管理员）。
type UpdateRoleModulesRequest struct {
	Modules []string `json:"modules"`
}

// UpdateEmployeeStatusRequest 启用/停用员工（管理员）。
type UpdateEmployeeStatusRequest struct {
	IsActive *bool `json:"is_active"`
}

// ICDDiagnosis ICD-10 诊断目录条目（工作站诊断检索）。
type ICDDiagnosis struct {
	Code string `json:"code"`
	Name string `json:"name"`
}

// TemplateField 模板字段（复用 emr_fields / emr_template_fields，HIS 只读）。
type TemplateField struct {
	Code        string   `json:"code"`
	Name        string   `json:"name"`
	IsVisible   bool     `json:"is_visible"`
	IsRequired  bool     `json:"is_required"`
	Category    string   `json:"category"`
	InputType   string   `json:"input_type"`
	Description string   `json:"description"`
	Options     []string `json:"options"`
}

// QualityResult 质控报告（复用听写病历质检引擎的输出，只读透传）。
type QualityResult struct {
	RunID          string           `json:"run_id"`
	RuleSetVersion string           `json:"rule_set_version"`
	TotalScore     int              `json:"total_score"`
	Grade          string           `json:"grade"`
	BlockingCount  int              `json:"blocking_count"`
	PendingCount   int              `json:"pending_count"`
	OverallResult  string           `json:"overall_result"`
	Results        []QualityFinding `json:"results"`
}

// QualityFinding 单条质控结果。
type QualityFinding struct {
	RuleCode     string `json:"rule_code"`
	SectionCode  string `json:"section_code"`
	Result       string `json:"result"`
	Message      string `json:"message"`
	Blocking     bool   `json:"blocking"`
	ManualReview bool   `json:"manual_review"`
}

// EMRSaveResponse 病历保存/完成响应：病历摘要 + 质控报告（复用引擎输出）。
type EMRSaveResponse struct {
	Record  *EMRRecordSummary `json:"record"`
	Quality *QualityResult    `json:"quality,omitempty"`
}

// CompleteEMRResponse 完成病历响应：正常完成或需确认非阻断问题。
type CompleteEMRResponse struct {
	Record      *EMRRecordSummary `json:"record"`
	Quality     *QualityResult    `json:"quality,omitempty"`
	NeedConfirm bool              `json:"need_confirm,omitempty"`
}

// EmployeeWithRoles 员工（共享 employees）+ HIS 角色 + 医生档案聚合，供管理页员工列表使用。
type EmployeeWithRoles struct {
	ID           int64    `json:"id"`
	Name         string   `json:"name"`
	Phone        string   `json:"phone"`
	Email        string   `json:"email"`
	IsActive     bool     `json:"is_active"`
	Roles        []string `json:"roles"`
	DoctorID     *int64   `json:"doctor_id,omitempty"`
	DepartmentID *int64   `json:"department_id,omitempty"`
	Title        string   `json:"title,omitempty"`
	LicenseNo    string   `json:"license_no,omitempty"`
	Prescribe    bool     `json:"is_prescribing"`
	Doctor       string   `json:"doctor_status,omitempty"`
}

// PatientSearchRow 挂号查重结果（客户 + 病案号）。
type PatientSearchRow struct {
	ID     int64  `json:"id"`
	Name   string `json:"name"`
	Phone  string `json:"phone"`
	Gender string `json:"gender"`
	Age    int    `json:"age"`
	MRN    string `json:"mrn"`
}

// QualityFindingItem 质检结果中的单条规则结果（供 HIS 展示阻断/提示原因）。
type QualityFindingItem struct {
	RuleCode     string `json:"rule_code"`
	SectionCode  string `json:"section_code"`
	Result       string `json:"result"`
	Message      string `json:"message"`
	Blocking     bool   `json:"blocking"`
	ManualReview bool   `json:"manual_review"`
}

// QualityReportLatest 记录最近一次质检报告。
type QualityReportLatest struct {
	RunID          string               `json:"run_id"`
	RuleSetVersion string               `json:"rule_set_version"`
	OverallResult  string               `json:"overall_result"`
	TotalScore     int                  `json:"total_score"`
	Grade          string               `json:"grade"`
	BlockingCount  int                  `json:"blocking_count"`
	PendingCount   int                  `json:"pending_count"`
	StartedAt      *time.Time           `json:"started_at,omitempty"`
	Findings       []QualityFindingItem `json:"findings"`
}

// ChargeItemPage 收费项目分页结果（管理页搜索/分页）。
type ChargeItemPage struct {
	Items    []ChargeItem `json:"items"`
	Total    int          `json:"total"`
	Page     int          `json:"page"`
	PageSize int          `json:"page_size"`
}
