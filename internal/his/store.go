package his

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"
)

// querier 抽象 *pgxpool.Pool 与 pgx.Tx 的公共查询能力。
type querier interface {
	Exec(ctx context.Context, sql string, args ...any) (pgconn.CommandTag, error)
	Query(ctx context.Context, sql string, args ...any) (pgx.Rows, error)
	QueryRow(ctx context.Context, sql string, args ...any) pgx.Row
}

// Store 是 HIS 域的数据访问层。
type Store struct {
	pool *pgxpool.Pool
}

// NewStore 创建 Store。
func NewStore(pool *pgxpool.Pool) *Store {
	return &Store{pool: pool}
}

// mrnFormat 病案号格式：M + 8 位流水（租户内唯一、永不复用）。
func mrnFormat(seq int64) string {
	return fmt.Sprintf("M%08d", seq)
}

// ---------------------------------------------------------------------------
// 仪表盘

// DashboardMetrics 返回门户卡片指标。
func (s *Store) DashboardMetrics(ctx context.Context, tenantID int64) (DashboardMetrics, error) {
	var m DashboardMetrics
	// 今日门诊量：今日已挂号（含已就诊）
	err := s.pool.QueryRow(ctx, `
		SELECT COUNT(*) FROM his_registrations
		WHERE tenant_id = $1 AND visit_date = CURRENT_DATE
		  AND status IN ('registered','visited')`, tenantID).Scan(&m.TodayVisits)
	if err != nil {
		return m, err
	}
	// 待就诊：今日已挂号未接诊
	err = s.pool.QueryRow(ctx, `
		SELECT COUNT(*) FROM his_registrations
		WHERE tenant_id = $1 AND visit_date = CURRENT_DATE
		  AND status = 'registered'`, tenantID).Scan(&m.PendingVisits)
	if err != nil {
		return m, err
	}
	// 待审核处方：已开具未审核
	err = s.pool.QueryRow(ctx, `
		SELECT COUNT(*) FROM his_prescriptions
		WHERE tenant_id = $1 AND status = 'issued'`, tenantID).Scan(&m.PendingPrescriptions)
	if err != nil {
		return m, err
	}
	return m, nil
}

// ---------------------------------------------------------------------------
// 医生

const doctorColumns = `id, employee_id, department_id, title, license_no, is_prescribing, status`

func scanDoctor(row pgx.Row) (Doctor, error) {
	var d Doctor
	err := row.Scan(&d.ID, &d.EmployeeID, &d.DepartmentID, &d.Title, &d.LicenseNo, &d.IsPrescribing, &d.Status)
	return d, err
}

