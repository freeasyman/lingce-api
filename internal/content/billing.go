package content

// 获客先锋（产品编码 huoke）的计费接入。
//
// 四个交付点与计量：
//
//   - 需求洞察完成：hour = 录音时长（recordings.duration 秒 / 3600，保留 2 位小数），
//     时长未知则跳过；由 worker 异步触发，不设预检闸门。
//   - AI 回答诊断完成：prompt 1（1 问题 × 3 家 AI × 1 轮 = 1 题次）。
//   - 文稿交付成功（生成并保存为正式文稿）：record 1。
//   - 图片交付成功（图片生成并写入内容）：shot 1，多图按"内容 + 图位"区分。
//
// 扣量一律发生在业务成功落库之后：ErrAlreadyDeducted 视为幂等成功，其他错误只告警、
// 不回滚业务（MVP 允许欠扣）。用户主动触发的 AI 诊断/文稿生成/图片生成入口在开始
// 生成前做 CheckQuota 预检；计费未接入或预检自身报错时放行（fail-open）。
// 选题生成、预览、草稿保存、查看/下载、列表查询均不扣量。

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"math"
	"net/http"

	"github.com/freeasyman/lingce-api/internal/billing"
	"github.com/freeasyman/lingce-api/pkg/httputil"
)

const (
	// contentBillingProductCode 是获客先锋的产品编码。
	contentBillingProductCode = "huoke"

	contentBillingUnitHour   = "hour"
	contentBillingUnitPrompt = "prompt"
	contentBillingUnitRecord = "record"
	contentBillingUnitShot   = "shot"

	contentBillingReasonInsight      = "获客先锋-需求洞察"
	contentBillingReasonAIVisibility = "获客先锋-AI回答诊断"
	contentBillingReasonArticle      = "获客先锋-文稿交付"
	contentBillingReasonImage        = "获客先锋-图片交付"

	// contentQuotaExhaustedCode 是交付前闸门拒绝时返回给调用方的错误码。
	contentQuotaExhaustedCode = "CONTENT_QUOTA_EXHAUSTED"
)

// ErrContentQuotaExhausted 表示预检发现获客先锋额度不足，生成入口据此拒绝开始生成。
var ErrContentQuotaExhausted = errors.New("额度已用完，请及时联系客服，加购增量包")

// contentQuotaExhaustedError 是闸门拒绝的具体错误，携带缺口明细；同时满足
// errors.Is(err, ErrContentQuotaExhausted) 与 errors.As(err, *billing.InsufficientEntitlementError)，
// 便于入口区分"额度不足"与计费自身故障。
type contentQuotaExhaustedError struct {
	insufficient *billing.InsufficientEntitlementError
}

func (e *contentQuotaExhaustedError) Error() string {
	if e == nil || e.insufficient == nil {
		return ErrContentQuotaExhausted.Error()
	}
	return fmt.Sprintf("%s（%s 额度不足：当前 %.2f，本次需要 %.2f）",
		ErrContentQuotaExhausted.Error(), e.insufficient.Unit, e.insufficient.Current, e.insufficient.Required)
}

func (e *contentQuotaExhaustedError) Unwrap() []error {
	if e == nil || e.insufficient == nil {
		return []error{ErrContentQuotaExhausted}
	}
	return []error{ErrContentQuotaExhausted, e.insufficient}
}

// billingService 是本模块依赖的最小计费能力，*billing.Service 天然实现；
// 单元测试注入 fake，避免依赖真实数据库与计费事务。
type billingService interface {
	Deduct(ctx context.Context, input billing.DeductInput) (*billing.DeductResult, error)
	CheckQuota(ctx context.Context, tenantID int64, productCode string, items []billing.DeductItem) error
}

// UseBilling 给获客先锋接入计费：服务层持有计费能力，Handler 只负责把预检拒绝
// 映射成 HTTP 响应。nil 表示不接计费（旧部署/测试）。
func (s *Service) UseBilling(svc *billing.Service) {
	if s == nil {
		return
	}
	s.billing = wrapBillingService(svc)
}

// wrapBillingService 避免 nil 指针被包成非空接口（typed-nil 陷阱）。
func wrapBillingService(svc *billing.Service) billingService {
	if svc == nil {
		return nil
	}
	return svc
}

