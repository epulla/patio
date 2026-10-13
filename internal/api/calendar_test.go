package api

import (
	"testing"
	"time"
)

func TestParseCalendarTimeTimedValues(t *testing.T) {
	previousLocal := time.Local
	time.Local = time.FixedZone("test", -5*60*60)
	t.Cleanup(func() { time.Local = previousLocal })

	tests := []struct {
		name     string
		value    string
		want     string
		wantTime time.Time
		wantErr  string
	}{
		{
			name:     "offset",
			value:    "2026-10-13T10:00:00-05:00",
			want:     "2026-10-13T15:00:00.000Z",
			wantTime: time.Date(2026, 10, 13, 15, 0, 0, 0, time.UTC),
		},
		{
			name:     "local time",
			value:    "2026-10-13T10:00:00",
			want:     "2026-10-13T15:00:00.000Z",
			wantTime: time.Date(2026, 10, 13, 10, 0, 0, 0, time.Local),
		},
		{
			name:    "invalid",
			value:   "garbage",
			wantErr: "times need a timezone offset or local time, e.g. 2026-10-13T10:00:00-05:00 or 2026-10-13T10:00:00; use YYYY-MM-DD for all-day items",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, parsed, err := parseCalendarTime(tt.value, false)
			if tt.wantErr != "" {
				if err == nil || err.Error() != tt.wantErr {
					t.Fatalf("error = %v, want %q", err, tt.wantErr)
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			if got != tt.want {
				t.Errorf("stored value = %q, want %q", got, tt.want)
			}
			if !parsed.Equal(tt.wantTime) {
				t.Errorf("parsed time = %v, want %v", parsed, tt.wantTime)
			}
		})
	}
}
