package billing

// 运营端计费 HTTP 接口：机构档案、开通/加量/赠送/续期/加产品/延长试用、收款核销、
// 价目与套餐、渠道与销售、行业与科室字典。
//
// 路径沿用仓库既有风格 /api/v1/ops/...；全部只允许运营账号（admin）。
// 数据权限（只能看自己组织名下的租户）在 service 层按 JWT 里的运营账号 id 校验。
// 响应沿用仓库既有封装：成功 {"data": ...}，失败 {"code","message","details"}；
// 前端 billingService.ts 的 invalid/conflict/not_found 分别对应 HTTP 400/409/404。

import (
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strconv"
	"strings"

	"github.com/freeasyman/lingce-api/internal/router"
	"github.com/freeasyman/lingce-api/pkg/httputil"
)

// maxInstitutionBodyBytes 限制机构相关请求体大小（一单最多几十行产品，1MB 足够）。
const maxInstitutionBodyBytes = 1 << 20

// institutionRoutes 是运营端计费第 1 阶段的全部路由。
func (h *Handler) institutionRoutes() []router.Route {
	ops := func(method, path string, handler http.HandlerFunc) router.Route {
		return router.Route{Method: method, Path: path, Handler: handler, Auth: true, AllowedUserTypes: []string{"admin"}}
	}
	base := "/api/v1/ops/billing"
	institution := base + "/institutions"
	return []router.Route{
		// 机构档案
		ops("GET", institution, h.ListInstitutions),
		ops("POST", institution, h.CreateInstitution),
		ops("GET", institution+"/{id}", h.GetInstitution),
		ops("PUT", institution+"/{id}", h.UpdateInstitution),
		// 机构管理员
		ops("GET", institution+"/{id}/admins", h.ListInstitutionAdmins),
		ops("POST", institution+"/{id}/admins", h.CreateInstitutionAdmin),
		ops("PUT", institution+"/{id}/admins/{admin_id}", h.UpdateInstitutionAdmin),
		ops("POST", institution+"/{id}/admins/{admin_id}/reset-password", h.ResetInstitutionAdminPassword),
		ops("POST", institution+"/{id}/admins/{admin_id}/set-primary", h.SetInstitutionAdminPrimary),
		// 机构动作
		ops("POST", institution+"/{id}/activate-formal", h.ActivateFormalInstitution),
		ops("POST", institution+"/{id}/activate-trial", h.ActivateTrialInstitution),
		ops("POST", institution+"/{id}/migrate-trial", h.MigrateTrialInstitution),
		ops("POST", institution+"/{id}/new-system-trial", h.StartNewSystemTrial),
		ops("POST", institution+"/{id}/trial-products", h.SetTrialProducts),
		ops("POST", institution+"/{id}/increments", h.AddInstitutionIncrement),
		ops("POST", institution+"/{id}/gifts", h.GiftInstitutionIncrement),
		ops("POST", institution+"/{id}/renew", h.RenewInstitution),
		ops("POST", institution+"/{id}/products", h.AddInstitutionProduct),
		ops("POST", institution+"/{id}/extend-trial", h.ExtendInstitutionTrial),
		ops("POST", institution+"/{id}/payments", h.ReceiveInstitutionPayment),
		// 服务生命周期（第 2 批）：暂停 / 恢复
		ops("POST", institution+"/{id}/suspend", h.SuspendInstitution),
		ops("POST", institution+"/{id}/resume", h.ResumeInstitution),
		// 更正：作废订单 / 人工调额度
		ops("GET", institution+"/{id}/voidable-orders", h.ListVoidableInstitutionOrders),
		ops("POST", institution+"/{id}/entitlement-corrections", h.CorrectInstitutionEntitlement),
		ops("POST", base+"/orders/{orderId}/void", h.VoidBillingOrder),
		// 归属
		ops("GET", institution+"/{id}/ownerships", h.ListInstitutionOwnerships),
		ops("POST", institution+"/{id}/ownerships", h.CreateInstitutionOwnership),
		ops("PUT", institution+"/{id}/ownerships/{ownership_id}", h.UpdateInstitutionOwnership),
		ops("POST", institution+"/{id}/ownerships/{ownership_id}/close", h.CloseInstitutionOwnership),
		// 通知函（文档留档 + PDF 下载）
		ops("POST", institution+"/{id}/documents", h.GenerateInstitutionDocument),
		ops("GET", institution+"/{id}/documents", h.ListInstitutionDocuments),
		ops("GET", base+"/documents/{docId}/file", h.DownloadBillingDocument),
		// 服务期
		ops("GET", institution+"/{id}/periods", h.ListInstitutionPeriods),
		ops("POST", institution+"/{id}/periods", h.CreateInstitutionPeriod),
		ops("PUT", institution+"/{id}/periods/{period_id}", h.UpdateInstitutionPeriod),
		// 价目 / 渠道 / 字典
		ops("GET", base+"/pricing", h.GetBillingPricing),
		ops("GET", base+"/channels", h.GetBillingChannels),
		ops("POST", base+"/channels", h.SaveBillingChannel),
		ops("POST", base+"/sales", h.SaveBillingSales),
		ops("GET", base+"/dictionaries", h.GetBillingDictionaries),
		ops("POST", base+"/dictionaries/{kind}/items", h.CreateBillingDictItem),
		ops("PUT", base+"/dictionaries/{kind}/items/{id}", h.UpdateBillingDictItem),
		ops("POST", base+"/dictionaries/{kind}/items/{id}/toggle", h.ToggleBillingDictItem),
		// 财务：收款单 + 核销
		ops("GET", base+"/payments", h.ListBillingPayments),
		ops("POST", base+"/payments", h.CreateBillingPayment),
		ops("POST", base+"/payments/{id}/allocations", h.AllocateBillingPayment),
		// 财务：开票记录（第 3 批）
		ops("GET", base+"/invoices", h.ListBillingInvoices),
		ops("POST", base+"/invoices", h.CreateBillingInvoice),
		ops("POST", base+"/invoices/{id}/issue", h.IssueBillingInvoice),
		ops("POST", base+"/invoices/{id}/void", h.VoidBillingInvoice),
		ops("GET", institution+"/{id}/invoiceable-orders", h.ListInstitutionInvoiceableOrders),
		// 财务：应收 / 收款 / 开票 CSV 导出
		ops("GET", base+"/export/{kind}", h.ExportBillingFinance),
		// 改价：新增价目版本（审批人 + 原因必填，写 system_action_logs）
		ops("POST", base+"/catalog/versions", h.CreateBillingCatalogVersion),
		// 机构变更记录（system_action_logs）
		ops("GET", institution+"/{id}/action-logs", h.ListInstitutionActionLogs),
		// 可核销订单（新建跨机构收款单的订单选择器）
		ops("GET", institution+"/{id}/allocatable-orders", h.ListInstitutionAllocatableOrders),
	}
}

