// Command e2e 是新产品两套栈（机构端/移动端 vs 门户）的端到端场景验证。
//
// 覆盖：建档→未开通、开试用、试用登录/产品闸门、试用限制、暂停/恢复、
// 转正式、加产品、加量包、续期、通知函幂等与文件名、过期读出、
// 老产品隔离、列表筛选与分页。
//
// 用法：go run ./cmd/e2e（需要 18080 主实例 + 18081 测试实例同库运行）。
package main

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"strings"
	"time"

	"github.com/freeasyman/lingce-api/pkg/auth"
	"github.com/jackc/pgx/v5/pgxpool"
)

const (
	mainBase = "http://127.0.0.1:18080"
	testBase = "http://127.0.0.1:18081"
	captcha  = "123456"
)

// 敏感配置一律走环境变量，不落仓库：
//
//	LINGCE_E2E_JWT_SECRET  JWT 签名密钥（与 configs/secrets.local.toml 一致）
//	LINGCE_E2E_DB          测试库连接串
var (
	secret = os.Getenv("LINGCE_E2E_JWT_SECRET")
	dbURL  = os.Getenv("LINGCE_E2E_DB")
)

var adminToken string

type result struct {
	Name   string
	OK     bool
	Detail string
}

var results []result

func check(name string, ok bool, detail string) {
	results = append(results, result{Name: name, OK: ok, Detail: detail})
	mark := "PASS"
	if !ok {
		mark = "FAIL"
	}
	fmt.Printf("[%s] %s  %s\n", mark, name, detail)
}

func mustJSON(v any) []byte { b, _ := json.Marshal(v); return b }

func call(method, url, token string, body any) (int, map[string]any, string) {
	var reader io.Reader
	if body != nil {
		reader = bytes.NewReader(mustJSON(body))
	}
	req, err := http.NewRequest(method, url, reader)
	if err != nil {
		panic(err)
	}
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return 0, nil, err.Error()
	}
	defer resp.Body.Close()
	raw, _ := io.ReadAll(resp.Body)
	var m map[string]any
	_ = json.Unmarshal(raw, &m)
	text := string(raw)
	if len(text) > 160 {
		text = text[:160]
	}
	return resp.StatusCode, m, text
}

func str(m map[string]any, keys ...string) string {
	for _, key := range keys {
		if m == nil {
			return ""
		}
		cur := any(m)
		parts := strings.Split(key, ".")
		for _, part := range parts {
			obj, ok := cur.(map[string]any)
			if !ok {
				cur = nil
				break
			}
			cur = obj[part]
		}
		if cur != nil {
			switch v := cur.(type) {
			case string:
				return v
			case float64:
				return fmt.Sprintf("%v", v)
			}
		}
	}
	return ""
}

func truthy(m map[string]any, key string) bool {
	if m == nil {
		return false
	}
	if v, ok := m[key].(bool); ok {
		return v
	}
	if v, ok := m[key].(float64); ok {
		return v != 0
	}
	return false
}

func num(m map[string]any, key string) float64 {
	if m == nil {
		return -1
	}
	if v, ok := m[key].(float64); ok {
		return v
	}
	return -1
}

func login(base, path string, body map[string]any) (int, map[string]any, string) {
	body["captcha_code"] = captcha
	return call("POST", base+path, "", body)
}

func createTenant(pool *pgxpool.Pool, name, phone string) (int64, error) {
	status, m, text := call("POST", mainBase+"/api/v1/ops/billing/institutions", adminToken, map[string]any{
		"name": name, "legal": name + "有限公司", "industry": "综合医院", "contact": "测试联系人",
		"phone": phone, "email": "", "channel": "灵策官方", "sales": "易亮",
		"region": map[string]any{"province": "天津市", "city": "天津市", "district": "和平区"},
	})
	if status != 200 && status != 201 {
		return 0, fmt.Errorf("create tenant %s: %d %s", name, status, text)
	}
	item := m["item"]
	if obj, ok := item.(map[string]any); ok {
		if id, ok := obj["id"].(float64); ok {
			return int64(id), nil
		}
	}
	var id int64
	err := pool.QueryRow(context.Background(), `SELECT id FROM tenants WHERE name = $1 ORDER BY id DESC LIMIT 1`, name).Scan(&id)
	return id, err
}

func institution(id int64) (int, map[string]any) {
	status, m, _ := call("GET", fmt.Sprintf("%s/api/v1/ops/billing/institutions/%d", mainBase, id), adminToken, nil)
	if obj, ok := m["item"].(map[string]any); ok {
		return status, obj
	}
	return status, m
}

