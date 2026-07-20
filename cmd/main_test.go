package main

import (
	"bytes"
	"context"
	"encoding/json"
	"net"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/oneclickvirt/speedtest/model"
)

func TestSpeedtestFlagDefaultsAndParsing(t *testing.T) {
	var defaults cliOptions
	if err := newSpeedtestFlagSet(&defaults).Parse(nil); err != nil {
		t.Fatal(err)
	}
	if defaults.language != "zh" || defaults.platform != "net" || defaults.operator != "global" || defaults.method != "speedtest" || defaults.num != -1 || !defaults.showHead {
		t.Fatalf("unexpected defaults: %+v", defaults)
	}

	var configured cliOptions
	if err := newSpeedtestFlagSet(&configured).Parse([]string{"-l", "en", "-pf", "cn", "-opt", "ct", "-m", "origin", "-num", "2", "-nearby", "-s=false"}); err != nil {
		t.Fatal(err)
	}
	if configured.language != "en" || configured.platform != "cn" || configured.operator != "ct" || configured.method != "origin" || configured.num != 2 || !configured.nearby || configured.showHead {
		t.Fatalf("unexpected parsed options: %+v", configured)
	}

	var registry cliOptions
	if err := newSpeedtestFlagSet(&registry).Parse([]string{"-registry", "-num", "1"}); err != nil {
		t.Fatal(err)
	}
	if !registry.registry || registry.num != 1 {
		t.Fatalf("registry mode not parsed: %+v", registry)
	}
}

func TestWriteRegistryReportProducesStructuredJSON(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(`[{"id":"fixture","name":"Fixture","host":"fixture.test:443"}]`))
	}))
	defer server.Close()
	var output bytes.Buffer
	err := writeRegistryReport(context.Background(), &output, server.Client(), []model.RegistrySource{{Name: "fixture", URL: server.URL}}, 1,
		func(context.Context, string, string) (net.Conn, error) {
			client, peer := net.Pipe()
			go peer.Close()
			return client, nil
		})
	if err != nil {
		t.Fatal(err)
	}
	var report model.RegistryReport
	if err := json.Unmarshal(output.Bytes(), &report); err != nil {
		t.Fatal(err)
	}
	if report.Availability != model.ServerAvailable || len(report.Selected) != 1 || report.Source != "fixture" {
		t.Fatalf("unexpected CLI report: %+v", report)
	}
}
