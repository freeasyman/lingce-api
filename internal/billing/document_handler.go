package billing

// 通知函文档的 HTTP 接口：生成、列表、下载 PDF。
//
// 路径与鉴权沿用运营端计费既有做法（/api/v1/ops/billing/*，只允许 admin），
// 数据权限在 service 层按 JWT 里的运营账号 id 校验（越权 403）。

import (
	"fmt"
	"net/http"
	"strconv"
	"strings"

	"github.com/freeasyman/lingce-api/pkg/httputil"
)

// GenerateInstitutionDocument 生成一份通知函（PDF 落库）并返回元数据。
func (h *Handler) GenerateInstitutionDocument(w http.ResponseWriter, r *http.Request) {
	tenantID, ok := requireInstitutionID(w, r)
	if !ok {
		return
	}
	adminID, ok := requireAdminActor(w, r)
	if !ok {
		return
	}
	if h.documents == nil {
		httputil.WriteInternalError(w, "文档能力未装配")
		return
	}
	var req GenerateDocumentRequest
	if err := decodeInstitutionBody(r, &req); err != nil {
		httputil.WriteBadRequest(w, "invalid request body")
		return
	}
	item, err := h.documents.Generate(r.Context(), tenantID, adminID, req)
	if err != nil {
		writeInstitutionError(w, err)
		return
	}
	// 与前端约定返回 {"data": ...}（页面 billingService 的 unwrap 兼容两种形状）。
	httputil.WriteSuccess(w, map[string]any{"data": item})
}

// ListInstitutionDocuments 返回机构下已生成的通知函记录（新→旧）。
func (h *Handler) ListInstitutionDocuments(w http.ResponseWriter, r *http.Request) {
	tenantID, ok := requireInstitutionID(w, r)
	if !ok {
		return
	}
	adminID, ok := requireAdminActor(w, r)
	if !ok {
		return
	}
	if h.documents == nil {
		httputil.WriteInternalError(w, "文档能力未装配")
		return
	}
	result, err := h.documents.List(r.Context(), tenantID, adminID)
	if err != nil {
		writeInstitutionError(w, err)
		return
	}
	httputil.WriteSuccess(w, map[string]any{"data": result})
}

// DownloadBillingDocument 下载一份通知函 PDF。
//
// Content-Disposition 同时带 ASCII 兜底名与 RFC 5987 的中文名：
// attachment; filename="LC-TR-2026-0005.pdf"; filename*=UTF-8”%E8%AF%95...
func (h *Handler) DownloadBillingDocument(w http.ResponseWriter, r *http.Request) {
	docID, ok := requirePathID(w, r, "docId", "Invalid document ID")
	if !ok {
		return
	}
	adminID, ok := requireAdminActor(w, r)
	if !ok {
		return
	}
	if h.documents == nil {
		httputil.WriteInternalError(w, "文档能力未装配")
		return
	}
	doc, err := h.documents.Download(r.Context(), docID, adminID)
	if err != nil {
		writeInstitutionError(w, err)
		return
	}
	w.Header().Set("Content-Type", "application/pdf")
	w.Header().Set("Content-Disposition", contentDispositionAttachment(documentFileName(doc), documentFallbackFileName(doc)))
	w.Header().Set("Content-Length", strconv.Itoa(len(doc.Content)))
	w.Header().Set("Cache-Control", "private, no-store")
	w.Header().Set("X-Content-Type-Options", "nosniff")
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write(doc.Content)
}

// contentDispositionAttachment 生成附件下载头：ASCII 兜底名 + UTF-8 文件名。
func contentDispositionAttachment(fileName, fallbackName string) string {
	fallback := strings.TrimSpace(fallbackName)
	if fallback == "" {
		fallback = "document.pdf"
	}
	return fmt.Sprintf("attachment; filename=%q; filename*=UTF-8''%s", fallback, encodeRFC5987(fileName))
}

// encodeRFC5987 按 RFC 5987 的 attr-char 集合对文件名做百分号编码。
func encodeRFC5987(value string) string {
	const hexDigits = "0123456789ABCDEF"
	var builder strings.Builder
	builder.Grow(len(value) * 3)
	for _, char := range []byte(value) {
		switch {
		case char >= 'A' && char <= 'Z',
			char >= 'a' && char <= 'z',
			char >= '0' && char <= '9',
			strings.IndexByte("!#$&+-.^_`|~", char) >= 0:
			builder.WriteByte(char)
		default:
			builder.WriteByte('%')
			builder.WriteByte(hexDigits[char>>4])
			builder.WriteByte(hexDigits[char&0x0f])
		}
	}
	return builder.String()
}
