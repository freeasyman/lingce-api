package splitdemo

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"math"
	"os"
	"sort"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"
)

const (
	defaultPromptVersion         = "v0.3"
	defaultModelName             = "qwen-max"
	defaultMaxChunkChars         = 12000
	twoPassScanOverlapSeconds    = 300
	dashscopeInputLimit          = 30720
	tokenPerRuneRatio            = 1.3
	outputReserve                = 2000
	summaryOutputReserve         = 1000
	encounterSummarySystemPrompt = `你是医疗记录助手。从就诊对话中提取关键信息,只输出 JSON,不要任何解释。

输出格式:
{"patient_hint":"患者特征,如'阿姨,58岁'或'小伙子,20多岁'","chief_complaint":"主诉,10-20字","disposition":"处置,如'开了XX药'或'建议XX检查',10-20字"}

如果某项信息对话中未提及,该字段填空字符串 ""。`
	encounterSummaryUserPrompt = `就诊对话:

{{encounter_text}}

提取患者、主诉、处置,输出 JSON。`
	defaultSystemPrompt = `你是一个医疗录音结构化助手。你的唯一任务是把一段连续的门诊录音转写文本,按时间轴切分成连续的片段,并判断每个片段的类型。

## 你要做的事

把整条时间轴切成连续、不重叠、完整覆盖的片段。每个片段判断两件事:
1. 它的类型(是一次就诊,还是非就诊内容)
2. 它的起止位置

## 你不要做的事

- 不要识别患者姓名、年龄、性别、电话
- 不要生成主诉、诊断、病历、处方
- 不要评价医生表现
- 不要总结对话内容
- 不要提取任何可用于宣传的片段

以上都由后续环节处理。你只回答"边界在哪里、这段是什么类型"。

## 片段类型定义

- encounter:一次就诊。一个患者(含陪同家属)与医生的连续诊疗交互,包含问诊、查体、解释、开方、医嘱中至少一项。
- non_encounter_talk:非就诊对话。医生与同事讨论排班/器械、接电话、闲聊。即使内容涉及某位患者的病情,只要患者不在场,也归此类。
- idle:静默或无有效内容。
- uncertain:无法判断类型,需人工复核。

## 就诊边界的裁定规则

判定为同一次就诊(不切开):
- 同一患者连续讲多个不相关的问题
- 家属代述,患者本人不在场
- 患者中途离开做检查后返回
- 医生短暂离开,患者留在诊室
- 内部短静默(30 秒以内)
- 医生查阅资料、书写记录时的静默
- 夫妻同诊、母子同诊等多位患者同时在场、医生交替问诊的情况 -- 判为一次 encounter,并标记 multi_patient: true
- 同一患者的连续沟通,无论话题怎么转换,都不切开。话题从胃痛转到睡眠、再转到体检报告,只要医生没有重新做基础问诊,就是一次就诊
- 一次就诊包含问诊、查体、解释病情、看报告、讲治疗方案、谈费用等多个阶段。这些阶段之间不是就诊边界
- 医生为了举例说明而提到的其他患者。例如"我有个病人和你情况差不多,四十多岁,也是这个症状,吃了三个月就好了"。这个被提到的患者不在现场,不构成新的就诊

## 重要:不要依赖说话人标记

转写文本里的说话人标记(speaker_0、speaker_1 等)**极不可靠**。很多录音的说话人分离完全失效,整条录音的所有发言都被标成同一个说话人。

因此:
- 不要用"说话人标记有没有变化"来判断是否换了患者
- 所有发言都是 speaker_0 也完全正常,这不代表只有一个人在说话
- 你必须靠**说话内容本身**来判断谁在说话、说的是谁的病情

如何从内容判断说话人角色:
- 提问、下医嘱、解释病理、开药的是医生
- 描述症状、回答提问、询问治疗方案的是患者或家属
- 同一行文本里可能混进了多个人的话(转写没断开),要按语义拆解

## 判断"是不是换了患者"

看这三件事,任意一件成立就说明换人了:

1. **有明确的告别或交接** -- 前一位患者的诊疗已经收尾
2. **新的主诉从零开始被陈述** -- 有人开始讲一个全新的、与前文毫无关联的症状,并且医生在重新做基础问诊("多久了""什么时候开始的""以前看过吗")
3. **患者的身份特征变了** -- 年龄、性别、称谓、孩子的名字发生变化

反过来,以下情况说明还是同一位患者,不要切:
- 话题从一个症状转到另一个症状,但医生没有重新做基础问诊
- 讨论从病情转到费用、流程、注意事项 -- 这仍属于本次就诊
- 医生举例提到其他患者("我有个病人和你情况差不多,四十多岁"),这个人没有实际参与对话

判定为不是就诊:
- 患者只是问路、拿报告、取药,没有诊疗内容
- 医生与同事的任何交流

## 边界信号(按可靠性排序)

最强信号 -- 告别与交接(优先级高于一切,只要出现就应当在此处切开):

通用告别:
- 互道再见:"拜拜"、"再见"、"谢谢医生"、"慢走"、"辛苦了"
- 送客动作:"那我们就先这样"、"好,你先过去吧"、"行,那就这样"

公立医院/医保门诊的结束方式(以开单、指路、交接科室为主):
- 开单指路:"拿着单子去二楼抽血"、"这个方子去药房拿药"、"到一楼收费处交费"
- 交接科室:"我给你开个住院证,去住院部办手续"、"转到骨科去看看"、"挂个专家号再看"
- 医嘱收尾:"回去按时吃药,两周后复查"、"不舒服再来"、"先吃一周看看效果"
- 叫号:"下一个"、"下一位"

私立/消费医疗的结束方式(以交接非医疗人员为主):
- 交接给客服、咨询师、前台:"我让客服跟你详细说费用"、"你带妈妈去前台了解一下"、"到那边找小王办手续"
- 交接后医生可能还会和同事有几句交代,这几句属于本次就诊的尾部,不要单独成段

这类信号即使前后所有发言都被标成同一个说话人,也必须切开。告别语的语义本身就证明了这次就诊结束了。

**容易混淆的一点:去做检查不等于结束。**
- "先去做个 CT,做完拿过来我看"、"去拍个片子,回来找我" -- 患者会返回,**这是同一次就诊**,不要切
- "拿着单子去二楼抽血"(没说要回来)、"结果出来了再挂号" -- 这次就诊已经结束,应当切开

判断依据:医生有没有明确表示患者要回到这里。说了要回来就不切,没说就切。

强信号:
- 叫号或点名:"下一个"、"张三"、"李阿姨来了"
- 明确的结束语:"没什么问题了"、"回去吃药"、"下周再来"、"好,就这样"
- 明确的开场语:"哪里不舒服"、"什么时候开始的"、"坐吧"
- 长静默(超过 90 秒)
- 时间戳跳跃:相邻两句发言之间的时间差超过 30 秒。你必须主动检查每对相邻发言的时间戳,计算间隔。间隔超过 30 秒是强边界信号,超过 90 秒几乎必然是边界。注意:转写文本里的静默不会有任何文字提示,只表现为时间戳的数字跳跃(如 [05:10] 后面紧接 [06:12]),你必须自己做减法计算。

中等信号:
- 主诉突变且与前文无关联
- 称谓变化:"阿姨"变成"小伙子"
- 中等静默(30 到 90 秒)
- 寒暄模式重现

弱信号:
- 固定问诊句式重复出现
- 语气回到问诊开场状态

## 最重要的原则:宁可多切,不要漏切

把两个不同患者错误地合并成一段,是严重错误。它会导致 A 患者的主诉和 B 患者的诊断混进同一份病历,产生临床安全风险,而且很难被发现。

把一次就诊错误地切成两段,是轻微错误。人工合并即可修复。

因此:
- 遇到疑似不同患者时,优先切开
- 拿不准的边界,切开并标记低置信度
- 不要为了让片段"看起来完整"而合并可疑区域

但"优先切开"有前提:必须存在上一节所列的换人迹象之一。以下情况不属于"拿不准",不要切:
- 话题变了,但医生没有重新做基础问诊
- 讨论转向费用、流程、后续安排
- 只是被提到了另一个患者,那个人没有实际参与对话
- 出现了一段静默,但静默前后讨论的是同一个人的同一件事

一次就诊可能长达二三十分钟,包含问诊、查体、解释、看报告、讲方案、谈费用等多个阶段。**阶段切换不是就诊边界。** 把一次完整就诊按阶段切成四五段,会让医生反复做无意义的合并,最终不再信任切分结果。

## 输出格式

只输出 JSON,不要任何解释文字、不要 markdown 代码块标记。

{
  "segments": [
    {
      "index": 1,
      "segment_type": "encounter",
      "start_seconds": 0,
      "end_seconds": 412,
      "boundary_confidence": 0.93,
      "boundary_reasons": ["closing_phrase:没问题了回去吧", "silence_38s"],
      "silence_gap_seconds": 38,
      "needs_review": false,
      "multi_patient": false,
      "contains_clinical_info": null,
      "text_preview": "医生:坐吧,哪里不舒服? 患者:最近胃一直疼..."
    }
  ]
}

字段说明:
- index: 从 1 开始的连续序号
- segment_type: encounter | non_encounter_talk | idle | uncertain
- start_seconds / end_seconds: 相对录音开始的偏移秒数
- boundary_confidence: 0.0 到 1.0,你对这个片段边界判定的可信程度
- boundary_reasons: 你依据了哪些信号,写成 "信号类型:具体证据" 的形式
- silence_gap_seconds: 该片段与下一片段之间的静默秒数,不确定时填 null
- needs_review: boundary_confidence 低于 0.75 时必须为 true
- multi_patient: 仅 encounter 类型有意义,疑似多位患者同时就诊时为 true,否则 false
- contains_clinical_info: 仅 non_encounter_talk 类型有意义,该段是否涉及患者病情。其他类型填 null
- text_preview: 该片段开头约 100 字,用于人工识别这段在讲什么

## 硬性约束

1. 第一个片段的 start_seconds 必须是 0
2. 最后一个片段的 end_seconds 必须等于录音总时长
3. 相邻片段必须首尾相接:segments[i].end_seconds == segments[i+1].start_seconds
4. 不允许有间隙,不允许重叠
5. 每个片段的 end_seconds 必须大于 start_seconds`
	defaultUserPrompt = `录音总时长:{{duration_seconds}} 秒

转写文本:

{{transcript}}

请按 system prompt 的要求切分这条录音的时间轴,输出 JSON。`
	twoPassScanSystemPrompt = `你是医疗录音边界检测助手。你的唯一任务是从转写文本中找出所有可能是"就诊结束"的位置，不做判断，只做发现。

**重要：必须从头到尾完整扫描整段文本，不要遗漏开头部分的信号。越早出现的信号越重要，务必报告。**

## 要找的信号

强信号（必须报告）：
- 告别语：拜拜、再见、谢谢医生、慢走、辛苦了
- 公立医院开单指路：去二楼抽血、去药房拿药、拿着单子、到收费处、做完CT回来找我（这个是中途不算）、不舒服再来
- 公立医院交接科室：转到骨科、挂专家号、开住院证
- 私立/消费医疗交接：让客服跟你详细说、带你去前台、找小王办手续
- 叫号/点名：下一个、下一位

中等信号（也要报告）：
- 相邻两句时间戳间隔超过 30 秒（你必须计算每对相邻行的时间差）

## 扫描策略

1. 先完整浏览一遍文本，标记所有疑似位置
2. 按时间顺序输出，从最早的开始
3. 宁可多报不要漏报，第二步会筛选

## 输出格式

只输出 JSON 数组，不要任何解释文字：

[
  {"at_seconds": 615, "signal_type": "farewell", "signal_text": "拜拜"},
  {"at_seconds": 1968, "signal_type": "farewell", "signal_text": "谢谢医生"},
  {"at_seconds": 2100, "signal_type": "silence_gap", "signal_text": "间隔 62 秒"}
]

signal_type 的值：farewell / handoff / call_next / silence_gap
at_seconds 精确到秒。按时间从早到晚排序。`
	twoPassScanUserPrompt = `录音总时长：{{duration_seconds}} 秒

以下是转写文本（每行格式：[开始时间] 内容）：

{{transcript}}

请**从头到尾**扫描整段文本，找出所有可能是就诊结束的位置。特别注意：文本开头的信号同样重要，不要遗漏。按时间顺序输出 JSON 数组。`
	twoPassJudgeSystemPrompt = `你是医疗录音边界判定助手。你会看到一段 6 分钟左右的录音片段，其中某个时间点被标记为疑似就诊边界。你的任务是判断这里是不是真正的就诊结束。

## 是就诊结束（返回 true）

- 边界后出现了新患者描述自己的症状，医生重新做基础问诊（"多久了"、"什么时候开始的"、"以前看过吗"）
- 边界处有明确的告别语，后面是叫号或新患者

## 不是就诊结束（返回 false）

- 同一患者话题转换（从症状聊到费用、复查安排、用药方法）
- 患者中途离开做检查，但医生明确说了要回来
- 静默后同一对话继续
- 医生举例提到另一个患者，但那个人没有出现在对话里

## 不要依赖说话人标记

转写里说话人标记（speaker_0 等）可能全是同一个，不可信。靠内容判断：提问、下医嘱、解释病理的是医生；描述症状、回答提问的是患者。

## 输出格式

只输出 JSON，不要任何解释：

{"is_boundary": true, "confidence": 0.91, "reason": "32:47 处互道拜拜，32:54 医生开始询问新患者"}`
	twoPassJudgeUserPrompt = `疑似边界在 {{at_seconds}} 秒（{{at_time}}），触发信号：{{signal_text}}

以下是边界前后各约 3 分钟的对话：

{{window_transcript}}

这里是不是真正的就诊结束？`
)