// ListDoctors 返回租户下全部医生（含停用），供管理台配置。
func (s *Store) ListDoctors(ctx context.Context, tenantID int64) ([]Doctor, error) {
	rows, err := s.pool.Query(ctx, `
		SELECT `+doctorColumns+` FROM his_doctors
		WHERE tenant_id = $1 ORDER BY id`, tenantID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	doctors := make([]Doctor, 0)
	for rows.Next() {
		d, err := scanDoctor(rows)
		if err != nil {
			return nil, err
		}
		doctors = append(doctors, d)
	}
	return doctors, rows.Err()
}

// GetDoctor 返回单个医生。
func (s *Store) GetDoctor(ctx context.Context, tenantID, id int64) (Doctor, error) {
	return scanDoctor(s.pool.QueryRow(ctx, `
		SELECT `+doctorColumns+` FROM his_doctors
		WHERE tenant_id = $1 AND id = $2`, tenantID, id))
}

// CreateDoctor 新增医生（绑定员工）。
func (s *Store) CreateDoctor(ctx context.Context, tenantID int64, req CreateDoctorRequest) (Doctor, error) {
	isPrescribing := true
	if req.IsPrescribing != nil {
		isPrescribing = *req.IsPrescribing
	}
	row := s.pool.QueryRow(ctx, `
		INSERT INTO his_doctors (tenant_id, employee_id, department_id, title, license_no, is_prescribing)
		VALUES ($1, $2, $3, $4, $5, $6)
		RETURNING `+doctorColumns,
		tenantID, req.EmployeeID, req.DepartmentID, req.Title, req.LicenseNo, isPrescribing)
	return scanDoctor(row)
}

// UpdateDoctor 部分更新医生。
func (s *Store) UpdateDoctor(ctx context.Context, tenantID, id int64, req UpdateDoctorRequest) (Doctor, error) {
	sets := []string{}
	args := []any{tenantID, id}
	add := func(column string, value any) {
		args = append(args, value)
		sets = append(sets, fmt.Sprintf("%s = COALESCE($%d, %s)", column, len(args), column))
	}
	if req.DepartmentID != nil {
		add("department_id", req.DepartmentID)
	}
	if req.Title != nil {
		add("title", req.Title)
	}
	if req.LicenseNo != nil {
		add("license_no", req.LicenseNo)
	}
	if req.IsPrescribing != nil {
		add("is_prescribing", req.IsPrescribing)
	}
	if req.Status != nil {
		add("status", req.Status)
	}
	if len(sets) == 0 {
		return s.GetDoctor(ctx, tenantID, id)
	}
	return scanDoctor(s.pool.QueryRow(ctx, fmt.Sprintf(`
		UPDATE his_doctors SET %s, updated_at = NOW()
		WHERE tenant_id = $1 AND id = $2
		RETURNING `+doctorColumns, strings.Join(sets, ", ")), args...))
}

// ---------------------------------------------------------------------------
// 房间

const roomColumns = `id, name, type, status`

func scanRoom(row pgx.Row) (Room, error) {
	var r Room
	err := row.Scan(&r.ID, &r.Name, &r.Type, &r.Status)
	return r, err
}

// ListRooms 返回全部房间。
func (s *Store) ListRooms(ctx context.Context, tenantID int64) ([]Room, error) {
	rows, err := s.pool.Query(ctx, `SELECT `+roomColumns+` FROM his_rooms WHERE tenant_id = $1 ORDER BY id`, tenantID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	rooms := make([]Room, 0)
	for rows.Next() {
		r, err := scanRoom(rows)
		if err != nil {
			return nil, err
		}
		rooms = append(rooms, r)
	}
	return rooms, rows.Err()
}

// CreateRoom 新增房间。
func (s *Store) CreateRoom(ctx context.Context, tenantID int64, req CreateRoomRequest) (Room, error) {
	roomType := strings.TrimSpace(req.Type)
	if roomType == "" {
		roomType = "diagnose"
	}
	return scanRoom(s.pool.QueryRow(ctx, `
		INSERT INTO his_rooms (tenant_id, name, type) VALUES ($1, $2, $3)
		RETURNING `+roomColumns, tenantID, req.Name, roomType))
}

// UpdateRoom 部分更新房间。
func (s *Store) UpdateRoom(ctx context.Context, tenantID, id int64, req UpdateRoomRequest) (Room, error) {
	sets := []string{}
	args := []any{tenantID, id}
	add := func(column string, value any) {
		args = append(args, value)
		sets = append(sets, fmt.Sprintf("%s = COALESCE($%d, %s)", column, len(args), column))
	}
	if req.Name != nil {
		add("name", req.Name)
	}
	if req.Type != nil {
		add("type", req.Type)
	}
	if req.Status != nil {
		add("status", req.Status)
	}
	if len(sets) == 0 {
		return Room{}, pgx.ErrNoRows
	}
	return scanRoom(s.pool.QueryRow(ctx, fmt.Sprintf(`
		UPDATE his_rooms SET %s, updated_at = NOW()
		WHERE tenant_id = $1 AND id = $2
		RETURNING `+roomColumns, strings.Join(sets, ", ")), args...))
}

// ---------------------------------------------------------------------------
// 药品字典

const drugColumns = `id, code, name, spec, unit, manufacturer, drug_type, retail_price, purchase_price, is_rx, status`

func scanDrug(row pgx.Row) (Drug, error) {
	var d Drug
	err := row.Scan(&d.ID, &d.Code, &d.Name, &d.Spec, &d.Unit, &d.Manufacturer, &d.DrugType,
		&d.RetailPrice, &d.PurchasePrice, &d.IsRx, &d.Status)
	return d, err
}

// ListDrugs 按名称/编码模糊搜索药品（空关键字返回全部，按 id 升序）。
func (s *Store) ListDrugs(ctx context.Context, tenantID int64, keyword string) ([]Drug, error) {
	kw := "%" + strings.TrimSpace(keyword) + "%"
	rows, err := s.pool.Query(ctx, `
		SELECT `+drugColumns+` FROM his_drugs
		WHERE tenant_id = $1
		  AND ($2 = '%%' OR name ILIKE $2 OR code ILIKE $2)
		ORDER BY id`, tenantID, kw)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	drugs := make([]Drug, 0)
	for rows.Next() {
		d, err := scanDrug(rows)
		if err != nil {
			return nil, err
		}
		drugs = append(drugs, d)
	}
	return drugs, rows.Err()
}

// CreateDrug 新增药品。
func (s *Store) CreateDrug(ctx context.Context, tenantID int64, req CreateDrugRequest) (Drug, error) {
	isRx := true
	if req.IsRx != nil {
		isRx = *req.IsRx
	}
	drugType := strings.TrimSpace(req.DrugType)
	if drugType == "" {
		drugType = "western"
	}
	return scanDrug(s.pool.QueryRow(ctx, `
		INSERT INTO his_drugs (tenant_id, code, name, spec, unit, manufacturer, drug_type,
		                       retail_price, purchase_price, is_rx)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10)
		RETURNING `+drugColumns,
		tenantID, req.Code, req.Name, req.Spec, req.Unit, req.Manufacturer, drugType,
		req.RetailPrice, req.PurchasePrice, isRx))
}

// UpdateDrug 部分更新药品。
func (s *Store) UpdateDrug(ctx context.Context, tenantID, id int64, req UpdateDrugRequest) (Drug, error) {
	sets := []string{}
	args := []any{tenantID, id}
	add := func(column string, value any) {
		args = append(args, value)
		sets = append(sets, fmt.Sprintf("%s = COALESCE($%d, %s)", column, len(args), column))
	}
	if req.Spec != nil {
		add("spec", req.Spec)
	}
	if req.Unit != nil {
		add("unit", req.Unit)
	}
	if req.Manufacturer != nil {
		add("manufacturer", req.Manufacturer)
	}
	if req.DrugType != nil {
		add("drug_type", req.DrugType)
	}
	if req.RetailPrice != nil {
		add("retail_price", req.RetailPrice)
	}
	if req.PurchasePrice != nil {
		add("purchase_price", req.PurchasePrice)
	}
	if req.IsRx != nil {
		add("is_rx", req.IsRx)
	}
	if req.Status != nil {
		add("status", req.Status)
	}
	if len(sets) == 0 {
		return Drug{}, pgx.ErrNoRows
	}
	return scanDrug(s.pool.QueryRow(ctx, fmt.Sprintf(`
		UPDATE his_drugs SET %s, updated_at = NOW()
		WHERE tenant_id = $1 AND id = $2
		RETURNING `+drugColumns, strings.Join(sets, ", ")), args...))
}

// ---------------------------------------------------------------------------
// 原子收费项目

const chargeItemColumns = `id, code, name, category, unit, price, status`

func scanChargeItem(row pgx.Row) (ChargeItem, error) {
	var c ChargeItem
	err := row.Scan(&c.ID, &c.Code, &c.Name, &c.Category, &c.Unit, &c.Price, &c.Status)
	return c, err
}

// ListChargeItems 返回全部原子收费项目。
func (s *Store) ListChargeItems(ctx context.Context, tenantID int64) ([]ChargeItem, error) {
	rows, err := s.pool.Query(ctx, `SELECT `+chargeItemColumns+` FROM his_charge_items WHERE tenant_id = $1 ORDER BY id`, tenantID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	items := make([]ChargeItem, 0)
	for rows.Next() {
		c, err := scanChargeItem(rows)
		if err != nil {
			return nil, err
		}
		items = append(items, c)
	}
	return items, rows.Err()
}

// CreateChargeItem 新增原子收费项目。
func (s *Store) CreateChargeItem(ctx context.Context, tenantID int64, req CreateChargeItemRequest) (ChargeItem, error) {
	category := strings.TrimSpace(req.Category)
	if category == "" {
		category = "other"
	}
	unit := strings.TrimSpace(req.Unit)
	if unit == "" {
		unit = "次"
	}
	return scanChargeItem(s.pool.QueryRow(ctx, `
		INSERT INTO his_charge_items (tenant_id, code, name, category, unit, price)
		VALUES ($1, $2, $3, $4, $5, $6)
		RETURNING `+chargeItemColumns, tenantID, req.Code, req.Name, category, unit, req.Price))
}

// UpdateChargeItem 部分更新原子收费项目。
func (s *Store) UpdateChargeItem(ctx context.Context, tenantID, id int64, req UpdateChargeItemRequest) (ChargeItem, error) {
	sets := []string{}
	args := []any{tenantID, id}
	add := func(column string, value any) {
		args = append(args, value)
		sets = append(sets, fmt.Sprintf("%s = COALESCE($%d, %s)", column, len(args), column))
	}
	if req.Name != nil {
		add("name", req.Name)
	}
	if req.Category != nil {
		add("category", req.Category)
	}
	if req.Unit != nil {
		add("unit", req.Unit)
	}
	if req.Price != nil {
		add("price", req.Price)
	}
	if req.Status != nil {
		add("status", req.Status)
	}
	if len(sets) == 0 {
		return ChargeItem{}, pgx.ErrNoRows
	}
	return scanChargeItem(s.pool.QueryRow(ctx, fmt.Sprintf(`
		UPDATE his_charge_items SET %s, updated_at = NOW()
		WHERE tenant_id = $1 AND id = $2
		RETURNING `+chargeItemColumns, strings.Join(sets, ", ")), args...))
}

// ---------------------------------------------------------------------------
// 打包产品

const bundleColumns = `id, code, name, price, description, valid_from, valid_to, status`

func scanBundle(row pgx.Row) (Bundle, error) {
	var b Bundle
	var validFrom, validTo *string
	err := row.Scan(&b.ID, &b.Code, &b.Name, &b.Price, &b.Description, &validFrom, &validTo, &b.Status)
	if err != nil {
		return b, err
	}
	b.ValidFrom = validFrom
	b.ValidTo = validTo
	return b, nil
}

func (s *Store) loadBundleItems(ctx context.Context, bundleID int64) ([]BundleItem, error) {
	rows, err := s.pool.Query(ctx, `
		SELECT bi.charge_item_id, COALESCE(ci.name, ''), bi.quantity
		FROM his_bundle_items bi
		LEFT JOIN his_charge_items ci ON ci.id = bi.charge_item_id
		WHERE bi.bundle_id = $1
		ORDER BY bi.id`, bundleID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	items := make([]BundleItem, 0)
	for rows.Next() {
		var item BundleItem
		if err := rows.Scan(&item.ChargeItemID, &item.ChargeItemName, &item.Quantity); err != nil {
			return nil, err
		}
		items = append(items, item)
	}
	return items, rows.Err()
}

func (s *Store) bundleWithItems(ctx context.Context, b Bundle) (Bundle, error) {
	items, err := s.loadBundleItems(ctx, b.ID)
	if err != nil {
		return b, err
	}
	b.Items = items
	return b, nil
}

// ListBundles 返回全部打包产品（含组成）。
func (s *Store) ListBundles(ctx context.Context, tenantID int64) ([]Bundle, error) {
	rows, err := s.pool.Query(ctx, `SELECT `+bundleColumns+` FROM his_bundles WHERE tenant_id = $1 ORDER BY id`, tenantID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	bundles := make([]Bundle, 0)
	for rows.Next() {
		b, err := scanBundle(rows)
		if err != nil {
			return nil, err
		}
		b, err = s.bundleWithItems(ctx, b)
		if err != nil {
			return nil, err
		}
		bundles = append(bundles, b)
	}
	return bundles, rows.Err()
}

// GetBundle 返回单个打包产品。
func (s *Store) GetBundle(ctx context.Context, tenantID, id int64) (Bundle, error) {
	b, err := scanBundle(s.pool.QueryRow(ctx, `
		SELECT `+bundleColumns+` FROM his_bundles WHERE tenant_id = $1 AND id = $2`, tenantID, id))
	if err != nil {
		return b, err
	}
	return s.bundleWithItems(ctx, b)
}

// replaceBundleItems 全量替换打包组成（事务内调用）。
func (s *Store) replaceBundleItems(ctx context.Context, tx pgx.Tx, bundleID int64, items []BundleItem) error {
	if _, err := tx.Exec(ctx, `DELETE FROM his_bundle_items WHERE bundle_id = $1`, bundleID); err != nil {
		return err
	}
	for _, item := range items {
		if _, err := tx.Exec(ctx, `
			INSERT INTO his_bundle_items (bundle_id, charge_item_id, quantity)
			VALUES ($1, $2, $3)
			ON CONFLICT (bundle_id, charge_item_id) DO UPDATE SET quantity = EXCLUDED.quantity`,
			bundleID, item.ChargeItemID, item.Quantity); err != nil {
			return err
		}
	}
	return nil
}

// CreateBundle 新增打包产品（含组成，事务）。
func (s *Store) CreateBundle(ctx context.Context, tenantID int64, req CreateBundleRequest) (Bundle, error) {
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return Bundle{}, err
	}
	defer tx.Rollback(ctx)

	b, err := scanBundle(tx.QueryRow(ctx, `
		INSERT INTO his_bundles (tenant_id, code, name, price, description, valid_from, valid_to)
		VALUES ($1, $2, $3, $4, $5, $6, $7)
		RETURNING `+bundleColumns,
		tenantID, req.Code, req.Name, req.Price, req.Description, req.ValidFrom, req.ValidTo))
	if err != nil {
		return Bundle{}, err
	}
	if err := s.replaceBundleItems(ctx, tx, b.ID, req.Items); err != nil {
		return Bundle{}, err
	}
	if err := tx.Commit(ctx); err != nil {
		return Bundle{}, err
	}
	return s.bundleWithItems(ctx, b)
}

// UpdateBundle 部分更新打包产品；Items 非 nil 时事务内替换组成。
func (s *Store) UpdateBundle(ctx context.Context, tenantID, id int64, req UpdateBundleRequest) (Bundle, error) {
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return Bundle{}, err
	}
	defer tx.Rollback(ctx)

	sets := []string{}
	args := []any{tenantID, id}
	add := func(column string, value any) {
		args = append(args, value)
		sets = append(sets, fmt.Sprintf("%s = COALESCE($%d, %s)", column, len(args), column))
	}
	if req.Name != nil {
		add("name", req.Name)
	}
	if req.Price != nil {
		add("price", req.Price)
	}
	if req.Description != nil {
		add("description", req.Description)
	}
	if req.ValidFrom != nil {
		add("valid_from", req.ValidFrom)
	}
	if req.ValidTo != nil {
		add("valid_to", req.ValidTo)
	}
	if req.Status != nil {
		add("status", req.Status)
	}

	var b Bundle
	if len(sets) > 0 {
		b, err = scanBundle(tx.QueryRow(ctx, fmt.Sprintf(`
			UPDATE his_bundles SET %s, updated_at = NOW()
			WHERE tenant_id = $1 AND id = $2
			RETURNING `+bundleColumns, strings.Join(sets, ", ")), args...))
		if err != nil {
			return Bundle{}, err
		}
	} else {
		b, err = scanBundle(tx.QueryRow(ctx, `
			SELECT `+bundleColumns+` FROM his_bundles WHERE tenant_id = $1 AND id = $2`, tenantID, id))
		if err != nil {
			return Bundle{}, err
		}
	}

	if req.Items != nil {
		if err := s.replaceBundleItems(ctx, tx, b.ID, req.Items); err != nil {
			return Bundle{}, err
		}
	}
	if err := tx.Commit(ctx); err != nil {
		return Bundle{}, err
	}
	return s.bundleWithItems(ctx, b)
}

// ---------------------------------------------------------------------------
// 患者建档与病案号

// FindCustomerByPhone 手机号精确查重（含合并标记，P1 用精确匹配）。
func (s *Store) FindCustomerByPhone(ctx context.Context, tenantID int64, phone string) (int64, error) {
	var id int64
	err := s.pool.QueryRow(ctx, `
		SELECT id FROM customers WHERE tenant_id = $1 AND phone = $2
		ORDER BY id LIMIT 1`, tenantID, phone).Scan(&id)
	return id, err
}

// CreateCustomer 建档写入 customers 主档（HIS 来源；到院患者视为已转化）。
func (s *Store) CreateCustomer(ctx context.Context, tenantID, createdBy int64, req RegisterPatientRequest) (int64, error) {
	var id int64
	err := s.pool.QueryRow(ctx, `
		INSERT INTO customers (tenant_id, name, phone, gender, age, source, lifecycle_stage, status, momentum, extra_data, created_by, created_at, updated_at)
		VALUES ($1, $2, $3, $4, $5, 'his', 'patient', 'converted', 50, $6::jsonb, $7, NOW(), NOW())
		RETURNING id`,
		tenantID, req.Name, req.Phone, nullIfEmpty(req.Gender), req.Age,
		extraDataWithAddress(req.Address, req.Allergy), createdBy).Scan(&id)
	return id, err
}

func nullIfEmpty(value string) *string {
	if value == "" {
		return nil
	}
	return &value
}

func extraDataWithAddress(address, allergy string) string {
	fields := []string{}
	if address != "" {
		fields = append(fields, `"address":"`+strings.ReplaceAll(address, `"`, `\"`)+`"`)
	}
	if allergy != "" {
		fields = append(fields, `"allergy":"`+strings.ReplaceAll(allergy, `"`, `\"`)+`"`)
	}
	if len(fields) == 0 {
		return "{}"
	}
	return `{` + strings.Join(fields, ",") + `}`
}

// NextNumber 原子递增租户内编号流水（scope: mrn/rx/order/po），返回序列值。
func (s *Store) NextNumber(ctx context.Context, tenantID int64, scope string) (int64, error) {
	return nextNumber(ctx, s.pool, tenantID, scope)
}

// nextNumber 在指定连接/事务内递增编号流水，保证采购单号与建单同一事务提交。
func nextNumber(ctx context.Context, q querier, tenantID int64, scope string) (int64, error) {
	var seq int64
	err := q.QueryRow(ctx, `
		INSERT INTO his_number_counters (tenant_id, scope, last_seq, updated_at)
		VALUES ($1, $2, 1, NOW())
		ON CONFLICT (tenant_id, scope) DO UPDATE
		SET last_seq = his_number_counters.last_seq + 1, updated_at = NOW()
		RETURNING last_seq`, tenantID, scope).Scan(&seq)
	if err != nil {
		return 0, err
	}
	return seq, nil
}

// IssueMRN 发放病案号（一人一号、永不复用）。
func (s *Store) IssueMRN(ctx context.Context, tenantID int64) (string, error) {
	seq, err := s.NextNumber(ctx, tenantID, "mrn")
	if err != nil {
		return "", err
	}
	return mrnFormat(seq), nil
}

// EnsureMRN 若患者尚无病案号则发放，返回现有或新发的病案号。
func (s *Store) EnsureMRN(ctx context.Context, tenantID, customerID int64) (string, error) {
	var existing string
	err := s.pool.QueryRow(ctx, `
		SELECT value FROM patient_identifiers
		WHERE tenant_id = $1 AND customer_id = $2 AND identifier_type = 'mrn' AND status = 'active'
		ORDER BY id LIMIT 1`, tenantID, customerID).Scan(&existing)
	if err == nil {
		return existing, nil
	}
	if err != pgx.ErrNoRows {
		return "", err
	}
	mrn, err := s.IssueMRN(ctx, tenantID)
	if err != nil {
		return "", err
	}
	if _, err := s.pool.Exec(ctx, `
		INSERT INTO patient_identifiers (tenant_id, customer_id, identifier_type, value, issued_by)
		VALUES ($1, $2, 'mrn', $3, 'his')`, tenantID, customerID, mrn); err != nil {
		return "", err
	}
	return mrn, nil
}

// ListPatientIdentifiers 返回患者的全部标识（病案号/外部 HIS 号）。
func (s *Store) ListPatientIdentifiers(ctx context.Context, tenantID, customerID int64) ([]PatientIdentifier, error) {
	rows, err := s.pool.Query(ctx, `
		SELECT id, customer_id, identifier_type, value, issued_by, issued_at, status
		FROM patient_identifiers
		WHERE tenant_id = $1 AND customer_id = $2
		ORDER BY id`, tenantID, customerID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	ids := make([]PatientIdentifier, 0)
	for rows.Next() {
		var item PatientIdentifier
		if err := rows.Scan(&item.ID, &item.CustomerID, &item.IdentifierType, &item.Value,
			&item.IssuedBy, &item.IssuedAt, &item.Status); err != nil {
			return nil, err
		}
		ids = append(ids, item)
	}
	return ids, rows.Err()
}

// ---------------------------------------------------------------------------
// 排班与号源

const scheduleColumns = `s.id, s.doctor_id,
	(SELECT COALESCE(e.full_name, e.name, e.username, '') FROM his_doctors d
	  JOIN employees e ON e.id = d.employee_id WHERE d.id = s.doctor_id) AS doctor_name,
	s.department_id, s.room_id, s.work_date::text,
	s.period, to_char(s.start_time, 'HH24:MI'), to_char(s.end_time, 'HH24:MI'),
	s.slot_interval_minutes, s.status, s.remark`

const scheduleCountColumns = `COUNT(sl.id) AS total_slots,
	COUNT(sl.id) FILTER (WHERE sl.status = 'free') AS free_slots,
	COUNT(sl.id) FILTER (WHERE sl.status = 'reserved') AS reserved_slots,
	COUNT(sl.id) FILTER (WHERE sl.status = 'registered') AS registered_slots`

const scheduleFromClause = `FROM his_schedules s
	LEFT JOIN his_slots sl ON sl.schedule_id = s.id`

type scheduleQuery struct {
	DateFrom     string
	DateTo       string
	DoctorID     int64
	DepartmentID int64
}

func scanSchedule(row pgx.Row) (Schedule, error) {
	var sched Schedule
	err := row.Scan(&sched.ID, &sched.DoctorID, &sched.DoctorName, &sched.DepartmentID, &sched.RoomID, &sched.WorkDate,
		&sched.Period, &sched.StartTime, &sched.EndTime, &sched.SlotIntervalMinutes, &sched.Status, &sched.Remark,
		&sched.TotalSlots, &sched.FreeSlots, &sched.ReservedSlots, &sched.RegisteredSlots)
	return sched, err
}

// ListSchedules 按日期区间/医生/科室查询排班（含号源统计）。
func (s *Store) ListSchedules(ctx context.Context, tenantID int64, q scheduleQuery) ([]Schedule, error) {
	rows, err := s.pool.Query(ctx, `
		SELECT `+scheduleColumns+`, `+scheduleCountColumns+`
		`+scheduleFromClause+`
		WHERE s.tenant_id = $1
		  AND ($2 = '' OR s.work_date >= $2::date)
		  AND ($3 = '' OR s.work_date <= $3::date)
		  AND ($4 = 0 OR s.doctor_id = $4)
		  AND ($5 = 0 OR s.department_id = $5)
		GROUP BY s.id
		ORDER BY s.work_date, s.period, s.id`,
		tenantID, q.DateFrom, q.DateTo, q.DoctorID, q.DepartmentID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	schedules := make([]Schedule, 0)
	for rows.Next() {
		sched, err := scanSchedule(rows)
		if err != nil {
			return nil, err
		}
		schedules = append(schedules, sched)
	}
	return schedules, rows.Err()
}

// GetSchedule 返回单个排班（含号源统计）。
func (s *Store) GetSchedule(ctx context.Context, tenantID, id int64) (Schedule, error) {
	return scanSchedule(s.pool.QueryRow(ctx, `
		SELECT `+scheduleColumns+`, `+scheduleCountColumns+`
		`+scheduleFromClause+`
		WHERE s.tenant_id = $1 AND s.id = $2
		GROUP BY s.id`, tenantID, id))
}

// ListSlots 返回排班下的全部号源。
func (s *Store) ListSlots(ctx context.Context, tenantID, scheduleID int64) ([]Slot, error) {
	rows, err := s.pool.Query(ctx, `
		SELECT sl.id, sl.schedule_id, sl.seq_no, to_char(sl.time_start, 'HH24:MI'),
		       to_char(sl.time_end, 'HH24:MI'), sl.status, sl.registration_id
		FROM his_slots sl
		JOIN his_schedules sc ON sc.id = sl.schedule_id AND sc.tenant_id = $1
		WHERE sl.schedule_id = $2
		ORDER BY sl.seq_no`, tenantID, scheduleID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	slots := make([]Slot, 0)
	for rows.Next() {
		var slot Slot
		if err := rows.Scan(&slot.ID, &slot.ScheduleID, &slot.SeqNo, &slot.TimeStart, &slot.TimeEnd,
			&slot.Status, &slot.RegistrationID); err != nil {
			return nil, err
		}
		slots = append(slots, slot)
	}
	return slots, rows.Err()
}

// slotTimes 生成号源时间区间（[start, end) 按间隔切分，返回 HH:MM 字符串对）。
func slotTimes(startTime, endTime time.Time, intervalMinutes int) [][2]string {
	interval := time.Duration(intervalMinutes) * time.Minute
	times := make([][2]string, 0)
	for t := startTime; t.Before(endTime); t = t.Add(interval) {
		next := t.Add(interval)
		if next.After(endTime) {
			next = endTime
		}
		times = append(times, [2]string{t.Format("15:04"), next.Format("15:04")})
	}
	return times
}

func (s *Store) regenerateSlots(ctx context.Context, tx pgx.Tx, scheduleID int64, startTime, endTime string, intervalMinutes int) error {
	if _, err := tx.Exec(ctx, `DELETE FROM his_slots WHERE schedule_id = $1`, scheduleID); err != nil {
		return err
	}
	start, err := time.Parse("15:04", startTime)
	if err != nil {
		return err
	}
	end, err := time.Parse("15:04", endTime)
	if err != nil {
		return err
	}
	for seq, pair := range slotTimes(start, end, intervalMinutes) {
		if _, err := tx.Exec(ctx, `
			INSERT INTO his_slots (schedule_id, seq_no, time_start, time_end)
			VALUES ($1, $2, $3::time, $4::time)`, scheduleID, seq+1, pair[0], pair[1]); err != nil {
			return err
		}
	}
	return nil
}

// countNonFreeSlots 统计非空闲号源数（已约/已挂号），用于时间变更保护。
func (s *Store) countNonFreeSlots(ctx context.Context, tx pgx.Tx, scheduleID int64) (int64, error) {
	var count int64
	err := tx.QueryRow(ctx, `
		SELECT COUNT(*) FROM his_slots
		WHERE schedule_id = $1 AND status IN ('reserved','registered')`, scheduleID).Scan(&count)
	return count, err
}

// CreateSchedule 新增排班并生成号源（事务）。
func (s *Store) CreateSchedule(ctx context.Context, tenantID int64, req CreateScheduleRequest) (Schedule, error) {
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return Schedule{}, err
	}
	defer tx.Rollback(ctx)

	period := req.Period
	if period == "" {
		period = "morning"
	}
	var id int64
	err = tx.QueryRow(ctx, `
		INSERT INTO his_schedules (tenant_id, doctor_id, department_id, room_id, work_date,
		                           period, start_time, end_time, slot_interval_minutes, remark)
		VALUES ($1, $2, $3, $4, $5::date, $6, $7::time, $8::time, $9, $10)
		RETURNING id`,
		tenantID, req.DoctorID, req.DepartmentID, req.RoomID, req.WorkDate,
		period, req.StartTime, req.EndTime, req.SlotIntervalMinutes, req.Remark).Scan(&id)
	if err != nil {
		return Schedule{}, err
	}
	if err := s.regenerateSlots(ctx, tx, id, req.StartTime, req.EndTime, req.SlotIntervalMinutes); err != nil {
		return Schedule{}, err
	}
	if err := tx.Commit(ctx); err != nil {
		return Schedule{}, err
	}
	return s.GetSchedule(ctx, tenantID, id)
}

// UpdateSchedule 部分更新排班；时间/间隔变化受已约号源保护；停诊禁用空闲号源。
func (s *Store) UpdateSchedule(ctx context.Context, tenantID, id int64, req UpdateScheduleRequest) (Schedule, error) {
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return Schedule{}, err
	}
	defer tx.Rollback(ctx)

	// 先锁定排班行（FOR UPDATE 不能与 GROUP BY 并用，锁与统计分开执行）。
	var lockedID int64
	if err := tx.QueryRow(ctx, `
		SELECT id FROM his_schedules WHERE tenant_id = $1 AND id = $2
		FOR UPDATE`, tenantID, id).Scan(&lockedID); err != nil {
		return Schedule{}, err
	}
	current, err := scanSchedule(tx.QueryRow(ctx, `
		SELECT `+scheduleColumns+`, `+scheduleCountColumns+`
		`+scheduleFromClause+`
		WHERE s.tenant_id = $1 AND s.id = $2
		GROUP BY s.id`, tenantID, id))
	if err != nil {
		return Schedule{}, err
	}

	startTime, endTime := current.StartTime, current.EndTime
	interval := current.SlotIntervalMinutes
	timeChanged := false

	if req.StartTime != nil {
		startTime = *req.StartTime
		timeChanged = true
	}
	if req.EndTime != nil {
		endTime = *req.EndTime
		timeChanged = true
	}
	if req.SlotIntervalMinutes != nil {
		interval = *req.SlotIntervalMinutes
		timeChanged = true
	}

	if timeChanged {
		nonFree, err := s.countNonFreeSlots(ctx, tx, id)
		if err != nil {
			return Schedule{}, err
		}
		if nonFree > 0 {
			return Schedule{}, ErrScheduleHasBookings
		}
	}

	sets := []string{}
	args := []any{tenantID, id}
	add := func(column string, value any) {
		args = append(args, value)
		sets = append(sets, fmt.Sprintf("%s = COALESCE($%d, %s)", column, len(args), column))
	}
	if req.StartTime != nil {
		add("start_time", req.StartTime)
	}
	if req.EndTime != nil {
		add("end_time", req.EndTime)
	}
	if req.SlotIntervalMinutes != nil {
		add("slot_interval_minutes", req.SlotIntervalMinutes)
	}
	if req.Status != nil {
		add("status", req.Status)
	}
	if req.Remark != nil {
		add("remark", req.Remark)
	}
	if len(sets) > 0 {
		if _, err := tx.Exec(ctx, fmt.Sprintf(`
			UPDATE his_schedules SET %s, updated_at = NOW()
			WHERE tenant_id = $1 AND id = $2`, strings.Join(sets, ", ")), args...); err != nil {
			return Schedule{}, err
		}
	}

	if timeChanged {
		if err := s.regenerateSlots(ctx, tx, id, startTime, endTime, interval); err != nil {
			return Schedule{}, err
		}
	}
	if req.Status != nil {
		target := *req.Status
		if target == "suspended" {
			if _, err := tx.Exec(ctx, `
				UPDATE his_slots SET status = 'disabled'
				WHERE schedule_id = $1 AND status = 'free'`, id); err != nil {
				return Schedule{}, err
			}
		} else if target == "open" {
			if _, err := tx.Exec(ctx, `
				UPDATE his_slots SET status = 'free'
				WHERE schedule_id = $1 AND status = 'disabled'`, id); err != nil {
				return Schedule{}, err
			}
		}
	}

	if err := tx.Commit(ctx); err != nil {
		return Schedule{}, err
	}
	return s.GetSchedule(ctx, tenantID, id)
}

// ---------------------------------------------------------------------------
// 挂号与分诊队列

const registrationColumns = `r.id, r.customer_id, COALESCE(c.name, ''), COALESCE(c.phone, ''),
	COALESCE(pi.value, ''), r.department_id, r.doctor_id, COALESCE(e.full_name, e.name, e.username, ''),
	d.employee_id, r.schedule_id, r.slot_id,
	COALESCE(to_char(sl.time_start, 'HH24:MI'), ''), COALESCE(to_char(sl.time_end, 'HH24:MI'), ''),
	r.reg_type, r.source, r.visit_date::text, r.fee, r.status, en.id, r.created_by, r.created_at`

const registrationFromClause = `FROM his_registrations r
	JOIN customers c ON c.id = r.customer_id
	LEFT JOIN patient_identifiers pi ON pi.customer_id = r.customer_id
		AND pi.identifier_type = 'mrn' AND pi.status = 'active'
	LEFT JOIN his_slots sl ON sl.id = r.slot_id
	LEFT JOIN his_doctors d ON d.id = r.doctor_id
	LEFT JOIN employees e ON e.id = d.employee_id
	LEFT JOIN encounters en ON en.tenant_id = r.tenant_id
		AND en.source_type = 'his' AND en.source_id = r.id`

func scanRegistration(row pgx.Row) (Registration, error) {
	var reg Registration
	err := row.Scan(&reg.ID, &reg.CustomerID, &reg.CustomerName, &reg.Phone, &reg.MRN,
		&reg.DepartmentID, &reg.DoctorID, &reg.DoctorName, &reg.DoctorEmployeeID, &reg.ScheduleID, &reg.SlotID,
		&reg.SlotTimeStart, &reg.SlotTimeEnd, &reg.RegType, &reg.Source, &reg.VisitDate,
		&reg.Fee, &reg.Status, &reg.EncounterID, &reg.CreatedBy, &reg.CreatedAt)
	return reg, err
}

func (s *Store) loadRegistration(ctx context.Context, q querier, tenantID, id int64) (Registration, error) {
	return scanRegistration(q.QueryRow(ctx, `
		SELECT `+registrationColumns+`
		`+registrationFromClause+`
		WHERE r.tenant_id = $1 AND r.id = $2`, tenantID, id))
}

// ListRegistrations 挂号台列表（?date=&status=&department_id=&doctor_id=）。
func (s *Store) ListRegistrations(ctx context.Context, tenantID int64, date, status string, doctorID, departmentID int64) ([]Registration, error) {
	rows, err := s.pool.Query(ctx, `
		SELECT `+registrationColumns+`
		`+registrationFromClause+`
		WHERE r.tenant_id = $1
		  AND ($2 = '' OR r.visit_date = $2::date)
		  AND ($3 = '' OR r.status = $3)
		  AND ($4 = 0 OR r.doctor_id = $4)
		  AND ($5 = 0 OR r.department_id = $5)
		ORDER BY r.id DESC
		LIMIT 200`, tenantID, date, status, doctorID, departmentID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	items := make([]Registration, 0)
	for rows.Next() {
		reg, err := scanRegistration(rows)
		if err != nil {
			return nil, err
		}
		items = append(items, reg)
	}
	return items, rows.Err()
}

// ListQueue 分诊队列（?date=&doctor_id=&status=）。
func (s *Store) ListQueue(ctx context.Context, tenantID int64, date string, doctorID int64, status string) ([]QueueEntry, error) {
	rows, err := s.pool.Query(ctx, `
		SELECT q.id, q.encounter_id, q.registration_id, r.customer_id, COALESCE(c.name, ''),
		       COALESCE(c.phone, ''), COALESCE(c.gender, ''), COALESCE(c.age, 0), q.seq_no, q.status, q.called_at,
		       r.doctor_id, COALESCE(e.full_name, e.name, e.username, '')
		FROM his_queue q
		JOIN his_registrations r ON r.id = q.registration_id
		JOIN customers c ON c.id = r.customer_id
		LEFT JOIN his_doctors d ON d.id = r.doctor_id
		LEFT JOIN employees e ON e.id = d.employee_id
		WHERE q.tenant_id = $1
		  AND ($2 = '' OR r.visit_date = $2::date)
		  AND ($3 = 0 OR r.doctor_id = $3)
		  AND ($4 = '' OR q.status = $4)
		ORDER BY q.seq_no`, tenantID, date, doctorID, status)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	items := make([]QueueEntry, 0)
	for rows.Next() {
		var q QueueEntry
		if err := rows.Scan(&q.ID, &q.EncounterID, &q.RegistrationID, &q.CustomerID, &q.CustomerName,
			&q.Phone, &q.Gender, &q.Age, &q.SeqNo, &q.Status, &q.CalledAt, &q.DoctorID, &q.DoctorName); err != nil {
			return nil, err
		}
		items = append(items, q)
	}
	return items, rows.Err()
}

// acquireSlot 在事务内抢占号源（原子条件更新，避免并发重复锁号）。
func acquireSlot(ctx context.Context, tx pgx.Tx, slotID *int64, scheduleID int64, targetStatus string) (int64, string, string, error) {
	if slotID != nil {
		var gotID int64
		var startTime, endTime string
		err := tx.QueryRow(ctx, `
			UPDATE his_slots
			SET status = $3, updated_at = NOW()
			WHERE id = $1 AND schedule_id = $2 AND status = 'free'
			RETURNING id, to_char(time_start, 'HH24:MI'), to_char(time_end, 'HH24:MI')`,
			*slotID, scheduleID, targetStatus).Scan(&gotID, &startTime, &endTime)
		if err != nil {
			return 0, "", "", err
		}
		return gotID, startTime, endTime, nil
	}
	var id int64
	var startTime, endTime string
	err := tx.QueryRow(ctx, `
		SELECT id, to_char(time_start, 'HH24:MI'), to_char(time_end, 'HH24:MI')
		FROM his_slots
		WHERE schedule_id = $1 AND status = 'free'
		ORDER BY seq_no
		LIMIT 1
		FOR UPDATE SKIP LOCKED`, scheduleID).Scan(&id, &startTime, &endTime)
	if err != nil {
		return 0, "", "", err
	}
	if _, err := tx.Exec(ctx, `
		UPDATE his_slots SET status = $2, updated_at = NOW()
		WHERE id = $1 AND status = 'free'`, id, targetStatus); err != nil {
		return 0, "", "", err
	}
	return id, startTime, endTime, nil
}

// ensureEncounterForRegistration 挂号确认后生产共享 encounters 事实（source_type='his'）。
// visit_type 沿用共享域取值：该患者已有 HIS 就诊 → return_visit，否则 first_visit。
func ensureEncounterForRegistration(ctx context.Context, tx pgx.Tx, tenantID int64, reg Registration) (int64, error) {
	visitType := "first_visit"
	var priorCount int
	if err := tx.QueryRow(ctx, `
		SELECT COUNT(*) FROM encounters
		WHERE tenant_id = $1 AND patient_id = $2 AND source_type = 'his' AND id <> ALL (
			SELECT id FROM encounters WHERE source_type = 'his' AND source_id = $3
		)`, tenantID, reg.CustomerID, reg.ID).Scan(&priorCount); err == nil && priorCount > 0 {
		visitType = "return_visit"
	}
	var encounterID int64
	err := tx.QueryRow(ctx, `
		INSERT INTO encounters (
			tenant_id, source_type, source_id, patient_id, patient_name, patient_candidate_json,
			provider_id, provider_name, provider_role, department_id, channel, visit_type,
			started_at, status, created_at, updated_at
		)
		VALUES ($1, 'his', $2, $3, NULLIF($4, ''), '{}'::jsonb, $5, NULLIF($6, ''), 'doctor', $7, 'store', $8, NOW(), 'new', NOW(), NOW())
		ON CONFLICT (tenant_id, source_type, source_id) DO UPDATE
		SET patient_id = EXCLUDED.patient_id,
		    patient_name = EXCLUDED.patient_name,
		    provider_id = EXCLUDED.provider_id,
		    provider_name = EXCLUDED.provider_name,
		    department_id = EXCLUDED.department_id,
		    updated_at = NOW()
		RETURNING id`,
		tenantID, reg.ID, reg.CustomerID, reg.CustomerName, reg.DoctorEmployeeID, reg.DoctorName,
		reg.DepartmentID, visitType).Scan(&encounterID)
	return encounterID, err
}

// nextQueueSeq 计算排班下下一个分诊序号。
func nextQueueSeq(ctx context.Context, tx pgx.Tx, tenantID int64, scheduleID *int64) (int, error) {
	var seq int
	err := tx.QueryRow(ctx, `
		SELECT COALESCE(MAX(q.seq_no), 0) + 1
		FROM his_queue q
		JOIN his_registrations r ON r.id = q.registration_id
		WHERE r.tenant_id = $1 AND ($2::bigint IS NULL OR r.schedule_id = $2)`,
		tenantID, scheduleID).Scan(&seq)
	return seq, err
}

// CreateRegistration 创建挂号/预约：锁号 → 写挂号单 →（现场）生产 encounter + 入队。
func (s *Store) CreateRegistration(ctx context.Context, tenantID, createdBy int64, req CreateRegistrationRequest) (Registration, error) {
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return Registration{}, err
	}
	defer tx.Rollback(ctx)

	var doctorID, departmentID *int64
	var workDate string
	var scheduleStatus string
	err = tx.QueryRow(ctx, `
		SELECT doctor_id, department_id, work_date::text, status
		FROM his_schedules WHERE tenant_id = $1 AND id = $2
		FOR UPDATE`, tenantID, req.ScheduleID).Scan(&doctorID, &departmentID, &workDate, &scheduleStatus)
	if err != nil {
		if err == pgx.ErrNoRows {
			return Registration{}, ErrNotFound
		}
		return Registration{}, err
	}
	if scheduleStatus != "open" {
		return Registration{}, ErrScheduleNotOpen
	}

	regStatus := "registered"
	if req.RegType == "appointment" {
		regStatus = "reserved"
	}

	// 先写挂号单（同事务内回滚安全），再抢号源并回填 slot_id。
	var regID int64
	err = tx.QueryRow(ctx, `
		INSERT INTO his_registrations (tenant_id, customer_id, department_id, doctor_id, schedule_id,
		                               reg_type, source, visit_date, fee, status, created_by)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8::date, $9, $10, $11)
		RETURNING id`,
		tenantID, req.CustomerID, departmentID, doctorID, req.ScheduleID,
		req.RegType, req.Source, workDate, req.Fee, regStatus, createdBy).Scan(&regID)
	if err != nil {
		return Registration{}, err
	}

	slotID, _, _, err := acquireSlot(ctx, tx, req.SlotID, req.ScheduleID, regStatus)
	if err != nil {
		if err == pgx.ErrNoRows {
			return Registration{}, ErrSlotUnavailable
		}
		return Registration{}, err
	}
	if _, err := tx.Exec(ctx, `
		UPDATE his_registrations SET slot_id = $2, updated_at = NOW() WHERE id = $1`, regID, slotID); err != nil {
		return Registration{}, err
	}
	if _, err := tx.Exec(ctx, `
		UPDATE his_slots SET registration_id = $2 WHERE id = $1`, slotID, regID); err != nil {
		return Registration{}, err
	}

	if regStatus == "registered" {
		if err := s.activateRegistration(ctx, tx, tenantID, regID); err != nil {
			return Registration{}, err
		}
	}

	if err := tx.Commit(ctx); err != nil {
		return Registration{}, err
	}
	return s.loadRegistration(ctx, s.pool, tenantID, regID)
}

// activateRegistration 挂号生效：生产 encounter 并进入分诊队列（事务内调用）。
func (s *Store) activateRegistration(ctx context.Context, tx pgx.Tx, tenantID, regID int64) error {
	reg, err := s.loadRegistration(ctx, tx, tenantID, regID)
	if err != nil {
		return err
	}
	encounterID, err := ensureEncounterForRegistration(ctx, tx, tenantID, reg)
	if err != nil {
		return err
	}
	seq, err := nextQueueSeq(ctx, tx, tenantID, reg.ScheduleID)
	if err != nil {
		return err
	}
	_, err = tx.Exec(ctx, `
		INSERT INTO his_queue (tenant_id, encounter_id, registration_id, seq_no)
		VALUES ($1, $2, $3, $4)`, tenantID, encounterID, regID, seq)
	return err
}

// ConfirmRegistration 预约到院确认：号源 reserved→registered，生产 encounter 并入队。
func (s *Store) ConfirmRegistration(ctx context.Context, tenantID, id int64) (Registration, error) {
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return Registration{}, err
	}
	defer tx.Rollback(ctx)

	var status string
	err = tx.QueryRow(ctx, `
		SELECT status FROM his_registrations WHERE tenant_id = $1 AND id = $2
		FOR UPDATE`, tenantID, id).Scan(&status)
	if err != nil {
		if err == pgx.ErrNoRows {
			return Registration{}, ErrNotFound
		}
		return Registration{}, err
	}
	if status != "reserved" {
		return Registration{}, ErrInvalidState
	}
	if _, err := tx.Exec(ctx, `
		UPDATE his_registrations SET status = 'registered', updated_at = NOW()
		WHERE id = $1`, id); err != nil {
		return Registration{}, err
	}
	if _, err := tx.Exec(ctx, `
		UPDATE his_slots SET status = 'registered'
		WHERE registration_id = $1 AND status = 'reserved'`, id); err != nil {
		return Registration{}, err
	}
	if err := s.activateRegistration(ctx, tx, tenantID, id); err != nil {
		return Registration{}, err
	}
	var customerID int64
	_ = tx.QueryRow(ctx, `SELECT customer_id FROM his_registrations WHERE id = $1`, id).Scan(&customerID)
	if err := tx.Commit(ctx); err != nil {
		return Registration{}, err
	}
	// 到院确认 = 首诊建档，此时发放病案号（预约阶段不发放；提交后补发，失败可由前台建档兜底）。
	if customerID > 0 {
		if _, err := s.EnsureMRN(ctx, tenantID, customerID); err != nil {
			return Registration{}, err
		}
	}
	return s.loadRegistration(ctx, s.pool, tenantID, id)
}

// RefundRegistration 退号：释放号源、encounter 取消、移出分诊队列。
func (s *Store) RefundRegistration(ctx context.Context, tenantID, id int64) (Registration, error) {
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return Registration{}, err
	}
	defer tx.Rollback(ctx)

	var status string
	err = tx.QueryRow(ctx, `
		SELECT status FROM his_registrations WHERE tenant_id = $1 AND id = $2
		FOR UPDATE`, tenantID, id).Scan(&status)
	if err != nil {
		if err == pgx.ErrNoRows {
			return Registration{}, ErrNotFound
		}
		return Registration{}, err
	}
	if status != "reserved" && status != "registered" {
		return Registration{}, ErrInvalidState
	}
	if _, err := tx.Exec(ctx, `
		UPDATE his_registrations SET status = 'refunded', updated_at = NOW()
		WHERE id = $1`, id); err != nil {
		return Registration{}, err
	}
	if _, err := tx.Exec(ctx, `
		UPDATE his_slots SET status = 'free', registration_id = NULL
		WHERE registration_id = $1`, id); err != nil {
		return Registration{}, err
	}
	if _, err := tx.Exec(ctx, `
		UPDATE encounters SET status = 'cancelled', updated_at = NOW()
		WHERE source_type = 'his' AND source_id = $1 AND status <> 'completed'`, id); err != nil {
		return Registration{}, err
	}
	if _, err := tx.Exec(ctx, `DELETE FROM his_queue WHERE registration_id = $1`, id); err != nil {
		return Registration{}, err
	}
	if err := tx.Commit(ctx); err != nil {
		return Registration{}, err
	}
	return s.loadRegistration(ctx, s.pool, tenantID, id)
}

// NoShowRegistration 爽约：释放号源，挂号单标记 no_show。
func (s *Store) NoShowRegistration(ctx context.Context, tenantID, id int64) (Registration, error) {
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return Registration{}, err
	}
	defer tx.Rollback(ctx)

	var status string
	err = tx.QueryRow(ctx, `
		SELECT status FROM his_registrations WHERE tenant_id = $1 AND id = $2
		FOR UPDATE`, tenantID, id).Scan(&status)
	if err != nil {
		if err == pgx.ErrNoRows {
			return Registration{}, ErrNotFound
		}
		return Registration{}, err
	}
	if status != "reserved" {
		return Registration{}, ErrInvalidState
	}
	if _, err := tx.Exec(ctx, `
		UPDATE his_registrations SET status = 'no_show', updated_at = NOW()
		WHERE id = $1`, id); err != nil {
		return Registration{}, err
	}
	if _, err := tx.Exec(ctx, `
		UPDATE his_slots SET status = 'free', registration_id = NULL
		WHERE registration_id = $1`, id); err != nil {
		return Registration{}, err
	}
	if err := tx.Commit(ctx); err != nil {
		return Registration{}, err
	}
	return s.loadRegistration(ctx, s.pool, tenantID, id)
}

// StartQueueItem 接诊开始。
func (s *Store) StartQueueItem(ctx context.Context, tenantID, registrationID int64) (QueueEntry, error) {
	tag, err := s.pool.Exec(ctx, `
		UPDATE his_queue SET status = 'in_progress', called_at = NOW()
		WHERE tenant_id = $1 AND registration_id = $2 AND status = 'waiting'`,
		tenantID, registrationID)
	if err != nil {
		return QueueEntry{}, err
	}
	if tag.RowsAffected() == 0 {
		return QueueEntry{}, ErrInvalidState
	}
	if _, err := s.pool.Exec(ctx, `
		UPDATE encounters SET status = 'in_progress', updated_at = NOW()
		WHERE source_type = 'his' AND source_id = $1`, registrationID); err != nil {
		return QueueEntry{}, err
	}
	return s.queueItemByRegistration(ctx, tenantID, registrationID)
}

// CompleteQueueItem 接诊完成。
func (s *Store) CompleteQueueItem(ctx context.Context, tenantID, registrationID int64) (QueueEntry, error) {
	tag, err := s.pool.Exec(ctx, `
		UPDATE his_queue SET status = 'done'
		WHERE tenant_id = $1 AND registration_id = $2 AND status = 'in_progress'`,
		tenantID, registrationID)
	if err != nil {
		return QueueEntry{}, err
	}
	if tag.RowsAffected() == 0 {
		return QueueEntry{}, ErrInvalidState
	}
	if _, err := s.pool.Exec(ctx, `
		UPDATE his_registrations SET status = 'visited', updated_at = NOW()
		WHERE id = $1`, registrationID); err != nil {
		return QueueEntry{}, err
	}
	if _, err := s.pool.Exec(ctx, `
		UPDATE encounters SET status = 'completed', ended_at = NOW(), updated_at = NOW()
		WHERE source_type = 'his' AND source_id = $1`, registrationID); err != nil {
		return QueueEntry{}, err
	}
	return s.queueItemByRegistration(ctx, tenantID, registrationID)
}

func (s *Store) queueItemByRegistration(ctx context.Context, tenantID, registrationID int64) (QueueEntry, error) {
	var q QueueEntry
	err := s.pool.QueryRow(ctx, `
		SELECT q.id, q.encounter_id, q.registration_id, r.customer_id, COALESCE(c.name, ''),
		       COALESCE(c.phone, ''), COALESCE(c.gender, ''), COALESCE(c.age, 0), q.seq_no, q.status, q.called_at,
		       r.doctor_id, COALESCE(e.full_name, e.name, e.username, '')
		FROM his_queue q
		JOIN his_registrations r ON r.id = q.registration_id
		JOIN customers c ON c.id = r.customer_id
		LEFT JOIN his_doctors d ON d.id = r.doctor_id
		LEFT JOIN employees e ON e.id = d.employee_id
		WHERE q.tenant_id = $1 AND q.registration_id = $2`, tenantID, registrationID).
		Scan(&q.ID, &q.EncounterID, &q.RegistrationID, &q.CustomerID, &q.CustomerName,
			&q.Phone, &q.Gender, &q.Age, &q.SeqNo, &q.Status, &q.CalledAt, &q.DoctorID, &q.DoctorName)
	if err != nil {
		return QueueEntry{}, err
	}
	return q, nil
}

// ---------------------------------------------------------------------------
// 医生工作站：病历（复用 emr 域）、处方、检查申请、治疗医嘱

// ListPublishedTemplates 返回已发布当前病历模板（租户模板 + 全局模板）。
func (s *Store) ListPublishedTemplates(ctx context.Context, tenantID int64, visitType string, specialty string) ([]EMRTemplate, error) {
	rows, err := s.pool.Query(ctx, `
		SELECT id::text, code, name, COALESCE(version_no, ''), document_type, visit_type,
		       COALESCE(specialty, ''), department_id
		FROM emr_templates
		WHERE status = 'published' AND is_current
		  AND tenant_id IN (0, $1)
		  AND ($2 = '' OR visit_type IN ($2, '通用'))
		  AND ($3 = '' OR specialty = $3)
		ORDER BY tenant_id DESC, code`, tenantID, visitType, specialty)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	items := make([]EMRTemplate, 0)
	for rows.Next() {
		var item EMRTemplate
		if err := rows.Scan(&item.ID, &item.Code, &item.Name, &item.VersionNo, &item.DocumentType,
			&item.VisitType, &item.Specialty, &item.DepartmentID); err != nil {
			return nil, err
		}
		items = append(items, item)
	}
	return items, rows.Err()
}

// FindEMRRecordByEncounter 查询就诊已有病历。
func (s *Store) FindEMRRecordByEncounter(ctx context.Context, tenantID, encounterID int64) (*string, error) {
	var id string
	err := s.pool.QueryRow(ctx, `
		SELECT id::text FROM emr_records
		WHERE tenant_id = $1 AND encounter_id = $2
		ORDER BY created_at DESC LIMIT 1`, tenantID, encounterID).Scan(&id)
	if err != nil {
		if err == pgx.ErrNoRows {
			return nil, nil
		}
		return nil, err
	}
	return &id, nil
}

// GetEMRRecordSummary 病历摘要（含内容与快照）。
func (s *Store) GetEMRRecordSummary(ctx context.Context, tenantID int64, recordID string) (*EMRRecordSummary, error) {
	var summary EMRRecordSummary
	var content, snapshot []byte
	err := s.pool.QueryRow(ctx, `
		SELECT r.id::text, r.encounter_id, r.customer_id, r.template_id::text, r.document_type, r.visit_type,
		       COALESCE(t.name, ''), r.status, r.started_at, r.updated_at,
		       r.content_json, r.patient_snapshot_json
		FROM emr_records r
		LEFT JOIN emr_templates t ON t.id = r.template_id
		WHERE r.tenant_id = $1 AND r.id = $2::uuid`, tenantID, recordID).
		Scan(&summary.ID, &summary.EncounterID, &summary.CustomerID, &summary.TemplateID, &summary.DocumentType, &summary.VisitType,
			&summary.TemplateName, &summary.Status, &summary.StartedAt, &summary.UpdatedAt, &content, &snapshot)
	if err != nil {
		return nil, err
	}
	if err := json.Unmarshal(content, &summary.Content); err != nil {
		summary.Content = map[string]any{}
	}
	if err := json.Unmarshal(snapshot, &summary.PatientSnapshot); err != nil {
		summary.PatientSnapshot = map[string]any{}
	}
	return &summary, nil
}

// ListEMRHistory 患者既往就诊摘要（按时间倒序，最多 10 条）。
func (s *Store) ListEMRHistory(ctx context.Context, tenantID, customerID int64) ([]EMRHistoryItem, error) {
	rows, err := s.pool.Query(ctx, `
		SELECT id::text, started_at, visit_type, status,
		       COALESCE(content_json->'diagnosis'->>'diagnosis_name', '')
		FROM emr_records
		WHERE tenant_id = $1 AND customer_id = $2
		ORDER BY started_at DESC LIMIT 10`, tenantID, customerID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	items := make([]EMRHistoryItem, 0)
	for rows.Next() {
		var item EMRHistoryItem
		if err := rows.Scan(&item.RecordID, &item.StartedAt, &item.VisitType, &item.Status, &item.Diagnosis); err != nil {
			return nil, err
		}
		items = append(items, item)
	}
	return items, rows.Err()
}

// CustomerSummary 患者摘要（含过敏史，来自 extra_data）。
func (s *Store) CustomerSummary(ctx context.Context, tenantID, customerID int64) (CustomerSummary, error) {
	var summary CustomerSummary
	var extraData []byte
	err := s.pool.QueryRow(ctx, `
		SELECT c.id, COALESCE(c.name, ''), COALESCE(c.phone, ''), COALESCE(c.gender, ''),
		       COALESCE(c.age, 0), COALESCE(pi.value, ''), c.extra_data
		FROM customers c
		LEFT JOIN patient_identifiers pi ON pi.customer_id = c.id
			AND pi.identifier_type = 'mrn' AND pi.status = 'active'
		WHERE c.tenant_id = $1 AND c.id = $2`, tenantID, customerID).
		Scan(&summary.CustomerID, &summary.Name, &summary.Phone, &summary.Gender,
			&summary.Age, &summary.MRN, &extraData)
	if err != nil {
		return summary, err
	}
	var extra map[string]any
	if err := json.Unmarshal(extraData, &extra); err == nil {
		if allergy, ok := extra["allergy"].(string); ok && allergy != "" {
			summary.Allergy = allergy
		}
	}
	if summary.Allergy == "" {
		summary.Allergy = "无"
	}
	return summary, nil
}

// ---------------------------------------------------------------------------
// 处方

const prescriptionColumns = `id, encounter_id, customer_id, doctor_id, COALESCE(prescription_no, ''), type, diagnosis, status, created_at`

func (s *Store) loadPrescription(ctx context.Context, q querier, tenantID, id int64) (Prescription, error) {
	var p Prescription
	err := q.QueryRow(ctx, `
		SELECT `+prescriptionColumns+` FROM his_prescriptions WHERE tenant_id = $1 AND id = $2`,
		tenantID, id).Scan(&p.ID, &p.EncounterID, &p.CustomerID, &p.DoctorID, &p.PrescriptionNo,
		&p.Type, &p.Diagnosis, &p.Status, &p.CreatedAt)
	if err != nil {
		return p, err
	}
	return s.prescriptionWithItems(ctx, q, p)
}

func (s *Store) prescriptionWithItems(ctx context.Context, q querier, p Prescription) (Prescription, error) {
	rows, err := q.Query(ctx, `
		SELECT id, drug_id, item_type, name, spec, dose, dose_unit, frequency, route, days,
		       quantity, unit, price, amount, usage_text
		FROM his_prescription_items WHERE prescription_id = $1 ORDER BY id`, p.ID)
	if err != nil {
		return p, err
	}
	defer rows.Close()

	items := make([]PrescriptionItem, 0)
	for rows.Next() {
		var item PrescriptionItem
		if err := rows.Scan(&item.ID, &item.DrugID, &item.ItemType, &item.Name, &item.Spec, &item.Dose,
			&item.DoseUnit, &item.Frequency, &item.Route, &item.Days, &item.Quantity, &item.Unit,
			&item.Price, &item.Amount, &item.UsageText); err != nil {
			return p, err
		}
		items = append(items, item)
	}
	p.Items = items
	return p, rows.Err()
}

// ListPrescriptionsByEncounter 就诊处方列表。
func (s *Store) ListPrescriptionsByEncounter(ctx context.Context, tenantID, encounterID int64) ([]Prescription, error) {
	rows, err := s.pool.Query(ctx, `
		SELECT `+prescriptionColumns+` FROM his_prescriptions
		WHERE tenant_id = $1 AND encounter_id = $2 ORDER BY id`, tenantID, encounterID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	items := make([]Prescription, 0)
	for rows.Next() {
		var p Prescription
		if err := rows.Scan(&p.ID, &p.EncounterID, &p.CustomerID, &p.DoctorID, &p.PrescriptionNo,
			&p.Type, &p.Diagnosis, &p.Status, &p.CreatedAt); err != nil {
			return nil, err
		}
		p, err = s.prescriptionWithItems(ctx, s.pool, p)
		if err != nil {
			return nil, err
		}
		items = append(items, p)
	}
	return items, rows.Err()
}

// drugLookup 从药品字典解析处方明细（名称/规格/单位/类型/零售价）。
type drugLookup struct {
	Name     string
	Spec     string
	Unit     string
	DrugType string
	Price    float64
}

func (s *Store) lookupDrug(ctx context.Context, q querier, tenantID, drugID int64) (drugLookup, error) {
	var d drugLookup
	err := q.QueryRow(ctx, `
		SELECT name, spec, unit, drug_type, retail_price FROM his_drugs
		WHERE tenant_id = $1 AND id = $2 AND status = 'active'`, tenantID, drugID).
		Scan(&d.Name, &d.Spec, &d.Unit, &d.DrugType, &d.Price)
	return d, err
}

func (s *Store) replacePrescriptionItemsTx(ctx context.Context, tx pgx.Tx, tenantID, prescriptionID int64, inputs []PrescriptionItemInput) error {
	if _, err := tx.Exec(ctx, `DELETE FROM his_prescription_items WHERE prescription_id = $1`, prescriptionID); err != nil {
		return err
	}
	for _, input := range inputs {
		drug, err := s.lookupDrug(ctx, tx, tenantID, input.DrugID)
		if err != nil {
			return err
		}
		itemType := "drug"
		if drug.DrugType == "chinese_herb" {
			itemType = "herb"
		}
		amount := drug.Price * input.Quantity
		if _, err := tx.Exec(ctx, `
			INSERT INTO his_prescription_items (prescription_id, drug_id, item_type, name, spec, dose,
				dose_unit, frequency, route, days, quantity, unit, price, amount, usage_text)
			VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13, $14, $15)`,
			prescriptionID, input.DrugID, itemType, drug.Name, drug.Spec, input.Dose, input.DoseUnit,
			input.Frequency, input.Route, input.Days, input.Quantity, drug.Unit, drug.Price, amount,
			input.UsageText); err != nil {
			return err
		}
	}
	return nil
}

// CreatePrescription 开具处方（草稿态）。
func (s *Store) CreatePrescription(ctx context.Context, tenantID int64, req CreatePrescriptionRequest) (Prescription, error) {
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return Prescription{}, err
	}
	defer tx.Rollback(ctx)

	var customerID int64
	var doctorID *int64
	if err := tx.QueryRow(ctx, `
		SELECT r.customer_id, r.doctor_id FROM his_registrations r
		WHERE r.tenant_id = $1 AND r.id IN (
			SELECT e.source_id FROM encounters e
			WHERE e.tenant_id = $1 AND e.source_type = 'his' AND e.id = $2
		)`, tenantID, req.EncounterID).Scan(&customerID, &doctorID); err != nil {
		if err == pgx.ErrNoRows {
			return Prescription{}, ErrNotFound
		}
		return Prescription{}, err
	}

	var id int64
	err = tx.QueryRow(ctx, `
		INSERT INTO his_prescriptions (tenant_id, encounter_id, customer_id, doctor_id, diagnosis)
		VALUES ($1, $2, $3, $4, $5) RETURNING id`,
		tenantID, req.EncounterID, customerID, doctorID, req.Diagnosis).Scan(&id)
	if err != nil {
		return Prescription{}, err
	}
	if err := s.replacePrescriptionItemsTx(ctx, tx, tenantID, id, req.Items); err != nil {
		return Prescription{}, err
	}
	// 处方类型：含饮片 → herbal；混合 → mixed。
	prescriptionType, err := s.prescriptionType(ctx, tx, id)
	if err != nil {
		return Prescription{}, err
	}
	if _, err := tx.Exec(ctx, `UPDATE his_prescriptions SET type = $2 WHERE id = $1`, id, prescriptionType); err != nil {
		return Prescription{}, err
	}
	if err := tx.Commit(ctx); err != nil {
		return Prescription{}, err
	}
	return s.loadPrescription(ctx, s.pool, tenantID, id)
}

// IssuePrescription 处方开具：生成处方号，draft → issued。
func (s *Store) IssuePrescription(ctx context.Context, tenantID, id int64) (Prescription, error) {
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return Prescription{}, err
	}
	defer tx.Rollback(ctx)

	var status string
	err = tx.QueryRow(ctx, `
		SELECT status FROM his_prescriptions WHERE tenant_id = $1 AND id = $2
		FOR UPDATE`, tenantID, id).Scan(&status)
	if err != nil {
		if err == pgx.ErrNoRows {
			return Prescription{}, ErrNotFound
		}
		return Prescription{}, err
	}
	if status != "draft" {
		return Prescription{}, ErrInvalidState
	}
	seq, err := s.NextNumber(ctx, tenantID, "rx")
	if err != nil {
		return Prescription{}, err
	}
	prescriptionNo := fmt.Sprintf("P%s%04d", time.Now().Format("20060102"), seq)
	if _, err := tx.Exec(ctx, `
		UPDATE his_prescriptions SET status = 'issued', prescription_no = $2, updated_at = NOW()
		WHERE id = $1`, id, prescriptionNo); err != nil {
		return Prescription{}, err
	}
	if err := tx.Commit(ctx); err != nil {
		return Prescription{}, err
	}
	return s.loadPrescription(ctx, s.pool, tenantID, id)
}

// VoidPrescription 作废处方（未收费前可作废）。
func (s *Store) VoidPrescription(ctx context.Context, tenantID, id int64) (Prescription, error) {
	tag, err := s.pool.Exec(ctx, `
		UPDATE his_prescriptions SET status = 'void', updated_at = NOW()
		WHERE tenant_id = $1 AND id = $2 AND status IN ('draft', 'issued')`, tenantID, id)
	if err != nil {
		return Prescription{}, err
	}
	if tag.RowsAffected() == 0 {
		return Prescription{}, ErrInvalidState
	}
	return s.loadPrescription(ctx, s.pool, tenantID, id)
}

// UpdatePrescription 修改处方明细（未收费前可改）。
func (s *Store) UpdatePrescription(ctx context.Context, tenantID, id int64, req UpdatePrescriptionRequest) (Prescription, error) {
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return Prescription{}, err
	}
	defer tx.Rollback(ctx)

	var status string
	err = tx.QueryRow(ctx, `
		SELECT status FROM his_prescriptions WHERE tenant_id = $1 AND id = $2
		FOR UPDATE`, tenantID, id).Scan(&status)
	if err != nil {
		if err == pgx.ErrNoRows {
			return Prescription{}, ErrNotFound
		}
		return Prescription{}, err
	}
	if status != "draft" && status != "issued" {
		return Prescription{}, ErrInvalidState
	}
	if req.Diagnosis != nil {
		if _, err := tx.Exec(ctx, `UPDATE his_prescriptions SET diagnosis = $2, updated_at = NOW() WHERE id = $1`, id, *req.Diagnosis); err != nil {
			return Prescription{}, err
		}
	}
	if req.Items != nil {
		if err := s.replacePrescriptionItemsTx(ctx, tx, tenantID, id, req.Items); err != nil {
			return Prescription{}, err
		}
		prescriptionType, err := s.prescriptionType(ctx, tx, id)
		if err != nil {
			return Prescription{}, err
		}
		if _, err := tx.Exec(ctx, `UPDATE his_prescriptions SET type = $2, updated_at = NOW() WHERE id = $1`, id, prescriptionType); err != nil {
			return Prescription{}, err
		}
	}
	if err := tx.Commit(ctx); err != nil {
		return Prescription{}, err
	}
	return s.loadPrescription(ctx, s.pool, tenantID, id)
}

