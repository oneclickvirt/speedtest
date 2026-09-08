package sp

import (
	"bufio"
	"context"
	"encoding/csv"
	"fmt"
	"io"
	"math"
	"net"
	"os"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/imroc/req/v3"
	"github.com/oneclickvirt/speedtest/model"
	"github.com/showwin/speedtest-go/speedtest"
)

var speedtestClient = speedtest.New(speedtest.WithUserConfig(
	&speedtest.UserConfig{
		UserAgent:      "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/99.0.4844.74 Safari/537.36",
		PingMode:       speedtest.TCP,
		TestMode:       speedtest.HTTPTest,
		MaxConnections: 8,
	}))

const (
	legacyIDFallbackAttemptTimeout = 30 * time.Second
	legacyIDFallbackPingTimeout    = 5 * time.Second
)

func speedtestAttemptContext(ctx context.Context, timeout time.Duration) (context.Context, context.CancelFunc) {
	if ctx == nil {
		ctx = context.Background()
	}
	if timeout <= 0 {
		return ctx, func() {}
	}
	return context.WithTimeout(ctx, timeout)
}

func speedtestClientForNetwork(value string) *speedtest.Speedtest {
	network, err := model.NormalizeNetwork(value)
	if err != nil || network == model.NetworkAuto {
		return speedtestClient
	}
	config := &speedtest.UserConfig{
		UserAgent:      speedtest.DefaultUserAgent,
		PingMode:       speedtest.HTTP,
		TestMode:       speedtest.HTTPTest,
		MaxConnections: 8,
	}
	// WithDoer must be applied after WithUserConfig because the upstream
	// constructor otherwise replaces the transport. HTTP ping and throughput
	// then share one forced-family dialer.
	return speedtest.New(
		speedtest.WithUserConfig(config),
		speedtest.WithDoer(model.NewHTTPClient(network, 30*time.Second)),
	)
}

// pinSpeedtestServer resolves the host that speedtest-go uses for TCP and UDP
// probes. Its HTTP transport is configured separately, so the URL hostname is
// left untouched for TLS SNI and Host headers.
func pinSpeedtestServer(server *speedtest.Server, network model.Network) error {
	return pinSpeedtestServerContext(context.Background(), server, network)
}

func pinSpeedtestServerContext(ctx context.Context, server *speedtest.Server, network model.Network) error {
	if server == nil || network == model.NetworkAuto {
		return nil
	}
	if ctx == nil {
		ctx = context.Background()
	}
	ctx, cancel := context.WithTimeout(ctx, 8*time.Second)
	defer cancel()
	address, err := model.ResolveServerAddress(ctx, server.Host, network)
	if err != nil {
		return err
	}
	server.Host = address
	return nil
}

func pinSpeedtestServers(servers speedtest.Servers, value string) speedtest.Servers {
	return pinSpeedtestServersContext(context.Background(), servers, value)
}

func pinSpeedtestServersContext(ctx context.Context, servers speedtest.Servers, value string) speedtest.Servers {
	network, err := model.NormalizeNetwork(value)
	if err != nil || network == model.NetworkAuto {
		return servers
	}
	pinned := make(speedtest.Servers, 0, len(servers))
	for _, server := range servers {
		if err := pinSpeedtestServerContext(ctx, server, network); err != nil {
			if model.EnableLoger {
				Logger.Info(fmt.Sprintf("resolve speedtest host %q: %v", server.Host, err))
			}
			continue
		}
		pinned = append(pinned, server)
	}
	return pinned
}

func requestClientForNetwork(value string, timeout time.Duration) *req.Client {
	network, err := model.NormalizeNetwork(value)
	if err != nil {
		network = model.NetworkAuto
	}
	client := req.C().SetTimeout(timeout)
	if network != model.NetworkAuto {
		// A proxy can satisfy the request through a different family, which
		// invalidates an explicit IPv4/IPv6 speedtest selection.
		client.SetProxy(nil)
	}
	dial := model.DialContext(network)
	client.SetDial(func(ctx context.Context, networkName, address string) (net.Conn, error) {
		return dial(ctx, networkName, address)
	})
	return client
}

// checkCDN checks if a CDN is available by testing with a known test file
func checkCDN(baseUrl string) bool {
	return checkCDNWithNetwork(baseUrl, "")
}

func checkCDNWithNetwork(baseUrl, network string) bool {
	return checkCDNWithNetworkContext(context.Background(), baseUrl, network)
}

