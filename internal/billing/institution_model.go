package billing

// 运营端计费「第 1 阶段」的数据契约：机构档案、归属、服务期、字典、渠道与销售、价目与套餐。
//
// 字段命名与前端 apps/operation-console/src/billingMock.ts 保持一致（camelCase），
// 前端 billingService.ts 只需把 mock 换成 HTTP 调用即可继续用同一套类型。
// 数据库列名仍是仓库习惯的 snake_case，映射在本文件的 DTO 与 store 的 SQL 之间完成。

import (
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"
)

// mobilePhonePattern 与前端 billingService.ts 的 ^1\d{10}$ 一致。
var mobilePhonePattern = regexp.MustCompile(`^1\d{10}$`)

// 计费模式（tenants.billing_mode，内部字段，租户侧接口不返回）。
const (
	BillingModeEnforce = "enforce"
	BillingModeObserve = "observe"
)

// 客户类型（tenants.customer_type，本期只落库，运营端暂不展示）。
const (
	CustomerTypeSingle = "single"
	CustomerTypeGroup  = "group"
)

// 租户账号模式（tenants.account_mode）：与前端 Institution.mode 一致。
const (
	AccountModeFormal = "formal"
	AccountModeTrial  = "trial"
)

// 服务期状态与来源。
const (
	PeriodStatusActive     = "active"
	PeriodStatusPending    = "pending"
	PeriodStatusExpired    = "expired"
	PeriodStatusTerminated = "terminated"

	PeriodSourceActivation = "activation"
	PeriodSourceTrial      = "trial"
	PeriodSourceRenewal    = "renewal"
	PeriodSourceManual     = "manual"
)

// 机构服务状态（GET /ops/billing/institutions/{id} 的 status）。
//
// 判定优先级见 institutionStatus：paused > unopened > expired > trial > active。
// 前端「已暂停」徽标直接读这个字段，不再自己按日期猜。
const (
	InstitutionStatusUnopened = "unopened"
	InstitutionStatusTrial    = "trial"
	InstitutionStatusActive   = "active"
	InstitutionStatusPaused   = "paused"
	InstitutionStatusExpired  = "expired"
)

// 字典类型。
const (
	DictKindIndustry   = "industry"
	DictKindDepartment = "department"
)

// 机构列表筛选值（与 billingService.ts 的 InstitutionFilter 一致）。
const (
	InstitutionFilterAll      = "all"
	InstitutionFilterUnopened = "unopened"
	InstitutionFilterExpiring = "expiring"
	// InstitutionFilterTrialExpiring 试用临期（跟单名单）。
	InstitutionFilterTrialExpiring = "trial-expiring"
	InstitutionFilterExpired       = "expired"
	InstitutionFilterUnpaid        = "unpaid"
	InstitutionFilterTrial         = "trial"
)

// 渠道类型（前端 ChannelType）：ops_organizations.type 的取值映射。
// platform -> 灵策自营；agent -> 个人代理；其余 -> 渠道商。
const (
	ChannelTypeSelf      = "灵策自营"
	ChannelTypeAgency    = "渠道商"
	ChannelTypeAgent     = "个人代理"
	opsOrgTypePlatform   = "platform"
	opsOrgTypeAgency     = "agency"
	opsOrgTypeSalesAgent = "agent"
)

// 收款单状态（与迁移 CHECK 一致）。
const (
	PaymentStatusUnallocated = "unallocated"
	PaymentStatusPartial     = "partial"
	PaymentStatusAllocated   = "allocated"
	PaymentStatusVoid        = "void"

	// 前端展示状态。
	paymentStateUnallocated = "未核销"
	paymentStatePartial     = "部分核销"
	paymentStateAllocated   = "已核销"
	paymentStateVoid        = "已作废"
)

// 默认管理员口令：与 tenant 模块 createDefaultTenantAdminTx 的口径一致，
// 运营新建销售账号后由本人首次登录修改。
const defaultOpsAdminPassword = "123456"

// maxActiveProducts 防止异常请求一次开通/续期过多产品（六个产品是当前上限）。
const maxActiveProducts = 32

var (
	// ErrInstitutionNotFound 表示租户不存在（404）。
	ErrInstitutionNotFound = errors.New("billing: institution not found")
	// ErrInstitutionPhoneConflict 表示联系人手机号已被其它机构占用（409）。
	ErrInstitutionPhoneConflict = errors.New("billing: institution contact phone already used")
	// ErrInstitutionCodeConflict 表示机构代码（忽略大小写）已存在且无法自动续号（409）。
	ErrInstitutionCodeConflict = errors.New("billing: institution code already used")
	// ErrInstitutionAdminNotFound 表示管理员不存在或不属于该机构（404）。
	ErrInstitutionAdminNotFound = errors.New("billing: institution admin not found")
	// ErrInstitutionAdminPhoneConflict 表示管理员手机号已被其它账号占用（409）。
	ErrInstitutionAdminPhoneConflict = errors.New("billing: institution admin phone already used")
	// ErrProductAlreadyActive 表示该机构已有该产品（409）。
	ErrProductAlreadyActive = errors.New("billing: product already active for institution")
	// ErrProductNotActive 表示该机构还没有该产品，不能加量/赠送（409）。
	ErrProductNotActive = errors.New("billing: product not active for institution")
	// ErrInstitutionNotOpened 表示机构尚未开通，不能续期/加量/加产品（409）。
	ErrInstitutionNotOpened = errors.New("billing: institution has no active subscription")
	// ErrPeerNotFound 表示渠道/销售/字典项/归属/服务期不存在（404）。
	ErrPeerNotFound = errors.New("billing: related record not found")
	// ErrPeerConflict 表示渠道名/销售手机号/字典名重复等冲突（409）。
	ErrPeerConflict = errors.New("billing: related record conflict")
	// ErrPaymentNotFound 表示收款单不存在（404）。
	ErrPaymentNotFound = errors.New("billing: payment not found")
	// ErrAllocationConflict 表示同一笔收款对同一订单已经核销过（409）。
	ErrAllocationConflict = errors.New("billing: allocation already exists for payment and order")
	// ErrOrderNotAllocatable 表示订单状态不允许核销（400）。
	ErrOrderNotAllocatable = errors.New("billing: order is not allocatable")
	// ErrNoPermission 表示运营账号没有该租户的数据权限（403）。
	ErrNoPermission = errors.New("billing: no permission for tenant")
	// ErrAdminScopeMissing 表示运营账号没有配置归属组织（403）。
	ErrAdminScopeMissing = errors.New("billing: admin org scope is missing")
	// ErrPeriodWindowInvalid 表示服务期起止日不合法（400）。
	ErrPeriodWindowInvalid = errors.New("billing: subscription period window is invalid")
	// ErrBundleMismatch 表示套餐与勾选产品不一致（400）。
	ErrBundleMismatch = errors.New("billing: bundle does not match selected products")
	// ErrSubscriptionPaused 表示机构服务已被暂停，扣量/预检拒绝执行（409）。
	ErrSubscriptionPaused = errors.New("billing: subscription is paused")
	// ErrSubscriptionExpired 表示机构服务期已到期，扣量/预检拒绝执行。
	ErrSubscriptionExpired = errors.New("billing: subscription is expired")
	// ErrOrderNotVoidable 表示订单已收款/已核销/状态不允许作废（409）。
	ErrOrderNotVoidable = errors.New("billing: order is not voidable")
	// ErrInvoiceNotFound 表示开票记录不存在（404）。
	ErrInvoiceNotFound = errors.New("billing: invoice not found")
	// ErrInvoiceNoExists 表示发票号已被其它发票占用（409）。
	ErrInvoiceNoExists = errors.New("billing: invoice number already exists")
	// ErrInvoiceStatusInvalid 表示发票当前状态不允许该操作（409）。
	ErrInvoiceStatusInvalid = errors.New("billing: invoice status does not allow the operation")
	// ErrOrderNotInvoiceable 表示订单已作废/取消/退款，不能开票（409）。
	ErrOrderNotInvoiceable = errors.New("billing: order is not invoiceable")
	// ErrInvoiceExceedsOrder 表示订单累计开票金额超过应收（400）。
	ErrInvoiceExceedsOrder = errors.New("billing: invoice amount exceeds order payable")
)