type Service struct {
	store *Store
	llm   *Client
}

func NewService(store *Store, llm *Client) *Service {
	return &Service{store: store, llm: llm}
}

func (s *Service) ListRecordings(ctx context.Context, tenantID int64, minDurationSeconds, page, pageSize int, recordingID int64, query string) ([]RecordingListItem, int64, error) {
	return s.store.ListRecordings(ctx, tenantID, minDurationSeconds, page, pageSize, recordingID, query)
}

func (s *Service) GetRecording(ctx context.Context, recordingID int64) (*RecordingDetail, error) {
	return s.store.GetRecording(ctx, recordingID)
}

func (s *Service) SaveAnnotation(ctx context.Context, record *AnnotationRecord) error {
	if record == nil {
		return fmt.Errorf("empty annotation record")
	}
	if record.RecordingID <= 0 {
		return fmt.Errorf("recording_id is required")
	}
	existing, err := s.store.LoadAnnotation(ctx, record.RecordingID)
	if err != nil {
		return err
	}
	if existing != nil {
		history := make([]*AnnotationRecord, 0, len(existing.History)+1)
		history = append(history, cloneAnnotationRecord(existing))
		history = append(history, existing.History...)
		record.History = history
	} else {
		record.History = nil
	}
	return s.store.SaveAnnotation(ctx, record)
}

func (s *Service) LoadAnnotation(ctx context.Context, recordingID int64) (*AnnotationRecord, error) {
	return s.store.LoadAnnotation(ctx, recordingID)
}

func (s *Service) UndoAnnotation(ctx context.Context, recordingID int64) (*AnnotationRecord, error) {
	if recordingID <= 0 {
		return nil, fmt.Errorf("recording_id is required")
	}
	current, err := s.store.LoadAnnotation(ctx, recordingID)
	if err != nil {
		return nil, err
	}
	if current == nil || len(current.History) == 0 {
		return nil, fmt.Errorf("no annotation history")
	}
	prev := cloneAnnotationRecord(current.History[0])
	if prev == nil {
		return nil, fmt.Errorf("annotation history is empty")
	}
	prev.History = append([]*AnnotationRecord(nil), current.History[1:]...)
	if err := s.store.SaveAnnotation(ctx, prev); err != nil {
		return nil, err
	}
	return prev, nil
}

func (s *Service) GetAnnotationOverview(ctx context.Context, recordingID int64) (*AnnotationOverviewResponse, error) {
	overall, err := s.buildAnnotationOverview(ctx)
	if err != nil {
		return nil, err
	}
	resp := &AnnotationOverviewResponse{
		ReasonOptions: correctionReasonOptions(),
		Overall:       *overall,
	}
	if record, err := s.store.LoadAnnotation(ctx, recordingID); err == nil && record != nil {
		resp.Recording = buildAnnotationSummary(record)
	}
	return resp, nil
}

func (s *Service) buildAnnotationOverview(ctx context.Context) (*AnnotationOverview, error) {
	items, err := s.store.LoadAllAnnotations(ctx)
	if err != nil {
		return nil, err
	}
	stats := map[string]int{}
	total := 0
	ids := make([]int64, 0, len(items))
	for _, record := range items {
		if record == nil {
			continue
		}
		ids = append(ids, record.RecordingID)
		for _, correction := range record.Corrections {
			code := strings.TrimSpace(correction.ReasonCode)
			if code == "" {
				code = string(CorrectionReasonOther)
			}
			stats[code]++
			total++
		}
	}
	return &AnnotationOverview{
		TotalRecordings:       len(items),
		TotalCorrections:      total,
		ByReason:              buildReasonStats(stats),
		AnnotatedRecordingIDs: ids,
	}, nil
}

func (s *Service) SummarizeEncounterText(ctx context.Context, model, encounterText string) (*EncounterSummary, error) {
	if strings.TrimSpace(model) == "" {
		model = defaultModelName
	}
	return s.summarizeEncounterTextWithFallback(ctx, model, encounterText, false)
}

func (s *Service) SplitRecording(ctx context.Context, req SplitRequest) (*SplitResponse, error) {
	return s.splitRecordingWithInput(ctx, req, nil, nil)
}

func (s *Service) SplitRecordingWithProgress(ctx context.Context, req SplitRequest, report func(SplitProgress)) (*SplitResponse, error) {
	return s.splitRecordingWithInput(ctx, req, nil, report)
}

func (s *Service) SplitSyntheticCase(ctx context.Context, caseID string, req SplitRequest, report func(SplitProgress)) (*SplitResponse, *SyntheticCase, error) {
	syntheticCase, err := s.store.GetSyntheticCase(ctx, caseID)
	if err != nil {
		return nil, nil, err
	}
	if syntheticCase == nil {
		return nil, nil, fmt.Errorf("synthetic case not found")
	}
	recording, err := s.buildSyntheticRecordingDetail(ctx, syntheticCase)
	if err != nil {
		return nil, syntheticCase, err
	}
	resp, err := s.splitRecordingWithInput(ctx, req, recording, report)
	if err != nil {
		return nil, syntheticCase, err
	}
	return resp, syntheticCase, nil
}

func (s *Service) splitRecordingWithInput(ctx context.Context, req SplitRequest, recording *RecordingDetail, report func(SplitProgress)) (*SplitResponse, error) {
	if req.RecordingID <= 0 && recording == nil {
		return nil, fmt.Errorf("recording_id is required")
	}
	if strings.TrimSpace(req.Model) == "" {
		req.Model = defaultModelName
	}
	if strings.TrimSpace(req.PromptVersion) == "" {
		req.PromptVersion = defaultPromptVersion
	}
	if strings.TrimSpace(req.SystemPrompt) == "" {
		req.SystemPrompt = defaultSystemPrompt
	}
	if strings.TrimSpace(req.UserPrompt) == "" {
		req.UserPrompt = defaultUserPrompt
	}
	chunkChars := defaultMaxChunkChars
	if req.MaxChunkChars != nil && *req.MaxChunkChars > 0 {
		chunkChars = *req.MaxChunkChars
	}
	temp := 0.1
	if req.Temperature != nil {
		temp = *req.Temperature
	}

	if recording == nil {
		var err error
		recording, err = s.store.GetRecording(ctx, req.RecordingID)
		if err != nil {
			return nil, err
		}
	}
	if shouldUseTwoPass(req, recording) {
		return s.splitRecordingTwoPass(ctx, req, recording, report, temp)
	}
	totalSeconds := recordingDurationSeconds(recording)
	inputChunks := buildInputChunks(recording, chunkChars)
	if len(inputChunks) == 0 {
		inputChunks = []inputChunk{{StartSeconds: 0, EndSeconds: totalSeconds, Content: rawTranscriptFallback(recording)}}
	}
	if report != nil {
		report(SplitProgress{
			Stage:       "preparing",
			Message:     fmt.Sprintf("录音已加载，准备切分，共 %d 块", len(inputChunks)),
			TotalChunks: len(inputChunks),
		})
	}

	started := time.Now()
	segments := make([]SplitSegment, 0, 16)
	usage := Usage{}
	rawOutputs := make([]string, 0, len(inputChunks))
	for idx, chunk := range inputChunks {
		if report != nil {
			report(SplitProgress{
				Stage:           "running",
				Message:         fmt.Sprintf("正在处理第 %d/%d 块", idx+1, len(inputChunks)),
				TotalChunks:     len(inputChunks),
				CompletedChunks: idx,
				CurrentChunk:    idx + 1,
				PartialSegments: len(segments),
			})
		}
		content := buildChunkContent(recording, chunk, idx+1, len(inputChunks))
		finalUserPrompt := composeUserPrompt(req.UserPrompt, content)
		contentText, chunkUsage, err := s.llm.ChatCompletion(ctx, llmChatRequest{
			Model: req.Model,
			Messages: []llmMessage{
				{Role: "system", Content: req.SystemPrompt},
				{Role: "user", Content: finalUserPrompt},
			},
			Temperature: &temp,
		})
		if err != nil {
			return nil, err
		}
		usage.PromptTokens += chunkUsage.PromptTokens
		usage.CompletionTokens += chunkUsage.CompletionTokens
		usage.TotalTokens += chunkUsage.TotalTokens
		rawOutputs = append(rawOutputs, contentText)
		parsed, err := parseSegmentsOutput(contentText)
		if err != nil {
			repaired, repairErr := s.repairSegmentsOutput(ctx, req.Model, contentText)
			if repairErr != nil {
				return nil, fmt.Errorf("parse chunk %d output: %w", idx+1, err)
			}
			repaired = sanitizeSegmentsPayload(repaired)
			parsed, err = parseSegmentsOutput(repaired)
			if err != nil {
				if extracted, ok := extractFirstJSONObject(repaired); ok {
					parsed, err = parseSegmentsOutput(extracted)
				}
			}
			if err != nil {
				return nil, fmt.Errorf("parse chunk %d output after repair: %w", idx+1, err)
			}
			contentText = repaired
		}
		parsed = constrainSegmentsToChunk(parsed, chunk)
		segments = append(segments, parsed...)
		if report != nil {
			report(SplitProgress{
				Stage:           "running",
				Message:         fmt.Sprintf("第 %d/%d 块完成，当前已产出 %d 段", idx+1, len(inputChunks), len(segments)),
				TotalChunks:     len(inputChunks),
				CompletedChunks: idx + 1,
				CurrentChunk:    idx + 1,
				PartialSegments: len(segments),
			})
		}
	}

	normalized := normalizeSegments(segments, totalSeconds)
	utterances := buildUtterances(recording)
	enrichSegments(normalized, utterances)
	boundaries := buildBoundaryReviews(normalized, utterances)
	summary := buildSplitSummary(normalized)
	encounters := buildEncounterViews(normalized, utterances)
	if len(encounters) > 0 {
		if report != nil {
			report(SplitProgress{
				Stage:           "summary",
				Message:         fmt.Sprintf("正在生成就诊摘要... 第 1/%d 个", len(encounters)),
				TotalChunks:     len(inputChunks),
				CompletedChunks: len(inputChunks),
				CurrentChunk:    len(inputChunks),
				SummaryTotal:    len(encounters),
				SummaryDone:     0,
				CurrentSummary:  1,
				PartialSegments: len(normalized),
			})
		}
		if err := s.fillEncounterSummaries(ctx, req.Model, encounters, report, len(inputChunks), len(normalized)); err != nil {
			return nil, err
		}
	}
	validation := validateSegments(normalized, totalSeconds)
	elapsed := time.Since(started)
	resp := &SplitResponse{
		RecordingID:        req.RecordingID,
		RecordingDuration:  totalSeconds,
		SplitModel:         req.Model,
		SplitPromptVersion: req.PromptVersion,
		InputKind:          recording.TranscriptSource,
		ChunkCount:         len(inputChunks),
		ElapsedMS:          elapsed.Milliseconds(),
		Usage:              usage,
		Segments:           normalized,
		Boundaries:         boundaries,
		Summary:            summary,
		Encounters:         encounters,
		Validation:         validation,
		RawOutput:          strings.Join(rawOutputs, "\n\n--- chunk ---\n\n"),
	}
	if report != nil {
		report(SplitProgress{
			Stage:           "completed",
			Message:         fmt.Sprintf("切分完成，共 %d 段", len(normalized)),
			TotalChunks:     len(inputChunks),
			CompletedChunks: len(inputChunks),
			CurrentChunk:    len(inputChunks),
			PartialSegments: len(normalized),
		})
	}
	return resp, nil
}

