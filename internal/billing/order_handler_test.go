package billing

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/freeasyman/lingce-api/pkg/auth"
)

func newOrderTestMux(repo Repository) http.Handler {
	module := &Module{handler: NewHandler(NewService(repo))}
	mux := http.NewServeMux()
	module.RegisterRoutes(mux, testJWTSecret, "")
	return mux
}

func decodeErrorCode(t *testing.T, response *httptest.ResponseRecorder) string {
	t.Helper()
	var payload struct {
		Code    string `json:"code"`
		Message string `json:"message"`
	}
	if err := json.Unmarshal(response.Body.Bytes(), &payload); err != nil {
		t.Fatalf("decode error response: %v (%s)", err, response.Body.String())
	}
	return payload.Code
}

func TestOpsOrderRoutesRequireAdmin(t *testing.T) {
	repo := &fakeRepository{}
	mux := newOrderTestMux(repo)
	tenant7 := int64(7)
	employeeToken := mustToken(t, 11, auth.UserTypeEmployee, &tenant7)

	routes := []struct {
		method string
		target string
		body   string
	}{
		{http.MethodPost, "/api/v1/ops/orders", `{}`},
		{http.MethodGet, "/api/v1/ops/orders", ""},
		{http.MethodPost, "/api/v1/ops/orders/gift/preview", `{}`},
		{http.MethodGet, "/api/v1/ops/orders/1", ""},
		{http.MethodPost, "/api/v1/ops/orders/1/settle", `{"paid_amount":0}`},
		{http.MethodPost, "/api/v1/ops/orders/1/activate", ""},
		{http.MethodPost, "/api/v1/ops/orders/1/refund", ""},
	}
	for _, route := range routes {
		if response := doRequest(t, mux, route.method, route.target, employeeToken, route.body); response.Code != http.StatusForbidden {
			t.Fatalf("%s %s should be 403 for employee, got %d body=%s", route.method, route.target, response.Code, response.Body.String())
		}
		if response := doRequest(t, mux, route.method, route.target, "", route.body); response.Code != http.StatusUnauthorized {
			t.Fatalf("%s %s should be 401 without token, got %d", route.method, route.target, response.Code)
		}
	}
	if repo.insertOrderCalls != 0 || repo.settleCalls != 0 || repo.activateCalls != 0 || repo.refundCalls != 0 {
		t.Fatalf("forbidden/unauthorized requests must not reach store: %+v", repo)
	}
}

func TestCreateOrderHandler(t *testing.T) {
	repo := &fakeRepository{catalogItems: orderCatalogFixture()}
	mux := newOrderTestMux(repo)
	adminToken := mustToken(t, 1, auth.UserTypeAdmin, nil)

	body := `{"tenant_id":9,"order_type":"single",
		"items":[{"product_code":"tingxie","edition":"standard","quota_type":"official",
		          "quotas":{"record":2500,"round":5000,"hour":400},"price":21600}],
		"source":"direct","salesman_id":12,"price_list_version":"2026"}`
	response := doRequest(t, mux, http.MethodPost, "/api/v1/ops/orders", adminToken, body)
	if response.Code != http.StatusCreated {
		t.Fatalf("status = %d, want 201, body=%s", response.Code, response.Body.String())
	}
	var order Order
	if err := json.Unmarshal(response.Body.Bytes(), &order); err != nil {
		t.Fatalf("decode order: %v (%s)", err, response.Body.String())
	}
	if !strings.HasPrefix(order.OrderNo, "BL") || len(order.OrderNo) != 19 {
		t.Fatalf("unexpected order_no: %q", order.OrderNo)
	}
	if order.Status != OrderStatusCreated || order.TenantID != 9 {
		t.Fatalf("unexpected order: %+v", order)
	}
	if order.TotalAmount != Decimal("21600.00") || order.PayableAmount != Decimal("21600.00") {
		t.Fatalf("unexpected amounts: total=%s payable=%s", order.TotalAmount, order.PayableAmount)
	}
	if len(order.Items) != 1 || order.Items[0].ProductCode != "tingxie" {
		t.Fatalf("unexpected items: %+v", order.Items)
	}
	if repo.insertOrderInput.SalesmanID == nil || *repo.insertOrderInput.SalesmanID != 12 {
		t.Fatalf("salesman_id not passed: %+v", repo.insertOrderInput.SalesmanID)
	}

	if response := doRequest(t, mux, http.MethodPost, "/api/v1/ops/orders", adminToken, `{"tenant_id":9`); response.Code != http.StatusBadRequest {
		t.Fatalf("malformed body should be 400, got %d", response.Code)
	}
	if response := doRequest(t, mux, http.MethodPost, "/api/v1/ops/orders", adminToken,
		`{"tenant_id":9,"order_type":"single","items":[{"product_code":"tingxie","edition":"standard","quotas":{}}]}`); response.Code != http.StatusBadRequest {
		t.Fatalf("invalid items should be 400, got %d body=%s", response.Code, response.Body.String())
	} else if code := decodeErrorCode(t, response); code != "ORDER_ITEMS_INVALID" {
		t.Fatalf("invalid items code = %q, want ORDER_ITEMS_INVALID", code)
	}
	if response := doRequest(t, mux, http.MethodPost, "/api/v1/ops/orders", adminToken,
		`{"tenant_id":9,"order_type":"gift","items":[{"product_code":"tingxie","edition":"standard","quotas":{"record":500}}]}`); response.Code != http.StatusBadRequest {
		t.Fatalf("gift without reason should be 400, got %d body=%s", response.Code, response.Body.String())
	} else if code := decodeErrorCode(t, response); code != "BAD_REQUEST" {
		t.Fatalf("gift without reason code = %q, want BAD_REQUEST", code)
	}
}

