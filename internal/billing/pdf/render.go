package pdf

// A4 单页排版与绘制。
//
// 两遍式布局：先用 dry 模式（只量宽度、只推进 y）量出内容底部，超过单页可用高度就按
// 0.02 的步长整体缩小字号与间距重排，直到放得下；随后再正式绘制一遍。这样既保证
// 「永远单页」，也不会把内容裁掉。
//
// 坐标单位全部是 mm，字号是 pt；页边距与设计稿一致：左右 18mm、上 17mm。

import (
	"bytes"
	"errors"
	"fmt"
	"strings"

	"github.com/jung-kurt/gofpdf"
)

const (
	pageWidth  = 210.0
	pageHeight = 297.0

	marginLeft  = 18.0
	marginRight = 18.0
	contentTop  = 17.0

	// contentBottom 是正文区下边界：底部顾问卡/落款必须收在这条线以上。
	contentBottom = 270.0
	// footerRuleY / footerTextY 是页脚横线与文字基线（固定贴页底）。
	footerRuleY = 279.0
	footerTextY = 283.6

	// minScale 是自动缩小的下限；正常内容（6 个产品 + 4 条提示）在 1.0 就能放下。
	minScale = 0.72
)

// contentWidth 是正文可用宽度（174mm）。
const contentWidth = pageWidth - marginLeft - marginRight

// contentRight 是正文右边界 x 坐标。
const contentRight = pageWidth - marginRight

// rgb 是 0-255 的 RGB 颜色。
type rgb struct{ r, g, b int }

// 颜色取自 Demo 的 .letter-page 样式。
var (
	colorPrimary    = rgb{0x4b, 0x42, 0x84} // 品牌紫：logo、序号、顾问头像
	colorTitle      = rgb{0x24, 0x21, 0x30} // 标题/落款这类高对比文字
	colorBody       = rgb{0x3b, 0x37, 0x48} // 正文
	colorHeading    = rgb{0x3a, 0x35, 0x60} // 小标题
	colorMuted      = rgb{0x8a, 0x84, 0xa0} // 次要说明（信息盒标签、日期）
	colorMetaLabel  = rgb{0xa2, 0x9c, 0xb5} // 页眉「编号 / 日期」标签
	colorMetaValue  = rgb{0x6b, 0x65, 0x80} // 页眉编号/日期值
	colorBorder     = rgb{0xe4, 0xdf, 0xf2} // 信息盒/表格外框
	colorInnerLine  = rgb{0xf0, 0xec, 0xf9} // 信息盒/表格内分隔线
	colorSoftFill   = rgb{0xfc, 0xfb, 0xff} // 信息盒/表格底色
	colorHeaderFill = rgb{0xf6, 0xf3, 0xfd} // 表头底色
	colorRule       = rgb{0xe8, 0xe4, 0xf0} // 页眉分隔线
	colorFooterText = rgb{0xb3, 0xad, 0xc4} // 页脚文字
	colorSealRed    = rgb{0xc0, 0x39, 0x2b} // 电子章红
	colorWhite      = rgb{0xff, 0xff, 0xff}
)

// widthKey 是字宽缓存的键。同一份文档里同一段文字会在不同字号下测量
// （例如「灵策」既用于页头 logo，也用于顾问头像的「灵」），字号必须进 key，
// 否则会命中错误的缓存宽度、把字推到偏左/偏右。
type widthKey struct {
	text string
	bold bool
	size float64
}

// canvas 是排版游标：dry=true 时只推进 y、不落笔。
type canvas struct {
	f          *gofpdf.Fpdf
	k          float64 // 整体缩放系数
	dry        bool
	y          float64
	widthCache map[widthKey]float64
}

func newCanvas(f *gofpdf.Fpdf, scale float64, dry bool) *canvas {
	return &canvas{f: f, k: scale, dry: dry, widthCache: make(map[widthKey]float64, 512)}
}

// ptToMM 把 pt 换算成 mm（1pt = 25.4/72 mm）。
func ptToMM(pt float64) float64 { return pt * 25.4 / 72 }

// scale 按整体缩放系数换算设计稿里的 mm 值。
func (c *canvas) scale(value float64) float64 { return value * c.k }

