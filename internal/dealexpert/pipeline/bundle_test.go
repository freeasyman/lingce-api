package pipeline

import (
	"bytes"
	"encoding/json"
	"strings"
	"sync"
	"testing"

	"github.com/freeasyman/lingce-api/internal/dealexpert/compiler"
)

func base(t *testing.T) Bundle {
	t.Helper()
	b, err := DefaultBundle()
	if err != nil {
		t.Fatal(err)
	}
	return b
}

func TestBundleReplayAndIsolation(t *testing.T) {
	b := base(t)
	raw := b.JSON()
	again, err := ReadBundle(raw)
	if err != nil {
		t.Fatal(err)
	}
	if b.ID() != again.ID() || b.ContractHash() != ContractHash() {
		t.Fatal("bundle hash changed")
	}
	raw[0] = '!'
	if b.JSON()[0] != '{' {
		t.Fatal("external mutation changed bundle")
	}
	i, _, _ := compiler.Contracts()
	i[0] = '!'
	i, _, _ = compiler.Contracts()
	if i[0] != '{' {
		t.Fatal("external mutation changed compiler")
	}
	if err := CheckVersion(b, again); err != nil {
		t.Fatal(err)
	}
	var wg sync.WaitGroup
	for n := 0; n < 20; n++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			c, err := DefaultBundle()
			if err != nil || !bytes.Equal(b.JSON(), c.JSON()) {
				t.Error("nondeterministic bundle")
			}
		}()
	}
	wg.Wait()
}

func TestImmutableVersionAndTenantFork(t *testing.T) {
	b := base(t)
	s := b.Spec()
	s.SystemPrompt += "changed"
	changed, err := NewBundle(s)
	if err != nil {
		t.Fatal(err)
	}
	if changed.ID() == b.ID() {
		t.Fatal("changed prompt retained hash")
	}
	if err := CheckVersion(b, changed); err == nil {
		t.Fatal("same version silently overwritten")
	}
	fork, err := b.Fork(17, "tenant17-bundle-v2.0.0", "tenant17-prompt-v2.0.0", s.SystemPrompt, s.UserTemplate)
	if err != nil {
		t.Fatal(err)
	}
	if fork.ID() == b.ID() || fork.Spec().ParentBundleID != b.ID() || fork.ContractHash() != b.ContractHash() {
		t.Fatal("fork lost identity or contract")
	}
	for _, versions := range [][2]string{{b.Spec().Version, "new"}, {"new", b.Spec().PromptVersion}} {
		if _, err := b.Fork(17, versions[0], versions[1], s.SystemPrompt, s.UserTemplate); err == nil {
			t.Fatal("tenant reused parent version")
		}
	}
	if _, err := fork.Fork(18, "v3", "p3", s.SystemPrompt, s.UserTemplate); err == nil {
		t.Fatal("tenant fork became global parent")
	}
}

func TestBundleRejectsUntrackedContracts(t *testing.T) {
	b := base(t)
	for _, field := range []string{"system_prompt", "input_schema", "intermediate_schema", "final_schema", "contract_hash", "pipeline_version", "rules_version"} {
		t.Run(field, func(t *testing.T) {
			var d map[string]any
			if err := json.Unmarshal(b.JSON(), &d); err != nil {
				t.Fatal(err)
			}
			d[field] = "modified"
			raw, err := json.Marshal(d)
			if err != nil {
				t.Fatal(err)
			}
			if _, err := ReadBundle(raw); err == nil {
				t.Fatal("tampered bundle accepted")
			}
		})
	}
	for _, raw := range [][]byte{[]byte(`{`), append(b.JSON(), []byte(` {}`)...), bytes.Replace(b.JSON(), []byte(`"tenant_id":0`), []byte(`"tenant_id":0,"tenant_id":0`), 1)} {
		if _, err := ReadBundle(raw); err == nil {
			t.Fatal("invalid or noncanonical bytes accepted")
		}
	}
}

func TestBundleRejectsBadSpec(t *testing.T) {
	for _, change := range []func(*BundleSpec){func(s *BundleSpec) { s.TenantID = -1 }, func(s *BundleSpec) { s.Version = "" }, func(s *BundleSpec) { s.PromptVersion = "" }, func(s *BundleSpec) { s.SystemPrompt = " " }, func(s *BundleSpec) { s.UserTemplate = "no placeholder" }, func(s *BundleSpec) { s.UserTemplate = "{{input_json}}{{input_json}}" }, func(s *BundleSpec) { s.TenantID = 17 }, func(s *BundleSpec) { s.ParentBundleID = "sha256:" + strings.Repeat("a", 64) }} {
		s := base(t).Spec()
		change(&s)
		if _, err := NewBundle(s); err == nil {
			t.Fatal("invalid spec accepted")
		}
	}
}
