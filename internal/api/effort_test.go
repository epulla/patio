package api

import "testing"

func TestValidEffortLevelToken(t *testing.T) {
	for _, tc := range []struct {
		value string
		valid bool
	}{
		{"", true}, {"low", true}, {"thinking_level-2", true},
		{"Upper", false}, {"2high", false}, {"", true},
		{"this-level-token-is-too-long-to-accept", false},
	} {
		if got := validEffort(tc.value); got != tc.valid {
			t.Errorf("validEffort(%q) = %v, want %v", tc.value, got, tc.valid)
		}
	}
	if got := effortFromThinking(false); got != "low" {
		t.Errorf("effortFromThinking(false) = %q, want low", got)
	}
	if got := effortFromThinking(true); got != "" {
		t.Errorf("effortFromThinking(true) = %q, want empty", got)
	}
}
