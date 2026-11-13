package his

import (
	"strings"
	"unicode"

	"github.com/mozillazg/go-pinyin"
)

// pinyinArgs 使用 go-pinyin 默认配置：小写、无音调、逐字转写。
var pinyinArgs = pinyin.NewArgs()

// PinyinOf 生成名称的全拼与首字母串，供收费项目/药品拼音检索。
//
// 规则（与 HIS 字典拼音约定一致）：
//   - 汉字转小写全拼，多字直接拼接；
//   - ASCII 字母/数字保留并转小写，连续字母数字视为一个词元；
//   - 其余符号（空格、括号、斜杠、百分号等）跳过。
//
// 例：血常规 -> xuechanggui / xcg；甲硝唑片 -> jiaxiaozuopian / jxzp；
// 妇科B超 -> fukebchao / fkb；CT（胸部） -> ctxiongbu / cxb。
func PinyinOf(name string) (full, initials string) {
	name = strings.TrimSpace(name)
	if name == "" {
		return "", ""
	}
	runes := []rune(name)
	var fullBuilder, initialBuilder strings.Builder
	for index := 0; index < len(runes); {
		current := runes[index]
		if unicode.Is(unicode.Han, current) {
			end := index
			for end < len(runes) && unicode.Is(unicode.Han, runes[end]) {
				end++
			}
			for _, syllables := range pinyin.Pinyin(string(runes[index:end]), pinyinArgs) {
				if len(syllables) == 0 {
					continue
				}
				syllable := strings.ToLower(syllables[0])
				fullBuilder.WriteString(syllable)
				if syllable != "" {
					initialBuilder.WriteString(syllable[:1])
				}
			}
			index = end
			continue
		}
		if isPinyinASCIIAlnum(current) {
			end := index
			for end < len(runes) && isPinyinASCIIAlnum(runes[end]) {
				end++
			}
			token := strings.ToLower(string(runes[index:end]))
			fullBuilder.WriteString(token)
			if token != "" {
				initialBuilder.WriteString(token[:1])
			}
			index = end
			continue
		}
		index++
	}
	return fullBuilder.String(), initialBuilder.String()
}

// isPinyinASCIIAlnum 判断是否为需要保留的 ASCII 字母/数字。
func isPinyinASCIIAlnum(value rune) bool {
	return value < unicode.MaxASCII &&
		(value >= 'a' && value <= 'z' || value >= 'A' && value <= 'Z' || value >= '0' && value <= '9')
}
