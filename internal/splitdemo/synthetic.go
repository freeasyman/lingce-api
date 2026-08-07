package splitdemo

import (
	"context"
	"fmt"
	"sort"
	"strconv"
	"strings"
)

type syntheticSourceBundle struct {
	Source    SyntheticSource
	Detail    *RecordingDetail
	Utterance []Utterance
}

func (s *Service) ListSyntheticCandidates(ctx context.Context, tenantID int64, minDurationSeconds, maxDurationSeconds, limit int) ([]SyntheticCandidate, error) {
	return s.store.ListSyntheticCandidates(ctx, tenantID, minDurationSeconds, maxDurationSeconds, limit)
}

func (s *Service) SaveSyntheticCase(ctx context.Context, record *SyntheticCase) error {
	if err := s.populateSyntheticSourceSummaries(ctx, record, nil, defaultModelName); err != nil {
		return err
	}
	return s.store.SaveSyntheticCase(ctx, record)
}

func (s *Service) ListSyntheticCases(ctx context.Context) ([]*SyntheticCase, error) {
	return s.store.ListSyntheticCases(ctx)
}

func (s *Service) GetSyntheticCase(ctx context.Context, caseID string) (*SyntheticCase, error) {
	return s.store.GetSyntheticCase(ctx, caseID)
}

func (s *Service) DeleteSyntheticCase(ctx context.Context, caseID string) error {
	return s.store.DeleteSyntheticCase(ctx, caseID)
}

func (s *Service) LatestSyntheticRun(ctx context.Context, caseID string) (*SplitRunRecord, error) {
	return s.store.runStore.LatestByCase(ctx, caseID)
}

func (s *Service) ListSyntheticRuns(ctx context.Context, caseID string, limit int) ([]*SplitRunRecord, error) {
	return s.store.runStore.ListByCase(ctx, caseID, limit)
}

func (s *Service) BuildSyntheticPreview(ctx context.Context, draft *SyntheticCase) (*SyntheticCase, error) {
	if draft == nil {
		return nil, fmt.Errorf("empty synthetic case")
	}
	cases, _, _, err := s.prepareSyntheticBundles(ctx, draft)
	if err != nil {
		return nil, err
	}
	if err := s.populateSyntheticSourceSummaries(ctx, draft, cases, defaultModelName); err != nil {
		return nil, err
	}
	_ = cases
	return draft, nil
}

func (s *Service) populateSyntheticSourceSummaries(ctx context.Context, syntheticCase *SyntheticCase, bundles []syntheticSourceBundle, model string) error {
	if syntheticCase == nil {
		return fmt.Errorf("synthetic case is nil")
	}
	if len(bundles) == 0 {
		var err error
		bundles, _, _, err = s.prepareSyntheticBundles(ctx, syntheticCase)
		if err != nil {
			return err
		}
	}
	for idx := range syntheticCase.Segments {
		if idx >= len(bundles) {
			break
		}
		if syntheticCase.Segments[idx].Summary != nil {
			continue
		}
		text := joinUtterances(bundles[idx].Utterance)
		if strings.TrimSpace(text) == "" {
			continue
		}
		summary, err := s.SummarizeEncounterText(ctx, model, text)
		if err != nil {
			return fmt.Errorf("summarize synthetic source %d: %w", idx+1, err)
		}
		syntheticCase.Segments[idx].Summary = summary
	}
	return nil
}

