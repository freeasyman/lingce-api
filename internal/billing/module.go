package billing

import (
	"context"
	"net/http"
	"time"

	"github.com/freeasyman/lingce-api/internal/tenant"
	"github.com/jackc/pgx/v5/pgxpool"
)

// Module 是计费缴费模块的装配入口，供 cmd/lingce-api/main.go 注册。
type Module struct{ handler *Handler }

// NewModule 用连接池装配 store → service → handler。
//
// 运营端机构能力复用同一套计费 store；建档时通过 tenant.Store 走既有的
// 「建租户 + 建主管理员 + 初始化角色/科室」路径，避免两套建档逻辑漂移。
func NewModule(pool *pgxpool.Pool) *Module {
	store := NewStore(pool)
	service := NewService(store)
	handler := NewHandler(service)
	handler.institutions = NewInstitutionService(service, store, tenant.NewStore(pool))
	return &Module{handler: handler}
}

// RegisterRoutes 注册计费模块的全部 HTTP 路由。
func (m *Module) RegisterRoutes(mux *http.ServeMux, jwtSecret, internalToken string) {
	m.handler.RegisterRoutes(mux, jwtSecret, internalToken)
}

// Service 暴露服务层，供同进程其他模块（如 D 阶段扣量前查余额）直接调用。
func (m *Module) Service() *Service { return m.handler.service }

// StartExpiryWorker 启动每日到期处理（E3）：启动先跑一次，之后每天一次。
// 仓库没有统一调度器，由 main.go 以 goroutine 方式调用。
func (m *Module) StartExpiryWorker(ctx context.Context, interval time.Duration) {
	if m == nil || m.handler == nil || m.handler.service == nil {
		return
	}
	m.handler.service.StartExpiryWorker(ctx, interval)
}
