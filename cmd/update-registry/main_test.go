package main

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
	"time"
)

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
