package content

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"strings"
	"sync"

	"github.com/freeasyman/lingce-api/pkg/llmgateway"
)

// AI 答案可见性：同一患者问题，在多个 AI 引擎各问一遍，
// 逐引擎分析（是否提到机构/医生、信源、竞品、事实错误），最后汇总缺口诊断。

const aiVisibilityAnswerPrompt = `你是一个通用医疗 AI 助手（类似豆包、元宝）。请直接回答患者的问题。
遇到评价性/建议性问题（例如"某家医院怎么样""某地哪家医院好"），要基于公开信息正面回答：
查到该机构的公开资料就客观介绍（资质、科室、医生、口碑、注意事项等）；
没查到就说明没有找到公开信息，不要因为"保持中立"就直接拒答。
回答控制在 400 字以内。`

const aiVisibilityAnalyzePrompt = `你是医疗内容运营专家。下面是患者问题和某个通用 AI 助手的回答。
请分析这个回答，输出 JSON（我方机构名：%s；我方医生：%s）：
{
  "mentioned_org": false,
  "mentioned_doctor": false,
  "sources": [],
  "competitors": [],
  "fact_errors": [],
  "local_hint": ""
}
mentioned_org = AI 回答里是否提到了我方机构（按机构名或常见别称判断）；
mentioned_doctor = AI 回答里是否提到了我方的医生（只认上面「我方医生」名单里的姓名，提到别家或无关医生不算）；
sources = AI 回答里提到的信源/引用来源（如官网、公众号、某平台），没有就空数组；
competitors = AI 推荐的竞品机构/医生，没有就空数组；
fact_errors = AI 关于我方机构的事实错误或缺失，没有就空数组；
local_hint = 有没有给出本地具体指向（一句话）。
只返回 JSON。`