// ListInstitutions 机构列表：?filter=all|unopened|expiring|expired|unpaid|trial&keyword=。
func (h *Handler) ListInstitutions(w http.ResponseWriter, r *http.Request) {
	adminID, ok := requireAdminActor(w, r)
	if !ok {
		return
	}
	query := r.URL.Query()
	result, err := h.institutions.ListInstitutions(r.Context(), InstitutionQuery{
		Filter:  query.Get("filter"),
		Keyword: query.Get("keyword"),
	}, adminID)
	if err != nil {
		writeInstitutionError(w, err)
		return
	}
	httputil.WriteSuccess(w, result)
}

// GetInstitution 机构详情。
func (h *Handler) GetInstitution(w http.ResponseWriter, r *http.Request) {
	tenantID, ok := requireInstitutionID(w, r)
	if !ok {
		return
	}
	adminID, ok := requireAdminActor(w, r)
	if !ok {
		return
	}
	item, err := h.institutions.GetInstitutionForAdmin(r.Context(), tenantID, adminID)
	if err != nil {
		// 与 billingService.getInstitution 的契约一致：机构不存在时返回 {item: null}，不抛错。
		if errors.Is(err, ErrInstitutionNotFound) {
			httputil.WriteSuccess(w, InstitutionDetailResult{Item: nil, IsMock: false})
			return
		}
		writeInstitutionError(w, err)
		return
	}
	httputil.WriteSuccess(w, InstitutionDetailResult{Item: item, IsMock: false})
}

// CreateInstitution 机构建档（只建档案 + 主管理员，不涉及产品与钱）。
func (h *Handler) CreateInstitution(w http.ResponseWriter, r *http.Request) {
	adminID, ok := requireAdminActor(w, r)
	if !ok {
		return
	}
	var req CreateInstitutionRequest
	if err := decodeInstitutionBody(r, &req); err != nil {
		httputil.WriteBadRequest(w, "invalid request body")
		return
	}
	item, err := h.institutions.CreateInstitution(r.Context(), req, adminID)
	if err != nil {
		writeInstitutionError(w, err)
		return
	}
	httputil.WriteSuccess(w, item)
}

// UpdateInstitution 编辑机构档案。
// 只更新请求体里出现的字段；code（机构代码）与 billing_mode（内部字段）不可改。
func (h *Handler) UpdateInstitution(w http.ResponseWriter, r *http.Request) {
	tenantID, ok := requireInstitutionID(w, r)
	if !ok {
		return
	}
	adminID, ok := requireAdminActor(w, r)
	if !ok {
		return
	}
	var req UpdateInstitutionRequest
	if err := decodeInstitutionBody(r, &req); err != nil {
		httputil.WriteBadRequest(w, "invalid request body")
		return
	}
	item, err := h.institutions.UpdateInstitution(r.Context(), tenantID, req, adminID)
	if err != nil {
		writeInstitutionError(w, err)
		return
	}
	httputil.WriteSuccess(w, InstitutionDetailResult{Item: item, IsMock: false})
}

// ListInstitutionAdmins 机构管理员列表。
func (h *Handler) ListInstitutionAdmins(w http.ResponseWriter, r *http.Request) {
	tenantID, ok := requireInstitutionID(w, r)
	if !ok {
		return
	}
	adminID, ok := requireAdminActor(w, r)
	if !ok {
		return
	}
	result, err := h.institutions.ListInstitutionAdmins(r.Context(), tenantID, adminID)
	if err != nil {
		writeInstitutionError(w, err)
		return
	}
	httputil.WriteSuccess(w, result)
}

// CreateInstitutionAdmin 新增机构管理员（初始密码不回传）。
func (h *Handler) CreateInstitutionAdmin(w http.ResponseWriter, r *http.Request) {
	tenantID, ok := requireInstitutionID(w, r)
	if !ok {
		return
	}
	adminID, ok := requireAdminActor(w, r)
	if !ok {
		return
	}
	var req CreateInstitutionAdminRequest
	if err := decodeInstitutionBody(r, &req); err != nil {
		httputil.WriteBadRequest(w, "invalid request body")
		return
	}
	item, err := h.institutions.CreateInstitutionAdmin(r.Context(), tenantID, req, adminID)
	if err != nil {
		writeInstitutionError(w, err)
		return
	}
	httputil.WriteSuccess(w, InstitutionAdminItemResult{Item: *item})
}

// UpdateInstitutionAdmin 修改管理员基本信息/启停。
func (h *Handler) UpdateInstitutionAdmin(w http.ResponseWriter, r *http.Request) {
	tenantID, ok := requireInstitutionID(w, r)
	if !ok {
		return
	}
	adminAccountID, ok := requirePathID(w, r, "admin_id", "Invalid admin ID")
	if !ok {
		return
	}
	adminID, ok := requireAdminActor(w, r)
	if !ok {
		return
	}
	var req UpdateInstitutionAdminRequest
	if err := decodeInstitutionBody(r, &req); err != nil {
		httputil.WriteBadRequest(w, "invalid request body")
		return
	}
	item, err := h.institutions.UpdateInstitutionAdmin(r.Context(), tenantID, adminAccountID, req, adminID)
	if err != nil {
		writeInstitutionError(w, err)
		return
	}
	httputil.WriteSuccess(w, InstitutionAdminItemResult{Item: *item})
}

