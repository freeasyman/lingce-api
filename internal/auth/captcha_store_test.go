package auth

import (
	"testing"
	"time"
)

func TestCaptchaStoreVerifyIsCaseInsensitive(t *testing.T) {
	store := NewCaptchaStore()
	defer store.Close()

	store.Save("captcha-1", "AbC1", time.Minute)

	if !store.Verify("captcha-1", "aBc1") {
		t.Fatalf("expected captcha comparison to ignore case")
	}
}

func TestCaptchaStoreVerifyIsOneTimeUse(t *testing.T) {
	store := NewCaptchaStore()
	defer store.Close()

	store.Save("captcha-2", "AbC1", time.Minute)

	if !store.Verify("captcha-2", "ABC1") {
		t.Fatalf("expected first verification to pass")
	}
	if store.Verify("captcha-2", "ABC1") {
		t.Fatalf("expected captcha to be one-time use")
	}
}
