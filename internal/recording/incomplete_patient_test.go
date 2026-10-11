package recording

import "testing"

func TestValidatePatientCompletion(t *testing.T) {
	badAge := 131
	badGender := "猜测"
	for _, c := range []struct {
		name  string
		req   CompletePatientRequest
		fails bool
	}{
		{"complete", CompletePatientRequest{Name: "张先生", Phone: "13800000000"}, false},
		{"missing_name", CompletePatientRequest{Phone: "13800000000"}, true},
		{"missing_phone", CompletePatientRequest{Name: "张先生"}, true},
		{"whitespace", CompletePatientRequest{Name: " ", Phone: " "}, true},
		{"age_range", CompletePatientRequest{Name: "张先生", Phone: "13800000000", Age: &badAge}, true},
		{"gender", CompletePatientRequest{Name: "张先生", Phone: "13800000000", Gender: &badGender}, true},
	} {
		t.Run(c.name, func(t *testing.T) {
			if err := validatePatientCompletion(c.req); (err != nil) != c.fails {
				t.Fatalf("unexpected error: %v", err)
			}
		})
	}
}
