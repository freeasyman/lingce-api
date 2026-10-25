package content

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/freeasyman/lingce-api/pkg/llmgateway"
)

// 创作层：选题（创作简报）→ 内容（正文 + 发布包）。
// 6 种形式各一个 system prompt + 各一个 json_schema，不兼顾。

// contentFormPromptCode 形式 → 提示词模块 code。
var contentFormPromptCode = map[string]string{
	"article":    "content_article_gen",
	"graphic":    "content_graphic_gen",
	"script":     "content_script_gen",
	"storyboard": "content_storyboard_gen",
	"poster":     "content_poster_gen",
	"moments":    "content_moments_gen",
}

// contentFormPrompt 形式 → 内置兜底 system prompt。
var contentFormPrompt = map[string]string{
	"article": `你是资深「医院公众号内容主编」，负责把选题简报写成一篇能发布、能被搜到、能被转发的公众号文章。

【你的背景——三样都要有】
1) 医学底子：你懂常见病的成因、鉴别、治疗路径，看得懂简报里的专业内容和医生原话。不会把「中医辨证」写成玄学，也不会把「手术指征」写错。你写的东西，医生看了不会皱眉。
2) 用户心理：你知道患者点开文章那一刻在想什么——怕、疑、急，想找「和我一样的人」。你知道他在哪一句会划走，在哪一句会停下来。
3) 传播技巧：你懂新媒体的转发逻辑。人转发一篇文章不是为了帮作者，是为了「借这篇文章表达自己」——要么这句话说出了我想说但说不出来的话，要么转给家人朋友正好有用。

【任务】这是医院「内容获客」第三环「创作」。你的交付是一篇真正能发、能获客的公众号文章：
- 建立对「医生 + 机构」的双重信任：让读者觉得「这个医生真懂」，也让读者觉得「这家医院/这个门诊靠谱、流程清楚、值得去」；
- 标题和小标题覆盖微信「搜一搜」的长尾词（简报 expected_help.search 里的患者会搜的词）；
- 文章本身值得转发——正文有共鸣，结尾有一句共鸣金句刺激读者转发整篇；
- 结构：开头钩子 → 正文 4 节左右层层递进 → 结尾（金句 → CTA）；
- 封面必配图，正文 1~3 个关键转折节配图。

【使用机构知识库】
user prompt 会附带「机构知识库」（机构信息 / 特色优势 / 硬规则 / 软偏好）和「品牌素材」：
- 机构信息 + 特色优势：真实、不夸大，用来建立对「医生 + 机构」的信任；
- 硬规则：和合规红线一样是锁死项，必须严格遵守；
- 软偏好：用于文风语气；
- 品牌素材（配色/字体/logo/照片）：只用于生图提示词 image_prompt，不写进正文。

【lead 怎么写】开头钩子，1~2 段，没有小标题，放在第一节之前。三选一：患者原话 / 扎心场景（「你有没有过…」）/ 反常识。按这篇的目标，选最能让人停下来的一种，不要三种混。

【heading 怎么写】每节一个小标题，短而有力、能单独看懂、尽量带上患者会搜的词。不要用「背景/问题/结论」这种结构标签，直接用具体的小标题。

【image_prompt 怎么写】（封面 + 配图，用中文写，是生图提示词不是一句话要点）
- 封面图：按 2.35:1 横向构图，但画面主体和关键文字必须放在【中间 1:1 正方形安全区】内——转发/朋友圈会把封面裁成正方形，上下两侧只放背景/装饰，关键内容不能被切掉。图上文字印标题或核心一句。
- 正文配图：只给 1~3 个「关键转折/最能说明问题」的节配图，其余节的 image_prompt 留空。配图必须和这一节内容相关，不是随便一张装饰图。
- 共同要求：画面主体（画什么）· 图上文字（一字不差）· 风格（扁平插画/手绘/实拍/白底卡片）· 配色（照品牌主色，品牌素材里有就用，没有写具体色值）· 构图。不要画 logo、二维码、水印（后台会叠加真实的，模型别画）。

【share_quote 怎么写】文末一句共鸣金句，刺激读者转发【整篇文章】。
先想清楚这篇要制造哪种「共鸣时刻」，三选一、不混：
A. 被理解（治愈型）——读者觉得「终于有人懂我」。例：「你不是想多了，你的身体真的在提醒你。」
B. 被点醒（认知型）——读者觉得「原来如此，我该重视」。例：「结节不是等它长大，是趁它还小的时候搞明白它到底是什么。」
C. 被代言（想跟谁说却说不出）——读者觉得「这就是我想跟爸妈说的话」。例：「怕冷不是矫情，是身体在提醒你该看看了。」
约束：一句话、不超过 30 字、是读者的共鸣、不是医院广告、绝不出现「转发/分享/点赞」字样。

【摘要 copy 怎么写】公众号后台必填，≤120 字。
最重要：文章转发到聊天里，别人只能看到摘要的【前 15 个字符】。所以前 15 个字必须是整篇最抓人的那句（转发钩子），不能有铺垫。后面再补全：文章讲什么、帮读者解决什么。

【发挥与锁死】
锁死（不能改）：核心主张 core_claim、切入角度 angle、真实素材 facts/quotes/golden_quote、合规红线 forbidden_claims。
发挥（可自由创作）：文风、语气、措辞；正文节数微调（默认 4 节）；开头钩子形式（三选一）；每节怎么展开；金句方向 A/B/C（由选题决定）；配图放哪几节。

【输出】只返回一个 JSON 对象，不要任何解释、不要 markdown 代码块。`,

	"graphic": `你是资深「医疗新媒体内容创作专家」，负责把选题简报写成一篇能直接发布的小红书/公众号图文笔记。

你懂四件事：
1) 图文结构 —— 第一张卡是封面（有钩子），每张卡片讲一个点，卡与卡之间有逻辑递进；
2) 钩子与传播 —— 文案口语化、emoji 分段、有记忆点，用金句/原话做钩子；
3) 医疗合规 —— 不承诺疗效、不制造焦虑、不出现患者正脸、不用绝对化用语；
4) 生图 —— 每张卡要配一张图，你要给出能直接喂给文生图模型的完整提示词。

【任务】这是医院「内容获客」第三环「创作」。你的交付是一篇真正能发、能获客的图文笔记：
- 每张卡讲一个点，点与点之间按"误区→真相→行动"或"为什么→是什么→怎么办"递进；
- 第一张卡（封面）标题命中患者的搜索词或痛点（搜索获客）；
- 用上简报里的真实原话/金句，不脑补事实；
- 结尾有 CTA（引导咨询/到院）。

【使用机构知识库】
user prompt 会附带「机构知识库」（机构信息 / 特色优势 / 硬规则 / 软偏好）和「品牌素材」：
- 机构信息 + 特色优势：真实、不夸大，用来建立对「医生 + 机构」的信任；
- 硬规则：和合规红线一样是锁死项，必须严格遵守；
- 软偏好：用于文风语气；
- 品牌素材（配色/字体/logo/照片）：只用于生图提示词 image_prompt，不写进正文。

【card_title 怎么写】card_title 是这张卡的具体标题（如「为什么症状这么杂」），短而有力、能钩人。不要用「Hook / 核心观点 / CTA」这种结构标签。

【image_prompt 怎么写】（每张卡都要，是生图提示词，不是一句话要点）
用中文写。必须交代清楚，让文生图模型能直接出一张合格的图：
- 画面主体：画什么（医生背影/人体示意图/诊室/白底卡片…）
- 图上文字：要印上去的字（这张卡的标题/核心一句，一字不差写出来）
- 风格：扁平插画/手绘/实拍/白底黑字卡片
- 配色：照品牌主色（简报里有就用，没有就写具体色值）
- 构图：居中大字/左文右图/上下分区
- 不要画 logo、二维码、水印（后台会叠加真实的，模型别画）
比例由 ratio 字段定，不要在 image_prompt 里重复写。

【发挥与锁死】
锁死（不能改）：核心主张 core_claim、切入角度 angle、真实素材 facts/quotes/golden_quote、合规红线 forbidden_claims。
发挥（你可自由创作）：文风、语气、措辞、卡片数量与顺序（默认按简报骨架，可微调）、每张卡怎么展开。

【输出】只返回一个 JSON 对象，不要任何解释、不要 markdown 代码块。`,

	"script": `你是资深医疗短视频口播稿策划，专门给临床医生写 60–180 秒（3 分钟封顶）的出镜口播稿——医生能直接念，剪辑能直接照着做。

【你懂七件事】
1. 钩子十来种，按选题挑最合适的：对号入座 / 反常识 / 直接提问 / 场景代入 / 结论前置 / 身份背书 / 误区纠正 / 症状信号 / 时机 / 人群边界
2. 60–180 秒节奏：钩子（前 5 秒）→ 展开 → 佐证 → 收口；不是把短话拉长
3. 医生短视频账号（医生IP）怎么做——每条内容分三种类型，按选题选最合适的一种，从"医疗项目"出发、用表达框架把卖点讲出来：
   · 价值型：平和、清楚，像门诊里把机制讲透；比喻用来"讲明白"，不煽情、不打鸡血
   · 观点型：鲜明、有反差、敢下明确判断；语气相对硬、干脆、有锋芒
   · 案例型：有温度、有代入感，比喻和画面细节用得多，把患者的处境讲出来
4. 说话方式：通俗易懂、风趣有趣、善用比喻比拟，把医学机制翻译成人话
5. 必要的表情手势只在需要处标，克制；不写口头禅、不写夸张表演
6. 包装素材（给剪辑看）：每个关键观点标出配什么证据——数据/图表/图片/生理解剖图/检查单/术式演示；字幕要完整，要突出的词用 **加粗**
7. 合规红线：不承诺疗效、不绝对化、不制造焦虑、不暴露隐私、不诱导就诊；✗ 不谈价格/性价比/值不值，✗ 不做"做哪个更好"的商业推荐

【任务】根据选题简报，写一条口播稿，输出 JSON。

【表达框架】钩 → 破 → 立 → 据 → 界 → 收
1 钩  开场一句，让目标人群"这说的就是我"
2 破  点破一个普遍误区/疑问
3 立  医生给出明确判断
4 据  用数据/机制/真实案例支撑（这段配包装素材）
5 界  说清什么情况适用、什么情况不适用
6 收  一句能被复述的结论（不写任何行动号召）
六步是话术节奏，不是固定 6 段；短内容可合并步，长内容"据"可拆多段。

【总字数：1~3 分钟 = 260~780 字】
按 260 字/分钟折算（≈4.3 字/秒），这条口播的总时长必须落在 1~3 分钟：
- 1 分钟 = 260 字；1.5 分钟 = 390 字；2 分钟 = 520 字；3 分钟 = 780 字。
- 整条 words 总字数必须 ≥260 字（撑满 1 分钟），≤780 字（不超过 3 分钟）。
- 拿不准写多长，就按 1.5~2 分钟（390~520 字）来写。
- 段数对应字数：3-4 段≈260 字；5-6 段≈520 字；7-8 段≈780 字。
- 每段是一整段完整表达（通常 60~120 字，对应 14~28 秒），
  不是一句话一段，也不许出现只有十来个字的碎段。
- 宁可少一段、把每段说透，也不要为凑段数切成碎句。

【字段写作规范】
- title：能直接发的标题，借用选题的钩子；不空泛
- segments[].words：口语化、句子短、医生能一口气念完；不写书面长句、不堆术语
- segments[].action：只在需要处标（出镜/表情/手势/切空镜），可空；不写口头禅、不写夸张表演
- segments[].subtitle：完整字幕，突出词用 **加粗**；不是只打关键词
- segments[].evidence：这段配的证据素材，没有就写空字符串（别写空格、逗号）；不编造不存在的证据

【反平庸铁律（必须做到）】
1. 三类型开头截然不同：价值型=一个反常识/疑问；观点型=一句话的明确判断/反差（如"越X越不一定Y"）；案例型=真实患者场景。禁止串味。
2. 句子短、口语化，像跟朋友聊天；但【一个段 = 一个完整意思，可以说 2~4 句话】，不许把一句完整的话拆成多个段；总段数 3~8。不写"更让她困扰的是…"这种书面长句。
3. 每出现一个抽象机制（寒热/阴阳/内分泌/激素等），必须配一个生活里的比方，整条至少 2 个比喻。
4. 收尾必须有一句 10~15 字、能被截图转发的话，要有反转或有画面感。
5. 观点型默认档3/4（明确判断/反差），不许用档1陈述型。

【锁死】
- 简报的 core_claim / angle / facts / quotes / golden_quote / forbidden_claims / bias_risk / goal 不能改
- 只讲医学指征：什么手术、什么情况需要/不能做、医生怎么判断
- 无 CTA、不诱导就诊；案例型只能用 facts/quotes 里的真实录音案例，绝不编造

【观点型锐度（可选 1~4，按内容自选，最多到 4、绝不到 5）】
档1 陈述型："从我的经验看，这种情况更适合 X"
档2 破误区型："很多人以为 X，其实临床上不是这样"
档3 明确判断型："先说结论：这种情况我不建议做手术"
档4 反差型："越贵的术式，不一定越适合你"
档5（红线，绝对禁用）：恐吓、绝对化、"别去别家"踩同行

【发挥】文风语气（按三类型）、比喻具体选择、口语化措辞、结构填肉（六步怎么合并）、时长段数微调、表情手势标注、包装素材选择。

【输出】只返回一个 JSON 对象，不要任何解释、不要 markdown 代码块。`,

	"storyboard": `你是资深「医疗短视频分镜师」，负责把选题简报写成分镜脚本（镜头表）。

你懂四件事：
1) 分镜语言 —— 景别（特写/近景/中景/全景）、运镜（固定/推/拉/摇/移）、画面构图；
2) 医生出镜 —— 分镜里医生会出镜讲解，也要有场景/物品/字幕/动画的画面；
3) 画面可执行 —— 画面描述具体到"谁、在哪、做什么、镜头怎么动"，给手绘分镜/实拍直接能用；
4) 医疗合规 —— 不承诺疗效、不制造焦虑、不出现患者正脸。

【任务】这是医院「内容获客」第三环「创作」。你的交付是一个真正能拍的分镜脚本：
- 每个镜头交代：景别、运镜、画面内容、台词/旁白、字幕、时长；
- 开头镜头有钩子，结尾镜头有 CTA；
- 用上简报里的真实素材（医生讲解/金句/数据）。

【发挥与锁死】
锁死（不能改）：核心主张 core_claim、切入角度 angle、真实素材 facts/quotes/golden_quote、合规红线 forbidden_claims。
发挥（你可自由创作）：镜头数量与顺序（默认按简报骨架，可微调）、景别/运镜怎么设计、画面怎么构图。

【输出】只返回一个 JSON 对象，不要任何解释、不要 markdown 代码块。`,

	"poster": `你是资深「医疗科普海报文案」，负责把选题简报写成一张能直接出图的海报文案。

你懂四件事：
1) 海报信息密度 —— 主标题短而有钩子，卖点简洁（每条≤15字），CTA 明确；
2) 视觉层级 —— 主标题最大、卖点次之、CTA 醒目，配色照品牌主色；
3) 一句话看懂 —— 患者扫一眼就知道"讲什么、对我有什么用、下一步干什么"；
4) 医疗合规 —— 不承诺疗效、不制造焦虑、不出现患者正脸。

【任务】这是医院「内容获客」第三环「创作」。你的交付是一张真正能贴出来、能获客的海报：
- 主标题命中痛点/搜索词/金句；
- 卖点 3~5 条，每一条都是"患者关心的点"，不是空话；
- CTA 明确（扫码咨询/到院/留资）。

【发挥与锁死】
锁死（不能改）：核心主张 core_claim、切入角度 angle、真实素材 facts/quotes/golden_quote、合规红线 forbidden_claims。
发挥（你可自由创作）：主标题措辞、卖点条数与顺序、副标题怎么起。

【输出】只返回一个 JSON 对象，不要任何解释、不要 markdown 代码块。`,

	"moments": `你是资深「医疗朋友圈文案」，负责把选题简报写成一条能直接发的朋友圈内容。

你懂四件事：
1) 朋友圈语境 —— 是"人"在说话，不是官方公告；口语化、有温度、像医生/机构自己在分享；
2) 折叠红线 —— 配文控制在 6 行内避免折叠，重点放前面；
3) 图片配合 —— 每张图配一个要点（文字要点供配图用）；
4) 医疗合规 —— 不承诺疗效、不制造焦虑、不出现患者正脸。

【任务】这是医院「内容获客」第三环「创作」。你的交付是一条真正能发、能获客的朋友圈：
- 开头一句抓住人（金句/误区/真实场景）；
- 配文短而有信息量，不是流水账；
- 图片要点 3~4 条，对应几张图；
- 有 CTA（引导私信/咨询）。

【发挥与锁死】
锁死（不能改）：核心主张 core_claim、切入角度 angle、真实素材 facts/quotes/golden_quote、合规红线 forbidden_claims。
发挥（你可自由创作）：配文语气、分行/emoji、图片数量与要点分配。

【输出】只返回一个 JSON 对象，不要任何解释、不要 markdown 代码块。`,
}

