package content

import "strings"

const (
	contentGoalClarify = "clarify"
	contentGoalSave    = "save"
	contentGoalComment = "comment"
	contentGoalConvert = "convert"
)

type contentGoalStrategy struct {
	Label        string
	NoteStyle    string
	StrategyText string
}

var contentGoalStrategies = map[string]contentGoalStrategy{
	contentGoalClarify: {
		Label:     "讲清楚",
		NoteStyle: "knowledge",
		StrategyText: strings.TrimSpace(`
## 内容目标
- 本篇目标是把核心问题讲清楚，优先提升理解度与判断力。
- 结构优先采用“先结论，后解释，再补充边界”的表达方式。
- 正文每张卡只讲一个重点，避免信息并列过多。
- 优先使用“概念区分、原因解释、常见误区、正确做法”这类科普结构。
- 语气清晰、直接、克制，不故意制造过强悬念。
- 互动引导可以有，但权重要低于知识传递本身。
- 发布文案重点帮助用户快速抓住结论，并说明为什么要知道这件事。
`),
	},
	contentGoalSave: {
		Label:     "让用户收藏",
		NoteStyle: "knowledge",
		StrategyText: strings.TrimSpace(`
## 内容目标
- 本篇目标是强化收藏价值，让用户觉得这是一篇值得反复查看的实用笔记。
- 内容优先组织成“清单、步骤、避坑、自查、对照表、判断标准”。
- 每张卡都要有明确可提取的信息点，减少空泛表达。
- 尽量让用户产生“这条很有用，先存起来”的感觉。
- 多用“自查一下”“以后遇到可以对照”“这一条很多人会忽略”这类收藏导向语气。
- 发布文案要强化“建议收藏”“以后用得上”“需要时回来翻”的感受。
- 置顶评论可补充一个额外判断点、适用人群或补充条件，增强资料感。
`),
	},
	contentGoalComment: {
		Label:     "激发评论",
		NoteStyle: "qa",
		StrategyText: strings.TrimSpace(`
## 内容目标
- 本篇目标是激发用户评论区互动，而不仅仅是被动阅读。
- 开头就要制造代入感，让用户产生“这说的不就是我吗”的感觉。
- 正文中适当加入提问句、对号入座句、分类型判断句。
- 多设置“你是哪一种”“你中几条”“你有没有这种经历”这类互动钩子。
- 允许轻微争议感、反常识感或共鸣感，但不能夸张失实。
- 互动引导必须明确，结尾要推动用户留言，而不是只说收藏关注。
- 置顶评论继续承接互动，适合追问、补充选择题、邀请用户讲经历。
`),
	},
	contentGoalConvert: {
		Label:     "兼顾转化",
		NoteStyle: "story",
		StrategyText: strings.TrimSpace(`
## 内容目标
- 本篇目标是在保证内容可信的前提下，增强用户的行动意愿。
- 内容要突出“问题识别 -> 风险意识 -> 正确应对 -> 建议进一步评估”的路径。
- 可以适度加强痛点、后果、误区成本，但不能制造恐吓。
- 更适合使用“场景化表达、案例化表达、常见决策错误、专业建议”。
- 要让用户意识到：这件事不能只靠自己猜，最好做进一步判断或寻求专业帮助。
- 结尾不做硬销售，但要自然引向“评估一下”“别自己乱试”“建议专业判断”。
- 置顶评论可补充“哪些情况建议尽快就医或评估”。
`),
	},
}

func normalizeContentGoal(goal *string) string {
	if goal == nil {
		return ""
	}
	return strings.ToLower(strings.TrimSpace(*goal))
}

func getContentGoalStrategy(goal *string) (contentGoalStrategy, bool) {
	normalized := normalizeContentGoal(goal)
	strategy, ok := contentGoalStrategies[normalized]
	return strategy, ok
}
