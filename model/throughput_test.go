package model

import (
	"context"
	"testing"
)

func TestBenchmarkServersUsesLimitAndPreservesOrder(t *testing.T) {
	servers := []ServerMetadata{{ID: "a", Name: "A"}, {ID: "b", Name: "B"}, {ID: "c", Name: "C"}}
	results := BenchmarkServers(context.Background(), servers, 2, func(_ context.Context, server ServerMetadata) ThroughputResult {
		return ThroughputResult{ID: server.ID, Name: server.Name, Status: ThroughputAvailable, DownloadMbps: 100, UploadMbps: 50}
	})
	if len(results) != 2 || results[0].ID != "a" || results[1].ID != "b" {
		t.Fatalf("unexpected benchmark order: %+v", results)
	}
}

func TestBenchmarkServersMarksRemainingCanceledWithoutCallingProbe(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	calls := 0
	results := BenchmarkServers(ctx, []ServerMetadata{{ID: "a"}, {ID: "b"}}, 2, func(_ context.Context, server ServerMetadata) ThroughputResult {
		calls++
		cancel()
		return ThroughputResult{ID: server.ID, Status: ThroughputAvailable}
	})
	if calls != 1 || len(results) != 2 || results[1].Status != ThroughputCanceled {
		t.Fatalf("unexpected canceled benchmark: calls=%d results=%+v", calls, results)
	}
}

func TestBenchmarkServersContinuesUntilSuccessTarget(t *testing.T) {
	calls := 0
	results := BenchmarkServers(context.Background(), []ServerMetadata{{ID: "first"}, {ID: "second"}}, 1, func(_ context.Context, server ServerMetadata) ThroughputResult {
		calls++
		if server.ID == "first" {
			return ThroughputResult{ID: server.ID, Status: ThroughputUnavailable}
		}
		return ThroughputResult{ID: server.ID, Status: ThroughputAvailable}
	})
	if calls != 2 || len(results) != 2 || results[1].Status != ThroughputAvailable {
		t.Fatalf("calls=%d results=%+v, want failed candidate followed by success", calls, results)
	}
}

func TestBenchmarkServersCapsAttemptsAtTwiceSuccessTarget(t *testing.T) {
	servers := []ServerMetadata{{ID: "one"}, {ID: "two"}, {ID: "three"}, {ID: "four"}}
	calls := 0
	results := BenchmarkServers(context.Background(), servers, 1, func(_ context.Context, server ServerMetadata) ThroughputResult {
		calls++
		return ThroughputResult{ID: server.ID, Status: ThroughputUnavailable}
	})
	if calls != 2 || len(results) != 2 {
		t.Fatalf("calls/results = %d/%d, want 2/2", calls, len(results))
	}
}

func TestProbeThroughputRejectsMissingURLWithoutNetwork(t *testing.T) {
	result := ProbeThroughput(context.Background(), ServerMetadata{ID: "fixture"})
	if result.Status != ThroughputUnavailable || result.Error == "" {
		t.Fatalf("unexpected missing URL result: %+v", result)
	}
}
