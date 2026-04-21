// collector/resolution.go
// Zoos Global — Microsoft DNS Monitor for Datadog
// https://www.zoosglobal.com
//
//go:build windows
// +build windows

package collector

import (
	"context"
	"encoding/binary"
	"fmt"
	"math/rand"
	"net"
	"strings"
	"time"

	"github.com/ZoosGlobal/datadog-dns-integration/statsd"
)

// resolveVia127 resolves a domain against 127.0.0.1:53 using net.Resolver.
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

// probeViaRawDNS sends a raw DNS UDP packet directly to 127.0.0.1:53
// and measures RTT. Uses a random subdomain to bust cache.
// This is the same technique used for forwarder probing — proven to work.
// Returns ok=true if server responded (even NXDOMAIN = server is UP).
func probeViaRawDNS(server, baseDomain string, timeoutSec int) (bool, float64) {
	txID := uint16(rand.Intn(0xFFFF))
	probeDomain := fmt.Sprintf("%s.%s", randomLabel(12), baseDomain)

	buf := make([]byte, 0, 512)
	header := make([]byte, 12)
	binary.BigEndian.PutUint16(header[0:], txID)
	binary.BigEndian.PutUint16(header[2:], 0x0100)
	binary.BigEndian.PutUint16(header[4:], 1)
	buf = append(buf, header...)
	for _, label := range strings.Split(strings.TrimSuffix(probeDomain, "."), ".") {
		buf = append(buf, byte(len(label)))
		buf = append(buf, []byte(label)...)
	}
	buf = append(buf, 0x00, 0x00, 0x01, 0x00, 0x01)

	conn, err := net.DialTimeout("udp", fmt.Sprintf("%s:53", server),
		time.Duration(timeoutSec)*time.Second)
	if err != nil {
		return false, 0
	}
	defer conn.Close()
	_ = conn.SetDeadline(time.Now().Add(time.Duration(timeoutSec) * time.Second))

	t0 := time.Now()
	if _, err := conn.Write(buf); err != nil {
		return false, 0
	}
	resp := make([]byte, 512)
	n, err := conn.Read(resp)
	elapsed := time.Since(t0).Seconds() * 1000

	if err != nil || n < 12 {
		return false, elapsed
	}
	respID := binary.BigEndian.Uint16(resp[0:])
	flags := binary.BigEndian.Uint16(resp[2:])
	qr := (flags >> 15) & 0x1
	rcode := flags & 0x000F
	// QR=1 (response) + ID matches + not SERVFAIL = server is UP
	return qr == 1 && respID == txID && rcode != 2, elapsed
}

// CollectResolution measures DNS resolution response time against 127.0.0.1:53.
//
// Baseline: raw UDP/53 probe against 127.0.0.1 — uses cache-busting random
// subdomain. NXDOMAIN = server UP and responding. Immune to missing A records.
//
// External: net.Resolver lookup of configured probe domain (e.g. www.google.com).
func CollectResolution(client *statsd.Client, tags []string, probeDomain string, warnMs, critMs float64) []string {
	var lines []string
	probeTargetCount := 0

	// BASELINE — raw UDP/53 probe against 127.0.0.1
	// Uses the same technique as forwarder probing — proven reliable.
	// Measures pure DNS server responsiveness, independent of zone content.
	{
		probeTargetCount++
		rtags := append(tags, "category:resolution", "probe_scope:baseline", "zone:_baseline")

		// Use probeDomain as the base for random subdomain generation
		// Falls back to "example.com" if probeDomain is empty
		base := probeDomain
		if base == "" {
			base = "example.com"
		}

		ok, ms := probeViaRawDNS("127.0.0.1", base, 5)

		status := 0.0
		if ok {
			status = 1
		}

		scStatus := 2.0 // CRITICAL
		if ok {
			if ms <= warnMs {
				scStatus = 0 // OK
			} else if ms <= critMs {
				scStatus = 1 // WARNING
			}
		}

		lines = append(lines,
			client.Line("dns.resolution.status", status, "g", rtags),
			client.Line("dns.resolution.latency_ms", ms, "d", rtags),
			client.Line("dns.resolution.service_check", scStatus, "g", rtags),
		)
	}

	// EXTERNAL — net.Resolver lookup of a real domain
	// Tests full recursive resolution through forwarders to the internet.
	if probeDomain != "" {
		probeTargetCount++
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

	// Active probe target count
	lines = append(lines,
		client.Line("dns.resolution.internal_probe_targets",
			float64(probeTargetCount), "g",
			append(tags, "category:resolution")),
	)

	return lines
}
