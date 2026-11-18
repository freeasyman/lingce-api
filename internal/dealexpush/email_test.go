package dealexpush

import (
	"context"
	"encoding/base64"
	"errors"
	"fmt"
	"io"
	"mime"
	"mime/multipart"
	"sort"
	"strings"
	"testing"
	"time"

	"github.com/freeasyman/lingce-api/internal/config"
	"github.com/freeasyman/lingce-api/internal/dealexpertpermission"
)

// fakeEmailStore 是内存版 EmailStore：覆盖路由/默认收件人/白名单/投递幂等。
type fakeEmailStore struct {
	event      *EmailEvent
	analysis   *AnalysisCopy
	employees  map[int64]*EmailEmployee
	managerIDs map[int64]int64
	adminID    int64
	routes     map[string]*AlertRoute
	deliveries map[string]*EmailDelivery
	nextID     int64
}

func newFakeEmailStore() *fakeEmailStore {
	return &fakeEmailStore{
		employees:  map[int64]*EmailEmployee{},
		managerIDs: map[int64]int64{},
		routes:     map[string]*AlertRoute{},
		deliveries: map[string]*EmailDelivery{},
		analysis:   &AnalysisCopy{},
	}
}

func (f *fakeEmailStore) addEmployee(id int64, name, email string) {
	f.employees[id] = &EmailEmployee{ID: id, Name: name, Email: email, Role: "consultant"}
}

func routeKey(tenantID, ownerID int64, eventType string) string {
	return fmt.Sprintf("%d:%d:%s", tenantID, ownerID, eventType)
}

func deliveryKey(eventID, employeeID int64) string {
	return fmt.Sprintf("%d:%d", eventID, employeeID)
}

func (f *fakeEmailStore) LoadEmailEvent(_ context.Context, eventID int64) (*EmailEvent, error) {
	if f.event == nil || f.event.ID != eventID {
		return nil, ErrEmailEventNotFound
	}
	copied := *f.event
	return &copied, nil
}

func (f *fakeEmailStore) LoadAnalysisCopy(_ context.Context, _, _ int64, _ string) (*AnalysisCopy, error) {
	copied := *f.analysis
	return &copied, nil
}

func (f *fakeEmailStore) Employee(_ context.Context, tenantID, employeeID int64) (*EmailEmployee, error) {
	employee, ok := f.employees[employeeID]
	if !ok {
		return nil, nil
	}
	copied := *employee
	return &copied, nil
}

func (f *fakeEmailStore) DefaultManager(_ context.Context, _, ownerEmployeeID int64) (int64, error) {
	if managerID := f.managerIDs[ownerEmployeeID]; managerID > 0 {
		return managerID, nil
	}
	return f.adminID, nil
}

func (f *fakeEmailStore) Route(_ context.Context, tenantID, ownerEmployeeID int64, eventType string) (*AlertRoute, error) {
	route, ok := f.routes[routeKey(tenantID, ownerEmployeeID, eventType)]
	if !ok {
		return nil, nil
	}
	copied := *route
	copied.ToEmployeeIDs = append([]int64{}, route.ToEmployeeIDs...)
	copied.CcEmployeeIDs = append([]int64{}, route.CcEmployeeIDs...)
	return &copied, nil
}

func (f *fakeEmailStore) ListRoutes(_ context.Context, tenantID, ownerEmployeeID int64) ([]*AlertRoute, error) {
	items := make([]*AlertRoute, 0)
	for _, route := range f.routes {
		if route.TenantID != tenantID {
			continue
		}
		if ownerEmployeeID > 0 && route.OwnerEmployeeID != ownerEmployeeID {
			continue
		}
		copied := *route
		items = append(items, &copied)
	}
	sort.Slice(items, func(i, j int) bool { return items[i].ID < items[j].ID })
	return items, nil
}

func (f *fakeEmailStore) ListEmployees(_ context.Context, tenantID int64) ([]*EmailEmployee, error) {
	items := make([]*EmailEmployee, 0, len(f.employees))
	for _, employee := range f.employees {
		copied := *employee
		items = append(items, &copied)
	}
	sort.Slice(items, func(i, j int) bool { return items[i].ID < items[j].ID })
	return items, nil
}

