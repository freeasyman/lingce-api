package content

import (
	"context"
	"encoding/json"
	"log/slog"
	"net/http"
	"strings"

	"github.com/freeasyman/lingce-api/pkg/httputil"
)

// generateImageAsync 后台生成单张内容图片（知识内容海报等单图形式）。
// 生成后写回 content.images（替换为这一张），并清空 generation_status。
// 复用分镜异步的 generation_status 机制：浏览器退出不中断。
func (s *Service) generateImageAsync(ctx context.Context, tenantID, contentID int64, prompt, referenceImage, logoURL, qrURL, size string) {
	_ = s.store.setContentGenerationStatus(ctx, tenantID, contentID, "running", -1)
	defer func() { _ = s.store.setContentGenerationStatus(ctx, tenantID, contentID, "", -1) }()

	urls, err := s.GenerateCardImage(ctx, tenantID, prompt, referenceImage, logoURL, qrURL, 1, size)
	if err != nil {
		slog.Error("async image: generate", "error", err, "content_id", contentID)
		return
	}
	rec, err := s.store.getContentItemV2(ctx, tenantID, contentID)
	if err != nil {
		slog.Error("async image: load content", "error", err, "content_id", contentID)
		return
	}
	imgs := []string{}
	if len(urls) > 0 {
		imgs = []string{urls[0]}
	}
	newImgs, _ := json.Marshal(imgs)
	if err := s.store.updateContentItemV2(ctx, tenantID, contentID, "", rec.Title, rec.Body, rec.PublishPackage, newImgs); err != nil {
		slog.Error("async image: save", "error", err, "content_id", contentID)
	}
}

// GenerateImageAsync 触发后台生成单张内容图片，立即返回，不阻塞请求。
func (h *Handler) GenerateImageAsync(w http.ResponseWriter, r *http.Request) {
	tenantID, ok := tenantOf(r)
	if !ok {
		httputil.WriteUnauthorized(w, "Invalid token")
		return
	}
	var req struct {
		ContentID      int64  `json:"content_id"`
		Prompt         string `json:"prompt"`
		ReferenceImage string `json:"reference_image"`
		LogoURL        string `json:"logo_url"`
		QRURL          string `json:"qr_url"`
		Size           string `json:"size"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil || req.ContentID <= 0 || strings.TrimSpace(req.Prompt) == "" {
		httputil.WriteBadRequest(w, "content_id and prompt are required")
		return
	}
	go func() {
		slog.Info("async image started", "tenant_id", tenantID, "content_id", req.ContentID, "prompt", req.Prompt)
		h.service.generateImageAsync(context.Background(), tenantID, req.ContentID, req.Prompt, req.ReferenceImage, req.LogoURL, req.QRURL, req.Size)
	}()
	httputil.WriteSuccess(w, map[string]any{"started": true})
}
