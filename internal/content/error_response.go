package content

import (
	"errors"
	"net/http"
	"strings"

	"github.com/freeasyman/lingce-api/pkg/httputil"
)

// 参数/字段的中文名，用于把 "xxx is required" 翻译成用户能看懂的话。
var contentFieldLabels = map[string]string{
	"question":     "患者问题",
	"region":       "地域",
	"prompt":       "生成内容",
	"topic_title":  "选题标题",
	"topic_id":     "选题",
	"insight_ids":  "灵感",
	"image_url":    "图片地址",
	"url":          "网址",
	"title":        "标题",
	"body":         "正文",
	"target":       "目标渠道",
	"run_id":       "诊断记录",
	"content_id":   "内容",
	"account_id":   "账号",
	"platform":     "平台",
	"plan_item_id": "计划项",
}

// writeContentError 把业务错误映射成正确的 HTTP 语义：
//   - BizError        → 按其 code/status 返回
//   - 参数缺失         → 400（缺少必填参数：xxx）
//   - 资源不存在       → 404（记录不存在或已被删除）
//   - 其余             → 500（技术细节只进日志）
//
// 之前这些情况统一返回 500「服务暂时不可用」，用户完全不知道该怎么办。
func writeContentError(w http.ResponseWriter, err error) {
	var bizErr *BizError
	if errors.As(err, &bizErr) {
		status := http.StatusBadRequest
		if bizErr.Status > 0 {
			status = bizErr.Status
		}
		httputil.WriteError(w, status, bizErr.Code, bizErr.Message, nil)
		return
	}

	msg := err.Error()
	lower := strings.ToLower(msg)

	if field, ok := requiredField(lower); ok {
		label := field
		if v, hit := contentFieldLabels[field]; hit {
			label = v
		}
		httputil.WriteError(w, http.StatusBadRequest, "MISSING_PARAM", "缺少必填内容："+label, nil)
		return
	}

	if strings.Contains(lower, "not found") || strings.Contains(lower, "no rows in result set") {
		httputil.WriteError(w, http.StatusNotFound, "NOT_FOUND", "记录不存在或已被删除", nil)
		return
	}

	httputil.WriteInternalError(w, msg)
}

// requiredField 从 "xxx is required" / "xxx is required for yyy" 里取出字段名。
func requiredField(lowerMsg string) (string, bool) {
	idx := strings.Index(lowerMsg, "is required")
	if idx < 0 {
		return "", false
	}
	head := strings.TrimSpace(lowerMsg[:idx])
	// 取最后一个词，兼容 "topic_id is required" 与 "prompt is required for cards"
	if f := strings.Fields(head); len(f) > 0 {
		token := f[len(f)-1]
		token = strings.Trim(token, "：:,，")
		if token != "" {
			return token, true
		}
	}
	return "", false
}
