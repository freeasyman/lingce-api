package his

import (
	"fmt"
	"strings"
	"time"
	"unicode/utf8"
)

// ErrTreatmentCompleted 表示治疗医嘱已达到计划次数，不能继续执行。
// 包装 ErrInvalidState，保持既有调用方对 ErrInvalidState 的判断兼容。
var ErrTreatmentCompleted = fmt.Errorf("treatment order already completed: %w", ErrInvalidState)

const (
	maxTreatmentPlanTimes = 999
	maxTreatmentNoteRunes = 500
	treatmentDateLayout   = "2006-01-02"
)

// normalizeTreatmentStatusFilter 把执行站页签状态映射为数据库状态集合。
// pending 同时覆盖未开始（ordered）与已收费待执行（charged）。
func normalizeTreatmentStatusFilter(status string) ([]string, error) {
	switch strings.TrimSpace(status) {
	case "":
		return nil, nil
	case "pending", "ordered", "charged":
		return []string{"ordered", "charged"}, nil
	case "executing":
		return []string{"executing"}, nil
	case "completed":
		return []string{"completed"}, nil
	default:
		return nil, ErrInvalidRequest
	}
}

// normalizeTreatmentDate 校验执行站业务日期（YYYY-MM-DD），空值表示不筛选。
func normalizeTreatmentDate(date string) (string, error) {
	date = strings.TrimSpace(date)
	if date == "" {
		return "", nil
	}
	parsed, err := time.Parse(treatmentDateLayout, date)
	if err != nil {
		return "", ErrInvalidRequest
	}
	return parsed.Format(treatmentDateLayout), nil
}

// normalizeTreatmentPlanTotal 校验治疗计划次数：至少 1 次，上限防止误录。
func normalizeTreatmentPlanTotal(times int) (int, error) {
	if times < 1 || times > maxTreatmentPlanTimes {
		return 0, ErrInvalidRequest
	}
	return times, nil
}

// normalizeCreateTreatmentOrder 校验并清洗开治疗医嘱入参。
func normalizeCreateTreatmentOrder(req CreateTreatmentOrderRequest) (CreateTreatmentOrderRequest, error) {
	if req.EncounterID <= 0 {
		return req, ErrInvalidRequest
	}
	plan, err := normalizeTreatmentPlanTotal(req.PlanTotalTimes)
	if err != nil {
		return req, err
	}
	req.PlanTotalTimes = plan
	req.ChargeItemName = strings.TrimSpace(req.ChargeItemName)
	if req.ChargeItemID <= 0 && req.ChargeItemName == "" {
		return req, ErrInvalidRequest
	}
	return req, nil
}

// normalizeTreatmentNote 清洗执行备注：去掉首尾空白，限制长度（按字符数而非字节）。
func normalizeTreatmentNote(note string) (string, error) {
	note = strings.TrimSpace(note)
	if utf8.RuneCountInString(note) > maxTreatmentNoteRunes {
		return "", ErrInvalidRequest
	}
	return note, nil
}

// nextTreatmentProgress 计算下一次执行的次数与医嘱状态。
// 已达计划次数时返回 ErrTreatmentCompleted，避免重复执行或超出上限。
func nextTreatmentProgress(planTotal, completed int) (int, string, error) {
	if planTotal < 1 || completed < 0 {
		return 0, "", ErrInvalidRequest
	}
	if completed >= planTotal {
		return completed, "", ErrTreatmentCompleted
	}
	next := completed + 1
	status := "executing"
	if next >= planTotal {
		status = "completed"
	}
	return next, status, nil
}