// contentBriefGlossary 选题简报字段说明（注入 user prompt，禁止脑补）。
const contentBriefGlossary = `选题简报字段说明（据此创作，禁止脑补）：
title                标题（参考，可重新起更好的）
recommendation       推荐理由（为什么发这篇）
goal                 内容目标（科普/品牌/医生IP/转化/GEO）
expected_help        预期帮助（search 患者搜索词 / cognition 认知目标 / conversion 获客预期）
core_claim           核心主张（这篇的结论，锁死不能改）
angle                切入角度（锁死不能改）
audience             目标人群（具体到"哪类人看到会说这就是我"）
suggested_forms      建议形式
facts[]              事实（只写这些，别添加简报外的事实）
quotes[]             原话（可引用做金句，保留说话人）
golden_quote         金句（可能为 null）
key_points[]         每节的要点（用来搭正文骨架，不要漏掉关键点）
per_form_structure   各形式骨架（含数量，默认按它来，可微调）
forbidden_claims[]   合规红线（绝对不能写）
bias_risk            偏见风险（注意别踩）`

// contentHardRules 素材溯源 + 硬规则（注入 user prompt）。
const contentHardRules = `【素材溯源规则】
facts[] 必须来自简报 facts；quotes[]/golden_quote 必须来自简报 quotes/golden_quote（保留原话和说话人）；key_points[] 必须来自简报 key_points；core_claim / angle 锁死不能改。所有素材不脑补、不编造。

【硬规则】
0. 必须命中简报预期帮助里至少一个效果（搜索/认知/转化）。
1. 所有素材来自简报，不脑补、不编造。
2. 锁死 core_claim / angle / facts/quotes/golden_quote / forbidden_claims。
3. 不承诺疗效、不制造焦虑、不出现患者正脸、不用绝对化用语。
4. 每个字段都必须出现，没有内容写 null 或 []，不许省略字段。
5. 只返回一个 JSON 对象，不要解释、不要 markdown 代码块。`

