package dealexpush

import (
	"log/slog"
	"net/http"

	"github.com/freeasyman/lingce-api/internal/config"
	"github.com/freeasyman/lingce-api/pkg/sms"
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

// NewModuleWithService 用已有 Service 构建模块（main 注入真实短信发送器）。
func NewModuleWithService(service *Service) *Module {
	return &Module{handler: NewHandler(service)}
}

// NewAliyunTemplateSender 用 pkg/sms 阿里云客户端构建模板发送器。
func NewAliyunTemplateSender(client *sms.AliyunClient) TemplateSender {
	return aliyunSender{client: client}
}
