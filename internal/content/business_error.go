package content

// BizError 业务前置条件错误：可以直接展示给用户，并带上可操作的指引。
// 与系统故障区分开，避免把「还没配置账号」这类问题显示成「服务暂时不可用」。
type BizError struct {
	Code    string
	Message string
	// Status 期望的 HTTP 状态码（0 表示默认 400）
	Status int
}

func (e *BizError) Error() string { return e.Message }

// NewBizError 构造一个业务错误。
func NewBizError(code, message string) error { return &BizError{Code: code, Message: message} }