// fetchTopicBrief 取选题完整简报（16 字段）。
func (s *Store) fetchTopicBrief(ctx context.Context, tenantID, topicID int64) (string, error) {
	var brief string
	err := s.pool.QueryRow(ctx, `
		SELECT COALESCE(brief::text, '{}') FROM content_topics WHERE id = $1 AND tenant_id = $2`,
		topicID, tenantID).Scan(&brief)
	if err != nil {
		return "", err
	}
	return brief, nil
}

// brandContext 组装品牌素材注入文本。
func (s *Service) brandContext(ctx context.Context, tenantID int64) string {
	k, err := s.store.getOrgKnowledge(ctx, tenantID)
	if err != nil || k == nil || len(k.BrandAssets) == 0 || string(k.BrandAssets) == "{}" || string(k.BrandAssets) == "[]" {
		return ""
	}
	var assets []struct {
		Type  string `json:"type"`
		URL   string `json:"url"`
		Value string `json:"value"`
		Label string `json:"label"`
	}
	if json.Unmarshal(k.BrandAssets, &assets) != nil || len(assets) == 0 {
		return ""
	}
	var b strings.Builder
	b.WriteString("品牌素材（做视觉时用）：\n")
	for _, a := range assets {
		label := strings.TrimSpace(a.Label)
		switch a.Type {
		case "color":
			if a.Value != "" {
				b.WriteString(fmt.Sprintf("- 配色：%s（%s）\n", a.Value, label))
			}
		case "font":
			if a.Value != "" {
				b.WriteString(fmt.Sprintf("- 字体：%s（%s）\n", a.Value, label))
			}
		default:
			if a.URL != "" {
				b.WriteString(fmt.Sprintf("- %s：%s（%s）\n", a.Type, a.URL, label))
			}
		}
	}
	return strings.TrimSpace(b.String())
}

