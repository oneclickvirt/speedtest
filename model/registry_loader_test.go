package model

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func TestDefaultRegistrySourcesBelongToSpeedtestRepository(t *testing.T) {
	for _, source := range DefaultRegistrySources() {
		if !strings.Contains(source.URL, "oneclickvirt/speedtest/main/model/snapshot/speedtest-servers.json") || strings.Contains(source.URL, "ecs-data") {
			t.Fatalf("unexpected registry source: %+v", source)
		}
	}
}

func TestLoadServerRegistryRejectsBadManifestAndUsesNextSource(t *testing.T) {
	data := []byte(`[{"id":"fixture","name":"Fixture","host":"fixture.test:443","url":"https://fixture.test/upload"}]`)
	hash := sha256.Sum256(data)
	manifest := ServerRegistryManifest{Schema: SpeedtestRegistrySchema, File: "speedtest-servers.json", Count: 1, SHA256: hex.EncodeToString(hash[:]), GeneratedAt: time.Now().UTC().Format(time.RFC3339)}
	manifestData, _ := json.Marshal(manifest)
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		switch request.URL.Path {
		case "/cdn-manifest":
			bad := manifest
			bad.Count = 2
			_ = json.NewEncoder(writer).Encode(bad)
		case "/raw-manifest":
			_, _ = writer.Write(manifestData)
		default:
			_, _ = writer.Write(data)
		}
	}))
	defer server.Close()
	loaded, err := LoadServerRegistry(context.Background(), server.Client(), []RegistrySource{
		{Name: "cdn", URL: server.URL + "/cdn-data", ManifestURL: server.URL + "/cdn-manifest"},
		{Name: "raw", URL: server.URL + "/raw-data", ManifestURL: server.URL + "/raw-manifest"},
	}, 1)
	if err != nil || loaded.Source != "raw" || !loaded.Fallback {
		t.Fatalf("unexpected manifest fallback: %+v, %v", loaded, err)
	}
	if loaded.Metadata.Schema != SpeedtestRegistrySchema || loaded.Metadata.Count != 1 || loaded.Metadata.SHA256 != manifest.SHA256 {
		t.Fatalf("manifest metadata missing: %+v", loaded.Metadata)
	}
}

func TestResolveServerRegistryFallsBackAndSelects(t *testing.T) {
	cdn := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(`{"message":"temporary proxy payload"}`))
	}))
	defer cdn.Close()
	raw := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(`[
			{"id":"bad","name":"Bad","host":"bad.test:8080","provider":"one","country":"CN","city":"A"},
			{"id":"good","name":"Good","host":"good.test:8080","provider":"two","country":"US","city":"B"}
		]`))
	}))
	defer raw.Close()

	report := ResolveServerRegistry(context.Background(), raw.Client(), []RegistrySource{
		{Name: "cdn", URL: cdn.URL},
		{Name: "raw", URL: raw.URL},
	}, 1, 1, time.Second, 2, func(_ context.Context, _, address string) (net.Conn, error) {
		if address != "good.test:8080" {
			return nil, errors.New("fixture unavailable")
		}
		client, server := net.Pipe()
		go server.Close()
		return client, nil
	})
	if report.Source != "raw" || !report.Fallback || report.Availability != ServerAvailable {
		t.Fatalf("unexpected report metadata: %+v", report)
	}
	if len(report.Selected) != 1 || report.Selected[0].ID != "good" || report.Selected[0].Source != "raw" {
		t.Fatalf("unexpected selected servers: %+v", report.Selected)
	}
	if report.Servers[0].Availability != ServerUnavailable || report.Servers[0].Error != "connection_error" {
		t.Fatalf("unavailable evidence missing: %+v", report.Servers)
	}
}

func TestResolveServerRegistryReportsAllUnavailable(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(`[{"id":"dead","name":"Dead","host":"dead.test:443"}]`))
	}))
	defer server.Close()
	report := ResolveServerRegistry(context.Background(), server.Client(), []RegistrySource{{Name: "raw", URL: server.URL}}, 1, 1, time.Second, 1,
		func(context.Context, string, string) (net.Conn, error) { return nil, errors.New("offline") })
	if report.Availability != ServerUnavailable || len(report.Selected) != 0 || report.Error == "" {
		t.Fatalf("expected explicit unavailable report: %+v", report)
	}
}

func TestLoadServerRegistryFallsBackToEmbeddedSnapshot(t *testing.T) {
	server := httptest.NewServer(http.NotFoundHandler())
	defer server.Close()
	loaded, err := LoadServerRegistry(context.Background(), server.Client(), []RegistrySource{{Name: "cdn", URL: server.URL}}, 10)
	if err != nil {
		t.Fatal(err)
	}
	if loaded.Source != "embedded" || !loaded.Fallback || len(loaded.Servers) < 10 {
		t.Fatalf("unexpected embedded fallback: %#v", loaded)
	}
	if loaded.Metadata.Schema != SpeedtestRegistrySchema || loaded.Metadata.Count != len(loaded.Servers) || loaded.Metadata.GeneratedAt == "" || len(loaded.Metadata.SHA256) != 64 {
		t.Fatalf("unexpected embedded metadata: %+v", loaded.Metadata)
	}
	for _, server := range loaded.Servers {
		if server.Source != "embedded" || (server.Availability != ServerCandidate && server.Availability != ServerUnavailable) {
			t.Fatalf("invalid embedded node metadata: %#v", server)
		}
	}
}
