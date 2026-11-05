package pipeline

import (
	"bytes"
	"encoding/json"
	"reflect"
	"strings"
	"testing"
)

func TestPhase4PreprocessingSeparatesControlsWithoutRewriting(t *testing.T) {
	const marker = "[转写过长，已截断]"
	clean := []byte(`[{"text":"原句。","speaker":"doctor"},{"text":"前半句[转写过长，已截断]后半句","speaker":"customer"},{"text":"……！？","speaker":"customer"},{"text":"[转写过长，已截断]","speaker":"consultant"}]`)
	asr := []byte(`[{"text":"原句。","start_ms":123},{"text":"前半句[转写过长，已截断]后半句","start_ms":500}]`)
	cleanBefore, asrBefore := bytes.Clone(clean), bytes.Clone(asr)
	got, trace, dropped, truncated, err := decodeSourceTranscript(clean, asr)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 3 {
		t.Fatalf("got %d sources; want original and two exact substrings", len(got))
	}
	if !truncated || len(dropped) < 2 {
		t.Fatal("missing control/noise audit")
	}
	if got[0].Text != "原句。" || got[0].Speaker != "医生" || got[0].StartMS == nil || *got[0].StartMS != 123 {
		t.Fatal("unchanged source provenance lost")
	}
	for i, want := range []string{"前半句", "后半句"} {
		s := got[i+1]
		if s.Text != want || s.Speaker != "客户" || s.StartMS != nil || s.TimingBasis != "unknown" {
			t.Fatalf("split source changed text or guessed timing: %+v", s)
		}
	}
	for _, s := range got {
		if strings.Contains(s.Text, marker) {
			t.Fatal("control marker sent as dialogue")
		}
	}
	// Check source origin through JSON to avoid coupling the test to struct names.
	raw, _ := json.Marshal(trace)
	var maps []map[string]any
	if err := json.Unmarshal(raw, &maps); err != nil {
		t.Fatal(err)
	}
	originals := map[string]string{"$[0]": "原句。", "$[1]": "前半句" + marker + "后半句", "$[2]": "……！？", "$[3]": marker}
	kept := 0
	for _, m := range maps {
		if m["disposition"] != "kept" {
			continue
		}
		kept++
		start, ok1 := m["start_byte"].(float64)
		end, ok2 := m["end_byte"].(float64)
		if !ok1 || !ok2 || m["original_text_hash"] == nil {
			t.Fatal("missing byte provenance")
		}
		original := originals[m["cleaned_json_path"].(string)]
		id := m["id"].(string)
		found := false
		for _, s := range got {
			if s.ID == id {
				found = true
				if original[int(start):int(end)] != s.Text {
					t.Fatal("quote is not exact original substring")
				}
			}
		}
		if !found {
			t.Fatal("mapping does not bind source")
		}
	}
	if kept != 3 {
		t.Fatalf("retained mappings=%d", kept)
	}
	again, againTrace, againDropped, againTruncated, err := decodeSourceTranscript(clean, asr)
	if err != nil || !reflect.DeepEqual(got, again) || !reflect.DeepEqual(trace, againTrace) || !reflect.DeepEqual(dropped, againDropped) || truncated != againTruncated {
		t.Fatal("preprocessing not deterministic")
	}
	if !bytes.Equal(clean, cleanBefore) || !bytes.Equal(asr, asrBefore) {
		t.Fatal("preprocessing mutated source bytes")
	}
}

func TestPhase4PreprocessingControlPositionsAndEmpty(t *testing.T) {
	const marker = "[转写过长，已截断]"
	for _, tt := range []struct {
		name, text string
		want       []string
		truncated  bool
	}{
		{"start", marker + "原文", []string{"原文"}, true},
		{"end", "原文" + marker, []string{"原文"}, true},
		{"multiple", "甲" + marker + "乙" + marker + "丙", []string{"甲", "乙", "丙"}, true},
		{"control_only", marker, []string{}, true},
		{"noise_only", "……！？", []string{}, false},
		{"whitespace_only", " \n\t", []string{}, false},
	} {
		t.Run(tt.name, func(t *testing.T) {
			clean, _ := json.Marshal([]map[string]string{{"text": tt.text, "speaker": "speaker_1"}})
			got, _, _, truncated, err := decodeSourceTranscript(clean, []byte(`[]`))
			if err != nil {
				t.Fatal(err)
			}
			texts := []string{}
			for _, s := range got {
				texts = append(texts, s.Text)
				if s.Speaker != "未知" || s.RoleBasis != "unknown" || s.StartMS != nil {
					t.Fatal("guessed role/time")
				}
			}
			if !reflect.DeepEqual(texts, tt.want) || truncated != tt.truncated {
				t.Fatalf("got %q truncated=%t", texts, truncated)
			}
		})
	}
	for _, raw := range []string{`[] {}`, `[{"text":"原文"}] []`} {
		if _, _, _, _, err := decodeSourceTranscript([]byte(raw), []byte(`[]`)); err == nil {
			t.Fatal("trailing source JSON accepted")
		}
	}
	if _, _, _, _, err := decodeSourceTranscript([]byte(`[{"text":"原文"}]`), []byte(`[] {}`)); err == nil {
		t.Fatal("trailing ASR JSON accepted")
	}
	got, _, _, _, err := decodeSourceTranscript([]byte(`[]`), []byte(`[]`))
	if err != nil || len(got) != 0 {
		t.Fatal("valid empty source must be distinguishable from corrupt JSON")
	}
}

func TestPhase4AssemblyRejectsUnnormalizedDialogue(t *testing.T) {
	for _, text := range []string{"……！？", "[转写过长，已截断]", "有效原文[转写过长，已截断]"} {
		m := material()
		m.Transcript[0].Text = text
		if _, err := AssembleInput(m); err == nil {
			t.Fatalf("unnormalized source accepted: %q", text)
		}
	}
	m := material()
	m.Transcript = nil
	m.Facts = nil
	if _, err := AssembleInput(m); err != nil {
		t.Fatal("empty normalized transcript should be a reviewable input", err)
	}
}
