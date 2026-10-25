package content

import (
	"context"
	"encoding/json"
	"log/slog"
	"strings"

	"github.com/jackc/pgx/v5"
)

// 提示词注册表：内容获客的提示词统一走「旧提示词管理模块」
// (recording_analysis_prompts + recording_analysis_tenant_configs)，
// 由 /recordings/prompts 接口与 operation-console「创造方案」页管理，支持机构覆盖。
//
// 设计：
//   - system_prompt / user_prompt_template 由 DB 拥有（运营可改、机构可覆盖、可在线测试）。
//   - 输出必须是 JSON：代码在提示词末尾拼结构契约，避免散文导致解析崩坏。

// formPromptCode 形式 → 旧提示词模块的 code。
var formPromptCode = map[string]string{
	"article":    "content_article_gen",
	"graphic":    "content_graphic_gen",
	"script":     "content_script_gen",
	"storyboard": "content_storyboard_gen",
	"poster":     "content_poster_gen",
	"moments":    "content_moments_gen",
}

// formJsonContract 每种形式的输出结构契约（代码保证，DB 只负责创作指令）。
var formJsonContract = map[string]string{
	"article":    `{"title":"标题","body":"正文（可含 markdown 小标题/加粗）","publish_package":{"summary":"摘要（分享时显示）","share_copy":"分享朋友圈的推荐文案"}}`,
	"script":     `{"segments":[{"phase":"开场","words":"口播词","action":"画面/动作提示","subtitle":"字幕关键句","duration":"0-3s"},{"phase":"正文","words":"...","action":"...","subtitle":"...","duration":"3-24s"},{"phase":"结尾","words":"...","action":"...","subtitle":"...","duration":"24-30s"}],"publish_package":{"title":"视频标题","tags":["#话题"],"description":"视频描述"}}`,
	"storyboard": `{"shots":[{"scene":"景别(特写/中景/全景)","camera":"运镜(固定/推/摇)","dialogue":"台词","duration":"时长","subtitle":"字幕","description":"画面描述(给手绘分镜用)"}]}`,
	"poster":     `{"title":"主标题","subtitle":"副标题","bullets":["信息点1","信息点2","信息点3"],"publish_package":{"copy":"发布配文","tags":["#话题"],"cta":"行动号召"}}`,
	"moments":    `{"copy":"朋友圈配文(注意分行和emoji)","image_points":["图片要点1","图片要点2","图片要点3"],"publish_package":{"mention":"@提醒(可空)","visibility_note":"可见范围提醒"}}`,
	"graphic": `{"cover_title":"封面标题（≤20字，有钩子）","cover_subtitle":"封面副标题","cards":["卡片1文案","卡片2文案","卡片3文案"],` +
		`"publish_package":{"title":"发布标题","tags":["#话题1","#话题2"],"pinned_comment":"首条评论（补充信息或CTA）"}}`,
}

// formCreativeDefault 内置兜底（DB 模块不可用时）。
var formCreativeDefault = map[string]string{
	"article":    "你是医疗科普公众号作者。根据选题，写一篇公众号文章文稿。要求：正文要有小标题分段、专业准确、不承诺疗效。",
	"script":     "你是医疗短视频口播稿策划。根据选题，写一条 30 秒口播稿。要求：口语化、有钩子、有记忆点；字幕是关键词不是全文。",
	"storyboard": "你是医疗短视频分镜师。根据选题，写分镜脚本。要求：3~5 个镜头，画面描述具体到人物动作和构图。",
	"poster":     "你是医疗科普海报文案。根据选题，写海报文案。要求：主标题短而有钩子，信息点简洁。",
	"moments":    "你是医疗科普朋友圈文案。根据选题，写朋友圈内容。要求：配文口语化、有温度，不超过6行避免折叠。",
	"graphic": "你是小红书医疗科普笔记策划。根据下面的选题，生成一篇笔记的完整文稿。要求：" +
		"1. 文案口语化、有记忆点，emoji 分段。2. 用上证据里的金句/原话（如果有）。3. 每张卡片讲一个点，不堆砌。4. 不承诺疗效、不出现患者正脸描述。",
}

// promptTemplate 旧提示词模块里的一条 prompt。
type promptTemplate struct {
	SystemPrompt string
	UserPrompt   string
}

// getPromptOutputSchema 从旧提示词模块取 output_schema（JSON）。
func (s *Store) getPromptOutputSchema(ctx context.Context, code string) (json.RawMessage, error) {
	var schema json.RawMessage
	err := s.pool.QueryRow(ctx, `
		SELECT COALESCE(output_schema, '{}'::json)
		FROM recording_analysis_prompts
		WHERE code = $1 AND is_active = true
		ORDER BY id DESC LIMIT 1`, code).Scan(&schema)
	if err == pgx.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return schema, nil
}

