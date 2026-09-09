package followup

import "net/http"

import "github.com/freeasyman/lingce-api/pkg/llmgateway"
import "github.com/jackc/pgx/v5/pgxpool"

// Module 是随访中心 Worker API 的最小装配器。
// 署名：Codex
// 时间：2026-09-09
type Module struct {
	handler *Handler
}

// NewModule 创建随访模块。
func NewModule(llm *llmgateway.Client, pool *pgxpool.Pool) *Module {
	service := NewService(llm, pool)
	return &Module{handler: NewHandler(service)}
}

// RegisterRoutes 注册随访模块路由。
func (m *Module) RegisterRoutes(mux *http.ServeMux, internalToken string) {
	m.handler.RegisterRoutes(mux, internalToken)
}
