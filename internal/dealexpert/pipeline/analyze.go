package pipeline

import (
	"context"
	"fmt"

	"github.com/freeasyman/lingce-api/internal/dealexpert/compiler"
)

type AnalysisRequest struct {
	RunID              string
	Material           InputMaterial
	Bundle             Bundle
	Model              ModelConfig
	PreprocessingAudit []byte
}

type AnalysisResult struct {
	Run  Run
	Card compiler.Result
}

// Analyze is the independent v2 shadow-run entry point. The caller owns input
// acquisition and authorization; this function owns no DB query, task creation,
// clock read or legacy result mutation.
func Analyze(ctx context.Context, req AnalysisRequest, audit AuditWriter, call TextCall) (AnalysisResult, error) {
	if req.RunID == "" {
		return AnalysisResult{}, fmt.Errorf("run id required")
	}
	run, err := NewRunFromInput(req.RunID, req.Material, req.Bundle, req.Model, req.PreprocessingAudit)
	if err != nil {
		return AnalysisResult{}, err
	}
	card, err := (Executor{Audit: audit}).Execute(ctx, run, req.Bundle, call)
	if err != nil {
		return AnalysisResult{Run: run}, err
	}
	return AnalysisResult{Run: run, Card: card}, nil
}
