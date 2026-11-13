package his

import "testing"

func TestNormalizeBirthDate(t *testing.T) {
	valid := map[string]string{
		"1996-03-15":   "1996-03-15",
		"1996/3/5":     "1996-03-05",
		"1996.03.05":   "1996-03-05",
		"19960305":     "1996-03-05",
		" 2000-01-01 ": "2000-01-01",
	}
	for raw, want := range valid {
		got, err := normalizeBirthDate(raw)
		if err != nil {
			t.Fatalf("normalizeBirthDate(%q) error: %v", raw, err)
		}
		if got != want {
			t.Fatalf("normalizeBirthDate(%q) = %q, want %q", raw, got, want)
		}
	}
	for _, raw := range []string{"1996-13-01", "1996-02-30", "1899-01-01", "2999-01-01", "1996-03", "abc"} {
		if _, err := normalizeBirthDate(raw); err == nil {
			t.Fatalf("normalizeBirthDate(%q) should fail", raw)
		}
	}
}

func TestBirthDateFromIDCard(t *testing.T) {
	if got := birthDateFromIDCard("110101199603155678"); got != "1996-03-15" {
		t.Fatalf("18 位身份证解析 = %q, want 1996-03-15", got)
	}
	if got := birthDateFromIDCard("110101960315567"); got != "1996-03-15" {
		t.Fatalf("15 位身份证解析 = %q, want 1996-03-15", got)
	}
	for _, raw := range []string{"", "123", "110101199613355678", "110101189903155678", "abc"} {
		if got := birthDateFromIDCard(raw); got != "" {
			t.Fatalf("birthDateFromIDCard(%q) = %q, want empty", raw, got)
		}
	}
}

func TestBirthDateForRequestFallsBackToIDCard(t *testing.T) {
	birth, err := birthDateForRequest("", "110101199603155678")
	if err != nil {
		t.Fatalf("birthDateForRequest error: %v", err)
	}
	if birth == nil || birth.Format(birthDateLayout) != "1996-03-15" {
		t.Fatalf("birthDateForRequest = %v, want 1996-03-15", birth)
	}
	if birth, err := birthDateForRequest("", ""); err != nil || birth != nil {
		t.Fatalf("empty request should return nil, got birth=%v err=%v", birth, err)
	}
}