func (s *Service) buildSyntheticRecordingDetail(ctx context.Context, syntheticCase *SyntheticCase) (*RecordingDetail, error) {
	if syntheticCase == nil {
		return nil, fmt.Errorf("synthetic case is nil")
	}
	bundles, transcript, duration, err := s.prepareSyntheticBundles(ctx, syntheticCase)
	if err != nil {
		return nil, err
	}
	segments := make([]map[string]interface{}, 0, len(bundles)*8)
	for _, bundle := range bundles {
		for _, utt := range bundle.Utterance {
			segments = append(segments, map[string]interface{}{
				"start_seconds": utt.StartSeconds,
				"end_seconds":   utt.EndSeconds,
				"speaker":       utt.Speaker,
				"speaker_role":  utt.SpeakerRole,
				"text":          utt.Text,
			})
		}
		switch bundle.Source.GapType {
		case SyntheticGapCallNext:
			segments = append(segments, map[string]interface{}{
				"start_seconds": bundle.Source.EndSeconds,
				"end_seconds":   bundle.Source.EndSeconds + 3,
				"speaker":       "doctor",
				"speaker_role":  "doctor",
				"text":          "下一个",
			})
		case SyntheticGapClosing:
			segments = append(segments, map[string]interface{}{
				"start_seconds": bundle.Source.EndSeconds,
				"end_seconds":   bundle.Source.EndSeconds + 4,
				"speaker":       "doctor",
				"speaker_role":  "doctor",
				"text":          "好，就这样，回去按时吃药",
			})
		case SyntheticGapColleague:
			segments = append(segments, map[string]interface{}{
				"start_seconds": bundle.Source.EndSeconds,
				"end_seconds":   bundle.Source.EndSeconds + 6,
				"speaker":       "doctor",
				"speaker_role":  "doctor",
				"text":          "我先去看一下另一个病人",
			}, map[string]interface{}{
				"start_seconds": bundle.Source.EndSeconds + 6,
				"end_seconds":   bundle.Source.EndSeconds + 11,
				"speaker":       "nurse",
				"speaker_role":  "nurse",
				"text":          "好，等下叫我",
			})
		}
	}
	text := transcript
	return &RecordingDetail{
		ID:                -1,
		TenantID:          1,
		TenantName:        "synthetic",
		EmployeeID:        0,
		EmployeeName:      "synthetic",
		RecordingDuration: &duration,
		TranscriptText:    &text,
		TranscriptionSegs: segments,
		TranscriptSource:  "synthetic.case",
	}, nil
}

func (s *Service) prepareSyntheticBundles(ctx context.Context, syntheticCase *SyntheticCase) ([]syntheticSourceBundle, string, int, error) {
	if syntheticCase == nil {
		return nil, "", 0, fmt.Errorf("synthetic case is nil")
	}
	out := make([]syntheticSourceBundle, 0, len(syntheticCase.Segments))
	var parts []string
	cursor := 0
	for idx, source := range syntheticCase.Segments {
		detail, err := s.store.GetRecording(ctx, source.RecordingID)
		if err != nil {
			return nil, "", 0, fmt.Errorf("load source %d: %w", source.RecordingID, err)
		}
		if detail == nil {
			return nil, "", 0, fmt.Errorf("source recording %d not found", source.RecordingID)
		}
		utterances := buildUtterances(detail)
		if len(utterances) == 0 {
			start := cursor
			end := start + maxInt(recordingDurationSeconds(detail), 1)
			source.StartSeconds = start
			source.EndSeconds = end
			syntheticCase.Segments[idx] = source
			out = append(out, syntheticSourceBundle{Source: source, Detail: detail, Utterance: nil})
			cursor = end
			if source.GapSeconds > 0 {
				cursor += source.GapSeconds
			}
			continue
		}
		start := cursor
		end := cursor
		shifted := make([]Utterance, 0, len(utterances))
		for _, utt := range utterances {
			item := utt
			item.StartSeconds += start
			item.EndSeconds += start
			shifted = append(shifted, item)
			parts = append(parts, fmt.Sprintf("[%s-%s] %s: %s", formatSeconds(item.StartSeconds), formatSeconds(item.EndSeconds), speakerLabel(item), item.Text))
			if item.EndSeconds > end {
				end = item.EndSeconds
			}
		}
		if end <= start {
			end = start + maxInt(recordingDurationSeconds(detail), 1)
		}
		source.StartSeconds = start
		source.EndSeconds = end
		syntheticCase.Segments[idx] = source
		out = append(out, syntheticSourceBundle{Source: source, Detail: detail, Utterance: shifted})
		cursor = end
		if source.GapSeconds > 0 {
			cursor += source.GapSeconds
		}
	}
	text := strings.Join(parts, "\n")
	if syntheticCase.DurationSeconds <= 0 {
		syntheticCase.DurationSeconds = cursor
	}
	syntheticCase.Transcript = text
	syntheticCase.GroundTruth = buildSyntheticGroundTruth(syntheticCase.Segments)
	return out, text, syntheticCase.DurationSeconds, nil
}

