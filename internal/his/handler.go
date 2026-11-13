package his

import (
	"encoding/json"
	"log/slog"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/freeasyman/lingce-api/internal/middleware"
	"github.com/freeasyman/lingce-api/internal/router"
	"github.com/freeasyman/lingce-api/internal/tenancy"
	"github.com/freeasyman/lingce-api/pkg/auth"
	"github.com/freeasyman/lingce-api/pkg/httputil"
)

// rateLimiter 开放 API 简单固定窗口限流（按客户端）。
type rateLimiter struct {
	mu   sync.Mutex
	hits map[int64][]time.Time
}

func newRateLimiter() *rateLimiter {
	return &rateLimiter{hits: map[int64][]time.Time{}}
}

// allow 判断是否允许本次请求；perMinute 为该客户端每分钟上限。
func (l *rateLimiter) allow(clientID int64, perMinute int) bool {
	if perMinute <= 0 {
		return true
	}
	now := time.Now()
	cutoff := now.Add(-time.Minute)
	l.mu.Lock()
	defer l.mu.Unlock()
	kept := l.hits[clientID][:0]
	for _, hit := range l.hits[clientID] {
		if hit.After(cutoff) {
			kept = append(kept, hit)
		}
	}
	if len(kept) >= perMinute {
		l.hits[clientID] = kept
		return false
	}
	l.hits[clientID] = append(kept, now)
	return true
}

// Handler 是 HIS 域的 HTTP 入口。
type Handler struct {
	service     *Service
	limiter     *rateLimiter
	importCache *importFileCache
}

// NewHandler 创建 Handler。
func NewHandler(service *Service) *Handler {
	return &Handler{service: service, limiter: newRateLimiter(), importCache: newImportFileCache()}
}

// requireOpenClient 开放 API 鉴权：X-API-Key 哈希匹配 + 限流，返回租户。
func (h *Handler) requireOpenClient(w http.ResponseWriter, r *http.Request) (int64, bool) {
	key := strings.TrimSpace(r.Header.Get("X-API-Key"))
	if key == "" {
		httputil.WriteUnauthorized(w, "missing X-API-Key")
		return 0, false
	}
	client, tenantID, err := h.service.store.ResolveOpenClient(r.Context(), APIKeyHash(key))
	if err != nil {
		httputil.WriteUnauthorized(w, "invalid api key")
		return 0, false
	}
	if client.Status != "active" {
		httputil.WriteForbidden(w, "api client disabled")
		return 0, false
	}
	if !h.limiter.allow(client.ID, client.RateLimit) {
		httputil.WriteError(w, http.StatusTooManyRequests, "rate_limited", "请求过于频繁", nil)
		return 0, false
	}
	return tenantID, true
}

// requireTenant 校验登录态并解析租户，返回 claims 与 tenantID。
func (h *Handler) requireTenant(w http.ResponseWriter, r *http.Request) (*auth.Claims, int64, bool) {
	claims := middleware.GetUserClaims(r.Context())
	if claims == nil {
		httputil.WriteUnauthorized(w, "Invalid token")
		return nil, 0, false
	}
	tenantID, err := tenancy.RequireTenantID(claims, r.URL.Query().Get("tenant_id"))
	if err != nil {
		httputil.WriteBadRequest(w, err.Error())
		return nil, 0, false
	}
	return claims, tenantID, true
}

// requireHisRole 关键操作角色校验（租户未配置角色时放行，首用兼容）。
func (h *Handler) requireModule(w http.ResponseWriter, r *http.Request, tenantID, employeeID int64, module string) bool {
	if err := h.service.RequireModule(r.Context(), tenantID, employeeID, module); err != nil {
		if err == ErrForbiddenRole {
			httputil.WriteForbidden(w, "当前角色无权执行该操作")
			return false
		}
		httputil.WriteInternalError(w, err.Error())
		return false
	}
	return true
}

func (h *Handler) requireHisRole(w http.ResponseWriter, r *http.Request, tenantID, employeeID int64, roles ...string) bool {
	if err := h.service.RequireRole(r.Context(), tenantID, employeeID, roles...); err != nil {
		if err == ErrForbiddenRole {
			httputil.WriteForbidden(w, "当前角色无权执行该操作")
			return false
		}
		httputil.WriteInternalError(w, err.Error())
		return false
	}
	return true
}

func decodeJSON(w http.ResponseWriter, r *http.Request, dst any) bool {
	if err := json.NewDecoder(r.Body).Decode(dst); err != nil {
		httputil.WriteBadRequest(w, "invalid request body")
		return false
	}
	return true
}

func parseIDParam(w http.ResponseWriter, r *http.Request, name string) (int64, bool) {
	id, err := strconv.ParseInt(r.PathValue(name), 10, 64)
	if err != nil || id <= 0 {
		httputil.WriteBadRequest(w, "invalid "+name)
		return 0, false
	}
	return id, true
}

func writeServiceError(w http.ResponseWriter, err error) {
	switch err {
	case ErrInvalidRequest:
		httputil.WriteBadRequest(w, "invalid request")
	case ErrNotFound:
		httputil.WriteNotFound(w, "not found")
	case ErrScheduleHasBookings:
		httputil.WriteBadRequest(w, "排班已有已约/已挂号号源，不能修改时间或间隔；请先处理号源或停诊")
	case ErrScheduleOccupied:
		httputil.WriteBadRequest(w, "排班已有占用号源，不能删除；请先取消相关挂号或预约")
	case ErrScheduleNotOpen:
		httputil.WriteBadRequest(w, "排班未开放（已停诊）")
	case ErrSlotUnavailable:
		httputil.WriteError(w, http.StatusConflict, "slot_unavailable", "号源已被占用或不可用", nil)
	case ErrTreatmentCompleted:
		httputil.WriteError(w, http.StatusConflict, "treatment_completed", "该治疗已完成计划次数，不能重复执行", nil)
	case ErrInvalidState:
		httputil.WriteBadRequest(w, "当前状态不允许该操作")
	case ErrOrderPartiallyPaid:
		httputil.WriteBadRequest(w, "该单已部分收款，请先结清或退费后再调整身份")
	case ErrDispensedCannotRefund:
		httputil.WriteBadRequest(w, "处方已发药，请先退药再退费")
	case ErrRefundOnlyPaid:
		httputil.WriteBadRequest(w, "仅已收费订单可退费；未收费请走作废，欠费不支持退费")
	case ErrRefundQuantityExceeded:
		httputil.WriteBadRequest(w, "退费数量超过可退数量")
	case ErrLabSampledCannotRefund:
		httputil.WriteBadRequest(w, "检验已采样，不能退费")
	case ErrExamStartedCannotRefund:
		httputil.WriteBadRequest(w, "检查已执行，不能退费")
	case ErrTreatmentExecutedCannotRefund:
		httputil.WriteBadRequest(w, "治疗已执行，不能退费")
	case ErrTreatmentUnpaid:
		httputil.WriteBadRequest(w, "请先完成缴费")
	case ErrTreatmentExecuted:
		httputil.WriteBadRequest(w, "治疗已执行，不能取消")
	case ErrInsufficientStock:
		httputil.WriteBadRequest(w, "库存不足，无法发药")
	case ErrSignatureMismatch:
		httputil.WriteBadRequest(w, "文书内容与签名不一致，可能存在改动，禁止归档/打印")
	case ErrPurchaseAlreadyReceived:
		httputil.WriteError(w, http.StatusConflict, "purchase_already_received", "该采购单已入库，不能重复收货", nil)
	case ErrChargeItemCodeExists:
		httputil.WriteError(w, http.StatusConflict, "charge_item_code_exists", "收费项目编码已存在，请换一个编码", nil)
	case ErrDrugDiscountLocked:
		httputil.WriteBadRequest(w, "药品折扣率锁定为 1.00，不能低于 1")
	case ErrInvalidDiscountRate:
		httputil.WriteBadRequest(w, "折扣率需满足 0 < rate ≤ 1")
	case ErrInvalidPriceIdentity:
		httputil.WriteBadRequest(w, "价格身份不合法")
	case ErrNegotiationForbidden:
		httputil.WriteForbidden(w, "议价权限不足：仅管理员/店长可议价，收费员需在上限内")
	case ErrNegotiationReasonRequired:
		httputil.WriteBadRequest(w, "议价必须填写原因")
	case ErrNegotiationExceedsPayable:
		httputil.WriteBadRequest(w, "议价金额不能超过可折行应收")
	case ErrInvalidRole:
		httputil.WriteBadRequest(w, "角色不存在")
	case ErrInvalidModule:
		httputil.WriteBadRequest(w, "模块不存在")
	case ErrEmptyRoleModules:
		httputil.WriteBadRequest(w, "请至少为一个角色保留一个模块")
	case ErrAdminRoleLocked:
		httputil.WriteBadRequest(w, "管理员固定拥有全部模块，不可修改")
	case ErrForbiddenRole:
		httputil.WriteForbidden(w, "当前角色无权执行该操作")
	case ErrInvalidDateRange, ErrInvalidWorkbenchDate:
		httputil.WriteBadRequest(w, err.Error())
	default:
		httputil.WriteInternalError(w, err.Error())
	}
}

