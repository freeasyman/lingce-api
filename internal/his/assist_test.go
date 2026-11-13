package his

import (
	"strings"
	"testing"
)

func TestSummarizePresentIllness(t *testing.T) {
	if got := summarizePresentIllness("   ", assistPresentIllnessLimit); got != "" {
		t.Fatalf("blank text = %q, want empty", got)
	}
	if got := summarizePresentIllness("  白带增多伴异味  ", assistPresentIllnessLimit); got != "白带增多伴异味" {
		t.Fatalf("trimmed text = %q", got)
	}
	exact := strings.Repeat("病", assistPresentIllnessLimit)
	if got := summarizePresentIllness(exact, assistPresentIllnessLimit); got != exact {
		t.Fatalf("exact limit text should not be truncated, got %q", got)
	}
	longer := strings.Repeat("病", assistPresentIllnessLimit+1)
	got := summarizePresentIllness(longer, assistPresentIllnessLimit)
	if want := strings.Repeat("病", assistPresentIllnessLimit) + "…"; got != want {
		t.Fatalf("over limit text = %q, want %q", got, want)
	}
	if got := summarizePresentIllness("内容", 0); got != "" {
		t.Fatalf("limit 0 = %q, want empty", got)
	}
}

func TestRankFrequentDiagnoses(t *testing.T) {
	if got := rankFrequentDiagnoses(nil, assistDiagnosisLimit); got == nil || len(got) != 0 {
		t.Fatalf("nil counts should return empty non-nil slice, got %#v", got)
	}

	counts := map[string]int{
		"高血压":   3,
		" 高血压 ": 2,
		"糖尿病":   1,
		"感冒":    5,
		"":      9,
		"零次":    0,
	}
	got := rankFrequentDiagnoses(counts, assistDiagnosisLimit)
	if len(got) != 3 {
		t.Fatalf("len = %d, want 3: %#v", len(got), got)
	}
	// 次数倒序；同次数按名称升序；同名去空格合并。
	if got[0].Name != "感冒" || got[0].Count != 5 {
		t.Fatalf("first = %#v, want 感冒 5", got[0])
	}
	if got[1].Name != "高血压" || got[1].Count != 5 {
		t.Fatalf("second = %#v, want 高血压 5", got[1])
	}
	if got[2].Name != "糖尿病" || got[2].Count != 1 {
		t.Fatalf("third = %#v, want 糖尿病 1", got[2])
	}

	if limited := rankFrequentDiagnoses(counts, 2); len(limited) != 2 {
		t.Fatalf("limit 2 len = %d", len(limited))
	}
	if zero := rankFrequentDiagnoses(counts, 0); zero == nil || len(zero) != 0 {
		t.Fatalf("limit 0 should return empty non-nil slice, got %#v", zero)
	}
}

func TestClassifyFeeBucket(t *testing.T) {
	cases := []struct {
		category string
		itemType string
		want     string
	}{
		{"挂号", "", "registration"},
		{"诊查", "", "registration"},
		{"药品", "drug", "prescription"},
		{"", "drug", "prescription"},
		{"检验", "charge_item", "lab"},
		{"检查", "charge_item", "exam"},
		{"治疗", "charge_item", "treatment"},
		{"材料", "", "material"},
		{"耗材", "", "material"},
		{"其他", "", "other"},
		{"", "registration_fee", "registration"},
		{"", "", "other"},
	}
	for _, item := range cases {
		if got := classifyFeeBucket(item.category, item.itemType); got != item.want {
			t.Fatalf("classifyFeeBucket(%q, %q) = %q, want %q", item.category, item.itemType, got, item.want)
		}
	}
}

