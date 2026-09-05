package sp

import (
	"bufio"
	"context"
	"encoding/csv"
	"fmt"
	"io"
	"math"
	"net"
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
	if server == nil || network == model.NetworkAuto {
		return nil
	}
	ctx, cancel := context.WithTimeout(context.Background(), 8*time.Second)
	defer cancel()
	address, err := model.ResolveServerAddress(ctx, server.Host, network)
	if err != nil {
		return err
	}
	server.Host = address
	return nil
}

func pinSpeedtestServers(servers speedtest.Servers, value string) speedtest.Servers {
	network, err := model.NormalizeNetwork(value)
	if err != nil || network == model.NetworkAuto {
		return servers
	}
	pinned := make(speedtest.Servers, 0, len(servers))
	for _, server := range servers {
		if err := pinSpeedtestServer(server, network); err != nil {
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
	testUrl := baseUrl + "https://raw.githubusercontent.com/spiritLHLS/ecs/main/back/test"
	client := requestClientForNetwork(network, 6*time.Second)

	resp, err := client.R().Get(testUrl)
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
	return getDataWithNetwork(endpoint, "")
}

func getDataWithNetwork(endpoint, network string) string {
	client := requestClientForNetwork(network, 10*time.Second)
	client.R().
		SetRetryCount(2).
		SetRetryBackoffInterval(1*time.Second, 5*time.Second).
		SetRetryFixedInterval(2 * time.Second)
	if model.EnableLoger {
		InitLogger()
		defer Logger.Sync()
	}

	// First, find an available CDN
	var availableCdn string
	for _, baseUrl := range model.CdnList {
		if checkCDNWithNetwork(baseUrl, network) {
			availableCdn = baseUrl
			if model.EnableLoger {
				Logger.Info(fmt.Sprintf("CDN available: %s", baseUrl))
			}
			break
		}
		time.Sleep(500 * time.Millisecond)
	}

	if availableCdn == "" {
		if model.EnableLoger {
			Logger.Info("No CDN available, trying direct access")
		}
		// Try direct access without CDN
		resp, err := client.R().Get(endpoint)
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
	resp, err := client.R().Get(url)
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

func parseDataFromID(data, url string) speedtest.Servers {
	return parseDataFromIDWithClient(data, url, speedtestClient)
}

func parseDataFromIDWithClient(data, url string, client *speedtest.Speedtest) speedtest.Servers {
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
		serverPtr, errFetch := client.FetchServerByID(id)
		if errFetch != nil {
			if model.EnableLoger {
				Logger.Info(fmt.Sprintf("Error fetching server by ID %s: %v", id, errFetch))
			}
			continue
		}
		if serverPtr == nil {
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
	headers1 := []string{"位置", "上传速度", "下载速度", "延迟", "丢包率"}
	headers2 := []string{"Location", "Upload Speed", "Download Speed", "Latency", "PacketLoss"}
	if language == "zh" {
		for index, header := range headers1 {
			if index == 0 {
				fmt.Print(" ")
			}
			fmt.Print(formatString(header, 16))
		}
		fmt.Println()
	} else if language == "en" {
		for index, header := range headers2 {
			if index == 0 {
				fmt.Print(" ")
			}
			fmt.Print(formatString(header, 16))
		}
		fmt.Println()
	}
}
