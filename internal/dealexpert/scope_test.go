package dealexpert

import "testing"

func TestEvaluateDealScope(t *testing.T) {
	cases := []struct {
		name         string
		roles        []string
		allowed      bool
		allEmployees bool
		code         string
	}{
		{name: "consultant", roles: []string{"consultant"}, allowed: true, allEmployees: false, code: "consultant"},
		{name: "sales_manager", roles: []string{"sales_manager"}, allowed: true, allEmployees: true, code: "manager"},
		{name: "director", roles: []string{"director"}, allowed: true, allEmployees: true, code: "manager"},
		{name: "tenant admin", roles: []string{"admin"}, allowed: true, allEmployees: true, code: "manager"},
		{name: "operating_manager", roles: []string{"operating_manager"}, allowed: true, allEmployees: true, code: "manager"},
		{name: "reception_manager", roles: []string{"reception_manager"}, allowed: true, allEmployees: true, code: "manager"},
		{name: "operating_supervisor", roles: []string{"operating_supervisor"}, allowed: true, allEmployees: true, code: "manager"},
		{name: "marketing_manager", roles: []string{"marketing_manager"}, allowed: true, allEmployees: true, code: "manager"},
		{name: "doctor denied", roles: []string{"doctor"}, allowed: false, code: "denied"},
		{name: "receptionist denied", roles: []string{"receptionist"}, allowed: false, code: "denied"},
		{name: "consultant+manager", roles: []string{"consultant", "sales_manager"}, allowed: true, allEmployees: true, code: "manager"},
		{name: "followup denied", roles: []string{"followup"}, allowed: false, code: "denied"},
		{name: "staff denied", roles: []string{"staff"}, allowed: false, code: "denied"},
		{name: "unknown role denied", roles: []string{"nurse"}, allowed: false, code: "denied"},
		{name: "no role denied", roles: nil, allowed: false, code: "unassigned"},
		{name: "customer service denied", roles: []string{"customer_service"}, allowed: false, code: "denied"},
		{name: "nurse denied", roles: []string{"nurse"}, allowed: false, code: "denied"},
	}
	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			rule := EvaluateDealScope(testCase.roles)
			if rule.Allowed != testCase.allowed {
				t.Fatalf("allowed = %v, want %v", rule.Allowed, testCase.allowed)
			}
			if rule.AllEmployees != testCase.allEmployees {
				t.Fatalf("allEmployees = %v, want %v", rule.AllEmployees, testCase.allEmployees)
			}
			if rule.Code != testCase.code {
				t.Fatalf("code = %s, want %s", rule.Code, testCase.code)
			}
		})
	}
}

func TestIsActiveFollowUpStatus(t *testing.T) {
	active := []string{FollowUpPending, FollowUpProcessing, FollowUpWaitingCustomer, FollowUpWaitingInternal, FollowUpPaused}
	for _, status := range active {
		if !IsActiveFollowUpStatus(status) {
			t.Fatalf("%s should be active", status)
		}
	}
	if IsActiveFollowUpStatus(FollowUpCompleted) {
		t.Fatal("completed should not be active")
	}
}

func TestResultRulesCoverAllResults(t *testing.T) {
	results := []string{
		ResultAppointed, ResultDeposit, ResultPaid, ResultPostponed, ResultAbandoned,
		ResultWaitingCustomer, ResultNeedManager, ResultNeedSpecialist, ResultPauseContact,
	}
	for _, result := range results {
		if _, ok := resultRules[result]; !ok {
			t.Fatalf("result %s has no rule", result)
		}
	}
	// 预约不等于付款：只有已付款要求金额。
	if resultRules[ResultPaid].outcomeType != "付款" || !resultRules[ResultPaid].needAmount {
		t.Fatal("paid result must create a payment outcome and require amount")
	}
	if resultRules[ResultAppointed].needAmount {
		t.Fatal("appointment must not require amount")
	}
}
