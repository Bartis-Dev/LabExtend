//go:build linux

package agent

import (
	"bufio"
	"os"
	"runtime"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"time"

	pb "github.com/Bartis-Dev/LabExtend/internal/grpc/pb"
)

// hostCollector samples /proc with state retained across calls so deltas
// (CPU %, network rates) can be computed. Safe for concurrent use.
type hostCollector struct {
	mu sync.Mutex

	prevCPU      cpuTotals
	prevSampleAt time.Time
}

func newHostCollector() *hostCollector { return &hostCollector{} }

// Sample reads current values and computes deltas against the previous call.
// On the very first call the rate fields will be 0 because there is no
// baseline to subtract.
func (c *hostCollector) Sample() *pb.Heartbeat {
	c.mu.Lock()
	defer c.mu.Unlock()

	hb := &pb.Heartbeat{}

	var si syscall.Sysinfo_t
	if err := syscall.Sysinfo(&si); err == nil {
		hb.UptimeSeconds = si.Uptime
		hb.LoadAvg_1M = float64(si.Loads[0]) / 65536.0
		hb.MemTotalBytes = si.Totalram * uint64(si.Unit)
	}

	// Memory used: prefer /proc/meminfo's MemAvailable (what `free`'s
	// 'available' column shows) over the Sysinfo `Totalram-Freeram-Bufferram`
	// formula, which does NOT subtract the page cache and so reports a node
	// with mostly-cached RAM as "almost full". MemAvailable is what Linux
	// itself considers usable without swapping — page-cache and reclaimable
	// slab are counted as free because the kernel can evict them instantly
	// when an app actually wants the memory.
	//
	// Fallback to the old Sysinfo formula if /proc/meminfo can't be read
	// (containers in restricted modes, oddball kernels).
	if total, available, ok := readMemInfo(); ok && total > 0 {
		hb.MemTotalBytes = total
		if available > total {
			available = total
		}
		hb.MemUsedBytes = total - available
	} else if hb.MemTotalBytes > 0 {
		var si2 syscall.Sysinfo_t
		if err := syscall.Sysinfo(&si2); err == nil {
			hb.MemUsedBytes = (si2.Totalram - si2.Freeram - si2.Bufferram) * uint64(si2.Unit)
		}
	}

	var st syscall.Statfs_t
	if err := syscall.Statfs("/", &st); err == nil {
		hb.DiskTotalBytes = st.Blocks * uint64(st.Bsize)
		hb.DiskUsedBytes = (st.Blocks - st.Bavail) * uint64(st.Bsize)
	}

	hb.CpuCores = uint32(runtime.NumCPU())

	// CPU%: from /proc/stat aggregate cpu line, deltas across samples.
	if cur, err := readCPUTotals(); err == nil {
		if !c.prevSampleAt.IsZero() && c.prevCPU.total > 0 {
			totalDelta := cur.total - c.prevCPU.total
			idleDelta := cur.idle - c.prevCPU.idle
			if totalDelta > 0 {
				hb.CpuPercent = 100.0 * float64(totalDelta-idleDelta) / float64(totalDelta)
			}
		}
		c.prevCPU = cur
	}

	if rx, tx, err := readNetBytes(); err == nil {
		hb.NetRxBytes = rx
		hb.NetTxBytes = tx
	}

	if rd, wr, err := readDiskBytes(); err == nil {
		hb.DiskReadBytes = rd
		hb.DiskWriteBytes = wr
	}

	c.prevSampleAt = time.Now()
	return hb
}

// readMemInfo parses /proc/meminfo and returns (total, available, ok).
// Values are converted from kB to bytes. We only look at two lines; the
// scanner exits early once both are found.
//
// MemAvailable was introduced in kernel 3.14 (2014). Every Linux you'd
// realistically run today has it.
func readMemInfo() (total, available uint64, ok bool) {
	f, err := os.Open("/proc/meminfo")
	if err != nil {
		return 0, 0, false
	}
	defer f.Close()

	var haveTotal, haveAvail bool
	sc := bufio.NewScanner(f)
	for sc.Scan() {
		line := sc.Text()
		switch {
		case strings.HasPrefix(line, "MemTotal:"):
			total = parseMemInfoKB(line)
			haveTotal = true
		case strings.HasPrefix(line, "MemAvailable:"):
			available = parseMemInfoKB(line)
			haveAvail = true
		}
		if haveTotal && haveAvail {
			break
		}
	}
	return total, available, haveTotal && haveAvail
}

