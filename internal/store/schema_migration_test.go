package store

import (
	"strconv"
	"testing"
)

func TestLoadEmbeddedSchemaMigrations(t *testing.T) {
	t.Parallel()

	migrations, err := loadEmbeddedSchemaMigrations("")
	if err != nil {
		t.Fatalf("load embedded schema migrations: %v", err)
	}
	if len(migrations) == 0 {
		t.Fatalf("expected embedded migrations")
	}

	prev := ""
	prevDate := ""
	prevSeq := 0
	seen := map[string]struct{}{}
	for i, migration := range migrations {
		if migration.Version == "" {
			t.Fatalf("migration %d has empty version", i)
		}
		if migration.Name == "" {
			t.Fatalf("migration %s has empty name", migration.Version)
		}
		if migration.Path == "" {
			t.Fatalf("migration %s has empty path", migration.Version)
		}
		if migration.SQL == "" {
			t.Fatalf("migration %s has empty SQL", migration.Version)
		}
		if migration.Checksum == "" {
			t.Fatalf("migration %s has empty checksum", migration.Version)
		}
		if prev != "" && migration.Version < prev {
			t.Fatalf("migrations not sorted: %s before %s", prev, migration.Version)
		}
		datePart := migration.Version[:8]
		seqPart := migration.Version[9:]
		seq, err := strconv.Atoi(seqPart)
		if err != nil {
			t.Fatalf("migration %s has invalid sequence: %v", migration.Version, err)
		}
		if datePart == prevDate && seq != prevSeq+1 {
			t.Fatalf("migration sequence gap on %s: expected %03d, got %s", datePart, prevSeq+1, seqPart)
		}
		if _, ok := seen[migration.Version]; ok {
			t.Fatalf("duplicate migration version %s", migration.Version)
		}
		seen[migration.Version] = struct{}{}
		prev = migration.Version
		prevDate = datePart
		prevSeq = seq
	}

	if _, ok := seen["20260807_001"]; !ok {
		t.Fatalf("expected baseline migration 20260807_001")
	}
}
