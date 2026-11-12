package billing

// 运营端计费第 1 阶段的纯逻辑单测：机构代码、额度折算、订单金额分摊、入参校验。
// 不依赖数据库，随 `go test ./internal/billing/...` 一起跑。

import (
	"encoding/json"
	"strings"
	"testing"
	"time"
)

// timeNowForTest 固定纯函数单测使用的「今天」。
func timeNowForTest() time.Time {
	return time.Date(2026, 11, 12, 0, 0, 0, 0, time.UTC)
}

func TestInstitutionPinyinCode(t *testing.T) {
	cases := map[string]string{
		"上海张医生口腔门诊部":  "SHZY",
		"杭州王院长中医门诊部":  "HZWY",
		"李院长眼科":       "LYCY",
		"hope clinic": "HOPE",
		"12345678":    "1234",
		"":            "GENR",
		"测试":          "CS",
	}
	for name, want := range cases {
		if got := institutionPinyinCode(name); got != want {
			t.Fatalf("institutionPinyinCode(%q) = %q, want %q", name, got, want)
		}
	}
}

func TestNextInstitutionCode(t *testing.T) {
	existing := []string{"LC-SHZY-0001", "lc-shzy-0007", "LC-OTHER-0009"}
	if got := nextInstitutionCode("上海张医生口腔门诊部", existing); got != "LC-SHZY-0008" {
		t.Fatalf("nextInstitutionCode = %q, want LC-SHZY-0008", got)
	}
	if got := nextInstitutionCode("仁和口腔", nil); got != "LC-RHKQ-0001" {
		t.Fatalf("nextInstitutionCode(empty) = %q, want LC-RHKQ-0001", got)
	}
}

func TestValidateInstitutionCode(t *testing.T) {
	if err := validateInstitutionCode("LC-SHZY-0001"); err != nil {
		t.Fatalf("合法代码应通过: %v", err)
	}
	if err := validateInstitutionCode("LC SHZY"); err == nil {
		t.Fatalf("含空格代码应被拒绝")
	}
	if err := validateInstitutionCode(strings.Repeat("A", 41)); err == nil {
		t.Fatalf("超长代码应被拒绝")
	}
}

func TestScaleQuotaAmount(t *testing.T) {
	cases := []struct {
		base Decimal
		num  int64
		want string
	}{
		{"400", 14, "15.34"},
		{"2500", 14, "95.89"},
		{"80", 7, "1.53"},
		{"1", 1, "1"}, // 每单位最少 1
	}
	for _, item := range cases {
		got, err := scaleQuotaAmount(item.base, item.num, 365)
		if err != nil {
			t.Fatalf("scaleQuotaAmount(%s, %d) error: %v", item.base, item.num, err)
		}
		if string(got) != item.want {
			t.Fatalf("scaleQuotaAmount(%s, %d) = %s, want %s", item.base, item.num, got, item.want)
		}
	}
	if _, err := scaleQuotaAmount("100", 1, 0); err == nil {
		t.Fatalf("分母为 0 应报错")
	}
}

func TestMultiplyQuotaAmount(t *testing.T) {
	got, err := multiplyQuotaAmount("80", 3)
	if err != nil {
		t.Fatalf("multiplyQuotaAmount error: %v", err)
	}
	if string(got) != "240" {
		t.Fatalf("multiplyQuotaAmount = %s, want 240", got)
	}
}

func TestDistributeItemPrices(t *testing.T) {
	items := []OrderItem{
		{ProductCode: "yingke", Price: "12600"},
		{ProductCode: "kehu", Price: "10800"},
	}
	if err := distributeItemPrices(items, "17800"); err != nil {
		t.Fatalf("distributeItemPrices error: %v", err)
	}
	total, err := parseAmountCents("17800")
	if err != nil {
		t.Fatalf("parse total: %v", err)
	}
	sum := int64(0)
	for _, item := range items {
		cents, err := parseAmountCents(item.Price)
		if err != nil {
			t.Fatalf("parse item price: %v", err)
		}
		sum += cents
	}
	if sum != total {
		t.Fatalf("分摊后金额合计 %d != 总额 %d", sum, total)
	}
	if items[0].Price == "12600" && items[1].Price == "10800" {
		t.Fatalf("套餐价应重新分摊到各行")
	}
}

