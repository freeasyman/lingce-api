package billing

// 通知函内容与编号规则的单元测试（不依赖数据库）。

import (
	"strings"
	"testing"
	"time"
)

func TestNextDocumentNo(t *testing.T) {
	prefix := documentNoPrefix(DocTypeTrialNotice, 2026)
	if prefix != "LC-TR-2026-" {
		t.Fatalf("试用编号前缀 = %q", prefix)
	}
	cases := []struct {
		name     string
		existing []string
		want     string
	}{
		{"空表从 0001 开始", nil, "LC-TR-2026-0001"},
		{"取最大流水 +1", []string{"LC-TR-2026-0002", "LC-TR-2026-0007"}, "LC-TR-2026-0008"},
		{"忽略其它类型/年份", []string{"LC-SV-2026-0009", "LC-TR-2025-0042"}, "LC-TR-2026-0001"},
		{"忽略非法后缀", []string{"LC-TR-2026-ABC", "LC-TR-2026-0003"}, "LC-TR-2026-0004"},
	}
	for _, item := range cases {
		t.Run(item.name, func(t *testing.T) {
			if got := nextDocumentNo(prefix, item.existing); got != item.want {
				t.Fatalf("nextDocumentNo = %q, want %q", got, item.want)
			}
		})
	}
	if got := documentNoPrefix(DocTypeServiceConfirm, 2026); got != "LC-SV-2026-" {
		t.Fatalf("正式编号前缀 = %q", got)
	}
	if got := documentKindOf(DocTypeTrialNotice); got != "trial" {
		t.Fatalf("trial_notice 的 kind = %q", got)
	}
	if got := documentKindOf(DocTypeServiceConfirm); got != "formal" {
		t.Fatalf("service_confirm 的 kind = %q", got)
	}
}

func TestDocumentFormatters(t *testing.T) {
	if got := formatDocumentDate("2026-11-12"); got != "2026 年 11 月 12 日" {
		t.Fatalf("日期格式化 = %q", got)
	}
	if got := formatDocumentDate(""); got != "" {
		t.Fatalf("空日期 = %q", got)
	}
	numbers := map[string]string{
		"0": "0", "48": "48", "2500": "2,500", "68800": "68,800", "1234567": "1,234,567", "95.89": "95.89",
	}
	for raw, want := range numbers {
		if got := formatDocumentNumber(Decimal(raw)); got != want {
			t.Fatalf("formatDocumentNumber(%s) = %q, want %q", raw, got, want)
		}
	}
	if got := formatDocumentMoney(6880000); got != "¥ 68,800" {
		t.Fatalf("formatDocumentMoney = %q", got)
	}
	if got := formatDocumentPhone("13900002002", " "); got != "139 0000 2002" {
		t.Fatalf("手机号格式化 = %q", got)
	}
	if got := formatDocumentPhone("010-88886666", "-"); got != "010-88886666" {
		t.Fatalf("非手机号应原样返回，实际 %q", got)
	}
	if got := documentPeriodDays("2026-11-12", "2026-11-26"); got != 14 {
		t.Fatalf("试用天数 = %d, want 14", got)
	}
	if got := documentPeriodDays("", "2026-11-26"); got != 0 {
		t.Fatalf("缺开始日应返回 0，实际 %d", got)
	}
}

// trialInstitution 是组装通知函用的机构档案样例。
func trialInstitution() *Institution {
	return &Institution{
		ID:      5,
		Name:    "李院长眼科",
		Legal:   "李院长眼科门诊部有限公司",
		Contact: "李强",
		Title:   "院长",
		Phone:   "13900002002",
		Sales:   "胡晓敏",
		Start:   "2026-11-12",
		End:     "2026-11-26",
		Mode:    AccountModeTrial,
		Open:    true,
		Products: map[string]map[string]InstitutionQuota{
			"tingxie": {
				"record": {Granted: "48", Used: "0"},
				"round":  {Granted: "96", Used: "0"},
				"hour":   {Granted: "8", Used: "0"},
			},
			"hegui":   {"hour": {Granted: "8", Used: "0"}},
			"suifang": {"hour": {Granted: "8", Used: "0"}},
		},
		Admins: []InstitutionAdmin{{ID: 1, Name: "李强", Phone: "13900002002", Role: "主管理员"}},
	}
}

