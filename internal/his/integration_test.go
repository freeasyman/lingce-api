package his

// HIS 域 PostgreSQL 集成测试：使用临时 schema + 最小 shim 表，不接触业务数据。
// 设置 HIS_TEST_DATABASE_URL 后运行（跳过逻辑与 followup 集成测试一致）。
// 覆盖：病案号发放与查重、排班号源生成与停诊、挂号锁号、encounter 生产、
// 分诊队列、预约确认/退号/爽约、号源并发抢占、接诊完成闭环。

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
)

func newTestService(t *testing.T) (*Service, *pgxpool.Pool) {
	t.Helper()
	url := os.Getenv("HIS_TEST_DATABASE_URL")
	if url == "" {
		t.Skip("set HIS_TEST_DATABASE_URL for PostgreSQL integration tests")
	}
	ctx := context.Background()
	admin, err := pgxpool.New(ctx, url)
	if err != nil {
		t.Fatal(err)
	}
	schema := fmt.Sprintf("his_test_%d", time.Now().UnixNano())
	if _, err := admin.Exec(ctx, "CREATE SCHEMA "+schema); err != nil {
		admin.Close()
		t.Fatal(err)
	}
	t.Cleanup(func() { _, _ = admin.Exec(ctx, "DROP SCHEMA "+schema+" CASCADE"); admin.Close() })

	cfg, err := pgxpool.ParseConfig(url)
	if err != nil {
		t.Fatal(err)
	}
	cfg.ConnConfig.RuntimeParams["search_path"] = schema + ", public"
	cfg.MaxConns = 12
	pool, err := pgxpool.NewWithConfig(ctx, cfg)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(pool.Close)

	// 最小 shim：tenants/customers/employees/encounters 只需覆盖迁移 FK 与查询所用列。
	// encounters shim 镜像生产约束（source_type CHECK 含 'his' + 唯一索引）。
	if _, err := pool.Exec(ctx, `
		CREATE TABLE tenants (id bigint PRIMARY KEY, name text, code text, deleted_at timestamp);
		CREATE TABLE customers (
			id bigserial PRIMARY KEY, tenant_id bigint, name text, phone text, gender text,
			age int, source text, lifecycle_stage text, status text, momentum int,
			extra_data jsonb, created_by bigint, created_at timestamptz, updated_at timestamptz
		);
		CREATE TABLE employees (
			id bigint PRIMARY KEY, tenant_id bigint, name text, full_name text,
			username text, phone text, deleted_at timestamp
		);
		CREATE TABLE encounters (
			id bigserial PRIMARY KEY,
			tenant_id bigint NOT NULL,
			source_type varchar(32) NOT NULL,
			source_id bigint NOT NULL,
			patient_id bigint,
			patient_name varchar(255),
			patient_candidate_json jsonb NOT NULL DEFAULT '{}'::jsonb,
			provider_id bigint,
			provider_name varchar(128) NOT NULL DEFAULT '',
			provider_role varchar(64) NOT NULL DEFAULT '',
			department_id bigint,
			channel varchar(32) NOT NULL DEFAULT 'store',
			visit_type varchar(32) NOT NULL DEFAULT 'consultation',
			started_at timestamptz,
			ended_at timestamptz,
			status varchar(32) NOT NULL DEFAULT 'new',
			created_at timestamptz NOT NULL DEFAULT NOW(),
			updated_at timestamptz NOT NULL DEFAULT NOW(),
			CONSTRAINT chk_encounters_source_type CHECK (source_type::text = ANY (ARRAY[
				'recording','transcript','phone','wechat','manual','realtime',
				'external_data','external_queue','his'
			]::text[])),
			CONSTRAINT uq_encounters_tenant_source UNIQUE (tenant_id, source_type, source_id),
			CONSTRAINT chk_encounters_status CHECK (status::text = ANY (ARRAY[
				'new','unmatched','matched','analyzed','closed','archived',
				'in_progress','completed','cancelled'
			]::text[]))
		);
		CREATE TABLE emr_templates (
			id uuid PRIMARY KEY DEFAULT gen_random_uuid(), tenant_id bigint, code text, name text,
			version_no text, document_type text, visit_type text, department_id bigint,
			specialty text, status text, print_title text, description text,
			is_current boolean NOT NULL DEFAULT true
		);
		CREATE TABLE emr_records (
			id uuid PRIMARY KEY DEFAULT gen_random_uuid(), tenant_id bigint NOT NULL,
			encounter_id bigint, customer_id bigint, template_id uuid,
			document_type varchar(16), visit_type varchar(16), status varchar(16),
			started_at timestamptz NOT NULL DEFAULT NOW(), updated_at timestamptz NOT NULL DEFAULT NOW(),
			content_json jsonb NOT NULL DEFAULT '{}'::jsonb,
			patient_snapshot_json jsonb NOT NULL DEFAULT '{}'::jsonb,
			created_at timestamptz NOT NULL DEFAULT NOW()
		);
		INSERT INTO tenants VALUES (1, '测试机构', 't1', NULL);
		INSERT INTO employees VALUES (10, 1, '李医生', '李医生', NULL, NULL);
	`); err != nil {
		t.Fatal(err)
	}

	migrationSQL, err := os.ReadFile("../store/migrations/20261107_081_his_core.sql")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := pool.Exec(ctx, string(migrationSQL)); err != nil {
		t.Fatalf("apply his migration: %v", err)
	}
	// 082 扩展 encounters source_type；084 员工角色表（作用在 shim 上，同时校验 SQL 可执行性）。
	for _, migration := range []string{
		"20261107_082_encounter_source_his.sql",
		"20261107_084_his_staff_roles.sql",
	} {
		sourceSQL, err := os.ReadFile("../store/migrations/" + migration)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := pool.Exec(ctx, string(sourceSQL)); err != nil {
			t.Fatalf("apply %s: %v", migration, err)
		}
	}
	return NewService(NewStore(pool)), pool
}

func registerTestPatient(t *testing.T, svc *Service, tenantID int64, name, phone string) RegisterPatientResponse {
	t.Helper()
	resp, err := svc.RegisterPatient(context.Background(), tenantID, 10, RegisterPatientRequest{Name: name, Phone: phone})
	if err != nil {
		t.Fatalf("register patient %s: %v", phone, err)
	}
	return resp
}

