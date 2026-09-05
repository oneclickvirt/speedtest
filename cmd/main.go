package main

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"net/http"
	"os"
	"strings"
	"time"

	"github.com/oneclickvirt/basics/network/resolver"
	. "github.com/oneclickvirt/defaultset"
	"github.com/oneclickvirt/speedtest/model"
	"github.com/oneclickvirt/speedtest/sp"
)

type cliOptions struct {
	showVersion bool
	nearby      bool
	showHead    bool
	help        bool
	language    string
	operator    string
	platform    string
	method      string
	dnsMode     string
	network     string
	num         int
	registry    bool
}

type cliTargetMode string

const (
	targetCustom               cliTargetMode = "custom"
	targetAutomaticNearby      cliTargetMode = "automatic-nearby"
	targetRepresentativeGlobal cliTargetMode = "representative-global"
)

type cliTarget struct {
	mode      cliTargetMode
	url       string
	parseType string
}

func newSpeedtestFlagSet(options *cliOptions) *flag.FlagSet {
	set := flag.NewFlagSet("speedtest", flag.ContinueOnError)
	set.BoolVar(&options.help, "h", false, "Show help information")
	set.BoolVar(&options.showVersion, "v", false, "Show version information")
	set.BoolVar(&options.nearby, "nearby", false, "Test only nearby servers")
	set.BoolVar(&options.showHead, "s", true, "Show head")
	set.BoolVar(&model.EnableLoger, "e", false, "Enable logging")
	set.StringVar(&options.language, "l", "zh", "Language parameter (options: en, zh)")
	set.StringVar(&options.platform, "pf", "net", "Platform parameter (options: net, cn)")
	set.StringVar(&options.operator, "opt", "global", "Operator parameter (options: cmcc, cu, ct, sg, tw, jp, hk, global)")
	set.StringVar(&options.method, "m", "speedtest", "Test Method parameter (options: origin, speedtest, speedtest-go)")
	set.StringVar(&options.dnsMode, "dns-mode", "auto", "DNS mode (auto, system, doh, or dot)")
	set.StringVar(&options.network, "ip-version", "auto", "Request IP family (auto, 4/ipv4, or 6/ipv6)")
	set.IntVar(&options.num, "num", -1, "Number of test servers, default -1 not to limit")
	set.BoolVar(&options.registry, "registry", false, "Load, probe, and select registry servers as JSON")
	return set
}

func writeRegistryReport(ctx context.Context, output io.Writer, client *http.Client, sources []model.RegistrySource, limit int, dial model.ServerDialFunc) error {
	return writeRegistryReportForLanguageWithNetwork(ctx, output, client, sources, limit, dial, "", "")
}

func writeRegistryReportForLanguage(ctx context.Context, output io.Writer, client *http.Client, sources []model.RegistrySource, limit int, dial model.ServerDialFunc, language string) error {
	return writeRegistryReportForLanguageWithNetwork(ctx, output, client, sources, limit, dial, language, "")
}

func writeRegistryReportForLanguageWithNetwork(ctx context.Context, output io.Writer, client *http.Client, sources []model.RegistrySource, limit int, dial model.ServerDialFunc, language, networkValue string) error {
	if limit <= 0 {
		limit = 2
	}
	network, _ := model.NormalizeNetwork(networkValue)
	report := model.ResolveServerRegistryForLanguageWithNetwork(ctx, client, sources, 1, limit, 2*time.Second, 8, dial, language, network)
	encoder := json.NewEncoder(output)
	encoder.SetIndent("", "  ")
	return encoder.Encode(report)
}

