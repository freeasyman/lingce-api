package patientops

import (
	"errors"
	"testing"
)

func TestPlanAgeAdmissionPolicy(t *testing.T) {
	min, max := 3, 12
	for _, tc := range []struct {
		name    string
		age     int
		product planProduct
		want    string
	}{
		{"unknown unrestricted", 0, planProduct{}, "ready"},
		{"unknown bounded optional", 0, planProduct{AgeMin: &min, AgeMax: &max}, "needs_confirmation"},
		{"unknown required", 0, planProduct{AgeMin: &min, AgeMax: &max, AgeRequired: true}, "needs_age"},
		{"required without bounds", 0, planProduct{AgeRequired: true}, "needs_age"},
		{"known in range", 7, planProduct{AgeMin: &min, AgeMax: &max, AgeRequired: true}, "ready"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if status, _ := planAgeAdmission(tc.age, &tc.product); status != tc.want {
				t.Fatalf("got %s want %s", status, tc.want)
			}
		})
	}
	products := []planProduct{{ID: 1, ApplicableKeywords: []string{"体态"}, AgeMin: &min, AgeMax: &max}}
	f := planTestFact()
	f.Age = 30
	if p, _, _, _ := matchPlanProduct(f, products); p != nil {
		t.Fatal("known outside-range age must remain a hard exclusion even when age_required is false")
	}
	f.Age = 0
	f.Interest = ""
	f.Concern = ""
	f.Question = ""
	if p, _, _, _ := matchPlanProduct(f, products); p != nil {
		t.Fatal("unknown age must not create an irrelevant product match")
	}
}

func TestPlanProductPrefersReadyMatch(t *testing.T) {
	min := 3
	products := []planProduct{
		{ID: 1, Name: "体态", ApplicableKeywords: []string{"体态", "脊柱侧弯"}, AgeMin: &min, AgeRequired: true},
		{ID: 2, ApplicableKeywords: []string{"体态"}, AgeMin: &min},
		{ID: 3, ApplicableKeywords: []string{"体态"}},
	}
	f := planTestFact()
	f.Age = 0
	p, _, _, _ := matchPlanProduct(f, products)
	if p == nil || p.ID != 3 {
		t.Fatalf("ready relevant product should win over pending products: %+v", p)
	}
	p, _, _, _ = matchPlanProduct(f, products[:2])
	if p == nil || p.ID != 2 {
		t.Fatalf("optional bounded product should win over mandatory-age product: %+v", p)
	}
}

func TestPlanSuitabilityConfirmationsNoLongerBlockAdmission(t *testing.T) {
	pid := int64(36)
	preview := &PlanPreview{Candidates: []PlanCandidate{
		{CustomerID: 1, CustomerName: "已知客户", AdmissionStatus: "ready"},
		{CustomerID: 2, CustomerName: "未知年龄", RecommendedProductID: &pid, ProductVersion: 4, AdmissionStatus: "needs_confirmation"},
		{CustomerID: 3, CustomerName: "必须补全", RecommendedProductID: &pid, ProductVersion: 4, AdmissionStatus: "needs_age", AdmissionReason: "必须补全年龄"},
	}}
	valid := PlanSuitabilityConfirmation{CustomerID: 2, ProductID: pid, ProductVersion: 4, Reason: "人工核对适用条件"}
	for _, tc := range []struct {
		name    string
		ids     []int64
		reviews []PlanSuitabilityConfirmation
		wantOK  bool
	}{
		{"ready", []int64{1}, nil, true},
		{"unknown age no confirmation", []int64{1, 2}, nil, true},
		{"valid", []int64{1, 2}, []PlanSuitabilityConfirmation{valid}, true},
		{"missing age no confirmation", []int64{1, 3}, nil, true},
		{"excluded cannot override", []int64{4}, nil, false},
		{"deduplicated", []int64{1, 1}, nil, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			selected, err := selectPlanCandidates(preview, tc.ids, tc.reviews)
			if tc.wantOK {
				if err != nil || len(selected) == 0 {
					t.Fatalf("%v: %+v", err, selected)
				}
			} else {
				var rejected *PlanAdmissionError
				if !errors.As(err, &rejected) || len(selected) != 0 {
					t.Fatalf("atomic rejection required: %v %+v", err, selected)
				}
			}
		})
	}
}
