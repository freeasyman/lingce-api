package emrpermission

import "testing"

func TestAccessHasUsesConfiguredAbilities(t *testing.T) {
	access := &Access{
		Role: "doctor",
		Abilities: map[string]struct{}{
			"record.read": {},
			"record.edit": {},
		},
	}

	if !access.Has("record.read") {
		t.Fatal("expected configured read ability")
	}
	if access.Has("record.archive") {
		t.Fatal("did not expect archive ability")
	}
}

func TestAccessHasAllowsCreateAndSubmitWithEditAbility(t *testing.T) {
	access := &Access{
		Abilities: map[string]struct{}{
			"record.edit": {},
		},
	}

	if !access.Has("record.create") {
		t.Fatal("expected edit ability to allow create")
	}
	if !access.Has("record.submit") {
		t.Fatal("expected edit ability to allow submit")
	}
}

func TestAccessCanRecordByScope(t *testing.T) {
	departmentID := int64(12)
	otherDepartmentID := int64(13)
	cases := []struct {
		name         string
		access       *Access
		doctorID     int64
		recordDeptID *int64
		want         bool
	}{
		{
			name:     "self allows own record",
			access:   &Access{UserID: 7, Scope: "self"},
			doctorID: 7,
			want:     true,
		},
		{
			name:     "self denies another doctor",
			access:   &Access{UserID: 7, Scope: "self"},
			doctorID: 8,
			want:     false,
		},
		{
			name:         "department allows same department",
			access:       &Access{DepartmentID: &departmentID, Scope: "department"},
			doctorID:     8,
			recordDeptID: &departmentID,
			want:         true,
		},
		{
			name:         "department denies another department",
			access:       &Access{DepartmentID: &departmentID, Scope: "department"},
			doctorID:     8,
			recordDeptID: &otherDepartmentID,
			want:         false,
		},
		{
			name:         "tenant allows all departments",
			access:       &Access{Scope: "tenant"},
			doctorID:     8,
			recordDeptID: &otherDepartmentID,
			want:         true,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := tc.access.CanRecord(tc.doctorID, tc.recordDeptID); got != tc.want {
				t.Fatalf("CanRecord() = %v, want %v", got, tc.want)
			}
		})
	}
}

func TestAdminAccessBypassesConfiguredAbilityCheck(t *testing.T) {
	access := &Access{Admin: true}

	if !access.Has("quality.manage") {
		t.Fatal("expected admin to have every ability")
	}
	if !access.CanManagePermissions() {
		t.Fatal("expected admin to manage permissions")
	}
	if !access.CanRecord(99, nil) {
		t.Fatal("expected admin to access every record")
	}
}
