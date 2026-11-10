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

func TestIsActiveSalesTaskStatus(t *testing.T) {
	active := []string{SalesTaskPending, SalesTaskProcessing, SalesTaskWaitingCustomer, SalesTaskWaitingInternal, SalesTaskPaused}
	for _, status := range active {
		if !IsActiveSalesTaskStatus(status) {
			t.Fatalf("%s should be active", status)
		}
	}
	if IsActiveSalesTaskStatus(SalesTaskCompleted) {
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

func TestDealScopeAllowsEmployee(t *testing.T) {
	all := &dealScope{EmployeeIDs: nil}
	if !all.allowsAllEmployees() || !all.allowsEmployee(42) {
		t.Fatal("tenant scope should allow every employee")
	}
	self := &dealScope{EmployeeIDs: []int64{7}}
	if self.allowsAllEmployees() {
		t.Fatal("self scope must not be treated as all employees")
	}
	if !self.allowsEmployee(7) || self.allowsEmployee(8) {
		t.Fatal("self scope should only allow the current employee")
	}
	custom := &dealScope{EmployeeIDs: []int64{2, 3}}
	if !custom.allowsEmployee(2) || !custom.allowsEmployee(3) || custom.allowsEmployee(1) {
		t.Fatal("custom scope should only allow listed employees")
	}
	empty := &dealScope{EmployeeIDs: []int64{}}
	if empty.allowsAllEmployees() || empty.allowsEmployee(1) {
		t.Fatal("empty custom scope must not allow anyone")
	}
	var nilScope *dealScope
	if nilScope.allowsEmployee(1) {
		t.Fatal("nil scope must not allow anyone")
	}
}

func TestDealScopeManagerIndependentFromVisibility(t *testing.T) {
	// 负责人即使按 custom 只看部分员工，仍然拥有更正/驳回等负责人动作。
	scope := &dealScope{EmployeeIDs: []int64{2, 3}, Manager: true}
	if !scope.Manager {
		t.Fatal("manager flag should stay true for custom scope")
	}
	consultant := &dealScope{EmployeeIDs: []int64{7}, Manager: false}
	if consultant.Manager {
		t.Fatal("consultant must not be manager")
	}
}
