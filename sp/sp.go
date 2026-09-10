package sp

import (
	"context"
	"fmt"
	"io"
	"os"
	"os/exec"
	"sort"
	"strings"
	"sync"
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

var runOfficialSpeedtestCommand = func(ctx context.Context, args ...string) ([]byte, error) {
	return execCommandContext(ctx, "speedtest", args...).CombinedOutput()
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
// form used by orchestrators that run multiple diagnostics concurrently. If
// the external client fails or returns an incomplete result, the same request
// is retried through the pure-Go nearby implementation.
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
	if officialNearbySpeedTestWithNetworkContextTo(ctx, writer, network) {
		return
	}
	nearbySpeedTestWithNetworkContextTo(ctx, writer, network)
}

type nearbyMeasurement struct {
	Upload     string
	Download   string
	Latency    string
	PacketLoss string
}

func officialNearbySpeedTestWithNetworkContextTo(ctx context.Context, writer io.Writer, network string) bool {
	// speedtest --progress=no --accept-license --accept-gdpr
	args := []string{"--progress=no", "--accept-license", "--accept-gdpr"}
	args = append(args, officialNetworkArgs(network)...)
	temp, err := runOfficialSpeedtestCommand(ctx, args...)
	if err != nil {
		return false
	}
	measurement, ok := parseOfficialNearbyMeasurement(string(temp))
	if !ok {
		return false
	}
	writeNearbyMeasurement(writer, measurement)
	return true
}

func parseOfficialNearbyMeasurement(output string) (nearbyMeasurement, bool) {
	var measurement nearbyMeasurement
	for _, line := range strings.Split(output, "\n") {
		key, value, found := strings.Cut(line, ":")
		if !found {
			continue
		}
		value = strings.TrimSpace(strings.SplitN(value, "(", 2)[0])
		switch {
		case strings.Contains(key, "Idle Latency"):
			measurement.Latency = value
		case strings.Contains(key, "Download"):
			measurement.Download = value
		case strings.Contains(key, "Upload"):
			measurement.Upload = value
		case strings.Contains(key, "Packet Loss"):
			measurement.PacketLoss = strings.TrimSpace(strings.SplitN(line, ":", 2)[1])
		}
	}
	if measurement.Latency == "" || measurement.Download == "" || measurement.Upload == "" {
		return nearbyMeasurement{}, false
	}
	if measurement.PacketLoss == "" {
		measurement.PacketLoss = "N/A"
	}
	return measurement, true
}

func writeNearbyMeasurement(writer io.Writer, measurement nearbyMeasurement) {
	fmt.Fprint(writer, " "+formatString("Speedtest.net", 16))
	fmt.Fprint(writer, formatString(measurement.Upload, 16))
	fmt.Fprint(writer, formatString(measurement.Download, 16))
	fmt.Fprint(writer, formatString(measurement.Latency, 16))
	fmt.Fprint(writer, formatString(measurement.PacketLoss, 16))
	fmt.Fprintln(writer)
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
	targets, usedFallback := customSpeedtestTargetsFromData(ctx, data, url, byWhat, client, network)
	if usedFallback {
		// A legacy ID can disappear from the official catalog while its exact
		// upload.php endpoint remains live. The Ookla CLI accepts only an ID,
		// so use the reconstructed direct endpoint through speedtest-go.
		customTargetsSpeedTestWithClientContextToWithFallback(ctx, writer, targets, num, language, client, true)
		return
	}
	officialTargetsSpeedTestContextToWithFallback(ctx, writer, targets, num, language, network, usedFallback)
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
	usedDirectEndpoint := false
	for _, metadata := range servers {
		serverID := strings.TrimPrefix(strings.TrimSpace(metadata.ID), "global-")
		if serverID == "" {
			continue
		}
		server, directEndpoint, err := fetchOrBuildRegistrySpeedtestServerWithFallback(ctx, client, metadata)
		if err != nil || server == nil {
			if model.EnableLoger && err != nil {
				Logger.Info(err.Error())
			}
			continue
		}
		usedDirectEndpoint = usedDirectEndpoint || directEndpoint
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
	if usedDirectEndpoint {
		customTargetsSpeedTestWithClientContextToWithFallback(ctx, writer, targets, len(targets), language, client, true)
		return
	}
	officialTargetsSpeedTestContextTo(ctx, writer, targets, len(targets), language, network)
}

func officialTargetsSpeedTest(targets speedtest.Servers, num int, language, network string) {
	officialTargetsSpeedTestContextTo(context.Background(), os.Stdout, targets, num, language, network)
}

func officialTargetsSpeedTestTo(writer io.Writer, targets speedtest.Servers, num int, language, network string) {
	officialTargetsSpeedTestContextTo(context.Background(), writer, targets, num, language, network)
}

// rankSpeedtestTargetsByLatency preserves the historical sequential candidate
// phase used by direct API calls. Preloaded callers use the bounded concurrent
// variant below.
func probeSpeedtestTarget(ctx context.Context, server *speedtest.Server, retryAlternates bool) error {
	pingTimeout := time.Duration(0)
	if retryAlternates {
		pingTimeout = legacyIDFallbackPingTimeout
	}
	pingCtx, pingCancel := speedtestAttemptContext(ctx, pingTimeout)
	err := server.PingTestContext(pingCtx, nil)
	pingCancel()
	return err
}

func recordSpeedtestTargetProbe(ctx context.Context, server *speedtest.Server, retryAlternates bool, probe func(context.Context, *speedtest.Server, bool) error) {
	if err := probe(ctx, server, retryAlternates); err != nil {
		server.Latency = 1000 * time.Millisecond
		if model.EnableLoger {
			Logger.Info(err.Error())
		}
	}
}

func rankSpeedtestTargetsByLatency(ctx context.Context, targets speedtest.Servers, retryAlternates bool) speedtest.Servers {
	if ctx == nil {
		ctx = context.Background()
	}
	ranked := make(speedtest.Servers, 0, len(targets))
	for _, server := range targets {
		if server == nil {
			continue
		}
		if err := ctx.Err(); err != nil {
			break
		}
		recordSpeedtestTargetProbe(ctx, server, retryAlternates, probeSpeedtestTarget)
		ranked = append(ranked, server)
	}
	sort.SliceStable(ranked, func(i, j int) bool {
		if ranked[i].Latency == ranked[j].Latency {
			return ranked[i].ID < ranked[j].ID
		}
		return ranked[i].Latency < ranked[j].Latency
	})
	return ranked
}

// rankSpeedtestTargetsByLatencyConcurrent performs the same lightweight ping
// ranking with a bounded worker pool for the front-loaded preload phase.
func rankSpeedtestTargetsByLatencyConcurrent(ctx context.Context, targets speedtest.Servers, retryAlternates bool) speedtest.Servers {
	return rankSpeedtestTargetsByLatencyConcurrentWithProbe(ctx, targets, retryAlternates, probeSpeedtestTarget)
}

const concurrentCandidateProbeWorkers = 8

// rankSpeedtestTargetsByLatencyConcurrentWithProbe keeps worker scheduling
// independently testable without opening real speedtest streams.
func rankSpeedtestTargetsByLatencyConcurrentWithProbe(ctx context.Context, targets speedtest.Servers, retryAlternates bool, probe func(context.Context, *speedtest.Server, bool) error) speedtest.Servers {
	if ctx == nil {
		ctx = context.Background()
	}
	if probe == nil {
		probe = probeSpeedtestTarget
	}
	// Candidate probes are deliberately bounded. This short phase can overlap
	// other diagnostics, but all workers are joined before any transfer starts.
	type rankedCandidate struct {
		server *speedtest.Server
		index  int
		probed bool
	}
	ranked := make([]rankedCandidate, len(targets))
	candidates := 0
	for index, server := range targets {
		if server != nil {
			ranked[index] = rankedCandidate{server: server, index: index}
			candidates++
		}
	}
	if candidates == 0 {
		return nil
	}
	jobs := make(chan int)
	workers := candidates
	if workers > concurrentCandidateProbeWorkers {
		workers = concurrentCandidateProbeWorkers
	}
	var wait sync.WaitGroup
	wait.Add(workers)
	for range workers {
		go func() {
			defer wait.Done()
			for index := range jobs {
				// A ready send can race cancellation. Do not turn queued work into
				// another network probe once the enclosing run has stopped.
				if ctx.Err() != nil {
					continue
				}
				server := targets[index]
				if server == nil {
					continue
				}
				recordSpeedtestTargetProbe(ctx, server, retryAlternates, probe)
				ranked[index].probed = true
			}
		}()
	}
sendJobs:
	for index, server := range targets {
		if server == nil {
			continue
		}
		if ctx.Err() != nil {
			break
		}
		select {
		case jobs <- index:
		case <-ctx.Done():
			break sendJobs
		}
	}
	close(jobs)
	wait.Wait()
	filtered := ranked[:0]
	for _, candidate := range ranked {
		if candidate.server != nil {
			filtered = append(filtered, candidate)
		}
	}
	ranked = filtered
	sort.SliceStable(ranked, func(i, j int) bool {
		if ranked[i].probed != ranked[j].probed {
			return ranked[i].probed
		}
		if !ranked[i].probed {
			return ranked[i].index < ranked[j].index
		}
		if ranked[i].server.Latency == ranked[j].server.Latency {
			return ranked[i].server.ID < ranked[j].server.ID
		}
		return ranked[i].server.Latency < ranked[j].server.Latency
	})
	servers := make(speedtest.Servers, 0, len(ranked))
	for _, candidate := range ranked {
		servers = append(servers, candidate.server)
	}
	return servers
}

func officialTargetsSpeedTestContextTo(ctx context.Context, writer io.Writer, targets speedtest.Servers, num int, language, network string) {
	officialTargetsSpeedTestContextToWithFallback(ctx, writer, targets, num, language, network, false)
}

func officialTargetsSpeedTestContextToWithFallback(ctx context.Context, writer io.Writer, targets speedtest.Servers, num int, language, network string, retryAlternates bool) {
	targets = rankSpeedtestTargetsByLatency(ctx, targets, retryAlternates)
	officialTargetsSpeedTestContextToWithPreloadedTargets(ctx, writer, targets, num, language, network, retryAlternates)
}

// officialTargetsSpeedTestContextToWithPreloadedTargets runs the established
// official-client transfer logic after candidate latency has already been
// collected and sorted.
func officialTargetsSpeedTestContextToWithPreloadedTargets(ctx context.Context, writer io.Writer, targets speedtest.Servers, num int, language, network string, retryAlternates bool) int {
	if writer == nil {
		writer = io.Discard
	}
	if ctx == nil {
		ctx = context.Background()
	}
	if len(targets) == 0 {
		fmt.Fprintln(writer, "No match servers")
		if model.EnableLoger {
			Logger.Info("No match servers")
		}
		return 0
	}
	if num == -1 || num >= len(targets) {
		num = len(targets)
	}
	attemptLimit := speedtestAttemptLimit(num, len(targets))
	completed := 0
	for _, server := range targets[:attemptLimit] {
		if completed >= num {
			break
		}
		attemptTimeout := standardSpeedtestAttemptTimeout
		if retryAlternates {
			attemptTimeout = legacyIDFallbackAttemptTimeout
		}
		attemptCtx, attemptCancel := speedtestAttemptContext(ctx, attemptTimeout)
		var serverName, UPStr, DLStr, Latency, PacketLoss string
		// speedtest --progress=no --accept-license --accept-gdpr
		args := []string{"--progress=no", "--server-id=" + server.ID, "--accept-license", "--accept-gdpr"}
		args = append(args, officialNetworkArgs(network)...)
		temp, err := runOfficialSpeedtestCommand(attemptCtx, args...)
		attemptCancel()
		if err != nil {
			continue
		}
		serverName = server.Name
		measurement, ok := parseOfficialNearbyMeasurement(string(temp))
		if ok {
			UPStr, DLStr, Latency, PacketLoss = measurement.Upload, measurement.Download, measurement.Latency, measurement.PacketLoss
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
			completed++
		}
	}
	return completed
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
	nearbySpeedTestWithNetworkContextTo(ctx, writer, network)
}

func nearbySpeedTestWithNetworkContextTo(ctx context.Context, writer io.Writer, network string) bool {
	client := speedtestClientForNetwork(network)
	serverList, err := client.FetchServerListContext(ctx)
	if err != nil || serverList == nil {
		if model.EnableLoger && err != nil {
			Logger.Info(err.Error())
		}
		return false
	}
	targets, err := serverList.FindServer([]int{})
	if err != nil {
		if model.EnableLoger {
			Logger.Info(err.Error())
		}
		return false
	}
	targets = pinSpeedtestServersContext(ctx, targets, network)
	targets = rankSpeedtestTargetsByLatencyConcurrent(ctx, targets, true)
	analyzer := client.NewPacketLossAnalyzer()
	for index, nearbyServer := range targets {
		if index >= speedtestAttemptLimit(1, len(targets)) || ctx.Err() != nil {
			break
		}
		if nearbyServer == nil {
			continue
		}
		if nearbyServer.Context != nil {
			nearbyServer.Context.Reset()
		}
		attemptCtx, attemptCancel := speedtestAttemptContext(ctx, standardSpeedtestAttemptTimeout)
		err = nearbyServer.DownloadTestContext(attemptCtx)
		if err != nil {
			if model.EnableLoger {
				Logger.Info(err.Error())
			}
			attemptCancel()
			if nearbyServer.Context != nil {
				nearbyServer.Context.Reset()
			}
			continue
		}
		err = nearbyServer.UploadTestContext(attemptCtx)
		if err != nil {
			if model.EnableLoger {
				Logger.Info(err.Error())
			}
			attemptCancel()
			if nearbyServer.Context != nil {
				nearbyServer.Context.Reset()
			}
			continue
		}
		packetLossText := "N/A"
		err := analyzer.RunWithContext(attemptCtx, nearbyServer.Host, func(packetLoss *transport.PLoss) {
			if packetLoss == nil {
				return
			}
			packetLossText = strings.ReplaceAll(packetLoss.String(), "Packet Loss: ", "")
		})
		attemptCancel()
		if err != nil && model.EnableLoger {
			Logger.Info(err.Error())
		}
		writeNearbyMeasurement(writer, nearbyMeasurement{
			Upload:     formatMbps(nearbyServer.ULSpeed.Mbps()),
			Download:   formatMbps(nearbyServer.DLSpeed.Mbps()),
			Latency:    nearbyServer.Latency.String(),
			PacketLoss: packetLossText,
		})
		if nearbyServer.Context != nil {
			nearbyServer.Context.Reset()
		}
		return true
	}
	return false
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
	targets, usedFallback := customSpeedtestTargetsFromData(ctx, data, url, byWhat, client, network)
	customTargetsSpeedTestWithClientContextToWithFallback(ctx, writer, targets, num, language, client, usedFallback)
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
		server, err := registrySpeedtestServer(client, metadata)
		if err != nil || server == nil {
			if model.EnableLoger && err != nil {
				Logger.Info(err.Error())
			}
			continue
		}
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
	customTargetsSpeedTestWithClientContextToWithFallback(ctx, writer, targets, num, language, client, false)
}

func customTargetsSpeedTestWithClientContextToWithFallback(ctx context.Context, writer io.Writer, targets speedtest.Servers, num int, language string, client *speedtest.Speedtest, retryAlternates bool) {
	targets = rankSpeedtestTargetsByLatency(ctx, targets, retryAlternates)
	customTargetsSpeedTestWithClientContextToWithPreloadedTargets(ctx, writer, targets, num, language, client, retryAlternates)
}

// customTargetsSpeedTestWithClientContextToWithPreloadedTargets runs the
// historical speedtest-go transfer path after the candidate phase has finished.
func customTargetsSpeedTestWithClientContextToWithPreloadedTargets(ctx context.Context, writer io.Writer, targets speedtest.Servers, num int, language string, client *speedtest.Speedtest, retryAlternates bool) int {
	if writer == nil {
		writer = io.Discard
	}
	if ctx == nil {
		ctx = context.Background()
	}
	var err1, err2, err3 error
	if client == nil {
		client = speedtestClient
	}
	analyzer := client.NewPacketLossAnalyzer()
	var PacketLoss string
	if len(targets) == 0 {
		fmt.Fprintln(writer, "No match servers")
		if model.EnableLoger {
			Logger.Info("No match servers")
		}
		return 0
	}
	if num == -1 || num >= len(targets) {
		num = len(targets)
	}
	attemptLimit := speedtestAttemptLimit(num, len(targets))
	completed := 0
	for _, server := range targets[:attemptLimit] {
		if err := ctx.Err(); err != nil {
			return completed
		}
		if completed >= num {
			break
		}
		attemptTimeout := standardSpeedtestAttemptTimeout
		if retryAlternates {
			attemptTimeout = legacyIDFallbackAttemptTimeout
		}
		attemptCtx, attemptCancel := speedtestAttemptContext(ctx, attemptTimeout)
		PacketLoss = ""
		err1 = server.DownloadTestContext(attemptCtx)
		if err1 == nil && attemptCtx.Err() != nil {
			err1 = attemptCtx.Err()
		}
		if err1 != nil {
			attemptCancel()
			if server.Context != nil {
				server.Context.Reset()
			}
			continue
		}
		err2 = server.UploadTestContext(attemptCtx)
		if err2 == nil && attemptCtx.Err() != nil {
			err2 = attemptCtx.Err()
		}
		if err2 != nil {
			attemptCancel()
			if server.Context != nil {
				server.Context.Reset()
			}
			continue
		}
		err3 = analyzer.RunWithContext(attemptCtx, server.Host, func(packetLoss *transport.PLoss) {
			if packetLoss == nil {
				PacketLoss = "N/A"
				return
			}
			PacketLoss = strings.ReplaceAll(packetLoss.String(), "Packet Loss: ", "")
		})
		attemptCancel()
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
		if server.ULSpeed.Mbps() <= 0 || server.DLSpeed.Mbps() <= 0 {
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
		completed++
		if server.Context != nil {
			server.Context.Reset()
		}
	}
	return completed
}

func speedtestAttemptLimit(successTarget, candidates int) int {
	if successTarget <= 0 || candidates <= 0 {
		return 0
	}
	attempts := successTarget * 2
	if attempts > candidates {
		attempts = candidates
	}
	return attempts
}