// ResetInstitutionAdminPassword 重置管理员密码（默认口令 + session_version bump，响应不含明文）。
func (h *Handler) ResetInstitutionAdminPassword(w http.ResponseWriter, r *http.Request) {
	tenantID, ok := requireInstitutionID(w, r)
	if !ok {
		return
	}
	adminAccountID, ok := requirePathID(w, r, "admin_id", "Invalid admin ID")
	if !ok {
		return
	}
	adminID, ok := requireAdminActor(w, r)
	if !ok {
		return
	}
	item, err := h.institutions.ResetInstitutionAdminPassword(r.Context(), tenantID, adminAccountID, adminID)
	if err != nil {
		writeInstitutionError(w, err)
		return
	}
	httputil.WriteSuccess(w, InstitutionAdminItemResult{Item: *item})
}

// SetInstitutionAdminPrimary 把管理员设为主管理员（同一机构只能有一个）。
func (h *Handler) SetInstitutionAdminPrimary(w http.ResponseWriter, r *http.Request) {
	tenantID, ok := requireInstitutionID(w, r)
	if !ok {
		return
	}
	adminAccountID, ok := requirePathID(w, r, "admin_id", "Invalid admin ID")
	if !ok {
		return
	}
	adminID, ok := requireAdminActor(w, r)
	if !ok {
		return
	}
	item, err := h.institutions.SetInstitutionAdminPrimary(r.Context(), tenantID, adminAccountID, adminID)
	if err != nil {
		writeInstitutionError(w, err)
		return
	}
	httputil.WriteSuccess(w, InstitutionAdminItemResult{Item: *item})
}

// ActivateFormalInstitution 正式开通。
func (h *Handler) ActivateFormalInstitution(w http.ResponseWriter, r *http.Request) {
	tenantID, ok := requireInstitutionID(w, r)
	if !ok {
		return
	}
	adminID, ok := requireAdminActor(w, r)
	if !ok {
		return
	}
	var req FormalActivationRequest
	if err := decodeInstitutionBody(r, &req); err != nil {
		httputil.WriteBadRequest(w, "invalid request body")
		return
	}
	item, err := h.institutions.ActivateFormal(r.Context(), tenantID, req, adminID)
	if err != nil {
		writeInstitutionError(w, err)
		return
	}
	httputil.WriteSuccess(w, item)
}

// ActivateTrialInstitution 试用开通。
func (h *Handler) ActivateTrialInstitution(w http.ResponseWriter, r *http.Request) {
	tenantID, ok := requireInstitutionID(w, r)
	if !ok {
		return
	}
	adminID, ok := requireAdminActor(w, r)
	if !ok {
		return
	}
	var req TrialActivationRequest
	if err := decodeInstitutionBody(r, &req); err != nil {
		httputil.WriteBadRequest(w, "invalid request body")
		return
	}
	item, err := h.institutions.ActivateTrial(r.Context(), tenantID, req, adminID)
	if err != nil {
		writeInstitutionError(w, err)
		return
	}
	httputil.WriteSuccess(w, item)
}

// AddInstitutionIncrement 加量。
func (h *Handler) AddInstitutionIncrement(w http.ResponseWriter, r *http.Request) {
	tenantID, ok := requireInstitutionID(w, r)
	if !ok {
		return
	}
	adminID, ok := requireAdminActor(w, r)
	if !ok {
		return
	}
	var req IncrementRequest
	if err := decodeInstitutionBody(r, &req); err != nil {
		httputil.WriteBadRequest(w, "invalid request body")
		return
	}
	item, err := h.institutions.AddIncrement(r.Context(), tenantID, req, adminID)
	if err != nil {
		writeInstitutionError(w, err)
		return
	}
	httputil.WriteSuccess(w, item)
}

// GiftInstitutionIncrement 赠送增量包。
func (h *Handler) GiftInstitutionIncrement(w http.ResponseWriter, r *http.Request) {
	tenantID, ok := requireInstitutionID(w, r)
	if !ok {
		return
	}
	adminID, ok := requireAdminActor(w, r)
	if !ok {
		return
	}
	var req GiftIncrementRequest
	if err := decodeInstitutionBody(r, &req); err != nil {
		httputil.WriteBadRequest(w, "invalid request body")
		return
	}
	item, err := h.institutions.GiftIncrement(r.Context(), tenantID, req, adminID)
	if err != nil {
		writeInstitutionError(w, err)
		return
	}
	httputil.WriteSuccess(w, item)
}

// RenewInstitution 续期。
func (h *Handler) RenewInstitution(w http.ResponseWriter, r *http.Request) {
	tenantID, ok := requireInstitutionID(w, r)
	if !ok {
		return
	}
	adminID, ok := requireAdminActor(w, r)
	if !ok {
		return
	}
	var req RenewRequest
	if err := decodeInstitutionBody(r, &req); err != nil {
		httputil.WriteBadRequest(w, "invalid request body")
		return
	}
	item, err := h.institutions.RenewInstitution(r.Context(), tenantID, req, adminID)
	if err != nil {
		writeInstitutionError(w, err)
		return
	}
	httputil.WriteSuccess(w, item)
}

// AddInstitutionProduct 加产品。
func (h *Handler) AddInstitutionProduct(w http.ResponseWriter, r *http.Request) {
	tenantID, ok := requireInstitutionID(w, r)
	if !ok {
		return
	}
	adminID, ok := requireAdminActor(w, r)
	if !ok {
		return
	}
	var req AddProductRequest
	if err := decodeInstitutionBody(r, &req); err != nil {
		httputil.WriteBadRequest(w, "invalid request body")
		return
	}
	item, err := h.institutions.AddProduct(r.Context(), tenantID, req, adminID)
	if err != nil {
		writeInstitutionError(w, err)
		return
	}
	httputil.WriteSuccess(w, item)
}

// StartNewSystemTrial 正式客户叠加新系统试用：不改原合同/原服务期，只开新系统窗口。
func (h *Handler) StartNewSystemTrial(w http.ResponseWriter, r *http.Request) {
	tenantID, ok := requireInstitutionID(w, r)
	if !ok {
		return
	}
	adminID, ok := requireAdminActor(w, r)
	if !ok {
		return
	}
	var req TrialActivationRequest
	if err := decodeInstitutionBody(r, &req); err != nil {
		httputil.WriteBadRequest(w, "invalid request body")
		return
	}
	item, err := h.institutions.StartNewSystemTrial(r.Context(), tenantID, req, adminID)
	if err != nil {
		writeInstitutionError(w, err)
		return
	}
	httputil.WriteSuccess(w, item)
}

