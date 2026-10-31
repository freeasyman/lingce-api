package patientops

import (
	"context"
	"crypto/sha256"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/freeasyman/lingce-api/internal/customer"
	"github.com/freeasyman/lingce-api/internal/customercontact"
	"github.com/freeasyman/lingce-api/pkg/llmgateway"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

// Explicitly opt in to a local database. Each test owns an isolated schema and
// exercises the real compatibility trigger, SQL, transactions and row locks.
func contactTestService(t *testing.T) (*Service, context.Context) {
	t.Helper()
	dsn := os.Getenv("LINGCE_CONTACT_TEST_DATABASE_URL")
	if dsn == "" {
		t.Skip("set LINGCE_CONTACT_TEST_DATABASE_URL to run local PostgreSQL integration tests")
	}
	config, err := pgxpool.ParseConfig(dsn)
	if err != nil {
		t.Fatal(err)
	}
	if config.ConnConfig.Host != "127.0.0.1" && config.ConnConfig.Host != "localhost" {
		t.Fatal("contact integration tests require a local PostgreSQL host")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 40*time.Second)
	t.Cleanup(cancel)
	admin, err := pgxpool.NewWithConfig(ctx, config)
	if err != nil {
		t.Fatal(err)
	}
	schema := fmt.Sprintf("contact_test_%d", time.Now().UnixNano())
	ident := pgx.Identifier{schema}.Sanitize()
	if _, err := admin.Exec(ctx, "CREATE SCHEMA "+ident); err != nil {
		t.Fatal(err)
	}
	config = config.Copy()
	config.ConnConfig.RuntimeParams["search_path"] = schema
	pool, err := pgxpool.NewWithConfig(ctx, config)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		pool.Close()
		_, _ = admin.Exec(context.Background(), "DROP SCHEMA "+ident+" CASCADE")
		admin.Close()
	})
	for _, path := range []string{"testdata/contact_schema.sql", "../store/migrations/20261014_004_customer_identity_trigger_safety.sql", "../store/migrations/20261031_074_screenshot_customer_leads.sql"} {
		sql, err := os.ReadFile(filepath.Clean(path))
		if err != nil {
			t.Fatal(err)
		}
		if _, err := pool.Exec(ctx, string(sql)); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := pool.Exec(ctx, `CREATE TRIGGER sync_contact_test BEFORE INSERT OR UPDATE ON customer_identities FOR EACH ROW EXECUTE FUNCTION sync_customer_identity_compatibility()`); err != nil {
		t.Fatal(err)
	}
	return NewService(pool, llmgateway.NewClient("http://127.0.0.1:1", "unused-test-key")), ctx
}

func contactTestCustomer(t *testing.T, s *Service, ctx context.Context, tenant int64, name string) int64 {
	t.Helper()
	var id int64
	if err := s.pool.QueryRow(ctx, `INSERT INTO customers(tenant_id,name) VALUES($1,$2) RETURNING id`, tenant, name).Scan(&id); err != nil {
		t.Fatal(err)
	}
	return id
}
func contactTestSource(t *testing.T, s *Service, ctx context.Context, tenant int64, customerID *int64, payload string) int64 {
	t.Helper()
	var id int64
	if err := s.pool.QueryRow(ctx, `INSERT INTO patient_conversation_sources(tenant_id,customer_id,recognition_payload) VALUES($1,$2,$3::jsonb) RETURNING id`, tenant, customerID, payload).Scan(&id); err != nil {
		t.Fatal(err)
	}
	return id
}
func contactTestLoad(t *testing.T, s *Service, ctx context.Context, tenant, id int64) []customercontact.Contact {
	t.Helper()
	result, err := customercontact.Load(ctx, s.pool, tenant, []int64{id})
	if err != nil {
		t.Fatal(err)
	}
	return result[id]
}
func contactTestCount(t *testing.T, s *Service, ctx context.Context, query string, args ...any) int {
	t.Helper()
	var n int
	if err := s.pool.QueryRow(ctx, query, args...).Scan(&n); err != nil {
		t.Fatal(err)
	}
	return n
}

