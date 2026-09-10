package model

import (
	"context"
	"errors"
	"fmt"
	"net"
	"net/http"
	"net/url"
	"path"
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
	Source       string             `json:"-"`
	Availability ServerAvailability `json:"availability"`
	LatencyMS    int64              `json:"latency_ms,omitempty"`
	Error        string             `json:"error,omitempty"`
	Network      string             `json:"network,omitempty"`
	ResolvedHost string             `json:"resolved_host,omitempty"`
}

type RegistryReport struct {
	SchemaVersion string             `json:"schema_version"`
	Source        string             `json:"-"`
	Fallback      bool               `json:"-"`
	Metadata      RegistryMetadata   `json:"metadata"`
	Availability  ServerAvailability `json:"availability"`
	Servers       []ServerMetadata   `json:"servers"`
	Selected      []ServerMetadata   `json:"selected,omitempty"`
	Error         string             `json:"error,omitempty"`
}

type ServerDialFunc func(context.Context, string, string) (net.Conn, error)

func ProbeServers(ctx context.Context, servers []ServerMetadata, timeout time.Duration, concurrency int, dial ServerDialFunc) []ServerMetadata {
	return ProbeServersWithNetwork(ctx, servers, timeout, concurrency, dial, NetworkAuto)
}

// ProbeServersWithNetwork applies an explicit address-family policy while
// retaining a caller-supplied dial function for fixtures and custom routes.
func ProbeServersWithNetwork(ctx context.Context, servers []ServerMetadata, timeout time.Duration, concurrency int, dial ServerDialFunc, network Network) []ServerMetadata {
	if ctx == nil {
		ctx = context.Background()
	}
	normalizedNetwork, networkErr := NormalizeNetwork(string(network))
	if networkErr == nil {
		network = normalizedNetwork
	}
	result := append([]ServerMetadata(nil), servers...)
	if networkErr != nil {
		markPendingServersUnavailable(result, "invalid network")
		return result
	}
	// Registry status and an earlier reachability result are ranking hints, not
	// permanent exclusions. Every syntactically valid endpoint still reaches
	// the throughput stage, which is the authoritative availability check.
	for index := range result {
		if result[index].Availability != ServerAvailable {
			result[index].Availability = ServerCandidate
		}
	}
	if timeout <= 0 {
		timeout = 3 * time.Second
	}
	if concurrency <= 0 {
		concurrency = 8
	}
	customDial := dial != nil
	if dial == nil {
		dial = DialContext(network)
	}
	var httpClient *http.Client
	if !customDial {
		httpClient = NewHTTPClient(network, timeout)
	}
	jobs := make(chan int)
	workers := min(concurrency, len(result))
	var wg sync.WaitGroup
	wg.Add(workers)
	for range workers {
		go func() {
			defer wg.Done()
			for index := range jobs {
				server := &result[index]
				if server.Availability == "" {
					server.Availability = ServerCandidate
				}
				if strings.TrimSpace(server.Host) == "" {
					server.Availability, server.Error = ServerUnavailable, "missing host"
					continue
				}
				probeCtx, cancel := context.WithTimeout(ctx, timeout)
				started := time.Now()
				address := server.Host
				if network != NetworkAuto {
					resolvedAddress, resolveErr := ResolveServerAddress(probeCtx, server.Host, network)
					if resolveErr != nil {
						server.LatencyMS = time.Since(started).Milliseconds()
						cancel()
						server.Availability, server.Error = ServerCandidate, classifyServerError(resolveErr)
						continue
					}
					address = resolvedAddress
					server.Network = string(network)
					server.ResolvedHost = address
				}
				dialNetwork := string(network)
				if dialNetwork == "" {
					dialNetwork = "tcp"
				}
				conn, err := dial(probeCtx, dialNetwork, address)
				server.LatencyMS = time.Since(started).Milliseconds()
				cancel()
				if err != nil {
					server.Availability, server.Error = ServerCandidate, classifyServerError(err)
					continue
				}
				if conn != nil {
					_ = conn.Close()
				}
				if !customDial && strings.TrimSpace(server.URL) != "" {
					probeCtx, probeCancel := context.WithTimeout(ctx, timeout)
					probeURL, probeURLErr := serverLatencyURL(server.URL)
					req, requestErr := http.NewRequestWithContext(probeCtx, http.MethodGet, probeURL, nil)
					if probeURLErr != nil {
						requestErr = probeURLErr
					}
					if requestErr == nil {
						response, httpErr := httpClient.Do(req)
						if httpErr == nil {
							_ = response.Body.Close()
							if response.StatusCode >= 400 && response.StatusCode != http.StatusMethodNotAllowed {
								server.Availability, server.Error = ServerCandidate, fmt.Sprintf("HTTP %d", response.StatusCode)
								probeCancel()
								continue
							}
						} else {
							server.Availability, server.Error = ServerCandidate, classifyServerError(httpErr)
							probeCancel()
							continue
						}
					} else {
						server.Availability, server.Error = ServerUnavailable, "invalid speedtest URL"
						probeCancel()
						continue
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
			markPendingServersCandidate(result, classifyServerError(ctx.Err()))
			return result
		}
	}
	close(jobs)
	wg.Wait()
	return result
}

func markPendingServersCandidate(servers []ServerMetadata, reason string) {
	for index := range servers {
		if servers[index].Availability == ServerCandidate || servers[index].Availability == "" {
			servers[index].Availability = ServerCandidate
			if servers[index].Error == "" {
				servers[index].Error = reason
			}
		}
	}
}

func serverLatencyURL(endpoint string) (string, error) {
	parsed, err := url.Parse(strings.TrimSpace(endpoint))
	if err != nil || (parsed.Scheme != "http" && parsed.Scheme != "https") || parsed.Hostname() == "" {
		return "", errors.New("invalid speedtest URL")
	}
	parsed.Path = path.Join(path.Dir(parsed.Path), "latency.txt")
	parsed.RawPath = ""
	parsed.RawQuery = ""
	parsed.Fragment = ""
	return parsed.String(), nil
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
	candidates := make([]ServerMetadata, 0, len(servers))
	for _, server := range servers {
		switch server.Availability {
		case ServerAvailable:
			available = append(available, server)
		case ServerCandidate:
			candidates = append(candidates, server)
		}
	}
	if len(available) == 0 && len(candidates) == 0 {
		return nil, errors.New("no available speedtest servers")
	}
	sort.SliceStable(available, func(i, j int) bool {
		if available[i].LatencyMS == available[j].LatencyMS {
			return available[i].ID < available[j].ID
		}
		return available[i].LatencyMS < available[j].LatencyMS
	})
	sort.SliceStable(candidates, func(i, j int) bool {
		if candidates[i].LatencyMS == candidates[j].LatencyMS {
			return candidates[i].ID < candidates[j].ID
		}
		return candidates[i].LatencyMS < candidates[j].LatencyMS
	})
	eligible := append(available, candidates...)
	if limit > 0 && len(eligible) > limit {
		eligible = eligible[:limit]
	}
	return eligible, nil
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