// SetTrialProducts 调整试用期的产品集合：可增加/移除产品，不改到期日、不收款。
func (h *Handler) SetTrialProducts(w http.ResponseWriter, r *http.Request) {
	tenantID, ok := requireInstitutionID(w, r)
	if !ok {
		return
	}
	adminID, ok := requireAdminActor(w, r)
	if !ok {
		return
	}
	var req SetTrialProductsRequest
	if err := decodeInstitutionBody(r, &req); err != nil {
		httputil.WriteBadRequest(w, "invalid request body")
		return
	}
	item, err := h.institutions.SetTrialProducts(r.Context(), tenantID, req, adminID)
	if err != nil {
		writeInstitutionError(w, err)
		return
	}
	httputil.WriteSuccess(w, item)
}

// MigrateTrialInstitution 存量客户迁移试用：保留租户与数据，发放一年试用额度，不收款。
func (h *Handler) MigrateTrialInstitution(w http.ResponseWriter, r *http.Request) {
	tenantID, ok := requireInstitutionID(w, r)
	if !ok {
		return
	}
	adminID, ok := requireAdminActor(w, r)
	if !ok {
		return
	}
	var req TrialActivationRequest
	if err := decodeInstitutionBody(r, &req); err != nil {
		httputil.WriteBadRequest(w, "invalid request body")
		return
	}
	item, err := h.institutions.MigrateTrial(r.Context(), tenantID, req, adminID)
	if err != nil {
		writeInstitutionError(w, err)
		return
	}
	httputil.WriteSuccess(w, item)
}

// ExtendInstitutionTrial 延长试用。
func (h *Handler) ExtendInstitutionTrial(w http.ResponseWriter, r *http.Request) {
	tenantID, ok := requireInstitutionID(w, r)
	if !ok {
		return
	}
	adminID, ok := requireAdminActor(w, r)
	if !ok {
		return
	}
	var req ExtendTrialRequest
	if err := decodeInstitutionBody(r, &req); err != nil {
		httputil.WriteBadRequest(w, "invalid request body")
		return
	}
	item, err := h.institutions.ExtendTrial(r.Context(), tenantID, req, adminID)
	if err != nil {
		writeInstitutionError(w, err)
		return
	}
	httputil.WriteSuccess(w, item)
}

// ReceiveInstitutionPayment 机构维度收款（自动核销最早应收）。
func (h *Handler) ReceiveInstitutionPayment(w http.ResponseWriter, r *http.Request) {
	tenantID, ok := requireInstitutionID(w, r)
	if !ok {
		return
	}
	adminID, ok := requireAdminActor(w, r)
	if !ok {
		return
	}
	var req ReceivePaymentRequest
	if err := decodeInstitutionBody(r, &req); err != nil {
		httputil.WriteBadRequest(w, "invalid request body")
		return
	}
	item, err := h.institutions.ReceiveInstitutionPayment(r.Context(), tenantID, req, adminID)
	if err != nil {
		writeInstitutionError(w, err)
		return
	}
	httputil.WriteSuccess(w, item)
}

// SuspendInstitution 暂停服务（原因必填；重复暂停幂等）。
func (h *Handler) SuspendInstitution(w http.ResponseWriter, r *http.Request) {
	tenantID, ok := requireInstitutionID(w, r)
	if !ok {
		return
	}
	adminID, ok := requireAdminActor(w, r)
	if !ok {
		return
	}
	var req SuspendRequest
	if err := decodeInstitutionBody(r, &req); err != nil {
		httputil.WriteBadRequest(w, "invalid request body")
		return
	}
	item, err := h.institutions.SuspendInstitution(r.Context(), tenantID, req, adminID)
	if err != nil {
		writeInstitutionError(w, err)
		return
	}
	httputil.WriteSuccess(w, item)
}

// ResumeInstitution 恢复服务（无请求体；重复恢复幂等）。
func (h *Handler) ResumeInstitution(w http.ResponseWriter, r *http.Request) {
	tenantID, ok := requireInstitutionID(w, r)
	if !ok {
		return
	}
	adminID, ok := requireAdminActor(w, r)
	if !ok {
		return
	}
	item, err := h.institutions.ResumeInstitution(r.Context(), tenantID, adminID)
	if err != nil {
		writeInstitutionError(w, err)
		return
	}
	httputil.WriteSuccess(w, item)
}

// ListVoidableInstitutionOrders 列出该机构可作废的订单（未收款 + 未核销）。
func (h *Handler) ListVoidableInstitutionOrders(w http.ResponseWriter, r *http.Request) {
	tenantID, ok := requireInstitutionID(w, r)
	if !ok {
		return
	}
	adminID, ok := requireAdminActor(w, r)
	if !ok {
		return
	}
	items, err := h.institutions.ListVoidableOrders(r.Context(), tenantID, adminID)
	if err != nil {
		writeInstitutionError(w, err)
		return
	}
	httputil.WriteSuccess(w, map[string]any{"items": items})
}

// VoidBillingOrder 作废订单（只允许未收款且未核销；已作废再作废幂等 200）。
func (h *Handler) VoidBillingOrder(w http.ResponseWriter, r *http.Request) {
	orderID, ok := requirePathID(w, r, "orderId", "Invalid order ID")
	if !ok {
		return
	}
	adminID, ok := requireAdminActor(w, r)
	if !ok {
		return
	}
	var req VoidOrderRequest
	if err := decodeInstitutionBody(r, &req); err != nil {
		httputil.WriteBadRequest(w, "invalid request body")
		return
	}
	order, err := h.institutions.VoidOrder(r.Context(), orderID, req, adminID)
	if err != nil {
		writeInstitutionError(w, err)
		return
	}
	httputil.WriteSuccess(w, order)
}