func TestListAndGetOrderHandler(t *testing.T) {
	createdAt := time.Date(2026, 11, 10, 9, 0, 0, 0, time.UTC)
	repo := &fakeRepository{
		orders: map[int64]Order{
			1: {
				ID: 1, OrderNo: "BL1", TenantID: 9, OrderType: OrderTypeSingle,
				Status: OrderStatusSettled, TotalAmount: Decimal("21600.00"), PayableAmount: Decimal("21600.00"),
				CreatedAt: createdAt, UpdatedAt: createdAt,
				Items: []OrderItem{{ProductCode: "tingxie", Edition: "standard", Quotas: Quantities(`{"record":2500}`), Price: Decimal("21600.00")}},
			},
		},
		listOrdersItems: []Order{{ID: 1, OrderNo: "BL1", TenantID: 9, Status: OrderStatusSettled, CreatedAt: createdAt}},
		listOrdersTotal: 1,
	}
	mux := newOrderTestMux(repo)
	adminToken := mustToken(t, 1, auth.UserTypeAdmin, nil)

	response := doRequest(t, mux, http.MethodGet,
		"/api/v1/ops/orders?tenant_id=9&status=settled&order_type=single&page=2&page_size=20", adminToken, "")
	if response.Code != http.StatusOK {
		t.Fatalf("status = %d, body=%s", response.Code, response.Body.String())
	}
	var page OrderPage
	if err := json.Unmarshal(response.Body.Bytes(), &page); err != nil {
		t.Fatalf("decode order page: %v (%s)", err, response.Body.String())
	}
	if page.Total != 1 || page.Page != 2 || page.PageSize != 20 || len(page.Items) != 1 {
		t.Fatalf("unexpected page: %+v", page)
	}
	if repo.listOrdersQuery.TenantID != 9 || repo.listOrdersQuery.Status != OrderStatusSettled || repo.listOrdersQuery.OrderType != OrderTypeSingle {
		t.Fatalf("filters not passed: %+v", repo.listOrdersQuery)
	}

	if response := doRequest(t, mux, http.MethodGet, "/api/v1/ops/orders?tenant_id=abc", adminToken, ""); response.Code != http.StatusBadRequest {
		t.Fatalf("invalid tenant_id should be 400, got %d", response.Code)
	}
	if response := doRequest(t, mux, http.MethodGet, "/api/v1/ops/orders?status=unknown", adminToken, ""); response.Code != http.StatusBadRequest {
		t.Fatalf("invalid status should be 400, got %d body=%s", response.Code, response.Body.String())
	}

	response = doRequest(t, mux, http.MethodGet, "/api/v1/ops/orders/1", adminToken, "")
	if response.Code != http.StatusOK {
		t.Fatalf("detail status = %d, body=%s", response.Code, response.Body.String())
	}
	var detail Order
	if err := json.Unmarshal(response.Body.Bytes(), &detail); err != nil {
		t.Fatalf("decode order detail: %v", err)
	}
	if detail.OrderNo != "BL1" || len(detail.Items) != 1 || detail.Items[0].ProductCode != "tingxie" {
		t.Fatalf("unexpected detail: %+v", detail)
	}

	response = doRequest(t, mux, http.MethodGet, "/api/v1/ops/orders/99", adminToken, "")
	if response.Code != http.StatusNotFound {
		t.Fatalf("missing order should be 404, got %d body=%s", response.Code, response.Body.String())
	}
	if code := decodeErrorCode(t, response); code != "ORDER_NOT_FOUND" {
		t.Fatalf("missing order code = %q, want ORDER_NOT_FOUND", code)
	}
	if response := doRequest(t, mux, http.MethodGet, "/api/v1/ops/orders/abc", adminToken, ""); response.Code != http.StatusBadRequest {
		t.Fatalf("invalid order id should be 400, got %d", response.Code)
	}
}

