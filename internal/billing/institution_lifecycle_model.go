package billing

// 计费运营端第 2 批的数据契约：暂停 / 恢复服务、作废订单、人工调额度。
//
// 这几个接口都挂在 /api/v1/ops/billing 下，请求体字段沿用前端习惯的 camelCase，
// 同时接受 snake_case 兼容脚本调用；校验失败统一返回 *ValidationError（handler → 400）。

import (
	"fmt"
	"strings"
)

// maxLifecycleReasonLength 是暂停/作废/调额度原因的长度上限（与赠送保持一致）。
const maxLifecycleReasonLength = 500

// SuspendRequest 是 POST /institutions/{id}/suspend 的请求体。
type SuspendRequest struct {
	Reason string `json:"reason"`
}

// normalize 校验暂停原因必填。
func (r SuspendRequest) normalize() (SuspendRequest, error) {
	r.Reason = strings.TrimSpace(r.Reason)
	if r.Reason == "" {
		return SuspendRequest{}, invalidf("请填暂停原因")
	}
	if len([]rune(r.Reason)) > maxLifecycleReasonLength {
		return SuspendRequest{}, invalidf("暂停原因过长")
	}
	return r, nil
}

// VoidOrderRequest 是 POST /orders/{orderId}/void 的请求体。
type VoidOrderRequest struct {
	Reason string `json:"reason"`
}

// normalize 校验作废原因必填。
func (r VoidOrderRequest) normalize() (VoidOrderRequest, error) {
	r.Reason = strings.TrimSpace(r.Reason)
	if r.Reason == "" {
		return VoidOrderRequest{}, invalidf("请填作废原因")
	}
	if len([]rune(r.Reason)) > maxLifecycleReasonLength {
		return VoidOrderRequest{}, invalidf("作废原因过长")
	}
	return r, nil
}

// EntitlementCorrectionRequest 是 POST /institutions/{id}/entitlement-corrections 的请求体。
//
// Amount 是带符号的调整量：正数加额度、负数减额度；单位与价目表一致（hour/record/...）。
type EntitlementCorrectionRequest struct {
	ProductCode string  `json:"productCode"`
	Unit        string  `json:"unit"`
	Amount      Decimal `json:"amount"`
	Reason      string  `json:"reason"`
	Approver    string  `json:"approver"`
	// IdempotencyKey 由前端每次提交生成；重试同一笔调整时复用，避免重复加减额度。
	IdempotencyKey string `json:"idempotencyKey"`

	SnakeProductCode    string `json:"product_code"`
	SnakeIdempotencyKey string `json:"idempotency_key"`
}

// normalize 合并产品代码别名并做基础校验（产品/单位/金额/原因）。
// 「减少不能把余额减成负数」与审批人阈值需要读库/价目表，在 service 层校验。
func (r EntitlementCorrectionRequest) normalize() (EntitlementCorrectionRequest, error) {
	if r.ProductCode == "" {
		r.ProductCode = r.SnakeProductCode
	}
	if r.IdempotencyKey == "" {
		r.IdempotencyKey = r.SnakeIdempotencyKey
	}
	r.IdempotencyKey = strings.TrimSpace(r.IdempotencyKey)
	r.ProductCode = normalizeEnum(r.ProductCode)
	r.Unit = normalizeEnum(r.Unit)
	r.Reason = strings.TrimSpace(r.Reason)
	r.Approver = strings.TrimSpace(r.Approver)

	if r.ProductCode == "" {
		return EntitlementCorrectionRequest{}, invalidf("请选择产品")
	}
	if r.Unit == "" {
		return EntitlementCorrectionRequest{}, invalidf("请选择单位")
	}
	if !IsValidUnit(r.Unit) {
		return EntitlementCorrectionRequest{}, invalidf("单位不合法")
	}
	amount, err := parseEntitlementCents("amount", r.Amount)
	if err != nil {
		return EntitlementCorrectionRequest{}, invalidf("调整数量 %s", err)
	}
	if amount == 0 {
		return EntitlementCorrectionRequest{}, invalidf("调整数量不能为 0")
	}
	if amount > maxQuotaAmountCents || amount < -maxQuotaAmountCents {
		return EntitlementCorrectionRequest{}, invalidf("调整数量超出范围")
	}
	// 统一成整数分文本，避免前端传 1e2 / 1.000 这类写法落库后口径不一致。
	r.Amount = Decimal(centsToDecimal(amount))
	if r.Reason == "" {
		return EntitlementCorrectionRequest{}, invalidf("请填调整原因")
	}
	if len(r.IdempotencyKey) > 128 {
		return EntitlementCorrectionRequest{}, invalidf("幂等键过长")
	}
	if len([]rune(r.Reason)) > maxLifecycleReasonLength {
		return EntitlementCorrectionRequest{}, invalidf("调整原因过长")
	}
	return r, nil
}

// correctionNeedsApproval 判断本次调整是否达到「1 个增量包」的审批阈值。
//
// 口径：按该产品该单位的增量包规格算 1 包；没有增量包规格时退回 1 个单位。
// 绝对值 >= 1 包需要审批人（任务口径）。返回包大小（分）与是否需要审批。
func correctionNeedsApproval(product catalogProduct, unit string, amountCents int64) (int64, bool) {
	packCents := int64(100) // 没有增量包规格时，1 个单位 = 1 包
	for _, inc := range product.Increments {
		if normalizeEnum(inc.Unit) != normalizeEnum(unit) {
			continue
		}
		if cents, err := parseAmountCents(inc.Amount); err == nil && cents > 0 {
			packCents = cents
		}
		break
	}
	abs := amountCents
	if abs < 0 {
		abs = -abs
	}
	return packCents, abs >= packCents
}

// correctionReasonWithApprover 生成写进流水的可读原因，审批人按任务要求带在 reason 里。
func correctionReasonWithApprover(reason, approver string) string {
	if strings.TrimSpace(approver) == "" {
		return "人工调额度：" + reason
	}
	return fmt.Sprintf("人工调额度：%s · 审批人 %s", reason, strings.TrimSpace(approver))
}
