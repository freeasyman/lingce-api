package emrrecord

// 听写病历的「AI 增强降级」契约。
//
// 核心操作（保存病历 / 提交病历）永远可用；质检是 AI 增强，额度不足、服务暂停
// 或到期时跳过质检，但必须把结果显性告知医生并落库：
//   - 接口返回 quality.status / quality.message，前端据此提示；
//   - emr_records.quality_status / quality_status_reason / quality_status_at 留痕，
//     病历详情与列表能事后看出「这份病历没质检、为什么没质检」。
//
// 对照 B 类（生成病历草稿、赢客成交分析等本身即 AI 产出的操作）：没有 AI 就没有
// 产出，无法降级，仍由 billing 预检直接 402 阻断，不在本文件范围内。

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"time"

	"github.com/freeasyman/lingce-api/internal/billing"
)

// 落库到 emr_records.quality_status 的状态值。
const (
	// QualityStatusChecked 表示最近一次提交/归档级质检已经真实执行并落库。
	QualityStatusChecked = "checked"
	// QualityStatusSkippedNoQuota 表示质检额度不足，本次提交跳过质检但保存成功。
	QualityStatusSkippedNoQuota = "skipped_no_quota"
	// QualityStatusSkippedPaused 表示机构服务已暂停，本次提交跳过质检但保存成功。
	QualityStatusSkippedPaused = "skipped_subscription_paused"
	// QualityStatusSkippedExpired 表示机构服务已到期，本次提交跳过质检但保存成功。
	QualityStatusSkippedExpired = "skipped_subscription_expired"
)

// 面向医生的降级文案：必须说明「本次保存没有质检」与恢复方式，不能只写日志。
const (
	QualitySkipNoQuotaMessage = "质检额度已用完，本次保存未执行质检；续费后可恢复，请联系您的专属顾问"
	QualitySkipPausedMessage  = "服务已暂停，本次保存未执行质检；恢复服务后可恢复质检，请联系您的专属顾问"
	QualitySkipExpiredMessage = "服务已到期，本次保存未执行质检；续费后可恢复质检，请联系您的专属顾问"
)

// QualityDegradation 是一次质检降级的返回体，序列化到接口响应的 quality 字段：
//
//	{"ok": true, "quality": {"status": "skipped_no_quota", "message": "..."}}
type QualityDegradation struct {
	Status  string    `json:"status"`
	Message string    `json:"message"`
	Reason  string    `json:"reason,omitempty"`
	At      time.Time `json:"at"`
}

// MarshalJSON 保持 WriteOutcome 既有字段契约（Record/Snapshot 等 PascalCase），
// 并把质检报告或降级状态统一输出到 quality 字段，前端只需读一种结构。
func (o *WriteOutcome) MarshalJSON() ([]byte, error) {
	if o == nil {
		return []byte("null"), nil
	}
	type outcomeAlias WriteOutcome
	payload := struct {
		*outcomeAlias
		Quality any `json:"quality,omitempty"`
	}{outcomeAlias: (*outcomeAlias)(o)}
	if o.QualityReport != nil {
		payload.Quality = o.QualityReport
	} else if o.QualityStatus != nil {
		payload.Quality = o.QualityStatus
	}
	return json.Marshal(payload)
}

// qualityRoundSkip 判断本次提交能否真实执行一轮完成级质控。
//
// 返回 nil 表示照常执行质检并扣 round；返回非 nil 表示本次跳过质检（额度不足 /
// 服务暂停 / 到期），调用方必须继续保存病历。计费未接入或计费自身故障时放行，
// 不能因为计费不可用把医生的核心操作拖垮。
func (s *Service) qualityRoundSkip(ctx context.Context, tenantID int64) *QualityDegradation {
	if s == nil || s.billing == nil {
		return nil
	}
	err := s.billing.CheckQuota(ctx, tenantID, billingProductCode,
		[]billing.DeductItem{{Unit: billingUnitRound, Amount: 1}})
	if err == nil {
		return nil
	}
	gate, ok := billing.GateRejection(err)
	if !ok {
		// 计费自身故障（连不上库等）不是额度不足：保持既有 fail-open 口径，
		// 照常质检，避免计费故障把病历提交整体拖垮。
		slog.Warn("emr quality quota check failed, running quality anyway",
			"tenant_id", tenantID, "error", err)
		return nil
	}
	degradation := &QualityDegradation{At: time.Now()}
	switch gate.Code {
	case billing.CodeInsufficientEntitlement:
		degradation.Status = QualityStatusSkippedNoQuota
		degradation.Message = QualitySkipNoQuotaMessage
		if gate.Insufficient != nil {
			degradation.Reason = fmt.Sprintf("round 额度不足：当前 %.2f，本次需要 %.2f，缺口 %.2f",
				gate.Insufficient.Current, gate.Insufficient.Required, gate.Insufficient.Shortfall)
		}
	case billing.CodeSubscriptionPaused:
		degradation.Status = QualityStatusSkippedPaused
		degradation.Message = QualitySkipPausedMessage
		degradation.Reason = gate.Message
	case billing.CodeSubscriptionExpired:
		degradation.Status = QualityStatusSkippedExpired
		degradation.Message = QualitySkipExpiredMessage
		degradation.Reason = gate.Message
	default:
		// 未知拒绝口径按计费故障处理，不扩大阻断范围。
		return nil
	}
	slog.Warn("emr quality skipped, record save continues",
		"tenant_id", tenantID, "status", degradation.Status, "reason", degradation.Reason)
	return degradation
}