// ConflictError 是带业务提示的 409 冲突；Unwrap 保留 sentinel，handler 既能按类型给友好文案，
// 也能用 errors.Is 做统一分类。
type ConflictError struct {
	Sentinel error
	Message  string
}

func (e *ConflictError) Error() string { return e.Message }
func (e *ConflictError) Unwrap() error { return e.Sentinel }

// PermissionError 是带业务提示的 403。
type PermissionError struct{ Message string }

func (e *PermissionError) Error() string { return e.Message }

// InstitutionRegion 与前端 region: { province, city, district } 对应（存行政区划代码）。
type InstitutionRegion struct {
	Province string `json:"province"`
	City     string `json:"city"`
	District string `json:"district"`
}

// InstitutionQuota 是某产品某单位的额度 { granted, used, balance }。
// balance 与 product_entitlements.balance 一致（rollover + granted - used），
// 前端「调额度」抽屉用它展示真实可调余额，负向调整时由后端再次兜底校验。
type InstitutionQuota struct {
	Granted Decimal `json:"granted"`
	Used    Decimal `json:"used"`
	Balance Decimal `json:"balance"`
}

// InstitutionAdmin 是机构管理员账号（来自 employees + institution_employee_roles）。
type InstitutionAdmin struct {
	ID        int64  `json:"id"`
	Name      string `json:"name"`
	Phone     string `json:"phone"`
	Role      string `json:"role"`
	LastLogin string `json:"lastLogin"`
	Active    bool   `json:"active"`
}

// InstitutionReceivable 是一张应收单（来自 billing_orders）。
type InstitutionReceivable struct {
	No     string  `json:"no"`
	Amount Decimal `json:"amount"`
	Paid   Decimal `json:"paid"`
}

// InstitutionPayLog 是机构维度的一条收款记录（同一张收款单按机构拆出核销额）。
type InstitutionPayLog struct {
	No      string  `json:"no"`
	Amount  Decimal `json:"amount"`
	Date    string  `json:"date"`
	Method  string  `json:"method"`
	Invoice string  `json:"invoice"`
}

// InstitutionConsumption 是一条消耗明细（来自 entitlement_ledger）。
//
// Type / Source 用来区分「交付扣量」与「人工调额度更正」：
//   - 扣量：type=usage、source=purchase/trial/...，amount 为负；
//   - 更正：source=correction，正向 type=grant、负向 type=usage，amount 带符号。
//
// Deliverable 对扣量是产品名，对更正是「人工调额度」。
type InstitutionConsumption struct {
	At          string  `json:"at"`
	ProductCode string  `json:"productCode"`
	Deliverable string  `json:"deliverable"`
	Unit        string  `json:"unit"`
	Amount      Decimal `json:"amount"`
	OrderNo     string  `json:"orderNo"`
	Type        string  `json:"type"`
	Source      string  `json:"source"`
	Reason      string  `json:"reason"`
}

// InstitutionLog 是变更记录（本轮不做完整审计留痕，接口保留空数组）。
type InstitutionLog struct {
	At   string `json:"at"`
	By   string `json:"by"`
	What string `json:"what"`
}

// InstitutionDocument 是机构档案「文档记录」里的一行（tenant_documents 的读模型）。
// Kind 沿用前端契约的 trial / formal；DocType 是落库值 trial_notice / service_confirm。
type InstitutionDocument struct {
	ID          int64  `json:"id"`
	No          string `json:"no"`
	Kind        string `json:"kind"`
	DocType     string `json:"docType"`
	At          string `json:"at"`
	GeneratedAt string `json:"generatedAt"`
	By          string `json:"by"`
	FileSize    int64  `json:"fileSize"`
}

