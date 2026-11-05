package pipeline

import (
	"os"
	"strings"
	"testing"
)

func TestAuditMigrationIsAppendOnlyAndHasRequiredHashes(t *testing.T) {
	raw, err := os.ReadFile("../../store/migrations/20261031_073_deal_expert_v2_audit.sql")
	if err != nil {
		t.Fatal(err)
	}
	s := string(raw)
	for _, needle := range []string{"deal_expert_pipeline_bundles", "deal_expert_analysis_runs", "deal_expert_analysis_attempt_events", "deal_expert_analysis_compilations", "payload_hash", "run_hash", "card_hash", "BEFORE UPDATE OR DELETE", "deal_expert_v2_immutable"} {
		if !strings.Contains(s, needle) {
			t.Fatalf("migration missing %s", needle)
		}
	}
	if strings.Contains(strings.ToLower(s), "on conflict (run_id) do update") || strings.Contains(strings.ToLower(s), "on conflict (bundle_id) do update") {
		t.Fatal("audit migration updates immutable rows")
	}
}