func TestBuildDocumentLetterTrial(t *testing.T) {
	letter, err := buildDocumentLetter(documentLetterInput{
		Institution: trialInstitution(),
		DocType:     DocTypeTrialNotice,
		DocNo:       "LC-TR-2026-0005",
		Date:        time.Date(2026, 11, 12, 10, 0, 0, 0, time.Local),
		SalesPhone:  "13511110002",
	})
	if err != nil {
		t.Fatalf("组装试用通知函失败: %v", err)
	}
	if letter.Title != "试用开通通知函" {
		t.Fatalf("标题 = %q", letter.Title)
	}
	if letter.InfoSection != "试用信息" || letter.ProductSection != "试用内容" || letter.QuotaHeader != "试用额度" {
		t.Fatalf("试用小标题不对: %+v", letter)
	}
	if !strings.Contains(letter.Salutation, "李院长眼科门诊部有限公司") || !strings.Contains(letter.Salutation, "李强（院长）") {
		t.Fatalf("抬头 = %q", letter.Salutation)
	}
	if !strings.Contains(letter.Intro, "试用期间不收取任何费用") {
		t.Fatalf("试用引言应强调不收费: %q", letter.Intro)
	}
	if len(letter.InfoRows) != 2 || !strings.Contains(letter.InfoRows[1].Value, "¥ 0") || !letter.InfoRows[1].ValueBold {
		t.Fatalf("试用费用行不对: %+v", letter.InfoRows)
	}
	if !strings.Contains(letter.InfoRows[0].Value, "共 14 天") || !strings.Contains(letter.InfoRows[0].Value, "2026 年 11 月 26 日") {
		t.Fatalf("试用期限行不对: %+v", letter.InfoRows[0])
	}
	if len(letter.Products) != 3 {
		t.Fatalf("产品行数 = %d, want 3: %+v", len(letter.Products), letter.Products)
	}
	// 产品按价目表顺序（赢客/客户/随访/听写/合规/获客），单位按 record/round/hour 说人话。
	if letter.Products[0].Name != "随访顾问" || letter.Products[2].Name != "合规卫士" {
		t.Fatalf("产品顺序不对: %+v", letter.Products)
	}
	if got := letter.Products[1].Quota; got != "48 份病历　/　96 轮质控　/　8 小时录音" {
		t.Fatalf("听写病历额度 = %q", got)
	}
	if !strings.Contains(strings.Join(letter.StartItems, "|"), "139 0000 2002") {
		t.Fatalf("开始使用应带管理员手机号: %+v", letter.StartItems)
	}
	if letter.Advisor.Name != "胡晓敏" || letter.Advisor.Phone != "135-1111-0002" {
		t.Fatalf("顾问卡 = %+v", letter.Advisor)
	}
	if letter.FooterRight != "本通知函由系统自动生成" {
		t.Fatalf("页脚 = %q", letter.FooterRight)
	}
	if strings.Contains(letter.FooterLeft, "渠道") || strings.Contains(letter.Advisor.Name, "渠道") {
		t.Fatalf("顾问卡不应出现渠道/组织")
	}
}

func TestBuildDocumentLetterFormalFeeText(t *testing.T) {
	institution := trialInstitution()
	institution.Mode = AccountModeFormal
	institution.Start = "2026-03-15"
	institution.End = "2027-03-14"

	// 全部收讫：显示「已收讫」金额。
	institution.Receivables = []InstitutionReceivable{{No: "BL1", Amount: "68800", Paid: "68800"}}
	letter, err := buildDocumentLetter(documentLetterInput{
		Institution: institution,
		DocType:     DocTypeServiceConfirm,
		DocNo:       "LC-SV-2026-0005",
		Date:        time.Date(2026, 11, 12, 10, 0, 0, 0, time.Local),
	})
	if err != nil {
		t.Fatalf("组装正式确认函失败: %v", err)
	}
	if letter.Title != "服务开通确认函" || letter.InfoSection != "服务信息" || letter.QuotaHeader != "服务额度" {
		t.Fatalf("正式函小标题不对: %+v", letter)
	}
	if got := letter.InfoRows[1].Value; got != "¥ 68,800（已收讫）" {
		t.Fatalf("已收讫费用 = %q", got)
	}
	if !strings.Contains(letter.InfoRows[0].Value, "共 364 天") {
		t.Fatalf("服务期文案 = %q", letter.InfoRows[0].Value)
	}

	// 部分收讫：绝不能把未收款金额写进给客户的函。
	institution.Receivables = []InstitutionReceivable{{No: "BL1", Amount: "68800", Paid: "20000"}}
	letter, err = buildDocumentLetter(documentLetterInput{
		Institution: institution,
		DocType:     DocTypeServiceConfirm,
		DocNo:       "LC-SV-2026-0006",
		Date:        time.Date(2026, 11, 12, 10, 0, 0, 0, time.Local),
	})
	if err != nil {
		t.Fatalf("组装正式确认函失败: %v", err)
	}
	if got := letter.InfoRows[1].Value; got != "以双方合同约定为准" {
		t.Fatalf("部分收讫费用文案 = %q, want 以双方合同约定为准", got)
	}
	joined := letter.InfoRows[0].Value + letter.InfoRows[1].Value + strings.Join(letter.StartItems, "") + strings.Join(letter.TipItems, "")
	if strings.Contains(joined, "68,800") || strings.Contains(joined, "20,000") || strings.Contains(joined, "未收") {
		t.Fatalf("正式函泄露了金额/未收款信息: %q", joined)
	}
}

func TestBuildDocumentLetterRejectsIncompletePeriod(t *testing.T) {
	institution := trialInstitution()
	institution.Start = ""
	if _, err := buildDocumentLetter(documentLetterInput{Institution: institution, DocType: DocTypeTrialNotice, Date: time.Now()}); err == nil {
		t.Fatal("缺服务期应当报错")
	}
}

func TestContentDispositionAttachment(t *testing.T) {
	header := contentDispositionAttachment("试用开通通知函-LC-TR-2026-0005.pdf", "LC-TR-2026-0005.pdf")
	want := "attachment; filename=\"LC-TR-2026-0005.pdf\"; filename*=UTF-8''%E8%AF%95%E7%94%A8%E5%BC%80%E9%80%9A%E9%80%9A%E7%9F%A5%E5%87%BD-LC-TR-2026-0005.pdf"
	if header != want {
		t.Fatalf("Content-Disposition = %q", header)
	}
}