func buildSyntheticGroundTruth(segments []SyntheticSource) []GroundTruthMark {
	marks := make([]GroundTruthMark, 0, len(segments))
	for i := 1; i < len(segments); i++ {
		marks = append(marks, GroundTruthMark{
			AtSeconds:   segments[i].StartSeconds,
			MarkType:    "encounter_boundary",
			GapType:     string(segments[i-1].GapType),
			Description: fmt.Sprintf("source_%d_to_%d", segments[i-1].RecordingID, segments[i].RecordingID),
		})
	}
	return marks
}

func EvaluateSyntheticCase(caseData *SyntheticCase, result *SplitResponse, toleranceSeconds int) *EvalReport {
	if toleranceSeconds <= 0 {
		toleranceSeconds = 15
	}
	report := &EvalReport{
		CaseID:           caseData.ID,
		ToleranceSeconds: toleranceSeconds,
		ByGapType:        map[string]*GapTypeStat{},
	}
	if caseData == nil || result == nil {
		return report
	}
	predicted := predictedEncounterBoundaries(result)
	groundTruth := append([]GroundTruthMark(nil), caseData.GroundTruth...)
	sort.Slice(groundTruth, func(i, j int) bool { return groundTruth[i].AtSeconds < groundTruth[j].AtSeconds })
	usedPredicted := make([]bool, len(predicted))
	var totalOffset int
	for _, gt := range groundTruth {
		report.TotalGroundTruth++
		gapStat := report.ensureGapType(gt.GapType)
		gapStat.Total++
		bestIdx := -1
		bestDelta := toleranceSeconds + 1
		for i, pred := range predicted {
			if usedPredicted[i] {
				continue
			}
			delta := absInt(pred - gt.AtSeconds)
			if delta <= toleranceSeconds && delta < bestDelta {
				bestDelta = delta
				bestIdx = i
			}
		}
		if bestIdx >= 0 {
			usedPredicted[bestIdx] = true
			report.Hits++
			gapStat.Hits++
			totalOffset += absInt(predicted[bestIdx] - gt.AtSeconds)
			if absInt(predicted[bestIdx]-gt.AtSeconds) > report.MaxOffset {
				report.MaxOffset = absInt(predicted[bestIdx] - gt.AtSeconds)
			}
			report.Details = append(report.Details, EvalDetail{
				Kind:          "hit",
				GroundTruthAt: gt.AtSeconds,
				PredictedAt:   predicted[bestIdx],
				OffsetSeconds: predicted[bestIdx] - gt.AtSeconds,
				GapType:       gt.GapType,
				ContextText:   syntheticContextText(caseData.Transcript, gt.AtSeconds),
			})
			continue
		}
		report.Missed++
		gapStat.Missed++
		report.Details = append(report.Details, EvalDetail{
			Kind:          "missed",
			GroundTruthAt: gt.AtSeconds,
			GapType:       gt.GapType,
			ContextText:   syntheticContextText(caseData.Transcript, gt.AtSeconds),
		})
	}
	for i, pred := range predicted {
		if usedPredicted[i] {
			continue
		}
		report.Extra++
		report.Details = append(report.Details, EvalDetail{
			Kind:        "extra",
			PredictedAt: pred,
			ContextText: syntheticContextText(caseData.Transcript, pred),
		})
	}
	if report.Hits+report.Extra > 0 {
		report.Precision = float64(report.Hits) / float64(report.Hits+report.Extra)
	}
	if report.TotalGroundTruth > 0 {
		report.Recall = float64(report.Hits) / float64(report.TotalGroundTruth)
	}
	if report.Hits > 0 {
		report.MeanOffset = float64(totalOffset) / float64(report.Hits)
	}
	return report
}

func predictedEncounterBoundaries(result *SplitResponse) []int {
	out := make([]int, 0, len(result.Encounters))
	for idx, encounter := range result.Encounters {
		if idx == 0 {
			continue
		}
		out = append(out, encounter.StartSeconds)
	}
	return out
}

