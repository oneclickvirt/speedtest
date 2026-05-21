package sp

import (
	"math"
	"testing"
)

func TestFormatMbps(t *testing.T) {
	tests := []struct {
		name string
		value float64
		want  string
	}{
		{name: "negative", value: -0.0001, want: "0.00 Mbps"},
		{name: "nan", value: math.NaN(), want: "0.00 Mbps"},
		{name: "positive", value: 293.075, want: "293.08 Mbps"},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := formatMbps(tc.value)
			if got != tc.want {
				t.Fatalf("formatMbps(%v) = %q, want %q", tc.value, got, tc.want)
			}
		})
	}
}