func TestNormalizeCreateInstitutionRequest(t *testing.T) {
	base := CreateInstitutionRequest{
		Name:        " 测试口腔 ",
		Legal:       " 测试口腔有限公司 ",
		Industry:    "口腔",
		Departments: []string{"口腔科", "咨询客服", "口腔科", " "},
		Region:      InstitutionRegion{Province: "310000", City: "310100", District: "310104"},
		Contact:     "张三",
		Phone:       "13800000000",
		Channel:     "看医界",
		Sales:       "朱媛媛",
	}
	normalized, err := base.normalize()
	if err != nil {
		t.Fatalf("合法建档入参应通过: %v", err)
	}
	if normalized.Name != "测试口腔" || normalized.Legal != "测试口腔有限公司" {
		t.Fatalf("名称未 trim: %+v", normalized)
	}
	if len(normalized.Departments) != 2 {
		t.Fatalf("科室应去重去空，实际 %v", normalized.Departments)
	}

	bad := base
	bad.Legal = " "
	if _, err := bad.normalize(); err == nil || !strings.Contains(err.Error(), "机构全称") {
		t.Fatalf("缺全称应报「请填机构全称」，实际 %v", err)
	}
	bad = base
	bad.Phone = "123"
	if _, err := bad.normalize(); err == nil || !strings.Contains(err.Error(), "手机号") {
		t.Fatalf("手机号格式应被校验，实际 %v", err)
	}
	bad = base
	bad.Departments = nil
	if _, err := bad.normalize(); err == nil {
		t.Fatalf("没有科室应被拒绝")
	}
	bad = base
	bad.Region.District = ""
	if _, err := bad.normalize(); err == nil {
		t.Fatalf("缺区县应被拒绝")
	}
}

func TestNormalizeFormalActivationRequest(t *testing.T) {
	// camelCase（前端契约）与 snake_case 都应接受。
	camel := FormalActivationRequest{
		ProductCodes: []string{"yingke", "YINGKE", ""},
		EndDate:      "2027-11-11",
		Payment:      ActivationPayment{Mode: "paid", Amount: "1000"},
	}
	normalized, err := camel.normalize()
	if err != nil {
		t.Fatalf("camelCase 入参应通过: %v", err)
	}
	if len(normalized.ProductCodes) != 1 || normalized.ProductCodes[0] != "yingke" {
		t.Fatalf("产品应去重: %v", normalized.ProductCodes)
	}
	if normalized.Payment.Mode != "paid" {
		t.Fatalf("收款方式不对: %s", normalized.Payment.Mode)
	}

	fromTrial := true
	snake := FormalActivationRequest{
		SnakeProductCodes: []string{"kehu"},
		SnakeEndDate:      "2027-11-11",
		SnakeFromTrial:    &fromTrial,
	}
	normalized, err = snake.normalize()
	if err != nil {
		t.Fatalf("snake_case 入参应通过: %v", err)
	}
	if !normalized.FromTrial || len(normalized.ProductCodes) != 1 || normalized.ProductCodes[0] != "kehu" {
		t.Fatalf("snake_case 合并失败: %+v", normalized)
	}
	if normalized.Payment.Mode != "unpaid" {
		t.Fatalf("未给收款信息时应按挂账处理，实际 %s", normalized.Payment.Mode)
	}

	if _, err := (FormalActivationRequest{EndDate: "2027-11-11"}).normalize(); err == nil {
		t.Fatalf("没有产品应被拒绝")
	}
	if _, err := (FormalActivationRequest{ProductCodes: []string{"yingke"}}).normalize(); err == nil {
		t.Fatalf("没有到期日应被拒绝")
	}
	if _, err := (FormalActivationRequest{
		ProductCodes: []string{"yingke"}, EndDate: "2027-11-11",
		Payment: ActivationPayment{Mode: "paid", Amount: "1.234"},
	}).normalize(); err == nil {
		t.Fatalf("金额超过两位小数应被拒绝")
	}
}

func TestNormalizeRenewRequest(t *testing.T) {
	normalized, err := (RenewRequest{Mode: "YEAR"}).normalize()
	if err != nil || normalized.Mode != "year" {
		t.Fatalf("整年续期应通过: %+v %v", normalized, err)
	}
	if _, err := (RenewRequest{Mode: "custom"}).normalize(); err == nil {
		t.Fatalf("按天续期缺到期日应被拒绝")
	}
	if _, err := (RenewRequest{Mode: "custom", EndDate: "2027/11/11"}).normalize(); err == nil {
		t.Fatalf("非法日期应被拒绝")
	}
	normalized, err = (RenewRequest{Mode: "custom", SnakeEndDate: "2027-11-11"}).normalize()
	if err != nil || normalized.EndDate != "2027-11-11" {
		t.Fatalf("snake_case end_date 合并失败: %+v %v", normalized, err)
	}
}