func shouldUseTwoPass(req SplitRequest, recording *RecordingDetail) bool {
	if req.UseTwoPass != nil {
		return *req.UseTwoPass
	}
	return recording != nil && len(recording.TranscriptionSegs) > 0
}

func (s *Service) splitRecordingTwoPass(ctx context.Context, req SplitRequest, recording *RecordingDetail, report func(SplitProgress), temp float64) (*SplitResponse, error) {
	totalSeconds := recordingDurationSeconds(recording)
	utterances := buildUtterances(recording)
	if len(utterances) == 0 {
		return s.splitRecordingSinglePass(ctx, req, recording, report, temp)
	}
	if report != nil {
		report(SplitProgress{
			Stage:       "preparing",
			Message:     fmt.Sprintf("录音已加载，准备两遍切分，共 %d 条发言", len(utterances)),
			TotalChunks: len(utterances),
		})
	}
	started := time.Now()
	scanPrompt := firstNonEmpty(req.ScanSystemPrompt, twoPassScanSystemPrompt)
	scanUserPrompt := firstNonEmpty(req.ScanUserPrompt, twoPassScanUserPrompt)
	judgePrompt := firstNonEmpty(req.JudgeSystemPrompt, twoPassJudgeSystemPrompt)
	judgeUserPrompt := firstNonEmpty(req.JudgeUserPrompt, twoPassJudgeUserPrompt)
	candidates, usage, err := s.scanCandidateBoundaries(ctx, req.Model, utterances, totalSeconds, scanPrompt, scanUserPrompt, report, req)
	if err != nil {
		return nil, err
	}
	judgments, judgedUsage, err := s.judgeCandidateBoundaries(ctx, req.Model, utterances, candidates, judgePrompt, judgeUserPrompt, report, req)
	if err != nil {
		return nil, err
	}
	usage.PromptTokens += judgedUsage.PromptTokens
	usage.CompletionTokens += judgedUsage.CompletionTokens
	usage.TotalTokens += judgedUsage.TotalTokens
	if report != nil {
		report(SplitProgress{
			Stage:            "merging",
			Message:          fmt.Sprintf("合并确认边界与过短片段，候选 %d 个，已判定 %d 个", len(candidates), len(judgments)),
			TotalCandidates:  len(candidates),
			JudgedCandidates: len(judgments),
			TotalChunks:      len(candidates),
			CompletedChunks:  len(judgments),
		})
	}
	segments := buildSegmentsFromJudgments(judgments, utterances, totalSeconds)
	normalized := normalizeSegments(segments, totalSeconds)
	enrichSegments(normalized, utterances)
	boundaries := buildBoundaryReviews(normalized, utterances)
	summary := buildSplitSummary(normalized)
	encounters := buildEncounterViews(normalized, utterances)
	if len(encounters) > 0 {
		if report != nil {
			report(SplitProgress{
				Stage:           "summary",
				Message:         fmt.Sprintf("正在生成就诊摘要... 第 1/%d 个", len(encounters)),
				TotalChunks:     len(candidates),
				CompletedChunks: len(candidates),
				CurrentChunk:    len(candidates),
				SummaryTotal:    len(encounters),
				SummaryDone:     0,
				CurrentSummary:  1,
				PartialSegments: len(normalized),
			})
		}
		if err := s.fillEncounterSummaries(ctx, req.Model, encounters, report, len(candidates), len(normalized)); err != nil {
			return nil, err
		}
	}
	validation := validateSegments(normalized, totalSeconds)
	elapsed := time.Since(started)

	// 保存详细的调试日志
	debugLog := fmt.Sprintf(`# 两遍切分调试日志 - v%s
录音 ID: %d
总时长: %d 秒 (%.1f 分钟)
模型: %s

## 第一步：扫描候选边界
找到 %d 个候选边界

候选列表：
%s

## 第二步：逐一判定
判定了 %d 个候选

判定结果：
%s

## 最终输出
共 %d 段

段列表：
%s
`,
		req.PromptVersion,
		req.RecordingID,
		totalSeconds,
		float64(totalSeconds)/60.0,
		req.Model,
		len(candidates),
		mustJSONStringIndent(candidates),
		len(judgments),
		mustJSONStringIndent(judgments),
		len(normalized),
		mustJSONStringIndent(normalized),
	)

	// 保存到文件
	debugPath := fmt.Sprintf("data/debug/recording_%d_v%s_%d.log", req.RecordingID, req.PromptVersion, time.Now().Unix())
	os.MkdirAll("data/debug", 0755)
	if err := os.WriteFile(debugPath, []byte(debugLog), 0644); err == nil {
		log.Printf("调试日志已保存: %s", debugPath)
	}

	resp := &SplitResponse{
		RecordingID:        req.RecordingID,
		RecordingDuration:  totalSeconds,
		SplitModel:         req.Model,
		SplitPromptVersion: req.PromptVersion,
		InputKind:          recording.TranscriptSource,
		ChunkCount:         len(candidates),
		ElapsedMS:          elapsed.Milliseconds(),
		Usage:              usage,
		Segments:           normalized,
		Boundaries:         boundaries,
		Summary:            summary,
		Encounters:         encounters,
		Validation:         validation,
		RawOutput:          strings.Join([]string{mustJSONString(candidates), mustJSONString(judgments)}, "\n\n--- two pass ---\n\n"),
	}
	if report != nil {
		report(SplitProgress{
			Stage:           "completed",
			Message:         fmt.Sprintf("切分完成，共 %d 段", len(normalized)),
			TotalChunks:     len(candidates),
			CompletedChunks: len(candidates),
			CurrentChunk:    len(candidates),
			PartialSegments: len(normalized),
		})
	}
	return resp, nil
}

func (s *Service) splitRecordingSinglePass(ctx context.Context, req SplitRequest, recording *RecordingDetail, report func(SplitProgress), temp float64) (*SplitResponse, error) {
	totalSeconds := recordingDurationSeconds(recording)
	inputChunks := buildInputChunks(recording, defaultMaxChunkChars)
	if len(inputChunks) == 0 {
		inputChunks = []inputChunk{{StartSeconds: 0, EndSeconds: totalSeconds, Content: rawTranscriptFallback(recording)}}
	}
	if report != nil {
		report(SplitProgress{
			Stage:       "preparing",
			Message:     fmt.Sprintf("录音已加载，准备切分，共 %d 块", len(inputChunks)),
			TotalChunks: len(inputChunks),
		})
	}
	started := time.Now()
	segments := make([]SplitSegment, 0, 16)
	usage := Usage{}
	rawOutputs := make([]string, 0, len(inputChunks))
	for idx, chunk := range inputChunks {
		if report != nil {
			report(SplitProgress{
				Stage:           "running",
				Message:         fmt.Sprintf("正在处理第 %d/%d 块", idx+1, len(inputChunks)),
				TotalChunks:     len(inputChunks),
				CompletedChunks: idx,
				CurrentChunk:    idx + 1,
				PartialSegments: len(segments),
			})
		}
		content := buildChunkContent(recording, chunk, idx+1, len(inputChunks))
		finalUserPrompt := composeUserPrompt(req.UserPrompt, content)
		contentText, chunkUsage, err := s.llm.ChatCompletion(ctx, llmChatRequest{
			Model: req.Model,
			Messages: []llmMessage{
				{Role: "system", Content: req.SystemPrompt},
				{Role: "user", Content: finalUserPrompt},
			},
			Temperature: &temp,
		})
		if err != nil {
			return nil, err
		}
		usage.PromptTokens += chunkUsage.PromptTokens
		usage.CompletionTokens += chunkUsage.CompletionTokens
		usage.TotalTokens += chunkUsage.TotalTokens
		rawOutputs = append(rawOutputs, contentText)
		parsed, err := parseSegmentsOutput(contentText)
		if err != nil {
			repaired, repairErr := s.repairSegmentsOutput(ctx, req.Model, contentText)
			if repairErr != nil {
				return nil, fmt.Errorf("parse chunk %d output: %w", idx+1, err)
			}
			repaired = sanitizeSegmentsPayload(repaired)
			parsed, err = parseSegmentsOutput(repaired)
			if err != nil {
				if extracted, ok := extractFirstJSONObject(repaired); ok {
					parsed, err = parseSegmentsOutput(extracted)
				}
			}
			if err != nil {
				return nil, fmt.Errorf("parse chunk %d output after repair: %w", idx+1, err)
			}
			contentText = repaired
		}
		parsed = constrainSegmentsToChunk(parsed, chunk)
		segments = append(segments, parsed...)
		if report != nil {
			report(SplitProgress{
				Stage:           "running",
				Message:         fmt.Sprintf("第 %d/%d 块完成，当前已产出 %d 段", idx+1, len(inputChunks), len(segments)),
				TotalChunks:     len(inputChunks),
				CompletedChunks: idx + 1,
				CurrentChunk:    idx + 1,
				PartialSegments: len(segments),
			})
		}
	}
	normalized := normalizeSegments(segments, totalSeconds)
	utterances := buildUtterances(recording)
	enrichSegments(normalized, utterances)
	boundaries := buildBoundaryReviews(normalized, utterances)
	summary := buildSplitSummary(normalized)
	encounters := buildEncounterViews(normalized, utterances)
	if len(encounters) > 0 {
		if report != nil {
			report(SplitProgress{
				Stage:           "summary",
				Message:         fmt.Sprintf("正在生成就诊摘要... 第 1/%d 个", len(encounters)),
				TotalChunks:     len(inputChunks),
				CompletedChunks: len(inputChunks),
				CurrentChunk:    len(inputChunks),
				SummaryTotal:    len(encounters),
				SummaryDone:     0,
				CurrentSummary:  1,
				PartialSegments: len(normalized),
			})
		}
		if err := s.fillEncounterSummaries(ctx, req.Model, encounters, report, len(inputChunks), len(normalized)); err != nil {
			return nil, err
		}
	}
	validation := validateSegments(normalized, totalSeconds)
	elapsed := time.Since(started)
	resp := &SplitResponse{
		RecordingID:        req.RecordingID,
		RecordingDuration:  totalSeconds,
		SplitModel:         req.Model,
		SplitPromptVersion: req.PromptVersion,
		InputKind:          recording.TranscriptSource,
		ChunkCount:         len(inputChunks),
		ElapsedMS:          elapsed.Milliseconds(),
		Usage:              usage,
		Segments:           normalized,
		Boundaries:         boundaries,
		Summary:            summary,
		Encounters:         encounters,
		Validation:         validation,
		RawOutput:          strings.Join(rawOutputs, "\n\n--- chunk ---\n\n"),
	}
	if report != nil {
		report(SplitProgress{
			Stage:           "completed",
			Message:         fmt.Sprintf("切分完成，共 %d 段", len(normalized)),
			TotalChunks:     len(inputChunks),
			CompletedChunks: len(inputChunks),
			CurrentChunk:    len(inputChunks),
			PartialSegments: len(normalized),
		})
	}
	return resp, nil
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			return strings.TrimSpace(value)
		}
	}
	return ""
}

func mustJSONString(v interface{}) string {
	b, err := json.Marshal(v)
	if err != nil {
		return "[]"
	}
	return string(b)
}

func mustJSONStringIndent(v interface{}) string {
	b, err := json.MarshalIndent(v, "", "  ")
	if err != nil {
		return "[]"
	}
	return string(b)
}

