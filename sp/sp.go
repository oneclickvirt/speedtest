package sp

import (
	"fmt"
	"os/exec"
	"sort"
	"strings"
	"time"

	"github.com/oneclickvirt/speedtest/model"
	"github.com/showwin/speedtest-go/speedtest"
	"github.com/showwin/speedtest-go/speedtest/transport"
)

// 检查sudo是否可用
func isSudoAvailable() bool {
	cmd := exec.Command("sudo", "-n", "true")
	err := cmd.Run()
	return err == nil
}

// 如果sudo可用，则使用sudo执行命令
func execCommand(name string, arg ...string) *exec.Cmd {
	if hasRootPermission() {
		if isSudoAvailable() {
			return exec.Command("sudo", append([]string{name}, arg...)...)
		}
	}
	return exec.Command(name, arg...)
}

func OfficialAvailableTest() error {
	if model.EnableLoger {
		InitLogger()
		defer Logger.Sync()
	}
	// 首先检查 speedtest 命令是否存在
	_, err := exec.LookPath("speedtest")
	if err != nil {
		if model.EnableLoger {
			Logger.Info("Speedtest command not found: " + err.Error())
		}
		return fmt.Errorf("Speedtest command not found")
	}
	// 再进行版本检测
	spvCheck := execCommand("speedtest", "--version")
	output, err := spvCheck.CombinedOutput()
	if err != nil {
		if model.EnableLoger {
			Logger.Info(err.Error())
		}
		return err
	} else {
		version := strings.Split(string(output), "\n")[0]
		if strings.Contains(version, "Speedtest by Ookla") &&
			!strings.Contains(string(output), "not valid") &&
			!strings.Contains(string(output), "err") &&
			!strings.Contains(string(output), "Kommando nicht gefunden") {
			// 此时确认可使用speedtest命令进行测速
			return nil
		}
		if model.EnableLoger {
			Logger.Info(string(output))
		}
	}
	return fmt.Errorf("No match speedtest command")
}

func OfficialNearbySpeedTest() {
	if model.EnableLoger {
		InitLogger()
		defer Logger.Sync()
	}
	var UPStr, DLStr, Latency, PacketLoss string // serverID,
	// speedtest --progress=no --accept-license --accept-gdpr
	sptCheck := execCommand("speedtest", "--progress=no", "--accept-license", "--accept-gdpr")
	temp, err := sptCheck.CombinedOutput()
	if err == nil {
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
			fmt.Print(" " + formatString("Speedtest.net", 16))
			fmt.Print(formatString(UPStr, 16))
			fmt.Print(formatString(DLStr, 16))
			fmt.Print(formatString(Latency, 16))
			fmt.Print(formatString(PacketLoss, 16))
			fmt.Println()
		}
	}
}

func OfficialCustomSpeedTest(url, byWhat string, num int, language string) {
	if model.EnableLoger {
		InitLogger()
		defer Logger.Sync()
	}
	if !strings.Contains(url, ".net") {
		fmt.Println("Official speedtest only use .net platform, can not use other platforms.")
		return
	}
	data := getData(url)
	var targets speedtest.Servers
	if byWhat == "id" {
		targets = parseDataFromID(data, url)
	} else if byWhat == "url" {
		targets = parseDataFromURL(data, url)
	}
	officialTargetsSpeedTest(targets, num, language)
}

// OfficialRegistrySpeedTest runs the official client only against the
// prefiltered registry selection supplied by the caller.
func OfficialRegistrySpeedTest(servers []model.ServerMetadata, language string) {
	if model.EnableLoger {
		InitLogger()
		defer Logger.Sync()
	}
	targets := make(speedtest.Servers, 0, len(servers))
	for _, metadata := range servers {
		serverID := strings.TrimPrefix(strings.TrimSpace(metadata.ID), "global-")
		if serverID == "" {
			continue
		}
		server, err := speedtestClient.FetchServerByID(serverID)
		if err != nil || server == nil {
			if model.EnableLoger && err != nil {
				Logger.Info(err.Error())
			}
			continue
		}
		server.ID = serverID
		server.Name = registryServerLabel(metadata)
		targets = append(targets, server)
	}
	officialTargetsSpeedTest(targets, len(targets), language)
}