// RegisterRoutes 注册 HIS 路由。
func (h *Handler) RegisterRoutes(mux *http.ServeMux, jwtSecret, internalToken string) {
	router.Register(mux, []router.Route{
		// 门户指标与首页工作台
		{Method: "GET", Path: "/api/v1/his/dashboard/metrics", Handler: h.DashboardMetrics, Auth: true, AllowedUserTypes: []string{"admin", "employee"}},
		{Method: "GET", Path: "/api/v1/his/dashboard/workbench", Handler: h.DashboardWorkbench, Auth: true, AllowedUserTypes: []string{"admin", "employee"}},

		// 主数据：医生
		{Method: "GET", Path: "/api/v1/his/master/doctors", Handler: h.ListDoctors, Auth: true, AllowedUserTypes: []string{"admin", "employee"}},
		{Method: "GET", Path: "/api/v1/his/master/employees", Handler: h.ListMasterEmployees, Auth: true, AllowedUserTypes: []string{"admin", "employee"}},
		{Method: "PATCH", Path: "/api/v1/his/master/employees/{id}", Handler: h.UpdateMasterEmployee, Auth: true, AllowedUserTypes: []string{"admin", "employee"}},
		{Method: "POST", Path: "/api/v1/his/master/doctors", Handler: h.CreateDoctor, Auth: true, AllowedUserTypes: []string{"admin", "employee"}},
		{Method: "PATCH", Path: "/api/v1/his/master/doctors/{id}", Handler: h.UpdateDoctor, Auth: true, AllowedUserTypes: []string{"admin", "employee"}},

		// 主数据：房间
		{Method: "GET", Path: "/api/v1/his/master/rooms", Handler: h.ListRooms, Auth: true, AllowedUserTypes: []string{"admin", "employee"}},
		{Method: "POST", Path: "/api/v1/his/master/rooms", Handler: h.CreateRoom, Auth: true, AllowedUserTypes: []string{"admin", "employee"}},
		{Method: "PATCH", Path: "/api/v1/his/master/rooms/{id}", Handler: h.UpdateRoom, Auth: true, AllowedUserTypes: []string{"admin", "employee"}},

		// 主数据：药品
		{Method: "GET", Path: "/api/v1/his/master/drugs", Handler: h.ListDrugs, Auth: true, AllowedUserTypes: []string{"admin", "employee"}},
		{Method: "POST", Path: "/api/v1/his/master/drugs", Handler: h.CreateDrug, Auth: true, AllowedUserTypes: []string{"admin", "employee"}},
		{Method: "PATCH", Path: "/api/v1/his/master/drugs/{id}", Handler: h.UpdateDrug, Auth: true, AllowedUserTypes: []string{"admin", "employee"}},

		// 主数据：原子收费项目
		{Method: "GET", Path: "/api/v1/his/master/charge-items", Handler: h.ListChargeItems, Auth: true, AllowedUserTypes: []string{"admin", "employee"}},
		{Method: "GET", Path: "/api/v1/his/master/charge-items/search", Handler: h.SearchChargeItems, Auth: true, AllowedUserTypes: []string{"admin", "employee"}},
		{Method: "POST", Path: "/api/v1/his/master/charge-items", Handler: h.CreateChargeItem, Auth: true, AllowedUserTypes: []string{"admin", "employee"}},
		{Method: "PATCH", Path: "/api/v1/his/master/charge-items/{id}", Handler: h.UpdateChargeItem, Auth: true, AllowedUserTypes: []string{"admin", "employee"}},
		{Method: "GET", Path: "/api/v1/his/master/charge-items/{id}/links", Handler: h.GetChargeItemLinks, Auth: true, AllowedUserTypes: []string{"admin", "employee"}},
		{Method: "PUT", Path: "/api/v1/his/master/charge-items/{id}/links", Handler: h.UpdateChargeItemLinks, Auth: true, AllowedUserTypes: []string{"admin", "employee"}},

		// 灵活 Excel/CSV 导入（预览 → 列映射 → 提交）
		{Method: "POST", Path: "/api/v1/his/imports/preview", Handler: h.PreviewImport, Auth: true, AllowedUserTypes: []string{"admin", "employee"}},
		{Method: "POST", Path: "/api/v1/his/imports/commit", Handler: h.CommitImport, Auth: true, AllowedUserTypes: []string{"admin", "employee"}},
		{Method: "GET", Path: "/api/v1/his/imports/template", Handler: h.ImportTemplate, Auth: true, AllowedUserTypes: []string{"admin", "employee"}},

		// 主数据：打包产品
		{Method: "GET", Path: "/api/v1/his/master/bundles", Handler: h.ListBundles, Auth: true, AllowedUserTypes: []string{"admin", "employee"}},
		{Method: "GET", Path: "/api/v1/his/master/bundles/{id}", Handler: h.GetBundle, Auth: true, AllowedUserTypes: []string{"admin", "employee"}},
		{Method: "POST", Path: "/api/v1/his/master/bundles", Handler: h.CreateBundle, Auth: true, AllowedUserTypes: []string{"admin", "employee"}},
		{Method: "PATCH", Path: "/api/v1/his/master/bundles/{id}", Handler: h.UpdateBundle, Auth: true, AllowedUserTypes: []string{"admin", "employee"}},

		// 会员/身份折扣
		{Method: "GET", Path: "/api/v1/his/discount-rules", Handler: h.GetDiscountRules, Auth: true, AllowedUserTypes: []string{"admin", "employee"}},
		{Method: "PUT", Path: "/api/v1/his/discount-rules/settings", Handler: h.UpdateDiscountSettings, Auth: true, AllowedUserTypes: []string{"admin", "employee"}},
		{Method: "PUT", Path: "/api/v1/his/discount-rules/{identity}", Handler: h.UpdateDiscountRules, Auth: true, AllowedUserTypes: []string{"admin", "employee"}},

		// 患者档案
		{Method: "GET", Path: "/api/v1/his/patients", Handler: h.ListPatients, Auth: true, AllowedUserTypes: []string{"admin", "employee"}},
		{Method: "PATCH", Path: "/api/v1/his/patients/{customer_id}/identity", Handler: h.UpdatePatientIdentity, Auth: true, AllowedUserTypes: []string{"admin", "employee"}},
		{Method: "GET", Path: "/api/v1/his/patients/{customer_id}/overview", Handler: h.PatientOverview, Auth: true, AllowedUserTypes: []string{"admin", "employee"}},
		{Method: "GET", Path: "/api/v1/his/patients/{customer_id}/visits", Handler: h.PatientVisits, Auth: true, AllowedUserTypes: []string{"admin", "employee"}},

		// 患者建档与病案号
		{Method: "POST", Path: "/api/v1/his/patients/register", Handler: h.RegisterPatient, Auth: true, AllowedUserTypes: []string{"admin", "employee"}},
		{Method: "GET", Path: "/api/v1/his/patients/search", Handler: h.SearchPatients, Auth: true, AllowedUserTypes: []string{"admin", "employee"}},
		{Method: "GET", Path: "/api/v1/his/patients/{customer_id}/identifiers", Handler: h.ListPatientIdentifiers, Auth: true, AllowedUserTypes: []string{"admin", "employee"}},

		// 排班与号源
		{Method: "GET", Path: "/api/v1/his/schedules", Handler: h.ListSchedules, Auth: true, AllowedUserTypes: []string{"admin", "employee"}},
		{Method: "POST", Path: "/api/v1/his/schedules", Handler: h.CreateSchedule, Auth: true, AllowedUserTypes: []string{"admin", "employee"}},
		{Method: "GET", Path: "/api/v1/his/schedules/{id}", Handler: h.GetSchedule, Auth: true, AllowedUserTypes: []string{"admin", "employee"}},
		{Method: "PATCH", Path: "/api/v1/his/schedules/{id}", Handler: h.UpdateSchedule, Auth: true, AllowedUserTypes: []string{"admin", "employee"}},
		{Method: "DELETE", Path: "/api/v1/his/schedules/{id}", Handler: h.DeleteSchedule, Auth: true, AllowedUserTypes: []string{"admin", "employee"}},
		{Method: "GET", Path: "/api/v1/his/schedules/{id}/slots", Handler: h.ListSlots, Auth: true, AllowedUserTypes: []string{"admin", "employee"}},

		// 挂号与分诊队列
		{Method: "GET", Path: "/api/v1/his/registrations", Handler: h.ListRegistrations, Auth: true, AllowedUserTypes: []string{"admin", "employee"}},
		{Method: "POST", Path: "/api/v1/his/registrations", Handler: h.CreateRegistration, Auth: true, AllowedUserTypes: []string{"admin", "employee"}},
		{Method: "POST", Path: "/api/v1/his/registrations/{id}/confirm", Handler: h.ConfirmRegistration, Auth: true, AllowedUserTypes: []string{"admin", "employee"}},
		{Method: "POST", Path: "/api/v1/his/registrations/{id}/refund", Handler: h.RefundRegistration, Auth: true, AllowedUserTypes: []string{"admin", "employee"}},
		{Method: "POST", Path: "/api/v1/his/registrations/{id}/no-show", Handler: h.NoShowRegistration, Auth: true, AllowedUserTypes: []string{"admin", "employee"}},
		{Method: "GET", Path: "/api/v1/his/queue", Handler: h.ListQueue, Auth: true, AllowedUserTypes: []string{"admin", "employee"}},
		{Method: "POST", Path: "/api/v1/his/queue/{registration_id}/start", Handler: h.StartQueueItem, Auth: true, AllowedUserTypes: []string{"admin", "employee"}},
		{Method: "POST", Path: "/api/v1/his/queue/{registration_id}/complete", Handler: h.CompleteQueueItem, Auth: true, AllowedUserTypes: []string{"admin", "employee"}},

		// 医生工作站：病历（复用 emr 域）
		{Method: "GET", Path: "/api/v1/his/workbench/templates", Handler: h.ListTemplates, Auth: true, AllowedUserTypes: []string{"admin", "employee"}},
		{Method: "GET", Path: "/api/v1/his/workbench/context", Handler: h.WorkbenchContext, Auth: true, AllowedUserTypes: []string{"admin", "employee"}},
		{Method: "POST", Path: "/api/v1/his/emr/ensure", Handler: h.EnsureEMRRecord, Auth: true, AllowedUserTypes: []string{"admin", "employee"}},
		{Method: "POST", Path: "/api/v1/his/emr/records/{id}/save", Handler: h.SaveEMRRecord, Auth: true, AllowedUserTypes: []string{"admin", "employee"}},
		{Method: "POST", Path: "/api/v1/his/emr/records/{id}/complete", Handler: h.CompleteEMRRecord, Auth: true, AllowedUserTypes: []string{"admin", "employee"}},
		{Method: "GET", Path: "/api/v1/his/emr/records/{id}/quality/latest", Handler: h.LatestEMRQuality, Auth: true, AllowedUserTypes: []string{"admin", "employee"}},
		{Method: "GET", Path: "/api/v1/his/emr/templates/{id}/fields", Handler: h.TemplateFields, Auth: true, AllowedUserTypes: []string{"admin", "employee"}},

		// 医生工作站：处方
		{Method: "GET", Path: "/api/v1/his/prescriptions", Handler: h.ListPrescriptions, Auth: true, AllowedUserTypes: []string{"admin", "employee"}},
		{Method: "POST", Path: "/api/v1/his/prescriptions", Handler: h.CreatePrescription, Auth: true, AllowedUserTypes: []string{"admin", "employee"}},
		{Method: "POST", Path: "/api/v1/his/prescriptions/{id}/issue", Handler: h.IssuePrescription, Auth: true, AllowedUserTypes: []string{"admin", "employee"}},
		{Method: "PATCH", Path: "/api/v1/his/prescriptions/{id}", Handler: h.UpdatePrescription, Auth: true, AllowedUserTypes: []string{"admin", "employee"}},
		{Method: "POST", Path: "/api/v1/his/prescriptions/{id}/void", Handler: h.VoidPrescription, Auth: true, AllowedUserTypes: []string{"admin", "employee"}},

		// 医生工作站：检查申请与治疗医嘱
		{Method: "GET", Path: "/api/v1/his/service-requests", Handler: h.ListServiceRequests, Auth: true, AllowedUserTypes: []string{"admin", "employee"}},
		{Method: "POST", Path: "/api/v1/his/service-requests", Handler: h.CreateServiceRequest, Auth: true, AllowedUserTypes: []string{"admin", "employee"}},
		{Method: "GET", Path: "/api/v1/his/treatment-orders", Handler: h.ListTreatmentOrders, Auth: true, AllowedUserTypes: []string{"admin", "employee"}},
		{Method: "POST", Path: "/api/v1/his/treatment-orders", Handler: h.CreateTreatmentOrder, Auth: true, AllowedUserTypes: []string{"admin", "employee"}},
		{Method: "POST", Path: "/api/v1/his/treatment-orders/{id}/execute", Handler: h.ExecuteTreatment, Auth: true, AllowedUserTypes: []string{"admin", "employee"}},
		{Method: "POST", Path: "/api/v1/his/treatment-orders/{id}/cancel", Handler: h.CancelTreatmentOrder, Auth: true, AllowedUserTypes: []string{"admin", "employee"}},
		{Method: "POST", Path: "/api/v1/his/treatment-orders/{id}/executions", Handler: h.ExecuteTreatment, Auth: true, AllowedUserTypes: []string{"admin", "employee"}},
		{Method: "GET", Path: "/api/v1/his/treatment-orders/{id}/executions", Handler: h.ListTreatmentExecutions, Auth: true, AllowedUserTypes: []string{"admin", "employee"}},

		// 检验工作站（LIS）
		{Method: "GET", Path: "/api/v1/his/lab/requests", Handler: h.ListLabRequests, Auth: true, AllowedUserTypes: []string{"admin", "employee"}},
		{Method: "POST", Path: "/api/v1/his/lab/requests", Handler: h.CreateLabRequest, Auth: true, AllowedUserTypes: []string{"admin", "employee"}},
		{Method: "GET", Path: "/api/v1/his/lab/requests/{id}", Handler: h.GetLabRequest, Auth: true, AllowedUserTypes: []string{"admin", "employee"}},
		{Method: "POST", Path: "/api/v1/his/lab/requests/{id}/sample", Handler: h.SampleLabRequest, Auth: true, AllowedUserTypes: []string{"admin", "employee"}},
		{Method: "POST", Path: "/api/v1/his/lab/requests/{id}/report", Handler: h.ReportLabRequest, Auth: true, AllowedUserTypes: []string{"admin", "employee"}},
		{Method: "POST", Path: "/api/v1/his/lab/requests/{id}/cancel", Handler: h.CancelLabRequest, Auth: true, AllowedUserTypes: []string{"admin", "employee"}},

		// 检查工作站（PACS）
		{Method: "GET", Path: "/api/v1/his/exam/requests", Handler: h.ListExamRequests, Auth: true, AllowedUserTypes: []string{"admin", "employee"}},
		{Method: "POST", Path: "/api/v1/his/exam/requests", Handler: h.CreateExamRequest, Auth: true, AllowedUserTypes: []string{"admin", "employee"}},
		{Method: "GET", Path: "/api/v1/his/exam/requests/{id}", Handler: h.GetExamRequest, Auth: true, AllowedUserTypes: []string{"admin", "employee"}},
		{Method: "POST", Path: "/api/v1/his/exam/requests/{id}/start", Handler: h.StartExamRequest, Auth: true, AllowedUserTypes: []string{"admin", "employee"}},
		{Method: "POST", Path: "/api/v1/his/exam/requests/{id}/report", Handler: h.ReportExamRequest, Auth: true, AllowedUserTypes: []string{"admin", "employee"}},
		{Method: "POST", Path: "/api/v1/his/exam/requests/{id}/cancel", Handler: h.CancelExamRequest, Auth: true, AllowedUserTypes: []string{"admin", "employee"}},

		// 打包收费
		{Method: "GET", Path: "/api/v1/his/orders", Handler: h.ListOrders, Auth: true, AllowedUserTypes: []string{"admin", "employee"}},
		{Method: "GET", Path: "/api/v1/his/orders/{id}", Handler: h.GetOrder, Auth: true, AllowedUserTypes: []string{"admin", "employee"}},
		{Method: "GET", Path: "/api/v1/his/orders/{id}/reprice-preview", Handler: h.PreviewOrderReprice, Auth: true, AllowedUserTypes: []string{"admin", "employee"}},
		{Method: "POST", Path: "/api/v1/his/orders", Handler: h.CreateOrder, Auth: true, AllowedUserTypes: []string{"admin", "employee"}},
		{Method: "PATCH", Path: "/api/v1/his/orders/{id}", Handler: h.UpdateOrder, Auth: true, AllowedUserTypes: []string{"admin", "employee"}},
		{Method: "POST", Path: "/api/v1/his/orders/{id}/payments", Handler: h.AddPayment, Auth: true, AllowedUserTypes: []string{"admin", "employee"}},
		{Method: "POST", Path: "/api/v1/his/orders/{id}/refund", Handler: h.RefundOrder, Auth: true, AllowedUserTypes: []string{"admin", "employee"}},
		{Method: "POST", Path: "/api/v1/his/orders/{id}/refunds", Handler: h.RefundOrderItems, Auth: true, AllowedUserTypes: []string{"admin", "employee"}},
		{Method: "POST", Path: "/api/v1/his/orders/{id}/void", Handler: h.VoidOrder, Auth: true, AllowedUserTypes: []string{"admin", "employee"}},

		// 药房
		{Method: "GET", Path: "/api/v1/his/pharmacy/prescriptions", Handler: h.ListPharmacyPrescriptions, Auth: true, AllowedUserTypes: []string{"admin", "employee"}},
		{Method: "POST", Path: "/api/v1/his/pharmacy/prescriptions/{id}/dispense", Handler: h.DispensePrescription, Auth: true, AllowedUserTypes: []string{"admin", "employee"}},
		{Method: "GET", Path: "/api/v1/his/pharmacy/stock", Handler: h.ListStock, Auth: true, AllowedUserTypes: []string{"admin", "employee"}},
		{Method: "POST", Path: "/api/v1/his/pharmacy/stock/inbound", Handler: h.InboundStock, Auth: true, AllowedUserTypes: []string{"admin", "employee"}},

		// 采购
		{Method: "GET", Path: "/api/v1/his/purchase/orders", Handler: h.ListPurchaseOrders, Auth: true, AllowedUserTypes: []string{"admin", "employee"}},
		{Method: "POST", Path: "/api/v1/his/purchase/orders", Handler: h.CreatePurchaseOrder, Auth: true, AllowedUserTypes: []string{"admin", "employee"}},
		{Method: "POST", Path: "/api/v1/his/purchase/orders/{id}/receive", Handler: h.ReceivePurchaseOrder, Auth: true, AllowedUserTypes: []string{"admin", "employee"}},
		{Method: "POST", Path: "/api/v1/his/purchase/orders/{id}/cancel", Handler: h.CancelPurchaseOrder, Auth: true, AllowedUserTypes: []string{"admin", "employee"}},

		// 医疗文书
		{Method: "POST", Path: "/api/v1/his/documents/generate", Handler: h.GenerateDocument, Auth: true, AllowedUserTypes: []string{"admin", "employee"}},
		{Method: "GET", Path: "/api/v1/his/documents", Handler: h.ListDocuments, Auth: true, AllowedUserTypes: []string{"admin", "employee"}},
		{Method: "GET", Path: "/api/v1/his/documents/{id}", Handler: h.GetDocument, Auth: true, AllowedUserTypes: []string{"admin", "employee"}},
		{Method: "POST", Path: "/api/v1/his/documents/{id}/signatures", Handler: h.SignDocument, Auth: true, AllowedUserTypes: []string{"admin", "employee"}},
		{Method: "POST", Path: "/api/v1/his/documents/{id}/archive", Handler: h.ArchiveDocument, Auth: true, AllowedUserTypes: []string{"admin", "employee"}},
		{Method: "POST", Path: "/api/v1/his/documents/{id}/print", Handler: h.PrintDocument, Auth: true, AllowedUserTypes: []string{"admin", "employee"}},
		{Method: "POST", Path: "/api/v1/his/followup-documents", Handler: h.SubmitFollowupDocument, Auth: true, AllowedUserTypes: []string{"admin", "employee"}},

		// 基础报表
		{Method: "GET", Path: "/api/v1/his/reports/summary", Handler: h.ReportSummary, Auth: true, AllowedUserTypes: []string{"admin", "employee"}},

		// 统计概览（KPI / 日营收 / 支付方式 / Top 项目 / 挂号状态）
		{Method: "GET", Path: "/api/v1/his/stats/overview", Handler: h.StatsOverview, Auth: true, AllowedUserTypes: []string{"admin", "employee"}},

		// 诊断检索（ICD-10 目录）
		{Method: "GET", Path: "/api/v1/his/icd10", Handler: h.SearchDiagnosis, Auth: true, AllowedUserTypes: []string{"admin", "employee"}},

		// 员工角色（管理台配置 + 当前员工查询）
		{Method: "GET", Path: "/api/v1/his/staff-roles", Handler: h.ListStaffRoles, Auth: true, AllowedUserTypes: []string{"admin", "employee"}},
		{Method: "GET", Path: "/api/v1/his/role-modules", Handler: h.MyModules, Auth: true, AllowedUserTypes: []string{"admin", "employee"}},
		{Method: "GET", Path: "/api/v1/his/role-modules/defaults", Handler: h.DefaultRoleModules, Auth: true, AllowedUserTypes: []string{"admin", "employee"}},
		{Method: "GET", Path: "/api/v1/his/role-modules/config", Handler: h.RoleModuleConfig, Auth: true, AllowedUserTypes: []string{"admin", "employee"}},
		{Method: "PUT", Path: "/api/v1/his/role-modules/{role}", Handler: h.UpdateRoleModules, Auth: true, AllowedUserTypes: []string{"admin", "employee"}},
		{Method: "PUT", Path: "/api/v1/his/staff-roles/{employee_id}", Handler: h.UpdateStaffRoles, Auth: true, AllowedUserTypes: []string{"admin", "employee"}},
		{Method: "GET", Path: "/api/v1/his/my-roles", Handler: h.MyRoles, Auth: true, AllowedUserTypes: []string{"admin", "employee"}},

		// 开放 API 客户端管理（管理台）
		{Method: "GET", Path: "/api/v1/his/open-clients", Handler: h.ListOpenClients, Auth: true, AllowedUserTypes: []string{"admin", "employee"}},
		{Method: "POST", Path: "/api/v1/his/open-clients", Handler: h.CreateOpenClient, Auth: true, AllowedUserTypes: []string{"admin", "employee"}},
		{Method: "PATCH", Path: "/api/v1/his/open-clients/{id}", Handler: h.UpdateOpenClient, Auth: true, AllowedUserTypes: []string{"admin", "employee"}},

		// 开放 API（挂号小程序/H5；X-API-Key 鉴权，不走员工登录）
		{Method: "GET", Path: "/open/his/v1/departments", Handler: h.OpenDepartments},
		{Method: "GET", Path: "/open/his/v1/doctors", Handler: h.OpenDoctors},
		{Method: "GET", Path: "/open/his/v1/schedules", Handler: h.OpenSchedules},
		{Method: "POST", Path: "/open/his/v1/appointments", Handler: h.OpenCreateAppointment},
		{Method: "GET", Path: "/open/his/v1/appointments/{id}", Handler: h.OpenGetAppointment},
		{Method: "POST", Path: "/open/his/v1/appointments/{id}/cancel", Handler: h.OpenCancelAppointment},
	}, router.RouteDeps{
		JWTSecret:     jwtSecret,
		InternalToken: strings.TrimSpace(internalToken),
	})
}

