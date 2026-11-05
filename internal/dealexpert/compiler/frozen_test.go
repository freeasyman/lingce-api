package compiler

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"testing"
)

const frozenManifestHash = "3cd0703b8135968d7c670e62cedd91b259a98a48d95b99e02aeff55fbc6cc5dc"

func checkFrozenV200(root string, raw []byte) error {
	sum := sha256.Sum256(raw)
	if hex.EncodeToString(sum[:]) != frozenManifestHash {
		return fmt.Errorf("frozen manifest changed")
	}
	var files map[string]string
	if err := json.Unmarshal(raw, &files); err != nil {
		return err
	}
	seen := map[string]bool{}
	err := filepath.WalkDir(root, func(path string, entry os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if entry.IsDir() {
			return nil
		}
		name, err := filepath.Rel(root, path)
		if err != nil {
			return err
		}
		want, ok := files[name]
		if !ok {
			return fmt.Errorf("unlisted frozen file %s", name)
		}
		contents, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		got := sha256.Sum256(contents)
		if hex.EncodeToString(got[:]) != want {
			return fmt.Errorf("frozen v2.0.0 changed: %s", name)
		}
		seen[name] = true
		return nil
	})
	if err != nil {
		return err
	}
	if len(seen) != len(files) {
		return fmt.Errorf("missing frozen file")
	}
	return nil
}

func TestFrozenV200Implementation(t *testing.T) {
	raw, err := os.ReadFile("frozen-v200-manifest.json")
	if err != nil {
		t.Fatal(err)
	}
	if err := checkFrozenV200("v200", raw); err != nil {
		t.Fatal(err)
	}
}

func TestFrozenV200RejectsDrift(t *testing.T) {
	raw, err := os.ReadFile("frozen-v200-manifest.json")
	if err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"changed_source", "changed_schema", "added_source", "missing_file", "changed_manifest"} {
		t.Run(name, func(t *testing.T) {
			dir := t.TempDir()
			var files map[string]string
			if err := json.Unmarshal(raw, &files); err != nil {
				t.Fatal(err)
			}
			for path := range files {
				contents, err := os.ReadFile(filepath.Join("v200", path))
				if err != nil {
					t.Fatal(err)
				}
				target := filepath.Join(dir, path)
				if err := os.MkdirAll(filepath.Dir(target), 0700); err != nil {
					t.Fatal(err)
				}
				if err := os.WriteFile(target, contents, 0600); err != nil {
					t.Fatal(err)
				}
			}
			manifest := append([]byte(nil), raw...)
			switch name {
			case "changed_source":
				err = os.WriteFile(filepath.Join(dir, "policy.go"), []byte("package v200"), 0600)
			case "changed_schema":
				err = os.WriteFile(filepath.Join(dir, "card.schema.json"), []byte(`{}`), 0600)
			case "added_source":
				err = os.WriteFile(filepath.Join(dir, "bypass.go"), []byte("package v200"), 0600)
			case "missing_file":
				err = os.Remove(filepath.Join(dir, "schema.go"))
			case "changed_manifest":
				manifest = append(manifest, ' ')
			}
			if err != nil {
				t.Fatal(err)
			}
			if err := checkFrozenV200(dir, manifest); err == nil {
				t.Fatal("accepted frozen implementation drift")
			}
		})
	}
}
