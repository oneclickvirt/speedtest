package sp

import (
	"context"
	"fmt"
	"io"
	"os"
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
	return execCommandContext(context.Background(), name, arg...)
}

func execCommandContext(ctx context.Context, name string, arg ...string) *exec.Cmd {
	if ctx == nil {
		ctx = context.Background()
	}
	if hasRootPermission() {
		if isSudoAvailable() {
			return exec.CommandContext(ctx, "sudo", append([]string{name}, arg...)...)
		}
	}
	return exec.CommandContext(ctx, name, arg...)
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
	OfficialNearbySpeedTestContext(context.Background())
}

// OfficialNearbySpeedTestContext runs the Ookla client with cancellation.
func OfficialNearbySpeedTestContext(ctx context.Context) {
	OfficialNearbySpeedTestWithNetworkContextTo(ctx, os.Stdout, "")
}

// OfficialNearbySpeedTestWithNetwork passes an explicit family to the Ookla
// client when requested. Empty keeps the CLI's normal automatic behavior.
func OfficialNearbySpeedTestWithNetwork(network string) {
	OfficialNearbySpeedTestWithNetworkContextTo(context.Background(), os.Stdout, network)
}

// OfficialNearbySpeedTestWithNetworkContext keeps an explicit address family
// while allowing callers to stop a stuck external speedtest process.
func OfficialNearbySpeedTestWithNetworkContext(ctx context.Context, network string) {
	OfficialNearbySpeedTestWithNetworkContextTo(ctx, os.Stdout, network)
}

// OfficialNearbySpeedTestWithNetworkTo is the writer-aware form of
// OfficialNearbySpeedTestWithNetwork. The legacy entry point above remains
// compatible for command-line callers.
func OfficialNearbySpeedTestWithNetworkTo(writer io.Writer, network string) {
	OfficialNearbySpeedTestWithNetworkContextTo(context.Background(), writer, network)
}

// OfficialNearbySpeedTestWithNetworkContextTo is the cancellable, writer-aware
// form used by orchestrators that run multiple diagnostics concurrently.
func OfficialNearbySpeedTestWithNetworkContextTo(ctx context.Context, writer io.Writer, network string) {
	if writer == nil {
		writer = io.Discard
	}
	if ctx == nil {
		ctx = context.Background()
	}
	if model.EnableLoger {
		InitLogger()
		defer Logger.Sync()
	}
	var UPStr, DLStr, Latency, PacketLoss string // serverID,
	// speedtest --progress=no --accept-license --accept-gdpr
	args := []string{"--progress=no", "--accept-license", "--accept-gdpr"}
	args = append(args, officialNetworkArgs(network)...)
	sptCheck := execCommandContext(ctx, "speedtest", args...)
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
			fmt.Fprint(writer, " "+formatString("Speedtest.net", 16))
			fmt.Fprint(writer, formatString(UPStr, 16))
			fmt.Fprint(writer, formatString(DLStr, 16))
			fmt.Fprint(writer, formatString(Latency, 16))
			fmt.Fprint(writer, formatString(PacketLoss, 16))
			fmt.Fprintln(writer)
		}
	}
}

func OfficialCustomSpeedTest(url, byWhat string, num int, language string) {
	OfficialCustomSpeedTestWithNetworkContextTo(context.Background(), os.Stdout, url, byWhat, num, language, "")
}

func OfficialCustomSpeedTestWithNetwork(url, byWhat string, num int, language, network string) {
	OfficialCustomSpeedTestWithNetworkContextTo(context.Background(), os.Stdout, url, byWhat, num, language, network)
}

// OfficialCustomSpeedTestWithNetworkContext runs an official test with a
// caller-owned cancellation boundary.
func OfficialCustomSpeedTestWithNetworkContext(ctx context.Context, url, byWhat string, num int, language, network string) {
	OfficialCustomSpeedTestWithNetworkContextTo(ctx, os.Stdout, url, byWhat, num, language, network)
}

// OfficialCustomSpeedTestWithNetworkTo is the writer-aware form of
// OfficialCustomSpeedTestWithNetwork.
func OfficialCustomSpeedTestWithNetworkTo(writer io.Writer, url, byWhat string, num int, language, network string) {
	OfficialCustomSpeedTestWithNetworkContextTo(context.Background(), writer, url, byWhat, num, language, network)
}