func (s *Store) prescriptionType(ctx context.Context, q querier, prescriptionID int64) (string, error) {
	var hasDrug, hasHerb bool
	err := q.QueryRow(ctx, `
		SELECT EXISTS(SELECT 1 FROM his_prescription_items WHERE prescription_id = $1 AND item_type = 'drug'),
		       EXISTS(SELECT 1 FROM his_prescription_items WHERE prescription_id = $1 AND item_type = 'herb')`,
		prescriptionID).Scan(&hasDrug, &hasHerb)
	if err != nil {
		return "", err
	}
	switch {
	case hasDrug && hasHerb:
		return "mixed", nil
	case hasHerb:
		return "herbal", nil
	default:
		return "western", nil
	}
}

// ---------------------------------------------------------------------------
// 检查申请与治疗医嘱

func (s *Store) CreateServiceRequest(ctx context.Context, tenantID int64, req CreateServiceRequestRequest) (ServiceRequest, error) {
	var customerID int64
	if err := s.pool.QueryRow(ctx, `
		SELECT r.customer_id FROM his_registrations r
		JOIN encounters e ON e.source_type = 'his' AND e.source_id = r.id
		WHERE r.tenant_id = $1 AND e.id = $2`, tenantID, req.EncounterID).Scan(&customerID); err != nil {
		if err == pgx.ErrNoRows {
			return ServiceRequest{}, ErrNotFound
		}
		return ServiceRequest{}, err
	}
	requestType := req.RequestType
	if requestType == "" {
		requestType = "exam"
	}
	var item ServiceRequest
	err := s.pool.QueryRow(ctx, `
		INSERT INTO his_service_requests (tenant_id, encounter_id, customer_id, request_type, item_name, specimen_type, purpose)
		VALUES ($1, $2, $3, $4, $5, $6, $7)
		RETURNING id, encounter_id, customer_id, request_type, item_name, specimen_type, purpose, status, created_at`,
		tenantID, req.EncounterID, customerID, requestType, req.ItemName, req.SpecimenType, req.Purpose).
		Scan(&item.ID, &item.EncounterID, &item.CustomerID, &item.RequestType, &item.ItemName,
			&item.SpecimenType, &item.Purpose, &item.Status, &item.CreatedAt)
	return item, err
}

