// Package pipeline owns versioned invocations around the pure card compiler.
// A bundle binds the prompt and exact executable contracts, not queue labels.
package pipeline

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"regexp"
	"strings"

	"github.com/freeasyman/lingce-api/internal/dealexpert/compiler"
)

const Version = "deal-pipeline-v2.0.0"

type BundleSpec struct {
	TenantID      int64  `json:"tenant_id"`
	Version       string `json:"version"`
	PromptVersion string `json:"prompt_version"`
	SystemPrompt  string `json:"system_prompt"`
	UserTemplate  string `json:"user_template"`
	// Tenant customization must fork a named global bundle, never overwrite it.
	ParentBundleID string `json:"parent_bundle_id"`
}

type bundleDocument struct {
	BundleSpec
	PipelineVersion    string          `json:"pipeline_version"`
	RulesVersion       string          `json:"rules_version"`
	InputSchema        json.RawMessage `json:"input_schema"`
	IntermediateSchema json.RawMessage `json:"intermediate_schema"`
	FinalSchema        json.RawMessage `json:"final_schema"`
	ContractHash       string          `json:"contract_hash"`
}

// Bundle exposes copies only. Runtime contracts cannot be supplied by callers.
type Bundle struct {
	raw              []byte
	id, contractHash string
	spec             BundleSpec
	contract         compiler.Contract
}

func (b Bundle) ID() string                  { return b.id }
func (b Bundle) ContractHash() string        { return b.contractHash }
func (b Bundle) Spec() BundleSpec            { return b.spec }
func (b Bundle) JSON() []byte                { return bytes.Clone(b.raw) }
func (b Bundle) RulesVersion() string        { return b.contract.Version() }
func (b Bundle) Contract() compiler.Contract { return b.contract }

var hashPattern = regexp.MustCompile(`^sha256:[a-f0-9]{64}$`)
var versionPattern = regexp.MustCompile(`^[a-zA-Z0-9][a-zA-Z0-9._-]{0,127}$`)

func digest(raw []byte) string {
	sum := sha256.Sum256(raw)
	return "sha256:" + hex.EncodeToString(sum[:])
}

// ContractHash is computed from the actual schemas and rule/pipeline versions
// embedded in this executable. Semantic compiler changes require a rule bump.
func ContractHash() string {
	contract, _ := compiler.SelectContract(compiler.RulesVersion)
	return contractHash(Version, contract)
}

func contractHash(pipelineVersion string, contract compiler.Contract) string {
	i, m, f := contract.Contracts()
	raw, _ := json.Marshal(struct {
		Pipeline, Rules            string
		Input, Intermediate, Final json.RawMessage
	}{pipelineVersion, contract.Version(), i, m, f})
	return digest(raw)
}

func NewBundle(spec BundleSpec) (Bundle, error) {
	return NewBundleForRules(spec, compiler.RulesVersion)
}

// NewBundleForRules explicitly selects a supported executable contract. The
// bundle wire format stays unchanged, preserving all published v2.0.0 hashes.
func NewBundleForRules(spec BundleSpec, rulesVersion string) (Bundle, error) {
	selected, err := compiler.SelectContract(rulesVersion)
	if err != nil {
		return Bundle{}, err
	}
	if spec.TenantID < 0 || !versionPattern.MatchString(spec.Version) || !versionPattern.MatchString(spec.PromptVersion) {
		return Bundle{}, fmt.Errorf("invalid bundle scope or version")
	}
	if strings.TrimSpace(spec.SystemPrompt) == "" || strings.Count(spec.UserTemplate, "{{input_json}}") != 1 {
		return Bundle{}, fmt.Errorf("prompt and exactly one input placeholder are required")
	}
	if spec.TenantID == 0 && spec.ParentBundleID != "" {
		return Bundle{}, fmt.Errorf("global bundle cannot have a tenant parent")
	}
	if spec.TenantID > 0 && !hashPattern.MatchString(spec.ParentBundleID) {
		return Bundle{}, fmt.Errorf("tenant bundle requires an immutable parent id")
	}
	i, m, f := selected.Contracts()
	contract := contractHash(Version, selected)
	doc := bundleDocument{spec, Version, selected.Version(), i, m, f, contract}
	raw, err := json.Marshal(doc)
	if err != nil {
		return Bundle{}, err
	}
	return Bundle{raw, digest(raw), contract, spec, selected}, nil
}

// ReadBundle validates persisted bytes against the executable that will replay
// them. An old compiler must be kept to replay an older contract; silently
// substituting today's schema or prompt is forbidden.
func ReadBundle(raw []byte) (Bundle, error) {
	var doc bundleDocument
	d := json.NewDecoder(bytes.NewReader(raw))
	d.DisallowUnknownFields()
	if err := d.Decode(&doc); err != nil {
		return Bundle{}, err
	}
	if doc.PipelineVersion != Version {
		return Bundle{}, fmt.Errorf("unsupported pipeline version %q", doc.PipelineVersion)
	}
	b, err := NewBundleForRules(doc.BundleSpec, doc.RulesVersion)
	if err != nil {
		return Bundle{}, err
	}
	// Generated bundles have one canonical serialization. This check also rejects
	// duplicate fields, trailing JSON, changed schemas and stale rule versions.
	if !bytes.Equal(b.raw, raw) {
		return Bundle{}, fmt.Errorf("bundle bytes or executable contract mismatch")
	}
	return b, nil
}

// Fork requires both a distinct release version and distinct prompt version.
func (b Bundle) Fork(tenantID int64, version, promptVersion, systemPrompt, userTemplate string) (Bundle, error) {
	if b.id == "" || b.spec.TenantID != 0 || tenantID <= 0 {
		return Bundle{}, fmt.Errorf("tenant forks require a global parent")
	}
	if version == b.spec.Version || promptVersion == b.spec.PromptVersion {
		return Bundle{}, fmt.Errorf("tenant customization requires independent versions")
	}
	return NewBundleForRules(BundleSpec{tenantID, version, promptVersion, systemPrompt, userTemplate, b.ID()}, b.RulesVersion())
}

// Register compares scope+version identity before allowing publication. Storage
// enforces the same condition atomically; this helper is useful for offline
// release checks and does not imply that an in-memory check replaces a DB key.
func CheckVersion(existing, candidate Bundle) error {
	if existing.id == "" || candidate.id == "" {
		return fmt.Errorf("empty bundle")
	}
	if existing.spec.TenantID != candidate.spec.TenantID || existing.spec.Version != candidate.spec.Version {
		return fmt.Errorf("bundle identity mismatch")
	}
	if existing.id != candidate.id {
		return fmt.Errorf("immutable version conflict")
	}
	return nil
}
