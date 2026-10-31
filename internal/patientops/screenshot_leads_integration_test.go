package patientops

import (
	"encoding/json"
	"github.com/freeasyman/lingce-api/pkg/llmgateway"
	"net/http"
	"net/http/httptest"
	"sync"
	"sync/atomic"
	"testing"

	"os"
)

func TestScreenshotAutomaticLeadRulesIntegration(t *testing.T) {
	s, ctx := contactTestService(t)
	create := func(tenant int64, payload string) (int64, *int64, string) {
		t.Helper()
		source := contactTestSource(t, s, ctx, tenant, nil, payload)
		id, status, err := s.completeScreenshotRecognition(ctx, tenant, source)
		if err != nil {
			t.Fatal(err)
		}
		return source, id, status
	}
	source, id, status := create(1, `{"platform":"朋友圈","nickname":"刘女士"}`)
	if id == nil || status != "pending_confirm" {
		t.Fatalf("missing pending lead: %v %s", id, status)
	}
	if n := contactTestCount(t, s, ctx, `SELECT count(*) FROM customers WHERE id=$1 AND name='微信 刘女士' AND coalesce(phone,'')='' AND extra_data->>'identity_status'='pending_confirm'`, *id); n != 1 {
		t.Fatal("incorrect name or fabricated phone")
	}
	if n := contactTestCount(t, s, ctx, `SELECT count(*) FROM customer_identities WHERE customer_id=$1 AND coalesce(channel_id,'')='' AND nickname='刘女士'`, *id); n != 1 {
		t.Fatal("nickname promoted to account")
	}
	idAgain, statusAgain, err := s.completeScreenshotRecognition(ctx, 1, source)
	if err != nil || *idAgain != *id || statusAgain != status {
		t.Fatal("retry lost pending state", err)
	}
	_, other, _ := create(1, `{"platform":"微信","nickname":"刘女士"}`)
	if *other == *id {
		t.Fatal("nickname deduplication")
	}
	_, unknown, _ := create(1, `{"platform":"未知","nickname":"刘女士"}`)
	if n := contactTestCount(t, s, ctx, `SELECT count(*) FROM customers WHERE id=$1 AND name='刘女士'`, *unknown); n != 1 {
		t.Fatal("unknown prefix")
	}
	_, accountOnly, _ := create(1, `{"platform":"小红书","account":"XHS-123"}`)
	if n := contactTestCount(t, s, ctx, `SELECT count(*) FROM customers WHERE id=$1 AND name='小红书 XHS-123'`, *accountOnly); n != 1 {
		t.Fatal("account-only display fallback")
	}
	_, platformOnly, _ := create(1, `{"platform":"抖音"}`)
	if platformOnly == nil {
		t.Fatal("platform-only lead not created")
	}
	_, none, emptyStatus := create(1, `{}`)
	if none != nil || emptyStatus != "pending" {
		t.Fatal("empty source created lead")
	}
	existing := contactTestCustomer(t, s, ctx, 1, "真实客户")
	linked := contactTestSource(t, s, ctx, 1, &existing, `{"platform":"微信","nickname":"其他昵称"}`)
	bound, st, err := s.completeScreenshotRecognition(ctx, 1, linked)
	if err != nil || *bound != existing || st != "confirmed" {
		t.Fatal("explicit binding overwritten", err)
	}
	queue, err := s.ListUnlinkedSources(ctx, 1, 1, 100)
	if err != nil {
		t.Fatal(err)
	}
	count, err := s.CountUnlinkedSources(ctx, 1)
	if err != nil || count != queue.Total || count != 5 {
		t.Fatalf("queue/count mismatch: %+v %d %v", queue, count, err)
	}
	detail, err := s.GetUnlinkedSource(ctx, 1, source)
	if err != nil || detail.CustomerID == nil {
		t.Fatal("auto bound source absent from review", err)
	}
	statusResult, err := s.GetScreenshotStatus(ctx, 1, source)
	if err != nil || statusResult.CustomerID == nil || *statusResult.CustomerID != *id || statusResult.MatchStatus != "pending_confirm" {
		t.Fatal("status lost binding", err)
	}
}

