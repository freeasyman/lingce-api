package emrrecord

import (
	"fmt"
	"strings"
)

// validateContentChangeAudit 在病历正式保存前校验修改留痕的必要输入。
//
// 操作人由当前登录身份提供，修改时间由 emr_process_records.occurred_at
// 自动生成，修改前后内容由正式快照和 content_changes 保存。因此业务层
// 只要求修改原因在写入前存在；写入成功后，appendProcessResult 会把其余
// 审计信息一次性写入过程记录。内容没有变化时不要求修改原因。
//
// 署名：Codex
// 时间：2026-09-14
func validateContentChangeAudit(actorID int64, before, after map[string]any, note string) error {
	changes := diffTopLevel(before, after)
	if len(changes) == 0 {
		return nil
	}
	if actorID <= 0 {
		return fmt.Errorf("病历内容已修改，缺少有效操作人")
	}
	if strings.TrimSpace(note) == "" {
		return fmt.Errorf("病历内容已修改，必须填写修改原因")
	}
	if err := validateAuditChanges(changes); err != nil {
		return err
	}
	return nil
}

// validateAuditTrail 校验正式病历操作写入过程记录前所需的审计元数据。
//
// 操作人来自当前登录身份；操作时间由 emr_process_records.occurred_at 的
// 数据库默认值生成；前后内容及字段变化由本次正式快照和 diffTopLevel
// 生成。这里在写过程记录前验证这三类输入，避免因为业务代码遗漏而产生
// 不完整的审计记录。新增正式快照没有“之前的正式快照”是合法情况，
// 但每个实际变化项必须同时保留 before 和 after 两个键（新增/删除时
// 其中一个值可以是 null）。
//
// 署名：Codex
// 时间：2026-09-14
func validateAuditTrail(actorID int64, note string, outcome *WriteOutcome, changes []any) error {
	if actorID <= 0 {
		return fmt.Errorf("审计记录缺少有效操作人")
	}
	if len(changes) == 0 {
		return nil
	}
	if outcome == nil || outcome.Snapshot == nil || strings.TrimSpace(outcome.Snapshot.ID) == "" {
		return fmt.Errorf("病历内容已修改，但缺少修改后快照")
	}
	if strings.TrimSpace(note) == "" {
		return fmt.Errorf("病历内容已修改，必须填写修改原因")
	}
	return validateAuditChanges(changes)
}

func validateAuditChanges(changes []any) error {
	for index, rawChange := range changes {
		change, ok := rawChange.(map[string]any)
		if !ok {
			return fmt.Errorf("审计记录第 %d 项修改明细格式无效", index+1)
		}
		for _, key := range []string{"section_code", "change_type", "before", "after"} {
			if _, exists := change[key]; !exists {
				return fmt.Errorf("审计记录第 %d 项缺少%s", index+1, key)
			}
		}
	}
	return nil
}
