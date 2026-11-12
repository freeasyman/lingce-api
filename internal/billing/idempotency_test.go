package billing

import (
	"context"
	"errors"
	"testing"
)

// IsDeducted 是交付入口判断"同一笔交付是否已付费"的只读查询：
// 命中幂等键返回 true，未知键返回 false，入参不合法返回 ValidationError。
func TestIsDeducted(t *testing.T) {
	repo := &fakeRepository{deductKeys: map[string]struct{}{"emr:record:rec-1:draft": {}}}
	service := NewService(repo)

	deducted, err := service.IsDeducted(context.Background(), 7, "emr:record:rec-1:draft")
	if err != nil {
		t.Fatalf("IsDeducted() error = %v", err)
	}
	if !deducted {
		t.Fatal("known idempotency key should be reported as deducted")
	}

	deducted, err = service.IsDeducted(context.Background(), 7, "emr:record:rec-2:draft")
	if err != nil {
		t.Fatalf("IsDeducted() unknown key error = %v", err)
	}
	if deducted {
		t.Fatal("unknown idempotency key must not be reported as deducted")
	}

	// 入参校验与扣量共用同一套幂等键规则，避免非法键查出"未扣过"后被当成可重复扣。
	for name, call := range map[string]func() (bool, error){
		"empty tenant": func() (bool, error) { return service.IsDeducted(context.Background(), 0, "emr:record:rec-1:draft") },
		"empty key":    func() (bool, error) { return service.IsDeducted(context.Background(), 7, " ") },
		"short key":    func() (bool, error) { return service.IsDeducted(context.Background(), 7, "not-a-key") },
	} {
		t.Run(name, func(t *testing.T) {
			if _, err := call(); err == nil {
				t.Fatal("expected validation error")
			} else {
				var invalid *ValidationError
				if !errors.As(err, &invalid) {
					t.Fatalf("error type = %T, want *ValidationError", err)
				}
			}
		})
	}
}

// 计费层自身故障必须如实返回错误，让交付入口决定回退到正常预检，不能吞成"已扣过"。
func TestIsDeductedPropagatesRepositoryError(t *testing.T) {
	repositoryErr := errors.New("billing db down")
	repo := &fakeRepository{isDeductedErr: repositoryErr}
	service := NewService(repo)

	deducted, err := service.IsDeducted(context.Background(), 7, "emr:record:rec-1:draft")
	if !errors.Is(err, repositoryErr) {
		t.Fatalf("IsDeducted() error = %v, want %v", err, repositoryErr)
	}
	if deducted {
		t.Fatal("repository failure must not be treated as deducted")
	}
}
