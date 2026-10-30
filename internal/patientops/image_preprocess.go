package patientops

import (
	"bytes"
	"encoding/base64"
	"fmt"
	"image"
	"image/color"
	"image/draw"
	"image/jpeg"
	_ "image/png"
	"strings"

	xdraw "golang.org/x/image/draw"
)

const (
	screenshotTargetWidth = 900
	screenshotJPEGQuality = 82
	screenshotEdgeMaxRate = 0.10
	screenshotEdgeBgTol   = 14
	screenshotEdgeBgRatio = 0.985
)

type screenshotPreprocessResult struct {
	DataURL         string
	OriginalWidth   int
	OriginalHeight  int
	ProcessedWidth  int
	ProcessedHeight int
	OriginalBytes   int
	ProcessedBytes  int
	Cropped         bool
	Resized         bool
}

// preprocessScreenshotDataURL 在 API 层本地处理截图，不改变原始 Base64。
// 规则：只裁掉边缘空白/纯色区域，按宽度缩放（不按长边），再压成 JPEG。
func preprocessScreenshotDataURL(raw string) (*screenshotPreprocessResult, error) {
	header, payload, ok := strings.Cut(strings.TrimSpace(raw), ",")
	if !ok || !strings.HasPrefix(header, "data:image/") || !strings.Contains(header, ";base64") {
		return nil, fmt.Errorf("unsupported screenshot data url")
	}
	decoded, err := base64.StdEncoding.DecodeString(payload)
	if err != nil {
		return nil, fmt.Errorf("decode screenshot base64: %w", err)
	}
	src, _, err := image.Decode(bytes.NewReader(decoded))
	if err != nil {
		return nil, fmt.Errorf("decode screenshot image: %w", err)
	}
	bounds := src.Bounds()
	originalW, originalH := bounds.Dx(), bounds.Dy()
	if originalW <= 0 || originalH <= 0 {
		return nil, fmt.Errorf("empty screenshot image")
	}

	rgba := image.NewNRGBA(image.Rect(0, 0, originalW, originalH))
	draw.Draw(rgba, rgba.Bounds(), src, bounds.Min, draw.Src)

	cropped, cropRect := trimScreenshotEdges(rgba)
	work := rgba
	if cropped {
		work = image.NewNRGBA(image.Rect(0, 0, cropRect.Dx(), cropRect.Dy()))
		draw.Draw(work, work.Bounds(), rgba, cropRect.Min, draw.Src)
	}

	targetW := screenshotTargetWidth
	if targetW > work.Bounds().Dx() {
		targetW = work.Bounds().Dx()
	}
	targetH := work.Bounds().Dy() * targetW / work.Bounds().Dx()
	if targetH <= 0 {
		targetH = work.Bounds().Dy()
	}
	resized := targetW != work.Bounds().Dx()
	if resized {
		dst := image.NewNRGBA(image.Rect(0, 0, targetW, targetH))
		xdraw.CatmullRom.Scale(dst, dst.Bounds(), work, work.Bounds(), draw.Over, nil)
		work = dst
	}

	var out bytes.Buffer
	if err := jpeg.Encode(&out, work, &jpeg.Options{Quality: screenshotJPEGQuality}); err != nil {
		return nil, fmt.Errorf("encode screenshot jpeg: %w", err)
	}
	if out.Len() == 0 {
		return nil, fmt.Errorf("empty processed screenshot")
	}
	return &screenshotPreprocessResult{
		DataURL:         "data:image/jpeg;base64," + base64.StdEncoding.EncodeToString(out.Bytes()),
		OriginalWidth:   originalW,
		OriginalHeight:  originalH,
		ProcessedWidth:  work.Bounds().Dx(),
		ProcessedHeight: work.Bounds().Dy(),
		OriginalBytes:   len(decoded),
		ProcessedBytes:  out.Len(),
		Cropped:         cropped,
		Resized:         resized,
	}, nil
}

// trimScreenshotEdges 只裁掉与四角背景色一致、连续且占比极高的边缘区域，
// 避免切掉聊天内容；最多裁掉每边 10%。
func trimScreenshotEdges(img *image.NRGBA) (bool, image.Rectangle) {
	w, h := img.Bounds().Dx(), img.Bounds().Dy()
	if w < 20 || h < 20 {
		return false, img.Bounds()
	}
	bg := averageCornerColor(img)
	top, bottom := 0, h
	left, right := 0, w
	maxY := int(float64(h) * screenshotEdgeMaxRate)
	maxX := int(float64(w) * screenshotEdgeMaxRate)
	for top < maxY && rowMatchesBackground(img, top, bg) {
		top++
	}
	for bottom > h-maxY && rowMatchesBackground(img, bottom-1, bg) {
		bottom--
	}
	for left < maxX && colMatchesBackground(img, left, bg) {
		left++
	}
	for right > w-maxX && colMatchesBackground(img, right-1, bg) {
		right--
	}
	if top == 0 && bottom == h && left == 0 && right == w {
		return false, img.Bounds()
	}
	if right-left < 20 || bottom-top < 20 {
		return false, img.Bounds()
	}
	return true, image.Rect(left, top, right, bottom)
}

func averageCornerColor(img *image.NRGBA) color.NRGBA {
	w, h := img.Bounds().Dx(), img.Bounds().Dy()
	points := [][2]int{{0, 0}, {w - 1, 0}, {0, h - 1}, {w - 1, h - 1}}
	var r, g, b, a int
	for _, p := range points {
		c := img.NRGBAAt(p[0], p[1])
		r += int(c.R)
		g += int(c.G)
		b += int(c.B)
		a += int(c.A)
	}
	return color.NRGBA{uint8(r / 4), uint8(g / 4), uint8(b / 4), uint8(a / 4)}
}

func rowMatchesBackground(img *image.NRGBA, y int, bg color.NRGBA) bool {
	w := img.Bounds().Dx()
	match := 0
	for x := 0; x < w; x++ {
		if nearColor(img.NRGBAAt(x, y), bg) {
			match++
		}
	}
	return float64(match)/float64(w) >= screenshotEdgeBgRatio
}

func colMatchesBackground(img *image.NRGBA, x int, bg color.NRGBA) bool {
	h := img.Bounds().Dy()
	match := 0
	for y := 0; y < h; y++ {
		if nearColor(img.NRGBAAt(x, y), bg) {
			match++
		}
	}
	return float64(match)/float64(h) >= screenshotEdgeBgRatio
}

func nearColor(a, b color.NRGBA) bool {
	return absInt(int(a.R)-int(b.R)) <= screenshotEdgeBgTol &&
		absInt(int(a.G)-int(b.G)) <= screenshotEdgeBgTol &&
		absInt(int(a.B)-int(b.B)) <= screenshotEdgeBgTol
}

func absInt(v int) int {
	if v < 0 {
		return -v
	}
	return v
}