func (s *Service) scanCandidateBoundaries(ctx context.Context, model string, utterances []Utterance, totalSeconds int, systemPrompt, userPrompt string, report func(SplitProgress), req SplitRequest) ([]CandidateBoundary, Usage, error) {
	if len(utterances) == 0 {
		return nil, Usage{}, nil
	}
	var usage Usage
	text := buildScanTranscript(utterances)
	availableChars := scanTranscriptBudget(systemPrompt, userPrompt)
	if availableChars <= 0 {
		availableChars = 12000
	}
	if utf8.RuneCountInString(text) > availableChars {
		var all []CandidateBoundary
		start := 0
		for start < totalSeconds {
			window, end := buildScanWindowByChars(utterances, start, availableChars)
			if len(window) == 0 {
				break
			}
			cands, u, err := s.scanCandidateBoundariesOnce(ctx, model, window, totalSeconds, systemPrompt, userPrompt, report)
			if err != nil {
				return nil, usage, err
			}
			usage.PromptTokens += u.PromptTokens
			usage.CompletionTokens += u.CompletionTokens
			usage.TotalTokens += u.TotalTokens
			all = append(all, cands...)
			if end >= totalSeconds {
				break
			}
			start = maxInt(0, end-twoPassScanOverlapSeconds)
		}
		return dedupeCandidates(all), usage, nil
	}
	return s.scanCandidateBoundariesOnce(ctx, model, utterances, totalSeconds, systemPrompt, userPrompt, report)
}

func estimateTokens(s string) int {
	return int(float64(utf8.RuneCountInString(s)) * tokenPerRuneRatio)
}

func scanTranscriptBudget(systemPrompt, userPrompt string) int {
	templateUser := strings.ReplaceAll(userPrompt, "{{duration_seconds}}", "99999")
	templateUser = strings.ReplaceAll(templateUser, "{{transcript}}", "")
	overhead := estimateTokens(systemPrompt) + estimateTokens(templateUser)
	availableTokens := dashscopeInputLimit - overhead - outputReserve
	budget := int(float64(availableTokens) / tokenPerRuneRatio)
	if budget < 4000 {
		return 4000
	}
	// 限制单次窗口大小为 12000 字符（约 40 分钟），避免 LLM 注意力衰减
	if budget > 12000 {
		budget = 12000
	}
	return budget
}

func buildScanWindowByChars(utterances []Utterance, startSeconds, maxChars int) ([]Utterance, int) {
	if len(utterances) == 0 {
		return nil, startSeconds
	}
	if maxChars <= 0 {
		maxChars = scanTranscriptBudget("", "")
	}
	startIdx := -1
	for idx, utt := range utterances {
		if utt.EndSeconds > startSeconds || utt.StartSeconds >= startSeconds {
			startIdx = idx
			break
		}
	}
	if startIdx < 0 {
		return nil, startSeconds
	}
	window := make([]Utterance, 0, 128)
	totalChars := 0
	endSeconds := utterances[startIdx].EndSeconds
	for idx := startIdx; idx < len(utterances); idx++ {
		line := fmt.Sprintf("[%s] %s", formatSeconds(utterances[idx].StartSeconds), strings.TrimSpace(utterances[idx].Text))
		lineChars := utf8.RuneCountInString(line) + 1
		if len(window) > 0 && totalChars+lineChars > maxChars {
			break
		}
		window = append(window, utterances[idx])
		totalChars += lineChars
		if utterances[idx].EndSeconds > endSeconds {
			endSeconds = utterances[idx].EndSeconds
		}
	}
	return window, endSeconds
}

func (s *Service) scanCandidateBoundariesOnce(ctx context.Context, model string, utterances []Utterance, totalSeconds int, systemPrompt, userPrompt string, report func(SplitProgress)) ([]CandidateBoundary, Usage, error) {
	return s.scanCandidateBoundariesOnceWithRetry(ctx, model, utterances, totalSeconds, systemPrompt, userPrompt, report, false)
}

func (s *Service) scanCandidateBoundariesOnceWithRetry(ctx context.Context, model string, utterances []Utterance, totalSeconds int, systemPrompt, userPrompt string, report func(SplitProgress), retried bool) ([]CandidateBoundary, Usage, error) {
	temp := 0.0
	transcript := buildScanTranscript(utterances)
	finalUserPrompt := strings.ReplaceAll(userPrompt, "{{duration_seconds}}", fmt.Sprintf("%d", totalSeconds))
	finalUserPrompt = strings.ReplaceAll(finalUserPrompt, "{{transcript}}", transcript)
	userOverheadRunes := utf8.RuneCountInString(finalUserPrompt) - utf8.RuneCountInString(transcript)
	log.Printf("[splitdemo] scan input: system=%d rune, user=%d rune, transcript=%d rune, total=%d rune, est_tokens=%d",
		utf8.RuneCountInString(systemPrompt),
		userOverheadRunes,
		utf8.RuneCountInString(transcript),
		utf8.RuneCountInString(systemPrompt)+utf8.RuneCountInString(finalUserPrompt),
		estimateTokens(systemPrompt+finalUserPrompt))
	content, usage, err := s.llm.ChatCompletion(ctx, llmChatRequest{
		Model: model,
		Messages: []llmMessage{
			{Role: "system", Content: systemPrompt},
			{Role: "user", Content: finalUserPrompt},
		},
		Temperature: &temp,
	})
	if err != nil {
		if !retried && strings.Contains(err.Error(), "Range of input length") && len(utterances) > 1 {
			mid := len(utterances) / 2
			left, leftUsage, leftErr := s.scanCandidateBoundariesOnceWithRetry(ctx, model, utterances[:mid], totalSeconds, systemPrompt, userPrompt, report, true)
			if leftErr != nil {
				return nil, Usage{}, leftErr
			}
			right, rightUsage, rightErr := s.scanCandidateBoundariesOnceWithRetry(ctx, model, utterances[mid:], totalSeconds, systemPrompt, userPrompt, report, true)
			if rightErr != nil {
				return nil, Usage{}, rightErr
			}
			return dedupeCandidates(append(left, right...)), Usage{
				PromptTokens:     leftUsage.PromptTokens + rightUsage.PromptTokens,
				CompletionTokens: leftUsage.CompletionTokens + rightUsage.CompletionTokens,
				TotalTokens:      leftUsage.TotalTokens + rightUsage.TotalTokens,
			}, nil
		}
		return nil, Usage{}, err
	}
	candidates, err := parseCandidateBoundariesOutput(content)
	if err != nil {
		return nil, usage, fmt.Errorf("parse candidate boundaries: %w", err)
	}
	if report != nil {
		report(SplitProgress{
			Stage:           "scanning",
			Message:         fmt.Sprintf("第一步：扫描边界信号... 候选 %d 个", len(candidates)),
			TotalCandidates: len(candidates),
		})
	}
	return candidates, usage, nil
}

func (s *Service) judgeCandidateBoundaries(ctx context.Context, model string, utterances []Utterance, candidates []CandidateBoundary, systemPrompt, userPrompt string, report func(SplitProgress), req SplitRequest) ([]BoundaryJudgment, Usage, error) {
	if len(candidates) == 0 {
		return nil, Usage{}, nil
	}
	out := make([]BoundaryJudgment, 0, len(candidates))
	usage := Usage{}
	for idx, candidate := range candidates {
		window := buildCandidateWindow(utterances, candidate.AtSeconds, 180)
		finalUserPrompt := strings.ReplaceAll(userPrompt, "{{at_seconds}}", fmt.Sprintf("%d", candidate.AtSeconds))
		finalUserPrompt = strings.ReplaceAll(finalUserPrompt, "{{at_time}}", formatSeconds(candidate.AtSeconds))
		finalUserPrompt = strings.ReplaceAll(finalUserPrompt, "{{signal_text}}", candidate.SignalText)
		finalUserPrompt = strings.ReplaceAll(finalUserPrompt, "{{window_transcript}}", window)
		userOverheadRunes := utf8.RuneCountInString(finalUserPrompt) - utf8.RuneCountInString(window)
		log.Printf("[splitdemo] judge input: system=%d rune, user=%d rune, transcript=%d rune, total=%d rune, est_tokens=%d",
			utf8.RuneCountInString(systemPrompt),
			userOverheadRunes,
			utf8.RuneCountInString(window),
			utf8.RuneCountInString(systemPrompt)+utf8.RuneCountInString(finalUserPrompt),
			estimateTokens(systemPrompt+finalUserPrompt))
		temp := 0.0
		content, u, err := s.llm.ChatCompletion(ctx, llmChatRequest{
			Model: model,
			Messages: []llmMessage{
				{Role: "system", Content: systemPrompt},
				{Role: "user", Content: finalUserPrompt},
			},
			Temperature: &temp,
		})
		if err != nil {
			return nil, usage, err
		}
		usage.PromptTokens += u.PromptTokens
		usage.CompletionTokens += u.CompletionTokens
		usage.TotalTokens += u.TotalTokens
		judgment, err := parseBoundaryJudgmentOutput(content)
		if err != nil {
			repaired, repairErr := s.repairBoundaryJudgmentOutput(ctx, model, content)
			if repairErr != nil {
				return nil, usage, fmt.Errorf("parse candidate %d judgment: %w", idx+1, err)
			}
			repaired = sanitizeSegmentsPayload(repaired)
			judgment, err = parseBoundaryJudgmentOutput(repaired)
			if err != nil {
				if extracted, ok := extractFirstJSONObject(repaired); ok {
					judgment, err = parseBoundaryJudgmentOutput(extracted)
				}
			}
			if err != nil {
				return nil, usage, fmt.Errorf("parse candidate %d judgment after repair: %w", idx+1, err)
			}
		}
		judgment.AtSeconds = candidate.AtSeconds
		judgment.SignalType = candidate.SignalType
		judgment.SignalText = candidate.SignalText
		out = append(out, judgment)
		if report != nil {
			report(SplitProgress{
				Stage:            "judging",
				Message:          fmt.Sprintf("第二步：判定候选 %d/%d", idx+1, len(candidates)),
				TotalCandidates:  len(candidates),
				JudgedCandidates: idx + 1,
			})
		}
	}
	return out, usage, nil
}

func buildSegmentsFromJudgments(judgments []BoundaryJudgment, utterances []Utterance, totalSeconds int) []SplitSegment {
	cuts := make([]BoundaryJudgment, 0, len(judgments))
	for _, item := range judgments {
		if item.IsBoundary {
			cuts = append(cuts, item)
		}
	}
	sort.SliceStable(cuts, func(i, j int) bool { return cuts[i].AtSeconds < cuts[j].AtSeconds })
	filtered := make([]BoundaryJudgment, 0, len(cuts))
	for _, item := range cuts {
		if len(filtered) == 0 {
			filtered = append(filtered, item)
			continue
		}
		prev := filtered[len(filtered)-1]
		if item.AtSeconds-prev.AtSeconds < 60 {
			if item.Confidence > prev.Confidence {
				filtered[len(filtered)-1] = item
			}
			continue
		}
		filtered = append(filtered, item)
	}
	bounds := []int{0}
	for _, item := range filtered {
		if item.AtSeconds > 0 && item.AtSeconds < totalSeconds {
			bounds = append(bounds, item.AtSeconds)
		}
	}
	bounds = append(bounds, totalSeconds)
	bounds = dedupeInts(bounds)
	segments := make([]SplitSegment, 0, len(bounds)-1)
	for i := 0; i < len(bounds)-1; i++ {
		start := bounds[i]
		end := bounds[i+1]
		if end <= start {
			continue
		}
		selected := sliceUtterancesByRange(utterances, start, end)
		seg := SplitSegment{
			SegmentType:        "encounter",
			StartSeconds:       start,
			EndSeconds:         end,
			BoundaryConfidence: 0.85,
			NeedsReview:        false,
			Utterances:         selected,
		}
		if len(selected) == 0 {
			seg.SegmentType = "idle"
			seg.NeedsReview = true
		}
		segments = append(segments, seg)
	}
	segments = mergeShortSegments(segments, utterances, 60)
	for i := range segments {
		if segments[i].BoundaryConfidence <= 0 {
			segments[i].BoundaryConfidence = 0.85
		}
		segments[i].ReasonLabels = translateReasonLabels(segments[i].BoundaryReasons)
		segments[i].ConfidenceLabel = confidenceLabel(segments[i].BoundaryConfidence)
	}
	return segments
}

