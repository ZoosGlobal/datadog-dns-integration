// collector/process.go
// Zoos Global — Microsoft DNS Monitor for Datadog
// https://www.zoosglobal.com
//
//go:build windows
// +build windows

package collector

import (
	"log"
	"math"
	"time"

	"github.com/ZoosGlobal/datadog-dns-integration/statsd"
)

// parseWMIDatetime parses a WMI datetime string (yyyymmddHHMMSS.ffffff+zzz)
// and returns the corresponding time.Time.
func parseWMIDatetime(s string) (time.Time, error) {
	// WMI format: "20260421174512.000000+330"
	// We need at least 14 chars for yyyymmddHHMMSS
	if len(s) < 14 {
		return time.Time{}, nil
	}
	return time.ParseInLocation("20060102150405", s[:14], time.Local)
}

// CollectProcess reads dns.exe process metrics from WMI.
// Host-level CPU and memory are already collected by the Datadog Agent —
// these metrics are scoped to dns.exe only.
//
// Metrics added vs v1 spec:
//   - dns.process.private_mem_mb   — private committed pages (memory leak signal)
//   - dns.process.uptime_minutes   — time since dns.exe last started (restart detection)
func CollectProcess(client *statsd.Client, tags []string) []string {
	var lines []string
	ptags := append(tags, "category:process", "process:dns")

	var procs []Win32_Process
	q := `SELECT Name, WorkingSetSize, VirtualSize, PrivatePageCount, ThreadCount, HandleCount, ReadOperationCount, WriteOperationCount, CreationDate FROM Win32_Process WHERE Name = 'dns.exe'`
	if err := queryWMI(q, &procs); err == nil && len(procs) > 0 {
		p := procs[0]

		wsMB      := float64(p.WorkingSetSize)   / 1024 / 1024
		vsMB      := float64(p.VirtualSize)       / 1024 / 1024
		privMB    := float64(p.PrivatePageCount)  / 1024 / 1024

		lines = append(lines,
			client.Line("dns.process.working_set_mb",    wsMB,   "g", append(ptags, "memory_source:working_set")),
			client.Line("dns.process.virtual_mem_mb",    vsMB,   "g", append(ptags, "memory_source:virtual")),
			client.Line("dns.process.private_mem_mb",    privMB, "g", append(ptags, "memory_source:private")),
			client.Line("dns.process.thread_count",      float64(p.ThreadCount),      "g", ptags),
			client.Line("dns.process.handle_count",      float64(p.HandleCount),      "g", ptags),
			client.Line("dns.process.io_read_ops_total", float64(p.ReadOperationCount),  "c", ptags),
			client.Line("dns.process.io_write_ops_total",float64(p.WriteOperationCount), "c", ptags),
		)

		// Uptime in minutes — detect unexpected restarts
		if p.CreationDate != "" {
			startTime, err := parseWMIDatetime(p.CreationDate)
			if err == nil && !startTime.IsZero() {
				uptimeMin := math.Round(time.Since(startTime).Minutes()*100) / 100
				lines = append(lines,
					client.Line("dns.process.uptime_minutes", uptimeMin, "g", ptags),
				)
			} else if err != nil {
				log.Printf("[dns-monitor] uptime parse failed: %v", err)
			}
		}
	}

	// CPU % scoped to dns process (via WMI PerfMon)
	var cpuProcs []Win32_PerfFormattedData_PerfProc_Process
	cpuQ := `SELECT Name, PercentProcessorTime FROM Win32_PerfFormattedData_PerfProc_Process WHERE Name = 'dns'`
	if err := queryWMI(cpuQ, &cpuProcs); err == nil && len(cpuProcs) > 0 {
		lines = append(lines,
			client.Line("dns.process.cpu_pct", float64(cpuProcs[0].PercentProcessorTime), "g", ptags),
		)
	}

	return lines
}