// DashboardMetrics 返回门户卡片指标。
func (h *Handler) DashboardMetrics(w http.ResponseWriter, r *http.Request) {
	_, tenantID, ok := h.requireTenant(w, r)
	if !ok {
		return
	}
	metrics, err := h.service.DashboardMetrics(r.Context(), tenantID)
	if err != nil {
		httputil.WriteInternalError(w, "failed to load his metrics")
		return
	}
	httputil.WriteSuccess(w, metrics)
}

// DashboardWorkbench 返回首页工作台聚合（卡片 / 待办 / 医生汇总）。
func (h *Handler) DashboardWorkbench(w http.ResponseWriter, r *http.Request) {
	_, tenantID, ok := h.requireTenant(w, r)
	if !ok {
		return
	}
	workbench, err := h.service.Workbench(r.Context(), tenantID, r.URL.Query().Get("date"))
	if err != nil {
		writeServiceError(w, err)
		return
	}
	httputil.WriteSuccess(w, workbench)
}

// ListDoctors 返回医生列表。
func (h *Handler) ListDoctors(w http.ResponseWriter, r *http.Request) {
	_, tenantID, ok := h.requireTenant(w, r)
	if !ok {
		return
	}
	doctors, err := h.service.ListDoctors(r.Context(), tenantID)
	if err != nil {
		httputil.WriteInternalError(w, "failed to list doctors")
		return
	}
	httputil.WriteSuccess(w, doctors)
}

// CreateDoctor 新增医生。
func (h *Handler) CreateDoctor(w http.ResponseWriter, r *http.Request) {
	claims, tenantID, ok := h.requireTenant(w, r)
	if !ok {
		return
	}
	if !h.requireHisRole(w, r, tenantID, claims.UserID, "admin") {
		return
	}
	var req CreateDoctorRequest
	if !decodeJSON(w, r, &req) {
		return
	}
	doctor, err := h.service.CreateDoctor(r.Context(), tenantID, req)
	if err != nil {
		writeServiceError(w, err)
		return
	}
	httputil.WriteJSON(w, http.StatusCreated, doctor)
}

// UpdateDoctor 更新医生。
func (h *Handler) UpdateDoctor(w http.ResponseWriter, r *http.Request) {
	claims, tenantID, ok := h.requireTenant(w, r)
	if !ok {
		return
	}
	if !h.requireHisRole(w, r, tenantID, claims.UserID, "admin") {
		return
	}
	id, ok := parseIDParam(w, r, "id")
	if !ok {
		return
	}
	var req UpdateDoctorRequest
	if !decodeJSON(w, r, &req) {
		return
	}
	doctor, err := h.service.UpdateDoctor(r.Context(), tenantID, id, req)
	if err != nil {
		writeServiceError(w, err)
		return
	}
	httputil.WriteSuccess(w, doctor)
}

// ListRooms 返回房间列表。
func (h *Handler) ListRooms(w http.ResponseWriter, r *http.Request) {
	_, tenantID, ok := h.requireTenant(w, r)
	if !ok {
		return
	}
	rooms, err := h.service.ListRooms(r.Context(), tenantID)
	if err != nil {
		httputil.WriteInternalError(w, "failed to list rooms")
		return
	}
	httputil.WriteSuccess(w, rooms)
}

// CreateRoom 新增房间。
func (h *Handler) CreateRoom(w http.ResponseWriter, r *http.Request) {
	claims, tenantID, ok := h.requireTenant(w, r)
	if !ok {
		return
	}
	if !h.requireHisRole(w, r, tenantID, claims.UserID, "admin") {
		return
	}
	var req CreateRoomRequest
	if !decodeJSON(w, r, &req) {
		return
	}
	room, err := h.service.CreateRoom(r.Context(), tenantID, req)
	if err != nil {
		writeServiceError(w, err)
		return
	}
	httputil.WriteJSON(w, http.StatusCreated, room)
}

// UpdateRoom 更新房间。
func (h *Handler) UpdateRoom(w http.ResponseWriter, r *http.Request) {
	claims, tenantID, ok := h.requireTenant(w, r)
	if !ok {
		return
	}
	if !h.requireHisRole(w, r, tenantID, claims.UserID, "admin") {
		return
	}
	id, ok := parseIDParam(w, r, "id")
	if !ok {
		return
	}
	var req UpdateRoomRequest
	if !decodeJSON(w, r, &req) {
		return
	}
	room, err := h.service.UpdateRoom(r.Context(), tenantID, id, req)
	if err != nil {
		writeServiceError(w, err)
		return
	}
	httputil.WriteSuccess(w, room)
}

// ListDrugs 返回药品列表（?keyword= 模糊搜索）。
func (h *Handler) ListDrugs(w http.ResponseWriter, r *http.Request) {
	_, tenantID, ok := h.requireTenant(w, r)
	if !ok {
		return
	}
	drugs, err := h.service.ListDrugs(r.Context(), tenantID, r.URL.Query().Get("keyword"))
	if err != nil {
		httputil.WriteInternalError(w, "failed to list drugs")
		return
	}
	httputil.WriteSuccess(w, drugs)
}

// CreateDrug 新增药品。
func (h *Handler) CreateDrug(w http.ResponseWriter, r *http.Request) {
	claims, tenantID, ok := h.requireTenant(w, r)
	if !ok {
		return
	}
	if !h.requireHisRole(w, r, tenantID, claims.UserID, "admin") {
		return
	}
	var req CreateDrugRequest
	if !decodeJSON(w, r, &req) {
		return
	}
	drug, err := h.service.CreateDrug(r.Context(), tenantID, req)
	if err != nil {
		writeServiceError(w, err)
		return
	}
	httputil.WriteJSON(w, http.StatusCreated, drug)
}

// UpdateDrug 更新药品。
func (h *Handler) UpdateDrug(w http.ResponseWriter, r *http.Request) {
	claims, tenantID, ok := h.requireTenant(w, r)
	if !ok {
		return
	}
	if !h.requireHisRole(w, r, tenantID, claims.UserID, "admin") {
		return
	}
	id, ok := parseIDParam(w, r, "id")
	if !ok {
		return
	}
	var req UpdateDrugRequest
	if !decodeJSON(w, r, &req) {
		return
	}
	drug, err := h.service.UpdateDrug(r.Context(), tenantID, id, req)
	if err != nil {
		writeServiceError(w, err)
		return
	}
	httputil.WriteSuccess(w, drug)
}

// SearchChargeItems 收费项目分页搜索（?keyword=&category=&page=&page_size=）。
func (h *Handler) SearchChargeItems(w http.ResponseWriter, r *http.Request) {
	_, tenantID, ok := h.requireTenant(w, r)
	if !ok {
		return
	}
	page, _ := strconv.Atoi(r.URL.Query().Get("page"))
	pageSize, _ := strconv.Atoi(r.URL.Query().Get("page_size"))
	result, err := h.service.SearchChargeItems(r.Context(), tenantID,
		r.URL.Query().Get("keyword"), r.URL.Query().Get("category"), page, pageSize)
	if err != nil {
		httputil.WriteInternalError(w, "failed to load charge items")
		return
	}
	httputil.WriteSuccess(w, result)
}