func TestScreenshotContactProspectIntegration(t *testing.T) {
	s, ctx := contactTestService(t)
	source := contactTestSource(t, s, ctx, 1, nil, `{"platform":"小红书","nickname":"张小姐爱生活"}`)
	req := CreateProspectRequest{Name: "张三"}
	id, err := s.CreateProspect(ctx, 1, 1, source, req)
	if err != nil {
		t.Fatal(err)
	}
	contacts := contactTestLoad(t, s, ctx, 1, id)
	if len(contacts) != 1 || contacts[0].ExternalID != "" || contacts[0].ExternalName != "张小姐爱生活" || !contacts[0].CanContact() {
		t.Fatalf("nickname-only clue lost: %+v", contacts)
	}
	if n := contactTestCount(t, s, ctx, `SELECT count(*) FROM customer_identities WHERE customer_id=$1 AND coalesce(channel_id,'')='' AND coalesce(external_id,'')=''`, id); n != 1 {
		t.Fatal("nickname became a fabricated account")
	}
	repeated, err := s.CreateProspect(ctx, 1, 1, source, req)
	if err != nil || repeated != id {
		t.Fatalf("replay: %d %v", repeated, err)
	}
	if n := contactTestCount(t, s, ctx, `SELECT count(*) FROM customers`); n != 1 {
		t.Fatalf("replay created %d customers", n)
	}
	var wg sync.WaitGroup
	errs := make(chan error, 8)
	for n := 0; n < 8; n++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			replay, err := s.CreateProspect(ctx, 1, 1, source, req)
			if err == nil && replay != id {
				err = fmt.Errorf("different customer on replay: %d", replay)
			}
			errs <- err
		}()
	}
	wg.Wait()
	close(errs)
	for err := range errs {
		if err != nil {
			t.Fatal(err)
		}
	}
	if n := contactTestCount(t, s, ctx, `SELECT count(*) FROM customers`); n != 1 {
		t.Fatal("concurrent replay created duplicate customers")
	}

	// An explicit empty nickname differs from an omitted nickname.
	blank := ""
	source2 := contactTestSource(t, s, ctx, 1, nil, `{"platform":"微信","nickname":"不采用的识别昵称"}`)
	id2, err := s.CreateProspect(ctx, 1, 1, source2, CreateProspectRequest{Name: "人工客户", Nickname: &blank})
	if err != nil {
		t.Fatal(err)
	}
	if contacts := contactTestLoad(t, s, ctx, 1, id2); len(contacts) != 0 {
		t.Fatalf("explicitly cleared nickname was saved: %+v", contacts)
	}
	// A nickname belongs to each selected customer; it never causes merging.
	source3 := contactTestSource(t, s, ctx, 1, nil, `{"platform":"小红书","nickname":"张小姐爱生活"}`)
	id3, err := s.CreateProspect(ctx, 1, 1, source3, req)
	if err != nil || id3 == id {
		t.Fatalf("same nickname merged customers: %d %v", id3, err)
	}
	if len(contactTestLoad(t, s, ctx, 1, id3)) != 1 {
		t.Fatal("second customer's nickname missing")
	}
}

func TestContactLoadLegacyAndTenantIsolationIntegration(t *testing.T) {
	s, ctx := contactTestService(t)
	local := contactTestCustomer(t, s, ctx, 1, "本租户")
	foreign := contactTestCustomer(t, s, ctx, 2, "其他租户")
	deleted := contactTestCustomer(t, s, ctx, 1, "已删除")
	// Exercise old/new-column compatibility and exact duplicate projection.
	for _, id := range []int64{local, foreign, deleted} {
		for n := 0; n < 2; n++ {
			if _, err := s.pool.Exec(ctx, `INSERT INTO customer_identities(customer_id,channel_type,external_name) VALUES($1,'小红书','同一昵称')`, id); err != nil {
				t.Fatal(err)
			}
		}
	}
	if _, err := s.pool.Exec(ctx, `UPDATE customers SET deleted_at=now() WHERE id=$1`, deleted); err != nil {
		t.Fatal(err)
	}
	if _, err := s.pool.Exec(ctx, `INSERT INTO customer_identities(customer_id,channel_type,external_id) VALUES($1,'patient','MRN-123'),($1,'__auto__','123')`, local); err != nil {
		t.Fatal(err)
	}
	loaded, err := customercontact.Load(ctx, s.pool, 1, []int64{local, foreign, deleted})
	if err != nil {
		t.Fatal(err)
	}
	if len(loaded) != 1 || len(loaded[local]) != 1 || loaded[local][0].ChannelType != "xiaohongshu" {
		t.Fatalf("legacy/tenant/internal projection: %+v", loaded)
	}
}