func (f *fakeEmailStore) UpsertRoute(_ context.Context, route *AlertRoute) (*AlertRoute, error) {
	f.nextID++
	route.ID = f.nextID
	route.CreatedAt = time.Now()
	route.UpdatedAt = route.CreatedAt
	copied := *route
	copied.ToEmployeeIDs = append([]int64{}, route.ToEmployeeIDs...)
	copied.CcEmployeeIDs = append([]int64{}, route.CcEmployeeIDs...)
	f.routes[routeKey(route.TenantID, route.OwnerEmployeeID, route.EventType)] = &copied
	return route, nil
}

func (f *fakeEmailStore) EmployeesExist(_ context.Context, _ int64, employeeIDs []int64) (bool, error) {
	for _, id := range employeeIDs {
		if _, ok := f.employees[id]; !ok {
			return false, nil
		}
	}
	return true, nil
}

func (f *fakeEmailStore) Delivery(_ context.Context, eventID, employeeID int64) (*EmailDelivery, error) {
	delivery, ok := f.deliveries[deliveryKey(eventID, employeeID)]
	if !ok {
		return nil, nil
	}
	copied := *delivery
	return &copied, nil
}

func (f *fakeEmailStore) SaveDelivery(_ context.Context, delivery *EmailDelivery) error {
	if existing, ok := f.deliveries[deliveryKey(delivery.EventID, delivery.RecipientEmployeeID)]; ok && existing.Status == "sent" {
		return nil
	}
	f.nextID++
	copied := *delivery
	copied.ID = f.nextID
	copied.CreatedAt = time.Now()
	f.deliveries[deliveryKey(delivery.EventID, delivery.RecipientEmployeeID)] = &copied
	delivery.ID = copied.ID
	return nil
}

func (f *fakeEmailStore) EnsureShortLink(_ context.Context, tenantID, recordingID, employeeID int64) (string, error) {
	return fmt.Sprintf("L%dE%dT%d", recordingID, employeeID, tenantID), nil
}

// fakeEmailMailer 记录发出的邮件并可按收件人注入失败。
type fakeEmailMailer struct {
	messages []EmailMessage
	calls    int
	failFor  map[string]error
}

func (f *fakeEmailMailer) Send(_ context.Context, msg EmailMessage) (string, error) {
	f.calls++
	f.messages = append(f.messages, msg)
	if err, ok := f.failFor[msg.To]; ok && err != nil {
		return "", err
	}
	return fmt.Sprintf("msg-%d", f.calls), nil
}

func testEmailConfig(dryRun bool, whitelist ...string) config.DealEmailConfig {
	return config.DealEmailConfig{
		Enabled: true, DryRun: &dryRun, From: "noreply@test.local",
		LinkBase: "https://test.local/s", Whitelist: whitelist,
	}
}

func newEmailTestService(store EmailStore, mailer EmailMailer, cfg config.DealEmailConfig) *Service {
	return &Service{log: testLogger(), emailCfg: cfg, emailStore: store, mailer: mailer}
}

// newEmailFixture 构造：咨询师 42（白名单）、负责人 7（白名单）、抄送 8（无邮箱）。
func newEmailFixture(t *testing.T) (*fakeEmailStore, *fakeEmailMailer, *Service, string, string) {
	t.Helper()
	store := newFakeEmailStore()
	store.event = &EmailEvent{
		ID: 9, TenantID: 1, RecordingID: 100, RecipientEmployeeID: 42,
		AnalysisVersion: "v0.17", RecipientRole: "consultant",
		ConsultantEmployeeID: 42, CustomerName: "杜舒然", CustomerGender: "女",
	}
	store.analysis = &AnalysisCopy{
		DealStatus: "意向明确未成交", FirstTaskTitle: "24小时内微信跟进",
		ManagerReason: "客户说“怕有后遗症，身边有人说会干眼”", ManagerAction: "准备干眼检查流程说明",
	}
	store.addEmployee(42, "廖晓琴", "freeasyman@yeah.net")
	store.addEmployee(7, "王院长", "yiliang@lingce.pro")
	store.addEmployee(8, "吴双", "")
	store.managerIDs[42] = 7
	store.adminID = 7
	mailer := &fakeEmailMailer{failFor: map[string]error{}}
	service := newEmailTestService(store, mailer, testEmailConfig(false, "freeasyman@yeah.net", "yiliang@lingce.pro"))
	return store, mailer, service, "freeasyman@yeah.net", "yiliang@lingce.pro"
}

