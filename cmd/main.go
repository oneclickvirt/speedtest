package main

import "github.com/oneclickvirt/speedtest/sp"

func main() {
	sp.NearbySpeedTest("en")
	sp.CustomSpeedTest("https://raw.githubusercontent.com/spiritLHLS/speedtest.net-CN-ID/main/CN_Telecom.csv", 2)
}
