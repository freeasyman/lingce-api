package billing

// 从机构档案数据组装通知函内容（纯函数，不查库）。
//
// 文案与 06-原型/运营端计费Demo.html 的 openLetter() 保持一致，额度单位按产品说人话：
// 小时接诊录音 / 小时沟通录音 / 小时接待录音 / 份病历 / 轮质控 /
// 张截图 / 次对话 / 人次 / 道题 / 份脚本 / 张图文。
//
// 小时类单位必须写清「这些小时是什么录音」：客户看不懂「小时随访生成」，
// 但看得懂「400 小时接诊录音」。单位仍是 hour，只改展示文案。

import (
	"sort"
	"strconv"
	"strings"
	"time"

	billingpdf "github.com/freeasyman/lingce-api/internal/billing/pdf"
)

// documentProductOrder 是内容表里的产品顺序（与设计稿一致）。
var documentProductOrder = []string{"yingke", "kehu", "suifang", "tingxie", "hegui", "huoke"}

// documentUnitOrder 是每个产品下额度单位的展示顺序（与价目表/设计稿一致）。
var documentUnitOrder = map[string][]string{
	"yingke":  {"hour"},
	"kehu":    {"hour", "shot", "record", "person"},
	"suifang": {"hour"},
	"tingxie": {"record", "round", "hour"},
	"hegui":   {"hour"},
	"huoke":   {"hour", "prompt", "record", "shot"},
}

// documentUnitLabels 是「产品 + 单位」对应的中文单位文案。
//
// 这是后端通知函的唯一单位文案表；前端对应表在
// packages/product-ui/src/quota-product-units.ts，两边的 hour 文案由
// document_unit_copy_test.go / quota-product-units.test.ts 锁定为同一组值。
var documentUnitLabels = map[string]map[string]string{
	"yingke":  {"hour": "小时接待录音"},
	"kehu":    {"hour": "小时接诊录音", "shot": "张截图", "record": "次对话", "person": "人次"},
	"suifang": {"hour": "小时接诊录音"},
	"tingxie": {"record": "份病历", "round": "轮质控", "hour": "小时接诊录音"},
	"hegui":   {"hour": "小时沟通录音"},
	"huoke":   {"hour": "小时沟通录音", "prompt": "道题", "record": "份脚本", "shot": "张图文"},
}

// documentUnitFallback 是产品不在上表时的兜底单位文案。
var documentUnitFallback = map[string]string{
	"hour": "小时", "record": "条", "round": "轮", "shot": "张", "prompt": "题", "person": "人",
}

// documentProductFallbackNames 是 product_catalog 查不到时的产品名兜底。
var documentProductFallbackNames = map[string]string{
	"yingke": "赢客专家", "kehu": "客户总监", "suifang": "随访顾问",
	"tingxie": "听写病历", "hegui": "合规卫士", "huoke": "获客先锋",
}

// documentLetterInput 是组装通知函所需的全部输入。
type documentLetterInput struct {
	Institution  *Institution
	DocType      string
	DocNo        string
	Date         time.Time
	ProductNames map[string]string
	SalesName    string
	SalesPhone   string
	Company      string
}

// documentProductCodes 返回机构已开通的产品编码（排序稳定）。
func documentProductCodes(products map[string]map[string]InstitutionQuota) []string {
	seen := make(map[string]struct{}, len(products))
	codes := make([]string, 0, len(products))
	for code := range products {
		normalized := normalizeEnum(code)
		if normalized == "" {
			continue
		}
		if _, ok := seen[normalized]; ok {
			continue
		}
		seen[normalized] = struct{}{}
		codes = append(codes, normalized)
	}
	sort.Strings(codes)
	return codes
}

