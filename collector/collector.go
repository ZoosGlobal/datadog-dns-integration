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

// Run executes one full collection cycle:
//  1. Builds global tags (env, host, role:dns, dns_server)
//  2. Runs all collectors in sequence
//  3. Flushes all metric lines to DogStatsD in batched UDP packets
//  4. Returns the number of metrics emitted
func Run(cfg *config.Config) (int, error) {
	// Build global tags
	hostname, _ := os.Hostname()
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

	t0 := time.Now()
	var allLines []string

	// ── 1. Service health ────────────────────────────────────────────────────
	allLines = append(allLines, CollectService(client, nil)...)

	// ── 2. Perfmon counters ──────────────────────────────────────────────────
	allLines = append(allLines, CollectPerfmon(client, nil)...)

	// ── 3. Forwarder availability ────────────────────────────────────────────
	allLines = append(allLines, CollectForwarders(
		client, nil,
		cfg.ForwarderIPs,
		cfg.ForwarderProbeDomain,
		cfg.ForwarderTimeoutSec,
	)...)

	// ── 4. Resolution latency ────────────────────────────────────────────────
	allLines = append(allLines, CollectResolution(
		client, nil,
		cfg.ResolutionProbeDomain,
		cfg.ResolutionProbeWarnMs,
		cfg.ResolutionProbeCritMs,
	)...)

	// ── 5. Zone health ───────────────────────────────────────────────────────
	allLines = append(allLines, CollectZones(client, nil)...)

	// ── 6. Process metrics ───────────────────────────────────────────────────
	allLines = append(allLines, CollectProcess(client, nil)...)

	// ── Self-monitoring: collection duration ─────────────────────────────────
	collectMs := time.Since(t0).Seconds() * 1000
	allLines = append(allLines, client.Line("dns.monitor.collection_duration_ms", collectMs, "g",
		[]string{"category:monitor"}))
	allLines = append(allLines, client.Line("dns.monitor.metrics_emitted", float64(len(allLines)+1), "g",
		[]string{"category:monitor"}))

	// ── Flush all metrics in batched UDP packets ──────────────────────────────
	client.Flush(allLines)

	log.Printf("[dns-monitor] cycle complete | metrics:%d | duration:%.0fms | statsd:%s:%d",
		len(allLines), collectMs, cfg.StatsDHost, cfg.StatsDPort)

	return len(allLines), nil
}