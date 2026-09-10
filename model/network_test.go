package model

import (
	"bufio"
	"bytes"
	"context"
	"errors"
	"io"
	"log"
	"net"
	"net/http"
	"strings"
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

func TestNewThroughputHTTPClientDisablesIdleConnectionReuse(t *testing.T) {
	client := NewThroughputHTTPClient(NetworkIPv4, time.Second)
	transport, ok := client.Transport.(*http.Transport)
	if !ok {
		t.Fatalf("throughput transport type = %T, want *http.Transport", client.Transport)
	}
	if !transport.DisableKeepAlives {
		t.Fatal("throughput client may reuse a malformed speedtest server connection")
	}
	if transport.Proxy != nil {
		t.Fatal("explicit-family throughput client unexpectedly uses an environment proxy")
	}
}

func TestNewThroughputHTTPClientDoesNotPublishTrailingServerBytes(t *testing.T) {
	listener, err := net.Listen("tcp4", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = listener.Close() })

	requestClose := make(chan bool, 1)
	serverDone := make(chan error, 1)
	go func() {
		connection, acceptErr := listener.Accept()
		if acceptErr != nil {
			serverDone <- acceptErr
			return
		}
		defer connection.Close()
		request, readErr := http.ReadRequest(bufio.NewReader(connection))
		if readErr != nil {
			serverDone <- readErr
			return
		}
		requestClose <- request.Close
		_ = request.Body.Close()
		_, writeErr := io.WriteString(connection, "HTTP/1.1 200 OK\r\nContent-Length: 1\r\n\r\nxTRAILING-BINARY-PAYLOAD")
		serverDone <- writeErr
	}()

	var logs bytes.Buffer
	previousWriter, previousFlags, previousPrefix := log.Writer(), log.Flags(), log.Prefix()
	log.SetOutput(&logs)
	log.SetFlags(0)
	log.SetPrefix("")
	t.Cleanup(func() {
		log.SetOutput(previousWriter)
		log.SetFlags(previousFlags)
		log.SetPrefix(previousPrefix)
	})

	client := NewThroughputHTTPClient(NetworkIPv4, time.Second)
	response, err := client.Get("http://" + listener.Addr().String())
	if err != nil {
		t.Fatal(err)
	}
	body, err := io.ReadAll(response.Body)
	_ = response.Body.Close()
	if err != nil {
		t.Fatal(err)
	}
	if string(body) != "x" {
		t.Fatalf("response body = %q, want declared body only", body)
	}
	if closeRequested := <-requestClose; !closeRequested {
		t.Fatal("throughput request did not ask the malformed endpoint to close")
	}
	if err := <-serverDone; err != nil {
		t.Fatal(err)
	}
	if output := logs.String(); strings.Contains(output, "Unsolicited response received on idle HTTP channel") {
		t.Fatalf("trailing speedtest payload leaked into process logs: %q", output)
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
	if receivedNetwork != "tcp4" || receivedAddress != "198.51.100.99:8443" {
		t.Fatalf("dialled %s %s, want tcp4 198.51.100.99:8443", receivedNetwork, receivedAddress)
	}
	if probed[0].Network != string(NetworkIPv4) || probed[0].ResolvedHost != "198.51.100.99:8443" {
		t.Fatalf("pinned endpoint was not retained: %+v", probed[0])
	}
}

func TestProbeServersWithNetworkAcceptsNilContextAndPinsIPv6Dial(t *testing.T) {
	var receivedNetwork, receivedAddress string
	probed := ProbeServersWithNetwork(nil, []ServerMetadata{{ID: "fixture", Host: "[2001:db8::99]:8443"}}, time.Second, 1,
		func(_ context.Context, networkName, address string) (net.Conn, error) {
			receivedNetwork, receivedAddress = networkName, address
			client, peer := net.Pipe()
			go peer.Close()
			return client, nil
		}, NetworkIPv6)
	if len(probed) != 1 || probed[0].Availability != ServerAvailable {
		t.Fatalf("unexpected IPv6 probe result: %+v", probed)
	}
	if receivedNetwork != "tcp6" || receivedAddress != "[2001:db8::99]:8443" {
		t.Fatalf("dialled %s %s, want tcp6 [2001:db8::99]:8443", receivedNetwork, receivedAddress)
	}
}
