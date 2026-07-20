package main

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestUpdateSnapshotMergesChinaAndGlobalRegistries(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/global-a":
			_, _ = w.Write([]byte(`[{"id":"9001","url":"https://global.example/speedtest/upload.php","host":"global.example:443","name":"Global","country":"US","sponsor":"Fixture"}]`))
		case "/global-b":
			_, _ = w.Write([]byte(`[{"id":"9001","url":"https://duplicate.example/speedtest/upload.php","host":"duplicate.example:443","name":"Duplicate","country":"US","sponsor":"Fixture"},{"id":"9002","url":"https://second.example/speedtest/upload.php","host":"second.example:443","name":"Second","country":"JP","sponsor":"Fixture"}]`))
		default:
			_, _ = w.Write([]byte(`[{"id":"1","name":"China","host":"cn.example:443","url":"https://cn.example/speedtest/upload.php"},{"id":"2","name":"China2","host":"cn2.example:443","url":"https://cn2.example/speedtest/upload.php"}]`))
		}
	}))
	defer server.Close()
	output := filepath.Join(t.TempDir(), "speedtest-servers.json")
	if err := updateSnapshot(context.Background(), server.Client(), updateConfig{Source: server.URL + "/china", GlobalSources: []string{server.URL + "/global-a", server.URL + "/global-b"}, Output: output, Minimum: 4, Timeout: time.Second}); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(output)
	if err != nil {
		t.Fatal(err)
	}
	var records []struct {
		ID string `json:"id"`
	}
	if err := json.Unmarshal(data, &records); err != nil || len(records) != 4 {
		t.Fatalf("merged registry count = %d, err=%v", len(records), err)
	}
	if records[2].ID != "global-9001" {
		t.Fatalf("global server was not normalized: %+v", records)
	}
	if records[3].ID != "global-9002" {
		t.Fatalf("global registries were not stably merged: %+v", records)
	}
}

func TestUpdateSnapshotToleratesOneFailedGlobalSource(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/failed":
			http.Error(w, "unavailable", http.StatusServiceUnavailable)
		case "/global":
			_, _ = w.Write([]byte(`[{"id":"9001","host":"global.example:443","name":"Global"}]`))
		default:
			_, _ = w.Write([]byte(`[{"id":"1","host":"cn.example:443","name":"China"}]`))
		}
	}))
	defer server.Close()
	output := filepath.Join(t.TempDir(), "speedtest-servers.json")
	err := updateSnapshot(context.Background(), server.Client(), updateConfig{
		Source:        server.URL + "/china",
		GlobalSources: []string{server.URL + "/failed", server.URL + "/global"},
		Output:        output,
		Minimum:       2,
		Timeout:       time.Second,
	})
	if err != nil {
		t.Fatal(err)
	}
}

func TestUpdateSnapshotRejectsAllFailedGlobalSources(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/china" {
			_, _ = w.Write([]byte(`[{"id":"1","host":"cn.example:443","name":"China"}]`))
			return
		}
		http.Error(w, "unavailable", http.StatusServiceUnavailable)
	}))
	defer server.Close()
	output := filepath.Join(t.TempDir(), "speedtest-servers.json")
	err := updateSnapshot(context.Background(), server.Client(), updateConfig{
		Source:        server.URL + "/china",
		GlobalSources: []string{server.URL + "/failed-a", server.URL + "/failed-b"},
		Output:        output,
		Minimum:       1,
		Timeout:       time.Second,
	})
	if err == nil {
		t.Fatal("expected all-global-sources failure")
	}
	if _, statErr := os.Stat(output); !os.IsNotExist(statErr) {
		t.Fatalf("failed global update wrote output: %v", statErr)
	}
}

func TestDefaultGlobalSourcesUseDistinctGeographicAnchors(t *testing.T) {
	if len(defaultGlobalSourceURLs) != 5 {
		t.Fatalf("default global source count = %d", len(defaultGlobalSourceURLs))
	}
	anchors := make(map[string]struct{}, len(defaultGlobalSourceURLs))
	for _, endpoint := range defaultGlobalSourceURLs {
		parsed, err := url.Parse(endpoint)
		if err != nil {
			t.Fatal(err)
		}
		query := parsed.Query()
		if parsed.Host != "www.speedtest.net" || query.Get("engine") != "js" || query.Get("https_functional") != "true" || query.Get("limit") != "100" || query.Get("lat") == "" || query.Get("lon") == "" {
			t.Fatalf("invalid default global source: %s", endpoint)
		}
		anchor := query.Get("lat") + "," + query.Get("lon")
		if _, exists := anchors[anchor]; exists {
			t.Fatalf("duplicate global anchor: %s", anchor)
		}
		anchors[anchor] = struct{}{}
	}
}

func TestUpdateSnapshotNormalizesAndIsIdempotent(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(`[
  {"id":"b","name":"B","host":"b.example:443","url":"https://b.example/speedtest/upload.php"},
  {"id":"a","name":"A","host":"a.example:443","url":"https://a.example/speedtest/upload.php"}
]`))
	}))
	defer server.Close()
	output := filepath.Join(t.TempDir(), "speedtest-servers.json")
	config := updateConfig{Source: server.URL, Output: output, Minimum: 2, Timeout: time.Second}
	if err := updateSnapshot(context.Background(), server.Client(), config); err != nil {
		t.Fatal(err)
	}
	first, err := os.ReadFile(output)
	if err != nil {
		t.Fatal(err)
	}
	var records []struct {
		ID string `json:"id"`
	}
	if err := json.Unmarshal(first, &records); err != nil || len(records) != 2 || records[0].ID != "a" || records[1].ID != "b" {
		t.Fatalf("snapshot was not normalized: records=%+v err=%v", records, err)
	}
	if err := updateSnapshot(context.Background(), server.Client(), config); err != nil {
		t.Fatal(err)
	}
	second, err := os.ReadFile(output)
	if err != nil {
		t.Fatal(err)
	}
	if string(first) != string(second) {
		t.Fatal("idempotent update changed snapshot")
	}
}

func TestUpdateSnapshotRejectsMinimumAndTimeout(t *testing.T) {
	config := updateConfig{Source: "https://example.test", Output: "snapshot.json", Minimum: 0, Timeout: time.Second}
	if err := updateSnapshot(context.Background(), nil, config); err == nil {
		t.Fatal("expected invalid minimum error")
	}
	config.Minimum, config.Timeout = 1, 0
	if err := updateSnapshot(context.Background(), nil, config); err == nil {
		t.Fatal("expected invalid timeout error")
	}
}

func TestUpdateSnapshotRejectsSevereCountDrop(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(`[{"id":"one","name":"One","host":"one.example:443"}]`))
	}))
	defer server.Close()
	output := filepath.Join(t.TempDir(), "speedtest-servers.json")
	existing := `[
		{"id":"one","name":"One","host":"one.example:443"},
		{"id":"two","name":"Two","host":"two.example:443"},
		{"id":"three","name":"Three","host":"three.example:443"}
	]`
	if err := os.WriteFile(output, []byte(existing), 0o644); err != nil {
		t.Fatal(err)
	}
	err := updateSnapshot(context.Background(), server.Client(), updateConfig{Source: server.URL, Output: output, Minimum: 1, Timeout: time.Second})
	if err == nil {
		t.Fatal("expected severe count drop error")
	}
	current, readErr := os.ReadFile(output)
	if readErr != nil || string(current) != existing {
		t.Fatalf("count drop overwrote snapshot: err=%v data=%q", readErr, current)
	}
}
