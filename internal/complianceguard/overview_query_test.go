package complianceguard

import "testing"

func TestRecentCheckItemsConditionParameters(t *testing.T) {
	query, args := recentCheckItemsCondition(7, "confirmed")
	if query == "" || len(args) != 1 || args[0] != int64(7) {
		t.Fatalf("confirmed query args = %q %#v", query, args)
	}
	query, args = recentCheckItemsCondition(7, "pending")
	if query == "" || len(args) != 2 || args[0] != int64(7) || args[1] != "pending" {
		t.Fatalf("pending query args = %q %#v", query, args)
	}
}
