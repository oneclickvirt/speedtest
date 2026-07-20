package model

import (
	"context"
	"errors"
	"fmt"
	"net"
	"net/http"
	"sort"
	"strings"
	"sync"
	"time"
)

type ServerAvailability string

const (
	ServerCandidate   ServerAvailability = "candidate"
	ServerAvailable   ServerAvailability = "available"
	ServerUnavailable ServerAvailability = "unavailable"
)

type ServerMetadata struct {
	ID           string             `json:"id"`
	Name         string             `json:"name"`
	Host         string             `json:"host"`
	URL          string             `json:"url,omitempty"`
	Provider     string             `json:"provider,omitempty"`
	Country      string             `json:"country,omitempty"`
	City         string             `json:"city,omitempty"`
	Source       string             `json:"source,omitempty"`
	Availability ServerAvailability `json:"availability"`
	LatencyMS    int64              `json:"latency_ms,omitempty"`
	Error        string             `json:"error,omitempty"`
}

type RegistryReport struct {
	SchemaVersion string             `json:"schema_version"`
	Source        string             `json:"source,omitempty"`
	Fallback      bool               `json:"fallback"`
	Availability  ServerAvailability `json:"availability"`
	Servers       []ServerMetadata   `json:"servers"`
	Selected      []ServerMetadata   `json:"selected,omitempty"`
	Error         string             `json:"error,omitempty"`
}

type ServerDialFunc func(context.Context, string, string) (net.Conn, error)

func ProbeServers(ctx context.Context, servers []ServerMetadata, timeout time.Duration, concurrency int, dial ServerDialFunc) []ServerMetadata {
	if timeout <= 0 {
		timeout = 3 * time.Second
	}
	if concurrency <= 0 {
		concurrency = 8
	}
	customDial := dial != nil
	if dial == nil {
		dial = (&net.Dialer{}).DialContext
	}
	result := append([]ServerMetadata(nil), servers...)
	jobs := make(chan int)
	workers := min(concurrency, len(result))
	var wg sync.WaitGroup
	wg.Add(workers)
	for range workers {
		go func() {
			defer wg.Done()
			for index := range jobs {
				server := &result[index]
				if server.Availability == ServerUnavailable {
					continue
				}
				if server.Availability == "" {
					server.Availability = ServerCandidate
				}
				if strings.TrimSpace(server.Host) == "" {
					server.Availability, server.Error = ServerUnavailable, "missing host"
					continue
				}
				probeCtx, cancel := context.WithTimeout(ctx, timeout)
				started := time.Now()
				conn, err := dial(probeCtx, "tcp", server.Host)
				server.LatencyMS = time.Since(started).Milliseconds()
				cancel()
				if err != nil {
					server.Availability, server.Error = ServerUnavailable, classifyServerError(err)
					continue
				}
				if conn != nil {
					_ = conn.Close()
				}
				if !customDial && strings.TrimSpace(server.URL) != "" {
					probeCtx, probeCancel := context.WithTimeout(ctx, timeout)
					req, requestErr := http.NewRequestWithContext(probeCtx, http.MethodHead, server.URL, nil)
					if requestErr == nil {
						response, httpErr := (&http.Client{Timeout: timeout}).Do(req)
						if httpErr == nil {
							_ = response.Body.Close()
							if response.StatusCode >= 400 && response.StatusCode != http.StatusMethodNotAllowed {
								server.Availability, server.Error = ServerUnavailable, fmt.Sprintf("HTTP %d", response.StatusCode)
								probeCancel()
								continue
							}
						} else {
							server.Availability, server.Error = ServerUnavailable, classifyServerError(httpErr)
							probeCancel()
							continue
						}
					}
					probeCancel()
				}
				server.Availability, server.Error = ServerAvailable, ""
			}
		}()
	}
	for index := range result {
		select {
		case jobs <- index:
		case <-ctx.Done():
			close(jobs)
			wg.Wait()
			markPendingServersUnavailable(result, classifyServerError(ctx.Err()))
			return result
		}
	}
	close(jobs)
	wg.Wait()
	return result
}

func markPendingServersUnavailable(servers []ServerMetadata, reason string) {
	for index := range servers {
		if servers[index].Availability == ServerCandidate || servers[index].Availability == "" {
			servers[index].Availability = ServerUnavailable
			servers[index].Error = reason
		}
	}
}

func SelectAvailableServers(servers []ServerMetadata, limit int) ([]ServerMetadata, error) {
	available := make([]ServerMetadata, 0, len(servers))
	for _, server := range servers {
		if server.Availability == ServerAvailable {
			available = append(available, server)
		}
	}
	if len(available) == 0 {
		return nil, errors.New("no available speedtest servers")
	}
	sort.SliceStable(available, func(i, j int) bool {
		if available[i].LatencyMS == available[j].LatencyMS {
			return available[i].ID < available[j].ID
		}
		return available[i].LatencyMS < available[j].LatencyMS
	})
	if limit > 0 && len(available) > limit {
		available = available[:limit]
	}
	return available, nil
}

func classifyServerError(err error) string {
	if errors.Is(err, context.Canceled) {
		return "canceled"
	}
	if errors.Is(err, context.DeadlineExceeded) {
		return "timeout"
	}
	var netErr net.Error
	if errors.As(err, &netErr) && netErr.Timeout() {
		return "timeout"
	}
	return "connection_error"
}
