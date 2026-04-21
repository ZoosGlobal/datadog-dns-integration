// collector/process.go
// Zoos Global — Microsoft DNS Monitor for Datadog
// https://www.zoosglobal.com
//
//go:build windows
// +build windows

package collector

import (
	"github.com/ZoosGlobal/datadog-dns-integration/statsd"
)

// CollectProcess reads dns.exe process metrics from WMI.
// Host-level CPU and memory are already collected by the Datadog Agent —
// these metrics are scoped to the dns.exe process only.
func CollectProcess(client *statsd.Client, tags []string) []string {
	var lines []string
	ptags := append(tags, "category:process", "process:dns")

	// Working set, virtual size, thread count, handle count
	var procs []Win32_Process
	q := `SELECT Name, WorkingSetSize, VirtualSize, ThreadCount, HandleCount, ReadOperationCount, WriteOperationCount FROM Win32_Process WHERE Name = 'dns.exe'`
	if err := queryWMI(q, &procs); err == nil && len(procs) > 0 {
		p := procs[0]
		wsMB := float64(p.WorkingSetSize) / 1024 / 1024
		vsMB := float64(p.VirtualSize) / 1024 / 1024

		lines = append(lines,
			client.Line("dns.process.working_set_mb", wsMB, "g", append(ptags, "memory_source:working_set")),
			client.Line("dns.process.virtual_mem_mb", vsMB, "g", append(ptags, "memory_source:virtual")),
			client.Line("dns.process.thread_count", float64(p.ThreadCount), "g", ptags),
			client.Line("dns.process.handle_count", float64(p.HandleCount), "g", ptags),
			client.Line("dns.process.io_read_ops_total", float64(p.ReadOperationCount), "c", ptags),
			client.Line("dns.process.io_write_ops_total", float64(p.WriteOperationCount), "c", ptags),
		)
	}

	// CPU % scoped to dns process (via PerfMon)
	var cpuProcs []Win32_PerfFormattedData_PerfProc_Process
	cpuQ := `SELECT Name, PercentProcessorTime FROM Win32_PerfFormattedData_PerfProc_Process WHERE Name = 'dns'`
	if err := queryWMI(cpuQ, &cpuProcs); err == nil && len(cpuProcs) > 0 {
		lines = append(lines,
			client.Line("dns.process.cpu_pct", float64(cpuProcs[0].PercentProcessorTime), "g", ptags),
		)
	}

	return lines
}