// CorrectInstitutionEntitlement 人工调额度（正数加、负数减；写 correction 流水）。
func (h *Handler) CorrectInstitutionEntitlement(w http.ResponseWriter, r *http.Request) {
	tenantID, ok := requireInstitutionID(w, r)
	if !ok {
		return
	}
	adminID, ok := requireAdminActor(w, r)
	if !ok {
		return
	}
	var req EntitlementCorrectionRequest
	if err := decodeInstitutionBody(r, &req); err != nil {
		httputil.WriteBadRequest(w, "invalid request body")
		return
	}
	item, err := h.institutions.CorrectEntitlement(r.Context(), tenantID, req, adminID)
	if err != nil {
		writeInstitutionError(w, err)
		return
	}
	httputil.WriteSuccess(w, item)
}

// ListInstitutionOwnerships 归属历史。
func (h *Handler) ListInstitutionOwnerships(w http.ResponseWriter, r *http.Request) {
	tenantID, ok := requireInstitutionID(w, r)
	if !ok {
		return
	}
	adminID, ok := requireAdminActor(w, r)
	if !ok {
		return
	}
	items, err := h.institutions.ListOwnerships(r.Context(), tenantID, adminID)
	if err != nil {
		writeInstitutionError(w, err)
		return
	}
	httputil.WriteSuccess(w, map[string]any{"items": items})
}

// CreateInstitutionOwnership 新增归属。
func (h *Handler) CreateInstitutionOwnership(w http.ResponseWriter, r *http.Request) {
	tenantID, ok := requireInstitutionID(w, r)
	if !ok {
		return
	}
	adminID, ok := requireAdminActor(w, r)
	if !ok {
		return
	}
	var req OwnershipPayload
	if err := decodeInstitutionBody(r, &req); err != nil {
		httputil.WriteBadRequest(w, "invalid request body")
		return
	}
	item, err := h.institutions.CreateOwnership(r.Context(), tenantID, req, adminID)
	if err != nil {
		writeInstitutionError(w, err)
		return
	}
	httputil.WriteSuccess(w, item)
}

// UpdateInstitutionOwnership 修改归属。
func (h *Handler) UpdateInstitutionOwnership(w http.ResponseWriter, r *http.Request) {
	tenantID, ok := requireInstitutionID(w, r)
	if !ok {
		return
	}
	ownershipID, ok := requirePathID(w, r, "ownership_id", "Invalid ownership ID")
	if !ok {
		return
	}
	adminID, ok := requireAdminActor(w, r)
	if !ok {
		return
	}
	var req OwnershipPayload
	if err := decodeInstitutionBody(r, &req); err != nil {
		httputil.WriteBadRequest(w, "invalid request body")
		return
	}
	item, err := h.institutions.UpdateOwnership(r.Context(), tenantID, ownershipID, req, adminID)
	if err != nil {
		writeInstitutionError(w, err)
		return
	}
	httputil.WriteSuccess(w, item)
}

// CloseInstitutionOwnership 关闭归属。
func (h *Handler) CloseInstitutionOwnership(w http.ResponseWriter, r *http.Request) {
	tenantID, ok := requireInstitutionID(w, r)
	if !ok {
		return
	}
	ownershipID, ok := requirePathID(w, r, "ownership_id", "Invalid ownership ID")
	if !ok {
		return
	}
	adminID, ok := requireAdminActor(w, r)
	if !ok {
		return
	}
	var req OwnershipClosePayload
	if err := decodeInstitutionBody(r, &req); err != nil && !errors.Is(err, io.EOF) {
		httputil.WriteBadRequest(w, "invalid request body")
		return
	}
	item, err := h.institutions.CloseOwnership(r.Context(), tenantID, ownershipID, req, adminID)
	if err != nil {
		writeInstitutionError(w, err)
		return
	}
	httputil.WriteSuccess(w, item)
}

// ListInstitutionPeriods 服务期历史。
func (h *Handler) ListInstitutionPeriods(w http.ResponseWriter, r *http.Request) {
	tenantID, ok := requireInstitutionID(w, r)
	if !ok {
		return
	}
	adminID, ok := requireAdminActor(w, r)
	if !ok {
		return
	}
	items, err := h.institutions.ListSubscriptionPeriods(r.Context(), tenantID, adminID)
	if err != nil {
		writeInstitutionError(w, err)
		return
	}
	httputil.WriteSuccess(w, map[string]any{"items": items})
}

// CreateInstitutionPeriod 手工补录服务期。
func (h *Handler) CreateInstitutionPeriod(w http.ResponseWriter, r *http.Request) {
	tenantID, ok := requireInstitutionID(w, r)
	if !ok {
		return
	}
	adminID, ok := requireAdminActor(w, r)
	if !ok {
		return
	}
	var req SubscriptionPeriodPayload
	if err := decodeInstitutionBody(r, &req); err != nil {
		httputil.WriteBadRequest(w, "invalid request body")
		return
	}
	item, err := h.institutions.CreateSubscriptionPeriod(r.Context(), tenantID, req, adminID)
	if err != nil {
		writeInstitutionError(w, err)
		return
	}
	httputil.WriteSuccess(w, item)
}

// UpdateInstitutionPeriod 调整服务期。
func (h *Handler) UpdateInstitutionPeriod(w http.ResponseWriter, r *http.Request) {
	tenantID, ok := requireInstitutionID(w, r)
	if !ok {
		return
	}
	periodID, ok := requirePathID(w, r, "period_id", "Invalid period ID")
	if !ok {
		return
	}
	adminID, ok := requireAdminActor(w, r)
	if !ok {
		return
	}
	var req SubscriptionPeriodPayload
	if err := decodeInstitutionBody(r, &req); err != nil {
		httputil.WriteBadRequest(w, "invalid request body")
		return
	}
	item, err := h.institutions.UpdateSubscriptionPeriod(r.Context(), tenantID, periodID, req, adminID)
	if err != nil {
		writeInstitutionError(w, err)
		return
	}
	httputil.WriteSuccess(w, item)
}

// GetBillingPricing 产品与价格。
func (h *Handler) GetBillingPricing(w http.ResponseWriter, r *http.Request) {
	payload, err := h.institutions.GetPricing(r.Context())
	if err != nil {
		writeInstitutionError(w, err)
		return
	}
	httputil.WriteSuccess(w, payload)
}

