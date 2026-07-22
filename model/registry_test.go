package model

import (
	"context"
	"errors"
	"net"
	"net/http"
	"net/http/httptest"
	"net/url"
	"testing"
	"time"
)

func TestProbeAndSelectAvailableServers(t *testing.T) {
	servers := []ServerMetadata{{ID: "bad", Host: "bad:1"}, {ID: "good", Host: "good:2"}}
	probed := ProbeServers(context.Background(), servers, time.Second, 2, func(_ context.Context, _, address string) (net.Conn, error) {
		if address == "bad:1" {
			return nil, errors.New("failed")
		}
		local, remote := net.Pipe()
		go remote.Close()
		return local, nil
	})
	selected, err := SelectAvailableServers(probed, 1)
	if err != nil {
		t.Fatal(err)
	}
	if len(selected) != 1 || selected[0].ID != "good" || probed[0].Availability != ServerUnavailable {
		t.Fatalf("unexpected selection: probed=%+v selected=%+v", probed, selected)
	}
}

func TestProbeServersRejectsHTTP404(t *testing.T) {
	server := httptest.NewServer(http.NotFoundHandler())
	defer server.Close()
	parsed, err := url.Parse(server.URL)
	if err != nil {
		t.Fatal(err)
	}
	probed := ProbeServers(context.Background(), []ServerMetadata{{ID: "bad-path", Host: parsed.Host, URL: server.URL + "/upload"}}, time.Second, 1, nil)
	if len(probed) != 1 || probed[0].Availability != ServerUnavailable || probed[0].Error != "HTTP 404" {
		t.Fatalf("404 endpoint was accepted: %+v", probed)
	}
}

func TestSelectAvailableServersReturnsUnavailable(t *testing.T) {
	if _, err := SelectAvailableServers([]ServerMetadata{{Availability: ServerUnavailable}}, 1); err == nil {
		t.Fatal("expected unavailable error")
	}
}

func TestIsMainlandChinaServerKeepsCompatibilityRegionsSeparate(t *testing.T) {
	tests := []struct {
		country  string
		mainland bool
	}{
		{country: "China", mainland: true},
		{country: "CN", mainland: true},
		{country: "People's Republic of China", mainland: true},
		{country: "中国大陆", mainland: true},
		{country: "Hong Kong"},
		{country: "HK"},
		{country: "Taiwan"},
		{country: "Macao"},
		{country: "Japan"},
	}
	for _, test := range tests {
		t.Run(test.country, func(t *testing.T) {
			if got := IsMainlandChinaServer(ServerMetadata{Country: test.country}); got != test.mainland {
				t.Fatalf("IsMainlandChinaServer(%q) = %v, want %v", test.country, got, test.mainland)
			}
		})
	}
}

func TestFilterServersForLanguageExcludesMainlandAndUnknownOnlyForEnglish(t *testing.T) {
	servers := []ServerMetadata{
		{ID: "cn", Country: "China"},
		{ID: "unknown"},
		{ID: "hk", Country: "Hong Kong"},
		{ID: "jp", Country: "Japan"},
	}
	english := FilterServersForLanguage(servers, "en")
	if len(english) != 2 || english[0].ID != "hk" || english[1].ID != "jp" {
		t.Fatalf("unexpected English scope: %+v", english)
	}
	chinese := FilterServersForLanguage(servers, "zh")
	if len(chinese) != len(servers) {
		t.Fatalf("Chinese scope changed: %+v", chinese)
	}
}

func TestSelectRepresentativeServersSpreadsRegions(t *testing.T) {
	servers := []ServerMetadata{
		{ID: "jp-slow", Country: "Japan", Availability: ServerAvailable, LatencyMS: 20},
		{ID: "jp-fast", Country: "Japan", Availability: ServerAvailable, LatencyMS: 10},
		{ID: "uk", Country: "United Kingdom", Availability: ServerAvailable, LatencyMS: 80},
		{ID: "us", Country: "United States", Availability: ServerAvailable, LatencyMS: 100},
		{ID: "au", Country: "Australia", Availability: ServerAvailable, LatencyMS: 60},
		{ID: "br", Country: "Brazil", Availability: ServerAvailable, LatencyMS: 150},
	}
	selected, err := SelectRepresentativeServers(servers, 5)
	if err != nil {
		t.Fatal(err)
	}
	want := []string{"jp-fast", "uk", "us", "au", "br"}
	if len(selected) != len(want) {
		t.Fatalf("selected %d servers, want %d: %+v", len(selected), len(want), selected)
	}
	for index, id := range want {
		if selected[index].ID != id {
			t.Fatalf("selected[%d] = %q, want %q: %+v", index, selected[index].ID, id, selected)
		}
	}
}
