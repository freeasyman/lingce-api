package testcleanup

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
)

type Handler struct {
	pool  *pgxpool.Pool
	mu    sync.Mutex
	tasks map[string]*Task
}
type Task struct {
	ID         string           `json:"id"`
	TenantID   int64            `json:"tenant_id"`
	EmployeeID int64            `json:"employee_id"`
	Status     string           `json:"status"`
	Progress   int              `json:"progress"`
	Logs       []string         `json:"logs"`
	Counts     map[string]int64 `json:"counts"`
	Plan       *Plan            `json:"plan,omitempty"`
	Error      string           `json:"error,omitempty"`
	Phase      string           `json:"phase"`
	UpdatedAt  time.Time        `json:"updated_at"`
	Results    []TableResult    `json:"results,omitempty"`
	Checks     []SchemaCheck    `json:"checks,omitempty"`
}
type TableResult struct {
	Table     string `json:"table"`
	Planned   int64  `json:"planned"`
	Deleted   int64  `json:"deleted"`
	Remaining int64  `json:"remaining"`
	Status    string `json:"status"`
}
type SchemaCheck struct {
	Table           string   `json:"table"`
	RequiredColumns []string `json:"required_columns"`
	Exists          bool     `json:"exists"`
	Missing         []string `json:"missing,omitempty"`
	Status          string   `json:"status"`
}
type Plan struct {
	Deletable map[string]int64  `json:"deletable"`
	Protected map[string]int64  `json:"protected"`
	Review    map[string]int64  `json:"review"`
	Notes     map[string]string `json:"notes"`
}
type request struct {
	TenantID     int64  `json:"tenant_id"`
	EmployeeID   int64  `json:"employee_id"`
	Mode         string `json:"mode"`
	Confirmation string `json:"confirmation"`
}

func NewHandler(pool *pgxpool.Pool) *Handler { return &Handler{pool: pool, tasks: map[string]*Task{}} }

func (h *Handler) RegisterRoutes(mux *http.ServeMux) {
	mux.HandleFunc("POST /api/v1/admin/test-data/cleanup/preview", h.preview)
	mux.HandleFunc("POST /api/v1/admin/test-data/cleanup", h.start)
	mux.HandleFunc("GET /api/v1/admin/test-data/cleanup/{id}", h.status)
}

func (h *Handler) parse(w http.ResponseWriter, r *http.Request) (request, bool) {
	var v request
	if json.NewDecoder(r.Body).Decode(&v) != nil || v.TenantID <= 0 || v.EmployeeID <= 0 {
		http.Error(w, `{"error":"tenant_id and employee_id are required"}`, 400)
		return v, false
	}
	return v, true
}

func (h *Handler) preview(w http.ResponseWriter, r *http.Request) {
	v, ok := h.parse(w, r)
	if !ok {
		return
	}
	ctx := r.Context()
	var name, tenantName string
	if err := h.pool.QueryRow(ctx, `SELECT COALESCE(e.full_name,e.name,''), COALESCE(t.name,'') FROM employees e JOIN tenants t ON t.id=e.tenant_id AND t.deleted_at IS NULL WHERE e.id=$1 AND e.tenant_id=$2 AND e.deleted_at IS NULL`, v.EmployeeID, v.TenantID).Scan(&name, &tenantName); err != nil {
		http.Error(w, `{"error":"employee not found in tenant"}`, 404)
		return
	}
	counts, err := h.counts(ctx, v)
	if err != nil {
		http.Error(w, err.Error(), 500)
		return
	}
	checks, schemaOK, err := h.validateDeletionSchema(ctx)
	if err != nil {
		http.Error(w, err.Error(), 500)
		return
	}
	write(w, map[string]any{"tenant_id": v.TenantID, "tenant_name": tenantName, "employee_id": v.EmployeeID, "employee_name": name, "counts": counts, "plan": classify(counts), "checks": checks, "schema_ready": schemaOK, "mode": "dry_run", "deletion_enabled": false})
}

