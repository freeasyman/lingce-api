package patientops

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"strconv"
	"testing"

	"github.com/freeasyman/lingce-api/internal/middleware"
	"github.com/freeasyman/lingce-api/internal/product"
	"github.com/freeasyman/lingce-api/pkg/auth"
)

func planAgeTestService(t *testing.T) (*Service, context.Context) {
	t.Helper()
	s, ctx := contactTestService(t)
	setup := `CREATE TABLE tenants(id bigint PRIMARY KEY, name text); INSERT INTO tenants VALUES(1,'测试机构'),(2,'其他机构');
 ALTER TABLE products ADD tenant_id bigint,ADD industry text DEFAULT '',ADD aliases jsonb DEFAULT '[]',ADD price numeric DEFAULT 100,ADD applicable text DEFAULT '',ADD selling_point text DEFAULT '',ADD upgrade_to text DEFAULT '',ADD combine_with text DEFAULT '',ADD status text DEFAULT 'active',ADD is_template bool DEFAULT false,ADD applicable_keywords jsonb DEFAULT '[]',ADD age_min int,ADD age_max int,ADD gender text DEFAULT 'all',ADD contraindications text DEFAULT '',ADD review_status text DEFAULT 'approved',ADD version int DEFAULT 1,ADD created_at timestamptz DEFAULT now(),ADD updated_at timestamptz DEFAULT now(),ADD deleted_at timestamptz;
 ALTER TABLE patient_ops_plans ADD name text DEFAULT '测试计划',ADD plan_type text DEFAULT 'sales',ADD scenario text DEFAULT '',ADD period_start date,ADD period_end date,ADD target_count int,ADD target_amount numeric DEFAULT 1000,ADD product_ids jsonb DEFAULT '[36]',ADD filter_config jsonb DEFAULT '{}',ADD created_by bigint DEFAULT 1,ADD published_at timestamptz,ADD created_at timestamptz DEFAULT now(),ADD updated_at timestamptz DEFAULT now();
 ALTER TABLE patient_ops_plan_members ADD updated_at timestamptz DEFAULT now(),ADD UNIQUE(plan_id,customer_id);
 ALTER TABLE patient_operation_tasks ADD dedupe_key text DEFAULT '',ADD created_by bigint,ADD updated_at timestamptz DEFAULT now();
 CREATE UNIQUE INDEX ON patient_operation_tasks(tenant_id,dedupe_key) WHERE dedupe_key<>'';
 INSERT INTO products(id,tenant_id,name,applicable_keywords,age_min,age_max) VALUES(36,1,'儿童体态','["体态"]',3,12);
 INSERT INTO products(id,tenant_id,name,applicable_keywords) VALUES(37,1,'通用评估','["评估"]');
 INSERT INTO patient_ops_plans(id,tenant_id,status) VALUES(1,1,'draft'),(2,1,'published');`
	if _, err := s.pool.Exec(ctx, setup); err != nil {
		t.Fatal(err)
	}
	migration, err := os.ReadFile("../store/migrations/20261031_075_plan_age_admission.sql")
	if err != nil {
		t.Fatal(err)
	}
	if _, err = s.pool.Exec(ctx, string(migration)); err != nil {
		t.Fatal(err)
	}
	// LLM enrichment is intentionally disabled; this test exercises SQL admission and task transactions.
	s.llm = nil
	return s, ctx
}

func planAgeCustomer(t *testing.T, s *Service, ctx context.Context, tenant int64, name string, age int) int64 {
	t.Helper()
	id := contactTestCustomer(t, s, ctx, tenant, name)
	if _, err := s.pool.Exec(ctx, `UPDATE customers SET phone='13800000001',age=$2 WHERE id=$1`, id, age); err != nil {
		t.Fatal(err)
	}
	contactTestSource(t, s, ctx, tenant, &id, `{"platform":"微信","product_interest":["体态评估"],"concern":"想了解"}`)
	return id
}

