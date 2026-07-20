package model

import (
	"context"
	_ "embed"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"sort"
	"strconv"
	"strings"
	"time"
)

//go:embed snapshot/speedtest-servers.json
var embeddedServerRegistry []byte

const (
	SpeedtestRegistryRawURL = "https://raw.githubusercontent.com/oneclickvirt/speedtest/main/model/snapshot/speedtest-servers.json"
	SpeedtestRegistryCDNURL = "https://cdn.spiritlhl.net/" + SpeedtestRegistryRawURL
)

type RegistrySource struct {
	Name string
	URL  string
}

type RegistryLoadResult struct {
	Servers  []ServerMetadata
	Source   string
	Fallback bool
}

func DefaultRegistrySources() []RegistrySource {
	return []RegistrySource{
		{Name: "cdn", URL: SpeedtestRegistryCDNURL},
		{Name: "raw", URL: SpeedtestRegistryRawURL},
	}
}

// NormalizeServerRegistrySnapshot validates an upstream registry and emits the
// stable, minimal shape consumed by this module's embedded loader. It is used
// by the repository-local updater so runtime never depends on another project.
func NormalizeServerRegistrySnapshot(data []byte, minimum int) ([]byte, error) {
	servers, err := decodeServerRegistry(data, "snapshot", minimum)
	if err != nil {
		return nil, err
	}
	sort.SliceStable(servers, func(i, j int) bool {
		if servers[i].ID == servers[j].ID {
			return servers[i].Host < servers[j].Host
		}
		return servers[i].ID < servers[j].ID
	})
	encoded, err := json.MarshalIndent(servers, "", "  ")
	if err != nil {
		return nil, err
	}
	return append(encoded, '\n'), nil
}

func LoadServerRegistry(ctx context.Context, client *http.Client, sources []RegistrySource, minimum int) (RegistryLoadResult, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	if client == nil {
		client = &http.Client{Timeout: 12 * time.Second}
	}
	if minimum < 1 {
		minimum = 1
	}
	var lastErr error
	for index, source := range sources {
		data, err := fetchServerRegistry(ctx, client, source.URL)
		if err != nil {
			lastErr = fmt.Errorf("load %s registry: %w", source.Name, err)
			continue
		}
		servers, err := decodeServerRegistry(data, source.Name, minimum)
		if err != nil {
			lastErr = fmt.Errorf("validate %s registry: %w", source.Name, err)
			continue
		}
		return RegistryLoadResult{Servers: servers, Source: source.Name, Fallback: index > 0}, nil
	}
	if len(embeddedServerRegistry) > 0 {
		servers, err := decodeServerRegistry(embeddedServerRegistry, "embedded", minimum)
		if err == nil {
			return RegistryLoadResult{Servers: servers, Source: "embedded", Fallback: true}, nil
		}
		lastErr = fmt.Errorf("validate embedded registry: %w", err)
	}
	if lastErr == nil {
		lastErr = errors.New("no registry sources configured")
	}
	return RegistryLoadResult{}, lastErr
}

func ResolveServerRegistry(ctx context.Context, client *http.Client, sources []RegistrySource, minimum, limit int, timeout time.Duration, concurrency int, dial ServerDialFunc) RegistryReport {
	if ctx == nil {
		ctx = context.Background()
	}
	report := RegistryReport{
		SchemaVersion: "speedtest.registry/v1",
		Availability:  ServerUnavailable,
		Servers:       []ServerMetadata{},
	}
	loaded, err := LoadServerRegistry(ctx, client, sources, minimum)
	if err != nil {
		report.Fallback = true
		report.Error = err.Error()
		return report
	}
	report.Source = loaded.Source
	report.Fallback = loaded.Fallback
	report.Servers = ProbeServers(ctx, loaded.Servers, timeout, concurrency, dial)
	selected, err := SelectAvailableServers(report.Servers, limit)
	if err != nil {
		report.Error = err.Error()
		return report
	}
	report.Availability = ServerAvailable
	report.Selected = selected
	return report
}

func fetchServerRegistry(ctx context.Context, client *http.Client, endpoint string) ([]byte, error) {
	parsed, err := url.Parse(endpoint)
	if err != nil || (parsed.Scheme != "http" && parsed.Scheme != "https") || parsed.Hostname() == "" {
		return nil, errors.New("invalid registry URL")
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, parsed.String(), nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Accept", "application/json")
	req.Header.Set("User-Agent", "oneclickvirt-speedtest/registry-v1")
	response, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("HTTP %d", response.StatusCode)
	}
	data, err := io.ReadAll(io.LimitReader(response.Body, 16<<20))
	if err != nil {
		return nil, err
	}
	if !json.Valid(data) {
		return nil, errors.New("registry response is not valid JSON")
	}
	return data, nil
}

func decodeServerRegistry(data []byte, source string, minimum int) ([]ServerMetadata, error) {
	var input []ServerMetadata
	if err := json.Unmarshal(data, &input); err != nil {
		return nil, err
	}
	seen := make(map[string]struct{}, len(input))
	servers := make([]ServerMetadata, 0, len(input))
	for _, server := range input {
		server.ID = strings.TrimSpace(server.ID)
		server.Name = strings.TrimSpace(server.Name)
		server.Host = strings.TrimSpace(server.Host)
		if !normalizeRegistryHost(&server) {
			continue
		}
		if server.ID == "" || !validRegistryHost(server.Host) {
			continue
		}
		key := strings.ToLower(server.ID)
		if _, exists := seen[key]; exists {
			continue
		}
		seen[key] = struct{}{}
		if server.Name == "" {
			server.Name = server.ID
		}
		server.Source = source
		if server.Availability == ServerUnavailable {
			if server.Error == "" {
				server.Error = "static node unavailable"
			}
		} else {
			server.Availability = ServerCandidate
			server.Error = ""
		}
		servers = append(servers, server)
	}
	if len(servers) < minimum {
		return nil, fmt.Errorf("registry has %d valid servers; require at least %d", len(servers), minimum)
	}
	return servers, nil
}

func validRegistryHost(host string) bool {
	name, port, err := net.SplitHostPort(host)
	if err != nil || strings.Trim(name, "[]") == "" || port == "" {
		return false
	}
	portNumber, err := strconv.Atoi(port)
	if err != nil || portNumber < 1 || portNumber > 65535 {
		return false
	}
	if strings.ContainsAny(name, " /\\") {
		return false
	}
	return true
}

func normalizeRegistryHost(server *ServerMetadata) bool {
	host := strings.TrimSpace(server.Host)
	if host != "" {
		if validRegistryHost(host) {
			return true
		}
		if strings.Contains(host, ":") {
			return false
		}
		server.Host = net.JoinHostPort(host, "80")
		return validRegistryHost(server.Host)
	}
	parsed, err := url.Parse(strings.TrimSpace(server.URL))
	if err != nil || parsed.Hostname() == "" {
		return false
	}
	port := parsed.Port()
	if port == "" {
		if parsed.Scheme == "https" {
			port = "443"
		} else {
			port = "80"
		}
	}
	server.Host = net.JoinHostPort(parsed.Hostname(), port)
	return validRegistryHost(server.Host)
}