// styleContext 组装风格模板注入文本（视觉用的）。
func (s *Service) styleContext(ctx context.Context, tenantID int64) string {
	tpls, err := s.store.listStyleTemplates(ctx, tenantID, "")
	if err != nil || len(tpls) == 0 {
		return ""
	}
	var b strings.Builder
	for _, t := range tpls {
		if len(t.ExtractedStyle) == 0 || string(t.ExtractedStyle) == "{}" {
			continue
		}
		b.WriteString(fmt.Sprintf("- 风格模板「%s」：%s\n", t.Name, string(t.ExtractedStyle)))
	}
	if b.Len() == 0 {
		return ""
	}
	return "风格模板（照着这个视觉做）：\n" + strings.TrimSpace(b.String())
}

// contentUserPromptFallback 创作通用用户提示词模板（代码兜底，含 {{占位符}}）。
// 占位符：{{topic_brief}} 选题简报 / {{org_knowledge}} 机构知识库 / {{brand_assets}} 品牌素材
//        {{style_templates}} 风格模板 / {{count_requirement}} 数量覆盖
const contentUserPromptFallback = "选题简报如下（JSON）：\n\n{{topic_brief}}\n\n" +
	contentBriefGlossary +
	"\n\n{{org_knowledge}}\n\n{{brand_assets}}\n\n{{style_templates}}\n\n{{count_requirement}}\n\n" +
	contentHardRules

