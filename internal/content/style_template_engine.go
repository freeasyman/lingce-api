package content

import (
	"context"
	"encoding/json"
	"fmt"
	"image"
	_ "image/gif"
	_ "image/jpeg"
	_ "image/png"
	"io"
	"net/http"
	"regexp"
	"strings"
	"time"

	"github.com/freeasyman/lingce-api/pkg/llmgateway"
	"github.com/freeasyman/lingce-api/pkg/oss"
)

const learnStyleTemplatePrompt = `你是视觉设计分析专家。分析这张图片的视觉风格，提取可复用的设计参数，返回 JSON：
{"colors":["#hex"],"fonts":"字体风格描述","spacing":"间距/留白描述","decorations":"装饰元素描述(色块/徽章/虚线框/渐变等)","layout":"版式描述","highlights":"重点字高亮方式"}。
只返回 JSON，不要其他内容。`

const verifyImageTextPrompt = `逐字原样读出图中所有文字。然后与下面"预期文字"逐字对比，找出：1) 错字/乱码 2) 缺失的字 3) 多出的字。
若完全一致返回 {"pass":true,"errors":[]}；否则返回 {"pass":false,"errors":["图中"X"应为"Y""]}。
只返回 JSON，不要其他内容。`

// LearnStyleTemplateRequest 学风格模板请求。
type LearnStyleTemplateRequest struct {
	ImageURL string `json:"image_url"`
	Name     string `json:"name"`
	Type     string `json:"type"` // visual / layout
	Industry string `json:"industry,omitempty"`
	Level    string `json:"level,omitempty"` // global / industry / tenant
}

// VerifyImageTextResult OCR 校验结果。
type VerifyImageTextResult struct {
	Pass   bool     `json:"pass"`
	Errors []string `json:"errors"`
}

// multimodalCall 调多模态 llm，返回文本内容。
func (s *Service) multimodalCall(ctx context.Context, tenantID int64, imageURL, prompt string) (string, error) {
	selectedModel, err := s.resolveLLMModelConfig(ctx, tenantID, []string{"image_analysis", "image_understanding", "chat"})
	if err != nil {
		return "", fmt.Errorf("resolve llm config for multimodal: %w", err)
	}
	req := llmgateway.ImageInferenceRequest{
		TenantID:      tenantID,
		CallerService: "lingce-api",
		CallerModule:  contentCallerModule("multimodal"),
		FunctionType:  selectedModel.FunctionType,
		Provider:      selectedModel.Provider,
		ModelCode:     selectedModel.ModelCode,
		Messages: []llmgateway.ImageMessage{
			{Role: "user", Content: []llmgateway.ImageContentPart{
				{Image: imageURL},
				{Text: prompt},
			}},
		},
	}
	resp, err := s.llmClient.ImageInference(ctx, req)
	if err != nil {
		return "", fmt.Errorf("multimodal inference failed: %w", err)
	}
	return resp.Content, nil
}

// extractJSONObject 从 llm 文本中提取 JSON 对象（容忍 markdown 包裹）。
func extractJSONObject(content string) (map[string]any, error) {
	raw := strings.TrimSpace(content)
	if idx := strings.Index(raw, "```"); idx >= 0 {
		raw = raw[idx+3:]
		if strings.HasPrefix(strings.TrimSpace(raw), "json") {
			raw = strings.TrimSpace(raw)[4:]
		}
		if end := strings.LastIndex(raw, "```"); end >= 0 {
			raw = raw[:end]
		}
		raw = strings.TrimSpace(raw)
	}
	if start := strings.Index(raw, "{"); start >= 0 {
		if end := strings.LastIndex(raw, "}"); end > start {
			raw = raw[start : end+1]
		}
	}
	var out map[string]any
	if err := json.Unmarshal([]byte(raw), &out); err != nil {
		return nil, fmt.Errorf("unmarshal json: %w (raw: %.200s)", err, raw)
	}
	return out, nil
}

