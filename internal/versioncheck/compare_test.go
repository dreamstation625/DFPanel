package versioncheck

import "testing"

func TestCompare(t *testing.T) {
	tests := []struct {
		left, right string
		want        int
		valid       bool
	}{
		{"0.0.1-beta.17", "0.0.1-beta.18", -1, true},
		{"0.0.1-beta.17", "0.0.1", -1, true},
		{"0.0.1", "0.0.1-rc.1", 1, true},
		{"v1.2.0", "1.1.9", 1, true},
		{"1.0.0-beta.2", "1.0.0-beta.11", -1, true},
		{"1.0.0-beta.02", "1.0.0-beta.2", 0, true},
		{"1.0.0-beta", "1.0.0-beta.1", -1, true},
		{"dev", "1.0.0", 0, false},
		{"", "1.0.0", 0, false},
	}
	for _, test := range tests {
		got, valid := Compare(test.left, test.right)
		if got != test.want || valid != test.valid {
			t.Errorf("Compare(%q, %q) = (%d, %t), want (%d, %t)",
				test.left, test.right, got, valid, test.want, test.valid)
		}
	}
}