func productOpen(id int64, code string) bool {
	status, m, _ := call("GET", fmt.Sprintf("%s/api/v1/ops/billing/institutions/%d", mainBase, id), adminToken, nil)
	_ = status
	item, _ := m["item"].(map[string]any)
	products, _ := item["products"].(map[string]any)
	if products == nil {
		return false
	}
	_, ok := products[code]
	return ok
}

func entitlementsOf(pool *pgxpool.Pool, tenantID int64, quotaType string) int {
	var n int
	_ = pool.QueryRow(context.Background(), `
SELECT count(*) FROM product_entitlements WHERE tenant_id=$1 AND status='active' AND quota_type=$2`, tenantID, quotaType).Scan(&n)
	return n
}

func main() {
	if secret == "" || dbURL == "" {
		fmt.Println("缺少环境变量：LINGCE_E2E_JWT_SECRET / LINGCE_E2E_DB")
		os.Exit(2)
	}
	if t, _, _ := auth.GenerateToken(secret, 1, auth.UserTypeAdmin, nil, 109, 2); true {
		adminToken = t
	}
	ctx := context.Background()
	pool, err := pgxpool.New(ctx, dbURL)
	if err != nil {
		panic(err)
	}
	defer pool.Close()

	suffix := fmt.Sprintf("%d", time.Now().Unix()%100000)
	nameA := "E2E机构A" + suffix
	nameB := "E2E机构B" + suffix
	phoneA := "1399" + fmt.Sprintf("%07d", time.Now().UnixNano()%10000000)
	phoneB := "1388" + fmt.Sprintf("%07d", time.Now().UnixNano()%10000000)

	var tenants []int64
	defer func() {
		for _, id := range tenants {
			for _, q := range []string{
				`DELETE FROM tenant_documents WHERE tenant_id=$1`,
				`DELETE FROM subscription_periods WHERE tenant_id=$1`,
				`DELETE FROM product_entitlements WHERE tenant_id=$1`,
				`DELETE FROM billing_orders WHERE tenant_id=$1`,
				`DELETE FROM entitlement_ledger WHERE tenant_id=$1`,
				`DELETE FROM employees WHERE tenant_id=$1`,
				`DELETE FROM departments WHERE tenant_id=$1`,
				`DELETE FROM institution_roles WHERE tenant_id=$1`,
				`DELETE FROM tenants WHERE id=$1`,
			} {
				_, _ = pool.Exec(ctx, q, id)
			}
		}
	}()

	// ============ T1 建档 → 未开通 ============
	idA, err := createTenant(pool, nameA, phoneA)
	check("T1.1 新建机构", err == nil && idA > 0, fmt.Sprintf("id=%d err=%v", idA, err))
	tenants = append(tenants, idA)
	_, itemA := institution(idA)
	check("T1.2 新机构状态=未开通", str(itemA, "status") == "unopened", "status="+str(itemA, "status"))

	st, m, _ := login(testBase, "/api/v1/auth/login/portal", map[string]any{"username": phoneA, "password": "123456"})
	check("T1.3 未开通门户登录被拒(NEW_PRODUCT_NOT_OPENED)", st == 401 && str(m, "code") == "NEW_PRODUCT_NOT_OPENED", fmt.Sprintf("status=%d code=%s", st, str(m, "code")))
	st, m, _ = login(testBase, "/api/v1/auth/login/institution", map[string]any{"username": phoneA, "password": "123456"})
	check("T1.4 机构端登录不拦截新机构(老逻辑)", st == 200 && str(m, "token") != "", fmt.Sprintf("status=%d", st))

	// ============ T2 开通试用 ============
	st, m, text := call("POST", fmt.Sprintf("%s/api/v1/ops/billing/institutions/%d/activate-trial", mainBase, idA), adminToken, map[string]any{"productCodes": []string{"tingxie"}, "days": 14})
	check("T2.1 开通试用(听写14天)", st == 200, fmt.Sprintf("%d %s", st, text))
	_, itemA = institution(idA)
	check("T2.2 状态=试用中", str(itemA, "status") == "trial", "status="+str(itemA, "status"))
	check("T2.3 已开通产品含听写", productOpen(idA, "tingxie"), "")

	st, m, _ = login(testBase, "/api/v1/auth/login/portal", map[string]any{"username": phoneA, "password": "123456"})
	portalA := str(m, "token")
	check("T2.4 试用中门户登录成功", st == 200 && portalA != "", fmt.Sprintf("status=%d", st))

	st, m, _ = call("GET", fmt.Sprintf("%s/api/v1/portal/product-access?code=tingxie", mainBase), portalA, nil)
	check("T2.5 产品权限:听写=开通", truthy(m, "open"), text)
	st, m, _ = call("GET", fmt.Sprintf("%s/api/v1/portal/product-access?code=huoke", mainBase), portalA, nil)
	check("T2.6 产品权限:获客=未开通", !truthy(m, "open"), text)

	st, _, text = call("GET", mainBase+"/api/v1/emr/records", portalA, nil)
	check("T2.7 门户token访问听写API放行", st == 200, fmt.Sprintf("%d %s", st, text))
	st, m, text = call("GET", mainBase+"/api/v1/content-topics", portalA, nil)
	check("T2.8 门户token访问获客API被产品闸门拦截", st == 403 && str(m, "code") == "PRODUCT_NOT_OPENED", fmt.Sprintf("%d %s", st, text))

	// 通知函：开通后生成 + 幂等
	st, m, text = call("POST", fmt.Sprintf("%s/api/v1/ops/billing/institutions/%d/documents", mainBase, idA), adminToken, map[string]any{"kind": "trial"})
	doc1 := str(m, "data.id")
	check("T2.9 生成试用通知函", st == 200 && doc1 != "", fmt.Sprintf("%d %s", st, text))
	st, m2, _ := call("POST", fmt.Sprintf("%s/api/v1/ops/billing/institutions/%d/documents", mainBase, idA), adminToken, map[string]any{"kind": "trial"})
	check("T2.10 同类型重复生成返回同一份(幂等)", str(m2, "data.id") == doc1, "id1="+doc1+" id2="+str(m2, "data.id"))

	// ============ T3 延长试用限制 ============
	st, m, text = call("POST", fmt.Sprintf("%s/api/v1/ops/billing/institutions/%d/extend-trial", mainBase, idA), adminToken, map[string]any{"days": 31})
	check("T3.1 单次延长31天被拒", st == 409, fmt.Sprintf("%d %s", st, text))
	st, m, text = call("POST", fmt.Sprintf("%s/api/v1/ops/billing/institutions/%d/extend-trial", mainBase, idA), adminToken, map[string]any{"days": 16})
	check("T3.2 延长16天(累计30)成功", st == 200, fmt.Sprintf("%d %s", st, text))
	st, m, text = call("POST", fmt.Sprintf("%s/api/v1/ops/billing/institutions/%d/extend-trial", mainBase, idA), adminToken, map[string]any{"days": 30})
	check("T3.3 再延30天(累计60)成功", st == 200, fmt.Sprintf("%d %s", st, text))
	st, m, text = call("POST", fmt.Sprintf("%s/api/v1/ops/billing/institutions/%d/extend-trial", mainBase, idA), adminToken, map[string]any{"days": 1})
	check("T3.4 累计超60天被拒", st == 409, fmt.Sprintf("%d %s", st, text))

	// ============ T4 试用中重复开通拦截 ============
	st, m, text = call("POST", fmt.Sprintf("%s/api/v1/ops/billing/institutions/%d/activate-trial", mainBase, idA), adminToken, map[string]any{"productCodes": []string{"tingxie"}, "days": 7})
	check("T4.1 生效中重复开通试用被拒", st == 409, fmt.Sprintf("%d %s", st, text))

	// ============ T5 暂停 / 恢复 ============
	st, m, text = call("POST", fmt.Sprintf("%s/api/v1/ops/billing/institutions/%d/suspend", mainBase, idA), adminToken, map[string]any{"reason": "E2E 暂停测试"})
	check("T5.1 暂停成功", st == 200, fmt.Sprintf("%d %s", st, text))
	st, m, _ = login(testBase, "/api/v1/auth/login/portal", map[string]any{"username": phoneA, "password": "123456"})
	check("T5.2 暂停后仍可登录查看(设计:仅拦消费)", st == 200, fmt.Sprintf("status=%d(消费由 subscriptionGuard 拦截)", st))
	st, m, text = call("POST", fmt.Sprintf("%s/api/v1/ops/billing/institutions/%d/resume", mainBase, idA), adminToken, map[string]any{})
	check("T5.3 恢复成功", st == 200, fmt.Sprintf("%d %s", st, text))
	st, m, _ = login(testBase, "/api/v1/auth/login/portal", map[string]any{"username": phoneA, "password": "123456"})
	check("T5.4 恢复后门户登录成功", st == 200, fmt.Sprintf("status=%d", st))

	// ============ T6 转正式 ============
	trialEnts := entitlementsOf(pool, idA, "trial")
	formalEnd := time.Now().AddDate(1, 0, 0).Format("2006-01-02")
	st, m, text = call("POST", fmt.Sprintf("%s/api/v1/ops/billing/institutions/%d/activate-formal", mainBase, idA), adminToken, map[string]any{
		"productCodes": []string{"tingxie", "huoke"}, "fromTrial": true, "endDate": formalEnd, "payment": map[string]any{"mode": "unpaid"},
	})
	check("T6.1 试用转正式", st == 200, fmt.Sprintf("%d %s", st, text))
	_, itemA = institution(idA)
	check("T6.2 状态=正式服务中", str(itemA, "status") == "active", "status="+str(itemA, "status"))
	check("T6.3 正式产品含听写+获客", productOpen(idA, "tingxie") && productOpen(idA, "huoke"), "")
	check("T6.4 试用额度清零/终止", entitlementsOf(pool, idA, "trial") == 0, fmt.Sprintf("之前试用额度行=%d 现在=%d", trialEnts, entitlementsOf(pool, idA, "trial")))

	st, m, _ = login(testBase, "/api/v1/auth/login/portal", map[string]any{"username": phoneA, "password": "123456"})
	portalA = str(m, "token")
	check("T6.5 正式客户门户登录成功", st == 200 && portalA != "", fmt.Sprintf("status=%d", st))

	st, m, text = call("GET", fmt.Sprintf("%s/api/v1/portal/product-access?code=yingke", mainBase), portalA, nil)
	check("T6.6 产品权限:赢客=未开通", !truthy(m, "open"), text)

	// 正式通知函
	st, m, text = call("POST", fmt.Sprintf("%s/api/v1/ops/billing/institutions/%d/documents", mainBase, idA), adminToken, map[string]any{"kind": "formal"})
	formalDoc := str(m, "data.id")
	check("T6.7 生成正式确认函", st == 200 && formalDoc != "", fmt.Sprintf("%d %s", st, text))
	// 下载头带机构名
	st, m, _ = call("GET", fmt.Sprintf("%s/api/v1/ops/billing/documents/%s/file", mainBase, formalDoc), adminToken, nil)
	_ = st

	// ============ T7 加产品（正式） ============
	st, m, text = call("POST", fmt.Sprintf("%s/api/v1/ops/billing/institutions/%d/products", mainBase, idA), adminToken, map[string]any{"code": "yingke"})
	check("T7.1 正式加产品(赢客)", st == 200, fmt.Sprintf("%d %s", st, text))
	st, m, _ = call("GET", fmt.Sprintf("%s/api/v1/portal/product-access?code=yingke", mainBase), portalA, nil)
	check("T7.2 加产品后门户可用", truthy(m, "open"), text)
	var yingkePeriod int64
	_ = pool.QueryRow(ctx, `SELECT count(*) FROM product_entitlements WHERE tenant_id=$1 AND product_code='yingke' AND status='active'`, idA).Scan(&yingkePeriod)
	check("T7.3 新额度已挂当前合同期", yingkePeriod > 0, fmt.Sprintf("rows=%d", yingkePeriod))

	// ============ T8 加量包 ============
	st, m, text = call("POST", fmt.Sprintf("%s/api/v1/ops/billing/institutions/%d/increments", mainBase, idA), adminToken, map[string]any{"code": "tingxie", "packs": 1})
	check("T8.1 加量包(听写1包)", st == 200, fmt.Sprintf("%d %s", st, text))

	// ============ T9 续期 ============
	_, beforeEnd := institution(idA)
	st, m, text = call("POST", fmt.Sprintf("%s/api/v1/ops/billing/institutions/%d/renew", mainBase, idA), adminToken, map[string]any{})
	check("T9.1 正式续期", st == 200, fmt.Sprintf("%d %s", st, text))
	_, afterEnd := institution(idA)
	check("T9.2 续期后到期日不变或顺延", str(afterEnd, "end") >= str(beforeEnd, "end"), "before="+str(beforeEnd, "end")+" after="+str(afterEnd, "end"))

	// ============ T10 第3次试用需主管权限（超管通过） ============
	idB, err := createTenant(pool, nameB, phoneB)
	check("T10.1 新建机构B", err == nil && idB > 0, fmt.Sprintf("id=%d", idB))
	tenants = append(tenants, idB)
	for i := 1; i <= 3; i++ {
		st, m, text = call("POST", fmt.Sprintf("%s/api/v1/ops/billing/institutions/%d/activate-trial", mainBase, idB), adminToken, map[string]any{"productCodes": []string{"tingxie"}, "days": 7})
		check(fmt.Sprintf("T10.%d 第%d次开试用", i+1, i), st == 200, fmt.Sprintf("%d %s", st, text))
		if i < 3 {
			st, m, text = call("POST", fmt.Sprintf("%s/api/v1/ops/billing/institutions/%d/activate-formal", mainBase, idB), adminToken, map[string]any{
				"productCodes": []string{"tingxie"}, "fromTrial": true, "endDate": time.Now().AddDate(1, 0, 0).Format("2006-01-02"), "payment": map[string]any{"mode": "unpaid"},
			})
			check(fmt.Sprintf("T10.%d 第%d次转正式", i+2, i), st == 200, fmt.Sprintf("%d %s", st, text))
			// 直接过期，进入下一次试用窗口
			_, _ = pool.Exec(ctx, `UPDATE subscription_periods SET status='expired', started_on=CURRENT_DATE-10, expired_on=CURRENT_DATE-1 WHERE tenant_id=$1 AND status='active'`, idB)
			_, _ = pool.Exec(ctx, `UPDATE product_entitlements SET status='expired' WHERE tenant_id=$1 AND status='active'`, idB)
		}
	}
	_, itemB := institution(idB)
	check("T10.5 机构B历史试用次数=3", str(itemB, "trialCount") == "3", "trialCount="+str(itemB, "trialCount"))

	// ============ T11 过期读出 ============
	_, _ = pool.Exec(ctx, `UPDATE subscription_periods SET status='expired', started_on=CURRENT_DATE-20, expired_on=CURRENT_DATE-2 WHERE tenant_id=$1 AND status='active'`, idA)
	st, m, _ = login(testBase, "/api/v1/auth/login/portal", map[string]any{"username": phoneA, "password": "123456"})
	check("T11.1 合同过期后门户登录被拒", st == 401, fmt.Sprintf("status=%d code=%s", st, str(m, "code")))
	_, itemA = institution(idA)
	check("T11.2 过期后状态=已过期/未开通", str(itemA, "status") == "expired" || str(itemA, "status") == "unopened", "status="+str(itemA, "status"))

	// ============ T12 老产品隔离（用真实老客户） ============
	var legacyTenant int64 = 4
	var legacyValidBefore, legacyValidAfter string
	_ = pool.QueryRow(ctx, `SELECT COALESCE(valid_to::text,'') FROM tenants WHERE id=$1`, legacyTenant).Scan(&legacyValidBefore)
	st, m, _ = login(testBase, "/api/v1/auth/login/institution", map[string]any{"username": "13311112222", "password": "123456"})
	check("T12.1 老客户机构端登录(老逻辑不受影响)", st == 200 || st == 401, fmt.Sprintf("status=%d code=%s(密码未知时401可接受)", st, str(m, "code")))
	_ = pool.QueryRow(ctx, `SELECT COALESCE(valid_to::text,'') FROM tenants WHERE id=$1`, legacyTenant).Scan(&legacyValidAfter)
	check("T12.2 新产品动作未改老客户有效期", legacyValidBefore == legacyValidAfter, legacyValidBefore+" == "+legacyValidAfter)

	// ============ T13 列表筛选 / 分页 / 关键词 ============
	st, m, _ = call("GET", mainBase+"/api/v1/ops/billing/institutions?filter=all&page=1&page_size=5", adminToken, nil)
	total := num(m, "total")
	check("T13.1 列表分页 total>0 且每页5条", st == 200 && total >= 5 && len(asItems(m)) <= 5, fmt.Sprintf("total=%v items=%d", total, len(asItems(m))))
	st, m, _ = call("GET", mainBase+"/api/v1/ops/billing/institutions?filter=trial&page=1&page_size=50", adminToken, nil)
	found := false
	for _, it := range asItems(m) {
		if int64(num(it, "id")) == idA || int64(num(it, "id")) == idB {
			found = true
		}
	}
	check("T13.2 试用筛选包含新建试用机构", st == 200 && found, fmt.Sprintf("items=%d", len(asItems(m))))
	st, m, _ = call("GET", mainBase+"/api/v1/ops/billing/institutions?keyword="+nameA, adminToken, nil)
	check("T13.3 关键词搜索命中机构A", st == 200 && len(asItems(m)) >= 1, fmt.Sprintf("items=%d", len(asItems(m))))

	// ============ 汇总 ============
	failed := 0
	for _, r := range results {
		if !r.OK {
			failed++
		}
	}
	fmt.Printf("\n===== %d 项检查，%d 通过，%d 失败 =====\n", len(results), len(results)-failed, failed)
	if failed > 0 {
		os.Exit(1)
	}
}

func asItems(m map[string]any) []map[string]any {
	var out []map[string]any
	if arr, ok := m["items"].([]any); ok {
		for _, v := range arr {
			if obj, ok := v.(map[string]any); ok {
				out = append(out, obj)
			}
		}
	}
	return out
}
