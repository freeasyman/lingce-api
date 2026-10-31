package patientops

import (
	"net/http/httptest"
	"testing"
)

func TestTaskPlanFilterParsing(t *testing.T) {
	for _, v := range []string{"-1", "abc", "1.5", "9223372036854775808"} {
		if _, err := parseTaskFilterOptions(httptest.NewRequest("GET", "/?plan_id="+v, nil)); err == nil {
			t.Fatalf("accepted %s", v)
		}
	}
	for _, v := range []string{"none", "0"} {
		o, e := parseTaskFilterOptions(httptest.NewRequest("GET", "/?plan_id="+v+"&q=needle", nil))
		if e != nil || !o.NoPlan || o.Query != "needle" {
			t.Fatalf("bad parse: %+v %v", o, e)
		}
	}
}

func TestTaskPlanFilterIntegration(t *testing.T) {
	s, ctx := planAgeTestService(t)
	id := contactTestCustomer(t, s, ctx, 1, "目标客户")
	foreign := contactTestCustomer(t, s, ctx, 2, "外部客户")
	_, err := s.pool.Exec(ctx, `INSERT INTO employees(id,tenant_id,name) VALUES(2,1,'同事')`)
	if err != nil {
		t.Fatal(err)
	}
	_, err = s.pool.Exec(ctx, `INSERT INTO patient_operation_tasks(tenant_id,customer_id,employee_id,plan_id,goal,due_at) VALUES(1,$1,1,1,'needle',now()-interval '1 day'),(1,$1,1,2,'other',NULL),(1,$1,1,NULL,'manual',NULL),(1,$1,2,1,'needle',NULL),(2,$2,1,1,'needle',NULL)`, id, foreign)
	if err != nil {
		t.Fatal(err)
	}
	plan := int64(1)
	for _, tc := range []struct {
		team  bool
		query string
		opt   TaskFilterOptions
		want  int64
	}{{false, "", TaskFilterOptions{PlanID: &plan}, 1}, {true, "needle", TaskFilterOptions{PlanID: &plan}, 2}, {false, "", TaskFilterOptions{NoPlan: true}, 1}, {false, "absent", TaskFilterOptions{PlanID: &plan}, 0}} {
		tasks, n, e := s.ListPatientTasks(ctx, 1, 1, 1, 1, tc.team, "全部", tc.query, tc.opt)
		if e != nil || n != tc.want || len(tasks) > 1 {
			t.Fatalf("list: %d %+v %v", n, tasks, e)
		}
		tc.opt.Query = tc.query
		counts, e := s.PatientTaskSummary(ctx, 1, 1, tc.team, tc.opt)
		if e != nil {
			t.Fatal(e)
		}
		total := counts.Mine.Total
		if tc.team {
			total = counts.Team.Total
		}
		if int64(total) != n {
			t.Fatalf("summary mismatch %+v list=%d", counts, n)
		}
	}
	tasks, n, e := s.ListPatientTasks(ctx, 1, 1, 2, 1, true, "全部", "needle", TaskFilterOptions{PlanID: &plan})
	if e != nil || n != 2 || len(tasks) != 1 {
		t.Fatalf("pagination %d %+v %v", n, tasks, e)
	}
	tasks, n, e = s.ListPatientTasks(ctx, 1, 1, 1, 20, true, "逾期", "needle", TaskFilterOptions{PlanID: &plan})
	if e != nil || n != 1 || len(tasks) != 1 {
		t.Fatalf("time %d %+v %v", n, tasks, e)
	}
}
