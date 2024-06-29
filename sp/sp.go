package sp

import (
	"encoding/csv"
	"fmt"
	"io"
	"log"
	"sort"
	"strings"
	"time"

	"github.com/imroc/req/v3"
	"github.com/oneclickvirt/speedtest/model"
	"github.com/showwin/speedtest-go/speedtest"
	"github.com/showwin/speedtest-go/speedtest/transport"
)

func checkError(err error) {
	if err != nil {
		log.Fatal(err)
	}
}

func NearbySpeedTest(language string) {
	if language == "zh" {
		fmt.Printf("%-12s\t %-11s\t %-11s\t %-11s\t %-12s\n",
			"位置", "上传速度", "下载速度", "延迟", "丢包率")
	} else if language == "en" {
		fmt.Printf("%-12s\t %-11s\t %-11s\t %-11s\t %-12s\n",
			"Location", "Upload Speed", "Download Speed", "Latency", "PacketLoss")
	}
	var speedtestClient = speedtest.New()
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
		fmt.Printf("%-12s\t %-11s\t %-11s\t %-11s\t %-12s\n",
			//NearbyServer.Name,
			"Speedtest.net",
			fmt.Sprintf("%.2f Mbps", NearbyServer.ULSpeed.Mbps()),
			fmt.Sprintf("%.2f Mbps", NearbyServer.DLSpeed.Mbps()),
			NearbyServer.Latency,
			PacketLoss)
		NearbyServer.Context.Reset()
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

func parseData(data string) speedtest.Servers {
	var targets speedtest.Servers
	reader := csv.NewReader(strings.NewReader(data))
	reader.Comma = ','
	records, err := reader.ReadAll()
	if err != nil {
		log.Fatal(err)
	}
	speedtestClient := speedtest.New()
	for _, record := range records {
		customURL := record[5] + ":" + record[6]
		target, errFetch := speedtestClient.CustomServer(customURL)
		if errFetch != nil {
			continue
		}
		target.Name = record[3]
		targets = append(targets, target)
	}
	return targets
}

func CustomSpeedTest(url string, num int) {
	data := getData(url)
	targets := parseData(data)
	var pingList []time.Duration
	var err error
	serverMap := make(map[time.Duration]*speedtest.Server)
	for _, server := range targets {
		err = server.PingTest(nil)
		checkError(err)
		pingList = append(pingList, server.Latency)
		serverMap[server.Latency] = server
		server.Context.Reset()
	}
	sort.Slice(pingList, func(i, j int) bool {
		return pingList[i] < pingList[j]
	})
	analyzer := speedtest.NewPacketLossAnalyzer(nil)
	var PacketLoss string
	for i := 0; i < num && i < len(pingList); i++ {
		server := serverMap[pingList[i]]
		server.DownloadTest()
		server.UploadTest()
		err = analyzer.Run(server.Host, func(packetLoss *transport.PLoss) {
			PacketLoss = strings.ReplaceAll(packetLoss.String(), "Packet Loss: ", "")
		})
		checkError(err)
		fmt.Printf("%-12s\t %-11s\t %-11s\t %-11s\t %-12s\n",
			server.Name,
			fmt.Sprintf("%.2f Mbps", server.ULSpeed.Mbps()),
			fmt.Sprintf("%.2f Mbps", server.DLSpeed.Mbps()),
			server.Latency,
			PacketLoss)
		server.Context.Reset()
	}
}
