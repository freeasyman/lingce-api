package badge

import (
	"testing"
	"time"
)

func TestDetermineHealthStatus(t *testing.T) {
	b25 := 25
	b15 := 15
	b5 := 5
	d6h := 6 * time.Hour
	d18h := 18 * time.Hour
	d30h := 30 * time.Hour

	cases := []struct {
		name      string
		online    bool
		battery   *int
		recording bool
		offline   *time.Duration
		exists    bool
		want      string
	}{
		{
			name:      "healthy baseline",
			online:    true,
			battery:   &b25,
			recording: true,
			offline:   &d6h,
			exists:    true,
			want:      "healthy",
		},
		{
			name:      "warning low battery",
			online:    true,
			battery:   &b15,
			recording: true,
			offline:   &d6h,
			exists:    true,
			want:      "warning",
		},
		{
			name:      "error critical battery",
			online:    true,
			battery:   &b5,
			recording: true,
			offline:   &d6h,
			exists:    true,
			want:      "error",
		},
		{
			name:      "warning offline 12 to 24h",
			online:    false,
			battery:   &b25,
			recording: true,
			offline:   &d18h,
			exists:    true,
			want:      "warning",
		},
		{
			name:      "error offline over 24h",
			online:    false,
			battery:   &b25,
			recording: true,
			offline:   &d30h,
			exists:    true,
			want:      "error",
		},
		{
			name:      "error recording failure",
			online:    true,
			battery:   &b25,
			recording: false,
			offline:   &d6h,
			exists:    true,
			want:      "error",
		},
		{
			name:      "error not exists",
			online:    true,
			battery:   &b25,
			recording: true,
			offline:   &d6h,
			exists:    false,
			want:      "error",
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := determineHealthStatus(tc.online, tc.battery, tc.recording, tc.offline, tc.exists)
			if got != tc.want {
				t.Fatalf("determineHealthStatus()=%s, want=%s", got, tc.want)
			}
		})
	}
}
