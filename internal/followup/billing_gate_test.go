package followup

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/freeasyman/lingce-api/internal/billing"
)

// 服务暂停/到期必须阻断随访生成，并把客户口径文案写进 run。
func TestEnsureFollowupQuotaBlocksPausedAndExpired(t *testing.T) {
	cases := []struct {
		name    string
		err     error
		want    error
		message string
	}{
		{name: "paused", err: billing.ErrSubscriptionPaused, want: billing.ErrSubscriptionPaused, message: billing.MessageSubscriptionPaused},
		{name: "expired", err: billing.ErrSubscriptionExpired, want: billing.ErrSubscriptionExpired, message: billing.MessageSubscriptionExpired},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			fake := newFakeFollowupBilling()
			fake.quotaErr = tc.err
			svc := newFollowupTestService(fake, 3600, nil)

			err := svc.ensureFollowupQuota(context.Background(), 7, 42)
			if !errors.Is(err, tc.want) {
				t.Fatalf("ensureFollowupQuota = %v, want %v", err, tc.want)
			}
			if err.Error() != tc.message {
				t.Fatalf("message = %q, want %q", err.Error(), tc.message)
			}
			if strings.Contains(err.Error(), "运营") {
				t.Fatalf("客户口径出现内部称呼: %q", err.Error())
			}
			if fake.deductCount() != 0 {
				t.Fatalf("precheck must not deduct")
			}
		})
	}
}

// 额度不足的原生错误文案也统一成客户口径。
func TestFollowupQuotaExhaustedMessageIsCustomerFacing(t *testing.T) {
	if got := ErrFollowupQuotaExhausted.Error(); got != billing.MessageInsufficientEntitlement {
		t.Fatalf("message = %q, want %q", got, billing.MessageInsufficientEntitlement)
	}
	if strings.Contains(ErrFollowupQuotaExhausted.Error(), "运营") {
		t.Fatalf("客户口径出现内部称呼: %q", ErrFollowupQuotaExhausted.Error())
	}
}