// resolvePromptUserTemplate 读 user_prompt_template（表），空则用代码兜底。
func (s *Service) resolvePromptUserTemplate(ctx context.Context, code, fallback string) string {
	if p, err := s.store.getPromptByCode(ctx, code); err == nil && p != nil && strings.TrimSpace(p.UserPrompt) != "" {
		return strings.TrimSpace(p.UserPrompt)
	}
	return fallback
}

// knowledgeBlock 机构知识库注入块（含标题；空则空串）。
func (s *Service) knowledgeBlock(ctx context.Context, tenantID int64) string {
	if kc := s.knowledgeContext(ctx, tenantID); kc != "" {
		return "【机构知识库】\n" + kc
	}
	return ""
}

// brandBlock 品牌素材注入块。
func (s *Service) brandBlock(ctx context.Context, tenantID int64) string {
	if bc := s.brandContext(ctx, tenantID); bc != "" {
		return "【品牌素材】\n" + bc
	}
	return ""
}

// styleBlock 风格模板注入块。
func (s *Service) styleBlock(ctx context.Context, tenantID int64) string {
	if sc := s.styleContext(ctx, tenantID); sc != "" {
		return "【风格模板】\n" + sc
	}
	return ""
}

// countBlock 数量覆盖块（空则空串）。
func countBlock(count int) string {
	if count <= 0 {
		return ""
	}
	return fmt.Sprintf("【数量要求】本次生成 %d 个（张卡/个镜头/节），按这个数量来。", count)
}

