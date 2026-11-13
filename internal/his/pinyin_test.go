package his

import "testing"

func TestPinyinOfChineseNames(t *testing.T) {
	cases := []struct {
		name     string
		full     string
		initials string
	}{
		{"血常规", "xuechanggui", "xcg"},
		{"甲硝唑片", "jiaxiaozuopian", "jxzp"},
		{"测试拼音项目", "ceshipinyinxiangmu", "cspyxm"},
		{"阿奇霉素片", "aqimeisupian", "aqmsp"},
		{"妇科检查", "fukejiancha", "fkjc"},
		// 患者姓名的前缀（演示-）与数字后缀：搜索是 ILIKE '%kw%'，
		// hejun / hj / fuzhenyanzheng / fzyz 都必须能在全拼/首字母串中命中。
		{"演示-何军", "yanshihejun", "yshj"},
		{"复诊验证202100", "fuzhenyanzheng202100", "fzyz2"},
	}
	for _, testCase := range cases {
		full, initials := PinyinOf(testCase.name)
		if full != testCase.full || initials != testCase.initials {
			t.Fatalf("PinyinOf(%q) = %q/%q, want %q/%q",
				testCase.name, full, initials, testCase.full, testCase.initials)
		}
	}
}

func TestPinyinOfEnglishDigitsAndSymbols(t *testing.T) {
	cases := []struct {
		name     string
		full     string
		initials string
	}{
		{"妇科B超", "fukebchao", "fkbc"},
		{"维生素C片", "weishengsucpian", "wsscp"},
		{"CT（胸部）", "ctxiongbu", "cxb"},
		{"2%利多卡因", "2liduokayin", "2ldky"},
		{"HIV 抗体(筛查)", "hivkangtishaicha", "hktsc"},
		{"（）/ - %", "", ""},
		{"", "", ""},
	}
	for _, testCase := range cases {
		full, initials := PinyinOf(testCase.name)
		if full != testCase.full || initials != testCase.initials {
			t.Fatalf("PinyinOf(%q) = %q/%q, want %q/%q",
				testCase.name, full, initials, testCase.full, testCase.initials)
		}
	}
}

// 首字母检索串必须包含 jxz，确保「甲硝唑片」可按 jxz 命中。
func TestPinyinOfDrugInitialsContainJXZ(t *testing.T) {
	_, initials := PinyinOf("甲硝唑片")
	if len(initials) < 3 || initials[:3] != "jxz" {
		t.Fatalf("initials = %q, want prefix jxz", initials)
	}
}