// ListChargeItems 返回原子收费项目列表。
func (h *Handler) ListChargeItems(w http.ResponseWriter, r *http.Request) {
	_, tenantID, ok := h.requireTenant(w, r)
	if !ok {
		return
	}
	items, err := h.service.ListChargeItems(r.Context(), tenantID)
	if err != nil {
		httputil.WriteInternalError(w, "failed to list charge items")
		return
	}
	httputil.WriteSuccess(w, items)
}

// CreateChargeItem 新增原子收费项目。
func (h *Handler) CreateChargeItem(w http.ResponseWriter, r *http.Request) {
	claims, tenantID, ok := h.requireTenant(w, r)
	if !ok {
		return
	}
	if !h.requireHisRole(w, r, tenantID, claims.UserID, "admin") {
		return
	}
	var req CreateChargeItemRequest
	if !decodeJSON(w, r, &req) {
		return
	}
	item, err := h.service.CreateChargeItem(r.Context(), tenantID, req)
	if err != nil {
		writeServiceError(w, err)
		return
	}
	httputil.WriteJSON(w, http.StatusCreated, item)
}

// UpdateChargeItem 更新原子收费项目。
func (h *Handler) UpdateChargeItem(w http.ResponseWriter, r *http.Request) {
	claims, tenantID, ok := h.requireTenant(w, r)
	if !ok {
		return
	}
	if !h.requireHisRole(w, r, tenantID, claims.UserID, "admin") {
		return
	}
	id, ok := parseIDParam(w, r, "id")
	if !ok {
		return
	}
	var req UpdateChargeItemRequest
	if !decodeJSON(w, r, &req) {
		return
	}
	item, err := h.service.UpdateChargeItem(r.Context(), tenantID, id, req)
	if err != nil {
		writeServiceError(w, err)
		return
	}
	httputil.WriteSuccess(w, item)
}

// GetChargeItemLinks 收费项目「随项目带出」绑定列表。
func (h *Handler) GetChargeItemLinks(w http.ResponseWriter, r *http.Request) {
	_, tenantID, ok := h.requireTenant(w, r)
	if !ok {
		return
	}
	id, ok := parseIDParam(w, r, "id")
	if !ok {
		return
	}
	links, err := h.service.GetChargeItemLinks(r.Context(), tenantID, id)
	if err != nil {
		writeServiceError(w, err)
		return
	}
	httputil.WriteSuccess(w, links)
}

// UpdateChargeItemLinks 整体覆盖写收费项目绑定（管理页编辑弹窗保存）。
func (h *Handler) UpdateChargeItemLinks(w http.ResponseWriter, r *http.Request) {
	claims, tenantID, ok := h.requireTenant(w, r)
	if !ok {
		return
	}
	if !h.requireHisRole(w, r, tenantID, claims.UserID, "admin") {
		return
	}
	id, ok := parseIDParam(w, r, "id")
	if !ok {
		return
	}
	var req UpdateChargeItemLinksRequest
	if !decodeJSON(w, r, &req) {
		return
	}
	links, err := h.service.UpdateChargeItemLinks(r.Context(), tenantID, id, req)
	if err != nil {
		writeServiceError(w, err)
		return
	}
	httputil.WriteSuccess(w, links)
}

// ListBundles 返回打包产品列表。
func (h *Handler) ListBundles(w http.ResponseWriter, r *http.Request) {
	_, tenantID, ok := h.requireTenant(w, r)
	if !ok {
		return
	}
	bundles, err := h.service.ListBundles(r.Context(), tenantID)
	if err != nil {
		httputil.WriteInternalError(w, "failed to list bundles")
		return
	}
	httputil.WriteSuccess(w, bundles)
}

// GetBundle 返回单个打包产品。
func (h *Handler) GetBundle(w http.ResponseWriter, r *http.Request) {
	_, tenantID, ok := h.requireTenant(w, r)
	if !ok {
		return
	}
	id, ok := parseIDParam(w, r, "id")
	if !ok {
		return
	}
	bundle, err := h.service.GetBundle(r.Context(), tenantID, id)
	if err != nil {
		writeServiceError(w, err)
		return
	}
	httputil.WriteSuccess(w, bundle)
}

// CreateBundle 新增打包产品。
func (h *Handler) CreateBundle(w http.ResponseWriter, r *http.Request) {
	claims, tenantID, ok := h.requireTenant(w, r)
	if !ok {
		return
	}
	if !h.requireHisRole(w, r, tenantID, claims.UserID, "admin") {
		return
	}
	var req CreateBundleRequest
	if !decodeJSON(w, r, &req) {
		return
	}
	bundle, err := h.service.CreateBundle(r.Context(), tenantID, req)
	if err != nil {
		writeServiceError(w, err)
		return
	}
	httputil.WriteJSON(w, http.StatusCreated, bundle)
}

// UpdateBundle 更新打包产品。
func (h *Handler) UpdateBundle(w http.ResponseWriter, r *http.Request) {
	claims, tenantID, ok := h.requireTenant(w, r)
	if !ok {
		return
	}
	if !h.requireHisRole(w, r, tenantID, claims.UserID, "admin") {
		return
	}
	id, ok := parseIDParam(w, r, "id")
	if !ok {
		return
	}
	var req UpdateBundleRequest
	if !decodeJSON(w, r, &req) {
		return
	}
	bundle, err := h.service.UpdateBundle(r.Context(), tenantID, id, req)
	if err != nil {
		writeServiceError(w, err)
		return
	}
	httputil.WriteSuccess(w, bundle)
}

// RegisterPatient 建档 + 病案号发放。
func (h *Handler) RegisterPatient(w http.ResponseWriter, r *http.Request) {
	claims, tenantID, ok := h.requireTenant(w, r)
	if !ok {
		return
	}
	if !h.requireModule(w, r, tenantID, claims.UserID, "register") {
		return
	}
	var req RegisterPatientRequest
	if !decodeJSON(w, r, &req) {
		return
	}
	result, err := h.service.RegisterPatient(r.Context(), tenantID, claims.UserID, req)
	if err != nil {
		writeServiceError(w, err)
		return
	}
	httputil.WriteSuccess(w, result)
}

// ListPatientIdentifiers 返回患者的病案号等标识。
func (h *Handler) ListPatientIdentifiers(w http.ResponseWriter, r *http.Request) {
	_, tenantID, ok := h.requireTenant(w, r)
	if !ok {
		return
	}
	customerID, ok := parseIDParam(w, r, "customer_id")
	if !ok {
		return
	}
	identifiers, err := h.service.ListPatientIdentifiers(r.Context(), tenantID, customerID)
	if err != nil {
		httputil.WriteInternalError(w, "failed to list patient identifiers")
		return
	}
	httputil.WriteSuccess(w, identifiers)
}

// ListSchedules 查询排班（?date_from=&date_to=&doctor_id=&department_id=）。
func (h *Handler) ListSchedules(w http.ResponseWriter, r *http.Request) {
	_, tenantID, ok := h.requireTenant(w, r)
	if !ok {
		return
	}
	q := scheduleQuery{
		DateFrom: r.URL.Query().Get("date_from"),
		DateTo:   r.URL.Query().Get("date_to"),
	}
	if v := r.URL.Query().Get("doctor_id"); v != "" {
		q.DoctorID, _ = strconv.ParseInt(v, 10, 64)
	}
	if v := r.URL.Query().Get("department_id"); v != "" {
		q.DepartmentID, _ = strconv.ParseInt(v, 10, 64)
	}
	schedules, err := h.service.ListSchedules(r.Context(), tenantID, q)
	if err != nil {
		httputil.WriteInternalError(w, "failed to list schedules")
		return
	}
	httputil.WriteSuccess(w, schedules)
}

// CreateSchedule 新增排班并生成号源。
func (h *Handler) CreateSchedule(w http.ResponseWriter, r *http.Request) {
	claims, tenantID, ok := h.requireTenant(w, r)
	if !ok {
		return
	}
	if !h.requireHisRole(w, r, tenantID, claims.UserID, "admin") {
		return
	}
	var req CreateScheduleRequest
	if !decodeJSON(w, r, &req) {
		return
	}
	sched, err := h.service.CreateSchedule(r.Context(), tenantID, req)
	if err != nil {
		writeServiceError(w, err)
		return
	}
	httputil.WriteJSON(w, http.StatusCreated, sched)
}

// GetSchedule 返回单个排班。
func (h *Handler) GetSchedule(w http.ResponseWriter, r *http.Request) {
	_, tenantID, ok := h.requireTenant(w, r)
	if !ok {
		return
	}
	id, ok := parseIDParam(w, r, "id")
	if !ok {
		return
	}
	sched, err := h.service.GetSchedule(r.Context(), tenantID, id)
	if err != nil {
		writeServiceError(w, err)
		return
	}
	httputil.WriteSuccess(w, sched)
}

// UpdateSchedule 更新排班（时间/间隔受已约号源保护）。
func (h *Handler) UpdateSchedule(w http.ResponseWriter, r *http.Request) {
	claims, tenantID, ok := h.requireTenant(w, r)
	if !ok {
		return
	}
	if !h.requireHisRole(w, r, tenantID, claims.UserID, "admin") {
		return
	}
	id, ok := parseIDParam(w, r, "id")
	if !ok {
		return
	}
	var req UpdateScheduleRequest
	if !decodeJSON(w, r, &req) {
		return
	}
	sched, err := h.service.UpdateSchedule(r.Context(), tenantID, id, req)
	if err != nil {
		writeServiceError(w, err)
		return
	}
	httputil.WriteSuccess(w, sched)
}

// DeleteSchedule 删除排班（仅无占用号源时允许，级联删除号源）。
func (h *Handler) DeleteSchedule(w http.ResponseWriter, r *http.Request) {
	claims, tenantID, ok := h.requireTenant(w, r)
	if !ok {
		return
	}
	if !h.requireHisRole(w, r, tenantID, claims.UserID, "admin") {
		return
	}
	id, ok := parseIDParam(w, r, "id")
	if !ok {
		return
	}
	if err := h.service.DeleteSchedule(r.Context(), tenantID, id); err != nil {
		writeServiceError(w, err)
		return
	}
	httputil.WriteSuccess(w, map[string]any{"id": id, "deleted": true})
}

// ListSlots 返回排班号源。
func (h *Handler) ListSlots(w http.ResponseWriter, r *http.Request) {
	_, tenantID, ok := h.requireTenant(w, r)
	if !ok {
		return
	}
	id, ok := parseIDParam(w, r, "id")
	if !ok {
		return
	}
	slots, err := h.service.ListSlots(r.Context(), tenantID, id)
	if err != nil {
		writeServiceError(w, err)
		return
	}
	httputil.WriteSuccess(w, slots)
}

// ListRegistrations 挂号台列表（?date=&status=&department_id=&doctor_id=）。
func (h *Handler) ListRegistrations(w http.ResponseWriter, r *http.Request) {
	_, tenantID, ok := h.requireTenant(w, r)
	if !ok {
		return
	}
	var doctorID, departmentID int64
	if v := r.URL.Query().Get("doctor_id"); v != "" {
		doctorID, _ = strconv.ParseInt(v, 10, 64)
	}
	if v := r.URL.Query().Get("department_id"); v != "" {
		departmentID, _ = strconv.ParseInt(v, 10, 64)
	}
	items, err := h.service.ListRegistrations(r.Context(), tenantID,
		r.URL.Query().Get("date"), r.URL.Query().Get("status"), doctorID, departmentID)
	if err != nil {
		httputil.WriteInternalError(w, "failed to list registrations")
		return
	}
	httputil.WriteSuccess(w, items)
}

// CreateRegistration 创建挂号/预约。
func (h *Handler) CreateRegistration(w http.ResponseWriter, r *http.Request) {
	claims, tenantID, ok := h.requireTenant(w, r)
	if !ok {
		return
	}
	if !h.requireModule(w, r, tenantID, claims.UserID, "billing") {
		return
	}
	var req CreateRegistrationRequest
	if !decodeJSON(w, r, &req) {
		return
	}
	reg, err := h.service.CreateRegistration(r.Context(), tenantID, claims.UserID, req)
	if err != nil {
		writeServiceError(w, err)
		return
	}
	httputil.WriteJSON(w, http.StatusCreated, reg)
}

// ConfirmRegistration 预约到院确认。
func (h *Handler) ConfirmRegistration(w http.ResponseWriter, r *http.Request) {
	claims, tenantID, ok := h.requireTenant(w, r)
	if !ok {
		return
	}
	if !h.requireModule(w, r, tenantID, claims.UserID, "billing") {
		return
	}
	id, ok := parseIDParam(w, r, "id")
	if !ok {
		return
	}
	reg, err := h.service.ConfirmRegistration(r.Context(), tenantID, id)
	if err != nil {
		writeServiceError(w, err)
		return
	}
	httputil.WriteSuccess(w, reg)
}

