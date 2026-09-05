package model

import (
	"context"
	"errors"
	"net"
	"net/http"
	"testing"
	"time"
)

func TestResolveServerAddressPinsRequestedFamily(t *testing.T) {
	originalLookup := lookupIPAddr
	t.Cleanup(func() { lookupIPAddr = originalLookup })
	lookupIPAddr = func(_ context.Context, host string) ([]net.IPAddr, error) {
		if host != "dual-stack.example" {
			return nil, errors.New("unexpected host")
		}
		return []net.IPAddr{
			{IP: net.ParseIP("2001:db8::45")},
			{IP: net.ParseIP("192.0.2.45")},
		}, nil
	}

	ipv4, err := ResolveServerAddress(context.Background(), "dual-stack.example:443", NetworkIPv4)
	if err != nil || ipv4 != "192.0.2.45:443" {
		t.Fatalf("IPv4 address = %q, %v", ipv4, err)
	}
	ipv6, err := ResolveServerAddress(context.Background(), "dual-stack.example:443", NetworkIPv6)
	if err != nil || ipv6 != "[2001:db8::45]:443" {
		t.Fatalf("IPv6 address = %q, %v", ipv6, err)
	}
	if _, err := ResolveServerAddress(context.Background(), "192.0.2.45:443", NetworkIPv6); err == nil {
		t.Fatal("IPv6 request unexpectedly accepted an IPv4-only endpoint")
	}
}

func TestNewHTTPClientBypassesProxyForExplicitFamily(t *testing.T) {
	for _, network := range []Network{NetworkIPv4, NetworkIPv6} {
		client := NewHTTPClient(network, time.Second)
		transport, ok := client.Transport.(*http.Transport)
		if !ok || transport.Proxy != nil {
			t.Fatalf("explicit %s client must not use an environment proxy", network)
		}
	}
	client := NewHTTPClient(NetworkAuto, time.Second)
	transport, ok := client.Transport.(*http.Transport)
	if !ok || transport.Proxy == nil {
		t.Fatal("automatic client unexpectedly stopped honoring environment proxy settings")
	}
}

func TestProbeServersWithNetworkPassesPinnedAddressToLaterStages(t *testing.T) {
	originalLookup := lookupIPAddr
	t.Cleanup(func() { lookupIPAddr = originalLookup })
	lookupIPAddr = func(_ context.Context, host string) ([]net.IPAddr, error) {
		if host != "dual-stack.example" {
			return nil, errors.New("unexpected host")
		}
		return []net.IPAddr{
			{IP: net.ParseIP("2001:db8::99")},
			{IP: net.ParseIP("198.51.100.99")},
		}, nil
	}

	var receivedNetwork, receivedAddress string
	probed := ProbeServersWithNetwork(context.Background(), []ServerMetadata{{ID: "fixture", Host: "dual-stack.example:8443"}}, time.Second, 1,
		func(_ context.Context, networkName, address string) (net.Conn, error) {
			receivedNetwork, receivedAddress = networkName, address
			client, peer := net.Pipe()
			go peer.Close()
			return client, nil
		}, NetworkIPv4)
	if len(probed) != 1 || probed[0].Availability != ServerAvailable {
		t.Fatalf("unexpected probe result: %+v", probed)
	}
	if receivedNetwork != "tcp" || receivedAddress != "198.51.100.99:8443" {
		t.Fatalf("dialled %s %s, want tcp 198.51.100.99:8443", receivedNetwork, receivedAddress)
	}
	if probed[0].Network != string(NetworkIPv4) || probed[0].ResolvedHost != "198.51.100.99:8443" {
		t.Fatalf("pinned endpoint was not retained: %+v", probed[0])
	}
}
