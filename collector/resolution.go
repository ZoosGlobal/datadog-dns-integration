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

// resolveVia127 resolves a domain against 127.0.0.1 (the local DNS server)
// and returns latency in milliseconds and whether it succeeded.
func resolveVia127(domain string, timeoutSec int) (bool, float64) {
	dialer := &net.Dialer{
		Timeout: time.Duration(timeoutSec) * time.Second,
	}

	resolver := &net.Resolver{
		PreferGo: true,
		Dial: func(ctx context.Context, network, address string) (net.Conn, error) {
			return dialer.DialContext(ctx, "udp", "127.0.0.1:53")
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

// CollectResolution measures DNS resolution latency against the local server
// and emits dns.resolution.* metrics.
func CollectResolution(client *statsd.Client, tags []string, probeDomain string, warnMs, critMs float64) []string {
	var lines []string
	rtags := append(tags, "category:resolution", "probe_scope:baseline", "zone:_baseline")

	// Baseline: always probe server's own hostname — guaranteed to resolve
	hostname, _ := os.Hostname()
	if hostname != "" {
		ok, ms := resolveVia127(hostname, 5)
		status := 0.0
		if ok {
			status = 1
		}
		lines = append(lines,
			client.Line("dns.resolution.status", status, "g", rtags),
			client.Line("dns.resolution.latency_ms", ms, "d", rtags),
		)

		// Service check thresholds
		scStatus := 2.0 // CRITICAL
		if ok {
			if ms <= warnMs {
				scStatus = 0 // OK
			} else if ms <= critMs {
				scStatus = 1 // WARNING
			}
		}
		lines = append(lines, client.Line("dns.resolution.service_check", scStatus, "g", rtags))
	}

	// External probe domain (if configured)
	if probeDomain != "" && probeDomain != hostname {
		etags := append(tags, "category:resolution", "probe_scope:external")
		ok, ms := resolveVia127(probeDomain, 5)
		status := 0.0
		if ok {
			status = 1
		}
		lines = append(lines,
			client.Line("dns.resolution.status", status, "g", etags),
			client.Line("dns.resolution.latency_ms", ms, "d", etags),
		)
	}

	return lines
}