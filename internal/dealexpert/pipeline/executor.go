package pipeline

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/freeasyman/lingce-api/internal/dealexpert/compiler"
	"github.com/freeasyman/lingce-api/pkg/llmgateway"
)

type AuditWriter interface {
	AppendRun(context.Context, Run, Bundle) error
	AppendAttemptEvent(context.Context, AttemptEvent) error
	AppendCompilation(context.Context, string, []byte, string, string, string, []byte) error
}

// TextCall is injected so the ordering contract is testable without a live
// provider. Production passes TextInferenceWithRaw from the gateway client.
type TextCall func(context.Context, llmgateway.TextInferenceRequest) (*llmgateway.TextInferenceResponse, []byte, []byte, error)

type Executor struct{ Audit AuditWriter }

// Execute persists the run and request before a provider call. Every response
// and failure is append-only. A structurally invalid model response receives at
// most one controlled correction request; semantic/model retries are forbidden.
func (e Executor) Execute(ctx context.Context, run Run, bundle Bundle, call TextCall) (compiler.Result, error) {
	var empty compiler.Result
	if e.Audit == nil || call == nil {
		return empty, fmt.Errorf("audit writer and text call are required")
	}
	if err := e.Audit.AppendRun(ctx, run, bundle); err != nil {
		return empty, err
	}
	prior := ""
	validationError := ""
	for attempt := 1; attempt <= 2; attempt++ {
		req, requestRaw, err := run.Request(bundle, attempt, prior, validationError)
		if err != nil {
			return empty, err
		}
		if err := e.Audit.AppendAttemptEvent(ctx, AttemptEvent{RunID: run.ID(), Attempt: attempt, Type: "request", Payload: requestRaw}); err != nil {
			return empty, err
		}
		resp, gatewayRequest, gatewayRaw, callErr := call(ctx, req)
		if len(gatewayRequest) > 0 && string(gatewayRequest) != string(requestRaw) {
			return empty, fmt.Errorf("gateway request differs from audited request")
		}
		if callErr != nil {
			if auditErr := e.Audit.AppendAttemptEvent(ctx, AttemptEvent{RunID: run.ID(), Attempt: attempt, Type: "error", GatewayRaw: string(gatewayRaw), ErrorStage: "gateway", ErrorMessage: callErr.Error()}); auditErr != nil {
				return empty, fmt.Errorf("audit gateway error: %w (original: %v)", auditErr, callErr)
			}
			return empty, callErr
		}
		metadata, _ := json.Marshal(map[string]any{"request_id": resp.RequestID, "usage": resp.Usage, "cost": resp.Cost, "latency_ms": resp.LatencyMS, "provider": resp.Provider, "model_code": resp.ModelCode, "price_version": resp.PriceVersion})
		if err := e.Audit.AppendAttemptEvent(ctx, AttemptEvent{RunID: run.ID(), Attempt: attempt, Type: "response", Payload: metadata, RawContent: resp.Content, GatewayRaw: string(gatewayRaw)}); err != nil {
			return empty, err
		}
		result, compileErr := bundle.Contract().Compile(run.Spec().Snapshot, []byte(resp.Content))
		if compileErr == nil {
			diagnostics, _ := json.Marshal(result.Diagnostics)
			if err := e.Audit.AppendCompilation(ctx, run.ID(), result.Card, result.Status, bundle.RulesVersion(), bundle.ContractHash(), diagnostics); err != nil {
				return empty, err
			}
			return result, nil
		}
		ce, ok := compileErr.(*compiler.Error)
		if !ok || ce.Stage != "intermediate_structure" || attempt == 2 {
			if auditErr := e.Audit.AppendAttemptEvent(ctx, AttemptEvent{RunID: run.ID(), Attempt: attempt, Type: "error", GatewayRaw: string(gatewayRaw), ErrorStage: "compiler", ErrorMessage: compileErr.Error()}); auditErr != nil {
				return empty, fmt.Errorf("audit compiler error: %w (original: %v)", auditErr, compileErr)
			}
			return empty, compileErr
		}
		if auditErr := e.Audit.AppendAttemptEvent(ctx, AttemptEvent{RunID: run.ID(), Attempt: attempt, Type: "error", ErrorStage: "intermediate_structure", ErrorMessage: compileErr.Error()}); auditErr != nil {
			return empty, fmt.Errorf("audit correction error: %w (original: %v)", auditErr, compileErr)
		}
		prior = resp.Content
		validationError = compileErr.Error()
	}
	return empty, fmt.Errorf("unreachable executor state")
}