func normalizeAndValidateCLI(options cliOptions, positional []string) (cliOptions, error) {
	options.language = strings.ToLower(strings.TrimSpace(options.language))
	options.platform = strings.ToLower(strings.TrimSpace(options.platform))
	options.operator = strings.ToLower(strings.TrimSpace(options.operator))
	options.method = strings.ToLower(strings.TrimSpace(options.method))
	options.dnsMode = strings.ToLower(strings.TrimSpace(options.dnsMode))
	options.network = strings.ToLower(strings.TrimSpace(options.network))

	if len(positional) > 0 {
		return options, fmt.Errorf("unexpected positional arguments: %s", strings.Join(positional, " "))
	}
	if options.language != "en" && options.language != "zh" {
		return options, fmt.Errorf("invalid -l %q: supported values are en and zh", options.language)
	}
	if options.platform != "net" && options.platform != "cn" {
		return options, fmt.Errorf("invalid -pf %q: supported values are net and cn", options.platform)
	}
	validOperators := map[string]bool{"cmcc": true, "cu": true, "ct": true, "sg": true, "tw": true, "jp": true, "hk": true, "global": true}
	if !validOperators[options.operator] {
		return options, fmt.Errorf("invalid -opt %q: supported values are cmcc, cu, ct, sg, tw, jp, hk, and global", options.operator)
	}
	if options.method != "origin" && options.method != "speedtest" && options.method != "speedtest-go" {
		return options, fmt.Errorf("invalid -m %q: supported values are origin, speedtest, and speedtest-go", options.method)
	}
	if options.dnsMode != "auto" && options.dnsMode != "system" && options.dnsMode != "doh" && options.dnsMode != "dot" {
		return options, fmt.Errorf("invalid -dns-mode %q: supported values are auto, system, doh, and dot", options.dnsMode)
	}
	if _, err := model.NormalizeNetwork(options.network); err != nil {
		return options, fmt.Errorf("invalid -ip-version %q: use auto, 4/ipv4, or 6/ipv6", options.network)
	}
	if options.num == 0 || options.num < -1 {
		return options, fmt.Errorf("invalid -num %d: use -1 or a positive number", options.num)
	}
	if options.registry && options.nearby {
		return options, fmt.Errorf("-registry and -nearby cannot be used together")
	}
	if options.platform == "cn" && options.operator == "global" && !options.nearby && !options.registry {
		return options, fmt.Errorf("invalid combination: -pf cn does not support -opt global")
	}
	if options.language == "en" {
		if options.platform == "cn" {
			return options, fmt.Errorf("invalid English selection: -pf cn is mainland-China specific; use -pf net")
		}
		switch options.operator {
		case "cmcc", "cu", "ct":
			return options, fmt.Errorf("invalid English selection: -opt %s is mainland-China specific; choose global, sg, tw, jp, or hk", options.operator)
		}
	}
	return options, nil
}

func resolveCLITarget(options cliOptions) (cliTarget, error) {
	if options.nearby {
		if options.language == "en" {
			return cliTarget{mode: targetRepresentativeGlobal}, nil
		}
		return cliTarget{mode: targetAutomaticNearby}, nil
	}
	if options.language == "en" && options.platform == "net" && options.operator == "global" {
		return cliTarget{mode: targetRepresentativeGlobal}, nil
	}

	target := cliTarget{mode: targetCustom}
	if options.platform == "cn" {
		target.parseType = "url"
		switch options.operator {
		case "cmcc":
			target.url = model.CnCMCC
		case "cu":
			target.url = model.CnCU
		case "ct":
			target.url = model.CnCT
		case "hk":
			target.url = model.CnHK
		case "tw":
			target.url = model.CnTW
		case "jp":
			target.url = model.CnJP
		case "sg":
			target.url = model.CnSG
		}
	} else {
		target.parseType = "id"
		switch options.operator {
		case "cmcc":
			target.url = model.NetCMCC
		case "cu":
			target.url = model.NetCU
		case "ct":
			target.url = model.NetCT
		case "hk":
			target.url = model.NetHK
		case "tw":
			target.url = model.NetTW
		case "jp":
			target.url = model.NetJP
		case "sg":
			target.url = model.NetSG
		case "global":
			target.url = model.NetGlobal
		}
	}
	if target.url == "" || target.parseType == "" {
		return cliTarget{}, fmt.Errorf("unsupported -pf %s and -opt %s combination", options.platform, options.operator)
	}
	return target, nil
}