func TestSummarizeAssistFee(t *testing.T) {
	if got := summarizeAssistFee(nil); got.Status != "未开单" || got.Total != 0 {
		t.Fatalf("empty fee = %#v", got)
	}

	orders := []Order{
		{
			Status:        "void",
			PayableTotal:  999,
			PaidTotal:     999,
			RefundedTotal: 999,
			Items:         []OrderItem{{Name: "作废项", Category: "检验", Payable: 999}},
		},
		{
			Status:        "paid",
			PaidTotal:     90,
			RefundedTotal: 10,
			Items: []OrderItem{
				{Name: "挂号费", ItemType: "registration_fee", Category: "挂号", Quantity: 1, Payable: 10},
				{Name: "甲硝唑片", ItemType: "drug", Category: "药品", Quantity: 1, Payable: 40},
				{Name: "血常规", ItemType: "charge_item", Category: "检验", Quantity: 1, Payable: 25},
				{Name: "妇科B超", ItemType: "charge_item", Category: "检查", Quantity: 1, Payable: 50, RefundedQuantity: 1},
				{Name: "红光治疗", ItemType: "charge_item", Category: "治疗", Quantity: 1, Payable: 88},
				{Name: "一次性注射器", ItemType: "charge_item", Category: "材料", Quantity: 2, Payable: 2.4},
				{Name: "其他", ItemType: "charge_item", Category: "其他", Quantity: 1, Payable: 1},
			},
		},
		{
			Status: "pending",
			Items:  []OrderItem{{Name: "复查B超", Category: "检查", Quantity: 1, Payable: 20}},
		},
	}
	got := summarizeAssistFee(orders)
	if got.Registration != 10 || got.Prescription != 40 || got.Lab != 25 ||
		got.Exam != 20 || got.Treatment != 88 || got.Material != 2.4 || got.Other != 1 {
		t.Fatalf("bucket amounts = %#v", got)
	}
	// 合计 = 10 + 40 + 25 + (50-50) + 88 + 2.4 + 1 + 20 = 186.40
	if got.Total != 186.4 {
		t.Fatalf("total = %v, want 186.4", got.Total)
	}
	if got.Paid != 80 || got.Unpaid != 106.4 || got.Refunded != 10 {
		t.Fatalf("paid/unpaid/refunded = %v/%v/%v", got.Paid, got.Unpaid, got.Refunded)
	}
	if got.Status != "部分收" {
		t.Fatalf("status = %q, want 部分收", got.Status)
	}

	refunded := summarizeAssistFee([]Order{{
		Status:        "refunded",
		PaidTotal:     100,
		RefundedTotal: 100,
		Items:         []OrderItem{{Name: "检查", Category: "检查", Quantity: 1, Payable: 100, RefundedQuantity: 1}},
	}})
	if refunded.Total != 0 || refunded.Refunded != 100 || refunded.Status != "已退" {
		t.Fatalf("fully refunded = %#v", refunded)
	}

	paid := summarizeAssistFee([]Order{{
		Status:    "paid",
		PaidTotal: 30,
		Items:     []OrderItem{{Name: "检验", Category: "检验", Quantity: 1, Payable: 30}},
	}})
	if paid.Paid != 30 || paid.Unpaid != 0 || paid.Status != "已收" {
		t.Fatalf("fully paid = %#v", paid)
	}

	voidOnly := summarizeAssistFee([]Order{{Status: "void"}})
	if voidOnly.Status != "已退" || voidOnly.Total != 0 {
		t.Fatalf("void only = %#v", voidOnly)
	}
}

func TestBuildRecordSummary(t *testing.T) {
	got := buildRecordSummary("入睡困难3年余", "3年前入睡困难、多梦。",
		[]string{"高血压", "睡眠障碍"}, "【处方】\nP1（已开具）\n1. 甲硝唑片")
	want := "主诉：入睡困难3年余\n现病史：3年前入睡困难、多梦。\n诊断：高血压、睡眠障碍\n处置/医嘱：\n【处方】\nP1（已开具）\n1. 甲硝唑片"
	if got != want {
		t.Fatalf("summary = %q, want %q", got, want)
	}
	if got := buildRecordSummary(" ", "", nil, "  "); got != "" {
		t.Fatalf("empty summary = %q, want empty", got)
	}
	if got := buildRecordSummary("", "", nil, "【检验】\n血常规"); got != "处置/医嘱：\n【检验】\n血常规" {
		t.Fatalf("orders-only summary = %q", got)
	}
}

func TestDiagnosisNamesFromContent(t *testing.T) {
	content := map[string]any{
		"diagnosis":     map[string]any{"diagnosis_name": "高血压"},
		"tcm_diagnosis": map[string]any{"diagnosis_name": "眩晕"},
		"diagnoses": []any{
			map[string]any{"name": "高血压"},
			map[string]any{"diagnosis_name": "睡眠障碍"},
			map[string]any{"name": " "},
		},
	}
	got := diagnosisNamesFromContent(content)
	want := []string{"高血压", "眩晕", "睡眠障碍"}
	if len(got) != len(want) {
		t.Fatalf("names = %#v, want %#v", got, want)
	}
	for index := range want {
		if got[index] != want[index] {
			t.Fatalf("names = %#v, want %#v", got, want)
		}
	}
	if got := diagnosisNamesFromContent(nil); got == nil || len(got) != 0 {
		t.Fatalf("nil content should return empty non-nil slice, got %#v", got)
	}
}