// OfficialCustomSpeedTestWithNetworkContextTo is the cancellable, isolated
// output form used by GoECS.
func OfficialCustomSpeedTestWithNetworkContextTo(ctx context.Context, writer io.Writer, url, byWhat string, num int, language, network string) {
	if writer == nil {
		writer = io.Discard
	}
	if ctx == nil {
		ctx = context.Background()
	}
	if model.EnableLoger {
		InitLogger()
		defer Logger.Sync()
	}
	if !strings.Contains(url, ".net") {
		fmt.Fprintln(writer, "Official speedtest only use .net platform, can not use other platforms.")
		return
	}
	data := getDataWithNetworkContext(ctx, url, network)
	client := speedtestClientForNetwork(network)
	var targets speedtest.Servers
	if byWhat == "id" {
		targets = parseDataFromIDWithClientContext(ctx, data, url, client)
	} else if byWhat == "url" {
		targets = parseDataFromURLWithClient(data, url, client)
	}
	targets = pinSpeedtestServersContext(ctx, targets, network)
	officialTargetsSpeedTestContextTo(ctx, writer, targets, num, language, network)
}

// OfficialRegistrySpeedTest runs the official client only against the
// prefiltered registry selection supplied by the caller.
func OfficialRegistrySpeedTest(servers []model.ServerMetadata, language string) {
	OfficialRegistrySpeedTestWithNetworkContextTo(context.Background(), os.Stdout, servers, language, "")
}

func OfficialRegistrySpeedTestWithNetwork(servers []model.ServerMetadata, language, network string) {
	OfficialRegistrySpeedTestWithNetworkContextTo(context.Background(), os.Stdout, servers, language, network)
}

// OfficialRegistrySpeedTestWithNetworkContext runs selected official servers
// under a caller-owned cancellation boundary.
func OfficialRegistrySpeedTestWithNetworkContext(ctx context.Context, servers []model.ServerMetadata, language, network string) {
	OfficialRegistrySpeedTestWithNetworkContextTo(ctx, os.Stdout, servers, language, network)
}

// OfficialRegistrySpeedTestWithNetworkTo runs the official client and writes
// all human-readable rows to writer.
func OfficialRegistrySpeedTestWithNetworkTo(writer io.Writer, servers []model.ServerMetadata, language, network string) {
	OfficialRegistrySpeedTestWithNetworkContextTo(context.Background(), writer, servers, language, network)
}

// OfficialRegistrySpeedTestWithNetworkContextTo is the cancellable, isolated
// output form used by the structured runner.
func OfficialRegistrySpeedTestWithNetworkContextTo(ctx context.Context, writer io.Writer, servers []model.ServerMetadata, language, network string) {
	if writer == nil {
		writer = io.Discard
	}
	if ctx == nil {
		ctx = context.Background()
	}
	if model.EnableLoger {
		InitLogger()
		defer Logger.Sync()
	}
	targets := make(speedtest.Servers, 0, len(servers))
	client := speedtestClientForNetwork(network)
	for _, metadata := range servers {
		serverID := strings.TrimPrefix(strings.TrimSpace(metadata.ID), "global-")
		if serverID == "" {
			continue
		}
		server, err := client.FetchServerByIDContext(ctx, serverID)
		if err != nil || server == nil {
			if model.EnableLoger && err != nil {
				Logger.Info(err.Error())
			}
			continue
		}
		server.ID = serverID
		server.Name = registryServerLabel(metadata)
		if metadata.ResolvedHost != "" {
			server.Host = metadata.ResolvedHost
		} else if normalizedNetwork, normalizeErr := model.NormalizeNetwork(network); normalizeErr == nil {
			if err := pinSpeedtestServerContext(ctx, server, normalizedNetwork); err != nil {
				if model.EnableLoger {
					Logger.Info(err.Error())
				}
				continue
			}
		}
		targets = append(targets, server)
	}
	officialTargetsSpeedTestContextTo(ctx, writer, targets, len(targets), language, network)
}

func officialTargetsSpeedTest(targets speedtest.Servers, num int, language, network string) {
	officialTargetsSpeedTestContextTo(context.Background(), os.Stdout, targets, num, language, network)
}

func officialTargetsSpeedTestTo(writer io.Writer, targets speedtest.Servers, num int, language, network string) {
	officialTargetsSpeedTestContextTo(context.Background(), writer, targets, num, language, network)
}

