package content

import (
	"archive/zip"
	"bytes"
	"fmt"
	"strings"
)

// docxCard 是导出 docx 时的一张卡片。
type docxCard struct {
	CardTitle string `json:"card_title"`
	Body      string `json:"body"`
}

// buildDocx 用最小 WordprocessingML 生成 docx（标题 + 卡片正文 + 发布包），纯标准库无依赖。
func buildDocx(title string, cards []docxCard, publishPackage map[string]any) ([]byte, error) {
	buf := new(bytes.Buffer)
	zw := zip.NewWriter(buf)

	contentTypes := `<?xml version="1.0" encoding="UTF-8" standalone="yes"?>
<Types xmlns="http://schemas.openxmlformats.org/package/2006/content-types">
<Default Extension="rels" ContentType="application/vnd.openxmlformats-package.relationships+xml"/>
<Default Extension="xml" ContentType="application/xml"/>
<Override PartName="/word/document.xml" ContentType="application/vnd.openxmlformats-officedocument.wordprocessingml.document.main+xml"/>
</Types>`

	rels := `<?xml version="1.0" encoding="UTF-8" standalone="yes"?>
<Relationships xmlns="http://schemas.openxmlformats.org/package/2006/relationships">
<Relationship Id="rId1" Type="http://schemas.openxmlformats.org/officeDocument/2006/relationships/officeDocument" Target="word/document.xml"/>
</Relationships>`

	var doc strings.Builder
	doc.WriteString(`<?xml version="1.0" encoding="UTF-8" standalone="yes"?>
<w:document xmlns:w="http://schemas.openxmlformats.org/wordprocessingml/2006/main"><w:body>`)

	// 大标题
	doc.WriteString(docxPara(title, true, 36))
	doc.WriteString(docxPara("", false, 0))

	for i, c := range cards {
		head := strings.TrimSpace(c.CardTitle)
		if head == "" {
			doc.WriteString(docxPara(fmt.Sprintf("卡 %d", i+1), true, 28))
		} else {
			doc.WriteString(docxPara(fmt.Sprintf("卡 %d：%s", i+1, head), true, 28))
		}
		for _, line := range splitLines(c.Body) {
			doc.WriteString(docxPara(line, false, 0))
		}
		doc.WriteString(docxPara("", false, 0))
	}

	// 发布包
	if len(publishPackage) > 0 {
		doc.WriteString(docxPara("发布包", true, 28))
		if v, ok := publishPackage["copy"].(string); ok && v != "" {
			doc.WriteString(docxPara("发布文案："+v, false, 0))
		}
		if tags, ok := publishPackage["tags"].([]any); ok && len(tags) > 0 {
			ss := make([]string, 0, len(tags))
			for _, t := range tags {
				if s, ok := t.(string); ok {
					ss = append(ss, s)
				}
			}
			if len(ss) > 0 {
				doc.WriteString(docxPara("标签："+strings.Join(ss, " "), false, 0))
			}
		}
		for _, key := range []struct{ k, label string }{
			{"pinned_comment", "置顶评论"},
			{"cta", "CTA"},
			{"publish_time", "发布时间建议"},
		} {
			if v, ok := publishPackage[key.k].(string); ok && v != "" {
				doc.WriteString(docxPara(key.label+"："+v, false, 0))
			}
		}
	}

	doc.WriteString(`</w:body></w:document>`)

	files := []struct{ name, content string }{
		{"[Content_Types].xml", contentTypes},
		{"_rels/.rels", rels},
		{"word/document.xml", doc.String()},
	}
	for _, f := range files {
		w, err := zw.Create(f.name)
		if err != nil {
			return nil, err
		}
		if _, err := w.Write([]byte(f.content)); err != nil {
			return nil, err
		}
	}
	if err := zw.Close(); err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}

// docxPara 生成一个段落；bold 加粗，sizeHalfPt 为半磅字号（0 表示默认）。
func docxPara(text string, bold bool, sizeHalfPt int) string {
	runPr := ""
	if bold || sizeHalfPt > 0 {
		var b strings.Builder
		b.WriteString("<w:rPr>")
		if bold {
			b.WriteString("<w:b/>")
		}
		if sizeHalfPt > 0 {
			b.WriteString(fmt.Sprintf(`<w:sz w:val="%d"/>`, sizeHalfPt))
		}
		b.WriteString("</w:rPr>")
		runPr = b.String()
	}
	if text == "" {
		return "<w:p/>"
	}
	return fmt.Sprintf(`<w:p><w:r>%s<w:t xml:space="preserve">%s</w:t></w:r></w:p>`, runPr, escapeXML(text))
}

func splitLines(s string) []string {
	s = strings.ReplaceAll(s, "\r\n", "\n")
	lines := strings.Split(s, "\n")
	out := make([]string, 0, len(lines))
	for _, l := range lines {
		out = append(out, strings.TrimSpace(l))
	}
	return out
}

func escapeXML(s string) string {
	s = strings.ReplaceAll(s, "&", "&amp;")
	s = strings.ReplaceAll(s, "<", "&lt;")
	s = strings.ReplaceAll(s, ">", "&gt;")
	s = strings.ReplaceAll(s, `"`, "&quot;")
	return s
}
