// collector/zones.go
// Zoos Global — Microsoft DNS Monitor for Datadog
// https://www.zoosglobal.com
//
//go:build windows
// +build windows

package collector

import (
	"fmt"

	"github.com/StackExchange/wmi"
	"github.com/ZoosGlobal/datadog-dns-integration/statsd"
)

// CollectZones reads zone information from the MicrosoftDNS WMI namespace
// and emits dns.zones.* metrics.
func CollectZones(client *statsd.Client, tags []string) []string {
	var lines []string
	ztags := append(tags, "category:zones")

	var zones []MicrosoftDNS_Zone
	// MicrosoftDNS namespace — separate from root/cimv2
	q := `SELECT Name, ZoneType, IsPaused, IsShutdown, DsIntegrated FROM MicrosoftDNS_Zone`
	if err := wmi.QueryNamespace(q, &zones, `root\MicrosoftDNS`); err != nil {
		// Fallback: emit zero — namespace may not be accessible without DNS Admin
		lines = append(lines, client.Line("dns.zones.total_count", 0, "g", ztags))
		return lines
	}

	total := 0
	primary := 0
	secondary := 0
	stub := 0
	adIntegrated := 0

	for _, z := range zones {
		// Skip auto-created zones (0-9.in-addr.arpa etc)
		if z.Name == "." || z.Name == "" {
			continue
		}
		total++

		// ZoneType: 0=Primary, 1=Secondary, 3=Stub
		switch z.ZoneType {
		case 0:
			primary++
		case 1:
			secondary++
		case 3:
			stub++
		}

		if z.DsIntegrated {
			adIntegrated++
		}

		// Per-zone metrics
		zname := fmt.Sprintf("zone:%s", z.Name)
		ztypeTag := "zone_type:primary"
		switch z.ZoneType {
		case 1:
			ztypeTag = "zone_type:secondary"
		case 3:
			ztypeTag = "zone_type:stub"
		}

		perTags := append(tags, "category:zones", zname, ztypeTag)
		isPaused := 0.0
		if z.IsPaused {
			isPaused = 1
		}
		isShutdown := 0.0
		if z.IsShutdown {
			isShutdown = 1
		}
		isDsInt := 0.0
		if z.DsIntegrated {
			isDsInt = 1
		}

		lines = append(lines,
			client.Line("dns.zones.is_paused", isPaused, "g", perTags),
			client.Line("dns.zones.is_shutdown", isShutdown, "g", perTags),
			client.Line("dns.zones.ad_integrated", isDsInt, "g", perTags),
		)
	}

	lines = append(lines,
		client.Line("dns.zones.total_count", float64(total), "g", ztags),
		client.Line("dns.zones.primary_count", float64(primary), "g", ztags),
		client.Line("dns.zones.secondary_count", float64(secondary), "g", ztags),
		client.Line("dns.zones.stub_count", float64(stub), "g", ztags),
		client.Line("dns.zones.ad_integrated_count", float64(adIntegrated), "g", ztags),
	)

	return lines
}