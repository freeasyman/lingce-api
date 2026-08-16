package wecom

import "testing"

func TestHasValidBaseLicense(t *testing.T) {
	const now = int64(2_000)
	tests := []struct {
		name string
		info []LicenseActiveInfo
		want bool
	}{
		{name: "empty", want: false},
		{
			name: "valid base license",
			info: []LicenseActiveInfo{{Type: LicenseAccountTypeBase, ExpireTime: now + 1}},
			want: true,
		},
		{
			name: "expired base license",
			info: []LicenseActiveInfo{{Type: LicenseAccountTypeBase, ExpireTime: now}},
			want: false,
		},
		{
			name: "valid non-base license",
			info: []LicenseActiveInfo{{Type: 2, ExpireTime: now + 1}},
			want: false,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := hasValidBaseLicense(tt.info, now); got != tt.want {
				t.Fatalf("hasValidBaseLicense() = %v, want %v", got, tt.want)
			}
		})
	}
}
