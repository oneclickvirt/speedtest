package main

import (
	"bytes"
	"context"
	"encoding/json"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/oneclickvirt/speedtest/model"
)

func TestSpeedtestFlagDefaultsAndParsing(t *testing.T) {
	var defaults cliOptions
	if err := newSpeedtestFlagSet(&defaults).Parse(nil); err != nil {
		t.Fatal(err)
	}
	if defaults.language != "zh" || defaults.platform != "net" || defaults.operator != "global" || defaults.method != "speedtest" || defaults.dnsMode != "auto" || defaults.network != "auto" || defaults.num != -1 || !defaults.showHead {
		t.Fatalf("unexpected defaults: %+v", defaults)
	}

	var configured cliOptions
	if err := newSpeedtestFlagSet(&configured).Parse([]string{"-l", "en", "-pf", "cn", "-opt", "ct", "-m", "origin", "-ip-version", "4", "-num", "2", "-nearby", "-s=false"}); err != nil {
		t.Fatal(err)
	}
	if configured.language != "en" || configured.platform != "cn" || configured.operator != "ct" || configured.method != "origin" || configured.dnsMode != "auto" || configured.network != "4" || configured.num != 2 || !configured.nearby || configured.showHead {
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

func TestNormalizeAndValidateCLI(t *testing.T) {
	base := cliOptions{language: " EN ", platform: " NET ", operator: " GLOBAL ", method: " SPEEDTEST-GO ", dnsMode: " DoT ", network: " IPV4 ", num: -1}
	normalized, err := normalizeAndValidateCLI(base, nil)
	if err != nil {
		t.Fatal(err)
	}
	if normalized.language != "en" || normalized.platform != "net" || normalized.operator != "global" || normalized.method != "speedtest-go" || normalized.dnsMode != "dot" || normalized.network != "ipv4" {
		t.Fatalf("options were not normalized: %+v", normalized)
	}

	tests := []struct {
		name    string
		mutate  func(*cliOptions)
		args    []string
		message string
	}{
		{name: "language", mutate: func(o *cliOptions) { o.language = "fr" }, message: "invalid -l"},
		{name: "platform", mutate: func(o *cliOptions) { o.platform = "other" }, message: "invalid -pf"},
		{name: "operator", mutate: func(o *cliOptions) { o.operator = "other" }, message: "invalid -opt"},
		{name: "method", mutate: func(o *cliOptions) { o.method = "other" }, message: "invalid -m"},
		{name: "DNS mode", mutate: func(o *cliOptions) { o.dnsMode = "other" }, message: "invalid -dns-mode"},
		{name: "IP version", mutate: func(o *cliOptions) { o.network = "other" }, message: "invalid -ip-version"},
		{name: "zero count", mutate: func(o *cliOptions) { o.num = 0 }, message: "invalid -num"},
		{name: "negative count", mutate: func(o *cliOptions) { o.num = -2 }, message: "invalid -num"},
		{name: "positional", args: []string{"unexpected"}, message: "unexpected positional"},
		{name: "conflicting modes", mutate: func(o *cliOptions) { o.registry, o.nearby = true, true }, message: "cannot be used together"},
		{name: "English CN platform", mutate: func(o *cliOptions) { o.language, o.platform, o.operator = "en", "cn", "hk" }, message: "mainland-China specific"},
		{name: "English mobile", mutate: func(o *cliOptions) { o.language, o.operator = "en", "cmcc" }, message: "mainland-China specific"},
		{name: "English unicom", mutate: func(o *cliOptions) { o.language, o.operator = "en", "cu" }, message: "mainland-China specific"},
		{name: "English telecom", mutate: func(o *cliOptions) { o.language, o.operator = "en", "ct" }, message: "mainland-China specific"},
		{name: "CN global", mutate: func(o *cliOptions) { o.language, o.platform = "zh", "cn" }, message: "does not support -opt global"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			options := cliOptions{language: "zh", platform: "net", operator: "global", method: "speedtest", dnsMode: "auto", network: "auto", num: -1}
			if test.mutate != nil {
				test.mutate(&options)
			}
			_, err := normalizeAndValidateCLI(options, test.args)
			if err == nil || !strings.Contains(err.Error(), test.message) {
				t.Fatalf("error = %v, want message containing %q", err, test.message)
			}
		})
	}
}

func TestResolveCLITargetLanguageBehavior(t *testing.T) {
	tests := []struct {
		name    string
		options cliOptions
		mode    cliTargetMode
		url     string
	}{
		{name: "English default", options: cliOptions{language: "en", platform: "net", operator: "global"}, mode: targetRepresentativeGlobal},
		{name: "English nearby", options: cliOptions{language: "en", platform: "net", operator: "global", nearby: true}, mode: targetRepresentativeGlobal},
		{name: "Chinese nearby", options: cliOptions{language: "zh", platform: "net", operator: "global", nearby: true}, mode: targetAutomaticNearby},
		{name: "Chinese default", options: cliOptions{language: "zh", platform: "net", operator: "global"}, mode: targetCustom, url: model.NetGlobal},
		{name: "English explicit Japan", options: cliOptions{language: "en", platform: "net", operator: "jp"}, mode: targetCustom, url: model.NetJP},
		{name: "Chinese explicit mainland", options: cliOptions{language: "zh", platform: "cn", operator: "cmcc"}, mode: targetCustom, url: model.CnCMCC},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			target, err := resolveCLITarget(test.options)
			if err != nil {
				t.Fatal(err)
			}
			if target.mode != test.mode || target.url != test.url {
				t.Fatalf("target = %+v, want mode=%s url=%s", target, test.mode, test.url)
			}
		})
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
	if report.Availability != model.ServerAvailable || len(report.Selected) != 1 || report.Source != "" {
		t.Fatalf("unexpected CLI report: %+v", report)
	}
	if strings.Contains(output.String(), `"fallback"`) || strings.Contains(output.String(), `"source"`) {
		t.Fatalf("registry provenance leaked into structured output: %s", output.String())
	}
}

