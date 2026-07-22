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
	Metadata      RegistryMetadata   `json:"metadata"`
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

// IsMainlandChinaServer identifies mainland China without grouping Hong Kong,
// Macao, or Taiwan into the same scope.
func IsMainlandChinaServer(server ServerMetadata) bool {
	country := normalizeCountry(server.Country)
	switch country {
	case "hong kong", "hongkong", "hk", "macao", "macau", "mo", "taiwan", "tw":
		return false
	case "cn", "china", "mainland china", "china mainland", "prc", "peoples republic of china", "people s republic of china", "中国", "中国大陆", "中华人民共和国":
		return true
	default:
		return strings.Contains(country, "mainland china")
	}
}

// FilterServersForLanguage applies the geography contract used by automatic
// English selection. Unknown countries are omitted because they cannot be
// proven to be outside mainland China.
func FilterServersForLanguage(servers []ServerMetadata, language string) []ServerMetadata {
	if strings.ToLower(strings.TrimSpace(language)) != "en" {
		return append([]ServerMetadata(nil), servers...)
	}
	filtered := make([]ServerMetadata, 0, len(servers))
	for _, server := range servers {
		if normalizeCountry(server.Country) == "" || IsMainlandChinaServer(server) {
			continue
		}
		filtered = append(filtered, server)
	}
	return filtered
}

// SelectRepresentativeServers keeps latency ordering within a region while
// spreading automatic English selections across geographic regions.
func SelectRepresentativeServers(servers []ServerMetadata, limit int) ([]ServerMetadata, error) {
	available, err := SelectAvailableServers(servers, 0)
	if err != nil {
		return nil, err
	}
	if limit <= 0 || limit > len(available) {
		limit = len(available)
	}

	regionOrder := []string{"asia", "europe", "north-america", "oceania", "south-america", "africa", "other"}
	buckets := make(map[string][]ServerMetadata, len(regionOrder))
	for _, server := range available {
		region := serverRegion(server.Country)
		buckets[region] = append(buckets[region], server)
	}

	selected := make([]ServerMetadata, 0, limit)
	for len(selected) < limit {
		added := false
		for _, region := range regionOrder {
			bucket := buckets[region]
			if len(bucket) == 0 {
				continue
			}
			selected = append(selected, bucket[0])
			buckets[region] = bucket[1:]
			added = true
			if len(selected) == limit {
				break
			}
		}
		if !added {
			break
		}
	}
	return selected, nil
}

func normalizeCountry(country string) string {
	country = strings.ToLower(strings.TrimSpace(country))
	replacer := strings.NewReplacer("_", " ", "-", " ", ".", " ", ",", " ", "'", " ", "’", " ")
	return strings.Join(strings.Fields(replacer.Replace(country)), " ")
}

func serverRegion(country string) string {
	switch normalizeCountry(country) {
	case "jp", "japan", "sg", "singapore", "kr", "south korea", "korea", "hk", "hong kong", "hongkong", "tw", "taiwan", "mo", "macao", "macau", "my", "malaysia", "id", "indonesia", "th", "thailand", "vn", "vietnam", "in", "india":
		return "asia"
	case "gb", "uk", "united kingdom", "england", "fr", "france", "de", "germany", "nl", "netherlands", "es", "spain", "it", "italy", "se", "sweden", "no", "norway", "fi", "finland", "pl", "poland":
		return "europe"
	case "us", "usa", "united states", "united states of america", "ca", "canada", "mx", "mexico":
		return "north-america"
	case "au", "australia", "nz", "new zealand":
		return "oceania"
	case "br", "brazil", "ar", "argentina", "cl", "chile", "co", "colombia", "pe", "peru":
		return "south-america"
	case "za", "south africa", "eg", "egypt", "ng", "nigeria", "ke", "kenya", "ma", "morocco":
		return "africa"
	default:
		return "other"
	}
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