func (s *Store) ListServiceRequests(ctx context.Context, tenantID, encounterID int64) ([]ServiceRequest, error) {
	rows, err := s.pool.Query(ctx, `
		SELECT id, encounter_id, customer_id, request_type, item_name, specimen_type, purpose, status, created_at
		FROM his_service_requests WHERE tenant_id = $1 AND encounter_id = $2 ORDER BY id`, tenantID, encounterID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	items := make([]ServiceRequest, 0)
	for rows.Next() {
		var item ServiceRequest
		if err := rows.Scan(&item.ID, &item.EncounterID, &item.CustomerID, &item.RequestType, &item.ItemName,
			&item.SpecimenType, &item.Purpose, &item.Status, &item.CreatedAt); err != nil {
			return nil, err
		}
		items = append(items, item)
	}
	return items, rows.Err()
}

func (s *Store) CreateTreatmentOrder(ctx context.Context, tenantID int64, req CreateTreatmentOrderRequest) (TreatmentOrder, error) {
	var customerID int64
	if err := s.pool.QueryRow(ctx, `
		SELECT r.customer_id FROM his_registrations r
		JOIN encounters e ON e.source_type = 'his' AND e.source_id = r.id
		WHERE r.tenant_id = $1 AND e.id = $2`, tenantID, req.EncounterID).Scan(&customerID); err != nil {
		if err == pgx.ErrNoRows {
			return TreatmentOrder{}, ErrNotFound
		}
		return TreatmentOrder{}, err
	}
	var name string
	if err := s.pool.QueryRow(ctx, `SELECT name FROM his_charge_items WHERE tenant_id = $1 AND id = $2 AND status = 'active'`,
		tenantID, req.ChargeItemID).Scan(&name); err != nil {
		if err == pgx.ErrNoRows {
			return TreatmentOrder{}, ErrNotFound
		}
		return TreatmentOrder{}, err
	}
	var item TreatmentOrder
	err := s.pool.QueryRow(ctx, `
		INSERT INTO his_treatment_orders (tenant_id, encounter_id, customer_id, charge_item_id, plan_total_times)
		VALUES ($1, $2, $3, $4, $5)
		RETURNING id, encounter_id, customer_id, charge_item_id, plan_total_times, completed_times, status, created_at`,
		tenantID, req.EncounterID, customerID, req.ChargeItemID, req.PlanTotalTimes).
		Scan(&item.ID, &item.EncounterID, &item.CustomerID, &item.ChargeItemID, &item.PlanTotalTimes,
			&item.CompletedTimes, &item.Status, &item.CreatedAt)
	item.ChargeItemName = name
	return item, err
}

func (s *Store) ListTreatmentOrders(ctx context.Context, tenantID, encounterID int64) ([]TreatmentOrder, error) {
	rows, err := s.pool.Query(ctx, `
		SELECT t.id, t.encounter_id, t.customer_id, t.charge_item_id, COALESCE(ci.name, ''),
		       t.plan_total_times, t.completed_times, t.status, t.created_at
		FROM his_treatment_orders t
		LEFT JOIN his_charge_items ci ON ci.id = t.charge_item_id
		WHERE t.tenant_id = $1 AND t.encounter_id = $2 ORDER BY t.id`, tenantID, encounterID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	items := make([]TreatmentOrder, 0)
	for rows.Next() {
		var item TreatmentOrder
		if err := rows.Scan(&item.ID, &item.EncounterID, &item.CustomerID, &item.ChargeItemID, &item.ChargeItemName,
			&item.PlanTotalTimes, &item.CompletedTimes, &item.Status, &item.CreatedAt); err != nil {
			return nil, err
		}
		items = append(items, item)
	}
	return items, rows.Err()
}

// AddTreatmentExecution 治疗执行登记：每次治疗一条记录，completed_times + 1。
func (s *Store) AddTreatmentExecution(ctx context.Context, tenantID, orderID, executorID int64, note string) (TreatmentOrder, error) {
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return TreatmentOrder{}, err
	}
	defer tx.Rollback(ctx)

	var status string
	var planTotal, completed int
	err = tx.QueryRow(ctx, `
		SELECT status, plan_total_times, completed_times FROM his_treatment_orders
		WHERE tenant_id = $1 AND id = $2 FOR UPDATE`, tenantID, orderID).
		Scan(&status, &planTotal, &completed)
	if err != nil {
		if err == pgx.ErrNoRows {
			return TreatmentOrder{}, ErrNotFound
		}
		return TreatmentOrder{}, err
	}
	if status == "completed" {
		return TreatmentOrder{}, ErrInvalidState
	}
	if _, err := tx.Exec(ctx, `
		INSERT INTO his_treatment_executions (treatment_order_id, executor_id, note)
		VALUES ($1, $2, $3)`, orderID, executorID, note); err != nil {
		return TreatmentOrder{}, err
	}
	newCompleted := completed + 1
	newStatus := "executing"
	if newCompleted >= planTotal {
		newStatus = "completed"
	}
	if _, err := tx.Exec(ctx, `
		UPDATE his_treatment_orders SET completed_times = $2, status = $3, updated_at = NOW()
		WHERE id = $1`, orderID, newCompleted, newStatus); err != nil {
		return TreatmentOrder{}, err
	}
	if err := tx.Commit(ctx); err != nil {
		return TreatmentOrder{}, err
	}
	return s.loadTreatmentOrder(ctx, tenantID, orderID)
}

func (s *Store) loadTreatmentOrder(ctx context.Context, tenantID, orderID int64) (TreatmentOrder, error) {
	var item TreatmentOrder
	err := s.pool.QueryRow(ctx, `
		SELECT t.id, t.encounter_id, t.customer_id, t.charge_item_id, COALESCE(ci.name, ''),
		       t.plan_total_times, t.completed_times, t.status, t.created_at
		FROM his_treatment_orders t
		LEFT JOIN his_charge_items ci ON ci.id = t.charge_item_id
		WHERE t.tenant_id = $1 AND t.id = $2`, tenantID, orderID).
		Scan(&item.ID, &item.EncounterID, &item.CustomerID, &item.ChargeItemID, &item.ChargeItemName,
			&item.PlanTotalTimes, &item.CompletedTimes, &item.Status, &item.CreatedAt)
	return item, err
}

// WorkbenchContext 组装接诊上下文。
func (s *Store) WorkbenchContext(ctx context.Context, tenantID, encounterID int64) (*WorkbenchContext, error) {
	ctx2 := ctx
	var regID int64
	err := s.pool.QueryRow(ctx, `
		SELECT source_id FROM encounters WHERE tenant_id = $1 AND source_type = 'his' AND id = $2`,
		tenantID, encounterID).Scan(&regID)
	if err != nil {
		if err == pgx.ErrNoRows {
			return nil, ErrNotFound
		}
		return nil, err
	}
	reg, err := s.loadRegistration(ctx2, s.pool, tenantID, regID)
	if err != nil {
		return nil, err
	}
	customer, err := s.CustomerSummary(ctx, tenantID, reg.CustomerID)
	if err != nil {
		return nil, err
	}
	recordID, err := s.FindEMRRecordByEncounter(ctx, tenantID, encounterID)
	if err != nil {
		return nil, err
	}
	context3 := &WorkbenchContext{
		Registration: &reg,
		Customer:     customer,
	}
	if recordID != nil {
		record, err := s.GetEMRRecordSummary(ctx, tenantID, *recordID)
		if err != nil {
			return nil, err
		}
		context3.Record = record
	}
	if history, err := s.ListEMRHistory(ctx, tenantID, reg.CustomerID); err == nil {
		context3.History = history
	}
	if prescriptions, err := s.ListPrescriptionsByEncounter(ctx, tenantID, encounterID); err == nil {
		context3.Prescriptions = prescriptions
	}
	if requests, err := s.ListServiceRequests(ctx, tenantID, encounterID); err == nil {
		context3.ServiceRequests = requests
	}
	if orders, err := s.ListTreatmentOrders(ctx, tenantID, encounterID); err == nil {
		context3.TreatmentOrders = orders
	}
	return context3, nil
}

// ---------------------------------------------------------------------------
// 打包收费

const orderColumns = `o.id, o.order_no, o.customer_id, COALESCE(c.name, ''), o.encounter_id,
	o.items_total, o.discount_total, o.payable_total, o.paid_total, o.status, o.created_at`

func (s *Store) loadOrder(ctx context.Context, q querier, tenantID, id int64) (Order, error) {
	var o Order
	err := q.QueryRow(ctx, `
		SELECT `+orderColumns+`
		FROM his_orders o JOIN customers c ON c.id = o.customer_id
		WHERE o.tenant_id = $1 AND o.id = $2`, tenantID, id).
		Scan(&o.ID, &o.OrderNo, &o.CustomerID, &o.CustomerName, &o.EncounterID,
			&o.ItemsTotal, &o.DiscountTotal, &o.PayableTotal, &o.PaidTotal, &o.Status, &o.CreatedAt)
	if err != nil {
		return o, err
	}
	return s.orderWithItems(ctx, q, o)
}

func (s *Store) orderWithItems(ctx context.Context, q querier, o Order) (Order, error) {
	rows, err := q.Query(ctx, `
		SELECT id, order_id, item_type, ref_id, prescription_id, name, unit_price, quantity,
		       line_total, discount_amount, payable
		FROM his_order_items WHERE order_id = $1 ORDER BY id`, o.ID)
	if err != nil {
		return o, err
	}
	defer rows.Close()

	items := make([]OrderItem, 0)
	for rows.Next() {
		var item OrderItem
		if err := rows.Scan(&item.ID, &item.OrderID, &item.ItemType, &item.RefID, &item.PrescriptionID,
			&item.Name, &item.UnitPrice, &item.Quantity, &item.LineTotal, &item.DiscountAmount, &item.Payable); err != nil {
			return o, err
		}
		items = append(items, item)
	}
	o.Items = items
	return o, rows.Err()
}

// ListOrders 收费台列表（?status=&encounter_id=&customer_id=）。
func (s *Store) ListOrders(ctx context.Context, tenantID int64, status string, encounterID, customerID int64) ([]Order, error) {
	rows, err := s.pool.Query(ctx, `
		SELECT `+orderColumns+`
		FROM his_orders o JOIN customers c ON c.id = o.customer_id
		WHERE o.tenant_id = $1
		  AND ($2 = '' OR o.status = $2)
		  AND ($3 = 0 OR o.encounter_id = $3)
		  AND ($4 = 0 OR o.customer_id = $4)
		ORDER BY o.id DESC LIMIT 200`, tenantID, status, encounterID, customerID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	items := make([]Order, 0)
	for rows.Next() {
		var o Order
		if err := rows.Scan(&o.ID, &o.OrderNo, &o.CustomerID, &o.CustomerName, &o.EncounterID,
			&o.ItemsTotal, &o.DiscountTotal, &o.PayableTotal, &o.PaidTotal, &o.Status, &o.CreatedAt); err != nil {
			return nil, err
		}
		o, err = s.orderWithItems(ctx, s.pool, o)
		if err != nil {
			return nil, err
		}
		items = append(items, o)
	}
	return items, rows.Err()
}

// orderCustomer 就诊对应的患者。
func orderCustomer(ctx context.Context, q querier, tenantID, encounterID int64) (int64, error) {
	var customerID int64
	err := q.QueryRow(ctx, `
		SELECT r.customer_id FROM his_registrations r
		JOIN encounters e ON e.source_type = 'his' AND e.source_id = r.id
		WHERE r.tenant_id = $1 AND e.id = $2`, tenantID, encounterID).Scan(&customerID)
	if err != nil {
		if err == pgx.ErrNoRows {
			return 0, ErrNotFound
		}
		return 0, err
	}
	return customerID, nil
}

// resolveOrderLine 解析收费行（价格与名称来自字典/打包价，禁止前端传价）。
type resolvedLine struct {
	Name           string
	UnitPrice      float64
	PrescriptionID *int64
}

func (s *Store) resolveOrderLine(ctx context.Context, q querier, tenantID, encounterID int64, input OrderLineInput) (resolvedLine, error) {
	switch input.ItemType {
	case "charge_item":
		var line resolvedLine
		err := q.QueryRow(ctx, `
			SELECT name, price FROM his_charge_items
			WHERE tenant_id = $1 AND id = $2 AND status = 'active'`, tenantID, input.RefID).
			Scan(&line.Name, &line.UnitPrice)
		if err != nil {
			return line, err
		}
		return line, nil
	case "bundle":
		var line resolvedLine
		err := q.QueryRow(ctx, `
			SELECT name, price FROM his_bundles
			WHERE tenant_id = $1 AND id = $2 AND status = 'active'`, tenantID, input.RefID).
			Scan(&line.Name, &line.UnitPrice)
		if err != nil {
			return line, err
		}
		return line, nil
	case "drug":
		var line resolvedLine
		err := q.QueryRow(ctx, `
			SELECT name, retail_price FROM his_drugs
			WHERE tenant_id = $1 AND id = $2 AND status = 'active'`, tenantID, input.RefID).
			Scan(&line.Name, &line.UnitPrice)
		if err != nil {
			return line, err
		}
		if input.PrescriptionID != nil {
			var status, errEnc int64
			var rxStatus string
			err := q.QueryRow(ctx, `
				SELECT id, encounter_id, status FROM his_prescriptions
				WHERE tenant_id = $1 AND id = $2 AND status IN ('draft', 'issued')`,
				tenantID, *input.PrescriptionID).Scan(&status, &errEnc, &rxStatus)
			if err != nil {
				return line, err
			}
			if errEnc != encounterID {
				return line, ErrInvalidRequest
			}
			line.PrescriptionID = input.PrescriptionID
		}
		return line, nil
	default:
		return resolvedLine{}, ErrInvalidRequest
	}
}

// replaceOrderItemsTx 全量替换收费行（事务内）。
func (s *Store) replaceOrderItemsTx(ctx context.Context, tx pgx.Tx, tenantID, encounterID, orderID int64, inputs []OrderLineInput) error {
	if _, err := tx.Exec(ctx, `DELETE FROM his_order_items WHERE order_id = $1`, orderID); err != nil {
		return err
	}
	for _, input := range inputs {
		line, err := s.resolveOrderLine(ctx, tx, tenantID, encounterID, input)
		if err != nil {
			return err
		}
		lineTotal := line.UnitPrice*input.Quantity - input.DiscountAmount
		if lineTotal < 0 {
			return ErrInvalidRequest
		}
		if _, err := tx.Exec(ctx, `
			INSERT INTO his_order_items (order_id, item_type, ref_id, prescription_id, name, unit_price,
				quantity, line_total, discount_amount, payable)
			VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $8)`,
			orderID, input.ItemType, input.RefID, line.PrescriptionID, line.Name, line.UnitPrice,
			input.Quantity, lineTotal, input.DiscountAmount); err != nil {
			return err
		}
	}
	return nil
}

// recomputeOrderTotals 重算合计（事务内）。
func recomputeOrderTotals(ctx context.Context, tx pgx.Tx, orderID int64, discountTotal float64) error {
	var itemsTotal float64
	if err := tx.QueryRow(ctx, `
		SELECT COALESCE(SUM(line_total), 0) FROM his_order_items WHERE order_id = $1`, orderID).Scan(&itemsTotal); err != nil {
		return err
	}
	payable := itemsTotal - discountTotal
	if payable < 0 {
		return ErrInvalidRequest
	}
	_, err := tx.Exec(ctx, `
		UPDATE his_orders SET items_total = $2, discount_total = $3, payable_total = $4, updated_at = NOW()
		WHERE id = $1`, orderID, itemsTotal, discountTotal, payable)
	return err
}

// CreateOrder 划价：生成收费单（pending）。
func (s *Store) CreateOrder(ctx context.Context, tenantID, createdBy int64, req CreateOrderRequest) (Order, error) {
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return Order{}, err
	}
	defer tx.Rollback(ctx)

	customerID, err := orderCustomer(ctx, tx, tenantID, req.EncounterID)
	if err != nil {
		return Order{}, err
	}
	seq, err := s.NextNumber(ctx, tenantID, "order")
	if err != nil {
		return Order{}, err
	}
	orderNo := fmt.Sprintf("O%s%04d", time.Now().Format("20060102"), seq)
	var id int64
	if err := tx.QueryRow(ctx, `
		INSERT INTO his_orders (tenant_id, customer_id, encounter_id, order_no, status, created_by)
		VALUES ($1, $2, $3, $4, 'pending', $5) RETURNING id`,
		tenantID, customerID, req.EncounterID, orderNo, createdBy).Scan(&id); err != nil {
		return Order{}, err
	}
	if err := s.replaceOrderItemsTx(ctx, tx, tenantID, req.EncounterID, id, req.Items); err != nil {
		return Order{}, err
	}
	if err := recomputeOrderTotals(ctx, tx, id, req.DiscountTotal); err != nil {
		return Order{}, err
	}
	if err := tx.Commit(ctx); err != nil {
		return Order{}, err
	}
	return s.loadOrder(ctx, s.pool, tenantID, id)
}

// UpdateOrder 调整收费单（仅 pending）。
func (s *Store) UpdateOrder(ctx context.Context, tenantID, id int64, req UpdateOrderRequest) (Order, error) {
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return Order{}, err
	}
	defer tx.Rollback(ctx)

	var status string
	var encounterID int64
	err = tx.QueryRow(ctx, `
		SELECT status, encounter_id FROM his_orders WHERE tenant_id = $1 AND id = $2
		FOR UPDATE`, tenantID, id).Scan(&status, &encounterID)
	if err != nil {
		if err == pgx.ErrNoRows {
			return Order{}, ErrNotFound
		}
		return Order{}, err
	}
	if status != "pending" {
		return Order{}, ErrInvalidState
	}
	if req.Items != nil {
		if err := s.replaceOrderItemsTx(ctx, tx, tenantID, encounterID, id, req.Items); err != nil {
			return Order{}, err
		}
	}
	var discountTotal float64
	_ = tx.QueryRow(ctx, `SELECT discount_total FROM his_orders WHERE id = $1`, id).Scan(&discountTotal)
	if req.DiscountTotal != nil {
		discountTotal = *req.DiscountTotal
	}
	if err := recomputeOrderTotals(ctx, tx, id, discountTotal); err != nil {
		return Order{}, err
	}
	if err := tx.Commit(ctx); err != nil {
		return Order{}, err
	}
	return s.loadOrder(ctx, s.pool, tenantID, id)
}