// Institution 是机构档案，字段与 billingMock.ts 的 Institution 一致。
// products 是 产品代码 -> 单位 -> {granted, used, balance} 的两层对象。
type Institution struct {
	ID           int64                                  `json:"id"`
	Name         string                                 `json:"name"`
	Legal        string                                 `json:"legal"`
	Code         string                                 `json:"code"`
	Industry     string                                 `json:"industry"`
	Departments  []string                               `json:"departments"`
	Region       InstitutionRegion                      `json:"region"`
	Contact      string                                 `json:"contact"`
	Title        string                                 `json:"title"`
	Phone        string                                 `json:"phone"`
	Email        string                                 `json:"email"`
	Address      string                                 `json:"address"`
	Channel      string                                 `json:"channel"`
	Sales        string                                 `json:"sales"`
	Start        string                                 `json:"start"`
	End          string                                 `json:"end"`
	Mode         string                                 `json:"mode"`
	Open         bool                                   `json:"open"`
	Status       string                                 `json:"status"`
	PausedAt     string                                 `json:"pausedAt,omitempty"`
	PausedReason string                                 `json:"pausedReason,omitempty"`
	Products     map[string]map[string]InstitutionQuota `json:"products"`
	Admins       []InstitutionAdmin                     `json:"admins"`
	Receivables  []InstitutionReceivable                `json:"receivables"`
	PayLogs      []InstitutionPayLog                    `json:"payLogs"`
	Consumptions []InstitutionConsumption               `json:"consumptions"`
	Logs         []InstitutionLog                       `json:"logs"`
	Documents    []InstitutionDocument                  `json:"documents"`
	// Contracts 是该机构的全部合同行（服务期 Tab 展示用，含历史/未来）。
	Contracts []InstitutionContract `json:"contracts,omitempty"`
	// TrialCount 历史试用次数（含当前），前端展示「已试用 N 次」。
	TrialCount int `json:"trialCount"`
}

// InstitutionContract 一行合同（服务期）。
type InstitutionContract struct {
	ID        int64    `json:"id"`
	Type      string   `json:"type"`
	Status    string   `json:"status"`
	StartedOn string   `json:"startedOn"`
	ExpiredOn string   `json:"expiredOn"`
	Products  []string `json:"products"`
}

// InstitutionListResult 是 GET /ops/billing/institutions 的 data。
type InstitutionListResult struct {
	Items  []Institution `json:"items"`
	Total  int64         `json:"total"`
	IsMock bool          `json:"isMock"`
}

// InstitutionDetailResult 是 GET /ops/billing/institutions/{id} 的 data。
type InstitutionDetailResult struct {
	Item   *Institution `json:"item"`
	IsMock bool         `json:"isMock"`
}

// InstitutionQuery 是机构列表的筛选条件。
type InstitutionQuery struct {
	Filter  string
	Keyword string
	// Page/PageSize 服务端分页（PageSize<=0 时按默认 20，最多 100）。
	Page     int
	PageSize int
	// OrgID 为运营账号的数据权限范围：0 表示平台账号（可看全部）。
	OrgID int64
}

// normalizeInstitutionQuery 归一化筛选值与关键词。
func normalizeInstitutionQuery(query InstitutionQuery) (InstitutionQuery, error) {
	query.Filter = normalizeEnum(query.Filter)
	if query.Filter == "" {
		query.Filter = InstitutionFilterAll
	}
	switch query.Filter {
	case InstitutionFilterAll, InstitutionFilterUnopened, InstitutionFilterExpiring,
		InstitutionFilterTrialExpiring, InstitutionFilterExpired, InstitutionFilterUnpaid, InstitutionFilterTrial:
	default:
		return InstitutionQuery{}, invalidf("filter must be one of all/unopened/expiring/trial-expiring/expired/unpaid/trial")
	}
	query.Keyword = strings.TrimSpace(query.Keyword)
	if len([]rune(query.Keyword)) > 64 {
		return InstitutionQuery{}, invalidf("keyword must be at most 64 characters")
	}
	if query.Page < 1 {
		query.Page = 1
	}
	if query.PageSize <= 0 {
		query.PageSize = 20
	}
	if query.PageSize > 100 {
		query.PageSize = 100
	}
	return query, nil
}

// CreateInstitutionRequest 是 POST /ops/billing/institutions 的请求体。
// 字段与 billingService.ts 的 CreateInstitutionPayload 一致。
type CreateInstitutionRequest struct {
	Name     string            `json:"name"`
	Legal    string            `json:"legal"`
	Code     string            `json:"code"`
	Industry string            `json:"industry"`
	Region   InstitutionRegion `json:"region"`
	Contact  string            `json:"contact"`
	Title    string            `json:"title"`
	Phone    string            `json:"phone"`
	Email    string            `json:"email"`
	Address  string            `json:"address"`
	Channel  string            `json:"channel"`
	Sales    string            `json:"sales"`
}

// normalize 校验建档入参，规则与 billingService.ts createInstitution 一致。
func (r CreateInstitutionRequest) normalize() (CreateInstitutionRequest, error) {
	r.Name = strings.TrimSpace(r.Name)
	r.Legal = strings.TrimSpace(r.Legal)
	r.Code = strings.TrimSpace(r.Code)
	r.Industry = strings.TrimSpace(r.Industry)
	r.Contact = strings.TrimSpace(r.Contact)
	r.Title = strings.TrimSpace(r.Title)
	r.Phone = strings.TrimSpace(r.Phone)
	r.Email = strings.TrimSpace(r.Email)
	r.Address = strings.TrimSpace(r.Address)
	r.Channel = strings.TrimSpace(r.Channel)
	r.Sales = strings.TrimSpace(r.Sales)
	r.Region.Province = strings.TrimSpace(r.Region.Province)
	r.Region.City = strings.TrimSpace(r.Region.City)
	r.Region.District = strings.TrimSpace(r.Region.District)

	if r.Legal == "" {
		return CreateInstitutionRequest{}, invalidf("请填机构全称")
	}
	if r.Name == "" {
		return CreateInstitutionRequest{}, invalidf("请填机构简称")
	}
	if len([]rune(r.Name)) > 255 || len([]rune(r.Legal)) > 255 {
		return CreateInstitutionRequest{}, invalidf("机构名称过长")
	}
	if len([]rune(r.Code)) > 40 {
		return CreateInstitutionRequest{}, invalidf("机构代码过长")
	}
	if r.Industry == "" {
		return CreateInstitutionRequest{}, invalidf("请选择行业")
	}
	if r.Region.Province == "" || r.Region.City == "" || r.Region.District == "" {
		return CreateInstitutionRequest{}, invalidf("请选择省 / 市 / 区")
	}
	if r.Contact == "" {
		return CreateInstitutionRequest{}, invalidf("请填联系人姓名")
	}
	if len([]rune(r.Contact)) > 100 {
		return CreateInstitutionRequest{}, invalidf("联系人姓名过长")
	}
	if !mobilePhonePattern.MatchString(r.Phone) {
		return CreateInstitutionRequest{}, invalidf("手机号格式不对")
	}
	if len([]rune(r.Email)) > 255 {
		return CreateInstitutionRequest{}, invalidf("邮箱过长")
	}
	if r.Channel == "" {
		return CreateInstitutionRequest{}, invalidf("请选择渠道")
	}
	if r.Sales == "" {
		return CreateInstitutionRequest{}, invalidf("请选择销售")
	}
	return r, nil
}

