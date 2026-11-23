package patientops

import (
	"errors"
	"strings"
	"testing"
)

func TestScreenshotQueueAndDuplicatePaginationIntegration(t *testing.T) {
	s, ctx := contactTestService(t)
	var first int64
	for _, payload := range []string{`{"platform":"朋友圈","nickname":"同昵称"}`, `{"platform":"微信","nickname":"同昵称"}`, `{"platform":"抖音","nickname":"同昵称"}`} {
		source := contactTestSource(t, s, ctx, 1, nil, payload)
		id, _, err := s.completeScreenshotRecognition(ctx, 1, source)
		if err != nil {
			t.Fatal(err)
		}
		if first == 0 {
			first = *id
		}
	}
	// Legacy unbound and deleted-customer pending entries must not affect either total.
	contactTestSource(t, s, ctx, 1, nil, `{}`)
	deleted := contactTestCustomer(t, s, ctx, 1, "已删除")
	if _, err := s.pool.Exec(ctx, `UPDATE customers SET deleted_at=NOW() WHERE id=$1`, deleted); err != nil {
		t.Fatal(err)
	}
	contactTestSource(t, s, ctx, 1, &deleted, `{}`)
	// Multiple screenshots for one customer must count as one customer and one duplicate owner.
	extra := contactTestSource(t, s, ctx, 1, &first, `{"platform":"微信","nickname":"同昵称"}`)
	if _, _, err := s.completeScreenshotRecognition(ctx, 1, extra); err != nil {
		t.Fatal(err)
	}
	count, _, err := s.CountUnlinkedSources(ctx, 1, 0)
	if err != nil || count != 4 {
		t.Fatal(count, err)
	}
	for page := 1; page <= 4; page++ {
		list, err := s.ListUnlinkedSources(ctx, 1, page, 1)
		if err != nil {
			t.Fatal(err)
		}
		if list.Total != 4 || list.CustomerTotal != 3 || len(list.Items) != 1 {
			t.Fatalf("bad pagination %+v", list)
		}
		item := list.Items[0]
		if item.MatchStatus != "pending_confirm" {
			t.Fatal("legacy pending in queue")
		}
		expected := 2
		if item.Recognition.Platform == "抖音" {
			expected = 0
		}
		if item.DuplicateLeadCount != expected {
			t.Fatalf("bad duplicate count %+v", item)
		}
	}
	foreign, err := s.ListUnlinkedSources(ctx, 2, 1, 100)
	if err != nil || foreign.Total != 0 {
		t.Fatal("tenant leak", err)
	}
}

func TestScreenshotReuseRefreshesOnlyAutomaticLeadNameIntegration(t *testing.T) {
	s, ctx := contactTestService(t)
	create := func(payload string) int64 {
		t.Helper()
		src := contactTestSource(t, s, ctx, 1, nil, payload)
		id, _, err := s.completeScreenshotRecognition(ctx, 1, src)
		if err != nil || id == nil {
			t.Fatal(err)
		}
		return *id
	}
	id := create(`{"platform":"小红书","account":"same-id","nickname":"旧昵称"}`)
	if next := create(`{"platform":"小红书","account":"same-id","nickname":"新昵称"}`); next != id {
		t.Fatal("account not reused")
	}
	if n := contactTestCount(t, s, ctx, `SELECT count(*) FROM customers WHERE id=$1 AND name='小红书 新昵称'`, id); n != 1 {
		t.Fatal("pending name stale")
	}
	create(`{"platform":"小红书","account":"same-id"}`)
	if n := contactTestCount(t, s, ctx, `SELECT count(*) FROM customers WHERE id=$1 AND name='小红书 新昵称'`, id); n != 1 {
		t.Fatal("empty nickname overwrote display")
	}
	if _, err := s.pool.Exec(ctx, `UPDATE customers SET name='人工正式姓名',extra_data=extra_data||'{"identity_status":"confirmed"}'::jsonb WHERE id=$1`, id); err != nil {
		t.Fatal(err)
	}
	create(`{"platform":"小红书","account":"same-id","nickname":"更新昵称"}`)
	if n := contactTestCount(t, s, ctx, `SELECT count(*) FROM customers WHERE id=$1 AND name='人工正式姓名'`, id); n != 1 {
		t.Fatal("formal name overwritten")
	}
}

func TestPlanExplicitUnconfirmedCustomersIntegration(t *testing.T) {
	s, ctx := contactTestService(t)
	// Supply only fields required by loadPlan. Rejection must happen before candidate/model loading.
	if _, err := s.pool.Exec(ctx, `ALTER TABLE patient_ops_plans ADD name text DEFAULT '测试计划',ADD plan_type text DEFAULT 'status',ADD scenario text DEFAULT 'new_consultation',ADD period_start date,ADD period_end date,ADD target_count int,ADD target_amount numeric,ADD product_ids jsonb DEFAULT '[]',ADD filter_config jsonb DEFAULT '{}',ADD created_by bigint DEFAULT 1,ADD published_at timestamptz,ADD created_at timestamptz DEFAULT now(),ADD updated_at timestamptz DEFAULT now(); INSERT INTO patient_ops_plans(id,tenant_id,status) VALUES(1,1,'draft'),(2,1,'published')`); err != nil {
		t.Fatal(err)
	}
	source := contactTestSource(t, s, ctx, 1, nil, `{"platform":"微信","nickname":"待确认人"}`)
	id, _, err := s.completeScreenshotRecognition(ctx, 1, source)
	if err != nil {
		t.Fatal(err)
	}
	formal := contactTestCustomer(t, s, ctx, 1, "正式客户")
	foreign := contactTestCustomer(t, s, ctx, 2, "别租户")
	if _, err := s.pool.Exec(ctx, `UPDATE customers SET extra_data='{"identity_status":"pending_confirm"}' WHERE id=$1`, foreign); err != nil {
		t.Fatal(err)
	}
	if err := s.rejectUnconfirmedPlanCustomers(ctx, 1, []int64{foreign, formal}); err != nil {
		t.Fatal("foreign customer leaked", err)
	}
	for _, publish := range []bool{true, false} {
		var err error
		if publish {
			_, err = s.PublishPlan(ctx, 1, 1, 1, PublishPlanRequest{CustomerIDs: []int64{formal, *id}})
		} else {
			_, err = s.AddPlanMembers(ctx, 1, 1, 2, AddPlanMembersRequest{CustomerIDs: []int64{formal, *id}})
		}
		var rejected *UnconfirmedPlanCustomersError
		if !errors.As(err, &rejected) || len(rejected.Customers) != 1 || rejected.Customers[0].CustomerID != *id || !strings.Contains(err.Error(), "本次请求未执行") {
			t.Fatalf("missing structured reason: %v", err)
		}
	}
	if n := contactTestCount(t, s, ctx, `SELECT count(*) FROM patient_ops_plan_members`); n != 0 {
		t.Fatal("partial write")
	}
	if n := contactTestCount(t, s, ctx, `SELECT count(*) FROM patient_operation_tasks`); n != 0 {
		t.Fatal("task created")
	}
}
