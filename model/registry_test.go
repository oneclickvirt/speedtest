package model

import (
	"context"
	"errors"
	"net"
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

func TestSelectAvailableServersReturnsUnavailable(t *testing.T) {
	if _, err := SelectAvailableServers([]ServerMetadata{{Availability: ServerUnavailable}}, 1); err == nil {
		t.Fatal("expected unavailable error")
	}
}
