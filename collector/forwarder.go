// collector/forwarder.go
// Zoos Global — Microsoft DNS Monitor for Datadog
// https://www.zoosglobal.com
//
// Forwarder availability is tested by sending a real DNS query
// directly to each forwarder IP via UDP/53 and measuring RTT.
// NXDOMAIN = forwarder UP. Timeout/SERVFAIL = forwarder DOWN.
//
//go:build windows
// +build windows

package collector

import (
	"encoding/binary"
	"fmt"
	"math"
	"math/rand"
	"net"
	"os/exec"
	"strings"
	"time"

	"github.com/ZoosGlobal/datadog-dns-integration/statsd"
)

// buildDNSQuery constructs a minimal DNS A-record query packet.
func buildDNSQuery(domain string, txID uint16) []byte {
	buf := make([]byte, 0, 512)
	header := make([]byte, 12)
	binary.BigEndian.PutUint16(header[0:], txID)
	binary.BigEndian.PutUint16(header[2:], 0x0100)
	binary.BigEndian.PutUint16(header[4:], 1)
	buf = append(buf, header...)
	for _, label := range strings.Split(strings.TrimSuffix(domain, "."), ".") {
		buf = append(buf, byte(len(label)))
		buf = append(buf, []byte(label)...)
	}
	buf = append(buf, 0x00, 0x00, 0x01, 0x00, 0x01)
	return buf
}

// isValidDNSResponse returns true if response is valid and forwarder is working.
// NXDOMAIN (RCODE=3) is treated as UP — forwarder responded correctly.
func isValidDNSResponse(data []byte, txID uint16) bool {
	if len(data) < 12 {
		return false
	}
	respID := binary.BigEndian.Uint16(data[0:])
	flags  := binary.BigEndian.Uint16(data[2:])
	qr     := (flags >> 15) & 0x1
	rcode  := flags & 0x000F
	return qr == 1 && respID == txID && rcode != 2
}

// randomLabel generates a random 12-char alphanumeric string.
func randomLabel(n int) string {
	const chars = "abcdefghijklmnopqrstuvwxyz0123456789"
	b := make([]byte, n)
	for i := range b {
		b[i] = chars[rand.Intn(len(chars))]
	}
	return string(b)
}

// probeForwarder sends a DNS query directly to forwarder IP and measures RTT.
func probeForwarder(ip, baseDomain string, timeoutSec int) (bool, float64) {
	txID       := uint16(rand.Intn(0xFFFF))
	probeDomain := fmt.Sprintf("%s.%s", randomLabel(12), baseDomain)
	query      := buildDNSQuery(probeDomain, txID)

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
	elapsedMs := time.Since(t0).Seconds() * 1000
	elapsedMs  = math.Round(elapsedMs*100) / 100

	if err != nil {
		return false, elapsedMs
	}
	return isValidDNSResponse(resp[:n], txID), elapsedMs
}

// subnetTag converts IP to /24 for bounded-cardinality tagging.
func subnetTag(ip string) string {
	parts := strings.Split(ip, ".")
	if len(parts) == 4 {
		return fmt.Sprintf("%s.%s.%s.0/24", parts[0], parts[1], parts[2])
	}
	return ip
}

// forwarderServerInfo holds server-level forwarder config from PowerShell.
type forwarderServerInfo struct {
	useRootHint bool
	timeoutSec  int
}

// getForwarderServerInfo reads UseRootHint and Timeout from Get-DnsServerForwarder.
func getForwarderServerInfo() forwarderServerInfo {
	script := `
$f = Get-DnsServerForwarder -ErrorAction SilentlyContinue
if ($null -eq $f) { Write-Output "0|0"; exit }
$rh = if ($f.UseRootHint) { 1 } else { 0 }
$to = 0
try { $to = [int]$f.Timeout.TotalSeconds } catch { try { $to = [int]$f.Timeout } catch {} }
Write-Output "$rh|$to"
`
	cmd := exec.Command("powershell.exe",
		"-NonInteractive", "-NoProfile", "-ExecutionPolicy", "Bypass",
		"-Command", strings.TrimSpace(script))

	out, err := cmd.Output()
	if err != nil {
		return forwarderServerInfo{}
	}

	parts := strings.Split(strings.TrimSpace(string(out)), "|")
	if len(parts) != 2 {
		return forwarderServerInfo{}
	}

	useRootHint := strings.TrimSpace(parts[0]) == "1"
	timeoutSec  := 0
	fmt.Sscanf(strings.TrimSpace(parts[1]), "%d", &timeoutSec)

	return forwarderServerInfo{useRootHint: useRootHint, timeoutSec: timeoutSec}
}