func checkCDNWithNetworkContext(ctx context.Context, baseUrl, network string) bool {
	if ctx == nil {
		ctx = context.Background()
	}
	if err := ctx.Err(); err != nil {
		return false
	}
	testUrl := baseUrl + "https://raw.githubusercontent.com/spiritLHLS/ecs/main/back/test"
	client := requestClientForNetwork(network, 6*time.Second)

	resp, err := client.R().SetContext(ctx).Get(testUrl)
	if err != nil || resp == nil {
		return false
	}
	defer resp.Body.Close()

	b, err := io.ReadAll(resp.Body)
	if err != nil {
		return false
	}

	return strings.Contains(string(b), "success")
}

func getData(endpoint string) string {
	return getDataWithNetworkContext(context.Background(), endpoint, "")
}

func getDataWithNetwork(endpoint, network string) string {
	return getDataWithNetworkContext(context.Background(), endpoint, network)
}

// getDataWithNetworkContext loads a registry while honoring the caller's
// cancellation. This matters on IPv6-only or partially connected hosts where
// a CDN probe can otherwise consume every retry timeout before the benchmark
// starts.
func getDataWithNetworkContext(ctx context.Context, endpoint, network string) string {
	if ctx == nil {
		ctx = context.Background()
	}
	client := requestClientForNetwork(network, 10*time.Second)
	if model.EnableLoger {
		InitLogger()
		defer Logger.Sync()
	}

	// First, find an available CDN
	var availableCdn string
	for _, baseUrl := range model.CdnList {
		if err := ctx.Err(); err != nil {
			return ""
		}
		if checkCDNWithNetworkContext(ctx, baseUrl, network) {
			availableCdn = baseUrl
			if model.EnableLoger {
				Logger.Info(fmt.Sprintf("CDN available: %s", baseUrl))
			}
			break
		}
		select {
		case <-ctx.Done():
			return ""
		case <-time.After(500 * time.Millisecond):
		}
	}

	if availableCdn == "" {
		if model.EnableLoger {
			Logger.Info("No CDN available, trying direct access")
		}
		// Try direct access without CDN
		resp, err := client.R().SetContext(ctx).
			SetRetryCount(2).
			SetRetryBackoffInterval(1*time.Second, 5*time.Second).
			SetRetryFixedInterval(2 * time.Second).
			Get(endpoint)
		if err == nil && resp != nil {
			defer resp.Body.Close()
			b, err := io.ReadAll(resp.Body)
			if err == nil && !strings.Contains(string(b), "error") {
				if model.EnableLoger {
					Logger.Info(fmt.Sprintf("Direct access success, received data length: %d", len(b)))
				}
				return string(b)
			}
		}
		if model.EnableLoger {
			Logger.Info("Direct access failed")
		}
		return ""
	}

	// Use the available CDN
	url := availableCdn + endpoint
	resp, err := client.R().SetContext(ctx).
		SetRetryCount(2).
		SetRetryBackoffInterval(1*time.Second, 5*time.Second).
		SetRetryFixedInterval(2 * time.Second).
		Get(url)
	if err == nil && resp != nil {
		defer resp.Body.Close()
		b, err := io.ReadAll(resp.Body)
		if err == nil && !strings.Contains(string(b), "error") {
			if model.EnableLoger {
				Logger.Info(fmt.Sprintf("CDN access success, received data length: %d", len(b)))
			}
			return string(b)
		}
	}

	if model.EnableLoger {
		Logger.Info(fmt.Sprintf("CDN access failed: %v", err))
	}
	return ""
}

// 自动检测 CSV 分隔符
func detectSeparator(data string) rune {
	scanner := bufio.NewScanner(strings.NewReader(data))
	if scanner.Scan() {
		firstLine := scanner.Text()
		if strings.Contains(firstLine, ";") {
			return ';'
		} else if strings.Contains(firstLine, "\t") {
			return '\t'
		}
	}
	return ','
}

func parseDataFromURL(data, url string) speedtest.Servers {
	return parseDataFromURLWithClient(data, url, speedtestClient)
}

