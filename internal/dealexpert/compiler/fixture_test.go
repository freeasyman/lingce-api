package compiler

import (
	"encoding/json"
	"os"
	"testing"
)

func fixture(t *testing.T, name string) (map[string]any, map[string]any) {
	t.Helper()
	var env, model map[string]any
	for _, item := range []struct {
		suffix string
		dst    *map[string]any
	}{{"input", &env}, {"output", &model}} {
		raw, err := os.ReadFile("testdata/" + name + "-" + item.suffix + ".json")
		if err != nil {
			t.Fatal(err)
		}
		if err := json.Unmarshal(raw, item.dst); err != nil {
			t.Fatal(err)
		}
	}
	return env, model
}
func encode(t *testing.T, v any) []byte {
	t.Helper()
	raw, err := json.Marshal(v)
	if err != nil {
		t.Fatal(err)
	}
	return raw
}
func seal(t *testing.T, e, m map[string]any) {
	t.Helper()
	id, err := SnapshotID(encode(t, e["input"]))
	if err != nil {
		t.Fatal(err)
	}
	e["input_snapshot_id"] = id
	m["input_snapshot_id"] = id
}
