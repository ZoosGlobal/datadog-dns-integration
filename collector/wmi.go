// collector/wmi.go
// Zoos Global — Microsoft DNS Monitor for Datadog
// https://www.zoosglobal.com
//
// WMI query helpers. All DNS statistics, process info, and service
// state are read via WMI — no PowerShell subprocess needed.
//
//go:build windows
// +build windows

package collector

import (
	"fmt"

	"github.com/StackExchange/wmi"
)

// queryWMI executes a WQL query and populates dst (must be a pointer to a slice of structs).
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
	CacheHits                   uint64 // may be zero on older OS
	DynamicUpdateReceived       uint64
	SecureUpdateReceived        uint64
	ZoneTransferRequestReceived uint64
	ZoneTransferSuccess         uint64
	ZoneTransferFailure         uint64
	NotifySent                  uint64
	NotifyReceived              uint64
	UnmatchedResponsesReceived  uint64
}

// Win32_Process — DNS process metrics.
type Win32_Process struct {
	Name             string
	WorkingSetSize   uint64
	VirtualSize      uint64
	ThreadCount      uint32
	HandleCount      uint32
	ReadOperationCount  uint64
	WriteOperationCount uint64
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