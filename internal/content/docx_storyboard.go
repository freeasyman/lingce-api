package content

import (
	"fmt"
	"strings"
)

type docxStoryboardShot struct {
	Scene       string   `json:"scene"`
	Cast        string   `json:"cast"`
	ShotSize    string   `json:"shot_size"`
	Camera      string   `json:"camera"`
	Visual      string   `json:"visual"`
	Action      string   `json:"action"`
	Dialogue    string   `json:"dialogue"`
	Tone        string   `json:"tone"`
	Subtitle    string   `json:"subtitle"`
	KineticText string   `json:"kinetic_text"`
	Transition  string   `json:"transition"`
	Duration    int      `json:"duration"`
	Materials   []string `json:"materials"`
}

type docxStoryboardChapter struct {
	Title string               `json:"title"`
	Shots []docxStoryboardShot `json:"shots"`
}

type docxMaterial struct {
	Item   string `json:"item"`
	Status string `json:"status"`
}

// buildStoryboardDocx 分镜 → 带样式的 Word（三块：全局剪辑说明 / 分镜表 / 备货清单）。
func buildStoryboardDocx(title string, globalEditing map[string]any, chapters []docxStoryboardChapter, checklist []docxMaterial, pkg map[string]any) ([]byte, error) {
	var doc strings.Builder
	doc.WriteString(`<?xml version="1.0" encoding="UTF-8" standalone="yes"?>
<w:document xmlns:w="http://schemas.openxmlformats.org/wordprocessingml/2006/main"><w:body>`)

	doc.WriteString(docxRunPara(title, true, 40, "111827"))
	doc.WriteString("<w:p/>")

	// 第 1 块：全局剪辑说明
	doc.WriteString(docxRunPara("一、全局剪辑说明", true, 30, "7C3AED"))
	ge := map[string]string{
		"intro": "片头", "outro": "片尾", "bgm": "BGM/音效", "color_grade": "调色风格",
		"subtitle_style": "字幕系统", "kinetic_style": "花字系统", "brand": "机构标识", "pacing": "整片节奏",
	}
	for _, key := range []string{"intro", "outro", "bgm", "color_grade", "subtitle_style", "kinetic_style", "brand", "pacing"} {
		if v, ok := globalEditing[key].(string); ok && strings.TrimSpace(v) != "" {
			doc.WriteString(docxRunPara(ge[key]+"："+v, false, 21, "374151"))
		}
	}
	doc.WriteString("<w:p/>")

	// 第 2 块：分镜表
	doc.WriteString(docxRunPara("二、分镜表", true, 30, "7C3AED"))
	shotNo := 0
	for ci, ch := range chapters {
		doc.WriteString(docxRunPara(fmt.Sprintf("第 %d 章　%s", ci+1, strings.TrimSpace(ch.Title)), true, 26, "2563EB"))
		for _, s := range ch.Shots {
			shotNo++
			head := fmt.Sprintf("第 %d 镜　%s · %s · %s · %s · %d 秒", shotNo, s.Scene, s.Cast, s.ShotSize, s.Camera, s.Duration)
			if strings.TrimSpace(s.Transition) != "" {
				head += "　→ " + strings.TrimSpace(s.Transition)
			}
			doc.WriteString(docxRunPara(head, true, 22, "111827"))
			if strings.TrimSpace(s.Dialogue) != "" {
				doc.WriteString(docxRunPara("台词："+s.Dialogue, false, 22, ""))
			}
			if strings.TrimSpace(s.Visual) != "" {
				doc.WriteString(docxRunPara("画面："+s.Visual, false, 20, "6B7280"))
			}
			if strings.TrimSpace(s.Action) != "" {
				doc.WriteString(docxRunPara("动作："+s.Action, false, 20, "6B7280"))
			}
			if strings.TrimSpace(s.Tone) != "" {
				doc.WriteString(docxRunPara("语气："+s.Tone, false, 20, "0369A1"))
			}
			if strings.TrimSpace(s.Subtitle) != "" {
				doc.WriteString(docxRunPara("字幕："+s.Subtitle, false, 20, "374151"))
			}
			if strings.TrimSpace(s.KineticText) != "" {
				doc.WriteString(docxRunPara("花字："+s.KineticText, true, 20, "BE123C"))
			}
			if len(s.Materials) > 0 {
				doc.WriteString(docxRunPara("素材："+strings.Join(s.Materials, "、"), false, 20, "B45309"))
			}
			doc.WriteString("<w:p/>")
		}
	}
	doc.WriteString("<w:p/>")

	// 第 3 块：备货清单
	doc.WriteString(docxRunPara("三、拍前备货总清单", true, 30, "7C3AED"))
	for _, m := range checklist {
		color := "6B7280"
		if m.Status == "需准备" {
			color = "B45309"
		}
		doc.WriteString(docxRunPara(fmt.Sprintf("【%s】%s", m.Status, m.Item), false, 21, color))
	}
	doc.WriteString("<w:p/>")

	doc.WriteString(`<w:sectPr><w:pgSz w:w="11906" w:h="16838"/><w:pgMar w:top="1440" w:right="1440" w:bottom="1440" w:left="1440"/></w:sectPr></w:body></w:document>`)

	return zipSimpleDocx(doc.String())
}
