package content

import (
	"encoding/base64"
	"encoding/json"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/freeasyman/lingce-api/pkg/httputil"
)

// requestBaseURL 取本次请求的公开源（含反向代理后的协议）。
func requestBaseURL(r *http.Request) string {
	scheme := "http"
	if r.TLS != nil || strings.EqualFold(r.Header.Get("X-Forwarded-Proto"), "https") {
		scheme = "https"
	}
	return scheme + "://" + r.Host
}

// UploadPublicFreeCheckupImage 长图上传：前端生成后上传，返回可直接长按保存的 https 图片地址。
func (h *Handler) UploadPublicFreeCheckupImage(w http.ResponseWriter, r *http.Request) {
	if !checkupLimiter.allow(clientKey(r), 30, time.Hour) {
		freeCheckupTooMany(w, "上传过于频繁，请稍后再试")
		return
	}
	var req struct {
		Data string `json:"data"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		httputil.WriteBadRequest(w, "invalid body")
		return
	}
	raw := strings.TrimSpace(req.Data)
	contentType := "image/jpeg"
	if i := strings.Index(raw, ";base64,"); i > 0 {
		contentType = strings.TrimPrefix(raw[:i], "data:")
		raw = raw[i+len(";base64,"):]
	} else if strings.HasPrefix(raw, "data:") {
		httputil.WriteBadRequest(w, "unsupported image payload")
		return
	}
	data, err := base64.StdEncoding.DecodeString(raw)
	if err != nil || len(data) == 0 {
		httputil.WriteBadRequest(w, "invalid image data")
		return
	}
	if len(data) > 5<<20 {
		httputil.WriteBadRequest(w, "image too large")
		return
	}
	_ = h.service.store.DeleteOldFreeCheckupImages(r.Context(), 7*24*time.Hour)
	id, err := h.service.store.InsertFreeCheckupImage(r.Context(), contentType, data)
	if err != nil {
		writeContentError(w, err)
		return
	}
	// 图片地址用「本次请求的公开源」构造：客户端刚用它上传成功，一定能读回。
	// （与二维码不同，图片是一次性资源，不需要固定域名）
	httputil.WriteSuccess(w, map[string]any{
		"url": requestBaseURL(r) + "/api/v1/public/free-checkup/image/" + strconv.FormatInt(id, 10),
	})
}

// GetPublicFreeCheckupImage 读取长图（公开可访问，微信长按保存走这里）。
func (h *Handler) GetPublicFreeCheckupImage(w http.ResponseWriter, r *http.Request) {
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil || id <= 0 {
		httputil.WriteBadRequest(w, "invalid id")
		return
	}
	ct, data, err := h.service.store.GetFreeCheckupImage(r.Context(), id)
	if err != nil || len(data) == 0 {
		http.NotFound(w, r)
		return
	}
	w.Header().Set("Content-Type", ct)
	w.Header().Set("Cache-Control", "public, max-age=604800, immutable")
	_, _ = w.Write(data)
}