func officialTargetsSpeedTest(targets speedtest.Servers, num int, language string) {
	type pingedServer struct {
		server *speedtest.Server
	}
	pinged := make([]pingedServer, 0, len(targets))
	for _, server := range targets {
		if server == nil {
			continue
		}
		if err := server.PingTest(nil); err != nil {
			server.Latency = 1000 * time.Millisecond
			if model.EnableLoger {
				Logger.Info(err.Error())
			}
		}
		pinged = append(pinged, pingedServer{server: server})
	}
	sort.SliceStable(pinged, func(i, j int) bool {
		if pinged[i].server.Latency == pinged[j].server.Latency {
			return pinged[i].server.ID < pinged[j].server.ID
		}
		return pinged[i].server.Latency < pinged[j].server.Latency
	})
	if len(pinged) == 0 {
		fmt.Println("No match servers")
		if model.EnableLoger {
			Logger.Info("No match servers")
		}
		return
	}
	if num == -1 || num >= len(pinged) {
		num = len(pinged)
	}
	for i := 0; i < len(pinged); i++ {
		server := pinged[i].server
		if i < num {
			var serverName, UPStr, DLStr, Latency, PacketLoss string
			// speedtest --progress=no --accept-license --accept-gdpr
			sptCheck := execCommand("speedtest", "--progress=no", "--server-id="+server.ID, "--accept-license", "--accept-gdpr")
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
					if language == "zh" {
						fmt.Print(" " + formatString(serverName, 16))
					} else if language == "en" {
						name := serverName
						name = strings.ReplaceAll(name, "中国香港", "HongKong")
						name = strings.ReplaceAll(name, "洛杉矶", "LosAngeles")
						name = strings.ReplaceAll(name, "日本东京", "Tokyo,Japan")
						name = strings.ReplaceAll(name, "新加坡", "Singapore")
						name = strings.ReplaceAll(name, "法兰克福", "Frankfurt")
						fmt.Print(" " + formatString(name, 16))
					}
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

func registryServerLabel(server model.ServerMetadata) string {
	city := strings.TrimSpace(server.City)
	country := strings.TrimSpace(server.Country)
	if city != "" && country != "" {
		return city + "," + country
	}
	if name := strings.TrimSpace(server.Name); name != "" {
		return name
	}
	if country != "" {
		return country
	}
	return strings.TrimSpace(server.ID)
}

func NearbySpeedTest() {
	if model.EnableLoger {
		InitLogger()
		defer Logger.Sync()
	}
	serverList, err := speedtestClient.FetchServers()
	if err != nil || serverList == nil {
		if model.EnableLoger && err != nil {
			Logger.Info(err.Error())
		}
		return
	}
	targets, err := serverList.FindServer([]int{})
	if err != nil {
		if model.EnableLoger {
			Logger.Info(err.Error())
		}
		return
	}
	analyzer := speedtestClient.NewPacketLossAnalyzer()
	var LowestLatency time.Duration
	var NearbyServer *speedtest.Server
	var PacketLoss string
	for _, server := range targets {
		if server == nil {
			continue
		}
		if err := server.PingTest(nil); err != nil {
			if model.EnableLoger {
				Logger.Info(err.Error())
			}
			continue
		}
		if LowestLatency == 0 && NearbyServer == nil {
			LowestLatency = server.Latency
			NearbyServer = server
		} else if server.Latency < LowestLatency && NearbyServer != nil {
			LowestLatency = server.Latency
			NearbyServer = server
		}
		if server.Context != nil {
			server.Context.Reset()
		}
	}
	if NearbyServer != nil {
		err = NearbyServer.DownloadTest()
		if err != nil {
			if model.EnableLoger {
				Logger.Info(err.Error())
			}
			return
		}
		err = NearbyServer.UploadTest()
		if err != nil {
			if model.EnableLoger {
				Logger.Info(err.Error())
			}
			return
		}
		err := analyzer.Run(NearbyServer.Host, func(packetLoss *transport.PLoss) {
			if packetLoss == nil {
				PacketLoss = "N/A"
				return
			}
			PacketLoss = strings.ReplaceAll(packetLoss.String(), "Packet Loss: ", "")
		})
		if err == nil {
			fmt.Print(" " + formatString("Speedtest.net", 16))
			fmt.Print(formatString(formatMbps(NearbyServer.ULSpeed.Mbps()), 16))
			fmt.Print(formatString(formatMbps(NearbyServer.DLSpeed.Mbps()), 16))
			fmt.Print(formatString(NearbyServer.Latency.String(), 16))
			fmt.Print(formatString(PacketLoss, 16))
			fmt.Println()
			if NearbyServer.Context != nil {
				NearbyServer.Context.Reset()
			}
		} else if model.EnableLoger {
			Logger.Info(err.Error())
		}
	}
}

func CustomSpeedTest(url, byWhat string, num int, language string) {
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
	customTargetsSpeedTest(targets, num, language)
}

// RegistrySpeedTest runs speedtest-go only against a caller-owned, prefiltered
// registry selection.
func RegistrySpeedTest(servers []model.ServerMetadata, language string) {
	if model.EnableLoger {
		InitLogger()
		defer Logger.Sync()
	}
	targets := make(speedtest.Servers, 0, len(servers))
	for _, metadata := range servers {
		if strings.TrimSpace(metadata.URL) == "" {
			continue
		}
		server, err := speedtestClient.CustomServer(metadata.URL)
		if err != nil || server == nil {
			if model.EnableLoger && err != nil {
				Logger.Info(err.Error())
			}
			continue
		}
		server.ID = strings.TrimPrefix(strings.TrimSpace(metadata.ID), "global-")
		server.Name = registryServerLabel(metadata)
		targets = append(targets, server)
	}
	customTargetsSpeedTest(targets, len(targets), language)
}

func customTargetsSpeedTest(targets speedtest.Servers, num int, language string) {
	type pingedServer struct {
		server *speedtest.Server
	}
	pinged := make([]pingedServer, 0, len(targets))
	var err, err1, err2, err3 error
	for _, server := range targets {
		if server == nil {
			continue
		}
		err = server.PingTest(nil)
		if err != nil {
			server.Latency = 1000 * time.Millisecond
			if model.EnableLoger {
				Logger.Info(err.Error())
			}
		}
		pinged = append(pinged, pingedServer{server: server})
	}
	sort.SliceStable(pinged, func(i, j int) bool {
		if pinged[i].server.Latency == pinged[j].server.Latency {
			return pinged[i].server.ID < pinged[j].server.ID
		}
		return pinged[i].server.Latency < pinged[j].server.Latency
	})
	analyzer := speedtestClient.NewPacketLossAnalyzer()
	var PacketLoss string
	if len(pinged) == 0 {
		fmt.Println("No match servers")
		if model.EnableLoger {
			Logger.Info("No match servers")
		}
		return
	}
	if num == -1 || num >= len(pinged) {
		num = len(pinged)
	}
	for i := 0; i < len(pinged); i++ {
		server := pinged[i].server
		if i < num {
			err1 = server.DownloadTest()
			err2 = server.UploadTest()
			err3 = analyzer.Run(server.Host, func(packetLoss *transport.PLoss) {
				if packetLoss == nil {
					PacketLoss = "N/A"
					return
				}
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
				if server.Context != nil {
					server.Context.Reset()
				}
				continue
			}
			if err2 != nil {
				if model.EnableLoger {
					Logger.Info(server.ID)
					Logger.Info(err2.Error())
				}
				if server.Context != nil {
					server.Context.Reset()
				}
				continue
			}
			if language == "zh" {
				fmt.Print(" " + formatString(server.Name, 16))
			} else if language == "en" {
				name := server.Name
				name = strings.ReplaceAll(name, "中国香港", "HongKong")
				name = strings.ReplaceAll(name, "洛杉矶", "LosAngeles")
				name = strings.ReplaceAll(name, "日本东京", "Tokyo,Japan")
				name = strings.ReplaceAll(name, "新加坡", "Singapore")
				name = strings.ReplaceAll(name, "法兰克福", "Frankfurt")
				fmt.Print(" " + formatString(name, 16))
			}
			fmt.Print(formatString(formatMbps(server.ULSpeed.Mbps()), 16))
			fmt.Print(formatString(formatMbps(server.DLSpeed.Mbps()), 16))
			fmt.Print(formatString(server.Latency.String(), 16))
			fmt.Print(formatString(PacketLoss, 16))
			fmt.Println()
		}
		if server.Context != nil {
			server.Context.Reset()
		}
	}
}
