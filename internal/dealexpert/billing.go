package dealexpert

// 赢客专家（产品编码 yingke）的计费接入。
//
// 唯一交付点：一次接待记录分析成功落库（consultation_analyses.status='analyzed'）
// 之后，按录音时长扣 hour（recordings.duration 秒 / 3600，保留 2 位小数）；
// 时长未知或为 0 时跳过整笔。
//
// 该产品由 worker 异步触发、没有用户点击入口，因此在调用模型之前做一次只读
// CheckQuota 软闸门：额度不足时不调模型、不扣量，把分析记录置为 failed 并写明
// 原因；计费未接入或预检报非 ErrInsufficientEntitlement 错误时放行（fail-open）。
//
// 扣量一律发生在业务成功落库之后：ErrAlreadyDeducted 视为幂等成功，其他错误
// 只告警、不回滚已经落库的分析结果（MVP 允许欠扣）。

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"math"

	"github.com/freeasyman/lingce-api/internal/billing"
	"github.com/freeasyman/lingce-api/pkg/llmgateway"
)

const (
	// dealExpertBillingProductCode 是赢客专家的产品编码。
	dealExpertBillingProductCode = "yingke"
	// dealExpertBillingUnitHour 是按录音时长计量的单位。
	dealExpertBillingUnitHour = "hour"
	// dealExpertBillingReason 是写进额度流水的原因，便于运营对账。
	dealExpertBillingReason = "赢客专家-成交分析"
)

// ErrDealExpertQuotaExhausted 表示预检发现赢客专家额度不足，异步分析据此终止本次 run。
var ErrDealExpertQuotaExhausted = errors.New("额度已用完，请及时联系客服，加购增量包")

// dealExpertQuotaExhaustedError 是软闸门拒绝的具体错误，携带缺口明细；同时满足
// errors.Is(err, ErrDealExpertQuotaExhausted) 与 errors.As(err, *billing.InsufficientEntitlementError)，
// 便于调用方区分"额度不足"与计费自身故障。
type dealExpertQuotaExhaustedError struct {
	insufficient *billing.InsufficientEntitlementError
}

func (e *dealExpertQuotaExhaustedError) Error() string {
	if e == nil || e.insufficient == nil {
		return ErrDealExpertQuotaExhausted.Error()
	}
	return fmt.Sprintf("%s（%s 额度不足：当前 %.2f，本次需要 %.2f）",
		ErrDealExpertQuotaExhausted.Error(), e.insufficient.Unit, e.insufficient.Current, e.insufficient.Required)
}

func (e *dealExpertQuotaExhaustedError) Unwrap() []error {
	if e == nil || e.insufficient == nil {
		return []error{ErrDealExpertQuotaExhausted}
	}
	return []error{ErrDealExpertQuotaExhausted, e.insufficient}
}

// billingService 是本模块依赖的最小计费能力，*billing.Service 天然实现；
// 单元测试注入 fake，避免依赖真实数据库与计费事务。
type billingService interface {
	Deduct(ctx context.Context, input billing.DeductInput) (*billing.DeductResult, error)
	CheckQuota(ctx context.Context, tenantID int64, productCode string, items []billing.DeductItem) error
}