func TestNormalizeOwnershipAndPeriodPayload(t *testing.T) {
	if _, _, _, _, err := (&InstitutionService{}).normalizeOwnershipPayload(nil, OwnershipPayload{}); err == nil {
		t.Fatalf("归属缺渠道应被拒绝")
	}
	// normalizeSubscriptionPeriodPayload 只做纯校验，不需要 service。
	if _, _, _, _, err := normalizeSubscriptionPeriodPayload(SubscriptionPeriodPayload{
		StartedOn: "2027-01-01", ExpiredOn: "2026-01-01",
	}, timeNowForTest()); err == nil {
		t.Fatalf("服务期起止日倒置应被拒绝")
	}
	if _, _, _, _, err := normalizeSubscriptionPeriodPayload(SubscriptionPeriodPayload{ExpiredOn: ""}, timeNowForTest()); err == nil {
		t.Fatalf("缺到期日应被拒绝")
	}
	started, expired, status, source, err := normalizeSubscriptionPeriodPayload(SubscriptionPeriodPayload{
		StartedOn: "2026-01-01", ExpiredOn: "2026-12-31",
	}, timeNowForTest())
	if err != nil {
		t.Fatalf("合法服务期应通过: %v", err)
	}
	if formatDateOnly(started) != "2026-01-01" || formatDateOnly(expired) != "2026-12-31" ||
		status != PeriodStatusActive || source != PeriodSourceManual {
		t.Fatalf("服务期默认值不对: %+v %+v %s %s", started, expired, status, source)
	}
}

// TestActionRequestJSONContract 锁定请求体的字段名：
// 前端 billingService.ts 的 payload 是 camelCase（productCodes/bundleKey/endDate/
// fromTrial/giftNewcomer/channelId），后端必须能原样解析。
func TestActionRequestJSONContract(t *testing.T) {
	var formal FormalActivationRequest
	raw := `{"productCodes":["yingke","kehu"],"bundleKey":"deal","endDate":"2027-11-11",
	         "fromTrial":true,"giftNewcomer":true,
	         "payment":{"mode":"paid","amount":17800,"date":"2026-11-12","method":"对公转账"}}`
	if err := json.Unmarshal([]byte(raw), &formal); err != nil {
		t.Fatalf("解析正式开通请求失败: %v", err)
	}
	normalized, err := formal.normalize()
	if err != nil {
		t.Fatalf("正式开通请求校验失败: %v", err)
	}
	if len(normalized.ProductCodes) != 2 || normalized.BundleKey != "deal" || normalized.EndDate != "2027-11-11" ||
		!normalized.FromTrial || !normalized.GiftNewcomer || string(normalized.Payment.Amount) != "17800" {
		t.Fatalf("camelCase 字段未正确解析: %+v", normalized)
	}

	var trial TrialActivationRequest
	if err := json.Unmarshal([]byte(`{"productCodes":["tingxie","hegui"],"days":14}`), &trial); err != nil {
		t.Fatalf("解析试用请求失败: %v", err)
	}
	if normalized, err := trial.normalize(); err != nil || len(normalized.ProductCodes) != 2 || normalized.Days != 14 {
		t.Fatalf("试用请求字段未正确解析: %+v %v", normalized, err)
	}

	var renew RenewRequest
	if err := json.Unmarshal([]byte(`{"mode":"custom","endDate":"2027-06-30"}`), &renew); err != nil {
		t.Fatalf("解析续期请求失败: %v", err)
	}
	if normalized, err := renew.normalize(); err != nil || normalized.Mode != "custom" || normalized.EndDate != "2027-06-30" {
		t.Fatalf("续期请求字段未正确解析: %+v %v", normalized, err)
	}

	var increment IncrementRequest
	if err := json.Unmarshal([]byte(`{"code":"yingke","packs":2}`), &increment); err != nil {
		t.Fatalf("解析加量请求失败: %v", err)
	}
	if normalized, err := increment.normalize(); err != nil || normalized.Code != "yingke" || normalized.Packs != 2 {
		t.Fatalf("加量请求字段未正确解析: %+v %v", normalized, err)
	}

	var sales SalesPayload
	if err := json.Unmarshal([]byte(`{"id":3,"channelId":8,"name":"张三","phone":"13800000000","active":true}`), &sales); err != nil {
		t.Fatalf("解析销售请求失败: %v", err)
	}
	if sales.ID != 3 || sales.ChannelID != 8 || sales.Name != "张三" || !*sales.Active {
		t.Fatalf("销售请求字段未正确解析: %+v", sales)
	}

	var receive ReceivePaymentRequest
	if err := json.Unmarshal([]byte(`{"amount":1000,"date":"2026-11-12","method":"对公转账","memo":"补款"}`), &receive); err != nil {
		t.Fatalf("解析收款请求失败: %v", err)
	}
	if normalized, err := receive.normalize(); err != nil || string(normalized.Amount) != "1000" || normalized.Memo != "补款" {
		t.Fatalf("收款请求字段未正确解析: %+v %v", normalized, err)
	}
}
