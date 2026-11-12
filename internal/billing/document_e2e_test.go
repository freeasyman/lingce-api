package billing_test

// 通知函文档的集成测试（真实 PostgreSQL）：
//
//	BILLING_TEST_DATABASE_URL='postgresql://yiliiang@127.0.0.1:5432/lingce_billing_test?sslmode=disable' \
//	  go test ./internal/billing/ -run TestInstitutionDocumentFlow -count=1 -v
//
// 覆盖：未开通 400 → 试用开通 → 生成 PDF（单页、>20KB）→ 重复生成保留旧版并递增编号
// → 列表新→旧 → 下载字节与落库一致 → 越权 403 → 不存在 404。

import (
	"bytes"
	"errors"
	"fmt"
	"regexp"
	"strings"
	"testing"
	"time"

	"github.com/freeasyman/lingce-api/internal/billing"
)

var e2ePagePattern = regexp.MustCompile(`/Type\s*/Page\b`)

func TestInstitutionDocumentFlow(t *testing.T) {
	env := newBillingTestEnv(t)
	ctx := env.ctx

	var documentsTable bool
	if err := env.pool.QueryRow(ctx, `SELECT to_regclass('public.tenant_documents') IS NOT NULL`).Scan(&documentsTable); err != nil {
		t.Fatalf("检查 tenant_documents 失败: %v", err)
	}
	if !documentsTable {
		t.Fatalf("tenant_documents 未建表，请先执行 20261112_010 迁移")
	}

	channelName := "文档渠道-" + env.suffix
	salesName := "文档销售-" + env.suffix
	var channelID int64
	if err := env.pool.QueryRow(ctx, `
INSERT INTO ops_organizations (name, type, status) VALUES ($1, 'agency', 'active') RETURNING id`, channelName).Scan(&channelID); err != nil {
		t.Fatalf("创建渠道失败: %v", err)
	}
	env.channelIDs = append(env.channelIDs, channelID)
	env.seedSales(channelID, salesName)
	institution := env.createInstitution("通知函测试眼科", "眼科", []string{"眼科"}, channelName, salesName)
	documents := billing.NewDocumentService(env.svc, billing.NewStore(env.pool))

	// 未开通：生成通知函返回 400（ValidationError），而不是 500。
	if _, err := documents.Generate(ctx, institution.ID, env.adminID, billing.GenerateDocumentRequest{Kind: "trial"}); err == nil {
		t.Fatalf("未开通机构生成通知函应当失败")
	} else {
		var validation *billing.ValidationError
		if !errors.As(err, &validation) {
			t.Fatalf("未开通应返回 ValidationError（HTTP 400），实际 %T: %v", err, err)
		}
		if !strings.Contains(err.Error(), "尚未开通") {
			t.Fatalf("错误文案应说明未开通: %v", err)
		}
	}

	// 非法 kind 也是 400。
	if _, err := documents.Generate(ctx, institution.ID, env.adminID, billing.GenerateDocumentRequest{Kind: "unknown"}); err == nil {
		t.Fatalf("非法 kind 应当失败")
	}

	trial, err := env.svc.ActivateTrial(ctx, institution.ID, billing.TrialActivationRequest{
		ProductCodes: []string{"tingxie", "hegui"}, Days: 14,
	}, env.adminID)
	if err != nil {
		t.Fatalf("试用开通失败: %v", err)
	}
	if !trial.Open {
		t.Fatalf("试用开通后机构应为已开通")
	}

	// 生成第一份试用通知函。
	first, err := documents.Generate(ctx, institution.ID, env.adminID, billing.GenerateDocumentRequest{Kind: "trial"})
	if err != nil {
		t.Fatalf("生成试用通知函失败: %v", err)
	}
	year := time.Now().Year()
	if first.DocType != billing.DocTypeTrialNotice || first.Kind != "trial" {
		t.Fatalf("文档类型不对: %+v", first)
	}
	if !strings.HasPrefix(first.DocNo, fmt.Sprintf("LC-TR-%d-", year)) {
		t.Fatalf("试用编号格式不对: %s", first.DocNo)
	}
	if first.FileSize <= 20*1024 {
		t.Fatalf("PDF 体积 %d 字节，字体疑似没有内嵌", first.FileSize)
	}
	if first.GeneratedBy == "" {
		t.Fatalf("操作人不应为空")
	}

	// 再生成一次：编号递增、旧记录保留。
	second, err := documents.Generate(ctx, institution.ID, env.adminID, billing.GenerateDocumentRequest{Kind: "trial"})
	if err != nil {
		t.Fatalf("第二次生成失败: %v", err)
	}
	if second.ID == first.ID || second.DocNo == first.DocNo {
		t.Fatalf("每次生成都应是新记录: first=%+v second=%+v", first, second)
	}
	var firstSeq, secondSeq int
	if _, err := fmt.Sscanf(first.DocNo[strings.LastIndex(first.DocNo, "-")+1:], "%d", &firstSeq); err != nil {
		t.Fatalf("解析编号失败: %v", err)
	}
	if _, err := fmt.Sscanf(second.DocNo[strings.LastIndex(second.DocNo, "-")+1:], "%d", &secondSeq); err != nil {
		t.Fatalf("解析编号失败: %v", err)
	}
	if secondSeq != firstSeq+1 {
		t.Fatalf("编号应递增: %s -> %s", first.DocNo, second.DocNo)
	}

	// 正式确认函：同一机构也能生成，编号走 LC-SV 前缀。
	formal, err := documents.Generate(ctx, institution.ID, env.adminID, billing.GenerateDocumentRequest{Kind: "formal"})
	if err != nil {
		t.Fatalf("生成服务开通确认函失败: %v", err)
	}
	if formal.DocType != billing.DocTypeServiceConfirm || !strings.HasPrefix(formal.DocNo, fmt.Sprintf("LC-SV-%d-", year)) {
		t.Fatalf("正式文档类型/编号不对: %+v", formal)
	}

	// 列表：新→旧，三条记录都在。
	list, err := documents.List(ctx, institution.ID, env.adminID)
	if err != nil {
		t.Fatalf("列出文档失败: %v", err)
	}
	if list.Total != 3 || len(list.Items) != 3 {
		t.Fatalf("文档列表应有 3 条，实际 %+v", list)
	}
	if list.Items[0].ID != formal.ID {
		t.Fatalf("列表应按生成时间倒序，首条应为最新: %+v", list.Items)
	}
	for _, item := range list.Items {
		if item.GeneratedAt == "" || item.FileSize <= 0 {
			t.Fatalf("列表字段不完整: %+v", item)
		}
	}

	// 下载：字节以 %PDF- 开头、大小与落库一致、单页。
	doc, err := documents.Download(ctx, first.ID, env.adminID)
	if err != nil {
		t.Fatalf("下载文档失败: %v", err)
	}
	if !bytes.HasPrefix(doc.Content, []byte("%PDF-")) {
		t.Fatalf("下载内容不是 PDF")
	}
	if int64(len(doc.Content)) != doc.FileSize || doc.FileSize != first.FileSize {
		t.Fatalf("PDF 大小不一致: len=%d file_size=%d first=%d", len(doc.Content), doc.FileSize, first.FileSize)
	}
	if pages := len(e2ePagePattern.FindAll(doc.Content, -1)); pages != 1 {
		t.Fatalf("PDF 页数 = %d, want 1", pages)
	}

	// 越权：另一个渠道（组织）的运营账号看不到该机构的文档，生成与下载都应 403。
	otherOrgID, otherAdminID := seedOtherOpsContext(env)
	if _, err := documents.Generate(ctx, institution.ID, otherAdminID, billing.GenerateDocumentRequest{Kind: "trial"}); !errors.Is(err, billing.ErrNoPermission) {
		t.Fatalf("越权生成应返回 ErrNoPermission，实际 %v", err)
	}
	if _, err := documents.Download(ctx, first.ID, otherAdminID); !errors.Is(err, billing.ErrNoPermission) {
		t.Fatalf("越权下载应返回 ErrNoPermission，实际 %v", err)
	}
	if _, err := documents.List(ctx, institution.ID, otherAdminID); !errors.Is(err, billing.ErrNoPermission) {
		t.Fatalf("越权列表应返回 ErrNoPermission，实际 %v", err)
	}
	_ = otherOrgID

	// 不存在的文档：404。
	if _, err := documents.Download(ctx, 99_999_999, env.adminID); !errors.Is(err, billing.ErrDocumentNotFound) {
		t.Fatalf("不存在的文档应返回 ErrDocumentNotFound，实际 %v", err)
	}
}