func officialTargetsSpeedTestContextTo(ctx context.Context, writer io.Writer, targets speedtest.Servers, num int, language, network string) {
	if writer == nil {
		writer = io.Discard
	}
	if ctx == nil {
		ctx = context.Background()
	}
	type pingedServer struct {
		server *speedtest.Server
	}
	pinged := make([]pingedServer, 0, len(targets))
	for _, server := range targets {
		if server == nil {
			continue
		}
		if err := server.PingTestContext(ctx, nil); err != nil {
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
		fmt.Fprintln(writer, "No match servers")
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
			args := []string{"--progress=no", "--server-id=" + server.ID, "--accept-license", "--accept-gdpr"}
			args = append(args, officialNetworkArgs(network)...)
			sptCheck := execCommandContext(ctx, "speedtest", args...)
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
						fmt.Fprint(writer, " "+formatString(serverName, 16))
					} else if language == "en" {
						name := serverName
						name = strings.ReplaceAll(name, "中国香港", "HongKong")
						name = strings.ReplaceAll(name, "洛杉矶", "LosAngeles")
						name = strings.ReplaceAll(name, "日本东京", "Tokyo,Japan")
						name = strings.ReplaceAll(name, "新加坡", "Singapore")
						name = strings.ReplaceAll(name, "法兰克福", "Frankfurt")
						fmt.Fprint(writer, " "+formatString(name, 16))
					}
					fmt.Fprint(writer, formatString(UPStr, 16))
					fmt.Fprint(writer, formatString(DLStr, 16))
					fmt.Fprint(writer, formatString(Latency, 16))
					fmt.Fprint(writer, formatString(PacketLoss, 16))
					fmt.Fprintln(writer)
				}
			}
		}
	}
}

