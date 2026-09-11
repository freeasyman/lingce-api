package complianceguard

import (
	"net/http"

	"github.com/freeasyman/lingce-api/pkg/llmgateway"
	"github.com/jackc/pgx/v5/pgxpool"
)

// Module 是合规卫士 API 的装配器。
//
// 署名：Codex，合规卫士开发 Agent
// 时间：2026-09-11
type Module struct {
	handler *Handler
}

// NewModule 创建合规卫士模块，并保留现有调试接口。
//
// 署名：Codex，合规卫士开发 Agent
// 时间：2026-09-11
func NewModule(pool *pgxpool.Pool, llm *llmgateway.Client) *Module {
	service := NewService(pool, llm)
	return &Module{handler: NewHandler(service)}
}

// RegisterRoutes 注册正式内部接口和现有登录调试接口。
//
// 署名：Codex，合规卫士开发 Agent
// 时间：2026-09-11
func (m *Module) RegisterRoutes(mux *http.ServeMux, jwtSecret, internalToken string) {
	m.handler.RegisterRoutes(mux, jwtSecret, internalToken)
}