// UseBilling 给赢客专家的接待记录分析生成器接入计费。赢客专家没有独立 Module，
// ConsultationAnalysisGenerator 同时持有"调模型前预检"与"落库后扣量"两个点，
// 因此由它直接持有计费能力。nil 表示不接计费（旧部署/测试）。
func (g *ConsultationAnalysisGenerator) UseBilling(svc *billing.Service) {
	if g == nil {
		return
	}
	g.billing = wrapBillingService(svc)
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
// 调用方按"时长未知"处理：跳过本次 hour 预检/扣量。
func (g *ConsultationAnalysisGenerator) recordingSeconds(ctx context.Context, tenantID, recordingID int64) (int64, error) {
	if g.lookupRecordingDuration != nil {
		return g.lookupRecordingDuration(ctx, tenantID, recordingID)
	}
	if g.pool == nil {
		return 0, fmt.Errorf("database pool is not configured")
	}
	var seconds int64
	if err := g.pool.QueryRow(ctx, `
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

// analysisDeductionInput 组装成交分析的扣量请求：hour = 录音时长（保留 2 位），
// 幂等键固定为 deal:recording:<recordingID>:analysis。
// 时长未知（<= 0）或四舍五入后为 0 时返回 ok=false，调用方跳过整笔扣量。
func analysisDeductionInput(tenantID, recordingID, seconds int64) (billing.DeductInput, bool) {
	hours := recordingHours(seconds)
	if recordingID <= 0 || hours <= 0 {
		return billing.DeductInput{}, false
	}
	return billing.DeductInput{
		TenantID:       tenantID,
		ProductCode:    dealExpertBillingProductCode,
		Items:          []billing.DeductItem{{Unit: dealExpertBillingUnitHour, Amount: hours}},
		IdempotencyKey: fmt.Sprintf("deal:recording:%d:analysis", recordingID),
		Reason:         dealExpertBillingReason,
	}, true
}

// ensureAnalysisQuota 是异步分析调模型前的只读软闸门：hour = 本次录音时长。
//
// 额度不足返回 ErrDealExpertQuotaExhausted（调用方不调模型、不扣量，并把分析记录
// 置为 failed）；时长未知时无法计量，放行；计费未接入或预检本身报错时放行并告警
// （fail-open），不能因计费故障拖垮分析。
func (g *ConsultationAnalysisGenerator) ensureAnalysisQuota(ctx context.Context, tenantID, recordingID int64) error {
	if g == nil || g.billing == nil {
		return nil
	}
	seconds, err := g.recordingSeconds(ctx, tenantID, recordingID)
	if err != nil {
		slog.Warn("deal expert quota precheck skipped: recording duration unavailable",
			"tenant_id", tenantID, "recording_id", recordingID, "error", err)
		return nil
	}
	hours := recordingHours(seconds)
	if hours <= 0 {
		slog.Warn("deal expert quota precheck skipped: recording duration unknown",
			"tenant_id", tenantID, "recording_id", recordingID, "seconds", seconds)
		return nil
	}
	err = g.billing.CheckQuota(ctx, tenantID, dealExpertBillingProductCode,
		[]billing.DeductItem{{Unit: dealExpertBillingUnitHour, Amount: hours}})
	if err == nil {
		return nil
	}
	if !errors.Is(err, billing.ErrInsufficientEntitlement) {
		slog.Warn("deal expert quota precheck failed, allowing analysis",
			"tenant_id", tenantID, "recording_id", recordingID, "error", err)
		return nil
	}
	var insufficient *billing.InsufficientEntitlementError
	if errors.As(err, &insufficient) {
		return &dealExpertQuotaExhaustedError{insufficient: insufficient}
	}
	return ErrDealExpertQuotaExhausted
}

// callLLMWithQuotaGate 在调用模型之前先过额度软闸门：额度不足时直接返回错误，
// call 不会被调用，因此不会产生模型成本，也不会进入扣量路径。
// 抽象出 call 既是生产上的最小改动，也让单测能直接验证"闸门拒绝时不调模型"。
func (g *ConsultationAnalysisGenerator) callLLMWithQuotaGate(
	ctx context.Context,
	tenantID, recordingID int64,
	call func() (*llmgateway.TextInferenceResponse, error),
) (*llmgateway.TextInferenceResponse, error) {
	if err := g.ensureAnalysisQuota(ctx, tenantID, recordingID); err != nil {
		slog.Warn("deal expert analysis rejected: quota exhausted",
			"tenant_id", tenantID, "recording_id", recordingID, "error", err)
		return nil, err
	}
	if call == nil {
		return nil, fmt.Errorf("llm call is not configured")
	}
	return call()
}

// chargeAnalysis 在成交分析成功落库（status='analyzed'）后扣 hour。
//
// 时长未知时跳过扣量（无法计量小时数）；扣量失败只告警，不回滚已经落库的分析结果。
func (g *ConsultationAnalysisGenerator) chargeAnalysis(ctx context.Context, tenantID, recordingID int64) {
	if g == nil || g.billing == nil {
		return
	}
	seconds, err := g.recordingSeconds(ctx, tenantID, recordingID)
	if err != nil {
		slog.Warn("deal expert deduction skipped: recording duration unavailable",
			"tenant_id", tenantID, "recording_id", recordingID, "error", err)
		return
	}
	input, ok := analysisDeductionInput(tenantID, recordingID, seconds)
	if !ok {
		return
	}
	deductOrWarn(ctx, g.billing, input)
}

// deductOrWarn 调用计费扣量：ErrAlreadyDeducted 视为幂等成功，其余错误只告警。
func deductOrWarn(ctx context.Context, svc billingService, input billing.DeductInput) {
	if svc == nil {
		return
	}
	if _, err := svc.Deduct(ctx, input); err != nil && !errors.Is(err, billing.ErrAlreadyDeducted) {
		slog.Warn("deal expert deduction failed, business result kept",
			"tenant_id", input.TenantID,
			"product_code", input.ProductCode,
			"idempotency_key", input.IdempotencyKey,
			"error", err)
	}
}