func TestResolveEmailRecipientsConsultantDefault(t *testing.T) {
	store, _, service, _, _ := newEmailFixture(t)
	event, _ := store.LoadEmailEvent(context.Background(), 9)
	recipients, err := service.resolveEmailRecipients(context.Background(), event)
	if err != nil {
		t.Fatalf("resolve: %v", err)
	}
	if len(recipients) != 1 {
		t.Fatalf("recipients = %d, want 1", len(recipients))
	}
	if recipients[0].EmployeeID != 42 || recipients[0].Kind != "to" || recipients[0].Skip != "" {
		t.Fatalf("unexpected recipient: %+v", recipients[0])
	}
}

func TestResolveEmailRecipientsManagerDefault(t *testing.T) {
	store, _, service, _, _ := newEmailFixture(t)
	store.event.RecipientRole = "manager"
	store.event.RecipientEmployeeID = store.adminID // 事件收件人是 admin，但 owner 必须是咨询师
	event, _ := store.LoadEmailEvent(context.Background(), 9)
	recipients, err := service.resolveEmailRecipients(context.Background(), event)
	if err != nil {
		t.Fatalf("resolve: %v", err)
	}
	if len(recipients) != 1 || recipients[0].EmployeeID != 7 || recipients[0].Kind != "to" {
		t.Fatalf("manager default recipients = %+v, want employee 7 to", recipients)
	}

	// 没有 manager_id 时回退租户管理员。
	store.managerIDs = map[int64]int64{}
	recipients, err = service.resolveEmailRecipients(context.Background(), event)
	if err != nil {
		t.Fatalf("resolve fallback: %v", err)
	}
	if len(recipients) != 1 || recipients[0].EmployeeID != 7 {
		t.Fatalf("admin fallback recipients = %+v, want employee 7", recipients)
	}
}

func TestResolveEmailRecipientsRouteOverride(t *testing.T) {
	store, _, service, _, _ := newEmailFixture(t)
	store.routes[routeKey(1, 42, "consultant")] = &AlertRoute{
		ID: 1, TenantID: 1, OwnerEmployeeID: 42, EventType: "consultant",
		ToEmployeeIDs: []int64{7, 42}, CcEmployeeIDs: []int64{8, 7},
	}
	event, _ := store.LoadEmailEvent(context.Background(), 9)
	recipients, err := service.resolveEmailRecipients(context.Background(), event)
	if err != nil {
		t.Fatalf("resolve: %v", err)
	}
	if len(recipients) != 3 {
		t.Fatalf("recipients = %+v, want 3 (to 7,42 + cc 8; 7 deduped from cc)", recipients)
	}
	kinds := map[int64]string{}
	for _, recipient := range recipients {
		kinds[recipient.EmployeeID] = recipient.Kind
	}
	if kinds[7] != "to" || kinds[42] != "to" || kinds[8] != "cc" {
		t.Fatalf("kinds = %v", kinds)
	}
}

func TestResolveEmailRecipientsEmptyRouteFallsBackToDefault(t *testing.T) {
	store, _, service, _, _ := newEmailFixture(t)
	store.routes[routeKey(1, 42, "consultant")] = &AlertRoute{
		ID: 1, TenantID: 1, OwnerEmployeeID: 42, EventType: "consultant",
		ToEmployeeIDs: []int64{}, CcEmployeeIDs: []int64{},
	}
	event, _ := store.LoadEmailEvent(context.Background(), 9)
	recipients, err := service.resolveEmailRecipients(context.Background(), event)
	if err != nil {
		t.Fatalf("resolve: %v", err)
	}
	if len(recipients) != 1 || recipients[0].EmployeeID != 42 || recipients[0].Kind != "to" {
		t.Fatalf("empty route must fall back to default owner, got %+v", recipients)
	}
}

