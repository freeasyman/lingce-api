package badge

import "testing"

func TestParseRecipients(t *testing.T) {
	tests := []struct {
		name string
		raw  string
		want []string
	}{
		{
			name: "single recipient",
			raw:  "zhanglu@lingce.pro",
			want: []string{"zhanglu@lingce.pro"},
		},
		{
			name: "comma and semicolon separators",
			raw:  "zhanglu@lingce.pro, yiliang@lingce.pro;ops@lingce.pro",
			want: []string{"zhanglu@lingce.pro", "yiliang@lingce.pro", "ops@lingce.pro"},
		},
		{
			name: "deduplicate case-insensitive",
			raw:  "A@lingce.pro, a@lingce.pro, b@lingce.pro",
			want: []string{"A@lingce.pro", "b@lingce.pro"},
		},
		{
			name: "ignore empty entries",
			raw:  ", , ; ;z@lingce.pro;",
			want: []string{"z@lingce.pro"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := parseRecipients(tt.raw)
			if len(got) != len(tt.want) {
				t.Fatalf("unexpected length: got=%d want=%d (%v)", len(got), len(tt.want), got)
			}
			for i := range tt.want {
				if got[i] != tt.want[i] {
					t.Fatalf("unexpected recipient at %d: got=%q want=%q", i, got[i], tt.want[i])
				}
			}
		})
	}
}