func TestScreenshotContactConcurrentAssociationIntegration(t *testing.T) {
	s, ctx := contactTestService(t)
	id := contactTestCustomer(t, s, ctx, 1, "客户一")
	var wg sync.WaitGroup
	errs := make(chan error, 12)
	for i := 0; i < 12; i++ {
		source := contactTestSource(t, s, ctx, 1, nil, `{"platform":"微信","account":"WX-1","nickname":"张小姐"}`)
		wg.Add(1)
		go func() { defer wg.Done(); errs <- s.ConfirmScreenshot(ctx, 1, 1, source, id) }()
	}
	wg.Wait()
	close(errs)
	for err := range errs {
		if err != nil {
			t.Fatal(err)
		}
	}
	if n := contactTestCount(t, s, ctx, `SELECT count(*) FROM customer_identities WHERE customer_id=$1`, id); n != 1 {
		t.Fatalf("concurrent duplicates: %d", n)
	}
	source := contactTestSource(t, s, ctx, 1, nil, `{"platform":"weixin","account":"WX-1","nickname":"张小姐新昵称"}`)
	if err := s.ConfirmScreenshot(ctx, 1, 1, source, id); err != nil {
		t.Fatal(err)
	}
	source2 := contactTestSource(t, s, ctx, 1, nil, `{"platform":"抖音","nickname":"爱生活的张小姐"}`)
	if err := s.ConfirmScreenshot(ctx, 1, 1, source2, id); err != nil {
		t.Fatal(err)
	}
	if err := s.ConfirmScreenshot(ctx, 1, 1, source2, id); err != nil {
		t.Fatal(err)
	}
	contacts := contactTestLoad(t, s, ctx, 1, id)
	if len(contacts) != 2 || contacts[1].ExternalName != "张小姐新昵称" {
		t.Fatalf("multiple channels/update lost: %+v", contacts)
	}
	if !planHasContact(planCustomerFact{Contacts: contacts}) {
		t.Fatal("platform-only customer excluded")
	}
}

func TestScreenshotContactTenantAndRollbackIntegration(t *testing.T) {
	s, ctx := contactTestService(t)
	foreign := contactTestCustomer(t, s, ctx, 2, "其他租户")
	deleted := contactTestCustomer(t, s, ctx, 1, "已删除")
	if _, err := s.pool.Exec(ctx, `UPDATE customers SET deleted_at=now() WHERE id=$1`, deleted); err != nil {
		t.Fatal(err)
	}
	for _, payload := range []string{`{"platform":"微信","nickname":"昵称"}`, `{}`} {
		source := contactTestSource(t, s, ctx, 1, nil, payload)
		for _, id := range []int64{foreign, deleted} {
			if err := s.ConfirmScreenshot(ctx, 1, 1, source, id); err == nil {
				t.Fatal("invalid association accepted")
			}
			if err := s.syncScreenshotContact(ctx, 1, source, &id); err == nil {
				t.Fatal("invalid context binding accepted")
			}
		}
		if n := contactTestCount(t, s, ctx, `SELECT count(*) FROM patient_conversation_sources WHERE id=$1 AND customer_id IS NULL AND match_status='pending'`, source); n != 1 {
			t.Fatal("failed transaction changed source")
		}
	}
	local := contactTestCustomer(t, s, ctx, 1, "本租户")
	source := contactTestSource(t, s, ctx, 2, nil, `{"platform":"微信","nickname":"昵称"}`)
	if err := s.ConfirmScreenshot(ctx, 1, 1, source, local); err == nil {
		t.Fatal("cross-tenant source accepted")
	}
	if n := contactTestCount(t, s, ctx, `SELECT count(*) FROM customer_identities`); n != 0 {
		t.Fatal("failed associations wrote contacts")
	}
	// Force a contact storage failure and check that the source stays unbound.
	source = contactTestSource(t, s, ctx, 1, nil, `{"platform":"微信","nickname":"拒绝写入"}`)
	if _, err := s.pool.Exec(ctx, `ALTER TABLE customer_identities ADD CHECK (nickname <> '拒绝写入')`); err != nil {
		t.Fatal(err)
	}
	if err := s.ConfirmScreenshot(ctx, 1, 1, source, local); err == nil {
		t.Fatal("injected contact failure ignored")
	}
	if n := contactTestCount(t, s, ctx, `SELECT count(*) FROM patient_conversation_sources WHERE id=$1 AND customer_id IS NULL`, source); n != 1 {
		t.Fatal("association committed without contact")
	}
	before := contactTestCount(t, s, ctx, `SELECT count(*) FROM customers`)
	if _, err := s.CreateProspect(ctx, 1, 1, source, CreateProspectRequest{Name: "不会保留的客户"}); err == nil {
		t.Fatal("prospect contact failure ignored")
	}
	if after := contactTestCount(t, s, ctx, `SELECT count(*) FROM customers`); after != before {
		t.Fatal("customer committed despite failed contact")
	}
	// Force the final binding write to fail after the contact insert.
	source = contactTestSource(t, s, ctx, 1, nil, `{"platform":"微信","nickname":"正常昵称"}`)
	if _, err := s.pool.Exec(ctx, `ALTER TABLE patient_conversation_sources ADD CHECK (match_status <> 'confirmed')`); err != nil {
		t.Fatal(err)
	}
	if err := s.ConfirmScreenshot(ctx, 1, 1, source, local); err == nil {
		t.Fatal("binding failure ignored")
	}
	if n := contactTestCount(t, s, ctx, `SELECT count(*) FROM customer_identities`); n != 0 {
		t.Fatal("contact committed despite failed association")
	}

}

