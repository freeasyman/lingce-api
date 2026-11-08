package dealexpush

import (
	"log/slog"
	"net/http"

	"github.com/freeasyman/lingce-api/internal/config"
	"github.com/jackc/pgx/v5/pgxpool"
)

type Module struct {
	handler *Handler
}

func NewModule(pool *pgxpool.Pool, cfg config.DealPushConfig, log *slog.Logger) *Module {
	return &Module{handler: NewHandler(NewService(pool, cfg, log))}
}

func (m *Module) RegisterRoutes(mux *http.ServeMux, jwtSecret string) {
	m.handler.RegisterRoutes(mux, jwtSecret)
}
