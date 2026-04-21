// collector/perfmon.go
// Zoos Global — Microsoft DNS Monitor for Datadog
// https://www.zoosglobal.com
//
//go:build windows
// +build windows

package collector

import (
	"github.com/ZoosGlobal/datadog-dns-integration/statsd"
)

// CollectPerfmon reads DNS Server performance counters from WMI
// and emits dns.performance.* metrics.
func CollectPerfmon(client *statsd.Client, tags []string) []string {
	var lines []string

	var counters []Win32_PerfFormattedData_DNS_DNSServer
	q := `SELECT * FROM Win32_PerfFormattedData_DNS_DNSServer`
	if err := queryWMI(q, &counters); err != nil || len(counters) == 0 {
		return lines
	}

	c := counters[0]
	ptags := append(tags, "category:performance")

	type perfMetric struct {
		name  string
		value uint64
		mtype string
	}

	metrics := []perfMetric{
		{"dns.performance.queries_received_total", c.TotalQueryReceived, "c"},
		{"dns.performance.responses_sent_total", c.TotalResponseSent, "c"},
		{"dns.performance.udp_queries_total", c.UDPQueryReceived, "c"},
		{"dns.performance.tcp_queries_total", c.TCPQueryReceived, "c"},
		{"dns.performance.recursive_queries_total", c.RecursiveQueries, "c"},
		{"dns.performance.recursive_query_failures_total", c.RecursiveQueryFailure, "c"},
		{"dns.performance.recursive_query_timeouts_total", c.RecursiveTimeOut, "c"},
		{"dns.performance.dynamic_updates_total", c.DynamicUpdateReceived, "c"},
		{"dns.performance.secure_updates_total", c.SecureUpdateReceived, "c"},
		{"dns.performance.zone_transfer_requests_total", c.ZoneTransferRequestReceived, "c"},
		{"dns.performance.zone_transfer_success_total", c.ZoneTransferSuccess, "c"},
		{"dns.performance.zone_transfer_failures_total", c.ZoneTransferFailure, "c"},
		{"dns.performance.notify_sent_total", c.NotifySent, "c"},
		{"dns.performance.notify_received_total", c.NotifyReceived, "c"},
		{"dns.performance.unmatched_responses_total", c.UnmatchedResponsesReceived, "c"},
	}

	for _, m := range metrics {
		lines = append(lines, client.Line(m.name, float64(m.value), m.mtype, ptags))
	}

	return lines
}