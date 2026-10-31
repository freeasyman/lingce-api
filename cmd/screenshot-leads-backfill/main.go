// screenshot-leads-backfill previews historical screenshots by default. Applying
// requires an explicit list of reviewed source IDs and never changes a binding.
package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/freeasyman/lingce-api/internal/config"
	"github.com/freeasyman/lingce-api/internal/patientops"
	"github.com/jackc/pgx/v5/pgxpool"
)

func main() {
	cfgPath := flag.String("config", "configs/dev.toml", "config file")
	tenant := flag.Int64("tenant", 0, "required tenant ID")
	idsArg := flag.String("ids", "", "comma-separated source IDs; required with -apply")
	apply := flag.Bool("apply", false, "create leads only for the exact reviewed IDs")
	reviewedArg := flag.String("reviewed-pending-ids", "", "explicit IDs whose pending payload was individually reviewed; must also be in -ids")
	operator := flag.Int64("operator", 0, "operator ID required with -apply")
	flag.Parse()
	if *tenant <= 0 {
		fail(fmt.Errorf("-tenant is required"))
	}
	var ids []int64
	if strings.TrimSpace(*idsArg) != "" {
		for _, part := range strings.Split(*idsArg, ",") {
			id, err := strconv.ParseInt(strings.TrimSpace(part), 10, 64)
			if err != nil || id <= 0 {
				fail(fmt.Errorf("invalid source ID"))
			}
			ids = append(ids, id)
		}
	}
	if *apply && (len(ids) == 0 || *operator <= 0) {
		fail(fmt.Errorf("-apply requires reviewed -ids and -operator"))
	}
	var reviewed []int64
	if strings.TrimSpace(*reviewedArg) != "" {
		for _, part := range strings.Split(*reviewedArg, ",") {
			id, err := strconv.ParseInt(strings.TrimSpace(part), 10, 64)
			if err != nil || id <= 0 {
				fail(fmt.Errorf("invalid reviewed pending ID"))
			}
			reviewed = append(reviewed, id)
		}
	}
	if len(reviewed) > 0 && !*apply {
		fail(fmt.Errorf("-reviewed-pending-ids requires -apply; preview already lists reviewable sources"))
	}
	cfg, err := config.Load(*cfgPath)
	if err != nil {
		fail(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()
	pool, err := pgxpool.New(ctx, cfg.Database.DSN())
	if err != nil {
		fail(err)
	}
	defer pool.Close()
	service := patientops.NewService(pool, nil)
	if *apply {
		result, err := service.BackfillReviewedScreenshotLeads(ctx, *tenant, *operator, ids, reviewed)
		if err != nil {
			fail(err)
		}
		if err := json.NewEncoder(os.Stdout).Encode(result); err != nil {
			fail(err)
		}
		return
	}
	tx, err := pool.Begin(ctx)
	if err != nil {
		fail(err)
	}
	defer tx.Rollback(ctx)
	if _, err := tx.Exec(ctx, `SET TRANSACTION READ ONLY`); err != nil {
		fail(err)
	}
	rows, err := tx.Query(ctx, `SELECT s.id,s.customer_id,s.match_status,s.analysis_status,s.recognition_payload,
 s.customer_id IS NOT NULL AND (c.id IS NULL OR c.deleted_at IS NOT NULL)
 FROM patient_conversation_sources s LEFT JOIN customers c ON c.id=s.customer_id AND c.tenant_id=s.tenant_id
 WHERE s.tenant_id=$1 AND s.source_type='screenshot' AND (COALESCE(cardinality($2::bigint[]),0)=0 OR s.id=ANY($2)) ORDER BY s.id`, *tenant, ids)
	if err != nil {
		fail(err)
	}
	defer rows.Close()
	type item struct {
		SourceID              int64                         `json:"source_id"`
		CustomerID            *int64                        `json:"customer_id,omitempty"`
		MatchStatus           string                        `json:"match_status"`
		AnalysisStatus        string                        `json:"analysis_status"`
		Recognition           json.RawMessage               `json:"recognition"`
		Category              string                        `json:"category"`
		NormalizedRecognition patientops.ScreenshotAnalysis `json:"normalized_recognition"`
	}
	result := struct {
		TenantID int64          `json:"tenant_id"`
		ReadOnly bool           `json:"read_only"`
		Counts   map[string]int `json:"counts"`
		Items    []item         `json:"items"`
	}{*tenant, true, map[string]int{}, []item{}}
	for rows.Next() {
		var x item
		var deleted bool
		if err := rows.Scan(&x.SourceID, &x.CustomerID, &x.MatchStatus, &x.AnalysisStatus, &x.Recognition, &deleted); err != nil {
			fail(err)
		}
		if err := json.Unmarshal(x.Recognition, &x.NormalizedRecognition); err != nil {
			fail(err)
		}
		x.NormalizedRecognition.Normalize()
		switch {
		case deleted:
			x.Category = "linked_deleted_customer"
		case x.CustomerID != nil:
			x.Category = "linked_live_customer"
		case x.MatchStatus == "rejected":
			x.Category = "rejected"
		case x.AnalysisStatus == "pending" && patientops.ScreenshotPendingPayloadReviewable(x.Recognition):
			x.Category = "pending_payload_requires_review"
		case x.AnalysisStatus != "succeeded":
			x.Category = "analysis_incomplete"
		case x.NormalizedRecognition.Platform == "" && x.NormalizedRecognition.Account == "" && x.NormalizedRecognition.Nickname == "":
			x.Category = "no_identity_clue"
		default:
			x.Category = "eligible_unbound"
		}
		result.Counts[x.Category]++
		result.Items = append(result.Items, x)
	}
	if err := rows.Err(); err != nil {
		fail(err)
	}
	enc := json.NewEncoder(os.Stdout)
	enc.SetIndent("", "  ")
	if err := enc.Encode(result); err != nil {
		fail(err)
	}
}
func fail(err error) { fmt.Fprintln(os.Stderr, err); os.Exit(1) }
