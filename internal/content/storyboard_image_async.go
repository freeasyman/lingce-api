package content

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"time"

	"github.com/freeasyman/lingce-api/pkg/httputil"
)

// storyboardShotPromptGo 生成分镜单帧线稿的 prompt（与前端 storyboardShotPrompt 对齐）。
func storyboardShotPromptGo(shotNo, sceneNo int, shot map[string]any) string {
	dur := 3
	switch d := shot["duration"].(type) {
	case float64:
		if d > 0 {
			dur = int(d)
		}
	case int:
		if d > 0 {
			dur = d
		}
	case json.Number:
		if v, err := d.Int64(); err == nil && v > 0 {
			dur = int(v)
		}
	}
	str := func(k string) string { return fmt.Sprintf("%v", shot[k]) }
	return fmt.Sprintf(
		`分镜脚本单帧，标准分镜格布局：一条清晰的外框，框内从上到下分三栏——上页眉横栏、中部画面、下页脚横栏。`+
			`上页眉横栏（横贯整幅，左右分列）：左「场次 %02d　镜号 %02d」，右「景别 %s　机位 %s」。`+
			`中部画面：%s（%s），黑白线条分镜、占画面主体；人物旁一个对话框，写台词「%s」。`+
			`下页脚横栏（横贯整幅，左右分列）：左「动作：%s」，右「时长 %d 秒」。`+
			`整体要像一张正式的分镜脚本单页：排版工整、字号统一、外框与页眉页脚分栏清晰；`+
			`不要随手画一堆大小不一的矩形框，不要装饰性元素。`+
			`画风黑白极简线条、无阴影、无素描排线；所有文字必须中文，绝不能出现英文、字母、外文。`,
		sceneNo, shotNo, str("shot_size"), str("camera"), str("visual"), str("action"), str("dialogue"), str("action"), dur,
	)
}

// generateStoryboardImagesAsync 后台按顺序生成分镜缺失的线稿图，逐张写回 content.images。
// images 是"稀疏数组"（无人物镜头不占位、缺图的镜头是空串），所以能按 index 精确定位缺哪张。
func (s *Service) generateStoryboardImagesAsync(ctx context.Context, tenantID, contentID, regenerateIndex int64) {
	rec, err := s.store.getContentItemV2(ctx, tenantID, contentID)
	if err != nil {
		slog.Error("async storyboard images: load content", "error", err, "content_id", contentID)
		return
	}
	var body struct {
		Chapters []struct {
			Shots []map[string]any `json:"shots"`
		} `json:"chapters"`
	}
	if err := json.Unmarshal([]byte(rec.Body), &body); err != nil {
		slog.Error("async storyboard images: parse body", "error", err, "content_id", contentID)
		return
	}
	var imgs []string
	if err := json.Unmarshal(rec.Images, &imgs); err != nil {
		imgs = []string{}
	}

	type job struct {
		imgIdx, shotNo, sceneNo int
		shot                    map[string]any
	}
	var jobs []job
	imgIdx, shotNo := 0, 0
	for ci := range body.Chapters {
		for _, sh := range body.Chapters[ci].Shots {
			shotNo++
			if fmt.Sprintf("%v", sh["cast"]) == "无人物" {
				continue
			}
			jobs = append(jobs, job{imgIdx: imgIdx, shotNo: shotNo, sceneNo: ci + 1, shot: sh})
			imgIdx++
		}
	}

	_ = s.store.setContentGenerationStatus(ctx, tenantID, contentID, "running", int(regenerateIndex))
	defer func() { _ = s.store.setContentGenerationStatus(ctx, tenantID, contentID, "", -1) }()

	for _, j := range jobs {
		if regenerateIndex >= 0 {
			if int64(j.imgIdx) != regenerateIndex {
				continue // 只重生成指定那一镜
			}
		} else if j.imgIdx < len(imgs) && imgs[j.imgIdx] != "" {
			continue // 已有图，跳过
		}
		urls, err := s.GenerateCardImage(ctx, tenantID, storyboardShotPromptGo(j.shotNo, j.sceneNo, j.shot), "", "", "", 1, "")
		if err != nil {
			slog.Error("async storyboard images: generate", "error", err, "shot_no", j.shotNo)
			continue // 失败不中断整体，继续下一张
		}
		// 拉长到 imgIdx 长度，保持稀疏对齐
		for len(imgs) <= j.imgIdx {
			imgs = append(imgs, "")
		}
		if len(urls) > 0 {
			imgs[j.imgIdx] = urls[0]
		}
		newImgs, _ := json.Marshal(imgs)
		if err := s.store.updateContentItemV2(ctx, tenantID, contentID, "", rec.Title, rec.Body, rec.PublishPackage, newImgs); err != nil {
			slog.Error("async storyboard images: save", "error", err, "shot_no", j.shotNo)
		}
		time.Sleep(3 * time.Second)
	}
}

// GenerateStoryboardImagesAsync 触发后台生成分镜缺失线稿，立即返回，不阻塞请求。
func (h *Handler) GenerateStoryboardImagesAsync(w http.ResponseWriter, r *http.Request) {
	tenantID, ok := tenantOf(r)
	if !ok {
		httputil.WriteUnauthorized(w, "Invalid token")
		return
	}
	var req struct {
		ContentID int64  `json:"content_id"`
		ShotIndex *int64 `json:"shot_index"` // nil = 生成缺失；非 nil = 只重生成该镜
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil || req.ContentID <= 0 {
		httputil.WriteBadRequest(w, "content_id is required")
		return
	}
	regenerateIndex := int64(-1)
	if req.ShotIndex != nil {
		regenerateIndex = *req.ShotIndex
	}
	// 用独立 context，脱离请求生命周期，浏览器关掉也能继续跑
	go func() {
		slog.Info("async storyboard images started", "tenant_id", tenantID, "content_id", req.ContentID, "shot_index", regenerateIndex)
		h.service.generateStoryboardImagesAsync(context.Background(), tenantID, req.ContentID, regenerateIndex)
	}()
	httputil.WriteSuccess(w, map[string]any{"started": true})
}
