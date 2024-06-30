package sp

import (
	"encoding/csv"
	"fmt"
	"io"
	"os/exec"
	"sort"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/imroc/req/v3"
	. "github.com/oneclickvirt/defaultset"
	"github.com/oneclickvirt/speedtest/model"
	"github.com/showwin/speedtest-go/speedtest"
	"github.com/showwin/speedtest-go/speedtest/transport"
)

var speedtestClient = speedtest.New(speedtest.WithUserConfig(
	&speedtest.UserConfig{
		UserAgent:      "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/99.0.4844.74 Safari/537.36",
		PingMode:       speedtest.TCP,
		MaxConnections: 8,
	}))

func getData(endpoint string) string {
	client := req.C()
	client.SetTimeout(10 * time.Second)
	client.R().
		SetRetryCount(2).
		SetRetryBackoffInterval(1*time.Second, 5*time.Second).
		SetRetryFixedInterval(2 * time.Second)
	if model.EnableLoger {
		InitLogger()
		defer Logger.Sync()
	}
	for _, baseUrl := range model.CdnList {
		url := baseUrl + endpoint
		resp, err := client.R().Get(url)
		if err == nil {
			defer resp.Body.Close()
			b, err := io.ReadAll(resp.Body)
			if err == nil {
				return string(b)
			}
		}
		if model.EnableLoger {
			Logger.Info(err.Error())
		}
	}
	return ""
}

func parseDataFromURL(data, url string) speedtest.Servers {
	if model.EnableLoger {
		InitLogger()
		defer Logger.Sync()
	}
	var targets speedtest.Servers
	reader := csv.NewReader(strings.NewReader(data))
	reader.Comma = ','
	records, err := reader.ReadAll()
	if err == nil {
		if len(records) > 0 && (records[0][6] == "country_code" || records[0][1] == "country_code") {
			records = records[1:]
		}
		for _, record := range records {
			customURL := record[5]
			target, errFetch := speedtestClient.CustomServer(customURL)
			if errFetch != nil {
				if model.EnableLoger {
					Logger.Info(err.Error())
				}
				continue
			}
			target.Name = record[10] + record[7] + record[8]
			targets = append(targets, target)
		}
	}
	return targets
}

