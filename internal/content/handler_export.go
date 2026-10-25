package content

import (
	"archive/zip"
	"bytes"
	"encoding/base64"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"

	"github.com/freeasyman/lingce-api/pkg/httputil"
)

// ExportImagesZip 把多张图片（公网 URL）打包成 ZIP 下载。
func (h *Handler) ExportImagesZip(w http.ResponseWriter, r *http.Request) {
	if _, ok := tenantOf(r); !ok {
		httputil.WriteUnauthorized(w, "Invalid token")
		return
	}
	var req struct {
		URLs []string `json:"urls"`
		Name string   `json:"name"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		httputil.WriteBadRequest(w, "Invalid request body")
		return
	}
	if len(req.URLs) == 0 {
		httputil.WriteBadRequest(w, "urls is required")
		return
	}
	buf, ok := buildImagesZip(r.Context(), req.URLs)
	if !ok {
		httputil.WriteInternalError(w, "所有图片下载失败")
		return
	}
	name := strings.TrimSpace(req.Name)
	if name == "" {
		name = "卡片图"
	}
	encoded := url.QueryEscape(name + ".zip")
	w.Header().Set("Content-Type", "application/zip")
	w.Header().Set("Content-Disposition", fmt.Sprintf(`attachment; filename="content-images.zip"; filename*=UTF-8''%s`, encoded))
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write(buf.Bytes())
}

// buildImagesZip 逐张下载并打包，返回 (zip buffer, 是否至少成功一张)。
func buildImagesZip(ctx context.Context, urls []string) (*bytes.Buffer, bool) {
	buf := new(bytes.Buffer)
	zw := zip.NewWriter(buf)
	client := &http.Client{}
	ok := 0
	for i, u := range urls {
		u = strings.TrimSpace(u)
		if u == "" {
			continue
		}
		req, err := http.NewRequestWithContext(ctx, http.MethodGet, u, nil)
		if err != nil {
			continue
		}
		resp, err := client.Do(req)
		if err != nil {
			continue
		}
		data, rerr := io.ReadAll(resp.Body)
		resp.Body.Close()
		if rerr != nil || resp.StatusCode != http.StatusOK {
			continue
		}
		f, err := zw.Create(fmt.Sprintf("卡%d.png", i+1))
		if err != nil {
			continue
		}
		if _, err := f.Write(data); err != nil {
			continue
		}
		ok++
	}
	_ = zw.Close()
	return buf, ok > 0
}

// ExportDocx 把标题 + 卡片正文 + 发布包导出成 Word docx。
func (h *Handler) ExportDocx(w http.ResponseWriter, r *http.Request) {
	if _, ok := tenantOf(r); !ok {
		httputil.WriteUnauthorized(w, "Invalid token")
		return
	}
	var req struct {
		Title          string              `json:"title"`
		Cards          []docxCard          `json:"cards"`
		PublishPackage map[string]any      `json:"publish_package"`
		Images         []string            `json:"images"`
		Mode             string                   `json:"mode"`
		Cover            map[string]any           `json:"cover"`
		Segments         []docxScriptSegment      `json:"segments"`
		Bgm              string                   `json:"bgm"`
		Brand            string                   `json:"brand"`
		GlobalEditing    map[string]any           `json:"global_editing"`
		Chapters         []docxStoryboardChapter  `json:"chapters"`
		MaterialChecklist []docxMaterial          `json:"material_checklist"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		httputil.WriteBadRequest(w, "Invalid request body")
		return
	}
	var data []byte
	var buildErr error
	if len(req.Chapters) > 0 {
		data, buildErr = buildStoryboardDocx(req.Title, req.GlobalEditing, req.Chapters, req.MaterialChecklist, req.PublishPackage)
	} else if len(req.Segments) > 0 {
		data, buildErr = buildScriptDocx(req.Title, req.Mode, req.Cover, req.Segments, req.Bgm, req.Brand, req.PublishPackage)
	} else {
		data, buildErr = buildDocx(req.Title, req.Cards, req.PublishPackage, req.Images)
	}
	if buildErr != nil {
		httputil.WriteInternalError(w, "生成 docx 失败: "+buildErr.Error())
		return
	}
	name := strings.TrimSpace(req.Title)
	if name == "" {
		name = "内容"
	}
	encoded := url.QueryEscape(name + ".docx")
	w.Header().Set("Content-Type", "application/vnd.openxmlformats-officedocument.wordprocessingml.document")
	w.Header().Set("Content-Disposition", fmt.Sprintf(`attachment; filename="content.docx"; filename*=UTF-8''%s`, encoded))
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write(data)
}


