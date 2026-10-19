package patientops

import (
	"testing"
	"time"
)

func planTestTime(daysAgo int) string {
	return time.Now().AddDate(0, 0, -daysAgo).Format(time.RFC3339)
}

func planTestFact() planCustomerFact {
	return planCustomerFact{
		CustomerID:   1,
		Name:         "测试客户",
		Phone:        "13800138000",
		Age:          7,
		Tags:         "[]",
		HasIdentity:  true,
		LastSourceAt: planTestTime(3),
		Interest:     `["青少年脊柱侧弯训练","体态评估"]`,
		Concern:      "价格",
	}
}

func TestNormalizePlanFilterDefaults(t *testing.T) {
	filter := normalizePlanFilter(map[string]any{})
	if filter.ContactWindowDays != 7 || filter.FollowupDays != 60 || filter.DormantDays != 90 {
		t.Fatalf("unexpected defaults: %+v", filter)
	}
	filter = normalizePlanFilter(map[string]any{"contact_window_days": float64(14), "followup_days": float64(45), "dormant_days": float64(120)})
	if filter.ContactWindowDays != 14 || filter.FollowupDays != 45 || filter.DormantDays != 120 {
		t.Fatalf("unexpected override: %+v", filter)
	}
}

func TestPlanScenarioForInviteVisitRequiresIntent(t *testing.T) {
	now := time.Now()
	fact := planTestFact()
	scenario, reason := planScenarioFor(fact, normalizePlanFilter(nil), now)
	if scenario != "invite_visit" || reason != "" {
		t.Fatalf("expected invite_visit, got %q %q", scenario, reason)
	}

	fact.Interest = "[]"
	fact.Question = "待确认"
	fact.Concern = "待确认"
	scenario, reason = planScenarioFor(fact, normalizePlanFilter(nil), now)
	if scenario != "" || reason == "" {
		t.Fatalf("expected exclusion without intent, got %q %q", scenario, reason)
	}
}

func TestPlanScenarioForVisitWindows(t *testing.T) {
	filter := normalizePlanFilter(nil)
	cases := []struct {
		daysAgo  int
		expected string
	}{
		{10, "post_visit"},
		{70, "followup_due"},
		{200, "dormant_wake"},
	}
	for _, item := range cases {
		fact := planTestFact()
		fact.LastVisitAt = planTestTime(item.daysAgo)
		scenario, reason := planScenarioFor(fact, filter, time.Now())
		if scenario != item.expected || reason != "" {
			t.Fatalf("days=%d expected %s, got %q %q", item.daysAgo, item.expected, scenario, reason)
		}
	}
}

func TestPlanExclusionRules(t *testing.T) {
	filter := normalizePlanFilter(nil)
	now := time.Now()

	fact := planTestFact()
	fact.OpenTasks = 1
	if reason := planExclusionReason(fact, filter, now, false); reason != "已有未完成任务" {
		t.Fatalf("open task should be excluded, got %q", reason)
	}

	fact = planTestFact()
	fact.LastContactedAt = planTestTime(2)
	if reason := planExclusionReason(fact, filter, now, false); reason == "" {
		t.Fatal("recent contact should be excluded")
	}

	fact = planTestFact()
	fact.Status = "lost"
	if reason := planExclusionReason(fact, filter, now, false); reason == "" {
		t.Fatal("lost customer should be excluded")
	}
}

func TestMatchPlanProduct(t *testing.T) {
	ageMin, ageMax := 3, 12
	product := planProduct{
		ID:                 36,
		Name:               "儿童体态健康·10次",
		ApplicableKeywords: []string{"体态", "脊柱侧弯"},
		AgeMin:             &ageMin,
		AgeMax:             &ageMax,
		Gender:             "all",
		Contraindications:  "急性期不做评估",
	}
	fact := planTestFact()
	matched, score, reason, risk := matchPlanProduct(fact, []planProduct{product})
	if matched == nil || matched.ID != 36 || score <= 0 || reason == "" {
		t.Fatalf("expected product match, got %+v %d %q", matched, score, reason)
	}
	if risk == "" {
		t.Fatal("expected risk text")
	}

	fact.Age = 30
	if matched, _, _, _ = matchPlanProduct(fact, []planProduct{product}); matched != nil {
		t.Fatal("age outside range must not match")
	}

	fact = planTestFact()
	fact.Interest = "[]"
	fact.Concern = "待确认"
	fact.Question = ""
	if matched, _, _, _ = matchPlanProduct(fact, []planProduct{product}); matched != nil {
		t.Fatal("customer without product signal must not match")
	}
}

func TestValidatePlan(t *testing.T) {
	target := 50000.0
	statusPlan := &Plan{Name: "邀约", PlanType: "status", Scenario: "invite_visit"}
	if err := validatePlan(statusPlan); err != nil {
		t.Fatalf("status plan should be valid: %v", err)
	}

	if err := validatePlan(&Plan{Name: "邀约", PlanType: "status", Scenario: "unknown"}); err == nil {
		t.Fatal("unknown scenario must be rejected")
	}

	if err := validatePlan(&Plan{Name: "业绩", PlanType: "sales", TargetAmount: &target}); err == nil {
		t.Fatal("sales plan without product must be rejected")
	}

	if err := validatePlan(&Plan{Name: "业绩", PlanType: "sales", TargetAmount: &target, ProductIDs: []int64{36}}); err != nil {
		t.Fatalf("valid sales plan rejected: %v", err)
	}

	if err := validatePlan(&Plan{Name: "业绩", PlanType: "sales", ProductIDs: []int64{36}}); err == nil {
		t.Fatal("sales plan without target amount must be rejected")
	}
}

func TestPlanJoinList(t *testing.T) {
	if value := planJoinList(`["脊柱侧弯","体态评估"]`); value != "脊柱侧弯、体态评估" {
		t.Fatalf("unexpected join: %q", value)
	}
	if value := planJoinList("[]"); value != "" {
		t.Fatalf("empty array should be empty, got %q", value)
	}
	if value := planJoinList("脊柱侧弯"); value != "脊柱侧弯" {
		t.Fatalf("plain string should pass through, got %q", value)
	}
}
