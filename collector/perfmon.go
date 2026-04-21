// collector/perfmon.go
// Zoos Global — Microsoft DNS Monitor for Datadog
// https://www.zoosglobal.com
//
// Perfmon counters via PDH (Performance Data Helper) API.
// Win32_PerfFormattedData_DNS_DNSServer WMI class is not available on all
// Windows Server builds — PDH is the reliable alternative.
//
//go:build windows
// +build windows

package collector

import (
	"fmt"
	"log"
	"unsafe"

	"github.com/ZoosGlobal/datadog-dns-integration/statsd"
	"golang.org/x/sys/windows"
)

// PDH error codes
const (
	pdhNoError          = 0x00000000
	pdhMoreData         = 0x800007D2
	pdhInvalidData      = 0xC0000BC6
	pdhCstatusNoObject  = 0xC0000BB8
	pdhCstatusNoCounter = 0xC0000BB9
)

var (
	modpdh               = windows.NewLazySystemDLL("pdh.dll")
	procPdhOpenQuery     = modpdh.NewProc("PdhOpenQuery")
	procPdhAddCounter    = modpdh.NewProc("PdhAddEnglishCounterW")
	procPdhCollectData   = modpdh.NewProc("PdhCollectQueryData")
	procPdhGetFormValue  = modpdh.NewProc("PdhGetFormattedCounterValue")
	procPdhCloseQuery    = modpdh.NewProc("PdhCloseQuery")
)

type pdhFmtCounterValue struct {
	CStatus    uint32
	_          uint32 // padding
	DoubleValue float64
}

// pdhQuery wraps a PDH query handle for collecting DNS counters.
type pdhQuery struct {
	handle   uintptr
	counters map[string]uintptr
}

func newPdhQuery() (*pdhQuery, error) {
	var handle uintptr
	ret, _, _ := procPdhOpenQuery.Call(0, 0, uintptr(unsafe.Pointer(&handle)))
	if ret != pdhNoError {
		return nil, fmt.Errorf("PdhOpenQuery failed: 0x%X", ret)
	}
	return &pdhQuery{handle: handle, counters: make(map[string]uintptr)}, nil
}

func (q *pdhQuery) addCounter(path string) error {
	pathPtr, err := windows.UTF16PtrFromString(path)
	if err != nil {
		return err
	}
	var counterHandle uintptr
	ret, _, _ := procPdhAddCounter.Call(q.handle, uintptr(unsafe.Pointer(pathPtr)), 0, uintptr(unsafe.Pointer(&counterHandle)))
	if ret != pdhNoError {
		return fmt.Errorf("PdhAddEnglishCounterW(%s) failed: 0x%X", path, ret)
	}
	q.counters[path] = counterHandle
	return nil
}

func (q *pdhQuery) collect() error {
	ret, _, _ := procPdhCollectData.Call(q.handle)
	if ret != pdhNoError {
		return fmt.Errorf("PdhCollectQueryData failed: 0x%X", ret)
	}
	return nil
}

func (q *pdhQuery) value(path string) (float64, error) {
	handle, ok := q.counters[path]
	if !ok {
		return 0, fmt.Errorf("counter not registered: %s", path)
	}
	var val pdhFmtCounterValue
	// PDH_FMT_DOUBLE = 0x00000200
	ret, _, _ := procPdhGetFormValue.Call(handle, 0x00000200, 0, uintptr(unsafe.Pointer(&val)))
	if ret != pdhNoError {
		return 0, fmt.Errorf("PdhGetFormattedCounterValue failed: 0x%X", ret)
	}
	return val.DoubleValue, nil
}

func (q *pdhQuery) close() {
	procPdhCloseQuery.Call(q.handle)
}

// dnsCounterPaths maps metric names to their Windows PDH counter paths.
// These are the English counter names (PdhAddEnglishCounterW handles locale).
var dnsCounterPaths = []struct {
	metric string
	path   string
	mtype  string
}{
	{"dns.performance.queries_received_total",           `\DNS\Total Query Received`, "c"},
	{"dns.performance.responses_sent_total",             `\DNS\Total Response Sent`, "c"},
	{"dns.performance.udp_queries_total",                `\DNS\UDP Query Received`, "c"},
	{"dns.performance.tcp_queries_total",                `\DNS\TCP Query Received`, "c"},
	{"dns.performance.recursive_queries_total",          `\DNS\Recursive Queries`, "c"},
	{"dns.performance.recursive_query_failures_total",   `\DNS\Recursive Query Failure`, "c"},
	{"dns.performance.recursive_query_timeouts_total",   `\DNS\Recursive TimeOut`, "c"},
	{"dns.performance.dynamic_updates_total",            `\DNS\Dynamic Update Received`, "c"},
	{"dns.performance.secure_updates_total",             `\DNS\Secure Update Received`, "c"},
	{"dns.performance.zone_transfer_requests_total",     `\DNS\Zone Transfer Request Received`, "c"},
	{"dns.performance.zone_transfer_success_total",      `\DNS\Zone Transfer Success`, "c"},
	{"dns.performance.zone_transfer_failures_total",     `\DNS\Zone Transfer Failure`, "c"},
	{"dns.performance.notify_sent_total",                `\DNS\Notify Sent`, "c"},
	{"dns.performance.notify_received_total",            `\DNS\Notify Received`, "c"},
	{"dns.performance.unmatched_responses_total",        `\DNS\Unmatched Responses Received`, "c"},
}

// CollectPerfmon reads DNS Server performance counters via the PDH API
// and emits dns.performance.* metrics.
func CollectPerfmon(client *statsd.Client, tags []string) []string {
	var lines []string
	ptags := append(tags, "category:performance")

	q, err := newPdhQuery()
	if err != nil {
		log.Printf("[dns-monitor] PDH open failed: %v", err)
		lines = append(lines, client.Line("dns.performance.available", 0, "g", ptags))
		return lines
	}
	defer q.close()

	// Register all counters — skip any that don't exist on this OS
	registered := 0
	for _, c := range dnsCounterPaths {
		if err := q.addCounter(c.path); err != nil {
			log.Printf("[dns-monitor] PDH counter skipped %s: %v", c.path, err)
		} else {
			registered++
		}
	}

	if registered == 0 {
		log.Printf("[dns-monitor] perfmon unavailable (no DNS PDH counters registered)")
		lines = append(lines, client.Line("dns.performance.available", 0, "g", ptags))
		return lines
	}

	// Collect once — rate counters need two samples for /sec values;
	// for totals (cumulative) one sample is sufficient.
	if err := q.collect(); err != nil {
		log.Printf("[dns-monitor] PDH collect failed: %v", err)
		lines = append(lines, client.Line("dns.performance.available", 0, "g", ptags))
		return lines
	}

	lines = append(lines, client.Line("dns.performance.available", 1, "g", ptags))

	for _, c := range dnsCounterPaths {
		val, err := q.value(c.path)
		if err != nil {
			continue
		}
		lines = append(lines, client.Line(c.metric, val, c.mtype, ptags))
	}

	log.Printf("[dns-monitor] perfmon metrics: %d", len(lines))
	return lines
}
