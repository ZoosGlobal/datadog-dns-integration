// main.go
// Zoos Global — Microsoft DNS Monitor for Datadog
// https://www.zoosglobal.com
//
// dns-monitor.exe
//
// The binary is designed to run-once per invocation.
// The Datadog Agent's checks.d Python wrapper calls it every interval.
// The Agent owns scheduling — the binary owns collection.
//
// Usage:
//   dns-monitor.exe                         # run with default config
//   dns-monitor.exe --config path\to\config.yaml
//   dns-monitor.exe --version
//
//go:build windows
// +build windows

package main

import (
	"flag"
	"fmt"
	"log"
	"os"

	"github.com/ZoosGlobal/datadog-dns-integration/collector"
	"github.com/ZoosGlobal/datadog-dns-integration/config"
)

const (
	version = "1.0.0"
	product = "Zoos Global — Microsoft DNS Monitor for Datadog"
	website = "https://www.zoosglobal.com"
)

func main() {
	var (
		configPath  = flag.String("config", "", "Path to config.yaml (default: config.yaml next to binary)")
		showVersion = flag.Bool("version", false, "Print version and exit")
	)
	flag.Parse()

	if *showVersion {
		fmt.Printf("%s\nVersion : %s\nWebsite : %s\n", product, version, website)
		os.Exit(0)
	}

	// Load configuration
	cfg, err := config.Load(*configPath)
	if err != nil {
		log.Fatalf("[dns-monitor] config error: %v", err)
	}

	// Run one collection cycle — push all metrics to DogStatsD — exit
	count, err := collector.Run(cfg)
	if err != nil {
		log.Fatalf("[dns-monitor] collection failed: %v", err)
	}

	_ = count
	os.Exit(0)
}