// orderedProductCodes 按设计稿顺序排列产品编码，未知产品排在最后。
func orderedProductCodes(codes []string) []string {
	known := make(map[string]struct{}, len(documentProductOrder))
	for _, code := range documentProductOrder {
		known[code] = struct{}{}
	}
	ordered := make([]string, 0, len(codes))
	for _, code := range documentProductOrder {
		for _, candidate := range codes {
			if candidate == code {
				ordered = append(ordered, code)
				break
			}
		}
	}
	extra := make([]string, 0, len(codes))
	for _, code := range codes {
		if _, ok := known[code]; !ok {
			extra = append(extra, code)
		}
	}
	sort.Strings(extra)
	return append(ordered, extra...)
}

// documentUnitLabel 返回某产品某单位的中文文案。
func documentUnitLabel(productCode, unit string) string {
	if labels, ok := documentUnitLabels[productCode]; ok {
		if label, ok := labels[unit]; ok {
			return label
		}
	}
	if label, ok := documentUnitFallback[unit]; ok {
		return label
	}
	return unit
}

// orderedUnits 返回某产品下要展示的额度单位：先按既定顺序，再补未列出的单位。
func orderedUnits(productCode string, quota map[string]InstitutionQuota) []string {
	units := make([]string, 0, len(quota))
	seen := make(map[string]struct{}, len(quota))
	for _, unit := range documentUnitOrder[productCode] {
		if _, ok := quota[unit]; ok {
			units = append(units, unit)
			seen[unit] = struct{}{}
		}
	}
	extra := make([]string, 0, len(quota))
	for unit := range quota {
		if _, ok := seen[unit]; !ok {
			extra = append(extra, unit)
		}
	}
	sort.Strings(extra)
	return append(units, extra...)
}

// documentProductRows 把机构额度组装成「产品 + 额度」表行。
func documentProductRows(institution *Institution, productNames map[string]string) []billingpdf.ProductRow {
	codes := orderedProductCodes(documentProductCodes(institution.Products))
	rows := make([]billingpdf.ProductRow, 0, len(codes))
	for _, code := range codes {
		quota := institution.Products[code]
		parts := make([]string, 0, 4)
		for _, unit := range orderedUnits(code, quota) {
			entry := quota[unit]
			cents, err := parseAmountCents(entry.Granted)
			if err != nil || cents <= 0 {
				continue
			}
			parts = append(parts, formatDocumentNumber(entry.Granted)+" "+documentUnitLabel(code, unit))
		}
		if len(parts) == 0 {
			continue
		}
		name := strings.TrimSpace(productNames[code])
		if name == "" {
			name = documentProductFallbackNames[code]
		}
		if name == "" {
			name = code
		}
		rows = append(rows, billingpdf.ProductRow{Name: name, Quota: strings.Join(parts, "　/　")})
	}
	return rows
}