// renderContentUserPrompt 读 user 模板并填充占位符。
func (s *Service) renderContentUserPrompt(ctx context.Context, tenantID, topicID int64, form string, count int) (string, error) {
	// 全站标准：一行三样齐全，user 模板就读当前形式那一行
	tpl := s.resolvePromptUserTemplate(ctx, contentFormPromptCode[form], contentUserPromptFallback)
	brief, err := s.store.fetchTopicBrief(ctx, tenantID, topicID)
	if err != nil {
		return "", err
	}
	result := tpl
	result = strings.ReplaceAll(result, "{{topic_brief}}", brief)
	result = strings.ReplaceAll(result, "{{org_knowledge}}", s.knowledgeBlock(ctx, tenantID))
	result = strings.ReplaceAll(result, "{{brand_assets}}", s.brandBlock(ctx, tenantID))
	result = strings.ReplaceAll(result, "{{style_templates}}", s.styleBlock(ctx, tenantID))
	result = strings.ReplaceAll(result, "{{count_requirement}}", countBlock(count))
	for strings.Contains(result, "\n\n\n") {
		result = strings.ReplaceAll(result, "\n\n\n", "\n\n")
	}
	return strings.TrimSpace(result), nil
}

// publishPackageProperties 发布包（所有形式都要，完整 6 项）。
func publishPackageProperties(includeCover bool, noCTA bool) map[string]any {
	props := map[string]any{
		"copy":           map[string]any{"type": "string"},
		"tags":           map[string]any{"type": "array", "items": map[string]any{"type": "string"}},
		"pinned_comment": map[string]any{"type": "string"},
	}
	required := []string{"copy", "tags", "pinned_comment"}
	if !noCTA {
		props["cta"] = map[string]any{"type": "string"}
		props["publish_time"] = map[string]any{"type": "string"}
		required = append(required, "cta", "publish_time")
	}
	if includeCover {
		props["cover"] = map[string]any{"type": "string"}
		required = append([]string{"cover"}, required...)
	}
	return map[string]any{
		"type":                 "object",
		"properties":           props,
		"required":             required,
		"additionalProperties": false,
	}
}

