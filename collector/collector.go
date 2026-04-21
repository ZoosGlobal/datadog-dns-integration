// collector/collector.go
// Zoos Global — Microsoft DNS Monitor for Datadog
// https://www.zoosglobal.com
//
//go:build windows
// +build windows

package collector

import (
	"fmt"
	"log"
	"os"
	"time"

	"github.com/ZoosGlobal/datadog-dns-integration/config"
	"github.com/ZoosGlobal/datadog-dns-integration/statsd"
)

// Run executes one full collection cycle and flushes all metrics to DogStatsD.
func Run(cfg *config.Config) (int, error) {
	hostname, _ := os.Hostname()

	// Build global tags
	globalTags := []string{
		fmt.Sprintf("env:%s", cfg.Env),
		fmt.Sprintf("host:%s", hostname),
		"role:dns",
		fmt.Sprintf("dns_server:%s", hostname),
	}
	globalTags = append(globalTags, cfg.GlobalTags...)

	// Connect to DogStatsD
	client, err := statsd.New(cfg.StatsDHost, cfg.StatsDPort, globalTags)
	if err != nil {
		return 0, fmt.Errorf("connecting to DogStatsD: %w", err)
	}
	defer client.Close()

	// Auto-detect forwarders if none configured
	forwarderIPs := cfg.ForwarderIPs
	if len(forwarderIPs) == 0 {
		forwarderIPs = DetectForwarders()
	}

	t0 := time.Now()
	var allLines []string

	// ── 1. Service health ────────────────────────────────────────────────────
	svcLines := CollectService(client, nil)
	allLines = append(allLines, svcLines...)
	log.Printf("[dns-monitor] service metrics: %d", len(svcLines))
	for _, l := range svcLines { log.Println(l) }

	// ── 2. Perfmon counters via PDH ──────────────────────────────────────────
	perfLines := CollectPerfmon(client, nil)
	allLines = append(allLines, perfLines...)
	log.Printf("[dns-monitor] perfmon metrics: %d", len(perfLines))

	// ── 3. Forwarder availability ────────────────────────────────────────────
	fwdLines := CollectForwarders(client, nil, forwarderIPs, cfg.ForwarderProbeDomain, cfg.ForwarderTimeoutSec)
	allLines = append(allLines, fwdLines...)
	log.Printf("[dns-monitor] forwarders metrics: %d", len(fwdLines))
	for _, l := range fwdLines { log.Println(l) }

	// ── 4. Resolution latency ────────────────────────────────────────────────
	resLines := CollectResolution(client, nil, cfg.ResolutionProbeDomain, cfg.ResolutionProbeWarnMs, cfg.ResolutionProbeCritMs)
	allLines = append(allLines, resLines...)
	log.Printf("[dns-monitor] resolution metrics: %d", len(resLines))
	for _, l := range resLines { log.Println(l) }

	// ── 5. Zone health ───────────────────────────────────────────────────────
	zoneLines := CollectZones(client, nil)
	allLines = append(allLines, zoneLines...)
	log.Printf("[dns-monitor] zones metrics: %d", len(zoneLines))
	for _, l := range zoneLines { log.Println(l) }

	// ── 6. Process metrics ───────────────────────────────────────────────────
	procLines := CollectProcess(client, nil)
	allLines = append(allLines, procLines...)
	log.Printf("[dns-monitor] process metrics: %d", len(procLines))
	for _, l := range procLines { log.Println(l) }

	// ── Self-monitoring ───────────────────────────────────────────────────────
	collectMs := time.Since(t0).Seconds() * 1000
	monLines := []string{
		client.Line("dns.monitor.collection_duration_ms", collectMs, "g", []string{"category:monitor"}),
		client.Line("dns.monitor.metrics_emitted", float64(len(allLines)+2), "g", []string{"category:monitor"}),
	}
	allLines = append(allLines, monLines...)
	for _, l := range monLines { log.Println(l) }

	// ── Flush all to DogStatsD ────────────────────────────────────────────────
	client.Flush(allLines)

	log.Printf("[dns-monitor] cycle complete | metrics:%d | duration:%.0fms | statsd:%s:%d",
		len(allLines), collectMs, cfg.StatsDHost, cfg.StatsDPort)

	return len(allLines), nil
}