// GetBillingChannels 渠道与销售目录。
func (h *Handler) GetBillingChannels(w http.ResponseWriter, r *http.Request) {
	payload, err := h.institutions.GetChannelDirectory(r.Context())
	if err != nil {
		writeInstitutionError(w, err)
		return
	}
	httputil.WriteSuccess(w, payload)
}

// SaveBillingChannel 新增/修改渠道。
func (h *Handler) SaveBillingChannel(w http.ResponseWriter, r *http.Request) {
	adminID, ok := requireAdminActor(w, r)
	if !ok {
		return
	}
	var req ChannelPayload
	if err := decodeInstitutionBody(r, &req); err != nil {
		httputil.WriteBadRequest(w, "invalid request body")
		return
	}
	item, err := h.institutions.SaveChannel(r.Context(), req, adminID)
	if err != nil {
		writeInstitutionError(w, err)
		return
	}
	httputil.WriteSuccess(w, item)
}

// SaveBillingSales 新增/修改销售。
func (h *Handler) SaveBillingSales(w http.ResponseWriter, r *http.Request) {
	adminID, ok := requireAdminActor(w, r)
	if !ok {
		return
	}
	var req SalesPayload
	if err := decodeInstitutionBody(r, &req); err != nil {
		httputil.WriteBadRequest(w, "invalid request body")
		return
	}
	item, err := h.institutions.SaveSales(r.Context(), req, adminID)
	if err != nil {
		writeInstitutionError(w, err)
		return
	}
	httputil.WriteSuccess(w, item)
}

// GetBillingDictionaries 行业与科室字典。
func (h *Handler) GetBillingDictionaries(w http.ResponseWriter, r *http.Request) {
	payload, err := h.institutions.GetDictionaries(r.Context())
	if err != nil {
		writeInstitutionError(w, err)
		return
	}
	httputil.WriteSuccess(w, payload)
}

// CreateBillingDictItem 新增字典项。
func (h *Handler) CreateBillingDictItem(w http.ResponseWriter, r *http.Request) {
	adminID, ok := requireAdminActor(w, r)
	if !ok {
		return
	}
	var req DictItemPayload
	if err := decodeInstitutionBody(r, &req); err != nil {
		httputil.WriteBadRequest(w, "invalid request body")
		return
	}
	req.ID = 0
	item, err := h.institutions.SaveDictItem(r.Context(), r.PathValue("kind"), req, adminID)
	if err != nil {
		writeInstitutionError(w, err)
		return
	}
	httputil.WriteSuccess(w, item)
}

// UpdateBillingDictItem 修改字典项。
func (h *Handler) UpdateBillingDictItem(w http.ResponseWriter, r *http.Request) {
	adminID, ok := requireAdminActor(w, r)
	if !ok {
		return
	}
	itemID, ok := requirePathID(w, r, "id", "Invalid dict item ID")
	if !ok {
		return
	}
	var req DictItemPayload
	if err := decodeInstitutionBody(r, &req); err != nil {
		httputil.WriteBadRequest(w, "invalid request body")
		return
	}
	req.ID = itemID
	item, err := h.institutions.SaveDictItem(r.Context(), r.PathValue("kind"), req, adminID)
	if err != nil {
		writeInstitutionError(w, err)
		return
	}
	httputil.WriteSuccess(w, item)
}

// ToggleBillingDictItem 启停字典项。
func (h *Handler) ToggleBillingDictItem(w http.ResponseWriter, r *http.Request) {
	adminID, ok := requireAdminActor(w, r)
	if !ok {
		return
	}
	itemID, ok := requirePathID(w, r, "id", "Invalid dict item ID")
	if !ok {
		return
	}
	item, err := h.institutions.ToggleDictItem(r.Context(), r.PathValue("kind"), itemID, adminID)
	if err != nil {
		writeInstitutionError(w, err)
		return
	}
	httputil.WriteSuccess(w, item)
}

// ListBillingPayments 收款单列表。
func (h *Handler) ListBillingPayments(w http.ResponseWriter, r *http.Request) {
	adminID, ok := requireAdminActor(w, r)
	if !ok {
		return
	}
	result, err := h.institutions.ListPayments(r.Context(), adminID)
	if err != nil {
		writeInstitutionError(w, err)
		return
	}
	httputil.WriteSuccess(w, result)
}

// CreateBillingPayment 新建收款单（可同时核销多租户多订单）。
func (h *Handler) CreateBillingPayment(w http.ResponseWriter, r *http.Request) {
	adminID, ok := requireAdminActor(w, r)
	if !ok {
		return
	}
	var req CreatePaymentRequest
	if err := decodeInstitutionBody(r, &req); err != nil {
		httputil.WriteBadRequest(w, "invalid request body")
		return
	}
	result, err := h.institutions.CreatePayment(r.Context(), req, adminID)
	if err != nil {
		writeInstitutionError(w, err)
		return
	}
	httputil.WriteSuccess(w, result)
}

// AllocateBillingPayment 给已有收款单追加核销。
func (h *Handler) AllocateBillingPayment(w http.ResponseWriter, r *http.Request) {
	paymentID, ok := requirePathID(w, r, "id", "Invalid payment ID")
	if !ok {
		return
	}
	adminID, ok := requireAdminActor(w, r)
	if !ok {
		return
	}
	var req CreatePaymentRequest
	if err := decodeInstitutionBody(r, &req); err != nil {
		httputil.WriteBadRequest(w, "invalid request body")
		return
	}
	result, err := h.institutions.AllocatePayment(r.Context(), paymentID, req, adminID)
	if err != nil {
		writeInstitutionError(w, err)
		return
	}
	httputil.WriteSuccess(w, result)
}

// ListBillingInvoices 开票记录列表：?tenantId=&status=（同时兼容 tenant_id）。
func (h *Handler) ListBillingInvoices(w http.ResponseWriter, r *http.Request) {
	adminID, ok := requireAdminActor(w, r)
	if !ok {
		return
	}
	query := r.URL.Query()
	tenantID := int64(0)
	if raw := firstQueryValue(query.Get("tenantId"), query.Get("tenant_id")); raw != "" {
		parsed, err := parseTenantIDQuery(raw)
		if err != nil {
			httputil.WriteBadRequest(w, err.Error())
			return
		}
		tenantID = parsed
	}
	result, err := h.institutions.ListInvoices(r.Context(), InvoiceFilter{
		TenantID: tenantID,
		Status:   query.Get("status"),
	}, adminID)
	if err != nil {
		writeInstitutionError(w, err)
		return
	}
	httputil.WriteSuccess(w, result)
}