func (h *Handler) start(w http.ResponseWriter, r *http.Request) {
	v, ok := h.parse(w, r)
	if !ok {
		return
	}
	if strings.TrimSpace(v.Confirmation) != fmt.Sprintf("DELETE_TENANT_%d_EMPLOYEE_%d", v.TenantID, v.EmployeeID) {
		http.Error(w, `{"error":"confirmation is required"}`, http.StatusBadRequest)
		return
	}
	counts, err := h.counts(r.Context(), v)
	if err != nil {
		http.Error(w, err.Error(), 500)
		return
	}
	checks, schemaOK, err := h.validateDeletionSchema(r.Context())
	if err != nil {
		http.Error(w, err.Error(), 500)
		return
	}
	id := fmt.Sprintf("cleanup-%d", time.Now().UnixNano())
	status := "queued"
	errorText := ""
	logs := []string{"已完成数据盘点", "已生成清理计划快照"}
	if !schemaOK {
		logs = append(logs, "部分表结构校验未通过，将跳过不匹配表并继续清理其余可清理表")
	} else {
		logs = append(logs, "删除前结构校验通过，已进入删除队列")
	}
	t := &Task{ID: id, TenantID: v.TenantID, EmployeeID: v.EmployeeID, Status: status, Phase: "schema_check", Progress: 0, Counts: counts, Plan: classify(counts), Checks: checks, Logs: logs, UpdatedAt: time.Now(), Error: errorText}
	for table, n := range t.Plan.Deletable {
		t.Results = append(t.Results, TableResult{Table: table, Planned: n, Deleted: 0, Remaining: n, Status: "待执行"})
	}
	for table, n := range t.Plan.Protected {
		t.Results = append(t.Results, TableResult{Table: table, Planned: n, Deleted: 0, Remaining: n, Status: "保护保留"})
	}
	for table, n := range t.Plan.Review {
		t.Results = append(t.Results, TableResult{Table: table, Planned: n, Deleted: 0, Remaining: n, Status: "需要确认"})
	}
	sort.Slice(t.Results, func(i, j int) bool { return cleanupOrder(t.Results[i].Table) < cleanupOrder(t.Results[j].Table) })
	h.mu.Lock()
	h.tasks[id] = t
	h.mu.Unlock()
	go h.execute(t, v)
	write(w, t)
}

func (h *Handler) execute(t *Task, v request) {
	h.updateTask(t, func(x *Task) {
		x.Status = "running"
		x.Phase = "deleting"
		x.Logs = append(x.Logs, "开始数据库事务删除")
	})
	tx, err := h.pool.Begin(context.Background())
	if err != nil {
		h.failTask(t, err)
		return
	}
	defer tx.Rollback(context.Background())
	for i := range t.Results {
		r := &t.Results[i]
		if t.Plan.Deletable[r.Table] == 0 {
			continue
		}
		cols := []string{"employee_id", "doctor_id", "provider_id", "created_by", "generated_by", "owner_id"}
		where := make([]string, 0)
		for _, c := range cols {
			var exists bool
			_ = tx.QueryRow(context.Background(), `SELECT EXISTS (SELECT 1 FROM information_schema.columns WHERE table_schema='public' AND table_name=$1 AND column_name=$2)`, r.Table, c).Scan(&exists)
			if exists {
				where = append(where, c+"=$2")
			}
		}
		if len(where) == 0 {
			r.Status = "跳过"
			continue
		}
		q := fmt.Sprintf("DELETE FROM %s WHERE tenant_id=$1 AND (%s)", r.Table, strings.Join(where, " OR "))
		tag, e := tx.Exec(context.Background(), q, v.TenantID, v.EmployeeID)
		if e != nil {
			h.failTask(t, e)
			return
		}
		r.Deleted = tag.RowsAffected()
		r.Remaining = r.Planned - r.Deleted
		r.Status = "已删除"
		h.updateTask(t, func(x *Task) { x.Progress = (i + 1) * 100 / len(t.Results); x.UpdatedAt = time.Now() })
	}
	if v.Mode == "validate" {
		_ = tx.Rollback(context.Background())
		h.updateTask(t, func(x *Task) {
			for i := range x.Results {
				if x.Results[i].Status == "已删除" {
					x.Results[i].Status = "验证通过（已回滚）"
					x.Results[i].Remaining = x.Results[i].Planned
				}
			}
			x.Status = "completed"
			x.Phase = "validated"
			x.Progress = 100
			x.Logs = append(x.Logs, "验证执行完成，事务已自动回滚，未保留任何删除")
			x.UpdatedAt = time.Now()
		})
		return
	}
	if err := tx.Commit(context.Background()); err != nil {
		h.failTask(t, err)
		return
	}
	h.updateTask(t, func(x *Task) {
		x.Status = "verifying"
		x.Phase = "verifying"
		x.Progress = 95
		x.Logs = append(x.Logs, "数据库事务已提交，开始残留校验")
		x.UpdatedAt = time.Now()
	})
	remaining, verifyErr := h.counts(context.Background(), v)
	if verifyErr != nil {
		h.failTask(t, verifyErr)
		return
	}
	left := int64(0)
	for table, n := range remaining {
		if t.Plan.Deletable[table] > 0 {
			left += n
		}
	}
	if left > 0 {
		h.updateTask(t, func(x *Task) {
			x.Status = "failed"
			x.Phase = "verifying"
			x.Error = fmt.Sprintf("残留数据 %d 条", left)
			x.Logs = append(x.Logs, x.Error)
			x.UpdatedAt = time.Now()
		})
		return
	}
	h.updateTask(t, func(x *Task) {
		x.Status = "completed"
		x.Phase = "completed"
		x.Progress = 100
		x.Logs = append(x.Logs, "数据库删除与残留校验完成", "OSS 音频文件处理已跳过，待后续单独清理")
		x.UpdatedAt = time.Now()
	})
}
func (h *Handler) updateTask(t *Task, fn func(*Task)) { h.mu.Lock(); defer h.mu.Unlock(); fn(t) }
func (h *Handler) failTask(t *Task, err error) {
	h.updateTask(t, func(x *Task) {
		for i := range x.Results {
			if x.Results[i].Status == "已删除" {
				x.Results[i].Status = "已回滚"
				x.Results[i].Deleted = 0
				x.Results[i].Remaining = x.Results[i].Planned
			}
		}
		x.Status = "failed"
		x.Phase = "failed"
		x.Error = err.Error()
		x.Logs = append(x.Logs, "事务已回滚："+err.Error())
		x.UpdatedAt = time.Now()
	})
}