// RefundRegistration 退号。
func (h *Handler) RefundRegistration(w http.ResponseWriter, r *http.Request) {
	claims, tenantID, ok := h.requireTenant(w, r)
	if !ok {
		return
	}
	if !h.requireModule(w, r, tenantID, claims.UserID, "billing") {
		return
	}
	id, ok := parseIDParam(w, r, "id")
	if !ok {
		return
	}
	reg, err := h.service.RefundRegistration(r.Context(), tenantID, id)
	if err != nil {
		writeServiceError(w, err)
		return
	}
	httputil.WriteSuccess(w, reg)
}

// NoShowRegistration 爽约。
func (h *Handler) NoShowRegistration(w http.ResponseWriter, r *http.Request) {
	claims, tenantID, ok := h.requireTenant(w, r)
	if !ok {
		return
	}
	if !h.requireModule(w, r, tenantID, claims.UserID, "billing") {
		return
	}
	id, ok := parseIDParam(w, r, "id")
	if !ok {
		return
	}
	reg, err := h.service.NoShowRegistration(r.Context(), tenantID, id)
	if err != nil {
		writeServiceError(w, err)
		return
	}
	httputil.WriteSuccess(w, reg)
}

// ListQueue 分诊队列（?date=&doctor_id=&status=）。
func (h *Handler) ListQueue(w http.ResponseWriter, r *http.Request) {
	_, tenantID, ok := h.requireTenant(w, r)
	if !ok {
		return
	}
	var doctorID int64
	if v := r.URL.Query().Get("doctor_id"); v != "" {
		doctorID, _ = strconv.ParseInt(v, 10, 64)
	}
	items, err := h.service.ListQueue(r.Context(), tenantID, r.URL.Query().Get("date"), doctorID, r.URL.Query().Get("status"))
	if err != nil {
		httputil.WriteInternalError(w, "failed to list queue")
		return
	}
	httputil.WriteSuccess(w, items)
}

// StartQueueItem 接诊开始。
func (h *Handler) StartQueueItem(w http.ResponseWriter, r *http.Request) {
	_, tenantID, ok := h.requireTenant(w, r)
	if !ok {
		return
	}
	registrationID, ok := parseIDParam(w, r, "registration_id")
	if !ok {
		return
	}
	item, err := h.service.StartQueueItem(r.Context(), tenantID, registrationID)
	if err != nil {
		writeServiceError(w, err)
		return
	}
	httputil.WriteSuccess(w, item)
}

// CompleteQueueItem 接诊完成。
func (h *Handler) CompleteQueueItem(w http.ResponseWriter, r *http.Request) {
	_, tenantID, ok := h.requireTenant(w, r)
	if !ok {
		return
	}
	registrationID, ok := parseIDParam(w, r, "registration_id")
	if !ok {
		return
	}
	item, err := h.service.CompleteQueueItem(r.Context(), tenantID, registrationID)
	if err != nil {
		writeServiceError(w, err)
		return
	}
	httputil.WriteSuccess(w, item)
}

// ListTemplates 已发布病历模板（?visit_type=&specialty=）。
func (h *Handler) ListTemplates(w http.ResponseWriter, r *http.Request) {
	_, tenantID, ok := h.requireTenant(w, r)
	if !ok {
		return
	}
	items, err := h.service.ListTemplates(r.Context(), tenantID,
		r.URL.Query().Get("visit_type"), r.URL.Query().Get("specialty"))
	if err != nil {
		httputil.WriteInternalError(w, "failed to list templates")
		return
	}
	httputil.WriteSuccess(w, items)
}

// WorkbenchContext 接诊上下文（?encounter_id=）。
func (h *Handler) WorkbenchContext(w http.ResponseWriter, r *http.Request) {
	_, tenantID, ok := h.requireTenant(w, r)
	if !ok {
		return
	}
	encounterID, err := strconv.ParseInt(r.URL.Query().Get("encounter_id"), 10, 64)
	if err != nil || encounterID <= 0 {
		httputil.WriteBadRequest(w, "encounter_id is required")
		return
	}
	ctx2, err := h.service.WorkbenchContext(r.Context(), tenantID, encounterID)
	if err != nil {
		writeServiceError(w, err)
		return
	}
	httputil.WriteSuccess(w, ctx2)
}

// EnsureEMRRecord 确保就诊病历存在。
func (h *Handler) EnsureEMRRecord(w http.ResponseWriter, r *http.Request) {
	claims, tenantID, ok := h.requireTenant(w, r)
	if !ok {
		return
	}
	var req EnsureEMRRequest
	if !decodeJSON(w, r, &req) {
		return
	}
	record, err := h.service.EnsureEMRRecord(r.Context(), tenantID, claims.UserID, req)
	if err != nil {
		writeServiceError(w, err)
		return
	}
	httputil.WriteSuccess(w, record)
}

// SaveEMRRecord 保存病历内容。
func (h *Handler) SaveEMRRecord(w http.ResponseWriter, r *http.Request) {
	claims, tenantID, ok := h.requireTenant(w, r)
	if !ok {
		return
	}
	recordID := strings.TrimSpace(r.PathValue("id"))
	if recordID == "" {
		httputil.WriteBadRequest(w, "record id is required")
		return
	}
	var req SaveEMRRequest
	if !decodeJSON(w, r, &req) {
		return
	}
	record, err := h.service.SaveEMRRecord(r.Context(), tenantID, claims.UserID, recordID, req)
	if err != nil {
		httputil.WriteBadRequest(w, err.Error())
		return
	}
	httputil.WriteSuccess(w, record)
}

// TemplateFields 模板字段（复用 EMR 模板配置，只读）。
func (h *Handler) TemplateFields(w http.ResponseWriter, r *http.Request) {
	_, tenantID, ok := h.requireTenant(w, r)
	if !ok {
		return
	}
	templateID := strings.TrimSpace(r.PathValue("id"))
	if templateID == "" {
		httputil.WriteBadRequest(w, "template id is required")
		return
	}
	fields, err := h.service.TemplateFields(r.Context(), tenantID, templateID)
	if err != nil {
		slog.Error("his: template fields", "error", err)
		httputil.WriteInternalError(w, "failed to load template fields")
		return
	}
	httputil.WriteSuccess(w, fields)
}

// CompleteEMRRecord 完成病历。
func (h *Handler) CompleteEMRRecord(w http.ResponseWriter, r *http.Request) {
	claims, tenantID, ok := h.requireTenant(w, r)
	if !ok {
		return
	}
	recordID := strings.TrimSpace(r.PathValue("id"))
	if recordID == "" {
		httputil.WriteBadRequest(w, "record id is required")
		return
	}
	var req SaveEMRRequest
	if !decodeJSON(w, r, &req) {
		return
	}
	response, err := h.service.CompleteEMRRecord(r.Context(), tenantID, claims.UserID, recordID, req)
	if err != nil && response != nil {
		httputil.WriteSuccess(w, response)
		return
	}
	if err != nil {
		httputil.WriteBadRequest(w, err.Error())
		return
	}
	httputil.WriteSuccess(w, response)
}

// ListPrescriptions 就诊处方列表（?encounter_id=）。
func (h *Handler) ListPrescriptions(w http.ResponseWriter, r *http.Request) {
	_, tenantID, ok := h.requireTenant(w, r)
	if !ok {
		return
	}
	encounterID, err := strconv.ParseInt(r.URL.Query().Get("encounter_id"), 10, 64)
	if err != nil || encounterID <= 0 {
		httputil.WriteBadRequest(w, "encounter_id is required")
		return
	}
	items, err := h.service.ListPrescriptions(r.Context(), tenantID, encounterID)
	if err != nil {
		httputil.WriteInternalError(w, "failed to list prescriptions")
		return
	}
	httputil.WriteSuccess(w, items)
}

// CreatePrescription 开具处方（草稿）。
func (h *Handler) CreatePrescription(w http.ResponseWriter, r *http.Request) {
	claims, tenantID, ok := h.requireTenant(w, r)
	if !ok {
		return
	}
	if !h.requireHisRole(w, r, tenantID, claims.UserID, "doctor") {
		return
	}
	var req CreatePrescriptionRequest
	if !decodeJSON(w, r, &req) {
		return
	}
	item, err := h.service.CreatePrescription(r.Context(), tenantID, req)
	if err != nil {
		writeServiceError(w, err)
		return
	}
	httputil.WriteJSON(w, http.StatusCreated, item)
}

// IssuePrescription 处方开具（生成处方号）。
func (h *Handler) IssuePrescription(w http.ResponseWriter, r *http.Request) {
	claims, tenantID, ok := h.requireTenant(w, r)
	if !ok {
		return
	}
	if !h.requireHisRole(w, r, tenantID, claims.UserID, "doctor") {
		return
	}
	id, ok := parseIDParam(w, r, "id")
	if !ok {
		return
	}
	item, err := h.service.IssuePrescription(r.Context(), tenantID, id)
	if err != nil {
		writeServiceError(w, err)
		return
	}
	httputil.WriteSuccess(w, item)
}

// UpdatePrescription 修改处方明细。
func (h *Handler) UpdatePrescription(w http.ResponseWriter, r *http.Request) {
	claims, tenantID, ok := h.requireTenant(w, r)
	if !ok {
		return
	}
	if !h.requireHisRole(w, r, tenantID, claims.UserID, "doctor") {
		return
	}
	id, ok := parseIDParam(w, r, "id")
	if !ok {
		return
	}
	var req UpdatePrescriptionRequest
	if !decodeJSON(w, r, &req) {
		return
	}
	item, err := h.service.UpdatePrescription(r.Context(), tenantID, id, req)
	if err != nil {
		writeServiceError(w, err)
		return
	}
	httputil.WriteSuccess(w, item)
}

// VoidPrescription 作废处方。
func (h *Handler) VoidPrescription(w http.ResponseWriter, r *http.Request) {
	claims, tenantID, ok := h.requireTenant(w, r)
	if !ok {
		return
	}
	if !h.requireHisRole(w, r, tenantID, claims.UserID, "doctor") {
		return
	}
	id, ok := parseIDParam(w, r, "id")
	if !ok {
		return
	}
	item, err := h.service.VoidPrescription(r.Context(), tenantID, id)
	if err != nil {
		writeServiceError(w, err)
		return
	}
	httputil.WriteSuccess(w, item)
}

// ListServiceRequests 检查申请列表（?encounter_id=）。
func (h *Handler) ListServiceRequests(w http.ResponseWriter, r *http.Request) {
	_, tenantID, ok := h.requireTenant(w, r)
	if !ok {
		return
	}
	encounterID, err := strconv.ParseInt(r.URL.Query().Get("encounter_id"), 10, 64)
	if err != nil || encounterID <= 0 {
		httputil.WriteBadRequest(w, "encounter_id is required")
		return
	}
	items, err := h.service.ListServiceRequests(r.Context(), tenantID, encounterID)
	if err != nil {
		httputil.WriteInternalError(w, "failed to list service requests")
		return
	}
	httputil.WriteSuccess(w, items)
}

// CreateServiceRequest 开检查/检验申请。
func (h *Handler) CreateServiceRequest(w http.ResponseWriter, r *http.Request) {
	claims, tenantID, ok := h.requireTenant(w, r)
	if !ok {
		return
	}
	if !h.requireHisRole(w, r, tenantID, claims.UserID, "doctor") {
		return
	}
	var req CreateServiceRequestRequest
	if !decodeJSON(w, r, &req) {
		return
	}
	item, err := h.service.CreateServiceRequest(r.Context(), tenantID, req)
	if err != nil {
		writeServiceError(w, err)
		return
	}
	httputil.WriteJSON(w, http.StatusCreated, item)
}

// ListTreatmentOrders 治疗医嘱列表。
// 带 encounter_id 时按就诊查询（医生工作站）；否则按执行站页签 status + date 查询。
func (h *Handler) ListTreatmentOrders(w http.ResponseWriter, r *http.Request) {
	_, tenantID, ok := h.requireTenant(w, r)
	if !ok {
		return
	}
	if raw := strings.TrimSpace(r.URL.Query().Get("encounter_id")); raw != "" {
		encounterID, err := strconv.ParseInt(raw, 10, 64)
		if err != nil || encounterID <= 0 {
			httputil.WriteBadRequest(w, "invalid encounter_id")
			return
		}
		items, err := h.service.ListTreatmentOrders(r.Context(), tenantID, encounterID)
		if err != nil {
			httputil.WriteInternalError(w, "failed to list treatment orders")
			return
		}
		httputil.WriteSuccess(w, items)
		return
	}
	items, err := h.service.ListTreatmentStationOrders(r.Context(), tenantID,
		r.URL.Query().Get("status"), r.URL.Query().Get("date"))
	if err != nil {
		writeServiceError(w, err)
		return
	}
	httputil.WriteSuccess(w, items)
}

// CreateTreatmentOrder 开治疗医嘱。
func (h *Handler) CreateTreatmentOrder(w http.ResponseWriter, r *http.Request) {
	claims, tenantID, ok := h.requireTenant(w, r)
	if !ok {
		return
	}
	if !h.requireHisRole(w, r, tenantID, claims.UserID, "doctor") {
		return
	}
	var req CreateTreatmentOrderRequest
	if !decodeJSON(w, r, &req) {
		return
	}
	item, err := h.service.CreateTreatmentOrder(r.Context(), tenantID, claims.UserID, req)
	if err != nil {
		writeServiceError(w, err)
		return
	}
	httputil.WriteJSON(w, http.StatusCreated, item)
}