// CreateBillingInvoice 新建开票记录。
func (h *Handler) CreateBillingInvoice(w http.ResponseWriter, r *http.Request) {
	adminID, ok := requireAdminActor(w, r)
	if !ok {
		return
	}
	var req CreateInvoiceRequest
	if err := decodeInstitutionBody(r, &req); err != nil {
		httputil.WriteBadRequest(w, "invalid request body")
		return
	}
	invoice, err := h.institutions.CreateInvoice(r.Context(), req, adminID)
	if err != nil {
		writeInstitutionError(w, err)
		return
	}
	httputil.WriteSuccess(w, invoice)
}

// IssueBillingInvoice 标记发票已开。
func (h *Handler) IssueBillingInvoice(w http.ResponseWriter, r *http.Request) {
	invoiceID, ok := requirePathID(w, r, "id", "Invalid invoice ID")
	if !ok {
		return
	}
	adminID, ok := requireAdminActor(w, r)
	if !ok {
		return
	}
	var req IssueInvoiceRequest
	if err := decodeInstitutionBody(r, &req); err != nil {
		httputil.WriteBadRequest(w, "invalid request body")
		return
	}
	invoice, err := h.institutions.IssueInvoice(r.Context(), invoiceID, req, adminID)
	if err != nil {
		writeInstitutionError(w, err)
		return
	}
	httputil.WriteSuccess(w, invoice)
}

// VoidBillingInvoice 作废发票。
func (h *Handler) VoidBillingInvoice(w http.ResponseWriter, r *http.Request) {
	invoiceID, ok := requirePathID(w, r, "id", "Invalid invoice ID")
	if !ok {
		return
	}
	adminID, ok := requireAdminActor(w, r)
	if !ok {
		return
	}
	var req VoidInvoiceRequest
	if err := decodeInstitutionBody(r, &req); err != nil {
		httputil.WriteBadRequest(w, "invalid request body")
		return
	}
	invoice, err := h.institutions.VoidInvoice(r.Context(), invoiceID, req, adminID)
	if err != nil {
		writeInstitutionError(w, err)
		return
	}
	httputil.WriteSuccess(w, invoice)
}

// ListInstitutionInvoiceableOrders 机构可开票订单（关联订单开票时选单用）。
func (h *Handler) ListInstitutionInvoiceableOrders(w http.ResponseWriter, r *http.Request) {
	tenantID, ok := requireInstitutionID(w, r)
	if !ok {
		return
	}
	adminID, ok := requireAdminActor(w, r)
	if !ok {
		return
	}
	items, err := h.institutions.ListInvoiceableOrders(r.Context(), tenantID, adminID)
	if err != nil {
		writeInstitutionError(w, err)
		return
	}
	httputil.WriteSuccess(w, map[string]any{"items": items})
}

// ListInstitutionActionLogs 机构变更记录（system_action_logs）。
func (h *Handler) ListInstitutionActionLogs(w http.ResponseWriter, r *http.Request) {
	tenantID, ok := requireInstitutionID(w, r)
	if !ok {
		return
	}
	adminID, ok := requireAdminActor(w, r)
	if !ok {
		return
	}
	items, err := h.institutions.ListActionLogs(r.Context(), tenantID, adminID)
	if err != nil {
		writeInstitutionError(w, err)
		return
	}
	httputil.WriteSuccess(w, map[string]any{"items": items, "total": len(items)})
}

// ListInstitutionAllocatableOrders 机构可核销订单（新建收款单/追加核销时的订单选择器）。
func (h *Handler) ListInstitutionAllocatableOrders(w http.ResponseWriter, r *http.Request) {
	tenantID, ok := requireInstitutionID(w, r)
	if !ok {
		return
	}
	adminID, ok := requireAdminActor(w, r)
	if !ok {
		return
	}
	items, err := h.institutions.ListAllocatableOrders(r.Context(), tenantID, adminID)
	if err != nil {
		writeInstitutionError(w, err)
		return
	}
	httputil.WriteSuccess(w, map[string]any{"items": items})
}

// firstQueryValue 返回第一个非空 query 值（兼容 camelCase 与 snake_case）。
func firstQueryValue(values ...string) string {
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			return strings.TrimSpace(value)
		}
	}
	return ""
}

// ---------------------------------------------------------------------------
// 请求解析与错误映射
// ---------------------------------------------------------------------------

// decodeInstitutionBody 解析 JSON 请求体（限制大小）。
func decodeInstitutionBody(r *http.Request, dst any) error {
	if r.Body == nil {
		return io.EOF
	}
	decoder := json.NewDecoder(io.LimitReader(r.Body, maxInstitutionBodyBytes))
	return decoder.Decode(dst)
}

// requireInstitutionID 解析路径里的机构 id（复用 {id} 参数）。
func requireInstitutionID(w http.ResponseWriter, r *http.Request) (int64, bool) {
	return requirePathID(w, r, "id", "Invalid institution ID")
}

// requirePathID 解析路径参数为字符串形式的正整数 id。
func requirePathID(w http.ResponseWriter, r *http.Request, name, message string) (int64, bool) {
	value, err := strconv.ParseInt(strings.TrimSpace(r.PathValue(name)), 10, 64)
	if err != nil || value <= 0 {
		httputil.WriteBadRequest(w, message)
		return 0, false
	}
	return value, true
}

