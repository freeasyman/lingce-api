package product

import "testing"

func TestAgeRequiredOldAndNewClients(t *testing.T) {
	min := 3
	optional := false
	for _, tc := range []struct {
		req      CreateRequest
		required bool
	}{
		{CreateRequest{}, false},
		{CreateRequest{AgeMin: &min}, true},
		{CreateRequest{AgeMin: &min, AgeRequired: &optional}, false},
	} {
		if got := normalizeCreate(tc.req); got.AgeRequired == nil || *got.AgeRequired != tc.required {
			t.Fatalf("incorrect age default: %+v", got)
		}
	}
}
