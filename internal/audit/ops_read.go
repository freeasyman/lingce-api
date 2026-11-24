// Package audit 提供运营侧安全审计能力。
package audit

import (
	"context"
	"net"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/freeasyman/lingce-api/internal/middleware"
	"github.com/freeasyman/lingce-api/pkg/auth"
	"github.com/jackc/pgx/v5/pgxpool"
)

// opsReadPIIPrefixes 是需要审计的平台 admin 跨租户读路径。
// 未带 tenant_id 的 admin 读请求不落审计（无明确目标租户）。
var opsReadPIIPrefixes = []string{
	"/api/v1/recordings/",
	"/api/v1/recording-tasks",
	"/api/v1/emr/records",
	"/api/v1/quality/",
	"/api/v1/compliance/check-items",
	"/api/v1/compliance/findings",
	"/api/v1/customers",
	"/api/v1/patient-operations/",
	"/api/v1/deal-expert/",
	"/api/v1/his/",
}

type opsReadEntry struct {
	TenantID  int64
	ActorID   int64
	RoutePath string
	IP        string
	UserAgent string
}

// OpsReadLogger 异步把平台 admin 的跨租户读请求写入 system_action_logs。
type OpsReadLogger struct {
	pool *pgxpool.Pool
	ch   chan opsReadEntry
}

// NewOpsReadLogger 创建审计记录器并启动异步写入协程。
func NewOpsReadLogger(pool *pgxpool.Pool) *OpsReadLogger {
	l := &OpsReadLogger{pool: pool, ch: make(chan opsReadEntry, 2048)}
	go l.worker()
	return l
}

func (l *OpsReadLogger) worker() {
	for entry := range l.ch {
		ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
		_, err := l.pool.Exec(ctx, `
			INSERT INTO system_action_logs (
				tenant_id, actor_id, log_type, action_code, action_name,
				route_path, result, object_type, request_summary, ip_address, user_agent, created_at
			) VALUES ($1, $2, 'ops_read_audit', 'ops.cross_tenant.read', '运营跨租户读取',
				$3, 'success', 'request', $4, $5, $6, NOW())
		`, entry.TenantID, entry.ActorID, entry.RoutePath, map[string]any{"method": "GET"}, entry.IP, entry.UserAgent)
		cancel()
		if err != nil {
			// 审计失败不回滚业务，仅记日志。
			continue
		}
	}
}

func (l *OpsReadLogger) isTargetPath(path string) bool {
	for _, prefix := range opsReadPIIPrefixes {
		if strings.HasPrefix(path, prefix) {
			return true
		}
	}
	return false
}

// Middleware 返回一个审计中间件：仅记录平台 admin 的 GET/HEAD 跨租户读请求。
func (l *OpsReadLogger) Middleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		next.ServeHTTP(w, r)
		if r.Method != http.MethodGet && r.Method != http.MethodHead {
			return
		}
		claims := middleware.GetUserClaims(r.Context())
		if claims == nil || claims.UserType != auth.UserTypeAdmin {
			return
		}
		if claims.TenantID != nil && *claims.TenantID > 0 {
			return // tenant-bound admin 只在本租户内，无需跨租户审计
		}
		if !l.isTargetPath(r.URL.Path) {
			return
		}
		rawTenant := strings.TrimSpace(r.URL.Query().Get("tenant_id"))
		if rawTenant == "" {
			return
		}
		tenantID, err := strconv.ParseInt(rawTenant, 10, 64)
		if err != nil || tenantID <= 0 {
			return
		}
		entry := opsReadEntry{
			TenantID:  tenantID,
			ActorID:   claims.UserID,
			RoutePath: r.URL.Path,
			IP:        clientIP(r),
			UserAgent: r.UserAgent(),
		}
		select {
		case l.ch <- entry:
		default:
			// 队列满：丢弃审计条目，绝不阻塞业务请求。
		}
	})
}

func clientIP(r *http.Request) string {
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		return r.RemoteAddr
	}
	return host
}
