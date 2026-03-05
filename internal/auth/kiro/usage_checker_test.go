package kiro

import (
	"testing"
	"time"
)

func TestParseUnixTimestampAuto(t *testing.T) {
	tests := []struct {
		name  string
		input float64
		want  time.Time
	}{
		{
			name:  "seconds",
			input: 1741150800,
			want:  time.Unix(1741150800, 0),
		},
		{
			name:  "milliseconds",
			input: 1741150800000,
			want:  time.Unix(1741150800, 0),
		},
		{
			name:  "microseconds",
			input: 1741150800000000,
			want:  time.Unix(1741150800, 0),
		},
		{
			name:  "nanoseconds",
			input: 1741150800000000000,
			want:  time.Unix(1741150800, 0),
		},
		{
			name:  "zero",
			input: 0,
			want:  time.Time{},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := parseUnixTimestampAuto(tt.input)
			if !got.Equal(tt.want) {
				t.Fatalf("parseUnixTimestampAuto(%v) = %v, want %v", tt.input, got, tt.want)
			}
		})
	}
}
