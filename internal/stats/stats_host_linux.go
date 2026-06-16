//go:build linux

package stats

import (
	"context"
	"os"
	"os/exec"
	"strconv"
	"strings"
	"time"
)

// hostCPUSampleInterval is the gap between the two CPUUsageNSec reads used to
// turn systemd's cumulative CPU counter into an instantaneous rate. Kept short
// because Read runs this concurrently with the ~3s podman stream, so it adds no
// wall time of its own.
var hostCPUSampleInterval = 900 * time.Millisecond

// hostCmdTimeout bounds each systemctl call so a wedged systemd never hangs the
// stats read.
const hostCmdTimeout = 3 * time.Second

// readHostProcesses reports the resource usage of lerd's own host-side processes
// (the lerd-ui/watcher/tray daemons and any host-side workers such as a Vite or
// host-proxy dev server run via fnm) using systemd's per-unit cgroup accounting.
// Container units appear here too — Read drops those by name so the podman
// measurement wins. Linux only; the macOS stub returns nothing.
func readHostProcesses() ([]ContainerStat, error) {
	units := listLerdServices()
	if len(units) == 0 {
		return nil, nil
	}
	start := time.Now()
	first := showProps(units)
	time.Sleep(hostCPUSampleInterval)
	elapsed := time.Since(start).Seconds()
	cur := showProps(units)

	totalRAM := hostTotalRAM()
	var rows []ContainerStat
	for _, u := range units {
		c, ok := cur[u]
		if !ok {
			continue
		}
		cpuPct := 0.0
		if prev, ok := first[u]; ok && elapsed > 0 && c.cpuNsec >= prev.cpuNsec {
			cpuPct = float64(c.cpuNsec-prev.cpuNsec) / 1e9 / elapsed * 100
		}
		memPct := 0.0
		if totalRAM > 0 {
			memPct = float64(c.memBytes) / float64(totalRAM) * 100
		}
		rows = append(rows, ContainerStat{
			Name:       strings.TrimSuffix(u, ".service"),
			CPUPercent: cpuPct,
			MemBytes:   c.memBytes,
			MemLimit:   totalRAM,
			MemPercent: memPct,
		})
	}
	return rows, nil
}

// listLerdServices returns the running lerd-prefixed user services. This
// includes container quadlet units (lerd-mysql.service, …) which Read dedupes
// against the podman rows, leaving only the genuine host-side processes.
func listLerdServices() []string {
	ctx, cancel := context.WithTimeout(context.Background(), hostCmdTimeout)
	defer cancel()
	out, err := exec.CommandContext(ctx, "systemctl", "--user", "list-units",
		"--type=service", "--state=running", "--no-legend", "--plain", "--no-pager",
		"lerd-*.service").Output()
	if err != nil {
		return nil
	}
	var units []string
	for _, line := range strings.Split(strings.TrimSpace(string(out)), "\n") {
		fields := strings.Fields(line)
		if len(fields) == 0 {
			continue
		}
		name := fields[0]
		if strings.HasPrefix(name, "lerd-") && strings.HasSuffix(name, ".service") {
			units = append(units, name)
		}
	}
	return units
}

type hostProps struct {
	cpuNsec  uint64
	memBytes int64
}

// showProps batches one `systemctl show` over all units and parses the per-unit
// CPU and memory counters. Units with accounting disabled report "[not set]"
// (left as zero). Blocks are separated by blank lines and labelled by Id.
func showProps(units []string) map[string]hostProps {
	ctx, cancel := context.WithTimeout(context.Background(), hostCmdTimeout)
	defer cancel()
	args := append([]string{"--user", "show", "-p", "Id", "-p", "CPUUsageNSec", "-p", "MemoryCurrent"}, units...)
	out, err := exec.CommandContext(ctx, "systemctl", args...).Output()
	if err != nil {
		return nil
	}
	res := make(map[string]hostProps, len(units))
	var id string
	var p hostProps
	flush := func() {
		if id != "" {
			res[id] = p
		}
		id, p = "", hostProps{}
	}
	for _, line := range strings.Split(string(out), "\n") {
		line = strings.TrimSpace(line)
		if line == "" {
			flush()
			continue
		}
		key, val, ok := strings.Cut(line, "=")
		if !ok {
			continue
		}
		switch key {
		case "Id":
			id = val
		case "CPUUsageNSec":
			if n, err := strconv.ParseUint(val, 10, 64); err == nil {
				p.cpuNsec = n
			}
		case "MemoryCurrent":
			if n, err := strconv.ParseInt(val, 10, 64); err == nil {
				p.memBytes = n
			}
		}
	}
	flush()
	return res
}

// hostTotalRAM reads MemTotal from /proc/meminfo (bytes), used as the host
// memory denominator for host-process rows so the dashboard's "% of host" stays
// consistent whether or not any container reported a limit.
func hostTotalRAM() int64 {
	data, err := os.ReadFile("/proc/meminfo")
	if err != nil {
		return 0
	}
	for _, line := range strings.Split(string(data), "\n") {
		if !strings.HasPrefix(line, "MemTotal:") {
			continue
		}
		fields := strings.Fields(line)
		if len(fields) >= 2 {
			if kb, err := strconv.ParseInt(fields[1], 10, 64); err == nil {
				return kb * 1024
			}
		}
	}
	return 0
}