// parseMemInfoKB pulls the numeric kB value out of a /proc/meminfo line
// like "MemTotal:       16289340 kB".
func parseMemInfoKB(line string) uint64 {
	fields := strings.Fields(line)
	if len(fields) < 2 {
		return 0
	}
	kb, err := strconv.ParseUint(fields[1], 10, 64)
	if err != nil {
		return 0
	}
	return kb * 1024
}

// cpuTotals is the per-sample aggregate from /proc/stat cpu line.
type cpuTotals struct {
	total uint64
	idle  uint64
}

// readCPUTotals parses the first ("cpu") line of /proc/stat:
//
//	cpu user nice system idle iowait irq softirq steal guest guest_nice
func readCPUTotals() (cpuTotals, error) {
	f, err := os.Open("/proc/stat")
	if err != nil {
		return cpuTotals{}, err
	}
	defer f.Close()

	sc := bufio.NewScanner(f)
	if !sc.Scan() {
		return cpuTotals{}, sc.Err()
	}
	fields := strings.Fields(sc.Text())
	if len(fields) < 5 || fields[0] != "cpu" {
		return cpuTotals{}, nil
	}

	var t cpuTotals
	for i := 1; i < len(fields); i++ {
		v, err := strconv.ParseUint(fields[i], 10, 64)
		if err != nil {
			continue
		}
		t.total += v
		// field 4 (index 4 because [0]="cpu") = idle; we also count iowait
		// (index 5) as "idle" the way `top` does.
		if i == 4 || i == 5 {
			t.idle += v
		}
	}
	return t, nil
}

// readNetBytes sums rx/tx bytes across all non-loopback interfaces from
// /proc/net/dev.
func readNetBytes() (rx uint64, tx uint64, err error) {
	f, ferr := os.Open("/proc/net/dev")
	if ferr != nil {
		return 0, 0, ferr
	}
	defer f.Close()

	sc := bufio.NewScanner(f)
	// Skip 2 header lines.
	_ = sc.Scan()
	_ = sc.Scan()
	for sc.Scan() {
		line := sc.Text()
		colon := strings.IndexByte(line, ':')
		if colon < 0 {
			continue
		}
		iface := strings.TrimSpace(line[:colon])
		if iface == "lo" || strings.HasPrefix(iface, "docker") || strings.HasPrefix(iface, "veth") || strings.HasPrefix(iface, "br-") {
			continue
		}
		fields := strings.Fields(line[colon+1:])
		if len(fields) < 16 {
			continue
		}
		if v, e := strconv.ParseUint(fields[0], 10, 64); e == nil {
			rx += v
		}
		if v, e := strconv.ParseUint(fields[8], 10, 64); e == nil {
			tx += v
		}
	}
	return rx, tx, nil
}

// readDiskBytes sums read/written bytes across all real block devices from
// /proc/diskstats. Sectors are 512 bytes.
//
// Layout:  major minor name reads_completed merged sectors_read time
//          writes_completed merged sectors_written time io_in_progress
//          time_io weighted_time
func readDiskBytes() (read uint64, write uint64, err error) {
	f, ferr := os.Open("/proc/diskstats")
	if ferr != nil {
		return 0, 0, ferr
	}
	defer f.Close()

	sc := bufio.NewScanner(f)
	for sc.Scan() {
		fields := strings.Fields(sc.Text())
		if len(fields) < 14 {
			continue
		}
		name := fields[2]
		if !isRealBlockDevice(name) {
			continue
		}
		if v, e := strconv.ParseUint(fields[5], 10, 64); e == nil {
			read += v * 512
		}
		if v, e := strconv.ParseUint(fields[9], 10, 64); e == nil {
			write += v * 512
		}
	}
	return read, write, nil
}

// isRealBlockDevice excludes loop, ram, dm-, partitions of disks etc.
// We sum top-level disks only: sda, nvme0n1, vda, xvda — but NOT sda1, sda2.
func isRealBlockDevice(name string) bool {
	if strings.HasPrefix(name, "loop") || strings.HasPrefix(name, "ram") || strings.HasPrefix(name, "sr") || strings.HasPrefix(name, "dm-") || strings.HasPrefix(name, "fd") {
		return false
	}
	// nvme0n1 is whole device; nvme0n1p1 is a partition — skip 'p' partitions.
	if strings.HasPrefix(name, "nvme") && strings.Contains(name, "p") {
		return false
	}
	// sda is whole; sda1 is partition.
	if (strings.HasPrefix(name, "sd") || strings.HasPrefix(name, "vd") || strings.HasPrefix(name, "xvd") || strings.HasPrefix(name, "hd")) && len(name) > 3 {
		last := name[len(name)-1]
		if last >= '0' && last <= '9' {
			return false
		}
	}
	return true
}
