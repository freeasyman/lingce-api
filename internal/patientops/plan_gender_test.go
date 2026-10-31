package patientops

import "testing"

func TestPlanGenderHint(t *testing.T) {
	cases := []struct {
		name     string
		customer string
		product  string
		want     string
	}{
		{"unknown female product", "", "female", "产品限女性，客户性别未知，请确认"},
		{"unknown male product", "", "male", "产品限男性，客户性别未知，请确认"},
		{"known female match", "女", "female", ""},
		{"known female match english", "female", "female", ""},
		{"known male match", "male", "male", ""},
		{"product gender all", "", "all", ""},
		{"product gender empty", "", "", ""},
		{"customer male product female", "male", "female", ""},
	}
	for _, c := range cases {
		if got := planGenderHint(c.customer, c.product); got != c.want {
			t.Errorf("%s: got %q want %q", c.name, got, c.want)
		}
	}
}