func parseDataFromURLWithClient(data, url string, client *speedtest.Speedtest) speedtest.Servers {
	if model.EnableLoger {
		InitLogger()
		defer Logger.Sync()
	}

	var targets speedtest.Servers
	if data == "" {
		if model.EnableLoger {
			Logger.Info("No data received for parsing")
		}
		return targets
	}

	separator := detectSeparator(data)
	reader := csv.NewReader(strings.NewReader(data))
	reader.Comma = separator

	records, err := reader.ReadAll()
	if err != nil {
		if model.EnableLoger {
			Logger.Info(fmt.Sprintf("CSV parsing error: %v", err))
		}
		return targets
	}

	// 过滤掉标题行
	if len(records) > 0 && len(records[0]) > 6 && (records[0][6] == "country_code" || records[0][1] == "country_code") {
		records = records[1:]
	}

	for _, record := range records {
		if len(record) == 0 {
			continue // 跳过空行
		}
		if len(record) < 11 {
			if model.EnableLoger {
				Logger.Info(fmt.Sprintf("Skipping record with insufficient columns: %v", record))
			}
			continue
		}

		customURL := record[5]
		if client == nil {
			client = speedtestClient
		}
		target, errFetch := client.CustomServer(customURL)
		if errFetch != nil {
			if model.EnableLoger {
				Logger.Info(fmt.Sprintf("Error fetching server from URL %s: %v", customURL, errFetch))
			}
			continue
		}
		if target == nil {
			continue
		}
		target.Name = record[10] + record[7] + record[8]
		targets = append(targets, target)
	}
	return targets
}

type legacyIDRecord struct {
	id   string
	name string
	host string
	ip   string
	port string
}

func parseLegacyIDRecords(data string) []legacyIDRecord {
	if data == "" {
		return nil
	}
	reader := csv.NewReader(strings.NewReader(data))
	reader.Comma = detectSeparator(data)
	records, err := reader.ReadAll()
	if err != nil {
		return nil
	}
	if len(records) > 0 && len(records[0]) > 6 && (records[0][6] == "country_code" || records[0][1] == "country_code") {
		records = records[1:]
	}
	parsed := make([]legacyIDRecord, 0, len(records))
	for _, record := range records {
		if len(record) < 4 {
			continue
		}
		id := strings.TrimSpace(record[0])
		if id == "" {
			continue
		}
		parsed = append(parsed, legacyIDRecord{
			id:   id,
			name: strings.TrimSpace(record[3]),
			ip:   strings.TrimSpace(record[4]),
			host: strings.TrimSpace(record[5]),
			port: strings.TrimSpace(record[6]),
		})
	}
	return parsed
}

func legacyIDServerName(source, name string) string {
	if strings.Contains(source, "Mobile") {
		return "移动" + name
	}
	if strings.Contains(source, "Telecom") {
		return "电信" + name
	}
	if strings.Contains(source, "Unicom") {
		return "联通" + name
	}
	return name
}

func usableSpeedtestServer(server *speedtest.Server) bool {
	return server != nil && strings.TrimSpace(server.Host) != "" && strings.TrimSpace(server.URL) != ""
}

func registrySpeedtestServer(client *speedtest.Speedtest, metadata model.ServerMetadata) (*speedtest.Server, error) {
	if client == nil {
		client = speedtestClient
	}
	endpoint := strings.TrimSpace(metadata.URL)
	if endpoint == "" {
		return nil, fmt.Errorf("registry server %q has no URL", metadata.ID)
	}
	server, err := client.CustomServer(endpoint)
	if err != nil || server == nil {
		if err == nil {
			err = fmt.Errorf("registry server %q is unavailable", metadata.ID)
		}
		return nil, err
	}
	server.ID = strings.TrimPrefix(strings.TrimSpace(metadata.ID), "global-")
	server.Name = registryServerLabel(metadata)
	if host := strings.TrimSpace(metadata.Host); host != "" {
		server.Host = host
	}
	return server, nil
}

func legacyIDServerEndpoint(record legacyIDRecord) string {
	host := strings.Trim(strings.TrimSpace(record.host), "[]")
	if host == "" {
		host = strings.Trim(strings.TrimSpace(record.ip), "[]")
	}
	if host == "" || strings.ContainsAny(host, "/?#@") {
		return ""
	}
	port, err := strconv.Atoi(strings.TrimSpace(record.port))
	if err != nil || port < 1 || port > 65535 {
		return ""
	}
	scheme := "http"
	if port == 443 {
		scheme = "https"
	}
	return scheme + "://" + net.JoinHostPort(host, strconv.Itoa(port))
}

