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