// writeInstitutionError 把新接口的错误映射成 HTTP 状态码：
// invalid=400 / conflict=409 / not_found=404，其余沿用既有计费错误映射。
func writeInstitutionError(w http.ResponseWriter, err error) {
	var permission *PermissionError
	if errors.As(err, &permission) {
		httputil.WriteError(w, http.StatusForbidden, "FORBIDDEN", permission.Message, nil)
		return
	}
	var conflict *ConflictError
	if errors.As(err, &conflict) {
		httputil.WriteError(w, http.StatusConflict, conflictCode(conflict.Sentinel), conflict.Message, nil)
		return
	}

	switch {
	case errors.Is(err, ErrNoPermission):
		httputil.WriteError(w, http.StatusForbidden, "FORBIDDEN", "没有该机构的数据权限", nil)
	case errors.Is(err, ErrAdminScopeMissing):
		httputil.WriteError(w, http.StatusForbidden, "ADMIN_SCOPE_MISSING", "当前账号未配置归属组织，无法查看机构", nil)
	case errors.Is(err, ErrInstitutionNotFound):
		httputil.WriteError(w, http.StatusNotFound, "INSTITUTION_NOT_FOUND", "机构不存在", nil)
	case errors.Is(err, ErrInstitutionAdminNotFound):
		httputil.WriteError(w, http.StatusNotFound, "ADMIN_NOT_FOUND", "管理员不存在", nil)
	case errors.Is(err, ErrPaymentNotFound):
		httputil.WriteError(w, http.StatusNotFound, "PAYMENT_NOT_FOUND", "收款单不存在", nil)
	case errors.Is(err, ErrInvoiceNotFound):
		httputil.WriteError(w, http.StatusNotFound, "INVOICE_NOT_FOUND", "开票记录不存在", nil)
	case errors.Is(err, ErrInvoiceNoExists):
		httputil.WriteError(w, http.StatusConflict, "INVOICE_NO_EXISTS", "发票号已存在，请核对后重试", nil)
	case errors.Is(err, ErrInvoiceStatusInvalid):
		httputil.WriteError(w, http.StatusConflict, "INVOICE_STATUS_INVALID", "发票当前状态不允许该操作", nil)
	case errors.Is(err, ErrOrderNotInvoiceable):
		httputil.WriteError(w, http.StatusConflict, "ORDER_NOT_INVOICEABLE", "订单已作废/取消/退款，不能开票", nil)
	case errors.Is(err, ErrInvoiceExceedsOrder):
		httputil.WriteError(w, http.StatusBadRequest, "INVOICE_EXCEEDS_ORDER", "开票金额超过订单应收", nil)
	case errors.Is(err, ErrDocumentNotFound):
		httputil.WriteError(w, http.StatusNotFound, "DOCUMENT_NOT_FOUND", "文档不存在", nil)
	case errors.Is(err, ErrPeerNotFound):
		httputil.WriteError(w, http.StatusNotFound, "NOT_FOUND", "记录不存在", nil)
	case errors.Is(err, ErrInstitutionPhoneConflict):
		httputil.WriteError(w, http.StatusConflict, "INSTITUTION_PHONE_USED", "该手机号已被使用", nil)
	case errors.Is(err, ErrInstitutionAdminPhoneConflict):
		httputil.WriteError(w, http.StatusConflict, "ADMIN_PHONE_USED", "该手机号已被使用", nil)
	case errors.Is(err, ErrInstitutionCodeConflict):
		httputil.WriteError(w, http.StatusConflict, "INSTITUTION_CODE_USED", "机构代码已被使用", nil)
	case errors.Is(err, ErrProductAlreadyActive):
		httputil.WriteError(w, http.StatusConflict, "PRODUCT_ALREADY_ACTIVE", "该机构已有此产品，请用「加量」", nil)
	case errors.Is(err, ErrProductNotActive):
		httputil.WriteError(w, http.StatusConflict, "PRODUCT_NOT_ACTIVE", "该机构还没有此产品，请先开通或加产品", nil)
	case errors.Is(err, ErrInstitutionNotOpened):
		httputil.WriteError(w, http.StatusConflict, "INSTITUTION_NOT_OPENED", "机构尚未开通或已过期", nil)
	case errors.Is(err, ErrPeerConflict):
		httputil.WriteError(w, http.StatusConflict, "CONFLICT", "记录冲突，请刷新后重试", nil)
	case errors.Is(err, ErrAllocationConflict):
		httputil.WriteError(w, http.StatusConflict, "ALLOCATION_CONFLICT", "该收款单已核销过此订单（金额与本次不一致）", nil)
	case errors.Is(err, ErrOrderNotAllocatable):
		httputil.WriteError(w, http.StatusConflict, "ORDER_NOT_ALLOCATABLE", "订单当前状态不允许核销", nil)
	case errors.Is(err, ErrPeriodWindowInvalid):
		httputil.WriteError(w, http.StatusBadRequest, "PERIOD_WINDOW_INVALID", "服务期到期日不能早于开始日", nil)
	case errors.Is(err, ErrBundleMismatch):
		httputil.WriteError(w, http.StatusBadRequest, "BUNDLE_MISMATCH", "套餐与勾选的产品不一致", nil)
	case errors.Is(err, ErrSubscriptionPaused):
		httputil.WriteError(w, http.StatusConflict, "SUBSCRIPTION_PAUSED", "该机构服务已暂停，请联系运营恢复", nil)
	case errors.Is(err, ErrOrderNotVoidable):
		httputil.WriteError(w, http.StatusConflict, "ORDER_NOT_VOIDABLE", "订单当前状态不允许作废", nil)
	default:
		writeBillingError(w, err)
	}
}

// conflictCode 给带提示的冲突补一个稳定的错误码。
func conflictCode(sentinel error) string {
	switch {
	case errors.Is(sentinel, ErrInstitutionPhoneConflict):
		return "INSTITUTION_PHONE_USED"
	case errors.Is(sentinel, ErrInstitutionAdminPhoneConflict):
		return "ADMIN_PHONE_USED"
	case errors.Is(sentinel, ErrInstitutionCodeConflict):
		return "INSTITUTION_CODE_USED"
	case errors.Is(sentinel, ErrSubscriptionPaused):
		return "SUBSCRIPTION_PAUSED"
	case errors.Is(sentinel, ErrOrderNotVoidable):
		return "ORDER_NOT_VOIDABLE"
	case errors.Is(sentinel, ErrInvoiceStatusInvalid):
		return "INVOICE_STATUS_INVALID"
	case errors.Is(sentinel, ErrOrderNotInvoiceable):
		return "ORDER_NOT_INVOICEABLE"
	default:
		return "CONFLICT"
	}
}
