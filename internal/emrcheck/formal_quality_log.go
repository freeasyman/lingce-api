package emrcheck

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"
)

const formalQualityLogPath = "/Users/yiliiang/Documents/lingce-web/apps/emr/data/log/emr_formal_quality_llm.log"

var formalQualityLogMu sync.Mutex

func appendFormalQualityLLMLog(entry map[string]any) {
	formalQualityLogMu.Lock()
	defer formalQualityLogMu.Unlock()

	if err := os.MkdirAll(filepath.Dir(formalQualityLogPath), 0o755); err != nil {
		return
	}
	raw, err := json.Marshal(entry)
	if err != nil {
		return
	}
	file, err := os.OpenFile(formalQualityLogPath, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o644)
	if err != nil {
		return
	}
	defer file.Close()
	_, _ = file.Write(append(raw, '\n'))
}

func newFormalQualityLogEntry() map[string]any {
	return map[string]any{
		"logged_at": time.Now().UTC().Format(time.RFC3339Nano),
		"source":    "formal_emr_quality_run",
	}
}

func qualityPromptSectionLengths(prompt string) map[string]int {
	markers := []string{
		"【当前病历模板】",
		"【模板栏位】",
		"【本次需要大模型判断的质量规则】",
		"【患者与就诊上下文】",
		"【结构化电子病历 JSON】",
		"【已经完成的程序质检结果】",
		"【输出要求】",
	}
	sections := make(map[string]int, len(markers))
	for index, marker := range markers {
		start := strings.Index(prompt, marker)
		if start < 0 {
			continue
		}
		end := len(prompt)
		for _, next := range markers[index+1:] {
			if candidate := strings.Index(prompt[start+len(marker):], next); candidate >= 0 {
				end = start + len(marker) + candidate
				break
			}
		}
		sections[marker] = end - start
	}
	return sections
}