func TestPlanAgePreviewAndPublishingIntegration(t *testing.T) {
	s, ctx := planAgeTestService(t)
	known := planAgeCustomer(t, s, ctx, 1, "已知年龄", 7)
	unknown := planAgeCustomer(t, s, ctx, 1, "未知年龄", 0)
	outside := planAgeCustomer(t, s, ctx, 1, "超龄", 30)
	pending := planAgeCustomer(t, s, ctx, 1, "身份待确认", 0)
	foreign := planAgeCustomer(t, s, ctx, 2, "其他租户", 7)
	if _, err := s.pool.Exec(ctx, `UPDATE customers SET extra_data='{"identity_status":"pending_confirm"}' WHERE id=$1`, pending); err != nil {
		t.Fatal(err)
	}
	preview, err := s.PreviewPlan(ctx, 1, 1)
	if err != nil {
		t.Fatal(err)
	}
	if preview.EligibleCount != 1 || preview.PendingCount != 1 || preview.ExcludedCount != 2 {
		t.Fatalf("bad preview %+v", preview)
	}
	for _, c := range preview.Candidates {
		if c.CustomerID == outside || c.CustomerID == pending || c.CustomerID == foreign {
			t.Fatal("excluded customer in candidates")
		}
		if c.CustomerID == unknown && c.AdmissionStatus != "needs_age" {
			t.Fatalf("must require actual age: %+v", c)
		}
	}
	found := false
	for _, c := range preview.Excluded {
		if c.CustomerID == pending {
			found = true
		}
	}
	if !found {
		t.Fatal("unconfirmed identity must have a visible exclusion reason")
	}
	review := PlanSuitabilityConfirmation{CustomerID: unknown, ProductID: 36, ProductVersion: 1, Reason: "人工已核对适用条件"}
	for _, publish := range []bool{true, false} {
		if publish {
			_, err = s.PublishPlan(ctx, 1, 1, 1, PublishPlanRequest{CustomerIDs: []int64{known, unknown}, SuitabilityConfirmations: []PlanSuitabilityConfirmation{review}})
		} else {
			_, err = s.AddPlanMembers(ctx, 1, 1, 2, AddPlanMembersRequest{CustomerIDs: []int64{known, unknown}, SuitabilityConfirmations: []PlanSuitabilityConfirmation{review}})
		}
		var rejected *PlanAdmissionError
		if !errors.As(err, &rejected) {
			t.Fatalf("required age bypassed: %v", err)
		}
	}
	if n := contactTestCount(t, s, ctx, `SELECT count(*) FROM patient_operation_tasks`); n != 0 {
		t.Fatal("partial tasks")
	}
	if n := contactTestCount(t, s, ctx, `SELECT count(*) FROM patient_ops_plan_members`); n != 0 {
		t.Fatal("partial members")
	}
	// Product policy is explicitly changed through the real product service.
	optional := false
	ps := product.NewService(product.NewStore(s.pool))
	changed, err := ps.Update(ctx, 36, product.UpdateRequest{AgeRequired: &optional})
	if err != nil {
		t.Fatal(err)
	}
	if changed.AgeRequired || changed.Version != 2 {
		t.Fatalf("product policy not saved: %+v", changed)
	}
	list, _, err := ps.List(ctx, product.ListRequest{TenantID: 1})
	if err != nil || len(list) != 2 {
		t.Fatalf("product list: %v %+v", err, list)
	}
	preview, err = s.PreviewPlan(ctx, 1, 1)
	if err != nil {
		t.Fatal(err)
	}
	for _, c := range preview.Candidates {
		if c.CustomerID == unknown && c.AdmissionStatus != "needs_confirmation" {
			t.Fatalf("expected manual confirmation: %+v", c)
		}
	}
	_, err = s.PublishPlan(ctx, 1, 1, 1, PublishPlanRequest{CustomerIDs: []int64{known, unknown}, SuitabilityConfirmations: []PlanSuitabilityConfirmation{review}})
	var rejected *PlanAdmissionError
	if !errors.As(err, &rejected) {
		t.Fatalf("stale product confirmation accepted: %v", err)
	}
	review.ProductVersion = 2
	detail, err := s.PublishPlan(ctx, 1, 1, 1, PublishPlanRequest{CustomerIDs: []int64{known, unknown}, SuitabilityConfirmations: []PlanSuitabilityConfirmation{review}})
	if err != nil {
		t.Fatal(err)
	}
	if len(detail.Members) != 2 || detail.Stats.TaskTotal != 2 {
		t.Fatalf("bad publication %+v", detail)
	}
	if n := contactTestCount(t, s, ctx, `SELECT count(*) FROM patient_ops_product_suitability_reviews WHERE tenant_id=1 AND plan_id=1 AND customer_id=$1 AND product_id=36 AND product_version=2 AND operator_id=1 AND customer_age=0`, unknown); n != 1 {
		t.Fatal("missing audit")
	}
	if n := contactTestCount(t, s, ctx, `SELECT count(*) FROM patient_operation_tasks WHERE customer_id=$1 AND risk_medical LIKE '%年龄未知%' AND content LIKE '%实际使用者的年龄%'`, unknown); n != 1 {
		t.Fatal("age caveat missing in published task")
	}
	// Append follows the same confirmation and audit policy.
	another := planAgeCustomer(t, s, ctx, 1, "追加未知年龄", 0)
	_, err = s.AddPlanMembers(ctx, 1, 1, 1, AddPlanMembersRequest{CustomerIDs: []int64{another}})
	if !errors.As(err, &rejected) {
		t.Fatalf("append bypass: %v", err)
	}
	review.CustomerID = another
	detail, err = s.AddPlanMembers(ctx, 1, 1, 1, AddPlanMembersRequest{CustomerIDs: []int64{another}, SuitabilityConfirmations: []PlanSuitabilityConfirmation{review}})
	if err != nil {
		t.Fatal(err)
	}
	if len(detail.Members) != 3 {
		t.Fatal("append failed")
	}
}

