package opportunityalert

import "context"

type NoopMessageSender struct{}

func NewNoopMessageSender() *NoopMessageSender {
	return &NoopMessageSender{}
}

func (s *NoopMessageSender) IsEnabled() bool {
	return false
}

func (s *NoopMessageSender) SendInternalMessage(ctx context.Context, req MessageSendRequest) error {
	return nil
}
