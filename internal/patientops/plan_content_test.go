package patientops

import "testing"

func TestPlanSalutationFor(t *testing.T) {
	cases := []struct {
		name     string
		nickname string
		gender   string
		age      int
		want     string
	}{
		{"孙晟瑞", "", "", 12, "孙晟瑞家长"},
		{"小红薯687DEA97", "小红薯687DEA97", "", 12, "家长"},
		{"张张", "张张", "", 8, "家长"},
		{"王大力", "", "male", 32, "王先生"},
		{"李静", "", "female", 28, "李女士"},
		{"王大力", "", "", 32, "您好"},
		{"ROR", "", "", 0, "您好"},
	}
	for _, item := range cases {
		if got := planSalutationFor(item.name, item.nickname, item.gender, item.age); got != item.want {
			t.Fatalf("planSalutationFor(%q,%q,%q,%d)=%q, want %q", item.name, item.nickname, item.gender, item.age, got, item.want)
		}
	}
}

func TestEnsurePlanSalutation(t *testing.T) {
	if got := ensurePlanSalutation("您好，想了解孩子的情况", "徐楚琳家长"); got != "徐楚琳家长，您好，想了解孩子的情况" {
		t.Fatalf("unexpected salutation prefix: %q", got)
	}
	if got := ensurePlanSalutation("徐楚琳家长您好，想了解孩子的情况", "徐楚琳家长"); got != "徐楚琳家长您好，想了解孩子的情况" {
		t.Fatalf("should not duplicate salutation: %q", got)
	}
}
