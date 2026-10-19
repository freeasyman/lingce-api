package store

import (
	"regexp"
	"strings"
	"testing"
)

func TestCurrentBaselinePreservesSerialSequenceOwnership(t *testing.T) {
	script, err := loadConvergenceScript("20261012_001", "20261012_001.sql")
	if err != nil {
		t.Fatal(err)
	}
	defaults := regexp.MustCompile(`ALTER TABLE public\.(\w+) ALTER COLUMN (\w+) SET DEFAULT nextval\('public\.(\w+)'::regclass\);`).FindAllStringSubmatch(script.SQL, -1)
	if len(defaults) == 0 {
		t.Fatal("baseline has no serial defaults to verify")
	}
	for _, match := range defaults {
		ownership := "ALTER SEQUENCE public." + match[3] + " OWNED BY public." + match[1] + "." + match[2] + ";"
		if !strings.Contains(script.SQL, ownership) {
			t.Errorf("serial sequence lookup would fail: missing %s", ownership)
		}
	}
}
