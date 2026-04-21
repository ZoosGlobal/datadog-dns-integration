# checks.d/dns_monitor.py
# Zoos Global — Microsoft DNS Monitor for Datadog
# https://www.zoosglobal.com
#
# Thin Agent check wrapper — all metric collection is done by dns-monitor.exe.
# This file exists solely so the Datadog Agent can schedule and supervise
# the binary via its standard check lifecycle.
#
# The binary:
#   - reads config.yaml next to itself
#   - collects all DNS metrics
#   - pushes directly to DogStatsD 127.0.0.1:8125
#   - exits cleanly
#
# The Agent handles: scheduling, timeout enforcement, failure logging.
# This wrapper handles: nothing — just calls the binary.

import os
import subprocess
from datadog_checks.base import AgentCheck


class DnsMonitorCheck(AgentCheck):

    def check(self, instance):
        binary_path = instance.get(
            'binary_path',
            r'C:\ProgramData\Datadog\dns-monitor.exe'
        )
        config_path = instance.get(
            'config_path',
            r'C:\ProgramData\Datadog\dns-monitor-config.yaml'
        )
        timeout = instance.get('timeout', 55)

        if not os.path.isfile(binary_path):
            self.service_check(
                'dns_monitor.binary_present',
                AgentCheck.CRITICAL,
                message=f'dns-monitor.exe not found at: {binary_path}'
            )
            return

        self.service_check('dns_monitor.binary_present', AgentCheck.OK)

        try:
            result = subprocess.run(
                [binary_path, '--config', config_path],
                timeout=timeout,
                capture_output=True,
                text=True
            )

            if result.returncode == 0:
                self.service_check('dns_monitor.collection', AgentCheck.OK)
            else:
                self.service_check(
                    'dns_monitor.collection',
                    AgentCheck.CRITICAL,
                    message=f'binary exited {result.returncode}: {result.stderr[:200]}'
                )

        except subprocess.TimeoutExpired:
            self.service_check(
                'dns_monitor.collection',
                AgentCheck.CRITICAL,
                message=f'dns-monitor.exe timed out after {timeout}s'
            )
        except Exception as e:
            self.service_check(
                'dns_monitor.collection',
                AgentCheck.CRITICAL,
                message=str(e)
            )