// getPromptByCode 从旧提示词模块按 code 取活跃 prompt。
func (s *Store) getPromptByCode(ctx context.Context, code string) (*promptTemplate, error) {
	var p promptTemplate
	err := s.pool.QueryRow(ctx, `
		SELECT COALESCE(system_prompt,''), COALESCE(user_prompt_template,'')
		FROM recording_analysis_prompts
		WHERE code = $1 AND is_active = true
		ORDER BY id DESC LIMIT 1`, code).
		Scan(&p.SystemPrompt, &p.UserPrompt)
	if err == pgx.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &p, nil
}

// getTenantPromptOverride 取机构覆盖（旧模块的 recording_analysis_tenant_configs）。
func (s *Store) getTenantPromptOverride(ctx context.Context, tenantID int64, code string) (*promptTemplate, error) {
	var p promptTemplate
	err := s.pool.QueryRow(ctx, `
		SELECT COALESCE(custom_system_prompt,''), COALESCE(custom_user_prompt_template,'')
		FROM recording_analysis_tenant_configs
		WHERE tenant_id = $1 AND prompt_code = $2 AND is_enabled = true
		ORDER BY priority DESC NULLS LAST, id DESC LIMIT 1`, tenantID, code).
		Scan(&p.SystemPrompt, &p.UserPrompt)
	if err == pgx.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &p, nil
}

// resolveDraftSystemPrompt 解析某形式的系统提示词：旧提示词模块（含机构覆盖）优先，
// 否则内置兜底；末尾统一拼代码保证的 JSON 结构契约。
func (s *Service) resolveDraftSystemPrompt(ctx context.Context, tenantID int64, form string) (systemPrompt string, modelRef string, source string) {
	creative := formCreativeDefault[form]
	contract := formJsonContract[form]
	source = "code"

	code, ok := formPromptCode[form]
	if !ok {
		return creative + "\n\n## 输出格式（必须严格遵守）\n只返回一个 JSON 对象，不要任何解释、不要 markdown 代码块：\n" + contract, "", source
	}

	if p, err := s.store.getPromptByCode(ctx, code); err != nil {
		slog.Warn("load prompt module failed", "error", err, "form", form, "code", code)
	} else if p != nil {
		if strings.TrimSpace(p.SystemPrompt) != "" {
			creative = strings.TrimSpace(p.SystemPrompt)
			source = "db:" + code
		}
		if strings.TrimSpace(p.UserPrompt) != "" {
			contract = strings.TrimSpace(p.UserPrompt)
			source += "+user"
		}
	}

	if o, err := s.store.getTenantPromptOverride(ctx, tenantID, code); err != nil {
		slog.Warn("load tenant prompt override failed", "error", err, "tenant", tenantID, "code", code)
	} else if o != nil {
		if strings.TrimSpace(o.SystemPrompt) != "" {
			creative = strings.TrimSpace(o.SystemPrompt)
			source += "+tenant"
		}
		if strings.TrimSpace(o.UserPrompt) != "" {
			contract = strings.TrimSpace(o.UserPrompt)
			source += "+tenantUser"
		}
	}

	systemPrompt = creative + "\n\n## 输出格式（必须严格遵守）\n只返回一个 JSON 对象，不要任何解释、不要 markdown 代码块：\n" + contract
	return systemPrompt, modelRef, source
}

// resolvePromptByCode 按 code 从旧提示词模块取 prompt（含机构覆盖）；DB 无则用 fallback。
// 用于非创作类 prompt（选题/补全/计划/归并/AI先答/学模板/明验/洞察提取）。
func (s *Service) resolvePromptByCode(ctx context.Context, tenantID int64, code, fallback string) string {
	prompt := fallback
	if p, err := s.store.getPromptByCode(ctx, code); err != nil {
		slog.Warn("load prompt module failed", "error", err, "code", code)
	} else if p != nil && strings.TrimSpace(p.SystemPrompt) != "" {
		prompt = strings.TrimSpace(p.SystemPrompt)
		if strings.TrimSpace(p.UserPrompt) != "" {
			prompt = prompt + "\n\n" + strings.TrimSpace(p.UserPrompt)
		}
	}
	if o, err := s.store.getTenantPromptOverride(ctx, tenantID, code); err == nil && o != nil {
		if strings.TrimSpace(o.SystemPrompt) != "" {
			prompt = strings.TrimSpace(o.SystemPrompt)
		}
	}
	return prompt
}
