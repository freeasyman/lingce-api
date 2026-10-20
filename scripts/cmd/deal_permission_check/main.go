// 赢客专家权限验收脚本：检查角色分配、数据范围决策与可见数据是否符合预期。
//
//	DATABASE_URL='postgresql://...' go run ./scripts/cmd/deal_permission_check
//	DATABASE_URL='...' go run ./scripts/cmd/deal_permission_check -apply-roles
//
// -apply-roles 会为验收租户 9001 的 9101～9106 补齐角色关联（仅验收数据，可重复执行）。
package main

import (
	"context"
	"flag"
	"fmt"
	"os"
	"sort"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/freeasyman/lingce-api/internal/dealexpert"
)

const tenantID = int64(9001)

type expectation struct {
	employeeID int64
	name       string
	role       string
	visible    int
	allowed    bool
}

var expectations = []expectation{
	{employeeID: 9101, name: "王晓", role: "consultant", visible: 4, allowed: true},
	{employeeID: 9102, name: "陈琳", role: "consultant", visible: 4, allowed: true},
	{employeeID: 9103, name: "赵主管", role: "sales_manager", visible: 8, allowed: true},
	{employeeID: 9104, name: "周院长", role: "director", visible: 8, allowed: true},
	{employeeID: 9105, name: "李护士", role: "followup", visible: 0, allowed: false},
	{employeeID: 9106, name: "无权限员工", role: "staff", visible: 0, allowed: false},
}

func main() {
	applyRoles := flag.Bool("apply-roles", false, "补齐验收租户 9001 的员工角色关联")
	flag.Parse()

	dbURL := os.Getenv("DATABASE_URL")
	if dbURL == "" {
		fmt.Fprintln(os.Stderr, "DATABASE_URL is required")
		os.Exit(2)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
	defer cancel()

	pool, err := pgxpool.New(ctx, dbURL)
	if err != nil {
		fmt.Fprintln(os.Stderr, "connect:", err)
		os.Exit(1)
	}
	defer pool.Close()

	if *applyRoles {
		if err := seedRoles(ctx, pool); err != nil {
			fmt.Fprintln(os.Stderr, "apply roles:", err)
			os.Exit(1)
		}
		fmt.Println("已补齐 9101～9106 的验收角色关联")
	}

	service := dealexpert.NewService(dealexpert.NewStore(pool))
	failures := 0
	fmt.Printf("%-6s %-8s %-14s %-18s %-10s %-10s %s\n", "员工", "姓名", "实际角色", "范围决策", "可见接待", "期望", "结果")
	for _, item := range expectations {
		roles := rolesOf(ctx, pool, item.employeeID)
		rule := dealexpert.EvaluateDealScope(roles)

		visible := 0
		if rule.Allowed {
			var filter *int64
			if !rule.AllEmployees {
				employeeID := item.employeeID
				filter = &employeeID
			}
			overview, err := service.Overview(ctx, tenantID, nil, nil, filter)
			if err != nil {
				fmt.Printf("%-6d %-8s %-14s %-18s %-10s %-10s %s\n", item.employeeID, item.name, joinRoles(roles), rule.Code, "查询失败", item.visible, "FAIL")
				fmt.Fprintln(os.Stderr, "overview:", err)
				failures++
				continue
			}
			visible = overview.TotalEncounters
		}

		ok := rule.Allowed == item.allowed && (!rule.Allowed || visible == item.visible)
		result := "PASS"
		if !ok {
			result = "FAIL"
			failures++
		}
		fmt.Printf("%-6d %-8s %-14s %-18s %-10d %-10d %s\n", item.employeeID, item.name, joinRoles(roles), rule.Code, visible, item.visible, result)
	}

	// 接待归属校验：8001 属于 9101，9102 不应看到。
	owner8001, err := service.EncounterConsultant(ctx, tenantID, 8001)
	if err != nil {
		fmt.Fprintln(os.Stderr, "encounter owner:", err)
		failures++
	} else if owner8001 != 9101 {
		fmt.Printf("接待 8001 归属=%d，期望 9101，FAIL\n", owner8001)
		failures++
	} else {
		fmt.Println("接待归属：8001 → 9101，PASS")
	}

	// 越权访问模拟：咨询师 9102 请求 8001 应被拒绝。
	rule9102 := dealexpert.EvaluateDealScope(rolesOf(ctx, pool, 9102))
	if rule9102.Allowed && !rule9102.AllEmployees && owner8001 != 9102 {
		fmt.Println("越权校验：9102 访问 8001 会被拒绝（负责人不匹配），PASS")
	} else {
		fmt.Println("越权校验：9102 的可见范围无法拦截 8001，FAIL")
		failures++
	}

	if failures > 0 {
		fmt.Printf("权限验收失败：%d 项\n", failures)
		os.Exit(1)
	}
	fmt.Println("权限验收通过")
}

func rolesOf(ctx context.Context, pool *pgxpool.Pool, employeeID int64) []string {
	rows, err := pool.Query(ctx, `
		SELECT DISTINCT lower(trim(role_code))
		FROM institution_employee_roles
		WHERE tenant_id = $1 AND employee_id = $2 AND trim(COALESCE(role_code, '')) <> ''
		ORDER BY 1`, tenantID, employeeID)
	if err != nil {
		return nil
	}
	defer rows.Close()
	roles := make([]string, 0, 2)
	for rows.Next() {
		var code string
		if err := rows.Scan(&code); err == nil {
			roles = append(roles, code)
		}
	}
	sort.Strings(roles)
	return roles
}

func seedRoles(ctx context.Context, pool *pgxpool.Pool) error {
	tx, err := pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback(ctx) }()

	for _, item := range expectations {
		if _, err := tx.Exec(ctx, `
			DELETE FROM institution_employee_roles
			WHERE tenant_id = $1 AND employee_id = $2`, tenantID, item.employeeID); err != nil {
			return err
		}
		if _, err := tx.Exec(ctx, `
			INSERT INTO institution_employee_roles (employee_id, role_id, tenant_id, role_code, source, created_at, updated_at)
			VALUES ($1,
				(SELECT id FROM institution_roles WHERE tenant_id = $2 AND code = $3 LIMIT 1),
				$2, $3, 'deal_acceptance', NOW(), NOW())`,
			item.employeeID, tenantID, item.role); err != nil {
			return err
		}
	}
	return tx.Commit(ctx)
}

func joinRoles(roles []string) string {
	if len(roles) == 0 {
		return "(无)"
	}
	out := roles[0]
	for _, role := range roles[1:] {
		out += "," + role
	}
	return out
}
