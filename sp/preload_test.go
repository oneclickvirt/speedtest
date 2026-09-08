package sp

import (
	"context"
	"errors"
	"fmt"
	"sync/atomic"
	"testing"
	"time"

	"github.com/showwin/speedtest-go/speedtest"
)

func TestConcurrentCandidateRankingIsBoundedAndSorted(t *testing.T) {
	targets := make(speedtest.Servers, concurrentCandidateProbeWorkers+4)
	latencies := make(map[*speedtest.Server]time.Duration, len(targets))
	for index := range targets {
		targets[index] = &speedtest.Server{ID: fmt.Sprintf("candidate-%02d", index)}
		latencies[targets[index]] = time.Duration(len(targets)-index) * time.Millisecond
	}

	started := make(chan struct{}, len(targets))
	release := make(chan struct{})
	var active atomic.Int32
	var peak atomic.Int32
	probe := func(ctx context.Context, server *speedtest.Server, _ bool) error {
		current := active.Add(1)
		for {
			previous := peak.Load()
			if current <= previous || peak.CompareAndSwap(previous, current) {
				break
			}
		}
		defer active.Add(-1)
		started <- struct{}{}
		select {
		case <-release:
			server.Latency = latencies[server]
			return nil
		case <-ctx.Done():
			return ctx.Err()
		}
	}

	result := make(chan speedtest.Servers, 1)
	go func() {
		result <- rankSpeedtestTargetsByLatencyConcurrentWithProbe(context.Background(), targets, false, probe)
	}()
	for index := 0; index < concurrentCandidateProbeWorkers; index++ {
		select {
		case <-started:
		case <-time.After(time.Second):
			t.Fatal("candidate probes did not reach the worker limit")
		}
	}
	if got := peak.Load(); got != concurrentCandidateProbeWorkers {
		t.Fatalf("peak concurrent probes = %d, want %d", got, concurrentCandidateProbeWorkers)
	}
	close(release)

	var ranked speedtest.Servers
	select {
	case ranked = <-result:
	case <-time.After(time.Second):
		t.Fatal("candidate ranking did not finish")
	}
	if len(ranked) != len(targets) {
		t.Fatalf("ranked candidates = %d, want %d", len(ranked), len(targets))
	}
	for index, server := range ranked {
		want := fmt.Sprintf("candidate-%02d", len(targets)-index-1)
		if server.ID != want {
			t.Fatalf("ranked[%d] = %q, want %q", index, server.ID, want)
		}
	}
}

func TestConcurrentCandidateRankingStopsQueuedProbesOnCancellation(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	targets := make(speedtest.Servers, concurrentCandidateProbeWorkers*3)
	for index := range targets {
		targets[index] = &speedtest.Server{ID: fmt.Sprintf("candidate-%02d", index)}
	}

	started := make(chan struct{}, concurrentCandidateProbeWorkers)
	var calls atomic.Int32
	probe := func(ctx context.Context, _ *speedtest.Server, _ bool) error {
		calls.Add(1)
		started <- struct{}{}
		<-ctx.Done()
		return ctx.Err()
	}
	result := make(chan speedtest.Servers, 1)
	go func() {
		result <- rankSpeedtestTargetsByLatencyConcurrentWithProbe(ctx, targets, false, probe)
	}()
	select {
	case <-started:
	case <-time.After(time.Second):
		t.Fatal("candidate probe did not start")
	}
	cancel()
	select {
	case <-result:
	case <-time.After(time.Second):
		t.Fatal("candidate ranking did not stop after cancellation")
	}
	if got := calls.Load(); got > concurrentCandidateProbeWorkers {
		t.Fatalf("started %d probes after cancellation, limit is %d", got, concurrentCandidateProbeWorkers)
	}
}

func TestCustomSpeedtestPreloadReportsCanceledParent(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	preload := StartCustomSpeedTestPreload(ctx, "https://example.invalid/servers.csv", "id", "tcp4")
	if err := preload.Wait(context.Background()); !errors.Is(err, context.Canceled) {
		t.Fatalf("preload error = %v, want context cancellation", err)
	}
}

func TestCustomSpeedtestPreloadWaitPrefersCallerCancellation(t *testing.T) {
	preload := &CustomSpeedTestPreload{done: make(chan struct{})}
	close(preload.done)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if err := preload.Wait(ctx); !errors.Is(err, context.Canceled) {
		t.Fatalf("wait error = %v, want context cancellation", err)
	}
}
