package main

import (
	"reflect"
	"testing"
)

func TestParseTenantIDs(t *testing.T) {
	got, err := parseTenantIDs(" 5002, 5001,5002 ,, 5003 ")
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	want := []int64{5001, 5002, 5003}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("ids = %v, want %v", got, want)
	}
	if got, err := parseTenantIDs(""); err != nil || got != nil {
		t.Fatalf("empty = %v, %v; want nil, nil", got, err)
	}
	for _, raw := range []string{"0", "-1", "abc", "5001,x"} {
		if _, err := parseTenantIDs(raw); err == nil {
			t.Fatalf("parseTenantIDs(%q) succeeded, want error", raw)
		}
	}
}

func TestParseOptions(t *testing.T) {
	opts, err := parseOptions([]string{"--config", "./configs/dev.toml", "--tenant-ids", "5001", "--apply"})
	if err != nil {
		t.Fatalf("parse options: %v", err)
	}
	if !opts.Apply || len(opts.TenantIDs) != 1 || opts.TenantIDs[0] != 5001 {
		t.Fatalf("opts = %+v", opts)
	}
	if _, err := parseOptions([]string{"--config", ""}); err == nil {
		t.Fatal("empty config and dsn must be rejected")
	}
}

func TestAssertNotProduction(t *testing.T) {
	for _, database := range []string{"lingce_billing_test", "lingce_dev", "lingce_clean"} {
		if err := assertNotProduction(database); err != nil {
			t.Fatalf("assertNotProduction(%q) = %v, want nil", database, err)
		}
	}
	for _, database := range []string{"lingce_prod", "lingce_prod_20261101", "lingce_prod_schema_base_20261019_008", "prod", "production", ""} {
		if err := assertNotProduction(database); err == nil {
			t.Fatalf("assertNotProduction(%q) = nil, want error", database)
		}
	}
}