// seedOtherOpsContext 建一个与机构无关的渠道组织 + 运营账号（用于越权断言）。
func seedOtherOpsContext(env *billingTestEnv) (int64, int64) {
	env.t.Helper()
	var orgID int64
	if err := env.pool.QueryRow(env.ctx, `
INSERT INTO ops_organizations (name, type, status) VALUES ($1, 'agency', 'active') RETURNING id`,
		"越权渠道-"+env.suffix).Scan(&orgID); err != nil {
		env.t.Fatalf("创建越权渠道失败: %v", err)
	}
	env.channelIDs = append(env.channelIDs, orgID)
	var adminID int64
	phone := "17" + fmt.Sprintf("%09d", (time.Now().UnixNano()/1000)%1_000_000_000)
	if err := env.pool.QueryRow(env.ctx, `
INSERT INTO operations_admins (name, phone, username, password_hash, org_id, is_active, session_version, created_at, updated_at)
VALUES ($1, $2::varchar, $2::text, 'x', $3, 1, 1, NOW(), NOW())
RETURNING id`, "越权运营-"+env.suffix, phone, orgID).Scan(&adminID); err != nil {
		env.t.Fatalf("创建越权运营账号失败: %v", err)
	}
	env.salesIDs = append(env.salesIDs, adminID)
	return orgID, adminID
}