func (r *EvalReport) ensureGapType(gapType string) *GapTypeStat {
	if r.ByGapType == nil {
		r.ByGapType = map[string]*GapTypeStat{}
	}
	if stat, ok := r.ByGapType[gapType]; ok {
		return stat
	}
	stat := &GapTypeStat{}
	r.ByGapType[gapType] = stat
	return stat
}

func syntheticContextText(transcript string, seconds int) string {
	if strings.TrimSpace(transcript) == "" {
		return ""
	}
	lines := strings.Split(transcript, "\n")
	if len(lines) == 0 {
		return ""
	}
	idx := clamp(seconds/60, 0, len(lines)-1)
	start := maxInt(0, idx-1)
	end := minInt(len(lines), idx+2)
	return strings.Join(lines[start:end], "\n")
}

func absInt(v int) int {
	if v < 0 {
		return -v
	}
	return v
}

func buildSyntheticMatchReport(caseData *SyntheticCase, result *SplitResponse) *MatchReport {
	report := &MatchReport{}
	if caseData == nil || result == nil {
		return report
	}
	sources := append([]SyntheticSource(nil), caseData.Segments...)
	encounters := append([]EncounterView(nil), result.Encounters...)
	report.MaterialCount = len(sources)
	report.EncounterCount = len(encounters)
	materialOverlaps := make([][]int, len(sources))
	encounterOverlaps := make([][]int, len(encounters))
	for i, src := range sources {
		for j, enc := range encounters {
			ov := overlapSeconds(src.StartSeconds, src.EndSeconds, enc.StartSeconds, enc.EndSeconds)
			if ov <= 0 {
				continue
			}
			srcDur := maxInt(src.EndSeconds-src.StartSeconds, 1)
			encDur := maxInt(enc.EndSeconds-enc.StartSeconds, 1)
			srcRatio := float64(ov) / float64(srcDur)
			encRatio := float64(ov) / float64(encDur)
			if srcRatio >= 0.3 || encRatio >= 0.3 {
				materialOverlaps[i] = append(materialOverlaps[i], j+1)
				encounterOverlaps[j] = append(encounterOverlaps[j], i+1)
			}
		}
	}
	for i, src := range sources {
		item := MaterialMatch{
			MaterialIndex: i + 1,
			RecordingID:   src.RecordingID,
			StartSeconds:  src.StartSeconds,
			EndSeconds:    src.EndSeconds,
			Summary:       src.Summary,
			MatchedEncounters: append([]int(nil), materialOverlaps[i]...),
		}
		switch len(materialOverlaps[i]) {
		case 0:
			item.Verdict = "missing"
			report.MissedCount++
		case 1:
			encIdx := materialOverlaps[i][0] - 1
			if len(encounterOverlaps[encIdx]) > 1 {
				item.Verdict = "merged"
				for _, other := range encounterOverlaps[encIdx] {
					if other != i+1 {
						item.MergedWith = append(item.MergedWith, other)
					}
				}
			} else {
				item.Verdict = "matched"
			}
		default:
			item.Verdict = "oversplit"
			report.OversplitCount++
		}
		report.Materials = append(report.Materials, item)
	}
	for i, enc := range encounters {
		item := EncounterMatch{
			Seq:              i + 1,
			StartSeconds:     enc.StartSeconds,
			EndSeconds:       enc.EndSeconds,
			Summary:          summaryFromEncounter(enc),
			CoveredMaterials: append([]int(nil), encounterOverlaps[i]...),
		}
		switch len(encounterOverlaps[i]) {
		case 0:
			item.Verdict = "swallowed"
		case 1:
			if len(materialOverlaps[encounterOverlaps[i][0]-1]) == 1 {
				item.Verdict = "matched"
			} else {
				item.Verdict = "partial"
			}
		default:
			item.Verdict = "swallowed"
		}
		report.Encounters = append(report.Encounters, item)
	}
	for i := 0; i < len(sources)-1; i++ {
		nextStart := sources[i+1].StartSeconds
		report.Seams = append(report.Seams, SeamDetail{
			AfterMaterial: i + 1,
			AtSeconds:     nextStart,
			GapType:       string(sources[i].GapType),
			GapSeconds:    sources[i].GapSeconds,
			Detected:      seamDetected(encounters, nextStart),
			BeforeText:    syntheticSeamContext(caseData.Transcript, nextStart, true),
			AfterText:     syntheticSeamContext(caseData.Transcript, nextStart, false),
			AIConfidence:  nearestEncounterConfidence(encounters, nextStart),
		})
	}
	return report
}