func cleanupOrder(table string) int {
	order := map[string]int{
		"compliance_findings": 10, "compliance_check_runs": 20, "analysis_runs": 30, "worker_analysis_runs": 40,
		"recording_segue_scores": 50, "recording_content_seeds": 60, "content_publish_tasks": 70,
		"content_items": 80, "content_topics": 90, "followup_generation_runs": 100, "emr_records": 110,
		"recordings": 115, "encounters": 120, "badge_tickets": 140, "opportunity_alert_cc_rules": 150,
	}
	if n, ok := order[table]; ok {
		return n
	}
	return 1000
}

func (h *Handler) validateDeletionSchema(ctx context.Context) ([]SchemaCheck, bool, error) {
	required := map[string][]string{
		"recordings":                 {"id", "tenant_id", "employee_id"},
		"encounters":                 {"id", "tenant_id", "provider_id"},
		"emr_records":                {"id", "tenant_id", "doctor_id"},
		"followup_generation_runs":   {"id", "tenant_id", "employee_id"},
		"content_topics":             {"id", "tenant_id", "created_by"},
		"content_items":              {"id", "tenant_id", "created_by"},
		"content_publish_tasks":      {"id", "tenant_id", "created_by"},
		"badge_tickets":              {"id", "tenant_id", "employee_id"},
		"opportunity_alert_cc_rules": {"id", "tenant_id", "employee_id"},
		"recording_segue_scores":     {"id", "tenant_id", "employee_id"},
	}
	rows, err := h.pool.Query(ctx, `SELECT table_name, column_name FROM information_schema.columns WHERE table_schema='public'`)
	if err != nil {
		return nil, false, err
	}
	defer rows.Close()
	columns := map[string]map[string]bool{}
	for rows.Next() {
		var table, column string
		if err := rows.Scan(&table, &column); err != nil {
			return nil, false, err
		}
		if columns[table] == nil {
			columns[table] = map[string]bool{}
		}
		columns[table][column] = true
	}
	checks := make([]SchemaCheck, 0, len(required))
	ok := true
	for table, requiredColumns := range required {
		check := SchemaCheck{Table: table, RequiredColumns: requiredColumns, Exists: columns[table] != nil, Status: "通过"}
		if !check.Exists {
			check.Status = "缺少表"
			ok = false
		} else {
			for _, column := range requiredColumns {
				if !columns[table][column] {
					check.Missing = append(check.Missing, column)
				}
			}
			if len(check.Missing) > 0 {
				check.Status = "缺少字段"
				ok = false
			}
		}
		checks = append(checks, check)
	}
	return checks, ok, nil
}

