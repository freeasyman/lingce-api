package captcha

import (
	"crypto/rand"
	"image"
	"image/color"
	"image/draw"
	"math/big"

	"golang.org/x/image/font"
	"golang.org/x/image/font/basicfont"
	"golang.org/x/image/math/fixed"
)

const (
	Width  = 160
	Height = 52
	Length = 4
)

// Generate creates a new captcha code and image
func Generate() (string, *image.RGBA, error) {
	code := generateCode(Length)
	img := createImage(code)
	return code, img, nil
}

// generateCode generates a random alphanumeric code
func generateCode(length int) string {
	const charset = "23456789ABCDEFGHJKLMNPQRSTUVWXYZ" // Exclude confusing characters
	code := make([]byte, length)
	for i := range code {
		n, _ := rand.Int(rand.Reader, big.NewInt(int64(len(charset))))
		code[i] = charset[n.Int64()]
	}
	return string(code)
}

// createImage creates an image with the captcha code
func createImage(code string) *image.RGBA {
	img := image.NewRGBA(image.Rect(0, 0, Width, Height))

	// Fill background
	draw.Draw(img, img.Bounds(), &image.Uniform{color.White}, image.Point{}, draw.Src)

	// Add light noise lines while keeping readability.
	addNoiseLines(img, 3)

	// Draw text
	drawText(img, code)

	return img
}

// addNoiseLines adds random lines to the image
func addNoiseLines(img *image.RGBA, count int) {
	for i := 0; i < count; i++ {
		x1, _ := rand.Int(rand.Reader, big.NewInt(Width))
		y1, _ := rand.Int(rand.Reader, big.NewInt(Height))
		x2, _ := rand.Int(rand.Reader, big.NewInt(Width))
		y2, _ := rand.Int(rand.Reader, big.NewInt(Height))

		drawLine(img, int(x1.Int64()), int(y1.Int64()), int(x2.Int64()), int(y2.Int64()),
			color.RGBA{225, 225, 225, 255})
	}
}

// drawLine draws a line on the image
func drawLine(img *image.RGBA, x1, y1, x2, y2 int, c color.Color) {
	dx := abs(x2 - x1)
	dy := abs(y2 - y1)
	sx, sy := 1, 1
	if x1 >= x2 {
		sx = -1
	}
	if y1 >= y2 {
		sy = -1
	}
	err := dx - dy

	for {
		img.Set(x1, y1, c)
		if x1 == x2 && y1 == y2 {
			break
		}
		e2 := err * 2
		if e2 > -dy {
			err -= dy
			x1 += sx
		}
		if e2 < dx {
			err += dx
			y1 += sy
		}
	}
}

// drawText draws the captcha text on the image
func drawText(img *image.RGBA, text string) {
	startX := 12
	baseY := 10
	step := 36
	textColor := color.RGBA{30, 30, 30, 255}
	scale := 2

	for i, char := range text {
		jitterX, _ := rand.Int(rand.Reader, big.NewInt(3)) // 0..2
		jitterY, _ := rand.Int(rand.Reader, big.NewInt(3)) // 0..2
		x := startX + i*step + int(jitterX.Int64())
		y := baseY + int(jitterY.Int64())
		drawScaledChar(img, string(char), x, y, scale, textColor)
	}
}

// drawScaledChar renders a glyph with Face7x13 and scales it up with nearest-neighbor,
// giving a visibly larger captcha text while keeping dependencies lightweight.
func drawScaledChar(dst *image.RGBA, char string, x, y, scale int, c color.Color) {
	glyphW := 12
	glyphH := 18
	glyph := image.NewRGBA(image.Rect(0, 0, glyphW, glyphH))

	d := &font.Drawer{
		Dst:  glyph,
		Src:  image.NewUniform(c),
		Face: basicfont.Face7x13,
		Dot:  fixed.Point26_6{X: fixed.I(1), Y: fixed.I(13)},
	}
	d.DrawString(char)

	for gy := 0; gy < glyphH; gy++ {
		for gx := 0; gx < glyphW; gx++ {
			_, _, _, a := glyph.At(gx, gy).RGBA()
			if a == 0 {
				continue
			}
			for sy := 0; sy < scale; sy++ {
				for sx := 0; sx < scale; sx++ {
					tx := x + gx*scale + sx
					ty := y + gy*scale + sy
					paintThickPixel(dst, tx, ty, c)
				}
			}
		}
	}
}

func paintThickPixel(dst *image.RGBA, x, y int, c color.Color) {
	offsets := [][2]int{
		{0, 0}, {1, 0}, {0, 1}, {-1, 0}, {0, -1},
	}
	for _, off := range offsets {
		tx := x + off[0]
		ty := y + off[1]
		if tx >= 0 && tx < Width && ty >= 0 && ty < Height {
			dst.Set(tx, ty, c)
		}
	}
}

func abs(x int) int {
	if x < 0 {
		return -x
	}
	return x
}