// ActivationPayment 是开通时的收款选择（已收款 or 挂账）。
type ActivationPayment struct {
	Mode   string  `json:"mode"`
	Amount Decimal `json:"amount"`
	Date   string  `json:"date"`
	Method string  `json:"method"`
}

// FormalActivationRequest 是 POST /ops/billing/institutions/{id}/activate-formal 的请求体。
// 同时接受 billingService.ts 的 camelCase（productCodes/bundleKey/endDate/fromTrial/giftNewcomer）
// 与仓库习惯的 snake_case，两者以 camelCase 优先。
type FormalActivationRequest struct {
	ProductCodes []string          `json:"productCodes"`
	BundleKey    string            `json:"bundleKey"`
	EndDate      string            `json:"endDate"`
	FromTrial    bool              `json:"fromTrial"`
	GiftNewcomer bool              `json:"giftNewcomer"`
	Payment      ActivationPayment `json:"payment"`

	SnakeProductCodes []string `json:"product_codes"`
	SnakeBundleKey    string   `json:"bundle_key"`
	SnakeEndDate      string   `json:"end_date"`
	SnakeFromTrial    *bool    `json:"from_trial"`
	SnakeGiftNewcomer *bool    `json:"gift_newcomer"`
}

// normalize 合并 camelCase / snake_case 入参并做基础校验。
func (r FormalActivationRequest) normalize() (FormalActivationRequest, error) {
	if len(r.ProductCodes) == 0 {
		r.ProductCodes = r.SnakeProductCodes
	}
	if r.BundleKey == "" {
		r.BundleKey = r.SnakeBundleKey
	}
	if r.EndDate == "" {
		r.EndDate = r.SnakeEndDate
	}
	if r.SnakeFromTrial != nil {
		r.FromTrial = *r.SnakeFromTrial
	}
	if r.SnakeGiftNewcomer != nil {
		r.GiftNewcomer = *r.SnakeGiftNewcomer
	}
	r.BundleKey = normalizeEnum(r.BundleKey)
	r.EndDate = strings.TrimSpace(r.EndDate)

	codes := make([]string, 0, len(r.ProductCodes))
	seen := make(map[string]struct{}, len(r.ProductCodes))
	for _, raw := range r.ProductCodes {
		code := normalizeEnum(raw)
		if code == "" {
			continue
		}
		if _, exists := seen[code]; exists {
			continue
		}
		seen[code] = struct{}{}
		codes = append(codes, code)
	}
	r.ProductCodes = codes
	if len(codes) == 0 {
		return FormalActivationRequest{}, invalidf("至少选一个产品")
	}
	if len(codes) > maxActiveProducts {
		return FormalActivationRequest{}, invalidf("产品数量过多")
	}
	if r.EndDate == "" {
		return FormalActivationRequest{}, invalidf("请填到期日")
	}

	mode := normalizeEnum(r.Payment.Mode)
	switch mode {
	case "", "unpaid":
		r.Payment.Mode = "unpaid"
		r.Payment.Amount = ""
	case "paid":
		r.Payment.Mode = "paid"
		if _, err := parseAmountCents(r.Payment.Amount); err != nil {
			return FormalActivationRequest{}, invalidf("实收金额 %s", err)
		}
	default:
		return FormalActivationRequest{}, invalidf("payment.mode must be paid/unpaid")
	}
	r.Payment.Date = strings.TrimSpace(r.Payment.Date)
	r.Payment.Method = strings.TrimSpace(r.Payment.Method)
	return r, nil
}

// TrialActivationRequest 是 POST /ops/billing/institutions/{id}/activate-trial 的请求体。
type TrialActivationRequest struct {
	ProductCodes []string `json:"productCodes"`
	Days         int      `json:"days"`

	SnakeProductCodes []string `json:"product_codes"`
}

// normalize 合并入参并校验试用天数。
func (r TrialActivationRequest) normalize() (TrialActivationRequest, error) {
	if len(r.ProductCodes) == 0 {
		r.ProductCodes = r.SnakeProductCodes
	}
	codes := make([]string, 0, len(r.ProductCodes))
	seen := make(map[string]struct{}, len(r.ProductCodes))
	for _, raw := range r.ProductCodes {
		code := normalizeEnum(raw)
		if code == "" {
			continue
		}
		if _, exists := seen[code]; exists {
			continue
		}
		seen[code] = struct{}{}
		codes = append(codes, code)
	}
	r.ProductCodes = codes
	if len(codes) == 0 {
		return TrialActivationRequest{}, invalidf("至少选一个产品")
	}
	if len(codes) > maxActiveProducts {
		return TrialActivationRequest{}, invalidf("产品数量过多")
	}
	if r.Days < 1 {
		return TrialActivationRequest{}, invalidf("试用天数至少 1 天")
	}
	if r.Days > 3650 {
		return TrialActivationRequest{}, invalidf("试用天数最多 3650 天")
	}
	return r, nil
}

// IncrementRequest 是加量/赠送的请求体：{ code, packs }。
type IncrementRequest struct {
	Code  string `json:"code"`
	Packs int    `json:"packs"`

	CamelProductCode string `json:"productCode"`
	SnakeProductCode string `json:"product_code"`
}

// normalize 合并产品代码别名并校验包数（1..maxGiftPacks）。
func (r IncrementRequest) normalize() (IncrementRequest, error) {
	if r.Code == "" {
		r.Code = r.CamelProductCode
	}
	if r.Code == "" {
		r.Code = r.SnakeProductCode
	}
	r.Code = normalizeEnum(r.Code)
	if r.Code == "" {
		return IncrementRequest{}, invalidf("请选择产品")
	}
	if r.Packs < 1 {
		return IncrementRequest{}, invalidf("包数至少 1")
	}
	if r.Packs > maxGiftPacks {
		return IncrementRequest{}, invalidf("单次最多 %d 个增量包", maxGiftPacks)
	}
	return r, nil
}

