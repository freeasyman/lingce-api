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
	Width  = 120
	Height = 40
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

	// Add noise lines
	addNoiseLines(img, 5)

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
			color.RGBA{200, 200, 200, 255})
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
	point := fixed.Point26_6{X: fixed.I(10), Y: fixed.I(28)}
	d := &font.Drawer{
		Dst:  img,
		Src:  image.NewUniform(color.RGBA{50, 50, 50, 255}),
		Face: basicfont.Face7x13,
		Dot:  point,
	}

	for _, char := range text {
		d.DrawString(string(char))
		// Add random spacing
		offset, _ := rand.Int(rand.Reader, big.NewInt(5))
		d.Dot.X += fixed.I(int(offset.Int64()) + 15)
	}
}

func abs(x int) int {
	if x < 0 {
		return -x
	}
	return x
}