// AddPayment 收款：部分/全额，付清后联动处方 charged。
func (s *Store) AddPayment(ctx context.Context, tenantID, operatorID, orderID int64, req CreatePaymentRequest) (Order, error) {
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return Order{}, err
	}
	defer tx.Rollback(ctx)

	var status string
	var payable, paid float64
	err = tx.QueryRow(ctx, `
		SELECT status, payable_total, paid_total FROM his_orders
		WHERE tenant_id = $1 AND id = $2 FOR UPDATE`, tenantID, orderID).Scan(&status, &payable, &paid)
	if err != nil {
		if err == pgx.ErrNoRows {
			return Order{}, ErrNotFound
		}
		return Order{}, err
	}
	if status != "pending" && status != "partial" {
		return Order{}, ErrInvalidState
	}
	if req.Amount <= 0 || req.Amount > payable-paid {
		return Order{}, ErrInvalidRequest
	}
	method := req.Method
	if method == "" {
		method = "cash"
	}
	if _, err := tx.Exec(ctx, `
		INSERT INTO his_payments (tenant_id, order_id, method, amount, operator_id)
		VALUES ($1, $2, $3, $4, $5)`, tenantID, orderID, method, req.Amount, operatorID); err != nil {
		return Order{}, err
	}
	newPaid := paid + req.Amount
	newStatus := "partial"
	if newPaid >= payable {
		newStatus = "paid"
	}
	if _, err := tx.Exec(ctx, `
		UPDATE his_orders SET paid_total = $2, status = $3, updated_at = NOW()
		WHERE id = $1`, orderID, newPaid, newStatus); err != nil {
		return Order{}, err
	}
	if newStatus == "paid" {
		// 处方收费联动：药品行关联的处方 charged。
		if _, err := tx.Exec(ctx, `
			UPDATE his_prescriptions SET status = 'charged', updated_at = NOW()
			WHERE id IN (
				SELECT prescription_id FROM his_order_items
				WHERE order_id = $1 AND prescription_id IS NOT NULL
			) AND status IN ('draft', 'issued')`, orderID); err != nil {
			return Order{}, err
		}
	}
	if err := tx.Commit(ctx); err != nil {
		return Order{}, err
	}
	return s.loadOrder(ctx, s.pool, tenantID, orderID)
}

