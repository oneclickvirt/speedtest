package main

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/oneclickvirt/speedtest/model"
)

const defaultSourceURL = "https://raw.githubusercontent.com/xykt/NetQuality/main/ref/speedtest_cn.json"

var defaultGlobalSourceURLs = []string{
	"https://www.speedtest.net/api/js/servers?engine=js&limit=100&https_functional=true&lat=51.5074&lon=-0.1278",
	"https://www.speedtest.net/api/js/servers?engine=js&limit=100&https_functional=true&lat=35.6762&lon=139.6503",
	"https://www.speedtest.net/api/js/servers?engine=js&limit=100&https_functional=true&lat=-33.8688&lon=151.2093",
	"https://www.speedtest.net/api/js/servers?engine=js&limit=100&https_functional=true&lat=1.3521&lon=103.8198",
	"https://www.speedtest.net/api/js/servers?engine=js&limit=100&https_functional=true&lat=-23.5505&lon=-46.6333",
}

type updateConfig struct {
	Source        string
	GlobalSources []string
	Output        string
	Manifest      string
	Minimum       int
	Timeout       time.Duration
	AllowStale    bool
}

// remoteFetchError marks a transport, HTTP, or body-read failure. A successful
// response with malformed data is intentionally not wrapped in this type: it
// needs human attention instead of silently retaining stale data.
type remoteFetchError struct {
	err error
}

func (err *remoteFetchError) Error() string {
	return err.err.Error()
}

func (err *remoteFetchError) Unwrap() error {
	return err.err
}

type stringListFlag []string

func (values *stringListFlag) String() string {
	return strings.Join(*values, ",")
}

func (values *stringListFlag) Set(value string) error {
	value = strings.TrimSpace(value)
	if value == "" {
		return errors.New("source URL is empty")
	}
	*values = append(*values, value)
	return nil
}