// ExportDocxGet 是 ExportDocx 的 GET 版本，payload 放在 query（base64），用于导航式下载。
func (h *Handler) ExportDocxGet(w http.ResponseWriter, r *http.Request) {
	if _, ok := tenantOf(r); !ok {
		httputil.WriteUnauthorized(w, "Invalid token")
		return
	}
	raw := r.URL.Query().Get("payload")
	decoded, err := base64.RawURLEncoding.DecodeString(raw)
	if err != nil {
		httputil.WriteBadRequest(w, "Invalid payload")
		return
	}
	var req struct {
		Title          string              `json:"title"`
		Cards          []docxCard          `json:"cards"`
		PublishPackage map[string]any      `json:"publish_package"`
		Images         []string            `json:"images"`
		Mode             string                   `json:"mode"`
		Cover            map[string]any           `json:"cover"`
		Segments         []docxScriptSegment      `json:"segments"`
		Bgm              string                   `json:"bgm"`
		Brand            string                   `json:"brand"`
		GlobalEditing    map[string]any           `json:"global_editing"`
		Chapters         []docxStoryboardChapter  `json:"chapters"`
		MaterialChecklist []docxMaterial          `json:"material_checklist"`
	}
	if err := json.Unmarshal(decoded, &req); err != nil {
		httputil.WriteBadRequest(w, "Invalid payload json")
		return
	}
	var data []byte
	var buildErr error
	if len(req.Chapters) > 0 {
		data, buildErr = buildStoryboardDocx(req.Title, req.GlobalEditing, req.Chapters, req.MaterialChecklist, req.PublishPackage)
	} else if len(req.Segments) > 0 {
		data, buildErr = buildScriptDocx(req.Title, req.Mode, req.Cover, req.Segments, req.Bgm, req.Brand, req.PublishPackage)
	} else {
		data, buildErr = buildDocx(req.Title, req.Cards, req.PublishPackage, req.Images)
	}
	if buildErr != nil {
		httputil.WriteInternalError(w, "生成 docx 失败: "+buildErr.Error())
		return
	}
	name := strings.TrimSpace(req.Title)
	if name == "" {
		name = "内容"
	}
	encoded := url.QueryEscape(name + ".docx")
	w.Header().Set("Content-Type", "application/vnd.openxmlformats-officedocument.wordprocessingml.document")
	w.Header().Set("Content-Disposition", fmt.Sprintf(`attachment; filename="content.docx"; filename*=UTF-8''%s`, encoded))
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write(data)
}

// ExportImagesZipGet 是 ExportImagesZip 的 GET 版本，urls 放在 query（base64 JSON），用于导航式下载。
func (h *Handler) ExportImagesZipGet(w http.ResponseWriter, r *http.Request) {
	if _, ok := tenantOf(r); !ok {
		httputil.WriteUnauthorized(w, "Invalid token")
		return
	}
	raw := r.URL.Query().Get("urls")
	decoded, err := base64.RawURLEncoding.DecodeString(raw)
	if err != nil {
		httputil.WriteBadRequest(w, "Invalid urls")
		return
	}
	var urls []string
	if err := json.Unmarshal(decoded, &urls); err != nil || len(urls) == 0 {
		httputil.WriteBadRequest(w, "Invalid urls json")
		return
	}
	buf, ok := buildImagesZip(r.Context(), urls)
	if !ok {
		httputil.WriteInternalError(w, "所有图片下载失败")
		return
	}
	name := strings.TrimSpace(r.URL.Query().Get("name"))
	if name == "" {
		name = "卡片图"
	}
	encoded := url.QueryEscape(name + ".zip")
	w.Header().Set("Content-Type", "application/zip")
	w.Header().Set("Content-Disposition", fmt.Sprintf(`attachment; filename="content-images.zip"; filename*=UTF-8''%s`, encoded))
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write(buf.Bytes())
}
