package dealexpertpermission

import (
	"reflect"
	"testing"
)

func TestNormalizeDataScope(t *testing.T) {
	cases := map[string]string{
		"":        DataScopeSelf,
		"self":    DataScopeSelf,
		" custom": DataScopeCustom,
		"CUSTOM":  DataScopeCustom,
		"tenant":  DataScopeTenant,
		"bogus":   DataScopeSelf,
	}
	for input, want := range cases {
		if got := NormalizeDataScope(input); got != want {
			t.Fatalf("NormalizeDataScope(%q) = %q, want %q", input, got, want)
		}
	}
}

func TestDefaultDataScopeForRole(t *testing.T) {
	cases := map[string]string{
		"admin":         DataScopeTenant,
		"manager":       DataScopeCustom,
		"sales_manager": DataScopeCustom,
		"director":      DataScopeCustom,
		"consultant":    DataScopeSelf,
		"staff":         DataScopeSelf,
		"":              DataScopeSelf,
	}
	for role, want := range cases {
		if got := DefaultDataScopeForRole(role); got != want {
			t.Fatalf("DefaultDataScopeForRole(%q) = %q, want %q", role, got, want)
		}
	}
}

func TestResolveVisibility(t *testing.T) {
	cases := []struct {
		name  string
		scope string
		ids   []int64
		self  int64
		admin bool
		want  *Visibility
	}{
		{
			name:  "admin forced tenant",
			scope: DataScopeCustom,
			ids:   []int64{2, 3},
			self:  1,
			admin: true,
			want:  &Visibility{Scope: DataScopeTenant, All: true, EmployeeIDs: []int64{}},
		},
		{
			name:  "self only sees self even if ids passed",
			scope: DataScopeSelf,
			ids:   []int64{2, 3},
			self:  1,
			want:  &Visibility{Scope: DataScopeSelf, EmployeeIDs: []int64{1}},
		},
		{
			name:  "custom dedupes and keeps order",
			scope: DataScopeCustom,
			ids:   []int64{3, 2, 3, 0, -1},
			self:  1,
			want:  &Visibility{Scope: DataScopeCustom, EmployeeIDs: []int64{3, 2}},
		},
		{
			name:  "custom empty means nobody",
			scope: DataScopeCustom,
			self:  1,
			want:  &Visibility{Scope: DataScopeCustom, EmployeeIDs: []int64{}},
		},
		{
			name:  "non-admin tenant is clamped to self",
			scope: DataScopeTenant,
			self:  1,
			want:  &Visibility{Scope: DataScopeSelf, EmployeeIDs: []int64{1}},
		},
	}
	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			got := ResolveVisibility(testCase.scope, testCase.ids, testCase.self, testCase.admin)
			if !reflect.DeepEqual(got, testCase.want) {
				t.Fatalf("ResolveVisibility = %#v, want %#v", got, testCase.want)
			}
		})
	}
}

func TestVisibilityAllows(t *testing.T) {
	all := &Visibility{Scope: DataScopeTenant, All: true}
	if !all.Allows(99) {
		t.Fatal("tenant visibility should allow every employee")
	}
	custom := &Visibility{Scope: DataScopeCustom, EmployeeIDs: []int64{2, 3}}
	if !custom.Allows(2) || !custom.Allows(3) {
		t.Fatal("custom visibility should allow listed employees")
	}
	if custom.Allows(1) {
		t.Fatal("custom visibility must not allow unlisted employees")
	}
	empty := &Visibility{Scope: DataScopeCustom, EmployeeIDs: []int64{}}
	if empty.Allows(1) {
		t.Fatal("empty custom visibility must not allow anyone")
	}
	var nilVisibility *Visibility
	if nilVisibility.Allows(1) {
		t.Fatal("nil visibility must not allow anyone")
	}
	if got := all.VisibilityEmployeeIDs(); got != nil {
		t.Fatalf("tenant should return nil SQL filter, got %#v", got)
	}
	if got := custom.VisibilityEmployeeIDs(); !reflect.DeepEqual(got, []int64{2, 3}) {
		t.Fatalf("custom SQL filter = %#v", got)
	}
}

func TestNormalizeVisibleEmployeeIDs(t *testing.T) {
	got := NormalizeVisibleEmployeeIDs([]int64{5, 0, 5, -2, 7})
	want := []int64{5, 7}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("NormalizeVisibleEmployeeIDs = %#v, want %#v", got, want)
	}
	if empty := NormalizeVisibleEmployeeIDs(nil); empty == nil || len(empty) != 0 {
		t.Fatalf("empty input should return non-nil empty slice, got %#v", empty)
	}
}
