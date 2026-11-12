package billing

// 开票记录的数据模型与入参归一化。
//
// 规则口径：
//   - 一张发票只归属一个机构；order_id 可空（手填金额开票），关联订单时累计开票金额不能超过订单应收；
//   - 已作废订单（void / cancelled / refunded）不允许开票；
//   - 发票号在「标记已开」时落库，非空时全局唯一；待开票阶段允许为空；
//   - 作废的发票不计入订单累计开票金额，可重新开票。

import (
	"fmt"
	"strings"
)

const (
	InvoiceStatusApplied = "applied"
	InvoiceStatusIssued  = "issued"
	InvoiceStatusVoided  = "voided"

	InvoiceTypeNormal  = "增值税普通"
	InvoiceTypeSpecial = "增值税专用"

	invoiceStatusAppliedLabel = "待开票"
)

// InvoiceStatuses 是允许的状态枚举（列表过滤用）。
var InvoiceStatuses = []string{InvoiceStatusApplied, InvoiceStatusIssued, InvoiceStatusVoided}

// Invoice 是开票记录的一行（列表与详情共用）。
type Invoice struct {
	ID          int64   `json:"id"`
	InvoiceNo   string  `json:"invoiceNo"`
	TenantID    int64   `json:"tenantId"`
	TenantName  string  `json:"tenantName"`
	OrderID     *int64  `json:"orderId"`
	OrderNo     string  `json:"orderNo"`
	Amount      Decimal `json:"amount"`
	InvoiceType string  `json:"invoiceType"`
	Title       string  `json:"title"`
	TaxNo       string  `json:"taxNo"`
	Status      string  `json:"status"`
	IssuedOn    string  `json:"issuedOn"`
	Note        string  `json:"note"`
	CreatedBy   *int64  `json:"createdBy"`
	CreatedAt   string  `json:"createdAt"`
	UpdatedAt   string  `json:"updatedAt"`
}

// InvoiceListResult 是 GET /ops/billing/invoices 的 data。
type InvoiceListResult struct {
	Items  []Invoice `json:"items"`
	Total  int64     `json:"total"`
	IsMock bool      `json:"isMock"`
}

// InvoiceFilter 是开票记录列表的过滤条件。
type InvoiceFilter struct {
	TenantID int64
	Status   string
	// OrgID 是运营账号的数据权限范围：0 表示平台账号（可看全部）。
	OrgID int64
}

// normalizeInvoiceFilter 归一化开票记录过滤条件。
func normalizeInvoiceFilter(filter InvoiceFilter) (InvoiceFilter, error) {
	if filter.TenantID < 0 {
		return InvoiceFilter{}, invalidf("tenantId must be a positive integer")
	}
	status := normalizeEnum(filter.Status)
	if status != "" && !isValidInvoiceStatus(status) {
		return InvoiceFilter{}, invalidf("status must be one of applied/issued/voided")
	}
	filter.Status = status
	return filter, nil
}

// isValidInvoiceStatus 判断发票状态是否合法。
func isValidInvoiceStatus(status string) bool {
	for _, candidate := range InvoiceStatuses {
		if status == candidate {
			return true
		}
	}
	return false
}

// CreateInvoiceRequest 是 POST /ops/billing/invoices 的请求体。
type CreateInvoiceRequest struct {
	TenantID    int64   `json:"tenantId"`
	OrderID     int64   `json:"orderId"`
	Amount      Decimal `json:"amount"`
	InvoiceType string  `json:"invoiceType"`
	Title       string  `json:"title"`
	TaxNo       string  `json:"taxNo"`
	InvoiceNo   string  `json:"invoiceNo"`
	Note        string  `json:"note"`

	// 兼容运营端既有脚本/表单的 snake_case 写法。
	SnakeTenantID int64  `json:"tenant_id"`
	SnakeOrderID  int64  `json:"order_id"`
	SnakeType     string `json:"invoice_type"`
}

