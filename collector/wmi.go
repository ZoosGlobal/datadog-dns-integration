// collector/wmi.go
// Zoos Global — Microsoft DNS Monitor for Datadog
// https://www.zoosglobal.com
//
//go:build windows
// +build windows

package collector

import (
	"fmt"

	"github.com/StackExchange/wmi"
)

// queryWMI executes a WQL query and populates dst.
func queryWMI(query string, dst interface{}) error {
	if err := wmi.Query(query, dst); err != nil {
		return fmt.Errorf("WMI query %q: %w", query, err)
	}
	return nil
}

// Win32_Service — minimal projection for DNS service state.
type Win32_Service struct {
	Name      string
	State     string
	StartMode string
}

// Win32_PerfFormattedData_DNS_DNSServer — DNS Server performance counters.
type Win32_PerfFormattedData_DNS_DNSServer struct {
	TotalQueryReceived          uint64
	TotalResponseSent           uint64
	UDPQueryReceived            uint64
	TCPQueryReceived            uint64
	RecursiveQueries            uint64
	RecursiveQueryFailure       uint64
	RecursiveTimeOut            uint64
	CacheHits                   uint64
	DynamicUpdateReceived       uint64
	SecureUpdateReceived        uint64
	ZoneTransferRequestReceived uint64
	ZoneTransferSuccess         uint64
	ZoneTransferFailure         uint64
	NotifySent                  uint64
	NotifyReceived              uint64
	UnmatchedResponsesReceived  uint64
}

// Win32_Process — DNS process metrics including creation time for uptime
// and PrivatePageCount for private memory (distinct from working set).
type Win32_Process struct {
	Name                string
	WorkingSetSize      uint64
	VirtualSize         uint64
	PrivatePageCount    uint64 // private committed memory — catches memory leaks
	ThreadCount         uint32
	HandleCount         uint32
	ReadOperationCount  uint64
	WriteOperationCount uint64
	CreationDate        string // WMI datetime string — used to compute uptime
}

// Win32_PerfFormattedData_PerfProc_Process — per-process CPU.
type Win32_PerfFormattedData_PerfProc_Process struct {
	Name                 string
	PercentProcessorTime uint64
}

// MicrosoftDNS_Zone — DNS zone info from the MicrosoftDNS WMI namespace.
type MicrosoftDNS_Zone struct {
	Name         string
	ZoneType     uint32
	IsPaused     bool
	IsShutdown   bool
	DsIntegrated bool
}