func TestBuildOrdersText(t *testing.T) {
	days := 7
	prescriptions := []Prescription{{
		ID: 1, PrescriptionNo: "P1", Status: "issued",
		Items: []PrescriptionItem{{
			Name: "甲硝唑片", Spec: "0.2g", Route: "口服", Frequency: "bid",
			Days: &days, Quantity: 1, Unit: "盒", Amount: 6.5,
		}},
	}}
	requests := []ServiceRequest{
		{RequestType: "lab", ItemName: "血常规", Status: "pending"},
		{RequestType: "exam", ItemName: "妇科B超", Status: "reported"},
	}
	treatments := []TreatmentOrder{{ChargeItemName: "红光治疗", PlanTotalTimes: 2, Status: "ordered"}}
	orders := []Order{{
		Status: "paid",
		Items: []OrderItem{
			{Name: "血常规", Category: "检验", Quantity: 1, Payable: 25},
			{Name: "妇科B超", Category: "检查", Quantity: 1, Payable: 80},
			{Name: "红光治疗", Category: "治疗", Quantity: 1, Payable: 88},
			{Name: "一次性注射器", Category: "材料", Quantity: 2, Payable: 2.4},
		},
	}}
	got := buildOrdersText(prescriptions, requests, treatments, orders)
	for _, want := range []string{
		"【处方】", "P1（已开具）", "甲硝唑片 0.2g 口服 bid 7天 1盒 ¥6.50",
		"【检验】\n血常规（待处理）", "【检查】\n妇科B超（已出报告）",
		"【治疗】\n红光治疗 ×2次（已开单）", "【诊疗项目】\n一次性注射器 ×2 ¥2.40",
	} {
		if !strings.Contains(got, want) {
			t.Fatalf("orders text missing %q:\n%s", want, got)
		}
	}
	if strings.Contains(got, "妇科B超 ¥80.00") {
		t.Fatalf("service request covered order item should not be duplicated:\n%s", got)
	}

	// 没有临床申请/医嘱时，回退收费行；同类多行都要保留，不因第一行而丢项。
	fallback := buildOrdersText(nil, nil, nil, []Order{{
		Status: "pending",
		Items: []OrderItem{
			{Name: "血常规", Category: "检验", Quantity: 1, Payable: 25},
			{Name: "尿常规", Category: "检验", Quantity: 1, Payable: 15},
			{Name: "妇科B超", Category: "检查", Quantity: 1, Payable: 80},
			{Name: "妇科检查", Category: "检查", Quantity: 1, Payable: 50, RefundedQuantity: 1},
			{Name: "红光治疗", Category: "治疗", Quantity: 1, Payable: 88},
		},
	}})
	for _, want := range []string{
		"【检验】\n血常规 ¥25.00\n尿常规 ¥15.00",
		"【检查】\n妇科B超 ¥80.00\n妇科检查 ¥50.00（已退）",
		"【治疗】\n红光治疗 ¥88.00",
	} {
		if !strings.Contains(fallback, want) {
			t.Fatalf("fallback orders text missing %q:\n%s", want, fallback)
		}
	}

	if got := buildOrdersText(nil, nil, nil, nil); got != "" {
		t.Fatalf("empty orders text = %q, want empty", got)
	}
}

func TestBuildRecordText(t *testing.T) {
	content := map[string]any{
		"present_illness": "3天前出现白带增多。",
		"chief_complaint": "白带增多5天",
		"custom_note":     "自定义字段",
		"diagnosis":       map[string]any{"diagnosis_name": "宫颈炎性疾病"},
		"empty_field":     " ",
	}
	got := buildRecordText([]string{"门诊病历", "姓名：演示-李静"}, content, []string{"宫颈炎性疾病"})
	want := strings.Join([]string{
		"门诊病历",
		"姓名：演示-李静",
		"主诉：白带增多5天",
		"现病史：3天前出现白带增多。",
		"custom_note：自定义字段",
		"诊断：宫颈炎性疾病",
	}, "\n")
	if got != want {
		t.Fatalf("record text = %q, want %q", got, want)
	}

	if got := buildRecordText(nil, map[string]any{"empty": " "}, nil); got != "" {
		t.Fatalf("empty record text = %q, want empty", got)
	}
}

func TestAssistGenderLabel(t *testing.T) {
	cases := map[string]string{
		"female":  "女",
		"male":    "男",
		"女":       "女",
		"男":       "男",
		"unknown": "unknown",
		"":        "",
	}
	for raw, want := range cases {
		if got := assistGenderLabel(raw); got != want {
			t.Fatalf("assistGenderLabel(%q) = %q, want %q", raw, got, want)
		}
	}
}