// CollectForwarders probes each forwarder and emits dns.forwarders.* metrics.
//
// Metrics added vs v1:
//   - dns.forwarders.use_root_hint      — config compliance signal
//   - dns.forwarders.timeout_sec        — forwarder timeout setting (SLA context)
//   - dns.forwarders.best_probe_latency_ms — fastest forwarder RTT this cycle
func CollectForwarders(client *statsd.Client, tags []string, forwarderIPs []string, probeDomain string, timeoutSec int) []string {
	var lines []string
	ftags := append(tags, "category:forwarders")

	// Server-level forwarder config
	svrInfo := getForwarderServerInfo()

	useRootHint := 0.0
	if svrInfo.useRootHint {
		useRootHint = 1
	}

	lines = append(lines,
		client.Line("dns.forwarders.configured_count", float64(len(forwarderIPs)), "g", ftags),
		client.Line("dns.forwarders.use_root_hint",    useRootHint,                "g", ftags),
		client.Line("dns.forwarders.timeout_sec",      float64(svrInfo.timeoutSec),"g", ftags),
	)

	if len(forwarderIPs) == 0 {
		lines = append(lines,
			client.Line("dns.forwarders.available_count", 0, "g", ftags),
			client.Line("dns.forwarders.degraded_count",  0, "g", ftags),
		)
		return lines
	}

	availCount := 0
	bestLatency := math.MaxFloat64
	bestLatencyFound := false

	for _, ip := range forwarderIPs {
		subnet := subnetTag(ip)
		itags  := append(ftags,
			fmt.Sprintf("forwarder_ip:%s", ip),
			fmt.Sprintf("forwarder_subnet:%s", subnet),
		)

		// PRIMARY: DNS resolution probe (cache-busting random subdomain)
		up, latencyMs := probeForwarder(ip, probeDomain, timeoutSec)

		availVal := 0.0
		if up {
			availVal = 1
			availCount++
			if latencyMs < bestLatency {
				bestLatency      = latencyMs
				bestLatencyFound = true
			}
		}

		lines = append(lines,
			client.Line("dns.forwarders.availability",    availVal,  "g", itags),
			client.Line("dns.forwarders.probe_latency_ms",latencyMs, "d", itags),
		)

		// SECONDARY: TCP/53 reachability
		tcpUp := 0.0
		conn, err := net.DialTimeout("tcp", fmt.Sprintf("%s:53", ip), 3*time.Second)
		if err == nil {
			conn.Close()
			tcpUp = 1
		}
		lines = append(lines, client.Line("dns.forwarders.tcp_reachable", tcpUp, "g", itags))

		// Resolver-broken: TCP up but DNS failing
		resolverBroken := 0.0
		if !up && tcpUp == 1 {
			resolverBroken = 1
		}
		lines = append(lines, client.Line("dns.forwarders.resolver_broken", resolverBroken, "g", itags))
	}

	// Fleet-level summary
	lines = append(lines,
		client.Line("dns.forwarders.available_count", float64(availCount),                         "g", ftags),
		client.Line("dns.forwarders.degraded_count",  float64(len(forwarderIPs)-availCount),       "g", ftags),
		client.Line("dns.forwarders.availability_pct",float64(availCount)/float64(len(forwarderIPs))*100, "g", ftags),
	)

	// Best RTT across all UP forwarders — used for SLA reporting
	if bestLatencyFound {
		lines = append(lines,
			client.Line("dns.forwarders.best_probe_latency_ms", bestLatency, "d", ftags),
		)
	}

	return lines
}