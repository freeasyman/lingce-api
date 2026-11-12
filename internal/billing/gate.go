package billing

// 客户侧交付闸门的统一拒绝口径。
//
// 六产品的所有交付入口都可能在调模型/落库之前先过 CheckQuota（或内部扣量接口），
// 计费层会把「服务暂停 / 服务到期 / 额度不足」三种情况返回给产品端。历史实现里
// 各产品自己定义错误码与文案，既有「额度已用完，请及时联系客服，加购增量包」这种
// 不一致的写法，也有把暂停/到期当成计费故障 fail-open 的漏洞。
//
// 这里把三种拒绝收敛成唯一的 (HTTP 状态码, 错误码, 客户文案)：
//   - 服务到期：SUBSCRIPTION_EXPIRED + 「服务已到期，请联系您的专属顾问」
//   - 服务暂停：SUBSCRIPTION_PAUSED  + 「服务已暂停，请联系您的专属顾问」
//   - 额度不足：INSUFFICIENT_ENTITLEMENT + 「当前额度已用完，请联系客服加购增量包」
//
// 运营端自己的错误提示（例如「该机构服务已暂停，请联系运营恢复」）不走这里，
// 仍保留在 institution_handler 的内部口径中。

import (
	"errors"
	"net/http"
)

const (
	// CodeSubscriptionExpired 表示服务期已到期，新的交付不再执行。
	CodeSubscriptionExpired = "SUBSCRIPTION_EXPIRED"
	// CodeSubscriptionPaused 表示机构服务被暂停，新的交付不再执行。
	CodeSubscriptionPaused = "SUBSCRIPTION_PAUSED"
	// CodeInsufficientEntitlement 表示额度不足以完成本次交付。
	CodeInsufficientEntitlement = "INSUFFICIENT_ENTITLEMENT"

	// MessageSubscriptionExpired 是客户侧到期文案，不能出现「运营」等内部称呼。
	MessageSubscriptionExpired = "服务已到期，请联系您的专属顾问"
	// MessageSubscriptionPaused 是客户侧暂停文案。
	MessageSubscriptionPaused = "服务已暂停，请联系您的专属顾问"
	// MessageInsufficientEntitlement 是客户侧额度不足文案。
	MessageInsufficientEntitlement = "当前额度已用完，请联系客服加购增量包"
)

// CustomerGate 是一次客户侧交付拒绝的完整口径。
type CustomerGate struct {
	Status  int
	Code    string
	Message string
	// Insufficient 只在额度不足时非空，便于调用方透传 unit/current/required/shortfall。
	Insufficient *InsufficientEntitlementError
}

// GateRejection 把交付闸门错误翻译成客户侧口径。
//
// 只识别三种情况；计费自身故障（连不上库、参数错误等）返回 ok=false，调用方
// 仍按各自既有策略 fail-open，不能因为计费故障拖垮业务。
func GateRejection(err error) (*CustomerGate, bool) {
	if err == nil {
		return nil, false
	}
	switch {
	case errors.Is(err, ErrSubscriptionExpired):
		return &CustomerGate{
			Status:  http.StatusConflict,
			Code:    CodeSubscriptionExpired,
			Message: MessageSubscriptionExpired,
		}, true
	case errors.Is(err, ErrSubscriptionPaused):
		return &CustomerGate{
			Status:  http.StatusConflict,
			Code:    CodeSubscriptionPaused,
			Message: MessageSubscriptionPaused,
		}, true
	case errors.Is(err, ErrInsufficientEntitlement):
		gate := &CustomerGate{
			Status:  http.StatusPaymentRequired,
			Code:    CodeInsufficientEntitlement,
			Message: MessageInsufficientEntitlement,
		}
		var insufficient *InsufficientEntitlementError
		if errors.As(err, &insufficient) {
			gate.Insufficient = insufficient
		}
		return gate, true
	default:
		return nil, false
	}
}

// IsGateRejection 判断错误是否属于三种客户侧交付拒绝之一。
func IsGateRejection(err error) bool {
	_, ok := GateRejection(err)
	return ok
}