// RefundOrder 退费（全额）：已发药处方不允许退费；处方 charged → issued 回冲。
func (s *Store) RefundOrder(ctx context.Context, tenantID, operatorID, orderID int64, reason string) (Order, error) {
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return Order{}, err
	}
	defer tx.Rollback(ctx)

	var status string
	var paid float64
	err = tx.QueryRow(ctx, `
		SELECT status, paid_total FROM his_orders
		WHERE tenant_id = $1 AND id = $2 FOR UPDATE`, tenantID, orderID).Scan(&status, &paid)
	if err != nil {
		if err == pgx.ErrNoRows {
			return Order{}, ErrNotFound
		}
		return Order{}, err
	}
	if status != "paid" && status != "partial" {
		return Order{}, ErrInvalidState
	}
	var dispensed bool
	if err := tx.QueryRow(ctx, `
		SELECT EXISTS(
			SELECT 1 FROM his_order_items oi
			JOIN his_prescriptions p ON p.id = oi.prescription_id
			WHERE oi.order_id = $1 AND p.status = 'dispensed'
		)`, orderID).Scan(&dispensed); err != nil {
		return Order{}, err
	}
	if dispensed {
		return Order{}, ErrDispensedCannotRefund
	}
	if _, err := tx.Exec(ctx, `
		INSERT INTO his_refunds (tenant_id, order_id, amount, reason, operator_id)
		VALUES ($1, $2, $3, $4, $5)`, tenantID, orderID, paid, reason, operatorID); err != nil {
		return Order{}, err
	}
	if _, err := tx.Exec(ctx, `
		UPDATE his_orders SET status = 'refunded', updated_at = NOW() WHERE id = $1`, orderID); err != nil {
		return Order{}, err
	}
	if _, err := tx.Exec(ctx, `
		UPDATE his_prescriptions SET status = 'issued', updated_at = NOW()
		WHERE id IN (
			SELECT prescription_id FROM his_order_items
			WHERE order_id = $1 AND prescription_id IS NOT NULL
		) AND status = 'charged'`, orderID); err != nil {
		return Order{}, err
	}
	if err := tx.Commit(ctx); err != nil {
		return Order{}, err
	}
	return s.loadOrder(ctx, s.pool, tenantID, orderID)
}

// VoidOrder 作废收费单（仅 pending）。
func (s *Store) VoidOrder(ctx context.Context, tenantID, orderID int64) (Order, error) {
	tag, err := s.pool.Exec(ctx, `
		UPDATE his_orders SET status = 'void', updated_at = NOW()
		WHERE tenant_id = $1 AND id = $2 AND status = 'pending'`, tenantID, orderID)
	if err != nil {
		return Order{}, err
	}
	if tag.RowsAffected() == 0 {
		return Order{}, ErrInvalidState
	}
	return s.loadOrder(ctx, s.pool, tenantID, orderID)
}

// ---------------------------------------------------------------------------
// 药房

// ListPharmacyPrescriptions 药房处方队列（?status=charged|dispensed，默认 charged）。
func (s *Store) ListPharmacyPrescriptions(ctx context.Context, tenantID int64, status string) ([]PharmacyPrescription, error) {
	if status == "" {
		status = "charged"
	}
	rows, err := s.pool.Query(ctx, `
		SELECT p.id, p.encounter_id, p.customer_id, p.doctor_id, COALESCE(p.prescription_no, ''),
		       p.type, p.diagnosis, p.status, p.created_at,
		       COALESCE(c.name, ''), COALESCE(pi.value, '')
		FROM his_prescriptions p
		JOIN customers c ON c.id = p.customer_id
		LEFT JOIN patient_identifiers pi ON pi.customer_id = p.customer_id
			AND pi.identifier_type = 'mrn' AND pi.status = 'active'
		WHERE p.tenant_id = $1 AND p.status = $2
		ORDER BY p.id`, tenantID, status)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	items := make([]PharmacyPrescription, 0)
	for rows.Next() {
		var item PharmacyPrescription
		if err := rows.Scan(&item.ID, &item.EncounterID, &item.CustomerID, &item.DoctorID, &item.PrescriptionNo,
			&item.Type, &item.Diagnosis, &item.Status, &item.CreatedAt, &item.CustomerName, &item.MRN); err != nil {
			return nil, err
		}
		item, err = s.prescriptionWithItemsPharmacy(ctx, item)
		if err != nil {
			return nil, err
		}
		items = append(items, item)
	}
	return items, rows.Err()
}

func (s *Store) prescriptionWithItemsPharmacy(ctx context.Context, p PharmacyPrescription) (PharmacyPrescription, error) {
	full, err := s.prescriptionWithItems(ctx, s.pool, p.Prescription)
	if err != nil {
		return p, err
	}
	p.Prescription = full
	return p, nil
}

// InboundStock 入库：叠加库存 + 流水。
func (s *Store) InboundStock(ctx context.Context, tenantID, operatorID int64, req InboundStockRequest) (StockItem, error) {
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return StockItem{}, err
	}
	defer tx.Rollback(ctx)

	if err := inboundStockTx(ctx, tx, tenantID, operatorID, req, "inbound", nil); err != nil {
		return StockItem{}, err
	}
	if err := tx.Commit(ctx); err != nil {
		return StockItem{}, err
	}
	return s.stockItemByDrug(ctx, tenantID, req.DrugID)
}

// inboundStockTx 在既有事务内叠加库存并写流水，供普通入库与采购收货共用。
func inboundStockTx(ctx context.Context, tx pgx.Tx, tenantID, operatorID int64, req InboundStockRequest, refType string, refID *int64) error {
	if refType == "" {
		refType = "inbound"
	}
	var stockID int64
	err := tx.QueryRow(ctx, `
		SELECT id FROM his_stock
		WHERE tenant_id = $1 AND drug_id = $2 AND batch_no = $3 LIMIT 1`,
		tenantID, req.DrugID, req.BatchNo).Scan(&stockID)
	if err == pgx.ErrNoRows {
		err = tx.QueryRow(ctx, `
			INSERT INTO his_stock (tenant_id, drug_id, batch_no, quantity, expiry_date, location)
			VALUES ($1, $2, $3, $4, $5, $6) RETURNING id`,
			tenantID, req.DrugID, req.BatchNo, req.Quantity, req.ExpiryDate, req.Location).Scan(&stockID)
		if err != nil {
			return err
		}
	} else if err != nil {
		return err
	} else {
		if _, err := tx.Exec(ctx, `
			UPDATE his_stock SET quantity = quantity + $3, expiry_date = COALESCE($4, expiry_date),
				location = COALESCE($5, location), updated_at = NOW()
			WHERE id = $1 AND tenant_id = $2`, stockID, tenantID, req.Quantity, req.ExpiryDate, req.Location); err != nil {
			return err
		}
	}
	if _, err := tx.Exec(ctx, `
		INSERT INTO his_stock_movements (tenant_id, drug_id, move_type, quantity, batch_no, ref_type, ref_id, created_by)
		VALUES ($1, $2, 'inbound', $3, $4, $5, $6, $7)`,
		tenantID, req.DrugID, req.Quantity, req.BatchNo, refType, refID, operatorID); err != nil {
		return err
	}
	return nil
}

func (s *Store) stockItemByDrug(ctx context.Context, tenantID, drugID int64) (StockItem, error) {
	var item StockItem
	var expiryDate *string
	err := s.pool.QueryRow(ctx, `
		SELECT d.id, d.name, d.spec, d.unit, st.batch_no, st.quantity,
		       to_char(st.expiry_date, 'YYYY-MM-DD'), st.location
		FROM his_drugs d
		JOIN his_stock st ON st.drug_id = d.id AND st.tenant_id = $1
		WHERE d.id = $2 AND d.tenant_id = $1
		ORDER BY st.expiry_date NULLS LAST LIMIT 1`, tenantID, drugID).
		Scan(&item.DrugID, &item.DrugName, &item.Spec, &item.Unit, &item.BatchNo, &item.Quantity,
			&expiryDate, &item.Location)
	if err != nil {
		return item, err
	}
	item.ExpiryDate = expiryDate
	item.ExpiringSoon = expiryDate != nil && *expiryDate != "" && *expiryDate <= time.Now().AddDate(0, 0, 30).Format("2006-01-02")
	return item, nil
}

// ListStock 库存列表（?keyword= 药品名模糊）。
func (s *Store) ListStock(ctx context.Context, tenantID int64, keyword string) ([]StockItem, error) {
	kw := "%" + strings.TrimSpace(keyword) + "%"
	rows, err := s.pool.Query(ctx, `
		SELECT d.id, d.name, d.spec, d.unit, COALESCE(st.batch_no, ''), COALESCE(st.quantity, 0),
		       to_char(st.expiry_date, 'YYYY-MM-DD'), COALESCE(st.location, '')
		FROM his_drugs d
		JOIN his_stock st ON st.drug_id = d.id AND st.tenant_id = d.tenant_id
		WHERE d.tenant_id = $1 AND ($2 = '%%' OR d.name ILIKE $2)
		ORDER BY d.id`, tenantID, kw)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	items := make([]StockItem, 0)
	for rows.Next() {
		var item StockItem
		var expiryDate *string
		if err := rows.Scan(&item.DrugID, &item.DrugName, &item.Spec, &item.Unit, &item.BatchNo,
			&item.Quantity, &expiryDate, &item.Location); err != nil {
			return nil, err
		}
		item.ExpiryDate = expiryDate
		item.ExpiringSoon = expiryDate != nil && *expiryDate != "" && *expiryDate <= time.Now().AddDate(0, 0, 30).Format("2006-01-02")
		items = append(items, item)
	}
	return items, rows.Err()
}

// DispensePrescription 发药：扣库存 + 流水，处方 charged → dispensed。
func (s *Store) DispensePrescription(ctx context.Context, tenantID, operatorID, prescriptionID int64) (PharmacyPrescription, error) {
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return PharmacyPrescription{}, err
	}
	defer tx.Rollback(ctx)

	var status string
	err = tx.QueryRow(ctx, `
		SELECT status FROM his_prescriptions WHERE tenant_id = $1 AND id = $2
		FOR UPDATE`, tenantID, prescriptionID).Scan(&status)
	if err != nil {
		if err == pgx.ErrNoRows {
			return PharmacyPrescription{}, ErrNotFound
		}
		return PharmacyPrescription{}, err
	}
	if status != "charged" {
		return PharmacyPrescription{}, ErrInvalidState
	}

	rows, err := tx.Query(ctx, `
		SELECT drug_id, quantity FROM his_prescription_items WHERE prescription_id = $1`, prescriptionID)
	if err != nil {
		return PharmacyPrescription{}, err
	}
	type rxItem struct {
		drugID   int64
		quantity float64
	}
	items := make([]rxItem, 0)
	for rows.Next() {
		var item rxItem
		if err := rows.Scan(&item.drugID, &item.quantity); err != nil {
			rows.Close()
			return PharmacyPrescription{}, err
		}
		items = append(items, item)
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return PharmacyPrescription{}, err
	}
	if len(items) == 0 {
		return PharmacyPrescription{}, ErrInvalidRequest
	}

	for _, item := range items {
		tag, err := tx.Exec(ctx, `
			UPDATE his_stock SET quantity = quantity - $3, updated_at = NOW()
			WHERE tenant_id = $1 AND drug_id = $2 AND quantity >= $3
			  AND id = (SELECT id FROM his_stock WHERE tenant_id = $1 AND drug_id = $2
			            ORDER BY expiry_date NULLS LAST LIMIT 1)`,
			tenantID, item.drugID, item.quantity)
		if err != nil {
			return PharmacyPrescription{}, err
		}
		if tag.RowsAffected() == 0 {
			return PharmacyPrescription{}, ErrInsufficientStock
		}
		if _, err := tx.Exec(ctx, `
			INSERT INTO his_stock_movements (tenant_id, drug_id, move_type, quantity, ref_type, ref_id, created_by)
			VALUES ($1, $2, 'dispense', $3, 'prescription', $4, $5)`,
			tenantID, item.drugID, item.quantity, prescriptionID, operatorID); err != nil {
			return PharmacyPrescription{}, err
		}
	}

	if _, err := tx.Exec(ctx, `
		UPDATE his_prescriptions SET status = 'dispensed', updated_at = NOW()
		WHERE id = $1`, prescriptionID); err != nil {
		return PharmacyPrescription{}, err
	}
	if err := tx.Commit(ctx); err != nil {
		return PharmacyPrescription{}, err
	}

	full, err := s.loadPrescription(ctx, s.pool, tenantID, prescriptionID)
	if err != nil {
		return PharmacyPrescription{}, err
	}
	var customerName, mrn string
	_ = s.pool.QueryRow(ctx, `
		SELECT COALESCE(c.name, ''), COALESCE(pi.value, '')
		FROM his_prescriptions p
		JOIN customers c ON c.id = p.customer_id
		LEFT JOIN patient_identifiers pi ON pi.customer_id = p.customer_id
			AND pi.identifier_type = 'mrn' AND pi.status = 'active'
		WHERE p.id = $1`, prescriptionID).Scan(&customerName, &mrn)
	return PharmacyPrescription{Prescription: full, CustomerName: customerName, MRN: mrn}, nil
}

// ---------------------------------------------------------------------------
// 医疗文书（统一文书服务：渲染快照 → 签名留痕 → 归档打印）

// FindDocumentByRef 同类型+引用幂等查询。
func (s *Store) FindDocumentByRef(ctx context.Context, tenantID int64, docType string, refID int64) (*int64, error) {
	var id int64
	err := s.pool.QueryRow(ctx, `
		SELECT id FROM his_documents
		WHERE tenant_id = $1 AND doc_type = $2 AND ref_id = $3
		ORDER BY id DESC LIMIT 1`, tenantID, docType, refID).Scan(&id)
	if err != nil {
		if err == pgx.ErrNoRows {
			return nil, nil
		}
		return nil, err
	}
	return &id, nil
}