// GiftIncrementRequest 是赠送增量包的请求体。
type GiftIncrementRequest struct {
	Code     string `json:"code"`
	Packs    int    `json:"packs"`
	Reason   string `json:"reason"`
	Approver string `json:"approver"`

	CamelProductCode string `json:"productCode"`
	SnakeProductCode string `json:"product_code"`
}

// normalize 合并产品代码别名并校验赠送规则：原因必填，>1 包必须有审批人。
func (r GiftIncrementRequest) normalize() (GiftIncrementRequest, error) {
	if r.Code == "" {
		r.Code = r.CamelProductCode
	}
	if r.Code == "" {
		r.Code = r.SnakeProductCode
	}
	r.Code = normalizeEnum(r.Code)
	r.Reason = strings.TrimSpace(r.Reason)
	r.Approver = strings.TrimSpace(r.Approver)
	if r.Code == "" {
		return GiftIncrementRequest{}, invalidf("请选择产品")
	}
	if r.Packs < 1 {
		return GiftIncrementRequest{}, invalidf("包数至少 1")
	}
	if r.Packs > maxGiftPacks {
		return GiftIncrementRequest{}, invalidf("单次最多 %d 个增量包", maxGiftPacks)
	}
	if r.Reason == "" {
		return GiftIncrementRequest{}, invalidf("请填赠送原因")
	}
	if len([]rune(r.Reason)) > 500 {
		return GiftIncrementRequest{}, invalidf("赠送原因过长")
	}
	if r.Packs > 1 && r.Approver == "" {
		return GiftIncrementRequest{}, invalidf("超过 1 个增量包需要审批人")
	}
	return r, nil
}

// RenewRequest 是续期请求体：{ mode: year|custom, endDate }。
type RenewRequest struct {
	Mode    string `json:"mode"`
	EndDate string `json:"endDate"`

	SnakeEndDate string `json:"end_date"`
}

// normalize 合并到期日别名并校验续期方式。
func (r RenewRequest) normalize() (RenewRequest, error) {
	if r.EndDate == "" {
		r.EndDate = r.SnakeEndDate
	}
	r.EndDate = strings.TrimSpace(r.EndDate)
	r.Mode = normalizeEnum(r.Mode)
	if r.Mode == "" {
		r.Mode = "year"
	}
	if r.Mode != "year" && r.Mode != "custom" {
		return RenewRequest{}, invalidf("mode must be year/custom")
	}
	if r.Mode == "custom" {
		if r.EndDate == "" {
			return RenewRequest{}, invalidf("请填新到期日")
		}
		if _, err := parseDateOnly(r.EndDate); err != nil {
			return RenewRequest{}, invalidf("新到期日必须是 YYYY-MM-DD")
		}
	} else {
		r.EndDate = ""
	}
	return r, nil
}

// AddProductRequest 是加产品请求体：{ code }。
type AddProductRequest struct {
	Code string `json:"code"`

	CamelProductCode string `json:"productCode"`
	SnakeProductCode string `json:"product_code"`
}

// normalize 合并产品代码别名。
func (r AddProductRequest) normalize() (AddProductRequest, error) {
	if r.Code == "" {
		r.Code = r.CamelProductCode
	}
	if r.Code == "" {
		r.Code = r.SnakeProductCode
	}
	r.Code = normalizeEnum(r.Code)
	if r.Code == "" {
		return AddProductRequest{}, invalidf("请选择产品")
	}
	return r, nil
}

// SetTrialProductsRequest 是调整试用产品集合请求体：{ productCodes }。
type SetTrialProductsRequest struct {
	ProductCodes      []string `json:"productCodes"`
	SnakeProductCodes []string `json:"product_codes"`
}

func (r SetTrialProductsRequest) normalize() (SetTrialProductsRequest, error) {
	if len(r.ProductCodes) == 0 {
		r.ProductCodes = r.SnakeProductCodes
	}
	codes := make([]string, 0, len(r.ProductCodes))
	seen := make(map[string]struct{}, len(r.ProductCodes))
	for _, raw := range r.ProductCodes {
		code := normalizeEnum(raw)
		if code == "" {
			continue
		}
		if _, exists := seen[code]; exists {
			continue
		}
		seen[code] = struct{}{}
		codes = append(codes, code)
	}
	r.ProductCodes = codes
	if len(codes) == 0 {
		return SetTrialProductsRequest{}, invalidf("至少选一个产品")
	}
	if len(codes) > maxActiveProducts {
		return SetTrialProductsRequest{}, invalidf("产品数量过多")
	}
	return r, nil
}

// ExtendTrialRequest 是延长试用请求体：{ days }。
type ExtendTrialRequest struct {
	Days int `json:"days"`
}

// normalize 校验延长天数。
func (r ExtendTrialRequest) normalize() (ExtendTrialRequest, error) {
	if r.Days < 1 {
		return ExtendTrialRequest{}, invalidf("延长天数至少 1 天")
	}
	if r.Days > 3650 {
		return ExtendTrialRequest{}, invalidf("延长天数最多 3650 天")
	}
	return r, nil
}

// ReceivePaymentRequest 是机构维度收款请求体（billingService.ts receivePayment 的 payload）。
type ReceivePaymentRequest struct {
	Amount Decimal `json:"amount"`
	Date   string  `json:"date"`
	Method string  `json:"method"`
	Memo   string  `json:"memo"`
}

// normalize 校验收款金额与到账日。
func (r ReceivePaymentRequest) normalize() (ReceivePaymentRequest, error) {
	cents, err := parseAmountCents(r.Amount)
	if err != nil {
		return ReceivePaymentRequest{}, invalidf("金额 %s", err)
	}
	if cents <= 0 {
		return ReceivePaymentRequest{}, invalidf("金额要大于 0")
	}
	r.Date = strings.TrimSpace(r.Date)
	if r.Date == "" {
		return ReceivePaymentRequest{}, invalidf("请填到账日")
	}
	if _, err := parseDateOnly(r.Date); err != nil {
		return ReceivePaymentRequest{}, invalidf("到账日必须是 YYYY-MM-DD")
	}
	r.Method = strings.TrimSpace(r.Method)
	if r.Method == "" {
		r.Method = "对公转账"
	}
	if len([]rune(r.Method)) > 64 {
		return ReceivePaymentRequest{}, invalidf("收款方式过长")
	}
	r.Memo = strings.TrimSpace(r.Memo)
	if len([]rune(r.Memo)) > 500 {
		return ReceivePaymentRequest{}, invalidf("备注过长")
	}
	return r, nil
}