func parseDataFromID(data, url string) speedtest.Servers {
	if model.EnableLoger {
		InitLogger()
		defer Logger.Sync()
	}
	var targets speedtest.Servers
	reader := csv.NewReader(strings.NewReader(data))
	reader.Comma = ','
	records, err := reader.ReadAll()
	if err == nil {
		if len(records) > 0 && (records[0][6] == "country_code" || records[0][1] == "country_code") {
			records = records[1:]
		}
		for _, record := range records {
			id := record[0]
			serverPtr, errFetch := speedtestClient.FetchServerByID(id)
			if errFetch != nil {
				if model.EnableLoger {
					Logger.Info(err.Error())
				}
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

func OfficialAvailableTest() error {
	if model.EnableLoger {
		InitLogger()
		defer Logger.Sync()
	}
	spvCheck := exec.Command("speedtest", "--version")
	output, err := spvCheck.CombinedOutput()
	if err != nil {
		return err
	} else {
		version := strings.Split(string(output), "\n")[0]
		if strings.Contains(version, "Speedtest by Ookla") && !strings.Contains(version, "err") {
			// 此时确认可使用speedtest命令进行测速
			return nil
		}
	}
	return fmt.Errorf("No match speedtest command")
}

func OfficialNearbySpeedTest() {
	if model.EnableLoger {
		InitLogger()
		defer Logger.Sync()
	}
	var serverName, UPStr, DLStr, Latency, PacketLoss string // serverID,
	// speedtest --progress=no --accept-license --accept-gdpr
	sptCheck := exec.Command("speedtest", "--progress=no", "--accept-license", "--accept-gdpr")
	temp, err := sptCheck.CombinedOutput()
	if err == nil {
		serverName = "Speedtest.net"
		tempList := strings.Split(string(temp), "\n")
		for _, line := range tempList {
			if strings.Contains(line, "Idle Latency") {
				Latency = strings.TrimSpace(strings.Split(strings.Split(line, ":")[1], "(")[0])
			} else if strings.Contains(line, "Download") {
				DLStr = strings.TrimSpace(strings.Split(strings.Split(line, ":")[1], "(")[0])
			} else if strings.Contains(line, "Upload") {
				UPStr = strings.TrimSpace(strings.Split(strings.Split(line, ":")[1], "(")[0])
			} else if strings.Contains(line, "Packet Loss") {
				PacketLoss = strings.TrimSpace(strings.Split(line, ":")[1])
			}
		}
		if Latency != "" && DLStr != "" && UPStr != "" && PacketLoss != "" {
			fmt.Print(formatString(serverName, 16))
			fmt.Print(formatString(UPStr, 16))
			fmt.Print(formatString(DLStr, 16))
			fmt.Print(formatString(Latency, 16))
			fmt.Print(formatString(PacketLoss, 16))
			fmt.Println()
		}
	}
}

func OfficialCustomSpeedTest(url, byWhat string, num int) {
	if model.EnableLoger {
		InitLogger()
		defer Logger.Sync()
	}
	if !strings.Contains(url, ".net") {
		return
	}
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
			if model.EnableLoger {
				Logger.Info(err.Error())
			}
		}
		pingList = append(pingList, server.Latency)
		serverMap[server.Latency] = server
	}
	sort.Slice(pingList, func(i, j int) bool {
		return pingList[i] < pingList[j]
	})
	if num == -1 || num >= len(pingList) {
		num = len(pingList)
	} else if len(pingList) == 0 {
		fmt.Println("No match servers")
		if model.EnableLoger {
			Logger.Info("No match servers")
		}
		return
	}
	var serverName, UPStr, DLStr, Latency, PacketLoss string
	for i := 0; i < len(pingList); i++ {
		server := serverMap[pingList[i]]
		if i < num {
			// speedtest --progress=no --accept-license --accept-gdpr
			sptCheck := exec.Command("speedtest", "--progress=no", "--server-id="+server.ID, "--accept-license", "--accept-gdpr")
			temp, err := sptCheck.CombinedOutput()
			if err == nil {
				serverName = server.Name
				tempList := strings.Split(string(temp), "\n")
				for _, line := range tempList {
					if strings.Contains(line, "Idle Latency") {
						Latency = strings.TrimSpace(strings.Split(strings.Split(line, ":")[1], "(")[0])
					} else if strings.Contains(line, "Download") {
						DLStr = strings.TrimSpace(strings.Split(strings.Split(line, ":")[1], "(")[0])
					} else if strings.Contains(line, "Upload") {
						UPStr = strings.TrimSpace(strings.Split(strings.Split(line, ":")[1], "(")[0])
					} else if strings.Contains(line, "Packet Loss") {
						PacketLoss = strings.TrimSpace(strings.Split(line, ":")[1])
					}
				}
				if Latency != "" && DLStr != "" && UPStr != "" && PacketLoss != "" {
					fmt.Print(formatString(serverName, 16))
					fmt.Print(formatString(UPStr, 16))
					fmt.Print(formatString(DLStr, 16))
					fmt.Print(formatString(Latency, 16))
					fmt.Print(formatString(PacketLoss, 16))
					fmt.Println()
				}
			}
		}
	}
}

func NearbySpeedTest() {
	if model.EnableLoger {
		InitLogger()
		defer Logger.Sync()
	}
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
		if err == nil {
			fmt.Print(formatString("Speedtest.net", 16))
			fmt.Print(formatString(fmt.Sprintf("%-8s", fmt.Sprintf("%.2f", NearbyServer.ULSpeed.Mbps())+" Mbps"), 16))
			fmt.Print(formatString(fmt.Sprintf("%-8s", fmt.Sprintf("%.2f", NearbyServer.DLSpeed.Mbps())+" Mbps"), 16))
			fmt.Print(formatString(fmt.Sprintf("%s", NearbyServer.Latency), 16))
			fmt.Print(formatString(PacketLoss, 16))
			fmt.Println()
			NearbyServer.Context.Reset()
		} else if model.EnableLoger {
			Logger.Info(err.Error())
		}
	}
}

func CustomSpeedTest(url, byWhat string, num int) {
	if model.EnableLoger {
		InitLogger()
		defer Logger.Sync()
	}
	data := getData(url)
	var targets speedtest.Servers
	if byWhat == "id" {
		targets = parseDataFromID(data, url)
	} else if byWhat == "url" {
		targets = parseDataFromURL(data, url)
	}
	var pingList []time.Duration
	var err, err1, err2, err3 error
	serverMap := make(map[time.Duration]*speedtest.Server)
	for _, server := range targets {
		err = server.PingTest(nil)
		if err != nil {
			server.Latency = 1000 * time.Millisecond
			if model.EnableLoger {
				Logger.Info(err.Error())
			}
		}
		pingList = append(pingList, server.Latency)
		serverMap[server.Latency] = server
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
		if model.EnableLoger {
			Logger.Info("No match servers")
		}
		return
	}
	for i := 0; i < len(pingList); i++ {
		server := serverMap[pingList[i]]
		if i < num {
			err1 = server.DownloadTest()
			err2 = server.UploadTest()
			err3 = analyzer.Run(server.Host, func(packetLoss *transport.PLoss) {
				PacketLoss = strings.ReplaceAll(packetLoss.String(), "Packet Loss: ", "")
			})
			if err3 != nil {
				if model.EnableLoger {
					Logger.Info(server.ID)
					Logger.Info(err3.Error())
				}
				PacketLoss = "N/A"
			}
			if err1 != nil {
				if model.EnableLoger {
					Logger.Info(server.ID)
					Logger.Info(err1.Error())
				}
				server.Context.Reset()
				continue
			}
			if err2 != nil {
				if model.EnableLoger {
					Logger.Info(server.ID)
					Logger.Info(err2.Error())
				}
				server.Context.Reset()
				continue
			}
			fmt.Print(formatString(server.Name, 16))
			fmt.Print(formatString(fmt.Sprintf("%-8s", fmt.Sprintf("%.2f", server.ULSpeed.Mbps())+" Mbps"), 16))
			fmt.Print(formatString(fmt.Sprintf("%-8s", fmt.Sprintf("%.2f", server.DLSpeed.Mbps())+" Mbps"), 16))
			fmt.Print(formatString(fmt.Sprintf("%s", server.Latency), 16))
			fmt.Print(formatString(PacketLoss, 16))
			fmt.Println()
		}
		server.Context.Reset()
	}
}