// CreateDocument 写入文书快照。
func (s *Store) CreateDocument(ctx context.Context, tenantID int64, docType string, refID, encounterID, customerID *int64, snapshot map[string]any) (int64, error) {
	raw, err := json.Marshal(snapshot)
	if err != nil {
		return 0, err
	}
	var id int64
	err = s.pool.QueryRow(ctx, `
		INSERT INTO his_documents (tenant_id, doc_type, ref_id, encounter_id, customer_id, content_snapshot)
		VALUES ($1, $2, $3, $4, $5, $6::jsonb) RETURNING id`,
		tenantID, docType, refID, encounterID, customerID, string(raw)).Scan(&id)
	return id, err
}

// LoadDocument 文书详情（含签名）。
func (s *Store) LoadDocument(ctx context.Context, tenantID, id int64) (*Document, error) {
	var doc Document
	var snapshot []byte
	err := s.pool.QueryRow(ctx, `
		SELECT id, doc_type, ref_id, encounter_id, customer_id, content_snapshot, status, printed_at, created_at
		FROM his_documents WHERE tenant_id = $1 AND id = $2`, tenantID, id).
		Scan(&doc.ID, &doc.DocType, &doc.RefID, &doc.EncounterID, &doc.CustomerID,
			&snapshot, &doc.Status, &doc.PrintedAt, &doc.CreatedAt)
	if err != nil {
		return nil, err
	}
	if err := json.Unmarshal(snapshot, &doc.ContentSnapshot); err != nil {
		return nil, err
	}
	signatures, err := s.ListSignatures(ctx, tenantID, id)
	if err != nil {
		return nil, err
	}
	doc.Signatures = signatures
	return &doc, nil
}

// ListDocuments 文书列表（?customer_id=&encounter_id=&doc_type=&status=）。
func (s *Store) ListDocuments(ctx context.Context, tenantID, customerID, encounterID int64, docType, status string) ([]Document, error) {
	rows, err := s.pool.Query(ctx, `
		SELECT id, doc_type, ref_id, encounter_id, customer_id, content_snapshot, status, printed_at, created_at
		FROM his_documents
		WHERE tenant_id = $1
		  AND ($2 = 0 OR customer_id = $2)
		  AND ($3 = 0 OR encounter_id = $3)
		  AND ($4 = '' OR doc_type = $4)
		  AND ($5 = '' OR status = $5)
		ORDER BY id DESC LIMIT 200`, tenantID, customerID, encounterID, docType, status)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	docs := make([]Document, 0)
	for rows.Next() {
		var doc Document
		var snapshot []byte
		if err := rows.Scan(&doc.ID, &doc.DocType, &doc.RefID, &doc.EncounterID, &doc.CustomerID,
			&snapshot, &doc.Status, &doc.PrintedAt, &doc.CreatedAt); err != nil {
			return nil, err
		}
		if err := json.Unmarshal(snapshot, &doc.ContentSnapshot); err != nil {
			return nil, err
		}
		docs = append(docs, doc)
	}
	return docs, rows.Err()
}