func legacyIDFallbackTargetsFromRecords(ctx context.Context, data, source string, client *speedtest.Speedtest) speedtest.Servers {
	if ctx == nil {
		ctx = context.Background()
	}
	if client == nil {
		client = speedtestClient
	}
	targets := make(speedtest.Servers, 0)
	for _, record := range parseLegacyIDRecords(data) {
		if ctx.Err() != nil {
			return targets
		}
		endpoint := legacyIDServerEndpoint(record)
		if endpoint == "" {
			continue
		}
		target, err := client.CustomServer(endpoint)
		if err != nil || target == nil {
			continue
		}
		target.ID = record.id
		target.Name = legacyIDServerName(source, record.name)
		targets = append(targets, target)
	}
	return targets
}

func legacyIDFallbackTargetsFromRegistry(ctx context.Context, data, source string, client *speedtest.Speedtest, servers []model.ServerMetadata) speedtest.Servers {
	if ctx == nil {
		ctx = context.Background()
	}
	byID := make(map[string]model.ServerMetadata, len(servers))
	for _, server := range servers {
		id := strings.TrimPrefix(strings.TrimSpace(server.ID), "global-")
		if id != "" && strings.TrimSpace(server.URL) != "" {
			byID[id] = server
		}
	}
	targets := make(speedtest.Servers, 0)
	for _, record := range parseLegacyIDRecords(data) {
		if ctx.Err() != nil {
			return targets
		}
		metadata, ok := byID[record.id]
		if !ok {
			continue
		}
		target, err := registrySpeedtestServer(client, metadata)
		if err != nil {
			continue
		}
		target.ID = record.id
		target.Name = legacyIDServerName(source, record.name)
		targets = append(targets, target)
	}
	return targets
}

func legacyIDFallbackTargets(ctx context.Context, data, source string, client *speedtest.Speedtest) speedtest.Servers {
	// Legacy CSVs contain the exact endpoint hostname and port selected by the
	// historical profile. Prefer that data over a current Ookla ID lookup: old
	// IDs routinely disappear from ios-config before their endpoint stops
	// serving the standard upload.php protocol.
	if targets := legacyIDFallbackTargetsFromRecords(ctx, data, source, client); len(targets) > 0 {
		return targets
	}
	loaded, err := model.LoadEmbeddedServerRegistry(1)
	if err != nil {
		if model.EnableLoger {
			Logger.Info(fmt.Sprintf("load embedded speedtest registry: %v", err))
		}
		return nil
	}
	return legacyIDFallbackTargetsFromRegistry(ctx, data, source, client, loaded.Servers)
}

func fetchOrBuildRegistrySpeedtestServer(ctx context.Context, client *speedtest.Speedtest, metadata model.ServerMetadata) (*speedtest.Server, error) {
	server, _, err := fetchOrBuildRegistrySpeedtestServerWithFallback(ctx, client, metadata)
	return server, err
}

// fetchOrBuildRegistrySpeedtestServerWithFallback reports whether the caller
// must use the reconstructed direct endpoint instead of an Ookla server ID.
func fetchOrBuildRegistrySpeedtestServerWithFallback(ctx context.Context, client *speedtest.Speedtest, metadata model.ServerMetadata) (*speedtest.Server, bool, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	if client == nil {
		client = speedtestClient
	}
	serverID := strings.TrimPrefix(strings.TrimSpace(metadata.ID), "global-")
	if serverID != "" {
		server, err := client.FetchServerByIDContext(ctx, serverID)
		if err == nil && usableSpeedtestServer(server) {
			server.ID = serverID
			server.Name = registryServerLabel(metadata)
			return server, false, nil
		}
	}
	server, err := registrySpeedtestServer(client, metadata)
	if err != nil {
		return nil, false, err
	}
	return server, true, nil
}

func customSpeedtestTargetsFromData(ctx context.Context, data, source, byWhat string, client *speedtest.Speedtest, network string) (speedtest.Servers, bool) {
	var targets speedtest.Servers
	if byWhat == "id" {
		targets = parseDataFromIDWithClientContext(ctx, data, source, client)
	} else if byWhat == "url" {
		targets = parseDataFromURLWithClient(data, source, client)
	}
	targets = pinSpeedtestServersContext(ctx, targets, network)
	if len(targets) > 0 || byWhat != "id" {
		return targets, false
	}

	// The legacy CSV is still the source of the selected IDs. Only when its
	// central ID lookup produced no usable target do we reconstruct matching IDs
	// from the registry embedded in this exact release.
	fallback := legacyIDFallbackTargets(ctx, data, source, client)
	fallback = pinSpeedtestServersContext(ctx, fallback, network)
	if len(fallback) > 0 && model.EnableLoger {
		Logger.Info("using embedded registry fallback for legacy speedtest IDs")
	}
	return fallback, len(fallback) > 0
}

