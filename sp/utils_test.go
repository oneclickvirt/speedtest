package sp

import (
	"bytes"
	"context"
	"math"
	"strings"
	"testing"
	"time"
)

func TestFormatMbps(t *testing.T) {
	tests := []struct {
		name  string
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

func TestShowHeadToWritesToCallerWriter(t *testing.T) {
	var output bytes.Buffer
	ShowHeadTo(&output, "en")
	if got := output.String(); got == "" || !strings.Contains(got, "Location") || !strings.Contains(got, "PacketLoss") {
		t.Fatalf("ShowHeadTo output = %q, want the English table header", got)
	}
}

func TestShowHeadToAcceptsNilWriter(t *testing.T) {
	ShowHeadTo(nil, "en")
}

func TestGetDataWithNetworkContextStopsWhenCanceled(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	started := time.Now()
	if got := getDataWithNetworkContext(ctx, "https://example.invalid/servers.csv", "tcp6"); got != "" {
		t.Fatalf("canceled registry load returned %q", got)
	}
	if elapsed := time.Since(started); elapsed > 250*time.Millisecond {
		t.Fatalf("canceled registry load took %s", elapsed)
	}
}