// LearnStyleTemplate 从截图学风格模板，落库 content_style_templates。
func (s *Service) LearnStyleTemplate(ctx context.Context, tenantID int64, req LearnStyleTemplateRequest) (map[string]any, error) {
	if s.llmClient == nil {
		return nil, fmt.Errorf("llm gateway is not configured")
	}
	if strings.TrimSpace(req.ImageURL) == "" {
		return nil, fmt.Errorf("image_url is required")
	}
	name := strings.TrimSpace(req.Name)
	if name == "" {
		name = "学到的模板"
	}
	tplType := strings.TrimSpace(req.Type)
	if tplType != "layout" {
		tplType = "visual"
	}
	level := strings.TrimSpace(req.Level)
	if level == "" {
		level = "tenant"
	}

	content, err := s.multimodalCall(ctx, tenantID, req.ImageURL, s.resolvePromptByCode(ctx, tenantID, "content_learn_style", learnStyleTemplatePrompt))
	if err != nil {
		return nil, err
	}
	style, err := extractJSONObject(content)
	if err != nil {
		return nil, err
	}
	styleJSON, _ := json.Marshal(style)

	var id int64
	err = s.store.pool.QueryRow(ctx, `
		INSERT INTO content_style_templates (tenant_id, level, industry, type, name, reference_url, extracted_style)
		VALUES ($1,$2,$3,$4,$5,$6,$7::jsonb)
		RETURNING id`,
		tenantID, level, req.Industry, tplType, name, req.ImageURL, string(styleJSON),
	).Scan(&id)
	if err != nil {
		return nil, fmt.Errorf("insert style template: %w", err)
	}

	result := map[string]any{"id": id, "name": name, "type": tplType, "style": style}
	return result, nil
}

// VerifyImageText 校验图片文字 vs 预期文字，返回错字/缺失。
func (s *Service) VerifyImageText(ctx context.Context, tenantID int64, imageURL, expectedText string) (*VerifyImageTextResult, error) {
	if s.llmClient == nil {
		return nil, fmt.Errorf("llm gateway is not configured")
	}
	if strings.TrimSpace(imageURL) == "" {
		return nil, fmt.Errorf("image_url is required")
	}
	prompt := s.resolvePromptByCode(ctx, tenantID, "content_verify_image_text", verifyImageTextPrompt) + "\n\n预期文字：\n" + expectedText
	content, err := s.multimodalCall(ctx, tenantID, imageURL, prompt)
	if err != nil {
		return nil, err
	}
	obj, err := extractJSONObject(content)
	if err != nil {
		return nil, err
	}
	result := &VerifyImageTextResult{Pass: true}
	if v, ok := obj["pass"].(bool); ok {
		result.Pass = v
	}
	if errs, ok := obj["errors"].([]any); ok {
		for _, e := range errs {
			if s, ok := e.(string); ok {
				result.Errors = append(result.Errors, s)
			}
		}
	}
	return result, nil
}

const contentLayoutUserFallback = "下面是公众号文章的 HTML（含行内样式）：\n\n{{article_html}}"

const contentLayoutExtractPrompt = `你是公众号排版分析专家。下面是公众号文章的 HTML（含行内样式）。分析它的排版，提取一套可复用的「排版 DNA」。返回 JSON：

{
 "bg":"正文内容区的底色 hex（放文字那块区域的背景色，不是浏览器页面的白）",
 "container":"容器 CSS（背景色、内边距）",
 "title":"标题 CSS",
 "lead":"导语 CSS",
 "heading":"小节标题 CSS",
 "paragraph":"正文 CSS",
 "quote":"引用块 CSS",
 "cta":"行动号召 CSS",
 "palette":{"accent":"强调色 hex（标题/序号/高亮主色）","text":"正文色 hex"},
 "heading_numbering":{"style":"badge|circle|line|none","shape":"rounded|circle","text_color":"hex","bg_color":"hex"},
 "divider":{"style":"solid|dashed|dotted|none","color":"hex","thickness":"1px"},
 "highlight":{"style":"marker|underline|none","color":"hex","opacity":0.15},
 "card":{"radius":数字,"shadow":true或false}
}

规则：CSS 只保留 font-size/color/font-weight/line-height/text-align/letter-spacing/margin/padding/border/background 这些属性；颜色用文章里真实出现的 hex；没有的元素写空串或 null 或 none；数字只写数值不带 px。只返回 JSON，不要解释。`


// ExtractArticleLayoutRequest 从公众号链接提取排版请求。
type ExtractArticleLayoutRequest struct {
	URL   string         `json:"url"`
	Name  string         `json:"name"`
	Style map[string]any `json:"style"` // 已提取的 style（保存时传入）
}

// fetchArticleHTML 服务端抓取文章 HTML（带 UA，绕过简单反爬）。
func fetchArticleHTML(ctx context.Context, url string) (string, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return "", fmt.Errorf("bad url: %w", err)
	}
	req.Header.Set("User-Agent", "Mozilla/5.0 (Macintosh; Intel Mac OS X 10_15_7) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/120.0 Safari/537.36")
	client := &http.Client{Timeout: 20 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return "", fmt.Errorf("fetch article failed: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 400 {
		return "", fmt.Errorf("fetch article http %d", resp.StatusCode)
	}
	body, err := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if err != nil {
		return "", err
	}
	return string(body), nil
}

var htmlTagRegex = regexp.MustCompile(`(?s)<script.*?</script>|<style.*?</style>|<!--.*?-->`)

