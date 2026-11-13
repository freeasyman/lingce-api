package his

import (
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/freeasyman/lingce-api/pkg/httputil"
)

const (
	importCacheTTL      = 30 * time.Minute
	importCacheMaxFiles = 8
)

// ImportCommitRequest 是提交导入的 JSON 入参。
// rows 与 file_token 二选一：优先使用预览时返回的 file_token，避免前端重新上传大文件。
type ImportCommitRequest struct {
	Target    string         `json:"target"`
	Mapping   map[string]int `json:"mapping"`
	Rows      [][]string     `json:"rows"`
	FileToken string         `json:"file_token"`
}

// cachedImport 是预览解析后暂存的数据（只存解析结果，不落盘）。
type cachedImport struct {
	tenantID int64
	target   string
	headers  []string
	rows     []importRow
	created  time.Time
	expires  time.Time
}

type importFileCache struct {
	mu      sync.Mutex
	entries map[string]cachedImport
}

func newImportFileCache() *importFileCache {
	return &importFileCache{entries: make(map[string]cachedImport)}
}

func (cache *importFileCache) put(tenantID int64, target string, headers []string, rows []importRow) string {
	token := make([]byte, 16)
	if _, err := rand.Read(token); err != nil {
		return ""
	}
	key := hex.EncodeToString(token)
	now := time.Now()
	cache.mu.Lock()
	defer cache.mu.Unlock()
	cache.cleanupLocked(now)
	if len(cache.entries) >= importCacheMaxFiles {
		var oldestKey string
		var oldest time.Time
		for entryKey, entry := range cache.entries {
			if oldestKey == "" || entry.created.Before(oldest) {
				oldestKey, oldest = entryKey, entry.created
			}
		}
		delete(cache.entries, oldestKey)
	}
	cache.entries[key] = cachedImport{
		tenantID: tenantID,
		target:   target,
		headers:  headers,
		rows:     rows,
		created:  now,
		expires:  now.Add(importCacheTTL),
	}
	return key
}

func (cache *importFileCache) get(token string, tenantID int64) (cachedImport, bool) {
	cache.mu.Lock()
	defer cache.mu.Unlock()
	cache.cleanupLocked(time.Now())
	entry, ok := cache.entries[token]
	if !ok || entry.tenantID != tenantID || time.Now().After(entry.expires) {
		return cachedImport{}, false
	}
	return entry, true
}

func (cache *importFileCache) cleanupLocked(now time.Time) {
	for key, entry := range cache.entries {
		if now.After(entry.expires) {
			delete(cache.entries, key)
		}
	}
}

// PreviewImport 解析上传的 Excel/CSV，识别表头并建议列映射。
func (h *Handler) PreviewImport(w http.ResponseWriter, r *http.Request) {
	_, tenantID, ok := h.requireTenant(w, r)
	if !ok {
		return
	}
	r.Body = http.MaxBytesReader(w, r.Body, importMaxFileBytes+2<<20)
	if err := r.ParseMultipartForm(importMaxFileBytes); err != nil {
		httputil.WriteBadRequest(w, "文件上传失败或文件超过 10MB")
		return
	}
	target := strings.TrimSpace(r.FormValue("target"))
	if _, ok := importSpec(target); !ok {
		httputil.WriteBadRequest(w, "请选择有效的导入目标")
		return
	}
	file, header, err := r.FormFile("file")
	if err != nil {
		httputil.WriteBadRequest(w, "请选择要上传的文件")
		return
	}
	defer file.Close()
	if header.Size > importMaxFileBytes {
		httputil.WriteBadRequest(w, "文件超过 10MB，请拆分后导入")
		return
	}
	data, err := io.ReadAll(io.LimitReader(file, importMaxFileBytes+1))
	if err != nil {
		httputil.WriteBadRequest(w, "读取文件失败，请重试")
		return
	}
	if len(data) > importMaxFileBytes {
		httputil.WriteBadRequest(w, "文件超过 10MB，请拆分后导入")
		return
	}

	sheets, err := parseImportFile(header.Filename, data)
	if err != nil {
		writeImportError(w, err)
		return
	}
	preview, rows, err := buildImportPreview(sheets, target)
	if err != nil {
		writeImportError(w, err)
		return
	}
	preview.FileToken = h.importCache.put(tenantID, target, preview.Headers, rows)
	httputil.WriteSuccess(w, preview)
}

