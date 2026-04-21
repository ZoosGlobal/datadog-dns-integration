// collector/forwarder_detect.go
// Zoos Global — Microsoft DNS Monitor for Datadog
// https://www.zoosglobal.com
//
// Auto-detects configured forwarder IPs from the local DNS server
// when none are specified in config.yaml.
//
//go:build windows
// +build windows

package collector

import (
	"log"
	"os/exec"
	"strings"
)

// DetectForwarders reads forwarder IPs from the local DNS server
// using PowerShell Get-DnsServerForwarder.
// Called at startup when config.ForwarderIPs is empty.
func DetectForwarders() []string {
	script := `
(Get-DnsServerForwarder -ErrorAction SilentlyContinue).IPAddress |
  ForEach-Object { $_.ToString() } |
  Where-Object { $_ -and $_.Trim() } |
  Select-Object -Unique
`
	cmd := exec.Command("powershell.exe",
		"-NonInteractive", "-NoProfile", "-ExecutionPolicy", "Bypass",
		"-Command", strings.TrimSpace(script))

	out, err := cmd.Output()
	if err != nil {
		log.Printf("[dns-monitor] forwarder auto-detect failed: %v", err)
		return nil
	}

	var ips []string
	for _, line := range strings.Split(strings.TrimSpace(string(out)), "\n") {
		ip := strings.TrimSpace(line)
		ip = strings.Trim(ip, "\r")
		if ip != "" && ip != "null" {
			ips = append(ips, ip)
		}
	}

	if len(ips) > 0 {
		log.Printf("[dns-monitor] auto-detected forwarders: %v", ips)
	} else {
		log.Printf("[dns-monitor] no forwarders configured on this DNS server")
	}

	return ips
}
