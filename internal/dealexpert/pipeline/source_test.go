package pipeline

import (
	"bytes"
	"context"
	"encoding/json"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

func TestSourceTranscriptPreservesRoleTimeAndOriginalText(t *testing.T) {
	clean := []byte(`[{"text":"原话。","speaker_role":"doctor"},{"text":"重复","speaker_role":"customer"},{"text":"未知时间","speaker_role":"speaker_1"},{"text":" ","speaker_role":"consultant"},{"text":"[转写过长，已截断]","speaker_role":"consultant"}]`)
	asr := []byte(`[{"text":"原话。","start_time":1.235},{"text":"重复","start_time":2},{"text":"重复","start_time":3}]`)
	got, trace, dropped, truncated, err := decodeSourceTranscript(clean, asr)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 3 || got[0].Text != "原话。" || got[0].Speaker != "医生" || got[0].StartMS == nil || *got[0].StartMS != 1235 || trace[0].ASRIndex == nil || *trace[0].ASRIndex != 0 {
		t.Fatalf("wrong first source: %+v", got)
	}
	if got[1].StartMS != nil || got[1].TimingBasis != "unknown" || got[2].Speaker != "未知" || got[2].RoleBasis != "unknown" {
		t.Fatal("inferred ambiguous timing or role")
	}
	if len(dropped) != 2 || dropped[0] != "$[3]" || dropped[1] != "$[4]" || !truncated || got[2].ID != "s3" {
		t.Fatal("preprocessing trace missing")
	}
}

func TestSourceTranscriptRejectsMalformed(t *testing.T) {
	for _, raw := range []string{`null`, `{}`, `[{"text":7}]`, `["text"]`} {
		t.Run(raw, func(t *testing.T) {
			if _, _, _, _, err := decodeSourceTranscript([]byte(raw), []byte(`[]`)); err == nil {
				t.Fatal("corrupt source accepted")
			}
		})
	}
	for _, raw := range []string{`[{"text":"s","start_time":-1}]`, `[{"text":"s","start_ms":1.5}]`} {
		got, _, _, _, err := decodeSourceTranscript([]byte(`{"segments":[{"text":"s","speaker":"doctor"}]}`), []byte(raw))
		if err != nil || got[0].StartMS != nil {
			t.Fatal("unsafe timestamp accepted", err)
		}
	}
	got, _, _, _, err := decodeSourceTranscript([]byte(`[{"text":"s","speaker":"doctor"}]`), []byte(`[{"text":"s","start_ms":25}]`))
	if err != nil || got[0].StartMS == nil || *got[0].StartMS != 25 {
		t.Fatal("millisecond unit lost", err)
	}
}

func TestPhase3PostgresSourceLoader(t *testing.T) {
	ctx, tx := auditTestDB(t)
	_, err := tx.Exec(ctx, `CREATE TABLE employees(id BIGINT,tenant_id BIGINT,full_name TEXT,name TEXT);
CREATE TABLE encounters(id BIGINT,tenant_id BIGINT);
CREATE TABLE recordings(id BIGINT,tenant_id BIGINT,encounter_id BIGINT,employee_id BIGINT,deleted_at TIMESTAMP,recorded_at TIMESTAMP,cleaned_transcription JSONB,transcription_segments JSON,transcription_status TEXT,cleaned_transcription_status TEXT,duration INTEGER);
CREATE TABLE deal_expert_analyses(id BIGINT,tenant_id BIGINT,recording_id BIGINT,encounter_id BIGINT,employee_id BIGINT,cleaned_transcript TEXT,customer_id BIGINT);
CREATE TABLE deal_outcome_events(id BIGINT,tenant_id BIGINT,encounter_id BIGINT,type TEXT,amount NUMERIC(12,2),occurred_at TIMESTAMPTZ,source TEXT,source_ref TEXT,note TEXT,confirmed_by BIGINT);
CREATE TABLE products(id BIGINT,tenant_id BIGINT,name TEXT,version INTEGER,applicable TEXT,deleted_at TIMESTAMP,status TEXT,is_template BOOLEAN,review_status TEXT,updated_at TIMESTAMP);
INSERT INTO employees VALUES(223,17,'咨询师甲',''); INSERT INTO encounters VALUES(15,17);
INSERT INTO recordings VALUES(15,17,15,223,NULL,'2026-10-01 10:00:00','[{"text":"原话","speaker_role":"doctor"},{"text":"未录到时间","speaker_role":"customer"}]','[{"text":"原话","start_time":1.235}]','completed','completed',100);
INSERT INTO deal_expert_analyses VALUES(12,17,15,15,223,'[转写过长，已截断]',55);
INSERT INTO deal_outcome_events VALUES(700,17,15,'付款',123.45,NOW(),'employee','','',223),(701,18,15,'退款',9,NOW(),'employee','','',224);
INSERT INTO products VALUES(10,17,'批准产品',3,'说明',NULL,'active',false,'approved',NOW()),(11,18,'其他租户',1,'',NULL,'active',false,'approved',NOW()),(12,17,'未批准',1,'',NULL,'active',false,'pending',NOW());`)
	if err != nil {
		t.Fatal(err)
	}
	at := time.Date(2026, 10, 31, 10, 0, 0, 0, time.UTC)
	s, err := loadSourceSnapshot(ctx, tx, 17, 12, at)
	if err != nil {
		t.Fatal(err)
	}
	if s.Material.Recording.RecordedAt == nil || s.Material.Recording.RecordedAt.UTC().Format(time.RFC3339) != "2026-10-01T02:00:00Z" {
		t.Fatalf("Shanghai wall-clock time was misread: %v", s.Material.Recording.RecordedAt)
	}
	if s.Material.Context.IdentityConfirmed || s.Material.Context.ContactAuthorized != nil || s.Material.Context.Coverage != "unknown" {
		t.Fatal("association became authorization or coverage")
	}
	if len(s.Material.Facts) != 1 || s.Material.Facts[0].SourceRecordID != 700 || !strings.Contains(s.Material.Facts[0].Text, "123.45") || len(s.Material.Products) != 1 || s.Material.Products[0].ID != 10 {
		t.Fatal("wrong facts/catalog")
	}
	if !bytes.Contains(s.Audit, []byte(`"legacy_request_was_truncated":true`)) || !bytes.Contains(s.Audit, []byte(`"source_record_id":700`)) {
		t.Fatal("source provenance absent")
	}
	if _, err := loadSourceSnapshot(ctx, tx, 18, 12, at); err == nil {
		t.Fatal("cross tenant source read")
	}
	t.Run("control_and_noise_separated_in_real_store", func(t *testing.T) {
		if _, err := tx.Exec(ctx, `UPDATE recordings SET cleaned_transcription='[{"text":"前文[转写过长，已截断]后文","speaker":"doctor"},{"text":"……！？","speaker":"customer"}]',transcription_segments='[{"text":"前文[转写过长，已截断]后文","start_time":2}]'`); err != nil {
			t.Fatal(err)
		}
		cleaned, err := loadSourceSnapshot(ctx, tx, 17, 12, at)
		if err != nil {
			t.Fatal(err)
		}
		if len(cleaned.Material.Transcript) != 2 || cleaned.Material.Transcript[0].Text != "前文" || cleaned.Material.Transcript[1].Text != "后文" || !cleaned.Material.Context.Truncated || cleaned.Material.Context.Coverage != "partial" {
			t.Fatal("real source normalization failed")
		}
		for _, part := range cleaned.Material.Transcript {
			if part.Speaker != "医生" || part.StartMS != nil {
				t.Fatal("source role/time was guessed")
			}
		}
		var audit map[string]any
		if json.Unmarshal(cleaned.Audit, &audit) != nil || audit["source_loader_version"] != "deal-source-v2.0.2" || !strings.Contains(audit["cleaned_raw"].(string), truncationControl) {
			t.Fatal("version/original source audit missing")
		}
		if _, err := tx.Exec(ctx, `UPDATE recordings SET cleaned_transcription='[{"text":"……！？","speaker":"customer"}]'`); err != nil {
			t.Fatal(err)
		}
		noise, err := loadSourceSnapshot(ctx, tx, 17, 12, at)
		if err != nil || len(noise.Material.Transcript) != 0 || noise.Material.Context.Coverage != "unknown" || noise.Material.Context.Truncated {
			t.Fatal("noise was mistaken for missing dialogue or corruption", err)
		}
	})
	before := bytes.Clone(s.Audit)
	if _, err := tx.Exec(ctx, `UPDATE products SET name='变更'; UPDATE recordings SET cleaned_transcription='[]'`); err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(before, s.Audit) || s.Material.Products[0].Name != "批准产品" {
		t.Fatal("frozen source changed")
	}
	empty, err := loadSourceSnapshot(ctx, tx, 17, 12, at)
	if err != nil || len(empty.Material.Transcript) != 0 {
		t.Fatal("valid empty source failed or silently restored old transcript", err)
	}
}

func TestPhase3PostgresShared32SourcesReadOnly(t *testing.T) {
	dsn := strings.TrimSpace(os.Getenv("DEAL_PHASE3_DATABASE_URL"))
	if dsn == "" {
		t.Skip("set DEAL_PHASE3_DATABASE_URL for current-source validation")
	}
	ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
	defer cancel()
	pool, err := pgxpool.New(ctx, dsn)
	if err != nil {
		t.Fatal(err)
	}
	defer pool.Close()
	ids := []int64{12, 13, 14, 15, 16, 17, 18, 19, 20, 21, 22, 23, 24, 25, 26, 27, 28, 29, 30, 31, 32, 33, 34, 35, 36, 37, 38, 39, 40, 41, 42, 43}
	recordings := map[int64]bool{}
	for _, id := range ids {
		s, err := LoadSourceSnapshot(ctx, pool, 17, id, time.Now().UTC())
		if err != nil {
			t.Fatalf("analysis %d: %v", id, err)
		}
		recordings[s.Material.Recording.RecordingID] = true
		if s.Material.Context.ContactAuthorized != nil {
			t.Fatal("contact inferred")
		}
		var audit map[string]any
		if json.Unmarshal(s.Audit, &audit) != nil || audit["source_mode"] != "current_sources_shadow" {
			t.Fatal("missing source audit")
		}
		t.Logf("analysis_id=%d recording_id=%d sources=%d facts=%d products=%d coverage=%s", id, s.Material.Recording.RecordingID, len(s.Material.Transcript), len(s.Material.Facts), len(s.Material.Products), s.Material.Context.Coverage)
	}
	if len(recordings) != 31 {
		t.Fatalf("distinct recordings=%d", len(recordings))
	}
	if _, err := LoadSourceSnapshot(ctx, pool, 18, 12, time.Now().UTC()); err == nil || !strings.Contains(err.Error(), pgx.ErrNoRows.Error()) {
		t.Fatal("cross tenant source not rejected", err)
	}
}

func TestSourceTranscriptAcceptsLegacyDoubleEncodedSegments(t *testing.T) {
	// 历史 recordings.transcription_segments 可能把分段数组再编码成 JSON 字符串。
	asr := []byte(`"[{\"text\":\"原话。\",\"start_time\":1.235}]"`)
	got, trace, _, _, err := decodeSourceTranscript([]byte(`[{"text":"原话。","speaker":"customer"}]`), asr)
	if err != nil {
		t.Fatalf("legacy double-encoded segments rejected: %v", err)
	}
	if len(got) != 1 || got[0].StartMS == nil || *got[0].StartMS != 1235 {
		t.Fatalf("legacy timing not recovered: %+v", got)
	}
	if len(trace) != 1 || trace[0].ASRIndex == nil || *trace[0].ASRIndex != 0 {
		t.Fatalf("legacy mapping not recorded: %+v", trace)
	}
	for _, raw := range []string{`"[{\"text\":\"a\"}] trailing"`, `"not json"`, `"{}"`} {
		if _, _, _, _, err := decodeSourceTranscript([]byte(`[{"text":"a"}]`), []byte(raw)); err == nil {
			t.Fatalf("malformed legacy ASR accepted: %s", raw)
		}
	}
}