// ListSignatures 文书签名列表。
func (s *Store) ListSignatures(ctx context.Context, tenantID, documentID int64) ([]Signature, error) {
	rows, err := s.pool.Query(ctx, `
		SELECT id, document_id, signer_type, signer_customer_id, signer_employee_id,
		       signature_path, content_hash, signed_at, device_ip, provider
		FROM his_signatures WHERE document_id = $1 ORDER BY id`, documentID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	items := make([]Signature, 0)
	for rows.Next() {
		var item Signature
		if err := rows.Scan(&item.ID, &item.DocumentID, &item.SignerType, &item.SignerCustomerID,
			&item.SignerEmployeeID, &item.SignaturePath, &item.ContentHash, &item.SignedAt,
			&item.DeviceIP, &item.Provider); err != nil {
			return nil, err
		}
		items = append(items, item)
	}
	return items, rows.Err()
}

// AddSignature 签署文书（绑定当前内容哈希；内容变更后哈希不符 → 归档拦截）。
func (s *Store) AddSignature(ctx context.Context, tenantID int64, req CreateSignatureRequest, documentID int64, contentHash string, employeeID int64) (Signature, error) {
	var item Signature
	err := s.pool.QueryRow(ctx, `
		INSERT INTO his_signatures (document_id, signer_type, signer_customer_id, signer_employee_id,
			signature_path, content_hash, device_ip, provider)
		VALUES ($1, $2, $3, $4, $5, $6, $7, 'local_pad')
		RETURNING id, document_id, signer_type, signer_customer_id, signer_employee_id,
			signature_path, content_hash, signed_at, device_ip, provider`,
		documentID, req.SignerType, req.SignerCustomerID, employeeID, req.SignaturePath,
		contentHash, req.DeviceIP).
		Scan(&item.ID, &item.DocumentID, &item.SignerType, &item.SignerCustomerID, &item.SignerEmployeeID,
			&item.SignaturePath, &item.ContentHash, &item.SignedAt, &item.DeviceIP, &item.Provider)
	if err != nil {
		return item, err
	}
	// 首次签名后文书进入 signed 状态。
	if _, err := s.pool.Exec(ctx, `
		UPDATE his_documents SET status = 'signed', updated_at = NOW()
		WHERE id = $1 AND status = 'draft'`, documentID); err != nil {
		return item, err
	}
	return item, nil
}

// ArchiveDocument 归档：若有签名则校验哈希一致，随后 status → archived。
func (s *Store) ArchiveDocument(ctx context.Context, tenantID, id int64) (*Document, error) {
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback(ctx)

	var status string
	err = tx.QueryRow(ctx, `
		SELECT status FROM his_documents WHERE tenant_id = $1 AND id = $2 FOR UPDATE`, tenantID, id).Scan(&status)
	if err != nil {
		return nil, err
	}
	if status == "archived" {
		_ = tx.Commit(ctx)
		return s.LoadDocument(ctx, tenantID, id)
	}
	if status != "draft" && status != "signed" {
		return nil, ErrInvalidState
	}
	if _, err := tx.Exec(ctx, `
		UPDATE his_documents SET status = 'archived', updated_at = NOW() WHERE id = $1`, id); err != nil {
		return nil, err
	}
	if err := tx.Commit(ctx); err != nil {
		return nil, err
	}
	return s.LoadDocument(ctx, tenantID, id)
}

// MarkDocumentPrinted 标记已打印（纸质归档兜底）。
func (s *Store) MarkDocumentPrinted(ctx context.Context, tenantID, id int64) (*Document, error) {
	if _, err := s.pool.Exec(ctx, `
		UPDATE his_documents SET status = 'printed', printed_at = NOW(), updated_at = NOW()
		WHERE tenant_id = $1 AND id = $2 AND status IN ('draft', 'signed', 'archived')`, tenantID, id); err != nil {
		return nil, err
	}
	return s.LoadDocument(ctx, tenantID, id)
}

// VerifySignaturesIntact 校验签名与当前快照一致（内容变更 → 哈希不符）。
func (s *Store) VerifySignaturesIntact(ctx context.Context, tenantID, documentID int64) (bool, error) {
	doc, err := s.LoadDocument(ctx, tenantID, documentID)
	if err != nil {
		return false, err
	}
	if len(doc.Signatures) == 0 {
		return true, nil
	}
	raw, err := json.Marshal(doc.ContentSnapshot)
	if err != nil {
		return false, err
	}
	hash := contentHash(string(raw))
	for _, sig := range doc.Signatures {
		if sig.ContentHash != hash {
			return false, nil
		}
	}
	return true, nil
}

// UpsertFollowupExtension 随访文书补录字段（外接随访产品）。
func (s *Store) UpsertFollowupExtension(ctx context.Context, followupID int64, req FollowupDocumentRequest) error {
	_, err := s.pool.Exec(ctx, `
		INSERT INTO his_followup_doc_extensions (followup_id, symptoms, adherence, adverse_reactions, education, next_followup_at)
		VALUES ($1, $2, $3, $4, $5, $6)
		ON CONFLICT (followup_id) DO UPDATE
		SET symptoms = EXCLUDED.symptoms, adherence = EXCLUDED.adherence,
		    adverse_reactions = EXCLUDED.adverse_reactions, education = EXCLUDED.education,
		    next_followup_at = EXCLUDED.next_followup_at, updated_at = NOW()`,
		followupID, req.Symptoms, req.Adherence, req.AdverseReactions, req.Education, req.NextFollowupAt)
	return err
}

// ---------------------------------------------------------------------------
// 基础报表

// ReportSummary 经营汇总。
func (s *Store) ReportSummary(ctx context.Context, tenantID int64, dateFrom, dateTo string) (ReportSummary, error) {
	var summary ReportSummary
	if dateFrom == "" {
		dateFrom = time.Now().Format("2006-01") + "-01"
	}
	if dateTo == "" {
		dateTo = time.Now().Format("2006-01-02")
	}

	err := s.pool.QueryRow(ctx, `
		SELECT
		  (SELECT COUNT(*) FROM his_registrations WHERE tenant_id = $1 AND visit_date BETWEEN $2::date AND $3::date AND status IN ('registered','visited')),
		  (SELECT COUNT(*) FROM his_registrations WHERE tenant_id = $1 AND visit_date BETWEEN $2::date AND $3::date AND status = 'visited'),
		  (SELECT COALESCE(SUM(p.amount), 0) FROM his_payments p JOIN his_orders o ON o.id = p.order_id WHERE p.tenant_id = $1 AND p.paid_at::date BETWEEN $2::date AND $3::date AND p.status = 'paid'),
		  (SELECT COALESCE(SUM(r.amount), 0) FROM his_refunds r WHERE r.tenant_id = $1 AND r.created_at::date BETWEEN $2::date AND $3::date)`,
		tenantID, dateFrom, dateTo).
		Scan(&summary.Visits, &summary.CompletedVisits, &summary.Revenue, &summary.RefundTotal)
	if err != nil {
		return summary, err
	}
	summary.NetRevenue = summary.Revenue - summary.RefundTotal

	methodRows, err := s.pool.Query(ctx, `
		SELECT p.method, COALESCE(SUM(p.amount), 0), COUNT(*)
		FROM his_payments p
		WHERE p.tenant_id = $1 AND p.paid_at::date BETWEEN $2::date AND $3::date AND p.status = 'paid'
		GROUP BY p.method ORDER BY 2 DESC`, tenantID, dateFrom, dateTo)
	if err != nil {
		return summary, err
	}
	defer methodRows.Close()
	for methodRows.Next() {
		var item PaymentMethodStat
		if err := methodRows.Scan(&item.Method, &item.Amount, &item.Count); err != nil {
			return summary, err
		}
		summary.PaymentMethods = append(summary.PaymentMethods, item)
	}
	methodRows.Close()

	doctorRows, err := s.pool.Query(ctx, `
		SELECT r.doctor_id, COALESCE(e.full_name, e.name, e.username, ''), COUNT(DISTINCT r.id),
		       (SELECT COUNT(*) FROM his_prescriptions p WHERE p.doctor_id = r.doctor_id AND p.tenant_id = $1 AND p.created_at::date BETWEEN $2::date AND $3::date)
		FROM his_registrations r
		LEFT JOIN his_doctors d ON d.id = r.doctor_id
		LEFT JOIN employees e ON e.id = d.employee_id
		WHERE r.tenant_id = $1 AND r.visit_date BETWEEN $2::date AND $3::date
		GROUP BY r.doctor_id, e.full_name, e.name, e.username ORDER BY 3 DESC`, tenantID, dateFrom, dateTo)
	if err != nil {
		return summary, err
	}
	defer doctorRows.Close()
	for doctorRows.Next() {
		var item DoctorWorkloadStat
		if err := doctorRows.Scan(&item.DoctorID, &item.DoctorName, &item.VisitCount, &item.PrescriptionCnt); err != nil {
			return summary, err
		}
		summary.DoctorWorkload = append(summary.DoctorWorkload, item)
	}
	doctorRows.Close()

	drugRows, err := s.pool.Query(ctx, `
		SELECT m.drug_id, d.name, COALESCE(SUM(m.quantity), 0)
		FROM his_stock_movements m
		JOIN his_drugs d ON d.id = m.drug_id
		WHERE m.tenant_id = $1 AND m.move_type = 'dispense' AND m.created_at::date BETWEEN $2::date AND $3::date
		GROUP BY m.drug_id, d.name ORDER BY 3 DESC LIMIT 10`, tenantID, dateFrom, dateTo)
	if err != nil {
		return summary, err
	}
	defer drugRows.Close()
	for drugRows.Next() {
		var item DrugConsumptionStat
		if err := drugRows.Scan(&item.DrugID, &item.DrugName, &item.Quantity); err != nil {
			return summary, err
		}
		summary.TopDrugs = append(summary.TopDrugs, item)
	}
	drugRows.Close()

	return summary, nil
}

// ---------------------------------------------------------------------------
// 开放接口（挂号小程序/H5）

// ListOpenClients 开放客户端列表。
func (s *Store) ListOpenClients(ctx context.Context, tenantID int64) ([]OpenClient, error) {
	rows, err := s.pool.Query(ctx, `
		SELECT id, name, scopes, status, rate_limit, created_at
		FROM his_api_clients WHERE tenant_id = $1 ORDER BY id`, tenantID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	items := make([]OpenClient, 0)
	for rows.Next() {
		var item OpenClient
		if err := rows.Scan(&item.ID, &item.Name, &item.Scopes, &item.Status, &item.RateLimit, &item.CreatedAt); err != nil {
			return nil, err
		}
		items = append(items, item)
	}
	return items, rows.Err()
}

// CreateOpenClient 创建客户端（存哈希）。
func (s *Store) CreateOpenClient(ctx context.Context, tenantID int64, req CreateOpenClientRequest, apiKeyHash string) (OpenClient, error) {
	scopes := req.Scopes
	if scopes == "" {
		scopes = "read"
	}
	rateLimit := req.RateLimit
	if rateLimit <= 0 {
		rateLimit = 60
	}
	var item OpenClient
	err := s.pool.QueryRow(ctx, `
		INSERT INTO his_api_clients (tenant_id, name, api_key_hash, scopes, rate_limit)
		VALUES ($1, $2, $3, $4, $5)
		RETURNING id, name, scopes, status, rate_limit, created_at`,
		tenantID, req.Name, apiKeyHash, scopes, rateLimit).
		Scan(&item.ID, &item.Name, &item.Scopes, &item.Status, &item.RateLimit, &item.CreatedAt)
	return item, err
}

// UpdateOpenClient 更新客户端。
func (s *Store) UpdateOpenClient(ctx context.Context, tenantID, id int64, req UpdateOpenClientRequest) (OpenClient, error) {
	sets := []string{}
	args := []any{tenantID, id}
	add := func(column string, value any) {
		args = append(args, value)
		sets = append(sets, fmt.Sprintf("%s = COALESCE($%d, %s)", column, len(args), column))
	}
	if req.Status != nil {
		add("status", req.Status)
	}
	if req.RateLimit != nil {
		add("rate_limit", req.RateLimit)
	}
	if len(sets) == 0 {
		return OpenClient{}, pgx.ErrNoRows
	}
	var item OpenClient
	err := s.pool.QueryRow(ctx, fmt.Sprintf(`
		UPDATE his_api_clients SET %s WHERE tenant_id = $1 AND id = $2
		RETURNING id, name, scopes, status, rate_limit, created_at`,
		strings.Join(sets, ", ")), args...).
		Scan(&item.ID, &item.Name, &item.Scopes, &item.Status, &item.RateLimit, &item.CreatedAt)
	return item, err
}

// ResolveOpenClient 按 API Key 哈希解析客户端与租户。
func (s *Store) ResolveOpenClient(ctx context.Context, apiKeyHash string) (OpenClient, int64, error) {
	var item OpenClient
	var tenantID int64
	err := s.pool.QueryRow(ctx, `
		SELECT id, tenant_id, name, scopes, status, rate_limit, created_at
		FROM his_api_clients WHERE api_key_hash = $1`, apiKeyHash).
		Scan(&item.ID, &tenantID, &item.Name, &item.Scopes, &item.Status, &item.RateLimit, &item.CreatedAt)
	if err != nil {
		return item, 0, err
	}
	return item, tenantID, nil
}

// ListOpenDepartments 科室列表（复用 departments）。
func (s *Store) ListOpenDepartments(ctx context.Context, tenantID int64) ([]OpenDepartment, error) {
	rows, err := s.pool.Query(ctx, `
		SELECT id, name FROM departments
		WHERE tenant_id = $1 AND deleted_at IS NULL AND COALESCE(is_active, true)
		ORDER BY id`, tenantID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	items := make([]OpenDepartment, 0)
	for rows.Next() {
		var item OpenDepartment
		if err := rows.Scan(&item.ID, &item.Name); err != nil {
			return nil, err
		}
		items = append(items, item)
	}
	return items, rows.Err()
}

// ListOpenDoctors 医生列表（his_doctors + employees + departments）。
func (s *Store) ListOpenDoctors(ctx context.Context, tenantID int64) ([]OpenDoctor, error) {
	rows, err := s.pool.Query(ctx, `
		SELECT d.id, COALESCE(e.full_name, e.name, e.username, ''), d.title, d.department_id,
		       COALESCE(dep.name, '')
		FROM his_doctors d
		LEFT JOIN employees e ON e.id = d.employee_id
		LEFT JOIN departments dep ON dep.id = d.department_id
		WHERE d.tenant_id = $1 AND d.status = 'active'
		ORDER BY d.id`, tenantID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	items := make([]OpenDoctor, 0)
	for rows.Next() {
		var item OpenDoctor
		if err := rows.Scan(&item.DoctorID, &item.Name, &item.Title, &item.DepartmentID, &item.DepartmentName); err != nil {
			return nil, err
		}
		items = append(items, item)
	}
	return items, rows.Err()
}

// ListOpenSchedules 排班 + 号源（供患者端选择）。
func (s *Store) ListOpenSchedules(ctx context.Context, tenantID int64, date string, departmentID, doctorID int64) ([]OpenSchedule, error) {
	schedules, err := s.ListSchedules(ctx, tenantID, scheduleQuery{
		DateFrom: date, DateTo: date, DoctorID: doctorID, DepartmentID: departmentID,
	})
	if err != nil {
		return nil, err
	}
	items := make([]OpenSchedule, 0)
	for _, sched := range schedules {
		if sched.Status != "open" {
			continue
		}
		var doctorName, departmentName string
		_ = s.pool.QueryRow(ctx, `
			SELECT COALESCE(e.full_name, e.name, e.username, ''), COALESCE(dep.name, '')
			FROM his_doctors d
			LEFT JOIN employees e ON e.id = d.employee_id
			LEFT JOIN departments dep ON dep.id = d.department_id
			WHERE d.id = $1`, sched.DoctorID).Scan(&doctorName, &departmentName)
		slots, err := s.ListSlots(ctx, tenantID, sched.ID)
		if err != nil {
			return nil, err
		}
		items = append(items, OpenSchedule{Schedule: sched, DoctorName: doctorName, DepartmentName: departmentName, Slots: slots})
	}
	return items, nil
}

// ReleaseExpiredReservations 释放超时未确认的预约号源（P1：无 cron，机会式执行）。
func (s *Store) ReleaseExpiredReservations(ctx context.Context, tenantID int64, timeout time.Duration) error {
	_, err := s.pool.Exec(ctx, `
		UPDATE his_slots SET status = 'free', registration_id = NULL, updated_at = NOW()
		WHERE registration_id IN (
			SELECT id FROM his_registrations
			WHERE tenant_id = $1 AND status = 'reserved'
			  AND created_at < NOW() - make_interval(secs => $2)
		)`, tenantID, int64(timeout.Seconds()))
	if err != nil {
		return err
	}
	_, err = s.pool.Exec(ctx, `
		UPDATE his_registrations SET status = 'expired', updated_at = NOW()
		WHERE tenant_id = $1 AND status = 'reserved'
		  AND created_at < NOW() - make_interval(secs => $2)`,
		tenantID, int64(timeout.Seconds()))
	return err
}

// FindOrCreateOpenCustomer 患者端按手机号建档（预约阶段不发病案号，到院确认再发）。
func (s *Store) FindOrCreateOpenCustomer(ctx context.Context, tenantID int64, name, phone string) (int64, error) {
	id, err := s.FindCustomerByPhone(ctx, tenantID, phone)
	if err == nil {
		return id, nil
	}
	if err != pgx.ErrNoRows {
		return 0, err
	}
	return s.CreateCustomer(ctx, tenantID, 0, RegisterPatientRequest{Name: name, Phone: phone, Allergy: "无"})
}

// LoadOpenAppointment 开放接口挂号单状态。
func (s *Store) LoadOpenAppointment(ctx context.Context, tenantID, registrationID int64) (OpenAppointment, error) {
	var item OpenAppointment
	err := s.pool.QueryRow(ctx, `
		SELECT r.id, r.slot_id, COALESCE(to_char(sl.time_start, 'HH24:MI'), ''),
		       COALESCE(to_char(sl.time_end, 'HH24:MI'), ''), r.visit_date::text, r.status,
		       COALESCE(e.full_name, e.name, e.username, ''), COALESCE(dep.name, '')
		FROM his_registrations r
		LEFT JOIN his_slots sl ON sl.id = r.slot_id
		LEFT JOIN his_doctors d ON d.id = r.doctor_id
		LEFT JOIN employees e ON e.id = d.employee_id
		LEFT JOIN departments dep ON dep.id = r.department_id
		WHERE r.tenant_id = $1 AND r.id = $2`, tenantID, registrationID).
		Scan(&item.RegistrationID, &item.SlotID, &item.SlotTimeStart, &item.SlotTimeEnd,
			&item.VisitDate, &item.Status, &item.DoctorName, &item.DepartmentName)
	if err != nil {
		return item, err
	}
	return item, nil
}

// CancelAppointment 患者取消预约：释放号源，挂号单标记 cancelled。
func (s *Store) CancelAppointment(ctx context.Context, tenantID, id int64) (Registration, error) {
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return Registration{}, err
	}
	defer tx.Rollback(ctx)

	var status string
	err = tx.QueryRow(ctx, `
		SELECT status FROM his_registrations WHERE tenant_id = $1 AND id = $2
		FOR UPDATE`, tenantID, id).Scan(&status)
	if err != nil {
		if err == pgx.ErrNoRows {
			return Registration{}, ErrNotFound
		}
		return Registration{}, err
	}
	if status != "reserved" {
		return Registration{}, ErrInvalidState
	}
	if _, err := tx.Exec(ctx, `
		UPDATE his_registrations SET status = 'cancelled', updated_at = NOW()
		WHERE id = $1`, id); err != nil {
		return Registration{}, err
	}
	if _, err := tx.Exec(ctx, `
		UPDATE his_slots SET status = 'free', registration_id = NULL
		WHERE registration_id = $1`, id); err != nil {
		return Registration{}, err
	}
	if err := tx.Commit(ctx); err != nil {
		return Registration{}, err
	}
	return s.loadRegistration(ctx, s.pool, tenantID, id)
}

// ---------------------------------------------------------------------------
// 员工角色（admin/doctor/pharmacist/cashier/nurse，可叠加）

var hisRoleSet = map[string]bool{"admin": true, "doctor": true, "pharmacist": true, "cashier": true, "nurse": true}

// ListStaffRoles 员工角色列表（含未分配员工，供管理台配置）。
func (s *Store) ListStaffRoles(ctx context.Context, tenantID int64) ([]StaffRole, error) {
	rows, err := s.pool.Query(ctx, `
		SELECT e.id, COALESCE(NULLIF(NULLIF(e.full_name, 'unknown'), ''), NULLIF(NULLIF(e.name, 'unknown'), ''), COALESCE(e.username, ''), COALESCE(e.phone, ''), '员工' || e.id),
		       COALESCE(array_agg(r.role ORDER BY r.role) FILTER (WHERE r.role IS NOT NULL), '{}')
		FROM employees e
		LEFT JOIN his_staff_roles r ON r.employee_id = e.id AND r.tenant_id = $1
		WHERE e.tenant_id = $1 AND e.deleted_at IS NULL
		GROUP BY e.id, e.full_name, e.name, e.username, e.phone
		ORDER BY e.id`, tenantID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	items := make([]StaffRole, 0)
	for rows.Next() {
		var item StaffRole
		if err := rows.Scan(&item.EmployeeID, &item.EmployeeName, &item.Roles); err != nil {
			return nil, err
		}
		items = append(items, item)
	}
	return items, rows.Err()
}

// ReplaceStaffRoles 覆盖式设置员工角色。
func (s *Store) ReplaceStaffRoles(ctx context.Context, tenantID, employeeID int64, roles []string) error {
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)

	var exists bool
	if err := tx.QueryRow(ctx, `
		SELECT EXISTS(SELECT 1 FROM employees WHERE id = $1 AND tenant_id = $2 AND deleted_at IS NULL)`,
		employeeID, tenantID).Scan(&exists); err != nil {
		return err
	}
	if !exists {
		return ErrNotFound
	}
	if _, err := tx.Exec(ctx, `DELETE FROM his_staff_roles WHERE tenant_id = $1 AND employee_id = $2`, tenantID, employeeID); err != nil {
		return err
	}
	for _, role := range roles {
		if !hisRoleSet[role] {
			return ErrInvalidRequest
		}
		if _, err := tx.Exec(ctx, `
			INSERT INTO his_staff_roles (tenant_id, employee_id, role) VALUES ($1, $2, $3)`,
			tenantID, employeeID, role); err != nil {
			return err
		}
	}
	return tx.Commit(ctx)
}

// MyRoles 当前员工的角色列表。
func (s *Store) MyRoles(ctx context.Context, tenantID, employeeID int64) ([]string, error) {
	rows, err := s.pool.Query(ctx, `
		SELECT role FROM his_staff_roles WHERE tenant_id = $1 AND employee_id = $2 ORDER BY role`,
		tenantID, employeeID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	roles := make([]string, 0)
	for rows.Next() {
		var role string
		if err := rows.Scan(&role); err != nil {
			return nil, err
		}
		roles = append(roles, role)
	}
	return roles, rows.Err()
}

// HasAnyRole 判断员工是否拥有任一指定角色。
func (s *Store) HasAnyRole(ctx context.Context, tenantID, employeeID int64, roles ...string) (bool, error) {
	var count int
	err := s.pool.QueryRow(ctx, `
		SELECT COUNT(*) FROM his_staff_roles
		WHERE tenant_id = $1 AND employee_id = $2 AND role = ANY($3::text[])`,
		tenantID, employeeID, roles).Scan(&count)
	return count > 0, err
}

// TenantHasRoleAssignments 租户是否已配置过任何角色（未配置时前端全开放，便于首用）。
func (s *Store) TenantHasRoleAssignments(ctx context.Context, tenantID int64) (bool, error) {
	var exists bool
	err := s.pool.QueryRow(ctx, `
		SELECT EXISTS(SELECT 1 FROM his_staff_roles WHERE tenant_id = $1)`, tenantID).Scan(&exists)
	return exists, err
}

// SearchDiagnosisCatalog 检索 ICD-10 诊断目录（名称模糊 / 编码前缀）。
func (s *Store) SearchDiagnosisCatalog(ctx context.Context, keyword string, limit int) ([]ICDDiagnosis, error) {
	if limit <= 0 || limit > 50 {
		limit = 20
	}
	kw := strings.TrimSpace(keyword)
	if kw == "" {
		return []ICDDiagnosis{}, nil
	}
	rows, err := s.pool.Query(ctx, `
		SELECT diagnosis_code, diagnosis_name
		FROM emr_icd10_diagnosis_catalog
		WHERE is_active
		  AND (diagnosis_name ILIKE $1 OR normalized_name ILIKE $1 OR diagnosis_code ILIKE $2)
		ORDER BY CASE WHEN diagnosis_code ILIKE $2 THEN 0 ELSE 1 END, diagnosis_code
		LIMIT $3`, "%"+kw+"%", kw+"%", limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	items := make([]ICDDiagnosis, 0, limit)
	for rows.Next() {
		var item ICDDiagnosis
		if err := rows.Scan(&item.Code, &item.Name); err != nil {
			return nil, err
		}
		items = append(items, item)
	}
	return items, rows.Err()
}

// TemplateFields 读取模板字段（复用 emr_template_fields + emr_fields，HIS 只读不写）。
func (s *Store) TemplateFields(ctx context.Context, tenantID int64, templateID string) ([]TemplateField, error) {
	rows, err := s.pool.Query(ctx, `
		SELECT f.code, f.name, tf.is_visible, tf.is_required, f.content_category,
		       COALESCE(f.input_type, '长文本'), COALESCE(f.description, ''), f.options_json
		FROM emr_template_fields tf
		JOIN emr_fields f ON f.id = tf.field_id
		WHERE tf.template_id = $1::uuid
		ORDER BY tf.display_order, tf.created_at`,
		templateID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	fields := make([]TemplateField, 0)
	for rows.Next() {
		var field TemplateField
		var optionsRaw []byte
		if err := rows.Scan(&field.Code, &field.Name, &field.IsVisible, &field.IsRequired, &field.Category,
			&field.InputType, &field.Description, &optionsRaw); err != nil {
			return nil, err
		}
		field.Options = parseFieldOptions(optionsRaw)
		fields = append(fields, field)
	}
	return fields, rows.Err()
}

// parseFieldOptions 解析字段选项（options_json 兼容 {"options":[...]} 与 [...])。
func parseFieldOptions(raw []byte) []string {
	if len(raw) == 0 {
		return nil
	}
	var wrapper struct {
		Options []string `json:"options"`
	}
	if err := json.Unmarshal(raw, &wrapper); err == nil && len(wrapper.Options) > 0 {
		return wrapper.Options
	}
	var list []string
	if err := json.Unmarshal(raw, &list); err == nil {
		return list
	}
	var obj map[string]any
	if err := json.Unmarshal(raw, &obj); err == nil {
		if options, ok := obj["options"].([]any); ok {
			out := make([]string, 0, len(options))
			for _, option := range options {
				switch value := option.(type) {
				case string:
					out = append(out, value)
				case map[string]any:
					if label, ok := value["label"].(string); ok {
						out = append(out, label)
					} else if name, ok := value["name"].(string); ok {
						out = append(out, name)
					}
				}
			}
			return out
		}
	}
	return nil
}

// RoleModules 返回某角色可见模块：优先租户自定义，缺省用 tenant_id=0 模板。
func (s *Store) RoleModules(ctx context.Context, tenantID int64, role string) ([]string, error) {
	rows, err := s.pool.Query(ctx, `
		SELECT module FROM his_role_modules
		WHERE role = $1 AND tenant_id IN ($2, 0)
		ORDER BY (tenant_id = $2) DESC`, role, tenantID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	modules := make([]string, 0)
	for rows.Next() {
		var module string
		if err := rows.Scan(&module); err != nil {
			return nil, err
		}
		modules = append(modules, module)
	}
	return modules, rows.Err()
}

// ModulesForEmployee 员工（角色并集）可见模块；admin 返回全部。
func (s *Store) ModulesForEmployee(ctx context.Context, tenantID, employeeID int64) ([]string, error) {
	has, err := s.HasAnyRole(ctx, tenantID, employeeID, "admin")
	if err != nil {
		return nil, err
	}
	if has {
		return []string{"home", "register", "clinic", "billing", "pharmacy", "execute", "lab", "check", "inventory", "purchase", "patients", "stats", "admin"}, nil
	}
	rows, err := s.pool.Query(ctx, `
		SELECT DISTINCT rm.module
		FROM his_staff_roles sr
		JOIN his_role_modules rm ON rm.role = sr.role AND rm.tenant_id IN (sr.tenant_id, 0)
		WHERE sr.tenant_id = $1 AND sr.employee_id = $2
		ORDER BY rm.module`, tenantID, employeeID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	modules := make([]string, 0)
	for rows.Next() {
		var module string
		if err := rows.Scan(&module); err != nil {
			return nil, err
		}
		modules = append(modules, module)
	}
	return modules, rows.Err()
}

// RoleModuleRow 角色模块行。
type RoleModuleRow struct {
	Role   string
	Module string
}

// RoleModuleRows 读取租户 0 模板的全部角色模块行。
func (s *Store) RoleModuleRows(ctx context.Context) ([]RoleModuleRow, error) {
	rows, err := s.pool.Query(ctx, `SELECT role, module FROM his_role_modules WHERE tenant_id = 0 ORDER BY role, module`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := make([]RoleModuleRow, 0)
	for rows.Next() {
		var row RoleModuleRow
		if err := rows.Scan(&row.Role, &row.Module); err != nil {
			return nil, err
		}
		out = append(out, row)
	}
	return out, rows.Err()
}

// ListEmployeesWithRoles 员工 + 角色 + 医生档案（仅本租户、未删除）。
func (s *Store) ListEmployeesWithRoles(ctx context.Context, tenantID int64) ([]EmployeeWithRoles, error) {
	rows, err := s.pool.Query(ctx, `
		SELECT e.id,
		       COALESCE(NULLIF(e.full_name, ''), e.name, ''),
		       COALESCE(e.phone, ''), COALESCE(e.email, ''), COALESCE(e.is_active, 1) = 1,
		       COALESCE(array_agg(DISTINCT sr.role) FILTER (WHERE sr.role IS NOT NULL), '{}') AS roles,
		       d.id, COALESCE(d.title, ''), COALESCE(d.license_no, ''), COALESCE(d.is_prescribing, false), COALESCE(d.status, '')
		FROM employees e
		LEFT JOIN his_staff_roles sr ON sr.tenant_id = e.tenant_id AND sr.employee_id = e.id
		LEFT JOIN his_doctors d ON d.tenant_id = e.tenant_id AND d.employee_id = e.id
		WHERE e.tenant_id = $1 AND e.deleted_at IS NULL
		GROUP BY e.id, e.full_name, e.name, e.phone, e.email, e.is_active, d.id, d.title, d.license_no, d.is_prescribing, d.status
		ORDER BY e.id`, tenantID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := make([]EmployeeWithRoles, 0)
	for rows.Next() {
		var row EmployeeWithRoles
		if err := rows.Scan(&row.ID, &row.Name, &row.Phone, &row.Email, &row.IsActive, &row.Roles,
			&row.DoctorID, &row.Title, &row.LicenseNo, &row.Prescribe, &row.Doctor); err != nil {
			return nil, err
		}
		out = append(out, row)
	}
	return out, rows.Err()
}

// SearchPatients 挂号查重：手机号 / 姓名模糊匹配，返回客户与病案号。
func (s *Store) SearchPatients(ctx context.Context, tenantID int64, keyword string) ([]PatientSearchRow, error) {
	rows, err := s.pool.Query(ctx, `
		SELECT c.id, COALESCE(c.name, ''), COALESCE(c.phone, ''), COALESCE(c.gender, ''), COALESCE(c.age, 0),
		       COALESCE(pi.value, '')
		FROM customers c
		LEFT JOIN patient_identifiers pi
		  ON pi.tenant_id = c.tenant_id AND pi.customer_id = c.id AND pi.identifier_type = 'mrn'
		WHERE c.tenant_id = $1 AND (c.phone ILIKE $2 OR c.name ILIKE $2)
		ORDER BY c.id DESC
		LIMIT 10`, tenantID, "%"+keyword+"%")
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := make([]PatientSearchRow, 0)
	for rows.Next() {
		var row PatientSearchRow
		if err := rows.Scan(&row.ID, &row.Name, &row.Phone, &row.Gender, &row.Age, &row.MRN); err != nil {
			return nil, err
		}
		out = append(out, row)
	}
	return out, rows.Err()
}