func TestScreenshotAutomaticAccountConcurrencyIntegration(t *testing.T) {
	s, ctx := contactTestService(t)
	foreign := contactTestCustomer(t, s, ctx, 2, "跨租户")
	deleted := contactTestCustomer(t, s, ctx, 1, "已删除")
	for _, id := range []int64{foreign, deleted} {
		if _, err := s.pool.Exec(ctx, `INSERT INTO customer_identities(customer_id,channel_type,external_id) VALUES($1,'微信','WX-1')`, id); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := s.pool.Exec(ctx, `UPDATE customers SET deleted_at=NOW() WHERE id=$1`, deleted); err != nil {
		t.Fatal(err)
	}
	const n = 12
	sources := make([]int64, n)
	for i := range sources {
		sources[i] = contactTestSource(t, s, ctx, 1, nil, `{"platform":"wechat","account":"WX-1","nickname":"张女士"}`)
	}
	var wg sync.WaitGroup
	ids := make(chan int64, n)
	errs := make(chan error, n)
	for _, source := range sources {
		wg.Add(1)
		go func(source int64) {
			defer wg.Done()
			id, _, err := s.completeScreenshotRecognition(ctx, 1, source)
			if err != nil {
				errs <- err
				return
			}
			ids <- *id
		}(source)
	}
	wg.Wait()
	close(errs)
	close(ids)
	for err := range errs {
		t.Fatal(err)
	}
	var first int64
	for id := range ids {
		if first == 0 {
			first = id
		}
		if id != first || id == foreign || id == deleted {
			t.Fatal("unsafe reuse", id, first)
		}
	}
	if n := contactTestCount(t, s, ctx, `SELECT count(*) FROM customer_identities WHERE customer_id=$1`, first); n != 1 {
		t.Fatal("duplicate identity")
	}
	source := contactTestSource(t, s, ctx, 1, nil, `{"platform":"抖音","account":"WX-1"}`)
	id, _, err := s.completeScreenshotRecognition(ctx, 1, source)
	if err != nil || *id == first {
		t.Fatal("cross-platform reuse", err)
	}
	// Canonical lookup reuses legacy Chinese platform aliases too.
	live := contactTestCustomer(t, s, ctx, 1, "已有客户")
	if _, err := s.pool.Exec(ctx, `INSERT INTO customer_identities(customer_id,channel_type,external_id) VALUES($1,'微信朋友圈','legacy-id')`, live); err != nil {
		t.Fatal(err)
	}
	source = contactTestSource(t, s, ctx, 1, nil, `{"platform":"weixin","account":"legacy-id"}`)
	id, _, err = s.completeScreenshotRecognition(ctx, 1, source)
	if err != nil || *id != live {
		t.Fatal("legacy alias not reused", err)
	}
}

func TestScreenshotReviewConfirmRejectAndAdmissionIntegration(t *testing.T) {
	s, ctx := contactTestService(t)
	source := contactTestSource(t, s, ctx, 1, nil, `{"platform":"微信","account":"shared","nickname":"昵称"}`)
	id, _, err := s.completeScreenshotRecognition(ctx, 1, source)
	if err != nil {
		t.Fatal(err)
	}
	source2 := contactTestSource(t, s, ctx, 1, nil, `{"platform":"微信","account":"shared"}`)
	if _, _, err := s.completeScreenshotRecognition(ctx, 1, source2); err != nil {
		t.Fatal(err)
	}
	if _, err := s.pool.Exec(ctx, `INSERT INTO patient_operation_tasks(tenant_id,customer_id,employee_id) VALUES(1,$1,1)`, *id); err == nil {
		t.Fatal("unconfirmed formal task admitted")
	}
	if _, err := s.pool.Exec(ctx, `INSERT INTO patient_ops_plan_members(tenant_id,plan_id,customer_id) VALUES(1,1,$1)`, *id); err == nil {
		t.Fatal("unconfirmed plan admitted")
	}
	if _, err := s.ReviewScreenshot(ctx, 2, 1, source, ScreenshotReviewRequest{Action: "reject"}); err == nil {
		t.Fatal("cross tenant review")
	}
	if _, err := s.ReviewScreenshot(ctx, 1, 9, source, ScreenshotReviewRequest{Action: "reject", ExpectedMatchStatus: "confirmed"}); err == nil {
		t.Fatal("stale review accepted")
	}
	if _, err := s.ReviewScreenshot(ctx, 1, 9, source, ScreenshotReviewRequest{Action: "reject"}); err != nil {
		t.Fatal(err)
	}
	if n := contactTestCount(t, s, ctx, `SELECT count(*) FROM customers WHERE id=$1 AND deleted_at IS NULL`, *id); n != 1 {
		t.Fatal("shared lead deleted")
	}
	again, st, err := s.completeScreenshotRecognition(ctx, 1, source)
	if err != nil || *again != *id || st != "rejected" {
		t.Fatal("rejection lost on duplicate", err)
	}
	if _, err := s.ReviewScreenshot(ctx, 1, 9, source2, ScreenshotReviewRequest{Action: "confirm"}); err == nil {
		t.Fatal("blank confirmation")
	}
	if _, err := s.ReviewScreenshot(ctx, 1, 9, source2, ScreenshotReviewRequest{Action: "confirm", Name: "真实姓名"}); err != nil {
		t.Fatal(err)
	}
	if n := contactTestCount(t, s, ctx, `SELECT count(*) FROM customers WHERE id=$1 AND name='真实姓名' AND extra_data->>'identity_status'='confirmed'`, *id); n != 1 {
		t.Fatal("identity not confirmed")
	}
	if _, err := s.pool.Exec(ctx, `INSERT INTO patient_operation_tasks(tenant_id,customer_id,employee_id) VALUES(1,$1,1)`, *id); err != nil {
		t.Fatal(err)
	}
	if _, err := s.pool.Exec(ctx, `INSERT INTO patient_ops_plan_members(tenant_id,plan_id,customer_id) VALUES(1,1,$1)`, *id); err != nil {
		t.Fatal(err)
	}
	if n := contactTestCount(t, s, ctx, `SELECT count(*) FROM screenshot_customer_reviews WHERE operator_id=9`); n != 2 {
		t.Fatal("reviews unaudited")
	}
}

func TestScreenshotLeadMergeAndRollbackIntegration(t *testing.T) {
	s, ctx := contactTestService(t)
	for _, q := range []string{`CREATE TABLE patient_operation_records(id bigserial,tenant_id bigint,customer_id bigint,content text)`, `CREATE TABLE patient_operation_task_drafts(id bigserial,tenant_id bigint,customer_id bigint,content text)`} {
		if _, err := s.pool.Exec(ctx, q); err != nil {
			t.Fatal(err)
		}
	}
	source := contactTestSource(t, s, ctx, 1, nil, `{"platform":"小红书","nickname":"线索昵称"}`)
	from, _, err := s.completeScreenshotRecognition(ctx, 1, source)
	if err != nil {
		t.Fatal(err)
	}
	target := contactTestCustomer(t, s, ctx, 1, "已有客户")
	for _, table := range []string{"patient_operation_records", "patient_operation_task_drafts"} {
		if _, err := s.pool.Exec(ctx, `INSERT INTO `+table+`(tenant_id,customer_id,content) VALUES(1,$1,'历史记录')`, *from); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := s.pool.Exec(ctx, `INSERT INTO customer_interactions(customer_id,content) VALUES($1,'沟通')`, *from); err != nil {
		t.Fatal(err)
	}
	// A forced failure after source migrations must roll all writes back.
	if _, err := s.pool.Exec(ctx, `ALTER TABLE customer_identities ADD CHECK(customer_id<>`+intString(target)+`)`); err != nil {
		t.Fatal(err)
	}
	req := ScreenshotReviewRequest{Action: "merge", CustomerID: target, ExpectedCustomerID: from, ExpectedMatchStatus: "pending_confirm"}
	if _, err := s.ReviewScreenshot(ctx, 1, 7, source, req); err == nil {
		t.Fatal("expected merge failure")
	}
	if n := contactTestCount(t, s, ctx, `SELECT count(*) FROM patient_operation_records WHERE customer_id=$1`, *from); n != 1 {
		t.Fatal("merge partially committed")
	}
	if n := contactTestCount(t, s, ctx, `SELECT count(*) FROM customers WHERE id=$1 AND deleted_at IS NULL`, *from); n != 1 {
		t.Fatal("source lost on failure")
	}
	if _, err := s.pool.Exec(ctx, `ALTER TABLE customer_identities DROP CONSTRAINT customer_identities_customer_id_check`); err != nil {
		t.Fatal(err)
	}
	if _, err := s.ReviewScreenshot(ctx, 1, 7, source, req); err != nil {
		t.Fatal(err)
	}
	for _, table := range []string{"patient_operation_records", "patient_operation_task_drafts", "customer_interactions", "customer_identities", "patient_conversation_sources"} {
		if n := contactTestCount(t, s, ctx, `SELECT count(*) FROM `+table+` WHERE customer_id=$1`, target); n != 1 {
			t.Fatal("merge lost " + table)
		}
	}
	if n := contactTestCount(t, s, ctx, `SELECT count(*) FROM customers WHERE id=$1 AND deleted_at IS NOT NULL AND extra_data->>'identity_status'='merged'`, *from); n != 1 {
		t.Fatal("source not retired")
	}
	if n := contactTestCount(t, s, ctx, `SELECT count(*) FROM screenshot_customer_reviews WHERE action='merge' AND operator_id=7`); n != 1 {
		t.Fatal("merge audit missing")
	}
}
func intString(v int64) string { b, _ := json.Marshal(v); return string(b) }

func TestScreenshotAutomaticRollbackDeletedBindingIntegration(t *testing.T) {
	s, ctx := contactTestService(t)
	source := contactTestSource(t, s, ctx, 1, nil, `{"platform":"微信","nickname":"拒绝写入"}`)
	if _, err := s.pool.Exec(ctx, `ALTER TABLE customer_identities ADD CHECK(nickname<>'拒绝写入')`); err != nil {
		t.Fatal(err)
	}
	if _, _, err := s.completeScreenshotRecognition(ctx, 1, source); err == nil {
		t.Fatal("expected contact failure")
	}
	if n := contactTestCount(t, s, ctx, `SELECT count(*) FROM customers`); n != 0 {
		t.Fatal("orphan created after rollback")
	}
	deleted := contactTestCustomer(t, s, ctx, 1, "旧档案")
	if _, err := s.pool.Exec(ctx, `UPDATE customers SET deleted_at=NOW() WHERE id=$1`, deleted); err != nil {
		t.Fatal(err)
	}
	linked := contactTestSource(t, s, ctx, 1, &deleted, `{"platform":"微信","nickname":"旧截图"}`)
	id, _, err := s.completeScreenshotRecognition(ctx, 1, linked)
	if err != nil || *id != deleted {
		t.Fatal("deleted binding replaced", err)
	}
	if _, err := s.ReviewScreenshot(ctx, 1, 1, linked, ScreenshotReviewRequest{Action: "confirm", Name: "误恢复"}); err == nil {
		t.Fatal("deleted customer confirmed")
	}
	// New status constraint actually rejects unknown states.
	if _, err := s.pool.Exec(ctx, `UPDATE patient_conversation_sources SET match_status='made_up' WHERE id=$1`, source); err == nil {
		t.Fatal("state check absent")
	}
	// Migration can be replayed without losing reviews.
	sql, err := os.ReadFile("../store/migrations/20261031_074_screenshot_customer_leads.sql")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := s.pool.Exec(ctx, string(sql)); err != nil {
		t.Fatal(err)
	}

}

func TestScreenshotBackfillIdempotenceAndAtomicityIntegration(t *testing.T) {
	s, ctx := contactTestService(t)
	one := contactTestSource(t, s, ctx, 1, nil, `{"platform":"小红书","nickname":"线索一"}`)
	bad := contactTestSource(t, s, ctx, 1, nil, `{"platform":"微信","nickname":"失败"}`)
	if _, err := s.pool.Exec(ctx, `UPDATE patient_conversation_sources SET analysis_status='failed' WHERE id=$1`, bad); err != nil {
		t.Fatal(err)
	}
	if _, err := s.BackfillScreenshotLeads(ctx, 1, 5, []int64{one, bad}); err == nil {
		t.Fatal("incomplete screenshot admitted")
	}
	if n := contactTestCount(t, s, ctx, `SELECT count(*) FROM customers`); n != 0 {
		t.Fatal("partial batch applied")
	}
	deleted := contactTestCustomer(t, s, ctx, 1, "已删除档案")
	if _, err := s.pool.Exec(ctx, `UPDATE customers SET deleted_at=NOW() WHERE id=$1`, deleted); err != nil {
		t.Fatal(err)
	}
	linked := contactTestSource(t, s, ctx, 1, &deleted, `{"platform":"微信","nickname":"旧截图"}`)
	empty := contactTestSource(t, s, ctx, 1, nil, `{}`)
	result, err := s.BackfillScreenshotLeads(ctx, 1, 5, []int64{one, one, linked, empty})
	if err != nil {
		t.Fatal(err)
	}
	if result[one] != "pending_confirm" || result[linked] != "skipped_already_bound" || result[empty] != "skipped_no_unambiguous_identity" {
		t.Fatal(result)
	}
	result, err = s.BackfillScreenshotLeads(ctx, 1, 5, []int64{one})
	if err != nil || result[one] != "skipped_already_bound" {
		t.Fatal("repeat batch", result, err)
	}
	if n := contactTestCount(t, s, ctx, `SELECT count(*) FROM screenshot_customer_reviews WHERE action='backfill'`); n != 1 {
		t.Fatal("backfill duplicated audit")
	}
	foreign := contactTestSource(t, s, ctx, 2, nil, `{"platform":"微信","nickname":"其他租户"}`)
	if _, err := s.BackfillScreenshotLeads(ctx, 1, 5, []int64{foreign}); err == nil {
		t.Fatal("cross tenant backfill accepted")
	}
}

func TestScreenshotBoundExistingConfirmationScopeIntegration(t *testing.T) {
	s, ctx := contactTestService(t)
	existing := contactTestCustomer(t, s, ctx, 1, "正式客户")
	for _, payload := range []string{`{"platform":"微信","account":"existing"}`} {
		src := contactTestSource(t, s, ctx, 1, nil, payload)
		if err := s.ConfirmScreenshot(ctx, 1, 1, src, existing); err != nil {
			t.Fatal(err)
		}
	}
	one := contactTestSource(t, s, ctx, 1, nil, `{"platform":"微信","account":"existing"}`)
	two := contactTestSource(t, s, ctx, 1, nil, `{"platform":"微信","account":"existing"}`)
	for _, src := range []int64{one, two} {
		id, _, err := s.completeScreenshotRecognition(ctx, 1, src)
		if err != nil || *id != existing {
			t.Fatal(err)
		}
	}
	if _, err := s.ReviewScreenshot(ctx, 1, 5, one, ScreenshotReviewRequest{Action: "confirm"}); err != nil {
		t.Fatal(err)
	}
	if n := contactTestCount(t, s, ctx, `SELECT count(*) FROM patient_conversation_sources WHERE id=$1 AND match_status='pending_confirm'`, two); n != 1 {
		t.Fatal("confirming existing customer confirmed unrelated source")
	}
}

// Exercises the real analyze entry point, provider response parsing, final
// transaction and duplicate-upload path. The model endpoint is local and fake.
func TestScreenshotRecognitionAutomaticallyCreatesLeadIntegration(t *testing.T) {
	s, ctx := contactTestService(t)
	if _, err := s.pool.Exec(ctx, `CREATE TABLE recording_analysis_prompts(id bigserial,code text,is_active boolean,system_prompt text,user_prompt_template text,output_schema jsonb); INSERT INTO recording_analysis_prompts(code,is_active,system_prompt,user_prompt_template) VALUES('patient_screenshot_analysis_v1',true,'Only facts','{{customer_context}}'); CREATE TABLE llm_model_configs(id bigserial,tenant_id bigint,function_type text,provider text,model_code text,model_params json,extra_params json,is_active boolean,is_default boolean,updated_at timestamptz,deleted_at timestamptz); INSERT INTO llm_model_configs(tenant_id,function_type,provider,model_code,is_active,is_default) VALUES(1,'patient_screenshot_analysis','test','test-model',true,true)`); err != nil {
		t.Fatal(err)
	}
	var calls atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		if r.URL.Path != "/v1/inference/image" {
			t.Error("unexpected provider path")
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{"content": `{"platform":"微信","nickname":"真实识别昵称","account":""}`})
	}))
	defer server.Close()
	s.llm = llmgateway.NewClient(server.URL, "local-fixture")
	image := "data:image/png;base64,local-test-fixture"
	req := ScreenshotAnalyzeRequest{TenantID: 1, EmployeeID: 1, ImageData: image}
	result, err := s.AnalyzeScreenshot(ctx, req)
	if err != nil {
		t.Fatal(err)
	}
	if result.CustomerID == nil || result.MatchStatus != "pending_confirm" || result.AnalysisStatus != "succeeded" {
		t.Fatalf("successful recognition did not create lead: %+v", result)
	}
	again, err := s.AnalyzeScreenshot(ctx, req)
	if err != nil {
		t.Fatal(err)
	}
	if !again.Reused || *again.CustomerID != *result.CustomerID || again.MatchStatus != "pending_confirm" || calls.Load() != 1 {
		t.Fatalf("duplicate reran model or confirmed lead: %+v", again)
	}
	if _, err := s.ReviewScreenshot(ctx, 1, 1, result.SourceID, ScreenshotReviewRequest{Action: "reject"}); err != nil {
		t.Fatal(err)
	}
	again, err = s.AnalyzeScreenshot(ctx, req)
	if err != nil || again.MatchStatus != "rejected" || calls.Load() != 1 {
		t.Fatal("rejected source revived", err)
	}
	if n := contactTestCount(t, s, ctx, `SELECT count(*) FROM customers`); n != 1 {
		t.Fatal("duplicate customer created")
	}
}

