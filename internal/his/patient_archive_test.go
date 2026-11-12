package his

import (
	"errors"
	"strings"
	"testing"
)

func TestNormalizePatientLimit(t *testing.T) {
	cases := []struct {
		raw   string
		want  int
		valid bool
	}{
		{"", 20, true},
		{"1", 1, true},
		{"100", 100, true},
		{"101", 100, true},
		{"9999", 100, true},
		{" 25 ", 25, true},
		{"0", 0, false},
		{"-3", 0, false},
		{"abc", 0, false},
	}
	for _, tc := range cases {
		got, err := normalizePatientLimit(tc.raw)
		if tc.valid {
			if err != nil {
				t.Fatalf("normalizePatientLimit(%q) returned error: %v", tc.raw, err)
			}
			if got != tc.want {
				t.Fatalf("normalizePatientLimit(%q) = %d, want %d", tc.raw, got, tc.want)
			}
			continue
		}
		if !errors.Is(err, ErrInvalidRequest) {
			t.Fatalf("normalizePatientLimit(%q) error = %v, want ErrInvalidRequest", tc.raw, err)
		}
	}
}

func TestNormalizePatientKeyword(t *testing.T) {
	got, err := normalizePatientKeyword("  张芳  ")
	if err != nil {
		t.Fatalf("normalizePatientKeyword returned error: %v", err)
	}
	if got != "张芳" {
		t.Fatalf("normalizePatientKeyword trimmed = %q, want 张芳", got)
	}

	limit := strings.Repeat("患", patientKeywordMaxRunes)
	if _, err := normalizePatientKeyword(limit); err != nil {
		t.Fatalf("keyword at max length should be valid: %v", err)
	}
	if _, err := normalizePatientKeyword(limit + "者"); !errors.Is(err, ErrInvalidRequest) {
		t.Fatalf("keyword over max length error = %v, want ErrInvalidRequest", err)
	}
}

func TestDecodePatientTags(t *testing.T) {
	cases := map[string][]string{
		`["高血压"," 复诊 ","高血压",""]`:   {"高血压", "复诊"},
		`{"tags":["糖尿病", 3, "复诊"]}`: {"糖尿病", "复诊"},
		`null`:                      {},
		`not-json`:                  {},
		`[]`:                        {},
	}
	for raw, want := range cases {
		got := decodePatientTags(raw)
		if len(got) != len(want) {
			t.Fatalf("decodePatientTags(%q) = %v, want %v", raw, got, want)
		}
		for i := range want {
			if got[i] != want[i] {
				t.Fatalf("decodePatientTags(%q) = %v, want %v", raw, got, want)
			}
		}
	}
}

func TestPatientOrderAmount(t *testing.T) {
	cases := []struct {
		status  string
		payable float64
		paid    float64
		want    float64
	}{
		{"paid", 58, 58, 58},
		{"partial", 100, 40, 40},
		{"partial", 100, 0, 100},
		{"pending", 88, 0, 88},
		{"refunded", 88, 88, 88},
	}
	for _, tc := range cases {
		if got := patientOrderAmount(tc.status, tc.payable, tc.paid); got != tc.want {
			t.Fatalf("patientOrderAmount(%q,%v,%v) = %v, want %v", tc.status, tc.payable, tc.paid, got, tc.want)
		}
	}
}

func TestAllergyFromExtraData(t *testing.T) {
	if got := allergyFromExtraData(nil); got != "无" {
		t.Fatalf("empty extra_data allergy = %q, want 无", got)
	}
	if got := allergyFromExtraData([]byte(`{"allergy":" 青霉素 "}`)); got != "青霉素" {
		t.Fatalf("trimmed allergy = %q, want 青霉素", got)
	}
	if got := allergyFromExtraData([]byte(`{"allergy":""}`)); got != "无" {
		t.Fatalf("blank allergy = %q, want 无", got)
	}
	if got := allergyFromExtraData([]byte(`not-json`)); got != "无" {
		t.Fatalf("invalid extra_data allergy = %q, want 无", got)
	}
}
