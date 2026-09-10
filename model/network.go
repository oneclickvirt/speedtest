package model

import (
	"context"
	"crypto/tls"
	"fmt"
	"net"
	"net/http"
	"strings"
	"time"

	networkutils "github.com/oneclickvirt/basics/network/utils"
)

// Network identifies the address family used by a request. Empty/auto keeps
// the operating system's normal dual-stack selection; tcp4 and tcp6 are
// explicit and never fall back to the other family.
type Network string

const (
	NetworkAuto Network = ""
	NetworkIPv4 Network = "tcp4"
	NetworkIPv6 Network = "tcp6"
)

func NormalizeNetwork(value string) (Network, error) {
	network, err := networkutils.NormalizeNetwork(value)
	if err != nil {
		return NetworkAuto, fmt.Errorf("invalid network %q: expected auto, ipv4, or ipv6", value)
	}
	return Network(network), nil
}

func DialContext(network Network) func(context.Context, string, string) (net.Conn, error) {
	dial, err := networkutils.DialContext(string(network))
	if err == nil {
		return dial
	}
	return (&net.Dialer{Timeout: 8 * time.Second, KeepAlive: 30 * time.Second}).DialContext
}

var lookupIPAddr = func(ctx context.Context, host string) ([]net.IPAddr, error) {
	return net.DefaultResolver.LookupIPAddr(ctx, host)
}

// ResolveServerAddress converts a hostname and port into an address pinned to
// the requested family. Keeping the URL hostname unchanged preserves TLS SNI,
// while TCP and UDP paths in speedtest-go can use this literal address.
func ResolveServerAddress(ctx context.Context, address string, network Network) (string, error) {
	network, err := NormalizeNetwork(string(network))
	if err != nil {
		return "", err
	}
	address = strings.TrimSpace(address)
	if network == NetworkAuto {
		return address, nil
	}
	if ctx == nil {
		ctx = context.Background()
	}
	host, port, err := net.SplitHostPort(address)
	if err != nil || strings.Trim(strings.TrimSpace(host), "[]") == "" || port == "" {
		return "", fmt.Errorf("invalid server address %q", address)
	}
	host = strings.Trim(host, "[]")
	if ip := net.ParseIP(host); ip != nil {
		if matchesNetwork(ip, network) {
			return net.JoinHostPort(ip.String(), port), nil
		}
		return "", fmt.Errorf("server address %q does not support %s", address, networkLabel(network))
	}
	addresses, err := lookupIPAddr(ctx, host)
	if err != nil {
		return "", fmt.Errorf("resolve %s: %w", host, err)
	}
	for _, candidate := range addresses {
		if matchesNetwork(candidate.IP, network) {
			return net.JoinHostPort(candidate.IP.String(), port), nil
		}
	}
	return "", fmt.Errorf("no %s address for %s", networkLabel(network), host)
}

func matchesNetwork(ip net.IP, network Network) bool {
	if ip == nil {
		return false
	}
	switch network {
	case NetworkIPv4:
		return ip.To4() != nil
	case NetworkIPv6:
		return ip.To4() == nil && ip.To16() != nil
	default:
		return true
	}
}

func networkLabel(network Network) string {
	switch network {
	case NetworkIPv4:
		return "IPv4"
	case NetworkIPv6:
		return "IPv6"
	default:
		return "automatic"
	}
}

// NewHTTPClient creates the family-aware client used by registry and metadata
// requests. Auto retains ordinary dual-stack behavior and connection reuse.
func NewHTTPClient(network Network, timeout time.Duration) *http.Client {
	return newHTTPClient(network, timeout, false)
}

// NewThroughputHTTPClient creates the family-aware client used for speedtest
// transfer endpoints. Some Ookla-compatible servers send bytes after the
// declared response body has ended. Reusing those connections makes net/http
// print the trailing binary payload as an "Unsolicited response" diagnostic,
// so transfer clients deliberately close each HTTP/1.x connection instead.
func NewThroughputHTTPClient(network Network, timeout time.Duration) *http.Client {
	return newHTTPClient(network, timeout, true)
}

func newHTTPClient(network Network, timeout time.Duration, disableKeepAlives bool) *http.Client {
	if timeout <= 0 {
		timeout = 12 * time.Second
	}
	proxy := http.ProxyFromEnvironment
	// A proxy decides the origin-side family itself. For an explicit IPv4 or
	// IPv6 measurement, bypass it so the requested family reaches the target.
	if network != NetworkAuto {
		proxy = nil
	}
	transport := &http.Transport{
		Proxy:                 proxy,
		DialContext:           DialContext(network),
		DisableKeepAlives:     disableKeepAlives,
		ForceAttemptHTTP2:     true,
		MaxIdleConns:          32,
		MaxIdleConnsPerHost:   8,
		IdleConnTimeout:       30 * time.Second,
		TLSHandshakeTimeout:   8 * time.Second,
		ResponseHeaderTimeout: timeout,
		ExpectContinueTimeout: 1 * time.Second,
		TLSClientConfig:       &tls.Config{MinVersion: tls.VersionTLS12},
	}
	return &http.Client{Timeout: timeout, Transport: transport}
}