// buildDocumentLetter 把机构数据组装成 PDF 渲染所需的纯数据 DTO。
func buildDocumentLetter(input documentLetterInput) (billingpdf.Letter, error) {
	institution := input.Institution
	if institution == nil {
		return billingpdf.Letter{}, invalidf("机构不存在")
	}
	trial := input.DocType == DocTypeTrialNotice

	periodStart := strings.TrimSpace(institution.Start)
	periodEnd := strings.TrimSpace(institution.End)
	if periodStart == "" || periodEnd == "" {
		return billingpdf.Letter{}, invalidf("机构服务期不完整，暂时无法生成通知函")
	}

	company := strings.TrimSpace(input.Company)
	if company == "" {
		company = DefaultDocumentCompany
	}

	rows := documentProductRows(institution, input.ProductNames)
	if len(rows) == 0 {
		return billingpdf.Letter{}, invalidf("机构还没有可展示的产品额度，无法生成通知函")
	}

	periodLabel := "服务期限"
	feeLabel := "服务费用"
	infoSection := "服务信息"
	productSection := "服务内容"
	quotaHeader := "服务额度"
	if trial {
		periodLabel = "试用期限"
		feeLabel = "试用费用"
		infoSection = "试用信息"
		productSection = "试用内容"
		quotaHeader = "试用额度"
	}

	feeText, feeBold := documentFeeText(institution, trial)

	// 抬头：客户全称 + 联系人 + 职务，用全角空格分隔（职务为空时不额外留空格，也不加括号）。
	salutation := "尊敬的　" + documentCustomerName(institution) + "　"
	if contact := strings.TrimSpace(institution.Contact); contact != "" {
		salutation += contact
		if title := strings.TrimSpace(institution.Title); title != "" {
			salutation += "　" + title
		}
	}
	salutation += "："

	intro := "您好！感谢贵机构对灵策的信任与支持。我们已为贵机构开通以下正式服务，现将相关信息正式告知如下，敬请查阅。"
	if trial {
		intro = "您好！感谢贵机构对灵策的信任与支持。我们已为您开通以下产品的试用服务，试用期间不收取任何费用。现将相关信息正式告知如下，敬请查阅。"
	}

	tips := []string{
		"服务额度使用完后，可随时联系您的专属顾问增购增量包。",
		"服务到期前我们会提前提醒您续期，服务期内产生的数据均可随时查看。",
	}
	if trial {
		tips = []string{
			"试用期至 " + formatDocumentDate(periodEnd) + "，试用额度有限，请合理安排使用。",
			"试用期内贵机构产生的数据在试用结束后仍会保留；试用到期后服务自动停止，如需继续使用，请联系您的专属顾问。",
		}
	}

	startItems := []string{"打开浏览器访问　**https://portal.khgl.xyz**"}
	if adminPhone := documentAdminPhone(institution); adminPhone != "" {
		startItems = append(startItems, "使用管理员手机号 **"+formatDocumentPhone(adminPhone, " ")+
			"** 登录；初始密码由您的专属顾问单独告知，首次登录后请及时修改")
	} else {
		startItems = append(startItems, "使用管理员账号登录；初始密码由您的专属顾问单独告知，首次登录后请及时修改")
	}

	advisorName := strings.TrimSpace(input.SalesName)
	if advisorName == "" {
		advisorName = strings.TrimSpace(institution.Sales)
	}
	isBrandAdvisor := false
	if advisorName == "" {
		// 没有具体员工顾问：名字用「灵策顾问」，头像改用灵策品牌标记。
		advisorName = "灵策顾问"
		isBrandAdvisor = true
	}

	dateText := input.Date.Format("2006-01-02")
	periodText := formatDocumentDate(periodStart) + "　至　" + formatDocumentDate(periodEnd)
	if days := documentPeriodDays(periodStart, periodEnd); days > 0 {
		periodText += "（共 " + strconv.Itoa(days) + " 天）"
	}

	return billingpdf.Letter{
		DocNo:          input.DocNo,
		DocDate:        dateText,
		Title:          documentTitleOf(input.DocType),
		Salutation:     salutation,
		Intro:          intro,
		InfoSection:    infoSection,
		InfoRows:       []billingpdf.InfoRow{{Label: periodLabel, Value: periodText}, {Label: feeLabel, Value: feeText, ValueBold: feeBold}},
		ProductSection: productSection,
		QuotaHeader:    quotaHeader,
		Products:       rows,
		StartSection:   "开始使用",
		StartItems:     startItems,
		TipSection:     "温馨提示",
		TipItems:       tips,
		Advisor:        billingpdf.Advisor{Name: advisorName, Phone: formatDocumentPhone(input.SalesPhone, "-"), IsBrand: isBrandAdvisor},
		AdvisorSection: "您的专属顾问",
		Company:        company,
		SignDate:       formatDocumentDate(dateText),
		FooterLeft:     company,
		FooterRight:    documentFooterNote,
	}, nil
}

// institutionDocumentOf 把存储行映射成机构档案里的文档记录。
func institutionDocumentOf(doc Document) InstitutionDocument {
	return InstitutionDocument{
		ID:          doc.ID,
		No:          doc.DocNo,
		Kind:        documentKindOf(doc.DocType),
		DocType:     doc.DocType,
		At:          doc.GeneratedAt.Format("2006-01-02 15:04"),
		GeneratedAt: doc.GeneratedAt.Format(time.RFC3339),
		By:          doc.GeneratedByName,
		FileSize:    doc.FileSize,
	}
}