func TestScreenshotRebindAndAmbiguousAccountIntegration(t *testing.T) {
	s, ctx := contactTestService(t)
	first := contactTestCustomer(t, s, ctx, 1, "同账号客户一")
	second := contactTestCustomer(t, s, ctx, 1, "同账号客户二")
	for _, id := range []int64{first, second} {
		if _, err := s.pool.Exec(ctx, `INSERT INTO customer_identities(customer_id,channel_type,external_id) VALUES($1,'微信','ambiguous')`, id); err != nil {
			t.Fatal(err)
		}
	}
	ambiguous := contactTestSource(t, s, ctx, 1, nil, `{"platform":"微信","account":"ambiguous"}`)
	id, status, err := s.completeScreenshotRecognition(ctx, 1, ambiguous)
	if err != nil || id != nil || status != "pending" {
		t.Fatal("ambiguous account assigned automatically", err)
	}
	source := contactTestSource(t, s, ctx, 1, nil, `{"platform":"微信","nickname":"新线索"}`)
	lead, _, err := s.completeScreenshotRecognition(ctx, 1, source)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := s.ReviewScreenshot(ctx, 1, 2, source, ScreenshotReviewRequest{Action: "rebind", CustomerID: first}); err != nil {
		t.Fatal(err)
	}
	if n := contactTestCount(t, s, ctx, `SELECT count(*) FROM patient_conversation_sources WHERE id=$1 AND customer_id=$2 AND match_status='confirmed'`, source, first); n != 1 {
		t.Fatal("manual rebind missing")
	}
	if n := contactTestCount(t, s, ctx, `SELECT count(*) FROM customers WHERE id=$1 AND deleted_at IS NOT NULL`, *lead); n != 1 {
		t.Fatal("orphan lead remains in customer list")
	}
	// Rejecting the final source retires only the auto lead, preserving evidence.
	last := contactTestSource(t, s, ctx, 1, nil, `{"platform":"抖音","nickname":"无关昵称"}`)
	lastLead, _, err := s.completeScreenshotRecognition(ctx, 1, last)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := s.ReviewScreenshot(ctx, 1, 2, last, ScreenshotReviewRequest{Action: "reject"}); err != nil {
		t.Fatal(err)
	}
	if n := contactTestCount(t, s, ctx, `SELECT count(*) FROM customers WHERE id=$1 AND deleted_at IS NOT NULL`, *lastLead); n != 1 {
		t.Fatal("rejected orphan live")
	}
	if n := contactTestCount(t, s, ctx, `SELECT count(*) FROM customer_identities WHERE customer_id=$1`, *lastLead); n != 1 {
		t.Fatal("rejected evidence removed")
	}
}

