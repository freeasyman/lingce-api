package content

import (
	"context"
	"encoding/json"
	"log/slog"
	"net/http"
	"strings"
	"time"

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
		return
	}
	// 图片生成并写入内容成功：扣 shot 1 张；扣量失败只告警，不回滚图片。
	if len(imgs) > 0 && imgs[0] != "" {
		s.chargeSingleImage(ctx, tenantID, contentID)
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
	// 图片生成入口的交付前预检：单图 shot 1 张。额度不足拒绝开始生成。
	if err := h.service.ensureAsyncImageQuota(r.Context(), tenantID, 1); err != nil {
		writeContentError(w, err)
		return
	}
	go func() {
		slog.Info("async image started", "tenant_id", tenantID, "content_id", req.ContentID, "prompt", req.Prompt)
		h.service.generateImageAsync(context.Background(), tenantID, req.ContentID, req.Prompt, req.ReferenceImage, req.LogoURL, req.QRURL, req.Size)
	}()
	httputil.WriteSuccess(w, map[string]any{"started": true})
}

// generateImagesAsync 后台按顺序生成多张图（朋友圈配图等），逐张写回 content.images。
// 复用分镜异步的 generation_status 机制：浏览器退出不中断。
func (s *Service) generateImagesAsync(ctx context.Context, tenantID, contentID int64, prompts []string, referenceImage, logoURL, qrURL, size string) {
	_ = s.store.setContentGenerationStatus(ctx, tenantID, contentID, "running", -1)
	defer func() { _ = s.store.setContentGenerationStatus(ctx, tenantID, contentID, "", -1) }()

	rec, err := s.store.getContentItemV2(ctx, tenantID, contentID)
	if err != nil {
		slog.Error("async images: load content", "error", err, "content_id", contentID)
		return
	}
	var imgs []string
	if err := json.Unmarshal(rec.Images, &imgs); err != nil {
		imgs = []string{}
	}
	for len(imgs) < len(prompts) {
		imgs = append(imgs, "")
	}
	for i, prompt := range prompts {
		// 空白 prompt 不会产出图片，也从未计入预检；跳过它保持图位下标不变。
		if strings.TrimSpace(prompt) == "" {
			continue
		}
		urls, err := s.GenerateCardImage(ctx, tenantID, prompt, referenceImage, logoURL, qrURL, 1, size)
		if err != nil {
			slog.Error("async images: generate", "error", err, "index", i)
			continue
		}
		if len(urls) > 0 {
			imgs[i] = urls[0]
		}
		newImgs, _ := json.Marshal(imgs)
		if err := s.store.updateContentItemV2(ctx, tenantID, contentID, "", rec.Title, rec.Body, rec.PublishPackage, newImgs); err != nil {
			slog.Error("async images: save", "error", err, "index", i)
		} else if len(urls) > 0 {
			// 该图位生成并写入内容成功：扣 shot 1 张（同一图位重复生成不重复扣）。
			s.chargeImageSlot(ctx, tenantID, contentID, i)
		}
		time.Sleep(3 * time.Second)
	}
}

// asyncImagePromptCount 返回一次多图异步请求实际要生成的图位数。
// 空白 prompt 不会产出图片，也不计入预检；生成循环使用同一口径跳过。
// 预检与生成必须共用这个函数，避免按 1 张预检却真实生成 N 张（低余额欠扣）。
func asyncImagePromptCount(prompts []string) int {
	count := 0
	for _, prompt := range prompts {
		if strings.TrimSpace(prompt) != "" {
			count++
		}
	}
	return count
}

// GenerateImagesAsync 触发后台生成多张图，立即返回，不阻塞请求。
func (h *Handler) GenerateImagesAsync(w http.ResponseWriter, r *http.Request) {
	tenantID, ok := tenantOf(r)
	if !ok {
		httputil.WriteUnauthorized(w, "Invalid token")
		return
	}
	var req struct {
		ContentID      int64    `json:"content_id"`
		Prompts        []string `json:"prompts"`
		ReferenceImage string   `json:"reference_image"`
		LogoURL        string   `json:"logo_url"`
		QRURL          string   `json:"qr_url"`
		Size           string   `json:"size"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil || req.ContentID <= 0 || len(req.Prompts) == 0 {
		httputil.WriteBadRequest(w, "content_id and prompts are required")
		return
	}
	imageCount := asyncImagePromptCount(req.Prompts)
	if imageCount == 0 {
		httputil.WriteBadRequest(w, "content_id and prompts are required")
		return
	}
	// 图片生成入口的交付前预检：按本次实际要生成的图位数（prompts 数量）检查 shot。
	// 余额不足整单拒绝，避免只生成一部分图、留下半成品内容。
	if err := h.service.ensureAsyncImageQuota(r.Context(), tenantID, imageCount); err != nil {
		writeContentError(w, err)
		return
	}
	go func() {
		slog.Info("async images started", "tenant_id", tenantID, "content_id", req.ContentID, "count", imageCount)
		h.service.generateImagesAsync(context.Background(), tenantID, req.ContentID, req.Prompts, req.ReferenceImage, req.LogoURL, req.QRURL, req.Size)
	}()
	httputil.WriteSuccess(w, map[string]any{"started": true, "images": imageCount})
}
