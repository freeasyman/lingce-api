package emrrecord

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/freeasyman/lingce-api/internal/emrquality"
)

func TestQualityConfirmationResponsePreservesEditableStatus(t *testing.T) {
	response := httptest.NewRecorder()
	writeActionError(response, &WriteOutcome{
		Record:        &Record{Status: "草稿"},
		QualityReport: &emrquality.Report{Results: []emrquality.RuleResult{{Result: "problem", Blocking: false}}},
	}, fmt.Errorf("complete: %w", ErrQualityConfirmationRequired))
	if response.Code != http.StatusConflict {
		t.Fatalf("status = %d", response.Code)
	}
	var body struct {
		Code    string `json:"code"`
		Details struct {
			Status  string             `json:"status"`
			Quality *emrquality.Report `json:"quality"`
		} `json:"details"`
	}
	if err := json.Unmarshal(response.Body.Bytes(), &body); err != nil {
		t.Fatal(err)
	}
	if body.Code != "EMR_QUALITY_CONFIRMATION_REQUIRED" || body.Details.Status != "草稿" || body.Details.Quality == nil {
		t.Fatalf("unexpected response: %s", response.Body.String())
	}
}

func TestOrdinaryActionErrorDoesNotRequestConfirmation(t *testing.T) {
	response := httptest.NewRecorder()
	writeActionError(response, nil, fmt.Errorf("record not editable"))
	if response.Code != http.StatusBadRequest {
		t.Fatalf("status = %d", response.Code)
	}
}