// 渠道 / 销售 / 字典 / 价目 / 收款 DTO。

// ChannelSales 是渠道下的一个销售（billingMock.ts 的 ChannelSales）。
type ChannelSales struct {
	ID          int64  `json:"id"`
	Name        string `json:"name"`
	Phone       string `json:"phone"`
	Active      bool   `json:"active"`
	ChannelID   int64  `json:"channelId,omitempty"`
	ChannelName string `json:"channelName,omitempty"`
}

// Channel 是渠道（billingMock.ts 的 Channel）。
type Channel struct {
	ID     int64          `json:"id"`
	Name   string         `json:"name"`
	Type   string         `json:"type"`
	Active bool           `json:"active"`
	Sales  []ChannelSales `json:"sales"`
}

// ChannelWithStats 是渠道 + 机构数。
type ChannelWithStats struct {
	Channel
	InstitutionCount int64 `json:"institutionCount"`
}

// SalesWithStats 是销售 + 所属渠道 + 机构数。
type SalesWithStats struct {
	ChannelSales
	ChannelID        int64  `json:"channelId"`
	ChannelName      string `json:"channelName"`
	InstitutionCount int64  `json:"institutionCount"`
}

// ChannelDirectory 是 GET /ops/billing/channels 的 data。
type ChannelDirectory struct {
	Channels []ChannelWithStats `json:"channels"`
	Sales    []SalesWithStats   `json:"sales"`
	IsMock   bool               `json:"isMock"`
}

// ChannelPayload 是 saveChannel 的请求体。
type ChannelPayload struct {
	ID     int64  `json:"id"`
	Name   string `json:"name"`
	Type   string `json:"type"`
	Active *bool  `json:"active"`
}

// SalesPayload 是 saveSales 的请求体。
type SalesPayload struct {
	ID        int64  `json:"id"`
	ChannelID int64  `json:"channelId"`
	Name      string `json:"name"`
	Phone     string `json:"phone"`
	Active    *bool  `json:"active"`

	SnakeChannelID int64 `json:"channel_id"`
}

// DictItem 是行业/科室字典项。
type DictItem struct {
	ID     int64  `json:"id"`
	Name   string `json:"name"`
	Order  int    `json:"order"`
	Active bool   `json:"active"`
	Type   string `json:"type,omitempty"` // clinical / functional（仅科室字典）
}

// DictionaryPayload 是 GET /ops/billing/dictionaries 的 data。
type DictionaryPayload struct {
	Industries  []DictItem `json:"industries"`
	Departments []DictItem `json:"departments"`
	IsMock      bool       `json:"isMock"`
}

// DictItemPayload 是保存字典项的请求体。
type DictItemPayload struct {
	ID    int64  `json:"id"`
	Name  string `json:"name"`
	Order int    `json:"order"`
	Code  string `json:"code,omitempty"` // 科室编码（新增科室必填）
	Type  string `json:"type,omitempty"` // clinical / functional（科室必填）
}

// ProductDefinition 是价目表的一行（billingMock.ts 的 ProductDefinition）。
type ProductDefinition struct {
	Code     string             `json:"code"`
	Name     string             `json:"name"`
	Price    Decimal            `json:"price"`
	Units    map[string]Decimal `json:"units"`
	Inc      map[string]Decimal `json:"inc"`
	IncPrice Decimal            `json:"incPrice"`
}

// BundleDefinition 是套餐（billingMock.ts 的 BundleDefinition + singlePrice）。
type BundleDefinition struct {
	Key         string   `json:"key"`
	Name        string   `json:"name"`
	Price       Decimal  `json:"price"`
	Items       []string `json:"items"`
	SinglePrice Decimal  `json:"singlePrice"`
}

// PricingPayload 是 GET /ops/billing/pricing 的 data。
type PricingPayload struct {
	Products []ProductDefinition `json:"products"`
	Bundles  []BundleDefinition  `json:"bundles"`
	IsMock   bool                `json:"isMock"`
}

// PaymentRecord 是财务收款单列表的一行（billingMock.ts 的 PaymentRecord + 扩展字段）。
type PaymentRecord struct {
	ID              int64   `json:"id"`
	No              string  `json:"no"`
	Payer           string  `json:"payer"`
	Amount          Decimal `json:"amount"`
	Date            string  `json:"date"`
	Method          string  `json:"method"`
	Settled         Decimal `json:"settled"`
	ReceivableTotal Decimal `json:"receivableTotal"`
	State           string  `json:"state"`
	Status          string  `json:"status"`
	Note            string  `json:"note"`
	VoucherNo       string  `json:"voucher_no"`
	// Allocations 是核销明细；收款单列表直接带出，前端展开行即可查看，无需二次请求。
	Allocations []PaymentAllocationRecord `json:"allocations,omitempty"`
}

// PaymentListResult 是 GET /ops/billing/payments 的 data。
type PaymentListResult struct {
	Items  []PaymentRecord `json:"items"`
	IsMock bool            `json:"isMock"`
}

// PaymentAllocationInput 是一条核销指令：订单 + 金额。
type PaymentAllocationInput struct {
	OrderID int64   `json:"orderId"`
	Amount  Decimal `json:"amount"`

	SnakeOrderID int64 `json:"order_id"`
}

// CreatePaymentRequest 是 POST /ops/billing/payments 的请求体。
// Allocations 支持跨租户、可部分；amount 是收款金额，核销合计不得超过它。
type CreatePaymentRequest struct {
	PayerName   string                   `json:"payer"`
	Amount      Decimal                  `json:"amount"`
	ReceivedOn  string                   `json:"date"`
	Method      string                   `json:"method"`
	VoucherNo   string                   `json:"voucherNo"`
	Note        string                   `json:"memo"`
	Allocations []PaymentAllocationInput `json:"allocations"`
}

// PaymentAllocationRecord 是核销明细（含订单与机构信息，便于前端展示）。
type PaymentAllocationRecord struct {
	ID         int64   `json:"id"`
	PaymentID  int64   `json:"paymentId"`
	OrderID    int64   `json:"orderId"`
	OrderNo    string  `json:"orderNo"`
	TenantID   int64   `json:"tenantId"`
	TenantName string  `json:"tenantName"`
	Amount     Decimal `json:"amount"`
}