// setFont 设置字体（字号按缩放系数换算）。
func (c *canvas) setFont(bold bool, size float64) {
	style := ""
	if bold {
		style = "B"
	}
	c.f.SetFont(fontFamily, style, size*c.k)
}

// textWidth 返回文本在当前字号下的宽度（mm）。
func (c *canvas) textWidth(text string, bold bool, size float64) float64 {
	if text == "" {
		return 0
	}
	key := widthKey{text: text, bold: bold, size: size}
	if width, ok := c.widthCache[key]; ok {
		return width
	}
	c.setFont(bold, size)
	width := c.f.GetStringWidth(text)
	c.widthCache[key] = width
	return width
}

// text 在 (x, baseline) 处绘制一行文本；dry 模式只量不算。
func (c *canvas) text(x, baseline float64, text string, bold bool, size float64, color rgb) {
	if text == "" || c.dry {
		return
	}
	c.setFont(bold, size)
	c.f.SetTextColor(color.r, color.g, color.b)
	c.f.Text(x, baseline, text)
}

// baseline 计算行盒内文字的基线：垂直居中后再按字号抬高约 0.36em。
func baseline(top, lineHeight, size float64) float64 {
	return top + lineHeight/2 + ptToMM(size)*0.36
}

// hline 画一条水平线。
func (c *canvas) hline(x1, x2, y float64, color rgb, width float64) {
	if c.dry {
		return
	}
	c.f.SetDrawColor(color.r, color.g, color.b)
	c.f.SetLineWidth(c.scale(width))
	c.f.Line(x1, y, x2, y)
}

// spacedWidth 返回带字距文本的总宽度。
func (c *canvas) spacedWidth(text string, bold bool, size, spacing float64) float64 {
	runes := []rune(text)
	if len(runes) == 0 {
		return 0
	}
	total := 0.0
	for _, r := range runes {
		total += c.textWidth(string(r), bold, size)
	}
	return total + spacing*float64(len(runes)-1)
}

// spacedText 逐字绘制带字距的文本（标题、logo 用），返回总宽度。
func (c *canvas) spacedText(x, baselineY float64, text string, bold bool, size, spacing float64, color rgb) float64 {
	runes := []rune(text)
	if len(runes) == 0 {
		return 0
	}
	cursor := x
	for _, r := range runes {
		width := c.textWidth(string(r), bold, size)
		c.text(cursor, baselineY, string(r), bold, size, color)
		cursor += width + spacing
	}
	return cursor - x - spacing
}

// spacedCentered 居中绘制带字距文本。
func (c *canvas) spacedCentered(baselineY float64, text string, bold bool, size, spacing float64, color rgb) {
	width := c.spacedWidth(text, bold, size, spacing)
	c.spacedText((pageWidth-width)/2, baselineY, text, bold, size, spacing, color)
}

// ---------------------------------------------------------------------------
// 各区块绘制
// ---------------------------------------------------------------------------

// brand 画页眉：logo + 副标 + 右上角编号/日期 + 分隔线。
func (c *canvas) brand(letter Letter) {
	logoBaseline := contentTop + c.scale(8.8)
	logoWidth := c.spacedText(marginLeft, logoBaseline, "灵策", true, 21, c.scale(2.2), colorPrimary)
	c.text(marginLeft+logoWidth+c.scale(4.2), logoBaseline-c.scale(0.7), "智能医疗 · AI", false, 8, colorMuted)

	// 右上角两行：编号 / 日期，值比标签深一点。
	c.metaLine(contentTop+c.scale(3.6), "编号", letter.DocNo)
	c.metaLine(contentTop+c.scale(7.8), "日期", letter.DocDate)

	dividerY := contentTop + c.scale(17.2)
	c.hline(marginLeft, contentRight, dividerY, colorRule, 0.25)
	c.y = dividerY
}

