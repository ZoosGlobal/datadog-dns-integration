// collector/zones.go
// Zoos Global — Microsoft DNS Monitor for Datadog
// https://www.zoosglobal.com
//
// Zone metrics via PowerShell Get-DnsServerZone — more reliable than
// MicrosoftDNS WMI namespace which requires DNS Admin rights.
//
//go:build windows
// +build windows

package collector

import (
	"encoding/json"
	"log"
	"os/exec"
	"strings"
	"time"

	"github.com/ZoosGlobal/datadog-dns-integration/statsd"
)

type psZone struct {
	ZoneName            string `json:"ZoneName"`
	ZoneType            string `json:"ZoneType"`
	IsReverseLookupZone bool   `json:"IsReverseLookupZone"`
	IsPaused            bool   `json:"IsPaused"`
	IsAutoCreated       bool   `json:"IsAutoCreated"`
	IsDsIntegrated      bool   `json:"IsDsIntegrated"`
	IsSigned            bool   `json:"IsSigned"`
}

// getZonesViaPowerShell queries zone info via a brief PowerShell subprocess.
// This avoids the MicrosoftDNS WMI namespace permission requirement.
func getZonesViaPowerShell() ([]psZone, error) {
	script := `
Get-DnsServerZone -ErrorAction SilentlyContinue |
  Where-Object { -not $_.IsAutoCreated } |
  Select-Object ZoneName,ZoneType,IsReverseLookupZone,IsPaused,IsAutoCreated,IsDsIntegrated,IsSigned |
  ConvertTo-Json -Compress
`
	ctx, cancel := time.AfterFunc(10*time.Second, func() {})
	defer cancel()
	_ = ctx

	cmd := exec.Command("powershell.exe",
		"-NonInteractive", "-NoProfile", "-ExecutionPolicy", "Bypass",
		"-Command", strings.TrimSpace(script))

	out, err := cmd.Output()
	if err != nil {
		return nil, err
	}

	raw := strings.TrimSpace(string(out))
	if raw == "" || raw == "null" {
		return nil, nil
	}

	// PowerShell returns a single object (not array) when there is only one zone
	var zones []psZone
	if raw[0] == '[' {
		if err := json.Unmarshal(out, &zones); err != nil {
			return nil, err
		}
	} else {
		var single psZone
		if err := json.Unmarshal(out, &single); err != nil {
			return nil, err
		}
		zones = []psZone{single}
	}

	return zones, nil
}

// CollectZones reads DNS zone information and emits dns.zones.* metrics.
func CollectZones(client *statsd.Client, tags []string) []string {
	var lines []string
	ztags := append(tags, "category:zones")

	zones, err := getZonesViaPowerShell()
	if err != nil {
		log.Printf("[dns-monitor] zone query failed: %v", err)
		lines = append(lines, client.Line("dns.zones.total_count", 0, "g", ztags))
		return lines
	}

	total, primary, secondary, stub, adIntegrated, signed := 0, 0, 0, 0, 0, 0
	forwardCount, reverseCount := 0, 0

	for _, z := range zones {
		if z.ZoneName == "" {
			continue
		}
		total++

		if z.IsReverseLookupZone {
			reverseCount++
		} else {
			forwardCount++
		}

		switch z.ZoneType {
		case "Primary":
			primary++
		case "Secondary":
			secondary++
		case "Stub":
			stub++
		}

		if z.IsDsIntegrated {
			adIntegrated++
		}
		if z.IsSigned {
			signed++
		}

		// Per-zone metrics
		perTags := append(tags, "category:zones",
			"zone:"+z.ZoneName,
			"zone_type:"+strings.ToLower(z.ZoneType))

		isPaused := 0.0
		if z.IsPaused {
			isPaused = 1
		}
		isDsInt := 0.0
		if z.IsDsIntegrated {
			isDsInt = 1
		}
		isSigned := 0.0
		if z.IsSigned {
			isSigned = 1
		}

		lines = append(lines,
			client.Line("dns.zones.is_paused", isPaused, "g", perTags),
			client.Line("dns.zones.ad_integrated", isDsInt, "g", perTags),
			client.Line("dns.zones.dnssec_signed", isSigned, "g", perTags),
		)
	}

	lines = append(lines,
		client.Line("dns.zones.total_count", float64(total), "g", ztags),
		client.Line("dns.zones.forward_count", float64(forwardCount), "g", ztags),
		client.Line("dns.zones.reverse_count", float64(reverseCount), "g", ztags),
		client.Line("dns.zones.primary_count", float64(primary), "g", ztags),
		client.Line("dns.zones.secondary_count", float64(secondary), "g", ztags),
		client.Line("dns.zones.stub_count", float64(stub), "g", ztags),
		client.Line("dns.zones.ad_integrated_count", float64(adIntegrated), "g", ztags),
		client.Line("dns.zones.dnssec_signed_count", float64(signed), "g", ztags),
	)

	return lines
}