// CancelTreatmentOrder 取消未执行的治疗医嘱（联动删除对应收费行）。
func (h *Handler) CancelTreatmentOrder(w http.ResponseWriter, r *http.Request) {
	claims, tenantID, ok := h.requireTenant(w, r)
	if !ok {
		return
	}
	if !h.requireHisRole(w, r, tenantID, claims.UserID, "doctor", "nurse") {
		return
	}
	id, ok := parseIDParam(w, r, "id")
	if !ok {
		return
	}
	item, err := h.service.CancelTreatmentOrder(r.Context(), tenantID, id)
	if err != nil {
		writeServiceError(w, err)
		return
	}
	httputil.WriteSuccess(w, item)
}

// ExecuteTreatment 治疗执行登记。
func (h *Handler) ExecuteTreatment(w http.ResponseWriter, r *http.Request) {
	claims, tenantID, ok := h.requireTenant(w, r)
	if !ok {
		return
	}
	if !h.requireHisRole(w, r, tenantID, claims.UserID, "doctor", "nurse") {
		return
	}
	id, ok := parseIDParam(w, r, "id")
	if !ok {
		return
	}
	var req ExecuteTreatmentRequest
	if !decodeJSON(w, r, &req) {
		return
	}
	item, err := h.service.ExecuteTreatment(r.Context(), tenantID, id, claims.UserID, req.Note)
	if err != nil {
		writeServiceError(w, err)
		return
	}
	httputil.WriteSuccess(w, item)
}

// ListTreatmentExecutions 治疗执行历史（第 N 次 / 执行时间 / 执行人 / 备注）。
func (h *Handler) ListTreatmentExecutions(w http.ResponseWriter, r *http.Request) {
	_, tenantID, ok := h.requireTenant(w, r)
	if !ok {
		return
	}
	id, ok := parseIDParam(w, r, "id")
	if !ok {
		return
	}
	items, err := h.service.ListTreatmentExecutions(r.Context(), tenantID, id)
	if err != nil {
		writeServiceError(w, err)
		return
	}
	httputil.WriteSuccess(w, items)
}

// ListLabRequests 检验工作站列表（?status=&date=&keyword=）。
func (h *Handler) ListLabRequests(w http.ResponseWriter, r *http.Request) {
	_, tenantID, ok := h.requireTenant(w, r)
	if !ok {
		return
	}
	query := r.URL.Query()
	items, err := h.service.ListLabRequests(r.Context(), tenantID,
		query.Get("status"), query.Get("date"), query.Get("keyword"))
	if err != nil {
		writeServiceError(w, err)
		return
	}
	httputil.WriteSuccess(w, items)
}

// CreateLabRequest 开检验申请。
func (h *Handler) CreateLabRequest(w http.ResponseWriter, r *http.Request) {
	claims, tenantID, ok := h.requireTenant(w, r)
	if !ok {
		return
	}
	if !h.requireHisRole(w, r, tenantID, claims.UserID, "doctor", "nurse", "labtech") {
		return
	}
	var req CreateLabRequestRequest
	if !decodeJSON(w, r, &req) {
		return
	}
	item, err := h.service.CreateLabRequest(r.Context(), tenantID, claims.UserID, req)
	if err != nil {
		writeServiceError(w, err)
		return
	}
	httputil.WriteJSON(w, http.StatusCreated, item)
}

// GetLabRequest 检验申请详情（含结果项）。
func (h *Handler) GetLabRequest(w http.ResponseWriter, r *http.Request) {
	_, tenantID, ok := h.requireTenant(w, r)
	if !ok {
		return
	}
	id, ok := parseIDParam(w, r, "id")
	if !ok {
		return
	}
	item, err := h.service.GetLabRequest(r.Context(), tenantID, id)
	if err != nil {
		writeServiceError(w, err)
		return
	}
	httputil.WriteSuccess(w, item)
}

// SampleLabRequest 采样（仅待采样）。
func (h *Handler) SampleLabRequest(w http.ResponseWriter, r *http.Request) {
	claims, tenantID, ok := h.requireTenant(w, r)
	if !ok {
		return
	}
	if !h.requireHisRole(w, r, tenantID, claims.UserID, "nurse", "labtech", "doctor") {
		return
	}
	id, ok := parseIDParam(w, r, "id")
	if !ok {
		return
	}
	item, err := h.service.SampleLabRequest(r.Context(), tenantID, id)
	if err != nil {
		writeServiceError(w, err)
		return
	}
	httputil.WriteSuccess(w, item)
}

// ReportLabRequest 录入检验结果（仅已采样；事务内替换结果项）。
func (h *Handler) ReportLabRequest(w http.ResponseWriter, r *http.Request) {
	claims, tenantID, ok := h.requireTenant(w, r)
	if !ok {
		return
	}
	if !h.requireHisRole(w, r, tenantID, claims.UserID, "labtech", "doctor", "nurse") {
		return
	}
	id, ok := parseIDParam(w, r, "id")
	if !ok {
		return
	}
	var req ReportLabRequestRequest
	if !decodeJSON(w, r, &req) {
		return
	}
	item, err := h.service.ReportLabRequest(r.Context(), tenantID, id, req.Items)
	if err != nil {
		writeServiceError(w, err)
		return
	}
	httputil.WriteSuccess(w, item)
}

// CancelLabRequest 取消检验申请（仅待采样）。
func (h *Handler) CancelLabRequest(w http.ResponseWriter, r *http.Request) {
	claims, tenantID, ok := h.requireTenant(w, r)
	if !ok {
		return
	}
	if !h.requireHisRole(w, r, tenantID, claims.UserID, "doctor", "nurse", "labtech") {
		return
	}
	id, ok := parseIDParam(w, r, "id")
	if !ok {
		return
	}
	item, err := h.service.CancelLabRequest(r.Context(), tenantID, id)
	if err != nil {
		writeServiceError(w, err)
		return
	}
	httputil.WriteSuccess(w, item)
}

// ListExamRequests 检查工作站列表（?status=&date=&keyword=）。
func (h *Handler) ListExamRequests(w http.ResponseWriter, r *http.Request) {
	_, tenantID, ok := h.requireTenant(w, r)
	if !ok {
		return
	}
	query := r.URL.Query()
	items, err := h.service.ListExamRequests(r.Context(), tenantID,
		query.Get("status"), query.Get("date"), query.Get("keyword"))
	if err != nil {
		writeServiceError(w, err)
		return
	}
	httputil.WriteSuccess(w, items)
}

// CreateExamRequest 开检查申请。
func (h *Handler) CreateExamRequest(w http.ResponseWriter, r *http.Request) {
	claims, tenantID, ok := h.requireTenant(w, r)
	if !ok {
		return
	}
	if !h.requireHisRole(w, r, tenantID, claims.UserID, "doctor", "nurse", "checktech") {
		return
	}
	var req CreateExamRequestRequest
	if !decodeJSON(w, r, &req) {
		return
	}
	item, err := h.service.CreateExamRequest(r.Context(), tenantID, claims.UserID, req)
	if err != nil {
		writeServiceError(w, err)
		return
	}
	httputil.WriteJSON(w, http.StatusCreated, item)
}

// GetExamRequest 检查申请详情（含报告）。
func (h *Handler) GetExamRequest(w http.ResponseWriter, r *http.Request) {
	_, tenantID, ok := h.requireTenant(w, r)
	if !ok {
		return
	}
	id, ok := parseIDParam(w, r, "id")
	if !ok {
		return
	}
	item, err := h.service.GetExamRequest(r.Context(), tenantID, id)
	if err != nil {
		writeServiceError(w, err)
		return
	}
	httputil.WriteSuccess(w, item)
}

// StartExamRequest 开始检查（仅待检查）。
func (h *Handler) StartExamRequest(w http.ResponseWriter, r *http.Request) {
	claims, tenantID, ok := h.requireTenant(w, r)
	if !ok {
		return
	}
	if !h.requireHisRole(w, r, tenantID, claims.UserID, "checktech", "doctor", "nurse") {
		return
	}
	id, ok := parseIDParam(w, r, "id")
	if !ok {
		return
	}
	item, err := h.service.StartExamRequest(r.Context(), tenantID, id)
	if err != nil {
		writeServiceError(w, err)
		return
	}
	httputil.WriteSuccess(w, item)
}

// ReportExamRequest 录入检查报告（仅检查中；事务内 upsert 报告）。
func (h *Handler) ReportExamRequest(w http.ResponseWriter, r *http.Request) {
	claims, tenantID, ok := h.requireTenant(w, r)
	if !ok {
		return
	}
	if !h.requireHisRole(w, r, tenantID, claims.UserID, "checktech", "doctor") {
		return
	}
	id, ok := parseIDParam(w, r, "id")
	if !ok {
		return
	}
	var req ReportExamRequestRequest
	if !decodeJSON(w, r, &req) {
		return
	}
	item, err := h.service.ReportExamRequest(r.Context(), tenantID, claims.UserID, id, req)
	if err != nil {
		writeServiceError(w, err)
		return
	}
	httputil.WriteSuccess(w, item)
}

// CancelExamRequest 取消检查申请（仅待检查）。
func (h *Handler) CancelExamRequest(w http.ResponseWriter, r *http.Request) {
	claims, tenantID, ok := h.requireTenant(w, r)
	if !ok {
		return
	}
	if !h.requireHisRole(w, r, tenantID, claims.UserID, "doctor", "nurse", "checktech") {
		return
	}
	id, ok := parseIDParam(w, r, "id")
	if !ok {
		return
	}
	item, err := h.service.CancelExamRequest(r.Context(), tenantID, id)
	if err != nil {
		writeServiceError(w, err)
		return
	}
	httputil.WriteSuccess(w, item)
}

// ListOrders 收费单列表（?status=&encounter_id=&customer_id=）。
func (h *Handler) ListOrders(w http.ResponseWriter, r *http.Request) {
	_, tenantID, ok := h.requireTenant(w, r)
	if !ok {
		return
	}
	var encounterID, customerID int64
	if v := r.URL.Query().Get("encounter_id"); v != "" {
		encounterID, _ = strconv.ParseInt(v, 10, 64)
	}
	if v := r.URL.Query().Get("customer_id"); v != "" {
		customerID, _ = strconv.ParseInt(v, 10, 64)
	}
	items, err := h.service.ListOrders(r.Context(), tenantID, r.URL.Query().Get("status"), encounterID, customerID)
	if err != nil {
		httputil.WriteInternalError(w, "failed to list orders")
		return
	}
	httputil.WriteSuccess(w, items)
}

// GetOrder 收费单详情。
func (h *Handler) GetOrder(w http.ResponseWriter, r *http.Request) {
	_, tenantID, ok := h.requireTenant(w, r)
	if !ok {
		return
	}
	id, ok := parseIDParam(w, r, "id")
	if !ok {
		return
	}
	order, err := h.service.GetOrder(r.Context(), tenantID, id)
	if err != nil {
		writeServiceError(w, err)
		return
	}
	httputil.WriteSuccess(w, order)
}

// PreviewOrderReprice 收费前预览按当前身份重算结果（不落库）。
func (h *Handler) PreviewOrderReprice(w http.ResponseWriter, r *http.Request) {
	claims, tenantID, ok := h.requireTenant(w, r)
	if !ok {
		return
	}
	if !h.requireModule(w, r, tenantID, claims.UserID, "billing") {
		return
	}
	id, ok := parseIDParam(w, r, "id")
	if !ok {
		return
	}
	preview, err := h.service.PreviewOrderReprice(r.Context(), tenantID, id)
	if err != nil {
		writeServiceError(w, err)
		return
	}
	httputil.WriteSuccess(w, preview)
}

// GetDiscountRules 身份 × 大类折扣矩阵。
func (h *Handler) GetDiscountRules(w http.ResponseWriter, r *http.Request) {
	_, tenantID, ok := h.requireTenant(w, r)
	if !ok {
		return
	}
	matrix, err := h.service.DiscountMatrix(r.Context(), tenantID)
	if err != nil {
		writeServiceError(w, err)
		return
	}
	httputil.WriteSuccess(w, matrix)
}

// UpdateDiscountRules 覆盖写某身份的大类折率。
func (h *Handler) UpdateDiscountRules(w http.ResponseWriter, r *http.Request) {
	claims, tenantID, ok := h.requireTenant(w, r)
	if !ok {
		return
	}
	if !h.requireModule(w, r, tenantID, claims.UserID, "admin") {
		return
	}
	identity := strings.TrimSpace(r.PathValue("identity"))
	var req struct {
		Rates map[string]float64 `json:"rates"`
	}
	if !decodeJSON(w, r, &req) {
		return
	}
	if err := h.service.UpdateDiscountRules(r.Context(), tenantID, identity, req.Rates); err != nil {
		writeServiceError(w, err)
		return
	}
	matrix, err := h.service.DiscountMatrix(r.Context(), tenantID)
	if err != nil {
		writeServiceError(w, err)
		return
	}
	httputil.WriteSuccess(w, matrix)
}

