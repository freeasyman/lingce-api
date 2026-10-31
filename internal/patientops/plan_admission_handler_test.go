package patientops

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strconv"
	"testing"

	"github.com/freeasyman/lingce-api/internal/middleware"
	"github.com/freeasyman/lingce-api/pkg/auth"
)

func TestPlanUnconfirmedHandlerResponseIntegration(t *testing.T) {
	s, ctx := contactTestService(t)
	if _, err := s.pool.Exec(ctx, `ALTER TABLE patient_ops_plans ADD name text DEFAULT '测试计划',ADD plan_type text DEFAULT 'status',ADD scenario text DEFAULT 'new_consultation',ADD period_start date,ADD period_end date,ADD target_count int,ADD target_amount numeric,ADD product_ids jsonb DEFAULT '[]',ADD filter_config jsonb DEFAULT '{}',ADD created_by bigint DEFAULT 1,ADD published_at timestamptz,ADD created_at timestamptz DEFAULT now(),ADD updated_at timestamptz DEFAULT now(); INSERT INTO patient_ops_plans(id,tenant_id,status) VALUES(1,1,'draft'),(2,1,'published')`); err != nil {
		t.Fatal(err)
	}
	source := contactTestSource(t, s, ctx, 1, nil, `{"platform":"微信","nickname":"待确认客户"}`)
	id, _, err := s.completeScreenshotRecognition(ctx, 1, source)
	if err != nil {
		t.Fatal(err)
	}
	tenant := int64(1)
	token, _, err := auth.GenerateToken("test-secret", 1, auth.UserTypeEmployee, &tenant, 1, 1)
	if err != nil {
		t.Fatal(err)
	}
	h := &Handler{service: s}
	for _, test := range []struct {
		plan    int
		handler http.HandlerFunc
	}{{1, h.PublishPlan}, {2, h.AddPlanMembers}} {
		body, _ := json.Marshal(map[string]any{"customer_ids": []int64{*id}})
		req := httptest.NewRequest(http.MethodPost, "/", bytes.NewReader(body))
		req.SetPathValue("id", strconv.Itoa(test.plan))
		req.Header.Set("Authorization", "Bearer "+token)
		w := httptest.NewRecorder()
		middleware.Auth("test-secret")(test.handler).ServeHTTP(w, req)
		if w.Code != 422 {
			t.Fatalf("status %d: %s", w.Code, w.Body.String())
		}
		var response struct {
			Code    string                        `json:"code"`
			Details UnconfirmedPlanCustomersError `json:"details"`
		}
		if err := json.Unmarshal(w.Body.Bytes(), &response); err != nil {
			t.Fatal(err)
		}
		if response.Code != "UNCONFIRMED_CUSTOMERS" || len(response.Details.Customers) != 1 || response.Details.Customers[0].CustomerID != *id {
			t.Fatal(w.Body.String())
		}
	}
}