func parseDataFromID(data, url string) speedtest.Servers {
	return parseDataFromIDWithClientContext(context.Background(), data, url, speedtestClient)
}

func parseDataFromIDWithClient(data, url string, client *speedtest.Speedtest) speedtest.Servers {
	return parseDataFromIDWithClientContext(context.Background(), data, url, client)
}

func parseDataFromIDWithClientContext(ctx context.Context, data, url string, client *speedtest.Speedtest) speedtest.Servers {
	if ctx == nil {
		ctx = context.Background()
	}
	if model.EnableLoger {
		InitLogger()
		defer Logger.Sync()
	}

	var targets speedtest.Servers
	if data == "" {
		if model.EnableLoger {
			Logger.Info("No data received for parsing")
		}
		return targets
	}

	separator := detectSeparator(data)
	reader := csv.NewReader(strings.NewReader(data))
	reader.Comma = separator

	records, err := reader.ReadAll()
	if err != nil {
		if model.EnableLoger {
			Logger.Info(fmt.Sprintf("CSV parsing error: %v", err))
		}
		return targets
	}

	// 过滤掉标题行
	if len(records) > 0 && len(records[0]) > 6 && (records[0][6] == "country_code" || records[0][1] == "country_code") {
		records = records[1:]
	}

	for _, record := range records {
		if err := ctx.Err(); err != nil {
			return targets
		}
		if len(record) == 0 {
			continue // 跳过空行
		}
		if len(record) < 4 {
			if model.EnableLoger {
				Logger.Info(fmt.Sprintf("Skipping record with insufficient columns: %v", record))
			}
			continue
		}

		id := record[0]
		if client == nil {
			client = speedtestClient
		}
		serverPtr, errFetch := client.FetchServerByIDContext(ctx, id)
		if errFetch != nil {
			if model.EnableLoger {
				Logger.Info(fmt.Sprintf("Error fetching server by ID %s: %v", id, errFetch))
			}
			continue
		}
		if !usableSpeedtestServer(serverPtr) {
			if model.EnableLoger {
				Logger.Info(fmt.Sprintf("Incomplete server data for ID %s", id))
			}
			continue
		}

		serverPtr.Name = legacyIDServerName(url, record[3])
		targets = append(targets, serverPtr)
	}
	return targets
}

// 计算字符串的显示宽度（考虑中文字符）
func displayWidth(s string) int {
	width := 0
	for _, r := range s {
		if utf8.RuneLen(r) == 3 {
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
		padding := width - displayW
		return s + fmt.Sprintf("%*s", padding, "")
	}
	return s
}

func formatMbps(value float64) string {
	if math.IsNaN(value) || math.IsInf(value, 0) || value < 0 {
		value = 0
	}
	return fmt.Sprintf("%.2f Mbps", math.Round((value+1e-9)*100)/100)
}

func ShowHead(language string) {
	ShowHeadTo(io.Writer(os.Stdout), language)
}

// ShowHeadTo renders the speed-test table header to the caller's writer.
// Keeping the writer explicit makes independent speed-test jobs safe to run
// concurrently without redirecting process-wide stdout.
func ShowHeadTo(writer io.Writer, language string) {
	if writer == nil {
		writer = io.Discard
	}
	headers1 := []string{"位置", "上传速度", "下载速度", "延迟", "丢包率"}
	headers2 := []string{"Location", "Upload Speed", "Download Speed", "Latency", "PacketLoss"}
	if language == "zh" {
		for index, header := range headers1 {
			if index == 0 {
				fmt.Fprint(writer, " ")
			}
			fmt.Fprint(writer, formatString(header, 16))
		}
		fmt.Fprintln(writer)
	} else if language == "en" {
		for index, header := range headers2 {
			if index == 0 {
				fmt.Fprint(writer, " ")
			}
			fmt.Fprint(writer, formatString(header, 16))
		}
		fmt.Fprintln(writer)
	}
}