// UpdateDiscountSettings 设置收费员议价上限。
func (h *Handler) UpdateDiscountSettings(w http.ResponseWriter, r *http.Request) {
	claims, tenantID, ok := h.requireTenant(w, r)
	if !ok {
		return
	}
	if !h.requireModule(w, r, tenantID, claims.UserID, "admin") {
		return
	}
	var req struct {
		CashierMaxNegotiationRate float64 `json:"cashier_max_negotiation_rate"`
	}
	if !decodeJSON(w, r, &req) {
		return
	}
	if err := h.service.UpdateCashierMaxNegotiationRate(r.Context(), tenantID, req.CashierMaxNegotiationRate); err != nil {
		writeServiceError(w, err)
		return
	}
	matrix, err := h.service.DiscountMatrix(r.Context(), tenantID)
	if err != nil {
		writeServiceError(w, err)
		return
	}
	httputil.WriteSuccess(w, matrix)
}

// UpdatePatientIdentity 设置患者价格身份（留痕）。
func (h *Handler) UpdatePatientIdentity(w http.ResponseWriter, r *http.Request) {
	claims, tenantID, ok := h.requireTenant(w, r)
	if !ok {
		return
	}
	if !h.requireModule(w, r, tenantID, claims.UserID, "patients") {
		return
	}
	customerID, ok := parseIDParam(w, r, "customer_id")
	if !ok {
		return
	}
	var req struct {
		PriceIdentity string `json:"price_identity"`
	}
	if !decodeJSON(w, r, &req) {
		return
	}
	if err := h.service.SetPatientPriceIdentity(r.Context(), tenantID, customerID, claims.UserID, strings.TrimSpace(req.PriceIdentity)); err != nil {
		writeServiceError(w, err)
		return
	}
	overview, err := h.service.PatientOverview(r.Context(), tenantID, customerID)
	if err != nil {
		writeServiceError(w, err)
		return
	}
	httputil.WriteSuccess(w, overview)
}

// CreateOrder 划价。
func (h *Handler) CreateOrder(w http.ResponseWriter, r *http.Request) {
	claims, tenantID, ok := h.requireTenant(w, r)
	if !ok {
		return
	}
	if !h.requireModule(w, r, tenantID, claims.UserID, "billing") {
		return
	}
	var req CreateOrderRequest
	if !decodeJSON(w, r, &req) {
		return
	}
	order, err := h.service.CreateOrder(r.Context(), tenantID, claims.UserID, req)
	if err != nil {
		writeServiceError(w, err)
		return
	}
	httputil.WriteJSON(w, http.StatusCreated, order)
}

// UpdateOrder 调整收费单（未收费前）。
func (h *Handler) UpdateOrder(w http.ResponseWriter, r *http.Request) {
	claims, tenantID, ok := h.requireTenant(w, r)
	if !ok {
		return
	}
	if !h.requireModule(w, r, tenantID, claims.UserID, "billing") {
		return
	}
	id, ok := parseIDParam(w, r, "id")
	if !ok {
		return
	}
	var req UpdateOrderRequest
	if !decodeJSON(w, r, &req) {
		return
	}
	order, err := h.service.UpdateOrder(r.Context(), tenantID, id, claims.UserID, req)
	if err != nil {
		writeServiceError(w, err)
		return
	}
	httputil.WriteSuccess(w, order)
}

// AddPayment 收款。
func (h *Handler) AddPayment(w http.ResponseWriter, r *http.Request) {
	claims, tenantID, ok := h.requireTenant(w, r)
	if !ok {
		return
	}
	if !h.requireModule(w, r, tenantID, claims.UserID, "billing") {
		return
	}
	id, ok := parseIDParam(w, r, "id")
	if !ok {
		return
	}
	var req CreatePaymentRequest
	if !decodeJSON(w, r, &req) {
		return
	}
	order, err := h.service.AddPayment(r.Context(), tenantID, claims.UserID, id, req)
	if err != nil {
		writeServiceError(w, err)
		return
	}
	httputil.WriteSuccess(w, order)
}

// RefundOrder 退费。
func (h *Handler) RefundOrder(w http.ResponseWriter, r *http.Request) {
	claims, tenantID, ok := h.requireTenant(w, r)
	if !ok {
		return
	}
	if !h.requireModule(w, r, tenantID, claims.UserID, "billing") {
		return
	}
	id, ok := parseIDParam(w, r, "id")
	if !ok {
		return
	}
	var req RefundOrderRequest
	if !decodeJSON(w, r, &req) {
		return
	}
	order, err := h.service.RefundOrder(r.Context(), tenantID, claims.UserID, id, req.Reason)
	if err != nil {
		writeServiceError(w, err)
		return
	}
	httputil.WriteSuccess(w, order)
}

// RefundOrderItems 按项退费（items 为空 = 整单全退）。
func (h *Handler) RefundOrderItems(w http.ResponseWriter, r *http.Request) {
	claims, tenantID, ok := h.requireTenant(w, r)
	if !ok {
		return
	}
	if !h.requireModule(w, r, tenantID, claims.UserID, "billing") {
		return
	}
	id, ok := parseIDParam(w, r, "id")
	if !ok {
		return
	}
	var req RefundOrderRequest
	if !decodeJSON(w, r, &req) {
		return
	}
	order, err := h.service.RefundOrderItems(r.Context(), tenantID, claims.UserID, id, req)
	if err != nil {
		writeServiceError(w, err)
		return
	}
	httputil.WriteSuccess(w, order)
}

// VoidOrder 作废收费单。
func (h *Handler) VoidOrder(w http.ResponseWriter, r *http.Request) {
	claims, tenantID, ok := h.requireTenant(w, r)
	if !ok {
		return
	}
	if !h.requireModule(w, r, tenantID, claims.UserID, "billing") {
		return
	}
	id, ok := parseIDParam(w, r, "id")
	if !ok {
		return
	}
	order, err := h.service.VoidOrder(r.Context(), tenantID, id)
	if err != nil {
		writeServiceError(w, err)
		return
	}
	httputil.WriteSuccess(w, order)
}

// ListPharmacyPrescriptions 药房处方队列（?status=charged|dispensed）。
func (h *Handler) ListPharmacyPrescriptions(w http.ResponseWriter, r *http.Request) {
	_, tenantID, ok := h.requireTenant(w, r)
	if !ok {
		return
	}
	items, err := h.service.ListPharmacyPrescriptions(r.Context(), tenantID, r.URL.Query().Get("status"))
	if err != nil {
		slog.Error("his: list pharmacy prescriptions", "error", err)
		httputil.WriteInternalError(w, "failed to list pharmacy prescriptions")
		return
	}
	httputil.WriteSuccess(w, items)
}

// DispensePrescription 发药（扣库存）。
func (h *Handler) DispensePrescription(w http.ResponseWriter, r *http.Request) {
	claims, tenantID, ok := h.requireTenant(w, r)
	if !ok {
		return
	}
	if !h.requireModule(w, r, tenantID, claims.UserID, "pharmacy") {
		return
	}
	id, ok := parseIDParam(w, r, "id")
	if !ok {
		return
	}
	item, err := h.service.DispensePrescription(r.Context(), tenantID, claims.UserID, id)
	if err != nil {
		writeServiceError(w, err)
		return
	}
	httputil.WriteSuccess(w, item)
}

// ListStock 库存列表（?keyword=）。
func (h *Handler) ListStock(w http.ResponseWriter, r *http.Request) {
	_, tenantID, ok := h.requireTenant(w, r)
	if !ok {
		return
	}
	items, err := h.service.ListStock(r.Context(), tenantID, r.URL.Query().Get("keyword"))
	if err != nil {
		httputil.WriteInternalError(w, "failed to list stock")
		return
	}
	httputil.WriteSuccess(w, items)
}

// InboundStock 入库。
func (h *Handler) InboundStock(w http.ResponseWriter, r *http.Request) {
	claims, tenantID, ok := h.requireTenant(w, r)
	if !ok {
		return
	}
	if !h.requireModule(w, r, tenantID, claims.UserID, "pharmacy") {
		return
	}
	var req InboundStockRequest
	if !decodeJSON(w, r, &req) {
		return
	}
	item, err := h.service.InboundStock(r.Context(), tenantID, claims.UserID, req)
	if err != nil {
		writeServiceError(w, err)
		return
	}
	httputil.WriteJSON(w, http.StatusCreated, item)
}

// GenerateDocument 生成文书快照。
func (h *Handler) GenerateDocument(w http.ResponseWriter, r *http.Request) {
	_, tenantID, ok := h.requireTenant(w, r)
	if !ok {
		return
	}
	var req GenerateDocumentRequest
	if !decodeJSON(w, r, &req) {
		return
	}
	doc, err := h.service.GenerateDocument(r.Context(), tenantID, req)
	if err != nil {
		writeServiceError(w, err)
		return
	}
	httputil.WriteSuccess(w, doc)
}

// ListDocuments 文书列表。
func (h *Handler) ListDocuments(w http.ResponseWriter, r *http.Request) {
	_, tenantID, ok := h.requireTenant(w, r)
	if !ok {
		return
	}
	var customerID, encounterID int64
	if v := r.URL.Query().Get("customer_id"); v != "" {
		customerID, _ = strconv.ParseInt(v, 10, 64)
	}
	if v := r.URL.Query().Get("encounter_id"); v != "" {
		encounterID, _ = strconv.ParseInt(v, 10, 64)
	}
	docs, err := h.service.store.ListDocuments(r.Context(), tenantID, customerID, encounterID,
		r.URL.Query().Get("doc_type"), r.URL.Query().Get("status"))
	if err != nil {
		httputil.WriteInternalError(w, "failed to list documents")
		return
	}
	httputil.WriteSuccess(w, docs)
}

// GetDocument 文书详情。
func (h *Handler) GetDocument(w http.ResponseWriter, r *http.Request) {
	_, tenantID, ok := h.requireTenant(w, r)
	if !ok {
		return
	}
	id, ok := parseIDParam(w, r, "id")
	if !ok {
		return
	}
	doc, err := h.service.store.LoadDocument(r.Context(), tenantID, id)
	if err != nil {
		writeServiceError(w, err)
		return
	}
	httputil.WriteSuccess(w, doc)
}

// SignDocument 签署文书。
func (h *Handler) SignDocument(w http.ResponseWriter, r *http.Request) {
	claims, tenantID, ok := h.requireTenant(w, r)
	if !ok {
		return
	}
	id, ok := parseIDParam(w, r, "id")
	if !ok {
		return
	}
	var req CreateSignatureRequest
	if !decodeJSON(w, r, &req) {
		return
	}
	doc, err := h.service.SignDocument(r.Context(), tenantID, claims.UserID, id, req)
	if err != nil {
		writeServiceError(w, err)
		return
	}
	httputil.WriteSuccess(w, doc)
}

// ArchiveDocument 归档文书。
func (h *Handler) ArchiveDocument(w http.ResponseWriter, r *http.Request) {
	_, tenantID, ok := h.requireTenant(w, r)
	if !ok {
		return
	}
	id, ok := parseIDParam(w, r, "id")
	if !ok {
		return
	}
	doc, err := h.service.ArchiveDocument(r.Context(), tenantID, id)
	if err != nil {
		writeServiceError(w, err)
		return
	}
	httputil.WriteSuccess(w, doc)
}

// PrintDocument 打印文书（纸质归档兜底）。
func (h *Handler) PrintDocument(w http.ResponseWriter, r *http.Request) {
	_, tenantID, ok := h.requireTenant(w, r)
	if !ok {
		return
	}
	id, ok := parseIDParam(w, r, "id")
	if !ok {
		return
	}
	doc, err := h.service.PrintDocument(r.Context(), tenantID, id)
	if err != nil {
		writeServiceError(w, err)
		return
	}
	httputil.WriteSuccess(w, doc)
}

// SubmitFollowupDocument 随访文书补录 + 生成。
func (h *Handler) SubmitFollowupDocument(w http.ResponseWriter, r *http.Request) {
	_, tenantID, ok := h.requireTenant(w, r)
	if !ok {
		return
	}
	var req FollowupDocumentRequest
	if !decodeJSON(w, r, &req) {
		return
	}
	doc, err := h.service.SubmitFollowupDocument(r.Context(), tenantID, req)
	if err != nil {
		writeServiceError(w, err)
		return
	}
	httputil.WriteSuccess(w, doc)
}

// ReportSummary 经营汇总（?date_from=&date_to=）。
func (h *Handler) ReportSummary(w http.ResponseWriter, r *http.Request) {
	_, tenantID, ok := h.requireTenant(w, r)
	if !ok {
		return
	}
	summary, err := h.service.ReportSummary(r.Context(), tenantID,
		r.URL.Query().Get("date_from"), r.URL.Query().Get("date_to"))
	if err != nil {
		httputil.WriteInternalError(w, "failed to load report summary")
		return
	}
	httputil.WriteSuccess(w, summary)
}

// StatsOverview 统计概览（?date_from=&date_to=，默认最近 7 天）。
func (h *Handler) StatsOverview(w http.ResponseWriter, r *http.Request) {
	_, tenantID, ok := h.requireTenant(w, r)
	if !ok {
		return
	}
	overview, err := h.service.StatsOverview(r.Context(), tenantID,
		r.URL.Query().Get("date_from"), r.URL.Query().Get("date_to"))
	if err != nil {
		writeServiceError(w, err)
		return
	}
	httputil.WriteSuccess(w, overview)
}