func mergeShortSegments(segments []SplitSegment, utterances []Utterance, minSeconds int) []SplitSegment {
	if len(segments) == 0 {
		return segments
	}
	out := make([]SplitSegment, 0, len(segments))
	for _, seg := range segments {
		if seg.DurationSeconds >= minSeconds || len(out) == 0 {
			out = append(out, seg)
			continue
		}
		prev := &out[len(out)-1]
		prev.EndSeconds = seg.EndSeconds
		prev.DurationSeconds = prev.EndSeconds - prev.StartSeconds
		prev.Utterances = sliceUtterancesByRange(utterances, prev.StartSeconds, prev.EndSeconds)
		prev.FullText = joinUtterances(prev.Utterances)
		prev.NeedsReview = true
	}
	for i := range out {
		out[i].Index = i + 1
		out[i].DurationSeconds = out[i].EndSeconds - out[i].StartSeconds
	}
	return out
}

func parseCandidateBoundariesOutput(content string) ([]CandidateBoundary, error) {
	cleaned := stripCodeFence(strings.TrimSpace(content))
	if extracted, ok := extractFirstJSONArray(cleaned); ok {
		cleaned = extracted
	}
	var out []CandidateBoundary
	if err := json.Unmarshal([]byte(cleaned), &out); err == nil {
		return out, nil
	}
	var wrapper struct {
		Candidates []CandidateBoundary `json:"candidates"`
	}
	if err := json.Unmarshal([]byte(cleaned), &wrapper); err != nil {
		return nil, err
	}
	return wrapper.Candidates, nil
}

func parseBoundaryJudgmentOutput(content string) (BoundaryJudgment, error) {
	cleaned := stripCodeFence(strings.TrimSpace(content))
	if extracted, ok := extractFirstJSONObject(cleaned); ok {
		cleaned = extracted
	}
	var out BoundaryJudgment
	if err := json.Unmarshal([]byte(cleaned), &out); err != nil {
		return BoundaryJudgment{}, err
	}
	return out, nil
}

func (s *Service) repairBoundaryJudgmentOutput(ctx context.Context, model, raw string) (string, error) {
	temp := 0.0
	content, _, err := s.llm.ChatCompletion(ctx, llmChatRequest{
		Model: model,
		Messages: []llmMessage{
			{Role: "system", Content: "你是 JSON 修复助手。把用户提供的内容修复成合法完整的 JSON，只输出 JSON，不要解释。必须保留原始含义，不要新增字段，不要改字段名。"},
			{Role: "user", Content: "下面内容可能截断、混入解释文字或格式错误，请修复为一个完整合法的 JSON 对象，且必须包含 is_boundary、confidence、reason 这三个字段。\n\n" + raw},
		},
		Temperature: &temp,
	})
	return content, err
}

func extractFirstJSONArray(content string) (string, bool) {
	start := strings.Index(content, "[")
	if start < 0 {
		return "", false
	}
	depth := 0
	inString := false
	escaped := false
	for idx := start; idx < len(content); idx++ {
		ch := content[idx]
		if inString {
			if escaped {
				escaped = false
				continue
			}
			if ch == '\\' {
				escaped = true
				continue
			}
			if ch == '"' {
				inString = false
			}
			continue
		}
		switch ch {
		case '"':
			inString = true
		case '[':
			depth++
		case ']':
			depth--
			if depth == 0 {
				return content[start : idx+1], true
			}
		}
	}
	return "", false
}

func buildScanTranscript(utterances []Utterance) string {
	parts := make([]string, 0, len(utterances))
	for _, item := range utterances {
		parts = append(parts, fmt.Sprintf("[%s] %s", formatSeconds(item.StartSeconds), strings.TrimSpace(item.Text)))
	}
	return strings.Join(parts, "\n")
}

func buildCandidateWindow(utterances []Utterance, atSeconds, radius int) string {
	start := maxInt(0, atSeconds-radius)
	end := atSeconds + radius
	selected := sliceUtterancesByRange(utterances, start, end)
	return joinUtterances(selected)
}

func dedupeCandidates(items []CandidateBoundary) []CandidateBoundary {
	if len(items) == 0 {
		return nil
	}
	sort.SliceStable(items, func(i, j int) bool { return items[i].AtSeconds < items[j].AtSeconds })
	out := make([]CandidateBoundary, 0, len(items))
	for _, item := range items {
		if len(out) == 0 {
			out = append(out, item)
			continue
		}
		prev := out[len(out)-1]
		if absInt(item.AtSeconds-prev.AtSeconds) <= 5 {
			if len(item.SignalText) > len(prev.SignalText) {
				out[len(out)-1] = item
			}
			continue
		}
		out = append(out, item)
	}
	return out
}

func dedupeInts(values []int) []int {
	if len(values) == 0 {
		return nil
	}
	sort.Ints(values)
	out := values[:0]
	last := values[0] - 1
	for _, v := range values {
		if v == last {
			continue
		}
		out = append(out, v)
		last = v
	}
	return out
}


func constrainSegmentsToChunk(segments []SplitSegment, chunk inputChunk) []SplitSegment {
	if len(segments) == 0 {
		return nil
	}
	chunkDuration := maxInt(chunk.EndSeconds-chunk.StartSeconds, 0)
	useLocalOffset := false
	if chunk.StartSeconds > 0 && chunkDuration > 0 {
		maxEnd := 0
		for _, seg := range segments {
			if seg.EndSeconds > maxEnd {
				maxEnd = seg.EndSeconds
			}
		}
		if maxEnd <= chunkDuration+30 {
			useLocalOffset = true
		}
	}
	out := make([]SplitSegment, 0, len(segments))
	for _, seg := range segments {
		if useLocalOffset {
			seg.StartSeconds += chunk.StartSeconds
			seg.EndSeconds += chunk.StartSeconds
		}
		start := maxInt(seg.StartSeconds, chunk.StartSeconds)
		end := minInt(seg.EndSeconds, chunk.EndSeconds)
		if end <= start {
			continue
		}
		seg.StartSeconds = start
		seg.EndSeconds = end
		out = append(out, seg)
	}
	return out
}

func minInt(a, b int) int {
	if a < b {
		return a
	}
	return b
}

func maxInt(a, b int) int {
	if a > b {
		return a
	}
	return b
}

type inputChunk struct {
	StartSeconds int
	EndSeconds   int
	Content      string
}

func buildInputChunks(recording *RecordingDetail, maxChars int) []inputChunk {
	if maxChars <= 0 {
		maxChars = defaultMaxChunkChars
	}
	if len(recording.TranscriptionSegs) > 0 {
		chunks := make([]inputChunk, 0, 8)
		var current []map[string]interface{}
		currentChars := 0
		startSeconds := -1
		endSeconds := 0
		for _, seg := range recording.TranscriptionSegs {
			line := renderTranscriptLine(seg)
			lineChars := len(line)
			segStart := pickSecondsValue(seg, "start_seconds", "start_time", "start", "offset")
			segEnd := pickSecondsValue(seg, "end_seconds", "end_time", "end")
			if segEnd < segStart {
				segEnd = segStart
			}
			if startSeconds < 0 {
				startSeconds = segStart
			}
			if currentChars+lineChars > maxChars && len(current) > 0 {
				chunks = append(chunks, inputChunk{
					StartSeconds: startSeconds,
					EndSeconds:   endSeconds,
					Content:      renderChunkLines(current),
				})
				current = current[:0]
				currentChars = 0
				startSeconds = segStart
			}
			current = append(current, seg)
			currentChars += lineChars
			endSeconds = segEnd
		}
		if len(current) > 0 {
			chunks = append(chunks, inputChunk{
				StartSeconds: startSeconds,
				EndSeconds:   endSeconds,
				Content:      renderChunkLines(current),
			})
		}
		return chunks
	}
	if len(recording.StructuredTranscript) > 0 {
		return []inputChunk{{StartSeconds: 0, EndSeconds: recordingDurationSeconds(recording), Content: renderChunkLines(recording.StructuredTranscript)}}
	}
	if len(recording.CleanedTranscription) > 0 {
		return []inputChunk{{StartSeconds: 0, EndSeconds: recordingDurationSeconds(recording), Content: renderChunkLines(recording.CleanedTranscription)}}
	}
	if len(recording.TimelineTranscript) > 0 {
		return []inputChunk{{StartSeconds: 0, EndSeconds: recordingDurationSeconds(recording), Content: renderChunkLines(recording.TimelineTranscript)}}
	}
	if recording.TranscriptText != nil && strings.TrimSpace(*recording.TranscriptText) != "" {
		return []inputChunk{{StartSeconds: 0, EndSeconds: recordingDurationSeconds(recording), Content: strings.TrimSpace(*recording.TranscriptText)}}
	}
	return nil
}

func renderChunkLines(segments []map[string]interface{}) string {
	lines := make([]string, 0, len(segments))
	for _, seg := range segments {
		lines = append(lines, renderTranscriptLine(seg))
	}
	return strings.Join(lines, "\n")
}

func renderTranscriptLine(seg map[string]interface{}) string {
	speaker := firstText(seg, "speaker_role", "speaker", "role")
	if speaker == "" {
		speaker = "unknown"
	}
	text := firstText(seg, "text", "content", "transcript", "utterance")
	start := pickSecondsValue(seg, "start_seconds", "start_time", "start", "offset")
	end := pickSecondsValue(seg, "end_seconds", "end_time", "end")
	if start >= 0 && end >= start {
		return fmt.Sprintf("[%s-%s] %s: %s", formatSeconds(start), formatSeconds(end), speaker, strings.TrimSpace(text))
	}
	return fmt.Sprintf("%s: %s", speaker, strings.TrimSpace(text))
}

func buildChunkContent(recording *RecordingDetail, chunk inputChunk, idx, total int) string {
	header := []string{
		fmt.Sprintf("录音ID: %d", recording.ID),
		fmt.Sprintf("录音总时长: %d 秒", recordingDurationSeconds(recording)),
		fmt.Sprintf("当前片段: 第 %d/%d 块", idx, total),
		fmt.Sprintf("片段覆盖: %s - %s", formatSeconds(chunk.StartSeconds), formatSeconds(chunk.EndSeconds)),
		fmt.Sprintf("输入来源: %s", recording.TranscriptSource),
		"",
		"转写内容:",
		chunk.Content,
	}
	return strings.Join(header, "\n")
}

func rawTranscriptFallback(recording *RecordingDetail) string {
	if recording.TranscriptText != nil {
		return strings.TrimSpace(*recording.TranscriptText)
	}
	return ""
}

func composeUserPrompt(userPrompt, transcript string) string {
	userPrompt = strings.TrimSpace(userPrompt)
	if userPrompt == "" {
		userPrompt = defaultUserPrompt
	}
	if strings.Contains(userPrompt, "{{transcript}}") {
		userPrompt = strings.ReplaceAll(userPrompt, "{{transcript}}", transcript)
		return strings.ReplaceAll(userPrompt, "{{duration_seconds}}", extractDurationFromTranscriptHeader(transcript))
	}
	return strings.TrimSpace(userPrompt + "\n\n" + transcript)
}

func extractDurationFromTranscriptHeader(transcript string) string {
	for _, line := range strings.Split(transcript, "\n") {
		if strings.HasPrefix(line, "录音总时长:") {
			parts := strings.Fields(strings.TrimPrefix(line, "录音总时长:"))
			if len(parts) > 0 {
				return parts[0]
			}
		}
	}
	return "0"
}

func parseSegmentsOutput(content string) ([]SplitSegment, error) {
	cleaned := stripCodeFence(strings.TrimSpace(content))
	raw, err := decodeSegmentsPayload(cleaned)
	if err != nil {
		sanitized := sanitizeSegmentsPayload(cleaned)
		raw, err = decodeSegmentsPayload(sanitized)
		if err != nil {
			return nil, err
		}
	}
	out := make([]SplitSegment, 0, len(raw.Segments))
	for _, seg := range raw.Segments {
		item := SplitSegment{
			SegmentType:          firstText(seg, "segment_type", "type"),
			StartSeconds:         pickIntValue(seg, "start_seconds", "start"),
			EndSeconds:           pickIntValue(seg, "end_seconds", "end"),
			BoundaryConfidence:   pickFloatValue(seg, "boundary_confidence", "confidence"),
			BoundaryReasons:      pickStringSlice(seg, "boundary_reasons", "reasons"),
			NeedsReview:          pickBoolValue(seg, "needs_review", "review"),
			MultiPatient:         pickBoolValue(seg, "multi_patient"),
			ContainsClinicalInfo: pickOptionalBool(seg, "contains_clinical_info"),
			TextPreview:          firstText(seg, "text_preview", "preview"),
			FullText:             firstText(seg, "full_text", "text", "content"),
		}
		if item.SegmentType == "" {
			item.SegmentType = "uncertain"
		}
		out = append(out, item)
	}
	return out, nil
}

