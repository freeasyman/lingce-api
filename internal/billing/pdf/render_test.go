package pdf

// 渲染结果的硬性校验：合法 PDF、A4 单页、体积达到「字体确实嵌入」的量级。

import (
	"bytes"
	"regexp"
	"strings"
	"testing"

	"github.com/jung-kurt/gofpdf"
)

// pageObjectPattern 只匹配页面对象 /Type /Page，不匹配页树 /Type /Pages。
var pageObjectPattern = regexp.MustCompile(`/Type\s*/Page\b`)

// trialLetter 是试用开通通知函的完整样例（与 billing 层实际组装的 DTO 同构）。
func trialLetter() Letter {
	return Letter{
		DocNo:       "LC-TR-2026-0005",
		DocDate:     "2026-11-12",
		Title:       "试用开通通知函",
		Salutation:  "尊敬的　李院长眼科　李强（院长）：",
		Intro:       "您好！感谢贵机构对灵策的信任与支持。我们已为您开通以下产品的试用服务，试用期间不收取任何费用。现将相关信息正式告知如下，敬请查阅。",
		InfoSection: "试用信息",
		InfoRows: []InfoRow{
			{Label: "试用期限", Value: "2026 年 11 月 12 日　至　2026 年 11 月 26 日（共 14 天）"},
			{Label: "试用费用", Value: "¥ 0（试用期内不产生任何费用）", ValueBold: true},
		},
		ProductSection: "试用内容",
		QuotaHeader:    "试用额度",
		Products: []ProductRow{
			{Name: "听写病历", Quota: "48 份病历　/　96 轮质控　/　8 小时接诊录音"},
			{Name: "合规卫士", Quota: "8 小时沟通录音"},
			{Name: "随访顾问", Quota: "8 小时接诊录音"},
		},
		StartSection: "开始使用",
		StartItems: []string{
			"打开浏览器访问　**https://portal.khgl.xyz**",
			"使用管理员手机号 **139 0000 2002** 登录；初始密码由您的专属顾问单独告知，首次登录后请及时修改",
		},
		TipSection: "温馨提示",
		TipItems: []string{
			"试用额度按试用天数折算，如不够使用，可随时联系顾问为您追加。",
			"试用期内产生的病历、录音等数据在试用结束后仍会保留；如需转为正式服务，请联系您的专属顾问。",
		},
		Advisor:     Advisor{Name: "胡晓敏", Phone: "135-1111-0002"},
		Company:     "天津灵策科技有限公司",
		SignDate:    "2026 年 11 月 12 日",
		FooterLeft:  "天津灵策科技有限公司",
		FooterRight: "本函由系统自动生成，仅作服务开通告知之用，不构成合同，亦不具法律效力。",
	}
}

// formalLetter 是服务开通确认函的完整样例（6 个产品，表格最长）。
func formalLetter() Letter {
	return Letter{
		DocNo:       "LC-SV-2026-0005",
		DocDate:     "2026-11-12",
		Title:       "服务开通确认函",
		Salutation:  "尊敬的　杭州王院长中医门诊部　王丽（医务科）：",
		Intro:       "您好！感谢贵机构对灵策的信任与支持。我们已为贵机构开通以下正式服务，现将相关信息正式告知如下，敬请查阅。",
		InfoSection: "服务信息",
		InfoRows: []InfoRow{
			{Label: "服务期限", Value: "2026 年 3 月 15 日　至　2027 年 3 月 14 日（共 364 天）"},
			{Label: "服务费用", Value: "¥ 68,800（已收讫）", ValueBold: true},
		},
		ProductSection: "服务内容",
		QuotaHeader:    "服务额度",
		Products: []ProductRow{
			{Name: "赢客专家", Quota: "400 小时接待录音"},
			{Name: "客户总监", Quota: "400 小时接诊录音　/　5,000 张截图　/　10,000 次对话　/　10,000 人次"},
			{Name: "随访顾问", Quota: "400 小时接诊录音"},
			{Name: "听写病历", Quota: "2,500 份病历　/　5,000 轮质控　/　400 小时接诊录音"},
			{Name: "合规卫士", Quota: "400 小时沟通录音"},
			{Name: "获客先锋", Quota: "400 小时沟通录音　/　240 道题　/　600 份脚本　/　1,200 张图文"},
		},
		StartSection: "开始使用",
		StartItems: []string{
			"打开浏览器访问　**https://portal.khgl.xyz**",
			"使用管理员手机号 **137 0000 3003** 登录；初始密码由您的专属顾问单独告知，首次登录后请及时修改",
		},
		TipSection: "温馨提示",
		TipItems: []string{
			"服务额度使用完后，可随时联系您的专属顾问增购增量包。",
			"服务到期前我们会提前提醒您续期，服务期内产生的数据均可随时查看。",
		},
		Advisor:     Advisor{Name: "朱媛媛", Phone: "135-1111-0001"},
		Company:     "天津灵策科技有限公司",
		SignDate:    "2026 年 11 月 12 日",
		FooterLeft:  "天津灵策科技有限公司",
		FooterRight: "本函由系统自动生成，仅作服务开通告知之用，不构成合同，亦不具法律效力。",
	}
}

