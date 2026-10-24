package content

import "testing"

func TestNormalizeValueLevel(t *testing.T) {
	cases := []struct{ lv, j, wantLv, wantJ string }{
		{"", "极高价值。'上热下寒'是中医特色", "高", "'上热下寒'是中医特色"},
		{"", "高价值。干性湿疹患者", "高", "干性湿疹患者"},
		{"", "中等价值。熬夜话题", "中", "熬夜话题"},
		{"", "高：精准纠正误区", "高", "精准纠正误区"},
		{"", "该洞察切中了痛点", "", "该洞察切中了痛点"},
		{"", "高血压是常见病", "", "高血压是常见病"},
		{"高", "极高价值。x", "高", "x"},
	}
	for _, c := range cases {
		lv, j := normalizeValueLevel(c.lv, c.j)
		if lv != c.wantLv || j != c.wantJ {
			t.Errorf("normalizeValueLevel(%q,%q)=%q,%q want %q,%q", c.lv, c.j, lv, j, c.wantLv, c.wantJ)
		}
	}
}