func createTestSchedule(t *testing.T, svc *Service, tenantID int64, slotsTime string) Schedule {
	t.Helper()
	doctor, err := svc.CreateDoctor(context.Background(), tenantID, CreateDoctorRequest{EmployeeID: 10, Title: "主治医师"})
	if err != nil {
		t.Fatalf("create doctor: %v", err)
	}
	sched, err := svc.CreateSchedule(context.Background(), tenantID, CreateScheduleRequest{
		DoctorID: doctor.ID, WorkDate: "2026-11-20", Period: "morning",
		StartTime: "09:00", EndTime: slotsTime, SlotIntervalMinutes: 15,
	})
	if err != nil {
		t.Fatalf("create schedule: %v", err)
	}
	return sched
}

func TestPatientRegisterAndMRN(t *testing.T) {
	svc, _ := newTestService(t)

	first := registerTestPatient(t, svc, 1, "张女士", "13800000001")
	if first.MRN != "M00000001" || first.IsExisting {
		t.Fatalf("first register: mrn=%s existing=%v", first.MRN, first.IsExisting)
	}

	again := registerTestPatient(t, svc, 1, "张女士", "13800000001")
	if !again.IsExisting || again.CustomerID != first.CustomerID || again.MRN != "M00000001" {
		t.Fatalf("dedup failed: existing=%v id=%d/%d mrn=%s", again.IsExisting, again.CustomerID, first.CustomerID, again.MRN)
	}

	second := registerTestPatient(t, svc, 1, "王先生", "13800000002")
	if second.MRN != "M00000002" || second.IsExisting {
		t.Fatalf("second register: mrn=%s existing=%v", second.MRN, second.IsExisting)
	}
}

func TestScheduleSlotsAndSuspend(t *testing.T) {
	svc, _ := newTestService(t)

	sched := createTestSchedule(t, svc, 1, "09:30")
	if sched.TotalSlots != 2 || sched.FreeSlots != 2 {
		t.Fatalf("expected 2 free slots, got total=%d free=%d", sched.TotalSlots, sched.FreeSlots)
	}

	suspended := "suspended"
	updated, err := svc.UpdateSchedule(context.Background(), 1, sched.ID, UpdateScheduleRequest{Status: &suspended})
	if err != nil {
		t.Fatalf("suspend: %v", err)
	}
	if updated.Status != "suspended" {
		t.Fatalf("status = %s", updated.Status)
	}
	slots, err := svc.ListSlots(context.Background(), 1, sched.ID)
	if err != nil || len(slots) != 2 {
		t.Fatalf("list slots after suspend: %v %d", err, len(slots))
	}
	for _, slot := range slots {
		if slot.Status != "disabled" {
			t.Fatalf("slot %d status = %s, want disabled", slot.ID, slot.Status)
		}
	}

	opened := "open"
	if _, err := svc.UpdateSchedule(context.Background(), 1, sched.ID, UpdateScheduleRequest{Status: &opened}); err != nil {
		t.Fatalf("reopen: %v", err)
	}
}

func TestWalkinRegistrationFlow(t *testing.T) {
	svc, pool := newTestService(t)

	sched := createTestSchedule(t, svc, 1, "09:30")
	patient := registerTestPatient(t, svc, 1, "张女士", "13800000001")

	first, err := svc.CreateRegistration(context.Background(), 1, 10, CreateRegistrationRequest{
		CustomerID: patient.CustomerID, ScheduleID: sched.ID, RegType: "walkin", Fee: 10,
	})
	if err != nil {
		t.Fatalf("first walkin: %v", err)
	}
	if first.Status != "registered" || first.SlotID == nil {
		t.Fatalf("first walkin: status=%s slot=%v", first.Status, first.SlotID)
	}
	if first.EncounterID == nil {
		t.Fatal("encounter not created for walkin")
	}

	second, err := svc.CreateRegistration(context.Background(), 1, 10, CreateRegistrationRequest{
		CustomerID: patient.CustomerID, ScheduleID: sched.ID, RegType: "walkin", Fee: 10,
	})
	if err != nil {
		t.Fatalf("second walkin: %v", err)
	}
	if *second.SlotID == *first.SlotID {
		t.Fatal("second walkin took the same slot")
	}

	// 号源耗尽
	if _, err := svc.CreateRegistration(context.Background(), 1, 10, CreateRegistrationRequest{
		CustomerID: patient.CustomerID, ScheduleID: sched.ID, RegType: "walkin", Fee: 10,
	}); !errors.Is(err, ErrSlotUnavailable) {
		t.Fatalf("third walkin err = %v, want ErrSlotUnavailable", err)
	}

	// 分诊队列顺序
	queue, err := svc.ListQueue(context.Background(), 1, "2026-11-20", 0, "")
	if err != nil || len(queue) != 2 {
		t.Fatalf("queue: %v len=%d", err, len(queue))
	}
	if queue[0].SeqNo != 1 || queue[1].SeqNo != 2 {
		t.Fatalf("queue seq = %d, %d", queue[0].SeqNo, queue[1].SeqNo)
	}

	// 接诊完成闭环
	item, err := svc.StartQueueItem(context.Background(), 1, first.ID)
	if err != nil || item.Status != "in_progress" {
		t.Fatalf("start queue: %v %v", err, item.Status)
	}
	done, err := svc.CompleteQueueItem(context.Background(), 1, first.ID)
	if err != nil || done.Status != "done" {
		t.Fatalf("complete queue: %v %v", err, done.Status)
	}
	regs, err := svc.ListRegistrations(context.Background(), 1, "2026-11-20", "", 0, 0)
	if err != nil {
		t.Fatal(err)
	}
	var visited bool
	for _, reg := range regs {
		if reg.ID == first.ID && reg.Status == "visited" {
			visited = true
		}
	}
	if !visited {
		t.Fatal("registration not marked visited after completion")
	}
	var encounterStatus string
	_ = pool.QueryRow(context.Background(), `
		SELECT status FROM encounters WHERE source_type='his' AND source_id=$1`, first.ID).Scan(&encounterStatus)
	if encounterStatus != "completed" {
		t.Fatalf("encounter status = %s, want completed", encounterStatus)
	}
}