func TestPlanAgeMigrationDefaultsAndAgeCompletionIntegration(t *testing.T) {
	s, ctx := planAgeTestService(t)
	if n := contactTestCount(t, s, ctx, `SELECT count(*) FROM products WHERE (id=36 AND age_required=true) OR (id=37 AND age_required=false)`); n != 2 {
		t.Fatal("migration changed original safety policy")
	}
	id := planAgeCustomer(t, s, ctx, 1, "补全年龄", 0)
	if _, err := s.pool.Exec(ctx, `UPDATE customers SET age=7 WHERE id=$1`, id); err != nil {
		t.Fatal(err)
	}
	detail, err := s.PublishPlan(ctx, 1, 1, 1, PublishPlanRequest{CustomerIDs: []int64{id}})
	if err != nil {
		t.Fatal(err)
	}
	if len(detail.Members) != 1 {
		t.Fatal("completed age not accepted")
	}
	if n := contactTestCount(t, s, ctx, `SELECT count(*) FROM patient_ops_product_suitability_reviews`); n != 0 {
		t.Fatal("known age must not require manual override")
	}
}

func TestPlanAdmissionStaleSnapshotIntegration(t *testing.T) {
	s, ctx := planAgeTestService(t)
	id := planAgeCustomer(t, s, ctx, 1, "变更客户", 7)
	plan, err := s.loadPlan(ctx, 1, 1)
	if err != nil {
		t.Fatal(err)
	}
	preview, err := s.buildPlanCandidates(ctx, plan)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := s.pool.Exec(ctx, `UPDATE customers SET age=30 WHERE id=$1`, id); err != nil {
		t.Fatal(err)
	}
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer tx.Rollback(ctx)
	if err := validatePlanAdmissionSnapshot(ctx, tx, plan, preview.Candidates); err == nil {
		t.Fatal("stale age accepted")
	}
	tx.Rollback(ctx)
	if _, err := s.pool.Exec(ctx, `UPDATE customers SET age=7 WHERE id=$1;`, id); err != nil {
		t.Fatal(err)
	}
	if _, err := s.pool.Exec(ctx, `UPDATE products SET version=version+1 WHERE id=36`); err != nil {
		t.Fatal(err)
	}
	tx, err = s.pool.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer tx.Rollback(ctx)
	if err := validatePlanAdmissionSnapshot(ctx, tx, plan, preview.Candidates); err == nil {
		t.Fatal("stale product accepted")
	}
}