// metaLine 在页面右侧画一行「标签 + 值」。
func (c *canvas) metaLine(baselineY float64, label, value string) {
	labelText := label + "　"
	labelWidth := c.textWidth(labelText, false, 8)
	valueWidth := c.textWidth(value, false, 8)
	startX := contentRight - labelWidth - valueWidth
	c.text(startX, baselineY, labelText, false, 8, colorMetaLabel)
	c.text(startX+labelWidth, baselineY, value, false, 8, colorMetaValue)
}

// title 画居中大标题（不加英文副标题、不加标题下横线）。
func (c *canvas) title(letter Letter) {
	top := c.y + c.scale(5.6)
	titleSize := 18.0
	lineHeight := ptToMM(titleSize) * c.k * 1.35
	c.spacedCentered(top+lineHeight*0.62, letter.Title, true, titleSize, c.scale(1.9), colorTitle)
	c.y = top + lineHeight + c.scale(3.6)
}

// salutation 画抬头（加粗 11pt）。
func (c *canvas) salutation(letter Letter) {
	lineHeight := ptToMM(11) * c.k * 1.55
	c.text(marginLeft, baseline(c.y, lineHeight, 11*c.k), letter.Salutation, true, 11, colorTitle)
	c.y += lineHeight + c.scale(3.2)
}

// paragraph 画一段正文（自动断行）。
func (c *canvas) paragraph(text string, size, lineFactor float64) {
	lineHeight := ptToMM(size) * lineFactor * c.k
	for _, line := range c.wrapText(text, size, contentWidth) {
		c.drawLine(marginLeft, baseline(c.y, lineHeight, size*c.k), line, size, colorBody)
		c.y += lineHeight
	}
	c.y += c.scale(3.0)
}

// sectionTitleLineHeight 是小标题（10.8pt）的行高。
func (c *canvas) sectionTitleLineHeight() float64 {
	return ptToMM(10.8) * c.k * 1.45
}

// sectionTitleBlockHeight 是小标题占用的总高度：上间距 + 行高 + 下间距。
func (c *canvas) sectionTitleBlockHeight() float64 {
	return c.scale(4.8) + c.sectionTitleLineHeight() + c.scale(2.4)
}

// drawSectionTitleAt 在 top 处画小标题（top 是上间距的起点），不推进游标。
func (c *canvas) drawSectionTitleAt(top float64, text string) {
	lineHeight := c.sectionTitleLineHeight()
	c.text(marginLeft, baseline(top+c.scale(4.8), lineHeight, 10.8*c.k), text, true, 10.8, colorHeading)
}

// sectionTitle 画小标题：比正文大一号、加粗、无竖线装饰。
func (c *canvas) sectionTitle(text string) {
	c.drawSectionTitleAt(c.y, text)
	c.y += c.sectionTitleBlockHeight()
}

// infoBox 画信息盒（圆角描边 + 两行「标签 / 值」+ 内部分隔线）。
func (c *canvas) infoBox(rows []InfoRow) {
	rowHeight := c.scale(9.4)
	boxHeight := rowHeight * float64(len(rows))
	if !c.dry {
		c.f.SetFillColor(colorSoftFill.r, colorSoftFill.g, colorSoftFill.b)
		c.f.SetDrawColor(colorBorder.r, colorBorder.g, colorBorder.b)
		c.f.SetLineWidth(c.scale(0.28))
		c.f.RoundedRect(marginLeft, c.y, contentWidth, boxHeight, c.scale(2.2), "1234", "DF")
		for i := 1; i < len(rows); i++ {
			c.hline(marginLeft+c.scale(0.25), contentRight-c.scale(0.25), c.y+rowHeight*float64(i), colorInnerLine, 0.22)
		}
	}
	for i, row := range rows {
		top := c.y + rowHeight*float64(i)
		lineBaseline := baseline(top, rowHeight, 9.6*c.k)
		c.text(marginLeft+c.scale(4.6), lineBaseline, row.Label, false, 9.6, colorMuted)
		c.text(marginLeft+c.scale(32), lineBaseline, row.Value, row.ValueBold, 9.6, colorBody)
	}
	c.y += boxHeight + c.scale(3.4)
}

