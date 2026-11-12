package pdf

// 中英混排断行。
//
// gofpdf 自带的 MultiCell 只在空格处断行，一段没有空格的中文会整段不换行直接溢出。
// 这里按「字符宽度累加」自行断行：
//   - 汉字、全角标点逐字断（每字一个排版单元）；
//   - 连续的 ASCII 字母/数字/URL 片段作为一个整体单元，不把 https://app.lingce.com 拆碎；
//   - 全角收尾标点（，。；：！？）》等）粘到前一个字上，避免出现在行首；
//   - 全角起始标点（（《「等）粘到后一个字上，避免孤零零留在行尾；
//   - 行首丢弃空格，行尾空格不计宽。

import (
	"strings"
	"unicode"
)

// span 是一段同样式文本（bold 控制是否用 SemiBold 字体）。
type span struct {
	text string
	bold bool
}

// textUnit 是断行算法的最小单元。
type textUnit struct {
	text  string
	bold  bool
	width float64
	space bool
}

// textLine 是断行后的一行。
type textLine struct {
	units []textUnit
	width float64
}

// markupSpans 把 "普通 **加粗** 普通" 解析成富文本片段。
//
// 只支持 ** 加粗；落单的 ** 按普通字符处理，不会吞掉正文。
func markupSpans(text string) []span {
	spans := make([]span, 0, 4)
	bold := false
	rest := text
	for rest != "" {
		index := strings.Index(rest, "**")
		if index < 0 {
			spans = append(spans, span{text: rest, bold: bold})
			break
		}
		if index > 0 {
			spans = append(spans, span{text: rest[:index], bold: bold})
		}
		bold = !bold
		rest = rest[index+2:]
	}
	// 收尾的 ** 没有配对时，把最后一段当作普通文本，避免整段变粗。
	if bold {
		for i := len(spans) - 1; i >= 0; i-- {
			if spans[i].text != "" {
				spans[i].bold = false
				break
			}
		}
	}
	return spans
}

// isSpaceRune 判断是否排版空格（半角、全角、不换行空格）。
func isSpaceRune(r rune) bool {
	switch r {
	case ' ', '\t', '\n', '\u00a0', '\u3000':
		return true
	}
	return false
}

// isClosingPunct 判断全角收尾标点（不能出现在行首）。
func isClosingPunct(r rune) bool {
	switch r {
	case '，', '。', '、', '；', '：', '！', '？', '）', '】', '》', '」', '』', '”', '’',
		')', ']', '}', '％', '‰':
		return true
	}
	return false
}

// isOpeningPunct 判断全角起始标点（不能出现在行尾）。
func isOpeningPunct(r rune) bool {
	switch r {
	case '（', '【', '《', '「', '『', '“', '‘', '(', '[', '{':
		return true
	}
	return false
}

// isCJK 判断是否按字断行的中文/全角字符。
func isCJK(r rune) bool {
	switch {
	case unicode.Is(unicode.Han, r):
		return true
	case r >= 0x3000 && r <= 0x303f: // CJK 标点
		return true
	case r >= 0xff00 && r <= 0xffef: // 全角字符
		return true
	case r == '·', r == '—', r == '…', r == '“', r == '”', r == '‘', r == '’':
		return true
	}
	return false
}

// isWordRune 判断是否 ASCII 词内字符（字母、数字、URL 标点等）。
func isWordRune(r rune) bool {
	return !isSpaceRune(r) && !isCJK(r) && !isClosingPunct(r) && !isOpeningPunct(r)
}

// unitsFor 把富文本片段切成排版单元。
func (c *canvas) unitsFor(spans []span, size float64) []textUnit {
	units := make([]textUnit, 0, 64)
	// pending 暂存需要粘到下一个字上的起始标点。
	var pending strings.Builder

	flushPending := func(bold bool) {
		if pending.Len() == 0 {
			return
		}
		text := pending.String()
		pending.Reset()
		units = append(units, textUnit{text: text, bold: bold, width: c.textWidth(text, bold, size)})
	}

	for _, item := range spans {
		runes := []rune(item.text)
		for i := 0; i < len(runes); i++ {
			r := runes[i]
			if isSpaceRune(r) {
				flushPending(item.bold)
				units = append(units, textUnit{text: string(r), bold: item.bold, width: c.textWidth(string(r), item.bold, size), space: true})
				continue
			}
			if isClosingPunct(r) {
				// 粘到前一个非空格单元；前面只有空格时单独成单元，保证不丢标点。
				target := -1
				for j := len(units) - 1; j >= 0; j-- {
					if !units[j].space {
						target = j
						break
					}
				}
				if target >= 0 {
					units[target].text += string(r)
					units[target].width = c.textWidth(units[target].text, units[target].bold, size)
					continue
				}
			}
			if isOpeningPunct(r) {
				pending.WriteRune(r)
				continue
			}
			if isWordRune(r) {
				// 连续 ASCII 片段整体作为一个单元。
				j := i
				for j < len(runes) && isWordRune(runes[j]) {
					j++
				}
				prefix := pending.String()
				pending.Reset()
				text := prefix + string(runes[i:j])
				units = append(units, textUnit{text: text, bold: item.bold, width: c.textWidth(text, item.bold, size)})
				i = j - 1
				continue
			}
			// 汉字/全角字：逐字成单元。
			prefix := pending.String()
			pending.Reset()
			text := prefix + string(r)
			units = append(units, textUnit{text: text, bold: item.bold, width: c.textWidth(text, item.bold, size)})
		}
	}
	flushPending(false)
	return units
}

// wrapSpans 按最大宽度把富文本断成多行。
func (c *canvas) wrapSpans(spans []span, size, maxWidth float64) []textLine {
	units := c.unitsFor(spans, size)
	lines := make([]textLine, 0, 8)
	current := textLine{units: make([]textUnit, 0, 32)}

	flush := func() {
		for len(current.units) > 0 && current.units[len(current.units)-1].space {
			current.width -= current.units[len(current.units)-1].width
			current.units = current.units[:len(current.units)-1]
		}
		lines = append(lines, current)
		current = textLine{units: make([]textUnit, 0, 32)}
	}

	for _, unit := range units {
		if unit.space && len(current.units) == 0 {
			continue // 行首不排空格
		}
		if !unit.space && current.width+unit.width > maxWidth && len(current.units) > 0 {
			flush()
		}
		current.units = append(current.units, unit)
		current.width += unit.width
	}
	if len(current.units) > 0 {
		flush()
	}
	if len(lines) == 0 {
		lines = append(lines, textLine{})
	}
	return lines
}

// wrapText 是 wrapSpans 的纯文本快捷方式。
func (c *canvas) wrapText(text string, size, maxWidth float64) []textLine {
	return c.wrapSpans(markupSpans(text), size, maxWidth)
}