func TestEmailWhitelistFiltersRecipients(t *testing.T) {
	store, mailer, service, consultantEmail, _ := newEmailFixture(t)
	service.emailCfg = testEmailConfig(false, consultantEmail)
	store.routes[routeKey(1, 42, "consultant")] = &AlertRoute{
		ID: 1, TenantID: 1, OwnerEmployeeID: 42, EventType: "consultant",
		ToEmployeeIDs: []int64{7, 42}, CcEmployeeIDs: []int64{8},
	}
	results, err := service.SendEventEmail(context.Background(), 9, 1)
	if err != nil {
		t.Fatalf("send: %v", err)
	}
	if mailer.calls != 1 || len(mailer.messages) != 1 || mailer.messages[0].To != consultantEmail {
		t.Fatalf("mailer calls=%d messages=%+v, want only whitelisted consultant", mailer.calls, mailer.messages)
	}
	if len(results) != 3 {
		t.Fatalf("results = %d, want 3", len(results))
	}
	statuses := map[int64]string{}
	for _, result := range results {
		statuses[result.RecipientEmployeeID] = result.Status + ":" + result.Error
	}
	if statuses[42] != "sent:" {
		t.Fatalf("consultant status = %q", statuses[42])
	}
	if statuses[7] != "skipped:not_in_whitelist" {
		t.Fatalf("manager status = %q", statuses[7])
	}
	if statuses[8] != "skipped:no_email" {
		t.Fatalf("cc status = %q", statuses[8])
	}
}

func TestSendEventEmailSendsOnePerRecipient(t *testing.T) {
	store, mailer, service, consultantEmail, managerEmail := newEmailFixture(t)
	store.employees[8].Email = "cc@lingce.pro"
	service.emailCfg = testEmailConfig(false, "freeasyman@yeah.net", "yiliang@lingce.pro", "cc@lingce.pro")
	store.routes[routeKey(1, 42, "consultant")] = &AlertRoute{
		ID: 1, TenantID: 1, OwnerEmployeeID: 42, EventType: "consultant",
		ToEmployeeIDs: []int64{42, 7}, CcEmployeeIDs: []int64{8},
	}
	results, err := service.SendEventEmail(context.Background(), 9, 1)
	if err != nil {
		t.Fatalf("send: %v", err)
	}
	if mailer.calls != 3 {
		t.Fatalf("mailer calls = %d, want 3 (to+to+cc)", mailer.calls)
	}
	seen := map[string]int{}
	for _, message := range mailer.messages {
		seen[message.To]++
		if strings.Contains(message.To, ",") || strings.Contains(message.To, ";") {
			t.Fatalf("each email must have exactly one To recipient, got %q", message.To)
		}
		if message.Subject != "接待分析已完成 · 杜女士" {
			t.Fatalf("subject = %q", message.Subject)
		}
		if !strings.Contains(message.Text, "https://test.local/s/L100E") {
			t.Fatalf("text missing per-employee link: %q", message.Text)
		}
		if !strings.Contains(message.HTML, "<html") {
			t.Fatal("html version missing")
		}
	}
	if seen[consultantEmail] != 1 || seen[managerEmail] != 1 || seen["cc@lingce.pro"] != 1 {
		t.Fatalf("seen = %v", seen)
	}
	if len(results) != 3 {
		t.Fatalf("results = %d", len(results))
	}
	deliveries := map[int64]*EmailDelivery{}
	for _, delivery := range store.deliveries {
		deliveries[delivery.RecipientEmployeeID] = delivery
	}
	if deliveries[42].Status != "sent" || deliveries[7].Status != "sent" {
		t.Fatalf("deliveries = %+v", deliveries)
	}
	if deliveries[42].MessageID == "" || deliveries[42].SentAt == nil {
		t.Fatalf("sent delivery must record message id/sent_at: %+v", deliveries[42])
	}
	if deliveries[8].Status != "sent" || deliveries[8].Kind != "cc" {
		t.Fatalf("cc delivery = %+v", deliveries[8])
	}
}