func decodeSegmentsPayload(content string) (*struct {
	Segments []map[string]interface{} `json:"segments"`
}, error) {
	if extracted, ok := extractFirstJSONObject(content); ok {
		content = extracted
	}
	var raw struct {
		Segments []map[string]interface{} `json:"segments"`
	}
	if err := json.Unmarshal([]byte(content), &raw); err != nil {
		return nil, err
	}
	return &raw, nil
}

func sanitizeSegmentsPayload(content string) string {
	lines := strings.Split(content, "\n")
	for i, line := range lines {
		trimmed := strings.TrimSpace(line)
		trimmed = strings.TrimPrefix(trimmed, "* ")
		trimmed = strings.TrimPrefix(trimmed, "- ")
		trimmed = strings.TrimPrefix(trimmed, "• ")
		trimmed = strings.ReplaceAll(trimmed, ",*", ",")
		trimmed = strings.ReplaceAll(trimmed, "[*", "[")
		trimmed = strings.ReplaceAll(trimmed, "{*", "{")
		lines[i] = trimmed
	}
	sanitized := strings.Join(lines, "\n")
	sanitized = strings.ReplaceAll(sanitized, ",]", "]")
	sanitized = strings.ReplaceAll(sanitized, ",}", "}")
	return sanitized
}

func (s *Service) repairSegmentsOutput(ctx context.Context, model, raw string) (string, error) {
	temp := 0.0
	content, _, err := s.llm.ChatCompletion(ctx, llmChatRequest{
		Model: model,
		Messages: []llmMessage{
			{Role: "system", Content: "你是 JSON 修复助手。把用户提供的内容修复成合法完整的 JSON，只输出 JSON，不要解释。必须保留原始含义，不要新增字段，不要改字段名。"},
			{Role: "user", Content: "下面内容可能截断或格式错误，请修复为一个完整合法的 JSON 对象，且必须包含 segments 数组。\n\n" + raw},
		},
		Temperature: &temp,
	})
	return content, err
}

func extractFirstJSONObject(content string) (string, bool) {
	start := strings.Index(content, "{")
	if start < 0 {
		return "", false
	}
	depth := 0
	inString := false
	escaped := false
	for idx := start; idx < len(content); idx++ {
		ch := content[idx]
		if inString {
			if escaped {
				escaped = false
				continue
			}
			if ch == '\\' {
				escaped = true
				continue
			}
			if ch == '"' {
				inString = false
			}
			continue
		}
		switch ch {
		case '"':
			inString = true
		case '{':
			depth++
		case '}':
			depth--
			if depth == 0 {
				return content[start : idx+1], true
			}
		}
	}
	return "", false
}

func normalizeSegments(input []SplitSegment, totalSeconds int) []SplitSegment {
	if totalSeconds < 0 {
		totalSeconds = 0
	}
	if len(input) == 0 {
		return []SplitSegment{{
			Index:              1,
			SegmentType:        "uncertain",
			StartSeconds:       0,
			EndSeconds:         totalSeconds,
			BoundaryConfidence: 0,
			NeedsReview:        true,
			TextPreview:        "",
		}}
	}
	sort.SliceStable(input, func(i, j int) bool {
		if input[i].StartSeconds == input[j].StartSeconds {
			return input[i].EndSeconds < input[j].EndSeconds
		}
		return input[i].StartSeconds < input[j].StartSeconds
	})
	out := make([]SplitSegment, 0, len(input)+4)
	cursor := 0
	for _, seg := range input {
		start := clamp(seg.StartSeconds, 0, totalSeconds)
		end := clamp(seg.EndSeconds, 0, totalSeconds)
		if end < start {
			end = start
		}
		if start > cursor {
			out = append(out, SplitSegment{
				SegmentType:        "idle",
				StartSeconds:       cursor,
				EndSeconds:         start,
				BoundaryConfidence: 1,
				NeedsReview:        false,
				TextPreview:        "",
			})
		}
		if start < cursor {
			start = cursor
		}
		if end <= start {
			continue
		}
		seg.StartSeconds = start
		seg.EndSeconds = end
		if seg.BoundaryConfidence <= 0 {
			seg.BoundaryConfidence = 0.5
		}
		if seg.SegmentType == "" {
			seg.SegmentType = "uncertain"
			seg.NeedsReview = true
		}
		out = append(out, seg)
		cursor = end
	}
	if cursor < totalSeconds {
		out = append(out, SplitSegment{
			SegmentType:        "idle",
			StartSeconds:       cursor,
			EndSeconds:         totalSeconds,
			BoundaryConfidence: 1,
			NeedsReview:        false,
		})
	}
	for i := range out {
		out[i].Index = i + 1
		out[i].DurationSeconds = out[i].EndSeconds - out[i].StartSeconds
		if out[i].BoundaryConfidence < 0 {
			out[i].BoundaryConfidence = 0
		}
		if out[i].BoundaryConfidence > 1 {
			out[i].BoundaryConfidence = 1
		}
		out[i].ConfidenceLabel = confidenceLabel(out[i].BoundaryConfidence)
		out[i].ReasonLabels = translateReasonLabels(out[i].BoundaryReasons)
	}
	return out
}

func buildUtterances(recording *RecordingDetail) []Utterance {
	source := recording.TranscriptionSegs
	if len(source) == 0 {
		source = recording.CleanedTranscription
	}
	if len(source) == 0 {
		source = recording.TimelineTranscript
	}
	if len(source) == 0 {
		source = recording.StructuredTranscript
	}
	out := make([]Utterance, 0, len(source))
	for _, seg := range source {
		text := strings.TrimSpace(firstText(seg, "text", "content", "transcript", "utterance"))
		if text == "" {
			continue
		}
		start := pickSecondsValue(seg, "start_seconds", "start_time", "start", "offset")
		end := pickSecondsValue(seg, "end_seconds", "end_time", "end")
		if end < start {
			end = start
		}
		speaker := strings.TrimSpace(firstText(seg, "speaker", "speaker_id", "role"))
		if speaker == "" {
			speaker = "unknown"
		}
		role := strings.TrimSpace(firstText(seg, "speaker_role"))
		out = append(out, Utterance{
			StartSeconds: start,
			EndSeconds:   end,
			Speaker:      speaker,
			SpeakerRole:  role,
			Text:         text,
		})
	}
	sort.SliceStable(out, func(i, j int) bool {
		if out[i].StartSeconds == out[j].StartSeconds {
			return out[i].EndSeconds < out[j].EndSeconds
		}
		return out[i].StartSeconds < out[j].StartSeconds
	})
	return out
}

func enrichSegments(segments []SplitSegment, utterances []Utterance) {
	for i := range segments {
		selected := sliceUtterancesByRange(utterances, segments[i].StartSeconds, segments[i].EndSeconds)
		segments[i].Utterances = selected
		segments[i].FullText = joinUtterances(selected)
		if segments[i].TextPreview == "" {
			segments[i].TextPreview = shortenText(segments[i].FullText, 100)
		}
	}
}

func buildBoundaryReviews(segments []SplitSegment, utterances []Utterance) []BoundaryReview {
	if len(segments) < 2 {
		return nil
	}
	out := make([]BoundaryReview, 0, len(segments)-1)
	for i := 0; i < len(segments)-1; i++ {
		left := segments[i]
		right := segments[i+1]
		boundaryAt := left.EndSeconds
		review := BoundaryReview{
			Index:             i + 1,
			BoundarySeconds:   boundaryAt,
			BoundaryTimeLabel: formatSeconds(boundaryAt),
			SilenceGapSeconds: left.SilenceGapSeconds,
			LeftSegmentIndex:  left.Index,
			RightSegmentIndex: right.Index,
			LeftSegmentType:   left.SegmentType,
			RightSegmentType:  right.SegmentType,
			LeftUtterances:    aroundBoundaryLeft(utterances, boundaryAt, 5),
			RightUtterances:   aroundBoundaryRight(utterances, boundaryAt, 5),
			ReasonLabels:      left.ReasonLabels,
			ConfidenceLabel:   left.ConfidenceLabel,
			NeedsReview:       left.NeedsReview || right.NeedsReview,
		}
		if left.SilenceGapSeconds != nil {
			review.SilenceGapLabel = formatDurationChinese(*left.SilenceGapSeconds)
		}
		out = append(out, review)
	}
	return out
}

func buildSplitSummary(segments []SplitSegment) SplitSummary {
	summary := SplitSummary{TotalSegmentCount: len(segments)}
	for _, seg := range segments {
		switch seg.SegmentType {
		case "encounter":
			summary.EncounterCount++
		case "non_encounter_talk":
			summary.NonEncounterTalkCount++
		case "idle":
			summary.IdleCount++
		case "uncertain":
			summary.UncertainCount++
		}
	}
	return summary
}

func buildEncounterViews(segments []SplitSegment, utterances []Utterance) []EncounterView {
	encounterIndexes := make([]int, 0, len(segments))
	for idx, seg := range segments {
		if seg.SegmentType == "encounter" {
			encounterIndexes = append(encounterIndexes, idx)
		}
	}
	out := make([]EncounterView, 0, len(encounterIndexes))
	for pos, segIdx := range encounterIndexes {
		seg := segments[segIdx]
		item := EncounterView{
			EncounterNumber:    pos + 1,
			SegmentIndex:       seg.Index,
			StartSeconds:       seg.StartSeconds,
			EndSeconds:         seg.EndSeconds,
			DurationSeconds:    seg.DurationSeconds,
			PatientClue:        extractPatientClue(seg),
			OpeningLine:        openingLine(seg.Utterances),
			ClosingLine:        closingLine(seg.Utterances),
			FullText:           seg.FullText,
			BoundaryConfidence: seg.BoundaryConfidence,
			ConfidenceLabel:    seg.ConfidenceLabel,
			ReasonLabels:       seg.ReasonLabels,
			NeedsReview:        seg.NeedsReview,
			MultiPatient:       seg.MultiPatient,
			Utterances:         seg.Utterances,
		}
		if pos > 0 {
			prevSegIdx := encounterIndexes[pos-1]
			item.BoundaryFromPrev = buildEncounterBoundaryView(pos, segments[prevSegIdx], seg, segments[prevSegIdx+1:segIdx], utterances)
		}
		out = append(out, item)
	}
	return out
}

func (s *Service) fillEncounterSummaries(ctx context.Context, model string, encounters []EncounterView, report func(SplitProgress), totalChunks, partialSegments int) error {
	for idx := range encounters {
		if report != nil {
			report(SplitProgress{
				Stage:           "summary",
				Message:         fmt.Sprintf("正在生成就诊摘要... 第 %d/%d 个", idx+1, len(encounters)),
				TotalChunks:     totalChunks,
				CompletedChunks: totalChunks,
				CurrentChunk:    totalChunks,
				SummaryTotal:    len(encounters),
				SummaryDone:     idx,
				CurrentSummary:  idx + 1,
				PartialSegments: partialSegments,
			})
		}
		summary, err := s.summarizeEncounterTextWithFallback(ctx, model, encounters[idx].FullText, false)
		if err != nil {
			return fmt.Errorf("summarize encounter %d: %w", encounters[idx].EncounterNumber, err)
		}
		encounters[idx].PatientHint = strings.TrimSpace(summary.PatientHint)
		encounters[idx].ChiefComplaint = strings.TrimSpace(summary.ChiefComplaint)
		encounters[idx].Disposition = strings.TrimSpace(summary.Disposition)
	}
	if report != nil {
		report(SplitProgress{
			Stage:           "summary",
			Message:         fmt.Sprintf("就诊摘要生成完成，共 %d 个", len(encounters)),
			TotalChunks:     totalChunks,
			CompletedChunks: totalChunks,
			CurrentChunk:    totalChunks,
			SummaryTotal:    len(encounters),
			SummaryDone:     len(encounters),
			CurrentSummary:  len(encounters),
			PartialSegments: partialSegments,
		})
	}
	return nil
}

