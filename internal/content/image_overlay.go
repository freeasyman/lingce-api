package content

import (
	"bytes"
	"context"
	"fmt"
	"image"
	"image/draw"
	_ "image/jpeg"
	"image/png"
	"net/http"
	"time"

	xdraw "golang.org/x/image/draw"
)

func (s *Service) composeBrandAssets(ctx context.Context, data []byte, contentType, logoURL, qrURL string) ([]byte, string) {
	if logoURL == "" && qrURL == "" {
		return data, contentType
	}
	src, _, err := image.Decode(bytes.NewReader(data))
	if err != nil {
		return data, contentType
	}
	dst := image.NewRGBA(src.Bounds())
	draw.Draw(dst, dst.Bounds(), src, src.Bounds().Min, draw.Src)
	width := dst.Bounds().Dx()
	margin := maxInt(18, width/50)

	if logoURL != "" {
		if logo, err := fetchRemoteImage(ctx, logoURL); err == nil {
			targetW := minInt(96, width/10)
			targetH := targetW * logo.Bounds().Dy() / maxInt(1, logo.Bounds().Dx())
			rect := image.Rect(margin, margin, margin+targetW, margin+targetH)
			xdraw.CatmullRom.Scale(dst, rect, logo, logo.Bounds(), draw.Over, nil)
		}
	}
	if qrURL != "" {
		if qr, err := fetchRemoteImage(ctx, qrURL); err == nil {
			size := minInt(140, width/7)
			x0 := width - margin - size
			y0 := dst.Bounds().Dy() - margin - size
			// White plate keeps the QR readable over photos.
			draw.Draw(dst, image.Rect(x0-6, y0-6, x0+size+6, y0+size+6), image.White, image.Point{}, draw.Src)
			xdraw.CatmullRom.Scale(dst, image.Rect(x0, y0, x0+size, y0+size), qr, qr.Bounds(), draw.Over, nil)
		}
	}

	var out bytes.Buffer
	if err := png.Encode(&out, dst); err != nil {
		return data, contentType
	}
	return out.Bytes(), "image/png"
}

func fetchRemoteImage(ctx context.Context, url string) (image.Image, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, err
	}
	client := &http.Client{Timeout: 20 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("asset status %d", resp.StatusCode)
	}
	img, _, err := image.Decode(resp.Body)
	return img, err
}

func minInt(a, b int) int {
	if a < b {
		return a
	}
	return b
}

func maxInt(a, b int) int {
	if a > b {
		return a
	}
	return b
}