func TestSendEventEmailIsIdempotent(t *testing.T) {
	store, mailer, service, _, _ := newEmailFixture(t)
	store.routes[routeKey(1, 42, "consultant")] = &AlertRoute{
		ID: 1, TenantID: 1, OwnerEmployeeID: 42, EventType: "consultant",
		ToEmployeeIDs: []int64{42, 7}, CcEmployeeIDs: []int64{},
	}
	if _, err := service.SendEventEmail(context.Background(), 9, 1); err != nil {
		t.Fatalf("first send: %v", err)
	}
	if mailer.calls != 2 {
		t.Fatalf("first calls = %d, want 2", mailer.calls)
	}
	results, err := service.SendEventEmail(context.Background(), 9, 1)
	if err != nil {
		t.Fatalf("second send: %v", err)
	}
	if mailer.calls != 2 {
		t.Fatalf("second send must not resend, calls = %d", mailer.calls)
	}
	for _, result := range results {
		if result.Status != "sent" {
			t.Fatalf("idempotent result = %+v", result)
		}
	}
}

func TestSendEventEmailRecordsFailure(t *testing.T) {
	store, mailer, service, _, managerEmail := newEmailFixture(t)
	mailer.failFor[managerEmail] = errors.New("smtp: 550 mailbox unavailable")
	store.routes[routeKey(1, 42, "consultant")] = &AlertRoute{
		ID: 1, TenantID: 1, OwnerEmployeeID: 42, EventType: "consultant",
		ToEmployeeIDs: []int64{42, 7}, CcEmployeeIDs: []int64{},
	}
	results, err := service.SendEventEmail(context.Background(), 9, 1)
	if err != nil {
		t.Fatalf("send: %v", err)
	}
	statuses := map[int64]string{}
	for _, result := range results {
		statuses[result.RecipientEmployeeID] = result.Status
	}
	if statuses[42] != "sent" || statuses[7] != "failed" {
		t.Fatalf("statuses = %v", statuses)
	}
	var failed *EmailDelivery
	for _, delivery := range store.deliveries {
		if delivery.RecipientEmployeeID == 7 {
			failed = delivery
		}
	}
	if failed == nil || !strings.Contains(failed.Error, "550") {
		t.Fatalf("failed delivery = %+v", failed)
	}
}

func TestSendEventEmailDryRun(t *testing.T) {
	store, mailer, service, _, _ := newEmailFixture(t)
	service.emailCfg = testEmailConfig(true, "freeasyman@yeah.net", "yiliang@lingce.pro")
	store.routes[routeKey(1, 42, "consultant")] = &AlertRoute{
		ID: 1, TenantID: 1, OwnerEmployeeID: 42, EventType: "consultant",
		ToEmployeeIDs: []int64{42, 7}, CcEmployeeIDs: []int64{},
	}
	results, err := service.SendEventEmail(context.Background(), 9, 1)
	if err != nil {
		t.Fatalf("send: %v", err)
	}
	if mailer.calls != 0 {
		t.Fatalf("dry run must not call smtp, calls = %d", mailer.calls)
	}
	for _, result := range results {
		if result.Status != "skipped" || result.Error != "dry_run" {
			t.Fatalf("dry run result = %+v", result)
		}
	}
}

func TestSendEventEmailNoRecipientsRecordsSkipped(t *testing.T) {
	store, mailer, service, _, _ := newEmailFixture(t)
	store.event.RecipientRole = "manager"
	store.event.ConsultantEmployeeID = 0
	store.adminID = 0
	results, err := service.SendEventEmail(context.Background(), 9, 1)
	if err != nil {
		t.Fatalf("send: %v", err)
	}
	if mailer.calls != 0 {
		t.Fatalf("mailer calls = %d, want 0", mailer.calls)
	}
	if len(results) != 1 || results[0].Status != "skipped" || results[0].Error != "no_recipients" {
		t.Fatalf("results = %+v", results)
	}
	delivery, ok := store.deliveries[deliveryKey(9, 0)]
	if !ok || delivery.Status != "skipped" || delivery.Error != "no_recipients" {
		t.Fatalf("event-level skipped delivery = %+v", delivery)
	}
}

func TestSendEventEmailTenantMismatch(t *testing.T) {
	_, _, service, _, _ := newEmailFixture(t)
	if _, err := service.SendEventEmail(context.Background(), 9, 2); !errors.Is(err, ErrEmailEventNotFound) {
		t.Fatalf("err = %v, want ErrEmailEventNotFound", err)
	}
}

