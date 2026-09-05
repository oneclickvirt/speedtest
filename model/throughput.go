package model

import (
	"context"
	"errors"
	"fmt"
	"net"
	"net/url"
	"strings"
	"time"

	showwinspeedtest "github.com/showwin/speedtest-go/speedtest"
)

type ThroughputStatus string

const (
	ThroughputAvailable   ThroughputStatus = "available"
	ThroughputUnavailable ThroughputStatus = "unavailable"
	ThroughputTimeout     ThroughputStatus = "timeout"
	ThroughputCanceled    ThroughputStatus = "canceled"
)

type ThroughputResult struct {
	ID           string           `json:"id"`
	Name         string           `json:"name"`
	Status       ThroughputStatus `json:"status"`
	LatencyMS    float64          `json:"latency_ms,omitempty"`
	DownloadMbps float64          `json:"download_mbps,omitempty"`
	UploadMbps   float64          `json:"upload_mbps,omitempty"`
	DurationMS   int64            `json:"duration_ms"`
	Error        string           `json:"error,omitempty"`
}

type ThroughputProbe func(context.Context, ServerMetadata) ThroughputResult

// BenchmarkServers runs selected servers sequentially so multiple throughput
// probes do not compete for the same link. The caller's deadline bounds the
// ping, download, and upload requests through speedtest-go's context APIs.
func BenchmarkServers(ctx context.Context, servers []ServerMetadata, limit int, probe ThroughputProbe) []ThroughputResult {
	return BenchmarkServersWithNetwork(ctx, servers, limit, probe, NetworkAuto)
}

func BenchmarkServersWithNetwork(ctx context.Context, servers []ServerMetadata, limit int, probe ThroughputProbe, network Network) []ThroughputResult {
	if ctx == nil {
		ctx = context.Background()
	}
	if limit <= 0 || limit > len(servers) {
		limit = len(servers)
	}
	if probe == nil {
		probe = func(ctx context.Context, server ServerMetadata) ThroughputResult {
			return ProbeThroughputWithNetwork(ctx, server, network)
		}
	}
	results := make([]ThroughputResult, 0, limit)
	for _, server := range servers[:limit] {
		if err := ctx.Err(); err != nil {
			results = append(results, ThroughputResult{
				ID: server.ID, Name: server.Name, Status: throughputContextStatus(err), Error: err.Error(),
			})
			continue
		}
		results = append(results, probe(ctx, server))
	}
	return results
}

func ProbeThroughput(ctx context.Context, metadata ServerMetadata) (result ThroughputResult) {
	return ProbeThroughputWithNetwork(ctx, metadata, effectiveNetwork(metadata))
}

func ProbeThroughputWithNetwork(ctx context.Context, metadata ServerMetadata, network Network) (result ThroughputResult) {
	if ctx == nil {
		ctx = context.Background()
	}
	result.ID, result.Name = metadata.ID, metadata.Name
	result.Status = ThroughputUnavailable
	started := time.Now()
	defer func() { result.DurationMS = time.Since(started).Milliseconds() }()
	endpoint, err := url.Parse(strings.TrimSpace(metadata.URL))
	if err != nil || (endpoint.Scheme != "http" && endpoint.Scheme != "https") || endpoint.Hostname() == "" {
		result.Error = "speedtest server URL is unavailable"
		return result
	}
	if network == NetworkAuto {
		network = effectiveNetwork(metadata)
	}
	config := &showwinspeedtest.UserConfig{
		UserAgent:      showwinspeedtest.DefaultUserAgent,
		PingMode:       showwinspeedtest.TCP,
		TestMode:       showwinspeedtest.HTTPTest,
		MaxConnections: 8,
	}
	var serverClient *showwinspeedtest.Speedtest
	if network == NetworkAuto {
		serverClient = showwinspeedtest.New(showwinspeedtest.WithUserConfig(config))
	} else {
		// WithDoer follows WithUserConfig so the upstream library cannot replace
		// the explicit-family transport. HTTP ping shares that transport.
		config.PingMode = showwinspeedtest.HTTP
		serverClient = showwinspeedtest.New(
			showwinspeedtest.WithUserConfig(config),
			showwinspeedtest.WithDoer(NewHTTPClient(network, 30*time.Second)),
		)
	}
	server, err := serverClient.CustomServer(endpoint.String())
	if err != nil || server == nil {
		result.Error = formatThroughputError("create speedtest server", err)
		return result
	}
	if network != NetworkAuto {
		resolvedHost, resolveErr := ResolveServerAddress(ctx, server.Host, network)
		if resolveErr != nil {
			result.Error = formatThroughputError("resolve speedtest server", resolveErr)
			return result
		}
		server.Host = resolvedHost
	}
	if err = server.PingTestContext(ctx, nil); err != nil {
		return failedThroughputResult(result, ctx, "ping", err)
	}
	result.LatencyMS = float64(server.Latency) / float64(time.Millisecond)
	if err = server.DownloadTestContext(ctx); err != nil {
		return failedThroughputResult(result, ctx, "download", err)
	}
	if err = server.UploadTestContext(ctx); err != nil {
		return failedThroughputResult(result, ctx, "upload", err)
	}
	if err = ctx.Err(); err != nil {
		return failedThroughputResult(result, ctx, "throughput", err)
	}
	result.DownloadMbps = server.DLSpeed.Mbps()
	result.UploadMbps = server.ULSpeed.Mbps()
	if result.DownloadMbps <= 0 || result.UploadMbps <= 0 {
		result.Error = "speedtest returned no usable throughput"
		return result
	}
	result.Status = ThroughputAvailable
	return result
}

func effectiveNetwork(metadata ServerMetadata) Network {
	for _, value := range []string{metadata.Network} {
		if strings.TrimSpace(value) == "" {
			continue
		}
		if network, err := NormalizeNetwork(value); err == nil {
			return network
		}
	}
	host := strings.TrimSpace(metadata.Host)
	if parsed, _, err := net.SplitHostPort(host); err == nil {
		host = parsed
	}
	if ip := net.ParseIP(strings.Trim(host, "[]")); ip != nil {
		if ip.To4() != nil {
			return NetworkIPv4
		}
		return NetworkIPv6
	}
	return NetworkAuto
}

func failedThroughputResult(result ThroughputResult, ctx context.Context, stage string, err error) ThroughputResult {
	if contextErr := ctx.Err(); contextErr != nil {
		result.Status = throughputContextStatus(contextErr)
		result.Error = contextErr.Error()
		return result
	}
	result.Status = ThroughputUnavailable
	result.Error = formatThroughputError(stage, err)
	return result
}

func throughputContextStatus(err error) ThroughputStatus {
	if errors.Is(err, context.Canceled) {
		return ThroughputCanceled
	}
	if errors.Is(err, context.DeadlineExceeded) {
		return ThroughputTimeout
	}
	return ThroughputUnavailable
}

func formatThroughputError(stage string, err error) string {
	if err == nil {
		return stage + " failed"
	}
	return fmt.Sprintf("%s: %v", stage, err)
}
