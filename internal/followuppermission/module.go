package followuppermission

import (
	"net/http"

	"github.com/jackc/pgx/v5/pgxpool"
)

type Module struct {
	handler *Handler
}

func NewModule(pool *pgxpool.Pool) *Module {
	return &Module{handler: NewHandler(NewService(pool))}
}

func (m *Module) RegisterRoutes(mux *http.ServeMux, jwtSecret string) {
	m.handler.RegisterRoutes(mux, jwtSecret)
}
