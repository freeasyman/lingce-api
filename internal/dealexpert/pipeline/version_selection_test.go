package pipeline

import (
	"bytes"
	"encoding/json"
	"strings"
	"testing"

	"github.com/freeasyman/lingce-api/internal/dealexpert/compiler"
)

func TestBundleSelectsFrozenRules(t *testing.T) {
	current := base(t)
	selected, err := NewBundleForRules(current.Spec(), "deal-compiler-v2.0.0")
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(current.JSON(), selected.JSON()) || current.ID() != selected.ID() {
		t.Fatal("explicit old rule changed bundle bytes")
	}
	if selected.RulesVersion() != compiler.RulesVersion || selected.Contract().Version() != selected.RulesVersion() {
		t.Fatal("bundle lost rule selection")
	}
	_, expected, _ := selected.Contract().Contracts()
	r, _ := sampleRun(t)
	req, _, err := r.Request(selected, 1, "", "")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasSuffix(req.Messages[0].Content, "JSON Schema:\n"+string(expected)) {
		t.Fatal("request schema not from frozen bundle")
	}
	loaded, err := ReadBundle(selected.JSON())
	if err != nil || loaded.RulesVersion() != selected.RulesVersion() {
		t.Fatalf("rule did not survive read: %v", err)
	}
	var doc bundleDocument
	if err := json.Unmarshal(selected.JSON(), &doc); err != nil {
		t.Fatal(err)
	}
	if doc.RulesVersion != selected.RulesVersion() {
		t.Fatal("serialized rule mismatch")
	}
}

func TestBundleUnknownRuleCannotUseCurrentSchemas(t *testing.T) {
	spec := base(t).Spec()
	for _, version := range []string{"", "future", "deal-compiler-v2.0.1"} {
		if _, err := NewBundleForRules(spec, version); err == nil {
			t.Fatal("unknown rule fell back to current")
		}
	}
}

func TestSelectV210BundleIsExplicitAndDistinct(t *testing.T) {
	spec := base(t).Spec()
	b, err := NewBundleForRules(spec, "deal-compiler-v2.1.0-rc7")
	if err != nil {
		t.Fatal(err)
	}
	if b.RulesVersion() != "deal-compiler-v2.1.0-rc7" {
		t.Fatal(b.RulesVersion())
	}
	if b.ContractHash() == ContractHash() {
		t.Fatal("v2.1 contract hash reused v2.0")
	}
	if _, err := NewBundleForRules(spec, "deal-compiler-v2.1.1"); err == nil {
		t.Fatal("unknown v2.1 accepted")
	}
}
