package security

// 路由 → 租户守卫 静态审计测试（防复发）。
//
// 规则：凡注册了带路径参数（{id}/{code}/...）的接口，其 handler 体内必须包含
// 至少一个租户/身份守卫（直接调用或调用与 handler 同包、自身做守卫的 helper）。
// 路由级 AllowedUserTypes: ["admin"] 视同已守卫（平台运营按设计可跨租户）。
// 新接口漏守卫会直接让本测试失败；确属平台全局资源/服务层已校验的接口需要加入
// exceptions 并写明理由，不能无理由豁免。
//
// 这是静态检查（正则级），不替代运行时校验；新增复杂守卫 helper 时记得在
// guardTokens 里覆盖。

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"runtime"
	"sort"
	"strings"
	"testing"
)

// 直接出现在 handler 体内的守卫令牌。
var guardTokens = []string{
	"RequireSameTenant",
	"RequireTenantID",
	"ResolveScope",
	"claims.TenantID",
	"requireTenant",
	"requireModule",
	"requireOpenClient",
	"assertRecordingAccess",
	"authorizeRecord",
	"authorize(",
	"requireAbility",
	"requirePortalAdmin",
	"requireInstitutionAdminIdentity",
	"requireInstitutionMenuAccess",
	"requireManager",
	"requirePlatformAdmin",
	"requireEMRRecordAccess",
	"requireRecordingOwned",
	"getTaskTenantIDFromClaimsOrQuery",
	"getTaskTenantScope",
	"planTenantAndEmployee",
	"UserTypeAdmin",
	"IsInstitutionAdmin",
}

var routeRe = regexp.MustCompile(`(?s)\{Method:\s*"([A-Z]+)",\s*Path:\s*"([^"]+)",\s*Handler:\s*h\.([A-Za-z0-9_]+).*?\}`)
var handlerRe = regexp.MustCompile(`func \(h \*Handler\) ([A-Za-z0-9_]+)\(`)
var helperRe = regexp.MustCompile(`func (?:\([^)]*\) )?([A-Za-z0-9_]+)\(`)
var adminOnlyRe = regexp.MustCompile(`AllowedUserTypes:\s*\[\]string\{\s*"admin"\s*\}`)

// exceptions 是「服务层/内部信任边界已校验，handler 层无显式守卫」的豁免清单。
// 每一条必须有理由；新增豁免需要评审。
var exceptions = map[string]string{
	"GET /api/v1/recordings/prompts/codes/{code}":               "平台级提示词目录，全员只读",
	"POST /api/v1/recordings/prompts/codes/{code}/actions/test": "平台级提示词调试（handler 内限定 debug 录音范围）",
	"GET /api/v1/frontdesk/knowledge-bases/{kb_type}":           "经 getFrontdeskTenantID 取租户（kb_type 非 id）",
	"PUT /api/v1/frontdesk/knowledge-bases/{kb_type}":           "同上",
	"GET /api/v1/analysis/routes/{id}":                          "内部分析路由管理，仅平台运营使用（见审计文档待复核）",
	"PUT /api/v1/analysis/routes/{id}":                          "同上",
	"POST /api/v1/analysis/routes/{id}/publish":                 "同上",
	"POST /api/v1/analysis/routes/{id}/rollback":                "同上",
	"GET /api/v1/analysis/runs/{id}":                            "同上",
	"GET /api/v1/analysis/runs/{id}/steps":                      "同上",
	"POST /api/v1/employees/learning-tasks/{push_id}/actions/acknowledge": "push 任务按员工 token 关联",
	"GET /api/v1/mobile/recordings/{id}":                        "mobile/service.go:209 校验 TenantID 与 EmployeeID",
	"GET /api/v1/mobile/tasks/{id}":                             "mobile service 校验所属租户与归属员工",
	"GET /api/v1/tenants/{id}/medical-specialties":              "平台级科室字典，无租户维度",
	"POST /api/v1/internal/deal-expert/recordings/{id}/analyze": "X-Internal-Token 服务间信任边界",
}

func repoRoot(t *testing.T) string {
	_, file, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("cannot locate test file")
	}
	dir := filepath.Dir(file) // .../internal/security
	root := filepath.Dir(filepath.Dir(dir))
	if _, err := os.Stat(filepath.Join(root, "go.mod")); err != nil {
		t.Fatalf("repo root not found: %v", err)
	}
	return root
}