func TestSettleOrderHandlerErrorCodes(t *testing.T) {
	activatedAt := time.Date(2026, 11, 10, 11, 0, 0, 0, time.UTC)
	repo := &fakeRepository{orders: map[int64]Order{
		1: {ID: 1, TenantID: 9, OrderType: OrderTypeSingle, PayableAmount: Decimal("21600.00"), Status: OrderStatusCreated},
		2: {ID: 2, TenantID: 9, OrderType: OrderTypeSingle, PayableAmount: Decimal("21600.00"), Status: OrderStatusSettled, ActivatedAt: &activatedAt},
	}}
	mux := newOrderTestMux(repo)
	adminToken := mustToken(t, 1, auth.UserTypeAdmin, nil)

	response := doRequest(t, mux, http.MethodPost, "/api/v1/ops/orders/1/settle", adminToken, `{"paid_amount":100}`)
	if response.Code != http.StatusBadRequest {
		t.Fatalf("amount mismatch status = %d, body=%s", response.Code, response.Body.String())
	}
	if code := decodeErrorCode(t, response); code != "ORDER_AMOUNT_MISMATCH" {
		t.Fatalf("amount mismatch code = %q", code)
	}

	if response := doRequest(t, mux, http.MethodPost, "/api/v1/ops/orders/1/settle", adminToken, `{}`); response.Code != http.StatusBadRequest {
		t.Fatalf("missing paid_amount should be 400, got %d", response.Code)
	}
	response = doRequest(t, mux, http.MethodPost, "/api/v1/ops/orders/99/settle", adminToken, `{"paid_amount":0}`)
	if response.Code != http.StatusNotFound {
		t.Fatalf("missing order status = %d, body=%s", response.Code, response.Body.String())
	}
	if code := decodeErrorCode(t, response); code != "ORDER_NOT_FOUND" {
		t.Fatalf("missing order code = %q", code)
	}

	response = doRequest(t, mux, http.MethodPost, "/api/v1/ops/orders/2/settle", adminToken, `{"paid_amount":21600}`)
	if response.Code != http.StatusConflict {
		t.Fatalf("settled order status = %d, body=%s", response.Code, response.Body.String())
	}
	if code := decodeErrorCode(t, response); code != "ORDER_STATUS_INVALID" {
		t.Fatalf("settled order code = %q", code)
	}

	response = doRequest(t, mux, http.MethodPost, "/api/v1/ops/orders/1/settle", adminToken,
		`{"paid_amount":21600,"paid_at":"2026-11-10T10:00:00Z","note":"财务确认"}`)
	if response.Code != http.StatusOK {
		t.Fatalf("settle status = %d, body=%s", response.Code, response.Body.String())
	}
	var settled Order
	if err := json.Unmarshal(response.Body.Bytes(), &settled); err != nil {
		t.Fatalf("decode settled order: %v", err)
	}
	if settled.Status != OrderStatusSettled || settled.ConfirmedBy == nil || *settled.ConfirmedBy != 1 {
		t.Fatalf("unexpected settled order: %+v", settled)
	}
	if repo.settleInput.PaidAmount != Decimal("21600.00") {
		t.Fatalf("paid amount = %s, want 21600.00", repo.settleInput.PaidAmount)
	}
}

