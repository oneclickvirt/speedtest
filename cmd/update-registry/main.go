package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"time"

	"github.com/oneclickvirt/speedtest/model"
)

const defaultSourceURL = "https://raw.githubusercontent.com/xykt/NetQuality/main/ref/speedtest_cn.json"

type updateConfig struct {
	Source  string
	Output  string
	Minimum int
	Timeout time.Duration
}

func main() {
	config := updateConfig{}
	flag.StringVar(&config.Source, "source", defaultSourceURL, "upstream registry URL")
	flag.StringVar(&config.Output, "output", "model/snapshot/speedtest-servers.json", "snapshot output path")
	flag.IntVar(&config.Minimum, "minimum", 10, "minimum valid servers")
	flag.DurationVar(&config.Timeout, "timeout", 30*time.Second, "upstream request timeout")
	flag.Parse()
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
	requestCtx, cancel := context.WithTimeout(ctx, config.Timeout)
	defer cancel()
	request, err := http.NewRequestWithContext(requestCtx, http.MethodGet, config.Source, nil)
	if err != nil {
		return fmt.Errorf("create registry request: %w", err)
	}
	request.Header.Set("Accept", "application/json")
	request.Header.Set("User-Agent", "oneclickvirt-speedtest-registry-sync/1")
	response, err := client.Do(request)
	if err != nil {
		return fmt.Errorf("fetch registry: %w", err)
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		return fmt.Errorf("fetch registry: HTTP %d", response.StatusCode)
	}
	raw, err := io.ReadAll(io.LimitReader(response.Body, 16<<20))
	if err != nil {
		return fmt.Errorf("read registry: %w", err)
	}
	data, err := model.NormalizeServerRegistrySnapshot(raw, config.Minimum)
	if err != nil {
		return fmt.Errorf("validate registry: %w", err)
	}
	current, readErr := os.ReadFile(config.Output)
	if readErr == nil {
		normalizedCurrent, normalizeErr := model.NormalizeServerRegistrySnapshot(current, 1)
		if normalizeErr == nil {
			currentCount, countErr := snapshotCount(normalizedCurrent)
			candidateCount, candidateErr := snapshotCount(data)
			if countErr == nil && candidateErr == nil && currentCount > 0 && candidateCount*100 < currentCount*65 {
				return fmt.Errorf("registry count dropped from %d to %d", currentCount, candidateCount)
			}
			if bytes.Equal(normalizedCurrent, data) {
				return nil
			}
		}
	}
	if readErr != nil && !errors.Is(readErr, os.ErrNotExist) {
		return fmt.Errorf("read existing snapshot: %w", readErr)
	}
	if err := os.MkdirAll(filepath.Dir(config.Output), 0o755); err != nil {
		return fmt.Errorf("create snapshot directory: %w", err)
	}
	temporary, err := os.CreateTemp(filepath.Dir(config.Output), ".speedtest-registry-*")
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
	if err := os.Rename(temporaryName, config.Output); err != nil {
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