func TestAppointmentLifecycle(t *testing.T) {
	svc, pool := newTestService(t)

	sched := createTestSchedule(t, svc, 1, "09:30")
	patient := registerTestPatient(t, svc, 1, "张女士", "13800000001")
	slots, err := svc.ListSlots(context.Background(), 1, sched.ID)
	if err != nil || len(slots) != 2 {
		t.Fatalf("slots: %v", err)
	}

	appt, err := svc.CreateRegistration(context.Background(), 1, 10, CreateRegistrationRequest{
		CustomerID: patient.CustomerID, ScheduleID: sched.ID, SlotID: &slots[0].ID,
		RegType: "appointment", Source: "miniprogram", Fee: 10,
	})
	if err != nil {
		t.Fatalf("appointment: %v", err)
	}
	if appt.Status != "reserved" || appt.EncounterID != nil {
		t.Fatalf("appointment should be reserved without encounter: %s %v", appt.Status, appt.EncounterID)
	}

	confirmed, err := svc.ConfirmRegistration(context.Background(), 1, appt.ID)
	if err != nil {
		t.Fatalf("confirm: %v", err)
	}
	if confirmed.Status != "registered" || confirmed.EncounterID == nil {
		t.Fatalf("after confirm: %s %v", confirmed.Status, confirmed.EncounterID)
	}

	// 退号释放号源
	if _, err := svc.RefundRegistration(context.Background(), 1, appt.ID); err != nil {
		t.Fatalf("refund: %v", err)
	}
	var slotStatus string
	var regID *int64
	_ = pool.QueryRow(context.Background(), `SELECT status, registration_id FROM his_slots WHERE id=$1`, slots[0].ID).Scan(&slotStatus, &regID)
	if slotStatus != "free" || regID != nil {
		t.Fatalf("slot after refund: %s %v", slotStatus, regID)
	}
	var encounterStatus string
	_ = pool.QueryRow(context.Background(), `SELECT status FROM encounters WHERE source_type='his' AND source_id=$1`, appt.ID).Scan(&encounterStatus)
	if encounterStatus != "cancelled" {
		t.Fatalf("encounter after refund: %s", encounterStatus)
	}

	// 爽约释放号源
	appt2, err := svc.CreateRegistration(context.Background(), 1, 10, CreateRegistrationRequest{
		CustomerID: patient.CustomerID, ScheduleID: sched.ID, SlotID: &slots[0].ID,
		RegType: "appointment", Source: "desk", Fee: 10,
	})
	if err != nil {
		t.Fatalf("appointment2: %v", err)
	}
	if _, err := svc.NoShowRegistration(context.Background(), 1, appt2.ID); err != nil {
		t.Fatalf("no-show: %v", err)
	}
	_ = pool.QueryRow(context.Background(), `SELECT status, registration_id FROM his_slots WHERE id=$1`, slots[0].ID).Scan(&slotStatus, &regID)
	if slotStatus != "free" || regID != nil {
		t.Fatalf("slot after no-show: %s %v", slotStatus, regID)
	}
}

func TestSlotConcurrentLocking(t *testing.T) {
	svc, _ := newTestService(t)

	sched := createTestSchedule(t, svc, 1, "09:15") // 1 个号源
	p1 := registerTestPatient(t, svc, 1, "甲", "13900000001")
	p2 := registerTestPatient(t, svc, 1, "乙", "13900000002")
	slots, err := svc.ListSlots(context.Background(), 1, sched.ID)
	if err != nil || len(slots) != 1 {
		t.Fatalf("slots: %v", err)
	}

	results := make(chan error, 2)
	var wg sync.WaitGroup
	for _, p := range []RegisterPatientResponse{p1, p2} {
		wg.Add(1)
		go func(customerID int64) {
			defer wg.Done()
			_, err := svc.CreateRegistration(context.Background(), 1, 10, CreateRegistrationRequest{
				CustomerID: customerID, ScheduleID: sched.ID, SlotID: &slots[0].ID,
				RegType: "walkin", Fee: 10,
			})
			results <- err
		}(p.CustomerID)
	}
	wg.Wait()
	close(results)

	successes, failures := 0, 0
	for err := range results {
		if err == nil {
			successes++
		} else if errors.Is(err, ErrSlotUnavailable) {
			failures++
		} else {
			t.Fatalf("unexpected error: %v", err)
		}
	}
	if successes != 1 || failures != 1 {
		t.Fatalf("concurrent locking: %d success, %d failures, want 1/1", successes, failures)
	}
}

func walkinEncounter(t *testing.T, svc *Service, tenantID int64) (int64, RegisterPatientResponse) {
	t.Helper()
	ctx := context.Background()
	sched := createTestSchedule(t, svc, tenantID, "09:15")
	patient := registerTestPatient(t, svc, tenantID, "张女士", "13811112222")
	reg, err := svc.CreateRegistration(ctx, tenantID, 10, CreateRegistrationRequest{
		CustomerID: patient.CustomerID, ScheduleID: sched.ID, RegType: "walkin", Fee: 10,
	})
	if err != nil {
		t.Fatalf("walkin: %v", err)
	}
	if reg.EncounterID == nil {
		t.Fatal("no encounter")
	}
	return *reg.EncounterID, patient
}