// PaymentCreateResult 是创建收款单的返回：收款单 + 核销明细 + 本次未核销余额。
type PaymentCreateResult struct {
	Payment            PaymentRecord             `json:"payment"`
	Allocations        []PaymentAllocationRecord `json:"allocations"`
	UnallocatedBalance Decimal                   `json:"unallocatedBalance"`
}

// SubscriptionPeriod 是服务期历史的一行。
type SubscriptionPeriod struct {
	ID        int64  `json:"id"`
	TenantID  int64  `json:"tenantId"`
	StartedOn string `json:"startedOn"`
	ExpiredOn string `json:"expiredOn"`
	Status    string `json:"status"`
	Source    string `json:"source"`
	CreatedAt string `json:"createdAt"`
	UpdatedAt string `json:"updatedAt"`
}

// Ownership 是归属历史的一行。
type Ownership struct {
	ID            int64  `json:"id"`
	TenantID      int64  `json:"tenantId"`
	OrgID         int64  `json:"orgId"`
	OrgName       string `json:"orgName"`
	SalesAdminID  int64  `json:"salesAdminId"`
	SalesName     string `json:"salesName"`
	Source        string `json:"source"`
	EffectiveFrom string `json:"effectiveFrom"`
	EffectiveTo   string `json:"effectiveTo"`
	CreatedAt     string `json:"createdAt"`
}

// OwnershipPayload 是新增/修改归属的请求体。
type OwnershipPayload struct {
	OrgID         int64  `json:"orgId"`
	SalesAdminID  int64  `json:"salesAdminId"`
	Source        string `json:"source"`
	EffectiveFrom string `json:"effectiveFrom"`

	SnakeOrgID         int64  `json:"org_id"`
	SnakeSalesAdminID  int64  `json:"sales_admin_id"`
	SnakeEffectiveFrom string `json:"effective_from"`
}

// OwnershipClosePayload 是关闭归属的请求体。
type OwnershipClosePayload struct {
	EffectiveTo string `json:"effectiveTo"`
}

// SubscriptionPeriodPayload 是手工新增/调整服务期的请求体。
type SubscriptionPeriodPayload struct {
	StartedOn string `json:"startedOn"`
	ExpiredOn string `json:"expiredOn"`
	Status    string `json:"status"`
	Source    string `json:"source"`

	SnakeStartedOn string `json:"started_on"`
	SnakeExpiredOn string `json:"expired_on"`
}

// ---------------------------------------------------------------------------
// 纯函数：机构代码、日期、额度折算
// ---------------------------------------------------------------------------

// institutionPinyinInitial 与 billingMock.ts 的 PINYIN_INITIAL 一致：
// 机构代码规则要求「名字拼音首字母最多 4 位」，不引入第三方拼音库。
var institutionPinyinInitial = map[rune]string{
	'上': "S", '海': "H", '张': "Z", '医': "Y", '生': "S", '口': "K", '腔': "Q", '门': "M", '诊': "Z", '部': "B",
	'杭': "H", '州': "Z", '王': "W", '院': "Y", '长': "C", '中': "Z", '北': "B", '京': "J", '李': "L", '眼': "Y",
	'科': "K", '苏': "S", '赵': "Z", '综': "Z", '合': "H", '陈': "C", '美': "M", '容': "R", '测': "C", '试': "S",
	'华': "H", '康': "K", '仁': "R", '和': "H", '泰': "T", '安': "A", '宁': "N", '福': "F", '瑞': "R", '佳': "J",
	'鑫': "X", '盛': "S", '隆': "L", '兴': "X", '达': "D", '宏': "H", '远': "Y", '青': "Q", '天': "T", '明': "M",
	'心': "X", '妇': "F", '儿': "E", '保': "B", '健': "J", '复': "F", '体': "T", '检': "J", '护': "H", '理': "L",
	'养': "Y", '老': "L", '新': "X", '大': "D", '永': "Y",
}

// institutionPinyinCode 取名称前 4 个汉字的拼音首字母；纯英文/数字名取前 4 位；
// 两者都没有时返回 GENR（与 billingMock.ts 的 pinyinCode 一致）。
func institutionPinyinCode(name string) string {
	runes := []rune(strings.TrimSpace(name))
	letters := make([]string, 0, 4)
	for _, char := range runes {
		if char >= '\u4e00' && char <= '\u9fa5' {
			initial := institutionPinyinInitial[char]
			if initial == "" {
				initial = "X"
			}
			letters = append(letters, initial)
			if len(letters) == 4 {
				break
			}
		}
	}
	if len(letters) > 0 {
		return strings.Join(letters, "")
	}
	ascii := make([]rune, 0, 4)
	for _, char := range runes {
		if (char >= 'A' && char <= 'Z') || (char >= 'a' && char <= 'z') || (char >= '0' && char <= '9') {
			ascii = append(ascii, char)
			if len(ascii) == 4 {
				break
			}
		}
	}
	if len(ascii) > 0 {
		return strings.ToUpper(string(ascii))
	}
	return "GENR"
}

// nextInstitutionCode 按「LC-<拼音≤4>-<4 位流水>」生成下一个未被占用的代码。
// existing 传该前缀下的已有代码（大小写不敏感）；同前缀独立递增，撞号自动 +1。
func nextInstitutionCode(name string, existing []string) string {
	prefix := "LC-" + institutionPinyinCode(name)
	maxSequence := 0
	for _, code := range existing {
		upper := strings.ToUpper(strings.TrimSpace(code))
		if !strings.HasPrefix(upper, strings.ToUpper(prefix)+"-") {
			continue
		}
		suffix := upper[len(prefix)+1:]
		if len(suffix) != 4 {
			continue
		}
		value, err := strconv.Atoi(suffix)
		if err != nil || value <= 0 {
			continue
		}
		if value > maxSequence {
			maxSequence = value
		}
	}
	return fmt.Sprintf("%s-%04d", prefix, maxSequence+1)
}