const aiVisibilityGapPrompt = `你是医疗机构「AI 可见性」诊断顾问。你只服务下面这一家机构（下文称「我方机构」），不是给行业写评论。
输入是：同一个患者问题在多个 AI 引擎的回答原文，以及每个引擎的结构化判定
（mentioned_org 是否提到我方机构、mentioned_doctor 是否提到我方医生、competitors 竞品、fact_errors 事实错误、local_hint 本地指向、sources 信源）。
输入第一行会给出「我方机构」的名称和「我方医生」名单，回答里就用这个名字称呼它；名称显示为「我们机构」时，一律用「你们」，不许编造机构名。

【铁律】
1. 只诊断这家机构。凡是换一家医院也成立的话，一律不许写。典型反面例子（禁止出现）：
   「行业分诊逻辑缺失」「某类技术未被结构化」「就医服务信息断层」「患者教育不足」「缺乏统一的决策路径」「民营/区级机构能力未被覆盖」。
2. 结论只能来自输入里的引擎判定（analysis）和回答原文（answer）。
   不得引入未提供的行业数据、排名、患者量、市场规模、政策、竞品内部资料；不许编造任何数字、比例、时间、出处。
   凡引用引擎说法，必须是回答原文里真实出现过的句子，或 competitors 里真实出现过的名字。
3. 每写一条 gap 都要能回答「这条说的是这家机构自己的什么事」。
   写完自检一遍：把句子里的「你们」换成「任何一家医院」，如果这句话照样成立，就删掉重写。
4. 不许写「加强内容建设」「提升品牌曝光」「优化搜索引擎」「做好内容运营」这类无法执行的口号。
5. 输出是给机构负责人看的自然语言，一律禁止出现内部字段名：mentioned_org、mentioned_doctor、competitors、fact_errors、local_hint、sources。
   要表达时用「提到你们」「提到你们的医生」「竞品/被推荐的机构」「事实错误」「本地指向」「信源」这些说法。

【先判定属于哪一种情况，再写】
A. 没有任何一个引擎提到你们 —— 你们在这几个 AI 眼里不存在。
   gap 必须围绕「你们没被 AI 写进去」和它的具体后果来写，至少覆盖：
   - 患者被分流到哪：逐个引擎点名回答里实际推荐的竞品机构/科室/医生（只能取 competitors 与回答原文里的名字），
     说明问这个问题的患者会在这一步被带去谁那里；
   - 没有一条关于你们的事实错误，不代表你们没问题：是因为 AI 根本没写你们，连写错的机会都没有——
     零提及比写错更严重，你们连「被纠正」的入口都没有；
   - 信源层面：结合 sources 和回答里出现的信源（域名、平台名、或哪家机构的官网/公众号），
     说明 AI 实际读的是谁的内容，而你们的官网、公众号内容完全没有被收录采信；
   - 事实空白：这些回答里没有任何一条来自你们的事实（科室、专家、技术、门诊时间、地址、接诊范围、预约方式），
     患者看完 AI 的回答，仍然不知道你们能看这个病、找谁看、怎么挂号。
B. 有引擎提到了你们 —— 你们的机构进了 AI 的回答。
   gap 只能写「关于我方机构的事实错误、信息缺失、口径不一致」，并做到：
   - 逐条引用引擎原文里的错误说法（引原句 + 标明哪个引擎），说明错在哪；
     正确口径只能用输入里已有的事实，输入没给的写「与你们口径不符，需核对」，不许自己编一个「正确说法」；
   - 横向对比多个引擎对我方机构的说法是否一致，不一致就指出是哪几家引擎、差在哪一句；
   - 指出残缺：只提了名字、没提科室/专家/地址/接诊范围/预约方式，等于没被真正写进去。
C. 有引擎提到了你们的医生 —— 对医生做同样的判断：
   姓名、头衔、科室、擅长方向是否写错或张冠李戴；是否只有名字、没有任何可挂号信息（门诊时间/预约方式）。
A 与 B/C 同时存在时：以 B/C 为主体写，但必须点明「哪几个引擎里你们依然不存在」，以及那部分患者被分流给了谁。

【三段输出】
- source_analysis：只写与这家机构有关的信源结论。逐个列出 AI 回答里实际出现的来源（域名/平台名/哪家机构官网或公众号），
  区分「你们能影响的」和「完全没出现你们内容的」，最后给一句明确结论：你们官网/公众号是否被收录采信。
- gap：按上面判定的情况写，分条编号（1. 2. 3.），每条都要落到具体竞品名、具体科室、具体引擎原句或具体缺失字段。
- advice：给这家机构明天就能执行的动作，分条编号，每条注明「对应 gap 第 X 条」，并且必须点名：
  1) 官网：补哪个栏目/页面（栏目名 + 页面标题 + 页面必须出现的字段，如科室、专家、门诊时间、地址、预约入口）；
  2) 公众号：写什么（给 2~3 个具体文章标题，说明每篇对应哪个问题、要压过哪个竞品说法）；
  3) GEO：具体怎么调（在哪些平台铺什么内容、结构化数据/FAQ、NAP 一致性、sitemap 与收录提交、便于 AI 抓取摘录的写法）。
  没有把握的量化承诺（如「提升 30%」「排名前三」）一律不写。

【输出格式】
只返回一个 JSON 对象，不要 markdown 代码块，不要任何解释文字：
{"source_analysis":"...","gap":"...","advice":"..."}
三个字段都是中文字符串，可用 \n 分段，分条用 1. 2. 3. 编号，不要嵌套 JSON，不要用 * 或 # 标题符号。`

// aiVisibilityDoctorNames 取机构知识库录入的医生名单（姓名 + 头衔），
// 用于判断 AI 回答是否提到了我方医生；没录入时返回空串。
func (s *Service) aiVisibilityDoctorNames(ctx context.Context, tenantID int64) string {
	k, err := s.store.getOrgKnowledge(ctx, tenantID)
	if err != nil || k == nil {
		return ""
	}
	var doctors []map[string]any
	if json.Unmarshal(k.Doctors, &doctors) != nil || len(doctors) == 0 {
		return ""
	}
	items := make([]string, 0, len(doctors))
	for _, d := range doctors {
		name := strings.TrimSpace(fmt.Sprint(d["name"]))
		if name == "" {
			continue
		}
		head := name
		if t := strings.TrimSpace(fmt.Sprint(d["title"])); t != "" && t != "<nil>" {
			head += " " + t
		}
		items = append(items, head)
	}
	return strings.Join(items, "、")
}

