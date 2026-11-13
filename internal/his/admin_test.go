package his

import (
	"errors"
	"testing"
)

func TestHISRoleAndModuleCatalogs(t *testing.T) {
	roles := HISRoles()
	if len(roles) != 9 {
		t.Fatalf("roles len = %d, want 9: %v", len(roles), roles)
	}
	if roles[0] != "admin" {
		t.Fatalf("roles[0] = %q, want admin", roles[0])
	}
	seenRoles := map[string]bool{}
	for _, role := range roles {
		if seenRoles[role] {
			t.Fatalf("duplicate role %q", role)
		}
		seenRoles[role] = true
	}
	for _, want := range []string{"assistant", "therapist", "labtech", "checktech", "chief", "other"} {
		if !seenRoles[want] {
			t.Fatalf("missing role %q in %v", want, roles)
		}
	}

	modules := HISModules()
	if len(modules) != 14 {
		t.Fatalf("modules len = %d, want 14 (13 nav + dictionary): %v", len(modules), modules)
	}
	seenModules := map[string]bool{}
	for _, module := range modules {
		if !hisModuleSet[module] {
			t.Fatalf("module %q missing from module set", module)
		}
		if seenModules[module] {
			t.Fatalf("duplicate module %q", module)
		}
		seenModules[module] = true
	}
	if !seenModules["pharmacy"] || !seenModules["admin"] {
		t.Fatalf("module catalog must use HIS nav ids, got %v", modules)
	}
	if !seenModules["dictionary"] {
		t.Fatalf("module catalog must include dictionary permission point, got %v", modules)
	}
}

func TestNormalizeRoleModules(t *testing.T) {
	got, err := normalizeRoleModules("doctor", []string{"stats", "home", "stats", " clinic "})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	want := []string{"home", "clinic", "stats"}
	if len(got) != len(want) {
		t.Fatalf("modules = %v, want %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("modules = %v, want %v（按模块目录排序并去重）", got, want)
		}
	}

	withDictionary, err := normalizeRoleModules("doctor", []string{"dictionary", "home"})
	if err != nil {
		t.Fatalf("dictionary should be assignable: %v", err)
	}
	if len(withDictionary) != 2 || withDictionary[0] != "home" || withDictionary[1] != "dictionary" {
		t.Fatalf("modules = %v, want [home dictionary]", withDictionary)
	}

	cases := []struct {
		name    string
		role    string
		modules []string
		wantErr error
	}{
		{name: "未知角色", role: "pharmacist", modules: []string{"home"}, wantErr: ErrInvalidRole},
		{name: "空角色", role: "", modules: []string{"home"}, wantErr: ErrInvalidRole},
		{name: "admin 锁定", role: "admin", modules: []string{"home"}, wantErr: ErrAdminRoleLocked},
		{name: "未知模块", role: "doctor", modules: []string{"home", "settings"}, wantErr: ErrInvalidModule},
		{name: "空模块", role: "doctor", modules: []string{}, wantErr: ErrEmptyRoleModules},
		{name: "仅空白", role: "doctor", modules: []string{"  "}, wantErr: ErrInvalidModule},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if _, err := normalizeRoleModules(tc.role, tc.modules); !errors.Is(err, tc.wantErr) {
				t.Fatalf("err = %v, want %v", err, tc.wantErr)
			}
		})
	}
}

func TestBuildRoleModuleConfig(t *testing.T) {
	defaults := map[string][]string{
		"doctor": []string{"stats", "home", "clinic", "patients"},
		"nurse":  []string{"execute", "home"},
		"other":  []string{"home"},
	}
	tenant := map[string][]string{
		"doctor": {"clinic", "home"},
		"other":  {"unknown", "stats"},
	}
	config := buildRoleModuleConfig(defaults, tenant)

	if got := config["doctor"]; len(got) != 2 || got[0] != "home" || got[1] != "clinic" {
		t.Fatalf("doctor = %v, want [home clinic]（租户覆盖替换默认，不是并集）", got)
	}
	if got := config["nurse"]; len(got) != 2 || got[0] != "home" || got[1] != "execute" {
		t.Fatalf("nurse = %v, want default [home execute]", got)
	}
	if got := config["other"]; len(got) != 1 || got[0] != "stats" {
		t.Fatalf("other = %v, want [stats]（未知模块被丢弃）", got)
	}
	if got := config["admin"]; len(got) != 14 {
		t.Fatalf("admin modules = %v, want all 14 (13 nav + dictionary)", got)
	}
	if _, ok := config["chief"]; !ok {
		t.Fatalf("config must include all 9 roles, missing chief")
	}
	if got := config["chief"]; len(got) != 0 {
		t.Fatalf("chief = %v, want empty", got)
	}
}

func TestNormalizeCreateChargeItem(t *testing.T) {
	req := CreateChargeItemRequest{Code: " C900 ", Name: " 测试项目 ", Category: " 检查 ", Unit: " 次 ", Price: 12.5}
	if err := normalizeCreateChargeItem(&req); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if req.Code != "C900" || req.Name != "测试项目" || req.Category != "检查" || req.Unit != "次" {
		t.Fatalf("fields not trimmed: %+v", req)
	}

	bad := []CreateChargeItemRequest{
		{Name: "无编码", Price: 1},
		{Code: "C1", Price: 1},
		{Code: "C1", Name: "  ", Price: 1},
		{Code: "C1", Name: "负价", Price: -0.01},
	}
	for _, item := range bad {
		candidate := item
		if err := normalizeCreateChargeItem(&candidate); !errors.Is(err, ErrInvalidRequest) {
			t.Fatalf("%+v err = %v, want ErrInvalidRequest", item, err)
		}
	}
}

func TestNormalizeUpdateChargeItem(t *testing.T) {
	name, status, price := " 改名后 ", " inactive ", 0.0
	req := UpdateChargeItemRequest{Name: &name, Status: &status, Price: &price}
	if err := normalizeUpdateChargeItem(&req); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if *req.Name != "改名后" || *req.Status != "inactive" || *req.Price != 0 {
		t.Fatalf("fields not normalized: %+v", req)
	}

	invalidStatus := "deleted"
	negative := -1.0
	blankName := " "
	blankCategory := ""
	cases := []struct {
		name string
		req  UpdateChargeItemRequest
	}{
		{name: "空更新", req: UpdateChargeItemRequest{}},
		{name: "状态非法", req: UpdateChargeItemRequest{Status: &invalidStatus}},
		{name: "负价", req: UpdateChargeItemRequest{Price: &negative}},
		{name: "空名称", req: UpdateChargeItemRequest{Name: &blankName}},
		{name: "空分类", req: UpdateChargeItemRequest{Category: &blankCategory}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			candidate := tc.req
			if err := normalizeUpdateChargeItem(&candidate); !errors.Is(err, ErrInvalidRequest) {
				t.Fatalf("err = %v, want ErrInvalidRequest", err)
			}
		})
	}
}

func TestIsValidSchedulePeriod(t *testing.T) {
	valid := []string{"", "morning", "afternoon", "evening", "early_night", "late_night", "full_day"}
	for _, period := range valid {
		if !isValidSchedulePeriod(period) {
			t.Fatalf("period %q should be valid", period)
		}
	}
	invalid := []string{"night", "noon", "全天", "morning "}
	for _, period := range invalid {
		if isValidSchedulePeriod(period) {
			t.Fatalf("period %q should be invalid", period)
		}
	}
}