func strArr(items ...string) []string { return items }

func strProp() map[string]any { return map[string]any{"type": "string"} }

// buildContentOutputSchema 构建某形式的 json_schema（代码兜底）。
func buildContentOutputSchema(form string) (*llmgateway.OutputSchema, error) {
	var bodyProps map[string]any
	var bodyRequired []string
	hasCover := true

	switch form {
	case "article":
		bodyProps = map[string]any{
			"title": strProp(),
			"cover": map[string]any{
				"type": "object",
				"properties": map[string]any{
					"image_prompt": strProp(),
				},
				"required":             []string{"image_prompt"},
				"additionalProperties": false,
			},
			"lead": strProp(),
			"sections": map[string]any{
				"type": "array",
				"items": map[string]any{
					"type": "object",
					"properties": map[string]any{
						"heading":      strProp(),
						"paragraphs":   map[string]any{"type": "array", "items": strProp()},
						"image_prompt": strProp(),
					},
					"required":             []string{"heading", "paragraphs", "image_prompt"},
					"additionalProperties": false,
				},
			},
			"share_quote": strProp(),
		}
		bodyRequired = []string{"title", "cover", "lead", "sections", "share_quote"}
		hasCover = false
	case "graphic":
		bodyProps = map[string]any{
			"title": strProp(),
			"cards": map[string]any{
				"type": "array",
				"items": map[string]any{
					"type": "object",
					"properties": map[string]any{
						"card_title":   strProp(),
						"body":         strProp(),
						"ratio":        strProp(),
						"image_prompt": strProp(),
					},
					"required":             []string{"card_title", "body", "ratio", "image_prompt"},
					"additionalProperties": false,
				},
			},
		}
		bodyRequired = []string{"title", "cards"}
		hasCover = false
	case "script":
		bodyProps = map[string]any{
			"type":  map[string]any{"type": "string", "enum": []string{"价值型", "观点型", "案例型"}},
			"title": strProp(),
			"segments": map[string]any{
				"type": "array",
				"items": map[string]any{
					"type": "object",
					"properties": map[string]any{
						"phase":    map[string]any{"type": "string", "enum": []string{"钩", "破", "立", "据", "界", "收"}},
						"words":    strProp(),
						"action":   strProp(),
						"subtitle": strProp(),
						"evidence": strProp(),
					},
					"required":             []string{"phase", "words", "action", "subtitle", "evidence"},
					"additionalProperties": false,
				},
			},
		}
		bodyRequired = []string{"type", "title", "segments"}
		hasCover = false
	case "storyboard":
		bodyProps = map[string]any{
			"title": strProp(),
			"shots": map[string]any{
				"type": "array",
				"items": map[string]any{
					"type": "object",
					"properties": map[string]any{
						"scene":    strProp(),
						"camera":   strProp(),
						"visual":   strProp(),
						"dialogue": strProp(),
						"subtitle": strProp(),
						"duration": strProp(),
					},
					"required":             []string{"scene", "camera", "visual", "dialogue", "subtitle", "duration"},
					"additionalProperties": false,
				},
			},
		}
		bodyRequired = []string{"title", "shots"}
	case "poster":
		bodyProps = map[string]any{
			"title":          strProp(),
			"subtitle":       strProp(),
			"selling_points": map[string]any{"type": "array", "items": strProp()},
			"cta":            strProp(),
		}
		bodyRequired = []string{"title", "subtitle", "selling_points", "cta"}
	case "moments":
		bodyProps = map[string]any{
			"copy":         strProp(),
			"image_points": map[string]any{"type": "array", "items": strProp()},
		}
		bodyRequired = []string{"copy", "image_points"}
	default:
		return nil, fmt.Errorf("unsupported form: %s", form)
	}

	bodyProps["publish_package"] = publishPackageProperties(hasCover, form == "script")
	bodyRequired = append(bodyRequired, "publish_package")

	schema := map[string]any{
		"type":                 "object",
		"properties":           bodyProps,
		"required":             bodyRequired,
		"additionalProperties": false,
	}
	return &llmgateway.OutputSchema{Name: "content_" + form, Strict: true, Schema: schema}, nil
}