func TestActivateOrderHandler(t *testing.T) {
	period := ServicePeriod{StartedOn: stringPtr("2026-11-10"), ExpiredOn: stringPtr("2027-11-09")}
	activatedAt := time.Date(2026, 11, 10, 11, 0, 0, 0, time.UTC)
	items := []OrderItem{{
		ProductCode: "tingxie", Edition: EditionStandard, QuotaType: QuotaTypeOfficial,
		Quotas: Quantities(`{"record":2500,"round":5000,"hour":400}`), Price: Decimal("21600.00"),
	}}
	repo := &fakeRepository{
		orders: map[int64]Order{
			1: {ID: 1, OrderNo: "BL1", TenantID: 9, OrderType: OrderTypeSingle, Status: OrderStatusSettled, Items: items},
			2: {ID: 2, OrderNo: "BL2", TenantID: 9, OrderType: OrderTypeSingle, Status: OrderStatusSettled, Items: items, ActivatedAt: &activatedAt},
		},
		entitlementItems: []Entitlement{{
			ProductCode: "tingxie", Unit: "record", Edition: EditionStandard, QuotaType: QuotaTypeOfficial,
			Granted: Decimal("2500"), Balance: Decimal("2500"),
		}},
		adminName:        "运营小张",
		activationPeriod: period,
	}
	mux := newOrderTestMux(repo)
	adminToken := mustToken(t, 1, auth.UserTypeAdmin, nil)

	response := doRequest(t, mux, http.MethodPost, "/api/v1/ops/orders/1/activate", adminToken, "")
	if response.Code != http.StatusOK {
		t.Fatalf("activate status = %d, body=%s", response.Code, response.Body.String())
	}
	var result ActivationResult
	if err := json.Unmarshal(response.Body.Bytes(), &result); err != nil {
		t.Fatalf("decode activation: %v (%s)", err, response.Body.String())
	}
	if result.Order.ActivatedAt == nil || result.ServicePeriod == nil || *result.ServicePeriod.ExpiredOn != "2027-11-09" {
		t.Fatalf("unexpected activation payload: %+v", result)
	}
	if len(result.Entitlements) != 1 || result.Entitlements[0].Balance != Decimal("2500") {
		t.Fatalf("unexpected entitlements: %+v", result.Entitlements)
	}
	if repo.activateInput.Operator != "运营小张" {
		t.Fatalf("operator = %q", repo.activateInput.Operator)
	}

	// 重复点击：409 ORDER_ALREADY_ACTIVATED，而不是双倍发额度。
	response = doRequest(t, mux, http.MethodPost, "/api/v1/ops/orders/1/activate", adminToken, "")
	if response.Code != http.StatusConflict {
		t.Fatalf("repeat activation status = %d, body=%s", response.Code, response.Body.String())
	}
	if code := decodeErrorCode(t, response); code != "ORDER_ALREADY_ACTIVATED" {
		t.Fatalf("repeat activation code = %q", code)
	}
	if repo.grantCalls != 1 {
		t.Fatalf("activation must not grant twice, calls=%d", repo.grantCalls)
	}

	response = doRequest(t, mux, http.MethodPost, "/api/v1/ops/orders/2/activate", adminToken, "")
	if response.Code != http.StatusConflict || decodeErrorCode(t, response) != "ORDER_ALREADY_ACTIVATED" {
		t.Fatalf("activated order should be 409 ORDER_ALREADY_ACTIVATED, got %d body=%s", response.Code, response.Body.String())
	}
	response = doRequest(t, mux, http.MethodPost, "/api/v1/ops/orders/99/activate", adminToken, "")
	if response.Code != http.StatusNotFound || decodeErrorCode(t, response) != "ORDER_NOT_FOUND" {
		t.Fatalf("missing order should be 404 ORDER_NOT_FOUND, got %d body=%s", response.Code, response.Body.String())
	}
}

func TestRefundOrderHandlerStatusCodes(t *testing.T) {
	activatedAt := time.Date(2026, 11, 10, 11, 0, 0, 0, time.UTC)
	repo := &fakeRepository{orders: map[int64]Order{
		1: {ID: 1, TenantID: 9, OrderType: OrderTypeSingle, Status: OrderStatusSettled, PayableAmount: Decimal("21600.00")},
		2: {ID: 2, TenantID: 9, OrderType: OrderTypeSingle, Status: OrderStatusSettled, ActivatedAt: &activatedAt},
		3: {ID: 3, TenantID: 9, OrderType: OrderTypeSingle, Status: OrderStatusCreated},
	}}
	mux := newOrderTestMux(repo)
	adminToken := mustToken(t, 1, auth.UserTypeAdmin, nil)

	response := doRequest(t, mux, http.MethodPost, "/api/v1/ops/orders/1/refund", adminToken, "")
	if response.Code != http.StatusOK {
		t.Fatalf("refund status = %d, body=%s", response.Code, response.Body.String())
	}
	var refunded Order
	if err := json.Unmarshal(response.Body.Bytes(), &refunded); err != nil {
		t.Fatalf("decode refunded order: %v", err)
	}
	if refunded.Status != OrderStatusRefunded {
		t.Fatalf("unexpected refunded order: %+v", refunded)
	}

	response = doRequest(t, mux, http.MethodPost, "/api/v1/ops/orders/2/refund", adminToken, "")
	if response.Code != http.StatusBadRequest {
		t.Fatalf("activated order refund status = %d, body=%s", response.Code, response.Body.String())
	}
	if code := decodeErrorCode(t, response); code != "ORDER_ALREADY_ACTIVATED" {
		t.Fatalf("activated order refund code = %q", code)
	}

	response = doRequest(t, mux, http.MethodPost, "/api/v1/ops/orders/3/refund", adminToken, "")
	if response.Code != http.StatusConflict || decodeErrorCode(t, response) != "ORDER_STATUS_INVALID" {
		t.Fatalf("unsettled refund should be 409 ORDER_STATUS_INVALID, got %d body=%s", response.Code, response.Body.String())
	}

	response = doRequest(t, mux, http.MethodPost, "/api/v1/ops/orders/99/refund", adminToken, "")
	if response.Code != http.StatusNotFound || decodeErrorCode(t, response) != "ORDER_NOT_FOUND" {
		t.Fatalf("missing refund should be 404 ORDER_NOT_FOUND, got %d body=%s", response.Code, response.Body.String())
	}
}