func runRepresentativeGlobal(options cliOptions) error {
	limit := options.num
	if limit <= 0 {
		limit = 2
	}
	network, _ := model.NormalizeNetwork(options.network)
	report := model.ResolveServerRegistryForLanguageWithNetwork(context.Background(), nil, model.DefaultRegistrySources(), 1, limit, 2*time.Second, 8, nil, options.language, network)
	if report.Availability != model.ServerAvailable || len(report.Selected) == 0 {
		if report.Error == "" {
			report.Error = "no representative global speedtest servers are available"
		}
		return errors.New(report.Error)
	}
	if options.method == "speedtest" {
		if err := sp.OfficialAvailableTest(); err == nil {
			sp.OfficialRegistrySpeedTestWithNetwork(report.Selected, options.language, options.network)
			return nil
		}
		fmt.Println("Can not match speedtest command, switch to use origin test")
	}
	sp.RegistrySpeedTestWithNetwork(report.Selected, options.language, options.network)
	return nil
}

func main() {
	var options cliOptions
	speedtestFlag := newSpeedtestFlagSet(&options)
	if err := speedtestFlag.Parse(os.Args[1:]); err != nil {
		os.Exit(2)
	}
	if options.help {
		fmt.Println("项目地址:", Blue("https://github.com/oneclickvirt/speedtest"))
		fmt.Printf("Usage: %s [options]\n", os.Args[0])
		speedtestFlag.PrintDefaults()
		return
	}
	if options.showVersion {
		fmt.Println("项目地址:", Blue("https://github.com/oneclickvirt/speedtest"))
		fmt.Println(model.SpeedTestVersion)
		return
	}
	var err error
	options, err = normalizeAndValidateCLI(options, speedtestFlag.Args())
	if err != nil {
		fmt.Fprintln(os.Stderr, "parameter error:", err)
		os.Exit(2)
	}
	dnsStatus := resolver.Configure(context.Background(), resolver.Config{Mode: resolver.ParseMode(options.dnsMode)})
	if dnsStatus.Active == resolver.ModeDoH || dnsStatus.Active == resolver.ModeDoT {
		defer resolver.Shutdown()
	}
	if options.registry {
		if err := writeRegistryReportForLanguageWithNetwork(context.Background(), os.Stdout, nil, model.DefaultRegistrySources(), options.num, nil, options.language, options.network); err != nil {
			fmt.Fprintln(os.Stderr, err)
			os.Exit(1)
		}
		return
	}
	fmt.Println("项目地址:", Blue("https://github.com/oneclickvirt/speedtest"))
	target, err := resolveCLITarget(options)
	if err != nil {
		fmt.Fprintln(os.Stderr, "parameter error:", err)
		os.Exit(2)
	}
	go func() {
		network, _ := model.NormalizeNetwork(options.network)
		client := model.NewHTTPClient(network, 3*time.Second)
		resp, err := client.Get("https://hits.spiritlhl.net/speedtest.svg?action=hit&title=Hits&title_bg=%23555555&count_bg=%230eecf8&edge_flat=false")
		if err == nil && resp != nil {
			resp.Body.Close()
		}
	}()
	if options.showHead {
		sp.ShowHead(options.language)
	}
	if target.mode == targetAutomaticNearby {
		if options.method == "origin" || options.method == "speedtest-go" {
			sp.NearbySpeedTestWithNetwork(options.network)
		} else {
			sp.OfficialNearbySpeedTestWithNetwork(options.network)
		}
		return
	}
	if target.mode == targetRepresentativeGlobal {
		if err := runRepresentativeGlobal(options); err != nil {
			fmt.Fprintln(os.Stderr, "speedtest unavailable:", err)
		}
		return
	}
	if options.method == "origin" || options.method == "speedtest-go" {
		sp.CustomSpeedTestWithNetwork(target.url, target.parseType, options.num, options.language, options.network)
	} else {
		err := sp.OfficialAvailableTest()
		if err == nil {
			sp.OfficialCustomSpeedTestWithNetwork(target.url, target.parseType, options.num, options.language, options.network)
		} else {
			fmt.Println("Can not match speedtest command, switch to use origin test")
			sp.CustomSpeedTestWithNetwork(target.url, target.parseType, options.num, options.language, options.network)
		}
	}
}
