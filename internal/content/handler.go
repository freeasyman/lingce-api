package content

import (
	"net/http"

	"github.com/freeasyman/lingce-api/internal/router"
)

type Handler struct {
	service *Service
}

func NewHandler(service *Service) *Handler {
	return &Handler{service: service}
}

// RegisterRoutes registers content module routes
func (h *Handler) RegisterRoutes(mux *http.ServeMux, jwtSecret string) {
	deps := router.RouteDeps{JWTSecret: jwtSecret}
	h.registerContentOrgProfileRoutes(mux, deps)
	h.registerAIRoutes(mux, deps)
	h.registerInsightEngineRoutes(mux, deps)
	h.registerStyleTemplateRoutes(mux, deps)
	h.registerJudgeEngineRoutes(mux, deps)
	h.registerV2Routes(mux, deps)
	h.registerDraftRoutes(mux, deps)
}