// recordingSeconds 读取录音时长（recordings.duration 单位为秒）。
//
// lookupRecordingDuration 是单测注入点；生产环境走数据库。查不到时返回错误，
// 调用方按"时长未知"处理：跳过本次 hour 扣量。
func (s *Service) recordingSeconds(ctx context.Context, tenantID, recordingID int64) (int64, error) {
	if s.lookupRecordingDuration != nil {
		return s.lookupRecordingDuration(ctx, tenantID, recordingID)
	}
	if s.store == nil || s.store.pool == nil {
		return 0, fmt.Errorf("database pool is not configured")
	}
	var seconds int64
	if err := s.store.pool.QueryRow(ctx, `
		SELECT COALESCE(duration, 0)
		FROM recordings
		WHERE id=$1 AND tenant_id=$2 AND deleted_at IS NULL
	`, recordingID, tenantID).Scan(&seconds); err != nil {
		return 0, fmt.Errorf("load recording duration: %w", err)
	}
	return seconds, nil
}

// recordingHours 把秒换算成小时，四舍五入到 2 位小数。
func recordingHours(seconds int64) float64 {
	if seconds <= 0 {
		return 0
	}
	return math.Round(float64(seconds)/3600*100) / 100
}

// ensureContentQuota 是交付前只读预检：额度不足返回 ErrContentQuotaExhausted；
// 计费未接入或预检本身报错时放行并告警（fail-open），不能因计费故障拖垮业务。
func (s *Service) ensureContentQuota(ctx context.Context, tenantID int64, unit string, amount float64) error {
	if s == nil || s.billing == nil {
		return nil
	}
	err := s.billing.CheckQuota(ctx, tenantID, contentBillingProductCode,
		[]billing.DeductItem{{Unit: unit, Amount: amount}})
	if err == nil {
		return nil
	}
	if !errors.Is(err, billing.ErrInsufficientEntitlement) {
		slog.Warn("content quota precheck failed, allowing delivery",
			"tenant_id", tenantID, "product_code", contentBillingProductCode, "unit", unit, "error", err)
		return nil
	}
	var insufficient *billing.InsufficientEntitlementError
	if errors.As(err, &insufficient) {
		return &contentQuotaExhaustedError{insufficient: insufficient}
	}
	return ErrContentQuotaExhausted
}

// insightDeductionInput 组装需求洞察的扣量请求：hour = 录音时长（保留 2 位），
// 幂等键固定为 content:insight:<recordingID>:analysis。
// 时长未知（<= 0）或四舍五入后为 0 时返回 ok=false，调用方跳过整笔扣量。
func insightDeductionInput(tenantID, recordingID, seconds int64) (billing.DeductInput, bool) {
	hours := recordingHours(seconds)
	if recordingID <= 0 || hours <= 0 {
		return billing.DeductInput{}, false
	}
	return billing.DeductInput{
		TenantID:       tenantID,
		ProductCode:    contentBillingProductCode,
		Items:          []billing.DeductItem{{Unit: contentBillingUnitHour, Amount: hours}},
		IdempotencyKey: fmt.Sprintf("content:insight:%d:analysis", recordingID),
		Reason:         contentBillingReasonInsight,
	}, true
}

// aiVisibilityDeductionInput 组装 AI 回答诊断的扣量请求：prompt 1 题次，
// 幂等键 content:ai-visibility:<runID>:round（一次诊断 = 一个问题的一轮对照）。
func aiVisibilityDeductionInput(tenantID, runID int64) billing.DeductInput {
	return billing.DeductInput{
		TenantID:       tenantID,
		ProductCode:    contentBillingProductCode,
		Items:          []billing.DeductItem{{Unit: contentBillingUnitPrompt, Amount: 1}},
		IdempotencyKey: fmt.Sprintf("content:ai-visibility:%d:round", runID),
		Reason:         contentBillingReasonAIVisibility,
	}
}

// articleDeductionInput 组装文稿交付的扣量请求：record 1 份，
// 幂等键 content:article:<articleID>:deliver（同一篇文稿只扣一次）。
func articleDeductionInput(tenantID, articleID int64) (billing.DeductInput, bool) {
	if articleID <= 0 {
		return billing.DeductInput{}, false
	}
	return billing.DeductInput{
		TenantID:       tenantID,
		ProductCode:    contentBillingProductCode,
		Items:          []billing.DeductItem{{Unit: contentBillingUnitRecord, Amount: 1}},
		IdempotencyKey: fmt.Sprintf("content:article:%d:deliver", articleID),
		Reason:         contentBillingReasonArticle,
	}, true
}

// singleImageDeductionInput 组装"一份内容只交付一张图"的扣量请求：shot 1，
// 幂等键 content:image:<contentID>:deliver。
func singleImageDeductionInput(tenantID, contentID int64) (billing.DeductInput, bool) {
	if contentID <= 0 {
		return billing.DeductInput{}, false
	}
	return billing.DeductInput{
		TenantID:       tenantID,
		ProductCode:    contentBillingProductCode,
		Items:          []billing.DeductItem{{Unit: contentBillingUnitShot, Amount: 1}},
		IdempotencyKey: fmt.Sprintf("content:image:%d:deliver", contentID),
		Reason:         contentBillingReasonImage,
	}, true
}