// productTable 画产品额度表（外框圆角、表头浅紫、行间细分隔线）。
func (c *canvas) productTable(header string, rows []ProductRow, firstColumn string) {
	headerHeight := c.scale(8.8)
	rowHeight := c.scale(9.3)
	tableHeight := headerHeight + rowHeight*float64(len(rows))
	nameColumnWidth := c.scale(38)

	if !c.dry {
		// 表头（上圆角）打底，再铺正文区（下圆角），最后画外框与分隔线。
		c.f.SetFillColor(colorHeaderFill.r, colorHeaderFill.g, colorHeaderFill.b)
		c.f.RoundedRect(marginLeft, c.y, contentWidth, tableHeight, c.scale(2.2), "12", "F")
		c.f.SetFillColor(colorSoftFill.r, colorSoftFill.g, colorSoftFill.b)
		c.f.RoundedRect(marginLeft+c.scale(0.25), c.y+headerHeight, contentWidth-c.scale(0.5),
			tableHeight-headerHeight-c.scale(0.25), c.scale(2.0), "34", "F")

		c.f.SetDrawColor(colorBorder.r, colorBorder.g, colorBorder.b)
		c.f.SetLineWidth(c.scale(0.28))
		c.f.RoundedRect(marginLeft, c.y, contentWidth, tableHeight, c.scale(2.2), "1234", "D")
		c.hline(marginLeft, contentRight, c.y+headerHeight, colorBorder, 0.28)
		for i := 1; i < len(rows); i++ {
			c.hline(marginLeft+c.scale(0.25), contentRight-c.scale(0.25),
				c.y+headerHeight+rowHeight*float64(i), colorInnerLine, 0.22)
		}
	}

	headerBaseline := baseline(c.y, headerHeight, 9*c.k)
	if firstColumn == "" {
		firstColumn = "产品"
	}
	c.text(marginLeft+c.scale(4.6), headerBaseline, firstColumn, false, 9, colorMuted)
	c.text(marginLeft+nameColumnWidth, headerBaseline, header, false, 9, colorMuted)

	for i, row := range rows {
		top := c.y + headerHeight + rowHeight*float64(i)
		rowBaseline := baseline(top, rowHeight, 9.6*c.k)
		c.text(marginLeft+c.scale(4.6), rowBaseline, row.Name, false, 9.6, colorBody)
		c.text(marginLeft+nameColumnWidth, rowBaseline, row.Quota, false, 9.6, colorBody)
	}
	c.y += tableHeight + c.scale(3.4)
}

// numberedList 画带圆形序号的条目列表。
func (c *canvas) numberedList(items []string) {
	badgeRadius := c.scale(2.8)
	textX := marginLeft + c.scale(9.2)
	maxWidth := contentWidth - c.scale(9.2)
	lineHeight := ptToMM(9.6) * 1.75 * c.k

	for index, item := range items {
		lines := c.wrapText(item, 9.6, maxWidth)
		itemHeight := lineHeight * float64(len(lines))
		if itemHeight < badgeRadius*2 {
			itemHeight = badgeRadius * 2
		}
		badgeCenterY := c.y + badgeRadius
		if !c.dry {
			c.f.SetFillColor(colorPrimary.r, colorPrimary.g, colorPrimary.b)
			c.f.Circle(marginLeft+badgeRadius, badgeCenterY, badgeRadius, "F")
		}
		number := fmt.Sprintf("%d", index+1)
		numberWidth := c.textWidth(number, true, 8)
		c.text(marginLeft+badgeRadius-numberWidth/2, badgeCenterY+ptToMM(8)*c.k*0.36, number, true, 8, colorWhite)

		for i, line := range lines {
			c.drawLine(textX, badgeCenterY+ptToMM(9.6)*c.k*0.36+lineHeight*float64(i), line, 9.6, colorBody)
		}
		c.y += itemHeight + c.scale(1.2)
	}
}

