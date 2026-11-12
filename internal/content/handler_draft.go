package content

import (
	"encoding/json"
	"log/slog"
	"net/http"
	"strings"

	"github.com/freeasyman/lingce-api/internal/router"
	"github.com/freeasyman/lingce-api/pkg/httputil"
	"github.com/google/uuid"
)

// GenerateGraphicNoteDraft 生成图文笔记文稿。
func (h *Handler) GenerateGraphicNoteDraft(w http.ResponseWriter, r *http.Request) {
	tenantID, ok := tenantOf(r)
	if !ok {
		httputil.WriteUnauthorized(w, "Invalid token")
		return
	}
	var req GenerateGraphicNoteDraftRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		httputil.WriteBadRequest(w, "Invalid request body")
		return
	}
	draft, err := h.service.GenerateGraphicNoteDraft(r.Context(), tenantID, req)
	if err != nil {
		writeContentError(w, err)
		return
	}
	httputil.WriteSuccess(w, draft)
}

// cardImageIdempotencyKey 把请求头规范成计费层可接受的 <module>:<object>:<action> 格式。
// 请求头缺失/非法/过长时退回唯一键：宁可每次主动生成都计费，也不能让键非法导致漏扣。
func cardImageIdempotencyKey(header string) string {
	header = strings.TrimSpace(header)
	candidate := "content:card-image:" + header
	if header == "" || len(candidate) > 128 || strings.ContainsAny(candidate, " \t\r\n") || strings.Contains(candidate, "::") || strings.HasSuffix(candidate, ":") {
		return "content:card-image:" + uuid.NewString()
	}
	return candidate
}

// SaveContentV2 保存内容。
func (h *Handler) SaveContentV2(w http.ResponseWriter, r *http.Request) {
	tenantID, ok := tenantOf(r)
	if !ok {
		httputil.WriteUnauthorized(w, "Invalid token")
		return
	}
	var req struct {
		TopicID        *int64          `json:"topic_id"`
		Form           string          `json:"form"`
		Title          string          `json:"title"`
		Body           string          `json:"body"`
		PublishPackage json.RawMessage `json:"publish_package"`
		Images         json.RawMessage `json:"images"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		httputil.WriteBadRequest(w, "Invalid request body")
		return
	}
	if req.Form == "" {
		httputil.WriteBadRequest(w, "form is required")
		return
	}
	id, err := h.service.SaveContentV2(r.Context(), tenantID, req.TopicID, req.Form, req.Title, req.Body, req.PublishPackage, req.Images)
	if err != nil {
		writeContentError(w, err)
		return
	}
	httputil.WriteSuccess(w, map[string]int64{"id": id})
}

// GenerateCardImage 生成卡片图（文生图 + 模板 + 素材叠加）。
func (h *Handler) GenerateCardImage(w http.ResponseWriter, r *http.Request) {
	tenantID, ok := tenantOf(r)
	if !ok {
		httputil.WriteUnauthorized(w, "Invalid token")
		return
	}
	var req struct {
		Prompt         string `json:"prompt"`
		ReferenceImage string `json:"reference_image"`
		LogoURL        string `json:"logo_url"`
		QRURL          string `json:"qr_url"`
		N              int    `json:"n"`
		Size           string `json:"size"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		httputil.WriteBadRequest(w, "Invalid request body")
		return
	}
	count := normalizeCardImageCount(req.N)
	// 同步生图是会真实调用图片模型的交付接口：生成前按张数预检 shot，
	// 额度不足直接拒绝，不能像历史上那样绕过计费白调模型。
	if err := h.service.ensureContentQuota(r.Context(), tenantID, contentBillingUnitShot, float64(count)); err != nil {
		writeContentError(w, err)
		return
	}
	urls, err := h.service.GenerateCardImage(r.Context(), tenantID, req.Prompt, req.ReferenceImage, req.LogoURL, req.QRURL, count, req.Size)
	if err != nil {
		slog.Error("generate card image failed", "error", err, "tenant_id", tenantID, "n", count, "prompt_len", len(req.Prompt))
		writeContentError(w, err)
		return
	}
	// 只按实际返回的图片张数扣 shot（空交付不扣）；Idempotency-Key 用于系统重试去重，
	// 未提供时每次交付生成唯一键，避免同一 prompt 的多次主动生成被误判为重复。
	idempotencyKey := cardImageIdempotencyKey(r.Header.Get("Idempotency-Key"))
	h.service.chargeCardImages(r.Context(), tenantID, len(urls), idempotencyKey)
	httputil.WriteSuccess(w, map[string][]string{"urls": urls})
}