func TestEmailCopyRedactsSensitiveData(t *testing.T) {
	analysis := &AnalysisCopy{
		ManagerReason: "赵子墨干眼手术后犹豫，报价6000元，电话13800000000",
		ManagerAction: "让赵子墨补缴6000元并确认术前时间",
	}
	subject, text, htmlBody := buildEmailCopy("manager", "赵子墨", "赵女士", "王院长", "廖晓琴", time.Now(), analysis, "https://test.local/s/ABC123")
	for _, forbidden := range []string{"赵子墨", "6000", "13800000000", "干眼", "手术", "术前"} {
		for name, content := range map[string]string{"subject": subject, "text": text, "html": htmlBody} {
			if strings.Contains(content, forbidden) {
				t.Fatalf("%s leaked %q: %q", name, forbidden, content)
			}
		}
	}
	if !strings.Contains(subject, "赵女士") {
		t.Fatalf("subject = %q", subject)
	}
	if !strings.Contains(text, "（金额略）") || !strings.Contains(text, "（电话略）") || !strings.Contains(text, "（诊疗信息略）") {
		t.Fatalf("text not sanitized: %q", text)
	}
	if !strings.Contains(htmlBody, "https://test.local/s/ABC123") {
		t.Fatal("html missing CTA link")
	}
	if !strings.Contains(text, "原因：") || !strings.Contains(text, "建议动作：") {
		t.Fatalf("manager copy must include reason and action: %q", text)
	}
}

func TestBuildMIMEMessageSingleRecipient(t *testing.T) {
	msg := EmailMessage{
		From: "noreply@test.local", To: "freeasyman@yeah.net",
		Subject: "接待分析已完成 · 杜女士",
		Text:    "纯文本正文 中文", HTML: "<html><body>HTML 中文</body></html>",
		MessageID: "<id@test.local>",
	}
	raw := string(buildMIMEMessage(msg))
	if !strings.Contains(raw, "To: freeasyman@yeah.net\r\n") {
		t.Fatalf("missing single To header: %q", raw)
	}
	if strings.Contains(raw, "Cc:") {
		t.Fatal("email must not include Cc header")
	}
	if want := mime.BEncoding.Encode("UTF-8", msg.Subject); !strings.Contains(raw, "Subject: "+want+"\r\n") {
		t.Fatalf("subject not encoded: %q", raw)
	}

	header, body, ok := strings.Cut(raw, "\r\n\r\n")
	if !ok {
		t.Fatal("no header/body separator")
	}
	mediaType, params, err := mime.ParseMediaType(headerValue(header, "Content-Type"))
	if err != nil || mediaType != "multipart/alternative" {
		t.Fatalf("content type = %q, %v", mediaType, err)
	}
	reader := multipart.NewReader(strings.NewReader(body), params["boundary"])
	readPart := func() (string, string) {
		part, err := reader.NextPart()
		if err != nil {
			t.Fatalf("next part: %v", err)
		}
		data, err := io.ReadAll(part)
		if err != nil {
			t.Fatalf("read part: %v", err)
		}
		decoded, err := base64.StdEncoding.DecodeString(strings.ReplaceAll(string(data), "\r\n", ""))
		if err != nil {
			t.Fatalf("decode base64 body: %v", err)
		}
		return part.Header.Get("Content-Type"), string(decoded)
	}
	if contentType, decoded := readPart(); !strings.Contains(contentType, "text/plain") || decoded != msg.Text {
		t.Fatalf("plain part = %q %q", contentType, decoded)
	}
	if contentType, decoded := readPart(); !strings.Contains(contentType, "text/html") || decoded != msg.HTML {
		t.Fatalf("html part = %q %q", contentType, decoded)
	}
}

func headerValue(header, key string) string {
	for _, line := range strings.Split(header, "\r\n") {
		prefix := key + ": "
		if strings.HasPrefix(line, prefix) {
			return strings.TrimSpace(strings.TrimPrefix(line, prefix))
		}
	}
	return ""
}