// normalize 合并别名字段并校验开票基础字段。
// 「订单是否存在、是否可开、累计金额是否超应收」需要读库，在 service 层校验。
func (r CreateInvoiceRequest) normalize() (CreateInvoiceRequest, error) {
	if r.TenantID == 0 {
		r.TenantID = r.SnakeTenantID
	}
	if r.OrderID == 0 {
		r.OrderID = r.SnakeOrderID
	}
	if strings.TrimSpace(r.InvoiceType) == "" {
		r.InvoiceType = r.SnakeType
	}
	r.InvoiceType = strings.TrimSpace(r.InvoiceType)
	r.Title = strings.TrimSpace(r.Title)
	r.TaxNo = strings.TrimSpace(r.TaxNo)
	r.InvoiceNo = strings.TrimSpace(r.InvoiceNo)
	r.Note = strings.TrimSpace(r.Note)

	if r.TenantID <= 0 {
		return CreateInvoiceRequest{}, invalidf("请选择开票机构")
	}
	if r.OrderID < 0 {
		return CreateInvoiceRequest{}, invalidf("orderId must be a positive integer")
	}
	invoiceType, err := normalizeInvoiceType(r.InvoiceType)
	if err != nil {
		return CreateInvoiceRequest{}, err
	}
	r.InvoiceType = invoiceType
	if r.Title == "" {
		return CreateInvoiceRequest{}, invalidf("请填发票抬头")
	}
	if len([]rune(r.Title)) > 200 {
		return CreateInvoiceRequest{}, invalidf("发票抬头过长")
	}
	if len(r.TaxNo) > 64 {
		return CreateInvoiceRequest{}, invalidf("税号过长")
	}
	if len(r.InvoiceNo) > 64 {
		return CreateInvoiceRequest{}, invalidf("发票号过长")
	}
	if len([]rune(r.Note)) > 500 {
		return CreateInvoiceRequest{}, invalidf("备注过长")
	}
	amountCents, err := parseAmountCents(r.Amount)
	if err != nil {
		return CreateInvoiceRequest{}, invalidf("开票金额 %s", err)
	}
	// 关联订单时金额可留空，service 会按订单剩余可开票金额带出；手填金额必须 > 0。
	if amountCents <= 0 && r.OrderID == 0 {
		return CreateInvoiceRequest{}, invalidf("开票金额要大于 0")
	}
	if amountCents > 0 {
		r.Amount = centsToDecimal(amountCents)
	} else {
		r.Amount = ""
	}
	return r, nil
}

// normalizeInvoiceType 归一化发票类型：默认增值税普通发票。
func normalizeInvoiceType(raw string) (string, error) {
	switch normalizeEnum(raw) {
	case "", "增值税普通", "普通", "normal", "vat_normal", "普通发票":
		return InvoiceTypeNormal, nil
	case "增值税专用", "专用", "special", "vat_special", "专用发票":
		return InvoiceTypeSpecial, nil
	default:
		return "", invalidf("发票类型只支持增值税普通/增值税专用")
	}
}

// IssueInvoiceRequest 是 POST /ops/billing/invoices/{id}/issue 的请求体。
//
// 发票号可空：为空时沿用创建时填写的号码；仍为空则按发票 id 生成内部唯一号。
// 开票日期为空时默认服务端今天。
type IssueInvoiceRequest struct {
	InvoiceNo string `json:"invoiceNo"`
	IssuedOn  string `json:"issuedOn"`

	SnakeInvoiceNo string `json:"invoice_no"`
	SnakeIssuedOn  string `json:"issued_on"`
}

// normalize 合并别名并校验开票日期格式。
func (r IssueInvoiceRequest) normalize() (IssueInvoiceRequest, error) {
	if strings.TrimSpace(r.InvoiceNo) == "" {
		r.InvoiceNo = r.SnakeInvoiceNo
	}
	if strings.TrimSpace(r.IssuedOn) == "" {
		r.IssuedOn = r.SnakeIssuedOn
	}
	r.InvoiceNo = strings.TrimSpace(r.InvoiceNo)
	r.IssuedOn = strings.TrimSpace(r.IssuedOn)
	if len(r.InvoiceNo) > 64 {
		return IssueInvoiceRequest{}, invalidf("发票号过长")
	}
	if r.IssuedOn != "" {
		if _, err := parseDateOnly(r.IssuedOn); err != nil {
			return IssueInvoiceRequest{}, invalidf("开票日期必须是 YYYY-MM-DD")
		}
	}
	return r, nil
}

// VoidInvoiceRequest 是 POST /ops/billing/invoices/{id}/void 的请求体。
type VoidInvoiceRequest struct {
	Note string `json:"note"`
}

// InvoiceableOrder 是机构可开票订单（含累计已开票与剩余可开票金额）。
type InvoiceableOrder struct {
	ID              int64   `json:"id"`
	OrderNo         string  `json:"orderNo"`
	OrderType       string  `json:"orderType"`
	PayableAmount   Decimal `json:"payableAmount"`
	InvoicedAmount  Decimal `json:"invoicedAmount"`
	Invoiceable     Decimal `json:"invoiceable"`
	Status          string  `json:"status"`
	OrderCreatedAt  string  `json:"createdAt"`
	InvoiceableText string  `json:"invoiceableText"`
}

// invoiceNumberFor 按发票 id 生成内部发票号（财务补号前的占位号，保证唯一）。
func invoiceNumberFor(id int64, issuedOn string) string {
	date := strings.ReplaceAll(strings.TrimSpace(issuedOn), "-", "")
	if len(date) != 8 {
		date = "00000000"
	}
	return fmt.Sprintf("INV%s%06d", date, id)
}

// invoiceStatusLabel 把状态码转成 CSV / 提示文案。
func invoiceStatusLabel(status string) string {
	switch normalizeEnum(status) {
	case InvoiceStatusApplied:
		return invoiceStatusAppliedLabel
	case InvoiceStatusIssued:
		return "已开票"
	case InvoiceStatusVoided:
		return "已作废"
	default:
		return status
	}
}
