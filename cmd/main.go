package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"strings"
	"time"

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
	num         int
	registry    bool
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
	set.IntVar(&options.num, "num", -1, "Number of test servers, default -1 not to limit")
	set.BoolVar(&options.registry, "registry", false, "Load, probe, and select registry servers as JSON")
	return set
}

func writeRegistryReport(ctx context.Context, output io.Writer, client *http.Client, sources []model.RegistrySource, limit int, dial model.ServerDialFunc) error {
	if limit <= 0 {
		limit = 2
	}
	report := model.ResolveServerRegistry(ctx, client, sources, 1, limit, 2*time.Second, 8, dial)
	encoder := json.NewEncoder(output)
	encoder.SetIndent("", "  ")
	return encoder.Encode(report)
}

func main() {
	var options cliOptions
	speedtestFlag := newSpeedtestFlagSet(&options)
	if err := speedtestFlag.Parse(os.Args[1:]); err != nil {
		return
	}
	if options.registry {
		if err := writeRegistryReport(context.Background(), os.Stdout, nil, model.DefaultRegistrySources(), options.num, (model.ServerDialFunc)((&net.Dialer{}).DialContext)); err != nil {
			fmt.Fprintln(os.Stderr, err)
		}
		return
	}
	fmt.Println("项目地址:", Blue("https://github.com/oneclickvirt/speedtest"))
	if options.help {
		fmt.Printf("Usage: %s [options]\n", os.Args[0])
		speedtestFlag.PrintDefaults()
		return
	}
	if options.showVersion {
		fmt.Println(model.SpeedTestVersion)
		return
	}
	go func() {
		client := &http.Client{Timeout: 3 * time.Second}
		resp, err := client.Get("https://hits.spiritlhl.net/speedtest.svg?action=hit&title=Hits&title_bg=%23555555&count_bg=%230eecf8&edge_flat=false")
		if err == nil && resp != nil {
			resp.Body.Close()
		}
	}()
	if options.showHead {
		sp.ShowHead(options.language)
	}
	if options.nearby {
		if strings.ToLower(options.method) == "origin" {
			sp.NearbySpeedTest()
		} else if strings.ToLower(options.method) == "speedtest" {
			sp.OfficialNearbySpeedTest()
		}
		return
	}
	var url, parseType string
	if strings.ToLower(options.platform) == "cn" {
		if strings.ToLower(options.operator) == "cmcc" {
			url = model.CnCMCC
		} else if strings.ToLower(options.operator) == "cu" {
			url = model.CnCU
		} else if strings.ToLower(options.operator) == "ct" {
			url = model.CnCT
		} else if strings.ToLower(options.operator) == "hk" {
			url = model.CnHK
		} else if strings.ToLower(options.operator) == "tw" {
			url = model.CnTW
		} else if strings.ToLower(options.operator) == "jp" {
			url = model.CnJP
		} else if strings.ToLower(options.operator) == "sg" {
			url = model.CnSG
		}
		parseType = "url"
	} else if strings.ToLower(options.platform) == "net" {
		if strings.ToLower(options.operator) == "cmcc" {
			url = model.NetCMCC
		} else if strings.ToLower(options.operator) == "cu" {
			url = model.NetCU
		} else if strings.ToLower(options.operator) == "ct" {
			url = model.NetCT
		} else if strings.ToLower(options.operator) == "hk" {
			url = model.NetHK
		} else if strings.ToLower(options.operator) == "tw" {
			url = model.NetTW
		} else if strings.ToLower(options.operator) == "jp" {
			url = model.NetJP
		} else if strings.ToLower(options.operator) == "sg" {
			url = model.NetSG
		} else if strings.ToLower(options.operator) == "global" {
			url = model.NetGlobal
		}
		parseType = "id"
	}
	if strings.ToLower(options.method) == "origin" {
		if url != "" && parseType != "" {
			sp.CustomSpeedTest(url, parseType, options.num, options.language)
		} else {
			fmt.Println("-opt/-pf with wrong operator.")
		}
	} else if strings.ToLower(options.method) == "speedtest" {
		err := sp.OfficialAvailableTest()
		if err == nil {
			if url != "" && parseType != "" {
				sp.OfficialCustomSpeedTest(url, parseType, options.num, options.language)
			} else {
				fmt.Println("-opt/-pf with wrong operator.")
			}
		} else {
			fmt.Println("Can not match speedtest command, switch to use origin test")
			if url != "" && parseType != "" {
				sp.CustomSpeedTest(url, parseType, options.num, options.language)
			} else {
				fmt.Println("-opt/-pf with wrong operator.")
			}
		}
	} else {
		fmt.Println("-m with wrong operator.")
	}

}