func (s *Service) summarizeEncounterTextWithFallback(ctx context.Context, model, encounterText string, retried bool) (*EncounterSummary, error) {
	summary, err := s.summarizeEncounterTextOnce(ctx, model, encounterText)
	if err == nil {
		return summary, nil
	}
	if !retried && strings.Contains(err.Error(), "Range of input length") {
		chunks := splitEncounterTextForSummary(encounterText, summaryTranscriptBudget())
		if len(chunks) > 1 {
			parts := make([]EncounterSummary, 0, len(chunks))
			for _, chunk := range chunks {
				item, chunkErr := s.summarizeEncounterTextWithFallback(ctx, model, chunk, true)
				if chunkErr != nil {
					return nil, chunkErr
				}
				parts = append(parts, *item)
			}
			merged := mergeEncounterSummaries(parts)
			return &merged, nil
		}
	}
	return nil, err
}

func (s *Service) summarizeEncounterTextOnce(ctx context.Context, model, encounterText string) (*EncounterSummary, error) {
	temp := 0.1
	prompt := strings.ReplaceAll(encounterSummaryUserPrompt, "{{encounter_text}}", encounterText)
	userOverheadRunes := utf8.RuneCountInString(prompt) - utf8.RuneCountInString(encounterText)
	log.Printf("[splitdemo] summary input: system=%d rune, user=%d rune, transcript=%d rune, total=%d rune, est_tokens=%d",
		utf8.RuneCountInString(encounterSummarySystemPrompt),
		userOverheadRunes,
		utf8.RuneCountInString(encounterText),
		utf8.RuneCountInString(encounterSummarySystemPrompt)+utf8.RuneCountInString(prompt),
		estimateTokens(encounterSummarySystemPrompt+prompt))
	content, _, err := s.llm.ChatCompletion(ctx, llmChatRequest{
		Model: model,
		Messages: []llmMessage{
			{Role: "system", Content: encounterSummarySystemPrompt},
			{Role: "user", Content: prompt},
		},
		Temperature: &temp,
	})
	if err != nil {
		return nil, err
	}
	summary, err := parseEncounterSummaryOutput(content)
	if err != nil {
		return nil, err
	}
	return &summary, nil
}

func summaryTranscriptBudget() int {
	templateUser := strings.ReplaceAll(encounterSummaryUserPrompt, "{{encounter_text}}", "")
	overhead := estimateTokens(encounterSummarySystemPrompt) + estimateTokens(templateUser)
	availableTokens := dashscopeInputLimit - overhead - summaryOutputReserve
	budget := int(float64(availableTokens) / tokenPerRuneRatio)
	if budget < 3000 {
		return 3000
	}
	return budget
}

func splitEncounterTextForSummary(text string, maxRunes int) []string {
	if strings.TrimSpace(text) == "" {
		return nil
	}
	if maxRunes <= 0 {
		maxRunes = summaryTranscriptBudget()
	}
	lines := strings.Split(text, "\n")
	chunks := make([]string, 0, 4)
	var current []string
	currentRunes := 0
	for _, line := range lines {
		lineRunes := utf8.RuneCountInString(line) + 1
		if lineRunes > maxRunes {
			if len(current) > 0 {
				chunks = append(chunks, strings.Join(current, "\n"))
				current = nil
				currentRunes = 0
			}
			runes := []rune(line)
			for start := 0; start < len(runes); start += maxRunes {
				end := minInt(len(runes), start+maxRunes)
				chunks = append(chunks, string(runes[start:end]))
			}
			continue
		}
		if len(current) > 0 && currentRunes+lineRunes > maxRunes {
			chunks = append(chunks, strings.Join(current, "\n"))
			current = nil
			currentRunes = 0
		}
		current = append(current, line)
		currentRunes += lineRunes
	}
	if len(current) > 0 {
		chunks = append(chunks, strings.Join(current, "\n"))
	}
	return chunks
}

func mergeEncounterSummaries(items []EncounterSummary) EncounterSummary {
	return EncounterSummary{
		PatientHint:    joinUniqueSummaryFields(items, func(item EncounterSummary) string { return item.PatientHint }, 2),
		ChiefComplaint: joinUniqueSummaryFields(items, func(item EncounterSummary) string { return item.ChiefComplaint }, 3),
		Disposition:    joinUniqueSummaryFields(items, func(item EncounterSummary) string { return item.Disposition }, 3),
	}
}

func joinUniqueSummaryFields(items []EncounterSummary, pick func(EncounterSummary) string, limit int) string {
	seen := map[string]struct{}{}
	out := make([]string, 0, len(items))
	for _, item := range items {
		value := strings.TrimSpace(pick(item))
		if value == "" {
			continue
		}
		if _, ok := seen[value]; ok {
			continue
		}
		seen[value] = struct{}{}
		out = append(out, value)
		if limit > 0 && len(out) >= limit {
			break
		}
	}
	return strings.Join(out, "；")
}

func parseEncounterSummaryOutput(content string) (EncounterSummary, error) {
	cleaned := stripCodeFence(strings.TrimSpace(content))
	start := strings.Index(cleaned, "{")
	end := strings.LastIndex(cleaned, "}")
	if start >= 0 && end > start {
		cleaned = cleaned[start : end+1]
	}
	var out EncounterSummary
	if err := json.Unmarshal([]byte(cleaned), &out); err != nil {
		return EncounterSummary{}, err
	}
	return out, nil
}

func openingLine(utterances []Utterance) string {
	if len(utterances) == 0 {
		return ""
	}
	return shortenText(utterances[0].Text, 30)
}

func closingLine(utterances []Utterance) string {
	if len(utterances) == 0 {
		return ""
	}
	return shortenText(utterances[len(utterances)-1].Text, 30)
}

func buildEncounterBoundaryView(previousNumber int, prevEncounter, nextEncounter SplitSegment, middle []SplitSegment, utterances []Utterance) *EncounterBoundaryView {
	view := &EncounterBoundaryView{
		PreviousEncounterNumber: previousNumber,
		NextEncounterNumber:     previousNumber + 1,
		StartSeconds:            prevEncounter.EndSeconds,
		EndSeconds:              nextEncounter.StartSeconds,
		DurationSeconds:         maxInt(nextEncounter.StartSeconds-prevEncounter.EndSeconds, 0),
		LeftUtterances:          tailUtterances(prevEncounter.Utterances, 5),
		RightUtterances:         headUtterances(nextEncounter.Utterances, 5),
		GapItems:                make([]EncounterGapItem, 0, len(middle)),
	}
	for _, seg := range middle {
		item := EncounterGapItem{
			SegmentIndex:    seg.Index,
			SegmentType:     seg.SegmentType,
			StartSeconds:    seg.StartSeconds,
			EndSeconds:      seg.EndSeconds,
			DurationSeconds: seg.DurationSeconds,
			Label:           segmentGapLabel(seg),
			TextPreview:     shortenText(stripTimestampPrefix(seg.FullText), 80),
		}
		if seg.SegmentType == "idle" {
			item.TextPreview = ""
		}
		view.GapItems = append(view.GapItems, item)
	}
	view.GapSummary = buildGapSummary(view.GapItems)
	return view
}

func tailUtterances(items []Utterance, limit int) []Utterance {
	if len(items) <= limit {
		return items
	}
	return items[len(items)-limit:]
}

func headUtterances(items []Utterance, limit int) []Utterance {
	if len(items) <= limit {
		return items
	}
	return items[:limit]
}

func segmentGapLabel(seg SplitSegment) string {
	switch seg.SegmentType {
	case "idle":
		return "静默"
	case "non_encounter_talk":
		return "非就诊对话"
	case "uncertain":
		return "待确认"
	default:
		return seg.SegmentType
	}
}

func buildGapSummary(items []EncounterGapItem) string {
	if len(items) == 0 {
		return "两次就诊直接相连，无中间间隙"
	}
	parts := make([]string, 0, len(items))
	for _, item := range items {
		switch item.SegmentType {
		case "idle":
			parts = append(parts, fmt.Sprintf("静默 %s", formatDurationChinese(item.DurationSeconds)))
		case "non_encounter_talk":
			if item.TextPreview != "" {
				parts = append(parts, fmt.Sprintf("非就诊对话 %s：%s", formatDurationChinese(item.DurationSeconds), item.TextPreview))
			} else {
				parts = append(parts, fmt.Sprintf("非就诊对话 %s", formatDurationChinese(item.DurationSeconds)))
			}
		case "uncertain":
			parts = append(parts, fmt.Sprintf("待确认 %s", formatDurationChinese(item.DurationSeconds)))
		}
	}
	return strings.Join(parts, "；")
}

func extractPatientClue(seg SplitSegment) string {
	source := stripTimestampPrefix(seg.FullText)
	if source == "" {
		source = seg.TextPreview
	}
	runes := []rune(strings.TrimSpace(source))
	if len(runes) > 200 {
		source = string(runes[:200])
	}
	titles := []string{"阿姨", "大爷", "小伙子", "师傅", "姑娘", "大姐", "阿叔", "阿伯", "靓女", "小朋友", "阿弟", "阿姨啊"}
	symptoms := []string{"胃疼", "头晕", "咳嗽", "发烧", "牙痛", "肚子痛", "胸闷", "流鼻涕", "出血", "胀", "麻", "痛", "疼"}
	title := firstMatch(source, titles)
	symptom := firstMatch(source, symptoms)
	switch {
	case title != "" && symptom != "":
		return title + "、" + symptom
	case title != "":
		return title
	case symptom != "":
		return symptom
	default:
		return "—"
	}
}

func firstMatch(source string, items []string) string {
	for _, item := range items {
		if strings.Contains(source, item) {
			return item
		}
	}
	return ""
}

func stripTimestampPrefix(text string) string {
	lines := strings.Split(strings.TrimSpace(text), "\n")
	cleaned := make([]string, 0, len(lines))
	for _, line := range lines {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		if idx := strings.Index(line, "] "); strings.HasPrefix(line, "[") && idx >= 0 {
			line = line[idx+2:]
		}
		cleaned = append(cleaned, line)
	}
	return strings.Join(cleaned, " ")
}

func validateSegments(segments []SplitSegment, totalSeconds int) ValidationResult {
	result := ValidationResult{
		IsValid:        true,
		StartsAtZero:   len(segments) > 0 && segments[0].StartSeconds == 0,
		EndsAtDuration: len(segments) > 0 && segments[len(segments)-1].EndSeconds == totalSeconds,
		Continuous:     true,
	}
	if !result.StartsAtZero {
		result.Messages = append(result.Messages, "第一个片段不是从 00:00 开始")
	}
	if !result.EndsAtDuration {
		result.Messages = append(result.Messages, "最后一个片段没有覆盖到录音结尾")
	}
	for i := 0; i < len(segments)-1; i++ {
		if segments[i].EndSeconds < segments[i+1].StartSeconds {
			result.GapCount++
			result.Continuous = false
		}
		if segments[i].EndSeconds > segments[i+1].StartSeconds {
			result.OverlapCount++
			result.Continuous = false
		}
	}
	if result.GapCount > 0 {
		result.Messages = append(result.Messages, fmt.Sprintf("存在 %d 处间隙", result.GapCount))
	}
	if result.OverlapCount > 0 {
		result.Messages = append(result.Messages, fmt.Sprintf("存在 %d 处重叠", result.OverlapCount))
	}
	result.IsValid = result.StartsAtZero && result.EndsAtDuration && result.Continuous
	if result.IsValid {
		result.Messages = append(result.Messages, "契约校验通过：完整覆盖、连续不重叠")
	}
	return result
}

func sliceUtterancesByRange(utterances []Utterance, start, end int) []Utterance {
	out := make([]Utterance, 0, 16)
	for _, item := range utterances {
		if item.EndSeconds < start {
			continue
		}
		if item.StartSeconds > end {
			break
		}
		out = append(out, item)
	}
	return out
}

func aroundBoundaryLeft(utterances []Utterance, boundaryAt, limit int) []Utterance {
	tmp := make([]Utterance, 0, limit)
	for _, item := range utterances {
		if item.StartSeconds >= boundaryAt {
			break
		}
		tmp = append(tmp, item)
	}
	if len(tmp) <= limit {
		return tmp
	}
	return tmp[len(tmp)-limit:]
}

func aroundBoundaryRight(utterances []Utterance, boundaryAt, limit int) []Utterance {
	out := make([]Utterance, 0, limit)
	for _, item := range utterances {
		if item.EndSeconds <= boundaryAt {
			continue
		}
		out = append(out, item)
		if len(out) >= limit {
			break
		}
	}
	return out
}

