package patientops

import (
	"github.com/freeasyman/lingce-api/pkg/llmgateway"
	"github.com/jackc/pgx/v5/pgxpool"
	"net/http"
)

type Module struct{ handler *Handler }

func NewModule(pool *pgxpool.Pool, llm *llmgateway.Client) *Module {
	return &Module{handler: NewHandler(NewService(pool, llm))}
}
func (m *Module) RegisterRoutes(mux *http.ServeMux, jwtSecret, token string) {
	m.handler.RegisterRoutes(mux, jwtSecret, token)
}