// GenerateDraft 通用文稿生成（按 form）。
func (h *Handler) GenerateDraft(w http.ResponseWriter, r *http.Request) {
	tenantID, ok := tenantOf(r)
	if !ok {
		httputil.WriteUnauthorized(w, "Invalid token")
		return
	}
	var req struct {
		Form             string `json:"form"`
		TopicTitle       string `json:"topic_title"`
		TopicDescription string `json:"topic_description"`
		Evidence         string `json:"evidence"`
		CardCount        int    `json:"card_count"`
		TopicID          *int64 `json:"topic_id"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		httputil.WriteBadRequest(w, "Invalid request body")
		return
	}
	draft, err := h.service.GenerateDraft(r.Context(), tenantID, req.Form, GenerateGraphicNoteDraftRequest{
		TopicTitle: req.TopicTitle, TopicDescription: req.TopicDescription, Evidence: req.Evidence, CardCount: req.CardCount, TopicID: req.TopicID,
	})
	if err != nil {
		writeContentError(w, err)
		return
	}
	httputil.WriteSuccess(w, draft)
}

// GenerateContentV2 选题 → 内容（读完整 16 字段简报，按形式，json_schema 结构化输出）。
func (h *Handler) GenerateContentV2(w http.ResponseWriter, r *http.Request) {
	tenantID, ok := tenantOf(r)
	if !ok {
		httputil.WriteUnauthorized(w, "Invalid token")
		return
	}
	var req struct {
		TopicID  int64  `json:"topic_id"`
		Form     string `json:"form"`
		Count    int    `json:"count"`
		WhoPosts string `json:"who_posts"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		httputil.WriteBadRequest(w, "Invalid request body")
		return
	}
	content, err := h.service.GenerateContentV2(r.Context(), tenantID, req.TopicID, req.Form, req.Count, req.WhoPosts)
	if err != nil {
		slog.Error("generate content v2 failed", "tenant_id", tenantID, "topic_id", req.TopicID, "form", req.Form, "error", err)
		writeContentError(w, err)
		return
	}
	httputil.WriteSuccess(w, content)
}

func (h *Handler) registerDraftRoutes(mux *http.ServeMux, deps router.RouteDeps) {
	routes := []router.Route{
		{Method: "POST", Path: "/api/v1/content-drafts/actions/generate", Handler: h.GenerateDraft, Auth: true, AllowedUserTypes: []string{"admin", "employee"}},
		{Method: "POST", Path: "/api/v1/content-drafts/actions/generate-graphic-note", Handler: h.GenerateGraphicNoteDraft, Auth: true, AllowedUserTypes: []string{"admin", "employee"}},
		{Method: "POST", Path: "/api/v1/content-items", Handler: h.SaveContentV2, Auth: true, AllowedUserTypes: []string{"admin", "employee"}},
		{Method: "POST", Path: "/api/v1/content-items/actions/generate-v2", Handler: h.GenerateContentV2, Auth: true, AllowedUserTypes: []string{"admin", "employee"}},
		{Method: "POST", Path: "/api/v1/content-items/actions/export-images-zip", Handler: h.ExportImagesZip, Auth: true, AllowedUserTypes: []string{"admin", "employee"}},
		{Method: "POST", Path: "/api/v1/content-items/actions/export-docx", Handler: h.ExportDocx, Auth: true, AllowedUserTypes: []string{"admin", "employee"}},
		{Method: "GET", Path: "/api/v1/content-items/actions/export-docx", Handler: h.ExportDocxGet, Auth: true, AllowedUserTypes: []string{"admin", "employee"}},
		{Method: "GET", Path: "/api/v1/content-items/actions/export-images-zip", Handler: h.ExportImagesZipGet, Auth: true, AllowedUserTypes: []string{"admin", "employee"}},
		{Method: "POST", Path: "/api/v1/content-cards/actions/generate-image", Handler: h.GenerateCardImage, Auth: true, AllowedUserTypes: []string{"admin", "employee"}},
		{Method: "POST", Path: "/api/v1/content-items/actions/generate-storyboard-images-async", Handler: h.GenerateStoryboardImagesAsync, Auth: true, AllowedUserTypes: []string{"admin", "employee"}},
		{Method: "POST", Path: "/api/v1/content-items/actions/generate-image-async", Handler: h.GenerateImageAsync, Auth: true, AllowedUserTypes: []string{"admin", "employee"}},
		{Method: "POST", Path: "/api/v1/content-items/actions/generate-images-async", Handler: h.GenerateImagesAsync, Auth: true, AllowedUserTypes: []string{"admin", "employee"}},
	}
	router.Register(mux, routes, deps)
}
