package api

import "testing"

func TestEffortForNearestSupported(t *testing.T) {
	tests := []struct {
		level     string
		supported []string
		want      string
	}{
		{"default", []string{"low"}, ""},
		{"small", []string{"medium", "high"}, "medium"},
		{"xhigh", []string{"low", "high"}, "high"},
		{"high", []string{"custom"}, ""},
		{"high", []string{"unknown", "xhigh"}, "xhigh"},
	}
	for _, test := range tests {
		if got := effortFor(test.level, test.supported); got != test.want {
			t.Errorf("effortFor(%q, %v)=%q, want %q", test.level, test.supported, got, test.want)
		}
	}
}
