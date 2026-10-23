package content

import (
	"context"
	"encoding/base64"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/freeasyman/lingce-api/pkg/llmgateway"
	"github.com/freeasyman/lingce-api/pkg/oss"
)

// GenerateCardImage 生成卡片图候选：文生图（可带参考模板）+ 素材叠加 + 上传 OSS。返回 n 个候选 URL。
func (s *Service) GenerateCardImage(ctx context.Context, tenantID int64, prompt, referenceImage, logoURL, qrURL string, n int) ([]string, error) {
	if s.llmClient == nil {
		return nil, fmt.Errorf("llm gateway is not configured")
	}
	if strings.TrimSpace(prompt) == "" {
		return nil, fmt.Errorf("prompt is required")
	}
	if n <= 0 {
		n = 3
	}
	if n > 4 {
		n = 4
	}

	selectedModel, err := s.resolveLLMModelConfig(ctx, tenantID, []string{"image_generation"})
	if err != nil {
		return nil, fmt.Errorf("resolve llm config for image generation: %w", err)
	}

	resp, err := s.llmClient.GenerateImage(ctx, llmgateway.ImageGenerationRequest{
		TenantID:      tenantID,
		CallerService: "lingce-api",
		CallerModule:  contentCallerModule("card_image"),
		FunctionType:  selectedModel.FunctionType,
		Provider:      selectedModel.Provider,
		ModelCode:     selectedModel.ModelCode,
		Prompt:        prompt,
		ImageURL:      referenceImage,
		N:             n,
		Size:          "1024x1024",
	})
	if err != nil {
		return nil, fmt.Errorf("generate image: %w", err)
	}
	if len(resp.Images) == 0 {
		return nil, fmt.Errorf("image generation returned no image")
	}

	urls := make([]string, 0, len(resp.Images))
	for i, source := range resp.Images {
		source = strings.TrimSpace(source)
		if source == "" {
			continue
		}
		url, err := s.downloadComposeUpload(ctx, tenantID, source, prompt, logoURL, qrURL)
		if err != nil {
			return nil, fmt.Errorf("process image %d: %w", i, err)
		}
		urls = append(urls, url)
	}
	if len(urls) == 0 {
		return nil, fmt.Errorf("no image produced")
	}
	return urls, nil
}

func (s *Service) downloadComposeUpload(ctx context.Context, tenantID int64, source, prompt, logoURL, qrURL string) (string, error) {
	var data []byte
	contentType := "image/png"
	var err error
	if strings.HasPrefix(source, "data:") {
		parts := strings.SplitN(source, ",", 2)
		if len(parts) != 2 {
			return "", fmt.Errorf("invalid image data uri")
		}
		data, err = base64.StdEncoding.DecodeString(parts[1])
		if err != nil {
			return "", fmt.Errorf("decode image data uri: %w", err)
		}
	} else {
		req, reqErr := http.NewRequestWithContext(ctx, http.MethodGet, source, nil)
		if reqErr != nil {
			return "", fmt.Errorf("create download request: %w", reqErr)
		}
		httpResp, reqErr := (&http.Client{Timeout: 30 * time.Second}).Do(req)
		if reqErr != nil {
			return "", fmt.Errorf("download image: %w", reqErr)
		}
		defer httpResp.Body.Close()
		if httpResp.StatusCode != http.StatusOK {
			return "", fmt.Errorf("download image: status %d", httpResp.StatusCode)
		}
		data, err = io.ReadAll(httpResp.Body)
		if err != nil {
			return "", fmt.Errorf("read image: %w", err)
		}
		if ct := httpResp.Header.Get("Content-Type"); strings.HasPrefix(ct, "image/") {
			contentType = ct
		}
	}

	data, contentType = s.composeBrandAssets(ctx, data, contentType, logoURL, qrURL)

	if s.ossClient == nil {
		return source, nil
	}
	key := oss.GenerateObjectKey(fmt.Sprintf("content/card/%d", tenantID), ".png")
	url, err := s.ossClient.UploadBytes(ctx, key, data, &oss.UploadOptions{
		ContentType: contentType,
		Metadata:    map[string]string{"prompt": sanitizeHeaderValue(prompt)},
	})
	if err != nil {
		return "", fmt.Errorf("upload image: %w", err)
	}
	return url, nil
}

// sanitizeHeaderValue 清洗 HTTP 头值：去掉控制字符/非 ASCII（中文、emoji、换行），截断。
func sanitizeHeaderValue(v string) string {
	var b strings.Builder
	for _, r := range v {
		if r >= 32 && r < 127 {
			b.WriteRune(r)
		} else {
			b.WriteByte(' ')
		}
		if b.Len() >= 120 {
			break
		}
	}
	return strings.TrimSpace(b.String())
}