func TestPrescriptionFlow(t *testing.T) {
	svc, _ := newTestService(t)
	ctx := context.Background()

	drug, err := svc.CreateDrug(ctx, 1, CreateDrugRequest{
		Code: "AMX001", Name: "阿莫西林胶囊", Spec: "0.25g*24", Unit: "盒",
		DrugType: "western", RetailPrice: 12.5,
	})
	if err != nil {
		t.Fatalf("create drug: %v", err)
	}
	encounterID, _ := walkinEncounter(t, svc, 1)

	p, err := svc.CreatePrescription(ctx, 1, CreatePrescriptionRequest{
		EncounterID: encounterID, Diagnosis: "上呼吸道感染",
		Items: []PrescriptionItemInput{{DrugID: drug.ID, Dose: "0.5g", DoseUnit: "g", Frequency: "tid", Route: "口服", Quantity: 2}},
	})
	if err != nil {
		t.Fatalf("create prescription: %v", err)
	}
	if p.Status != "draft" || p.PrescriptionNo != "" || len(p.Items) != 1 {
		t.Fatalf("draft: %+v", p)
	}
	if p.Items[0].Price != 12.5 || p.Items[0].Amount != 25 {
		t.Fatalf("pricing: price=%v amount=%v", p.Items[0].Price, p.Items[0].Amount)
	}

	issued, err := svc.IssuePrescription(ctx, 1, p.ID)
	if err != nil {
		t.Fatalf("issue: %v", err)
	}
	if issued.Status != "issued" || !strings.HasPrefix(issued.PrescriptionNo, "P") {
		t.Fatalf("issued: %+v", issued)
	}

	if _, err := svc.VoidPrescription(ctx, 1, p.ID); err != nil {
		t.Fatalf("void: %v", err)
	}
	if _, err := svc.UpdatePrescription(ctx, 1, p.ID, UpdatePrescriptionRequest{Diagnosis: strPtr("x")}); !errors.Is(err, ErrInvalidState) {
		t.Fatalf("update voided err = %v", err)
	}
}

