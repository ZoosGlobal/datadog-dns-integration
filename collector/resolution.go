// collector/resolution.go
// Zoos Global — Microsoft DNS Monitor for Datadog
// https://www.zoosglobal.com
//
//go:build windows
// +build windows

package collector

import (
	"context"
	"net"
	"os"
	"time"

	"github.com/ZoosGlobal/datadog-dns-integration/statsd"
)

// resolveVia127 resolves a domain against 127.0.0.1:53 directly.
func resolveVia127(domain string, timeoutSec int) (bool, float64) {
	resolver := &net.Resolver{
		PreferGo: true,
		Dial: func(ctx context.Context, network, address string) (net.Conn, error) {
			d := net.Dialer{Timeout: time.Duration(timeoutSec) * time.Second}
			return d.DialContext(ctx, "udp", "127.0.0.1:53")
		},
	}

	ctx, cancel := context.WithTimeout(context.Background(), time.Duration(timeoutSec)*time.Second)
	defer cancel()

	t0 := time.Now()
	addrs, err := resolver.LookupHost(ctx, domain)
	elapsed := time.Since(t0).Seconds() * 1000
	_ = addrs
	return err == nil, elapsed
}

// CollectResolution measures DNS resolution latency against 127.0.0.1:53
// and emits dns.resolution.* metrics.
//
// Metrics added vs v1:
//   - dns.resolution.internal_probe_targets — count of active probe targets
//     (tells you if zone discovery is populating targets correctly)
func CollectResolution(client *statsd.Client, tags []string, probeDomain string, warnMs, critMs float64) []string {
	var lines []string

	// Count active probe targets — always 2 (baseline + external) when both configured
	probeTargetCount := 0

	// BASELINE: always probe the server's own hostname against 127.0.0.1:53
	// Most reliable signal — guaranteed resolvable if DNS is working at all
	hostname, _ := os.Hostname()
	if hostname != "" {
		probeTargetCount++
		rtags := append(tags, "category:resolution", "probe_scope:baseline", "zone:_baseline")

		ok, ms := resolveVia127(hostname, 5)

		status := 0.0
		if ok {
			status = 1
		}

		scStatus := 2.0 // CRITICAL — probe failed or too slow
		if ok {
			if ms <= warnMs {
				scStatus = 0 // OK
			} else if ms <= critMs {
				scStatus = 1 // WARNING
			}
		}

		lines = append(lines,
			client.Line("dns.resolution.status",        status,   "g", rtags),
			client.Line("dns.resolution.latency_ms",    ms,       "d", rtags),
			client.Line("dns.resolution.service_check", scStatus, "g", rtags),
		)
	}

	// EXTERNAL: configured probe domain (e.g. www.google.com)
	if probeDomain != "" && probeDomain != hostname {
		probeTargetCount++
		etags := append(tags, "category:resolution", "probe_scope:external")

		ok, ms := resolveVia127(probeDomain, 5)

		status := 0.0
		if ok {
			status = 1
		}

		lines = append(lines,
			client.Line("dns.resolution.status",     status, "g", etags),
			client.Line("dns.resolution.latency_ms", ms,     "d", etags),
		)
	}

	// Probe target count — confirms zone discovery is working
	// If this drops to 0, the resolution collector is not probing anything
	lines = append(lines,
		client.Line("dns.resolution.internal_probe_targets",
			float64(probeTargetCount), "g",
			append(tags, "category:resolution")),
	)

	return lines
}