func TestWriteEnglishRegistryReportExcludesMainlandAndSpreadsRegions(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(`[
			{"id":"cn","name":"China","host":"cn.test:443","country":"China"},
			{"id":"jp","name":"Japan","host":"jp.test:443","country":"Japan"},
			{"id":"uk","name":"UK","host":"uk.test:443","country":"United Kingdom"},
			{"id":"us","name":"US","host":"us.test:443","country":"United States"},
			{"id":"au","name":"AU","host":"au.test:443","country":"Australia"}
		]`))
	}))
	defer server.Close()
	var output bytes.Buffer
	err := writeRegistryReportForLanguage(context.Background(), &output, server.Client(), []model.RegistrySource{{Name: "fixture", URL: server.URL}}, 3,
		func(context.Context, string, string) (net.Conn, error) {
			client, peer := net.Pipe()
			go peer.Close()
			return client, nil
		}, "en")
	if err != nil {
		t.Fatal(err)
	}
	var report model.RegistryReport
	if err := json.Unmarshal(output.Bytes(), &report); err != nil {
		t.Fatal(err)
	}
	if len(report.Servers) != 4 || len(report.Selected) != 3 {
		t.Fatalf("unexpected English registry sizes: %+v", report)
	}
	for _, server := range append(report.Servers, report.Selected...) {
		if model.IsMainlandChinaServer(server) || server.ID == "cn" {
			t.Fatalf("mainland server leaked into English report: %+v", server)
		}
	}
	if report.Selected[0].Country != "Japan" || report.Selected[1].Country != "United Kingdom" || report.Selected[2].Country != "United States" {
		t.Fatalf("selection is not geographically representative: %+v", report.Selected)
	}
}
