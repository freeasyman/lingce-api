package followup

// 随访顾问（产品编码 suifang）的计费接入。
//
// 唯一交付点：processRun 中随访任务成功持久化（saveGeneratedTasks 写入
// recording_tasks）且至少生成 1 条任务之后，按录音时长扣 hour
// （recordings.duration 秒 / 3600，保留 2 位小数）；时长未知或为 0 时跳过整笔。
// 模型返回空任务列表属于空交付，不扣量，并由 settleGeneratedTasks 把 run
// 收敛为 failed（EMPTY_DELIVERY）。
//
// 该产品由 worker 异步触发、没有用户点击入口，因此在调用模型之前做一次只读
// CheckQuota 软闸门：额度不足时不调模型、不扣量，把本次 run 标记为 failed
// （last_error_code=QUOTA_EXHAUSTED）并写明原因；计费未接入或预检报非
// ErrInsufficientEntitlement 错误时放行（fail-open）。
//
// 扣量一律发生在业务成功落库之后：ErrAlreadyDeducted 视为幂等成功，其他错误
// 只告警、不回滚已经落库的随访任务（MVP 允许欠扣）。

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
	// followupBillingProductCode 是随访顾问的产品编码。
	followupBillingProductCode = "suifang"
	// followupBillingUnitHour 是按录音时长计量的单位。
	followupBillingUnitHour = "hour"
	// followupBillingReason 是写进额度流水的原因，便于运营对账。
	followupBillingReason = "随访顾问-随访分析"
	// followupQuotaExhaustedCode 是软闸门拒绝时写进 run 的失败码。
	followupQuotaExhaustedCode = "QUOTA_EXHAUSTED"
	// followupSubscriptionPausedCode / followupSubscriptionExpiredCode 是服务闸门
	// 拒绝时写进 run 的失败码，与客户侧错误码保持一致。
	followupSubscriptionPausedCode  = billing.CodeSubscriptionPaused
	followupSubscriptionExpiredCode = billing.CodeSubscriptionExpired
)

// ErrFollowupQuotaExhausted 表示预检发现随访顾问额度不足，异步 run 据此终止。
var ErrFollowupQuotaExhausted = errors.New(billing.MessageInsufficientEntitlement)

// followupGateError 让服务暂停/到期的客户文案原样写进 run，同时保留对计费层
// 错误链的 errors.Is/As 能力，便于调用方区分具体拒绝原因。
type followupGateError struct {
	message string
	cause   error
}

func (e *followupGateError) Error() string { return e.message }
func (e *followupGateError) Unwrap() error { return e.cause }

// followupQuotaExhaustedError 是软闸门拒绝的具体错误，携带缺口明细；同时满足
// errors.Is(err, ErrFollowupQuotaExhausted) 与 errors.As(err, *billing.InsufficientEntitlementError)，
// 便于调用方区分"额度不足"与计费自身故障。
type followupQuotaExhaustedError struct {
	insufficient *billing.InsufficientEntitlementError
}

func (e *followupQuotaExhaustedError) Error() string {
	if e == nil || e.insufficient == nil {
		return ErrFollowupQuotaExhausted.Error()
	}
	return fmt.Sprintf("%s（%s 额度不足：当前 %.2f，本次需要 %.2f）",
		ErrFollowupQuotaExhausted.Error(), e.insufficient.Unit, e.insufficient.Current, e.insufficient.Required)
}

func (e *followupQuotaExhaustedError) Unwrap() []error {
	if e == nil || e.insufficient == nil {
		return []error{ErrFollowupQuotaExhausted}
	}
	return []error{ErrFollowupQuotaExhausted, e.insufficient}
}

// billingService 是本模块依赖的最小计费能力，*billing.Service 天然实现；
// 单元测试注入 fake，避免依赖真实数据库与计费事务。
type billingService interface {
	Deduct(ctx context.Context, input billing.DeductInput) (*billing.DeductResult, error)
	CheckQuota(ctx context.Context, tenantID int64, productCode string, items []billing.DeductItem) error
}

// UseBilling 给随访顾问模块接入计费：下游统一由 Service 持有计费能力。
// nil 表示不接计费（旧部署/测试）。
func (m *Module) UseBilling(svc *billing.Service) {
	if m == nil || m.handler == nil || m.handler.service == nil {
		return
	}
	m.handler.service.UseBilling(svc)
}

