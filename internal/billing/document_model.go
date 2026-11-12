package billing

// 通知函文档（tenant_documents）的数据契约与文案工具。
//
// 文档类型只允许两种：
//   · trial_notice    试用开通通知函（LC-TR-<年>-<4 位流水>）
//   · service_confirm 服务开通确认函（LC-SV-<年>-<4 位流水>）
// 前端接口里的 kind 沿用 mock 的 'trial' / 'formal'，两者在 documentKindOf 里互转。

import (
	"errors"
	"fmt"
	"strconv"
	"strings"
	"time"
)

// 文档类型（落库值，与迁移里的 CHECK 一致）。
const (
	DocTypeTrialNotice    = "trial_notice"
	DocTypeServiceConfirm = "service_confirm"
)

// 文档种类（前端 kind，与 billingMock.ts 的 DocumentRecord.kind 一致）。
const (
	DocumentKindTrial  = "trial"
	DocumentKindFormal = "formal"
)

const (
	documentNoPrefixTrial  = "LC-TR-"
	documentNoPrefixFormal = "LC-SV-"

	// DefaultDocumentCompany 是通知函落款/页脚的公司全称，需要换主体时改这一处。
	DefaultDocumentCompany = "天津灵策科技有限公司"
	// documentFooterNote 是页脚右侧的免责声明：说明只是开通告知，不构成合同、不具法律效力。
	documentFooterNote = "本函由系统自动生成，仅作服务开通告知之用，不构成合同，亦不具法律效力。"
)

var (
	// ErrDocumentNotFound 表示文档记录不存在（404）。
	ErrDocumentNotFound = errors.New("billing: document not found")
	// ErrDocumentNoExists 表示编号撞号（插入唯一索引冲突），服务层换号重试（409 内部用）。
	ErrDocumentNoExists = errors.New("billing: document no already exists")
)

// Document 是 tenant_documents 的一行（Content 只在下载时读取）。
type Document struct {
	ID              int64
	TenantID        int64
	DocType         string
	DocNo           string
	GeneratedAt     time.Time
	GeneratedBy     int64
	GeneratedByName string
	FileSize        int64
	Content         []byte
}

// GenerateDocumentRequest 是 POST /institutions/{id}/documents 的请求体。
type GenerateDocumentRequest struct {
	// Kind 取 trial（试用开通通知函）或 formal（服务开通确认函）。
	Kind string `json:"kind"`
}

// DocumentItem 是生成接口的返回，也是列表里的一行。
type DocumentItem struct {
	ID          int64  `json:"id"`
	DocNo       string `json:"docNo"`
	DocType     string `json:"docType"`
	Kind        string `json:"kind"`
	GeneratedAt string `json:"generatedAt"`
	At          string `json:"at"`
	GeneratedBy string `json:"generatedBy"`
	FileSize    int64  `json:"fileSize"`
}

// DocumentListResult 是 GET /institutions/{id}/documents 的 data。
type DocumentListResult struct {
	Items  []DocumentItem `json:"items"`
	Total  int64          `json:"total"`
	IsMock bool           `json:"isMock"`
}

// documentItemOf 把存储行转换成接口契约。
func documentItemOf(doc Document) DocumentItem {
	return DocumentItem{
		ID:          doc.ID,
		DocNo:       doc.DocNo,
		DocType:     doc.DocType,
		Kind:        documentKindOf(doc.DocType),
		GeneratedAt: doc.GeneratedAt.Format(time.RFC3339),
		At:          doc.GeneratedAt.Format("2006-01-02 15:04"),
		GeneratedBy: doc.GeneratedByName,
		FileSize:    doc.FileSize,
	}
}

// documentKindOf 把落库类型转成前端 kind。
func documentKindOf(docType string) string {
	if docType == DocTypeTrialNotice {
		return DocumentKindTrial
	}
	return DocumentKindFormal
}

// documentTypeOfKind 把请求里的 kind 转成落库类型。
func documentTypeOfKind(kind string) (string, error) {
	switch normalizeEnum(kind) {
	case DocumentKindTrial:
		return DocTypeTrialNotice, nil
	case DocumentKindFormal:
		return DocTypeServiceConfirm, nil
	}
	return "", invalidf("kind 只能是 trial 或 formal")
}

// documentTitleOf 返回文档标题（也是 PDF 大标题）。
func documentTitleOf(docType string) string {
	if docType == DocTypeTrialNotice {
		return "试用开通通知函"
	}
	return "服务开通确认函"
}

// documentFileBaseName 返回下载文件名（不含 .pdf）。
func documentFileBaseName(docType, docNo string) string {
	return documentTitleOf(docType) + "-" + docNo
}

// documentNoPrefix 返回某年某类文档的编号前缀，例如 LC-TR-2026-。
func documentNoPrefix(docType string, year int) string {
	prefix := documentNoPrefixFormal
	if docType == DocTypeTrialNotice {
		prefix = documentNoPrefixTrial
	}
	return fmt.Sprintf("%s%d-", prefix, year)
}

