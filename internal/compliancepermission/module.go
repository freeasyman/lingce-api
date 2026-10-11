package compliancepermission

import (
	"net/http"

	"github.com/jackc/pgx/v5/pgxpool"
)

type Module struct {
	handler *Handler
	service *Service
}

func NewModule(pool *pgxpool.Pool) *Module {
	service := NewService(pool)
	return &Module{handler: NewHandler(service), service: service}
}

func (m *Module) RegisterRoutes(mux *http.ServeMux, jwtSecret string) {
	m.handler.RegisterRoutes(mux, jwtSecret)
}

func (m *Module) Service() *Service {
	return m.service
}