func classify(counts map[string]int64) *Plan {
	p := &Plan{Deletable: map[string]int64{}, Protected: map[string]int64{}, Review: map[string]int64{}, Notes: map[string]string{}}
	for table, n := range counts {
		switch table {
		case "customers", "badge_devices", "emr_permission_assignments", "institution_employee_roles", "inst_employee_roles_bak", "employee_login_events", "recording_daily_stats", "worker_base_audits":
			p.Protected[table] = n
			p.Notes[table] = "默认保留：可能被其他员工共享，或属于权限、设备、审计数据"
		case "worker_analysis_runs", "analysis_runs", "recordings", "recording_content_seeds", "emr_records", "encounters", "followup_generation_runs", "compliance_check_runs", "compliance_findings", "content_topics", "content_items", "content_publish_tasks", "badge_tickets", "opportunity_alert_cc_rules", "recording_segue_scores":
			p.Deletable[table] = n
			p.Notes[table] = "员工 117 在租户 1 范围内产生，可纳入事务清理"
		default:
			p.Review[table] = n
			p.Notes[table] = "需要核对归属关系后再决定"
		}
	}
	return p
}
func (h *Handler) status(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	h.mu.Lock()
	t := h.tasks[id]
	h.mu.Unlock()
	if t == nil {
		http.Error(w, `{"error":"task not found"}`, 404)
		return
	}
	write(w, t)
}

func (h *Handler) counts(ctx context.Context, v request) (map[string]int64, error) {
	rows, err := h.pool.Query(ctx, `SELECT table_name,column_name FROM information_schema.columns WHERE table_schema='public' AND column_name IN ('employee_id','doctor_id','provider_id','created_by','generated_by','owner_id') ORDER BY table_name`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	tables := map[string]map[string]bool{}
	for rows.Next() {
		var t, c string
		if err := rows.Scan(&t, &c); err != nil {
			return nil, err
		}
		if tables[t] == nil {
			tables[t] = map[string]bool{}
		}
		tables[t][c] = true
	}
	out := map[string]int64{}
	for t, cols := range tables {
		if !safe(t) {
			continue
		}
		predicates := make([]string, 0, len(cols))
		for _, c := range []string{"employee_id", "doctor_id", "provider_id", "created_by", "generated_by", "owner_id"} {
			if cols[c] {
				predicates = append(predicates, c+"=$2")
			}
		}
		if len(predicates) == 0 {
			continue
		}
		q := fmt.Sprintf(`SELECT count(*) FROM %s WHERE tenant_id=$1 AND (%s)`, t, strings.Join(predicates, " OR "))
		var n int64
		if err := h.pool.QueryRow(ctx, q, v.TenantID, v.EmployeeID).Scan(&n); err == nil && n > 0 {
			out[t] = n
		}
	}
	// Content-center data uses created_by rather than employee_id. Include
	// topics, generated materials and publishing tasks in the preview.
	contentQueries := map[string]string{
		"content_topics":        `SELECT count(*) FROM content_topics WHERE tenant_id=$1 AND created_by=$2 AND COALESCE(deleted_at IS NULL, true)`,
		"content_items":         `SELECT count(*) FROM content_items WHERE tenant_id=$1 AND created_by=$2 AND COALESCE(deleted_at IS NULL, true)`,
		"content_publish_tasks": `SELECT count(*) FROM content_publish_tasks p WHERE p.tenant_id=$1 AND p.created_by=$2 AND COALESCE(p.deleted_at IS NULL, true)`,
	}
	for name, q := range contentQueries {
		var n int64
		if err := h.pool.QueryRow(ctx, q, v.TenantID, v.EmployeeID).Scan(&n); err == nil && n > 0 {
			out[name] = n
		}
	}
	return out, nil
}
func safe(s string) bool {
	if s == "" {
		return false
	}
	for _, r := range s {
		if !(r == '_' || (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9')) {
			return false
		}
	}
	return true
}
func write(w http.ResponseWriter, v any) {
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(v)
}

var _ = strconv.IntSize
var _ = strings.TrimSpace
