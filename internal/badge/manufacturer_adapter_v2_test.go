package badge

import "testing"

func TestReadVendorDeviceSnapshots(t *testing.T) {
	data := map[string]interface{}{
		"items": []interface{}{
			map[string]interface{}{"device_no": "XM001"},
			map[string]interface{}{"deviceNo": "XM002"},
			"XM003",
		},
	}
	got := readVendorDeviceSnapshots(data)
	if len(got) != 3 {
		t.Fatalf("len(readVendorDeviceSnapshots)=%d, want=3", len(got))
	}
}

func TestReadIntField(t *testing.T) {
	data := map[string]interface{}{"battery_level": "18"}
	got, ok := readIntField(data, "battery_level")
	if !ok || got != 18 {
		t.Fatalf("readIntField()=(%d,%v), want=(18,true)", got, ok)
	}
}