func TestHerbPrescriptionType(t *testing.T) {
	svc, _ := newTestService(t)
	ctx := context.Background()

	herb, err := svc.CreateDrug(ctx, 1, CreateDrugRequest{
		Code: "H001", Name: "当归", Spec: "统装", Unit: "g", DrugType: "chinese_herb", RetailPrice: 0.5,
	})
	if err != nil {
		t.Fatal(err)
	}
	western, err := svc.CreateDrug(ctx, 1, CreateDrugRequest{
		Code: "W001", Name: "维生素C片", Spec: "100mg*100", Unit: "瓶", DrugType: "western", RetailPrice: 8,
	})
	if err != nil {
		t.Fatal(err)
	}
	encounterID, _ := walkinEncounter(t, svc, 1)

	herbRx, err := svc.CreatePrescription(ctx, 1, CreatePrescriptionRequest{
		EncounterID: encounterID, Diagnosis: "血虚",
		Items: []PrescriptionItemInput{{DrugID: herb.ID, Dose: "10g", Quantity: 7, UsageText: "每日一剂，水煎服"}},
	})
	if err != nil {
		t.Fatal(err)
	}
	if herbRx.Type != "herbal" || herbRx.Items[0].ItemType != "herb" {
		t.Fatalf("herb type: %s %s", herbRx.Type, herbRx.Items[0].ItemType)
	}

	mixedRx, err := svc.CreatePrescription(ctx, 1, CreatePrescriptionRequest{
		EncounterID: encounterID, Diagnosis: "气血两虚",
		Items: []PrescriptionItemInput{
			{DrugID: herb.ID, Dose: "10g", Quantity: 7},
			{DrugID: western.ID, Dose: "1片", Frequency: "bid", Quantity: 1},
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	if mixedRx.Type != "mixed" {
		t.Fatalf("mixed type: %s", mixedRx.Type)
	}
}

func TestServiceRequestAndTreatmentFlow(t *testing.T) {
	svc, _ := newTestService(t)
	ctx := context.Background()

	item, err := svc.CreateChargeItem(ctx, 1, CreateChargeItemRequest{
		Code: "TRT001", Name: "红光治疗", Category: "治疗", Unit: "次", Price: 88,
	})
	if err != nil {
		t.Fatal(err)
	}
	encounterID, _ := walkinEncounter(t, svc, 1)

	req, err := svc.CreateServiceRequest(ctx, 1, CreateServiceRequestRequest{
		EncounterID: encounterID, RequestType: "lab", ItemName: "血常规", SpecimenType: "静脉血", Purpose: "炎症排查",
	})
	if err != nil || req.Status != "ordered" {
		t.Fatalf("service request: %v %+v", err, req)
	}

	order, err := svc.CreateTreatmentOrder(ctx, 1, CreateTreatmentOrderRequest{
		EncounterID: encounterID, ChargeItemID: item.ID, PlanTotalTimes: 2,
	})
	if err != nil || order.ChargeItemName != "红光治疗" {
		t.Fatalf("treatment order: %v %+v", err, order)
	}

	exec1, err := svc.ExecuteTreatment(ctx, 1, order.ID, 10, "第一次")
	if err != nil || exec1.CompletedTimes != 1 || exec1.Status != "executing" {
		t.Fatalf("exec1: %v %+v", err, exec1)
	}
	exec2, err := svc.ExecuteTreatment(ctx, 1, order.ID, 10, "第二次")
	if err != nil || exec2.CompletedTimes != 2 || exec2.Status != "completed" {
		t.Fatalf("exec2: %v %+v", err, exec2)
	}
	if _, err := svc.ExecuteTreatment(ctx, 1, order.ID, 10, "超额"); !errors.Is(err, ErrInvalidState) {
		t.Fatalf("over-exec err = %v", err)
	}
}

func TestWorkbenchContext(t *testing.T) {
	svc, _ := newTestService(t)
	ctx := context.Background()

	sched := createTestSchedule(t, svc, 1, "09:15")
	patient := registerTestPatient(t, svc, 1, "赵女士", "13822223333")
	reg, err := svc.CreateRegistration(ctx, 1, 10, CreateRegistrationRequest{
		CustomerID: patient.CustomerID, ScheduleID: sched.ID, RegType: "walkin", Fee: 10,
	})
	if err != nil || reg.EncounterID == nil {
		t.Fatalf("walkin: %v", err)
	}

	context2, err := svc.WorkbenchContext(ctx, 1, *reg.EncounterID)
	if err != nil {
		t.Fatalf("context: %v", err)
	}
	if context2.Customer.CustomerID != patient.CustomerID || context2.Customer.MRN != patient.MRN {
		t.Fatalf("customer summary: %+v", context2.Customer)
	}
	if context2.Customer.Allergy != "无" {
		t.Fatalf("default allergy: %s", context2.Customer.Allergy)
	}
	if context2.Registration == nil || context2.Registration.ID != reg.ID {
		t.Fatalf("registration missing: %+v", context2.Registration)
	}
}

func strPtr(v string) *string { return &v }

func TestBundleOrderPricing(t *testing.T) {
	svc, _ := newTestService(t)
	ctx := context.Background()

	item1, err := svc.CreateChargeItem(ctx, 1, CreateChargeItemRequest{Code: "C01", Name: "妇科检查", Category: "检查", Unit: "次", Price: 50})
	if err != nil {
		t.Fatal(err)
	}
	item2, err := svc.CreateChargeItem(ctx, 1, CreateChargeItemRequest{Code: "C02", Name: "红光治疗", Category: "治疗", Unit: "次", Price: 55})
	if err != nil {
		t.Fatal(err)
	}
	// 原子合计 105，打包价 88（独立定价）
	bundle, err := svc.CreateBundle(ctx, 1, CreateBundleRequest{
		Code: "B01", Name: "妇科检查+红光套餐", Price: 88,
		Items: []BundleItem{{ChargeItemID: item1.ID, Quantity: 1}, {ChargeItemID: item2.ID, Quantity: 1}},
	})
	if err != nil {
		t.Fatal(err)
	}
	encounterID, _ := walkinEncounter(t, svc, 1)

	order, err := svc.CreateOrder(ctx, 1, 10, CreateOrderRequest{
		EncounterID: encounterID, DiscountTotal: 8,
		Items: []OrderLineInput{
			{ItemType: "bundle", RefID: bundle.ID, Quantity: 1},
			{ItemType: "charge_item", RefID: item1.ID, Quantity: 1},
		},
	})
	if err != nil {
		t.Fatalf("create order: %v", err)
	}
	if order.ItemsTotal != 138 || order.DiscountTotal != 8 || order.PayableTotal != 130 {
		t.Fatalf("totals: items=%v discount=%v payable=%v", order.ItemsTotal, order.DiscountTotal, order.PayableTotal)
	}
	if order.Items[0].UnitPrice != 88 {
		t.Fatalf("bundle line price = %v, want 88", order.Items[0].UnitPrice)
	}

	// 全额收款
	paid, err := svc.AddPayment(ctx, 1, 10, order.ID, CreatePaymentRequest{Method: "wechat", Amount: 130})
	if err != nil {
		t.Fatalf("pay: %v", err)
	}
	if paid.Status != "paid" || paid.PaidTotal != 130 {
		t.Fatalf("paid order: %+v", paid)
	}

	// 已收费不可调整
	if _, err := svc.UpdateOrder(ctx, 1, order.ID, UpdateOrderRequest{DiscountTotal: floatPtr(10)}); !errors.Is(err, ErrInvalidState) {
		t.Fatalf("update paid order err = %v", err)
	}

	// 退费（无处方行，直接退）
	refunded, err := svc.RefundOrder(ctx, 1, 10, order.ID, "客户取消")
	if err != nil || refunded.Status != "refunded" {
		t.Fatalf("refund: %v %+v", err, refunded)
	}
}

func TestOrderPaymentChargesPrescriptionAndDispense(t *testing.T) {
	svc, _ := newTestService(t)
	ctx := context.Background()

	drug, err := svc.CreateDrug(ctx, 1, CreateDrugRequest{
		Code: "AMX002", Name: "阿莫西林胶囊", Spec: "0.25g*24", Unit: "盒",
		DrugType: "western", RetailPrice: 12.5,
	})
	if err != nil {
		t.Fatal(err)
	}
	encounterID, _ := walkinEncounter(t, svc, 1)
	rx, err := svc.CreatePrescription(ctx, 1, CreatePrescriptionRequest{
		EncounterID: encounterID, Diagnosis: "上呼吸道感染",
		Items: []PrescriptionItemInput{{DrugID: drug.ID, Dose: "0.5g", Quantity: 2}},
	})
	if err != nil {
		t.Fatal(err)
	}
	issued, err := svc.IssuePrescription(ctx, 1, rx.ID)
	if err != nil {
		t.Fatal(err)
	}

	order, err := svc.CreateOrder(ctx, 1, 10, CreateOrderRequest{
		EncounterID: encounterID,
		Items: []OrderLineInput{{
			ItemType: "drug", RefID: drug.ID, Quantity: 2, PrescriptionID: &issued.ID,
		}},
	})
	if err != nil {
		t.Fatalf("create order: %v", err)
	}
	if _, err := svc.AddPayment(ctx, 1, 10, order.ID, CreatePaymentRequest{Method: "cash", Amount: order.PayableTotal}); err != nil {
		t.Fatalf("pay: %v", err)
	}

	charged, err := svc.ListPrescriptions(ctx, 1, encounterID)
	if err != nil || charged[0].Status != "charged" {
		t.Fatalf("rx after pay: %v %+v", err, charged)
	}

	// 入库 10 盒后发药 2 盒
	if _, err := svc.InboundStock(ctx, 1, 10, InboundStockRequest{DrugID: drug.ID, BatchNo: "B20261101", Quantity: 10}); err != nil {
		t.Fatalf("inbound: %v", err)
	}
	dispensed, err := svc.DispensePrescription(ctx, 1, 10, rx.ID)
	if err != nil || dispensed.Status != "dispensed" {
		t.Fatalf("dispense: %v %+v", err, dispensed)
	}
	stock, err := svc.ListStock(ctx, 1, drug.Name)
	if err != nil || len(stock) != 1 || stock[0].Quantity != 8 {
		t.Fatalf("stock after dispense: %v %+v", err, stock)
	}

	// 已发药 → 退费拒绝
	if _, err := svc.RefundOrder(ctx, 1, 10, order.ID, "退"); !errors.Is(err, ErrDispensedCannotRefund) {
		t.Fatalf("refund dispensed err = %v", err)
	}
}

func TestRefundRestoresPrescriptionToIssued(t *testing.T) {
	svc, _ := newTestService(t)
	ctx := context.Background()

	drug, err := svc.CreateDrug(ctx, 1, CreateDrugRequest{
		Code: "AMX003", Name: "阿莫西林胶囊", Spec: "0.25g*24", Unit: "盒", DrugType: "western", RetailPrice: 12.5,
	})
	if err != nil {
		t.Fatal(err)
	}
	encounterID, _ := walkinEncounter(t, svc, 1)
	rx, _ := svc.CreatePrescription(ctx, 1, CreatePrescriptionRequest{
		EncounterID: encounterID, Diagnosis: "感染",
		Items: []PrescriptionItemInput{{DrugID: drug.ID, Quantity: 1}},
	})
	issued, _ := svc.IssuePrescription(ctx, 1, rx.ID)

	order, err := svc.CreateOrder(ctx, 1, 10, CreateOrderRequest{
		EncounterID: encounterID,
		Items:       []OrderLineInput{{ItemType: "drug", RefID: drug.ID, Quantity: 1, PrescriptionID: &issued.ID}},
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := svc.AddPayment(ctx, 1, 10, order.ID, CreatePaymentRequest{Method: "cash", Amount: order.PayableTotal}); err != nil {
		t.Fatal(err)
	}
	if _, err := svc.RefundOrder(ctx, 1, 10, order.ID, "退费"); err != nil {
		t.Fatal(err)
	}
	list, err := svc.ListPrescriptions(ctx, 1, encounterID)
	if err != nil || list[0].Status != "issued" {
		t.Fatalf("rx after refund: %v %+v", err, list)
	}
}

func TestVoidOrderAndInsufficientStock(t *testing.T) {
	svc, _ := newTestService(t)
	ctx := context.Background()

	item, err := svc.CreateChargeItem(ctx, 1, CreateChargeItemRequest{Code: "C03", Name: "雾化", Category: "治疗", Unit: "次", Price: 30})
	if err != nil {
		t.Fatal(err)
	}
	drug, err := svc.CreateDrug(ctx, 1, CreateDrugRequest{
		Code: "AMX004", Name: "头孢", Spec: "0.25g*24", Unit: "盒", DrugType: "western", RetailPrice: 20,
	})
	if err != nil {
		t.Fatal(err)
	}
	encounterID, _ := walkinEncounter(t, svc, 1)

	pending, err := svc.CreateOrder(ctx, 1, 10, CreateOrderRequest{
		EncounterID: encounterID,
		Items:       []OrderLineInput{{ItemType: "charge_item", RefID: item.ID, Quantity: 1}},
	})
	if err != nil {
		t.Fatal(err)
	}
	// 部分收款后不能作废
	if _, err := svc.AddPayment(ctx, 1, 10, pending.ID, CreatePaymentRequest{Method: "cash", Amount: 10}); err != nil {
		t.Fatal(err)
	}
	if _, err := svc.VoidOrder(ctx, 1, pending.ID); !errors.Is(err, ErrInvalidState) {
		t.Fatalf("void partial order err = %v", err)
	}

	// 库存不足发药
	rx, _ := svc.CreatePrescription(ctx, 1, CreatePrescriptionRequest{
		EncounterID: encounterID, Diagnosis: "感染",
		Items: []PrescriptionItemInput{{DrugID: drug.ID, Quantity: 5}},
	})
	issued, _ := svc.IssuePrescription(ctx, 1, rx.ID)
	order2, err := svc.CreateOrder(ctx, 1, 10, CreateOrderRequest{
		EncounterID: encounterID,
		Items:       []OrderLineInput{{ItemType: "drug", RefID: drug.ID, Quantity: 5, PrescriptionID: &issued.ID}},
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := svc.AddPayment(ctx, 1, 10, order2.ID, CreatePaymentRequest{Method: "cash", Amount: order2.PayableTotal}); err != nil {
		t.Fatal(err)
	}
	if _, err := svc.InboundStock(ctx, 1, 10, InboundStockRequest{DrugID: drug.ID, Quantity: 2}); err != nil {
		t.Fatal(err)
	}
	if _, err := svc.DispensePrescription(ctx, 1, 10, rx.ID); !errors.Is(err, ErrInsufficientStock) {
		t.Fatalf("dispense insufficient err = %v", err)
	}
}

func floatPtr(v float64) *float64 { return &v }

func TestPrescriptionDocumentLifecycle(t *testing.T) {
	svc, pool := newTestService(t)
	ctx := context.Background()

	drug, err := svc.CreateDrug(ctx, 1, CreateDrugRequest{
		Code: "DOC01", Name: "阿莫西林胶囊", Spec: "0.25g*24", Unit: "盒", DrugType: "western", RetailPrice: 12.5,
	})
	if err != nil {
		t.Fatal(err)
	}
	encounterID, _ := walkinEncounter(t, svc, 1)
	rx, err := svc.CreatePrescription(ctx, 1, CreatePrescriptionRequest{
		EncounterID: encounterID, Diagnosis: "上呼吸道感染",
		Items: []PrescriptionItemInput{{DrugID: drug.ID, Dose: "0.5g", Frequency: "tid", Quantity: 2}},
	})
	if err != nil {
		t.Fatal(err)
	}
	issued, err := svc.IssuePrescription(ctx, 1, rx.ID)
	if err != nil {
		t.Fatal(err)
	}

	doc, err := svc.GenerateDocument(ctx, 1, GenerateDocumentRequest{DocType: "门诊处方", RefID: rx.ID})
	if err != nil {
		t.Fatalf("generate: %v", err)
	}
	if doc.Status != "draft" {
		t.Fatalf("doc status: %s", doc.Status)
	}
	snapshot := doc.ContentSnapshot
	if snapshot["prescription_no"] != issued.PrescriptionNo || snapshot["name"] != "张女士" {
		t.Fatalf("snapshot: %+v", snapshot)
	}

	// 幂等：重复生成返回同一条
	again, err := svc.GenerateDocument(ctx, 1, GenerateDocumentRequest{DocType: "门诊处方", RefID: rx.ID})
	if err != nil || again.ID != doc.ID {
		t.Fatalf("idempotent: %v %d/%d", err, again.ID, doc.ID)
	}

	// 签名
	signed, err := svc.SignDocument(ctx, 1, 10, doc.ID, CreateSignatureRequest{
		SignerType: "doctor", SignaturePath: "/signatures/doc1.png",
	})
	if err != nil || signed.Status != "signed" || len(signed.Signatures) != 1 {
		t.Fatalf("sign: %v %+v", err, signed)
	}

	// 篡改快照 → 归档拒绝
	if _, err := pool.Exec(ctx, `
		UPDATE his_documents SET content_snapshot = jsonb_set(content_snapshot, '{name}', '"篡改"')
		WHERE id = $1`, doc.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := svc.ArchiveDocument(ctx, 1, doc.ID); !errors.Is(err, ErrSignatureMismatch) {
		t.Fatalf("archive after tamper err = %v", err)
	}
	// 还原快照 → 归档成功
	raw, _ := json.Marshal(snapshot)
	if _, err := pool.Exec(ctx, `UPDATE his_documents SET content_snapshot = $2::jsonb WHERE id = $1`, doc.ID, string(raw)); err != nil {
		t.Fatal(err)
	}
	archived, err := svc.ArchiveDocument(ctx, 1, doc.ID)
	if err != nil || archived.Status != "archived" {
		t.Fatalf("archive: %v %+v", err, archived)
	}
	printed, err := svc.PrintDocument(ctx, 1, doc.ID)
	if err != nil || printed.Status != "printed" || printed.PrintedAt == nil {
		t.Fatalf("print: %v %+v", err, printed)
	}
}

func TestServiceRequestAndFollowupDocuments(t *testing.T) {
	svc, _ := newTestService(t)
	ctx := context.Background()

	encounterID, _ := walkinEncounter(t, svc, 1)
	req, err := svc.CreateServiceRequest(ctx, 1, CreateServiceRequestRequest{
		EncounterID: encounterID, RequestType: "lab", ItemName: "血常规", SpecimenType: "静脉血", Purpose: "炎症排查",
	})
	if err != nil {
		t.Fatal(err)
	}
	doc, err := svc.GenerateDocument(ctx, 1, GenerateDocumentRequest{DocType: "检查申请单", RefID: req.ID})
	if err != nil {
		t.Fatalf("generate request doc: %v", err)
	}
	if doc.ContentSnapshot["items"] != "血常规" || doc.ContentSnapshot["specimen_type"] != "静脉血" {
		t.Fatalf("request doc snapshot: %+v", doc.ContentSnapshot)
	}

	// 随访文书：补录两次 → 更新同一条扩展记录
	patient := registerTestPatient(t, svc, 1, "随访患者", "13833334444")
	followupDoc, err := svc.SubmitFollowupDocument(ctx, 1, FollowupDocumentRequest{
		FollowupID: 9001, CustomerID: patient.CustomerID, FollowupMethod: "电话",
		FollowupAt: "2026-11-07", Symptoms: "已缓解", Adherence: "良好",
		AdverseReactions: "无", Assessment: "好转", Education: "按时服药",
	})
	if err != nil || followupDoc.ContentSnapshot["adherence"] != "良好" {
		t.Fatalf("followup doc: %v %+v", err, followupDoc)
	}
	updated, err := svc.SubmitFollowupDocument(ctx, 1, FollowupDocumentRequest{
		FollowupID: 9001, CustomerID: patient.CustomerID, FollowupMethod: "电话",
		FollowupAt: "2026-11-07", Symptoms: "完全缓解", Adherence: "良好",
		AdverseReactions: "无", Assessment: "痊愈", Education: "按时服药",
	})
	if err != nil || updated.ContentSnapshot["symptoms"] != "完全缓解" {
		t.Fatalf("followup doc update: %v %+v", err, updated)
	}
}

func TestReportSummary(t *testing.T) {
	svc, _ := newTestService(t)
	ctx := context.Background()

	item, err := svc.CreateChargeItem(ctx, 1, CreateChargeItemRequest{Code: "RP01", Name: "诊查费", Category: "诊查", Unit: "次", Price: 50})
	if err != nil {
		t.Fatal(err)
	}
	encounterID, _ := walkinEncounter(t, svc, 1)
	order, err := svc.CreateOrder(ctx, 1, 10, CreateOrderRequest{
		EncounterID: encounterID,
		Items:       []OrderLineInput{{ItemType: "charge_item", RefID: item.ID, Quantity: 1}},
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := svc.AddPayment(ctx, 1, 10, order.ID, CreatePaymentRequest{Method: "wechat", Amount: 50}); err != nil {
		t.Fatal(err)
	}

	summary, err := svc.ReportSummary(ctx, 1, "2026-11-01", "2026-12-31")
	if err != nil {
		t.Fatalf("report: %v", err)
	}
	if summary.Visits < 1 || summary.CompletedVisits > summary.Visits {
		t.Fatalf("visits: %+v", summary)
	}
	if summary.Revenue < 50 || summary.NetRevenue != summary.Revenue-summary.RefundTotal {
		t.Fatalf("revenue: %+v", summary)
	}
	if len(summary.PaymentMethods) == 0 || summary.PaymentMethods[0].Method != "wechat" {
		t.Fatalf("methods: %+v", summary.PaymentMethods)
	}
}

func TestOpenClientAndAppointmentFlow(t *testing.T) {
	svc, _ := newTestService(t)
	ctx := context.Background()

	clientResp, err := svc.CreateOpenClient(ctx, 1, CreateOpenClientRequest{Name: "挂号小程序", Scopes: "read,appointment", RateLimit: 60})
	if err != nil || clientResp.APIKey == "" {
		t.Fatalf("create client: %v %+v", err, clientResp)
	}
	// Key 哈希可解析、明文不复现
	resolved, tenantID, err := svc.store.ResolveOpenClient(ctx, APIKeyHash(clientResp.APIKey))
	if err != nil || resolved.ID != clientResp.Client.ID || tenantID != 1 {
		t.Fatalf("resolve client: %v %+v %d", err, resolved, tenantID)
	}
	if _, _, err := svc.store.ResolveOpenClient(ctx, APIKeyHash("lk_wrong")); err == nil {
		t.Fatal("wrong key resolved")
	}

	sched := createTestSchedule(t, svc, 1, "09:15")

	appt, err := svc.CreateOpenAppointment(ctx, 1, OpenAppointmentRequest{
		PatientName: "小程序患者", PatientPhone: "13844445555", ScheduleID: sched.ID,
	})
	if err != nil || appt.Status != "reserved" {
		t.Fatalf("open appointment: %v %+v", err, appt)
	}

	// 预约阶段不发病案号
	var customerID int64
	_ = svc.store.pool.QueryRow(ctx, `SELECT customer_id FROM his_registrations WHERE id = $1`, appt.RegistrationID).Scan(&customerID)
	identifiers, err := svc.ListPatientIdentifiers(ctx, 1, customerID)
	if err != nil || len(identifiers) != 0 {
		t.Fatalf("identifiers before confirm: %v %+v", err, identifiers)
	}

	// 患者取消 → 号源释放
	cancelled, err := svc.CancelOpenAppointment(ctx, 1, appt.RegistrationID)
	if err != nil || cancelled.Status != "cancelled" {
		t.Fatalf("cancel: %v %+v", err, cancelled)
	}

	// 再约 → 到院确认 → 病案号发放
	appt2, err := svc.CreateOpenAppointment(ctx, 1, OpenAppointmentRequest{
		PatientName: "小程序患者", PatientPhone: "13844445555", ScheduleID: sched.ID,
	})
	if err != nil {
		t.Fatalf("appointment2: %v", err)
	}
	confirmed, err := svc.ConfirmRegistration(ctx, 1, appt2.RegistrationID)
	if err != nil || confirmed.Status != "registered" {
		t.Fatalf("confirm: %v %+v", err, confirmed)
	}
	identifiers, err = svc.ListPatientIdentifiers(ctx, 1, customerID)
	if err != nil || len(identifiers) != 1 || identifiers[0].IdentifierType != "mrn" {
		t.Fatalf("identifiers after confirm: %v %+v", err, identifiers)
	}
}

func TestOpenReservationExpiryRelease(t *testing.T) {
	svc, _ := newTestService(t)
	ctx := context.Background()

	sched := createTestSchedule(t, svc, 1, "09:15")
	appt, err := svc.CreateOpenAppointment(ctx, 1, OpenAppointmentRequest{
		PatientName: "超时患者", PatientPhone: "13855556666", ScheduleID: sched.ID,
	})
	if err != nil {
		t.Fatalf("appointment: %v", err)
	}
	// 模拟 10 分钟未确认
	if _, err := svc.store.pool.Exec(ctx, `
		UPDATE his_registrations SET created_at = NOW() - interval '10 minutes' WHERE id = $1`, appt.RegistrationID); err != nil {
		t.Fatal(err)
	}

	schedules, err := svc.ListOpenSchedules(ctx, 1, "2026-11-20", 0, 0)
	if err != nil {
		t.Fatalf("list schedules: %v", err)
	}
	if len(schedules) != 1 || schedules[0].FreeSlots != 1 {
		t.Fatalf("expired slot not released: %+v", schedules)
	}
	var status string
	_ = svc.store.pool.QueryRow(ctx, `SELECT status FROM his_registrations WHERE id = $1`, appt.RegistrationID).Scan(&status)
	if status != "expired" {
		t.Fatalf("registration status = %s, want expired", status)
	}
}

func TestRateLimiter(t *testing.T) {
	limiter := newRateLimiter()
	for i := 0; i < 5; i++ {
		if !limiter.allow(1, 5) {
			t.Fatalf("request %d should be allowed", i)
		}
	}
	if limiter.allow(1, 5) {
		t.Fatal("6th request should be rate limited")
	}
	if !limiter.allow(2, 5) {
		t.Fatal("other client should not be affected")
	}
}

func TestStaffRoles(t *testing.T) {
	svc, _ := newTestService(t)
	ctx := context.Background()

	// 未配置任何角色 → MyRoles 返回 nil（前端全开放），RequireRole 放行
	roles, err := svc.MyRoles(ctx, 1, 10)
	if err != nil || roles != nil {
		t.Fatalf("my roles before config: %v %v", err, roles)
	}
	if err := svc.RequireRole(ctx, 1, 10, "admin"); err != nil {
		t.Fatalf("require role before config should pass: %v", err)
	}

	// 分配角色：员工 10 = cashier + pharmacist（可叠加）
	if err := svc.UpdateStaffRoles(ctx, 1, 10, []string{"cashier", "pharmacist"}); err != nil {
		t.Fatalf("update roles: %v", err)
	}
	list, err := svc.ListStaffRoles(ctx, 1)
	if err != nil || len(list) == 0 {
		t.Fatalf("list roles: %v %+v", err, list)
	}
	found := false
	for _, item := range list {
		if item.EmployeeID == 10 {
			found = true
			if len(item.Roles) != 2 || item.Roles[0] != "cashier" || item.Roles[1] != "pharmacist" {
				t.Fatalf("roles for 10: %v", item.Roles)
			}
		}
	}
	if !found {
		t.Fatal("employee 10 not in role list")
	}

	roles, err = svc.MyRoles(ctx, 1, 10)
	if err != nil || len(roles) != 2 {
		t.Fatalf("my roles: %v %v", err, roles)
	}

	// 权限校验
	if err := svc.RequireRole(ctx, 1, 10, "cashier"); err != nil {
		t.Fatalf("cashier should pass: %v", err)
	}
	if err := svc.RequireRole(ctx, 1, 10, "doctor"); !errors.Is(err, ErrForbiddenRole) {
		t.Fatalf("doctor should be forbidden: %v", err)
	}

	// 覆盖式更新
	if err := svc.UpdateStaffRoles(ctx, 1, 10, []string{"admin"}); err != nil {
		t.Fatalf("replace roles: %v", err)
	}
	roles, _ = svc.MyRoles(ctx, 1, 10)
	if len(roles) != 1 || roles[0] != "admin" {
		t.Fatalf("replaced roles: %v", roles)
	}

	// 非法角色拒绝
	if err := svc.UpdateStaffRoles(ctx, 1, 10, []string{"superman"}); !errors.Is(err, ErrInvalidRequest) {
		t.Fatalf("invalid role err: %v", err)
	}

	// 不存在员工拒绝
	if err := svc.UpdateStaffRoles(ctx, 1, 99999, []string{"admin"}); !errors.Is(err, ErrNotFound) {
		t.Fatalf("missing employee err: %v", err)
	}
}