type routeDecl struct {
	method, path, handler, file, literal string
}

type handlerBody struct{ file, text string }

func TestRoutesWithPathParamsHaveTenantGuards(t *testing.T) {
	root := repoRoot(t)

	var files []string
	err := filepath.Walk(filepath.Join(root, "internal"), func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		if !info.IsDir() && strings.HasSuffix(path, ".go") && !strings.HasSuffix(path, "_test.go") {
			files = append(files, path)
		}
		return nil
	})
	if err != nil {
		t.Fatalf("walk: %v", err)
	}

	allSource := map[string]string{}
	for _, f := range files {
		b, err := os.ReadFile(f)
		if err != nil {
			t.Fatalf("read %s: %v", f, err)
		}
		allSource[f] = string(b)
	}

	var routes []routeDecl
	for f, src := range allSource {
		for _, m := range routeRe.FindAllStringSubmatch(src, -1) {
			if !strings.Contains(m[2], "{") {
				continue
			}
			routes = append(routes, routeDecl{m[1], m[2], m[3], f, m[0]})
		}
	}

	// helper 函数：自身包含守卫令牌、且与 handler 同包（目录）的函数，调用它视同守卫。
	// 跨包的基础设施函数（middleware.GetUserClaims 等）不算，避免"取一下 claims 就算守卫"。
	helperGuards := map[string]bool{}
	for f, src := range allSource {
		for _, m := range helperRe.FindAllStringSubmatch(src, -1) {
			name := m[1]
			if name == "Register" || name == "RegisterRoutes" {
				continue
			}
			start := strings.Index(src, m[0])
			window := src[start:min(len(src), start+4000)]
			for _, tok := range guardTokens {
				if strings.Contains(window, tok) {
					helperGuards[name+"|"+filepath.Dir(f)] = true
					break
				}
			}
		}
	}

	// handler 实现体。
	bodies := map[string][]handlerBody{}
	for f, src := range allSource {
		idx := 0
		for idx < len(src) {
			loc := handlerRe.FindStringSubmatchIndex(src[idx:])
			if loc == nil {
				break
			}
			name := src[idx+loc[2] : idx+loc[3]]
			start := idx + loc[0]
			end := strings.Index(src[start:], "\nfunc ")
			if end < 0 {
				end = len(src) - start
			}
			bodies[name] = append(bodies[name], handlerBody{f, src[start : start+end]})
			idx = start + 1
		}
	}

	var violations []string
	for _, rt := range routes {
		key := rt.method + " " + rt.path
		if _, exempt := exceptions[key]; exempt {
			continue
		}
		// 路由级 admin-only：平台运营按设计可跨租户，视同已守卫。
		if adminOnlyRe.MatchString(rt.literal) {
			continue
		}
		cands := bodies[rt.handler]
		if len(cands) == 0 {
			violations = append(violations, fmt.Sprintf("%-6s %-58s %s（未找到 handler 实现）", rt.method, rt.path, rt.file))
			continue
		}
		// 同目录实现优先。
		picked := cands[0]
		for _, c := range cands {
			if filepath.Dir(c.file) == filepath.Dir(rt.file) {
				picked = c
				break
			}
		}

		guarded := false
		for _, tok := range guardTokens {
			if strings.Contains(picked.text, tok) {
				guarded = true
				break
			}
		}
		if !guarded {
			dir := filepath.Dir(rt.file)
			for hname := range helperGuards {
				if strings.HasSuffix(hname, "|"+dir) && strings.Contains(picked.text, strings.TrimSuffix(hname, "|"+dir)+"(") {
					guarded = true
					break
				}
			}
		}
		if !guarded {
			violations = append(violations, fmt.Sprintf("%-6s %-58s %-34s %s", rt.method, rt.path, rt.handler, rt.file))
		}
	}

	sort.Strings(violations)
	if len(violations) > 0 {
		t.Fatalf("以下带路径参数的接口缺少租户/身份守卫（修复或补充豁免理由）:\n%s",
			strings.Join(violations, "\n"))
	}
}

func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}
