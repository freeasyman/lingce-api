package content

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"strings"
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

// storyboardImageJob 是一个需要出图的分镜镜头及其稳定图位下标。
// 无人物镜头不占图位，因此图位下标与 shots 的下标不是一回事。
type storyboardImageJob struct {
	ImageIndex int
	ShotNo     int
	SceneNo    int
	Shot       map[string]any
}

// storyboardImageJobs 解析正文里的分镜镜头，并按"无人物不占位"的规则分配图位下标。
// 预检与后台生成共用这一份口径，保证"要生成几张"与"实际生成几张"一致。
func storyboardImageJobs(body string) ([]storyboardImageJob, error) {
	var parsed struct {
		Chapters []struct {
			Shots []map[string]any `json:"shots"`
		} `json:"chapters"`
	}
	if err := json.Unmarshal([]byte(body), &parsed); err != nil {
		return nil, err
	}
	jobs := make([]storyboardImageJob, 0)
	imageIndex, shotNo := 0, 0
	for chapterIndex := range parsed.Chapters {
		for _, shot := range parsed.Chapters[chapterIndex].Shots {
			shotNo++
			if fmt.Sprintf("%v", shot["cast"]) == "无人物" {
				continue
			}
			jobs = append(jobs, storyboardImageJob{
				ImageIndex: imageIndex,
				ShotNo:     shotNo,
				SceneNo:    chapterIndex + 1,
				Shot:       shot,
			})
			imageIndex++
		}
	}
	return jobs, nil
}

// storyboardPendingImageSlots 返回本次实际要生成的分镜图位：
//   - regenerateIndex >= 0：只重生成指定图位（没有对应镜头时返回空）；
//   - regenerateIndex < 0：所有还没有图的缺失图位。
//
// 预检与后台生成必须共用这个结果，避免低余额时按 1 张放行、实际生成 N 张。
func storyboardPendingImageSlots(jobs []storyboardImageJob, images []string, regenerateIndex int64) []int {
	slots := make([]int, 0, len(jobs))
	for _, job := range jobs {
		if regenerateIndex >= 0 {
			if int64(job.ImageIndex) == regenerateIndex {
				slots = append(slots, job.ImageIndex)
			}
			continue
		}
		if job.ImageIndex < len(images) && strings.TrimSpace(images[job.ImageIndex]) != "" {
			continue // 已有图，跳过
		}
		slots = append(slots, job.ImageIndex)
	}
	return slots
}

// pendingStoryboardImageSlots 读取内容并计算本次要生成的分镜图位。
// 生成入口据此按实际图位数做额度预检；返回空切片表示没有图需要生成。
func (s *Service) pendingStoryboardImageSlots(ctx context.Context, tenantID, contentID, regenerateIndex int64) ([]int, error) {
	if s == nil || s.store == nil {
		return nil, NewBizError("CONTENT_STORE_NOT_CONFIGURED", "内容存储未配置")
	}
	rec, err := s.store.getContentItemV2(ctx, tenantID, contentID)
	if err != nil {
		return nil, fmt.Errorf("加载内容失败: %w", err)
	}
	if rec == nil {
		return nil, &BizError{Code: "CONTENT_NOT_FOUND", Message: "内容不存在或已被删除", Status: http.StatusNotFound}
	}
	jobs, err := storyboardImageJobs(rec.Body)
	if err != nil {
		return nil, NewBizError("INVALID_STORYBOARD_BODY", "分镜正文解析失败，无法生成图片")
	}
	var images []string
	if err := json.Unmarshal(rec.Images, &images); err != nil {
		images = nil
	}
	return storyboardPendingImageSlots(jobs, images, regenerateIndex), nil
}

// generateStoryboardImagesAsync 后台按顺序生成指定图位的分镜线稿，逐张写回 content.images。
// slots 由入口在预检前算好并传入，保证预检张数 = 实际生成张数；逐图位扣 shot 1。
func (s *Service) generateStoryboardImagesAsync(ctx context.Context, tenantID, contentID int64, slots []int, regenerateIndex int64) {
	rec, err := s.store.getContentItemV2(ctx, tenantID, contentID)
	if err != nil {
		slog.Error("async storyboard images: load content", "error", err, "content_id", contentID)
		return
	}
	if rec == nil {
		slog.Error("async storyboard images: content not found", "content_id", contentID)
		return
	}
	jobs, err := storyboardImageJobs(rec.Body)
	if err != nil {
		slog.Error("async storyboard images: parse body", "error", err, "content_id", contentID)
		return
	}
	jobBySlot := make(map[int]storyboardImageJob, len(jobs))
	for _, job := range jobs {
		jobBySlot[job.ImageIndex] = job
	}
	var imgs []string
	if err := json.Unmarshal(rec.Images, &imgs); err != nil {
		imgs = []string{}
	}

	_ = s.store.setContentGenerationStatus(ctx, tenantID, contentID, "running", int(regenerateIndex))
	defer func() { _ = s.store.setContentGenerationStatus(ctx, tenantID, contentID, "", -1) }()

	for _, slot := range slots {
		job, ok := jobBySlot[slot]
		if !ok {
			continue
		}
		urls, err := s.GenerateCardImage(ctx, tenantID, storyboardShotPromptGo(job.ShotNo, job.SceneNo, job.Shot), "", "", "", 1, "")
		if err != nil {
			slog.Error("async storyboard images: generate", "error", err, "shot_no", job.ShotNo)
			continue // 失败不中断整体，继续下一张
		}
		// 拉长到图位下标长度，保持稀疏对齐
		for len(imgs) <= slot {
			imgs = append(imgs, "")
		}
		if len(urls) > 0 {
			imgs[slot] = urls[0]
		}
		newImgs, _ := json.Marshal(imgs)
		if err := s.store.updateContentItemV2(ctx, tenantID, contentID, "", rec.Title, rec.Body, rec.PublishPackage, newImgs); err != nil {
			slog.Error("async storyboard images: save", "error", err, "shot_no", job.ShotNo)
		} else if len(urls) > 0 {
			// 该镜图片生成并写入内容成功：扣 shot 1 张（同一图位重生成不重复扣）。
			s.chargeImageSlot(ctx, tenantID, contentID, slot)
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
	// 交付前预检：先算出本次实际要生成的分镜图位（缺失图位 or 指定重生成 1 张），
	// 按图位数检查 shot 额度；余额不足整单拒绝，避免只生成一部分图。
	slots, err := h.service.pendingStoryboardImageSlots(r.Context(), tenantID, req.ContentID, regenerateIndex)
	if err != nil {
		writeContentError(w, err)
		return
	}
	if err := h.service.ensureAsyncImageQuota(r.Context(), tenantID, len(slots)); err != nil {
		writeContentError(w, err)
		return
	}
	// 用独立 context，脱离请求生命周期，浏览器关掉也能继续跑
	go func() {
		slog.Info("async storyboard images started", "tenant_id", tenantID, "content_id", req.ContentID,
			"shot_index", regenerateIndex, "images", len(slots))
		h.service.generateStoryboardImagesAsync(context.Background(), tenantID, req.ContentID, slots, regenerateIndex)
	}()
	httputil.WriteSuccess(w, map[string]any{"started": true, "images": len(slots)})
}
