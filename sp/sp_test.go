package sp

import (
	"bytes"
	"context"
	"errors"
	"strings"
	"testing"

	showwinspeedtest "github.com/showwin/speedtest-go/speedtest"
)

const officialFixtureWithoutPacketLoss = `
   Idle Latency:     9.25 ms   (jitter: 0.40ms, low: 9.00ms, high: 10.00ms)
       Download:   932.37 Mbps (data used: 1.0 GB)
         Upload:    68.46 Mbps (data used: 100 MB)
`

func TestParseOfficialNearbyMeasurementAllowsMissingPacketLoss(t *testing.T) {
	measurement, ok := parseOfficialNearbyMeasurement(officialFixtureWithoutPacketLoss)
	if !ok {
		t.Fatal("complete throughput result without packet loss was rejected")
	}
	if measurement.Upload != "68.46 Mbps" || measurement.Download != "932.37 Mbps" || measurement.Latency != "9.25 ms" || measurement.PacketLoss != "N/A" {
		t.Fatalf("unexpected parsed measurement: %+v", measurement)
	}
}

func TestOfficialNearbyWritesRowWhenPacketLossIsMissing(t *testing.T) {
	original := runOfficialSpeedtestCommand
	var receivedArgs []string
	runOfficialSpeedtestCommand = func(_ context.Context, args ...string) ([]byte, error) {
		receivedArgs = append([]string(nil), args...)
		return []byte(officialFixtureWithoutPacketLoss), nil
	}
	t.Cleanup(func() { runOfficialSpeedtestCommand = original })

	var output bytes.Buffer
	OfficialNearbySpeedTestWithNetworkContextTo(context.Background(), &output, "tcp4")
	if got := output.String(); !strings.Contains(got, "Speedtest.net") || !strings.Contains(got, "N/A") {
		t.Fatalf("nearby result with missing packet loss was not rendered: %q", got)
	}
	if got := strings.Join(receivedArgs, " "); !strings.Contains(got, "--ip-version=4") {
		t.Fatalf("official nearby test did not preserve IPv4 selection: %q", got)
	}
}

func TestOfficialTargetsContinueAfterFailedCandidate(t *testing.T) {
	original := runOfficialSpeedtestCommand
	calls := 0
	runOfficialSpeedtestCommand = func(context.Context, ...string) ([]byte, error) {
		calls++
		if calls == 1 {
			return nil, errors.New("first candidate unavailable")
		}
		return []byte(officialFixtureWithoutPacketLoss), nil
	}
	t.Cleanup(func() { runOfficialSpeedtestCommand = original })

	var output bytes.Buffer
	completed := officialTargetsSpeedTestContextToWithPreloadedTargets(context.Background(), &output, showwinspeedtest.Servers{
		&showwinspeedtest.Server{ID: "first", Name: "First"},
		&showwinspeedtest.Server{ID: "second", Name: "Second"},
	}, 1, "en", "tcp4", false)
	if completed != 1 || calls != 2 {
		t.Fatalf("completed=%d calls=%d, want one success after two attempts", completed, calls)
	}
	if got := output.String(); !strings.Contains(got, "Second") || strings.Contains(got, "First") {
		t.Fatalf("unexpected official fallback output: %q", got)
	}
}
