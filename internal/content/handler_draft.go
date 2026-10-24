package content

import (
	"encoding/json"
	"log/slog"
	"net/http"

	"github.com/freeasyman/lingce-api/internal/router"
	"github.com/freeasyman/lingce-api/pkg/httputil"
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
		httputil.WriteInternalError(w, err.Error())
		return
	}
	httputil.WriteSuccess(w, draft)
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
		httputil.WriteInternalError(w, err.Error())
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
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		httputil.WriteBadRequest(w, "Invalid request body")
		return
	}
	urls, err := h.service.GenerateCardImage(r.Context(), tenantID, req.Prompt, req.ReferenceImage, req.LogoURL, req.QRURL, req.N)
	if err != nil {
		slog.Error("generate card image failed", "error", err, "tenant_id", tenantID, "n", req.N, "prompt_len", len(req.Prompt))
		httputil.WriteInternalError(w, err.Error())
		return
	}
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
		httputil.WriteInternalError(w, err.Error())
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
		TopicID int64  `json:"topic_id"`
		Form    string `json:"form"`
		Count   int    `json:"count"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		httputil.WriteBadRequest(w, "Invalid request body")
		return
	}
	content, err := h.service.GenerateContentV2(r.Context(), tenantID, req.TopicID, req.Form, req.Count)
	if err != nil {
		httputil.WriteInternalError(w, err.Error())
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
		{Method: "POST", Path: "/api/v1/content-cards/actions/generate-image", Handler: h.GenerateCardImage, Auth: true, AllowedUserTypes: []string{"admin", "employee"}},
	}
	router.Register(mux, routes, deps)
}
