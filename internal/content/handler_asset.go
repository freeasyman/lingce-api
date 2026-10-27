package content

import (
	"io"
	"net/http"
	"path/filepath"
	"strings"

	"github.com/freeasyman/lingce-api/pkg/httputil"
	ossutil "github.com/freeasyman/lingce-api/pkg/oss"
)

const contentAssetMaxBytes = 10 * 1024 * 1024

var contentAssetExts = map[string]string{
	".jpg":  "image/jpeg",
	".jpeg": "image/jpeg",
	".png":  "image/png",
	".webp": "image/webp",
	".gif":  "image/gif",
	".svg":  "image/svg+xml",
}

// UploadContentAsset 上传内容图片素材（品牌素材：logo/二维码/医生照片等）→ OSS，返回 URL。
func (h *Handler) UploadContentAsset(w http.ResponseWriter, r *http.Request) {
	tenantID, ok := tenantOf(r)
	if !ok {
		httputil.WriteUnauthorized(w, "Invalid token")
		return
	}
	if h.service.ossClient == nil {
		httputil.WriteInternalError(w, "对象存储未配置，暂时无法上传")
		return
	}
	if err := r.ParseMultipartForm(contentAssetMaxBytes); err != nil {
		httputil.WriteBadRequest(w, "上传内容过大或格式错误")
		return
	}
	file, header, err := r.FormFile("file")
	if err != nil {
		httputil.WriteBadRequest(w, "缺少 file 字段")
		return
	}
	defer file.Close()

	ext := strings.ToLower(filepath.Ext(header.Filename))
	contentType, ok := contentAssetExts[ext]
	if !ok {
		httputil.WriteBadRequest(w, "只支持 jpg/png/webp/gif/svg")
		return
	}
	payload, err := io.ReadAll(io.LimitReader(file, contentAssetMaxBytes+1))
	if err != nil {
		writeContentError(w, err)
		return
	}
	if int64(len(payload)) > contentAssetMaxBytes {
		httputil.WriteBadRequest(w, "图片超过 10MB")
		return
	}

	key := ossutil.GenerateObjectKey("content/assets/"+itoa(tenantID), ext)
	url, err := h.service.ossClient.UploadBytes(r.Context(), key, payload, &ossutil.UploadOptions{ContentType: contentType})
	if err != nil {
		httputil.WriteInternalError(w, "上传失败: "+err.Error())
		return
	}
	httputil.WriteSuccess(w, map[string]any{"url": url})
}

func itoa(v int64) string {
	if v == 0 {
		return "0"
	}
	var buf [20]byte
	i := len(buf)
	neg := v < 0
	if neg {
		v = -v
	}
	for v > 0 {
		i--
		buf[i] = byte('0' + v%10)
		v /= 10
	}
	if neg {
		i--
		buf[i] = '-'
	}
	return string(buf[i:])
}
