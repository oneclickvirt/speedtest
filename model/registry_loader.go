package model

import (
	"context"
	"crypto/sha256"
	_ "embed"
	"encoding/hex"
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

//go:embed snapshot/manifest.json
var embeddedServerRegistryManifest []byte

const (
	SpeedtestRegistrySchema      = "speedtest.servers/v1"
	SpeedtestRegistryRawURL      = "https://raw.githubusercontent.com/oneclickvirt/speedtest/main/model/snapshot/speedtest-servers.json"
	SpeedtestManifestRawURL      = "https://raw.githubusercontent.com/oneclickvirt/speedtest/main/model/snapshot/manifest.json"
	SpeedtestRegistryCDNURL      = "https://cdn.spiritlhl.net/" + SpeedtestRegistryRawURL
	SpeedtestManifestRegistryURL = "https://cdn.spiritlhl.net/" + SpeedtestManifestRawURL
)

type RegistrySource struct {
	Name        string
	URL         string
	ManifestURL string
}

type RegistryLoadResult struct {
	Servers  []ServerMetadata
	Source   string
	Fallback bool
	Metadata RegistryMetadata
}

type RegistryMetadata struct {
	Schema      string `json:"schema"`
	Count       int    `json:"count"`
	SHA256      string `json:"sha256"`
	GeneratedAt string `json:"generated_at,omitempty"`
}

func DefaultRegistrySources() []RegistrySource {
	return []RegistrySource{
		{Name: "cdn", URL: SpeedtestRegistryCDNURL, ManifestURL: SpeedtestManifestRegistryURL},
		{Name: "raw", URL: SpeedtestRegistryRawURL, ManifestURL: SpeedtestManifestRawURL},
	}
}

type ServerRegistryManifest struct {
	Schema      string `json:"schema"`
	File        string `json:"file"`
	Count       int    `json:"count"`
	SHA256      string `json:"sha256"`
	GeneratedAt string `json:"generated_at"`
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
	return LoadServerRegistryWithNetwork(ctx, client, sources, minimum, NetworkAuto)
}

// LoadServerRegistryWithNetwork makes registry and manifest requests through
// the requested family when the caller does not provide a custom client.
func LoadServerRegistryWithNetwork(ctx context.Context, client *http.Client, sources []RegistrySource, minimum int, network Network) (RegistryLoadResult, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	if client == nil {
		client = NewHTTPClient(network, 12*time.Second)
	}
	if minimum < 1 {
		minimum = 1
	}
	var lastErr error
	for index, source := range sources {
		data, metadata, err := loadServerRegistrySnapshot(ctx, client, source)
		if err != nil {
			lastErr = fmt.Errorf("load %s registry: %w", registrySourceLabel(source.Name), err)
			continue
		}
		servers, err := decodeServerRegistry(data, source.Name, minimum)
		if err != nil {
			lastErr = fmt.Errorf("validate %s registry: %w", registrySourceLabel(source.Name), err)
			continue
		}
		if metadata.Count == 0 {
			metadata = serverRegistryMetadata(data, servers)
		}
		return RegistryLoadResult{Servers: servers, Source: source.Name, Fallback: index > 0, Metadata: metadata}, nil
	}
	if len(embeddedServerRegistry) > 0 {
		metadata, err := validateServerRegistryManifest(embeddedServerRegistryManifest, embeddedServerRegistry)
		if err != nil {
			return RegistryLoadResult{}, fmt.Errorf("validate embedded registry manifest: %w", err)
		}
		servers, err := decodeServerRegistry(embeddedServerRegistry, "embedded", minimum)
		if err == nil {
			return RegistryLoadResult{Servers: servers, Source: "embedded", Fallback: true, Metadata: metadata}, nil
		}
		lastErr = fmt.Errorf("validate embedded registry: %w", err)
	}
	if lastErr == nil {
		lastErr = errors.New("no registry sources configured")
	}
	return RegistryLoadResult{}, lastErr
}

func loadServerRegistrySnapshot(ctx context.Context, client *http.Client, source RegistrySource) ([]byte, RegistryMetadata, error) {
	if source.ManifestURL == "" {
		data, err := fetchServerRegistry(ctx, client, source.URL)
		return data, RegistryMetadata{}, err
	}
	manifestData, err := fetchServerRegistry(ctx, client, source.ManifestURL)
	if err != nil {
		return nil, RegistryMetadata{}, fmt.Errorf("load manifest: %w", err)
	}
	data, err := fetchServerRegistry(ctx, client, source.URL)
	if err != nil {
		return nil, RegistryMetadata{}, err
	}
	metadata, err := validateServerRegistryManifest(manifestData, data)
	if err != nil {
		return nil, RegistryMetadata{}, err
	}
	return data, metadata, nil
}

func validateServerRegistryManifest(data, snapshot []byte) (RegistryMetadata, error) {
	var manifest ServerRegistryManifest
	decoder := json.NewDecoder(strings.NewReader(string(data)))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&manifest); err != nil {
		return RegistryMetadata{}, fmt.Errorf("decode manifest: %w", err)
	}
	var extra any
	if err := decoder.Decode(&extra); err != io.EOF {
		return RegistryMetadata{}, errors.New("manifest contains trailing JSON")
	}
	if manifest.Schema != SpeedtestRegistrySchema || manifest.File != "speedtest-servers.json" || manifest.Count < 1 {
		return RegistryMetadata{}, errors.New("manifest schema, file, or count is invalid")
	}
	if _, err := time.Parse(time.RFC3339, manifest.GeneratedAt); err != nil {
		return RegistryMetadata{}, fmt.Errorf("manifest generated_at is invalid: %w", err)
	}
	hash := sha256.Sum256(snapshot)
	if !strings.EqualFold(manifest.SHA256, hex.EncodeToString(hash[:])) {
		return RegistryMetadata{}, errors.New("manifest SHA-256 does not match snapshot")
	}
	servers, err := decodeServerRegistry(snapshot, "manifest", manifest.Count)
	if err != nil {
		return RegistryMetadata{}, fmt.Errorf("manifest snapshot validation failed: %w", err)
	}
	if len(servers) != manifest.Count {
		return RegistryMetadata{}, fmt.Errorf("manifest count %d does not match snapshot count %d", manifest.Count, len(servers))
	}
	return RegistryMetadata{Schema: manifest.Schema, Count: manifest.Count, SHA256: strings.ToLower(manifest.SHA256), GeneratedAt: manifest.GeneratedAt}, nil
}