// validateInstitutionCode 校验手工填写的机构代码：非空、≤40 字符、只允许字母数字与 -_。
func validateInstitutionCode(code string) error {
	code = strings.TrimSpace(code)
	if code == "" {
		return nil
	}
	if len([]rune(code)) > 40 {
		return invalidf("机构代码最多 40 个字符")
	}
	for _, char := range code {
		switch {
		case char >= 'A' && char <= 'Z',
			char >= 'a' && char <= 'z',
			char >= '0' && char <= '9',
			char == '-', char == '_':
		default:
			return invalidf("机构代码只能包含字母、数字、-、_")
		}
	}
	return nil
}

// parseDateOnly 把 YYYY-MM-DD 解析成 UTC 零点日期。
func parseDateOnly(raw string) (time.Time, error) {
	return time.Parse("2006-01-02", strings.TrimSpace(raw))
}

// formatDateOnly 输出 YYYY-MM-DD；零值返回空串。
func formatDateOnly(value time.Time) string {
	if value.IsZero() {
		return ""
	}
	return value.Format("2006-01-02")
}

// scaleQuotaAmount 按 num/den 折算额度（四舍五入到 2 位小数），最少 1。
// 用于试用额度与中途加产品的按天折算：标准档 × 天数 ÷ 365。
func scaleQuotaAmount(base Decimal, num, den int64) (Decimal, error) {
	value, err := parseDecimalFloat(base)
	if err != nil {
		return "", err
	}
	if den <= 0 {
		return "", fmt.Errorf("denominator must be positive")
	}
	if num < 0 {
		return "", fmt.Errorf("numerator must not be negative")
	}
	scaled := math.Round(value*float64(num)/float64(den)*100) / 100
	if scaled < 1 {
		scaled = 1
	}
	return formatQuotaFloat(scaled), nil
}

// scaleQuotaWhole 按 num/den 折算额度并四舍五入取整，最少 1。
//
// 只用于「按天折算」的试用额度：标准档 × 试用天数 ÷ 365。
// 取整发生在发放时（存储、页面、PDF 三处一致），避免库里有 95.89 份而页面显示 96 份。
func scaleQuotaWhole(base Decimal, num, den int64) (Decimal, error) {
	value, err := parseDecimalFloat(base)
	if err != nil {
		return "", err
	}
	if den <= 0 {
		return "", fmt.Errorf("denominator must be positive")
	}
	if num < 0 {
		return "", fmt.Errorf("numerator must not be negative")
	}
	granted := math.Round(value * float64(num) / float64(den))
	if granted < 1 {
		granted = 1
	}
	return Decimal(strconv.FormatInt(int64(granted), 10)), nil
}

// multiplyQuotaAmount 按包数放大额度：base × packs。
func multiplyQuotaAmount(base Decimal, packs int64) (Decimal, error) {
	value, err := parseDecimalFloat(base)
	if err != nil {
		return "", err
	}
	if packs <= 0 {
		return "", fmt.Errorf("packs must be positive")
	}
	return formatQuotaFloat(math.Round(value*float64(packs)*100) / 100), nil
}

// parseDecimalFloat 把 Decimal 解析成 float64（额度折算用，金额仍走整数分）。
func parseDecimalFloat(value Decimal) (float64, error) {
	text := strings.TrimSpace(string(value))
	if text == "" {
		return 0, nil
	}
	number, err := strconv.ParseFloat(text, 64)
	if err != nil || math.IsInf(number, 0) || math.IsNaN(number) {
		return 0, fmt.Errorf("invalid decimal %q", text)
	}
	return number, nil
}

// formatQuotaFloat 把额度格成最多 2 位小数的 Decimal 文本。
func formatQuotaFloat(value float64) Decimal {
	return Decimal(strconv.FormatFloat(math.Round(value*100)/100, 'f', -1, 64))
}

// parseQuotaMap 把价目表/订单里的 JSONB 额度对象解析成 单位 -> 数量，
// 并按单位名排序返回（保证发放流水顺序稳定）。
func parseQuotaMap(quantities Quantities) ([]quotaPair, error) {
	raw := make(map[string]Decimal)
	if len(quantities) > 0 {
		if err := json.Unmarshal(quantities, &raw); err != nil {
			return nil, fmt.Errorf("decode quotas: %w", err)
		}
	}
	units := make([]string, 0, len(raw))
	for unit := range raw {
		units = append(units, unit)
	}
	sort.Strings(units)
	pairs := make([]quotaPair, 0, len(units))
	for _, unit := range units {
		amount := raw[unit]
		if strings.TrimSpace(string(amount)) == "" {
			amount = "0"
		}
		pairs = append(pairs, quotaPair{Unit: normalizeEnum(unit), Amount: amount})
	}
	return pairs, nil
}

// quotaPair 是一个单位与数量的组合。
type quotaPair struct {
	Unit   string
	Amount Decimal
}

// daysUntil 返回到期日距 today 的天数（负数表示已过期）。
func daysUntil(expiredOn, today time.Time) int {
	return int(math.Round(expiredOn.Sub(today).Hours() / 24))
}

// addDaysDate 返回 date + days。
func addDaysDate(date time.Time, days int) time.Time {
	return date.AddDate(0, 0, days)
}

// distributeItemPrices 把订单总额按各 item 的原价比例分摊到 item.price 上，
// 保证 Σ items[].price == total_amount（套餐价、按天续费等"总额不等于单项相加"的场景）。
// 分摊用整数分做最大余数法，避免出现"行金额加起来差 1 分"。
func distributeItemPrices(items []OrderItem, total Decimal) error {
	if len(items) == 0 {
		return nil
	}
	totalCents, err := parseAmountCents(total)
	if err != nil {
		return err
	}
	weights := make([]int64, len(items))
	var weightSum int64
	for index, item := range items {
		cents, err := parseAmountCents(item.Price)
		if err != nil {
			return err
		}
		weights[index] = cents
		weightSum += cents
	}
	if weightSum == 0 {
		// 没有价格权重（例如赠送单）：把总额平均分摊，余数给最后一行。
		per := totalCents / int64(len(items))
		for index := range items {
			items[index].Price = centsToDecimal(per)
		}
		items[len(items)-1].Price = centsToDecimal(per + totalCents - per*int64(len(items)))
		return nil
	}

	assigned := int64(0)
	for index := range items {
		share := int64(0)
		if index == len(items)-1 {
			share = totalCents - assigned
		} else {
			share = int64(math.Round(float64(totalCents) * float64(weights[index]) / float64(weightSum)))
			assigned += share
		}
		items[index].Price = centsToDecimal(share)
	}
	return nil
}