func main() {
	config := updateConfig{}
	globalSources := stringListFlag(append([]string(nil), defaultGlobalSourceURLs...))
	flag.StringVar(&config.Source, "source", defaultSourceURL, "upstream registry URL")
	flag.Var(&globalSources, "global-source", "additional global Ookla registry URL (repeatable)")
	flag.StringVar(&config.Output, "output", "model/snapshot/speedtest-servers.json", "snapshot output path")
	flag.StringVar(&config.Manifest, "manifest", "model/snapshot/manifest.json", "snapshot manifest output path")
	flag.IntVar(&config.Minimum, "minimum", 10, "minimum valid servers")
	flag.DurationVar(&config.Timeout, "timeout", 30*time.Second, "upstream request timeout")
	flag.BoolVar(&config.AllowStale, "allow-stale", false, "retain a verified embedded snapshot when all remote fetches are unavailable")
	flag.Parse()
	config.GlobalSources = append([]string(nil), globalSources...)
	if err := updateSnapshot(context.Background(), http.DefaultClient, config); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

func updateSnapshot(ctx context.Context, client *http.Client, config updateConfig) error {
	if ctx == nil {
		ctx = context.Background()
	}
	if client == nil {
		client = &http.Client{}
	}
	if config.Source == "" || config.Output == "" || config.Minimum < 1 || config.Timeout <= 0 {
		return errors.New("source, output, minimum, and timeout must be valid")
	}
	if config.Manifest == "" {
		config.Manifest = filepath.Join(filepath.Dir(config.Output), "manifest.json")
	}
	requestCtx, cancel := context.WithTimeout(ctx, config.Timeout)
	defer cancel()
	raw, err := fetchSource(requestCtx, client, config.Source)
	if err != nil {
		var fetchErr *remoteFetchError
		if errors.As(err, &fetchErr) {
			return retainVerifiedStaleSnapshot(config, err)
		}
		return err
	}
	servers, err := parseServerMetadata(raw)
	if err != nil {
		return fmt.Errorf("decode China registry: %w", err)
	}
	globalServers, err := fetchGlobalServers(requestCtx, client, config.GlobalSources)
	if err != nil {
		var fetchErr *remoteFetchError
		if errors.As(err, &fetchErr) {
			return retainVerifiedStaleSnapshot(config, err)
		}
		return err
	}
	servers = append(servers, globalServers...)
	merged, err := json.Marshal(servers)
	if err != nil {
		return err
	}
	data, err := model.NormalizeServerRegistrySnapshot(merged, config.Minimum)
	if err != nil {
		return fmt.Errorf("validate registry: %w", err)
	}
	return replaceSnapshot(config.Output, config.Manifest, data)
}

func retainVerifiedStaleSnapshot(config updateConfig, upstreamErr error) error {
	if !config.AllowStale {
		return upstreamErr
	}
	if err := validateExistingSnapshot(config.Output, config.Manifest, config.Minimum); err != nil {
		return fmt.Errorf("%w; cannot retain stale snapshot: %v", upstreamErr, err)
	}
	return nil
}

func fetchGlobalServers(ctx context.Context, client *http.Client, sources []string) ([]model.ServerMetadata, error) {
	type fetchResult struct {
		index   int
		servers []model.ServerMetadata
		err     error
	}

	endpoints := make([]string, 0, len(sources))
	for _, source := range sources {
		if source = strings.TrimSpace(source); source != "" {
			endpoints = append(endpoints, source)
		}
	}
	if len(endpoints) == 0 {
		return nil, nil
	}

	results := make(chan fetchResult, len(endpoints))
	for index, endpoint := range endpoints {
		go func() {
			raw, err := fetchSource(ctx, client, endpoint)
			if err != nil {
				results <- fetchResult{index: index, err: err}
				return
			}
			servers, err := parseGlobalServers(raw)
			if err != nil {
				err = fmt.Errorf("decode registry: %w", err)
			}
			results <- fetchResult{index: index, servers: servers, err: err}
		}()
	}

	ordered := make([][]model.ServerMetadata, len(endpoints))
	errorsBySource := make([]error, 0, len(endpoints))
	parseErrors := make([]error, 0)
	successes := 0
	for range endpoints {
		result := <-results
		if result.err != nil {
			wrapped := fmt.Errorf("global source %d: %w", result.index+1, result.err)
			errorsBySource = append(errorsBySource, wrapped)
			var fetchErr *remoteFetchError
			if !errors.As(result.err, &fetchErr) {
				parseErrors = append(parseErrors, wrapped)
			}
			continue
		}
		ordered[result.index] = result.servers
		successes++
	}
	if len(parseErrors) > 0 {
		return nil, errors.Join(parseErrors...)
	}
	if successes == 0 {
		return nil, &remoteFetchError{err: fmt.Errorf("all global registries failed: %w", errors.Join(errorsBySource...))}
	}

	servers := make([]model.ServerMetadata, 0)
	for _, sourceServers := range ordered {
		servers = append(servers, sourceServers...)
	}
	return servers, nil
}

func fetchSource(ctx context.Context, client *http.Client, endpoint string) ([]byte, error) {
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
	if err != nil {
		return nil, fmt.Errorf("create registry request: %w", err)
	}
	request.Header.Set("Accept", "application/json")
	request.Header.Set("User-Agent", "oneclickvirt-speedtest-registry-sync/1")
	response, err := client.Do(request)
	if err != nil {
		return nil, &remoteFetchError{err: fmt.Errorf("fetch registry: %w", err)}
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		return nil, &remoteFetchError{err: fmt.Errorf("fetch registry: HTTP %d", response.StatusCode)}
	}
	raw, err := io.ReadAll(io.LimitReader(response.Body, 16<<20))
	if err != nil {
		return nil, &remoteFetchError{err: fmt.Errorf("read registry: %w", err)}
	}
	return raw, nil
}

func parseServerMetadata(data []byte) ([]model.ServerMetadata, error) {
	var servers []model.ServerMetadata
	if err := json.Unmarshal(data, &servers); err != nil {
		return nil, err
	}
	return servers, nil
}

type globalServer struct {
	ID      string `json:"id"`
	URL     string `json:"url"`
	Host    string `json:"host"`
	Name    string `json:"name"`
	Country string `json:"country"`
	Sponsor string `json:"sponsor"`
}

func parseGlobalServers(data []byte) ([]model.ServerMetadata, error) {
	var input []globalServer
	if err := json.Unmarshal(data, &input); err != nil {
		return nil, err
	}
	servers := make([]model.ServerMetadata, 0, len(input))
	for _, server := range input {
		id := strings.TrimSpace(server.ID)
		if id != "" {
			id = "global-" + id
		}
		servers = append(servers, model.ServerMetadata{ID: id, URL: server.URL, Host: server.Host, Name: server.Name, Country: server.Country, City: server.Name, Provider: server.Sponsor})
	}
	return servers, nil
}

type snapshotManifest struct {
	Schema      string `json:"schema"`
	File        string `json:"file"`
	Count       int    `json:"count"`
	SHA256      string `json:"sha256"`
	GeneratedAt string `json:"generated_at"`
}

func replaceSnapshot(output, manifestOutput string, candidate []byte) error {
	count, err := snapshotCount(candidate)
	if err != nil {
		return err
	}
	hash := sha256.Sum256(candidate)
	manifest := snapshotManifest{Schema: model.SpeedtestRegistrySchema, File: filepath.Base(output), Count: count, SHA256: hex.EncodeToString(hash[:]), GeneratedAt: time.Now().UTC().Format(time.RFC3339)}
	manifestData, err := json.MarshalIndent(manifest, "", "  ")
	if err != nil {
		return err
	}
	manifestData = append(manifestData, '\n')
	current, readErr := os.ReadFile(output)
	if readErr == nil {
		normalizedCurrent, normalizeErr := model.NormalizeServerRegistrySnapshot(current, 1)
		if normalizeErr == nil {
			currentCount, countErr := snapshotCount(normalizedCurrent)
			if countErr == nil && currentCount > 0 && count*100 < currentCount*65 {
				return fmt.Errorf("registry count dropped from %d to %d", currentCount, count)
			}
			if bytes.Equal(current, candidate) && manifestMatches(manifestOutput, output, candidate, count) {
				return nil
			}
		}
	} else if !errors.Is(readErr, os.ErrNotExist) {
		return fmt.Errorf("read existing snapshot: %w", readErr)
	}
	if err := writeAtomicSnapshot(output, candidate); err != nil {
		return err
	}
	return writeAtomicSnapshot(manifestOutput, manifestData)
}

func manifestMatches(path, output string, snapshot []byte, count int) bool {
	data, err := os.ReadFile(path)
	if err != nil {
		return false
	}
	var manifest snapshotManifest
	if json.Unmarshal(data, &manifest) != nil || manifest.Schema != model.SpeedtestRegistrySchema || manifest.File != filepath.Base(output) || manifest.Count != count {
		return false
	}
	hash := sha256.Sum256(snapshot)
	return manifest.SHA256 == hex.EncodeToString(hash[:])
}

func validateExistingSnapshot(output, manifestOutput string, minimum int) error {
	snapshot, err := os.ReadFile(output)
	if err != nil {
		return fmt.Errorf("read existing snapshot: %w", err)
	}
	normalized, err := model.NormalizeServerRegistrySnapshot(snapshot, minimum)
	if err != nil {
		return fmt.Errorf("validate existing snapshot: %w", err)
	}
	if !bytes.Equal(snapshot, normalized) {
		return errors.New("existing snapshot is not canonical")
	}
	count, err := snapshotCount(snapshot)
	if err != nil {
		return fmt.Errorf("count existing snapshot: %w", err)
	}
	manifestData, err := os.ReadFile(manifestOutput)
	if err != nil {
		return fmt.Errorf("read existing manifest: %w", err)
	}
	var manifest snapshotManifest
	if err := json.Unmarshal(manifestData, &manifest); err != nil {
		return fmt.Errorf("decode existing manifest: %w", err)
	}
	if manifest.Schema != model.SpeedtestRegistrySchema || manifest.File != filepath.Base(output) || manifest.Count != count {
		return errors.New("existing manifest does not match snapshot metadata")
	}
	if _, err := time.Parse(time.RFC3339, manifest.GeneratedAt); err != nil {
		return fmt.Errorf("validate existing manifest timestamp: %w", err)
	}
	hash := sha256.Sum256(snapshot)
	if manifest.SHA256 != hex.EncodeToString(hash[:]) {
		return errors.New("existing manifest hash does not match snapshot")
	}
	return nil
}

func writeAtomicSnapshot(output string, data []byte) error {
	if output == "" {
		return errors.New("snapshot path is empty")
	}
	if err := os.MkdirAll(filepath.Dir(output), 0o755); err != nil {
		return fmt.Errorf("create snapshot directory: %w", err)
	}
	temporary, err := os.CreateTemp(filepath.Dir(output), ".speedtest-registry-*")
	if err != nil {
		return fmt.Errorf("create temporary snapshot: %w", err)
	}
	temporaryName := temporary.Name()
	defer os.Remove(temporaryName)
	if err := temporary.Chmod(0o644); err != nil {
		_ = temporary.Close()
		return fmt.Errorf("set snapshot permissions: %w", err)
	}
	if _, err := temporary.Write(data); err != nil {
		_ = temporary.Close()
		return fmt.Errorf("write snapshot: %w", err)
	}
	if err := temporary.Sync(); err != nil {
		_ = temporary.Close()
		return fmt.Errorf("sync snapshot: %w", err)
	}
	if err := temporary.Close(); err != nil {
		return fmt.Errorf("close snapshot: %w", err)
	}
	if err := os.Rename(temporaryName, output); err != nil {
		return fmt.Errorf("replace snapshot: %w", err)
	}
	return nil
}

func snapshotCount(data []byte) (int, error) {
	var records []json.RawMessage
	if err := json.Unmarshal(data, &records); err != nil {
		return 0, err
	}
	return len(records), nil
}
