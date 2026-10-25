package content

import (
	"archive/zip"
	"bytes"
	"fmt"
	"strings"
)

// docxScriptSegment 口播的一段（与 content_script_gen 的 schema 对应）。
type docxScriptSegment struct {
	Phase       string `json:"phase"`
	Words       string `json:"words"`
	Action      string `json:"action"`
	Tone        string `json:"tone"`
	Subtitle    string `json:"subtitle"`
	KineticText string `json:"kinetic_text"`
	Transition  string `json:"transition"`
	Evidence    string `json:"evidence"`
}

// docxRunPara 生成一个可带颜色/加粗/字号的段落（color 为 "RRGGBB"，空则不设色）。
func docxRunPara(text string, bold bool, sizeHalfPt int, color string) string {
	if strings.TrimSpace(text) == "" {
		return "<w:p/>"
	}
	var pr strings.Builder
	pr.WriteString("<w:rPr>")
	if bold {
		pr.WriteString("<w:b/><w:bCs/>")
	}
	if sizeHalfPt > 0 {
		pr.WriteString(fmt.Sprintf(`<w:sz w:val="%d"/><w:szCs w:val="%d"/>`, sizeHalfPt, sizeHalfPt))
	}
	if color != "" {
		pr.WriteString(fmt.Sprintf(`<w:color w:val="%s"/>`, color))
	}
	pr.WriteString("</w:rPr>")
	return fmt.Sprintf(`<w:p><w:r>%s<w:t xml:space="preserve">%s</w:t></w:r></w:p>`, pr.String(), escapeXML(text))
}

// buildScriptDocx 口播稿 → 带样式的 Word。
// mode: recite（纯念词稿）/ full（完整方案）/ edit（剪辑指引）
func buildScriptDocx(title, mode string, cover map[string]any, segs []docxScriptSegment, bgm, brand string, pkg map[string]any) ([]byte, error) {
	var doc strings.Builder
	doc.WriteString(`<?xml version="1.0" encoding="UTF-8" standalone="yes"?>
<w:document xmlns:w="http://schemas.openxmlformats.org/wordprocessingml/2006/main"><w:body>`)

	doc.WriteString(docxRunPara(title, true, 40, "111827"))
	doc.WriteString("<w:p/>")

	if mode != "recite" {
		if v, ok := cover["title"].(string); ok && strings.TrimSpace(v) != "" {
			doc.WriteString(docxRunPara("封面标题："+v, true, 26, "2563EB"))
		}
		if v, ok := cover["visual"].(string); ok && strings.TrimSpace(v) != "" {
			doc.WriteString(docxRunPara("首帧画面："+v, false, 22, "6B7280"))
		}
		doc.WriteString("<w:p/>")
	}

	for i, s := range segs {
		if mode == "recite" {
			doc.WriteString(docxRunPara(s.Words, false, 26, ""))
			doc.WriteString("<w:p/>")
			continue
		}
		head := fmt.Sprintf("第 %d 段", i+1)
		if strings.TrimSpace(s.Transition) != "" {
			head += "   → " + strings.TrimSpace(s.Transition)
		}
		doc.WriteString(docxRunPara(head, true, 28, "7C3AED"))
		doc.WriteString(docxRunPara(s.Words, true, 24, ""))
		if v := strings.TrimSpace(s.Tone); v != "" {
			doc.WriteString(docxRunPara("语气："+v, false, 21, "0369A1"))
		}
		if v := strings.TrimSpace(s.Action); v != "" {
			doc.WriteString(docxRunPara("表情手势："+v, false, 21, "6B7280"))
		}
		if v := strings.TrimSpace(s.Subtitle); v != "" {
			doc.WriteString(docxRunPara("字幕："+v, false, 21, "374151"))
		}
		if v := strings.TrimSpace(s.KineticText); v != "" {
			doc.WriteString(docxRunPara("花字："+v, true, 21, "BE123C"))
		}
		if v := strings.TrimSpace(s.Evidence); v != "" {
			doc.WriteString(docxRunPara("包装素材："+v, false, 21, "B45309"))
		}
		doc.WriteString("<w:p/>")
	}

	if mode != "recite" {
		if strings.TrimSpace(bgm) != "" {
			doc.WriteString(docxRunPara("BGM / 音效："+bgm, false, 21, "6B7280"))
		}
		if strings.TrimSpace(brand) != "" {
			doc.WriteString(docxRunPara("机构标识："+brand, false, 21, "6B7280"))
		}
		if mode == "edit" && len(pkg) > 0 {
			doc.WriteString("<w:p/>")
			doc.WriteString(docxRunPara("发布包", true, 24, "7C3AED"))
			if v, _ := pkg["copy"].(string); strings.TrimSpace(v) != "" {
				doc.WriteString(docxRunPara("发布文案："+v, false, 21, "374151"))
			}
			if v, _ := pkg["pinned_comment"].(string); strings.TrimSpace(v) != "" {
				doc.WriteString(docxRunPara("置顶评论："+v, false, 21, "374151"))
			}
		}
	}

	doc.WriteString(`<w:sectPr><w:pgSz w:w="11906" w:h="16838"/><w:pgMar w:top="1440" w:right="1440" w:bottom="1440" w:left="1440"/></w:sectPr></w:body></w:document>`)

	return zipSimpleDocx(doc.String())
}

// zipSimpleDocx 打包成最小可用的 docx（纯文本，无图片）。
func zipSimpleDocx(documentXML string) ([]byte, error) {
	buf := new(bytes.Buffer)
	zw := zip.NewWriter(buf)
	write := func(name, body string) error {
		w, err := zw.Create(name)
		if err != nil {
			return err
		}
		_, err = w.Write([]byte(body))
		return err
	}
	contentTypes := `<?xml version="1.0" encoding="UTF-8" standalone="yes"?>
<Types xmlns="http://schemas.openxmlformats.org/package/2006/content-types"><Default Extension="rels" ContentType="application/vnd.openxmlformats-package.relationships+xml"/><Default Extension="xml" ContentType="application/xml"/><Override PartName="/word/document.xml" ContentType="application/vnd.openxmlformats-officedocument.wordprocessingml.document.main+xml"/></Types>`
	rels := `<?xml version="1.0" encoding="UTF-8" standalone="yes"?>
<Relationships xmlns="http://schemas.openxmlformats.org/package/2006/relationships"><Relationship Id="rId1" Type="http://schemas.openxmlformats.org/officeDocument/2006/relationships/officeDocument" Target="word/document.xml"/></Relationships>`
	if err := write("[Content_Types].xml", contentTypes); err != nil {
		return nil, err
	}
	if err := write("_rels/.rels", rels); err != nil {
		return nil, err
	}
	if err := write("word/document.xml", documentXML); err != nil {
		return nil, err
	}
	if err := zw.Close(); err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}