// ListOpenClients 开放客户端列表。
func (h *Handler) ListOpenClients(w http.ResponseWriter, r *http.Request) {
	_, tenantID, ok := h.requireTenant(w, r)
	if !ok {
		return
	}
	items, err := h.service.ListOpenClients(r.Context(), tenantID)
	if err != nil {
		httputil.WriteInternalError(w, "failed to list open clients")
		return
	}
	httputil.WriteSuccess(w, items)
}

// CreateOpenClient 创建客户端（API Key 仅返回一次）。
func (h *Handler) CreateOpenClient(w http.ResponseWriter, r *http.Request) {
	claims, tenantID, ok := h.requireTenant(w, r)
	if !ok {
		return
	}
	if !h.requireHisRole(w, r, tenantID, claims.UserID, "admin") {
		return
	}
	var req CreateOpenClientRequest
	if !decodeJSON(w, r, &req) {
		return
	}
	resp, err := h.service.CreateOpenClient(r.Context(), tenantID, req)
	if err != nil {
		writeServiceError(w, err)
		return
	}
	httputil.WriteJSON(w, http.StatusCreated, resp)
}

// UpdateOpenClient 更新客户端（停用/启用/限流）。
func (h *Handler) UpdateOpenClient(w http.ResponseWriter, r *http.Request) {
	claims, tenantID, ok := h.requireTenant(w, r)
	if !ok {
		return
	}
	if !h.requireHisRole(w, r, tenantID, claims.UserID, "admin") {
		return
	}
	id, ok := parseIDParam(w, r, "id")
	if !ok {
		return
	}
	var req UpdateOpenClientRequest
	if !decodeJSON(w, r, &req) {
		return
	}
	client, err := h.service.UpdateOpenClient(r.Context(), tenantID, id, req)
	if err != nil {
		writeServiceError(w, err)
		return
	}
	httputil.WriteSuccess(w, client)
}

// OpenDepartments 开放接口：科室列表。
func (h *Handler) OpenDepartments(w http.ResponseWriter, r *http.Request) {
	tenantID, ok := h.requireOpenClient(w, r)
	if !ok {
		return
	}
	items, err := h.service.ListOpenDepartments(r.Context(), tenantID)
	if err != nil {
		httputil.WriteInternalError(w, "failed to list departments")
		return
	}
	httputil.WriteSuccess(w, items)
}

// OpenDoctors 开放接口：医生列表。
func (h *Handler) OpenDoctors(w http.ResponseWriter, r *http.Request) {
	tenantID, ok := h.requireOpenClient(w, r)
	if !ok {
		return
	}
	items, err := h.service.ListOpenDoctors(r.Context(), tenantID)
	if err != nil {
		httputil.WriteInternalError(w, "failed to list doctors")
		return
	}
	httputil.WriteSuccess(w, items)
}

// OpenSchedules 开放接口：排班号源（?date=&department_id=&doctor_id=）。
func (h *Handler) OpenSchedules(w http.ResponseWriter, r *http.Request) {
	tenantID, ok := h.requireOpenClient(w, r)
	if !ok {
		return
	}
	var departmentID, doctorID int64
	if v := r.URL.Query().Get("department_id"); v != "" {
		departmentID, _ = strconv.ParseInt(v, 10, 64)
	}
	if v := r.URL.Query().Get("doctor_id"); v != "" {
		doctorID, _ = strconv.ParseInt(v, 10, 64)
	}
	items, err := h.service.ListOpenSchedules(r.Context(), tenantID, r.URL.Query().Get("date"), departmentID, doctorID)
	if err != nil {
		httputil.WriteInternalError(w, "failed to list schedules")
		return
	}
	httputil.WriteSuccess(w, items)
}

// OpenCreateAppointment 开放接口：患者预约。
func (h *Handler) OpenCreateAppointment(w http.ResponseWriter, r *http.Request) {
	tenantID, ok := h.requireOpenClient(w, r)
	if !ok {
		return
	}
	var req OpenAppointmentRequest
	if !decodeJSON(w, r, &req) {
		return
	}
	item, err := h.service.CreateOpenAppointment(r.Context(), tenantID, req)
	if err != nil {
		writeServiceError(w, err)
		return
	}
	httputil.WriteJSON(w, http.StatusCreated, item)
}

// OpenGetAppointment 开放接口：挂号单状态。
func (h *Handler) OpenGetAppointment(w http.ResponseWriter, r *http.Request) {
	tenantID, ok := h.requireOpenClient(w, r)
	if !ok {
		return
	}
	id, ok := parseIDParam(w, r, "id")
	if !ok {
		return
	}
	item, err := h.service.GetOpenAppointment(r.Context(), tenantID, id)
	if err != nil {
		writeServiceError(w, err)
		return
	}
	httputil.WriteSuccess(w, item)
}

// OpenCancelAppointment 开放接口：取消预约。
func (h *Handler) OpenCancelAppointment(w http.ResponseWriter, r *http.Request) {
	tenantID, ok := h.requireOpenClient(w, r)
	if !ok {
		return
	}
	id, ok := parseIDParam(w, r, "id")
	if !ok {
		return
	}
	item, err := h.service.CancelOpenAppointment(r.Context(), tenantID, id)
	if err != nil {
		writeServiceError(w, err)
		return
	}
	httputil.WriteSuccess(w, item)
}

// ListStaffRoles 员工角色列表（管理台）。
func (h *Handler) ListStaffRoles(w http.ResponseWriter, r *http.Request) {
	claims, tenantID, ok := h.requireTenant(w, r)
	if !ok {
		return
	}
	if !h.requireHisRole(w, r, tenantID, claims.UserID, "admin") {
		return
	}
	items, err := h.service.ListStaffRoles(r.Context(), tenantID)
	if err != nil {
		httputil.WriteInternalError(w, "failed to list staff roles")
		return
	}
	httputil.WriteSuccess(w, items)
}

// UpdateStaffRoles 覆盖式设置员工角色。
func (h *Handler) UpdateStaffRoles(w http.ResponseWriter, r *http.Request) {
	claims, tenantID, ok := h.requireTenant(w, r)
	if !ok {
		return
	}
	if !h.requireHisRole(w, r, tenantID, claims.UserID, "admin") {
		return
	}
	employeeID, ok := parseIDParam(w, r, "employee_id")
	if !ok {
		return
	}
	var req UpdateStaffRolesRequest
	if !decodeJSON(w, r, &req) {
		return
	}
	if err := h.service.UpdateStaffRoles(r.Context(), tenantID, employeeID, req.Roles); err != nil {
		writeServiceError(w, err)
		return
	}
	httputil.WriteSuccess(w, map[string]any{"employee_id": employeeID, "roles": req.Roles})
}

// MyRoles 当前员工角色（nil 表示租户未配置角色，前端全开放）。
func (h *Handler) MyRoles(w http.ResponseWriter, r *http.Request) {
	claims, tenantID, ok := h.requireTenant(w, r)
	if !ok {
		return
	}
	roles, err := h.service.MyRoles(r.Context(), tenantID, claims.UserID)
	if err != nil {
		httputil.WriteInternalError(w, "failed to load my roles")
		return
	}
	httputil.WriteSuccess(w, roles)
}

// SearchDiagnosis ICD-10 诊断检索（?keyword=）。
func (h *Handler) SearchDiagnosis(w http.ResponseWriter, r *http.Request) {
	_, tenantID, ok := h.requireTenant(w, r)
	if !ok {
		return
	}
	items, err := h.service.SearchDiagnosis(r.Context(), tenantID, r.URL.Query().Get("keyword"))
	if err != nil {
		slog.Error("his: search diagnosis", "error", err)
		httputil.WriteInternalError(w, "failed to search diagnosis")
		return
	}
	httputil.WriteSuccess(w, items)
}

// MyModules 当前员工可见模块（角色并集，admin 全量）。
func (h *Handler) MyModules(w http.ResponseWriter, r *http.Request) {
	claims, tenantID, ok := h.requireTenant(w, r)
	if !ok {
		return
	}
	modules, err := h.service.ModulesForEmployee(r.Context(), tenantID, claims.UserID)
	if err != nil {
		slog.Error("his: my modules", "error", err)
		httputil.WriteInternalError(w, "failed to load role modules")
		return
	}
	httputil.WriteSuccess(w, modules)
}

// DefaultRoleModules 各角色默认模块映射（管理页展示用）。
func (h *Handler) DefaultRoleModules(w http.ResponseWriter, r *http.Request) {
	claims, tenantID, ok := h.requireTenant(w, r)
	if !ok {
		return
	}
	if !h.requireHisRole(w, r, tenantID, claims.UserID, "admin") {
		return
	}
	modules, err := h.service.DefaultRoleModules(r.Context(), 0)
	if err != nil {
		slog.Error("his: default role modules", "error", err)
		httputil.WriteInternalError(w, "failed to load default role modules")
		return
	}
	httputil.WriteSuccess(w, modules)
}

// RoleModuleConfig 当前租户生效的「角色 × 模块」配置（管理页权限矩阵）。
func (h *Handler) RoleModuleConfig(w http.ResponseWriter, r *http.Request) {
	claims, tenantID, ok := h.requireTenant(w, r)
	if !ok {
		return
	}
	if !h.requireHisRole(w, r, tenantID, claims.UserID, "admin") {
		return
	}
	config, err := h.service.RoleModuleConfig(r.Context(), tenantID)
	if err != nil {
		slog.Error("his: role module config", "error", err)
		httputil.WriteInternalError(w, "failed to load role module config")
		return
	}
	httputil.WriteSuccess(w, config)
}

// UpdateRoleModules 覆盖式写入某角色的模块配置（管理员；admin 角色固定全量）。
func (h *Handler) UpdateRoleModules(w http.ResponseWriter, r *http.Request) {
	claims, tenantID, ok := h.requireTenant(w, r)
	if !ok {
		return
	}
	if !h.requireHisRole(w, r, tenantID, claims.UserID, "admin") {
		return
	}
	role := strings.TrimSpace(r.PathValue("role"))
	var req UpdateRoleModulesRequest
	if !decodeJSON(w, r, &req) {
		return
	}
	modules, err := h.service.UpdateRoleModules(r.Context(), tenantID, role, req.Modules)
	if err != nil {
		writeServiceError(w, err)
		return
	}
	httputil.WriteSuccess(w, map[string]any{"role": role, "modules": modules})
}

// ListMasterEmployees 员工 + HIS 角色 + 医生档案（管理页员工列表）。
func (h *Handler) ListMasterEmployees(w http.ResponseWriter, r *http.Request) {
	_, tenantID, ok := h.requireTenant(w, r)
	if !ok {
		return
	}
	rows, err := h.service.ListEmployeesWithRoles(r.Context(), tenantID)
	if err != nil {
		slog.Error("his: list master employees", "error", err)
		httputil.WriteInternalError(w, "failed to load employees")
		return
	}
	httputil.WriteSuccess(w, rows)
}

// UpdateMasterEmployee 启用/停用员工（管理页员工列表；不允许停用当前登录账号）。
func (h *Handler) UpdateMasterEmployee(w http.ResponseWriter, r *http.Request) {
	claims, tenantID, ok := h.requireTenant(w, r)
	if !ok {
		return
	}
	if !h.requireHisRole(w, r, tenantID, claims.UserID, "admin") {
		return
	}
	employeeID, ok := parseIDParam(w, r, "id")
	if !ok {
		return
	}
	var req UpdateEmployeeStatusRequest
	if !decodeJSON(w, r, &req) {
		return
	}
	if req.IsActive == nil {
		httputil.WriteBadRequest(w, "is_active is required")
		return
	}
	if claims.UserID == employeeID && !*req.IsActive {
		httputil.WriteBadRequest(w, "不能停用当前登录账号")
		return
	}
	active, err := h.service.SetEmployeeActive(r.Context(), tenantID, employeeID, *req.IsActive)
	if err != nil {
		writeServiceError(w, err)
		return
	}
	httputil.WriteSuccess(w, map[string]any{"id": employeeID, "is_active": active})
}

// SearchPatients 挂号查重（手机号/姓名）。
func (h *Handler) SearchPatients(w http.ResponseWriter, r *http.Request) {
	_, tenantID, ok := h.requireTenant(w, r)
	if !ok {
		return
	}
	keyword := strings.TrimSpace(r.URL.Query().Get("keyword"))
	if keyword == "" {
		httputil.WriteSuccess(w, []PatientSearchRow{})
		return
	}
	rows, err := h.service.SearchPatients(r.Context(), tenantID, keyword)
	if err != nil {
		slog.Error("his: search patients", "error", err)
		httputil.WriteInternalError(w, "failed to search patients")
		return
	}
	httputil.WriteSuccess(w, rows)
}

// LatestEMRQuality 病历最近一次质检结果（阻断/提示原因）。
func (h *Handler) LatestEMRQuality(w http.ResponseWriter, r *http.Request) {
	_, tenantID, ok := h.requireTenant(w, r)
	if !ok {
		return
	}
	recordID := r.PathValue("id")
	report, err := h.service.LatestQualityReport(r.Context(), tenantID, recordID)
	if err != nil {
		httputil.WriteInternalError(w, "failed to load quality report")
		return
	}
	httputil.WriteSuccess(w, report)
}