func summaryFromEncounter(enc EncounterView) *EncounterSummary {
	if strings.TrimSpace(enc.PatientHint) == "" && strings.TrimSpace(enc.ChiefComplaint) == "" && strings.TrimSpace(enc.Disposition) == "" {
		return nil
	}
	return &EncounterSummary{
		PatientHint:    enc.PatientHint,
		ChiefComplaint: enc.ChiefComplaint,
		Disposition:    enc.Disposition,
	}
}

func seamDetected(encounters []EncounterView, seam int) bool {
	for _, enc := range encounters {
		if absInt(enc.StartSeconds-seam) <= 20 {
			return true
		}
	}
	return false
}

func nearestEncounterConfidence(encounters []EncounterView, seam int) float64 {
	bestDelta := 21
	best := 0.0
	for _, enc := range encounters {
		delta := absInt(enc.StartSeconds - seam)
		if delta <= 20 && delta < bestDelta {
			bestDelta = delta
			best = enc.BoundaryConfidence
		}
	}
	return best
}

func overlapSeconds(aStart, aEnd, bStart, bEnd int) int {
	start := maxInt(aStart, bStart)
	end := minInt(aEnd, bEnd)
	if end <= start {
		return 0
	}
	return end - start
}

func syntheticSeamContext(transcript string, seam int, before bool) string {
	lines := strings.Split(strings.TrimSpace(transcript), "\n")
	if len(lines) == 0 {
		return ""
	}
	type lineItem struct {
		seconds int
		text    string
	}
	items := make([]lineItem, 0, len(lines))
	for _, line := range lines {
		sec, ok := parseSyntheticLineSeconds(line)
		if !ok {
			continue
		}
		items = append(items, lineItem{seconds: sec, text: strings.TrimSpace(line)})
	}
	if len(items) == 0 {
		return ""
	}
	if before {
		kept := make([]string, 0, 5)
		for _, item := range items {
			if item.seconds >= seam {
				break
			}
			if item.seconds < seam {
				kept = append(kept, item.text)
			}
		}
		if len(kept) > 5 {
			kept = kept[len(kept)-5:]
		}
		return strings.Join(kept, "\n")
	}
	kept := make([]string, 0, 5)
	for _, item := range items {
		if item.seconds < seam {
			continue
		}
		kept = append(kept, item.text)
		if len(kept) >= 5 {
			break
		}
	}
	return strings.Join(kept, "\n")
}

func parseSyntheticLineSeconds(line string) (int, bool) {
	line = strings.TrimSpace(line)
	if !strings.HasPrefix(line, "[") {
		return 0, false
	}
	end := strings.Index(line, "]")
	if end <= 1 {
		return 0, false
	}
	raw := line[1:end]
	parts := strings.Split(raw, "-")
	if len(parts) == 0 {
		return 0, false
	}
	return parseSyntheticClock(parts[0])
}

func parseSyntheticClock(raw string) (int, bool) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return 0, false
	}
	parts := strings.Split(raw, ":")
	switch len(parts) {
	case 2:
		mins := numericStringToInt(parts[0])
		secs := numericStringToInt(parts[1])
		return mins*60 + secs, true
	case 3:
		hours := numericStringToInt(parts[0])
		mins := numericStringToInt(parts[1])
		secs := numericStringToInt(parts[2])
		return hours*3600 + mins*60 + secs, true
	default:
		return numericStringToInt(raw), true
	}
}

func numericStringToInt(raw string) int {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return 0
	}
	n, _ := strconv.Atoi(raw)
	return n
}
