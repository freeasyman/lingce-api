package pdf

// 中文字体：Noto Sans SC（Regular + Bold），SIL Open Font License 1.1。
//
// 字体文件已裁到「GB2312 全集 + ASCII + Latin-1 + 中文标点」（7,626 字符）并转成
// TrueType 轮廓，单个约 2.2MB。用 //go:embed 编进二进制，部署时不依赖任何外部文件、
// 也不需要系统字体；PDF 里再做二次子集嵌入（只嵌实际用到的字），单份通知函约几百 KB。
//
// 许可证原文见 fonts/OFL.txt（OFL 1.1 要求随字体一起保留）。

import (
	_ "embed"

	"github.com/jung-kurt/gofpdf"
)

const (
	// fontFamily 是注册到 gofpdf 的字体族名；正文用空 style，加粗用 "B"。
	fontFamily = "notosc"
)

//go:embed fonts/NotoSansSC-Regular.ttf
var regularTTF []byte

//go:embed fonts/NotoSansSC-Bold.ttf
var boldTTF []byte

// addFonts 把两个内嵌字体注册到文档实例。
//
// 字体解析失败时 gofpdf 只往 stdout 打印，不会设置 Err()，所以这里在注册后用
// 一次 SetFont + 量字宽兜底：量不出宽度就说明字体没注册成功，直接报错，避免生成乱码 PDF。
func addFonts(f *gofpdf.Fpdf) error {
	f.AddUTF8FontFromBytes(fontFamily, "", regularTTF)
	f.AddUTF8FontFromBytes(fontFamily, "B", boldTTF)
	f.SetFont(fontFamily, "", 10)
	if f.Err() {
		return ErrFontUnavailable
	}
	if width := f.GetStringWidth("灵策通知函"); width <= 0 {
		return ErrFontUnavailable
	}
	return nil
}