func TestResolveSMTPConfigFallsBackToTicketNotify(t *testing.T) {
	fallback := config.TicketNotifyConfig{
		SMTPHost: "smtp.163.com", SMTPPort: 465,
		SMTPUser: "tianjinlingce@163.com", SMTPPass: "secret", From: "tianjinlingce@163.com",
	}
	resolved := ResolveSMTPConfig(config.DealEmailConfig{From: "custom@example.com"}, fallback)
	if resolved.Host != fallback.SMTPHost || resolved.Port != 465 || resolved.User != fallback.SMTPUser ||
		resolved.Pass != fallback.SMTPPass || resolved.From != "custom@example.com" {
		t.Fatalf("resolved = %+v", resolved)
	}
	override := ResolveSMTPConfig(config.DealEmailConfig{
		SMTPHost: "smtp.example.com", SMTPPort: 2525, SMTPUser: "u", SMTPPass: "p", From: "f@example.com",
	}, fallback)
	if override.Host != "smtp.example.com" || override.Port != 2525 || override.User != "u" || override.Pass != "p" {
		t.Fatalf("override = %+v", override)
	}
}

func TestEmailWhitelisted(t *testing.T) {
	whitelist := []string{"freeasyman@yeah.net", " Yiliang@Lingce.Pro "}
	cases := map[string]bool{
		"freeasyman@yeah.net": true,
		"FREEASYMAN@YEAH.NET": true,
		"yiliang@lingce.pro":  true,
		"other@example.com":   false,
		"":                    false,
	}
	for email, want := range cases {
		if got := emailWhitelisted(whitelist, email); got != want {
			t.Fatalf("emailWhitelisted(%q) = %v, want %v", email, got, want)
		}
	}
	if emailWhitelisted(nil, "freeasyman@yeah.net") {
		t.Fatal("empty whitelist must reject all recipients")
	}
}

func TestNormalizeEventType(t *testing.T) {
	cases := map[string]string{
		"consultant": "consultant", "manager": "manager", "intervention": "manager",
		"MANAGER": "manager", "": "consultant", "other": "consultant",
	}
	for role, want := range cases {
		if got := normalizeEventType(role); got != want {
			t.Fatalf("normalizeEventType(%q) = %q, want %q", role, got, want)
		}
	}
}

func TestSendEmailForEventTriggersDelivery(t *testing.T) {
	_, mailer, service, _, _ := newEmailFixture(t)
	service.sendEmailForEvent(context.Background(), 9)
	if mailer.calls != 1 {
		t.Fatalf("event insertion must trigger one email, calls = %d", mailer.calls)
	}

	// 开关关闭时不得发信。
	service.emailCfg.Enabled = false
	service.sendEmailForEvent(context.Background(), 9)
	if mailer.calls != 1 {
		t.Fatalf("disabled email must not send, calls = %d", mailer.calls)
	}
}

func TestCanAccessAlertRoutes(t *testing.T) {
	abilities := func(codes ...string) map[string]struct{} {
		set := map[string]struct{}{}
		for _, code := range codes {
			set[code] = struct{}{}
		}
		return set
	}
	cases := []struct {
		name   string
		access *dealexpertpermission.Access
		want   bool
	}{
		{"nil", nil, false},
		{"disabled", &dealexpertpermission.Access{Enabled: false, Admin: true}, false},
		{"admin", &dealexpertpermission.Access{Enabled: true, Admin: true}, true},
		{"workbench read (frontend guard)", &dealexpertpermission.Access{Enabled: true, Abilities: abilities(dealexpertpermission.AbilityWorkbenchRead)}, true},
		{"permission manage", &dealexpertpermission.Access{Enabled: true, Abilities: abilities(dealexpertpermission.AbilityPermissionManage)}, true},
		{"staff without alert access", &dealexpertpermission.Access{Enabled: true, Abilities: abilities(dealexpertpermission.AbilityTaskRead)}, false},
	}
	for _, item := range cases {
		t.Run(item.name, func(t *testing.T) {
			if got := canAccessAlertRoutes(item.access); got != item.want {
				t.Fatalf("canAccessAlertRoutes(%+v) = %v, want %v", item.access, got, item.want)
			}
		})
	}
}