func TestScreenshotPlaceholderIdentityIntegration(t *testing.T) {
	s, ctx := contactTestService(t)
	var previous int64
	for _, account := range []string{"未识别", "未知", "无", "暂无", "不适用", "null", "undefined", " NULL "} {
		raw, _ := json.Marshal(map[string]string{"platform": "未知", "account": account, "nickname": " 张女士 "})
		source := contactTestSource(t, s, ctx, 1, nil, string(raw))
		id, status, err := s.completeScreenshotRecognition(ctx, 1, source)
		if err != nil || id == nil || *id == previous || status != "pending_confirm" {
			t.Fatalf("placeholder reused: %v %v", id, err)
		}
		if n := contactTestCount(t, s, ctx, `SELECT count(*) FROM customer_identities WHERE customer_id=$1 AND coalesce(channel_id,'')='' AND coalesce(external_id,'')='' AND coalesce(channel,'')='' AND nickname='张女士'`, *id); n != 1 {
			t.Fatal("placeholder identity persisted")
		}
		previous = *id
		if _, _, err := s.completeScreenshotRecognition(ctx, 1, source); err != nil {
			t.Fatal(err)
		}
		if n := contactTestCount(t, s, ctx, `SELECT count(*) FROM customer_identities WHERE customer_id=$1`, *id); n != 1 {
			t.Fatal("unknown-platform retry duplicated contact")
		}
	}
	for _, nick := range []string{"未识别", "未知", "无", "暂无", "不适用", "null", "undefined"} {
		raw, _ := json.Marshal(map[string]string{"platform": "未知", "account": "未识别", "nickname": nick})
		source := contactTestSource(t, s, ctx, 1, nil, string(raw))
		id, _, err := s.completeScreenshotRecognition(ctx, 1, source)
		if err != nil || id != nil {
			t.Fatal("placeholder-only lead created", err)
		}
	}
	source := contactTestSource(t, s, ctx, 1, nil, `{"platform":"","account":"未知","nickname":"李女士"}`)
	id, _, err := s.completeScreenshotRecognition(ctx, 1, source)
	if err != nil || id == nil || *id == previous {
		t.Fatal("different nickname merged", err)
	}
	var a ScreenshotAnalysis
	_ = json.Unmarshal([]byte(`{"account":"未识别","nickname":" undefined ","platform":"暂无"}`), &a)
	a.Normalize()
	b, _ := json.Marshal(a)
	var obj map[string]any
	_ = json.Unmarshal(b, &obj)
	if _, ok := obj["account"]; ok || a.Nickname != "" || a.Platform != "" {
		t.Fatal("unclean API JSON", string(b))
	}
}