// cleanArticleHTML 去掉脚本/样式/注释并截断，降低噪声。
func cleanArticleHTML(html string) string {
	out := htmlTagRegex.ReplaceAllString(html, "")
	const maxLen = 40000
	if len(out) > maxLen {
		out = out[:maxLen]
	}
	return out
}

// extractImageAssets 从文章 HTML 里抽取背景图 + 装饰图标（mmbiz 小图，去重，最多 6 个）。
func extractImageAssets(html string) (bg string, icons []string) {
	if m := regexp.MustCompile(`background(?:-image)?\s*:\s*url\([^)]*?(https?://[^)"'\\\\\s]+)`).FindStringSubmatch(html); len(m) > 1 {
		bg = strings.ReplaceAll(m[1], "&amp;", "&")
	}
	seen := map[string]bool{}
	for _, m := range regexp.MustCompile(`(?:src|data-src)=["'](https://mmbiz\.qpic\.cn/[^"']+)["']`).FindAllStringSubmatch(html, -1) {
		u := strings.ReplaceAll(m[1], "&amp;", "&")
		if u == bg || seen[u] || !strings.Contains(u, "640?") {
			continue
		}
		seen[u] = true
		icons = append(icons, u)
		if len(icons) >= 6 {
			break
		}
	}
	return bg, icons
}

// uploadRemoteImage 下载远程图片并上传 OSS；失败返回空（不阻断提取）。
func (s *Service) uploadRemoteImage(ctx context.Context, tenantID int64, source string) string {
	if s.ossClient == nil || strings.TrimSpace(source) == "" {
		return source
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, source, nil)
	if err != nil {
		return ""
	}
	resp, err := (&http.Client{Timeout: 30 * time.Second}).Do(req)
	if err != nil {
		return ""
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return ""
	}
	data, err := io.ReadAll(resp.Body)
	if err != nil || len(data) == 0 {
		return ""
	}
	ct := resp.Header.Get("Content-Type")
	ext := "png"
	if strings.Contains(ct, "jpeg") || strings.Contains(ct, "jpg") {
		ext = "jpg"
	} else if strings.Contains(ct, "gif") {
		ext = "gif"
	}
	if !strings.HasPrefix(ct, "image/") {
		ct = "image/png"
	}
	key := oss.GenerateObjectKey(fmt.Sprintf("content/asset/%d", tenantID), "."+ext)
	url, err := s.ossClient.UploadBytes(ctx, key, data, &oss.UploadOptions{ContentType: ct})
	if err != nil {
		return ""
	}
	return url
}