func officialNetworkArgs(value string) []string {
	network, err := model.NormalizeNetwork(value)
	if err != nil {
		return nil
	}
	switch network {
	case model.NetworkIPv4:
		return []string{"--ip-version=4"}
	case model.NetworkIPv6:
		return []string{"--ip-version=6"}
	default:
		return nil
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
	NearbySpeedTestWithNetworkContextTo(context.Background(), os.Stdout, "")
}

// NearbySpeedTestWithNetwork runs the Go client with an optional explicit
// family. It is the pure-Go path used when the official binary is absent.
func NearbySpeedTestWithNetwork(network string) {
	NearbySpeedTestWithNetworkContextTo(context.Background(), os.Stdout, network)
}

// NearbySpeedTestWithNetworkContext runs the pure-Go nearby test with
// cancellation and an optional address-family pin.
func NearbySpeedTestWithNetworkContext(ctx context.Context, network string) {
	NearbySpeedTestWithNetworkContextTo(ctx, os.Stdout, network)
}

// NearbySpeedTestWithNetworkTo is the writer-aware form of
// NearbySpeedTestWithNetwork.
func NearbySpeedTestWithNetworkTo(writer io.Writer, network string) {
	NearbySpeedTestWithNetworkContextTo(context.Background(), writer, network)
}

// NearbySpeedTestWithNetworkContextTo is the cancellable, writer-aware form.
func NearbySpeedTestWithNetworkContextTo(ctx context.Context, writer io.Writer, network string) {
	if writer == nil {
		writer = io.Discard
	}
	if ctx == nil {
		ctx = context.Background()
	}
	if model.EnableLoger {
		InitLogger()
		defer Logger.Sync()
	}
	client := speedtestClientForNetwork(network)
	serverList, err := client.FetchServerListContext(ctx)
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
	targets = pinSpeedtestServersContext(ctx, targets, network)
	analyzer := client.NewPacketLossAnalyzer()
	var LowestLatency time.Duration
	var NearbyServer *speedtest.Server
	var PacketLoss string
	for _, server := range targets {
		if server == nil {
			continue
		}
		if err := server.PingTestContext(ctx, nil); err != nil {
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
		err = NearbyServer.DownloadTestContext(ctx)
		if err != nil {
			if model.EnableLoger {
				Logger.Info(err.Error())
			}
			return
		}
		err = NearbyServer.UploadTestContext(ctx)
		if err != nil {
			if model.EnableLoger {
				Logger.Info(err.Error())
			}
			return
		}
		err := analyzer.RunWithContext(ctx, NearbyServer.Host, func(packetLoss *transport.PLoss) {
			if packetLoss == nil {
				PacketLoss = "N/A"
				return
			}
			PacketLoss = strings.ReplaceAll(packetLoss.String(), "Packet Loss: ", "")
		})
		if err == nil {
			fmt.Fprint(writer, " "+formatString("Speedtest.net", 16))
			fmt.Fprint(writer, formatString(formatMbps(NearbyServer.ULSpeed.Mbps()), 16))
			fmt.Fprint(writer, formatString(formatMbps(NearbyServer.DLSpeed.Mbps()), 16))
			fmt.Fprint(writer, formatString(NearbyServer.Latency.String(), 16))
			fmt.Fprint(writer, formatString(PacketLoss, 16))
			fmt.Fprintln(writer)
			if NearbyServer.Context != nil {
				NearbyServer.Context.Reset()
			}
		} else if model.EnableLoger {
			Logger.Info(err.Error())
		}
	}
}

func CustomSpeedTest(url, byWhat string, num int, language string) {
	CustomSpeedTestWithNetworkContextTo(context.Background(), os.Stdout, url, byWhat, num, language, "")
}

func CustomSpeedTestWithNetwork(url, byWhat string, num int, language, network string) {
	CustomSpeedTestWithNetworkContextTo(context.Background(), os.Stdout, url, byWhat, num, language, network)
}

// CustomSpeedTestWithNetworkContext runs the pure-Go custom test with
// cancellation while preserving the requested address family.
func CustomSpeedTestWithNetworkContext(ctx context.Context, url, byWhat string, num int, language, network string) {
	CustomSpeedTestWithNetworkContextTo(ctx, os.Stdout, url, byWhat, num, language, network)
}

// CustomSpeedTestWithNetworkTo is the writer-aware form of
// CustomSpeedTestWithNetwork.
func CustomSpeedTestWithNetworkTo(writer io.Writer, url, byWhat string, num int, language, network string) {
	CustomSpeedTestWithNetworkContextTo(context.Background(), writer, url, byWhat, num, language, network)
}

// CustomSpeedTestWithNetworkContextTo is the cancellable, isolated output
// form used by GoECS and other concurrent callers.
func CustomSpeedTestWithNetworkContextTo(ctx context.Context, writer io.Writer, url, byWhat string, num int, language, network string) {
	if writer == nil {
		writer = io.Discard
	}
	if ctx == nil {
		ctx = context.Background()
	}
	if model.EnableLoger {
		InitLogger()
		defer Logger.Sync()
	}
	data := getDataWithNetworkContext(ctx, url, network)
	client := speedtestClientForNetwork(network)
	var targets speedtest.Servers
	if byWhat == "id" {
		targets = parseDataFromIDWithClientContext(ctx, data, url, client)
	} else if byWhat == "url" {
		targets = parseDataFromURLWithClient(data, url, client)
	}
	targets = pinSpeedtestServersContext(ctx, targets, network)
	customTargetsSpeedTestWithClientContextTo(ctx, writer, targets, num, language, client)
}

// RegistrySpeedTest runs speedtest-go only against a caller-owned, prefiltered
// registry selection.
func RegistrySpeedTest(servers []model.ServerMetadata, language string) {
	RegistrySpeedTestWithNetworkContextTo(context.Background(), os.Stdout, servers, language, "")
}

func RegistrySpeedTestWithNetwork(servers []model.ServerMetadata, language, network string) {
	RegistrySpeedTestWithNetworkContextTo(context.Background(), os.Stdout, servers, language, network)
}

// RegistrySpeedTestWithNetworkContext runs a caller-selected registry with
// cancellation.
func RegistrySpeedTestWithNetworkContext(ctx context.Context, servers []model.ServerMetadata, language, network string) {
	RegistrySpeedTestWithNetworkContextTo(ctx, os.Stdout, servers, language, network)
}

// RegistrySpeedTestWithNetworkTo runs speedtest-go against the supplied
// registry and writes rows to writer.
func RegistrySpeedTestWithNetworkTo(writer io.Writer, servers []model.ServerMetadata, language, network string) {
	RegistrySpeedTestWithNetworkContextTo(context.Background(), writer, servers, language, network)
}

// RegistrySpeedTestWithNetworkContextTo is the cancellable, writer-aware
// registry test entry point.
func RegistrySpeedTestWithNetworkContextTo(ctx context.Context, writer io.Writer, servers []model.ServerMetadata, language, network string) {
	if writer == nil {
		writer = io.Discard
	}
	if ctx == nil {
		ctx = context.Background()
	}
	if model.EnableLoger {
		InitLogger()
		defer Logger.Sync()
	}
	targets := make(speedtest.Servers, 0, len(servers))
	client := speedtestClientForNetwork(network)
	for _, metadata := range servers {
		if strings.TrimSpace(metadata.URL) == "" {
			continue
		}
		server, err := client.CustomServer(metadata.URL)
		if err != nil || server == nil {
			if model.EnableLoger && err != nil {
				Logger.Info(err.Error())
			}
			continue
		}
		server.ID = strings.TrimPrefix(strings.TrimSpace(metadata.ID), "global-")
		server.Name = registryServerLabel(metadata)
		if metadata.ResolvedHost != "" {
			server.Host = metadata.ResolvedHost
		} else if normalizedNetwork, normalizeErr := model.NormalizeNetwork(network); normalizeErr == nil {
			if err := pinSpeedtestServerContext(ctx, server, normalizedNetwork); err != nil {
				if model.EnableLoger {
					Logger.Info(err.Error())
				}
				continue
			}
		}
		targets = append(targets, server)
	}
	customTargetsSpeedTestWithClientContextTo(ctx, writer, targets, len(targets), language, client)
}

func customTargetsSpeedTest(targets speedtest.Servers, num int, language string) {
	customTargetsSpeedTestWithClientContextTo(context.Background(), os.Stdout, targets, num, language, speedtestClient)
}

func customTargetsSpeedTestWithClient(targets speedtest.Servers, num int, language string, client *speedtest.Speedtest) {
	customTargetsSpeedTestWithClientContextTo(context.Background(), os.Stdout, targets, num, language, client)
}

func customTargetsSpeedTestWithClientTo(writer io.Writer, targets speedtest.Servers, num int, language string, client *speedtest.Speedtest) {
	customTargetsSpeedTestWithClientContextTo(context.Background(), writer, targets, num, language, client)
}

func customTargetsSpeedTestWithClientContextTo(ctx context.Context, writer io.Writer, targets speedtest.Servers, num int, language string, client *speedtest.Speedtest) {
	if writer == nil {
		writer = io.Discard
	}
	if ctx == nil {
		ctx = context.Background()
	}
	type pingedServer struct {
		server *speedtest.Server
	}
	pinged := make([]pingedServer, 0, len(targets))
	var err, err1, err2, err3 error
	for _, server := range targets {
		if server == nil {
			continue
		}
		err = server.PingTestContext(ctx, nil)
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
	if client == nil {
		client = speedtestClient
	}
	analyzer := client.NewPacketLossAnalyzer()
	var PacketLoss string
	if len(pinged) == 0 {
		fmt.Fprintln(writer, "No match servers")
		if model.EnableLoger {
			Logger.Info("No match servers")
		}
		return
	}
	if num == -1 || num >= len(pinged) {
		num = len(pinged)
	}
	for i := 0; i < len(pinged); i++ {
		if err := ctx.Err(); err != nil {
			return
		}
		server := pinged[i].server
		if i < num {
			err1 = server.DownloadTestContext(ctx)
			err2 = server.UploadTestContext(ctx)
			err3 = analyzer.RunWithContext(ctx, server.Host, func(packetLoss *transport.PLoss) {
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
				fmt.Fprint(writer, " "+formatString(server.Name, 16))
			} else if language == "en" {
				name := server.Name
				name = strings.ReplaceAll(name, "中国香港", "HongKong")
				name = strings.ReplaceAll(name, "洛杉矶", "LosAngeles")
				name = strings.ReplaceAll(name, "日本东京", "Tokyo,Japan")
				name = strings.ReplaceAll(name, "新加坡", "Singapore")
				name = strings.ReplaceAll(name, "法兰克福", "Frankfurt")
				fmt.Fprint(writer, " "+formatString(name, 16))
			}
			fmt.Fprint(writer, formatString(formatMbps(server.ULSpeed.Mbps()), 16))
			fmt.Fprint(writer, formatString(formatMbps(server.DLSpeed.Mbps()), 16))
			fmt.Fprint(writer, formatString(server.Latency.String(), 16))
			fmt.Fprint(writer, formatString(PacketLoss, 16))
			fmt.Fprintln(writer)
		}
		if server.Context != nil {
			server.Context.Reset()
		}
	}
}