// nextDocumentNo 在同前缀的已有编号里取最大流水 +1，生成下一个编号。
//
// 4 位流水补零（0005）；超过 9999 时自然进位成 5 位，不再补零。
func nextDocumentNo(prefix string, existing []string) string {
	maxSequence := 0
	for _, docNo := range existing {
		docNo = strings.ToUpper(strings.TrimSpace(docNo))
		if !strings.HasPrefix(docNo, strings.ToUpper(prefix)) {
			continue
		}
		suffix := docNo[len(prefix):]
		if len(suffix) < 4 {
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
	return fmt.Sprintf("%s%04d", prefix, maxSequence+1)
}

// formatDocumentDate 把 2026-11-12 转成「2026 年 11 月 12 日」。
func formatDocumentDate(raw string) string {
	value := strings.TrimSpace(raw)
	if value == "" {
		return ""
	}
	parsed, err := time.Parse("2006-01-02", value)
	if err != nil {
		return value
	}
	return fmt.Sprintf("%d 年 %d 月 %d 日", parsed.Year(), int(parsed.Month()), parsed.Day())
}

// formatDocumentNumber 把额度数字格式化成带千分位的文案（最多两位小数）。
func formatDocumentNumber(value Decimal) string {
	cents, err := parseAmountCents(value)
	if err != nil {
		return strings.TrimSpace(string(value))
	}
	whole := cents / 100
	fraction := cents % 100
	grouped := groupThousands(whole)
	if fraction == 0 {
		return grouped
	}
	return fmt.Sprintf("%s.%02d", grouped, fraction)
}

// groupThousands 给整数加千分位。
func groupThousands(value int64) string {
	text := strconv.FormatInt(value, 10)
	if len(text) <= 3 {
		return text
	}
	var builder strings.Builder
	offset := len(text) % 3
	if offset > 0 {
		builder.WriteString(text[:offset])
	}
	for index := offset; index < len(text); index += 3 {
		if builder.Len() > 0 {
			builder.WriteByte(',')
		}
		builder.WriteString(text[index : index+3])
	}
	return builder.String()
}

// formatDocumentMoney 把「分」格式化成「¥ 68,800」。
func formatDocumentMoney(cents int64) string {
	if cents < 0 {
		return ""
	}
	whole := cents / 100
	fraction := cents % 100
	money := "¥ " + groupThousands(whole)
	if fraction != 0 {
		money = fmt.Sprintf("%s.%02d", money, fraction)
	}
	return money
}

// formatDocumentPhone 把手机号按 3-4-4 分段；非 11 位手机号原样返回。
func formatDocumentPhone(phone, separator string) string {
	trimmed := strings.TrimSpace(phone)
	digits := make([]rune, 0, len(trimmed))
	for _, char := range trimmed {
		if char >= '0' && char <= '9' {
			digits = append(digits, char)
		}
	}
	if len(digits) == 11 && digits[0] == '1' {
		return string(digits[0:3]) + separator + string(digits[3:7]) + separator + string(digits[7:11])
	}
	return trimmed
}

// documentPeriodDays 计算服务期天数：与设计稿一致，取「到期日 - 开始日」，最小 1 天。
func documentPeriodDays(start, end string) int {
	startDate, err := time.Parse("2006-01-02", strings.TrimSpace(start))
	if err != nil {
		return 0
	}
	endDate, err := time.Parse("2006-01-02", strings.TrimSpace(end))
	if err != nil {
		return 0
	}
	days := int(endDate.Sub(startDate).Hours() / 24)
	if days < 1 {
		days = 1
	}
	return days
}

// documentFeeText 生成费用文案。
//
// 正式版只在「应收全部收讫」时显示金额；未收讫/部分收讫一律显示「以双方合同约定为准」，
// 绝不把未收款金额写进给客户的通知函。
func documentFeeText(institution *Institution, trial bool) (string, bool) {
	if trial {
		return "¥ 0（试用期内不产生任何费用）", true
	}
	totalCents := int64(0)
	paidCents := int64(0)
	for _, receivable := range institution.Receivables {
		if value, err := parseAmountCents(receivable.Amount); err == nil {
			totalCents += value
		}
		if value, err := parseAmountCents(receivable.Paid); err == nil {
			paidCents += value
		}
	}
	if totalCents > 0 && paidCents >= totalCents {
		return formatDocumentMoney(totalCents) + "（已收讫）", true
	}
	return "以双方合同约定为准", false
}

// documentAdminPhone 取登录用的管理员手机号：优先主管理员，其次机构联系人。
func documentAdminPhone(institution *Institution) string {
	for _, admin := range institution.Admins {
		if phone := strings.TrimSpace(admin.Phone); phone != "" {
			return phone
		}
	}
	return strings.TrimSpace(institution.Phone)
}

// documentCustomerName 取抬头里的客户全称：优先开票主体（legal），没有就用简称。
func documentCustomerName(institution *Institution) string {
	if legal := strings.TrimSpace(institution.Legal); legal != "" {
		return legal
	}
	return strings.TrimSpace(institution.Name)
}