func TestScreenshotReviewedPendingBackfillIntegration(t *testing.T) {
	s, ctx := contactTestService(t)
	one := contactTestSource(t, s, ctx, 1, nil, `{"account":"未识别","nickname":"张女士","patient_utterances":["想了解项目"]}`)
	bad := contactTestSource(t, s, ctx, 1, nil, `{"account":"only-account"}`)
	if _, err := s.pool.Exec(ctx, `UPDATE patient_conversation_sources SET analysis_status='pending'`); err != nil {
		t.Fatal(err)
	}
	if _, err := s.BackfillScreenshotLeads(ctx, 1, 5, []int64{one}); err == nil {
		t.Fatal("implicit pending approval")
	}
	if _, err := s.BackfillReviewedScreenshotLeads(ctx, 1, 5, []int64{one}, []int64{bad}); err == nil {
		t.Fatal("review IDs outside batch")
	}
	if _, err := s.BackfillReviewedScreenshotLeads(ctx, 1, 5, []int64{one, bad}, []int64{one, bad}); err == nil {
		t.Fatal("incomplete payload approved")
	}
	if n := contactTestCount(t, s, ctx, `SELECT count(*) FROM customers`); n != 0 {
		t.Fatal("partial batch committed")
	}
	if n := contactTestCount(t, s, ctx, `SELECT count(*) FROM patient_conversation_sources WHERE id=$1 AND analysis_status='pending'`, one); n != 1 {
		t.Fatal("partial status repair")
	}
	result, err := s.BackfillReviewedScreenshotLeads(ctx, 1, 5, []int64{one}, []int64{one})
	if err != nil || result[one] != "pending_confirm" {
		t.Fatal(result, err)
	}
	if n := contactTestCount(t, s, ctx, `SELECT count(*) FROM screenshot_customer_reviews WHERE source_id=$1 AND detail->>'previous_analysis_status'='pending' AND detail->>'pending_payload_human_reviewed'='true' AND detail->'original_recognition_payload'->>'account'='未识别'`, one); n != 1 {
		t.Fatal("pending review unaudited")
	}
	if n := contactTestCount(t, s, ctx, `SELECT count(*) FROM patient_conversation_sources WHERE id=$1 AND analysis_status='succeeded' AND match_status='pending_confirm'`, one); n != 1 {
		t.Fatal("pending remains stuck")
	}
	result, err = s.BackfillReviewedScreenshotLeads(ctx, 1, 5, []int64{one}, []int64{one})
	if err != nil || result[one] != "skipped_already_bound" {
		t.Fatal("retry unsafe", err)
	}
	if _, err := s.pool.Exec(ctx, `UPDATE patient_conversation_sources SET analysis_status='processing',recognition_payload='{"nickname":"李女士","conversation_summary":"咨询"}' WHERE id=$1`, bad); err != nil {
		t.Fatal(err)
	}
	if _, err := s.BackfillReviewedScreenshotLeads(ctx, 1, 5, []int64{bad}, []int64{bad}); err == nil {
		t.Fatal("processing auto repaired")
	}
}
