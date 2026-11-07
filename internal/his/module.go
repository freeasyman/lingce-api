package his

import (
	"net/http"

	"github.com/freeasyman/lingce-api/internal/emrrecord"
	"github.com/jackc/pgx/v5/pgxpool"
)

// Module 是 HIS 域的最小装配器。
type Module struct {
	handler *Handler
}

// NewModule 创建 HIS 模块；emr 为共享病历域服务（HIS 复用其病历生命周期）。
func NewModule(pool *pgxpool.Pool, emr *emrrecord.Service) *Module {
	return &Module{handler: NewHandler(NewService(NewStore(pool), emr))}
}

// RegisterRoutes 注册 HIS 路由。
func (m *Module) RegisterRoutes(mux *http.ServeMux, jwtSecret, internalToken string) {
	m.handler.RegisterRoutes(mux, jwtSecret, internalToken)
}
