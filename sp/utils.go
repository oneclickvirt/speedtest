package sp

import (
	"bufio"
	"encoding/csv"
	"fmt"
	"io"
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
		MaxConnections: 8,
	}))

// checkCDN checks if a CDN is available by testing with a known test file
func checkCDN(baseUrl string) bool {
	testUrl := baseUrl + "https://raw.githubusercontent.com/spiritLHLS/ecs/main/back/test"
	client := req.C()
	client.SetTimeout(6 * time.Second)

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

	// First, find an available CDN
	var availableCdn string
	for _, baseUrl := range model.CdnList {
		if checkCDN(baseUrl) {
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
		target, errFetch := speedtestClient.CustomServer(customURL)
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
		serverPtr, errFetch := speedtestClient.FetchServerByID(id)
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
