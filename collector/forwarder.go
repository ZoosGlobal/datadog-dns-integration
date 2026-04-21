// collector/forwarder.go
// Zoos Global — Microsoft DNS Monitor for Datadog
// https://www.zoosglobal.com
//
// Forwarder availability is tested by sending a real DNS query
// directly to each forwarder IP via UDP/53 and measuring RTT.
// NXDOMAIN = forwarder is UP (it resolved correctly).
// Timeout / SERVFAIL / connection refused = forwarder is DOWN.
//
//go:build windows
// +build windows

package collector

import (
	"encoding/binary"
	"fmt"
	"math/rand"
	"net"
	"strings"
	"time"

	"github.com/ZoosGlobal/datadog-dns-integration/statsd"
)

// buildDNSQuery constructs a minimal DNS A-record query packet.
// Uses a random subdomain prefix to defeat forwarder caching.
func buildDNSQuery(domain string, txID uint16) []byte {
	buf := make([]byte, 0, 512)

	// Header: TX ID, flags (QR=0, RD=1), QDCOUNT=1, rest=0
	header := make([]byte, 12)
	binary.BigEndian.PutUint16(header[0:], txID)
	binary.BigEndian.PutUint16(header[2:], 0x0100) // standard query, recursion desired
	binary.BigEndian.PutUint16(header[4:], 1)       // QDCOUNT=1
	buf = append(buf, header...)

	// Question: encode domain as length-prefixed labels
	for _, label := range strings.Split(strings.TrimSuffix(domain, "."), ".") {
		buf = append(buf, byte(len(label)))
		buf = append(buf, []byte(label)...)
	}
	buf = append(buf, 0x00)          // root label
	buf = append(buf, 0x00, 0x01)    // QTYPE = A
	buf = append(buf, 0x00, 0x01)    // QCLASS = IN
	return buf
}

// isValidDNSResponse checks that the response matches our query ID
// and has the QR bit set (is a response). NXDOMAIN (RCODE=3) is treated
// as UP — the forwarder resolved and responded correctly.
func isValidDNSResponse(data []byte, txID uint16) bool {
	if len(data) < 12 {
		return false
	}
	respID := binary.BigEndian.Uint16(data[0:])
	flags := binary.BigEndian.Uint16(data[2:])
	qr := (flags >> 15) & 0x1
	rcode := flags & 0x000F
	// QR=1 (response), ID matches, RCODE != SERVFAIL (2)
	return qr == 1 && respID == txID && rcode != 2
}

// randomLabel generates a random lowercase alphanumeric string.
func randomLabel(n int) string {
	const chars = "abcdefghijklmnopqrstuvwxyz0123456789"
	b := make([]byte, n)
	for i := range b {
		b[i] = chars[rand.Intn(len(chars))]
	}
	return string(b)
}

// probeForwarder sends a DNS query directly to a forwarder IP and measures RTT.
func probeForwarder(ip, baseDomain string, timeoutSec int) (bool, float64) {
	txID := uint16(rand.Intn(0xFFFF))
	probeDomain := fmt.Sprintf("%s.%s", randomLabel(12), baseDomain)
	query := buildDNSQuery(probeDomain, txID)

	conn, err := net.DialTimeout("udp", fmt.Sprintf("%s:53", ip), time.Duration(timeoutSec)*time.Second)
	if err != nil {
		return false, 0
	}
	defer conn.Close()

	_ = conn.SetDeadline(time.Now().Add(time.Duration(timeoutSec) * time.Second))

	t0 := time.Now()
	if _, err := conn.Write(query); err != nil {
		return false, 0
	}

	resp := make([]byte, 512)
	n, err := conn.Read(resp)
	elapsed := time.Since(t0).Seconds() * 1000 // ms

	if err != nil {
		return false, elapsed
	}

	return isValidDNSResponse(resp[:n], txID), elapsed
}

// subnetTag converts an IP to /24 for bounded-cardinality tagging.
func subnetTag(ip string) string {
	parts := strings.Split(ip, ".")
	if len(parts) == 4 {
		return fmt.Sprintf("%s.%s.%s.0/24", parts[0], parts[1], parts[2])
	}
	return ip
}

// CollectForwarders probes each configured forwarder and emits
// dns.forwarders.* metrics.
func CollectForwarders(client *statsd.Client, tags []string, forwarderIPs []string, probeDomain string, timeoutSec int) []string {
	var lines []string
	ftags := append(tags, "category:forwarders")

	lines = append(lines, client.Line("dns.forwarders.configured_count", float64(len(forwarderIPs)), "g", ftags))

	if len(forwarderIPs) == 0 {
		lines = append(lines, client.Line("dns.forwarders.available_count", 0, "g", ftags))
		return lines
	}

	availCount := 0

	for _, ip := range forwarderIPs {
		subnet := subnetTag(ip)
		itags := append(ftags, fmt.Sprintf("forwarder_ip:%s", ip), fmt.Sprintf("forwarder_subnet:%s", subnet))

		up, latencyMs := probeForwarder(ip, probeDomain, timeoutSec)

		availVal := 0.0
		if up {
			availVal = 1
			availCount++
		}

		lines = append(lines,
			client.Line("dns.forwarders.availability", availVal, "g", itags),
			client.Line("dns.forwarders.probe_latency_ms", latencyMs, "d", itags),
		)

		// Secondary: TCP/53 reachability
		tcpUp := 0.0
		conn, err := net.DialTimeout("tcp", fmt.Sprintf("%s:53", ip), 3*time.Second)
		if err == nil {
			conn.Close()
			tcpUp = 1
		}
		lines = append(lines, client.Line("dns.forwarders.tcp_reachable", tcpUp, "g", itags))

		// Detect: TCP up but DNS resolution failing = resolver process broken
		resolverBroken := 0.0
		if !up && tcpUp == 1 {
			resolverBroken = 1
		}
		lines = append(lines, client.Line("dns.forwarders.resolver_broken", resolverBroken, "g", itags))
	}

	lines = append(lines, client.Line("dns.forwarders.available_count", float64(availCount), "g", ftags))
	lines = append(lines, client.Line("dns.forwarders.degraded_count", float64(len(forwarderIPs)-availCount), "g", ftags))

	// Fleet availability %
	if len(forwarderIPs) > 0 {
		pct := float64(availCount) / float64(len(forwarderIPs)) * 100
		lines = append(lines, client.Line("dns.forwarders.availability_pct", pct, "g", ftags))
	}

	return lines
}