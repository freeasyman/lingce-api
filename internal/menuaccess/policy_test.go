package menuaccess

import "testing"

func TestResolveAllowedMenuCodesExpandsFeatureItems(t *testing.T) {
	defs := []MenuDefinition{
		{Code: "doctor_recordings", FeatureCode: "recording_center"},
		{Code: "consultant_recordings", FeatureCode: "recording_center"},
		{Code: "roles", FeatureCode: "rbac"},
	}
	items := []PolicyItem{
		{ItemType: "feature", ItemCode: "recording_center", Enabled: true},
		{ItemType: "menu", ItemCode: "roles", Enabled: true},
	}

	got := ResolveAllowedMenuCodes(defs, items, nil)
	for _, code := range []string{"doctor_recordings", "consultant_recordings", "roles"} {
		if _, ok := got[code]; !ok {
			t.Fatalf("expected %s to be allowed", code)
		}
	}
}

func TestResolveAllowedMenuCodesFeatureDenyKeepsExplicitMenuAllow(t *testing.T) {
	defs := []MenuDefinition{
		{Code: "doctor_recordings", FeatureCode: "recording_center"},
		{Code: "consultant_recordings", FeatureCode: "recording_center"},
	}
	items := []PolicyItem{
		{ItemType: "feature", ItemCode: "recording_center", Enabled: true},
	}
	overrides := []PolicyOverride{
		{ItemType: "feature", ItemCode: "recording_center", OverrideMode: "deny"},
		{ItemType: "menu", ItemCode: "doctor_recordings", OverrideMode: "allow"},
	}

	got := ResolveAllowedMenuCodes(defs, items, overrides)
	if _, ok := got["doctor_recordings"]; !ok {
		t.Fatalf("expected doctor_recordings to remain allowed")
	}
	if _, ok := got["consultant_recordings"]; ok {
		t.Fatalf("expected consultant_recordings to be denied")
	}
}