// resolveContentOutputSchema 优先用提示词表里的 output_schema，空则用代码兜底。
func (s *Service) resolveContentOutputSchema(ctx context.Context, tenantID int64, form string) (*llmgateway.OutputSchema, error) {
	if raw, err := s.store.getPromptOutputSchema(ctx, contentFormPromptCode[form]); err != nil {
		return nil, err
	} else if len(raw) > 0 && string(raw) != "{}" && string(raw) != "null" {
		var schema map[string]any
		if err := json.Unmarshal(raw, &schema); err != nil {
			return nil, fmt.Errorf("invalid output_schema for %s: %w", form, err)
		}
		return &llmgateway.OutputSchema{Name: "content_" + form, Strict: true, Schema: schema}, nil
	}
	return buildContentOutputSchema(form)
}

// GenerateContentV2 选题 → 内容（按形式）。count<=0 时用简报骨架的默认数量。
func (s *Service) GenerateContentV2(ctx context.Context, tenantID, topicID int64, form string, count int) (map[string]any, error) {
	form = normalizeFormCode(form)
	if _, ok := contentFormPrompt[form]; !ok {
		return nil, fmt.Errorf("unsupported form: %s", form)
	}
	if topicID <= 0 {
		return nil, fmt.Errorf("topic_id is required")
	}

	outSchema, err := s.resolveContentOutputSchema(ctx, tenantID, form)
	if err != nil {
		return nil, err
	}

	userPrompt, err := s.renderContentUserPrompt(ctx, tenantID, topicID, form, count)
	if err != nil {
		return nil, err
	}

	selectedModel, err := s.resolveLLMModelConfig(ctx, tenantID, []string{"content_" + form, "content_generation", "chat"})
	if err != nil {
		return nil, fmt.Errorf("resolve llm config for content: %w", err)
	}

	llmReq := llmgateway.TextInferenceRequest{
		TenantID:      tenantID,
		CallerService: "lingce-api",
		CallerModule:  contentCallerModule("content_generation"),
		FunctionType:  selectedModel.FunctionType,
		Provider:      selectedModel.Provider,
		ModelCode:     selectedModel.ModelCode,
		Billing:       newBillingMetadata("content", "topic", topicID, "content_generation", "content_"+form),
		Messages: []llmgateway.Message{
			{Role: "system", Content: s.resolvePromptByCode(ctx, tenantID, contentFormPromptCode[form], contentFormPrompt[form])},
			{Role: "user", Content: userPrompt},
		},
		Params: &llmgateway.Params{OutputSchema: outSchema, TimeoutSeconds: 180},
	}
	applyModelParamsToLLMRequest(&llmReq, selectedModel.ModelParams)

	llmResp, err := s.llmClient.TextInference(ctx, llmReq)
	if err != nil {
		return nil, fmt.Errorf("generate content: %w", err)
	}

	return parseContentResult(llmResp.Content)
}

// parseContentResult 解析内容 JSON（对象根）。
func parseContentResult(content string) (map[string]any, error) {
	raw := strings.TrimSpace(content)
	if idx := strings.Index(raw, "```"); idx >= 0 {
		raw = raw[idx+3:]
		if strings.HasPrefix(strings.TrimSpace(raw), "json") {
			raw = strings.TrimSpace(raw)[4:]
		}
		if end := strings.LastIndex(raw, "```"); end >= 0 {
			raw = raw[:end]
		}
	}
	// 取第一个 { 到最后一个 } 之间
	start := strings.Index(raw, "{")
	end := strings.LastIndex(raw, "}")
	if start < 0 || end <= start {
		return nil, fmt.Errorf("content output is not a JSON object")
	}
	var out map[string]any
	if err := json.Unmarshal([]byte(raw[start:end+1]), &out); err != nil {
		return nil, fmt.Errorf("parse content output: %w", err)
	}
	return out, nil
}
