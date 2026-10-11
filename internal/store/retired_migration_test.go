package store

import "testing"

func TestRetiredComplianceMigrationIsVerifiedButNeverExecutable(t *testing.T) {
	migrations, err := loadEmbeddedSchemaMigrations("")
	if err != nil {
		t.Fatal(err)
	}
	for _, m := range migrations {
		if m.Version == "20261009_004" {
			t.Fatal("retired prompt SQL remains executable")
		}
	}
	if err := validateNoUnknownAppliedMigrations(map[string]string{"20261009_004": retiredSchemaMigrations["20261009_004"]}, migrations); err != nil {
		t.Fatal(err)
	}
	if err := validateNoUnknownAppliedMigrations(map[string]string{"20261009_004": "changed"}, migrations); err == nil {
		t.Fatal("retired checksum mismatch accepted")
	}
	if err := validateNoUnknownAppliedMigrations(map[string]string{"20990101_001": "unknown"}, migrations); err == nil {
		t.Fatal("unknown migration accepted")
	}
	if err := validateNoUnknownAppliedMigrations(map[string]string{}, migrations); err != nil {
		t.Fatal(err)
	}
}
