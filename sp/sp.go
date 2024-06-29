package sp

import (
	"encoding/csv"
	"fmt"
	"io"
	"log"
	"sort"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/imroc/req/v3"
	"github.com/oneclickvirt/speedtest/model"
	"github.com/showwin/speedtest-go/speedtest"
	"github.com/showwin/speedtest-go/speedtest/transport"
)

var speedtestClient = speedtest.New(speedtest.WithUserConfig(
	&speedtest.UserConfig{
		UserAgent:      "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/99.0.4844.74 Safari/537.36",
		PingMode:       speedtest.TCP,
		MaxConnections: 1,
	}))

func checkError(err error) {
	if err != nil {
		log.Fatal(err)
	}
}

func getData(endpoint string) string {
	client := req.C()
	client.SetTimeout(10 * time.Second)
	client.R().
		SetRetryCount(2).
		SetRetryBackoffInterval(1*time.Second, 5*time.Second).
		SetRetryFixedInterval(2 * time.Second)
	for _, baseUrl := range model.CdnList {
		url := baseUrl + endpoint
		resp, err := client.R().Get(url)
		if err == nil {
			defer resp.Body.Close()
			b, err := io.ReadAll(resp.Body)
			if err == nil {
				return string(b)
			}
		} else {
			log.Println("Error accessing URL:", url, err)
		}
	}
	return ""
}

func parseDataFromURL(data, url string) speedtest.Servers {
	var targets speedtest.Servers
	reader := csv.NewReader(strings.NewReader(data))
	reader.Comma = ','
	records, err := reader.ReadAll()
	if err != nil {
		log.Fatal(err)
	}
	for _, record := range records {
		customURL := record[5]
		target, errFetch := speedtestClient.CustomServer(customURL)
		if errFetch != nil {
			continue
		}
		target.Name = record[10] + record[7] + record[8]
		targets = append(targets, target)
	}
	return targets
}

func parseDataFromID(data, url string) speedtest.Servers {
	var targets speedtest.Servers
	reader := csv.NewReader(strings.NewReader(data))
	reader.Comma = ','
	records, err := reader.ReadAll()
	if err != nil {
		log.Fatal(err)
	}
	for _, record := range records {
		id := record[0]
		serverPtr, errFetch := speedtestClient.FetchServerByID(id)
		if errFetch != nil {
			continue
		}
		if strings.Contains(url, "Mobile") {
			serverPtr.Name = "移动" + record[3]
		} else if strings.Contains(url, "Telecom") {
			serverPtr.Name = "电信" + record[3]
		} else if strings.Contains(url, "Unicom") {
			serverPtr.Name = "联通" + record[3]
		} else {
			serverPtr.Name = record[3]
		}
		targets = append(targets, serverPtr)
	}
	return targets
}

// 计算字符串的显示宽度（考虑中文字符）
func displayWidth(s string) int {
	width := 0
	for _, r := range s {
		if utf8.RuneLen(r) == 3 {
			// 假设每个中文字符宽度为2
			width += 2
		} else {
			width += 1
		}
	}
	return width
}

// 格式化字符串以确保左对齐
func formatString(s string, width int) string {
	displayW := displayWidth(s)
	if displayW < width {
		// 计算需要填充的空格数
		padding := width - displayW
		return s + fmt.Sprintf("%*s", padding, "")
	}
	return s
}

func ShowHead(language string) {
	headers1 := []string{"位置", "上传速度", "下载速度", "延迟", "丢包率"}
	headers2 := []string{"Location", "Upload Speed", "Download Speed", "Latency", "PacketLoss"}
	if language == "zh" {
		for _, header := range headers1 {
			fmt.Print(formatString(header, 16))
		}
		fmt.Println()
	} else if language == "en" {
		for _, header := range headers2 {
			fmt.Print(formatString(header, 16))
		}
		fmt.Println()
	}
}

func NearbySpeedTest() {
	serverList, _ := speedtestClient.FetchServers()
	targets, _ := serverList.FindServer([]int{})
	analyzer := speedtest.NewPacketLossAnalyzer(nil)
	var LowestLatency time.Duration
	var NearbyServer *speedtest.Server
	var PacketLoss string
	for _, server := range targets {
		server.PingTest(nil)
		if LowestLatency == 0 && NearbyServer == nil {
			LowestLatency = server.Latency
			NearbyServer = server
		} else if server.Latency < LowestLatency && NearbyServer != nil {
			NearbyServer = server
		}
		server.Context.Reset()
	}
	if NearbyServer != nil {
		NearbyServer.DownloadTest()
		NearbyServer.UploadTest()
		err := analyzer.Run(NearbyServer.Host, func(packetLoss *transport.PLoss) {
			PacketLoss = strings.ReplaceAll(packetLoss.String(), "Packet Loss: ", "")
		})
		checkError(err)
		fmt.Print(formatString("Speedtest.net", 16))
		fmt.Print(formatString(fmt.Sprintf("%-8s", fmt.Sprintf("%.2f", NearbyServer.ULSpeed.Mbps())+" Mbps"), 16))
		fmt.Print(formatString(fmt.Sprintf("%-8s", fmt.Sprintf("%.2f", NearbyServer.DLSpeed.Mbps())+" Mbps"), 16))
		fmt.Print(formatString(fmt.Sprintf("%s", NearbyServer.Latency), 16))
		fmt.Print(formatString(PacketLoss, 16))
		fmt.Println()
		NearbyServer.Context.Reset()
	}
}

func CustomSpeedTest(url, byWhat string, num int) {
	data := getData(url)
	var targets speedtest.Servers
	if byWhat == "id" {
		targets = parseDataFromID(data, url)
	} else if byWhat == "url" {
		targets = parseDataFromURL(data, url)
	}
	var pingList []time.Duration
	var err error
	serverMap := make(map[time.Duration]*speedtest.Server)
	for _, server := range targets {
		err = server.PingTest(nil)
		if err != nil {
			server.Latency = 1000 * time.Millisecond
		}
		pingList = append(pingList, server.Latency)
		serverMap[server.Latency] = server
		server.Context.Reset()
	}
	sort.Slice(pingList, func(i, j int) bool {
		return pingList[i] < pingList[j]
	})
	analyzer := speedtest.NewPacketLossAnalyzer(nil)
	var PacketLoss string
	if num == -1 || num >= len(pingList) {
		num = len(pingList)
	} else if len(pingList) == 0 {
		fmt.Println("No match servers")
		return
	}
	for i := 0; i < num && i < len(pingList); i++ {
		server := serverMap[pingList[i]]
		server.DownloadTest()
		server.UploadTest()
		err = analyzer.Run(server.Host, func(packetLoss *transport.PLoss) {
			PacketLoss = strings.ReplaceAll(packetLoss.String(), "Packet Loss: ", "")
		})
		if err != nil {
			PacketLoss = "N/A"
		}
		fmt.Print(formatString(server.Name, 16))
		fmt.Print(formatString(fmt.Sprintf("%-8s", fmt.Sprintf("%.2f", server.ULSpeed.Mbps())+" Mbps"), 16))
		fmt.Print(formatString(fmt.Sprintf("%-8s", fmt.Sprintf("%.2f", server.DLSpeed.Mbps())+" Mbps"), 16))
		fmt.Print(formatString(fmt.Sprintf("%s", server.Latency), 16))
		fmt.Print(formatString(PacketLoss, 16))
		fmt.Println()
		server.Context.Reset()
	}
}
