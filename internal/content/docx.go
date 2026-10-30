package content

import (
	"archive/zip"
	"bytes"
	"fmt"
	"image"
	_ "image/gif"
	_ "image/jpeg"
	_ "image/png"
	"io"
	"net/http"
	"strings"
)

// docxCard 是导出 docx 时的一张卡片。
type docxCard struct {
	CardTitle string `json:"card_title"`
	Body      string `json:"body"`
}

type docxMedia struct {
	Name      string
	Ext       string
	Data      []byte
	RelID     string
	WidthEMU  int
	HeightEMU int
}

const emuPerPx = 9525           // 96 DPI
const docMaxWidthEMU = 5029200  // 5.5 inch
const docMaxHeightEMU = 6858000 // 7.5 inch

// buildDocx 用最小 WordprocessingML 生成 docx（标题 + 卡片正文 + 卡片图 + 发布包）。
func buildDocx(title string, cards []docxCard, publishPackage map[string]any, imageURLs []string) ([]byte, error) {
	medias := fetchMedias(imageURLs)

	buf := new(bytes.Buffer)
	zw := zip.NewWriter(buf)

	var defaultTypes strings.Builder
	defaultTypes.WriteString(`<Default Extension="rels" ContentType="application/vnd.openxmlformats-package.relationships+xml"/><Default Extension="xml" ContentType="application/xml"/>`)
	seenExt := map[string]bool{}
	for _, m := range medias {
		if !seenExt[m.Ext] {
			seenExt[m.Ext] = true
			defaultTypes.WriteString(fmt.Sprintf(`<Default Extension="%s" ContentType="image/%s"/>`, m.Ext, m.Ext))
		}
	}
	contentTypes := `<?xml version="1.0" encoding="UTF-8" standalone="yes"?>
<Types xmlns="http://schemas.openxmlformats.org/package/2006/content-types">` + defaultTypes.String() + `
<Override PartName="/word/document.xml" ContentType="application/vnd.openxmlformats-officedocument.wordprocessingml.document.main+xml"/>
</Types>`

	rels := `<?xml version="1.0" encoding="UTF-8" standalone="yes"?>
<Relationships xmlns="http://schemas.openxmlformats.org/package/2006/relationships">
<Relationship Id="rId1" Type="http://schemas.openxmlformats.org/officeDocument/2006/relationships/officeDocument" Target="word/document.xml"/>
</Relationships>`

	var docRels strings.Builder
	docRels.WriteString(`<?xml version="1.0" encoding="UTF-8" standalone="yes"?>
<Relationships xmlns="http://schemas.openxmlformats.org/package/2006/relationships">`)
	for _, m := range medias {
		docRels.WriteString(fmt.Sprintf(`<Relationship Id="%s" Type="http://schemas.openxmlformats.org/officeDocument/2006/relationships/image" Target="media/%s"/>`, m.RelID, m.Name))
	}
	docRels.WriteString(`</Relationships>`)

	var doc strings.Builder
	doc.WriteString(`<?xml version="1.0" encoding="UTF-8" standalone="yes"?>
<w:document xmlns:w="http://schemas.openxmlformats.org/wordprocessingml/2006/main" xmlns:r="http://schemas.openxmlformats.org/officeDocument/2006/relationships" xmlns:wp="http://schemas.openxmlformats.org/drawingml/2006/wordprocessingDrawing" xmlns:a="http://schemas.openxmlformats.org/drawingml/2006/main" xmlns:pic="http://schemas.openxmlformats.org/drawingml/2006/picture"><w:body>`)

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
		if i < len(medias) && len(medias[i].Data) > 0 {
			doc.WriteString(docxImagePara(medias[i], i+1))
		}
		doc.WriteString(docxPara("", false, 0))
	}

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
		{"word/_rels/document.xml.rels", docRels.String()},
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
	for _, m := range medias {
		w, err := zw.Create("word/media/" + m.Name)
		if err != nil {
			return nil, err
		}
		if _, err := w.Write(m.Data); err != nil {
			return nil, err
		}
	}
	if err := zw.Close(); err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}

// fetchMedias 下载图片并按页面宽度缩放，失败的单张跳过。
func fetchMedias(urls []string) []docxMedia {
	var out []docxMedia
	client := &http.Client{}
	idx := 0
	for _, u := range urls {
		u = strings.TrimSpace(u)
		if u == "" {
			continue
		}
		data, err := fetchImageBytes(client, u)
		if err != nil {
			continue
		}
		w, h, err := imageDims(data)
		if err != nil || w == 0 || h == 0 {
			continue
		}
		idx++
		cx := w * emuPerPx
		cy := h * emuPerPx
		if cx > docMaxWidthEMU {
			r := float64(docMaxWidthEMU) / float64(cx)
			cx = docMaxWidthEMU
			cy = int(float64(cy) * r)
		}
		if cy > docMaxHeightEMU {
			r := float64(docMaxHeightEMU) / float64(cy)
			cy = docMaxHeightEMU
			cx = int(float64(cx) * r)
		}
		ext := detectImageExt(data)
		out = append(out, docxMedia{
			Name:      fmt.Sprintf("image%d.%s", idx, ext),
			Ext:       ext,
			Data:      data,
			RelID:     fmt.Sprintf("rIdImg%d", idx),
			WidthEMU:  cx,
			HeightEMU: cy,
		})
	}
	return out
}

func fetchImageBytes(client *http.Client, url string) ([]byte, error) {
	req, err := http.NewRequest(http.MethodGet, url, nil)
	if err != nil {
		return nil, err
	}
	resp, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("image status %d", resp.StatusCode)
	}
	return io.ReadAll(io.LimitReader(resp.Body, 20<<20))
}

func imageDims(data []byte) (int, int, error) {
	cfg, _, err := image.DecodeConfig(bytes.NewReader(data))
	if err != nil {
		return 0, 0, err
	}
	return cfg.Width, cfg.Height, nil
}

func detectImageExt(data []byte) string {
	if len(data) > 3 && data[0] == 0xFF && data[1] == 0xD8 {
		return "jpeg"
	}
	if len(data) > 6 && data[0] == 'G' && data[1] == 'I' && data[2] == 'F' {
		return "gif"
	}
	return "png"
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

// docxImagePara 生成一个内嵌图片段落。
func docxImagePara(m docxMedia, id int) string {
	return fmt.Sprintf(`<w:p><w:r><w:drawing><wp:inline distT="0" distB="0" distL="0" distR="0">`+
		`<wp:extent cx="%d" cy="%d"/><wp:docPr id="%d" name="Picture %d"/>`+
		`<a:graphic><a:graphicData uri="http://schemas.openxmlformats.org/drawingml/2006/picture"><pic:pic>`+
		`<pic:nvPicPr><pic:cNvPr id="%d" name="%s"/><pic:cNvPicPr/></pic:nvPicPr>`+
		`<pic:blipFill><a:blip r:embed="%s"/><a:stretch><a:fillRect/></a:stretch></pic:blipFill>`+
		`<pic:spPr><a:xfrm><a:off x="0" y="0"/><a:ext cx="%d" cy="%d"/></a:xfrm><a:prstGeom prst="rect"><a:avLst/></a:prstGeom></pic:spPr>`+
		`</pic:pic></a:graphicData></a:graphic></wp:inline></w:drawing></w:r></w:p>`,
		m.WidthEMU, m.HeightEMU, id, id, id, m.Name, m.RelID, m.WidthEMU, m.HeightEMU)
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