// UseBilling 把 *billing.Service 收敛成窄接口注入 Service。
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
// 调用方按"时长未知"处理：跳过本次 hour 预检/扣量。
func (s *Service) recordingSeconds(ctx context.Context, tenantID, recordingID int64) (int64, error) {
	if s.lookupRecordingDuration != nil {
		return s.lookupRecordingDuration(ctx, tenantID, recordingID)
	}
	if s.pool == nil {
		return 0, fmt.Errorf("database pool is not configured")
	}
	var seconds int64
	if err := s.pool.QueryRow(ctx, `
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

// analysisDeductionInput 组装随访分析的扣量请求：hour = 录音时长（保留 2 位），
// 幂等键固定为 followup:recording:<recordingID>:analysis。
// 时长未知（<= 0）或四舍五入后为 0 时返回 ok=false，调用方跳过整笔扣量。
func analysisDeductionInput(tenantID, recordingID, seconds int64) (billing.DeductInput, bool) {
	hours := recordingHours(seconds)
	if recordingID <= 0 || hours <= 0 {
		return billing.DeductInput{}, false
	}
	return billing.DeductInput{
		TenantID:       tenantID,
		ProductCode:    followupBillingProductCode,
		Items:          []billing.DeductItem{{Unit: followupBillingUnitHour, Amount: hours}},
		IdempotencyKey: fmt.Sprintf("followup:recording:%d:analysis", recordingID),
		Reason:         followupBillingReason,
	}, true
}

// ensureFollowupQuota 是异步 run 调模型前的只读软闸门：hour = 本次录音时长。
//
// 额度不足返回 ErrFollowupQuotaExhausted（调用方不调模型、不扣量，并把 run 置为
// failed）；时长未知时无法计量，放行；计费未接入或预检本身报错时放行并告警
// （fail-open），不能因计费故障拖垮随访生成。
func (s *Service) ensureFollowupQuota(ctx context.Context, tenantID, recordingID int64) error {
	if s == nil || s.billing == nil {
		return nil
	}
	seconds, err := s.recordingSeconds(ctx, tenantID, recordingID)
	if err != nil {
		slog.Warn("followup quota precheck skipped: recording duration unavailable",
			"tenant_id", tenantID, "recording_id", recordingID, "error", err)
		return nil
	}
	hours := recordingHours(seconds)
	if hours <= 0 {
		slog.Warn("followup quota precheck skipped: recording duration unknown",
			"tenant_id", tenantID, "recording_id", recordingID, "seconds", seconds)
		return nil
	}
	err = s.billing.CheckQuota(ctx, tenantID, followupBillingProductCode,
		[]billing.DeductItem{{Unit: followupBillingUnitHour, Amount: hours}})
	if err == nil {
		return nil
	}
	if _, ok := billing.GateRejection(err); !ok {
		// 计费自身故障仍 fail-open，不能因计费故障拖垮随访生成。
		slog.Warn("followup quota precheck failed, allowing generation",
			"tenant_id", tenantID, "recording_id", recordingID, "error", err)
		return nil
	}
	switch {
	case errors.Is(err, billing.ErrSubscriptionPaused):
		return &followupGateError{message: billing.MessageSubscriptionPaused, cause: err}
	case errors.Is(err, billing.ErrSubscriptionExpired):
		return &followupGateError{message: billing.MessageSubscriptionExpired, cause: err}
	}
	var insufficient *billing.InsufficientEntitlementError
	if errors.As(err, &insufficient) {
		return &followupQuotaExhaustedError{insufficient: insufficient}
	}
	return ErrFollowupQuotaExhausted
}

// callLLMWithQuotaGate 在调用模型之前先过额度软闸门：额度不足时直接返回错误，
// call 不会被调用，因此不会产生模型成本，也不会进入扣量路径。
// 抽象出 call 既是生产上的最小改动，也让单测能直接验证"闸门拒绝时不调模型"。
func (s *Service) callLLMWithQuotaGate(
	ctx context.Context,
	req GenerateRequest,
	call func() (*llmgateway.TextInferenceResponse, error),
) (*llmgateway.TextInferenceResponse, error) {
	if err := s.ensureFollowupQuota(ctx, req.TenantID, req.RecordingID); err != nil {
		slog.Warn("followup analysis rejected: quota exhausted",
			"tenant_id", req.TenantID, "recording_id", req.RecordingID, "error", err)
		return nil, err
	}
	if call == nil {
		return nil, fmt.Errorf("llm call is not configured")
	}
	return call()
}

// chargeFollowupAnalysis 在随访任务成功持久化后扣 hour。
//
// 调用方必须先确认本次至少生成 1 条任务（见 settleGeneratedTasks）：空交付
// 不应进入本函数。时长未知时跳过扣量（无法计量小时数）；扣量失败只告警，
// 不回滚已经落库的任务。
func (s *Service) chargeFollowupAnalysis(ctx context.Context, tenantID, recordingID int64) {
	if s == nil || s.billing == nil {
		return
	}
	seconds, err := s.recordingSeconds(ctx, tenantID, recordingID)
	if err != nil {
		slog.Warn("followup deduction skipped: recording duration unavailable",
			"tenant_id", tenantID, "recording_id", recordingID, "error", err)
		return
	}
	input, ok := analysisDeductionInput(tenantID, recordingID, seconds)
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
		slog.Warn("followup deduction failed, business result kept",
			"tenant_id", input.TenantID,
			"product_code", input.ProductCode,
			"idempotency_key", input.IdempotencyKey,
			"error", err)
	}
}
