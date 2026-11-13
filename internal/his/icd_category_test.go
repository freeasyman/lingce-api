package his

import "testing"

func TestICD10Category(t *testing.T) {
	cases := map[string]string{
		"N72.X00": "泌尿生殖系统疾病",
		"M13.900": "肌肉骨骼系统和结缔组织疾病",
		"D50.900": "血液及造血器官疾病和某些涉及免疫机制的疾患",
		"D00.000": "肿瘤",
		"H25.900": "眼和附器疾病",
		"H60.000": "耳和乳突疾病",
		"A01.000": "某些传染病和寄生虫病",
		"":        "",
	}
	for code, want := range cases {
		if got := icd10Category(code); got != want {
			t.Fatalf("icd10Category(%q) = %q, want %q", code, got, want)
		}
	}
}