func TestPlanAgeHandlerRejectsWholeBatchIntegration(t *testing.T) {
	s, ctx := planAgeTestService(t)
	known := planAgeCustomer(t, s, ctx, 1, "可入选", 7)
	unknown := planAgeCustomer(t, s, ctx, 1, "须补全年龄", 0)
	tenant := int64(1)
	token, _, err := auth.GenerateToken("test-secret", 1, auth.UserTypeEmployee, &tenant, 1, 1)
	if err != nil {
		t.Fatal(err)
	}
	h := &Handler{service: s}
	for _, tc := range []struct {
		plan    int
		handler http.HandlerFunc
	}{{1, h.PublishPlan}, {2, h.AddPlanMembers}} {
		body, _ := json.Marshal(map[string]any{"customer_ids": []int64{known, unknown}})
		req := httptest.NewRequest(http.MethodPost, "/", bytes.NewReader(body))
		req.SetPathValue("id", strconv.Itoa(tc.plan))
		req.Header.Set("Authorization", "Bearer "+token)
		w := httptest.NewRecorder()
		middleware.Auth("test-secret")(tc.handler).ServeHTTP(w, req)
		var response struct {
			Code    string             `json:"code"`
			Details PlanAdmissionError `json:"details"`
		}
		if err := json.Unmarshal(w.Body.Bytes(), &response); err != nil {
			t.Fatal(err)
		}
		if w.Code != 422 || response.Code != "PLAN_ADMISSION_REQUIRED" || len(response.Details.Customers) != 1 || response.Details.Customers[0].CustomerID != unknown {
			t.Fatalf("unexpected response %d %s", w.Code, w.Body.String())
		}
	}
	if n := contactTestCount(t, s, ctx, `SELECT count(*) FROM patient_operation_tasks`); n != 0 {
		t.Fatal("handler created partial tasks")
	}
	if n := contactTestCount(t, s, ctx, `SELECT count(*) FROM patient_ops_plan_members`); n != 0 {
		t.Fatal("handler created partial members")
	}
}

func TestPlanUnknownAgeGenericProductAndDefaultPublishIntegration(t *testing.T) {
	s, ctx := planAgeTestService(t)
	id := planAgeCustomer(t, s, ctx, 1, "未知年龄通用产品客户", 0)
	if _, err := s.pool.Exec(ctx, `UPDATE patient_ops_plans SET product_ids='[36,37]' WHERE id=1`); err != nil {
		t.Fatal(err)
	}
	preview, err := s.PreviewPlan(ctx, 1, 1)
	if err != nil {
		t.Fatal(err)
	}
	if preview.EligibleCount != 1 || preview.PendingCount != 0 || len(preview.Candidates) != 1 || preview.Candidates[0].RecommendedProductID == nil || *preview.Candidates[0].RecommendedProductID != 37 {
		t.Fatalf("generic relevant product must be usable: %+v", preview)
	}
	detail, err := s.PublishPlan(ctx, 1, 1, 1, PublishPlanRequest{})
	if err != nil {
		t.Fatal(err)
	}
	if len(detail.Members) != 1 || detail.Members[0].CustomerID != id {
		t.Fatal("default publish failed")
	}
}