// aiEngineAnswer 一个引擎的回答 + 分析。
type aiEngineAnswer struct {
	Engine        string         `json:"engine"`
	Provider      string         `json:"provider"`
	ModelCode     string         `json:"model_code"`
	Answer        string         `json:"answer"`
	Sources       []searchResult `json:"sources,omitempty"`
	SearchQueries []string       `json:"search_queries,omitempty"`
	SearchCount   int            `json:"search_count,omitempty"`
	Error         string         `json:"error,omitempty"`
	Analysis      map[string]any `json:"analysis,omitempty"`
}

// listAIEngines 取所有启用的 ai_assistant 引擎配置（按 provider+model 去重）。
func (s *Service) listAIEngines(ctx context.Context, tenantID int64) ([]llmModelSelection, error) {
	query := `
		SELECT COALESCE(function_type,''), COALESCE(provider,''), COALESCE(model_code,''), COALESCE(model_params, extra_params, '{}'::json)
		FROM llm_model_configs
		WHERE deleted_at IS NULL AND COALESCE(is_active,true)=true
		  AND function_type = 'ai_assistant'
		  AND tenant_id IN ($1, 0)
		ORDER BY CASE WHEN tenant_id=$1 THEN 0 ELSE 1 END, COALESCE(is_default,false), id`
	rows, err := s.store.pool.Query(ctx, query, tenantID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	seen := map[string]bool{}
	var out []llmModelSelection
	for rows.Next() {
		var sel llmModelSelection
		if err := rows.Scan(&sel.FunctionType, &sel.Provider, &sel.ModelCode, &sel.ModelParams); err != nil {
			return nil, err
		}
		key := sel.Provider + "|" + sel.ModelCode
		if seen[key] {
			continue
		}
		seen[key] = true
		out = append(out, sel)
	}
	return out, rows.Err()
}

// engineLabel 引擎显示名。
func engineLabel(provider, modelCode string) string {
	switch strings.ToLower(provider) {
	case "dashscope":
		return "千问"
	case "deepseek":
		return "DeepSeek"
	case "doubao":
		return "豆包"
	case "hunyuan":
		return "元宝"
	}
	return provider + "/" + modelCode
}

// aiEngineAnswer 先搜公开资料，再让引擎基于资料作答，同时返回真实信源。
func (s *Service) aiEngineAnswer(ctx context.Context, tenantID int64, sel llmModelSelection, question string) (string, []searchResult, []string, int, error) {
	// 每家用自己的联网搜索，保证"患者问这家 AI 时真实会看到什么"。
	//   deepseek  → 自己的搜索 API（能拿到结构化信源 URL）
	//   千问/豆包 → 各自厂商的联网搜索（信源由答案分析提取，厂商接口不返回 URL）
	var sources []searchResult
	userContent := question
	enableSearch := true
	if sel.Provider == "deepseek" {
		var serr error
		sources, serr = s.searchWeb(ctx, tenantID, question, 8)
		if serr != nil {
			slog.Warn("ai visibility: deepseek search failed", "error", serr)
		}
		if len(sources) > 0 {
			var b strings.Builder
			b.WriteString("【网上搜到的公开资料】\n")
			for i, r := range sources {
				fmt.Fprintf(&b, "[%d] %s（%s）\n%s\n", i+1, r.Title, r.SiteName, r.Summary)
			}
			userContent = b.String() + "\n【患者问题】" + question
		}
		// DeepSeek 已经把资料喂进来了，不需要再开它自己的搜索
		enableSearch = false
	}
	req := llmgateway.TextInferenceRequest{
		TenantID:      tenantID,
		CallerService: "lingce-api",
		CallerModule:  contentCallerModule("ai_visibility_answer"),
		FunctionType:  sel.FunctionType,
		Provider:      sel.Provider,
		ModelCode:     sel.ModelCode,
		Billing: &llmgateway.BillingMetadata{
			BusinessDomain:     "content",
			BusinessObjectType: "ai_visibility_run",
			BillingSubject:     "ai_visibility_answer",
			BillingScene:       "ai_visibility",
			CostLayer:          "product",
		},
		Messages: []llmgateway.Message{
			{Role: "system", Content: s.resolvePromptByCode(ctx, tenantID, "content_ai_visibility_answer", aiVisibilityAnswerPrompt)},
			{Role: "user", Content: userContent},
		},
	}
	applyModelParamsToLLMRequest(&req, sel.ModelParams)
	if req.Params == nil {
		req.Params = &llmgateway.Params{}
	}
	req.Params.EnableSearch = &enableSearch
	// 联网检索 + 长回答偶发很慢（豆包/千问都可能 1~3 分钟），给足 300 秒
	req.Params.TimeoutSeconds = 300
	resp, err := s.llmClient.TextInference(ctx, req)
	if err != nil {
		return "", sources, nil, 0, err
	}
	// 厂商返回引用（豆包 annotations）时优先用它，比 DeepSeek 搜索更贴合本引擎答案
	if len(resp.Citations) > 0 {
		sources = make([]searchResult, 0, len(resp.Citations))
		for _, c := range resp.Citations {
			sources = append(sources, searchResult{Title: c.Title, URL: c.URL, SiteName: c.SiteName, Date: c.PublishTime})
		}
	}
	return strings.TrimSpace(resp.Content), sources, resp.SearchQueries, resp.SearchCount, nil
}

// aiEngineAnalyze 分析一个引擎的回答。
func (s *Service) aiEngineAnalyze(ctx context.Context, tenantID int64, sel llmModelSelection, question, answer, orgName, doctorNames string) (map[string]any, error) {
	prompt := fmt.Sprintf(s.resolvePromptByCode(ctx, tenantID, "content_ai_visibility_analyze", aiVisibilityAnalyzePrompt), orgName, doctorNames)
	req := llmgateway.TextInferenceRequest{
		TenantID:      tenantID,
		CallerService: "lingce-api",
		CallerModule:  contentCallerModule("ai_visibility_analyze"),
		FunctionType:  sel.FunctionType,
		Provider:      sel.Provider,
		ModelCode:     sel.ModelCode,
		Billing: &llmgateway.BillingMetadata{
			BusinessDomain:     "content",
			BusinessObjectType: "ai_visibility_run",
			BillingSubject:     "ai_visibility_analyze",
			BillingScene:       "ai_visibility",
			CostLayer:          "product",
		},
		Messages: []llmgateway.Message{
			{Role: "system", Content: prompt},
			{Role: "user", Content: fmt.Sprintf("患者问题：%s\n\nAI 回答：%s", question, answer)},
		},
	}
	applyModelParamsToLLMRequest(&req, sel.ModelParams)
	if req.Params == nil {
		req.Params = &llmgateway.Params{}
	}
	req.Params.TimeoutSeconds = 120
	resp, err := s.llmClient.TextInference(ctx, req)
	if err != nil {
		return nil, err
	}
	return extractJSONObject(resp.Content)
}

// aiVisibilityGap 汇总缺口诊断。
// 缺口是「针对这家机构」的诊断，所以要把机构名/医生名单一起给模型：
// 否则模型只知道「我方机构」这个词，写出来必然是行业级评论。
func (s *Service) aiVisibilityGap(ctx context.Context, tenantID int64, question, orgName, doctorNames string, results []aiEngineAnswer) (map[string]any, error) {
	// 缺口诊断用通用快模型（别用豆包，它长上下文要 100s+）
	sel, err := s.resolveLLMModelConfig(ctx, tenantID, []string{"content_topic", "chat", "ai_assistant"})
	if err != nil {
		return nil, err
	}
	var b strings.Builder
	fmt.Fprintf(&b, "患者问题：%s\n我方机构：%s\n我方医生：%s\n\n", question, orgName, doctorNames)
	for _, r := range results {
		fmt.Fprintf(&b, "引擎 %s：\n回答：%s\n分析：%v\n\n", r.Engine, r.Answer, r.Analysis)
	}
	req := llmgateway.TextInferenceRequest{
		TenantID:      tenantID,
		CallerService: "lingce-api",
		CallerModule:  contentCallerModule("ai_visibility_gap"),
		FunctionType:  sel.FunctionType,
		Provider:      sel.Provider,
		ModelCode:     sel.ModelCode,
		Billing: &llmgateway.BillingMetadata{
			BusinessDomain:     "content",
			BusinessObjectType: "ai_visibility_run",
			BillingSubject:     "ai_visibility_gap",
			BillingScene:       "ai_visibility",
			CostLayer:          "product",
		},
		Messages: []llmgateway.Message{
			{Role: "system", Content: s.resolvePromptByCode(ctx, tenantID, "content_ai_visibility_gap", aiVisibilityGapPrompt)},
			{Role: "user", Content: b.String()},
		},
	}
	applyModelParamsToLLMRequest(&req, sel.ModelParams)
	if req.Params == nil {
		req.Params = &llmgateway.Params{}
	}
	req.Params.TimeoutSeconds = 120
	resp, err := s.llmClient.TextInference(ctx, req)
	if err != nil {
		return nil, err
	}
	return extractJSONObject(resp.Content)
}

// StartAIVisibility 创建异步诊断任务，立即返回 run_id；后台跑多引擎。
func (s *Service) StartAIVisibility(ctx context.Context, tenantID int64, question, region string, insightID, topicID int64) (int64, error) {
	if s.llmClient == nil {
		return 0, fmt.Errorf("llm gateway is not configured")
	}
	question = strings.TrimSpace(question)
	if question == "" {
		return 0, fmt.Errorf("question is required")
	}
	// AI 诊断入口的交付前预检：prompt 1 题次。额度不足拒绝开始诊断。
	if err := s.ensureContentQuota(ctx, tenantID, contentBillingUnitPrompt, 1); err != nil {
		return 0, err
	}
	region = strings.TrimSpace(region)

	orgName := "我们机构"
	if profile, err := s.store.GetContentOrgProfile(ctx, tenantID); err == nil && profile != nil && profile.InstitutionName != "" {
		orgName = profile.InstitutionName
	}
	doctorNames := s.aiVisibilityDoctorNames(ctx, tenantID)
	if doctorNames == "" {
		doctorNames = "（未录入）"
	}

	q := question
	if region != "" && !strings.Contains(question, region) {
		q = region + "，" + question
	}

	engines, err := s.listAIEngines(ctx, tenantID)
	if err != nil {
		return 0, err
	}
	if len(engines) == 0 {
		return 0, fmt.Errorf("未配置 AI 引擎（ai_assistant）")
	}

	runID, err := s.store.createAIVisibilityRun(ctx, tenantID, q, region, orgName, len(engines), insightID, topicID)
	if err != nil {
		return 0, err
	}
	go s.loadAIVisibilityRun(context.Background(), tenantID, runID, engines, q, orgName, doctorNames)
	return runID, nil
}

// PublicCheckupTenantID 公共"免费体检"使用的保留租户号：独立产品，不挂任何真实机构。
const PublicCheckupTenantID int64 = 9999

// StartAIVisibilityPublic 公共"免费体检"入口：独立产品，不挂租户（保留租户号 9999），
// 机构名/医生名由请求方直接给出，不消耗机构额度。复用同一套引擎与缺口逻辑。
func (s *Service) StartAIVisibilityPublic(ctx context.Context, orgName, doctorNames, question, region string) (int64, error) {
	if s.llmClient == nil {
		return 0, fmt.Errorf("llm gateway is not configured")
	}
	// 清理僵尸任务：API 重启会中断后台 goroutine，running 超过 10 分钟视为失败。
	// 只作用于公共体检租户，避免影响机构自己的诊断记录。
	_, _ = s.store.pool.Exec(ctx, `UPDATE content_ai_visibility_runs SET status='failed'
		WHERE tenant_id=$1 AND status='running' AND created_at < now() - interval '10 minutes'`, PublicCheckupTenantID)
	question = strings.TrimSpace(question)
	if question == "" {
		return 0, fmt.Errorf("question is required")
	}
	region = strings.TrimSpace(region)
	orgName = strings.TrimSpace(orgName)
	if orgName == "" {
		orgName = "某某某机构"
	}
	doctorNames = strings.TrimSpace(doctorNames)
	if doctorNames == "" {
		doctorNames = "（未录入）"
	}
	q := question
	if region != "" && !strings.Contains(question, region) {
		q = region + "，" + question
	}
	engines, err := s.listAIEngines(ctx, PublicCheckupTenantID)
	if err != nil {
		return 0, err
	}
	if len(engines) == 0 {
		return 0, fmt.Errorf("未配置 AI 引擎（ai_assistant）")
	}
	runID, err := s.store.createAIVisibilityRun(ctx, PublicCheckupTenantID, q, region, orgName, len(engines), 0, 0)
	if err != nil {
		return 0, err
	}
	go s.loadAIVisibilityRun(context.Background(), PublicCheckupTenantID, runID, engines, q, orgName, doctorNames)
	return runID, nil
}

// loadAIVisibilityRun 后台逐个问引擎 + 分析，边跑边更新进度；全部完成后写缺口。
func (s *Service) loadAIVisibilityRun(ctx context.Context, tenantID, runID int64, engines []llmModelSelection, q, orgName, doctorNames string) {
	// 三家并行跑：总耗时取最慢的那个，而不是三者相加
	results := make([]aiEngineAnswer, len(engines))
	var wg sync.WaitGroup
	var mu sync.Mutex
	done := 0

	for i, eng := range engines {
		wg.Add(1)
		go func(idx int, sel llmModelSelection) {
			defer wg.Done()
			item := aiEngineAnswer{Engine: engineLabel(sel.Provider, sel.ModelCode), Provider: sel.Provider, ModelCode: sel.ModelCode}
			answer, sources, queries, searchCount, err := s.aiEngineAnswer(ctx, tenantID, sel, q)
			if err != nil {
				// 技术细节只进日志，返回给客户的是能看懂的话
				item.Error = "这次没能拿到该 AI 的回答（可能超时），稍后可单独重试"
				slog.Warn("ai visibility: engine answer failed", "engine", item.Engine, "error", err)
			} else {
				item.Answer = answer
				item.Sources = sources
				item.SearchQueries = queries
				item.SearchCount = searchCount
				analysis, aerr := s.aiEngineAnalyze(ctx, tenantID, sel, q, answer, orgName, doctorNames)
				if aerr != nil {
					item.Error = "该 AI 的回答已拿到，但解析失败，稍后可重试"
					slog.Warn("ai visibility: engine analyze failed", "engine", item.Engine, "error", aerr)
				} else {
					item.Analysis = analysis
				}
			}

			mu.Lock()
			results[idx] = item
			done++
			partial := make([]any, 0, len(engines))
			for _, r := range results {
				if r.Engine != "" {
					partial = append(partial, r)
				}
			}
			p := done
			mu.Unlock()

			// 边跑边落：前端轮询能逐个看到引擎结果
			_ = s.store.updateAIVisibilityRunEngines(ctx, tenantID, runID, partial)
			_ = s.store.updateAIVisibilityRunProgress(ctx, tenantID, runID, p)
		}(i, eng)
	}
	wg.Wait()

	resultsAny := make([]aiEngineAnswer, 0, len(engines))
	for _, r := range results {
		if r.Engine != "" {
			resultsAny = append(resultsAny, r)
		}
	}

	gap := map[string]any{}
	if g, err := s.aiVisibilityGap(ctx, tenantID, q, orgName, doctorNames, resultsAny); err == nil {
		gap = g
	} else {
		slog.Warn("ai visibility: gap failed", "error", err)
	}

	engAny := make([]any, 0, len(resultsAny))
	for _, r := range resultsAny {
		engAny = append(engAny, r)
	}
	if err := s.store.finishAIVisibilityRun(ctx, tenantID, runID, engAny, gap); err != nil {
		slog.Error("ai visibility: finish run failed", "error", err)
		_ = s.store.failAIVisibilityRun(ctx, tenantID, runID)
		return
	}
	// 交付判定：至少一家 AI 返回了回答/分析才构成一次有效诊断。
	// 全部引擎失败（超时/配置错误）属于空交付，不扣 prompt。
	if !aiVisibilityDeliveryComplete(resultsAny) {
		slog.Warn("ai visibility run produced no usable engine result; empty delivery is not charged",
			"tenant_id", tenantID, "run_id", runID, "engines", len(engines))
		return
	}
	// 诊断完成并落库后扣 prompt 1 题次；扣量失败只告警，不回滚诊断结果。
	s.chargeAIVisibility(ctx, tenantID, runID)
}

// aiVisibilityDeliveryComplete 判断一次 AI 可见性诊断是否构成有效交付：
// 至少一家引擎返回了非空回答、分析结果或检索证据，才算拿到了可用的对照结果。
func aiVisibilityDeliveryComplete(results []aiEngineAnswer) bool {
	for _, item := range results {
		if strings.TrimSpace(item.Answer) != "" || len(item.Analysis) > 0 || item.SearchCount > 0 {
			return true
		}
	}
	return false
}
