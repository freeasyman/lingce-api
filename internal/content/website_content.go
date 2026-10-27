package content

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/freeasyman/lingce-api/pkg/llmgateway"
)

// websiteColumnTemplates 官网 4 个栏目的页面结构（生成时注入到提示词）。
var websiteColumnTemplates = map[string]string{
	"医生页":  "医生介绍页，结构：医生简介、擅长领域、出诊时间、预约方式",
	"项目页":  "项目介绍页，结构：项目介绍、适应人群、流程、价格或医保、常见疑问",
	"FAQ":  "问答页，一问一答，直接回答患者的高频问题",
	"机构介绍": "机构介绍页，结构：机构简介、资质、科室、地址交通、联系方式",
}

// websiteColumns 允许的官网栏目顺序。
var websiteColumns = []string{"医生页", "项目页", "FAQ", "机构介绍"}

// GenerateWebsiteContent 直接生成一条官网栏目内容（不依赖诊断）。
func (s *Service) GenerateWebsiteContent(ctx context.Context, tenantID int64, column, subject string) (map[string]any, error) {
	if s.llmClient == nil {
		return nil, fmt.Errorf("llm gateway is not configured")
	}
	column = strings.TrimSpace(column)
	template, ok := websiteColumnTemplates[column]
	if !ok {
		return nil, NewBizError("BAD_COLUMN", "官网栏目不合法，可选：医生页 / 项目页 / FAQ / 机构介绍")
	}
	channel := "官网-" + column
	subject = strings.TrimSpace(subject)

	orgName := "我们机构"
	if profile, err := s.store.GetContentOrgProfile(ctx, tenantID); err == nil && profile != nil && profile.InstitutionName != "" {
		orgName = profile.InstitutionName
	}

	sel, err := s.resolveLLMModelConfig(ctx, tenantID, []string{"content_article", "chat"})
	if err != nil {
		return nil, err
	}

	// 机构介绍不需要额外主题，用机构名
	theme := subject
	if theme == "" {
		theme = orgName
	}

	system := fmt.Sprintf(`你是医疗官网内容专家，为「%s」生成官网%s。%s。
输出 JSON：{"title":"标题","body":"正文（分段落，可直接发布）"}。
要求：
  · 医疗合规：不承诺疗效、不制造焦虑、不诱导就诊
  · 内容客观、信息可查证，方便 AI（豆包/千问/DeepSeek）引用
  · 只返回 JSON`, orgName, column, template)

	req := llmgateway.TextInferenceRequest{
		TenantID:      tenantID,
		CallerService: "lingce-api",
		CallerModule:  contentCallerModule("website_generate"),
		FunctionType:  sel.FunctionType,
		Provider:      sel.Provider,
		ModelCode:     sel.ModelCode,
		Messages: []llmgateway.Message{
			{Role: "system", Content: system},
			{Role: "user", Content: fmt.Sprintf("机构：%s\n主题：%s", orgName, theme)},
		},
	}
	applyModelParamsToLLMRequest(&req, sel.ModelParams)
	resp, err := s.llmClient.TextInference(ctx, req)
	if err != nil {
		return nil, err
	}
	obj, err := extractJSONObject(resp.Content)
	if err != nil {
		return nil, err
	}
	title := strings.TrimSpace(fmt.Sprint(obj["title"]))
	body := strings.TrimSpace(fmt.Sprint(obj["body"]))
	if title == "" || title == "<nil>" {
		title = column + "：" + theme
	}

	id, err := s.store.insertContentItemWithSource(ctx, tenantID, 0, 0, "article", title, body)
	if err != nil {
		return nil, err
	}
	if err := s.store.updateContentItemChannel(ctx, tenantID, id, channel); err != nil {
		return nil, err
	}
	return map[string]any{"content_id": id, "title": title, "channel": channel, "column": column}, nil
}

// WebsiteContentTemplate 对外返回栏目结构说明（前端展示用，可选）。
func websiteTemplateJSON() json.RawMessage {
	m := map[string]string{}
	for _, c := range websiteColumns {
		m[c] = websiteColumnTemplates[c]
	}
	b, _ := json.Marshal(m)
	return b
}