func TestScreenshotContactContextReuseAndCandidateIntegration(t *testing.T) {
	s, ctx := contactTestService(t)
	id := contactTestCustomer(t, s, ctx, 1, "张三")
	source := contactTestSource(t, s, ctx, 1, &id, `{"platform":"小红书","account":"ABCDEF","nickname":"张小姐"}`)
	if err := s.syncBoundScreenshotContact(ctx, 1, source); err != nil {
		t.Fatal(err)
	}
	other := contactTestCustomer(t, s, ctx, 1, "同昵称")
	otherSource := contactTestSource(t, s, ctx, 1, &other, `{"platform":"微信","account":"ABCDEF","nickname":"张小姐"}`)
	if err := s.syncBoundScreenshotContact(ctx, 1, otherSource); err != nil {
		t.Fatal(err)
	}
	candidates, err := s.findCandidates(ctx, 1, "xhs", "ABCDEF", "张小姐", nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(candidates) != 2 || candidates[0].ID != id || !strings.Contains(candidates[0].Reason, "同平台账号一致") || strings.Contains(candidates[1].Reason, "同平台账号一致") {
		t.Fatalf("platform exact match ordering: %+v", candidates)
	}
	foreign := contactTestCustomer(t, s, ctx, 2, "张小姐")
	if len(contactTestLoad(t, s, ctx, 1, foreign)) != 0 {
		t.Fatal("tenant contact load leaked")
	}
	// Reuse goes through the public path without making an LLM call.
	image := "data:image/png;base64,aW50ZWdyYXRpb24="
	hash := fmt.Sprintf("%x", sha256.Sum256([]byte(image)))
	unbound := contactTestSource(t, s, ctx, 1, nil, `{"platform":"抖音","nickname":"爱生活"}`)
	if _, err := s.pool.Exec(ctx, `UPDATE patient_conversation_sources SET content_hash=$1 WHERE id=$2`, hash, unbound); err != nil {
		t.Fatal(err)
	}
	reused, err := s.AnalyzeScreenshot(ctx, ScreenshotAnalyzeRequest{TenantID: 1, EmployeeID: 1, CustomerID: &id, ImageData: image})
	if err != nil || !reused.Reused || reused.MatchStatus != "confirmed" {
		t.Fatalf("reuse failed: %+v %v", reused, err)
	}
	if len(contactTestLoad(t, s, ctx, 1, id)) != 2 {
		t.Fatal("reused screenshot contact was not saved")
	}
	_, err = s.AnalyzeScreenshot(ctx, ScreenshotAnalyzeRequest{TenantID: 1, EmployeeID: 1, CustomerID: &other, ImageData: image})
	if err != ErrScreenshotCustomerConflict {
		t.Fatalf("reuse reassigned source: %v", err)
	}
}

func TestAddNicknameOnlyContactIntegration(t *testing.T) {
	s, ctx := contactTestService(t)
	id := contactTestCustomer(t, s, ctx, 1, "张三")
	cs := customer.NewService(customer.NewStore(s.pool))
	nickname := " 张小姐 "
	added, err := cs.AddCustomerIdentity(ctx, id, customer.AddIdentityRequest{Channel: "小红书", Nickname: &nickname})
	if err != nil {
		t.Fatal(err)
	}
	if added.ChannelID != "" || added.Nickname == nil || *added.Nickname != "张小姐" {
		t.Fatalf("nickname-only add fabricated an account: %+v", added)
	}
	if _, err := cs.AddCustomerIdentity(ctx, id, customer.AddIdentityRequest{Channel: "小红书"}); err == nil {
		t.Fatal("empty contact accepted")
	}
}

func TestContactProjectionsAndSearchIntegration(t *testing.T) {
	s, ctx := contactTestService(t)
	id := contactTestCustomer(t, s, ctx, 1, "张三")
	for _, payload := range []string{`{"platform":"微信","account":"WX-1","nickname":"张小姐"}`, `{"platform":"小红书","nickname":"张小姐爱生活","patient_question":"咨询产品"}`} {
		source := contactTestSource(t, s, ctx, 1, nil, payload)
		if err := s.ConfirmScreenshot(ctx, 1, 1, source, id); err != nil {
			t.Fatal(err)
		}
	}
	cs := customer.NewService(customer.NewStore(s.pool))
	tenant := int64(1)
	for _, keyword := range []string{"张小姐爱生活", "WX-1"} {
		list, total, err := cs.ListCustomers(ctx, customer.CustomerListRequest{TenantID: &tenant, Search: &keyword, Page: 1, PageSize: 20})
		if err != nil || total != 1 || len(list) != 1 || len(list[0].Identities) != 2 {
			t.Fatalf("list/search %s: %+v %d %v", keyword, list, total, err)
		}
		candidates, err := cs.SearchCandidates(ctx, 1, keyword, 8)
		if err != nil || len(candidates) != 1 || candidates[0].ID != id {
			t.Fatalf("typeahead/search %s: %+v %v", keyword, candidates, err)
		}
	}
	detail, err := cs.GetCustomerByID(ctx, id)
	if err != nil || len(detail.Identities) != 2 {
		t.Fatalf("detail: %+v %v", detail, err)
	}
	plan := &Plan{ID: 1, TenantID: 1, PlanType: "status", Scenario: "invite_visit"}
	preview, err := s.buildPlanCandidates(ctx, plan)
	if err != nil || len(preview.Candidates) != 1 || len(preview.Candidates[0].Contacts) != 2 {
		t.Fatalf("plan preview: %+v %v", preview, err)
	}
	if _, err := s.pool.Exec(ctx, `INSERT INTO patient_ops_plan_members(tenant_id,plan_id,customer_id) VALUES(1,1,$1)`, id); err != nil {
		t.Fatal(err)
	}
	members, err := s.loadPlanMembers(ctx, 1, 1)
	if err != nil || len(members) != 1 || len(members[0].Contacts) != 2 {
		t.Fatalf("members: %+v %v", members, err)
	}
	if _, err := s.pool.Exec(ctx, `INSERT INTO patient_operation_tasks(tenant_id,customer_id,employee_id) VALUES(1,$1,1)`, id); err != nil {
		t.Fatal(err)
	}
	tasks, err := s.ListCustomerTasks(ctx, 1, id)
	if err != nil || len(tasks) != 1 || len(tasks[0].Contacts) != 2 {
		t.Fatalf("customer tasks: %+v %v", tasks, err)
	}
	all, total, err := s.ListPatientTasks(ctx, 1, 1, 1, 20, true, "全部", "")
	if err != nil || total != 1 || len(all) != 1 || len(all[0].Contacts) != 2 {
		t.Fatalf("task list: %+v %d %v", all, total, err)
	}
}