// bottomBlock 画页脚上方的顾问卡 + 落款 + 电子章，锚定在正文区底部。
func (c *canvas) bottomBlock(letter Letter) {
	headingHeight := c.sectionTitleBlockHeight()
	cardHeight := c.scale(16)
	cardWidth := c.scale(101)
	blockHeight := headingHeight + cardHeight
	blockTop := c.y + c.scale(8)
	if anchor := contentBottom - blockHeight; blockTop < anchor {
		blockTop = anchor
	}
	// 顾问区的小标题与「开始使用 / 温馨提示」同级，卡片贴着正文区底部。
	c.drawSectionTitleAt(blockTop, letter.advisorSectionTitle())
	cardTop := blockTop + headingHeight

	// 顾问卡：只写姓名 + 电话。
	if !c.dry {
		c.f.SetFillColor(0xfa, 0xf8, 0xff)
		c.f.SetDrawColor(colorBorder.r, colorBorder.g, colorBorder.b)
		c.f.SetLineWidth(c.scale(0.28))
		c.f.RoundedRect(marginLeft, cardTop, cardWidth, cardHeight, c.scale(2.2), "1234", "DF")

		avatarRadius := c.scale(4.6)
		avatarX := marginLeft + c.scale(6.6)
		avatarY := cardTop + cardHeight/2
		c.f.SetFillColor(colorPrimary.r, colorPrimary.g, colorPrimary.b)
		c.f.Circle(avatarX, avatarY, avatarRadius, "F")
	}
	avatarCenterX := marginLeft + c.scale(6.6)
	avatarCenterY := cardTop + cardHeight/2
	if letter.Advisor.IsBrand {
		// 没有具体员工顾问：头像里画灵策品牌标记（两个小字），而不是取名字首字。
		size := 6.4
		gap := c.scale(0.35)
		width := c.textWidth("灵", true, size) + gap + c.textWidth("策", true, size)
		baseline := avatarCenterY + ptToMM(size)*c.k*0.40
		x := avatarCenterX - width/2
		c.text(x, baseline, "灵", true, size, colorWhite)
		c.text(x+c.textWidth("灵", true, size)+gap, baseline, "策", true, size, colorWhite)
	} else {
		initialRunes := []rune(strings.TrimSpace(letter.Advisor.Name))
		initial := "灵"
		if len(initialRunes) > 0 {
			initial = string(initialRunes[0])
		}
		initialWidth := c.textWidth(initial, true, 10)
		// 汉字字面中心在基线上方约 0.40em（Noto Sans SC 实测）：基线放在圆心下方 0.40em，
		// 字面中心才落在圆心上。上下方向用正号（gofpdf 的 y 向下为正）。
		c.text(avatarCenterX-initialWidth/2, avatarCenterY+ptToMM(10)*c.k*0.40, initial, true, 10, colorWhite)
	}
	c.text(marginLeft+c.scale(14.6), cardTop+c.scale(6.8), letter.Advisor.Name, true, 10.5, colorTitle)
	c.text(marginLeft+c.scale(14.6), cardTop+c.scale(12.0), letter.Advisor.Phone, false, 9, colorMuted)

	// 落款：公司名 + 日期，右对齐。
	companyBaseline := cardTop + c.scale(8.2)
	companyWidth := c.textWidth(letter.Company, true, 11)
	c.text(contentRight-companyWidth, companyBaseline, letter.Company, true, 11, colorTitle)
	dateBaseline := companyBaseline + c.scale(4.9)
	dateWidth := c.textWidth(letter.SignDate, false, 9.5)
	c.text(contentRight-dateWidth, dateBaseline, letter.SignDate, false, 9.5, colorMuted)

	// 电子章：红色圆形、半透明，盖在公司名落款的后部（只压住名字尾部）。
	c.seal(contentRight+c.scale(2.5), companyBaseline-c.scale(5.8), c.scale(10), letter.sealLines())

	c.y = blockTop + blockHeight
}

// seal 画电子章：圆环 + 两行字，正立、半透明。
func (c *canvas) seal(centerX, centerY, radius float64, lines []string) {
	if c.dry {
		return
	}
	size := 8.4
	lineHeight := ptToMM(size) * 1.35 * c.k
	totalHeight := lineHeight * float64(len(lines))
	firstBaseline := centerY - totalHeight/2 + lineHeight*0.75

	c.f.TransformBegin()
	c.f.SetAlpha(0.42, "Normal")
	c.f.SetDrawColor(colorSealRed.r, colorSealRed.g, colorSealRed.b)
	c.f.SetLineWidth(c.scale(0.7))
	c.f.Circle(centerX, centerY, radius, "D")
	for i, line := range lines {
		width := c.textWidth(line, true, size)
		c.setFont(true, size)
		c.f.SetTextColor(colorSealRed.r, colorSealRed.g, colorSealRed.b)
		c.f.Text(centerX-width/2, firstBaseline+lineHeight*float64(i), line)
	}
	c.f.SetAlpha(1.0, "Normal")
	c.f.TransformEnd()
}

