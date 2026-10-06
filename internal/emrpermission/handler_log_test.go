package emrpermission

import (
	"context"
	"errors"
	"net/http/httptest"
	"testing"

	"github.com/freeasyman/lingce-api/internal/middleware"
	"github.com/freeasyman/lingce-api/internal/support"
	"net/http"
)

type capturedLogs struct {
	requests []support.SystemActionLogWriteRequest
	err      error
}

func (c *capturedLogs) CreateSystemActionLog(_ context.Context, req support.SystemActionLogWriteRequest, _, _, _ string) (*support.SystemActionLogResponse, error) {
	c.requests = append(c.requests, req)
	return nil, c.err
}

func TestPermissionLogsPreserveActualFailureAndSnapshots(t *testing.T) {
	logs := &capturedLogs{}
	h := NewHandler(nil, logs)
	before := &AssignmentResponse{Enabled: true, EmrRoleCode: "doctor", Scope: "self", Abilities: []string{"record.read"}}
	after := &AssignmentResponse{Enabled: true, EmrRoleCode: "doctor", Scope: "tenant", Abilities: []string{"record.read", "record.edit"}}
	req := httptest.NewRequest(http.MethodPut, "/api/v1/emr/permissions/22", nil)
	req.Header.Set("X-Request-ID", "permission-test")
	middleware.RequestID(http.HandlerFunc(func(_ http.ResponseWriter, r *http.Request) {
		if err := h.recordLog(r, 1, 10, 22, "success", nil, before, after, after); err != nil {
			t.Fatal(err)
		}
		if err := h.recordLog(r, 1, 10, 22, "failure", errors.New("SQLSTATE 42703: missing column"), before, before, after); err != nil {
			t.Fatal(err)
		}
	})).ServeHTTP(httptest.NewRecorder(), req)
	if len(logs.requests) != 2 {
		t.Fatal("expected success and failure logs")
	}
	log := logs.requests[0]
	if log.ActionCode != "emr_permission_update" || log.ActionName != "修改电子病历权限" || log.Result != "success" {
		t.Fatalf("incorrect action: %#v", log)
	}
	if *log.TenantID != 1 || *log.ActorID != 10 || log.ObjectID != int64(22) || *log.RequestID != "permission-test" {
		t.Fatalf("missing identity/request fields: %#v", log)
	}
	if log.BeforeSummary["scope"] != "self" || log.AfterSummary["scope"] != "tenant" {
		t.Fatalf("missing snapshots: %#v", log)
	}
	failure := logs.requests[1]
	if failure.Result != "failure" || *failure.ErrorMessage != "SQLSTATE 42703: missing column" {
		t.Fatal("technical failure must remain in log")
	}
	if failure.AfterSummary["scope"] != "self" {
		t.Fatal("failed write must not claim attempted values were saved")
	}
}

func TestPermissionLoadFailureIsNotAnUpdate(t *testing.T) {
	logs := &capturedLogs{}
	h := NewHandler(nil, logs)
	if err := h.recordLog(httptest.NewRequest(http.MethodGet, "/api/v1/emr/permissions", nil), 1, 10, 0, "failure", errors.New("database unavailable"), nil, nil, nil); err != nil {
		t.Fatal(err)
	}
	if logs.requests[0].ActionCode != "emr_permission_load" || logs.requests[0].Result != "failure" {
		t.Fatal("incorrect loading action/result")
	}
	logs.err = errors.New("log persistence unavailable")
	if h.recordLog(httptest.NewRequest(http.MethodPut, "/api/v1/emr/permissions/22", nil), 1, 10, 22, "success", nil, nil, nil, nil) == nil {
		t.Fatal("logging failure cannot be silently swallowed")
	}
}

func TestPermissionErrorMessagesHideDatabaseDetails(t *testing.T) {
	if got := mapPermissionError(errors.New("abilities is required when emr is enabled")); got != "请勾选“查看病案”，再选择该员工需要的其他能力。" {
		t.Fatal(got)
	}
	if got := mapPermissionError(errors.New("failed to upsert: SQLSTATE 23505")); got != "权限保存失败，请重试。" {
		t.Fatal(got)
	}
}
