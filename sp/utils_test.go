package sp

import (
	"bytes"
	"context"
	"io"
	"math"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/oneclickvirt/speedtest/model"
	showwinspeedtest "github.com/showwin/speedtest-go/speedtest"
)

func TestFormatMbps(t *testing.T) {
	tests := []struct {
		name  string
		value float64
		want  string
	}{
		{name: "negative", value: -0.0001, want: "0.00 Mbps"},
		{name: "nan", value: math.NaN(), want: "0.00 Mbps"},
		{name: "positive", value: 293.075, want: "293.08 Mbps"},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := formatMbps(tc.value)
			if got != tc.want {
				t.Fatalf("formatMbps(%v) = %q, want %q", tc.value, got, tc.want)
			}
		})
	}
}

func TestShowHeadToWritesToCallerWriter(t *testing.T) {
	var output bytes.Buffer
	ShowHeadTo(&output, "en")
	if got := output.String(); got == "" || !strings.Contains(got, "Location") || !strings.Contains(got, "PacketLoss") {
		t.Fatalf("ShowHeadTo output = %q, want the English table header", got)
	}
}

func TestShowHeadToAcceptsNilWriter(t *testing.T) {
	ShowHeadTo(nil, "en")
}

func TestGetDataWithNetworkContextStopsWhenCanceled(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	started := time.Now()
	if got := getDataWithNetworkContext(ctx, "https://example.invalid/servers.csv", "tcp6"); got != "" {
		t.Fatalf("canceled registry load returned %q", got)
	}
	if elapsed := time.Since(started); elapsed > 250*time.Millisecond {
		t.Fatalf("canceled registry load took %s", elapsed)
	}
}

func incompleteIDLookupClient() *showwinspeedtest.Speedtest {
	return showwinspeedtest.New(
		showwinspeedtest.WithUserConfig(&showwinspeedtest.UserConfig{MaxConnections: 1}),
		showwinspeedtest.WithDoer(&http.Client{Transport: roundTripFunc(func(request *http.Request) (*http.Response, error) {
			body := `<settings><servers><server id="16204" url="http://incomplete.example/speedtest/upload.php" host=""/></servers></settings>`
			return &http.Response{
				StatusCode: http.StatusOK,
				Body:       io.NopCloser(strings.NewReader(body)),
				Header:     make(http.Header),
				Request:    request,
			}, nil
		})}),
	)
}

func TestParseDataFromIDWithClientSkipsIncompleteUpstreamServer(t *testing.T) {
	data := "id,country_code,country,city,ip,host,port,supplier\n16204,CN,China,Suzhou,127.0.0.1,incomplete.example,8080,fixture\n"
	targets := parseDataFromIDWithClientContext(context.Background(), data, model.NetCMCC, incompleteIDLookupClient())
	if len(targets) != 0 {
		t.Fatalf("incomplete ID lookup returned %d target(s), want none", len(targets))
	}
}

func TestCustomSpeedtestTargetsFallBackToEmbeddedRegistry(t *testing.T) {
	data := "16204,CN,China,Suzhou,127.0.0.1,incomplete.example,8080,fixture\n"
	targets, usedFallback := customSpeedtestTargetsFromData(context.Background(), data, model.NetCMCC, "id", incompleteIDLookupClient(), "auto")
	if !usedFallback {
		t.Fatal("embedded fallback was not selected")
	}
	if len(targets) != 1 {
		t.Fatalf("fallback targets = %d, want 1", len(targets))
	}
	if targets[0].ID != "16204" || targets[0].Name != "移动Suzhou" {
		t.Fatalf("fallback target = %+v, want legacy ID and carrier label", targets[0])
	}
	if strings.TrimSpace(targets[0].Host) == "" || strings.TrimSpace(targets[0].URL) == "" {
		t.Fatalf("fallback target is incomplete: %+v", targets[0])
	}
}

func TestLegacyIDFallbackTargetsPreserveIPv4(t *testing.T) {
	data := "16204,CN,China,Suzhou,127.0.0.1,fixture.example,8080,fixture\n"
	servers := []model.ServerMetadata{{
		ID:   "global-16204",
		Name: "Suzhou",
		Host: "127.0.0.1:8080",
		URL:  "http://127.0.0.1:8080/speedtest/upload.php",
	}}
	targets := legacyIDFallbackTargetsFromRegistry(context.Background(), data, model.NetCMCC, incompleteIDLookupClient(), servers)
	targets = pinSpeedtestServersContext(context.Background(), targets, "ipv4")
	if len(targets) != 1 {
		t.Fatalf("IPv4 fallback targets = %d, want 1", len(targets))
	}
	if targets[0].Host != "127.0.0.1:8080" {
		t.Fatalf("IPv4 fallback host = %q, want 127.0.0.1:8080", targets[0].Host)
	}
}

func TestLegacyIDFallbackTargetsBuildDirectEndpointForMissingSnapshotID(t *testing.T) {
	data := "13516,CA,United States,LosAngeles,72.34.255.50,speedtest.lax01.xfernet.net.prod.hosts.ooklaserver.net,8080,Xfernet\n"
	targets := legacyIDFallbackTargets(context.Background(), data, model.NetGlobal, incompleteIDLookupClient())
	if len(targets) != 1 {
		t.Fatalf("fallback targets = %d, want 1", len(targets))
	}
	target := targets[0]
	if target.ID != "13516" || target.Name != "LosAngeles" {
		t.Fatalf("unexpected fallback target: %+v", target)
	}
	if target.Host != "speedtest.lax01.xfernet.net.prod.hosts.ooklaserver.net:8080" {
		t.Fatalf("fallback host = %q", target.Host)
	}
	if target.URL != "http://speedtest.lax01.xfernet.net.prod.hosts.ooklaserver.net:8080/speedtest/upload.php" {
		t.Fatalf("fallback URL = %q", target.URL)
	}
}

func TestLegacyIDFallbackTargetsRejectInvalidEndpoint(t *testing.T) {
	data := "missing,CN,China,Nowhere,127.0.0.1,invalid.example,not-a-port,fixture\n"
	targets := legacyIDFallbackTargetsFromRecords(context.Background(), data, model.NetGlobal, incompleteIDLookupClient())
	if len(targets) != 0 {
		t.Fatalf("invalid endpoint produced %d target(s)", len(targets))
	}
}

func TestFetchOrBuildRegistrySpeedtestServerUsesDirectURLForIncompleteLookup(t *testing.T) {
	metadata := model.ServerMetadata{
		ID:      "global-16204",
		Name:    "Suzhou",
		Host:    "127.0.0.1:8080",
		URL:     "http://127.0.0.1:8080/speedtest/upload.php",
		Country: "China",
		City:    "Suzhou",
	}
	server, usedDirectEndpoint, err := fetchOrBuildRegistrySpeedtestServerWithFallback(context.Background(), incompleteIDLookupClient(), metadata)
	if err != nil {
		t.Fatalf("fetch or build registry server: %v", err)
	}
	if !usedDirectEndpoint {
		t.Fatal("incomplete ID lookup should select the direct endpoint")
	}
	if server.ID != "16204" || server.Host != "127.0.0.1:8080" || server.URL == "" {
		t.Fatalf("direct registry fallback = %+v", server)
	}
}