// imageSlotDeductionInput 组装多图交付的扣量请求：shot 1，
// 幂等键 content:image:<contentID>-<slot>:deliver（图位是图片在本内容里的稳定下标，
// 同一图位重复生成/重试不会重复扣量）。
func imageSlotDeductionInput(tenantID, contentID int64, slot int) (billing.DeductInput, bool) {
	if contentID <= 0 || slot < 0 {
		return billing.DeductInput{}, false
	}
	return billing.DeductInput{
		TenantID:       tenantID,
		ProductCode:    contentBillingProductCode,
		Items:          []billing.DeductItem{{Unit: contentBillingUnitShot, Amount: 1}},
		IdempotencyKey: fmt.Sprintf("content:image:%d-%d:deliver", contentID, slot),
		Reason:         contentBillingReasonImage,
	}, true
}

// chargeInsight 在需求洞察成功落库后扣 hour。worker 异步交付不设预检闸门；
// 时长未知时跳过扣量，扣量失败只告警。
func (s *Service) chargeInsight(ctx context.Context, tenantID, recordingID int64) {
	if s == nil || s.billing == nil {
		return
	}
	seconds, err := s.recordingSeconds(ctx, tenantID, recordingID)
	if err != nil {
		slog.Warn("content insight deduction skipped: duration unavailable",
			"tenant_id", tenantID, "recording_id", recordingID, "error", err)
		return
	}
	input, ok := insightDeductionInput(tenantID, recordingID, seconds)
	if !ok {
		return
	}
	deductOrWarn(ctx, s.billing, input)
}

// chargeAIVisibility 在 AI 回答诊断完成落库后扣 prompt 1 题次。
func (s *Service) chargeAIVisibility(ctx context.Context, tenantID, runID int64) {
	if s == nil || s.billing == nil || runID <= 0 {
		return
	}
	deductOrWarn(ctx, s.billing, aiVisibilityDeductionInput(tenantID, runID))
}

// chargeArticle 在文稿保存成功（content_items 落库）后扣 record 1 份。
func (s *Service) chargeArticle(ctx context.Context, tenantID, articleID int64) {
	if s == nil || s.billing == nil {
		return
	}
	input, ok := articleDeductionInput(tenantID, articleID)
	if !ok {
		return
	}
	deductOrWarn(ctx, s.billing, input)
}

// chargeSingleImage 在单图生成并写入内容成功后扣 shot 1。
func (s *Service) chargeSingleImage(ctx context.Context, tenantID, contentID int64) {
	if s == nil || s.billing == nil {
		return
	}
	input, ok := singleImageDeductionInput(tenantID, contentID)
	if !ok {
		return
	}
	deductOrWarn(ctx, s.billing, input)
}

// chargeImageSlot 在多图/分镜的某个图位生成并写入内容成功后扣 shot 1。
func (s *Service) chargeImageSlot(ctx context.Context, tenantID, contentID int64, slot int) {
	if s == nil || s.billing == nil {
		return
	}
	input, ok := imageSlotDeductionInput(tenantID, contentID, slot)
	if !ok {
		return
	}
	deductOrWarn(ctx, s.billing, input)
}

// deductOrWarn 调用计费扣量：ErrAlreadyDeducted 视为幂等成功，其余错误只告警。
func deductOrWarn(ctx context.Context, svc billingService, input billing.DeductInput) {
	if svc == nil {
		return
	}
	if _, err := svc.Deduct(ctx, input); err != nil && !errors.Is(err, billing.ErrAlreadyDeducted) {
		slog.Warn("content deduction failed, business result kept",
			"tenant_id", input.TenantID,
			"product_code", input.ProductCode,
			"idempotency_key", input.IdempotencyKey,
			"error", err)
	}
}

// writeContentQuotaExhausted 返回额度不足：402 Payment Required +
// CONTENT_QUOTA_EXHAUSTED，details 带产品与缺口明细。
func writeContentQuotaExhausted(w http.ResponseWriter, cause error) {
	details := map[string]any{"product_code": contentBillingProductCode}
	var insufficient *billing.InsufficientEntitlementError
	if errors.As(cause, &insufficient) {
		details["unit"] = insufficient.Unit
		details["current"] = insufficient.Current
		details["required"] = insufficient.Required
		details["shortfall"] = insufficient.Shortfall
	}
	slog.Warn("content generation rejected: quota exhausted", "error", cause)
	httputil.WriteError(w, http.StatusPaymentRequired, contentQuotaExhaustedCode, ErrContentQuotaExhausted.Error(), details)
}