// drawLine 绘制断行后的一行富文本。
func (c *canvas) drawLine(x, lineBaseline float64, line textLine, size float64, color rgb) {
	cursor := x
	for _, unit := range line.units {
		c.text(cursor, lineBaseline, unit.text, unit.bold, size, color)
		cursor += unit.width
	}
}

// footer 画固定页脚：公司名 + 自动生成说明，上方一条细分隔线。
func (c *canvas) footer(letter Letter) {
	if c.dry {
		return
	}
	c.hline(marginLeft, contentRight, footerRuleY, colorInnerLine, 0.25)
	c.text(marginLeft, footerTextY, letter.FooterLeft, false, 8, colorFooterText)
	width := c.textWidth(letter.FooterRight, false, 8)
	c.text(contentRight-width, footerTextY, letter.FooterRight, false, 8, colorFooterText)
}

// layout 依次排版全部区块，返回内容底部 y（mm）。
func (c *canvas) layout(letter Letter) float64 {
	c.y = contentTop
	c.brand(letter)
	c.title(letter)
	c.salutation(letter)
	c.paragraph(letter.Intro, 10, 1.9)
	c.sectionTitle(letter.InfoSection)
	c.infoBox(letter.InfoRows)
	c.sectionTitle(letter.ProductSection)
	c.productTable(letter.QuotaHeader, letter.Products, letter.ProductColumnLabel)
	c.sectionTitle(letter.StartSection)
	c.numberedList(letter.StartItems)
	c.sectionTitle(letter.TipSection)
	c.numberedList(letter.TipItems)
	c.bottomBlock(letter)
	return c.y
}

// Render 把一份通知函渲染成 PDF 字节流（恒为 A4 单页）。
func Render(letter Letter) ([]byte, error) {
	if err := letter.validate(); err != nil {
		return nil, err
	}

	doc := gofpdf.New("P", "mm", "A4", "")
	doc.SetMargins(0, 0, 0)
	doc.SetAutoPageBreak(false, 0)
	doc.SetCompression(true)
	doc.SetTitle(letter.Title, true)
	doc.SetSubject(letter.DocNo, true)
	doc.SetAuthor(letter.Company, true)
	doc.SetCreator("灵策运营后台", true)
	if err := addFonts(doc); err != nil {
		return nil, err
	}

	// 第一遍：按 1.0 量；放不下就整体缩小重排，直到放进单页。
	scale := 1.0
	for i := 0; i < 20; i++ {
		measure := newCanvas(doc, scale, true)
		if measure.layout(letter) <= contentBottom+0.01 {
			break
		}
		scale -= 0.02
		if scale <= minScale {
			scale = minScale
			break
		}
	}

	// 第二遍：正式绘制。
	doc.AddPage()
	draw := newCanvas(doc, scale, false)
	contentEnd := draw.layout(letter)
	draw.footer(letter)
	if contentEnd > contentBottom+0.5 {
		return nil, fmt.Errorf("%w（内容底部 %.1fmm，可用 %.1fmm）", ErrContentOverflow, contentEnd, contentBottom)
	}
	if doc.PageNo() != 1 {
		return nil, fmt.Errorf("%w（页数 %d）", ErrContentOverflow, doc.PageNo())
	}

	var buffer bytes.Buffer
	if err := doc.Output(&buffer); err != nil {
		return nil, fmt.Errorf("billing pdf: 输出 PDF 失败: %w", err)
	}
	data := buffer.Bytes()
	if !bytes.HasPrefix(data, []byte("%PDF-")) {
		return nil, errors.New("billing pdf: 输出不是合法 PDF")
	}
	return data, nil
}