// CommitImport 按字段映射逐行导入，返回成功/失败统计与错误行明细。
func (h *Handler) CommitImport(w http.ResponseWriter, r *http.Request) {
	claims, tenantID, ok := h.requireTenant(w, r)
	if !ok {
		return
	}
	var req ImportCommitRequest
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 16<<20)).Decode(&req); err != nil {
		httputil.WriteBadRequest(w, "导入参数格式不正确")
		return
	}
	target := strings.TrimSpace(req.Target)
	spec, ok := importSpec(target)
	if !ok {
		httputil.WriteBadRequest(w, "请选择有效的导入目标")
		return
	}
	if len(req.Rows) > importMaxRows {
		httputil.WriteBadRequest(w, "单次最多导入 5000 行，请拆分后导入")
		return
	}

	var rows []importRow
	headerWidth := 0
	switch {
	case strings.TrimSpace(req.FileToken) != "":
		entry, ok := h.importCache.get(strings.TrimSpace(req.FileToken), tenantID)
		if !ok {
			httputil.WriteBadRequest(w, "文件预览已过期，请重新上传文件")
			return
		}
		if entry.target != target {
			httputil.WriteBadRequest(w, "导入目标与预览文件不一致，请重新上传")
			return
		}
		rows = entry.rows
		headerWidth = len(entry.headers)
	case req.Rows != nil:
		rows = make([]importRow, 0, len(req.Rows))
		for index, cells := range req.Rows {
			rows = append(rows, importRow{Number: index + 2, Cells: cells})
			if len(cells) > headerWidth {
				headerWidth = len(cells)
			}
		}
	default:
		httputil.WriteBadRequest(w, "请先上传文件并完成预览")
		return
	}

	if err := ValidateImportMapping(spec.Key, req.Mapping); err != nil {
		writeImportError(w, err)
		return
	}
	for key, index := range req.Mapping {
		if headerWidth > 0 && index >= headerWidth {
			httputil.WriteBadRequest(w, "字段「"+key+"」选择的列不存在，请重新映射")
			return
		}
	}
	if len(rows) > importMaxRows {
		httputil.WriteBadRequest(w, "单次最多导入 5000 行，请拆分后导入")
		return
	}

	result, err := h.service.CommitImport(r.Context(), tenantID, claims.UserID, target, req.Mapping, rows)
	if err != nil {
		writeImportError(w, err)
		return
	}
	httputil.WriteSuccess(w, result)
}

// ImportTemplate 下载空白 CSV 模板（UTF-8 BOM，Excel 可直接打开）。
func (h *Handler) ImportTemplate(w http.ResponseWriter, r *http.Request) {
	_, _, ok := h.requireTenant(w, r)
	if !ok {
		return
	}
	target := strings.TrimSpace(r.URL.Query().Get("target"))
	data, err := ImportTemplateCSV(target)
	if err != nil {
		writeImportError(w, err)
		return
	}
	filename := "lingce-import-" + target + "-template.csv"
	w.Header().Set("Content-Type", "text/csv; charset=utf-8")
	w.Header().Set("Content-Disposition", `attachment; filename="`+filename+`"`)
	w.Header().Set("Cache-Control", "no-store")
	w.WriteHeader(http.StatusOK)
	if _, err := w.Write(data); err != nil {
		slog.Error("write import template failed", "error", err)
	}
}

func writeImportError(w http.ResponseWriter, err error) {
	var importErr *ImportError
	if errors.As(err, &importErr) {
		httputil.WriteBadRequest(w, importErr.Message)
		return
	}
	slog.Error("import failed", "error", err)
	httputil.WriteInternalError(w, "import failed")
}
