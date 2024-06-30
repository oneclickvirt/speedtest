package main

import (
	"flag"
	"fmt"
	"net/http"
	"strings"

	. "github.com/oneclickvirt/defaultset"
	"github.com/oneclickvirt/speedtest/model"
	"github.com/oneclickvirt/speedtest/sp"
)

func main() {
	go func() {
		http.Get("https://hits.seeyoufarm.com/api/count/incr/badge.svg?url=https%3A%2F%2Fgithub.com%2Foneclickvirt%2Fspeedtest&count_bg=%2323E01C&title_bg=%23555555&icon=sonarcloud.svg&icon_color=%23E7E7E7&title=hits&edge_flat=false")
	}()
	fmt.Println("项目地址:", Blue("https://github.com/oneclickvirt/speedtest"))
	var showVersion, nearByServer, showHead bool
	var language, operator, platform string
	var num int
	flag.BoolVar(&showVersion, "v", false, "Show version information")
	flag.BoolVar(&nearByServer, "nearby", false, "Test only nearby servers")
	flag.BoolVar(&showHead, "s", true, "Show head")
	flag.BoolVar(&model.EnableLoger, "e", false, "Enable logging")
	flag.StringVar(&language, "l", "zh", "Language parameter (options: en, zh)")
	flag.StringVar(&platform, "pf", "net", "Platform parameter (options: net, cn)")
	flag.StringVar(&operator, "opt", "global", "Operator parameter (options: cmcc, cu, ct, sg, tw, jp, hk, global)")
	flag.IntVar(&num, "num", -1, "Number of test servers, default -1 not to limit")
	flag.Parse()
	if showVersion {
		fmt.Println(model.SpeedTestVersion)
		return
	}
	if showHead {
		sp.ShowHead(language)
	}
	if nearByServer {
		sp.NearbySpeedTest()
		return
	}
	var url, parseType string
	if strings.ToLower(platform) == "cn" {
		if strings.ToLower(operator) == "cmcc" {
			url = model.CnCMCC
		} else if strings.ToLower(operator) == "cu" {
			url = model.CnCU
		} else if strings.ToLower(operator) == "ct" {
			url = model.CnCT
		} else if strings.ToLower(operator) == "hk" {
			url = model.CnHK
		} else if strings.ToLower(operator) == "tw" {
			url = model.CnTW
		} else if strings.ToLower(operator) == "jp" {
			url = model.CnJP
		} else if strings.ToLower(operator) == "sg" {
			url = model.CnSG
		}
		parseType = "url"
	} else if strings.ToLower(platform) == "net" {
		if strings.ToLower(operator) == "cmcc" {
			url = model.NetCMCC
		} else if strings.ToLower(operator) == "cu" {
			url = model.NetCU
		} else if strings.ToLower(operator) == "ct" {
			url = model.NetCT
		} else if strings.ToLower(operator) == "hk" {
			url = model.NetHK
		} else if strings.ToLower(operator) == "tw" {
			url = model.NetTW
		} else if strings.ToLower(operator) == "jp" {
			url = model.NetJP
		} else if strings.ToLower(operator) == "sg" {
			url = model.NetSG
		} else if strings.ToLower(operator) == "global" {
			url = model.NetGlobal
		}
		parseType = "id"
	}
	if url != "" && parseType != "" {
		sp.CustomSpeedTest(url, parseType, num)
	} else {
		fmt.Println("Wrong operator.")
	}
}