func serverRegistryMetadata(snapshot []byte, servers []ServerMetadata) RegistryMetadata {
	hash := sha256.Sum256(snapshot)
	return RegistryMetadata{Schema: SpeedtestRegistrySchema, Count: len(servers), SHA256: hex.EncodeToString(hash[:])}
}

func ResolveServerRegistry(ctx context.Context, client *http.Client, sources []RegistrySource, minimum, limit int, timeout time.Duration, concurrency int, dial ServerDialFunc) RegistryReport {
	return resolveServerRegistry(ctx, client, sources, minimum, limit, timeout, concurrency, dial, "", NetworkAuto)
}

// ResolveServerRegistryWithNetwork is the family-aware variant used when a
// caller explicitly requests IPv4 or IPv6.
func ResolveServerRegistryWithNetwork(ctx context.Context, client *http.Client, sources []RegistrySource, minimum, limit int, timeout time.Duration, concurrency int, dial ServerDialFunc, network Network) RegistryReport {
	return resolveServerRegistry(ctx, client, sources, minimum, limit, timeout, concurrency, dial, "", network)
}

// ResolveServerRegistryForLanguage preserves the existing registry behavior
// for Chinese callers and applies the international selection policy for
// English callers.
func ResolveServerRegistryForLanguage(ctx context.Context, client *http.Client, sources []RegistrySource, minimum, limit int, timeout time.Duration, concurrency int, dial ServerDialFunc, language string) RegistryReport {
	return resolveServerRegistry(ctx, client, sources, minimum, limit, timeout, concurrency, dial, language, NetworkAuto)
}

func ResolveServerRegistryForLanguageWithNetwork(ctx context.Context, client *http.Client, sources []RegistrySource, minimum, limit int, timeout time.Duration, concurrency int, dial ServerDialFunc, language string, network Network) RegistryReport {
	return resolveServerRegistry(ctx, client, sources, minimum, limit, timeout, concurrency, dial, language, network)
}

func resolveServerRegistry(ctx context.Context, client *http.Client, sources []RegistrySource, minimum, limit int, timeout time.Duration, concurrency int, dial ServerDialFunc, language string, network Network) RegistryReport {
	if ctx == nil {
		ctx = context.Background()
	}
	report := RegistryReport{
		SchemaVersion: "speedtest.registry/v1",
		Availability:  ServerUnavailable,
		Servers:       []ServerMetadata{},
	}
	loaded, err := LoadServerRegistryWithNetwork(ctx, client, sources, minimum, network)
	if err != nil {
		report.Fallback = true
		report.Error = err.Error()
		return report
	}
	report.Source = loaded.Source
	report.Fallback = loaded.Fallback
	report.Metadata = loaded.Metadata
	eligible := FilterServersForLanguage(loaded.Servers, language)
	if len(eligible) == 0 {
		report.Error = "no eligible speedtest servers for language scope"
		return report
	}
	report.Servers = ProbeServersWithNetwork(ctx, eligible, timeout, concurrency, dial, network)
	var selected []ServerMetadata
	if strings.EqualFold(strings.TrimSpace(language), "en") {
		selected, err = SelectRepresentativeServers(report.Servers, limit)
	} else {
		selected, err = SelectAvailableServers(report.Servers, limit)
	}
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
		return nil, errors.New("create registry request failed")
	}
	req.Header.Set("Accept", "application/json")
	req.Header.Set("User-Agent", "oneclickvirt-speedtest/registry-v1")
	response, err := client.Do(req)
	if err != nil {
		if ctxErr := ctx.Err(); ctxErr != nil {
			return nil, ctxErr
		}
		return nil, errors.New("registry request failed")
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("HTTP %d", response.StatusCode)
	}
	data, err := io.ReadAll(io.LimitReader(response.Body, 16<<20))
	if err != nil {
		return nil, errors.New("registry response read failed")
	}
	if !json.Valid(data) {
		return nil, errors.New("registry response is not valid JSON")
	}
	return data, nil
}

func registrySourceLabel(name string) string {
	switch strings.ToLower(strings.TrimSpace(name)) {
	case "cdn", "raw":
		return strings.ToLower(strings.TrimSpace(name))
	default:
		return "remote"
	}
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
