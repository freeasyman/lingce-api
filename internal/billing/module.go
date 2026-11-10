package billing

import (
	"net/http"

	"github.com/jackc/pgx/v5/pgxpool"
)

// Module 是计费缴费模块的装配入口，供 cmd/lingce-api/main.go 注册。
type Module struct{ handler *Handler }

// NewModule 用连接池装配 store → service → handler。
func NewModule(pool *pgxpool.Pool) *Module {
	return &Module{handler: NewHandler(NewService(NewStore(pool)))}
}

// RegisterRoutes 注册计费模块的全部 HTTP 路由。
func (m *Module) RegisterRoutes(mux *http.ServeMux, jwtSecret, internalToken string) {
	m.handler.RegisterRoutes(mux, jwtSecret, internalToken)
}

// Service 暴露服务层，供同进程其他模块（如 D 阶段扣量前查余额）直接调用。
func (m *Module) Service() *Service { return m.handler.service }