// assertSinglePagePDF 校验 PDF 头部、页数与体积。
func assertSinglePagePDF(t *testing.T, data []byte) {
	t.Helper()
	if !bytes.HasPrefix(data, []byte("%PDF-")) {
		t.Fatalf("输出不是以 %%PDF- 开头: %q", data[:min(8, len(data))])
	}
	if pages := len(pageObjectPattern.FindAll(data, -1)); pages != 1 {
		t.Fatalf("页数 = %d, want 1（内容溢出了单页）", pages)
	}
	if len(data) <= 20*1024 {
		t.Fatalf("PDF 体积 = %d 字节, want > 20KB（字体疑似没有内嵌）", len(data))
	}
}

// newTestCanvas 构造一个只用于量宽度的 canvas（字体来自内嵌文件）。
func newTestCanvas(t *testing.T) *canvas {
	t.Helper()
	doc := gofpdf.New("P", "mm", "A4", "")
	if err := addFonts(doc); err != nil {
		t.Fatalf("加载内嵌字体失败: %v", err)
	}
	return newCanvas(doc, 1.0, true)
}

func TestRenderTrialNoticeIsSinglePage(t *testing.T) {
	data, err := Render(trialLetter())
	if err != nil {
		t.Fatalf("Render 失败: %v", err)
	}
	assertSinglePagePDF(t, data)
}

func TestRenderFormalConfirmIsSinglePage(t *testing.T) {
	data, err := Render(formalLetter())
	if err != nil {
		t.Fatalf("Render 失败: %v", err)
	}
	assertSinglePagePDF(t, data)
}

func TestRenderRejectsIncompleteLetter(t *testing.T) {
	if _, err := Render(Letter{Title: "试用开通通知函"}); err == nil {
		t.Fatal("缺少正文的 DTO 应当报错")
	}
}

func TestRenderShrinksInsteadOfOverflowing(t *testing.T) {
	letter := trialLetter()
	letter.Salutation = "尊敬的　" + "超长机构名称测试" + "　欧阳娜娜（院长）："
	letter.Products = append(letter.Products,
		ProductRow{Name: "客户总监", Quota: "400 小时接诊录音　/　5,000 张截图　/　10,000 次对话　/　10,000 人次"},
		ProductRow{Name: "获客先锋", Quota: "400 小时沟通录音　/　240 道题　/　600 份脚本　/　1,200 张图文"},
	)
	letter.TipItems = append(letter.TipItems,
		"服务到期前我们会提前提醒您续期，服务期内产生的数据均可随时查看。",
		"如需增购增量包或调整额度，请提前联系您的专属顾问，我们会按合同约定为您安排。",
	)
	data, err := Render(letter)
	if err != nil {
		t.Fatalf("Render 失败: %v", err)
	}
	assertSinglePagePDF(t, data)
}

// TestWrapSpansBreaksCJK 校验中文按字断行、ASCII 片段不被拆碎、行宽不超限。
func TestWrapSpansBreaksCJK(t *testing.T) {
	doc := newTestCanvas(t)
	lines := doc.wrapText("打开浏览器访问　https://portal.khgl.xyz 并使用管理员手机号 139 0000 2002 登录；初始密码由您的专属顾问单独告知，首次登录后请及时修改", 9.6, 100)
	if len(lines) < 2 {
		t.Fatalf("长句应当断成多行，实际 %d 行", len(lines))
	}
	for index, line := range lines {
		if line.width > 100.5 {
			t.Fatalf("第 %d 行宽度 %.1fmm 超出 100mm", index+1, line.width)
		}
	}
	joined := ""
	for _, line := range lines {
		for _, unit := range line.units {
			joined += unit.text
		}
	}
	if !strings.Contains(joined, "https://portal.khgl.xyz") {
		t.Fatalf("URL 不应被拆碎: %q", joined)
	}
}