// ExtractArticleLayout 从公众号链接提取排版模板，落库 content_style_templates(type=layout)。
func (s *Service) ExtractArticleLayout(ctx context.Context, tenantID int64, req ExtractArticleLayoutRequest) (map[string]any, error) {
	if s.llmClient == nil {
		return nil, fmt.Errorf("llm gateway is not configured")
	}
	name := strings.TrimSpace(req.Name)

	// 拿到 style：要么用传入的（保存模式），要么抓取提取（提取模式）
	var style map[string]any
	var rawHTML string
	var err error
	if len(req.Style) > 0 {
		style = req.Style
	} else {
		if strings.TrimSpace(req.URL) == "" {
			return nil, fmt.Errorf("url is required")
		}
		rawHTML, err = fetchArticleHTML(ctx, req.URL)
		if err != nil {
			return nil, err
		}
		html := cleanArticleHTML(rawHTML)
		selectedModel, err := s.resolveLLMModelConfig(ctx, tenantID, []string{"content_layout_extract", "content_generation", "chat"})
		if err != nil {
			return nil, fmt.Errorf("resolve llm config for layout extract: %w", err)
		}
		userTpl := s.resolvePromptUserTemplate(ctx, "content_layout_extract", contentLayoutUserFallback)
		userPrompt := strings.ReplaceAll(userTpl, "{{article_html}}", html)
		llmReq := llmgateway.TextInferenceRequest{
			TenantID:      tenantID,
			CallerService: "lingce-api",
			CallerModule:  contentCallerModule("layout_extract"),
			FunctionType:  selectedModel.FunctionType,
			Provider:      selectedModel.Provider,
			ModelCode:     selectedModel.ModelCode,
			Billing:       newBillingMetadata("content", "layout", 0, "layout_extract", "layout_extract"),
			Messages: []llmgateway.Message{
				{Role: "system", Content: s.resolvePromptByCode(ctx, tenantID, "content_layout_extract", contentLayoutExtractPrompt)},
				{Role: "user", Content: userPrompt},
			},
			Params: &llmgateway.Params{TimeoutSeconds: 120},
		}
		applyModelParamsToLLMRequest(&llmReq, selectedModel.ModelParams)
		llmResp, err := s.llmClient.TextInference(ctx, llmReq)
		if err != nil {
			return nil, fmt.Errorf("extract layout: %w", err)
		}
		style, err = extractJSONObject(llmResp.Content)
		if err != nil {
			return nil, err
		}
	}

	// 图片类装饰：背景图 + 图标素材（下载转存 OSS）
	if bgURL, iconURLs := extractImageAssets(rawHTML); bgURL != "" || len(iconURLs) > 0 {
		if bgURL != "" {
			if up := s.uploadRemoteImage(ctx, tenantID, bgURL); up != "" {
				style["background"] = map[string]any{"color": style["bg"], "image": up}
			}
		}
		if len(iconURLs) > 0 {
			assets := make([]map[string]any, 0, len(iconURLs))
			for _, u := range iconURLs {
				if up := s.uploadRemoteImage(ctx, tenantID, u); up != "" {
					assets = append(assets, map[string]any{"url": up})
				}
			}
			if len(assets) > 0 {
				style["assets"] = assets
			}
		}
	}

	// 小标题装饰图：序号图（小近方形）+ 横幅图（扁平宽），按宽高比识别
	if badgeURL, bannerURL := s.classifyHeadingDecorations(ctx, rawHTML); badgeURL != "" || bannerURL != "" {
		deco := map[string]any{}
		if badgeURL != "" {
			deco["badge_image"] = s.uploadRemoteImage(ctx, tenantID, badgeURL)
		}
		if bannerURL != "" {
			deco["banner_image"] = s.uploadRemoteImage(ctx, tenantID, bannerURL)
		}
		if len(deco) > 0 {
			style["heading_decoration"] = deco
		}
	}

	// name 为空：只提取不保存（预览用）
	if name == "" {
		return map[string]any{"type": "layout", "style": style}, nil
	}

	styleJSON, _ := json.Marshal(style)
	var id int64
	err = s.store.pool.QueryRow(ctx, `
		INSERT INTO content_style_templates (tenant_id, level, industry, type, name, reference_url, extracted_style)
		VALUES ($1,'tenant','','layout',$2,$3,$4::jsonb)
		RETURNING id`,
		tenantID, name, req.URL, string(styleJSON),
	).Scan(&id)
	if err != nil {
		return nil, fmt.Errorf("insert layout template: %w", err)
	}
	return map[string]any{"id": id, "name": name, "type": "layout", "style": style}, nil
}

// collectImageURLs 收集文章里所有候选图片 URL（背景图 + img src），去重保序。
func collectImageURLs(html string) []string {
	seen := map[string]bool{}
	var out []string
	add := func(u string) {
		u = strings.ReplaceAll(strings.ReplaceAll(u, "&amp;", "&"), "\\x26", "&")
		if u == "" || seen[u] || !strings.HasPrefix(u, "http") {
			return
		}
		seen[u] = true
		out = append(out, u)
	}
	for _, m := range regexp.MustCompile(`background(?:-image)?\s*:\s*url\([^)]*?(https?://[^)"'\\\s]+)`).FindAllStringSubmatch(html, -1) {
		add(m[1])
	}
	for _, m := range regexp.MustCompile(`(?:src|data-src)=["'](https?://[^"']+)["']`).FindAllStringSubmatch(html, -1) {
		add(m[1])
	}
	if len(out) > 24 {
		out = out[:24]
	}
	return out
}

// downloadImageSize 只解码图片头拿宽高，不整张解码。
func downloadImageSize(ctx context.Context, url string) (int, int, bool) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return 0, 0, false
	}
	req.Header.Set("User-Agent", "Mozilla/5.0")
	resp, err := (&http.Client{Timeout: 15 * time.Second}).Do(req)
	if err != nil {
		return 0, 0, false
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return 0, 0, false
	}
	cfg, _, err := image.DecodeConfig(io.LimitReader(resp.Body, 1<<20))
	if err != nil {
		return 0, 0, false
	}
	return cfg.Width, cfg.Height, true
}

// classifyHeadingDecorations 按宽高比识别「小节序号图」（小且近方形）与「小标题横幅图」（扁平且宽）。
func (s *Service) classifyHeadingDecorations(ctx context.Context, html string) (badge, banner string) {
	for _, u := range collectImageURLs(html) {
		w, h, ok := downloadImageSize(ctx, u)
		if !ok || w == 0 || h == 0 {
			continue
		}
		ratio := float64(w) / float64(h)
		if banner == "" && ratio >= 2.6 && w >= 280 {
			banner = u
		} else if badge == "" && ratio >= 0.7 && ratio <= 1.5 && w <= 220 {
			badge = u
		}
		if badge != "" && banner != "" {
			break
		}
	}
	return badge, banner
}
