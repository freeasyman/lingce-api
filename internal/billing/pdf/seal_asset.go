package pdf

import (
	"bytes"
	_ "embed"

	"github.com/jung-kurt/gofpdf"
)

//go:embed assets/seal.png
var sealPNG []byte

const sealImageName = "lingce-seal"

// sealImage 把实拍公章（已抠图、透明底）盖到指定圆心和直径的位置。
func (c *canvas) sealImage(centerX, centerY, diameter float64) {
	// 测量阶段还没有 AddPage，不能绘制图片，否则会写坏文档状态。
	if c.dry {
		return
	}
	if c.f.GetImageInfo(sealImageName) == nil {
		c.f.RegisterImageOptionsReader(sealImageName, gofpdf.ImageOptions{ImageType: "PNG"}, bytes.NewReader(sealPNG))
	}
	c.f.ImageOptions(sealImageName, centerX-diameter/2, centerY-diameter/2, diameter, diameter, false,
		gofpdf.ImageOptions{ImageType: "PNG", AllowNegativePosition: true}, 0, "")
}