func joinUtterances(utterances []Utterance) string {
	lines := make([]string, 0, len(utterances))
	for _, item := range utterances {
		lines = append(lines, fmt.Sprintf("[%s] %s: %s", formatSeconds(item.StartSeconds), speakerLabel(item), item.Text))
	}
	return strings.Join(lines, "\n")
}

func speakerLabel(item Utterance) string {
	if strings.TrimSpace(item.SpeakerRole) != "" {
		return item.SpeakerRole
	}
	return item.Speaker
}

func shortenText(text string, limit int) string {
	runes := []rune(strings.TrimSpace(text))
	if len(runes) <= limit {
		return string(runes)
	}
	return string(runes[:limit]) + "..."
}

func confidenceLabel(v float64) string {
	switch {
	case v >= 0.85:
		return "高"
	case v >= 0.65:
		return "中"
	default:
		return "低"
	}
}

func translateReasonLabels(items []string) []string {
	out := make([]string, 0, len(items))
	for _, item := range items {
		item = strings.TrimSpace(item)
		if item == "" {
			continue
		}
		lower := strings.ToLower(item)
		switch {
		case strings.HasPrefix(lower, "closing_phrase:"):
			out = append(out, "结束语 - "+strings.TrimSpace(strings.SplitN(item, ":", 2)[1]))
		case strings.HasPrefix(lower, "opening_phrase:"):
			out = append(out, "开场语 - "+strings.TrimSpace(strings.SplitN(item, ":", 2)[1]))
		case strings.HasPrefix(lower, "greeting:"):
			out = append(out, "寒暄开场 - "+strings.TrimSpace(strings.SplitN(item, ":", 2)[1]))
		case strings.HasPrefix(lower, "topic_shift"):
			if strings.Contains(item, ":") {
				out = append(out, "话题切换 - "+strings.TrimSpace(strings.SplitN(item, ":", 2)[1]))
			} else {
				out = append(out, "话题切换")
			}
		case strings.HasPrefix(lower, "silence_"):
			out = append(out, "静默 "+strings.TrimPrefix(lower, "silence_")+" 秒")
		case strings.Contains(lower, "next_patient_call"):
			out = append(out, "出现下一位患者信号")
		default:
			out = append(out, item)
		}
	}
	return out
}

func correctionReasonOptions() []CorrectionReasonOption {
	return []CorrectionReasonOption{
		{Code: string(CorrectionReasonPatientReturned), Label: "患者中途离开后返回", Description: "患者去做检查/取药后回到诊室，被误判为两次就诊", Actions: []string{"merge"}},
		{Code: string(CorrectionReasonColleagueTalk), Label: "医生与同事交流", Description: "医生和护士/同事讨论，被误判为一次就诊", Actions: []string{"delete"}},
		{Code: string(CorrectionReasonMultiPatient), Label: "多患者同诊", Description: "夫妻同诊、母子同诊被切成多段", Actions: []string{"merge"}},
		{Code: string(CorrectionReasonFamilyProxy), Label: "家属代述", Description: "家属替患者描述病情，被误判为独立就诊", Actions: []string{"merge"}},
		{Code: string(CorrectionReasonTopicShift), Label: "同一患者话题跳转", Description: "同一患者连续问不相关问题，被误判为两次", Actions: []string{"merge"}},
		{Code: string(CorrectionReasonFalseBoundary), Label: "同一患者被无故切开", Description: "同一患者的连续沟通，中间没有任何话题跳转，却被切成两段", Actions: []string{"merge"}},
		{Code: string(CorrectionReasonMentionedPatient), Label: "举例提到的其他患者", Description: "医生举例说'有个和你类似的病人'，被误判为新的就诊", Actions: []string{"merge", "delete"}},
		{Code: string(CorrectionReasonMissedBoundary), Label: "漏切边界", Description: "两个不同患者被合并成一段", Actions: []string{"split"}},
		{Code: string(CorrectionReasonBoundaryOffset), Label: "边界位置偏移", Description: "类型判对了，但起止秒数不准", Actions: []string{"adjust"}},
		{Code: string(CorrectionReasonNotEncounter), Label: "非就诊内容", Description: "问路、取报告、纯闲聊被判成就诊", Actions: []string{"delete"}},
		{Code: string(CorrectionReasonMissingEncounter), Label: "遗漏的就诊", Description: "AI 完全没识别出这段就诊", Actions: []string{"add"}},
		{Code: string(CorrectionReasonOther), Label: "其他", Description: "需要补充备注", Actions: []string{"merge", "split", "adjust", "delete", "add"}},
	}
}

func buildReasonStats(stats map[string]int) []CorrectionReasonStat {
	options := correctionReasonOptions()
	out := make([]CorrectionReasonStat, 0, len(options))
	for _, option := range options {
		if count := stats[option.Code]; count > 0 {
			out = append(out, CorrectionReasonStat{Code: option.Code, Label: option.Label, Count: count})
		}
	}
	sort.SliceStable(out, func(i, j int) bool {
		if out[i].Count == out[j].Count {
			return out[i].Code < out[j].Code
		}
		return out[i].Count > out[j].Count
	})
	return out
}

func buildAnnotationSummary(record *AnnotationRecord) *AnnotationSummary {
	if record == nil {
		return nil
	}
	stats := map[string]int{}
	for _, item := range record.Corrections {
		code := strings.TrimSpace(item.ReasonCode)
		if code == "" {
			code = string(CorrectionReasonOther)
		}
		stats[code]++
	}
	return &AnnotationSummary{
		RecordingID:      record.RecordingID,
		TotalCorrections: len(record.Corrections),
		ByReason:         buildReasonStats(stats),
	}
}

func shortenPreviewText(text string, limit int) string {
	runes := []rune(strings.TrimSpace(text))
	if len(runes) <= limit {
		return string(runes)
	}
	return string(runes[:limit]) + "..."
}

func formatDurationChinese(totalSeconds int) string {
	if totalSeconds <= 0 {
		return "0秒"
	}
	hours := totalSeconds / 3600
	minutes := (totalSeconds % 3600) / 60
	seconds := totalSeconds % 60
	parts := make([]string, 0, 3)
	if hours > 0 {
		parts = append(parts, fmt.Sprintf("%d小时", hours))
	}
	if minutes > 0 || hours > 0 {
		parts = append(parts, fmt.Sprintf("%d分", minutes))
	}
	parts = append(parts, fmt.Sprintf("%d秒", seconds))
	return strings.Join(parts, "")
}

func recordingDurationSeconds(recording *RecordingDetail) int {
	if recording == nil {
		return 0
	}
	if recording.RecordingDuration == nil || *recording.RecordingDuration <= 0 {
		maxSeen := 0
		for _, seg := range append(append(append(recording.TranscriptionSegs, recording.CleanedTranscription...), recording.TimelineTranscript...), recording.StructuredTranscript...) {
			if end := pickSecondsValue(seg, "end_seconds", "end_time", "end"); end > maxSeen {
				maxSeen = end
			}
		}
		return maxSeen
	}
	return *recording.RecordingDuration
}

func pickSecondsValue(m map[string]interface{}, keys ...string) int {
	// 第一优先级：直接按调用方传入的 keys 查找（秒级字段）
	if value := pickFirstRaw(m, keys...); value != nil {
		return numericToInt(value)
	}

	// 第二优先级：尝试秒级别名（不做单位换算）
	secondsAliases := []string{
		"startTime", "start_time", "start", "begin", "begin_time",
		"sentence_begin", "sentence_start", "from", "offset",
	}
	endAliases := []string{
		"endTime", "end_time", "end", "finish", "finish_time",
		"sentence_end", "sentence_finish", "to",
	}
	isEnd := false
	for _, key := range keys {
		if strings.Contains(key, "end") {
			isEnd = true
			break
		}
	}
	aliases := secondsAliases
	if isEnd {
		aliases = endAliases
	}
	if value := pickFirstRaw(m, aliases...); value != nil {
		v := numericToInt(value)
		// 如果值超过 86400（一天秒数），大概率是毫秒
		if v > 86400 {
			return int(math.Round(float64(v) / 1000.0))
		}
		return v
	}

	// 第三优先级：毫秒字段，按调用方 key 匹配对应的 _ms 变体
	for _, key := range keys {
		var msVariants []string
		switch {
		case strings.Contains(key, "start"):
			msVariants = []string{
				"start_ms", "start_time_ms", "startTimeMs",
				"begin_ms", "begin_time_ms", "beginTimeMs",
				"sentence_begin_ms", "sentenceBeginMs",
				"from_ms", "fromMs", "offset_ms", "offsetMs",
			}
		case strings.Contains(key, "end"):
			msVariants = []string{
				"end_ms", "end_time_ms", "endTimeMs",
				"finish_ms", "finish_time_ms", "finishTimeMs",
				"sentence_end_ms", "sentenceEndMs",
				"to_ms", "toMs",
			}
		}
		if value := pickFirstRaw(m, msVariants...); value != nil {
			return int(math.Round(float64(numericToInt(value)) / 1000.0))
		}
	}
	return 0
}

func pickFirstRaw(m map[string]interface{}, keys ...string) interface{} {
	for _, key := range keys {
		if value, ok := m[key]; ok {
			return value
		}
	}
	return nil
}

func formatSeconds(sec int) string {
	if sec < 0 {
		sec = 0
	}
	m := sec / 60
	s := sec % 60
	return fmt.Sprintf("%02d:%02d", m, s)
}

func stripCodeFence(content string) string {
	content = strings.TrimSpace(content)
	if strings.HasPrefix(content, "```") {
		content = strings.TrimPrefix(content, "```json")
		content = strings.TrimPrefix(content, "```")
		content = strings.TrimSuffix(content, "```")
	}
	return strings.TrimSpace(content)
}

func firstText(m map[string]interface{}, keys ...string) string {
	for _, key := range keys {
		if value, ok := m[key]; ok {
			if s, ok := value.(string); ok {
				s = strings.TrimSpace(s)
				if s != "" {
					return s
				}
			}
		}
	}
	return ""
}

func pickIntValue(m map[string]interface{}, keys ...string) int {
	for _, key := range keys {
		if value, ok := m[key]; ok {
			return numericToInt(value)
		}
	}
	return 0
}

func numericToInt(value interface{}) int {
	switch v := value.(type) {
	case int:
		return v
	case int32:
		return int(v)
	case int64:
		return int(v)
	case float64:
		return int(math.Round(v))
	case json.Number:
		if parsed, err := v.Float64(); err == nil {
			return int(math.Round(parsed))
		}
	case string:
		if parsed, err := strconv.ParseFloat(strings.TrimSpace(v), 64); err == nil {
			return int(math.Round(parsed))
		}
	}
	return 0
}

func pickFloatValue(m map[string]interface{}, keys ...string) float64 {
	for _, key := range keys {
		if value, ok := m[key]; ok {
			switch v := value.(type) {
			case float64:
				return v
			case float32:
				return float64(v)
			case int:
				return float64(v)
			case int64:
				return float64(v)
			case string:
				if parsed, err := strconv.ParseFloat(strings.TrimSpace(v), 64); err == nil {
					return parsed
				}
			}
		}
	}
	return 0
}

func pickBoolValue(m map[string]interface{}, keys ...string) bool {
	for _, key := range keys {
		if value, ok := m[key]; ok {
			switch v := value.(type) {
			case bool:
				return v
			case string:
				return strings.EqualFold(strings.TrimSpace(v), "true") || v == "1"
			case int:
				return v != 0
			case int64:
				return v != 0
			}
		}
	}
	return false
}

func pickOptionalBool(m map[string]interface{}, key string) *bool {
	if value, ok := m[key]; ok {
		v := pickBoolValue(map[string]interface{}{key: value}, key)
		return &v
	}
	return nil
}

func pickStringSlice(m map[string]interface{}, keys ...string) []string {
	for _, key := range keys {
		if value, ok := m[key]; ok {
			switch v := value.(type) {
			case []interface{}:
				out := make([]string, 0, len(v))
				for _, item := range v {
					if s, ok := item.(string); ok && strings.TrimSpace(s) != "" {
						out = append(out, strings.TrimSpace(s))
					}
				}
				if len(out) > 0 {
					return out
				}
			case []string:
				if len(v) > 0 {
					return v
				}
			case string:
				if strings.TrimSpace(v) != "" {
					return []string{strings.TrimSpace(v)}
				}
			}
		}
	}
	return nil
}

func clamp(v, min, max int) int {
	if v < min {
		return min
	}
	if v > max {
		return max
	}
	return v
}
