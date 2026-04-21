// collector/service.go
// Zoos Global — Microsoft DNS Monitor for Datadog
// https://www.zoosglobal.com
//
//go:build windows
// +build windows

package collector

import (
	"fmt"

	"github.com/ZoosGlobal/datadog-dns-integration/statsd"
)

// CollectService queries the Windows DNS Server service state via WMI
// and emits dns.service.* metrics.
func CollectService(client *statsd.Client, tags []string) []string {
	var lines []string

	var services []Win32_Service
	q := `SELECT Name, State, StartMode FROM Win32_Service WHERE Name = 'DNS'`
	if err := queryWMI(q, &services); err != nil {
		// Cannot query — emit CRITICAL
		lines = append(lines, client.Line("dns.service.up", 0, "g", append(tags, "category:service")))
		lines = append(lines, client.Line("dns.service.status", 2, "g", append(tags, "category:service")))
		return lines
	}

	if len(services) == 0 {
		// Service not found
		lines = append(lines, client.Line("dns.service.up", 0, "g", append(tags, "category:service")))
		lines = append(lines, client.Line("dns.service.status", 2, "g", append(tags, "category:service")))
		return lines
	}

	svc := services[0]
	isRunning := 0.0
	scStatus := 2.0 // CRITICAL by default
	if svc.State == "Running" {
		isRunning = 1
		scStatus = 0 // OK
	}

	isAuto := 0.0
	if svc.StartMode == "Auto" {
		isAuto = 1
	}

	stags := append(tags, "category:service")
	lines = append(lines,
		client.Line("dns.service.up", isRunning, "g", stags),
		client.Line("dns.service.status", scStatus, "g", stags),
		client.Line("dns.service.start_auto", isAuto, "g", stags),
	)

	// Also check Windows Internal Database (WID) — used when DNS is AD-integrated
	var widSvc []Win32_Service
	widQ := `SELECT Name, State FROM Win32_Service WHERE Name = 'MSSQL$MICROSOFT##WID'`
	if err := queryWMI(widQ, &widSvc); err == nil && len(widSvc) > 0 {
		widUp := 0.0
		if widSvc[0].State == "Running" {
			widUp = 1
		}
		lines = append(lines, client.Line("dns.service.wid_running", widUp, "g", stags))
	} else {
		// WID not present — SQL Server backend or standalone
		lines = append(lines, client.Line("dns.service.wid_running", -1, "g", stags))
	}

	_ = fmt.Sprintf("service state: %s", svc.State) // keep import
	return lines
}